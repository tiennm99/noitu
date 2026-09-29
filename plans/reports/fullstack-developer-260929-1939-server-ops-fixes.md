# Server core, packaging, CI and deployment fixes

Date: 2026-09-29, branch `dev`, nothing committed.

## Changes per finding

Server core review:

- **1 (shutdown order).** `serve()` in `server/cmd/noitu-server/main.go` now builds the wsapi server on `context.Background()`. The signal context is only the "stop now" trigger, so rooms and sessions survive until `StartDraining` has run and the drain has waited. The sequence lives in `drainAndShutdown` (drain, wait up to `NOITU_DRAIN_TIMEOUT`, `Shutdown`, then a bounded 2s flush). `run()` was split into `run` (store, listener, signal context) and `serve` so the sequence is testable.
- **4 (second signal).** `serve` calls the signal context's `stop` as soon as the context fires, so a second SIGTERM/SIGINT kills the process.
- **Shutdown flush.** After `api.Shutdown()` the process sleeps `shutdownFlush` (2s). wsapi exposes no live-session count, so this is a fixed bound, as the brief allowed.
- **2 (Hard, equal-score losses).** `negamax` returns `loseScore - depth`. The doc comment on `loseScore` explains it.
- **5 (out-of-turn resign).** `Engine.Resign` only calls `settle()` (and restarts the clock) when the turn moved. The README was right, so only code changed.
- **6 (`<ref name="a/b"/>`).** `refElement` in `wikitext.go` reads quoted attribute values whole.
- **7 (builder version).** The store now reads `builder_version` in `loadMeta` and refuses a missing or different value, naming the version found. The constant is exported as `dictionary.RequiredBuilderVersion`, and the builder's `builderVer` is now that same constant, so the two cannot drift. Test fixtures write the row.
- **8 (IdleTimeout).** `newHTTPServer` sets `IdleTimeout: 120s` (with the existing `ReadHeaderTimeout`) for both the public and debug listeners. No Read/WriteTimeout.
- **9 (real-corpus seed).** `playRealGame` now draws openings from a PCG seeded by `seed`, through a new `Store.RandomOpeningWordFrom(rng, min)` (`RandomOpeningWord` delegates to the same helper).
- **10 (syllable nits).** Dropped the `ngh` coda, the duplicate `ao` and `eu` nuclei, and the redundant `0x031B` clause.
- **3.** Belongs to wsapi. Not touched.

Security and ops review:

- **M1.** New "Coolify and Traefik" section in `docs/deployment.md`. It gives the exact env vars and what each changes (`NOITU_TRUSTED_PROXIES` as the Traefik subnet, `NOITU_MAX_CONNECTIONS_PER_IP=32` as a starting point, `NOITU_DRAIN_TIMEOUT`). It also covers Cloudflare (Traefik `forwardedHeaders.trustedIPs`) and a Traefik `/readyz` load-balancer health check label.
- **M2.** Added `noitu-server -healthcheck` (`checkHealth`: GET `/healthz` on `NOITU_ADDR`, wildcard or bare-port host dialled on 127.0.0.1, exit 0 on 200, else 1 with the reason on stderr). Added a Dockerfile `HEALTHCHECK` (interval 30s, timeout 5s, start-period 10s, retries 3). The docs explain how Coolify uses it and state the stop-grace rule: `NOITU_DRAIN_TIMEOUT + 2s < container stop grace`, so at most about 6s with Docker's 10s default. Also documented that a second signal ends the process.
- **L4.** `proto.yml` now uses `bufbuild/buf-action@v1` with `setup_only: true`. I confirmed `setup_only` in the action's `action.yml`. I dropped `version: latest`: the `version` input is optional and I could not confirm that `latest` is a valid value, so an unset version is the safer way to get the newest buf.
- **L5.** `COPY LICENSE /app/LICENSE` in the Dockerfile, and `app/LICENSE` added to the CI "licence travels with the data" check. Docs note the licence file now ships. The third-party notices gap is accepted while the image is unpublished (recorded in `deployment.md` and here; no code change). The web footer link is the web agent's job.
- **L6.** `go-version: stable` in every `setup-go` step (ci.yml x2, proto.yml), with `go.mod` left as the minimum. Added `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` to the Go job and `npm audit --omit=dev --audit-level=high` to the web job. Moving major tags only, no SHA pins.
- **L8.** Covered with finding 8.
- **N2.** Documented only, in the Observability section.
- **N3.** `.dockerignore` now excludes `.claude`, `**/.claude`, `.env*` and `**/.env*`.
- **N4.** `persist-credentials: false` on all five checkout steps.
- I also added `docker exec noitu /app/noitu-server -healthcheck` to the CI image job, so the HEALTHCHECK command is exercised.

