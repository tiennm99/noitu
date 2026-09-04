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

.PHONY: help fetch-dict verify-dict dict server web test test-go test-web run clean

help:
	@echo "fetch-dict  download + checksum the upstream dictionary (~179 MB) into data/"
	@echo "verify-dict re-check the downloaded dictionary against its pinned SHA-256"
	@echo "dict        derive $(DICT_OUT) from $(DICT_SRC)"
	@echo "server      build the Go server binary"
	@echo "web         build the SvelteKit frontend"
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

server:
	cd server && CGO_ENABLED=0 go build -o ../$(SERVER_BIN) ./cmd/noitu-server

web:
	cd web && npm ci && npm run build

test: test-go test-web

test-go:
	cd server && go vet ./... && go test ./... -race

test-web:
	@if [ -d web/node_modules ]; then cd web && npm test; else echo "web/ not set up yet, skipping"; fi

run: server
	./$(SERVER_BIN)

clean:
	rm -f $(SERVER_BIN) $(SERVER_BIN).exe $(DICT_OUT)
	rm -rf web/build web/.svelte-kit
