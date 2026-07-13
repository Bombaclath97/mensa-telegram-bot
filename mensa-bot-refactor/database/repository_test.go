package database

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain initialises a single real (temp-file) SQLite database for the whole package.
// A real file — not :memory: — is used so that foreign-key CASCADE behaviour, which is what
// several of these tests exercise, is identical to production.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mensa-db-test-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("DB_PATH", filepath.Join(dir, "test.db"))

	if err := Init(); err != nil {
		panic("database.Init failed: " + err.Error())
	}

	code := m.Run()
	_ = Close()
	os.RemoveAll(dir)
	os.Exit(code)
}

// clearAll wipes every table so each test starts from a known-empty DB.
func clearAll(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"groups_associations", "pending_join_requests", "user_preferences", "users", "groups",
	} {
		if err := Get().Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("failed to clear %s: %v", table, err)
		}
	}
	// Reset the seeded-admin set that some tests populate (package-global state).
	SeedBotAdmins(nil)
}

func newUser(id int64) *User {
	return &User{
		TelegramID:   id,
		MensaEmail:   "user@mensa.it",
		FirstName:    "Mario",
		LastName:     "Rossi",
		MemberNumber: 1000 + id,
		Language:     "it",
	}
}

func TestUserCRUDRoundTrip(t *testing.T) {
	clearAll(t)
	repo := NewUserRepository()

	u := newUser(42)
	if err := repo.Create(u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(42)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("GetByID returned nil for an existing user")
	}
	if got.MensaEmail != "user@mensa.it" || got.MemberNumber != 1042 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	// Update.
	got.FirstName = "Luigi"
	if err := repo.Update(got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	reread, _ := repo.GetByID(42)
	if reread.FirstName != "Luigi" {
		t.Fatalf("update not persisted: %+v", reread)
	}

	// Lookups.
	if exists, _ := repo.Exists(42); !exists {
		t.Fatal("Exists should be true")
	}
	byEmail, err := repo.GetByEmail("user@mensa.it")
	if err != nil || byEmail == nil || byEmail.TelegramID != 42 {
		t.Fatalf("GetByEmail: %v %+v", err, byEmail)
	}

	// Delete.
	if err := repo.Delete(42); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	after, err := repo.GetByID(42)
	if err != nil {
		t.Fatalf("GetByID after delete errored: %v", err)
	}
	if after != nil {
		t.Fatal("user should be gone after Delete")
	}
}

// TestGetByIDNotFoundConvention: a missing row is (nil, nil), never an error. Callers rely
// on this to distinguish "not registered" from a real DB failure.
func TestGetByIDNotFoundConvention(t *testing.T) {
	clearAll(t)
	repo := NewUserRepository()
	got, err := repo.GetByID(99999)
	if err != nil {
		t.Fatalf("not-found must not be an error, got %v", err)
	}
	if got != nil {
		t.Fatalf("not-found must return nil, got %+v", got)
	}
}

func TestGetAll(t *testing.T) {
	clearAll(t)
	repo := NewUserRepository()
	for _, id := range []int64{1, 2, 3} {
		if err := repo.Create(newUserUnique(id)); err != nil {
			t.Fatalf("Create %d: %v", id, err)
		}
	}
	users, err := repo.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("GetAll returned %d users, want 3", len(users))
	}
}

func newUserUnique(id int64) *User {
	u := newUser(id)
	// avoid the unique-ish email clashing on GetByEmail-based tests
	u.MensaEmail = "user" + u.LastName + itoa(id) + "@mensa.it"
	return u
}

func itoa(i int64) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}

// TestForeignKeysAreEnforced proves FK enforcement is actually ON (it is only ON if the DSN
// carries _foreign_keys=on; the PRAGMA-on-the-pool approach silently leaves it off). An
// association referencing a non-existent user must be REJECTED.
func TestForeignKeysAreEnforced(t *testing.T) {
	clearAll(t)
	assoc := NewGroupAssociationRepository()

	// Neither user 777 nor group 888 exist.
	err := assoc.Create(&GroupAssociation{UserID: 777, GroupID: 888})
	if err == nil {
		t.Fatal("creating an association with no parent rows succeeded — foreign keys are OFF")
	}
}

