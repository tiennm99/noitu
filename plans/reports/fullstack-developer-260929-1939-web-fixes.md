# Web fixes from the review

Branch `dev`, 2026-09-29. Not committed. Only files under `web/` changed. Playwright was not run (no browser), and no e2e spec was added or changed.

## Gates

`cd web && npm run check && npm run lint && npm test`: check 0 errors and 0 warnings (386 files), lint clean, 326 tests passed in 18 files (was 270 in 16). `tests/bundle.test.js` is in that run and passes.

Fail-without-fix check: I reverted each behavioural fix one at a time and ran its test file. Every revert made at least one test fail, and each file was restored afterwards. The reverts were:
- `session_not_resumable` in `LEAVES_ROOM`
- the `iAmOut` row check
- the ordinal reuse in the chat history
- the numbering in `chainToText`
- `freshSession`
- the `game.leave()` on a refused resume
- the `onResumeRefused()` call in `client.js` (4 tests fail)
- the shortest-RTT clock filter
- the GameOverPanel focus guard
- the ChatPanel refused-send guard
- the ChatPanel offline disable
- the away banner's `role="status"`

Finding 1 is the exception: it needed no client change, so there was nothing to revert.

## Per finding

1. **Game ended while away.** No client change was needed. The reducer already handles the replay sequence from `playing`: `RoomState` fills in the room and leaves the phase alone, `ChatHistory` replaces the chat, and `GameOver` then moves the phase to `over` with the result and standings. The lobby actions appear because `over` renders the Lobby. Three store tests pin it: the exact sequence lands on `over` with standings, the room snapshot survives (so Ready and Leave are available), and a mid-game `RoomState` alone does not end the game. These tests pass on the unmodified reducer, so they are regression pins and not fail-without proofs.
2. **Refused resume.**
   - `session_not_resumable` is now in `LEAVES_ROOM` (`game-apply.js`).
   - `client.js` tracks whether the current Hello carried a token. After the Welcome, an `error` that arrives before any other non-Pong frame is a refused resume. The client then drops the spent token and calls the new `onResumeRefused` option. Pongs are skipped because the client pings right after Hello. `connection.svelte.js` wires the option to `game.leave()`. It runs before the error is forwarded, so the board clears and the error banner still shows.
   - Tests in `ws-client.test.js` cover: both codes, a Pong in between, an error after the restored room (no report), a Hello without a token (no report), a spent token, and error forwarding. Two tests in `connection.test.js` and one in `game-store.test.js` cover the rest.
   - On `/play`, an in-game refusal now lands on an idle board with the error banner. I did not add a "start again" button.
3. **`iAmOut`.** It is now true when `elimination !== null` or the player's own row in `gamePlayers` is eliminated. Two tests: out from the row alone, and another player's elimination does not count.
4. **`/play` reload.**
   - `connect({ freshSession: true })` forgets the stored token before a new client is created. `startGame()` uses it.
   - I did not call `forgetSession()` unconditionally before `connect()`. A rematch calls `startGame()` again on the socket that already exists. Forgetting there would delete the token this game's Welcome had just stored, and a later socket drop would open a fresh session instead of resuming the game. The flag only acts when a new client is created, so an existing socket keeps its token.
   - `forgetStoredSession()` is now a module-level export in `client.js`, and the client's `forgetSession` is the same function. It is needed because there is no client yet on the first mount.
   - `connection.test.js` checks the first Hello carries no token with the flag, carries the stored token without it, and that a second `connect({freshSession})` on a live socket keeps the Welcome's token.
   - The `+page.svelte` call site itself is not covered, because there is no page-level test harness.
5. **Held action vs resume.** In `online/+page.svelte` the flush effect now waits for the resume's `RoomState` before flushing a held action, if the socket dropped while in a room.
   - It notes the roster when the socket drops. After reopening, it releases the action once the roster object changes, since each `RoomState` replaces it.
   - If the resume is refused and the room is gone, the held action is cleared instead of sent.
   - An action held outside a room (cancelling the queue) is still flushed as soon as the socket opens.
   - This is page logic and has no unit test. It needs a human or e2e check.
