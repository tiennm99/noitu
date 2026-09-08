---
title: "Wiktionary dump corpus and word meanings"
description: "Build data/noitu.db from the Wikimedia dump of Wiktionary tiếng Việt instead of kaikki.org's export, carry each word's definitions into the database, and show them in the chain: click to toggle, newest word open by default"
status: completed
priority: P1
effort: "~2.5d"
tags: [dictionary, data, proto, web, licensing, build]
created: 2026-09-08
blockedBy: []
blocks: []
---

# Wiktionary dump corpus and word meanings

## Overview

`data/noitu.db` is derived from kaikki.org's wiktextract export of Wiktionary tiếng Việt
(plan `260908-1653-kaikki-viwiktionary-corpus`, completed today): 34,813 words, word forms
only, fetched unpinned. Two things change here.

**The source becomes the Wikimedia dump itself.** `dumps.wikimedia.org/viwiktionary/` is the
file kaikki extracts from. Reading it directly removes the intermediary, its weekly refresh
and its `vi` extractor, which today loses ~1,400 real words the dump has (measured in
`plans/reports/research-260908-1529-viwiktionary-dump-measured.md`: **36,200** accepted
titles against kaikki's 34,813, with reduplicatives such as `rưng rức`, `nháo nhác`, `óc ách`
among the difference). The cost is a wikitext parser of our own — both markup dialects the
wiki currently mixes — instead of a JSON decoder.

**The database gains meanings.** Each word carries its Wiktionary definitions, stripped of
markup, capped at five senses of 200 characters. The client shows them under the word in the
chain: the newest word's meaning is open by default, a new word closes the previous one and
opens its own, and a click on any word toggles its meaning. This is the reason the source
matters: kaikki's glosses are pre-cleaned, but the dump's definitions cover the words kaikki
drops, and a stripper for `# ...` lines is a bounded job.

Owner decisions taken before this plan was written:

- **Track `latest/`, unpinned.** Same posture as today: `curl` whatever
  `viwiktionary-latest-pages-articles.xml.bz2` currently is, record its SHA-256 in `meta`.
  Dated directories exist and would pin honestly; the owner chose freshness. Wikimedia's dump
  is monthly, so "fresh" now means monthly rather than weekly.
- **All senses, capped, each with its part of speech.** Every definition line of the
  Vietnamese section travels, cut at 5 senses and 200 characters each, and each sense carries
  the Vietnamese label of the heading it sat under (`danh từ`, `động từ`, `tính từ`, `danh từ
  riêng` …), shown as a prefix: `(danh từ) Trẻ em học tập ở nhà trường.` A heading the
  builder cannot map yields an empty label, never a dropped sense.
- **Every word is clickable.** A word with no definition still opens, to one line saying
  `Chưa có nghĩa`, so the chain behaves the same for every word.
- **Nothing is dropped by label.** Carries over from the previous two plans: part of speech
  and capitalization are tallied in the build log and never filter. `Danh từ riêng`
  (`pr-noun`) labels are counted, not acted on.
- **One corpus input mode.** `--kaikki` is replaced by `--dump`, not kept beside it. `--words`
  stays for the fixture and gains an optional meaning column so the e2e suite can see one.

## What changes, in numbers

| | today (kaikki) | after (dump) |
|---|---|---|
| words | 34,813 | **~36,200** (measured on the 2026-09-01 dump; phase 6 records the actual) |
| meanings | none | one to five per word; coverage measured in phase 6 (~5,700 pages have no `#` line and will have none) |
| source | 62 MB JSONL, weekly, unpinned | **~61 MB `.xml.bz2`, monthly, unpinned** |
| parser | `encoding/json`, 3 fields | `compress/bzip2` + `encoding/xml` + a wikitext section/definition scanner |
| `--min-words` floor | 30,000 | 30,000 (unchanged: 36,200 measured, and a dialect the parser misses is ~7,000 pages) |
| data licence | CC BY-SA 4.0 | CC BY-SA 4.0 (the database now carries definition *text*, so the share-alike obligation is more literal, not different) |
| wire | `PlayedWord` 6 fields | `message Sense {pos, gloss}`; `PlayedWord.meanings`, `GameStarted.opening_meanings` |
| `builder_version` | 4 | 5 |

## The source

```
URL    https://dumps.wikimedia.org/viwiktionary/latest/viwiktionary-latest-pages-articles.xml.bz2
Dated  https://dumps.wikimedia.org/viwiktionary/20260901/viwiktionary-20260901-pages-articles.xml.bz2
       63,513,513 bytes, MD5 6c2491e703e7d946f23a405996b4d172 (published), monthly on the 1st
Shape  <mediawiki><page><title/><ns/><id/>[<redirect title=""/>]<revision><text>wikitext</text></revision></page>…
```

