package wsapi

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// maxNicknameRunes caps the display name. Runes, not bytes: a Vietnamese name
// is multi-byte, and a byte cap would cut it far shorter than a Latin one.
const maxNicknameRunes = 20

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
	s := norm.NFC.String(raw)

	// Drop anything non-printing. Format characters (Cf) are the important
	// case: zero-width joiners and bidi overrides are invisible, so they can
	// pad a name past a visual check or reverse how it renders.
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

	// Collapse runs of whitespace so a name cannot be padded into a column of
	// its own, then trim the edges.
	s = strings.Join(strings.Fields(s), " ")

	if runes := []rune(s); len(runes) > maxNicknameRunes {
		s = strings.TrimSpace(string(runes[:maxNicknameRunes]))
	}

	if s == "" || denylisted(s) {
		return defaultNickname
	}
	return s
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
