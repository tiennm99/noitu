---
title: "Research: building a dictionary DB from the dumps.wikimedia.org viwiktionary dump — measured"
date: 2026-09-08T15:29+07:00
type: research
status: complete
scope: research-only — dump downloaded to scratchpad, parsed, run through the real build-dictionary filter; no repo file touched
follows: research-260908-1434-viwiktionary-as-dictionary-source.md, plans/260908-1525-dictionary-corpus-switch/plan.md
---

# Can we build our own Vietnamese dictionary DB from the viwiktionary dump?

## Executive summary

**Yes, and it is cheap: 63.5 MB download, 34 s of stdlib Python, no wiktextract, no Lua.** The
2026-09-01 `pages-articles` dump was downloaded, checksum-verified against Wikimedia's published
MD5, parsed, and every list below was run through our real `build-dictionary --words` filter.
Numbers are measured, not estimated.

**But viwiktionary alone is not a corpus.** It yields **36,200** accepted words, **31,637** after
dropping pages labelled proper-noun. Both sit below the shipped 48,216 and below the 40,000
`--min-words` floor. The 14:34 report's "~16k from this source" undercounted by 2×, because the
`minhqnd` aggregate only ingested a 17k-row slice; the full dump is 36k. Still not enough.

**Where it earns its place is on top of the pending plan.** Against the plan's undertheseanlp
corpus (re-measured this session at **61,026**, matching the plan's ~61,276), the 2026 dump adds
**3,084 words** the plan would otherwise lose — `học liệu`, `thiên hà lùn`, `cải thảo`, `bùn đỏ`,
`báng súng`, `tăng đoàn` — for a union of **64,110**. That is the modern-vocabulary gap the plan's
own open question names, and the plan's 2018 wiktionary branch (26,845 accepted) is 9,780 words
behind the 2026 dump. **Recommendation: after the corpus switch lands, add the dated dump as a
second `--words` input, pinned by dated URL + checksum.** Licensing stays clean: CC BY-SA 4.0 is
one-way compatible into the GPLv3 the plan already adopts.

**Do not use viwiktionary's proper-noun labels as a drop rule.** They agree with the
capitalization blocklist 86% of the time, but they tag `mặt trời`, `trái đất`, `thứ hai`,
`tháng ba`, `tin lành`, `bàn là` as proper-only and `việt nam` as common. As a *filter* the label
is worse than capitalization; as a *report column* it is fine.

---

## Method

- Web: 2 fetches (dump index, kaikki index), plus direct `curl` of the dump, its checksum file,
  4 sample raw wikitext pages, and the plan's pinned undertheseanlp file.
- Local: `extract.py` (streams bz2 → per-page markers, 34 s), `classify.py` (slices the
  Vietnamese section, classifies POS labels, emits word lists), `build-dictionary --words` for
  every list, sqlite for overlaps. Scripts are in the session scratchpad; each is < 100 lines.

## The dump

| | |
|---|---|
| File | `https://dumps.wikimedia.org/viwiktionary/20260901/viwiktionary-20260901-pages-articles.xml.bz2` |
| Size | 63,513,513 bytes |
| MD5 (Wikimedia-published, verified) | `6c2491e703e7d946f23a405996b4d172` |
| SHA-256 (computed here; Wikimedia publishes md5/sha1 only for this run) | `ed66c932f535b0b362d1141c02273b02ed01c4101e856378bd15e88d277bb8d9` |
| Cadence | monthly, dated dirs `20251220 … 20260901`, plus rolling `latest/` |
| Pages / ns0 pages | 391,543 / 349,461 |
| **ns0 pages with a Vietnamese section** | **43,013** (on-wiki category says 43,037 — parser is complete) |
| Redirect pages among them | 3,237; 834 are case-only (`mặt trời` → `Mặt Trời`) |

Dated directories are immutable, so a pin is honest. `latest/` is not. kaikki.org tracks the same
dump (extracted 2026-09-06, 56,502 Vietnamese *senses*) but publishes rolling files — the earlier
report's pinning concern stands; the dump removes the need for kaikki entirely.

### Two wikitext dialects, both live

| Format | Pages | Language marker | POS marker |
|---|---|---|---|
| legacy | 35,885 | `{{-vie-}}` | `{{-noun-}}`, `{{-verb-}}`, `{{-pr-noun-}}`, `{{-place-}}` … |
| new | 7,128 | `== {{langname\|vi}} ==` | `{{ĐM\|noun}}`, `{{ĐM\|pr-noun}}`, `{{vi-noun}}`, `{{vi-pr-noun}}` … |

A parser must handle both; the wiki is mid-migration, so the mix shifts every month. Legacy
section codes collide with language codes (`{{-adj-}}` vs `{{-eng-}}`), which is why the
extractor keeps an explicit section-code set and treats every other 2–3-letter code as a language
switch. Languages seen switching out of Vietnamese: `tyz`, `mtq`, `eng`, `nut`, `nuo`, `fra` …

## Classification of the 43,013 pages

| Class | Pages | Meaning |
|---|---|---|
| common | 31,950 | ≥1 common POS label (noun/verb/adj/adv/phrase/idiom/…) |
| proper-only | 5,103 | only `pr-noun` / `place` / `vi-pr-noun` labels |
| no-pos | 5,764 | Vietnamese section, no POS marker at all |
| nôm-only | 169 | only Nôm/Hán character sections |
| proper+common | 27 | |

The **no-pos** class is real vocabulary, not junk: 5,538 of them pass our filter and **4,912 are
already shipped** (`kỵ mã`, `khoái hoạt`, `quá tay`, `nặn óc`). Keep them.

## Through `build-dictionary --words`

| List | Accepted | Rejected <2 syll | punct | non-VN |
|---|---|---|---|---|
| all 43,013 titles | **36,200** | 6,361 | 155 | 162 |
| minus proper-only | **31,637** | 6,051 | 121 | 84 |
| proper-only alone | 4,661 | 310 | 34 | 78 |
| undertheseanlp, plan rule (hongocduc+wiktionary, no tudientv, drop caps-only) | **61,026** | | | |
| undertheseanlp `wiktionary` branch alone (2018 snapshot) | 26,845 | | | |

Playability of a viwiktionary-only DB vs shipped:

| metric | shipped | vi-all | vi-no-proper |
|---|---|---|---|
| words | 48,216 | 36,200 | 31,637 |
| syllables | 6,676 | 6,172 | 5,981 |
| syllables that open a word | 5,049 | 4,600 | 4,515 |
| …with ≥2 continuations | 3,682 | 3,246 | 3,168 |
| dead-end syllables | 1,627 | 1,572 | 1,466 |

Every breadth metric regresses. **Option A (replace) is dead on measurement, not opinion.**

## Overlaps — what the dump is actually good for

| | |
|---|---|
| shipped ∩ viwiktionary | **34,570 of 48,216 (72%)** — viwiktionary confirms most of what we ship |
| shipped, not in viwiktionary | 13,646 (9,713 two-syllable) — `thai sản`, `vách núi`, `điện từ trường`, `giả kim`: real words, so absence ≠ junk |
| viwiktionary-only, not shipped | 1,630 |
| shipped words viwiktionary labels proper-only | 4,523 (`ba đồn`, `vị thanh`, `xuân lập` …) |
| **plan corpus (61,026) + vi-no-proper → union** | **64,110 (+3,084)** |
| plan corpus words viwiktionary labels proper-only | 293 — includes `thứ hai`, `tháng năm`, `tin lành`, `tân ước`, `bàn là` |
| 2018 wiktionary branch vs 2026 dump | 26,420 shared; **9,780 new since 2018**; 425 gone |
| shipped words in neither plan corpus nor viwiktionary | 9,267 |

### Proper-noun label vs capitalization blocklist

- viwiktionary proper-only ∩ undertheseanlp caps-only: **4,015 of 4,661 (86%)** agree.
- Caps-only words viwiktionary calls common: 139 (incl. `việt nam`).
- Common words viwiktionary calls proper-only: `mặt trời`, `trái đất`, `sao thủy` (celestial
  bodies are `Mặt Trời` on-wiki), weekday/month names, `tin lành`, `bàn là`.

Verdict: capitalization (the plan's rule) is the better *drop* signal. The label is a useful
*second opinion* for the plan's Phase 3 audit sample, nothing more.

## Licensing

Wiktionary text is CC BY-SA 4.0 / GFDL. Creative Commons declares CC BY-SA 4.0 **one-way
compatible with GPLv3**, so folding viwiktionary into the GPLv3 `noitu.db` the plan produces is
clean. `data/ATTRIBUTION.md` gains one entry: edition, dump date, URL, checksum. No `NOTICE`
restructure.

## Recommendation

1. **Do not switch to viwiktionary.** 36,200 < 40,000 floor; every playability metric regresses.
2. **Land the undertheseanlp plan as written.** Its numbers reproduce (61,026 vs ~61,276).
3. **Then add the dated dump as a second `--words` source** — a follow-up phase, not a change to
   the current plan. Concretely: `make fetch-dict` also fetches the pinned dump; a ~100-line
   extractor (stdlib, both dialects, redirects skipped, `--drop-proper` off by default) emits a
   word list; the builder reads two `--words` files or the Makefile concatenates them.
   Expected result: **~64,100 words**, +3,084 modern/compound vocabulary, 12-month freshness via a
   monthly-dated pin.
4. **Do not drop on viwiktionary's proper-noun label.** Record it in `meta`/build log as a
   count; use it to spot-check the capitalization rule in Phase 3.
5. Keep kaikki out. The dump is smaller than kaikki's JSONL, pins honestly, and the parser is
   trivial because we only need titles + section labels, not senses.

## Addendum — kaikki.org `dictionary/Vietnamese/kaikki.org-dictionary-Vietnamese.jsonl`

Asked after the main report. This file is **not viwiktionary**: kaikki's `dictionary/` tree is
the *English* Wiktionary, filtered to `lang_code = vi`. Dump 2026-09-02, extracted 2026-09-06,
79,161,423 bytes, SHA-256 `d878bd23fe4d6ac4480736858a85b2bbca0376d6cc9b30395fff0ba1e16cf97b`
at fetch time. Measured the same way as everything above.

| | |
|---|---|
| rows / distinct words | 51,896 / 45,281 |
| POS | noun 19,206 · character 9,029 · verb 8,992 · adj 7,070 · **name 3,814** · adv 1,160 · … |
| rejected `< 2 syllables` | 22,115 — half the file is single syllables and Hán/Nôm characters |
| **accepted, all** | **22,628** |
| **accepted, `name`-only words dropped** | **21,244** |

| metric | shipped | kaikki-en | vi-no-proper | uts-plan |
|---|---|---|---|---|
| words | 48,216 | 21,244 | 31,637 | 61,026 |
| syllables | 6,676 | 5,145 | 5,981 | 7,005 |
| openers ≥2 | 3,682 | 2,431 | 3,168 | 4,164 |
| dead-end | 1,627 | 1,399 | 1,466 | 1,513 |

Smallest of every candidate as a corpus. **As a third supplement it is the best one:**

| overlap | words |
|---|---|
| kaikki-en ∩ shipped | 20,231 (adds 1,013) |
| kaikki-en adds over plan corpus | 4,394 |
| kaikki-en adds over plan corpus ∪ viwiktionary | **3,693** |
| **union plan ∪ viwiktionary ∪ kaikki-en** | **67,803** |
| shipped words in none of the three | 6,187 |

The additions are exactly the modern register the other two lack: `sổ hồng`, `quay xe`, `thi hành
án`, `nhà máy lọc dầu`, `cát tặc`, `giấy ướt`, `máy tính tiền`, `thập lục phân`, `đường tiêu hóa`,
`tân tổng thống`, `sói đồng cỏ`. English Wiktionary's Vietnamese section is more actively curated
than viwiktionary's.

**Its `name` POS is the cleanest proper-noun label seen so far.** `hà nội`, `việt nam`, `trái đất`
are `name`; `thứ hai`, `tin lành` are not; `mặt trời` is both `name` and `noun` and so survives.
Of 1,483 accepted name-only words, 1,359 are shipped today and 247 survive the plan's caps rule
(`thượng đế`, `bắc cực`, `trung thu`, `siêu nhân` — arguable either way). Good audit column; still
not a sole drop rule.

**Two blockers to using the URL as given:**

1. **It is deprecated.** kaikki's index marks the postprocessed per-language file as *"deprecated
   and will be removed in the future"* and points to the raw download page, where the only
   non-deprecated form of this data is the full-edition `raw-wiktextract-data.jsonl`
   (23.1 GB, 2.7 GB compressed) filtered by `lang_code`. There is no per-language file in the
   replacement layout — the `downloads/<code>/` entries are per *edition*, not per language.
2. **It cannot be pinned.** Rolling URL, refreshed weekly, no archived snapshots. A
   `DICT_SHA256`-style pin breaks on the next refresh; `make fetch-dict` would fail weekly.

Ways to use it anyway, in order of preference:

- **Vendor the derived word list.** Run the reduction once, commit `data/sources/kaikki-en-vi.txt`
  (~21k lines, ~400 KB) with the fetch date, URL and file SHA-256 in `ATTRIBUTION.md`. Pins
  honestly, costs nothing at build time, refreshed deliberately. Note this is a *derived list*,
  not a copy of their file, so the plan's "never commit the source file" rule is not violated in
  spirit — decide explicitly.
- **Pin the enwiktionary dump and extract ourselves.** `enwiktionary-20260902-pages-articles`
  is >1 GB and wiktextract with Lua expansion takes hours. Correct but disproportionate for
  ~3.7k words.
- **Re-pin on every refresh.** Not acceptable; turns a data pin into a weekly chore.

License: same CC BY-SA 4.0 / GFDL as all Wiktionary text, so the GPLv3 result stays clean.
Attribution should name enwiktionary + wiktextract/kaikki (the extraction is Tatu Ylonen's work).

**Verdict:** yes, usable, and worth ~3,700 modern words on top of plan + viwiktionary — but only
via a vendored derived list, never as a live fetch of that URL.

## Addendum — undertheseanlp `wiktionary` branch alone (the option chosen for the plan)

Measured after the user chose to use undertheseanlp with its wiktionary data only. Rows with
`wiktionary` in `source`: 32,484 → 32,374 lowercase forms → 4,883 caps-only dropped (case
evidence from wiktionary rows only) → 27,491 → **22,310 accepted**.

| metric | shipped | uts-wik only | vi-dump 2026 | wik ∪ dump | uts-plan (GPL) |
|---|---|---|---|---|---|
| words | 48,216 | **22,310** | 31,637 | 31,817 | 61,026 |
| syllables | 6,676 | 5,484 | 5,981 | 6,012 | 7,005 |
| openers | 5,049 | 4,050 | 4,515 | 4,539 | 5,492 |
| openers ≥2 | 3,682 | 2,686 | 3,168 | 3,180 | 4,164 |
| dead-end | 1,627 | 1,434 | 1,466 | 1,473 | 1,513 |

- shipped ∩ uts-wik 21,230 · shipped lost 26,986 (4,335 proper-noun drops, 22,651 absent
  from the branch) · new 1,080.
- The 2018 branch is a near-subset of the 2026 dump: union adds 180 words. Same license.
- **Case-rule casualties:** 271 caps-only drops have a lowercase form in another branch
  (247 in hongocduc): `mặt trời`, `trái đất`, `hệ mặt trời`, `phật giáo`, `nguyên đán`,
  `tia x`. Wiktionary holds `Mặt Trời` / `Trái Đất` capitalized only.
- Probes: all 11 known-bad absent ✓. Of 10 ordinary probes, `mặt trời` dropped by the case
  rule; `con người`, `cánh diều` not in the branch at all; the other 7 present.
- Decision recorded in `plans/260908-1525-dictionary-corpus-switch/plan.md`: wiktionary-only,
  CC BY-SA 4.0 unchanged, floor 20,000, viwiktionary 2026 dump as the upgrade path.

## Sources

- https://dumps.wikimedia.org/viwiktionary/ (index; dated dirs 20251220–20260901)
- https://dumps.wikimedia.org/viwiktionary/20260901/ (file list, md5/sha1 sums)
- https://kaikki.org/viwiktionary/ (dump 2026-09-01, extracted 2026-09-06, 56,502 vi senses)
- https://kaikki.org/dictionary/Vietnamese/ and https://kaikki.org/dictionary/rawdata.html (enwiktionary edition; deprecation notice, no snapshots)
- raw wikitext of `Hà Nội`, `học sinh`, `nhà`, `mặt trời` on vi.wiktionary.org
- https://raw.githubusercontent.com/undertheseanlp/dictionary/2c078cfc…/dictionary/words.txt (plan's pin, sha256 verified `4c3e0e61…`)
- Local: `server/cmd/build-dictionary`, `data/noitu.db`

## Unresolved questions

- The 9,267 shipped words in neither the plan corpus nor viwiktionary are still an unquantified
  junk/real mix; this dump does not settle them.
- Whether Wikimedia publishes a sha256 file for this run later (only md5/sha1 exist today).
  Pin on our computed SHA-256 or on the published MD5 — decide when the phase is written.
- Legacy → new wikitext migration pace: if the wiki finishes it, the legacy branch of the parser
  becomes dead code; harmless, but the pin test should assert the extracted count so a dialect
  the parser misses shows up as a drop.
