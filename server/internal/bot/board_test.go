package bot

import (
	"iter"

	"github.com/tiennm99dev/noitu/server/internal/game"
)

// engineBoard adapts an Engine to Board for the simulations in this package.
// The server never needs it: the room hands the bot a Board of its own.
type engineBoard struct {
	e    *game.Engine
	dict game.Dictionary
}

// BoardFor wraps an engine so a test can drive a strategy against it.
//
// The dictionary comes from the engine rather than the caller: handing in a
// different one would let the bot search a graph the engine does not validate
// against, and that failure surfaces as the engine rejecting its own bot's
// move at runtime.
func BoardFor(e *game.Engine) Board {
	return &engineBoard{e: e, dict: e.Dict()}
}

func (b *engineBoard) LegalMoves() []string { return b.e.LegalMoves() }

func (b *engineBoard) Used(word string) bool { return b.e.Used(word) }

func (b *engineBoard) WordsStartingWith(syllable string) iter.Seq[string] {
	return b.dict.WordsStartingWith(syllable)
}

func (b *engineBoard) LastSyllable(word string) (string, bool) {
	return b.dict.LastSyllable(word)
}
