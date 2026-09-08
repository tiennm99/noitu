---
phase: 1
title: "Wire contract"
status: completed
priority: P1
effort: "1h"
dependencies: []
---

# Phase 1: Wire contract

## Overview

Adds the three messages chat needs to `proto/noitu/v1/game.proto`, regenerates
both trees, and extends the cross-language fixtures so the Go and JavaScript
clients are checked against one artifact.

## Requirements

- Functional: a client can send text; the server can deliver one message to
  each recipient rendered from their side, and replay a bounded history.
- Non-functional: additive only. The current maxima are `ClientMessage` 12
  (`leave_room`, with 8 reserved) and `ServerMessage` 12 (`room_state`, with
  2, 3 and 11 reserved), so the next free tags are 13 on the client oneof and
  13 and 14 on the server oneof.

## Architecture

```proto
// SendChat is one line of text from a seated player to the other.
message SendChat {
  string text = 1;
}

// ChatMessage is one line as one recipient sees it. Rendered per recipient
// like every other room message: from_me is the only field that differs, and
// it is what lets the client style its own words without matching names.
message ChatMessage {
  bool from_me = 1;
  // Server-sanitized, as everywhere a name is shown. Empty when the author
  // has left the room: the seat they spoke from may be somebody else's now,
  // and a name outlives neither. The client labels an empty author itself.
  string author = 2;
  string text = 3;
  // Server clock. The client renders it; it never orders by its own clock.
  int64 sent_unix_ms = 4;
}

// ChatHistory is the whole panel, oldest first, sent when a player is seated
// in a room or resumes into one. A snapshot rather than a replayed stream, for
// the same reason RoomState is one: a client that missed frames is correct
// again from the next one instead of having to catch up on events.
//
// Scoped to the recipient: it carries only what was said while they held their
// seat, which is why it is built per seat rather than broadcast.
message ChatHistory {
  repeated ChatMessage messages = 1;
}
```

Wired into the oneofs as `send_chat = 13` (client) and `chat_message = 13`,
`chat_history = 14` (server).

`ProtocolVersion` does **not** change, and the honest reason is not that an
old client is unaffected — it is pushed `ChatMessage` frames it never asked
for, because delivery is unconditional and there is no capability negotiation.
It drops them silently: the generated union yields `case: undefined` and the
store's `apply` switch has no `default` arm. The degradation is one-way chat
for the length of a deploy, and it is accepted rather than unnoticed. Phase 3
adds the `default` arm so the next contract addition is at least visible in a
console. If one-way chat is ever *not* acceptable, bumping `ProtocolVersion` is
the only mechanism the codebase has, and it forces a stale tab to reload.

## Related Code Files

- Modify: `proto/noitu/v1/game.proto`
- Modify (generated, do not hand-edit): `server/gen/noitu/v1/game.pb.go`,
  `web/src/lib/proto/noitu/v1/game_pb.js`, `web/src/lib/proto/noitu/v1/game_pb.d.ts`
- Modify: `server/internal/wsapi/wire_test.go` — add `client_send_chat`,
  `server_chat_message`, `server_chat_history` variants
- Create (generated): `proto/testdata/client_send_chat.bin`,
  `proto/testdata/server_chat_message.bin`, `proto/testdata/server_chat_history.bin`
- Modify: `web/tests/game-wire.test.js`

## Implementation Steps

1. Add the three messages and their oneof arms to `game.proto`.
2. Lint and regenerate: `go run github.com/bufbuild/buf/cmd/buf@v1.58.0 lint`
   then `... generate` from the repo root (no global `buf` needed).
3. Add the three variants to `wire_test.go`. Give them Vietnamese text with
   diacritics and a real millisecond timestamp — the JavaScript runtime
   surfaces `int64` as a bigint, and a zero value would skip that. Give
   `ChatHistory` two messages: one from each side, and one with an empty
   `author`, so the repeated field, both `from_me` values and the departed-author
   case are all exercised.
4. Regenerate fixtures: `cd server && go test ./internal/wsapi -update`.
5. Assert the new fixture in `web/tests/game-wire.test.js`, including that
   `sentUnixMs` arrives as a bigint and that an empty `author` survives the
   round trip.

## Success Criteria

- [x] `buf lint` passes and both generated trees are current.
- [x] `TestVariantTablesCoverEveryOneofArm` passes, so every new arm is covered.
- [x] `go test ./internal/wsapi` and `npm test` both decode the new fixtures.
- [x] `git diff` on the generated trees shows only the new messages.

## Risk Assessment

Low. The one thing to get wrong is a tag collision with a reserved number;
`buf lint` catches it. If a later phase finds `ChatHistory` unnecessary, the
tag is reserved rather than reused — the cost of carrying it is one unused
message, not a migration.

The `author`-empty convention is load-bearing for Phase 2: if Phase 2 stores
and sends a departed player's name instead, two different people can appear
under one attribution. Signal: the Phase 4 vacate tests. Response: the
convention is stated here and tested there; keep them in step.
