// Package cleanup implements the membership-expiry sweep (gap G09).
//
// ############################################################################
// # THIS IS THE MOST DANGEROUS CODE IN THE REPOSITORY.                       #
// # It kicks users out of Telegram groups and deletes them from the DB.      #
// # Read this header before changing a single line.                          #
// ############################################################################
//
// The legacy implementation (mensa-bot/cleanupRoutine.go) contained TWO independent
// bugs that would EACH, ON THEIR OWN, have kicked and deleted the ENTIRE membership:
//
//  1. cleanupRoutine.go:18 called IsMembershipActive(user.TelegramID) — it passed the
//     TELEGRAM ID where the MEMBERSHIP NUMBER belongs. The API was then queried with a
//     nonsense member_id, answered "not active" for every single member, and the sweep
//     kicked and deleted all of them. The api package now makes this a COMPILE ERROR by
//     taking a distinct api.MemberNumber type; we pass user.MemberNumber (never .TelegramID).
//
//  2. The response DTO was tagged `json:"isMembershipActive"` while the API emits
//     `is_membership_active`. Every response therefore decoded to the zero value
//     (false == expired) for every member. The tag is fixed in api/mensa_api.go, and
//     cleanupAssertDTODecodes below RE-VERIFIES IT AT RUNTIME on every sweep instead of
//     trusting it: if the tag ever regresses, the sweep aborts instead of massacring.
//
// On top of not reproducing those, this implementation adds safety rails that the legacy
// did not have. They are load-bearing, not decoration:
//
//   - FAIL CLOSED: any error, timeout, non-200 or empty/suspicious payload from the API
//     means the user is treated as ACTIVE and skipped. An API outage is a no-op sweep,
//     never a massacre. Nobody is ever deleted on an inconclusive check.
//   - CIRCUIT BREAKER: the full expired set is computed BEFORE any destructive action. If
//     it exceeds an absolute count or a fraction of the membership, the whole sweep aborts
//     and deletes nobody. A sweep that wants to remove most of the membership is a bug.
//   - DRY RUN (default ON): logs every action it would take and changes nothing until an
//     operator explicitly sets CLEANUP_DRY_RUN=false.
//   - International members (/manual_add, IsInternational) are NEVER swept: their member
//     number is vouched for by a bot admin and does not exist in the Mensa Italia API, so
//     checking them would report every one of them as expired. See cleanupIsSweepable.
//   - Bounded concurrency for API calls and a throttle on Telegram calls (the legacy
//     spawned an unbounded goroutine per user).
//   - Every deletion is logged at info level with Telegram ID + member number (audit trail).
//
// Trigger: an internal ticker only. There is deliberately NO HTTP endpoint — the legacy's
// GET /cleanup_routine had no authentication whatsoever, so anyone who could reach the port
// could trigger a mass kick.
package cleanup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Mensa-Italia/mensa-telegram-bot/alert"
	"github.com/Mensa-Italia/mensa-telegram-bot/api"
	"github.com/Mensa-Italia/mensa-telegram-bot/database"
	"github.com/Mensa-Italia/mensa-telegram-bot/logger"
	"github.com/Mensa-Italia/mensa-telegram-bot/membership"
	tb "github.com/Mensa-Italia/mensa-telegram-bot/telegram_bot"
	"github.com/Mensa-Italia/mensa-telegram-bot/tolgee"

	"github.com/go-telegram/bot/models"
)

// Tolgee keys used by this package.
const (
	keyOwnerExpired      = "telegrambot.cleanup.ownerexpired"
	keyExpiredMembership = "telegrambot.cleanup.expiredmembership"
)

// Defaults for every tunable. All are overridable by env var; all are chosen to be safe
// rather than eager.
const (
	defaultIntervalHours      = 24
	defaultMaxDeletions       = 10   // absolute circuit-breaker cap
	defaultMaxFraction        = 0.25 // fractional circuit-breaker cap
	defaultFractionMinMembers = 20   // the fraction rule only applies above this population
	defaultAPIConcurrency     = 4
	defaultTelegramDelayMS    = 100
	defaultDryRun             = true // MUST be opt-out, see the package doc / report
	defaultSkipBotAdmins      = true
)

// ErrSweepInProgress is returned by RunOnce when a sweep is already running.
var ErrSweepInProgress = errors.New("cleanup: a sweep is already in progress")

