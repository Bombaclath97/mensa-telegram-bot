package steps

import (
	"strings"

	"github.com/Bombaclath97/mensa-telegram-bot/convo"
)

type AskEmail struct{}

func (s *AskEmail) Handle(ctx *convo.ConvoContext) convo.Step {
	if ctx.Email == "" {
		if ctx.Input == "" {
			println("Insert your email:")
			return s
		}

		email := ctx.Input
		if !strings.HasSuffix(email, "@mensa.it") {
			println("Invalid email domain.")
			return &EndFailed{}
		}

		ctx.Email = email
	}

	return &AskMembership{}
}

func (s *AskEmail) GetAvailableStep() convo.AvailableSteps {
	return convo.AskEmail
}
