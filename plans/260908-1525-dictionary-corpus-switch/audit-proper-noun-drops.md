# Audit: proper-noun drops and the wiktionary-only corpus

Date: 2026-09-08. Phase 3 of `plan.md`. Every number here is reproducible from the
commands listed; the shipped `data/noitu.db` was copied aside before any build and is the
"old" side of every comparison.

## Commands

```sh
# from server/, after `make fetch-dict`
go run ./cmd/build-dictionary --merged ../data/undertheseanlp-words.jsonl \
   --out /tmp/wik.db --report-drops /tmp/drops-wik.txt
go run ./cmd/build-dictionary --merged ../data/undertheseanlp-words.jsonl \
   --sources hongocduc,wiktionary --out /tmp/hw.db --report-drops /tmp/drops-hw.txt
# sample: python, random.seed(1), random.sample(sorted(drops-wik), 100)
# casualties: (drops-wik − drops-hw) ∩ words(hw.db)
go test ./internal/bot/ -run RealCorpus -v      # once per database at data/noitu.db
```

## Build result

```
rejected     97: contains punctuation
rejected   5074: fewer than 2 syllables
rejected     37: no Vietnamese letters
rejected   4747: only ever capitalized
accepted 22419 distinct words (sources: wiktionary)
```

22,419 vs the 22,310 the research report measured. The delta is the rule: the report
dropped any form containing an uppercase letter, the builder drops forms that *begin* with
one, so `tia X`, `bộ bài tây`, `chữ Hán`, `châu Á` survive. All are ordinary words.

## 1. Drop audit — 100 sampled, seed 1

Verdicts: **P** proper noun (correct drop) · **X** not a Vietnamese word (would be rejected
by the filter anyway) · **U** unclear · **C** common word (false positive).

| | | | | |
|---|---|---|---|---|
| a yun P | an hoà tây P | an minh bắc P | an thuận P | ba vinh P |
| ban cơ U | brao X | bàn đạt P | bành trạch P | bàu năng P |
| bá xuyên P | bình hiệp P | bình tấn P | bằng la P | chiềng sơ P |
| châu hội P | chù lá phù lá P | chư krêy P | cur X | côn đảo P |
| cơ kiều P | cẩm trung P | hùng vương P | hồng bàng P | irc X |
| khánh gia P | lão quân P | lương đài P | mặc dương P | nhơn hoà lập P |
| ninh lai P | ninh thạnh P | noong bua P | nội thôn U | phiếu hữu mai U |
| phù lảng P | phương cao kén ngựa U | phần lão U | quán hành P | quắc hương P |
| rã bản P | sam mứn P | sơn hạ P | sơn vy P | tam ngọc P |
| thanh liên P | thanh văn P | thiệu đô P | thái an P | thượng lâm P |
| thượng tiến P | thạch xuân P | thới quản P | tiên cát P | tiên thọ P |
| tri lễ P | triệu giang P | triệu việt vương P | trung tú P | trà khê P |
| trà kót P | trà linh P | trường khánh P | trường long P | trần đình thâm P |
| trọng do P | tuân lộ P | tân hưng P | tân mỹ P | tân nhựt P |
| tân trì P | tây hiếu P | tăng sâm P | tĩnh gia P | tịnh an P |
| tốt động P | vinh an P | việt–mường X | vân canh P | võ trường toản P |
| võ văn tồn P | văn môn P | văn quân P | văn đình dận P | vĩnh hoà hưng bắc P |
| vĩnh lương P | vĩnh lộc b P | vĩnh trinh P | vũ thư P | vạn thuỷ P |
| xuân mai P | xuân quan P | xá bung P | xá cẩu P | xá khắc P |
| xốp cộp P | yên cát P | ô qua U | đồng nai P | đổ rượu ra sông thết quân lính C |

**P 89 · X 4 · U 6 · C 1.** Gate is ≤2 common words in 100: **passes.** The bulk is
commune names, ethnonyms and historical persons — the content the rule exists to remove.

