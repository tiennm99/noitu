---
title: "Research: using Từ điển tiếng Việt (Hoàng Phê) in noitu"
date: 2026-09-10T09:39+07:00
type: research
status: complete
scope: research-only — no code touched
supersedes-part-of: plans/reports/research-260907-1650-vietnamese-dictionary-sources.md (Finding 1)
---

# Research: using Từ điển tiếng Việt (Hoàng Phê) in noitu

## Executive summary

**You cannot ship Hoàng Phê data. That has not changed since 2026-09-07 and will not
change without a signed licence.** Re-verified today: the only digital copy in public
circulation is the 2003 scan on Internet Archive, rights **"Attribution-NonCommercial-NoDerivs
4.0 International"** (39,924 entries, PDF/EPUB/OCR text). NoDerivs forbids sharing anything
derived from it — a headword list in `noitu.db` is exactly that. No open dataset of Hoàng Phê
exists on GitHub; the one new candidate found (`Trannosaur/published_dicts`) is Wiktionary /
Panlex material only.

**What is new and actually actionable: Vietlex licenses this data, with precedent.** In 2008
VCCorp/BaamBoo *bought* the rights to *Từ điển tiếng Việt 2008* so the public could look it up
free at `tratu.baamboo.com` (today `tratu.soha.vn`). Vietlex states it is "sẵn sàng hợp tác"
on data, and its corpus is already licensed to Oxford University Press, Naver and Osaka
University. Contact is public: `vietlextudien@gmail.com`, (84) 903 457 937. So the question
"can noitu use Hoàng Phê?" has exactly one legitimate yes-path: **ask Vietlex and pay**.

**The CC tag on that scan is probably not a real licence.** A CC licence conveys only rights
the licensor holds. The Internet Archive uploader of a scanned 2003 book is not the rights
holder — that is Hoàng Phê's heirs / Viện Ngôn ngữ học / NXB Đà Nẵng, and Vietnam's life + 50
term (Hoàng Phê d. 2005) runs to ~2055. Treat "CC BY-NC-ND on archive.org" as an uploader's
assertion, not a grant. Everything reasoned from that tag inherits the defect — including
path 3 below, which is defensible on *practical* grounds (private, unshared, aggregate-only),
not because the tag says so.

**And the honest nuance the previous report got wrong:** CC BY-NC-ND does not forbid you from
*making* a private derivative — it forbids *sharing* one, and forbids commercial use. A
strictly local, never-committed, never-shipped audit that reports an **aggregate number**
("what % of our 34,813 words are in Hoàng Phê") is defensible. The moment that per-word
membership decides which words ship, the shipped database encodes Hoàng Phê's *selection* and
becomes a derivative work. Aggregate metric: fine. Per-word filter: not fine.

---

## Methodology

- 4 web searches (budget 5), 5 page fetches, 2026-09-10.
- Local evidence: `data/noitu.db` `meta` + `words` read directly; `Dockerfile`, `.gitignore`,
  `data/ATTRIBUTION.md`, `Makefile` read.
- Prior art reused, not re-researched: `research-260907-1650-vietnamese-dictionary-sources.md`,
  `research-260908-1507-undertheseanlp-dictionary.md`.

## What the project ships today

| Fact | Value |
|---|---|
| Corpus | Wiktionary tiếng Việt (dump builder in `server/cmd/build-dictionary`) |
| Current `data/noitu.db` (stale, kaikki-built, `builder_version` 4) | 34,813 words / 1,664 aliases |
| Licence of derived DB | CC BY-SA 4.0, share-alike, recorded in `data/ATTRIBUTION.md` |
| Distribution | `data/*.db` gitignored, **but** `Dockerfile:69` bakes `noitu.db` into the public image |

That last row is the crux. "We never commit the database" is not a defence — the container
image *is* distribution, and it is the artifact a Hoàng Phê licence would have to cover.

## The four paths, ranked

### 1. License it from Vietlex — the only way to ship it

| Item | Evidence |
|---|---|
| Vietlex = Trung tâm Từ điển học, compiler of the Hoàng Phê–Vietlex line (2007→) | vietlex.com |
| Sells/licences data; corpus ~150M syllables, customers incl. OUP, Naver, Osaka Univ. | vietlex.com/about |
| Precedent: VCCorp/BaamBoo bought *TĐTV 2008* rights for free public lookup (2008) | Dân trí |
| Precedent: Prodic 2007 licence let distributors "cài đặt, sao chép dữ liệu … trên các sản phẩm bán ra" | Dân trí |
| Contact | `vietlextudien@gmail.com`, (84) 903 457 937, Hà Nội |

What to ask them for, concretely, so the ask is answerable:
- ~36,000 headwords **only** (no definition text) — a far smaller ask than the full entries,
  and enough for validation, bot vocabulary and tiering. Definitions already come from
  Wiktionary under CC BY-SA.
- Right to embed in a **redistributed container image** of an open-source, non-commercial game.
- No sublicensing, no bulk re-export: the DB exposes membership queries, never a dump endpoint.

Consequences if they say yes — budget for these before asking:
- `noitu.db` stops being CC BY-SA-only. The share-alike section of `data/ATTRIBUTION.md`
  breaks: you would be mixing CC BY-SA text (Wiktionary meanings) with a proprietary,
  non-share-alike headword list in one artifact. **The clean shape is two files** —
  `noitu.db` (CC BY-SA, unchanged) and a separate licensed `headwords.db` the server reads
  alongside it, with its own licence file and its own attribution.
