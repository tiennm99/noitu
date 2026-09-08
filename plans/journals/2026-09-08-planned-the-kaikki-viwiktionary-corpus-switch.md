---
title: Planned the kaikki viwiktionary corpus switch
date: 2026-09-08
summary: "Plan to replace the 2018 undertheseanlp wiktionary rows with kaikki.org's current Wiktionary tiếng Việt extraction, fetched unpinned; 26,845 -> 34,813 words, data licence to CC BY-SA 4.0"
---

# Planned the kaikki viwiktionary corpus switch

## What happened

- Measured kaikki's `viwiktionary` edition Vietnamese file through the real filter: 34,813 words (25,392 shared with today, 9,421 gained, 1,453 lost), a 96% subset of the raw dump. kaikki's enwiktionary Vietnamese file is 21,244 and deprecated.
- Owner chose the viwiktionary file only, and to fetch kaikki's newest file each build with no checksum pin.
- Plan written: `plans/260908-1653-kaikki-viwiktionary-corpus/` — four phases: kaikki reader with streamed SHA-256 into `meta`, unpinned fetch with loud failure modes, measurement note, relicense to CC BY-SA 4.0.
- Previous plan `260908-1525-dictionary-corpus-switch` marked completed.

## Decision

- Freshness over reproducibility: two builds a week apart may differ; `meta.source_sha256` identifies the bytes. Fallback if bit-for-bit rebuilds are ever needed: keep the fetched file as a release asset.
- The 3.0 licence chosen earlier today only applied to the 2018 snapshot; current Wiktionary text is 4.0, so the database returns to 4.0.

## Next steps

- Validate or cook the plan.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
