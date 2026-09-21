package wsapi

import (
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// FuzzSanitizeText guards the filter every player-supplied string crosses
// before it is shown to a stranger: a nickname, a chat line and a submitted
// word all go through it. The invariants are the ones sanitizeText's own
// comment promises — no control or format character survives, no rune count
// over the cap, no run of combining marks longer than the cap allows — plus
// the one every caller depends on implicitly: it never panics on arbitrary
// input, however malformed.
//
// Bounds are fixed at the nickname's rather than fuzzed themselves: every real
// call site passes a compile-time constant, so a bound the fuzzer chooses
// would be exercising a contract sanitizeText was never asked to keep.
func FuzzSanitizeText(f *testing.F) {
	seeds := []string{
		"Minh",
		"Nguyễn Thuý",
		"",
		"   \t\n ",
		"Mi\x00nh\x07",
		"Mi\u200bnh\u200d",
		"Minh\u202e",
		"  Minh    Nguyen  ",
		"Minh\nNguyen",
		strings.Repeat("a", 40),
		strings.Repeat("ữ", maxNicknameRunes),
		"Minh" + strings.Repeat(string(stackingMark), 50),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		got := sanitizeText(raw, maxNicknameRunes, maxNicknameMarks)

		if n := utf8.RuneCountInString(got); n > maxNicknameRunes {
			t.Fatalf("sanitizeText(%q) kept %d runes, want at most %d", raw, n, maxNicknameRunes)
		}

		marks := 0
		for _, r := range got {
			switch {
			case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
				t.Fatalf("sanitizeText(%q) = %q kept a control/format character %U", raw, got, r)
			case !unicode.IsPrint(r):
				t.Fatalf("sanitizeText(%q) = %q kept a non-printing character %U", raw, got, r)
			case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r):
				marks++
				if marks > maxNicknameMarks {
					t.Fatalf("sanitizeText(%q) = %q stacked %d combining marks, want at most %d", raw, got, marks, maxNicknameMarks)
				}
			default:
				marks = 0
			}
		}
	})
}

func TestSanitizeNickname(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Minh", "Minh"},
		{"vietnamese kept", "Nguyễn Thuý", "Nguyễn Thuý"},
		{"empty falls back", "", defaultNickname},
		{"whitespace only falls back", "   \t\n ", defaultNickname},
		{"control characters stripped", "Mi\x00nh\x07", "Minh"},
		{"zero width stripped", "Mi\u200bnh\u200d", "Minh"},
		{"bidi override stripped", "Minh\u202e", "Minh"},
		{"whitespace collapsed", "  Minh    Nguyen  ", "Minh Nguyen"},
		{"newlines become spaces", "Minh\nNguyen", "Minh Nguyen"},
		{"over length truncated", strings.Repeat("a", 40), strings.Repeat("a", maxNicknameRunes)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeNickname(tc.in); got != tc.want {
				t.Errorf("sanitizeNickname(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSanitizeNicknameCountsRunesNotBytes guards the cap against being applied
// in bytes, which would cut a Vietnamese name to a third of the length a Latin
// one keeps.
func TestSanitizeNicknameCountsRunesNotBytes(t *testing.T) {
	in := strings.Repeat("ữ", maxNicknameRunes)
	got := sanitizeNickname(in)
	if n := len([]rune(got)); n != maxNicknameRunes {
		t.Errorf("kept %d runes, want %d (byte-based truncation?)", n, maxNicknameRunes)
	}
}

// TestDistinguishSeparatesIdenticalNames covers the collision the fallback
// creates — players who all send nothing — and the one it always could:
// players who choose the same name.
func TestDistinguishSeparatesIdenticalNames(t *testing.T) {
	if got := distinguish("Minh", []string{"Thuý"}); got != "Minh" {
		t.Errorf("distinct names should be left alone, got %q", got)
	}

	// A whole room of unnamed players, seated one at a time. Every one of them
	// has to end up with a name none of the others is already using: two
	// suffixed identically is the same failure as two unsuffixed.
	var taken []string
	for range maxPlayers {
		got := distinguish(defaultNickname, taken)
		if slices.Contains(taken, got) {
			t.Fatalf("distinguish returned %q, which is already in %v", got, taken)
		}
		if n := len([]rune(got)); n > maxNicknameRunes {
			t.Errorf("distinguished name %q is %d runes, over the %d cap", got, n, maxNicknameRunes)
		}
		taken = append(taken, got)
	}

	// The suffix must not push a name that is already at the cap past it.
	long := strings.Repeat("a", maxNicknameRunes)
	if got := distinguish(long, []string{long}); len([]rune(got)) > maxNicknameRunes {
		t.Errorf("distinguished name is %d runes, over the %d cap", len([]rune(got)), maxNicknameRunes)
	}
}
