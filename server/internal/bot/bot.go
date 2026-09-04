// Package bot chooses moves for the computer opponent.
//
// The bot is a player like any other: it picks a word, and that word goes
// through the same Engine.Submit validation a human's would. Nothing here can
// bend the rules, because nothing here applies them.
//
// Choose is pure and fast. The pause that makes the bot feel human is a
// duration this package reports and the caller schedules — sleeping inside
// Choose would make every test wait in real time.
package bot

import (
	"errors"
	"iter"
	"math/rand/v2"
	"time"

	"github.com/tiennm99dev/noitu/server/internal/game"
)

// Difficulty selects a strategy.
type Difficulty int

const (
	Easy Difficulty = iota + 1
	Medium
	Hard
)

func (d Difficulty) String() string {
	switch d {
	case Easy:
		return "easy"
	case Medium:
		return "medium"
	case Hard:
		return "hard"
	}
	return "unknown"
}

// ErrNoMove means the bot has nothing legal to play, so it has lost.
var ErrNoMove = errors.New("bot: no legal move")

// Board is what a strategy may look at. It is deliberately narrower than
// *game.Engine: a strategy can read the position but cannot play a move, so it
// cannot sidestep validation.
//
// Only what the strategies actually use. A dictionary's static out-degree is
// deliberately absent — what matters is how many continuations remain unplayed,
// which the strategies derive from WordsStartingWith and Used.
type Board interface {
	LegalMoves() []string
	Used(word string) bool
	WordsStartingWith(syllable string) iter.Seq[string]
	LastSyllable(word string) (string, bool)
}

// Strategy picks a move for the position.
type Strategy interface {
	Choose(b Board) (string, error)
	// ThinkingDelay is how long the caller should wait before playing the
	// chosen move, so the bot does not answer instantly.
	ThinkingDelay() time.Duration
	Difficulty() Difficulty
}

// New returns the strategy for a difficulty, seeded by rng.
//
// The rng is injected rather than global so a test can replay an identical
// game, and so two concurrent rooms never share a source.
func New(d Difficulty, rng *rand.Rand) (Strategy, error) {
	if rng == nil {
		// Every strategy dereferences this; failing here beats a nil panic on
		// the first move.
		return nil, errors.New("bot: nil rng")
	}

	switch d {
	case Easy:
		return &easy{rng: rng}, nil
	case Medium:
		return &medium{rng: rng}, nil
	case Hard:
		return &hard{rng: rng}, nil
	}
	return nil, errors.New("bot: unknown difficulty")
}

// thinkingDelay returns a human-looking pause. Faster as difficulty rises,
// which reads as the bot being more sure of itself.
func thinkingDelay(rng *rand.Rand, min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	return min + time.Duration(rng.Int64N(int64(max-min)))
}

// engineBoard adapts an Engine to Board.
type engineBoard struct {
	e    *game.Engine
	dict game.Dictionary
}

// BoardFor wraps an engine so strategies can inspect it.
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

// remainingOutDegree counts the continuations still available from a syllable,
// ignoring `excluding` — the move being considered, which will itself be spent
// once played. Pass "" to exclude nothing.
//
// This is the number that matters, not the dictionary's static out-degree: a
// syllable with fifty words is still a dead end if all fifty are used.
func remainingOutDegree(b Board, syllable, excluding string) int {
	n := 0
	for word := range b.WordsStartingWith(syllable) {
		if word != excluding && !b.Used(word) {
			n++
		}
	}
	return n
}
