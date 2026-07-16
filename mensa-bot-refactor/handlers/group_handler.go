package handlers

import (
	"context"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
	"github.com/Bombaclath97/mensa-telegram-bot/tolgee"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// MatchMyChatMember is the match predicate for MyChatMemberHandler.
//
// NOTE FOR THE INTEGRATOR: go-telegram dispatches to the FIRST matching handler only
// (see bot@v1.17.0/process_update.go), so "bot joined" and "bot promoted" MUST NOT be
// registered as two separate match funcs — a bot added straight to a group AS an admin
// matches both and only one would ever run. Register this single handler:
//
//	b.RegisterHandlerMatchFunc(handlers.MatchMyChatMember, handlers.MyChatMemberHandler)
//
// and it dispatches internally to the join / promotion branches.
func MatchMyChatMember(update *models.Update) bool {
	if update == nil || update.MyChatMember == nil {
		return false
	}
	chatType := update.MyChatMember.Chat.Type
	return chatType == models.ChatTypeGroup || chatType == models.ChatTypeSupergroup
}

// MyChatMemberHandler reacts to changes of the BOT's own membership in a group.
//
// MyChatMember updates always concern the bot itself, so no bot-ID check is needed.
// Unlike the legacy onBotJoinsGroup (which read update.Message.Chat.ID on a MyChatMember
// update and nil-panicked), this reads update.MyChatMember.Chat.ID, and unlike the legacy
// matchBotBecomesAdmin (which dereferenced the nillable NewChatMember.Administrator) it
// gates on the ChatMember.Type discriminator, which is always populated.
func MyChatMemberHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !MatchMyChatMember(update) {
		return
	}

	mcm := update.MyChatMember
	groupID := mcm.Chat.ID
	oldStatus := mcm.OldChatMember.Type
	newStatus := mcm.NewChatMember.Type

	logger.Debug("Bot chat member status changed", map[string]interface{}{
		"group_id":   groupID,
		"old_status": string(oldStatus),
		"new_status": string(newStatus),
	})

	joined := groupIsInsideStatus(newStatus) && !groupIsInsideStatus(oldStatus)
	promoted := newStatus == models.ChatMemberTypeAdministrator && oldStatus != models.ChatMemberTypeAdministrator

	if !joined && !promoted {
		// Left / kicked / demoted / restriction tweaks: nothing to persist here.
		return
	}

	groupRepo := database.NewGroupRepository()
	// Idempotent upsert; also guarantees the FK target exists before any association write.
	if err := groupRepo.Register(groupID); err != nil {
		logger.Error("Failed to register group", map[string]interface{}{
			"group_id": groupID,
			"error":    err.Error(),
		})
		return
	}

	if joined {
		// Greet the group in the language of whoever added the bot.
		userRepo := database.NewUserRepository()
		lang := userRepo.GetLanguage(mcm.From.ID)

		if _, err := tb.Send(ctx, groupID, tolgee.T("telegrambot.welcome.botjoined", lang)); err != nil {
			logger.Warn("Failed to greet group", map[string]interface{}{
				"group_id": groupID,
				"error":    err.Error(),
			})
		}

		logger.Info("Bot joined group", map[string]interface{}{"group_id": groupID})
	}

	if promoted {
		logger.Info("Bot promoted to administrator", map[string]interface{}{"group_id": groupID})
	}

	// Sync the group's human admins in both cases. When the bot is only a plain member it
	// usually cannot call getChatAdministrators; the call simply fails and is logged.
	groupSyncAdmins(ctx, groupID)
}

// groupIsInsideStatus reports whether the given status means the bot is present in the chat.
func groupIsInsideStatus(status models.ChatMemberType) bool {
	switch status {
	case models.ChatMemberTypeMember,
		models.ChatMemberTypeAdministrator,
		models.ChatMemberTypeOwner,
		models.ChatMemberTypeRestricted:
		return true
	default:
		// "left", "kicked" and the zero value (no previous membership) all mean "outside".
		return false
	}
}

// groupSyncAdmins flags every registered member who administers the group with
// is_group_admin = true. This is the only writer of that column: /profilo's "(admin)"
// suffix and /elimina's demote branch both depend on it.
func groupSyncAdmins(ctx context.Context, groupID int64) {
	admins, err := tb.GetChatAdministrators(ctx, groupID)
	if err != nil {
		logger.Error("Failed to get chat administrators", map[string]interface{}{
			"group_id": groupID,
			"error":    err.Error(),
		})
		return
	}

	userRepo := database.NewUserRepository()
	assocRepo := database.NewGroupAssociationRepository()

	for _, admin := range admins {
		adminUser := groupAdminUser(admin)
		if adminUser == nil || adminUser.IsBot {
			continue
		}

		registered, err := userRepo.Exists(adminUser.ID)
		if err != nil {
			logger.Error("Failed to check admin registration", map[string]interface{}{
				"user_id":  adminUser.ID,
				"group_id": groupID,
				"error":    err.Error(),
			})
			continue
		}
		if !registered {
			continue
		}

		// PromoteToAdmin creates the association if it is missing, then sets the flag.
		if err := assocRepo.PromoteToAdmin(adminUser.ID, groupID); err != nil {
			logger.Error("Failed to flag group admin", map[string]interface{}{
				"user_id":  adminUser.ID,
				"group_id": groupID,
				"error":    err.Error(),
			})
			continue
		}

		logger.Info("Flagged registered member as group admin", map[string]interface{}{
			"user_id":  adminUser.ID,
			"group_id": groupID,
		})
	}
}

// groupAdminUser extracts the Telegram user out of a ChatMember without dereferencing a
// nil variant pointer (the legacy code's crash).
func groupAdminUser(member models.ChatMember) *models.User {
	switch member.Type {
	case models.ChatMemberTypeOwner:
		if member.Owner == nil {
			return nil
		}
		return member.Owner.User
	case models.ChatMemberTypeAdministrator:
		if member.Administrator == nil {
			return nil
		}
		return &member.Administrator.User
	default:
		return nil
	}
}
