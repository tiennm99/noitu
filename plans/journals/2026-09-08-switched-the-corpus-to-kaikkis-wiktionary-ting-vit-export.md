---
title: "Switched the corpus to kaikki's Wiktionary tiếng Việt export"
date: 2026-09-08
summary: "Replaced the pinned 2018 undertheseanlp rows with kaikki.org's current wiktextract export fetched unpinned; 26,845 -> 34,813 words; provenance by streamed SHA-256; data licence back to CC BY-SA 4.0"
---

# Switched the corpus to kaikki's Wiktionary tiếng Việt export

## What happened

- `build-dictionary` reads kaikki's JSONL with `--kaikki`, hashing the stream and writing `source_sha256`, `source_rows`, `source_fetched_at` into `meta`; `--merged`/`--sources` and the undertheseanlp reader are gone; `--min-words` floor 30,000; `builder_version` 3.
- Makefile and Dockerfile fetch the rolling kaikki URL with `curl -fL`, no checksum, atomic `.part` rename; `verify-dict` and `DICT_SHA256` removed; the web test guards Makefile == Dockerfile == builder constant and the viwiktionary path.
- Data licence back to CC BY-SA 4.0 (current Wiktionary text); `data/LICENSE` restored from git history; NOTICE/ATTRIBUTION/README/footer credit Wiktionary tiếng Việt contributors and wiktextract/kaikki.org with the LREC citation.
- Measured: 34,813 words (+9,421 / -1,453 vs before), all graph metrics up, bot game length unchanged. Lost words are mostly reduplicatives the vi extractor misses and deleted person-name pages; recorded in `measurement.md`.
- Verified: Go vet/test, web check/test (174), both Docker variants with CI file checks, 404 and truncated-download failure modes. e2e not runnable locally (Chromium download fails), CI covers it.

## Decision

- Owner: viwiktionary edition only; fetch newest, no pin. Two builds a week apart may differ; the SHA-256 in `meta` identifies the bytes. Fallback if reproducibility is ever needed: keep a fetched copy as a release asset.

## Next steps

- Commit (not yet committed); watch CI's e2e job.

> Historical work record — not durable authority. Prefer docs/specs/ADRs for current decisions.
