package convo

import (
	"time"

	"gorm.io/gorm"
)

type ConvoContext struct {
	gorm.Model
	UserID            int64
	Email             string
	Membership        string
	FullName          string
	CurrentStep       AvailableSteps
	LastInteraction   time.Time
	Input             string
	CallbackUrl       string
	LockedInteraction bool
}

func (c *ConvoContext) SetInput(input string) {
	c.Input = input
	c.LastInteraction = time.Now()
}
