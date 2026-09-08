---
phase: 3
title: "Phase 3: Audit the corpus"
status: done
priority: P1
effort: "2h"
dependencies: [1, 2]
---

# Phase 3: Audit the corpus

> **Outcome.** Executed 2026-09-08; see [`audit-proper-noun-drops.md`](./audit-proper-noun-drops.md).
> The drop rule passed its sample gate (1 common word in 100) but cost 215 words under
> wiktionary-only case evidence, including everyday vocabulary. Narrowing was measured and
> rejected. **The owner abandoned the rule**: every word is kept regardless of case. Final
> corpus 26,845 words; playability and bot results recorded in the audit note.

## Overview

The gate. A rule that removes thousands of words on a capitalization heuristic has to be
looked at before it ships. This phase samples what was dropped, measures what the corpus
gained and lost, and confirms the game graph is not worse.

## Requirements

- Functional: a hand audit of 100 randomly sampled proper-noun drops, recorded in the
  plan directory with the verdict per entry.
- Functional: a measured diff against the current `data/noitu.db` — gained, lost, and the
  reason for each loss bucket.
- Functional: a playability comparison on the graph the game actually walks.
- Non-functional: every number in this phase is reproducible from a command written down
  in the audit note, not from a one-off shell line nobody kept.

## Architecture

Three measurements, each against the freshly built database and the current one:

1. **Drop audit.** Sample 100 of the dropped keys. Each is *proper noun* (correct),
   *common word* (a false positive, the thing we are looking for), or *unclear*. The rule
   passes at ≤2 common words in 100; that tolerance is the difference between a filter and
   a corpus edit.
2. **Corpus diff.** Overlap, gained, lost. Split the lost into: dropped as proper nouns,
   absent from the wiktionary branch. The report measured 4,335 / 22,651 of 26,986 lost;
   this re-measures from the shipped build. Separately count the **271 case casualties**
   — drops that have a lowercase form in `hongocduc` or `tudientv` — and list them in
   full; that list is what the pass/narrow/exception decision is made on.
3. **Playability.** Words, syllables, syllables that can open a word, how many have ≥2
   continuations, and dead-end syllables — the last of these being what decides whether
   the game hands somebody an unanswerable position. Current: 48,216 / 6,676 / 5,049 /
   3,682 / 1,627. Expected: 22,310 / 5,484 / 4,050 / 2,686 / 1,434. The corpus is smaller
   by design, so the question is not "is it bigger" but "does the game still run": add
   bot-versus-bot game lengths from `server/internal/bot`'s real-corpus tests on both
   databases.

## Related Code Files

- Create: `plans/260908-1525-dictionary-corpus-switch/audit-proper-noun-drops.md` —
  the sample, the verdicts, the commands that produced them
- Read only: `data/noitu.db` (current, before regeneration), the newly built database

## Implementation Steps

1. Build the new database to a scratch path, leaving the shipped one in place for
   comparison. Do not overwrite `data/noitu.db` in this phase.
2. Emit the dropped keys from the builder — a `--report-drops <file>` flag, or a temporary
   local patch if the flag would otherwise have no user. Prefer the flag: the next person
   to change this rule will want it too.
3. Sample 100 with a fixed seed, write them into the audit note, and judge each one by
   hand. Vietnamese place and person names are the expected content; anything that reads
   as an ordinary word is a false positive and gets called one.
4. Run the corpus diff and the playability comparison; put both tables in the audit note.
5. Build `--sources hongocduc,wiktionary` to a second scratch path purely to enumerate the
   case casualties (words dropped under `wiktionary` alone but kept when hongocduc's
   lowercase forms are visible). Nothing from that build ships.
6. Decide: pass, narrow, exception list, or abandon the rule. Record the decision and its
   reason in the audit note — and if the rule changes, Phase 1's tests change with it.

## Success Criteria

- [ ] The audit note exists, with 100 judged entries and the commands that produced them.
- [ ] ≤2 of 100 sampled drops are ordinary words, the 271 known case casualties aside —
      those are listed in full and judged as a group.
- [ ] The new corpus has >20,000 words.
- [ ] The five graph numbers and bot-game lengths are recorded for both databases; bot
      games on the new graph complete without the engine running out of moves earlier
      than on today's.
- [ ] The loss buckets are quantified, not estimated.
- [ ] A pass/narrow/exception-list/abandon decision is written down with its reason.

## Risk Assessment

**The audit passes on a sample and the corpus is still wrong in the tail.** 100 of several
thousand is a sample, not a proof. Signal: players disputing rejected words after release.
Response: the drop list is a file — a disputed word can be checked against it in seconds,
and a per-word exception list is a small change on top of this design.

**The audit fails and the phase becomes a redesign.** Signal: >2 common words in 100.
Response: apply Phase 1's pre-decided narrowing (every syllable capitalized) or the
exception list, re-audit once. If it fails again, ship without the drop — the proper-noun
problem then stays exactly as bad as it is today rather than getting worse.

**The corpus is too thin in play.** 22,310 words is less than half of today's. Signal: bot
games noticeably shorter, or the dead-end rate per move up rather than down. Response: this
is the trigger for the documented upgrade path — the 2026-09-01 viwiktionary dump (31,637
words, same license) as a second `--words` source — not for re-admitting GPL data.

**Playability regresses in a way these five numbers do not capture.** Signal: aggregate
counts look fine but bot games end oddly short or long. Response: `server/internal/bot`'s
real-corpus tests run bot-versus-bot games on the shipped database; run them before
accepting the phase, since they exercise the graph rather than counting it.
