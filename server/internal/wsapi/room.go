package wsapi

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/bot"
	"github.com/tiennm99dev/noitu/server/internal/dictionary"
	"github.com/tiennm99dev/noitu/server/internal/game"
)

// This file holds the room's core type, its constructor, its input loop,
// and the small seat-authority helpers every other file in this package
// reads. Everything that only ever runs on the room goroutine still lives
// wherever the review's file split put it (room_lobby.go, room_game.go,
// room_presence.go, room_chat.go, bot_board.go) — this is a file boundary,
// not a change to who may touch a *room.

// roomModeBot and roomModePvP are the two values a room's mode ever takes.
// They double as the label under which every mode-keyed metric and the
// word_rejected log line group their counts, so a reader checking one against
// the other is checking against the same string everywhere.
const (
	roomModeBot = "bot"
	roomModePvP = "pvp"
)

// botPlayerID is the seat the bot occupies. It is a normal player to the
// engine, which is the whole point: the bot's moves go through the same
// validation as a human's, so there is one rule implementation rather than two.
const botPlayerID game.PlayerID = "bot"

// maxPlayers is how many seats a room has, and minPlayers how many it takes
// to start one. Both are sent to the client in RoomState rather than compiled
// into it, so the lobby draws whatever the server allows and widening a room
// is a server change alone.
const (
	maxPlayers = 4
	minPlayers = 2
)

// minOpeningOutDegree keeps the first word from being a dead end. Opening on a
// syllable with two continuations makes for a game that ends before it starts.
const minOpeningOutDegree = 20

// maxSuggestions is how many of the words still playable a losing player is
// shown. Enough to see what the position wanted, few enough that it reads as
// a hint rather than a dump of the dictionary.
const maxSuggestions = 3

// chatHistoryLimit is how many messages a room keeps, and the same window the
// client holds. Enough to catch up on after a reload, few enough that a room
// that lives all day cannot grow.
const chatHistoryLimit = 20

// maxChatRunes caps one message, counted in runes for the reason
// maxNicknameRunes is.
const maxChatRunes = 200

// maxWordRunes caps a submitted word before the engine sees it. The longest
// dictionary entries are well under this, so it bounds abuse without ever
// deciding a real move.
const maxWordRunes = 64

// maxChatMarks caps mark stacking in a message, as maxNicknameMarks does for a
// name. A message is ten times longer, so the same stack does ten times more
// damage.
const maxChatMarks = 2

// roomInputCap buffers the room's inbox. A sender that finds it full is either
// flooding past the rate limiter or racing a room that is shutting down;
// neither is worth blocking a session goroutine for.
const roomInputCap = 32

// defaultIdleWindow is how long a lobby nobody starts a game in stays open
// when nothing else is configured.
//
// A room now outlives its games, so something has to bound it: without this
// one open tab holds a goroutine and a room code for the life of the process.
// Long enough to read an invite and talk about it, short enough that abandoned
// rooms do not accumulate. A running game needs no such bound — the turn clock
// already ends it.
const defaultIdleWindow = 10 * time.Minute

// seat is one side of a game.
type seat struct {
	id       game.PlayerID
	nickname string
	sess     *session // nil for the bot, or while a human is disconnected
	// ready is this seat's declaration that it wants the next game to start.
	// Only ever set on the guest's seat: the owner's readiness is StartGame
	// itself. Cleared whenever a game begins, so every game is agreed again.
	ready bool
	// chatFrom is where the room's conversation stood when this seat was
	// filled. A replay starts there, which is what keeps a stranger who walks
	// in with the code from being handed what the last two people said.
	chatFrom uint64
	// wins counts the games this seat has taken since it was filled. A room
	// outlives its games, so a running tally has to live on something that
	// does too; the seat is the shortest-lived thing that still spans them,
	// and vacating it is exactly when the tally stops meaning one player.
	wins uint32
	// graceUntil is when this seat stops being held for the player who dropped
	// out of it, and zero while they are connected. Per seat rather than per
	// room because any number of them can be waiting at once.
	graceUntil time.Time
}

