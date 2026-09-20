---
title: "Codebase health scan: noitu"
date: 2026-09-21
mode: codebase scan (not a PR review)
commit: dd3b463 (dev)
verdict: healthy; one silent feature regression, one availability gap, a handful of hygiene items
---

# Codebase health scan

Scope: whole repo at `dd3b463`. Generated trees (`server/gen`, `web/src/lib/proto`) not reviewed.
Prior findings from `code-review-260908-2302-codebase-cleanup.md` verified as fixed and not re-reported.

## Baseline (run here, ARM64 Linux)

| Check | Result |
|---|---|
| `go vet ./...` | clean |
| `go test ./... -race -cover` | all pass |
| coverage | vietnamese 100%, game 94.7%, bot 91.2%, wsapi 91.2%, build-dictionary 89.1%, dictionary 88.0%, `cmd/noitu-server` 0% |
| `npm run check` | 376 files, 0 errors, 0 warnings |
| `npm test` | 12 files, 190 tests, all pass |
| Playwright e2e | skipped (no browser on this host, per environment constraint) |

Code quality is above average for this size. Ownership between `game`, `wsapi` and the web store is
clean and deliberately documented; the engine is transport-free, the room owns the engine on one
goroutine, and the hub owns only registries. Comments explain *why* rather than restating code. The
findings below are gaps, not a pattern of carelessness.

---

# Confirmed findings (code path traced)

## C1 — `PlayedWord.player_id` is declared and consumed but never set. Chain attribution is dead in PvP

**Impact: high (silent feature loss in 3–4 player rooms). Size: S. Confidence: high.**

- `proto/noitu/v1/game.proto:170-173` declares `string player_id = 6` with the rationale "with four
  people at the table the chain also has to say whose the other words were".
- `server/internal/wsapi/convert.go:107-116` — `PlayedWord()` sets `Word, Typed, ByMe, Points,
  Syllables, Meanings`. **`PlayerId` is never assigned**, although `game.Move.Player` is right there
  in the argument (`server/internal/game/engine.go:235`).
- `web/src/lib/stores/game.svelte.js:324` reads `playerId: played.playerId` → always `undefined`.
- `web/src/lib/components/ChainHistory.svelte:90-91` guards on `game.nameOf(entry.playerId)`, which
  returns `''` for an absent id (`game.svelte.js:526-533`), so the byline never renders.

Net effect: in a 3- or 4-player room the chain shows *what* was played but never *who* played it —
exactly the gap `player_id` was added for in `4e2e9e1 feat(online)!: seat two to four players`.

Why both suites miss it — this is the AI-risk pattern worth naming:

- `server/internal/wsapi/convert_test.go:172-185` (`TestPlayedWordKeepsTypedInput`) asserts word,
  typed, by_me, points, syllables. Not player_id.
- `server/internal/wsapi/wire_test.go:90` hand-writes `PlayerId: "p2"` into the cross-language
  fixture, proving the *wire* can carry it.
- `web/tests/game-store.test.js:200` hand-writes `playerId: 'p1'` into the `played` fixture, proving
  the *store* maps it.

Three tests touch the field; none exercises the producer. Each side is green against a fixture the
other side never produces.

Fix sketch:

```go
// convert.go
func PlayedWord(m game.Move, byMe bool, meanings []dictionary.Sense) *noituv1.PlayedWord {
	return &noituv1.PlayedWord{
		Word: m.Word, Typed: m.Typed, ByMe: byMe,
		PlayerId:  string(m.Player),
		Points:    uint32(m.Points), Syllables: uint32(m.Syllables),
		Meanings:  Senses(meanings),
	}
}
```

Plus one assertion in `TestPlayedWordKeepsTypedInput`, and one end-to-end assertion in
`multiplayer_test.go` that a three-seat `turn_update` names the seat that played.

## C2 — No global room cap and no connection cap; the per-session limiter cannot bound either

**Impact: high (availability). Size: M. Confidence: high.**

- `server/internal/wsapi/server.go:73-91` — `handleWS` accepts every upgrade. Nothing counts
  connections, per IP or in total.
- `server/internal/wsapi/session.go:54-55` — `roomsPerSecond = 0.2, roomBurst = 5`, and that bucket
  is **per session** (`session.go:135`). It bounds one connection, not the process.
