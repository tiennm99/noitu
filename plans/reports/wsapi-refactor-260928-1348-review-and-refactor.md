# wsapi review and refactor (dev vs main)

Scope: `server/internal/wsapi/` only. Nothing committed.

## Flake root cause (fixed)

Three separate races, each proven by repetition before and after its fix.

1. **Test-harness race — `TestAGameOutlivesItsFirstElimination`,
   `TestLeavingMidGameFreesTheSeatAndLeavesTheRestPlaying` (and
   `TestResigningOutOfTurnIsRefused`, `TestStandingsReachEverySeat` share the
   same block).** Two guests sent `SetReady` from separate connections at
   once, and the test awaited only one `room_state` per client before the owner
   sent `StartGame`. When the owner's start reached the room before the third
   player's ready, the server correctly refused it with `not_everyone_ready`,
   and the test then timed out waiting for `game_started`. The improved `await`
   diagnostic showed exactly this: `awaiting "game_started": ... (skipped
   [error:not_everyone_ready room_state])`. The server was right, so this is not
   a server bug. Fix: a new `startWith` helper (multiplayer_test.go) readies the
   guests one at a time, drains each resulting `room_state` from every client,
   then starts. Before the fix it failed about 1 run in 20 when run in
   isolation. After it: 0 failures in 80.
2. **Server ordering bug — `TestChatFromASeatlessConnectionIsRefused`
   (turned up by `-race -count=8`).** `lobbyKick` sent `kicked` to the target
   *before* `vacate` released the connection's room binding. A client that
   reacted to `kicked` right away could get its next frame routed back to the
   room, where it was refused as `not_your_seat` instead of `not_in_a_room`.
   Fix: vacate first, then notify (room_lobby.go, `lobbyKick`). With
   `-race -cpu 1,2,4 -count=100`: 2/300 failures before, 0/300 after.

3. **Server ordering bug — `TestMetricsCountWordSubmissions`.**
   `handleSubmit` sent `move_rejected` *before* `recordRejection` incremented
   `noitu_words_rejected`, so a reader that saw the refusal could read the
   counter before it moved (`= 2, want 3`). Fix: count first, then send. This
   is the order every other metric in the package already uses. With
   `-race -cpu 1,2,4 -count=100`: 1/300 failures before, 0/300 after.

## Bugs fixed (behaviour changes)

| Sev | Where | Bug | Fix |
|---|---|---|---|
| Low | room_lobby.go `lobbyKick` | A kicked client was told before it was released, so its next action got the wrong refusal (see above) | Release, then send `kicked` |
| Low | room_game.go `handleSubmit` | A rejection was sent to the player before it was counted in metrics (see above) | Count, then send |
| Low | hub.go `newRegisteredRoom` / old `reserveCode` | The code was checked under one lock hold and registered under another, so two creators that drew the same code could overwrite each other's room in `h.rooms`. The room was also built before the capacity check | The capacity check, code draw and registration now run in one critical section (`unusedCodeLocked`) |
| Low | room_presence.go `handleResume` | Two connections presenting the same resume token at once could both pass `hub.resumable`. The second resume displaced the first, which stayed attached to a seat that was no longer its own (never closed, and refused as `not_your_seat` from then on) | Refuse with `session_not_resumable` unless the seat is empty or still held by the session being replaced |

No wire, close-code, log-line, metric or env-var names changed.

## Refactors (no behaviour change)

- **dispatch.go**: one `toRoom(limiter, notIn, dropped, build)` helper replaces
  five hand-rolled copies of "charge limiter → find room → send → answer on
  drop" (submit, resign, claim, chat, lobby). Every error code is kept per
  message. `toLobby` wraps it for the four lobby actions. `allowRoom` replaces
  three copies of the room-budget check. `session.handleSubmit` was folded in
  and removed.
- **Handshake gate**: `dispatch` now keys on `s.greeted` instead of
  `nickname() == ""`. The mutex around `greeted` is gone, because only the
  reader goroutine touches it (the same as `reportedWords`).
- **session.go**: `send` and `trySend` shared their encode-and-select body.
  Both now go through `enqueue`, which returns `errOutboxFull` or
  `errSessionClosed`. `closeOnce` was dropped because `context.CancelFunc` is
  already idempotent. `attach` takes a `game.PlayerID`. The no-op
  `playerIDFor` cast was removed.
- **room.go**: `connected()` (an `iter.Seq[*seat]`) replaces nine copies of
  `if s == nil || s.sess == nil { continue }` across the broadcast paths.
  `takeSeat` replaces three copies of seat construction plus attach plus
  quick-match dequeue (create, join, bot). `stopCountingLive` replaces the
  duplicated `liveCounted` CompareAndSwap. `occupied` is now
  `seatedCount() > 0`. `seatIDs` moved next to the seat type. The unused
  `sendTo` was removed (callers already hold the acting session).
- **room_presence.go**: `holdSeat` merges `disconnectGhostSeat` with the body
  of `handleDisconnect`.
- **room_lobby.go**: `takenNicknames` lost its argument, which never excluded
  anything (the joiner's seat is free when it is called).
- **room_game.go**: the `wordsSubmitted` increment sat between a comment and
  the code that comment describes. It now comes before the comment.
- **Comments**: removed plan and report references (a `plans/reports/...` path
  in metrics.go, "the review's file split", "the improvement report").

## Tests

- The harness `await` now reports every frame it skipped on failure
  (`describe`), and `read` is `recv` without the Fatal.
- New helpers: `createRoom`, `joinRoom` (62 inline proto literals replaced),
  `agreeAndStart` (the ready-then-start sequence used in about 10 places),
  `startWith` (multiplayer).
- `pvpGame` moved into wsapi_test.go, and `startPvP` and `pvpRoom` are now built
  on `pvpLobby` plus `agreeAndStart` instead of repeating the handshake.
  `awaitNoRooms` uses `hub.roomCount()`.
- No test was deleted or weakened.

## Verification

- `go vet ./...` clean, `gofmt -l` clean.
- `go test -race -count=3 ./internal/wsapi/` passes.
  `go test -race -count=10 ./internal/wsapi/` passes (290s).
  `go test -count=10 ./internal/wsapi/` passes.
- go.mod and go.sum are untouched.

## Deferred / not changed

- `handleResign` and `handleClaimDeadEnd` answer a missing game differently
  (resign is silent, claim sends `game_not_started`). Merging them would change
  the wire behaviour, so both were left as they are.
- `session.attach` sends a `disconnectInput` to the previous room, so a player
  who moves from room A to room B holds a reconnect window in A instead of
  leaving it at once. This may be intentional, and it is a product call.
- Test bodies in regression_test.go overlap with the topic files (lobby, game,
  chat). Merging them needs a case-by-case coverage review and was not done.
- `hub.expireToken` uses `time.AfterFunc`, which is not cancelled on
  shutdown. Each timer lives at most GraceFor, so this is harmless.
