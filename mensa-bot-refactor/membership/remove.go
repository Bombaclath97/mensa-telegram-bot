// Package membership removes members from Telegram groups.
//
// It is the single implementation of "take this person out of this group", shared by the
// expiry sweep (cleanup) and member-requested deletion (/elimina, the profile card). Before
// it existed the two paths each had their own copy, and they had drifted: the sweep kept the
// row when a kick failed, /elimina ignored every Telegram error and deleted the row anyway,
// orphaning the member inside the group with no record left.
//
// It also owns the retry queue for /elimina (pending_removals, see pending.go): a member who
// asks to be deleted has their profile deleted immediately, and any group we could not remove
// them from is retried in the background instead of being forgotten.
package membership

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	tb "github.com/Mensa-Italia/mensa-telegram-bot/telegram_bot"
)

// Outcome is what happened to one member in one group.
type Outcome int

const (
	// Kicked: removed (ban + unban). In dry run: would have been removed.
	Kicked Outcome = iota
	// AlreadyOut: they had left, or group admins had banned them. Nothing was sent, and in
	// particular an admin's ban was NOT lifted.
	AlreadyOut
	// OwnerWarned: they own the group, which nobody can kick. The group was told instead.
	OwnerWarned
	// Gone: the bot can no longer see the group (removed, deleted, migrated). Nothing to do.
	Gone
	// Failed: a transient error. The caller must keep enough state to retry.
	Failed
)

func (o Outcome) String() string {
	switch o {
	case Kicked:
		return "kicked"
	case AlreadyOut:
		return "already_out"
	case OwnerWarned:
		return "owner_warned"
	case Gone:
		return "gone"
	case Failed:
		return "failed"
	}
	return "unknown"
}

// Resolved reports whether the member needs nothing more from us in this group.
func (o Outcome) Resolved() bool { return o != Failed }

// Result describes one RemoveFromGroup call.
type Result struct {
	Outcome Outcome
	// Role is the member's status before we acted ("" if it could not be read).
	Role models.ChatMemberType
	// DemoteFailed is set when an admin could not be demoted. The kick was still attempted.
	DemoteFailed bool
	// Err is the error behind Failed or Gone.
	Err error
}

// Options tunes RemoveFromGroup.
type Options struct {
	// DryRun reads the member's role and reports what would happen, but sends nothing that
	// changes anything: no demote, no kick, no owner notice.
	DryRun bool
	// Delay is waited before every Telegram call, to stay inside rate limits.
	Delay time.Duration
	// OwnerNotice is posted in the group when the member is its owner. Empty posts nothing.
	OwnerNotice string
}

// maxAttempts is how many times one Telegram call is tried when Telegram answers 429.
const maxAttempts = 3

// maxRetryWait caps how long we honour a 429's retry_after. Telegram can ask for minutes;
// blocking a user-facing /elimina that long is worse than failing and retrying in the
// background.
const maxRetryWait = 30 * time.Second

// retryWait turns a 429's retry_after into a wait. A variable so tests do not sleep.
var retryWait = func(retryAfter int) time.Duration {
	d := time.Duration(retryAfter) * time.Second
	if d > maxRetryWait {
		d = maxRetryWait
	}
	if d <= 0 {
		d = time.Second
	}
	return d
}

// RemoveFromGroup takes userID out of groupID.
//
//	left / banned -> AlreadyOut, nothing sent
//	creator       -> OwnerWarned, the group gets OwnerNotice
//	administrator -> demote, then kick
//	anything else -> kick (ban + unban: they can rejoin later, it is not a permaban)
func RemoveFromGroup(ctx context.Context, groupID, userID int64, opt Options) Result {
	var member *models.ChatMember
	err := call(ctx, opt, func() error {
		var e error
		member, e = tb.GetChatMember(ctx, groupID, userID)
		return e
	})
	if err != nil {
		if IsGroupGone(err) {
			return Result{Outcome: Gone, Err: err}
		}
		return Result{Outcome: Failed, Err: err}
	}
	if member == nil {
		return Result{Outcome: Failed, Err: errors.New("telegram returned no chat member")}
	}

	res := Result{Role: member.Type}

	switch member.Type {
	case models.ChatMemberTypeLeft, models.ChatMemberTypeBanned:
		// Banned matters most here. KickUser unbans after banning so a kick is not a
		// permaban, and that unban would also lift a ban the group's admins had set.
		res.Outcome = AlreadyOut
		return res

	case models.ChatMemberTypeOwner:
		res.Outcome = OwnerWarned
		if !opt.DryRun && opt.OwnerNotice != "" {
			// Best effort: the owner stays either way, and failing to post is not a reason to
			// keep retrying the removal.
			_ = call(ctx, opt, func() error {
				_, e := tb.Send(ctx, groupID, opt.OwnerNotice)
				return e
			})
		}
		return res
	}

	if opt.DryRun {
		res.Outcome = Kicked
		return res
	}

	if member.Type == models.ChatMemberTypeAdministrator {
		// Telegram refuses to kick an admin. If the demote fails the kick is still worth
		// trying (the bot may have been given the right in the meantime); its error decides.
		if err := call(ctx, opt, func() error {
			_, e := tb.DemoteUser(ctx, groupID, userID)
			return e
		}); err != nil {
			res.DemoteFailed = true
		}
	}

	if err := call(ctx, opt, func() error { return tb.KickUser(ctx, groupID, userID) }); err != nil {
		if IsGroupGone(err) {
			res.Outcome, res.Err = Gone, err
			return res
		}
		res.Outcome, res.Err = Failed, err
		return res
	}

	res.Outcome = Kicked
	return res
}

// call runs fn after the configured delay, retrying on 429 up to maxAttempts.
func call(ctx context.Context, opt Options, fn func() error) error {
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := wait(ctx, opt.Delay); err != nil {
			return err
		}
		err = fn()
		var tooMany *bot.TooManyRequestsError
		if !errors.As(err, &tooMany) || attempt == maxAttempts {
			return err
		}
		if err := wait(ctx, retryWait(tooMany.RetryAfter)); err != nil {
			return err
		}
	}
	return err
}

func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// IsGroupGone reports whether a Telegram error means the group is permanently out of reach
// for the bot, as opposed to a transient failure worth retrying.
//
// Erring is safe in both directions: a false positive only drops a stale record, a false
// negative only defers a removal to a later retry. It never removes anybody.
func IsGroupGone(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{
		"chat not found",
		"bot is not a member",
		"bot was kicked",
		"group chat was upgraded",
		"chat_id is empty",
		"user not found",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// IsBotGone reports whether a Telegram error means the BOT can no longer see the group:
// it was removed, or the chat no longer exists. Stricter than IsGroupGone, which also
// counts errors about the user ("user not found") and the upgrade case; use this one before
// recording that the bot has left a group.
func IsBotGone(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"chat not found", "bot is not a member", "bot was kicked"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// DefaultDelay is the pause between Telegram calls, from CLEANUP_TELEGRAM_DELAY_MS
// (default 100ms). Shared with the sweep so one knob governs the bot's kick rate.
func DefaultDelay() time.Duration {
	ms := 100
	if raw := strings.TrimSpace(os.Getenv("CLEANUP_TELEGRAM_DELAY_MS")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			ms = v
		}
	}
	return time.Duration(ms) * time.Millisecond
}
