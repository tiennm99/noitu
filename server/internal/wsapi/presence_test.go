package wsapi

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/bot"
)

// A seat's reconnect window: the race between a connection tearing down and
// the room seating it, and what happens once the window runs out.

// TestCreateOpensGraceWindowForATornDownConnection covers the race between a
// connection dying and the room draining the message that seats it: nothing
// else would ever tell this room the creator is gone, since leaveRoom only
// notifies a room the session had already attached to.
func TestCreateOpensGraceWindowForATornDownConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dead := &session{id: "dead", ctx: ctx, out: make(chan []byte, 1)}

	r := &room{hub: &hub{}, dict: chainDict(), turnLimit: time.Second, graceFor: time.Minute}
	r.handleCreate(createInput{sess: dead})

	s := r.seats[0]
	if s == nil {
		t.Fatal("handleCreate did not seat the creator")
	}
	if s.sess != nil {
		t.Error("a torn-down connection was left looking connected")
	}
	if s.graceUntil.IsZero() {
		t.Error("no grace window was opened for the torn-down connection")
	}
}

// TestJoinOpensGraceWindowForATornDownConnection is the same race on the
// other seating path: without this, allConnected() would report true for a
// seat nobody is behind.
func TestJoinOpensGraceWindowForATornDownConnection(t *testing.T) {
	live := &session{id: "live", ctx: context.Background(), out: make(chan []byte, 8)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dead := &session{id: "dead", ctx: ctx, out: make(chan []byte, 1)}

	r := &room{hub: &hub{}, dict: chainDict(), turnLimit: time.Second, graceFor: time.Minute}
	r.handleCreate(createInput{sess: live})
	r.handleJoin(joinInput{sess: dead})

	s := r.seats[1]
	if s == nil {
		t.Fatal("handleJoin did not seat the second player")
	}
	if s.sess != nil {
		t.Error("a torn-down connection was left looking connected")
	}
	if r.allConnected() {
		t.Error("allConnected must not report true with a ghost seat behind it")
	}
}

// TestQuickMatchAutoStartSkipsAGhostSeat is the consequence C1 warns about: a
// real player auto-started into a game against a dead socket burns the whole
// turn clock before anyone notices. allConnected() reporting the ghost seat
// honestly is what keeps this from ever reaching beginGame.
func TestQuickMatchAutoStartSkipsAGhostSeat(t *testing.T) {
	live := &session{id: "live", ctx: context.Background(), out: make(chan []byte, 8)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dead := &session{id: "dead", ctx: ctx, out: make(chan []byte, 1)}

	r := &room{hub: &hub{}, dict: chainDict(), turnLimit: time.Second, graceFor: time.Minute}
	r.handleCreate(createInput{sess: live, autoStart: true})
	r.handleJoin(joinInput{sess: dead})

	if r.engine != nil {
		t.Error("a game was auto-started against a connection that had already torn down")
	}
}

// TestStartBotCancelsTheRoomForATornDownConnection: a bot room has no lobby to
// wait in and no idle timer while it has no engine yet, so a ghost seat here
// must end the room outright rather than being left to a grace window that
// nothing would ever clear.
func TestStartBotCancelsTheRoomForATornDownConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dead := &session{id: "dead", ctx: ctx, out: make(chan []byte, 1)}

	hctx, hcancel := context.WithCancel(context.Background())
	defer hcancel()
	r := newRoom(&hub{ctx: hctx}, "AAAAAA", time.Second, time.Second, time.Minute, roomModeBot)
	r.dict = chainDict()
	r.handleStartBot(startBotInput{sess: dead, difficulty: bot.Easy})

	if r.ctx.Err() == nil {
		t.Error("a room seated only by a torn-down connection must be cancelled")
	}
	if r.engine != nil {
		t.Error("a bot game was started against a connection that had already torn down")
	}
}

// TestResumeOpensGraceWindowForATornDownConnection covers the same race on
// the resume path: the new connection presenting the token can die before the
// room drains the resumeInput it produced.
func TestResumeOpensGraceWindowForATornDownConnection(t *testing.T) {
	live := &session{id: "live", ctx: context.Background(), out: make(chan []byte, 8)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dead := &session{id: "dead", ctx: ctx, out: make(chan []byte, 1)}

	r := &room{hub: &hub{}, dict: chainDict(), turnLimit: time.Second, graceFor: time.Minute}
	r.handleCreate(createInput{sess: live})
	r.seats[0].sess = nil
	r.seats[0].graceUntil = time.Now().Add(time.Minute)

	r.handleResume(resumeInput{player: "p1", sess: dead})

	s := r.seats[0]
	if s.sess != nil {
		t.Error("a torn-down resuming connection was left looking connected")
	}
	if s.graceUntil.IsZero() {
		t.Error("no grace window was reopened for the torn-down resuming connection")
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
