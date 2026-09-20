---
title: "noitu — improvement directions"
date: 2026-09-21
type: brainstorm
status: advisory, read-only
scope: gameplay/bot, product, UX, dictionary, architecture/ops, DX, docs
---

# noitu — improvement directions

Method: read `README.md`, `docs/deployment.md`, `proto/noitu/v1/game.proto`, `server/internal/{game,bot,dictionary,vietnamese,wsapi}`, `server/cmd/{noitu-server,build-dictionary}`, `web/src` (stores, routes, components, `i18n/vi.js`), `Makefile`, `Dockerfile`, both CI workflows, the three 2026-09-10 UI/UX reports, the cleanup review, and every journal tail. Everything below cites a file I read. Speculation is labelled.

## Baseline: what is already true (so nothing here repeats it)

The three 2026-09-10 UI/UX reports are **mostly delivered** — verified in the tree, not assumed:

- Tokens: `--border-strong`, `--accent: #12692f`, `--space-1..8`, `--text-1..9`, `--radius-pill` all exist in `web/src/app.css:1-130`. The three AA failures are closed.
- Clock: `mine` / `stalled` / `spoken` derivations in `CountdownRing.svelte:14-51` — the false-panic red on the bot's turn, the stalled-socket clock and the screen-reader time marks are all done.
- `interactive-widget=resizes-content` in `app.html:13-16`; skip link at `+layout.svelte:18`; per-route `<svelte:head>` on all three routes; `reconnectNow()` at `ws/connection.svelte.js:53`; clipboard fallback plus visible invite URL at `RoomCodePanel.svelte:50,104`; `unready: 'Bỏ sẵn sàng'` (`vi.js:120`); lobby-local error block (`Lobby.svelte:192-203`); invite-path nickname gate (`online/+page.svelte:93,378`); `RoomCodePanel compact` kept in the post-game lobby (`Lobby.svelte:88-94`).

Two findings from those reports are **still open** and are folded into C1 below: the chat toggle still sits under the whole chain (`online/+page.svelte:362` renders `ChatPanel` after `GameBoard`), and it is only collapsible while `playing && !wide`, so lobby chat has no unread badge.

The cleanup review (`code-review-260908-2302`) is fully applied; `deadcode`/`staticcheck` were clean at `f00d0ef` and nothing since has added a `TODO`.

---

## A. Gameplay, rules and bot

### A1. The bot plays to survive; the human plays for points

**Problem.** `game/engine.go:265-307` prices a word on chain length, syllable count, speed and link rarity. The bot never sees any of it: `bot.Board` (`bot/bot.go:50-55`) exposes only `LegalMoves`, `Used`, `WordsStartingWith`, `LastSyllable`, and `hard.Choose` maximises "how few replies the opponent has" (`strategy_hard.go:137-166`). So Hard plays a *strangling* game while the player is scored on a *fluent* one. The ladder is also coarse — three constants (`hardKillRate 0.85`, `searchDepth 4`, `branchCap 12`) and one latency test (`realcorpus_test.go:149-202`, worst case must stay under 150ms).

**Options.** (a) Retune the three constants and add a fourth "Chuyên gia" difficulty at higher depth — cheapest, but the fourth tier is the same personality with more nodes. (b) Give the search a small points term, so tighter *and* longer/rarer words are preferred; the bot then reads as a player rather than a trap-setter, and its final score becomes comparable to yours. (c) Adaptive difficulty that widens `branchCap`/`searchDepth` from the player's recent results — hides what the difficulty picker means and makes bug reports irreproducible.

**Rec.** (b), then (a) if the ladder still feels flat. Keep `Board` narrow — add `OutDegree(syllable)` only, and score inside the strategy; do not hand the strategy the Engine. **Size S-M.** **Depends on:** nothing.

### A2. Points are a number with no visible cause

**Problem.** `PlayedWord.points` (`game.proto:164-178`) is a single integer. Four terms went into it and the player sees none of them, so the one mechanic that rewards reaching for a three-syllable compound or a rare link teaches nothing. The client cannot re-derive it — it has no wordlist, by design.

