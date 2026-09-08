---
phase: 1
title: "Phase 1: Read the dump"
status: completed
priority: P1
effort: "6h"
dependencies: []
---

# Phase 1: Read the dump

## Overview

Replace the kaikki JSONL reader in `build-dictionary` with a streaming reader for the
Wikimedia `pages-articles.xml.bz2` dump that yields, per Vietnamese-section page, the title
for `accept()` and the stripped definition lines for the meanings table.

## Requirements

- Functional: `--dump <file>` reads a bzip2-compressed MediaWiki XML export. Pages with
  `ns != 0` or a `<redirect>` element are skipped and counted. Every other page's `<title>`
  and latest `<revision><text>` are handed to the section scanner.
- Functional: the scanner finds the Vietnamese section in either dialect — `{{-vie-}}` or a
  level-2 heading whose text is `{{langname|vi}}` — and stops at the section's end: for
  legacy pages, the next `{{-xxx-}}` template whose code is not in the section-code set; for
  new pages, the next level-2 heading. Pages with no Vietnamese section are counted, not
  read. Pages with a section in *both* dialects are read once, first dialect wins, counted.
- Functional: the title goes to `accept()` unchanged from today. Part of speech labels seen in
  the section (`{{-noun-}}`, `{{ĐM|noun}}`, `{{vi-noun}}` and kin, `pr-noun`/`place`
  included) are tallied for the log and never filter.
- Functional: within the section, every line starting with `# ` (exactly one `#`, then a
  space — not `#:`, `#*`, `##`) is a definition. It is stripped (see Architecture), capped at
  200 runes with an ellipsis, and appended to the page's senses up to 5. Empty results are
  dropped. Senses are kept in page order.
- Functional: each sense carries the part of speech of the heading it sits under, as a
  Vietnamese label from a fixed map keyed by the heading code — the same codes in both
  dialects (`{{-noun-}}`, `{{ĐM|noun}}`, `{{vi-noun}}` → `danh từ`). Starting map: `noun` danh
  từ, `verb` động từ, `adj` tính từ, `adv` phó từ, `pr-noun` danh từ riêng, `place` địa danh,
  `pron` đại từ, `num` số từ, `conj` liên từ, `prep` giới từ, `intj` thán từ, `part` trợ từ,
  `phrase` cụm từ, `idiom` thành ngữ, `prov` tục ngữ, `abbr` viết tắt, `prefix` tiền tố,
  `suffix` hậu tố, `char` chữ. A definition under a heading outside the map, or before any
  heading, has an empty label and is kept; unmapped codes are counted for the log. Step 1's
  frequency list decides what else the map needs. (Validation session 1.)
- Functional: two pages whose titles normalize to the same word (`Việt Nam` and `việt nam`
  both being real pages) merge: the word is one entry, senses concatenate in page order and
  the cap applies to the union.
- Functional: provenance is measured during the one streaming pass: SHA-256 of the compressed
  bytes as read, `source_pages` (pages with a Vietnamese section, redirects excluded),
  `source_fetched_at` from the file's modification time. `source_rows` is gone.
- Functional: the build log reports pages seen, ns0 pages, redirects skipped, pages with a
  Vietnamese section per dialect, POS tally, definitions kept, definitions dropped as empty
  after stripping, and the ten commonest template names dropped whole.
- Functional: `--kaikki` and `kaikki_list.go` and its tests are removed. Exactly one of
  `--dump` / `--words` must be given, same rule as today.
- Non-functional: a stream that is not bzip2, XML that ends mid-page, or a `<text>` element
  the decoder cannot read is an error naming the page title or byte offset. A count of
  Vietnamese-section pages under 20,000 is an error naming the count, distinct from the word
  floor, so a parser that silently misses a dialect is caught before `--min-words` is.
- Non-functional: memory is one page at a time. The XML decoder is used token by token; only
  `title`, `ns`, `redirect` and `text` of the current page are held.
- Non-functional: wall time under two minutes on the real dump on a laptop. Measure and
  record; see Risk.

