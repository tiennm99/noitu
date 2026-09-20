---
title: "dev branch review: 13 commits, main..dev"
date: 2026-09-21
reviewer: code-reviewer
scope: git diff main..dev (93 files, +8215/-467)
baseline: plans/reports/code-reviewer-260921-0016-codebase-health-scan.md
verdict: APPROVE WITH CHANGES — two must-fix before ship, one of them a doc that makes a new security knob worse than its default
---

# Verdict

**APPROVE WITH CHANGES.** All eight confirmed health-scan findings (C1–C8) are closed, plus
H1–H8. The work is above the usual bar: comments explain intent, the new concurrency surfaces
(`liveCounted` CAS, hub cap under the registering lock, frame limiter before decode) are
reasoned about rather than sprinkled, and the drain default is `0s` so it changes nothing
until an operator opts in. 75 wsapi tests, 219 web tests, 3 fuzz targets.

Two findings block: **F1** (a peer can throw a Svelte runtime error in every other client's
tab) and **F2** (the documented nginx config makes the new trusted-proxy mode a limiter
bypass rather than a limiter fix). Neither is in a test's blind spot by accident — both are
cross-boundary, which is where this diff's seams are.

## Checks run here (ARM64 Linux, Go 1.27, node 24)

| Check | Result |
|---|---|
| `go vet ./...` | clean |
| `golangci-lint run ./...` | 0 issues |
| `go test ./... -race -count=1` | all pass (wsapi 29.4s) |
| `npm run lint` | 0 errors, 33 `jsdoc/reject-any-type` warnings |
| `npm run check` | 380 files, 0 errors, 0 warnings |
| `npm test` | 12 files, 219 tests, pass |
| binary smoke vs `data/fixture.db` | `/healthz` 200, `/readyz` 200, `/version` `dev`, `/debug/vars` 404 on public mux and 20 `noitu_*` keys on the debug mux, clean SIGTERM. Process stopped. |
| Playwright | skipped (no browser on this host) |

---

# Confirmed findings

## F1 — Chat `{#each}` key is not unique; two lines in the same millisecond throw at runtime

**Severity: high (remote-triggerable client crash). Size: S. Confidence: high.**

`web/src/lib/components/ChatPanel.svelte:166`

```svelte
{#each messages as entry (entry.playerId + "@" + entry.atMs)}
```

- `server/internal/wsapi/room.go:1518` stamps `at: time.Now()`; the wire carries
  `sent_unix_ms` (`proto/noitu/v1/game.proto`, `ChatMessage.sent_unix_ms`), millisecond
  resolution.
- `session.go:47-48` — `chatsPerSecond = 2.0, chatBurst = 5`. Five lines go out back to back,
  and the room handles them serially in memory with no I/O between.
- `web/src/lib/stores/game.svelte.js:442-450` stores only `playerId` and `atMs`. The room's
  own `chatEntry.seq` (`room.go:232`) — which *is* unique — never reaches the wire.

Failure: one player sends two lines that land in the same millisecond → every recipient's
`(playerId, atMs)` key collides → Svelte's documented runtime error `each_key_duplicate`
("Keyed each block has duplicate key at indexes %a% and %b%") — a runtime error, not a
dev-only warning, so the chat panel and whatever unwinds above it break in a shipped build.
Reachable accidentally by a fast double-Enter and deliberately by a scripted client.

Fix (no proto change): stamp each store entry with `n: state.chatCount` before the increment
(and the index in the `chatHistory` arm), then key on `entry.n`. Or drop the key — the list
is append-and-trim, which an unkeyed `each` handles correctly. Test: two `SendChat` frames
back to back in `web/tests/game-store.test.js`.

## F2 — The documented nginx config turns `NOITU_TRUSTED_PROXIES` into a limiter bypass

**Severity: high (availability, in the deployment the doc tells you to build). Size: S (doc).
Confidence: high.**

`docs/deployment.md:110-125` (nginx snippet) and `docs/deployment.md:151`.

The snippet sets `Upgrade`, `Connection`, `Host` and timeouts. It does **not** set
`X-Forwarded-For`. Line 151 then asserts: *"Both proxies above append the real client to
`X-Forwarded-For` by default."* That is true for Caddy and false for nginx — nginx adds no
XFF unless you write `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;`, which is
precisely why that idiom exists. What nginx *does* do is pass the client's own
`X-Forwarded-For` header through untouched.

