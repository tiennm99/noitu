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
	"unicode/utf8"

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

// mySlot is the recipient's own row in a RoomState, which is the only place
// their role and their readiness live now.
func mySlot(state *noituv1.RoomState) *noituv1.PlayerSlot {
	for _, p := range state.GetPlayers() {
		if p.GetIsMe() {
			return p
		}
	}
	return nil
}

// otherSlot is the one other player in a two-player room, or nil when the
// recipient is alone in it. Most of the lobby tests below are about two
// people, so this is the shape they read the list in.
func otherSlot(state *noituv1.RoomState) *noituv1.PlayerSlot {
	for _, p := range state.GetPlayers() {
		if !p.GetIsMe() {
			return p
		}
	}
	return nil
}

// slotFor finds one named seat, for the tests that seat more than two.
func slotFor(state *noituv1.RoomState, id string) *noituv1.PlayerSlot {
	for _, p := range state.GetPlayers() {
		if p.GetPlayerId() == id {
			return p
		}
	}
	return nil
}

// myScore and otherScore read a two-player turn update the way it used to
// carry the numbers, out of the table that replaced the two fields.
func myScore(u *noituv1.TurnUpdate) uint32 {
	for _, p := range u.GetPlayers() {
		if p.GetIsMe() {
			return p.GetScore()
		}
	}
	return 0
}

