# Server architecture review — noitu Go server

Branch `dev` @ 5178a97 · 2026-09-21 · read-only · scope `server/`, `proto/`, `Makefile`, `Dockerfile`, `.github/`

## Verdict

The server is in good shape and the design is sound. `go vet`, `golangci-lint`
(defaults) and `go test ./... -race -count=1` are all clean; coverage is
88–100% everywhere except `cmd/noitu-server` at **0.0%**. No data race, no
goroutine leak (`TestGoroutinesReturnToBaseline`), no auth bypass, no injection
surface, no PII in logs.

The one-goroutine-per-room actor with a single `inputs chan any` is **the right
shape and should stay**. `room.go`'s 1858 lines are a *file* problem, not an
architecture problem: every method is on `*room`, called only from `run()`, so
the engine-ownership invariant holds by construction and a pure file split
preserves it at zero risk. Do not convert it to typed handlers or a state
machine, and do not split lobby from game — see §1 for why each would cost more
than it buys.

Real defects are small and few. The most valuable work is not defect-fixing: it
is (a) the per-IP join limiter, which is simultaneously the CI flake and a
CGNAT availability bug, (b) removing `State.History` from the hot broadcast
path, (c) splitting the four oversized files, (d) closing the 0% coverage hole.

## Top 10 ranked actions

| # | Action | Kind | Size | Risk | Why now |
|---|---|---|---|---|---|
| 1 | Per-IP join limiter is shared by every client behind one NAT egress (1/s, burst 5) — raise it, key it better, or make it configurable | fix | S | low | Explains today's CI flake **and** breaks a whole café/CGNAT of real players (§4 C2, §5) |
| 2 | Add a per-IP concurrent-connection cap (F6) | fix | S | low | One host can hold all 2000 sockets; the only remaining unbounded-per-peer resource |
| 3 | Split `room.go` into 7 files along existing seams | refactor | M | ~nil | 1858 lines is past the point where a reviewer can hold it; pure moves, invariants preserved by construction (§1) |
| 4 | Drop `History` from `game.State`; add `LastMove()` + `UsedWords()` | refactor | S | low | O(chain) copy on **every** TurnUpdate and every bot move, for two call sites (§2) |
| 5 | Refuse `StartGame` / quick-match auto-start while draining (F7) | fix | S | low | `beginGame` can raise `liveGames` after the drain decision; drain then waits its full timeout (§4 C3) |
| 6 | Guard `attach` against a session already torn down | fix | S | low | Quick match can auto-start a game against a dead socket, burning a 30 s turn clock in front of a real player (§4 C1) |
| 7 | `cmd/noitu-server` at 0% — add `main_test.go` for the env parsers + drain loop | fix | S | nil | Only real coverage hole; all pure functions; config parsing is where a prod misconfig hides |
| 8 | Split `wsapi_test.go` (2534 lines, 75 tests) by topic; delete the 3-line `hub_test.go` | refactor | M | nil | Mirrors #3; a test file nobody can navigate stops being extended (§5) |
| 9 | Split `session.go` into socket half / protocol half | refactor | S | nil | Two unrelated concerns in one 746-line file (§1) |
| 10 | Dockerfile: `golang:1.25-alpine` → `golang:1-alpine`, `alpine:3.22` → `alpine:3` | fix | S | low | House rule is moving-major; dependabot is currently generating churn the rule exists to avoid (§6) |

Deliberately **not** in the list, with reasons in-section: typed handler
dispatch (§1), lobby/game actor split (§1), `Engine` API consolidation (§2),
bot `Board` changes (§2), committing word data (§3).

---

## 1. Structural assessment

### `wsapi/room.go` — keep the actor, split the file

The actor is doing real work that a redesign would have to re-earn:

- `room.go:387` `run()` is the only place the engine is touched; every handler
  is a `*room` method reachable only from the type switch at `room.go:474-511`.