Failure: operator follows the doc, sets `NOITU_TRUSTED_PROXIES=127.0.0.1` as the same doc
instructs. `server.go:243-262` now trusts the peer, walks the header from the right, and
takes the attacker's forged value as the key. The attacker rotates it per frame and the
`joinLimiter` (1/s, burst 5) becomes unlimited — strictly worse than the shared-bucket
behaviour C3 set out to fix, and the operator has no signal that it happened.

Fix: add to both `location` blocks and correct line 151.

```nginx
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
```

Consider a startup `slog.Warn` when `NOITU_TRUSTED_PROXIES` is non-empty and the first
request from a trusted peer carries no XFF at all — that is the misconfiguration signature,
and it is cheap to notice once.

## F3 — A quick-match waiter can be seated after its own teardown, starting a game against a ghost

**Severity: medium-high. Size: S. Confidence: high (code-traced; not covered by
`TestQuickMatchDropsADisconnectedWaiter`, which `settle()`s first).**

`server/internal/wsapi/hub.go:188-208`

```go
waiter := h.waiting[0]
h.waiting = h.waiting[1:]
h.mu.Unlock()
…
r.send(createInput{sess: waiter, autoStart: true})
```

Interleaving: the waiter's socket dies exactly here. `session.run`'s teardown order is
`cancelQuickMatch` → `leaveRoom` → `close` (`session.go:280-284`). `cancelQuickMatch` finds
nothing (already dequeued), `leaveRoom` finds no room (not yet attached), and *then*
`handleCreate` (`room.go:573-588`) seats the dead session and `attach`es it. The room never
receives a `disconnectInput`, so `allConnected()` is true, `autoStart` fires
(`room.go:725-733`), and the live player is put in a game against a socket that is gone —
burning a full `NOITU_TURN_LIMIT` per ghost turn until the engine eliminates it. `send` is
safe (`session.go:224-237` drops on `ctx.Done`), so nothing panics; it is a stuck game, not a
crash.

Fix: loop the dequeue past any waiter whose `ctx.Err() != nil` (falling through to the
enqueue branch if the queue empties), **and** in `handleCreate`/`handleJoin` follow `attach`
with `if m.sess.ctx.Err() != nil { r.send(disconnectInput{…}) }` — the second half is what
closes the residual window rather than narrowing it.

## F4 — `NearMiss` can suggest a word the player cannot legally play

**Severity: medium (the feature misfires in the case it exists for). Size: S. Confidence: high.**

`server/internal/wsapi/room.go:992-1005`, `server/internal/game/engine.go:212-232`.

`Submit` returns `ReasonNotInDictionary` **before** the link check (`first != e.current`) and
before the already-used check. `nearMissFor` fires on exactly that reason, so the suggestion
is drawn from the whole corpus with no regard for the syllable in play; `WordInput.svelte:160-166`
then fills the field with it on click. Failure: player mistypes a real word that does not
link from the current syllable (or is already in the chain) → suggested → filled → submitted
→ rejected again, now for `wrong_link`. The "did you mean" is the one place a player is
entitled to trust the server's vocabulary, and it hands back a word that was never playable.

Fix in `nearMissFor`, which already holds `r.dict` and `r.engine`:

```go
if first, ok := r.dict.FirstSyllable(suggestion); !ok || first != r.engine.Current() {
    return ""
}
```

(`game.Dictionary` is embedded in the wsapi `Dictionary` interface, so `FirstSyllable` is in
hand.) Filtering already-used words needs one engine accessor; the link filter alone removes
the large majority of the misfires.

## F5 — A waiter refused for `server_full` is dropped server-side but still "searching" on screen

**Severity: medium. Size: S. Confidence: high (the server behaviour is asserted by
`TestQuickMatchServerFullTellsBothSides`; the client half is not).**

`hub.go:194-199` sends the waiter `roomCreateError(...)` and nothing else. The web store
clears `state.queued` only on `roomState` (`game.svelte.js:292`) or `quickMatchStatus`
(`:489`), so the waiting panel (`online/+page.svelte:482-495`) keeps counting up beside a
`server_full` banner for somebody the queue no longer contains. The cancel button still
works, so it is a lie rather than a trap — but the elapsed counter and the bot nudge are both
now wrong.

Fix (server, one line, and it keeps the client rule "queued is server-owned"):