// ErrCircuitBreaker is returned when the expired set is implausibly large and the sweep
// refuses to touch anybody.
var ErrCircuitBreaker = errors.New("cleanup: circuit breaker tripped, sweep aborted, nobody was removed")

// running guards RunOnce against overlapping with itself (two ticks, or a tick and a
// manual call, must never sweep concurrently).
var running atomic.Bool

// Report summarises a sweep. Returned by RunOnce so callers/tests can assert on it.
type Report struct {
	DryRun          bool
	TotalMembers    int // rows in `users`
	Checked         int // members actually submitted to the API
	Skipped         int // not sweepable (international, no member number, bot admin)
	Inconclusive    int // API errored/timed out/returned junk -> treated as ACTIVE
	Expired         int // conclusively expired
	Deleted         int // user rows actually deleted (0 when DryRun)
	KickedFromGroup int // successful kicks (0 when DryRun)
	OwnersWarned    int // expired group owners we cannot kick
	Failed          int // members whose group cleanup failed; deletion deferred to next sweep
	BreakerTripped  bool
	BreakerReason   string
	Duration        time.Duration

	// ExpiredMembers are the members conclusively reported expired, in the order checked.
	// Filled even when the breaker trips, so a preview can show who it would have removed.
	ExpiredMembers []database.User
}

// config is resolved from the environment on every sweep, so an operator can retune the
// safety rails (or flip dry-run off) without a redeploy of a long-running process.
type cleanupConfig struct {
	dryRun             bool
	maxDeletions       int
	maxFraction        float64
	fractionMinMembers int
	apiConcurrency     int
	telegramDelay      time.Duration
	skipBotAdmins      bool
}

func cleanupLoadConfig() cleanupConfig {
	return cleanupConfig{
		dryRun:             cleanupEnvBool("CLEANUP_DRY_RUN", defaultDryRun),
		maxDeletions:       cleanupEnvInt("CLEANUP_MAX_DELETIONS", defaultMaxDeletions),
		maxFraction:        cleanupEnvFloat("CLEANUP_MAX_FRACTION", defaultMaxFraction),
		fractionMinMembers: cleanupEnvInt("CLEANUP_FRACTION_MIN_MEMBERS", defaultFractionMinMembers),
		apiConcurrency:     cleanupEnvInt("CLEANUP_API_CONCURRENCY", defaultAPIConcurrency),
		telegramDelay:      time.Duration(cleanupEnvInt("CLEANUP_TELEGRAM_DELAY_MS", defaultTelegramDelayMS)) * time.Millisecond,
		skipBotAdmins:      cleanupEnvBool("CLEANUP_SKIP_BOT_ADMINS", defaultSkipBotAdmins),
	}
}

// ---------------------------------------------------------------------------
// Ticker
// ---------------------------------------------------------------------------

// Interval is the sweep period, from CLEANUP_INTERVAL_HOURS (default 24h).
func Interval() time.Duration {
	h := cleanupEnvInt("CLEANUP_INTERVAL_HOURS", defaultIntervalHours)
	if h <= 0 {
		logger.Warn("cleanup: CLEANUP_INTERVAL_HOURS is not positive, falling back to the default", map[string]interface{}{
			"value":   h,
			"default": defaultIntervalHours,
		})
		h = defaultIntervalHours
	}
	return time.Duration(h) * time.Hour
}

// Start runs the sweep on an internal ticker until ctx is cancelled. It BLOCKS, so the
// integrator must call it as `go cleanup.Start(ctx)`.
//
// By default it does NOT sweep immediately on boot: a deploy that restarts the process
// while the Mensa API is briefly unreachable should not race straight into a destructive
// pass, and a crash-looping container would otherwise sweep on every restart. The first
// sweep happens one interval after start.
//
// CLEANUP_RUN_ON_START=true overrides that and sweeps once at boot. It exists so a sweep
// can actually be exercised on demand (the alternative — an HTTP trigger — is what the
// legacy had, and it was an unauthenticated endpoint that would mass-evict the membership
// for anyone who could reach the port). Leave it false in production.
func Start(ctx context.Context) {
	interval := Interval()
	cfg := cleanupLoadConfig()
	runOnStart := cleanupEnvBool("CLEANUP_RUN_ON_START", false)

	logger.Info("cleanup: sweep scheduler started", map[string]interface{}{
		"interval":       interval.String(),
		"dry_run":        cfg.dryRun,
		"max_deletions":  cfg.maxDeletions,
		"max_fraction":   cfg.maxFraction,
		"run_on_start":   runOnStart,
		"first_sweep_at": time.Now().Add(interval).Format(time.RFC3339),
	})
	if cfg.dryRun {
		// INFO, not WARN: dry run is the DEFAULT (see defaultDryRun) and therefore the normal
		// state of a fresh deployment, so this fires on every boot. A WARN that is always
		// present on a correctly configured system is exactly what teaches operators to skim
		// past WARNs — the genuinely abnormal case is the armed one, flagged just below.
		logger.Info("cleanup: DRY RUN is active — the sweep will report what it would do and delete nobody. Set CLEANUP_DRY_RUN=false to arm it.", nil)
	} else {
		logger.Warn("cleanup: ARMED — the sweep will DM and delete expired members for real.", nil)
	}

	if runOnStart {
		logger.Warn("cleanup: CLEANUP_RUN_ON_START is set — sweeping now", nil)
		scheduledSweep(ctx)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("cleanup: sweep scheduler stopped", nil)
			return
		case <-ticker.C:
			scheduledSweep(ctx)
		}
	}
}