// TestCascadeDeleteOnUser: deleting a user must cascade-delete their associations. This only
// works when the FK carries ON DELETE CASCADE *and* enforcement is on.
func TestCascadeDeleteOnUser(t *testing.T) {
	clearAll(t)
	users := NewUserRepository()
	groups := NewGroupRepository()
	assoc := NewGroupAssociationRepository()

	if err := users.Create(newUser(10)); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := groups.Register(500); err != nil {
		t.Fatalf("register group: %v", err)
	}
	if err := assoc.Register(10, 500); err != nil {
		t.Fatalf("register association: %v", err)
	}

	// Sanity: the association exists.
	if in, _ := assoc.IsUserInGroup(10, 500); !in {
		t.Fatal("association should exist before delete")
	}

	if err := users.Delete(10); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	// The association must have cascaded away.
	rows, err := assoc.GetUserGroups(10)
	if err != nil {
		t.Fatalf("GetUserGroups: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("associations did not cascade on user delete: %d remain", len(rows))
	}
	// The group row itself must survive (only the join row cascades).
	if exists, _ := groups.Exists(500); !exists {
		t.Fatal("the group row should NOT have been deleted")
	}
}

// TestCascadeDeleteOnGroup: deleting a group cascades its associations.
func TestCascadeDeleteOnGroup(t *testing.T) {
	clearAll(t)
	users := NewUserRepository()
	groups := NewGroupRepository()
	assoc := NewGroupAssociationRepository()

	users.Create(newUser(11))
	groups.Register(600)
	assoc.Register(11, 600)

	if err := groups.Delete(600); err != nil {
		t.Fatalf("delete group: %v", err)
	}
	rows, _ := assoc.GetGroupUsers(600)
	if len(rows) != 0 {
		t.Fatalf("associations did not cascade on group delete: %d remain", len(rows))
	}
	// The user survives.
	if u, _ := users.GetByID(11); u == nil {
		t.Fatal("the user row should NOT have been deleted")
	}
}

func TestGroupAssociationPromoteDemote(t *testing.T) {
	clearAll(t)
	users := NewUserRepository()
	assoc := NewGroupAssociationRepository()
	users.Create(newUser(20))

	// Register (also creates the group), then promote.
	if err := assoc.Register(20, 700); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := assoc.PromoteToAdmin(20, 700); err != nil {
		t.Fatalf("promote: %v", err)
	}
	got, _ := assoc.Get(20, 700)
	if got == nil || !got.IsGroupAdmin {
		t.Fatalf("expected group admin flag set: %+v", got)
	}

	if err := assoc.DemoteFromAdmin(20, 700); err != nil {
		t.Fatalf("demote: %v", err)
	}
	got, _ = assoc.Get(20, 700)
	if got == nil || got.IsGroupAdmin {
		t.Fatalf("expected group admin flag cleared: %+v", got)
	}
}

func TestGroupAssociationRegisterIdempotent(t *testing.T) {
	clearAll(t)
	users := NewUserRepository()
	assoc := NewGroupAssociationRepository()
	users.Create(newUser(21))

	for i := 0; i < 3; i++ {
		if err := assoc.Register(21, 800); err != nil {
			t.Fatalf("register iteration %d: %v", i, err)
		}
	}
	rows, _ := assoc.GetUserGroups(21)
	if len(rows) != 1 {
		t.Fatalf("idempotent Register produced %d rows, want 1", len(rows))
	}
}

func TestDeleteAllForUser(t *testing.T) {
	clearAll(t)
	users := NewUserRepository()
	assoc := NewGroupAssociationRepository()
	users.Create(newUser(22))
	assoc.Register(22, 900)
	assoc.Register(22, 901)

	if err := assoc.DeleteAllForUser(22); err != nil {
		t.Fatalf("DeleteAllForUser: %v", err)
	}
	rows, _ := assoc.GetUserGroups(22)
	if len(rows) != 0 {
		t.Fatalf("DeleteAllForUser left %d rows", len(rows))
	}
}

// --- Admin seeding -------------------------------------------------------

func TestParseAdminIDs(t *testing.T) {
	ids, err := ParseAdminIDs(" 111, 222 ,333,")
	if err != nil {
		t.Fatalf("ParseAdminIDs: %v", err)
	}
	if len(ids) != 3 || ids[0] != 111 || ids[2] != 333 {
		t.Fatalf("parsed = %v", ids)
	}
	if _, err := ParseAdminIDs("abc"); err == nil {
		t.Fatal("expected an error on a malformed id")
	}
	empty, err := ParseAdminIDs("")
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty input should be (nil,nil): %v %v", empty, err)
	}
}

// TestSeedBotAdminsFlagsExistingUser: seeding after a user already exists flips their flag.
func TestSeedBotAdminsFlagsExistingUser(t *testing.T) {
	clearAll(t)
	repo := NewUserRepository()
	repo.Create(newUser(555)) // IsBotAdmin defaults false

	if err := SeedBotAdmins([]int64{555}); err != nil {
		t.Fatalf("SeedBotAdmins: %v", err)
	}
	got, _ := repo.GetByID(555)
	if !got.IsBotAdmin {
		t.Fatal("existing user should have been flagged is_bot_admin=true")
	}
	if admin, _ := repo.IsBotAdmin(555); !admin {
		t.Fatal("IsBotAdmin should report true")
	}
}