```go
waiter.send(quickMatchStatusMsg(false))
waiter.send(roomCreateError(waiter.id, err))
```

## F6 — Connection cap is global only; one IP can take every slot, and each socket mints 20 log lines

**Severity: medium. Size: M. Confidence: high.**

`server/internal/wsapi/server.go:146-152` caps total sockets at `NOITU_MAX_CONNECTIONS`
(2000). There is no per-IP concurrent cap, which is the half of health-scan C2 that is still
open. One host opening 2000 sockets locks every real player out at the upgrade, and nothing
in the process says who did it (`noitu_connections_open` is a single number).

Amplifier: `session.handleReportWord` (`session.go:689-728`) allows 20 distinct words per
*session*, each producing a `word_reported` Info line, and the budget resets on every new
socket. Reconnect-in-a-loop is therefore a log-volume vector gated only by the WS handshake
cost.

Fix: a `keyedLimiter`-shaped per-IP concurrent-connection counter in `handleWS` (the
`keyedLimiter` + sweeper already exist), say 10–20 per IP, keyed on `s.clientIP(r)` so it
inherits F2's fix. Per-IP is the right granularity here even behind a proxy, once XFF is
actually trustworthy.

## F7 — A game can start *during* a drain, extending the wait the drain is bounded by

**Severity: medium-low (opt-in path only; default `0s` is unaffected). Size: S.
Confidence: high.**

`hub.startDraining` (`hub.go:293`) refuses **new rooms** (`newRegisteredRoom:251-253`).
Rooms that already exist keep their lobby, so `handleLobby`'s start action and the quick-match
`autoStart` both reach `beginGame` (`room.go:904-906`) and call `hub.gameStarted()` after
drain began. A room that rematches keeps doing so. `waitForGamesToFinish`
(`main.go:170-193`) therefore holds the deploy for the full `NOITU_DRAIN_TIMEOUT` rather than
for the games that existed when the signal arrived.

The `/readyz` half is correct: both the readiness handler and the room refusal read the same
`atomic.Bool`, so there is no window where one has flipped and the other has not.

Fix: refuse `beginGame` while draining (`if r.hub.isDraining() { broadcastError("server_restarting"); return }`),
or snapshot the live-game count at drain start and wait only for it to reach zero. The first
matches the intent stated in the drain comment.

## F8 — `gamesFinished` is not incremented on the cancelled-mid-game path

**Severity: low (metrics only; the gauge is correct). Size: S. Confidence: high.**

`room.go:904` increments `gamesStarted`; `room.go:1281` increments `gamesFinished` only in
`broadcastGameOver`. The run-teardown CAS (`room.go:394-398`) correctly repairs
`hub.liveGames` for a room cancelled mid-game but does not touch the counter. After every
drain-with-timeout or shutdown, `noitu_games_started` exceeds `noitu_games_finished`
permanently, which is the exact signal an operator would read as "games are hanging".

Fix: move `metrics.gamesFinished.Add(r.mode, 1)` inside the CAS, and call the same helper
from `broadcastGameOver`.

## F9 — `write`'s rename fallback treats every error as the Windows case and deletes the good database

**Severity: low. Size: S. Confidence: high.**

`server/cmd/build-dictionary/main.go:384-390`. C7's fix is right in substance — POSIX now
gets the atomic replace the comment promises — but the retry is keyed on *any* `os.Rename`
error, while the comment says "Windows refuses to rename over an existing file". An `EXDEV`
or `EACCES` rename failure now removes the existing dictionary and then fails the retry,
which is the state C7 set out to make impossible.

Fix: `if errors.Is(err, fs.ErrExist) || runtime.GOOS == "windows"` before the destructive
branch, and return the original rename error otherwise.

## F10 — Limiter keys are not normalized for IPv4-mapped IPv6

**Severity: low. Size: XS. Confidence: high.**

`server.go:236-262`: `trusted()` calls `Unmap()` before matching, but the value *returned* as
the key is the raw string. `::ffff:203.0.113.5` and `203.0.113.5` are therefore two buckets
for one client, reachable when a dual-stack listener and a proxy-supplied header disagree on
form. Fix: parse once and return `addr.Unmap().String()` on both paths.

## F11 — `cancelQueue` ignores whether the cancel actually went out

`web/src/routes/online/+page.svelte:319-322` — `send(cancelQuickMatch())` returns a boolean
the caller drops. With a closed socket the server has already dequeued the player (teardown),
but the panel keeps saying "searching". Same class as F5; fixing F5 server-side does not
cover this one. Low.