// scheduledSweep runs one sweep and tells the admins about anything that needs a human.
func scheduledSweep(ctx context.Context) {
	report, err := RunOnce(ctx)
	if err != nil {
		logger.Error("cleanup: sweep did not complete", map[string]interface{}{"error": err.Error()})
	}
	if msg, ok := SweepAlert(report, err); ok {
		alert.Admins(ctx, msg)
	}
}

// SweepAlert decides whether a finished sweep is worth an admin's attention, and words the
// alert (HTML). A quiet sweep (nothing expired, API healthy) sends nothing.
func SweepAlert(r Report, err error) (string, bool) {
	var b strings.Builder

	switch {
	case errors.Is(err, ErrSweepInProgress):
		return "", false

	case errors.Is(err, ErrCircuitBreaker):
		fmt.Fprintf(&b, "🚨 <b>Cleanup sweep aborted by the circuit breaker</b>\n\n")
		fmt.Fprintf(&b, "%d of %d checked members came back expired: %s.\n", r.Expired, r.Checked, tolgee.EscapeHTML(r.BreakerReason))
		fmt.Fprintf(&b, "<b>Nobody was removed.</b> This is usually an API problem or a renewal wave, not a real mass expiry.\n\n")
		fmt.Fprintf(&b, "Run /sweep_preview to see who is affected.")
		return b.String(), true

	case err != nil:
		fmt.Fprintf(&b, "🚨 <b>Cleanup sweep failed</b>\n\n<code>%s</code>\n\nNobody was removed.", tolgee.EscapeHTML(err.Error()))
		return b.String(), true
	}

	// The API answered, but too often without a usable answer: those members were treated
	// as active, so expired members may be slipping through.
	apiUnhealthy := r.Checked > 0 && r.Inconclusive*2 >= r.Checked

	if r.Expired == 0 && !apiUnhealthy {
		return "", false
	}

	if r.DryRun {
		fmt.Fprintf(&b, "🔍 <b>Dry-run sweep</b>: %d member(s) would be removed. Nothing was changed.\n", r.Expired)
		if r.Expired > 0 {
			fmt.Fprintf(&b, "Run /sweep_preview for the list.\n")
		}
	} else {
		fmt.Fprintf(&b, "🧹 <b>Cleanup sweep</b>: %d expired, %d removed, %d group kick(s).\n", r.Expired, r.Deleted, r.KickedFromGroup)
		if r.Failed > 0 {
			fmt.Fprintf(&b, "⚠️ %d member(s) could not be fully removed and will be retried next sweep.\n", r.Failed)
		}
		if r.OwnersWarned > 0 {
			fmt.Fprintf(&b, "⚠️ %d expired member(s) own a group and cannot be kicked: a human has to sort that out.\n", r.OwnersWarned)
		}
	}
	if apiUnhealthy {
		fmt.Fprintf(&b, "⚠️ <b>Mensa API looks unhealthy</b>: %d of %d checks were inconclusive and counted as active.\n", r.Inconclusive, r.Checked)
	}
	return strings.TrimRight(b.String(), "\n"), true
}

// ---------------------------------------------------------------------------
// The sweep
// ---------------------------------------------------------------------------

// RunOnce performs a single sweep. It is safe to call concurrently with itself: a second
// caller returns ErrSweepInProgress instead of running a parallel (double-kicking) pass.
func RunOnce(ctx context.Context) (Report, error) {
	return runOnce(ctx, false)
}

