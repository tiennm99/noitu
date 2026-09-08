---
title: "Dictionary corpus switch"
description: "Replace the 179 MB minhqnd aggregate with the wiktionary branch of undertheseanlp/dictionary only (hongocduc and tudientv excluded) and keep data/noitu.db on CC BY-SA 4.0"
status: completed
priority: P1
effort: "~1d"
tags: [dictionary, data, licensing, build]
created: 2026-09-08
updated: 2026-09-08
blockedBy: []
blocks: []
---

# Dictionary corpus switch

## Overview

`data/noitu.db` was derived from `minhqnd/dictionary` v2.0.0: a 179 MB SQLite aggregate of
five upstreams, redistributed as CC BY-SA 4.0. Its license chain does not close — three of
its five inputs are GPL (FVDP, tudientv, "Vietnamese Explanatory Dictionary") and GPL does
not permit relicensing to CC BY-SA.

This plan switches the corpus to the **`wiktionary` source inside `undertheseanlp/dictionary`**
— a single 4.8 MB JSONL file carrying, per word, which of three source dictionaries contain
it. Only rows whose `source` list contains `wiktionary` are read. `hongocduc` (GPL) is
excluded so the data license does not change; `tudientv` is excluded because its own README
declares copyright *"Chưa rõ"* and names Soha/Vietlex (Hoàng Phê) as its primary source.
`data/noitu.db` **stays CC BY-SA 4.0**: the wiktionary branch is a 2018-12-10 scrape of
vi.wiktionary.org, whose text is CC BY-SA.

Evidence, all measured through the real `build-dictionary` filter:
`plans/reports/research-260908-1507-undertheseanlp-dictionary.md` (branch licensing),
`plans/reports/research-260908-1529-viwiktionary-dump-measured.md` (yields, alternatives)
and [`audit-proper-noun-drops.md`](./audit-proper-noun-drops.md) (the Phase 3 gate).

## What changes, in numbers

| | before | after |
|---|---|---|
| words | 48,216 | **26,845** |
| syllables | 6,676 | 5,709 |
| syllables that can open a word | 5,049 | 4,158 |
| …with ≥2 continuations | 3,682 | 2,787 |
| dead-end syllables | 1,627 | 1,551 |
| source download | 179 MB SQLite | 4.8 MB JSONL |
| data license | CC BY-SA 4.0 | **CC BY-SA 4.0 (unchanged)** |
| `--min-words` floor | 40,000 | **20,000** |

**This is a deliberate 44% cut.** 1,280 words gained, 22,651 lost — every one of them
simply absent from a 2018 wiktionary scrape (`bánh đúc`, `lò vi sóng`, `dân làng`, `thót
tim`). Bot-versus-bot games run about a quarter shorter on the thinner graph (easy-vs-easy
12.9 moves vs 17.5). This loss is accepted here; the upgrade path is written down below.

## Decisions taken

- **Only `wiktionary` rows are read.** `hongocduc` and `tudientv` inform nothing. That is
  what keeps the license CC BY-SA 4.0 and what makes "we do not touch that data" true
  without qualification.
- **Capitalization is not a filter.** The original design dropped words that appear only
  ever capitalized, as proper nouns. It was built, run and audited at the Phase 3 gate: the
  rule was precise on a 100-word sample (1 common word) but, with wiktionary-only case
  evidence, it also removed 215 words including `mặt trời`, `trái đất`, `tổ quốc`, `dường
  như`, `phật giáo`. Narrowing was measured and did not help. **The owner chose to keep
  every word regardless of case.** Proper nouns like `hà nội` remain playable, exactly as
  they are today. The mechanism was removed from the builder rather than left behind a flag.
- **Rejected alternatives, so nobody reopens them without new evidence:**
  - `hongocduc + wiktionary` (61,271 words) — forces `noitu.db` to GPLv3. Rejected to keep
    CC BY-SA 4.0.
  - the 2026-09-01 viwiktionary dump (36,200 words without a case rule, same license,
    monthly pin) — the 2018 branch is a near-subset of it. Rejected for now in favour of the
    simpler single-file pin; **this is the documented upgrade path** if the 27k corpus
    proves too thin in play. See the 15:29 report.
  - kaikki.org enwiktionary Vietnamese — unpinnable rolling file, deprecated by its host.
- **The SQLite input path is deleted, not kept "just in case."** The fixture path
  (`--words`) stays because the e2e suite and the Docker smoke build use it.
- **The build floor drops to 20,000.** The old 40,000 default would reject the new corpus;
  it moved once, in `main.go`.

