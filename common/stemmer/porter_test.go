package stemmer

import (
	"testing"
)

func TestPorterStemmer(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"running", "run"},
		{"connects", "connect"},
		{"connection", "connect"},
		{"connections", "connect"},
		{"relational", "relat"},
		{"conditional", "condit"},
		{"rational", "ration"},
		{"valenci", "valenc"},
		{"hesitanci", "hesit"},
		{"digitizer", "digit"},
		{"conformably", "conform"},
		{"radically", "radic"},
		{"different", "differ"},
		{"happy", "happi"},
	}

	for _, tc := range tests {
		actual := Stem(tc.input)
		if actual != tc.expected {
			t.Errorf("Stem(%q): expected %q, got %q", tc.input, tc.expected, actual)
		}
	}
}
