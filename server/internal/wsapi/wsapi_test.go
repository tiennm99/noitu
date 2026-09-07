package wsapi

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/game"
	"google.golang.org/protobuf/proto"
)

// testDict is a hand-built word graph.
//
// A dozen words with edges a reader can follow beats a real dictionary here:
// when a test says the bot must lose, the reason is visible in the graph
// rather than buried in 48,000 entries.
type testDict struct {
	// words maps a canonical word to its first and last syllable.
	words   map[string][2]string
	opening string
}

func newTestDict(opening string, words ...string) *testDict {
	d := &testDict{words: map[string][2]string{}, opening: opening}
	for _, w := range append(words, opening) {
		parts := strings.Fields(w)
		d.words[w] = [2]string{parts[0], parts[len(parts)-1]}
	}
	return d
}

func (d *testDict) Resolve(word string) (string, bool) {
	_, ok := d.words[word]
	return word, ok
}

func (d *testDict) FirstSyllable(word string) (string, bool) {
	e, ok := d.words[word]
	return e[0], ok
}

func (d *testDict) LastSyllable(word string) (string, bool) {
	e, ok := d.words[word]
	return e[1], ok
}

func (d *testDict) WordsStartingWith(syllable string) iter.Seq[string] {
	return func(yield func(string) bool) {
		// Sorted so a failing test reproduces rather than depending on map
		// iteration order.
		for _, w := range slices.Sorted(maps(d.words)) {
			if d.words[w][0] == syllable && !yield(w) {
				return
			}
		}
	}
}

func (d *testDict) OutDegree(syllable string) (int, error) {
	n := 0
	for range d.WordsStartingWith(syllable) {
		n++
	}
	return n, nil
}

func (d *testDict) RandomOpeningWord(int) (string, error) { return d.opening, nil }

func maps(m map[string][2]string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// chainDict builds a graph that is a single forced path, so the whole game is
// predictable: each word has exactly one legal continuation.
//
//	a b -> b c -> c d -> d e (dead end)
func chainDict() *testDict {
	return newTestDict("a b", "b c", "c d", "d e")
}

// --- test client -----------------------------------------------------------

type testClient struct {
	t    *testing.T
	conn *websocket.Conn
	ctx  context.Context
}

func newTestServer(t *testing.T, dict Dictionary, cfg Config) (*Server, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	if cfg.TurnLimit == 0 {
		cfg.TurnLimit = 2 * time.Second
	}
	if cfg.GraceFor == 0 {
		cfg.GraceFor = time.Second
	}
	// Long enough that no test loses its lobby to the idle clock. The one
	// test that exercises that clock sets its own.
	if cfg.IdleFor == 0 {
		cfg.IdleFor = 30 * time.Second
	}

	api := NewServer(ctx, dict, cfg)
	hs := httptest.NewServer(api)
	t.Cleanup(func() {
		api.Shutdown()
		hs.Close()
	})
	return api, "ws" + strings.TrimPrefix(hs.URL, "http")
}

func dial(t *testing.T, url string) *testClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	conn, _, err := websocket.Dial(ctx, url+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })

	return &testClient{t: t, conn: conn, ctx: ctx}
}

func (c *testClient) send(m *noituv1.ClientMessage) {
	c.t.Helper()
	raw, err := proto.Marshal(m)
	if err != nil {
		c.t.Fatalf("marshal: %v", err)
	}
	if err := c.conn.Write(c.ctx, websocket.MessageBinary, raw); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

func (c *testClient) recv() *noituv1.ServerMessage {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	typ, raw, err := c.conn.Read(ctx)
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageBinary {
		c.t.Fatalf("expected a binary frame, got %v", typ)
	}
	var m noituv1.ServerMessage
	if err := proto.Unmarshal(raw, &m); err != nil {
		c.t.Fatalf("unmarshal: %v", err)
	}
	return &m
}

// await reads until a message of the wanted case arrives, so a test states the
// event it cares about rather than every frame that precedes it.
func (c *testClient) await(want string) *noituv1.ServerMessage {
	c.t.Helper()
	for range 20 {
		m := c.recv()
		if payloadCase(m) == want {
			return m
		}
	}
	c.t.Fatalf("never received a %q message", want)
	return nil
}

func payloadCase(m *noituv1.ServerMessage) string {
	switch m.GetPayload().(type) {
	case *noituv1.ServerMessage_Welcome:
		return "welcome"
	case *noituv1.ServerMessage_GameStarted:
		return "game_started"
	case *noituv1.ServerMessage_TurnUpdate:
		return "turn_update"
	case *noituv1.ServerMessage_MoveRejected:
		return "move_rejected"
	case *noituv1.ServerMessage_GameOver:
		return "game_over"
	case *noituv1.ServerMessage_OpponentLeft:
		return "opponent_left"
	case *noituv1.ServerMessage_Error:
		return "error"
	case *noituv1.ServerMessage_Pong:
		return "pong"
	case *noituv1.ServerMessage_RoomState:
		return "room_state"
	}
	// Named rather than empty: a missing arm here makes every await for that
	// message time out with nothing to say about why.
	return fmt.Sprintf("unmapped(%T)", m.GetPayload())
}

func (c *testClient) hello(nickname string) *noituv1.Welcome {
	c.t.Helper()
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Hello{Hello: &noituv1.Hello{
		ProtocolVersion: ProtocolVersion,
		Nickname:        nickname,
	}}})
	return c.await("welcome").GetWelcome()
}