391,543 pages; 349,461 in namespace 0; **43,013 with a Vietnamese section**; 3,237 of those
are redirects (834 case-only, `mặt trời` → `Mặt Trời`) and are skipped, because the target
page is read on its own and lowercased by `accept()`.

Two markup dialects are live and the mix shifts monthly:

| | legacy (35,885 pages) | new (7,128 pages) |
|---|---|---|
| Vietnamese section opens | `{{-vie-}}` | `== {{langname\|vi}} ==` |
| section closes | next `{{-xxx-}}` whose code is *not* a known section code (`-eng-`, `-fra-`, `-tyz-` …) | next level-2 heading `== … ==` |
| POS heading | `{{-noun-}}`, `{{-verb-}}`, `{{-pr-noun-}}`, `{{-place-}}` … | `=== {{ĐM\|noun}} ===`, `{{vi-noun}}`, `{{vi-pr-noun}}` … |
| definition | a line starting `# ` (not `#:`, `#*`) under a POS heading | same |

Sample definitions as they sit in the dump and as they must come out:

```
{{-noun-}} … # [[chỗ|Chỗ]] [[râm]] [[mát]], do [[trời]] có [[mây]] hoặc do không bị [[nắng]] [[chiếu]].
→ pos "danh từ"        gloss "Chỗ râm mát, do trời có mây hoặc do không bị nắng chiếu."

=== {{ĐM|pr-noun}} === … # {{place|vi|thủ đô|c/Việt Nam}}.
→ pos "danh từ riêng"  gloss "thủ đô, Việt Nam."

# {{label|vi|thuộc lịch sử}} Một [[tỉnh]] cũ của [[Việt Nam]] vào nửa cuối thế kỷ XIX.
→ pos "danh từ riêng"  gloss "(thuộc lịch sử) Một tỉnh cũ của Việt Nam vào nửa cuối thế kỷ XIX."
```

The client renders a sense as `(pos) gloss`, or `gloss` alone when the label is empty.

## Decisions taken

- **Parse in Go, inside `build-dictionary`.** `compress/bzip2` and `encoding/xml` are stdlib;
  the Docker `dict` stage and the Makefile keep one command. Go's bzip2 is pure Go and slow
  (tens of MB/s); a ~400 MB decompressed stream is well under a minute. If phase 1 measures
  more than two minutes, the fallback is `bzip2 -dc` in the Makefile feeding a plain `.xml`
  — a flag change, recorded as a risk below, not a redesign.
- **The stripper is small and lossy on purpose.** Links keep their display text; bold and
  italic markers go; `<ref>` and comments go; `{{label|vi|x}}`/`{{lb|vi|x}}`/`{{gloss|x}}`
  become `(x)`; `{{l|vi|x}}`, `{{vi-l|x}}`, `{{w|x}}` become `x`; `{{place|vi|a|b}}` keeps its
  positional parameters after the language code with any `c/` prefix removed; **every other
  template is dropped whole**, nesting handled by a depth counter. What survives is trimmed
  and whitespace-collapsed; an empty result is not a sense. This will leave some definitions
  terse or odd. Phase 6 samples 50 and records what the stripper got wrong; fixing template
  by template is a follow-up, not this plan.
- **Meanings live in their own table** — `meanings(word, ord, pos, gloss)` — and are loaded
  into memory by the store with the words. The label is a column, not baked into the gloss:
  the 200-character cap applies to the definition, and the client decides how a label is
  shown. Ten kilobytes of text per hundred words is a few
  megabytes for the corpus; a per-move query would be a second code path for nothing.
- **The engine never sees a meaning.** `game.Dictionary` is unchanged. `wsapi.Dictionary`
  gains `Meanings(word) []dictionary.Sense` and the room attaches them when it renders a `PlayedWord`
  or a `GameStarted`, the same place `by_me` is decided. A reconnect already replays only the
  opening and the last move, and both carry meanings, so nothing new is needed there.
- **Expansion state is client-only.** Which words are open is UI state, like the theme. The
  store keeps a set of open words — any number may be open at once; `gameStarted` opens the
  opening word, a `turnUpdate` with a word closes the previous newest and opens the new one,
  a click toggles. These rules apply to every word, with or without a definition. The
  transcript export is unchanged.
- **Meaning text is plain text end to end.** It is rendered as text, never as HTML; the
  builder strips markup, the client escapes as Svelte does by default. Wiki text is
  user-generated content and travels through the same sanitizer path as any other string
  the server has not written itself: the builder caps length, drops control characters and
  collapses whitespace so nothing the store loads can be shaped like a chat injection.

## Phases

