package wsapi

import (
	"context"
	"iter"
	"log/slog"
	"math/rand/v2"
	"slices"
	"time"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/bot"
	"github.com/tiennm99dev/noitu/server/internal/dictionary"
	"github.com/tiennm99dev/noitu/server/internal/game"
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

// Room input messages. Everything that can change a game arrives as one of
// these on a single channel, which is what makes the engine safe without a
// lock: the room goroutine is its only reader.

// createInput and startBotInput seat the first player. Seating is a message
// rather than a direct write so that every touch of room state — seats and
// engine alike — happens on the room goroutine, which makes the ownership
// invariant provable by reading run() rather than by reasoning about which
// writes happened before `go r.run()`.
type createInput struct {
	sess *session
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

// chatEntry is one line of the room's conversation.
type chatEntry struct {
	// seq is this message's place in the room's whole conversation, compared
	// against a seat's chatFrom to decide what that player may be replayed.
	seq uint64
	// author and name are cleared together when the seat is vacated: the words
	// stay, the attribution does not. Keeping the name would let the next
	// person to request that nickname inherit a stranger's messages, since
	// distinguish only compares against the seat that is currently occupied.
	author game.PlayerID
	name   string
	text   string
	at     time.Time
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
}

func newRoom(h *hub, code string, turnLimit, graceFor, idleFor time.Duration) *room {
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

// handleCreate seats the room's creator, who owns it, and opens the lobby.
//
// The code goes out in the RoomState the run loop broadcasts, so a client can
// never be handed a code before the seat behind it exists.
func (r *room) handleCreate(m createInput) {
	r.seats[0] = &seat{id: "p1", nickname: m.sess.nickname(), sess: m.sess, chatFrom: r.chatSeq}
	r.owner = "p1"
	m.sess.attach(r, "p1")
	r.lobbyChanged = true
	// Deliberately sent to a brand-new room's creator, where it is always
	// empty: it is what replaces the conversation a client may still be
	// holding from a room it was in before this one.
	r.sendChatHistory(r.seats[0])
}

// handleResign is one player giving up on their own turn. The seat, not the
// claimed id, is the authority, as everywhere a connection acts on a room.
//
// Only the player to act may give up. Giving up is a move — it is what is
// played instead of a word — and a seat that could spend it while somebody
// else was thinking would be deciding the turn of a player who had not
// finished theirs. Somebody who wants out of a game they are not on turn in
// leaves the room instead, which handleLobby answers.
func (r *room) handleResign(m resignInput) {
	if !r.occupies(m.sess, m.player) {
		m.sess.send(errorMsg("not_your_seat"))
		return
	}
	if r.engine == nil || r.engine.Over() {
		return
	}
	if r.engine.Turn() != m.player {
		m.sess.send(errorMsg("not_your_turn"))
		return
	}
	before := r.mark()
	if r.engine.Resign(m.player, time.Now()) {
		r.applyEliminations(before)
	}
}

// handleStartBot seats a bot opposite the player and begins immediately.
func (r *room) handleStartBot(m startBotInput) {
	strategy, err := bot.New(m.difficulty, rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())))
	if err != nil {
		m.sess.send(errorMsg("room_start_failed"))
		r.cancel()
		return
	}

	r.strategy = strategy
	r.seats[0] = &seat{id: "p1", nickname: m.sess.nickname(), sess: m.sess, chatFrom: r.chatSeq}
	r.seats[1] = &seat{id: botPlayerID, nickname: "Máy"}
	r.owner = "p1"
	m.sess.attach(r, "p1")

	if err := r.beginGame(); err != nil {
		slog.Error("could not start bot game", "room", r.code, "err", err)
		m.sess.send(errorMsg("game_start_failed"))
		r.cancel()
	}
}

// handleJoin seats another human in the lobby. It does not start anything: the
// owner does that, once everybody has said they are ready.
//
// The seat is bound here, on the room goroutine, and only on success. Binding
// it in the hub before this decision would leave a refused joiner still
// holding a seat, and every later Submit or Resign it sent would be applied to
// the real player sitting there.
func (r *room) handleJoin(m joinInput) {
	free := r.freeSeat()
	if free < 0 || !r.occupied() {
		m.sess.send(errorMsg("room_full"))
		return
	}
	// A room can have a free seat and still be mid-game — four people can
	// start a game three of them are in. Arriving in the middle of one is not
	// something to seat somebody for: they would have no words, no score, and
	// no way to be told what they had missed.
	if !r.inLobby() {
		m.sess.send(errorMsg("game_in_progress"))
		return
	}
	for _, s := range r.seats {
		if s != nil && s.sess == m.sess {
			m.sess.send(errorMsg("cannot_join_own_room"))
			return
		}
	}

	id := seatIDs[free]
	r.seats[free] = &seat{
		id:       id,
		nickname: distinguish(m.sess.nickname(), r.takenNicknames(id)),
		sess:     m.sess,
		// Seated now, so the conversation up to this point is not theirs to
		// read. A room code is pasted into group chats by design.
		chatFrom: r.chatSeq,
	}
	m.sess.attach(r, string(id))
	r.lobbyChanged = true
	r.sendChatHistory(r.seats[free])
}

// handleLobby applies one lobby action.
//
// Every refusal answers with a reason. A lobby button that silently does
// nothing is indistinguishable from one that is broken, and the player cannot
// see the state that refused them.
func (r *room) handleLobby(m lobbyInput) {
	if !r.occupies(m.sess, m.player) {
		m.sess.send(errorMsg("not_your_seat"))
		return
	}
	if r.strategy != nil {
		// A bot room has no lobby: one player, no readiness, nobody to kick.
		m.sess.send(errorMsg("not_in_a_room"))
		return
	}
	// Leaving is the exception: a player may want out of a game it is not
	// their turn in, and resigning is not open to them then. Readying,
	// starting and kicking all belong to a room between games.
	if !r.inLobby() && m.action != lobbyLeave {
		m.sess.send(errorMsg("game_in_progress"))
		return
	}

	mine := r.seatOf(m.player)
	isOwner := m.player == r.owner

	switch m.action {
	case lobbyReady:
		if isOwner {
			// The owner's readiness is StartGame. A flag of their own would
			// only be something they had to set before every single start.
			m.sess.send(errorMsg("owner_needs_no_ready"))
			return
		}
		mine.ready = m.ready
		r.lobbyChanged = true

	case lobbyStart:
		if !isOwner {
			m.sess.send(errorMsg("not_the_owner"))
			return
		}
		switch {
		case r.seatedCount() < minPlayers:
			m.sess.send(errorMsg("need_more_players"))
			return
		case !r.allConnected():
			m.sess.send(errorMsg("player_offline"))
			return
		case !r.guestsReady():
			m.sess.send(errorMsg("not_everyone_ready"))
			return
		}
		if err := r.beginGame(); err != nil {
			slog.Error("could not start pvp game", "room", r.code, "err", err)
			r.broadcastError("game_start_failed")
		}

	case lobbyKick:
		if !isOwner {
			m.sess.send(errorMsg("not_the_owner"))
			return
		}
		target := r.seatOf(m.target)
		switch {
		case target == nil:
			m.sess.send(errorMsg("no_one_to_kick"))
			return
		case target == mine:
			// Leaving is what an owner who wants out does, and it hands the
			// room on. Kicking yourself would drop the seat and the role
			// together while the others were still sitting here.
			m.sess.send(errorMsg("cannot_kick_self"))
			return
		case target.ready:
			// Readiness is a commitment, and the owner does not get to
			// overrule one: a player who is ready is waiting on the owner,
			// not in the way.
			m.sess.send(errorMsg("player_is_ready"))
			return
		}
		if target.sess != nil {
			target.sess.send(errorMsg("kicked"))
		}
		r.vacate(target)
		r.lobbyChanged = true

	case lobbyLeave:
		if r.inLobby() {
			// Unreadying first is deliberate friction: a player the other one
			// is waiting on should have to take that back before walking away.
			if mine.ready {
				m.sess.send(errorMsg("must_unready_first"))
				return
			}
		} else {
			// Out of a running game, which is the same thing to everybody else
			// as a reconnect window running out: somebody left. The engine
			// goes first, while the seat is still here to be named in what is
			// broadcast about it.
			before := r.mark()
			r.eliminateAbsent(mine, time.Now())
			r.applyEliminations(before)
		}
		r.vacate(mine)
		r.lobbyChanged = true
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

// beginGame builds the engine and tells both seats the game is on.
func (r *room) beginGame() error {
	opening, err := r.dict.RandomOpeningWord(minOpeningOutDegree)
	if err != nil {
		return err
	}

	// Seat order is turn order, so a player's place at the table is the place
	// they took in the lobby and nothing has to be shuffled or announced.
	ids := make([]game.PlayerID, 0, maxPlayers)
	for _, s := range r.seats {
		if s != nil {
			ids = append(ids, s.id)
		}
	}
	// Who leads is drawn rather than owned. Opening the game is an advantage —
	// the first player picks from a whole syllable, everyone after them plays
	// what is left of it — and giving it to whoever happened to create the
	// room would make the same person favourite in every game of a series.
	//
	// Rotating rather than shuffling keeps the table intact: everybody still
	// plays in the order they sat down, the cycle just starts somewhere else.
	// A bot room is left alone; it has no table to be fair about, and the
	// human opens.
	if r.strategy == nil {
		lead := rand.IntN(len(ids))
		ids = slices.Concat(ids[lead:], ids[:lead])
	}

	engine, err := game.New(r.dict, ids, opening, r.turnLimit, time.Now())
	if err != nil {
		return err
	}
	r.engine = engine
	r.opening = opening
	// Fresh per game: an override from the last one would describe a player
	// who has since come back and is playing this one.
	r.outWire = make(map[game.PlayerID]noituv1.GameEndReason, len(ids))
	// Never restarts at 1. A rematch reuses the same connections, so a
	// submission still in flight from the previous game would otherwise be
	// able to match a turn in this one and be applied to it.
	r.turnSeq++
	// Every game is agreed on its own. The readiness that started this one is
	// spent, so the lobby they come back to asks again.
	for _, s := range r.seats {
		if s != nil {
			s.ready = false
		}
	}

	state := r.engine.Snapshot()
	for _, s := range r.seats {
		r.sendGameStarted(s, state)
	}
	r.maybeScheduleBot()
	return nil
}

// sendGameStarted renders the opening position for one seat. my_turn and is_me
// are per-recipient, which is why this is built per seat rather than broadcast.
func (r *room) sendGameStarted(s *seat, state game.State) {
	if s == nil || s.sess == nil {
		return
	}
	s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_GameStarted{
		GameStarted: &noituv1.GameStarted{
			OpeningWord:     r.opening,
			OpeningMeanings: Senses(r.dict.Meanings(r.opening)),
			CurrentSyllable: state.Current,
			MyTurn:          state.Turn == s.id,
			DeadlineUnixMs:  state.Deadline.UnixMilli(),
			TurnSeq:         r.turnSeq,
			TurnLimitMs:     uint32(r.turnLimit.Milliseconds()),
			Players:         r.scoreRows(r.engine.Players(), state, s.id, nil),
			TurnPlayerId:    string(state.Turn),
		},
	}})
}

// handleSubmit runs one human move through the engine.
func (r *room) handleSubmit(m submitInput) {
	if !r.occupies(m.sess, m.player) {
		m.sess.send(errorMsg("not_your_seat"))
		return
	}
	if r.engine == nil {
		r.sendTo(m.player, errorMsg("game_not_started"))
		return
	}

	// A submission stamped with an old turn is answering a position that no
	// longer exists — a double-submit, or a word typed as the clock ran out.
	// Applying it to the current turn would play a word the player never
	// chose for this position.
	// The rejection carries the server's sequence, not the client's stale one,
	// so the client can resynchronise from the refusal instead of having to
	// wait for the next turn update to discover where the game actually is.
	if m.turnSeq != r.turnSeq {
		r.sendTo(m.player, moveRejectedMsg(noituv1.RejectReason_REJECT_REASON_NOT_YOUR_TURN, m.word, r.turnSeq))
		return
	}

	before := r.mark()
	move, reason := r.engine.Submit(m.player, m.word, time.Now())
	if reason != game.ReasonNone {
		r.sendTo(m.player, moveRejectedMsg(RejectReason(reason), m.word, m.turnSeq))
		// A rejection for an expired turn also took this player out of the
		// game, and everybody has to be told which.
		r.applyEliminations(before)
		return
	}

	// An accepted move never ends a game: a dead end is left for whoever
	// inherits it, which is what Submit's own comment explains.
	r.turnSeq++
	r.broadcastTurn(&move)
	r.maybeScheduleBot()
}

// handleBotMove applies what the worker chose.
func (r *room) handleBotMove(m botMoveInput) {
	if r.engine == nil || r.engine.Over() {
		return
	}
	// The position moved on while it was thinking; the chosen word answers a
	// board that no longer exists.
	if m.turnSeq != r.turnSeq || r.engine.Turn() != botPlayerID {
		return
	}

	now := time.Now()
	before := r.mark()

	if m.err != nil {
		// The bot has nothing to play. A human in this position keeps their
		// turn and loses it to the clock; the bot has no clock to spend, so
		// the position is settled now and reported for what it is rather than
		// as a resignation it never chose.
		if !r.engine.NoMove(now) {
			r.engine.Resign(botPlayerID, now)
		}
		r.applyEliminations(before)
		return
	}

	move, reason := r.engine.Submit(botPlayerID, m.word, now)
	if reason != game.ReasonNone {
		// The bot searched the same dictionary the engine validates against,
		// so this means the two disagree — a bug worth seeing, not a move to
		// retry.
		slog.Error("bot move rejected by engine", "room", r.code, "word", m.word, "reason", reason.String())
		r.engine.Resign(botPlayerID, now)
		r.applyEliminations(before)
		return
	}

	r.turnSeq++
	r.broadcastTurn(&move)
}

// maybeScheduleBot starts the bot thinking if it is now its turn.
func (r *room) maybeScheduleBot() {
	if r.strategy == nil || r.engine.Over() || r.engine.Turn() != botPlayerID {
		return
	}

	// The board is frozen here, on the room goroutine, before the worker
	// exists. Handing the worker the live engine instead would race every
	// resign and disconnect the room processes while the bot thinks — and
	// bot.Board.Used reads engine state, so the race would be real, not
	// theoretical.
	board := freezeBoard(r.engine, r.opening)
	seq := r.turnSeq
	strategy := r.strategy

	go func() {
		word, err := strategy.Choose(board)

		// The pause is a courtesy to the player, so it must not outlive the
		// room: a bot still sleeping after everyone left is a goroutine leak
		// per abandoned game.
		select {
		case <-time.After(strategy.ThinkingDelay()):
		case <-r.ctx.Done():
			return
		}
		r.send(botMoveInput{word: word, err: err, turnSeq: seq})
	}()
}

// broadcastTurn sends the position to every seat, rendered for each.
//
// move is nil when the turn moved without a word being played, which is what
// an elimination does: the syllable and the used set survive the player who
// could not answer them, and everybody still needs the new deadline and the
// new player to act.
func (r *room) broadcastTurn(move *game.Move) {
	state := r.engine.Snapshot()
	meanings := r.moveMeanings(move)
	for _, s := range r.seats {
		r.sendTurnUpdate(s, state, move, meanings)
	}
}

// moveMeanings looks up a played word's senses once per move; they are the
// same for every recipient. nil for no move.
func (r *room) moveMeanings(move *game.Move) []dictionary.Sense {
	if move == nil {
		return nil
	}
	return r.dict.Meanings(move.Word)
}

// sendTurnUpdate renders one position for one seat. by_me, my_turn and is_me
// are all per-recipient, which is why there is no single shared frame; the
// move's meanings are not, and arrive looked up.
func (r *room) sendTurnUpdate(s *seat, state game.State, move *game.Move, meanings []dictionary.Sense) {
	if s == nil || s.sess == nil {
		return
	}
	update := &noituv1.TurnUpdate{
		CurrentSyllable: state.Current,
		MyTurn:          state.Turn == s.id,
		DeadlineUnixMs:  state.Deadline.UnixMilli(),
		TurnSeq:         r.turnSeq,
		ChainLength:     uint32(state.ChainLength),
		Players:         r.scoreRows(r.engine.Players(), state, s.id, nil),
		TurnPlayerId:    string(state.Turn),
	}
	if move != nil {
		update.Played = PlayedWord(*move, move.Player == s.id, meanings)
	}
	s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_TurnUpdate{TurnUpdate: update}})
}