func (c *testClient) submit(word string, seq uint32) {
	c.t.Helper()
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_SubmitWord{
		SubmitWord: &noituv1.SubmitWord{Word: word, TurnSeq: seq},
	}})
}

// --- tests -----------------------------------------------------------------

// TestBotGamePlaysToCompletion walks a whole vs-bot game with no frontend
// involved. The graph is a forced chain, so the ending is not a matter of
// which word the bot happens to pick.
func TestBotGamePlaysToCompletion(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Ăn")

	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
		StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
	}})

	started := c.await("game_started").GetGameStarted()
	if started.GetOpeningWord() != "a b" {
		t.Fatalf("opening = %q, want %q", started.GetOpeningWord(), "a b")
	}
	if !started.GetMyTurn() {
		t.Fatal("human should move first in a bot game")
	}
	if started.GetCurrentSyllable() != "b" {
		t.Fatalf("current syllable = %q, want b", started.GetCurrentSyllable())
	}

	// "b c" is the only legal reply. The bot then has only "c d", which leaves
	// the human "d e", after which the bot has nothing and loses.
	c.submit("b c", started.GetTurnSeq())

	// The player's own move comes back first — every accepted move is fanned
	// out to both seats — so the bot's reply is the second update.
	if own := c.await("turn_update").GetTurnUpdate(); !own.GetPlayed().GetByMe() {
		t.Fatal("first update after submitting should be the player's own move")
	}
	botReply := c.await("turn_update").GetTurnUpdate()
	if botReply.GetPlayed().GetByMe() {
		t.Fatal("expected the bot's move, not another echo")
	}
	if !botReply.GetMyTurn() {
		t.Fatal("human should be on turn after the bot replies")
	}
	c.submit("d e", botReply.GetTurnSeq())

	over := c.await("game_over").GetGameOver()
	if !over.GetIWon() {
		t.Errorf("human should win when the bot runs out of words, got %+v", over)
	}
}

// TestPvPGameAlternatesTurns runs two clients through a full game and checks
// that each sees the other's move rendered from its own side.
func TestPvPGameAlternatesTurns(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})

	host := dial(t, url)
	host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	code := host.await("room_state").GetRoomState().GetRoomCode()

	guest := dial(t, url)
	guest.hello("Khách")
	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	readyAndStart(t, host, guest)

	hostStart := host.await("game_started").GetGameStarted()
	guestStart := guest.await("game_started").GetGameStarted()

	if hostStart.GetMyTurn() == guestStart.GetMyTurn() {
		t.Fatal("exactly one player must be on turn")
	}
	if !hostStart.GetMyTurn() {
		t.Fatal("the room creator takes the first turn")
	}

	host.submit("b c", hostStart.GetTurnSeq())

	// The same move, rendered per recipient: by_me flips, my_turn flips.
	hostUpdate := host.await("turn_update").GetTurnUpdate()
	guestUpdate := guest.await("turn_update").GetTurnUpdate()

	if !hostUpdate.GetPlayed().GetByMe() {
		t.Error("mover should see by_me = true")
	}
	if guestUpdate.GetPlayed().GetByMe() {
		t.Error("opponent should see by_me = false")
	}
	if hostUpdate.GetMyTurn() {
		t.Error("mover should not be on turn after moving")
	}
	if !guestUpdate.GetMyTurn() {
		t.Error("opponent should now be on turn")
	}
	if hostUpdate.GetMyScore() != guestUpdate.GetOpponentScore() {
		t.Errorf("scores disagree across recipients: %d vs %d",
			hostUpdate.GetMyScore(), guestUpdate.GetOpponentScore())
	}
}

// TestTurnTimeoutEndsGameServerSide proves the clock is the server's. The
// client sends nothing at all after the game starts.
func TestTurnTimeoutEndsGameServerSide(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{TurnLimit: 250 * time.Millisecond})

	host := dial(t, url)
	host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	code := host.await("room_state").GetRoomState().GetRoomCode()

	guest := dial(t, url)
	guest.hello("Khách")
	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	readyAndStart(t, host, guest)

	host.await("game_started")
	guest.await("game_started")

	over := guest.await("game_over").GetGameOver()
	if !over.GetIWon() {
		t.Error("the player who did not time out should win")
	}
	if over.GetReason() != noituv1.GameEndReason_GAME_END_REASON_TIMEOUT {
		t.Errorf("reason = %v, want TIMEOUT", over.GetReason())
	}
}

// TestRejectionsCarryTheRightReason checks each rejection a player can
// actually provoke reaches the client as a distinct enum, since the UI copy
// keys off exactly this value.
func TestRejectionsCarryTheRightReason(t *testing.T) {
	tests := []struct {
		name string
		word string
		want noituv1.RejectReason
	}{
		{"unknown word", "khong co", noituv1.RejectReason_REJECT_REASON_NOT_IN_DICTIONARY},
		{"wrong link", "c d", noituv1.RejectReason_REJECT_REASON_WRONG_LINK},
		{"single syllable", "b", noituv1.RejectReason_REJECT_REASON_TOO_FEW_SYLLABLES},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, url := newTestServer(t, chainDict(), Config{})
			c := dial(t, url)
			c.hello("Người thử")
			c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
				StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
			}})
			started := c.await("game_started").GetGameStarted()

			c.submit(tc.word, started.GetTurnSeq())

			got := c.await("move_rejected").GetMoveRejected()
			if got.GetReason() != tc.want {
				t.Errorf("reason = %v, want %v", got.GetReason(), tc.want)
			}
			if got.GetWord() != tc.word {
				t.Errorf("rejection echoed %q, want %q", got.GetWord(), tc.word)
			}
		})
	}
}

