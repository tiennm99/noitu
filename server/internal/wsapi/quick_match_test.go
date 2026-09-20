package wsapi

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
)

// TestQuickMatchPairsTwoWaiters is the core promise: two strangers who both
// ask are seated together and the first game begins itself, with nobody
// pressing ready or start.
func TestQuickMatchPairsTwoWaiters(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})

	first := dial(t, url)
	first.hello("Người chờ")
	first.quickMatch()
	if !first.await("quick_match_status").GetQuickMatchStatus().GetQueued() {
		t.Fatal("the first asker should be queued, nobody else waiting yet")
	}

	second := dial(t, url)
	second.hello("Người đến sau")
	second.quickMatch()

	if got := second.await("quick_match_status").GetQuickMatchStatus().GetQueued(); got {
		t.Error("the one who completes the pair must not be left queued")
	}
	if got := first.await("quick_match_status").GetQuickMatchStatus().GetQueued(); got {
		t.Error("the waiter must be told the wait is over once matched")
	}

	// The room begins its own first game: neither side sets ready or start.
	firstStart := first.await("game_started").GetGameStarted()
	secondStart := second.await("game_started").GetGameStarted()
	if firstStart.GetMyTurn() == secondStart.GetMyTurn() {
		t.Fatal("exactly one player should be dealt the opening turn")
	}
}

// TestQuickMatchCancelStopsTheWait covers withdrawing from the queue and
// staying out of it: a cancelled waiter must not surface later as somebody's
// match.
func TestQuickMatchCancelStopsTheWait(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})

	waiter := dial(t, url)
	waiter.hello("Người chờ")
	waiter.quickMatch()
	if !waiter.await("quick_match_status").GetQuickMatchStatus().GetQueued() {
		t.Fatal("expected to be queued")
	}

	waiter.cancelQuickMatch()
	if got := waiter.await("quick_match_status").GetQuickMatchStatus().GetQueued(); got {
		t.Error("a cancel should answer queued:false")
	}

	// A cancel a second time is not an error: it is idempotent.
	waiter.cancelQuickMatch()
	if got := waiter.await("quick_match_status").GetQuickMatchStatus().GetQueued(); got {
		t.Error("cancelling twice should still answer queued:false, not an error")
	}

	other := dial(t, url)
	other.hello("Người khác")
	other.quickMatch()
	if !other.await("quick_match_status").GetQuickMatchStatus().GetQueued() {
		t.Error("the cancelled waiter must not still be in line to be matched with")
	}
}

// TestQuickMatchDropsADisconnectedWaiter is the ghost case: a waiter whose
// socket ends without a CancelQuickMatch must not be handed to the next
// stranger who asks.
func TestQuickMatchDropsADisconnectedWaiter(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})

	ghost := dial(t, url)
	ghost.hello("Ma")
	ghost.quickMatch()
	ghost.await("quick_match_status")
	_ = ghost.conn.Close(websocket.StatusNormalClosure, "")
	settle()

	a := dial(t, url)
	a.hello("A")
	a.quickMatch()
	if !a.await("quick_match_status").GetQuickMatchStatus().GetQueued() {
		t.Fatal("A should be freshly queued, not matched with a ghost")
	}

	b := dial(t, url)
	b.hello("B")
	b.quickMatch()

	a.await("game_started")
	b.await("game_started")
}

// TestQuickMatchRefusedWhenAlreadySeated guards against a session that
// already holds a seat asking to be queued as well.
func TestQuickMatchRefusedWhenAlreadySeated(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})

	c := dial(t, url)
	c.hello("Chủ phòng")
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	c.await("room_state")

	c.quickMatch()
	if got := c.await("error").GetError().GetCode(); got != "already_in_a_room" {
		t.Errorf("error code = %q, want already_in_a_room", got)
	}
}

