---
phase: 4
title: "Phase 4: Meanings on the wire"
status: completed
priority: P1
effort: "3h"
dependencies: [3]
---

# Phase 4: Meanings on the wire

## Overview

Carry a word's senses in the two messages that introduce a word to the client —
`PlayedWord` and `GameStarted` — regenerate the Go and JS types and the binary fixtures,
and have the room attach the senses where it already renders those messages.

## Requirements

- Functional: `proto/noitu/v1/game.proto` gains
  ```proto
  // One definition of a word as Wiktionary gives it: the part of speech it
  // sits under, in Vietnamese ("danh từ"), empty when the heading was not one
  // the builder knows, and the definition stripped of markup at at most 200
  // characters. Plain text both: the client renders them as text, never as
  // markup.
  message Sense {
    string pos = 1;
    string gloss = 2;
  }
  message PlayedWord {
    …
    // At most five. Empty when the dictionary has no definition for the word.
    repeated Sense meanings = 7;
  }
  message GameStarted {
    …
    repeated Sense opening_meanings = 9;
  }
  ```
  (Validation session 1: `Sense` instead of a bare string, so the label is a field.)
- Functional: `wsapi.Dictionary` gains `Meanings(word string) []dictionary.Sense`.
  `game.Dictionary` does not change; the engine never reads a meaning. `convert.go` gains
  `Senses([]dictionary.Sense) []*noituv1.Sense`.
- Functional: `convert.PlayedWord` takes the senses (`PlayedWord(m game.Move, byMe bool,
  meanings []dictionary.Sense)`) and its one caller, `sendTurnUpdate` (`room.go:903`, also
  used by the resume path), passes `r.dict.Meanings(move.Word)`; `sendGameStarted` fills
  `OpeningMeanings` from `r.dict.Meanings(r.opening)`. Both paths are per recipient already;
  the lookup happens once per move, before the per-seat loop.
- Functional: the reconnect path (`GameStarted` + last `TurnUpdate`) carries meanings
  without further change; a test asserts a resumed client sees the opening's and last word's
  senses.
- Functional: `buf generate` regenerates `server/gen/` and `web/src/lib/proto/`;
  `go test ./internal/wsapi -update` regenerates `proto/testdata/`; the JS decode test reads
  the new field.
- Functional: the hand-built dictionaries in `wsapi` tests implement `Meanings`; at least
  one returns senses so the assertion is not vacuous.
- Non-functional: frame size grows by up to ~1 KB per move. Nothing in the client or server
  caps frames near that.

## Architecture

```
room.handleSubmit ── engine.Submit ──► Move ──► meanings := r.dict.Meanings(move.Word)
                                                 for each seat: PlayedWord(move, byMe, meanings)
room.sendGameStarted ─────────────────────────► OpeningMeanings: r.dict.Meanings(r.opening)
```

The bot room path renders through the same `sendTurnUpdate`, so bot moves carry meanings
without a second change.

## Related Code Files

- Modify: `proto/noitu/v1/game.proto`
- Regenerate: `server/gen/noitu/v1/game.pb.go`, `web/src/lib/proto/**`, `proto/testdata/*`
- Modify: `server/internal/wsapi/room.go` — `Dictionary` interface, `sendGameStarted`,
  the `PlayedWord(` call in `sendTurnUpdate`
- Modify: `server/internal/wsapi/convert.go` — `PlayedWord` signature, `Senses`
- Modify: `server/internal/wsapi/*_test.go` — fake dictionaries gain `Meanings`; new
  assertions on `Played.Meanings` and `OpeningMeanings`, including after a resume
- Modify: `web/tests/game-wire.test.js` — the fixture decode asserts the new fields

## Implementation Steps

1. Edit the schema; `buf lint`; `buf generate`.
2. Interface and room changes; fix compile errors in tests by adding `Meanings` to fakes.
3. Add the assertions; `go test ./internal/wsapi -update` then `go test ./... -race`.
4. `cd web && npm test` — the wire test decodes the regenerated fixtures.
5. `make proto-check` — committed generated code matches the schema.

## Success Criteria

- [x] `buf lint` clean; `make proto-check` clean.
- [x] A bot game and a room game deliver `Played.Meanings` with `Pos` and `Gloss` for a word
      the fake dictionary has senses for, and an empty list for one it does not.
- [x] A resumed session's `GameStarted.OpeningMeanings` and replayed `TurnUpdate.Played.Meanings`
      are populated.
- [x] `proto/testdata/` fixtures regenerated and the JS suite decodes them.

## Risk Assessment

**Field numbers.** `PlayedWord` uses 1–6, `GameStarted` 1–8; 7 and 9 are free and nothing
is reserved in either. Signal: `buf breaking` complaint. Response: none expected; adding a
repeated field is wire-compatible, and there is one client.

**Meanings on `PlayerEliminated.suggestions`.** Out of scope by the plan's non-goals; a
reviewer may ask. Response: the suggestions are shown to somebody who just lost the turn,
a list of words, and a meaning under each would be a second UI. Not this plan.