---

# Plausible (not traced to a failure)

- **P1** `clientIP` splits every `X-Forwarded-For` value on every upgrade with no hop
  ceiling. Go's 1 MB header limit allows ~100k entries, so it is a small per-handshake CPU
  and allocation cost an attacker controls. Cap at, say, 20 hops.
- **P2** The suggestion/report buttons survive a turn change that had no played word:
  `turnUpdate` clears `rejection` only `if (played)` (`game.svelte.js:381-385`), so after a
  timeout the previous rejection panel — and its fill-the-field button — stays live off-turn.
  Harmless (`guardInput` and the submit path both refuse), but it is stale UI the store
  otherwise avoids.
- **P3** `handleReportWord` spends the *chat* budget (`session.go:690`). Defensible (both are
  "player typed something at the room"), but undocumented, so a player who reports a word
  silently loses chat capacity. One comment would settle it.
- **P4** `/debug/vars` carries expvar's default `cmdline` and `memstats`. Verified 404 on the
  public mux, and `docs/deployment.md:169` already says to bind it away from players — so
  this is a non-issue as documented, recorded here only so the next reader does not re-derive
  it.
- **Checked and fine:** the `NearMiss` stripped index costs one map entry per canonical word
  (~30k floor, `--min-words 30000`) ≈ single-digit MB — not a memory concern. `capParts`
  holds the sum invariant by construction (`pointsFor` re-sums the *capped* parts), every
  term is non-negative, and `base+chain ≤ 40 < maxPointsPerWord`, so the trim loop can never
  underflow into base. `đ/Đ` folding and NFD mark-dropping are correct for Vietnamese; the
  ambiguity rule (`count != 1`) does what its comment says. The `Normalize` double-NFC fix is
  correct and its fuzz seed is committed.

---

# Health-scan closure (C1–C8)

| # | Status | Evidence |
|---|---|---|
| C1 `player_id` never set | **Closed** | `convert.go:146` sets `PlayerId: string(m.Player)`; `limits_test.go:17` `TestPlayedWordNamesItsPlayerOnTheWire` asserts it end-to-end |
| C2 no room / connection cap | **Closed with a gap** | `hub.go:264-271` checks `len(h.rooms) >= h.maxRooms` under the registering lock (correct against the race); `server.go:146` caps total sockets. **Per-IP connection cap still absent — see F6** |
| C3 proxy = one global bucket | **Closed, but see F2** | `Config.TrustedProxies`, `clientIP` walk-from-right, `NOITU_TRUSTED_PROXIES` default empty = old behaviour. `TestClientIPTrustsOnlyConfiguredProxies` covers it. The *documented* nginx config defeats it |
| C4 no frame-rate ceiling | **Closed** | `session.go:328-331` charges `frameLimiter` (20/s, burst 40) before `Decode`, so the no-payload and `Ping` arms are covered; `StartBotGame` limiter moved above the difficulty switch (`session.go:447-451`); `TestFrameFloodClosesTheConnection` |
| C5 raw `typed` crosses the boundary | **Closed** | `room.go:963` sanitizes before `engine.Submit`, so `Move.Typed` is filtered at the source rather than at the renderer; `TestTypedWordIsSanitizedBeforeItIsEchoed` |
| C6 builder DSN not escaped | **Closed** | `dictionary.DSN(path, readOnly)` exported and used at `build-dictionary/main.go:229,398` |
| C7 remove-before-rename | **Closed, imprecisely** | `main.go:384-390` renames first; see **F9** for the over-broad fallback |
| C8 exits leave sessions attached | **Closed** | `defer r.detachAll()` in `run` (`room.go:403`, `detachAll` at `:1720`) covers every exit, not just the two named paths; `TestIdleRoomReleasesItsSeats` |

Hygiene H1–H8 also closed: `dev` on both workflow triggers, `gofmt -l` gate,
`golangci-lint-action@v9`, `buf` moved to `latest`, `.github/dependabot.yml` (4 ecosystems),
`web/package.json#allowScripts` for `esbuild`, `npm run lint` in CI. Three `testing.F` targets
exist (`FuzzDecode`, `FuzzSanitizeText`, `FuzzNormalize`). No coverage floor (H8) — the only
one left, and it was the weakest item on the list.