// room owns one game.
//
// Every field below is touched only by the room goroutine after start. The
// exceptions are inputs and ctx, which exist precisely to be used from outside.
type room struct {
	code   string
	inputs chan any
	ctx    context.Context
	cancel context.CancelFunc

	hub       *hub
	dict      Dictionary
	engine    *game.Engine
	opening   string
	strategy  bot.Strategy
	turnLimit time.Duration
	graceFor  time.Duration
	idleFor   time.Duration

	// mode is roomModeBot or roomModePvP, fixed at creation. It is the label
	// every mode-keyed metric and the word_rejected log line use, kept as its
	// own field rather than re-derived from strategy == nil so the hub can set
	// it before the room goroutine has seated anyone or built an engine.
	mode string

	// liveCounted mirrors whether this room's game is the one hub.liveGames is
	// currently counting. Atomic rather than plain, because drain reads
	// hub.liveGameCount() from outside the room goroutine while this flips on
	// the goroutine itself; the CompareAndSwap in run's teardown is what
	// guarantees exactly one hub.gameFinished() per hub.gameStarted() even when
	// the room is cancelled mid-game instead of finishing normally.
	liveCounted atomic.Bool

	// autoStart marks a room opened by a quick match. Once both seats are
	// filled and connected it begins its own first game — see handleJoin —
	// and is cleared right there, so every later game in the room is agreed
	// with readiness and StartGame like any other.
	autoStart bool

	seats [maxPlayers]*seat

	// owner is the seat that may start a game and free the other one. It is a
	// field rather than "seats[0]" because the role outlives the player who
	// held it: an owner who leaves hands it to whoever is still here, and the
	// seat they vacate is then filled by an ordinary guest.
	owner game.PlayerID

	// turnSeq increments on every turn change. A client stamps its submission
	// with the sequence it was answering, so a move that crosses the deadline
	// is identifiable rather than silently applied to the next turn.
	turnSeq uint32

	// outWire overrides how one player's elimination is reported, for the
	// cases the engine cannot know about. A reconnect window running out is
	// the only one: to the engine that is a resignation, and to the other
	// players it is somebody who left.
	outWire map[game.PlayerID]noituv1.GameEndReason

	// chat is the room's recent conversation, oldest first, capped at
	// chatHistoryLimit. It belongs to the room, so it outlives each game and
	// dies only with the room itself.
	chat []chatEntry
	// chatSeq counts every message the room has accepted, ever. It keeps
	// rising as the history is trimmed, which is what makes a seat's chatFrom
	// meaningful after the entry it pointed at has been dropped.
	chatSeq uint64

	// lobbyChanged marks that something a player can see about the room's
	// occupants has changed: a seat filled or freed, a readiness set, an owner
	// promoted, a game finished. The run loop turns it into exactly one
	// RoomState broadcast per input, which is why no handler has to remember
	// to send one.
	lobbyChanged bool
}

// Dictionary is everything the transport layer needs from the wordlist: the
// engine's own contract, a way to pick an opening, and the meanings a word
// travels to the client with. The engine never sees a meaning; only the room
// attaches them, where it renders a word for a recipient.
//
// An interface rather than *dictionary.Store so a test can play a whole game
// against a hand-built graph of a dozen words, where the expected outcome is
// something a reader can verify by eye. *dictionary.Store satisfies it as
// written.
type Dictionary interface {
	game.Dictionary
	RandomOpeningWord(minOutDegree int) (string, error)
	// Meanings returns a canonical word's senses in order, nil for none.
	Meanings(word string) []dictionary.Sense
	// NearMiss finds the one real word a normalized submission differs from by
	// diacritics alone, reported only when exactly one such word exists.
	NearMiss(normalized string) (string, bool)
}

func newRoom(h *hub, code string, turnLimit, graceFor, idleFor time.Duration, mode string) *room {
	if idleFor <= 0 {
		idleFor = defaultIdleWindow
	}
	ctx, cancel := context.WithCancel(h.ctx)
	return &room{
		code:      code,
		inputs:    make(chan any, roomInputCap),
		ctx:       ctx,
		cancel:    cancel,
		hub:       h,
		dict:      h.dict,
		turnLimit: turnLimit,
		graceFor:  graceFor,
		idleFor:   idleFor,
		mode:      mode,
	}
}

