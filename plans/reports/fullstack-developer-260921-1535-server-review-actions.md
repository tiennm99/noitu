# Server architecture review — actions taken

Branch `worktree-agent-a38a77d87cb0d7781` (rebased onto `dev`@`3ef9f48` — see
note below) · worktree
`/workspace/tiennm99/noitu/.claude/worktrees/agent-a38a77d87cb0d7781`

## Worktree base — corrected before starting

The worktree handed to me was branched from an ancestor roughly 18 commits
behind `dev`@`3ef9f48` (missing quick-match, drain mode, dead-end claims, word
reports, counters and readiness — everything the review and this task assume
exists). `git status` showed a clean tree and the branch was a strict ancestor
of `3ef9f48` with zero commits of its own, so I fast-forwarded it
(`git merge --ff-only 3ef9f48`) before touching anything. No content was lost;
this was a pure catch-up.

## Deliverables

**1. Join limiter** — `session.go`: `joinsPerSecond`/`joinBurst` raised
1/5 → 5/20, comment rewritten with the room-code-space (31^6) and
CGNAT-egress reasoning the review gives. No existing test pinned the old
numbers.

**2. Per-IP concurrent-connection cap** — `server.go`: `Config.MaxConnectionsPerIP`
(env `NOITU_MAX_CONNECTIONS_PER_IP`, default `0` = off), enforced in
`handleWS` before `websocket.Accept`, HTTP 503 like the existing global cap.
`reserveIP`/`releaseIP` guarded by their own mutex, independent of the hub's.
Documented in both env tables (README.md, docs/deployment.md) with the
proxy warning. Tests: refuses past the cap, off by default, releases its slot
on disconnect (`limits_test.go`).

**3. Ghost seat** — `room.go` (now split, see below): `handleCreate`,
`handleJoin`, `handleStartBot` and `handleResume` all check
`sess.ctx.Err() != nil` right after seating. `handleCreate`/`handleJoin`/
`handleResume` reopen the grace window via a new `disconnectGhostSeat` helper
(the same effect `handleDisconnect` produces) so `allConnected()` reports the
seat honestly and quick match's auto-start cannot fire against it.
`handleStartBot` cancels the room instead — a bot room has no lobby to fall
back to and no idle timer while there is no engine yet, so a grace window
there would orphan the room forever. Five direct room-level tests, in the
style of `TestQuickMatchSkipsAWaiterWhoseConnectionEnded` (a `*session` built
with an already-cancelled `ctx`), plus one confirming quick-match auto-start
is skipped end to end.

**4. Drain refuses new games** — `handleLobby`'s `lobbyStart` arm and
`handleJoin`'s quick-match auto-start both check `r.hub.isDraining()` and
refuse with `server_restarting`, matching what `newRegisteredRoom` already
does for a brand-new room. Tests: an existing lobby's `StartGame` is refused
after `StartDraining`; quick-match's auto-start is refused the same way at
the room level; a game already running still finishes and decrements
`LiveGameCount` normally.