// TestQuickMatchRefusesASecondRequest guards the other half of the same
// check: a session already queued must not be queued twice.
func TestQuickMatchRefusesASecondRequest(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})

	c := dial(t, url)
	c.hello("Người chờ")
	c.quickMatch()
	c.await("quick_match_status")

	c.quickMatch()
	if got := c.await("error").GetError().GetCode(); got != "already_queued" {
		t.Errorf("error code = %q, want already_queued", got)
	}
}

// TestQuickMatchServerFullTellsBothSides is the room-cap edge: a match that
// cannot open a room must not leave either side believing it is still
// queued.
func TestQuickMatchServerFullTellsBothSides(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{MaxRooms: 1})

	filler := dial(t, url)
	filler.hello("Chiếm chỗ")
	filler.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_CreateRoom{CreateRoom: &noituv1.CreateRoom{}}})
	filler.await("room_state")

	waiter := dial(t, url)
	waiter.hello("Người chờ")
	waiter.quickMatch()
	waiter.await("quick_match_status")

	newcomer := dial(t, url)
	newcomer.hello("Người đến sau")
	newcomer.quickMatch()

	if got := newcomer.await("error").GetError().GetCode(); got != "server_full" {
		t.Errorf("newcomer error = %q, want server_full", got)
	}
	if got := waiter.await("error").GetError().GetCode(); got != "server_full" {
		t.Errorf("waiter error = %q, want server_full", got)
	}

	// Neither is left queued: a fresh pair must not resurrect this one.
	third := dial(t, url)
	third.hello("Người thứ ba")
	third.quickMatch()
	if !third.await("quick_match_status").GetQuickMatchStatus().GetQueued() {
		t.Fatal("third should be freshly queued, not immediately matched with a stale waiter")
	}
}

// TestQuickMatchSkipsAWaiterWhoseConnectionEnded covers the window between a
// waiter's socket closing and its teardown dequeuing it: a match drawn in that
// window must not seat the ghost.
func TestQuickMatchSkipsAWaiterWhoseConnectionEnded(t *testing.T) {
	h := newHub(t.Context(), chainDict(), time.Second, time.Second, time.Minute, 0)

	gone, cancelGone := context.WithCancel(t.Context())
	cancelGone()
	ghost := &session{id: "ghost", ctx: gone, out: make(chan []byte, 1)}
	live := &session{id: "live", ctx: t.Context(), out: make(chan []byte, 8)}

	h.mu.Lock()
	h.waiting = append(h.waiting, ghost)
	h.mu.Unlock()

	if err := h.quickMatch(live); err != nil {
		t.Fatalf("quickMatch: %v", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.waiting) != 1 || h.waiting[0] != live {
		t.Fatalf("queue = %v, want the live session alone", h.waiting)
	}
	if len(h.rooms) != 0 {
		t.Fatal("a room was opened for a waiter with no connection behind it")
	}
}

// TestNearMissIsWithheldWhenUnplayable: a real word one tone mark away is not
// offered when it does not answer the syllable in play, because taking the
// suggestion would only earn a second refusal.
func TestNearMissIsWithheldWhenUnplayable(t *testing.T) {
	// Opening "ngôn ngữ" leaves "ngữ" to answer; "cà phê" is real but does not.
	_, url := newTestServer(t, newTestDict("ngôn ngữ", "ngữ pháp", "cà phê"), Config{})
	c := dial(t, url)
	c.hello("Người chơi")
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
		StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
	}})
	started := c.await("game_started").GetGameStarted()

	c.submit("ca phe", started.GetTurnSeq())
	rejected := c.await("move_rejected").GetMoveRejected()
	if rejected.GetReason() != noituv1.RejectReason_REJECT_REASON_NOT_IN_DICTIONARY {
		t.Fatalf("reason = %v, want NOT_IN_DICTIONARY", rejected.GetReason())
	}
	if rejected.GetSuggestion() != "" {
		t.Errorf("suggestion = %q, want none for a word that does not link", rejected.GetSuggestion())
	}
}