// TestReplayingAWordIsRejected needs its own graph: the engine checks the link
// before reuse, so replaying the opening word on the very first turn reports
// WRONG_LINK. Only a word that links correctly *and* has been played can
// surface ALREADY_USED, which takes a cycle in the graph and a move to reach.
func TestReplayingAWordIsRejected(t *testing.T) {
	// a b -> b a -> back to a word starting with "a", which is the opening.
	dict := newTestDict("a b", "b a", "a c", "c a")
	_, url := newTestServer(t, dict, Config{TurnLimit: 10 * time.Second})

	host := dial(t, url)
	host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	code := host.await("room_state").GetRoomState().GetRoomCode()

	guest := dial(t, url)
	guest.hello("Khách")
	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	readyAndStart(t, host, guest)
	hostStart := host.await("game_started").GetGameStarted()
	guest.await("game_started")

	// Opening "a b" is used and current syllable is "b". Play "b a" so the
	// syllable returns to "a", where the opening word now links legally.
	host.submit("b a", hostStart.GetTurnSeq())
	guestTurn := guest.await("turn_update").GetTurnUpdate()

	guest.submit("a b", guestTurn.GetTurnSeq())

	got := guest.await("move_rejected").GetMoveRejected()
	if got.GetReason() != noituv1.RejectReason_REJECT_REASON_ALREADY_USED {
		t.Errorf("reason = %v, want ALREADY_USED", got.GetReason())
	}
}

// TestStaleTurnSeqIsRejected covers the double-submit guard: a word stamped
// with a turn that has already passed must not be applied to the current one.
func TestStaleTurnSeqIsRejected(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người thử")
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
		StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
	}})
	started := c.await("game_started").GetGameStarted()

	c.submit("b c", started.GetTurnSeq()+99)

	got := c.await("move_rejected").GetMoveRejected()
	if got.GetReason() != noituv1.RejectReason_REJECT_REASON_NOT_YOUR_TURN {
		t.Errorf("reason = %v, want NOT_YOUR_TURN", got.GetReason())
	}
}

// TestResumeWithinGraceRestoresGame drops a connection mid-game and brings it
// back with the resume token.
func TestResumeWithinGraceRestoresGame(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{TurnLimit: 10 * time.Second, GraceFor: 5 * time.Second})

	host := dial(t, url)
	welcome := host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	code := host.await("room_state").GetRoomState().GetRoomCode()

	guest := dial(t, url)
	guest.hello("Khách")
	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	readyAndStart(t, host, guest)
	host.await("game_started")
	guest.await("game_started")

	_ = host.conn.Close(websocket.StatusGoingAway, "")

	if left := guest.await("opponent_left").GetOpponentLeft(); !left.GetCanReconnect() {
		t.Error("opponent should be told the seat is being held")
	}

	// Reconnect with the token and expect the position back.
	back := dial(t, url)
	back.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Hello{Hello: &noituv1.Hello{
		ProtocolVersion: ProtocolVersion,
		Nickname:        "Chủ phòng",
		ResumeToken:     welcome.GetResumeToken(),
	}}})
	back.await("welcome")

	restored := back.await("game_started").GetGameStarted()
	if restored.GetOpeningWord() != "a b" {
		t.Errorf("resumed on %q, want the original opening", restored.GetOpeningWord())
	}
	if !restored.GetMyTurn() {
		t.Error("the resumed player was on turn and should still be")
	}
}

// TestGraceExpiryAwardsTheGame is the other half: nobody comes back.
func TestGraceExpiryAwardsTheGame(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{TurnLimit: 10 * time.Second, GraceFor: 200 * time.Millisecond})

	host := dial(t, url)
	host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	code := host.await("room_state").GetRoomState().GetRoomCode()

	guest := dial(t, url)
	guest.hello("Khách")
	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	readyAndStart(t, host, guest)
	host.await("game_started")
	guest.await("game_started")

	_ = host.conn.Close(websocket.StatusGoingAway, "")

	over := guest.await("game_over").GetGameOver()
	if !over.GetIWon() {
		t.Error("the player who stayed should win")
	}
	if over.GetReason() != noituv1.GameEndReason_GAME_END_REASON_OPPONENT_LEFT {
		t.Errorf("reason = %v, want OPPONENT_LEFT", over.GetReason())
	}
}

// TestUnknownRoomCodeIsRefused checks a join against a code nobody holds.
func TestUnknownRoomCodeIsRefused(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Khách")
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: "ZZZZZZ"},
	}})

	if code := c.await("error").GetError().GetCode(); code != "room_not_found" {
		t.Errorf("error code = %q, want room_not_found", code)
	}
}

// TestProtocolVersionMismatchIsRefused proves an incompatible client is told
// so rather than left to misread frames.
func TestProtocolVersionMismatchIsRefused(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Hello{Hello: &noituv1.Hello{
		ProtocolVersion: ProtocolVersion + 1,
		Nickname:        "Cũ",
	}}})

	if code := c.await("error").GetError().GetCode(); code != "protocol_version_mismatch" {
		t.Errorf("error code = %q, want protocol_version_mismatch", code)
	}
}

// TestHandshakeIsRequiredFirst rejects a client that skips Hello.
func TestHandshakeIsRequiredFirst(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})

	if code := c.await("error").GetError().GetCode(); code != "handshake_required" {
		t.Errorf("error code = %q, want handshake_required", code)
	}
}

