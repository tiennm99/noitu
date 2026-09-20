# Server ops/observability batch

Scope: server/, .github/, Dockerfile, Makefile, README.md, docs/deployment.md. Did not
touch proto/, web/, or generated code.

## What changed

### 1. Observability (D2)
- `server/internal/wsapi/metrics.go` (new): `metricSet` struct of `expvar.Int`/`expvar.Map`,
  one package var `metrics`. Counters: connections open/total; rooms live/total (bot/pvp);
  games started/finished (bot/pvp); words submitted/accepted/rejected (by
  `game.RejectReason.String()`); eliminations (by wire `GameEndReason` string); chat lines;
  joins refused (`too_many_attempts`/`room_not_found`/`room_full` — reused wire error codes
  as keys, one vocabulary instead of two); bot moves (by `bot.Difficulty.String()`); resumes
  attempted/succeeded.
- Wired at call sites: `server.go` (handleWS: connections), `hub.go` (newRegisteredRoom:
  rooms live/total, draining check), `room.go` (beginGame: games started; broadcastGameOver:
  games finished; handleSubmit: words + new `recordRejection` helper; handleBotMove: bot
  moves; handleChat: chat lines; handleJoin: room_full; broadcastElimination: eliminations;
  handleResume: resumes succeeded), `session.go` (resumeFrom: resumes attempted; JoinRoom
  dispatch: rate-limited/not-found refusals).
- `GET /debug/vars` only ever mounted on a **separate** `http.Server` in
  `cmd/noitu-server/main.go` (`newDebugServer`), started only when `NOITU_DEBUG_ADDR` is set.
  Never touches the public mux — verified by `TestDebugVarsNotOnPublicMux`.
- `word_rejected` Info log line in `room.recordRejection` (room.go): `reason`, `word`
  (normalized via `vietnamese.Normalize` after `sanitizeText`, capped at `maxWordRunes`=64 —
  never the raw typed text), `link` (`engine.Current()`), `mode`, `room`. No chat text or
  nicknames logged anywhere.
- Documented in `docs/deployment.md` under new "## Observability" section: what
  `NOITU_DEBUG_ADDR` does, every counter name, and exactly what the log line contains.

### 2. Ops hygiene (D4 + D1a)
- `GET /readyz` (server.go): 200 while accepting, 503 while draining (`hub.isDraining()`).
  `GET /healthz` unchanged.
- Drain mode: `hub.draining` (atomic.Bool) refuses new rooms with `errDraining` →
  `server_restarting` (session.go `roomCreateError`, reusing the existing i18n key).
  `hub.liveGames` (atomic.Int64) counts rooms with a game **running**, not lobbies;
  `room.liveCounted` (atomic.Bool) + a `CompareAndSwap` in `room.run()`'s teardown guarantees
  exactly one decrement per increment even if a room is cancelled mid-game rather than
  finishing normally. `main.go`'s `run()`: on ctx.Done, logs room/live-game count, calls
  `api.StartDraining()`, calls `waitForGamesToFinish(api, cfg.drainTimeout)` (polls
  `LiveGameCount()` every 200ms up to `NOITU_DRAIN_TIMEOUT`), logs again, then does the
  existing `api.Shutdown()` / `srv.Shutdown()`.
- Version stamp: `main.version` (default `"dev"`), set via `-X main.version=...` from
  `git describe --tags --always --dirty`. `Makefile`'s `server` target computes it directly;
  a new `image` target passes it to `docker build --build-arg VERSION=...` (`.dockerignore`
  deliberately excludes `.git`, so the Dockerfile can't run `git describe` itself — this is
  the standard, documented reason). `wsapi.Config.Version` → `Server.version` → printed in
  the `listening` log line and served as plain text by `GET /version`.
- `README.md` and `docs/deployment.md`: added `NOITU_DEBUG_ADDR`, `NOITU_DRAIN_TIMEOUT` to
  the env tables; new "Draining on deploy" section; "Version" subsection under "The image";
  `image` make target in both the targets table and "Without make" raw equivalents;
  `/readyz` and `/version` added to the endpoints list.