// inputMark is what the game looked like before an input: how many players
// were out, and who was to act. Remembered across the input so
// applyEliminations can tell that input's doing from what was already true,
// and whether it moved the turn.
type inputMark struct {
	out  int
	turn game.PlayerID
}

// mark reads the current game, or the zero mark when there is no game.
func (r *room) mark() inputMark {
	if r.engine == nil {
		return inputMark{}
	}
	return inputMark{out: r.engine.EliminatedCount(), turn: r.engine.Turn()}
}

// applyEliminations reports everybody the last input knocked out, then whatever
// the game became: finished, or one turn further on.
//
// Every path that takes a player out of a game ends here — a timeout, a
// resignation, a bot with nothing to play, a reconnect window running out — so
// there is one place that decides what the room says about it.
func (r *room) applyEliminations(before inputMark) {
	if r.engine == nil {
		return
	}
	state := r.engine.Snapshot()
	if len(state.Eliminated) == before.out {
		return
	}

	// An elimination does not move the position, so one lookup describes it
	// for everybody who went out on this input.
	suggestions := r.engine.Suggestions(maxSuggestions)
	for _, id := range state.Eliminated[before.out:] {
		r.broadcastElimination(id, suggestions)
	}

	if r.engine.Over() {
		r.broadcastGameOver(state)
		return
	}
	// A new turn nobody played into, and the sequence moves with it: a
	// submission already in flight was answering the position the player who
	// just went out was looking at.
	//
	// It moves only when the turn does. Somebody forfeiting out of turn — a
	// player who left the room, or whose reconnect window ran out — leaves the
	// syllable, the deadline and the player to act exactly as they were, so
	// the word that player is already sending still answers the board it was
	// typed for. Bumping the sequence there would refuse it for something
	// somebody else did.
	if state.Turn != before.turn {
		r.turnSeq++
	}
	r.broadcastTurn(nil)
}

