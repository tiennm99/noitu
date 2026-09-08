---
phase: 1
title: "Phase 1: Read the merged list"
status: todo
priority: P1
effort: "3h"
dependencies: []
---

# Phase 1: Read the merged list

## Overview

Teach `build-dictionary` a third input: undertheseanlp's merged JSONL. It selects words by
source membership, drops entries that appear only ever capitalized, and hands the survivors
to the normalization and filtering path that already exists.

## Requirements

- Functional: read `{"text": "...", "source": ["hongocduc", ...]}` lines; keep a word when
  its sources intersect the allowed set; drop a word when every raw form of it, across the
  allowed sources, begins with an uppercase letter.
- Functional: `tudientv` rows are skipped before any decision is made about a word —
  neither its words nor its capitalization inform the output.
- Functional: the proper-noun drop is counted and printed beside the existing reject
  reasons, so the build log says what the source contained.
- Non-functional: streaming line-by-line, one pass to gather, one pass to decide. The file
  is 4.8 MB; nothing here needs to be clever.
- Non-functional: everything downstream — `accept()`, syllable indexing, alias generation,
  `verify()`, `meta` — is reused unchanged.

## Architecture

Two maps keyed by the lowercased, whitespace-collapsed word:

```
forms[key]   -> set of raw spellings seen in allowed sources   ("Mặt Trời", "mặt trời")
sources[key] -> set of allowed sources containing it           {"hongocduc", "wiktionary"}
```

A key survives when `sources[key]` is non-empty and at least one entry in `forms[key]`
does not start with an uppercase letter. `Mặt Trời` survives on the strength of its
lowercase twin; `Hà Nội`, `Trương Công Định` and `A Mú Sung` have no lowercase form and go.

Surviving keys are then fed to the same `accept()` the SQLite path feeds, so the ≥2
syllable rule, the digit/punctuation rejections and the Vietnamese phonotactic check all
apply as they do today.

Case evidence deliberately comes from allowed sources only. The alternative — reading
`tudientv` rows for their capitalization while refusing their words — would be more
accurate and would undercut the claim that we do not use that data. Phase 3 measures what
the stricter choice costs.

## Related Code Files

- Create: `server/cmd/build-dictionary/merged_list.go`
- Create: `server/cmd/build-dictionary/merged_list_test.go`
- Modify: `server/cmd/build-dictionary/main.go` — `--merged` and `--sources` flags, dispatch
  in `run()`, `sourceURL`/`sourceLicense` constants, `meta` rows, package doc comment
- Modify: `server/cmd/build-dictionary/filter.go` — add the `rejectProperNoun` reason so
  the drop is reported through the existing counter, not a bespoke log line

## Implementation Steps

1. Add `rejectProperNoun rejectReason = "only ever capitalized"` to `filter.go`. Leave
   `accept()` alone — the case decision happens before it, on evidence `accept()` cannot
   see, since it lowercases.
2. Write `merged_list.go`: decode with `encoding/json` per line (`bufio.Scanner` with a
   raised buffer — the longest line is short, but a scanner that silently truncates is a
   bad way to lose words), build the two maps, apply the rule, return words plus a count
   per reject reason.
3. Add `--merged <file>` and `--sources hongocduc,wiktionary` to `main.go`. Unknown source
   names are an error, not a silent no-op: a typo in `--sources` must not quietly ship an
   empty or wrong corpus.
4. Dispatch in `run()`: `--merged` takes precedence in the same shape `--words` already
   does. Refuse `--merged` together with `--in`/`--words` rather than picking one.
5. Point `sourceURL`, `sourceLicense` and the package doc comment at the new source. Write
   `source_commit`, `sources_kept`, `sources_excluded` and `proper_nouns_dropped` into
   `meta` — provenance in the artifact, not only in a markdown file.
6. Tests in `merged_list_test.go`, table-driven, over hand-written JSONL fixtures:
   - `Mặt Trời` + `mặt trời` → kept
   - `Hà Nội` alone → dropped as a proper noun
   - a `tudientv`-only word → absent from the output
   - a word whose only lowercase form is in `tudientv` → dropped (the documented cost)
   - `học sinh` in all three → kept once, not thrice
   - a malformed line → a clear error naming the line number, not a skipped word
7. Run against the real file and record the counts. Compare to the report's 61,276, which
   used all-source case evidence; explain the delta rather than adjusting the number.

## Success Criteria

- [ ] `go test ./cmd/build-dictionary/` passes, new tests included.
- [ ] Building from the real merged file accepts >55,000 words and logs a proper-noun drop
      count in the low thousands.
- [ ] Every one of the 11 known-bad probes from the report is absent from the output
      (`trương công định`, `trần danh án`, `tân an thạnh`, `cẩm xá`, `vĩnh điện`,
      `cam lâm`, `hà nội`, `sài gòn`, `a mú sung`, `a lưới`, `kháng đón`).
- [ ] All 10 ordinary-word probes are present (`học sinh`, `mặt trời`, `con người`,
      `bánh mì`, `cánh diều`, `xe đạp`, `giáo viên`, `hoa hồng`, `tình yêu`, `nước mắm`).
- [ ] `--sources hongocduc` alone and `--sources hongocduc,wiktionary` both build, with the
      first strictly smaller.
- [ ] No word in the output is reachable only through `tudientv`.

## Risk Assessment

**The capitalization rule eats real words.** The probe set is 21 words; the rule fires on
thousands. Signal: Phase 3's hand audit finds common words among the drops. Response:
narrow the rule to require every syllable capitalized (`Hà Nội`, not `Kháng đón`), re-audit,
and if that still misfires, stop dropping and keep the count as a log line only — a corpus
with proper nouns in it is what we ship today and is survivable.

**Allowed-sources-only case evidence drops more than expected.** Signal: the drop count
comes in far above the report's 4,145. Response: measure how many drops have a lowercase
form only in `tudientv`; if that is a large share, reconsider reading `tudientv` for case
evidence alone and say so plainly in `ATTRIBUTION.md` rather than hiding it.

**A JSON schema surprise.** The file is 8 years old and unversioned; a row could carry an
unexpected shape. Signal: decode errors on the real file. Response: the error names the
line — inspect it, and only then decide between a tolerant skip with a counted reason and
a hard failure. Silent skipping is not an option.
