package main

import "testing"

// TestToMTProtoChannelID pins the Bot API -> MTProto id conversion.
//
// This is the one piece of the spike that can be verified without credentials, and it is
// also the most dangerous to get wrong: a bad conversion yields CHANNEL_INVALID, which
// looks identical to "Telegram refuses to let bots enumerate members". Getting a false
// negative here would kill the cutover design for no reason.
func TestToMTProtoChannelID(t *testing.T) {
	tests := []struct {
		name    string
		in      int64
		want    int64
		wantErr bool
	}{
		{
			name: "supergroup in Bot API form loses the -100 prefix",
			in:   -1001234567890,
			want: 1234567890,
		},
		{
			// The real reason to test this: the boundary where the -100 prefix meets a
			// short channel id. -1000000000001 is channel 1, the smallest legal value.
			name: "smallest legal supergroup id",
			in:   -1000000000001,
			want: 1,
		},
		{
			name: "bare mtproto id passes through unchanged",
			in:   1234567890,
			want: 1234567890,
		},
		{
			// A legacy basic group. These do NOT use channels.getParticipants at all, so
			// silently converting this to a garbage channel id would produce a misleading
			// CHANNEL_INVALID rather than an honest "wrong chat type".
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
			got, err := toMTProtoChannelID(tc.in)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("toMTProtoChannelID(%d) = %d, want an error", tc.in, got)
				}
				return
			}

			if err != nil {
				t.Fatalf("toMTProtoChannelID(%d) returned an unexpected error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("toMTProtoChannelID(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