**Options.** (a) Additive breakdown on `PlayedWord` (four small fields, or a `repeated PointPart{kind, value}`) plus a one-line "+15 hiếm" in the chain row. (b) A static rules/scoring screen in `vi.js` — no protocol change, but it is a table nobody reads mid-game. (c) Leave it.

**Rec.** (a) with a `repeated PointPart`, plus a short scoring paragraph on the help screen from C2. A repeated message means a future fifth term is not a wire break. **Size S.** **Depends on:** proto regen (routine here).

### A3. A dead end still costs the trapped player 30 seconds of nothing

**Problem.** `engine.go:250-258` deliberately leaves a dead end to the clock: the player handed an unanswerable syllable must time out. That is the right *rule* — but they sit through `NOITU_TURN_LIMIT` (30s) with nothing to do, and only afterwards learn from `PlayerEliminated.suggestions` that the position was empty. The engine knows instantly (`HasLegalMove`, `:340-347`); the bot gets to use that shortcut (`NoMove`, `:373-379`) and the human does not.

**Options.** (a) A `ClaimNoMove` client message: the server answers truthfully — if the position is dead, eliminate immediately with `EndNoLegalMove`; if not, the claim was wrong and costs the remaining speed bonus, or nothing at all. (b) Auto-resolve: the server settles a dead end without asking, which removes the "did I miss something?" moment that makes the game interesting. (c) Leave it; `Resign` exists, and `session.go:447-456` restricts it to the player to act, which is exactly the right shape to copy.

**Rec.** (a), modelled on `Resign`'s authorization. Converts up to 30s of dead air per elimination into one tap, and cannot be abused because the server verifies the claim. **Size S.** **Depends on:** proto addition.

### A4. One ruleset, one clock, no room options

**Problem.** `NOITU_TURN_LIMIT` is deployment-wide (`main.go:113`) and `RoomState` (`game.proto:343-366`) carries `max_players`/`min_players`/`grace_ms` but no game options. A room of four experienced players and a room of two beginners get the same 30s. `vietnamese.MinSyllables = 2` is a compile-time constant.

**Options.** (a) Nothing — one clock is one code path, and the README leans on that. (b) A `GameOptions` message on `CreateRoom`/`StartGame`, server-clamped, echoed in `RoomState` so the lobby draws what the server allows — the same trick `max_players` already uses. (c) Presets (chậm/thường/nhanh), which is (b) with the knobs hidden.

**Rec.** (c) — three presets, turn limit alone. Clamping an enum server-side is one switch; a free integer is a validation surface and an invitation to a 3-second room. **Size S-M.** **Depends on:** proto; keep the bot path on the same constant so "one implementation" survives.

---

## B. Product: progression, social, discovery

### B1. A room can only be reached by a code somebody already gave you

**Problem.** `hub.joinRoom` (`hub.go:126-137`) resolves a 6-character code and nothing else. There is no queue, no listing, no "play someone now". A first-time visitor with no friend online can only play the bot. That is the biggest ceiling on the online half of the product.

**Options.** (a) Quick-match: one in-memory FIFO in the hub keyed by nothing at all; two waiters become a room and the existing `createRoom`/`joinRoom` path does the rest. No moderation surface, no listing to abuse. (b) A public room list — needs a name, a listing message, and immediately raises "who can see my room". (c) Invite-by-link only, as today.

**Rec.** (a). It reuses the whole room machinery and adds one map plus a timeout; the client needs one button and one waiting state. **Size M.** **Depends on:** nothing. Second-order: a lone waiter must be offered the bot after N seconds, or quick-match becomes a dead end of its own.

### B2. Identity lasts exactly as long as a seat

**Problem.** `PlayerSlot.wins` resets when the seat is vacated (`game.proto:208-213`), nicknames live in `localStorage` (`settings.svelte.js:98,110-114`), and the solo record is a per-difficulty number in the same place (`:98,141-155`). Nothing survives a room, a device change or a restart — `docs/deployment.md:136-152` states this as a design position.

