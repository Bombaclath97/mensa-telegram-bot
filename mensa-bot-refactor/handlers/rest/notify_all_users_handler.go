package resthandlers

import (
	"log"
	"net/http"

	"github.com/Bombaclath97/bomba-go-utils/env"
)

func NotifyAllUsersHandler(w http.ResponseWriter, r *http.Request) {
	adminToken, found := env.GetEnv("BOT_ADMIN_TOKEN")

	if !found {
		log.Printf("ERROR: Bot was initialized without an admin token. Can't invoke admin functions!")
		http.Error(w, "Bot was initialized without an admin token. Can't invoke admin functions!", http.StatusPreconditionFailed)
		return
	}

	receivedToken := r.Header.Get("bot-admin-token")

	if adminToken != receivedToken {
		log.Printf("ERROR: Nice try satan! Invalid admin token!")
		http.Error(w, "I don't think so bitch! :)", http.StatusUnauthorized)
		return
	}

	// Get all users and send them a message
}
