# Web frontend review: whole codebase

Branch `dev` @ d3eb13e, 2026-09-29. Read-only review. The scope was `web/src` (excluding `lib/proto`), `web/tests`, `web/e2e` (read as code only), and the web config files.
Gates at start and end: `npm run lint` 0/0, `npm run check` 0 errors / 0 warnings (385 files), `npm test` 270 passed (16 files).
Playwright was not run (no browser on this host).

This review builds on these reports and does not repeat what they found:
- `web-refactor-260928-1348-review-and-refactor.md`
- `code-reviewer-260921-1529-web-architecture-review.md`
- `ui-ux-designer-260921-1529-whole-game-ux-review.md`

## Verdict

The store and reducer are careful and well tested for the message streams they expect. The real defects are all on **reconnect paths where the server's replay does not describe the state the player is actually in**. In each one the UI keeps showing a room or game that the server has already moved past:

- The game ended while the player was away.
- The player was eliminated before dropping.
- The resume was refused while the player was still in a room.
- A reload of `/play`.
- A held lobby action flushed straight after `Hello`.

None of these is covered by a unit test or an e2e test. Findings 1, 2 and 3 were confirmed against the real store with a throwaway Vitest file. The file fed the store the exact frame sequence `handleResume` / `resumeFrom` emit (`server/internal/wsapi/room_presence.go:113-173`, `dispatch.go:267-307`), all four assertions reproduced the stale state, and the file was deleted afterwards.

## Findings

