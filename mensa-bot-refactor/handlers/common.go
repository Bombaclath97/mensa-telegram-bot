package handlers

import "github.com/go-telegram/bot/models"

// IsPrivateChat checks if the update comes from a private chat
func IsPrivateChat(update *models.Update) bool {
	if update.Message != nil {
		return update.Message.Chat.Type == "private"
	}
	if update.CallbackQuery != nil && update.CallbackQuery.Message.Message != nil {
		return update.CallbackQuery.Message.Message.Chat.Type == "private"
	}
	return false
}
