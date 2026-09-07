package wsapi

import (
	"context"
	"iter"
	"log/slog"
	"math/rand/v2"
	"time"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/bot"
	"github.com/tiennm99dev/noitu/server/internal/game"
)

// botPlayerID is the seat the bot occupies. It is a normal player to the
// engine, which is the whole point: the bot's moves go through the same
// validation as a human's, so there is one rule implementation rather than two.
const botPlayerID game.PlayerID = "bot"

// minOpeningOutDegree keeps the first word from being a dead end. Opening on a
// syllable with two continuations makes for a game that ends before it starts.
const minOpeningOutDegree = 20

// maxSuggestions is how many of the words still playable a losing player is
// shown. Enough to see what the position wanted, few enough that it reads as
// a hint rather than a dump of the dictionary.
const maxSuggestions = 3

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

	seats [2]*seat

	// owner is the seat that may start a game and free the other one. It is a
	// field rather than "seats[0]" because the role outlives the player who
	// held it: an owner who leaves hands it to whoever is still here, and the
	// seat they vacate is then filled by an ordinary guest.
	owner game.PlayerID

	// turnSeq increments on every turn change. A client stamps its submission
	// with the sequence it was answering, so a move that crosses the deadline
	// is identifiable rather than silently applied to the next turn.
	turnSeq uint32

	// disconnected is the seat currently inside its reconnect grace window,
	// or nil. Only one seat can be waiting: if the second also drops, there is
	// nobody left to win and the room ends.
	disconnected *seat

	// lobbyChanged marks that something a player can see about the room's
	// occupants has changed: a seat filled or freed, a readiness set, an owner
	// promoted, a game finished. The run loop turns it into exactly one
	// RoomState broadcast per input, which is why no handler has to remember
	// to send one.
	lobbyChanged bool
}

// Dictionary is everything the transport layer needs from the wordlist: the
// engine's own contract, plus a way to pick an opening.
//
// An interface rather than *dictionary.Store so a test can play a whole game
// against a hand-built graph of a dozen words, where the expected outcome is
// something a reader can verify by eye. *dictionary.Store satisfies it as
// written.
type Dictionary interface {
	game.Dictionary
	RandomOpeningWord(minOutDegree int) (string, error)
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
				resetTurnTimer()
			case joinInput:
				r.handleJoin(m)
				resetTurnTimer()
			case submitInput:
				r.handleSubmit(m)
				resetTurnTimer()
			case botMoveInput:
				r.handleBotMove(m)
				resetTurnTimer()
			case lobbyInput:
				r.handleLobby(m)
				resetTurnTimer()
			case resignInput:
				if !r.occupies(m.sess, m.player) {
					m.sess.send(errorMsg("not_your_seat"))
					break
				}
				if r.engine != nil && r.engine.Resign(m.player) {
					r.broadcastGameOver()
				}
				resetTurnTimer()
			case disconnectInput:
				// A dropped connection is not a player leaving: the seat is
				// held for the reconnect window whether a game is running or
				// the room is sitting in its lobby, so a refresh does not cost
				// somebody their room.
				//
				// handleDisconnect returning false means the notice was stale —
				// from a connection the seat no longer holds — and acting on
				// that would evict a seat its new socket is sitting in.
				if r.handleDisconnect(m) {
					stop(graceTimer)
					graceTimer = time.NewTimer(r.graceFor)
				}
				resetTurnTimer()
			case resumeInput:
				r.handleResume(m)
				stop(graceTimer)
				graceTimer = nil
				resetTurnTimer()
			}

		case <-turnC:
			// The timer and every message land on the same select, so a move
			// that arrives at the deadline is either strictly before or
			// strictly after it. There is no window where both apply.
			if r.engine != nil && r.engine.Timeout(time.Now()) {
				r.broadcastGameOver()
			}
			resetTurnTimer()

		case <-graceC:
			graceTimer = nil
			r.handleGraceExpiry()
			resetTurnTimer()

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
		resetIdleTimer()
	}
}

