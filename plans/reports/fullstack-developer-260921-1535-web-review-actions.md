# Web review actions — implementation report

Branch `worktree-agent-ae280f2081bd718f6` (fast-forwarded from `main`/dd3b463 to `dev`@3ef9f48
before starting — the worktree had been created one point behind `dev`, missing quick-match,
dead-end claim, ESLint config and the rest the review was written against) · worktree
`/workspace/tiennm99/noitu/.claude/worktrees/agent-ae280f2081bd718f6`.

Seven commits on top of `dev`@3ef9f48, all `web/`, nothing pushed:

```
c4502f2 refactor(web): split the game store into shape, apply and store files
70ae09d refactor(web): type the wire instead of passing any across it
eace594 refactor(web): extract the online screen's request machine into a store
f0c2334 test(web): cover the room-session request machine
f0bcb60 refactor(web): extract ArmedButton for the three press-twice controls
69310a3 test(web): mount ChatPanel under jsdom for fold and unread accounting
cf48d27 test(web): mount WordInput and GameBoard under jsdom
```

## Verification

| | Before | After |
|---|---|---|
| `npm run lint` | 0 errors, 33 warnings (32 `jsdoc/reject-any-type`, 1 `check-param-names`) | 0 errors, **1 warning** |
| `npm run check` | 0 errors, 0 warnings (meaninglessly, per the review) | 0 errors, 0 warnings — now over a typed store |
| `npm test` | 221 passed / 12 files | **258 passed / 16 files** |

The one remaining warning: `tests/room-code.test.js:77` deliberately calls
`normalizeRoomCode(/** @type {any} */ (undefined))` against its documented `string`-only
signature, to prove the function's own defensive `String(raw ?? '')` — that is the point of the
test, not an oversight.

No Playwright run (no browser on this host, per workspace rules).

## Per deliverable

**1 — Resume latch time-box.** `stores/room-session.svelte.js` owns `resuming`; the page arms a
5s (`RESUME_TIMEOUT_MS`) timer once `resuming && connection.status === Status.OPEN`, calling
`session.noteResumeFailed(named)` if nothing has resolved it by then — same transition the
existing "resume answered with an error" effect already ran, now shared by both paths. A server
new enough to answer `session_not_resumable` clears it sooner through the ordinary error effect
(the message already existed in `vi.js`). The join form's `resumeFailed` banner reuses that same
string rather than inventing new copy. Unit-tested in `tests/room-session.test.js` (timeout,
already-resolved-before-the-timer-fires, named vs. unnamed invite code held behind it).

**2 — `leave()` calls `forgetSession()`.** One line in `routes/online/+page.svelte`'s `leave()`,
matching the teardown path. No dedicated test file (it is a page-level wiring fact, not store
logic) — covered implicitly by the held-action semantics test in point 6, and by inspection: `git
show eace594 -- routes/online/+page.svelte` shows the added call.

**3 — Type the store.** `initialState()` now returns `GameState` (real `@typedef` with one
`@property` per field, folded into `game-shape.js`), not `any`. `apply()` switches on
`payload.case` without destructuring, so the oneof narrows. `svelte-check` stayed at 0
errors/warnings after the retype — no latent bugs surfaced in the 16 components, which the review
flagged as a real possibility; I take that as the store's shape genuinely having matched its
usage everywhere, not as the check being weak (it now has a real type to fail against, and
deliberately-wrong scratch edits during development did produce the expected errors and warnings
before being fixed).

**4 — Extract `stores/room-session.svelte.js`.** Owns `pending`/`resuming`/`needName`/`stalled`/
`resumeFailed`/`queuedForS`/`heldAction`; no DOM, no runes beyond `$state`, modelled on
`bot-session.svelte.js`. The page keeps every timer (`setTimeout`/`setInterval`) and all layout —
`JoinPanel`/`RoomLayout` extraction from the review's secondary suggestion was explicitly out of
scope this round. `routes/online/+page.svelte` script is ~290 lines, down from ~400 in the
pre-fast-forward review's line numbers (harder to compare directly since the branch had drifted,
but the five request-machine `$effect`s the review named are gone from the page). Unit tests: join
latch + refusal retry, resume latch + timeout + already-resolved race, quick-match queued→seated,
held-action replace/retry/drop-on-teardown (18 tests, `tests/room-session.test.js`).

**5 — Type the wire.** `ws/client.js`, `ws/connection.svelte.js`, `stores/bot-session.svelte.js`
now use the generated `ServerMessage`/`ClientMessage` union instead of `any`; timers typed
`ReturnType<typeof setTimeout>`. Cleared every `jsdoc/reject-any-type` in `src/`. In `tests/`, five
files lost their casts for free once the store and wire were typed
(`game-store.test.js` ×3, `game-wire.test.js` ×2 via a typed `decode()`, `ws-client.test.js` ×6).
**Kept:** `tests/room-code.test.js:77` — see Verification above, the one deliberate `any`.

**6 — Held requests for lobby actions.** `room-session`'s `heldAction` slot (replace-not-queue: a
later action supersedes an earlier unset one, since e.g. readying then leaving before reconnect
means leave should win) backs `cancelQueue`, `leave`, `ready`/`unready`, `start`, `kick`. Each
tries `send()` first and only latches on refusal; a socket-open effect retries alongside the
existing join/create flush. `Lobby.svelte`'s local one-shot `unsent` flag (which never noticed a
background retry had succeeded) is now the `actionHeld` prop, driven straight off
`session.state.heldAction`. Tested: 6 of the 18 `room-session.test.js` cases.

