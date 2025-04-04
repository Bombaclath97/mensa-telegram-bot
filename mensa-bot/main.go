package main

import (
	"context"
	"os"
	"os/signal"
	"strings"

	"github.com/Bombaclath97/bomba-go-utils/logger"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/tolgee"
	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

var requestsToApprove = utils.RequestsToApprove{}
var intermediateUserSaver = utils.IntermediateUserSaver{}
var lockedUsers = utils.LockedUsers{}
var conversationStateSaver = utils.NewConversationStateSaver()

var log = logger.Configure("mensa-bot")

func main() {

	log.Println("Loading Tolgee")

	tolgee.Load(os.Getenv("TOLGEE_API_KEY"))

	godotenv.Load()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	b, err := bot.New(os.Getenv("BOT_TOKEN"), bot.Option(bot.WithDefaultHandler(noopHandler)))
	if err != nil {
		log.Fatalf("failed to create bot: %v", err)
	}

	b.RegisterHandlerMatchFunc(matchJoinRequest, onChatJoinRequest)
	b.RegisterHandlerMatchFunc(matchBotJoinsGroup, onBotJoinsGroup)
	b.RegisterHandlerMatchFunc(matchBotBecomesAdmin, onBotBecomesAdmin)

	// Publicly available commands
	b.RegisterHandlerMatchFunc(matchMessageReceivedInChat, onMessage)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, startHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/profilo", bot.MatchTypeExact, profileHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/approva", bot.MatchTypeExact, approveHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/app", bot.MatchTypeExact, appHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/elimina", bot.MatchTypeExact, deleteHandler)

	// Administration panel
	b.RegisterHandler(bot.HandlerTypeMessageText, "/manual_add", bot.MatchTypeExact, manualAddHandler)

	b.SetMyCommands(ctx, &bot.SetMyCommandsParams{
		Commands: []models.BotCommand{
			{Command: "start", Description: "Inizia la conversazione con il bot"},
			{Command: "profilo", Description: "Crea o visualizza il tuo profilo"},
			{Command: "approva", Description: "Richiedi l'approvazione delle richieste in sospeso"},
			{Command: "app", Description: "Tutte le informazioni sull'app ufficiale Mensa Italia"},
			{Command: "elimina", Description: "Elimina il tuo profilo"},
		},
		Scope: &models.BotCommandScopeAllPrivateChats{},
	})

	go func() {
		log.Println("Starting telegram bot")
		b.Start(ctx)
	}()

	r := gin.Default()

	r.POST("/callme_api/:token", func(ctx *gin.Context) {
		postCallmeAPI(ctx, b)
	})
	r.GET("/cleanup_routine", func(ctx *gin.Context) {
		cleanupRoutine(ctx, b)
	})

	certPath := os.Getenv("CERT_PATH")
	keyPath := os.Getenv("KEY_PATH")

	go func() {
		log.Printf("INFO: Starting server on port 8080 with cert %s and key %s\n", certPath, keyPath)
		if err := r.RunTLS(":8080", certPath, keyPath); err != nil {
			log.Fatalf("failed to run server: %v", err)
		}
	}()

	// Wait for the context to be cancelled
	<-ctx.Done()
	log.Println("Shutting down gracefully, press Ctrl+C again to force")
}

func noopHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	// No operation handler, does nothing
}

func matchJoinRequest(update *models.Update) bool {
	return update.ChatJoinRequest != nil
}

func matchMessageReceivedInChat(update *models.Update) bool {
	return update.Message != nil &&
		update.Message.Chat.Type == models.ChatTypePrivate &&
		(!strings.HasPrefix(update.Message.Text, "/") || update.Message.Text == "/cancel")
}

func matchBotJoinsGroup(update *models.Update) bool {
	return update.MyChatMember != nil && update.MyChatMember.NewChatMember == models.ChatMember{}
}

func matchBotBecomesAdmin(update *models.Update) bool {
	return update.MyChatMember != nil && update.MyChatMember.NewChatMember.Administrator.Status == models.ChatMemberTypeAdministrator
}

type callmeApi struct {
	Accepted bool `json:"accepted"`
}
