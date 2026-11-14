package membership

import (
	"context"
	"fmt"

	"github.com/go-telegram/bot/models"

	"github.com/Bombaclath97/mensa-telegram-bot/alert"
	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
	"github.com/Bombaclath97/mensa-telegram-bot/tolgee"
)

// RecordBotGone marks a group as left when cause says the bot can no longer see it, and
// alerts the admins the first time. It does nothing for any other error.
//
// The my_chat_member update normally records this, but it is missed when the bot is
// removed while it is down, or when the chat is deleted outright. Every path that talks to
// groups calls this, so the groups table heals itself on first contact.
//
// Reports whether the group was newly marked as left.
func RecordBotGone(ctx context.Context, groupID int64, cause error) bool {
	if !IsBotGone(cause) {
		return false
	}
	repo := database.NewGroupRepository()
	fields := map[string]interface{}{"group_id": groupID, "cause": cause.Error()}

	changed, err := repo.MarkLeft(groupID)
	if err != nil {
		fields["error"] = err.Error()
		logger.Error("membership: could not mark a vanished group as left", fields)
		return false
	}
	if !changed {
		return false
	}

	label := fmt.Sprintf("group <code>%d</code>", groupID)
	if g, err := repo.GetByID(groupID); err == nil && g != nil && g.Title != "" {
		label = fmt.Sprintf("<b>%s</b> (<code>%d</code>)", tolgee.EscapeHTML(g.Title), groupID)
	}

	logger.Warn("membership: the bot can no longer see this group, marked it as left", fields)
	alert.Admins(ctx, fmt.Sprintf("🚪 <b>The bot can no longer see %s</b>, so it was marked as left.\n\n"+
		"Telegram said: <code>%s</code>\n\nIf the bot should be there, add it back and make it an admin.",
		label, tolgee.EscapeHTML(cause.Error())))
	return true
}

// BotStatus maps the bot's Telegram chat-member status onto database.Group.Status.
func BotStatus(status models.ChatMemberType) string {
	switch status {
	case models.ChatMemberTypeAdministrator, models.ChatMemberTypeOwner:
		return database.GroupStatusAdmin
	case models.ChatMemberTypeMember, models.ChatMemberTypeRestricted:
		return database.GroupStatusMember
	default:
		return database.GroupStatusLeft
	}
}

// RefreshReport summarises RefreshGroups.
type RefreshReport struct {
	Total   int
	Updated int // status (and title) read and stored
	Left    int // newly found to be gone
	Failed  int // could not be read; left as they were
}

// RefreshGroups asks Telegram for the bot's own status in every known group and stores it,
// with the group's current title. It runs once at boot.
//
// It exists for two reasons. Rows written before the status column existed have no status,
// and some of them may be groups the bot was removed from long ago: those are exactly the
// rows that kept failing every member's group sync. And a my_chat_member update can be
// missed while the bot is down, so boot is a good moment to catch up.
func RefreshGroups(ctx context.Context) RefreshReport {
	var report RefreshReport
	repo := database.NewGroupRepository()
	groups, err := repo.GetAll()
	if err != nil {
		logger.Error("membership: could not load groups to refresh", map[string]interface{}{"error": err.Error()})
		return report
	}
	report.Total = len(groups)

	botID := tb.BotID()
	opt := Options{Delay: DefaultDelay()}

	for _, g := range groups {
		if ctx.Err() != nil {
			break
		}
		var member *models.ChatMember
		err := call(ctx, opt, func() error {
			var e error
			member, e = tb.GetChatMember(ctx, g.GroupID, botID)
			return e
		})
		if err != nil {
			if RecordBotGone(ctx, g.GroupID, err) {
				report.Left++
				continue
			}
			if IsBotGone(err) {
				continue // already recorded as left
			}
			report.Failed++
			logger.Warn("membership: could not read the bot's status in a group", map[string]interface{}{
				"group_id": g.GroupID, "error": err.Error(),
			})
			continue
		}

		status := BotStatus(member.Type)
		if status == database.GroupStatusLeft {
			if changed, _ := repo.MarkLeft(g.GroupID); changed {
				report.Left++
				logger.Warn("membership: the bot is no longer in this group, marked it as left", map[string]interface{}{"group_id": g.GroupID})
			}
			continue
		}

		title := ""
		_ = call(ctx, opt, func() error {
			chat, e := tb.GetChat(ctx, g.GroupID)
			if e == nil && chat != nil {
				title = chat.Title
			}
			return e
		})
		if err := repo.SetStatus(g.GroupID, status, title); err != nil {
			report.Failed++
			logger.Error("membership: could not store the bot's status in a group", map[string]interface{}{
				"group_id": g.GroupID, "error": err.Error(),
			})
			continue
		}
		report.Updated++
	}

	logger.Info("membership: group status refresh complete", map[string]interface{}{
		"total": report.Total, "updated": report.Updated, "left": report.Left, "failed": report.Failed,
	})
	return report
}
