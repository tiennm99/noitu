package wsapi

import (
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
