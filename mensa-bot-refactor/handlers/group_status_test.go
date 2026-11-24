package handlers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Mensa-Italia/mensa-telegram-bot/database"
	"github.com/Mensa-Italia/mensa-telegram-bot/internal/testtg"
)

const (
	grpAlertAdmin int64 = 990200
	grpA          int64 = -1005000000001
	grpB          int64 = -1005000000002
	grpC          int64 = -1005000000003
)

func setupGroupTests(t *testing.T) {
	t.Helper()
	setupDelete(t)
	if err := database.SeedBotAdmins([]int64{grpAlertAdmin}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { _ = database.SeedBotAdmins(nil) })
}

func botStatusUpdate(groupID int64, title string, from int64, oldStatus, newStatus models.ChatMemberType) *models.Update {
	return &models.Update{MyChatMember: &models.ChatMemberUpdated{
		Chat:          models.Chat{ID: groupID, Type: models.ChatTypeSupergroup, Title: title},
		From:          models.User{ID: from, FirstName: "Gianni"},
		OldChatMember: models.ChatMember{Type: oldStatus},
		NewChatMember: models.ChatMember{Type: newStatus},
	}}
}

func storedGroup(t *testing.T, id int64) *database.Group {
	t.Helper()
	g, err := database.NewGroupRepository().GetByID(id)
	if err != nil || g == nil {
		t.Fatalf("group %d: %+v, %v", id, g, err)
	}
	return g
}

func adminAlerts() []testtg.Call {
	var out []testtg.Call
	for _, c := range fakeTG.Calls("sendMessage") {
		if c.ChatID == grpAlertAdmin {
			out = append(out, c)
		}
	}
	return out
}

func TestBotJoiningRecordsStatusAndTitle(t *testing.T) {
	setupGroupTests(t)

	MyChatMemberHandler(context.Background(), nil, botStatusUpdate(grpA, "Mensa Piemonte", 1, models.ChatMemberTypeLeft, models.ChatMemberTypeMember))

	if g := storedGroup(t, grpA); g.Status != database.GroupStatusMember || g.Title != "Mensa Piemonte" {
		t.Fatalf("got %+v", g)
	}
	if len(adminAlerts()) != 0 {
		t.Fatal("joining is not an alert")
	}

	MyChatMemberHandler(context.Background(), nil, botStatusUpdate(grpA, "Mensa Piemonte", 1, models.ChatMemberTypeMember, models.ChatMemberTypeAdministrator))
	if g := storedGroup(t, grpA); g.Status != database.GroupStatusAdmin {
		t.Fatalf("promotion not recorded: %+v", g)
	}
}

