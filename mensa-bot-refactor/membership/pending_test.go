package membership

import (
	"context"
	"testing"
	"time"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/internal/testtg"
)

func setupPending(t *testing.T) {
	t.Helper()
	if !dbReady {
		t.Skip("no postgres available")
	}
	for _, table := range []string{
		"pending_removals", "groups_associations", "pending_join_requests", "user_preferences", "users", "groups",
	} {
		if err := database.Get().Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
	fakeTG.Reset()
	t.Setenv("CLEANUP_TELEGRAM_DELAY_MS", "0")
}

func addPending(t *testing.T, userID, groupID int64, attempts int) {
	t.Helper()
	row := &database.PendingRemoval{UserID: userID, GroupID: groupID, Attempts: attempts}
	if err := database.Get().Create(row).Error; err != nil {
		t.Fatalf("insert pending removal: %v", err)
	}
}

func pendingRow(t *testing.T, userID, groupID int64) *database.PendingRemoval {
	t.Helper()
	var rows []database.PendingRemoval
	database.Get().Where("user_id = ? AND group_id = ?", userID, groupID).Find(&rows)
	if len(rows) == 0 {
		return nil
	}
	return &rows[0]
}

func TestDrainResolvesAndDeletesTheRow(t *testing.T) {
	setupPending(t)
	addPending(t, 810001, chat, 0)

	r := DrainPendingRemovals(context.Background())

	if !fakeTG.Kicked(chat, 810001) {
		t.Fatal("the pending removal should have been retried and the member kicked")
	}
	if pendingRow(t, 810001, chat) != nil {
		t.Fatal("a resolved removal must delete its row")
	}
	if r.Resolved != 1 || r.Failed != 0 {
		t.Fatalf("unexpected report: %+v", r)
	}
}

func TestDrainDropsRowWhenMemberAlreadyLeft(t *testing.T) {
	setupPending(t)
	addPending(t, 810002, chat, 0)
	fakeTG.SetStatus(chat, 810002, "left")

	DrainPendingRemovals(context.Background())

	if pendingRow(t, 810002, chat) != nil || len(fakeTG.Calls("banChatMember")) != 0 {
		t.Fatal("a member who already left needs no kick, and the row should go")
	}
}

func TestDrainRecordsFailureAndKeepsTheRow(t *testing.T) {
	setupPending(t)
	addPending(t, 810003, chat, 1)
	fakeTG.Fail("banChatMember", chat, 810003, testtg.ErrTransient)

	r := DrainPendingRemovals(context.Background())

	row := pendingRow(t, 810003, chat)
	if row == nil || row.Attempts != 2 || row.LastError == "" {
		t.Fatalf("want the row kept with attempts=2 and an error, got %+v", row)
	}
	if r.Failed != 1 || r.GaveUp != 0 {
		t.Fatalf("unexpected report: %+v", r)
	}
}

func TestDrainGivesUpAtMaxAttempts(t *testing.T) {
	setupPending(t)
	addPending(t, 810004, chat, MaxPendingAttempts-1)
	fakeTG.Fail("banChatMember", chat, 810004, testtg.ErrTransient)

	r := DrainPendingRemovals(context.Background())
	if r.GaveUp != 1 {
		t.Fatalf("want GaveUp=1, got %+v", r)
	}

	// The row stays as a record for an admin, but is no longer retried.
	fakeTG.Reset()
	DrainPendingRemovals(context.Background())
	if n := len(fakeTG.Calls("")); n != 0 {
		t.Fatalf("a row at MaxPendingAttempts must not be retried, got %d call(s)", n)
	}
	if pendingRow(t, 810004, chat) == nil {
		t.Fatal("the given-up row must stay for an admin to see")
	}
}

// A member who asked to be deleted and then registered again is a member again. Kicking them
// would undo a legitimate join.
func TestDrainDoesNotKickAReregisteredMember(t *testing.T) {
	setupPending(t)
	addPending(t, 810005, chat, 0)
	now := time.Now()
	if err := database.Get().Create(&database.User{
		TelegramID: 810005, MensaEmail: "back@mensa.it", FirstName: "F", LastName: "L",
		MemberNumber: 5, Language: "it", GroupsSyncedAt: &now,
	}).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}

	r := DrainPendingRemovals(context.Background())

	if len(fakeTG.Calls("")) != 0 {
		t.Fatal("a re-registered member must not be touched")
	}
	if pendingRow(t, 810005, chat) != nil {
		t.Fatal("their pending removal should be dropped")
	}
	if r.Reregistered != 1 {
		t.Fatalf("unexpected report: %+v", r)
	}
}