- `room.go:540-543` — one `RoomState` per input, driven by the `lobbyChanged`
  flag, from the single place that knows the input finished. No handler has to
  remember to broadcast.
- `room.go:498-511` — timers are recomputed after *every* input rather than per
  arm, so a new input type cannot silently forget one.
- `room.go:362` `send()` never blocks a session goroutine, and checks
  `ctx.Done()` on its own before the `select` so a dead room cannot report a
  false success.

**Proposal (recommended): 7 files, pure moves, no signature changes.**

| File | Contents | ~LOC |
|---|---|---|
| `room.go` | struct, `newRoom`, `send`, `run` + timer closures, `inLobby`/`occupied`/`freeSeat`/`seatedCount`/`allConnected`/`seatOf`/`sendTo`/`broadcastError` | 350 |
| `room_inputs.go` | every `*Input` struct + `lobbyAction` consts (`room.go:94-204`) | 120 |
| `room_lobby.go` | `handleCreate`, `handleJoin`, `handleLobby`, `canStart`, `guestsReady`, `takenNicknames`, `vacate`, `promote`, `playerSlots`, `broadcastRoomState`, `seatIDs` | 380 |
| `room_game.go` | `beginGame`, `handleSubmit`, `handleResign`, `handleClaimDeadEnd`, `handleBotMove`, `maybeScheduleBot`, `mark`/`inputMark`, `applyEliminations`, `broadcastTurn`/`sendTurnUpdate`/`sendGameStarted`/`broadcastElimination`/`broadcastGameOver`, `scoreRows`, `wireEndReason`, `nearMissFor`, `recordRejection`, `handleReportWord` | 420 |
| `room_presence.go` | `handleDisconnect`, `nextGraceExpiry`, `handleGraceExpiry`, `eliminateAbsent`, `handleResume`, `detachAll` | 200 |
| `room_chat.go` | `chatEntry`, `handleChat`, `sendChatHistory`, `chatMessageFor` | 150 |
| `bot_board.go` | `frozenBoard`, `freezeBoard` (`room.go:1824-1858`) | 50 |

Invariants: **all preserved, by construction.** Same package, same receiver,
same call graph — the engine stays on the room goroutine because nothing else
can reach a `*room` method; the one-broadcast-per-input site does not move; the
per-recipient rendering loops are untouched. Risk is limited to a bad
copy-paste, which `go build` catches.

**Rejected — typed handlers (`interface{ apply(*room) }` or a handler map).**
The type switch is 30 lines and is the *only* dispatch in the package. Moving it
onto the input types scatters the two exceptions that currently read at a glance
(`chatInput` and `reportWordInput` set `idleActivity = false`, `room.go:489`,
`room.go:497`) into the types themselves, and buys no exhaustiveness — Go gives
none for either shape. Revisit only past ~20 arms.

**Rejected — a phase state machine.** The lobby/playing/over phase is already
derived in exactly one place (`room.go:1595` `inLobby()`), and every handler's
guard asks a *different* question — seat ownership (`occupies`), turn ownership,
owner role, readiness. A phase enum answers none of them and adds a field that
can disagree with `r.engine`.

**Rejected — separate lobby and game actors.** The state that spans both is
exactly the state that makes it hard: `seats` (with `wins`, `chatFrom`,
`graceUntil`), `chat`, `chatSeq`, `owner`, `turnSeq`. Two actors would need a
handoff protocol for all of it and would reintroduce the race the single
goroutine removes. `seat.wins` (`room.go:222-226`) and the chat log explicitly
outlive games; `handleGraceExpiry` (`room.go:1402`) must work identically in and
out of a game.

### `wsapi/hub.go` (402) — one split worth doing

