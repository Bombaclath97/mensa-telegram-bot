package utils

import (
	"context"

	"github.com/go-telegram/bot"
)

func SendMessage(b *bot.Bot, ctx context.Context, chatID int64, message string) {
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      message,
		ParseMode: "Markdown",
	})
}
