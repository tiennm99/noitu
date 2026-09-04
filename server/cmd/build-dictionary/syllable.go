package main

import (
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Vietnamese syllable structure is a closed system: onset + nucleus + coda,
// where each part is drawn from a fixed inventory. That makes it a reliable
// test for whether an entry is a Vietnamese word at all.
//
// An alphabet check alone cannot do this. "credit card", "dress code" and
// "come out" are spelled entirely with letters Vietnamese has, so they pass any
// letter-level filter, but no Vietnamese syllable begins "cr" or ends "ss".
// The multilingual source leaks entries like these into the `vi` rows, and
// without this check they become playable words.

// vietnameseOnsets is the complete inventory of syllable-initial consonants.
// Longest-match order matters, so this is sorted by descending length at init.
var vietnameseOnsets = []string{
	"ngh", "ng", "nh", "ch", "gh", "gi", "kh", "ph", "qu", "th", "tr",
	"b", "c", "d", "đ", "g", "h", "k", "l", "m", "n", "p", "r", "s", "t", "v", "x",
}

// vietnameseCodas is the complete inventory of syllable-final consonants and
// offglides.
var vietnameseCodas = []string{
	"ngh", "ng", "nh", "ch",
	"c", "m", "n", "p", "t", "i", "o", "u", "y",
}

// vietnameseNuclei is the inventory of vowel nuclei, diacritics stripped. Some
// are single vowels, others diphthongs or triphthongs.
var vietnameseNuclei = []string{
	"uye", "uya", "uyu", "oai", "oay", "uoi", "uou", "ieu", "yeu", "uai", "uay",
	"ai", "ao", "au", "ay", "eo", "eu", "ia", "ie", "iu", "oa", "oe", "oi", "oo",
	"ua", "ue", "ui", "uo", "uu", "uy", "ya", "ye", "yu", "ao", "eu",
	"a", "e", "i", "o", "u", "y",
}

func init() {
	byLengthDesc := func(s []string) {
		sort.SliceStable(s, func(i, j int) bool { return len(s[i]) > len(s[j]) })
	}
	byLengthDesc(vietnameseOnsets)
	byLengthDesc(vietnameseCodas)
	byLengthDesc(vietnameseNuclei)
}

// stripDiacritics reduces a syllable to its base letters, keeping đ (which is a
// distinct letter, not a d with a mark).
func stripDiacritics(syllable string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(syllable) {
		// Mn = nonspacing combining mark: every Vietnamese tone and letter mark.
		if isCombiningMark(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isCombiningMark(r rune) bool {
	// The ranges Vietnamese actually uses; cheaper and tighter than a full
	// unicode.Is(unicode.Mn, r) for this data.
	return (r >= 0x0300 && r <= 0x036F) || r == 0x031B
}

// isVietnameseSyllable reports whether a syllable fits Vietnamese phonotactics.
//
// Every onset and nucleus split must be tried, not just the longest. A greedy
// parser reads "gì" as the digraph onset "gi" with nothing left for a nucleus
// and wrongly rejects it; the correct parse is onset "g" plus nucleus "ì".
// The same applies to "gìn", "gỉ" and every other g + i syllable.
func isVietnameseSyllable(syllable string) bool {
	base := stripDiacritics(syllable)
	if base == "" {
		return false
	}

	// Onset is optional ("áo", "ăn"), so "" is a candidate too.
	candidates := []string{""}
	for _, onset := range vietnameseOnsets {
		if strings.HasPrefix(base, onset) {
			candidates = append(candidates, onset)
		}
	}

	for _, onset := range candidates {
		if parsesAsRhyme(base[len(onset):]) {
			return true
		}
	}

	return false
}

// parsesAsRhyme reports whether the remainder after an onset is a valid
// nucleus followed by at most one coda.
func parsesAsRhyme(rest string) bool {
	if rest == "" {
		return false
	}

	for _, nucleus := range vietnameseNuclei {
		if !strings.HasPrefix(rest, nucleus) {
			continue
		}
		tail := rest[len(nucleus):]
		if tail == "" {
			return true
		}
		for _, coda := range vietnameseCodas {
			if tail == coda {
				return true
			}
		}
	}

	return false
}