**Options.** (a) Keep it: honest, zero ops, and the game is fine as a drop-in. (b) An anonymous device token minted server-side plus a small read-write SQLite holding profile and aggregate stats, kept strictly separate from the read-only CC BY-SA dictionary file (licence hygiene matters — `dictionary.Open` opens `mode=ro` and closes the handle, `store.go:88-97`). (c) Real accounts/OAuth — a login wall on a casual word game.

**Rec.** (a) until D1 and D2 land. Progression without observability is a feature you cannot tell is working. When it comes, (b): two files, two licences, one binary. **Size L.** **Depends on:** D1.

### B3. A room turns away everyone once a game starts

**Problem.** `room.go:571` refuses a joiner with `game_in_progress` even when seats are free, and `README.md:83-86` defends it — correctly, for *players*. But an eliminated player already watches (`GameBoard.svelte:155`, `vi.js:140` `spectating`), so the rendering path for a non-acting participant exists. A friend who arrives two minutes late is simply told no.

**Options.** (a) Spectator seats: join as an observer, receive `TurnUpdate`/`ChatMessage`, seated in the lobby when the game ends. (b) A waiting list: the joiner is held and seated at the lobby return. (c) Leave it.

**Rec.** (b) first — no new per-recipient rendering, no spectator concept in `PlayerSlot`; the joiner waits in a pending list the room drains on lobby return. (a) is the better product and roughly triple the work. **Size M.** **Depends on:** proto (a pending/waiting flag in `RoomState`).

### B4. Solo has no reason to come back tomorrow

**Problem.** The only solo progression is `bestScores` per difficulty (`settings.svelte.js:141-155`), and every game opens on `RandomOpeningWord` (`store.go:409-418`), so no two runs are comparable and none is shareable.

**Options.** (a) A daily seeded solo challenge: the server derives the opening word and the bot RNG seed from the date, everyone gets the same board, and the transcript export (`lib/history-export.js`) becomes a shareable result. Fits the philosophy exactly — server-authoritative, nothing extra in the client. (b) Streaks/achievements — needs B2's persistence. (c) Nothing.

**Rec.** (a), without a leaderboard in v1. A shared board plus a shareable transcript is most of the loop; a leaderboard needs identity and anti-cheat the current model does not support. **Size M.** **Depends on:** a deterministic RNG path into `bot.New` (already injectable, `bot.go:70`) and a date-seeded opening pick.

---

## C. UX

### C1. What is left over from the 2026-09-10 reports

**Problem.** Two findings survive (verified above): the chat toggle and its unread badge sit below `ChainHistory`, which grows a row per turn (`online/+page.svelte:362`), so on a phone the badge is effectively unreachable mid-game; and `collapsible={playing && !wide}` means the lobby panel is never collapsible, so lobby chat has no badge at all while the log itself is below the fold.

**Options.** (a) Hoist a small unread pill into `GameBoard`'s `.top` row beside the connection badge, and drop `playing &&` from `collapsible`. (b) Reorder `.pane.talk` before `ChainHistory` in the stacked layout via `order` — puts the conversation above the game, which is wrong on a phone. (c) Leave it.

**Rec.** (a), both halves. **Size S.** **Depends on:** nothing.

### C2. Nothing anywhere states the rules

**Problem.** The landing page is a tagline, a nickname field, a difficulty picker and two buttons (`routes/+page.svelte:19-31`); `vi.js:10` is `'Trò chơi nối từ tiếng Việt'`. Grep finds no rules copy, no help screen, no scoring explanation anywhere in `web/src`. A player who does not already know *nối từ* learns the two-syllable minimum by having a word rejected, and learns scoring never.

**Options.** (a) A short `/help` route: the chain rule, the 2-syllable minimum, the clock, elimination, and the scoring terms from A2 — copy in `vi.js` plus one route. (b) A first-turn inline coach on `/play` — better conversion, more state, easy to get in the way on the second game. (c) A README link in the footer, which already carries the attribution (`AttributionFooter.svelte`).

**Rec.** (a), plus one line of rule text under the syllable on the *first* turn only. **Size S.** **Depends on:** A2 if scoring is to be explained truthfully.

### C3. "Không tìm thấy từ này trong từ điển" is a dead end on a near miss

