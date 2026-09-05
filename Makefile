# noitu — build targets.
#
# Every target has a raw equivalent documented in README.md, so contributors
# without `make` (notably on Windows) are never blocked.

DICT_URL    := https://github.com/minhqnd/dictionary/releases/download/v2.0.0/dictionary.db
DICT_SRC    := data/dictionary.db
# Pinned to the v2.0.0 release asset. A mismatch means the upstream artifact
# changed under the same tag, or the download was truncated.
DICT_SHA256 := 9259403f0675b2991a1bd0ef6d0dbc5933afdb135632af095a60662f09bbf1d3
DICT_OUT   := data/noitu.db
SERVER_BIN := noitu-server

.PHONY: help fetch-dict verify-dict dict proto proto-check server web web-dev test test-go test-web run clean

help:
	@echo "fetch-dict  download + checksum the upstream dictionary (~179 MB) into data/"
	@echo "verify-dict re-check the downloaded dictionary against its pinned SHA-256"
	@echo "dict        derive $(DICT_OUT) from $(DICT_SRC)"
	@echo "proto       regenerate the Go and JS wire types from proto/"
	@echo "proto-check lint the schema and verify the committed output is in sync"
	@echo "server      build the Go server binary"
	@echo "web         build the SvelteKit frontend"
	@echo "web-dev     run the frontend dev server, proxying /ws to a local server"
	@echo "test        run all tests"
	@echo "run         build and run the server locally"
	@echo "clean       remove build artifacts (keeps downloaded dictionary)"

# One-time download. Resumable (-C -) so an interrupted 179 MB fetch can continue.
fetch-dict:
	@mkdir -p data
	curl -L -C - -o $(DICT_SRC) $(DICT_URL)
	@echo "$(DICT_SHA256)  $(DICT_SRC)" | sha256sum -c -
	@echo "downloaded and verified $(DICT_SRC)"

# Verify an already-downloaded copy without re-fetching.
verify-dict:
	@echo "$(DICT_SHA256)  $(DICT_SRC)" | sha256sum -c -

$(DICT_SRC):
	@echo "$(DICT_SRC) not found — run 'make fetch-dict' first" >&2
	@exit 1

dict: $(DICT_SRC)
	cd server && go run ./cmd/build-dictionary --in ../$(DICT_SRC) --out ../$(DICT_OUT)

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

run: server
	./$(SERVER_BIN)

clean:
	rm -f $(SERVER_BIN) $(SERVER_BIN).exe $(DICT_OUT)
	rm -rf web/build web/.svelte-kit
