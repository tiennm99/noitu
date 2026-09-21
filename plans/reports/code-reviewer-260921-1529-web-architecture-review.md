# Web frontend — whole-project architecture review

Branch `dev` @ 5178a97 · 2026-09-21 · scope `/workspace/tiennm99/noitu/web` (src 4,392 LOC, tests 2,537, e2e 1,472)

## Checks run (read-only)

| Command | Result |
|---|---|
| `npm run lint` | 0 errors, **33 warnings** (32 × `jsdoc/reject-any-type`, 1 × `check-param-names` in `e2e/helpers.js:123`) |
| `npm run check` | 380 files, **0 errors, 0 warnings** — see A4: this number is mostly meaningless today |
| `npm test` | build OK, **221 passed / 12 files**, 3.8s |
| Playwright | not run (no browser on this host, per workspace rules) |

## Verdict

Structurally sound and unusually well-reasoned — the "store is a projection" invariant holds everywhere I
checked, the uncontrolled-input invariant is respected, and the comments explain *why* rather than *what*.
Three things are genuinely wrong and one of them is a stuck-UI dead end reachable after any deploy. The
bigger problem is not a defect: **`svelte-check`'s clean run is an illusion** — `initialState()` returns
`any` (`stores/game.svelte.js:54`), so every `game.state.*` read in every component is unchecked. Fix that
before any refactor, or the refactor lands blind.

Do not slice the store into per-domain stores. Do extract the page's request state machine.

## Top 10 ranked actions

| # | Action | Kind | Size | Risk | Why now |
|---|---|---|---|---|---|
| 1 | Time-box the resume latch; a stale token gets **silence** from the server, not an error → `?code=` + stale token = permanently disabled join form | fix | S | L | Confirmed against `server/internal/wsapi/session.go:643` + `hub.go:127`. Reachable after every deploy |
| 2 | `leave()` omits `forgetSession()` → next reload resumes into the room just left | fix | S | L | Confirmed; one-line asymmetry vs. the page-teardown path |
| 3 | Type `GameState`; delete `@returns {any}` on `initialState()` | fix | M | M | Unblocks every other item. Expect real errors to surface |
| 4 | Extract `stores/room-session.svelte.js` (join/resume/quick-match machine) from `online/+page.svelte` | refactor | M | M | Removes 5 of 7 `$effect`s; makes #1 unit-testable; `bot-session` is the precedent |
| 5 | Type the wire from `game_pb.d.ts`; `switch (payload.case)` for oneof narrowing | refactor | M | L | Clears 18 src lint warnings; makes the oneof exhaustive at build time |
| 6 | Fire-and-forget sends (`cancelQueue`, `leave`, lobby `report`) silently drop requests | fix | S | L | Best explanation for the `toBeEnabled` flake; user-visible dead buttons |
| 7 | Split `game.svelte.js` → `game-shape.js` / `game-apply.js` / store; wrap `apply` in try/catch | refactor | M | L | A throw mid-`apply` leaves a half-applied snapshot on screen |
| 8 | Component tests under jsdom via `mount()`; then cut Playwright 47 → ~12 | refactor | M | L | jsdom is already a devDependency; today 0 component tests exist |
| 9 | `ArmedButton.svelte` (3 duplicated arm/disarm blocks) + announce the armed state | refactor | S | L | DRY + the only a11y gap that loses information |
| 10 | `{#each entry.meanings as sense (sense.gloss)}` — duplicate gloss = Svelte duplicate-key throw | fix | S | L | Dictionary data is not guaranteed gloss-unique |

