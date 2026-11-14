package handlers

import (
	"context"
	"strconv"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	"github.com/Bombaclath97/mensa-telegram-bot/membership"
	"github.com/Bombaclath97/mensa-telegram-bot/model"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
	"github.com/Bombaclath97/mensa-telegram-bot/tolgee"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// DeleteCommandHandler handles the /elimina command
func DeleteCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// Only respond in private chats
	if !IsPrivateChat(update) {
		return
	}

	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	logger.Debug("Processing /elimina command", map[string]interface{}{
		"user_id": userID,
		"chat_id": chatID,
	})

	// Skip if user has active registration
	if model.Drafts.Exists(userID) {
		return
	}

	userRepo := database.NewUserRepository()
	lang := userRepo.GetLanguage(userID)

	// G21: a transient DB error must not look like "not registered".
	user, err := userRepo.GetByID(userID)
	if err != nil {
		logger.Error("Failed to look up user", map[string]interface{}{
			"error":   err.Error(),
			"user_id": userID,
		})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}

	if user == nil {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.startcommand.notregistered", lang))
		return
	}

	// Show delete confirmation keyboard
	keyboard := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{
					Text:         "\u2705 " + tolgee.T("telegrambot.deletecommand.confirmbutton", lang),
					CallbackData: "delete:confirm",
				},
			},
			{
				{
					Text:         "\u274C " + tolgee.T("telegrambot.general.cancel", lang),
					CallbackData: "delete:cancel",
				},
			},
		},
	}

	params := map[string]string{"name": user.FirstName}
	tb.SendWithKeyboard(ctx, chatID, tolgee.T("telegrambot.deletecommand.deleteprofile", lang, params), keyboard)
}

// DeleteCallbackHandler handles delete confirmation callbacks
func DeleteCallbackHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// Only respond in private chats
	if !IsPrivateChat(update) {
		return
	}

	q := update.CallbackQuery
	userID := q.From.ID
	chatID := q.Message.Message.Chat.ID
	messageID := q.Message.Message.ID

	logger.Debug("Processing delete callback", map[string]interface{}{
		"user_id":       userID,
		"callback_data": q.Data,
	})

	userRepo := database.NewUserRepository()
	lang := userRepo.GetLanguage(userID)

	switch q.Data {
	case "delete:confirm":
		res, err := DeleteMemberEverywhere(ctx, userID, lang)
		if err != nil {
			tb.AnswerCallbackSilent(ctx, q.ID)
			tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
			return
		}

		// Delete the confirmation message
		tb.DeleteMessage(ctx, chatID, messageID)

		tb.Send(ctx, chatID, deletedMessage(res, lang))

	case "delete:cancel":
		// Delete the confirmation message
		tb.DeleteMessage(ctx, chatID, messageID)

		tb.Send(ctx, chatID, tolgee.T("telegrambot.cancelcommand.cancelled", lang))
	}

	tb.AnswerCallbackSilent(ctx, q.ID)
}

// DeleteResult describes a completed DeleteMemberEverywhere.
type DeleteResult struct {
	// PendingGroups are the groups the member could not be removed from right now. Their
	// profile is deleted regardless; these are queued in pending_removals and retried by
	// membership.StartPendingRemovals.
	PendingGroups []int64
}

// DeleteMemberEverywhere removes a member from every group the bot manages and deletes their
// profile (the group associations cascade with it).
//
// It is the single implementation of "remove this member at their request", shared by
// /elimina and the profile card's delete button. The per-group work is membership.
// RemoveFromGroup, the same code the expiry sweep uses.
//
// The member asked for their data to be deleted, so the profile is deleted even when some
// group could not be handled right now. Those groups are recorded in pending_removals, in the
// same transaction, so the bot never loses track of a group it still has to remove them from.
//
// An error means nothing in the database changed: the group lookup or the transaction failed.
func DeleteMemberEverywhere(ctx context.Context, userID int64, lang string) (DeleteResult, error) {
	var result DeleteResult
	userRepo := database.NewUserRepository()
	groupAssocRepo := database.NewGroupAssociationRepository()

	groups, err := groupAssocRepo.GetUserGroups(userID)
	if err != nil {
		logger.Error("Failed to load user groups, aborting deletion", map[string]interface{}{
			"error":   err.Error(),
			"user_id": userID,
		})
		return result, err
	}

	opt := membership.Options{
		Delay:       membership.DefaultDelay(),
		OwnerNotice: tolgee.T("telegrambot.deletecommand.ownerdeleted", lang),
	}
	for _, assoc := range groups {
		res := membership.RemoveFromGroup(ctx, assoc.GroupID, userID, opt)
		fields := map[string]interface{}{
			"user_id":  userID,
			"group_id": assoc.GroupID,
			"outcome":  res.Outcome.String(),
			"role":     string(res.Role),
		}
		if res.Err != nil {
			fields["error"] = res.Err.Error()
		}
		if res.Outcome == membership.Failed {
			result.PendingGroups = append(result.PendingGroups, assoc.GroupID)
			logger.Warn("Could not remove member from group, queueing a retry", fields)
			continue
		}
		logger.Info("Removed member from group", fields)
	}

	if err := userRepo.DeleteWithPendingRemovals(userID, result.PendingGroups); err != nil {
		logger.Error("Failed to delete user", map[string]interface{}{
			"error":   err.Error(),
			"user_id": userID,
		})
		return DeleteResult{}, err
	}

	logger.Info("User deleted", map[string]interface{}{
		"user_id":        userID,
		"groups":         len(groups),
		"pending_groups": len(result.PendingGroups),
	})
	return result, nil
}

// deletedMessage is what the member is told after DeleteMemberEverywhere succeeds.
func deletedMessage(res DeleteResult, lang string) string {
	if len(res.PendingGroups) == 0 {
		return tolgee.T("telegrambot.deletecommand.deleted", lang)
	}
	return tolgee.T("telegrambot.deletecommand.deletedpending", lang, map[string]string{
		"count": strconv.Itoa(len(res.PendingGroups)),
	})
}
