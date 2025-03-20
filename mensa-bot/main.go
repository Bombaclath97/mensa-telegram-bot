package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/Bombaclath97/bomba-go-utils/logger"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/tolgee"
	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

var requestsToApprove = utils.RequestsToApprove{}
var conversationStateSaver = utils.ConversationStateSaver{}
var intermediateUserSaver = utils.IntermediateUserSaver{}
var lockedUsers = utils.LockedUsers{}

var log = logger.Configure("mensa-bot")

func main() {

	log.Println("Loading Tolgee")

	tolgee.Load(os.Getenv("TOLGEE_API_KEY"))

	godotenv.Load()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	opts := []bot.Option{
		bot.WithDefaultHandler(onMessage),
	}

	b, err := bot.New(os.Getenv("BOT_TOKEN"), opts...)
	if err != nil {
		log.Fatalf("failed to create bot: %v", err)
	}

	b.RegisterHandlerMatchFunc(matchJoinRequest, onChatJoinRequest)
	b.RegisterHandlerMatchFunc(func(update *models.Update) bool {
		return matchBotJoinsGroup(update, b, ctx)
	}, onBotJoinsGroup)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, startHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/profilo", bot.MatchTypeExact, profileHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/approva", bot.MatchTypeExact, approveHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/app", bot.MatchTypeExact, appHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/elimina", bot.MatchTypeExact, deleteHandler)

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

func matchJoinRequest(update *models.Update) bool {
	return update.ChatJoinRequest != nil
}

func matchBotJoinsGroup(update *models.Update, b *bot.Bot, ctx context.Context) bool {
	botUser, _ := b.GetMe(ctx)
	if update.Message != nil && update.Message.NewChatMembers != nil {
		for _, user := range update.Message.NewChatMembers {
			if user.ID == botUser.ID {
				return true
			}
		}
	}
	return false
}

type callmeApi struct {
	Accepted bool `json:"accepted"`
}
