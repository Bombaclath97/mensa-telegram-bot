package handlers

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	"github.com/Bombaclath97/mensa-telegram-bot/model"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
	"github.com/Bombaclath97/mensa-telegram-bot/tolgee"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// ProfileCommandHandler handles the /profilo command
// If user is registered, shows their profile
// If not registered, initiates the registration flow with inline keyboard
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

	// Check if user is already registered
	user, err := userRepo.GetByID(userID)
	if err != nil {
		logger.Error("Failed to check user registration", map[string]interface{}{"error": err.Error(), "user_id": userID})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}

	if user != nil {
		// User is registered, show their profile
		showProfile(ctx, chatID, user)
	} else {
		// User not registered, start registration flow
		startRegistration(ctx, chatID, userID, lang)
	}
}

// showProfile displays the user's profile information
func showProfile(ctx context.Context, chatID int64, user *database.User) {
	lang := user.GetLanguage()

	params := map[string]string{
		"name":    user.FirstName,
		"surname": user.LastName,
		"email":   user.MensaEmail,
	}

	tb.Send(ctx, chatID, tolgee.T("telegrambot.profilecommand.showprofile", lang, params))

	// Show groups if any
	groupAssocRepo := database.NewGroupAssociationRepository()
	groups, err := groupAssocRepo.GetUserGroups(user.TelegramID)
	if err != nil {
		logger.Error("Failed to get user groups", map[string]interface{}{"error": err.Error(), "user_id": user.TelegramID})
		return
	}

	if len(groups) > 0 {
		var groupLines []string
		for _, assoc := range groups {
			chat, err := tb.GetChat(ctx, assoc.GroupID)
			if err != nil {
				continue
			}
			line := fmt.Sprintf("- %s", chat.Title)
			if assoc.IsGroupAdmin {
				line += " " + tolgee.T("telegrambot.profilecommand.isadmin", lang)
			}
			groupLines = append(groupLines, line)
		}

		if len(groupLines) > 0 {
			params := map[string]string{
				"groupList": strings.Join(groupLines, "\n"),
			}
			tb.Send(ctx, chatID, tolgee.T("telegrambot.profilecommand.showgroups", lang, params))
		}
	}
}

// startRegistration initiates the profile creation flow with inline keyboard
func startRegistration(ctx context.Context, chatID, userID int64, lang string) {
	logger.Debug("Starting registration", map[string]interface{}{"user_id": userID})

	draft := &model.ProfileDraft{}
	model.Drafts.Set(userID, draft)

	text := tolgee.T("telegrambot.profilecommand.initiateprofileregister", lang)
	keyboard := ProfileKeyboard(draft, lang)

	msg, err := tb.SendWithKeyboard(ctx, chatID, text, keyboard)
	if err != nil {
		logger.Error("Failed to send registration message", map[string]interface{}{"error": err.Error(), "user_id": userID})
		return
	}

	if msg != nil {
		draft.MessageID = msg.ID
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

	switch q.Data {
	case CallbackFieldEmail:
		draft.ActiveField = model.FieldEmail
		tb.Send(ctx, chatID, tolgee.T("telegrambot.conversation.requestemail", lang))

	case CallbackFieldMember:
		draft.ActiveField = model.FieldMembershipNumber
		tb.Send(ctx, chatID, tolgee.T("telegrambot.conversation.requestmemberid", lang))

	case CallbackSubmit:
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

// handleSubmit processes the profile submission
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

	// TODO: Here you would typically:
	// 1. Verify membership with Mensa API (CheckIfIsMemberAndSendCallmeURL)
	// 2. Wait for app approval callback
	// 3. Then ask for first/last name
	// For now, we'll create the user directly (simplified flow)

	// Create user in database with default language
	user := &database.User{
		TelegramID:   userID,
		MensaEmail:   *draft.Email,
		MemberNumber: memberNum,
		FirstName:    "Da completare", // These would come from app verification
		LastName:     "Da completare",
		Language:     lang,
	}

	err = userRepo.Create(user)
	if err != nil {
		logger.Error("Failed to create user", map[string]interface{}{"error": err.Error(), "user_id": userID})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}

	logger.Info("User created successfully", map[string]interface{}{"user_id": userID, "email": *draft.Email})

	// Clean up draft
	model.Drafts.Delete(userID)

	// Delete the keyboard message
	if draft.MessageID > 0 {
		tb.DeleteMessage(ctx, chatID, draft.MessageID)
	}

	tb.Send(ctx, chatID, tolgee.T("telegrambot.conversation.creationsuccess", lang))
}

// ProfileMessageHandler handles text messages during profile registration
// (when user is filling in a field)
// Returns true if the message was handled, false otherwise
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
	if draft == nil || draft.ActiveField == model.FieldNone {
		return false // No active registration or no field being edited
	}

	userRepo := database.NewUserRepository()
	lang := userRepo.GetLanguage(userID)
	text := strings.TrimSpace(update.Message.Text)

	switch draft.ActiveField {
	case model.FieldEmail:
		// Validate email format
		if !isValidMensaEmail(text) {
			tb.Send(ctx, chatID, tolgee.T("telegrambot.conversation.emailnotvalid", lang))
			return true
		}

		// Check if email already exists
		exists, err := userRepo.ExistsByEmail(text)
		if err != nil {
			logger.Error("Failed to check email", map[string]interface{}{"error": err.Error()})
			tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
			return true
		}

		if exists {
			tb.Send(ctx, chatID, tolgee.T("telegrambot.conversation.emailalreadyregistered", lang))
			return true
		}

		draft.Email = &text

	case model.FieldMembershipNumber:
		// Validate it's a number
		if _, err := strconv.ParseInt(text, 10, 64); err != nil {
			tb.Send(ctx, chatID, tolgee.T("telegrambot.conversation.notvalidmemberid", lang))
			return true
		}
		draft.MembershipNumber = &text
	}

	// Reset active field
	draft.ActiveField = model.FieldNone

	// Delete the user's input message
	tb.DeleteMessage(ctx, chatID, update.Message.ID)

	// Update the keyboard message with checkmarks
	if draft.MessageID > 0 {
		tb.EditMessageWithKeyboard(
			ctx,
			chatID,
			draft.MessageID,
			tolgee.T("telegrambot.profilecommand.initiateprofileregister", lang),
			ProfileKeyboard(draft, lang),
		)
	}

	return true
}

// isValidMensaEmail validates email format for Mensa Italy
func isValidMensaEmail(email string) bool {
	// Accept @mensa.it emails
	if !strings.HasSuffix(strings.ToLower(email), "@mensa.it") {
		return false
	}

	// More specific validation: nome.cognome@mensa.it or nome.cognome123@mensa.it
	regex := `^[a-zA-Z]+\.[a-zA-Z]+[0-9]{0,2}@mensa\.it$`
	re := regexp.MustCompile(regex)
	return re.MatchString(email)
}
