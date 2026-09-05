package wsapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Config is everything the transport layer needs to run.
type Config struct {
	// TurnLimit is the same for bot and PvP games: one constant, one code
	// path, no mode-specific timing to reason about.
	TurnLimit time.Duration
	// GraceFor is how long a disconnected seat is held open.
	GraceFor time.Duration
	// AllowedOrigins is matched by coder/websocket against the Origin header.
	// Empty means same-origin only, which is the right default for a binary
	// that also serves the frontend.
	AllowedOrigins []string
	// WebDir is the built frontend. Empty, or missing on disk, serves the API
	// alone — which is the state until phase 6 produces a bundle.
	WebDir string
}

// Server wires the hub to an HTTP mux.
type Server struct {
	hub    *hub
	mux    *http.ServeMux
	cancel context.CancelFunc
	cfg    Config
}

// NewServer builds the handler tree.
func NewServer(ctx context.Context, dict Dictionary, cfg Config) *Server {
	ctx, cancel := context.WithCancel(ctx)

	s := &Server{
		hub:    newHub(ctx, dict, cfg.TurnLimit, cfg.GraceFor),
		mux:    http.NewServeMux(),
		cancel: cancel,
		cfg:    cfg,
	}

	s.mux.HandleFunc("GET /ws", s.handleWS)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	s.mountStatic()

	go s.sweepLimiters(ctx)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// Shutdown tells live games why they are ending, then stops the hub.
func (s *Server) Shutdown() {
	s.hub.shutdown()
	s.cancel()
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: s.cfg.AllowedOrigins,
	})
	if err != nil {
		// Accept has already written the rejection, including the origin
		// refusal, so there is nothing to add to the response here.
		slog.Debug("websocket accept rejected", "err", err, "origin", r.Header.Get("Origin"))
		return
	}

	sess := newSession(s.hub.ctx, conn, s.hub, clientIP(r))
	sess.run()

	// The token has to outlive the socket by exactly the grace window: that is
	// what a reconnect presents to reclaim its seat. Dropping it here, as the
	// connection ends, would make every resume fail to find its game.
	s.hub.expireToken(sess.resumeToken, s.cfg.GraceFor)
}

// immutablePrefix is where SvelteKit's adapter puts content-hashed assets.
const immutablePrefix = "/_app/immutable/"

// mountStatic serves the built frontend so one binary is the whole deployment.
//
// Unknown paths fall back to index.html because the frontend is a single-page
// app: a deep link is a client route, not a server 404.
func (s *Server) mountStatic() {
	if s.cfg.WebDir == "" {
		return
	}
	index := filepath.Join(s.cfg.WebDir, "index.html")
	if _, err := os.Stat(index); err != nil {
		slog.Warn("no frontend to serve", "dir", s.cfg.WebDir)
		return
	}

	root := filepath.Clean(s.cfg.WebDir)
	files := http.FileServer(http.Dir(root))

	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		clean := filepath.Join(root, filepath.Clean(r.URL.Path))

		// A path boundary, not a string prefix: with a root of /srv/web, a
		// prefix test would also accept /srv/webhooks. http.Dir re-anchors
		// anyway, but the SPA fallback below stats paths directly, so this is
		// the check that keeps it from being used to probe outside the bundle.
		if !underRoot(root, clean) {
			http.NotFound(w, r)
			return
		}
		if info, err := os.Stat(clean); err == nil && !info.IsDir() {
			// Everything under immutablePrefix carries a content hash in its
			// name, so a changed file is a changed URL and the old one can be
			// cached forever.
			if strings.HasPrefix(r.URL.Path, immutablePrefix) {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		// The shell names those hashed assets, so a cached copy outlives the
		// deploy that renamed them and the app loads into a blank page.
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
	})
}

// underRoot reports whether path is root itself or lies beneath it.
func underRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

// clientIP is the key the join limiter counts against.
//
// RemoteAddr is deliberately the only source. Behind the reverse proxy this
// deploys under, X-Forwarded-For is attacker-controlled unless the proxy is
// known to overwrite it, and trusting it unconditionally would let one client
// spend everyone else's budget by forging the header.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// sweepLimiters keeps the per-key rate limiter from growing without bound.
func (s *Server) sweepLimiters(ctx context.Context) {
	ticker := time.NewTicker(limiterIdleFor)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.hub.joinLimiter.sweep(now)
		}
	}
}
