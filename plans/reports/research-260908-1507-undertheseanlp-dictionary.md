---
title: "Research: switching the corpus to undertheseanlp/dictionary"
date: 2026-09-08T15:07+07:00
type: research
status: complete
scope: research-only — no code touched, no shipped DB modified
follows: research-260908-1434-viwiktionary-as-dictionary-source.md
---

# Research: switch to `undertheseanlp/dictionary`?

## Executive summary

**Do not ship the merged list, and do not switch wholesale — but take one thing from it
today.** All four branches were downloaded and run through our real
`build-dictionary --words` filter this session; every number below is measured, not
estimated.

**The blocker is `tudientv`.** Its own branch README says copyright is *"Chưa rõ"*
(unknown) and that its primary source is *"Từ điển Soha … do Vietlex biên soạn (Hoàng Phê
chủ biên)"* — i.e. it is an unlicensed derivative of the commercial Vietlex/Hoàng Phê
dictionary that the first report already ruled out. That answers that report's open
question with a hard no. The merged 79,226-word list contains it, so the merged list is
not shippable. It only contributes 1,300 words we don't otherwise have.

**The votes are not a quality signal.** The merged file is JSONL with a per-word `source`
array, which looked like the confidence-tier input the earlier report designed for.
Measured, it isn't: `hà nội` has 2 votes, `cẩm xá` 2, `a mú sung` 2, `kháng đón` 2, while
`cánh diều` has 1 and `mặt trời` 2. A vote≥3 gate (14,346 words) drops `mặt trời` and
`con người`. The three sources are not independent enough to vote. **That design dies here.**

**The real find is capitalization.** undertheseanlp preserves case; `minhqnd` does not —
**0 of its 70,511 Vietnamese rows carry any uppercase**, so case is destroyed upstream and
unrecoverable. 4,773 entries appear *only* ever capitalized (`Hà Nội`, `Trương Công Định`,
`A Mú Sung`, `Cẩm Xá`), and `Mặt Trời` is not among them because it also occurs lowercase.
That set is a proper-noun blocklist, and **4,145 of our shipped 48,216 words are on it.**

**Recommendation: use undertheseanlp as a filter, not as the corpus.** Apply the blocklist
to what we already ship — 48,216 → 44,071, removing 4,145 place and person names, with the
DB staying CC BY-SA 4.0 because no undertheseanlp string is shipped. Adopting its words as
well is a real option but costs a **relicense of `noitu.db` to GPLv3**.

---

## Licensing, per branch (read from each branch's own README)

| Branch | Words | Declared license | Verdict |
|---|---|---|---|
| `master` (merged) | 79,226 | GPL-3.0 for *"mã nguồn"* (code); **data unstated** | Not shippable — contains `tudientv` |
| `hongocduc` | 73,172 | *"Dữ liệu được phân phối theo GNU GPL"* — data **is** GPL | Usable → forces GPLv3 downstream |
| `tudientv` | 36,533 | **"Bản quyền: Chưa rõ."** Primary source: Soha/Vietlex, Hoàng Phê chủ biên | **Never ship.** Unlicensed commercial derivative |
| `wiktionary` | 32,484 | scrape of vi.wiktionary.org, **2018-12-10** | CC BY-SA, but an 8-year-old snapshot |

Repo last pushed **2018-12-10**. `hongocduc` is Hồ Ngọc Đức's 2003 wordlist. So the whole
thing is a 2018 snapshot of mostly-2003 data — which is exactly why it misses modern
vocabulary (below).

## Measured yields, through our own `accept()` filter

| Corpus | Accepted words | Note |
|---|---|---|
| **current `noitu.db` (minhqnd)** | **48,216** | today |
| undertheseanlp, everything | 67,523 | includes unlicensed `tudientv` |
| everything − proper nouns | 62,892 | still includes `tudientv` |
| `hongocduc` + `wiktionary` | 65,907 | GPL + CC BY-SA |
| **`hongocduc` + `wiktionary` − proper nouns** | **61,276** | the only clean "switch" candidate |
| `hongocduc` alone | 62,571 (58,776 − proper nouns) | GPL |
| `wiktionary` branch alone | 26,845 (22,636 − proper nouns) | worse than extracting the live dump |
| **current − blocklist** | **44,071** | filter-only, license unchanged |
| **union(current, hnd+wik) − blocklist** | **69,049** | breadth + precision, needs GPLv3 |

Source composition of the 69,600 space-separated merged entries, for the record:
`hongocduc` only 26,533 · all three 14,346 · `hongocduc`+`tudientv` 14,198 ·
`hongocduc`+`wiktionary` 9,144 · `wiktionary` only 3,568 · `tudientv` only 1,720 ·
`tudientv`+`wiktionary` 91.

## What a straight switch would cost

