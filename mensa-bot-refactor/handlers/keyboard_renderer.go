package handlers

import (
	"github.com/Bombaclath97/mensa-telegram-bot/model"
	"github.com/Bombaclath97/mensa-telegram-bot/tolgee"
	"github.com/go-telegram/bot/models"
)

// Callback data constants for profile keyboard
const (
	CallbackFieldEmail  = "profile:field:email"
	CallbackFieldMember = "profile:field:memnum"
	CallbackSubmit      = "profile:submit"
	CallbackCancel      = "profile:cancel"
)

// ProfileKeyboard generates an inline keyboard for the profile creation wizard
func ProfileKeyboard(d *model.ProfileDraft, lang string) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{
					Text:         "\u2709 " + tolgee.T("telegrambot.profile.email", lang) + checkDone(d.Email),
					CallbackData: CallbackFieldEmail,
				},
				{
					Text:         "\U0001F3AB " + tolgee.T("telegrambot.profile.membershipnumber", lang) + checkDone(d.MembershipNumber),
					CallbackData: CallbackFieldMember,
				},
			},
			{
				{
					Text:         "\u2705 " + tolgee.T("telegrambot.profile.submit", lang),
					CallbackData: CallbackSubmit,
				},
				{
					Text:         "\u274C " + tolgee.T("telegrambot.general.cancel", lang),
					CallbackData: CallbackCancel,
				},
			},
		},
	}
}

// checkDone returns a checkmark if the value is set
func checkDone(s *string) string {
	if s != nil {
		return " (\u2714)"
	}
	return ""
}