// Preview runs a full sweep in DRY RUN, whatever CLEANUP_DRY_RUN says, and returns its
// report. It calls the Mensa API for every member and reads each expired member's role in
// their groups, but changes nothing. Behind the admin-only /sweep_preview command.
func Preview(ctx context.Context) (Report, error) {
	return runOnce(ctx, true)
}

// Armed reports whether the scheduled sweep would act for real (CLEANUP_DRY_RUN=false).
func Armed() bool {
	return !cleanupLoadConfig().dryRun
}

func runOnce(ctx context.Context, forceDryRun bool) (Report, error) {
	if !running.CompareAndSwap(false, true) {
		logger.Warn("cleanup: a sweep is already running, skipping this trigger", nil)
		return Report{}, ErrSweepInProgress
	}
	defer running.Store(false)

	start := time.Now()
	cfg := cleanupLoadConfig()
	if forceDryRun {
		cfg.dryRun = true
	}
	report := Report{DryRun: cfg.dryRun}

	// RAIL 0 — never trust the DTO. If the json tags on CheckValidityResponse ever regress
	// to camelCase again, EVERY member decodes as inactive and this sweep is a massacre.
	// Assert the decode before we are allowed to interpret a single API answer.
	if err := cleanupAssertDTODecodes(); err != nil {
		logger.Error("cleanup: ABORTING — the membership DTO does not decode the API payload; every member would look expired", map[string]interface{}{
			"error": err.Error(),
		})
		return report, err
	}

	// A missing endpoint would make every check fail. Fail-closed already turns that into a
	// no-op, but say so out loud rather than logging one error per member.
	if strings.TrimSpace(os.Getenv("API_ENDPOINT")) == "" {
		err := errors.New("cleanup: API_ENDPOINT is not set, refusing to sweep")
		logger.Error("cleanup: ABORTING — API_ENDPOINT is not configured", nil)
		return report, err
	}

	logger.Info("cleanup: starting membership sweep", map[string]interface{}{
		"dry_run":         cfg.dryRun,
		"max_deletions":   cfg.maxDeletions,
		"max_fraction":    cfg.maxFraction,
		"api_concurrency": cfg.apiConcurrency,
	})

	userRepo := database.NewUserRepository()
	users, err := userRepo.GetAll()
	if err != nil {
		logger.Error("cleanup: ABORTING — could not load the membership", map[string]interface{}{"error": err.Error()})
		return report, fmt.Errorf("cleanup: failed to load users: %w", err)
	}
	report.TotalMembers = len(users)

	if len(users) == 0 {
		report.Duration = time.Since(start)
		logger.Info("cleanup: no members, nothing to do", nil)
		return report, nil
	}

	// PHASE 1 — read-only. Determine who is conclusively expired. NOTHING is destroyed here.
	expired, stats := cleanupCollectExpired(ctx, users, cfg)
	report.Checked = stats.checked
	report.Skipped = stats.skipped
	report.Inconclusive = stats.inconclusive
	report.Expired = len(expired)
	report.ExpiredMembers = expired

	logger.Info("cleanup: membership check complete", map[string]interface{}{
		"total":        report.TotalMembers,
		"checked":      report.Checked,
		"skipped":      report.Skipped,
		"inconclusive": report.Inconclusive,
		"expired":      report.Expired,
	})

	if len(expired) == 0 {
		report.Duration = time.Since(start)
		logger.Info("cleanup: no expired memberships, sweep complete", map[string]interface{}{
			"duration": report.Duration.String(),
		})
		return report, nil
	}

	// PHASE 2 — CIRCUIT BREAKER. Refuse to act on an implausibly large expired set.
	if tripped, reason := cleanupBreakerTripped(len(expired), report.Checked, cfg); tripped {
		report.BreakerTripped = true
		report.BreakerReason = reason
		report.Duration = time.Since(start)
		logger.Error("cleanup: CIRCUIT BREAKER TRIPPED — the sweep wanted to remove an implausible number of members. NOBODY was kicked or deleted. This is almost certainly a bug or an API misbehaviour, not a real mass expiry. Investigate before re-arming.", map[string]interface{}{
			"reason":             reason,
			"would_have_removed": len(expired),
			"checked":            report.Checked,
			"total_members":      report.TotalMembers,
			"max_deletions":      cfg.maxDeletions,
			"max_fraction":       cfg.maxFraction,
			"victims":            cleanupIDs(expired),
		})
		return report, ErrCircuitBreaker
	}

	// PHASE 3 — destructive. Sequential on purpose: it keeps the audit log ordered and stays
	// well inside Telegram's rate limits (the legacy fired an unbounded goroutine per user).
	for _, user := range expired {
		if ctx.Err() != nil {
			logger.Warn("cleanup: context cancelled mid-sweep, stopping", map[string]interface{}{
				"processed": report.Deleted + report.Failed,
			})
			break
		}
		cleanupRemoveMember(ctx, user, cfg, &report)
	}

	report.Duration = time.Since(start)
	logger.Info("cleanup: sweep complete", map[string]interface{}{
		"dry_run":           report.DryRun,
		"expired":           report.Expired,
		"deleted":           report.Deleted,
		"kicked_from_group": report.KickedFromGroup,
		"owners_warned":     report.OwnersWarned,
		"failed":            report.Failed,
		"duration":          report.Duration.String(),
	})
	return report, nil
}

