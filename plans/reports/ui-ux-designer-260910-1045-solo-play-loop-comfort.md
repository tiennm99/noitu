# Solo play loop — comfort review

Read-only review. Paths relative to `web/src/`.

## Verdict

The loop is well thought through on desktop: focus returns to the field on every turn, the field is pre-seeded with the required syllable, IME composition is respected, and the chain scrolls inside itself so the input never moves. Two defects hurt real play: the countdown turns danger-red during the **bot's** turn (false panic, every turn), and `disabled` on the input blurs the field each time the turn passes, which closes the on-screen keyboard and cannot be reopened programmatically — a phone player pays one extra tap per turn. Everything else is P2 legibility/contrast/session-end polish.

## What works (do not change)

- **No layout shift under the input.** `.board` is a flex column whose only flexible child is `.chain` (ChainHistory.svelte:91-96), so a growing chain, an inserted rejection line and the error banner all take space from the chain. The ring, syllable and input row never move. The comment at GameBoard.svelte:75-77 (resign above the chain) is correct reasoning.
- **Uncontrolled input + composition guards.** WordInput.svelte:11, 79, 93-115: `autocapitalize/autocorrect/spellcheck` off, no value write-back, submit refused mid-composition. This is exactly right for Telex/VNI and is the hardest thing here to get right.
- **Syllable seeding with caret placement** (WordInput.svelte:65-70) and re-seeding after a rejection (:24-26, :60) removes the one keystroke sequence every turn shares.
- **Focus is not stolen from the chat** (`typingElsewhere`, :36-43) and chain rows never grab focus (ChainHistory.svelte:36-37).
- **`SETTLE_MS = 300`** (countdown.js:10) and `Math.ceil` on the display (:48) — the clock never shows time the server has already taken. Correct call.
- **No decorative motion in the ring.** A continuous rAF arc, no pulse, no flash. Better for repeated play than the usual pulsing clock.
- **The input unmounts at game over** (GameBoard.svelte:51-53), so a leftover draft cannot leak into a rematch. Verified — not a bug.
- **Touch targets in the turn loop are fine:** input 14px padding + 16px text ≈ 52px, submit ≈ 52px, `.resign` `min-height: 44px`, error dismiss 44×44 with negative margins (GameBoard.svelte:152-165).
- **16px input font** (WordInput.svelte:138-140) — no iOS focus zoom.

---

## P1

### P1-1 — The clock goes red while the bot is thinking

**CountdownRing.svelte:13, 29-32**

Player experience: mid-game the ring turns danger-red and the number counts to 1 while the player can do nothing. It reads as "you are about to time out" when it is actually the bot's clock. Repeated 20+ times a session, this is the single most stress-inducing thing on the board.

Cause: `running` only checks `phase === 'playing' && deadlineMs > 0`, and `game.state.deadlineMs` is rewritten by *every* `turnUpdate` including the ones handing the turn to the bot (stores/game.svelte.js:336). `urgent` (:32) never consults `game.state.myTurn`.

Fix:

```js
const mine = $derived(game.state.myTurn);
const urgent = $derived(running && mine && left <= URGENT_SECONDS);
```

and desaturate the ring when it is not the player's clock, so the number is legible but not addressed to them:

```css
.ring:not(.mine):not(.idle) { color: var(--text-muted); }
.ring:not(.mine) .value { color: var(--text-muted); }
```

(add `class:mine` on the wrapper, :35).

### P1-2 — On a phone the keyboard closes on every turn and does not come back

**WordInput.svelte:110, 118 (`disabled={!enabled}`), :89, :51-59**

Player experience: type a word, send, the keyboard drops. The bot answers. "Đến lượt bạn" appears, the field lights up — but there is no keyboard. Tap the field, keyboard, type, send, keyboard drops. One dead tap per turn, forever.

