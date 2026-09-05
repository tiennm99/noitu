# Code review — phase 6 SvelteKit frontend

Date: 2026-09-05
Reviewer: code-reviewer subagent
Scope: `web/src/**`, `web/tests/**`, `web/*.config.js`, cross-checked against
`proto/noitu/v1/game.proto` and `server/internal/wsapi/*.go`.

Outcome: **DONE_WITH_CONCERNS**. One blocking defect, four high-severity
breakages, six medium, nine low. All blocking and high findings were fixed in
the same session; see
[`phase-06`](../260904-1125-noi-tu-web-game/phase-06-sveltekit-frontend.md#defects-found-by-review).

## What held up under scrutiny

- The store is a genuine projection of `ServerMessage`, with no client-side rule
  logic and all ten oneof arms handled.
- The displayed nickname is only ever `Welcome.accepted_nickname`.
- `RejectReason` completeness is driven off the generated schema, so it is a
  real guard rather than a restatement of the table it checks.
- Both inputs are uncontrolled at keystroke level and submit is blocked
  mid-composition.
- Every storage access is guarded, including browsers that throw on the property
  itself.
- No `{@html}` anywhere; server-supplied names and words go through text
  interpolation.
- The wordlist guard was independently re-verified: its own decode-and-search
  finds Vietnamese UI copy in the shipped chunks and no dictionary word, so the
  technique demonstrably works on this bundle.
- The error-code map matched the server exactly, 20 codes on each side.

## Blocking

**C1 — every rematch started two bot games.** The start was inferred from
`game.state.phase === 'idle'` inside an effect. `rematch()` calls `game.reset()`,
which sets that condition, then sends `StartBotGame` itself; the effect saw the
phase change and sent a second. The server allocates a room per request
(`hub.startBotRoom`) and reseats the session (`room.go` `handleStartBot`), so the
abandoned room kept its goroutine, its turn timer and a pointer to the same live
socket. Symptoms: two `GameStarted`, the old room's bot moves appended to the new
chain, `turnSeq` overwritten so submissions were rejected as stale, and a
timeout-loss about one turn later with no visible cause.

## High

- **H2** — arriving at `/play` with a non-idle store never started a game, since
  nothing reset the store on mount and only the game-over panel's home button
  called `reset()`.
- **H3** — a word submitted while the socket was down was cleared from the field
  and never sent; the input was gated on turn state only, never on the
  connection.
- **H4** — the backoff reset on socket open rather than on a successful
  handshake, so a protocol bump would have made every open tab reconnect every
  few hundred milliseconds indefinitely.
- **H5** — the first clock probe was scheduled a full interval out, leaving the
  opening turn drawn against the raw device clock.

## Medium

- **M1** — the best-score effect read and wrote the same reactive state; only a
  non-reactive flag kept it from looping.
- **M2** — re-entering the board re-scored the same result.
- **M3** — the animation loop ran every frame between turns.
- **M4** — no cache headers on the static bundle; a cached `index.html` outlives
  the deploy that renamed its assets.
- **M5** — a nickname changed after the socket opened never reached the server.
- **M6** — the error-code map had no completeness guard, only a correct list.

## Low

Prose outside the string table (two aria labels), an end-reason assertion that an
empty string satisfied, a bundle freshness claim that checked only existence, a
size budget with 8x headroom, `aria-modal` on an inline panel, a radio role
without radio keyboard behaviour, dead `forgetSession`, an index-keyed `each`,
and two owners for the `data-theme` attribute.

## Unresolved questions

1. Should the play route resume a live game when one exists, rather than always
   starting fresh? Answered for phase 6 by giving the current game up on the way
   out, but phase 7's PvP resume will have to revisit it.
2. Should a protocol mismatch force a page reload rather than only stopping
   reconnection? Currently the player is told to reload and must do it.