- CI and the public Docker image cannot carry a proprietary file without the licence
  permitting exactly that. If it does not, the image ships Wiktionary-only and the licensed
  list stays in a private deployment — which is a product decision, not a build detail.
- Expect money and a contract. There is no self-serve API.

### 2. Human editorial reference — legal today, zero licence needed

Buy the book (or the Vietlex e-dictionary) and use it the way a person uses a dictionary:
when a word is disputed, a human checks it and records **their own** verdict. Facts and
independent judgement are not the compilation. This scales to a few hundred adjudicated
words, not to 36,000, and it is the only path that needs no negotiation. Ship the result as
this project's own curated list, attributed as this project's editorial work.

### 3. Private aggregate audit of the OCR scan — defensible, useful, narrow

Two things hold here even though the scan's CC tag is unreliable (see summary): the tag's own
terms would permit it — CC BY-NC-ND 4.0 §2(a)(1)(B) grants "produce and reproduce, but not
Share, Adapted Material" — and, tag aside, a private unshared aggregate measurement is not an
act of distribution and carries no realistic exposure. So, locally and never committed:

```
extract headwords from the archive.org OCR text
→ intersect with our 34,813
→ report ONE number: % of our words present, plus a sample of misses for human reading
```

That number is the thing the 2026-09-07 report only estimated ("10–20% junk"). It sizes the
precision problem exactly and costs nothing. Hard boundaries: the extracted list never enters
git, never enters the image, never gates a per-word decision in the builder, and no derived
file is published. If it starts deciding which words ship, stop — you are now shipping the
selection, which Vietnam's IP Law protects in a compilation (Art. 14/22) independently of any
CC terms.

Quality caveat: a densely typeset two-column dictionary scan OCRs badly in Vietnamese —
mangled tone marks, run-together headwords, running heads mistaken for entries. The resulting
number is worth ±a few points, not more.

### 4. Runtime lookups against tratu.soha.vn / vtudien — reject

Breaks the server-authoritative offline design (`README` "the browser never holds the
wordlist" rests on a local DB, and per-turn latency budgets do not survive a third-party
round-trip), depends on a site whose TLS certificate does not even match its hostname today,
and the *TĐTV* content there is licensed **to VCCorp**, not to you — a CC BY-SA claim about
that site appears only in third-party download-site blurbs and could not be verified against
the site itself. Scraping it is `tudientv` all over again, which this project already refused
(`research-260908-1507`).

## What Hoàng Phê would actually buy you

Not breadth — 36,000 headwords is *fewer* than the current 34,813 multi-syllable words plus
single-syllable entries the game does not use. It buys **precision and authority**: a
rejection you can defend, a bot that never plays `trần danh án`, and a "words you missed"
hint that only teaches real words.

The legal free approximation of that, already recommended and still unimplemented, is the
tier design in `research-260907-1650`: FVDP `Viet11K/22K/39K/74K` as a membership filter,
a `tier` column, name-shape rejection. **Do that first.** It costs one builder change and no
lawyer. If the residual precision gap is still visible to players afterwards, that measured
gap is the business case to take to Vietlex — and path 3 gives you the number to put in the
email.

## Recommended next steps

1. Run the path-3 audit locally. Produce one number and a miss sample. Do not commit anything
   from it.
2. Implement the FVDP tier design (`--curated` inputs, `tier` column, filter-only so
   `noitu.db` stays CC BY-SA 4.0).
3. Re-measure. If the gap still matters, email Vietlex with the specific ask in path 1
   (headwords only, container redistribution, non-commercial OSS) and the measured gap.
4. Until a licence exists in writing, treat every Hoàng Phê-derived byte as unshippable —
   including "only for validation, never displayed".

## Sources

- [Từ điển tiếng Việt (Từ điển Hoàng Phê) — Internet Archive](https://archive.org/details/tu-dien-tieng-viet-vien-ngon-ngu-hoc) — rights: Attribution-NonCommercial-NoDerivs 4.0
- [Vietlex — Giới thiệu](https://vietlex.com/about/index.html)
- [Dân trí — "Từ điển có bản quyền được sử dụng miễn phí"](https://dantri.com.vn/cong-nghe/tu-dien-co-ban-quyen-duoc-su-dung-mien-phi-1206537994.htm)
- [Trannosaur/published_dicts](https://github.com/Trannosaur/published_dicts) — Wiktionary/Panlex only, no Hoàng Phê
- [undertheseanlp/dictionary](https://github.com/undertheseanlp/dictionary)
- [vhds.baothanhhoa.vn — Hoàng Phê–Viện Ngôn ngữ vs Hoàng Phê–Hồng Đức](https://vhds.baothanhhoa.vn/tu-dien-hoang-phe-vien-ngon-ngu-nbsp-va-hoang-phe-hong-duc-nbsp-bai-1-41928.htm)
- Local: `data/ATTRIBUTION.md`, `Dockerfile:69`, `data/noitu.db` `meta`

## Unresolved questions

- Will Vietlex license headwords-only for a non-commercial open-source game, and at what
  price? Unanswerable without contacting them.
- Exact licence terms of the 2008 VCCorp deal (scope, whether headword redistribution was
  included) — press coverage only; the contract is not public.
- Actual licence of `tratu.soha.vn` content: third-party sources claim CC BY-SA; the site
  could not be fetched (certificate mismatch) to verify. Irrelevant unless path 4 is revived.
- `data/noitu.db` on disk is kaikki-built (`builder_version` 4) while `data/ATTRIBUTION.md`
  documents the Wikimedia dump path. Stale local artifact or documentation drift — worth a
  separate check, unrelated to this research.
