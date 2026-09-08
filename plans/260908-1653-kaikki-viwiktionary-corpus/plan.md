---
title: "Kaikki viwiktionary corpus"
description: "Replace the 2018 undertheseanlp wiktionary rows with kaikki.org's current extraction of Wiktionary tiếng Việt, fetched unpinned at build time, and move the data licence to CC BY-SA 4.0"
status: completed
priority: P1
effort: "~1d"
tags: [dictionary, data, licensing, build]
created: 2026-09-08
blockedBy: []
blocks: []
---

# Kaikki viwiktionary corpus

## Overview

`data/noitu.db` is built from the `wiktionary` rows of `undertheseanlp/dictionary`: a
2018-12-10 scrape of vi.wiktionary.org, 26,845 words after filtering, pinned by commit and
SHA-256 (plan `260908-1525-dictionary-corpus-switch`, completed today). It is the same
website eight years staler than it needs to be.

This plan switches the source to **kaikki.org's extraction of Wiktionary tiếng Việt** — the
`Tiếng Việt` language file of the `viwiktionary` edition, produced by wiktextract from the
monthly Wikimedia dump (currently 2026-09-01) and refreshed roughly weekly. Same authors,
same website, current text. Measured through the real `build-dictionary` filter this
session: **34,813 words**, a near-superset of today's corpus (25,392 shared, 9,421 gained,
1,453 lost) and 96% of the raw dump (36,200).

Two owner decisions shape the plan:

- **viwiktionary only.** kaikki's English-Wiktionary Vietnamese file (21,244 words, marked
  deprecated by kaikki) is not used. The game is Vietnamese; one edition, one attribution.
- **No pin — fetch the newest file every build.** kaikki publishes rolling files with no
  archived snapshots, so a checksum pin would break weekly. The owner chose freshness over
  reproducibility. The build therefore has to *fail loudly* on a bad download (HTTP error,
  malformed JSON, too few words) and *record what it actually got* (SHA-256 and row count in
  `meta`) so any shipped database can still be traced to exact bytes.

Consequence: the current text of Wiktionary is CC BY-SA **4.0** (Wikimedia moved from 3.0
on 2023-06-29), so `data/noitu.db` returns to 4.0. The 3.0 chosen this afternoon only ever
applied to the 2018 snapshot.

Evidence: `plans/reports/research-260908-1529-viwiktionary-dump-measured.md` (dump and
kaikki-en measurements) plus this session's measurement of the kaikki-vi file, recorded in
[phase 3](./phase-03-measure-the-corpus.md).

## What changes, in numbers

| | today | after |
|---|---|---|
| words | 26,845 | **34,813** (30,231 if `name` POS were dropped — it is not) |
| syllables | 5,709 | 6,081 |
| syllables with ≥2 continuations | 2,787 | 3,163 |
| dead-end syllables | 1,551 | 1,594 |
| source | 4.8 MB JSONL, 2018, pinned | **62 MB JSONL, current, unpinned** |
| data licence | CC BY-SA 3.0 | **CC BY-SA 4.0** |
| `--min-words` floor | 20,000 | **30,000** |
| build reproducible | yes | **no** — traceable via `meta.source_sha256` |

## The source

```
URL   https://kaikki.org/viwiktionary/Tiếng Việt/kaikki.org-dictionary-TiếngViệt.jsonl
      (percent-encoded: /viwiktionary/Ti%E1%BA%BFng%20Vi%E1%BB%87t/kaikki.org-dictionary-Ti%E1%BA%BFngVi%E1%BB%87t.jsonl)
Size  62,288,004 bytes on 2026-09-06 — 44,564 rows, 41,507 distinct words, all lang_code "vi"
Row   {"word": "trở thành", "pos": "verb", "lang_code": "vi", "lang": "Tiếng Việt", "senses": [...], ...}
```

Only `word` and `lang_code` are read. `pos` is counted for the build log but never filters:
the owner's earlier decision that capitalization does not remove words carries over to the
`name` POS (5,138 name-only words). 5,475 words carry uppercase and 229 lowercase forms
occur twice; `accept()` lowercases and dedupes as it always has.

## Decisions taken

- **One corpus input mode.** `--merged`/`--sources` (undertheseanlp) is replaced by
  `--kaikki`, not kept beside it. Two readers for one database is dead weight, as the SQLite
  path was. The fixture path `--words` stays.
