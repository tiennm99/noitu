# noitu — build targets.
#
# Every target has a raw equivalent documented in README.md, so contributors
# without `make` (notably on Windows) are never blocked.

# Wikimedia regenerates the Wiktionary tiếng Việt dump monthly and repoints
# `latest/` at it; this tracks `latest/`, fetched fresh and unpinned by design,
# and the builder records the SHA-256 of what it read in the database's meta
# table. Dated directories exist should a build ever need reproducing.
DICT_URL    := https://dumps.wikimedia.org/viwiktionary/latest/viwiktionary-latest-pages-articles.xml.bz2
DICT_SRC    := data/viwiktionary-latest-pages-articles.xml.bz2
DICT_OUT   := data/noitu.db
FIXTURE_WORDS := testdata/fixture-words.txt
FIXTURE_DB    := data/fixture.db
SERVER_BIN := noitu-server

.PHONY: help fetch-dict dict fixture-dict proto proto-check server web web-dev test test-go test-web test-e2e run clean

help:
	@echo "fetch-dict  download the current Wiktionary tiếng Việt dump (~61 MB) into data/"
	@echo "dict        derive $(DICT_OUT) from $(DICT_SRC)"
	@echo "fixture-dict build the small test dictionary — no download needed"
	@echo "proto       regenerate the Go and JS wire types from proto/"
	@echo "proto-check lint the schema and verify the committed output is in sync"
	@echo "server      build the Go server binary"
	@echo "web         build the SvelteKit frontend"
	@echo "web-dev     run the frontend dev server, proxying /ws to a local server"
	@echo "test        run all tests"
	@echo "test-e2e    run the Playwright suite against the fixture dictionary"
	@echo "run         build and run the server locally"
	@echo "clean       remove build artifacts (keeps downloaded dictionary)"

# Fetches whatever `latest/` currently points at. -f so an HTTP error fails
# here rather than as a bzip2 error later; -R keeps the server's modification
# time, which is when the dump was generated and becomes source_fetched_at; no
# resume flag, because `latest` can be repointed between two attempts and a
# resumed file would splice two months together; downloaded to a .part name
# and renamed only on success, so an interrupted fetch never leaves a truncated
# file for the next `make dict` to consume.
fetch-dict:
	@mkdir -p data
	curl -fLR -o $(DICT_SRC).part $(DICT_URL) && mv $(DICT_SRC).part $(DICT_SRC)
	@echo "downloaded $(DICT_SRC)"

$(DICT_SRC):
	@echo "$(DICT_SRC) not found — run 'make fetch-dict' first" >&2
	@exit 1

dict: $(DICT_SRC)
	cd server && go run ./cmd/build-dictionary --dump ../$(DICT_SRC) --out ../$(DICT_OUT)

# The dictionary tests, end-to-end runs and CI all play against. Built from a
# checked-in word list through the same pipeline as the real one, so nothing
# has to download anything to get a working database.
$(FIXTURE_DB): $(FIXTURE_WORDS)
	cd server && go run ./cmd/build-dictionary --words ../$(FIXTURE_WORDS) --out ../$(FIXTURE_DB) --min-words 150

fixture-dict: $(FIXTURE_DB)

# Regenerates both targets from proto/noitu/v1/game.proto. Needs `buf`; the
# two code generators come from server/go.mod's tool directive and
# web/package.json, so there is nothing to install globally. Generated code is
# committed, so building and running the project never requires this target.
proto: web/node_modules
	buf generate

# The local equivalent of proto.yml: the schema is well-formed, and the
# committed generated trees match what the schema currently produces.
# --intent-to-add makes a newly emitted file visible: git diff alone ignores
# untracked files and would call an incomplete committed tree clean.
proto-check: proto
	buf lint
	git add --intent-to-add -- server/gen web/src/lib/proto
	git diff --exit-code -- server/gen web/src/lib/proto

web/node_modules: web/package.json web/package-lock.json
	cd web && npm ci
	@touch web/node_modules

server:
	cd server && CGO_ENABLED=0 go build -o ../$(SERVER_BIN) ./cmd/noitu-server

web: web/node_modules
	cd web && npm run build

# Vite serves the UI and proxies /ws to the Go binary on :8080, so the client
# resolves its socket from its own origin in dev exactly as it does in
# production. Run `make run` alongside this.
web-dev: web/node_modules
	cd web && npm run dev

test: test-go test-web

test-go:
	cd server && go vet ./... && go test ./... -race

# npm test builds before it runs: the bundle check reads the built output, and
# a stale build would let it pass over code that no longer exists.
test-web: web/node_modules
	cd web && npm run check && npm test

test-e2e: web/node_modules $(FIXTURE_DB) server
	cd web && npm run test:e2e

run: server
	./$(SERVER_BIN)

clean:
	rm -f $(SERVER_BIN) $(SERVER_BIN).exe $(DICT_OUT) $(FIXTURE_DB)
	rm -rf web/build web/.svelte-kit
