package wsapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/game"
)

const (
	// outboxCap buffers writes. A client that cannot keep up with this many
	// pending frames is not going to catch up, so the session is closed rather
	// than grown without bound.
	outboxCap = 32

	// pingEvery / pingTimeout are the liveness check. Reads carry no deadline
	// of their own: a player idling in the lobby between games is healthy and
	// silent, and a read timeout cannot tell that apart from a dead socket.
	// A ping can.
	pingEvery   = 20 * time.Second
	pingTimeout = 10 * time.Second
	pingMisses  = 2

	// writeTimeout bounds a single frame write, and drainTimeout the final
	// flush of whatever is still queued when the session ends.
	writeTimeout = 10 * time.Second
	drainTimeout = 2 * time.Second

	// submitsPerSecond bounds word submissions. Each one is a dictionary
	// lookup and a possible engine mutation; a human types far below this.
	submitsPerSecond = 5
	submitBurst      = 10

	// Chat gets its own budget so talking never costs a move. It can afford to
	// be humane about a burst — two people typing at each other is normal —
	// because the danger a limiter would otherwise be holding down is handled
	// where it actually lives: chat is delivered with trySend, so a recipient
	// who cannot keep up drops a line rather than losing their session.
	chatsPerSecond = 2.0
	chatBurst      = 5

	joinsPerSecond = 1
	joinBurst      = 5

	// Room creation is far more expensive than a join: each one is a
	// goroutine, an engine and a registry entry held until the game ends.
	roomsPerSecond = 0.2
	roomBurst      = 5
	limiterIdleFor = 5 * time.Minute
)

var errHandshake = errors.New("wsapi: first message must be Hello")

// session is one WebSocket connection.
//
// Exactly one reader goroutine and one writer goroutine touch the socket. The
// mutex below guards only the small mutable identity — nickname, room, seat —
// that the reader sets and the room goroutine reads.
type session struct {
	id          string
	resumeToken string
	conn        *websocket.Conn
	hub         *hub

	// remoteIP keys the join limiter. A session id is minted per connection,
	// so keying on it would let anyone brute-force room codes by reconnecting
	// between attempts — the limiter has to outlive the socket.
	remoteIP string

	// ctx is the teardown signal for everything except the read.
	ctx    context.Context
	cancel context.CancelFunc

	// readCtx is cancelled only after the writer has finished flushing.
	//
	// coder/websocket arms a context.AfterFunc on the context passed to Read
	// that hard-closes the underlying socket when it fires, so cancelling the
	// read context is the same as destroying the connection. Every frame
	// queued at teardown — the shutdown notice above all — would be written
	// into a socket that is already gone.
	//
	// It is rooted at Background rather than at the server context on purpose.
	// A child of the server context would be cancelled by Shutdown at the same
	// instant as ctx, which is precisely the ordering this exists to prevent.
	// Nothing leaks: run always signals ctx, and the goroutine watching it
	// always cancels this one.
	readCtx    context.Context
	cancelRead context.CancelFunc

	out chan []byte

	// flushed closes when the writer has drained, so teardown can wait for the
	// last frames to leave before tearing the socket down.
	flushed chan struct{}

	mu       sync.Mutex
	nick     string
	room     *room
	playerID game.PlayerID

	submitLimiter *bucket
	roomLimiter   *bucket
	chatLimiter   *bucket

	// greeted marks the handshake done. It is a one-shot transition: a second
	// Hello would re-register the session and rewrite its nickname mid-game.
	greeted bool

	closeOnce sync.Once
}

func newSession(ctx context.Context, conn *websocket.Conn, h *hub, remoteIP string) *session {
	readCtx, cancelRead := context.WithCancel(context.Background())
	ctx, cancel := context.WithCancel(ctx)
	return &session{
		readCtx:       readCtx,
		cancelRead:    cancelRead,
		flushed:       make(chan struct{}),
		id:            randomToken(),
		resumeToken:   randomToken(),
		remoteIP:      remoteIP,
		conn:          conn,
		hub:           h,
		ctx:           ctx,
		cancel:        cancel,
		out:           make(chan []byte, outboxCap),
		submitLimiter: newBucket(submitsPerSecond, submitBurst, time.Now()),
		roomLimiter:   newBucket(roomsPerSecond, roomBurst, time.Now()),
		chatLimiter:   newBucket(chatsPerSecond, chatBurst, time.Now()),
	}
}

func (s *session) nickname() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nick
}

func (s *session) setNickname(n string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nick = n
}

