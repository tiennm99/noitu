---
title: "Phase 3: Go Game Engine and Bot AI"
status: todo
phase: 3
priority: P1
effort: "4d"
dependencies: [2]
---

# Phase 3: Go Game Engine and Bot AI

## Overview

Pure, transport-free game logic: rule validation, per-game state, win/loss resolution, and
the bot's move selection at three difficulties. No WebSocket, no protobuf, no timers driven
by wall clock — the engine takes a deadline as data so it stays fully unit-testable.

## Requirements

**Functional**
- [ ] `Engine.Submit(playerID, raw)` validates: at least 2 syllables → first syllable matches current → word exists → not already used
- [ ] Distinct rejection reasons, not a boolean
- [ ] Used-word set per game; a word is spent for both players
- [ ] Detects `no legal move remains` for the player to act
- [ ] Turn deadline held as `time.Time`, enforced by the caller; engine exposes `IsExpired(now)`
- [ ] Bot difficulties: Easy (random), Medium (prefer low out-degree), Hard (dead-end win + depth-limited search)
- [ ] Score model: points per accepted word, chain length tracked

**Non-functional**
- [ ] Zero dependency on `wsapi` or generated protobuf types — engine speaks its own Go types
- [ ] Hard bot move decision ≤ 150ms
- [ ] Engine is safe under a single owning goroutine per game (documented; the room owns it)

## Architecture

The game is a **directed graph**: nodes = syllables, edges = words (`pháp ──"pháp luật"──► luật`).
A move traverses an unused edge from the current node. Formally this is *Directed Edge
Geography*, which is PSPACE-complete — so no perfect solver is attempted; the Hard bot uses
an instant-win check plus bounded search. Words of 3+ syllables are edges like any other —
they simply span extra syllables between `first` and `last`, so nothing in the graph model
changes.

```go
type RejectReason int
const (
    ReasonNone RejectReason = iota
    ReasonTooFewSyllables   // fewer than vietnamese.MinSyllables (2)
    ReasonWrongLink
    ReasonNotInDictionary
    ReasonAlreadyUsed
    ReasonNotYourTurn
    ReasonTimeout
)

type Engine struct {
    dict     *dictionary.Store
    used     map[string]struct{}
    current  string        // syllable the next word must start with
    turn     PlayerID
    deadline time.Time
    history  []Move
    scores   map[PlayerID]int
}

func New(dict *dictionary.Store, players []PlayerID, opening string, turnLimit time.Duration) (*Engine, error)
func (e *Engine) Submit(p PlayerID, raw string) (Move, RejectReason)
func (e *Engine) LegalMoves() ([]string, error)     // for the player to act
func (e *Engine) HasLegalMove() (bool, error)
func (e *Engine) IsExpired(now time.Time) bool
func (e *Engine) Snapshot() State
```

**Validation order matters** — check cheapest and most-informative first so the player gets
the most useful message: turn → syllable count (≥2) → link → dictionary → reuse.

**Scoring and word length:** longer words are worth more, so allowing 3+ syllables adds a
reason to reach for them rather than being merely permissive. See the scoring formula below.

**Bot strategies** (`internal/bot`), all implementing one interface:

```go
type Strategy interface{ Choose(e *game.Engine) (string, error) }
```

| Difficulty | Behaviour |
|---|---|
| **Easy** | Uniform random legal move. Adds a 400-900ms simulated "thinking" pause so it feels human |
| **Medium** | Score each legal move by the out-degree of the syllable it hands the opponent; pick randomly among the lowest quartile. Never plays a guaranteed kill |
| **Hard** | 1) if any legal move lands on a syllable with 0 remaining continuations → play it (instant win). 2) else negamax with alpha-beta, depth 4, over the remaining edge set; eval = `-log(1 + opponent legal move count)`. 3) else fall back to Medium |

Search cost is bounded by move ordering (ascending opponent out-degree first) and a node
cap; the branching factor is the out-degree of visited syllables, typically < 50.

