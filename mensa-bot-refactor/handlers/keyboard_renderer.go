package handlers

import (
	"strings"

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

// Field-status markers. Deliberately wordless so they need no translation.
const (
	markerFilled   = "✅"    // value captured
	markerAwaiting = "✏️ …" // this is the field we are waiting on
	markerEmpty    = "⬜ —"  // not filled in yet
)

// ProfileForm renders the registration wizard as ONE self-contained message: a header, the
// current state of every field, and the buttons. The whole flow lives in this single
// message — it is edited in place rather than followed by new ones, so the chat never
// fills with dead prompts and the controls never scroll out of reach.
//
// notice, when set, replaces the header (used for validation errors, so a bad email
// reports itself inside the form instead of spawning another message).
func ProfileForm(d *model.ProfileDraft, lang string, notice string) (string, *models.InlineKeyboardMarkup) {
	return profileFormText(d, lang, notice), profileFormKeyboard(d, lang)
}

func profileFormText(d *model.ProfileDraft, lang string, notice string) string {
	var b strings.Builder

	b.WriteString(profileFormHeader(d, lang, notice))
	b.WriteString("\n\n")

	// Field summary. The labels are the same keys the buttons use, so the button and the
	// row it reports on can never drift apart.
	b.WriteString("✉️ " + tolgee.T("telegrambot.profile.email", lang) + ": ")
	b.WriteString(fieldStatus(d.Email, d.ActiveField == model.FieldEmail))
	b.WriteString("\n")

	b.WriteString("\U0001F3AB " + tolgee.T("telegrambot.profile.membershipnumber", lang) + ": ")
	b.WriteString(fieldStatus(d.MembershipNumber, d.ActiveField == model.FieldMembershipNumber))

	return b.String()
}

// profileFormHeader is what the form says at the top: the error if there is one, else the
// prompt for the field being edited, else the introduction.
func profileFormHeader(d *model.ProfileDraft, lang string, notice string) string {
	if notice != "" {
		return notice
	}
	switch d.ActiveField {
	case model.FieldEmail:
		return tolgee.T("telegrambot.conversation.requestemail", lang)
	case model.FieldMembershipNumber:
		return tolgee.T("telegrambot.conversation.requestmemberid", lang)
	default:
		return tolgee.T("telegrambot.profilecommand.initiateprofileregister", lang)
	}
}

// fieldStatus renders one row's right-hand side: the captured value, a "waiting on you"
// marker, or an empty placeholder.
//
// The value is user-typed and goes into a ParseMode=HTML message, so it must be escaped —
// it does not pass through tolgee.T, which is where every other interpolated value gets
// escaped. Today's validators would reject anything containing HTML, but the escape is what
// makes that a defence rather than a coincidence.
func fieldStatus(value *string, awaiting bool) string {
	if value != nil {
		return markerFilled + " <code>" + tolgee.EscapeHTML(*value) + "</code>"
	}
	if awaiting {
		return markerAwaiting
	}
	return markerEmpty
}

// profileFormKeyboard builds the buttons for the form's current state.
//
// While a field is armed the ONLY button is Cancel: the bot is waiting for the user to
// type, and offering "Submit" or a second field to edit mid-entry is how the old flow got
// into two states at once.
func profileFormKeyboard(d *model.ProfileDraft, lang string) *models.InlineKeyboardMarkup {
	if d.ActiveField != model.FieldNone {
		return &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{{
				{
					Text:         "❌ " + tolgee.T("telegrambot.general.cancel", lang),
					CallbackData: CallbackCancel,
				},
			}},
		}
	}

	rows := [][]models.InlineKeyboardButton{
		{
			{
				Text:         "✉️ " + tolgee.T("telegrambot.profile.email", lang) + checkDone(d.Email),
				CallbackData: CallbackFieldEmail,
			},
			{
				Text:         "\U0001F3AB " + tolgee.T("telegrambot.profile.membershipnumber", lang) + checkDone(d.MembershipNumber),
				CallbackData: CallbackFieldMember,
			},
		},
	}

	// Submit only appears once there is something to submit — a disabled-looking button the
	// user can press and be scolded by is worse than no button.
	submitRow := []models.InlineKeyboardButton{}
	if d.Email != nil && d.MembershipNumber != nil {
		submitRow = append(submitRow, models.InlineKeyboardButton{
			Text:         "✅ " + tolgee.T("telegrambot.profile.submit", lang),
			CallbackData: CallbackSubmit,
		})
	}
	submitRow = append(submitRow, models.InlineKeyboardButton{
		Text:         "❌ " + tolgee.T("telegrambot.general.cancel", lang),
		CallbackData: CallbackCancel,
	})
	rows = append(rows, submitRow)

	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// checkDone returns a checkmark if the value is set
func checkDone(s *string) string {
	if s != nil {
		return " ✔"
	}
	return ""
}
