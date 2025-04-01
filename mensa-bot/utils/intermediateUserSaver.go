package utils

import model "git.bombaclath.cc/bombadurelli/mensa-bot-telegram/mensa-shared-models"

type intermediateUser struct {
	userId           int64
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

func (i *IntermediateUserSaver) SetUserId(userID, toSave int64) {
	user, exist := (*i)[userID]
	if !exist {
		user = intermediateUser{}
	}
	user.userId = toSave
	(*i)[userID] = user
}

func (i *IntermediateUserSaver) GetUserId(userID int64) int64 {
	user, exists := (*i)[userID]
	if !exists {
		return -1
	}
	return user.userId
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
	user := (*i)[userID]
	i.RemoveUser(userID)
	return model.User{
		TelegramID:   userID,
		FirstName:    user.firstName,
		LastName:     user.lastName,
		MensaEmail:   user.email,
		MemberNumber: user.memberNumber,
	}
}
