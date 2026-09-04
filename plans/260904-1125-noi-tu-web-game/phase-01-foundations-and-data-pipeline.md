---
title: "Phase 1: Foundations and Data Pipeline"
status: todo
phase: 1
priority: P1
effort: "3d"
dependencies: []
---

# Phase 1: Foundations and Data Pipeline

## Overview

Stand up the repo skeleton and produce `data/noitu.db` — the derived, normalized Vietnamese
wordlist of words with **at least 2 syllables** — from the upstream `minhqnd/dictionary`
SQLite release, with full CC BY-SA 4.0 compliance. Nothing downstream can be built or
tested without this data.

## Requirements

**Functional**
- [x] Reproducible build: upstream `dictionary.db` → `data/noitu.db`, runnable by any contributor
- [x] Output contains only Vietnamese (`lang_code = 'vi'`) entries of **2 or more space-separated syllables**
- [x] All words NFC-normalized and lowercased
- [x] Tone-placement aliases resolved (`hoà`→`hòa`, `thuý`→`thúy`, `quí`→`quý`, …)
- [x] Precomputed `first`/`last` syllable columns and out-degree table for O(1) engine lookups
- [x] Build fails loudly if entry count falls below a floor (40,000) or any row has fewer than 2 syllables
- [x] `--max-syllables` flag available (default: no cap) so a phrase cap can be applied later without a code change

**Non-functional**
- [x] Output DB is a few MB, opens read-only, no writes at runtime
- [x] License artifacts are correct and consistent across all five locations

## Architecture

