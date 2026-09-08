# Dictionary Data Attribution

The Vietnamese dictionary data used by this game is **not** original work of this project.
It is derived from Wiktionary text licensed under **CC BY-SA 4.0**, and this file records
the attribution and the modifications required by that license.

## Source

| Field | Value |
|---|---|
| Original work | Entries of [Wiktionary tiếng Việt](https://vi.wiktionary.org/), written by its contributors |
| Original license | [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/) (Wiktionary text is dual-licensed CC BY-SA / GFDL) — full text in [`LICENSE`](./LICENSE) |
| Extracted by | [wiktextract](https://github.com/tatuylonen/wiktextract), published on [kaikki.org](https://kaikki.org/viwiktionary/) by Tatu Ylonen; kaikki.org distributes the extracted data under the same CC BY-SA / GFDL terms as the underlying Wiktionary text |
| Asset | [`Tiếng Việt/kaikki.org-dictionary-TiếngViệt.jsonl`](https://kaikki.org/viwiktionary/Ti%E1%BA%BFng%20Vi%E1%BB%87t/kaikki.org-dictionary-Ti%E1%BA%BFngVi%E1%BB%87t.jsonl) — the Vietnamese-language entries of the Vietnamese Wiktionary edition, ~62 MB, ~44,000 entries |
| Refresh | kaikki re-extracts from the monthly Wikimedia dump about once a week |

**The asset is not pinned.** kaikki.org keeps no dated snapshots, so each build fetches the
current export. The exact bytes a given `data/noitu.db` was built from are recorded in its
`meta` table: `source_sha256` (SHA-256 of the file as read), `source_rows` (entries read)
and `source_fetched_at` (the file's modification time). Two builds a week apart may differ
by a few hundred words; the hash says which words a given image shipped.

The attribution chain has two links before this project — Wiktionary's contributors, who
wrote the entries, and wiktextract/kaikki.org, which turned the wiki markup into structured
data — and both are named here because CC BY-SA attribution belongs to the authors, not only
to the last host. kaikki.org asks users of its data to cite:
*Tatu Ylonen: Wiktextract: Wiktionary as Machine-Readable Structured Data, Proceedings of
the 13th Conference on Language Resources and Evaluation (LREC), pp. 1317–1325, Marseille,
20–25 June 2022.*

## Modifications made by this project

`server/cmd/build-dictionary` transforms the upstream export into `data/noitu.db`. The
derived database is a **modified version** of the source data. Changes:

1. **Language selection** — kept only entries with `lang_code = "vi"`. The file is
   Vietnamese-only today; any other language would be rejected and counted.
2. **Length filter** — kept only words of **2 or more space-separated syllables**, as
   required by the nối từ game rules. Single-syllable entries were dropped.
3. **Content filter** — dropped entries containing digits or punctuation, entries using
   letters Vietnamese does not have (f, j, w, z), and entries whose syllables do not fit
   Vietnamese phonotactics (a closed inventory of onsets, nuclei and codas). Diacritic-free
   Vietnamese words ("con cua") are kept.
4. **Normalization** — all words Unicode NFC-normalized, lowercased, and
   whitespace-collapsed. Capitalized headwords (`Hà Nội`) become lowercase entries; nothing
   is removed on the basis of capitalization or part of speech.
5. **Spelling aliases** — added an `aliases` table mapping alternative Vietnamese spellings
   to canonical entries. Two kinds: competing tone placement in open oa/oe/uy syllables
   (`hoà` → `hòa`, `thuý` → `thúy`), and i/y alternation in Sino-Vietnamese syllables
   (`quí` → `quý`, `lí` → `lý`). The majority are the i/y kind. These aliases are generated
   by this project and are not present upstream.
6. **Added columns and tables** — `first` and `last` syllable columns, a `syllables` count,
   an index on `first`, a `syllables` out-degree table, and a `meta` table recording
   provenance (source URL, SHA-256, row count, fetch time, licence). All added for game
   lookups.
7. **Deduplication** — an entry appears once per part of speech upstream; entries were
   deduplicated by normalized word form. A generated spelling variant that is itself a real
   word, or that more than one word would claim, is discarded rather than recorded as an
   alias.
8. **Dropped fields** — all senses, glosses, examples, translations, pronunciations,
   etymologies, categories and part-of-speech tags were discarded. The derived database
   contains **only word forms**, not meanings.

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
make fetch-dict   # downloads kaikki's current export (~62 MB) into data/
make dict         # derives data/noitu.db from it and records the file's SHA-256 in meta
```

Neither file is committed to version control; both are build artifacts. Because the export
is refreshed upstream, a rebuild on a later day may not be byte-identical to an earlier one.
