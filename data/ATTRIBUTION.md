# Dictionary Data Attribution

The Vietnamese dictionary data used by this game is **not** original work of this project.
It is derived from Wiktionary text licensed under **CC BY-SA 3.0 Unported**, and this file
records the attribution and the modifications required by that license.

## Source

| Field | Value |
|---|---|
| Original work | Entries of [Wiktionary tiếng Việt](https://vi.wiktionary.org/), written by its contributors |
| Original license | [CC BY-SA 3.0 Unported](https://creativecommons.org/licenses/by-sa/3.0/) at the time of the 2018 scrape (Wikimedia projects moved to 4.0 on 2023-06-29); Wiktionary text is dual-licensed CC BY-SA / GFDL |
| This database's license | **CC BY-SA 3.0 Unported**, the same licence the source text carried — full text in [`LICENSE`](./LICENSE) |
| Redistributed by | [undertheseanlp/dictionary](https://github.com/undertheseanlp/dictionary), a 2018-12-10 scrape of vi.wiktionary.org merged with two other wordlists |
| Asset | [`dictionary/words.txt` at commit `2c078cf`](https://raw.githubusercontent.com/undertheseanlp/dictionary/2c078cfc373b06e2980d324ce1d7bd13740c3319/dictionary/words.txt) — 4,813,111 bytes, 79,226 JSONL rows |
| SHA-256 | `4c3e0e6117e4bdfa97731e135c3d4a05881889909267394a8de8d88ef79f13f0` |
| Rows used | the 32,484 tagged `wiktionary` |
| Repository code license (not used here) | GPL-3.0 |

The asset is pinned by commit, so the URL is immutable and the checksum above always
describes the same bytes. The attribution chain has two links — Wiktionary's contributors,
then undertheseanlp as the intermediary that scraped and redistributed — and both are named
here because CC BY-SA attribution belongs to the authors, not only to the last host.

### Branches deliberately not used

The merged file tags every word with which of three wordlists contain it. Only
`wiktionary` rows are read; the other two are never consulted — not for words, not for
anything.

- **`hongocduc`** — Hồ Ngọc Đức's Free Vietnamese Dictionary Project wordlist, distributed
  under the GNU GPL according to the branch README (a mirror; the canonical site no longer
  resolves). Using it would make this database GPL-derived and force a relicense to GPLv3.
  Excluded so the data stays CC BY-SA.
- **`tudientv`** — its branch README declares copyright *"Chưa rõ"* (unknown) and names the
  Soha/Vietlex dictionary (Hoàng Phê, editor) as its primary source: an unlicensed
  derivative of a commercial work. Excluded, and not to be reopened.

## Modifications made by this project

`server/cmd/build-dictionary` transforms the upstream wordlist into `data/noitu.db`. The
derived database is a **modified version** of the source data. Changes:

1. **Source selection** — kept only rows whose source list contains `wiktionary`; rows from
   the two other wordlists were not read.
2. **Length filter** — kept only words of **2 or more space-separated syllables**, as
   required by the nối từ game rules. Single-syllable entries were dropped.
3. **Content filter** — dropped entries containing digits or punctuation, entries using
   letters Vietnamese does not have (f, j, w, z), and entries whose syllables do not fit
   Vietnamese phonotactics (a closed inventory of onsets, nuclei and codas). Diacritic-free
   Vietnamese words ("con cua") are kept.
4. **Normalization** — all words Unicode NFC-normalized, lowercased, and
   whitespace-collapsed. Capitalized headwords (`Hà Nội`) become lowercase entries; nothing
   is removed on the basis of capitalization.
5. **Spelling aliases** — added an `aliases` table mapping alternative Vietnamese spellings
   to canonical entries. Two kinds: competing tone placement in open oa/oe/uy syllables
   (`hoà` → `hòa`, `thuý` → `thúy`), and i/y alternation in Sino-Vietnamese syllables
   (`quí` → `quý`, `lí` → `lý`). The majority are the i/y kind. These aliases are generated
   by this project and are not present upstream.
6. **Added columns and tables** — `first` and `last` syllable columns, a `syllables` count,
   an index on `first`, a `syllables` out-degree table, and a `meta` table recording
   provenance (source URL, commit, sources kept and excluded). All added for game lookups.
7. **Deduplication** — entries were deduplicated by normalized form, so a word listed both
   capitalized and lowercase upstream appears once. A generated spelling variant that is
   itself a real word, or that more than one word would claim, is discarded rather than
   recorded as an alias.
8. **Dropped fields** — the per-word source tags were discarded after selection. The
   derived database contains **only word forms**, not meanings; the upstream wordlist
   carries none either.

## Share-alike obligation

CC BY-SA 3.0 is a **share-alike** license. The derived database `data/noitu.db`, and any
distribution of it, remains licensed under **CC BY-SA 3.0** — including when it is shipped
inside a container image or any other packaged build of this project.

This obligation applies to the **data only**. The source code of this project is licensed
separately under Apache-2.0 (see the repository root `LICENSE` and `NOTICE`). The derived
database is loaded at runtime from a file and is never compiled or linked into the binary,
keeping the two licensing regimes on separate artifacts.

## How to reproduce the derived data

```sh
make fetch-dict   # downloads the ~4.8 MB upstream wordlist into data/ and verifies its checksum
make dict         # derives data/noitu.db from it
```

Neither file is committed to version control; both are build artifacts.
