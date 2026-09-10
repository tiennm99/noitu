# UI/UX review — global layout, design system, responsiveness, accessibility

Date: 2026-09-10 · Scope: `web/src/app.css`, `app.html`, `+layout.svelte`, all 15 components' style blocks, route-level containers
Method: static read of every `<style>` block + live measurement (Edge/Playwright, `vite dev`, 360/400/768/1280 × light/dark) + WCAG ratios computed from the literal hex values in `app.css`.

## Verdict

The green repaint landed cleanly and the "keep the board on screen" fix is structurally correct — zero hardcoded colours in any component, `color-scheme` set in both themes, theme applied pre-paint, no horizontal scroll and exact 16px gutters at all four widths. Three real WCAG AA failures remain in the palette itself (`--border` at 1.47:1 as a form-control boundary, `--accent` at 4.29:1 as text on its own tint), and the single biggest gap is that **turn changes are never announced** — the one state change the whole game hangs on. Spacing and type have no scale tokens at all, which is where the visible inconsistency comes from.

## What works (verified, not assumed)

- **Colour token discipline is near-perfect.** `grep -rE '#[0-9a-f]{3,8}|rgba?\(|hsla?\('` across all 15 components and 3 routes: **zero matches**. Every colour resolves through a custom property, exactly as `app.css:1-5` claims.
- **Theme correctness.** Inline pre-paint script (`app.html:15-25`) sets `data-theme` before `%sveltekit.head%`; `color-scheme` declared in *both* blocks (`app.css:40`, `app.css:65`) — measured `getComputedStyle(html).colorScheme` = `light`/`dark` respectively. No token exists in only one theme (all 17 are declared twice). Persistence via `settings.svelte.js:117-126` with full storage guards (`safeStorage`/`read`/`write`, lines 19-51) that degrade to in-memory. No flash of wrong theme.
- **Responsive baseline.** `document.scrollWidth - clientWidth === 0` at 360, 400, 768, 1280 in both themes on `/` and `/online`; content gutter measured exactly 16px at 360 and 400. No element crosses the viewport edge. `100dvh` alongside `100vh` (`+layout.svelte:34-35`).
- **The board fix holds.** At 360×640 the chain gets a 263px internal scroll area while `.input-row` stays pinned at y=214 and the syllable at y=120 — the chain grows inside itself, not the page (`ChainHistory.svelte:91-96` + `:114-122`, `GameBoard.svelte:86-93`). Moving `.resign` *above* the chain (`GameBoard.svelte:74-80`) is the right call and the comment explains why.
- **No `outline: none` anywhere.** Focus is never suppressed.
- **Neutrals and semantics pass comfortably.** `--text` 15.35/15.39, `--text-muted` 4.88–7.64, `--danger` 6.04–7.46, `--warn` 5.11–10.11, all four seat colours 5.51–10.10 — every one passes AA body in both themes. The four-seat palette deliberately avoids the accent's green and it works.
- **Vietnamese typography is fine.** `lang="vi"` (`app.html:2`); `system-ui` → Segoe UI on Windows, SF on macOS, Roboto on Android, all with full diacritic coverage; body `line-height: 1.5` (`app.css:84`) leaves headroom for stacked marks. Verified in render: "Kỷ lục: Chưa có", "Đấu trực tuyến", "Nối tiếp tiếng" all clean at 360px, no clipping.
- **iOS zoom guarded.** Every text input is `font-size: 1rem` (`WordInput:140`, `ChatPanel:322`, `NicknameInput:45`, `online:406`) with the reason documented.
- **A11y patterns that are already right:** `role="alert"` on errors (`GameBoard:45`, `WordInput:122`, `ChatPanel:162`, `online:263`), `aria-live="polite"` on the connection badge (`ConnectionBadge:19`), `role="status"` on player banners (`PlayerStatus:55,57,64`), `aria-expanded`/`aria-controls` on chain rows (`ChainHistory:41-44`), real `<input type="radio">` in `DifficultyPicker` rather than fake ARIA radios, `<header>`/`<main>`/`<footer>` landmarks present.

---

## Findings

### P1 — broken or inaccessible for real users

