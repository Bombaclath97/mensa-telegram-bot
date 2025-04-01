package utils

import (
	"os"
	"strconv"
	"sync"
	"time"
)

// Ordine: MAIL MENSA, NUMERO DI TESSERA -> CERCA SE ESISTE -> CHIEDI NOME E COGNOME -> MANDA MAIL CON CODICE -> SE OK TUTTO SALVA SALVA
const (
	IDLE        = -1
	ASKED_EMAIL = iota
	ASKED_MEMBER_NUMBER
	AWAITING_APPROVAL
	ASKED_NAME
	ASKED_SURNAME
	ASKED_CONFIRMATION_CODE
	ASKED_DELETE_CONFIRMATION
	MANUAL_ASK_USER_ID
	MANUAL_ASK_EMAIL
	MANUAL_ASK_MEMBER_NUMBER
	MANUAL_ASK_NAME
	MANUAL_ASK_SURNAME
)

type ConversationStateSaver struct {
	states map[int64]int
	timers map[int64]*time.Timer
	mu     sync.Mutex
}

func NewConversationStateSaver() *ConversationStateSaver {
	return &ConversationStateSaver{
		states: make(map[int64]int),
		timers: make(map[int64]*time.Timer),
	}
}

func (c *ConversationStateSaver) SetState(userID int64, state int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Set the state
	c.states[userID] = state

	// Cancel any existing timer for the user
	if timer, exists := c.timers[userID]; exists {
		timer.Stop()
	}

	// Start a new timer to remove the state after N minutes
	timeoutMinutes, _ := strconv.Atoi(os.Getenv("CONVERSATION_TIMEOUT_MINUTES"))
	if timeoutMinutes == 0 {
		timeoutMinutes = 5 // Default to 5 minutes if not set
	}
	c.timers[userID] = time.AfterFunc(time.Duration(timeoutMinutes)*time.Minute, func() {
		c.RemoveState(userID)
	})
}

func (c *ConversationStateSaver) GetState(userID int64) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, ok := c.states[userID]
	if !ok {
		return IDLE
	}
	return state
}

func (c *ConversationStateSaver) RemoveState(userID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove the state
	delete(c.states, userID)

	// Stop and remove the timer
	if timer, exists := c.timers[userID]; exists {
		timer.Stop()
		delete(c.timers, userID)
	}
}