**Problem.** `REJECT_REASON_NOT_IN_DICTIONARY` (`game.proto:37`) comes back whenever `dict.Resolve` fails (`engine.go:212-215`). In Vietnamese a miss is very often one tone mark. The builder already generates an alias table for known spelling variants (`build-dictionary/main.go:327-355`) and `Resolve` consults it (`store.go:345-353`), but anything outside that table is a flat refusal. The UI now shows the rejected word back (the 2026-09-10 P2-3 fix), which helps the eye and not the vocabulary.

**Options.** (a) Server-side diacritic-insensitive near-match: strip marks (`norm.NFD` then drop `Mn`, the technique already in `build-dictionary/filter.go:83-99`), and when exactly one real word matches the stripped form, say "ý bạn là …?" without accepting the move. (b) Full edit-distance suggestions — a hint engine that hands out words the player did not know, which changes the game. (c) Nothing.

**Rec.** (a), restricted to *diacritics only* and to a *unique* match. It corrects typing, not vocabulary. Needs a second index in `Store` populated at `Open` (a `map[stripped][]word`, a few MB), not a scan. **Size S-M.** **Depends on:** proto (a `suggestion` field on `MoveRejected`).

---

## D. Architecture and operability

### D1. Nothing persists, and the restart cost is a product decision, not just an ops one

**Problem.** Rooms are in-memory maps on the hub (`hub.go:45-50`); `docs/deployment.md:136-140` says a restart ends every live game and tells players so (`hub.shutdown`, `hub.go:213-231` — genuinely good behaviour). But it means you cannot deploy during the evening, and every feature in section B is blocked behind it.

**Options.** (a) Keep it and add a *drain* mode: stop accepting new rooms, let live ones finish (already bounded by the turn clock), then exit — turning "deploy when quiet" into "deploy whenever". (b) Snapshot rooms to disk and restore: engine state is fully copyable (`Snapshot`, `engine.go:521-546`) but sockets are not, so restore means every client re-handshaking into a restored room; large and subtle. (c) A read-write SQLite for durable data only (profiles, stats, disputes), live rooms staying in memory.

**Rec.** (a) now, (c) when B2/E3 need it, (b) probably never. Drain is one flag on the hub plus a `/readyz` that goes unhealthy while draining. **Size S** for (a), **M** for (c). **Depends on:** nothing.

### D2. You cannot answer a single question about how the game is actually played

**Problem.** Twelve `slog` call sites in the whole `wsapi` package (4 in `room.go`, 6 in `session.go`, 2 in `server.go`), no metrics, no `/metrics`, no expvar, no game-event stream. Unanswerable today: how many rooms are live, how often a word is rejected and for which reason, which syllables end games, how long games last, and — the important one — **which words players type that the dictionary does not have**. That last question is the input to every decision in section E.

**Options.** (a) `expvar` counters plus `GET /debug/vars`: stdlib, zero dependencies, matching the project's dependency discipline (`server/go.mod` is tiny). (b) A Prometheus client and `/metrics`: better tooling, one dependency and a scrape endpoint to protect. (c) A structured `slog` event per terminal game event and per rejection, read from container logs.

**Rec.** (a) for counters and (c) for the rejection stream — specifically one `slog.Info("word_rejected", "reason", …, "word", …)` line, which costs nothing and is the corpus feedback loop. Gate (b) on actually having a Prometheus. **Size S.** **Depends on:** nothing. Second-order: a typed word is user content — cap the length, log only the normalized form, and say so in the deployment doc.

### D3. Behind a proxy, one abuser can lock every player out of joining

**Problem.** `clientIP` uses `RemoteAddr` and deliberately ignores `X-Forwarded-For` (`server.go:150-162`), and `docs/deployment.md:113-119` states the consequence plainly: behind a reverse proxy — the *supported* deployment — every player shares one bucket at `joinsPerSecond = 1, joinBurst = 5` (`session.go:49-50`). One script drains the shared bucket and everybody else gets `too_many_attempts`. The reasoning for not trusting XFF is right; the resulting default is not safe in the supported topology.

