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

// defaultRematchWindow is how long a finished room waits for both players to
// ask for another game when nothing else is configured. It bounds how long a
// room outlives its game.
const defaultRematchWindow = 30 * time.Second

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

// rematchInput is one player asking to play the same room again.
type rematchInput struct {
	sess   *session
	player game.PlayerID
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
	// wantsRematch is this seat's answer to the offer that opens when a game
	// ends. Cleared whenever a new game starts.
	wantsRematch bool
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

	hub          *hub
	dict         Dictionary
	engine       *game.Engine
	opening      string
	strategy     bot.Strategy
	turnLimit    time.Duration
	graceFor     time.Duration
	rematchAfter time.Duration

	seats [2]*seat

	// turnSeq increments on every turn change. A client stamps its submission
	// with the sequence it was answering, so a move that crosses the deadline
	// is identifiable rather than silently applied to the next turn.
	turnSeq uint32

	// disconnected is the seat currently inside its reconnect grace window,
	// or nil. Only one seat can be waiting: if the second also drops, there is
	// nobody left to win and the room ends.
	disconnected *seat

	// rematchUntil is when the offer that follows a finished game expires, or
	// the zero time when no offer is open. It is the one flag that keeps a
	// room alive past its game.
	rematchUntil time.Time
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

func newRoom(h *hub, code string, turnLimit, graceFor, rematchAfter time.Duration) *room {
	if rematchAfter <= 0 {
		rematchAfter = defaultRematchWindow
	}
	ctx, cancel := context.WithCancel(h.ctx)
	return &room{
		code:         code,
		inputs:       make(chan any, roomInputCap),
		ctx:          ctx,
		cancel:       cancel,
		hub:          h,
		dict:         h.dict,
		turnLimit:    turnLimit,
		graceFor:     graceFor,
		rematchAfter: rematchAfter,
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

	var turnTimer, graceTimer, rematchTimer *time.Timer
	stop := func(t *time.Timer) {
		if t != nil {
			t.Stop()
		}
	}
	defer func() {
		stop(turnTimer)
		stop(graceTimer)
		stop(rematchTimer)
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

	for {
		var turnC, graceC, rematchC <-chan time.Time
		if turnTimer != nil {
			turnC = turnTimer.C
		}
		if graceTimer != nil {
			graceC = graceTimer.C
		}
		if rematchTimer != nil {
			rematchC = rematchTimer.C
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
				// Leaving is how a rematch is declined, so a player who drops
				// while the offer is open ends the room rather than leaving
				// the other one watching a countdown that cannot resolve.
				//
				// handleDisconnect returning false means the notice was stale —
				// from a connection the seat no longer holds — and acting on
				// that would end a room whose players are both still here.
				if r.offeringRematch() {
					if applied, _ := r.handleDisconnect(m); applied {
						r.abandonRematch()
						return
					}
					break
				}
				if _, live := r.handleDisconnect(m); live {
					stop(graceTimer)
					graceTimer = time.NewTimer(r.graceFor)
				}
				resetTurnTimer()
			case resumeInput:
				r.handleResume(m)
				stop(graceTimer)
				graceTimer = nil
				resetTurnTimer()
			case rematchInput:
				r.handleRematch(m, time.Now())
				// A rematch that both sides accepted has already started a new
				// game, so the turn clock has to come back with it.
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
			r.endForAbandonment()
			return

		case <-rematchC:
			// Nobody, or only one of them, asked in time.
			r.abandonRematch()
			return
		}

		// Every way a game can end arrives here: a move, a resignation, a
		// disconnection, or the turn clock. A finished game closes the room
		// unless both players are still present to be asked for another. The
		// offer check keeps this from firing again while one is already open.
		if r.engine != nil && r.engine.Over() && !r.offeringRematch() {
			if !r.offerRematch(time.Now()) {
				return
			}
			stop(rematchTimer)
			rematchTimer = time.NewTimer(r.rematchAfter)
		}
		if !r.offeringRematch() {
			stop(rematchTimer)
			rematchTimer = nil
		}
	}
}

// handleCreate seats the room's creator and waits for an opponent.
func (r *room) handleCreate(m createInput) {
	r.seats[0] = &seat{id: "p1", nickname: m.sess.nickname(), sess: m.sess}
	m.sess.attach(r, "p1")
	m.sess.send(roomCreatedMsg(r.code))
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
	m.sess.attach(r, "p1")

	if err := r.beginGame(); err != nil {
		slog.Error("could not start bot game", "room", r.code, "err", err)
		m.sess.send(errorMsg("game_start_failed"))
		r.cancel()
	}
}

// handleJoin seats the second human and starts the game.
//
// The seat is bound here, on the room goroutine, and only on success. Binding
// it in the hub before this decision would leave a refused joiner still
// holding seat "p2", and every later Submit or Resign it sent would be applied
// to the real player sitting there.
func (r *room) handleJoin(m joinInput) {
	if r.seats[0] == nil || r.seats[1] != nil {
		m.sess.send(errorMsg("room_full"))
		return
	}
	if r.seats[0].sess == m.sess {
		m.sess.send(errorMsg("cannot_join_own_room"))
		return
	}
	r.seats[1] = &seat{
		id:       "p2",
		nickname: distinguish(m.sess.nickname(), r.seats[0].nickname),
		sess:     m.sess,
	}
	m.sess.attach(r, "p2")

	for i, s := range r.seats {
		if s.sess != nil {
			s.sess.send(roomJoinedMsg(r.code, r.seats[1-i].nickname))
		}
	}
	if err := r.beginGame(); err != nil {
		slog.Error("could not start pvp game", "room", r.code, "err", err)
		r.broadcastError("game_start_failed")
		r.cancel()
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
	for _, s := range r.seats {
		s.wantsRematch = false
	}
	r.rematchUntil = time.Time{}

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

// handleDisconnect holds the seat open, reporting whether a grace window
// should now run.
// handleDisconnect reports two separate things, because the caller needs both
// and they are not the same question: whether the notice actually applied to
// the seat, and whether a game is still running that the player could come
// back to. A disconnection during a rematch offer applies and is not live.
func (r *room) handleDisconnect(m disconnectInput) (applied, live bool) {
	s := r.seatOf(m.player)
	// A stale notice from a connection the player already replaced. Evicting
	// on it would drop the seat the new socket is sitting in.
	if s == nil || s.sess == nil || s.sess != m.sess {
		return false, false
	}
	s.sess = nil

	// Both sides gone: nobody is left to win, so there is nothing to hold the
	// room open for.
	if r.disconnected != nil && r.disconnected != s {
		r.cancel()
		return true, false
	}
	r.disconnected = s

	// Before the game starts there is no turn timer and no opponent, so no
	// clock can ever end this room. Without this it would sit in select
	// forever, holding a goroutine and a room code for a game nobody is in.
	if r.engine == nil {
		r.cancel()
		return true, false
	}

	live = !r.engine.Over()
	if other := r.opponentSeat(s.id); other != nil && other.sess != nil {
		// can_reconnect only means something while there is a game to come
		// back to. Promising it after the final move contradicts the frame
		// that follows it.
		other.sess.send(opponentLeftMsg(live, uint32(r.graceFor.Milliseconds())))
	}
	return true, live
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
	// A finished game has no seat to take, including one still waiting on a
	// rematch answer. The old connection stays exactly as it was.
	if r.engine != nil && r.engine.Over() {
		m.sess.send(errorMsg("game_already_over"))
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

	// Tell the other player their opponent is back. Without this the seat is
	// restored but the waiting player is left watching a disconnect banner for
	// somebody who is already playing again.
	if other := r.opponentSeat(s.id); other != nil && other.sess != nil {
		other.sess.send(roomJoinedMsg(r.code, s.nickname))
	}

	// Resumed into a room whose game has not started: the seat is restored and
	// the client waits for an opponent exactly as it was.
	if r.engine == nil {
		s.sess.send(roomCreatedMsg(r.code))
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

// endForAbandonment awards the game to whoever stayed.
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
}

// offerRematch decides what a finished game means for the room, and reports
// whether the room should keep running.
//
// Only a room with two connected humans can offer one. A bot room has nothing
// to negotiate — the client simply asks for another game — and a room whose
// opponent has already gone has nobody to ask.
func (r *room) offerRematch(now time.Time) bool {
	if r.strategy != nil {
		return false
	}
	for _, s := range r.seats {
		if s == nil || s.sess == nil {
			return false
		}
		s.wantsRematch = false
	}

	r.rematchUntil = now.Add(r.rematchAfter)
	r.broadcastRematchState(now)
	return true
}

// offeringRematch reports whether an offer is currently open.
func (r *room) offeringRematch() bool { return !r.rematchUntil.IsZero() }

// handleRematch records one player's answer and starts the next game once both
// have given it.
func (r *room) handleRematch(m rematchInput, now time.Time) {
	if !r.occupies(m.sess, m.player) {
		m.sess.send(errorMsg("not_your_seat"))
		return
	}
	if !r.offeringRematch() {
		m.sess.send(errorMsg("no_rematch_offered"))
		return
	}

	s := r.seatOf(m.player)
	s.wantsRematch = true

	if !r.seats[0].wantsRematch || !r.seats[1].wantsRematch {
		r.broadcastRematchState(now)
		return
	}

	// beginGame clears the offer and the acceptances, so the state broadcast
	// above is not repeated here: GameStarted is the answer.
	if err := r.beginGame(); err != nil {
		slog.Error("could not start rematch", "room", r.code, "err", err)
		r.broadcastError("game_start_failed")
		r.cancel()
	}
}

// broadcastRematchState tells each player where both answers stand. It is
// built per recipient because "mine" and "theirs" are different for each.
func (r *room) broadcastRematchState(now time.Time) {
	left := uint32(max(0, r.rematchUntil.Sub(now).Milliseconds()))

	for i, s := range r.seats {
		if s == nil || s.sess == nil {
			continue
		}
		s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_RematchState{
			RematchState: &noituv1.RematchState{
				IAccepted:        s.wantsRematch,
				OpponentAccepted: r.seats[1-i].wantsRematch,
				ExpiresInMs:      left,
			},
		}})
	}
}

// abandonRematch tells whoever is still here that the offer is dead, so their
// countdown resolves into an answer instead of just running out.
func (r *room) abandonRematch() {
	for _, s := range r.seats {
		if s == nil || s.sess == nil {
			continue
		}
		s.sess.send(opponentLeftMsg(false, 0))
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
