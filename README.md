# noitu

Trò chơi **nối từ** tiếng Việt trên web — chơi với máy hoặc đấu trực tuyến 2–4 người.

A web implementation of the Vietnamese word-chain game *nối từ*: each player submits a
meaningful word of **at least 2 syllables** whose **first syllable matches the last syllable
of the previous word**. No word may be reused. Fail to answer in time and you lose.

```
ngôn ngữ → ngữ pháp → pháp luật → luật lệ → ...
```

Playing a word that leaves the next player nothing to answer is not itself a win. They keep
the turn and lose it to the clock like any other, and are then shown a few words the
position still had — or told it had none.

A room seats two to four. Failing a turn takes that player out rather than ending the game:
the syllable and the used words survive them, the turn passes to whoever is next, and the
last player standing wins. Two seats is that same rule seen from close up, which is why
there is one implementation of it and not two.

## Status

Playable: vs bot at three difficulties, and online rooms of two to four by room code. See
[`plans/260904-1125-noi-tu-web-game/plan.md`](./plans/260904-1125-noi-tu-web-game/plan.md)
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

### Online play

A player creates a room and gets a six-character code and an invite link. The
alphabet omits `0`/`O` and `1`/`I`/`L`, because these codes get read aloud. The
others type the code or open the link, which seats them in the room's lobby.

A room holds up to four people and needs two to start. Both numbers are server
constants sent to the client in `RoomState`, so the lobby draws whatever the
server allows and widening a room is a server change alone.

The room is a lobby that outlives its games. Whoever created it owns it; the
rest are guests. Every guest readies, the owner starts — the owner has no
readiness of their own, because starting is the same statement — and a finished
game returns everybody to the lobby, where the next one is agreed the same way.
A guest must take their readiness back before leaving, and the owner can free
the seat of any guest who is not ready, naming it rather than pointing at "the
other one". An owner who leaves hands the room to whoever is left, and the last
player out closes it, as does ten minutes with no game started.

The room also keeps the score of the series: how many games each seat has won
since the room opened, shown in the lobby and beside each player's score on the
board. It is a room fact rather than a game one, credited the moment a game
ends, and a seat that is vacated takes its tally with it — the name on it no
longer means the same person.

Joining is a lobby thing: a room with a game running turns a latecomer away
even when it has seats going spare, because there is no way to hand somebody a
game already in progress.

The whole room travels as one `RoomState` per recipient — the seating, roles,
readiness, presence and the series score — so a client that missed a frame is correct again from
the next one rather than from a stream of deltas it has to replay. The
recipient's own row is in that list like everybody else's, marked `is_me`,
which is the only encoding of their role there is: a second one alongside would
be a second thing to keep in step with the server.

A player who is knocked out keeps their seat. They watch the rest of the game —
the chain, the clock, the chat — with only the word input gone, and the final
screen shows everybody's standings, ranked by who outlasted whom with each
score reported beside the place rather than deciding it.

A wide screen puts the game and the conversation side by side, so neither has
to be scrolled past to reach the other; a phone gets one column, the game first
with the chat below it, folded behind an unread count while a game is on.

Everybody in the room can talk, in the lobby and during a game. The
conversation reads as a log — one line per message, `name: text`, each seat
writing in its own colour — rather than as a stack of bubbles, which stops
being legible once four people are talking. The colour comes from the seat the
server says spoke, never from matching names. The conversation belongs to the
room rather than to a game, so it survives one starting and finishing, and it
dies with the room. A player is replayed what was said while
they held their seat: a refresh brings their conversation back, and somebody
who walks in with the code starts at silence rather than reading what the last
people in the room said. Text passes the same filter as a nickname before anyone sees
it — control and format characters dropped, whitespace collapsed, combining
marks capped — and a bot game has no chat, there being nobody to talk to.

When somebody leaves their seat their words stay in the conversation but their
name does not: the panel shows them as having left, in nobody's colour. A name left behind would be
one the next person to walk in could ask for, and the words above it would
become theirs.