type cleanupStats struct {
	checked      int
	skipped      int
	inconclusive int
}

// cleanupCollectExpired checks every sweepable member against the Mensa API with bounded
// concurrency and returns ONLY those conclusively reported as expired.
//
// Everything that is not a clear, well-formed "is_membership_active: false" — an error, a
// timeout, a non-200, an empty payload — counts as ACTIVE and is left alone.
func cleanupCollectExpired(ctx context.Context, users []database.User, cfg cleanupConfig) ([]database.User, cleanupStats) {
	client := api.NewMensaAPIClient()

	concurrency := cfg.apiConcurrency
	if concurrency < 1 {
		concurrency = 1
	}

	var (
		mu      sync.Mutex
		expired []database.User
		stats   cleanupStats
		wg      sync.WaitGroup
	)
	sem := make(chan struct{}, concurrency)

	for i := range users {
		user := users[i] // copy: never close over the loop slot

		if reason, ok := cleanupIsSweepable(&user, cfg); !ok {
			mu.Lock()
			stats.skipped++
			mu.Unlock()
			logger.Debug("cleanup: member not sweepable, skipping", map[string]interface{}{
				"telegram_id":   user.TelegramID,
				"member_number": user.MemberNumber,
				"reason":        reason,
			})
			continue
		}

		if ctx.Err() != nil {
			break
		}

		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			active, conclusive := cleanupCheckActive(ctx, client, &user)

			mu.Lock()
			defer mu.Unlock()
			stats.checked++
			if !conclusive {
				stats.inconclusive++
				return
			}
			if !active {
				expired = append(expired, user)
			}
		}()
	}

	wg.Wait()
	return expired, stats
}

// cleanupCheckActive asks the API about ONE member.
//
// Returns (active, conclusive). The first value is meaningful ONLY when conclusive is true.
// conclusive==false means "we do not know" and the caller MUST treat the member as active.
func cleanupCheckActive(ctx context.Context, client *api.MensaAPIClient, user *database.User) (active bool, conclusive bool) {
	callCtx, cancel := context.WithTimeout(ctx, api.DefaultTimeout)
	defer cancel()

	// THE BUG THAT DELETED EVERYONE: this must be the MEMBERSHIP number, never user.TelegramID.
	resp, err := client.CheckMembershipValidity(callCtx, api.MemberNumber(user.MemberNumber))
	if err != nil {
		// FAIL CLOSED. Errors, timeouts and non-200s are NOT evidence of expiry.
		logger.Warn("cleanup: membership check failed, treating the member as ACTIVE and skipping", map[string]interface{}{
			"telegram_id":   user.TelegramID,
			"member_number": user.MemberNumber,
			"error":         err.Error(),
		})
		return true, false
	}
	if resp == nil {
		logger.Warn("cleanup: membership check returned no payload, treating the member as ACTIVE and skipping", map[string]interface{}{
			"telegram_id":   user.TelegramID,
			"member_number": user.MemberNumber,
		})
		return true, false
	}

	// Suspicious-payload guard. A fully zero-valued response (no username, no name, not
	// active) is what a REGRESSED JSON TAG or a changed API contract looks like from in here
	// — it is indistinguishable from "expired" if you only read the bool. We refuse to delete
	// on it. This is the exact shape of legacy bug #2 and it must never again be actionable.
	if resp.Username == "" && resp.Name == "" && !resp.IsMembershipActive {
		logger.Warn("cleanup: membership check returned an empty payload (possible API/DTO contract drift) — treating the member as ACTIVE and skipping. If you see this for MANY members, the sweep is broken, not the membership.", map[string]interface{}{
			"telegram_id":   user.TelegramID,
			"member_number": user.MemberNumber,
		})
		return true, false
	}

	return resp.IsMembershipActive, true
}

