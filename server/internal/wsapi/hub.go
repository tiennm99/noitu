package wsapi

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tiennm99dev/noitu/server/internal/bot"
	"github.com/tiennm99dev/noitu/server/internal/game"
)

// roomCodeAlphabet omits 0/O and 1/I/L. Players read these codes aloud and
// retype them from a screenshot, so the characters that get confused are worth
// more than the extra entropy they would add.
const roomCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

const roomCodeLen = 6

// codeAttempts bounds the retry loop on collision. With a 31-character
// alphabet over 6 places, exhausting this many draws means the room table is
// far past any load this server is built for.
const codeAttempts = 10

var (
	errRoomNotFound = errors.New("wsapi: no such room")
	errNoRoomCode   = errors.New("wsapi: could not allocate a room code")
	errServerFull   = errors.New("wsapi: room limit reached")
	// errDraining is returned instead of errServerFull once the process has
	// started shutting down, so the creator is told to come back rather than
	// to wait — a full room fills back up, a draining one never will.
	errDraining = errors.New("wsapi: server draining")
	// errAlreadyQueued answers a second QuickMatch from a session already
	// waiting in the pairing queue.
	errAlreadyQueued = errors.New("wsapi: already queued for quick match")
)

// defaultMaxRooms bounds live rooms across the whole process when nothing else
// is configured. Each room is a goroutine, an engine and a registry entry held
// for up to the idle window, so without a ceiling the per-connection limiter
// only sets the rate at which a fleet of connections can fill memory.
const defaultMaxRooms = 1000

// hub owns the registries and nothing else.
//
// It never touches a game: rooms are handed out as pointers whose channels are
// the only way in. Keeping engine state out of the mutex is what stops the
// lock from becoming a bottleneck on every move, and what makes the
// one-goroutine-per-room rule enforceable by inspection.
type hub struct {
	ctx  context.Context
	dict Dictionary

	turnLimit time.Duration
	graceFor  time.Duration
	idleFor   time.Duration
	maxRooms  int

	mu       sync.Mutex
	rooms    map[string]*room
	sessions map[string]*session // by resume token

	// waiting is the FIFO of sessions queued for a quick match. No key and no
	// skill: the pool this server serves is small enough that "the next
	// stranger who also asked" is the whole matching policy. Guarded by mu
	// rather than a lock of its own — the hub is not a bottleneck any of this
	// adds meaningful contention to.
	waiting []*session

	joinLimiter *keyedLimiter

	// draining refuses every new room once set, so a creator is told to come
	// back rather than being seated in a room the shutdown below is about to
	// end anyway. Read and written from outside the hub's own goroutine (there
	// isn't one), so it is atomic rather than mutex-guarded.
	draining atomic.Bool

	// liveGames counts rooms with a game actually running, as opposed to
	// sitting in their lobby. Draining waits for this to reach zero rather
	// than for the room count to, because an empty lobby has nothing a
	// restart costs and waiting for it would make every deploy sit out
	// somebody's abandoned tab.
	liveGames atomic.Int64
}

func newHub(ctx context.Context, dict Dictionary, turnLimit, graceFor, idleFor time.Duration, maxRooms int) *hub {
	if maxRooms <= 0 {
		maxRooms = defaultMaxRooms
	}
	return &hub{
		ctx:         ctx,
		dict:        dict,
		turnLimit:   turnLimit,
		graceFor:    graceFor,
		idleFor:     idleFor,
		maxRooms:    maxRooms,
		rooms:       map[string]*room{},
		sessions:    map[string]*session{},
		joinLimiter: newKeyedLimiter(joinsPerSecond, joinBurst, limiterIdleFor),
	}
}

// register records a session so a later Hello can resume it.
func (h *hub) register(s *session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions[s.resumeToken] = s
}

// unregister drops a session's resume entry once its grace window has passed.
func (h *hub) unregister(token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, token)
}

// expireToken drops a resume entry once its grace window has passed, so a
// disconnected player can reclaim their seat until then and the map does not
// grow for every connection ever made.
func (h *hub) expireToken(token string, after time.Duration) {
	time.AfterFunc(after, func() { h.unregister(token) })
}