## The pin

```
URL     https://raw.githubusercontent.com/undertheseanlp/dictionary/2c078cfc373b06e2980d324ce1d7bd13740c3319/dictionary/words.txt
SHA256  4c3e0e6117e4bdfa97731e135c3d4a05881889909267394a8de8d88ef79f13f0
Size    4,813,111 bytes — 79,226 JSONL rows, 32,484 of them tagged wiktionary
Commit  2c078cf (2018-12-10, the repository's last)
```

Pinned by commit rather than branch: `raw.githubusercontent.com/<repo>/<sha>/` is
immutable, so the URL and the checksum cannot disagree later. The variable names
`DICT_URL` / `DICT_SHA256` were kept so `web/tests/dictionary-source.test.js` needed no
change — only their values moved.

## Phases

| # | Phase | Status |
|---|-------|--------|
| 1 | [Read the merged list](./phase-01-start.md) | Done |
| 2 | [Pin the source](./phase-02-pin-the-source.md) | Done |
| 3 | [Audit the corpus](./phase-03-audit-the-corpus.md) | Done — gate sent the case rule back; rule abandoned |
| 4 | [Update the attribution](./phase-04-update-the-attribution.md) | Done |
| 5 | [Retire the SQLite path](./phase-05-retire-the-sqlite-path.md) | Done |

All five phases are uncommitted in one working tree; they should land together so the
attribution never describes a database other than the one the build produces.

## Success criteria

- [x] `make fetch-dict && make dict` builds `data/noitu.db` from a 4.8 MB pinned file with
      no 179 MB download.
- [x] The shipped database has **more than 20,000 words** and **no word that reaches it
      through `hongocduc` or `tudientv` alone**.
- [x] `meta` records the source URL, commit, sources kept and sources excluded.
- [x] Playability is measured and recorded: syllables, openers, continuations, dead ends,
      and bot-versus-bot game lengths on the new graph versus the old.
- [x] `data/LICENSE` is unchanged; `NOTICE` and `data/ATTRIBUTION.md` name the new source,
      its commit and license, and record why the two other branches are excluded. The
      frontend attribution footer credits Wiktionary tiếng Việt.
- [x] `go vet ./... && go test ./...` (incl. `-race` on the touched packages), `npm run
      check && npm test`, and the Docker image (`noitu:ci` with the CI file checks, plus
      `FIXTURE_DICT=1`) are green; the fixture build path is untouched.
- [ ] The Playwright e2e suite is green. **Not verified locally**: Playwright's Chromium
      download timed out twice on this machine (2026-09-08), so every test failed at browser
      launch, not on game behaviour. The one e2e assertion this plan changes (the attribution
      footer's link text and href in `web/e2e/bot-game.spec.js`) was updated by hand. Verify
      in CI or after `npx playwright install chromium` succeeds.
- [x] `--in` and its schema auto-detection are gone, and no file outside `plans/` still
      mentions minhqnd, `dictionary.db`, `--in` or a 179 MB download.

## Licence version — decided after review

The owner asked whether the data could move to Apache-2.0 to simplify the repo. It cannot:
the words are Wiktionary contributors' text under a share-alike licence, and neither
undertheseanlp nor this project can relicense them. The owner then chose to **keep the
licence the source text actually carried, CC BY-SA 3.0 Unported**, rather than upgrade the
derivative to 4.0 under the later-version clause. `data/LICENSE` is now the BY-SA 3.0 legal
code; `NOTICE`, `ATTRIBUTION.md`, README, Dockerfile, deployment doc, the builder's meta
string and the in-game footer all say 3.0. The "unchanged" wording above predates this.

## Review

Code review (2026-09-08) returned DONE_WITH_CONCERNS; every finding was applied: the
builder's copy of the upstream commit is now guarded by `web/tests/dictionary-source.test.js`
alongside the Makefile/Dockerfile pin; fixture builds record "no upstream data" instead of
inheriting the CC BY-SA string; `ATTRIBUTION.md` states the 2018 snapshot was CC BY-SA 3.0
and why the derived database is 4.0; dead `contains()`, redundant sorts, the off-by-one
scanner line number, stale "48k" comments, the Makefile `curl` flags and the Dockerfile
download name were fixed; input-selection and fixture-licence tests were added.

## Open questions

- Is 26,845 words enough for the game to feel playable? Bot games are ~25% shorter; only
  real play can say whether that is felt. If not, the 2026 viwiktionary dump is the next
  step, not a return to GPL data.

<!-- slug: dictionary-corpus-switch -->
