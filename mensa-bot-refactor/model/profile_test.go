package model

import (
	"sync"
	"testing"
	"time"
)

func strp(s string) *string { return &s }

// newDraft is a minimal live draft for a user.
func setLiveDraft(s *ProfileDraftStore, userID int64) {
	s.Set(userID, &ProfileDraft{Email: strp("a.b@mensa.it")})
}

func TestSetGetRoundTrip(t *testing.T) {
	s := NewProfileDraftStore()
	setLiveDraft(s, 1)

	got := s.Get(1)
	if got == nil {
		t.Fatal("Get returned nil for a live draft")
	}
	if got.Email == nil || *got.Email != "a.b@mensa.it" {
		t.Fatalf("email not round-tripped: %+v", got)
	}
	if !s.Exists(1) {
		t.Fatal("Exists should be true")
	}
	if s.Get(2) != nil {
		t.Fatal("Get for an unknown user should be nil")
	}
}

// TestGetReturnsSnapshot proves mutating a returned draft does NOT bleed into the store
// (the store's whole concurrency-safety argument depends on this).
func TestGetReturnsSnapshot(t *testing.T) {
	s := NewProfileDraftStore()
	setLiveDraft(s, 1)

	snap := s.Get(1)
	snap.ActiveField = FieldLastName // mutate the copy

	fresh := s.Get(1)
	if fresh.ActiveField == FieldLastName {
		t.Fatal("writing to a returned draft leaked into the store; snapshot isolation is broken")
	}
}

func TestActiveFieldAndMessageID(t *testing.T) {
	s := NewProfileDraftStore()
	setLiveDraft(s, 1)

	if !s.SetActiveField(1, FieldEmail) {
		t.Fatal("SetActiveField should succeed on a live draft")
	}
	if !s.SetMessageID(1, 99) {
		t.Fatal("SetMessageID should succeed")
	}
	got := s.Get(1)
	if got.ActiveField != FieldEmail || got.MessageID != 99 {
		t.Fatalf("field/message id not persisted: %+v", got)
	}

	if s.SetActiveField(404, FieldEmail) {
		t.Fatal("SetActiveField on a missing draft should return false")
	}
}

func TestUpdate(t *testing.T) {
	s := NewProfileDraftStore()
	setLiveDraft(s, 1)

	out := s.Update(1, func(d *ProfileDraft) { d.FirstName = strp("Mario") })
	if out == nil || out.FirstName == nil || *out.FirstName != "Mario" {
		t.Fatalf("Update did not apply: %+v", out)
	}
	if got := s.Get(1); got.FirstName == nil || *got.FirstName != "Mario" {
		t.Fatal("Update did not persist to the store")
	}
	if s.Update(404, func(*ProfileDraft) {}) != nil {
		t.Fatal("Update on a missing draft should return nil")
	}
}

// TestExpiryViaSweep exercises the TTL path deterministically by forcing ExpiresAt into the
// past rather than sleeping.
func TestExpiryViaSweep(t *testing.T) {
	s := NewProfileDraftStore()
	setLiveDraft(s, 1)
	setLiveDraft(s, 2)

	// Force user 1 to be expired.
	s.Update(1, func(d *ProfileDraft) { d.ExpiresAt = time.Now().Add(-time.Minute) })

	if removed := s.SweepExpired(); removed != 1 {
		t.Fatalf("SweepExpired removed %d, want 1", removed)
	}
	if s.Exists(1) {
		t.Fatal("expired draft should have been swept")
	}
	if !s.Exists(2) {
		t.Fatal("live draft should have survived the sweep")
	}
}

// TestGetDropsExpired proves a lazy read also evicts an expired draft.
func TestGetDropsExpired(t *testing.T) {
	s := NewProfileDraftStore()
	setLiveDraft(s, 1)
	s.Update(1, func(d *ProfileDraft) { d.ExpiresAt = time.Now().Add(-time.Second) })

	if got := s.Get(1); got != nil {
		t.Fatal("Get should return nil (and evict) an expired draft")
	}
	// A second read confirms it was actually removed.
	if s.Exists(1) {
		t.Fatal("expired draft should have been evicted by Get")
	}
}

func TestIsExpired(t *testing.T) {
	d := &ProfileDraft{}
	if d.IsExpired() {
		t.Fatal("a zero ExpiresAt must never be considered expired")
	}
	d.ExpiresAt = time.Now().Add(-time.Second)
	if !d.IsExpired() {
		t.Fatal("a past ExpiresAt must be expired")
	}
	d.ExpiresAt = time.Now().Add(time.Hour)
	if d.IsExpired() {
		t.Fatal("a future ExpiresAt must not be expired")
	}
}

