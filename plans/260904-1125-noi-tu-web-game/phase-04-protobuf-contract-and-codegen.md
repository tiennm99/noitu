---
title: "Phase 4: Protobuf Contract and Codegen"
status: done
phase: 4
priority: P1
effort: "2d"
dependencies: [1]
---

# Phase 4: Protobuf Contract and Codegen

## Overview

One `.proto` file defines every message crossing the WebSocket, and `buf` generates both the
Go server types and the JavaScript client types from it. No hand-written message structs on
either side — the schema is the single source of truth for the wire contract.

Independent of phases 2-3; can be built in parallel with them.

## Requirements

**Functional**
- [x] `proto/noitu/v1/game.proto` covers handshake with nickname, create/join room, start bot game, submit word, move results, turn state, game over, errors, heartbeat
- [x] `buf generate` emits Go into `server/gen/noitu/v1` and JS into `web/src/lib/proto`
- [x] Protocol version field in the handshake — `Hello.protocol_version`, `Welcome.protocol_version`, and `wsapi.ProtocolVersion = 1`. Rejecting a mismatch lands in phase 5, which is where `Hello` is first read
- [x] Generated code is committed (reviewable diffs, no toolchain needed to build)

**Non-functional**
- [x] JS bundle cost of the protobuf runtime kept small — ESM, tree-shakeable. Measured against a budget in phase 6, where there is a bundle to measure
- [x] `buf lint` and `buf breaking` (against `main`) run in CI

## Architecture

