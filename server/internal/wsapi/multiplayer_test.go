package wsapi

import (
	"testing"
	"time"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
)

// What a room of more than two people does that a pair does not: seat up to
// four, refuse the fifth, wait for every guest before it starts, kick by name,
// and outlive the first player it knocks out.

// roomOf seats n players and leaves them in the lobby. The first is the owner.
//
// It returns the owner's last view of the room as well as the clients: every
// room_state the seating produced has been read by then, so a caller that
// awaited another would wait for one nothing is going to send.
func roomOf(t *testing.T, url string, n int) (clients []*testClient, state *noituv1.RoomState) {
	t.Helper()

	names := []string{"Chủ phòng", "Khách", "Người thứ ba", "Người thứ tư", "Người thứ năm"}
	host := dial(t, url)
	host.hello(names[0])
	host.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	state = host.await("room_state").GetRoomState()
	clients = append(clients, host)

	for i := 1; i < n; i++ {
		c := dial(t, url)
		c.hello(names[i])
		c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
			JoinRoom: &noituv1.JoinRoom{RoomCode: state.GetRoomCode()},
		}})
		clients = append(clients, c)
		// Everybody seated sees the arrival, which is also what keeps each
		// client's inbox drained before the next assertion reads from it.
		for _, seated := range clients {
			got := seated.await("room_state").GetRoomState()
			if seated == host {
				state = got
			}
		}
	}
	return clients, state
}

func TestARoomSeatsFourAndRefusesTheFifth(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	_, state := roomOf(t, url, maxPlayers)

	if got := len(state.GetPlayers()); got != maxPlayers {
		t.Fatalf("the room holds %d players, want %d", got, maxPlayers)
	}
	if state.GetMaxPlayers() != maxPlayers || state.GetMinPlayers() != minPlayers {
		t.Errorf("limits = %d/%d, want %d/%d",
			state.GetMinPlayers(), state.GetMaxPlayers(), minPlayers, maxPlayers)
	}
	// Exactly one row is the recipient's, and exactly one is the owner's.
	me, owners := 0, 0
	for _, p := range state.GetPlayers() {
		if p.GetIsMe() {
			me++
		}
		if p.GetIsOwner() {
			owners++
		}
	}
	if me != 1 || owners != 1 {
		t.Errorf("the room has %d rows marked mine and %d marked owner, want 1 of each", me, owners)
	}

	fifth := dial(t, url)
	fifth.hello("Người thứ năm")
	fifth.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_JoinRoom{
		JoinRoom: &noituv1.JoinRoom{RoomCode: state.GetRoomCode()},
	}})
	if got := fifth.await("error").GetError().GetCode(); got != "room_full" {
		t.Errorf("the fifth joiner got %q, want room_full", got)
	}
}

// Every guest, not merely the first: a room of four that starts on one yes has
// dealt three people a turn they never agreed to.
func TestStartWaitsForEveryGuest(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	clients, _ := roomOf(t, url, 3)
	host, second, third := clients[0], clients[1], clients[2]

	second.setReady(true)
	for _, c := range clients {
		c.await("room_state")
	}

	host.startGame()
	if got := host.await("error").GetError().GetCode(); got != "not_everyone_ready" {
		t.Fatalf("starting with one guest ready returned %q, want not_everyone_ready", got)
	}

	third.setReady(true)
	for _, c := range clients {
		state := c.await("room_state").GetRoomState()
		if !state.GetCanStart() {
			t.Errorf("can_start is false with everybody ready: %+v", state)
		}
	}

	host.startGame()
	// Who leads is drawn, so what has to hold is that the room agrees on one
	// leader and that exactly one player was dealt the turn.
	lead, onTurn := "", 0
	for _, c := range clients {
		start := c.await("game_started").GetGameStarted()
		if got := len(start.GetPlayers()); got != 3 {
			t.Errorf("the game was dealt to %d players, want 3", got)
		}
		switch {
		case lead == "":
			lead = start.GetTurnPlayerId()
		case start.GetTurnPlayerId() != lead:
			t.Errorf("turn_player_id = %q, want %q — the room must agree on who leads",
				start.GetTurnPlayerId(), lead)
		}
		if start.GetMyTurn() {
			onTurn++
		}
	}
	if onTurn != 1 {
		t.Errorf("%d players were dealt the first turn, want exactly 1", onTurn)
	}
}

