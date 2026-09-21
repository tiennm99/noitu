package wsapi

import (
	"testing"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
)

// Claiming a dead end: the syllable in play has no answer left.

// TestClaimDeadEndEliminatesImmediately walks a player into a real dead end
// and has them claim it rather than wait out the clock. The outcome must be
// exactly what a timeout would have produced: NO_LEGAL_MOVE, no answerable
// suggestions.
func TestClaimDeadEndEliminatesImmediately(t *testing.T) {
	// "b" starts nothing after "b c" is played, so whoever inherits "c" has no
	// move at all.
	_, url := newTestServer(t, newTestDict("a b", "b c"), Config{})
	host, guest, start := pvpRoom(t, url)
	lead, stuck := host, guest
	if !start.GetMyTurn() {
		lead, stuck = guest, host
	}

	lead.submit("b c", start.GetTurnSeq())
	lead.await("turn_update")
	turn := stuck.await("turn_update").GetTurnUpdate()
	if turn.GetCurrentSyllable() != "c" {
		t.Fatalf("current syllable = %q, want %q", turn.GetCurrentSyllable(), "c")
	}
	if !turn.GetMyTurn() {
		t.Fatal("the player left with the dead end should be on turn")
	}

	stuck.claimDeadEnd()

	out := stuck.await("player_eliminated").GetPlayerEliminated()
	if !out.GetIsMe() {
		t.Error("the claimant should be the one eliminated")
	}
	if out.GetReason() != noituv1.GameEndReason_GAME_END_REASON_NO_LEGAL_MOVE {
		t.Errorf("reason = %v, want NO_LEGAL_MOVE", out.GetReason())
	}
	if len(out.GetSuggestions()) != 0 {
		t.Errorf("suggestions = %v, want none for a genuine dead end", out.GetSuggestions())
	}

	stuck.await("game_over")
	lead.await("game_over")
}

// TestClaimDeadEndRefusedWhenAMoveExists checks the false claim costs nothing
// but the answer: the clock is untouched, proven by the original turn_seq
// still being accepted afterwards.
func TestClaimDeadEndRefusedWhenAMoveExists(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, start := pvpRoom(t, url)
	lead := host
	if !start.GetMyTurn() {
		lead = guest
	}

	lead.claimDeadEnd()
	if code := lead.await("error").GetError().GetCode(); code != "not_a_dead_end" {
		t.Errorf("code = %q, want not_a_dead_end", code)
	}

	// The turn_seq the claim was answered on is still the current one: a
	// submission stamped with it is still accepted rather than refused as
	// stale.
	lead.submit("b c", start.GetTurnSeq())
	lead.await("turn_update")
}

// TestClaimDeadEndOutOfTurnRefused: only the player to act may spend a claim,
// exactly as only they may spend a resignation.
func TestClaimDeadEndOutOfTurnRefused(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host, guest, start := pvpRoom(t, url)
	waits := guest
	if !start.GetMyTurn() {
		waits = host
	}

	waits.claimDeadEnd()
	if code := waits.await("error").GetError().GetCode(); code != "not_your_turn" {
		t.Errorf("code = %q, want not_your_turn", code)
	}
}
