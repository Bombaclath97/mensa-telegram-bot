package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/model"
)

// TestMain gives the callme handler a real DB: it resolves the user's language via
// database.Get(), which Fatal-exits if the DB was never initialised.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mensa-server-test-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("DB_PATH", filepath.Join(dir, "test.db"))
	if err := database.Init(); err != nil {
		panic("database.Init failed: " + err.Error())
	}
	code := m.Run()
	_ = database.Close()
	os.RemoveAll(dir)
	os.Exit(code)
}

// token with the exact shape the handler expects: "<userID>_<15 chars>".
func tokenFor(userID string) string {
	return userID + "_abcdefghijklmno" // 15-char suffix
}

// callWebhook drives CallmeHandler exactly as the mux would, populating the {token} path
// value, and returns the recorder.
func callWebhook(t *testing.T, token string, accepted bool) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(callmeRequest{Accepted: accepted})
	req := httptest.NewRequest(http.MethodPost, "/callme_api/"+token, strings.NewReader(string(body)))
	req.SetPathValue("token", token)
	rec := httptest.NewRecorder()
	CallmeHandler(rec, req)
	return rec
}

// setupLockedDraft installs a live, LOCKED draft for the user in the global store and
// returns the callme token it was locked with. Cleaned up via t.Cleanup.
//
// The draft carries an email and a membership number because that is the only state a draft
// can legitimately be in when the app calls back: it is locked by handleSubmit, which runs
// only after both fields are filled. A draft without them must be REFUSED, which is what
// TestCallmeAcceptedWithIncompleteDraftIsRefused pins down.
func setupLockedDraft(t *testing.T, userID int64, token string) {
	t.Helper()
	email := "mario.rossi@mensa.it"
	member := "12345"
	model.Drafts.Set(userID, &model.ProfileDraft{Email: &email, MembershipNumber: &member})
	if !model.Drafts.LockUser(userID, token) {
		t.Fatalf("failed to lock draft for %d", userID)
	}
	t.Cleanup(func() { model.Drafts.Delete(userID) })
}

// TestCallmeAcceptedRegistersTheMember: a valid token + accepted=true completes registration
// outright — the row is written and the draft is consumed. There is no name-collection step
// any more; the name comes from the Mensa API.
func TestCallmeAcceptedRegistersTheMember(t *testing.T) {
	const uid = int64(1001)
	tok := tokenFor("1001")
	setupLockedDraft(t, uid, tok)

	// The Mensa API is unreachable in this test. That must NOT block registration: the app
	// callback has already proven the member's identity, so the row is written with a
	// fallback name and the real one is synced later.
	t.Setenv("API_ENDPOINT", "http://127.0.0.1:1")

	rec := callWebhook(t, tok, true)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// The draft is consumed once the row exists.
	if model.Drafts.Get(uid) != nil {
		t.Error("draft should be gone after a completed registration")
	}

	user, err := database.NewUserRepository().GetByID(uid)
	if err != nil {
		t.Fatalf("looking up the registered user: %v", err)
	}
	if user == nil {
		t.Fatal("the member should have been registered by the approval callback")
	}
	if user.MensaEmail != "mario.rossi@mensa.it" || user.MemberNumber != 12345 {
		t.Errorf("registered the wrong details: %+v", user)
	}
	t.Cleanup(func() { _ = database.NewUserRepository().Delete(uid) })
}

// A locked draft missing its email / membership number must never produce a user row.
func TestCallmeAcceptedWithIncompleteDraftIsRefused(t *testing.T) {
	const uid = int64(1010)
	tok := tokenFor("1010")

	model.Drafts.Set(uid, &model.ProfileDraft{}) // no email, no membership number
	if !model.Drafts.LockUser(uid, tok) {
		t.Fatal("failed to lock draft")
	}
	t.Cleanup(func() { model.Drafts.Delete(uid) })

	rec := callWebhook(t, tok, true)

	if rec.Code == http.StatusOK {
		t.Fatalf("an incomplete draft must not register; got 200")
	}
	user, err := database.NewUserRepository().GetByID(uid)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if user != nil {
		t.Fatal("no user row may be written from an incomplete draft")
		_ = database.NewUserRepository().Delete(uid)
	}
}

