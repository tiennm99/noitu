---
title: Planned the Wiktionary dump corpus and word meanings
date: 2026-09-08
summary: "Six-phase plan: read the Wikimedia viwiktionary dump in Go, carry stripped definitions into the database and the wire, show them in the chain with toggle and auto-open"
---

# Planned the Wiktionary dump corpus and word meanings

## What happened

The owner asked to replace the kaikki.org-derived dictionary with one built from
Wiktionary's own dump, and mid-scout added a second request: show each played word's
meaning in the chain, click to toggle, newest word open, previous one closed on a new word.

Scouted the two earlier research reports (the dump was already measured at 36,200 accepted
titles vs kaikki's 34,813), the builder, the store, the proto, the chain component and the
reconnect path (which replays only the opening and the last move). Fetched four raw
wikitext pages to see what a definition line looks like in both markup dialects: `# ...`
under a POS heading, links and templates to strip, `{{place|vi|...}}` and `{{label|vi|...}}`
as the definition-shaped templates.

## Decision

- Source: `dumps.wikimedia.org/viwiktionary/latest/...pages-articles.xml.bz2`, unpinned
  (owner's choice, asked explicitly because dated directories would have allowed a pin).
- Meanings: all senses, capped at 5 x 200 chars (owner's choice).
- Parser in Go inside `build-dictionary` (`compress/bzip2` + `encoding/xml` + a small lossy
  wikitext stripper); `--kaikki` replaced by `--dump`; nothing dropped by label.
- New `meanings(word, ord, gloss)` table; `Store.Meanings()`; `PlayedWord.meanings` and
  `GameStarted.opening_meanings` on the wire; expansion state client-only in the store.
- Attribution must be rewritten in the same commit as the first dump build: the current
  `data/ATTRIBUTION.md` says the database holds only word forms.

Plan: `plans/260908-2056-wiktionary-dump-corpus-and-meanings/` (6 phases, validated with
`ak plan validate`, pinned with `ak plan use`).

## Friction

`set-active-plan.cjs` in the claude-code adapter fails with
`Cannot find module '../hooks/lib/ck-config-utils.cjs'`; not fixed (skill script, not
authorized). `ak plan use` succeeded, so the cross-session pointer is set.

## Next steps

Owner review of the plan, then `/ak:plan validate` or `/ak:cook`.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
