package steps

import (
	"github.com/Bombaclath97/mensa-telegram-bot/convo"
)

type EndCancelled struct{}

func (s *EndCancelled) Handle(ctx *convo.ConvoContext) convo.Step {
	println("Registration cancelled.") // Tolgee string
	// Clear the context
	convo.ClearConvoContext(ctx.UserID)
	return nil
}

func (s *EndCancelled) GetAvailableStep() convo.AvailableSteps {
	return convo.EndCancelled
}