// cleanupIsSweepable decides whether a member may be checked against the Mensa Italia API
// at all. Anything not sweepable is left strictly alone.
func cleanupIsSweepable(user *database.User, cfg cleanupConfig) (reason string, ok bool) {
	// International members were vouched for by a bot admin via /manual_add. Their member
	// number is not a Mensa Italia number and the /valid endpoint does not know it, so it
	// would answer "not active" for EVERY one of them and the sweep would delete the entire
	// international cohort. They cannot be verified here; they are out of scope.
	if user.IsInternational {
		return "international member (/manual_add): not verifiable against the Mensa Italia API", false
	}

	// No membership number == nothing to ask the API about. Querying with 0 is exactly the
	// nonsense-member_id situation that caused the legacy massacre.
	if user.MemberNumber <= 0 {
		return "missing or invalid membership number", false
	}

	// Do not let an automated sweep remove the people who administer the bot; an expired
	// admin is a human conversation, not a kick. Logged loudly so somebody acts on it.
	if cfg.skipBotAdmins && (user.IsBotAdmin || database.IsSeededBotAdmin(user.TelegramID)) {
		return "bot administrator (CLEANUP_SKIP_BOT_ADMINS)", false
	}

	return "", true
}

// cleanupBreakerTripped implements the circuit breaker. Both caps are evaluated against the
// number of members we actually managed to CHECK, not the raw table size: if the API only
// answered for 12 of 400 members, "3 of 12 expired" must not be diluted into a harmless-
// looking 0.75% of the membership.
func cleanupBreakerTripped(expired, checked int, cfg cleanupConfig) (bool, string) {
	if cfg.maxDeletions > 0 && expired > cfg.maxDeletions {
		return true, fmt.Sprintf("expired (%d) exceeds the absolute cap CLEANUP_MAX_DELETIONS (%d)", expired, cfg.maxDeletions)
	}

	// The fraction rule is meaningless on a tiny population (2 expired out of 3 checked is
	// 66% and perfectly plausible), so it only bites once the checked population is big
	// enough for the ratio to mean something. The absolute cap governs below that.
	if cfg.maxFraction > 0 && checked >= cfg.fractionMinMembers {
		fraction := float64(expired) / float64(checked)
		if fraction > cfg.maxFraction {
			return true, fmt.Sprintf("expired fraction %.2f of the %d checked members exceeds CLEANUP_MAX_FRACTION (%.2f)", fraction, checked, cfg.maxFraction)
		}
	}
	return false, ""
}

// ---------------------------------------------------------------------------
// Destructive phase
// ---------------------------------------------------------------------------

