package wsapi

import (
	"log/slog"
	"time"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/game"
)

// Seating and the lobby between games: who is in the room, whether they may
// start, and what happens when one of them leaves or is kicked.

// handleCreate seats the room's creator, who owns it, and opens the lobby.
//
// The code goes out in the RoomState the run loop broadcasts, so a client can
// never be handed a code before the seat behind it exists.
func (r *room) handleCreate(m createInput) {
	s := &seat{id: "p1", nickname: m.sess.nickname(), sess: m.sess, chatFrom: r.chatSeq}
	r.seats[0] = s
	r.owner = "p1"
	r.autoStart = m.autoStart
	m.sess.attach(r, "p1")
	r.lobbyChanged = true
	// A quick match already popped this session off the pairing queue before
	// sending it here, but a plain CreateRoom might still be seating somebody
	// who was also waiting in it from another attempt — one dequeue serves
	// both room-entry paths.
	r.hub.cancelQuickMatch(m.sess)

	if s.sess.ctx.Err() != nil {
		r.disconnectGhostSeat(s)
		return
	}
	// Deliberately sent to a brand-new room's creator, where it is always
	// empty: it is what replaces the conversation a client may still be
	// holding from a room it was in before this one.
	r.sendChatHistory(s)
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
		metrics.joinsRefused.Add("room_full", 1)
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
	s := &seat{
		id:       id,
		nickname: distinguish(m.sess.nickname(), r.takenNicknames(id)),
		sess:     m.sess,
		// Seated now, so the conversation up to this point is not theirs to
		// read. A room code is pasted into group chats by design.
		chatFrom: r.chatSeq,
	}
	r.seats[free] = s
	m.sess.attach(r, string(id))
	r.lobbyChanged = true
	r.hub.cancelQuickMatch(m.sess)

	if s.sess.ctx.Err() != nil {
		r.disconnectGhostSeat(s)
		return
	}
	r.sendChatHistory(s)

	// A quick match seats both players itself rather than waiting on
	// readiness and StartGame — there is no owner here to press it, only two
	// strangers who both already asked to be matched. The lobby is shown
	// first, with both seats filled, so the wait ends on an ordinary room a
	// beat before GameStarted rather than jumping straight into one with no
	// seating frame behind it.
	if r.autoStart && r.seatedCount() >= minPlayers && r.allConnected() {
		if r.hub.isDraining() {
			// The second seat filled after the drain decision. There is no
			// owner here to answer with server_restarting the way lobbyStart
			// does, so both seats are told directly; the lobby view they are
			// left in still shows each other, via lobbyChanged below.
			r.broadcastError("server_restarting")
			return
		}
		r.autoStart = false
		r.lobbyChanged = false
		r.broadcastRoomState()
		if err := r.beginGame(); err != nil {
			slog.Error("could not start quick-matched game", "room", r.code, "err", err)
			r.broadcastError("game_start_failed")
		}
	}
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
		case r.hub.isDraining():
			// newRegisteredRoom already refuses a brand-new room once draining
			// starts; this lobby existed before that point, and starting its
			// game now would raise hub.liveGames after the drain decided how
			// long to wait for exactly that number to reach zero.
			m.sess.send(errorMsg("server_restarting"))
			return
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

// seatIDs are the engine seat names, indexed by position. An id says which
// seat a player is in and nothing about their role: an owner who leaves hands
// that on, and the seat they vacate is refilled by an ordinary guest.
var seatIDs = [maxPlayers]game.PlayerID{"p1", "p2", "p3", "p4"}