// broadcastElimination tells the room one player is out.
//
// The suggestions go only to that player. They are what the position still had
// to offer, and the people who could still answer it are not the ones who
// needed to be told — an empty list is the answer for whoever was stuck, and
// noise for everybody else.
func (r *room) broadcastElimination(id game.PlayerID, suggestions []string) {
	name := ""
	if out := r.seatOf(id); out != nil {
		name = out.nickname
	}
	reason := r.wireEndReason(id)

	for _, s := range r.seats {
		if s == nil || s.sess == nil {
			continue
		}
		msg := &noituv1.PlayerEliminated{
			PlayerId: string(id),
			Name:     name,
			IsMe:     s.id == id,
			Reason:   reason,
		}
		if s.id == id {
			msg.Suggestions = suggestions
		}
		s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_PlayerEliminated{
			PlayerEliminated: msg,
		}})
	}
}

// wireEndReason says how one player left the game.
//
// The engine's answer, unless the room overrode it: a reconnect window running
// out is a resignation to the engine, because that is the only shape it has
// for a player who stops playing, and somebody who left to everybody in the
// room.
func (r *room) wireEndReason(p game.PlayerID) noituv1.GameEndReason {
	if code, overridden := r.outWire[p]; overridden {
		return code
	}
	return EndReason(r.engine.OutReason(p))
}