// send hands a message to the room without ever blocking the caller.
//
// A session goroutine must not be able to stall on a room: that would let one
// wedged game hold a connection open with no way out. A dropped message is
// recoverable — the client retries or the game times out — while a deadlock is
// not.
func (r *room) send(msg any) bool {
	// Check for a finished room first, on its own. Folding this into the
	// select below would make it a coin flip: the buffered channel and the
	// done channel are both ready, so select picks at random and half the
	// sends into a dead room report success. The caller then believes the
	// message is on its way to a goroutine that stopped reading, and whoever
	// was waiting for the reply waits forever.
	select {
	case <-r.ctx.Done():
		return false
	default:
	}

	select {
	case r.inputs <- msg:
		return true
	case <-r.ctx.Done():
		return false
	default:
		slog.Warn("room inbox full, dropping message", "room", r.code)
		return false
	}
}

// run is the room goroutine. It is the only place the engine is touched.
func (r *room) run() {
	defer r.cancel()
	defer r.hub.evict(r.code)
	defer metrics.roomsLive.Add(r.mode, -1)
	// Catches a room cancelled with a game still running — drain forcing the
	// last stragglers closed, or a shutdown mid-game — which never reaches
	// broadcastGameOver's own decrement.
	defer func() {
		if r.liveCounted.CompareAndSwap(true, false) {
			r.hub.gameFinished()
		}
	}()
	// Whatever ended the room — everybody leaving, the idle window, a server
	// shutdown — the connections still seated in it must stop pointing here.
	// A session that keeps a dead room would answer every later action with
	// "not in a room" and could never be seated anywhere else cleanly.
	defer r.detachAll()

	var turnTimer, graceTimer, idleTimer *time.Timer
	stop := func(t *time.Timer) {
		if t != nil {
			t.Stop()
		}
	}
	defer func() {
		stop(turnTimer)
		stop(graceTimer)
		stop(idleTimer)
	}()

	// resetTurnTimer rebuilds the deadline timer after anything that changes
	// whose turn it is. Recreating rather than resetting sidesteps the drain
	// problem entirely: a stopped timer's stale fire can never reach the
	// select because that channel is no longer the one being read.
	resetTurnTimer := func() {
		stop(turnTimer)
		turnTimer = nil
		if r.engine == nil || r.engine.Over() {
			return
		}
		turnTimer = time.NewTimer(time.Until(r.engine.Deadline()))
	}

	// resetGraceTimer arms one timer for the earliest reconnect window still
	// open. Several seats can be waiting at once, and a timer each would be a
	// timer per player to stop, drain and reason about; one wakeup at the
	// nearest deadline settles every window that has passed by the time it
	// fires.
	resetGraceTimer := func() {
		stop(graceTimer)
		graceTimer = nil
		next, waiting := r.nextGraceExpiry()
		if !waiting {
			return
		}
		graceTimer = time.NewTimer(time.Until(next))
	}

	// resetIdleTimer restarts the lobby's own deadline. It runs only while no
	// game does: a game is bounded by the turn clock, and a room that is being
	// played in is not idle.
	resetIdleTimer := func() {
		stop(idleTimer)
		idleTimer = nil
		if r.strategy != nil || !r.inLobby() {
			return
		}
		idleTimer = time.NewTimer(r.idleFor)
	}

	for {
		// Reset by every input except chat: talking is not playing, and a room
		// must not be holdable open forever by typing into it once a minute.
		idleActivity := true

		var turnC, graceC, idleC <-chan time.Time
		if turnTimer != nil {
			turnC = turnTimer.C
		}
		if graceTimer != nil {
			graceC = graceTimer.C
		}
		if idleTimer != nil {
			idleC = idleTimer.C
		}

		select {
		case <-r.ctx.Done():
			return

		case msg := <-r.inputs:
			switch m := msg.(type) {
			case createInput:
				r.handleCreate(m)
			case startBotInput:
				r.handleStartBot(m)
			case joinInput:
				r.handleJoin(m)
			case submitInput:
				r.handleSubmit(m)
			case botMoveInput:
				r.handleBotMove(m)
			case lobbyInput:
				r.handleLobby(m)
			case chatInput:
				r.handleChat(m)
				idleActivity = false
			case resignInput:
				r.handleResign(m)
			case claimDeadEndInput:
				r.handleClaimDeadEnd(m)
			case reportWordInput:
				r.handleReportWord(m)
				idleActivity = false
			case disconnectInput:
				// A dropped connection is not a player leaving: the seat is
				// held for the reconnect window whether a game is running or
				// the room is sitting in its lobby, so a refresh does not cost
				// somebody their room.
				r.handleDisconnect(m)
			case resumeInput:
				r.handleResume(m)
			}
			// Every input can move the turn, open or close a reconnect window,
			// or both — an elimination does all of it at once. Recomputing both
			// timers here rather than in each arm is what keeps a new input
			// type from silently forgetting one.
			resetTurnTimer()
			resetGraceTimer()

		case <-turnC:
			// The timer and every message land on the same select, so a move
			// that arrives at the deadline is either strictly before or
			// strictly after it. There is no window where both apply.
			if r.engine != nil {
				before := r.mark()
				if r.engine.Timeout(time.Now()) {
					r.applyEliminations(before)
				}
			}
			resetTurnTimer()

		case <-graceC:
			graceTimer = nil
			r.handleGraceExpiry()
			resetTurnTimer()
			resetGraceTimer()

		case <-idleC:
			// A lobby nobody started a game in. Whoever is still sitting in it
			// is told why it closed rather than watching their buttons stop
			// working.
			r.broadcastError("room_idle_closed")
			return
		}

		// One broadcast per input, from the one place that knows the input is
		// finished. A kick, a grace window running out and a game ending all
		// leave the room in the same state — a lobby — and this is where that
		// state goes out.
		if r.strategy == nil && r.lobbyChanged {
			r.lobbyChanged = false
			r.broadcastRoomState()
		}

		// A bot room is its game: there is no lobby to return to and nobody to
		// wait for, so it closes with the last move.
		if r.strategy != nil && r.engine != nil && r.engine.Over() {
			return
		}
		// Everyone has left, or the last reconnect window ran out. Nothing is
		// coming that could fill the room again — a joiner needs a code the
		// hub is about to forget.
		if !r.occupied() {
			return
		}
		if idleActivity {
			resetIdleTimer()
		}
	}
}