Size is fine and the mutex discipline is right (registries only, never engine
state — `hub.go:46-51`). One concern has its own policy, its own metrics and the
most obvious growth path: extract `quickmatch.go` (~90 lines: `waiting`,
`quickMatch`, `cancelQuickMatch`, `errAlreadyQueued`). While there, decide
whether the queue needs `hub.mu` at all — it is never mutated atomically with
`rooms` or `sessions`, so its own mutex would document that. Optional second
split: `roomcode.go` for `roomCodeAlphabet`/`randomCode`/`reserveCode` (~60).

### `wsapi/session.go` (746) — split by concern, keep the switch

Two unrelated halves: the socket (`run`, `readLoop`, `writeLoop`, `write`,
`drain`, `keepalive`, `close`, `send`, `trySend`, `attach`/`release` — ~330) and
the protocol (`dispatch` and its handlers, `handleHello`, `resumeFrom`,
`handleSubmit`, `handleReportWord`, `toRoom`, `roomCreateError` — ~350). Split
into `session.go` and `dispatch.go`.

The 16-arm `switch` at `session.go:441` is fine as a switch — it is the wire
contract read top to bottom, and each arm's error code is deliberately
different. The duplication worth removing is smaller: `Resign`
(`session.go:513`), `ClaimDeadEnd` (`session.go:524`) and `SendChat`
(`session.go:560`) each re-implement `toRoom`'s currentRoom-then-send shape with
their own codes. One helper taking `(limiter, input, missingCode, droppedCode)`
collapses ~30 lines and turns "which limiter pays for which action" into a
table. Do it with #9, not before.

**Missing arm:** `dispatch` has no `default`. A `ClientMessage` with an unset or
future payload is silently accepted and ignored. Add
`default: s.send(errorMsg("unknown_message"))` plus a counter — see §4 C5.

---

## 2. Engine and bot

**`State.History` is copied on every broadcast for two readers.**
`engine.go:576` does `append([]Move{}, e.history...)` inside `Snapshot()`, and
`Snapshot()` is called at `room.go:908` (beginGame), `room.go:1142`
(broadcastTurn — **every turn**), `room.go:1207` (applyEliminations) and
`room.go:1831` (freezeBoard — **every bot move**). `History` is read at exactly
two of those: `room.go:1487-1488` wants only the last move, and `room.go:1836`
wants only the played-word set.

Fix: drop `History` from `State`; add `Engine.LastMove() (Move, bool)` and
`Engine.UsedWords() iter.Seq[string]` (or a copied map). This removes an
O(chain) allocation from the hot path entirely. `State` is internal and `wsapi`
is its only non-test consumer, so the blast radius is `engine_test.go` +
`wsapi_test.go` assertions. Size S.

**`Standings()` runs on every `Snapshot()`** (`engine.go:587`) even mid-game,
where its own doc says it is meaningless (`engine.go:539-540`). Guard it:
`if e.over`. Trivial, same commit as above.

**`Engine` API surface: leave it.** `Submit`/`Timeout`/`NoMove`/`Resign` are
four genuinely different rules, not four spellings of one, and the caller-side
pairing with `mark()`/`applyEliminations` is already centralized in exactly one
function (`room.go:1203`). `pointsFor` returning `(int, []PointPart)` with
`capParts` trimming from the end (`engine.go:298-322`) is correct and
well-reasoned — a client with no wordlist cannot re-derive the breakdown, so it
has to travel. No change.

**Rule duplication engine↔room: essentially none.** One near-miss:
`room.go:1053-1056` (`nearMissFor`) re-applies the link and used-word checks
that `Submit` would apply. That is deliberate and commented — offering a
suggestion that would immediately be refused is worse than offering none. Leave.
`handleClaimDeadEnd` (`room.go:641`) calls `HasLegalMove()` then `NoMove()`,
which re-walks; a wasted scan on a rare input. Leave.

