---
title: "Research: A higher-standard Vietnamese dictionary for noitu"
date: 2026-09-07T16:50+07:00
type: research
status: complete
scope: research-only — no code touched
---

# Research: A higher-standard Vietnamese dictionary for noitu

## Executive Summary

**Hoàng Phê is not obtainable as data.** The only digital copy in circulation is a
scanned 2003 edition on Internet Archive released **CC BY-NC-ND 4.0** — non-commercial,
**no derivatives**. That license forbids exactly what we would need (extract headwords,
derive a DB, ship it). The live commercial product (`vtudien.com`, `tratu.soha.vn`, the
Vietlex apps) is proprietary Vietlex / Trung tâm Từ điển học data with no public dump and
no open license. There is no legitimate Hoàng Phê database. Stop looking for one.

**The real problem is not coverage — it is false positives.** Our current corpus
(`data/noitu.db`, 48,216 words / 6,676 syllables / 2,041 aliases, from
`minhqnd/dictionary` v2.0.0, CC BY-SA 4.0) already has plenty of words. A random sample
of 60 entries surfaces proper nouns (`trương công định`, `trần danh án`, `tân an thạnh`,
`cẩm xá`, `vĩnh điện`, `cam lâm`, `quỳnh bảo`), free phrases that are not lexical units
(`nước kết tinh`, `sắt rèn nghệ thuật`, `máy kéo tạ`, `chớ có trách`), and doubtful
fragments (`kháng đón`, `gấm ngày`, `cương liệu`). Roughly 10–20% of the sample is junk.
That is what an aggregated-from-Wiktionary source looks like. Swapping one aggregate for
another aggregate does not fix it.

**Recommendation: keep `minhqnd` as the breadth layer, add Hồ Ngọc Đức's tiered
wordlists as a quality/authority signal, and derive a confidence tier per word.** Hồ Ngọc
Đức's FVDP ships four hand-curated, frequency-tiered lists — `Viet11K`, `Viet22K`,
`Viet39K`, `Viet74K` — mirrored at `undertheseanlp/dictionary` branch `hongocduc`. That
tiering is precisely the axis a word-chain game needs: it gives bot difficulty, it
separates real words from Wiktionary lint, and it gives a defensible answer when a player
disputes a rejection.

---

## Methodology

- 5 web searches (budget cap), 7 targeted page fetches.
- Local evidence: `data/noitu.db` counted and sampled directly via `sqlite3`.
- Date range of materials: 1988 (Hoàng Phê 1st ed.) → 2026-08 (kaikki extraction).
- Key terms: `từ điển Hoàng Phê dataset`, `Vietlex bản quyền`, `Ho Ngoc Duc FVDP`,
  `undertheseanlp dictionary`, `kaikki wiktionary vi`, `nối từ wordlist github`.

---

## Finding 1 — Hoàng Phê: unavailable, and unusable if obtained

| Fact | Evidence |
|---|---|
| ~36,000 headwords explained; 39,924 total entries (Viện Ngôn ngữ học edition) | press/reference coverage |
| Two divergent lines: **Hoàng Phê – Viện Ngôn ngữ học** (1988 / 1994 / 2000, Đà Nẵng) and **Hoàng Phê – Vietlex** (2007→, continuously revised) | vhds.baothanhhoa.vn comparison series |
| Only digital copy: 2003 scan on Internet Archive — PDF / EPUB / JP2 / OCR text | archive.org item |
| Rights on that scan: **CC BY-NC-ND 4.0** | archive.org item |
| Vietlex sells the electronic edition; no dump, no data license | vietlex.vn |

**Legal read (not legal advice).** Vietnam's IP Law protects compilations: the *selection
and arrangement* of a dictionary's headword list is protected even where individual words
are not. Hoàng Phê 2007+ is well inside term. Extracting the headword list from an NC-ND
scan and shipping it in `noitu.db` breaches both the license we received it under and the
underlying copyright. Do not do it, and do not do it "just for validation, not for
display" — the derived DB is still a derivative of the selection.

**What *is* legitimately usable from Hoàng Phê:** nothing machine-readable. At most, a
human may consult a print copy as an *editorial reference* while hand-curating a
disputed-word list. Words a human independently confirms are facts; a wholesale
transcription of its headwords is not.