| # | Sev | Where | What is wrong |
|---|---|---|---|
| 1 | High | `stores/game-apply.js:81-98` + server `room_presence.go:159-165` | A player who drops mid-game and comes back inside the grace window, after that game has **ended**, is resumed into the lobby with `RoomState` + `ChatHistory` only. No `GameOver` arrives and `roomState` deliberately does not move `phase`, so the client stays in `phase: 'playing'`. Result: a frozen board, possibly a stale `myTurn: true`, and no Lobby (the Lobby is only rendered for `lobby`/`over`), so there is no Ready or Leave button. A guest in that state blocks the owner's Start until they navigate away. |
| 2 | High | `stores/game-apply.js:25` (`LEAVES_ROOM`), `:252-264` | A resume refused while the UI still shows a room is only rendered as a banner. The two cases are `session_not_resumable` (dropped for longer than the grace window, or the server restarted) and `game_already_over` (a bot room that closed on game over while the player was offline). The room or board stays on screen with `connection: open`. On `/play` the dead board has no rematch button (GameOverPanel needs `phase === 'over'`), and the input and resign stay enabled if `myTurn` was true. The `/online` page's own recovery (`+page.svelte:272-278`) only fires while `session.state.resuming`, which is set only on page mount, not on an in-page socket drop. |
| 3 | Med | `stores/game.svelte.js:117-119` (`iAmOut`) | `iAmOut` is `phase === 'playing' && elimination !== null`. A player eliminated in a game of 3 or more who refreshes or reconnects gets the replayed `GameStarted`. That runs `reset()`, which nulls `elimination`, even though the replayed `players` row has `isMe && eliminated: true`. So `iAmOut` is false, the spectator box is replaced by a permanently waiting WordInput, and a disabled claim/resign row is shown. This was confirmed in the store: `gamePlayers[me].eliminated === true`, `iAmOut === false`. |
| 4 | Med | `routes/play/+page.svelte:42-52, 70-78` | A reload mid-bot-game never runs the teardown, so the tab's resume token survives. The mount then sends `Hello{resumeToken}` and, as soon as the status reaches OPEN, `StartBotGame` on the same socket. The server resumes asynchronously (`resumeFrom` posts to the room goroutine). If the resume attaches first, the player sees the resumed board under a red `already_in_a_game` banner. If `StartBotGame` wins, a second bot room is opened, then displaced by the resume, and the client receives two `GameStarted` frames. Either outcome is wrong. The same happens if the tab holds a token from `/online` and the player opens `/play` by URL: it resumes the PvP seat on the bot screen. |
| 5 | Med | `routes/online/+page.svelte:221-227` (`flushAction`), `room-session.svelte.js:188-193` | A held lobby action is flushed the moment the status reaches OPEN, which is right after `Hello`. For an in-page reconnect, `Hello` carries the token and the server attaches the seat asynchronously on the room goroutine, so `KickPlayer`/`SetReady`/`StartGame` is most likely read before the attach and answered `not_in_a_room` (`dispatch.go:224-227`). The player sees the red `lobby-error` "Bạn không ở trong phòng nào." in a room they are in, and the held action is discarded (the send "succeeded"). Kick is reachable because its button is not gated on `offline` (`Lobby.svelte:113-122`). `flush()` for join/create already guards `resuming`; `flushAction` has no equivalent. |
| 6 | Med | `routes/online/+page.svelte:406-414` (`leave`) | Pressing Leave while the socket is down holds `leaveRoom`, then clears the room and **forgets the token**. The reconnect therefore opens a fresh session, and the held `LeaveRoom` it flushes is answered `not_in_a_room`. That lands as a red `join-error` on the join form the player just returned to. The held message cannot do anything useful: without the token, the seat is released by grace expiry either way. |
| 7 | Med | `components/ChatPanel.svelte:128-144, 213`; `online/+page.svelte:416-419` | A chat line sent while the socket is down is lost silently. `say()` ignores `send()`'s `false`, and `submit()` clears both `draft` and the field unconditionally. The send button is not gated on the connection, unlike WordInput (`WordInput.svelte:22-24`). The player types a message during a blip, presses Gửi, the text vanishes, and nothing is ever posted or explained. |
| 8 | Med (a11y) | `components/PlayerStatus.svelte:66-75` | Each away player's banner is `role="status"` (implicitly `aria-live="polite"`) and its text holds a countdown that changes every second. A screen reader announces "X mất kết nối, còn N giây" once a second for up to 30 s, once per dropped player, on both the board and the lobby. This is the same defect the UX review fixed for the quick-match counter (`online/+page.svelte:522-527`), but this one was missed. |
| 9 | Low | `components/GameOverPanel.svelte:33-35` | `panel.focus()` fires on every new result, with no guard. In the wide layout (or for a knocked-out spectator), a player typing in the chat input when the game ends has focus pulled off the chat mid-sentence, and the rest of their keystrokes go nowhere. WordInput already has the right guard (`typingElsewhere`, `WordInput.svelte:52-59`). |
| 10 | Low | `ws/client.js:291-299` | `clockOffsetMs` is overwritten by every pong, one sample every 5 s. On a jittery mobile link the error is up to ±RTT/2 per sample, so the ring and the seconds label jump by a few hundred ms every 5 s, and can tick back up a second. `SETTLE_MS` (300) only covers one side of that. |
| 11 | Low | `history-export.js:62-63`; `ChainHistory.svelte:68-73` | After any mid-game resume the chain holds only `[opening, lastMove]`, because `GameStarted` has no chain field. The transcript numbers the last move "2." even when `result.chainLength` is 30, and the live chain shows two unrelated words as if they were adjacent. |
| 12 | Low (a11y) | `components/ChatPanel.svelte:170-186`; `game-apply.js:244-250` | `chatHistory` renumbers every line (`++chatOrdinal`), so on each reconnect all up to 20 `<li>` are re-keyed and re-inserted inside an `aria-live="polite" aria-relevant="additions"` log. A screen reader re-reads the whole conversation after every blip. |
| 13 | Low (tests) | `tests/word-input.test.js:3-8` vs the rest of the file | The header says the submit-clear is "pinned down", but no test submits. Also untested: clearing only when `onsubmit` returns true, refusing while composing, and the out-of-turn `beforeinput` guard / `undoInput` revert. `leaves an in-progress composition alone` (`:103-118`) would pass with the composition handling deleted, because on the player's own turn nothing else touches the value. |
| 14 | Low (tests) | none | No coverage for findings 1 to 8, which are all reconnect paths. `CountdownRing`, `PlayerStatus`, `GameOverPanel` (focus, export click), `Lobby` and `ArmedButton` have no component test. `client.js` storage guards (`safeSessionStorage`, `storeToken` throwing) have no test, unlike `settings.svelte.js`. No test asserts that each `fill(t.x, {…})` supplies exactly `t.x`'s placeholders. A scan run for this review found no mismatches today. |
| 15 | Nit | `vite.config.js:27` | "The two suites that need a DOM": five do (chat-panel, settings-store, ws-client, game-board, word-input). |
| 16 | Nit | `routes/online/+page.svelte:679` | `h1 { font-size: 1.3rem }` is still off the type ramp (deferred last time). `var(--text-5)` (1.5rem) or `--text-4` (1.125rem) is the nearest step. |

## Recommended fixes (each small)

