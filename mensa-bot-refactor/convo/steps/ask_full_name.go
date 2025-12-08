package steps

import (
	"github.com/Bombaclath97/mensa-telegram-bot/convo"
)

type AskFullName struct{}

func (s *AskFullName) Handle(ctx *convo.ConvoContext) convo.Step {
	if ctx.FullName == "" {
		if ctx.Input == "" {
			println("Insert your full name:") // Tolgee string to telegram send
			return s
		}

		ctx.FullName = ctx.Input
	}

	return &ConfirmProfile{}
}

func (s *AskFullName) GetAvailableStep() convo.AvailableSteps {
	return convo.AskFullName
}
