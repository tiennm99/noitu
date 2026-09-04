---
title: "Phase 6: SvelteKit Frontend"
status: todo
phase: 6
priority: P1
effort: "5d"
dependencies: [4, 5]
---

# Phase 6: SvelteKit Frontend

## Overview

The playable UI: a SvelteKit SPA in JavaScript that connects over WebSocket, speaks
Protobuf, and renders the game. Vietnamese throughout, dark mode, local high score. This
phase delivers the complete vs-bot experience; phase 7 layers the online 1v1 flows on top.

## Requirements

**Functional**
- [ ] Home screen: nickname input, play vs bot (Easy / Trung bình / Khó), or go to online play
- [ ] Nickname persisted in `localStorage`, sent in `Hello`, and replaced by `accepted_nickname` from `Welcome`
- [ ] Game screen: chain history, current syllable prompt, input, countdown ring, both scores
- [ ] Rejections shown as specific Vietnamese messages, mapped from `RejectReason`
- [ ] Game-over screen: result, final score, new-personal-best marker, rematch / home
- [ ] Dark mode toggle, persisted; respects `prefers-color-scheme` on first visit
- [ ] Personal best per difficulty in `localStorage`
- [ ] Connection status indicator; automatic reconnect with backoff
- [ ] In-app footer credit for the CC BY-SA 4.0 dictionary source, with links

**Non-functional**
- [ ] `adapter-static` build, served by the Go binary
- [ ] Mobile-first; usable one-handed on a phone with the keyboard up
- [ ] No wordlist or dictionary data in the client bundle
- [ ] Vietnamese diacritic input (Telex/VNI IMEs) works — no input interception that breaks composition

## Architecture

**Stack:** SvelteKit 2 + Svelte 5 (runes: `$state`, `$derived`, `$effect`), plain JavaScript
with JSDoc types, `adapter-static`, Vite. Scaffold with `npx sv create`.

```
web/src/
  lib/
    proto/          # generated (phase 4) — do not hand-edit
    ws/client.js    # socket lifecycle, protobuf codec, reconnect backoff, ping/pong
    ws/messages.js  # thin senders: startBotGame(), submitWord(), createRoom(), joinRoom()
    stores/game.svelte.js    # $state game model, fed only by ServerMessage
    stores/settings.svelte.js# nickname + theme + high scores, localStorage-backed
    i18n/vi.js      # ALL user-facing strings, incl. RejectReason → message map
    components/     # ChainHistory, WordInput, CountdownRing, ScoreBoard, DifficultyPicker,
                    # ConnectionBadge, GameOverPanel, ThemeToggle, AttributionFooter
  routes/
    +layout.svelte  # theme application, footer
    +page.svelte    # home
    play/+page.svelte     # game screen (bot and PvP share it)
    online/+page.svelte   # room create/join (phase 7 fills this in)
```

**State rule:** the store is a projection of server messages. The client never decides
whether a word is valid, whose turn it is, or who won — it renders what the server sent.
The only client-owned state is theme, personal best, and the input box.

**Countdown:** derived from `deadline_unix_ms` minus a clock offset measured by `Ping`/`Pong`,
re-evaluated on `requestAnimationFrame`. Purely cosmetic — the server decides expiry.

**Reconnect:** exponential backoff (0.5s → 8s, jittered). On reconnect send `Hello` with the
stored `resume_token`; the server either restores the game or returns a typed error and the
UI falls back to the home screen with an explanation.

**Vietnamese input:** bind on `change`/submit and read `event.target.value`; never
re-write the input's value mid-composition, and listen for `compositionstart`/`compositionend`
before any normalization — rewriting the field while a Telex IME is composing corrupts
diacritic entry. Normalization is the server's job anyway.

**i18n:** every string lives in `lib/i18n/vi.js`, including the `RejectReason` map:

| Reason | Message |
|---|---|
| `TOO_FEW_SYLLABLES` | "Từ phải có ít nhất 2 tiếng." |
| `WRONG_LINK` | "Từ phải bắt đầu bằng tiếng \"{syllable}\"." |
| `NOT_IN_DICTIONARY` | "Không tìm thấy từ này trong từ điển." |
| `ALREADY_USED` | "Từ này đã được dùng rồi." |
| `NOT_YOUR_TURN` | "Chưa đến lượt bạn." |
| `TIMEOUT` | "Hết giờ!" |

**Theming:** CSS custom properties on `:root`, `[data-theme="dark"]` override, set from the
settings store before first paint to avoid a flash.

## Related Code Files

