---
phase: 6
title: "Phase 6: Measure and attribute"
status: completed
priority: P1
effort: "3h"
dependencies: [2, 3]
---

# Phase 6: Measure and attribute

## Overview

Record what the first dump-built database contains against the kaikki one, sample the
stripper's output, and rewrite every attribution surface so it names the Wikimedia dump,
drops kaikki.org and wiktextract, and stops claiming the database holds only word forms.

## Requirements

- Functional: `measurement.md` beside this plan, same shape as the kaikki plan's: the file
  measured (URL, SHA-256, `source_pages`, `source_fetched_at`, size), the build log, the
  graph metrics (words, syllables, openers, openers ≥2, dead ends) old vs new, bot-vs-bot at
  60 games, the corpus diff (shared / gained / lost with samples), wall time of the build,
  and the meanings numbers: `meaning_count`, `words_with_meaning` and its percentage, the
  distribution of senses per word, the share of senses with an empty part-of-speech label
  and the unmapped heading codes by frequency, the ten commonest dropped template names, and
  a 50-sense random sample read by a person with each marked fine / terse / wrong.
- Functional: `data/ATTRIBUTION.md` — source table names the dump
  (`viwiktionary-latest-pages-articles.xml.bz2`, monthly, unpinned), removes the wiktextract
  row and the LREC citation, states the chain is now Wiktionary tiếng Việt contributors →
  this project. Modifications list: item 1 becomes "section selection — the Vietnamese
  section of each page, either markup dialect; redirects skipped"; a new item describes the
  definition text: taken from `#` lines, markup stripped, templates other than the listed
  ones removed, capped at five senses of 200 characters, each labelled with the Vietnamese
  name of the part-of-speech heading it sat under, so the text is an *excerpt and
  modification* of the entry, not the entry; item 8 ("dropped fields") says everything else
  — examples, translations, pronunciations, etymologies, categories — is dropped, and the
  sentence "contains only word forms, not meanings" is deleted. The
  reproduce section names the new commands.
- Functional: `NOTICE` — affected artifacts name the dump file; the extraction lines go;
  "identified by the SHA-256 recorded in the database's meta table" stays.
- Functional: README — licence section drops wiktextract/kaikki; Setup and the raw commands
  already changed in phase 2; a sentence under the game description says the chain shows
  each word's Wiktionary meaning.
- Functional: `docs/deployment.md` "Updating the dictionary" says dumps.wikimedia.org and
  monthly.
- Functional: the in-game footer (`AttributionFooter.svelte`, `attributionSource`) already
  says Wiktionary tiếng Việt and links vi.wiktionary.org; verify and leave.
- Functional: `verify` in the builder and the pin test are the only two places allowed to
  encode numbers from this measurement (the coverage floor; the URL). Nothing else.
- Non-functional: the measurement is a record, not a gate — the plan's decision that nothing
  regresses is checked against the numbers, and a regression is reported to the owner, not
  silently accepted or silently fixed.

## Architecture

```sh
cp data/noitu.db /tmp/kaikki.db                    # before the first dump build
make fetch-dict && make dict
cd server && go test ./internal/bot/ -run RealCorpus -v -count=1   # per database
```

```sql
SELECT COUNT(*) FROM words;  SELECT COUNT(*) FROM syllables;
SELECT COUNT(*) FROM (SELECT first FROM words GROUP BY first HAVING COUNT(*) >= 2);
SELECT COUNT(*) FROM syllables WHERE out_degree = 0;
SELECT COUNT(*) FROM meanings;  SELECT COUNT(DISTINCT word) FROM meanings;
SELECT pos, COUNT(*) FROM meanings GROUP BY pos ORDER BY 2 DESC;   -- '' is the unmapped share
SELECT n, COUNT(*) FROM (SELECT word, COUNT(*) n FROM meanings GROUP BY word) GROUP BY n;
SELECT word, gloss FROM meanings ORDER BY RANDOM() LIMIT 50;
ATTACH '/tmp/kaikki.db' AS old;
SELECT COUNT(*) FROM words w JOIN old.words o ON o.word = w.word;        -- shared
SELECT word FROM words WHERE word NOT IN (SELECT word FROM old.words) ORDER BY RANDOM() LIMIT 25;  -- gained
SELECT word FROM old.words WHERE word NOT IN (SELECT word FROM words) ORDER BY RANDOM() LIMIT 25;  -- lost
```

## Related Code Files

- Create: `plans/260908-2056-wiktionary-dump-corpus-and-meanings/measurement.md`
- Modify: `data/ATTRIBUTION.md`, `NOTICE`, `README.md`, `docs/deployment.md`
- Verify only: `web/src/lib/components/AttributionFooter.svelte`, `web/src/lib/i18n/vi.js`

## Implementation Steps

1. Before the first real dump build, copy today's `data/noitu.db` aside.
2. Build; run the queries and the real-corpus bot tests on both databases; fill
   `measurement.md`.
3. Read the 50 senses; mark each; if more than ~10 are wrong for the same reason, that is a
   phase-1 stripper fix, made now, and the sample re-drawn.
4. Rewrite the four documents. Run `npx vitest run tests/dictionary-source.test.js` (README
   and ATTRIBUTION quote the URL).
5. `grep -rniI "kaikki\|wiktextract\|jsonl\|only word forms" --exclude-dir=plans .` finds
   nothing.
6. Plan status via `ak plan`; journal.

## Success Criteria

- [x] `measurement.md` complete with every table above; words ≥ 34,813 and no graph metric
      below the kaikki database's, or the regression is written up and shown to the owner.
- [x] Meaning coverage recorded; the 50-sense sample recorded with verdicts.
- [x] `data/ATTRIBUTION.md`, `NOTICE`, README and `docs/deployment.md` describe the dump and
      the definition text accurately; the pin test passes.
- [x] The grep in step 5 is empty.

## Risk Assessment

**The dump has fewer words than kaikki for some syllables.** Research measured the dump as a
near-superset (36,200 vs 34,813 through the same filter), but the two are different months
by the time this runs. Signal: `lost` is in the thousands. Response: list them, look for a
parser cause (a section boundary cut, a dialect miss) before accepting; a genuine wiki
deletion is accepted and recorded.

**The stripper sample is bad.** Signal: more than a fifth of 50 marked wrong. Response: fix
the stripper for the commonest cause in this plan; ship with the rest recorded; the
follow-up is a plan of its own, because it is template-by-template work against a moving
wiki.

**Attribution says less than the law wants.** CC BY-SA 4.0 asks for attribution, a licence
link, and an indication of modifications. The definition text makes the "modifications"
line load-bearing: the database now redistributes edited excerpts of the entries. The
ATTRIBUTION rewrite above is the compliance; a reviewer should read it against the licence
text once. Signal: none in code. Response: do the review.
