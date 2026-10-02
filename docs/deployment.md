# Deployment

The whole game is one binary. It serves the WebSocket API, the built frontend,
and its own health, readiness and version endpoints, and it reads a single
database file at startup. The supported shape is the container image behind a
reverse proxy that terminates TLS.

## Configuration

Every setting is an environment variable and every one has a working default,
so the image runs with nothing set.

| Variable | Default | Meaning |
|---|---|---|
| `NOITU_ADDR` | `:8080` | Listen address |
| `NOITU_DB_PATH` | `data/noitu.db` | Derived dictionary, opened read-only at startup |
| `NOITU_TURN_LIMIT` | `30s` | Turn deadline, identical for bot and online games |
| `NOITU_GRACE` | `30s` | How long a disconnected player's seat is held for a reconnect |
| `NOITU_ALLOWED_ORIGINS` | *(unset)* | Comma-separated origin allowlist. Unset means same-origin only |
| `NOITU_WEB_DIR` | *(unset)* | Built frontend to serve. Unset serves the API alone |
| `NOITU_TRUSTED_PROXIES` | *(unset)* | Comma-separated proxy addresses or CIDRs whose `X-Forwarded-For` is believed. Unset keys limiters on the socket peer |
| `NOITU_MAX_ROOMS` | `1000` | Ceiling on live rooms across the process; a creator past it is told `server_full` |
| `NOITU_MAX_CONNECTIONS` | `2000` | Ceiling on open WebSockets; the next upgrade gets HTTP 503 |
| `NOITU_MAX_CONNECTIONS_PER_IP` | `0` (off) | Ceiling on open WebSockets from one address; the next upgrade from it gets HTTP 503 |
| `NOITU_DEBUG_ADDR` | *(unset)* | Separate listen address for `GET /debug/vars` (expvar counters). Unset means the counters exist in the process but nothing serves them |
| `NOITU_DRAIN_TIMEOUT` | `0s` | How long a shutdown waits for live games to finish before ending them anyway; see "Draining on deploy" below |

An invalid duration or count is logged and ignored rather than silently
changing the rules of the game.

One timing is not configurable: an online room closes after 10 minutes in its
lobby with no game started. It is a fixed constant because nothing about a
deployment should change how long two people have to agree on a game, and a
running game is bounded by the turn clock rather than by this. Chatting
deliberately does not reset that window — talking is not playing, or a room
could be held open for the life of the process by one message every nine
minutes.

Chat's own bounds are fixed constants for the same reason: a room keeps its
last 20 messages and one message is capped at 200 runes
(`server/internal/wsapi/room.go`).

The image sets `NOITU_ADDR`, `NOITU_DB_PATH` and `NOITU_WEB_DIR` for you.

### Origins

Leave `NOITU_ALLOWED_ORIGINS` unset when the binary serves the frontend, which
is the normal case: the page and the socket share an origin and the browser's
own check is enough. Set it only when the frontend is served from somewhere
else, and then list exactly those origins. An allowlist that is wrong in the
permissive direction lets any page open a socket as one of your players.

## The image

```sh
docker build --build-arg VERSION="$(git describe --tags --always --dirty)" -t noitu:latest .
docker run -p 8080:8080 noitu:latest
```

`make image` runs the same build with `VERSION` filled in for you; see
"Version" below.

The build turns the committed corpus, `data/dictionary.txt`, into the ~7 MB
database the game uses in a builder stage, so it downloads nothing from
Wikimedia. Only the database is copied into the final image. The result is a
distroless image of about 25 MB running as a non-root user.

### Version

