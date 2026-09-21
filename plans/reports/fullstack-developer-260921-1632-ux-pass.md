# Whole-game UX pass — implementation report

Branch: `worktree-agent-a4990d1e576b04b80`
Worktree: `/workspace/tiennm99/noitu/.claude/worktrees/agent-a4990d1e576b04b80`
Base: fast-forwarded to `dev` `8c5bb8b` before starting (had `room-session.svelte.js`, `ArmedButton.svelte`, `chat-panel.test.js`).
Source: `plans/reports/ui-ux-designer-260921-1529-whole-game-ux-review.md`.

## Per-item summary

- **#1** Already done on `dev` (in-room rules links are `target="_blank"`). No change.
- **#2** Landing: `howToPlay`/`howToPlayExample` line added between the tagline and the nickname field (`routes/+page.svelte`). `ChainHistory.svelte`'s opening row gets an `openingCaption` line styled like `.corrected`.
- **#3** `GameBoard.svelte` header: `ConnectionBadge` gained a `compact` prop (dot-only, `sr-only` label, while `status === 'open'`); "Luật chơi" is now a 44px `.icon-button` anchor with `aria-label`; the room-code/difficulty `.mode` line moved from the header into `.prompt`, under the syllable. Chat pill kept its `data-testid="chat-pill"`/`chatOpen` `aria-label`; visible text dropped to an emoji + `sr-only` title + bare unread number (aria-label unchanged, still spells it out). `room-code`, `player-count`, `scoreboard`, `chat-toggle` testids untouched (they live in `RoomCodePanel`/`Lobby`/`ScoreBoard`/`ChatPanel`, not this file).
- **#4** `WordInput.svelte`: suggestion/report moved onto their own `.fixes` row at a 36px floor; suggestion is accent-filled ("primary"), report stays outlined. Classes kept as `suggestion`/`report` (plus `fix`) so `word-input.test.js`'s selectors still match. `useSuggestion`/`report` logic untouched — still fill-only, field still uncontrolled.
- **#5** "Bí từ" and "Đầu hàng" now share one `.secondary` row, both `ArmedButton`, both always mounted while `phase === 'playing' && !iAmOut`, disabled off-turn via `ArmedButton`'s own disarm-on-disable behaviour rather than being mounted/unmounted.
- **#6** Spectating box (`.spectating`, `data-testid="eliminated"`, moved here from `PlayerStatus.svelte`) now shows `youAreOut` plus the elimination suggestions or `noSuggestions`; the duplicate banner in `PlayerStatus.svelte` (and its `.banner.gone` style) is removed. `game-shape.js`'s `Elimination` typedef gained `syllable`; `game-apply.js` captures `state.currentSyllable` into it at knockout. `GameOverPanel.svelte` now reads `elimination.syllable` instead of the (possibly-moved-on) `game.state.currentSyllable`.
- **#7** `rejectMessages[NOT_YOUR_TURN]` extended to name the race for a submitted word. `errorMessages.not_your_turn` rewritten for the resign/claim race and routed (`game-apply.js`) into `state.claimError` alongside `not_a_dead_end`, so it renders beside the secondary row rather than the top banner.
- **#8** `online/+page.svelte`: the seconds counter moved out of the `role="status"` paragraph into a sibling `aria-hidden` one; `quickMatchWaiting` copy dropped its embedded `{n}`. `quickMatchNudge`/`quickMatchNudgeLink` reworded to say the nudge leaves the queue. The nudge's `/play` link now reads `settings.state.lastDifficulty` (new store field, persisted to `localStorage`, set from the landing `DifficultyPicker`'s `onselect`), falling back to plain `/play` when unset. Added `quickMatchHint`/`createRoomHint` sub-labels under the two entry buttons.
- **#9** `ChainHistory.svelte`: `.parts` is now a `<ul>` of pill `<li>`s (`aria-label={t.pointsBreakdown}`), rendered only when `index === 0 || open`. `pointKindLabels[SYLLABLES]` renamed `dài` → `từ dài`.
- **#10** `app.css`: ramp collapsed 9→7 with the review's exact values; `--accent-hover`/`--accent-pressed` added (light + dark) and wired into every accent-filled button (`WordInput`, `ChatPanel`, `Lobby`, `GameOverPanel`, both `routes/+page.svelte` and `routes/online/+page.svelte` primaries); light `--bg` lifted to `#eef3ef`. No webfont added — system stack kept; the syllable's old `1.6rem` literal and the countdown ring's `1.5rem` literal now read `var(--text-7)`/`var(--text-5)` per the review's table. Every existing `var(--text-N)` reference across all components was remapped in one pass (old 1/2→new 1, old 3/4/5→new 2, old 6→3, 7→4, 8→5, 9→6) so nothing changed size except the two literals above. The 34 counted `6px`/`10px`/`14px` spacing literals became `--space-2`/`--space-3`/`--space-4`. Left un-tokenized: the countdown ring's `1.75rem` urgent-value literal and `online/+page.svelte`'s `1.3rem` `h1` (no ramp step given for either in the review's table).
- **Copy** (`vi.js` + `app.html`): `howToPlay`/`howToPlayExample`, `openingCaption`, `difficultyHints` (new table, tested), `nicknameHint` (collapsed to one sentence), `onlineIntro` + `quickMatchHint`/`createRoomHint`, `quickMatchNudge`/`quickMatchNudgeLink`, `not_a_dead_end`, `winsLabel`/new `winsCompact` (split lobby vs. board), `need_more_players`, `opponentTurn`, `chatAuthorLeft`, meta description, `too_fast`, `pointKindLabels.SYLLABLES`, `pointsBreakdown`. `noBestScore` removed (superseded by `difficultyHints`). `spectating` key removed (superseded by the merged `youAreOut`).