Cause, two parts:
1. `enabled` requires `myTurn` (:16-18), so the instant the turn passes the input receives `disabled`. Setting `disabled` on the focused element blurs it, and a blurred input closes the on-screen keyboard.
2. On turn return, `field?.focus()` (:59) runs inside an `$effect` driven by a WebSocket message — not a user gesture. iOS Safari will not open the keyboard for a programmatic `focus()`, and Android Chrome is inconsistent. So the focus ring returns and the keyboard does not.

Tapping "Gửi" instead of the keyboard's send key loses focus even earlier: the button click blurs the input before `handleSubmit` runs.

Fix (both parts needed):

```js
// inside handleSubmit, after field.value = '' — still inside the user gesture,
// so iOS honours it and the keyboard stays up.
field.value = '';
field.focus();
```

and stop disabling the field itself. Keep the *submit* disabled (the guard at :79 already refuses out-of-turn sends) and leave the field editable so focus — and the keyboard — survive the bot's turn:

```svelte
<input ... aria-disabled={!enabled} />
<button type="submit" disabled={!enabled} ...>{t.submit}</button>
```

Speculative typing during the bot's turn then becomes possible, so the seed must correct a stale draft instead of yielding to it. Replace :65:

```js
// Yield to a draft only if it already starts with the syllable being asked
// for. A draft aimed at a position that has moved on is worse than no draft.
const draft = field.value.trim();
if (!syllable) return;
if (draft && draft.toLowerCase().startsWith(syllable.toLowerCase())) return;
field.value = `${syllable} `;
```

Keep the visual "not your turn" state on the field via `aria-disabled`, e.g. `input[aria-disabled='true'] { background: var(--surface-alt); color: var(--text-muted); }`, so nothing looks different.

Caveat worth a device check: iOS also dismisses the keyboard when an input becomes `readonly`, which is why the recommendation is fully editable rather than `readonly`.

---

## P2

### P2-1 — Accent text on `accent-soft` is 4.29:1 in the light theme

**app.css:20-21 pairing, used at ChainHistory.svelte:158-161 + :214-218, GameOverPanel.svelte:234-241, ScoreBoard.svelte:130-134**

Measured: `--accent #15803d` (L 0.1593) on `--accent-soft #dff2e5` (L 0.8485) = **4.29:1**. Fails WCAG AA 4.5:1 for normal text. Affected while playing: `.points` (`+3`, 0.8rem) inside every `li.mine` row, `.record` ("Kỷ lục mới!"), `.badge.win`. Dark theme measures 7.13:1 and is fine.

Player experience: the score gain on their own words — the thing the chain is read for — is the lowest-contrast text on screen, in the light theme they will use outdoors.

Fix: this is a token decision (owned elsewhere). Either darken the accent to ≥ `#0f6b32` (≈5.3:1 on `accent-soft`), or add an `--accent-ink` token for accent-coloured text sitting on `--accent-soft` and point these three rules at it. Do not solve it per-component.

### P2-2 — The required syllable has the tightest line-height on the board

**GameBoard.svelte:134-137**

```css
.syllable strong { font-size: 1.6rem; line-height: 1.2; }
```

Player experience: syllables with stacked diacritics — `ệ`, `ộ`, `ỗ`, `ặ`, `ừ` — have their tone mark riding into the `Nối tiếp tiếng` label 2px above (`.who` margin-bottom 2px, :118). The one glyph the player must read correctly every turn is the one with no headroom.

Fix:

```css
.syllable .label { margin-bottom: 2px; }
.syllable strong { font-size: 1.6rem; line-height: 1.35; }
```

1.35 × 1.6rem = 34.6px, enough for a full `ộ` stack plus descender. Same treatment is worth applying to `.who` (:117-121) which sits directly above it.

### P2-3 — A rejected word is deleted; the player retypes it under the clock

**WordInput.svelte:86-89, :121-123**

