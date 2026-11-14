package testtg

import (
	"context"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	tb "github.com/Bombaclath97/mensa-telegram-bot/telegram_bot"
)

// The fake is only useful if the real client parses its answers the way it parses Telegram's.
// This drives it through the same telegram_bot helpers production uses.
func TestFakeSpeaksTheBotAPI(t *testing.T) {
	s, err := Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	const chat, user int64 = -100123, 42

	m, err := tb.GetChatMember(ctx, chat, user)
	if err != nil || m.Type != models.ChatMemberTypeMember {
		t.Fatalf("default status: got %+v, %v", m, err)
	}

	s.SetStatus(chat, user, "creator")
	m, err = tb.GetChatMember(ctx, chat, user)
	if err != nil || m.Owner == nil || m.Owner.Status != models.ChatMemberTypeOwner {
		t.Fatalf("creator status: got %+v, %v", m, err)
	}

	s.SetStatus(chat, user, "administrator")
	m, err = tb.GetChatMember(ctx, chat, user)
	if err != nil || m.Administrator == nil {
		t.Fatalf("administrator status: got %+v, %v", m, err)
	}

	if _, err := tb.DemoteUser(ctx, chat, user); err != nil {
		t.Fatalf("DemoteUser: %v", err)
	}
	if err := tb.KickUser(ctx, chat, user); err != nil {
		t.Fatalf("KickUser: %v", err)
	}
	if !s.Demoted(chat, user) || !s.Kicked(chat, user) {
		t.Fatalf("calls not recorded: %+v", s.Calls(""))
	}

	if _, err := tb.Send(ctx, user, "hello"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if c := s.Calls("sendMessage"); len(c) != 1 || c[0].ChatID != user || c[0].Text != "hello" {
		t.Fatalf("sendMessage not recorded correctly: %+v", c)
	}

	s.Fail("getChatMember", chat, 0, ErrChatNotFound)
	if _, err := tb.GetChatMember(ctx, chat, 7); err == nil || !strings.Contains(strings.ToLower(err.Error()), "chat not found") {
		t.Fatalf("want a chat-not-found error, got %v", err)
	}

	s.Fail("banChatMember", chat, user, ErrTransient)
	if err := tb.KickUser(ctx, chat, user); err == nil {
		t.Fatal("want the configured ban failure")
	}

	s.SetTitle(chat, "Mensa Lombardia")
	if c, err := tb.GetChat(ctx, chat); err != nil || c.Title != "Mensa Lombardia" {
		t.Fatalf("getChat: %+v, %v", c, err)
	}
	if admins, err := tb.GetChatAdministrators(ctx, chat); err != nil || len(admins) != 0 {
		t.Fatalf("getChatAdministrators: %+v, %v", admins, err)
	}

	s.Reset()
	if len(s.Calls("")) != 0 {
		t.Fatal("Reset should forget calls")
	}
}
