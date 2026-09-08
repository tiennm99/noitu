# Measurement: first kaikki build vs the undertheseanlp corpus

Date: 2026-09-08. Phase 3 of `plan.md`. The pre-switch `data/noitu.db` (26,845 words) was
copied aside before the first kaikki build and is the "old" side of every table.

## The file measured

```
source_url         https://kaikki.org/viwiktionary/Ti%E1%BA%BFng%20Vi%E1%BB%87t/kaikki.org-dictionary-Ti%E1%BA%BFngVi%E1%BB%87t.jsonl
source_sha256      51ddc2fbd73cd7e2a468ccff3c31200928e7200cc58622f528ba3eb84c7a49f0
source_rows        44564
source_fetched_at  2026-09-08T10:04:08Z   (kaikki's export of 2026-09-06, dump 2026-09-01)
size               62,288,004 bytes; longest line 61,552 bytes
```

Build log:

```
rejected      4: contains a digit
rejected    136: contains punctuation
rejected   8348: fewer than 2 syllables
rejected    160: no Vietnamese letters
parts of speech: noun 16219, verb 9355, adj 6698, name 5180, unknown 4652, adv 1024,
  phrase 392, proverb 322, character 198, intj 175, pron 100, conj 70, prep 48, num 45,
  abbrev 38, particle 18, romanization 14, prefix 13, suffix 2, det 1
accepted 34813 distinct words
generated 1664 spelling aliases (552 skipped as ambiguous or already real words)
```

## Commands

```sh
cp data/noitu.db /tmp/old.db                       # before the switch
make fetch-dict && make dict                       # or the raw curl + go run lines in README
cd server && go test ./internal/bot/ -run RealCorpus -v -count=1   # once per database at data/noitu.db
# graph numbers and diff: sqlite, see the queries below
```

```sql
SELECT COUNT(*) FROM words;
SELECT COUNT(*) FROM syllables;
SELECT COUNT(DISTINCT first) FROM words;
SELECT COUNT(*) FROM (SELECT first FROM words GROUP BY first HAVING COUNT(*) >= 2);
SELECT COUNT(*) FROM syllables WHERE out_degree = 0;
```

## Playability

| metric | old (undertheseanlp) | new (kaikki) |
|---|---|---|
| words | 26,845 | **34,813** |
| syllables | 5,709 | 6,081 |
| syllables that open a word | 4,158 | 4,487 |
| …with ≥2 continuations | 2,787 | 3,163 |
| dead-end syllables | 1,551 | 1,594 (27.2% → 26.2% of syllables) |

Bot-versus-bot, 60 games each, same run:

| | old | new |
|---|---|---|
| hard-vs-easy win rate (moves) | 98% (3.6) | 97% (3.2) |
| medium-vs-easy | 87% | 95% |
| hard-vs-medium (moves) | 57% (3.1) | 67% (3.1) |
| easy-vs-easy game length | 14.4 moves | 14.4 moves |
| hard decision p95 | 1.5 ms | 3.1 ms |

Both real-corpus tests pass on both databases. Game length is unchanged; the stronger bots
win more often on the denser graph, which is the expected direction.

## Corpus diff

| | |
|---|---|
| shared | 25,392 |
| gained | **9,421** — `công nghiệp hóa`, `bóng râm`, `bội thu`, `đồng dao`, `bỏ ngỏ`, `đắt hàng`, `ấn độ giáo`, `chạy làng`, `béo nung núc`, `nhìn muốn rụng trứng` |
| lost | **1,453** — see below |

### The 1,453 lost words

Sample of 25 (seed 7): `trịnh tuệ`, `tuân khanh`, `qua quít`, `cúc pha`, `mành mành`,
`cây bài chặt`, `trảm phong`, `kéo cánh`, `ngọt lự`, `rưng rức`, `làu nhàu`, `trong lúc`,
`khí hư`, `trần ích tắc`, `nháo nhác`, `trần cảnh`, `óc ách`, `lườm lườm`, `hỗn luân`,
`trụi lủi`, `tục tác`, `lảu nhảu`, `phát sầu`, `hãng hàng không quốc gia việt nam`, `trói ké`.

Two kinds. Roughly a fifth are historical person names (`trần ích tắc`, `trần cảnh`, `trịnh
tuệ`) whose pages have since been deleted or moved. The rest are real vocabulary, heavily
reduplicatives (`rưng rức`, `nháo nhác`, `óc ách`, `làu nhàu`, `lườm lườm`), that the 2018
scrape had and the current wiktextract export does not. Most likely those pages still exist
but in a markup the `vi` extractor does not yet parse — the raw 2026 dump has 36,200
words to kaikki's 34,813, a 1,387-word gap of the same order. **Not acted on:** the plan
does not merge sources, and the net is +7,968 words. If the reduplicatives matter, the
option is a second `--words` input reduced from the raw dump, as the previous plan's
research described.

## Verdict

Every number above today's; nothing regresses. Corpus accepted as built.