// resumable returns the prior session for a token, if it is still eligible.
func (h *hub) resumable(token string) (*session, bool) {
	if token == "" {
		return nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.sessions[token]
	return s, ok
}

// startBotRoom creates a room already in play against the bot.
//
// The room goroutine starts before anyone is seated, and the seating itself is
// a message. That ordering is what lets the engine-ownership invariant be
// checked by reading run(), rather than by reasoning about which writes
// happened to precede a `go` statement.
func (h *hub) startBotRoom(s *session, difficulty bot.Difficulty) error {
	r, err := h.newRegisteredRoom(roomModeBot)
	if err != nil {
		return err
	}
	r.send(startBotInput{sess: s, difficulty: difficulty})
	return nil
}

// createRoom opens an empty PvP room. The room announces its own code once the
// creator is seated, so a client can never receive the code before the seat
// behind it exists.
func (h *hub) createRoom(s *session) error {
	r, err := h.newRegisteredRoom(roomModePvP)
	if err != nil {
		return err
	}
	r.send(createInput{sess: s})
	return nil
}

// quickMatch pairs s with the next stranger waiting, or queues it as that
// stranger for whoever asks next.
//
// A match sends both sides their QuickMatchStatus itself, before either
// input reaches the room: the room's own messages — RoomState, then
// GameStarted — are sent from its goroutine afterwards, and doing the status
// sends here first is what guarantees neither of them can arrive still
// claiming "queued". The enqueue path sends its own status for the same
// reason, symmetry, and because the caller has nobody else to hear from.
func (h *hub) quickMatch(s *session) error {
	h.mu.Lock()
	for _, w := range h.waiting {
		if w == s {
			h.mu.Unlock()
			return errAlreadyQueued
		}
	}
	// A waiter whose connection ended is skipped, not paired. Its teardown
	// dequeues it, but that runs after the socket has gone, and in that
	// window the queue still names a session nobody is behind; seating it
	// would start a game against an empty chair.
	var waiter *session
	for waiter == nil && len(h.waiting) > 0 {
		candidate := h.waiting[0]
		h.waiting = h.waiting[1:]
		if candidate.ctx.Err() == nil {
			waiter = candidate
		}
	}
	if waiter == nil {
		h.waiting = append(h.waiting, s)
		h.mu.Unlock()
		metrics.quickMatchQueued.Add(1)
		s.send(quickMatchStatusMsg(true))
		return nil
	}
	h.mu.Unlock()

	r, err := h.newRegisteredRoom(roomModePvP)
	if err != nil {
		// The waiter has no dispatch call site of its own to answer this
		// through, being the caller of an earlier message; s is told by
		// session.dispatch's own roomCreateError path instead.
		waiter.send(roomCreateError(waiter.id, err))
		return err
	}

	metrics.quickMatchMatched.Add(1)
	waiter.send(quickMatchStatusMsg(false))
	s.send(quickMatchStatusMsg(false))
	// autoStart carries through createInput because it is a room field the
	// goroutine sets for itself from handleCreate — nothing outside that
	// goroutine ever touches it directly.
	r.send(createInput{sess: waiter, autoStart: true})
	r.send(joinInput{sess: s})
	return nil
}

// cancelQuickMatch drops s from the pairing queue if it is there. Idempotent:
// called on a teardown or a room-entry path that may or may not have found it
// queued, and neither is worth a special case at the call site.
func (h *hub) cancelQuickMatch(s *session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, w := range h.waiting {
		if w == s {
			h.waiting = append(h.waiting[:i], h.waiting[i+1:]...)
			metrics.quickMatchCancelled.Add(1)
			return
		}
	}
}

// joinRoom offers a second player to a room. Whether they are seated is the
// room's decision, not the hub's.
func (h *hub) joinRoom(code string, s *session) error {
	h.mu.Lock()
	r, ok := h.rooms[code]
	h.mu.Unlock()
	if !ok {
		return errRoomNotFound
	}
	if !r.send(joinInput{sess: s}) {
		return errRoomNotFound
	}
	return nil
}

// newRegisteredRoom allocates a code, registers the room and starts it.
//
// mode is known here, before the room goroutine has processed a single input,
// because it is the hub method called — startBotRoom or createRoom — that
// decides it. Counting the room live from this point rather than from
// handleCreate/handleStartBot is deliberately generous: a room that fails to
// seat its creator still held a goroutine and a registry entry for a moment,
// and the gauge should say so.
func (h *hub) newRegisteredRoom(mode string) (*room, error) {
	if h.draining.Load() {
		return nil, errDraining
	}

	code, err := h.reserveCode()
	if err != nil {
		return nil, err
	}

	r := newRoom(h, code, h.turnLimit, h.graceFor, h.idleFor, mode)

	// The ceiling is checked under the same lock that registers the room, so
	// two creators racing for the last slot cannot both get it.
	h.mu.Lock()
	if len(h.rooms) >= h.maxRooms {
		h.mu.Unlock()
		r.cancel()
		return nil, errServerFull
	}
	h.rooms[code] = r
	h.mu.Unlock()

	metrics.roomsTotal.Add(mode, 1)
	metrics.roomsLive.Add(mode, 1)

	go r.run()
	return r, nil
}

// roomCount is how many rooms are live right now.
func (h *hub) roomCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.rooms)
}