**P1-1. Turn changes are never announced.**
`GameBoard.svelte:57` — `<p class="who" data-testid="turn-indicator">{turnLabel}</p>`
- **Symptom:** the single most important state change in the game ("Đến lượt bạn") reaches nobody who isn't looking at that line. A screen-reader user, or a player who tabbed away, learns nothing until the turn clock forfeits them.
- **Cause:** no `aria-live`/`role`. The pattern exists elsewhere in the same file (`GameBoard:45` uses `role="alert"`), so this is an omission, not a decision.
- **Fix:**
  ```svelte
  <p class="who" role="status" aria-live="polite" aria-atomic="true" data-testid="turn-indicator">{turnLabel}</p>
  ```

**P1-2. `--border` fails WCAG 1.4.11 (3:1) as the boundary of every form control and secondary button, in both themes.**
`app.css:13` `--border: #c2d1c8` · `app.css:47` `--border: #3a4a40`
- **Symptom:** light theme — inputs are white on `#f4f7f4` (1.05:1 fill difference) with a 1.47:1 border, so the field edge is effectively absent. Dark theme is worse in render: the room-code input at 1280 is barely findable (see measurement run). Low-vision users cannot locate the fields.
- **Ratios:** light `#c2d1c8` vs `#ffffff` **1.59**, vs `#f4f7f4` **1.47**, vs `#e9f0ea` **1.37**. Dark `#3a4a40` vs `#17201a` **1.78**, vs `#101512` **1.96**, vs `#1f2a23` **1.58**. All fail 3:1.
- **Cause:** one `--border` token serves both decorative card edges (where 1.4:1 is legitimate) and interactive component boundaries (where 3:1 is required). Sites: inputs `NicknameInput:42`, `WordInput:135`, `ChatPanel:318`, `online:403`; buttons `+page.svelte:48`, `Lobby:259`, `GameOverPanel:261`, `RoomCodePanel:98`, `online:418`; `ThemeToggle:26`; `Lobby:232`, `GameBoard:183`.
- **Fix:** split the token, keep `--border` for decoration.
  ```css
  :root        { --border-strong: #768b7f; } /* 3.64 / 3.37 / 3.14 — all pass */
  [data-theme='dark'] { --border-strong: #647f70; } /* 4.23 / 3.82 / 3.40 */
  ```
  Then swap `var(--border)` → `var(--border-strong)` at the twelve interactive sites above.

**P1-3. `--accent` as small text on its own tint fails 4.5:1 in the light theme.**
`app.css:19` `--accent: #15803d`
- **Symptom:** the points readout, the win badge and the record marker — the three things a player actually reads a score off — are under AA in the default theme.
- **Ratios (light):** `#15803d` on `--accent-soft #dff2e5` = **4.29** (FAIL); on `--surface-alt #e9f0ea` = **4.33** (FAIL). On `--surface`/`--bg` it passes (5.02 / 4.65). Dark passes everywhere (7.13–8.54).
- **Cause / sites:** `ChainHistory:214-218` `.points` renders inside `.rows > li.mine` whose background is `--accent-soft` (`:158-161`) at 0.8rem/600. `ScoreBoard:130-134` `.badge.win` — accent on accent-soft at 0.7rem/700. `GameOverPanel:234-241` `.record` — accent on accent-soft at 1rem/700 (not "large text": needs 18.66px bold). `ConnectionBadge:44-46` — accent on `--surface-alt` at 0.85rem.
- **Fix:** darken the accent one step. `--accent: #12692f` gives on-accent-soft **5.82**, on-surface-alt **5.87**, white-on-accent **6.80** (up from 5.02), on-bg **6.30** — every existing pair improves, nothing regresses, dark theme untouched. If the exact hue must be preserved, add `--accent-strong: #12692f` and use it only for the four text sites above.

### P2 — noticeable inconsistency or friction

**P2-4. Chat messages are silent to assistive tech.**
`ChatPanel.svelte:145` — `<ol bind:this={list} class:column data-testid="chat-log">`
- Symptom: incoming messages are never announced; the unread badge (`:136-138`) only exists in the collapsed variant.
- Fix: `<ol ... aria-live="polite" aria-relevant="additions">`.

