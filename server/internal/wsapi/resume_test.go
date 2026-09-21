package wsapi

import (
	"testing"
	"time"

	"github.com/coder/websocket"
	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
)

// Coming back with a resume token: within the grace window, past it, and
// what a token that no longer resolves to anything gets told.

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
	hostStart := host.await("game_started").GetGameStarted()
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
	if restored.GetMyTurn() != hostStart.GetMyTurn() {
		t.Error("the resumed player should come back to the turn they left")
	}
}

// TestUnknownResumeTokenIsAnsweredNotSilent: a token the server never
// registered, or has already forgotten past its grace window, used to get
// silence. The client's own resume latch then waited forever for a reply that
// was never coming — this is the fix, and the connection must still be usable
// afterward as the fresh session it is.
func TestUnknownResumeTokenIsAnsweredNotSilent(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Hello{Hello: &noituv1.Hello{
		ProtocolVersion: ProtocolVersion,
		Nickname:        "Người chơi",
		ResumeToken:     "no-such-token",
	}}})
	c.await("welcome")

	if code := c.await("error").GetError().GetCode(); code != "session_not_resumable" {
		t.Errorf("error code = %q, want session_not_resumable", code)
	}

	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	if code := c.await("room_state").GetRoomState().GetRoomCode(); code == "" {
		t.Error("a connection answered session_not_resumable must still be usable as a fresh session")
	}
}

// TestFreshHelloIsNotToldItCannotResume: a Hello with no resume token at all
// is not a resume attempt, and must not be answered as a failed one.
func TestFreshHelloIsNotToldItCannotResume(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người chơi")

	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	if m := c.recv(); payloadCase(m) == "error" {
		t.Fatalf("a fresh Hello with no resume token got %q, want none", m.GetError().GetCode())
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