func otherScore(u *noituv1.TurnUpdate) uint32 {
	for _, p := range u.GetPlayers() {
		if !p.GetIsMe() {
			return p.GetScore()
		}
	}
	return 0
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
	case *noituv1.ServerMessage_PlayerEliminated:
		return "player_eliminated"
	case *noituv1.ServerMessage_Error:
		return "error"
	case *noituv1.ServerMessage_Pong:
		return "pong"
	case *noituv1.ServerMessage_RoomState:
		return "room_state"
	case *noituv1.ServerMessage_ChatMessage:
		return "chat_message"
	case *noituv1.ServerMessage_ChatHistory:
		return "chat_history"
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
	if myScore(hostUpdate) != otherScore(guestUpdate) {
		t.Errorf("scores disagree across recipients: %d vs %d",
			myScore(hostUpdate), otherScore(guestUpdate))
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

	// Presence is part of the room's state now, so the seat being held is
	// something the other player reads there rather than in a message of its
	// own.
	if away := otherSlot(guest.await("room_state").GetRoomState()); away == nil || away.GetConnected() {
		t.Error("opponent should be shown as away while the seat is held")
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
// creates — players who all send nothing — and the one it always could:
// players who choose the same name.
func TestDistinguishSeparatesIdenticalNames(t *testing.T) {
	if got := distinguish("Minh", []string{"Thuý"}); got != "Minh" {
		t.Errorf("distinct names should be left alone, got %q", got)
	}

	// A whole room of unnamed players, seated one at a time. Every one of them
	// has to end up with a name none of the others is already using: two
	// suffixed identically is the same failure as two unsuffixed.
	var taken []string
	for range maxPlayers {
		got := distinguish(defaultNickname, taken)
		if slices.Contains(taken, got) {
			t.Fatalf("distinguish returned %q, which is already in %v", got, taken)
		}
		if n := len([]rune(got)); n > maxNicknameRunes {
			t.Errorf("distinguished name %q is %d runes, over the %d cap", got, n, maxNicknameRunes)
		}
		taken = append(taken, got)
	}

	// The suffix must not push a name that is already at the cap past it.
	long := strings.Repeat("a", maxNicknameRunes)
	if got := distinguish(long, []string{long}); len([]rune(got)) > maxNicknameRunes {
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

func (c *testClient) kickPlayer(target string) {
	c.t.Helper()
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_KickPlayer{
		KickPlayer: &noituv1.KickPlayer{PlayerId: target},
	}})
}

func (c *testClient) say(text string) {
	c.t.Helper()
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_SendChat{
		SendChat: &noituv1.SendChat{Text: text},
	}})
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

	if !mySlot(hostState).GetIsOwner() {
		t.Error("the player who created the room does not own it")
	}
	if mySlot(guestState).GetIsOwner() {
		t.Error("the player who joined was made owner")
	}
	if hostState.GetCanStart() || guestState.GetCanStart() {
		t.Error("a game can start with nobody ready")
	}
	if otherSlot(hostState) == nil || otherSlot(hostState).GetName() == "" {
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

	if !mySlot(guestState).GetReady() || otherSlot(guestState).GetReady() {
		t.Errorf("the guest should see only their own readiness, got %+v", guestState)
	}
	if mySlot(hostState).GetReady() || !otherSlot(hostState).GetReady() {
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
	if got := host.await("error").GetError().GetCode(); got != "need_more_players" {
		t.Errorf("starting alone returned %q, want need_more_players", got)
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
	guest.kickPlayer("p1")
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
	if otherSlot(hostState).GetReady() || mySlot(guestState).GetReady() {
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

// TestRoomKeepsARunningWinTally: a room outlives its games, so the score of
// the series is a room fact. It is credited before the lobby is broadcast, so
// the players see it the moment a game ends rather than one input later.
func TestRoomKeepsARunningWinTally(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, _ := pvpRoom(t, url)

	// The owner resigns, so the guest takes the first game.
	hostState, guestState := resignAndSettle(t, host, guest)
	if got := mySlot(guestState).GetWins(); got != 1 {
		t.Errorf("the winner's tally is %d after one game, want 1", got)
	}
	if got := mySlot(hostState).GetWins(); got != 0 {
		t.Errorf("the loser's tally is %d, want 0", got)
	}
	// And each player is told the whole table, not just their own row.
	if got := otherSlot(hostState).GetWins(); got != 1 {
		t.Errorf("the owner sees the guest's tally as %d, want 1", got)
	}

	// A second game the other way round leaves the series level.
	guest.setReady(true)
	host.await("room_state")
	host.startGame()
	host.await("game_started")
	guest.await("game_started")

	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Resign{Resign: &noituv1.Resign{}}})
	host.await("game_over")
	guest.await("game_over")
	hostState = host.await("room_state").GetRoomState()

	if got := mySlot(hostState).GetWins(); got != 1 {
		t.Errorf("the owner's tally is %d after winning one of two, want 1", got)
	}
	if got := otherSlot(hostState).GetWins(); got != 1 {
		t.Errorf("the guest's tally is %d after winning one of two, want 1", got)
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
	host.kickPlayer("p2")
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
	if otherSlot(alone) != nil {
		t.Errorf("the owner still sees a guest who left: %+v", alone)
	}
	if !mySlot(alone).GetIsOwner() || alone.GetCanStart() {
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
	host.kickPlayer("p2")
	if got := host.await("error").GetError().GetCode(); got != "player_is_ready" {
		t.Errorf("kicking a ready guest returned %q, want player_is_ready", got)
	}

	guest.setReady(false)
	host.await("room_state")
	host.kickPlayer("p2")

	if got := guest.await("error").GetError().GetCode(); got != "kicked" {
		t.Errorf("the kicked player was told %q", got)
	}
	if got := host.await("room_state").GetRoomState(); otherSlot(got) != nil {
		t.Errorf("the kicked seat is still occupied: %+v", got)
	}

	// The seat is free, and a kick is not a ban.
	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})
	if got := guest.await("room_state").GetRoomState(); otherSlot(got) == nil {
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
	if !mySlot(promoted).GetIsOwner() {
		t.Errorf("the player left behind was not promoted: %+v", promoted)
	}
	if otherSlot(promoted) != nil {
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
	if mySlot(promoted).GetReady() {
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

// --- chat -------------------------------------------------------------------

// resumeAs reconnects with a live token and returns the new connection, having
// consumed its Welcome. Reading a room's conversation back is what it is for:
// a seat's own resume is the only replay that carries the whole window.
func resumeAs(t *testing.T, url, nickname, token string) *testClient {
	t.Helper()

	c := dial(t, url)
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Hello{Hello: &noituv1.Hello{
		ProtocolVersion: ProtocolVersion,
		Nickname:        nickname,
		ResumeToken:     token,
	}}})
	c.await("welcome")
	return c
}

// TestChatReachesBothSeatsRenderedPerRecipient is the guard against from_me
// being computed once and shared, which would show a player their opponent's
// words as their own.
func TestChatReachesBothSeatsRenderedPerRecipient(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, _ := pvpLobby(t, url)

	host.say("Chào bạn")

	mine := host.await("chat_message").GetChatMessage()
	theirs := guest.await("chat_message").GetChatMessage()

	if !mine.GetFromMe() {
		t.Errorf("the sender was not shown their own message as theirs: %+v", mine)
	}
	if theirs.GetFromMe() {
		t.Errorf("the recipient was shown the sender's message as their own: %+v", theirs)
	}
	if theirs.GetText() != "Chào bạn" || theirs.GetAuthor() == "" {
		t.Errorf("the recipient's copy is wrong: %+v", theirs)
	}
	if theirs.GetSentUnixMs() == 0 {
		t.Error("a message carries no time")
	}
	// The seat behind a line is the same fact for everybody: from_me is the
	// only field that is relative to the reader, and a client colours a line
	// by its author rather than by matching names.
	if mine.GetPlayerId() != theirs.GetPlayerId() || theirs.GetPlayerId() == "" {
		t.Errorf("the author's seat differs between recipients: %q and %q",
			mine.GetPlayerId(), theirs.GetPlayerId())
	}
}

// TestChatWorksInTheLobbyAndInAGame: the conversation belongs to the room, not
// to a game, so neither phase is a special case.
func TestChatWorksInTheLobbyAndInAGame(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, _ := pvpLobby(t, url)

	host.say("trước ván")
	if got := guest.await("chat_message").GetChatMessage().GetText(); got != "trước ván" {
		t.Errorf("lobby message = %q", got)
	}

	// Started inline rather than through readyAndStart: pvpLobby has already
	// drained the room states that helper waits for.
	guest.setReady(true)
	host.await("room_state")
	host.startGame()
	host.await("game_started")
	guest.await("game_started")

	host.say("trong ván")
	if got := guest.await("chat_message").GetChatMessage().GetText(); got != "trong ván" {
		t.Errorf("in-game message = %q", got)
	}
}

// TestAJoinerSeesNothingSaidBeforeTheySatDown is the disclosure boundary. A
// room code is pasted into group chats by design, so redeeming one must not
// hand over a conversation the joiner was never part of.
func TestAJoinerSeesNothingSaidBeforeTheySatDown(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})

	host := dial(t, url)
	host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	code := host.await("room_state").GetRoomState().GetRoomCode()

	host.say("bí mật")
	host.await("chat_message")

	guest := dial(t, url)
	guest.hello("Khách")
	guest.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: code},
	}})

	if got := guest.await("chat_history").GetChatHistory().GetMessages(); len(got) != 0 {
		t.Errorf("a joiner was handed %d messages from before they arrived: %+v", len(got), got)
	}

	// And from there they share a conversation like anybody else.
	host.say("chào")
	if got := guest.await("chat_message").GetChatMessage().GetText(); got != "chào" {
		t.Errorf("message after joining = %q", got)
	}
}

