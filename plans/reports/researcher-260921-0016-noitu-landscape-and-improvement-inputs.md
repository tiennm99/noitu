# Research: noitu landscape scan — competitors, dictionary disputes, small-scale server practice

Prior reports (`research-260904-1058-noi-tu-game.md`, `research-2609{07,08,10}-*`) already cover
rules engine, bot AI, Vietnamese normalization, and an exhaustive dictionary-source comparison
(Wiktionary/undertheseanlp/Hoàng Phê). This report does not repeat those; it adds competitor
feature/complaint evidence, dispute-handling patterns, small-scale server practice, and ecosystem
notes.

## Summary

- Direct web competitors exist: `noitu.fun` and `wordfight.online` — both have ranked/Elo modes,
  neither has noitu's spectator-on-elimination or bot-difficulty design.
- The #1 recurring complaint across mobile nối từ apps is dictionary coverage ("từ điển quá ít"),
  not rules or UX — validates prior dictionary-focused work as the highest-leverage area.
- Every competitor with a fixed wordlist ships a manual "report word" channel; none of the ones
  found do live player voting mid-game. One Discord bot layers a separate community-wordlist repo
  on top of a base corpus — closest precedent to an allowlist overlay.
- Ranked/Elo matchmaking with wait-time-based window widening is the standard pattern for small
  pools (chess.com/lichess-style); directly applicable to a 2-4 player room-code game with low CCU.
- SQLite-as-embedded-store (not just read-only dictionary) is a well-documented pattern for adding
  room/stat persistence without a DB service — fits the existing `modernc.org/sqlite` dependency.
- `protovalidate` (Apache-2.0, Go+JS via `protovalidate-es`) is a concrete, license-compatible fit
  for schema-level proto validation, complementing (not replacing) noitu's Vietnamese-specific
  sanitization.
- Card-mechanic "Extended Mode" (skip/reverse/swap turn) and Elo ranked ladders are the clearest
  "features players expect but noitu lacks" signal found.
- Casual house-rule turn timers run 5-10s vs noitu's 30s default — worth noting as a design choice,
  not a defect.
- No changes found to Svelte 5/adapter-static or buf/protobuf-es that are breaking or urgent for
  noitu's current setup.

## 1. Competing implementations — rules, features, complaints

