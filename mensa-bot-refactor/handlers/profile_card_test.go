package handlers

import "testing"

// The Mensa API gives one "name" field; the DB stores first + last. Getting this wrong
// silently mangles every member's name, and nobody types it any more so nobody can correct
// it by hand.
func TestSplitName(t *testing.T) {
	tests := []struct {
		in          string
		first, last string
	}{
		{"Mario Rossi", "Mario", "Rossi"},
		{"Anna Maria De Luca", "Anna", "Maria De Luca"}, // everything after the first token
		{"Cher", "Cher", ""},                            // mononym: keep it, do not drop it
		{"  Mario   Rossi  ", "Mario", "Rossi"},         // stray whitespace
		{"", "", ""},
		{"   ", "", ""},
	}

	for _, tc := range tests {
		first, last := SplitName(tc.in)
		if first != tc.first || last != tc.last {
			t.Errorf("SplitName(%q) = (%q, %q), want (%q, %q)", tc.in, first, last, tc.first, tc.last)
		}
	}
}