6. **Leave while offline.** `leave()` now sends `LeaveRoom` if the socket can carry it and otherwise drops it, never holds it. It also clears any other held action. The seat is freed by grace expiry either way.
7. **Chat offline.** `onsend` returns a boolean and `say()` returns `send()`'s result. The draft is cleared only when the send went out. The send button is disabled while `connection.status !== OPEN`, and `submit()` also refuses offline. New tests: a refused send keeps the text, and the button is disabled and nothing is sent while reconnecting. The existing chat tests now set the connection to open.
8. **Away countdown.** Each banner's `role="status"` span holds only "X mất kết nối…". The seconds sit in a sibling `aria-hidden` span. `data-testid="away-…"` is on the outer `<p>`, so the e2e `toContainText('… mất kết nối')` still matches. `vi.js` swaps `playerDisconnectedIn` for `playerDisconnectedSeconds: '({n}s)'`.
9. **GameOverPanel focus.** New `src/lib/focus.js` exports `typingElsewhere(own)`. WordInput imports it instead of its private copy, and GameOverPanel only focuses when the player is not typing elsewhere. Tested with a focused external input.
10. **Clock offset.** The client keeps the last 5 pong samples `{rtt, offset}` and uses the offset of the one with the smallest RTT. The existing single-sample test is unchanged. New tests: a slow pong does not move the offset, a faster pong takes over, and an old fast sample ages out.
11. **Partial chain.**
    - `chainToText` numbers from `result.chainLength`: the opening word is always 1, and later words are numbered back from the total. It inserts a `…` line after the opening when words are missing (new string `exportGap`). Two tests cover a partial and a complete chain.
    - Skipped: the optional "…" row in `ChainHistory` on screen. It was marked optional.
12. **Chat re-announce.** A `chatHistory` line matching an on-screen line on `(atMs, playerId, text)` keeps that line's ordinal, so its keyed row is not re-inserted into the live region. Identical duplicate lines each claim one old line. Two tests.
13. **WordInput tests.**
    - Added: submit clears the field only when `onsubmit` returns true, and submit is refused during composition (and works after `compositionend`).
    - Added: `beforeinput` is cancelled out of turn and not on the player's turn, and a composition's text is reverted out of turn.
    - The vacuous composition test is rewritten as "leaves the player's own text alone on their turn". It fails if the revert ignores `enabled`, but its purpose is to pin the on-turn behaviour.
14. **Tests.**
    - New `tests/status-components.test.js` covers `CountdownRing`, `PlayerStatus`, `GameOverPanel` and `ArmedButton`. Details are below.
    - `ws-client.test.js` now covers storage that throws on read, on write, on remove, and a `sessionStorage` property that throws.
    - New `tests/reactive-props.svelte.js` is a helper only, so a test can change a mounted component's props (the `$state` rune needs a `.svelte.js` module).
    - `i18n.test.js` has a new test that scans `src/**` for `fill(t.key, {…})` calls and checks the object's keys equal the template's placeholders. It asserts more than 10 calls were matched, and it cannot see calls whose template is chosen by a conditional, which the test comment notes.
    - Skipped: a `Lobby` component test. It was marked "if time allows", and Lobby is large and mostly wiring.
15. **`vite.config.js`.** The comment no longer says "two suites".
16. **`h1` on `/online`.** Now `var(--text-5)`, which is what the rules page `h1` uses. The size goes from 1.3rem to 1.5rem, a small visible change.

`status-components.test.js` contents:
- `CountdownRing`: idle dash, rounded-up seconds and label, urgent only on the player's own turn, stalled instead of urgent offline, and the spoken 10 s mark.
- `PlayerStatus`: banner structure, countdown to zero, banner removed on return.
- `GameOverPanel`: standings and rank classes, no table for a one-row result, focus taken, focus refused while typing elsewhere, rematch button only when supplied, and export producing a `.txt` download.
- `ArmedButton`: arm, confirm, timeout disarm, disarm on `disabled`.

## Other changes

- **Footer link (security review L5, footer part).** `data/ATTRIBUTION.md` (the nine-item modification list) is not served over HTTP by the server or the web build. `AttributionFooter` now links "xem danh sách thay đổi" to `https://github.com/tiennm99/noitu/blob/main/data/ATTRIBUTION.md`. I confirmed the file exists on `main`. Strings `attributionModified` and `attributionChanges` are in `vi.js`. The rules page was not changed.
- **New error message.** `errcodes.go` now has `unknown_message`, added concurrently by the server agent. It made `tests/error-codes.test.js` fail, so I added a Vietnamese message for it in `vi.js`: "Máy chủ không hiểu yêu cầu này. Hãy tải lại trang." Please confirm the wording.

