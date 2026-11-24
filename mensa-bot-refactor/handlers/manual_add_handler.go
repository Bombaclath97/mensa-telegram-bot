package handlers

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Mensa-Italia/mensa-telegram-bot/database"
	"github.com/Mensa-Italia/mensa-telegram-bot/logger"
	"github.com/Mensa-Italia/mensa-telegram-bot/model"
	tb "github.com/Mensa-Italia/mensa-telegram-bot/telegram_bot"
	"github.com/Mensa-Italia/mensa-telegram-bot/tolgee"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// ---------------------------------------------------------------------------
// /manual_add — ADMIN-ONLY manual registration of an international Mensa member.
//
// International members have no @mensa.it address and therefore cannot pass the
// normal email + Mensa-app verification, so a bot admin enrols them by hand:
//
//	MANUAL_ASK_USER_ID -> MANUAL_ASK_EMAIL -> MANUAL_ASK_MEMBER_NUMBER
//	                   -> MANUAL_ASK_NAME  -> MANUAL_ASK_SURNAME -> User row
//
// The written row is keyed by the TARGET user's Telegram ID (the one typed in
// step 1) and carries IsInternational=true, which exempts it from the
// @mensa.it email-pattern check and from the Mensa-app approval round-trip.
//
// LEGACY BUG FIXED HERE: mensa-bot/utils/intermediateUserSaver.go's
// GetCompleteUserAndCleanup built the User with TelegramID = the ADMIN's ID and
// silently dropped the typed target ID, so /manual_add registered the profile
// under the admin's own account. This implementation keys the row by
// draft.TargetUserID.
//
// STATE: kept in a package-private store, deliberately SEPARATE from
// model.Drafts, so an admin running /manual_add never clobbers (nor is blocked
// by) their own registration draft. The foundation contract has no
// ManualAddDraft type; see the report.
// ---------------------------------------------------------------------------

// manualAddStep is the position in the /manual_add wizard.
type manualAddStep int

const (
	manualAddStepUserID manualAddStep = iota
	manualAddStepEmail
	manualAddStepMemberNumber
	manualAddStepName
	manualAddStepSurname
)

// manualAddDraft is the admin-side conversation state for one /manual_add run.
type manualAddDraft struct {
	Step manualAddStep

	TargetUserID int64
	Email        string
	MemberNumber int64
	FirstName    string
	LastName     string

	ExpiresAt time.Time
}

func (d *manualAddDraft) isExpired() bool {
	return !d.ExpiresAt.IsZero() && time.Now().After(d.ExpiresAt)
}

// manualAddStore holds the in-flight /manual_add conversations, keyed by the
// ADMIN's Telegram ID.
type manualAddStore struct {
	mu     sync.Mutex
	drafts map[int64]*manualAddDraft
}

var manualAddDrafts = &manualAddStore{drafts: make(map[int64]*manualAddDraft)}

func (s *manualAddStore) get(adminID int64) *manualAddDraft {
	s.mu.Lock()
	defer s.mu.Unlock()

	draft, ok := s.drafts[adminID]
	if !ok {
		return nil
	}
	if draft.isExpired() {
		delete(s.drafts, adminID)
		return nil
	}
	return draft
}

func (s *manualAddStore) set(adminID int64, draft *manualAddDraft) {
	s.mu.Lock()
	defer s.mu.Unlock()

	draft.ExpiresAt = time.Now().Add(model.ConversationTimeout())
	s.drafts[adminID] = draft
}

// touch extends the TTL of a live draft. Returns false when there is none.
func (s *manualAddStore) touch(adminID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	draft, ok := s.drafts[adminID]
	if !ok || draft.isExpired() {
		return false
	}
	draft.ExpiresAt = time.Now().Add(model.ConversationTimeout())
	return true
}

func (s *manualAddStore) delete(adminID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.drafts, adminID)
}

// ManualAddInProgress reports whether the given admin is in the middle of a
// /manual_add wizard. Exposed so other command handlers (e.g. /start) can stay
// out of the way if they wish; nothing is required to call it.
func ManualAddInProgress(adminID int64) bool {
	return manualAddDrafts.get(adminID) != nil
}

// ManualAddCancel aborts an in-flight /manual_add wizard for the given admin.
// Returns true if a wizard was actually cancelled. The /cancel command handler
// should call this (a cancelled admin must not be left mid-wizard).
func ManualAddCancel(adminID int64) bool {
	if manualAddDrafts.get(adminID) == nil {
		return false
	}
	manualAddDrafts.delete(adminID)
	logger.Info("Manual add cancelled", map[string]interface{}{"admin_id": adminID})
	return true
}