// cleanupRemoveMember kicks ONE expired member out of every group, DMs them, and deletes
// their row (associations cascade).
//
// The DB row is deleted only when the group cleanup fully resolved. If a group action fails
// transiently, we leave the row alone and retry on the next sweep instead of dropping the
// associations and losing the ability to ever kick them.
func cleanupRemoveMember(ctx context.Context, user database.User, cfg cleanupConfig, report *Report) {
	lang := user.GetLanguage()
	audit := map[string]interface{}{
		"telegram_id":   user.TelegramID,
		"member_number": user.MemberNumber,
		"email":         user.MensaEmail,
		"dry_run":       cfg.dryRun,
	}
	logger.Info("cleanup: member has an EXPIRED membership, removing them", audit)

	assocRepo := database.NewGroupAssociationRepository()
	associations, err := assocRepo.GetUserGroups(user.TelegramID)
	if err != nil {
		// We do not know which groups they are in, so we cannot clean them up, so we must not
		// delete the row (that would orphan them inside the groups forever).
		report.Failed++
		logger.Error("cleanup: could not load the member's groups, deferring their removal to the next sweep", map[string]interface{}{
			"telegram_id": user.TelegramID,
			"error":       err.Error(),
		})
		return
	}

	// The SAME reasoning as the error branch above, for the case that is not an error.
	//
	// An empty association list has two meanings, and only one of them is safe:
	//
	//	groups_synced_at NULL  -> we have NEVER probed. We do not know where they are.
	//	groups_synced_at set   -> we probed, and they really are in no groups.
	//
	// Until the probe exists for a member, an empty list is ignorance, not knowledge. Reading
	// it as "in no groups" walks straight through the loop below with nothing to kick, marks
	// the sweep resolved, and deletes the row — leaving an expired member sitting in the
	// group forever with no record they exist. That is precisely the outcome the error branch
	// above was written to prevent, reached through the happy path.
	//
	// Note this defers ONLY the never-probed. A member with a set timestamp and no groups is
	// genuinely groupless and still gets deleted, as they should: a blanket "never delete on
	// empty" rule would leak those rows forever.
	if len(associations) == 0 && user.GroupsSyncedAt == nil {
		report.Failed++
		logger.Warn("cleanup: member has no associations and has never been group-synced, deferring their removal", map[string]interface{}{
			"telegram_id": user.TelegramID,
			"reason":      "groups_synced_at is NULL: an empty association list here means we never looked, not that they are in no groups",
		})
		return
	}

	groupsResolved := true
	for _, assoc := range associations {
		if ctx.Err() != nil {
			return
		}
		switch cleanupHandleGroup(ctx, user, assoc.GroupID, cfg, report) {
		case groupOutcomeFailed:
			groupsResolved = false
		case groupOutcomeKicked, groupOutcomeOwnerWarned, groupOutcomeGone:
			// resolved
		}
	}

	if !groupsResolved {
		report.Failed++
		logger.Warn("cleanup: some groups could not be cleaned up, KEEPING the member's row for the next sweep", map[string]interface{}{
			"telegram_id": user.TelegramID,
		})
		return
	}

	// DM them, then delete. The DM is best-effort: a user who blocked the bot must still be
	// removable.
	if cfg.dryRun {
		logger.Info("cleanup: [DRY RUN] would DM the member and delete their row", audit)
		return
	}

	cleanupThrottle(cfg)
	if _, err := tb.Send(ctx, user.TelegramID, tolgee.T(keyExpiredMembership, lang)); err != nil {
		logger.Warn("cleanup: could not notify the member of their expiry (deleting them anyway)", map[string]interface{}{
			"telegram_id": user.TelegramID,
			"error":       err.Error(),
		})
	}

	userRepo := database.NewUserRepository()
	if err := userRepo.Delete(user.TelegramID); err != nil {
		report.Failed++
		logger.Error("cleanup: FAILED to delete the member's row", map[string]interface{}{
			"telegram_id": user.TelegramID,
			"error":       err.Error(),
		})
		return
	}

	report.Deleted++
	// AUDIT TRAIL: this line is the record that a human was removed. Do not downgrade it.
	logger.Info("cleanup: DELETED member with expired membership", audit)
}

type groupOutcome int

const (
	groupOutcomeKicked groupOutcome = iota
	groupOutcomeOwnerWarned
	groupOutcomeGone   // the chat is unreachable / the bot is not in it: nothing to do
	groupOutcomeFailed // transient: retry next sweep
)

// cleanupHandleGroup removes ONE expired member from ONE group.
//
// The Telegram work is membership.RemoveFromGroup, shared with /elimina. This wrapper adds
// what is specific to the sweep: its report, its audit log lines, and dropping the stale
// association when the group is gone.
//
//	creator        -> cannot be kicked at all; warn the group instead
//	administrator  -> demote first, then kick
//	left / banned  -> already out; nothing is sent (an admin's ban is never lifted)
//	anything else  -> kick (ban + unban, i.e. a kick, not a permaban)
func cleanupHandleGroup(ctx context.Context, user database.User, groupID int64, cfg cleanupConfig, report *Report) groupOutcome {
	fields := map[string]interface{}{
		"telegram_id": user.TelegramID,
		"group_id":    groupID,
	}

	res := membership.RemoveFromGroup(ctx, groupID, user.TelegramID, membership.Options{
		DryRun:      cfg.dryRun,
		Delay:       cfg.telegramDelay,
		OwnerNotice: tolgee.T(keyOwnerExpired, database.DefaultLanguage),
	})
	if res.Role != "" {
		fields["role"] = string(res.Role)
	}
	if res.Err != nil {
		fields["error"] = res.Err.Error()
	}

	switch res.Outcome {
	case membership.Gone:
		logger.Warn("cleanup: group is unreachable (bot removed or chat deleted), dropping the stale association", fields)
		if !cfg.dryRun {
			membership.RecordBotGone(ctx, groupID, res.Err)
			assocRepo := database.NewGroupAssociationRepository()
			if delErr := assocRepo.Delete(user.TelegramID, groupID); delErr != nil {
				fields["error"] = delErr.Error()
				logger.Warn("cleanup: could not drop the stale association", fields)
			}
		}
		return groupOutcomeGone

	case membership.Failed:
		// Covers both an unreadable role (kicking blind would skip the required demote) and
		// a failed kick. Either way the row is kept and the next sweep retries.
		logger.Error("cleanup: FAILED to remove the member from the group, will retry next sweep", fields)
		return groupOutcomeFailed

	case membership.AlreadyOut:
		logger.Info("cleanup: member is no longer in the group (left or banned), nothing to do", fields)
		return groupOutcomeGone

	case membership.OwnerWarned:
		report.OwnersWarned++
		logger.Warn("cleanup: expired member is the OWNER of the group — cannot demote or kick them, warning the group instead", fields)
		if cfg.dryRun {
			logger.Info("cleanup: [DRY RUN] would warn the group about its expired owner", fields)
		}
		return groupOutcomeOwnerWarned
	}

	// Kicked.
	if cfg.dryRun {
		fields["would_demote_first"] = res.Role == models.ChatMemberTypeAdministrator
		logger.Info("cleanup: [DRY RUN] would kick the member from the group", fields)
		return groupOutcomeKicked
	}
	if res.DemoteFailed {
		logger.Warn("cleanup: could not demote the member, the kick went through anyway", fields)
	}
	report.KickedFromGroup++
	logger.Info("cleanup: kicked the expired member from the group", fields)
	return groupOutcomeKicked
}