// attach binds this connection to a room seat.
func (s *session) attach(r *room, seatName string) {
	s.mu.Lock()
	previous, previousID := s.room, s.playerID
	s.room = r
	s.playerID = playerIDFor(seatName)
	s.mu.Unlock()

	// Releasing the old room is not tidiness. Nothing else tells it this
	// connection has gone: leaveRoom only ever notifies the current room, so an
	// unreleased room parks in select forever, holding a goroutine and a room
	// code for the life of the process. One connection asking for several rooms
	// is all it takes.
	if previous != nil && previous != r {
		previous.send(disconnectInput{player: previousID, sess: s})
	}
}

// release forgets a room this connection is no longer seated in, because it
// left or was kicked. The connection itself stays open.
//
// Guarded by identity: a release from a room the connection has already moved
// on from must not detach it from the one it is sitting in now.
func (s *session) release(r *room) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.room == r {
		s.room = nil
		s.playerID = ""
	}
}

func (s *session) currentRoom() (*room, game.PlayerID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.room, s.playerID
}

// send queues a message for the writer goroutine.
//
// Never blocks: the room goroutine calls this, and one unresponsive client
// must not be able to stall the game its opponent is still playing. A full
// outbox closes the session instead.
func (s *session) send(m *noituv1.ServerMessage) {
	raw, err := Encode(m)
	if err != nil {
		slog.Error("encode failed", "session", s.id, "err", err)
		return
	}

	select {
	case s.out <- raw:
	case <-s.ctx.Done():
	default:
		slog.Warn("outbox full, closing session", "session", s.id)
		s.close()
	}
}

// close signals teardown. It does not cancel the read context: that is done by
// run once the writer has flushed, so a client is told why it is being
// disconnected before the socket goes.
func (s *session) close() {
	s.closeOnce.Do(func() {
		s.cancel()
	})
}

// trySend queues a message and reports whether it fit.
//
// The difference from send is what a full outbox means: send closes the
// session, on the grounds that a client this far behind will not catch up.
// That is right for a game frame and wrong for a chat line, because it hands
// one player a way to disconnect the other into losing by abandonment.
//
// A line is droppable because the next replay carries it. A ChatHistory is
// not — it is the frame that corrects a whole panel, and there is nothing
// behind it — so that one still goes through send. This is for ChatMessage.
func (s *session) trySend(m *noituv1.ServerMessage) bool {
	raw, err := Encode(m)
	if err != nil {
		slog.Error("encode failed", "session", s.id, "err", err)
		return false
	}

	select {
	case s.out <- raw:
		return true
	case <-s.ctx.Done():
		return false
	default:
		slog.Warn("outbox full, dropping chat", "session", s.id)
		return false
	}
}

// run drives the connection until it closes.
func (s *session) run() {
	defer s.close()
	defer s.leaveRoom()

	s.conn.SetReadLimit(maxFrameBytes)

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); s.writeLoop() }()
	go func() { defer wg.Done(); s.keepalive() }()

	// Teardown ordering lives in its own goroutine because readLoop below is
	// blocked in Read and cannot run it. Whoever signals the close — a
	// protocol error here, a dead peer in keepalive, or Shutdown cancelling
	// the server context — gets the same sequence: flush, then drop the
	// socket.
	go func() {
		defer wg.Done()
		<-s.ctx.Done()
		select {
		case <-s.flushed:
		case <-time.After(drainTimeout):
		}
		s.cancelRead()
	}()

	err := s.readLoop()
	s.close()
	wg.Wait()

	status, reason := websocket.StatusNormalClosure, ""
	if err != nil && !errors.Is(err, context.Canceled) {
		status, reason = websocket.StatusPolicyViolation, "protocol error"
	}
	_ = s.conn.Close(status, reason)
}

// readLoop is the only reader of the socket.
func (s *session) readLoop() error {
	for {
		typ, raw, err := s.conn.Read(s.readCtx)
		if err != nil {
			return err
		}

		msg, err := Decode(typ, raw)
		if err != nil {
			s.send(errorMsg("bad_frame"))
			return err
		}
		if err := s.dispatch(msg); err != nil {
			return err
		}
	}
}

// writeLoop is the only writer of the socket. A single owner keeps frame order
// deterministic even though coder/websocket tolerates concurrent writes.
func (s *session) writeLoop() {
	defer close(s.flushed)

	for {
		select {
		case <-s.ctx.Done():
			s.drain()
			return
		case raw := <-s.out:
			if !s.write(raw) {
				return
			}
		}
	}
}

