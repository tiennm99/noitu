# One image: the binary, the built frontend, and the derived dictionary.
#
# The 179 MB upstream release is downloaded in a builder stage and never
# reaches the final image — only the ~3 MB database derived from it does. That
# derived database is CC BY-SA 4.0 while the code is Apache-2.0, so it is
# copied in as its own layer alongside its licence and attribution rather than
# being embedded in the binary.

# --- the frontend -----------------------------------------------------------
FROM node:24-alpine AS web

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# --- the binary -------------------------------------------------------------
FROM golang:1.25-alpine AS build

WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
# CGO_ENABLED=0 is what makes a distroless static image possible, and it works
# because the SQLite driver is pure Go.
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/noitu-server ./cmd/noitu-server
RUN CGO_ENABLED=0 go build -trimpath -o /out/build-dictionary ./cmd/build-dictionary

# --- the dictionary ---------------------------------------------------------
FROM alpine:3.22 AS dict

# Pinned by tag and checked by digest: the derived wordlist is the one thing in
# this image that cannot be rebuilt from the repository alone. The Makefile
# pins the same release for local builds, and a test asserts the two agree.
ARG DICT_URL=https://github.com/minhqnd/dictionary/releases/download/v2.0.0/dictionary.db
ARG DICT_SHA256=9259403f0675b2991a1bd0ef6d0dbc5933afdb135632af095a60662f09bbf1d3

# Set to 1 to build from the checked-in word sample instead of downloading the
# upstream release. That produces a playable but tiny dictionary, and exists so
# the image itself can be smoke-tested without a 179 MB download.
ARG FIXTURE_DICT=0

RUN apk add --no-cache curl
WORKDIR /work
COPY --from=build /out/build-dictionary /usr/local/bin/build-dictionary
COPY testdata/fixture-words.txt ./fixture-words.txt

RUN set -eu; \
    mkdir -p /out; \
    if [ "$FIXTURE_DICT" = "1" ]; then \
        build-dictionary --words ./fixture-words.txt --out /out/noitu.db --min-words 150; \
    else \
        curl -fsSL -o dictionary.db "$DICT_URL"; \
        echo "$DICT_SHA256  dictionary.db" | sha256sum -c -; \
        build-dictionary --in ./dictionary.db --out /out/noitu.db; \
    fi

# --- the image --------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=build /out/noitu-server /app/noitu-server
COPY --from=web /src/web/build /app/web

# The share-alike half of the image. data/LICENSE and data/ATTRIBUTION.md ship
# with the derived wordlist because CC BY-SA 4.0 applies to it wherever it is
# distributed, and a container image is distribution.
COPY --from=dict /out/noitu.db /app/data/noitu.db
COPY data/LICENSE /app/data/LICENSE
COPY data/ATTRIBUTION.md /app/data/ATTRIBUTION.md
COPY NOTICE /app/NOTICE

ENV NOITU_ADDR=:8080 \
    NOITU_DB_PATH=/app/data/noitu.db \
    NOITU_WEB_DIR=/app/web

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/noitu-server"]
