# One image: the binary, the built frontend, and the derived dictionary.
#
# The dictionary is built from data/dictionary.txt, the corpus committed to
# the repository, so a build downloads nothing from Wikimedia. That derived
# data is CC BY-SA 4.0 while the code is Apache-2.0, so the database is copied
# in as its own layer alongside its licence and attribution rather than being
# embedded in the binary.

# --- the frontend -----------------------------------------------------------
FROM node:24-alpine AS web

WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# --- the binary -------------------------------------------------------------
FROM golang:1-alpine AS build

# What GET /version answers and the startup log line carries. .dockerignore
# deliberately keeps .git out of the build context — a stale copy should
# never ship — so git describe cannot run in here; a caller that wants a real
# version passes it in, the way `make image` does. Unset, this defaults to
# "dev", which is honest about an unstamped build.
ARG VERSION=dev

WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
# CGO_ENABLED=0 is what makes a distroless static image possible, and it works
# because the SQLite driver is pure Go.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/noitu-server ./cmd/noitu-server
RUN CGO_ENABLED=0 go build -trimpath -o /out/build-dictionary ./cmd/build-dictionary

# --- the dictionary ---------------------------------------------------------
FROM alpine:3 AS dict

WORKDIR /work
COPY --from=build /out/build-dictionary /usr/local/bin/build-dictionary
COPY data/dictionary.txt ./dictionary.txt
RUN mkdir -p /out && build-dictionary --corpus ./dictionary.txt --out /out/noitu.db

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
COPY LICENSE /app/LICENSE

ENV NOITU_ADDR=:8080 \
    NOITU_DB_PATH=/app/data/noitu.db \
    NOITU_WEB_DIR=/app/web

EXPOSE 8080
USER nonroot:nonroot

# The image has no curl or wget, so the health check is the binary itself:
# -healthcheck GETs /healthz on NOITU_ADDR and exits 0 or 1. It is liveness
# only; /readyz is the drain signal a load balancer polls.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app/noitu-server", "-healthcheck"]
ENTRYPOINT ["/app/noitu-server"]
