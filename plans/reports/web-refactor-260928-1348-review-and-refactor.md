# Web review and refactor: dev vs main

Scope: `git diff main...dev -- web` (proto and lockfile excluded). Nothing committed.
Gates: `npm run lint` (0 errors, 0 warnings), `npm run check` (0/0), `npm test` (269 passed; the baseline was 266, plus 3 new tests).

## Findings

| Sev | Where (before the change) | Finding |
|---|---|---|
| High | `web/src/app.css` `.icon-button` (`--text-7`), `.rules-link` (`--text-4`), `.skip` (`--text-5`) | When the type ramp dropped from nine steps to seven, these three global rules kept their old token numbers. The meanings of those numbers changed: the dismiss "×" and the board's "?" went from 1.1rem to 2.25rem, rules links from 0.85rem to 1.125rem, and the skip link from 0.9rem to 1.5rem. The component files were remapped; app.css was missed. |
| Medium | `WordInput.svelte` style `button:hover/active:not(:disabled)` | The bare `button` rule also matched the `.fix` buttons. Its hover and press states outranked `.fix:hover`, so on hover or press the report button turned solid accent with dark text on top (low contrast). |
| Medium | `ChatPanel.svelte` draft vs field | The input unmounts while the panel is folded. After unfolding, the field was empty but `draft` still held the old text. "Gửi" stayed enabled and did nothing when pressed, and the typed text was lost. |
| Medium | `GameBoard.svelte` `:global(.resign)`, `:global(.claim-dead-end)`; `Lobby.svelte` `:global(.kick)` | These selectors were global without any scope, so they applied to every element in the app with that class. |
| Low | `online/+page.svelte` `ready/start/kick/leave/cancelQueue` | Five copies of `send(x); if (!sent) session.holdAction(...)`, next to a `dispatchAction` switch that already mapped each action to its message. |
| Low | `online/+page.svelte` resume timeout and resume error effects | The same three steps (`noteResumeFailed`, `forgetSession`, conditional `flush`) were written out in both places. |
| Low | `online/+page.svelte` queued-seconds effect | `resetQueued()` was called on both branches. |
| Low | `online/+page.svelte` doc above `ready()` | The comment described leaving the room, not readying. |
| Low | `play/+page.svelte` and `online/+page.svelte` | Identical `play`, `giveUp`, `claim` and `report` wrappers in both routes. |
| Low | `game-apply.js` `chatMessage` / `chatHistory` | The chat-line mapping was duplicated. The `error` case chained `||` comparisons across three code groups. `turnUpdate` had two separate `if (played)` blocks. |
| Low | `GameBoard.svelte` `canResign` / `canClaimDeadEnd` | Two identical deriveds. The `.resign` and `.claim-dead-end` CSS was about 80% the same, and `.claim-error` repeated `.error`. |
| Low | Banner CSS (`.error`/`.notice`) | Copied into GameBoard, Lobby and the online page, each with its own markup for the dismiss button. |
| Low | `ChatPanel.svelte` header doc | Said "Three facts" but listed two. Some spacing still used px literals where tokens exist. |
| Low | `room-session.svelte.js` `startResume` doc | Said "optionally behind a held join", but the function takes no argument. |
| Low | `ArmedButton.svelte` | `clearTimeout(timer); armed = false` appeared three times. |
| Low | `Lobby.svelte` `ownerAway` | Had an inline JSDoc param type that inference already covers. |
| Low | Tests | Finding codes in comments (`C6` in word-input, `C1`/"the review" in room-session and chat-panel). Each jsdom suite had its own copy of the gameStarted builder and the mount helper. `room-code.test.js:77` produced an `any` lint warning. |

No unused i18n keys were found: every `t.*` key has a `t.<key>` reference.

## Changes

