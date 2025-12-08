package resthandlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/Bombaclath97/mensa-telegram-bot/convo"
	"github.com/go-chi/chi/v5"
)

type callbackRequestJsonBody struct {
	Accepted bool `json:"accepted"`
}

type callbackResponseJsonBody struct {
	Message string `json:"message"`
}

// API path: /callback/{userId}_{blob}
func CallbackHandler(w http.ResponseWriter, r *http.Request) {
	userId := chi.URLParam(r, "userId")

	userIdInt, err := strconv.ParseInt(userId, 10, 64)
	if err != nil {
		log.Printf("ERROR: Invalid user ID %s: %v", userId, err)
		http.Error(w, fmt.Sprintf("Invalid user ID %s: %v", userId, err), http.StatusBadRequest)
		return
	}

	log.Printf("INFO: Received callme API request for user %s", userId)

	// Validate JSON body
	var callbackRequestJsonBody callbackRequestJsonBody

	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()

	if err := d.Decode(&callbackRequestJsonBody); err != nil {
		receivedBody, _ := io.ReadAll(r.Body)
		http.Error(w, fmt.Sprintf("ERROR: Received unexpected body: %s", receivedBody), http.StatusUnprocessableEntity)
	}

	w.Header().Set("Content-Type", "application/json")

	if !callbackRequestJsonBody.Accepted {
		log.Printf("INFO: User %s rejected the notification request!", userId)
		// Clear the conversation context on rejection
		if err := convo.ClearConvoContext(userIdInt); err != nil {
			log.Printf("ERROR: Failed to clear context for user %s: %v", userId, err)
		}
		json.NewEncoder(w).Encode(&callbackResponseJsonBody{Message: fmt.Sprintf("Utente %s: richiesta rifiutata!", userId)})
	} else {
		log.Printf("INFO: User %s accepted the notification request!", userId)
		// Update context: unlock interaction and set next step
		err := convo.UpdateConvoContext(userIdInt, func(ctx *convo.ConvoContext) error {
			ctx.LockedInteraction = false
			ctx.CurrentStep = convo.AskFullName
			return nil
		})
		if err != nil {
			log.Printf("ERROR: Failed to update context for user %s: %v", userId, err)
			json.NewEncoder(w).Encode(&callbackResponseJsonBody{Message: "Errore nell'aggiornamento del contesto"})
			return
		}
		json.NewEncoder(w).Encode(&callbackResponseJsonBody{Message: fmt.Sprintf("Utente %s sbloccato!", userId)})
	}
}