**Source:** [`github.com/minhqnd/dictionary`](https://github.com/minhqnd/dictionary) release
**v2.0.0** → [`dictionary.db`](https://github.com/minhqnd/dictionary/releases/download/v2.0.0/dictionary.db),
**179 MB** (verified via the GitHub API; not in git; 357k+ entries, 1,500+ language pairs).
Code MIT, **data CC BY-SA 4.0**.

We do **not** republish a derived dictionary — every build downloads this asset and derives
locally. See `plan.md` → Data Distribution for the CI and Docker consequences.

**Derived schema** (`data/noitu.db`):

```sql
CREATE TABLE words (
  word     TEXT PRIMARY KEY,   -- normalized, e.g. "pháp luật"
  first    TEXT NOT NULL,      -- first syllable, "pháp"
  last     TEXT NOT NULL,      -- last syllable, "luật"
  syllables INTEGER NOT NULL   -- 2, 3, 4, …
) WITHOUT ROWID;
CREATE INDEX idx_words_first ON words(first);

-- Out-degree per syllable: how many words start with it. Drives bot heuristics
-- and instant dead-end detection.
CREATE TABLE syllables (
  syllable   TEXT PRIMARY KEY,
  out_degree INTEGER NOT NULL
) WITHOUT ROWID;

-- Accepted spelling variants that map to a canonical word. Built offline so the
-- runtime never has to reason about Vietnamese tone placement.
CREATE TABLE aliases (
  variant   TEXT PRIMARY KEY,
  canonical TEXT NOT NULL REFERENCES words(word)
) WITHOUT ROWID;

CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);
-- rows: source_url, source_license, built_at, word_count, builder_version
```

**Why an alias table instead of a runtime normalizer:** Vietnamese old-style vs new-style
tone placement (`hoà` vs `hòa`) produces genuinely different codepoint sequences. A runtime
algorithm to re-place tone marks is fiddly and easy to get subtly wrong; generating both
variants once at build time is data, cheap to fix, and trivially testable. KISS.

**Pipeline:**

```
dictionary.db ──► filter lang_code='vi'
              ──► NFC normalize (golang.org/x/text/unicode/norm) + lowercase + collapse spaces
              ──► keep len(strings.Fields(w)) >= 2   (and <= --max-syllables when set)
              ──► drop entries containing digits, latin-only tokens, or punctuation
              ──► dedupe
              ──► generate tone-placement variants → aliases
              ──► compute first/last/syllables + out_degree
              ──► write noitu.db + meta
              ──► assert: count >= 40000, every row >= 2 syllables, no orphan aliases
```

**Note on the graph:** allowing 3+ syllable words does not change the graph model — an edge
still runs `first ──word──► last`; it may simply span more syllables in between. Out-degree,
dead-end detection, and the engine's lookups are unaffected.

## Related Code Files

- Create: `server/cmd/build-dictionary/main.go` — CLI: `--in dictionary.db --out data/noitu.db [--max-syllables N]`
- Create: `server/cmd/build-dictionary/filter.go` — vi + ≥2-syllable + junk filters
- Create: `server/cmd/build-dictionary/aliases.go` — tone-placement variant generation
- Create: `server/cmd/build-dictionary/main_test.go` — filter/alias unit tests with fixtures
- Create: `data/LICENSE` — CC BY-SA 4.0 full text
- Create: `data/ATTRIBUTION.md`
- Create: `NOTICE`
- Create: `README.md` (replace stub) — quickstart, architecture, **License** section
- Create: `.gitignore` — `data/*.db` (covers both the 179 MB upstream and the derived DB), `web/node_modules`, `web/build`, server binaries
- Create: `Makefile` (or `Taskfile.yml`) — `make fetch-dict`, `make dict`, `make server`, `make web`, `make test`
- Create: `server/go.mod` — `module github.com/tiennm99dev/noitu/server`, Go 1.25+ (raised by modernc.org/sqlite; local toolchain is 1.26.5)
- Modify: none (repo is empty apart from `LICENSE` + `README.md`)

## Implementation Steps

1. `.gitignore`, `Makefile`, `server/go.mod` (`go mod init`), directory skeleton per plan layout.
2. Write `data/LICENSE` (verbatim CC BY-SA 4.0) and `data/ATTRIBUTION.md` naming: source repo + release URL, upstream sources (Wiktionary, `vntk/dictionary`), license URL, and the explicit modification list from `plan.md`.
3. Write `NOTICE` + README **License** section stating the split: Apache-2.0 code / CC BY-SA 4.0 data, and that the two are distributed as separate artifacts.
4. `server/cmd/build-dictionary`: open upstream DB read-only via `modernc.org/sqlite`, stream `vi` words.
5. Normalization: `norm.NFC`, `strings.ToLower`, `strings.Fields` → join with single space.
6. Filters: at least 2 fields (and ≤ `--max-syllables` when set); reject any token containing a digit, ASCII-only letters, or punctuation.
7. Alias generation: for each canonical word, emit old-style tone-placement variants for the `oa/oe/uy` nuclei; skip a variant if it collides with a different canonical word (log collisions).
8. Compute `first`, `last`, `syllables`, `out_degree`; write output DB in one transaction; write `meta` rows.
9. Assertions: word count ≥ 40,000; 100% of rows ≥ 2 syllables; every alias resolves; fail non-zero otherwise.
10. `make fetch-dict` downloads the 179 MB asset from the pinned release URL into `data/` (resumable, checksum-logged); `make dict` derives from it. Both documented in the README as one-time setup.
11. Unit tests on fixtures: normalization, ≥2-syllable filter (including a 3- and a 4-syllable entry accepted and a 1-syllable rejected), junk rejection, alias generation, collision handling.

## Success Criteria

- [x] `make fetch-dict && make dict` produces `data/noitu.db` from the upstream release
- [x] `sqlite3 data/noitu.db "SELECT COUNT(*) FROM words"` ≥ 40,000
- [x] Zero rows where `word` has fewer than 2 syllables; 3- and 4-syllable entries present
- [x] `hoà`, `thuý`, `quí` each resolve through `aliases` to a canonical word present in `words`
- [x] `meta` records source URL, license, build timestamp, word count
- [x] `data/LICENSE`, `data/ATTRIBUTION.md`, `NOTICE`, README license section all present and mutually consistent
- [x] `go test ./cmd/...` green
- [x] `data/*.db` is git-ignored

## Risk Assessment

| Risk | Signal | Response |
|---|---|---|
| Upstream schema differs from README-documented shape | Query errors on first run | Inspect actual tables with `.schema` first; adapt the extraction query — the pipeline stages after extraction are schema-independent |
| Fewer than 40k clean ≥2-syllable entries | Count assertion fails | Union with [Viet74K](https://vietnamese-wordlist.duyet.net/Viet74K.txt) filtered to ≥2 syllables; add it as a second `--in` source and extend `ATTRIBUTION.md` |
| 179 MB download is slow or the release URL moves | `make fetch-dict` fails or stalls | URL is pinned to release `v2.0.0`, download is resumable, and the derived DB is cached locally so the fetch is a one-time cost per contributor |
| Allowing 3+ syllables admits multi-word phrases that are not really words | Playtest complaints | `--max-syllables` flag already present; applying a cap is a rebuild, not a code change |
| Upstream data contains proper nouns / non-words that make the game feel wrong | Playtest complaints in later phases | Add a denylist file consumed by the builder; regenerate — no code change needed |
| Alias generation creates false accepts (a variant that is a different real word) | Collision log non-empty | Collisions are skipped by design and logged; review the log before release |
| CC BY-SA obligations misread | License review | Attribution + share-alike on the data artifact only; code untouched. `NOTICE` states the boundary explicitly |