// occupies reports whether this connection is the one seated at p.
//
// The seat, not the claimed id, is the authority: a session that was never
// seated here — or was replaced by a reconnect — must not be able to act.
func (r *room) occupies(sess *session, p game.PlayerID) bool {
	s := r.seatOf(p)
	return s != nil && s.sess != nil && s.sess == sess
}

// inLobby reports whether the room is between games. Everything a lobby
// allows is refused while a game is running, and the engine is the authority
// on that.
func (r *room) inLobby() bool { return r.engine == nil || r.engine.Over() }

// occupied reports whether anybody still holds a seat, including a player
// inside their reconnect window. An empty room has nothing left to wait for.
func (r *room) occupied() bool {
	for _, s := range r.seats {
		if s != nil {
			return true
		}
	}
	return false
}

// freeSeat returns the index a joiner would take, or -1 when the room is full.
func (r *room) freeSeat() int {
	for i, s := range r.seats {
		if s == nil {
			return i
		}
	}
	return -1
}

// seatedCount is how many seats are held, including by players inside their
// reconnect window.
func (r *room) seatedCount() int {
	n := 0
	for _, s := range r.seats {
		if s != nil {
			n++
		}
	}
	return n
}

// allConnected reports whether every seated player has a socket. A game cannot
// start without one, because the first thing it does is deal everybody a turn.
func (r *room) allConnected() bool {
	for _, s := range r.seats {
		if s != nil && s.sess == nil {
			return false
		}
	}
	return true
}

func (r *room) broadcastError(code string) {
	for _, s := range r.seats {
		if s != nil && s.sess != nil {
			s.sess.send(errorMsg(code))
		}
	}
}

func (r *room) sendTo(p game.PlayerID, msg *noituv1.ServerMessage) {
	if s := r.seatOf(p); s != nil && s.sess != nil {
		s.sess.send(msg)
	}
}

func (r *room) seatOf(p game.PlayerID) *seat {
	for _, s := range r.seats {
		if s != nil && s.id == p {
			return s
		}
	}
	return nil
}
