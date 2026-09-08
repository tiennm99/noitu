---
phase: 2
title: "Phase 2: Pin the source"
status: todo
priority: P1
effort: "2h"
dependencies: [1]
---

# Phase 2: Pin the source

## Overview

Point the build machinery at the new file: pinned by commit and checksum, fetched by
`make fetch-dict` and by the Docker dictionary stage, with the existing agreement test
still guarding both.

## Requirements

- Functional: `make fetch-dict` downloads the pinned JSONL and verifies its SHA-256;
  `make dict` builds `data/noitu.db` from it.
- Functional: the Docker `dict` stage does the same, and its fixture escape hatch
  (`FIXTURE_DICT=1`) keeps working untouched.
- Functional: the fetched source file is gitignored — we distribute the derived database,
  never a copy of their file.
- Non-functional: `DICT_URL` / `DICT_SHA256` keep their names so
  `web/tests/dictionary-source.test.js` continues to guard Makefile/Dockerfile agreement
  with no change to the test.

## Architecture

The pin is a commit-addressed raw URL, which is immutable — unlike `master`, where the
URL and the checksum could silently diverge:

```make
DICT_URL    := https://raw.githubusercontent.com/undertheseanlp/dictionary/2c078cfc373b06e2980d324ce1d7bd13740c3319/dictionary/words.txt
DICT_SHA256 := 4c3e0e6117e4bdfa97731e135c3d4a05881889909267394a8de8d88ef79f13f0
DICT_SRC    := data/undertheseanlp-words.jsonl
```

`fetch-dict` and `verify-dict` keep their current shape — `curl` then `sha256sum -c` —
because that is already the right shape; only the size in the help text changes.

## Related Code Files

- Modify: `Makefile` — `DICT_URL`, `DICT_SHA256`, `DICT_SRC`, the `dict` target's flags
  (`--merged` in place of `--in`), and the `help` text's "~179 MB"
- Modify: `Dockerfile` — `ARG DICT_URL`, `ARG DICT_SHA256`, the `curl`/`sha256sum` lines,
  the `build-dictionary` invocation, and the `FIXTURE_DICT` comment's 179 MB reference
- Modify: `.gitignore` — ignore `data/undertheseanlp-words.jsonl` (`data/*.db` already
  covers the derived and old source databases)
- Verify unchanged: `web/tests/dictionary-source.test.js`, `.github/workflows/ci.yml`

## Implementation Steps

1. Update the three Makefile variables and the `dict` target to pass `--merged $(DICT_SRC)`.
2. Reword `help`, `fetch-dict` and the `$(DICT_SRC)` guard: the download is ~4.8 MB now,
   and saying "179 MB" would be the kind of stale comment that outlives three refactors.
3. Mirror all of it in the Dockerfile stage. Keep `FIXTURE_DICT=1` on `--words` — the
   image smoke test must not start needing a network.
4. Add the source file to `.gitignore` and confirm `git status` is clean after a fetch.
5. Run `make fetch-dict && make verify-dict && make dict` end to end from a clean `data/`.
6. Run `npm test` in `web/` — `dictionary-source.test.js` should pass untouched. If it
   needs editing, the variable names were changed unnecessarily; put them back.
7. Build the image with and without `FIXTURE_DICT=1` and confirm both produce a database.

## Success Criteria

- [ ] `make fetch-dict` pulls 4,813,111 bytes and the checksum verifies.
- [ ] `make dict` produces `data/noitu.db` with the Phase 1 word count.
- [ ] `web/tests/dictionary-source.test.js` passes with no edits to it.
- [ ] `docker build` succeeds both with `FIXTURE_DICT=1` and without.
- [ ] `git status` is clean after a fetch — no source file staged, ever.
- [ ] No file in the repository still claims a 179 MB dictionary download.

## Risk Assessment

**`raw.githubusercontent.com` is not an archive.** An 8-year-dormant repository could be
deleted or renamed, and then `make fetch-dict` fails for everyone. Signal: a 404 on fetch.
Response: the checksum makes any mirror verifiable, so host a copy as a release asset on
this repository and re-pin to it. Worth doing pre-emptively if the build ever gates CI.

**The Docker stage and the Makefile drift.** Signal: the agreement test fails. Response:
that is the test doing its job — fix the file that is behind, and do not weaken the test to
match.
