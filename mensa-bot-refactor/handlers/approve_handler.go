package handlers

import (
	"context"

	"github.com/Mensa-Italia/mensa-telegram-bot/database"
	"github.com/Mensa-Italia/mensa-telegram-bot/logger"
	"github.com/Mensa-Italia/mensa-telegram-bot/model"
	tb "github.com/Mensa-Italia/mensa-telegram-bot/telegram_bot"
	"github.com/Mensa-Italia/mensa-telegram-bot/tolgee"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// ApproveCommandHandler handles /approva: it drains the join requests that were buffered
// while the user was still unregistered (see ChatJoinRequestHandler), approving each one
// and writing the corresponding group association.
//
// Register with:
//
//	b.RegisterHandler(bot.HandlerTypeMessageText, "approva", bot.MatchTypeCommand, handlers.ApproveCommandHandler)
func ApproveCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !IsPrivateChat(update) || update.Message == nil {
		return
	}

	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	// Don't interfere with an in-flight registration wizard.
	if model.Drafts.Exists(userID) {
		return
	}

	logger.Debug("Processing /approva command", map[string]interface{}{"user_id": userID})

	userRepo := database.NewUserRepository()

	lang := userRepo.GetLanguage(userID)

	user, err := userRepo.GetByID(userID)
	if err != nil {
		logger.Error("Failed to look up user for /approva", map[string]interface{}{
			"user_id": userID,
			"error":   err.Error(),
		})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}

	if user == nil {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.startcommand.notregistered", lang))
		return
	}

	approved, buffered := DrainPendingJoinRequests(ctx, userID, lang)

	switch {
	case buffered == 0:
		tb.Send(ctx, chatID, tolgee.T("telegrambot.approvecommand.norequesttoapprove", lang))
	case approved == 0:
		// There WERE requests and not one could be approved: that is a real failure, not a
		// quiet no-op.
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
	default:
		tb.Send(ctx, chatID, tolgee.T("telegrambot.approvecommand.approved", lang))
	}
}

// DrainPendingJoinRequests approves every join request buffered for this user while they
// were still unregistered, and clears the buffer. It returns how many were approved and how
// many were buffered to begin with — the caller needs both to tell "nothing to do" apart
// from "everything failed".
//
// Extracted from /approva so that REGISTRATION can call it too. Until it did, the flow had a
// hole big enough to strand every new member: an unregistered user requesting to join was
// buffered and told to register, they registered — and then nothing happened. The buffer sat
// there until they somehow worked out that /approva existed and typed it. The README even
// promised "/approva (or the next automatic pass)"; there was no automatic pass.
//
// The caller owns the user-facing messaging: /approva sends a summary, registration folds it
// into the welcome. Hence no messages here beyond the per-group DMs, and notify=false below.
func DrainPendingJoinRequests(ctx context.Context, userID int64, lang string) (approved, buffered int) {
	pendingRepo := database.NewPendingJoinRequestRepository()

	pending, err := pendingRepo.GetForUser(userID)
	if err != nil {
		logger.Error("Failed to read pending join requests", map[string]interface{}{
			"user_id": userID,
			"error":   err.Error(),
		})
		return 0, 0
	}
	if len(pending) == 0 {
		return 0, 0
	}

	for _, request := range pending {
		if joinRequestApproveAndAssociate(ctx, userID, request.GroupID, "", lang, false) {
			approved++
			continue
		}

		// The request could not be approved (most often it expired or was handled by a
		// human admin in the meantime). Drop it so the buffer cannot get stuck forever.
		if delErr := pendingRepo.Delete(userID, request.GroupID); delErr != nil {
			logger.Warn("Failed to drop stale pending join request", map[string]interface{}{
				"user_id":  userID,
				"group_id": request.GroupID,
				"error":    delErr.Error(),
			})
		}
	}

	// Belt and braces: the buffer is now fully drained either way.
	if err := pendingRepo.DeleteForUser(userID); err != nil {
		logger.Warn("Failed to clear pending join requests", map[string]interface{}{
			"user_id": userID,
			"error":   err.Error(),
		})
	}

	logger.Info("Drained pending join requests", map[string]interface{}{
		"user_id":  userID,
		"approved": approved,
		"pending":  len(pending),
	})
	return approved, len(pending)
}
