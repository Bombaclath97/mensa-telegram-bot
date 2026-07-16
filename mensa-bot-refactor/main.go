package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Bombaclath97/mensa-telegram-bot/cleanup"
	"github.com/Bombaclath97/mensa-telegram-bot/config"
	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/handlers"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	"github.com/Bombaclath97/mensa-telegram-bot/model"
	"github.com/Bombaclath97/mensa-telegram-bot/server"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
	"github.com/Bombaclath97/mensa-telegram-bot/tolgee"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
)

func main() {
	// Health-probe mode, for the container HEALTHCHECK. Handled before ANY other startup:
	// it must not load .env, resolve secrets, open the database or touch Telegram — it is a
	// throwaway process that makes one HTTP call to the real one and exits.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		os.Exit(runHealthcheck())
	}

	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		// Will use system environment variables
	}

	// Resolve Docker's "*_FILE" secret convention into plain env vars.
	//
	// MUST run before anything reads its configuration: Swarm delivers BOT_TOKEN,
	// API_BEARER_TOKEN and DATABASE_URL as FILES under /run/secrets/, and every reader in
	// this codebase uses os.Getenv. Doing it here means none of them have to care.
	//
	// After godotenv.Load, so a local .env can point at fixture files the same way.
	if err := config.ExpandFileSecrets(); err != nil {
		// Before logger.Init, so this cannot use the logger.
		fmt.Fprintf(os.Stderr, "FATAL: could not resolve file-backed secrets: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	logger.Init(os.Getenv("LOG_LEVEL"))

	// Initialize Tolgee translations.
	// Called UNCONDITIONALLY: the CDN and the on-disk cache tiers work with no API
	// key at all, so guarding this on TOLGEE_API_KEY would needlessly render raw keys.
	// A translation outage must never stop the bot, so this is a Warn, not a Fatal.
	if err := tolgee.Load(os.Getenv("TOLGEE_API_KEY")); err != nil {
		logger.Warn("Failed to load Tolgee translations, the bot will render raw keys",
			map[string]interface{}{"error": err.Error()})
	} else {
		logger.Info("Tolgee translations loaded successfully",
			map[string]interface{}{"languages": strings.Join(tolgee.Languages(), ",")})
	}

	// Initialize database
	if err := database.Init(); err != nil {
		logger.Fatal("Failed to initialize database", map[string]interface{}{"error": err.Error()})
	}
	defer database.Close()

	// Seed the bot administrators from ADMIN_TELEGRAM_IDS.
	// Without this, nobody can use /manual_add.
	seedBotAdmins()

	// Setup context with signal handling.
	// G22: os.Kill (SIGKILL) cannot be caught and SIGTERM — what `docker stop` sends —
	// must be, otherwise the deferred database.Close() never runs.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
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

	// Initialize bot singleton (every telegrambot.* helper depends on this)
	tb.Init(b)

	registerHandlers(b)

	// G14: publish the command menu (descriptions come from Tolgee, per language).
	publishCommands(ctx, b)

	// Inbound HTTP server: GET /health + POST /callme_api/{token} (the Mensa app
	// approval webhook). Run(ctx) blocks until ctx is cancelled, then shuts down.
	srv := server.New()
	go func() {
		if err := srv.Run(ctx); err != nil {
			logger.Error("HTTP server stopped with an error", map[string]interface{}{
				"error": err.Error(),
				"addr":  srv.Addr(),
			})
		}
	}()

	// Membership-expiry cleanup sweep. Ticker-driven (CLEANUP_INTERVAL_HOURS), NOT
	// exposed over HTTP. Start blocks until ctx is cancelled, hence the goroutine.
	go cleanup.Start(ctx)

	logger.Info("Bot started successfully", map[string]interface{}{"http_addr": srv.Addr()})

	// Blocks until ctx is cancelled by SIGINT/SIGTERM; the server and cleanup
	// goroutines share the same ctx and stop with it.
	b.Start(ctx)

	logger.Info("Shutting down")
}

// seedBotAdmins parses ADMIN_TELEGRAM_IDS and flags those users is_bot_admin=true.
// The seeded set is also consulted by UserRepository.IsBotAdmin for users that are
// not (yet) registered, and re-applied by the User BeforeCreate hook if such a user
// registers later.
func seedBotAdmins() {
	raw := os.Getenv("ADMIN_TELEGRAM_IDS")
	if strings.TrimSpace(raw) == "" {
		logger.Warn("ADMIN_TELEGRAM_IDS is not set: /manual_add will be unavailable to everyone")
		return
	}

	ids, err := database.ParseAdminIDs(raw)
	if err != nil {
		logger.Fatal("Invalid ADMIN_TELEGRAM_IDS", map[string]interface{}{"error": err.Error()})
	}

	if err := database.SeedBotAdmins(ids); err != nil {
		logger.Fatal("Failed to seed bot admins", map[string]interface{}{"error": err.Error()})
	}

	logger.Info("Bot admins seeded", map[string]interface{}{"count": len(ids)})
}

// registerHandlers wires every Telegram handler the bot exposes.
//
// Match-func handlers are registered first; go-telegram dispatches to the FIRST
// matching handler only, and these predicates are disjoint from the command /
// callback ones (they fire on ChatJoinRequest / MyChatMember updates, which carry
// no Message and no CallbackQuery).
func registerHandlers(b *bot.Bot) {
	// Update-type handlers.
	// NOTE: exactly ONE registration for MyChatMember. A bot added to a group
	// directly AS an administrator satisfies both "joined" and "promoted"; two
	// separate match funcs would silently drop one branch, so the single handler
	// dispatches to both internally.
	b.RegisterHandlerMatchFunc(handlers.MatchChatJoinRequest, handlers.ChatJoinRequestHandler)
	b.RegisterHandlerMatchFunc(handlers.MatchMyChatMember, handlers.MyChatMemberHandler)

	// Commands
	b.RegisterHandler(bot.HandlerTypeMessageText, "start", bot.MatchTypeCommand, handlers.StartCommandHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "profilo", bot.MatchTypeCommand, handlers.ProfileCommandHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "approva", bot.MatchTypeCommand, handlers.ApproveCommandHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "app", bot.MatchTypeCommand, handlers.AppCommandHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "elimina", bot.MatchTypeCommand, handlers.DeleteCommandHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "lang", bot.MatchTypeCommand, handlers.LangCommandHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "cancel", bot.MatchTypeCommand, handlers.CancelCommandHandler)
	// Admin-only, deliberately NOT published via SetMyCommands.
	b.RegisterHandler(bot.HandlerTypeMessageText, "manual_add", bot.MatchTypeCommand, handlers.ManualAddCommandHandler)

	// Callbacks
	// NOTE: "pcard:" must be registered BEFORE "profile:" only if the prefixes overlapped —
	// they do not (pcard: vs profile:), so order is irrelevant here.
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "pcard:", bot.MatchTypePrefix, handlers.ProfileCardCallbackHandler)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "profile:", bot.MatchTypePrefix, handlers.ProfileCallbackHandler)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "delete:", bot.MatchTypePrefix, handlers.DeleteCallbackHandler)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "lang:", bot.MatchTypePrefix, handlers.LangCallbackHandler)
}

