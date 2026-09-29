# Server core review: whole codebase, 2026-09-29

Branch `dev` @ d3eb13e. This was a read-only review. Scope: `server/cmd/noitu-server`, `server/cmd/build-dictionary`,
`server/internal/{game,dictionary,vietnamese,bot}`, `Dockerfile`, `Makefile`,
`.github/workflows/ci.yml`. I read the two earlier reports first
(`server-core-refactor-260928-1348`, `code-reviewer-260921-1529-server-architecture-review`)
and do not repeat what they already fixed or deferred.

Baseline, re-run at the start of this review: `go vet`, `go test -race -count=1`, `gofmt -l` and
`golangci-lint` (0 issues) are all clean on the scoped packages. Coverage: vietnamese 100,
game 95.3, bot 91.2, dictionary 88.9, build-dictionary 88.6, **noitu-server 29.5**
(`run()` has no test at all).

Every finding below was reproduced. Experiments ran against a copy of the module in the
session scratchpad. No project file was changed.

## Findings

| # | Sev | Location | Finding |
|---|-----|----------|---------|
| 1 | **High** | `server/cmd/noitu-server/main.go:89-92` | The signal context is passed to `wsapi.NewServer`. On SIGTERM every room and every session is cancelled before `StartDraining` runs. As a result `NOITU_DRAIN_TIMEOUT` never does anything, and players never receive `server_restarting` |
| 2 | Med | `server/internal/bot/strategy_hard.go:155` | Negamax gives a loss the same score whatever its depth. In a position its search sees as lost, Hard plays a move that hands the opponent an immediate kill when a slower loss was available |
| 3 | Med (belongs to wsapi) | `server/internal/wsapi/nickname.go:68`, used before `Submit` at `room_game.go:232` and `dispatch.go:320` | `sanitizeText` deletes U+00A0 and every other non-ASCII space instead of turning it into a space. "ngữ pháp" reaches the engine as "ngữpháp" and is rejected as `fewer than two syllables`. This breaks the NBSP promise in `vietnamese.Normalize` (`normalize.go:35-36, 49-50`) |
| 4 | Low | `server/cmd/noitu-server/main.go:89-90, 131` | `stop()` is only called by the defer. A second SIGTERM or SIGINT during a drain is caught and ignored, so an operator cannot cut a long drain short |
| 5 | Low | `server/internal/game/engine.go:479-483` | `Resign` by a player who is not to act calls `settle()`. That eliminates the player to act at a dead end straight away, instead of on their own clock as `settle`'s doc and the README describe |
| 6 | Low | `server/cmd/build-dictionary/wikitext.go:129` | In `refElement`, the self-closing branch `[^>/]*` fails when an attribute contains `/`. The paired branch then swallows real definition text up to the next `</ref>` |
| 7 | Low | `server/internal/dictionary/store.go:38-40, 163-194`; `store_test.go:670` | `builder_version` is never read. The only guard against an older database is that a meta key is missing. The test named `TestOpenRefusesOlderBuilderVersion` only covers a missing `meaning_count`. This was raised on 2026-09-21 and is still open |
| 8 | Low | `server/cmd/noitu-server/main.go:104-108, 174` | Neither `http.Server` sets `IdleTimeout`, and `ReadTimeout` is 0 too, so idle keep-alive connections are never closed. They are not counted against `MaxConnections`, which only applies to `/ws` |
| 9 | Low (test) | `server/internal/bot/realcorpus_test.go:33, 43` | The `seed` parameter of `playRealGame` is never used, and openings come from the global `rand`. The real-corpus ladder, the one measurement the synthetic test defers to, cannot be reproduced run to run |
| 10 | Nit | `server/cmd/build-dictionary/syllable.go:30, 39, 69` | `"ngh"` is listed as a coda, which Vietnamese never has. `"ao"` and `"eu"` appear twice in the nuclei list. `r == 0x031B` is already inside the range before it |

### 1. High: SIGTERM kills every game before the drain begins

The path in code:

- `main.go:89` creates `ctx` with `signal.NotifyContext`, and `main.go:92` passes it to `wsapi.NewServer`.
- `NewServer` wraps that ctx (`server.go:88`) and gives it to the hub. Each room derives from the hub (`room.go:215`), and so does each session (`server.go:193`).
- Rooms `return` on `<-r.ctx.Done()` (`room.go:343-345`). Sessions tear down on `s.ctx.Done()`.
- So the moment the signal arrives, `ctx` is cancelled. Every room exits, `stopCountingLive` drops `liveGames` to 0, and every session starts closing. All of this happens before `main.go:140` `StartDraining()` runs.

What I measured, using a real process with the fixture DB, one bot game in progress, and SIGTERM:

