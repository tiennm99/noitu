package game

import (
	"slices"
	"testing"
	"time"
)

// The rules that only exist past two seats: a failed turn takes one player out
// rather than ending the game, the rest carry on from the same syllable, and
// the last one standing wins.

const (
	carol = PlayerID("carol")
	dave  = PlayerID("dave")
)

// fourPlayers opens a game on a chain long enough that nobody runs out of words
// by accident.
func fourPlayers(t *testing.T) *Engine {
	t.Helper()
	e, err := New(standardDict(), []PlayerID{alice, bob, carol, dave}, "ngôn ngữ", 20*time.Second, t0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

func TestTimeoutEliminatesAndPlaysOn(t *testing.T) {
	e := fourPlayers(t)
	syllable := e.Current()

	if !e.Timeout(t0.Add(21 * time.Second)) {
		t.Fatal("Timeout did not fire after the deadline")
	}

	if e.Over() {
		t.Error("a four-player game ended on the first timeout")
	}
	if e.Alive(alice) {
		t.Error("the player whose turn expired is still in the game")
	}
	if e.Turn() != bob {
		t.Errorf("Turn = %q, want %q", e.Turn(), bob)
	}
	// The position survives the player: whoever inherits the turn answers the
	// same syllable, against the same used set.
	if e.Current() != syllable {
		t.Errorf("Current = %q, want %q — an elimination is not a move", e.Current(), syllable)
	}
	// And they get a full turn to do it in, rather than the remains of one
	// somebody else spent.
	if want := t0.Add(41 * time.Second); !e.Deadline().Equal(want) {
		t.Errorf("Deadline = %v, want %v", e.Deadline(), want)
	}
}

func TestTheLastPlayerStandingWins(t *testing.T) {
	e := fourPlayers(t)

	// Three timeouts, each a turn limit after the last.
	for i, out := range []PlayerID{alice, bob, carol} {
		at := t0.Add(time.Duration(i+1) * 21 * time.Second)
		if !e.Timeout(at) {
			t.Fatalf("timeout %d did not fire", i)
		}
		if e.Alive(out) {
			t.Errorf("%q survived their own timeout", out)
		}
	}

	if !e.Over() {
		t.Fatal("the game did not end with one player left")
	}
	if e.Winner() != dave {
		t.Errorf("Winner = %q, want %q", e.Winner(), dave)
	}
}

// Standings rank by who outlasted whom. A player who scored more and went out
// earlier still places below one who was there at the end.
func TestStandingsRankByFinishingOrder(t *testing.T) {
	e := fourPlayers(t)

	// Alice banks a word before losing her next turn, so she outscores
	// everybody and still finishes last.
	if _, r := e.Submit(alice, "ngữ pháp", t0); r != ReasonNone {
		t.Fatalf("Submit: %s", r)
	}
	for i := range 3 {
		if !e.Timeout(t0.Add(time.Duration(i+1) * 21 * time.Second)) {
			t.Fatalf("timeout %d did not fire", i)
		}
	}

	got := e.Standings()
	if len(got) != 4 {
		t.Fatalf("Standings has %d rows, want 4", len(got))
	}
	// bob, carol, dave time out in that order, leaving alice.
	want := []PlayerID{alice, dave, carol, bob}
	for i, standing := range got {
		if standing.Player != want[i] {
			t.Errorf("rank %d is %q, want %q", i+1, standing.Player, want[i])
		}
		if standing.Rank != i+1 {
			t.Errorf("%q has rank %d at position %d", standing.Player, standing.Rank, i+1)
		}
	}
	if got[0].Reason != EndNone {
		t.Errorf("the winner left the game for reason %s, want %s", got[0].Reason, EndNone)
	}
	if got[1].Reason != EndTimeout {
		t.Errorf("an eliminated player's reason = %s, want %s", got[1].Reason, EndTimeout)
	}
}

// A dead end must not cost every remaining player a full turn clock each. The
// first one to face it loses it on their own time, as in a two-player game;
// everybody behind them has already seen that board and goes out with them,
// which leaves the player who closed the position standing.
func TestADeadEndSettlesInOneTurnNotThree(t *testing.T) {
	d := newDict("a b", "b c", "c d")
	e, err := New(d, []PlayerID{alice, bob, carol, dave}, "a b", 20*time.Second, t0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// a b -> b c -> c d, and "d" starts nothing.
	if _, r := e.Submit(alice, "b c", t0); r != ReasonNone {
		t.Fatalf("Submit b c: %s", r)
	}
	if _, r := e.Submit(bob, "c d", t0); r != ReasonNone {
		t.Fatalf("Submit c d: %s", r)
	}

	// Carol is on turn with nothing to play, and loses it to the clock.
	if !e.Timeout(t0.Add(21 * time.Second)) {
		t.Fatal("Timeout did not fire")
	}

	if !e.Over() {
		t.Fatal("a dead end left the game running with nobody able to answer it")
	}
	if e.Winner() != bob {
		t.Errorf("Winner = %q, want %q — the player who closed the position", e.Winner(), bob)
	}
	// Carol was the one on the clock. Dave and Alice never got a turn they
	// could have used, and go out with her rather than each spending one.
	if got := e.Snapshot().Eliminated; !slices.Equal(got, []PlayerID{carol, dave, alice}) {
		t.Errorf("Eliminated = %v, want [carol dave alice]", got)
	}
	if got := e.OutReason(dave); got != EndNoLegalMove {
		t.Errorf("dave went out for %s, want %s", got, EndNoLegalMove)
	}
}

// Past two seats a player may want out while somebody else is thinking, and
// holding them to a turn they have already given up on is not a rule worth
// having.
func TestResignOutOfTurnLeavesTheClockAlone(t *testing.T) {
	e := fourPlayers(t)
	deadline := e.Deadline()

	if !e.Resign(carol, t0.Add(5*time.Second)) {
		t.Fatal("Resign out of turn returned false")
	}

	if e.Over() {
		t.Error("one player leaving ended a four-player game")
	}
	if e.Alive(carol) {
		t.Error("the resigning player is still in the game")
	}
	if e.Turn() != alice {
		t.Errorf("Turn = %q, want %q — resigning out of turn moved the turn", e.Turn(), alice)
	}
	if !e.Deadline().Equal(deadline) {
		t.Error("resigning out of turn handed the player to act more time")
	}

	// And the turn skips the empty seat when it comes round.
	if _, r := e.Submit(alice, "ngữ pháp", t0.Add(6*time.Second)); r != ReasonNone {
		t.Fatalf("Submit: %s", r)
	}
	if e.Turn() != bob {
		t.Fatalf("Turn = %q, want %q", e.Turn(), bob)
	}
	if _, r := e.Submit(bob, "pháp luật", t0.Add(7*time.Second)); r != ReasonNone {
		t.Fatalf("Submit: %s", r)
	}
	if e.Turn() != dave {
		t.Errorf("Turn = %q, want %q — the turn stopped at an eliminated seat", e.Turn(), dave)
	}
}

// An eliminated player keeps their score and their words. Being out of the game
// is not being out of the record of it.
func TestAnEliminatedPlayerKeepsTheirScore(t *testing.T) {
	e := fourPlayers(t)

	if _, r := e.Submit(alice, "ngữ pháp", t0); r != ReasonNone {
		t.Fatalf("Submit: %s", r)
	}
	scored := e.Score(alice)
	if scored == 0 {
		t.Fatal("the move scored nothing")
	}

	e.Resign(alice, t0.Add(time.Second))

	if got := e.Score(alice); got != scored {
		t.Errorf("score after elimination = %d, want %d", got, scored)
	}
	if state := e.Snapshot(); len(state.History) != 1 || state.History[0].Player != alice {
		t.Errorf("the eliminated player's move is missing from the history: %+v", state.History)
	}
	if !slices.Contains(e.Players(), alice) {
		t.Error("Players dropped the eliminated seat, which the transport still has to render")
	}
}

// A submission from a player who is out is refused as a turn error, not
// silently applied to whoever is actually on turn.
func TestAnEliminatedPlayerCannotMove(t *testing.T) {
	e := fourPlayers(t)
	e.Resign(carol, t0)

	if _, r := e.Submit(carol, "ngữ pháp", t0.Add(time.Second)); r != ReasonNotYourTurn {
		t.Errorf("Submit from an eliminated player = %s, want %s", r, ReasonNotYourTurn)
	}
	if e.Resign(carol, t0.Add(time.Second)) {
		t.Error("Resign succeeded twice for the same player")
	}
}
