---
title: "Measurement: first dump-built database against the kaikki one"
date: 2026-09-08
plan: 260908-2056-wiktionary-dump-corpus-and-meanings
status: complete
---

# Measurement: the dump-built database

Both databases were built on 2026-09-08 on the same machine with the same `accept()` filter.
The kaikki one was built with the last kaikki-era builder (`builder_version` 4) from the
export kaikki served that afternoon; the dump one with this plan's builder (`builder_version`
5) from the `latest/` dump, which was the 2026-09-01 run.

## The file measured

| | |
|---|---|
| URL | `https://dumps.wikimedia.org/viwiktionary/latest/viwiktionary-latest-pages-articles.xml.bz2` |
| Resolved to | the 2026-09-01 run (`source_fetched_at` 2026-09-01T11:52:49Z, the file's own mtime kept by `curl -R`) |
| Size | 63,513,513 bytes |
| SHA-256 | `ed66c932f535b0b362d1141c02273b02ed01c4101e856378bd15e88d277bb8d9` (matches the research report's hash of the dated file) |
| `source_pages` | 43,011 |
| Decompressed | 505,982,744 bytes |

## Build log

```
pages 391543, in the main namespace 349461, redirects skipped 3237, without a Vietnamese section 303213
Vietnamese sections: legacy {{-vie-}} 35884, new == {{langname|vi}} == 7127, pages with both 0, titles merged 131
parts of speech: noun 15890, verb 8983, adj 6285, place 3258, pr-noun 1666, n 987, adv 689, phrase 589, idiom 541, v 503, adjc 499, proverb 333, proper noun 159, interj 131, …
headings without a label: cụm động từ 2, giải thích 1, định nghĩa 1
codes that ended a legacy section, commonest: tyz 256, eng 152, mtq 147, nut 49, vi-m 47, nuo 39, tyj 29, kpm 16, mlc 14, tou 14, aav-qal 13, vie-m 12, jra 10, sqi 10, bdq 9
definitions kept 40937 (cut at 200 characters: 515), dropped as empty after stripping 271
templates dropped whole, commonest: alternative spelling of 77, vi-alternative spelling of 40, senseid 37, vie alternative spelling of 36, misspelling of 34, syn of 32, mention 18, en 13, zh 13, synonym of 11
  rejected      4: contains a digit
  rejected    155: contains punctuation
  rejected   6359: fewer than 2 syllables
  rejected    162: no Vietnamese letters
  rejected 303213: not a Vietnamese-language entry
accepted 36200 distinct words, 35062 with a meaning, (43011 pages, sha256 ed66c932f535b0b362d1141c02273b02ed01c4101e856378bd15e88d277bb8d9) in 32s
generated 1736 spelling aliases (558 skipped as ambiguous or already real words)
wrote data/noitu.db
```

**Wall time: 21–32 s** for the whole build on a laptop, Go's pure-Go bzip2 included. The
two-minute risk did not materialise; the `bzip2 -dc` fallback was not needed.

The counters reconcile: 349,461 main-namespace pages − 3,237 redirects − 303,213 without a
Vietnamese section = 43,011 = `source_pages`. The definition counters are taken after
`accept()`, so they describe words that land: 40,937 definitions kept against 40,842 rows,
the difference being senses past the cap on the 131 merged title pairs. The "codes that
ended a legacy section" line lists only language codes (tyz, eng, mtq, nut, …), which is the
check that no heading code is missing from the maps and cutting sections short.

The legacy count (35,884) is one page short of the research report's 35,885 because one page
opens its Vietnamese section after a level-2 heading of another dialect and is read as that
one; the new-dialect count differs by one for the same reason (7,127 vs 7,128). "Pages with
both" is zero, so the merge path for mixed pages stays untested against real data.

## Graph metrics, old vs new

| metric | kaikki (v4) | dump (v5) | change |
|---|---|---|---|
| words | 34,813 | **36,200** | +1,387 |
| syllables | 6,081 | 6,172 | +91 |
| syllables that open a word | 4,487 | 4,600 | +113 |
| …with ≥ 2 continuations | 3,163 | 3,246 | +83 |
| dead-end syllables | 1,594 | 1,572 | −22 |
| aliases | 1,664 | 1,736 | +72 |
| database size on disk | 2.2 MB | 6.9 MB | meanings text |

No metric regresses.

## Corpus diff

| | |
|---|---|
| shared | **34,813** — every kaikki word is in the dump build |
| gained | 1,387 |
| lost | **0** |

The first build lost 15 words (`tây tạng`, `thượng hải`, `nam kinh`, …). All were pages that
run `{{-vie-}}{{-pron-}}{{vie-pron|…}}{{-place-}}` together on one line, which the scanner
read as no section at all. Fixed in phase 1 (headings are read from a line that starts with
`{{-`, several per line); the second build lost none.

Gained, 25 at random: nghiến ngấu, hồn ai nấy giữ, pháo đùng, trèo cao té đau, ngồi rồi, tối
như hũ nút, tiêu tức, kinh kỳ, rón rón, tự phê bình, vàng hương, trơ mắt ếch, thìa khóa, qua
cầu rút ván, tinh khí, rộng chân rộng cẳng, say lử cò bợ, miệng ăn, sảo thai, phỉ dạ, vắng
như chùa bà banh, cọc tìm trâu, óng a óng ánh, lồm lộp, phơi phóng. Reduplicatives and
idioms, as the research predicted.

## Bot vs bot, 60 games each

Run with `go test ./internal/bot/ -run RealCorpus -v -count=1` against each database copied
to `data/noitu.db`. (The kaikki database needed an empty `meanings` table and a
`meaning_count` row added to a scratch copy: the v5 store refuses a v4 file, by design.)

| | kaikki | dump |
|---|---|---|
| hard beats easy | 100% (3.4 moves) | 95% (3.5 moves) |
| medium beats easy | 88% | 82% |
| hard beats medium | 60% (2.9 moves) | 72% (4.1 moves) |
| easy vs easy, game length | 16.8 moves | 13.7 moves |
| hard decision latency | n=74 p95 2.6 ms max 3.4 ms | n=68 p95 2.5 ms max 4.0 ms |

Both pass the ladder test's thresholds. The differences are within what a 60-game sample
moves between runs; the corpus is 4% larger and the graph slightly better connected.

## Meanings

| | |
|---|---|
| `meaning_count` (senses) | 40,842 |
| `words_with_meaning` | 35,062 of 36,200 = **96.9%** (floor in `verify`: 60%) |
| senses per word | 1: 30,454 · 2: 3,773 · 3: 583 · 4: 167 · 5 (the cap): 85 |
| definitions cut at 200 characters | 587 |
| definitions dropped as empty after stripping | 942 |
| senses with an empty part-of-speech label | 4,849 = **11.9%** |

Labels, by sense: danh từ 14,471 · động từ 7,804 · tính từ 5,839 · (empty) 4,849 · địa danh
3,702 · danh từ riêng 1,859 · phó từ 758 · cụm từ 544 · thành ngữ 446 · tục ngữ 329 · thán từ
112 · đại từ 47 · liên từ 44 · số từ 17 · giới từ 11 · tiền tố 7 · trợ từ 3.

The empty share is above the plan's 10% target, but the unmapped-heading list has been worked
through: every code seen more than once is mapped (see below). What remains empty is the
research report's "no-pos" class — pages whose Vietnamese section has definitions under no
part-of-speech heading at all (`sun phát`, `kiến vàng`, `long đong` in the sample). Nothing
in the wikitext gives those a label.

**Headings without a label, by frequency:** cụm động từ 2, classifier 1, det 1, giải thích 1,
prop 1, reading 1, tiếng anh 1, tyj 1, định nghĩa 1, đồng nghĩa khác âm 1. Nothing worth a
map entry.

**Templates dropped whole, ten commonest:** `vi-han form of` 222, `vi-nom form of` 163,
`alternative spelling of` 96, `senseid` 79, `rfdef` 63, `abbreviation of` 48,
`vi-alternative spelling of` 45, `vie alternative spelling of` 44, `vie-han form of` 41,
`misspelling of` 36. The "form of" and "spelling of" family is the one worth teaching next:
a definition that is only `{{vi-alt sp|hoá thạch}}` strips to nothing today, and `hóa thạch`
is one of the 1,138 words without a meaning for that reason. `rfdef` is a request for a
definition and correctly yields none.

### Deviations from the plan's label map, decided in phase 1 from the frequency tables

- `pron` on this wiki is **pronunciation** (36,533 legacy headings, 5,501 new), not pronoun.
  The pronoun code is `pronoun` (158 + 141), also `per-pronoun`. The plan's `pron → đại từ`
  would have labelled 36,000 pronunciation sections as pronouns.
- The new dialect uses `{{section|code}}` (3,506 headings) as well as `{{ĐM|code}}`, and the
  shorthand codes `n` (647 + 9,488 wiki-wide) and `v` (353 + 3,774) beside `noun`/`verb`.
- `{{-dfn-}}` (4,615) is a "definitions" heading placed *under* a part-of-speech heading, so
  it is transparent to the label rather than a heading of its own.
- Bare headword lines `{{vi-noun}}`, `{{vie-noun}}`, `{{vi-pr-noun}}` set the label when
  their code is a part of speech; `{{vi-pron}}`, `{{vi-etym-sino}}` and kin do not.
- Two definition-shaped templates beyond the plan's list were taught because they top the
  definition-line template counts: `{{nhãn|…}}`/`{{context|…}}`/`{{term|…}}` as label
  spellings, `{{n-g|…}}` as a non-gloss definition, and `{{see-entry|x}}`/`{{like-entry|x}}`
  (13,122 + 1,038 definition lines wiki-wide) rendered as `Xem x`.
- Definition lines without a space after `#` (`#{{term|cái cân}} Đồ dùng…`) are accepted;
  only `##`, `#:`, `#*`, `#;` are sub-items.

## The 50-sense sample

Drawn with `SELECT word, pos, gloss FROM meanings ORDER BY RANDOM() LIMIT 50` from the
first build and read by hand. Verdicts: **fine 43, terse 5, wrong 2.**

| # | word | pos | gloss | verdict |
|---|---|---|---|---|
| 1 | quý tộc | danh từ | Họ dòng sang. | terse |
| 2 | khuyến mại | động từ | Xúc tiến việc mua bán hàng hóa, cung ứng dịch vụ bằng cách dành cho khách hàng những lợi ích nhất định. | fine |
| 3 | bất đắc dĩ | tính từ | Không có sự lựa chọn khác. | fine |
| 4 | hắc điếm | danh từ | (từ cổ) Quán trọ, khách sạn, nơi tạm trú (có thể do kẻ xấu lập ra nhằm cướp của, giết người khi có dịp). | fine |
| 5 | đèn vách | danh từ | Đèn dầu hoả treo trên vách nhà. | fine |
| 6 | chuẩn cơm mẹ nấu | cụm từ | Món hợp khẩu vị, ăn mãi không ngán. | fine |
| 7 | tác ác | động từ | Làm việc ác. | fine |
| 8 | ỉ ê | tính từ | Từ gợi tả tiếng khóc nhỏ, dai dẳng và ỉ eo một cách khó chịu (thường nói về trẻ con) | fine |
| 9 | dơ bẩn | tính từ | (Phương ngữ) Xem nhơ bẩn | fine (cross-reference) |
| 10 | tốt bộ | | Chỉ đẹp có bề ngoài. | fine, no label |
| 11 | hãm hại | động từ | Làm hại, giết chết bằng thủ đoạn ám muội. | fine |
| 12 | thuần hậu | tính từ | Chất phác hiền hậu. | fine |
| 13 | phúc bạc | | Phúc mỏng, ít phúc (không phải bạc là trắng, dù tác giả có ý đối với chữ má đào). | fine, no label |
| 14 | chặt chẽ | tính từ | Có quan hệ khăng khít, gắn kết với nhau. | fine |
| 15 | long đong | | Vất vả, nay đây mai đó, hay gặp nhiều rủi ro. | fine, no label |
| 16 | hồng bì | danh từ | Thứ cây cùng họ với cam, quít, quả nhỏ, da vàng, có lông nhung, vị chua ngọt. | fine |
| 17 | thế chiến i | danh từ riêng | Xem Chiến tranh thế giới thứ nhất. | fine (cross-reference) |
| 18 | khuyên nhủ | động từ | Khuyên bảo ân cần. | fine |
| 19 | thường thới hậu a | địa danh | Một xã thuộc huyện Hồng Ngự, tỉnh Đồng Tháp, Việt Nam. | fine |
| 20 | gạo sen | danh từ | Hạt trắng hình hạt gạo, ở đầu nhị đực của hoa sen, dùng để ướp chè. | fine |
| 21 | kèn trống | danh từ | Kèn và trống thường sử dụng trong đám ma. | fine |
| 22 | lộng quyền | động từ | Làm việc vượt quá quyền hạn của mình, lấn cả quyền hạn của người cấp trên. | fine |
| 23 | quay đĩa | danh từ | (Khẩu ngữ) máy quay đĩa (nói tắt) | fine |
| 24 | tam giáo | danh từ | (Id.). Ba thứ đạo ở Trung Quốc thời trước. | terse (a source abbreviation survived) |
| 25 | điều dưỡng viên | danh từ | Người phụ trách công tác điều dưỡng, … cho đến phục hồi, trị… | fine (cut at 200) |
| 26 | bán tống bán tháo | cụm từ | (Khẩu ngữ) Xem bán đổ bán tháo | fine (cross-reference) |
| 27 | kiểu cách | danh từ | Kiểu mẫu và cách thức. | fine |
| 28 | hiểm ác | tính từ | Ác một cách ngấm ngầm. | fine |
| 29 | ca nô | danh từ | Thuyền máy cỡ nhỏ, mạn cao, có buồng máy, buồng lái, dùng chạy trên quãng đường ngắn. | fine |
| 30 | bút pháp | danh từ | (cũ) Phong cách viết chữ Hán. | fine |
| 31 | thuần việt | tính từ | Có nguồn gốc từ người Việt. | fine |
| 32 | sun phát | | Muối của a-xít sun-phu-rích. | fine, no label |
| 33 | kiến vàng | | Loài kiến nhỏ, màu vàng, đốt đau. | fine, no label |
| 34 | trinh phú | địa danh | Một xã thuộc huyện Kế Sách, tỉnh Sóc Trăng, Việt Nam. | fine |
| 35 | lạnh gáy | tính từ | Xem lạnh người. | fine (cross-reference) |
| 36 | giải phóng | động từ | Làm cho được tự do, cho thoát khỏi địa vị nô lệ hoặc tình trạng bị áp bức, kiềm chế, ràng buộc. | fine |
| 37 | đẹp duyên | động từ | (kiểu cách) Xem kết duyên | fine (cross-reference) |
| 38 | thanh đạm | tính từ | (Ẩm thực) Giản dị, không có những món cầu kì hoặc đắt tiền. | fine |
| 39 | suy tàn | động từ | Ở trạng thái suy yếu và tàn lụi, không còn sức sống. | fine |
| 40 | tràng thạch | danh từ | (Địa lý học). | **wrong** — the definition after the label was a template the stripper dropped |
| 41 | toàn bộ | danh từ | Tất cả các phần, các bộ phận của một chỉnh thể. | fine |
| 42 | bát tiên | danh từ riêng | Tám vị tiên là Hán Chung Ly, … hay được vẽ trên màn trướng. | fine |
| 43 | ba mũi giáp công | danh từ | Tiến công bằng ba hình thức kết hợp: quân sự, chính trị và binh vận. | fine |
| 44 | sở ước | | Điều mình mong được. | fine, no label |
| 45 | giao diện lập trình ứng dụng | danh từ | (programming) Đặc tả quy định cách … | terse (English label from a pasted template) |
| 46 | ngộ gió | | Bị cảm vì gặp gió. | fine, no label |
| 47 | ảo tung chảo | tính từ | Cảm giác khó tin. | terse |
| 48 | nhóm bếp | | Đốt lửa cho củi bắt đầu cháy trong bếp. | fine, no label |
| 49 | trần hy tăng | danh từ riêng | Xem Trần Bích San | fine (cross-reference) |
| 50 | cung ứng | động từ | Cung cấp đáp ứng nhu cầu, thường là của sản xuất, hoặc của hành khách. | fine |

Two wrong out of fifty, for different reasons; well under the one-in-five threshold that
would have sent the stripper back to phase 1. The one systematic gap is a label followed by
a template-only definition (#40), which leaves a bare `(label).` — a follow-up for the
stripper, template by template.

## Words without a meaning

1,138 words (3.1%). Three read at random:

- `hóa thạch` — one definition, `# {{vi-alt sp|hoá thạch}}`, an alternative-spelling
  template the stripper does not know.
- `lóng lánh`, `khoái trá` — the page has only pronunciation, paronyms and a `{{-see-}}`
  section with `{{like-entry|…}}` as a bullet, not a `#` line. No definition to carry.

## Verification run alongside this measurement

- `go vet ./... && go test ./... -race`: green.
- `npm run check && npm test`: green (183 tests).
- `npm run test:e2e` and the two Docker image variants: see the plan's session log.

## After code review

The review (`plans/reports/code-reviewer-260908-2210-wiktionary-dump-corpus-and-meanings.md`)
found no critical issue; its findings were fixed in the same working tree: the bot-game e2e
assertion now reads the drawn opening's sense from the fixture list instead of assuming
`danh từ`; the chain-row styling is scoped to the outer list so the sense list renders as a
numbered list; the stripper drops format characters (bidi overrides, zero-width spaces) as
well as controls; a pre-v5 database is refused with a message that says to rebuild; a lone
short template parameter is no longer mistaken for a language code; the definition counters
are taken after `accept()`; section-ending codes are tallied; the meanings lookup happens once
per move; `aria-controls` is set only while the panel exists. The numbers above are from the
build after those fixes.
