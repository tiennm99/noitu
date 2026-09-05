---
title: "Phase 7: Online 1v1 and Release"
status: done
phase: 7
priority: P1
effort: "4d"
dependencies: [5, 6]
---

# Phase 7: Online 1v1 and Release

## Overview

Complete the online 1v1 experience end to end in the browser — room codes, waiting room,
opponent presence, reconnect UX, rematch — then verify the whole game with browser E2E tests
and ship it: CI, container image, deployment, and documentation.

## Requirements

**Functional**
- [x] Create a room → shareable 6-character code and a copyable invite link
- [x] Join by code, or by opening an invite link with the code prefilled
- [x] Waiting room until the opponent arrives; leaving cleans the room up
- [x] Both players' nicknames shown in the waiting room and scoreboard (server-sanitized values only)
- [x] Live opponent state: their turn, their timer, their disconnect and reconnect
- [x] Resign, and rematch in the same room after a game ends
- [x] Clear Vietnamese error states: room not found, room full — a room whose game has started is full, so there is no third state

**Non-functional**
- [x] E2E coverage of both modes with two real browser contexts
- [ ] CI: Go tests + race, JS tests, `buf lint`/`breaking`, generated-code drift check, builds
- [x] Deployable artifact: container image with the binary, the static frontend, and `noitu.db`
- [x] README documents setup, the license split, and deployment

## Architecture

No new server concepts — phase 5 already implements rooms, codes, reconnect, and resign.
This phase is the frontend surface for them plus release engineering.

**Invite link:** `https://<host>/online?code=ABC123`. The `online` route reads the query
param, prefills, and auto-joins when the code is well-formed. Room codes use an unambiguous
alphabet (no `0/O/1/I`) and are displayed grouped for reading aloud.

**Online screen states**

```
idle ──create──► waiting (code shown, copy button)
  │                 └──opponent joins──► playing
  └──join(code)──► playing | error(room_not_found | room_full | already_started)

playing ──opponent disconnects──► banner "Đối thủ mất kết nối… (Ns)"
        ├─ they return  ──► resume playing
        └─ grace expires ──► GameOver(END_OPPONENT_LEFT)
```

**Rematch:** after `GameOver` in a PvP room, either player may request a rematch; the room
resets its engine with a fresh opening word when both accept, or closes on decline/timeout.
Requires a small additive protocol change — `RequestRematch` / `RematchState` — which is why
it lands here rather than in phase 4: additive tags only, per the phase-4 compatibility rule.

**Deployment — Docker image on a VPS behind a reverse proxy (validated decision):**

```
Dockerfile (multi-stage)
  node  → npm ci && npm run build                 → /web/build
  go    → CGO_ENABLED=0 go build ./cmd/...        → /noitu-server
  data  → curl the 179 MB dictionary.db           → build-dictionary → /data/noitu.db
  final → distroless/static + binary + /web + /data/noitu.db + /data/LICENSE + ATTRIBUTION
```

`CGO_ENABLED=0` works because the SQLite driver is `modernc.org/sqlite` (pure Go) — the
payoff for that phase-1 decision. The 179 MB upstream download happens **only in the data
builder stage**, so it never reaches the final image; `noitu.db` is copied in as its own
layer, keeping the CC BY-SA 4.0 artifact physically distinct from the Apache-2.0 binary.

**CI data strategy (consequence of not republishing a derived DB):** CI must never download
179 MB per run.

| Job | Data used |
|---|---|
| Unit tests (every push/PR) | Small fixture DB built in-test from a checked-in word sample (phase 2 helper) |
| E2E tests (every push/PR) | Same fixture DB, ~200 curated words — enough to script deterministic games |
| Full dictionary build (manual / nightly) | Real upstream, with the download cached by release tag |
| Docker image build (release only) | Real upstream, inside the builder stage |

## Related Code Files