// TestOversizeFrameClosesConnection covers the read limit. The frame is built
// past maxFrameBytes, so the socket must close rather than the decoder be
// asked to parse it.
func TestOversizeFrameClosesConnection(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người thử")

	c.submit(strings.Repeat("x", maxFrameBytes+1), 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, _, err := c.conn.Read(ctx)
		if err == nil {
			continue
		}
		// The status matters, not merely that the socket closed: any unrelated
		// failure would otherwise let this pass while the read limit did
		// nothing.
		if got := websocket.CloseStatus(err); got != websocket.StatusMessageTooBig {
			t.Errorf("close status = %v, want StatusMessageTooBig", got)
		}
		return
	}
}

// TestSubmitRateLimit checks the token bucket refuses a burst well past what a
// person types, since each submit costs a dictionary lookup.
func TestSubmitRateLimit(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người thử")
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
		StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
	}})
	started := c.await("game_started").GetGameStarted()

	for range submitBurst + 5 {
		c.submit("khong co", started.GetTurnSeq())
	}

	for range 40 {
		m := c.recv()
		if payloadCase(m) == "error" && m.GetError().GetCode() == "too_fast" {
			return
		}
	}
	t.Error("never rate-limited despite submitting past the burst")
}

// TestOriginIsChecked confirms a cross-origin handshake is refused when the
// allowlist does not include it.
func TestOriginIsChecked(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{AllowedOrigins: []string{"example.com"}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, err := websocket.Dial(ctx, url+"/ws", &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Origin": {"http://evil.example"}},
	})
	if err == nil {
		t.Fatal("a disallowed origin completed the handshake")
	}
}

// TestRoomIsEvictedWhenGameEnds guards against rooms accumulating: a finished
// game must leave the registry so its code is reusable.
func TestRoomIsEvictedWhenGameEnds(t *testing.T) {
	api, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người thử")
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
		StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
	}})
	started := c.await("game_started").GetGameStarted()
	c.submit("b c", started.GetTurnSeq())
	c.await("game_over")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if api.hub.roomCount() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("room still registered after the game ended: %d live", api.hub.roomCount())
}

// TestPingIsAnswered covers the clock-offset path the client uses to render a
// countdown against the server's absolute deadline.
func TestPingIsAnswered(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người thử")

	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Ping{
		Ping: &noituv1.Ping{ClientTimeMs: 1234},
	}})

	pong := c.await("pong").GetPong()
	if pong.GetClientTimeMs() != 1234 {
		t.Errorf("pong echoed %d, want 1234", pong.GetClientTimeMs())
	}
	if pong.GetServerTimeMs() == 0 {
		t.Error("pong carried no server clock")
	}
}

// TestTextFrameIsRejected keeps the protocol binary-only: guessing at another
// encoding is how a parser becomes an attack surface.
func TestTextFrameIsRejected(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)

	if err := c.conn.Write(c.ctx, websocket.MessageText, []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if _, _, err := c.conn.Read(ctx); err != nil {
			return
		}
	}
}

func TestDecodeRejectsTextFrames(t *testing.T) {
	if _, err := Decode(websocket.MessageText, nil); !errors.Is(err, ErrNotBinary) {
		t.Errorf("Decode(text) error = %v, want ErrNotBinary", err)
	}
}

func TestHealthz(t *testing.T) {
	api, _ := newTestServer(t, chainDict(), Config{})
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("healthz returned %d, want 200", rec.Code)
	}
}

func TestRoomCodeAlphabetAvoidsLookalikes(t *testing.T) {
	for _, bad := range []rune{'0', 'O', '1', 'I', 'L'} {
		if strings.ContainsRune(roomCodeAlphabet, bad) {
			t.Errorf("alphabet contains the easily-confused %q", bad)
		}
	}

	seen := map[string]bool{}
	for range 1000 {
		code := randomCode()
		if len(code) != roomCodeLen {
			t.Fatalf("code %q is %d chars, want %d", code, len(code), roomCodeLen)
		}
		for _, r := range code {
			if !strings.ContainsRune(roomCodeAlphabet, r) {
				t.Fatalf("code %q contains %q, outside the alphabet", code, r)
			}
		}
		seen[code] = true
	}
	// Not a randomness test — just proof the generator is not returning a
	// constant, which a broken modulo could.
	if len(seen) < 900 {
		t.Errorf("only %d distinct codes in 1000 draws", len(seen))
	}
}

func TestFreezeBoardIncludesTheOpeningWord(t *testing.T) {
	// The engine counts the opening word as played but Snapshot's history does
	// not list it. A frozen board that missed it would let the bot choose a
	// word the engine then rejects as already used — the two would disagree
	// about the position while appearing to share one dictionary.
	dict := chainDict()
	e, err := game.New(dict, []game.PlayerID{"p1", "p2"}, "a b", time.Second, time.Now())
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	board := freezeBoard(e, "a b")
	if !board.Used("a b") {
		t.Error("frozen board does not consider the opening word played")
	}
	if !slices.Contains(board.LegalMoves(), "b c") {
		t.Errorf("legal moves = %v, want the one continuation", board.LegalMoves())
	}
}

