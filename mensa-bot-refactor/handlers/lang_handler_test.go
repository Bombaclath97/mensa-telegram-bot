package handlers

import "testing"

// The picker must be driven by whatever Tolgee actually serves. It used to be a hardcoded
// list of four, which left 7 of the project's 11 languages unreachable — that is the
// regression these tests exist to prevent.
func TestLanguagesFromTags(t *testing.T) {
	// The real project tags, as Tolgee reports them.
	projectTags := []string{"bn", "de", "en", "es", "fr", "hi", "it", "ja", "pt", "zh", "zh-Hans"}

	tests := []struct {
		name      string
		tags      []string
		base      string
		wantCodes []string
	}{
		{
			name: "every Tolgee language is offered, zh-Hans hidden as a duplicate of zh",
			tags: projectTags,
			base: "it",
			// zh-Hans dropped; everything else survives.
			wantCodes: []string{"bn", "de", "en", "es", "fr", "hi", "it", "ja", "pt", "zh"},
		},
		{
			name:      "a language Tolgee adds later shows up without a code change",
			tags:      []string{"it", "en", "sv"},
			base:      "it",
			wantCodes: []string{"it", "en", "sv"},
		},
		{
			name:      "Tolgee failed to load: fall back to the base language, never an empty menu",
			tags:      nil,
			base:      "it",
			wantCodes: []string{"it"},
		},
		{
			name:      "hidden tag alone still yields a non-empty menu",
			tags:      []string{"zh-Hans"},
			base:      "it",
			wantCodes: []string{"it"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := languagesFromTags(tc.tags, tc.base)

			if len(got) != len(tc.wantCodes) {
				t.Fatalf("got %d languages %v, want %d %v", len(got), codesOf(got), len(tc.wantCodes), tc.wantCodes)
			}
			for i, want := range tc.wantCodes {
				if got[i].Code != want {
					t.Errorf("position %d: got %q, want %q", i, got[i].Code, want)
				}
			}
			// Every button needs a label, or the user sees a blank row.
			for _, l := range got {
				if l.Name == "" || l.Flag == "" {
					t.Errorf("language %q has an empty label: flag=%q name=%q", l.Code, l.Flag, l.Name)
				}
			}
		})
	}
}

// The selected code arrives from user-controlled callback data and is written straight to
// the DB, so it is an authorization boundary, not a formality.
func TestLanguagesFromTagsRejectsUnoffered(t *testing.T) {
	offered := languagesFromTags([]string{"it", "en", "zh", "zh-Hans"}, "it")

	valid := func(code string) bool {
		for _, l := range offered {
			if l.Code == code {
				return true
			}
		}
		return false
	}

	for _, code := range []string{"it", "en", "zh"} {
		if !valid(code) {
			t.Errorf("%q is offered in the menu but would be rejected", code)
		}
	}
	// zh-Hans is deliberately not offered; neither is junk.
	for _, code := range []string{"zh-Hans", "", "xx", "it; DROP TABLE users", "../../etc/passwd"} {
		if valid(code) {
			t.Errorf("%q is not in the menu but would be accepted and written to the DB", code)
		}
	}
}

// Two languages per row, and no row ever exceeds two buttons.
func TestBuildLanguageKeyboardRowsOfTwo(t *testing.T) {
	kb := buildLanguageKeyboard()

	if len(kb.InlineKeyboard) == 0 {
		t.Fatal("language keyboard is empty")
	}
	total := 0
	for i, row := range kb.InlineKeyboard {
		if len(row) == 0 || len(row) > 2 {
			t.Errorf("row %d has %d buttons, want 1 or 2", i, len(row))
		}
		for _, btn := range row {
			if btn.CallbackData == "" {
				t.Errorf("row %d: button %q has no callback data", i, btn.Text)
			}
			total++
		}
	}
	if want := len(availableLanguages()); total != want {
		t.Errorf("keyboard has %d buttons, want %d (one per available language)", total, want)
	}
}

func codesOf(langs []Language) []string {
	out := make([]string, len(langs))
	for i, l := range langs {
		out[i] = l.Code
	}
	return out
}