## Finding 2 — Candidate corpora, compared

| Source | Size | Format | License | Verdict for noitu |
|---|---|---|---|---|
| **`minhqnd/dictionary` v2.0.0** *(current)* | 48,216 multi-syllable vi after our filters (179 MB source, 1,500+ langs) | SQLite | CC BY-SA 4.0 | Keep. Best raw breadth; worst precision. Aggregates Wiktionary + vntk. |
| **Hồ Ngọc Đức FVDP wordlists** | `Viet11K` / `Viet22K` / `Viet39K` / `Viet74K` (74k incl. phrases) | plain `.txt`, one word per line | GPL | **Add.** Hand-curated for spellchecking, **tiered by frequency/confidence**. The tiering is the value. |
| **`undertheseanlp/dictionary`** | 79,226 unique; hongocduc 73,172 / tudientv 36,533 / wiktionary 32,484 | per-source branches, txt + py | GPL-3.0 stated for *code*; data license unstated | Use as the **delivery vehicle** for FVDP + `tudientv`, and for its overlap table. License ambiguity is a real risk — see Risks. |
| **kaikki.org (wiktextract), Vietnamese** | 45,090 distinct words (enwiktionary dump 2026-08-05, extracted 2026-08-28) | JSONL | Wiktionary CC BY-SA 3.0 / GFDL | Optional. **Freshest**, carries POS + senses. Largely already inside `minhqnd`. Useful for POS-based filtering, not breadth. |
| `zeloru/vietnamese-wordnet` | nouns/verbs/adjectives from viet.wordnet.vn + viwiktionary | txt | unclear | Marginal. POS signal only. |
| `duyet/vietnamese-wordlist` | "most common" words | txt | permissive | Marginal; frequency signal, superseded by FVDP tiers. |
| `minhqnd/Noi-Tu-Discord` | `src/assets/wordPairs.json`, 60k+ | JSON | MIT (code) | **Worth inspecting** — same author as our upstream, but already filtered *for this exact game*. Free precision work. |
| `NNBnh/noi-tu` | `/words` dir, sourced from underthesea | txt | GPL-3.0, **archived 2023-12-25** | Reference only; stale. |

## Finding 3 — the `tudientv` branch is the closest thing to a "standard" dictionary

Of the three underthesea sources, `tudientv` (36,533 entries) is the one whose size
matches a real published Vietnamese–Vietnamese dictionary rather than a crowd aggregate —
Hoàng Phê itself is ~36,000 explained headwords. It is the highest-precision list in the
set. Overlap stats published by the repo:

```
all three sources          18,833   <- highest confidence
hongocduc only             29,140
hongocduc + tudientv       15,510
hongocduc + wiktionary      9,689
wiktionary only             3,864   <- lowest confidence
tudientv only               2,092
tudientv + wiktionary          98
```

## Recommended design: a confidence tier per word, not a source swap

Add a `tier` column to `words` in `data/noitu.db`, computed at build time from source
membership plus FVDP frequency band:

```
tier 1  core        in Viet22K, or in >=3 independent sources
tier 2  standard    in Viet39K / tudientv, or in 2 sources
tier 3  extended    in >=1 source, passes phonotactics   (today's whole corpus)
tier 4  rejected    proper-noun / phrase heuristics fire
```

What each tier buys:

- **Validation** — accept tiers 1–3, so no regression in what players may play. But the
  game-over "words you missed" hint and the bot's own moves draw from tiers 1–2 only, so
  the app never *teaches* `trương công định` as a word.
- **Bot difficulty** — easy bot: tier 1. Hard bot: all tiers. Today difficulty is tuned
  on search depth alone; lexicon breadth is a second, more human-feeling axis.
- **Dispute answers** — a rejection can say *why*, and a tier-3 acceptance is honest
  about being permissive.

Proper-noun suppression is cheap and high-yield: Vietnamese place/person names are
overwhelmingly 2–3 Sino-Vietnamese syllables and are absent from FVDP's curated lists.
`in minhqnd AND NOT in any FVDP tier AND matches name-shaped pattern` catches
`tân an thạnh`, `trần danh án`, `cẩm xá` without hand-listing them.

## Risks