**P2-5. The `/online` join screen jams against the left edge on tablet and desktop.**
`+layout.svelte:14` `const wide = $derived(page.route.id === '/online')` + `online/+page.svelte:312-314`
- **Symptom (measured):** at 1280 the shell widens to 1040 but the form is capped at 480 and never centred — 136px of gutter on the left, **664px on the right**. At 768: 16px left, 272px right. The header brand and theme toggle sit ~500px away from the content they belong to, and the footer is centred on a different axis than the form. Visibly broken alignment.
- **Cause:** `wide` is keyed to the route, but the route has two states (join form vs room). Only the room needs 1040.
- **Fix (one line):** `online/+page.svelte:312` → `.online:not(.room) { max-width: 480px; margin-inline: auto; }`. Better: derive the shell width from room state rather than route id.

**P2-6. The chain list can collapse to zero height on a short viewport.**
`ChainHistory.svelte:114-122` (`.rows { overflow-y: auto }`) inside `+layout.svelte:59-64` (`main { flex: 1; min-height: 0 }`) inside `.shell { min-height: 100dvh }` (`:34-35`)
- **Symptom:** with the on-screen keyboard open (visual viewport ~340–420px tall) the chain vanishes and the page offers no scroll to get it back.
- **Cause:** `overflow-y: auto` sets a flex item's *automatic minimum size* to 0, so `.rows` shrinks to nothing instead of overflowing and growing the document. Measured at 360×420 the chain floors at 52px — and only because the empty-state `<p class="empty">` has a content minimum; the real `.rows` has none. The shell is pinned at exactly `100dvh` (docH === vpH at 640), so there is no page scroll to fall back on.
- **Fix:** `ChainHistory.svelte:114` → add `min-height: 4.5rem;` to `.rows`. The page then grows and scrolls rather than swallowing the list.

**P2-7. All three routes share one document title.**
`app.html:7` `<title>Nối Từ</title>`; no `<svelte:head>` anywhere in `src/`.
- Symptom: tab strip, browser history and screen-reader page announcement are identical on `/`, `/play` and `/online`.
- Fix: per-route `<svelte:head><title>Chơi với máy · Nối Từ</title></svelte:head>`.

**P2-8. Heading order starts at `<h2>` on two of three routes.**
`+page.svelte` (landing) has no heading at all — the only title is the header `<a class="brand">` (`+layout.svelte:19`). `/play` renders `ChainHistory:22` `<h2>` and `GameOverPanel:38` `<h2>` under no `h1`. `/online` has an `<h1>` only in the join branch (`online:257`); once in a room it disappears while `ChainHistory`/`ChatPanel` still emit `h2`.
- Fix: `<h1 class="sr-only">` per route (or promote the landing tagline to `<h1>` with `.tagline` styling), and one in `GameBoard`.

**P2-9. The reduced-motion escape hatch makes the connection dot flicker instead of stopping.**
`app.css:111-118` sets `animation-duration: 0.01ms !important` · `ConnectionBadge.svelte:57-59` `animation: pulse 1.2s ease-in-out infinite`
- Symptom: an infinite animation at 0.01ms restarts ~100 000× per frame, so `opacity` resamples arbitrarily every frame — a flicker delivered to exactly the users who asked for less motion.
- Fix: in the same block add
  ```css
  animation-iteration-count: 1 !important;
  scroll-behavior: auto !important;
  ```

**P2-10. Two JS smooth scrolls override `prefers-reduced-motion` entirely.**
`ChainHistory.svelte:17` `list?.scrollTo({ top: 0, behavior: 'smooth' })` · `ChatPanel.svelte:80` `list.scrollTo({ top: list.scrollHeight, behavior: 'smooth' })`
- Symptom: an explicit `behavior` option beats any CSS rule, so the chain and chat still animate. The chain scrolls on *every* move.
- Fix: `behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'` (or a tiny shared helper, used by both).

**P2-11. `viewport-fit=cover` is opted into with no safe-area padding anywhere.**
`app.html:5` · `grep -rn "env(\|safe-area" src/` → **no matches**
- Symptom: on a notched iPhone in landscape the 16px gutter (`+layout.svelte:64`) sits under the cutout, and the footer's licence text sits under the home indicator.
- Fix: `+layout.svelte:59-65` and `:44-50` →
  ```css
  padding-left: max(16px, env(safe-area-inset-left));
  padding-right: max(16px, env(safe-area-inset-right));
  ```
  and `AttributionFooter.svelte:28` → `padding: 20px 16px max(28px, env(safe-area-inset-bottom));`