// TestLockUnlockTokenMatching is the auth-boundary test: an unlock with the WRONG token
// must fail and leave the draft locked. Both callme branches funnel through UnlockUser.
func TestLockUnlockTokenMatching(t *testing.T) {
	s := NewProfileDraftStore()
	setLiveDraft(s, 1)

	const good = "1_abcdefghijklmno"
	if !s.LockUser(1, good) {
		t.Fatal("LockUser should succeed on a live draft")
	}
	if !s.IsLocked(1) {
		t.Fatal("draft should be locked")
	}

	// Wrong token: MUST be rejected and MUST NOT unlock.
	if s.UnlockUser(1, "1_WRONGWRONGWRONG") {
		t.Fatal("UnlockUser accepted a mismatched token — auth boundary broken")
	}
	if !s.IsLocked(1) {
		t.Fatal("a failed unlock must leave the draft locked")
	}

	// Empty token must also be rejected.
	if s.UnlockUser(1, "") {
		t.Fatal("UnlockUser accepted an empty token")
	}
	if !s.IsLocked(1) {
		t.Fatal("draft must remain locked after empty-token attempt")
	}

	// Correct token: unlocks.
	if !s.UnlockUser(1, good) {
		t.Fatal("UnlockUser rejected the correct token")
	}
	if s.IsLocked(1) {
		t.Fatal("draft should be unlocked after a matching token")
	}

	// Second unlock (already unlocked) must fail: replay protection.
	if s.UnlockUser(1, good) {
		t.Fatal("UnlockUser succeeded on an already-unlocked draft")
	}
}

func TestUnlockMissingDraft(t *testing.T) {
	s := NewProfileDraftStore()
	if s.UnlockUser(404, "1_abcdefghijklmno") {
		t.Fatal("UnlockUser should fail when there is no draft")
	}
	if s.LockUser(404, "tok") {
		t.Fatal("LockUser should fail when there is no draft")
	}
}

// TestUnlockExpiredDraftFails: a draft whose approval window elapsed cannot be unlocked.
func TestUnlockExpiredDraftFails(t *testing.T) {
	s := NewProfileDraftStore()
	setLiveDraft(s, 1)
	const tok = "1_abcdefghijklmno"
	s.LockUser(1, tok)
	s.Update(1, func(d *ProfileDraft) { d.ExpiresAt = time.Now().Add(-time.Minute) })

	if s.UnlockUser(1, tok) {
		t.Fatal("an expired locked draft must not be unlockable even with the right token")
	}
}

func TestGetByToken(t *testing.T) {
	s := NewProfileDraftStore()
	setLiveDraft(s, 1)
	setLiveDraft(s, 2)
	const tok = "2_abcdefghijklmno"
	s.LockUser(2, tok)

	uid, d := s.GetByToken(tok)
	if uid != 2 || d == nil {
		t.Fatalf("GetByToken(%q) = (%d, %v), want user 2", tok, uid, d)
	}
	if uid, _ := s.GetByToken("nonexistent"); uid != 0 {
		t.Fatal("GetByToken for an unknown token should return 0")
	}
	if uid, _ := s.GetByToken(""); uid != 0 {
		t.Fatal("GetByToken(\"\") should return 0")
	}
}

func TestDelete(t *testing.T) {
	s := NewProfileDraftStore()
	setLiveDraft(s, 1)
	s.Delete(1)
	if s.Exists(1) {
		t.Fatal("draft should be gone after Delete")
	}
}

// TestConcurrentAccess stresses the store from many goroutines. Run under -race, its whole
// purpose is to trip the race detector if the locking is wrong.
func TestConcurrentAccess(t *testing.T) {
	s := NewProfileDraftStore()
	const workers = 32
	const iterations = 200

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			// Contend on a small shared key space so operations actually collide.
			uid := int64(w % 4)
			for i := 0; i < iterations; i++ {
				s.Set(uid, &ProfileDraft{Email: strp("x@mensa.it")})
				s.SetActiveField(uid, FieldEmail)
				s.SetMessageID(uid, i)
				s.Update(uid, func(d *ProfileDraft) { d.FirstName = strp("N") })
				s.Touch(uid)
				tok := "tok"
				s.LockUser(uid, tok)
				_ = s.IsLocked(uid)
				_, _ = s.GetByToken(tok)
				s.UnlockUser(uid, tok)
				_ = s.Get(uid)
				_ = s.Exists(uid)
				s.SweepExpired()
			}
		}(w)
	}
	wg.Wait()
}
