package wsapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"google.golang.org/protobuf/proto"
)

// Named so a reader can see what the sanitizer tests are sending: all three
// are invisible in source, which is exactly why they matter as input.
const (
	nul            = rune(0)
	zeroWidthSpace = rune(0x200B)
	bidiOverride   = rune(0x202E)
)

// Regressions for defects found in review. Each one failed before its fix, so
// each is a guard against the same mistake returning rather than a restatement
// of behaviour already covered elsewhere.

// startPvP runs two clients up to the point where the game is live.
func startPvP(t *testing.T, url string) (host, guest *testClient, code string) {
	t.Helper()
	host = dial(t, url)
	host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	code = host.await("room_created").GetRoomCreated().GetRoomCode()

	guest = dial(t, url)
	guest.hello("Khách")
	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	host.await("game_started")
	guest.await("game_started")
	return host, guest, code
}

// silentFor asserts the client receives no message of the given kind within a
// short window. Used where the correct behaviour is that nothing happens.
func silentFor(t *testing.T, c *testClient, unwanted string, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()

	for {
		_, raw, err := c.conn.Read(ctx)
		if err != nil {
			return // nothing arrived, which is the pass
		}
		var m noituv1.ServerMessage
		if err := proto.Unmarshal(raw, &m); err != nil {
			return
		}
		if payloadCase(&m) == unwanted {
			t.Fatalf("received an unexpected %q", unwanted)
		}
	}
}

// TestStrangerCannotResignForASeatedPlayer is the authorization case. A room
// code is pasted into group chats by design, so holding one must not be enough
// to act as a player who is already seated.
func TestStrangerCannotResignForASeatedPlayer(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{TurnLimit: 10 * time.Second})
	_, guest, code := startPvP(t, url)

	stranger := dial(t, url)
	stranger.hello("Kẻ lạ")
	stranger.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	if got := stranger.await("error").GetError().GetCode(); got != "room_full" {
		t.Fatalf("third joiner got %q, want room_full", got)
	}

	stranger.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Resign{Resign: &noituv1.Resign{}}})

	silentFor(t, guest, "game_over", 400*time.Millisecond)
}

// TestStrangerCannotSubmitForASeatedPlayer is the same hole via SubmitWord:
// turn_seq is a small integer, so its being unguessable is not a defence.
func TestStrangerCannotSubmitForASeatedPlayer(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{TurnLimit: 10 * time.Second})
	host, _, code := startPvP(t, url)

	stranger := dial(t, url)
	stranger.hello("Kẻ lạ")
	stranger.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	stranger.await("error")

	stranger.submit("b c", 1)

	silentFor(t, host, "turn_update", 400*time.Millisecond)
}

// TestCannotJoinYourOwnRoom stops a creator occupying both seats, which would
// leave one connection playing itself.
func TestCannotJoinYourOwnRoom(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host := dial(t, url)
	host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	code := host.await("room_created").GetRoomCreated().GetRoomCode()

	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	if got := host.await("error").GetError().GetCode(); got != "cannot_join_own_room" {
		t.Errorf("self-join returned %q", got)
	}
}

// TestShutdownNotifiesPlayersMidGame is acceptance criterion (i). It races
// Shutdown against in-flight reads, which is where cancelling the read context
// used to destroy the socket before the notice could leave.
//
// Several connections rather than one: whether any single socket loses that
// race is timing, so one client would make this an intermittent guard against
// a defect that is entirely deterministic in its consequences.
func TestShutdownNotifiesPlayersMidGame(t *testing.T) {
	api, url := newTestServer(t, chainDict(), Config{TurnLimit: 10 * time.Second})

	const players = 8
	clients := make([]*testClient, 0, players)
	for i := range players {
		c := dial(t, url)
		c.hello("Người chơi")
		c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
			StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
		}})
		c.await("game_started")
		clients = append(clients, c)
		_ = i
	}

	api.Shutdown()

	for i, c := range clients {
		if got := c.await("error").GetError().GetCode(); got != "server_restarting" {
			t.Errorf("client %d shutdown notice = %q, want server_restarting", i, got)
		}
	}
}

