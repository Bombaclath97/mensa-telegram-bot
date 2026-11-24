package membership

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Mensa-Italia/mensa-telegram-bot/database"
	"github.com/Mensa-Italia/mensa-telegram-bot/internal/testdb"
	"github.com/Mensa-Italia/mensa-telegram-bot/internal/testtg"
)

var (
	fakeTG  *testtg.Server
	dbReady bool
)

// TestMain installs the fake Bot API for every test, and a real Postgres for the
// pending-removal tests. RemoveFromGroup needs no database, so its tests run without Docker.
func TestMain(m *testing.M) {
	var err error
	fakeTG, err = testtg.Install()
	if err != nil {
		panic("testtg.Install failed: " + err.Error())
	}

	// Never sleep on a 429 in tests.
	retryWait = func(int) time.Duration { return 0 }

	terminate := func() {}
	dsn, stop, err := testdb.Start(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "membership: no postgres (is Docker running?), DB tests will skip: %v\n", err)
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
	chat int64 = -1003000000001
	usr  int64 = 800001
)

func remove(t *testing.T, opt Options) Result {
	t.Helper()
	return RemoveFromGroup(context.Background(), chat, usr, opt)
}

func writes() []string {
	var out []string
	for _, c := range fakeTG.Calls("") {
		if c.Method != "getChatMember" {
			out = append(out, c.Method)
		}
	}
	return out
}

func TestMemberIsKicked(t *testing.T) {
	fakeTG.Reset()
	res := remove(t, Options{})
	if res.Outcome != Kicked || !fakeTG.Kicked(chat, usr) {
		t.Fatalf("want a kick, got %+v, calls %v", res, writes())
	}
	if fakeTG.Demoted(chat, usr) {
		t.Fatal("a plain member must not be demoted")
	}
}

func TestAdminIsDemotedBeforeKick(t *testing.T) {
	fakeTG.Reset()
	fakeTG.SetStatus(chat, usr, "administrator")
	res := remove(t, Options{})
	w := writes()
	if res.Outcome != Kicked || len(w) < 2 || w[0] != "promoteChatMember" || w[1] != "banChatMember" {
		t.Fatalf("want demote then ban, got %+v, calls %v", res, w)
	}
}

func TestFailedDemoteStillKicks(t *testing.T) {
	fakeTG.Reset()
	fakeTG.SetStatus(chat, usr, "administrator")
	fakeTG.Fail("promoteChatMember", chat, usr, testtg.ErrTransient)
	res := remove(t, Options{})
	if res.Outcome != Kicked || !res.DemoteFailed || !fakeTG.Kicked(chat, usr) {
		t.Fatalf("want a kick with DemoteFailed, got %+v", res)
	}
}

func TestOwnerIsNeverKickedAndGroupIsTold(t *testing.T) {
	fakeTG.Reset()
	fakeTG.SetStatus(chat, usr, "creator")
	res := remove(t, Options{OwnerNotice: "owner left"})
	if res.Outcome != OwnerWarned {
		t.Fatalf("want OwnerWarned, got %+v", res)
	}
	if fakeTG.Called("banChatMember", chat, 0) || fakeTG.Demoted(chat, usr) {
		t.Fatal("the owner must never be kicked or demoted")
	}
	msgs := fakeTG.Calls("sendMessage")
	if len(msgs) != 1 || msgs[0].ChatID != chat || msgs[0].Text != "owner left" {
		t.Fatalf("want the notice posted in the group, got %+v", msgs)
	}
}

func TestOwnerWithoutNoticePostsNothing(t *testing.T) {
	fakeTG.Reset()
	fakeTG.SetStatus(chat, usr, "creator")
	if res := remove(t, Options{}); res.Outcome != OwnerWarned || len(writes()) != 0 {
		t.Fatalf("want OwnerWarned and no writes, got %+v, calls %v", res, writes())
	}
}

