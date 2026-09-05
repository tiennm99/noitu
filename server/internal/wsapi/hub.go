package wsapi

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
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
)

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

	mu       sync.Mutex
	rooms    map[string]*room
	sessions map[string]*session // by resume token

	joinLimiter *keyedLimiter
}

func newHub(ctx context.Context, dict Dictionary, turnLimit, graceFor time.Duration) *hub {
	return &hub{
		ctx:         ctx,
		dict:        dict,
		turnLimit:   turnLimit,
		graceFor:    graceFor,
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
	r, err := h.newRegisteredRoom()
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
	r, err := h.newRegisteredRoom()
	if err != nil {
		return err
	}
	r.send(createInput{sess: s})
	return nil
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
func (h *hub) newRegisteredRoom() (*room, error) {
	code, err := h.reserveCode()
	if err != nil {
		return nil, err
	}

	r := newRoom(h, code, h.turnLimit, h.graceFor)

	h.mu.Lock()
	h.rooms[code] = r
	h.mu.Unlock()

	go r.run()
	return r, nil
}

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
	h.mu.Unlock()

	for _, s := range sessions {
		s.send(errorMsg("server_restarting"))
	}
	for _, r := range rooms {
		r.cancel()
	}
}

// roomCount reports live rooms. Tests use it to assert eviction; nothing in
// the server depends on it.
func (h *hub) roomCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.rooms)
}

// playerIDFor is the seat a session holds. Declared here because the mapping
// between a connection and a seat is registry knowledge, not game knowledge.
func playerIDFor(seatName string) game.PlayerID { return game.PlayerID(seatName) }