## 2. Case casualties — 215, listed in full

Words the wiktionary rows hold only capitalized, which another branch has lowercase. They
are dropped under the plan's wiktionary-only evidence rule. Full list:
`case-casualties.txt` in the session scratchpad; reproduced here because it is the
decision input.

an bình · an dân · an hảo · an khang · an lạc · ba tiêu · biển hồ · bu lu · **bàn là** ·
**báo đáp** · bát tiên · bình chuẩn · bình chân · bình khang · bình nghị · bình sa · bình
thanh · bình thuỷ · bình trị · bình tâm · bình văn · bình điền · bích đào · bóng chim tăm
cá · bông trang · bạch cung · bản nguyên · **bảo toàn** · bắc phong · bẻ quế · bố chính ·
**bồ đề** · bồng sơn · cao biền dậy non · cao kỳ · cao nhân · cao sơn · cao tổ · cao xanh ·
cao đường · chà và · chày sương · chánh hội · **chân mây** · châu lệ · châu thành · chí
thiện · chí thành · chính tâm · **chúa nhật** · chúa trời · chị hằng · con tạo · cát lũy ·
cát tân · cát đằng · **công bình** · công dã tràng · **công giáo** · **công nguyên** ·
**cơ đốc giáo** · cường thịnh · cầm đuốc chơi đêm · cầu lam · cầu lộc · cầu ô · cẩm châu ·
cẩm đường · cửu kinh · **cựu ước** · diêm vương · diêm vương tinh · dã hạc · dương quan ·
dương đài · **dường như** · **giao tử** · giấc mai · giọt châu · giọt tương · hoa cái ·
**hoa kiều** · **hoà đồng** · hoá công · hoả tinh · hán học · hán tộc · hán tự · hán văn ·
hạ thần · hải phòng · hải triều · hải vương tinh · hằng nga · **hết sảy** · **hệ mặt
trời** · hội gió mây · hội long vân · **hồi giáo** · **khổng giáo** · kim tinh · **kinh
thánh** · la ve · lâm viên · **lưỡi hái** · lạc hầu · mạnh thường quân · **mặt trời** · nam
lâu · **nguyên đán** · **nho học** · nhân kiệt · năm cha ba mẹ · nước dương · nếm mật nằm
gai · nợ như chúa chổm · phong thu · **phù phiếm** · **phật giáo** · phật học · phật pháp ·
phật tiền · phật tổ · phật tự · **phật đản** · quang phục · quyết tiến · quân thiều · **quả
đất** · **sao hôm** · sao hỏa · sao kim · **sao mai** · sao mộc · song mai · sơn cương ·
sơn lâm · sơn mai · sơn nguyên · sơn trung · sư tử hà đông · **sừng trâu** · tam dân · tam
phủ · tam sơn · thanh nghị · thanh phong · thanh quang · thanh vận · thiên chúa · **thiên
chúa giáo** · thiên vương tinh · thuỷ liễu · thân giáp · thạch bàn · thạch thán · **thần
chết** · **tin lành** · tiên sư · **trái đất** · trướng huỳnh · trường giang · trường xuân ·
tu lý · tuần duyên · tân kỳ · **tân ước** · tây dương · tây thiên · tô hạp · tùng lâm · tế
tân · **tết nguyên đán** · **tổ quốc** · tử phòng · u minh · vinh thăng · **việt ngữ** ·
vách quế · vân hà · vân hán · vân trình · võ miếu · **văn giáo** · **văn miếu** · **văn
nhân** · văn quan · **văn võ** · văn đức · **vũ công** · **vũ hội** · vũng tàu · vương bá ·
vạn an · vạn kiếp · vạn phúc · vầng ô · **vớ bở** · **xe tơ** · xuân hoá · xuân phong ·
xuân tình · xích thố · xương thịnh · yên bình · yên chi · yên hoa · yên hà · đinh điền ·
**đường luật** · **đường thi** · đại danh · đỉnh giáp non thần · **địa cầu** · đỗ vũ