- **The floor moves to 30,000.** 34,813 measured; a truncated or reshaped download must
  fail, and 30,000 is the highest round number with a comfortable margin.
- **Provenance is measured, not asserted.** The reader hashes the file as it streams it and
  writes `source_sha256`, `source_rows` and `source_fetched_at` into `meta`. There is no
  commit to record, so this is what "which bytes" means from now on.
- **Attribution names three links.** Wiktionary tiếng Việt contributors (authors, CC BY-SA
  4.0), wiktextract/kaikki.org by Tatu Ylonen (extraction; kaikki asks for the LREC 2022
  citation), and this project. No GitHub intermediary any more.
- **`verify-dict` goes.** There is nothing to verify against. The
  Makefile/Dockerfile agreement test keeps guarding the URL and drops the checksum clauses.

## Phases

| # | Phase | Status |
|---|-------|--------|
| 1 | [Read the kaikki file](./phase-01-start.md) | Pending |
| 2 | [Fetch the newest file](./phase-02-fetch-the-newest-file.md) | Pending |
| 3 | [Measure the corpus](./phase-03-measure-the-corpus.md) | Pending |
| 4 | [Relicense to 4.0](./phase-04-relicense-to-4-0.md) | Pending |

Phase 4 lands in the same commit as the first kaikki-built `noitu.db`: a 4.0-derived
database in a tree whose `NOTICE` says 3.0 is the one ordering mistake with a legal
consequence. Phase 3 is measurement, not a gate — there is no drop rule left to audit.

## Success criteria

- [x] `make fetch-dict && make dict` downloads kaikki's current file and builds
      `data/noitu.db` with **more than 30,000 words**; a truncated or non-JSON download fails
      the build with a message naming the cause.
- [x] `meta` records `source_url`, `source_sha256`, `source_rows`, `source_fetched_at` and
      `source_license = CC BY-SA 4.0`; no `source_commit`, `sources_kept` or
      `sources_excluded` remain.
- [x] `data/LICENSE` is the CC BY-SA 4.0 legal code; `NOTICE`, `data/ATTRIBUTION.md`, README,
      Dockerfile, deployment doc, the builder's licence string and the in-game footer all say
      4.0 and credit Wiktionary tiếng Việt contributors and wiktextract/kaikki.org.
- [x] Playability measured and recorded against today's database, including bot game
      lengths; nothing regresses below today's numbers.
- [x] `go vet ./... && go test ./...`, `npm run check && npm test`, the Docker image with the
      CI file checks and `FIXTURE_DICT=1` are green. e2e: not runnable on this machine (Playwright Chromium download fails); the one changed assertion was updated by hand, CI verifies.
- [x] No file outside `plans/` names undertheseanlp, `--merged`, `--sources` or `DICT_SHA256`.

## Review

Code review (2026-09-08) returned DONE_WITH_CONCERNS; every finding was applied: `fetch-dict`
downloads to a `.part` name and renames on success so an interrupted fetch never feeds the
next `make dict`; a failed `Stat` is an error rather than a zero `source_fetched_at`;
`builder_version` bumped to 3 for the changed meta contract; `.gitignore` covers
`data/*.jsonl`; `ATTRIBUTION.md` states kaikki's redistribution terms; bare JSON literals
are malformed rather than "foreign-language"; the non-EOF read-error line number is exact;
the CI leak guard matches any `.jsonl`; byte-exact reader tests cover a missing final
newline, an HTML error page, a mid-line cut and a bare literal. The reviewer's residual
note stands: with no pin, the build trusts kaikki.org over TLS, and the recorded SHA-256 is
the compensating control; blast radius is game content only.

## Open questions

- kaikki's weekly refresh means two builds a week apart can differ. Accepted by the owner;
  `source_sha256` in `meta` is the answer to "which words did this image ship". Should a
  release ever need to be rebuilt bit-for-bit, the fallback is to keep the fetched file as a
  release asset — a Makefile variable change, not a redesign.
- The `unknown` POS bucket (4,616 words such as `tình báo`, `trọc phú`, `hội quán`) is real
  vocabulary the vi extractor could not label. Kept, like everything else.

<!-- slug: kaikki-viwiktionary-corpus -->