**P2-12. Dismiss/icon targets are inconsistent, and one is below WCAG 2.2 minimum.**
`GameBoard.svelte:152-165` does it right (44×44 with the padding negative-margined back out, comment and all). `ChatPanel.svelte:301-307` — the same `×` button with no width/height: ~18×18px, **fails 2.5.8 Target Size Minimum (24×24)**. `Lobby.svelte:228-238` `.kick` is 26×26 — passes 24, but is 40% of the 44 used elsewhere. `ThemeToggle.svelte:24-25` is 36×36, the only interactive element in the header.
- Fix: lift the `GameBoard` recipe into `app.css` as `.icon-button { width: 44px; height: 44px; display: inline-grid; place-items: center; }` and use it in all four places.

**P2-13. Focus indication: one recipe, six copies, and buttons excluded.**
`ChainHistory:149-152` (offset `-2px`), `ChatPanel:325-328`, `DifficultyPicker:75-78` (`:focus-within`), `NicknameInput:48-51`, `WordInput:143-146`, `online:411-414` — all `outline: 2px solid var(--accent)`, five with `outline-offset: 1px` and one with `-2px`. No `<button>` gets one; every button falls back to the UA ring, which is a different colour and thickness from the inputs beside it.
- Fix: delete all six and put one rule in `app.css`:
  ```css
  :where(a, button, input, summary, [tabindex]):focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  ```
  (Accent-on-bg is 4.65 light / 10.61 dark, so the ring itself passes 1.4.11.) Keep `DifficultyPicker`'s `:focus-within` override, since the real input there is `.sr-only`.

**P2-14. No spacing scale and no type scale exist.** See the token drift inventory below.

### P3 — polish

**P3-15. The idle countdown ring is effectively invisible.**
`CountdownRing.svelte:64-66` `.ring.idle { color: var(--border) }` and `:81-83` `.track { stroke: var(--border) }` → `#c2d1c8` on `#f4f7f4` = **1.47:1**, failing the 3:1 that 1.4.11 asks of a meaningful graphic. Confirmed in render: the between-turns ring reads as a smudge.
- Fix: `--border-strong` for `.track`, `--text-muted` for `.ring.idle` (5.29:1).

**P3-16. `/play` and `/` are a 560px column in a 1280px field.** Measured: `.board` is 528px wide with 669px of height inside an 800px viewport, and the chain's empty state leaves ~430px of dead background below; the landing leaves ~250px. The two-column pattern that would fix this already exists and works — `online/+page.svelte:334-362`. Consider `@media (min-width: 900px)` on `/play`: board + syllable + input left, chain right. Taste, not defect.

**P3-17. Uppercase Vietnamese micro-headings run tight.**
`ChainHistory:98-105`, `ChatPanel:204-218`, `GameOverPanel:202-209`: `text-transform: uppercase; letter-spacing: 0.04em` at 0.8–0.85rem. Uppercase Vietnamese stacks tone marks above caps ("CHUỖI TỪ", "TRÒ CHUYỆN") and there is no explicit `line-height`, so it inherits 1.5 and just clears. Add `line-height: 1.6` for headroom, or drop the uppercase — sentence case is easier to read in Vietnamese and loses nothing here.

**P3-18. `--radius` is nearly dead and the pill radius is unowned.** `var(--radius)` (12px) appears **twice** (`GameOverPanel:115`, `RoomCodePanel:71`) while `--radius-sm` carries the whole UI; `border-radius: 999px` is hardcoded **8 times** (`ChainHistory:209`, `ChatPanel:227`, `ConnectionBadge:31`, `GameOverPanel:224`, `GameOverPanel:237`, `Lobby:192`, `Lobby:233`, `ScoreBoard:124`). Add `--radius-pill: 999px`; decide whether `--radius` earns its keep.

**P3-19. Skip link absent.** Only two tab stops precede `main`, so low impact — but a `<a class="skip" href="#main">` is four lines.

**P3-20. `.sr-only` (`app.css:100-109`) omits `border: 0`.** Harmless today (nothing inside it has a border) but it is the one line missing from the canonical recipe.

---

## Contrast audit

Computed from the literal hex values in `app.css` (sRGB relative luminance, WCAG 2.x). Thresholds: **4.5** body text, **3.0** large text (≥18.66px bold / ≥24px) and non-text UI boundaries.

### Light theme (`app.css:6-41`)

