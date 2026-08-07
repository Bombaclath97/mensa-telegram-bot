package model

import (
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/Bombaclath97/mensa-telegram-bot/logger"
)

// Field represents the current active field being filled in the profile draft.
// It drives free-text capture: whatever the user types next lands in this field.
type Field string

const (
	FieldNone             Field = ""
	FieldEmail            Field = "email"
	FieldMembershipNumber Field = "memnum"
	FieldFirstName        Field = "firstname"
	FieldLastName         Field = "lastname"
)

// DefaultConversationTimeoutMinutes matches the legacy CONVERSATION_TIMEOUT_MINUTES default.
const DefaultConversationTimeoutMinutes = 5

// DefaultApprovalTimeoutMinutes is the TTL of a draft parked in AWAITING_APPROVAL.
//
// Approving is a human action in a SEPARATE app (receive push -> open the Mensa app ->
// tap approve), so it routinely takes far longer than a wizard keystroke. Reusing the
// short conversation TTL here silently dropped legitimately-approving members the moment
// they crossed the 5-minute mark. This window is deliberately generous and, unlike the
// wizard steps, is NOT re-armed while locked (nothing the user does in Telegram advances
// a locked draft), so it must be long enough to cover the whole app round-trip on its own.
const DefaultApprovalTimeoutMinutes = 30

// ConversationTimeout returns the draft TTL, from CONVERSATION_TIMEOUT_MINUTES
// (default 5 minutes).
func ConversationTimeout() time.Duration {
	return envMinutes("CONVERSATION_TIMEOUT_MINUTES", DefaultConversationTimeoutMinutes)
}

// ApprovalTimeout returns the TTL applied while a draft is AWAITING_APPROVAL from the
// Mensa app, from APPROVAL_TIMEOUT_MINUTES (default 30 minutes).
func ApprovalTimeout() time.Duration {
	return envMinutes("APPROVAL_TIMEOUT_MINUTES", DefaultApprovalTimeoutMinutes)
}

// envMinutes reads a positive integer number of minutes from env, falling back to def.
func envMinutes(key string, def int) time.Duration {
	minutes := def
	if raw := os.Getenv(key); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			minutes = parsed
		}
	}
	return time.Duration(minutes) * time.Minute
}

// ProfileDraft represents an in-progress profile registration.
//
// Lifecycle: email + membership number are collected via the inline keyboard, then the
// Mensa API is asked to push an approval request to the user's Mensa app; the draft is
// LOCKED (Locked=true, CallmeToken set) until the app calls back with the matching token.
// After the unlock the first name and last name are collected and the User row is written.
type ProfileDraft struct {
	Email            *string
	MembershipNumber *string
	FirstName        *string
	LastName         *string

	// ActiveField is the field the next free-text message fills in.
	ActiveField Field

	// MessageID is the ID of the keyboard message to edit.
	MessageID int

	// CallmeToken is the '<userID>_<15 random chars>' token handed to the Mensa app.
	CallmeToken string

	// Locked is true while the draft is AWAITING_APPROVAL from the Mensa app.
	Locked bool

	// ExpiresAt is when the janitor drops this draft.
	ExpiresAt time.Time
}

// IsExpired reports whether the draft has outlived its TTL.
func (d *ProfileDraft) IsExpired() bool {
	return !d.ExpiresAt.IsZero() && time.Now().After(d.ExpiresAt)
}

// clone returns a shallow copy of the draft. It MUST be called with the store lock held.
//
// A shallow copy is safe: the pointer fields (Email, MembershipNumber, FirstName,
// LastName) are only ever REPLACED with a fresh pointer by the store's mutators, never
// mutated through the existing pointer, so a snapshot can share them without exposing a
// caller to a concurrent in-place write.
func (d *ProfileDraft) clone() *ProfileDraft {
	if d == nil {
		return nil
	}
	cp := *d
	return &cp
}

