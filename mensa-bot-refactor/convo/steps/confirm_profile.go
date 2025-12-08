package steps

import (
	"github.com/Bombaclath97/mensa-telegram-bot/convo"
)

type ConfirmProfile struct{}

func (s *ConfirmProfile) Handle(ctx *convo.ConvoContext) convo.Step {
	if ctx.Input == "" {
		println("Confirm your profile - Email: " + ctx.Email + ", Membership: " + ctx.Membership + ", Name: " + ctx.FullName) // Tolgee string
		println("Reply 'yes' to confirm or 'no' to cancel")                                                                   // Tolgee string
		return s
	}

	if ctx.Input == "yes" {
		return &EndSuccess{}
	} else {
		return &EndFailed{}
	}
}

func (s *ConfirmProfile) GetAvailableStep() convo.AvailableSteps {
	return convo.ConfirmProfile
}
