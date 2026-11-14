package handlers

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/internal/testdb"
	"github.com/Bombaclath97/mensa-telegram-bot/internal/testtg"
)

// These tests cover DeleteMemberEverywhere, the shared "remove this member" path behind
// /elimina and the profile card's delete button. They run against a real Postgres and the
// fake Bot API in internal/testtg.
//
// The other tests in this package are pure and must keep running without Docker, so a
// missing database skips only these tests (dbReady), not the whole package.

var (
	fakeTG  *testtg.Server
	dbReady bool
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	var err error
	fakeTG, err = testtg.Install()
	if err != nil {
		panic("testtg.Install failed: " + err.Error())
	}

	terminate := func() {}
	dsn, stop, err := testdb.Start(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "handlers: no postgres (is Docker running?), DB tests will skip: %v\n", err)
	} else {
		terminate = stop
		os.Setenv("DATABASE_URL", dsn)
		if err := database.Init(); err != nil {
			terminate()
			panic("database.Init failed: " + err.Error())
		}
		dbReady = true
	}

	code := m.Run()

	if dbReady {
		_ = database.Close()
	}
	terminate()
	fakeTG.Close()
	os.Exit(code)
}

const (
	delGroupA int64 = -1002000000001
	delGroupB int64 = -1002000000002
)

