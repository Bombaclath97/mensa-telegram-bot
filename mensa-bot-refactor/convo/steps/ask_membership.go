package steps

import (
	"github.com/Bombaclath97/mensa-telegram-bot/convo"
	restclient "github.com/Bombaclath97/mensa-telegram-bot/rest_client"
)

type AskMembership struct{}

func (s *AskMembership) Handle(ctx *convo.ConvoContext) convo.Step {
	if ctx.Membership == "" {
		if ctx.Input == "" {
			println("Insert your membership number:") // Tolgee string to telegram send
			return s
		}

		ctx.Membership = ctx.Input
	}

	is_member, callback_url := restclient.CheckIfIsMemberAndSendCallmeURL(ctx.Email, ctx.Membership, ctx.UserID)

	if !is_member {
		return &EndFailed{}
	}

	println("Check mensa app for notification") // Tolgee string to telegram send
	ctx.CallbackUrl = callback_url
	ctx.LockedInteraction = true
	return &AskFullName{}
}

func (s *AskMembership) GetAvailableStep() convo.AvailableSteps {
	return convo.AskMembership
}
