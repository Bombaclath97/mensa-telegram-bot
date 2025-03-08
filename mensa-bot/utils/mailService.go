package utils

import (
	"fmt"
	"os"

	"git.bombaclath.cc/bombaclath97/mensa-bot-telegram/bot/model"
	"github.com/wneessen/go-mail"
)

func SendConfirmationEmail(email, firstName, confCode string) {
	username := os.Getenv("GMAIL_USERNAME")
	password := os.Getenv("GMAIL_PASSWORD")
	client, err := mail.NewClient("smtp.gmail.com", mail.WithTLSPortPolicy(mail.TLSMandatory),
		mail.WithSMTPAuth(mail.SMTPAuthPlain), mail.WithUsername(username), mail.WithPassword(password))
	if err != nil {
		fmt.Printf("failed to create mail client: %s\n", err)
		os.Exit(1)
	}

	message := mail.NewMsg()

	message.From(username)

	message.To(email)
	message.Subject("Mensa Bot Confirmation Code")
	message.SetBodyString(mail.TypeTextHTML, fmt.Sprintf(model.EMAIL_BODY, firstName, confCode))

	if err = client.DialAndSend(message); err != nil {
		fmt.Printf("failed to send mail: %s\n", err)
		os.Exit(1)
	}
}
