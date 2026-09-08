---
title: "Code review: Wiktionary dump corpus and word meanings"
date: 2026-09-08
reviewer: code-reviewer
plan: plans/260908-2056-wiktionary-dump-corpus-and-meanings/plan.md
verdict: DONE_WITH_CONCERNS
---

# Code review: Wiktionary dump corpus and word meanings

## Scope

Uncommitted working tree, 42 paths: builder rewrite (`dump.go`, `wikitext.go` + tests, 1,406
new lines), `meanings` table and store loader, `Sense` on the wire, chain UI, fixture list,
build/CI/docs/attribution. `kaikki_list*.go` deleted.

Checks run here: `go vet ./...` clean, `go test ./... -race` all packages ok,
`npm run check` 375 files / 0 errors, `npx vitest run` 183 passed. Playwright and Docker not
run (instructed). `grep -rniI "kaikki\|wiktextract\|jsonl\|only word forms" --exclude-dir=plans .`
is empty — phase 6 step 5 satisfied.

## Critical

None. No trust-boundary defect, no data loss, no breaking wire or schema change.

## High

### H1 — the new bot-game e2e assertion is ~40% flaky

`web/e2e/bot-game.spec.js:70`

```js
await expect(page.locator('.meanings').first()).toContainText('(danh từ)');
```

The opening word is drawn uniformly from the fixture words whose last syllable clears
`minOpeningOutDegree` (`server/internal/wsapi/room.go:33` = 20). In
`testdata/fixture-words.txt` only `sinh` reaches that (out-degree 22), so the opening is one
of the ten `…sinh` words — and four of them are labelled `động từ`, not `danh từ`:

```
phát sinh  động từ|Nảy sinh, xuất hiện.
khai sinh  động từ|Đăng ký việc sinh ra của một người.
tái sinh   động từ|Sinh ra lần nữa; làm sống lại.
ký sinh    động từ|Sống nhờ vào cơ thể sinh vật khác.
```

`RandomOpeningWord` (`server/internal/dictionary/store.go:399`) picks with `rand.IntN(n)`, so
the assertion fails on roughly two runs in five. It is the only assertion behind the plan's
"Senses show as `(danh từ) …`" success criterion, so that criterion is currently proved by a
test that will go red in CI on unrelated pull requests.

Fix, cheapest first: assert a label-shaped pattern plus the gloss of the word actually drawn
(read it out of the fixture list, which the suite already imports through
`web/e2e/fixture-dictionary.js`); or give the four `động từ` openers a `danh từ` sense first.
Do not simply delete the label assertion — it is the criterion.

## Medium

### M1 — the meanings list inherits the chain-row styling; the `<ol>` numbers never render

`web/src/lib/components/ChainHistory.svelte:64-73, 113-176`

The component styles bare `ol` and `li`. Svelte scopes by class and the nested sense items
are in the same markup, so they receive the same scope class. Compiled output, from running
`svelte/compiler` on the file:

```
li.svelte-o0xqf7 { display: flex; flex-wrap: wrap; align-items: baseline; gap: 8px;
                   padding: 8px 12px; border: 1px solid var(--border);
                   border-radius: var(--radius-sm); background: var(--surface); }
root_5 = <li class="svelte-o0xqf7"> </li>        // <- the sense item
```

Consequences: every sense renders as its own bordered, padded card on `--surface`, and
`display: flex` removes `display: list-item`, so `ol.meanings { list-style: decimal }` paints
nothing. Phase 5's non-functional requirement "numbered by the `<ol>`" is not met and the
panel does not look like the phase-5 sketch. Neither `svelte-check` nor the e2e selectors can
see this.

Fix: scope the row rules to the outer list (give the outer `<ol>` a class and use
`.rows > li`), or reset on the inner list (`.meanings li { display: revert; border: 0;
padding: 0; background: none; }`). Either keeps `chainWords` (`ol li .word`) working.

### M2 — format and bidi characters survive the stripper

`server/cmd/build-dictionary/wikitext.go:279-289`

