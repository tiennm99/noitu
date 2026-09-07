// Package game implements the nối từ rules: what counts as a legal move, whose
// turn it is, and when a game is over.
//
// It is transport-free by design. No WebSocket, no protobuf, no wall clock: the
// turn deadline is data the caller supplies and reads back, so every rule can
// be tested without a timer or a network. The room in the wsapi layer owns an
// Engine and is the only goroutine that touches it.
package game

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/tiennm99dev/noitu/server/internal/vietnamese"
)

// Scoring. Longer words are worth more, which gives players a reason to reach
// for three- and four-syllable compounds rather than always playing the
// shortest legal word.
const (
	basePoints       = 10
	chainBonus       = 2 // per word already played
	syllableBonus    = 5 // per syllable beyond the minimum
	maxPointsPerWord = 100
)

// Engine holds one game.
//
// Not safe for concurrent use. Exactly one goroutine owns an Engine — in the
// server that is the room goroutine, which serializes every input through a
// single channel.
type Engine struct {
	dict      Dictionary
	players   []PlayerID
	used      map[string]struct{}
	current   string
	turnIndex int
	turnLimit time.Duration
	deadline  time.Time
	history   []Move
	scores    map[PlayerID]int

	over      bool
	winner    PlayerID
	endReason EndReason
}

// New starts a game from an opening word.
//
// The opening word counts as played: it seeds the used set and fixes the
// syllable the first player must link from.
func New(dict Dictionary, players []PlayerID, opening string, turnLimit time.Duration, now time.Time) (*Engine, error) {
	if dict == nil {
		return nil, errors.New("game: nil dictionary")
	}
	if len(players) < 2 {
		return nil, fmt.Errorf("game: need at least 2 players, got %d", len(players))
	}
	if turnLimit <= 0 {
		return nil, fmt.Errorf("game: turn limit must be positive, got %v", turnLimit)
	}
	seen := make(map[PlayerID]struct{}, len(players))
	for _, p := range players {
		if _, dup := seen[p]; dup {
			return nil, fmt.Errorf("game: duplicate player %q", p)
		}
		seen[p] = struct{}{}
	}

	canonical, ok := dict.Resolve(opening)
	if !ok {
		return nil, fmt.Errorf("game: opening word %q is not in the dictionary", opening)
	}
	last, ok := dict.LastSyllable(canonical)
	if !ok {
		return nil, fmt.Errorf("game: opening word %q has no last syllable", canonical)
	}

	e := &Engine{
		dict:      dict,
		players:   append([]PlayerID{}, players...),
		used:      map[string]struct{}{canonical: {}},
		current:   last,
		turnLimit: turnLimit,
		deadline:  now.Add(turnLimit),
		scores:    make(map[PlayerID]int, len(players)),
	}
	for _, p := range players {
		e.scores[p] = 0
	}

	// An opening whose last syllable starts nothing hands the first player a
	// game they have already lost, with no move to make and no reason given —
	// it would resolve only when the turn timer expired, reported as a timeout.
	// Refuse it here so the caller picks another opening.
	if !e.HasLegalMove() {
		return nil, fmt.Errorf("game: opening word %q ends on %q, which starts no other word", canonical, last)
	}

	return e, nil
}

// Dict returns the dictionary this game is played against, so a bot searches
// the same word graph that Submit validates against.
func (e *Engine) Dict() Dictionary { return e.dict }

// Turn reports whose move it is.
func (e *Engine) Turn() PlayerID { return e.players[e.turnIndex] }

// Current reports the syllable the next word must start with.
func (e *Engine) Current() string { return e.current }

// Deadline reports when the current turn expires.
func (e *Engine) Deadline() time.Time { return e.deadline }

// Over reports whether the game has finished.
func (e *Engine) Over() bool { return e.over }

// Winner reports the winner. Meaningless while the game is in play.
func (e *Engine) Winner() PlayerID { return e.winner }

// ChainLength reports how many words have been played, opening word included.
func (e *Engine) ChainLength() int { return len(e.history) + 1 }

// Submit validates a player's word and, if legal, plays it.
//
// The returned Move carries the canonical spelling; on rejection the reason
// says which rule failed.
//
// Validation order is turn, then length, then dictionary, then link, then
// reuse. Resolving before checking the link is not optional: canonicalization
// can move the first syllable ("sỹ hai" resolves to "sĩ hai"), so a link check
// against what the player typed would reject legal moves.
//
// raw is untrusted input and is normalized before use, but its length is not
// bounded here: the transport layer caps message size before a word reaches
// this point.
func (e *Engine) Submit(p PlayerID, raw string, now time.Time) (Move, RejectReason) {
	if e.over {
		return Move{}, ReasonGameOver
	}
	if p != e.Turn() {
		return Move{}, ReasonNotYourTurn
	}
	if e.IsExpired(now) {
		e.expire()
		return Move{}, ReasonTimeout
	}

	normalized, syllables, err := vietnamese.Normalize(raw)
	if err != nil || !vietnamese.HasEnoughSyllables(syllables) {
		return Move{}, ReasonTooFewSyllables
	}

	canonical, ok := e.dict.Resolve(normalized)
	if !ok {
		return Move{}, ReasonNotInDictionary
	}

	first, ok := e.dict.FirstSyllable(canonical)
	if !ok {
		return Move{}, ReasonNotInDictionary
	}
	if first != e.current {
		return Move{}, ReasonWrongLink
	}

	if _, played := e.used[canonical]; played {
		return Move{}, ReasonAlreadyUsed
	}

	last, ok := e.dict.LastSyllable(canonical)
	if !ok {
		return Move{}, ReasonNotInDictionary
	}

	move := Move{
		Player:    p,
		Word:      canonical,
		Typed:     raw,
		First:     first,
		Last:      last,
		Syllables: len(syllables),
		Points:    e.pointsFor(len(syllables)),
		At:        now,
	}

	e.used[canonical] = struct{}{}
	e.history = append(e.history, move)
	e.scores[p] += move.Points
	e.current = last
	e.turnIndex = (e.turnIndex + 1) % len(e.players)
	e.deadline = now.Add(e.turnLimit)

	// A dead end is deliberately not the end of the game. Ending it here would
	// hand the mover a win the moment the position closed, before the other
	// player had seen the board at all — they get their turn, and lose it to
	// the clock like any other they cannot answer. NoMove lets a caller who
	// has nothing to wait for (the bot) settle it immediately instead.

	return move, ReasonNone
}