**Leave alone:** per-domain store slices (§1), `vi.js` namespacing (§5), manual chunking / font strategy (§4),
the uncontrolled-field design (§3), the `Set` + eslint-disable in `reset()` (dissolves under #3).

---

## 1. Structure

### 1.1 `routes/online/+page.svelte` (757) — extract the request machine

Seven `$effect`s, five of which are one state machine wearing a costume: `pending` (48), `resuming` (121),
`needName` (125), `stalled` (126), `queuedForS` (56), and the latch-clearing effect at 137-143, the flush at
216-219, the queue timer at 225-233, the stall timer at 239-247, the resume-failure handler at 253-268.

**Proposal** (mirrors `stores/bot-session.svelte.js` exactly):

- `lib/stores/room-session.svelte.js` (~130 LOC, no DOM, no runes beyond `$state`) — owns `pending`,
  `resuming`, `needName`, `stalled`, `waitedS`; exposes `request(req)`, `flush(isOpen)`, `noteRoom()`,
  `noteError(code)`, `noteResumeTimeout()`, `leave()`. Unit-testable in Vitest with a fake clock, exactly as
  `ws/client.js` already is (630 lines of tests prove the pattern works).
- `lib/components/JoinPanel.svelte` (~160) — the `{:else}` branch at 465-547: nickname, quick match, create,
  join form, `needName` / `error` / `stalled` notices.
- `lib/components/RoomLayout.svelte` (~90) — the two-column shell (408-464) plus `wide` (85), `chatFolded`
  (103), `chatUnread` (104), `talkPane` (106) and the media-query effect (87-94).
- Page drops to ~130: store wiring + the 11 one-line message senders (349-401).

**Invariant impact: none.** `room-session` holds *client intent* (what the player asked for), never server
state. `game` stays the sole projection. This is the boundary `bot-session.svelte.js:1-12` already argues for
in prose.

**Size M, risk M** — the risk is entirely in the teardown effect (185-212), which is load-bearing for seat
release. Port it verbatim; cover it with the existing `pvp-game.spec.js:235` spec before and after.

Secondary: the teardown at 185 is coupled to `inviteCode` (`$derived` on `page.url`, 128). Any future
in-app URL mutation on `/online` — a `replaceState` to drop the used `?code=`, say — would fire a full
leave-room-and-disconnect. Move teardown to `onDestroy` so it is not a reactive dependency of a query
parameter.

### 1.2 `stores/game.svelte.js` (646) — split the file, keep one state object

**Do not make this four stores with a dispatcher.** Three reasons:

1. The store's own comment at 300-302 states the failure mode a slice design invites: *"Merging fields
   selectively is how a client ends up believing a mixture of two states the server was never in."* Four
   reducers each handling part of a `RoomState` is precisely that, with the atomicity now spread across
   module boundaries.
2. Components read across the proposed domains. `ScoreBoard.svelte:15-17` reads `gamePlayers` +
   `standings` + `roomPlayers` + `nickname`; `nameOf()` (593-601) falls back across game → room; `myScore`
   (566-569) picks its table by `phase`.
3. There is no performance motive. 61 store tests run in 36ms.

**Proposal — mechanical file split, one `$state`:**

- `stores/game-shape.js` (~210) — the `ChainEntry`/`Sense`/`PointPart`/`PlayerSlot`/`PlayerScore` typedefs,
  the new `GameState` typedef (#3), `initialState()`, and the three wire decoders `toSenses`/`toParts`/
  `toScore` (200-230). Pure, zero reactivity, the natural home for the generated-type imports.
- `stores/game-apply.js` (~230) — `applyTo(state, msg)`: the switch at 286-518 as a pure function over a
  plain object. Testable without the runes compiler.
- `stores/game.svelte.js` (~200) — `$state`, `reset`, `leave`, the 12 derived accessors, the singleton.

**Size M, risk L** (mechanical). Sequence it *after* #3 and #5 so the decoders land typed.

While splitting, wrap the call site: `apply()` has no error boundary and is invoked from
`ws/client.js:245-246` inside `ws.onmessage`. A throw anywhere in the switch aborts mid-mutation — e.g.
`roomState` sets `queued`/`roomCode`/`canStart` (296-306) *before* mapping `players` (308), so a throw there
leaves a room on screen with no seats. Protobuf-es v2 always materialises repeated fields, so this is
plausible rather than confirmed, but the cost of `try { applyTo(...) } catch { /* report */ }` is one line.

### 1.3 `components/GameBoard.svelte` (495)

Two near-identical arm/disarm blocks: `arming`/`armTimer`/`armOrResign` (48-51, 99-109, 124) and
`claimArming`/`claimArmTimer`/`armOrClaim` (52-54, 112-122, 125), plus their disarm-on-turn-loss effects
(74-78, 84-88). `Lobby.svelte:55-81` has a third copy for kick.

- `components/ArmedButton.svelte` (~45) — props `{ label, confirmLabel, disabled, onconfirm }`; owns the
  timer, the disarm-on-disable effect, and (see §5) the announcement the armed state currently lacks. Three
  call sites, ~70 LOC deleted. **S / L.**
- `components/BoardHeader.svelte` (~55) — the `top` row at 129-154 (badge, mode label, rules link, chat
  pill). Board falls to ~330.

### 1.4 `components/Lobby.svelte` (467)

Extract `components/SeatList.svelte` (~140): the `<ul class="seats">` at 101-162 plus the kick arming, which
becomes an `ArmedButton`. Lobby → ~300. **S / L.** Lowest priority of the four.

### 1.5 Total

~700 LOC moved, ~150 new, two new pure modules Vitest can reach. No abstraction without a domain anchor:
every extracted unit is an existing repeated pattern (armed button, seat list) or an existing named concept
(the room session, the wire decoders).

---

## 2. Typing without TypeScript

18 `any` sites in `src/` (+ 12 in `tests/`+`e2e/`, 9 of which dissolve for free). `game_pb.d.ts` already
carries everything needed: `ServerMessage.payload` is a proper discriminated union
(`game_pb.d.ts:1185+`), and `moduleResolution: "bundler"` resolves `import('…/game_pb.js').ServerMessage`
through the sibling `.d.ts`.

### Site A — `ws/client.js:100, 269, 297` (the oneof)

Before:

```js
 * @param {(msg: any) => void} options.onMessage
…
/** @param {any} msg */
function intercept(msg) {
	const payload = msg.payload;
	if (payload.case === 'welcome') {
		attempt = 0;
		if (payload.value.resumeToken) storeToken(payload.value.resumeToken);
```

After:

```js
/** @typedef {import('$lib/proto/noitu/v1/game_pb.js').ServerMessage} ServerMessage */
/** @typedef {import('$lib/proto/noitu/v1/game_pb.js').ClientMessage} ClientMessage */
…
 * @param {(msg: ServerMessage) => void} options.onMessage
…
/** @param {ServerMessage} msg */
function intercept(msg) {
	const payload = msg.payload;          // NOT destructured — see below
	if (payload.case === 'welcome') {
		attempt = 0;
		if (payload.value.resumeToken) storeToken(payload.value.resumeToken); // value: Welcome
```

`payload.case === 'pong'` then narrows `payload.value` to `Pong`, whose `clientTimeMs` is `bigint` — which
makes the `Number(...)` at 287-288 a *checked* conversion rather than a hopeful one. `send` (297) takes
`ClientMessage`; `messages.js` builders already return it, so no change there. The three timer `any`s
(107, 126, 128) become `ReturnType<typeof setTimeout>`, which matches the injected `schedule: typeof
setTimeout` exactly.

### Site B — `stores/game.svelte.js:54` (the one that matters)

`@returns {any}` on `initialState()` makes `const state = $state(initialState())` `any`, therefore
`game.state` is `any`, therefore **every** `game.state.foo` in all 16 components is unchecked. A typo
(`chainLenght`) compiles, ships, and renders `undefined`. This is why `svelte-check` reports 0 errors.

Before:

```js
/** @returns {any} */
function initialState() { return { phase: 'idle', chain: [], … }; }
…
// eslint-disable-next-line svelte/prefer-svelte-reactivity
const kept = new Set(['nickname', 'roomCode', …]);
function reset() {
	const fresh = initialState();
	for (const key of Object.keys(fresh)) {
		if (kept.has(key)) continue;
		state[key] = fresh[key];          // untyped index write
	}
}
```

After:

```js
/**
 * @typedef {object} GameState
 * @property {'idle'|'lobby'|'playing'|'over'} phase
 * @property {ChainEntry[]} chain
 * @property {string[]} expanded
 * …one @property per field, ~30 lines, prose comments unchanged…
 * @property {string|null} error
 */

/** @returns {GameState} */
function initialState() { … }

/** Fields a game ending does not change: they describe the room, not the game. */
const KEPT = /** @type {const} */ ([
	'nickname', 'roomCode', 'roomPlayers', 'canStart',
	'maxPlayers', 'minPlayers', 'graceMs', 'chat', 'chatCount'
]);

/**
 * @template {keyof GameState} K
 * @param {GameState} into
 * @param {GameState} from
 * @param {readonly K[]} keys
 */
function carry(into, from, keys) {
	for (const k of keys) into[k] = from[k];   // both sides are GameState[K]
}

function reset() {
	const fresh = initialState();
	carry(fresh, state, KEPT);
	Object.assign(state, fresh);
}

function leave() { Object.assign(state, initialState()); }
```

The generic `carry` typechecks without a suppression, and the `Set` plus its `eslint-disable` disappear.

### The one non-obvious step

`apply()` currently destructures: `const { case: kind, value } = msg.payload;` (287). TypeScript **loses
discriminated-union narrowing across a destructure**. The switch must become `switch (payload.case)` with
`payload.value` read inside each arm. That is the only mechanical change to the 230-line switch; everything
else is annotations.

### Remaining sites

- `game.svelte.js:200/204/209/213/218/308/471` → the wire types, renamed on import to avoid colliding with
  the store's own same-named typedefs: `@typedef {import('…').PlayerScore} WirePlayerScore`. The `?? []`
  guards at 204/213 are dead under protobuf-es v2 (repeated fields always materialise).
- `ChainHistory.svelte:115` → `@param {import('$lib/stores/game.svelte.js').PointPart} p` (already exported).
- `bot-session.svelte.js:68/74` → widen from `object|null` to `{ myScore: number }|null`, drop the cast.
- `connection.svelte.js:36` → `ClientMessage`.
- `tests/game-store.test.js:624/635/661` dissolve once `GameState` exists; `tests/ws-client.test.js` (6)
  needs the same `ServerMessage` typedef; `e2e/helpers.js:123` → `@param {...Page} guests`.

Net: 33 warnings → 0, and `svelte-check` starts earning its exit code. Budget for a handful of *real* errors
surfacing in components on the first run — that is the point.

---

## 3. Runtime correctness sweep

### Confirmed

**C1 — Resume latch has no timeout; the server answers an unknown token with silence.**
`online/+page.svelte:160-173` sets `resuming = true` and, when the URL carries a code, `pending = {kind:
'join', code}`. `flush()` refuses to send anything while `resuming` (288). `resuming` is cleared only by
arriving in a room (137-143) or by an error (253-268). Server side: `session.go:643`
`if prior, ok := s.hub.resumable(token); ok && … { s.resumeFrom(prior) }` — and `hub.go:127-135` returns
`ok == false` for an unknown token, with **no message sent**. So a stale token (server restarted, or the
session was GC'd after grace) plus an invite link leaves the screen with an OPEN socket, `resuming` stuck
true, `pending` stuck set → all three buttons disabled reading "Đang kết nối…" (509/513/538), and the
`stalled` banner never fires because it requires `connection.status !== OPEN` (240). Permanent dead end;
only a manual reload escapes, which reproduces it.
*Fix:* a `RESUME_TIMEOUT_MS` timer armed alongside `resuming`, running the same path the error branch does.
Belongs in `room-session` (#4). Secondary: ask the server to answer a non-resumable token explicitly — the
`resumeFrom` comment at 651-656 already commits to "every failing branch has to say so", and this branch
does not.

**C2 — `leave()` does not forget the session.** 368-372 sends `LeaveRoom`, wipes the store, clears `pending`
— but omits `forgetSession()`, which the page-teardown path at 203 does call. The token stays in
sessionStorage, so the next load of `/online` in that tab takes the `hasStoredSession()` branch (160) and
tries to resume into the room the player deliberately left. Best case the server refuses and C1's stuck
state is one step away; worst case the seat is still live and they are put back.

**C3 — Fire-and-forget sends.** `send()` returns a boolean everywhere else in this file and is checked at
302 and in `Lobby.report()` (62-66). It is *ignored* at `cancelQueue` (328) and `leave` (369). Cancelling a
quick match while the socket is reconnecting therefore clears `pending` locally while `game.state.queued`
stays true — the waiting panel (490-503) stays up with its elapsed clock running and its Cancel button
doing nothing on every subsequent press. Same class as the flake below.

**C4 — Duplicate-key throw on meanings.** `ChainHistory.svelte:124`
`{#each entry.meanings as sense (sense.gloss)}`. Two senses with different `pos` and identical `gloss` are
not excluded by the wire contract; Svelte throws `each_key_duplicate`. Key by index — the list is neither
reordered nor filtered. Same fragility, lower probability, at `ChainHistory.svelte:68`
`{#each entries as entry, index (entry.word)}` (safe only because the server refuses repeats).

**C5 — The lobby chat never reopens after the first game.** `chatFolded` starts `false` (103) and is set
`true` by the effect at 108-110 when `phase === 'playing'`. Nothing ever sets it back. After a game ends the
phase goes `over` → (next game) `playing`; it never returns to `lobby` while the player stays in the room
(`game.svelte.js:322` only promotes `idle` → `lobby`). So on narrow screens the between-games lobby has a
folded chat forever after the first game — the regression that 5178a97's fix does not cover. The pill in the
board header keeps it reachable, so this is UX, not a trap. Fix: fold on the `lobby → playing` transition
only, or unfold on `over`.

**C6 — Seeding writes into a field the connection has disabled.** `WordInput.svelte:103-128` depends on
`myTurn`/`phase`/`turnSeq`/`currentSyllable`/`rejection` but **not** on `connection.status`, while `enabled`
(22-24) does. During a reconnect on the player's own turn the effect still calls `field.focus()` and writes
`field.value = "${syllable} "`, after `lockedValue` was captured pre-seed at 70-73. The first composition
event then calls `undoInput` (92-95) and yanks the seed back out. Cosmetic, but it makes the seed
non-deterministic exactly when the player is anxious. Add `enabled` to the guard at 104.

**C7 — `console.warn` ships to production.** `game.svelte.js:514`. Intentional per the comment, and I agree
with the intent — but it prints internal oneof case names to the console of every player on a version skew.
Route it through a one-shot dev-only guard or leave it; low.

### Plausible

**P1 — the `toBeEnabled` flake in `e2e/helpers.js:130.** `readyAndStart` clicks the guest's ready button and
immediately asserts the owner's Start is enabled (10s `expect` timeout). `Lobby.svelte` line ~213 gates it on
`!s.canStart || offline`, both server-owned, so the assertion is correct — but nothing between the click and
the assertion proves the `SetReady` *left the client*. `onready` → `report(onready(...))` (224) → `send()`,
which returns `false` if the socket is not `readyState === 1` (`client.js:299`) and **nothing retries**:
`unsent` (60) draws a banner and the request is gone. Meanwhile the button itself is only disabled on
`offline`, which is `connection.status`, and status reaches `OPEN` inside `ws.onopen` — a window exists where
Playwright sees an enabled button over a socket in an ambiguous state.
*Two fixes, both worth it:* (a) give lobby actions the same held-request latch `pending` already gives
join/create (lands free with #4); (b) in the helper, assert the guest's own state before starting —
`await expect(guest.getByTestId('my-ready')).toHaveText('Sẵn sàng')` — which converts a mystery timeout into
a precise failure on the right page.

**P2 — route-swap socket race.** `/online` teardown calls `disconnect()` (209) which nulls the module
singleton; `/play` mount calls `connect()` (69). If SvelteKit ever creates the new page component before
destroying the old one, `connect()` runs first and `disconnect()` then kills the socket the new screen owns,
leaving `/play` with `pending` set and no transport and no retry (`session.flush` is only driven by a status
*change*, `play/+page.svelte:49-52`). I could not prove the ordering without a browser. A `connect()` that is
idempotent-by-generation, or an owner token on the singleton, removes the question.

**P3 — resume-failure handler swallows unrelated errors.** `online/+page.svelte:253-268` treats *any*
`game.state.error` arriving while `resuming` as a failed resume: it clears the error silently and spends the
held code. A `server_full` or `too_fast` landing in that window is erased with no trace. Narrow it to the
codes the resume path can actually produce (`game_already_over`).

### Verified correct (do not "fix")

- **Countdown vs. server clock** — `countdown.js`, the midpoint offset estimate (`client.js:293`), and the
  rAF loop gated on `running` (`CountdownRing.svelte:30-37`), `SETTLE_MS` erring early included.
- **Chat unread accounting** — the two effects at `ChatPanel.svelte:64-66` and 83-86 are correct under fold,
  resync (`count < seenAt`), the `CHAT_WINDOW` cap (counted against `chatCount`, not `chat.length`), and the
  single-mount-across-phases arrangement the page comment at 417-422 depends on.
- **Storage failures** — `settings.svelte.js:389-421` and `client.js:68-90` guard the property access itself,
  not just the call; `readBestScores` (428-445) validates shape and value; `app.html`'s pre-paint theme
  script has its own `try`.
- **Uncontrolled-input invariant** — `beforeinput` guard + `input` undo + composition flags
  (`WordInput.svelte:83-95, 205-208`) and the three direct-write exceptions (seed 124, suggestion 164, clear
  146) are each correct at their call site. Only C6 breaks the pattern.
- **`untrack` usage** — load-bearing at `+page.svelte` 139/150/218/255 and `play/+page.svelte` 37/51/58; each
  prevents a self-retriggering effect, none hides a dependency that should be tracked.
- **`PlayerStatus.svelte:36-48`** grace bookkeeping handles the same-length-different-ids case correctly.

---

## 4. Performance and bundle

Client build (`vite build`): largest chunk 83.13 kB / 25.98 kB gz (Svelte runtime + `@bufbuild/protobuf`),
then 33.37 / 26.18 / 21.91 kB; `/online` route node 20.61 kB; CSS 13.61 kB (GameOverPanel) + 9.70 kB
(online route) + 3.98 kB (layout). Whole build comfortably under the 400 kB budget.

**`tests/bundle.test.js` guards, well:** dictionary words absent *after unicode-escape decoding*; no
`.db/.sqlite/.csv/.tsv`; total < 400 kB; build newer than src, so the assertions cannot go vacuous — that
last one is the good idea here, most bundle tests are silently stale. **Gaps:** no per-chunk ceiling (a
single 300 kB chunk passes), no gzip assertion, no sourcemap check. A largest-chunk budget is ~6 lines and
is the one that would catch an accidental dependency.

**Chunking / fonts / CSS: nothing to do.** No web fonts (`app.css:45` — correct where system fonts already
carry the diacritics). No `manualChunks`, and none is warranted: every screen needs the socket and the proto
on first interaction. `preload-data="hover"` over five routes is cheap. `ssr = false` + `prerender = false`
means the SSR output under `.svelte-kit/output/server/` is built but never copied into `build/`.

**Svelte 5 idioms: current throughout** — runes, snippets instead of slots, `page` from `$app/state` not the
deprecated `$app/stores`. No `export let`, no `$:`, no `createEventDispatcher`. Two nits: the bare
`messages.length;` dependency read at `ChatPanel.svelte:92` is obscure (assign it to a `const`), and
`$effect(() => () => clearTimeout(t))` (`GameBoard.svelte:124-125`, `Lobby.svelte:81`) is a dependency-free
effect used purely for teardown — `onDestroy` states that plainly and cannot later acquire a dependency.

---

## 5. Accessibility and i18n

**Good already, unusually so:** skip link; `sr-only` `<h1>` on the room screen (413); zero-specificity
`:focus-visible` ring via `:where()` (`app.css:132-138`); 44px `.icon-button`; `prefers-reduced-motion`
honoured in CSS *and* script (`motion.js` — necessary, since an explicit `behavior` beats the CSS rule);
turn indicator as `role="status" aria-live="polite" aria-atomic` (`GameBoard.svelte:203-210`); `role="timer"`
plus a separate two-mark spoken region rather than 60 announcements/second (`CountdownRing.svelte:48-51,
77-79`); deliberate *non*-duplication of the connection announcement (165-171); focus moved to the result
panel rather than to its button (`GameOverPanel.svelte:25-34`).

**Gaps, ranked:**

1. **Armed buttons announce nothing.** Resign/claim/kick change their own label on the first press
   (`GameBoard.svelte:239`, `Lobby.svelte:146`). A screen reader announces a control's name on focus, not on
   in-place mutation — so a non-sighted player presses once, hears nothing, and either presses again blind or
   walks away. Fold `aria-pressed` (or a discreet live region) into `ArmedButton` (#9).
2. **Chat log live region.** `ChatPanel.svelte` (the `<ol aria-live="polite" aria-relevant="additions">`)
   announces the reader's *own* messages back to them, and `aria-relevant` is inconsistently implemented.
   Prefer a dedicated `sr-only` `role="log"` carrying only the newest line where `fromMe` is false — the
   pattern `CountdownRing` already uses.
3. **Rejection alert doubles as a description.** `WordInput.svelte:200-202` points `aria-describedby` at the
   same element that carries `role="alert"` (220), so the text is announced twice, and the alert contains two
   focusable buttons. Split: an `sr-only` alert for the announcement, a plain `<p id="word-rejection">` for
   the description and the buttons.
4. **Focus after suggestion-fill is right, state is not.** `useSuggestion()` (161-167) focuses the field and
   places the caret correctly, but leaves `game.state.rejection` set — so `aria-invalid` stays true and the
   banner still offers the suggestion the player just took.
5. **`ScoreBoard` turn row has no `aria-current`.** `class:active` (24) is the turn indicator for sighted
   users only; the row is otherwise indistinguishable.
6. **Colour contrast is fine.** Spot-checked the risky pairs: `--warn #8a5a08` on `--surface-alt #e9f0ea`
   ≈ 5.1:1; `--text-muted #5b6a61` on `--bg #f4f7f4` ≈ 5.2:1; `--player-1 #1d5c8f` on white ≈ 7.1:1;
   `--player-3 #8a4c12` on white ≈ 6.7:1; dark-theme `--warn #e9b949` on `#1f2a23` ≈ 8.1:1. All ≥ 4.5:1 at
   the sizes used. `--border-strong` exists precisely because `--border` does not clear 3:1, and says so.

**`vi.js` (359 lines) — leave it flat.** Namespacing buys nothing here: one locale, no runtime loader, no
key collisions, and `t.foo` is already checked by `svelte-check` (it is a typed object literal, so `t.subimt`
is a compile error today — this is one of the few places typing currently works). The existing
comment-grouped sections are the right amount of structure. The real gap is that `tests/i18n.test.js` covers
the *enum-derived* maps exhaustively (reject reasons, end reasons, point kinds, difficulties, error codes)
but nothing asserts that `fill()` placeholders in the flat `t` keys match their call sites — a `{name}` typo
in `playerTurn` renders literally. A ~15-line test extracting `{…}` tokens from each `t` value and checking
them against `fill()` call sites would close it.

---

## 6. Tests

**Vitest, by module.** Covered: `countdown` (10), `room-code` (14), `history-export` (8), `i18n` (14),
`settings` store (16), `ws/client` (32 — genuinely good: injected clock, socket factory, scheduler),
`game` store (61, fed real decoded `ServerMessage`s — the right call), `bot-session` (12), wire round-trip
(43), error codes (3), dictionary source (4), bundle (4).

**Uncovered: every `.svelte` file.** There are zero component tests and no component-test harness. Nothing
in `tests/` imports a component. So all of this is browser-only today:

| Behaviour | Where it lives |
|---|---|
| Chat unread across fold / phase / resync | `ChatPanel.svelte:58-96` — *this is what broke CI today* |
| Word field seed, guard, undo, suggestion-fill, submit-clear | `WordInput.svelte:70-167` |
| Arm/disarm and the disabled matrix | `GameBoard.svelte:74-125`, `Lobby.svelte:55-81` |
| Ring states (mine / stalled / urgent / idle) | `CountdownRing.svelte:15-51` |
| Grace countdown bookkeeping | `PlayerStatus.svelte:36-54` |

**Recommendation: mount components under jsdom.** `jsdom` is *already* a devDependency and two suites
already opt in per-file; Svelte 5 components compiled by the vite plugin mount directly with no extra
library:

```js
// @vitest-environment jsdom
import { mount, unmount, flushSync } from 'svelte';
import ChatPanel from '../src/lib/components/ChatPanel.svelte';
```

No new dependency, no browser, runs on this ARM64 host. Start with `ChatPanel` (unread) and `WordInput`
(seed/guard) — between them they cover the two most-regressed behaviours in the tree.

**Playwright: 47 specs across 3 files, `workers: 1`, `fullyParallel: false`, 60s timeout.**

*Delete — the Go suite already proves the rule, and the Vietnamese string is proven by `i18n.test.js`:*
`bot-game.spec.js:114` (word not in dictionary), `:127` (too short), `:137` (does not link),
`pvp-game.spec.js:588` (unknown room code), `:608` (latecomer refused), `:624` (fifth player turned away),
`:394` (room outlives its owner).

*Replace with a unit or component test:* `bot-game.spec.js:242` (personal best — `settings-store.test.js`
owns the logic), `:264` (attribution footer — static markup), `:40` (syllable pre-seeded — a `WordInput`
component test), `pvp-game.spec.js:460` and `:528` (chat unread / send gating — a `ChatPanel` component
test). `bot-game.spec.js:272` (deep link served by the binary, not a 404) is a *server* concern and belongs
in the Go suite.

*Keep — genuinely browser-only:* all of `reconnect.spec.js` (socket cut, resume, forfeit, dead-connection
typing — multi-context and transport-level); `pvp-game.spec.js:100` (turns alternate), `:140`/`:164` (invite
link, nameless guest), `:193` (uncontrolled field refuses text out of turn — IME behaviour no jsdom test can
prove), `:497` (a turn does not steal the chat field), `:515` (rendered as text, never markup — the XSS
assertion), `:674` (spectator after knockout); `bot-game.spec.js:16` (one end-to-end game), `:195` (download
plumbing), `:217` (rematch starts exactly one game).

47 → ~14, multi-player and reconnect coverage untouched. Sequence *after* the component tests land.

---

## Unresolved questions

1. **P2 (route-swap socket race):** does SvelteKit destroy the outgoing page component before creating the
   incoming one? Unverifiable here without a browser. If it does not, `/online → /play` can leave `/play`
   transport-less.
2. **C1's server half:** should `handleHello` answer a non-resumable token explicitly? That is a protocol
   change and a server decision — the client-side timeout (#1) is sufficient and should land regardless, but
   the explicit answer is the better contract and `resumeFrom`'s own comment already argues for it.
3. Does the dictionary builder guarantee gloss-unique senses per word (C4)? If it does, the duplicate-key
   risk is theoretical — but the key should still not depend on a guarantee nothing in this repo states.
4. Is the `chatFolded` behaviour in C5 intended (fold once, stay folded) or an unnoticed consequence of
   5178a97? This is a product call, not a defect call.
5. Playwright wall-clock today is unmeasured here (no browser). The 47 → 14 recommendation assumes the suite
   is a meaningful part of CI time; if it runs in under three minutes, prioritise #8's component-test half
   and defer the deletions.
