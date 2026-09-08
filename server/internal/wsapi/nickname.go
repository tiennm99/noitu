package wsapi

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// maxNicknameRunes caps the display name. Runes, not bytes: a Vietnamese name
// is multi-byte, and a byte cap would cut it far shorter than a Latin one.
const maxNicknameRunes = 20

// maxNicknameMarks is how many combining marks may follow one base rune. Two
// covers every Vietnamese cluster - a vowel can carry a diacritic and a tone
// mark and no more - so anything beyond it is stacking, not writing.
const maxNicknameMarks = 2

// denylisted is the hook for blocking names outright. It is deliberately a
// variable and deliberately empty: v1 has no abuse signal to tune a list
// against, and this way adding one later is a data change in one place rather
// than a new pass through the sanitizer.
var denylisted = func(string) bool { return false }

// sanitizeNickname turns untrusted input into something safe to show a
// stranger.
//
// This is an input-validation boundary, not cosmetics: the result is rendered
// in another player's browser, so anything that could forge UI or hide
// characters has to be gone before it is stored. The order matters —
// normalizing first means the length cap and the character filters see the
// same form the client will render, rather than a decomposed variant that
// counts differently.
//
// When nothing usable survives, the caller gets the generic name. Making that
// unique is the room's job, not this function's: two players can just as
// easily both *choose* "Minh", so the collision has to be resolved where both
// names are known.
func sanitizeNickname(raw string) string {
	s := sanitizeText(raw, maxNicknameRunes, maxNicknameMarks)
	if s == "" || denylisted(s) {
		return defaultNickname
	}
	return s
}

// sanitizeText is the filter itself, without the nickname policy around it.
//
// Shared with chat, which needs the same guarantees at a different size: text
// that is safe to render in a stranger's browser, on one line, bounded. The
// caller decides the bounds and what an empty result means.
//
// Returns "" when nothing usable survives.
func sanitizeText(raw string, maxRunes, maxMarks int) string {
	s := norm.NFC.String(raw)

	// Drop anything non-printing. Format characters (Cf) are the important
	// case: zero-width joiners and bidi overrides are invisible, so they can
	// pad text past a visual check or reverse how it renders.
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' ' // collapsed below
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		case !unicode.IsPrint(r):
			return -1
		}
		return r
	}, s)

	s = capMarks(s, maxMarks)

	// Collapse runs of whitespace so text cannot be padded into a column of
	// its own, then trim the edges.
	s = strings.Join(strings.Fields(s), " ")

	if runes := []rune(s); len(runes) > maxRunes {
		s = strings.TrimSpace(string(runes[:maxRunes]))
	}
	return s
}

// capMarks limits how many combining marks may follow one base rune.
//
// The filter above cannot catch these: a combining mark is printable, is not a
// control or format character, and NFC leaves an uncomposable one where it is.
// A base rune followed by a hundred of them renders as a glyph cluster tall
// enough to cover the page it is displayed on, which is a layout attack rather
// than a word.
func capMarks(s string, maxMarks int) string {
	var b strings.Builder
	b.Grow(len(s))

	marks := 0
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
			if marks >= maxMarks {
				continue
			}
			marks++
		} else {
			marks = 0
		}
		b.WriteRune(r)
	}
	return b.String()
}

// defaultNickname is what an unusable name falls back to.
const defaultNickname = "Người chơi"

// distinguish returns a name for the joining player that their opponent cannot
// be confused with. Nicknames are the only way to tell two strangers apart, so
// letting both sides render the same string defeats the point of having them.
func distinguish(name, taken string) string {
	if name != taken {
		return name
	}
	suffix := " 2"
	if runes := []rune(name); len(runes)+len(suffix) > maxNicknameRunes {
		name = strings.TrimSpace(string(runes[:maxNicknameRunes-len(suffix)]))
	}
	return name + suffix
}
