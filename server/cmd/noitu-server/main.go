// Command noitu-server runs the nối từ game server: the WebSocket API, the
// rooms behind it, and the built frontend, in one binary.
package main

import (
	"context"
	"errors"
	"expvar"
	"flag"
	"fmt"
	"log/slog"
	"net"
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

	// shutdownFlush is how long the process lingers after telling every
	// session the server is restarting. Each session writes that notice from
	// its own goroutine, and http.Server.Shutdown does not wait for hijacked
	// WebSocket connections, so returning at once could exit before the
	// notice reached a socket. wsapi exposes no live-session count to wait on,
	// so this is a short fixed bound.
	shutdownFlush = 2 * time.Second

	// idleTimeout closes idle keep-alive HTTP connections, which would
	// otherwise be held forever outside every connection cap. It is
	// deliberately the only connection deadline: ReadTimeout and WriteTimeout
	// stay on a hijacked connection and would drop every WebSocket game after
	// that interval.
	idleTimeout = 120 * time.Second

	// healthcheckTimeout bounds the -healthcheck request.
	healthcheckTimeout = 3 * time.Second
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
	flushWait           time.Duration
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	healthcheck := flag.Bool("healthcheck", false,
		"probe GET /healthz on the configured address (NOITU_ADDR) and exit 0 if it answers 200, 1 otherwise")
	flag.Parse()
	if *healthcheck {
		if err := checkHealth(env("NOITU_ADDR", defaultAddr), healthcheckTimeout); err != nil {
			fmt.Fprintln(os.Stderr, "healthcheck failed:", err)
			os.Exit(1)
		}
		return
	}

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

	ln, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return serve(ctx, stop, cfg, store, ln)
}

// serve runs the server on ln until ctx is cancelled — by a signal in
// production — or the listener fails, then drains and shuts down.
//
// ctx is only the "stop now" trigger. The wsapi server is built on its own
// background context, because a room derives its lifetime from the context it
// is given: handing it the signal context would cancel every room and session
// at the moment of the signal, before the drain could keep a game alive or
// tell a player the server is restarting. api.Shutdown cancels that context
// itself once the drain is over.
//
// release is called as soon as ctx fires. In production it is the signal
// context's stop, which restores the default signal behaviour so a second
// SIGTERM or SIGINT during a long drain terminates the process instead of
// being swallowed. It may be nil.
func serve(ctx context.Context, release func(), cfg config, dict wsapi.Dictionary, ln net.Listener) error {
	api := wsapi.NewServer(context.Background(), dict, wsapi.Config{
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

	srv := newHTTPServer(cfg.addr, api)
	debugSrv := newDebugServer(cfg.debugAddr)

	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", ln.Addr().String(), "turn_limit", cfg.turnLimit, "web_dir", cfg.webDir, "version", version)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
		api.Shutdown()
		_ = shutdownServer(debugSrv)
		return err
	case <-ctx.Done():
	}
	if release != nil {
		release()
	}

	drainAndShutdown(api, cfg.drainTimeout, cfg.flushWait)

	_ = shutdownServer(debugSrv)
	return shutdownServer(srv)
}

// lifecycle is the slice of *wsapi.Server the shutdown sequence drives,
// narrowed so the ordering can be tested against a recorder.
type lifecycle interface {
	gameCounter
	RoomCount() int
	StartDraining()
	Shutdown()
}

// drainAndShutdown runs the shutdown sequence in its required order.
//
// Draining comes first: stop seating new rooms and flip /readyz unhealthy, so
// a load balancer stops sending this instance new traffic while the games it
// already has finish on their own turn clock. A creator refused during this
// window gets server_restarting rather than server_full — the room is not
// coming back, unlike a full one. Only then are players told why the sockets
// are going, and flush gives those notices time to reach them.
func drainAndShutdown(api lifecycle, drainTimeout, flush time.Duration) {
	slog.Info("draining", "rooms", api.RoomCount(), "live_games", api.LiveGameCount())
	api.StartDraining()
	waitForGamesToFinish(api, drainTimeout)

	slog.Info("shutting down", "rooms", api.RoomCount(), "live_games", api.LiveGameCount())
	api.Shutdown()
	if flush > 0 {
		time.Sleep(flush)
	}
}

// newHTTPServer builds a listener — the public one and the debug one alike. Only ReadHeaderTimeout and
// IdleTimeout are set: see idleTimeout for why the other deadlines are not.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       idleTimeout,
	}
}

// checkHealth GETs /healthz on the address the server listens on and reports
// anything but a 200 as an error. It exists because the container image is
// distroless: no curl or wget for a container health check to shell out to.
func checkHealth(addr string, timeout time.Duration) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("address %q: %w", addr, err)
	}
	// A wildcard listen address is not something to dial; loopback reaches it.
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/healthz answered %s", resp.Status)
	}
	return nil
}

// shutdownServer stops srv gracefully, giving in-flight requests up to
// shutdownGrace. A nil srv — the debug listener when it is not configured —
// is a no-op.
func shutdownServer(srv *http.Server) error {
	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	return srv.Shutdown(ctx)
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
	return newHTTPServer(addr, mux)
}

// gameCounter is the drain loop's only dependency on *wsapi.Server, narrowed
// so the polling logic can be tested without a live server behind it.
type gameCounter interface {
	LiveGameCount() int64
}

// waitForGamesToFinish blocks until every room's game has ended or timeout
// passes, whichever is first. timeout <= 0 returns immediately, which is
// today's behaviour: rooms are told the server is restarting and torn down
// with whatever they were doing.
//
// Only games count, not lobbies: a room nobody has started a game in has
// nothing a restart costs, and waiting for it would make every deploy sit out
// somebody's abandoned tab for the full timeout.
func waitForGamesToFinish(api gameCounter, timeout time.Duration) {
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
		flushWait:           shutdownFlush,
	}
}

// envInt falls back loudly, like envDuration. Zero is a real value — for the
// limits it configures it means "use the built-in default" — so only a
// negative or unparseable one is rejected.
func envInt(key string, fallback int) int {
	return envParsed(key, fallback, "ignoring invalid integer", strconv.Atoi,
		func(n int) bool { return n >= 0 })
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
	return envParsed(key, fallback, "ignoring invalid duration", time.ParseDuration,
		func(d time.Duration) bool { return d > 0 })
}

// envNonNegDuration is envDuration with zero accepted as a real value rather
// than a trigger for the fallback: NOITU_DRAIN_TIMEOUT=0 means "do not wait",
// which is a deliberate choice an operator can make explicitly, not a typo.
func envNonNegDuration(key string, fallback time.Duration) time.Duration {
	return envParsed(key, fallback, "ignoring invalid duration", time.ParseDuration,
		func(d time.Duration) bool { return d >= 0 })
}

// envParsed reads key through parse, returning fallback when it is unset or
// blank, and — with a warning, so a misconfiguration is visible rather than
// silently ignored — when it fails to parse or is not valid.
func envParsed[T any](key string, fallback T, invalidMsg string, parse func(string) (T, error), valid func(T) bool) T {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := parse(raw)
	if err != nil || !valid(v) {
		slog.Warn(invalidMsg, "key", key, "value", raw, "using", fallback)
		return fallback
	}
	return v
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
