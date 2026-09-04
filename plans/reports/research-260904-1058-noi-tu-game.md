# Research Report: Vietnamese "Nối Từ" Word-Chain Game

Conducted: 2026-09-04 10:58 (Asia/Saigon) · Repo: `D:/tiennm99dev/noitu` (empty, initial commit)

## Table of Contents
1. [Executive Summary](#executive-summary)
2. [Methodology](#methodology)
3. [Game Rules](#1-game-rules)
4. [Data Model & Core Algorithms](#2-data-model--core-algorithms)
5. [Bot AI](#3-bot-ai)
6. [Vietnamese Text Normalization](#4-vietnamese-text-normalization-the-real-bug-source)
7. [Dictionary Sources](#5-dictionary-sources-ranked)
8. [Implementation Recommendations](#6-implementation-recommendations)
9. [Common Pitfalls](#7-common-pitfalls)
10. [References](#references)
11. [Open Questions](#open-questions)

## Executive Summary

Nối từ = Vietnamese word-chain. Player says a **2-syllable** meaningful word; next player must say a 2-syllable word whose **first syllable equals the previous word's last syllable**. No reuse. Fail to answer in time → lose. Example: `ngôn ngữ → ngữ pháp → pháp luật → luật lệ`.

Implementation is trivial as a game loop; the hard 20% is (a) getting a clean 2-syllable Vietnamese word list, (b) Unicode/tone normalization, (c) a bot that doesn't feel dumb. Model it as a **directed graph**: nodes = syllables, edges = words (`a→b` for word "a b"). A move = traverse an unused edge from the current node. This is exactly **Directed Edge Geography** — PSPACE-complete, so no cheap perfect solver; use heuristic + depth-limited search.

Best data source: **`minhqnd/Noi-Tu-Discord` → `src/assets/wordPairs.json`** (MIT, ~60k word pairs, already keyed `firstSyllable → [lastSyllables]` — the exact index the game needs). Fallback/expansion: `duyet/vietnamese-wordlist` Viet74K (74k raw, filter to 2-syllable) and vi.wiktionary dumps.

## Methodology
- Sources: 4 web searches + 1 repo fetch (skill cap 5)
- Date range: 2020–2026; dictionaries current as of Jan 2026 (Wiktionary copy)
- Terms: `luật chơi nối từ`, `vietnamese wordlist github json`, `github bot nối từ thuật toán`, `Viet74K vietnamese two-syllable dataset`

---

## 1. Game Rules

**Core rule**: next word's first syllable == previous word's last syllable.

| Rule | Standard | Notes |
|---|---|---|
| Word length | exactly 2 syllables (từ ghép) | most online implementations enforce 2 strictly; some allow ≥2 |
| Validity | must exist in dictionary, meaningful | typically noun/adj compounds |
| Reuse | banned within a round | track a used-set |
| Timeout | 10–30s per turn | loss condition |
| Chain link | on **syllable**, not letter | despite folk phrasing "chữ cái cuối" |
| Start word | random or player-chosen | pick a high-out-degree node so game doesn't die instantly |

**Loss conditions**: timeout, invalid/unknown word, wrong first syllable, repeated word, no legal move remains.

**Modes seen in the wild** (Nối từ tiếng Việt app, gamevui, vuanoitu.fun, wordfight.online):
- *Thử thách* — solo, N rounds vs clock + target score
- *Thách đấu* — 1v1 alternating
- *Đấu trường* — 4 players round-robin

**Design decision needed early**: does the bot lose when it has no move (fair), or does it get to challenge a rare word? Most implementations: no move = bot loses.

## 2. Data Model & Core Algorithms

### Graph model
```
word "pháp luật"  =>  edge  pháp ──"pháp luật"──> luật
state = (currentSyllable, usedWords:Set)
legalMoves(s) = { w in adj[s] : w not in usedWords }
```

### Index structure (build once at load)
```js
// Map<firstSyllable, string[]>  -- full words (or last syllables)
{ "pháp": ["pháp luật", "pháp lý", "pháp danh", ...], "luật": ["luật lệ", "luật sư", ...] }
```
Lookup O(1); validation O(1) with a `Set<string>` of full normalized words.

### Validation pipeline
```
input -> trim -> NFC -> lowercase -> collapse spaces
      -> split on space; assert length === 2
      -> assert syllables[0] === currentSyllable
      -> assert dictionary.has(word)
      -> assert !used.has(word)
```

### Dead-end precomputation
- `outDegree[syllable]` = number of words starting with it.
- **Killer syllables**: `outDegree === 0` (or very low) — rare endings. Precompute the list; moving there is an instant win.
- Syllables with outDegree 0 make the *next* player lose immediately. Mark them at build time.

## 3. Bot AI

Perfect play = Directed Edge Geography, PSPACE-complete → no exact solver at 60k edges. Practical ladder:

| Difficulty | Strategy |
|---|---|
| Easy | random legal move |
| Medium | prefer moves ending in a **low out-degree** syllable; avoid handing the player a hub |
| Hard | 1) instant win: any move to `outDegree==0`; 2) negamax depth 3–5 with alpha-beta over remaining edges, eval = `-log(remaining moves for opponent)`; 3) fall back to Medium heuristic |
| Cruel | opening book of known trap chains |

Cost control: at depth d, branching = out-degree of visited syllables (often <50). Depth 4 is cheap; order moves by ascending opponent out-degree and cap node count.

Anti-frustration: cap the bot below always-play-the-killer, or players quit.

## 4. Vietnamese Text Normalization (the real bug source)

1. **Unicode form** — normalize to **NFC**. `ữ` can be one codepoint or `ư` + combining tilde. Mismatch = false rejections.
2. **Tone placement variants** — `hoà`/`hòa`, `thuý`/`thúy`, `quí`/`quý`. Old-style vs new-style placement are *different codepoints*. Build an alias map or a tone-position canonicalizer, else valid words get rejected.
3. **Case & whitespace** — lowercase, collapse multiple/NBSP spaces.
4. **Syllable split** — Vietnamese syllables are space-delimited; `split(/\s+/)` is correct. Do NOT use a word-segmenter here.
5. **Used-set keying** — key on the normalized full word.
6. **Encoding** — Viet74K ships Unicode *and* TCVN3/ABC variants; take the Unicode one.

## 5. Dictionary Sources (ranked)

| # | Source | Content | Format | License | Verdict |
|---|---|---|---|---|---|
| 1 | [minhqnd/Noi-Tu-Discord](https://github.com/minhqnd/Noi-Tu-Discord) `src/assets/wordPairs.json` + `customWords.json` | ~60k 2-syllable pairs, purpose-built for nối từ | JSON `{"từ_đầu": ["từ_cuối", ...]}` | MIT | **Start here.** Already the exact index shape; MIT-safe to vendor |
| 2 | [duyet/vietnamese-wordlist](https://github.com/duyet/vietnamese-wordlist) — [Viet74K.txt](https://vietnamese-wordlist.duyet.net/Viet74K.txt) | 74k words, all lengths, dictionary-sorted | plain txt, Unicode + TCVN3 | unclear/aggregated | Expansion set; filter `split(' ').length===2` |
| 3 | [undertheseanlp/dictionary](https://github.com/undertheseanlp/dictionary) | consolidated VN dictionary from the underthesea NLP group | JSON/txt | check repo | Good for definitions / POS filtering (noun+adj only) |
| 4 | [viet-yomitan](https://github.com/onlyduyy/viet-yomitan) | Từ Điển Tiếng Việt Thông Dụng, 42,012 entries | Yomitan dict (JSON in zip) | check | High-quality curated monolingual entries |
| 5 | [Trannosaur/published_dicts](https://github.com/Trannosaur/published_dicts) | vi.wiktionary + en.wiktionary derived, Jan 2026 | JSON | CC BY-SA (Wiktionary) | Attribution required; largest coverage |
| 6 | [vntk/dictionary](https://github.com/vntk/dictionary) | Node package, lookup + examples | npm | check | Runtime lookup, not bulk list |
| 7 | [NNBnh/noi-tu](https://github.com/NNBnh/noi-tu), [lvdat/bot-noi-tu](https://github.com/lvdat/bot-noi-tu) | reference implementations + wordlists | — | check | Cross-check coverage / borrow trap lists |
| 8 | [titoBouzout/Dictionaries](https://github.com/titoBouzout/Dictionaries/blob/master/Vietnamese_vi_VN.txt) | spellcheck syllable list | txt | — | Syllable validation only, not compounds |

**Recommended pipeline**: vendor #1 as base → union with 2-syllable filter of #2 → optionally POS-filter with #3 → dedupe after NFC normalization → emit `words.json` (Set) and `word-index.json` (adjacency). Keep the build script in-repo so the dataset is reproducible.

**Licensing**: MIT (#1) is safe to redistribute with attribution. Wiktionary-derived (#5) is CC BY-SA — attribute and isolate in a clearly-marked file if used.

## 6. Implementation Recommendations

### Suggested layout (stack-agnostic; repo is empty so nothing is imposed yet)
```
data/
  raw/                 # downloaded sources
  words.json           # normalized Set of valid 2-syllable words
  word-index.json      # { firstSyllable: [word, ...] }
scripts/
  build-dictionary.mjs # raw -> normalized artifacts, reproducible
src/
  normalize.js         # NFC, tone-variant canonicalization, split
  dictionary.js        # load, has(), movesFrom()
  game-engine.js       # state, applyMove, validate, win/lose
  bot.js               # difficulty strategies
```

### Minimal engine sketch
```js
export function createGame({ index, words, startWord }) {
  const used = new Set([startWord]);
  let current = startWord.split(' ')[1];
  return {
    play(raw) {
      const w = normalize(raw);
      const s = w.split(' ');
      if (s.length !== 2)   return { ok: false, reason: 'NOT_TWO_SYLLABLES' };
      if (s[0] !== current) return { ok: false, reason: 'WRONG_LINK' };
      if (!words.has(w))    return { ok: false, reason: 'NOT_IN_DICTIONARY' };
      if (used.has(w))      return { ok: false, reason: 'ALREADY_USED' };
      used.add(w); current = s[1];
      return { ok: true, current };
    },
    moves: () => (index[current] ?? []).filter(w => !used.has(w)),
  };
}
```

### Build order
1. `build-dictionary.mjs` + normalization — everything depends on data quality.
2. Engine + unit tests on rules (wrong link, reuse, unknown word, timeout).
3. CLI loop (1 human vs bot).
4. Bot difficulty ladder.
5. UI / multiplayer / timer / scoring if in scope.

## 7. Common Pitfalls
- **Linking on last *letter* instead of last *syllable*** — folk description says "chữ cái cuối", real play links syllables.
- **Skipping NFC** → valid words rejected; unreproducible across OS/keyboards.
- **Ignoring `hoà`/`hòa` tone-placement variants** → biggest source of "my word IS real!" complaints.
- **Dictionary full of 1- and 3+-syllable entries** → filter at build time, not runtime.
- **Bot always plays the killer syllable** → unwinnable; players leave.
- **Reloading a 60k-entry JSON per request** in a server context → load once at boot.
- **No per-session used-set** → infinite `a→b→a→b` loops.
- **Client-side-only validation** in multiplayer → cheatable; validate server-side.

## References
- Rules: [luatchoi.edu.vn/noi-tu](https://www.luatchoi.edu.vn/noi-tu) · [gamevui.vn](https://gamevui.vn/noi-tu-tieng-viet/game) · [hoanghamobile roundup](https://hoanghamobile.com/tin-tuc/noi-tu-online/)
- Live games: [vuanoitu.fun](https://vuanoitu.fun/) · [wordfight.online](https://wordfight.online/) · [App Store: Nối từ tiếng Việt](https://apps.apple.com/vn/app/n%E1%BB%91i-t%E1%BB%AB-ti%E1%BA%BFng-vi%E1%BB%87t/id6449588406?l=vi)
- Implementations: [minhqnd/Noi-Tu-Discord](https://github.com/minhqnd/Noi-Tu-Discord) · [NNBnh/noi-tu](https://github.com/NNBnh/noi-tu) · [lvdat/bot-noi-tu](https://github.com/lvdat/bot-noi-tu)
- Data: [duyet/vietnamese-wordlist](https://github.com/duyet/vietnamese-wordlist) · [Viet74K.txt](https://vietnamese-wordlist.duyet.net/Viet74K.txt) · [undertheseanlp/dictionary](https://github.com/undertheseanlp/dictionary) · [viet-yomitan](https://github.com/onlyduyy/viet-yomitan) · [Trannosaur/published_dicts](https://github.com/Trannosaur/published_dicts) · [vntk/dictionary](https://github.com/vntk/dictionary) · [titoBouzout/Dictionaries](https://github.com/titoBouzout/Dictionaries/blob/master/Vietnamese_vi_VN.txt)
- NLP resources: [vndee/awsome-vietnamese-nlp](https://github.com/vndee/awsome-vietnamese-nlp)

## Next Steps
1. Decide stack + target (CLI, web, Discord bot, mobile) — nothing in repo constrains this yet.
2. Vendor `wordPairs.json` from Noi-Tu-Discord (MIT) into `data/raw/`, write `build-dictionary.mjs`, verify entry count and 2-syllable purity.
3. Implement `normalize.js` with NFC + tone-placement canonicalization; unit-test `hoà/hòa`, `thuý/thúy`, `quí/quý`.
4. Implement engine + rule tests, then a CLI loop before any UI.
5. Add bot ladder once the engine is green.

## Open Questions
1. Target platform: CLI, web app, Discord bot, or mobile?
2. Strict 2-syllable only, or allow ≥2-syllable words?
3. Single-player vs bot, or real-time multiplayer? (multiplayer needs a server + authoritative validation — big architecture delta)
4. Are word definitions in scope? (pushes toward source #3/#4/#5)
5. Should max-difficulty bot be beatable, or is "cruel mode" wanted?
6. Vietnamese-only UI, or bilingual?