// ProfileDraftStore manages in-memory profile drafts during registration.
// Drafts are ephemeral (lost on restart, by design) but mutex-safe and TTL'd: an
// abandoned draft is dropped after ConversationTimeout() so it cannot soft-brick the
// user out of /start and /elimina, nor leak memory.
//
// CONCURRENCY MODEL. A profile draft is touched by two independent goroutine families:
// the Telegram update dispatcher(s) and the net/http callme webhook. To make every field
// access data-race free, the store NEVER hands out the live *ProfileDraft it stores in the
// map. Get / GetByToken / Update return a *snapshot copy* taken under the lock, and every
// mutation goes through a store method (Set, Update, SetActiveField, SetMessageID,
// LockUser, UnlockUser, Touch) that holds mu. Callers read the snapshot freely and commit
// changes through those methods; they must never assume a write through a returned pointer
// is visible to the store.
type ProfileDraftStore struct {
	mu     sync.RWMutex
	drafts map[int64]*ProfileDraft
}

// NewProfileDraftStore creates a new profile draft store and starts its janitor.
func NewProfileDraftStore() *ProfileDraftStore {
	s := &ProfileDraftStore{
		drafts: make(map[int64]*ProfileDraft),
	}
	go s.janitor()
	return s
}

func (s *ProfileDraftStore) janitor() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.SweepExpired()
	}
}

// SweepExpired removes every expired draft. Called periodically by the janitor;
// exported so it can be driven deterministically in tests.
//
// Abandoned registrations are logged individually and on purpose. They used to vanish in
// silence, which made a very common support question unanswerable: a user who starts /start,
// gets the approval push and never finishes leaves NO trace at all, so "the member says the
// app never asked them" and "the member ignored the prompt" produced identical logs. The
// step the draft died at is the thing that distinguishes them.
func (s *ProfileDraftStore) SweepExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	removed := 0
	for userID, draft := range s.drafts {
		if draft.IsExpired() {
			logger.Info("registration abandoned: draft expired before completion",
				map[string]interface{}{
					"user_id":    userID,
					"step":       draft.abandonedStep(),
					"expired_at": draft.ExpiresAt.UTC().Format(time.RFC3339),
				})
			delete(s.drafts, userID)
			removed++
		}
	}
	return removed
}

// abandonedStep names the point a draft died at, for the expiry log.
//
// Locked is checked FIRST because it is the interesting case: the draft was waiting on the
// Mensa app's callme callback, so an expiry here means the push was never answered — either
// the user ignored it or it never reached them. The ActiveField cases are ordinary
// conversation drop-off.
func (d *ProfileDraft) abandonedStep() string {
	if d.Locked {
		return "awaiting_mensa_app_approval"
	}
	if d.ActiveField != FieldNone {
		return "awaiting_" + string(d.ActiveField)
	}
	return "idle"
}

// Get retrieves a profile draft for a user. Returns nil if there is none or it expired.
//
// The returned draft is a SNAPSHOT COPY: it is safe to read from any goroutine, but writing
// to its fields does NOT update the store. Use Update / SetActiveField / SetMessageID (or
// LockUser / UnlockUser / Touch) to mutate the stored draft.
func (s *ProfileDraftStore) Get(userID int64) *ProfileDraft {
	s.mu.Lock()
	defer s.mu.Unlock()

	draft, ok := s.drafts[userID]
	if !ok {
		return nil
	}
	if draft.IsExpired() {
		delete(s.drafts, userID)
		return nil
	}
	return draft.clone()
}

// Update applies fn to the stored draft under the store lock and returns a fresh snapshot
// of the mutated draft (or nil if there is no live draft). fn runs while the lock is held,
// so it MUST only touch draft fields — never perform I/O or call back into the store.
func (s *ProfileDraftStore) Update(userID int64, fn func(*ProfileDraft)) *ProfileDraft {
	s.mu.Lock()
	defer s.mu.Unlock()

	draft, ok := s.drafts[userID]
	if !ok || draft.IsExpired() {
		return nil
	}
	fn(draft)
	return draft.clone()
}