func TestSanitizeNickname(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Minh", "Minh"},
		{"vietnamese kept", "Nguyễn Thuý", "Nguyễn Thuý"},
		{"empty falls back", "", defaultNickname},
		{"whitespace only falls back", "   \t\n ", defaultNickname},
		{"control characters stripped", "Mi\x00nh\x07", "Minh"},
		{"zero width stripped", "Mi​nh‍", "Minh"},
		{"bidi override stripped", "Minh‮", "Minh"},
		{"whitespace collapsed", "  Minh    Nguyen  ", "Minh Nguyen"},
		{"newlines become spaces", "Minh\nNguyen", "Minh Nguyen"},
		{"over length truncated", strings.Repeat("a", 40), strings.Repeat("a", maxNicknameRunes)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeNickname(tc.in); got != tc.want {
				t.Errorf("sanitizeNickname(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSanitizeNicknameCountsRunesNotBytes guards the cap against being applied
// in bytes, which would cut a Vietnamese name to a third of the length a Latin
// one keeps.
func TestSanitizeNicknameCountsRunesNotBytes(t *testing.T) {
	in := strings.Repeat("ữ", maxNicknameRunes)
	got := sanitizeNickname(in)
	if n := len([]rune(got)); n != maxNicknameRunes {
		t.Errorf("kept %d runes, want %d (byte-based truncation?)", n, maxNicknameRunes)
	}
}

// TestDistinguishSeparatesIdenticalNames covers the collision the fallback
// creates — two players who both send nothing — and the one it always could:
// two players who choose the same name.
func TestDistinguishSeparatesIdenticalNames(t *testing.T) {
	if got := distinguish(defaultNickname, defaultNickname); got == defaultNickname {
		t.Error("two unnamed players must not render identically")
	}
	if got := distinguish("Minh", "Thuý"); got != "Minh" {
		t.Errorf("distinct names should be left alone, got %q", got)
	}

	// The suffix must not push the name past the cap.
	long := strings.Repeat("a", maxNicknameRunes)
	if got := distinguish(long, long); len([]rune(got)) > maxNicknameRunes {
		t.Errorf("distinguished name is %d runes, over the %d cap", len([]rune(got)), maxNicknameRunes)
	}
}

func TestBucketRefills(t *testing.T) {
	now := time.Now()
	b := newBucket(5, 2, now)

	if !b.allow(now) || !b.allow(now) {
		t.Fatal("burst should cover the first two")
	}
	if b.allow(now) {
		t.Fatal("third call should exhaust the bucket")
	}

	// 5/sec means one token back after 200ms.
	if !b.allow(now.Add(220 * time.Millisecond)) {
		t.Error("bucket did not refill")
	}
}

func TestKeyedLimiterIsPerKey(t *testing.T) {
	now := time.Now()
	l := newKeyedLimiter(1, 1, time.Minute)

	if !l.allow("a", now) {
		t.Fatal("first call for a key should pass")
	}
	if l.allow("a", now) {
		t.Fatal("second call for the same key should be refused")
	}
	if !l.allow("b", now) {
		t.Error("a different key must have its own bucket")
	}
}

func TestKeyedLimiterSweepsIdleBuckets(t *testing.T) {
	now := time.Now()
	l := newKeyedLimiter(10, 5, time.Minute)
	l.allow("gone", now)

	l.sweep(now.Add(2 * time.Minute))

	l.mu.Lock()
	n := len(l.buckets)
	l.mu.Unlock()
	if n != 0 {
		t.Errorf("%d buckets survived the sweep, want 0", n)
	}
}

func TestErrorMessagesAreUIKeysNotProse(t *testing.T) {
	// Error copy lives in the frontend. A server that sent prose would put
	// Vietnamese strings in two places, and an internal error string would
	// leak server detail to anyone holding a socket.
	m := errorMsg("room_not_found").GetError()
	if strings.ContainsAny(m.GetCode(), " .") {
		t.Errorf("error code %q looks like prose", m.GetCode())
	}
	if m.GetMessage() != m.GetCode() {
		t.Errorf("message %q diverged from code %q", m.GetMessage(), m.GetCode())
	}
}

// Fail the build, not a test, if the helper drifts from what the server needs.
var _ Dictionary = (*testDict)(nil)

// TestGoroutinesReturnToBaseline guards the leak the risk table names: a bot
// worker, a room, or a session that outlives its game costs a goroutine per
// abandoned match, which only shows up under sustained play.
func TestGoroutinesReturnToBaseline(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{TurnLimit: 5 * time.Second})

	// Warm up first: the HTTP server and the dialer start pools of their own,
	// and counting those as a leak would make this test lie.
	playOneBotGame(t, url)
	settle()
	baseline := runtime.NumGoroutine()

	for range 10 {
		playOneBotGame(t, url)
	}
	settle()

	// A little slack for the transport's own bookkeeping; a real leak here is
	// one goroutine per game, so ten games would show ten or more.
	if got := runtime.NumGoroutine(); got > baseline+5 {
		t.Errorf("goroutines grew from %d to %d over 10 games", baseline, got)
	}
}

func playOneBotGame(t *testing.T, url string) {
	t.Helper()
	c := dial(t, url)
	c.hello("Người thử")
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
		StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
	}})
	started := c.await("game_started").GetGameStarted()
	c.submit("b c", started.GetTurnSeq())
	c.await("turn_update")
	botReply := c.await("turn_update").GetTurnUpdate()
	c.submit("d e", botReply.GetTurnSeq())
	c.await("game_over")
	_ = c.conn.Close(websocket.StatusNormalClosure, "")
}

// settle waits for teardown to finish. Rooms and sessions end asynchronously,
// so sampling immediately would count goroutines that are already on their way
// out.
func settle() {
	for range 20 {
		runtime.Gosched()
		time.Sleep(25 * time.Millisecond)
	}
	runtime.GC()
}

// --- the lobby -------------------------------------------------------------

// pvpRoom seats two players and plays them into a game, so a test about what
// happens next does not restate the whole handshake.
func pvpRoom(t *testing.T, url string) (host, guest *testClient, start *noituv1.GameStarted) {
	t.Helper()

	host, guest, _ = pvpLobby(t, url)
	guest.setReady(true)
	host.await("room_state")
	host.startGame()

	start = host.await("game_started").GetGameStarted()
	guest.await("game_started")
	return host, guest, start
}

