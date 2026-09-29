# Security, dependency and operations review

Date: 2026-09-29. Branch `dev` at `d3eb13e`. Read-only review; no project file changed
except this report.

Threat model used: a public, internet-facing hobby game server behind Coolify/Traefik. No
accounts, no payments, no ambient credentials (no cookies; the resume token lives in
`localStorage` and is sent inside the protocol). The only personal data is a nickname and
chat text. What an attacker can realistically take from this service is **availability**
(everyone else's ability to play), **log/disk hygiene**, and **what strangers see rendered**.
Findings are ranked against that, not against a generic checklist.

Prior decisions respected (from `plans/reports/fullstack-developer-260921-0027-server-ops-observability.md`
and `docs/deployment.md`): trusted-proxy mode is opt-in; per-IP connection cap is off by
default because of NAT/CGNAT; rejected/reported words are logged normalised and capped; the
upstream dump is deliberately unpinned; `/debug/vars` lives on a separate listener; moving
major tags over exact pins (no SHA pinning recommended here).

## Scope

- `server/cmd/noitu-server/main.go`
- `server/internal/wsapi/` abuse surfaces only: `server.go`, `session.go`, `dispatch.go`,
  `codec.go`, `ratelimit.go`, `nickname.go`, `hub.go`, `room_chat.go`, parts of `room.go` /
  `room_game.go` (log lines only)
- `Dockerfile`, `.dockerignore`, `.github/workflows/ci.yml`, `.github/workflows/proto.yml`,
  `.github/dependabot.yml`, `Makefile`, `docs/deployment.md`
- `server/go.mod`, `web/package.json`, `web/package-lock.json`
- Licensing: `LICENSE`, `NOTICE`, `data/LICENSE`, `data/ATTRIBUTION.md`, README licence
  section, `web/src/lib/components/AttributionFooter.svelte`
- GitHub repo settings (read via `gh api`)

## Commands run and results

| Check | Result |
|---|---|
| `cd server && go list -m -u all \| grep '\['` | Updates available, none security-flagged: `modernc.org/sqlite` 1.58.0 -> 1.60.1, `modernc.org/libc` 1.75.6 -> 1.77.1, `golang.org/x/text` 0.41.0 -> 0.42.0, `golang.org/x/sys` 0.47.0 -> 0.48.0, `dustin/go-humanize` 1.0.1 -> 1.1.0, plus tool-only modules (`x/tools`, `x/mod`, `x/sync`, `modernc.org/cc,ccgo,gc`, `google/pprof`, deprecated `golang/protobuf` 1.5.0 as an indirect of the protoc tool) |
| `govulncheck ./...` (installed binary) | Fails: the binary was built with go1.26 and the host Go is go1.27.1. Not a project defect. |
| `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` (v1.8.0, DB updated 2026-09-28, go1.27.1 stdlib) | **No vulnerabilities found** (12 modules scanned) |
| `cd web && npm audit --omit=dev` | **0 vulnerabilities** (the only runtime dependency is `@bufbuild/protobuf`) |
| `cd web && npm audit` | 5 findings (3 low, 2 moderate), two real advisories, both dev-only; see "Dependency advisories" |
| Live probe: built `noitu-server` in scratchpad, `NOITU_MAX_CONNECTIONS=2`, opened 2 sockets that never send `Hello`, waited 65 s (past two keepalive rounds) | Both sockets still alive; a third upgrade got **HTTP 503**. Confirms finding H1. Server process stopped afterwards. |
| `curl` of `/healthz`, `/readyz`, `/version` on the probe server | 200s; no security headers on any response (L3) |
| `gh api repos/tiennm99/noitu/...` | Public repo; default workflow token permission `write`, Actions may approve PRs; vulnerability alerts disabled (404), Dependabot security updates disabled, secret scanning disabled |
| `git ls-tree origin/main -- .github` | `dependabot.yml` is **not** on `main` (only on `dev`) |
| `gh api repos/<action>/releases/latest` | checkout v7.0.1, setup-go v7.0.0, setup-node v7.0.0, upload-artifact v7.0.1, golangci-lint-action v9.3.0; `bufbuild/buf-setup-action` is **archived** |

## Findings

Legend for "Action": **Fix now**, **Document as accepted**, **Non-issue** (under this threat model).

| ID | Sev | Location | Action | Title |
|---|---|---|---|---|
| H1 | High | `server/internal/wsapi/server.go:163-177`, `session.go:290-354`, `dispatch.go:164-170`, `session.go:171` | Fix now | One client can exhaust the global connection and room ceilings: no per-IP default, no Hello deadline, room budget is per socket |
| M1 | Med | `docs/deployment.md:93-176` | Fix now (docs + live config) | No Coolify/Traefik recipe; the real deployment likely runs with one shared join bucket and no per-IP cap |
| M2 | Med | `Dockerfile:67-87`, `docs/deployment.md:241-271` | Fix now | Distroless image cannot pass a Coolify HTTP health check, and a drain longer than the container stop grace ends in SIGKILL |
| M3 | Med | `.github/dependabot.yml` (dev only), repo settings, `ci.yml:26,27,69,97,101,124` | Fix now | Dependabot is not running at all; alerts/security updates/secret scanning off; actions three majors behind |
| L1 | Low | `server/internal/wsapi/server.go:268-291,336-342` | Fix now | IPv6 clients are keyed on the full /128, so every per-IP limit is free to bypass from one /64 |
| L2 | Low | `room_game.go:223,239,288-300`, `dispatch.go:343-344` | Fix now | `word_rejected` / `word_reported` have no aggregate bound; one script can flood the log |
| L3 | Low | `server/internal/wsapi/server.go:209-247` | Fix now | No security response headers (framing, sniffing, referrer) |
| L4 | Low | `.github/workflows/proto.yml:213-215` | Fix now | `bufbuild/buf-setup-action` is archived |
| L5 | Low | `Dockerfile:73-79`, `ci.yml:162`, `AttributionFooter.svelte:10-23` | Fix now (small) / Document | Apache-2.0 `LICENSE` and third-party notices not in the image; UI credit does not point at the modification record |
| L6 | Low | `ci.yml:27-29,97-100`, `proto.yml:217-220` | Fix now | CI tests on Go 1.25.0 while the image ships Go 1.27.x; no vuln scan in CI |
| L7 | Low | GitHub repo settings | Fix now | Default `GITHUB_TOKEN` is `write` and Actions may approve PRs |
| L8 | Low | `server/cmd/noitu-server/main.go:104-108` | Fix now | Public `http.Server` has no `IdleTimeout` |
| N1 | Nit | `server/internal/wsapi/nickname.go:62-72` | Document or fix | Blank-rendering letters (U+3164, U+115F, U+2800, ...) survive the sanitiser |
| N2 | Nit | `main.go:168-175`, `docs/deployment.md:180-189` | Document | `NOITU_DEBUG_ADDR` inside a container is reachable from the whole Docker network |
| N3 | Nit | `.dockerignore` | Fix now | `.claude/` and `.env*` are not excluded from the build context |
| N4 | Nit | `ci.yml:26,68,96,138`, `proto.yml:205` | Fix now | `actions/checkout` persists the token into `.git/config` before `npm ci` runs dependency scripts |
| N5 | Nit | `Dockerfile:67` | Optional | `static-debian12` is the previous distroless base; `static-debian13` exists |
| N6 | Nit | `session.go:334-343` | Non-issue | Client control-frame pings bypass the frame limiter |
| D1 | Low | `web/package-lock.json` (`cookie@0.6.0` via `@sveltejs/kit@2.70.3`) | Non-issue | GHSA-pxg6-pf52-xh8x, unreachable |
| D2 | Low | `web/package-lock.json` (`vitest@3.2.7`, `@vitest/mocker`) | Non-issue for prod; bump when offered | GHSA-82fw-gwwq-j7x9, dev-only |

---

### H1 (High, fix now): one client can take the whole server offline

**Where.** `server/internal/wsapi/server.go:163-177` (global cap, per-IP cap off unless
configured), `session.go:290-354` (reads have no deadline; nothing closes a socket that never
says `Hello`), `session.go:171` + `dispatch.go:164-170` (room budget `roomLimiter` is per
session, so it resets on reconnect).

**What is wrong.** The two process-wide ceilings (`NOITU_MAX_CONNECTIONS`=2000,
`NOITU_MAX_ROOMS`=1000) exist to protect memory, but nothing stops a single address from
spending all of them:

1. The per-IP connection cap defaults to off (a deliberate decision, for CGNAT), and behind
   the proxy it *cannot* be turned on until trusted-proxy mode is on.
2. A socket that upgrades and never sends `Hello` is held forever. `readLoop` has no deadline
   by design, and the keepalive only proves the peer is alive; every WebSocket library
   answers pings automatically. Verified live: two silent sockets were still open after 65 s
   and the next upgrade got 503.
3. Past `Hello`, an idle session in no room is also held forever.
4. The room budget (`roomsPerSecond`=0.2, burst 5) sits on the session, not the address.
   Reconnecting gets a fresh budget, and one connection holds roughly one lobby, so about 1000
   connections from one host fill `MaxRooms`.

**Scenario.** A 20-line script on one laptop opens 2000 sockets and sends nothing. Every real
player's browser now gets `503 server full` on `/ws` and sits on "Đang kết nối…" until the
script stops. The variant that opens 1000 lobbies makes every "create room / bot game / quick
match" answer `server_full`. It needs no bandwidth, no amplification and no skill. Under this
threat model availability is the asset, so this is the finding that matters most.

**Fix (small).**
- Add a handshake deadline: in `session.run`, arm `time.AfterFunc(helloTimeout, ...)` (for
  example 10 s) that calls `s.close()` unless the handshake finished. Stop it in `handleHello`.
  `greeted` is dispatch-only, so either stop the timer there or read an `atomic.Bool`. Add a
  test next to the limits tests.
- Deployment: set `NOITU_TRUSTED_PROXIES` to the Traefik network (see M1) and
  `NOITU_MAX_CONNECTIONS_PER_IP` to a CGNAT-tolerant value (for example 32). Consider
  making a non-zero per-IP default apply automatically whenever `TrustedProxies` is non-empty.
  That keeps the NAT reasoning intact, because the cap only applies once the address is the
  real client.
- Key the room-creation budget on the address as well, the same way `joinLimiter` is keyed:
  a `hub.roomLimiter *keyedLimiter` alongside the per-session bucket, swept by
  `sweepLimiters`.
- Optional: close a greeted session that has sat in no room and sent no frame for, say,
  15 minutes.

### M1 (Med, fix now): the documented proxies are nginx and Caddy, the real one is Traefik

**Where.** `docs/deployment.md:93-176`. No mention of Coolify or Traefik anywhere in `README.md`
or `docs/`.

**What is wrong.** The client-address section is correct, but it only tells an operator what
to do for nginx and Caddy. The deployment target is Coolify/Traefik. If
`NOITU_TRUSTED_PROXIES` is unset there (the default), the docs themselves say the result:
every player shares Traefik's address, so they share one join bucket (`joinsPerSecond`=5,
burst 20), and H1's per-IP cap cannot be enabled.

