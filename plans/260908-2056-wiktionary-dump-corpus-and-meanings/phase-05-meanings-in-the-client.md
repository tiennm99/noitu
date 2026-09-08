---
phase: 5
title: "Phase 5: Meanings in the client"
status: completed
priority: P1
effort: "5h"
dependencies: [4]
---

# Phase 5: Meanings in the client

## Overview

Show a word's meanings under it in the chain, open for the newest word, closed for the rest,
with a click on any word toggling its own; keep the open/closed set in the store so the
rules are testable without a browser, and cover the behaviour end to end.

## Requirements

- Functional: `ChainEntry` gains `meanings: {pos: string, gloss: string}[]`, filled from
  `played.meanings` and `value.openingMeanings`.
- Functional: the store keeps `expanded: Set<string>` of words whose meaning is open, and
  `toggleMeaning(word)`. Any number of words may be open at once. Rules (validation
  session 1: they apply to every word, with or without definitions):
  - `gameStarted`: `expanded = {openingWord}`.
  - `turnUpdate` with a word: remove the previous newest word (`chain[chain.length-1].word`
    before the push), add the new word. A `turnUpdate` without a word (an elimination)
    changes nothing.
  - `toggleMeaning(word)`: flip membership.
  - `reset()` clears the set. A resumed session gets `gameStarted` then one `turnUpdate` and
    ends up with only the last word open, which is the same as a fresh one.
- Functional: `ChainHistory.svelte` renders every word as a `<button type="button">` with
  `aria-expanded` and `aria-controls` pointing at its panel. The panel is shown when the word
  is in `expanded`: an `<ol class="meanings">` with one `<li>` per sense rendered as
  `(pos) gloss`, or `gloss` alone when `pos` is empty; when the word has no senses the panel
  is a single `<p class="meanings none">` with `t.meaningNone`. Everything else in the row —
  who played it, badges, points, the correction note — is unchanged.
- Functional: strings in `web/src/lib/i18n/vi.js`: `meaningShow` / `meaningHide` for the
  button's `aria-label` (`Xem nghĩa của {word}` / `Ẩn nghĩa của {word}`) and `meaningNone`
  (`Chưa có nghĩa`).
- Functional: the auto-scroll effect keeps working — the newest row is at the top and its
  open list is what the scroll lands on.
- Functional: `history-export.js` is unchanged (non-goal).
- Non-functional: the meanings are rendered as text (`{sense}`), never with `{@html}`.
- Non-functional: the row stays legible on a phone: the list wraps under the word at full
  row width (`flex-basis: 100%`), muted colour, ~0.85rem, numbered by the `<ol>`.
- Non-functional: `prefers-reduced-motion` respected if any open/close transition is added;
  the simplest is none.

## Architecture

```
game.svelte.js
  state.chain[i].meanings    from the wire
  state.expanded             Set<string>, client-only
  toggleMeaning(word)

ChainHistory.svelte
  <li>
    <button class="word" type="button" aria-expanded={open} aria-controls={id}
            aria-label={fill(open ? t.meaningHide : t.meaningShow, { word: entry.word })}
            onclick={() => game.toggleMeaning(entry.word)}>{entry.word}</button>
    … by / meta / corrected as today …
    {#if open}
      {#if entry.meanings.length}
        <ol class="meanings" {id}>
          {#each entry.meanings as sense}<li>{sense.pos ? `(${sense.pos}) ` : ''}{sense.gloss}</li>{/each}
        </ol>
      {:else}
        <p class="meanings none" {id}>{t.meaningNone}</p>
      {/if}
    {/if}
  </li>
```

`id` is derived from the row's index in the chain, not from the word, so two rows never
share one (a word is never played twice, but the opening word could in principle be typed
as a later variant).

The e2e helper `chainWords` selects `ol li .word`; the class stays on the button so the
helper and every existing spec keep working, and the nested `<ol class="meanings">` has no
`.word` inside it.

## Related Code Files

- Modify: `web/src/lib/stores/game.svelte.js` — `ChainEntry` typedef, `expanded`,
  `toggleMeaning`, the `gameStarted`/`turnUpdate`/`reset` branches
- Modify: `web/src/lib/components/ChainHistory.svelte` — markup and styles
- Modify: `web/src/lib/i18n/vi.js` — three strings
- Modify: `web/tests/game-store.test.js` — the expansion rules above, each as a case
- Modify: `web/tests/i18n.test.js` — only if it enumerates keys
- Modify: `web/e2e/helpers.js` — `openMeanings(page)` returning the words whose list is
  visible; `web/e2e/bot-game.spec.js` — newest open, previous closed after a move, click
  toggles; `web/e2e/pvp-game.spec.js` — the other browser sees the same word open
- Modify: `testdata/fixture-words.txt` — meanings for the words the specs play (phase 3)

## Implementation Steps

1. Store first, with tests: opening open; new word closes previous and opens itself; a word
   with no meanings is opened the same way; elimination leaves the set alone; toggle flips
   and two words can be open together; reset clears; the resume sequence ends with one open.
2. Component markup and styles; check both themes and a 360px viewport.
3. i18n strings; run the copy test.
4. e2e: fixture meanings, helper, three assertions. CI runs the browser; locally, run what
   Playwright allows.
5. `npm run check && npm test`.

## Success Criteria

- [x] `npm run check && npm test` green; store tests cover all seven rules.
- [x] In a bot game the opening word's meaning is open; after the bot's first move only the
      bot's word is open; clicking the opening word opens it again while the bot's stays
      open, and clicking the bot's word closes it.
- [x] Senses render as `(danh từ) …`; a sense with an empty label renders the gloss alone; a
      word without meanings opens to `Chưa có nghĩa`.
- [x] e2e specs assert the three behaviours and pass in CI.
- [x] Nothing in the transcript export changed.

## Risk Assessment

**The button steals focus from the word input.** The README says focus is left alone while
a player types, and a chain row that grabs focus on render would break that. Signal: typing
interrupted after a move. Response: never call `focus()` in the component; the button is
only focusable by the user's own click or Tab.

**Long meanings push the chain off screen on a phone.** Five senses of 200 characters is a
tall row. Signal: the newest row fills the viewport. Response: the cap is server-side and
already chosen; if it proves too tall, `max-height` with `overflow-y: auto` on the list is
a style change. Do not shorten text client-side — it would differ from what the server sent.

**A resumed session opens two words.** `gameStarted` opens the opening word; the replayed
`turnUpdate` must close it. Covered by the rule ordering and a store test using the resume
sequence.
