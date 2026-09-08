package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStripWikitext(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"links keep display text",
			"[[chỗ|Chỗ]] [[râm]] [[mát]], do [[trời]] có [[mây]] hoặc do không bị [[nắng]] [[chiếu]].",
			"Chỗ râm mát, do trời có mây hoặc do không bị nắng chiếu."},
		{"place template keeps its parameters and drops the type prefix",
			"{{place|vi|thủ đô|c/Việt Nam}}.",
			"thủ đô, Việt Nam."},
		{"label becomes a parenthesis",
			"{{label|vi|thuộc lịch sử}} Một [[tỉnh]] cũ của [[Việt Nam]] vào nửa cuối thế kỷ XIX.",
			"(thuộc lịch sử) Một tỉnh cũ của Việt Nam vào nửa cuối thế kỷ XIX."},
		{"Vietnamese label spellings",
			"{{nhãn|vi|tin học}} {{context|cũ}} {{term|Hóa học}} Cấu trúc.",
			"(tin học) (cũ) (Hóa học) Cấu trúc."},
		{"nested template inside a kept one",
			"{{label|vi|{{w|Hà Nội}}}} Thủ đô.",
			"(Hà Nội) Thủ đô."},
		{"unknown template dropped whole, nesting included",
			"{{rfdef|vi|{{w|x}}}}",
			""},
		{"definition that is only a cross-reference",
			"{{see-entry|bà la sát}}.",
			"Xem bà la sát."},
		{"non-gloss definition",
			"{{n-g|Trợ từ nhấn mạnh.}}",
			"Trợ từ nhấn mạnh."},
		{"link templates keep the last parameter",
			"{{l|vi|nói}}, {{l|vi|nói năng|nói năng (hiếm)}} và {{w|Việt Nam}}.",
			"nói, nói năng (hiếm) và Việt Nam."},
		{"ref mid-sentence and a lone ref",
			"Một loài [[cá]]<ref>Từ điển</ref> nước ngọt<ref name=\"a\" />.",
			"Một loài cá nước ngọt."},
		{"comment, bold, italic, entities",
			"'''Rất''' ''nhanh''<!-- todo -->&nbsp;và&amp;mạnh.",
			"Rất nhanh và&mạnh."},
		{"category link dropped, external link keeps label",
			"Một [[thành phố]] [[Thể loại:Địa danh]] ([http://example.org trang web]).",
			"Một thành phố (trang web)."},
		{"named parameters are not text",
			"{{lb|vi|thơ ca|sort=x}} Câu.",
			"(thơ ca) Câu."},
		{"a lone short parameter is text, not a language code",
			"{{q|con}} Một loài vật, {{l|con}} là con.",
			"(con) Một loài vật, con là con."},
		{"format and bidi characters are dropped",
			"M\u200bột \u202enghĩa\u202c.",
			"Một nghĩa."},
		{"a stray closer is not text", "Một }} nghĩa.", "Một nghĩa."},
		{"only punctuation is empty", "(...).", ""},
		{"control characters and whitespace collapse", "  Một  từ \t hai  ", "Một từ hai"},
		{"unclosed template does not leak", "Một {{label|vi|x từ.", "Một"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripWikitext(c.in, nil); got != c.want {
				t.Errorf("stripWikitext(%q)\n got %q\nwant %q", c.in, got, c.want)
			}
		})
	}
}

func TestStripWikitextCountsDroppedTemplates(t *testing.T) {
	dropped := make(map[string]int)
	stripWikitext("{{rfdef|vi}} {{RfDef|vi}} {{senseid|vi|x}}", dropped)
	if dropped["rfdef"] != 2 || dropped["senseid"] != 1 {
		t.Errorf("dropped = %v, want rfdef 2 (case-folded), senseid 1", dropped)
	}
}

