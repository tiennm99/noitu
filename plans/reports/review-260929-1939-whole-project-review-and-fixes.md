# Whole-project review and fixes (dev, 2026-09-29)

Four parallel reviews (server core, wsapi, web, security/ops) followed by three
parallel implementation passes with disjoint file ownership. Nothing is
committed. Baseline before and after: Go vet, gofmt, golangci-lint and
`go test ./... -race` clean; web check, lint and vitest clean (270 → 326 tests).

## Reviews

- [Server core](code-reviewer-260929-1939-server-core-review.md) — 10 findings
- [wsapi](code-reviewer-260929-1939-wsapi-review.md) — 13 findings
- [Web](code-reviewer-260929-1939-web-review.md) — 16 findings
- [Security and ops](code-reviewer-260929-1939-security-ops-review.md) — 8 fix-now, 6 nits, 2 non-issues

## Implementation

- [Server core and ops fixes](fullstack-developer-260929-1939-server-ops-fixes.md)
- [wsapi fixes](fullstack-developer-260929-1939-wsapi-fixes.md)
- [Web fixes](fullstack-developer-260929-1939-web-fixes.md)

Highest-impact fixes:

1. SIGTERM no longer kills every game before the drain: the restart notice and
   `NOITU_DRAIN_TIMEOUT` now work; a second signal exits immediately.
2. A kicked player's token can no longer resume into whoever now holds that
   seat.
3. One client can no longer hold the global connection or room caps: sockets
   that never send Hello close after 10s, and the room budget is charged per
   address.
4. A player who reconnects into the lobby after their game ended while away is
   now replayed that game's GameOver, so the UI is no longer stuck on a frozen
   board; refused resumes now clear the stale room or board on the client.
5. Hard bot prefers the slower loss in lost positions (41/3000 boards fixed).
6. Non-breaking spaces in a typed word are now treated as spaces.
7. Container HEALTHCHECK via `noitu-server -healthcheck`; LICENSE in the image;
   security headers; IdleTimeout; CI runs govulncheck and npm audit; buf action
   replaced; checkout without persisted credentials.

Decisions taken in this session (product questions the reviewers raised):

- Resume-after-game-over replays GameOver rather than adding a proto field.
- Reloading `/play` starts a fresh game on the same rung (the page's documented
  intent); a socket drop inside the same tab still resumes.
- A kicked player who returns gets `session_not_resumable`; kicks do not revoke
  tokens.
- Ready toggles count as lobby activity for the idle window.
- Resigning out of turn follows the README: no immediate knock-out.
- Added after the web pass: `/play` shows a "Chơi lại" button when a refused
  resume leaves the board idle under the error banner.

## Manual steps outside the repo

Coolify (verified today: the app deploys `main` with no env vars and the
dashboard health check off):

- Set `NOITU_TRUSTED_PROXIES` to the Traefik/Docker network range, then
  `NOITU_MAX_CONNECTIONS_PER_IP=32`.
- Set `NOITU_DRAIN_TIMEOUT` so that drain + 2s is under the container stop
  grace (6s or less under Docker's 10s default), or raise the grace.
- After the next deploy, confirm the Dockerfile HEALTHCHECK is picked up.

GitHub:

- The maintainer chose to drop `dependabot.yml` rather than move it to main;
  the CI govulncheck and npm audit steps are the dependency signal instead.
  Enable Dependabot alerts and secret scanning in repo settings if wanted.
- Set default workflow permissions to read-only and disable "Allow GitHub
  Actions to create and approve pull requests".

## Needs a human visual check (no browser on this host)

- The `.primary` buttons in light and dark, all states, on the online page,
  landing page, ChatPanel, WordInput and GameOverPanel.
- The `/online` heading at 1.5rem (was 1.3rem); the away banner and footer on a
  phone width; the new `/play` restart button.
- Reconnect flows on `/online` (held actions after a drop, leave while offline,
  chat while offline) and the `/play` refused-resume path.
- Playwright was not run.

## Unresolved questions

- Confirm the wording of the new `unknown_message` string in `vi.js`.
- Whether the first `proto.yml` run with `bufbuild/buf-action@v1` and no
  `version` input resolves to the newest buf.