Everybody sees the others' server-sanitized nicknames, never the raw input, and
two people asking for the same name are told apart before either is shown it. A
disconnect holds the seat for a grace window and shows the rest of the room a
countdown; a return inside it resumes the same position, rebuilt from the engine
rather than from a recorded stream, or the lobby when no game is running. Any
number of windows can be open at once, and each is settled on its own deadline.

The turn clock is deliberately not paused for a seat that has dropped. A player
who loses their connection on their own turn loses it the way anybody else
would; the window decides only whether they are still in the game afterwards.

### The frontend

`web/` is a SvelteKit single-page app in JavaScript, built by `adapter-static` and served by
the Go binary. It renders what the server sent and decides nothing: the store is a projection
of `ServerMessage`, so validity, turn order and the result all come from one authority. The
only client-owned state is the theme, the personal best per difficulty, and the input box.

The word field is deliberately uncontrolled. Vietnamese diacritics are composed over several
keystrokes by a Telex or VNI input method, and writing the value back on every keystroke
cancels that composition and mangles the accent. The one write the client does make is the
seed: when a turn arrives the field is filled with the syllable the word has to start with,
once per turn and never over text the player has already typed, so no composition can be in
progress when it happens.

The chain is listed newest first, and a finished game can be downloaded as a plain-text
transcript — the chain in playing order, who played what, and the final score.

Every Vietnamese string lives in `web/src/lib/i18n/vi.js`, including the map from
`RejectReason` to a message. That is why `ServerError.code` is a UI key such as
`room_not_found` and never prose. A test walks the generated enums and fails when a value has
no message, so a schema change cannot quietly ship an untranslated screen.

The countdown is drawn against the server's clock, estimated from the `Ping`/`Pong` round
trip, and settles 300ms early so the ring never claims more time than the server allows.

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

## Running the frontend in dev

```sh
make run       # the Go binary on :8080
make web-dev   # Vite on :5173, proxying /ws to :8080
```

The client resolves its socket from its own origin in both environments, so there is no
dev-only URL to get wrong.

## Make targets

| Target | Does |
|---|---|
| `fetch-dict` | Download the upstream `dictionary.db` (~179 MB) into `data/` |
| `dict` | Derive `data/noitu.db` from the upstream database |
| `server` | Build the Go server binary |
| `web` | Build the SvelteKit frontend to static assets |
| `web-dev` | Run the frontend dev server, proxying `/ws` to a local server |
| `proto` | Regenerate the Go and JS wire types from `proto/` (needs `buf`) |
| `proto-check` | Lint the schema and verify the committed generated code is in sync |
| `fixture-dict` | Build the small test dictionary, no download needed |
| `test` | Run Go and JavaScript tests |
| `test-e2e` | Run the Playwright suite against the fixture dictionary |
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

# web
cd web && npm ci && npm run build

# test-web (npm test builds first, then checks the bundle carries no wordlist)
cd web && npm run check && npm test

# fixture-dict
cd server && go run ./cmd/build-dictionary --words ../testdata/fixture-words.txt --out ../data/fixture.db --min-words 150

# test-e2e (needs the fixture dictionary above)
cd web && npm run test:e2e

# proto (only when proto/noitu/v1/game.proto changes)
cd web && npm ci
buf generate && buf lint
```

## Testing

| Suite | What it covers |
|---|---|
| `cd server && go test ./... -race` | The rules engine, the bot, the dictionary, and the whole transport layer |
| `cd web && npm test` | The store, the socket client, the Vietnamese copy, and the built bundle |
| `cd web && npm run test:e2e` | Real browsers against the real binary: a bot game, online games across two to four browser contexts, elimination, and reconnect |

The end-to-end suite plays against a small dictionary derived from
[`testdata/fixture-words.txt`](./testdata/fixture-words.txt) through the same
builder the real one uses, so CI never downloads the 179 MB upstream release.

## Deployment

One container image carries the binary, the built frontend and the derived
dictionary. See [`docs/deployment.md`](./docs/deployment.md) for configuration,
reverse-proxy requirements, and what a restart costs.

```sh
docker build -t noitu:latest .
docker run -p 8080:8080 noitu:latest
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
