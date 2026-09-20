# Quick-match implementation report

- Branch: `worktree-agent-ad9e4fa5c795f9e96`
- Worktree: `/workspace/tiennm99/noitu/.claude/worktrees/agent-ad9e4fa5c795f9e96`
- Status: complete

## Setup note

The worktree's branch was at `dd3b463`, five commits behind `dev` and missing
the proto commit (`865222d`) that adds `QuickMatch`/`CancelQuickMatch`. It had
zero commits of its own beyond that point, so `git merge --ff-only dev`
fast-forwarded it cleanly before any implementation work started.

## Files touched

Server (commit `4a78b8a`):
- `server/internal/wsapi/hub.go` — `waiting []*session` FIFO, `quickMatch`/
  `cancelQuickMatch` methods, `errAlreadyQueued`, queue cleared on `shutdown`.
- `server/internal/wsapi/room.go` — `createInput.autoStart`, `room.autoStart`,
  dequeue calls in `handleCreate`/`handleStartBot`/`handleJoin`, auto-begin
  logic in `handleJoin`.
- `server/internal/wsapi/session.go` — `QuickMatch`/`CancelQuickMatch` dispatch
  cases, `cancelQuickMatch` on connection teardown.
- `server/internal/wsapi/codec.go` — `quickMatchStatusMsg`.
- `server/internal/wsapi/metrics.go` — `quickMatchQueued/Cancelled/Matched`.
- `server/internal/wsapi/wsapi_test.go` — `payloadCase` case, `testClient`
  helpers `quickMatch()`/`cancelQuickMatch()`.
- `server/internal/wsapi/quick_match_test.go` (new) — 6 tests.

Web (commit `e6f2d19`):
- `web/src/lib/ws/messages.js` — `quickMatch()`/`cancelQuickMatch()` builders.
- `web/src/lib/stores/game.svelte.js` — `state.queued`, `quickMatchStatus`
  case, clears on `roomState`.
- `web/src/lib/i18n/vi.js` — quick-match strings, `already_in_a_room` /
  `already_queued` error codes, one added sentence in `rulesRoomBody`.
- `web/src/routes/online/+page.svelte` — "Chơi ngay" button, waiting panel
  (elapsed time, cancel, 20s bot nudge), cancel-on-leave.
- `web/tests/game-store.test.js`, `web/tests/ws-client.test.js` — new tests.
- `README.md` — one sentence in "Online play".

## Design choices

- Queue is a plain `[]*session` guarded by the hub's existing mutex — the
  pool is small (brainstormer report B1), no key/skill needed.
- Reused `createInput`/`joinInput`/`beginGame` exactly as instructed. Added
  one field, `createInput.autoStart`, carried into a room-goroutine-only
  `room.autoStart` field; `handleJoin` broadcasts `RoomState` (both seats)
  itself before calling `beginGame`, then clears the flag, so message order
  is: `QuickMatchStatus{false}` (sent synchronously from `hub.quickMatch`,
  before either room input is even sent) → `RoomState` → `GameStarted`.
- `hub.quickMatch` sends both `QuickMatchStatus` messages itself (to the
  caller and to the popped waiter) before touching the room, which is what
  guarantees neither status message can race a `RoomState`/`GameStarted` the
  room produces afterwards — no separate synchronization needed, only
  program order plus the channel send/receive happens-before edge.
- `roomLimiter` is charged once, for the caller of `QuickMatch`, mirroring
  `CreateRoom`; the popped waiter (already charged when it first queued) is
  not charged again.
- Dequeue on "enters a room another way" is hooked into `room.go`'s
  `handleCreate`/`handleStartBot`/`handleJoin` (the actual seating point),
  not into `session.go`'s pre-checks — so a refused attempt (rate limit, full
  room) does not silently drop a real wait.
- `hub.shutdown` needed no new code: a queued session is already registered
  in `h.sessions` (Hello ran before `QuickMatch` could), so the existing
  broadcast to every session already reaches it; the queue slice is cleared
  for tidiness.

## Verification

```
cd server && gofmt -l . && go vet ./... && golangci-lint run ./... && go test ./... -race -count=1
→ 0 issues; all packages ok (wsapi 30.3s, includes TestGoroutinesReturnToBaseline)

cd web && npm run lint && npm run check && npm test
→ lint: 0 errors, 29 pre-existing `any`-type warnings (untouched files)
→ check: 380 files, 0 errors, 0 warnings
→ test: 12 files, 202 tests passed
```

## Deferred / not touched

- `ClaimDeadEnd`, `ReportWord`, `PlayedWord.parts`, `MoveRejected.suggestion`
  — the sibling agent's arms, per instructions.
- No changes to `proto/`, `server/gen`, `web/src/lib/proto` — generated code
  untouched as instructed.
- Did not add a `queuedSinceMs` server-clock-synced timer; the waiting
  panel's elapsed time is a client-side `setInterval`, consistent with it
  being a "still looking" indicator rather than a deadline.

## Unresolved questions

None. Behaviour matches the spec's description of refusal codes, ordering,
idempotency, and the room-cap edge case, all covered by
`quick_match_test.go`.