- Modify: `proto/noitu/v1/game.proto` — add `RequestRematch`, `RematchState` (new tags only)
- Modify: `server/gen/...`, `web/src/lib/proto/...` — regenerate
- Modify: `server/internal/wsapi/room.go` — rematch handling, room reset
- Modify: `web/src/routes/online/+page.svelte` — replace the phase-6 stub with the full create/join/waiting/error flow
- Create: `web/src/lib/components/RoomCodePanel.svelte`, `WaitingRoom.svelte`, `OpponentStatus.svelte`, `RematchPrompt.svelte`
- Modify: `web/src/lib/stores/game.svelte.js` — room/opponent/rematch state
- Modify: `web/src/lib/i18n/vi.js` — online-mode strings
- Create: `e2e/bot-game.spec.js`, `e2e/pvp-game.spec.js`, `e2e/reconnect.spec.js` (Playwright)
- Create: `playwright.config.js`
- Create: `Dockerfile`, `.dockerignore`
- Create: `.github/workflows/ci.yml`
- Modify: `README.md` — quickstart, architecture, deployment, license split
- Create: `docs/deployment.md`

## Implementation Steps

1. Add `RequestRematch` / `RematchState` to the proto with new tags; regenerate both targets; extend the round-trip tests.
2. Server: rematch in `room.go` — both-accept resets the engine with a new opening word and re-emits `GameStarted`; decline or timeout closes the room. Cover with a room test.
3. `online/+page.svelte`: the state machine above, code input (uppercase, alphabet-restricted, paste-friendly), `?code=` prefill and auto-join.
4. `RoomCodePanel`: large grouped code, copy button, copy invite link, Web Share on mobile where available.
5. `WaitingRoom`: opponent-pending state with a cancel that leaves the room cleanly.
6. `OpponentStatus`: turn indicator, disconnect banner with the grace countdown, reconnect confirmation.
7. `RematchPrompt` on the game-over panel for PvP; bot mode keeps a plain "chơi lại".
8. Vietnamese strings for every new state, including the three join errors.
9. Playwright setup: build the frontend, run the real server against the **~200-word fixture DB** (never the real dictionary — CI must not download 179 MB), run specs against it.
10. E2E `bot-game.spec.js`: pick Hard, play scripted valid and invalid words, assert rejection messages, play to game over, assert the score and personal-best marker.
11. E2E `pvp-game.spec.js`: two browser contexts with different nicknames, create + join by code, assert each side shows the other's sanitized nickname, alternate turns, assert each side sees the other's word, resign ends it correctly.
12. E2E `reconnect.spec.js`: drop one context's socket mid-game, assert the opponent's disconnect banner, restore within grace, assert the board matches on both sides.
13. `Dockerfile` multi-stage as above; verify the image runs with only `NOITU_DB_PATH` set.
14. `ci.yml`: `go vet`, `go test ./... -race`, `npm test`, `npm run build`, `buf lint`, `buf breaking`, generated-code drift check, Playwright — all on the fixture DB. Separate release-only job builds the Docker image. Add an assertion that the built image contains `data/LICENSE` and `data/ATTRIBUTION.md`.
15. `README.md`: what the game is, quickstart (**`make fetch-dict` downloads 179 MB once**, then `make dict`), `make` targets, architecture diagram, **License** section (Apache-2.0 code / CC BY-SA 4.0 data with attribution), and a link to `data/ATTRIBUTION.md`.
16. `docs/deployment.md`: env vars (incl. `NOITU_TURN_LIMIT=20s`, `NOITU_ALLOWED_ORIGINS`), TLS/`wss://` behind a reverse proxy (`Upgrade`/`Connection` headers, proxy read timeout longer than the WS keepalive, buffering disabled), health check, and how `noitu.db` reaches the container.

## Success Criteria

- [x] Two independent browser contexts play a full game via a shared room code; two physical machines untested
- [x] Invite link opens straight into the room
- [x] Both join errors the protocol has show correct Vietnamese messages, plus a malformed code caught before it is sent
- [x] Disconnect shows the opponent a grace countdown; return inside it resumes with identical boards; expiry awards the win
- [x] Rematch restarts in the same room with a new opening word
- [x] Resign ends the game immediately with the right winner
- [x] Playwright suite green: bot game, PvP game, reconnect
- [ ] CI green on all steps including the generated-code drift check, without ever downloading the 179 MB upstream DB
- [x] `docker run` serves a playable game; the image contains `data/LICENSE`, `data/ATTRIBUTION.md` and `NOTICE` but no upstream file — verified against a fixture-dictionary build, not an upstream one
- [x] README license section and in-app attribution agree with `data/ATTRIBUTION.md`
- [ ] Full pass over `plan.md` success criteria — every box checkable

