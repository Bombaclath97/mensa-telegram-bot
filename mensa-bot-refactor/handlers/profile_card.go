package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/Bombaclath97/mensa-telegram-bot/api"
	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
	"github.com/Bombaclath97/mensa-telegram-bot/tolgee"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// The profile card is the registered user's counterpart to the registration form: ONE
// message, edited in place. Tapping a button repaints the same card rather than pushing a
// new message, so /profilo leaves exactly one thing in the chat no matter how much you
// poke at it.
const (
	CallbackCardLanguage = "pcard:lang"    // open the language picker inside the card
	CallbackCardSetLang  = "pcard:setlang" // pcard:setlang:<code>
	CallbackCardSync     = "pcard:sync"    // re-read name + membership from the Mensa API
	CallbackCardDelete   = "pcard:delete"  // arm the delete confirmation
	CallbackCardConfirm  = "pcard:delete:confirm"
	CallbackCardBack     = "pcard:back" // back to the card from any sub-view
)

// cardView is which face of the card is showing.
type cardView int

const (
	viewProfile cardView = iota
	viewLanguage
	viewDeleteConfirm
)

// ProfileCard renders the card for a registered member.
//
// notice, when set, is shown under the title (e.g. "synced" / "sync failed"), so feedback
// lands inside the card instead of as another message.
func ProfileCard(user *database.User, groups []database.GroupAssociation, groupTitles map[int64]string,
	lang string, view cardView, notice string) (string, *models.InlineKeyboardMarkup) {

	var b strings.Builder
	b.WriteString("<b>" + tolgee.T("telegrambot.profilecard.title", lang) + "</b>\n")

	if notice != "" {
		b.WriteString("\n" + notice + "\n")
	}

	b.WriteString("\n")
	b.WriteString(cardRow(tolgee.T("telegrambot.profilecard.name", lang),
		strings.TrimSpace(user.FirstName+" "+user.LastName)))
	b.WriteString(cardRow(tolgee.T("telegrambot.profile.email", lang), user.MensaEmail))
	b.WriteString(cardRow(tolgee.T("telegrambot.profile.membershipnumber", lang),
		fmt.Sprintf("%d", user.MemberNumber)))
	b.WriteString(cardRow(tolgee.T("telegrambot.profilecard.language", lang),
		getLanguageName(user.GetLanguage())))

	// Groups
	b.WriteString("\n<b>" + tolgee.T("telegrambot.profilecard.groups", lang) + "</b>\n")
	if len(groups) == 0 {
		b.WriteString("<i>" + tolgee.T("telegrambot.profilecard.nogroups", lang) + "</i>\n")
	}
	for _, assoc := range groups {
		title, ok := groupTitles[assoc.GroupID]
		if !ok {
			continue // could not resolve the chat; skip rather than print a bare ID
		}
		line := "• " + tolgee.EscapeHTML(title)
		if assoc.IsGroupAdmin {
			line += " " + tolgee.T("telegrambot.profilecommand.isadmin", lang)
		}
		b.WriteString(line + "\n")
	}

	switch view {
	case viewLanguage:
		return b.String(), cardLanguageKeyboard(lang)
	case viewDeleteConfirm:
		// Reuse the /elimina copy: the consequences are identical, and one warning that is
		// maintained beats two that drift apart.
		text := b.String() + "\n" + tolgee.T("telegrambot.deletecommand.deleteprofile", lang,
			map[string]string{"name": user.FirstName})
		return text, cardDeleteKeyboard(lang)
	default:
		return b.String(), cardKeyboard(lang)
	}
}

func cardRow(label, value string) string {
	return fmt.Sprintf("%s: <code>%s</code>\n", label, tolgee.EscapeHTML(value))
}

func cardKeyboard(lang string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "🌍 " + tolgee.T("telegrambot.profilecard.changelanguage", lang), CallbackData: CallbackCardLanguage},
				{Text: "🔄 " + tolgee.T("telegrambot.profilecard.sync", lang), CallbackData: CallbackCardSync},
			},
			{
				{Text: "🗑 " + tolgee.T("telegrambot.profilecard.delete", lang), CallbackData: CallbackCardDelete},
			},
		},
	}
}

