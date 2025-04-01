package main

import (
	"context"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/tolgee"
	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func onBotJoinsGroup(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatId := update.Message.Chat.ID
	utils.RegisterBotGroup(chatId)
	utils.SendMessage(b, ctx, chatId, tolgee.GetTranslation("telegrambot.welcome.botjoined", "it"))
}
