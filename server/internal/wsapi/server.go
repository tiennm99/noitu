package wsapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	// IdleFor is how long a room sits in its lobby with no game started before
	// it closes. Zero falls back to a built-in default.
	IdleFor time.Duration
	// AllowedOrigins is matched by coder/websocket against the Origin header.
	// Empty means same-origin only, which is the right default for a binary
	// that also serves the frontend.
	AllowedOrigins []string
	// WebDir is the built frontend. Empty, or missing on disk, serves the API
	// alone, which is how the server runs before the frontend has been built.
	WebDir string
	// TrustedProxies lists the addresses, or CIDR ranges, of reverse proxies
	// whose X-Forwarded-For header is believed. Empty means the header is
	// ignored and every limiter keys on the socket's own peer address.
	TrustedProxies []string
	// MaxRooms caps live rooms across the process; zero means a built-in
	// default. MaxConnections caps open sockets the same way.
	MaxRooms       int
	MaxConnections int
	// MaxConnectionsPerIP caps how many open sockets one address may hold at
	// once. Zero — the default — turns it off: an address is only ever one
	// player behind a trusted proxy that unmasks the real client (see
	// TrustedProxies and clientIP); everywhere else it can be a whole NAT
	// egress, and capping it would cap that egress at one player.
	MaxConnectionsPerIP int
	// Version is what GET /version answers and what the startup log line
	// carries. Empty falls back to defaultVersion, which is what a plain
	// `go run` or a test server — nothing built with -ldflags — reports.
	Version string
}

// defaultVersion is what an unstamped build reports: a local `go run` or a
// test server, neither of which passes -X main.version through -ldflags.
const defaultVersion = "dev"

// defaultMaxConnections bounds open WebSockets when nothing else is set. Each
// one is three goroutines and an outbox; the number is generous for one
// binary and small next to what the host can hold.
const defaultMaxConnections = 2000

// Server wires the hub to an HTTP mux.
type Server struct {
	hub     *hub
	mux     *http.ServeMux
	cancel  context.CancelFunc
	cfg     Config
	version string

	proxies  []netip.Prefix
	maxConns int64
	conns    atomic.Int64

	// maxConnsPerIP is 0 when the cap is off. connsByIP is only ever touched
	// under its own mutex, separate from the hub's: it is purely a transport
	// accounting concern, one HTTP handler wide, with nothing to do with
	// rooms or sessions.
	maxConnsPerIP int64
	connsByIPMu   sync.Mutex
	connsByIP     map[string]int
}

