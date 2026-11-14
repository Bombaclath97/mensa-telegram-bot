package cleanup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Bombaclath97/mensa-telegram-bot/database"
)

func TestSweepAlert(t *testing.T) {
	cases := []struct {
		name     string
		r        Report
		err      error
		wantSend bool
		want     []string
	}{
		{"quiet sweep", Report{Checked: 50}, nil, false, nil},
		{"overlapping trigger", Report{}, ErrSweepInProgress, false, nil},
		{"breaker", Report{Checked: 40, Expired: 12, BreakerReason: "expired (12) exceeds the absolute cap"}, ErrCircuitBreaker, true,
			[]string{"circuit breaker", "12 of 40", "Nobody was removed", "/sweep_preview"}},
		{"failed sweep", Report{}, errors.New("cleanup: API_ENDPOINT is not set <x>"), true,
			[]string{"failed", "API_ENDPOINT is not set &lt;x&gt;"}},
		{"dry run with expired", Report{DryRun: true, Checked: 50, Expired: 2}, nil, true,
			[]string{"Dry-run", "2 member(s) would be removed", "/sweep_preview"}},
		{"armed with trouble", Report{Checked: 50, Expired: 3, Deleted: 1, KickedFromGroup: 2, Failed: 1, OwnersWarned: 1}, nil, true,
			[]string{"3 expired, 1 removed, 2 group kick(s)", "1 member(s) could not be fully removed", "own a group"}},
		{"api unhealthy, nothing expired", Report{DryRun: true, Checked: 10, Inconclusive: 6}, nil, true,
			[]string{"Mensa API looks unhealthy", "6 of 10"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg, send := SweepAlert(c.r, c.err)
			if send != c.wantSend {
				t.Fatalf("send = %v, want %v (msg %q)", send, c.wantSend, msg)
			}
			for _, w := range c.want {
				if !strings.Contains(msg, w) {
					t.Fatalf("alert %q is missing %q", msg, w)
				}
			}
		})
	}
}

// Preview never changes anything, even when the real sweep is armed, and it reports who
// would be removed.
func TestPreviewIgnoresArmedSetting(t *testing.T) {
	armDestructive(t, map[string]bool{"141": false, "142": true})
	insertUser(t, 600041, 141)
	insertUser(t, 600042, 142)
	insertAssoc(t, 600041, groupA)

	report, err := Preview(context.Background())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if !report.DryRun {
		t.Fatal("a preview must report itself as a dry run")
	}
	if userCount(t) != 2 {
		t.Fatal("a preview must delete nobody")
	}
	for _, m := range []string{"banChatMember", "unbanChatMember", "promoteChatMember", "sendMessage"} {
		if n := len(fakeTG.Calls(m)); n != 0 {
			t.Fatalf("a preview made %d %s call(s)", n, m)
		}
	}
	if len(report.ExpiredMembers) != 1 || report.ExpiredMembers[0].TelegramID != 600041 {
		t.Fatalf("want member 600041 listed as expired, got %+v", report.ExpiredMembers)
	}
}

// When the breaker trips, the preview still says who it would have removed, and why it
// stopped.
func TestPreviewListsMembersWhenBreakerTrips(t *testing.T) {
	armDestructive(t, map[string]bool{"151": false, "152": false})
	t.Setenv("CLEANUP_MAX_DELETIONS", "1")
	insertUser(t, 600051, 151)
	insertUser(t, 600052, 152)

	report, err := Preview(context.Background())
	if !errors.Is(err, ErrCircuitBreaker) {
		t.Fatalf("want ErrCircuitBreaker, got %v", err)
	}
	if len(report.ExpiredMembers) != 2 || report.BreakerReason == "" {
		t.Fatalf("want both members listed and a reason, got %+v", report)
	}
}

// A scheduled sweep that trips the breaker DMs the admins.
func TestScheduledSweepAlertsAdmins(t *testing.T) {
	armDestructive(t, map[string]bool{"161": false, "162": false})
	t.Setenv("CLEANUP_MAX_DELETIONS", "1")
	insertUser(t, 600061, 161)
	insertUser(t, 600062, 162)
	if err := database.SeedBotAdmins([]int64{990001}); err != nil {
		t.Fatalf("seed admins: %v", err)
	}
	t.Cleanup(func() { _ = database.SeedBotAdmins(nil) })

	scheduledSweep(context.Background())

	msgs := fakeTG.Calls("sendMessage")
	if len(msgs) != 1 || msgs[0].ChatID != 990001 || !strings.Contains(msgs[0].Text, "circuit breaker") {
		t.Fatalf("want one breaker alert to the admin, got %+v", msgs)
	}
	if userCount(t) != 2 {
		t.Fatal("nobody may be removed when the breaker trips")
	}
}
