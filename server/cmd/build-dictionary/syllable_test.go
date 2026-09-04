package main

import "testing"

func TestIsVietnameseSyllableAccepts(t *testing.T) {
	// A parser that matches the longest onset greedily rejects the whole g+i
	// family, which includes some of the most common words in the language.
	gFamily := []string{"gì", "gỉ", "gìn", "giữ", "gi", "giả", "giáo", "gia"}
	// Ordinary syllables across the onset, nucleus and coda inventories.
	ordinary := []string{
		"pháp", "luật", "ngôn", "ngữ", "nghiêng", "nghề", "khuya", "quyền",
		"tuyết", "người", "đường", "ươm", "ăn", "áo", "yêu", "oanh", "uy",
		"xoài", "khoẻ", "thuý", "quý", "chương", "trường", "nhanh", "phở",
	}

	for _, group := range [][]string{gFamily, ordinary} {
		for _, s := range group {
			if !isVietnameseSyllable(s) {
				t.Errorf("isVietnameseSyllable(%q) = false, want true", s)
			}
		}
	}
}

func TestIsVietnameseSyllableRejects(t *testing.T) {
	// Foreign syllables that use only letters Vietnamese has, so an
	// alphabet-level check cannot catch them.
	tests := []struct{ in, why string }{
		{"credit", "onset cluster cr"},
		{"card", "coda rd"},
		{"dress", "onset cluster dr and coda ss"},
		{"code", "coda de"},
		{"come", "coda me"},
		{"york", "coda rk"},
		{"cup", "valid shape but see note"}, // sanity: see below
		{"", "empty"},
	}

	for _, tc := range tests {
		if tc.in == "cup" {
			// "cup" is phonotactically legal in Vietnamese (c + u + p), so it
			// is NOT rejected here. Documented so the expectation is explicit.
			if !isVietnameseSyllable("cup") {
				t.Error(`isVietnameseSyllable("cup") = false; it is a legal Vietnamese shape`)
			}
			continue
		}
		if isVietnameseSyllable(tc.in) {
			t.Errorf("isVietnameseSyllable(%q) = true, want false (%s)", tc.in, tc.why)
		}
	}
}

func TestStripDiacritics(t *testing.T) {
	tests := map[string]string{
		"ngữ":   "ngu",
		"đường": "đuong",
		"khoẻ":  "khoe",
		"pháp":  "phap",
	}

	for in, want := range tests {
		if got := stripDiacritics(in); got != want {
			t.Errorf("stripDiacritics(%q) = %q, want %q", in, got, want)
		}
	}
}