The `strings.Map` drops `unicode.IsControl`, which is category Cc only. Verified by running
`stripWikitext` on a copy of the file: an input carrying U+202E and U+200B comes out with both
characters intact. U+202E RLO, U+200B ZWSP, U+200E/200F and the U+2066–2069 isolates therefore
reach the database, the wire, the chain panel and the button's `aria-label`.

Nothing is executed — Svelte interpolates and there is no `{@html}` anywhere in `web/src`
(confirmed) — so this is display integrity rather than XSS. But the plan states the builder
"drops control characters … so nothing the store loads can be shaped like a chat injection",
and a bidi override is precisely that shape: one in a gloss reverses the rest of the rendered
line. One-line fix: also return `-1` for `unicode.Is(unicode.Cf, r)`.

### M3 — a v4 database is refused with an opaque message

`server/internal/dictionary/store.go:146-176`

Requiring `meaning_count` is intended (phase 3, and `measurement.md` records the refusal as
by design). But what a developer with a pre-existing `data/noitu.db` sees is

```
read dictionary meaning_count: sql: no rows in result set
```

which names the key and nothing else — not that the file predates `builder_version` 5, not
that `make fetch-dict && make dict` is the fix. `source_license` already carries an
`(is this a noitu.db?)` hint; give this one the equivalent. Docker builds the database fresh,
so this is developer ergonomics rather than a deploy risk.

## Low

- **L1 `isLangCode` eats real words.** `server/cmd/build-dictionary/wikitext.go:396-412`
  drops any 2–3-letter lowercase-ASCII first parameter, not only a language code. Verified:
  `{{q|con}} Một loài vật.` → `Một loài vật.` (qualifier lost); `{{gloss|hoa}} nghĩa.` →
  `nghĩa.`; `{{l|con}} là con.` → `là con.`. Phase 1 specified "positional params after a
  leading `vi`". Bounded loss; tighten to a short code set, or only drop when a positional
  parameter remains.
- **L2 the definition counters describe more than the database.** `dump.go:130-133` calls
  `definitions()` before `accept()`, so `defsKept` / `defsEmpty` / `defsCut` include pages
  whose title is rejected (6,679 in the measured run). The log line reads as if it described
  the table: "definitions kept 55858" against 40,842 rows. Move the call below `accept`, or
  reword the line.
- **L3 no signal for the plan's own top risk.** A legacy `{{-xxx-}}` whose code is missing
  from `posLabelMap`/`otherSectionCodes` ends the Vietnamese section early
  (`wikitext.go:isLegacyHeading`, `legacySection`): the definitions after it are lost
  silently — the page still counts, the word still lands, nothing is tallied. The plan's risk
  register relies on "definitions-kept is well below the page count", which is weak. Add a
  tally of the codes that terminated a section and print the top N beside `unmappedPos`.
- **L4 dangling `aria-controls`.** `ChainHistory.svelte:36` points at `panelId` while the
  panel is not rendered, so the reference resolves to nothing whenever the word is closed.
  Render the panel always and hide it, or drop `aria-controls` and keep `aria-expanded`.
- **L5 lookup placement differs from phase 4.** Phase 4 says the `Meanings` lookup happens
  "once per move, before the per-seat loop"; `room.go:911` calls it (and `slices.Clone`s)
  once per recipient. The comment admits it. Harmless at ≤10 seats — but fix the code or the
  phase file so the record matches.
- **L6 `measurement.md` counters do not reconcile.** In code
  `prov.pages + noVietnamese == ns0 - redirects` always holds; the recorded log gives
  349,461 − 3,237 − 303,229 = 42,995 while the file table and `source_pages` say 43,011. One
  of the two is from the earlier build. Re-copy both from a single run.
- **L7 dead map entry.** `otherSectionCodes` contains `"Han": true`; codes are lowercased
  before lookup and `"han"` is already present (`wikitext.go:103`).
- **L8 stray closer.** `stripTemplates` passes an unmatched `}}` through verbatim
  (`"Một }} nghĩa."` unchanged). Cosmetic.

## Plan verification

