package steps

import (
	"github.com/Bombaclath97/mensa-telegram-bot/convo"
)

type EndTimeout struct{}

func (s *EndTimeout) Handle(ctx *convo.ConvoContext) convo.Step {
	println("Registration timed out.") // Tolgee string
	// Clear the context
	convo.ClearConvoContext(ctx.UserID)
	return nil
}

func (s *EndTimeout) GetAvailableStep() convo.AvailableSteps {
	return convo.EndTimeout
}