// handleCreate seats the room's creator, who owns it, and opens the lobby.
//
// The code goes out in the RoomState the run loop broadcasts, so a client can
// never be handed a code before the seat behind it exists.
func (r *room) handleCreate(m createInput) {
	r.seats[0] = &seat{id: "p1", nickname: m.sess.nickname(), sess: m.sess}
	r.owner = "p1"
	m.sess.attach(r, "p1")
	r.lobbyChanged = true
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
	r.seats[0] = &seat{id: "p1", nickname: m.sess.nickname(), sess: m.sess}
	r.seats[1] = &seat{id: botPlayerID, nickname: "Máy"}
	r.owner = "p1"
	m.sess.attach(r, "p1")

	if err := r.beginGame(); err != nil {
		slog.Error("could not start bot game", "room", r.code, "err", err)
		m.sess.send(errorMsg("game_start_failed"))
		r.cancel()
	}
}

// handleJoin seats a second human in the lobby. It no longer starts anything:
// the owner does that, once this player says they are ready.
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
	// A game in progress fills both seats, so this only catches a room whose
	// seat was freed by the very disconnect that ended the game — for the
	// moment before the room notices.
	if !r.inLobby() {
		m.sess.send(errorMsg("room_full"))
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
		nickname: distinguish(m.sess.nickname(), r.otherNickname(id)),
		sess:     m.sess,
	}
	m.sess.attach(r, string(id))
	r.lobbyChanged = true
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
	if !r.inLobby() {
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
		guest := r.guestSeat()
		switch {
		case guest == nil:
			m.sess.send(errorMsg("need_two_players"))
			return
		case guest.sess == nil:
			m.sess.send(errorMsg("opponent_offline"))
			return
		case !guest.ready:
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
		guest := r.guestSeat()
		if guest == nil {
			m.sess.send(errorMsg("no_one_to_kick"))
			return
		}
		// Readiness is a commitment, and the owner does not get to overrule
		// one: a guest who is ready is waiting on the owner, not in the way.
		if guest.ready {
			m.sess.send(errorMsg("player_is_ready"))
			return
		}
		if guest.sess != nil {
			guest.sess.send(errorMsg("kicked"))
		}
		r.vacate(guest)
		r.lobbyChanged = true

	case lobbyLeave:
		// Unreadying first is deliberate friction: a player the other one is
		// waiting on should have to take that back before walking away.
		if mine.ready {
			m.sess.send(errorMsg("must_unready_first"))
			return
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

	engine, err := game.New(r.dict, []game.PlayerID{r.seats[0].id, r.seats[1].id}, opening, r.turnLimit, time.Now())
	if err != nil {
		return err
	}
	r.engine = engine
	r.opening = opening
	// Never restarts at 1. A rematch reuses the same connections, so a
	// submission still in flight from the previous game would otherwise be
	// able to match a turn in this one and be applied to it.
	r.turnSeq++
	// Every game is agreed on its own. The readiness that started this one is
	// spent, so the lobby they come back to asks again.
	for _, s := range r.seats {
		s.ready = false
	}

	for _, s := range r.seats {
		r.sendGameStarted(s)
	}
	r.maybeScheduleBot()
	return nil
}

// sendGameStarted renders the opening position for one seat. my_turn is
// per-recipient, which is why this is built per seat rather than broadcast.
func (r *room) sendGameStarted(s *seat) {
	if s.sess == nil {
		return
	}
	s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_GameStarted{
		GameStarted: &noituv1.GameStarted{
			OpeningWord:     r.opening,
			CurrentSyllable: r.engine.Current(),
			MyTurn:          r.engine.Turn() == s.id,
			DeadlineUnixMs:  r.engine.Deadline().UnixMilli(),
			TurnSeq:         r.turnSeq,
			TurnLimitMs:     uint32(r.turnLimit.Milliseconds()),
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

	move, reason := r.engine.Submit(m.player, m.word, time.Now())
	if reason != game.ReasonNone {
		r.sendTo(m.player, moveRejectedMsg(RejectReason(reason), m.word, m.turnSeq))
		// A rejection for an expired turn is also the end of the game.
		if r.engine.Over() {
			r.broadcastGameOver()
		}
		return
	}

	r.turnSeq++
	r.broadcastTurn(move)

	if r.engine.Over() {
		r.broadcastGameOver()
		return
	}
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

	if m.err != nil {
		// The bot has nothing to play. A human in this position keeps their
		// turn and loses it to the clock; the bot has no clock to spend, so
		// the position is settled now and reported for what it is rather than
		// as a resignation it never chose.
		if !r.engine.NoMove() {
			r.engine.Resign(botPlayerID)
		}
		r.broadcastGameOver()
		return
	}

	move, reason := r.engine.Submit(botPlayerID, m.word, time.Now())
	if reason != game.ReasonNone {
		// The bot searched the same dictionary the engine validates against,
		// so this means the two disagree — a bug worth seeing, not a move to
		// retry.
		slog.Error("bot move rejected by engine", "room", r.code, "word", m.word, "reason", reason.String())
		r.engine.Resign(botPlayerID)
		r.broadcastGameOver()
		return
	}

	r.turnSeq++
	r.broadcastTurn(move)
	if r.engine.Over() {
		r.broadcastGameOver()
	}
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

// broadcastTurn sends the move to both seats, rendered for each.
func (r *room) broadcastTurn(move game.Move) {
	state := r.engine.Snapshot()
	for i, s := range r.seats {
		if s.sess == nil {
			continue
		}
		opponent := r.seats[1-i]
		s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_TurnUpdate{
			TurnUpdate: &noituv1.TurnUpdate{
				Played:          PlayedWord(move, move.Player == s.id),
				CurrentSyllable: state.Current,
				MyTurn:          state.Turn == s.id,
				DeadlineUnixMs:  state.Deadline.UnixMilli(),
				TurnSeq:         r.turnSeq,
				MyScore:         uint32(state.Scores[s.id]),
				OpponentScore:   uint32(state.Scores[opponent.id]),
				ChainLength:     uint32(state.ChainLength),
			},
		}})
	}
}

// broadcastGameOver reports the result from each seat's point of view.
func (r *room) broadcastGameOver() {
	state := r.engine.Snapshot()
	for _, s := range r.seats {
		if s.sess == nil {
			continue
		}
		s.sess.send(r.gameOverFor(state, s.id, EndReason(state.EndReason)))
	}
	// A finished game is a return to the lobby, and the run loop reports the
	// state they are returning to.
	r.lobbyChanged = true
}

// gameOverFor renders a finished game for one seat.
//
// The loser is told what could have been played from the position the game
// ended on. The winner is not: they are not the one who was stuck, and it is
// the loser for whom an empty list answers the question — nothing could have
// been played, so the position, not the player, ended the game.
func (r *room) gameOverFor(state game.State, id game.PlayerID, reason noituv1.GameEndReason) *noituv1.ServerMessage {
	iWon := state.Winner == id

	var suggestions []string
	if !iWon {
		suggestions = r.engine.Suggestions(maxSuggestions)
	}

	return &noituv1.ServerMessage{Payload: &noituv1.ServerMessage_GameOver{
		GameOver: &noituv1.GameOver{
			IWon:        iWon,
			Reason:      reason,
			MyScore:     uint32(state.Scores[id]),
			ChainLength: uint32(state.ChainLength),
			Suggestions: suggestions,
		},
	}}
}

// handleDisconnect holds the seat open and reports whether the notice applied.
//
// A dropped connection is not a player leaving. The seat is kept for the
// reconnect window whether a game is running or the room is sitting in its
// lobby, so refreshing the page does not cost somebody the room they are in.
func (r *room) handleDisconnect(m disconnectInput) bool {
	s := r.seatOf(m.player)
	// A stale notice from a connection the player already replaced. Evicting
	// on it would drop the seat the new socket is sitting in.
	if s == nil || s.sess == nil || s.sess != m.sess {
		return false
	}
	s.sess = nil

	// A second seat dropping means nobody is here: during a game there is
	// nobody left to win, and in a lobby nobody left to play. The room ends
	// rather than waiting out a window with no winner to declare.
	if r.disconnected != nil && r.disconnected != s {
		r.cancel()
		return false
	}
	r.disconnected = s
	r.lobbyChanged = true

	// A player mid-game is told their opponent may be coming back, with how
	// long they have. In a lobby the same fact is part of the room's state and
	// travels with the rest of it, so there is nothing extra to send.
	live := r.engine != nil && !r.engine.Over()
	if other := r.opponentSeat(s.id); live && other != nil && other.sess != nil {
		other.sess.send(opponentLeftMsg(true, uint32(r.graceFor.Milliseconds())))
	}
	return true
}

// handleGraceExpiry decides what a reconnect window running out means.
func (r *room) handleGraceExpiry() {
	if r.disconnected == nil {
		return
	}
	// A live game is awarded first: once the seat is gone there is no opponent
	// left to award it against.
	r.endForAbandonment()
	r.vacate(r.disconnected)
	r.lobbyChanged = true
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
	// The seat keeps the name it was given. Re-reading it from the new
	// connection would let a reconnect rename a player mid-game, including
	// into their opponent's name.
	if r.disconnected == s {
		r.disconnected = nil
	}

	// Both players need the room's state again: this one to render the lobby
	// it came back to, the other to stop watching a disconnect banner for
	// somebody who is already back. The run loop sends it to both.
	r.lobbyChanged = true

	// Resumed between games, or before the first one. The lobby state above is
	// the whole answer; there is no position to replay.
	if r.inLobby() {
		return
	}
	r.sendGameStarted(s)
	if state := r.engine.Snapshot(); len(state.History) > 0 {
		last := state.History[len(state.History)-1]
		opponent := r.opponentSeat(s.id)
		s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_TurnUpdate{
			TurnUpdate: &noituv1.TurnUpdate{
				Played:          PlayedWord(last, last.Player == s.id),
				CurrentSyllable: state.Current,
				MyTurn:          state.Turn == s.id,
				DeadlineUnixMs:  state.Deadline.UnixMilli(),
				TurnSeq:         r.turnSeq,
				MyScore:         uint32(state.Scores[s.id]),
				OpponentScore:   uint32(state.Scores[opponent.id]),
				ChainLength:     uint32(state.ChainLength),
			},
		}})
	}
}

// endForAbandonment awards a live game to whoever stayed. The room itself
// survives: the winner is still sitting in it, and it is theirs to hand on or
// leave.
func (r *room) endForAbandonment() {
	if r.engine == nil || r.engine.Over() || r.disconnected == nil {
		return
	}
	// Resign on the absent player's behalf, then report the transport reason
	// rather than the engine's: from the winner's side this is an opponent who
	// left, not one who chose to give up.
	r.engine.Resign(r.disconnected.id)

	state := r.engine.Snapshot()
	for _, s := range r.seats {
		if s.sess == nil {
			continue
		}
		s.sess.send(r.gameOverFor(state, s.id, noituv1.GameEndReason_GAME_END_REASON_OPPONENT_LEFT))
	}
	r.lobbyChanged = true
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

// seatIDs are the two engine seat names, indexed by position. An id says which
// seat a player is in and nothing about their role: an owner who leaves hands
// that on, and the seat they vacate is refilled by an ordinary guest.
var seatIDs = [2]game.PlayerID{"p1", "p2"}

func (r *room) ownerSeat() *seat { return r.seatOf(r.owner) }

// guestSeat is the seat that is not the owner's, or nil when nobody else is
// here.
func (r *room) guestSeat() *seat {
	for _, s := range r.seats {
		if s != nil && s.id != r.owner {
			return s
		}
	}
	return nil
}

// otherNickname is the name already taken in this room, so a joiner can be
// distinguished from it.
func (r *room) otherNickname(mine game.PlayerID) string {
	for _, s := range r.seats {
		if s != nil && s.id != mine {
			return s.nickname
		}
	}
	return ""
}

// canStart reports whether StartGame would be accepted. The server answers
// this rather than the client because it owns every condition that feeds it.
func (r *room) canStart() bool {
	if r.strategy != nil || !r.inLobby() {
		return false
	}
	owner, guest := r.ownerSeat(), r.guestSeat()
	return owner != nil && owner.sess != nil &&
		guest != nil && guest.sess != nil && guest.ready
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
	if r.disconnected == s {
		r.disconnected = nil
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

// broadcastRoomState sends the whole lobby to each occupant.
//
// Built per recipient because every field in it is relative to who is being
// told: their role, their readiness, and the other player. One snapshot rather
// than a stream of deltas is what lets a client that missed a frame - or has
// just reconnected - be correct again from the next one.
func (r *room) broadcastRoomState() {
	canStart := r.canStart()

	for i, s := range r.seats {
		if s == nil || s.sess == nil {
			continue
		}
		other := r.seats[1-i]
		state := &noituv1.RoomState{
			RoomCode:        r.code,
			IAmOwner:        s.id == r.owner,
			CanStart:        canStart,
			IAmReady:        s.ready,
			OpponentPresent: other != nil,
		}
		if other != nil {
			state.OpponentName = other.nickname
			state.OpponentReady = other.ready
			state.OpponentConnected = other.sess != nil
		}
		s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_RoomState{RoomState: state}})
	}
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

func (r *room) opponentSeat(p game.PlayerID) *seat {
	for i, s := range r.seats {
		if s != nil && s.id == p {
			return r.seats[1-i]
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
