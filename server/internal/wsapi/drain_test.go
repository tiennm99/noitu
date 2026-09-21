package wsapi

import (
	"io"
	"net/http"
	"testing"
	"time"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
)

// TestReadyzFlipsOnDrain: /healthz is liveness and never moves; /readyz is
// what a load balancer should stop trusting once the process has started
// shutting down.
func TestReadyzFlipsOnDrain(t *testing.T) {
	api, url := newTestServer(t, chainDict(), Config{})
	httpURL := "http" + url[len("ws"):]

	resp, err := http.Get(httpURL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("before draining: status = %d, want 200", resp.StatusCode)
	}

	api.StartDraining()

	resp, err = http.Get(httpURL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("while draining: status = %d, want 503", resp.StatusCode)
	}

	resp, err = http.Get(httpURL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz while draining: status = %d, want 200 (liveness must not move)", resp.StatusCode)
	}
}

// TestDrainRefusesNewRooms: a creator past the drain point is told the server
// is restarting, not that it is full — a full room might free up, a draining
// one never will.
func TestDrainRefusesNewRooms(t *testing.T) {
	api, url := newTestServer(t, chainDict(), Config{})
	api.StartDraining()

	c := dial(t, url)
	c.hello("Người thử")
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})

	if got := c.await("error").GetError().GetCode(); got != "server_restarting" {
		t.Errorf("error code = %q, want server_restarting", got)
	}
}

// TestLiveGameCountTracksGamesNotLobbies is the invariant a drain waits on:
// an empty room, or one sitting in its lobby, costs a restart nothing, and
// must not be counted the way a running game is.
func TestLiveGameCountTracksGamesNotLobbies(t *testing.T) {
	api, url := newTestServer(t, chainDict(), Config{TurnLimit: 200 * time.Millisecond})

	host, guest, _ := pvpLobby(t, url)
	if got := api.LiveGameCount(); got != 0 {
		t.Fatalf("a lobby with nobody playing counted as %d live games, want 0", got)
	}

	guest.setReady(true)
	host.await("room_state")
	host.startGame()
	_, waits, _ := awaitLead(t, host, guest)

	if got := api.LiveGameCount(); got != 1 {
		t.Fatalf("live games = %d after starting, want 1", got)
	}

	// Nobody submits anything: the lead's own turn runs out the clock, which
	// is enough to end a two-seat game.
	waits.await("game_over")

	settle()
	if got := api.LiveGameCount(); got != 0 {
		t.Errorf("live games = %d after the game ended, want 0", got)
	}
}

// TestDrainRefusesStartGameOnAnExistingLobby covers what newRegisteredRoom's
// own drain check cannot: a lobby that existed before the drain decision has
// no further room-creation call to refuse, so beginGame itself has to know.
func TestDrainRefusesStartGameOnAnExistingLobby(t *testing.T) {
	api, url := newTestServer(t, chainDict(), Config{})
	host, guest, _ := pvpLobby(t, url)
	guest.setReady(true)
	host.await("room_state")

	api.StartDraining()

	host.startGame()
	if got := host.await("error").GetError().GetCode(); got != "server_restarting" {
		t.Errorf("error code = %q, want server_restarting", got)
	}
}

// TestDrainRefusesQuickMatchAutoStart is C3's other beginGame call site: the
// second seat filling after the drain decision must not start a game either,
// even though nobody sent StartGame for it to refuse.
func TestDrainRefusesQuickMatchAutoStart(t *testing.T) {
	h := &hub{}
	h.draining.Store(true)
	first := &session{id: "a", ctx: t.Context(), out: make(chan []byte, 8)}
	second := &session{id: "b", ctx: t.Context(), out: make(chan []byte, 8)}

	r := &room{hub: h, dict: chainDict(), turnLimit: time.Second, graceFor: time.Minute}
	r.handleCreate(createInput{sess: first, autoStart: true})
	r.handleJoin(joinInput{sess: second})

	if r.engine != nil {
		t.Error("quick match auto-started a game after the drain decision")
	}
}

// TestDrainLetsALiveGameFinish is the other half of C3: draining must not cut
// a game that was already running short, which is the very outcome it exists
// to avoid.
func TestDrainLetsALiveGameFinish(t *testing.T) {
	api, url := newTestServer(t, chainDict(), Config{TurnLimit: 200 * time.Millisecond})
	lead, waits, _ := pvpGame(t, url)

	api.StartDraining()

	// Nobody submits anything: the lead's own turn runs out the clock, which
	// is enough to end a two-seat game, same as TestLiveGameCountTracksGamesNotLobbies.
	waits.await("game_over")
	lead.await("game_over")
	if got := api.LiveGameCount(); got != 0 {
		t.Errorf("live games = %d after the game ended during a drain, want 0", got)
	}
}

// TestVersionEndpoint: GET /version answers with exactly the string the
// server was configured with, in plain text, so a deploy check can diff it
// against what was just built without parsing anything.
func TestVersionEndpoint(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{Version: "v1.2.3-test"})
	httpURL := "http" + url[len("ws"):]

	resp, err := http.Get(httpURL + "/version")
	if err != nil {
		t.Fatalf("GET /version: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if got := string(body); got != "v1.2.3-test" {
		t.Errorf("/version = %q, want %q", got, "v1.2.3-test")
	}
}

// TestVersionEndpointDefaultsWhenUnset: a test server, like a plain `go run`,
// never sets Config.Version, and /version must still answer something rather
// than an empty body a deploy check would misread as broken.
func TestVersionEndpointDefaultsWhenUnset(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	httpURL := "http" + url[len("ws"):]

	resp, err := http.Get(httpURL + "/version")
	if err != nil {
		t.Fatalf("GET /version: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if got := string(body); got != defaultVersion {
		t.Errorf("/version = %q, want the default %q", got, defaultVersion)
	}
}

// TestDebugVarsNotOnPublicMux: expvar's counters are process-global, but
// /debug/vars is only ever supposed to answer on the separate address
// NOITU_DEBUG_ADDR names. The public mux this test dials must never carry it.
func TestDebugVarsNotOnPublicMux(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	httpURL := "http" + url[len("ws"):]

	resp, err := http.Get(httpURL + "/debug/vars")
	if err != nil {
		t.Fatalf("GET /debug/vars: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("/debug/vars on the public mux = %d, want 404 (it must live on NOITU_DEBUG_ADDR alone)", resp.StatusCode)
	}
}