- Create: `web/` — SvelteKit scaffold (`package.json`, `svelte.config.js`, `vite.config.js`, `jsconfig.json`)
- Create: `web/src/lib/ws/client.js`, `web/src/lib/ws/messages.js`
- Create: `web/src/lib/stores/game.svelte.js`, `web/src/lib/stores/settings.svelte.js`
- Create: `web/src/lib/i18n/vi.js`
- Create: `web/src/lib/components/*.svelte` (per list above)
- Create: `web/src/routes/+layout.svelte`, `+page.svelte`, `play/+page.svelte`, `online/+page.svelte`
- Create: `web/src/app.css` — tokens, light/dark palettes
- Create: `web/src/lib/ws/client.test.js`, `web/src/lib/stores/game.test.js` (Vitest)
- Modify: `Makefile` — `make web`, `make web-dev`
- Modify: `server/internal/wsapi/server.go` — serve `NOITU_WEB_DIR` with SPA fallback

## Implementation Steps

1. Scaffold with `npx sv create web` (SvelteKit 2 / Svelte 5, JavaScript, Vitest, ESLint+Prettier); switch to `adapter-static` with `fallback: 'index.html'`.
2. Wire `make proto` output into `web/src/lib/proto`; add `@bufbuild/protobuf`.
3. `ws/client.js`: connect (`ws://` in dev via Vite proxy, same-origin `wss://` in prod), binary frames, decode to `ServerMessage`, dispatch to the store, ping/pong clock offset, backoff reconnect, `resume_token` in `sessionStorage`.
4. `stores/game.svelte.js`: `$state` model (phase, chain, currentSyllable, myTurn, deadline, scores, lastRejection, gameOver); one reducer per `ServerMessage` variant.
5. `stores/settings.svelte.js`: nickname, theme (system default, then explicit), `bestScore[difficulty]`, all `localStorage`-backed with try/catch so private-mode browsing still works.
6. Components: `ChainHistory` (scrolling word list, most recent pinned and highlighted, syllable-count badge on 3+ syllable words), `WordInput` (IME-safe, disabled when not your turn, autofocus on turn start), `CountdownRing` (SVG arc from the derived remaining time, colour shift under 5s), `NicknameInput` (20-char cap, mirrors the server rule), `ScoreBoard`, `DifficultyPicker`, `ConnectionBadge`, `GameOverPanel`, `ThemeToggle`, `AttributionFooter`.
7. Routes: home (nickname + mode + difficulty), `play` (the board, works for bot now and PvP in phase 7), `online` stub.
8. `i18n/vi.js` with every string and the rejection map; assert in a test that each proto `RejectReason` has an entry.
9. Personal best: on `GameOver`, compare and store per difficulty; show a "kỷ lục mới" marker.
10. `AttributionFooter`: names `minhqnd/dictionary`, links the repo and the CC BY-SA 4.0 deed — the user-visible half of the license obligation.
11. Vitest: store reducers for each server message, reconnect backoff schedule, rejection-map completeness, high-score persistence with `localStorage` throwing.
12. Verify the production bundle contains no word data (`grep` the built assets for known words) and record the bundle size.

## Success Criteria

- [ ] Full vs-bot game playable in a browser at all three difficulties
- [ ] Words of 2, 3, and 4 syllables are all accepted and scored, with the syllable bonus visible
- [ ] Nickname persists across reloads; the displayed name is always `accepted_nickname` from the server, never the raw input
- [ ] Every rejection reason renders its specific Vietnamese message
- [ ] Countdown matches the server deadline within ~200ms; expiry is announced by the server, not the client
- [ ] Dark mode persists across reloads with no flash of the wrong theme
- [ ] Personal best per difficulty persists and updates
- [ ] Killing the server mid-game shows a connection state and reconnects when it returns
- [ ] Diacritics typed with a Telex IME enter correctly on desktop and mobile
- [ ] `npm run build` → `adapter-static` output served correctly by the Go binary, deep links included
- [ ] Built assets contain no dictionary words
- [ ] Attribution footer present with working links
- [ ] `npm test` green

## Risk Assessment

| Risk | Signal | Response |
|---|---|---|
| IME composition broken by input handling | Diacritics mangled while typing | Never rewrite the input value during composition; guard with `compositionstart`/`end`. Explicitly tested on a real mobile keyboard, not just a desktop emulator |
| Countdown drift makes a legitimate move look late | Player sees 2s left, server says timeout | Clock offset from `Ping`/`Pong`; the ring visually settles ~300ms early so the client never claims more time than the server allows |
| Svelte 5 runes in `.svelte.js` store files misused | Reactivity silently stops updating | Stores use the documented `.svelte.js` rune-module pattern; store reducers are unit-tested outside components |
| Protobuf runtime bloats the bundle | Bundle budget exceeded | Measured in step 12; `protobuf-es` is tree-shakeable and only generated messages are imported |
| `localStorage` unavailable (private mode / blocked) | Crash on load | Every read/write wrapped in try/catch with a working in-memory default |
| Dev/prod WS URL divergence | Works in `npm run dev`, breaks when served by Go | Same-origin `wss://` resolved from `location` in prod; Vite proxy only in dev; both paths exercised before phase 7 |