**Anti-frustration:** Hard plays the instant-win move only `hardKillRate` of the time
(a tunable constant, default 0.85). A bot that always wins is not a game.

**Scoring:** `10 + 2 × chainLength + 5 × (syllables − 2)` per accepted word, capped; final
score reported at game over. The syllable term rewards longer compounds now that they are
legal. Client persists a personal best in `localStorage` (phase 6) — no server storage.

## Related Code Files

- Create: `server/internal/game/engine.go`
- Create: `server/internal/game/state.go` — `Move`, `State`, `PlayerID`, `RejectReason`
- Create: `server/internal/game/engine_test.go`
- Create: `server/internal/bot/bot.go` — `Strategy` interface, difficulty registry, thinking delay
- Create: `server/internal/bot/strategy_easy.go`
- Create: `server/internal/bot/strategy_medium.go`
- Create: `server/internal/bot/strategy_hard.go`
- Create: `server/internal/bot/bot_test.go`
- Create: `server/internal/bot/simulate_test.go` — 100-game difficulty ladder assertion

## Implementation Steps

1. `state.go`: `PlayerID`, `Move{Player, Word, First, Last, Syllables, Points, At}`, `RejectReason` with `String()`, `State` snapshot struct.
2. `engine.New`: seed `used` with the opening word, set `current` to its last syllable, assign first turn.
3. `Submit`: normalize via `vietnamese.Normalize`, then run the validation order above; on success record the move, add to `used`, advance `current` and `turn`, reset deadline, add points.
4. `LegalMoves`: `dict.WordsStartingWith(current)` minus `used`. `HasLegalMove` short-circuits on the first hit rather than materializing the slice.
5. Engine unit tests, one per rejection reason, plus: reuse blocked across both players, chain advances correctly, no-legal-move detection, `IsExpired` boundary.
6. `bot.Strategy` interface + registry keyed by difficulty; shared randomized thinking delay helper.
7. Easy: uniform pick. Medium: out-degree-of-result scoring, lowest-quartile random pick, explicit guard that skips a 0-out-degree kill.
8. Hard: instant-win scan gated by `hardKillRate`; negamax + alpha-beta depth 4 with move ordering and node cap; Medium fallback.
9. `simulate_test.go`: 100 headless bot-vs-bot games per pairing; assert Hard beats Easy well above chance and Medium sits between. Deterministic via a seeded RNG.
10. Benchmark Hard's `Choose` to confirm ≤150ms on the real DB.

## Success Criteria

- [ ] `go test ./internal/game/... ./internal/bot/... -race` green
- [ ] Every `RejectReason` has a test that produces exactly it
- [ ] A 1-syllable submission yields `ReasonTooFewSyllables`; 3- and 4-syllable words are accepted and score the syllable bonus
- [ ] A word played by either player cannot be replayed by the other
- [ ] `HasLegalMove` returns false on a synthetic dead-end board and the engine reports the correct winner
- [ ] Hard win rate vs Easy > 70% over 100 seeded games; Medium strictly between Easy and Hard
- [ ] `BenchmarkHardChoose` ≤ 150ms/op against the real `noitu.db`
- [ ] Engine package imports no transport or protobuf package (verified by an import assertion test)

## Risk Assessment

| Risk | Signal | Response |
|---|---|---|
| Depth-4 search too slow on hub syllables (high out-degree) | Benchmark exceeds 150ms | Node cap already present; reduce to depth 3 for syllables with out-degree > 200 — the depth is a tunable constant |
| Hard bot feels unbeatable | Playtest win rate ≈ 0% | Lower `hardKillRate`; it exists precisely for this dial |
| Bot picks obscure words that feel unfair | Playtest complaints | Out of scope for v1 (no frequency data in the derived DB); note as a post-v1 item requiring a frequency column |
| Engine mutated from two goroutines | `-race` failures once rooms land in phase 5 | Ownership rule documented here and enforced in phase 5: exactly one goroutine per room owns its engine |
