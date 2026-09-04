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
- [x] `vietnamese.Normalize(raw) (word string, syllables []string, err error)` — NFC, lowercase, whitespace collapse, syllable split (any syllable count; length rules belong to the engine)
- [x] Store resolves a player-typed variant to its canonical word via `aliases`
- [x] `Store.Resolve(word)` → canonical word + exists
- [x] `Store.WordsStartingWith(syllable)` → words (for bot move generation)
- [x] `Store.OutDegree(syllable)` → int (0 means dead end)
- [x] Store opens the DB **read-only**; concurrent-safe for many goroutines

**Non-functional**
- [x] Lookup ≤ 1ms p99 under 100 concurrent readers
- [x] Normalization identical to the builder's — shared code path, not a reimplementation (DRY)

## Architecture

`internal/vietnamese` is imported by **both** `server/cmd/build-dictionary` and the server, so the
corpus and player input can never diverge. Phase 1 writes the builder against this package;
this phase hardens and tests it.

```go
// internal/vietnamese
func Normalize(raw string) (string, []string, error)  // NFC + lower + collapse + split
const MinSyllables = 2
func HasEnoughSyllables(sylls []string) bool          // len(sylls) >= MinSyllables

// internal/dictionary — loaded fully into memory at Open; no runtime SQL.
type Store struct{ /* maps, all written once during Open */ }
func Open(path string) (*Store, error)     // file:...?mode=ro, closed before Open returns
func (s *Store) Resolve(word string) (canonical string, ok bool)
func (s *Store) LastSyllable(word string) (string, bool)
func (s *Store) WordsStartingWith(syl string) []string   // shared slice, do not modify
func (s *Store) OutDegree(syl string) (int, error)       // ErrNotFound if unknown
func (s *Store) RandomOpeningWord(minOutDegree int) (string, error)
func (s *Store) WordCount() int
func (s *Store) AliasCount() int
func (s *Store) License() string
```

**Revised during implementation — measured, not assumed.** The original design queried
SQLite per lookup and kept only `syllables` in memory. Measurement rejected that: a SQL
round-trip benchmarked at **55us** against **8.9ns** for a map hit. It would also have
threatened phase 3, where the hard bot explores hundreds of candidate moves inside a
150ms budget.

(An earlier draft justified this with a "p99 of 1.0046ms". That figure was wrong — the
Windows clock quantizes `time.Now()` to ~1ms, so it measured timer resolution, not the
code. Cite the benchmark, which does not time individual operations, and do not
reintroduce a latency test built on `time.Now()` around a nanosecond-scale call.)

The whole dictionary is now loaded at `Open` and the database closed immediately.
Measured on the real 48,216-word corpus: **~70ms load, ~7.8 MB heap**, and per operation,
all allocation-free:

| Operation | Cost | Allocations |
|---|---|---|
| `Resolve` | 8.9 ns | 0 |
| `WordsStartingWith` (full iteration) | 10.7 ns | 0 |
| `OutDegree` | 15 ns | 0 |
| `RandomOpeningWord` | 18 ns | 0 |

`Resolve` returns no error (a map lookup cannot fail) and `Close` is gone — there is
nothing left open. The result is both faster and simpler: no connection pool, no prepared
statements, no tail latency.

`WordsStartingWith` returns an `iter.Seq[string]`, not a slice. Returning the backing
slice let a caller sort, shuffle or `append` into dictionary state: verified on the real
corpus, a caller's write landed in the Store, 2,449 of 5,049 buckets had spare capacity
for `append` to scribble into, and `-race` confirmed the write/write race across
goroutines. An iterator removes the hazard structurally rather than by comment.

`Open` validates what it loaded against the builder's `meta.word_count`, cross-checks
every `syllables.out_degree` against the words actually indexed, and rejects orphan
aliases. Without that, a truncated database opens cleanly and the server starts, rejects
every word a player types, and fails every room creation.

**Resolve order:** exact hit in `words` → else `aliases` lookup → else not found.

**The engine chains on the canonical word, never on the alias the player typed.**
Canonicalization can move **either end** of the word. In the shipped dictionary roughly
half the aliases differ in the last syllable and more than a third in the first — typing
`sỹ hai` resolves to `sĩ hai`, moving the first syllable from `sỹ` to `sĩ`. An engine that
link-checked against the syllables the player typed would therefore **reject legal moves**.
`Resolve` returns the canonical form and `FirstSyllable`/`LastSyllable` report that form's
ends; phase 3 must use those, never `vietnamese.Normalize`'s split of the raw input.
All lookups are map reads; the database is closed before `Open` returns.

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

- [x] `go test ./internal/... -race` green
- [x] Composed and decomposed spellings of the same word both resolve to one canonical entry
- [x] Words of 2, 3, and 4 syllables all resolve; a 1-syllable input is rejected by `HasEnoughSyllables`
- [x] `hoà lợi`-style tone variants resolve via `aliases`
- [x] Store refuses to open a missing or writable-mode DB path with a clear error
- [x] Startup log line names the data source and license
- [x] Builder and server share one normalization implementation (no duplicate NFC/lowercase logic)

## Risk Assessment

| Risk | Signal | Response |
|---|---|---|
| `modernc.org/sqlite` read performance disappoints under load | p99 lookup > 1ms in the concurrency test | Widen the in-memory cache: load `words` into a `map[string]string` too (~60k entries, a few MB) and use SQL only for cold paths |
| Normalization drift between builder and server | A word in the DB fails to match the same string typed by a player | Prevented structurally by the shared package; a regression test asserts round-trip on a sample of real DB rows |
| `strings.Fields` mishandles an exotic Unicode space | Rare rejection reports | `strings.Fields` already splits on all `unicode.IsSpace`; test explicitly covers NBSP and tab |
