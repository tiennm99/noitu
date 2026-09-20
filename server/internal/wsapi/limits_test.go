package wsapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"google.golang.org/protobuf/proto"
)

// TestPlayedWordNamesItsPlayerOnTheWire is the producer-side check: the chain
// byline in a room of three or four is drawn from PlayedWord.player_id, and a
// fixture that hand-writes the field proves nothing about the room.
func TestPlayedWordNamesItsPlayerOnTheWire(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	lead, waits, start := pvpGame(t, url)

	lead.submit("b c", start.GetTurnSeq())
	played := waits.await("turn_update").GetTurnUpdate().GetPlayed()

	if played.GetPlayerId() == "" {
		t.Fatal("player_id is empty on the wire")
	}
	if played.GetPlayerId() != start.GetTurnPlayerId() {
		t.Errorf("player_id = %q, want the leader %q", played.GetPlayerId(), start.GetTurnPlayerId())
	}
}

// TestTypedWordIsSanitizedBeforeItIsEchoed guards the trust boundary the typed
// text crosses: every seat is shown it, so format characters must be gone
// before the engine stores it.
func TestTypedWordIsSanitizedBeforeItIsEchoed(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	lead, waits, start := pvpGame(t, url)

	// A zero-width joiner and a bidi override inside an otherwise legal word.
	lead.submit("b‍ ‮c", start.GetTurnSeq())
	played := waits.await("turn_update").GetTurnUpdate().GetPlayed()

	if played.GetWord() != "b c" {
		t.Fatalf("word = %q, want the move accepted as %q", played.GetWord(), "b c")
	}
	if played.GetTyped() != "b c" {
		t.Errorf("typed = %q still carries non-printing characters", played.GetTyped())
	}
}

// TestRoomCapRefusesTheNextRoom bounds live rooms across the process, not per
// connection: a fleet of connections each under its own limiter must still
// hit a ceiling.
func TestRoomCapRefusesTheNextRoom(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{MaxRooms: 2})

	for range 2 {
		c := dial(t, url)
		c.hello("Chủ phòng")
		c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
		c.await("room_state")
	}

	third := dial(t, url)
	third.hello("Người thứ ba")
	third.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	if got := third.await("error").GetError().GetCode(); got != "server_full" {
		t.Errorf("error code = %q, want server_full", got)
	}
}

// TestConnectionCapRefusesBeforeUpgrade: the refusal is an HTTP status a
// client can read, and it costs the server no socket.
func TestConnectionCapRefusesBeforeUpgrade(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{MaxConnections: 1})

	first := dial(t, url)
	first.hello("Một")

	_, resp, err := websocket.Dial(t.Context(), url+"/ws", nil)
	if err == nil {
		t.Fatal("second connection was accepted past the cap")
	}
	if resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("want 503 before the upgrade, got %v (err %v)", resp, err)
	}
}

// TestFrameFloodClosesTheConnection: a message that matches no dispatch arm
// used to be free at line rate. Now every frame is metered before it is
// decoded, and a flood is closed rather than throttled.
func TestFrameFloodClosesTheConnection(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người thử")

	empty, _ := proto.Marshal(&noituv1.ClientMessage{})
	for range frameBurst + 20 {
		if err := c.conn.Write(c.ctx, websocket.MessageBinary, empty); err != nil {
			return // closed on us mid-flood, which is the point
		}
	}
	for range 5 {
		_, _, err := c.conn.Read(c.ctx)
		if err != nil {
			return
		}
	}
	t.Error("connection survived a frame flood")
}

// TestClientIPTrustsOnlyConfiguredProxies covers the three shapes the limiter
// key can take: no proxy configured, a trusted proxy carrying a forwarded
// chain, and a forged header from a peer that is not a proxy.
func TestClientIPTrustsOnlyConfiguredProxies(t *testing.T) {
	req := func(remote, xff string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/ws", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}

	plain := &Server{}
	if got := plain.clientIP(req("203.0.113.9:4000", "10.0.0.1")); got != "203.0.113.9" {
		t.Errorf("no proxies configured: got %q, want the peer", got)
	}

	s := &Server{proxies: parsePrefixes([]string{"127.0.0.1", "10.0.0.0/8", "not-an-address"})}
	cases := []struct{ remote, xff, want string }{
		// The proxy appended the client; the client forged nothing.
		{"127.0.0.1:5000", "198.51.100.7", "198.51.100.7"},
		// Two trusted hops behind the peer, then the client.
		{"127.0.0.1:5000", "198.51.100.7, 10.1.2.3", "198.51.100.7"},
		// The client forged a header; the proxy appended the real address
		// after it, and the rightmost untrusted entry wins.
		{"10.9.9.9:5000", "1.2.3.4, 198.51.100.7", "198.51.100.7"},
		// A peer that is not a proxy is taken at its socket address, header
		// or not.
		{"203.0.113.9:4000", "198.51.100.7", "203.0.113.9"},
		// A garbage hop falls back to the proxy itself rather than keying a
		// bucket on whatever was typed.
		{"127.0.0.1:5000", "not an ip", "127.0.0.1"},
		// An IPv4-mapped peer still matches its IPv4 prefix.
		{"[::ffff:127.0.0.1]:5000", "198.51.100.7", "198.51.100.7"},
	}
	for _, tc := range cases {
		if got := s.clientIP(req(tc.remote, tc.xff)); got != tc.want {
			t.Errorf("remote=%s xff=%q: got %q, want %q", tc.remote, tc.xff, got, tc.want)
		}
	}
}

// TestIdleRoomReleasesItsSeats: a room that closes on its idle clock must let
// go of the connections still sitting in it, or they are stuck pointing at a
// goroutine that has exited and can never be seated cleanly again.
func TestIdleRoomReleasesItsSeats(t *testing.T) {
	api, url := newTestServer(t, chainDict(), Config{IdleFor: 200 * time.Millisecond})
	host := dial(t, url)
	host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	host.await("room_state")

	if got := host.await("error").GetError().GetCode(); got != "room_idle_closed" {
		t.Fatalf("error code = %q, want room_idle_closed", got)
	}
	settle()
	if n := api.hub.roomCount(); n != 0 {
		t.Fatalf("%d rooms still registered after the idle close", n)
	}

	// The connection is free again: a second room opens and seats it.
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	if host.await("room_state").GetRoomState().GetRoomCode() == "" {
		t.Error("could not be seated in a new room after the idle close")
	}
}

// pvpGame seats two players, starts the game and sorts them into the one who
// drew the first turn and the one who waits.
func pvpGame(t *testing.T, url string) (lead, waits *testClient, start *noituv1.GameStarted) {
	t.Helper()
	host, guest, _ := pvpLobby(t, url)
	guest.setReady(true)
	host.await("room_state")
	host.startGame()
	return awaitLead(t, host, guest)
}
