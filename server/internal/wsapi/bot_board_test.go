package wsapi

import (
	"slices"
	"testing"
	"time"

	"github.com/tiennm99dev/noitu/server/internal/game"
)

// The bot's frozen view of a position.

func TestFreezeBoardIncludesTheOpeningWord(t *testing.T) {
	// The engine counts the opening word as played but Snapshot's history does
	// not list it. A frozen board that missed it would let the bot choose a
	// word the engine then rejects as already used — the two would disagree
	// about the position while appearing to share one dictionary.
	dict := chainDict()
	e, err := game.New(dict, []game.PlayerID{"p1", "p2"}, "a b", time.Second, time.Now())
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	board := freezeBoard(e)
	if !board.Used("a b") {
		t.Error("frozen board does not consider the opening word played")
	}
	if !slices.Contains(board.LegalMoves(), "b c") {
		t.Errorf("legal moves = %v, want the one continuation", board.LegalMoves())
	}
}
