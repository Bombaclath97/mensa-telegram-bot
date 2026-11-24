package handlers

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/Mensa-Italia/mensa-telegram-bot/api"
	"github.com/Mensa-Italia/mensa-telegram-bot/database"
	"github.com/Mensa-Italia/mensa-telegram-bot/logger"
	"github.com/Mensa-Italia/mensa-telegram-bot/model"
	tb "github.com/Mensa-Italia/mensa-telegram-bot/telegram_bot"
	"github.com/Mensa-Italia/mensa-telegram-bot/tolgee"
	"github.com/Mensa-Italia/mensa-telegram-bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Registration is a single self-editing form message (see keyboard_renderer.go) plus the
// Mensa app's approval:
//
//  1. /profilo on an unregistered user opens the form.
//  2. The Email button arms the field; free text fills it (must be a @mensa.it address AND
//     must not already be registered).
//  3. The Numero di tessera button arms the field; free text fills it (must parse as int).
//  4. Submit calls the Mensa API (POST {API_ENDPOINT}/request) with the email, the
//     membership number and a freshly generated callme URL. HTTP 200 means the pair is a
//     real Mensa member and a push was sent to the official Mensa app. A 4xx means it is
//     not, and NO row is written. Anything else (the API is down) leaves the draft intact
//     so the member can simply press Submit again — see api.ErrMemberNotFound.
//  5. On success the draft is LOCKED with the issued token (AWAITING_APPROVAL). While
//     locked, every free-text message and every keyboard callback from that user is
//     ignored — only the app callback can advance the flow.
//  6. server/callme_handler.go token-matches the app's callback and unlocks the draft.
//  7. ProfileCompleteAfterApproval writes the row, taking the member's NAME from the Mensa
//     API rather than asking for it — a typed name is an unverified name.
//
// The User row therefore never exists before the Mensa app has approved the identity, and
// the name on it is Mensa's, not one the user chose.

// ProfileCommandHandler handles the /profilo command.
// If the user is registered, shows their profile; otherwise it starts the registration flow.
func ProfileCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// Only respond in private chats
	if !IsPrivateChat(update) {
		return
	}

	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	logger.Debug("Processing /profilo command", map[string]interface{}{
		"user_id": userID,
		"chat_id": chatID,
	})

	userRepo := database.NewUserRepository()
	lang := userRepo.GetLanguage(userID)

	// Check if user already has an active draft (registration in progress)
	if model.Drafts.Exists(userID) {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.conversation.alreadyinprogress", lang))
		return
	}

	// Check if user is already registered.
	// A DB failure must NOT read as "not registered" (G21).
	user, err := userRepo.GetByID(userID)
	if err != nil {
		logger.Error("Failed to check user registration", map[string]interface{}{"error": err.Error(), "user_id": userID})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}

	if user != nil {
		// User is registered: show the editable profile card.
		showProfileCard(ctx, chatID, user)
	} else {
		// User not registered, start registration flow
		startRegistration(ctx, chatID, userID, lang,
			update.Message.From.FirstName, update.Message.From.LastName)
	}
}

// startRegistration opens the registration form.
//
// tgFirst/tgLast are the user's Telegram display name. They are NOT what gets registered —
// the real name comes from the Mensa API — but they are kept as a fallback for the case
// where that lookup fails after the app has already approved the member.
func startRegistration(ctx context.Context, chatID, userID int64, lang string, tgFirst, tgLast string) {
	logger.Debug("Starting registration", map[string]interface{}{"user_id": userID})

	draft := &model.ProfileDraft{}
	if tgFirst != "" {
		draft.FirstName = &tgFirst
	}
	if tgLast != "" {
		draft.LastName = &tgLast
	}
	model.Drafts.Set(userID, draft)

	text, keyboard := ProfileForm(draft, lang, "")

	msg, err := tb.SendWithKeyboard(ctx, chatID, text, keyboard)
	if err != nil {
		logger.Error("Failed to send registration message", map[string]interface{}{"error": err.Error(), "user_id": userID})
		model.Drafts.Delete(userID)
		return
	}

	if msg != nil {
		// Commit through the store, not through the shared pointer: once Set stored the
		// draft it belongs to the store's lock.
		model.Drafts.SetMessageID(userID, msg.ID)
	}
}

