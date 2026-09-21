package wsapi

import (
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
)

// The lobby between games: seating, readiness, starting, kicking, and
// leaving.

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

	hostState, guestState := resignAndSettle(t, host, guest, first)
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
	host, guest, first := pvpRoom(t, url)

	// The owner resigns, so the guest takes the first game.
	hostState, guestState := resignAndSettle(t, host, guest, first)
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
	second := guest.await("game_started").GetGameStarted()

	resignFrom(t, guest, host, second)
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

// TestLobbyActionsAreRefusedDuringAGame keeps the lobby's own business out of a
// game in progress. Leaving is not part of it: a player may walk out of a game
// whether or not it is their turn, which
// TestLeavingMidGameFreesTheSeatAndLeavesTheRestPlaying covers.
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

	c.resign()
	c.await("game_over")

	// not_in_a_room is the session reporting that the room has gone: the
	// goroutine and engine are released rather than parked in a lobby no bot
	// can ready for.
	c.setReady(true)
	if got := c.await("error").GetError().GetCode(); got != "not_in_a_room" {
		t.Errorf("a finished bot room answered %q, want it to be gone", got)
	}
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