`GET /version` answers with the build's version, and the same string opens
the startup log line — the fastest way to confirm a deploy actually replaced
the running process rather than restarted the old one. It comes from
`-X main.version=...` at link time, populated from `git describe --tags
--always --dirty`. `make server` runs that command directly; the Dockerfile
cannot — `.dockerignore` deliberately keeps `.git` out of the build context,
so a stale copy never ships in the image — so it takes the version as the
`VERSION` build-arg instead, which `make image` supplies. Building the image
directly with `docker build .` and no `--build-arg VERSION=...` reports
`dev`, which is an honest answer for an unstamped build rather than a wrong
one.

### What travels with the data

The derived wordlist is CC BY-SA 4.0 while the code is Apache-2.0, so the image
carries `data/LICENSE`, `data/ATTRIBUTION.md`, `NOTICE` and the Apache-2.0
`LICENSE` that `NOTICE` refers to. CI asserts all four are present, and that
the upstream file is not. Removing them would put the image out of compliance.

The image does not carry a notice file for the third-party Go and JavaScript
dependencies. That is accepted while the image is only built and run by the
operator; it must be generated before an image is ever published or handed to
someone else.

## Behind a reverse proxy

The socket is a normal HTTP upgrade, but three settings are easy to get wrong
and each one breaks the game in a way that looks like something else.

**Forward the upgrade.** Without `Upgrade` and `Connection` the handshake
returns 400 or 502 and the page sits on "Đang kết nối…" forever.

**Set a read timeout longer than the keepalive.** The server pings every 20
seconds. A proxy that closes idle connections sooner will cut players off mid
game, and it will look like a client bug because the server logs a clean close.

**Turn response buffering off.** A proxy that buffers will hold frames until it
has enough to flush, which turns a 30-second turn into a guess.

nginx:

```nginx
location /ws {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_read_timeout 120s;
    proxy_send_timeout 120s;
    proxy_buffering off;
}

location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```

Caddy needs none of this: it forwards upgrades and streams by default.

```caddy
noitu.example {
    reverse_proxy 127.0.0.1:8080
}
```

### Coolify and Traefik

Coolify fronts every app with Traefik. With its default forwarded-headers
settings Traefik discards any `X-Forwarded-For` the client sent and appends the
real peer, so it is safe to trust. Left at the defaults of this server it is
not trusted, and the whole player base shares one client address: one join
bucket, and no way to switch the per-address connection cap on. Set these on
the app's environment variables:

