package bot

import (
	"math/rand/v2"
	"time"
)

// easy plays a uniformly random legal move.
//
// It does not look at what the move hands the opponent, so it will happily
// give away a winning position — which is the point.
type easy struct {
	rng *rand.Rand
}

func (s *easy) Difficulty() Difficulty { return Easy }

func (s *easy) ThinkingDelay() time.Duration {
	return thinkingDelay(s.rng, 400*time.Millisecond, 900*time.Millisecond)
}

func (s *easy) Choose(b Board) (string, error) {
	moves := b.LegalMoves()
	if len(moves) == 0 {
		return "", ErrNoMove
	}
	return moves[s.rng.IntN(len(moves))], nil
}
