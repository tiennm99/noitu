package main

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/dictionary"
	"github.com/tiennm99dev/noitu/server/internal/wsapi"
	_ "modernc.org/sqlite"
)

// chainStore opens a three-word dictionary through the real store, so serve is
// exercised against the same Dictionary implementation production uses.
func chainStore(t *testing.T) *dictionary.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "noitu.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	_, err = db.Exec(`
CREATE TABLE words (word TEXT PRIMARY KEY, first TEXT NOT NULL, last TEXT NOT NULL, syllables INTEGER NOT NULL) WITHOUT ROWID;
CREATE INDEX idx_words_first ON words(first);
CREATE TABLE syllables (syllable TEXT PRIMARY KEY, out_degree INTEGER NOT NULL) WITHOUT ROWID;
CREATE TABLE aliases (variant TEXT PRIMARY KEY, canonical TEXT NOT NULL) WITHOUT ROWID;
CREATE TABLE meanings (word TEXT NOT NULL, ord INTEGER NOT NULL, pos TEXT NOT NULL, gloss TEXT NOT NULL, PRIMARY KEY (word, ord)) WITHOUT ROWID;
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO meta VALUES ('builder_version','` + dictionary.RequiredBuilderVersion + `'),('source_license','CC BY-SA 4.0'),('word_count','3'),('meaning_count','0');
INSERT INTO words VALUES ('a b','a','b',2),('b c','b','c',2),('c d','c','d',2);
INSERT INTO syllables VALUES ('a',1),('b',1),('c',1),('d',0);`)
	if err != nil {
		t.Fatal(err)
	}
	store, err := dictionary.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store
}

// openingAtA lets the test choose the opening word: the store's own picker
// wants a syllable with many continuations, which a three-word chain lacks.
type openingAtA struct{ *dictionary.Store }

func (openingAtA) RandomOpeningWord(int) (string, error) { return "a b", nil }

// The deploy path: a signal must start the drain while the game is still
// alive, keep waiting for it up to the drain timeout, tell the player the
// server is restarting, and only then let the process exit.
func TestServeDrainsLiveGamesBeforeShuttingDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	const drain = 1500 * time.Millisecond
	cfg := config{
		addr:         addr,
		turnLimit:    30 * time.Second,
		grace:        30 * time.Second,
		drainTimeout: drain,
		flushWait:    100 * time.Millisecond,
	}

	// The context stands in for the signal context.
	sig, signal := context.WithCancel(context.Background())
	defer signal()
	released := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- serve(sig, func() { close(released) }, cfg, openingAtA{chainStore(t)}, ln)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn := dialWhenUp(t, ctx, addr)
	defer func() { _ = conn.CloseNow() }()

	write := func(m *noituv1.ClientMessage) {
		t.Helper()
		raw, err := proto.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.Write(ctx, websocket.MessageBinary, raw); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	read := func() (*noituv1.ServerMessage, error) {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return nil, err
		}
		var m noituv1.ServerMessage
		return &m, proto.Unmarshal(raw, &m)
	}
	await := func(match func(*noituv1.ServerMessage) bool) *noituv1.ServerMessage {
		t.Helper()
		for range 20 {
			m, err := read()
			if err != nil {
				t.Fatalf("read before the expected message: %v", err)
			}
			if match(m) {
				return m
			}
		}
		t.Fatal("expected message never arrived")
		return nil
	}

	write(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Hello{Hello: &noituv1.Hello{
		ProtocolVersion: wsapi.ProtocolVersion, Nickname: "Người thử",
	}}})
	await(func(m *noituv1.ServerMessage) bool { return m.GetWelcome() != nil })
	write(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
		StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
	}})
	await(func(m *noituv1.ServerMessage) bool { return m.GetGameStarted() != nil })

	begin := time.Now()
	signal()

	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("the signal context was never released for a second signal to act on")
	}

	// Draining, with the game still running: readiness has flipped but the
	// player has not been dropped.
	deadline := time.Now().Add(drain / 2)
	for {
		resp, err := http.Get("http://" + addr + "/readyz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusServiceUnavailable {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("/readyz did not report draining while the game was alive")
		}
		time.Sleep(20 * time.Millisecond)
	}

	m := await(func(m *noituv1.ServerMessage) bool { return m.GetError() != nil })
	if code := m.GetError().GetCode(); code != "server_restarting" {
		t.Errorf("error code = %q, want server_restarting", code)
	}
	if waited := time.Since(begin); waited < drain-100*time.Millisecond {
		t.Errorf("shut down after %v, before the %v drain timeout: the live game was not waited for", waited, drain)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve returned %v", err)
		}
	case <-time.After(drain + 10*time.Second):
		t.Fatal("serve did not return after the drain timeout")
	}
}

func dialWhenUp(t *testing.T, ctx context.Context, addr string) *websocket.Conn {
	t.Helper()
	var lastErr error
	for range 50 {
		conn, _, err := websocket.Dial(ctx, "ws://"+addr+"/ws", nil)
		if err == nil {
			return conn
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("server never came up: %v", lastErr)
	return nil
}

// recorder logs the order the shutdown sequence drives the server in, and how
// many games were alive at each step.
type recorder struct {
	mu    sync.Mutex
	games int64
	steps []string
}

func (r *recorder) note(step string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, step)
}
func (r *recorder) RoomCount() int { return 1 }
func (r *recorder) LiveGameCount() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.games
}
func (r *recorder) StartDraining() {
	r.note("drain")
	r.mu.Lock()
	alive := r.games
	r.mu.Unlock()
	if alive == 0 {
		r.note("drain-with-no-games")
	}
}
func (r *recorder) Shutdown() { r.note("shutdown") }

func TestDrainAndShutdownOrdersDrainBeforeShutdown(t *testing.T) {
	r := &recorder{games: 1}
	start := time.Now()
	drainAndShutdown(r, 300*time.Millisecond, 0)

	if got := strings.Join(r.steps, ","); got != "drain,shutdown" {
		t.Errorf("steps = %q, want drain then shutdown, with games alive at the drain", got)
	}
	if took := time.Since(start); took < 250*time.Millisecond || took > 5*time.Second {
		t.Errorf("returned after %v, want about the 300ms drain timeout", took)
	}
}

func TestServersSetOnlyAnIdleTimeout(t *testing.T) {
	for name, srv := range map[string]*http.Server{
		"public": newHTTPServer(":0", http.NotFoundHandler()),
		"debug":  newDebugServer(":0"),
	} {
		if srv.IdleTimeout != idleTimeout {
			t.Errorf("%s: IdleTimeout = %v, want %v", name, srv.IdleTimeout, idleTimeout)
		}
		// A read or write deadline outlives the hijack and would end every
		// WebSocket game when it fires.
		if srv.ReadTimeout != 0 || srv.WriteTimeout != 0 {
			t.Errorf("%s: ReadTimeout/WriteTimeout = %v/%v, want both unset", name, srv.ReadTimeout, srv.WriteTimeout)
		}
	}
}

func TestCheckHealth(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
		}
	}))
	defer ok.Close()
	okAddr := strings.TrimPrefix(ok.URL, "http://")
	_, port, _ := net.SplitHostPort(okAddr)

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer broken.Close()

	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := dead.Addr().String()
	_ = dead.Close()

	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{"healthy server", okAddr, false},
		{"bare port, as in the default :8080", ":" + port, false},
		{"wildcard host is dialled on loopback", "0.0.0.0:" + port, false},
		{"non-200 fails", strings.TrimPrefix(broken.URL, "http://"), true},
		{"nothing listening fails", deadAddr, true},
		{"malformed address fails", "not-an-address", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkHealth(tc.addr, time.Second)
			if (err != nil) != tc.wantErr {
				t.Errorf("checkHealth(%q) = %v, wantErr %v", tc.addr, err, tc.wantErr)
			}
		})
	}
}
