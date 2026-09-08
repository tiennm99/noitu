---
phase: 1
title: "Phase 1: Read the merged list"
status: done
priority: P1
effort: "3h"
dependencies: []
---

# Phase 1: Read the merged list

## Overview

Teach `build-dictionary` a third input: undertheseanlp's merged JSONL. It selects words by
source membership and hands them to the normalization and filtering path that already
exists.

## Requirements

- Functional: read `{"text": "...", "source": ["hongocduc", ...]}` lines; keep a word when
  its sources intersect the allowed set — **`wiktionary` only** by default.
- Functional: `hongocduc` and `tudientv` rows are skipped before any decision is made about
  a word. They inform nothing.
- Functional: capitalization is not a filter. `Hà Nội` and `hà nội` are the same word and
  land as the lowercase form, exactly as every other input mode already behaves.
- Functional: the `--min-words` default moves from 40,000 to 20,000. The new corpus is
  ~26,800 words; the old floor would reject it.
- Non-functional: streaming line-by-line. The file is 4.8 MB; nothing here needs to be
  clever.
- Non-functional: everything downstream — `accept()`, syllable indexing, alias generation,
  `verify()`, `meta` — is reused unchanged.

## Architecture

```
--merged <file>  --sources wiktionary
        │
        ▼  one row at a time
   sources ∩ allowed ≠ ∅ ?  ── no ──▶ skipped, never counted
        │ yes
        ▼
     accept()   (NFC, lowercase, ≥2 syllables, alphabet, phonotactics)
        │
        ▼
     finish()   (floor, aliases, atomic write, verify)  ← shared with --words and --in
```

The `--sources` flag is kept general — a comma list validated against the three known
names — even though only `wiktionary` is passed, so a comparison build against another
branch is a flag away and never a code patch. Unknown names are an error, not a no-op.

`finish()` is new: the three input modes used to each carry their own copy of the floor
check, alias generation, write and verify. One copy is what keeps a fixture from drifting
into a different shape from the database production loads.

## Related Code Files

- Created: `server/cmd/build-dictionary/merged_list.go`
- Created: `server/cmd/build-dictionary/merged_list_test.go`
- Modified: `server/cmd/build-dictionary/main.go` — `--merged` and `--sources` flags,
  mutually-exclusive input check, `finish()`, `sourceSpec.url` / `sourceSpec.extra`, meta
  rows, package doc comment, `--min-words` default
- Unchanged: `server/cmd/build-dictionary/filter.go`

## Implementation Steps

1. Write `merged_list.go`: decode with `encoding/json` per line (`bufio.Scanner` with a
   raised buffer — a scanner that silently truncates is a bad way to lose words), skip rows
   with no allowed source, feed the rest to `accept()`.
2. Add `--merged <file>` and `--sources wiktionary` (the default) to `main.go`. Change the
   `--min-words` default to 20,000 in the same edit; the fixture path passes its own floor
   explicitly and is unaffected.
3. Dispatch in `run()`: exactly one of `--merged`, `--words`, `--in` may be given. Refuse
   two rather than picking one.
4. Write `source_url`, `source_commit`, `sources_kept` and `sources_excluded` into `meta` —
   provenance in the artifact, not only in a markdown file.
5. Tests in `merged_list_test.go`, table-driven, over hand-written JSONL fixtures:
   - `Hà Nội` tagged wiktionary → kept as `hà nội`
   - a `hongocduc`-only word and a `tudientv`-only word → absent from the output
   - `học sinh` in all three, plus `Học sinh` → kept once
   - a malformed line → a clear error naming the line number, not a skipped word
   - `--sources hongocduc,wiktionary` → strictly more words; `tudientv` still absent
   - meta rows as above
6. Run against the real file and record the counts.

## Result

```
rejected      2: contains a digit
rejected    147: contains punctuation
rejected   5335: fewer than 2 syllables
rejected     87: no Vietnamese letters
accepted 26845 distinct words (sources: wiktionary)
generated 1507 spelling aliases
```

The first cut of this phase also implemented a capitalization-based proper-noun drop
(`only ever capitalized` → rejected, `--report-drops` for audit). It reached the Phase 3
gate, was audited, and was removed by the owner's decision — see `plan.md` and
`audit-proper-noun-drops.md`. Nothing of it remains in the code.

## Success Criteria

- [x] `go test ./cmd/build-dictionary/` passes, new tests included.
- [x] Building from the real merged file accepts >20,000 words (26,845).
- [x] Ordinary-word probes present: `học sinh`, `bánh mì`, `xe đạp`, `giáo viên`, `hoa
      hồng`, `tình yêu`, `nước mắm`, `mặt trời`, `trái đất`. `con người` and `cánh diều` are
      not in the 2018 branch at all — expected, recorded.
- [x] `--sources wiktionary` (default) and `--sources hongocduc,wiktionary` both build, with
      the first strictly smaller (26,845 vs 61,271 with no case rule).
- [x] No word in the output is reachable only through `hongocduc` or `tudientv`.

## Risk Assessment

**A JSON schema surprise.** The file is 8 years old and unversioned; a row could carry an
unexpected shape. Signal: decode errors on the real file. Response: the error names the
line — inspect it, and only then decide between a tolerant skip with a counted reason and
a hard failure. Silent skipping is not an option. (Observed: none; all 79,226 rows decode.)
