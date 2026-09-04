---
title: "Phase 7: Online 1v1 and Release"
status: todo
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
- [ ] Create a room → shareable 6-character code and a copyable invite link
- [ ] Join by code, or by opening an invite link with the code prefilled
- [ ] Waiting room until the opponent arrives; leaving cleans the room up
- [ ] Both players' nicknames shown in the waiting room and scoreboard (server-sanitized values only)
- [ ] Live opponent state: their turn, their timer, their disconnect and reconnect
- [ ] Resign, and rematch in the same room after a game ends
- [ ] Clear Vietnamese error states: room not found, room full, game already started

**Non-functional**
- [ ] E2E coverage of both modes with two real browser contexts
- [ ] CI: Go tests + race, JS tests, `buf lint`/`breaking`, generated-code drift check, builds
- [ ] Deployable artifact: container image with the binary, the static frontend, and `noitu.db`
- [ ] README documents setup, the license split, and deployment

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

- [ ] Two people on different machines play a full game via a shared room code
- [ ] Invite link opens straight into the room
- [ ] All three join errors show correct Vietnamese messages
- [ ] Disconnect shows the opponent a grace countdown; return inside it resumes with identical boards; expiry awards the win
- [ ] Rematch restarts in the same room with a new opening word
- [ ] Resign ends the game immediately with the right winner
- [ ] Playwright suite green: bot game, PvP game, reconnect
- [ ] CI green on all steps including the generated-code drift check, without ever downloading the 179 MB upstream DB
- [ ] `docker run` with `NOITU_DB_PATH` serves a playable game; the image contains `data/LICENSE` and `data/ATTRIBUTION.md` but not the 179 MB upstream file
- [ ] README license section and in-app attribution agree with `data/ATTRIBUTION.md`
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