// ProfileCallbackHandler handles inline keyboard callbacks for profile registration
func ProfileCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// Only respond in private chats
	if !IsPrivateChat(update) {
		return
	}

	q := update.CallbackQuery
	userID := q.From.ID
	chatID := q.Message.Message.Chat.ID

	logger.Debug("Processing profile callback", map[string]interface{}{
		"user_id":       userID,
		"callback_data": q.Data,
	})

	userRepo := database.NewUserRepository()
	lang := userRepo.GetLanguage(userID)

	draft := model.Drafts.Get(userID)
	if draft == nil {
		tb.AnswerCallback(ctx, q.ID, tolgee.T("telegrambot.conversation.sessionexpired", lang))
		return
	}

	// While the draft is AWAITING_APPROVAL the only thing that may advance it is the
	// Mensa app callback. Ignore every button press.
	if draft.Locked {
		tb.AnswerCallback(ctx, q.ID, tolgee.T("telegrambot.conversation.awaitappinteraction", lang))
		return
	}

	// Any interaction keeps the conversation alive.
	model.Drafts.Touch(userID)

	switch q.Data {
	case CallbackFieldEmail:
		profileArmField(ctx, chatID, userID, model.FieldEmail, lang)

	case CallbackFieldMember:
		profileArmField(ctx, chatID, userID, model.FieldMembershipNumber, lang)

	case CallbackSubmit:
		// The Submit button is only rendered once both fields are filled, so an incomplete
		// submit means a stale keyboard. Answer with a toast rather than a chat message —
		// the form is the only thing that should be talking.
		if draft.Email == nil || draft.MembershipNumber == nil {
			tb.AnswerCallback(ctx, q.ID, tolgee.T("telegrambot.conversation.fieldsmissing", lang))
			return
		}
		handleSubmit(ctx, chatID, userID, draft, lang)

	case CallbackCancel:
		model.Drafts.Delete(userID)
		// Delete the keyboard message
		if draft.MessageID > 0 {
			tb.DeleteMessage(ctx, chatID, draft.MessageID)
		}
		tb.Send(ctx, chatID, tolgee.T("telegrambot.cancelcommand.cancelled", lang))
	}

	tb.AnswerCallbackSilent(ctx, q.ID)
}

