package handlers

import (
	"context"
	"strings"

	"github.com/Mensa-Italia/mensa-telegram-bot/database"
	"github.com/Mensa-Italia/mensa-telegram-bot/logger"
	tb "github.com/Mensa-Italia/mensa-telegram-bot/telegram_bot"
	"github.com/Mensa-Italia/mensa-telegram-bot/tolgee"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Language represents a selectable language with its flag.
type Language struct {
	Code string
	Flag string
	Name string
}

// languageDisplay maps a Tolgee language tag to how it is rendered in the picker.
// Names are in the language itself, which is what a speaker scanning the menu looks for.
// A tag Tolgee serves but that is missing here still appears, labelled with its raw tag —
// so adding a language in Tolgee surfaces it in the bot without a code change.
var languageDisplay = map[string]Language{
	"it": {Code: "it", Flag: "\U0001F1EE\U0001F1F9", Name: "Italiano"},
	"en": {Code: "en", Flag: "\U0001F1EC\U0001F1E7", Name: "English"},
	"es": {Code: "es", Flag: "\U0001F1EA\U0001F1F8", Name: "Español"},
	"de": {Code: "de", Flag: "\U0001F1E9\U0001F1EA", Name: "Deutsch"},
	"fr": {Code: "fr", Flag: "\U0001F1EB\U0001F1F7", Name: "Français"},
	"pt": {Code: "pt", Flag: "\U0001F1F5\U0001F1F9", Name: "Português"},
	"ja": {Code: "ja", Flag: "\U0001F1EF\U0001F1F5", Name: "日本語"},
	"zh": {Code: "zh", Flag: "\U0001F1E8\U0001F1F3", Name: "中文"},
	"hi": {Code: "hi", Flag: "\U0001F1EE\U0001F1F3", Name: "हिन्दी"},
	"bn": {Code: "bn", Flag: "\U0001F1E7\U0001F1E9", Name: "বাংলা"},
}

// hiddenFromPicker are tags Tolgee serves that must not be offered as a choice.
// zh-Hans is Simplified Chinese, which "zh" already covers in this project — showing both
// gives the user two buttons for the same language. It stays a valid stored value, so a
// user who somehow already has it keeps working; it is only kept out of the menu.
var hiddenFromPicker = map[string]bool{
	"zh-Hans": true,
}

// availableLanguages is the picker's contents: every language Tolgee actually loaded,
// minus the hidden ones. Driving this off Tolgee (rather than a hardcoded list) is the
// point — the project serves 11 languages and the old hardcoded four made 7 of them
// unreachable. Falls back to the base language if Tolgee loaded nothing, so /lang can
// never render an empty keyboard.
func availableLanguages() []Language {
	return languagesFromTags(tolgee.Languages(), tolgee.BaseLanguage())
}

// languagesFromTags is the pure core of availableLanguages, split out so it can be tested
// without a loaded Tolgee.
func languagesFromTags(tags []string, base string) []Language {
	langs := make([]Language, 0, len(tags))
	for _, tag := range tags {
		if hiddenFromPicker[tag] {
			continue
		}
		if l, ok := languageDisplay[tag]; ok {
			langs = append(langs, l)
			continue
		}
		// Unknown to the display registry: still offer it, labelled with its tag.
		langs = append(langs, Language{Code: tag, Flag: "\U0001F310", Name: tag})
	}

	if len(langs) == 0 {
		if l, ok := languageDisplay[base]; ok {
			return []Language{l}
		}
		return []Language{{Code: base, Flag: "\U0001F310", Name: base}}
	}
	return langs
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

// buildLanguageKeyboard creates the inline keyboard with language options, 2 per row.
func buildLanguageKeyboard() *models.InlineKeyboardMarkup {
	langs := availableLanguages()

	var rows [][]models.InlineKeyboardButton
	for i := 0; i < len(langs); i += 2 {
		var row []models.InlineKeyboardButton

		row = append(row, models.InlineKeyboardButton{
			Text:         langs[i].Flag + " " + langs[i].Name,
			CallbackData: "lang:" + langs[i].Code,
		})

		if i+1 < len(langs) {
			row = append(row, models.InlineKeyboardButton{
				Text:         langs[i+1].Flag + " " + langs[i+1].Name,
				CallbackData: "lang:" + langs[i+1].Code,
			})
		}

		rows = append(rows, row)
	}

	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// isValidLanguage reports whether code is a language the user is actually allowed to pick.
// This is the authorization boundary for the callback: the code arrives from user-controlled
// callback data and is written straight to the DB, so anything not in the picker is refused.
func isValidLanguage(code string) bool {
	for _, lang := range availableLanguages() {
		if lang.Code == code {
			return true
		}
	}
	return false
}

// getLanguageName returns the display label for a language code, for the confirmation message.
func getLanguageName(code string) string {
	for _, lang := range availableLanguages() {
		if lang.Code == code {
			return lang.Flag + " " + lang.Name
		}
	}
	return code
}
