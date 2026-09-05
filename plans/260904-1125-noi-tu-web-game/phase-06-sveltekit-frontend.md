---
title: "Phase 6: SvelteKit Frontend"
status: done
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
- [x] Home screen: nickname input, play vs bot (Easy / Trung bình / Khó), or go to online play
- [x] Nickname persisted in `localStorage`, sent in `Hello`, and replaced by `accepted_nickname` from `Welcome`
- [x] Game screen: chain history, current syllable prompt, input, countdown ring, both scores
- [x] Rejections shown as specific Vietnamese messages, mapped from `RejectReason`
- [x] Game-over screen: result, final score, new-personal-best marker, rematch / home
- [x] Dark mode toggle, persisted; respects `prefers-color-scheme` on first visit
- [x] Personal best per difficulty in `localStorage`
- [x] Connection status indicator; automatic reconnect with backoff
- [x] In-app footer credit for the CC BY-SA 4.0 dictionary source, with links

**Non-functional**
- [x] `adapter-static` build, served by the Go binary
- [ ] Mobile-first; usable one-handed on a phone with the keyboard up
- [x] No wordlist or dictionary data in the client bundle
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

- [ ] Full vs-bot game playable in a browser at all three difficulties — the protocol path is verified headlessly at all three; no browser playtest yet
- [ ] Words of 2, 3, and 4 syllables are all accepted and scored, with the syllable bonus visible
- [x] Nickname persists across reloads; the displayed name is always `accepted_nickname` from the server, never the raw input
- [x] Every rejection reason renders its specific Vietnamese message
- [ ] Countdown matches the server deadline within ~200ms; expiry is announced by the server, not the client
- [ ] Dark mode persists across reloads with no flash of the wrong theme
- [x] Personal best per difficulty persists and updates
- [ ] Killing the server mid-game shows a connection state and reconnects when it returns
- [ ] Diacritics typed with a Telex IME enter correctly on desktop and mobile
- [x] `npm run build` → `adapter-static` output served correctly by the Go binary, deep links included
- [x] Built assets contain no dictionary words
- [x] Attribution footer present with working links
- [x] `npm test` green

## Risk Assessment

| Risk | Signal | Response |
|---|---|---|
| IME composition broken by input handling | Diacritics mangled while typing | Never rewrite the input value during composition; guard with `compositionstart`/`end`. Explicitly tested on a real mobile keyboard, not just a desktop emulator |
| Countdown drift makes a legitimate move look late | Player sees 2s left, server says timeout | Clock offset from `Ping`/`Pong`; the ring visually settles ~300ms early so the client never claims more time than the server allows |
| Svelte 5 runes in `.svelte.js` store files misused | Reactivity silently stops updating | Stores use the documented `.svelte.js` rune-module pattern; store reducers are unit-tested outside components |
| Protobuf runtime bloats the bundle | Bundle budget exceeded | Measured in step 12; `protobuf-es` is tree-shakeable and only generated messages are imported |
| `localStorage` unavailable (private mode / blocked) | Crash on load | Every read/write wrapped in try/catch with a working in-memory default |
| Dev/prod WS URL divergence | Works in `npm run dev`, breaks when served by Go | Same-origin `wss://` resolved from `location` in prod; Vite proxy only in dev; both paths exercised before phase 7 |

## Phase 6 Outcome (2026-09-05)

The vs-bot UI is built, typed, tested, and served by the Go binary. 117 JavaScript tests pass
and `svelte-check` reports no errors across 368 files.

### What was verified, and how

| Claim | Evidence |
|---|---|
| The binary serves the SPA, deep links included | `curl` against a running server: `/`, `/play?difficulty=2` and `/online` all return `index.html`; hashed assets and the favicon return their own content types |
| Path traversal cannot escape the bundle | `/../server/go.mod` is normalised to a redirect; the percent-encoded form is refused with 400 |
| A bot game plays end to end at every difficulty | A headless client built from the same generated types played three moves against the bot at Easy, Medium and Hard against a fixture dictionary, with alternating turns and correct running scores |
| The nickname shown is the server's | The handshake returned `accepted_nickname` and the store adopts it |
| No wordlist reaches the browser | A bundle test greps every built file for dictionary words and asserts a size budget |

The bundle is 188,227 bytes across 28 files.

### Deliberate design points

**The store is a reducer over `ServerMessage` and nothing else.** Validity, turn order and the
result are read from the wire, never computed. That is what lets one screen serve both the bot
and, in phase 7, online play.

**The word field is uncontrolled.** A Telex or VNI input method composes a diacritic across
several keystrokes, and writing the value back on each keystroke cancels the composition. The
field is read on submit and cleared only there, where composition has already ended.

**The countdown settles 300ms early.** A ring showing time left while the server has already
timed the player out reads as the game cheating; reaching zero a fraction early reads as
rounding.

**`Hello` is sent before the open status is announced.** The server refuses everything before
the handshake, so a listener reacting to "connected" by sending a message would otherwise race
it. Ordering it explicitly turns a timing accident into a contract.

### Defects found by review

