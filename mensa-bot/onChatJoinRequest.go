package main

import (
	"context"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/tolgee"
	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func onChatJoinRequest(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatId := update.ChatJoinRequest.Chat.ID
	userId := update.ChatJoinRequest.From.ID
	firstName := update.ChatJoinRequest.From.FirstName

	if conversationStateSaver.GetState(userId) > -1 {
		return
	}

	// Check if user has a bot profile registered
	if !utils.IsMemberRegistered(userId) {
		log.Printf("INFO: User %d requested to join chat %d but is not registered", userId, chatId)

		paramMap := map[string]string{
			"name": firstName,
		}

		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.welcome.invitetojoin", "it", paramMap))
		requestsToApprove.AddRequest(userId, chatId)
	} else {
		log.Printf("INFO: Approving chat join request for user %d in chat %d", userId, chatId)
		b.ApproveChatJoinRequest(ctx, &bot.ApproveChatJoinRequestParams{
			ChatID: chatId,
			UserID: userId,
		})

		paramMap := map[string]string{
			"groupName": update.ChatJoinRequest.Chat.Title,
		}

		utils.SendMessage(b, ctx, userId, tolgee.GetTranslation("telegrambot.welcome.approvedtogroup", "it", paramMap))
		utils.RegisterGroupForUser(userId, chatId)
	}
}