## Architecture

```
--dump <file.xml.bz2>
  │ os.Open → io.TeeReader(f, sha256) → bufio.Reader → compress/bzip2 → xml.NewDecoder
  ▼
for each <page>:
  ns==0, no <redirect>  ─no─► count, skip
  section := vietnameseSection(text)      // dialect-aware slice of the wikitext
  section == ""          ─yes─► count "no Vietnamese section", skip
  word, syllables, reason, ok := accept(title)
  senses := definitions(section)          // []sense{pos, gloss}: stripped, ≤5, ≤200 runes each
  pos tally from section headings
  if ok: words[word] = entry{…}; meanings[word] = merge(meanings[word], senses)
```

`type sense struct { pos, gloss string }` lives in `wikitext.go`; `pos` is the mapped
Vietnamese label or empty.

Files:

```
server/cmd/build-dictionary/
  dump.go            readDump: bz2+xml streaming, page loop, provenance, dumpSourceURL const
  wikitext.go        vietnameseSection, definitions, stripWikitext, posLabels, posLabelMap
  dump_test.go       byte-exact fixtures: one legacy page, one new-dialect page, a redirect,
                     a foreign-only page, a truncated stream, a non-bzip2 file
  wikitext_test.go   table tests for the stripper and the section boundaries
```

The section-code set for legacy pages is the list of `{{-xxx-}}` codes that are *headings*
rather than language switches: `etym, pron, noun, verb, adj, adv, pr-noun, place, phrase,
idiom, prov, syn, synonym, ant, trans, ref, reference, see, der, info, num, pron, conj,
prep, intj, part, abbr, char, hanzi, nom, …`. Build it from the real dump in this phase:
extract every `{{-xxx-}}` code, list them by frequency, and classify by hand; the language
codes are 2–3 letters and the section codes are English abbreviations, so the split is
readable. Record the set in `wikitext.go` with the frequency it was seen at.

The stripper, in order:

1. Drop `<!-- … -->`, `<ref …>…</ref>`, `<ref … />`, any other tag pair or lone tag.
2. Templates by a depth counter over `{{`/`}}`. For each outermost template, split on `|`
   outside nested braces and brackets, take the name:
   - `label`, `lb`, `gloss`, `qualifier`, `q` → `(` join of positional params after a leading
     `vi` `)`;
   - `l`, `vi-l`, `w`, `m`, `link` → the last positional param (display text if any);
   - `place` → positional params after `vi`, each with a leading `[a-z]/` removed, joined by
     `, `;
   - anything else → dropped, name counted.
3. Links: `[[Thể loại:…]]`/`[[Category:…]]` dropped; `[[a|b]]` → `b`; `[[a]]` → `a`;
   external `[http… label]` → `label`.
4. `'''`, `''` removed. `&nbsp;` and the common entities decoded.
5. Control characters (`unicode.IsControl`) removed; whitespace collapsed; trimmed. A result
   that is only punctuation is empty.
6. Longer than 200 runes: cut at the last space before 200 and append `…`.

## Related Code Files

- Create: `server/cmd/build-dictionary/dump.go`, `wikitext.go`, `dump_test.go`,
  `wikitext_test.go`
- Delete: `server/cmd/build-dictionary/kaikki_list.go`, `kaikki_list_test.go`
- Modify: `server/cmd/build-dictionary/main.go` — package doc, `config.kaikki` → `dump`,
  flag text, `run()` switch, `runFromKaikkiList` → `runFromDump`, `entry` gains nothing (the
  meanings map travels beside `words` into `finish()` — phase 3 persists it),
  `builderVer = "5"`
- Modify: `server/cmd/build-dictionary/main_test.go` — `fixtureSource` writes a small bzip2
  XML dump instead of JSONL (Go has no bzip2 *writer*: commit a tiny `testdata/mini-dump.xml.bz2`
  built once with `bzip2`, plus its uncompressed source beside it for review)