**Options.** (a) `NOITU_TRUSTED_PROXY_CIDRS`: when `RemoteAddr` falls inside it, take the right-most XFF hop; otherwise `RemoteAddr` as today. Explicit, unset by default, no behaviour change for direct deploys. (b) A per-room failed-join counter — defends the room-code secret but not the shared bucket. (c) Leave it and raise the limit, trading a lockout for a weaker brute-force defence on a 6-character code.

**Rec.** (a) and (b) together; they defend different things, and (a) restores the limiter's stated purpose. **Size S.** **Depends on:** nothing.

### D4. Operational hygiene: no version stamp, no readiness, no capacity number

**Problem.** `/healthz` returns `"ok"` (`server.go:55-58`) and `docs/deployment.md:122-132` correctly calls it liveness-only; the binary carries no version stamp (no `-ldflags -X` in the `Dockerfile` or `Makefile`), so "is the new image live?" cannot be answered without playing a game; and nothing states how many concurrent rooms one binary handles — each room is a goroutine plus an engine plus timers (`room.go:326-486`) and the dictionary is ~7MB of shared maps (`store.go:8-14`), so the number is probably large, but it is unmeasured (speculation).

**Options.** (a) `/readyz` returning JSON with version, dictionary word count and live-room count, version injected at link time. (b) The same fields on `/healthz` — breaks the liveness contract by giving it a new way to fail. (c) A Go load harness (N concurrent bot games against the real corpus), run by hand, with the number recorded in `docs/deployment.md`.

**Rec.** (a) and (c). Both small; both turn deploy-day guesses into facts. **Size S.** **Depends on:** D1(a) for the draining state.

---

## E. Dictionary quality and word disputes

### E1. The corpus is thinner than it has been, and the journals said to watch for it

**Problem.** The shipped corpus is viwiktionary-only: `research-260908-1529` measured **36,200** accepted words from the dump against **61,026** for the undertheseanlp corpus and **64,110** for the union; the last kaikki-derived database was 34,813 words (`research-260910-0939`). `--min-words` defaults to 30,000 (`build-dictionary/main.go:68`). The journal for the switch ends with "Watch real play for thinness; the viwiktionary dump is the upgrade path, not GPL data". Nobody has watched, because of D2.

**Options.** (a) Union the dump with a second permissively-licensed list as an extra `--words` input, pinned by dated URL and checksum — the research report's own recommendation; licence compatibility must be re-checked per source. (b) A small curated additions/removals list kept in-repo (our own text, Apache-2.0, no upstream data) applied after the dump pass: cheap, targeted, and it keeps the two licence regimes separate as `NOTICE` requires. (c) Stay single-source.

**Rec.** (b) now — it is the mechanism that lets E3's disputes turn into fixes at all — and (a) once D2 says which words are actually missing. Doing (a) blind adds 28k words of unknown quality to a game whose problem may be the opposite (E2). **Size M.** **Depends on:** D2 for evidence, E3 for input.

### E2. Every Wiktionary entry is equally playable, including the ones nobody knows

**Problem.** `build-dictionary/filter.go:30-67` accepts any entry that is 2+ syllables, digit-free, punctuation-free and spelled from the Vietnamese alphabet. No frequency, no register, no proper-noun rule — and `research-260908-1529` showed the dump's proper-noun labels are *worse* than capitalisation as a filter. The bot searches exactly the graph the player is validated against (`bot.Board` over `WordsStartingWith`), so Hard can and does win with entries a native speaker has never met, which reads as cheating rather than as losing.

**Options.** (a) A `common` flag per word from a frequency list: validation accepts everything, the *bot* plays only common words, and `Suggestions` shows only common ones. Player-facing fairness without narrowing what a human may play. (b) Drop rare words entirely — narrows the game and throws away the corpus's long tail. (c) Nothing.

**Rec.** (a). Highest-value dictionary change available, and it takes no word away from a player. Needs a Vietnamese frequency source with a compatible licence — the open question for the research agent. **Size M-L.** **Depends on:** that list; a `words.common` column; one `Dictionary` method the bot's `Board` can see.

### E3. A player who is right has nowhere to say so

**Problem.** No dispute path exists anywhere — grep finds nothing in `server/` or `web/src`. A real word rejected as `NOT_IN_DICTIONARY` is simply lost, and the corpus journal records 1,138 words still carrying no meaning because the stripper does not know the `*form of` template family.