// broadcastGameOver reports the result from each seat's point of view.
func (r *room) broadcastGameOver(state game.State) {
	// The reason the game ended is the reason the last player went out, which
	// with two seats is the only elimination there was.
	reason := noituv1.GameEndReason_GAME_END_REASON_UNSPECIFIED
	if n := len(state.Eliminated); n > 0 {
		reason = r.wireEndReason(state.Eliminated[n-1])
	}

	// Credited before anything is sent, so the RoomState the run loop
	// broadcasts after a finished game already carries the game just won.
	if s := r.seatOf(state.Winner); s != nil {
		s.wins++
	}

	ranks := make(map[game.PlayerID]int, len(state.Standings))
	order := make([]game.PlayerID, 0, len(state.Standings))
	for _, standing := range state.Standings {
		ranks[standing.Player] = standing.Rank
		order = append(order, standing.Player)
	}

	for _, s := range r.seats {
		if s == nil || s.sess == nil {
			continue
		}
		s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_GameOver{
			GameOver: &noituv1.GameOver{
				IWon:        state.Winner == s.id,
				Reason:      reason,
				ChainLength: uint32(state.ChainLength),
				Standings:   r.scoreRows(order, state, s.id, ranks),
			},
		}})
	}
	// A finished game is a return to the lobby, and the run loop reports the
	// state they are returning to.
	r.lobbyChanged = true
}

