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

// AppCommandHandler handles the /app command
func AppCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// Only respond in private chats
	if !IsPrivateChat(update) {
		return
	}

	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	logger.Debug("Processing /app command", map[string]interface{}{
		"user_id": userID,
		"chat_id": chatID,
	})

	userRepo := database.NewUserRepository()
	lang := userRepo.GetLanguage(userID)

	tb.Send(ctx, chatID, tolgee.T("telegrambot.appcommand.sendapp", lang))
}