func TestKickNamesASeat(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	clients, _ := roomOf(t, url, 3)
	host, second, third := clients[0], clients[1], clients[2]

	// The owner cannot free their own seat: leaving is what an owner who wants
	// out does, and it hands the room on rather than dropping it.
	host.kickPlayer("p1")
	if got := host.await("error").GetError().GetCode(); got != "cannot_kick_self" {
		t.Errorf("kicking themselves returned %q, want cannot_kick_self", got)
	}

	// Readiness is a commitment, per target rather than per room.
	second.setReady(true)
	for _, c := range clients {
		c.await("room_state")
	}
	host.kickPlayer("p2")
	if got := host.await("error").GetError().GetCode(); got != "player_is_ready" {
		t.Errorf("kicking a ready player returned %q, want player_is_ready", got)
	}

	// The unready one goes, and the ready one is untouched.
	host.kickPlayer("p3")
	if got := third.await("error").GetError().GetCode(); got != "kicked" {
		t.Fatalf("the kicked player was told %q", got)
	}

	state := host.await("room_state").GetRoomState()
	if got := len(state.GetPlayers()); got != 2 {
		t.Fatalf("the room holds %d players after one kick, want 2", got)
	}
	if slotFor(state, "p3") != nil {
		t.Error("the kicked seat is still occupied")
	}
	if seat := slotFor(state, "p2"); seat == nil || !seat.GetReady() {
		t.Error("kicking one player disturbed another's readiness")
	}
}

// The room outlives the first player it knocks out: the rest carry on from the
// same syllable, and the player who went out stays to watch.
func TestAGameOutlivesItsFirstElimination(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{TurnLimit: 10 * time.Second})
	clients, _ := roomOf(t, url, 3)
	host, second, third := clients[0], clients[1], clients[2]

	second.setReady(true)
	third.setReady(true)
	for _, c := range clients {
		c.await("room_state")
	}

	host.startGame()
	starts := map[*testClient]*noituv1.GameStarted{}
	for _, c := range clients {
		starts[c] = c.await("game_started").GetGameStarted()
	}

	// The player on turn gives up. Two are left, so the game does not end.
	// Which player that is, is drawn at the start, so the test follows the
	// turn rather than assuming the owner has it. Players are listed in turn
	// order from the leader, so the seat after them inherits the position.
	lead := onTurnClient(t, clients, starts)
	leadID := starts[lead].GetTurnPlayerId()
	nextID := starts[lead].GetPlayers()[1].GetPlayerId()
	next := clientWithID(t, clients, starts, nextID)

	lead.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Resign{Resign: &noituv1.Resign{}}})

	for _, c := range clients {
		out := c.await("player_eliminated").GetPlayerEliminated()
		if out.GetPlayerId() != leadID {
			t.Errorf("%q went out, want %q", out.GetPlayerId(), leadID)
		}
		if got := out.GetIsMe(); got != (c == lead) {
			t.Errorf("is_me = %v for the wrong recipient", got)
		}
		if c != lead && len(out.GetSuggestions()) != 0 {
			t.Errorf("a player who is still in was sent suggestions %v", out.GetSuggestions())
		}
	}

	// A turn update with no word: the position survived the player who left it.
	update := next.await("turn_update").GetTurnUpdate()
	if update.GetPlayed() != nil {
		t.Error("an elimination reported a word as played")
	}
	if update.GetCurrentSyllable() != starts[next].GetCurrentSyllable() {
		t.Errorf("the syllable moved on an elimination: %q -> %q",
			starts[next].GetCurrentSyllable(), update.GetCurrentSyllable())
	}
	if update.GetTurnPlayerId() != nextID || !update.GetMyTurn() {
		t.Errorf("turn went to %q, want %q", update.GetTurnPlayerId(), nextID)
	}
	if update.GetTurnSeq() == starts[next].GetTurnSeq() {
		t.Error("the turn sequence did not move, so a stale submission could still land")
	}

	// The player who went out is still in the room and still being told what
	// is happening in it.
	watching := lead.await("turn_update").GetTurnUpdate()
	if watching.GetMyTurn() {
		t.Error("an eliminated player was dealt a turn")
	}
	var eliminated bool
	for _, p := range watching.GetPlayers() {
		if p.GetPlayerId() == leadID {
			eliminated = p.GetEliminated()
		}
	}
	if !eliminated {
		t.Error("the table does not show the eliminated player as out")
	}

	// And they can still talk, which is the other half of staying in the room.
	lead.say("chúc may mắn")
	if got := next.await("chat_message").GetChatMessage().GetText(); got != "chúc may mắn" {
		t.Errorf("an eliminated player's message arrived as %q", got)
	}
}

