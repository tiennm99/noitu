---
title: Built the dictionary from the Wiktionary dump and gave every word its meaning
date: 2026-09-08
summary: "Replaced kaikki with the Wikimedia viwiktionary dump, added a meanings table, Sense on the wire and a toggling meanings panel in the chain; 36,200 words, 96.9% with a meaning, kaikki a strict subset"
---

# Built the dictionary from the Wiktionary dump and gave every word its meaning

## What happened

Executed all six phases of `plans/260908-2056-wiktionary-dump-corpus-and-meanings` in one
working tree (uncommitted, awaiting the owner's go-ahead).

- **Reader.** `server/cmd/build-dictionary/dump.go` streams the bzip2 XML one page at a
  time; `wikitext.go` finds the Vietnamese section in both markup dialects and turns `#`
  lines into plain-text senses. Whole build on the real dump: 21–32 s. Go's bzip2 was never
  a problem; the plan's `bzip2 -dc` fallback was not needed.
- **What the dump taught us, against the plan.** `{{-pron-}}` is pronunciation, not pronoun
  (36k headings would have been mislabelled). The new dialect also writes headings as
  `{{section|code}}` with `n`/`v` shorthands. `{{-dfn-}}` sits under a POS heading and is
  transparent. Fifteen kaikki words were lost by the first build because a handful of pages
  put `{{-vie-}}{{-pron-}}…{{-place-}}` on one line; the scanner now reads several headings
  per line and the loss is zero.
- **Database.** `meanings(word, ord, pos, gloss)`, `meaning_count` and `words_with_meaning`
  in meta, `builder_version` 5. The store loads senses into memory, exposes `Meanings()`
  and refuses a v4 database with a message that says to rebuild.
- **Wire and client.** `message Sense {pos, gloss}`, `PlayedWord.meanings` (7),
  `GameStarted.opening_meanings` (9). The chain renders every word as a button; the newest
  word's panel is open, a new word closes the previous newest, a click toggles any word,
  `Chưa có nghĩa` for a word without one. Open state is a `string[]` with set semantics
  because Svelte 5's `$state` proxies arrays and not Sets.
- **Numbers** (`measurement.md`): 36,200 words vs kaikki's 34,813 — 34,813 shared, 1,387
  gained, 0 lost; 40,842 senses on 35,062 words (96.9%); 11.9% of senses carry no label,
  all from pages with no POS heading at all; no graph metric regressed; bot ladder holds.
  50-sense hand sample: 43 fine, 5 terse, 2 wrong.
- **Attribution** rewritten: the database now redistributes edited excerpts of the entries'
  text, so the modification record is load-bearing; kaikki/wiktextract gone from every
  surface outside `plans/`.

## Review and what it caught

`code-reviewer` found no critical issue and four real ones, all fixed: the new bot-game
e2e assertion assumed the opening was a `danh từ` (four of the ten fixture openers are
`động từ`, ~40% flake) — it now reads the drawn word's sense from the fixture list; the
chain-row CSS leaked onto the nested `<ol class="meanings">` (each sense a bordered card,
no numbering) — row rules are scoped to `.rows > li`; the stripper dropped only `Cc`, so
bidi overrides and zero-width spaces survived — `Cf` is dropped too; the v4-database
refusal named a missing row instead of the fix. Eight low items also fixed, including a
tally of the codes that end a legacy section, which is the plan's own top risk made
visible (it lists only language codes).

## Verification

`go vet && go test ./... -race` green; `npm run check && npm test` green (183);
`npx playwright test` 38/39 on every full run — the failure is the pre-existing four-player
seating spec, shown to flake 1-in-4 on `HEAD` too (stashed tree). Both meanings specs pass
every run. Docker image variants were **not** built locally: the daemon was not running.
Left to CI.

## Environment friction

No `make` on this machine; Playwright's bundled Chromium was a version behind
(`PLAYWRIGHT_CHANNEL=chrome` works); Docker Desktop off. Saved to memory so the next
session skips the discovery.

## Next steps

- Owner decides on the commit (`data/ATTRIBUTION.md` must land with the first dump-built
  database; it is in the same tree).
- CI proves the two Docker variants and the `.bz2`/`.xml` leak guard.
- Follow-up worth a plan of its own: teach the stripper the `*form of` /
  `*alternative spelling of` template family — 1,138 words still have no meaning, and
  `hóa thạch` is one of them.
- The four-player e2e flake is a room-join timing issue independent of this work.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