1. **License stacking.** `minhqnd` is CC BY-SA 4.0 (share-alike); FVDP is **GPL**. The
   two are *one-way* compatible (CC BY-SA 4.0 → GPLv3, not the reverse). A DB derived
   from both is defensible only if the combined `noitu.db` ships under **GPLv3**, or if
   FVDP is used purely as a *filter* (a membership test) and no FVDP-originated string
   ships that is not also in the CC BY-SA source. **The filter-only route is strongly
   preferred** — it keeps `noitu.db` CC BY-SA 4.0 and leaves `NOTICE` /
   `data/ATTRIBUTION.md` structurally intact. Verify FVDP's exact license text before
   either route.
2. **`undertheseanlp` data license is unstated.** The README declares GPL-3.0 for *source
   code*; the data's status is inherited from upstreams, not declared. Prefer pulling
   FVDP from its original Hồ Ngọc Đức distribution; the underthesea branch is a mirror of
   convenience.
3. **`minhqnd` v2.0.0 pin.** Makefile and Dockerfile pin URL and SHA-256, with
   `web/tests/dictionary-source.test.js` asserting they agree. Any new source must join
   that same pinning discipline or the guarantee dies.
4. **Tiering is not free precision.** FVDP is a *spellcheck* list: it carries inflected
   and colloquial forms and misses domain vocabulary. Tier 1 will reject some legitimate
   words. Accepting all of tiers 1–3 for validation, as designed above, contains that
   blast radius.

## Recommended next steps

1. Pull `Viet11K/22K/39K/74K` and `tudientv`, measure overlap against our 48,216 — how
   many of ours are in **zero** curated lists? That number sizes the junk problem exactly
   and replaces the 10–20% sample estimate.
2. Inspect `minhqnd/Noi-Tu-Discord`'s `wordPairs.json` (MIT, same upstream author,
   already game-filtered). If its precision holds up, it is a free tier-2 signal.
3. Confirm FVDP's license text verbatim; decide **filter-only vs GPL relicense** of
   `noitu.db`. Filter-only is the recommendation.
4. Only then plan the builder change (`server/cmd/build-dictionary`): `--curated <list>`
   inputs, a `tier` column, name-shape rejection, updated `meta` rows.
5. Hoàng Phê: close the thread. If a human-curated "authoritative disagreement" list is
   ever wanted, that is manual editorial work against a print copy, not a data import.

## Sources

- [Từ điển tiếng Việt (Từ điển Hoàng Phê) — Internet Archive](https://archive.org/details/tu-dien-tieng-viet-vien-ngon-ngu-hoc)
- [Từ điển "Hoàng Phê – Viện Ngôn ngữ" và "Hoàng Phê – Hồng Đức"](https://vhds.baothanhhoa.vn/tu-dien-hoang-phe-vien-ngon-ngu-nbsp-va-hoang-phe-hong-duc-nbsp-bai-1-41928.htm)
- [Vietlex](https://vietlex.vn/cong-cu)
- [minhqnd/dictionary](https://github.com/minhqnd/dictionary)
- [undertheseanlp/dictionary](https://github.com/undertheseanlp/dictionary) · [branch `hongocduc`](https://github.com/undertheseanlp/dictionary/tree/hongocduc)
- [kaikki.org — Vietnamese](https://kaikki.org/dictionary/Vietnamese/index.html)
- [minhqnd/Noi-Tu-Discord](https://github.com/minhqnd/Noi-Tu-Discord)
- [NNBnh/noi-tu](https://github.com/NNBnh/noi-tu)
- [zeloru/vietnamese-wordnet](https://github.com/zeloru/vietnamese-wordnet)
- [duyet/vietnamese-wordlist](https://github.com/duyet/vietnamese-wordlist)

## Unresolved questions

- Is `minhqnd/dictionary` v2.0.0 still the newest release, or has a v3 shipped since the
  2026-09-04 pin?
- FVDP's exact license text and canonical home — the Leipzig URL
  (`informatik.uni-leipzig.de/~duc/Dict/`) now 404s; only mirrors were reachable.
- Does `tudientv` (36,533 entries) itself derive from a copyrighted printed dictionary?
  If it is an unattributed scrape of a commercial product, adopting it inherits that
  problem. **Check before use.**
- Do we want tier-gated bot vocabulary, or is search depth alone the difficulty axis the
  game design wants? Product call, not a research finding.
