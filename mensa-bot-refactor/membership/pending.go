package membership

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Bombaclath97/mensa-telegram-bot/alert"
	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	"github.com/Bombaclath97/mensa-telegram-bot/tolgee"
)

// MaxPendingAttempts is how many background retries a pending removal gets. After that it
// stays in pending_removals as a record for a human and is no longer retried.
const MaxPendingAttempts = 5

const defaultPendingIntervalMinutes = 60

// keyOwnerDeleted is posted in a group whose owner deleted their profile.
const keyOwnerDeleted = "telegrambot.deletecommand.ownerdeleted"

// DrainReport summarises one pass over pending_removals.
type DrainReport struct {
	Total        int // rows looked at
	Resolved     int // removed, already out, owner warned, or group gone: row deleted
	Reregistered int // the member registered again, so the removal was dropped
	Failed       int // still failing: attempts bumped
	GaveUp       int // reached MaxPendingAttempts on this pass

	// GaveUpRows are the removals abandoned on this pass, for the admin alert.
	GaveUpRows []database.PendingRemoval
}

var draining atomic.Bool

// DrainPendingRemovals retries every pending removal once.
//
// It ignores CLEANUP_DRY_RUN on purpose. That switch guards the expiry sweep, which removes
// people who did not ask to leave. Every row here is a member who asked to be removed, and
// /elimina itself has always acted immediately.
func DrainPendingRemovals(ctx context.Context) DrainReport {
	var report DrainReport
	if !draining.CompareAndSwap(false, true) {
		return report
	}
	defer draining.Store(false)

	repo := database.NewPendingRemovalRepository()
	rows, err := repo.List(MaxPendingAttempts)
	if err != nil {
		logger.Error("membership: could not load pending removals", map[string]interface{}{"error": err.Error()})
		return report
	}
	report.Total = len(rows)

	userRepo := database.NewUserRepository()
	opt := Options{Delay: DefaultDelay(), OwnerNotice: tolgee.T(keyOwnerDeleted, database.DefaultLanguage)}

	for _, row := range rows {
		if ctx.Err() != nil {
			break
		}
		fields := map[string]interface{}{"telegram_id": row.UserID, "group_id": row.GroupID, "attempt": row.Attempts + 1}

		// They registered again since asking to be deleted, so being in the group is now
		// legitimate. Kicking them would undo a valid join.
		registered, err := userRepo.Exists(row.UserID)
		if err != nil {
			logger.Warn("membership: could not check whether a pending removal re-registered, skipping it this pass", fields)
			continue
		}
		if registered {
			if err := repo.DeleteForUser(row.UserID); err == nil {
				report.Reregistered++
				logger.Info("membership: member registered again, dropping their pending removals", fields)
			}
			continue
		}

		res := RemoveFromGroup(ctx, row.GroupID, row.UserID, opt)
		fields["outcome"] = res.Outcome.String()

		if res.Outcome.Resolved() {
			if err := repo.Delete(row.UserID, row.GroupID); err != nil {
				logger.Warn("membership: resolved a pending removal but could not delete its row; it will be retried", fields)
				continue
			}
			report.Resolved++
			logger.Info("membership: pending removal resolved", fields)
			continue
		}

		report.Failed++
		errText := ""
		if res.Err != nil {
			errText = res.Err.Error()
		}
		fields["error"] = errText
		attempts, err := repo.RecordFailure(row.UserID, row.GroupID, errText)
		if err != nil {
			logger.Error("membership: could not record a failed pending removal", fields)
			continue
		}
		if attempts >= MaxPendingAttempts {
			report.GaveUp++
			row.Attempts = attempts
			row.LastError = errText
			report.GaveUpRows = append(report.GaveUpRows, row)
			logger.Error("membership: GIVING UP on a pending removal after repeated failures. The member deleted their profile but is still in this group; an admin has to remove them by hand.", fields)
			continue
		}
		logger.Warn("membership: pending removal failed, will retry", fields)
	}

	if report.Total > 0 {
		logger.Info("membership: pending removals pass complete", map[string]interface{}{
			"total": report.Total, "resolved": report.Resolved, "reregistered": report.Reregistered,
			"failed": report.Failed, "gave_up": report.GaveUp,
		})
	}
	return report
}

// PendingInterval is how often pending removals are retried, from
// PENDING_REMOVAL_INTERVAL_MINUTES (default 60).
func PendingInterval() time.Duration {
	m := defaultPendingIntervalMinutes
	if raw := strings.TrimSpace(os.Getenv("PENDING_REMOVAL_INTERVAL_MINUTES")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			m = v
		}
	}
	return time.Duration(m) * time.Minute
}

// StartPendingRemovals retries pending removals on a ticker until ctx is cancelled. It
// blocks, so call it as `go membership.StartPendingRemovals(ctx)`. The first pass runs at
// start: the queue only ever holds members who asked to leave, so there is no reason to
// wait an interval.
func StartPendingRemovals(ctx context.Context) {
	interval := PendingInterval()
	logger.Info("membership: pending-removal retrier started", map[string]interface{}{"interval": interval.String()})

	drainAndAlert(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			drainAndAlert(ctx)
		}
	}
}

func drainAndAlert(ctx context.Context) {
	if msg, ok := PendingAlert(DrainPendingRemovals(ctx)); ok {
		alert.Admins(ctx, msg)
	}
}

// PendingAlert words the admin alert for removals abandoned on a pass (HTML). Nothing
// abandoned, no alert.
func PendingAlert(r DrainReport) (string, bool) {
	if len(r.GaveUpRows) == 0 {
		return "", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🚨 <b>Gave up removing %d member(s) from a group</b>\n\n", len(r.GaveUpRows))
	b.WriteString("They deleted their profile with /elimina, but after repeated failures they are still in the group. Remove them by hand:\n")
	for _, row := range r.GaveUpRows {
		fmt.Fprintf(&b, "\n• user <code>%d</code> in group <code>%d</code> (%d attempts)", row.UserID, row.GroupID, row.Attempts)
		if row.LastError != "" {
			fmt.Fprintf(&b, ": <i>%s</i>", tolgee.EscapeHTML(row.LastError))
		}
	}
	b.WriteString("\n\n/pending lists every pending removal.")
	return b.String(), true
}