- **app.css**: remapped `.icon-button` to `--text-4`, `.rules-link` to `--text-2` and `.skip` to `--text-2`, which are the same sizes they had on the old ramp. Updated the `.rules-link` comment, since the board header now uses a "?" icon-button.
- **New `AlertBanner.svelte`**: one `role="alert"` banner with `tone` (`error`/`notice`), `testid`, and an optional `ondismiss`. It replaces 8 hand-written banners in GameBoard (error, claim error), Lobby (`lobby-error`, `lobby-unsent`) and the online page (`name-needed`, `join-error`, `resume-failed`, `connect-stalled`). All test ids and roles are unchanged. GameBoard's `.offline` strip keeps its own markup because it is deliberately not announced and it carries the retry button.
- **`turnActions` in `ws/connection.svelte.js`**: `submit`, `resign`, `claimDeadEnd`, `reportWord`. Both game routes pass these to GameBoard, and the four duplicated wrapper functions in each route are gone. GameBoard's props contract is unchanged, so the component tests still inject their own handlers.
- **online/+page.svelte**:
  - `act(action)` = `dispatchAction` + hold-on-failure. `ready`, `start`, `kick`, `leave` and `cancelQueue` are now one-liners on top of it.
  - `abandonResume()` is shared by the timeout path and the error path.
  - The queued-seconds effect is simplified.
  - The misplaced doc comment is fixed.
- **game-apply.js**:
  - `toChatLine()` is used by both chat cases.
  - The error codes are grouped into named Sets: `LEAVES_ROOM`, `ANSWERED_BY_THE_BUTTON`, `ENDS_THE_QUEUE`.
  - The rejection clearing moved into the single `if (played)` block.
  - Behaviour is identical and the game-store tests pass unchanged.
- **GameBoard.svelte**: one `canPlayInsteadOfAWord` derived. The shared secondary-button CSS is written once as `.secondary :global(button)`, with the resign/claim colour differences layered on top and everything scoped under `.secondary`.
- **Lobby.svelte**: kick CSS scoped under `.seats :global(...)`, `ownerAway` simplified, the local `.error` CSS removed.
- **WordInput.svelte**: submit-button styles scoped to `.input-row button`. A `caretToEnd()` helper replaces two copies of the same `setSelectionRange` call.
- **ChatPanel.svelte**: an untracked, once-per-mount effect writes `draft` back into the field when it remounts. It never writes during a composition. The doc is fixed and px spacing moved to tokens.
- **ArmedButton.svelte**: added a `disarm()` helper.
- **room-session.svelte.js**: fixed the `startResume` doc.
- **Tests**:
  - New `tests/component-support.js` (`receive`, `startGame`, `render`), used by the game-board, word-input and chat-panel suites.
  - Removed the finding codes from comments and fixed the `room-code.test.js:77` warning with an `unknown`-to-`string` cast.
  - New tests: a fold/unfold round trip keeps a sendable chat draft (confirmed to fail without the fix); dismissing a board error; a refused claim appears beside the claim/resign row and can be dismissed.
- **tests/error-codes.test.js**: the scanner now also picks up the two code literals at `toRoom(limiter, notIn, dropped, …)` call sites. Server work in progress (not mine) moved `not_in_a_game` behind that helper in `server/internal/wsapi/dispatch.go`, and without this the "no stale message" guard failed.

## Bugs fixed (user-visible)

1. The dismiss "×", the board's "?" button, rules links and the skip link are back to their intended sizes (they had grown to as much as 2.25rem).
2. The report-word button no longer turns solid accent with dark text on hover or press.
3. A chat draft now survives folding and unfolding the panel, and "Gửi" no longer stays enabled over an empty field.
4. The resign, claim and kick button styles no longer apply to unrelated elements elsewhere in the app that share those class names.

## Checked

- The e2e selectors (`getByTestId`, `getByRole('alert')`, `.badge`, `.role`) and the `.resign`/`.claim-dead-end`/`.suggestion` classes used by the unit tests are all still present. Playwright was not run (no browser on this host).
- The wire contract is untouched: `messages.js` did not change, and `turnActions` calls the same builders.

## Deferred

- A global `.primary` accent-button class. The same fill, hover, press and disabled styles are repeated in six places (online page, Lobby, WordInput, ChatPanel, GameOverPanel, landing page). Consolidating them runs into Lobby's `.actions button` specificity and needs a visual check that this host cannot do.
- `rules/+page.svelte` repeats the section id in the table of contents and in each `<section>`. It could be generated from one array, but it is static and easy to read as it is.
- `online/+page.svelte` `h1 { font-size: 1.3rem }` is off the type ramp.
- `startResume()` + `holdPendingJoin()` could be merged into a single `startResume(heldJoin?)`. Left alone to keep the store API and its tests stable.
- The guard in `error-codes.test.js` depends on the server's call-site shape. If the in-flight server refactor changes `toRoom` again, the regex will need updating too.
