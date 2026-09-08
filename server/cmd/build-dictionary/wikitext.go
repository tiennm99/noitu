package main

import (
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// This file reads the wikitext of one Wiktionary tiếng Việt page: it finds the
// Vietnamese section, walks its part-of-speech headings and turns each
// definition line into plain text.
//
// The wiki is mid-migration between two markup dialects and both are live
// (2026-09-01 dump: 35,885 legacy pages, 7,129 new):
//
//	legacy   {{-vie-}} opens the section, {{-noun-}} and kin are the headings,
//	         and the section ends at the next {{-xxx-}} whose code is a
//	         language rather than a heading.
//	new      == {{langname|vi}} == opens the section, === {{ĐM|noun}} === or
//	         === {{section|noun}} === are the headings, and the next level-2
//	         heading ends it.
//
// Nothing here is a general wikitext parser. It knows exactly the shapes a
// definition line takes on this wiki and drops the rest on purpose; what
// survives is plain text, capped, safe to render as text and never as markup.

// sense is one definition with the Vietnamese part-of-speech label of the
// heading it sat under. pos is empty when the heading was one the label map
// does not know, never a reason to drop the definition.
type sense struct {
	pos   string
	gloss string
}

const (
	// maxSenses and maxGlossRunes bound what one word carries to the client.
	maxSenses     = 5
	maxGlossRunes = 200
	// ellipsis marks a definition cut at maxGlossRunes.
	ellipsis = "…"
)

// posLabelMap maps a part-of-speech heading code, the same in both dialects
// ({{-noun-}}, {{ĐM|noun}}, {{section|noun}}, {{vi-noun}}), to the Vietnamese
// label the client shows in front of a sense.
//
// Codes and their frequencies in Vietnamese sections of the 2026-09-01 dump:
// noun 13,674 + n 647 · verb 7,595 + v 353 · adj 5,318 + adjc 526 · place 3,352
// · pr-noun 1,397 + 329 · adv 982 · phrase 412 · proverb 295 · idiom 241 ·
// interj 169 · pronoun 158 · num 123 · conj 88 · prep 88 · part 29. Note that
// "pron" on this wiki is pronunciation, not pronoun.
var posLabelMap = map[string]string{
	"noun": "danh từ", "n": "danh từ",
	"verb": "động từ", "v": "động từ", "tr-verb": "động từ", "intr-verb": "động từ", "aux-verb": "động từ",
	"adj": "tính từ", "adjc": "tính từ", "adjective": "tính từ",
	"adv": "phó từ", "adverb": "phó từ", "advb": "phó từ",
	"pr-noun": "danh từ riêng", "proper": "danh từ riêng", "propn": "danh từ riêng", "proper noun": "danh từ riêng", "name": "danh từ riêng",
	"pr-adj":  "tính từ riêng",
	"place":   "địa danh",
	"pronoun": "đại từ", "per-pronoun": "đại từ",
	"num": "số từ", "numeral": "số từ",
	"conj": "liên từ", "conjunction": "liên từ",
	"prep":   "giới từ",
	"interj": "thán từ", "intj": "thán từ", "interjection": "thán từ",
	"part": "trợ từ", "particle": "trợ từ",
	"phrase":  "cụm từ",
	"idiom":   "thành ngữ",
	"proverb": "tục ngữ", "prov": "tục ngữ",
	"abbr": "viết tắt", "abr": "viết tắt",
	"prefix": "tiền tố",
	"suffix": "hậu tố",
	"letter": "chữ cái",
	"symbol": "ký hiệu",
}

// otherSectionCodes are heading codes that are not parts of speech: they sit
// inside a language section and reset the current label without being
// counted as unmapped. Frequencies in Vietnamese sections, 2026-09-01 dump:
// pron 36,533 · ref 27,438 · trans 16,073 · paro 8,058 · etym 4,267 · syn
// 3,165 · info 1,846 · hanviet 1,601 · hanviet-t 1,441 · etymology 1,429 ·
// reference 1,426 · see 959 · related 461 · drv 277 · synonym 262 · ant 236 ·
// desction 128 · usage 110 · expr 92 · homo 57 · desc 54 · further 48 · forms
// 41 · note 33 · derived 28 · anagram 24 · compound 21 · redup 20 · translit 17
// · cat 16 · antonym 15. "dfn" (4,615) is a "definitions" heading placed under a
// part-of-speech heading, so it is a heading for the section boundary but
// transparent to the label: see classifyHeading.
var otherSectionCodes = map[string]bool{
	"pron": true, "pronunciation": true, "ref": true, "reference": true, "references": true,
	"trans": true, "translations": true, "paro": true, "paronym": true, "etym": true, "etymology": true,
	"syn": true, "synonym": true, "ant": true, "antonym": true, "info": true,
	"hanviet": true, "hanviet-t": true, "see": true, "see also": true, "related": true, "rel": true,
	"related terms": true, "drv": true, "der": true, "derived": true, "derived terms": true,
	"desction": true, "desc": true, "usage": true, "usage notes": true, "expr": true, "homo": true,
	"further": true, "further reading": true, "forms": true, "note": true, "anagram": true,
	"anagrams": true, "ana": true, "compound": true, "redup": true, "translit": true, "cat": true,
	"coord": true, "coordinate": true, "alt": true, "alter": true, "alter form": true,
	"alternative form": true, "alternative forms": true, "alternative script": true,
	"glyph origin": true, "han": true, "nôm": true, "han character": true, "kanji": true,
	"rom": true, "romanization": true, "mut": true, "participle": true, "ptcp": true,
	"syllable": true, "article": true, "contr": true, "cmavo": true, "dfn": true, "com": true,
	// The same sections written out in Vietnamese, as a few new-dialect pages do.
	"phát âm": true, "từ nguyên": true, "từ nguyên 1": true, "từ nguyên 2": true, "tham khảo": true,
	"xem thêm": true, "cách viết khác": true, "phồn thể": true, "hán-nôm": true, "hán nôm": true,
	"chữ hán": true, "chữ nôm": true, "chú ý": true, "đồng nghĩa": true, "từ đồng nghĩa": true,
	"bản dịch": true, "dịch": true, "dấu phụ": true, "liên kết ngoài": true, "thuật ngữ liên quan": true,
	"từ tương tự": true, "meronym": true, "meronyms": true, "nguồn gốc ký tự chữ nôm": true,
}

var (
	// legacyTemplate matches one {{-code-}} template, optionally with
	// parameters: {{-noun-}}, {{-pr-noun-}}, {{-vie-|...}}. Not anchored: a few
	// pages run {{-vie-}}{{-pron-}}{{vie-pron|…}}{{-place-}} together on one
	// line, so a line is read as headings when it starts with one and may
	// carry several.
	legacyTemplate = regexp.MustCompile(`\{\{-([A-Za-z0-9-]+?)-(?:\|[^}]*)?\}\}`)
	// headingLine matches == text == at any level and captures the level.
	headingLine = regexp.MustCompile(`^(={2,6})\s*(.*?)\s*=+\s*$`)
	// sectionTemplate captures the code of {{ĐM|code}} and {{section|code}}.
	sectionTemplate = regexp.MustCompile(`\{\{(?:ĐM|đm|DM|dm|section)\|([^}|]+)`)
	// headwordTemplate captures the code of {{vi-code}} / {{vie-code}} at the
	// start of a line: the new dialect's headword line, which names the POS.
	headwordTemplate = regexp.MustCompile(`^\{\{vie?-([a-z -]+)`)
	// langnameVi is the new dialect's Vietnamese section heading text.
	langnameVi = regexp.MustCompile(`^\{\{langname\|vi\}\}$`)

	htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	refElement  = regexp.MustCompile(`(?s)<ref\b[^>/]*/>|<ref\b[^>]*>.*?</ref>`)
	anyTag      = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
	spaces      = regexp.MustCompile(`\s+`)
)

// isLegacyHeading reports whether a {{-code-}} is a heading inside a language
// section. Every other code — language and script codes such as eng, tyz,
// aav-qal, Latn — ends the Vietnamese section.
func isLegacyHeading(code string) bool {
	_, pos := posLabelMap[code]
	return pos || otherSectionCodes[code]
}

// vietnameseSection returns the wikitext of the page's Vietnamese section and
// which dialect opened it: "legacy", "new", or "" when the page has none.
// When both dialects open a section on one page the first one in the text
// wins and both is reported so the build log can count it. ender is the
// {{-code-}} that closed a legacy section, empty when a heading or the end of
// the page did: a heading code missing from the maps shows up there as a
// section-ending code, which is the signal that definitions are being lost.
func vietnameseSection(text string) (section, dialect string, both bool, ender string) {
	lines := strings.Split(text, "\n")
	legacyAt, newAt := -1, -1
	for i, line := range lines {
		line = strings.TrimRight(line, "\r ")
		if legacyAt < 0 && strings.HasPrefix(line, "{{-") {
			for _, m := range legacyTemplate.FindAllStringSubmatchIndex(line, -1) {
				if line[m[2]:m[3]] == "vie" {
					legacyAt = i
					// Whatever follows the marker on its own line belongs to
					// the section.
					lines[i] = line[m[1]:]
					break
				}
			}
		}
		if newAt < 0 {
			if m := headingLine.FindStringSubmatch(line); m != nil && len(m[1]) == 2 && langnameVi.MatchString(m[2]) {
				newAt = i
			}
		}
	}
	both = legacyAt >= 0 && newAt >= 0
	switch {
	case legacyAt < 0 && newAt < 0:
		return "", "", false, ""
	case newAt < 0 || (legacyAt >= 0 && legacyAt < newAt):
		section, ender = legacySection(lines[legacyAt:])
		return section, "legacy", both, ender
	default:
		return newSection(lines[newAt+1:]), "new", both, ""
	}
}

// legacySection runs from after {{-vie-}} to the next {{-xxx-}} whose code is
// not a heading, or the next level-2 heading, which is what a new-dialect
// language section on a mixed page opens with. A language code sharing a line
// with Vietnamese headings ends the section at that line; the line is lost,
// which is the conservative side of a rare shape.
func legacySection(lines []string) (section, ender string) {
	for i, line := range lines {
		line = strings.TrimRight(line, "\r ")
		if strings.HasPrefix(line, "{{-") {
			for _, m := range legacyTemplate.FindAllStringSubmatch(line, -1) {
				if !isLegacyHeading(m[1]) {
					return strings.Join(lines[:i], "\n"), m[1]
				}
			}
		}
		if m := headingLine.FindStringSubmatch(line); m != nil && len(m[1]) == 2 {
			return strings.Join(lines[:i], "\n"), ""
		}
	}
	return strings.Join(lines, "\n"), ""
}

// newSection runs from after == {{langname|vi}} == to the next level-2
// heading.
func newSection(lines []string) string {
	for i, line := range lines {
		line = strings.TrimRight(line, "\r ")
		if m := headingLine.FindStringSubmatch(line); m != nil && len(m[1]) == 2 {
			return strings.Join(lines[:i], "\n")
		}
	}
	return strings.Join(lines, "\n")
}

// headingKind says what a heading line means for the label of the
// definitions under it.
type headingKind int

const (
	notHeading headingKind = iota
	// posHeading names a part of speech: the code decides the label.
	posHeading
	// otherHeading is a section such as pronunciation or etymology: the label
	// resets to empty and nothing is counted as unmapped.
	otherHeading
)

// classifyHeading reads one line as a heading in either dialect.
//
//	{{-noun-}}                       legacy heading; the last of several on
//	                                 one line decides
//	=== {{ĐM|noun}} ===              new heading
//	=== {{section|n}} ===            new heading, shorthand code
//	=== Danh từ ===                  new heading written out
//	{{vi-noun}} / {{vie-noun}}       new headword line; refines the POS only
func classifyHeading(line string) (code string, kind headingKind) {
	line = strings.TrimRight(line, "\r ")
	if strings.HasPrefix(line, "{{-") {
		kind = notHeading
		for _, m := range legacyTemplate.FindAllStringSubmatch(line, -1) {
			if c, k := classifyCode(m[1]); k != notHeading {
				code, kind = c, k
			}
		}
		return code, kind
	}
	if m := headingLine.FindStringSubmatch(line); m != nil && len(m[1]) >= 3 {
		text := m[2]
		if sm := sectionTemplate.FindStringSubmatch(text); sm != nil {
			code = strings.TrimSpace(sm[1])
		} else {
			code = strings.ToLower(text)
		}
		return classifyCode(code)
	}
	if m := headwordTemplate.FindStringSubmatch(line); m != nil {
		// Only a headword template whose code is a part of speech counts;
		// {{vi-pron}}, {{vi-etym-sino}} and kin are not headings.
		code = strings.TrimSpace(m[1])
		if _, ok := posLabelMap[code]; ok {
			return code, posHeading
		}
	}
	return "", notHeading
}

// classifyCode sorts a heading code seen in either dialect. "dfn" is the one
// heading that changes nothing: the wiki places {{-dfn-}} under {{-noun-}} to
// introduce the definitions, so the label above it must carry through.
func classifyCode(code string) (string, headingKind) {
	switch {
	case code == "dfn":
		return code, notHeading
	case otherSectionCodes[code]:
		return code, otherHeading
	}
	return code, posHeading
}

// isLabelValue reports whether a heading was written out as one of the
// Vietnamese labels already ("Danh từ").
func isLabelValue(text string) bool {
	for _, l := range posLabelMap {
		if l == text {
			return true
		}
	}
	return false
}

// posLabel maps a heading code to its Vietnamese label: through the map, or
// as itself when the heading was already written out in Vietnamese.
func posLabel(code string) (label string, mapped bool) {
	if label, ok := posLabelMap[code]; ok {
		return label, true
	}
	if isLabelValue(code) {
		return code, true
	}
	return "", false
}

// sectionStats counts what the scanner saw across sections, for the build log.
type sectionStats struct {
	pos         map[string]int // part-of-speech heading codes seen
	unmappedPos map[string]int // part-of-speech heading codes with no label
	defsKept    int
	defsEmpty   int            // definitions that stripped to nothing
	defsCut     int            // definitions cut at maxGlossRunes
	dropped     map[string]int // template names dropped whole
}

func newSectionStats() *sectionStats {
	return &sectionStats{
		pos:         make(map[string]int),
		unmappedPos: make(map[string]int),
		dropped:     make(map[string]int),
	}
}

// definitions walks the Vietnamese section and returns its senses in page
// order, at most maxSenses of them, each labelled with the part of speech of
// the heading above it. Every heading and definition is counted in stats
// whether or not it made the cut.
func definitions(section string, stats *sectionStats) []sense {
	var senses []sense
	pos := ""
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimRight(line, "\r ")
		switch code, kind := classifyHeading(line); kind {
		case posHeading:
			stats.pos[code]++
			label, mapped := posLabel(code)
			if !mapped {
				stats.unmappedPos[code]++
			}
			pos = label
			continue
		case otherHeading:
			pos = ""
			continue
		}
		if !isDefinitionLine(line) {
			continue
		}
		gloss := stripWikitext(line[1:], stats.dropped)
		if gloss == "" {
			stats.defsEmpty++
			continue
		}
		if cut, wasCut := capGloss(gloss); wasCut {
			stats.defsCut++
			gloss = cut
		}
		stats.defsKept++
		if len(senses) < maxSenses {
			senses = append(senses, sense{pos: pos, gloss: gloss})
		}
	}
	return senses
}