func cardLanguageKeyboard(lang string) *models.InlineKeyboardMarkup {
	langs := availableLanguages()

	var rows [][]models.InlineKeyboardButton
	for i := 0; i < len(langs); i += 2 {
		row := []models.InlineKeyboardButton{{
			Text:         langs[i].Flag + " " + langs[i].Name,
			CallbackData: CallbackCardSetLang + ":" + langs[i].Code,
		}}
		if i+1 < len(langs) {
			row = append(row, models.InlineKeyboardButton{
				Text:         langs[i+1].Flag + " " + langs[i+1].Name,
				CallbackData: CallbackCardSetLang + ":" + langs[i+1].Code,
			})
		}
		rows = append(rows, row)
	}
	rows = append(rows, []models.InlineKeyboardButton{
		{Text: "⬅️ " + tolgee.T("telegrambot.general.cancel", lang), CallbackData: CallbackCardBack},
	})

	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func cardDeleteKeyboard(lang string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{{
			{Text: "🚨 " + tolgee.T("telegrambot.deletecommand.confirmbutton", lang), CallbackData: CallbackCardConfirm},
			{Text: "❌ " + tolgee.T("telegrambot.general.cancel", lang), CallbackData: CallbackCardBack},
		}},
	}
}

// showProfileCard sends the card as a NEW message (the entry point, /profilo).
func showProfileCard(ctx context.Context, chatID int64, user *database.User) {
	lang := user.GetLanguage()
	groups, titles := cardGroups(ctx, user.TelegramID)

	text, keyboard := ProfileCard(user, groups, titles, lang, viewProfile, "")
	if _, err := tb.SendWithKeyboard(ctx, chatID, text, keyboard); err != nil {
		logger.Error("Failed to send the profile card", map[string]interface{}{
			"error":   err.Error(),
			"user_id": user.TelegramID,
		})
	}
}