// The bug: leaving used to be ignored, so the group stayed "active" forever.
func TestBotRemovedIsRecordedAndAlerted(t *testing.T) {
	setupGroupTests(t)
	addMember(t, 720001, grpA)
	if err := database.NewGroupRepository().SetStatus(grpA, database.GroupStatusAdmin, "Mensa Sicilia"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	MyChatMemberHandler(context.Background(), nil, botStatusUpdate(grpA, "Mensa Sicilia", 555, models.ChatMemberTypeAdministrator, models.ChatMemberTypeBanned))

	if g := storedGroup(t, grpA); g.Status != database.GroupStatusLeft {
		t.Fatalf("removal not recorded: %+v", g)
	}
	if assocCount(t, 720001) != 1 {
		t.Fatal("associations are kept when the bot leaves, in case it is added back")
	}
	a := adminAlerts()
	if len(a) != 1 || !strings.Contains(a[0].Text, "removed from") || !strings.Contains(a[0].Text, "Mensa Sicilia") || !strings.Contains(a[0].Text, "555") {
		t.Fatalf("want one removal alert naming the group and who did it, got %+v", a)
	}

	// Added back: active again.
	MyChatMemberHandler(context.Background(), nil, botStatusUpdate(grpA, "Mensa Sicilia", 555, models.ChatMemberTypeLeft, models.ChatMemberTypeAdministrator))
	if g := storedGroup(t, grpA); g.Status != database.GroupStatusAdmin {
		t.Fatalf("re-adding not recorded: %+v", g)
	}
}

func TestBotDemotedIsRecordedAndAlerted(t *testing.T) {
	setupGroupTests(t)

	MyChatMemberHandler(context.Background(), nil, botStatusUpdate(grpA, "Mensa Puglia", 9, models.ChatMemberTypeAdministrator, models.ChatMemberTypeMember))

	if g := storedGroup(t, grpA); g.Status != database.GroupStatusMember {
		t.Fatalf("demotion not recorded: %+v", g)
	}
	if a := adminAlerts(); len(a) != 1 || !strings.Contains(a[0].Text, "no longer an admin") {
		t.Fatalf("want one demotion alert, got %+v", a)
	}
}

// The bug this PR fixes: one group the bot had left made every member's sync incomplete,
// so groups_synced_at stayed NULL and the cleanup sweep deferred everyone forever.
func TestSyncSkipsLeftGroupsAndHealsVanishedOnes(t *testing.T) {
	setupGroupTests(t)
	repo := database.NewGroupRepository()
	_ = repo.SetStatus(grpA, database.GroupStatusAdmin, "A")
	_ = repo.SetStatus(grpB, database.GroupStatusAdmin, "B") // bot was removed while down
	_ = repo.SetStatus(grpC, database.GroupStatusLeft, "C")  // already known to be left
	addMember(t, 720010)
	database.Get().Model(&database.User{}).Where("telegram_id = ?", 720010).Update("groups_synced_at", nil)

	fakeTG.Fail("getChatMember", grpB, 0, testtg.ErrChatNotFound)

	if err := SyncMemberGroups(context.Background(), 720010); err != nil {
		t.Fatalf("a vanished group must not make the sync incomplete: %v", err)
	}

	u, _ := database.NewUserRepository().GetByID(720010)
	if u == nil || u.GroupsSyncedAt == nil {
		t.Fatal("the member should be stamped as synced")
	}
	if assocCount(t, 720010) != 1 {
		t.Fatalf("want an association with group A only, got %d", assocCount(t, 720010))
	}
	if fakeTG.Called("getChatMember", grpC, 0) {
		t.Fatal("a group the bot has left must not be probed")
	}
	if g := storedGroup(t, grpB); g.Status != database.GroupStatusLeft {
		t.Fatalf("the vanished group should be marked left: %+v", g)
	}
	if len(adminAlerts()) != 1 {
		t.Fatalf("want one alert for the vanished group, got %+v", adminAlerts())
	}
}

// A transient failure is still an unknown answer: the sync must stay unstamped.
func TestSyncStaysIncompleteOnTransientError(t *testing.T) {
	setupGroupTests(t)
	_ = database.NewGroupRepository().SetStatus(grpA, database.GroupStatusAdmin, "A")
	addMember(t, 720011)
	database.Get().Model(&database.User{}).Where("telegram_id = ?", 720011).Update("groups_synced_at", nil)
	fakeTG.Fail("getChatMember", grpA, 720011, testtg.ErrTransient)

	if err := SyncMemberGroups(context.Background(), 720011); err == nil {
		t.Fatal("a transient error must leave the sync incomplete")
	}
	if g := storedGroup(t, grpA); g.Status != database.GroupStatusAdmin {
		t.Fatal("a transient error must not mark the group left")
	}
}

func TestProfileHidesLeftGroups(t *testing.T) {
	setupGroupTests(t)
	addMember(t, 720020, grpA, grpB)
	_ = database.NewGroupRepository().SetStatus(grpB, database.GroupStatusLeft, "")

	active, err := database.NewGroupAssociationRepository().GetUserActiveGroups(720020)
	if err != nil || len(active) != 1 || active[0].GroupID != grpA {
		t.Fatalf("want only group A, got %+v, %v", active, err)
	}
	all, _ := database.NewGroupAssociationRepository().GetUserGroups(720020)
	if len(all) != 2 {
		t.Fatal("the removal paths still see both groups")
	}
}

func TestFormatGroups(t *testing.T) {
	if msg := FormatGroups(nil, nil); !strings.Contains(msg, "No groups") {
		t.Fatalf("empty: %q", msg)
	}
	now := time.Now()
	msg := FormatGroups([]database.Group{
		{GroupID: 3, Status: database.GroupStatusLeft, Title: "Gone", UpdatedAt: now},
		{GroupID: 1, Status: database.GroupStatusAdmin, Title: "Mensa <Roma>", UpdatedAt: now},
		{GroupID: 2, Status: database.GroupStatusMember, UpdatedAt: now},
		{GroupID: 4, Status: ""},
	}, map[int64]int{1: 12})
	for _, want := range []string{
		"4 group(s)", "1 admin · 1 not admin · 1 unknown · 1 left", "cannot approve join requests",
		"Mensa &lt;Roma&gt;</b> · admin · 12 member(s)", "(no title recorded)", "status unknown",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("groups %q is missing %q", msg, want)
		}
	}
	if strings.Index(msg, "Mensa &lt;Roma&gt;") > strings.Index(msg, "Gone") {
		t.Fatal("admin groups should be listed before left ones")
	}
}

func TestGroupsCommandForAdmin(t *testing.T) {
	setupGroupTests(t)
	_ = database.NewGroupRepository().SetStatus(grpA, database.GroupStatusAdmin, "Mensa Toscana")

	GroupsCommandHandler(context.Background(), nil, privateCommand(grpAlertAdmin, "/groups"))

	if a := adminAlerts(); len(a) != 1 || !strings.Contains(a[0].Text, "Mensa Toscana") {
		t.Fatalf("want the group list, got %+v", a)
	}
}
