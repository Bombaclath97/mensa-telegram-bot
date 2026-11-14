package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/Bombaclath97/mensa-telegram-bot/cleanup"
	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	"github.com/Bombaclath97/mensa-telegram-bot/membership"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
	"github.com/Bombaclath97/mensa-telegram-bot/tolgee"
)

// Admin-only operational commands. Like /manual_add they are not published in the command
// menu, and they answer nothing at all to a non-admin: their existence must not leak.
//
// Replies are in English and do not go through Tolgee: they are for operators.

// maxListed caps how many members a reply lists, to stay under Telegram's 4096-char limit.
const maxListed = 40

// telegramMaxText is Telegram's limit on one message.
const telegramMaxText = 4096

// adminFromUpdate returns the sender if they are a bot admin in a private chat. Anyone else
// gets ok=false and must be ignored silently.
func adminFromUpdate(update *models.Update, command string) (chatID int64, ok bool) {
	if !IsPrivateChat(update) || update.Message == nil || update.Message.From == nil {
		return 0, false
	}
	userID := update.Message.From.ID
	isAdmin, err := database.NewUserRepository().IsBotAdmin(userID)
	if err != nil {
		logger.Error("Failed to check bot admin status", map[string]interface{}{
			"error": err.Error(), "user_id": userID, "command": command,
		})
		return 0, false
	}
	if !isAdmin {
		logger.Debug("Ignoring admin command from non-admin", map[string]interface{}{"user_id": userID, "command": command})
		return 0, false
	}
	return update.Message.Chat.ID, true
}

// SweepPreviewCommandHandler handles /sweep_preview: a full dry-run sweep on demand, with
// the result sent back to the admin. Nothing is changed, whatever CLEANUP_DRY_RUN says.
func SweepPreviewCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, ok := adminFromUpdate(update, "sweep_preview")
	if !ok {
		return
	}
	logger.Info("Admin requested a sweep preview", map[string]interface{}{"admin_id": update.Message.From.ID})

	tb.Send(ctx, chatID, "🔍 Running a dry-run sweep. It checks every member against the Mensa API, so it can take a minute. Nothing will be changed.")

	report, err := cleanup.Preview(ctx)
	if errors.Is(err, cleanup.ErrSweepInProgress) {
		tb.Send(ctx, chatID, "⏳ A sweep is already running. Try again in a few minutes.")
		return
	}
	if err != nil && !errors.Is(err, cleanup.ErrCircuitBreaker) {
		tb.Send(ctx, chatID, fmt.Sprintf("🚨 <b>Preview failed</b>\n\n<code>%s</code>", tolgee.EscapeHTML(err.Error())))
		return
	}

	groups := map[int64]int{}
	assocRepo := database.NewGroupAssociationRepository()
	for _, u := range report.ExpiredMembers {
		if assocs, err := assocRepo.GetUserGroups(u.TelegramID); err == nil {
			groups[u.TelegramID] = len(assocs)
		} else {
			groups[u.TelegramID] = -1
		}
	}

	tb.Send(ctx, chatID, FormatSweepPreview(report, cleanup.Armed(), groups))
}

