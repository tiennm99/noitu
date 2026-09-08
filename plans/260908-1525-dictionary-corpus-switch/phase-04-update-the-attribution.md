---
phase: 4
title: "Phase 4: Update the attribution"
status: done
priority: P1
effort: "1h"
dependencies: [3]
---

# Phase 4: Update the attribution

> **Outcome.** Done 2026-09-08. `data/LICENSE` untouched; `NOTICE` section 2 names the new
> upstream, `data/ATTRIBUTION.md` rewritten (eight modifications, both attribution links,
> both exclusions). Also updated, beyond the plan's list: the frontend attribution footer
> (`AttributionFooter.svelte`, `i18n/vi.js`) and its e2e assertion now credit Wiktionary
> tiếng Việt instead of minhqnd — the user-visible half of the CC BY-SA obligation.

## Overview

The data license does not change — the wiktionary branch is CC BY-SA 4.0, the same license
`noitu.db` already carries — but everything that names the source does. This phase rewrites
the attribution and notice text and regenerates the shipped database in one commit, so the
attribution never describes a database other than the one in the tree.

## Requirements

- Functional: `data/LICENSE` is untouched — still the CC BY-SA 4.0 text.
- Functional: `NOTICE` section 2 names the new upstream, keeps CC BY-SA 4.0, and keeps
  section 1 (Apache-2.0 for code) byte-identical.
- Functional: `data/ATTRIBUTION.md` records the new source, the branch actually used, the
  two branches excluded and why, and the full list of modifications.
- Functional: the regenerated `data/noitu.db` carries matching `meta` provenance.
- Non-functional: no overclaiming. The branch is a 2018 scrape of vi.wiktionary.org made by
  a third party; say that, and attribute Wiktionary's contributors as CC BY-SA requires.

## Architecture

The dual-license structure in `NOTICE` is correct and stays; only the source lines of its
second section change:

```
1. SOURCE CODE          — Apache-2.0        (unchanged: server/, tools/, web/, proto/)
2. DICTIONARY DATA      — CC BY-SA 4.0      (unchanged license, new upstream)
                          data/noitu.db, data/LICENSE, data/ATTRIBUTION.md
```

The attribution chain is now two links instead of an opaque aggregate: vi.wiktionary.org
contributors (CC BY-SA) → `undertheseanlp/dictionary`, branch data `wiktionary`, commit
`2c078cf` → this project. Both links are named.

The modifications list stays at eight: source selection replaces the old language filter
as entry 1, which the license requires us to declare as a change.

## Related Code Files

- Modify: `NOTICE` — section 2's upstream line, affected-artifacts line (`dictionary.db`
  is no longer the source), and the closing paragraph about build artifacts
- Modify: `data/ATTRIBUTION.md` — source table, branch selection, exclusions, modifications
  9 and 10, the reproduce block
- Modify: `README.md` — dictionary-source lines (`~179 MB`, `dictionary.db`, `minhqnd`,
  the manual `curl` + `--in` example)
- Modify: `docs/deployment.md` — the 179 MB builder-stage sentence
- Verify unchanged: `data/LICENSE`, `.github/workflows/ci.yml` required-files check
- Regenerate: `data/noitu.db`

## Implementation Steps

1. Rewrite `NOTICE` section 2's source lines: upstream `undertheseanlp/dictionary` at
   commit `2c078cf`, wiktionary data only; affected artifact `data/noitu.db`. Leave the
   share-alike paragraph as is — it is still true.
2. Rewrite `data/ATTRIBUTION.md`:
   - source is `undertheseanlp/dictionary`, file `dictionary/words.txt`, commit `2c078cf`
   - rows used: those tagged `wiktionary` — a 2018-12-10 scrape of vi.wiktionary.org,
     whose text is CC BY-SA 4.0 / GFDL by its contributors
   - `hongocduc` **excluded**: GNU GPL per the branch README; using it would relicense this
     database to GPLv3
   - `tudientv` **excluded**: its README declares copyright *"Chưa rõ"* and names
     Soha/Vietlex (Hoàng Phê) as its primary source. Say it plainly so nobody reopens it.
   - modifications renumbered: **1. source selection** replaces the language filter (the new
     file is Vietnamese-only); the other seven carry over, with normalization stating that
     capitalization removes nothing — the proper-noun drop was abandoned at the Phase 3 gate
   - reproduce block: `make fetch-dict` is ~4.8 MB now
3. Grep the repository for `minhqnd`, `dictionary.db`, `179` and `--in` and fix every
   survivor outside `plans/` — the reports are dated records and stay as written.
4. Regenerate `data/noitu.db` and confirm its `meta` rows agree with `ATTRIBUTION.md`:
   source URL, commit, license, `sources_kept = wiktionary`,
   `sources_excluded = hongocduc,tudientv`.
5. Run the CI required-files check locally, or read it and confirm by hand.

## Success Criteria

- [ ] `data/LICENSE` has no diff.
- [ ] `NOTICE` section 2 names the new upstream and still says CC BY-SA 4.0; section 1 is
      byte-identical.
- [ ] `data/ATTRIBUTION.md` lists eight modifications, names the branch used and the two
      excluded with reasons, and credits vi.wiktionary.org contributors.
- [ ] `meta` in the built database matches the attribution file.
- [ ] No file outside `plans/` names minhqnd, `dictionary.db` as a source, or a 179 MB
      download.
- [ ] CI's required-files check passes.

## Risk Assessment

**A stale source claim survives somewhere.** A README line or Docker comment naming the old
upstream is an attribution misstatement, not a typo. Signal: the grep in step 3 finds
something after the phase is called done. Response: grep is a step in the phase for that
reason; run it last, not first.

**Section 1 gets damaged while section 2 is edited.** The code's Apache-2.0 grant is not
in scope and must come out byte-identical. Signal: a diff touching section 1. Response:
review the `NOTICE` diff before committing.

**The attribution credits the wrong party.** CC BY-SA attribution belongs to Wiktionary's
contributors; undertheseanlp is the intermediary that scraped and redistributed. Signal:
an `ATTRIBUTION.md` that names only the GitHub repository. Response: step 2 names both
links of the chain explicitly.
