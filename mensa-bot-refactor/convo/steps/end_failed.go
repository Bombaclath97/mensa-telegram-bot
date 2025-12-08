package steps

import "github.com/Bombaclath97/mensa-telegram-bot/convo"

type EndFailed struct{}

func (s *EndFailed) Handle(ctx *convo.ConvoContext) convo.Step {
	println("Profile validation failed.") // Tolgee string to telegram send
	return nil
}

func (s *EndFailed) GetAvailableStep() convo.AvailableSteps {
	return convo.EndFailed // Useless as this is an ending step
}
