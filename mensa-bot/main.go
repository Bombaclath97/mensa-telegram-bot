package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/Bombaclath97/bomba-go-utils/logger"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"git.bombaclath.cc/bombadurelli/mensa-bot-telegram/bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

var requestsToApprove = utils.RequestsToApprove{}
var conversationStateSaver = utils.ConversationStateSaver{}
var intermediateUserSaver = utils.IntermediateUserSaver{}
var lockedUsers = utils.LockedUsers{}

func main() {
	logger.Configure("mensa-bot")

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
	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, startHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/profilo", bot.MatchTypeExact, profileHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/approva", bot.MatchTypeExact, approveHandler)
	b.RegisterHandler(bot.HandlerTypeMessageText, "/app", bot.MatchTypeExact, appHandler)

	b.SetMyCommands(ctx, &bot.SetMyCommandsParams{
		Commands: []models.BotCommand{
			{Command: "start", Description: "Inizia la conversazione con il bot"},
			{Command: "profilo", Description: "Crea o visualizza il tuo profilo"},
			{Command: "approva", Description: "Richiedi l'approvazione delle richieste in sospeso"},
			{Command: "app", Description: "Tutte le informazioni sull'app ufficiale Mensa Italia"},
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

type callmeApi struct {
	Accepted bool `json:"accepted"`
}