**Options.** (a) A `ReportWord` client message; the server records it (a log line via D2, or a row once D1(c) exists) and answers with a confirmation. (b) A prefilled GitHub issue link in the rejection line — zero server work, and it asks a casual player to have a GitHub account. (c) Nothing.

**Rec.** (a), logging only in v1, triaged by hand into E1(b)'s curated list. The full loop — report, triage, ship — is what makes the dictionary improve at all; every other option leaves it improving only when Wikimedia does. **Size S.** **Depends on:** D2 (where the report lands), E1(b) (where the fix lands), proto.

### E4. The dictionary cannot be rebuilt, only re-derived

**Problem.** `Makefile:10` and the `Dockerfile` both fetch `viwiktionary/latest/`, deliberately unpinned; `meta.source_sha256` identifies the bytes after the fact (`build-dictionary/main.go:508-519`) but there is no way to obtain those bytes again — Wikimedia repoints `latest/` monthly. So a regression traced to a corpus change cannot be reproduced or rolled back, and two journals name exactly this fallback ("keep the fetched file as a release asset") without having taken it.

**Options.** (a) Attach the derived `noitu.db` (a few MB) to each GitHub release and let a run consume it via `NOITU_DB_PATH`; rollback becomes redeploying an older image, which already carries its own copy. (b) Pin a dated dump URL and bump deliberately — contradicts the project's stated freshness preference and the house "moving tag over exact pin" rule. (c) Nothing.

**Rec.** (a). It keeps the moving-`latest` behaviour and makes the *output* recoverable, which is the property actually wanted. Ship `data/LICENSE` and `data/ATTRIBUTION.md` with the asset — CI already asserts this for the image (`ci.yml`, "The licence travels with the data"), and a release asset is distribution too. **Size S.** **Depends on:** a release workflow step.

---

## F. Developer experience

### F1. No linter on the JavaScript side

**Problem.** `web/package.json` has `svelte-check` (which does read the JSDoc types through `jsconfig.json`, so typing is genuinely covered) but no ESLint and no formatter check. House rules call for JavaScript + ESLint + JSDoc. Style consistency rests on discipline alone.

**Options.** (a) `eslint` with the Svelte and JSDoc plugins, wired into `npm run check` and CI. (b) A formatter check only. (c) Nothing.

**Rec.** (a) with a deliberately small rule set — the codebase is already clean, so a maximal config buys churn. **Size S.** **Depends on:** nothing.

### F2. The most valuable end-to-end coverage can only run in CI

**Problem.** `playwright.config.js` is Chromium-only, `workers: 1`, `fullyParallel: false`, 60s timeout; `e2e/pvp-game.spec.js` is 746 lines driving two-to-four browser contexts. Two journals record Chromium failing to install locally, and this workspace cannot run a browser at all (`CLAUDE.md`: none installed, no ARM64 builds, no root). Meanwhile `wsapi_test.go` is 2,292 lines of in-process multi-client coverage that runs everywhere in seconds.

**Options.** (a) Accept: e2e is a CI-only gate. (b) Move the multi-client *protocol* assertions (elimination order, grace expiry, owner handover, chat scoping) down into the Go suite where they already mostly live, and shrink Playwright to one smoke path per mode — faster signal, runnable locally, less flake (a four-player join flake is named in a journal). (c) Add a non-Chromium fallback — does not help; the constraint is the machine, not the engine.

**Rec.** (b). Keep e2e for what only a browser proves: IME composition, focus and keyboard behaviour, and the reconnect path through a real socket. **Size M.** **Depends on:** nothing.

### F3. Untrusted-input boundaries have no fuzz coverage; the proto workflow has one pin worth revisiting

**Problem.** Three functions parse hostile bytes — `Decode` (`codec.go`, capped at 4096 bytes), `sanitizeNickname` (`nickname.go`) and `vietnamese.Normalize` (`normalize.go:37-49`, shared by the corpus builder *and* the server, so a divergence silently unmatches the corpus). All are table-tested; none is fuzzed. Separately, `proto.yml` pins `buf` to exactly `1.69.0` against the house preference for a moving major tag, and there is no `buf format` check beside `buf lint`.

