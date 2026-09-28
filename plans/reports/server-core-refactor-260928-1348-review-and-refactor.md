# Server core review and refactor (dev vs main)

Scope: `git diff main...dev -- server/cmd server/internal/{game,dictionary,vietnamese,bot} Dockerfile Makefile .github`.
wsapi, proto, gen, and web were not touched. No exported identifier or signature changed.

## Findings

| Sev | Location | Finding | Action |
|-----|----------|---------|--------|
| Low | server/cmd/noitu-server/main.go (envInt/envDuration/envNonNegDuration) | Three copies of the same read/trim/parse/validate/warn logic; the two duration helpers differed by a single comparison | Added one generic `envParsed[T]` helper. The three named wrappers stay, with the same names, log messages, and fallback rules |
| Low | server/cmd/noitu-server/main.go (run shutdown tail) | The `context.WithTimeout` + `Shutdown` block appeared twice, once for the debug server and once for the public server | Added `shutdownServer(*http.Server) error`, which does nothing when passed nil |
| Low | server/internal/game/engine.go (pointsFor/capParts) | The score total was summed three times across two functions (twice in capParts, once in pointsFor), plus a hand-written in-place filter | Moved the capping into pointsFor. A single `sumParts` helper (moved from the test file) does all summing, and `slices.DeleteFunc` does the filtering. Trim order and results are unchanged |
| Low | server/internal/game/state.go:201 | The `State.Standings` doc said "meaningless" during play, but Snapshot now leaves it nil during play | Doc now says "Nil while the game is in play". wsapi only reads it when the game ends (room_game.go:569) |
| Nit | server/internal/dictionary/store.go | `dsn()` just wrapped `DSN(path, true)` and had one caller. `math/rand/v2` sat in its own misplaced import group | Replaced the call with `DSN(path, true)` directly and let gofmt sort the imports |
| Low | server/internal/dictionary/store_test.go (nearMissFixtureAt) | Copied the open/exec/close logic that the existing `writeDB` helper already provides | `nearMissFixture` now calls `writeDB`. Same data, same assertions |

No problems found in: build-dictionary (the Windows-only fallback when rename fails is correct and not duplicated; the `defer func(){ _ = x.Close() }()` changes are there to satisfy lint), the vietnamese double NFC (it is needed: `lower(NFC(x))` can stop being NFC, and dropping the first NFC is not safe either because Go's simple case mapping of U+0130 differs between composed and decomposed input), Dockerfile, Makefile, dependabot, and the CI workflows (moving major tags, as house rules require; no pins changed).

## Bugs fixed

None. I checked for resource leaks: the dictionary DB handle is closed inside `Open`, and the goroutines in main exit when the process ends. I also looked at the path where the listener fails at startup, which returns without shutting down the rooms or the debug listener. The process exits right away, so this is not a real leak, and I left the behaviour as it was.

## Verification

- `go vet ./...` clean. `go build ./...` passes.
- `go test -race -count=1 ./cmd/... ./internal/game/... ./internal/dictionary/... ./internal/vietnamese/... ./internal/bot/...` all pass.
- `gofmt -l` clean. `golangci-lint run` on the owned packages reports 0 issues.

## Deferred items (not changed)

- `wsapi/convert.go` `PointKind` switch and `game.PointKind.String` are parallel switches that must be kept in sync by hand. This belongs to wsapi, so I left it alone.
- `Engine.UsedWords` returns `maps.Keys` over the engine's live map. That is safe only because the room goroutine is the only one that touches the engine. The doc could say "do not Submit while ranging". I left it as is because the existing single-owner rule already covers it.
- engine.go `LegalMoves`/`HasLegalMove` repeat the `e.used[word]` lookup inline where `e.Used(word)` would do. This is older code, outside the diff.
- `.github/workflows/*`: `actions/setup-go@v5` could move to the current major. Left alone to avoid pin churn; dependabot will propose it.