// TestCreatingARoomReplaysAnEmptyHistory: it is what overwrites the panel a
// client may still be holding from the room it was in before this one.
func TestCreatingARoomReplaysAnEmptyHistory(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})

	c := dial(t, url)
	c.hello("Chủ phòng")
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})

	if got := c.await("chat_history").GetChatHistory().GetMessages(); len(got) != 0 {
		t.Errorf("a new room came with %d messages", len(got))
	}
}

// TestChatHistoryIsReplayedOnResumeInTheLobby is the commonest refresh there
// is, and the one a replay hung off the end of handleResume would miss: that
// function returns early for a lobby resume.
func TestChatHistoryIsReplayedOnResumeInTheLobby(t *testing.T) {
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

	host.say("một")
	host.await("chat_message")
	guest.say("hai")
	host.await("chat_message")

	// The tab reloads: same token, new socket, still in the lobby.
	_ = host.conn.Close(websocket.StatusAbnormalClosure, "")
	back := resumeAs(t, url, "Chủ phòng", welcome.GetResumeToken())

	history := back.await("chat_history").GetChatHistory().GetMessages()
	if len(history) != 2 {
		t.Fatalf("the resumed lobby replayed %d messages, want 2: %+v", len(history), history)
	}
	if history[0].GetText() != "một" || history[1].GetText() != "hai" {
		t.Errorf("history is out of order: %+v", history)
	}
	if !history[0].GetFromMe() || history[1].GetFromMe() {
		t.Errorf("authorship did not survive the resume: %+v", history)
	}
}