// scoreRows renders the players table for one recipient.
//
// order is the sequence to report them in — turn order while a game runs,
// finishing order once one has ended — and ranks is empty until there is a
// result, which is what makes a rank of zero mean "still playing" rather than
// needing a field of its own to say so.
func (r *room) scoreRows(order []game.PlayerID, state game.State, me game.PlayerID, ranks map[game.PlayerID]int) []*noituv1.PlayerScore {
	rows := make([]*noituv1.PlayerScore, 0, len(order))
	for _, id := range order {
		row := &noituv1.PlayerScore{
			PlayerId: string(id),
			IsMe:     id == me,
			Score:    uint32(state.Scores[id]),
			// A player the engine no longer knows is a seat that was vacated
			// mid-game, which only happens to somebody already out.
			Eliminated: !state.Alive[id],
			// The bot has no socket to lose, so it is never the one keeping
			// the room waiting.
			Connected: id == botPlayerID,
			Rank:      uint32(ranks[id]),
		}
		if s := r.seatOf(id); s != nil {
			row.Name = s.nickname
			row.Connected = row.Connected || s.sess != nil
		}
		rows = append(rows, row)
	}
	return rows
}

// handleDisconnect holds the seat open for the player who dropped out of it.
//
// A dropped connection is not a player leaving. The seat is kept for the
// reconnect window whether a game is running or the room is sitting in its
// lobby, so refreshing the page does not cost somebody the room they are in.
//
// The turn clock is deliberately not paused. A player who drops on their own
// turn loses it the way anybody else would; the window decides only whether
// they are still in the game afterwards.
func (r *room) handleDisconnect(m disconnectInput) {
	s := r.seatOf(m.player)
	// A stale notice from a connection the player already replaced. Acting on
	// it would evict the seat the new socket is sitting in.
	if s == nil || s.sess == nil || s.sess != m.sess {
		return
	}
	s.sess = nil
	s.graceUntil = time.Now().Add(r.graceFor)
	// Presence is part of the room's state, and the run loop is what sends it.
	// There is nothing extra to say to the players who are still here.
	r.lobbyChanged = true
}

