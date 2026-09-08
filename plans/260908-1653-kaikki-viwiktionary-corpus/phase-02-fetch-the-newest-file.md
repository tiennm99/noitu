---
phase: 2
title: "Phase 2: Fetch the newest file"
status: completed
priority: P1
effort: "2h"
dependencies: [1]
---

# Phase 2: Fetch the newest file

## Overview

Point the Makefile and the Docker dictionary stage at kaikki's rolling URL with no checksum,
keep the two build files in agreement, and make sure a failed download cannot pass as a
success anywhere.

## Requirements

- Functional: `make fetch-dict` downloads the kaikki file to `data/kaikki-viwiktionary-vi.jsonl`
  with `curl -fL` and fails on any HTTP error; `make dict` builds from it with `--kaikki`.
- Functional: `verify-dict` and `DICT_SHA256` are removed from the Makefile and Dockerfile;
  nothing in the repository claims a checksum for this file.
- Functional: the Docker `dict` stage downloads the same URL and runs the same command;
  `FIXTURE_DICT=1` keeps working untouched.
- Functional: `web/tests/dictionary-source.test.js` asserts the Makefile and Dockerfile URLs
  agree, that the URL is the kaikki viwiktionary `Tiếng Việt` file, and that the builder's
  `kaikkiSourceURL` constant is the same URL. The three checksum assertions go.
- Functional: the CI leak guard greps for `kaikki.*\.jsonl$`; `.gitignore` ignores the new
  file name.
- Non-functional: no resume flag (`-C -`) — a rolling file can change between attempts, and a
  resumed download would splice two versions together.

## Architecture

```make
DICT_URL := https://kaikki.org/viwiktionary/Ti%E1%BA%BFng%20Vi%E1%BB%87t/kaikki.org-dictionary-Ti%E1%BA%BFngVi%E1%BB%87t.jsonl
DICT_SRC := data/kaikki-viwiktionary-vi.jsonl
DICT_OUT := data/noitu.db

fetch-dict:
	@mkdir -p data
	curl -fL -o $(DICT_SRC).part $(DICT_URL) && mv $(DICT_SRC).part $(DICT_SRC)

dict: $(DICT_SRC)
	cd server && go run ./cmd/build-dictionary --kaikki ../$(DICT_SRC) --out ../$(DICT_OUT)
```

The Dockerfile mirrors it with `ARG DICT_URL` only. The URL must stay percent-encoded in
both files: Make and `sh` would otherwise split on the space in `Tiếng Việt`.

Correctness now rests on three checks instead of a checksum: `curl -f` plus an atomic
`.part` rename (HTTP errors and interrupted downloads), the JSON decoder (shape, and
truncation that lands mid-line), the 30,000 floor (content, including a truncation that
lands on a line boundary).

## Related Code Files

- Modify: `Makefile` — variables, `help` text, `fetch-dict`, remove `verify-dict` and its
  `.PHONY` entry, `dict` flags
- Modify: `Dockerfile` — `ARG DICT_URL`, remove `ARG DICT_SHA256` and the `sha256sum` line,
  download file name, `--kaikki`; stage comment (no longer "pinned … checked by digest")
- Modify: `web/tests/dictionary-source.test.js` — drop checksum tests, keep URL agreement,
  add the kaikki-path and builder-constant assertions
- Modify: `.github/workflows/ci.yml` — leak-guard pattern and comment
- Modify: `.gitignore` — `data/kaikki-viwiktionary-vi.jsonl` replaces the undertheseanlp entry
- Modify: `README.md` — Make targets table (`verify-dict` row gone), the manual `curl` and
  `build-dictionary` lines

## Implementation Steps

1. Update the Makefile variables and targets; delete `verify-dict`.
2. Mirror in the Dockerfile; rewrite the stage comment to say the file is fetched fresh and
   identified by the SHA-256 the builder records.
3. Rewrite the pin test as described. Run `npx vitest run tests/dictionary-source.test.js`.
4. Update the CI leak guard, `.gitignore` and the README lines.
5. Run `make fetch-dict && make dict` (or the raw commands on Windows) from a clean `data/`.
6. Build the image with and without `FIXTURE_DICT=1`; run the CI file checks against the
   real one and confirm no `*.jsonl` is inside.
7. Simulate a bad download once: point `DICT_URL` at a 404 and confirm `curl -f` fails the
   target; feed the builder a truncated copy of the file and confirm the build fails.

## Success Criteria

- [x] `make fetch-dict` fetches ~62 MB and `make dict` builds >30,000 words.
- [x] `grep -rn DICT_SHA256 --exclude-dir=plans .` finds nothing; `make verify-dict` is not a
      target.
- [x] `web/tests/dictionary-source.test.js` passes and fails if either URL is edited alone.
- [x] Both image variants build; the real one passes the CI file checks; no `.jsonl` inside.
- [x] A 404 URL and a truncated file each fail the build with a readable message.

## Risk Assessment

**kaikki.org is a single volunteer-run host.** An outage breaks `make fetch-dict` and the
release image build until it returns. Signal: `curl: (22)` on fetch. Response: wait, or
build from a previously fetched local copy (`make dict` needs only the file); if outages
recur, keep a copy as a release asset and point `DICT_URL` at it — the open question in
`plan.md`.

**Two builds ship different words.** By design. Signal: none needed. Response: `meta`
identifies the bytes; the audit note in phase 3 records what this first build contained.

**The space in the URL.** Signal: `curl` fetching `https://kaikki.org/viwiktionary/Ti%E1%BA%BFng`
and failing. Response: the URL stays percent-encoded in every file, and the pin test checks
that the string contains `Ti%E1%BA%BFng%20Vi%E1%BB%87t`.