// TestSeedBotAdminsAppliesToLaterRegistration: a seeded ID that registers AFTER boot is
// flagged by the BeforeCreate hook.
func TestSeedBotAdminsAppliesToLaterRegistration(t *testing.T) {
	clearAll(t)
	repo := NewUserRepository()

	// Seed an ID that does not exist yet.
	if err := SeedBotAdmins([]int64{556}); err != nil {
		t.Fatalf("SeedBotAdmins: %v", err)
	}
	// IsBotAdmin is true from the seed set even before the row exists.
	if admin, _ := repo.IsBotAdmin(556); !admin {
		t.Fatal("a seeded ID should be an admin before registering")
	}
	if !IsSeededBotAdmin(556) {
		t.Fatal("IsSeededBotAdmin should be true")
	}

	// Now they register.
	repo.Create(newUser(556))
	got, _ := repo.GetByID(556)
	if !got.IsBotAdmin {
		t.Fatal("a user registering after being seeded must be flagged is_bot_admin=true")
	}

	// A non-seeded user is not an admin.
	repo.Create(newUserUnique(557))
	if got, _ := repo.GetByID(557); got.IsBotAdmin {
		t.Fatal("a non-seeded user must not be a bot admin")
	}
}

// --- Language preference fallback (G17) ----------------------------------

// TestLanguageFallbackForUnregisteredUser: /lang before registration must persist the
// choice out-of-band and be carried into the User row on registration.
func TestLanguageFallbackForUnregisteredUser(t *testing.T) {
	clearAll(t)
	repo := NewUserRepository()

	// GetLanguage for a totally unknown user is the default.
	if lang := repo.GetLanguage(30); lang != DefaultLanguage {
		t.Fatalf("unknown user language = %q, want %q", lang, DefaultLanguage)
	}

	// Unregistered user picks English via /lang.
	if err := repo.UpdateLanguage(30, "en"); err != nil {
		t.Fatalf("UpdateLanguage (unregistered): %v", err)
	}
	// It must be remembered even though there is no users row.
	if lang := repo.GetLanguage(30); lang != "en" {
		t.Fatalf("pending language = %q, want en — the wizard would render in the wrong language", lang)
	}
	if u, _ := repo.GetByID(30); u != nil {
		t.Fatal("no users row should exist yet")
	}

	// They register with an empty Language: the pending preference is carried in.
	u := newUser(30)
	u.Language = ""
	if err := repo.Create(u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, _ := repo.GetByID(30)
	if got.Language != "en" {
		t.Fatalf("registered user language = %q, want en (carried from pending pref)", got.Language)
	}

	// The pending preference row must have been dropped.
	var pref UserPreference
	if err := Get().First(&pref, "telegram_id = ?", 30).Error; err == nil {
		t.Fatal("pending preference should have been deleted after registration")
	}
}

// TestUpdateLanguageForRegisteredUser updates the row in place.
func TestUpdateLanguageForRegisteredUser(t *testing.T) {
	clearAll(t)
	repo := NewUserRepository()
	repo.Create(newUser(31)) // language "it"

	if err := repo.UpdateLanguage(31, "de"); err != nil {
		t.Fatalf("UpdateLanguage: %v", err)
	}
	if lang := repo.GetLanguage(31); lang != "de" {
		t.Fatalf("language = %q, want de", lang)
	}
	// No stray preference row is created for a registered user.
	var pref UserPreference
	if err := Get().First(&pref, "telegram_id = ?", 31).Error; err == nil {
		t.Fatal("no pending preference should be created for a registered user")
	}
}

func TestUpdateLanguageIdempotentPending(t *testing.T) {
	clearAll(t)
	repo := NewUserRepository()
	// Two updates for the same unregistered user must not error (upsert).
	if err := repo.UpdateLanguage(32, "en"); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := repo.UpdateLanguage(32, "es"); err != nil {
		t.Fatalf("second: %v", err)
	}
	if lang := repo.GetLanguage(32); lang != "es" {
		t.Fatalf("language = %q, want es", lang)
	}
}

// --- Pending join requests ----------------------------------------------

func TestPendingJoinRequests(t *testing.T) {
	clearAll(t)
	repo := NewPendingJoinRequestRepository()

	repo.Add(40, 1000)
	repo.Add(40, 1001)
	repo.Add(40, 1000) // idempotent

	reqs, err := repo.GetForUser(40)
	if err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if len(reqs) != 2 {
		t.Fatalf("expected 2 pending requests, got %d", len(reqs))
	}

	if err := repo.DeleteForUser(40); err != nil {
		t.Fatalf("DeleteForUser: %v", err)
	}
	reqs, _ = repo.GetForUser(40)
	if len(reqs) != 0 {
		t.Fatalf("expected 0 after DeleteForUser, got %d", len(reqs))
	}
}
