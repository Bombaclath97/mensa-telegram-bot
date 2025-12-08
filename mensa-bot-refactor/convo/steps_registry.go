package convo

import "github.com/Bombaclath97/mensa-telegram-bot/registries"

type AvailableSteps int

const (
	AskEmail AvailableSteps = iota
	AskMembership
	AskFullName
	ConfirmProfile
	EndSuccess
	EndFailed
	EndCancelled
	EndTimeout
)

func SpawnStep(s AvailableSteps) Step {
	if f, ok := registries.Registry[s]; ok {
		return f()
	}
	return nil
}
