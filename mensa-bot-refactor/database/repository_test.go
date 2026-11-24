package database

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Mensa-Italia/mensa-telegram-bot/internal/testdb"
)

// TestMain runs the whole package against a real, throwaway PostgreSQL container — the same
// engine as production.
//
// That is the point. This schema leans on foreign keys with ON DELETE CASCADE, and several
// tests below exist purely to prove those cascades fire. The suite used to run on SQLite,
// which disables foreign keys PER CONNECTION by default and needed a DSN hack to behave;
// testing on one engine while shipping another is exactly where this class of bug hides.
//
// Skips rather than fails when Docker is unavailable, so `go test ./...` on a machine with no
// daemon reports honestly instead of exploding.
func TestMain(m *testing.M) {
	ctx := context.Background()

	dsn, terminate, err := testdb.Start(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: no postgres container (is Docker running?): %v\n", err)
		os.Exit(0)
	}
	os.Setenv("DATABASE_URL", dsn)

	if err := Init(); err != nil {
		terminate()
		panic("database.Init failed: " + err.Error())
	}

	code := m.Run()

	_ = Close()
	terminate()
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

// TestMigrateGroupIDToFreshTarget covers the simple case: the supergroup row does not exist
// yet, so the associations move wholesale.
func TestMigrateGroupIDToFreshTarget(t *testing.T) {
	clearAll(t)
	users := NewUserRepository()
	groups := NewGroupRepository()
	assoc := NewGroupAssociationRepository()

	const oldID, newID = -5536498156, -1004435267124

	users.Create(newUserUnique(30))
	users.Create(newUserUnique(31))
	assoc.Register(30, oldID)
	assoc.Register(31, oldID)
	assoc.PromoteToAdmin(31, oldID)

	if err := groups.MigrateGroupID(oldID, newID); err != nil {
		t.Fatalf("MigrateGroupID: %v", err)
	}

	if exists, _ := groups.Exists(oldID); exists {
		t.Error("the old group row survived the migration")
	}
	if exists, _ := groups.Exists(newID); !exists {
		t.Fatal("the new group row is missing")
	}

	for _, userID := range []int64{30, 31} {
		if got, _ := assoc.Get(userID, newID); got == nil {
			t.Errorf("user %d has no association under the new id", userID)
		}
		if got, _ := assoc.Get(userID, oldID); got != nil {
			t.Errorf("user %d still has an association under the old id", userID)
		}
	}

	got, _ := assoc.Get(31, newID)
	if got == nil || !got.IsGroupAdmin {
		t.Errorf("admin flag lost in migration: %+v", got)
	}
}

// TestMigrateGroupIDMergesIntoExistingTarget is the case a bare
// `UPDATE groups SET group_id = ...` cannot handle: the destination row ALREADY exists,
// because group_handler registered the upgraded chat when Telegram announced the bot's
// membership in it. A plain update would collide on the primary key.
func TestMigrateGroupIDMergesIntoExistingTarget(t *testing.T) {
	clearAll(t)
	users := NewUserRepository()
	groups := NewGroupRepository()
	assoc := NewGroupAssociationRepository()

	const oldID, newID = -5536498156, -1004435267124

	users.Create(newUserUnique(40)) // in both
	users.Create(newUserUnique(41)) // only in the old group
	users.Create(newUserUnique(42)) // only in the new group

	assoc.Register(40, oldID)
	assoc.Register(41, oldID)
	assoc.Register(40, newID)
	assoc.Register(42, newID)

	if err := groups.MigrateGroupID(oldID, newID); err != nil {
		t.Fatalf("MigrateGroupID: %v", err)
	}

	if exists, _ := groups.Exists(oldID); exists {
		t.Error("the old group row survived the migration")
	}

	for _, userID := range []int64{40, 41, 42} {
		if got, _ := assoc.Get(userID, newID); got == nil {
			t.Errorf("user %d is missing from the merged group", userID)
		}
	}

	rows, _ := assoc.GetUserGroups(40)
	if len(rows) != 1 {
		t.Errorf("user 40 has %d associations after the merge, want 1 (deduplicated)", len(rows))
	}
}

// TestMigrateGroupIDNeverDemotes: a member who was an admin under the OLD id but a plain
// member under the new one must stay an admin. Overwriting instead of OR-ing would silently
// strip their rights during what is meant to be a transparent id change.
func TestMigrateGroupIDNeverDemotes(t *testing.T) {
	clearAll(t)
	users := NewUserRepository()
	groups := NewGroupRepository()
	assoc := NewGroupAssociationRepository()

	const oldID, newID = -5536498156, -1004435267124

	users.Create(newUserUnique(50))
	assoc.Register(50, oldID)
	assoc.PromoteToAdmin(50, oldID)
	assoc.Register(50, newID) // present but NOT an admin here

	if err := groups.MigrateGroupID(oldID, newID); err != nil {
		t.Fatalf("MigrateGroupID: %v", err)
	}

	got, _ := assoc.Get(50, newID)
	if got == nil || !got.IsGroupAdmin {
		t.Errorf("migration demoted an admin: %+v", got)
	}
}

// TestMigrateGroupIDIsIdempotent: the repair script and the two self-healing code paths can
// all fire for the same upgrade, so a second run must be a no-op rather than an error.
func TestMigrateGroupIDIsIdempotent(t *testing.T) {
	clearAll(t)
	users := NewUserRepository()
	groups := NewGroupRepository()
	assoc := NewGroupAssociationRepository()

	const oldID, newID = -5536498156, -1004435267124

	users.Create(newUserUnique(60))
	assoc.Register(60, oldID)

	for i := 0; i < 3; i++ {
		if err := groups.MigrateGroupID(oldID, newID); err != nil {
			t.Fatalf("MigrateGroupID iteration %d: %v", i, err)
		}
	}

	rows, _ := assoc.GetUserGroups(60)
	if len(rows) != 1 {
		t.Errorf("user 60 has %d associations, want 1", len(rows))
	}

	// Same id in and out must not delete the row it was asked to keep.
	if err := groups.MigrateGroupID(newID, newID); err != nil {
		t.Fatalf("self-migration: %v", err)
	}
	if exists, _ := groups.Exists(newID); !exists {
		t.Error("migrating a group onto itself deleted it")
	}
}

// TestGetUnsyncedGroupIDs backs the backfill: it must return exactly the members the cleanup
// sweep is deferring, and stop returning them once they are stamped.
func TestGetUnsyncedGroupIDs(t *testing.T) {
	clearAll(t)
	users := NewUserRepository()

	users.Create(newUserUnique(70))
	users.Create(newUserUnique(71))

	ids, err := users.GetUnsyncedGroupIDs()
	if err != nil {
		t.Fatalf("GetUnsyncedGroupIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("got %d unsynced members, want 2 (a fresh user is never stamped)", len(ids))
	}

	if err := users.MarkGroupsSynced(70, time.Now()); err != nil {
		t.Fatalf("MarkGroupsSynced: %v", err)
	}

	ids, _ = users.GetUnsyncedGroupIDs()
	if len(ids) != 1 || ids[0] != 71 {
		t.Fatalf("after stamping user 70, got %v, want [71]", ids)
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

// TestSeedBotAdminsRevokesRemovedAdmin: taking an ID OUT of ADMIN_TELEGRAM_IDS must take the
// right away.
//
// This is the regression that matters. is_bot_admin used to be a one-way latch: the promotion
// was its only writer, so an admin removed from the env kept the flag on their row and
// IsBotAdmin's DB fallback went on answering true forever. There was no way to demote anyone
// short of editing the database by hand.
func TestSeedBotAdminsRevokesRemovedAdmin(t *testing.T) {
	clearAll(t)
	repo := NewUserRepository()
	repo.Create(newUser(558))
	repo.Create(newUserUnique(559))

	// Both are admins.
	if err := SeedBotAdmins([]int64{558, 559}); err != nil {
		t.Fatalf("SeedBotAdmins: %v", err)
	}
	if admin, _ := repo.IsBotAdmin(558); !admin {
		t.Fatal("558 should be an admin after the first seed")
	}

	// Reboot with 558 removed from the list.
	if err := SeedBotAdmins([]int64{559}); err != nil {
		t.Fatalf("SeedBotAdmins: %v", err)
	}

	got, _ := repo.GetByID(558)
	if got.IsBotAdmin {
		t.Fatal("558 was removed from ADMIN_TELEGRAM_IDS: the is_bot_admin column must be cleared")
	}
	if admin, _ := repo.IsBotAdmin(558); admin {
		t.Fatal("IsBotAdmin must report false for a revoked admin (the DB fallback used to keep saying true)")
	}
	if IsSeededBotAdmin(558) {
		t.Fatal("558 must be out of the seeded set")
	}

	// 559 is untouched.
	if admin, _ := repo.IsBotAdmin(559); !admin {
		t.Fatal("559 is still in the list and must remain an admin")
	}
}

// TestSeedBotAdminsEmptyListRevokesEveryone: .env.example promises "Leave blank to disable
// /manual_add entirely" — so a blank ADMIN_TELEGRAM_IDS must actually demote everyone, not
// quietly leave the incumbents in place.
func TestSeedBotAdminsEmptyListRevokesEveryone(t *testing.T) {
	clearAll(t)
	repo := NewUserRepository()
	repo.Create(newUser(560))

	if err := SeedBotAdmins([]int64{560}); err != nil {
		t.Fatalf("SeedBotAdmins: %v", err)
	}
	if admin, _ := repo.IsBotAdmin(560); !admin {
		t.Fatal("560 should be an admin after seeding")
	}

	// Blank the env var and reboot.
	if err := SeedBotAdmins(nil); err != nil {
		t.Fatalf("SeedBotAdmins(nil): %v", err)
	}

	if got, _ := repo.GetByID(560); got.IsBotAdmin {
		t.Fatal("an empty ADMIN_TELEGRAM_IDS must leave nobody flagged is_bot_admin")
	}
	if admin, _ := repo.IsBotAdmin(560); admin {
		t.Fatal("IsBotAdmin must report false once the list is emptied")
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
