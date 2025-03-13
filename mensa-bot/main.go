package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"

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
		b.Start(ctx)
	}()

	r := gin.Default()
	r.GET("/callme_api/:token", func(c *gin.Context) {
		fullToken := c.Param("token")
		userId := strings.Split(fullToken, "_")[0]
		userIdInt, _ := strconv.ParseInt(userId, 10, 64)

		if lockedUsers.UnlockUser(userIdInt, fullToken) {
			conversationStateSaver.SetState(userIdInt, utils.ASKED_NAME)
			b.SendMessage(ctx, &bot.SendMessageParams{
				ParseMode: "Markdown",
				ChatID:    userIdInt,
				Text:      "Richiesta approvata, grazie! Puoi per favore scrivermi il tuo nome ora?",
			})
			c.JSON(200, gin.H{"message": "Utente sbloccato"})
		} else {
			c.JSON(400, gin.H{"message": "Errore"})
		}
	})

	certPath := os.Getenv("CERT_PATH")
	keyPath := os.Getenv("KEY_PATH")

	go func() {
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