// pointsFor scores a word about to be played. The chain term counts the words
// already down, opening word included, which is what ChainLength reports.
func (e *Engine) pointsFor(syllables int) int {
	points := basePoints + chainBonus*e.ChainLength() + syllableBonus*(syllables-vietnamese.MinSyllables)
	return min(points, maxPointsPerWord)
}

// LegalMoves lists every word the player to act may play.
//
// Allocates, so the bot's search uses HasLegalMove and iterates directly rather
// than calling this per node.
func (e *Engine) LegalMoves() []string {
	var moves []string
	for word := range e.dict.WordsStartingWith(e.current) {
		if _, played := e.used[word]; !played {
			moves = append(moves, word)
		}
	}
	return moves
}

// Suggestions lists up to n words the player to act could still play, in a
// stable order so the same position always answers the same way.
//
// It is what a player who has just lost is shown, which is also why an empty
// result carries information: the position was a dead end, and nothing they
// could have typed would have answered it.
func (e *Engine) Suggestions(n int) []string {
	if n <= 0 {
		return nil
	}
	moves := e.LegalMoves()
	slices.Sort(moves)
	return moves[:min(n, len(moves))]
}

// HasLegalMove reports whether the player to act has anything to play. It stops
// at the first unused candidate instead of building the whole list.
func (e *Engine) HasLegalMove() bool {
	for word := range e.dict.WordsStartingWith(e.current) {
		if _, played := e.used[word]; !played {
			return true
		}
	}
	return false
}

// IsExpired reports whether the current turn's deadline has passed.
func (e *Engine) IsExpired(now time.Time) bool {
	return now.After(e.deadline)
}

// Timeout ends the game against the player whose turn expired. The caller
// drives this from its own timer; the engine never reads the clock itself.
func (e *Engine) Timeout(now time.Time) bool {
	if e.over || !e.IsExpired(now) {
		return false
	}
	e.expire()
	return true
}

// NoMove ends the game against the player to act when the position leaves
// them nothing to play, without waiting for their clock to run out.
//
// Only for a player who has no clock to wait for — the bot answers the moment
// it has searched, and making it sit out a turn limit it cannot use would
// stall the room. A human keeps their turn: see Submit.
func (e *Engine) NoMove() bool {
	if e.over || e.HasLegalMove() {
		return false
	}
	e.finish(e.opponentOf(e.Turn()), EndNoLegalMove)
	return true
}

// expire ends the game against the player to act, whose turn has run out.
//
// A player who never had a word to play did not run out of thinking time:
// there was nothing to think about, and reporting a timeout would blame them
// for a position nobody could have answered.
func (e *Engine) expire() {
	reason := EndTimeout
	if !e.HasLegalMove() {
		reason = EndNoLegalMove
	}
	e.finish(e.opponentOf(e.Turn()), reason)
}

// Resign ends the game against the player who gave up.
func (e *Engine) Resign(p PlayerID) bool {
	if e.over {
		return false
	}
	e.finish(e.opponentOf(p), EndResigned)
	return true
}

func (e *Engine) finish(winner PlayerID, reason EndReason) {
	e.over = true
	e.winner = winner
	e.endReason = reason
}

// opponentOf returns the other player. With more than two seats it returns the
// next one, which keeps the two-player case exact and the rest sane.
func (e *Engine) opponentOf(p PlayerID) PlayerID {
	for i, candidate := range e.players {
		if candidate == p {
			return e.players[(i+1)%len(e.players)]
		}
	}
	return p
}

// Used reports whether a canonical word has already been played.
func (e *Engine) Used(word string) bool {
	_, played := e.used[word]
	return played
}

// Snapshot copies the observable state for the transport layer.
func (e *Engine) Snapshot() State {
	scores := make(map[PlayerID]int, len(e.scores))
	for p, s := range e.scores {
		scores[p] = s
	}

	return State{
		Current:     e.current,
		Turn:        e.Turn(),
		Deadline:    e.deadline,
		History:     append([]Move{}, e.history...),
		Scores:      scores,
		ChainLength: e.ChainLength(),
		Over:        e.over,
		Winner:      e.winner,
		EndReason:   e.endReason,
	}
}
