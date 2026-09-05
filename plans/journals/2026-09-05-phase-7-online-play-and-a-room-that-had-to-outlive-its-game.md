---
title: "Phase 7: online play, and a room that had to outlive its game"
date: 2026-09-05
summary: "Shipped online 1v1, browser E2E and the container image; review found two defects caused by keeping a room alive past game over, plus a pre-existing room leak."
---

# Phase 7: online play, and a room that had to outlive its game

## What happened

Phase 7 in one pass: a rematch protocol, online 1v1 in the browser, 21 Playwright
specs, a container image, CI, and deployment docs.

The one real design change was on the server. Rooms used to die the instant
`engine.Over()` was true, so a rematch needed the room to survive its own game.
It does now, but only while both seats hold live connections — a bot room still
closes immediately, because there is nothing to negotiate with a bot and keeping
it alive leaks a goroutine and an engine per finished game.

## What that new state broke

Two of the three defects review found were direct consequences of it:

- **A game ending on the turn clock never offered a rematch.** The offer was
  opened from the message arm of the room loop, and the timer branch `return`ed
  before reaching it. Resign and no-legal-move endings worked; the most common
  natural ending did not, and the UI still showed the button. The end-of-game
  decision now sits after the whole `select`.

- **A second connection with the same resume token killed the room.**
  `resumeFrom` retired the old connection as soon as `r.send` succeeded, but the
  room decides asynchronously and refuses a resume into a finished game — which
  is exactly the rematch window. A duplicated tab was enough. The room now
  retires the old connection only once it has agreed to the swap.

The third predated the phase: `attach` overwrote the session's room pointer and
nothing told the old room, so one connection creating four rooms stranded three
of them forever. Fixed here because this is the phase that puts the server on
the internet.

## What building the browser suite found

Four more, none of which the unit tests could reach: a refreshed tab could not
rejoin its game, a restored opponent never cleared the other player's disconnect
banner, a socket that failed without closing was invisible to the client, and
the nickname typed on the online screen never reached the server because the
handshake had already gone out.

## Decision

Two testing tools do not work here, and both are now documented in
`e2e/socket-cut.js` rather than left to be rediscovered. Offline emulation does
not cut a loopback socket. A routed socket's close does not reliably reach the
server, so the grace window never starts — those tests take the page away
instead.

## A duplicate I wrote and removed

I added a bot difficulty-ladder test to satisfy a plan criterion, then found
`simulate_test.go` had covered it since phase 3 with better thresholds and a
real corpus variant. I had grepped one file and concluded the coverage was
missing. Deleted mine.

## Next steps

- CI has never run on GitHub; every step passes locally.
- The Dockerfile's upstream-downloading branch is unexercised — only the fixture
  branch has been built.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