Player experience: submit `hoa hông`, get "Không tìm thấy từ này trong từ điển", and the field is empty except the re-seeded `hoa `. In Vietnamese a rejection is very often one wrong tone mark. The player has to reconstruct the whole word from memory with the clock running, and the message never tells them what the server actually received.

Cause: :89 clears the field on a *successful send*, which is before the server's verdict. The store keeps the word (`state.rejection.word`, stores/game.svelte.js:347-350) but the markup at :122 renders only `.message`.

Fix — at minimum, show it, so the eye can spot the wrong mark without retyping:

```svelte
<p class="rejection" role="alert">
  <strong>{game.state.rejection.word}</strong> — {game.state.rejection.message}
</p>
```

```css
.rejection strong { font-weight: 600; }
```

Better, for the reasons above: restore it into the field and select it, so a retype overwrites and a correction is one edit away. In the `$effect` at :51-71, when `rejection` is what changed and the field holds only the bare seed, write back `rejection.word` and `setSelectionRange` over the part after the syllable.

### P2-4 — `confirm()` for resign covers the board while the server clock runs

**routes/play/+page.svelte:89-91**

Player experience: press "Đầu hàng" mid-turn, hesitate over the OS dialog, dismiss it — and the turn has expired, or the clock has jumped several seconds. `confirm()` blocks the main thread, so the rAF loop in CountdownRing stops (:20-27) while the server deadline does not.

Fix: two-step in place, no dialog, no blocking. Local state in GameBoard: first press swaps the label to a confirm and arms a 4s timeout that reverts it.

```svelte
<button type="button" class="resign" class:arming={arming} onclick={armOrResign}>
  {arming ? t.resignConfirm : t.resign}
</button>
```

Reuse `t.resignConfirm` or add a short `resignSure: 'Chắc chắn?'` to i18n/vi.js (`t.resignConfirm` is a full sentence and will wrap the button). Keeps the rAF loop alive and keeps the board visible.

### P2-5 — Game over: focus falls to `<body>`, and "Chơi lại" can land below the fold

**GameBoard.svelte:51-53; GameOverPanel.svelte:97-104**

Player experience:
- Keyboard/AT: the input unmounts, focus resets to the document top. A screen-reader user is told nothing — the panel has `role="group"` + `aria-label` (:37), which is not announced on insertion. To play again they Tab through the header, the badge, then the panel.
- Phone, after the common solo loss (`NO_LEGAL_MOVE` / timeout with `elimination.suggestions`): panel height ≈ h2 + reason + standings + stats + suggestion chips + record + export + actions + gaps + 40px padding ≈ 500-540px, on top of header 48 + `.top` 28 + scoreboard 60 + footer 65. At 360×640 the actions row is off-screen and the player scrolls to restart.
- The full-width button immediately under the results is `.export` ("Tải chuỗi từ", :97), not the rematch. Wrong thing in the primary slot.

Fix:
1. Move the `.export` button after the `.actions` div (:97 → after :104). One-line reorder; it is a keepsake, not the next action.
2. Announce and land focus without arming Enter. Do **not** focus the rematch button — a player who just submitted with Enter may still be holding it and would restart instantly. Focus the panel wrapper:

```svelte
<div class="panel" role="group" tabindex="-1" bind:this={panel} aria-label={...}>
```

```js
$effect(() => { if (result) panel?.focus(); });
```

```css
.panel:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
```

3. Add `aria-live="polite"` to the `h2` (:38) so the result is spoken.

### P2-6 — Urgency is signalled by colour alone, and by the worst colour pair for it

**CountdownRing.svelte:32, 55-59**

Green → red is the deuteranopia/protanopia confusion pair; roughly 1 in 12 men gets no urgency signal at all. Nothing else changes at 5s: the arc keeps shrinking at the same rate, the number keeps the same weight.

Fix — add a non-colour channel, no motion needed:

```css
.ring.urgent circle { stroke-width: 9; }
.ring.urgent .value { font-size: 1.75rem; }
```

