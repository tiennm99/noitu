package main

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/tiennm99dev/noitu/server/internal/vietnamese"
)

// rejectReason explains why a candidate word did not make it into the game
// dictionary. Counted per reason so the build log shows what the source
// actually contained rather than just a final total.
type rejectReason string

const (
	rejectEmpty      rejectReason = "empty"
	rejectTooShort   rejectReason = "fewer than 2 syllables"
	rejectDigit      rejectReason = "contains a digit"
	rejectPunct      rejectReason = "contains punctuation"
	rejectNonVietnam rejectReason = "no Vietnamese letters"
	// rejectNotVietnamese is an entry the upstream tags as another language
	// altogether, decided on its lang_code before the word is looked at.
	rejectNotVietnamese rejectReason = "not a Vietnamese-language entry"
)

// accept normalizes a raw source entry and decides whether it belongs in the
// game dictionary.
func accept(raw string) (word string, syllables []string, reason rejectReason, ok bool) {
	word, syllables, err := vietnamese.Normalize(raw)
	if err != nil {
		return "", nil, rejectEmpty, false
	}

	if !vietnamese.HasEnoughSyllables(syllables) {
		return "", nil, rejectTooShort, false
	}

	for _, r := range word {
		switch {
		case r == ' ':
			continue
		case unicode.IsDigit(r):
			return "", nil, rejectDigit, false
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			return "", nil, rejectPunct, false
		case !unicode.IsLetter(r):
			return "", nil, rejectPunct, false
		}
	}

	if !usesVietnameseAlphabet(word) {
		return "", nil, rejectNonVietnam, false
	}

	// Every syllable must also be a possible Vietnamese syllable. The alphabet
	// check alone lets "credit card" and "come out" through, since they use only
	// letters Vietnamese has.
	for _, syl := range syllables {
		if !isVietnameseSyllable(syl) {
			return "", nil, rejectNonVietnam, false
		}
	}

	return word, syllables, "", true
}

// vietnameseBaseLetters is the Vietnamese alphabet reduced to base letters:
// the Latin alphabet without f, j, w, z, plus đ. Every Vietnamese word is
// spelled from these once diacritics are stripped.
const vietnameseBaseLetters = "abcdeghiklmnopqrstuvxyđ"

// usesVietnameseAlphabet reports whether every letter of the word is a
// Vietnamese one.
//
// Diacritics are stripped first (NFD, dropping combining marks) so that "ngữ"
// reduces to "ngu" and is checked letter by letter. Checking the base letters
// rather than merely "does it contain a diacritic" is what keeps English out:
// an entry only needs one accented character to look Vietnamese otherwise, and
// entries like "thành phố new york", "credit card" and "come out" pass such a
// test while being unplayable as Vietnamese words.
func usesVietnameseAlphabet(word string) bool {
	stripped := norm.NFD.String(word)

	sawLetter := false
	for _, r := range stripped {
		if unicode.Is(unicode.Mn, r) || r == ' ' {
			// Combining mark or separator: not a base letter.
			continue
		}
		if !strings.ContainsRune(vietnameseBaseLetters, r) {
			return false
		}
		sawLetter = true
	}

	return sawLetter
}
