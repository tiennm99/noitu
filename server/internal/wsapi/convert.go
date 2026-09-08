// Package wsapi is the transport layer: it translates between the game engine
// and the generated protobuf wire types, and owns the WebSocket sessions and
// rooms built on top of them.
//
// The engine's reason enums and the wire enums are deliberately distinct
// types. Renumbering an internal constant must never silently change what a
// deployed client decodes, so every crossing goes through an explicit switch
// here rather than a cast.
package wsapi

import (
	"log"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/bot"
	"github.com/tiennm99dev/noitu/server/internal/dictionary"
	"github.com/tiennm99dev/noitu/server/internal/game"
)

// ProtocolVersion is the version this build speaks, sent in Welcome and
// expected in Hello.
//
// It is a whole-protocol number, not a per-message one: the wire contract only
// ever grows by addition, so a client and server that agree on this integer
// agree on everything they both know how to decode. Bump it when a change
// would make an older client misread a frame rather than merely ignore an
// unknown field.
const ProtocolVersion uint32 = 2

// RejectReason maps an engine rejection onto the wire enum.
//
// game.ReasonNone means the move was accepted and so has no wire counterpart;
// it maps to UNSPECIFIED, which callers must not put in a MoveRejected. Any
// other unmapped value is a bug — a reason was added to the engine without
// extending the contract — so it is logged rather than quietly flattened.
func RejectReason(r game.RejectReason) noituv1.RejectReason {
	switch r {
	case game.ReasonNone:
		return noituv1.RejectReason_REJECT_REASON_UNSPECIFIED
	case game.ReasonNotYourTurn:
		return noituv1.RejectReason_REJECT_REASON_NOT_YOUR_TURN
	case game.ReasonTooFewSyllables:
		return noituv1.RejectReason_REJECT_REASON_TOO_FEW_SYLLABLES
	case game.ReasonNotInDictionary:
		return noituv1.RejectReason_REJECT_REASON_NOT_IN_DICTIONARY
	case game.ReasonWrongLink:
		return noituv1.RejectReason_REJECT_REASON_WRONG_LINK
	case game.ReasonAlreadyUsed:
		return noituv1.RejectReason_REJECT_REASON_ALREADY_USED
	case game.ReasonTimeout:
		return noituv1.RejectReason_REJECT_REASON_TIMEOUT
	case game.ReasonGameOver:
		return noituv1.RejectReason_REJECT_REASON_GAME_OVER
	}
	log.Printf("wsapi: no wire mapping for game.RejectReason(%d) %q", int(r), r)
	return noituv1.RejectReason_REJECT_REASON_UNSPECIFIED
}

// EndReason maps an engine end condition onto the wire enum.
//
// GAME_END_REASON_OPPONENT_LEFT has no engine counterpart on purpose: a player
// disconnecting is a transport event, not a rule, so the room emits that value
// directly and the engine never learns about it.
func EndReason(r game.EndReason) noituv1.GameEndReason {
	switch r {
	case game.EndNone:
		return noituv1.GameEndReason_GAME_END_REASON_UNSPECIFIED
	case game.EndTimeout:
		return noituv1.GameEndReason_GAME_END_REASON_TIMEOUT
	case game.EndNoLegalMove:
		return noituv1.GameEndReason_GAME_END_REASON_NO_LEGAL_MOVE
	case game.EndResigned:
		return noituv1.GameEndReason_GAME_END_REASON_RESIGNED
	}
	log.Printf("wsapi: no wire mapping for game.EndReason(%d) %q", int(r), r)
	return noituv1.GameEndReason_GAME_END_REASON_UNSPECIFIED
}

// Difficulty maps a wire difficulty onto a bot strategy selector.
//
// This one runs client to server, so an unrecognized value is untrusted input
// rather than a bug: it is not logged, and the caller decides what to do with
// the false.
func Difficulty(d noituv1.Difficulty) (bot.Difficulty, bool) {
	switch d {
	case noituv1.Difficulty_DIFFICULTY_EASY:
		return bot.Easy, true
	case noituv1.Difficulty_DIFFICULTY_MEDIUM:
		return bot.Medium, true
	case noituv1.Difficulty_DIFFICULTY_HARD:
		return bot.Hard, true
	}
	return 0, false
}

// There is deliberately no server-to-client difficulty mapping: no message in
// the schema carries a Difficulty, so a client learns the difficulty only from
// the one it asked for.

// PlayedWord renders an accepted move for one recipient.
//
// byMe is the caller's business: the same move is sent to both players and
// only this flag differs, so the room serializes one message per player rather
// than broadcasting a single shared frame. meanings is the word's senses from
// the dictionary, the same for every recipient; the room looks them up once
// per move and passes them in.
func PlayedWord(m game.Move, byMe bool, meanings []dictionary.Sense) *noituv1.PlayedWord {
	return &noituv1.PlayedWord{
		Word:      m.Word,
		Typed:     m.Typed,
		ByMe:      byMe,
		Points:    uint32(m.Points),
		Syllables: uint32(m.Syllables),
		Meanings:  Senses(meanings),
	}
}

// Senses renders a word's dictionary senses for the wire. A word with none
// yields an empty list, which the client shows as "no meaning yet" rather than
// as a row without a panel.
func Senses(senses []dictionary.Sense) []*noituv1.Sense {
	out := make([]*noituv1.Sense, 0, len(senses))
	for _, s := range senses {
		out = append(out, &noituv1.Sense{Pos: s.Pos, Gloss: s.Gloss})
	}
	return out
}