// pvpLobby seats two players and stops there: the room exists, nobody is ready
// and no game has been started.
func pvpLobby(t *testing.T, url string) (host, guest *testClient, code string) {
	t.Helper()

	host = dial(t, url)
	host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	code = host.await("room_state").GetRoomState().GetRoomCode()

	guest = dial(t, url)
	guest.hello("Khách")
	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	host.await("room_state")
	guest.await("room_state")
	return host, guest, code
}

// readyAndStart takes a seated pair from their lobby into a game, which is
// what every test that is about the game itself needs to get past.
func readyAndStart(t *testing.T, host, guest *testClient) {
	t.Helper()
	host.await("room_state")
	guest.await("room_state")
	guest.setReady(true)
	host.await("room_state")
	host.startGame()
}

func (c *testClient) setReady(ready bool) {
	c.t.Helper()
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_SetReady{
		SetReady: &noituv1.SetReady{Ready: ready},
	}})
}

func (c *testClient) startGame() {
	c.t.Helper()
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartGame{StartGame: &noituv1.StartGame{}}})
}

func (c *testClient) kickPlayer() {
	c.t.Helper()
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_KickPlayer{KickPlayer: &noituv1.KickPlayer{}}})
}

func (c *testClient) leaveRoom() {
	c.t.Helper()
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_LeaveRoom{LeaveRoom: &noituv1.LeaveRoom{}}})
}

// resignAndSettle ends the game and returns the lobby each player lands back
// in.
func resignAndSettle(t *testing.T, host, guest *testClient) (hostState, guestState *noituv1.RoomState) {
	t.Helper()
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Resign{Resign: &noituv1.Resign{}}})
	host.await("game_over")
	guest.await("game_over")
	return host.await("room_state").GetRoomState(),
		guest.await("room_state").GetRoomState()
}

// TestLobbyOpensWithNobodyReady is the room a joiner lands in: it exists, the
// owner is known, and nothing has started.
func TestLobbyOpensWithNobodyReady(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})

	// Set up by hand rather than through pvpLobby: this test is about the
	// frames the handshake produces, and the helper consumes them.
	host := dial(t, url)
	host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	code := host.await("room_state").GetRoomState().GetRoomCode()

	guest := dial(t, url)
	guest.hello("Khách")
	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})

	hostState := host.await("room_state").GetRoomState()
	guestState := guest.await("room_state").GetRoomState()

	if !hostState.GetIAmOwner() {
		t.Error("the player who created the room does not own it")
	}
	if guestState.GetIAmOwner() {
		t.Error("the player who joined was made owner")
	}
	if hostState.GetCanStart() || guestState.GetCanStart() {
		t.Error("a game can start with nobody ready")
	}
	if !hostState.GetOpponentPresent() || hostState.GetOpponentName() == "" {
		t.Errorf("the owner cannot see who joined: %+v", hostState)
	}
	// Nothing starts on its own. Joining used to be the start signal, and a
	// player who joined to look at the room found themselves on the clock.
	silentFor(t, guest, "game_started", 250*time.Millisecond)
}

// TestReadyIsRenderedPerRecipient is the guard against the two flags being
// swapped, which would show a player their opponent's readiness as their own.
func TestReadyIsRenderedPerRecipient(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, _ := pvpLobby(t, url)

	guest.setReady(true)

	guestState := guest.await("room_state").GetRoomState()
	hostState := host.await("room_state").GetRoomState()

	if !guestState.GetIAmReady() || guestState.GetOpponentReady() {
		t.Errorf("the guest should see only their own readiness, got %+v", guestState)
	}
	if hostState.GetIAmReady() || !hostState.GetOpponentReady() {
		t.Errorf("the owner should see only the guest's readiness, got %+v", hostState)
	}
	if !hostState.GetCanStart() {
		t.Error("the owner cannot start a game their guest is ready for")
	}
}

// TestOwnerHasNoReadinessOfTheirOwn: Start is the owner's readiness, and a
// second flag they would have to set first buys nothing.
func TestOwnerHasNoReadinessOfTheirOwn(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, _, _ := pvpLobby(t, url)

	host.setReady(true)

	if got := host.await("error").GetError().GetCode(); got != "owner_needs_no_ready" {
		t.Errorf("the owner readying returned %q", got)
	}
}

// TestStartIsRefusedUntilTheGuestIsReady covers each way a start is not yet a
// game.
func TestStartIsRefusedUntilTheGuestIsReady(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})

	host := dial(t, url)
	host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	code := host.await("room_state").GetRoomState().GetRoomCode()

	// Alone in the room.
	host.startGame()
	if got := host.await("error").GetError().GetCode(); got != "need_two_players" {
		t.Errorf("starting alone returned %q, want need_two_players", got)
	}

	guest := dial(t, url)
	guest.hello("Khách")
	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	host.await("room_state")
	guest.await("room_state")

	// Seated, but not ready.
	host.startGame()
	if got := host.await("error").GetError().GetCode(); got != "not_everyone_ready" {
		t.Errorf("starting with an unready guest returned %q, want not_everyone_ready", got)
	}

	// A guest who readies and takes it back is not ready either.
	guest.setReady(true)
	host.await("room_state")
	guest.setReady(false)
	host.await("room_state")
	host.startGame()
	if got := host.await("error").GetError().GetCode(); got != "not_everyone_ready" {
		t.Errorf("starting after the guest unreadied returned %q", got)
	}
}