// nextGraceExpiry is the earliest reconnect window still open.
func (r *room) nextGraceExpiry() (time.Time, bool) {
	var next time.Time
	for _, s := range r.seats {
		if s == nil || s.sess != nil || s.graceUntil.IsZero() {
			continue
		}
		if next.IsZero() || s.graceUntil.Before(next) {
			next = s.graceUntil
		}
	}
	return next, !next.IsZero()
}

// handleGraceExpiry frees every seat whose reconnect window has run out.
//
// The engine goes first, while the seats are still here to be named: once one
// is vacated there is nobody left to attribute the elimination to, and the
// players who stayed would be told that somebody with no name went out.
func (r *room) handleGraceExpiry() {
	now := time.Now()

	var expired []*seat
	for _, s := range r.seats {
		if s == nil || s.sess != nil || s.graceUntil.IsZero() || s.graceUntil.After(now) {
			continue
		}
		expired = append(expired, s)
	}
	if len(expired) == 0 {
		return
	}

	before := r.mark()
	for _, s := range expired {
		r.eliminateAbsent(s, now)
	}
	r.applyEliminations(before)

	for _, s := range expired {
		r.vacate(s)
	}
	r.lobbyChanged = true
}

// eliminateAbsent takes a seat out of a live game once nobody is coming back
// to it.
//
// The engine is told this is a resignation, because that is the only shape it
// has for a player who stops playing. What the room reports is the transport
// fact instead: from everybody else's side this is somebody who left, not
// somebody who chose to give up.
func (r *room) eliminateAbsent(s *seat, now time.Time) {
	if r.engine == nil || r.engine.Over() || !r.engine.Alive(s.id) {
		return
	}
	r.outWire[s.id] = noituv1.GameEndReason_GAME_END_REASON_OPPONENT_LEFT
	r.engine.Resign(s.id, now)
}

// handleResume rebinds a seat to a new connection and replays the position.
//
// The replay is built from the engine, never from stored copies of past
// messages: a recorded stream can drift from the real state, and the resumed
// client would then be shown a board the server does not believe in.
func (r *room) handleResume(m resumeInput) {
	s := r.seatOf(m.player)
	if s == nil {
		m.sess.send(errorMsg("session_not_resumable"))
		return
	}

	// Accepted. Only now is the old connection finished: its token is spent and
	// its socket is either gone or about to be, and leaving it registered would
	// let a third connection claim the same seat.
	m.sess.attach(r, string(m.player))
	if m.prior != nil {
		m.sess.hub.unregister(m.prior.resumeToken)
		m.prior.close()
	}
	s.sess = m.sess
	s.graceUntil = time.Time{}
	// The seat keeps the name it was given. Re-reading it from the new
	// connection would let a reconnect rename a player mid-game, including
	// into somebody else's name.

	// Everybody needs the room's state again: this player to render the lobby
	// they came back to, the rest to stop watching a disconnect banner for
	// somebody who is already back. The run loop sends it to all of them.
	r.lobbyChanged = true

	// Before the lobby return below, not after it: a refresh in the lobby is
	// the commonest resume there is, and it is exactly the one that would miss
	// a replay hung off the end of this function.
	r.sendChatHistory(s)

	// Resumed between games, or before the first one. The lobby state above is
	// the whole answer; there is no position to replay.
	if r.inLobby() {
		return
	}
	state := r.engine.Snapshot()
	r.sendGameStarted(s, state)
	if len(state.History) > 0 {
		last := state.History[len(state.History)-1]
		r.sendTurnUpdate(s, state, &last, r.moveMeanings(&last))
	}
}

