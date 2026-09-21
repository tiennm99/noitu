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

// The socket half of a connection: reading and writing frames, the
// keepalive that detects a dead peer, and the identity — nickname, room,
// seat — the protocol half below reads and writes through the same mutex.

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

	// joinsPerSecond and joinBurst bound how many rooms one address may join
	// or attempt to join. The limiter exists to slow a brute-force walk of the
	// room-code space (31 characters over 6 places, ~8.9e8 codes) to
	// centuries even at this rate — it is not meant to ration ordinary play.
	// A single NAT/CGNAT egress (a café, a school, a mobile carrier) can be
	// many real players sharing one address, so the budget has to be generous
	// enough for a whole one of those, not just one person.
	joinsPerSecond = 5
	joinBurst      = 20

	// maxWordReportsPerSession bounds how many distinct words one session may
	// file with ReportWord. A duplicate report of a word already filed does
	// not count against it — it costs nothing new to acknowledge again — but
	// an unbounded stream of distinct ones would turn the corpus feedback loop
	// into a log-filling vector.
	maxWordReportsPerSession = 20

	// Room creation is far more expensive than a join: each one is a
	// goroutine, an engine and a registry entry held until the game ends.
	roomsPerSecond = 0.2
	roomBurst      = 5
	limiterIdleFor = 5 * time.Minute

	// framesPerSecond bounds every frame a connection sends, before it is
	// routed. The per-action limiters above only meter the actions they know
	// about; a Ping, or a ClientMessage with no payload set, matched none of
	// them and cost the reader a decode at line rate. A client past this is
	// not a player typing, so the connection is closed rather than throttled.
	framesPerSecond = 20
	frameBurst      = 40
)

var (
	errHandshake = errors.New("wsapi: first message must be Hello")
	errFlood     = errors.New("wsapi: frame rate exceeded")
)

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
	frameLimiter  *bucket

	// reportedWords is every distinct word this session has filed with
	// ReportWord, capped at maxWordReportsPerSession. Touched only from
	// dispatch, which is the sole reader of this connection's frames, so it
	// needs no lock of its own — unlike nick/room/playerID above, nothing else
	// ever reads or writes it.
	reportedWords map[string]struct{}

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
		frameLimiter:  newBucket(framesPerSecond, frameBurst, time.Now()),
		reportedWords: make(map[string]struct{}),
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
	// A connection that ends while queued must not leave a ghost in line: the
	// next two strangers to ask are paired with each other, not with a socket
	// that is already gone.
	defer s.hub.cancelQuickMatch(s)

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

	// readLoop only ever returns an error; a cancelled context is the one
	// that means the server chose to close.
	status, reason := websocket.StatusNormalClosure, ""
	if !errors.Is(err, context.Canceled) {
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
		if !s.frameLimiter.allow(time.Now()) {
			s.send(errorMsg("too_fast"))
			return errFlood
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

func randomToken() string {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}
