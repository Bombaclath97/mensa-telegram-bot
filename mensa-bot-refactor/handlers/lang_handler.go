package handlers

import (
	"context"
	"strings"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
	"github.com/Bombaclath97/mensa-telegram-bot/tolgee"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Language represents a supported language with its flag
type Language struct {
	Code string
	Flag string
	Name string
}

// SupportedLanguages lists all available languages
var SupportedLanguages = []Language{
	{Code: "it", Flag: "\U0001F1EE\U0001F1F9", Name: "Italiano"},
	{Code: "en", Flag: "\U0001F1EC\U0001F1E7", Name: "English"},
	{Code: "es", Flag: "\U0001F1EA\U0001F1F8", Name: "Español"},
	{Code: "de", Flag: "\U0001F1E9\U0001F1EA", Name: "Deutsch"},
}

// LangCommandHandler handles the /lang command
func LangCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// Only respond in private chats
	if !IsPrivateChat(update) {
		return
	}

	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	logger.Debug("Processing /lang command", map[string]interface{}{
		"user_id": userID,
		"chat_id": chatID,
	})

	userRepo := database.NewUserRepository()
	lang := userRepo.GetLanguage(userID)

	keyboard := buildLanguageKeyboard()
	tb.SendWithKeyboard(ctx, chatID, tolgee.T("telegrambot.lang.prompt", lang), keyboard)
}

// LangCallbackHandler handles language selection callbacks
func LangCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// Only respond in private chats
	if !IsPrivateChat(update) {
		return
	}

	q := update.CallbackQuery
	userID := q.From.ID
	chatID := q.Message.Message.Chat.ID
	messageID := q.Message.Message.ID

	// Extract language code from callback data (format: "lang:xx")
	parts := strings.Split(q.Data, ":")
	if len(parts) != 2 {
		tb.AnswerCallbackSilent(ctx, q.ID)
		return
	}

	selectedLang := parts[1]

	// Validate language code
	if !isValidLanguage(selectedLang) {
		tb.AnswerCallbackSilent(ctx, q.ID)
		return
	}

	logger.Debug("Processing language change", map[string]interface{}{
		"user_id":  userID,
		"language": selectedLang,
	})

	// Update user's language preference
	userRepo := database.NewUserRepository()
	err := userRepo.UpdateLanguage(userID, selectedLang)
	if err != nil {
		logger.Error("Failed to update language", map[string]interface{}{"error": err.Error(), "user_id": userID})
		tb.AnswerCallback(ctx, q.ID, tolgee.T("telegrambot.general.error", selectedLang))
		return
	}

	logger.Info("User language updated", map[string]interface{}{"user_id": userID, "language": selectedLang})

	// Delete the keyboard message
	tb.DeleteMessage(ctx, chatID, messageID)

	// Send confirmation in the new language
	params := map[string]string{
		"language": getLanguageName(selectedLang),
	}
	tb.Send(ctx, chatID, tolgee.T("telegrambot.lang.changed", selectedLang, params))

	tb.AnswerCallbackSilent(ctx, q.ID)
}

// buildLanguageKeyboard creates the inline keyboard with language options
func buildLanguageKeyboard() *models.InlineKeyboardMarkup {
	var rows [][]models.InlineKeyboardButton

	// Create a row for each pair of languages (2 per row)
	for i := 0; i < len(SupportedLanguages); i += 2 {
		var row []models.InlineKeyboardButton

		// First language in row
		lang := SupportedLanguages[i]
		row = append(row, models.InlineKeyboardButton{
			Text:         lang.Flag + " " + lang.Name,
			CallbackData: "lang:" + lang.Code,
		})

		// Second language in row (if exists)
		if i+1 < len(SupportedLanguages) {
			lang2 := SupportedLanguages[i+1]
			row = append(row, models.InlineKeyboardButton{
				Text:         lang2.Flag + " " + lang2.Name,
				CallbackData: "lang:" + lang2.Code,
			})
		}

		rows = append(rows, row)
	}

	return &models.InlineKeyboardMarkup{
		InlineKeyboard: rows,
	}
}

// isValidLanguage checks if a language code is supported
func isValidLanguage(code string) bool {
	for _, lang := range SupportedLanguages {
		if lang.Code == code {
			return true
		}
	}
	return false
}

// getLanguageName returns the display name for a language code
func getLanguageName(code string) string {
	for _, lang := range SupportedLanguages {
		if lang.Code == code {
			return lang.Flag + " " + lang.Name
		}
	}
	return code
}