// The bug this package fixes: KickUser bans then unbans, so "kicking" someone the group's
// admins had banned would lift their ban. A banned member must be left exactly as they are.
func TestBannedMemberIsNotUnbanned(t *testing.T) {
	fakeTG.Reset()
	fakeTG.SetStatus(chat, usr, "kicked")
	res := remove(t, Options{})
	if res.Outcome != AlreadyOut {
		t.Fatalf("want AlreadyOut, got %+v", res)
	}
	if fakeTG.Called("unbanChatMember", chat, 0) || fakeTG.Called("banChatMember", chat, 0) {
		t.Fatal("a member banned by the group's admins must not be banned or unbanned again")
	}
}

func TestMemberWhoLeftIsLeftAlone(t *testing.T) {
	fakeTG.Reset()
	fakeTG.SetStatus(chat, usr, "left")
	if res := remove(t, Options{}); res.Outcome != AlreadyOut || len(writes()) != 0 {
		t.Fatalf("want AlreadyOut and no writes, got %+v, calls %v", res, writes())
	}
}

func TestGoneGroup(t *testing.T) {
	fakeTG.Reset()
	fakeTG.Fail("getChatMember", chat, 0, testtg.ErrChatNotFound)
	if res := remove(t, Options{}); res.Outcome != Gone || len(writes()) != 0 {
		t.Fatalf("want Gone and no writes, got %+v", res)
	}
}

func TestUnreadableRoleFails(t *testing.T) {
	fakeTG.Reset()
	fakeTG.Fail("getChatMember", chat, usr, testtg.ErrTransient)
	if res := remove(t, Options{}); res.Outcome != Failed || res.Err == nil || len(writes()) != 0 {
		t.Fatalf("want Failed with an error and no writes, got %+v", res)
	}
}

func TestFailedKickFails(t *testing.T) {
	fakeTG.Reset()
	fakeTG.Fail("banChatMember", chat, usr, testtg.ErrTransient)
	if res := remove(t, Options{}); res.Outcome != Failed || res.Err == nil {
		t.Fatalf("want Failed, got %+v", res)
	}
}

func TestRateLimitIsRetried(t *testing.T) {
	fakeTG.Reset()
	fakeTG.FailTimes("banChatMember", chat, usr, testtg.ErrTooManyRequests, 1)
	res := remove(t, Options{})
	if res.Outcome != Kicked || !fakeTG.Kicked(chat, usr) {
		t.Fatalf("one 429 should be retried through, got %+v", res)
	}
	if n := len(fakeTG.Calls("banChatMember")); n != 2 {
		t.Fatalf("want 2 ban attempts, got %d", n)
	}
}

func TestPersistentRateLimitGivesUp(t *testing.T) {
	fakeTG.Reset()
	fakeTG.Fail("banChatMember", chat, usr, testtg.ErrTooManyRequests)
	res := remove(t, Options{})
	if res.Outcome != Failed {
		t.Fatalf("want Failed, got %+v", res)
	}
	if n := len(fakeTG.Calls("banChatMember")); n != maxAttempts {
		t.Fatalf("want %d ban attempts, got %d", maxAttempts, n)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	for _, status := range []string{"member", "administrator", "creator"} {
		fakeTG.Reset()
		fakeTG.SetStatus(chat, usr, status)
		remove(t, Options{DryRun: true, OwnerNotice: "x"})
		if w := writes(); len(w) != 0 {
			t.Fatalf("dry run for a %s made writes: %v", status, w)
		}
	}
}

func TestIsGroupGone(t *testing.T) {
	for _, msg := range []string{"Bad Request: chat not found", "Forbidden: bot was kicked from the supergroup chat"} {
		if !IsGroupGone(fmt.Errorf("%s", msg)) {
			t.Fatalf("%q should mean the group is gone", msg)
		}
	}
	if IsGroupGone(fmt.Errorf("temporary network blip")) || IsGroupGone(nil) {
		t.Fatal("a transient error or nil must not mean the group is gone")
	}
}
