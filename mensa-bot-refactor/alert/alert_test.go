package alert

import (
	"context"
	"testing"

	"github.com/Bombaclath97/mensa-telegram-bot/internal/testtg"
)

func TestAdminsDeliversToEveryAdminAndSurvivesFailures(t *testing.T) {
	fake, err := testtg.Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	defer fake.Close()

	recipients = func() []int64 { return []int64{101, 102, 103} }
	defer func() { recipients = nil }()

	// 102 never pressed Start: Telegram refuses the DM. The others must still get it.
	fake.Fail("sendMessage", 102, 0, &testtg.APIError{Code: 403, Description: "Forbidden: bot can't initiate conversation with a user"})

	if n := Admins(context.Background(), "hello <b>admins</b>"); n != 2 {
		t.Fatalf("want 2 deliveries, got %d", n)
	}
	for _, id := range []int64{101, 103} {
		if !fake.Called("sendMessage", id, 0) {
			t.Fatalf("admin %d did not get the alert", id)
		}
	}

	fake.Reset()
	recipients = func() []int64 { return nil }
	if n := Admins(context.Background(), "nobody"); n != 0 || len(fake.Calls("")) != 0 {
		t.Fatal("with no admins configured nothing is sent")
	}
}