// handleSubmit verifies the (email, membership number) pair against the Mensa API and,
// if the pair is a real member, parks the draft in AWAITING_APPROVAL until the member
// approves the request in the official Mensa app.
//
// It deliberately does NOT create the User row: the row is only written once the app has
// approved AND the real first/last name have been collected (see profileFinishRegistration).
func handleSubmit(ctx context.Context, chatID, userID int64, draft *model.ProfileDraft, lang string) {
	userRepo := database.NewUserRepository()

	// Validate all fields are filled
	if draft.Email == nil || draft.MembershipNumber == nil {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.conversation.fieldsmissing", lang))
		return
	}

	// Parse membership number
	memberNum, err := strconv.ParseInt(*draft.MembershipNumber, 10, 64)
	if err != nil {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.conversation.notvalidmemberid", lang))
		return
	}

	// Re-check the email is still free (it was checked when it was typed, but another
	// registration may have landed in the meantime).
	emailTaken, err := userRepo.ExistsByEmail(*draft.Email)
	if err != nil {
		logger.Error("Failed to check email uniqueness on submit", map[string]interface{}{
			"error":   err.Error(),
			"user_id": userID,
		})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}
	if emailTaken {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.conversation.emailalreadyregistered", lang))
		return
	}

	// Also make sure this Telegram account has not been registered meanwhile.
	registered, err := userRepo.Exists(userID)
	if err != nil {
		logger.Error("Failed to check user existence on submit", map[string]interface{}{
			"error":   err.Error(),
			"user_id": userID,
		})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}
	if registered {
		model.Drafts.Delete(userID)
		if draft.MessageID > 0 {
			tb.DeleteMessage(ctx, chatID, draft.MessageID)
		}
		tb.Send(ctx, chatID, tolgee.T("telegrambot.conversation.alreadyinprogress", lang))
		return
	}

	// MEMBERSHIP VERIFICATION.
	// POST {API_ENDPOINT}/request with the email, the membership number and a fresh
	// callme URL. A nil error (HTTP 200) means the pair is a real Mensa member and the
	// Mensa app has been pushed an approval request.
	token := utils.GenerateCallmeUrlEndpoint(userID)
	callmeURL := utils.BuildCallmeURL(token)

	client := api.NewMensaAPIClient()
	if err := client.RequestVerification(ctx, *draft.Email, api.MemberNumber(memberNum), callmeURL); err != nil {
		// No row is written on any failure. But WHY it failed decides what the user is told.
		if !errors.Is(err, api.ErrMemberNotFound) {
			// The API was unreachable or broken — we did NOT learn that they are not a
			// member. Saying so would be a lie, and telling them to re-check details that
			// are probably correct is unactionable. Keep the draft and the form so they can
			// just press Submit again once the API is back.
			logger.Error("Mensa API unreachable during verification; keeping the draft", map[string]interface{}{
				"error":   err.Error(),
				"user_id": userID,
			})
			profileRenderForm(ctx, chatID, 0, draft, lang, tolgee.T("telegrambot.general.error", lang))
			return
		}

		// A definitive negative: the pair really is not a member.
		logger.Warn("Mensa membership verification rejected the pair; aborting registration", map[string]interface{}{
			"error":   err.Error(),
			"user_id": userID,
		})

		model.Drafts.Delete(userID)
		if draft.MessageID > 0 {
			tb.DeleteMessage(ctx, chatID, draft.MessageID)
		}
		// This string names the exact email and membership number it could not match, so it
		// MUST be given them — without the params it renders the literal "{email}" and
		// "{member_id}" to the user.
		tb.Send(ctx, chatID, tolgee.T("telegrambot.conversation.associationnonexistent", lang, map[string]string{
			"email":     *draft.Email,
			"member_id": *draft.MembershipNumber,
		}))
		return
	}

	// AWAITING_APPROVAL: lock the draft against the issued token. From here on, only the
	// Mensa app callback (server/callme_handler.go) can move the flow forward.
	model.Drafts.SetActiveField(userID, model.FieldNone)
	if !model.Drafts.LockUser(userID, token) {
		// The draft expired between Get and LockUser.
		logger.Warn("Draft vanished before it could be locked", map[string]interface{}{"user_id": userID})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.conversation.sessionexpired", lang))
		return
	}

	logger.Info("Membership verification requested, awaiting Mensa app approval", map[string]interface{}{
		"user_id":       userID,
		"member_number": memberNum,
	})

	// Retire the keyboard: no button may be pressed while we wait for the app.
	if draft.MessageID > 0 {
		tb.DeleteMessage(ctx, chatID, draft.MessageID)
		model.Drafts.SetMessageID(userID, 0)
	}

	tb.Send(ctx, chatID, tolgee.T("telegrambot.conversation.awaitappinteraction", lang))
}

