package main

import (
	"context"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func onBotBecomesAdmin(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatId := update.MyChatMember.Chat.ID

	// Get all group admins and flag them as admins if they have a profile in the database
	admins, err := b.GetChatAdministrators(ctx, &bot.GetChatAdministratorsParams{
		ChatID: chatId,
	})

	if err != nil {
		log.Printf("Error getting chat admins: %v", err)
		return
	}

	for _, admin := range admins {
		if admin.Administrator.User.ID == b.ID() ||
			admin.Administrator.User.IsBot {
			continue
		}

		userId := admin.Administrator.User.ID
		if !utils.IsMemberRegistered(userId) {
			continue
		}

		utils.RegisterGroupAdmin(userId, chatId)
	}
}
