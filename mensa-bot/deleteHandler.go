package main

import (
	"context"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/tolgee"
	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func deleteHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.From.ID
	if conversationStateSaver.GetState(userId) > -1 {
		return
	}

	if !utils.IsMemberRegistered(userId) {
		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.startcommand.notregistered", "it"))
	} else {
		user, _ := utils.GetMember(userId)

		paramMap := map[string]string{
			"name": user.FirstName,
		}

		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.deletecommand.deleteprofile", "it", paramMap))
		conversationStateSaver.SetState(userId, utils.ASKED_DELETE_CONFIRMATION)
	}
}