// TestChatHistoryIsReplayedOnResumeMidGame covers the same during a game,
// where handleResume takes its other path.
func TestChatHistoryIsReplayedOnResumeMidGame(t *testing.T) {
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
	readyAndStart(t, host, guest)
	host.await("game_started")
	guest.await("game_started")

	host.say("đang chơi")
	host.await("chat_message")

	_ = host.conn.Close(websocket.StatusAbnormalClosure, "")
	back := resumeAs(t, url, "Chủ phòng", welcome.GetResumeToken())

	if got := back.await("chat_history").GetChatHistory().GetMessages(); len(got) != 1 {
		t.Errorf("a mid-game resume replayed %d messages, want 1", len(got))
	}
}

// TestChatHistoryIsCappedAndOrdered bounds what one room holds. Driven at the
// room directly: the cap is room state, and a socket would only add a rate
// limiter to wait out.
func TestChatHistoryIsCappedAndOrdered(t *testing.T) {
	sess := offlineSession(t, chatHistoryLimit+64)
	r := &room{
		code:  "TESTRM",
		seats: [maxPlayers]*seat{{id: "p1", nickname: "Chủ phòng", sess: sess}},
		owner: "p1",
	}

	const sent = chatHistoryLimit + 5
	for i := range sent {
		r.handleChat(chatInput{sess: sess, player: "p1", text: fmt.Sprintf("tin %d", i)})
	}

	if len(r.chat) != chatHistoryLimit {
		t.Fatalf("the room kept %d messages, want %d", len(r.chat), chatHistoryLimit)
	}
	if got, want := r.chat[0].text, fmt.Sprintf("tin %d", sent-chatHistoryLimit); got != want {
		t.Errorf("oldest kept message = %q, want %q", got, want)
	}
	if got, want := r.chat[len(r.chat)-1].text, fmt.Sprintf("tin %d", sent-1); got != want {
		t.Errorf("newest kept message = %q, want %q", got, want)
	}
	// The sequence keeps rising past the trim, which is what makes a seat's
	// watermark meaningful after the entry it pointed at is gone.
	if r.chatSeq != sent {
		t.Errorf("chatSeq = %d after %d messages", r.chatSeq, sent)
	}
}

// TestChatHistorySurvivesAGame: the conversation is the room's, and a game
// starting and finishing inside it changes nothing about that.
func TestChatHistorySurvivesAGame(t *testing.T) {
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

	host.say("trước ván")
	host.await("chat_message")

	guest.setReady(true)
	host.await("room_state")
	host.startGame()
	host.await("game_started")
	guest.await("game_started")
	resignAndSettle(t, host, guest)

	_ = host.conn.Close(websocket.StatusAbnormalClosure, "")
	back := resumeAs(t, url, "Chủ phòng", welcome.GetResumeToken())

	if got := back.await("chat_history").GetChatHistory().GetMessages(); len(got) != 1 {
		t.Errorf("a finished game left %d messages, want the 1 said before it", len(got))
	}
}