// Giving up is a move, so only the player to act may play it. A seat that
// could resign while somebody else was thinking would be ending its game on
// another player's turn.
func TestResigningOutOfTurnIsRefused(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{TurnLimit: 10 * time.Second})
	clients, _ := roomOf(t, url, 3)
	host, second, third := clients[0], clients[1], clients[2]

	second.setReady(true)
	third.setReady(true)
	for _, c := range clients {
		c.await("room_state")
	}

	host.startGame()
	starts := map[*testClient]*noituv1.GameStarted{}
	for _, c := range clients {
		starts[c] = c.await("game_started").GetGameStarted()
	}

	// The seat two along from the leader: not on turn, and not the one that
	// would inherit the turn either.
	lead := onTurnClient(t, clients, starts)
	quitterID := starts[lead].GetPlayers()[2].GetPlayerId()
	quitter := clientWithID(t, clients, starts, quitterID)
	seq := starts[lead].GetTurnSeq()

	quitter.resign()
	if got := quitter.await("error").GetError().GetCode(); got != "not_your_turn" {
		t.Errorf("an out-of-turn resignation answered %q, want not_your_turn", got)
	}

	// Nothing about the game moved: the word the player to act was already
	// typing is still answering this position.
	lead.submit("b c", seq)
	played := lead.await("turn_update").GetTurnUpdate().GetPlayed()
	if played == nil || played.GetWord() != "b c" {
		t.Fatalf("the position moved on a refused resignation: %v", played)
	}
}

// Leaving is what a player who wants out of a game they are not on turn in
// does. Their seat goes, the rest are told somebody left, and the player to
// act keeps the position, the clock and the sequence they were answering.
func TestLeavingMidGameFreesTheSeatAndLeavesTheRestPlaying(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{TurnLimit: 10 * time.Second})
	clients, _ := roomOf(t, url, 3)
	host, second, third := clients[0], clients[1], clients[2]

	second.setReady(true)
	third.setReady(true)
	for _, c := range clients {
		c.await("room_state")
	}

	host.startGame()
	starts := map[*testClient]*noituv1.GameStarted{}
	for _, c := range clients {
		starts[c] = c.await("game_started").GetGameStarted()
	}

	// The seat two along from the leader, as above: it is neither on turn nor
	// the one that inherits the turn, so nothing about the position depends on
	// it.
	lead := onTurnClient(t, clients, starts)
	leaverID := starts[lead].GetPlayers()[2].GetPlayerId()
	leaver := clientWithID(t, clients, starts, leaverID)
	seq := starts[lead].GetTurnSeq()

	leaver.leaveRoom()

	// Out of the game as somebody who left, not as somebody who gave up.
	out := lead.await("player_eliminated").GetPlayerEliminated()
	if out.GetPlayerId() != leaverID {
		t.Errorf("%q went out, want %q", out.GetPlayerId(), leaverID)
	}
	if got := out.GetReason(); got != noituv1.GameEndReason_GAME_END_REASON_OPPONENT_LEFT {
		t.Errorf("the room reported %v, want opponent_left", got)
	}

	update := lead.await("turn_update").GetTurnUpdate()
	if !update.GetMyTurn() {
		t.Error("the turn moved on a player leaving from behind it")
	}
	if update.GetTurnSeq() != seq {
		t.Errorf("the turn sequence moved to %d, want %d: the word in flight still answers this position",
			update.GetTurnSeq(), seq)
	}
	if update.GetDeadlineUnixMs() != starts[lead].GetDeadlineUnixMs() {
		t.Error("the clock restarted on somebody else leaving")
	}

	// And the seat is free, so the room has room for somebody again.
	state := lead.await("room_state").GetRoomState()
	if slotFor(state, leaverID) != nil {
		t.Error("the seat is still occupied by a player who left")
	}
	if got := len(state.GetPlayers()); got != 2 {
		t.Errorf("the room holds %d players after one left, want 2", got)
	}

	// The game is still on, and still playable.
	lead.submit("b c", seq)
	played := lead.await("turn_update").GetTurnUpdate().GetPlayed()
	if played == nil || played.GetWord() != "b c" {
		t.Fatalf("the game stopped being playable: %v", played)
	}
}