| Binary | `NOITU_DRAIN_TIMEOUT` | Server log | Client saw |
|---|---|---|---|
| current `main.go` | 3s | `draining rooms=0 live_games=0` right away | raw EOF, 8/8 runs with no `server_restarting` |
| `NewServer(context.Background(), …)` | 3s | `draining rooms=1 live_games=1` → `drain timed out` → `shutting down` | `server_restarting`, 8/8 |

Impact: all four steps of "Draining on deploy" in `docs/deployment.md` (lines 241-270) are false in production. Even with `DRAIN_TIMEOUT=0` the "tells players the server is restarting" claim (line 287) is false. Every deploy drops live games without telling the players. The wsapi drain tests do not catch this because `newTestServer` never cancels the ctx it passes in. The `waitForGamesToFinish` tests use a fake counter.

Fix (main.go only):

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
api := wsapi.NewServer(context.Background(), store, wsapi.Config{ ... })
...
case <-ctx.Done():
}
stop() // finding 4: a second signal now terminates instead of being swallowed
```

`api.Shutdown()` already cancels the server's own ctx (`server.go:141-144`), so rooms and the limiter sweeper still stop at shutdown.

Test: pull the post-signal sequence out into a helper, for example `drainAndShutdown(api, drainTimeout)`. Then add a test that starts a game on `NewServer(context.Background(), …)`, cancels a separate "signal" ctx, and asserts `LiveGameCount()==1` and that the client gets `server_restarting`. You can also add a `docker stop` step to the CI image job that greps the log for `draining rooms=1`.

Remaining risk after the fix, which I measured and could not reproduce: `srv.Shutdown` does not wait for hijacked WebSocket connections. `main` could therefore return before a session's writer has flushed the notice. It never happened in 8 of 8 runs with one client. With thousands of clients it could.

### 2. Med: Hard walks into immediate kills when every line looks lost

`negamax` returns `loseScore` (-1000) for "no reply" at any depth (`strategy_hard.go:155`). When every root move loses within the 4-ply horizon, all of them score the same. Hard then plays the first one, and the first in tightest-first order is often the move that lets the opponent kill immediately. Against a human, a slower loss often never happens, because the human does not find the forced line.

Measured on 3000 seeded random graphs (12 syllables, degree ≤6, so the search is full width and the budget never runs out): 41 times Hard played a move that loses at once while a move that does not was available. `nodeCap` from 3 to 20000 all gave 41 at full budget. An exact 4-ply solver confirmed that all 41 positions were forced losses, so this is a missing preference, not a search bug. I also checked the budget itself: on larger graphs (60 syllables, degree ≤30, 224 boards) `nodeCap=20000` never changed the chosen move. The budget is not a problem.

Fix: `return loseScore - float64(depth)`. `depth` is the remaining depth, so a sooner loss scores lower, and by negation a sooner win scores higher. Result: 41 → 0. The existing suite passes with it (`TestDifficultyLadder`: hard-vs-easy 95%, medium-vs-easy 93%, hard-vs-medium 78%, against 94/93/78 today). Add a `fakeBoard` test with two moves that both lose within the horizon, one in 1 ply and one in 3, and assert Hard picks the slower one.

### 3. Med (for the wsapi owner): NBSP and other Unicode spaces merge syllables

`sanitizeText` drops runes for which `!unicode.IsPrint(r)` is true (`nickname.go:68`). `IsPrint` counts only ASCII U+0020 as a printable space, so U+00A0, U+2009, U+202F and U+3000 are deleted rather than turned into spaces. Measured:

```
"ngữ pháp" -> sanitized "ngữpháp" -> Normalize "ngữpháp" (1 syllable)
Normalize alone -> "ngữ pháp" (2 syllables)
```

`vietnamese.Normalize` promises exactly this case ("non-breaking spaces that IMEs and copy-paste routinely introduce"), but the transport throws the space away first. A player who pastes a word is refused. Fix, in `sanitizeText`'s map: `case unicode.IsSpace(r): return ' '` before the `IsPrint` arm. The trailing `strings.Fields` join already collapses the result. Add a table case with ` ` to the sanitizeText tests.

### 5. Low: an out-of-turn resignation settles the player to act at once

Three seats. Alice plays into a dead end, so Bob is to act with 20s left. Carol resigns 1s later. The result I measured: `over=true winner=alice`, Bob eliminated with `no legal move` 1s into his turn. The final result would be the same after his clock ran out. But `settle`'s doc (`engine.go:451-458`) says the first player to face a dead end "still loses it on their own clock", and the README says the same. The UI also never gets to show Bob the board as his turn. Fix: in `Resign`, call `settle()` only when `e.Turn() != before`. The resignation then moved the turn, and the new player to act has already seen the board, which is the argument `settle` itself makes. No existing test covers resign-at-dead-end.

### 6. Low: `<ref name="a/b"/>` eats definition text

Measured: `Một từ.<ref name="a/b"/> Nghĩa thêm <ref>x</ref> cuối.` → `Một từ. cuối.` Verified replacement:
`(?s)<ref\b(?:[^>"]|"[^"]*")*/>|<ref\b[^>]*>.*?</ref>`. On five shapes it removes the self-closing ref with a slash, a bare `/>`, a named pair, a pair followed by a self-closing ref, and a pair whose attribute contains a slash, and keeps the text between them. I did not measure how common the shape is, because the dump is not on this host. Add the case to `TestStripWikitext`.

### 7. Low: the builder version is never checked

`requiredBuilderVersion` only appears in an error message. A v4 database that happens to carry both count keys, or a future v6 with the same keys and different semantics, loads without complaint. The test fixtures have no `builder_version` row and still open. Fix: read `builder_version` in `loadMeta` and refuse a mismatch. Add a `builder_version` row to `fixtureSchema` inserts. Add a builder test asserting `builderVer` equals the store's constant (export it, or put the check in `verify()`).

### 8. Low: no `IdleTimeout`

Go uses `ReadTimeout` when `IdleTimeout` is 0, and when both are 0 an idle keep-alive connection is kept forever. Static-asset and `/healthz` clients therefore hold file descriptors without limit, outside every connection cap. Fix: `IdleTimeout: 120 * time.Second` on both servers. It does not touch WebSockets, which are hijacked.

## Test gaps (beyond those tied to findings)

- `cmd/noitu-server` `run()`: its shutdown ordering has no test. That is what let finding 1 in.
- `bot`: no test covers "prefer the slower loss" or "a sooner win beats a later one". `TestSimulatedGamesAlwaysTerminate` only uses Hard against Hard. `TestAllStrategiesChooseLegalMoves` covers the other strategies on one small board, which is enough for legality.
- `game`: nothing covers resigning while a dead end is pending (finding 5).
- `dictionary`: `TestOpenRefusesOlderBuilderVersion` tests something other than what its name says (finding 7).

## Checked and clean

- **engine**: order of validation (turn → length → dictionary → link → reuse); the used set covers the opening word and alias reuse; the chain cap, the rarity ladder (1→15 … 32→0) and the parts sum after capping; speed clamps; `expire`/`settle` in 2 and 4 seats; turn skips eliminated seats; the winner is always `players[turnIndex]` once `aliveN<=1`; Standings order; Snapshot copies. The `Move.Syllables` count from the typed input is safe because every alias keeps the syllable count (`variantsFor` swaps within a syllable only).
- **vietnamese**: the double NFC is needed, as documented earlier. The fuzz properties imply idempotence. Cf and ZW characters are stripped by the caller, and the builder rejects them via `!IsLetter`.
- **dictionary**: `DSN` escapes `#`, `?` and `%`. `mode=ro` with rollback journalling works on read-only filesystems, and no WAL is used. `validate` covers counts, orphan meanings, out-degree and dangling aliases, and the builder's `verify` covers first-syllable presence. The opener prefix and binary search are correct for `minOutDegree<=0`. The `NearMiss` exclusion and ambiguity rule are correct. `WordsStartingWith` does not expose the backing slice.
- **bot**: all strategies return only `LegalMoves` entries. Kill-decline removes kills from the search as documented. Fail-soft budget exhaustion did not change any choice in my measurements. The thinking-delay range is fine.
- **build-dictionary**: bzip2 magic check; truncation and mid-page EOF errors report their location; the hash covers the whole file; the atomic temp+rename with its Windows-only fallback. I also tested a crash-left `noitu.db.tmp-journal`: SQLite discards a journal next to a zero-length file, so the next build is not poisoned. The alias poisoning does not depend on iteration order. The tone-shift is limited to open syllables and the `qu` exclusion holds. The gloss cap is in runes and matches SQLite `LENGTH`.
- **noitu-server env parsing**: blank values count as unset; invalid or negative values fall back and log a warning; zero drain is accepted; the list is trimmed and empty items dropped. All of it matches the README.
- **Dockerfile, Makefile, CI**: exec-form ENTRYPOINT, so SIGTERM reaches PID 1; distroless nonroot; `.dockerignore` keeps dumps and DBs out; moving major tags; the licence and no-dump image assertions; `.part` download then rename.

## Recommended order

1. Finding 1 together with finding 4: one small `main.go` change and one test. The deploy path depends on it.
2. Finding 3: hand to the wsapi owner. It is a one-line fix.
3. Finding 2: a one-line change and one test.
4. Findings 5, 6 and 7: each is a few lines with a test.
5. Findings 8, 9 and 10 whenever that code is next touched.

## Unresolved questions

- Finding 1: once it is fixed, should `main` also wait a short, bounded time after `api.Shutdown()` so session writers can flush before the process exits? That is only needed if many-client deploys show lost notices.
- Finding 5: is the immediate settle on an out-of-turn resignation intentional? If so, the `settle` doc and the README sentence need changing instead of the code.