### Direct web competitors
- **noitu.fun** — solo vs AI, "Ranked" mode with a leaderboard, and group/room-code mode (closest
  analog to noitu's own room model). Also ships non-chain minigames under one brand: "Extended
  Mode" adds card mechanics (reverse turn, assign-next-responder, swap opponent's word, skip turn),
  plus a trivia-style "Character Arena" and a Wheel-of-Fortune-style "Letter Assembly" mode. Rules
  text explicitly bans slang/shorthand and misspelled tone marks. [noitu.fun]
- **wordfight.online** — 3 modes: English word chain (shows IPA + Vietnamese gloss per word),
  Vietnamese word chain (2-syllable, same rule as noitu), and a separate "King of Vietnamese"
  word-puzzle mode. Turn timer 10-30s. **Elo-based leaderboard with skill-matched opponents**,
  progressive difficulty across up to 999 levels, 3-star mastery rating per level, replay-resistant
  randomized word lists, private shareable room links. [wordfight.online]
- Neither site documents spectator viewing of eliminated players, reconnect grace windows, or
  per-seat colored room chat — these look like noitu differentiators, not table stakes to add.

### Discord bots (adjacent platform, same game)
- **minhqnd/Noi-Tu-Discord** ("Moi Nối Từ") — bot-vs-player and PvP-with-bot-as-referee modes, DM
  play, `/leaderboard` and `/stats` (streak, personal record, wins), `/tratu` dictionary lookup
  backed by 357k+ definitions via `dict.minhqnd.com`, emoji-reaction feedback per submission
  (✅ correct / ❌ can't chain / 🔴 duplicate / ⚠️ format error). [github.com/minhqnd/Noi-Tu-Discord]
- **lvdat/bot-noi-tu** ("RaHub") — dictionary sourced from `undertheseanlp/dictionary` plus a
  **separate community-contribution repo** (`phobo-contribute-words`) merged in at build/runtime.
  This is the clearest real-world precedent for a base-corpus + community-overlay split (see §2).
  [github.com/lvdat/bot-noi-tu]

### Mobile apps — features and complaints
- Galaxy Team "Nối từ - Word Chain": Survival / Time-Limited(3min) / Level-Challenge-vs-AI modes.
  [play.google.com/.../com.galaxteam.wordchain]
- "Nối từ tiếng Việt" (iOS, MWM/id6449588406): 3 modes — Challenge (timed rounds + score
  threshold), Duel (2p turn-based), Arena (4p elimination — same shape as noitu's 2-4 elimination
  room), Game Center leaderboard integration, scoring by chain-word character count. **Rating 1.2/5
  (19 reviews)**, dominant complaint is a too-small dictionary ("từ điển quá ít") rejecting valid
  words and occasionally accepting non-Vietnamese junk; developer response cites Vietnamese's
  richness as an excuse rather than fixing it. [apps.apple.com/vn/.../id6449588406]
- Cross-app pattern found via search: apps expose an in-app "Report Error" / "báo lỗi" flow for a
  rejected word rather than any live dispute; devs publicly acknowledge coverage gaps as
  unavoidable rather than committing to fixes. [search results, ktcc.blog context]

### Rule-variant survey (Vietnamese how-to-play content)
- Casual/offline house rules commonly run **5-10s per turn** (ktcc.blog), notably faster than
  noitu's 30s default — a deliberate design choice for noitu given typed Vietnamese input
  (Telex/VNI composition), not evidence of a gap.
- "Từ hiểm" (dead-end/trap words — syllables that start almost nothing) is a named, well-understood
  strategic concept across the community, not something apps hide. This affirms noitu's design of
  showing the leftover words in a dead-end position rather than treating it as a bug.
- Recurring dispute across communities: "từ ghép" (compound) vs "từ đơn" (single-syllable) and
  which authority resolves disputes; common advice is to pre-agree a single reference dictionary
  (often citing the Institute of Linguistics / Hoàng Phê) as sole arbiter — already covered in
  `research-260910-0939-hoang-phe-in-noitu.md`.

### Implications for noitu
- Dictionary coverage is the make-or-break axis for player satisfaction industry-wide; any
  roadmap item competing for effort against dictionary work should clear a high bar.
- A lightweight "report this rejection" action (word + turn context) is standard and cheap; noitu
  has no such flow today per the README. Low-risk, high-precedent addition.
- Elo/ranked ladder and light "chaos" mechanics (skip/reverse/swap) are the two concrete feature
  gaps versus the nearest web competitors, if PvP breadth is a goal.

## 2. Dictionary disputes — patterns beyond corpus choice

(Corpus/source comparison already exhaustively covered in prior reports; this is new: process
patterns for *handling disputes at runtime*, and licensing-separation precedent.)

- **Static base + community overlay repo** (lvdat/bot-noi-tu + phobo-contribute-words): base
  dictionary from a versioned upstream corpus, disputed/missing words tracked in a *separate*
  repository merged at build or load time. Mirrors an allowlist-overlay design: keeps the
  overlay's provenance and license distinct from the base corpus, which matters for noitu's
  Apache-2.0-code / CC BY-SA-4.0-data split — an overlay of player-submitted words would need its
  own license decision (CC BY-SA if derived/mixed with Wiktionary text, or a fresh license if pure
  word-list-no-definition additions, which are likely uncopyrightable facts). [github.com/lvdat]
- **Scrabble's live challenge**: any player may challenge a just-played word before the next turn;
  in double-challenge scoring the *loser* of the challenge (wrong challenger or wrong player) loses
  their turn — a real cost that discourages frivolous challenges. [en.wikipedia.org/Challenge_(Scrabble)]
- **Words With Friends' approach**: no live human challenge at all — the client just keeps
  resubmitting until the server's own dictionary accepts something; the dictionary itself (Zynga's
  ENABLE-derived ~173k word list, deliberately different from tournament Scrabble lists) is the
  sole and silent arbiter. [wordfinder.yourdictionary.com; word.tips]
- No evidence found of any nối từ implementation running **live in-match player voting** on a
  disputed word (i.e., other seated players vote accept/reject before the turn resolves). The
  Scrabble-style post-hoc challenge with a real cost, or WWF's silent-server-arbiter, are the two
  patterns actually used in the wild — an async "report → maintainer/community review → next
  dictionary build" queue (closer to WWF, deferred) is simpler to implement correctly than a live
  vote and has no real-time consistency/latency problem to solve.

### Implications for noitu
- If a self-serve "report word" pipeline is ever built, model it as WWF/lvdat's deferred pattern
  (report now, reviewed and merged into a future `noitu.db` build) rather than live voting — no
  new consensus/anti-brigading problem, and it fits the existing single-file, rebuilt-not-mutated
  dictionary architecture.
- Any player-submitted word overlay must get its own explicit license decision before merging with
  the CC BY-SA data tree; do not assume submitted words inherit Wiktionary's license by default —
  bare word forms without wiktionary-derived definitions are likely factual/uncopyrightable, but a
  submission that carries a definition text is a new derivative work needing its own care.

## 3. Small-scale realtime game server practice

- **Matchmaking for small pools**: standard pattern (chess.com/lichess-style, per Awesomenauts
  postmortem) is `window = base_window + widen_rate × seconds_waited`, anchored on whichever queued
  player has waited longest, capped at a max window — trades match quality for queue time as the
  pool thins out. Directly portable to a "quick match" queue for noitu's current room-code-only
  online mode. [joostdevblog.blogspot.com; medium.com/@deephavendatalabs]
- Batch pooling ("gather for ~1-2 min then match everyone at once") is the standard fallback for
  genuinely tiny regional pools, cited as improving match quality up to ~300 concurrent queuers —
  likely overkill for noitu's expected scale, worth knowing only if online play grows.
- **Persistence without a DB service**: SQLite is a well-documented fit for exactly this
  ("zero-configuration... ideal for small to medium services," pure-Go `modernc.org/sqlite` needs
  no CGO, single static binary). Patterns seen: JSON-blob-per-row for full game/room state
  (simplest, fine at noitu's scale), or row-per-update table if write volume grows. A SQLite
  changelog-table + trigger + Go worker pattern exists for push-driven updates but is unnecessary
  complexity for a single authoritative in-process game engine like noitu's. noitu already
  depends on `modernc.org/sqlite` for the dictionary — reusing it (a second read-write DB file, or
  a second schema in-process) for room/series-stat persistence across restarts costs no new
  dependency. [oneuptime.com/.../sqlite-go; gist.github.com/rusco]
- **Rate limiting / abuse**: `golang.org/x/time/rate`'s token-bucket `Limiter` (Allow/Wait/Reserve)
  is the standing idiomatic choice for per-connection message throttling in a Go WebSocket server;
  described as covering "90% of cases" without needing Redis-backed distributed limiting, which is
  irrelevant to a single-binary deployment. Combine with a concurrent-connection cap per IP.
  [dev.to/lovestaco; oneuptime.com/.../websocket-rate-limiting]
- **Anonymous vs identity**: industry best practice (PlayFab, AWS Games Industry Lens) is
  zero-friction anonymous login by default, with an *optional* upgrade path to a recoverable
  identity later — matches noitu's current no-account, nickname-only model; nothing here argues
  for forcing accounts. Reconnect best practice: identity (not the transient socket/session token)
  is the thing that says two connections are the same player, and the server-issued session token
  should stay stable across a reconnect within the grace window — consistent with noitu's existing
  disconnect-grace-window design per its README. [learn.microsoft.com/playfab; docs.aws.amazon.com]
- **Observability**: not surfaced as a distinct concern in results beyond generic Prometheus/metrics
  advice seen in one unrelated Go arena-server repo (`nguyenbatam/arena_game_server`, Redis-backed,
  10k-CCU target) — that project's scale and dependency footprint (Redis, k8s) is not a fit for
  noitu's single-binary constraint; flagging only because Prometheus text-format `/metrics` next to
  the existing `/healthz` endpoint is a low-cost, dependency-light addition if observability becomes
  a goal.

### Implications for noitu
- A quick-match queue (as opposed to room-code-only) is implementable with the widen-by-wait-time
  formula and no new infrastructure; it's a pure in-memory queue in the existing Go process.
- Room/series persistence across restarts (currently implied in-memory, since README describes
  rooms closing after 10 idle minutes with no persistence mention) could reuse the SQLite
  dependency already in the binary rather than adding Redis/Postgres.
- `x/time/rate` per-connection + a connection-count cap per IP is the standard, low-effort answer
  to WS abuse; no evidence any competitor does more than this at this scale.

## 4. Ecosystem notes (brief)

- **SvelteKit/Svelte 5 + adapter-static**: no breaking change found for 2026. One caution
  surfaced repeatedly in 2026 guides: module-scope runes and `$derived` wrapping a store's `$`
  auto-subscription can break specifically under prerendering/hydration; recommended fix is
  keeping runes component-local and bridging global stores explicitly via `$effect`. Only relevant
  if noitu's frontend uses module-level `$state`/`$derived` for anything beyond the documented
  client-owned state (theme, personal best, input box) — worth a quick grep, not a redesign.
  [svelte.dev/docs/kit/adapter-static; khromov.se]
- **buf/protobuf-es**: `protovalidate` (Apache-2.0) plus `protovalidate-es` gives schema-level
  field constraints (e.g. string length/pattern) enforced identically from the same `.proto` file
  on both the Go and JS generated types — same "one wire contract" philosophy noitu already uses
  for message shape. Could formalize bounds already enforced by hand (e.g. nickname length) as
  proto annotations instead of duplicated Go+JS logic — but noitu's nickname sanitization (control
  chars, whitespace collapse, combining-mark cap) is Vietnamese-text-specific logic CEL/protovalidate
  doesn't replace; treat it as a complement for simple bounds, not a replacement for sanitization.
  [github.com/bufbuild/protovalidate]
- No 2026 buf CLI or `protoc-gen-es`/`@bufbuild/protobuf` breaking-change advisory found that
  affects noitu's current generation setup.

## Unresolved questions

- Does noitu currently persist rooms/series scores across a server restart at all, or is
  everything in-memory? README doesn't say; determines whether §3's SQLite-persistence point is
  a new feature or filling a known gap.
- Would a "report rejected word" flow be worth the review-queue maintenance cost given the project
  has one apparent maintainer, versus just improving corpus coverage directly (already the subject
  of 5 prior reports)?
- Is competitive/ranked PvP actually a goal for noitu, or is vs-bot + casual room-code play the
  intended scope? The Elo/quick-match findings only pay off if ranked play is in scope.
- Could not verify `phobo-contribute-words`' actual review/merge workflow or license (GitHub
  fetch returned only nav chrome, no README text) — the "allowlist overlay" characterization is
  inferred from the bot's own README description, not confirmed from that repo directly.

Status: DONE
Summary: Competitor scan of noitu.fun, wordfight.online, and 4 Discord/mobile nối từ implementations plus small-scale server-practice research (matchmaking, SQLite persistence, rate limiting) written to the report; dictionary coverage confirmed as the industry's #1 complaint, and a base+overlay community-wordlist precedent and protovalidate found as new, concrete inputs.
Concerns: none blocking; one item (phobo-contribute-words internals) unverified due to GitHub README not rendering through WebFetch — noted in Unresolved questions.