// onTurnClient is the client that drew the first turn.
func onTurnClient(t *testing.T, clients []*testClient, starts map[*testClient]*noituv1.GameStarted) *testClient {
	t.Helper()
	for _, c := range clients {
		if starts[c].GetMyTurn() {
			return c
		}
	}
	t.Fatal("no client was dealt the first turn")
	return nil
}

// clientWithID is the client seated at id, found through the is_me row it was
// sent — a client is told which seat is its own and nothing else identifies it.
func clientWithID(t *testing.T, clients []*testClient, starts map[*testClient]*noituv1.GameStarted, id string) *testClient {
	t.Helper()
	for _, c := range clients {
		for _, p := range starts[c].GetPlayers() {
			if p.GetIsMe() && p.GetPlayerId() == id {
				return c
			}
		}
	}
	t.Fatalf("no client is seated at %q", id)
	return nil
}

// The last elimination ends it, and everybody is shown the same table from
// their own side.
func TestStandingsReachEverySeat(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{TurnLimit: 10 * time.Second})
	clients, _ := roomOf(t, url, 3)
	host, second, third := clients[0], clients[1], clients[2]

	second.setReady(true)
	third.setReady(true)
	for _, c := range clients {
		c.await("room_state")
	}
	host.startGame()
	starts := map[*testClient]*noituv1.GameStarted{}
	for _, c := range clients {
		starts[c] = c.await("game_started").GetGameStarted()
	}

	// Each player gives up on their own turn, which is the only way to. The
	// first to act goes, the seat behind them inherits the position and goes
	// too, and the third is left standing — so the finishing order is the
	// draw's order and the table below is built from it rather than from the
	// seat ids.
	lead := onTurnClient(t, clients, starts)
	order := starts[lead].GetPlayers()
	leadID, nextID, survivorID := order[0].GetPlayerId(), order[1].GetPlayerId(), order[2].GetPlayerId()
	next := clientWithID(t, clients, starts, nextID)

	lead.resign()
	for _, c := range clients {
		c.await("player_eliminated")
	}
	next.resign()

	winners := 0
	for _, c := range clients {
		over := c.await("game_over").GetGameOver()
		if over.GetIWon() {
			winners++
		}

		standings := over.GetStandings()
		if len(standings) != 3 {
			t.Fatalf("standings has %d rows, want 3", len(standings))
		}
		// Finishing order: the survivor, then the players who went out, latest
		// first. Rank matches position, so the two cannot drift apart.
		want := []string{survivorID, nextID, leadID}
		mine := 0
		for i, row := range standings {
			if row.GetPlayerId() != want[i] {
				t.Errorf("rank %d is %q, want %q", i+1, row.GetPlayerId(), want[i])
			}
			if int(row.GetRank()) != i+1 {
				t.Errorf("%q has rank %d at position %d", row.GetPlayerId(), row.GetRank(), i+1)
			}
			if row.GetIsMe() {
				mine++
			}
		}
		if mine != 1 {
			t.Errorf("%d standings rows are marked mine, want 1", mine)
		}
	}
	if winners != 1 {
		t.Errorf("%d players were told they won, want 1", winners)
	}
}

// Two people dropping at once is two windows, not one. The room has to hold
// both seats and settle each on its own deadline.
func TestSeveralReconnectWindowsRunAtOnce(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{GraceFor: 300 * time.Millisecond})
	clients, _ := roomOf(t, url, 3)
	host, second, third := clients[0], clients[1], clients[2]

	second.conn.CloseNow()
	third.conn.CloseNow()

	// Both seats are held first, then both are freed. The owner is the one
	// still here to watch it happen.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state := host.await("room_state").GetRoomState()
		if len(state.GetPlayers()) == 1 {
			return
		}
	}
	t.Error("the room never freed the seats whose windows had run out")
}