// TestChatTextIsSanitizedAndCapped covers the three ways text is made safe to
// render in a stranger's browser.
func TestChatTextIsSanitizedAndCapped(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, _ := pvpLobby(t, url)

	// Invisible characters: a zero-width joiner and a bidi override.
	host.say("xin" + string(zeroWidthSpace) + "chào" + string(bidiOverride))
	if got := guest.await("chat_message").GetChatMessage().GetText(); got != "xinchào" {
		t.Errorf("invisible characters survived: %q", got)
	}

	// A stack of combining marks renders as a glyph cluster tall enough to
	// cover the board, and the rune cap alone does not stop it.
	host.say("a" + strings.Repeat("\u0350", 199))
	stacked := guest.await("chat_message").GetChatMessage().GetText()
	if marks := strings.Count(stacked, "\u0350"); marks > maxChatMarks {
		t.Errorf("a %d-mark stack survived, want at most %d", marks, maxChatMarks)
	}

	// Over the cap, cut on a rune boundary rather than through a character.
	host.say(strings.Repeat("ữ", maxChatRunes+100))
	long := guest.await("chat_message").GetChatMessage().GetText()
	if runes := []rune(long); len(runes) != maxChatRunes {
		t.Errorf("a long message came back %d runes, want %d", len(runes), maxChatRunes)
	}
	if !utf8.ValidString(long) {
		t.Error("the cap cut a character in half")
	}
}

// TestEmptyChatIsDroppedWithoutAnError: nothing survived the sanitizer, so
// there is no message to refuse and nobody who typed one.
func TestEmptyChatIsDroppedWithoutAnError(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, _ := pvpLobby(t, url)

	// Sent first, then a real one. Messages arrive in order, so a guest whose
	// next message is the real one never received the empty one — and unlike
	// silentFor, this leaves the connection alive to prove it.
	host.say("   " + string(zeroWidthSpace) + "  ")
	host.say("thật")

	if got := guest.await("chat_message").GetChatMessage().GetText(); got != "thật" {
		t.Errorf("the empty message was delivered as %q", got)
	}
	// And nothing follows it.
	silentFor(t, guest, "chat_message", 250*time.Millisecond)
}

// TestChatIsRateLimitedOnItsOwnBudget: a burst is refused, and it does not
// cost the sender their moves.
func TestChatIsRateLimitedOnItsOwnBudget(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, start := pvpRoom(t, url)

	// The player on turn does the talking, so the move that follows is one
	// they are allowed to make.
	mover, other := host, guest
	if !start.GetMyTurn() {
		mover, other = guest, host
	}

	for range chatBurst + 5 {
		mover.say("spam")
	}

	var refused bool
	for range 40 {
		if payloadCase(mover.recv()) == "error" {
			refused = true
			break
		}
	}
	if !refused {
		t.Fatal("a burst of chat was never refused")
	}

	// The move budget is separate, so a word still reaches the engine: the
	// opponent seeing the turn arrive is the proof it was accepted.
	mover.submit("b c", start.GetTurnSeq())
	awaitMyTurn(t, other)
}

// TestChatFromASeatlessConnectionIsRefused: losing the seat is not losing the
// socket. A kicked player's connection is still open and still believes it was
// in a room, which is the reachable half of the guard that also covers a
// connection replaced by a reconnect.
func TestChatFromASeatlessConnectionIsRefused(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, _ := pvpLobby(t, url)

	host.kickPlayer("p2")
	if got := guest.await("error").GetError().GetCode(); got != "kicked" {
		t.Fatalf("the guest was told %q rather than being kicked", got)
	}

	guest.say("cho tôi vào lại")
	if got := guest.await("error").GetError().GetCode(); got != "not_in_a_room" {
		t.Errorf("a kicked connection chatting returned %q, want not_in_a_room", got)
	}
	silentFor(t, host, "chat_message", 250*time.Millisecond)
}

