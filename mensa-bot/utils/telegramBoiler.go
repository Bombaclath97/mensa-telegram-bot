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

func DemoteUser(b *bot.Bot, ctx context.Context, chatID int64, userID int64) {
	b.PromoteChatMember(ctx, &bot.PromoteChatMemberParams{
		ChatID:              chatID,
		UserID:              userID,
		CanManageChat:       false,
		CanChangeInfo:       false,
		CanDeleteMessages:   false,
		CanInviteUsers:      false,
		CanRestrictMembers:  false,
		CanPinMessages:      false,
		CanPromoteMembers:   false,
		CanManageVideoChats: false,
		CanPostMessages:     false,
		CanEditMessages:     false,
		CanPostStories:      false,
		CanEditStories:      false,
		CanDeleteStories:    false,
		CanManageTopics:     false,
	})
}

func KickUser(b *bot.Bot, ctx context.Context, chatID int64, userID int64) {
	b.BanChatMember(ctx, &bot.BanChatMemberParams{
		ChatID: chatID,
		UserID: userID,
	})
	b.UnbanChatMember(ctx, &bot.UnbanChatMemberParams{
		ChatID: chatID,
		UserID: userID,
	})
}
