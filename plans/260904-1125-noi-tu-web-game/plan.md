---
title: "Noi Tu Web Game"
description: "Vietnamese nối từ web game — SvelteKit frontend, Go backend, WebSocket + Protobuf, server-authoritative dictionary over SQLite. Vs-bot and online 1v1."
status: pending
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
| 1 | [Foundations and Data Pipeline](./phase-01-foundations-and-data-pipeline.md) | Pending | — |
| 2 | [Go Dictionary and Normalization](./phase-02-go-dictionary-and-normalization.md) | Pending | 1 |
| 3 | [Go Game Engine and Bot AI](./phase-03-go-game-engine-and-bot-ai.md) | Pending | 2 |
| 4 | [Protobuf Contract and Codegen](./phase-04-protobuf-contract-and-codegen.md) | Pending | 1 |
| 5 | [Go WebSocket Server and Rooms](./phase-05-go-websocket-server-and-rooms.md) | Pending | 3, 4 |
| 6 | [SvelteKit Frontend](./phase-06-sveltekit-frontend.md) | Pending | 4, 5 |
| 7 | [Online 1v1 and Release](./phase-07-online-1v1-and-release.md) | Pending | 5, 6 |

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

- [ ] `server/cmd/build-dictionary` reproducibly turns the upstream `dictionary.db` into `data/noitu.db`; entry count and ≥2-syllable purity asserted
- [ ] `data/LICENSE`, `data/ATTRIBUTION.md`, `NOTICE`, README license section, and in-app credit all present and consistent
- [ ] Go engine unit tests cover: wrong link, unknown word, reuse, single-syllable input, timeout, no-legal-move, and the tone-variant cases `hoà/hòa`, `thuý/thúy`, `quí/quý`
- [ ] Words of 2, 3, and 4 syllables all accepted and chain correctly on first↔last syllable
- [ ] One `.proto` generates working Go and JS clients; no hand-written message types
- [ ] Vs-bot playable end to end at all 3 difficulties; Hard bot wins measurably more than Easy over 100 simulated games
- [ ] Online 1v1: two browsers join by room code with chosen nicknames, alternate turns, server-enforced 20s timer, correct win/loss, reconnect within grace window restores the game
- [ ] Vietnamese UI throughout; dark mode toggle persists; nickname and high score persist in localStorage
- [ ] `go build` produces one binary; `web` builds to static assets served by that binary
- [ ] No wordlist reachable from the client bundle (verified by inspecting the built assets)
- [ ] CI runs green without ever downloading the 179 MB upstream DB

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

## Open Questions

1. Does the losing player see the words the bot *could* have played (a teaching feature), or just the result? Plan currently assumes just the result.
2. Should the builder cap maximum syllables (e.g. reject 5+ syllable entries as phrases rather than words)? Plan currently applies no upper bound; the cap exists in the builder as a flag if playtesting says otherwise.
3. Domain name / TLS certificate source for the VPS deployment.