- Modify: `server/cmd/build-dictionary/filter.go` — `rejectNotVietnamese` comment now refers
  to a page with no Vietnamese section; the reason string can stay

## Implementation Steps

1. Download the current dump once locally (phase 2's URL, `curl -fLR`). Write a throwaway
   `go run` that streams it and prints every `{{-xxx-}}` code with counts and every level-2
   heading text with counts. Build the section-code set and the dialect markers from that
   output; keep the numbers in comments.
2. Write `wikitext.go`: `vietnameseSection`, `posLabels`, `definitions`, `stripWikitext`.
   Table-test the stripper on the three samples in `plan.md` plus: nested templates, a
   `<ref>` mid-sentence, a link with a category, a definition that is only `{{rfdef|vi}}`
   (must be empty), a 300-rune definition (must end in `…` at a word boundary), a legacy
   page whose senses sit under `{{-noun-}}` then `{{-verb-}}` (labels `danh từ`, `động từ` in
   order), a new-dialect page with `=== {{ĐM|pr-noun}} ===` (label `danh từ riêng`), a
   definition under an unmapped heading (empty label, sense kept).
3. Write `dump.go`: the streaming loop, provenance, counters, the error shapes listed under
   Non-functional. Test against `testdata/mini-dump.xml.bz2` (six pages, both dialects, a
   redirect, an English-only page) and byte-truncated copies of it.
4. Rewire `main.go`; delete the kaikki files; make `go vet ./... && go test ./cmd/...` green.
5. Run against the real dump with `--out /tmp/dump.db`. Read the log: pages per dialect should
   be within a few percent of 35,885 / 7,128 for the 2026-09-01 dump; accepted words near
   36,200. Record wall time. Keep this database for phase 6.
6. Sample 50 senses at random from the log (or from the phase-3 table) and read them. Fix any
   stripper rule that is clearly wrong across many entries; note the rest for phase 6.

## Success Criteria

- [x] `go run ./cmd/build-dictionary --dump ../data/viwiktionary-latest-pages-articles.xml.bz2 --out /tmp/dump.db`
      completes; the log shows ~43,000 Vietnamese-section pages, both dialects non-zero,
      ~36,200 accepted words, and a definitions-kept count above 30,000.
- [x] The stripper table tests pass on all listed shapes; `# {{rfdef|vi}}` yields no sense;
      labels come out in Vietnamese for both dialects and empty for an unmapped heading.
- [x] On the real dump, senses with an empty label are under 10% of all senses, or the
      unmapped-code list has been worked through.
- [x] A truncated dump, a non-bzip2 file and a mid-page cut each fail with a message naming
      the cause; a fixture with 3 Vietnamese pages fails the 20,000-page check by name.
- [x] `grep -rn kaikki server/` finds nothing.
- [x] Wall time on the real dump recorded in the phase-6 measurement file.

## Risk Assessment

**Go's bzip2 is slow.** Signal: step 5 takes longer than two minutes. Response: keep the
reader on an `io.Reader`, add `--dump` acceptance of a plain `.xml` (sniff the two magic
bytes `BZ`), and have the Makefile pipe `bzip2 -dc` in; the Docker `dict` stage on alpine has
`bzip2`. Decide in this phase, not later.

**The section-code set is incomplete.** A legacy heading code missing from the set is read as
a language switch and truncates the Vietnamese section early: definitions after it are lost,
the word is not. Signal: the definitions-kept count is well below the page count, or the
sample in step 6 shows senses cut off. Response: the step-1 frequency list is the source of
truth; every code seen more than ~20 times must be classified.

**Definitions in a template the stripper does not know.** The whole sense is dropped. Signal:
"definitions dropped as empty" is large, or a common template name tops the dropped list.
Response: teach the stripper that template if it is a definition-shaped one (`place`,
`label` are the known cases); otherwise accept the loss and record it in phase 6.

**Both dialects on one page.** Rare; first dialect wins and the page is counted. Signal: the
counter is not small. Response: read both sections and merge senses — a small change to
`vietnameseSection` returning a slice.
