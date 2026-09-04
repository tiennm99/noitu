package main

import (
	"sort"
	"testing"
)

// Vietnamese writes the same syllable with the tone mark on either vowel of an
// oa/oe/uy cluster. Both spellings are typed by real players, so both must
// resolve to one dictionary entry.
func TestToneShiftVariant(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"oa, tone on first", "hòa", "hoà"},
		{"oa, tone on second", "hoà", "hòa"},
		{"oe, tone on first", "khỏe", "khoẻ"},
		{"oe, tone on second", "khoẻ", "khỏe"},
		{"uy, tone on first", "thúy", "thuý"},
		{"uy, tone on second", "thuý", "thúy"},
		{"nang tone on oa", "họa", "hoạ"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := toneShiftVariant(tc.in)
			if !ok {
				t.Fatalf("toneShiftVariant(%q) produced no variant, want %q", tc.in, tc.want)
			}
			if got != tc.want {
				t.Errorf("toneShiftVariant(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestToneShiftVariantRejects(t *testing.T) {
	tests := []struct{ in, why string }{
		{"quý", `"qu" is a consonant onset, not a vowel cluster — no "qúy"`},
		{"hoàn", `closed syllable: final consonant, so no "hòan"`},
		{"roàng", `closed syllable: final "ng", so no "ròang"`},
		{"hoài", `triphthong "oai", so no "hòai"`},
		{"khoẻn", `closed "oe" syllable`},
		{"quả", "no shiftable cluster"},
		{"luật", "no shiftable cluster"},
		{"pháp", "no shiftable cluster"},
		{"ngữ", "no shiftable cluster"},
		{"điện", "no shiftable cluster"},
		{"tính", "no shiftable cluster"},
	}

	for _, tc := range tests {
		if got, ok := toneShiftVariant(tc.in); ok {
			t.Errorf("toneShiftVariant(%q) = %q, want no variant (%s)", tc.in, got, tc.why)
		}
	}
}

func TestIYVariant(t *testing.T) {
	tests := []struct{ in, want string }{
		{"quý", "quí"},
		{"quí", "quý"},
		{"lý", "lí"},
		{"mỹ", "mĩ"},
		{"kỹ", "kĩ"},
		{"sĩ", "sỹ"},
		{"tỷ", "tỉ"},
		{"hy", "hi"},
	}

	for _, tc := range tests {
		got, ok := iyVariant(tc.in)
		if !ok {
			t.Errorf("iyVariant(%q) produced no variant, want %q", tc.in, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("iyVariant(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIYVariantRejects(t *testing.T) {
	// "gi" and "ch" are digraphs that only ever take i; closed syllables and
	// diphthongs are out of scope entirely.
	for _, in := range []string{"gì", "chì", "tính", "kia", "đi", "nghi", "phi", "bi"} {
		if got, ok := iyVariant(in); ok {
			t.Errorf("iyVariant(%q) = %q, want no variant", in, got)
		}
	}
}

func TestVariantsFor(t *testing.T) {
	got := variantsFor("hòa bình")
	want := []string{"hoà bình"}
	assertSameStrings(t, got, want)

	got = variantsFor("pháp luật")
	if len(got) != 0 {
		t.Errorf("variantsFor(%q) = %v, want none", "pháp luật", got)
	}
}

// A word with two variable syllables must offer every combination, not just the
// half-and-half forms. "hoá lí" is the corpus spelling but "hóa lý" is the one
// most people type, and varying syllables independently never produced it.
func TestVariantsForVariesSyllablesTogether(t *testing.T) {
	// "hoá" carries the acute tone, so shifting it gives "hóa" (not "hòa").
	got := variantsFor("hoá lí")
	want := []string{"hóa lí", "hoá lý", "hóa lý"}
	assertSameStrings(t, got, want)
}

func TestVariantsForCapsCombinations(t *testing.T) {
	// Every syllable variable: 2^6 = 64 combinations before the cap.
	word := "hoá hoá hoá hoá hoá hoá"
	if got := len(variantsFor(word)); got > maxVariantsPerWord {
		t.Errorf("variantsFor produced %d variants, want at most %d", got, maxVariantsPerWord)
	}
}

// "v" and "s" do not alternate productively: "chức vị" must never yield
// "chức vỵ". Only the handful of genuinely alternating syllables are listed.
func TestIYRejectsNonAlternatingOnsets(t *testing.T) {
	for _, in := range []string{"vị", "vì", "vỷ", "sị", "sỳ", "si"} {
		if got, ok := iyVariant(in); ok {
			t.Errorf("iyVariant(%q) = %q, want no variant", in, got)
		}
	}
}

func TestIYExplicitPairs(t *testing.T) {
	for in, want := range map[string]string{"sĩ": "sỹ", "sỹ": "sĩ", "vĩ": "vỹ", "vỹ": "vĩ"} {
		got, ok := iyVariant(in)
		if !ok {
			t.Errorf("iyVariant(%q) produced no variant, want %q", in, want)
			continue
		}
		if got != want {
			t.Errorf("iyVariant(%q) = %q, want %q", in, got, want)
		}
	}
}

// With three words claiming one variant, a plain delete would let the third
// reinsert it and the winner would depend on map iteration order.
func TestBuildAliasesPoisonsAmbiguousVariants(t *testing.T) {
	words := map[string]entry{
		"lí do": {word: "lí do", first: "lí", last: "do", syllables: 2},
		"lý do": {word: "lý do", first: "lý", last: "do", syllables: 2},
	}

	for i := 0; i < 20; i++ {
		aliases, _ := buildAliases(words)
		if canonical, ok := aliases["lí do"]; ok {
			t.Fatalf("run %d: real word aliased to %q", i, canonical)
		}
		if canonical, ok := aliases["lý do"]; ok {
			t.Fatalf("run %d: real word aliased to %q", i, canonical)
		}
	}
}

// A generated variant that is itself a real word must not become an alias:
// doing so would let one entry answer to another entry's name.
func TestBuildAliasesSkipsCollisions(t *testing.T) {
	words := map[string]entry{
		"hòa bình": {word: "hòa bình", first: "hòa", last: "bình", syllables: 2},
		"hoà bình": {word: "hoà bình", first: "hoà", last: "bình", syllables: 2},
	}

	aliases, collisions := buildAliases(words)

	if len(aliases) != 0 {
		t.Errorf("aliases = %v, want none (both spellings are real words)", aliases)
	}
	if collisions != 2 {
		t.Errorf("collisions = %d, want 2", collisions)
	}
}

func TestBuildAliasesMapsToCanonical(t *testing.T) {
	words := map[string]entry{
		"hòa bình": {word: "hòa bình", first: "hòa", last: "bình", syllables: 2},
	}

	aliases, _ := buildAliases(words)

	if got := aliases["hoà bình"]; got != "hòa bình" {
		t.Errorf("aliases[%q] = %q, want %q", "hoà bình", got, "hòa bình")
	}
}

func assertSameStrings(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
