---
phase: 2
title: "Phase 2: Fetch the dump"
status: completed
priority: P1
effort: "2h"
dependencies: [1]
---

# Phase 2: Fetch the dump

## Overview

Point the Makefile, the Docker `dict` stage, the pin test, the CI leak guard and the README's
raw commands at Wikimedia's rolling `latest` dump instead of kaikki's JSONL, keeping the
three URL copies in agreement and a failed download unable to pass as a success.

## Requirements

- Functional: `DICT_URL` is
  `https://dumps.wikimedia.org/viwiktionary/latest/viwiktionary-latest-pages-articles.xml.bz2`;
  `DICT_SRC` is `data/viwiktionary-latest-pages-articles.xml.bz2`. `fetch-dict` uses
  `curl -fLR` — `-R` keeps the server's modification time, which is the dump's generation
  time and becomes `source_fetched_at` — to a `.part` name renamed on success, as today.
- Functional: `dict` runs `build-dictionary --dump ../$(DICT_SRC)`.
- Functional: the Dockerfile `dict` stage mirrors the URL and the flag; `FIXTURE_DICT=1` is
  untouched. `curl` gains `-R` there too.
- Functional: `web/tests/dictionary-source.test.js` asserts the Makefile and Dockerfile URLs
  agree, that the URL matches
  `^https://dumps\.wikimedia\.org/viwiktionary/latest/viwiktionary-latest-pages-articles\.xml\.bz2$`,
  that the builder's `dumpSourceURL` constant in `dump.go` is the same string, and that README
  and `data/ATTRIBUTION.md` quote it.
- Functional: the CI leak guard rejects any `\.bz2$` or `\.xml$` inside the image; the
  comment says the dump, not the export. `.gitignore` covers `data/*.bz2`, `data/*.bz2.part`,
  `data/*.xml` (for the phase-1 fallback) and drops the `.jsonl` lines.
- Functional: README's "Without make" block, Make targets table and Setup section describe
  a ~61 MB monthly dump; the `fetch-dict` help line says the same.
- Non-functional: still no resume flag. `latest` can be repointed between two attempts.

## Architecture

```make
DICT_URL := https://dumps.wikimedia.org/viwiktionary/latest/viwiktionary-latest-pages-articles.xml.bz2
DICT_SRC := data/viwiktionary-latest-pages-articles.xml.bz2

fetch-dict:
	@mkdir -p data
	curl -fLR -o $(DICT_SRC).part $(DICT_URL) && mv $(DICT_SRC).part $(DICT_SRC)

dict: $(DICT_SRC)
	cd server && go run ./cmd/build-dictionary --dump ../$(DICT_SRC) --out ../$(DICT_OUT)
```

Correctness rests on: `curl -f` plus the `.part` rename (HTTP errors, interrupted
downloads); the bzip2 decoder (a truncated stream errors at EOF); the XML decoder (a cut
mid-page); the 20,000-page check and the 30,000-word floor (content).

## Related Code Files

- Modify: `Makefile` — header comment, `DICT_URL`, `DICT_SRC`, `help`, `fetch-dict`, `dict`
- Modify: `Dockerfile` — `dict` stage comment, `ARG DICT_URL`, `curl -fsSLR`, file name,
  `--dump`
- Modify: `web/tests/dictionary-source.test.js` — URL pattern, builder file and constant name
- Modify: `.github/workflows/ci.yml` — leak guard pattern and comment; the `image` job's
  comment about "the real upstream release" still holds
- Modify: `.gitignore`
- Modify: `README.md` — Setup, Make targets, Without make, Architecture table's dictionary
  row wording if it names the export

## Implementation Steps

1. Update the Makefile; run `make fetch-dict` on a clean `data/` and check the file's mtime is
   the dump's, not now.
2. Mirror in the Dockerfile.
3. Rewrite the pin test; run `npx vitest run tests/dictionary-source.test.js`. Edit one URL
   copy alone and confirm it fails.
4. Update the CI leak guard, `.gitignore`, README.
5. `make dict`; then `docker build .` and `docker build --build-arg FIXTURE_DICT=1 .`; run
   the CI file checks against the real image and confirm no `.bz2`/`.xml` inside.
6. Point `DICT_URL` at a 404 once and confirm `fetch-dict` fails; feed `dict` a copy of the
   dump cut at 40 MB and confirm the build fails naming the cause.

## Success Criteria

- [x] `make fetch-dict` downloads ~61 MB with the server's mtime; `make dict` builds >30,000
      words with meanings.
- [x] `web/tests/dictionary-source.test.js` passes and fails when any one copy is edited.
- [x] Both image variants build; the real one passes the CI file checks; nothing matching
      `\.bz2$|\.xml$` inside.
- [x] `grep -rniI kaikki --exclude-dir=plans .` finds nothing outside `plans/`.
- [x] A 404 URL and a truncated file each fail the build readably.

## Risk Assessment

**`latest/` mid-repoint.** Wikimedia updates the `latest` symlinks file by file as a run
completes. Fetching during the run can return the previous month's file, which is correct,
or in principle a 404 for a moment. Signal: `curl: (22)`. Response: retry; nothing to fix.

**Dump size grows.** Irrelevant to correctness; the README's "~61 MB" goes stale slowly.
Signal: none. Response: update the number when it is off by a quarter.

**The Docker build now decompresses bzip2 in Go inside alpine.** Same code, same speed as
locally. If phase 1 chose the `bzip2 -dc` fallback, the stage needs `apk add bzip2` and the
pipe; the leak guard's `.xml` clause is why an uncompressed intermediate is also checked.
