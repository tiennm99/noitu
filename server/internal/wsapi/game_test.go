package wsapi

import (
	"math/rand/v2"
	"runtime"
	"testing"
	"time"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/bot"
	"github.com/tiennm99dev/noitu/server/internal/game"
)

// Playing a game to completion: submissions, rejections, timeouts, and the
// goroutine accounting a finished one leaves behind.

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

// TestTheFirstTurnIsDrawn checks that opening the room is not the same as
// opening the game. Moving first is an advantage, and handing it to the owner
// every time would make them favourite in every game of a series.
func TestTheFirstTurnIsDrawn(t *testing.T) {
	// Started here rather than over a pair of sockets: a series long enough to
	// tell a draw from a fixed lead is far more starts than a lobby's rate
	// limiter allows, and none of what is being checked is on the wire.
	// beginGame reports to the hub's live-game gauge, so this hand-built room
	// needs one even though nothing here reads it back.
	r := &room{hub: &hub{}, dict: chainDict(), turnLimit: time.Second}
	r.seats[0] = &seat{id: "p1"}
	r.seats[1] = &seat{id: "p2"}

	// There are two outcomes, so a series that only ever shows one of them is
	// a lead that is not being drawn. A fixed lead fails this every time; a
	// fair draw fails it about once in five hundred million runs.
	const games = 30
	led := map[game.PlayerID]int{}
	for range games {
		if err := r.beginGame(); err != nil {
			t.Fatalf("beginGame: %v", err)
		}
		led[r.engine.Turn()]++
	}

	if led["p1"] == 0 || led["p2"] == 0 {
		t.Errorf("over %d games p1 led %d and p2 %d; the first turn is not being drawn",
			games, led["p1"], led["p2"])
	}
}

// The one room where the lead is not drawn: the human opens against the bot.
func TestABotGameOpensWithTheHuman(t *testing.T) {
	strategy, err := bot.New(bot.Easy, rand.New(rand.NewPCG(1, 2)))
	if err != nil {
		t.Fatalf("bot.New: %v", err)
	}
	r := &room{hub: &hub{}, dict: chainDict(), turnLimit: time.Second, strategy: strategy}
	r.seats[0] = &seat{id: "p1"}
	r.seats[1] = &seat{id: botPlayerID}

	for range 20 {
		if err := r.beginGame(); err != nil {
			t.Fatalf("beginGame: %v", err)
		}
		if got := r.engine.Turn(); got != "p1" {
			t.Fatalf("the bot game opened on %q, want the human", got)
		}
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

	lead, waits, start := awaitLead(t, host, guest)

	lead.submit("b c", start.GetTurnSeq())

	// The same move, rendered per recipient: by_me flips, my_turn flips.
	moverUpdate := lead.await("turn_update").GetTurnUpdate()
	otherUpdate := waits.await("turn_update").GetTurnUpdate()

	if !moverUpdate.GetPlayed().GetByMe() {
		t.Error("mover should see by_me = true")
	}
	if otherUpdate.GetPlayed().GetByMe() {
		t.Error("opponent should see by_me = false")
	}
	if moverUpdate.GetMyTurn() {
		t.Error("mover should not be on turn after moving")
	}
	if !otherUpdate.GetMyTurn() {
		t.Error("opponent should now be on turn")
	}
	if myScore(moverUpdate) != otherScore(otherUpdate) {
		t.Errorf("scores disagree across recipients: %d vs %d",
			myScore(moverUpdate), otherScore(otherUpdate))
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

	_, waits, _ := awaitLead(t, host, guest)

	over := waits.await("game_over").GetGameOver()
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
	lead, waits, start := awaitLead(t, host, guest)

	// Opening "a b" is used and current syllable is "b". Play "b a" so the
	// syllable returns to "a", where the opening word now links legally.
	lead.submit("b a", start.GetTurnSeq())
	replyTurn := waits.await("turn_update").GetTurnUpdate()

	waits.submit("a b", replyTurn.GetTurnSeq())

	got := waits.await("move_rejected").GetMoveRejected()
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

// TestNearMissSuggestionOnWire is the end-to-end proof that a diacritic typo
// carries a suggestion: the dictionary layer is unit-tested on its own, but
// only this shows the room actually wires MoveRejected.suggestion up.
func TestNearMissSuggestionOnWire(t *testing.T) {
	_, url := newTestServer(t, newTestDict("ngôn ngữ", "ngữ pháp"), Config{})
	c := dial(t, url)
	c.hello("Người chơi")
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
		StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
	}})
	started := c.await("game_started").GetGameStarted()

	// Typed with no diacritics at all, which the dictionary does not know as a
	// word but which strips to exactly one real one.
	c.submit("ngu phap", started.GetTurnSeq())

	rejected := c.await("move_rejected").GetMoveRejected()
	if rejected.GetReason() != noituv1.RejectReason_REJECT_REASON_NOT_IN_DICTIONARY {
		t.Fatalf("reason = %v, want NOT_IN_DICTIONARY", rejected.GetReason())
	}
	if rejected.GetSuggestion() != "ngữ pháp" {
		t.Errorf("suggestion = %q, want %q", rejected.GetSuggestion(), "ngữ pháp")
	}
}
