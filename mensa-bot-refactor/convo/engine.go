package convo

import (
	"time"

	"github.com/Bombaclath97/bomba-go-utils/env"
)

type Store interface {
	Load(userID int64) (*ConvoContext, error)
	Save(ctx *ConvoContext) error
	Clear(userID int64) error
}

type Engine struct {
	conversationTTL time.Duration
}

func InitEngine() *Engine {
	minutes, _ := env.GetIntEnvOrDefault("CONVERSATION_TTL", 30)
	return &Engine{
		conversationTTL: time.Duration(minutes) * time.Minute,
	}
}

func (e *Engine) StartConvo(userID int64) error {
	ctx := &ConvoContext{
		UserID:          userID,
		CurrentStep:     AskEmail,
		LastInteraction: time.Now(),
	}
	return SaveConvoContext(ctx)
}

func (e *Engine) Process(userID int64, input string) {
	ctx, _ := GetConvoContext(userID)
	if ctx == nil {
		return
	}

	// timeout
	if time.Since(ctx.LastInteraction) >= e.conversationTTL {
		ctx.CurrentStep = EndTimeout
		SaveConvoContext(ctx)
		return
	}

	// cancel
	if input == "/cancel" {
		ctx.CurrentStep = EndCancelled
		SaveConvoContext(ctx)
		return
	}

	ctx.SetInput(input)

	step := SpawnStep(ctx.CurrentStep)
	if step == nil {
		return
	}

	nextStep := step.Handle(ctx)
	if nextStep == nil {
		return
	}

	ctx.CurrentStep = nextStep.GetAvailableStep()
	SaveConvoContext(ctx)
}