## Risk Assessment

| Risk | Signal | Response |
|---|---|---|
| Reverse proxy breaks WS upgrade in production | Works locally, 400/502 on deploy | `docs/deployment.md` states the required proxy headers and timeouts; the health check plus a post-deploy WS smoke test catch it before users do |
| Playwright PvP tests flake on timing | Intermittent CI red | Drive by deterministic server events (wait for the turn indicator), never fixed sleeps; run the suite with a long turn limit via env |
| Rematch protocol addition breaks phase-6 clients | Decode errors in an already-running client | Additive tags only; `buf breaking` in CI is the guard, and `protocol_version` lets the server reject a stale client cleanly |
| Room codes guessable enough to join a stranger's game | Reports of uninvited joins | `crypto/rand` codes plus per-IP join rate limiting from phase 5; 32^6 space with rate limiting makes scanning impractical |
| CC BY-SA obligations lost during packaging | Image ships without `data/LICENSE` | The Docker copy step includes the whole `data/` directory, and a CI assertion checks `data/LICENSE` and `data/ATTRIBUTION.md` exist in the built image |
| Scope creep into accounts/leaderboards at the finish line | New requests during release work | Explicit non-goals in `plan.md`; log them as post-v1 items instead |

## Phase 7 Outcome (2026-09-05)

Online 1v1 is playable end to end, the whole game is covered by a browser suite, and the
result ships as one 24 MB container image.

### What was verified, and how

| Claim | Evidence |
|---|---|
| Two players join by code and alternate turns | Two browser contexts against the real binary; each side sees the other's sanitized nickname and the other's words |
| An invite link opens straight into the room | The second context only opens a URL — no code typed, no button pressed |
| A rematch restarts the same room | One acceptance shows the other player a prompt; the second starts a new game with a fresh opening word and the room code unchanged |
| A disconnect is announced and survivable | Taking a player's page away raises the opponent's banner; returning inside the window restores the same position and clears it |
| An opponent who never returns forfeits | The grace window expires and the win is awarded, with the reason shown |
| The image runs and carries its obligations | `docker run` serves the app and a deep link; the image contains `data/LICENSE`, `data/ATTRIBUTION.md` and `NOTICE`, and no upstream database |
| The difficulty ladder is real | The simulation suite from phase 3 reports Hard beating Easy 94 games to 6, Medium beating Easy 93 to 7, and Hard beating Medium 78 to 22 |

21 browser tests, 139 JavaScript unit tests, and the Go suite under `-race` all pass.

### Deliberate design points

**The room now outlives its game, but only when there is someone to ask.** A finished game
opens a rematch offer when both seats still hold live connections. A bot room closes exactly
as before, because there is nothing to negotiate with a bot and keeping it alive would leak a
goroutine and an engine per finished game.

**There is no decline message.** Leaving is the decline, and the server already learns about
that from the socket closing. One message and one timeout cover every way a rematch does not
happen, which is a smaller protocol and one less state to get wrong.

**`turn_seq` no longer restarts at one.** A rematch reuses the same connections, so a
submission still in flight from the previous game could otherwise match a turn in the new one
and be applied to it.

**The fixture dictionary goes through the real builder.** `build-dictionary --words` reads a
checked-in list and runs it through the same filter, alias and write path production uses, so
a test database cannot drift into being shaped differently from what the server loads. Its
graph has exactly one syllable productive enough to open on, which is what makes a scripted
game deterministic without pinning the bot's replies.

### Defects found by building the browser suite

**A player who refreshed mid-game could not get back in.** The online screen waited for the
player to ask for a room before connecting — correct for someone who has just arrived and is
still typing their name, wrong for a tab that already holds a session. It now reconnects
immediately when a resume token is present.

