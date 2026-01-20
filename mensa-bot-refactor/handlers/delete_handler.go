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
	user, _ := userRepo.GetByID(userID)
	lang := userRepo.GetLanguage(userID)

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

	switch q.Data {
	case "delete:confirm":
		// Get user's groups before deletion
		groups, _ := groupAssocRepo.GetUserGroups(userID)

		// Kick from all groups
		for _, assoc := range groups {
			// Check user role in group
			chatMember, err := tb.GetChatMember(ctx, assoc.GroupID, userID)
			if err != nil {
				continue
			}

			// Don't kick if user is owner
			if chatMember.Owner != nil && chatMember.Owner.Status == "creator" {
				tb.Send(ctx, assoc.GroupID, tolgee.T("telegrambot.deletecommand.ownerdeleted", lang))
				continue
			}

			// Demote if admin
			if chatMember.Administrator != nil && chatMember.Administrator.Status == "administrator" {
				tb.DemoteUser(ctx, assoc.GroupID, userID)
			}

			// Kick user
			tb.KickUser(ctx, assoc.GroupID, userID)
		}

		// Delete user from database
		userRepo.Delete(userID)
		logger.Info("User deleted", map[string]interface{}{"user_id": userID})

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
