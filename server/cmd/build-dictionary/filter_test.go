package main

import "testing"

func TestAcceptKeepsValidWords(t *testing.T) {
	tests := []struct {
		raw       string
		want      string
		syllables int
	}{
		{"pháp luật", "pháp luật", 2},
		{"Pháp  Luật", "pháp luật", 2},
		{"vô tuyến điện", "vô tuyến điện", 3},
		{"con cua", "con cua", 2}, // no diacritics, still Vietnamese
		{"đi đứng", "đi đứng", 2}, // đ is a Vietnamese letter
		{"quy hoạch", "quy hoạch", 2},
	}

	for _, tc := range tests {
		word, syllables, reason, ok := accept(tc.raw, 0)
		if !ok {
			t.Errorf("accept(%q) rejected: %s", tc.raw, reason)
			continue
		}
		if word != tc.want {
			t.Errorf("accept(%q) word = %q, want %q", tc.raw, word, tc.want)
		}
		if len(syllables) != tc.syllables {
			t.Errorf("accept(%q) syllables = %d, want %d", tc.raw, len(syllables), tc.syllables)
		}
	}
}

func TestAcceptRejects(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want rejectReason
	}{
		{"empty", "   ", rejectEmpty},
		{"single syllable", "pháp", rejectTooShort},
		{"digit", "covid 19", rejectDigit},
		{"hyphen", "pháp-luật xxx", rejectPunct},
		{"english phrase", "hello world", rejectNonVietnam},
		{"letters Vietnamese lacks", "jazz music", rejectNonVietnam},
		// These pass a naive "contains f/j/w/z" test and a naive "has a
		// diacritic" test, yet are unplayable as Vietnamese words.
		{"english, no forbidden letters", "credit card", rejectNonVietnam},
		{"english, no forbidden letters 2", "dress code", rejectNonVietnam},
		{"english phrasal verb", "come out", rejectNonVietnam},
		// One Vietnamese diacritic must not launder an English word.
		{"mixed English with a diacritic", "thành phố new york", rejectNonVietnam},
		{"non-Latin script", "привет мир", rejectNonVietnam},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, reason, ok := accept(tc.raw, 0)
			if ok {
				t.Fatalf("accept(%q) succeeded, want rejection %s", tc.raw, tc.want)
			}
			if reason != tc.want {
				t.Errorf("accept(%q) reason = %s, want %s", tc.raw, reason, tc.want)
			}
		})
	}
}

func TestAcceptMaxSyllables(t *testing.T) {
	const raw = "công nghiệp hóa"

	if _, _, _, ok := accept(raw, 0); !ok {
		t.Errorf("accept(%q, no limit) rejected, want accepted", raw)
	}
	if _, _, _, ok := accept(raw, 3); !ok {
		t.Errorf("accept(%q, max 3) rejected, want accepted", raw)
	}
	_, _, reason, ok := accept(raw, 2)
	if ok {
		t.Fatalf("accept(%q, max 2) accepted, want rejection", raw)
	}
	if reason != rejectTooLong {
		t.Errorf("reason = %s, want %s", reason, rejectTooLong)
	}
}