// ProfileCompleteAfterApproval finishes registration once the Mensa app has approved.
//
// It is the ONLY supported way for the callme webhook (server/callme_handler.go) to hand
// the conversation back to the bot after model.Drafts.UnlockUser has token-matched the
// callback. Returns false if there is no live, unlocked, approved draft.
//
// The member's NAME comes from the Mensa API, not from the user. Asking them to type it
// meant storing an unverified string: nothing stopped a real member registering under
// someone else's name. The membership number is already proven at this point, so the API
// is both authoritative and available — and it makes the flow end the instant the member
// approves in the app, instead of two more prompts later.
//
// A Mensa API failure here does NOT abort: the app callback has already proven the
// identity, so the row is written with the member's Telegram display name as a placeholder
// and the real name is picked up by the profile card's Sync button. Refusing to register a
// verified member because a cosmetic lookup failed would be the wrong trade.
func ProfileCompleteAfterApproval(ctx context.Context, userID int64) bool {
	draft := model.Drafts.Get(userID)
	if draft == nil || draft.Locked {
		return false
	}
	// A row must never be written without proof that the Mensa app approved this member.
	if draft.CallmeToken == "" || draft.Email == nil || draft.MembershipNumber == nil {
		logger.Error("Refusing to complete an unverified registration", map[string]interface{}{"user_id": userID})
		return false
	}

	userRepo := database.NewUserRepository()
	lang := userRepo.GetLanguage(userID)

	memberNum, err := strconv.ParseInt(*draft.MembershipNumber, 10, 64)
	if err != nil {
		logger.Error("Membership number is not numeric at registration time", map[string]interface{}{
			"error": err.Error(), "user_id": userID,
		})
		tb.Send(ctx, userID, tolgee.T("telegrambot.general.error", lang))
		model.Drafts.Delete(userID)
		return false
	}

	// Authoritative name, straight from Mensa.
	first, last := "", ""
	if result, err := api.NewMensaAPIClient().CheckMembershipValidity(ctx, api.MemberNumber(memberNum)); err != nil {
		logger.Warn("Could not read the member's name from the Mensa API; falling back to the Telegram name",
			map[string]interface{}{"error": err.Error(), "user_id": userID})
	} else {
		first, last = SplitName(result.Name)
	}

	// Fallback: the Telegram display name captured when the wizard opened.
	if first == "" && draft.FirstName != nil {
		first = *draft.FirstName
		if draft.LastName != nil {
			last = *draft.LastName
		}
	}

	user := &database.User{
		TelegramID:   userID,
		MensaEmail:   *draft.Email,
		MemberNumber: memberNum,
		FirstName:    first,
		LastName:     last,
		Language:     lang,
	}
	if err := userRepo.Create(user); err != nil {
		logger.Error("Failed to create the user row after approval", map[string]interface{}{
			"error": err.Error(), "user_id": userID,
		})
		tb.Send(ctx, userID, tolgee.T("telegrambot.general.error", lang))
		return false
	}

	model.Drafts.Delete(userID)
	logger.Info("User registered", map[string]interface{}{
		"user_id": userID, "member_number": memberNum,
	})

	tb.Send(ctx, userID, tolgee.T("telegrambot.conversation.creationsuccess", lang))
	CompleteRegistrationSideEffects(ctx, userID, lang)
	showProfileCard(ctx, userID, user)
	return true
}

// CompleteRegistrationSideEffects runs everything that must happen the moment a member's row
// exists, regardless of HOW they registered (the app round-trip, or an admin's /manual_add).
//
// Both are best-effort and deliberately non-fatal: the member IS registered by the time this
// runs, and failing them here would be strictly worse than the degraded state each fallback
// leaves behind.
//
//  1. Drain the join requests buffered while they were unregistered. Without this the member
//     was told "register first", registered, and then sat there — the buffer only drained if
//     they discovered /approva on their own. This is the seam that was missing.
//  2. Probe every known group and write their group_associations. Nothing else can do this:
//     an existing member already inside a group never issues a join request, so this is the
//     only moment the bot can learn where they are. Without it the cleanup sweep would later
//     delete their row while kicking them from nothing.
//
// The order matters. Draining first approves the pending requests, and each approval already
// writes its own association; the sync then picks up every OTHER group they were already in.
func CompleteRegistrationSideEffects(ctx context.Context, userID int64, lang string) {
	if approved, _ := DrainPendingJoinRequests(ctx, userID, lang); approved > 0 {
		logger.Info("Drained buffered join requests on registration", map[string]interface{}{
			"user_id":  userID,
			"approved": approved,
		})
	}

	if err := SyncMemberGroups(ctx, userID); err != nil {
		// groups_synced_at stays NULL, so the cleanup sweep will defer this member rather
		// than delete them. That is the safe direction, and the next sync can fix it.
		logger.Warn("Could not sync the member's groups at registration", map[string]interface{}{
			"user_id": userID,
			"error":   err.Error(),
		})
	}
}

