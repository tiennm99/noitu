---
title: "Dictionary corpus switch"
description: "Replace the 179 MB minhqnd aggregate with undertheseanlp/dictionary (hongocduc + wiktionary, tudientv excluded), drop always-capitalized proper nouns, and relicense data/noitu.db to GPLv3"
status: pending
priority: P1
effort: "~1d"
tags: [dictionary, data, licensing, build]
created: 2026-09-08
blockedBy: []
blocks: []
---

# Dictionary corpus switch

## Overview

`data/noitu.db` is derived from `minhqnd/dictionary` v2.0.0: a 179 MB SQLite aggregate of
five upstreams, redistributed as CC BY-SA 4.0. Two problems. Its license chain does not
close — three of its five inputs are GPL (FVDP, tudientv, "Vietnamese Explanatory
Dictionary") and GPL does not permit relicensing to CC BY-SA. And it has already
lowercased everything (0 of 70,511 Vietnamese rows carry uppercase), which destroys the
one signal that separates a place name from a word.

This plan switches the corpus to `undertheseanlp/dictionary` — a single 4.8 MB JSONL file
carrying, per word, its raw capitalization and which of three source dictionaries contain
it. `hongocduc` (GPL) and `wiktionary` (CC BY-SA) are taken; `tudientv` is excluded
permanently because its own README declares copyright *"Chưa rõ"* and names Soha/Vietlex
(Hoàng Phê) as its primary source. Entries that appear only ever capitalized are dropped
as proper nouns. `data/noitu.db` then ships **GPLv3** — the honest license for
FVDP-derived data, and the reason this is a licensing improvement rather than a new risk.

Evidence, all measured through the real `build-dictionary` filter:
`plans/reports/research-260908-1507-undertheseanlp-dictionary.md`.

## What changes, in numbers

| | current | after |
|---|---|---|
| words | 48,216 | **~61,276** |
| syllables | 6,676 | 7,011 |
| syllables that can open a word | 5,049 | 5,496 |
| …with ≥2 continuations | 3,682 | 4,168 |
| dead-end syllables | 1,627 | **1,515** |
| source download | 179 MB SQLite | 4.8 MB JSONL |
| data license | CC BY-SA 4.0 | **GPLv3** |

24,978 words gained, 11,918 lost — of which 4,145 are proper nouns removed on purpose,
1,300 are `tudientv`-only, and 6,473 are absent from undertheseanlp entirely, including
genuine modern vocabulary (`tế bào gốc`, `hằng số vũ trụ`) that a 2003 wordlist cannot
have. That loss is accepted here and is the subject of a separate future decision, not
this plan.

## Decisions taken

- **`tudientv` is never read.** Not as content, and not as case evidence. Excluding it
  from evidence too is what makes "we do not touch that data" true without qualification.
  It costs a little accuracy in the proper-noun rule — a word capitalized in `hongocduc`
  but lowercase only in `tudientv` will be dropped — and Phase 3 measures that cost.
- **The measured 61,276 used all-source case evidence.** Under the allowed-sources-only
  rule the number will shift slightly. Phase 1 re-measures; the plan does not depend on
  the exact figure, only on it staying above the 40,000 `--min-words` floor and above
  today's 48,216.
- **Proper nouns are dropped, not tiered.** The per-word `source` array looked like a
  confidence signal and is not one: `hà nội` has 2 votes, `cẩm xá` 2, `kháng đón` 2, while
  `cánh diều` has 1 and `mặt trời` 2. Capitalization is the only usable signal in this
  data, so it is the only one used.
- **`noitu.db` becomes GPLv3; the code stays Apache-2.0.** The existing dual-license split
  in `NOTICE` holds; only its data half is rewritten.
- **The SQLite input path is deleted, not kept "just in case."** Two input modes for one
  source is dead weight; the fixture path (`--words`) stays because the e2e suite and the
  Docker smoke build use it.
- **The viwiktionary extractor is out of scope.** It remains the answer if a CC BY-SA
  corpus is ever needed again — see `research-260908-1434-viwiktionary-as-dictionary-source.md`.

## The pin

```
URL     https://raw.githubusercontent.com/undertheseanlp/dictionary/2c078cfc373b06e2980d324ce1d7bd13740c3319/dictionary/words.txt
SHA256  4c3e0e6117e4bdfa97731e135c3d4a05881889909267394a8de8d88ef79f13f0
Size    4,813,111 bytes — 79,226 JSONL rows
Commit  2c078cf (2018-12-10, the repository's last)
```

Pinned by commit rather than branch: `raw.githubusercontent.com/<repo>/<sha>/` is
immutable, so the URL and the checksum cannot disagree later. The variable names
`DICT_URL` / `DICT_SHA256` are kept so `web/tests/dictionary-source.test.js` needs no
change — only their values move.

## Phases

| # | Phase | Status |
|---|-------|--------|
| 1 | [Read the merged list](./phase-01-start.md) | Pending |
| 2 | [Pin the source](./phase-02-pin-the-source.md) | Pending |
| 3 | [Audit the corpus](./phase-03-audit-the-corpus.md) | Pending |
| 4 | [Relicense the data](./phase-04-relicense-the-data.md) | Pending |
| 5 | [Retire the SQLite path](./phase-05-retire-the-sqlite-path.md) | Pending |

Phase 3 is a gate: it may send Phase 1 back to narrow the proper-noun rule. Phase 4 must
land in the same commit as the first generated GPL-derived `noitu.db` — shipping that
database while `NOTICE` still says CC BY-SA 4.0 is the one ordering mistake with a legal
consequence.

## Success criteria

- [ ] `make fetch-dict && make dict` builds `data/noitu.db` from a 4.8 MB pinned file with
      no 179 MB download.
- [ ] The shipped database has **more than 55,000 words** and **no word that reaches it
      only via `tudientv`**.
- [ ] The proper-noun drop count is reported in the build log beside the other reject
      reasons, recorded in `meta`, and a 100-entry sample has been audited by hand.
- [ ] Playability does not regress: syllables ≥ 6,676 and dead-end syllables ≤ 1,627.
- [ ] `data/LICENSE`, `NOTICE` and `data/ATTRIBUTION.md` describe GPLv3, name both source
      branches with their own licenses, and record why `tudientv` is excluded.
- [ ] `make test-go`, `make test-web` and the e2e suite are green; the fixture build path
      is untouched.
- [ ] `--in` and its schema auto-detection are gone, and no doc still mentions the 179 MB
      download.

## Open questions

- The `hongocduc` GPL claim rests on the mirror's README; Hồ Ngọc Đức's canonical site
  404s. Assuming GPL is the conservative direction, so this does not block — but the
  evidence is a mirror, and `ATTRIBUTION.md` should say so rather than overclaim.
- The 6,473 words undertheseanlp does not have are an unquantified mix of junk and real
  modern vocabulary. Separating them needs a second live source; out of scope here.

<!-- slug: dictionary-corpus-switch -->
