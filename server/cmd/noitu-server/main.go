// Command noitu-server runs the nối từ game server: the WebSocket API, the
// rooms behind it, and the built frontend, in one binary.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/tiennm99dev/noitu/server/internal/dictionary"
	"github.com/tiennm99dev/noitu/server/internal/wsapi"
)

const (
	defaultAddr      = ":8080"
	defaultDBPath    = "data/noitu.db"
	defaultTurnLimit = 20 * time.Second
	defaultGrace     = 30 * time.Second

	// shutdownGrace bounds how long in-flight requests get once a signal
	// arrives. Rooms are told separately and immediately, so this only covers
	// the HTTP side.
	shutdownGrace = 10 * time.Second
)

type config struct {
	addr           string
	dbPath         string
	turnLimit      time.Duration
	grace          time.Duration
	allowedOrigins []string
	webDir         string
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
		"license", store.License(),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	api := wsapi.NewServer(ctx, store, wsapi.Config{
		TurnLimit:      cfg.turnLimit,
		GraceFor:       cfg.grace,
		AllowedOrigins: cfg.allowedOrigins,
		WebDir:         cfg.webDir,
	})

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           api,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.addr, "turn_limit", cfg.turnLimit, "web_dir", cfg.webDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	// Tell players why before the sockets go, rather than dropping them and
	// leaving the UI to guess.
	slog.Info("shutting down")
	api.Shutdown()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func loadConfig() config {
	return config{
		addr:           env("NOITU_ADDR", defaultAddr),
		dbPath:         env("NOITU_DB_PATH", defaultDBPath),
		turnLimit:      envDuration("NOITU_TURN_LIMIT", defaultTurnLimit),
		grace:          envDuration("NOITU_GRACE", defaultGrace),
		allowedOrigins: envList("NOITU_ALLOWED_ORIGINS"),
		webDir:         env("NOITU_WEB_DIR", ""),
	}
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// envDuration falls back loudly. A typo in a timing variable would otherwise
// silently change the rules of the game.
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