**Options.** (a) Three `FuzzXxx` functions seeded from the existing tables, run in CI for a bounded time. (b) A property test that `Normalize` is idempotent and agrees across both callers on the whole corpus. (c) Nothing.

**Rec.** (a) and (b) — both small, both guarding the one invariant the README stakes the game on. Move `buf` to a `1.x` tag and add `buf format --diff --exit-code`. **Size S.** **Depends on:** nothing.

### F4. Nothing watches the dependencies

**Problem.** `.github/` contains `workflows/` and nothing else — no Dependabot, no Renovate. `web/package.json` uses carets (fine) and `server/go.mod` pins exactly (as Go does), but nothing prompts an upgrade and nothing reports a CVE. The image is distroless and static, which limits the surface without removing it.

**Options.** (a) A `dependabot.yml` covering gomod, npm, GitHub Actions and Docker — four ecosystems, one file. (b) Renovate: better grouping, an app to install. (c) Manual.

**Rec.** (a). **Size S.** **Depends on:** nothing.

---

## G. Documentation

### G1. The README carries the architecture, the rules, the ops and the rationale

**Problem.** `README.md` is 316 lines and the best-written thing in the repo, but it now holds the rules, the online-play model, the frontend rationale, setup, configuration, every `make` target, testing and licensing. `docs/` holds one file. Meanwhile the *reasons* — why a dead end goes to the clock, why `is_me` is the only role encoding, why a departed player's name leaves the chat — live in code comments and in journals that explicitly say they are "not durable authority".

**Options.** (a) Split `docs/`: `rules.md`, `architecture.md`, `dictionary.md`, leaving `README.md` as philosophy plus quickstart plus licence. (b) A short `docs/decisions/` of ADRs extracted from the existing comments, each linking to the code that implements it rather than restating it. (c) Leave it; it is not yet painful.

**Rec.** (b) first, (a) when the README next grows. The decisions are the asset and are currently discoverable only by reading 20K LOC of (excellent) comments. Five or six ADRs — server authority, one rule implementation, proto as source of truth, two licences on two artifacts, no persistence — would cover most of it. **Size S.** **Depends on:** nothing.

### G2. The deployment doc is right about everything it covers and silent on capacity

**Problem.** `docs/deployment.md` covers configuration, proxy pitfalls, licence obligations, restart cost and dictionary updates, thoroughly. It has no capacity guidance, no log or metric reference (D2 would create one), and it documents the shared rate-limit bucket as a known limitation rather than as something configurable (D3).

**Rec.** Fold D2's metric names, D3's trusted-proxy variable and D4's measured room capacity into it as those land. Do not write the section ahead of the numbers. **Size S.** **Depends on:** D2, D3, D4.

---

## Top 10, ranked

Ranked on impact per unit of cost, weighted by fit with the stated philosophy: server-authoritative, one rule implementation, no wordlist in the client, proto as single source of truth, licences on separate artifacts, plain JS, Go preferred.

