package model

import "sync"

// Field represents the current active field being filled in the profile draft
type Field string

const (
	FieldNone             Field = ""
	FieldEmail            Field = "email"
	FieldMembershipNumber Field = "memnum"
)

// ProfileDraft represents an in-progress profile registration
type ProfileDraft struct {
	Email            *string
	MembershipNumber *string
	ActiveField      Field
	MessageID        int // ID of the keyboard message to edit
}

// ProfileDraftStore manages in-memory profile drafts during registration
type ProfileDraftStore struct {
	mu     sync.RWMutex
	drafts map[int64]*ProfileDraft
}

// NewProfileDraftStore creates a new profile draft store
func NewProfileDraftStore() *ProfileDraftStore {
	return &ProfileDraftStore{
		drafts: make(map[int64]*ProfileDraft),
	}
}

// Get retrieves a profile draft for a user
func (s *ProfileDraftStore) Get(userID int64) *ProfileDraft {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.drafts[userID]
}

// Set stores a profile draft for a user
func (s *ProfileDraftStore) Set(userID int64, draft *ProfileDraft) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drafts[userID] = draft
}

// Delete removes a profile draft for a user
func (s *ProfileDraftStore) Delete(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.drafts, userID)
}

// Exists checks if a user has an active draft
func (s *ProfileDraftStore) Exists(userID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, exists := s.drafts[userID]
	return exists
}

// Global draft store instance
var Drafts = NewProfileDraftStore()
