# Dictionary Data Attribution

The Vietnamese dictionary data used by this game is **not** original work of this project.
It is derived from a third-party dataset licensed under **CC BY-SA 4.0**, and this file
records the attribution and the modifications required by that license.

## Source

| Field | Value |
|---|---|
| Project | [minhqnd/dictionary](https://github.com/minhqnd/dictionary) |
| Asset | [`dictionary.db`, release v2.0.0](https://github.com/minhqnd/dictionary/releases/download/v2.0.0/dictionary.db) (~179 MB) |
| Author | [minhqnd](https://github.com/minhqnd) |
| Data license | [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/) — full text in [`LICENSE`](./LICENSE) |
| Code license (not used here) | MIT |

That project is itself an aggregation. Its own upstream sources include
[Wiktionary](https://vi.wiktionary.org/) (CC BY-SA) and
[vntk/dictionary](https://github.com/vntk/dictionary), among other Vietnamese dictionary
projects. Those upstream attributions carry through this file.

## Modifications made by this project

`server/cmd/build-dictionary` transforms the upstream `dictionary.db` into `data/noitu.db`.
The derived database is a **modified version** of the source data. Changes:

1. **Language filter** — kept only entries with `lang_code = 'vi'`; all other languages
   (of 1,500+ language pairs in the source) were dropped.
2. **Length filter** — kept only words of **2 or more space-separated syllables**, as
   required by the nối từ game rules. Single-syllable entries were dropped.
3. **Content filter** — dropped entries containing digits or punctuation, entries using
   letters Vietnamese does not have (f, j, w, z), and entries whose syllables do not fit
   Vietnamese phonotactics (a closed inventory of onsets, nuclei and codas). This removes
   loanwords and foreign phrases such as "credit card" and "world cup" that the
   multilingual source tags as Vietnamese. Diacritic-free Vietnamese words ("con cua")
   are kept.
4. **Normalization** — all words Unicode NFC-normalized, lowercased, and whitespace-collapsed.
5. **Spelling aliases** — added an `aliases` table mapping alternative Vietnamese spellings
   to canonical entries. Two kinds: competing tone placement in open oa/oe/uy syllables
   (`hoà` → `hòa`, `thuý` → `thúy`), and i/y alternation in Sino-Vietnamese syllables
   (`quí` → `quý`, `lí` → `lý`). The majority are the i/y kind. These aliases are generated
   by this project and are not present upstream.
6. **Added columns and tables** — `first` and `last` syllable columns, a `syllables` count,
   an index on `first`, a `syllables` out-degree table, and a `meta` table recording
   provenance. All added for game lookups.
7. **Deduplication** — the source lists a word once per sense; entries were deduplicated by
   normalized form. A generated spelling variant that is itself a real word, or that more
   than one word would claim, is discarded rather than recorded as an alias.
8. **Dropped fields** — all definitions, translations, pronunciations, examples, part-of-speech
   tags, and relations from the source were discarded. The derived database contains
   **only word forms**, not meanings.

## Share-alike obligation

CC BY-SA 4.0 is a **share-alike** license. The derived database `data/noitu.db`, and any
distribution of it, remains licensed under **CC BY-SA 4.0** — including when it is shipped
inside a container image or any other packaged build of this project.

This obligation applies to the **data only**. The source code of this project is licensed
separately under Apache-2.0 (see the repository root `LICENSE` and `NOTICE`). The derived
database is loaded at runtime from a file and is never compiled or linked into the binary,
keeping the two licensing regimes on separate artifacts.

## How to reproduce the derived data

```sh
make fetch-dict   # downloads the ~179 MB upstream dictionary.db into data/
make dict         # derives data/noitu.db from it
```

Neither file is committed to version control; both are build artifacts.