// isDefinitionLine accepts a top-level numbered item and nothing under it:
// "# text" and "#text" are definitions; "#: example", "#* quotation", "## sub-
// sense" and "#; term" are not.
func isDefinitionLine(line string) bool {
	if len(line) < 2 || line[0] != '#' {
		return false
	}
	switch line[1] {
	case '#', ':', '*', ';':
		return false
	}
	return true
}

// stripWikitext turns one definition line into plain text. Lossy on purpose:
// links keep their display text, formatting goes, the handful of templates
// that carry definition text are unwrapped and every other template is
// dropped whole (its name counted in dropped when non-nil). The result is
// trimmed and whitespace-collapsed; a result with no letter or digit is empty.
func stripWikitext(s string, dropped map[string]int) string {
	s = htmlComment.ReplaceAllString(s, "")
	s = refElement.ReplaceAllString(s, "")
	s = anyTag.ReplaceAllString(s, "")
	s = stripTemplates(s, dropped)
	s = stripLinks(s)
	s = strings.ReplaceAll(s, "'''", "")
	s = strings.ReplaceAll(s, "''", "")
	s = html.UnescapeString(s)
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			// Cc and Cf: control characters, and format characters such as a
			// bidi override or a zero-width space, which could reshape the
			// rest of a rendered line.
			return -1
		case unicode.IsSpace(r):
			// Non-breaking and other Unicode spaces become plain ones so the
			// ASCII-only collapse below catches them.
			return ' '
		}
		return r
	}, s)
	s = strings.TrimSpace(spaces.ReplaceAllString(s, " "))
	// A stray space before sentence punctuation is what unwrapping a template
	// at the end of a clause leaves behind.
	for _, p := range []string{" .", " ,", " ;", " :", " )"} {
		s = strings.ReplaceAll(s, p, p[1:])
	}
	if !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) {
		return ""
	}
	return s
}

