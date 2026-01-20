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

// StartCommandHandler handles the /start command
func StartCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// Only respond in private chats
	if !IsPrivateChat(update) {
		return
	}

	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	logger.Debug("Processing /start command", map[string]interface{}{
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
		// User not registered
		_, err := tb.Send(ctx, chatID, tolgee.T("telegrambot.startcommand.notregistered", lang))
		if err != nil {
			logger.Error("Failed to send start message", map[string]interface{}{"error": err.Error()})
		}
	} else {
		// User registered
		params := map[string]string{"name": user.FirstName}
		_, err := tb.Send(ctx, chatID, tolgee.T("telegrambot.startcommand.alreadyregistered", lang, params))
		if err != nil {
			logger.Error("Failed to send start message", map[string]interface{}{"error": err.Error()})
		}
	}
}
