package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"

	"git.bombaclath.cc/bombaclath97/mensa-bot-telegram/utils"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

var requestsToApprove = utils.RequestsToApprove{}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	opts := []bot.Option{}

	b, err := bot.New("6853027606:AAE4Hjn_E1OrVwhhEMYYvZxR3-w0p_8H0i4", opts...)
	if err != nil {
		log.Fatalf("failed to create bot: %v", err)
	}

	b.RegisterHandlerMatchFunc(matchJoinRequest, onChatJoinRequest)

	b.Start(ctx)
}

func matchJoinRequest(update *models.Update) bool {
	return update.ChatJoinRequest != nil
}

func onChatJoinRequest(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatId := update.ChatJoinRequest.Chat.ID
	userId := update.ChatJoinRequest.From.ID
	firstName := update.ChatJoinRequest.From.FirstName

	//Check if user has a bot profile registered
	if !utils.IsMemberRegistered(userId) {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: userId,
			Text:   fmt.Sprintf("Ciao %s! Non risulti ancora registrato", firstName),
		})
		requestsToApprove.AddRequest(userId, chatId)
	}
}