Switching to `hongocduc`+`wiktionary`−proper-nouns (61,276) against today's 48,216:
overlap 36,298, **gained 24,978, lost 11,918**. The losses break down:

```
4,145  dropped deliberately as always-capitalized proper nouns   <- wanted
1,300  present only in tudientv (unlicensed)                     <- unrecoverable cleanly
6,473  absent from undertheseanlp entirely                       <- the real cost
```

That last bucket is a mix of proper nouns minhqnd had already lowercased (`hồng kông`,
`mĩ đức tây`, `phù nam`, `cầu đỏ`) and **genuine modern vocabulary a 2003 list cannot
have**: `tế bào gốc`, `hằng số vũ trụ`, `đồng phạm giản đơn`, `bánh mì hoa cúc`,
`đèn hoa đăng`. minhqnd carries live Wiktionary; undertheseanlp is frozen at 2018.

## Blocklist validation

Against the always-capitalized set, 11 known-bad probes were all dropped
(`trương công định`, `trần danh án`, `tân an thạnh`, `cẩm xá`, `vĩnh điện`, `cam lâm`,
`hà nội`, `sài gòn`, `a mú sung`, `a lưới`, `kháng đón`) and 10 ordinary words all kept
(`học sinh`, `mặt trời`, `con người`, `bánh mì`, `cánh diều`, `xe đạp`, `giáo viên`,
`hoa hồng`, `tình yêu`, `nước mắm`). No false positives in the probe set.

It is not complete — it cannot catch a proper noun that never appears capitalized in
undertheseanlp, and 6,473 of our words are unknown to it entirely. It removes 4,145 of the
roughly 10–20% junk the first report sampled, which is the cheapest precision we have found.

## Options

| | Option | Words | License effect | Verdict |
|---|---|---|---|---|
| **A** | **Blocklist only** — filter today's corpus by undertheseanlp capitalization | 44,071 | **none**, stays CC BY-SA 4.0 | **Do this now.** Small, reversible, no new dependency in the shipped data |
| **B** | Union: current + `hongocduc`+`wiktionary`, blocklist applied | 69,049 | **`noitu.db` → GPLv3** | Best corpus. A deliberate license decision, not a technical one |
| **C** | Replace with `hongocduc`+`wiktionary` − proper nouns | 61,276 | `noitu.db` → GPLv3 | Worse than B for no gain — throws away 6,473 modern words |
| **D** | Ship the merged 79,226 list | 67,523 | unlicensed Vietlex derivative | **No.** |

GPL direction matters: CC BY-SA 4.0 → GPLv3 is permitted one-way, so a corpus mixing both
must go out as GPLv3. Filter-only use (A) ships no GPL string, so nothing changes.

## Recommended next steps

1. **A, now.** Vendor the derived blocklist (4,773 lowercased keys, ~90 KB) as a data file
   with its provenance recorded, add `--blocklist <file>` to `build-dictionary`, count the
   drops in the build log the way reject reasons already are, and pin the source commit
   the way `DICT_SHA256` is pinned. It is a membership test, so `data/ATTRIBUTION.md` gains
   a "used as a filter" note rather than a license change.
2. Spot-check 100 of the 4,145 drops by hand before it ships. A blocklist that eats a real
   word is worse than the proper noun it removed.
3. Decide B separately, as a licensing question: is `noitu.db` allowed to be GPLv3? If yes,
   B is the best corpus available from open data. If no, A is the ceiling and the
   viwiktionary extractor from the previous report is the only breadth route left, since
   Wiktionary text is CC BY-SA.
4. Drop `tudientv` from consideration permanently, and record why so nobody re-opens it.

## Sources

- [undertheseanlp/dictionary](https://github.com/undertheseanlp/dictionary) — branches
  [`master`](https://github.com/undertheseanlp/dictionary/tree/master),
  [`hongocduc`](https://github.com/undertheseanlp/dictionary/tree/hongocduc),
  [`tudientv`](https://github.com/undertheseanlp/dictionary/tree/tudientv),
  [`wiktionary`](https://github.com/undertheseanlp/dictionary/tree/wiktionary)
- [tudientv.com](http://tudientv.com/) — the `tudientv` branch's upstream
- Local: `data/noitu.db`, `data/dictionary.db`, `server/cmd/build-dictionary`

## Unresolved questions

- Is a GPLv3 `noitu.db` acceptable? Product/licensing call; decides option B.
- The 4,145 blocklist drops are unaudited. Sample them before shipping.
- 6,473 of our words are in no undertheseanlp list. Unknown split between junk and modern
  vocabulary; a second signal (live viwiktionary) would separate them.
- `hongocduc` is distributed as GPL by a mirror, and the first report could not reach Hồ
  Ngọc Đức's canonical site (404). The license claim rests on the mirror's README.
