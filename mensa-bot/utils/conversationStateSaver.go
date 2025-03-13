package utils

// Ordine: MAIL MENSA, NUMERO DI TESSERA -> CERCA SE ESISTE -> CHIEDI NOME E COGNOME -> MANDA MAIL CON CODICE -> SE OK TUTTO SALVA SALVA
const (
	ASKED_EMAIL = iota
	ASKED_MEMBER_NUMBER
	AWAITING_APPROVAL
	ASKED_NAME
	ASKED_SURNAME
	ASKED_CONFIRMATION_CODE
)

type ConversationStateSaver map[int64]int

func (c *ConversationStateSaver) SetState(userID int64, state int) {
	(*c)[userID] = state
}

func (c *ConversationStateSaver) GetState(userID int64) int {
	state, ok := (*c)[userID]
	if !ok {
		return -1
	}
	return state
}

func (c *ConversationStateSaver) RemoveState(userID int64) {
	delete(*c, userID)
}
