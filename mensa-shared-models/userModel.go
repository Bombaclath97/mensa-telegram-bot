package model

type User struct {
	TelegramID   int64  `json:"telegramId"`
	MensaEmail   string `json:"mensaEmail"`
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	MemberNumber int64  `json:"memberNumber"`
	IsBotAdmin   bool   `json:"isBotAdmin"`
}
