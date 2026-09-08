---
title: Dictionary corpus switch to wiktionary-only undertheseanlp
date: 2026-09-08
summary: "Replaced the 179 MB minhqnd aggregate with the wiktionary rows of undertheseanlp/dictionary; corpus 48,216 -> 26,845 words, CC BY-SA 4.0 kept, proper-noun drop abandoned after audit, SQLite path retired"
---

# Dictionary corpus switch to wiktionary-only undertheseanlp

## What happened

- Three research passes today measured every candidate through the real `build-dictionary` filter: the 2026-09-01 viwiktionary dump (36,200 words), kaikki's enwiktionary Vietnamese file (22,628, deprecated and unpinnable), undertheseanlp's merged list (61,271 but GPL via hongocduc) and its wiktionary rows alone (26,845). Reports: `plans/reports/research-260908-1529-viwiktionary-dump-measured.md`.
- Owner chose undertheseanlp wiktionary-only to keep `data/noitu.db` on CC BY-SA 4.0. Plan `plans/260908-1525-dictionary-corpus-switch/` rewritten around it, then executed end to end.
- `build-dictionary` gained `--merged` / `--sources` (default `wiktionary`, names validated), a shared `finish()` tail, `--min-words` default 20,000, and meta rows `source_commit`, `sources_kept`, `sources_excluded`. The SQLite `--in` path, schema auto-detection and their tests were deleted.
- Makefile, Dockerfile, `.gitignore`, CI leak guard, NOTICE, `data/ATTRIBUTION.md`, README, `docs/deployment.md` and the frontend attribution footer now name the new source. `data/LICENSE` untouched.

## Decision

- **Capitalization is not a filter.** The planned proper-noun drop was built and audited: precise on a 100-word sample (1 common word), but with wiktionary-only case evidence it removed 215 words including `mặt trời`, `trái đất`, `tổ quốc`, `dường như`. Narrowing to all-syllables-capitalized was measured (147 casualties remain, 410 junk entries return) and rejected. Owner decided to keep every word regardless of case; the mechanism was removed, not flagged off. Record: `audit-proper-noun-drops.md` in the plan dir.
- Attribution states the 2018 scrape was CC BY-SA 3.0 (Wikimedia moved to 4.0 in 2023) and that the derived database is 4.0 via the later-version clause. Both chain links are credited: Wiktionary tiếng Việt contributors, then undertheseanlp as intermediary.
- Rejected alternatives recorded in `plan.md`: hongocduc+wiktionary (GPLv3 relicense), viwiktionary 2026 dump (documented upgrade path if 27k proves thin), kaikki (unpinnable).

## Verification

- `go vet ./... && go test ./...` green, `-race` on touched packages green; `npm run check && npm test` 175/175; Docker `noitu:ci` built with the CI required-files and leak checks passing, `FIXTURE_DICT=1` variant built.
- Bot real-corpus tests pass on the new DB; easy-vs-easy games 12.9 moves vs 17.5 before, the expected depth loss.
- Code review (DONE_WITH_CONCERNS) fully applied: builder's copy of the pinned commit now guarded by `web/tests/dictionary-source.test.js`; fixture builds record "no upstream data" instead of a CC BY-SA string; dead `contains()`, redundant sorts, scanner line off-by-one, stale "48k" comments, `curl -f`, Dockerfile download name fixed.
- **Not verified:** Playwright e2e — Chromium download timed out twice on this machine. The one changed e2e assertion (footer link) was updated by hand. Needs CI or a successful `npx playwright install chromium`.

## Next steps

- Commit all five phases together so attribution and database never disagree (not yet committed).
- Confirm e2e in CI.
- Watch real play for thinness; the viwiktionary dump is the upgrade path, not GPL data.
- Delete the orphaned local `data/dictionary.db` (179 MB, gitignored).

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
