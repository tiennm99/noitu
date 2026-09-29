# wsapi transport review: whole package, read-only

Date: 2026-09-29 · branch `dev` @ d3eb13e · scope `server/internal/wsapi/**`, `proto/noitu/v1/game.proto` (read for contract only)

## Verdict

The actor model is sound. Every engine and seat mutation stays on the room goroutine, all
sends are non-blocking, and `-race` is clean. The findings below are lifecycle and
identity bugs at the edges of that model: what happens to a seat *id* after its seat is
freed, what happens to inputs queued behind a room's last one, and what happens when a
control notice has to share the inbox with player spam. Each one was reproduced with a
throwaway test in a scratch copy of the module, and the suggested fixes for #1, #2 and #4
were applied in that copy and pass the whole package under `-race` (the only failure was
`TestCrossLanguageFixtures`, and only because the copy has no `proto/testdata`).

Baseline on the real tree: `go vet ./internal/wsapi` clean, `go test ./internal/wsapi -race -count=1` ok (29.6s).
No project file was modified.

## Findings

| # | Sev | Where | Finding |
|---|---|---|---|
| 1 | High | room_presence.go:473, :380-385; room_lobby.go:189-191 | A kicked player's token resumes into whoever now holds the same seat id: their name, wins and chat window, and the real occupant is locked out |
| 2 | Med | room.go:262 (defer order), exits at :410/:425/:431 | Inputs queued behind a room's final input are never answered. A join or resume that loses the race waits forever |
| 3 | Med | dispatch.go:350-356, session.go:203-205, dispatch.go:296-298, room.go:236-258 | Disconnect and resume notices share the lossy 32-slot inbox. Four seats bursting unmetered `Resign` overflow it, a dropped disconnect leaves a ghost "connected" seat, and a dropped resume is reported as `game_already_over` |
| 4 | Low | room_lobby.go:66-69, :78-94 | `autoStart` stays armed after a quick-match pairing that never started, so the next code joiner starts a game nobody readied for |
| 5 | Low | room.go:330, :433-435 | The idle window is reset by *any* input, refused ones included. A refused `Resign` every few minutes holds a lobby open forever |
| 6 | Low | server.go:268-291, dispatch.go:68 | The join limiter is keyed on the full IPv6 address, so one /64 has 2^64 buckets (and F10's unmapped-v4 key split is still open) |
| 7 | Low | hub.go:192-197 | Quick match still queues a player while draining, and they wait until shutdown |
| 8 | Low | dispatch.go:28-157 | No `default` arm: an unknown or empty payload is silently dropped (prior C5, recorded "not done", never rejected) |
| 9 | Low (test) | chat_test.go:317 | `TestChatDoesNotKeepARoomAlive` passes with chat resetting the idle clock |
| 10 | Low (test) | limits_test.go:203 | `detachAll` has no test at all. The whole suite passes with `defer r.detachAll()` deleted, even though the prior review cited this test as its closure |
| 11 | Nit (test) | convert_test.go:141 | A swapped `PointKind` arm (CHAIN<->SYLLABLES) passes the whole suite |
| 12 | Nit | room_presence.go:482 | A resume does not leave the quick-match queue, unlike every `takeSeat` path |
| 13 | Nit (test) | lobby_test.go:474-488 | A hand-rolled copy of `awaitNoRooms` |

---

### 1. High: a stale token resumes into another player's seat

**What is wrong.** `handleResume` looks the seat up by id (`seatOf(m.player)`) and accepts
when `s.sess == nil`. Seat ids `p1..p4` are reused. A seat vacated while its player is in
the grace window never releases that player's prior session: `vacate` only calls `release`
when `s.sess != nil`, and `holdSeat` has already nil'd it. So the prior session still points
at the room with id `p2`, and its token stays live for the rest of *its own* grace window.

**Failure scenario (reproduced end to end).** Alice (p2) drops. The owner kicks her
offline seat, which is allowed because she is not ready. Carol joins and gets p2, chats,
then refreshes, which puts p2 in grace for Carol. Alice's tab reconnects within 30s and
`web/src/lib/ws/client.js:228` presents her stored token automatically.
Result: `BUG: kicked Alice resumed into seat p2 named "Carol"`. Alice's replayed
`ChatHistory` contained `"carol-only secret" from "Carol"`, and Carol's own resume was then
answered `session_not_resumable`. That is three README invariants broken at once: resume
into the wrong seat, take another seat's series score, and chat replay leaking. Grace
expiry followed by a rejoin hits the same root cause, but only in the microsecond gap
before the token expires.

**Fix (verified in scratch).** Remember which connection a window is being held for:

```go
// seat
heldFor *session // the connection whose drop opened graceUntil

// holdSeat
s.heldFor = s.sess
s.sess = nil

// handleResume
if s == nil || (s.sess != m.prior && (s.sess != nil || s.heldFor != m.prior)) {
    m.sess.send(errorMsg(codeSessionNotResumable))
    return
}
```

A refilled seat is a new `*seat`, so its `heldFor` can never be the kicked player's
session. Add the scratch reproduction as a regression test in resume_test.go.

### 2. Med: inputs queued at room exit are never answered

**What is wrong.** `run` returns as soon as the room is empty, idle, or a bot game ends,
and anything still in `r.inputs` is dropped without a reply. `r.cancel()` is also the
*first* defer registered, so it runs *last*. Until then `send` still returns true into a
room nobody reads, which is exactly the "caller waits forever" case `send`'s own comment
describes.

**Failure scenarios.** (a) The owner is alone and clicks Leave while a friend's
`JoinRoom` is behind it in the inbox. `hub.joinRoom` returned nil, so dispatch answers
nothing and the friend never gets a `room_state` or an error. Reproduced by queuing
create, leave, join and running the room: the joiner received **zero** frames.
(b) The last seat's grace timer and that player's `resumeInput` are ready on the same
`select`. If the grace arm wins, the room exits and the resume is never answered, which
leaves the client's resume latch hanging (the case handleHello:271-277 was written to prevent).

**Fix (verified in scratch).** Cancel first, then answer whatever is left:

```go
func (r *room) run() {
    defer r.refusePending() // runs last: after cancel, so nothing new is accepted
    ... existing defers ...
    defer r.cancel()        // registered last, so it runs first
```

`refusePending` drains non-blockingly and answers `joinInput` with `room_not_found`,
`resumeInput` with `session_not_resumable`, submit/resign/claim with `not_in_a_game`, and
lobby/chat with `not_in_a_room`.

### 3. Med: control notices can be dropped by a full inbox

**What is wrong.** `leaveRoom`'s `disconnectInput`, `attach`'s release of the previous room,
and `resumeFrom`'s `resumeInput` all go through `room.send`, which drops on a full inbox.
`Resign` is the one room input with no per-action limiter (`dispatch.go:118`, `nil`). Each
connection may burst 40 frames, and every refused `Resign` in a lobby still takes an inbox
slot.

**Measured.** With four seats each writing 39 `Resign` frames at once, the room logged
`room inbox full, dropping message` 4, 86 and 38 times across three runs, and 357 times
over three runs at `GOMAXPROCS=1`. One connection alone never overflowed it. Constructed
consequence (scratch test): the guest's disconnect was dropped, and after the grace window
had passed the seat was **still bound to its dead session** with `graceUntil` zero.
`allConnected()` then reports true, so `canStart` goes green against a dead socket, the
seat never expires, and a ready ghost cannot be kicked (`player_is_ready`). A dropped
`resumeInput` is also reported as `game_already_over` (dispatch.go:296-298), which is a
false statement: the room is alive, only busy.

**Fix.** (a) Charge `Resign` on `s.submitLimiter`, like `ClaimDeadEnd`. (b) Deliver the
two teardown notices reliably. `leaveRoom` runs on a dying session goroutine and can block
safely: `select { case r.inputs <- m: case <-r.ctx.Done(): }`. For `attach`, which runs on
*another* room's goroutine, do the same inside `go func(){...}()` so two rooms can never
wait on each other. (c) In `resumeFrom`, answer a busy room with `busy` rather than
`game_already_over`.

### 4. Low: quick-match `autoStart` outlives its pairing

**What is wrong.** `r.autoStart` is cleared only when the auto-start actually fires. If
the pairing's joiner was already torn down (the `holdSeat` return at :66-69), or either
side is offline at join time, the flag stays set. The next `handleJoin` that fills a second
connected seat starts a game with no readiness and no `StartGame`.
**Scenario (reproduced at handler level).** The quick-match partner dies before seating
and grace expires. The waiter shares the room code (it is in their `RoomState`). The
friend who joins is dropped straight into `game_started` with `ready=false`.
**Fix (verified).** The first `handleJoin` in the room is the pairing join, so consume the
flag there: `autoStart := r.autoStart; r.autoStart = false`, and also clear it on the
`holdSeat` early return.

### 5. Low: the idle window is not "ten minutes with no game started"

`idleActivity` is true for every input except chat and report, and for the grace arm.
Refused `Resign`, `SubmitWord` or `ClaimDeadEnd` (`game_not_started`), a stranger's refused
join (`room_full`) and ready toggles all restart it. Reproduced: with `IdleFor: 300ms`, a
refused `Resign` every 150ms kept the lobby open for 5x the window. **Fix:** reset only
when the input changed room state. Capture `changed := r.lobbyChanged` before the broadcast
block and call `resetIdleTimer()` only when `changed`. `handleChat` never sets
`lobbyChanged`, so the chat special case goes away.

### 6. Low: the join limiter treats every IPv6 address as its own client

`clientIP` returns the peer or hop address as a raw string, and `hub.joinLimiter` keys on
it. A single residential IPv6 customer controls a /64, which is 2^64 distinct buckets, and
that makes the 5/s code-walk guard (session.go:53-61) meaningless on a dual-stack
deployment. The same raw string also keeps prior finding F10 open, where `::ffff:a.b.c.d`
and `a.b.c.d` count as two keys. **Fix:** derive the limiter key once:
`addr.Unmap()`, and for `Is6()` use `netip.PrefixFrom(addr, 64).Masked().String()`. Use it
for `reserveIP` too.

### 7. Low: quick match queues while draining

`newRegisteredRoom` refuses rooms when draining, but the enqueue path (`hub.go:192-197`)
does not check. A lone player is told `queued:true` and then waits until shutdown. **Fix:**
`if h.draining.Load() { return errDraining }` at the top of `quickMatch`. dispatch
already maps that error to `server_restarting`.

### 8. Low: `dispatch` has no `default` arm (still open)

This was C5 in the 2026-09-21 review. The action report recorded it as "not done", not as
rejected. A `ClientMessage` with no payload, or one from a newer client, costs a frame and
gets no reply. **Fix:** `default: s.send(errorMsg(codeUnknownMessage))`, adding the code
to errcodes.go and `vi.js` (the web error-codes test will insist).

### 9. Low (test): `TestChatDoesNotKeepARoomAlive` cannot fail

Mutation check: with `idleActivity = false` removed from the chat arm, the test still
passes 3/3. The idle close merely arrives about 300ms later, and `await` allows 5s.
**Fix:** record `start` before the chats and fail if `room_idle_closed` arrives later than
`IdleFor + 150ms`.

### 10. Low (test): `detachAll` is untested

With `defer r.detachAll()` deleted, the **whole package passes**. `TestIdleRoomReleasesItsSeats`
proves the connection can create a new room, but `CreateRoom` succeeds either way, because
`attach` just overwrites the dead room pointer. **Fix:** after the idle close, have the
host `say("x")` and require `not_in_a_room`. Without `detachAll`, `toRoom` finds the dead
room and answers `busy`.

### 11. Nit (test): enum mapping is pinned for shape, not meaning

The exhaustiveness tests in convert_test.go prove each `game.PointKind` maps to a distinct
non-UNSPECIFIED wire value, but a swapped pair is still a bijection. Mutation check: CHAIN
and SYLLABLES swapped, suite green. (Swapping two reject reasons *is* caught, by
`TestRejectionsCarryTheRightReason`.) **Fix:** one line in `TestPointKindMappingIsExhaustive`:
`if want := "POINT_KIND_" + strings.ToUpper(k.String()); got.String() != want { t.Errorf(...) }`.
This works because `game.PointKind.String()` values are single words matching the wire suffixes.

### 12. Nit: a resume does not leave the quick-match queue

Every `takeSeat` path calls `hub.cancelQuickMatch`, but `handleResume` binds with
`attach` directly. A client that sends `QuickMatch` between its Hello and the room
draining the resume ends up both seated and queued. A later pairing would then pull it out
of a running game past `refuseMidGame`. **Fix:** `r.hub.cancelQuickMatch(m.sess)` next to
`m.sess.attach` at :482.

### 13. Nit (test)

`TestOneConnectionCannotStrandRooms` (lobby_test.go:474-488) re-implements `awaitNoRooms`
with its own lock-and-poll. Replace it with the helper.

## Test gaps with no coverage today

- A kicked or expired seat id being resumed by its old token (#1).
- Any input queued behind a room's final input (#2).
- Inbox overflow dropping a lifecycle notice (#3). The scratch test builds it directly on
  `newRoom` plus `offlineSession`.
- `autoStart` after a failed pairing (#4). `TestQuickMatchAutoStartSkipsAGhostSeat` stops
  one step short of it.
- The idle clock under refused actions (#5).

## Checked and clean

- **Engine ownership:** every `*room` handler is reached only from `run`'s switch. The bot
  worker sees a `frozenBoard` copy and exits on `r.ctx`. `strategy` is never used by two
  workers at once, because only the bot's own turn schedules one.
- **Timers:** all three are recreated rather than reset, stopped in a defer, and
  recomputed after every input. `graceC` is nil'd before rearming. A negative
  `time.Until` fires immediately, which is correct.
- **Session teardown:** `readCtx` is cancelled only after flush or 2s, with no leak of the
  three goroutines (`wg.Wait`). `close` is idempotent (`CancelFunc`). The outbox never
  blocks the room.
- **Hub:** the `rooms` and `sessions` maps are touched only under `mu`. Code draw, cap
  check and registration share one critical section. `evict` runs on exit. `liveGames`
  is decremented exactly once via the CAS.
- **Join semantics:** a running game refuses latecomers (`game_in_progress`), a full or
  empty room refuses (`room_full`), and a second join to your own room is refused.
  Start needs 2+ seats, everyone connected and every guest ready, and the owner has no
  ready flag. Promotion clears ready, and every game clears ready again.
- **Turn authority:** submit, resign, claim and chat all check `occupies(sess, id)` (the
  seat, not the claimed id) and the turn owner. A stale `turn_seq` is refused with the
  server's sequence. `turnSeq` never restarts across rematches, and it moves on an
  out-of-turn forfeit only when the turn does.
- **Resume races:** concurrent presentation of one token is guarded
  (`s.sess != m.prior`). A dead new connection reopens the window. A stale
  `disconnectInput` from the replaced connection is ignored.
- **Quick match:** it never pairs a connection with itself (`w == s`) and skips dead
  waiters. Teardown and every seating path dequeue. The status is sent before any room frame.
- **Chat:** `chatFrom` scopes replay to the seat's tenure. Vacating scrubs author and name
  together and re-syncs the remaining seats. Bot rooms have no chat. Chat uses `trySend`,
  history uses `send`.
- **Sanitisation:** NFC first, then Cc/Cf/non-printing dropped (which also removes
  Zl/Zp/NBSP), marks capped at 2, whitespace collapsed, and a rune cap on nickname (20),
  chat (200), echoed word and report (64). `distinguish` includes seats in grace.
- **Limits:** a 4 KiB read limit, 20/s frame limiter closing on flood, per-session room,
  submit and chat buckets, the join bucket per IP, a 20-report cap per session, a 32-frame
  outbox, and a limiter sweep.
- **Enum mapping:** `RejectReason` and `EndReason` are pinned both ways, including
  semantically through e2e tests. `PointKind` is pinned for shape only (#11).
- **Contract:** `errorMsg` carries only UI keys (`TestErrorMessagesAreUIKeysNotProse`),
  and the wire round-trip and oneof coverage tests are present. No proto change is needed
  for any fix above (#8 adds only an error-code string).
- **Test hygiene:** the remaining sleeps are deadline-bounded polls. `settle()` is used
  only before goroutine and count sampling, and not as a correctness wait, except in
  `TestQuickMatchDropsADisconnectedWaiter`, whose dead-waiter skip is covered separately by
  `TestQuickMatchSkipsAWaiterWhoseConnectionEnded`.

## Unresolved questions

- #1's fix answers `session_not_resumable` to a kicked player who comes back. Is that the
  right message, or should a kick explicitly revoke the token (`hub.unregister`) so the
  client learns it at Hello?
- #5: should a ready toggle count as activity? The proposed `lobbyChanged` rule says yes.
