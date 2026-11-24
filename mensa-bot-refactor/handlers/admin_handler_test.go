package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/Mensa-Italia/mensa-telegram-bot/cleanup"
	"github.com/Mensa-Italia/mensa-telegram-bot/database"
)

func TestFormatSweepPreview(t *testing.T) {
	r := cleanup.Report{
		TotalMembers: 10, Checked: 9, Skipped: 1, Expired: 2,
		ExpiredMembers: []database.User{
			{TelegramID: 1, FirstName: "Anna", LastName: "<Rossi>", MemberNumber: 11},
			{TelegramID: 2, FirstName: "Bruno", LastName: "Verdi", MemberNumber: 22},
		},
	}
	msg := FormatSweepPreview(r, false, map[int64]int{1: 2, 2: -1})
	for _, want := range []string{
		"dry run, nothing was changed", "Expired: <b>2</b>", "<b>DRY RUN</b>", "Within the safety limits",
		"Anna &lt;Rossi&gt; · #11 · <code>1</code> · 2 group(s)", "groups unknown",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("preview %q is missing %q", msg, want)
		}
	}

	r.BreakerTripped, r.BreakerReason = true, "too many"
	msg = FormatSweepPreview(r, true, nil)
	if !strings.Contains(msg, "circuit breaker would trip") || !strings.Contains(msg, "<b>ARMED</b>") {
		t.Fatalf("breaker/armed not shown: %q", msg)
	}

	if msg := FormatSweepPreview(cleanup.Report{TotalMembers: 3, Checked: 3}, false, nil); !strings.Contains(msg, "Nobody is expired") {
		t.Fatalf("empty preview: %q", msg)
	}
}

func TestFormatSweepPreviewCapsTheList(t *testing.T) {
	var r cleanup.Report
	for i := 0; i < maxListed+5; i++ {
		r.ExpiredMembers = append(r.ExpiredMembers, database.User{TelegramID: int64(i), FirstName: "X", LastName: "Y"})
	}
	r.Expired = len(r.ExpiredMembers)
	msg := FormatSweepPreview(r, false, nil)
	if !strings.Contains(msg, "… and 5 more") || len(msg) > telegramMaxText {
		t.Fatalf("list not capped (len %d): %q", len(msg), msg[len(msg)-80:])
	}
}

func TestFormatPending(t *testing.T) {
	if msg := FormatPending(nil); !strings.Contains(msg, "No pending removals") {
		t.Fatalf("empty: %q", msg)
	}
	msg := FormatPending([]database.PendingRemoval{
		{UserID: 5, GroupID: -100, Attempts: 2, LastError: "boom <x>"},
		{UserID: 6, GroupID: -200, Attempts: 5},
	})
	for _, want := range []string{"2 pending removal(s)", "<code>5</code>", "2/5 attempts", "boom &lt;x&gt;", "gave up"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("pending %q is missing %q", msg, want)
		}
	}
}

func TestTruncateCutsAtALineBreak(t *testing.T) {
	long := strings.Repeat("<b>line</b>\n", 1000)
	got := truncate(long)
	if len(got) > telegramMaxText || !strings.HasSuffix(got, "(truncated)") || strings.Contains(got, "<b>line\n") {
		t.Fatalf("bad truncation (len %d)", len(got))
	}
}

func privateCommand(userID int64, text string) *models.Update {
	return &models.Update{Message: &models.Message{
		From: &models.User{ID: userID},
		Chat: models.Chat{ID: userID, Type: "private"},
		Text: text,
	}}
}

// Admin commands must not reveal they exist: a non-admin gets no reply at all.
func TestAdminCommandsIgnoreNonAdmins(t *testing.T) {
	setupDelete(t)
	addMember(t, 710001)

	PendingCommandHandler(context.Background(), nil, privateCommand(710001, "/pending"))
	SweepPreviewCommandHandler(context.Background(), nil, privateCommand(710001, "/sweep_preview"))

	if n := len(fakeTG.Calls("")); n != 0 {
		t.Fatalf("a non-admin must get no reply, got %d call(s)", n)
	}
}

func TestPendingCommandForAdmin(t *testing.T) {
	setupDelete(t)
	if err := database.SeedBotAdmins([]int64{710002}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { _ = database.SeedBotAdmins(nil) })
	database.Get().Create(&database.PendingRemoval{UserID: 77, GroupID: -300, Attempts: 1})

	PendingCommandHandler(context.Background(), nil, privateCommand(710002, "/pending"))

	msgs := fakeTG.Calls("sendMessage")
	if len(msgs) != 1 || msgs[0].ChatID != 710002 || !strings.Contains(msgs[0].Text, "<code>77</code>") {
		t.Fatalf("want the queue sent to the admin, got %+v", msgs)
	}
}

// End to end: an admin's /sweep_preview checks members against the API and reports the
// expired one, while the sweep is ARMED, without touching anybody.
func TestSweepPreviewCommandForAdmin(t *testing.T) {
	setupDelete(t)
	if err := database.SeedBotAdmins([]int64{710003}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { _ = database.SeedBotAdmins(nil) })
	addMember(t, 710004, delGroupA) // member number 10004: expired
	addMember(t, 710005)            // member number 10005: active

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		id := r.PostFormValue("member_id")
		fmt.Fprintf(w, `{"username":"m%s","name":"Member %s","is_membership_active":%t}`, id, id, id != "10004")
	}))
	t.Cleanup(api.Close)
	t.Setenv("API_ENDPOINT", api.URL)
	t.Setenv("API_BEARER_TOKEN", "test")
	t.Setenv("CLEANUP_DRY_RUN", "false")

	SweepPreviewCommandHandler(context.Background(), nil, privateCommand(710003, "/sweep_preview"))

	msgs := fakeTG.Calls("sendMessage")
	if len(msgs) != 2 {
		t.Fatalf("want a 'running' notice and the result, got %+v", msgs)
	}
	result := msgs[1].Text
	for _, want := range []string{"Expired: <b>1</b>", "<code>710004</code>", "1 group(s)", "<b>ARMED</b>"} {
		if !strings.Contains(result, want) {
			t.Fatalf("result %q is missing %q", result, want)
		}
	}
	for _, m := range []string{"banChatMember", "unbanChatMember", "promoteChatMember"} {
		if len(fakeTG.Calls(m)) != 0 {
			t.Fatalf("a preview made a %s call", m)
		}
	}
	if !memberExists(t, 710004) {
		t.Fatal("a preview must delete nobody")
	}
}
