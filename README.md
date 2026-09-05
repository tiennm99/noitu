# noitu

Trò chơi **nối từ** tiếng Việt trên web — chơi với máy hoặc đấu 1v1 trực tuyến.

A web implementation of the Vietnamese word-chain game *nối từ*: each player submits a
meaningful word of **at least 2 syllables** whose **first syllable matches the last syllable
of the previous word**. No word may be reused. Fail to answer in time and you lose.

```
ngôn ngữ → ngữ pháp → pháp luật → luật lệ → ...
```

## Status

In development. See [`plans/260904-1125-noi-tu-web-game/plan.md`](./plans/260904-1125-noi-tu-web-game/plan.md)
for the implementation plan and phase breakdown.

## Architecture

The server is authoritative: the browser never holds the wordlist, so word validation
cannot be bypassed, and the bot and player-vs-player paths share one rule implementation.

```
SvelteKit SPA  ──WebSocket + Protobuf──►  Go server  ──►  SQLite dictionary
```

| Component | Choice |
|---|---|
| Frontend | SvelteKit 2 / Svelte 5 (JavaScript), `adapter-static` |
| Transport | WebSocket, Protobuf binary frames (`@bufbuild/protobuf` ↔ `protoc-gen-go`) |
| Backend | Go, [`coder/websocket`](https://github.com/coder/websocket) |
| Dictionary | SQLite via `modernc.org/sqlite` (CGo-free), read-only at runtime |

### The wire contract

[`proto/noitu/v1/game.proto`](./proto/noitu/v1/game.proto) is the single source of truth for
every message crossing the WebSocket. `buf` generates the Go types into `server/gen/` and the
JavaScript types into `web/src/lib/proto/`; both trees are committed, and neither side
hand-writes a message type.

The Go test suite emits binary fixtures into `proto/testdata/`, and the JavaScript suite
decodes those same bytes — so the two generated clients are checked against one artifact
rather than against each other's assumptions. Regenerate the fixtures with
`cd server && go test ./internal/wsapi -update` whenever the schema changes.

## Setup

Requires Go 1.25+, Node 20+, and optionally `make`. [`buf`](https://buf.build/docs/installation)
is needed only to change the WebSocket schema — the generated code is committed, so
building and running the project does not require it.

```sh
make fetch-dict   # one-time: downloads the ~179 MB upstream dictionary into data/
make dict         # derives data/noitu.db (the game's wordlist) from it
make test         # run all tests
make run          # build and start the server
```

`make fetch-dict` is a one-time cost per machine. Neither database file is committed;
both are build artifacts. See [`data/ATTRIBUTION.md`](./data/ATTRIBUTION.md).

## Running the server

```sh
make dict          # once, after fetch-dict
make run           # builds and starts on :8080
```

Configuration is environment-only; every variable has a working default.

| Variable | Default | Meaning |
|---|---|---|
| `NOITU_ADDR` | `:8080` | Listen address |
| `NOITU_DB_PATH` | `data/noitu.db` | Derived dictionary, loaded read-only at startup |
| `NOITU_TURN_LIMIT` | `20s` | Turn deadline, identical for bot and PvP games |
| `NOITU_GRACE` | `30s` | How long a disconnected player's seat is held for a reconnect |
| `NOITU_ALLOWED_ORIGINS` | *(unset)* | Comma-separated origin allowlist. Unset means same-origin only |
| `NOITU_WEB_DIR` | *(unset)* | Built frontend to serve. Unset serves the API alone |

An invalid duration is logged and ignored rather than silently changing the
rules of the game.

Endpoints: `GET /ws` (Protobuf over binary WebSocket frames), `GET /healthz`,
and — when `NOITU_WEB_DIR` is set — the frontend on everything else, with
unknown paths falling back to `index.html` because deep links are client routes.

### Smoke-testing without a frontend

The whole game is playable over a raw WebSocket client. Frames are binary
protobuf, so a text tool like `websocat` cannot compose them by hand; the
practical path is a short Go client importing `server/gen/noitu/v1`, which is
exactly what `server/internal/wsapi` tests do in-process.

Send `Hello{protocol_version: 1, nickname: "..."}` first — every other message
is refused until the handshake completes — then `StartBotGame` and reply to each
`TurnUpdate` with a `SubmitWord` carrying the `turn_seq` you were given.

## Make targets

| Target | Does |
|---|---|
| `fetch-dict` | Download the upstream `dictionary.db` (~179 MB) into `data/` |
| `dict` | Derive `data/noitu.db` from the upstream database |
| `server` | Build the Go server binary |
| `web` | Build the SvelteKit frontend to static assets |
| `proto` | Regenerate the Go and JS wire types from `proto/` (needs `buf`) |
| `proto-check` | Lint the schema and verify the committed generated code is in sync |
| `test` | Run Go and JavaScript tests |
| `run` | Build and run the server locally |
| `verify-dict` | Re-check the downloaded dictionary against its pinned SHA-256 |

### Without `make`

`make` is not installed everywhere (notably Windows). Every target is a thin wrapper:

```sh
# fetch-dict
curl -L -C - -o data/dictionary.db   https://github.com/minhqnd/dictionary/releases/download/v2.0.0/dictionary.db

# dict
cd server && go run ./cmd/build-dictionary --in ../data/dictionary.db --out ../data/noitu.db

# test
cd server && go vet ./... && go test ./... -race

# server
cd server && CGO_ENABLED=0 go build -o ../noitu-server ./cmd/noitu-server

# proto (only when proto/noitu/v1/game.proto changes)
cd web && npm ci
buf generate && buf lint
```

## License

This project is distributed under **two licenses**, applying to different artifacts.
See [`NOTICE`](./NOTICE) for the full statement.

| Artifact | License |
|---|---|
| All source code (`server/`, `web/`, `proto/`) | [Apache-2.0](./LICENSE) |
| Dictionary data (`data/noitu.db`) | [CC BY-SA 4.0](./data/LICENSE) |

The dictionary is derived from [minhqnd/dictionary](https://github.com/minhqnd/dictionary)
(data licensed CC BY-SA 4.0), which itself aggregates Wiktionary and other Vietnamese
dictionary sources. CC BY-SA is a **share-alike** license: any redistribution of the derived
database — including inside a container image — must carry the same license, the attribution,
and the record of modifications recorded in [`data/ATTRIBUTION.md`](./data/ATTRIBUTION.md).

The database is loaded at runtime from a file and is never embedded or linked into the Go
binary, keeping the two licensing regimes on separate artifacts.