// botCommands is the published private-chat command menu (G14).
// /manual_add is deliberately absent: it is admin-only and its existence must not
// be advertised. The key is the Tolgee key holding the localized description.
var botCommands = []struct {
	Command string
	Key     string
}{
	{"start", "telegrambot.commands.start"},
	{"profilo", "telegrambot.commands.profilo"},
	{"approva", "telegrambot.commands.approva"},
	{"app", "telegrambot.commands.app"},
	{"elimina", "telegrambot.commands.elimina"},
	{"lang", "telegrambot.commands.lang"},
	{"cancel", "telegrambot.commands.cancel"},
}

// publishCommands calls SetMyCommands once with no language code (the fallback menu,
// rendered in the Tolgee base language) and once per loaded language, so a German
// user sees German descriptions rather than hardcoded Italian.
func publishCommands(ctx context.Context, b *bot.Bot) {
	setCommands := func(lang string, languageCode string) {
		cmds := make([]models.BotCommand, 0, len(botCommands))
		for _, c := range botCommands {
			cmds = append(cmds, models.BotCommand{
				Command:     c.Command,
				Description: tolgee.T(c.Key, lang),
			})
		}

		_, err := b.SetMyCommands(ctx, &bot.SetMyCommandsParams{
			Commands:     cmds,
			Scope:        &models.BotCommandScopeAllPrivateChats{},
			LanguageCode: languageCode,
		})
		if err != nil {
			logger.Warn("Failed to publish the command menu", map[string]interface{}{
				"error":         err.Error(),
				"language_code": languageCode,
			})
		}
	}

	// Default menu (no language code): the base language.
	setCommands(tolgee.BaseLanguage(), "")

	// Per-language menus. Telegram's language_code must be a two-letter ISO 639-1 code —
	// it rejects anything else ("invalid language code specified"), so a regional Tolgee
	// tag like zh-Hans or pt-BR would fail the call. Skip those rather than log an error
	// on every boot: the base-language menu above already covers their speakers.
	published := 0
	for _, lang := range tolgee.Languages() {
		if !isISO639_1(lang) {
			logger.Debug("Skipping command menu for a non-ISO-639-1 language tag",
				map[string]interface{}{"language_code": lang})
			continue
		}
		setCommands(lang, lang)
		published++
	}

	logger.Info("Command menu published", map[string]interface{}{
		"commands":  len(botCommands),
		"languages": published,
	})
}