// handleChat delivers one line of text to everybody in the room.
func (r *room) handleChat(m chatInput) {
	// The seat, not the claimed id. A connection the room has already retired
	// - kicked, or replaced by a reconnect - can still have a frame in flight,
	// and by the time the room drains it that seat may belong to somebody else.
	if !r.occupies(m.sess, m.player) {
		m.sess.send(errorMsg("not_your_seat"))
		return
	}
	// A bot room has no conversation. Checked here rather than in the session,
	// because r.strategy is room-goroutine state.
	if r.strategy != nil {
		m.sess.send(errorMsg("not_in_a_room"))
		return
	}

	text := sanitizeText(m.text, maxChatRunes, maxChatMarks)
	// Nothing usable survived. There is no message to refuse and nobody to
	// tell: the client will not enable its send button for input that reduces
	// to this, so anything reaching here typed nothing.
	if text == "" {
		return
	}

	from := r.seatOf(m.player)
	r.chatSeq++
	entry := chatEntry{
		seq:    r.chatSeq,
		author: from.id,
		name:   from.nickname,
		text:   text,
		at:     time.Now(),
	}
	r.chat = append(r.chat, entry)
	if len(r.chat) > chatHistoryLimit {
		r.chat = r.chat[len(r.chat)-chatHistoryLimit:]
	}

	for _, s := range r.seats {
		if s == nil || s.sess == nil {
			continue
		}
		// Best effort: a chat frame is dropped rather than allowed to close a
		// session whose outbox is full. Losing a line is recoverable - the
		// next replay carries it - and closing a session costs its owner the
		// game.
		s.sess.trySend(chatMessageFor(entry, s.id))
	}
}

// sendChatHistory replays one seat's slice of the conversation.
//
// Scoped by the seat's chatFrom: a player is shown what was said while they
// were sitting there and nothing else. Sent from the handler, so it reaches the
// client before that input's RoomState - the client must not depend on the
// order, and does not, because a history replaces its panel wholesale.
func (r *room) sendChatHistory(s *seat) {
	if s == nil || s.sess == nil || r.strategy != nil {
		return
	}

	messages := make([]*noituv1.ChatMessage, 0, len(r.chat))
	for _, entry := range r.chat {
		if entry.seq <= s.chatFrom {
			continue
		}
		messages = append(messages, chatMessageFor(entry, s.id).GetChatMessage())
	}

	// send, not trySend: this is the frame that corrects a client's whole
	// panel, including the empty one that clears a conversation carried in
	// from another room. A dropped line recovers on the next replay; a dropped
	// replay has nothing behind it.
	s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_ChatHistory{
		ChatHistory: &noituv1.ChatHistory{Messages: messages},
	}})
}

