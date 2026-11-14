package cleanup

import (
	"context"
	"testing"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/internal/testtg"
)

// These tests cover the part of the sweep that acts on Telegram: kick, demote-then-kick, the
// owner warning, stale groups, and partial failures. They run against the fake Bot API in
// internal/testtg, so every assertion is about calls that would have reached real groups.
//
// DEPLOY.md lists these as a precondition for setting CLEANUP_DRY_RUN=false.

const (
	groupA int64 = -1001000000001
	groupB int64 = -1001000000002
)

func insertAssoc(t *testing.T, userID, groupID int64) {
	t.Helper()
	if err := database.Get().FirstOrCreate(&database.Group{GroupID: groupID}).Error; err != nil {
		t.Fatalf("insert group %d: %v", groupID, err)
	}
	if err := database.Get().Create(&database.GroupAssociation{UserID: userID, GroupID: groupID}).Error; err != nil {
		t.Fatalf("insert association %d/%d: %v", userID, groupID, err)
	}
}

func assocExists(t *testing.T, userID, groupID int64) bool {
	t.Helper()
	var n int64
	database.Get().Model(&database.GroupAssociation{}).
		Where("user_id = ? AND group_id = ?", userID, groupID).Count(&n)
	return n > 0
}

// armDestructive resets the fake, points the sweep at a mock API and arms real deletion.
func armDestructive(t *testing.T, active map[string]bool) {
	t.Helper()
	clearDB(t)
	fakeTG.Reset()
	newMockAPI(t, active).arm(t)
	t.Setenv("CLEANUP_DRY_RUN", "false")
	t.Setenv("CLEANUP_MAX_DELETIONS", "10")
}

func mustRun(t *testing.T) Report {
	t.Helper()
	report, err := RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	return report
}

