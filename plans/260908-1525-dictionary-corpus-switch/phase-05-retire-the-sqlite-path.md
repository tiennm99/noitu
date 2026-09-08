---
phase: 5
title: "Phase 5: Retire the SQLite path"
status: todo
priority: P2
effort: "1h"
dependencies: [4]
---

# Phase 5: Retire the SQLite path

## Overview

Delete the input mode nothing uses any more: reading a source SQLite database, with the
table and column auto-detection that existed because the upstream schema was not ours to
rely on. Two input modes for one source is dead weight, and dead weight in a build tool is
where stale assumptions hide.

## Requirements

- Functional: `--in`, `--table`, `--word-col`, `--lang-col` and `--lang` are gone, along
  with the schema resolution behind them.
- Functional: `--merged` (the corpus) and `--words` (the fixture) remain. The fixture path
  stays because the e2e suite and the Docker smoke build depend on it.
- Non-functional: the package doc comment describes what the tool now does, not what it
  used to.
- Non-functional: `modernc.org/sqlite` stays a dependency — it writes the output database.

## Architecture

What `run()` dispatches on collapses from three modes to two:

```
--merged <file>   the corpus:  undertheseanlp JSONL   (default path)
--words  <file>   the fixture: one word per line      (e2e, Docker smoke build)
```

`resolveSource` and its auto-detection tests go with `--in`. They were never about the
game's rules — they were insurance against an upstream schema we did not control, and we
no longer read that upstream.

## Related Code Files

- Modify: `server/cmd/build-dictionary/main.go` — remove `--in` and the four schema flags,
  `resolveSource`, the SQLite read path, and the `config` fields behind them; rewrite the
  package doc comment and the usage example
- Modify: `server/cmd/build-dictionary/main_test.go` — remove the source-resolution tests;
  keep and extend the ones about output shape
- Modify: `Makefile` — drop any remaining `--in` reference and 179 MB wording from `help`
- Verify: `Dockerfile` no longer mentions `--in`

## Implementation Steps

1. Remove the flags, the `config` fields, `resolveSource` and the SQLite read path.
2. Delete the tests that exist only to cover auto-detection. Do not delete tests covering
   output shape, `verify()`, or the reject-reason counts — those still describe behavior.
3. Rewrite the package doc comment: the input is a 4.8 MB JSONL wordlist with source
   membership and capitalization; the output is the game's syllable-indexed database.
4. `go build ./... && go vet ./... && go test ./...` from `server/`.
5. Grep for `--in`, `dictionary.db`, `resolveSource` and `lang_code` outside `plans/`;
   nothing should survive except in the dated reports.
6. `make dict` once more from a clean `data/` to prove the surviving path is the one that
   works.

## Success Criteria

- [ ] `go build`, `go vet` and `go test ./...` are clean from `server/`.
- [ ] `build-dictionary --help` lists `--merged` and `--words` and nothing about tables,
      columns or language codes.
- [ ] `make dict` and `make fixture-dict` both work.
- [ ] The e2e suite still builds its fixture and passes.
- [ ] No reference to `--in`, `resolveSource` or `lang_code` survives outside `plans/`.

## Risk Assessment

**Deleting the wrong tests.** The auto-detection tests and the output-shape tests live in
the same file. Signal: coverage of `verify()` or the reject counters disappears with the
deletion. Response: read `main_test.go` in full before cutting, and delete by test
function rather than by line range.

**Somebody wants the SQLite path back.** A future source could ship SQLite again. Signal:
a future plan proposing it. Response: git remembers, and re-adding a reader for a schema
we would then actually control is a smaller job than maintaining auto-detection for a
schema nobody reads. Deleting it is the cheaper bet.

**The fixture path is quietly coupled to something removed.** Signal: `make fixture-dict`
or the e2e build fails after the cut. Response: step 6 builds both paths; run it before
calling the phase done, not after.
