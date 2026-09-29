package wsapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/coder/websocket"
	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"google.golang.org/protobuf/proto"
)

// Lifecycle and abuse-surface guarantees at the edges of the room actor: what
// a seat id means after its seat is freed, what happens to inputs queued
// behind a room's last one, and what one address may hold.

// handlerRoom builds a room that is driven by direct handler calls, without
// its goroutine running, for the guarantees that live below the transport.
func handlerRoom(t *testing.T) *room {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := newHub(ctx, chainDict(), time.Second, time.Second, time.Minute, 10)
	return newRoom(h, "AAAAAA", time.Second, time.Second, time.Minute, roomModePvP)
}

// awaitFrame blocks on a session's outbox until a frame satisfies want, so a
// handler-level test waits on the event itself rather than on a sleep.
func awaitFrame(t *testing.T, s *session, want func(*noituv1.ServerMessage) bool) *noituv1.ServerMessage {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case raw := <-s.out:
			var m noituv1.ServerMessage
			if err := proto.Unmarshal(raw, &m); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if want(&m) {
				return &m
			}
		case <-timeout:
			t.Fatal("the awaited frame never reached the session")
		}
	}
}

func errorCode(m *noituv1.ServerMessage) string { return m.GetError().GetCode() }

// TestKickedPlayersTokenCannotResumeIntoAReusedSeat: seat ids are reused. A
// player who dropped, was kicked, and comes back inside their grace window
// must be refused, not dropped into whoever holds the id now.
func TestKickedPlayersTokenCannotResumeIntoAReusedSeat(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{GraceFor: 10 * time.Second})

	host := dial(t, url)
	host.hello("Chủ phòng")
	host.createRoom()
	code := host.await("room_state").GetRoomState().GetRoomCode()

	alice := dial(t, url)
	aliceWelcome := alice.hello("Alice")
	alice.joinRoom(code)
	host.await("room_state")

	_ = alice.conn.Close(websocket.StatusAbnormalClosure, "")
	host.await("room_state") // Alice is away

	host.kickPlayer("p2")
	host.await("room_state") // her seat is free

	carol := dial(t, url)
	carolWelcome := carol.hello("Carol")
	carol.joinRoom(code)
	host.await("room_state")
	carol.say("carol-only secret")
	host.await("chat_message")

	_ = carol.conn.Close(websocket.StatusAbnormalClosure, "")
	host.await("room_state") // Carol is away, on the seat Alice used to hold

	stale := resumeAs(t, url, "Alice", aliceWelcome.GetResumeToken())
	if got := stale.await("error").GetError().GetCode(); got != "session_not_resumable" {
		t.Fatalf("a kicked player's token got %q, want session_not_resumable", got)
	}

	// The seat still belongs to its real occupant.
	back := resumeAs(t, url, "Carol", carolWelcome.GetResumeToken())
	history := back.await("chat_history").GetChatHistory().GetMessages()
	if len(history) != 1 || !history[0].GetFromMe() {
		t.Errorf("the real occupant's resume did not restore their own seat: %+v", history)
	}
}

// TestRoomExitAnswersInputsQueuedBehindTheLastOne: the owner leaves an empty
// room while a friend's join is already queued behind it. The room is gone by
// the time the join is reached, and the friend must be told so rather than
// waiting forever.
func TestRoomExitAnswersInputsQueuedBehindTheLastOne(t *testing.T) {
	r := handlerRoom(t)
	host, friend := offlineSession(t, 64), offlineSession(t, 64)

	r.inputs <- createInput{sess: host}
	r.inputs <- lobbyInput{sess: host, player: "p1", action: lobbyLeave}
	r.inputs <- joinInput{sess: friend}
	r.inputs <- resumeInput{player: "p1", sess: friend, prior: host}
	r.run()

	got := queued(t, friend)
	var codes []string
	for _, m := range got {
		codes = append(codes, errorCode(m))
	}
	if !slices.Contains(codes, "room_not_found") || !slices.Contains(codes, "session_not_resumable") {
		t.Errorf("inputs queued behind the room's last were answered with %v, want room_not_found and session_not_resumable", codes)
	}
}

