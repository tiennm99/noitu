package wsapi

import (
	"errors"
	"log/slog"
	"time"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/vietnamese"
)

// The protocol half of a connection: routing one decoded ClientMessage to
// whatever it means, and the handshake and resume flow that has to run
// before anything else is meaningful.

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
		// The limiter is charged before the payload is inspected, so a bad
		// difficulty costs the same as a good one and cannot be used to probe
		// for free.
		if !s.roomLimiter.allow(time.Now()) {
			s.send(errorMsg("too_many_rooms"))
			return nil
		}
		difficulty, ok := Difficulty(p.StartBotGame.GetDifficulty())
		if !ok {
			s.send(errorMsg("unknown_difficulty"))
			return nil
		}
		if err := s.hub.startBotRoom(s, difficulty); err != nil {
			s.send(roomCreateError(s.id, err))
		}

	case *noituv1.ClientMessage_CreateRoom:
		// Creating a room allocates a goroutine and an engine, so one
		// connection must not be able to mint them without limit.
		if !s.roomLimiter.allow(time.Now()) {
			s.send(errorMsg("too_many_rooms"))
			return nil
		}
		if err := s.hub.createRoom(s); err != nil {
			s.send(roomCreateError(s.id, err))
		}

	case *noituv1.ClientMessage_JoinRoom:
		if !s.hub.joinLimiter.allow(s.remoteIP, time.Now()) {
			metrics.joinsRefused.Add("too_many_attempts", 1)
			s.send(errorMsg("too_many_attempts"))
			return nil
		}
		if err := s.hub.joinRoom(p.JoinRoom.GetRoomCode(), s); err != nil {
			metrics.joinsRefused.Add("room_not_found", 1)
			s.send(errorMsg("room_not_found"))
		}

	case *noituv1.ClientMessage_QuickMatch:
		if r, _ := s.currentRoom(); r != nil {
			s.send(errorMsg("already_in_a_room"))
			return nil
		}
		// A match mints a room exactly as CreateRoom does, so it is charged
		// the same way and for the same reason.
		if !s.roomLimiter.allow(time.Now()) {
			s.send(errorMsg("too_many_rooms"))
			return nil
		}
		if err := s.hub.quickMatch(s); err != nil {
			if errors.Is(err, errAlreadyQueued) {
				s.send(errorMsg("already_queued"))
			} else {
				s.send(roomCreateError(s.id, err))
			}
		}

	case *noituv1.ClientMessage_CancelQuickMatch:
		// Idempotent by design: a cancel that finds nothing queued is not an
		// error, it is the answer the player wanted.
		s.hub.cancelQuickMatch(s)
		s.send(quickMatchStatusMsg(false))

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

	case *noituv1.ClientMessage_ClaimDeadEnd:
		// Rate-limited on the same budget as a submission: a claim is the
		// alternative to playing a word, not a second action alongside it.
		if !s.submitLimiter.allow(time.Now()) {
			s.send(errorMsg("too_fast"))
			return nil
		}
		if r, id := s.currentRoom(); r != nil {
			if !r.send(claimDeadEndInput{sess: s, player: id}) {
				s.send(errorMsg("busy"))
			}
		} else {
			s.send(errorMsg("not_in_a_game"))
		}

	case *noituv1.ClientMessage_ReportWord:
		s.handleReportWord(p.ReportWord)

	case *noituv1.ClientMessage_SetReady:
		s.toRoom(lobbyInput{sess: s, action: lobbyReady, ready: p.SetReady.GetReady()})

	case *noituv1.ClientMessage_StartGame:
		s.toRoom(lobbyInput{sess: s, action: lobbyStart})

	case *noituv1.ClientMessage_KickPlayer:
		s.toRoom(lobbyInput{sess: s, action: lobbyKick, target: playerIDFor(p.KickPlayer.GetPlayerId())})

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

// roomCreateError names the refusal a room could not be opened for. A full
// server is the player's business — they should wait, not retry at once — and
// anything else is the server's, logged here because the client is only told
// that it failed.
func roomCreateError(sessionID string, err error) *noituv1.ServerMessage {
	if errors.Is(err, errServerFull) {
		return errorMsg("server_full")
	}
	if errors.Is(err, errDraining) {
		// The same key Shutdown sends to everyone already seated: a room
		// refused for this reason will not open a moment later the way a full
		// one might, so the client is told the same thing either way.
		return errorMsg("server_restarting")
	}
	slog.Error("open room", "session", sessionID, "err", err)
	return errorMsg("room_start_failed")
}

// toRoom forwards one lobby action to the room this connection is seated in.
//
// Rate-limited like a submission: every accepted action is broadcast to every
// seat, so an unbounded one lets a player flood the other's outbox until
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

	token := h.GetResumeToken()
	switch prior, ok := s.hub.resumable(token); {
	case ok && prior != s:
		s.resumeFrom(prior)
	case token != "" && !ok:
		// A token the server restarted since, or that outlived its grace
		// window, resolves to nothing. Silence here left the client's resume
		// latch waiting forever for a reply that was never coming — this
		// connection is answered and carries on as a fresh session instead of
		// being closed, since a fresh session is exactly what it is.
		s.send(errorMsg("session_not_resumable"))
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
	metrics.resumesAttempted.Add(1)
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

// handleReportWord validates a word report and, once it is worth logging,
// hands it to the current room for the context only the room goroutine may
// read — the syllable in play, and the room's own mode and code.
//
// Validation happens here rather than in the room because it is entirely
// about this connection: its own rate budget, and its own running count of
// distinct words already filed. Neither needs the room at all, and a session
// playing no game — smoke-testing the wire directly, per the README — can
// still file a report, acknowledged with mode "none" and no link.
func (s *session) handleReportWord(m *noituv1.ReportWord) {
	if !s.chatLimiter.allow(time.Now()) {
		s.send(errorMsg("too_fast"))
		return
	}

	word, syllables, err := vietnamese.Normalize(sanitizeText(m.GetWord(), maxWordRunes, maxNicknameMarks))
	if err != nil || !vietnamese.HasEnoughSyllables(syllables) {
		s.send(errorMsg("word_report_refused"))
		return
	}

	if _, already := s.reportedWords[word]; !already {
		if len(s.reportedWords) >= maxWordReportsPerSession {
			s.send(errorMsg("word_report_limit"))
			return
		}
		s.reportedWords[word] = struct{}{}
	}

	// currentRoom's player id is not needed here: the log line is about the
	// word and the room's context, never about who filed it.
	if r, _ := s.currentRoom(); r != nil {
		if !r.send(reportWordInput{sess: s, word: word}) {
			s.send(errorMsg("busy"))
		}
		return
	}

	metrics.wordsReported.Add(1)
	slog.Info("word_reported", "word", word, "link", "", "mode", "none", "room", "")
	s.send(wordReportedMsg(word))
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