// isISO639_1 reports whether tag is a bare two-letter language code, which is the only
// thing Telegram's SetMyCommands accepts as a language_code.
func isISO639_1(tag string) bool {
	if len(tag) != 2 {
		return false
	}
	for _, r := range tag {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// defaultHandler handles all messages that don't match a specific command.
//
// Free-text routing order (deliberate, see task G13/G02):
//  1. LOCKED (awaiting the Mensa-app approval) -> ignore entirely (legacy behavior);
//  2. an admin's /manual_add wizard step;
//  3. the profile registration wizard step;
//  4. a forwarded message -> sender lookup.
func defaultHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// Only respond in private chats
	if update.Message == nil || update.Message.Chat.Type != "private" {
		return
	}

	if update.Message.Text != "" {
		// Commands are routed by their own handlers (notably /cancel, the escape
		// hatch out of a locked or stuck draft), so never consume them here.
		if strings.HasPrefix(update.Message.Text, "/") {
			return
		}

		userID := int64(0)
		if update.Message.From != nil {
			userID = update.Message.From.ID
		}

		// 1. AWAITING_APPROVAL: swallow free text while the Mensa app decides.
		if userID != 0 && model.Drafts.IsLocked(userID) {
			logger.Debug("Ignoring message from a locked user (awaiting Mensa app approval)",
				map[string]interface{}{"user_id": userID})
			return
		}

		// 2. Admin /manual_add wizard. Returns false when the acting user has no
		//    manual-add draft, so it cannot steal registration input.
		if handlers.ManualAddMessageHandler(ctx, b, update) {
			return
		}

		// 3. Profile registration wizard. Returns false when there is no live draft
		//    or no armed field.
		if handlers.ProfileMessageHandler(ctx, b, update) {
			return
		}
	}

	// 4. Forwarded messages: check whether the original sender is registered.
	if update.Message.ForwardOrigin != nil {
		handleForwardedMessage(ctx, update)
	}
}

// handleForwardedMessage checks if a forwarded message's sender is registered.
//
// G21: a lookup error is NOT "not registered" — surface it, never silently degrade.
func handleForwardedMessage(ctx context.Context, update *models.Update) {
	userID := update.Message.From.ID
	chatID := update.Message.Chat.ID

	userRepo := database.NewUserRepository()
	lang := userRepo.GetLanguage(userID)

	// Check if the forwarder is registered
	forwarder, err := userRepo.GetByID(userID)
	if err != nil {
		logger.Error("Failed to look up the forwarding user", map[string]interface{}{
			"error":   err.Error(),
			"user_id": userID,
		})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}
	if forwarder == nil {
		return // Only registered users can check others
	}

	lang = forwarder.GetLanguage()
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
	sender, err := userRepo.GetByID(senderID)
	if err != nil {
		logger.Error("Failed to look up the forwarded sender", map[string]interface{}{
			"error":     err.Error(),
			"sender_id": senderID,
		})
		tb.Send(ctx, chatID, tolgee.T("telegrambot.general.error", lang))
		return
	}

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