## Files touched

`web/src/app.css`, `web/src/app.html`, `web/src/lib/i18n/vi.js`, `web/src/lib/stores/{game-apply,game-shape,settings.svelte}.js`, `web/src/lib/components/{AttributionFooter,ChainHistory,ChatPanel,ConnectionBadge,CountdownRing,DifficultyPicker,GameBoard,GameOverPanel,Lobby,NicknameInput,PlayerStatus,RoomCodePanel,ScoreBoard,ThemeToggle,WordInput}.svelte`, `web/src/routes/{+layout,+page,online/+page,rules/+page}.svelte`, `web/tests/{game-board,game-store,i18n,settings-store}.test.js`, `web/e2e/pvp-game.spec.js`.

Not touched: `server/`, `proto/`, `web/src/lib/proto/`, `word-input.test.js`, `chat-panel.test.js` (no markup change needed in either — verified their selectors/assertions still hold against the new markup).

## Tests added

- `game-board.test.js`: "the persistent claim/resign row" — both buttons mounted and enabled on-turn; both stay mounted and become `disabled` (not removed) once the turn moves on.
- `game-store.test.js`: `not_your_turn` routes to `claimError`, not the top banner (mirrors the existing `not_a_dead_end` test); elimination captures the syllable the player was stuck on and keeps it after `currentSyllable` moves on.
- `i18n.test.js`: `difficultyHints` walked against `DifficultySchema` the same way `difficultyLabels` already is.
- `settings-store.test.js`: `lastDifficulty` starts `null`, persists across a reload, ignores a non-numeric stored value, survives hostile storage.

## e2e (unverified — no browser on this host)

Two `pvp-game.spec.js` text assertions changed to match the new copy and are **unverified**:
- `wins-p1`/`wins-p2` (Lobby): `'Tỉ số 1'` → `'Ván thắng 1'` (and `0`).
- `series-p2` (ScoreBoard): `'Tỉ số 1'` → `'Thắng 1'`.
- `lobby-error` too-fast check: `'Bạn thao tác quá nhanh'` → `'Thao tác quá nhanh'`.

No other e2e-referenced testid was renamed (`room-code`, `player-count`, `scoreboard`, `chat-toggle`, `chat-pill`, `eliminated`, `player-out`, `turn-indicator`, `current-syllable`, `word-submit`, `standings`, `ready`/`wins-*`/`series-*` all kept). `eliminated` moved from `PlayerStatus.svelte`'s banner onto `GameBoard.svelte`'s merged spectating box — same attribute, same "is it visible" assertion in `pvp-game.spec.js:705`, not expected to need a spec change, but unverified since it cannot be run here.

## Verification tail

```
npm run lint    → 0 errors, 1 pre-existing warning (tests/room-code.test.js, unrelated)
npm run check   → 384 files, 0 errors, 0 warnings
npm test        → vite build succeeds; 16 test files, 266 tests passed
```

## Deferred (out of the requested scope)

Everything in the review not named in the deliverable list was left alone by design (KISS — the review's other findings are recorded there for a future pass): the online entry's two-primary-button emphasis, `/online`'s missing rules link, rules-page `.toc`/`.back` sizing, wide-screen board width cap, urgent-clock board glow, game-over `finalScore`/🏆 `role="img"`, lobby seat-colour/kick-disabled-reason, chat timestamp/contrast tweaks, reconnect-stalled ring dash, and the two motion micro-interactions.

## Unresolved questions

1. `.ring.urgent .value`'s `1.75rem` and `online/+page.svelte`'s `h1` `1.3rem` are the two font-size literals the review flagged that have no explicit new-ramp mapping in its table (unlike the syllable's `1.6rem` and the ring's base `1.5rem`, which do). Left as literals rather than guessing a step.
2. `eliminated`'s e2e assertion (`pvp-game.spec.js:705`, `toBeVisible()` only, no text check) should still pass since the testid moved intact to the new merged box, but this is unverified without a browser.