// ProfileMessageHandler handles text messages during profile registration
// (when the user is filling in a field).
// Returns true if the message was consumed by the registration flow, false otherwise.
func ProfileMessageHandler(ctx context.Context, b *bot.Bot, update *models.Update) bool {
	if update.Message == nil {
		return false
	}

	// Only respond in private chats
	if update.Message.Chat.Type != "private" {
		return false
	}

	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	draft := model.Drafts.Get(userID)
	if draft == nil {
		return false // No registration in progress
	}

	// AWAITING_APPROVAL: swallow free text while the Mensa app approval is pending,
	// exactly as the legacy did (state > -1 && !lockedUsers.IsUserLocked(...)).
	if draft.Locked {
		logger.Debug("Ignoring message from locked user (awaiting Mensa app approval)", map[string]interface{}{
			"user_id": userID,
		})
		return true
	}

	if draft.ActiveField == model.FieldNone {
		return false // Nothing being edited: let other handlers see the message
	}

	userRepo := database.NewUserRepository()
	lang := userRepo.GetLanguage(userID)
	text := strings.TrimSpace(update.Message.Text)

	// Keep the conversation alive on every step.
	model.Drafts.Touch(userID)

	switch draft.ActiveField {
	case model.FieldEmail:
		// Every failure below repaints the form with the error as its header and leaves the
		// field armed, so the user just types again. No new chat messages, ever.
		if !utils.IsValidMensaEmail(text) {
			profileRenderForm(ctx, chatID, update.Message.ID, draft, lang,
				tolgee.T("telegrambot.conversation.emailnotvalid", lang))
			return true
		}

		email := strings.ToLower(text)

		// Check if email already exists. A DB error is NOT "free to use".
		exists, err := userRepo.ExistsByEmail(email)
		if err != nil {
			logger.Error("Failed to check email", map[string]interface{}{"error": err.Error(), "user_id": userID})
			profileRenderForm(ctx, chatID, update.Message.ID, draft, lang,
				tolgee.T("telegrambot.general.error", lang))
			return true
		}

		if exists {
			profileRenderForm(ctx, chatID, update.Message.ID, draft, lang,
				tolgee.T("telegrambot.conversation.emailalreadyregistered", lang))
			return true
		}

		updated := model.Drafts.Update(userID, func(d *model.ProfileDraft) {
			d.Email = &email
			d.ActiveField = model.FieldNone
		})
		if updated == nil {
			return true // draft expired mid-edit
		}
		profileRenderForm(ctx, chatID, update.Message.ID, updated, lang, "")
		return true

	case model.FieldMembershipNumber:
		if _, err := strconv.ParseInt(text, 10, 64); err != nil {
			profileRenderForm(ctx, chatID, update.Message.ID, draft, lang,
				tolgee.T("telegrambot.conversation.notvalidmemberid", lang))
			return true
		}
		memberNumber := text
		updated := model.Drafts.Update(userID, func(d *model.ProfileDraft) {
			d.MembershipNumber = &memberNumber
			d.ActiveField = model.FieldNone
		})
		if updated == nil {
			return true // draft expired mid-edit
		}
		profileRenderForm(ctx, chatID, update.Message.ID, updated, lang, "")
		return true

	}

	// FieldFirstName / FieldLastName no longer exist as conversation steps: the name comes
	// from the Mensa API once the app approves (see ProfileCompleteAfterApproval).
	return true
}

// profileRenderForm repaints the wizard's single message in place.
//
// This is the whole point of the form: every state change — a field armed, a value
// accepted, a validation error — is an EDIT of the one message, never a new one. The chat
// therefore holds exactly one bot message for the entire registration, and the buttons are
// always the last thing on screen instead of scrolled away above a pile of dead prompts.
//
// inputMessageID, when > 0, is the user's own message; it is deleted so the typed email and
// membership number do not linger in the chat either.
// notice, when set, is shown as the form's header (validation errors report themselves
// inside the form).
func profileRenderForm(ctx context.Context, chatID int64, inputMessageID int, draft *model.ProfileDraft, lang, notice string) {
	if inputMessageID > 0 {
		tb.DeleteMessage(ctx, chatID, inputMessageID)
	}

	if draft.MessageID <= 0 {
		return
	}

	text, keyboard := ProfileForm(draft, lang, notice)
	if _, err := tb.EditMessageWithKeyboard(ctx, chatID, draft.MessageID, text, keyboard); err != nil {
		// "message is not modified" is expected when a repaint is a no-op; it is not worth a warning.
		logger.Debug("Could not repaint the profile form", map[string]interface{}{
			"error":   err.Error(),
			"chat_id": chatID,
		})
	}
}

// profileArmField points the form at a field and waits for the user to type it.
func profileArmField(ctx context.Context, chatID, userID int64, field model.Field, lang string) {
	updated := model.Drafts.Update(userID, func(d *model.ProfileDraft) {
		d.ActiveField = field
	})
	if updated == nil {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.conversation.sessionexpired", lang))
		return
	}
	profileRenderForm(ctx, chatID, 0, updated, lang, "")
}