// stripTemplates replaces every outermost {{...}} with its plain-text
// rendering. Nesting is tracked by depth, so a template inside a kept
// template's parameter is rendered recursively and one inside a dropped
// template goes with it.
func stripTemplates(s string, dropped map[string]int) string {
	var out strings.Builder
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch {
		case strings.HasPrefix(s[i:], "{{"):
			if depth == 0 {
				start = i + 2
			}
			depth++
			i++
		case strings.HasPrefix(s[i:], "}}"):
			// A closer with nothing open is stray markup, not text.
			if depth > 0 {
				depth--
				if depth == 0 {
					out.WriteString(renderTemplate(s[start:i], dropped))
				}
			}
			i++
		case depth == 0:
			out.WriteByte(s[i])
		}
	}
	if depth > 0 && dropped != nil {
		// Unbalanced braces: whatever opened and never closed is dropped, as a
		// template would be, rather than leaking half a template into a gloss.
		dropped["(unclosed)"]++
	}
	return out.String()
}

// renderTemplate maps one template body (the text between {{ and }}) to plain
// text. body still contains any nested templates verbatim.
//
// The kept templates are the ones that carry definition text on this wiki:
// context labels in four spellings, links in five, place descriptions,
// non-gloss definitions and the two cross-reference templates a "dfn" section
// is usually made of. Everything else is presentation or classification.
func renderTemplate(body string, dropped map[string]int) string {
	parts := splitTemplate(body)
	name := strings.ToLower(strings.TrimSpace(parts[0]))
	// Positional parameters only; key=value ones are presentation hints.
	var params []string
	for _, p := range parts[1:] {
		if strings.Contains(p, "=") && !strings.Contains(p, "[[") && !strings.Contains(p, "{{") {
			continue
		}
		params = append(params, strings.TrimSpace(stripTemplates(strings.TrimSpace(p), dropped)))
	}
	// A leading language code is markup, whether it is ours or a neighbour's
	// pasted in: the label templates take it first, the link ones too. Only
	// when something follows it, though: {{q|con}} is a one-word qualifier,
	// not a language.
	dropLang := func(ps []string) []string {
		if len(ps) > 1 && isLangCode(ps[0]) {
			return ps[1:]
		}
		return ps
	}
	switch name {
	case "label", "lb", "nhãn", "context", "term", "gloss", "qualifier", "q":
		params = dropLang(params)
		if len(params) == 0 {
			return ""
		}
		return "(" + strings.Join(params, ", ") + ")"
	case "l", "vi-l", "w", "m", "link":
		params = dropLang(params)
		if len(params) == 0 {
			return ""
		}
		return params[len(params)-1]
	case "n-g", "non-gloss", "non-gloss definition":
		return strings.Join(params, " ")
	case "see-entry", "like-entry":
		if len(params) == 0 {
			return ""
		}
		return "Xem " + params[0]
	case "place":
		params = dropLang(params)
		for i, p := range params {
			// "c/Việt Nam" is a typed place: the type prefix is markup.
			if len(p) > 2 && p[1] == '/' && p[0] >= 'a' && p[0] <= 'z' {
				params[i] = p[2:]
			}
		}
		return strings.Join(params, ", ")
	}
	if dropped != nil {
		dropped[name]++
	}
	return ""
}