// TestAbandonedRoomIsEvicted covers the leak: a room whose game never started
// has no turn timer and no opponent, so nothing else could ever end it.
func TestAbandonedRoomIsEvicted(t *testing.T) {
	api, url := newTestServer(t, chainDict(), Config{})

	host := dial(t, url)
	host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	host.await("room_created")

	_ = host.conn.Close(websocket.StatusGoingAway, "")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if api.hub.roomCount() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("%d rooms still live after the only player left before the game began", api.hub.roomCount())
}

// TestRoomCreationIsRateLimited stops one connection minting goroutines,
// engines and registry entries without bound.
func TestRoomCreationIsRateLimited(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người thử")

	for range roomBurst + 3 {
		c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	}

	for range 40 {
		m := c.recv()
		if payloadCase(m) == "error" && m.GetError().GetCode() == "too_many_rooms" {
			return
		}
	}
	t.Error("one connection created rooms past the burst without being limited")
}

// TestPvPGameRunsToAWinner plays a PvP game to its natural end, rather than
// stopping after the first move as the alternation test does.
func TestPvPGameRunsToAWinner(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{TurnLimit: 10 * time.Second})
	host, guest, _ := startPvP(t, url)

	// a b (opening) -> b c -> c d -> d e, after which the guest has nothing.
	host.submit("b c", 1)
	guestTurn := awaitMyTurn(t, guest)
	guest.submit("c d", guestTurn.GetTurnSeq())
	hostTurn := awaitMyTurn(t, host)
	host.submit("d e", hostTurn.GetTurnSeq())

	hostOver := host.await("game_over").GetGameOver()
	guestOver := guest.await("game_over").GetGameOver()

	if hostOver.GetIWon() == guestOver.GetIWon() {
		t.Fatalf("both players were told the same outcome: host=%v guest=%v",
			hostOver.GetIWon(), guestOver.GetIWon())
	}
	if !hostOver.GetIWon() {
		t.Error("the player who left their opponent with no legal move should win")
	}
	if hostOver.GetReason() != noituv1.GameEndReason_GAME_END_REASON_NO_LEGAL_MOVE {
		t.Errorf("reason = %v, want NO_LEGAL_MOVE", hostOver.GetReason())
	}
}

// awaitMyTurn reads turn updates until this client is the one on turn. Both
// players see every move, so the first update after a submission is the
// player's own echo.
func awaitMyTurn(t *testing.T, c *testClient) *noituv1.TurnUpdate {
	t.Helper()
	for range 10 {
		if u := c.await("turn_update").GetTurnUpdate(); u.GetMyTurn() {
			return u
		}
	}
	t.Fatal("never became the player on turn")
	return nil
}

// TestOpponentNeverSeesAnUnsanitizedNickname is the boundary that matters:
// sanitization is only worth anything if the hostile string cannot reach the
// other player's screen.
func TestOpponentNeverSeesAnUnsanitizedNickname(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{TurnLimit: 10 * time.Second})

	host := dial(t, url)
	host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	code := host.await("room_created").GetRoomCreated().GetRoomCode()

	hostile := "  Kẻ" + string(nul) + " xấu" + string(zeroWidthSpace) + string(bidiOverride) +
		"  " + strings.Repeat("z", 40)
	guest := dial(t, url)
	guest.hello(hostile)
	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})

	shown := host.await("room_joined").GetRoomJoined().GetOpponentName()
	for _, r := range []rune{nul, zeroWidthSpace, bidiOverride} {
		if strings.ContainsRune(shown, r) {
			t.Errorf("opponent name %q still carries the invisible rune %U", shown, r)
		}
	}
	if n := len([]rune(shown)); n > maxNicknameRunes {
		t.Errorf("opponent name is %d runes, over the %d cap", n, maxNicknameRunes)
	}
}

// TestRepeatedHelloIsRejected keeps the handshake a one-shot transition: a
// second Hello would rewrite the nickname of a player already in a game.
func TestRepeatedHelloIsRejected(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người thử")

	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Hello{Hello: &noituv1.Hello{
		ProtocolVersion: ProtocolVersion,
		Nickname:        "Người khác",
	}}})
	if got := c.await("error").GetError().GetCode(); got != "already_greeted" {
		t.Errorf("second hello returned %q, want already_greeted", got)
	}
}

