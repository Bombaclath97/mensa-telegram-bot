package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/joho/godotenv"

	"git.bombaclath.cc/bombaclath97/mensa-bot-telegram/bot/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

var requestsToApprove = utils.RequestsToApprove{}
var conversationStateSaver = utils.ConversationStateSaver{}
var intermediateUserSaver = utils.IntermediateUserSaver{}

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
	b.RegisterHandler(bot.HandlerTypeMessageText, "/approve", bot.MatchTypeExact, approveHandler)

	b.Start(ctx)
}

func matchJoinRequest(update *models.Update) bool {
	return update.ChatJoinRequest != nil
}
