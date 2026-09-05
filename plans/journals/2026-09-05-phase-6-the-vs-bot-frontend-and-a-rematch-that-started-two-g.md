---
title: "Phase 6: the vs-bot frontend, and a rematch that started two games"
date: 2026-09-05
summary: Built the SvelteKit vs-bot UI; review caught a lifecycle inferred from game state that made every rematch spawn two server rooms.
---

# Phase 6: the vs-bot frontend, and a rematch that started two games

## What happened

Committed phases 4 and 5 as two focused commits, then implemented phase 6: the
SvelteKit single-page app for vs-bot play, served by the Go binary.

The store is a reducer over `ServerMessage`. Validity, turn order, scores and the
result all come from the wire, so one screen can serve the bot now and online
play in phase 7.

Verified end to end without a browser: a headless client built from the same
generated protobuf types played bot games at all three difficulties against a
fixture dictionary, and curl confirmed the binary serves the app including deep
links while refusing path traversal.

## The defect worth remembering

Review found the game lifecycle was **inferred** rather than owned:

```js
$effect(() => {
  if (connection.status === OPEN && game.state.phase === 'idle') {
    send(startBotGame(difficulty));
  }
});
```

"Start a game when the board is idle" reads as an invariant but is a trigger.
`rematch()` clears the board and then sends its own request, so the effect saw
the phase change and sent a second. The server allocates a room per request and
reseats the session, so the abandoned room kept its goroutine and turn timer
pointed at the same live socket: the old room's bot moves landed in the new
chain, `turn_seq` was overwritten so submissions were rejected as stale, and a
turn later the player lost on a timeout with no visible cause.

The fix is to store the request as intent (`bot-session.svelte.js`) so clearing
the board cannot mean anything. Twelve tests now cover that wiring, because its
absence is exactly why the bug shipped.

Four more user-visible breakages came from the same review: a word typed during
a reconnect cleared silently, the backoff reset on socket open rather than on a
successful handshake (a protocol bump would have looped every open tab), the
first clock probe was scheduled a full interval out so the opening turn counted
against the device clock, and the served `index.html` was cacheable across a
deploy.

## Decision

Guards get negative-tested before they are trusted. The bundle wordlist check
passed on its first negative test because Rollup dropped the unused property I
planted; only a rendered string made it fail. Two guards in this phase were
broken deliberately and watched fail for the right reason before being kept.

## Next steps

- Phase 7: online 1v1 and release.
- Five criteria still need a real browser: one-handed mobile use, Telex
  diacritic entry, theme flash, kill-the-server reconnect, and the rematch path.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
