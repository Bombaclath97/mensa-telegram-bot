package mtproto

import (
	"errors"
	"strings"
	"testing"
)

// TestToChannelID pins the Bot API -> MTProto id conversion.
//
// This is the most dangerous thing here to get wrong: a bad conversion yields CHANNEL_INVALID,
// which reads EXACTLY like "Telegram refuses to let bots enumerate members". A false negative
// there would kill the feature for no reason.
func TestToChannelID(t *testing.T) {
	tests := []struct {
		name    string
		in      int64
		want    int64
		wantErr bool
	}{
		{
			name: "supergroup loses the -100 prefix and stays POSITIVE",
			in:   -1004345944461,
			want: 4345944461,
		},
		{
			// The boundary where the -100 prefix meets the smallest legal channel id.
			name: "smallest legal supergroup id",
			in:   -1000000000001,
			want: 1,
		},
		{
			name: "bare mtproto id passes through",
			in:   4345944461,
			want: 4345944461,
		},
		{
			// Legacy basic groups do NOT use channels.getParticipants at all — their member
			// list comes from messages.getFullChat. Silently converting this to a garbage
			// channel id would produce a misleading CHANNEL_INVALID instead of an honest
			// "wrong chat type".
			name:    "legacy basic group is rejected, not mangled",
			in:      -123456789,
			wantErr: true,
		},
		{
			name:    "exactly the -100 base does not convert to a positive id",
			in:      -1000000000000,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := toChannelID(tc.in)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("toChannelID(%d) = %d, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("toChannelID(%d): unexpected error %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("toChannelID(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestLegacyBasicGroupIsMatchable is the point of ErrLegacyBasicGroup existing.
//
// This used to be a bare fmt.Errorf string, so callers had nothing to match on and it fell
// into group_handler's `default:` branch — logged at ERROR as an unknown failure, on every
// promotion event, forever. It is neither unknown nor retryable: a basic group has no
// member-list API and only a human can upgrade the chat. Callers must be able to say so.
func TestLegacyBasicGroupIsMatchable(t *testing.T) {
	_, err := toChannelID(-123456789)
	if err == nil {
		t.Fatal("expected an error for a legacy basic group")
	}

	if !errors.Is(err, ErrLegacyBasicGroup) {
		t.Errorf("errors.Is(err, ErrLegacyBasicGroup) = false for %v — callers cannot classify it", err)
	}

	// The chat id must survive wrapping: it is the only thing that tells an operator WHICH
	// group to upgrade.
	if !strings.Contains(err.Error(), "-123456789") {
		t.Errorf("error %q does not name the offending chat id", err)
	}

	// A supergroup id must NOT match the sentinel.
	if _, err := toChannelID(-1004435267124); err != nil {
		t.Fatalf("a supergroup id should convert cleanly, got %v", err)
	}
}

// TestConfiguredRequiresAllThree: a half-configured MTProto must report itself unconfigured,
// so callers degrade cleanly instead of failing deep inside an RPC call with a confusing error.
func TestConfiguredRequiresAllThree(t *testing.T) {
	cases := []struct {
		name            string
		id, hash, token string
		want            bool
	}{
		{"all set", "123", "abc", "1:tok", true},
		{"no api id", "", "abc", "1:tok", false},
		{"no api hash", "123", "", "1:tok", false},
		{"no bot token", "123", "abc", "", false},
		{"nothing", "", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TG_API_ID", tc.id)
			t.Setenv("TG_API_HASH", tc.hash)
			t.Setenv("BOT_TOKEN", tc.token)

			if got := Configured(); got != tc.want {
				t.Fatalf("Configured() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestEnumerateUnconfiguredIsATypedError: callers must be able to tell "not set up" apart from
// "Telegram said no", because the first is a degradation and the second is a bug.
func TestEnumerateUnconfiguredIsATypedError(t *testing.T) {
	t.Setenv("TG_API_ID", "")
	t.Setenv("TG_API_HASH", "")
	t.Setenv("BOT_TOKEN", "")

	_, err := Enumerate(t.Context(), -1004345944461)
	if err == nil {
		t.Fatal("Enumerate with no config must fail")
	}
	if !isNotConfigured(err) {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}

func isNotConfigured(err error) bool {
	return err == ErrNotConfigured
}