**Tooling:** [`buf`](https://buf.build) drives both targets from one config.
- Go: `protoc-gen-go` (messages only — no gRPC, this is raw WS framing)
- JS: [`@bufbuild/protobuf`](https://github.com/bufbuild/protobuf-es) **v2.14.1** +
  `@bufbuild/protoc-gen-es` **v2.14.1** (both verified current on npm), generated with
  `target=js` plus `.d.ts`. Chosen over `protobuf.js`: it is the only fully
  conformance-compliant JS implementation, ships ESM for tree-shaking, and produces a much
  smaller browser bundle.

> `buf` **is** installed (v1.69.0), retiring the plan-time note that it was missing. It stays
> optional for contributors either way: generated code is committed, so `buf` is required only
> when the schema changes.

**Framing:** every WS frame is a binary message — `ClientMessage` client→server,
`ServerMessage` server→client, each a single `oneof`. No JSON fallback, no text frames.

```proto
syntax = "proto3";
package noitu.v1;
option go_package = "github.com/tiennm99dev/noitu/server/gen/noitu/v1;noituv1";

enum Difficulty { DIFFICULTY_UNSPECIFIED = 0; EASY = 1; MEDIUM = 2; HARD = 3; }

enum RejectReason {
  REJECT_UNSPECIFIED = 0;
  TOO_FEW_SYLLABLES = 1;   // fewer than 2 syllables
  WRONG_LINK = 2;
  NOT_IN_DICTIONARY = 3;
  ALREADY_USED = 4;
  NOT_YOUR_TURN = 5;
  TIMEOUT = 6;
}

enum GameEndReason {
  END_UNSPECIFIED = 0;
  END_TIMEOUT = 1;
  END_NO_LEGAL_MOVE = 2;
  END_OPPONENT_LEFT = 3;
  END_RESIGNED = 4;
}

message Hello        { uint32 protocol_version = 1; string resume_token = 2; string nickname = 3; }
message StartBotGame { Difficulty difficulty = 1; }
message CreateRoom   {}
message JoinRoom     { string room_code = 1; }
message SubmitWord   { string word = 1; uint32 turn_seq = 2; }  // turn_seq guards double-submits
message Resign       {}
message Ping         { int64 client_time_ms = 1; }

message ClientMessage {
  oneof payload {
    Hello hello = 1;  StartBotGame start_bot_game = 2;  CreateRoom create_room = 3;
    JoinRoom join_room = 4;  SubmitWord submit_word = 5;  Resign resign = 6;  Ping ping = 7;
  }
}

// accepted_nickname is what the server actually stored after sanitization — the client
// must display this, not the string it sent.
message Welcome      { string session_id = 1; string resume_token = 2; uint32 protocol_version = 3; string accepted_nickname = 4; }
message RoomCreated  { string room_code = 1; }
message RoomJoined   { string room_code = 1; string opponent_name = 2; }
message PlayedWord   { string word = 1; bool by_me = 2; uint32 points = 3; uint32 syllables = 4; }
message GameStarted {
  string opening_word = 1; string current_syllable = 2; bool my_turn = 3;
  int64 deadline_unix_ms = 4; uint32 turn_seq = 5; uint32 turn_limit_ms = 6;
}
message TurnUpdate {
  PlayedWord played = 1; string current_syllable = 2; bool my_turn = 3;
  int64 deadline_unix_ms = 4; uint32 turn_seq = 5;
  uint32 my_score = 6; uint32 opponent_score = 7; uint32 chain_length = 8;
}
message MoveRejected { RejectReason reason = 1; string word = 2; uint32 turn_seq = 3; }
message GameOver     { bool i_won = 1; GameEndReason reason = 2; uint32 my_score = 3; uint32 chain_length = 4; }
message OpponentLeft { bool can_reconnect = 1; uint32 grace_ms = 2; }
message ServerError  { string code = 1; string message = 2; }  // message is a UI key, not prose
message Pong         { int64 client_time_ms = 1; int64 server_time_ms = 2; }

message ServerMessage {
  oneof payload {
    Welcome welcome = 1;  RoomCreated room_created = 2;  RoomJoined room_joined = 3;
    GameStarted game_started = 4;  TurnUpdate turn_update = 5;  MoveRejected move_rejected = 6;
    GameOver game_over = 7;  OpponentLeft opponent_left = 8;  ServerError error = 9;  Pong pong = 10;
  }
}
```

**Design notes**
- `TurnUpdate` is sent to **both** players after every accepted move, with `by_me`/`my_turn`
  rendered per recipient — the server serializes one message per player, not a broadcast.
- `deadline_unix_ms` is an absolute server timestamp; the client renders a countdown from it.
  `Ping`/`Pong` carry both clocks so the client can correct for offset. Never trust the client clock.
- `turn_seq` on `SubmitWord` makes double-submits and late submissions detectable server-side.
- `ServerError.message` carries a **UI key** (e.g. `room_not_found`), not user-facing prose —
  the Vietnamese strings live in the frontend so all copy stays in one place.
- `Hello.nickname` is a **request**, not a fact. The server sanitizes it (phase 5) and returns
  the stored value in `Welcome.accepted_nickname`; `RoomJoined.opponent_name` is always a
  server-sanitized string. A client must never render another player's raw input.
- The engine's `RejectReason` and this proto enum are deliberately separate types with an
  explicit mapping function; the wire contract must not be hostage to internal refactors.

## Related Code Files

- Create: `proto/noitu/v1/game.proto`
- Create: `buf.yaml`, `buf.gen.yaml`, `buf.lock`
- Create: `server/gen/noitu/v1/game.pb.go` (generated, committed)
- Create: `web/src/lib/proto/game_pb.js` (generated, committed)
- Create: `server/internal/wsapi/convert.go` — engine ↔ proto enum mapping + tests
- Modify: `Makefile` — `make proto`
- Modify: `server/go.mod` — `google.golang.org/protobuf`
- Modify: `web/package.json` — `@bufbuild/protobuf`, dev dep `@bufbuild/protoc-gen-es`

## Implementation Steps

1. `buf.yaml` (module + lint/breaking config) and `buf.gen.yaml` with the two plugins and their output dirs.
2. Write `game.proto` as above; `buf lint` clean.
3. `make proto` → `buf generate`; commit both generated trees.
4. `convert.go`: `game.RejectReason` → `noituv1.RejectReason`, engine end reasons → `GameEndReason`. Exhaustive `switch` with a default that returns the UNSPECIFIED value **and** logs — a silently-dropped new reason is a bug.
5. Round-trip test in Go: marshal each `ServerMessage` variant, unmarshal, assert equality.
6. Round-trip test in JS (Vitest): decode a fixture emitted by the Go test, assert field values — proves cross-language wire compatibility, not just self-consistency.
7. CI step: `buf lint`, `buf breaking --against '.git#branch=main'`, and a check that regenerating produces no diff (generated code stays in sync).

## Success Criteria

- [x] `make proto` regenerates both targets with zero diff on a clean tree
- [x] `buf lint` clean; `buf breaking` wired into CI
- [x] Go round-trip test covers every `oneof` variant in both directions
- [x] JS decodes a Go-produced binary fixture and reads correct values
- [x] `convert.go` mapping is exhaustive; a test fails if an engine reason gains a value with no proto counterpart
- [x] No hand-written message struct exists in `server/` or `web/`

## Risk Assessment

| Risk | Signal | Response |
|---|---|---|
| Schema churn breaks an already-deployed client | Decode errors after a deploy | Additive-only changes, never reuse a tag, `reserved` on removals; `protocol_version` in `Hello` lets the server reject incompatible clients with a clear message instead of failing obscurely |
| `buf` unavailable in a contributor's environment | `make proto` fails locally | Generated code is committed, so building and running never requires `buf` — only changing the schema does |
| Protobuf runtime inflates the JS bundle | Bundle budget exceeded in phase 6 | `protobuf-es` is ESM/tree-shakeable and only the generated messages are imported; measure in phase 6 and drop to hand-rolled binary framing only if it genuinely fails the budget |
| Enum drift between engine and wire | A new reject reason silently arrives as UNSPECIFIED | Exhaustive-switch test plus a logged default in `convert.go` |


## Phase 4 Outcome (2026-09-04)

Delivered and verified end to end. `buf lint` clean under the `STANDARD` category with no
carve-outs, regeneration byte-identical, `go test ./... -race` green across every package,
and 23 JavaScript assertions decoding bytes the Go suite produced.

**Two deviations from the sketch above, both accepted before implementation**

| Sketch | Shipped | Why |
|---|---|---|
| `TOO_FEW_SYLLABLES`, `EASY`, `END_TIMEOUT` | `REJECT_REASON_TOO_FEW_SYLLABLES`, `DIFFICULTY_EASY`, `GAME_END_REASON_TIMEOUT` | buf's `STANDARD` lint mandates the enum-name prefix. Keeping the short names would have meant an `ENUM_VALUE_PREFIX` exception, weakening "buf lint clean" to "clean with a carve-out". `protobuf-es` strips the prefix, so the JS side still reads `RejectReason.WRONG_LINK` |
| `web/` untouched until phase 6 | `web/` bootstrapped as a bare npm package | The phase's own JS codegen and Vitest cross-language test have nowhere to live otherwise. Protobuf + Vitest only — no SvelteKit, no routes, no config |

**Additions beyond the sketch**

- `PlayedWord.typed` (field 5). `game.Move` carries both the canonical spelling and the raw
  input, and the UI has to show that a correction happened rather than appearing to rewrite
  the player's text.
- `REJECT_REASON_GAME_OVER` (7). `game.ReasonGameOver` exists in the engine; without a wire
  counterpart the exhaustiveness test fails, which is the test working.
- `wsapi.ProtocolVersion = 1`, giving `protocol_version` a defined value rather than a field
  nobody sets.

**Toolchain**

Both code generators resolve from the repo's own manifests — a `tool` directive in
`server/go.mod` and `web/package.json` — so regenerating needs only `buf`, with no globally
installed plugin binaries. `buf.gen.yaml` sets `clean: true`, so the JavaScript wire test
lives in `web/tests/`, outside the generated tree that gets wiped on each run.

**Cross-language checking**

`server/internal/wsapi/wire_test.go` writes 17 binary fixtures to `proto/testdata/` under
`-update`; both suites then read those same bytes. A JS-only round trip would only have
proven that runtime self-consistent, which is exactly the failure that ships a broken client.
`.gitattributes` marks the fixtures `binary`: nine of them contain `0x0a` and no NUL, so a
checkout with `autocrlf` enabled could otherwise rewrite them into messages that no longer
decode.

**Review found four guards that reported green while protecting nothing.** All fixed and
then negative-tested by deliberately breaking each one:

| Defect | Fix |
|---|---|
| `buf breaking --against '.git#branch=main'` fails on every PR — a `pull_request` checkout is a detached HEAD with no local `main` | `.git#ref=origin/main` |
| `git diff --exit-code` never reports untracked files, so a plugin emitting a *new* file would pass the sync check | `git add --intent-to-add` first, in both CI and `make proto-check` |
| The oneof-coverage test compared the schema against a hand-written list of arm names, which drifts in lockstep with the table it duplicates | Coverage now derived from what each sample message actually sets, via `WhichOneof` |
| The exhaustiveness walk ended at `String() == "unknown"`, so a reason added without a `String` case shrank the range under test and still passed | `game.NumRejectReasons` / `game.NumEndReasons` sentinels, plus a test that every reason is named |

`WireDifficulty` was also removed: it had no caller and no message in the schema carries a
`Difficulty`, so its doc comment described a capability the contract cannot express.

**Deferred to phase 5**, where `Hello` is first read: rejecting a mismatched
`protocol_version`, and nickname sanitization behind `Welcome.accepted_nickname`.