func setupDelete(t *testing.T) {
	t.Helper()
	if !dbReady {
		t.Skip("no postgres available")
	}
	for _, table := range []string{
		"groups_associations", "pending_join_requests", "user_preferences", "users", "groups",
	} {
		if err := database.Get().Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
	fakeTG.Reset()
}

func addMember(t *testing.T, telegramID int64, groups ...int64) {
	t.Helper()
	now := time.Now()
	u := &database.User{
		TelegramID:     telegramID,
		MensaEmail:     fmt.Sprintf("user%d@mensa.it", telegramID),
		FirstName:      "F",
		LastName:       "L",
		MemberNumber:   telegramID % 100000,
		Language:       "it",
		GroupsSyncedAt: &now,
	}
	if err := database.Get().Create(u).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	for _, g := range groups {
		if err := database.Get().FirstOrCreate(&database.Group{GroupID: g}).Error; err != nil {
			t.Fatalf("insert group: %v", err)
		}
		if err := database.Get().Create(&database.GroupAssociation{UserID: telegramID, GroupID: g}).Error; err != nil {
			t.Fatalf("insert association: %v", err)
		}
	}
}

func memberExists(t *testing.T, telegramID int64) bool {
	t.Helper()
	var n int64
	database.Get().Model(&database.User{}).Where("telegram_id = ?", telegramID).Count(&n)
	return n > 0
}

func assocCount(t *testing.T, telegramID int64) int64 {
	t.Helper()
	var n int64
	database.Get().Model(&database.GroupAssociation{}).Where("user_id = ?", telegramID).Count(&n)
	return n
}

// A plain member is kicked (ban + unban, never a permaban) from every group, then deleted.
func TestDeleteMemberEverywhereKicksFromAllGroups(t *testing.T) {
	setupDelete(t)
	addMember(t, 700001, delGroupA, delGroupB)

	if err := DeleteMemberEverywhere(context.Background(), 700001, "it"); err != nil {
		t.Fatalf("DeleteMemberEverywhere: %v", err)
	}

	for _, g := range []int64{delGroupA, delGroupB} {
		if !fakeTG.Kicked(g, 700001) {
			t.Fatalf("member not kicked from group %d", g)
		}
		if fakeTG.Demoted(g, 700001) {
			t.Fatalf("a plain member must not be demoted in group %d", g)
		}
	}
	if memberExists(t, 700001) || assocCount(t, 700001) != 0 {
		t.Fatal("row and associations should be gone")
	}
}

// An admin is demoted before the kick.
func TestDeleteMemberEverywhereDemotesAdminFirst(t *testing.T) {
	setupDelete(t)
	addMember(t, 700002, delGroupA)
	fakeTG.SetStatus(delGroupA, 700002, "administrator")

	if err := DeleteMemberEverywhere(context.Background(), 700002, "it"); err != nil {
		t.Fatalf("DeleteMemberEverywhere: %v", err)
	}

	demoteAt, banAt := -1, -1
	for i, c := range fakeTG.Calls("") {
		switch {
		case c.Method == "promoteChatMember" && demoteAt < 0:
			demoteAt = i
		case c.Method == "banChatMember" && banAt < 0:
			banAt = i
		}
	}
	if demoteAt < 0 || banAt < 0 || demoteAt > banAt {
		t.Fatalf("admin must be demoted before being kicked (demote at %d, ban at %d)", demoteAt, banAt)
	}
	if memberExists(t, 700002) {
		t.Fatal("row should be gone")
	}
}

// The owner cannot be kicked. The group is told instead, and the row is still deleted.
func TestDeleteMemberEverywhereWarnsGroupForOwner(t *testing.T) {
	setupDelete(t)
	addMember(t, 700003, delGroupA, delGroupB)
	fakeTG.SetStatus(delGroupA, 700003, "creator")

	if err := DeleteMemberEverywhere(context.Background(), 700003, "it"); err != nil {
		t.Fatalf("DeleteMemberEverywhere: %v", err)
	}

	if fakeTG.Called("banChatMember", delGroupA, 700003) || fakeTG.Demoted(delGroupA, 700003) {
		t.Fatal("the owner must never be kicked or demoted")
	}
	if !fakeTG.Called("sendMessage", delGroupA, 0) {
		t.Fatal("the group should be told its owner left")
	}
	if !fakeTG.Kicked(delGroupB, 700003) {
		t.Fatal("the owner of A is a plain member of B and should be kicked there")
	}
	if memberExists(t, 700003) {
		t.Fatal("row should be gone")
	}
}

// A group the bot can no longer see does not block the deletion.
func TestDeleteMemberEverywhereSkipsUnreachableGroup(t *testing.T) {
	setupDelete(t)
	addMember(t, 700004, delGroupA, delGroupB)
	fakeTG.Fail("getChatMember", delGroupA, 0, testtg.ErrChatNotFound)

	if err := DeleteMemberEverywhere(context.Background(), 700004, "it"); err != nil {
		t.Fatalf("DeleteMemberEverywhere: %v", err)
	}

	if fakeTG.Called("banChatMember", delGroupA, 0) {
		t.Fatal("no kick should be attempted in an unreachable group")
	}
	if !fakeTG.Kicked(delGroupB, 700004) {
		t.Fatal("the reachable group should still be handled")
	}
	if memberExists(t, 700004) {
		t.Fatal("row should be gone")
	}
}

// Only the requested member is affected; others in the same group are untouched.
func TestDeleteMemberEverywhereTouchesNobodyElse(t *testing.T) {
	setupDelete(t)
	addMember(t, 700005, delGroupA)
	addMember(t, 700006, delGroupA)

	if err := DeleteMemberEverywhere(context.Background(), 700005, "it"); err != nil {
		t.Fatalf("DeleteMemberEverywhere: %v", err)
	}

	for _, c := range fakeTG.Calls("") {
		if c.UserID == 700006 {
			t.Fatalf("bystander was touched by %s", c.Method)
		}
	}
	if !memberExists(t, 700006) || assocCount(t, 700006) != 1 {
		t.Fatal("bystander must keep their row and association")
	}
}

// A member with no groups is simply deleted, with no Telegram calls.
func TestDeleteMemberEverywhereWithNoGroups(t *testing.T) {
	setupDelete(t)
	addMember(t, 700007)

	if err := DeleteMemberEverywhere(context.Background(), 700007, "it"); err != nil {
		t.Fatalf("DeleteMemberEverywhere: %v", err)
	}
	if n := len(fakeTG.Calls("")); n != 0 {
		t.Fatalf("expected no Telegram calls, got %d", n)
	}
	if memberExists(t, 700007) {
		t.Fatal("row should be gone")
	}
}
