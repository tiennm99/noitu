---
title: "Noi Tu Web Game"
description: "Vietnamese nối từ web game — SvelteKit frontend, Go backend, WebSocket + Protobuf, server-authoritative dictionary over SQLite. Vs-bot and online 1v1."
status: complete
priority: P1
effort: "~3-4w"
tags: [game, sveltekit, go, websocket, protobuf, sqlite, vietnamese]
created: 2026-09-04
blockedBy: []
blocks: []
---

# Noi Tu Web Game

## Overview

Web implementation of **nối từ**, the Vietnamese word-chain game: a player submits a
meaningful word of **at least 2 syllables** whose **first syllable equals the previous
word's last syllable**. No reuse, timed turns. Loss on timeout, invalid word, wrong link,
repeat, or no legal move remaining.

Two modes ship in v1: **vs bot** (3 difficulties) and **online 1v1**. Both run through the
same server-authoritative engine — the browser never holds the wordlist, so validation
cannot be bypassed and the bot and PvP paths share one code path.

**Stack (user-selected):** SvelteKit (JavaScript) frontend · Go backend · WebSocket
transport with Protobuf framing · dictionary served from SQLite server-side.

**Research basis:** [`plans/reports/research-260904-1058-noi-tu-game.md`](../reports/research-260904-1058-noi-tu-game.md)

## Goals

| # | Goal | Priority |
|---|------|----------|
| 1 | Correct, server-authoritative nối từ rules incl. Vietnamese text normalization | P1 |
| 2 | Reproducible dictionary pipeline from `minhqnd/dictionary` with correct CC BY-SA 4.0 compliance | P1 |
| 3 | Typed WS protocol (Protobuf) shared by Go and JS, single source of truth | P1 |
| 4 | Playable vs-bot mode with 3 difficulty levels | P1 |
| 5 | Online 1v1 with nicknames, room codes, turn timer, reconnect | P1 |
| 6 | Vietnamese UI, dark mode, local high score | P2 |
| 7 | Single deployable Go binary, shipped as a Docker image | P2 |

## Non-goals (v1)

- Word definitions / meanings display (explicitly out of scope per user selection)
- Accounts, auth, persistent profiles, server-side leaderboards
- 4-player *đấu trường* mode
- Native mobile apps
- Player-submitted word additions / dictionary moderation UI
- Redistributing our derived dictionary as a downloadable artifact (see Data Distribution)

## Architecture

```
┌──────────────────────────────┐         ┌────────────────────────────────────┐
│  SvelteKit SPA (adapter-      │  WSS    │  Go server (single binary)         │
│  static, Svelte 5 runes)      │ ◄─────► │                                    │
│                               │ Protobuf│  wsapi ── hub ── room(s)           │
│  lib/ws     connection+codec  │ binary  │            │                       │
│  lib/proto  protobuf-es gen   │ frames  │            ├── game.Engine         │
│  lib/stores game state        │         │            └── bot.Bot             │
│  routes/    UI (vi, dark mode)│         │                    │               │
└──────────────────────────────┘         │  dictionary.Store ──┘               │
   localStorage: nickname, high score,   │      │ read-only SQL                │
                 theme                    │  data/noitu.db  (CC BY-SA 4.0)     │
                                          └────────────────────────────────────┘
                                                       ▲ built locally by
                                   server/cmd/build-dictionary (Go)
                                                       ▲ reads
                                   dictionary.db (179 MB, downloaded from
                                   minhqnd/dictionary release v2.0.0)
```

**Key decisions**