1. **Game ended while away.** The client cannot detect this with the current protocol: a mid-game `RoomState` is normal. This needs a server change.
   - In `handleResume`'s `inLobby()` branch, send the seat its `GameOver` if a game finished while it was detached. The room would keep the last standings and reason per game.
   - Alternatively, add `bool in_game = 13;` to `RoomState`, and have `roomState` set `phase = 'lobby'` when it is false and `phase === 'playing'`.
   - Add a server test: 2 players, one drops, the game ends, the dropped player resumes inside grace, and it must receive `GameOver` (or `in_game: false`).
   - This crosses the web/server boundary, so it is the lead's call.
2. **Refused resume.**
   - Add `'session_not_resumable'` to `LEAVES_ROOM`. It is only ever sent in reply to `Hello` (`dispatch.go:277`, `room_presence.go:120`), so leaving on it is always right. `game.leave()` then lands `/online` on the join form with the banner. The `resuming` effect still clears it on mount as today.
   - `game_already_over` is shared with a dropped `Resign`, so do not blanket-leave on it. In `client.js`, remember that the last `Hello` carried a token. If the frame right after `welcome` is an `error` whose code is `session_not_resumable` or `game_already_over`, report it through a new `onResumeRefused` callback. `resumeFrom` sends the error in the same call as the Welcome, so nothing can come between them. `connection.svelte.js` then calls `game.leave()`.
   - On `/play`, the resulting `idle` phase needs a "start again" affordance, or simply `startGame(difficulty)`.
3. **`iAmOut`:** `return state.phase === 'playing' && (state.elimination !== null || state.gamePlayers.some((p) => p.isMe && p.eliminated));`. GameBoard's spectating box already handles `elimination === null`: it shows `youAreOut` without suggestions.
4. **`/play` reload:** call `forgetSession()` (already exported) in `startGame()` before `connect()`. Reload-means-new-game on the same rung is what the page's own comment describes (`play/+page.svelte:22-24`). Otherwise, if resuming a bot game is the intent, mirror `/online`: when `hasStoredSession()`, hold `session.request` until the resume answers. That choice is a product decision (see Unresolved).
5. **Held action vs resume:** gate `flushAction` on the seat being re-established, not on the socket being OPEN. The smallest version is to flush from an effect keyed on `game.state.roomPlayers`, which changes on the `RoomState` the resume broadcasts, when `heldAction` is set and the status is OPEN. The alternative is to set `session.startResume()` whenever the status leaves OPEN while `inRoom`, and let the existing `noteRoom()` clear it.
6. **Leave while offline:** in `leave()`, send when possible and never hold: `if (!dispatchAction({ kind: 'leaveRoom' })) {/* token is forgotten; grace frees the seat */}`, and drop the `act` wrapper there.
7. **Chat offline:** make `onsend` return `boolean` (`say = (text) => send(sendChat(text))`). In `submit()`, only clear on `true`. Add `disabled={!sendable || offline}` to the button, using the same `connection.status` read as WordInput. Add a chat-panel test with `onsend: () => false` that expects the field to still hold the text.
8. **Away countdown:**
   - Split each banner into a live sentence without the number, "X mất kết nối", inside `role="status"`.
   - Move the seconds into an `aria-hidden="true"` span, as the quick-match counter already does.
   - Optionally announce once at 10 s.
9. **GameOverPanel focus:** `if (game.state.result && !typingElsewhere()) panel?.focus();`. Hoist WordInput's `typingElsewhere` into a tiny `$lib/focus.js` so both components use it.
10. **Clock offset:** keep the sample with the smallest RTT among the last ~5 pongs (a ring of `{rtt, offset}`) and use that offset. This is the standard NTP-style filter, about 10 lines, and `tests/ws-client.test.js:278` already has the harness to cover it.
11. **Partial chain:**
    - In `chainToText`, number from the server: `const n = chainLength - (chain.length - 1 - index)`, with `chainLength` passed in from `game.state.chainLength`.
    - Insert a `…` line when `chain.length < chainLength`.
    - Optionally render one "…" row in ChainHistory under the same condition.
12. **Chat re-announce:** in `chatHistory`, reuse the ordinal of an existing line with the same `(atMs, playerId, text)`, or give the replacement a fresh `{#key}` wrapper outside the live region. The first keeps Svelte from re-inserting the nodes.
13. **WordInput tests:** add three.
    - Submit clears the field when `onsubmit` returns true and keeps it when it returns false.
    - Submit is refused during `compositionstart`.
    - An out-of-turn `input` is reverted to `lockedValue`.

    Replace or delete the vacuous composition test.

## The deferred global `.primary` class: worth doing now, partially

