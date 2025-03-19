package main

import (
	"context"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/tolgee"
	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func startHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.From.ID
	if conversationStateSaver.GetState(userId) > -1 {
		return
	}
	if !utils.IsMemberRegistered(userId) {
		log.Printf("INFO: User %d is not registered", userId)

		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.startcommand.notregistered", "it"))
	} else {
		log.Printf("INFO: User %d is already registered", userId)
		paramMap := map[string]string{}

		chatUser, err := utils.GetMember(userId)
		if err != nil {
			paramMap["name"] = update.Message.From.FirstName
		} else {
			paramMap["name"] = chatUser.FirstName
		}

		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.startcommand.alreadyregistered", "it", paramMap))
	}
}