| Variable | Value | What it changes |
|---|---|---|
| `NOITU_TRUSTED_PROXIES` | the subnet Traefik reaches the app on, for example `10.0.1.0/24` | `X-Forwarded-For` is believed when the socket peer is inside this range, so join limiting and the per-address cap key on the real player. Find the range with `docker network inspect coolify` (or the app's own network, if Coolify put it on one) and use the narrowest CIDR that contains the Traefik container. Never a range the public can connect from |
| `NOITU_MAX_CONNECTIONS_PER_IP` | `32` as a starting point | One address may hold at most this many open sockets. Off (`0`) by default because it is only meaningful once the address above is the real client; 32 leaves room for a household or campus NAT. Without it one host can hold every socket up to `NOITU_MAX_CONNECTIONS` |
| `NOITU_DRAIN_TIMEOUT` | below the container stop grace; see "Draining on deploy" | Live games get this long to finish on redeploy instead of ending at once |

Variables take effect on the next deploy. An entry in `NOITU_TRUSTED_PROXIES`
that does not parse is logged as a warning at startup and skipped, so read the
startup log after redeploying; a range that is too narrow to contain Traefik
fails quietly, back to one shared address.

If Cloudflare or another CDN sits in front of Traefik, Traefik itself must
trust that CDN's ranges (`forwardedHeaders.trustedIPs` on the entrypoint),
otherwise it overwrites the header with the CDN edge address and every player
behind the same edge shares one bucket again.

To make Traefik stop routing to an instance that has started draining, give the
service a load-balancer health check on `/readyz` through Coolify's custom
labels: `traefik.http.services.<service>.loadbalancer.healthcheck.path=/readyz`
and `...healthcheck.interval=5s`, using the service name Coolify generated for
the app (visible in the container's labels). Without it Traefik keeps sending
new players to the old container until it is removed, and they are refused with
`server_restarting`.

### The client's own address

Rate limiting keys on the client's address, and by default that is the
socket's own peer, `RemoteAddr`. `X-Forwarded-For` is attacker-controlled
unless the proxy is known to append to it, so it is ignored until told
otherwise. Behind a proxy every player therefore shares one bucket, which
means one client brute-forcing room codes spends everybody's join budget.

The fix is to name the proxy. Set `NOITU_TRUSTED_PROXIES` to the address, or
CIDR range, the proxy connects from — `127.0.0.1` for the nginx and Caddy
examples above, or the container network's range under Compose — and the
server walks `X-Forwarded-For` from the right, taking the first hop that is
not itself a trusted proxy. Entries a client forged sit to the left of the one
the proxy appended, so they are never reached. A peer that is not on the list
is still keyed on its socket address, header or not.

Caddy appends the real client to `X-Forwarded-For` by default. nginx does
not: without the `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for`
lines in the snippet above it passes the client's own header through
untouched, and naming that proxy as trusted would let every client pick its
own limiter key. Do not list a range the public can connect from either; that
is the same as trusting the header unconditionally.

### Capacity

Two ceilings bound the process as a whole, on top of the per-connection rate
limits: `NOITU_MAX_ROOMS` live rooms and `NOITU_MAX_CONNECTIONS` open
sockets. Past the first, creating a room answers `server_full` and the player
is asked to wait; past the second, the upgrade itself is refused with HTTP 503
so the proxy can count it. Each connection also has a frame-rate ceiling, and a
client past it is disconnected rather than throttled. The defaults are
generous for one binary on a small host; lower them if memory is tight,
because a room is a goroutine and an engine held for up to its idle window.

Neither ceiling can be held by one client alone. A socket that has not sent
its `Hello` within ten seconds is closed, so an idle socket cannot sit on a
connection slot, and the room-creation budget is charged to the client
address as well as to the socket, so reconnecting does not refill it. The
per-address budget is deliberately wide (a burst of 30, refilling at one room
every two seconds), because without `NOITU_TRUSTED_PROXIES` every player
behind the proxy shares it.

A third ceiling, `NOITU_MAX_CONNECTIONS_PER_IP`, bounds how many of those
sockets one address may hold at once, and it is off by default. Turning it on
is safe only once the client's own address (above) is the real one: behind a
reverse proxy with `NOITU_TRUSTED_PROXIES` unset, every player shares the
proxy's own address, and the cap would seat one of them and refuse the rest.

## Observability

Set `NOITU_DEBUG_ADDR` to a second listen address — one that is not the one
players reach, and never a public interface — to expose `GET /debug/vars`
there: standard-library [`expvar`](https://pkg.go.dev/expvar), zero extra
dependencies, a JSON object of process counters refreshed on every write. It
is never mounted on the public address, unset or not, so leaving
`NOITU_DEBUG_ADDR` unset is the same as not having it. Besides the counters
below, expvar always publishes the process's full command line and its
runtime memory statistics; that is the standard library's own doing, not
something this server adds, and it is the whole reason `/debug/vars` lives on
a separate address rather than a route on the public mux one config change
could expose. In a container, binding it to `0.0.0.0` (`:6060`) makes it
reachable from every other container on the same Docker network, which on
Coolify can be the shared proxy network. Bind it to `127.0.0.1:6060`,
which only the container itself can reach, or leave it unset; do not put it on
a wildcard address on a shared network. The counters, all prefixed `noitu_`: connections open and
total; rooms live and total, each split `bot`/`pvp`; games started and
finished the same way; words submitted, accepted, and rejected by reason;
eliminations by reason; chat lines; join attempts refused, by whether it was
the rate limit, an unknown code, or a full room; bot moves by difficulty; dead-
end claims by whether the position actually had no legal move; words reported
as real by `ReportWord`; resumes attempted versus succeeded; and
`word_rejected`/`word_reported` log lines dropped by the process-wide
rate limit on them (`noitu_corpus_log_suppressed`). None of it is
read by the game itself — it is a second write next to a decision already
made, not an input to one.

Every rejected word also gets one structured log line at `Info`,
`word_rejected`, carrying `reason`, `word`, `link` (the syllable it had to
start with), `mode` (`bot`/`pvp`) and `room`. `word` is never the raw text a
player typed — it is normalized the same way the engine matches it (NFC,
lowercase, single-spaced) and capped at 64 runes — so the line is safe to
collect and is exactly the corpus-review question this project has open:
which words players type that the dictionary does not have. Nothing else a
player types is logged: not chat, not a nickname, not an accepted word.

A player who disputes a rejection this way — `ReportWord` — gets the same
treatment: one `Info` line, `word_reported`, carrying `word` (normalized the
same way), `link` (the syllable in play, empty when the report was not filed
mid-game), `mode` (`bot`/`pvp`/`none`) and `room`. Only words of at least two
syllables are recorded, and a session may file at most 20 distinct ones — never
the player's nickname, on both counts for the same reason `word_rejected`
never carries one. Between the two, this is the whole of the corpus dispute
loop: triaged by hand today, into whatever curated word list eventually
applies the fix.

## Health and readiness

`GET /healthz` returns 200 once the dictionary has loaded, for the rest of the
process's life. It does not report on live games, so it is a liveness check
rather than a readiness one, and it never moves — a proxy or orchestrator
using it to decide whether to kill the process must not see it fail during a
drain, because the process is still correctly finishing the games it has.

```sh
curl -fsS https://noitu.example/healthz
```

`GET /readyz` is the readiness check: 200 while the server is accepting new
rooms, 503 once it has started draining (see below). Point a load balancer's
"stop sending me new traffic" check here and its "restart me" check at
`/healthz`; pointing both at the same endpoint defeats the reason there are
two.

The image has no `curl` or `wget`, so a container health check cannot shell out
to one. The binary probes itself instead:

```sh
noitu-server -healthcheck
```

It sends `GET /healthz` to `NOITU_ADDR` (a bare `:8080`, or a wildcard host, is
dialled on `127.0.0.1`), and exits 0 on a 200 and 1 on anything else, printing
the reason to stderr. The Dockerfile's `HEALTHCHECK` runs it every 30 seconds
with a 10-second start period. It is a liveness check, so it deliberately does
not follow `/readyz` into a drain.

On Coolify, leave the dashboard's own HTTP health check switched off: it
executes `curl` or `wget` inside the container and cannot pass against this
image. Coolify picks the health check up from the Dockerfile after the next
deploy (the application then reports a custom health check found), and a
rolling update waits for the new container to be healthy before it removes the
old one. Confirm that in the deployment log after the first deploy.

A post-deploy check worth having beyond either is a real socket open, because
both health checks pass whether or not the proxy forwards upgrades. Opening
the site and starting a game against the bot is the shortest version of that.

## Draining on deploy

Rooms are in memory, so a restart has always ended every live game — but a
plain `kill` used to do that the instant the signal arrived, which is why
"deploy when the game is quiet" was the only advice this document had.
`SIGTERM` now runs a short sequence first:

1. The server stops accepting new rooms. A creator past this point is told
   `server_restarting` — the same UI key a live shutdown sends everyone else —
   rather than `server_full`, because unlike a full room this one is never
   coming back.
2. `GET /readyz` flips to 503, so a load balancer that checks it stops routing
   new players here.
3. The server waits up to `NOITU_DRAIN_TIMEOUT` for every room with a game
   *actually running* to finish on its own turn clock. A room sitting in its
   lobby does not count — it has no game a restart would cost, and waiting for
   one would make every deploy sit out somebody's abandoned tab.
4. Once every game has finished, or the timeout passes, every player still
   connected is told the server is restarting and the process shuts down as
   it always did.

After the notice goes out the process waits two more seconds, so the writes
reach the sockets before it exits, then closes its listeners.

Each step logs the room and live-game count, so "did the deploy actually
wait, and for what" is answered from the log rather than guessed at.

A second `SIGTERM` or `SIGINT` during a drain is not swallowed: the first
signal hands signal handling back to the runtime, so a second one ends the
process at once.

The container runtime, not this server, bounds the whole sequence. Docker
sends `SIGTERM`, waits its stop grace period (10 seconds by default), then
sends `SIGKILL`, which gives players no `server_restarting` notice and writes
no final log line. The rule is

    NOITU_DRAIN_TIMEOUT + 2s  <  container stop grace

so with Docker's default grace `NOITU_DRAIN_TIMEOUT` must stay at or below
about `6s`. A drain shorter than one turn only helps games in their last
seconds; to let games ride out a full `NOITU_TURN_LIMIT` (30 seconds by
default), raise the stop grace first, on Coolify wherever the application's
container stop timeout is configured, and only then raise the drain timeout.
If the grace cannot be raised, keep the drain short instead of letting the
kill land mid-drain.

The default, `NOITU_DRAIN_TIMEOUT=0s`, is today's behaviour: nothing waits,
every live game ends immediately. Setting it to something like `60s` turns
"deploy when the game is quiet" into "deploy whenever, and the games in
flight get up to a minute to finish before they are cut off anyway" — the
turn clock already bounds how long any one game can take, so a timeout a
little over `NOITU_TURN_LIMIT` covers the common case of a handful of games
mid-turn.

## Resuming from a second tab

A resume token is a bearer credential: whoever presents a live one takes the
seat, and the connection that held it before is closed. Opening the same
game in a second tab, or reloading with the token still in `localStorage`, is
therefore a takeover, not a copy — the newest connection to present the token
wins the seat, on purpose. There is no liveness check on the connection being
replaced beyond that; nothing here treats a second tab as an attack, because
the token already proves it came from the same player. A future version that
wants two tabs to share a seat, rather than fight over it, would need a
different design — this one intentionally does not.

## What a restart costs

A restart with `NOITU_DRAIN_TIMEOUT` unset, or a signal harder than `SIGTERM`,
ends every live game immediately and tells players the server is restarting
rather than leaving them waiting. There is no session persistence, by design,
in this version — a game that does not finish inside the drain window is
simply lost.

## Updating the dictionary

The dictionary is committed as text, `data/dictionary.txt`, and the image is
built from it, so the words a deploy ships are the words in the revision it
deploys. The upstream dump is not pinned: `dumps.wikimedia.org` regenerates
`viwiktionary/latest/` monthly, and the corpus header records the SHA-256 of
the dump it came from.

[`refresh-dictionary.yml`](../.github/workflows/refresh-dictionary.yml)
downloads the current dump on the 10th of every month, or on demand from the
Actions tab, regenerates the corpus and opens a pull request against `dev`.
Merging it, and `dev` onward to `main`, deploys the new words like any other
change. The pull request does not trigger CI, because GitHub does not run
workflows on a pull request opened with `GITHUB_TOKEN`; the builder verifies
the database before exporting the corpus, and CI runs again on the merge.
GitHub disables scheduled workflows after 60 days without a commit to the
repository, so re-enable it from the Actions tab if the pull requests stop.

To refresh by hand, run `make fetch-dict` and `make refresh-dict`, then commit
`data/dictionary.txt`.

To change the source itself, update `DICT_URL` in the `Makefile` and the
builder's constant (a test asserts the two agree), refresh the corpus, then
record what changed in `data/ATTRIBUTION.md`.

Nothing migrates, because nothing persists.