// SetActiveField arms the field the next free-text message fills. Returns false if there
// is no live draft.
func (s *ProfileDraftStore) SetActiveField(userID int64, field Field) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	draft, ok := s.drafts[userID]
	if !ok || draft.IsExpired() {
		return false
	}
	draft.ActiveField = field
	return true
}

// SetMessageID records the ID of the keyboard message to edit. Returns false if there is
// no live draft.
func (s *ProfileDraftStore) SetMessageID(userID int64, messageID int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	draft, ok := s.drafts[userID]
	if !ok || draft.IsExpired() {
		return false
	}
	draft.MessageID = messageID
	return true
}

// Set stores a profile draft for a user and (re)arms its TTL.
func (s *ProfileDraftStore) Set(userID int64, draft *ProfileDraft) {
	s.mu.Lock()
	defer s.mu.Unlock()

	draft.ExpiresAt = time.Now().Add(ConversationTimeout())
	s.drafts[userID] = draft
}

// Touch extends the TTL of an existing draft. Call it on every user interaction so an
// active conversation is never dropped mid-flow. Returns false if there is no live draft.
func (s *ProfileDraftStore) Touch(userID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	draft, ok := s.drafts[userID]
	if !ok || draft.IsExpired() {
		return false
	}
	draft.ExpiresAt = time.Now().Add(ConversationTimeout())
	return true
}

// Delete removes a profile draft for a user.
func (s *ProfileDraftStore) Delete(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.drafts, userID)
}

// Exists checks if a user has a live (non-expired) draft.
func (s *ProfileDraftStore) Exists(userID int64) bool {
	return s.Get(userID) != nil
}

// LockUser marks the user's draft as AWAITING_APPROVAL and records the callme token
// that must be presented to unlock it. Returns false if the user has no live draft.
func (s *ProfileDraftStore) LockUser(userID int64, token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	draft, ok := s.drafts[userID]
	if !ok || draft.IsExpired() {
		return false
	}
	draft.Locked = true
	draft.CallmeToken = token
	// Locked drafts get the (longer) approval TTL: nothing re-arms them while the member
	// is off in the Mensa app tapping approve, so the whole app round-trip must fit in one
	// window. See DefaultApprovalTimeoutMinutes.
	draft.ExpiresAt = time.Now().Add(ApprovalTimeout())
	return true
}

// UnlockUser unlocks a locked draft ONLY if the presented token matches the one issued
// at lock time. Returns false on a missing draft, an unlocked draft, or a token mismatch.
// Both the accepted and the rejected callback branches MUST go through this check.
func (s *ProfileDraftStore) UnlockUser(userID int64, token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	draft, ok := s.drafts[userID]
	if !ok || draft.IsExpired() {
		return false
	}
	if !draft.Locked || draft.CallmeToken == "" || draft.CallmeToken != token {
		return false
	}
	draft.Locked = false
	draft.ExpiresAt = time.Now().Add(ConversationTimeout())
	return true
}

// IsLocked reports whether the user's draft is waiting for the Mensa app approval.
func (s *ProfileDraftStore) IsLocked(userID int64) bool {
	draft := s.Get(userID)
	return draft != nil && draft.Locked
}

// GetByToken looks a live draft up by its callme token. Returns (userID, draft) or
// (0, nil) when no draft holds that token. The returned draft is a snapshot copy (see Get).
func (s *ProfileDraftStore) GetByToken(token string) (int64, *ProfileDraft) {
	if token == "" {
		return 0, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for userID, draft := range s.drafts {
		if draft.CallmeToken == token && !draft.IsExpired() {
			return userID, draft.clone()
		}
	}
	return 0, nil
}

// Drafts is the global draft store instance.
var Drafts = NewProfileDraftStore()
