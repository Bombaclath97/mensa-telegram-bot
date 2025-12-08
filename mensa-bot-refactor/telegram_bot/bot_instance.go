package telegrambot

import (
	"context"
	"sync"

	"github.com/go-telegram/bot"
)

var (
	instance *bot.Bot
	once     sync.Once
)

func Init(bot *bot.Bot) {
	once.Do(func() {
		instance = bot
	})
}

func Get() *bot.Bot {
	return instance
}

func Send(ctx context.Context, chatId int64, message string) {
	instance.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatId,
		Text:      message,
		ParseMode: "Markdown",
	})
}

func KickUserFromGroup(ctx context.Context, chatId int64, userId int64) {
	instance.UnbanChatMember(ctx, &bot.UnbanChatMemberParams{
		ChatID: chatId,
		UserID: userId,
	})
}

func ApproveJoinRequest(ctx context.Context, chatId int64, userId int64) {
	instance.ApproveChatJoinRequest(ctx, &bot.ApproveChatJoinRequestParams{
		ChatID: chatId,
		UserID: userId,
	})
}
