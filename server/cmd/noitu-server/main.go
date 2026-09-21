// Command noitu-server runs the nối từ game server: the WebSocket API, the
// rooms behind it, and the built frontend, in one binary.
package main

import (
	"context"
	"errors"
	"expvar"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tiennm99dev/noitu/server/internal/dictionary"
	"github.com/tiennm99dev/noitu/server/internal/wsapi"
)

const (
	defaultAddr      = ":8080"
	defaultDBPath    = "data/noitu.db"
	defaultTurnLimit = 30 * time.Second
	defaultGrace     = 30 * time.Second

	// shutdownGrace bounds how long in-flight requests get once a signal
	// arrives. Rooms are told separately and immediately, so this only covers
	// the HTTP side.
	shutdownGrace = 10 * time.Second

	// drainPollInterval is how often waitForGamesToFinish checks whether the
	// last live game has ended. Short enough that a drain does not overrun its
	// timeout by more than a blink, cheap enough to poll at all — the
	// alternative is a channel the hub would need to fan out to every room.
	drainPollInterval = 200 * time.Millisecond
)

// version is the build the process is running. The default here is what
// `go run` or a build with no -ldflags reports; a real build sets it with
// -X main.version=$(git describe --tags --always --dirty), from the Makefile
// server target or the Dockerfile's VERSION build-arg.
var version = "dev"

type config struct {
	addr                string
	dbPath              string
	turnLimit           time.Duration
	grace               time.Duration
	allowedOrigins      []string
	webDir              string
	trustedProxies      []string
	maxRooms            int
	maxConnections      int
	maxConnectionsPerIP int
	debugAddr           string
	drainTimeout        time.Duration
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if err := run(); err != nil {
		slog.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := loadConfig()

	store, err := dictionary.Open(cfg.dbPath)
	if err != nil {
		return err
	}

	// The store deliberately does not log — a library writing to the global
	// logger fights the server's own handler — so the CC BY-SA 4.0 attribution
	// that ships with the data surfaces here or nowhere.
	slog.Info("dictionary loaded",
		"path", cfg.dbPath,
		"words", store.WordCount(),
		"aliases", store.AliasCount(),
		"meanings", store.MeaningCount(),
		"license", store.License(),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	api := wsapi.NewServer(ctx, store, wsapi.Config{
		TurnLimit:           cfg.turnLimit,
		GraceFor:            cfg.grace,
		AllowedOrigins:      cfg.allowedOrigins,
		WebDir:              cfg.webDir,
		TrustedProxies:      cfg.trustedProxies,
		MaxRooms:            cfg.maxRooms,
		MaxConnections:      cfg.maxConnections,
		MaxConnectionsPerIP: cfg.maxConnectionsPerIP,
		Version:             version,
	})

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           api,
		ReadHeaderTimeout: 10 * time.Second,
	}

	debugSrv := newDebugServer(cfg.debugAddr)

	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.addr, "turn_limit", cfg.turnLimit, "web_dir", cfg.webDir, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	if debugSrv != nil {
		go func() {
			slog.Info("debug endpoint listening", "addr", cfg.debugAddr)
			if err := debugSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("debug endpoint exited", "err", err)
			}
		}()
	}

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	// Draining is the first half of shutdown: stop seating new rooms and flip
	// /readyz unhealthy, so a load balancer stops sending this instance new
	// traffic while the games it already has finish on their own turn clock.
	// A creator refused during this window gets server_restarting rather than
	// server_full — the room is not coming back, unlike a full one.
	slog.Info("draining", "rooms", api.RoomCount(), "live_games", api.LiveGameCount())
	api.StartDraining()
	waitForGamesToFinish(api, cfg.drainTimeout)

	// Tell players why before the sockets go, rather than dropping them and
	// leaving the UI to guess.
	slog.Info("shutting down", "rooms", api.RoomCount(), "live_games", api.LiveGameCount())
	api.Shutdown()

	if debugSrv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		_ = debugSrv.Shutdown(shutdownCtx)
		cancel()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// newDebugServer builds the expvar listener, or nil when NOITU_DEBUG_ADDR is
// unset. It is a separate *http.Server on its own address rather than a route
// on the public mux: /debug/vars is an operator surface, and the two must not
// be reachable from the same port a player's browser talks to.
func newDebugServer(addr string) *http.Server {
	if addr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("/debug/vars", expvar.Handler())
	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
}

// waitForGamesToFinish blocks until every room's game has ended or timeout
// passes, whichever is first. timeout <= 0 returns immediately, which is
// today's behaviour: rooms are told the server is restarting and torn down
// with whatever they were doing.
//
// Only games count, not lobbies: a room nobody has started a game in has
// nothing a restart costs, and waiting for it would make every deploy sit out
// somebody's abandoned tab for the full timeout.
func waitForGamesToFinish(api *wsapi.Server, timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(drainPollInterval)
	defer ticker.Stop()

	for {
		if api.LiveGameCount() == 0 {
			return
		}
		select {
		case <-deadline.C:
			slog.Info("drain timed out with games still running", "live_games", api.LiveGameCount())
			return
		case <-ticker.C:
		}
	}
}

func loadConfig() config {
	return config{
		addr:                env("NOITU_ADDR", defaultAddr),
		dbPath:              env("NOITU_DB_PATH", defaultDBPath),
		turnLimit:           envDuration("NOITU_TURN_LIMIT", defaultTurnLimit),
		grace:               envDuration("NOITU_GRACE", defaultGrace),
		allowedOrigins:      envList("NOITU_ALLOWED_ORIGINS"),
		webDir:              env("NOITU_WEB_DIR", ""),
		trustedProxies:      envList("NOITU_TRUSTED_PROXIES"),
		maxRooms:            envInt("NOITU_MAX_ROOMS", 0),
		maxConnections:      envInt("NOITU_MAX_CONNECTIONS", 0),
		maxConnectionsPerIP: envInt("NOITU_MAX_CONNECTIONS_PER_IP", 0),
		debugAddr:           env("NOITU_DEBUG_ADDR", ""),
		drainTimeout:        envNonNegDuration("NOITU_DRAIN_TIMEOUT", 0),
	}
}

// envInt falls back loudly, like envDuration. Zero means "use the built-in
// default", so it is what an unset or invalid value becomes.
func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		slog.Warn("ignoring invalid integer", "key", key, "value", raw, "using", fallback)
		return fallback
	}
	return n
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// envDuration falls back loudly. A typo in a timing variable would otherwise
// silently change the rules of the game. Zero is rejected along with anything
// unparseable, which is right for every duration except the drain timeout —
// see envNonNegDuration.
func envDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		slog.Warn("ignoring invalid duration", "key", key, "value", raw, "using", fallback)
		return fallback
	}
	return d
}

// envNonNegDuration is envDuration with zero accepted as a real value rather
// than a trigger for the fallback: NOITU_DRAIN_TIMEOUT=0 means "do not wait",
// which is a deliberate choice an operator can make explicitly, not a typo.
func envNonNegDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		slog.Warn("ignoring invalid duration", "key", key, "value", raw, "using", fallback)
		return fallback
	}
	return d
}

// envList returns nil for an unset variable, which coder/websocket reads as
// same-origin only.
func envList(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
