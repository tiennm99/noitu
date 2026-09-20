package game

import (
	"iter"
	"time"
)

// PlayerID identifies a seat at the table. The engine never learns anything
// else about a player: no name, no connection, no session.
type PlayerID string

// Dictionary is the slice of the word store the engine needs.
//
// An interface rather than *dictionary.Store so the engine can be tested
// against a hand-built word graph with no SQLite involved, and so the bot's
// search can be exercised on boards small enough to reason about.
type Dictionary interface {
	// Resolve maps a normalized word to its canonical form.
	Resolve(word string) (string, bool)
	// FirstSyllable and LastSyllable report the ends of a canonical word.
	// The engine must use these rather than splitting the player's input:
	// canonicalization can move either end.
	FirstSyllable(word string) (string, bool)
	LastSyllable(word string) (string, bool)
	// WordsStartingWith iterates the words that may follow a syllable.
	WordsStartingWith(syllable string) iter.Seq[string]
	// OutDegree reports how many words start with a syllable.
	OutDegree(syllable string) (int, error)
}

// RejectReason says why a submission was not accepted. Callers map these to
// player-facing messages, so each one has to be specific enough to act on.
type RejectReason int

const (
	ReasonNone RejectReason = iota
	ReasonNotYourTurn
	ReasonTooFewSyllables
	ReasonNotInDictionary
	ReasonWrongLink
	ReasonAlreadyUsed
	ReasonTimeout
	ReasonGameOver

	// NumRejectReasons is one past the last defined reason.
	//
	// It exists so a transport layer can prove it maps every reason without
	// guessing where the list ends. Deriving that from String returning
	// "unknown" would be circular: a reason added without a String case would
	// shrink the range being checked and the check would still pass.
	NumRejectReasons
)

func (r RejectReason) String() string {
	switch r {
	case ReasonNone:
		return "accepted"
	case ReasonNotYourTurn:
		return "not your turn"
	case ReasonTooFewSyllables:
		return "fewer than two syllables"
	case ReasonNotInDictionary:
		return "not in dictionary"
	case ReasonWrongLink:
		return "wrong first syllable"
	case ReasonAlreadyUsed:
		return "already used"
	case ReasonTimeout:
		return "turn expired"
	case ReasonGameOver:
		return "game already over"
	}
	return "unknown"
}

// Move is one accepted word.
//
// Word is always the canonical spelling, which may differ from what the player
// typed. Typed records the raw input so the UI can show that a correction
// happened rather than silently replacing the player's text.
type Move struct {
	Player    PlayerID
	Word      string
	Typed     string
	First     string
	Last      string
	Syllables int
	Points    int
	// Parts is how Points was arrived at: one entry per non-zero term, summing
	// exactly to Points even when the maxPointsPerWord cap trimmed them. See
	// pointsFor.
	Parts []PointPart
	At    time.Time
}

// PointKind names one term of a word's score. PointKindNone is the zero value
// and never appears in a PointPart — it exists only so the wire mapping in
// wsapi/convert.go has somewhere unreachable to send an unmapped value, the
// same shape RejectReason and EndReason already use.
type PointKind int

const (
	PointKindNone PointKind = iota
	PointKindBase
	PointKindChain
	PointKindSyllables
	PointKindSpeed
	PointKindRarity

	// NumPointKinds is one past the last defined kind. See NumRejectReasons
	// for why the count is a constant rather than a walk.
	NumPointKinds
)

func (k PointKind) String() string {
	switch k {
	case PointKindNone:
		return "none"
	case PointKindBase:
		return "base"
	case PointKindChain:
		return "chain"
	case PointKindSyllables:
		return "syllables"
	case PointKindSpeed:
		return "speed"
	case PointKindRarity:
		return "rarity"
	}
	return "unknown"
}

// PointPart is one named term of a word's score: how many points it
// contributed, and which of pointsFor's terms it was.
type PointPart struct {
	Kind  PointKind
	Value int
}

// EndReason says how a finished game ended.
type EndReason int

const (
	EndNone EndReason = iota
	EndTimeout
	EndNoLegalMove
	EndResigned

	// NumEndReasons is one past the last defined end reason. See
	// NumRejectReasons for why the count is a constant rather than a walk.
	NumEndReasons
)

func (r EndReason) String() string {
	switch r {
	case EndNone:
		return "in play"
	case EndTimeout:
		return "timeout"
	case EndNoLegalMove:
		return "no legal move"
	case EndResigned:
		return "resigned"
	}
	return "unknown"
}

// Standing is one player's final placing.
//
// Rank 1 is whoever was still standing when the game ended; below them, a
// player eliminated later ranks above one eliminated earlier. Outlasting
// somebody is what beats them, so the ranking is finishing order and the score
// is reported beside it rather than deciding it.
type Standing struct {
	Player PlayerID
	Score  int
	Rank   int
	// Reason is how this player left the game, and EndNone for the winner,
	// who did not.
	Reason EndReason
}

// State is a snapshot for the transport layer to render. It copies everything
// it exposes, so a caller can hold it without touching engine state.
type State struct {
	Current  string
	Turn     PlayerID
	Deadline time.Time
	History  []Move
	Scores   map[PlayerID]int
	// Alive says who is still in the game. A player who has been eliminated
	// keeps their score and their place in the history; they simply no longer
	// get a turn.
	Alive map[PlayerID]bool
	// Eliminated is the order players went out, first out first. A caller that
	// remembers its length can tell exactly who went out on the last input.
	Eliminated  []PlayerID
	ChainLength int
	Over        bool
	Winner      PlayerID
	EndReason   EndReason
	// Standings is the final table, best first. Meaningless while the game is
	// in play, for the same reason Winner is.
	Standings []Standing
}
