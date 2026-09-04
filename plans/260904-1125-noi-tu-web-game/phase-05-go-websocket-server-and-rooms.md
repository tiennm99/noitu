---
title: "Phase 5: Go WebSocket Server and Rooms"
status: todo
phase: 5
priority: P1
effort: "4d"
dependencies: [3, 4]
---

# Phase 5: Go WebSocket Server and Rooms

## Overview

The transport and orchestration layer: a WebSocket endpoint that speaks Protobuf frames,
a hub owning all live rooms, per-room goroutines driving the engine and turn timers, and
the bot wired in as a virtual player. After this phase the whole game is playable over
`websocat` with no frontend.

## Requirements

**Functional**
- [ ] `GET /ws` upgrades and runs the session loop; `GET /healthz` for liveness
- [ ] Bot rooms (1 human + bot) and PvP rooms (2 humans, joined by 6-char room code)
- [ ] Nicknames sanitized server-side and echoed back; opponents only ever see sanitized values
- [ ] Server-authoritative 20s turn timer (`NOITU_TURN_LIMIT`); expiry ends the game for the player on turn
- [ ] Every accepted move fans out a per-recipient `TurnUpdate`
- [ ] Bot moves scheduled on the room goroutine after a randomized thinking delay
- [ ] Session resume token issued in `Welcome`, honoured on reconnect within a grace window
- [ ] Serves the built frontend as static files so one binary is the whole deployment

**Non-functional**
- [ ] One goroutine owns each room's engine — no shared mutable game state (`-race` clean)
- [ ] Per-connection read limit, read/write deadlines, and rate limiting on `SubmitWord`
- [ ] Graceful shutdown drains rooms and tells clients why

## Architecture

```
             ┌──────── hub (mutex-guarded maps) ────────┐
 conn ──► session ──chan──► room goroutine ──► game.Engine
             │                    │                └► bot.Strategy
             │                    └── time.Timer (turn deadline)
             └── writer goroutine (owns the socket write side)
```

**Concurrency contract — the core invariant of this phase:**
- Each connection has exactly **one reader goroutine** and **one writer goroutine**. Nothing
  else touches the socket. (`coder/websocket` tolerates concurrent writes, but a single
  writer keeps ordering deterministic.)
- Each room has exactly **one goroutine** that owns its `game.Engine`. All input arrives as
  messages on the room's channel. The engine is never locked because it is never shared.
- The hub owns only the `code → *room` and `sessionID → *session` maps, guarded by a mutex.
  It never touches engine state.

**Room lifecycle**

```
CreateRoom  → generate code (6 chars, unambiguous alphabet: no 0/O/1/I) → room waits
JoinRoom    → second player attaches → engine created → GameStarted to both
StartBotGame→ room created immediately with a bot as player 2 → GameStarted
in play     → SubmitWord | timer fire | disconnect
GameOver    → both notified → room lingers briefly for a rematch, then hub evicts it
```

**Turn timer:** the room goroutine holds a `time.Timer` for the current deadline, selected
on alongside the input channel. On fire: mark the player on turn as the loser
(`END_TIMEOUT`) and broadcast `GameOver`. The absolute deadline also goes to clients in
`TurnUpdate.deadline_unix_ms` — clients render a countdown but never decide the outcome.

**Bot integration:** the bot is a player ID with no connection. When it is the bot's turn,
the room schedules `Choose` on a worker and delivers the result back through the same input
channel as a human move, so it flows through identical validation. If the bot has no legal
move, the human wins.

**Reconnect:** `Welcome` carries a `resume_token`. On disconnect, the room keeps the seat
open for `graceMs` (default 30s) and sends the opponent `OpponentLeft{can_reconnect}`. A
`Hello` carrying a valid token rebinds the seat and replays current state via `GameStarted`
+ the latest `TurnUpdate`. Timeout ends the game as `END_OPPONENT_LEFT`.

**Security / abuse limits**
- `conn.SetReadLimit(4096)` — no legitimate message approaches this (verified API: `SetReadLimit(n int64)`)
- Read deadlines come from `context.WithTimeout` passed to `conn.Read` — `coder/websocket`
  has **no** `SetReadDeadline`; everything is context-driven. Ping via `conn.Ping(ctx)` every
  20s, treating a ctx timeout as a miss and closing after 2
- `SubmitWord` rate-limited to ~5/sec per session (a submit is one dictionary lookup)
- Room codes from `crypto/rand`; join attempts rate-limited to make brute-forcing pointless
- Origin check via `websocket.AcceptOptions.OriginPatterns` from `NOITU_ALLOWED_ORIGINS`;
  never set `InsecureSkipVerify` outside local dev
- **Nickname sanitization** (`sanitizeNickname`): NFC normalize, strip control characters and
  zero-width joiners, collapse whitespace, trim, cap at 20 runes, reject empty → assign
  `Người chơi N`. Nicknames are shown to strangers, so this is a real input-validation
  boundary, not cosmetic. A denylist hook is left in place for post-v1 if abuse appears
- Every rejection returns a UI key, never a raw internal error string

## Related Code Files

