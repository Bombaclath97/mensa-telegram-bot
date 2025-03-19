package main

import (
	"context"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/tolgee"
	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func appHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.From.ID
	if conversationStateSaver.GetState(userId) > -1 {
		return
	}

	utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.appcommand.sendapp", "it"))
}
