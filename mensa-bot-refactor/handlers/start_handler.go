package handlers

import (
	"context"

	"github.com/Mensa-Italia/mensa-telegram-bot/database"
	"github.com/Mensa-Italia/mensa-telegram-bot/logger"
	"github.com/Mensa-Italia/mensa-telegram-bot/model"
	tb "github.com/Mensa-Italia/mensa-telegram-bot/telegram_bot"
	"github.com/Mensa-Italia/mensa-telegram-bot/tolgee"
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
	lang := userRepo.GetLanguage(userID)

	// G21: (nil, nil) means "not registered", (nil, err) means the query FAILED.
	// Conflating them told registered members they were not registered.
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
