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
	At        time.Time
}

// EndReason says how a finished game ended.
type EndReason int

const (
	EndNone EndReason = iota
	EndTimeout
	EndNoLegalMove
	EndResigned
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

// State is a snapshot for the transport layer to render. It copies everything
// it exposes, so a caller can hold it without touching engine state.
type State struct {
	Current     string
	Turn        PlayerID
	Deadline    time.Time
	History     []Move
	Scores      map[PlayerID]int
	ChainLength int
	Over        bool
	Winner      PlayerID
	EndReason   EndReason
}