| Pair | Hex on hex | Ratio | Body | Large/UI | Where |
|---|---|---|---|---|---|
| `--text` / `--bg` | `#16211b` on `#f4f7f4` | 15.35 | PASS | PASS | body |
| `--text` / `--surface` | `#16211b` on `#ffffff` | 16.57 | PASS | PASS | cards, inputs |
| `--text` / `--surface-alt` | `#16211b` on `#e9f0ea` | 14.29 | PASS | PASS | `GameOverPanel:153` |
| `--text` / `--accent-soft` | `#16211b` on `#dff2e5` | 14.18 | PASS | PASS | `ChainHistory:160` |
| `--text-muted` / `--bg` | `#5b6a61` on `#f4f7f4` | 5.29 | PASS | PASS | tagline, hints |
| `--text-muted` / `--surface` | `#5b6a61` on `#ffffff` | 5.71 | PASS | PASS | — |
| `--text-muted` / `--surface-alt` | `#5b6a61` on `#e9f0ea` | 4.92 | PASS | PASS | `ChainHistory:207-212` |
| `--text-muted` / `--accent-soft` | `#5b6a61` on `#dff2e5` | 4.88 | PASS | PASS | `.mine .by` |
| `--text-muted` / `--danger-soft` | `#5b6a61` on `#fbe4e2` | 4.70 | PASS | PASS | — |
| `--accent-text` / `--accent` | `#ffffff` on `#15803d` | 5.02 | PASS | PASS | primary buttons |
| `--accent` / `--bg` | `#15803d` on `#f4f7f4` | 4.65 | PASS | PASS | `ChainHistory:216` (unhighlighted) |
| `--accent` / `--surface` | `#15803d` on `#ffffff` | 5.02 | PASS | PASS | `GameOverPanel:128` |
| **`--accent` / `--surface-alt`** | `#15803d` on `#e9f0ea` | **4.33** | **FAIL** | PASS | `ConnectionBadge:45` (0.85rem) |
| **`--accent` / `--accent-soft`** | `#15803d` on `#dff2e5` | **4.29** | **FAIL** | PASS | `ChainHistory:214`, `ScoreBoard:131`, `GameOverPanel:238` |
| `--danger` / `--danger-soft` | `#a3231c` on `#fbe4e2` | 6.15 | PASS | PASS | error banners |
| `--danger` / `--surface` | `#a3231c` on `#ffffff` | 7.46 | PASS | PASS | — |
| `--danger` / `--bg` | `#a3231c` on `#f4f7f4` | 6.91 | PASS | PASS | `.resign` |
| `--warn` / `--surface-alt` | `#8a5a08` on `#e9f0ea` | 5.11 | PASS | PASS | `PlayerStatus:79`, `ConnectionBadge:50` |
| **`--border` / `--surface`** | `#c2d1c8` on `#ffffff` | **1.59** | FAIL | **FAIL** | input/button edges |
| **`--border` / `--bg`** | `#c2d1c8` on `#f4f7f4` | **1.47** | FAIL | **FAIL** | input/button edges, `CountdownRing:82` |
| **`--border` / `--surface-alt`** | `#c2d1c8` on `#e9f0ea` | **1.37** | FAIL | **FAIL** | `RoomCodePanel:98`, `online:418` |
| `--player-1..4` / `--surface` | `#1d5c8f`/`#7a3f8f`/`#8a4c12`/`#0e6e7a` on `#ffffff` | 7.05 / 7.15 / 6.72 / 5.95 | PASS | PASS | chat log |

### Dark theme (`app.css:43-66`)

