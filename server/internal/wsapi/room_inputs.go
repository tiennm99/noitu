package wsapi

import (
	"github.com/tiennm99dev/noitu/server/internal/bot"
	"github.com/tiennm99dev/noitu/server/internal/game"
)

// Room input messages. Everything that can change a room or a game arrives
// as one of these on room.inputs, which is what makes the engine safe
// without a lock: the room goroutine is its only reader.

// createInput and startBotInput seat the first player. Seating is a message
// rather than a direct write so that every touch of room state — seats and
// engine alike — happens on the room goroutine, which makes the ownership
// invariant provable by reading run() rather than by reasoning about which
// writes happened before `go r.run()`.
type createInput struct {
	sess *session
	// autoStart marks a room a quick match opened rather than a player asking
	// for a code: once both seats are filled and connected, the room begins
	// its own first game instead of waiting on readiness and StartGame.
	autoStart bool
}

type startBotInput struct {
	sess       *session
	difficulty bot.Difficulty
}

type joinInput struct {
	sess *session
}

// submitInput and resignInput carry the connection that sent them, not just
// the seat it claims. A room code is a shared secret — it is pasted into group
// chats by design — so holding one must not be enough to act as a player who
// is already seated.
type submitInput struct {
	sess    *session
	player  game.PlayerID
	word    string
	turnSeq uint32
}

// lobbyAction is one thing a player does to the room rather than to a game.
type lobbyAction uint8

const (
	lobbyReady lobbyAction = iota
	lobbyStart
	lobbyKick
	lobbyLeave
)

// lobbyInput is one lobby action. They share a type because they share every
// authorization step — the seat, the room's mode, and whether a game is
// running — and splitting them would mean four copies of those checks.
type lobbyInput struct {
	sess   *session
	player game.PlayerID
	action lobbyAction
	// ready is the value a lobbyReady is setting. Explicit rather than a
	// toggle: a toggle applied to a state the client is a frame behind on sets
	// the opposite of what the player clicked.
	ready bool
	// target is the seat a lobbyKick names. A room holds up to four people, so
	// "the other one" stopped being an answer.
	target game.PlayerID
}

// chatInput is one line of text from a seated player. It carries the
// connection, not just the seat it claims, for the same reason submitInput
// does: a room code is a shared secret, and a connection the room has retired
// must not be able to speak as the seat it used to hold.
type chatInput struct {
	sess   *session
	player game.PlayerID
	text   string
}

type resignInput struct {
	sess   *session
	player game.PlayerID
}

// claimDeadEndInput is the player to act saying the syllable has no answer
// left. Carries the connection, not just the claimed seat, for the same
// reason resignInput does.
type claimDeadEndInput struct {
	sess   *session
	player game.PlayerID
}

// reportWordInput is a word the session has already validated as reportable —
// long enough, and within its own per-session cap — waiting only on the room
// for the context a report is logged with: the syllable in play, if any.
type reportWordInput struct {
	sess *session
	word string
}

type disconnectInput struct {
	player game.PlayerID
	// sess identifies which connection dropped. A player who already
	// reconnected has a different session, and that stale notice must not
	// evict the seat the new connection just took.
	sess *session
}

type resumeInput struct {
	player game.PlayerID
	sess   *session
	// prior is the connection being replaced. The room retires it only once it
	// has decided the resume is allowed, because closing it on a refusal would
	// end the very game the client was trying to rejoin.
	prior *session
}

type botMoveInput struct {
	word string
	err  error
	// turnSeq the bot was thinking about. If the game moved on — a resign
	// landed while it thought — the move is stale and dropped.
	turnSeq uint32
}
