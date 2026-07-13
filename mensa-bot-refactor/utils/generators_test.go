package utils

import (
	"strings"
	"testing"
)

// TestGenerateParseRoundTrip: a generated token must parse back to the same user ID and be
// accepted by the shape validator. This is the callme generate/verify round-trip.
func TestGenerateParseRoundTrip(t *testing.T) {
	for _, id := range []int64{1, 42, 999999, -7} {
		tok := GenerateCallmeUrlEndpoint(id)

		gotID, err := ParseCallmeToken(tok)
		if err != nil {
			t.Fatalf("ParseCallmeToken(%q) errored: %v", tok, err)
		}
		if gotID != id {
			t.Fatalf("round-trip id = %d, want %d", gotID, id)
		}

		// Shape: "<id>_<15 chars>".
		idPart, suffix, found := strings.Cut(tok, "_")
		if !found || len(suffix) != callmeTokenSuffixLen {
			t.Fatalf("token %q has the wrong shape", tok)
		}
		_ = idPart
	}
}

// TestGenerateIsUnpredictable: two tokens for the same user must differ (the suffix is the
// shared secret; a constant suffix would let anyone forge the callback).
func TestGenerateIsUnpredictable(t *testing.T) {
	a := GenerateCallmeUrlEndpoint(42)
	b := GenerateCallmeUrlEndpoint(42)
	if a == b {
		t.Fatalf("two tokens for the same user were identical (%q) — the secret is not random", a)
	}
}

func TestParseCallmeTokenRejectsMalformed(t *testing.T) {
	bad := []string{
		"",                              // empty
		"nounderscore",                  // no separator
		"42_short",                      // suffix too short
		"42_" + strings.Repeat("a", 16), // suffix too long
		"notanumber_abcdefghijklmno",    // non-numeric id
		"_abcdefghijklmno",              // empty id
	}
	for _, tok := range bad {
		if _, err := ParseCallmeToken(tok); err == nil {
			t.Errorf("ParseCallmeToken(%q) should have errored", tok)
		}
	}
}

func TestBuildCallmeURL(t *testing.T) {
	t.Setenv("CALLME_BASE_URL", "https://bot.example/callme_api/")
	got := BuildCallmeURL("42_abcdefghijklmno")
	want := "https://bot.example/callme_api/42_abcdefghijklmno"
	if got != want {
		t.Fatalf("BuildCallmeURL = %q, want %q", got, want)
	}
}
