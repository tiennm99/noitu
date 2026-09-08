---
phase: 1
title: "Phase 1: Read the kaikki file"
status: completed
priority: P1
effort: "3h"
dependencies: []
---

# Phase 1: Read the kaikki file

## Overview

Replace the undertheseanlp reader in `build-dictionary` with one for kaikki's wiktextract
JSONL, hashing the input as it streams so the database records exactly which bytes it was
built from.

## Requirements

- Functional: `--kaikki <file>` reads one JSON object per line, keeps rows whose
  `lang_code` is `vi`, and feeds `word` to the existing `accept()`.
- Functional: rows with any other `lang_code` are counted and rejected, not silently
  skipped — the file is vi-only today, and a change in that would be worth seeing in the log.
- Functional: `pos` is tallied per value and printed with the reject counts. It never filters.
- Functional: the SHA-256 of the file and the row count are computed during the single
  streaming pass and written to `meta` as `source_sha256` and `source_rows`, with
  `source_fetched_at` taken from the file's modification time.
- Functional: `--merged` and `--sources` are removed along with `merged_list.go` and its
  tests. Exactly one of `--kaikki` / `--words` must be given, same rule as today.
- Functional: the `--min-words` default moves from 20,000 to 30,000.
- Non-functional: a malformed line is an error naming the line, as today. Lines can be
  long — a row carries every sense and translation — so the scanner buffer must allow
  several megabytes; measure the longest line on the real file and set the cap above it
  with headroom, or switch to `bufio.Reader.ReadBytes('\n')` which has no cap.
- Non-functional: `finish()`, `accept()`, aliases, `write`, `verify` unchanged.

## Architecture

```
--kaikki <file>
     │  one line at a time, bytes also fed to sha256.New()
     ▼
 json.Unmarshal → {word, lang_code, pos}
     │
     ├─ lang_code != "vi"  → rejects["not Vietnamese-language entry"]++
     ▼
 accept(word)   (NFC, lowercase, ≥2 syllables, alphabet, phonotactics)
     ▼
 finish()       (floor 30,000, aliases, atomic write, verify)
```

Decode only the three fields via a struct; `encoding/json` ignores the rest, so the 62 MB
of senses and translations cost I/O but not memory.

`sourceSpec.extra` already exists for mode-specific meta rows; the kaikki provenance uses it:

```
source_url          the kaikki URL (constant kaikkiSourceURL)
source_sha256       hex of the streamed bytes
source_rows         lines decoded
source_fetched_at   file mtime, RFC 3339 UTC
source_license      CC BY-SA 4.0 (https://creativecommons.org/licenses/by-sa/4.0/)
```

## Related Code Files

- Create: `server/cmd/build-dictionary/kaikki_list.go`
- Create: `server/cmd/build-dictionary/kaikki_list_test.go`
- Delete: `server/cmd/build-dictionary/merged_list.go`, `merged_list_test.go`
- Modify: `server/cmd/build-dictionary/main.go` — flags, dispatch, doc comment, `--min-words`
  default, `mergedProvenance` call site
- Modify: `server/cmd/build-dictionary/main_test.go` — `fixtureSource`/`defaultRows` emit
  kaikki-shaped rows (`{"word": ..., "pos": ..., "lang_code": "vi"}`); the "excluded source"
  row becomes a `lang_code: "en"` row

## Implementation Steps

1. Write `kaikki_list.go`: `kaikkiRow{Word, Pos, LangCode}`, `readKaikkiList(path, maxSyllables)`
   returning words, rejects, a `pos` tally, and a `kaikkiProvenance` (sha256, rows, mtime).
   Wrap the file in `io.TeeReader` into `sha256.New()` so hashing is free.
2. Add `rejectNotVietnamese rejectReason = "not a Vietnamese-language entry"` to `filter.go`.
3. In `main.go`: replace `merged`/`sources` with `kaikki` in `config`, flags and `run()`;
   `--min-words` default 30,000; `runFromKaikkiList` logs rejects, the POS tally
   (sorted, one line) and the accepted count, then calls `finish()` with the provenance spec.
4. Delete the merged reader and its tests. Rewrite `main_test.go` fixtures to kaikki rows.
5. Tests in `kaikki_list_test.go`: a `vi` row kept; an `en` row rejected and counted;
   `Hà Nội` kept as `hà nội`; a word listed twice with different `pos` kept once; a malformed
   line names its number; `meta` carries a 64-hex `source_sha256` equal to `sha256sum` of the
   fixture file, the right `source_rows`, and no `source_commit`/`sources_*` keys.
6. Run against the real file (`scratchpad/kk-vi-edition.jsonl` from this session, or a fresh
   download) and record the counts in phase 3.

## Success Criteria

- [x] `go test ./cmd/build-dictionary/` passes; `merged_list*.go` no longer exist.
- [x] Real file builds >30,000 words (measured 34,813) and logs a POS tally.
- [x] `build-dictionary --help` lists `--kaikki`, `--words`, `--out`, `--max-syllables`,
      `--min-words` and nothing else.
- [x] `meta.source_sha256` of a build equals `sha256sum` of the input file.
- [x] The fixture path (`--words ... --min-words 150`) is byte-for-byte unaffected in
      behaviour: same 205 words from `testdata/fixture-words.txt`.

## Risk Assessment

**A row exceeds the scanner buffer.** Signal: `bufio.Scanner: token too long` on the real
file. Response: pre-decided — use `bufio.Reader` with no line cap rather than guessing a
buffer size; measure the longest line once and note it in the phase result.

**kaikki changes the row shape.** `word` and `lang_code` are wiktextract's stable core fields
and unlikely to move. Signal: zero accepted words → the 30,000 floor fails the build with a
clear message. Response: read the new shape, adjust the struct; the failure mode is loud by
design.

**The file arrives partially.** `curl -f` catches HTTP errors but not a truncated body, and
a body cut exactly on a line boundary parses cleanly. Signal: a malformed final line (error
names it) or a word count under the 30,000 floor (13.8% headroom on 34,813). Response: the
fetch writes to a `.part` name and renames only on success (review finding, Phase 2), so an
interrupted download is never left for the next `make dict`; the floor covers the rest.
