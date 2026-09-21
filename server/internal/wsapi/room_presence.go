package wsapi

import (
	"time"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
)

// Presence: a seat's reconnect window, opening it, closing it, and what a
// resume does once a connection comes back inside one.

// disconnectGhostSeat opens the seat's reconnect window the moment it is
// filled, for a connection that turns out to have already torn down.
//
// The session can die between the hub handing this room the seating message
// and the room goroutine draining it off the queue — nothing else ever learns
// that, because leaveRoom only notifies a room the session was already
// attached to, and attaching is exactly what has not happened yet. Left
// seated as if connected, allConnected() would report true and quick match's
// own auto-start (see handleJoin) could begin a game against a socket nobody
// is behind. Applying the same grace window handleDisconnect would reuses the
// one mechanism that already bounds this instead of adding a second one.
func (r *room) disconnectGhostSeat(s *seat) {
	s.sess = nil
	s.graceUntil = time.Now().Add(r.graceFor)
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
	metrics.resumesSucceeded.Add(1)
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

	if m.sess.ctx.Err() != nil {
		// The new connection can die between the client's Hello landing and
		// this resume being drained off the room's queue, the same race
		// handleCreate and handleJoin guard against. Reopening the window it
		// just closed leaves the seat exactly as reachable as it was before
		// this resume was ever attempted.
		r.disconnectGhostSeat(s)
		return
	}

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
	if last, ok := r.engine.LastMove(); ok {
		r.sendTurnUpdate(s, state, &last, r.moveMeanings(&last))
	}
}

// detachAll releases every connection still bound to this room as it exits.
func (r *room) detachAll() {
	for _, s := range r.seats {
		if s != nil && s.sess != nil {
			s.sess.release(r)
		}
	}
}
