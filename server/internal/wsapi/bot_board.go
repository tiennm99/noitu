package wsapi

import (
	"iter"

	"github.com/tiennm99dev/noitu/server/internal/game"
)

// The bot's read-only view of a position, frozen off the engine on the room
// goroutine before a worker goroutine exists to race it.

// frozenBoard is an immutable position for a bot worker to search.
//
// It satisfies bot.Board without holding the engine. The dictionary is safe to
// share — the store loads once at Open and is read-only thereafter — but the
// used set is engine state, so it is copied.
type frozenBoard struct {
	legal []string
	used  map[string]struct{}
	dict  game.Dictionary
}

func freezeBoard(e *game.Engine) *frozenBoard {
	// UsedWords already includes the opening word — it seeds the engine's own
	// set — so there is nothing left to add here, and nothing to copy out of a
	// history that grows with the game.
	used := make(map[string]struct{})
	for word := range e.UsedWords() {
		used[word] = struct{}{}
	}

	return &frozenBoard{legal: e.LegalMoves(), used: used, dict: e.Dict()}
}

func (b *frozenBoard) LegalMoves() []string { return b.legal }

func (b *frozenBoard) Used(word string) bool {
	_, ok := b.used[word]
	return ok
}

func (b *frozenBoard) WordsStartingWith(syllable string) iter.Seq[string] {
	return b.dict.WordsStartingWith(syllable)
}

func (b *frozenBoard) LastSyllable(word string) (string, bool) {
	return b.dict.LastSyllable(word)
}