Review found one blocking defect and four user-visible breakages, all in code no test was
driving. The pattern behind the worst of them is worth naming: **the game lifecycle was
inferred from the game model instead of being owned.**

**Every rematch started two bot games, and the second killed the first.** The screen sent
`StartBotGame` from an effect whenever the board was idle. Clearing the board for a rematch is
exactly that condition, so the rematch button sent one request and the inference sent another.
The server allocates a room per request and does not deduplicate, so the player got two rooms
sharing one socket: the abandoned room kept its turn timer, overwrote the turn sequence that
the live game's submissions were tagged with, and about one turn later ended the game in a loss
nobody could explain. A request is now stored intent — `web/src/lib/stores/bot-session.svelte.js`
— and clearing the board cannot mean anything. Twelve tests cover it, because the absence of a
test over this wiring is why it shipped.

**Arriving at the board with a finished game on screen never started a new one.** Same root
cause: nothing reset the store on entry, so the header link or the browser Back button left a
stale phase behind and the idle condition never held. The screen now owns the socket and the
game for as long as it is mounted, and gives the current game up on the way out rather than
leaving a room whose timer keeps running somewhere the player cannot see.

**A word typed during a reconnect vanished silently.** The input stayed enabled while the socket
was down, the send returned false, and the field cleared anyway. The field is now gated on the
connection as well as the turn, and only clears a word that actually went out.

**A protocol bump would have put every open tab into a reconnect loop.** The backoff reset when
the socket opened, but the server accepts the connection and *then* refuses an unspeakable
`Hello`, so every retry looked like a success and restarted from the shortest delay. The
backoff now resets on `Welcome`, and `protocol_version_mismatch` stops reconnection outright.

**The opening turn counted down against the raw device clock.** The first clock probe was
scheduled a full interval out, but `GameStarted` arrives one round trip after `Hello`. A device
clock behind the server would have shown more time than the server allowed — the exact thing
the settle margin exists to prevent. The probe now goes out with the handshake.

Also fixed from the review: cache headers on the static bundle, where an `index.html` cached
across a deploy names hashed assets that no longer exist and the app loads into a blank page;
a record-keeping effect that read and wrote the same reactive state; an animation loop that ran
at 60fps between turns; and the difficulty picker, which claimed a radio role without radio
keyboard behaviour and now uses real radio inputs.

### Defects found while building

**The bundle guard was vacuous on its first negative test.** Planting an unused dictionary word
in the string table did not fail it, because Rollup dropped the unused property. Re-testing with
a rendered string made it fail correctly. It now also decodes `\uXXXX` escapes, so a minifier
configured to emit ASCII cannot hide the words from a substring search.

**`jsdom` broke the phase-4 fixture test.** Making it the global test environment rewrote
`import.meta.url` to an `http:` URL, so the cross-language fixture reader could not resolve its
path. The DOM is now opted into per file by the two suites that need it.

**`svelte-check` found a required prop that no caller passed.** `DifficultyPicker` declared
`onselect` as required while the home screen only binds `value`. The prop is optional now.

### Guards added after review

| Guard | What it would catch |
|---|---|
| `tests/bot-session.test.js` | A rematch sending two requests, or a result scored twice |
| `tests/error-codes.test.js` | A server error code with no Vietnamese message, read from the Go source that emits them; it also fails on copy for a code the server cannot send |
| `TestStaticCacheHeaders` | The shell becoming cacheable across a deploy |
| Build freshness in `tests/bundle.test.js` | The wordlist check reading a stale bundle |
| Handshake ordering in `tests/ws-client.test.js` | `Hello` losing its place ahead of the open status |

The error-code guard and the wordlist guard were both negative-tested by breaking what they
protect and watching them fail for the right reason. The bundle budget was 1.5 MB against a
188 KB bundle, which caught nothing; it is 400 KB now.

### Deviations from the plan

- Scaffolded by writing the config files directly rather than `npx sv create`, which is
  interactive. ESLint and Prettier were not added; `svelte-check` is wired into `make test-web`
  instead.
- Tests live in `web/tests/`, following the layout phase 4 established, not beside their
  sources.
- `npm test` now runs `vite build` first. The bundle check reads built output, and a stale
  build would let it pass over code that no longer exists. This also means the proto workflow's
  JavaScript step builds the frontend, which needs no dictionary and stays fast.

### Not verified

Five criteria need a real browser and are left unchecked: one-handed mobile use, Telex
diacritic entry, the absence of a theme flash, reconnect after the server is killed mid-game,
and the rematch button. The logic behind the last three is unit-tested — the pre-paint theme
script, the jittered backoff schedule, and the game-request latch — but none has been watched
happen. The rematch path is worth naming separately: it is where review found the blocking
defect, and the fix is verified by unit tests over the latch rather than by playing a game.

The countdown criterion is no longer known to be violated on the opening turn, since the clock
probe now goes out with the handshake, but the ~200ms figure has not been measured.

A resumed session shows the opening word and the last move rather than the whole chain. That
is the server's replay contract, which rebuilds the position from the engine rather than
replaying a recorded stream; the client renders what it is sent.