// ManualAddCommandHandler handles the /manual_add command.
//
// It is gated on is_bot_admin (seeded from ADMIN_TELEGRAM_IDS). A non-admin
// gets NO reply at all — same observable behaviour as if the command did not
// exist — so the command's existence is never leaked. It is also deliberately
// EXCLUDED from SetMyCommands.
func ManualAddCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !IsPrivateChat(update) {
		return
	}

	adminID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	userRepo := database.NewUserRepository()

	isAdmin, err := userRepo.IsBotAdmin(adminID)
	if err != nil {
		// Do not leak the command's existence on a DB failure either: log and
		// stay silent, exactly as for a non-admin.
		logger.Error("Failed to check bot admin status for /manual_add", map[string]interface{}{
			"error":    err.Error(),
			"admin_id": adminID,
		})
		return
	}
	if !isAdmin {
		logger.Debug("Ignoring /manual_add from non-admin", map[string]interface{}{"user_id": adminID})
		return
	}

	lang := userRepo.GetLanguage(adminID)

	logger.Info("Starting manual add wizard", map[string]interface{}{"admin_id": adminID})

	manualAddDrafts.set(adminID, &manualAddDraft{Step: manualAddStepUserID})

	tb.Send(ctx, chatID, tolgee.T("telegrambot.manualadd.requestuserid", lang))
}

// ManualAddMessageHandler feeds a free-text message into an in-flight
// /manual_add wizard.
//
// Returns true when the message was consumed by the wizard (the caller must
// then stop processing it). Returns false when the sender has no live
// /manual_add draft, so the normal message pipeline (profile field capture,
// forwarded-message lookup, ...) can carry on.
//
// The integrator must call this from defaultHandler BEFORE
// ProfileMessageHandler.
func ManualAddMessageHandler(ctx context.Context, b *bot.Bot, update *models.Update) bool {
	if update.Message == nil || update.Message.From == nil {
		return false
	}
	if update.Message.Chat.Type != "private" {
		return false
	}

	adminID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	draft := manualAddDrafts.get(adminID)
	if draft == nil {
		return false
	}

	text := strings.TrimSpace(update.Message.Text)
	if text == "" {
		return false
	}

	// Never swallow commands: they must keep routing (notably /cancel).
	if strings.HasPrefix(text, "/") {
		return false
	}

	// Keep the wizard alive while the admin is actively typing.
	manualAddDrafts.touch(adminID)

	userRepo := database.NewUserRepository()
	lang := userRepo.GetLanguage(adminID)

	switch draft.Step {
	case manualAddStepUserID:
		manualAddHandleUserID(ctx, chatID, adminID, draft, lang, text, userRepo)

	case manualAddStepEmail:
		manualAddHandleEmail(ctx, chatID, adminID, draft, lang, text, userRepo)

	case manualAddStepMemberNumber:
		manualAddHandleMemberNumber(ctx, chatID, draft, lang, text)

	case manualAddStepName:
		draft.FirstName = text
		draft.Step = manualAddStepSurname
		tb.Send(ctx, chatID, tolgee.T("telegrambot.manualadd.requestsurname", lang, map[string]string{
			"name": text,
		}))

	case manualAddStepSurname:
		draft.LastName = text
		manualAddFinish(ctx, chatID, adminID, draft, lang, userRepo)
	}

	return true
}

// manualAddHandleUserID validates and stores the TARGET user's Telegram ID.
func manualAddHandleUserID(ctx context.Context, chatID, adminID int64, draft *manualAddDraft, lang, text string, userRepo *database.UserRepository) {
	targetID, err := strconv.ParseInt(text, 10, 64)
	if err != nil || targetID <= 0 {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.manualadd.notvaliduserid", lang))
		return
	}

	exists, err := userRepo.Exists(targetID)
	if err != nil {
		logger.Error("Failed to check target user existence in /manual_add", map[string]interface{}{
			"error":     err.Error(),
			"target_id": targetID,
		})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}
	if exists {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.manualadd.useralreadyregistered", lang, map[string]string{
			"userId": strconv.FormatInt(targetID, 10),
		}))
		return
	}

	draft.TargetUserID = targetID
	draft.Step = manualAddStepEmail

	logger.Debug("Manual add: target user id accepted", map[string]interface{}{
		"admin_id":  adminID,
		"target_id": targetID,
	})

	tb.Send(ctx, chatID, tolgee.T("telegrambot.manualadd.requestemail", lang))
}

// manualAddHandleEmail stores the target's email.
//
// NOTE: NO @mensa.it pattern check here, by design — international members do
// not have a mensa.it address; that exemption is the whole point of the flow
// (and of IsInternational). Only a minimal sanity check and a uniqueness check
// are applied.
func manualAddHandleEmail(ctx context.Context, chatID, adminID int64, draft *manualAddDraft, lang, text string, userRepo *database.UserRepository) {
	email := strings.ToLower(text)

	if !manualAddLooksLikeEmail(email) {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.manualadd.notvalidemail", lang))
		return
	}

	exists, err := userRepo.ExistsByEmail(email)
	if err != nil {
		logger.Error("Failed to check email in /manual_add", map[string]interface{}{
			"error":    err.Error(),
			"admin_id": adminID,
		})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}
	if exists {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.manualadd.emailalreadyregistered", lang))
		return
	}

	draft.Email = email
	draft.Step = manualAddStepMemberNumber

	tb.Send(ctx, chatID, tolgee.T("telegrambot.manualadd.requestmemberid", lang))
}