// FormatSweepPreview renders a preview report (HTML). groups maps a member's Telegram ID to
// how many groups they are recorded in (-1: unknown).
func FormatSweepPreview(r cleanup.Report, armed bool, groups map[int64]int) string {
	var b strings.Builder
	b.WriteString("🔍 <b>Sweep preview</b> (dry run, nothing was changed)\n\n")
	fmt.Fprintf(&b, "Members: <b>%d</b> · checked %d · skipped %d · inconclusive %d\n", r.TotalMembers, r.Checked, r.Skipped, r.Inconclusive)
	fmt.Fprintf(&b, "Expired: <b>%d</b>\n\n", r.Expired)

	if armed {
		b.WriteString("The scheduled sweep is <b>ARMED</b>: it removes members for real.\n")
	} else {
		b.WriteString("The scheduled sweep is in <b>DRY RUN</b>: it only logs.\n")
	}

	switch {
	case r.BreakerTripped:
		fmt.Fprintf(&b, "🚨 <b>The circuit breaker would trip</b> (%s): a live sweep would remove <b>nobody</b>.\n", tolgee.EscapeHTML(r.BreakerReason))
	case r.Expired == 0:
		b.WriteString("✅ Nobody is expired.\n")
	default:
		b.WriteString("✅ Within the safety limits: a live sweep would remove the members below.\n")
	}
	if r.Checked > 0 && r.Inconclusive*2 >= r.Checked {
		fmt.Fprintf(&b, "⚠️ <b>Mensa API looks unhealthy</b>: %d of %d checks were inconclusive and counted as active.\n", r.Inconclusive, r.Checked)
	}

	if len(r.ExpiredMembers) > 0 {
		b.WriteString("\n<b>Expired members</b>\n")
		for i, u := range r.ExpiredMembers {
			if i == maxListed {
				fmt.Fprintf(&b, "… and %d more (full list in the logs)\n", len(r.ExpiredMembers)-maxListed)
				break
			}
			n := groups[u.TelegramID]
			groupText := fmt.Sprintf("%d group(s)", n)
			if n < 0 {
				groupText = "groups unknown"
			}
			fmt.Fprintf(&b, "• %s %s · #%d · <code>%d</code> · %s\n",
				tolgee.EscapeHTML(u.FirstName), tolgee.EscapeHTML(u.LastName), u.MemberNumber, u.TelegramID, groupText)
		}
	}

	return truncate(strings.TrimRight(b.String(), "\n"))
}

// PendingCommandHandler handles /pending: every group removal /elimina could not finish.
func PendingCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID, ok := adminFromUpdate(update, "pending")
	if !ok {
		return
	}
	rows, err := database.NewPendingRemovalRepository().List(0)
	if err != nil {
		logger.Error("Failed to list pending removals", map[string]interface{}{"error": err.Error()})
		tb.Send(ctx, chatID, fmt.Sprintf("🚨 Could not list pending removals: <code>%s</code>", tolgee.EscapeHTML(err.Error())))
		return
	}
	tb.Send(ctx, chatID, FormatPending(rows))
}

// FormatPending renders the pending-removal queue (HTML).
func FormatPending(rows []database.PendingRemoval) string {
	if len(rows) == 0 {
		return "✅ No pending removals: every /elimina finished."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "⏳ <b>%d pending removal(s)</b>\n", len(rows))
	fmt.Fprintf(&b, "Retried every %s, up to %d attempts.\n", membership.PendingInterval(), membership.MaxPendingAttempts)
	for i, r := range rows {
		if i == maxListed {
			fmt.Fprintf(&b, "\n… and %d more", len(rows)-maxListed)
			break
		}
		status := fmt.Sprintf("%d/%d attempts", r.Attempts, membership.MaxPendingAttempts)
		if r.Attempts >= membership.MaxPendingAttempts {
			status = "🚨 <b>gave up</b>, remove by hand"
		}
		fmt.Fprintf(&b, "\n• user <code>%d</code> in group <code>%d</code>: %s", r.UserID, r.GroupID, status)
		if r.LastError != "" {
			fmt.Fprintf(&b, "\n  <i>%s</i>", tolgee.EscapeHTML(r.LastError))
		}
	}
	return truncate(b.String())
}

// truncate keeps a message under Telegram's limit without cutting through an HTML tag: it
// cuts at the last line break that fits.
func truncate(s string) string {
	if len(s) <= telegramMaxText {
		return s
	}
	const note = "\n… (truncated)"
	cut := strings.LastIndex(s[:telegramMaxText-len(note)], "\n")
	if cut < 0 {
		cut = telegramMaxText - len(note)
	}
	return s[:cut] + note
}
