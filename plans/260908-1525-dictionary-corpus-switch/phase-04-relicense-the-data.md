---
phase: 4
title: "Phase 4: Relicense the data"
status: todo
priority: P1
effort: "2h"
dependencies: [3]
---

# Phase 4: Relicense the data

## Overview

`data/noitu.db` becomes GPLv3, because `hongocduc`'s data is GPL and GPL is copyleft. This
phase rewrites the data half of the licensing documents and regenerates the shipped
database — in one commit, because a GPL-derived database sitting in a tree that still
says CC BY-SA 4.0 is the only ordering mistake here with a legal consequence.

## Requirements

- Functional: `data/LICENSE` carries the GPLv3 text.
- Functional: `NOTICE` section 2 describes GPLv3 for the data, names both source branches
  with their own licenses, and keeps section 1 (Apache-2.0 for code) intact.
- Functional: `data/ATTRIBUTION.md` records the new source, the per-branch licenses, the
  full list of modifications, and why `tudientv` is excluded.
- Functional: the regenerated `data/noitu.db` carries matching `meta` provenance.
- Non-functional: no overclaiming. The `hongocduc` GPL statement comes from a mirror's
  README and the document says so.

## Architecture

The dual-license structure already in `NOTICE` is correct and stays; only its second
section changes:

```
1. SOURCE CODE          — Apache-2.0        (unchanged: server/, tools/, web/, proto/)
2. DICTIONARY DATA      — GPLv3             (was CC BY-SA 4.0)
                          data/noitu.db, data/LICENSE, data/ATTRIBUTION.md
```

Why GPLv3 rather than CC BY-SA 4.0 is worth stating in `ATTRIBUTION.md` rather than left
implicit: `hongocduc` is Hồ Ngọc Đức's FVDP wordlist, distributed under GNU GPL. The
previous source aggregated that same data and redistributed it as CC BY-SA 4.0, which GPL
does not permit. Ours is the stricter, and correct, direction.

The modifications list gains two entries beyond the current five — source selection and
the proper-noun drop — both of which the license requires us to declare as changes.

## Related Code Files

- Modify: `data/LICENSE` — CC BY-SA 4.0 text replaced with GPLv3
- Modify: `NOTICE` — section 2 rewritten
- Modify: `data/ATTRIBUTION.md` — source table, per-branch licenses, modifications 6 and 7,
  the `tudientv` exclusion note
- Modify: `README.md` — any dictionary-source or data-license claim
- Verify: `.github/workflows/ci.yml` — the required-files check (`data/LICENSE`,
  `data/ATTRIBUTION.md`, `NOTICE`, `data/noitu.db`) still holds
- Regenerate: `data/noitu.db`

## Implementation Steps

1. Replace `data/LICENSE` with the GPLv3 text, verbatim and complete.
2. Rewrite `NOTICE` section 2: GPLv3, the affected artifacts, both upstream branches with
   their licenses, and the copyleft obligation carrying into container images — the same
   point the current text makes about share-alike, which is no less true of GPL.
3. Rewrite `data/ATTRIBUTION.md`:
   - source is `undertheseanlp/dictionary` at commit `2c078cf`, file `dictionary/words.txt`
   - `hongocduc` (GNU GPL, per the branch README — a mirror of Hồ Ngọc Đức's 2003
     wordlist, whose canonical site no longer resolves) and `wiktionary` (CC BY-SA, a
     2018-12-10 scrape of vi.wiktionary.org)
   - `tudientv` **excluded**: its README declares copyright *"Chưa rõ"* and names
     Soha/Vietlex (Hoàng Phê) as its primary source. Say it plainly so nobody reopens it.
   - modifications 1–5 carried over, plus **6. source selection** and **7. proper-noun
     removal** with the count from Phase 3
   - the resulting license and why it is GPLv3 and not CC BY-SA 4.0
4. Grep the repository for `minhqnd`, `CC BY-SA`, `179 MB` and fix every survivor outside
   `plans/` — the reports are dated records and stay as written.
5. Regenerate `data/noitu.db` and confirm its `meta` rows agree with `ATTRIBUTION.md`. A
   database whose provenance contradicts the attribution file is worse than neither.
6. Run the CI required-files check locally, or read it and confirm by hand.

## Success Criteria

- [ ] `data/LICENSE` is the complete GPLv3 text.
- [ ] `NOTICE` section 2 says GPLv3 and names both branches with their licenses; section 1
      is unchanged.
- [ ] `data/ATTRIBUTION.md` lists seven modifications, states the `tudientv` exclusion and
      its reason, and does not claim more about the `hongocduc` license than the mirror
      supports.
- [ ] `meta` in the built database matches the attribution file — source URL, commit,
      license, sources kept and excluded, proper-noun drop count.
- [ ] No file outside `plans/` still describes the data as CC BY-SA 4.0 or names minhqnd
      as the source.
- [ ] CI's required-files check passes.

## Risk Assessment

**Relicensing is hard to walk back.** Once a GPLv3 `noitu.db` is published, that release
is GPLv3 permanently. Signal: none — this is simply true. Response: it is why the user
signed off before this plan was written; the mitigation is that it was a decision rather
than a side effect.

**A stale CC BY-SA claim survives somewhere.** A README line or a Docker label saying the
wrong license is a licensing misstatement, not a typo. Signal: the grep in step 4 finds
something after the phase is called done. Response: grep is a step in the phase for that
reason; run it last, not first.

**Section 1 gets damaged while section 2 is rewritten.** The code's Apache-2.0 grant is
not in scope and must come out byte-identical. Signal: a diff touching section 1.
Response: review the `NOTICE` diff before committing.