## Skipped

- H1, L1, L2, L3, N1: wsapi agent.
- M3, L7: GitHub settings, manual (below).
- D1, D2, N5, N6: non-issues or optional, per the brief.
- Web footer link for the modification record (L5 item 3): web agent.

## Verification

- `go vet ./...` clean, `gofmt -l .` empty, `golangci-lint run ./...` 0 issues.
- `go test ./... -race -count=1`: every package passes. The first full run had one wsapi failure (`TestFrameFloodClosesTheConnection`) while the other agent was mid-edit. A single rerun of `./internal/wsapi` passed.
- Tests that fail without their fix (I reverted each fix and confirmed the failure):
  - `TestServeDrainsLiveGamesBeforeShuttingDown` drives `serve` over a real WebSocket bot game. With the signal context passed to `NewServer` it fails with EOF and `draining rooms=0 live_games=0`.
  - `TestResignOutOfTurnDoesNotSettleAPendingDeadEnd`
  - `TestHardPrefersTheSlowerLossWhenEveryLineLoses`
  - the new `TestStripWikitext` case
  - `TestOpenRefusesAMismatchedBuilderVersion`
- Tests added that do not depend on a reverted fix:
  - `TestDrainAndShutdownOrdersDrainBeforeShutdown`
  - `TestServersSetOnlyAnIdleTimeout`
  - `TestCheckHealth`
  - `TestRandomOpeningWordFromIsReproducible`
  - `TestOpenRefusesADatabaseWithNoBuilderVersion`
- Docker is available. `docker build --build-arg FIXTURE_DICT=1 -t noitu:review .` succeeded. In the running container, `docker exec ... -healthcheck` returned 0, the container became `healthy`, `app/LICENSE` was in the image, and `docker stop` produced `draining` then `shutting down` log lines and exited in 2.3s. I removed the test container and image afterwards.
- `make help` still lists all 14 lines. Workflow YAML parses.
- The real-corpus ladder test is skipped here (no real dictionary), so the effect of the negamax change on it was not re-measured.

## Manual steps for the maintainer

Coolify (app "noitu", currently zero env vars, health check off):

1. Run `docker network inspect coolify` on the host and note the subnet Traefik reaches the app on (or the app's own network).
2. Set `NOITU_TRUSTED_PROXIES=<that CIDR>` and `NOITU_MAX_CONNECTIONS_PER_IP=32`.
3. Set `NOITU_DRAIN_TIMEOUT` below the container's stop grace minus 2s. With Docker's 10s default that means at most `6s`. Check what Coolify's stop timeout actually is before going higher.
4. Leave the dashboard HTTP health check off. After the next deploy, confirm Coolify picks up the Dockerfile `HEALTHCHECK`.
5. Optional: add the Traefik `loadbalancer.healthcheck.path=/readyz` custom label, using the generated service name.
6. If Cloudflare is in front, configure Traefik `forwardedHeaders.trustedIPs` for its ranges.

GitHub:

7. M3: merge `dev` into `main`, or cherry-pick `.github/dependabot.yml`. Enable Dependabot alerts and security updates, secret scanning and push protection. Then accept the action major bumps Dependabot proposes.
8. L7: Settings, Actions, Workflow permissions: set read-only and untick "Allow GitHub Actions to create and approve pull requests".

## Unresolved questions

- The stop grace Coolify applies to this container is unverified. The docs state the rule and Docker's default only.
- The Coolify claims (Dockerfile `HEALTHCHECK` honoured, `docker network inspect coolify`) come from the review reports and Coolify's public behaviour, not from a deploy. The docs say to confirm after the first deploy.
- `buf-action` with no `version` input is assumed to resolve to the newest buf. The first CI run on `proto.yml` will confirm it.
