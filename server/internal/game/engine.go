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
	"math/bits"
	"slices"
	"time"

	"github.com/tiennm99dev/noitu/server/internal/vietnamese"
)

// Scoring. A word is worth more the longer the chain it extends, the longer
// the word itself, the faster it was played, and the fewer words the corpus
// offered for the syllable it answered. Between them the four terms reward
// reaching for three- and four-syllable compounds, answering without stalling
// on the clock, and knowing a word for a syllable almost nothing follows.
const (
	basePoints = 10
	chainBonus = 2 // per word already played
	// chainBonusWords caps how much of the chain the chain term counts.
	// Uncapped it grows without bound, so a player's total would grow with the
	// square of how long they survived: length would drown the three terms
	// that reward the move itself, and every word late in a long game would
	// land on maxPointsPerWord with nothing to tell two of them apart.
	chainBonusWords = 15
	syllableBonus   = 5 // per syllable beyond the minimum
	// speedBonus is paid in full for an answer that arrives instantly and
	// falls linearly to nothing for one that arrives on the buzzer.
	speedBonus = 10
	// rarityBonus is paid in full for a syllable the corpus answers with a
	// single word and loses rarityHalvingPenalty for each doubling of the
	// answers available, so it is spent by 32.
	//
	// A ladder rather than a straight line because option counts are heavy
	// tailed: in the shipped corpus the syllable a player is handed has a
	// median of 14 answers and a maximum of 195, and going from one answer to
	// two is the whole of what "rare" means to a player while going from 100
	// to 200 is nothing they can feel.
	rarityBonus          = 15
	rarityHalvingPenalty = 3
	maxPointsPerWord     = 100
)

// Engine holds one game of two or more players.
//
// A player who fails their turn is eliminated and the rest play on from the
// same syllable; the game ends when one of them is left. Two seats is that
// same rule seen from close up, which is why there is one implementation of it
// and not two.
//
// Not safe for concurrent use. Exactly one goroutine owns an Engine — in the
// server that is the room goroutine, which serializes every input through a
// single channel.
type Engine struct {
	dict    Dictionary
	players []PlayerID
	// alive is parallel to players. Eliminating somebody clears their flag
	// rather than dropping them from the slice: their score, their words and
	// their place in turn order all have to survive them.
	alive  []bool
	aliveN int
	// outOrder is who went out, first out first, and outReason is why. Between
	// them they are the whole final table — a rank is a position in this list
	// read backwards, so nothing has to be recomputed to report one.
	outOrder  []PlayerID
	outReason map[PlayerID]EndReason
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
		alive:     make([]bool, len(players)),
		aliveN:    len(players),
		outReason: make(map[PlayerID]EndReason, len(players)),
		used:      map[string]struct{}{canonical: {}},
		current:   last,
		turnLimit: turnLimit,
		deadline:  now.Add(turnLimit),
		scores:    make(map[PlayerID]int, len(players)),
	}
	for i, p := range players {
		e.alive[i] = true
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

// Turn reports whose move it is. Always somebody still in the game, and once
// the last elimination has landed it is the winner.
func (e *Engine) Turn() PlayerID { return e.players[e.turnIndex] }

// Players reports the seats in turn order, eliminated ones included.
func (e *Engine) Players() []PlayerID { return append([]PlayerID{}, e.players...) }

// Alive reports whether a player is still in the game.
func (e *Engine) Alive(p PlayerID) bool {
	i := e.indexOf(p)
	return i >= 0 && e.alive[i]
}

// Score reports one player's points.
func (e *Engine) Score(p PlayerID) int { return e.scores[p] }

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
		e.expire(now)
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
		Points:    e.pointsFor(len(syllables), first, now),
		At:        now,
	}

	e.used[canonical] = struct{}{}
	e.history = append(e.history, move)
	e.scores[p] += move.Points
	e.current = last
	e.advance()
	e.deadline = now.Add(e.turnLimit)

	// A dead end is deliberately not the end of the game. Ending it here would
	// hand the mover a win the moment the position closed, before the other
	// player had seen the board at all — they get their turn, and lose it to
	// the clock like any other they cannot answer. NoMove lets a caller who
	// has nothing to wait for (the bot) settle it immediately instead.

	return move, ReasonNone
}

