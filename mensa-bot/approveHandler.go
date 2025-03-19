package main

import (
	"context"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/tolgee"
	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func approveHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	userId := update.Message.From.ID
	if conversationStateSaver.GetState(userId) > -1 {
		return
	}

	if !utils.IsMemberRegistered(update.Message.From.ID) {
		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.startcommand.notregistered", "it"))
	} else {
		requestsToJoin, ok := requestsToApprove.GetRequests(update.Message.From.ID)
		log.Printf("INFO: Approving requests for user %d: %v", update.Message.From.ID, requestsToJoin)
		if !ok {
			utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.approvecommand.norequesttoapprove", "it"))
		} else {
			for _, chatId := range requestsToJoin {
				b.ApproveChatJoinRequest(ctx, &bot.ApproveChatJoinRequestParams{
					ChatID: chatId,
					UserID: update.Message.From.ID,
				})

				utils.RegisterGroupForUser(update.Message.From.ID, chatId)
			}
			requestsToApprove.RemoveRequests(update.Message.From.ID)
			utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.approvecommand.approved", "it"))
		}
	}
}
