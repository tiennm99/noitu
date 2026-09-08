---
phase: 3
title: "Phase 3: Measure the corpus"
status: completed
priority: P1
effort: "1h"
dependencies: [1, 2]
---

# Phase 3: Measure the corpus

## Overview

Record what the first kaikki build contains against today's database: the graph numbers, the
words gained and lost, the POS mix, and bot-versus-bot behaviour. Measurement, not a gate —
no rule removes words any more, so there is nothing to audit by hand.

## Requirements

- Functional: a note in this plan directory with the numbers below and the commands that
  produced them, plus the `source_sha256` of the file measured so the note is tied to bytes.
- Functional: a diff against the current `data/noitu.db` (copied aside before Phase 2
  overwrites it): shared, gained, lost, with 25 random samples of each.
- Functional: playability on both databases: words, syllables, openers, openers with ≥2
  continuations, dead ends, and the `internal/bot` real-corpus test output.
- Non-functional: every number reproducible from a written command.

## Architecture

Pre-measured this session on kaikki's 2026-09-06 file (sha256 `51ddc2fbd73cd7e2…`); the
phase re-measures on whatever the build fetched and explains any delta:

| | today | kaikki-vi (2026-09-06) |
|---|---|---|
| words | 26,845 | 34,813 |
| syllables | 5,709 | 6,081 |
| openers ≥2 continuations | 2,787 | 3,163 |
| dead-end syllables | 1,551 | 1,594 |
| shared / gained / lost | | 25,392 / 9,421 / 1,453 |

POS of the 41,507 distinct words: noun 16,219 · verb 9,355 · adj 6,698 · name 5,180 ·
unknown 4,652 · adv 1,024 · phrase 392 · proverb 322 · character 198. `name` and `unknown`
are kept; the log prints the tally so a future decision has the numbers.

## Related Code Files

- Create: `plans/260908-1653-kaikki-viwiktionary-corpus/measurement.md`
- Read only: the pre-switch `data/noitu.db` copy, the new build, `server/internal/bot`
  real-corpus tests

## Implementation Steps

1. Before Phase 2 overwrites it, copy today's `data/noitu.db` to a scratch path.
2. After the first `make dict`, record `meta.source_sha256`, `source_rows`,
   `source_fetched_at` and the build log's reject and POS lines.
3. Compute the diff and the five graph numbers for both databases (sqlite, one script; keep
   the script text in the note).
4. Run `go test ./internal/bot/ -run RealCorpus -v` against both databases (swap the file
   under `data/noitu.db`, restore afterwards) and record the game-length and win-rate lines.
5. Look at the 1,453 lost words: they are words in the 2018 scrape that Wiktionary has since
   deleted or renamed. Sample 25, note what they look like (expected: misspellings, moved
   pages, deleted junk). No action unless the sample is mostly real vocabulary.

## Success Criteria

- [x] `measurement.md` exists with the tables, samples, bot lines, commands and the source
      SHA-256.
- [x] Words > 30,000; syllables, openers and ≥2-continuation counts all above today's.
- [x] Bot real-corpus tests pass on the new database; easy-vs-easy game length is not
      shorter than today's 12.9 moves.
- [x] The lost-word sample is characterised in one paragraph.

## Risk Assessment

**The fetched file differs from the one measured today.** Certain over time. Signal: counts
off from the table above. Response: record the new numbers and the SHA-256; the deltas are
the point of the note, not a failure.

**Dead ends rise slightly (1,551 → 1,594).** More words bring more rare final syllables.
Signal: already known. Response: the ratio of dead ends to syllables falls (27.2% → 26.2%),
so the graph is denser, not sparser; record it and move on.