// An expired ordinary member is kicked from every group, told why, and deleted.
func TestExpiredMemberIsKickedFromEveryGroup(t *testing.T) {
	armDestructive(t, map[string]bool{"101": false})
	insertUser(t, 600001, 101)
	insertAssoc(t, 600001, groupA)
	insertAssoc(t, 600001, groupB)

	report := mustRun(t)

	for _, g := range []int64{groupA, groupB} {
		if !fakeTG.Kicked(g, 600001) {
			t.Fatalf("member was not kicked (ban + unban) from group %d", g)
		}
		if fakeTG.Demoted(g, 600001) {
			t.Fatalf("an ordinary member must not be demoted in group %d", g)
		}
	}
	if !fakeTG.Called("sendMessage", 600001, 0) {
		t.Fatal("the member should have been DMed about the expiry")
	}
	if userExists(t, 600001) {
		t.Fatal("the member's row should have been deleted")
	}
	if assocExists(t, 600001, groupA) || assocExists(t, 600001, groupB) {
		t.Fatal("associations should have cascaded away with the row")
	}
	if report.KickedFromGroup != 2 || report.Deleted != 1 || report.Failed != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

// A kick is ban + unban, never a bare ban: an expired member must be able to rejoin once they
// renew.
func TestKickIsNotAPermaban(t *testing.T) {
	armDestructive(t, map[string]bool{"102": false})
	insertUser(t, 600002, 102)
	insertAssoc(t, 600002, groupA)

	mustRun(t)

	bans := len(fakeTG.Calls("banChatMember"))
	unbans := len(fakeTG.Calls("unbanChatMember"))
	if bans != 1 || unbans != 1 {
		t.Fatalf("want exactly one ban and one unban, got %d bans / %d unbans", bans, unbans)
	}
}

// A group admin is demoted BEFORE the kick; Telegram refuses to kick an admin otherwise.
func TestExpiredAdminIsDemotedThenKicked(t *testing.T) {
	armDestructive(t, map[string]bool{"103": false})
	insertUser(t, 600003, 103)
	insertAssoc(t, 600003, groupA)
	fakeTG.SetStatus(groupA, 600003, "administrator")

	mustRun(t)

	demoteAt, banAt := -1, -1
	for i, c := range fakeTG.Calls("") {
		switch {
		case c.Method == "promoteChatMember" && c.UserID == 600003 && demoteAt < 0:
			demoteAt = i
		case c.Method == "banChatMember" && c.UserID == 600003 && banAt < 0:
			banAt = i
		}
	}
	if demoteAt < 0 || banAt < 0 {
		t.Fatalf("admin should be demoted and kicked (demote at %d, ban at %d)", demoteAt, banAt)
	}
	if demoteAt > banAt {
		t.Fatal("the demote must happen before the ban")
	}
	if userExists(t, 600003) {
		t.Fatal("the admin's row should have been deleted")
	}
}

// A failed demote is not fatal: the kick is still attempted, and if it lands the member is
// removed.
func TestFailedDemoteStillAttemptsKick(t *testing.T) {
	armDestructive(t, map[string]bool{"104": false})
	insertUser(t, 600004, 104)
	insertAssoc(t, 600004, groupA)
	fakeTG.SetStatus(groupA, 600004, "administrator")
	fakeTG.Fail("promoteChatMember", groupA, 600004, testtg.ErrTransient)

	mustRun(t)

	if !fakeTG.Kicked(groupA, 600004) {
		t.Fatal("the kick must still be attempted after a failed demote")
	}
	if userExists(t, 600004) {
		t.Fatal("the kick succeeded, so the row should be deleted")
	}
}

// The group owner cannot be kicked or demoted by anyone. The group is warned instead, and the
// owner's row is still deleted.
func TestExpiredOwnerIsNotKickedButGroupIsWarned(t *testing.T) {
	armDestructive(t, map[string]bool{"105": false})
	insertUser(t, 600005, 105)
	insertAssoc(t, 600005, groupA)
	fakeTG.SetStatus(groupA, 600005, "creator")

	report := mustRun(t)

	if fakeTG.Called("banChatMember", groupA, 600005) || fakeTG.Demoted(groupA, 600005) {
		t.Fatal("the owner must never be kicked or demoted")
	}
	if !fakeTG.Called("sendMessage", groupA, 0) {
		t.Fatal("the group should have been warned about its expired owner")
	}
	if report.OwnersWarned != 1 {
		t.Fatalf("owners warned = %d, want 1", report.OwnersWarned)
	}
	if userExists(t, 600005) {
		t.Fatal("the owner's row should still be deleted")
	}
}

// A group the bot can no longer see counts as handled. The stale association is dropped and
// the member is still removed.
func TestGoneGroupIsResolvedNotRetried(t *testing.T) {
	armDestructive(t, map[string]bool{"106": false})
	insertUser(t, 600006, 106)
	insertAssoc(t, 600006, groupA)
	insertAssoc(t, 600006, groupB)
	fakeTG.Fail("getChatMember", groupA, 0, testtg.ErrChatNotFound)

	report := mustRun(t)

	if fakeTG.Called("banChatMember", groupA, 0) {
		t.Fatal("no kick should be attempted in a group the bot cannot see")
	}
	if !fakeTG.Kicked(groupB, 600006) {
		t.Fatal("the member should still be kicked from the reachable group")
	}
	if userExists(t, 600006) {
		t.Fatal("a gone group must not block the removal")
	}
	if report.Failed != 0 {
		t.Fatalf("failed = %d, want 0", report.Failed)
	}
}

// If we cannot read the member's role, we keep the row. Deleting it would cascade away the
// only record of the group they still need kicking from.
func TestTransientRoleLookupFailureKeepsTheRow(t *testing.T) {
	armDestructive(t, map[string]bool{"107": false})
	insertUser(t, 600007, 107)
	insertAssoc(t, 600007, groupA)
	fakeTG.Fail("getChatMember", groupA, 600007, testtg.ErrTransient)

	report := mustRun(t)

	if fakeTG.Called("banChatMember", groupA, 0) {
		t.Fatal("must not kick blind when the role is unknown")
	}
	if !userExists(t, 600007) || !assocExists(t, 600007, groupA) {
		t.Fatal("row and association must survive for the next sweep")
	}
	if fakeTG.Called("sendMessage", 600007, 0) {
		t.Fatal("the member must not be told they were removed when they were not")
	}
	if report.Failed != 1 || report.Deleted != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

// A kick that fails in one group keeps the row, even though the other group succeeded.
func TestFailedKickInOneGroupKeepsTheRow(t *testing.T) {
	armDestructive(t, map[string]bool{"108": false})
	insertUser(t, 600008, 108)
	insertAssoc(t, 600008, groupA)
	insertAssoc(t, 600008, groupB)
	fakeTG.Fail("banChatMember", groupA, 600008, testtg.ErrTransient)

	report := mustRun(t)

	if !fakeTG.Kicked(groupB, 600008) {
		t.Fatal("the working group should still have been handled")
	}
	if !userExists(t, 600008) || !assocExists(t, 600008, groupA) {
		t.Fatal("the row must survive so the failed group is retried next sweep")
	}
	if report.Failed != 1 || report.Deleted != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

// A member who blocked the bot cannot be DMed. The removal must still go through.
func TestBlockedDMDoesNotPreventRemoval(t *testing.T) {
	armDestructive(t, map[string]bool{"109": false})
	insertUser(t, 600009, 109)
	insertAssoc(t, 600009, groupA)
	fakeTG.Fail("sendMessage", 600009, 0, &testtg.APIError{Code: 403, Description: "Forbidden: bot was blocked by the user"})

	mustRun(t)

	if !fakeTG.Kicked(groupA, 600009) || userExists(t, 600009) {
		t.Fatal("a blocked DM must not stop the kick or the deletion")
	}
}

// An active member in the same group is never touched.
func TestActiveMemberIsNeverTouched(t *testing.T) {
	armDestructive(t, map[string]bool{"110": true, "111": false})
	insertUser(t, 600010, 110) // active
	insertUser(t, 600011, 111) // expired
	insertAssoc(t, 600010, groupA)
	insertAssoc(t, 600011, groupA)

	mustRun(t)

	for _, m := range []string{"getChatMember", "banChatMember", "unbanChatMember", "promoteChatMember", "sendMessage"} {
		for _, c := range fakeTG.Calls(m) {
			if c.UserID == 600010 || c.ChatID == 600010 {
				t.Fatalf("active member was touched by %s", m)
			}
		}
	}
	if !userExists(t, 600010) || !assocExists(t, 600010, groupA) {
		t.Fatal("active member must survive with their association")
	}
	if !fakeTG.Kicked(groupA, 600011) {
		t.Fatal("the expired member alongside them should be kicked")
	}
}

// Dry run reads roles but writes nothing to Telegram and deletes nothing.
func TestDryRunMakesNoTelegramWrites(t *testing.T) {
	armDestructive(t, map[string]bool{"112": false, "113": false, "114": false})
	t.Setenv("CLEANUP_DRY_RUN", "true")
	insertUser(t, 600012, 112)
	insertUser(t, 600013, 113)
	insertUser(t, 600014, 114)
	insertAssoc(t, 600012, groupA)
	insertAssoc(t, 600013, groupA)
	insertAssoc(t, 600014, groupB)
	fakeTG.SetStatus(groupA, 600013, "administrator")
	fakeTG.SetStatus(groupB, 600014, "creator")

	report := mustRun(t)

	for _, m := range []string{"banChatMember", "unbanChatMember", "promoteChatMember", "sendMessage"} {
		if n := len(fakeTG.Calls(m)); n != 0 {
			t.Fatalf("dry run made %d %s call(s)", n, m)
		}
	}
	if userCount(t) != 3 {
		t.Fatal("dry run must delete nobody")
	}
	if report.Deleted != 0 || report.KickedFromGroup != 0 {
		t.Fatalf("dry run report must show no real actions: %+v", report)
	}
}

// When the safety limit trips, Telegram receives no requests at all, not even role lookups.
func TestCircuitBreakerMakesNoTelegramCalls(t *testing.T) {
	armDestructive(t, map[string]bool{"121": false, "122": false, "123": false})
	t.Setenv("CLEANUP_MAX_DELETIONS", "2")
	for i, n := range []int64{121, 122, 123} {
		id := int64(600021 + i)
		insertUser(t, id, n)
		insertAssoc(t, id, groupA)
	}

	_, err := RunOnce(context.Background())
	if err != ErrCircuitBreaker {
		t.Fatalf("want ErrCircuitBreaker, got %v", err)
	}
	if n := len(fakeTG.Calls("")); n != 0 {
		t.Fatalf("a tripped breaker must not reach Telegram, got %d call(s)", n)
	}
	if userCount(t) != 3 {
		t.Fatal("a tripped breaker must delete nobody")
	}
}