// TestOnlyTheOwnerStartsAndKicks: the guest holds a room code, and a code is
// pasted into group chats by design.
func TestOnlyTheOwnerStartsAndKicks(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, _ := pvpLobby(t, url)
	_ = host

	guest.startGame()
	if got := guest.await("error").GetError().GetCode(); got != "not_the_owner" {
		t.Errorf("a guest starting the game returned %q, want not_the_owner", got)
	}
	guest.kickPlayer()
	if got := guest.await("error").GetError().GetCode(); got != "not_the_owner" {
		t.Errorf("a guest kicking returned %q, want not_the_owner", got)
	}
}

// TestNextGameNeedsAFreshReady is the whole replay flow: a finished game
// returns both players to the lobby, and the readiness that started the last
// one is spent.
func TestNextGameNeedsAFreshReady(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, first := pvpRoom(t, url)

	hostState, guestState := resignAndSettle(t, host, guest)
	if hostState.GetOpponentReady() || guestState.GetIAmReady() {
		t.Error("the readiness that started the last game survived it")
	}
	if hostState.GetCanStart() {
		t.Error("the owner can start a game nobody has readied for")
	}

	host.startGame()
	if got := host.await("error").GetError().GetCode(); got != "not_everyone_ready" {
		t.Errorf("starting a second game without a fresh ready returned %q", got)
	}

	guest.setReady(true)
	host.await("room_state")
	host.startGame()

	second := host.await("game_started").GetGameStarted()
	guest.await("game_started")

	if second.GetTurnSeq() <= first.GetTurnSeq() {
		t.Errorf("turn_seq must keep rising across games: %d then %d, so a submission "+
			"still in flight from the first could be applied to the second",
			first.GetTurnSeq(), second.GetTurnSeq())
	}
	if second.GetOpeningWord() == "" {
		t.Error("the second game needs its own opening word")
	}
}

// TestLobbyActionsAreRefusedDuringAGame keeps the lobby from being a way out
// of a game in progress.
func TestLobbyActionsAreRefusedDuringAGame(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, _ := pvpRoom(t, url)

	guest.setReady(false)
	if got := guest.await("error").GetError().GetCode(); got != "game_in_progress" {
		t.Errorf("unreadying mid-game returned %q, want game_in_progress", got)
	}
	host.kickPlayer()
	if got := host.await("error").GetError().GetCode(); got != "game_in_progress" {
		t.Errorf("kicking mid-game returned %q, want game_in_progress", got)
	}
	guest.leaveRoom()
	if got := guest.await("error").GetError().GetCode(); got != "game_in_progress" {
		t.Errorf("leaving mid-game returned %q, want game_in_progress", got)
	}
}

// TestLeavingNeedsAnUnreadyFirst is the friction the lobby is meant to have: a
// player the owner is waiting on has to take that back before walking away.
func TestLeavingNeedsAnUnreadyFirst(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, _ := pvpLobby(t, url)

	guest.setReady(true)
	guest.await("room_state")
	host.await("room_state")

	guest.leaveRoom()
	if got := guest.await("error").GetError().GetCode(); got != "must_unready_first" {
		t.Errorf("leaving while ready returned %q, want must_unready_first", got)
	}

	guest.setReady(false)
	guest.await("room_state")
	host.await("room_state")
	guest.leaveRoom()

	// The room survives: the owner is still in it, now on their own.
	alone := host.await("room_state").GetRoomState()
	if alone.GetOpponentPresent() {
		t.Errorf("the owner still sees a guest who left: %+v", alone)
	}
	if !alone.GetIAmOwner() || alone.GetCanStart() {
		t.Errorf("the room the owner is left with is wrong: %+v", alone)
	}
}

// TestKickFreesAnUnreadySeatOnly: readiness is a commitment, and the owner
// does not get to overrule one.
func TestKickFreesAnUnreadySeatOnly(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, code := pvpLobby(t, url)

	guest.setReady(true)
	host.await("room_state")
	host.kickPlayer()
	if got := host.await("error").GetError().GetCode(); got != "player_is_ready" {
		t.Errorf("kicking a ready guest returned %q, want player_is_ready", got)
	}

	guest.setReady(false)
	host.await("room_state")
	host.kickPlayer()

	if got := guest.await("error").GetError().GetCode(); got != "kicked" {
		t.Errorf("the kicked player was told %q", got)
	}
	if got := host.await("room_state").GetRoomState(); got.GetOpponentPresent() {
		t.Errorf("the kicked seat is still occupied: %+v", got)
	}

	// The seat is free, and a kick is not a ban.
	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	if got := guest.await("room_state").GetRoomState(); !got.GetOpponentPresent() {
		t.Errorf("a kicked player could not come back: %+v", got)
	}
}

// TestOwnerLeavingPromotesTheOtherPlayer: the role outlives the player who
// held it, or the room would be one nobody can start.
func TestOwnerLeavingPromotesTheOtherPlayer(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, code := pvpLobby(t, url)

	host.leaveRoom()

	promoted := guest.await("room_state").GetRoomState()
	if !promoted.GetIAmOwner() {
		t.Errorf("the player left behind was not promoted: %+v", promoted)
	}
	if promoted.GetOpponentPresent() {
		t.Errorf("the owner who left is still shown as present: %+v", promoted)
	}

	// And the promotion is real: the new owner can start a game with the next
	// person to walk in.
	third := dial(t, url)
	third.hello("Người mới")
	third.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	guest.await("room_state")
	third.await("room_state")
	third.setReady(true)
	guest.await("room_state")
	guest.startGame()

	guest.await("game_started")
	third.await("game_started")
}