| Decision | Choice | Rationale |
|---|---|---|
| Authority | Server validates every move; client is a view | User chose server-side dictionary; also the only way PvP is cheat-resistant |
| Word length | **≥ 2 syllables**, link on first↔last syllable | User decision. Closer to casual play; keeps 3+ syllable compounds in the corpus |
| Bot location | Server-side, same engine as PvP | One rule implementation, no duplication (DRY); bot "thinking delay" is server-timed |
| Turn limit | **20s**, identical for bot and PvP | One constant, one code path; `NOITU_TURN_LIMIT` overrides |
| Player identity | **User-typed nickname**, no accounts | User decision. Stored in `localStorage`, sanitized server-side |
| WS library (Go) | `github.com/coder/websocket` v1.8.15 (ISC) | `gorilla/websocket` archived 2022 and panics on concurrent writes; coder/websocket is context-aware and handles concurrent writes |
| Protobuf (JS) | `@bufbuild/protobuf` v2.14.1 + `protoc-gen-es` | Only fully conformant JS impl, ESM/tree-shakeable, small browser bundle |
| Protobuf (Go) | `protoc-gen-go` via same `buf.gen.yaml` | One schema, two targets, generated in CI |
| Go module path | `github.com/tiennm99dev/noitu/server` | Matches the actual git remote |
| SQLite driver | `modernc.org/sqlite` (CGo-free) | Keeps `CGO_ENABLED=0` cross-compilation and a distroless image; read-only lookups are not CGo-bound |
| Dictionary DB | Loaded at runtime from `data/noitu.db`, **not** `go:embed` | Keeps CC BY-SA 4.0 data a separate artifact from Apache-2.0 code — cleanest license boundary |
| Tone variants | Alias table built offline (`hoà`→`hòa`) | Precomputed lookup beats a fragile runtime tone-placement algorithm (KISS) |
| Room state | In-memory, no DB | v1 has no persistence requirement; restart drops live games (accepted, documented) |
| Deployment | Docker image on a VPS behind a reverse proxy | Most portable; phase 7 ships a distroless image and proxy docs |

## Data Distribution

**Decision (validated):** we do **not** republish a derived dictionary. Every build downloads
`dictionary.db` from the upstream release and derives `data/noitu.db` locally.

