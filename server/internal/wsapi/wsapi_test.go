package wsapi

import (
	"context"
	"fmt"
	"iter"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/coder/websocket"
	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/dictionary"
	"golang.org/x/text/unicode/norm"
	"google.golang.org/protobuf/proto"
)

// Shared test harness: a hand-built dictionary a reader can follow, a
// server wired to it over a real WebSocket, and the client-side helpers
// every topic file below drives a room through.

// testDict is a hand-built word graph.
//
// A dozen words with edges a reader can follow beats a real dictionary here:
// when a test says the bot must lose, the reason is visible in the graph
// rather than buried in tens of thousands of entries.
type testDict struct {
	// words maps a canonical word to its first and last syllable.
	words   map[string][2]string
	opening string
	// senses holds the meanings of the few words a test gives one to; every
	// other word has none, which is also a case the client must handle.
	senses map[string][]dictionary.Sense
}

func newTestDict(opening string, words ...string) *testDict {
	d := &testDict{words: map[string][2]string{}, opening: opening, senses: map[string][]dictionary.Sense{}}
	for _, w := range append(words, opening) {
		parts := strings.Fields(w)
		d.words[w] = [2]string{parts[0], parts[len(parts)-1]}
	}
	return d
}

// define gives a word one sense, so a test can see it arrive on the wire.
func (d *testDict) define(word, pos, gloss string) *testDict {
	d.senses[word] = append(d.senses[word], dictionary.Sense{Pos: pos, Gloss: gloss})
	return d
}

func (d *testDict) Meanings(word string) []dictionary.Sense { return d.senses[word] }

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

// NearMiss strips tone marks and the đ/d distinction by hand — the same
// typing-distance fold dictionary.stripDiacritics performs — since this test
// graph is built by hand rather than through the real Store.
func (d *testDict) NearMiss(normalized string) (string, bool) {
	strip := func(s string) string {
		s = norm.NFD.String(s)
		var b strings.Builder
		for _, r := range s {
			switch {
			case unicode.Is(unicode.Mn, r):
				continue
			case r == 'đ':
				r = 'd'
			}
			b.WriteRune(r)
		}
		return b.String()
	}

	key := strip(normalized)
	match, count := "", 0
	for w := range d.words {
		if w == normalized {
			continue
		}
		if strip(w) == key {
			match, count = w, count+1
		}
	}
	if count != 1 {
		return "", false
	}
	return match, true
}

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
	case *noituv1.ServerMessage_QuickMatchStatus:
		return "quick_match_status"
	case *noituv1.ServerMessage_WordReported:
		return "word_reported"
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

// Fail the build, not a test, if the helper drifts from what the server needs.
var _ Dictionary = (*testDict)(nil)

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

// awaitLead reads both game_started messages and sorts the pair into the one
// that drew the first turn and the one that waits.
//
// The lead is random per game, so a test that needs to play a move has to ask
// who may play it rather than assume the room's creator. It returns the
// leader's GameStarted for its turn_seq.
func awaitLead(t *testing.T, host, guest *testClient) (lead, waits *testClient, started *noituv1.GameStarted) {
	t.Helper()
	hostStart := host.await("game_started").GetGameStarted()
	guestStart := guest.await("game_started").GetGameStarted()
	if hostStart.GetMyTurn() == guestStart.GetMyTurn() {
		t.Fatal("exactly one player must be on turn")
	}
	if hostStart.GetMyTurn() {
		return host, guest, hostStart
	}
	return guest, host, guestStart
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

func (c *testClient) quickMatch() {
	c.t.Helper()
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_QuickMatch{QuickMatch: &noituv1.QuickMatch{}}})
}

func (c *testClient) cancelQuickMatch() {
	c.t.Helper()
	c.send(&noituv1.ClientMessage{
		Payload: &noituv1.ClientMessage_CancelQuickMatch{CancelQuickMatch: &noituv1.CancelQuickMatch{}},
	})
}

func (c *testClient) resign() {
	c.t.Helper()
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Resign{Resign: &noituv1.Resign{}}})
}

func (c *testClient) claimDeadEnd() {
	c.t.Helper()
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_ClaimDeadEnd{
		ClaimDeadEnd: &noituv1.ClaimDeadEnd{},
	}})
}

func (c *testClient) reportWord(word string) {
	c.t.Helper()
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_ReportWord{
		ReportWord: &noituv1.ReportWord{Word: word},
	}})
}

// resignFrom has one player of a two-player game give up, whichever of them
// drew the first turn.
//
// Only the player to act may give up, and who leads is drawn, so a test that
// needs a particular player to lose has to walk the turn to them first: the
// fixture graph is a forced path, so the other side has exactly one legal word
// and playing it hands the turn over.
//
// start is the loser's own GameStarted, which is where my_turn is rendered for
// them.
func resignFrom(t *testing.T, loser, other *testClient, start *noituv1.GameStarted) {
	t.Helper()
	if !start.GetMyTurn() {
		other.submit("b c", start.GetTurnSeq())
		loser.await("turn_update")
		other.await("turn_update")
	}
	loser.resign()
}

// resignAndSettle has the owner give up and returns the lobby each player
// lands back in. start is the owner's own GameStarted.
func resignAndSettle(t *testing.T, host, guest *testClient, start *noituv1.GameStarted) (hostState, guestState *noituv1.RoomState) {
	t.Helper()
	resignFrom(t, host, guest, start)
	host.await("game_over")
	guest.await("game_over")
	return host.await("room_state").GetRoomState(),
		guest.await("room_state").GetRoomState()
}

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
		// Generous rather than zero: most callers exercise a handler directly
		// and never touch a limiter, but one that does — handleReportWord — must
		// not panic on a nil bucket, and a test about something else has no
		// reason to also be a test of rate limiting.
		submitLimiter: newBucket(1000, 1000, time.Now()),
		roomLimiter:   newBucket(1000, 1000, time.Now()),
		chatLimiter:   newBucket(1000, 1000, time.Now()),
		frameLimiter:  newBucket(1000, 1000, time.Now()),
		reportedWords: make(map[string]struct{}),
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
