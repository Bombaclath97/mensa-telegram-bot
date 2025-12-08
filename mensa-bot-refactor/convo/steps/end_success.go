package steps

import (
	"github.com/Bombaclath97/mensa-telegram-bot/convo"
)

type EndSuccess struct{}

func (s *EndSuccess) Handle(ctx *convo.ConvoContext) convo.Step {
	println("Registration successful!") // Tolgee string
	// Clear the context after success
	convo.ClearConvoContext(ctx.UserID)
	return nil
}

func (s *EndSuccess) GetAvailableStep() convo.AvailableSteps {
	return convo.EndSuccess
}