Upstream asset: [`minhqnd/dictionary` release v2.0.0 → `dictionary.db`](https://github.com/minhqnd/dictionary/releases/download/v2.0.0/dictionary.db) — **179 MB**.

Consequences, all handled explicitly:

| Consequence | Handling |
|---|---|
| 179 MB is too large to fetch on every CI run | CI runs tests against a **small fixture DB** built from a checked-in word sample (phase 2). Only a manually-triggered/nightly job downloads the real upstream and builds the full DB |
| Docker build needs the upstream DB | Downloaded in the **builder stage** only; the final image carries just the derived `noitu.db`, so the 179 MB never ships |
| `data/noitu.db` is a local build artifact | Git-ignored. `make dict` is a documented prerequisite for running the real server |
| The derived DB *is* distributed inside our Docker image | CC BY-SA 4.0 still applies to that image layer — attribution files ship with it, asserted in CI (phase 7) |

## Licensing (mandatory — CC BY-SA 4.0 share-alike)

`minhqnd/dictionary` is **dual-licensed**: MIT for application code, **CC BY-SA 4.0 for the
dictionary data**. We consume only the data, so share-alike applies to our derived wordlist
wherever we distribute it (notably the Docker image).

| Artifact | License | File |
|---|---|---|
| All source code in this repo | Apache-2.0 (existing) | `LICENSE` |
| Derived dictionary `data/noitu.db` and the build script's output | **CC BY-SA 4.0** | `data/LICENSE` |
| Attribution + list of modifications | — | `data/ATTRIBUTION.md` |
| Root pointer to the split | — | `NOTICE`, README section |
| User-visible credit | — | in-app footer with link |

Attribution must name: `minhqnd/dictionary`, its own upstream sources (Wiktionary, `vntk/dictionary`),
the CC BY-SA 4.0 license URL, and **what we changed** (filtered to Vietnamese entries of ≥2
syllables, NFC-normalized, tone-variant aliases added, first/last syllable columns and indexes
added, all non-`vi` languages and all definitions/translations dropped).

## Phases

| # | Phase | Status | Depends on |
|---|-------|--------|-----------|
| 1 | [Foundations and Data Pipeline](./phase-01-foundations-and-data-pipeline.md) | Complete | — |
| 2 | [Go Dictionary and Normalization](./phase-02-go-dictionary-and-normalization.md) | Complete | 1 |
| 3 | [Go Game Engine and Bot AI](./phase-03-go-game-engine-and-bot-ai.md) | Complete | 2 |
| 4 | [Protobuf Contract and Codegen](./phase-04-protobuf-contract-and-codegen.md) | Complete | 1 |
| 5 | [Go WebSocket Server and Rooms](./phase-05-go-websocket-server-and-rooms.md) | Complete | 3, 4 |
| 6 | [SvelteKit Frontend](./phase-06-sveltekit-frontend.md) | Complete | 4, 5 |
| 7 | [Online 1v1 and Release](./phase-07-online-1v1-and-release.md) | Complete | 5, 6 |

Phases 2-3 and 4 are independent after phase 1 and can run in parallel if desired.

## Target Repository Layout

```
LICENSE                       # Apache-2.0 — code only
NOTICE                        # points at data/ licensing
README.md
buf.yaml  buf.gen.yaml
proto/noitu/v1/game.proto     # single protocol source of truth
data/
  LICENSE                     # CC BY-SA 4.0 full text
  ATTRIBUTION.md
  dictionary.db               # upstream, 179 MB, git-ignored, downloaded
  noitu.db                    # derived build artifact, git-ignored
server/cmd/build-dictionary/       # Go: dictionary.db -> noitu.db
server/                       # module github.com/tiennm99dev/noitu/server
  go.mod
  cmd/noitu-server/main.go
  internal/vietnamese/        # normalize.go, syllable.go
  internal/dictionary/        # store.go — read-only SQLite lookups
  internal/game/              # engine.go, state.go, rules.go
  internal/bot/               # bot.go, strategy_*.go
  internal/wsapi/             # hub.go, room.go, session.go, codec.go
  gen/noitu/v1/               # protoc-gen-go output
web/
  src/lib/proto/              # protobuf-es output
  src/lib/ws/                 # socket client, reconnect, codec
  src/lib/stores/             # game state (Svelte 5 runes)
  src/lib/components/
  src/routes/
```

## Success Criteria

- [x] `server/cmd/build-dictionary` reproducibly turns the upstream `dictionary.db` into `data/noitu.db`; entry count and ≥2-syllable purity asserted
- [x] `data/LICENSE`, `data/ATTRIBUTION.md`, `NOTICE`, README license section, and in-app credit all present and consistent
- [x] Go engine unit tests cover: wrong link, unknown word, reuse, single-syllable input, timeout, no-legal-move, and the tone-variant cases `hoà/hòa`, `thuý/thúy`, `quí/quý`
- [x] Words of 2, 3, and 4 syllables all accepted and chain correctly on first↔last syllable
- [x] One `.proto` generates working Go and JS clients; no hand-written message types
- [x] Vs-bot playable end to end at all 3 difficulties; Hard bot wins measurably more than Easy over 100 simulated games
- [x] Online 1v1: two browsers join by room code with chosen nicknames, alternate turns, server-enforced 20s timer, correct win/loss, reconnect within grace window restores the game
- [x] Vietnamese UI throughout; nickname and high score persist, checked in a real browser. The dark-mode toggle persists by the same mechanism but has only been unit-tested
- [x] `go build` produces one binary; `web` builds to static assets served by that binary
- [x] No wordlist reachable from the client bundle (verified by inspecting the built assets)
- [ ] CI runs green without ever downloading the 179 MB upstream DB — the workflows are written and every step passes locally, but they have not run on GitHub

## Risk Assessment

| Risk | Signal it happened | Response |
|---|---|---|
| Upstream `dictionary.db` yields too few clean ≥2-syllable entries (<40k) | Build script count assertion fails | Union with Viet74K multi-syllable filter (see research report §5) — decided before implementation, not mid-phase |
| Tone-variant aliasing misses real-world spellings | Players report valid words rejected | Alias table is data, not code: regenerate with an expanded rule set and rebuild `noitu.db`; add a rejected-word log to find gaps |
| CC BY-SA share-alike misapplied to code | License review flags the Apache/CC mix | Data stays a runtime-loaded separate artifact, never `go:embed`-linked; boundary documented in `NOTICE` and asserted in the image |
| Hard bot is unbeatable, players quit | Playtest win rate ≈0% vs Hard | Cap Hard's killer-syllable play rate; the difficulty ladder is tunable constants, not structure |
| In-memory rooms lost on deploy/restart | Live games drop | Accepted for v1 and stated in-app ("máy chủ đang khởi động lại"); persistence is a post-v1 item |
| Protobuf schema churn breaks a deployed client | Client decode errors after deploy | Additive-only field changes, reserved tags, version field in the handshake; server rejects unknown protocol versions with a clear message |
| Nicknames become an abuse surface (shown to strangers) | Offensive names reported | Server-side sanitization: length cap, control-char strip, whitespace collapse, empty → auto-name. A denylist is a cheap post-v1 addition if needed |
| Allowing 3+ syllable words admits phrases that feel wrong | Playtest complaints about unnatural entries | Builder supports a max-syllable cap and a denylist file; both are data changes, no code change |

## Validation Log

### Session 1 — 2026-09-04

**Verification Results**
- Tier: Full (7 phases)
- Claims checked: 14 · Verified: 11 · Failed: 1 · Imprecise: 2 · Unverified (env): 1

| Claim | Result | Evidence |
|---|---|---|
| proto `go_package` = `github.com/tiennm99/noitu/server/...` | **FAILED** | `git remote -v` → `github.com/tiennm99dev/noitu.git`. Corrected to `github.com/tiennm99dev/noitu/server` |
| `coder/websocket` provides `SetReadLimit`, `AcceptOptions.OriginPatterns`, `MessageBinary` | VERIFIED | pkg.go.dev, v1.8.15, ISC |
| "Read deadline per frame" via a socket deadline setter | **IMPRECISE** | coder/websocket exposes no `SetReadDeadline`; deadlines are `context.WithTimeout` on `Read`. Phase 5 corrected |
| `dictionary.db` downloadable from `minhqnd/dictionary` Releases | VERIFIED | GitHub API: release `v2.0.0`, asset `dictionary.db` |
| Upstream DB size assumed manageable | **IMPRECISE** | Asset is **179 MB**. Plan had no size claim; Data Distribution section added |
| `@bufbuild/protobuf` / `@bufbuild/protoc-gen-es` current | VERIFIED | npm registry, both v2.14.1 |
| Go ≥1.24, Node, Docker available locally | VERIFIED | go1.26.5, node v24.18.0, Docker 29.6.2 |
| `buf` installed locally | **UNVERIFIED (env gap)** | `buf: command not found`. Not blocking — generated code is committed, so `buf` is needed only to change the schema |

**Decisions confirmed**

| # | Question | Decision | Impact |
|---|---|---|---|
| 1 | Go module path | `github.com/tiennm99dev/noitu/server` | Phase 4 `go_package` corrected |
| 2 | Derived DB distribution | Do not republish — every build downloads upstream and derives locally | New Data Distribution section; phase 1 download step, phase 7 CI + Docker strategy |
| 3 | Turn time limit | 20s, identical for bot and PvP | Phase 5 default constant |
| 4 | Deployment target | Docker on a VPS behind a reverse proxy | Phase 7 docs scoped to proxy config, not a PaaS |
| 5 | Word length rule | **≥ 2 syllables** (changed from strict 2) | Phases 1, 2, 3, 4, 6 — filter, API, reject reason, proto enum, UI copy |
| 6 | Player identity in PvP | **User-typed nickname** | Phases 4, 5, 6, 7 — proto field, sanitization, input UI, display |

**Phase propagation:** phases 1-7 all updated. Reject reason renamed
`NOT_TWO_SYLLABLES` → `TOO_FEW_SYLLABLES` across the engine, proto, and Vietnamese copy.

### Whole-Plan Consistency Sweep

Re-read `plan.md` + all 7 phase files after propagation.

| Check | Result |
|---|---|
| Stale "2-syllable"/"exactly 2" claims | Resolved — all now "≥2 syllables" / "at least 2" |
| `NOT_TWO_SYLLABLES` enum name | Resolved — renamed everywhere (engine, proto, i18n) |
| `go_package` module path | Resolved — single correct value in phase 4 |
| Turn limit stated inconsistently | Resolved — 20s in plan.md + phase 5, `NOITU_TURN_LIMIT` override |
| Nickname referenced but never sourced | Resolved — proto field, sanitization, input, and display now specified |
| `online/+page.svelte` Create vs Modify across phases 6/7 | Resolved — phase 6 creates the stub, phase 7 modifies it |
| Data distribution vs Docker/CI steps | Resolved — consistent across plan.md, phase 1, phase 7 |
| Duplicate embedded proto schema | Single copy, in phase 4 only |

**Unresolved contradictions: none.**

## Phase 1 Outcome (2026-09-04)

Built and verified against the real upstream release.

| Metric | Value |
|---|---|
| Words | 48,216 (floor 40,000) |
| 2 / 3 / 4+ syllables | ~40k / ~4.5k / ~3.5k |
| Aliases | 2,041 |
| Derived DB | ~3 MB, from a 179 MB source |
| Coverage | `internal/vietnamese` 100%, `cmd/build-dictionary` 82.4% |

Upstream schema auto-detected (`words.word` / `lang_code`) with no flag override needed,
retiring the phase-1 "unknown upstream schema" risk. Upstream asset pinned by SHA-256.

**Four defects were found and fixed in alias generation, three of them only visible by
inspecting real output rather than fixture tests:** `quý`→`qúy`, `gì`→`gỳ`,
`hoàn`→`hòan` (tone shift must be confined to open syllables), and an i/y rule so broad it
invented ~340 non-words such as `chức vỵ`. A fifth, opposite defect was found by review:
varying one syllable at a time never produced `hóa lý`, the spelling most people type.
Variants are now generated as a cross product.

A Vietnamese phonotactic check (closed onset/nucleus/coda inventories) now rejects
loanwords the multilingual source tags as Vietnamese — `credit card`, `world cup`,
`come out`. Its first version wrongly rejected the entire `gì`/`gỉ`/`gìn` family by
greedily matching the `gi` digraph; it backtracks over onset candidates now.

## Phase 4 Outcome (2026-09-04)

The wire contract is fixed and generated into both languages. Detail in
[`phase-04`](./phase-04-protobuf-contract-and-codegen.md#phase-4-outcome-2026-09-04).

Enum values carry buf's mandated enum-name prefix (`REJECT_REASON_TOO_FEW_SYLLABLES`, not
`TOO_FEW_SYLLABLES`), so `buf lint` passes with no carve-out; `protobuf-es` strips the prefix,
leaving the JS side reading `RejectReason.WRONG_LINK`. Two fields were added that the plan
sketch lacked: `PlayedWord.typed`, because the engine keeps the player's raw input alongside
the canonical spelling and the UI has to show a correction happened, and
`REJECT_REASON_GAME_OVER`, because `game.ReasonGameOver` exists and the exhaustiveness test
correctly refused a wire contract without it.

`web/` now exists as a bare npm package — protobuf and Vitest only — because phase 4's own
JS codegen and cross-language test need somewhere to live. Phase 6 layers SvelteKit on top.

Both suites read the same 17 binary fixtures in `proto/testdata/`, emitted by the Go tests.
Review caught four guards that were reporting green while protecting nothing — a `buf
breaking` baseline that cannot resolve on a PR checkout, a sync check blind to untracked
files, and two tests whose oracles could drift with the code they checked. All four were
fixed and then negative-tested by deliberately breaking each one.

## Phase 5 Outcome (2026-09-05)

The transport layer is in: rooms, turn timers, the bot as a virtual player, reconnect, and
a single binary that serves both the API and the frontend. Detail in
[`phase-05`](./phase-05-go-websocket-server-and-rooms.md#phase-5-outcome-2026-09-05).

The concurrency contract holds structurally rather than by convention: the room goroutine
starts before anyone is seated and seating itself is a message, so reading `run()` is a
complete proof that one goroutine owns each engine. The bot searches a frozen board rather
than the live engine, and reads carry no deadline — liveness is ping-based, because a read
timeout cannot distinguish a healthy player idling in the lobby from a dead socket.

Seven defects were found and fixed: three by writing the tests, three more by review, and
a seventh while fixing those. The serious one was authorization — the hub bound a joiner
to a seat before the room decided whether to seat them, so anyone holding a room code
could resign or play on a seated player's behalf. Since the room code is the only
credential online 1v1 has, that was a phase-7 release blocker caught a phase early.

## Phase 6 Outcome (2026-09-05)

The vs-bot UI is built and served by the Go binary. Detail in
[`phase-06`](./phase-06-sveltekit-frontend.md#phase-6-outcome-2026-09-05).

The store is a reducer over `ServerMessage` and computes nothing: validity, turn order and the
result are read from the wire, which is what lets one screen serve both the bot and, in phase
7, online play. The word field is deliberately uncontrolled, because a Telex input method
composes a diacritic over several keystrokes and writing the value back cancels it. The
countdown is drawn against the server's clock and settles 300ms early, so the ring never claims
more time than the server allows.

Three defects were found by building and five more by review. The serious one was that the game
lifecycle was inferred from the game model rather than owned: the screen asked for a bot game
whenever the board was idle, and clearing the board for a rematch is exactly that condition, so
every rematch started two server-side rooms that then destroyed each other. A request is stored
intent now, with tests over the wiring whose absence let it through. Review also caught a word
silently dropped when typed during a reconnect, a protocol bump that would have put every open
tab into a reconnect loop, an opening turn counted against the device clock, and an
`index.html` cacheable across a deploy.

Four criteria need a real browser and stay open: one-handed mobile use, Telex diacritic entry,
the absence of a theme flash, and reconnect after the server is killed mid-game.

## Phase 7 Outcome (2026-09-05)

Online 1v1 is playable end to end and the game ships as one 24 MB container image. Detail in
[`phase-07`](./phase-07-online-1v1-and-release.md#phase-7-outcome-2026-09-05).

The rematch needed the one thing phase 5 deliberately did not do: let a room outlive its game.
It now does, but only while both seats hold live connections, so a bot room still closes the
moment its game ends. There is no decline message — leaving is the decline, which the server
already learns from the socket closing.

Building the browser suite found four defects the unit tests could not: a player who refreshed
mid-game could not rejoin, a restored opponent never cleared the other player's disconnect
banner, a socket that failed without closing was invisible to the client, and a nickname typed
on the online screen never reached the server because the handshake had already gone.

Two testing tools turned out not to work here and are documented where the next person will
look: offline emulation does not cut a loopback socket, and a routed socket's close does not
reliably reach the server, so the grace window never starts.

Review then reproduced three more. Two follow from the new state this phase introduced — a room
that outlives its game — and one predates it: a game ending on the turn clock never offered a
rematch although the button was there, a second connection presenting the same resume token
disconnected the player who had not left, and rooms leaked whenever one connection created
several. All three now have regression tests, and the two reachable through the room loop were
negative-tested by reverting the fix.

## Open Questions

1. Does the losing player see the words the bot *could* have played (a teaching feature), or just the result? Plan currently assumes just the result.
2. Should the builder cap maximum syllables (e.g. reject 5+ syllable entries as phrases rather than words)? Plan currently applies no upper bound; the cap exists in the builder as a flag if playtesting says otherwise.
3. Domain name / TLS certificate source for the VPS deployment.
4. Should the turn clock pause while a seat is inside its reconnect grace window? As
   built the grace window (30s) is longer than the turn limit (20s), so a player who
   drops on their own turn always loses on time before the window closes — the grace is
   unreachable for the player most likely to need it. Their reconnect is now told
   `game_already_over` rather than left waiting, but the underlying policy is unresolved.
5. Should bot rooms be registered in the joinable room namespace at all? They have no code
   to share, and excluding them removes a whole class of stranger interference for free.
6. Is closing the connection the right response to a full outbox mid-game? 32 queued
   frames is not much on a lossy mobile link, and the opponent currently sees that player
   as having left.