// write sends one frame.
//
// The deadline is its own, not derived from the session context. A frame that
// has already been dequeued must still reach the peer even when the session is
// ending — refusals are sent immediately before a close, and inheriting the
// cancelled context would fail every one of them. The timeout is what protects
// against a peer that has stopped reading.
func (s *session) write(raw []byte) bool {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	if err := s.conn.Write(ctx, websocket.MessageBinary, raw); err != nil {
		s.close()
		return false
	}
	return true
}

// drain flushes what is already queued after the session is cancelled.
//
// Refusals are the reason this exists: the server answers a bad handshake with
// a UI key and then closes, and without this the close wins the race and the
// client is left to guess why it was dropped. The context is fresh because
// s.ctx is by definition already cancelled here, and bounded because a peer
// that is not reading must not delay teardown.
func (s *session) drain() {
	for {
		select {
		case raw := <-s.out:
			ctx, cancel := context.WithTimeout(context.Background(), drainTimeout)
			err := s.conn.Write(ctx, websocket.MessageBinary, raw)
			cancel()
			if err != nil {
				return
			}
		default:
			return
		}
	}
}

// keepalive is what actually detects a dead peer, since reads have no deadline.
func (s *session) keepalive() {
	ticker := time.NewTicker(pingEvery)
	defer ticker.Stop()

	misses := 0
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(s.ctx, pingTimeout)
			err := s.conn.Ping(ctx)
			cancel()

			if err == nil {
				misses = 0
				continue
			}
			if misses++; misses >= pingMisses {
				slog.Info("peer unresponsive, closing", "session", s.id)
				s.close()
				return
			}
		}
	}
}

// dispatch routes one client message.
//
// Hello must come first: everything else needs a sanitized nickname and a
// registered resume token, and accepting them before the handshake would mean
// carrying "maybe not greeted yet" through every branch below.
func (s *session) dispatch(msg *noituv1.ClientMessage) error {
	if _, isHello := msg.GetPayload().(*noituv1.ClientMessage_Hello); !isHello && s.nickname() == "" {
		s.send(errorMsg("handshake_required"))
		return errHandshake
	}

	switch p := msg.GetPayload().(type) {
	case *noituv1.ClientMessage_Hello:
		return s.handleHello(p.Hello)

	case *noituv1.ClientMessage_StartBotGame:
		difficulty, ok := Difficulty(p.StartBotGame.GetDifficulty())
		if !ok {
			s.send(errorMsg("unknown_difficulty"))
			return nil
		}
		if !s.roomLimiter.allow(time.Now()) {
			s.send(errorMsg("too_many_rooms"))
			return nil
		}
		if err := s.hub.startBotRoom(s, difficulty); err != nil {
			slog.Error("start bot room", "session", s.id, "err", err)
			s.send(errorMsg("room_start_failed"))
		}

	case *noituv1.ClientMessage_CreateRoom:
		// Creating a room allocates a goroutine and an engine, so one
		// connection must not be able to mint them without limit.
		if !s.roomLimiter.allow(time.Now()) {
			s.send(errorMsg("too_many_rooms"))
			return nil
		}
		if err := s.hub.createRoom(s); err != nil {
			s.send(errorMsg("room_start_failed"))
		}

	case *noituv1.ClientMessage_JoinRoom:
		if !s.hub.joinLimiter.allow(s.remoteIP, time.Now()) {
			s.send(errorMsg("too_many_attempts"))
			return nil
		}
		if err := s.hub.joinRoom(p.JoinRoom.GetRoomCode(), s); err != nil {
			s.send(errorMsg("room_not_found"))
		}

	case *noituv1.ClientMessage_SubmitWord:
		s.handleSubmit(p.SubmitWord)

	case *noituv1.ClientMessage_Resign:
		// A silently dropped resignation leaves the player staring at a board
		// they thought they had left.
		if r, id := s.currentRoom(); r != nil {
			if !r.send(resignInput{sess: s, player: id}) {
				s.send(errorMsg("game_already_over"))
			}
		} else {
			s.send(errorMsg("not_in_a_game"))
		}

	case *noituv1.ClientMessage_SetReady:
		s.toRoom(lobbyInput{sess: s, action: lobbyReady, ready: p.SetReady.GetReady()})

	case *noituv1.ClientMessage_StartGame:
		s.toRoom(lobbyInput{sess: s, action: lobbyStart})

	case *noituv1.ClientMessage_KickPlayer:
		s.toRoom(lobbyInput{sess: s, action: lobbyKick})

	case *noituv1.ClientMessage_LeaveRoom:
		s.toRoom(lobbyInput{sess: s, action: lobbyLeave})

	case *noituv1.ClientMessage_SendChat:
		// Its own budget, so a talkative player never runs out of moves. The
		// seat itself is checked by the room, which is the only place that
		// knows whether this connection still holds one.
		if !s.chatLimiter.allow(time.Now()) {
			s.send(errorMsg("too_fast"))
			return nil
		}
		r, id := s.currentRoom()
		if r == nil {
			s.send(errorMsg("not_in_a_room"))
			return nil
		}
		// A dropped line would leave the player watching their own message
		// fail to appear with no reason given.
		if !r.send(chatInput{sess: s, player: id, text: p.SendChat.GetText()}) {
			s.send(errorMsg("busy"))
		}

	case *noituv1.ClientMessage_Ping:
		s.send(pongMsg(p.Ping.GetClientTimeMs(), time.Now().UnixMilli()))
	}
	return nil
}