The same accent fill, hover, press and disabled block (about 15 lines) appears in:
- `online/+page.svelte:699-721`
- `ChatPanel.svelte:330-351`
- `+page.svelte:84-97`
- `Lobby.svelte:383-407`
- WordInput and GameOverPanel

**Recommendation:**
- Add a global `.primary` to `app.css` carrying only colour and state: background, colour, a transparent border, the transition, and `:hover`/`:active`/`:disabled` with `:not(:disabled)`. Components keep their own sizing and padding.
- Apply it to the online page, the landing page, ChatPanel (add `class="primary"` to its send button), WordInput and GameOverPanel. Their local rules only restate the colours, so deleting them changes nothing visually.
- **Leave Lobby alone** for now. Its scoped `.actions button { background: var(--surface) }` compiles to (0,2,1), which beats a global (0,1,0) `.primary`. Adopting the global class there first means moving that background into `.actions button:not(.primary)`, which is the part that needs a visual check.
- Do not use `:where(.primary)`: zero specificity would lose to every scoped base `button` rule.

**`online/+page.svelte` (808 lines).** About 390 of those lines are CSS. The concrete split that is still worth doing is the join-form branch (`:483-590` plus its about 120 lines of styles) into `JoinPanel.svelte`, which receives `session`, `named`, `codeInput` and the three request callbacks. It was deferred by a task decision last time, so it is not listed as a finding.

## Checked and clean

- **`game-apply.js`:**
  - `roomState` applies the whole snapshot at once and never merges it with the old one.
  - `turnUpdate` without `played` keeps the rejection.
  - `LEAVES_ROOM` runs before the error is set.
  - The chat window is trimmed.
  - The try/catch boundary in `apply()` is intact.
  - `MoveRejected.turn_seq` is ignored, but every path that moves `turnSeq` on the server also sends a `TurnUpdate` or replay on the same ordered socket. No failure scenario was found, so this is not reported.
- **`client.js`:**
  - Backoff resets on `welcome` and not on open.
  - Liveness ignores throttled ticks.
  - `reconnectNow` cannot create a second socket.
  - The terminal-error stop works.
  - `close()` and a later `connect()` never share a client instance, because `disconnect()` discards it, so the stale-`onclose` clobber is unreachable today. A late `onclose` from a discarded client cannot touch the status, since its own status is already CLOSED.
  - Both storage accessors are guarded at the property and at the call.
- **`settings.svelte.js`:**
  - Every read and write is guarded.
  - Corrupt JSON and non-finite scores degrade cleanly.
  - The nickname cap counts code points.
  - The theme is normalised, and matches the inline script in `app.html`.
- **Countdown maths** (`countdown.js`): clamped, rounded up, settle margin applied, and the rAF loop runs only while `running` and is cancelled on cleanup.
- **Runes:**
  - Every `$effect` that writes state either writes state it does not read or uses `untrack`. The online, play, PlayerStatus and ChatPanel effects were checked individually.
  - Timers and listeners are cleaned up: matchMedia, the quick-match interval, the stall, resume and arm timers, the PlayerStatus interval, the ResizeObserver, rAF, and the RoomCodePanel timer.
  - No `$effect` that should be a `$derived` was found. The ones that assign are bindable props or trigger-style latches.
- **i18n:**
  - `checkJs` + `strict` makes a mistyped `t.key` a `svelte-check` error.
  - No Vietnamese prose outside `vi.js`, except the static `<meta description>`/`<title>` in `app.html`.
  - A throwaway scan of every `fill(t.x, {…})` found no missing or extra placeholder.
  - Server error codes are guarded by `tests/error-codes.test.js`.
- **The rules page scoring constants** match `server/internal/game/engine.go:28-51` and `vietnamese.MinSyllables`.
- **Focus:**
  - A turn arriving focuses the word field unless the player is typing elsewhere.
  - A reconnect re-enables the field and focuses it.
  - Game over focuses the panel, apart from finding 9.
  - ArmedButton announces its armed state through `aria-pressed`.
- **Keyboard:** the difficulty picker uses native radios, the chain rows are buttons with `aria-expanded`/`aria-controls`, and the skip link targets `#main`.
- **Configs:** the Vitest alias and `browser` condition are correct, the Playwright server env is sane, and dependencies use caret ranges (moving, as the workspace rule prefers).

## Unresolved questions

1. Finding 4: should reloading `/play` mid-game resume the bot game or start a fresh one on the same rung? The page comment implies a fresh one, while `hasStoredSession()`'s doc implies a resume.
2. Finding 1: server replay of `GameOver` or a new `RoomState.in_game` field? The first needs no proto change. The second is a smaller change on each side but bumps the contract.