### 3. Fuzz targets (F3)
- `server/internal/wsapi/codec_test.go`: `FuzzDecode`, seeded from `wire_test.go`'s
  `clientVariants()` plus malformed/short/wrong-type frames. Asserts non-binary always
  `ErrNotBinary`, otherwise either a real error or a real message — never both nil.
- `server/internal/wsapi/nickname_test.go` (new): `FuzzSanitizeText`, seeded from the
  existing `TestSanitizeNickname` table plus the stacking-mark/bidi/zero-width regression
  cases. Asserts: no control/format char in the output, no non-printable rune, rune count
  ≤ `maxNicknameRunes`, no run of combining marks over `maxNicknameMarks`.
- `server/internal/vietnamese/normalize_test.go`: `FuzzNormalize`, seeded from the existing
  table tests. Asserts NFC, lowercase, single-spaced, and word/syllables agree with each
  other (or `ErrEmpty` with empty output).
  **Found and fixed a real bug**: `Normalize` composed (NFC) then lowercased, but lowering a
  base rune can enable a composition that only exists for the lowercase form (e.g. `Y` + ring
  above has no precomposed codepoint; `y` + ring above does, U+1E99) — so the result could be
  non-NFC despite the doc's promise. Fixed by re-composing after lowering
  (`normalize.go`). Vietnamese text never hits this (no case-asymmetric diacritics in the
  language), so no dictionary data is invalidated by the fix. Regression seed corpus saved at
  `server/internal/vietnamese/testdata/fuzz/FuzzNormalize/d3ebd1ce3935d20e`.
- All three run their seed corpus under plain `go test` (fast, no `-fuzz` needed); verified
  each with `-fuzz -fuzztime 15-20s` locally, clean after the Normalize fix.

### 4. CI hygiene (F4 + reviewer findings)
- `.github/workflows/ci.yml`: `push` trigger now `[main, dev]`. Go job gained a `gofmt -l`
  format-check step and a `golangci-lint` step (`golangci/golangci-lint-action@v9`, moving
  major tag, default config — no `.golangci.yml` added, no linters disabled).
- `.github/workflows/proto.yml`: `push` trigger now `[main, dev]`; `bufbuild/buf-setup-action`
  `version:` changed from the pinned `1.69.0` to `latest`; `buf breaking` still compares
  against `.git#ref=origin/main`, untouched.
- `.github/dependabot.yml` (new): `gomod` (`/server`), `npm` (`/web`), `github-actions` (`/`),
  `docker` (`/`); weekly; each grouped for minor+patch.
- Fixed **23** pre-existing `errcheck` findings and **1** `staticcheck` finding (Unicode
  format chars in a string literal) across `server/` so the tree passes `golangci-lint`
  default linters clean — these were latent (hidden by golangci-lint's default
  `max-same-issues: 3`, confirmed with `--max-same-issues=0`), not something I introduced;
  fixed rather than suppressed per the instruction not to add lint noise. Files touched:
  `cmd/build-dictionary/{dump.go,main.go,main_test.go}`, `internal/dictionary/{store.go,
  store_test.go}`, `internal/wsapi/{multiplayer_test.go,limits_test.go}`. All are
  `defer x.Close()`/`Rollback()`/`os.Remove()` wrapped as `defer func() { _ = x.Close() }()`,
  or a raw Unicode literal replaced with its `\uXXXX` escape.