// TestStaleRejectionCarriesTheServerSequence lets a client resynchronise from
// the refusal instead of waiting for the next turn update to discover where
// the game actually is.
func TestStaleRejectionCarriesTheServerSequence(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người thử")
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
		StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
	}})
	started := c.await("game_started").GetGameStarted()

	c.submit("b c", started.GetTurnSeq()+99)

	got := c.await("move_rejected").GetMoveRejected()
	if got.GetTurnSeq() != started.GetTurnSeq() {
		t.Errorf("rejection carried seq %d, want the server's %d", got.GetTurnSeq(), started.GetTurnSeq())
	}
}

// TestResumeAfterGameEndedSaysSo. GraceFor is longer than TurnLimit, so a
// player who drops on their own turn loses before the grace window closes.
// Their reconnect must be told that, not left holding a Welcome and silence.
func TestResumeAfterGameEndedSaysSo(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{
		TurnLimit: 200 * time.Millisecond,
		GraceFor:  5 * time.Second,
	})

	host := dial(t, url)
	welcome := host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	code := host.await("room_created").GetRoomCreated().GetRoomCode()

	guest := dial(t, url)
	guest.hello("Khách")
	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	host.await("game_started")
	guest.await("game_started")

	// The host is on turn; drop and let the turn clock run out.
	_ = host.conn.Close(websocket.StatusGoingAway, "")
	guest.await("game_over")

	back := dial(t, url)
	back.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Hello{Hello: &noituv1.Hello{
		ProtocolVersion: ProtocolVersion,
		Nickname:        "Chủ phòng",
		ResumeToken:     welcome.GetResumeToken(),
	}}})
	back.await("welcome")

	if got := back.await("error").GetError().GetCode(); got != "game_already_over" {
		t.Errorf("resume into a finished game returned %q", got)
	}
}

func TestUnderRootIsAPathBoundaryNotAPrefix(t *testing.T) {
	root := filepath.Clean("/srv/web")

	if underRoot(root, filepath.Clean("/srv/webhooks/secret")) {
		t.Error("a sibling directory sharing a name prefix must not count as inside the root")
	}
	if !underRoot(root, filepath.Clean("/srv/web/index.html")) {
		t.Error("a real child was rejected")
	}
	if !underRoot(root, root) {
		t.Error("the root itself should be allowed")
	}
}

// TestRoomCodesAreUnbiased checks the rejection sampling. A modulo over 256
// would make the first eight letters of the alphabet ~12% more likely, which
// this tolerance catches and ordinary randomness does not trip.
func TestRoomCodesAreUnbiased(t *testing.T) {
	const draws = 10000

	counts := map[rune]int{}
	for range draws {
		for _, r := range randomCode() {
			counts[r]++
		}
	}

	expected := float64(draws*roomCodeLen) / float64(len(roomCodeAlphabet))
	for _, r := range roomCodeAlphabet {
		got := float64(counts[r])
		if got < expected*0.9 || got > expected*1.1 {
			t.Errorf("letter %q drawn %.0f times, expected ~%.0f", r, got, expected)
		}
	}
}

// TestStaticCacheHeaders pins the two cache policies the single-page bundle
// depends on. The failure they prevent is silent and total: an index.html
// cached across a deploy names hashed assets that no longer exist, so the app
// loads into a blank page with nothing in the log.
func TestStaticCacheHeaders(t *testing.T) {
	dir := t.TempDir()
	assets := filepath.Join(dir, "_app", "immutable", "chunks")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "abc123.js"), []byte("export{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(context.Background(), newTestDict("ngữ pháp", "pháp luật"), Config{WebDir: dir})
	defer srv.Shutdown()

	cases := []struct {
		name string
		path string
		want string
	}{
		{"hashed asset", "/_app/immutable/chunks/abc123.js", "public, max-age=31536000, immutable"},
		{"the shell", "/", "no-cache"},
		{"a deep link falling back to the shell", "/play?difficulty=2", "no-cache"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if got := rec.Header().Get("Cache-Control"); got != tc.want {
				t.Errorf("Cache-Control = %q, want %q", got, tc.want)
			}
		})
	}
}