The ring thickening and the number growing are both readable in greyscale and neither animates.

### P2-7 — Nothing guarantees the input stays above the on-screen keyboard

**app.html:5; routes/+layout.svelte:31-38; ChainHistory.svelte:91-96**

The shell is `min-height: 100dvh` and `.chain` (`flex: 1; min-height: 0; overflow-y: auto`) absorbs all vertical slack, so the document has **no scroll overflow**. With the default `interactive-widget=resizes-visual`, `dvh` does not shrink when the keyboard opens, so keyboard avoidance depends entirely on the browser panning the visual viewport. Measured stack above the input at 360px wide: header 48 + `.top` 28 + scoreboard ~60 + `.turn` 80 + three 14px gaps = ~258px, input bottom ~310px. On a 640px-tall viewport with a ~330px keyboard the input sits right on the boundary; with a taller keyboard (Vietnamese IME suggestion strip) or a shorter viewport it goes under, and there is no document scroll to recover it.

Fix, one line in app.html:5:

```html
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover, interactive-widget=resizes-content" />
```

`resizes-content` makes `dvh` shrink with the keyboard; the existing flex column then does the right thing automatically — `.chain` gives up height and the ring, syllable and input stay on screen. Chrome-only, but Safari's visual-viewport panning is the case that already works. Verify on a real 360×640 device with the Vietnamese keyboard up; my numbers are computed from the CSS, not measured.

---

## P3

- **`.who` is the faintest text on the board** (GameBoard.svelte:117-121): `0.85rem` + `--text-muted` for "Đến lượt bạn", the single most important state. Redundant with the scoreboard's `.active` row, so not a defect — but `1rem` and `color: var(--accent); font-weight: 600` when `game.state.myTurn` would make the turn hand-off readable in peripheral vision.
- **Chain rows are ~41px tall** (ChainHistory.svelte:134-136): 8px + 8px padding + 25px line. Under 44px. Not mid-turn critical (reading a definition is a lull activity), but `min-height: 44px` on `.row` costs nothing.
- **The chain yanks back to top while the player is reading** (ChainHistory.svelte:15-18): every accepted word scrolls to 0, including when the player has scrolled down to read an older definition. Guard it: `if ((list?.scrollTop ?? 0) < 48) list?.scrollTo(...)`.
- **Smooth scroll ignores `prefers-reduced-motion`** (ChainHistory.svelte:17): the global rule at app.css:111-118 only neuters CSS animations/transitions, not `scrollTo({behavior:'smooth'})`. Use `behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'`.
- **`.latest` emphasis is invisible in the dark theme** (ChainHistory.svelte:168-170): `box-shadow: var(--shadow)` is black at 40%/30% over `--surface #17201a` — nothing. Meanwhile `li.mine` carries a full accent border + tinted background, so in solo play the *alternation* marks turns and the newest word is unmarked. Use a structural cue: `border-inline-start: 3px solid var(--accent)` on `.latest`.
- **Standings render twice at game over**: ScoreBoard.svelte:14 swaps to `standings`, and GameOverPanel.svelte:44-58 lists them again. In solo that is the same two rows stacked. Suppress the panel's `ol.standings` when there are only two players, or drop it in bot mode.
- **A screen-reader player gets no time warning.** `role="timer"` (CountdownRing.svelte:35) is implicitly `aria-live="off"`, which correctly avoids 60 announcements a second but means the clock is silent. Add an `sr-only` `aria-live="polite"` node that emits only when `left` crosses 10 and 5, and only on the player's own turn.
- **The input never reports invalidity.** WordInput.svelte:101-115 has no `aria-invalid` / `aria-describedby`. Give the `.rejection` an `id` and wire both.
- **Two error surfaces, far apart.** `game.state.error` renders above the scoreboard (GameBoard.svelte:44-49) while rejections render under the input (WordInput.svelte:121). A mid-typing server error (`too_fast`, `busy`) lands ~250px away from where the eye is. Consider routing in-play server errors to the same slot as the rejection.
- **`viewport-fit=cover` with no `env(safe-area-inset-*)` anywhere.** `main` has `padding: 0 16px` (routes/+layout.svelte:64) — in landscape on a notched iPhone the board edge sits under the cutout. `padding-inline: max(16px, env(safe-area-inset-left)) ...`.
- **`<ol class="rows">` with `list-style: none`** (ChainHistory.svelte:114-122) drops list semantics in Safari/VoiceOver, so "1 of 24" is lost. Add `role="list"`.
- **DifficultyPicker is 3 fixed columns** (DifficultyPicker.svelte:50-54). At 360px each option gets ~104px minus 16px padding → "Trung bình" and "Kỷ lục: Chưa có" both wrap. Legible, just ragged. `grid-template-columns: repeat(auto-fit, minmax(96px, 1fr))` or a single column under 380px.
- **`fill()` runs 60×/s.** CountdownRing.svelte:35 recomputes `fill(t.secondsLeft, {n: left})` on every rAF tick (a regex replace + allocation) even though the string only changes once a second. Derive the label from `left`: `const timeLabel = $derived(fill(t.secondsLeft, { n: left }))`.

