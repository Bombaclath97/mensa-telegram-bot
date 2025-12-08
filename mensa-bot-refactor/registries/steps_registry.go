package registries

import (
	"github.com/Bombaclath97/mensa-telegram-bot/convo"
	"github.com/Bombaclath97/mensa-telegram-bot/convo/steps"
)

var Registry = map[convo.AvailableSteps]func() convo.Step{
	convo.AskEmail:       func() convo.Step { return &steps.AskEmailStep{} },
	convo.AskMembership:  func() convo.Step { return &steps.AskMembershipStep{} },
	convo.AskFullName:    func() convo.Step { return &steps.AskFullNameStep{} },
	convo.ConfirmProfile: func() convo.Step { return &steps.ConfirmProfileStep{} },
	convo.EndSuccess:     func() convo.Step { return &steps.EndSuccessStep{} },
	convo.EndFailed:      func() convo.Step { return &steps.EndFailedStep{} },
	convo.EndCancelled:   func() convo.Step { return &steps.EndCancelledStep{} },
	convo.EndTimeout:     func() convo.Step { return &steps.EndTimeoutStep{} },
}