// TestDisconnectNoticeSurvivesAFullInbox: the notice that a socket died is
// what starts its seat's reconnect window. Dropped by a full inbox, the seat
// stays bound to a dead connection and counts as connected forever.
func TestDisconnectNoticeSurvivesAFullInbox(t *testing.T) {
	r := handlerRoom(t)
	host := offlineSession(t, 4*roomInputCap)
	host.room, host.playerID = r, "p1"

	r.inputs <- createInput{sess: host}
	for len(r.inputs) < cap(r.inputs) {
		r.inputs <- lobbyInput{sess: host, player: "p1", action: lobbyReady, ready: true}
	}

	left := make(chan struct{})
	go func() {
		host.leaveRoom()
		close(left)
	}()
	go r.run()

	select {
	case <-left:
	case <-time.After(5 * time.Second):
		t.Fatal("leaveRoom never returned")
	}

	// FIFO: this is handled after the disconnect, so a seat that entered its
	// grace window refuses it, and one still bound to the dead connection
	// delivers it.
	r.sendReliably(chatInput{sess: host, player: "p1", text: "still here?"})
	got := awaitFrame(t, host, func(m *noituv1.ServerMessage) bool {
		return m.GetChatMessage() != nil || errorCode(m) == "not_your_seat"
	})
	if errorCode(got) != "not_your_seat" {
		t.Errorf("the seat is still bound to the disconnected session: got %s", describe(got))
	}
}

// TestBusyRoomIsNotReportedAsOver: a resume that finds the room's inbox full
// is being told the room is busy; game_already_over would be a false
// statement about a room that is alive.
func TestBusyRoomIsNotReportedAsOver(t *testing.T) {
	r := handlerRoom(t)
	prior, next := offlineSession(t, 8), offlineSession(t, 8)
	prior.room, prior.playerID = r, "p1"

	for len(r.inputs) < cap(r.inputs) {
		r.inputs <- lobbyInput{}
	}
	next.resumeFrom(prior)
	if got := queued(t, next); len(got) != 1 || errorCode(got[0]) != "busy" {
		t.Errorf("resume into a busy room got %v, want busy", got)
	}

	r.cancel()
	next.resumeFrom(prior)
	if got := queued(t, next); len(got) != 1 || errorCode(got[0]) != "game_already_over" {
		t.Errorf("resume into a finished room got %v, want game_already_over", got)
	}
}

// TestRefusedResignationsAreRateLimited: a resignation costs the room an
// input even when it is refused, so it shares the submission budget instead
// of being free.
func TestRefusedResignationsAreRateLimited(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	host := dial(t, url)
	host.hello("Chủ phòng")
	host.createRoom()
	host.await("room_state")

	const sent = submitBurst + 5
	for range sent {
		host.resign()
	}
	throttled := 0
	for range sent {
		if host.await("error").GetError().GetCode() == "too_fast" {
			throttled++
		}
	}
	if throttled == 0 {
		t.Error("no refused resignation was rate limited")
	}
}

// TestQuickMatchAutoStartDoesNotOutliveItsPairing: the pairing's joiner died
// before it was seated. The room must not start a game for whoever fills the
// second seat afterwards, because nobody readied for it.
func TestQuickMatchAutoStartDoesNotOutliveItsPairing(t *testing.T) {
	r := handlerRoom(t)
	waiter, partner, friend := offlineSession(t, 64), offlineSession(t, 64), offlineSession(t, 64)

	r.handleCreate(createInput{sess: waiter, autoStart: true})
	partner.cancel() // gone between the pairing and the seating
	r.handleJoin(joinInput{sess: partner})
	r.vacate(r.seatOf("p2"))

	r.handleJoin(joinInput{sess: friend})
	if r.engine != nil {
		t.Error("a game started for a joiner nobody had readied with")
	}
}

// TestResumeLeavesTheQuickMatchQueue: a connection that resumes into a seat
// while queued must not stay in the queue, or a later pairing pulls it out of
// the game it is playing.
func TestResumeLeavesTheQuickMatchQueue(t *testing.T) {
	r := handlerRoom(t)
	prior, next := offlineSession(t, 64), offlineSession(t, 64)
	r.handleCreate(createInput{sess: prior})
	r.holdSeat(r.seatOf("p1"))

	r.hub.waiting = []*session{next}
	r.handleResume(resumeInput{player: "p1", sess: next, prior: prior})

	if len(r.hub.waiting) != 0 {
		t.Error("a resumed connection is still queued for a quick match")
	}
}

// TestQuickMatchIsRefusedWhileDraining: a player queued now would wait for a
// stranger until the shutdown.
func TestQuickMatchIsRefusedWhileDraining(t *testing.T) {
	api, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người chơi")

	api.StartDraining()
	c.quickMatch()
	if got := c.await("error").GetError().GetCode(); got != "server_restarting" {
		t.Errorf("quick match while draining got %q, want server_restarting", got)
	}
	if n := len(api.hub.waiting); n != 0 {
		t.Errorf("%d sessions queued while draining", n)
	}
}

// TestEmptyMessageIsAnswered: a frame with no payload, or one from a newer
// client, is told it was not understood instead of being dropped.
func TestEmptyMessageIsAnswered(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người chơi")

	c.send(&noituv1.ClientMessage{})
	if got := c.await("error").GetError().GetCode(); got != "unknown_message" {
		t.Errorf("an empty message got %q, want unknown_message", got)
	}
}