| Pair | Hex on hex | Ratio | Body | Large/UI | Where |
|---|---|---|---|---|---|
| `--text` / `--bg` | `#e6ece7` on `#101512` | 15.39 | PASS | PASS | body |
| `--text` / `--surface` | `#e6ece7` on `#17201a` | 13.92 | PASS | PASS | cards, inputs |
| `--text` / `--surface-alt` | `#e6ece7` on `#1f2a23` | 12.39 | PASS | PASS | — |
| `--text` / `--accent-soft` | `#e6ece7` on `#1e3a2a` | 10.34 | PASS | PASS | `.mine` rows |
| `--text-muted` / `--bg` | `#9aab9f` on `#101512` | 7.64 | PASS | PASS | — |
| `--text-muted` / `--surface` | `#9aab9f` on `#17201a` | 6.92 | PASS | PASS | — |
| `--text-muted` / `--surface-alt` | `#9aab9f` on `#1f2a23` | 6.15 | PASS | PASS | — |
| `--text-muted` / `--accent-soft` | `#9aab9f` on `#1e3a2a` | 5.14 | PASS | PASS | — |
| `--accent-text` / `--accent` | `#0c1410` on `#5ddc8b` | 10.75 | PASS | PASS | primary buttons |
| `--accent` / `--surface` | `#5ddc8b` on `#17201a` | 9.60 | PASS | PASS | — |
| `--accent` / `--surface-alt` | `#5ddc8b` on `#1f2a23` | 8.54 | PASS | PASS | `ConnectionBadge:45` |
| `--accent` / `--accent-soft` | `#5ddc8b` on `#1e3a2a` | 7.13 | PASS | PASS | `.points`, `.badge.win` |
| `--danger` / `--danger-soft` | `#f2867d` on `#3a201e` | 6.04 | PASS | PASS | error banners |
| `--danger` / `--surface` | `#f2867d` on `#17201a` | 6.74 | PASS | PASS | — |
| `--warn` / `--surface-alt` | `#e9b949` on `#1f2a23` | 8.13 | PASS | PASS | `PlayerStatus:79` |
| **`--border` / `--bg`** | `#3a4a40` on `#101512` | **1.96** | FAIL | **FAIL** | input/button edges |
| **`--border` / `--surface`** | `#3a4a40` on `#17201a` | **1.78** | FAIL | **FAIL** | input/button edges |
| **`--border` / `--surface-alt`** | `#3a4a40` on `#1f2a23` | **1.58** | FAIL | **FAIL** | `online:418` |
| `--player-1..4` / `--surface` | `#79b9ec`/`#c193e0`/`#e6a563`/`#63cfdc` on `#17201a` | 7.92 / 6.77 / 7.89 / 9.13 | PASS | PASS | chat log |

**Summary:** 2 failing pairs in dark (both `--border`), 5 in light (3 × `--border`, 2 × `--accent`-on-tint). Everything else clears AA with margin. The palette is well constructed; the failures are both cases of one token doing two jobs.

---

## Token drift inventory

Colour drift: **none**. The drift is entirely in spacing, type and radii, because those tokens were never created.

### Missing: spacing scale
`app.css:6-66` declares no `--space-*`. Every margin/padding/gap is a literal. Distinct values in use: `1, 2, 4, 6, 7, 8, 10, 12, 14, 16, 18, 20, 24, 26, 28` px — a near-continuous 2px ramp with no rhythm.
- Near-duplicate pairs doing the same job: `padding: 8px 12px` (`ChainHistory:139`) vs `10px 12px` (`ChatPanel:294`, `GameBoard:145`, `WordInput:169`, `PlayerStatus:76`) vs `12px 14px` (`Lobby:165`, `NicknameInput:41`, `online:402`) — three paddings for "a card/field row".
- Container gaps: `20px` (`+page.svelte:31`), `16px` (`online:305`), `14px` (`GameBoard:90`, `Lobby:141`), `12px` (`GameOverPanel:112`), `10px`, `8px`, `6px`, `4px` — eight gap values across six sibling-level containers.
- Off-ramp one-offs: `26px` (`Lobby:229-230`, `.kick` size), `7px` (`ChainHistory:208`, `ScoreBoard:123`), `18px` (`online:417`), `-12px -6px` (`GameBoard:158`).
- **Fix:** `--space-1: 4px … --space-8: 32px` (4px base), then snap the ramp. `6px`, `7px`, `10px`, `14px`, `18px`, `26px` are the values to eliminate.

### Missing: type scale
**15 distinct `font-size` values** across 18 files, none tokenised:

| rem | count | notes |
|---|---|---|
| 0.85 | 13 | de facto "small" |
| 0.9 | 11 | de facto "small" too |
| 0.8 | 10 | third "small" |
| 1 | 6 | inputs (deliberate, iOS) |
| 0.7 | 4 | |
| 0.75 | 4 | |
| 1.4 | 3 | |
| 1.1 | 3 | |
| 2, 1.6, 1.5, 1.3, 1.05, 0.95, 0.78 | 1 each | **one-offs** |