// TestPromotedOwnerLosesTheirReadiness: their readiness is Start now, and a
// flag left set from being a guest would mean nothing.
func TestPromotedOwnerLosesTheirReadiness(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, _ := pvpLobby(t, url)

	guest.setReady(true)
	host.await("room_state")
	// Drained on both sides, so the state read below is the promotion and not
	// the readiness that preceded it.
	guest.await("room_state")
	host.leaveRoom()

	promoted := guest.await("room_state").GetRoomState()
	if promoted.GetIAmReady() {
		t.Errorf("the promoted owner is still carrying a guest's readiness: %+v", promoted)
	}
}

// TestLastPlayerOutClosesTheRoom bounds the code and the goroutine: nothing is
// coming that could fill a room whose code the hub is about to forget.
func TestLastPlayerOutClosesTheRoom(t *testing.T) {
	api, url := newTestServer(t, chainDict(), Config{})
	host, guest, _ := pvpLobby(t, url)

	guest.leaveRoom()
	host.await("room_state")
	host.leaveRoom()

	awaitNoRooms(t, api, "a room nobody is in")
}

// TestIdleLobbyCloses bounds a room nobody starts a game in. One open tab
// would otherwise hold a code and a goroutine for the life of the process.
func TestIdleLobbyCloses(t *testing.T) {
	api, url := newTestServer(t, chainDict(), Config{IdleFor: 150 * time.Millisecond})

	host := dial(t, url)
	host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	host.await("room_state")

	if got := host.await("error").GetError().GetCode(); got != "room_idle_closed" {
		t.Errorf("an idle room closed with %q", got)
	}
	awaitNoRooms(t, api, "an idle room")
}

// TestBotRoomHasNoLobby: a bot room is its game. Keeping it open would leave
// one goroutine and one engine per finished bot game.
func TestBotRoomHasNoLobby(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})

	c := dial(t, url)
	c.hello("Người chơi")
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
		StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
	}})
	c.await("game_started")

	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Resign{Resign: &noituv1.Resign{}}})
	c.await("game_over")

	// not_in_a_room is the session reporting that the room has gone: the
	// goroutine and engine are released rather than parked in a lobby no bot
	// can ready for.
	c.setReady(true)
	if got := c.await("error").GetError().GetCode(); got != "not_in_a_room" {
		t.Errorf("a finished bot room answered %q, want it to be gone", got)
	}
}

// awaitNoRooms waits for the hub to forget every room it holds.
func awaitNoRooms(t *testing.T, api *Server, what string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		api.hub.mu.Lock()
		left := len(api.hub.rooms)
		api.hub.mu.Unlock()

		if left == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s was never evicted from the hub", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestResumeReclaimsALobbySeat: a room outlives its games now, so a player who
// refreshes between them has a lobby to come back to rather than a refusal.
func TestResumeReclaimsALobbySeat(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})

	host := dial(t, url)
	welcome := host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	code := host.await("room_state").GetRoomState().GetRoomCode()

	guest := dial(t, url)
	guest.hello("Khách")
	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	host.await("room_state")
	guest.await("room_state")

	// The owner's tab reloads: same token, new socket.
	_ = host.conn.Close(websocket.StatusAbnormalClosure, "")
	guest.await("room_state")

	second := dial(t, url)
	second.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Hello{Hello: &noituv1.Hello{
		ProtocolVersion: ProtocolVersion,
		Nickname:        "Chủ phòng",
		ResumeToken:     welcome.GetResumeToken(),
	}}})
	second.await("welcome")

	back := second.await("room_state").GetRoomState()
	if !back.GetIAmOwner() {
		t.Errorf("the owner came back as a guest: %+v", back)
	}
	if !back.GetOpponentPresent() || back.GetRoomCode() != code {
		t.Errorf("the resumed lobby is not the one they left: %+v", back)
	}

	// And the room still works from both sides.
	guest.setReady(true)
	second.await("room_state")
	second.startGame()
	second.await("game_started")
	guest.await("game_started")
}

// TestOneConnectionCannotStrandRooms is the regression for rooms that outlived
// the only connection that could ever end them. attach overwrote the session's
// room pointer and nothing told the old room, so it parked in select forever
// holding a goroutine and a room code.
func TestOneConnectionCannotStrandRooms(t *testing.T) {
	api, url := newTestServer(t, chainDict(), Config{})

	c := dial(t, url)
	c.hello("Người chơi")

	const rooms = 4
	for range rooms {
		c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
		c.await("room_state")
	}

	_ = c.conn.Close(websocket.StatusNormalClosure, "")

	deadline := time.Now().Add(5 * time.Second)
	for {
		api.hub.mu.Lock()
		left := len(api.hub.rooms)
		api.hub.mu.Unlock()

		if left == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d rooms outlived the only connection that was ever in them", left, rooms)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestLobbyActionsAreRateLimited: every accepted action is broadcast to both
// seats, so an unbounded one lets a player fill the opponent's outbox until
// the server closes their session for falling behind.
func TestLobbyActionsAreRateLimited(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	_, guest, _ := pvpLobby(t, url)

	for i := range submitBurst + 5 {
		guest.setReady(i%2 == 0)
	}

	// The limiter answers before the room does, so a refusal has to appear in
	// the stream rather than an unbroken run of room states.
	for range 30 {
		if payloadCase(guest.recv()) == "error" {
			return
		}
	}
	t.Error("a burst of lobby actions was never refused")
}