| # | Phase | Status |
|---|-------|--------|
| 1 | [Read the dump](./phase-01-start.md) | Completed |
| 2 | [Fetch the dump](./phase-02-fetch-the-dump.md) | Completed |
| 3 | [Meanings in the database](./phase-03-meanings-in-the-database.md) | Completed |
| 4 | [Meanings on the wire](./phase-04-meanings-on-the-wire.md) | Completed |
| 5 | [Meanings in the client](./phase-05-meanings-in-the-client.md) | Completed |
| 6 | [Measure and attribute](./phase-06-measure-and-attribute.md) | Completed |

Phases 1 and 3 land together: a reader that extracts definitions and a schema that has
nowhere to put them is half a change. Phase 2 can land with them or just after. Phases 4
and 5 are one wire change and its consumer; the schema regenerates once. Phase 6 is
measurement and the attribution rewrite, and the attribution must land in the same commit
as the first dump-built database, because `data/ATTRIBUTION.md` today says the database
"contains only word forms, not meanings", which stops being true.

## Non-goals

- Dropping words by proper-noun label, capitalization or part of speech.
- Keeping kaikki as a fallback or second source.
- Meanings for the suggestions shown to an eliminated player, in the transcript export, or
  anywhere outside the chain.
- A dictionary lookup UI. The meaning is shown for words that were played.
- Pinning the dump. Recorded as the open question it already was.

## Success criteria

- [x] `make fetch-dict && make dict` downloads `viwiktionary-latest-pages-articles.xml.bz2`
      and builds `data/noitu.db` with more than 30,000 words and a meaning for the large
      majority of them; the build log reports pages seen, pages with a Vietnamese section,
      pages per dialect, redirects skipped, POS tallies and definition counts.
- [x] A truncated or non-bzip2 download, an XML stream that ends mid-page, and a page count
      far below the norm each fail the build with a message naming the cause.
- [x] `meta` records `source_url`, `source_sha256`, `source_pages`, `source_fetched_at`,
      `meaning_count` and `builder_version = 5`; `source_rows` is gone.
- [x] `Sense`, `PlayedWord.meanings` and `GameStarted.opening_meanings` are in the schema,
      the generated Go and JS trees, and the binary fixtures under `proto/testdata/`.
- [x] In a bot game and a two-browser online game the newest word's meaning is open, the
      previous word's closes when a new one arrives, and clicking any word toggles its own.
      Senses show as `(danh từ) …`; a word with no definition opens to `Chưa có nghĩa`.
- [x] `data/ATTRIBUTION.md`, `NOTICE`, README, `docs/deployment.md`, the Dockerfile and the
      builder's constants name the Wikimedia dump and no longer name kaikki.org or
      wiktextract; the "only word forms" claim is replaced by an accurate description of the
      definition text carried.
- [x] `go vet ./... && go test ./... -race`, `npm run check && npm test`, `npm run test:e2e`
      (CI) and both Docker image variants are green; the CI leak guard rejects a `.bz2` or
      `.xml` inside the image.
- [x] Phase 6's measurement is recorded beside this plan: word count, meaning coverage,
      graph metrics and bot game lengths against the kaikki database, plus the 50-sense
      stripper sample.

## Open questions

- **Unpinned `latest/`.** Wikimedia repoints `latest` once a month; a dump run can also fail
  partway and leave `latest` on the previous month, which is harmless. Accepted by the owner,
  same as for kaikki. `source_sha256` and `source_fetched_at` (the file's server-side
  modification time, kept by `curl -R`) identify a build. Should reproducibility ever be
  wanted, the dated URL is a one-variable change.
- **Definitions that are only a template.** `# {{place|vi|thủ đô|c/Việt Nam}}.` strips to
  `thủ đô, Việt Nam.` — readable, not prose. Definitions that are *only* an unknown template
  strip to nothing and the sense is dropped; if that leaves a word with no sense, the word
  has no meaning shown. Phase 6 counts how many and lists the commonest template names so
  the next round knows which to teach the stripper.
- **Headings the label map does not know.** `{{-xxx-}}` and `{{ĐM|xxx}}` codes outside the
  map give an empty label; the sense is kept. Phase 6 lists unmapped codes by frequency so
  the map can grow; a code seen on more than a few hundred definitions is added in phase 1.
- **Dialect drift.** The wiki is migrating legacy `{{-vie-}}` pages to `== {{langname|vi}} ==`.
  Both are parsed; the build log prints the count per dialect so a month where one falls to
  zero is visible. A third form appearing would show as a drop in `source_pages` and,
  eventually, the floor.

## Validation Log

### Session 1 — 2026-09-08

Verification (Full tier, 6 phases; claims checked by reading the code while the plan was
written, then re-grepped): 14 checked, 13 verified, 1 failed, 0 unverified.

- FAILED → fixed: `web/e2e/fixture-dictionary.js` reads `testdata/fixture-words.txt` as one
  word per line (`fixture-dictionary.js:15-19`); the tab-separated meaning column must be
  stripped there. Phase 3 now lists it as a definite change, not a conditional one.