// TestChatNeedsASeat: holding a socket is not holding a seat.
func TestChatNeedsASeat(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})

	c := dial(t, url)
	c.hello("Người chơi")
	c.say("có ai không")

	if got := c.await("error").GetError().GetCode(); got != "not_in_a_room" {
		t.Errorf("chatting from no room returned %q, want not_in_a_room", got)
	}
}

// TestBotRoomHasNoChat: there is nobody to talk to, and the check belongs on
// the room goroutine, which is the only place that knows.
func TestBotRoomHasNoChat(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})

	c := dial(t, url)
	c.hello("Người chơi")
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
		StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
	}})
	c.await("game_started")

	c.say("chào máy")
	if got := c.await("error").GetError().GetCode(); got != "not_in_a_room" {
		t.Errorf("chatting at a bot returned %q, want not_in_a_room", got)
	}
}

// TestChatDoesNotKeepARoomAlive: talking is not playing. Without this a room
// is held open for the life of the process by one message every nine minutes.
func TestChatDoesNotKeepARoomAlive(t *testing.T) {
	api, url := newTestServer(t, chainDict(), Config{IdleFor: 300 * time.Millisecond})

	host := dial(t, url)
	host.hello("Chủ phòng")
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	host.await("room_state")

	// Chatting throughout the window; the clock must keep running anyway.
	for range 4 {
		host.say("vẫn ở đây")
		time.Sleep(100 * time.Millisecond)
	}

	if got := host.await("error").GetError().GetCode(); got != "room_idle_closed" {
		t.Errorf("a chatted-in room closed with %q, want room_idle_closed", got)
	}
	awaitNoRooms(t, api, "a room held open by chat")
}

// TestVacatedSeatKeepsItsWordsButLosesItsAuthor: the words stay, the
// attribution does not — checked from the side that stayed, because a name
// left behind is a name the next joiner can ask for.
func TestVacatedSeatKeepsItsWordsButLosesItsAuthor(t *testing.T) {
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

	guest.say("tôi là khách")
	host.await("chat_message")
	guest.leaveRoom()
	host.await("room_state")

	// The player who stayed reloads and reads the room back.
	_ = host.conn.Close(websocket.StatusAbnormalClosure, "")
	back := resumeAs(t, url, "Chủ phòng", welcome.GetResumeToken())

	history := back.await("chat_history").GetChatHistory().GetMessages()
	if len(history) != 1 {
		t.Fatalf("the departed player's words are gone: %+v", history)
	}
	if got := history[0]; got.GetText() != "tôi là khách" || got.GetAuthor() != "" || got.GetFromMe() {
		t.Errorf("a vacated seat's message is still attributed: %+v", got)
	}
	// The seat goes with the name. A line still carrying it would be coloured
	// as whoever fills that seat next.
	if got := history[0].GetPlayerId(); got != "" {
		t.Errorf("a vacated seat's message still names its seat: %q", got)
	}
}

// TestTheRemainingPlayerIsResyncedWhenASeatIsVacated is the live half of the
// authorship rule. Clearing the store is not enough: the player who stayed is
// already holding frames that carry the departed name, and nothing else in the
// protocol would correct them before a reload.
func TestTheRemainingPlayerIsResyncedWhenASeatIsVacated(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, _ := pvpLobby(t, url)

	guest.say("số của tôi là …")
	host.await("chat_message")

	guest.leaveRoom()

	// No reload, no resume: the correction has to arrive on its own.
	history := host.await("chat_history").GetChatHistory().GetMessages()
	if len(history) != 1 {
		t.Fatalf("the remaining player was resynced with %d messages, want 1", len(history))
	}
	if got := history[0]; got.GetAuthor() != "" || got.GetText() != "số của tôi là …" {
		t.Errorf("the departed player's line is still attributed: %+v", got)
	}
}

