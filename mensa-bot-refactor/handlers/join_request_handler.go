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

// MatchChatJoinRequest is the match predicate for ChatJoinRequestHandler.
// Register with: b.RegisterHandlerMatchFunc(handlers.MatchChatJoinRequest, handlers.ChatJoinRequestHandler)
func MatchChatJoinRequest(update *models.Update) bool {
	return update != nil && update.ChatJoinRequest != nil
}

// ChatJoinRequestHandler gates every group the bot administers.
//
//   - registered member  -> the join request is approved immediately, the group
//     association is persisted (this is the write that makes /profilo's group list and
//     /elimina's kick-from-all-groups real) and the user gets a DM.
//   - unregistered user  -> the request is BUFFERED in the pending_join_requests table
//     (the legacy kept this in an in-memory map that was lost on every restart) and the
//     user is invited to register. They can later replay the buffer with /approva.
func ChatJoinRequestHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !MatchChatJoinRequest(update) {
		return
	}

	req := update.ChatJoinRequest
	groupID := req.Chat.ID
	userID := req.From.ID
	groupName := req.Chat.Title

	logger.Debug("Received chat join request", map[string]interface{}{
		"user_id":  userID,
		"group_id": groupID,
	})

	userRepo := database.NewUserRepository()
	pendingRepo := database.NewPendingJoinRequestRepository()

	user, err := userRepo.GetByID(userID)
	if err != nil {
		// A DB failure must never be read as "not registered": buffer the request so the
		// user can still get in via /approva once the DB is healthy again.
		logger.Error("Failed to look up user for join request", map[string]interface{}{
			"user_id":  userID,
			"group_id": groupID,
			"error":    err.Error(),
		})
		if addErr := pendingRepo.Add(userID, groupID); addErr != nil {
			logger.Error("Failed to buffer pending join request", map[string]interface{}{
				"user_id":  userID,
				"group_id": groupID,
				"error":    addErr.Error(),
			})
		}
		return
	}

	lang := userRepo.GetLanguage(userID)

	if user == nil {
		// Not a member (yet). Buffer the request and invite them to register.
		if addErr := pendingRepo.Add(userID, groupID); addErr != nil {
			logger.Error("Failed to buffer pending join request", map[string]interface{}{
				"user_id":  userID,
				"group_id": groupID,
				"error":    addErr.Error(),
			})
		}

		// Don't interrupt an in-flight registration wizard with an unsolicited DM; the
		// request is buffered either way and /approva will drain it.
		if model.Drafts.Exists(userID) {
			logger.Debug("Join request buffered during active registration", map[string]interface{}{
				"user_id":  userID,
				"group_id": groupID,
			})
			return
		}

		params := map[string]string{"name": req.From.FirstName}
		if _, err := tb.Send(ctx, userID, tolgee.T("telegrambot.welcome.invitetojoin", lang, params)); err != nil {
			// Very common: the user has never opened a private chat with the bot.
			logger.Warn("Could not DM unregistered user about join request", map[string]interface{}{
				"user_id": userID,
				"error":   err.Error(),
			})
		}
		return
	}

	// Registered member: auto-approve for any group the bot administers.
	joinRequestApproveAndAssociate(ctx, userID, groupID, groupName, lang, true)
}

// joinRequestApproveAndAssociate approves the pending Telegram join request and persists
// the group association. Shared with the /approva command handler, which passes
// notify=false because it sends a single summary message instead of one DM per group.
func joinRequestApproveAndAssociate(ctx context.Context, userID, groupID int64, groupName, lang string, notify bool) bool {
	ok, err := tb.ApproveJoinRequest(ctx, groupID, userID)
	if err != nil || !ok {
		fields := map[string]interface{}{"user_id": userID, "group_id": groupID}
		if err != nil {
			fields["error"] = err.Error()
		}
		logger.Error("Failed to approve chat join request", fields)
		return false
	}

	assocRepo := database.NewGroupAssociationRepository()
	// Register is idempotent and creates the group row first, so it cannot fail the FK.
	if err := assocRepo.Register(userID, groupID); err != nil {
		logger.Error("Failed to register group association", map[string]interface{}{
			"user_id":  userID,
			"group_id": groupID,
			"error":    err.Error(),
		})
		return false
	}

	pendingRepo := database.NewPendingJoinRequestRepository()
	if err := pendingRepo.Delete(userID, groupID); err != nil {
		logger.Warn("Failed to drop pending join request", map[string]interface{}{
			"user_id":  userID,
			"group_id": groupID,
			"error":    err.Error(),
		})
	}

	if notify {
		if groupName == "" {
			if chat, err := tb.GetChat(ctx, groupID); err == nil && chat != nil {
				groupName = chat.Title
			}
		}

		params := map[string]string{"groupName": groupName}
		if _, err := tb.Send(ctx, userID, tolgee.T("telegrambot.welcome.approvedtogroup", lang, params)); err != nil {
			logger.Warn("Could not DM user about approved join request", map[string]interface{}{
				"user_id": userID,
				"error":   err.Error(),
			})
		}
	}

	logger.Info("Approved chat join request", map[string]interface{}{
		"user_id":  userID,
		"group_id": groupID,
	})
	return true
}