func TestCapGlossCutsAtAWordBoundary(t *testing.T) {
	word := "từ "
	long := strings.Repeat(word, 120) // 360 runes
	got, cut := capGloss(long)
	if !cut {
		t.Fatal("a 360-rune gloss was not cut")
	}
	if n := utf8.RuneCountInString(got); n > maxGlossRunes {
		t.Errorf("cut gloss is %d runes, want at most %d", n, maxGlossRunes)
	}
	if !strings.HasSuffix(got, "từ"+ellipsis) {
		t.Errorf("cut gloss %q does not end on a whole word plus the ellipsis", got)
	}
	if short, cut := capGloss("ngắn"); cut || short != "ngắn" {
		t.Errorf("a short gloss was changed: %q %v", short, cut)
	}
}

const legacyPage = `{{-vie-}}
{{-pron-}}
{{vie-pron|học sinh}}

{{-noun-}}
# [[người|Người]] [[học]] ở [[nhà trường|trường]].
#: ''Học sinh giỏi.''
#{{label|vi|cũ}} [[môn đệ|Môn đệ]].

{{-verb-}}
# [[đi học|Đi học]].

{{-trans-}}
* {{eng}}: {{t|en|student}}

{{-eng-}}
{{-noun-}}
# Student, in English.
`

const newPage = `== {{langname|vi}} ==
=== {{ĐM|etym}} ===
Hán-Việt.

=== {{ĐM|pr-noun}} ===
{{vi-pr-noun}}

# {{place|vi|thủ đô|c/Việt Nam}}.

=== {{section|v}} ===
# [[đi|Đi]] về thủ đô.

=== {{ĐM|xyz}} ===
# Một nghĩa dưới đề mục lạ.

== {{langname|en}} ==
=== {{ĐM|pr-noun}} ===
# The capital of Vietnam.
`

func TestVietnameseSectionLegacy(t *testing.T) {
	section, dialect, both, _ := vietnameseSection(legacyPage)
	if dialect != "legacy" || both {
		t.Fatalf("dialect = %q both = %v, want legacy false", dialect, both)
	}
	if strings.Contains(section, "Student") {
		t.Error("the English section leaked into the Vietnamese one")
	}
	if !strings.Contains(section, "{{-trans-}}") {
		t.Error("the translations heading, a section heading rather than a language, cut the section short")
	}
}

func TestVietnameseSectionNew(t *testing.T) {
	section, dialect, both, _ := vietnameseSection(newPage)
	if dialect != "new" || both {
		t.Fatalf("dialect = %q both = %v, want new false", dialect, both)
	}
	if strings.Contains(section, "capital of Vietnam") {
		t.Error("the English section leaked into the Vietnamese one")
	}
	if !strings.Contains(section, "đề mục lạ") {
		t.Error("a level-3 heading ended the section; only a level-2 heading may")
	}
}

func TestVietnameseSectionAbsentAndBoth(t *testing.T) {
	if _, dialect, _, _ := vietnameseSection("{{-eng-}}\n{{-noun-}}\n# Word."); dialect != "" {
		t.Errorf("an English-only page reported dialect %q", dialect)
	}
	mixed := "== {{langname|vi}} ==\n# Mới.\n" + legacyPage
	section, dialect, both, _ := vietnameseSection(mixed)
	if dialect != "new" || !both {
		t.Errorf("dialect = %q both = %v, want the first dialect in the text and both=true", dialect, both)
	}
	if strings.Contains(section, "Người học") {
		t.Error("the first section should end where the legacy page starts a language section")
	}
}

func TestDefinitionsLegacy(t *testing.T) {
	stats := newSectionStats()
	section, _, _, _ := vietnameseSection(legacyPage)
	got := definitions(section, stats)
	want := []sense{
		{"danh từ", "Người học ở trường."},
		{"danh từ", "(cũ) Môn đệ."},
		{"động từ", "Đi học."},
	}
	assertSenses(t, got, want)
	if stats.pos["noun"] != 1 || stats.pos["verb"] != 1 {
		t.Errorf("pos tally = %v, want noun 1 verb 1", stats.pos)
	}
	if stats.pos["pron"] != 0 || stats.pos["trans"] != 0 {
		t.Errorf("pronunciation and translations were tallied as parts of speech: %v", stats.pos)
	}
	if stats.defsKept != 3 {
		t.Errorf("defsKept = %d, want 3", stats.defsKept)
	}
}

