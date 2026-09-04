package vietnamese

import (
	"errors"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		want      string
		syllables int
	}{
		{"plain two syllables", "pháp luật", "pháp luật", 2},
		{"uppercase", "PHÁP LUẬT", "pháp luật", 2},
		{"mixed case", "Pháp Luật", "pháp luật", 2},
		{"leading and trailing space", "  pháp luật  ", "pháp luật", 2},
		{"repeated spaces", "pháp    luật", "pháp luật", 2},
		{"tab separator", "pháp\tluật", "pháp luật", 2},
		{"non-breaking space", "pháp luật", "pháp luật", 2},
		{"newline separator", "pháp\nluật", "pháp luật", 2},
		{"three syllables", "vô tuyến điện", "vô tuyến điện", 3},
		{"four syllables", "công nghiệp hóa hiện", "công nghiệp hóa hiện", 4},
		{"single syllable passes through", "pháp", "pháp", 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			word, syllables, err := Normalize(tc.raw)
			if err != nil {
				t.Fatalf("Normalize(%q) returned error: %v", tc.raw, err)
			}
			if word != tc.want {
				t.Errorf("word = %q, want %q", word, tc.want)
			}
			if len(syllables) != tc.syllables {
				t.Errorf("got %d syllables, want %d", len(syllables), tc.syllables)
			}
		})
	}
}

// A word typed on one keyboard can arrive decomposed and on another composed.
// Both must normalize to the same string, or database lookups fail for reasons
// invisible to the player.
func TestNormalizeUnicodeEquivalence(t *testing.T) {
	composed := "ngôn ngữ"      // ữ as a single codepoint
	decomposed := "ngôn ngữ" // o+circumflex, u+horn+tilde

	gotComposed, _, err := Normalize(composed)
	if err != nil {
		t.Fatal(err)
	}
	gotDecomposed, _, err := Normalize(decomposed)
	if err != nil {
		t.Fatal(err)
	}

	if gotComposed != gotDecomposed {
		t.Errorf("composed %q and decomposed %q normalized differently: %q vs %q",
			composed, decomposed, gotComposed, gotDecomposed)
	}
}

func TestNormalizeEmpty(t *testing.T) {
	for _, raw := range []string{"", "   ", "\t\n", " "} {
		if _, _, err := Normalize(raw); !errors.Is(err, ErrEmpty) {
			t.Errorf("Normalize(%q) error = %v, want ErrEmpty", raw, err)
		}
	}
}

func TestHasEnoughSyllables(t *testing.T) {
	tests := []struct {
		syllables []string
		want      bool
	}{
		{[]string{"pháp"}, false},
		{[]string{"pháp", "luật"}, true},
		{[]string{"vô", "tuyến", "điện"}, true},
		{nil, false},
	}

	for _, tc := range tests {
		if got := HasEnoughSyllables(tc.syllables); got != tc.want {
			t.Errorf("HasEnoughSyllables(%v) = %v, want %v", tc.syllables, got, tc.want)
		}
	}
}
