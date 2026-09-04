// Package vietnamese normalizes Vietnamese word input.
//
// The same normalization runs in two places: the dictionary builder, which
// normalizes the corpus, and the game server, which normalizes what a player
// types. They must agree exactly — if they ever diverge, a word stored in the
// database stops matching the identical string typed by a player. Sharing this
// package is what prevents that.
package vietnamese

import (
	"errors"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// MinSyllables is the shortest word the game accepts. Nối từ is played with
// compound words; a single syllable is not a valid move.
const MinSyllables = 2

// ErrEmpty is returned when input contains no usable characters.
var ErrEmpty = errors.New("vietnamese: empty input")

// Normalize canonicalizes raw player or corpus input and splits it into syllables.
//
// It returns the canonical word (syllables joined by single spaces) and the
// syllables themselves. It deliberately does not enforce a syllable count:
// length is a game rule, not a text rule, so the engine owns that check.
//
// Steps, in order:
//   - NFC composition, so "ữ" written as a base plus a combining mark compares
//     equal to the single-codepoint form. Without this, words typed on different
//     keyboards or platforms fail to match identical database entries.
//   - lowercase, so capitalization never affects a lookup.
//   - whitespace split and rejoin, which collapses runs of spaces, tabs, and
//     non-breaking spaces that IMEs and copy-paste routinely introduce.
func Normalize(raw string) (string, []string, error) {
	composed := norm.NFC.String(raw)
	lowered := strings.ToLower(composed)

	// strings.Fields splits on every unicode.IsSpace rune, which covers tabs and
	// U+00A0 non-breaking spaces as well as ordinary spaces.
	syllables := strings.Fields(lowered)
	if len(syllables) == 0 {
		return "", nil, ErrEmpty
	}

	return strings.Join(syllables, " "), syllables, nil
}

// HasEnoughSyllables reports whether a word is long enough to be a legal move.
func HasEnoughSyllables(syllables []string) bool {
	return len(syllables) >= MinSyllables
}
