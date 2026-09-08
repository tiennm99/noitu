---
title: "Research: building our own dictionary DB from a viwiktionary dump"
date: 2026-09-08T14:34+07:00
type: research
status: complete
scope: research-only — no code touched
follows: research-260907-1650-vietnamese-dictionary-sources.md
---

# Research: can we build our own dictionary DB from a viwiktionary dump?

## Executive summary

**Technically yes, cheaply — but as a *replacement* it is a downgrade, and the answer to
"instead of the current database" is no.**

viwiktionary is already inside what we ship. The upstream `data/dictionary.db` carries a
per-row provenance table, and it says our corpus is four sources merged: `TVTD` 30,028
rows, `Wiktionary` (the vi edition) 17,365, `Wiktionary EN` 19,697, `tudientv.com` 3,421.
Of the 48,216 words in `data/noitu.db`, **15,924 (33%) trace to the viwiktionary source
and 32,292 (67%) do not.** A viwiktionary-only build would therefore have to *beat* our
current 16k contribution from that source by 3× before it merely broke even — and the
words it would add are the same crowd-sourced tail that produced the false-positive
problem the previous report measured.

**What is genuinely on offer is a self-controlled extraction, not more words.** wiktextract
has a real `vi` edition extractor (12 modules, ~138 KB) and **kaikki.org publishes a
prebuilt `viwiktionary/` dataset**, so we could take viwiktionary as a *primary* source
with POS and category metadata attached — which is exactly the filtering axis the current
opaque aggregate denies us. Proper nouns (`Danh từ riêng`), Nôm characters and
single-syllable morphemes become droppable by *label* rather than by heuristic.

**Recommendation: keep `minhqnd` as the breadth layer; add a self-extracted viwiktionary
as a second, POS-tagged source feeding the confidence tier from the previous report.** Do
not swap. And note the elephant: **`TVTD` alone is 24,369 of our shipped words (51%) and
its provenance is undocumented** — that is a bigger risk than anything viwiktionary poses.

---

## Methodology

- 5 web calls (budget cap): 1 search, 4 page fetches.
- Local evidence, measured directly this session: `data/noitu.db` and the 179 MB upstream
  `data/dictionary.db`, both via sqlite.
- Repo evidence: `server/cmd/build-dictionary/{main,filter}.go`, `Makefile`.
- Key terms: `viwiktionary statistics`, `Mục từ tiếng Việt`, `wiktextract editions`,
  `kaikki viwiktionary`.

---

## Finding 1 — viwiktionary's real size, in the only unit that matters

| Metric | Value | Source |
|---|---|---|
| Content pages | 346,778 | `vi.wiktionary.org/wiki/Special:Statistics` |
| Entries, all languages | 330,021 across **1,622 languages** | same |
| **Vietnamese-language entries** | **43,037** (`Thể loại:Mục từ tiếng Việt`) | category page |
| — of which Nôm characters | 1,731 (+ 4,380 entries carrying Nôm) | same |
| Total edits / active users / bots | 2,559,588 / **73** / 32 | Special:Statistics |

87% of viwiktionary's pages are entries, but **only ~13% of its entries are Vietnamese**;
the rest is foreign vocabulary written in Vietnamese. The usable ceiling is the 43,037,
minus Nôm characters, minus single-syllable words — which the game rejects outright
(`vietnamese.HasEnoughSyllables`, ≥2 syllables). Our corpus is 48,216 multi-syllable
words. **The ceiling of a viwiktionary-only build is plausibly below what we ship today,
and cannot exceed 43,037 even counting every entry we would have to throw away.**

73 active editors and 6.43 edits per page is a thinly maintained wiki. Freshness is not
the win here.

## Finding 2 — viwiktionary is already 33% of our corpus (measured)

`data/dictionary.db` has `sources(id, name)` and `words.source_id`. Measured this session,
Vietnamese rows only:

```
source              raw    multisyll   shipped in noitu.db
TVTD              30028        24474                 24369   <- 51% of our corpus
Wiktionary (vi)   17365        16216                 15924   <- 33%
Wiktionary EN     19697         5121                  4864   <- 10%
tudientv.com       3421         3064                  3059   <-  6%
                                                     48216
```

Two things follow.

1. **A swap is a 2/3 cut.** Rebuilding from viwiktionary alone replaces 48,216 words with
   ~16k of known provenance plus whatever self-extraction recovers beyond the aggregate's
   17,365-row snapshot. Even the optimistic case does not reach parity.
2. **The upstream deduplicates across sources, so agreement is unrecoverable.** Each word
   carries exactly one `source_id` — the "only in viwiktionary" count equals its entire
   set (16,216). We cannot ask "how many sources agree on this word?" of the current
   upstream at all. Self-extracting viwiktionary is the only way to get that second
   independent vote, which is the *actual* argument for doing it.

## Finding 3 — the extraction path is cheap and already exists