// TestRefusedActionsDoNotKeepALobbyAlive: the idle window restarts when the
// room changes, not when somebody pokes it. A refused action every 100ms must
// not hold a 300ms lobby open.
func TestRefusedActionsDoNotKeepALobbyAlive(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{IdleFor: 300 * time.Millisecond})
	host := dial(t, url)
	host.hello("Chủ phòng")
	host.createRoom()
	host.await("room_state")

	// The owner has no readiness, so each of these is refused and changes
	// nothing.
	const pokes = 9
	for range pokes {
		host.setReady(true)
		time.Sleep(100 * time.Millisecond)
	}

	refusedBeforeClose := 0
	for {
		m := host.recv()
		switch errorCode(m) {
		case "owner_needs_no_ready":
			refusedBeforeClose++
			continue
		case "room_idle_closed":
		default:
			continue
		}
		break
	}
	if refusedBeforeClose == pokes {
		t.Error("the lobby outlived the whole run of refused actions")
	}
}

// TestReadyToggleKeepsALobbyAlive: a readiness change is the room changing,
// so it restarts the idle window.
func TestReadyToggleKeepsALobbyAlive(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{IdleFor: 400 * time.Millisecond})
	host, guest, _ := pvpLobby(t, url)

	start := time.Now()
	for i := range 4 {
		guest.setReady(i%2 == 0)
		host.await("room_state")
		time.Sleep(150 * time.Millisecond)
	}
	if got := host.await("error").GetError().GetCode(); got != "room_idle_closed" {
		t.Fatalf("got %q, want room_idle_closed", got)
	}
	if elapsed := time.Since(start); elapsed < 700*time.Millisecond {
		t.Errorf("the lobby closed after %v, though readiness changed for ~600ms", elapsed)
	}
}

// TestSilentSocketIsClosed: a socket that never says Hello holds a slot of the
// global connection cap for nothing, so it is closed after the handshake
// deadline. A greeted connection is left alone.
func TestSilentSocketIsClosed(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{}, func(s *Server) {
		s.hub.helloTimeout = 200 * time.Millisecond
	})

	greeted := dial(t, url)
	greeted.hello("Người chơi")

	silent := dial(t, url)
	opened := time.Now()
	for {
		if _, err := silent.read(); err != nil {
			// read gives up after five seconds on its own, which would look
			// the same as a close; the server's deadline is a fraction of that.
			if time.Since(opened) > 3*time.Second {
				t.Fatalf("the silent socket was never closed by the server: %v", err)
			}
			break
		}
	}

	// The silent socket outlived its deadline, and the greeted one started
	// earlier, so its deadline has passed too.
	greeted.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Ping{Ping: &noituv1.Ping{ClientTimeMs: 7}}})
	if got := greeted.await("pong").GetPong().GetClientTimeMs(); got != 7 {
		t.Errorf("pong echoed %d, want 7", got)
	}
}

// TestRoomBudgetIsPerAddress: reconnecting mints a fresh per-connection
// budget, so the address is what has to hold the line.
func TestRoomBudgetIsPerAddress(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{}, func(s *Server) {
		s.hub.roomLimiter = newKeyedLimiter(0.001, roomBurst, time.Hour)
	})

	first := dial(t, url)
	first.hello("Một")
	for range roomBurst {
		first.createRoom()
		first.await("room_state")
	}

	// A new connection has a full budget of its own, but the same address.
	second := dial(t, url)
	second.hello("Hai")
	second.createRoom()
	if got := second.await("error").GetError().GetCode(); got != "too_many_rooms" {
		t.Errorf("a reconnect got %q, want too_many_rooms", got)
	}
}

