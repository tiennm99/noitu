package wsapi

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
)

// The room's own conversation: who sees what, when, and what survives a
// seat being vacated.

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
	start := host.await("game_started").GetGameStarted()
	guest.await("game_started")
	resignAndSettle(t, host, guest, start)

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
