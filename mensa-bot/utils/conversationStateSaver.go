package utils

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

type ConversationStateSaver map[int64]int

func (c *ConversationStateSaver) SetState(userID int64, state int) {
	if *c == nil {
		*c = make(map[int64]int)
	}
	(*c)[userID] = state
}

func (c *ConversationStateSaver) GetState(userID int64) int {
	if *c == nil {
		return IDLE
	}
	state, ok := (*c)[userID]
	if !ok {
		return IDLE
	}
	return state
}

func (c *ConversationStateSaver) RemoveState(userID int64) {
	if *c != nil {
		delete(*c, userID)
	}
}
