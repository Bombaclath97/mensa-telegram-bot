package handlers

import (
	"context"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	"github.com/Bombaclath97/mensa-telegram-bot/model"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
	"github.com/Bombaclath97/mensa-telegram-bot/tolgee"
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
	pendingRepo := database.NewPendingJoinRequestRepository()

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

	pending, err := pendingRepo.GetForUser(userID)
	if err != nil {
		logger.Error("Failed to read pending join requests", map[string]interface{}{
			"user_id": userID,
			"error":   err.Error(),
		})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}

	if len(pending) == 0 {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.approvecommand.norequesttoapprove", lang))
		return
	}

	approved := 0
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

	if approved == 0 {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}

	logger.Info("Drained pending join requests", map[string]interface{}{
		"user_id":  userID,
		"approved": approved,
		"pending":  len(pending),
	})

	tb.Send(ctx, chatID, tolgee.T("telegrambot.approvecommand.approved", lang))
}