// startDraining stops the hub from seating any new room. Existing rooms are
// untouched here — telling them to stop is the caller's job, once it has
// decided how long to wait for the ones with a game running.
func (h *hub) startDraining() { h.draining.Store(true) }

// isDraining reports whether startDraining has been called, which is what
// /readyz answers with.
func (h *hub) isDraining() bool { return h.draining.Load() }

// liveGameCount is how many rooms currently have a game running, as opposed
// to sitting in their lobby.
func (h *hub) liveGameCount() int64 { return h.liveGames.Load() }

// gameStarted and gameFinished keep liveGameCount accurate. A room calls
// gameStarted when its engine is built and gameFinished exactly once for
// every gameStarted — including when the room is cancelled mid-game rather
// than finishing normally, which is why room.run's teardown carries its own
// call rather than relying on broadcastGameOver alone.
func (h *hub) gameStarted()  { h.liveGames.Add(1) }
func (h *hub) gameFinished() { h.liveGames.Add(-1) }

// evict removes a finished room. Called by the room goroutine as it exits, so
// a code is reusable the moment its game is done.
func (h *hub) evict(code string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.rooms, code)
}

// reserveCode draws an unused room code.
//
// crypto/rand, not math/rand: a predictable code lets someone walk into a
// stranger's private game, which is a guessing attack on a 6-character secret
// rather than a fairness question.
func (h *hub) reserveCode() (string, error) {
	for range codeAttempts {
		code := randomCode()

		h.mu.Lock()
		_, taken := h.rooms[code]
		h.mu.Unlock()

		if !taken {
			return code, nil
		}
	}
	return "", errNoRoomCode
}

// randomCode draws a uniformly distributed room code.
//
// Rejection sampling rather than a modulo: 256 is not a multiple of 31, so
// `b % 31` would make the first eight letters 12% more likely than the rest.
// The bias is small, but the cost of removing it is one comparison, and it
// lets the entropy claim above be stated without an asterisk.
func randomCode() string {
	const limit = 256 - (256 % len(roomCodeAlphabet)) // 248

	out := make([]byte, 0, roomCodeLen)
	buf := make([]byte, roomCodeLen)
	for len(out) < roomCodeLen {
		// rand.Read from crypto/rand cannot fail; it panics internally instead.
		_, _ = rand.Read(buf)
		for _, b := range buf {
			if int(b) >= limit {
				continue // would bias the low letters
			}
			out = append(out, roomCodeAlphabet[int(b)%len(roomCodeAlphabet)])
			if len(out) == roomCodeLen {
				break
			}
		}
	}
	return string(out)
}

// shutdown tells every live room to stop, so clients learn why rather than
// finding the socket gone.
//
// A quick-match waiter needs nothing extra here: it registered with the hub
// the moment its Hello landed, same as any other connected session, so the
// loop below already reaches it.
func (h *hub) shutdown() {
	h.mu.Lock()
	rooms := make([]*room, 0, len(h.rooms))
	for _, r := range h.rooms {
		rooms = append(rooms, r)
	}
	sessions := make([]*session, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.waiting = nil
	h.mu.Unlock()

	for _, s := range sessions {
		s.send(errorMsg("server_restarting"))
	}
	for _, r := range rooms {
		r.cancel()
	}
}

// playerIDFor is the seat a session holds. Declared here because the mapping
// between a connection and a seat is registry knowledge, not game knowledge.
func playerIDFor(seatName string) game.PlayerID { return game.PlayerID(seatName) }
