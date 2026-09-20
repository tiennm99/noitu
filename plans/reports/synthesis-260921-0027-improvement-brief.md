# noitu improvement brief — synthesis of three agent reports

Date: 2026-09-21. Branch: `dev` at `dd3b463`. Sources (same directory):

- `researcher-260921-0016-noitu-landscape-and-improvement-inputs.md` — competitors, dispute patterns, small-server practice
- `brainstormer-260921-0016-improvement-directions.md` — 25 codebase-grounded directions, ranked top 10
- `code-reviewer-260921-0016-codebase-health-scan.md` — health scan; all tests green, 8 confirmed + 3 plausible findings

## Headline

Codebase is healthy (Go coverage 88–100% per package, 190 JS tests green, prior review findings all fixed).
Three things stand out across all reports:

1. **One shipped feature is silently dead**: `PlayedWord.player_id` is never set by the server
   (`server/internal/wsapi/convert.go:108`, verified), so 3–4 player rooms never show who played
   each word. Three fixture-based tests mask it.
2. **The supported deployment has a known, documented, unfixed DoS**: behind the reverse proxy all
   per-IP limiters collapse to one bucket (`server.go:150-162`, `docs/deployment.md:113-119`), and
   there is no global room or connection cap.
3. **Zero telemetry** — no metrics, ~12 log sites in `wsapi`. Every dictionary/bot/capacity
   decision is currently a guess. Research confirms dictionary coverage is the #1 complaint across
   every competing nối từ app, so measuring rejections is the cheapest high-leverage step.

## Fix now (bugs and security, all S–M, no product decision needed)

| # | Item | Source | Size |
|---|---|---|---|
| 1 | Set `PlayerId` in `wsapi.PlayedWord`; add a producer-side test | reviewer C1 | S |
| 2 | Trusted-proxy mode (`NOITU_TRUSTED_PROXY` / `X-Forwarded-For`) + per-room join throttle | reviewer C3, brainstorm D3 | S–M |
| 3 | Global room cap + connection cap in hub/server | reviewer C2 | M |
| 4 | Per-connection frame-rate ceiling; charge `Ping` and empty `ClientMessage` | reviewer C4 | S |
| 5 | Sanitize `Move.Typed` server-side (reuse `sanitizeText`) instead of relying on client `byMe` gate | reviewer C5 | S |
| 6 | Builder: escape `#` in SQLite DSN like the store does; rename-over instead of remove-then-rename | reviewer C6, C7 | S |
| 7 | `vacate` sessions on idle/cancel room exits | reviewer C8 | S |

## Build next (ranked, brainstormer top 10 cross-checked with research)

| # | Direction | Why | Size | Depends on |
|---|---|---|---|---|
| 1 | **Observability**: expvar counters + one `word_rejected` slog line (normalized, length-capped) | Unblocks every dictionary/bot decision below | S | decision on logging rejected words |
| 2 | **Claim a dead end** (`ClaimDeadEnd` msg, shape copied from `Resign`) | Server already knows via `HasLegalMove`; removes 30s dead air per elimination | S | — |
| 3 | **Rules/help surface** | No rules copy anywhere in `web/src`; competitors all state rules; precondition for strangers meeting via quick-match | S | — |
| 4 | **`ReportWord` message** + simple review queue | Every competitor with a fixed wordlist has one; research found no live-vote precedent, so keep it async | S | #1 for triage |
| 5 | **Quick-match** with wait-time-widening window | Rooms reachable only by code today; lichess-style widening fits small pools | M | rules surface (#3) |
| 6 | **`common` word tier the bot is held to** | Fixes "Hard won with a word nobody knows" without removing words from humans | M–L | licence-compatible frequency list (not yet found) |
| 7 | **Point breakdown on `PlayedWord`** + diacritics-only near-miss hint on `MoveRejected` | Additive proto changes; teaches what the server already knows | S each | — |
| 8 | **Move multi-client assertions into Go suite; Playwright → smoke** | e2e is CI-only, serial, flaky, cannot run on the dev box | M | — |
| 9 | **Drain mode, `/readyz`, version stamp, measured capacity** | Makes deploys routine; prerequisite for persistence talk | S | — |
| 10 | **Corpus widening / community allowlist overlay** (Discord-bot precedent) | Held behind #1: unmeasured 28k-word addition is hard to evaluate or undo | L | #1, licence decision |

Below the line, opportunistic: JS lint (ESLint per house rule), fuzz targets for `Decode`/`sanitizeText`/`Normalize`,
dependabot/renovate, `allowScripts` block in `web/package.json`, CI on `dev` branch (`ci.yml` runs push only on `main`),
`buf` pinned to exact 1.69.0 against the moving-tag rule, split `room.go` (1609 LOC) and `online/+page.svelte` (612 LOC, six `$effect`s).
Two leftovers from the 2026-09-10 UX reports: chat toggle renders after `ChainHistory`; lobby chat has no unread badge.

Research-only signals, not recommended yet: Elo/ranked ladder (noitu.fun, wordfight.online both have one),
card-mechanic "chaos" mode (skip/reverse/swap), 5–10s casual timers vs noitu's 30s. All depend on the answer to Q1.

## Decisions needed from the maintainer

1. **Is online play meant to grow, or is vs-bot the product?** Decides whether quick-match/ranked (rows 5, research signals) rank above solo items (dead-end claim, daily mode).
2. **May the server log rejected words** (normalized, capped)? Counters-only kills most of the value of rows 1, 4, 10.
3. **Is a curated in-repo word overlay acceptable** under the Apache-2.0 / CC BY-SA split? Our additions land in the same `.db`; licence statement must be settled first.
4. **Is the container ever run unproxied?** Decides whether the proxy-bucket fix (C3) or the global caps (C2) ship first.
5. **Is a fourth bot difficulty wanted**, or is the ladder deliberately three?

## Unresolved

- No licence-compatible Vietnamese word-frequency list identified; gates row 6.
- `phobo-contribute-words` (Discord-bot community wordlist) internals unverified; overlay characterization is inferred.
- Peak rooms / session length unknown; all capacity claims are speculation until row 1 lands.