**Bot `Board` (4 methods, `bot/bot.go:45-51`) is minimal and correctly narrow** —
a strategy can read the position and cannot play a move. `freezeBoard`
(`room.go:1830`) is the right boundary: frozen on the room goroutine before the
worker exists, so `Used` cannot race a concurrent resign. Cost is O(chain) per
bot move → O(chain²) per bot game; chains are tens of words, so this is not
worth restructuring. It gets cheaper for free once `UsedWords()` lands. The
worker's leak guard (`room.go:1123-1129`, `select` on `r.ctx.Done()` around the
thinking delay) is correct. No change.

---

## 3. Dictionary

`Store` is load-once, read-only, never hands out a mutable reference
(`store.go:60-64`), so unsynchronized concurrent use is genuinely safe. The
`validate()` pass (`store.go:289`) checks declared-vs-loaded counts, orphan
meanings, out-degree consistency and dangling aliases — that is the right set,
and it is what makes a truncated file fail at boot rather than mid-game.

`NearMiss` (`store.go:421`) and its `stripped` index (`store.go:376`) are a
clean addition: built once at `Open`, ambiguity refused rather than guessed,
and `stripDiacritics` correctly folds `đ/Đ` by hand since they have no canonical
decomposition. Cost: one extra map plus one string per word. On a 30 k-word
corpus the `count != 1` rule will suppress most hits (common stems have many
candidates) — worth a metric on suggestion hit rate before tuning it.

**Builder version drift risk:** `dictionary/store.go:41`
`requiredBuilderVersion = "5"` and `cmd/build-dictionary/main.go:40`
`builderVer = "5"` are two constants in two packages that must agree, with no
compile-time link and (unlike the dump URL, per the `Makefile` comment) I found
no test asserting they do. Either have the builder import the store's constant
or add a one-line test. Size S.

**Community allowlist overlay — design only, no data.** Plug point is
`store.go`, between `loadAliases` and `buildStrippedIndex`: a `mergeOverlay()`
step, after which `buildStrippedIndex()` and `validate()` run over the *merged*
result unchanged. Shape:

- `Open(path string, opts ...Option)` with `WithOverlay(path)`. The overlay is a
  second database built by the **same** `build-dictionary --words` path, so
  there is one schema and one validator.
- Merge rules to settle before writing code: (a) an overlay word colliding with
  a canonical word is a no-op, not an error; (b) `outDegree` and `openers` must
  be recomputed after merge, not merged — merging them is what would break the
  `validate()` out-degree invariant; (c) a *suppression* list needs its own
  table, because removing a word can orphan an `opener` and lower an out-degree
  below `minOpeningOutDegree`.
- Licensing: the overlay is a **separate artifact with its own LICENSE and
  ATTRIBUTION**, copied as its own layer exactly the way `Dockerfile:74-77`
  already does for `data/`. The licence question is open and out of scope here;
  no word data should be committed to this repo either way.

---

## 4. Correctness and robustness sweep, ranked

**Confirmed (path traced)**

- **C1 · medium · ghost seat from an attach-after-teardown race.**
  `session.run` defers `leaveRoom()` (`session.go:280`), which sends
  `disconnectInput` only if `currentRoom()` is non-nil. A session that dies
  between `hub.createRoom`/`quickMatch` handing the room an input and the room
  goroutine draining it is never in a room at that moment, so no disconnect is
  ever sent — and then `handleCreate`/`handleJoin` calls `attach` on it
  (`room.go:578`, `room.go:715`), leaving a seat with a non-nil dead `sess`. No
  grace window opens (that needs `disconnectInput`), so `allConnected()` reports
  true. Worst case is quick match: `handleJoin`'s auto-start (`room.go:726`)
  gates on `allConnected()`, so a real player is auto-started into a game
  against a dead socket and waits out the 30 s turn clock. Fix: check
  `m.sess.ctx.Err() != nil` at the top of `handleCreate`/`handleJoin` and refuse
  (for join) or `r.cancel()` (for create). Bounded elsewhere: a PvP lobby closes
  on the 10-min idle timer, a bot room on the turn clock.