- `wiktextract` ships a per-edition `vi` extractor: `analyze_template, descendant,
  etymology, example, linkage, models, page, pos, section_titles, sound, tags,
  translation` (~138 KB). Real, not a stub; less mature than `en`.
- **kaikki.org publishes `https://kaikki.org/viwiktionary/`** — prebuilt, so no dump
  download and no Lua-expanding extraction run is strictly required.
- Our builder already accepts a non-SQLite input: `--words <file>` (one word per line,
  `#` comments), used today for the e2e fixture. A JSONL → wordlist reduction feeds it
  with **zero builder changes**; a `--jsonl` input that keeps POS is a small addition.
- Everything downstream is unchanged: `accept()` normalizes, demands ≥2 syllables, and
  already rejects digits, punctuation, non-Vietnamese alphabets and impossible syllables.

Cost is real but small: a reduction script, a pin, and one new attribution entry.

## Finding 4 — licensing is fine; pinning discipline is the catch

Wiktionary text is **CC BY-SA 4.0 (dual GFDL)** — same family as our current CC BY-SA 4.0
posture, share-alike either way. No relicensing of `noitu.db`, no `NOTICE` restructure;
`data/ATTRIBUTION.md` gains an entry naming viwiktionary and the dump/extraction date.
This is materially cleaner than the FVDP (GPL) option from the previous report.

The catch is reproducibility. `Makefile` pins `DICT_URL` + `DICT_SHA256`, asserted by
`web/tests/dictionary-source.test.js`. Wikimedia dumps are dated and immutable
(`viwiktionary/YYYYMMDD/`) so they pin cleanly; **kaikki.org publishes rolling files with
no versioned releases**, so pinning it means pinning a SHA-256 of a file that can be
replaced under the same URL. Prefer the dated Wikimedia dump + our own extraction run if
the pin guarantee matters more than convenience.

## Options

| | Option | Verdict |
|---|---|---|
| **A** | Replace `minhqnd` with viwiktionary-only | **No.** ~2/3 corpus cut, precision unchanged. |
| **B** | Self-extract viwiktionary as a *second* POS-tagged source, feeding the tier column | **Yes.** Buys the independent second vote and label-based proper-noun/Nôm rejection. |
| **C** | Status quo | Acceptable but leaves 51% of the corpus (`TVTD`) unprovenanced. |

Option B is the previous report's tiering design with viwiktionary supplying a vote that
is *ours* rather than an aggregate's — and it is the one source whose license is
unambiguously compatible.

## Recommended next steps

1. **Measure the ceiling before building anything.** Pull `https://kaikki.org/viwiktionary/`,
   count Vietnamese lemmas with ≥2 syllables after our `accept()` rules. That single number
   decides B outright: if it lands near 16k, viwiktionary adds a vote but no breadth; if it
   lands near 35k, it is also a breadth layer.
2. Overlap it against our 48,216 — how many shipped words does viwiktionary confirm, and
   how many of our unconfirmed words are in *no* independent list.
3. Check whether the extraction exposes `Danh từ riêng` / Nôm / morpheme labels usably.
   That is the whole precision argument; if the labels are sparse, B loses most of its value.
4. **Chase `TVTD`.** It is 51% of what we ship and nothing in the repo says what it is. If
   it is a scrape of a commercial dictionary, that outranks every question in this report.
5. Only then: `--jsonl` input + `tier` column + a dated Wikimedia dump pin.

## Sources

- [vi.wiktionary.org — Special:Statistics](https://vi.wiktionary.org/wiki/Special:Statistics)
- [Thể loại:Mục từ tiếng Việt](https://vi.wiktionary.org/wiki/Th%E1%BB%83_lo%E1%BA%A1i:M%E1%BB%A5c_t%E1%BB%AB_ti%E1%BA%BFng_Vi%E1%BB%87t)
- [kaikki.org — editions index, incl. `viwiktionary/`](https://kaikki.org/)
- [tatuylonen/wiktextract](https://github.com/tatuylonen/wiktextract) — `src/wiktextract/extractor/vi`
- [Wikimedia dumps](https://dumps.wikimedia.org/viwiktionary/)
- Local: `data/dictionary.db` (`sources`, `words`), `data/noitu.db` (`meta`, `words`)

## Unresolved questions

- **How many viwiktionary Vietnamese entries survive our ≥2-syllable filter?** Unmeasured;
  the deciding number. Step 1 above.
- Is the `Wiktionary` source in `dictionary.db` really the vi edition? Inferred from its
  name standing beside `Wiktionary EN`; the upstream does not document it.
- What is `TVTD`? Undocumented, and the majority of our corpus.
- Does kaikki's vi extraction carry per-entry categories, or only POS? Determines whether
  proper nouns can be dropped by label.
- viwiktionary dump size and extraction runtime — not fetched (budget); only relevant if
  we reject the kaikki prebuilt for pinning reasons.
