package utils

import "git.bombaclath.cc/bombaclath97/mensa-bot-telegram/bot/model"

type RequestsToApprove map[int64][]int64

func (r *RequestsToApprove) AddRequest(userID, chatID int64) {
	(*r)[userID] = append((*r)[userID], chatID)
}

func (r *RequestsToApprove) RemoveRequests(userID int64) {
	delete(*r, userID)
}

func (r *RequestsToApprove) GetRequests(userID int64) ([]int64, bool) {
	chatID, ok := (*r)[userID]
	return chatID, ok
}

// Ordine: MAIL MENSA, NUMERO DI TESSERA -> CERCA SE ESISTE -> CHIEDI NOME E COGNOME -> MANDA MAIL CON CODICE -> SE OK TUTTO SALVA SALVA
const (
	ASKED_EMAIL = iota
	ASKED_MEMBER_NUMBER
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

type intermediateUser struct {
	firstName        string
	lastName         string
	email            string
	confirmationCode string
	memberNumber     int64
}

type IntermediateUserSaver map[int64]intermediateUser

func (i *IntermediateUserSaver) SetMemberNumber(userID int64, memberNumber int64) {
	user, exists := (*i)[userID]
	if !exists {
		user = intermediateUser{}
	}
	user.memberNumber = memberNumber
	(*i)[userID] = user
}

func (i *IntermediateUserSaver) SetFirstName(userID int64, firstName string) {
	user, exists := (*i)[userID]
	if !exists {
		user = intermediateUser{}
	}
	user.firstName = firstName
	(*i)[userID] = user
}

func (i *IntermediateUserSaver) SetLastName(userID int64, lastName string) {
	user, exists := (*i)[userID]
	if !exists {
		user = intermediateUser{}
	}
	user.lastName = lastName
	(*i)[userID] = user
}

func (i *IntermediateUserSaver) SetEmail(userID int64, email string) {
	user, exists := (*i)[userID]
	if !exists {
		user = intermediateUser{}
	}
	user.email = email
	(*i)[userID] = user
}

func (i *IntermediateUserSaver) SetConfirmationCode(userID int64, code string) {
	user, exists := (*i)[userID]
	if !exists {
		user = intermediateUser{}
	}
	user.confirmationCode = code
	(*i)[userID] = user
}

func (i *IntermediateUserSaver) GetFirstName(userID int64) string {
	user, exists := (*i)[userID]
	if !exists {
		return ""
	}
	return user.firstName
}

func (i *IntermediateUserSaver) GetLastName(userID int64) string {
	user, exists := (*i)[userID]
	if !exists {
		return ""
	}
	return user.lastName
}

func (i *IntermediateUserSaver) GetEmail(userID int64) string {
	user, exists := (*i)[userID]
	if !exists {
		return ""
	}
	return user.email
}

func (i *IntermediateUserSaver) GetConfirmationCode(userID int64) string {
	user, exists := (*i)[userID]
	if !exists {
		return ""
	}
	return user.confirmationCode
}

func (i *IntermediateUserSaver) GetMemberNumber(userID int64) int64 {
	user, exists := (*i)[userID]
	if !exists {
		return -1
	}
	return user.memberNumber
}

func (i *IntermediateUserSaver) RemoveUser(userID int64) {
	delete(*i, userID)
}

func (i *IntermediateUserSaver) GetCompleteUserAndCleanup(userID int64) model.User {
	i.RemoveUser(userID)
	user := (*i)[userID]
	return model.User{
		TelegramID:   userID,
		FirstName:    user.firstName,
		LastName:     user.lastName,
		MensaEmail:   user.email,
		MemberNumber: user.memberNumber,
	}
}