- `server/internal/wsapi/hub.go:140-154` — `newRegisteredRoom` has no ceiling on `len(h.rooms)`.

Each room is a goroutine, a 32-slot channel, an engine, up to three timers and a registry entry, held
for up to `defaultIdleWindow` (`room.go:67`, 10 minutes). N connections mint 5N rooms instantly and
0.2N/s thereafter. At 1 000 connections that is a 120 000-room steady state — each holding its
`chat` slice and engine — from a script, with no authentication anywhere in the protocol.

Fix sketch: a hub-level ceiling checked in `newRegisteredRoom` (return `errTooManyRooms` →
`room_start_failed`, or a new `server_busy` code), plus a per-IP concurrent-connection cap in
`handleWS` using the existing `keyedLimiter` shape. Both are cheap and neither changes the protocol.

## C3 — Behind the documented reverse proxy, every per-IP limiter is one global bucket

**Impact: high (availability), in the only supported deployment. Size: M. Confidence: high.**

- `server/internal/wsapi/server.go:150-162` — `clientIP` uses `RemoteAddr` only, deliberately
  ignoring `X-Forwarded-For`. The reasoning is correct and I am not proposing reversing it.
- `docs/deployment.md` ("The client's own address") states the consequence plainly: *"Behind a proxy
  every player therefore shares one bucket. If that becomes a problem, the fix is to make the proxy
  the only source of the header and teach the server to trust it."*
- `docs/deployment.md` ("Behind a reverse proxy") names the container-behind-a-proxy shape as **the**
  supported deployment.