**Scenario.** One client sends `JoinRoom` with random codes at 5/s, well under the 20/s frame
limit, so it is never disconnected. Every other player trying to join a friend's room by code
gets `too_many_attempts` for as long as the loop runs. The same client can also brute-force
room codes with the whole server's budget.

**Fix.** Add a short "Coolify / Traefik" subsection:
- Traefik, with its default `forwardedHeaders` (no `trustedIPs`), strips client-sent
  `X-Forwarded-*` and appends the real peer, so it is safe to trust. Set
  `NOITU_TRUSTED_PROXIES` to the Docker network the Traefik container reaches the app on
  (`docker network inspect coolify`, or the app's own network), never a public range.
- If Cloudflare or another CDN sits in front, either configure Traefik
  `forwardedHeaders.trustedIPs` for the CDN ranges or list them here too. Otherwise every
  player keys on a CDN edge address.
- Then set `NOITU_MAX_CONNECTIONS_PER_IP`.
- Confirm in the live Coolify app that this is actually set (unresolved question 1).

### M2 (Med, fix now): health checks and drain do not fit the Coolify lifecycle

**Where.** `Dockerfile:67-87` (distroless, no `HEALTHCHECK`), `docs/deployment.md:219-271`.

**What is wrong.**
1. Coolify runs its dashboard HTTP health check from **inside** the container with
   `curl`/`wget` (per the Coolify health-check docs). The distroless image has neither, so
   the check fails. The operator then has to disable it, and without it a rolling update does
   not wait for the new container before removing the old one.
2. The docs suggest `NOITU_DRAIN_TIMEOUT=60s` but never mention that the container runtime
   stop grace is what actually bounds it. Docker defaults to 10 s; check what Coolify uses.
   Past the grace the process gets SIGKILL: no `server_restarting` notice, no final log line.
   That is exactly the behaviour drain mode was built to remove.
3. `/readyz` flipping to 503 only helps if something polls it. Traefik's Docker provider
   keeps routing to a still-running container unless a Traefik health check is configured,
   so during a drain new players can land on the old instance and be refused with
   `server_restarting` while the new one is already up.

**Fix.**
- Add a `-healthcheck` mode to `noitu-server` (a GET to `http://127.0.0.1$NOITU_ADDR/healthz`,
  exit 0/1), plus
  `HEALTHCHECK CMD ["/app/noitu-server","-healthcheck"]` in the Dockerfile. It works on
  distroless and Coolify honours a Dockerfile `HEALTHCHECK`.
- Document: keep `NOITU_DRAIN_TIMEOUT` below the stop grace (or raise the grace in Coolify),
  and optionally point a Traefik load-balancer health check
  (`traefik.http.services.<svc>.loadbalancer.healthcheck.path=/readyz`) at `/readyz`.

### M3 (Med, fix now): the dependency-update bot is configured but not running

**Where.** `.github/dependabot.yml` exists on `dev` only (added in `8223f40`, 2026-09-21);
`origin/main` has no such file. Repo settings: vulnerability alerts disabled, Dependabot
security updates disabled, secret scanning and push protection disabled.

**What is wrong.** Dependabot version updates read their configuration from the default
branch only, so none of the four ecosystems is being watched. The house rule ("moving tag
over exact pin, a bot is the mechanism that rule assumes", `dependabot.yml:1-4`) is not
actually being served. Evidence: every workflow is on `checkout@v4`, `setup-go@v5`,
`setup-node@v4` and `upload-artifact@v4`, while v7 of each shipped in April-July 2026. There
are no open PRs.

**Scenario.** The next Go stdlib or `coder/websocket` advisory, or a Node base-image CVE,
arrives and nothing opens a PR or an alert. The repo is public, so these features are free.

**Fix.** Merge `dev` into `main` (or cherry-pick `dependabot.yml`). Enable Dependabot alerts,
security updates, secret scanning and push protection in the repo settings. Then accept the
actions major bumps Dependabot proposes. The groups only batch minor/patch, so majors arrive
as individual PRs, which is correct.

### L1 (Low, fix now): IPv6 keys on /128

**Where.** `server/internal/wsapi/server.go:268-291` (`clientIP` returns the address verbatim),
`server.go:336-342`.

**What is wrong.** Every per-address control (join limiter, per-IP connection cap, and the
per-IP room budget recommended in H1) keys on the literal address. A single IPv6 host
normally controls a /64 (2^64 addresses), and source-address rotation is trivial.

**Scenario.** With 1000 live rooms the chance of guessing a code is about 1.1e-6 per
attempt. One /64 at 2000 sockets x 5 joins/s finds a stranger's private lobby every couple of
minutes, and fills the per-IP cap once per address. The impact is limited, because the host
can kick and joins are refused mid-game, but it voids the limiter's "centuries" claim
(`session.go:53-61`).

**Fix.** Add a `limiterKey(ip string) string` that returns the IPv4 address unchanged, maps
IPv4-mapped IPv6 back to IPv4, and returns the `/64` prefix string for IPv6. Use it for
`remoteIP` and `reserveIP`/`releaseIP`. This is about 10 lines plus a table test.

### L2 (Low, fix now): corpus log lines are bounded per session, not in total

**Where.** `room_game.go:223` and `:239` (`recordRejection` on every rejection, including
not-your-turn), `room_game.go:288-300`, `dispatch.go:343-344`.

**What is wrong.** Each rejected submission writes one Info line. The per-session submit
limiter (5/s) is the only bound. The 20-distinct-reports cap is per session and resets on
reconnect. Rejections do not eliminate a player, so a bot game can be fed garbage
indefinitely.

**Scenario.** 2000 sockets x 5 rejected words/s is about 10k lines/s. Even with the host's
Docker log rotation (Coolify normally sets `max-size`, so verify it), the real corpus signal
the lines exist for is rotated out within minutes. Without rotation, the disk fills.

**Fix.** Put one process-wide `bucket` (for example 20 lines/s, burst 100) in front of both
log calls. Count suppressed lines in a new `noitu_corpus_log_suppressed` expvar so an
operator can see it happened. The metrics stay exact; only the log is sampled. Optionally
drop the not-your-turn reason from the corpus log, since it says nothing about the dictionary.

### L3 (Low, fix now): no security headers

**Where.** `server/internal/wsapi/server.go:209-247` (static handler), `:111-131`.

**What is wrong.** Responses carry none of `X-Content-Type-Options`, a framing policy, or
`Referrer-Policy`. Traefik/Coolify add none by default.

**Scenario.** A hostile page frames the game and overlays a decoy to trick a host into
clicking "kick" or "resign". Impact is low because there are no accounts and nothing of
value, but the fix is three lines.

**Fix.** In `mountStatic`'s handler (and the small text endpoints) set
`X-Content-Type-Options: nosniff`, `Content-Security-Policy: frame-ancestors 'self'` and
`Referrer-Policy: no-referrer`. A full script CSP can come later through SvelteKit's `kit.csp`
(hash mode works for prerendered pages). Leave HSTS to the proxy, and document that.

### L4 (Low, fix now): archived action in the proto workflow

**Where.** `.github/workflows/proto.yml:213-215`.

**What is wrong.** `bufbuild/buf-setup-action` is archived: no fixes and no Node runtime
bumps. Buf's replacement is `bufbuild/buf-action`.

**Fix.** `uses: bufbuild/buf-action@v1` with `setup_only: true` (and `version: latest` if you
want to keep that behaviour). Keep the existing `buf lint` / `buf breaking` / `buf generate`
steps. Moving major tag, in line with the house rule.

### L5 (Low): licence files in the image and the UI credit

**Where.** `Dockerfile:73-79`, `ci.yml:162`, `web/src/lib/components/AttributionFooter.svelte:10-23`.

**What is met (verified).** The CC BY-SA 4.0 obligation for the data is handled well:
`data/LICENSE` (full 4.0 text), `data/ATTRIBUTION.md` (source, licence, dated provenance via
SHA-256 in `meta`, and a nine-item modification list) and `NOTICE` are copied into the image.
`.dockerignore`'s `*.md` exclusion correctly re-includes `data/ATTRIBUTION.md`. CI asserts all
three plus the database, and asserts that no `.bz2`/`.xml` reached the image. The startup log
prints the licence from `meta`. A credit footer with links to vi.wiktionary.org and the CC BY-SA
4.0 deed is on every page (`+layout.svelte`), and chat and definitions are rendered by
interpolation, not `{@html}`.

**Gaps.**
1. The root Apache-2.0 `LICENSE` is not copied. `NOTICE` says "See the LICENSE file" and the
   image does not contain it. Apache-2.0 section 4(a) asks redistributors to include it. This
   only bites if the image is ever published or redistributed, but it is one line.
2. No third-party notices ship. The binary statically links BSD-3 (`x/text`, `protobuf`,
   `modernc.org/sqlite`), ISC (`coder/websocket`) and others. The web bundle contains Svelte's
   MIT runtime and `@bufbuild/protobuf` (Apache-2.0). Those licences ask for the notice in
   binary redistributions. Document as accepted while the image is not published; generate a
   `THIRD_PARTY_NOTICES` if it ever is.
3. CC BY-SA 4.0 section 3(a)(1)(B) asks to indicate that the material was modified. The
   footer says "dựa trên" (based on), which arguably does that, but the modification record is
   only reachable from the repository. Section 3(a)(2) allows satisfying this with a URI, so
   add a third link in the footer to `data/ATTRIBUTION.md` on GitHub (for example
   "những thay đổi").

**Fix.** `COPY LICENSE /app/LICENSE`, add `app/LICENSE` to the CI `for required in ...`
list, add the footer link, and note item 2 as accepted in the README licence section.

### L6 (Low, fix now): CI verifies a toolchain that does not ship

**Where.** `ci.yml:27-29`, `ci.yml:97-100`, `proto.yml:217-220` (`go-version-file: server/go.mod`
with `go 1.25.0` and no `toolchain` line); `Dockerfile:19` (`golang:1-alpine`, Go 1.27.x today).

**What is wrong.** CI tests and lints on Go 1.25.0. That version carries stdlib advisories
fixed in later 1.25.x releases (it would fail a govulncheck of its own stdlib). The shipped
binary is built with whatever `golang:1` is. Race and vet results come from a different
compiler and stdlib than production. No workflow runs `govulncheck` or `npm audit`.

**Fix.** Use `go-version: stable` (a moving tag, matching `golang:1`) while keeping `go 1.25.0`
as the module minimum. Add a `govulncheck` step (`golang/govulncheck-action@v1` or
`go run golang.org/x/vuln/cmd/govulncheck@latest ./...`) and `npm audit --omit=dev` in the
web job.

### L7 (Low, fix now): repository Actions defaults

**Where.** Repo settings: `default_workflow_permissions: write`,
`can_approve_pull_request_reviews: true`.

**What is wrong.** Both current workflows declare `permissions: contents: read`, so they are
fine today. Any future workflow that forgets the block gets a write token and can approve its
own PRs.

**Fix.** Settings, then Actions, then Workflow permissions: read-only, and untick "Allow
GitHub Actions to create and approve pull requests".

### L8 (Low, fix now): no idle timeout on the public listener

**Where.** `server/cmd/noitu-server/main.go:104-108`.

**What is wrong.** Only `ReadHeaderTimeout` is set. Idle keep-alive HTTP connections (not
WebSockets) are never reaped. Behind Traefik this is mostly Traefik's pool, but a direct
exposure (a Coolify port mapping, a dev box) lets idle sockets accumulate.

**Fix.** Add `IdleTimeout: 120 * time.Second`. Do **not** add `ReadTimeout`/`WriteTimeout`:
net/http leaves those deadlines on a hijacked connection, and `coder/websocket` would inherit
them and drop every game after that interval.

### Nits

- **N1** `nickname.go:62-72`: `unicode.IsPrint` accepts letters that render as blank (U+3164
  HANGUL FILLER, U+115F/U+1160, U+FFA0, U+2800 BRAILLE BLANK). The result is a nickname or
  chat line that looks empty, or one that impersonates "Người chơi" plus padding. Either strip
  that short list in the `strings.Map` or require at least one rune that is neither space nor
  in it. Low value; document if not fixed.
- **N2** `main.go:168-175`: in a container, `NOITU_DEBUG_ADDR=:6060` is reachable from every
  container on the same Docker network, which in Coolify can be the shared `coolify`
  network. expvar serves the command line and memstats. Add one sentence to the Observability
  section.
- **N3** `.dockerignore`: add `.claude` and `**/.env*`. `web/.claude/` is currently copied into
  the `web` build stage by `COPY web/ ./`. It does not reach the final image, but it is in the
  build context and cache.
- **N4** `actions/checkout` defaults to `persist-credentials: true`, writing the (read-only)
  token into `.git/config` before `npm ci` runs dependency install scripts. Set
  `persist-credentials: false`. `proto.yml` needs no push, so it can take the same setting.
- **N5** `Dockerfile:67`: `gcr.io/distroless/static-debian13:nonroot` is the current base.
  For a static binary the difference is only CA certificates and tzdata. Dependabot will not
  propose this because the suite is in the image name.
- **N6** `session.go:334-343`: client-sent WebSocket ping control frames are answered inside
  `coder/websocket` and never reach `frameLimiter`. The cost is one small pong per ping,
  bounded by TCP. Non-issue.

### Dependency advisories

| Advisory | Package (installed) | Severity | Reachable? | Action |
|---|---|---|---|---|
| GHSA-pxg6-pf52-xh8x | `cookie@0.6.0` via `@sveltejs/kit@2.70.3` (and `adapter-static@3.0.10`) | Low | **No.** The frontend is prerendered by `adapter-static`; the image copies only `web/build` and the Go binary serves the files. No SvelteKit server runtime, and so no `cookie.serialize`, exists in production. | Non-issue. Do **not** run `npm audit fix --force`: its proposed "fix" is `@sveltejs/kit@0.0.30`. If a clean audit is wanted, add `"overrides": {"cookie": "^0.7.0"}` (house rule: security pins live in `overrides`). |
| GHSA-82fw-gwwq-j7x9 | `vitest@3.2.7` / `@vitest/mocker` (range 2.1.0 to 4.1.10) | Moderate | **No** in production (devDependency, test runner only, not in the image). Relevant only on a developer machine or CI running the test server. | Non-issue for the service. Take the vitest major bump when Dependabot offers it (once M3 is fixed). |
| Go modules and stdlib | all | none | govulncheck v1.8.0, DB 2026-09-28: no vulnerabilities in the call graph | Clean. Routine bumps available (`modernc.org/sqlite` 1.60.1, `x/text` 0.42.0, ...), which Dependabot will propose. |

## Checked and clean

- **Origin check.** `coder/websocket` defaults to same host (`authenticateOrigin` compares
  `Origin` host with `r.Host`, and Traefik preserves Host). With no cookies or ambient auth,
  cross-site WebSocket hijacking has nothing to steal anyway. Permessage-deflate is disabled
  by default in v1.8.15, so there is no decompression bomb.
- **Frame limits.** `SetReadLimit(4096)`; text frames rejected; unparseable frames close the
  socket; a flat 20/s (burst 40) frame limiter closes floods; per-action budgets for
  submit, chat, lobby actions, reports and joins.
- **Memory per connection and room.** Outbox capped at 32 frames (session closed, never
  grown); chat delivered with `trySend`; chat history 20 lines x 200 runes; reported words at
  most 20 per session; `connsByIP` entries deleted at zero; `keyedLimiter` swept every 5 min;
  resume tokens expire after the grace window; pre-Hello sockets are never registered in the
  hub; quick-match queue cleaned on disconnect; bounded room inbox. Nothing grows without a
  bound other than through the ceilings in H1.
- **Room codes.** `crypto/rand` with rejection sampling, 31^6 (about 29.7 bits), fine for
  a lobby code given the join limiter (modulo L1). Session IDs and resume tokens are 128-bit
  `crypto/rand`.
- **Nickname and chat sanitisation.** NFC first, strips Cc/Cf (bidi overrides, zero-width
  characters) and anything non-printable, caps combining marks at 2, collapses whitespace,
  rune caps (20/200/64); fuzzed. Rendered by interpolation, never `{@html}` (`ChatPanel.svelte:182`).
- **Logging.** Only normalised, capped words go into `word_rejected`/`word_reported`; no
  nickname, chat or raw input. `slog` TextHandler quotes values, so no log injection. Accept
  rejections log at Debug only. Client errors are UI keys, never internal strings.
- **Trusted proxies.** Right-to-left `X-Forwarded-For` walk, skipping trusted hops, falling
  back to the peer on a malformed hop, IPv4-mapped handling via `Unmap`, unparseable entries
  warned and skipped. Opt-in and documented honestly.
- **Debug endpoint.** Separate `http.Server`, only when `NOITU_DEBUG_ADDR` is set. The public
  handler is its own mux, so expvar's `DefaultServeMux` registration is never exposed (test
  `TestDebugVarsNotOnPublicMux`).
- **Static serving.** Path-boundary check (`underRoot`), directories fall to the SPA shell (no
  listing), immutable caching only under `/_app/immutable/`. `/version` exposing a
  `git describe` string is a non-issue for an open-source project.
- **Env handling.** Invalid values are logged and fall back; none of the variables is a
  secret, so logging the raw value is fine.
- **Image.** Multi-stage; distroless `static`, `nonroot` user, `CGO_ENABLED=0`, `-trimpath`;
  `.git` excluded from the context; upstream dump never shipped (asserted in CI). The unpinned
  dump over HTTPS with SHA-256 recorded in `meta` is a documented, accepted decision: a
  poisoned dump could only inject text that is rendered safely.
- **Workflows.** Top-level `permissions: contents: read` on both; no `pull_request_target`,
  no `secrets.*`, no step that pushes, tags, publishes or comments; the only expression
  interpolated into `run:` is `github.event_name` (not attacker-controlled); moving major
  tags throughout (per house rule; no SHA pinning recommended).
- **Makefile.** No secrets; the `.part` download plus rename is atomic; `VERSION` comes from
  the repository's own tags.

## Recommended order

1. H1: Hello deadline, per-IP room budget, and (with M1) turn on trusted proxies plus a
   per-IP cap in the live Coolify app.
2. M3: get `dependabot.yml` onto `main` and switch on the free GitHub security features.
3. M2: `-healthcheck` mode plus `HEALTHCHECK`; document the stop grace against the drain
   timeout.
4. L1, L2, L3, L8: each is under 20 lines in `wsapi`/`main.go`.
5. L4, L6, L7, N3, N4: workflow and settings hygiene.
6. L5: `COPY LICENSE`, CI assertion, footer link to the modification record.

## Unresolved questions

1. Does the live Coolify application set `NOITU_TRUSTED_PROXIES` and
   `NOITU_MAX_CONNECTIONS_PER_IP`? I did not inspect the deployment. If it does not, M1's
   shared join bucket is live today.
2. What stop grace does Coolify give this container, and what is `NOITU_DRAIN_TIMEOUT` set to
   there? This decides whether M2's SIGKILL case happens on every deploy.
3. Is Cloudflare (or another CDN) in front of Traefik? That changes which ranges must be
   trusted.
4. Is the built image ever pushed to a registry, or only built by Coolify on the host? This
   decides whether L5 item 2 is "accepted" or "fix".