// chatMessageFor renders one entry from one seat's point of view.
//
// An entry whose author has been cleared belongs to nobody: it is from_me for
// neither player and carries no name, so the seat's next occupant is not shown
// a stranger's words as their own and the player who stayed cannot have them
// reattributed to whoever arrives next.
func chatMessageFor(entry chatEntry, id game.PlayerID) *noituv1.ServerMessage {
	return &noituv1.ServerMessage{Payload: &noituv1.ServerMessage_ChatMessage{
		ChatMessage: &noituv1.ChatMessage{
			FromMe:     entry.author != "" && entry.author == id,
			// Empty together with the name for a vacated seat: a line nobody
			// owns must not be coloured as somebody's either.
			PlayerId:   string(entry.author),
			Author:     entry.name,
			Text:       entry.text,
			SentUnixMs: entry.at.UnixMilli(),
		},
	}}
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

// seatIDs are the engine seat names, indexed by position. An id says which
// seat a player is in and nothing about their role: an owner who leaves hands
// that on, and the seat they vacate is refilled by an ordinary guest.
var seatIDs = [maxPlayers]game.PlayerID{"p1", "p2", "p3", "p4"}

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

// guestsReady reports whether every seat but the owner's has said yes. The
// owner's readiness is StartGame itself, which is why they are not counted.
func (r *room) guestsReady() bool {
	for _, s := range r.seats {
		if s != nil && s.id != r.owner && !s.ready {
			return false
		}
	}
	return true
}

// takenNicknames is every name already in this room except one seat's own, so
// a joiner can be told apart from all of them.
func (r *room) takenNicknames(except game.PlayerID) []string {
	names := make([]string, 0, maxPlayers)
	for _, s := range r.seats {
		if s != nil && s.id != except {
			names = append(names, s.nickname)
		}
	}
	return names
}

// canStart reports whether StartGame would be accepted. The server answers
// this rather than the client because it owns every condition that feeds it.
func (r *room) canStart() bool {
	if r.strategy != nil || !r.inLobby() {
		return false
	}
	return r.seatedCount() >= minPlayers && r.allConnected() && r.guestsReady()
}

// vacate frees a seat for good - the player left, was kicked, or never came
// back - and hands the room on when the seat was the owner's.
func (r *room) vacate(s *seat) {
	if s == nil {
		return
	}
	if s.sess != nil {
		// The connection stays open; it is simply no longer in this room, so
		// anything else it sends here is refused rather than applied to a seat
		// somebody else may now be sitting in.
		s.sess.release(r)
		s.sess = nil
	}
	for i, existing := range r.seats {
		if existing == s {
			r.seats[i] = nil
		}
	}
	// The words stay; the attribution goes. Both fields, not just the id: a
	// retained name lets the next person to ask for that nickname inherit
	// these messages, because distinguish only compares against the seat that
	// is occupied.
	scrubbed := false
	for i := range r.chat {
		if r.chat[i].author == s.id {
			r.chat[i].author = ""
			r.chat[i].name = ""
			scrubbed = true
		}
	}
	// Clearing the store is only half of it: the player who stayed is holding
	// frames that still carry the departed name, and RoomState carries no
	// chat. Without this re-sync they keep that attribution until they happen
	// to reload — long enough for somebody to join under the same nickname and
	// inherit a stranger's words.
	if scrubbed {
		// The loop above has already emptied this seat out of r.seats, so what
		// is left is exactly the players who need correcting.
		for _, other := range r.seats {
			r.sendChatHistory(other)
		}
	}
	if r.owner == s.id {
		r.promote()
	}
}

// promote hands the room to whoever is left.
func (r *room) promote() {
	for _, s := range r.seats {
		if s != nil {
			r.owner = s.id
			// The new owner starts games, and starting is their readiness. A
			// flag they set as a guest would sit there meaning nothing.
			s.ready = false
			return
		}
	}
	r.owner = ""
}

// broadcastRoomState sends the whole room to each occupant.
//
// Built per recipient because the field that matters most in it — which of
// these players is you — is relative to who is being told. One snapshot rather
// than a stream of deltas is what lets a client that missed a frame, or has
// just reconnected, be correct again from the next one.
func (r *room) broadcastRoomState() {
	canStart := r.canStart()

	for _, s := range r.seats {
		if s == nil || s.sess == nil {
			continue
		}
		s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_RoomState{
			RoomState: &noituv1.RoomState{
				RoomCode:   r.code,
				CanStart:   canStart,
				Players:    r.playerSlots(s.id),
				MaxPlayers: maxPlayers,
				MinPlayers: minPlayers,
				GraceMs:    uint32(r.graceFor.Milliseconds()),
			},
		}})
	}
}

// playerSlots renders the seating for one recipient, in seat order. That is
// the order they will play in, but not who plays first: the lead is drawn when
// the game starts, and the table sent with it is the one in turn order.
func (r *room) playerSlots(me game.PlayerID) []*noituv1.PlayerSlot {
	slots := make([]*noituv1.PlayerSlot, 0, maxPlayers)
	for _, s := range r.seats {
		if s == nil {
			continue
		}
		slots = append(slots, &noituv1.PlayerSlot{
			PlayerId:  string(s.id),
			Name:      s.nickname,
			IsMe:      s.id == me,
			IsOwner:   s.id == r.owner,
			Ready:     s.ready,
			Connected: s.sess != nil,
			Wins:      s.wins,
		})
	}
	return slots
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

func freezeBoard(e *game.Engine, opening string) *frozenBoard {
	state := e.Snapshot()

	// History omits the opening word, but the engine counts it as played. A
	// board that disagreed would let the bot pick a word the engine then
	// rejects as already used.
	used := make(map[string]struct{}, len(state.History)+1)
	used[opening] = struct{}{}
	for _, m := range state.History {
		used[m.Word] = struct{}{}
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
