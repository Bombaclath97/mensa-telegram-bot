package telegrambot

import (
	"context"
	"errors"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

var (
	instance *bot.Bot
	once     sync.Once
)

// Init initializes the bot singleton. Must be called once at startup.
func Init(b *bot.Bot) {
	once.Do(func() {
		instance = b
	})
}

// Get returns the bot instance
func Get() (*bot.Bot, error) {
	if instance == nil {
		return nil, errors.New("FATAL: Bot not initialized. Call Init() first")
	}
	return instance, nil
}

// MustGet returns the bot instance or panics if not initialized
func MustGet() *bot.Bot {
	if instance == nil {
		panic("FATAL: Bot not initialized. Call Init() first")
	}
	return instance
}

// Send sends a text message to the specified chat (plain text, no markdown)
func Send(ctx context.Context, chatID int64, message string) (*models.Message, error) {
	return instance.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID,
		Text:   message,
	})
}

// SendWithKeyboard sends a message with an inline keyboard (plain text, no markdown)
func SendWithKeyboard(ctx context.Context, chatID int64, message string, keyboard *models.InlineKeyboardMarkup) (*models.Message, error) {
	return instance.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        message,
		ReplyMarkup: keyboard,
	})
}

// SendMarkdown sends a message with Markdown formatting
func SendMarkdown(ctx context.Context, chatID int64, message string) (*models.Message, error) {
	return instance.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      message,
		ParseMode: models.ParseModeMarkdown,
	})
}

// SendPlain is an alias for Send (kept for backwards compatibility)
func SendPlain(ctx context.Context, chatID int64, message string) (*models.Message, error) {
	return Send(ctx, chatID, message)
}

// EditMessageText edits an existing message's text (plain text, no markdown)
func EditMessageText(ctx context.Context, chatID int64, messageID int, text string) (*models.Message, error) {
	return instance.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		Text:      text,
	})
}

// EditMessageKeyboard edits an existing message's inline keyboard
func EditMessageKeyboard(ctx context.Context, chatID int64, messageID int, keyboard *models.InlineKeyboardMarkup) (*models.Message, error) {
	return instance.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{
		ChatID:      chatID,
		MessageID:   messageID,
		ReplyMarkup: keyboard,
	})
}

// EditMessageWithKeyboard edits an existing message's text and inline keyboard
func EditMessageWithKeyboard(ctx context.Context, chatID int64, messageID int, text string, keyboard *models.InlineKeyboardMarkup) (*models.Message, error) {
	return instance.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   messageID,
		Text:        text,
		ReplyMarkup: keyboard,
	})
}

// DeleteMessage deletes a message
func DeleteMessage(ctx context.Context, chatID int64, messageID int) (bool, error) {
	return instance.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    chatID,
		MessageID: messageID,
	})
}

// AnswerCallback answers a callback query
func AnswerCallback(ctx context.Context, callbackQueryID string, text string) (bool, error) {
	return instance.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: callbackQueryID,
		Text:            text,
	})
}

// AnswerCallbackSilent answers a callback query without showing any message
func AnswerCallbackSilent(ctx context.Context, callbackQueryID string) (bool, error) {
	return instance.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: callbackQueryID,
	})
}

// GetChat retrieves chat information
func GetChat(ctx context.Context, chatID int64) (*models.ChatFullInfo, error) {
	return instance.GetChat(ctx, &bot.GetChatParams{
		ChatID: chatID,
	})
}

// GetChatMember retrieves information about a chat member
func GetChatMember(ctx context.Context, chatID, userID int64) (*models.ChatMember, error) {
	return instance.GetChatMember(ctx, &bot.GetChatMemberParams{
		ChatID: chatID,
		UserID: userID,
	})
}

// KickUser kicks (bans then unbans) a user from a group
func KickUser(ctx context.Context, chatID, userID int64) error {
	_, err := instance.BanChatMember(ctx, &bot.BanChatMemberParams{
		ChatID: chatID,
		UserID: userID,
	})
	if err != nil {
		return err
	}

	_, err = instance.UnbanChatMember(ctx, &bot.UnbanChatMemberParams{
		ChatID:       chatID,
		UserID:       userID,
		OnlyIfBanned: true,
	})
	return err
}

// DemoteUser removes admin privileges from a user
func DemoteUser(ctx context.Context, chatID, userID int64) (bool, error) {
	return instance.PromoteChatMember(ctx, &bot.PromoteChatMemberParams{
		ChatID:              chatID,
		UserID:              userID,
		CanManageChat:       false,
		CanDeleteMessages:   false,
		CanManageVideoChats: false,
		CanRestrictMembers:  false,
		CanPromoteMembers:   false,
		CanChangeInfo:       false,
		CanInviteUsers:      false,
		CanPinMessages:      false,
	})
}

// ApproveJoinRequest approves a user's request to join a chat
func ApproveJoinRequest(ctx context.Context, chatID, userID int64) (bool, error) {
	return instance.ApproveChatJoinRequest(ctx, &bot.ApproveChatJoinRequestParams{
		ChatID: chatID,
		UserID: userID,
	})
}

// DeclineJoinRequest declines a user's request to join a chat
func DeclineJoinRequest(ctx context.Context, chatID, userID int64) (bool, error) {
	return instance.DeclineChatJoinRequest(ctx, &bot.DeclineChatJoinRequestParams{
		ChatID: chatID,
		UserID: userID,
	})
}