Bold: everyday or dictionary-headword vocabulary by this auditor's reading, ~55 of 215.
The rest are Sino-Vietnamese literary terms, religious/astronomical names that Wiktionary
capitalizes by convention, and genuine place names (`hải phòng`, `vũng tàu`, `bồng sơn`).

**Narrowing does not fix this.** The plan's pre-decided narrowing — drop only when every
syllable is capitalized — was measured: drops fall 4,747 → 4,337, casualties fall only
215 → 147 (`mặt trời`, `trái đất`, `tổ quốc`-style two-cap forms stay dropped), and 410
proper nouns come back in, including junk like `bản mẫu:-vie-n-` and `con kde`. Rejected.

## 3. Corpus diff — old shipped vs new

| | |
|---|---|
| overlap | 21,320 |
| gained | 1,099 (`gân bò`, `rèn đúc`, `ích kỷ`, `đáng lý`, `thở hồng hộc`) |
| lost | **26,896** |
| — dropped as proper noun | 4,245 |
| — absent from the wiktionary branch | 22,651 |
| — other | 0 |

## 4. Playability

| metric | old | new |
|---|---|---|
| words | 48,216 | 22,419 |
| syllables | 6,676 | 5,485 |
| syllables that open a word | 5,049 | 4,052 |
| …with ≥2 continuations | 3,682 | 2,691 |
| dead-end syllables | 1,627 | 1,433 |

Bot-versus-bot, `internal/bot` real-corpus tests, 60 games each:

| | old | new |
|---|---|---|
| hard-vs-easy win rate (moves) | 98% (3.3) | 98% (3.5) |
| medium-vs-easy | 87% | 88% |
| hard-vs-medium (moves) | 72% (3.5) | 55% (2.9) |
| easy-vs-easy game length | 17.5 moves | **11.3 moves** |
| hard decision p95 | 5.2 ms | 1.1 ms |

Both tests pass on both databases. Easy-vs-easy games are a third shorter: the thinner
graph runs out of continuations sooner, which is the depth loss the plan predicted.
Hard-vs-medium falling to 55% means the graph gives the stronger bot fewer ways to trap.

## 5. Decision

Drop rule on the sample: **pass** (1 common word in 100).

Case casualties: **open — needs the owner's call**, because the options trade the
"wiktionary data only" decision against ~55 everyday words:

- **Accept** the 215 losses as measured.
- **Exception list**: a checked-in list of words to keep despite capitalization, curated
  from the 215 above. Cause-aligned, small, but a hand-maintained artifact.
- **Read hongocduc for case evidence only**: recovers all 215 mechanically, but uses GPL
  data as an input even though none of its words ship. Must be stated in `ATTRIBUTION.md`.

**Recorded decision (owner, 2026-09-08): abandon the capitalization rule.** Every
wiktionary-tagged word is kept regardless of case; proper nouns stay in the corpus as they
are in the shipped database today. The rule, its reject reason and `--report-drops` were
removed from the builder.

## 6. Final corpus, without the rule

```
accepted 26845 distinct words (sources: wiktionary)
```

| metric | old | new |
|---|---|---|
| words | 48,216 | 26,845 |
| syllables | 6,676 | 5,709 |
| syllables that open a word | 5,049 | 4,158 |
| …with ≥2 continuations | 3,682 | 2,787 |
| dead-end syllables | 1,627 | 1,551 |
| overlap / gained / lost | | 25,565 / 1,280 / 22,651 |

Every lost word is absent from the 2018 wiktionary branch; none is lost to a rule.

Bot-versus-bot, 60 games each: hard-vs-easy 95% (2.7 moves), medium-vs-easy 87%,
hard-vs-medium 63% (3.2 moves), easy-vs-easy **12.9 moves** (old: 17.5). Both real-corpus
tests pass.
