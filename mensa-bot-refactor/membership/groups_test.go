package membership

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
	"github.com/Bombaclath97/mensa-telegram-bot/internal/testtg"
	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
)

const alertAdmin int64 = 990100

func setupGroups(t *testing.T) {
	t.Helper()
	setupPending(t)
	if err := database.SeedBotAdmins([]int64{alertAdmin}); err != nil {
		t.Fatalf("seed admins: %v", err)
	}
	t.Cleanup(func() { _ = database.SeedBotAdmins(nil) })
}

func groupRow(t *testing.T, id int64) *database.Group {
	t.Helper()
	g, err := database.NewGroupRepository().GetByID(id)
	if err != nil || g == nil {
		t.Fatalf("group %d: %+v, %v", id, g, err)
	}
	return g
}

func alertsSent() []testtg.Call {
	var out []testtg.Call
	for _, c := range fakeTG.Calls("sendMessage") {
		if c.ChatID == alertAdmin {
			out = append(out, c)
		}
	}
	return out
}

func TestBotStatus(t *testing.T) {
	cases := map[models.ChatMemberType]string{
		models.ChatMemberTypeAdministrator: database.GroupStatusAdmin,
		models.ChatMemberTypeOwner:         database.GroupStatusAdmin,
		models.ChatMemberTypeMember:        database.GroupStatusMember,
		models.ChatMemberTypeRestricted:    database.GroupStatusMember,
		models.ChatMemberTypeLeft:          database.GroupStatusLeft,
		models.ChatMemberTypeBanned:        database.GroupStatusLeft,
	}
	for in, want := range cases {
		if got := BotStatus(in); got != want {
			t.Fatalf("BotStatus(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestIsBotGone(t *testing.T) {
	for _, msg := range []string{"Bad Request: chat not found", "Forbidden: bot was kicked from the supergroup chat", "Forbidden: bot is not a member of the supergroup chat"} {
		if !IsBotGone(errors.New(msg)) {
			t.Fatalf("%q means the bot is gone", msg)
		}
	}
	// "user not found" is about the USER, not the bot: it must never mark a group as left.
	for _, msg := range []string{"Bad Request: user not found", "Internal Server Error"} {
		if IsBotGone(errors.New(msg)) {
			t.Fatalf("%q must not mean the bot is gone", msg)
		}
	}
	if IsBotGone(nil) {
		t.Fatal("nil is not gone")
	}
}

func TestRecordBotGoneMarksLeftAndAlertsOnce(t *testing.T) {
	setupGroups(t)
	repo := database.NewGroupRepository()
	if err := repo.SetStatus(chat, database.GroupStatusAdmin, "Mensa Veneto"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	if !RecordBotGone(context.Background(), chat, errors.New("Forbidden: bot was kicked from the supergroup chat")) {
		t.Fatal("want the group newly marked as left")
	}
	if g := groupRow(t, chat); g.Status != database.GroupStatusLeft {
		t.Fatalf("status = %q, want left", g.Status)
	}
	a := alertsSent()
	if len(a) != 1 || !strings.Contains(a[0].Text, "Mensa Veneto") {
		t.Fatalf("want one alert naming the group, got %+v", a)
	}

	// Already left: no second alert.
	if RecordBotGone(context.Background(), chat, errors.New("Bad Request: chat not found")) || len(alertsSent()) != 1 {
		t.Fatal("a group already marked left must not alert again")
	}
	// Not a bot-gone error: nothing happens.
	if RecordBotGone(context.Background(), chat, errors.New("Internal Server Error")) {
		t.Fatal("a transient error must not mark anything")
	}
}

func TestRefreshGroups(t *testing.T) {
	setupGroups(t)
	repo := database.NewGroupRepository()
	const (
		gAdmin   int64 = -1004000000001
		gKicked  int64 = -1004000000002
		gFlaky   int64 = -1004000000003
		gDemoted int64 = -1004000000004
	)
	for _, g := range []int64{gAdmin, gKicked, gFlaky, gDemoted} {
		if err := repo.Register(g); err != nil { // status "" like a pre-migration row
			t.Fatalf("Register: %v", err)
		}
	}
	botID := tb.BotID()
	fakeTG.SetStatus(gAdmin, botID, "administrator")
	fakeTG.SetTitle(gAdmin, "Mensa Lazio")
	fakeTG.Fail("getChatMember", gKicked, botID, &testtg.APIError{Code: 403, Description: "Forbidden: bot was kicked from the supergroup chat"})
	fakeTG.Fail("getChatMember", gFlaky, botID, testtg.ErrTransient)
	fakeTG.SetStatus(gDemoted, botID, "member")

	r := RefreshGroups(context.Background())

	if g := groupRow(t, gAdmin); g.Status != database.GroupStatusAdmin || g.Title != "Mensa Lazio" {
		t.Fatalf("admin group: %+v", g)
	}
	if g := groupRow(t, gKicked); g.Status != database.GroupStatusLeft {
		t.Fatalf("kicked group: %+v", g)
	}
	if g := groupRow(t, gFlaky); g.Status != "" {
		t.Fatalf("an unreadable group must be left as it was: %+v", g)
	}
	if g := groupRow(t, gDemoted); g.Status != database.GroupStatusMember {
		t.Fatalf("non-admin group: %+v", g)
	}
	if r.Total != 4 || r.Updated != 2 || r.Left != 1 || r.Failed != 1 {
		t.Fatalf("unexpected report: %+v", r)
	}
	if len(alertsSent()) != 1 {
		t.Fatalf("want one alert for the kicked group, got %+v", alertsSent())
	}
}

// A pending removal in a group the bot was kicked from resolves, and the group is marked.
func TestDrainMarksVanishedGroupLeft(t *testing.T) {
	setupGroups(t)
	if err := database.NewGroupRepository().SetStatus(chat, database.GroupStatusAdmin, ""); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	addPending(t, 810020, chat, 0)
	fakeTG.Fail("getChatMember", chat, 0, testtg.ErrChatNotFound)

	DrainPendingRemovals(context.Background())

	if pendingRow(t, 810020, chat) != nil {
		t.Fatal("a removal from a vanished group is resolved")
	}
	if g := groupRow(t, chat); g.Status != database.GroupStatusLeft {
		t.Fatalf("the vanished group should be marked left: %+v", g)
	}
}
