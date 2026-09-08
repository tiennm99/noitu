---
phase: 3
title: "Phase 3: Meanings in the database"
status: completed
priority: P1
effort: "4h"
dependencies: [1]
---

# Phase 3: Meanings in the database

## Overview

Persist the senses phase 1 extracts in a `meanings` table, load them in `dictionary.Store`
behind a `Meanings(word)` method, and let the fixture word list carry meanings so tests and
the e2e suite have some.

## Requirements

- Functional: schema gains
  ```sql
  CREATE TABLE meanings (
    word  TEXT NOT NULL,
    ord   INTEGER NOT NULL,
    pos   TEXT NOT NULL,   -- Vietnamese part-of-speech label, '' when the heading was unmapped
    gloss TEXT NOT NULL,
    PRIMARY KEY (word, ord)
  ) WITHOUT ROWID;
  ```
  `ord` is the 0-based sense order from the page. No foreign key pragma; `verify()` checks
  the join instead, as it does for aliases. (Validation session 1: `pos` column.)
- Functional: `finish()` takes the meanings map beside `words`; `writeTo` inserts them in the
  same transaction. `meta` gains `meaning_count` (rows) and `words_with_meaning`.
- Functional: `verify()` adds: no meaning row whose word is missing from `words`; no empty
  gloss; no gloss over 200 runes; `words_with_meaning` is at least 60% of `word_count` for a
  `--dump` build (fixture builds are exempt — the check is passed a flag, or keyed on the
  source table prefix).
- Functional: `dictionary.Store` loads `meanings` into `map[string][]Sense` ordered by
  `ord`, where `type Sense struct { Pos, Gloss string }` is exported from the `dictionary`
  package, and exposes `Meanings(word string) []Sense` returning nil for a word with none.
  `Resolve` first: the caller passes a canonical word, as it does for `FirstSyllable`.
  Startup log gains the meanings count next to `words`.
- Functional: `--words` accepts an optional meaning column: `word<TAB>sense<TAB>sense…`,
  where a sense is `pos|gloss` or just `gloss` (the pipe never survives stripping, so it is a
  safe separator). Lines without a tab have no meaning. `testdata/fixture-words.txt` gains hand-written
  meanings for the words the e2e suite plays (phase 5 says which) and for at least half the
  list, so the fixture database exercises the same store paths as the real one.
- Non-functional: `Store` memory grows by the text size, a few MB. Startup time unchanged
  in practice (one more ordered scan).
- Non-functional: `builder_version = "5"` (set in phase 1; this is the contract it names).

## Architecture

```
build-dictionary                            dictionary.Store
  words   map[string]entry     ─┐             words     map[string]wordInfo
  meanings map[string][]sense  ─┼─► sqlite ─► meanings  map[string][]Sense
  aliases  map[string]string   ─┘             Meanings(word) []Sense
```

The store's `validate()` already cross-checks `word_count`; add the same for
`meaning_count` so a database whose meanings table was truncated on disk is refused at
startup rather than served silently without meanings.

## Related Code Files

- Modify: `server/cmd/build-dictionary/main.go` — `finish`, `write`, `writeTo`, `verify`,
  `runFromWordList` (tab-separated senses), `sourceSpec` unchanged
- Modify: `server/cmd/build-dictionary/main_test.go` — meanings written and verified; a
  fixture list with tabs; a `verify` failure on an orphan meaning row
- Modify: `server/internal/dictionary/store.go` — `meanings` field, `loadMeanings`,
  `Meanings()`, `validate` count check, `MeaningCount()`
- Modify: `server/internal/dictionary/store_test.go` — hand-built databases gain the table;
  `Meanings` returns ordered senses and nil; a mismatched `meaning_count` is refused
- Modify: `server/cmd/noitu-server/main.go` — startup log line
- Modify: `testdata/fixture-words.txt` — meanings column; header comment documents the format
- Modify: `web/e2e/fixture-dictionary.js` — it reads the list one word per line
  (`fixture-dictionary.js:15-19`); cut each line at the first tab before trimming, or the
  e2e graph gains words that are really `word<TAB>sense` strings (validation session 1)

## Implementation Steps

1. Schema, insert, meta and `verify` in the builder; tests first for the orphan-row and
   empty-gloss failures.
2. `runFromWordList` tab parsing; a test that a line with two tabs yields two senses in order,
   `danh từ|…` splits into label and gloss, a cell without a pipe has an empty label, and a
   line without a tab yields none. Update `fixture-dictionary.js` in the same step.
3. Store: load, method, validate; tests.
4. Fixture list: add meanings. Rebuild `data/fixture.db` (`make fixture-dict`) and start the
   server against it; the log shows the count.
5. Rebuild the real database from phase 1's dump and confirm `verify` passes the 60% rule.

## Success Criteria

- [x] `go test ./cmd/build-dictionary ./internal/dictionary -race` green.
- [x] `sqlite3 data/noitu.db 'SELECT COUNT(*) FROM meanings'` is above 40,000 and
      `words_with_meaning` above 60% of words (phase 6 records the actual).
- [x] `store.Meanings("học sinh")` on the real database returns a non-empty ordered slice
      whose first sense has `Pos == "danh từ"`; on a word with no `#` line it returns nil.
- [x] `npm run test:e2e` still builds its word graph from the fixture list correctly (no
      tab-carrying "words").
- [x] The fixture database has meanings for the e2e words and the server starts on it.

## Risk Assessment

**The 60% rule is a guess.** Research counted 5,764 of 43,013 pages with no POS marker, and
some of those have no `#` line either; the true coverage is unknown until phase 1 runs.
Signal: `verify` fails on the real dump with a coverage in the 50s. Response: measure, set
the floor a comfortable margin under the measurement, and record why in the code comment.
The rule exists to catch a stripper that suddenly returns nothing, not to demand quality.

**Fixture meanings drift from fixture words.** A word renamed in the list loses its meaning
silently. Signal: an e2e assertion on a meaning fails. Response: the builder's fixture path
logs words without meaning; keep the list short and hand-checked.