// offlineSession builds a session with no websocket behind it, for the two
// guarantees that live below the transport: who may speak for a seat, and what
// a full outbox costs the player behind it.
func offlineSession(t *testing.T, capacity int) *session {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &session{
		id:      randomToken(),
		ctx:     ctx,
		cancel:  cancel,
		out:     make(chan []byte, capacity),
		flushed: make(chan struct{}),
	}
}

// queued reads whatever the session has waiting for its writer, decoded.
// Deliberately not called drain: (*session).drain means the opposite — flush
// what is queued out to the socket.
func queued(t *testing.T, s *session) []*noituv1.ServerMessage {
	t.Helper()

	var out []*noituv1.ServerMessage
	for {
		select {
		case raw := <-s.out:
			var m noituv1.ServerMessage
			if err := proto.Unmarshal(raw, &m); err != nil {
				t.Fatalf("decode: %v", err)
			}
			out = append(out, &m)
		default:
			return out
		}
	}
}

// TestChatFromAConnectionThatLostItsSeatIsRefused covers handleChat's own
// authorization, which the transport cannot reach: a frame already queued when
// the seat was freed arrives after it, and by then the seat may be somebody
// else's. Driven directly because that ordering is a race over the wire.
func TestChatFromAConnectionThatLostItsSeatIsRefused(t *testing.T) {
	evicted := offlineSession(t, 8)
	seated := offlineSession(t, 8)

	r := &room{
		code:  "TESTRM",
		seats: [maxPlayers]*seat{{id: "p1", nickname: "Chủ phòng", sess: evicted}},
		owner: "p1",
	}

	// The seat changes hands while this connection's message is in flight —
	// a reconnect, or a kick and a new arrival.
	r.seats[0].sess = seated
	r.handleChat(chatInput{sess: evicted, player: "p1", text: "tôi vẫn ở đây"})

	refusals := queued(t, evicted)
	if len(refusals) != 1 || refusals[0].GetError().GetCode() != "not_your_seat" {
		t.Fatalf("a connection with no seat was answered %+v, want not_your_seat", refusals)
	}
	if len(r.chat) != 0 {
		t.Errorf("the room stored a message from a connection that holds no seat: %+v", r.chat)
	}
	if got := queued(t, seated); len(got) != 0 {
		t.Errorf("the seated player was sent %d frames from an impostor", len(got))
	}
}

// TestChatToAFullOutboxIsDroppedNotFatal is the guarantee the chat budget
// rests on: a player who cannot keep up loses a line, not their session. Every
// other frame closes a session at this point, which mid-game costs the game.
//
// Driven through handleChat rather than trySend directly, because the property
// under test is which delivery the chat path chooses — a test that called
// trySend itself would pass just as happily after somebody swapped the call
// site back to send.
func TestChatToAFullOutboxIsDroppedNotFatal(t *testing.T) {
	sender := offlineSession(t, 8)
	slow := offlineSession(t, 1)
	slow.out <- []byte("already queued")

	r := &room{
		code: "TESTRM",
		seats: [maxPlayers]*seat{
			{id: "p1", nickname: "Chủ phòng", sess: sender},
			{id: "p2", nickname: "Khách", sess: slow},
		},
		owner: "p1",
	}

	r.handleChat(chatInput{sess: sender, player: "p1", text: "bạn còn đó không"})

	select {
	case <-slow.ctx.Done():
		t.Fatal("a chat line closed the session of the player who could not keep up")
	default:
	}
	if len(r.chat) != 1 {
		t.Errorf("the room stored %d messages, want 1", len(r.chat))
	}
	// The sender is unaffected: their own copy went out and nothing was
	// refused.
	for _, m := range queued(t, sender) {
		if m.GetError() != nil {
			t.Errorf("the sender was refused: %q", m.GetError().GetCode())
		}
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
	if !mySlot(back).GetIsOwner() {
		t.Errorf("the owner came back as a guest: %+v", back)
	}
	if otherSlot(back) == nil || back.GetRoomCode() != code {
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