// isLangCode reports whether a template parameter is a language code rather
// than text: two or three lowercase ASCII letters, optionally with a
// hyphenated variant such as "nan-hbl".
func isLangCode(p string) bool {
	if len(p) < 2 || len(p) > 11 {
		return false
	}
	letters := 0
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z':
			letters++
		case r == '-':
			if letters < 2 {
				return false
			}
			letters = 0
		default:
			return false
		}
	}
	return letters >= 2 && letters <= 3
}

// splitTemplate splits a template body on | outside nested braces and
// brackets, so a link or template inside a parameter is not cut in two.
func splitTemplate(body string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(body); i++ {
		switch {
		case strings.HasPrefix(body[i:], "{{") || strings.HasPrefix(body[i:], "[["):
			depth++
			i++
		case strings.HasPrefix(body[i:], "}}") || strings.HasPrefix(body[i:], "]]"):
			if depth > 0 {
				depth--
			}
			i++
		case body[i] == '|' && depth == 0:
			parts = append(parts, body[start:i])
			start = i + 1
		}
	}
	return append(parts, body[start:])
}

// stripLinks renders wiki links as their display text and drops category
// links, which are classification rather than definition.
func stripLinks(s string) string {
	var out strings.Builder
	for {
		open := strings.Index(s, "[[")
		if open < 0 {
			break
		}
		close := strings.Index(s[open:], "]]")
		if close < 0 {
			break
		}
		out.WriteString(s[:open])
		inner := s[open+2 : open+close]
		lower := strings.ToLower(inner)
		if !strings.HasPrefix(lower, "thể loại:") && !strings.HasPrefix(lower, "category:") {
			if bar := strings.LastIndex(inner, "|"); bar >= 0 {
				inner = inner[bar+1:]
			}
			out.WriteString(inner)
		}
		s = s[open+close+2:]
	}
	out.WriteString(s)
	s = out.String()

	// External links: [http://… label] → label; a bare URL in brackets goes.
	out.Reset()
	for {
		open := strings.Index(s, "[http")
		if open < 0 {
			break
		}
		close := strings.Index(s[open:], "]")
		if close < 0 {
			break
		}
		out.WriteString(s[:open])
		inner := s[open+1 : open+close]
		if sp := strings.IndexByte(inner, ' '); sp >= 0 {
			out.WriteString(inner[sp+1:])
		}
		s = s[open+close+1:]
	}
	out.WriteString(s)
	return out.String()
}

// capGloss cuts a definition longer than maxGlossRunes at the last space
// before the limit and marks the cut with an ellipsis.
func capGloss(s string) (string, bool) {
	if utf8.RuneCountInString(s) <= maxGlossRunes {
		return s, false
	}
	runes := []rune(s)
	head := string(runes[:maxGlossRunes-utf8.RuneCountInString(ellipsis)])
	if sp := strings.LastIndexByte(head, ' '); sp > 0 {
		head = head[:sp]
	}
	return strings.TrimRight(head, " ,;:") + ellipsis, true
}