---

## Quick wins (comfort gained per unit of effort)

1. **Gate `urgent` on `myTurn`** and mute the ring when the clock is not the player's — CountdownRing.svelte:32. Two lines; removes a false panic spike from every single turn. (P1-1)
2. **`field.focus()` at the end of `handleSubmit`, and drop `disabled` from the `<input>`** (keep it on the button) with the syllable-corrective seed at :65 — WordInput.svelte. ~8 lines; removes one dead tap per turn on every phone. (P1-2)
3. **`interactive-widget=resizes-content`** in app.html:5. One attribute; makes the existing flex layout keyboard-safe for free. (P2-7)
4. **`line-height: 1.35` on `.syllable strong`** — GameBoard.svelte:136. One value; fixes the tone marks on the glyph the player reads most. (P2-2)
5. **Move `.export` below `.actions` and render the rejected word in the rejection line** — GameOverPanel.svelte:97 and WordInput.svelte:122. Two small edits; puts "Chơi lại" in the primary slot and stops blind retyping after a near-miss. (P2-5, P2-3)

---

## Unresolved questions

1. **What is `turnLimitMs` in solo?** It is server-owned (stores/game.svelte.js:306) and I could not read a value. `URGENT_SECONDS = 5` is only sensible relative to it — 5s of 30s is a fair warning, 5s of 10s is half the turn. Also determines whether a 10s warning tier is worth adding.
2. **How long does the bot take to answer, per difficulty?** Decides whether "the input is dead while the bot thinks" is a 400ms blink or a 3s freeze, i.e. whether P1-2's editable-field change is a convenience or essential.
3. **iOS behaviour of the P1-2 fix.** `field.focus()` inside a click-triggered submit *should* keep the keyboard up on iOS; the `disabled`→editable change *should* stop the blur. Both need a real device (iOS Safari + Android Chrome with a Vietnamese Telex keyboard). Please confirm before adopting rather than trusting my reading.
4. **Who owns `--accent` / `--accent-soft`?** P2-1 is a token fix (4.29:1, light theme only). Brief says another agent owns the token system — handing it over rather than patching three components.
5. **Is `.mine` tinting wanted in solo?** Half the chain is accent-green in a two-player game where the alternation already says whose word it is. Genuinely useful with four players. Worth deciding per mode, or worth leaving alone — taste, not a defect.
6. **Should a rejected word be restored into the field, or only displayed?** Restoring is kinder for a one-tone-mark miss but overwrites whatever the player started typing in the interim. Depends on typical rejection round-trip time, which I did not measure.