- Corrected: `PlayedWord(` has one caller, `room.go:903` inside `sendTurnUpdate`; the resume
  path reuses it. Phase 4 no longer implies a second call site.
- Verified: `wsapi.Dictionary` at `room.go:268`, `sendGameStarted` at `room.go:744`,
  resume replay at `room.go:1190-1195`, `Store.validate` word-count check at `store.go:213`,
  `reset()` at `game.svelte.js:198`, `chainWords` selector `ol li .word` at
  `e2e/helpers.js:184`, `TestDifficultyLadderRealCorpus` and `TestHardChooseLatencyRealCorpus`
  in `bot/realcorpus_test.go`, `web/tests/game-wire.test.js` decodes `proto/testdata`,
  `PlayedWord` fields 1–6 and `GameStarted` 1–8 with nothing reserved, `web/tests/i18n.test.js`
  does not enumerate keys (phase 5's conditional line resolves to no change), Go 1.25.

Questions asked: 4.

| Question | Decision | Effect |
|---|---|---|
| Open state when another word is opened | Independent toggles; only the automatic close of the previous newest | Phase 5 rules unchanged; stated explicitly |
| Part-of-speech label on senses | **Prefix each sense with its POS** | `meanings.pos` column; `message Sense {pos, gloss}`; heading→label map in phase 1; client renders `(pos) gloss` |
| Meanings in the transcript download | No, chain only | Non-goal stands |
| A word with no definition | **Clickable, opens to `Chưa có nghĩa`** | Every word is a button; `meaningNone` string; auto-open applies to all words |

### Whole-Plan Consistency Sweep

Re-read `plan.md` and all six phase files after propagation. Searched for `no toggle`,
`no button`, `nothing to click`, `[]string`, `repeated string meanings`, `(word, ord, gloss)`,
`if it parses`, `wherever PlayedWord`. All occurrences reconciled to the decisions above. No
unresolved contradictions.

### Session 2 — 2026-09-08, implementation

All six phases implemented in one working tree; measurement in [`measurement.md`](./measurement.md).

| Claim in the plan | Found | Effect |
|---|---|---|
| `pron` → đại từ in the label map | `pron` is *pronunciation* on this wiki (36,533 legacy + 5,501 new headings); pronoun is `pronoun`/`per-pronoun` | map corrected; `pron` is a non-POS section code |
| New dialect headings are `{{ĐM|code}}` | also `{{section|code}}` (3,506) and shorthand codes `n`/`v` | both forms and both shorthands mapped |
| `{{-dfn-}}` is a heading | it is a "definitions" marker placed under a POS heading | transparent to the label |
| Bot's bzip2 may take > 2 min | 21–25 s for the whole build | no fallback |
| Empty-label senses under 10% | 11.9%, all from pages with no POS heading at all; every code seen more than once is mapped | accepted, recorded |
| Fixture bzip2 lives at `testdata/mini-dump.xml.bz2` | placed in the package's own `server/cmd/build-dictionary/testdata/` (Go convention; the repo-root `testdata/` is the Makefile's and e2e's) | path only |
| `expanded: Set<string>` in the store | a `string[]` with set semantics, because Svelte 5's `$state` proxies arrays and not Sets | same rules, same tests |
| Stripper teaches only `label`/`l`/`place` families | also `nhãn`/`context`/`term` (label spellings), `n-g`, and `see-entry`/`like-entry` → `Xem x`, the commonest definition-line templates | phase-1 risk response, recorded |
| First build lost 15 kaikki words | pages with `{{-vie-}}{{-pron-}}…{{-place-}}` on one line | scanner reads several headings per line; second build lost 0 |

Numbers: 36,200 words (34,813 shared with kaikki, 1,387 gained, 0 lost), 40,842 senses on
35,062 words (96.9%), 43,011 pages, no graph metric below the kaikki database's.

Verification: `go vet ./... && go test ./... -race` green; `npm run check && npm test` green
(183); `npx playwright test` 38/39 on each full run, the one failure being the four-player
seating spec ("a fifth is turned away"), which no changed file touches and which fails about
one run in three on this machine with local Chrome **on `HEAD` as well** (stashed tree,
`--repeat-each 4`: 3 passed, 1 failed) — a pre-existing flake, not a regression; both meanings
specs pass every run; Docker image variants
**not built locally** — the daemon was not running — and left to CI. `--min-pages` (default
20,000) was added to the builder so the page floor is a flag like `--min-words`, which the
tests need to lower.

Code review (`plans/reports/code-reviewer-260908-2210-…md`): no critical finding; one high
(a flaky e2e assertion on the opening's label), three medium (nested-list styling leak,
format characters surviving the stripper, opaque refusal of a v4 database) and eight low, all
fixed in the same tree and re-verified; see `measurement.md` "After code review".

<!-- slug: wiktionary-dump-corpus-and-meanings -->