// TestLimiterKeyFoldsAddressesToWhatOneClientOwns: an IPv4-mapped address is
// its IPv4 host, and an IPv6 client owns a whole /64.
func TestLimiterKeyFoldsAddressesToWhatOneClientOwns(t *testing.T) {
	cases := []struct{ in, want string }{
		{"203.0.113.9", "203.0.113.9"},
		{"::ffff:203.0.113.9", "203.0.113.9"},
		{"2001:db8:1:2:aaaa:bbbb:cccc:dddd", "2001:db8:1:2::/64"},
		{"2001:db8:1:2:1::1", "2001:db8:1:2::/64"},
		{"2001:db8:1:3::1", "2001:db8:1:3::/64"},
		{"fe80::1%eth0", "fe80::/64"},
		{"not-an-address", "not-an-address"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := limiterKey(tc.in); got != tc.want {
			t.Errorf("limiterKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestCorpusLogIsBoundedProcessWide: only a flood is sampled, and the gap is
// counted and reported on the next line through.
func TestCorpusLogIsBoundedProcessWide(t *testing.T) {
	type line struct {
		msg  string
		args []any
	}
	var lines []line
	now := time.Now()
	c := newCorpusLogger(now)
	c.out = func(msg string, args ...any) { lines = append(lines, line{msg, args}) }

	before := metrics.corpusLogSuppressed.Value()
	const total = corpusLogBurst + 50
	for range total {
		c.info(now, "word_rejected", "word", "x")
	}
	if len(lines) != corpusLogBurst {
		t.Fatalf("%d lines got through at one instant, want the burst of %d", len(lines), corpusLogBurst)
	}
	if got := metrics.corpusLogSuppressed.Value() - before; got != 50 {
		t.Errorf("suppressed counter rose by %d, want 50", got)
	}

	c.info(now.Add(time.Second), "word_rejected", "word", "x")
	last := lines[len(lines)-1]
	if len(last.args) < 2 || last.args[len(last.args)-2] != "suppressed_before" || last.args[len(last.args)-1] != int64(50) {
		t.Errorf("the line after the gap did not report it: %v", last.args)
	}
}

// TestResponsesCarrySecurityHeaders covers the small text endpoints and an
// unknown path alike: the headers are set before routing.
func TestResponsesCarrySecurityHeaders(t *testing.T) {
	api, _ := newTestServer(t, chainDict(), Config{})
	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "frame-ancestors 'self'",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
	}
	for _, path := range []string{"/healthz", "/version", "/no/such/page"} {
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		for name, value := range want {
			if got := rec.Header().Get(name); got != value {
				t.Errorf("GET %s: %s = %q, want %q", path, name, got, value)
			}
		}
	}
}

// TestResumeIntoTheLobbyReplaysAMissedGameOver: a player who was away when the
// game ended comes back to the lobby it returned to, and is owed the result,
// after the room state.
func TestResumeIntoTheLobbyReplaysAMissedGameOver(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{TurnLimit: time.Second, GraceFor: 10 * time.Second})

	host := dial(t, url)
	host.hello("Chủ phòng")
	host.createRoom()
	code := host.await("room_state").GetRoomState().GetRoomCode()

	guest := dial(t, url)
	guestWelcome := guest.hello("Khách")
	guest.joinRoom(code)
	readyAndStart(t, host, guest)
	hostStart := host.await("game_started").GetGameStarted()
	guest.await("game_started")

	_ = guest.conn.Close(websocket.StatusAbnormalClosure, "")
	host.await("room_state") // the guest is away

	// The game ends while the guest is gone: the host gives up on their turn,
	// or the absent guest runs out the clock on theirs.
	if hostStart.GetMyTurn() {
		host.resign()
	}
	host.await("game_over")

	back := resumeAs(t, url, "Khách", guestWelcome.GetResumeToken())
	var order []string
	var over *noituv1.GameOver
	for over == nil {
		m := back.recv()
		order = append(order, payloadCase(m))
		over = m.GetGameOver()
		if len(order) > 6 {
			t.Fatalf("no game_over replayed after the resume: %v", order)
		}
	}
	if order[len(order)-2] != "room_state" {
		t.Errorf("frames = %v, want game_over right after room_state", order)
	}
	// The guest's own version: the host resigned exactly when it was theirs to
	// act, which hands the guest the win, and otherwise the guest lost the clock.
	if over.GetIWon() != hostStart.GetMyTurn() {
		t.Errorf("i_won = %v, want %v", over.GetIWon(), hostStart.GetMyTurn())
	}
	if len(over.GetStandings()) != 2 || !slices.ContainsFunc(over.GetStandings(), (*noituv1.PlayerScore).GetIsMe) {
		t.Errorf("standings are not rendered for the resuming seat: %+v", over.GetStandings())
	}
}

// TestResumeReplaysNothingForAGameSeenLive: a player who was connected when
// the game ended already has the result, so a later refresh in the lobby is
// answered with the lobby alone.
func TestResumeReplaysNothingForAGameSeenLive(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})

	host := dial(t, url)
	welcome := host.hello("Chủ phòng")
	host.createRoom()
	code := host.await("room_state").GetRoomState().GetRoomCode()
	guest := dial(t, url)
	guest.hello("Khách")
	guest.joinRoom(code)
	readyAndStart(t, host, guest)
	start := host.await("game_started").GetGameStarted()
	guest.await("game_started")
	resignAndSettle(t, host, guest, start)

	_ = host.conn.Close(websocket.StatusAbnormalClosure, "")
	back := resumeAs(t, url, "Chủ phòng", welcome.GetResumeToken())
	back.await("room_state")

	back.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_Ping{Ping: &noituv1.Ping{ClientTimeMs: 1}}})
	if m := back.recv(); payloadCase(m) != "pong" {
		t.Errorf("after the lobby state the next frame was %s, want nothing replayed", describe(m))
	}
}