// TestCallmeRejectedDropsDraft: a valid token + accepted=false unlocks and DELETES the draft.
func TestCallmeRejectedDropsDraft(t *testing.T) {
	const uid = int64(1002)
	tok := tokenFor("1002")
	setupLockedDraft(t, uid, tok)

	rec := callWebhook(t, tok, false)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if model.Drafts.Exists(uid) {
		t.Fatal("a rejected registration must drop the draft")
	}
}

// TestCallmeWrongTokenAcceptedRejected: the accepted=TRUE branch must token-match. A wrong
// token must be rejected and MUST NOT touch the still-locked draft.
func TestCallmeWrongTokenAcceptedRejected(t *testing.T) {
	const uid = int64(1003)
	realTok := tokenFor("1003")
	setupLockedDraft(t, uid, realTok)

	// Same user ID prefix (so ParseCallmeToken passes) but wrong secret suffix.
	wrong := "1003_ZZZZZZZZZZZZZZZ"
	rec := callWebhook(t, wrong, true)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a mismatched token", rec.Code)
	}
	if !model.Drafts.IsLocked(uid) {
		t.Fatal("a mismatched token must leave the draft LOCKED and untouched")
	}
	draft := model.Drafts.Get(uid)
	if draft.ActiveField == model.FieldFirstName {
		t.Fatal("a mismatched token must not advance the flow")
	}
}

// TestCallmeWrongTokenRejectedRejected: the accepted=FALSE branch must ALSO token-match.
// This is the legacy hole — anyone who knew a user's Telegram ID could POST {"accepted":false}
// and abort that user's registration. The new handler must reject it and keep the draft.
func TestCallmeWrongTokenRejectedRejected(t *testing.T) {
	const uid = int64(1004)
	realTok := tokenFor("1004")
	setupLockedDraft(t, uid, realTok)

	wrong := "1004_ZZZZZZZZZZZZZZZ"
	rec := callWebhook(t, wrong, false)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a mismatched token on the reject branch", rec.Code)
	}
	if !model.Drafts.Exists(uid) {
		t.Fatal("a mismatched reject must NOT drop the draft — that was the legacy vulnerability")
	}
	if !model.Drafts.IsLocked(uid) {
		t.Fatal("the draft must remain locked after a mismatched reject")
	}
}

// TestCallmeMalformedTokenIs4xx: a shape-invalid token is a 4xx and changes no state.
func TestCallmeMalformedTokenIs4xx(t *testing.T) {
	const uid = int64(1005)
	realTok := tokenFor("1005")
	setupLockedDraft(t, uid, realTok)

	for _, bad := range []string{
		"nounderscore",               // no separator
		"1005_short",                 // suffix too short
		"notanumber_abcdefghijklmno", // non-numeric id
	} {
		rec := callWebhook(t, bad, true)
		if rec.Code < 400 || rec.Code >= 500 {
			t.Fatalf("token %q: status = %d, want a 4xx", bad, rec.Code)
		}
	}
	// The real draft must be entirely untouched.
	if !model.Drafts.IsLocked(uid) {
		t.Fatal("malformed-token requests must not disturb an unrelated locked draft")
	}
}

// TestCallmeMalformedBodyIs400: a valid token but unparseable body is a 400 and unlocks
// nothing.
func TestCallmeMalformedBodyIs400(t *testing.T) {
	const uid = int64(1006)
	tok := tokenFor("1006")
	setupLockedDraft(t, uid, tok)

	req := httptest.NewRequest(http.MethodPost, "/callme_api/"+tok, strings.NewReader("{not json"))
	req.SetPathValue("token", tok)
	rec := httptest.NewRecorder()
	CallmeHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !model.Drafts.IsLocked(uid) {
		t.Fatal("a malformed body must not unlock the draft")
	}
}

// TestCallmeUnknownUserNoDraft: a well-formed token for a user with no draft at all is a
// 403 and does not panic (the DB lookup path for the session-expired DM must be safe).
func TestCallmeUnknownUserNoDraft(t *testing.T) {
	tok := tokenFor("999999")
	rec := callWebhook(t, tok, true)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a token with no matching draft", rec.Code)
	}
}

// TestHealthHandler covers the liveness probe.
func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	healthHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
}