| # | Direction | Size | Why here |
|---|---|---|---|
| 1 | **D2** — expvar counters plus a rejection event line | S | Everything in section E is currently guesswork. Cheapest item on the list and it unblocks the most expensive ones. Pure Go, no dependency, no protocol change. |
| 2 | **D3** — trusted-proxy IP plus per-room join throttle | S | A documented, *known* denial of service in the supported topology. The deployment doc already describes the fix it declined to take. Security work with a bounded diff. |
| 3 | **A3** — claim a dead end instead of waiting out the clock | S | Removes up to 30s of dead air from every elimination. The server already computes the answer; the authorization shape is copied from `Resign`. Best felt-quality per line in the game itself. |
| 4 | **C2** — a rules/help surface | S | Nothing anywhere states the rules. Copy plus one route; the largest new-player gain available, and a precondition for B1 sending strangers into rooms. |
| 5 | **E3** — a word-report message | S | Turns "the dictionary is wrong" from a lost complaint into an input. Small alone, and the only thing that makes the dictionary improve on a cadence other than Wikimedia's. |
| 6 | **B1** — quick-match | M | The online half is unreachable without a friend. Reuses `createRoom`/`joinRoom` wholesale; one map and a timeout in the hub. Biggest product ceiling lifted for a mid-sized change. |
| 7 | **E2** — a `common` tier the bot is held to | M-L | Fixes the fairness complaint players will actually voice ("it won with a word nobody knows") without taking any word away from a human. Below its impact only because it waits on a licence-compatible frequency list. |
| 8 | **A2 + C3** — point breakdown, and a diacritic-only near-miss hint | S each | Both teach the player something the server already knows and the client cannot derive. Both additive proto changes, which this repo does routinely and safely (`buf breaking` in CI, committed generated code, cross-language binary fixtures). |
| 9 | **F2** — push multi-client coverage into Go, shrink Playwright to smoke | M | The protocol suite already exists and runs everywhere in seconds; the browser suite is CI-only, serial and has a known join flake. Better signal, less flake, and it makes the repo workable on the machine it is actually developed on. |
| 10 | **D1(a) + D4** — drain mode, `/readyz`, version stamp, measured capacity | S | Turns "deploy when the game is quiet" into a routine deploy, and three ops unknowns into printed numbers. Prerequisite for taking B2 and E1(b) seriously. |

Just below the line, and why. **F1/F3/F4** (lint, fuzz, dependency bot) are all S and all worth doing in whatever week has room; they are off the top 10 only because nothing currently hurts. **E1** (corpus breadth) is deliberately held behind D2: adding 28k words of unmeasured quality to a corpus whose real problem may be E2's long tail is hard to undo and harder to evaluate. **B2** (identity and progression) is the largest product prize here and is correctly gated behind persistence and measurement — starting it now means shipping progression with no way to tell whether anyone progresses. **A1** (bot objective) is genuinely interesting but changes a component that is tested, tuned and shipped; it should follow the measurement that says the ladder is wrong. **A4, B3, B4, C1, G1, G2** are all real and all fine as opportunistic work.

## Questions for the research agent

1. **A Vietnamese word-frequency list whose licence permits shipping inside a container image alongside CC BY-SA 4.0 data** — the single blocker on E2. Aggregate-only use is not enough: the ranking itself ships.
2. **Whether any open Vietnamese wordlist has meaningfully better compound coverage than viwiktionary** (E1) — the 2026-09-08 measurement covered undertheseanlp; is anything newer, and under what licence?
3. **How comparable word-chain games handle a no-legal-move claim** (A3) — does anyone let the trapped player assert it, and does it get abused?
4. **Realtime-server practice for single-binary room games:** typical goroutine-per-room budgets, and where people move to a shared registry (D4's capacity number; whether D1(b) snapshotting is ever worth it).
5. **Trusted-proxy header handling in Go** (D3) — is right-most-hop-with-CIDR-allowlist still the recommended shape, and does `coder/websocket` or any common middleware already provide it?

## Unresolved questions

1. **Is online play meant to grow, or is the bot the product?** B1/B3/B4 and B2 point in different directions; rank 6 assumes online growth matters. If it does not, the top 10 reorders around solo — A3, B4, C2, E2.
2. **Is a curated in-repo word list acceptable** (E1b), given the care taken to keep Apache-2.0 code and CC BY-SA data on separate artifacts? Our own additions are our own text, but they land in the same `.db` file; the licence statement needs a decision before that happens, not after.
3. **Does logging rejected words (D2) count as user content** the project wants to avoid holding? My assumption is that a normalized, length-capped word is fine and worth documenting; the alternative is counters only, which costs E1 and E3 most of their value.
4. **Is a fourth bot difficulty wanted at all,** or is the ladder deliberately three? A1 assumes the gap is the bot's *personality*, not its depth — unverified, and D2 would tell us.
5. **How long is a typical session, and how many rooms are live at peak?** Every capacity and persistence judgement in section D is speculation; no telemetry exists to check it, which is itself the argument for ranking D2 first.
6. **Report path:** the task brief names `…-260921-0016-improvement-directions.md` while the session hook advertises `…-260921-0017-{slug}.md`. Written to the brief's path; confirm which is canonical.