So in the supported shape, `joinLimiter` (`session.go:49-50`, 1/s burst 5, keyed on the proxy's IP)
is a **single global limiter**. One client brute-forcing room codes spends the join budget for every
player on the server. That is a trivially reachable denial of service against a documented default.

The documented fix exists only as prose — there is no env var, no `Config` field, no code path for a
trusted-proxy mode. Fix sketch: add an opt-in `NOITU_TRUSTED_PROXY_HOPS` (default 0 = today's exact
behaviour) read in `loadConfig` (`cmd/noitu-server/main.go:109-118`) and threaded into `clientIP`, so
an operator who *has* made the proxy authoritative can say so. This adds the knob the doc already
names; it does not change the default or reverse the original decision.

## C4 — No per-connection message-rate ceiling; several dispatch arms are free

**Impact: medium (CPU exhaustion amplifier for C2). Size: S. Confidence: high.**

`server/internal/wsapi/session.go:399-493` rate-limits per message *type*, and three paths have no
budget at all:

- `session.go:489-490` — `ClientMessage_Ping` → `pongMsg` with no limiter. Self-limiting only because
  a full outbox closes the session (`session.go:202-208`), so it costs the attacker their socket.
- A `ClientMessage` with **no payload set** matches no `case`, returns `nil`, and sends nothing. It
  is completely free and can be replayed at line rate forever: one unmarshal + one dispatch per
  frame, per connection, indefinitely.
- `session.go:409-418` — `StartBotGame` checks `Difficulty(...)` **before** `roomLimiter.allow`, so
  an invalid difficulty is unlimited (it does cost an outbox slot, so it self-terminates).

`readLoop` (`session.go:289-305`) has no deadline and no frame counter. `maxFrameBytes` caps frame
*size* (`codec.go:195`) but not frame *rate*.

Fix sketch: one coarse `frameLimiter` bucket checked at the top of `dispatch`, generous enough that
no human hits it (say 30/s burst 60). Moving the `roomLimiter` check above the difficulty switch is a
one-line reorder.

## C5 — Raw player input crosses the trust boundary unsanitized as `PlayedWord.typed`

**Impact: medium (latent; not currently rendered to peers). Size: S. Confidence: high.**

- `server/internal/game/engine.go:236` — `Move{... Typed: raw ...}` stores the untrusted string.
- `server/internal/wsapi/convert.go:110` — `Typed: m.Typed`, and `room.go:943` builds this for
  **every** recipient, not just the submitter.
- `server/internal/vietnamese/normalize.go:37-49` — `Normalize` does NFC, lowercase and
  `strings.Fields`. It does **not** strip control or format characters, unlike `sanitizeText`
  (`nickname.go:413-441`) which chat and nicknames go through.

A word is accepted whenever its *normalized* form resolves, so `raw` may legitimately contain any
`unicode.IsSpace` rune (U+000B, U+000C, U+0085, U+2028 LINE SEPARATOR, NBSP) in unlimited quantity up
to the 4 KiB frame cap, and those bytes are broadcast verbatim to every seat.

Today nothing renders it: `ChainHistory.svelte:102` gates the correction line on `entry.byMe`, so
only the author sees their own input. The defect is that the boundary is inconsistent — the server's
own rule (`nickname.go:406-412`: "text that is safe to render in a stranger's browser") is applied to
two of three player-authored strings — and the guarantee rests on a client-side `{#if}` rather than
on the server. Any future UI that shows who typed what turns this into a live rendering bug.

Fix sketch: `Typed: sanitizeText(m.Typed, maxNicknameRunes, maxNicknameMarks)` in
`PlayedWord`, or clear `Typed` for non-authors (`if !byMe { typed = "" }`), which is closer to what
the field is actually for.

## C6 — The dictionary builder opens SQLite without the path escaping the store fixed

**Impact: low. Size: S. Confidence: high.**

`server/internal/dictionary/store.go:142-148` documents and fixes a real trap: SQLite reads `#` in a
`file:` URI as a fragment delimiter, so a bare path silently opens a different file. The builder
builds the same URIs by hand and skips it:

- `server/cmd/build-dictionary/main.go:228` — `sql.Open("sqlite", "file:"+path+"?mode=ro")`
- `server/cmd/build-dictionary/main.go:393` — `sql.Open("sqlite", "file:"+path)`

Any output path containing `#` (or `?`) writes and verifies a different file than the one named, then
`write` renames the *intended* path over nothing. Fix: export `dictionary.DSN(path)` (or duplicate
the three-line helper) and use it in both call sites. DRY violation with a concrete failure mode.

## C7 — `write` deletes the good database before the rename

**Impact: low. Size: S. Confidence: high.**

`server/cmd/build-dictionary/main.go:357-390`. The doc comment promises the rename is what makes the
build safe — "a failure partway through … leaves an empty but syntactically valid database where a
good one used to be". Then `main.go:381-383` runs `os.Remove(path)` *before* `os.Rename`, for a
Windows constraint. On POSIX that reintroduces exactly the window the comment rules out: an interrupt
between the two calls leaves no dictionary at all.

Fix: guard the pre-remove with `if runtime.GOOS == "windows"`, so POSIX gets the atomic replace the
comment describes. Optionally `f.Sync()` the temp file before renaming.

## C8 — Room exit paths other than "everybody left" leave sessions attached

**Impact: low. Size: S. Confidence: medium-high.**

`server/internal/wsapi/room.go:456-461` (idle close) and `room.go:400-401` (context cancelled) return
without calling `vacate` on the remaining seats, so `session.room` keeps pointing at a dead room
(`session.go:175-182` is the only release path). Consequences:

- The room struct — engine, `chat` slice, `outWire` map — is retained for the life of the connection.
- Error copy degrades: `toRoom` correctly answers `not_in_a_room` (`session.go:512-514`), but
  `handleSubmit` answers `busy` (`session.go:581-583`) for a room that is gone, not busy.

Fix: `for _, s := range r.seats { r.vacate(s) }` before those two returns.

---

# Plausible findings (worth a look, not traced to a failure)

## P1 — `Snapshot()` copies the whole history on every broadcast

`server/internal/game/engine.go:521-546` clones `history`, `scores`, `alive`, `outOrder` and rebuilds
`Standings()`; `room.go:910` calls it once per move, and `scoreRows` (`room.go:1098-1120`) allocates
per recipient. That is O(chain) per move, O(chain²) per game. At realistic chain lengths (tens of
words, ≤4 seats) this is noise — flagging it only because `broadcastTurn` is the hot path and the fix
is to pass the already-taken snapshot down rather than re-take it. **Size: S. Confidence: medium.**

## P2 — `readDump` aborts the whole build on a single malformed page

`server/cmd/build-dictionary/dump.go:128-130` returns a hard error when any page has no revision
text. Against a 61 MB monthly upstream that nobody pins, one bad page fails the entire dictionary
build (and therefore the release image job in `ci.yml:118-124`). A counted-and-skipped reject, with
the existing `minPages` floor (`main.go:106-109`) as the real guard, is more robust and loses
nothing. **Size: S. Confidence: medium** — this may be a deliberate fail-loud choice; the comment
does not say.

## P3 — One connection can hold two rooms for the grace window

`session.go:152-168` — `attach` releases the previous room with a `disconnectInput`, which starts a
grace window rather than vacating. A player who creates room A, starts a game, then joins room B
leaves A's opponents waiting out `NOITU_GRACE` for somebody who deliberately walked away. Probably
intended (it is the same code path as a refresh), and `TestOneConnectionCannotStrandRooms` proves
nothing leaks. Flagged as a product question, not a defect. **Size: S. Confidence: low.**

---

# Test-coverage gaps

| Behaviour | Where it lives | Covered? | Note |
|---|---|---|---|
| `PlayedWord.player_id` is populated by the server | `convert.go:107` | **No** | C1. Three fixtures hand-write the field; none produces it |
| Chain attribution reaches a 3rd/4th seat end-to-end | `room.go:929-946` | **No** | `multiplayer_test.go` checks turns and standings, not the chain byline |
| `cmd/noitu-server` config parsing | `main.go:109-157` | **No** | 0.0% coverage; `envDuration`/`envList` fallbacks are untested |
| Hub-level room ceiling | `hub.go:140` | n/a | No ceiling exists (C2) |
| Per-connection frame-rate ceiling | `session.go:399` | n/a | No ceiling exists (C4) |
| `PlayedWord.typed` sanitization | `convert.go:110` | **No** | `TestChatTextIsSanitizedAndCapped` and `TestOpponentNeverSeesAnUnsanitizedNickname` cover the other two strings |
| Fuzzing the untrusted-input boundary | `codec.go:215`, `nickname.go:413`, `normalize.go:37` | **No** | Zero `func Fuzz` in the repo. These three are ideal `testing.F` targets and would have surfaced C5 |
| SQLite DSN escaping in the builder | `build-dictionary/main.go:228,393` | **No** | `dictionary/store_test.go` covers the store's `dsn`; the builder's copies are untested |
| `write` interrupted between remove and rename | `build-dictionary/main.go:381` | **No** | C7 |
| Room close leaves no attached session | `room.go:456,400` | Partial | `TestIdleLobbyCloses` asserts the error and eviction, not seat release |

What *is* well covered and worth saying so, because it changes the risk calibration: 100 `wsapi`
tests including goroutine-baseline (`TestGoroutinesReturnToBaseline`), origin checking, oversize
frames, every rate limiter, seat authorization for strangers (`TestStrangerCannotSubmitForASeated
Player`), grace-window races, and exhaustive enum mapping tests. The error-code surface is closed:
all 33 `errorMsg` codes plus `room_idle_closed` have i18n entries (`web/src/lib/i18n/vi.js:204-239`).

---

# CI / build / dependency hygiene

| # | Item | Evidence | Size |
|---|---|---|---|
| H1 | CI does not run on the working branch | `.github/workflows/ci.yml:11-16` and `proto.yml:8-11` trigger on `push: branches: [main]` + `pull_request`. Active development is on `dev`, so pushes there are untested until a PR exists | S |
| H2 | No static analysis beyond `go vet` | `ci.yml:32-40`. The 260908 review ran `staticcheck` and `deadcode` by hand and they found real issues. Nothing keeps them green now | S |
| H3 | No `gofmt`/format gate | Absent from both workflows. Noted in the prior review as noisy under `core.autocrlf`; `gofmt -l` with `git config core.autocrlf input` in CI would still work | S |
| H4 | `buf` pinned to an exact version | `proto.yml:25-27` — `version: 1.69.0`. Repo convention prefers a moving major tag; `buf-setup-action` accepts a floating spec | S |
| H5 | No dependency-update automation | `.github/` contains only `workflows/`. No `dependabot.yml`, no renovate config. Given the "moving tags over pins" rule, a bot is the mechanism that rule assumes | S |
| H6 | Direct deps one minor behind | `go list -m -u all`: `golang.org/x/text v0.41.0 → v0.42.0`, `modernc.org/sqlite v1.58.0 → v1.59.0`. `coder/websocket v1.8.15` and `protobuf v1.36.12` are current. Not urgent | S |
| H7 | `allowScripts` not configured | `npm ci` warns: `esbuild@0.28.2 (postinstall) not yet covered by allowScripts`. Workspace convention (`/workspace/CLAUDE.md`, "Language Preferences") says install scripts are gated per package in `package.json#allowScripts`. `web/package.json` has no such block | S |
| H8 | No coverage floor | Coverage is measured only when asked for. A `-coverprofile` step with a floor would have made the `cmd/noitu-server` 0% visible | S |

Correctly done and worth not touching: the Dockerfile's three-stage split with the dump confined to
a builder stage, the CC BY-SA licence-travels-with-the-data assertion (`ci.yml:126-145`), the
`.dockerignore` dump exclusions, `buf breaking` against `origin/main`, and the `image` job depending
on `e2e` with a comment explaining why.

---

# Maintainability hot spots

| File | LOC | Note |
|---|---|---|
| `server/internal/wsapi/room.go` | 1609 | Room lifecycle, seating, lobby actions, chat store, engine bridging, per-recipient rendering and the frozen bot board in one file. All of it is genuinely room-goroutine state, so splitting by *concern* (`room_lobby.go`, `room_chat.go`, `room_broadcast.go`) preserves the ownership invariant while making the file navigable. Size: M |
| `server/internal/wsapi/wsapi_test.go` | 2292 | 68 tests in one file next to four focused test files. Same treatment: the names already cluster (chat, resume, lobby, limits) |
| `web/src/routes/online/+page.svelte` | 612 | Six interacting `$effect` blocks driving `resuming` / `needName` / `stalled` / `pending`, several using `untrack` to break cycles. This is the least inspectable code in the repo; the `untrack` calls are load-bearing, which is the signal. Extracting the join/resume state machine into a `.svelte.js` store — the way `bot-session.svelte.js` already does for the bot flow, for exactly the stated reason — would make it testable. Size: M |

No duplicated logic of consequence found. `vietnamese.Normalize` shared between builder and server is
the right call and its package doc says why. `bot.Board`/`frozenBoard` correctly copies engine state
rather than sharing it (`room.go:1570-1594`).

---

# Recommended actions, ranked

1. **C1** — set `PlayerId` in `PlayedWord`, assert it in `convert_test.go`, and add one multi-seat
   end-to-end assertion. A shipped feature is silently absent. (S)
2. **C3** — add the opt-in trusted-proxy config the deployment doc already promises, defaulting to
   today's behaviour. (M)
3. **C2** — hub-level room ceiling plus a per-IP concurrent-connection cap. (M)
4. **C4** — one coarse frame-rate bucket in `dispatch`; move the `roomLimiter` check above the
   difficulty switch. (S)
5. **C5** — sanitize or drop `PlayedWord.typed` for non-authors. (S)
6. **H1** — add `dev` to the CI push triggers, or `branches-ignore: []`. (S)
7. **H2/H3** — `staticcheck` and a format gate in the Go job. (S)
8. **C6/C7** — share the SQLite DSN helper; make the pre-rename remove Windows-only. (S)
9. Add `testing.F` targets for `Decode`, `sanitizeText` and `vietnamese.Normalize`. (S)
10. **H5/H7** — dependabot config; `allowScripts` for `esbuild`. (S)
11. **C8**, then the three maintainability splits when the files are next touched. (S/M)

---

# Unresolved questions

1. **C1** — was `player_id` ever wired up and later lost, or never implemented? `git log -S` shows
   the field arriving with `4e2e9e1` and no producer in any revision, which points at never. Worth
   confirming before assuming a regression in a later refactor.
2. **C3** — is the container ever run without a reverse proxy in front of it? If the supported shape
   is always proxied, the shared-bucket problem is not an edge case and should be ranked above C2.
3. **P2** — is the hard failure on a page with no revision text deliberate fail-loud, or an
   unconsidered path? The comment does not say, and that decides whether it is a fix or a non-issue.
4. **C5** — is `typed` intended to be visible to anyone but its author? If not, clearing it for
   non-authors is strictly better than sanitizing it, and also narrows the wire.
5. **H4** — is `buf 1.69.0` pinned because a newer release broke something? If so the reason belongs
   in a comment in `proto.yml`, per the repo's own version-pinning rule.