## `.primary` consolidation

`app.css` now has a global `.primary`: `border-color: transparent`, `background: var(--accent)`, `color: var(--accent-text)`, `transition: background-color 150ms ease-out`, plus `:hover:not(:disabled)` to `--accent-hover`, `:active:not(:disabled)` to `--accent-pressed`, and `:disabled` to `--surface-alt` background and `--text-muted` colour. It does not use `:where()`. Focus-visible is unchanged, since the global `:where(...)` focus rule was never touched. Sizing, padding, radius, weight and border width stay local.

Rules removed:
- **`online/+page.svelte`:**
  - `.primary` lost `background`, `color` and `transition`, keeping `min-height`, `padding`, `border: 0`, `border-radius` and `font-weight`.
  - Removed `.primary:hover:not(:disabled)`, `.primary:active:not(:disabled)` and `.primary:disabled`.
  - These were identical to the global ones.
- **`ChatPanel.svelte`:**
  - `.row button` lost `background`, `color` and `transition`.
  - Removed `.row button:hover:not(:disabled)`, `.row button:active:not(:disabled)` and `.row button:disabled`.
  - The send button gained `class="primary"`.
- **`WordInput.svelte`:**
  - `.input-row button` lost `background`, `color` and `transition`.
  - Removed `.input-row button:hover:not(:disabled)`, `:active:not(:disabled)` and `:disabled`.
  - The submit button gained `class="primary"`.
  - The `.fix.suggestion` rules are untouched.
- **`+page.svelte` (landing):**
  - Removed `.actions .primary`, `.actions .primary:hover` and `.actions .primary:active`.
  - The old hover and active had no `:not(:disabled)`, but that button is never disabled.
  - `.actions > *` kept size, padding, radius and weight. It no longer sets the border colour, background or `color: inherit`.
  - A new `.actions > :where(:not(.primary))` sets `border-color`, `background: var(--surface)` and `color: inherit`.
  - `border` was split into `border-width: 1px; border-style: solid`, so the shorthand's `currentcolor` cannot beat the global transparent.
- **`GameOverPanel.svelte`:**
  - Removed `.actions .primary`, `.actions .primary:hover` and `.actions .primary:active`.
  - `.actions button` kept its sizing and `border-width`/`border-style`. The border colour and `background: var(--surface)` moved to `.actions button:where(:not(.primary))`.
  - `:where()` keeps that rule's specificity at (0,1,1), so `.actions .export` still overrides it as before.

I checked the compiled selectors of the landing page and GameOverPanel with the Svelte compiler.

The report claimed the local rules only restated the colours. For the landing page and GameOverPanel that was wrong: `.actions > *` and `.actions button` set a background that would have beaten the global class. The `:where(:not(.primary))` split above is the fix. Lobby is untouched.

## Needs a human visual check (no browser here)

- The four `.primary` surfaces: landing "Chơi với máy", `/online` quick match and create buttons, the chat send button, and the word submit button. Check rest, hover, press, disabled and the dark theme.
- The GameOverPanel action row: the rematch button is accent-filled, home is a bordered button, and the export button is transparent and muted.
- The `/online` `h1` at 1.5rem.
- The away banner: the seconds should sit inline after the sentence, with a space between them.
- The footer: the extra sentence and link should still fit on a phone width.
- Behaviour that needs a real reconnect, ideally an e2e run when a browser exists:
  - Finding 5: kick or ready right after a socket cut.
  - Finding 6: leave while offline.
  - Finding 7: chat button disabled during a cut.
  - Finding 4: reload `/play` mid-game.
  - Finding 1 end to end, once the server change lands.

## Notes

- After a refused resume on `/play` the player sees an idle board with a banner and must use Home or reload. An automatic restart would be a product call.
- The server's replay for Finding 1 was not verified against the wsapi change, which is being written concurrently. The client relies only on the frame order given in the task.

Status: DONE_WITH_CONCERNS
Summary: All 16 findings, the `.primary` consolidation and the footer link are done in `web/`; check, lint and 326 tests pass, and each behavioural fix has a test that fails when reverted (Finding 1 needed no client change).
Concerns: The Finding 5 page logic, the `/play` `freshSession` call site and all CSS changes are unverified without a browser (list above). I added a Vietnamese string for the new server code `unknown_message`. I did not add an e2e spec or a Lobby component test.