- `make proto-check` unaffected (didn't touch `proto/`, `buf.yaml`, `buf.gen.yaml`, or the
  builder's `DICT_URL` constant).

### 5. Makefile / README
- `Makefile`: `VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo
  dev)`; `server` target now passes `-ldflags "-X main.version=$(VERSION)"`; new `image`
  target; help text and `.PHONY` updated for both. (`web-lint` target/step already present
  from a concurrent change — left untouched, not mine.)
- `README.md`: env table, endpoints list, Make targets table, "Without make" raw equivalents
  (server ldflags line, new `image` raw docker command), Deployment section's docker example
  now points at `make image`.

## Verification

```
cd server && go vet ./... && gofmt -l . && golangci-lint run ./... && go test ./... -race -count=1
```

Tail of the actual run:
```
0 issues.
ok      github.com/tiennm99dev/noitu/server/cmd/build-dictionary       2.803s
?       github.com/tiennm99dev/noitu/server/cmd/noitu-server    [no test files]
?       github.com/tiennm99dev/noitu/server/gen/noitu/v1        [no test files]
ok      github.com/tiennm99dev/noitu/server/internal/bot        1.609s
ok      github.com/tiennm99dev/noitu/server/internal/dictionary 4.074s
ok      github.com/tiennm99dev/noitu/server/internal/game       1.055s
ok      github.com/tiennm99dev/noitu/server/internal/vietnamese 1.030s
ok      github.com/tiennm99dev/noitu/server/internal/wsapi      28.489s
```

Also: built the real binary via `make server` (embeds `442ced3-dirty`, confirmed via
`/version`); ran it against the fixture dictionary with `NOITU_DEBUG_ADDR` set and curled
`/healthz` (200), `/readyz` (200, then 503 conceptually verified via the unit test),
`/version` (`442ced3-dirty`), `/debug/vars` on the public port (404) and on the debug port
(200, all `noitu_*` counters present at zero); sent `SIGTERM` and confirmed the `draining` /
`shutting down` log lines with room/live-game counts. Cleaned up the binary, fixture db, and
verified no leftover process. Also built the Dockerfile's `build` stage in isolation
(`docker build --target build --build-arg VERSION=...`, avoids the `web` stage's `npm`
entirely per the constraint) to confirm the new `-ldflags` line is valid; removed the test
image afterward.

New/changed tests, all in the behaviour list asked for:
- `TestReadyzFlipsOnDrain`, `TestDrainRefusesNewRooms`, `TestLiveGameCountTracksGamesNotLobbies`
  (drain_test.go)
- `TestVersionEndpoint`, `TestVersionEndpointDefaultsWhenUnset`, `TestDebugVarsNotOnPublicMux`
  (drain_test.go)
- `TestMetricsCountWordSubmissions` (metrics_test.go)
- `FuzzDecode`, `FuzzSanitizeText`, `FuzzNormalize`

Two existing tests (`TestTheFirstTurnIsDrawn`, `TestABotGameOpensWithTheHuman`) hand-build a
`room{}` and call `beginGame()` directly without going through the hub; `beginGame` now
touches `r.hub.gameStarted()`, so both were given `hub: &hub{}` — a zero-value hub is fine
since only its atomic fields are touched.

## Deferred / not done

- Did not add per-room `game_in_progress` join refusal to the `joinsRefused` metric — spec
  named exactly three buckets (rate limit/not found/full); adding a fourth wasn't asked for.
- Did not touch `ci.yml`'s image-build steps to pass `VERSION` — the deliverable named the
  Makefile and Dockerfile specifically; the Dockerfile's `ARG VERSION=dev` default keeps
  those builds working unchanged.
- Observed `TestStandingsReachEverySeat` fail once under full-suite load (`context deadline
  exceeded` reading a socket) but pass 5/5 in isolation and clean on every subsequent full
  run; looks like a pre-existing environment flake under CPU contention, not something my
  change touches (it doesn't call any of the changed code paths). Did not chase further since
  the suite has been green on every full re-run since.

## Unresolved questions

- None blocking. One judgment call worth flagging: `NOITU_DRAIN_TIMEOUT` accepts `0` as an
  explicit valid value (meaning "don't wait"), unlike every other duration env var where `0`/
  invalid falls back to the default — needed a second parser (`envNonNegDuration`) rather than
  reusing `envDuration`. Documented inline; flagging in case the two-parser asymmetry should
  instead be a single parser with a flag, but the current split reads clearer to me.
