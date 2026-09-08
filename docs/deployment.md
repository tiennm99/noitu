# Deployment

The whole game is one binary. It serves the WebSocket API, the built frontend,
and a health check, and it reads a single database file at startup. The
supported shape is the container image behind a reverse proxy that terminates
TLS.

## Configuration

Every setting is an environment variable and every one has a working default,
so the image runs with nothing set.

| Variable | Default | Meaning |
|---|---|---|
| `NOITU_ADDR` | `:8080` | Listen address |
| `NOITU_DB_PATH` | `data/noitu.db` | Derived dictionary, opened read-only at startup |
| `NOITU_TURN_LIMIT` | `20s` | Turn deadline, identical for bot and online games |
| `NOITU_GRACE` | `30s` | How long a disconnected player's seat is held for a reconnect |
| `NOITU_ALLOWED_ORIGINS` | *(unset)* | Comma-separated origin allowlist. Unset means same-origin only |
| `NOITU_WEB_DIR` | *(unset)* | Built frontend to serve. Unset serves the API alone |

An invalid duration is logged and ignored rather than silently changing the
rules of the game.

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
docker build -t noitu:latest .
docker run -p 8080:8080 noitu:latest
```

The build downloads the ~62 MB upstream Wiktionary export in a builder stage and
derives the ~2 MB database the game uses. Only the derived file is copied into
the final image, so the upstream export never ships. The result is a
distroless image of about 25 MB running as a non-root user.

Passing `--build-arg FIXTURE_DICT=1` builds the same image against the
checked-in word sample instead. It produces a playable but tiny dictionary and
exists so the image can be tested without the download; do not ship it.

### What travels with the data

The derived wordlist is CC BY-SA 4.0 while the code is Apache-2.0, so the image
carries `data/LICENSE`, `data/ATTRIBUTION.md` and `NOTICE` alongside it. CI
asserts all three are present, and that the upstream file is not. Removing them
would put the image out of compliance.

## Behind a reverse proxy

The socket is a normal HTTP upgrade, but three settings are easy to get wrong
and each one breaks the game in a way that looks like something else.

**Forward the upgrade.** Without `Upgrade` and `Connection` the handshake
returns 400 or 502 and the page sits on "Đang kết nối…" forever.

**Set a read timeout longer than the keepalive.** The server pings every 20
seconds. A proxy that closes idle connections sooner will cut players off mid
game, and it will look like a client bug because the server logs a clean close.

**Turn response buffering off.** A proxy that buffers will hold frames until it
has enough to flush, which turns a 20-second turn into a guess.

nginx:

```nginx
location /ws {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_read_timeout 120s;
    proxy_send_timeout 120s;
    proxy_buffering off;
}

location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
}
```

Caddy needs none of this: it forwards upgrades and streams by default.

```caddy
noitu.example {
    reverse_proxy 127.0.0.1:8080
}
```

### The client's own address

Rate limiting counts against `RemoteAddr` and deliberately ignores
`X-Forwarded-For`, because that header is attacker-controlled unless the proxy
overwrites it. Behind a proxy every player therefore shares one bucket. If that
becomes a problem, the fix is to make the proxy the only source of the header
and teach the server to trust it — not to trust it as things stand.

## Health check

`GET /healthz` returns 200 once the dictionary has loaded. It does not report
on live games, so it is a liveness check rather than a readiness one.

```sh
curl -fsS https://noitu.example/healthz
```

A post-deploy check worth having is a real socket open, because the health
check passes whether or not the proxy forwards upgrades. Opening the site and
starting a game against the bot is the shortest version of that.

## What a restart costs

Rooms are in memory. A restart ends every live game, and players are told the
server is restarting rather than being left waiting. Deploy when the game is
quiet, or accept that the games in flight are lost — there is no session
persistence, by design, in this version.

## Updating the dictionary

The wordlist is a build artifact, not runtime state, and the upstream export is
fetched fresh rather than pinned: rebuilding the image picks up whatever
kaikki.org currently serves, and the database's `meta` table records the
SHA-256 of the file it was built from. To update the dictionary, rebuild and
redeploy. To change the source itself, update `DICT_URL` in the `Dockerfile`,
the `Makefile` and the builder's constant (a test asserts the three agree),
then record what changed in `data/ATTRIBUTION.md`.

Nothing migrates, because nothing persists.
