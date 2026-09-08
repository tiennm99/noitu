---
phase: 4
title: "Phase 4: Relicense to 4.0"
status: completed
priority: P1
effort: "1h"
dependencies: [3]
---

# Phase 4: Relicense to 4.0

## Overview

Current Wiktionary text is CC BY-SA 4.0, so the derived database is too. Restore the 4.0
legal code, rewrite the attribution chain for a source with no commit to cite, and update
every place that says 3.0 — in the same commit as the first kaikki-built database.

## Requirements

- Functional: `data/LICENSE` is the CC BY-SA 4.0 legal code, byte-identical to the file this
  repository shipped before commit `39ef457` (`git show e19b083:data/LICENSE`).
- Functional: `NOTICE` section 2 says 4.0 and names Wiktionary tiếng Việt as the original
  work and kaikki.org/wiktextract as the extraction; section 1 byte-identical.
- Functional: `data/ATTRIBUTION.md` records the source URL, that the file is refreshed
  weekly and unpinned, that the exact bytes of any build are identified by
  `meta.source_sha256`, the three attribution links, and the modifications list.
- Functional: the in-game footer links to the 4.0 deed and says 4.0; the e2e assertion
  matches.
- Non-functional: no overclaiming about dates — the attribution names the dump date only as
  "the Wikimedia dump kaikki extracted at fetch time; see `source_fetched_at`", not a fixed
  date that goes stale.

## Architecture

Attribution chain, all three named:

```
Wiktionary tiếng Việt contributors      authors, CC BY-SA 4.0
  → wiktextract / kaikki.org (Tatu Ylonen)   extraction; cite LREC 2022 as kaikki requests
    → this project                            filter + index, data/noitu.db, CC BY-SA 4.0
```

Modifications list (renumbered): 1 language selection (`lang_code = vi`), 2 length filter,
3 content filter, 4 normalization, 5 spelling aliases, 6 added columns and tables,
7 deduplication, 8 dropped fields (senses, translations, POS, categories — everything but
the word form).

## Related Code Files

- Modify: `data/LICENSE` — restore 4.0 text from git history
- Modify: `NOTICE` — section 2 heading, source lines, share-alike sentence
- Modify: `data/ATTRIBUTION.md` — full rewrite of Source, Modifications 1 and 8, reproduce
  block (no checksum step)
- Modify: `README.md` — licence table row, dictionary-source paragraph
- Modify: `Dockerfile`, `docs/deployment.md`, `.github/workflows/ci.yml` — 3.0 → 4.0 in comments
- Modify: `server/cmd/build-dictionary/main.go` doc comment, `server/cmd/noitu-server/main.go`
  and `server/internal/dictionary/store.go` comments, `store_test.go` seed strings
- Modify: `web/src/lib/components/AttributionFooter.svelte` (deed URL, comment),
  `web/src/lib/i18n/vi.js` (`attributionLicense`), `web/e2e/bot-game.spec.js` (regex)
- Verify unchanged: `.github/workflows/ci.yml` required-files check

## Implementation Steps

1. `git show e19b083:data/LICENSE > data/LICENSE`; confirm the first lines read
   "Attribution-ShareAlike 4.0 International".
2. Rewrite `NOTICE` section 2 source lines; leave the share-alike paragraph's meaning, change
   the version.
3. Rewrite `data/ATTRIBUTION.md` as described. State plainly: "The file is not pinned. Each
   build fetches kaikki's current export; the SHA-256 and row count of the file a given
   database was built from are recorded in its `meta` table."
4. `grep -rn "3\.0\|by-sa/3" --exclude-dir=plans .` and fix every hit; then the same for
   `undertheseanlp`, `DICT_SHA256`, `--merged`, `--sources`.
5. Regenerate `data/noitu.db`; confirm `meta.source_license` says 4.0 and matches the files.
6. Run the full verification set from the previous plan: Go vet/test, web check/test, both
   image builds with the CI file checks.

## Success Criteria

- [x] `data/LICENSE` is the 4.0 text; `git diff e19b083 -- data/LICENSE` is empty.
- [x] No file outside `plans/` says CC BY-SA 3.0 or names undertheseanlp.
- [x] `NOTICE` section 1 has no diff.
- [x] `ATTRIBUTION.md` names all three links and states the unpinned-fetch policy.
- [x] `meta.source_license` in the built database says 4.0.
- [x] All verification suites green; the commit contains both the licence files and the
      builder change so no intermediate tree mixes 3.0 text with 4.0 data.

## Risk Assessment

**A stale 3.0 or undertheseanlp claim survives.** Signal: the grep in step 4 finds one after
the phase is called done. Response: grep is a step for that reason; run it last.

**The footer wording.** `giấy phép CC BY-SA 3.0` → `4.0` is one string, but the e2e regex
and the deed URL must change with it. Signal: e2e footer test fails in CI. Response: all
three edits are listed above; do them together.
