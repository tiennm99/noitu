# Phase 1 — Engine: elimination and standings

`server/internal/game/engine.go`, `state.go`.

The engine already carries `players []PlayerID` and advances `turnIndex` modulo
its length, so seating more than two costs nothing. What is two-player is the
*ending*: `opponentOf` picks the winner, and any single failure finishes the
game.

## What changes

- `alive []bool` parallel to `players`, plus `aliveCount`.
- `outOrder []PlayerID` — elimination order, first out first. Rank is derived
  from it, so nothing has to be recomputed on read.
- `outReason map[PlayerID]EndReason` — why each player went out.
- `Turn()` returns the next **living** player; `advance()` skips the dead.
- `eliminate(p, reason)` marks a player out, appends to `outOrder`, and finishes
  the game when one player is left.
- `expire(now)` eliminates the player to act, then keeps eliminating the next to
  act while the position has no legal move — which is what stops a dead end from
  costing every remaining player a full turn clock each.
- `NoMove()` and `Resign(p)` route through `eliminate` too. `Resign` works out
  of turn: a player may give up while somebody else is thinking.
- `Alive(p) bool` and `Standings() []Standing` for the transport layer.
- Delete `opponentOf`.

```go
// Standing is one player's final placing. Rank 1 is the winner; a player
// eliminated later ranks above one eliminated earlier.
type Standing struct {
    Player PlayerID
    Score  int
    Rank   int
    Reason EndReason // EndNone for the winner
}
```

`State` gains `Standings []Standing` and `Alive map[PlayerID]bool`.

## Invariants worth a test

- Two players: every existing outcome is identical to today's.
- Four players: A times out → A out, turn passes to B on the **same syllable**,
  scores and used set survive.
- A dead end closed by A eliminates B, C and D in one call and leaves A standing.
- `Resign` out of turn eliminates the resigner and does not disturb whose turn
  it is, unless the resigner *was* the player to act.
- Rank ordering: survivor 1, then reverse `outOrder`.
