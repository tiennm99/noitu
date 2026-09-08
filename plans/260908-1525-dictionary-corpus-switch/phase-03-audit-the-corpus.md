---
phase: 3
title: "Phase 3: Audit the corpus"
status: todo
priority: P1
effort: "2h"
dependencies: [1, 2]
---

# Phase 3: Audit the corpus

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
   `tudientv`-only, absent from undertheseanlp. The report measured 4,145 / 1,300 / 6,473
   under all-source case evidence; this re-measures under the shipped rule.
3. **Playability.** Words, syllables, syllables that can open a word, how many have ≥2
   continuations, and dead-end syllables — the last of these being what decides whether
   the game hands somebody an unanswerable position. Current: 48,216 / 6,676 / 5,049 /
   3,682 / 1,627.

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
5. Measure the documented cost of allowed-sources-only case evidence: how many drops have
   a lowercase form only in `tudientv`.
6. Decide: pass, narrow, or abandon the rule. Record the decision and its reason in the
   audit note — and if the rule is narrowed, Phase 1's tests change with it.

## Success Criteria

- [ ] The audit note exists, with 100 judged entries and the commands that produced them.
- [ ] ≤2 of 100 sampled drops are ordinary words.
- [ ] The new corpus has >55,000 words.
- [ ] Syllables ≥ 6,676 and dead-end syllables ≤ 1,627 — the graph is not worse than
      what players walk today.
- [ ] The three loss buckets are quantified, not estimated.
- [ ] A pass/narrow/abandon decision is written down with its reason.

## Risk Assessment

**The audit passes on a sample and the corpus is still wrong in the tail.** 100 of several
thousand is a sample, not a proof. Signal: players disputing rejected words after release.
Response: the drop list is a file — a disputed word can be checked against it in seconds,
and a per-word exception list is a small change on top of this design.

**The audit fails and the phase becomes a redesign.** Signal: >2 common words in 100.
Response: apply Phase 1's pre-decided narrowing (every syllable capitalized), re-audit
once. If it fails again, ship without the drop — the corpus is still bigger and denser
than today's, and the proper-noun problem stays exactly as bad as it currently is rather
than getting worse.

**Playability regresses in a way these five numbers do not capture.** Signal: aggregate
counts look fine but bot games end oddly short or long. Response: `server/internal/bot`'s
real-corpus tests run bot-versus-bot games on the shipped database; run them before
accepting the phase, since they exercise the graph rather than counting it.