- **C2 · medium · per-IP join limiter is far too tight for shared egress.**
  `joinsPerSecond = 1, joinBurst = 5` (`session.go:50-51`), keyed on
  `clientIP` (`session.go:475`). Every player behind one NAT/CGNAT egress — a
  café, a school, a Vietnamese mobile carrier — shares five room joins and then
  one per second, refused with `too_many_attempts`. This is also the CI flake
  (§5). The limiter is right in kind (it protects the 6-char room-code secret
  from brute force at `31^6 ≈ 8.9e8`); the *rate* is wrong. At 5/s it still
  takes centuries to walk the space. Raise the burst and rate substantially, and
  make them configurable so the e2e suite and a CGNAT deployment can tune them.
- **C3 · low-medium · F7, games can start after the drain decision.**
  `beginGame` (`room.go:855`) has no `isDraining()` check, so `hub.gameStarted()`
  (`room.go:907`) can raise `liveGames` after `startDraining()`. `newRegisteredRoom`
  refuses *new rooms* (`hub.go:261`) but an existing lobby is untouched.
  Consequence is bounded by `NOITU_DRAIN_TIMEOUT` — the drain waits its full
  timeout instead of exiting early — and the game is then killed mid-play
  anyway, which is the outcome the drain existed to avoid. Fix: in
  `handleLobby`'s `lobbyStart` arm and in `handleJoin`'s auto-start, refuse with
  `server_restarting` when `r.hub.isDraining()`.
- **C4 · low · player-driven log volume.** `recordRejection` (`room.go:1021`)
  emits `slog.Info("word_rejected", …)` for *every* rejected submission, up to
  5/s per connection. The corpus feedback loop it serves is real, but
  `noitu_words_rejected` already carries the reason breakdown. Sample it, or
  drop the line to Debug and keep the counter. The word is normalized and capped
  before logging and no player identity is attached — that part is right.
- **C5 · low · silent unknown-payload handling.** No `default` in `dispatch`
  (`session.go:441-575`). A future or malformed `ClientMessage` costs a decode
  and gets no answer. Add the arm plus a counter; the frame limiter
  (`session.go:328`) already bounds the flood case.
- **C6 · low · `Config.IdleFor` is a dead knob in production.**
  `server.go:27` is documented as operator-facing but `cmd/noitu-server`
  never sets it (`main.go:88-98`) and no `NOITU_*` variable exists — `docs/`
  and `README` correctly do not claim one. Rooms are fixed at
  `defaultIdleWindow` (10 min, `room.go:83`). Either wire `NOITU_IDLE_TIMEOUT`
  and document it, or delete the field and let the constant be the only source.
- **C7 · low · `/debug/vars` publishes `cmdline` and `memstats`.**
  `expvar.Handler()` (`main.go:166`) always exposes the full argv and heap
  stats. It is opt-in and on a separate listener (`main.go:158-162`), which is
  the right design — but `docs/deployment.md` should say explicitly: never bind
  `NOITU_DEBUG_ADDR` to a public interface.
- **C8 · informational · resume token has no liveness check on the prior
  connection.** `handleResume` (`room.go:1448`) accepts the takeover even when
  `s.sess != nil`, then closes the old connection (`room.go:1471`). With the
  token in `localStorage`, a second tab evicts the first. That reads as a
  deliberate "one seat, latest tab wins" — record it as a decision rather than
  fix it. The token itself is 128 bits from `crypto/rand` (`session.go:742`) and
  is registered/expired correctly (`hub.go:122`), so it is not guessable.

**Plausible (not traced to a failure)**

- **C9 · `reserveCode` before the ceiling check.** `newRegisteredRoom`
  (`hub.go:268`) draws a code — up to 10 `crypto/rand` rounds and 10 lock
  acquisitions (`hub.go:332-341`) — before checking `maxRooms` at
  `hub.go:277`. At the ceiling every refused create pays that. Cheap reorder:
  an advisory count check first, keeping the authoritative one under the
  registration lock where it correctly is today.

**Checked and clean**