// pointsFor scores a word about to be played. The chain term counts the words
// already down, opening word included, which is what ChainLength reports; link
// is the syllable the word answers, and now is when it was played, so both the
// speed and the rarity term have to be read before the move is applied.
func (e *Engine) pointsFor(syllables int, link string, now time.Time) int {
	points := basePoints +
		chainBonus*min(e.ChainLength(), chainBonusWords) +
		syllableBonus*(syllables-vietnamese.MinSyllables) +
		e.speedPoints(now) +
		e.rarityPoints(link)
	return min(points, maxPointsPerWord)
}

// speedPoints pays for the share of the turn the player left on the clock.
//
// Called from Submit after the expiry check, so the deadline is still this
// player's and has not passed; the clamps only keep an unexpired-but-late
// answer or a caller's clock skew from turning into negative or excess points.
func (e *Engine) speedPoints(now time.Time) int {
	remaining := e.deadline.Sub(now)
	if remaining <= 0 {
		return 0
	}
	if remaining > e.turnLimit {
		remaining = e.turnLimit
	}
	return int(int64(speedBonus) * int64(remaining) / int64(e.turnLimit))
}

// rarityPoints pays for how little the corpus offers for the syllable the word
// answers. It counts every word on that link, spent ones included: the reward
// is for knowing a word where the language has few, which is a property of the
// dictionary and not of how far this particular game has run them down.
//
// An unknown syllable scores nothing rather than the maximum. The link was
// just answered, so the corpus does hold a word for it; a dictionary that
// cannot count them is a dictionary that cannot price rarity.
func (e *Engine) rarityPoints(link string) int {
	options, err := e.dict.OutDegree(link)
	if err != nil || options < 1 {
		return 0
	}
	// bits.Len(1) is 1, so this is how many times the count has doubled past
	// the single answer that pays in full.
	halvings := bits.Len(uint(options)) - 1
	return max(rarityBonus-rarityHalvingPenalty*halvings, 0)
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

// Timeout eliminates the player whose turn expired. The caller drives this
// from its own timer; the engine never reads the clock itself.
//
// It reports that something happened, not that the game ended: past two seats
// a timeout usually just moves the turn on. Over answers the other question.
func (e *Engine) Timeout(now time.Time) bool {
	if e.over || !e.IsExpired(now) {
		return false
	}
	e.expire(now)
	return true
}

// NoMove eliminates the player to act when the position leaves them nothing to
// play, without waiting for their clock to run out.
//
// Only for a player who has no clock to wait for — the bot answers the moment
// it has searched, and making it sit out a turn limit it cannot use would
// stall the room. A human keeps their turn: see Submit.
func (e *Engine) NoMove(now time.Time) bool {
	if e.over || e.HasLegalMove() {
		return false
	}
	e.expire(now)
	return true
}

// expire ends the current turn against the player holding it.
//
// A player who never had a word to play did not run out of thinking time:
// there was nothing to think about, and reporting a timeout would blame them
// for a position nobody could have answered.
func (e *Engine) expire(now time.Time) {
	reason := EndTimeout
	if !e.HasLegalMove() {
		reason = EndNoLegalMove
	}
	e.eliminate(e.Turn(), reason)
	e.settle()
	if !e.over {
		e.deadline = now.Add(e.turnLimit)
	}
}

// settle clears out everybody a dead end leaves with nothing.
//
// The first player to face one still loses it on their own clock — they get
// their turn, for the reason Submit gives. Everyone behind them has already
// seen that board, so making each of them sit out a full turn limit they
// cannot use would add minutes of nothing to a game that is already decided.
// Going out together instead leaves the player who closed the position
// standing, which is exactly what two players get.
func (e *Engine) settle() {
	for !e.over && !e.HasLegalMove() {
		e.eliminate(e.Turn(), EndNoLegalMove)
	}
}

// Resign eliminates the player who stopped playing.
//
// It accepts a player who is not to act, because it is the engine's only shape
// for a seat that leaves a game — one whose player walked out of the room, or
// whose reconnect window ran out, neither of which waits for their turn. A
// resignation a player asked for is the transport's own rule: only the player
// to act may spend one. The clock restarts only when the elimination actually
// moved the turn on, so a seat going out from behind cannot hand the player to
// act more time than they had.
func (e *Engine) Resign(p PlayerID, now time.Time) bool {
	if e.over {
		return false
	}
	before := e.Turn()
	if !e.eliminate(p, EndResigned) {
		return false
	}
	e.settle()
	if !e.over && e.Turn() != before {
		e.deadline = now.Add(e.turnLimit)
	}
	return true
}

// eliminate takes one player out and ends the game when one is left.
//
// The seat stays in players. An eliminated player keeps their score and the
// words they played, and the transport layer still has them to render — being
// out of the game is not being out of the room.
func (e *Engine) eliminate(p PlayerID, reason EndReason) bool {
	i := e.indexOf(p)
	if i < 0 || !e.alive[i] {
		return false
	}

	e.alive[i] = false
	e.aliveN--
	e.outOrder = append(e.outOrder, p)
	e.outReason[p] = reason
	// The game-level reason is the latest elimination's, which with two seats
	// is the only one there ever was.
	e.endReason = reason

	if e.turnIndex == i {
		e.advance()
	}
	if e.aliveN <= 1 {
		e.over = true
		e.winner = e.players[e.turnIndex]
	}
	return true
}

// advance moves the turn to the next player still in the game.
func (e *Engine) advance() {
	for range e.players {
		e.turnIndex = (e.turnIndex + 1) % len(e.players)
		if e.alive[e.turnIndex] {
			return
		}
	}
}

func (e *Engine) indexOf(p PlayerID) int {
	for i, candidate := range e.players {
		if candidate == p {
			return i
		}
	}
	return -1
}

// EliminatedCount is how many players have gone out. A caller that remembers
// it across an input can tell exactly who that input knocked out.
func (e *Engine) EliminatedCount() int { return len(e.outOrder) }

// OutReason reports how a player left the game, and EndNone for one who has
// not. The transport layer needs it per player: with several seats, "why the
// game ended" and "why this player went out" stop being the same question.
func (e *Engine) OutReason(p PlayerID) EndReason { return e.outReason[p] }

// Standings is the final table, best first. Meaningless while the game is in
// play, for the same reason Winner is.
func (e *Engine) Standings() []Standing {
	out := make([]Standing, 0, len(e.players))
	if e.winner != "" {
		out = append(out, Standing{Player: e.winner, Score: e.scores[e.winner], Rank: 1, Reason: EndNone})
	}
	// Read backwards: outlasting somebody is what beats them, so of the players
	// who went out the last one to go placed highest.
	for i := len(e.outOrder) - 1; i >= 0; i-- {
		p := e.outOrder[i]
		out = append(out, Standing{
			Player: p,
			Score:  e.scores[p],
			Rank:   len(out) + 1,
			Reason: e.outReason[p],
		})
	}
	return out
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

	alive := make(map[PlayerID]bool, len(e.players))
	for i, p := range e.players {
		alive[p] = e.alive[i]
	}

	return State{
		Current:     e.current,
		Turn:        e.Turn(),
		Deadline:    e.deadline,
		History:     append([]Move{}, e.history...),
		Scores:      scores,
		Alive:       alive,
		Eliminated:  append([]PlayerID{}, e.outOrder...),
		ChainLength: e.ChainLength(),
		Over:        e.over,
		Winner:      e.winner,
		EndReason:   e.endReason,
		Standings:   e.Standings(),
	}
}