**5. Non-resumable resume token** — `session.go` `handleHello`: a non-empty
resume token that does not resolve to a live session now gets
`session_not_resumable` (the code the web client's table already has) instead
of silence, and the connection continues as a fresh session. An empty token
(no resume attempted) gets nothing, as before. Tests: the answer arrives and
the connection stays usable; a fresh Hello with no token gets no such error.

**6. Drop `History` from `game.State`** — `engine.go`: `Engine.LastMove()
(Move, bool)` and `Engine.UsedWords() iter.Seq[string]` (backed directly by
the engine's own `used` map, which already contains the opening word) added;
`State.History` removed; `Standings()` only computed in `Snapshot()` when
`e.over`. Two call sites migrated: `room.go`'s resume replay now uses
`LastMove()`, and `freezeBoard` now builds its used-set from `UsedWords()`
directly (dropping the `opening` parameter it no longer needs — the engine's
set already includes it). `engine_test.go`/`multiplayer_test.go` updated;
added `TestLastMove` and `TestUsedWords`. Full `game` package still green.

**7. File splits** — mechanical, verified by counting: every top-level
declaration from the original file appears exactly once afterward.
  - `room.go` (1919 lines) → `room.go` (517, struct/constructor/run loop/seat
    helpers), `room_inputs.go` (125, message types), `room_lobby.go` (375,
    seating and the lobby), `room_game.go` (622, everything touching a
    running game), `room_presence.go` (177, reconnect window and resume),
    `room_chat.go` (125, the room's conversation), `bot_board.go` (48, the
    bot's frozen board).
  - `session.go` (762 lines) → `session.go` (445, the socket) and
    `dispatch.go` (334, the protocol: `dispatch`, `handleHello`, `resumeFrom`,
    `handleSubmit`, `handleReportWord`, `toRoom`, `roomCreateError`,
    `leaveRoom`).
  - `wsapi_test.go` (2688 lines, 82 tests) → `wsapi_test.go` (615, harness
    only: dictionary fixture, server-over-socket helpers, client driving
    methods), `game_test.go`, `presence_test.go`, `resume_test.go`,
    `lobby_test.go`, `protocol_test.go`, `ratelimit_test.go`,
    `bot_board_test.go`, `chat_test.go`, `deadend_test.go`, `report_test.go`.
    Three nickname-sanitizing tests moved into the already-existing
    `nickname_test.go` alongside its fuzz target.
  - `hub_test.go` (3-line comment stub) deleted; its note now lives next to
    `hub.go`'s `roomCount`.
  - Not done: `hub.go`'s optional `quickmatch.go`/`roomcode.go` split — that
    was review §1's "worth doing" suggestion, not one of the 10 assigned
    items, and hub.go is not oversized (403 lines, one clear mutex
    discipline). Left alone per scope.

**8. `cmd/noitu-server` tests** — `main_test.go`: `env`, `envInt`,
`envDuration`, `envNonNegDuration`, `envList` all covered for their fallback,
validation and trim behaviour. `waitForGamesToFinish`'s `*wsapi.Server`
parameter narrowed to a `gameCounter` interface (its one method) so the
poll-then-check timing is testable with a fake — covers the "returns as soon
as the count hits zero", "respects its timeout", and "zero timeout returns
immediately" cases. `run()`/`main()`/`newDebugServer` remain untested — they
need a real listener and are out of the review's own recommended scope
("main_test.go for the env parsers + drain loop"). Package coverage: 0% →
33.8%.

**9. Dockerfile** — `golang:1.25-alpine` → `golang:1-alpine`,
`alpine:3.22` → `alpine:3`. `.github/dependabot.yml` checked: still covers
the same four ecosystems correctly; no change needed, it will simply have
less to propose now that these two also float.

**10. Decisions recorded** in `docs/deployment.md` — a new "Resuming from a
second tab" section states the two-tab takeover is intended (newest
connection with a live token wins the seat, on purpose); the `/debug/vars`
paragraph now says explicitly that expvar always publishes the process's
argv and heap stats, which is why it lives on a separate address and must
never bind a public interface.

## Verification tail

```
$ gofmt -l .
(clean)
$ go vet ./...
(clean)
$ golangci-lint run ./...
0 issues.
$ go test ./... -race -count=1
ok  	.../server/cmd/build-dictionary
ok  	.../server/cmd/noitu-server
ok  	.../server/internal/bot
ok  	.../server/internal/dictionary
ok  	.../server/internal/game
ok  	.../server/internal/vietnamese
ok  	.../server/internal/wsapi
```

Coverage: `vietnamese` 100, `game` 95.6 (was 95.5), `bot` 91.2, `wsapi` 91.3
(was 91.1), `dictionary` 89.0, `build-dictionary` 88.6, `cmd/noitu-server`
33.8 (was 0.0).

`git diff --stat 3ef9f48 HEAD` touches only `Dockerfile`, `README.md`,
`docs/deployment.md`, `server/**` — nothing under `web/`, `proto/`,
`server/gen/`, or `.github/`.

## Commits (9, on this branch, not pushed)

1. `fix(server): cap concurrent connections per client IP`
2. `fix(wsapi): loosen the join limiter and answer a non-resumable token`
3. `fix(wsapi): guard room seating races, refuse games during drain, and drop the history copy`
4. `test(wsapi): cover ghost seats, drain refusals and a silent resume`
5. `test(server): cover the env parsers and the drain-wait loop`
6. `build(docker): track the moving golang and alpine majors`
7. `docs: document the per-IP cap and record two open decisions`
8. `refactor(wsapi): split room.go and session.go along their seams`
9. `refactor(wsapi): split wsapi_test.go by topic, delete hub_test.go`

## Deferred (explicitly out of the 10 assigned items)

- **C4** (word_rejected log volume) — sampling or dropping to Debug, not done.
- **C5** (`dispatch`'s missing `default` arm / `unknown_message`) — not done.
- **C6** (`Config.IdleFor` dead knob) — left as-is; review's own unresolved
  question, needs a product decision (wire `NOITU_IDLE_TIMEOUT` or delete the
  field).
- **C9** (`reserveCode` before the `maxRooms` check) — cheap reorder, not done.
- Review §6's `make test-go` / `lint-go` Makefile target — not done, not one
  of the 10 items.
- 85% coverage floor in CI — not done; review's own unresolved question on
  whether `cmd/` should be held to it.
- Community allowlist overlay (§3) — design-only in the review, no code
  expected.

## Unresolved questions

None blocking. The two decisions the review flagged as needing a call (C2's
exact rate, C8's takeover semantics) were both resolved by this task's
explicit instructions (5/s burst 20; takeover is intended and now
documented). The deferred items above are all ones the review itself marked
as open product questions (C6, the coverage floor) or as optional/lower
priority than the 10 assigned actions (C4, C5, C9, the Makefile target).

Status: DONE
Summary: All 10 review actions implemented on branch `worktree-agent-a38a77d87cb0d7781` at `/workspace/tiennm99/noitu/.claude/worktrees/agent-a38a77d87cb0d7781`, in 9 focused commits; gofmt/vet/golangci-lint/`go test -race` all clean.
Concerns: The worktree's starting branch was 18 commits stale relative to the `dev`@`3ef9f48` base the task specified; I fast-forwarded it before starting (see note above) rather than working against code that predated quick-match, drain mode and dead-end claims entirely.