// repaintCard edits the existing card in place.
func repaintCard(ctx context.Context, chatID int64, messageID int, user *database.User, lang string, view cardView, notice string) {
	groups, titles := cardGroups(ctx, user.TelegramID)
	text, keyboard := ProfileCard(user, groups, titles, lang, view, notice)

	if _, err := tb.EditMessageWithKeyboard(ctx, chatID, messageID, text, keyboard); err != nil {
		logger.Debug("Could not repaint the profile card", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// cardGroups loads the member's groups and resolves each group's title.
func cardGroups(ctx context.Context, userID int64) ([]database.GroupAssociation, map[int64]string) {
	groupAssocRepo := database.NewGroupAssociationRepository()
	groups, err := groupAssocRepo.GetUserGroups(userID)
	if err != nil {
		logger.Error("Failed to load groups for the profile card", map[string]interface{}{
			"error":   err.Error(),
			"user_id": userID,
		})
		return nil, nil
	}

	titles := make(map[int64]string, len(groups))
	for _, assoc := range groups {
		chat, err := tb.GetChat(ctx, assoc.GroupID)
		if err != nil {
			logger.Warn("Failed to resolve group title", map[string]interface{}{
				"error":    err.Error(),
				"group_id": assoc.GroupID,
			})
			continue
		}
		titles[assoc.GroupID] = chat.Title
	}
	return groups, titles
}

// ProfileCardCallbackHandler serves every "pcard:" button.
func ProfileCardCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !IsPrivateChat(update) {
		return
	}

	q := update.CallbackQuery
	userID := q.From.ID
	chatID := q.Message.Message.Chat.ID
	messageID := q.Message.Message.ID

	userRepo := database.NewUserRepository()

	user, err := userRepo.GetByID(userID)
	if err != nil {
		logger.Error("Profile card: failed to load the user", map[string]interface{}{
			"error": err.Error(), "user_id": userID,
		})
		tb.AnswerCallback(ctx, q.ID, tolgee.T("telegrambot.general.error", userRepo.GetLanguage(userID)))
		return
	}
	if user == nil {
		// Deleted (possibly from another device) while the card was still on screen.
		tb.AnswerCallback(ctx, q.ID, tolgee.T("telegrambot.startcommand.notregistered", userRepo.GetLanguage(userID)))
		tb.DeleteMessage(ctx, chatID, messageID)
		return
	}
	lang := user.GetLanguage()

	data := q.Data

	switch {
	case data == CallbackCardLanguage:
		repaintCard(ctx, chatID, messageID, user, lang, viewLanguage, "")

	case strings.HasPrefix(data, CallbackCardSetLang+":"):
		selected := strings.TrimPrefix(data, CallbackCardSetLang+":")
		// The code arrives from user-controlled callback data and is written to the DB:
		// only a language actually offered in the picker may be accepted.
		if !isValidLanguage(selected) {
			tb.AnswerCallbackSilent(ctx, q.ID)
			return
		}
		if err := userRepo.UpdateLanguage(userID, selected); err != nil {
			logger.Error("Profile card: failed to change language", map[string]interface{}{
				"error": err.Error(), "user_id": userID,
			})
			tb.AnswerCallback(ctx, q.ID, tolgee.T("telegrambot.general.error", lang))
			return
		}
		user.Language = selected
		// Repaint in the NEW language, so the whole card switches at once.
		repaintCard(ctx, chatID, messageID, user, selected, viewProfile,
			tolgee.T("telegrambot.lang.changed", selected, map[string]string{
				"language": getLanguageName(selected),
			}))

	case data == CallbackCardSync:
		notice := syncFromMensa(ctx, user)
		repaintCard(ctx, chatID, messageID, user, lang, viewProfile, notice)

	case data == CallbackCardDelete:
		repaintCard(ctx, chatID, messageID, user, lang, viewDeleteConfirm, "")

	case data == CallbackCardConfirm:
		res, err := DeleteMemberEverywhere(ctx, userID, lang)
		if err != nil {
			tb.AnswerCallback(ctx, q.ID, tolgee.T("telegrambot.general.error", lang))
			return
		}
		tb.DeleteMessage(ctx, chatID, messageID)
		tb.Send(ctx, chatID, deletedMessage(res, lang))

	case data == CallbackCardBack:
		repaintCard(ctx, chatID, messageID, user, lang, viewProfile, "")
	}

	tb.AnswerCallbackSilent(ctx, q.ID)
}

// syncFromMensa re-reads the member's name from the Mensa API and persists it, returning
// the notice to show on the card.
//
// The name is Mensa's to own, not the user's to type: sourcing it from the API is what
// makes it trustworthy. This is also how a member picks up a name change (marriage, a
// correction) without anyone editing the database.
func syncFromMensa(ctx context.Context, user *database.User) string {
	lang := user.GetLanguage()

	result, err := api.NewMensaAPIClient().CheckMembershipValidity(ctx, api.MemberNumber(user.MemberNumber))
	if err != nil {
		logger.Warn("Profile card: Mensa sync failed", map[string]interface{}{
			"error": err.Error(), "user_id": user.TelegramID,
		})
		return tolgee.T("telegrambot.profilecard.syncfailed", lang)
	}

	first, last := SplitName(result.Name)
	if first == "" {
		return tolgee.T("telegrambot.profilecard.syncfailed", lang)
	}

	if err := database.NewUserRepository().UpdateName(user.TelegramID, first, last); err != nil {
		logger.Error("Profile card: failed to persist the synced name", map[string]interface{}{
			"error": err.Error(), "user_id": user.TelegramID,
		})
		return tolgee.T("telegrambot.general.error", lang)
	}

	user.FirstName, user.LastName = first, last
	logger.Info("Profile synced from the Mensa API", map[string]interface{}{"user_id": user.TelegramID})
	return tolgee.T("telegrambot.profilecard.synced", lang)
}

// SplitName turns the Mensa API's single "name" field into first + last.
// "Mario Rossi" -> ("Mario", "Rossi"); "Anna Maria De Luca" -> ("Anna", "Maria De Luca").
// A single token becomes the first name with an empty surname rather than being dropped.
func SplitName(full string) (first, last string) {
	fields := strings.Fields(strings.TrimSpace(full))
	switch len(fields) {
	case 0:
		return "", ""
	case 1:
		return fields[0], ""
	default:
		return fields[0], strings.Join(fields[1:], " ")
	}
}
