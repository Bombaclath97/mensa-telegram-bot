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

// ErrNotInitialized is returned by every helper when Init() has not run yet.
var ErrNotInitialized = errors.New("bot not initialized: call telegrambot.Init() first")

// Init initializes the bot singleton. Must be called once at startup.
func Init(b *bot.Bot) {
	once.Do(func() {
		instance = b
	})
}

// Get returns the bot instance, or ErrNotInitialized.
func Get() (*bot.Bot, error) {
	if instance == nil {
		return nil, ErrNotInitialized
	}
	return instance, nil
}

// MustGet returns the bot instance or panics if not initialized.
func MustGet() *bot.Bot {
	if instance == nil {
		panic(ErrNotInitialized)
	}
	return instance
}

// Every message the bot sends is parsed as HTML.
//
// The translations are authored with formatting (bold, <code>, links), which rendered as
// literal markup for as long as no ParseMode was set. HTML is used rather than Markdown
// because interpolated values — display names, group titles, typed input — are untrusted:
// HTML has a reliable escape (tolgee.EscapeHTML, applied to every substituted value),
// whereas Markdown v1 has none, so a member called "anna_b" would break the parse and
// Telegram would reject the whole message.
const parseMode = models.ParseModeHTML

// Send sends a text message to the specified chat.
func Send(ctx context.Context, chatID int64, message string) (*models.Message, error) {
	b, err := Get()
	if err != nil {
		return nil, err
	}
	return b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      message,
		ParseMode: parseMode,
	})
}

// SendWithKeyboard sends a message with an inline keyboard.
func SendWithKeyboard(ctx context.Context, chatID int64, message string, keyboard *models.InlineKeyboardMarkup) (*models.Message, error) {
	b, err := Get()
	if err != nil {
		return nil, err
	}
	return b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        message,
		ParseMode:   parseMode,
		ReplyMarkup: keyboard,
	})
}

// EditMessageText edits an existing message's text.
func EditMessageText(ctx context.Context, chatID int64, messageID int, text string) (*models.Message, error) {
	b, err := Get()
	if err != nil {
		return nil, err
	}
	return b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		Text:      text,
		ParseMode: parseMode,
	})
}

// EditMessageKeyboard edits an existing message's inline keyboard
func EditMessageKeyboard(ctx context.Context, chatID int64, messageID int, keyboard *models.InlineKeyboardMarkup) (*models.Message, error) {
	b, err := Get()
	if err != nil {
		return nil, err
	}
	return b.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{
		ChatID:      chatID,
		MessageID:   messageID,
		ReplyMarkup: keyboard,
	})
}

// EditMessageWithKeyboard edits an existing message's text and inline keyboard
func EditMessageWithKeyboard(ctx context.Context, chatID int64, messageID int, text string, keyboard *models.InlineKeyboardMarkup) (*models.Message, error) {
	b, err := Get()
	if err != nil {
		return nil, err
	}
	return b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   messageID,
		Text:        text,
		ParseMode:   parseMode,
		ReplyMarkup: keyboard,
	})
}

// DeleteMessage deletes a message
func DeleteMessage(ctx context.Context, chatID int64, messageID int) (bool, error) {
	b, err := Get()
	if err != nil {
		return false, err
	}
	return b.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    chatID,
		MessageID: messageID,
	})
}

// AnswerCallback answers a callback query
func AnswerCallback(ctx context.Context, callbackQueryID string, text string) (bool, error) {
	b, err := Get()
	if err != nil {
		return false, err
	}
	return b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: callbackQueryID,
		Text:            text,
	})
}

// AnswerCallbackSilent answers a callback query without showing any message
func AnswerCallbackSilent(ctx context.Context, callbackQueryID string) (bool, error) {
	b, err := Get()
	if err != nil {
		return false, err
	}
	return b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: callbackQueryID,
	})
}

// GetChat retrieves chat information
func GetChat(ctx context.Context, chatID int64) (*models.ChatFullInfo, error) {
	b, err := Get()
	if err != nil {
		return nil, err
	}
	return b.GetChat(ctx, &bot.GetChatParams{
		ChatID: chatID,
	})
}

// GetChatMember retrieves information about a chat member
func GetChatMember(ctx context.Context, chatID, userID int64) (*models.ChatMember, error) {
	b, err := Get()
	if err != nil {
		return nil, err
	}
	return b.GetChatMember(ctx, &bot.GetChatMemberParams{
		ChatID: chatID,
		UserID: userID,
	})
}

// GetChatAdministrators lists the administrators of a chat (creator included, bots included).
func GetChatAdministrators(ctx context.Context, chatID int64) ([]models.ChatMember, error) {
	b, err := Get()
	if err != nil {
		return nil, err
	}
	return b.GetChatAdministrators(ctx, &bot.GetChatAdministratorsParams{
		ChatID: chatID,
	})
}

// KickUser kicks a user from a group: ban then immediately unban, so they can re-join
// later. A bare ban would be a permaban.
func KickUser(ctx context.Context, chatID, userID int64) error {
	b, err := Get()
	if err != nil {
		return err
	}

	if _, err := b.BanChatMember(ctx, &bot.BanChatMemberParams{
		ChatID: chatID,
		UserID: userID,
	}); err != nil {
		return err
	}

	_, err = b.UnbanChatMember(ctx, &bot.UnbanChatMemberParams{
		ChatID:       chatID,
		UserID:       userID,
		OnlyIfBanned: true,
	})
	return err
}

// DemoteUser removes every admin privilege from a user in a group.
func DemoteUser(ctx context.Context, chatID, userID int64) (bool, error) {
	b, err := Get()
	if err != nil {
		return false, err
	}
	return b.PromoteChatMember(ctx, &bot.PromoteChatMemberParams{
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
		CanPostMessages:     false,
		CanEditMessages:     false,
		CanPostStories:      false,
		CanEditStories:      false,
		CanDeleteStories:    false,
		CanManageTopics:     false,
	})
}

// ApproveJoinRequest approves a user's request to join a chat.
func ApproveJoinRequest(ctx context.Context, chatID, userID int64) (bool, error) {
	b, err := Get()
	if err != nil {
		return false, err
	}
	return b.ApproveChatJoinRequest(ctx, &bot.ApproveChatJoinRequestParams{
		ChatID: chatID,
		UserID: userID,
	})
}

// DeclineJoinRequest declines a user's request to join a chat.
func DeclineJoinRequest(ctx context.Context, chatID, userID int64) (bool, error) {
	b, err := Get()
	if err != nil {
		return false, err
	}
	return b.DeclineChatJoinRequest(ctx, &bot.DeclineChatJoinRequestParams{
		ChatID: chatID,
		UserID: userID,
	})
}