// NewServer builds the handler tree.
func NewServer(ctx context.Context, dict Dictionary, cfg Config) *Server {
	ctx, cancel := context.WithCancel(ctx)

	version := cfg.Version
	if version == "" {
		version = defaultVersion
	}

	s := &Server{
		hub:           newHub(ctx, dict, cfg.TurnLimit, cfg.GraceFor, cfg.IdleFor, cfg.MaxRooms),
		mux:           http.NewServeMux(),
		cancel:        cancel,
		cfg:           cfg,
		version:       version,
		proxies:       parsePrefixes(cfg.TrustedProxies),
		maxConns:      int64(cfg.MaxConnections),
		maxConnsPerIP: int64(cfg.MaxConnectionsPerIP),
		connsByIP:     map[string]int{},
	}
	if s.maxConns <= 0 {
		s.maxConns = defaultMaxConnections
	}

	s.mux.HandleFunc("GET /ws", s.handleWS)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	// /readyz is a readiness check, distinct from /healthz above: it fails
	// while draining even though the process is still alive and still
	// finishing the games it already has, which is exactly the state a load
	// balancer should stop sending new traffic to.
	s.mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if s.hub.isDraining() {
			http.Error(w, "draining", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	s.mux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(s.version))
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

// StartDraining stops the server from seating any new room and flips
// /readyz to unhealthy. Existing games are untouched — a caller decides
// separately, via LiveGameCount, how long to wait before calling Shutdown.
func (s *Server) StartDraining() { s.hub.startDraining() }

// RoomCount is how many rooms — lobbies and running games alike — are live
// right now, for the log lines a drain or shutdown writes.
func (s *Server) RoomCount() int { return s.hub.roomCount() }

// LiveGameCount is how many of those rooms have a game actually running.
// Draining waits for this, not for RoomCount, because an empty lobby has
// nothing a restart costs.
func (s *Server) LiveGameCount() int64 { return s.hub.liveGameCount() }

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	// Refused before the upgrade, so a client that is over the line is told
	// so in HTTP terms it can read, and never costs a socket.
	if s.conns.Add(1) > s.maxConns {
		s.conns.Add(-1)
		http.Error(w, "server full", http.StatusServiceUnavailable)
		return
	}
	defer s.conns.Add(-1)

	ip := s.clientIP(r)
	if s.maxConnsPerIP > 0 {
		if !s.reserveIP(ip) {
			http.Error(w, "too many connections from this address", http.StatusServiceUnavailable)
			return
		}
		defer s.releaseIP(ip)
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: s.cfg.AllowedOrigins,
	})
	if err != nil {
		// Accept has already written the rejection, including the origin
		// refusal, so there is nothing to add to the response here.
		slog.Debug("websocket accept rejected", "err", err, "origin", r.Header.Get("Origin"))
		return
	}

	metrics.connectionsOpen.Add(1)
	metrics.connectionsTotal.Add(1)
	defer metrics.connectionsOpen.Add(-1)

	sess := newSession(s.hub.ctx, conn, s.hub, ip)
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
// RemoteAddr is the default and the only source when no proxy is trusted:
// X-Forwarded-For is attacker-controlled unless the proxy is known to append
// to it, and trusting it unconditionally would let one client spend everyone
// else's budget by forging the header. When the peer is a configured proxy,
// the header is walked from the right and the first address that is not
// itself a trusted proxy is the client — the entries a client could have
// forged all sit to the left of the one the proxy appended.
func (s *Server) clientIP(r *http.Request) string {
	peer := remoteHost(r.RemoteAddr)
	if len(s.proxies) == 0 || !s.trusted(peer) {
		return peer
	}

	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		if hop == "" || s.trusted(hop) {
			continue
		}
		if _, err := netip.ParseAddr(hop); err != nil {
			// A malformed hop is a header somebody wrote by hand; fall back
			// to the proxy's address rather than key a limiter on garbage.
			return peer
		}
		return hop
	}
	return peer
}

// trusted reports whether host is one of the configured proxies.
func (s *Server) trusted(host string) bool {
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, p := range s.proxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// reserveIP claims one of an address's connection slots, refusing once
// MaxConnectionsPerIP of them are already open. Only called when the cap is
// on; off is the default for exactly the reason clientIP's own comment gives —
// without a trusted proxy unmasking the real client, one address can be an
// entire NAT egress, and this would cap it at a single player.
func (s *Server) reserveIP(ip string) bool {
	s.connsByIPMu.Lock()
	defer s.connsByIPMu.Unlock()
	if int64(s.connsByIP[ip]) >= s.maxConnsPerIP {
		return false
	}
	s.connsByIP[ip]++
	return true
}

// releaseIP frees the slot reserveIP claimed. The entry is dropped once it
// reaches zero rather than left behind at 0, so the map does not grow for
// every address that has ever connected and disconnected.
func (s *Server) releaseIP(ip string) {
	s.connsByIPMu.Lock()
	defer s.connsByIPMu.Unlock()
	s.connsByIP[ip]--
	if s.connsByIP[ip] <= 0 {
		delete(s.connsByIP, ip)
	}
}

// remoteHost strips the port from a RemoteAddr.
func remoteHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// parsePrefixes reads proxy addresses as CIDR ranges, accepting a bare
// address as a range of one. An entry that parses as neither is logged and
// skipped rather than silently trusting nothing or everything.
func parsePrefixes(raw []string) []netip.Prefix {
	var out []netip.Prefix
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if p, err := netip.ParsePrefix(entry); err == nil {
			out = append(out, p.Masked())
			continue
		}
		if a, err := netip.ParseAddr(entry); err == nil {
			a = a.Unmap()
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		slog.Warn("ignoring unparseable trusted proxy", "entry", entry)
	}
	return out
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