**7 — Split `game.svelte.js`.** `stores/game-shape.js` (typedefs, `initialState()`, wire decoders
`toSenses`/`toParts`/`toScore`/`toSlot`) / `stores/game-apply.js` (`applyTo(state, msg,
{reset, leave})`, pure) / `stores/game.svelte.js` (`$state`, `reset`/`leave`, derived accessors,
singleton). `apply()` wraps `applyTo()` in try/catch, logging and keeping the previous snapshot on
a throw. All 61 pre-existing store tests pass unmodified against the new module split (they only
import from `game.svelte.js`, which still re-exports `CHAT_WINDOW` and `createGameStore`).

**8 — Component tests under jsdom (first half).** `tests/chat-panel.test.js` (9: fold-by-default,
opens on toggle, never folds when not collapsible, unread counts only while folded, clears on
open, recounts after a fold/reread cycle, send/clear, whitespace-only refused).
`tests/word-input.test.js` (7: seeds once per turn, does not seed/focus while offline — the C6
regression — does not reseed the same turn across a connection blip, reseeds on a genuine new
turn, leaves an in-progress composition alone on the player's own turn, suggestion fills on click,
no suggestion button when the server sent none). `tests/game-board.test.js` (3: chat pill absent
with no `onchatopen`, present once one is passed, click reaches the handler and shows the unread
count). Needed one infrastructure fix: `vite.config.js` now sets
`resolve.conditions: ['browser']` gated on `process.env.VITEST`, or Svelte resolves its
server-rendering entry point under Vitest and `mount()` throws
`lifecycle_function_unavailable`; `vite dev`/`vite build` are unaffected since `VITEST` is only
set by the Vitest CLI. jsdom also needed local polyfills for `Element.scrollTo` and
`ResizeObserver` (used by `ChatPanel`/`ChainHistory` for auto-scroll and reflow, neither
implemented by jsdom) — stubbed per test file, not globally.

`e2e/helpers.js`/`e2e/pvp-game.spec.js` fixed per the flake analysis: `playingPair` now calls
`joinRoomSeated` instead of `joinRoom`, and `readyAndStart` asserts
`guest.getByTestId('my-ready')` reads "Đã sẵn sàng" (the actual `t.isReady` string — the review's
own example text was illustrative, not the literal copy) before touching Start. **Unverified
locally** — no browser on this host; these are read-through-verified against the actual
`Lobby.svelte` markup and `vi.js` strings, not run.

Playwright suite (47→~14) was **not** cut, per the task's explicit instruction to keep it as-is
for now.

**9 — `ArmedButton.svelte`.** One component for resign, claim-dead-end and kick: owns the arm
timer and the disarm-on-`disabled` effect, adds `aria-pressed` (announces the armed state to
assistive tech on the same control that already has focus, closing the a11y gap the review
flagged — a screen reader speaks a control's name on focus, not on an in-place label mutation).
Behaviour is otherwise identical, with one acknowledged, minor, unstated-invariant change: kick
now arms per seat (each `ArmedButton` instance is independent) rather than sharing one
Lobby-level "which seat is armed" slot, so two seats could in principle be armed at once within
the 4s window. No test or review language documented the old cross-seat exclusivity as intended
behaviour, and no e2e spec exercises it. GameBoard's and Lobby's `<style>` blocks needed
`:global(.resign)`/`:global(.claim-dead-end)`/`:global(.kick...)` — those buttons are now rendered
by a child component, so the parent's scoped-style attribute no longer reaches them; the selectors
and rules themselves are untouched.

**10 — Meanings key.** `ChainHistory.svelte`'s inner `{#each entry.meanings as sense (sense.gloss)}`
→ keyed by index (`senseIndex`), with a comment stating why (list is neither reordered nor
filtered, so an index is stable; the dictionary gives no gloss-uniqueness guarantee).

**11 — Smaller findings.** Lobby chat now unfolds on the `over` phase transition (phase never
actually revisits `'lobby'` after the first game — it goes `over` → next `gameStarted` →
`'playing'` directly, so "fold once, stay folded forever" was the only reachable outcome without
this). `WordInput`'s turn-seed effect now gates on the same `enabled` derived value the submit
path already uses (folds in the connection check), fixing C6 and simplifying the guard from three
conditions restated to one shared one.

## Deferred / explicitly out of scope

- `JoinPanel.svelte` / `RoomLayout.svelte` extraction (review §1.1 secondary suggestion) — task
  said the page keeps layout.
- Playwright 47→~14 trim (review §6) — task said not yet.
- P2 (route-swap socket race), P3 (resume-failure swallows unrelated errors), and the rest of
  review §5's accessibility gaps beyond #9 — not in the 11-item scope given.
- The `?? []` guards on wire-decoded repeated fields (review notes these are dead under
  protobuf-es v2) — left as defensive; not an action item.

## Unresolved questions

1. Kick's per-seat independent arming (see #9) is a real, if narrow, behaviour change from a
   Lobby-level shared arm slot. Flagging rather than deciding — no stated invariant either way,
   and reverting to shared-slot semantics inside `ArmedButton` would mean lifting arm state back
   out to the parent, undoing the DRY the component exists for.
2. The e2e helper fixes (readyAndStart/playingPair) are read-through-verified only; this
   environment cannot run Playwright to confirm the flake is actually gone.

Status: DONE
Summary: All 11 deliverables landed on `worktree-agent-ae280f2081bd718f6` at
`/workspace/tiennm99/noitu/.claude/worktrees/agent-ae280f2081bd718f6`; lint 0 errors/1 deliberate
warning (was 33), check 0 errors/0 warnings, 258/258 tests green (was 221), 7 focused commits, no
push, no server/proto/generated-client edits.
Concerns: kick's arming is now per-seat rather than shared (see unresolved #1); e2e helper edits
are unverified without a browser (see unresolved #2).