- Create: `server/cmd/noitu-server/main.go` — flags/env, dict open, hub, HTTP mux, graceful shutdown
- Create: `server/internal/wsapi/server.go` — upgrade handler, origin check, static file serving
- Create: `server/internal/wsapi/session.go` — reader/writer goroutines, codec, resume tokens
- Create: `server/internal/wsapi/hub.go` — room + session registries, room code generation
- Create: `server/internal/wsapi/room.go` — room goroutine, timer, bot scheduling, fan-out
- Create: `server/internal/wsapi/codec.go` — protobuf marshal/unmarshal over binary frames
- Create: `server/internal/wsapi/ratelimit.go`
- Create: `server/internal/wsapi/nickname.go` — `sanitizeNickname` + tests
- Create: `server/internal/wsapi/room_test.go`, `session_test.go`, `hub_test.go`
- Modify: `server/go.mod` — `github.com/coder/websocket`
- Modify: `Makefile` — `make server`, `make run`

## Implementation Steps

1. `main.go`: config from env (`NOITU_ADDR`, `NOITU_DB_PATH`, `NOITU_TURN_LIMIT` default `20s`, `NOITU_ALLOWED_ORIGINS`, `NOITU_WEB_DIR`), open the dictionary read-only, construct hub, mount `/ws`, `/healthz`, and static assets, `signal.NotifyContext` shutdown. **Log the dictionary's `WordCount()`, `AliasCount()` and `License()` at startup** — the store deliberately does not log (a library writing to the global logger fights phase 5's structured logging), so the CC BY-SA attribution surfaces here or nowhere.
2. `codec.go`: `Encode(*ServerMessage) []byte` / `Decode([]byte) (*ClientMessage, error)`; binary message type only, reject text frames.
3. `session.go`: reader goroutine (read limit, per-read `context.WithTimeout`, decode, forward to the room or hub) and writer goroutine (buffered channel, single owner of writes). Generate `session_id` and `resume_token` with `crypto/rand`; sanitize `Hello.nickname`; send `Welcome` carrying `accepted_nickname`; reject mismatched `protocol_version`.
4. `hub.go`: registries + mutex; `CreateBotRoom`, `CreateRoom`, `JoinRoom(code)`, `ResumeSession(token)`, eviction of finished/idle rooms via a janitor ticker.
5. `room.go`: the goroutine — `select` over input channel, turn timer, and context done. Handle `SubmitWord` (validate turn_seq, call engine, fan out per-recipient `TurnUpdate` or `MoveRejected`), `Resign`, disconnect, timer fire, bot scheduling, and no-legal-move detection after every move.
6. Per-recipient rendering: build `TurnUpdate` twice, flipping `by_me`/`my_turn` and swapping scores. Do not broadcast one shared message.
7. Bot wiring: on bot turn, run `Choose` on a worker goroutine with the room's context, deliver via the input channel after the thinking delay; cancel cleanly if the room ends first.
8. Reconnect: grace timer per seat, `OpponentLeft` to the other player, state replay on successful resume, `END_OPPONENT_LEFT` on grace expiry.
9. `ratelimit.go`: token bucket per session for `SubmitWord` and per IP for room joins.
10. Tests with in-process WS clients: full bot game to completion; two clients play a PvP game; timeout ends the game; reuse and wrong-link rejections reach the client with the right enum; disconnect+resume within grace restores state; resume after grace fails cleanly. All under `-race`.
11. Manual smoke via `websocat` documented in the README.

## Success Criteria

- [ ] `go test ./internal/wsapi/... -race` green
- [ ] A full bot game is playable end to end over a raw WS client, no frontend involved
- [ ] Two in-process clients complete a PvP game with correct alternating turns and a correct winner
- [ ] Turn timeout ends the game server-side even if the client sends nothing
- [ ] Disconnect + resume within 30s restores the game; after 30s the opponent wins by `END_OPPONENT_LEFT`
- [ ] Read limit, rate limit, and origin check each covered by a test
- [ ] `sanitizeNickname` tested against: over-length, control chars, zero-width chars, whitespace-only, and empty input; opponents never receive an unsanitized string
- [ ] `go build ./cmd/noitu-server` yields one binary that serves both `/ws` and the static frontend
- [ ] Graceful shutdown notifies connected clients rather than dropping sockets silently

## Risk Assessment

| Risk | Signal | Response |
|---|---|---|
| Engine touched from two goroutines | `-race` failure | The one-goroutine-per-room rule is structural: the engine is only reachable through the room's channel. A review checklist item, not a runtime guard |
| Timer and message race (move accepted exactly at the deadline) | Flaky test, disputed loss | Resolve entirely inside the room goroutine: the timer fire and the move arrive on the same `select`, so one strictly precedes the other. `turn_seq` makes the outcome explicit to the client |
| Bot goroutine outlives its room | Goroutine leak under load test | Room context cancels the worker; a leak test asserts goroutine count returns to baseline after N games |
| Room code collision | Join lands in the wrong room | Generate with `crypto/rand` and retry on collision inside the hub mutex |
| Restart drops all live games | Players lose in-progress matches on deploy | Accepted for v1 (documented in `plan.md`); graceful shutdown sends a typed error so the UI can explain it |
| Offensive nicknames shown to strangers | Abuse reports | Sanitization caps length and strips control/zero-width characters; a denylist hook is already in `nickname.go` for a data-only fix if it becomes a real problem |
| Reconnect replay diverges from actual state | Resumed client shows a stale board | Replay is a fresh snapshot from the engine, never a stored copy of past messages |
