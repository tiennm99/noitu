package wsapi

import (
	"testing"
	"time"

	"github.com/coder/websocket"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
)

// definedChainDict is the forced chain with a meaning on the opening word and
// on the human's first reply, and none on the rest: both shapes must cross
// the wire, because the client shows a panel for every word.
func definedChainDict() *testDict {
	return chainDict().
		define("a b", "danh từ", "Từ mở đầu.").
		define("b c", "động từ", "Nối tiếp.").
		define("b c", "", "Nghĩa thứ hai.")
}

func assertSenses(t *testing.T, got []*noituv1.Sense, want ...[2]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d senses %v, want %d", len(got), got, len(want))
	}
	for i, w := range want {
		if got[i].GetPos() != w[0] || got[i].GetGloss() != w[1] {
			t.Errorf("sense %d = (%q, %q), want (%q, %q)", i, got[i].GetPos(), got[i].GetGloss(), w[0], w[1])
		}
	}
}

// TestBotGameCarriesMeanings checks the two messages that introduce a word to
// the client: the opening in GameStarted and each move in TurnUpdate, for a
// word with senses and for one without.
func TestBotGameCarriesMeanings(t *testing.T) {
	_, url := newTestServer(t, definedChainDict(), Config{})
	c := dial(t, url)
	c.hello("Ăn")

	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
		StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
	}})
	started := c.await("game_started").GetGameStarted()
	assertSenses(t, started.GetOpeningMeanings(), [2]string{"danh từ", "Từ mở đầu."})

	c.submit("b c", started.GetTurnSeq())
	own := c.await("turn_update").GetTurnUpdate()
	if !own.GetPlayed().GetByMe() {
		t.Fatal("first update should be the player's own move")
	}
	assertSenses(t, own.GetPlayed().GetMeanings(), [2]string{"động từ", "Nối tiếp."}, [2]string{"", "Nghĩa thứ hai."})

	// The bot's only move, "c d", has no definition: an empty list, not a
	// missing message.
	reply := c.await("turn_update").GetTurnUpdate()
	if reply.GetPlayed().GetWord() != "c d" {
		t.Fatalf("bot played %q, want c d", reply.GetPlayed().GetWord())
	}
	if n := len(reply.GetPlayed().GetMeanings()); n != 0 {
		t.Errorf("a word without a definition carried %d senses", n)
	}
}

// TestResumeReplaysMeanings drops a player after a move and checks that the
// replayed opening and last move both carry their senses, with no code path
// of their own.
func TestResumeReplaysMeanings(t *testing.T) {
	_, url := newTestServer(t, definedChainDict(), Config{TurnLimit: 10 * time.Second, GraceFor: 5 * time.Second})

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
	hostStart := host.await("game_started").GetGameStarted()
	guest.await("game_started")

	// Whoever was dealt the turn plays the only legal word.
	lead := guest
	if hostStart.GetMyTurn() {
		lead = host
	}
	lead.submit("b c", hostStart.GetTurnSeq())
	host.await("turn_update")
	guest.await("turn_update")

	_ = host.conn.Close(websocket.StatusGoingAway, "")
	guest.await("room_state")

	back := dial(t, url)
	back.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Hello{Hello: &noituv1.Hello{
		ProtocolVersion: ProtocolVersion,
		Nickname:        "Chủ phòng",
		ResumeToken:     welcome.GetResumeToken(),
	}}})
	back.await("welcome")

	restored := back.await("game_started").GetGameStarted()
	assertSenses(t, restored.GetOpeningMeanings(), [2]string{"danh từ", "Từ mở đầu."})
	replayed := back.await("turn_update").GetTurnUpdate()
	if replayed.GetPlayed().GetWord() != "b c" {
		t.Fatalf("replayed move = %q, want b c", replayed.GetPlayed().GetWord())
	}
	assertSenses(t, replayed.GetPlayed().GetMeanings(), [2]string{"động từ", "Nối tiếp."}, [2]string{"", "Nghĩa thứ hai."})
}
