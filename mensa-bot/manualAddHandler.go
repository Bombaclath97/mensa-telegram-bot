package main

import (
	"context"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func manualAddHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.From.ID
	if !utils.IsUserBotAdministrator(userId) {
		return
	}

	conversationStateSaver.SetState(userId, utils.MANUAL_ASK_USER_ID)
}