| Criterion | Verdict |
|---|---|
| build log reports pages / dialects / redirects / POS / definitions | met (`logDumpStats`) |
| truncated, non-bzip2, mid-page, page floor each fail by name | met, four tests in `dump_test.go` |
| `meta`: `source_url`/`sha256`/`pages`/`fetched_at`, `meaning_count`, `builder_version` 5, no `source_rows` | met, asserted in `main_test.go` |
| `Sense`, `PlayedWord.meanings`, `GameStarted.opening_meanings` in schema, both trees, `proto/testdata` | met; fields 7 and 9, nothing renumbered or reserved |
| chain behaviour: newest open, previous closes, click toggles, `Chưa có nghĩa` | store rules met (7 vitest cases); the browser proof is **H1** and the rendering is **M1** |
| attribution surfaces name the dump, "only word forms" gone | met; the modification record (item 6) reads accurately against CC BY-SA 4.0 |
| vet / test -race, check / test green; leak guard rejects `.bz2`/`.xml` | verified for the four commands; the guard is correct and cannot false-positive (final stage is distroless static, no `.xml`) |
| measurement recorded beside the plan | met, and honest: 11.9% empty labels against a 10% target, cleared through the plan's own escape clause |

`--kaikki` gone, `--dump` and `--min-pages` present, `wsapi.Dictionary` gained `Meanings`,
`dictionary` gained `Sense` / `Meanings` / `MeaningCount`. Nothing else in a public contract
moved; the only encoded measurement numbers are the coverage floor and the URL, as phase 6
requires. `builder_version` 5 and the dropped `source_rows` are both asserted by tests.

## Touchpoints — no regression found

- `internal/game` and `internal/bot` untouched; `game.Dictionary` unchanged.
- Resume: `sendGameStarted` and the replayed `sendTurnUpdate` both carry senses through the
  single `PlayedWord` call site; covered by `TestResumeReplaysMeanings`.
- Bot rooms render through the same `sendTurnUpdate`; covered by `TestBotGameCarriesMeanings`.
- `chainWords` (`ol li .word`) still matches exactly the row buttons — the nested
  `ol.meanings` contains no `.word`.
- `history-export.js` untouched; the transcript is unchanged (non-goal respected).
- `testdata/fixture-words.txt`: the word column is byte-identical to `HEAD` (diffed), so the
  e2e graph and every existing spec keep their assumptions; no duplicate words, no gloss over
  the cap, no cell with two pipes.
- Store expansion rules: `gameStarted` opens the opening word, `turnUpdate` with a word closes
  the previous newest and opens the new one, an elimination changes nothing, `toggleMeaning`
  flips, `reset()` clears (`expanded` is not in `kept`). The array-with-set-semantics choice
  is correct for Svelte 5 — `$state` proxies arrays, not `Set`s — and both mutation styles
  used (`push` and reassignment) are reactive.
- Concurrency: `Store` is write-once in `Open` and `Meanings` returns `slices.Clone`, so the
  per-room goroutines cannot reach or race dictionary state.
- Wikitext stripper: section boundaries for both dialects, inline headings on one line, `dfn`
  transparency, nested templates, unbalanced braces, the 200-rune cap at a word boundary and
  the "only punctuation is empty" rule are all covered by table tests and behave as specified.

## Recommended actions

1. H1 — make the bot-game meaning assertion independent of which opening was drawn.
2. M1 — stop the row `li`/`ol` rules applying to the nested meanings list.
3. M2 — drop `unicode.Cf` alongside `IsControl` in `stripWikitext`.
4. M3 — say "this database predates builder_version 5; rebuild" when `meaning_count` is absent.
5. L2, L3, L6 — align the definition counters with what lands, tally section-ending codes,
   re-copy the measurement numbers from one build.
6. L1, L4, L5, L7, L8 as follow-ups.

## Unresolved questions

- `npm run test:e2e` and the two Docker variants are claimed green in `measurement.md` and the
  plan; not re-run here (instructed). H1 means the e2e claim was true for the run that
  happened, not for the next one.
- `make proto-check` / `buf lint` not run (no `buf` in this session); the committed generated
  trees are self-consistent with the `.proto` by inspection.