// manualAddHandleMemberNumber validates and stores the Mensa MEMBERSHIP number
// (not the Telegram ID).
func manualAddHandleMemberNumber(ctx context.Context, chatID int64, draft *manualAddDraft, lang, text string) {
	memberNumber, err := strconv.ParseInt(text, 10, 64)
	if err != nil || memberNumber <= 0 {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.manualadd.notvalidmemberid", lang))
		return
	}

	draft.MemberNumber = memberNumber
	draft.Step = manualAddStepName

	tb.Send(ctx, chatID, tolgee.T("telegrambot.manualadd.requestname", lang))
}

// manualAddFinish writes the User row, keyed by the TARGET user's Telegram ID.
func manualAddFinish(ctx context.Context, chatID, adminID int64, draft *manualAddDraft, lang string, userRepo *database.UserRepository) {
	// Re-check both uniqueness constraints at commit time: the wizard is a
	// multi-minute conversation and the target may have self-registered in the
	// meantime (which would otherwise blow up on the PK / leave a dupe email).
	exists, err := userRepo.Exists(draft.TargetUserID)
	if err != nil {
		logger.Error("Failed to re-check target user before manual add commit", map[string]interface{}{
			"error":     err.Error(),
			"target_id": draft.TargetUserID,
		})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}
	if exists {
		manualAddDrafts.delete(adminID)
		tb.Send(ctx, chatID, tolgee.T("telegrambot.manualadd.useralreadyregistered", lang, map[string]string{
			"userId": strconv.FormatInt(draft.TargetUserID, 10),
		}))
		return
	}

	emailTaken, err := userRepo.ExistsByEmail(draft.Email)
	if err != nil {
		logger.Error("Failed to re-check email before manual add commit", map[string]interface{}{
			"error":     err.Error(),
			"target_id": draft.TargetUserID,
		})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}
	if emailTaken {
		manualAddDrafts.delete(adminID)
		tb.Send(ctx, chatID, tolgee.T("telegrambot.manualadd.emailalreadyregistered", lang))
		return
	}

	user := &database.User{
		// THE FIX: the row belongs to the TARGET user, not to the admin who
		// typed the wizard.
		TelegramID:      draft.TargetUserID,
		MensaEmail:      draft.Email,
		MemberNumber:    draft.MemberNumber,
		FirstName:       draft.FirstName,
		LastName:        draft.LastName,
		IsInternational: true,
		// Language is left empty on purpose: UserRepository.Create resolves the
		// TARGET's pending /lang preference (falling back to "it").
	}

	if err := userRepo.Create(user); err != nil {
		logger.Error("Failed to manually create user", map[string]interface{}{
			"error":     err.Error(),
			"admin_id":  adminID,
			"target_id": draft.TargetUserID,
		})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}

	manualAddDrafts.delete(adminID)

	logger.Info("International member manually registered", map[string]interface{}{
		"admin_id":      adminID,
		"target_id":     draft.TargetUserID,
		"member_number": draft.MemberNumber,
	})

	tb.Send(ctx, chatID, tolgee.T("telegrambot.manualadd.success", lang, map[string]string{
		"name":     draft.FirstName,
		"lastName": draft.LastName,
		"userId":   strconv.FormatInt(draft.TargetUserID, 10),
	}))

	// Best-effort courtesy DM to the newly registered member, in THEIR language.
	targetLang := userRepo.GetLanguage(draft.TargetUserID)
	if _, err := tb.Send(ctx, draft.TargetUserID, tolgee.T("telegrambot.manualadd.registerednotice", targetLang, map[string]string{
		"name": draft.FirstName,
	})); err != nil {
		// Entirely expected when the member has never DM'd the bot.
		logger.Debug("Could not notify manually added member", map[string]interface{}{
			"error":     err.Error(),
			"target_id": draft.TargetUserID,
		})
	}

	// Same post-registration work as the app flow: drain their buffered join requests and
	// learn which groups they are already in. A manually added international member reaches
	// this by a different path but ends up in exactly the same state, so skipping it here
	// would leave precisely the population /manual_add exists to serve invisible to the
	// cleanup sweep.
	//
	// NOTE: acts on the TARGET, not on the admin who ran the command.
	CompleteRegistrationSideEffects(ctx, draft.TargetUserID, targetLang)
}

// manualAddLooksLikeEmail is a deliberately permissive check: it only rejects
// obvious garbage. International members' addresses are arbitrary, so anything
// stricter would defeat the purpose of the flow.
func manualAddLooksLikeEmail(email string) bool {
	if strings.ContainsAny(email, " \t\n") {
		return false
	}
	at := strings.Index(email, "@")
	if at <= 0 || at != strings.LastIndex(email, "@") {
		return false
	}
	domain := email[at+1:]
	dot := strings.Index(domain, ".")
	return dot > 0 && dot < len(domain)-1
}
