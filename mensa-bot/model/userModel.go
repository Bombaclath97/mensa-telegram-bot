package model

type User struct {
	TelegramID        int64   `json:"telegramId"`
	MensaEmail        string  `json:"mensaEmail"`
	MembershipEndDate *string `json:"membershipEndDate,omitempty"`
	FirstName         string  `json:"firstName"`
	LastName          string  `json:"lastName"`
	MemberNumber      int64   `json:"memberNumber"`
}