---

# Merge check (conflict-resolved files)

| File | Result |
|---|---|
| `web/src/lib/ws/messages.js` | **Clean.** 10 exports, no duplicates; the four new builders (`quickMatch`, `cancelQuickMatch`, `claimDeadEnd`, `reportWord`) each have a consumer — verified reachable from `online/+page.svelte` and, for claim/report, `play/+page.svelte` too. Nothing orphaned |
| `web/src/routes/online/+page.svelte` | **Clean.** No duplicated `$effect`, handler or markup block; the only repeated statements are `flush(connection.status === Status.OPEN)` (in `request` and in the resume-failure effect) and `request({ kind: 'join', code })` (in `join` and the invite-link effect), both intentional. Page-leave teardown sends `cancelQuickMatch` *and* `disconnect()`, so the queue is released either way |
| `server/internal/wsapi/wsapi_test.go` | **Clean.** 75 test funcs, no duplicate names across the 9 wsapi test files, no duplicated helper definitions, compiles and passes under `-race` |

---

# Docs vs code

- **Env vars: all 11 present in both `README.md` and `docs/deployment.md`, and every default
  matches `loadConfig`** (`MAX_ROOMS` 1000, `MAX_CONNECTIONS` 2000, `DRAIN_TIMEOUT` 0s — the
  code passes 0 and the package constant supplies the documented number, which the docs state
  as the effective default, correctly).
- **Endpoints: `/ws`, `/healthz`, `/readyz`, `/version` documented and verified live.
  `/debug/vars` documented as debug-mux-only and verified 404 on the public mux.** No
  documented endpoint is missing and no undocumented one exists.
- **Mismatch (F2):** `docs/deployment.md:151` claims the nginx snippet at `:110` appends
  `X-Forwarded-For`. It does not, and the consequence is a limiter bypass rather than a
  cosmetic error.
- `README.md` "The word field is deliberately uncontrolled" still holds: `useSuggestion`
  (`WordInput.svelte:160-166`) writes `field.value` directly on an explicit click and adds no
  `bind:value`, which is the invariant, not a violation of it. The comment even says why the
  click is the safe moment. No change needed.
- README's rules-page claims check out: `/rules` exists, is linked from the landing page, the
  board header (`GameBoard.svelte`) and the lobby, and is a link rather than a button.

---

# Recommended actions, ranked

1. **F1** — unique chat key. Blocking.
2. **F2** — nginx `proxy_set_header X-Forwarded-For`, and fix the claim at `:151`. Blocking;
   ship it with, or before, the `NOITU_TRUSTED_PROXIES` announcement.
3. **F3** — skip dead waiters at dequeue + post-attach liveness check.
4. **F4** — filter `NearMiss` by the current link.
5. **F5/F11** — clear `queued` on the refusal path.
6. **F6** — per-IP concurrent-connection cap (finishes C2).
7. **F7** — refuse `beginGame` while draining.
8. **F8/F9/F10** — counter symmetry, Windows-only pre-remove, unmapped IP keys.
9. P1–P3 when the surrounding code is next touched.

---

# Unresolved questions

1. **F4** — is a suggestion that is real-but-unplayable *intended* ("this word exists, just
   not here")? The comment on `nearMissFor` argues only about spelling vs. vocabulary and
   does not address the link, so I read it as an oversight — but it is a product call and the
   i18n copy (`suggestionPrompt`) is phrased as a correction, not as trivia.
2. **F7** — should a drain also stop *rematches* in rooms that are mid-session, or only new
   first games? Stopping rematches is what bounds the wait; not stopping them is friendlier to
   the players already there. The drain comment argues for the deploy, which suggests the
   former.
3. **F6** — what is the expected legitimate concurrency per IP? A shared NAT (a school, a
   café) behind the new trusted-proxy mode could plausibly want 20+. The number decides
   whether a per-IP cap is safe to add at all, and it is not in the docs.
4. **Protocol version** — `PROTOCOL_VERSION` stays 2 through four new client messages and
   three new server fields. Correct as analysed (old clients ignore unknown server payloads,
   and the server only emits the new ones in reply to new client ones), but confirm that a
   cached SPA silently lacking quick-match is the intended degradation rather than something
   the handshake should refuse.
5. **F9** — was the "any rename error" fallback deliberate defensiveness, or is Windows the
   only case in mind? The comment says Windows; the code says anything.