Trust boundaries: `sanitizeText` is applied to nicknames, chat **and** the
echoed `typed` word (`room.go:966`, `nickname.go:52`), with NFC first so the
rune cap sees what the browser renders — correct order. `clientIP`
(`server.go:242`) walks XFF from the right and only when the peer is a
configured proxy; malformed hops fall back to the socket address rather than
keying a limiter on garbage. Static file serving is root-confined
(`server.go:225` `underRoot`). Seat authority is always the seat, never the
claimed id (`room.go:849` `occupies`, used by every handler that acts on a
game). `liveGames` accounting is exactly-once via `CompareAndSwap` on both the
normal path (`room.go:1289`) and the cancelled-mid-game path (`room.go:392`).
Limiter maps are swept (`ratelimit.go:81`, `server.go:316`). `errorMsg` carries
UI keys, never prose or internal errors — asserted by
`TestErrorMessagesAreUIKeysNotProse`.

---

## 5. Tests

**Structure.** 13 files, 143 tests. `wsapi_test.go` alone is 2534 lines / 75
tests spanning ~9 topics; `hub_test.go` is 3 lines of comment and should be
deleted (fold the note into `hub.go`). Split `wsapi_test.go` along the §1
seams — `lobby_test.go`, `chat_test.go`, `resume_test.go`, `deadend_test.go`,
`report_test.go`, `ratelimit_test.go`, `nickname_test.go` (already exists) —
keeping `wsapi_test.go` as the harness only (`newTestServer`, client helpers,
fixture dictionaries, ~350 lines).

**The browser-suite join flake — answer: no, there is no server-side ordering
that makes `can_start` late.** Traced: `SetReady` → `toRoom`
(`session.go:603`) → `handleLobby` sets `mine.ready` (`room.go:764-766`) → the
run loop broadcasts exactly one `RoomState` in the *same* iteration
(`room.go:540-543`), with `can_start` computed in that same frame
(`room.go:1755`, `room.go:1671`). One RTT, no timer, no second input. The client
derives both `isReady` and `canStart` straight from that frame
(`game.svelte.js:537`, `Lobby.svelte:211`), so there is no local optimism to
drift either.

What can make the frame **missing** rather than late, in likelihood order:

1. **The join was refused and the test never noticed.** `playingPair`
   (`pvp-game.spec.js:74-78`) uses `joinRoom`, **not** `joinRoomSeated` — it
   clicks "Vào phòng" and goes straight to `readyAndStart` with no assertion
   that the seat exists. The suite runs `workers: 1` against one server process
   with every page on `127.0.0.1`, so **all pages share one join bucket**
   (C2: 1/s, burst 5). A test that joins twice in quick succession after a
   previous test drained the bucket gets `too_many_attempts`, the guest never
   reaches the lobby, the `ready` click is a no-op, and the failure surfaces
   10 s later on the owner's `start-game` — exactly the observed symptom at
   `helpers.js:130`.
2. `Lobby.svelte:211` also gates on `offline`; a socket blip keeps the button
   disabled independently of `can_start`.

Fixes, in order: raise/expose the join limiter (C2, which is a production fix
first and a test fix second); use `joinRoomSeated` in `playingPair`; and have
`readyAndStart` assert each guest's own ready state landed before waiting on the
owner's button, so a dropped `SetReady` fails at the guest with a clear message
instead of timing out on a third party.

**What to move from Playwright into Go.** The server-rule assertions in
`pvp-game.spec.js` already have named in-process equivalents —
`TestStartIsRefusedUntilTheGuestIsReady` (wsapi_test.go:1332),
`TestOnlyTheOwnerStartsAndKicks` (:1373), `TestKickFreesAnUnreadySeatOnly`
(:1515), `TestOwnerLeavingPromotesTheOtherPlayer` (:1548),
`TestResumeWithinGraceRestoresGame` (:607). The browser suite should keep only
what a browser proves: rendering, focus, the unread badge, deep links, the
bundle carrying no wordlist. Concrete task: walk `pvp-game.spec.js` and delete
every assertion whose rule is already named by one of those tests.

