package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/handlers"
	"github.com/Bombaclath97/mensa-telegram-bot/logger"
	"github.com/Bombaclath97/mensa-telegram-bot/model"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
	"github.com/Bombaclath97/mensa-telegram-bot/tolgee"
	"github.com/Bombaclath97/mensa-telegram-bot/utils"
)

// callmeRequest is the body the Mensa app POSTs: {"accepted": true|false}.
type callmeRequest struct {
	Accepted bool `json:"accepted"`
}

// dmTimeout bounds the Telegram DM we send from the webhook. We deliberately do NOT use
// the request context for it: the state transition has already happened by then, and the
// caller hanging up must not abort the notification to the user.
const dmTimeout = 15 * time.Second

// completeTimeout budgets the whole post-approval completion: the Mensa API name lookup,
// the row write, and the confirmation DM + profile card.
const completeTimeout = 45 * time.Second

// callmeUserRepo resolves the user's language for the DM.
var callmeUserRepo = database.NewUserRepository()

// CallmeHandler serves POST /callme_api/{token}: the callback the official Mensa app
// hits once the user has accepted or rejected the identity-verification push we asked
// for during registration (api.MensaAPIClient.RequestVerification).
//
// The token is "<telegramID>_<15 random chars>" (utils.GenerateCallmeUrlEndpoint) and is
// the ONLY secret proving the caller is the app acting for that user. It is therefore
// token-matched against the locked draft on BOTH branches — the legacy handler checked
// the token only when accepted=true, so anyone who knew a user's Telegram ID could abort
// that user's registration by POSTing {"accepted": false} to <id>_anything.
//
// Outcomes:
//
//	accepted=true  -> draft unlocked, advanced to the first-name step, user DMed
//	                  telegrambot.conversation.approvedapproval
//	accepted=false -> draft unlocked and dropped, user DMed
//	                  telegrambot.conversation.rejectedapproval
//	bad/unknown/mismatched token, or malformed body -> 4xx, NO state change
func CallmeHandler(w http.ResponseWriter, r *http.Request) {
	fullToken := r.PathValue("token")

	userID, err := utils.ParseCallmeToken(fullToken)
	if err != nil {
		// Shape-invalid token. Same opaque body as a mismatch: never reveal whether a
		// token exists.
		logger.Warn("Callme callback with malformed token", map[string]interface{}{
			"error": err.Error(),
		})
		callmeReject(w)
		return
	}

	var body callmeRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes)).Decode(&body); err != nil {
		logger.Warn("Callme callback with unparseable body", map[string]interface{}{
			"user_id": userID,
			"error":   err.Error(),
		})
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	// AUTHENTICATION. UnlockUser succeeds only when this exact user has a live, locked
	// draft whose callme token equals the one presented. It is the single gate for both
	// branches, and it mutates nothing when it fails.
	if !model.Drafts.UnlockUser(userID, fullToken) {
		logger.Warn("Callme callback rejected: no live locked draft matches the token", map[string]interface{}{
			"user_id":  userID,
			"accepted": body.Accepted,
		})

		// If the user has NO live draft at all, the overwhelmingly likely cause is that
		// their approval window elapsed before they got to the Mensa app. Give them
		// something actionable instead of a silent 403 they will never see, so an
		// approving member is never left with nothing. We only do this when there is no
		// live draft: a mismatched token against a still-locked draft is a probe/attack
		// and must not disturb the in-progress registration (nor confirm the draft exists).
		if !model.Drafts.Exists(userID) {
			lang := callmeUserRepo.GetLanguage(userID)
			callmeNotify(userID, tolgee.T("telegrambot.conversation.sessionexpired", lang))
		}

		// The HTTP response stays opaque on every failure branch (see callmeReject).
		callmeReject(w)
		return
	}

	lang := callmeUserRepo.GetLanguage(userID)

	if !body.Accepted {
		// The user refused to confirm their identity in the app: drop the registration.
		model.Drafts.Delete(userID)

		logger.Info("Callme callback: user rejected the verification", map[string]interface{}{
			"user_id": userID,
		})

		callmeNotify(userID, tolgee.T("telegrambot.conversation.rejectedapproval", lang))
		writeJSON(w, http.StatusOK, map[string]string{"status": "rejected"})
		return
	}

	// Accepted: the Mensa app confirmed the email/membership-number pair belongs to this
	// person. Advance the wizard to the name-collection step (legacy ASKED_NAME).
	//
	// The transition is owned by handlers.ProfileAwaitFirstName so that the registration
	// flow stays the single source of truth for its own state machine (it arms
	// model.FieldFirstName and re-arms the TTL for the two name steps).
	//
	// The CallmeToken is deliberately NOT cleared: it is the draft's only proof that the
	// Mensa app approved this member, and profileFinishRegistration refuses to write a
	// user row without it. Clearing it here would make every approved registration fail
	// silently at the final step. Replay is already impossible without it — UnlockUser
	// requires a LOCKED draft, and this one is now unlocked.
	// Deliberately NOT r.Context(): completing the registration writes the user row, calls
	// the Mensa API for the member's name and DMs them. If the caller hung up, cancelling
	// that half-way would leave a member who has already approved in the app with no
	// profile. It gets its own budget instead.
	// Logged BEFORE the completion runs, not after. ProfileCompleteAfterApproval writes the
	// user row, DMs the member and syncs their groups, all of which log in their own right —
	// so emitting the approval afterwards printed the effects above their cause and made the
	// registration timeline read backwards ("User registered" preceding "user approved").
	logger.Info("Callme callback: user approved the verification, completing registration",
		map[string]interface{}{"user_id": userID})

	ctx, cancel := context.WithTimeout(context.Background(), completeTimeout)
	defer cancel()

	if !handlers.ProfileCompleteAfterApproval(ctx, userID) {
		// The TTL elapsed between the unlock and this call, or the row could not be written.
		logger.Warn("Callme callback: could not complete the registration", map[string]interface{}{
			"user_id": userID,
		})
		callmeReject(w)
		return
	}

	// No DM here: ProfileCompleteAfterApproval has already written the row and sent the
	// member their confirmation plus their profile card. The old "approved — now send me
	// your name" prompt is gone along with the name-collection steps.
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

// callmeReject writes the single opaque failure response used for every authentication
// failure (malformed token, unknown token, token mismatch, expired draft) so the caller
// cannot distinguish them.
func callmeReject(w http.ResponseWriter) {
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid request"})
}

// callmeNotify DMs the user on a detached, bounded context.
func callmeNotify(userID int64, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), dmTimeout)
	defer cancel()

	if _, err := tb.Send(ctx, userID, message); err != nil {
		logger.Error("Failed to DM the user after the callme callback", map[string]interface{}{
			"user_id": userID,
			"error":   err.Error(),
		})
	}
}
