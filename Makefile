# noitu — build targets.
#
# Every target has a raw equivalent documented in README.md, so contributors
# without `make` (notably on Windows) are never blocked.

DICT_URL    := https://raw.githubusercontent.com/undertheseanlp/dictionary/2c078cfc373b06e2980d324ce1d7bd13740c3319/dictionary/words.txt
DICT_SRC    := data/undertheseanlp-words.jsonl
# Pinned to a commit, not a branch, so the URL is immutable and cannot drift
# from the checksum. A mismatch means the download was truncated or tampered.
DICT_SHA256 := 4c3e0e6117e4bdfa97731e135c3d4a05881889909267394a8de8d88ef79f13f0
DICT_OUT   := data/noitu.db
FIXTURE_WORDS := testdata/fixture-words.txt
FIXTURE_DB    := data/fixture.db
SERVER_BIN := noitu-server

.PHONY: help fetch-dict verify-dict dict fixture-dict proto proto-check server web web-dev test test-go test-web test-e2e run clean

help:
	@echo "fetch-dict  download + checksum the upstream wordlist (~4.8 MB) into data/"
	@echo "verify-dict re-check the downloaded dictionary against its pinned SHA-256"
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

# One-time download. -f so an HTTP error page fails here, not as a confusing
# checksum mismatch.
fetch-dict:
	@mkdir -p data
	curl -fL -o $(DICT_SRC) $(DICT_URL)
	@echo "$(DICT_SHA256)  $(DICT_SRC)" | sha256sum -c -
	@echo "downloaded and verified $(DICT_SRC)"

# Verify an already-downloaded copy without re-fetching.
verify-dict:
	@echo "$(DICT_SHA256)  $(DICT_SRC)" | sha256sum -c -

$(DICT_SRC):
	@echo "$(DICT_SRC) not found — run 'make fetch-dict' first" >&2
	@exit 1

dict: $(DICT_SRC)
	cd server && go run ./cmd/build-dictionary --merged ../$(DICT_SRC) --out ../$(DICT_OUT)

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

# What CI runs: the schema is well-formed, and the committed generated trees
# match what the schema currently produces.
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
