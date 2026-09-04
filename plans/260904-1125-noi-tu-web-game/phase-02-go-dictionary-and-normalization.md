---
title: "Phase 2: Go Dictionary and Normalization"
status: todo
phase: 2
priority: P1
effort: "2d"
dependencies: [1]
---

# Phase 2: Go Dictionary and Normalization

## Overview

The read-side of the dictionary: a Go package that normalizes arbitrary player input the
same way the builder normalized the corpus, and a read-only SQLite store exposing the three
lookups the game engine needs. Everything above this layer treats words as opaque strings.

## Requirements

**Functional**
- [ ] `vietnamese.Normalize(raw) (word string, syllables []string, err error)` — NFC, lowercase, whitespace collapse, syllable split (any syllable count; length rules belong to the engine)
- [ ] Store resolves a player-typed variant to its canonical word via `aliases`
- [ ] `Store.Lookup(word)` → canonical word + exists
- [ ] `Store.WordsStartingWith(syllable)` → words (for bot move generation)
- [ ] `Store.OutDegree(syllable)` → int (0 means dead end)
- [ ] Store opens the DB **read-only**; concurrent-safe for many goroutines

**Non-functional**
- [ ] Lookup ≤ 1ms p99 under 100 concurrent readers
- [ ] Normalization identical to the builder's — shared code path, not a reimplementation (DRY)

## Architecture

`internal/vietnamese` is imported by **both** `server/cmd/build-dictionary` and the server, so the
corpus and player input can never diverge. Phase 1 writes the builder against this package;
this phase hardens and tests it.

```go
// internal/vietnamese
func Normalize(raw string) (string, []string, error)  // NFC + lower + collapse + split
const MinSyllables = 2
func HasEnoughSyllables(sylls []string) bool          // len(sylls) >= MinSyllables

// internal/dictionary
type Store struct{ db *sql.DB }
func Open(path string) (*Store, error)     // file:...?mode=ro&_pragma=busy_timeout(5000)
func (s *Store) Resolve(word string) (canonical string, ok bool, err error)
func (s *Store) WordsStartingWith(syl string) ([]string, error)
func (s *Store) OutDegree(syl string) (int, error)
func (s *Store) RandomOpeningWord(minOutDegree int) (string, error)
func (s *Store) Close() error
```

**Resolve order:** exact hit in `words` → else `aliases` lookup → else not found.

**The engine chains on the canonical word, never on the alias the player typed.**
An alias can differ from its canonical in the last syllable (`chức vỵ` vs `chức vị`), so
chaining on raw input would demand a next word linking from a syllable that is not in the
dictionary. `Resolve` therefore returns the canonical form, the engine records that, and the
client displays it — the player sees their word normalized to its dictionary spelling.
One prepared statement per query, held on the `Store`; `database/sql` handles pooling.

**Hot-path caching:** `OutDegree` is read on every bot move. Load the whole `syllables`
table into a `map[string]int` at `Open` (a few tens of thousands of entries, ~1MB) and serve
from memory. `WordsStartingWith` stays on SQL — the result sets are small and per-turn.

**Opening word selection:** pick uniformly from words whose `last` syllable has
`out_degree >= minOutDegree`, so a game never dies on move one.

## Related Code Files

- Create: `server/internal/vietnamese/normalize.go`
- Create: `server/internal/vietnamese/normalize_test.go`
- Create: `server/internal/dictionary/store.go`
- Create: `server/internal/dictionary/store_test.go`
- Create: `server/internal/dictionary/testdata/mini.db` — small fixture DB built by a test helper
- Modify: `server/go.mod` — add `modernc.org/sqlite`, `golang.org/x/text`
- Modify: `server/cmd/build-dictionary/main.go` — import `internal/vietnamese` instead of local normalization

## Implementation Steps

1. Implement `Normalize`: `norm.NFC.String` → `strings.ToLower` → `strings.Fields` → rejoin; return error on empty input.
2. Point the phase-1 builder at `internal/vietnamese` so one normalizer serves both sides.
3. `dictionary.Open`: DSN with `mode=ro`, verify `meta` table exists and log `word_count` + `source_license` at startup (license visibility).
4. Prepared statements for `Resolve` (words), `Resolve` (aliases), `WordsStartingWith`.
5. Load `syllables` into an in-memory map at open; `OutDegree` reads the map.
6. `RandomOpeningWord` via `ORDER BY RANDOM() LIMIT 1` over a joined out-degree filter.
7. Test helper that builds a tiny fixture DB in `t.TempDir()` from a hardcoded word set — no dependency on the real 60k DB in unit tests.
8. Tests: NFC equivalence (composed vs decomposed `ữ`), uppercase input, tabs/NBSP/multiple spaces, 1-syllable rejected while 3- and 4-syllable accepted, alias resolution for `hoà`/`thuý`/`quí`, unknown word, dead-end syllable returns 0.
9. Concurrency test: 100 goroutines × 1000 `Resolve` calls, `-race` clean.

## Success Criteria

- [ ] `go test ./internal/... -race` green
- [ ] Composed and decomposed spellings of the same word both resolve to one canonical entry
- [ ] Words of 2, 3, and 4 syllables all resolve; a 1-syllable input is rejected by `HasEnoughSyllables`
- [ ] `hoà lợi`-style tone variants resolve via `aliases`
- [ ] Store refuses to open a missing or writable-mode DB path with a clear error
- [ ] Startup log line names the data source and license
- [ ] Builder and server share one normalization implementation (no duplicate NFC/lowercase logic)

## Risk Assessment

| Risk | Signal | Response |
|---|---|---|
| `modernc.org/sqlite` read performance disappoints under load | p99 lookup > 1ms in the concurrency test | Widen the in-memory cache: load `words` into a `map[string]string` too (~60k entries, a few MB) and use SQL only for cold paths |
| Normalization drift between builder and server | A word in the DB fails to match the same string typed by a player | Prevented structurally by the shared package; a regression test asserts round-trip on a sample of real DB rows |
| `strings.Fields` mishandles an exotic Unicode space | Rare rejection reports | `strings.Fields` already splits on all `unicode.IsSpace`; test explicitly covers NBSP and tab |