func TestDefinitionsNewDialect(t *testing.T) {
	stats := newSectionStats()
	section, _, _, _ := vietnameseSection(newPage)
	got := definitions(section, stats)
	want := []sense{
		{"danh từ riêng", "thủ đô, Việt Nam."},
		{"động từ", "Đi về thủ đô."},
		{"", "Một nghĩa dưới đề mục lạ."},
	}
	assertSenses(t, got, want)
	if stats.unmappedPos["xyz"] != 1 {
		t.Errorf("unmapped headings = %v, want xyz 1", stats.unmappedPos)
	}
	if stats.unmappedPos["etym"] != 0 {
		t.Errorf("etymology counted as an unmapped part of speech: %v", stats.unmappedPos)
	}
}

func TestDefinitionsHeadwordLineAndWrittenOutHeading(t *testing.T) {
	section := "=== Danh từ ===\n# Một.\n{{vi-verb}}\n# Hai.\n{{vi-pron}}\n# Ba.\n=== Phát âm ===\n# Bốn."
	got := definitions(section, newSectionStats())
	want := []sense{{"danh từ", "Một."}, {"động từ", "Hai."}, {"động từ", "Ba."}, {"", "Bốn."}}
	assertSenses(t, got, want)
}

func TestDefinitionsSkipsEmptyAndCaps(t *testing.T) {
	stats := newSectionStats()
	lines := []string{"{{-noun-}}", "# {{rfdef|vi}}", "#: not a definition", "#* nor this", "## nor this"}
	for i := 0; i < 7; i++ {
		lines = append(lines, "# Nghĩa số "+string(rune('a'+i))+".")
	}
	lines = append(lines, "# "+strings.Repeat("dài ", 80))
	got := definitions(strings.Join(lines, "\n"), stats)
	if len(got) != maxSenses {
		t.Fatalf("got %d senses, want the cap of %d", len(got), maxSenses)
	}
	if got[0].gloss != "Nghĩa số a." {
		t.Errorf("first sense = %q, want the first real definition after the empty one", got[0].gloss)
	}
	if stats.defsEmpty != 1 || stats.dropped["rfdef"] != 1 {
		t.Errorf("empty = %d dropped = %v, want 1 and rfdef 1", stats.defsEmpty, stats.dropped)
	}
	if stats.defsKept != 8 || stats.defsCut != 1 {
		t.Errorf("kept = %d cut = %d, want 8 and 1 (counted past the cap)", stats.defsKept, stats.defsCut)
	}
}

func assertSenses(t *testing.T, got, want []sense) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d senses %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sense %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// A few pages run the section marker and the headings together on one line:
// {{-vie-}}{{-pron-}}{{vie-pron|Thượng|Hải}}{{-place-}}. The marker must still
// open the section and the last heading on the line must still label it.
func TestVietnameseSectionInlineHeadings(t *testing.T) {
	page := "{{-vie-}}{{-pron-}}{{vie-pron|Thượng|Hải}}{{-place-}}\n\n'''Thượng Hải'''\n# Thành phố lớn nhất [[Trung Quốc]].\n{{-eng-}}{{-noun-}}\n# Shanghai."
	section, dialect, _, ender := vietnameseSection(page)
	if dialect != "legacy" || ender != "eng" {
		t.Fatalf("dialect = %q ender = %q, want legacy ended by eng", dialect, ender)
	}
	if strings.Contains(section, "Shanghai") {
		t.Error("the English section, opened on a shared line, leaked in")
	}
	got := definitions(section, newSectionStats())
	assertSenses(t, got, []sense{{"địa danh", "Thành phố lớn nhất Trung Quốc."}})
}
