package main

import (
	"context"
	"os"
	"os/signal"
	"strings"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/handlers"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
	"github.com/Bombaclath97/mensa-telegram-bot/tolgee"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		// Will use system environment variables
	}

	// Initialize logger
	logger.Init(os.Getenv("LOG_LEVEL"))

	// Initialize Tolgee translations
	tolgeeKey := os.Getenv("TOLGEE_API_KEY")
	if tolgeeKey != "" {
		if err := tolgee.Load(tolgeeKey); err != nil {
			logger.Warn("Failed to load Tolgee translations", map[string]interface{}{"error": err.Error()})
		} else {
			logger.Info("Tolgee translations loaded successfully")
		}
	} else {
		logger.Warn("TOLGEE_API_KEY not set, translations will show keys")
	}

	// Initialize database
	if err := database.Init(); err != nil {
		logger.Fatal("Failed to initialize database", map[string]interface{}{"error": err.Error()})
	}
	defer database.Close()

	// Setup context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	// Get bot token from environment
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		logger.Fatal("BOT_TOKEN environment variable is required")
	}

	// Create bot with default message handler
	botOpts := []bot.Option{
		bot.WithDefaultHandler(defaultHandler),
	}

	b, err := bot.New(token, botOpts...)
	if err != nil {
		logger.Fatal("Couldn't initialize bot", map[string]interface{}{"error": err.Error()})
	}

	// Initialize bot singleton
	tb.Init(b)

	// Register command handlers
	b.RegisterHandler(bot.HandlerTypeMessageText, "start", bot.MatchTypeCommand, handlers.StartCommandHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "profilo", bot.MatchTypeCommand, handlers.ProfileCommandHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "elimina", bot.MatchTypeCommand, handlers.DeleteCommandHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "app", bot.MatchTypeCommand, handlers.AppCommandHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "lang", bot.MatchTypeCommand, handlers.LangCommandHandler)

	// Register callback handlers
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "profile:", bot.MatchTypePrefix, handlers.ProfileCallbackHandler)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "delete:", bot.MatchTypePrefix, handlers.DeleteCallbackHandler)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "lang:", bot.MatchTypePrefix, handlers.LangCallbackHandler)

	logger.Info("Bot started successfully")
	b.Start(ctx)
}

// defaultHandler handles all messages that don't match a specific command
func defaultHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// Only respond in private chats
	if update.Message == nil || update.Message.Chat.Type != "private" {
		return
	}

	// Handle text messages during profile registration
	if update.Message.Text != "" {
		// Skip if it's a command
		if strings.HasPrefix(update.Message.Text, "/") {
			return
		}

		// Try to handle as profile field input
		if handlers.ProfileMessageHandler(ctx, b, update) {
			return
		}
	}

	// Handle forwarded messages to check if sender is registered
	if update.Message.ForwardOrigin != nil {
		handleForwardedMessage(ctx, update)
	}
}

// handleForwardedMessage checks if a forwarded message's sender is registered
func handleForwardedMessage(ctx context.Context, update *models.Update) {
	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	// Check if the forwarder is registered
	userRepo := database.NewUserRepository()
	forwarder, _ := userRepo.GetByID(userID)
	if forwarder == nil {
		return // Only registered users can check others
	}

	lang := forwarder.GetLanguage()
	origin := update.Message.ForwardOrigin

	// Handle hidden user
	if origin.Type == models.MessageOriginTypeHiddenUser {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.forwardedhidden", lang))
		return
	}

	// Get the original sender
	if origin.MessageOriginUser == nil {
		return
	}

	senderID := origin.MessageOriginUser.SenderUser.ID
	sender, _ := userRepo.GetByID(senderID)

	if sender != nil {
		params := map[string]string{
			"name":     sender.FirstName,
			"lastName": sender.LastName,
		}
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.forwardedregistered", lang, params))
	} else {
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.forwardednotregistered", lang))
	}
}