// ---------------------------------------------------------------------------
// Guards & helpers
// ---------------------------------------------------------------------------

var (
	dtoAssertOnce sync.Once
	dtoAssertErr  error
)

// cleanupAssertDTODecodes proves, at runtime, that api.CheckValidityResponse still decodes
// the payload the Mensa API actually sends. Legacy bug #2 was a single wrong json tag that
// made every member decode as inactive; a unit test would not have protected the running
// process from a future edit. This does. If it fails, the sweep refuses to run.
func cleanupAssertDTODecodes() error {
	dtoAssertOnce.Do(func() {
		const payload = `{"username":"mario.rossi","name":"Mario Rossi","is_membership_active":true}`

		var got api.CheckValidityResponse
		if err := json.Unmarshal([]byte(payload), &got); err != nil {
			dtoAssertErr = fmt.Errorf("cleanup: DTO self-check failed to unmarshal the canonical /valid payload: %w", err)
			return
		}
		if !got.IsMembershipActive {
			dtoAssertErr = errors.New("cleanup: DTO SELF-CHECK FAILED — api.CheckValidityResponse.IsMembershipActive did not decode `is_membership_active: true`. The json tag has regressed (this is legacy bug #2) and EVERY member would be reported as expired. Refusing to sweep")
			return
		}
		if got.Username == "" || got.Name == "" {
			dtoAssertErr = errors.New("cleanup: DTO SELF-CHECK FAILED — api.CheckValidityResponse does not decode the username/name fields of the canonical /valid payload. Refusing to sweep")
			return
		}
	})
	return dtoAssertErr
}

// cleanupThrottle paces our Telegram calls. The legacy fired an unbounded goroutine per
// expired user and hammered the API.
func cleanupThrottle(cfg cleanupConfig) {
	if cfg.telegramDelay > 0 {
		time.Sleep(cfg.telegramDelay)
	}
}

func cleanupIDs(users []database.User) []int64 {
	ids := make([]int64, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.TelegramID)
	}
	return ids
}

func cleanupEnvInt(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		logger.Warn("cleanup: invalid integer env var, using the default", map[string]interface{}{
			"key": key, "value": raw, "default": def,
		})
		return def
	}
	return v
}

func cleanupEnvFloat(key string, def float64) float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		logger.Warn("cleanup: invalid float env var, using the default", map[string]interface{}{
			"key": key, "value": raw, "default": def,
		})
		return def
	}
	return v
}

// cleanupEnvBool defaults to `def` on anything it does not understand. For CLEANUP_DRY_RUN
// (def=true) that means a typo keeps the sweep SAFE rather than arming it.
func cleanupEnvBool(key string, def bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return def
	}
	switch raw {
	case "true", "1", "yes", "y", "on":
		return true
	case "false", "0", "no", "n", "off":
		return false
	default:
		logger.Warn("cleanup: invalid boolean env var, using the default", map[string]interface{}{
			"key": key, "value": raw, "default": def,
		})
		return def
	}
}