**The opponent's disconnect banner never cleared.** The server restored the seat but told
nobody, so the other player watched "đối thủ mất kết nối" for someone already playing again.
A resume now announces itself to the opponent.

**A dead socket was invisible to the client.** Nothing noticed a connection that failed
without closing, so the page kept showing a live connection and a running countdown over a
socket nothing could reach. The client now treats silence longer than three ping intervals as
a dead socket and reconnects.

**The nickname typed on the online screen never reached the server.** The socket opened on
arrival and `Hello` carries the name once, so both players were introduced under whatever was
stored before they got there. Connecting when the player actually asks for a room fixed it.

### Two tools that did not work, and why

**Offline emulation does not cut a loopback socket.** A test that "went offline" kept playing
happily against the local server. Routing the WebSocket cuts it for real.

**A routed socket's close does not reliably reach the server.** The page observes it, but the
grace window never starts. That makes socket routing the right tool for what the client does
about a dead connection and the wrong one for what the server does about a missing player; the
latter tests take the page away instead. Both limitations are recorded in `e2e/socket-cut.js`
so the next person does not rediscover them.

### Deviations from the plan

- Playwright lives under `web/` rather than the repository root, so there is still one npm
  package rather than two.
- There are two join errors, not three: a room whose game has started is full, and the server
  has no separate code for it. A malformed code is caught in the client before it is sent.
- CI is split across two workflows. `proto.yml` keeps the wire contract, `ci.yml` owns the
  tests and the image, and the duplicated test steps were removed from the former.
- The image is built on every push against the fixture word list, not only on release. An
  image built only at release time is an image that breaks at release time, and the build
  argument that makes this cheap already existed for local testing.

### Defects found by review

Review reproduced three defects, two of them consequences of the new state this phase
introduced: a room that outlives its game.

**A game that ended on the turn clock never offered a rematch, and the button was still
there.** The offer was opened from the message arm of the room loop, so the most common
natural ending — running out of time — closed the room with nothing to accept. The player
pressed "chơi lại" and got an error. The end-of-game decision now sits after the whole select,
so every way a game can end reaches it, and the online game-over panel no longer shows a
rematch button at all: the prompt appears only while an offer is actually open, so it is the
only thing that can ask.

**A second connection presenting the same resume token disconnected the player who was still
there.** `resumeFrom` retired the old connection as soon as the room had *accepted the
message*, but the room decides asynchronously and refuses a resume into a finished game — which
is exactly the state during a rematch offer. A duplicated tab therefore ended the room. The
room now retires the old connection only once it has agreed to the swap. Before this phase the
window did not exist, so a refused resume was unreachable.

**Rooms leaked when one connection created several.** `attach` overwrote the session's room
pointer and nothing told the old room, which then parked in `select` forever holding a
goroutine and a room code. Rate limiting bounds the rate, not the total, and connections are
free, so this was unbounded on a public endpoint. `attach` now releases the room it is leaving.
This one predates the phase; it is fixed here because this is the phase that puts the server on
the internet.

Also fixed: `RequestRematch` had no rate limit despite being the only client message that fans
out to both players, so a burst could fill the opponent's outbox until the server closed their
session. A stale disconnect notice was being treated as a rematch decline, which needed
`handleDisconnect` to stop conflating "this notice applied" with "a game is still running".
The client's liveness check could fire on a throttled background tab and close a healthy
socket, so it now ignores a tick that was itself late. And a stale resume token opened the
lobby with an error the player did nothing to cause.

Each of the three defects has a regression test, and the two that were reachable through the
room loop were negative-tested by reverting the fix and watching them fail.

### Not verified

CI has not run on GitHub. Every step passes locally, including the image build and its licence
assertions, but the workflows themselves are unexercised.

The image was built from the fixture word list, not the 179 MB upstream release. The
downloading branch of the `Dockerfile` is the same shape, pinned by the same checksum the
`Makefile` uses, but it has not been run here.

Two players on two physical machines have not played. Two independent browser contexts have,
which exercises everything except the network between them.