- `0.78rem` at `ChainHistory:223` is indistinguishable from the `0.8rem` used 10 times — pure noise.
- `0.95rem` at `ChatPanel:267` and `1.05rem` at `ChainHistory:173` are likewise single-use nudges off the ramp.
- Three interchangeable "small" sizes (0.8/0.85/0.9) are the main source of visual inconsistency between panels: `GameBoard:104` uses 0.85, `Lobby:247` uses 0.9, `AttributionFooter:31` uses 0.8, all for the same class of secondary text.
- **Fix:** `--text-xs: 0.75rem; --text-sm: 0.875rem; --text-base: 1rem; --text-lg: 1.125rem; --text-xl: 1.5rem; --text-2xl: 2rem`. Collapse 0.78→0.8→`xs`/`sm`, 0.95→`base`, 1.05→`base`, 1.1→`lg`.

### Radii
`--radius` (12px) used 2×; `--radius-sm` (8px) used ~20×; `999px` hardcoded 8×; `50%` hardcoded 2× (`ThemeToggle:27`, `ConnectionBadge:39`). Add `--radius-pill: 999px`.

### Duplicated component recipes (DRY, not tokens)
- The accent primary button is redeclared **6×**: `+page.svelte:57-61`, `GameOverPanel:267-271`, `Lobby:265-269`, `WordInput:153-159`, `ChatPanel:330-337`, `online:374-381` — with four different paddings (`14px`, `12px`, `14px 20px`, `10px 16px`).
- The bordered surface button is redeclared **5×**: `+page.svelte:46-55`, `Lobby:256-263`, `GameOverPanel:258-265`, `RoomCodePanel:96-102`, `online:416-422`.
- The disabled state (`background: var(--surface-alt); color: var(--text-muted)`) is redeclared **4×**: `WordInput:162-165`, `ChatPanel:339-342`, `Lobby:276-280`, `WordInput:148-151`.
- The `:focus-visible` ring **6×** (see P2-13).
- The uppercase micro-heading **3×** (`ChainHistory:98-105`, `ChatPanel:204-218`, `GameOverPanel:202-209`).
- **Fix:** three shared classes in `app.css` — `.btn`, `.btn-primary`, `.field-label` — plus the global focus rule. Svelte scoped styles don't prevent using global utility classes for this.

---

## Quick wins (top 5)

1. **Announce the turn.** One attribute set on `GameBoard.svelte:57`: `role="status" aria-live="polite" aria-atomic="true"`. Biggest accessibility gain in the app for the least code.
2. **Add `--border-strong`** (`#768b7f` light / `#647f70` dark) and swap it in on the ~12 input/button/toggle borders. Clears the only 1.4.11 failures and makes dark-theme form fields findable.
3. **Darken `--accent` to `#12692f`.** One line in `app.css:19`; clears both remaining AA text failures and raises white-on-accent from 5.02 to 6.80. Dark theme untouched.
4. **Centre the join screen:** `margin-inline: auto` on `online/+page.svelte:312`. One line, fixes the most visible layout defect (664px right gutter at 1280).
5. **One global focus ring** in `app.css` + delete the six copies, and add `animation-iteration-count: 1 !important; scroll-behavior: auto !important;` to the existing reduced-motion block (`app.css:111-118`). Consistency plus a fixed flicker in ~10 lines net negative.

---

## Unresolved questions

1. **Accent hue vs. contrast.** Darkening `--accent` to `#12692f` is the KISS fix, but the green was chosen deliberately ("word-game green", `app.css:16-18`). Is a slightly darker green acceptable brand-wise, or should this be a second `--accent-strong` token used only for accent-coloured text on tints?
2. **`/play` at desktop width.** Widening the play screen to two columns (board left, chain right) at ≥900px would use the laptop viewport properly, but it changes the game's shape and duplicates the `online` grid. Is the deliberate 560px "one column of reading" (`+layout.svelte:10-14`) a decision to preserve, or was it just never revisited for `/play`?
3. **Footer weight on game screens.** The CC BY-SA attribution is a legal obligation and must reach the player, but it costs 86px of measured height on every screen including `/play` at 360×420. Is a compacted single-line variant (smaller padding, still visible, still linked) acceptable to whoever owns the licence decision?
4. **`--radius` (12px)** survives at two call sites. Keep it as the "panel" radius and apply it more widely, or retire it and standardise on `--radius-sm`?
5. Report was requested at `plans/reports/…-1045-…`; the session hook advertises `web/plans/reports/…-1046-…` (a directory that does not exist). Written to the path in the task brief — confirm which is canonical.