// toRoom forwards one lobby action to the room this connection is seated in.
//
// Rate-limited like a submission: every accepted action is broadcast to both
// seats, so an unbounded one lets a player flood the other's outbox until
// their session is closed for falling behind. A dropped action would leave a
// button that did nothing and no reason why, so every failure answers.
func (s *session) toRoom(in lobbyInput) {
	if !s.submitLimiter.allow(time.Now()) {
		s.send(errorMsg("too_fast"))
		return
	}
	r, id := s.currentRoom()
	if r == nil {
		s.send(errorMsg("not_in_a_room"))
		return
	}
	in.player = id
	if !r.send(in) {
		s.send(errorMsg("not_in_a_room"))
	}
}

// handleHello completes the handshake, resuming a prior game when the client
// presents a token that is still live.
func (s *session) handleHello(h *noituv1.Hello) error {
	if v := h.GetProtocolVersion(); v != ProtocolVersion {
		s.send(errorMsg("protocol_version_mismatch"))
		return errors.New("wsapi: protocol version mismatch")
	}

	// The handshake is a one-shot transition. A second Hello would re-register
	// the session and rewrite the nickname of a player already seated in a
	// game, which nothing downstream expects.
	s.mu.Lock()
	repeat := s.greeted
	s.greeted = true
	s.mu.Unlock()
	if repeat {
		s.send(errorMsg("already_greeted"))
		return errors.New("wsapi: repeated hello")
	}

	s.setNickname(sanitizeNickname(h.GetNickname()))
	s.hub.register(s)
	s.send(welcomeMsg(s.id, s.resumeToken, s.nickname()))

	if prior, ok := s.hub.resumable(h.GetResumeToken()); ok && prior != s {
		s.resumeFrom(prior)
	}
	return nil
}

// resumeFrom takes over the seat a previous connection held.
//
// Every failing branch has to say so. A token can outlive its game — the turn
// clock keeps running through the grace window, so a player who dropped on
// their own turn loses before the window closes — and a client that got a
// Welcome and then silence has nothing to render and no reason to stop
// waiting.
func (s *session) resumeFrom(prior *session) {
	r, id := prior.currentRoom()
	if r == nil {
		s.send(errorMsg("game_already_over"))
		return
	}
	if !r.send(resumeInput{player: id, sess: s, prior: prior}) {
		s.send(errorMsg("game_already_over"))
		return
	}
	// Deliberately no attach and no close here. The room has not decided yet,
	// and a refused resume that had already closed the old connection would end
	// the game it was trying to rejoin.
}

func (s *session) handleSubmit(w *noituv1.SubmitWord) {
	if !s.submitLimiter.allow(time.Now()) {
		s.send(errorMsg("too_fast"))
		return
	}
	r, id := s.currentRoom()
	if r == nil {
		s.send(errorMsg("not_in_a_game"))
		return
	}
	// A dropped submission would otherwise leave the player waiting out the
	// turn clock with no idea their word never arrived.
	if !r.send(submitInput{sess: s, player: id, word: w.GetWord(), turnSeq: w.GetTurnSeq()}) {
		s.send(errorMsg("busy"))
	}
}

// leaveRoom tells the room this connection is gone, so the seat enters its
// grace window rather than the game simply stalling.
func (s *session) leaveRoom() {
	r, id := s.currentRoom()
	if r == nil {
		return
	}
	r.send(disconnectInput{player: id, sess: s})
}

func randomToken() string {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}
