package handlers

import (
	"context"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	"github.com/Bombaclath97/mensa-telegram-bot/model"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
	"github.com/Bombaclath97/mensa-telegram-bot/tolgee"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// DeleteCommandHandler handles the /elimina command
func DeleteCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// Only respond in private chats
	if !IsPrivateChat(update) {
		return
	}

	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	logger.Debug("Processing /elimina command", map[string]interface{}{
		"user_id": userID,
		"chat_id": chatID,
	})

	// Skip if user has active registration
	if model.Drafts.Exists(userID) {
		return
	}

	userRepo := database.NewUserRepository()
	lang := userRepo.GetLanguage(userID)

	// G21: a transient DB error must not look like "not registered".
	user, err := userRepo.GetByID(userID)
	if err != nil {
		logger.Error("Failed to look up user", map[string]interface{}{
			"error":   err.Error(),
			"user_id": userID,
		})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}

	if user == nil {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.startcommand.notregistered", lang))
		return
	}

	// Show delete confirmation keyboard
	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{
					Text:         "\u2705 " + tolgee.T("telegrambot.deletecommand.confirmbutton", lang),
					CallbackData: "delete:confirm",
				},
			},
			{
				{
					Text:         "\u274C " + tolgee.T("telegrambot.general.cancel", lang),
					CallbackData: "delete:cancel",
				},
			},
		},
	}

	params := map[string]string{"name": user.FirstName}
	tb.SendWithKeyboard(ctx, chatID, tolgee.T("telegrambot.deletecommand.deleteprofile", lang, params), keyboard)
}

// DeleteCallbackHandler handles delete confirmation callbacks
func DeleteCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// Only respond in private chats
	if !IsPrivateChat(update) {
		return
	}

	q := update.CallbackQuery
	userID := q.From.ID
	chatID := q.Message.Message.Chat.ID
	messageID := q.Message.Message.ID

	logger.Debug("Processing delete callback", map[string]interface{}{
		"user_id":       userID,
		"callback_data": q.Data,
	})

	userRepo := database.NewUserRepository()
	groupAssocRepo := database.NewGroupAssociationRepository()
	lang := userRepo.GetLanguage(userID)

	_ = groupAssocRepo // deletion goes through DeleteMemberEverywhere

	switch q.Data {
	case "delete:confirm":
		if err := DeleteMemberEverywhere(ctx, userID, lang); err != nil {
			tb.AnswerCallbackSilent(ctx, q.ID)
			tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
			return
		}

		// Delete the confirmation message
		tb.DeleteMessage(ctx, chatID, messageID)

		tb.Send(ctx, chatID, tolgee.T("telegrambot.deletecommand.deleted", lang))

	case "delete:cancel":
		// Delete the confirmation message
		tb.DeleteMessage(ctx, chatID, messageID)

		tb.Send(ctx, chatID, tolgee.T("telegrambot.cancelcommand.cancelled", lang))
	}

	tb.AnswerCallbackSilent(ctx, q.ID)
}

// DeleteMemberEverywhere kicks a member out of every group the bot manages and then deletes
// their row (the group associations cascade with it).
//
// It is the single implementation of "remove this member", shared by /elimina and the
// profile card's delete button — a destructive path duplicated in two places is a path
// where only one copy gets the next bug fix.
//
// A group lookup failure ABORTS: deleting the row first would cascade the associations
// away and destroy the only record of which groups the member still needs kicking from.
func DeleteMemberEverywhere(ctx context.Context, userID int64, lang string) error {
	userRepo := database.NewUserRepository()
	groupAssocRepo := database.NewGroupAssociationRepository()

	groups, err := groupAssocRepo.GetUserGroups(userID)
	if err != nil {
		logger.Error("Failed to load user groups, aborting deletion", map[string]interface{}{
			"error":   err.Error(),
			"user_id": userID,
		})
		return err
	}

	for _, assoc := range groups {
		chatMember, err := tb.GetChatMember(ctx, assoc.GroupID, userID)
		if err != nil {
			continue
		}

		// An owner cannot be kicked by a bot — Telegram forbids it. Tell the group instead.
		if chatMember.Owner != nil && chatMember.Owner.Status == "creator" {
			tb.Send(ctx, assoc.GroupID, tolgee.T("telegrambot.deletecommand.ownerdeleted", lang))
			continue
		}

		// An admin must be demoted before they can be kicked.
		if chatMember.Administrator != nil && chatMember.Administrator.Status == "administrator" {
			tb.DemoteUser(ctx, assoc.GroupID, userID)
		}

		tb.KickUser(ctx, assoc.GroupID, userID)
	}

	if err := userRepo.Delete(userID); err != nil {
		logger.Error("Failed to delete user", map[string]interface{}{
			"error":   err.Error(),
			"user_id": userID,
		})
		return err
	}

	logger.Info("User deleted", map[string]interface{}{"user_id": userID, "groups": len(groups)})
	return nil
}