**Coverage floors.** Current: `vietnamese` 100, `game` 95.5, `bot` 91.2,
`wsapi` 91.1, `dictionary` 89.0, `build-dictionary` 88.6, **`cmd/noitu-server`
0.0**. Propose an 85% per-package floor in CI (`go test -coverprofile` + a
small check script), excluding `gen/`. That floor is already met everywhere
except the one package that has never been tested — `loadConfig`, `envInt`,
`envDuration`, `envNonNegDuration`, `envList` and `waitForGamesToFinish` are all
pure and all trivially testable, and config parsing is precisely where a
production misconfiguration hides.

**Fuzz.** Three targets today: `FuzzNormalize`, `FuzzSanitizeText`,
`FuzzDecode` — all at the right boundaries. The gap is the *composition*: add
`FuzzSubmit` over the fixture dictionary asserting the property that any input
`Engine.Submit` accepts resolves to a canonical word whose `FirstSyllable`
equals the link and whose `LastSyllable` becomes the next link. A second cheap
one: `FuzzStripDiacritics` for idempotence
(`strip(strip(x)) == strip(x)`), which is what `NearMiss` silently relies on.

---

## 6. Build and ops

- **Dockerfile version pins.** `Dockerfile:19` `golang:1.25-alpine` and
  `Dockerfile:37` `alpine:3.22` are exact-minor pins; the house rule prefers a
  moving major, and `node:24-alpine` (`:4`) and
  `distroless/static-debian12:nonroot` already follow it. As written, the
  dependabot `docker` ecosystem generates exactly the churn the rule exists to
  avoid. Change to `golang:1-alpine` and `alpine:3`, or record in the README why
  these two are pinned harder. `go.mod` says `go 1.25.0`, so the Go pin is
  currently *consistent* — just needlessly tight.
- **`make test-go` does not mirror CI.** It runs `go vet && go test -race`
  (`Makefile`), while CI additionally enforces `gofmt -l` and
  `golangci-lint` (`ci.yml`). A contributor can pass `make test` and fail CI.
  Add a `lint-go` target running both, and call it from `test-go`.
- **Duplicated vet.** `golangci-lint` runs `govet` by default, so the separate
  `go vet` step in `ci.yml` is redundant. It is ~free and it fails faster with a
  clearer message — leave it.
- **Everything else is clean.** The image job's licence assertion (`ci.yml`,
  "The licence travels with the data") is a genuinely good check, as is the
  negative assertion that the upstream dump never reaches the final image. The
  `proto.yml` breaking-change gate with `--against '.git#ref=origin/main'` and
  the generated-tree sync check (with `--intent-to-add` for newly emitted files)
  are both correct. `dependabot.yml` covers all four manifests with sensible
  grouping. The `build-dictionary` binary is built once in the `build` stage and
  copied into `dict` — no duplicated toolchain. Nothing stale found.

---

## Unresolved questions

1. **C2 rate.** What join rate and burst do you want? The security argument
   caps nothing below ~5/s; the availability argument (Vietnamese mobile CGNAT)
   wants much more. This is a product call, not a review call — I have not
   changed it.
2. **C8 two-tab takeover.** Is "second tab evicts the first" the intended
   behaviour? If yes it should be a line in `docs/`; if no it needs a liveness
   check in `handleResume`.
3. **C6 `IdleFor`.** Wire it as `NOITU_IDLE_TIMEOUT`, or delete the field?
4. **Coverage floor.** Is 85% the number you want, and should `cmd/` be held to
   it or exempted with a documented reason?
5. **Overlay (§3).** Does the suppression case (removing a shipped word) need to
   exist at all in v1? It is the only part of the overlay design that touches
   `openers` and `outDegree` and therefore the only part that is not additive.
