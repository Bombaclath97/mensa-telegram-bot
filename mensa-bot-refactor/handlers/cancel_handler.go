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

// CancelCommandHandler handles the /cancel command (G15).
//
// It is the manual escape hatch out of ANY in-flight conversation:
//   - the profile registration wizard (model.Drafts), including the LOCKED
//     AWAITING_APPROVAL state — the unlock here is deliberately unconditional,
//     because the owner of the draft is the one asking to abort it;
//   - the admin /manual_add wizard (handlers.ManualAddCancel).
//
// Because /start and /elimina early-return while a draft exists, a stuck draft
// used to soft-brick the user; this command always clears it and always confirms.
func CancelCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	if !IsPrivateChat(update) {
		return
	}

	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	// Drop the profile draft (Delete removes the draft whatever its state:
	// locked / awaiting approval / mid-field). This is the unconditional unlock.
	hadDraft := model.Drafts.Exists(userID)
	model.Drafts.Delete(userID)

	// Drop a /manual_add wizard the user may be running as a bot admin.
	hadManualAdd := ManualAddCancel(userID)

	logger.Debug("Processing /cancel command", map[string]interface{}{
		"user_id":        userID,
		"had_draft":      hadDraft,
		"had_manual_add": hadManualAdd,
	})

	userRepo := database.NewUserRepository()
	lang := userRepo.GetLanguage(userID)

	if _, err := tb.Send(ctx, chatID, tolgee.T("telegrambot.cancelcommand.cancelled", lang)); err != nil {
		logger.Error("Failed to send cancel confirmation", map[string]interface{}{
			"error":   err.Error(),
			"user_id": userID,
		})
	}
}
