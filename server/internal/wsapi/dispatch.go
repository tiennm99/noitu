package wsapi

import (
	"errors"
	"log/slog"
	"time"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/game"
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
	if _, isHello := msg.GetPayload().(*noituv1.ClientMessage_Hello); !isHello && !s.greeted {
		s.send(errorMsg(codeHandshakeRequired))
		return errHandshake
	}

	switch p := msg.GetPayload().(type) {
	case *noituv1.ClientMessage_Hello:
		return s.handleHello(p.Hello)

	case *noituv1.ClientMessage_StartBotGame:
		if s.refuseMidGame() {
			return nil
		}
		// The limiter is charged before the payload is inspected, so a bad
		// difficulty costs the same as a good one and cannot be used to probe
		// for free.
		if !s.allowRoom() {
			return nil
		}
		difficulty, ok := Difficulty(p.StartBotGame.GetDifficulty())
		if !ok {
			s.send(errorMsg(codeUnknownDifficulty))
			return nil
		}
		if err := s.hub.startBotRoom(s, difficulty); err != nil {
			s.send(roomCreateError(s.id, err))
		}

	case *noituv1.ClientMessage_CreateRoom:
		if s.refuseMidGame() {
			return nil
		}
		// Creating a room allocates a goroutine and an engine, so one
		// connection must not be able to mint them without limit.
		if !s.allowRoom() {
			return nil
		}
		if err := s.hub.createRoom(s); err != nil {
			s.send(roomCreateError(s.id, err))
		}

	case *noituv1.ClientMessage_JoinRoom:
		if s.refuseMidGame() {
			return nil
		}
		if !s.hub.joinLimiter.allow(s.remoteIP, time.Now()) {
			metrics.joinsRefused.Add("too_many_attempts", 1)
			s.send(errorMsg(codeTooManyAttempts))
			return nil
		}
		if err := s.hub.joinRoom(p.JoinRoom.GetRoomCode(), s); err != nil {
			metrics.joinsRefused.Add("room_not_found", 1)
			s.send(errorMsg(codeRoomNotFound))
		}

	case *noituv1.ClientMessage_QuickMatch:
		if s.refuseMidGame() {
			return nil
		}
		if r, _ := s.currentRoom(); r != nil {
			s.send(errorMsg(codeAlreadyInARoom))
			return nil
		}
		// A match mints a room exactly as CreateRoom does, so it is charged
		// the same way and for the same reason.
		if !s.allowRoom() {
			return nil
		}
		if err := s.hub.quickMatch(s); err != nil {
			if errors.Is(err, errAlreadyQueued) {
				s.send(errorMsg(codeAlreadyQueued))
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
		// A dropped submission would otherwise leave the player waiting out the
		// turn clock with no idea their word never arrived.
		word, seq := p.SubmitWord.GetWord(), p.SubmitWord.GetTurnSeq()
		s.toRoom(s.submitLimiter, codeNotInAGame, codeBusy, func(id game.PlayerID) any {
			return submitInput{sess: s, player: id, word: word, turnSeq: seq}
		})

	case *noituv1.ClientMessage_Resign:
		// No budget of its own beyond the frame limiter: only one can ever be
		// accepted per game, and a refusal goes to the sender alone. A silently
		// dropped resignation leaves the player staring at a board they thought
		// they had left.
		s.toRoom(nil, codeNotInAGame, codeGameAlreadyOver, func(id game.PlayerID) any {
			return resignInput{sess: s, player: id}
		})

	case *noituv1.ClientMessage_ClaimDeadEnd:
		// Rate-limited on the same budget as a submission: a claim is the
		// alternative to playing a word, not a second action alongside it.
		s.toRoom(s.submitLimiter, codeNotInAGame, codeBusy, func(id game.PlayerID) any {
			return claimDeadEndInput{sess: s, player: id}
		})

	case *noituv1.ClientMessage_ReportWord:
		s.handleReportWord(p.ReportWord)

	case *noituv1.ClientMessage_SetReady:
		s.toLobby(lobbyInput{action: lobbyReady, ready: p.SetReady.GetReady()})

	case *noituv1.ClientMessage_StartGame:
		s.toLobby(lobbyInput{action: lobbyStart})

	case *noituv1.ClientMessage_KickPlayer:
		s.toLobby(lobbyInput{action: lobbyKick, target: game.PlayerID(p.KickPlayer.GetPlayerId())})

	case *noituv1.ClientMessage_LeaveRoom:
		s.toLobby(lobbyInput{action: lobbyLeave})

	case *noituv1.ClientMessage_SendChat:
		// Its own budget, so a talkative player never runs out of moves. The
		// seat itself is checked by the room, which is the only place that
		// knows whether this connection still holds one. A dropped line would
		// leave the player watching their own message fail to appear with no
		// reason given.
		text := p.SendChat.GetText()
		s.toRoom(s.chatLimiter, codeNotInARoom, codeBusy, func(id game.PlayerID) any {
			return chatInput{sess: s, player: id, text: text}
		})

	case *noituv1.ClientMessage_Ping:
		s.send(pongMsg(p.Ping.GetClientTimeMs(), time.Now().UnixMilli()))
	}
	return nil
}

// allowRoom charges the room budget, answering too_many_rooms when it is
// spent. Every path that mints a room — a bot game, a code, a quick match —
// holds a goroutine and an engine, so all three share it.
func (s *session) allowRoom() bool {
	if s.roomLimiter.allow(time.Now()) {
		return true
	}
	s.send(errorMsg(codeTooManyRooms))
	return false
}

// refuseMidGame answers already_in_a_game, and reports true, when this
// connection is seated in a room whose game is still being played.
//
// Opening or joining another room is what walks a player out of the one they
// are in, and mid-game that would be an abandonment the other players only
// learn of when the reconnect window runs out. Leaving is a deliberate act
// with its own message; a room change is not allowed to be a quiet way of
// doing it. A lobby or a finished game is left freely, as before.
//
// Checked before any limiter is charged, so a refusal costs nothing. The
// flag is read outside the room goroutine, which makes it a snapshot: a game
// starting at the same instant is a race the grace window already settles.
func (s *session) refuseMidGame() bool {
	r, _ := s.currentRoom()
	if r == nil || !r.liveCounted.Load() {
		return false
	}
	s.send(errorMsg(codeAlreadyInAGame))
	return true
}

// roomCreateError names the refusal a room could not be opened for. A full
// server is the player's business — they should wait, not retry at once — and
// anything else is the server's, logged here because the client is only told
// that it failed.
func roomCreateError(sessionID string, err error) *noituv1.ServerMessage {
	if errors.Is(err, errServerFull) {
		return errorMsg(codeServerFull)
	}
	if errors.Is(err, errDraining) {
		// The same key Shutdown sends to everyone already seated: a room
		// refused for this reason will not open a moment later the way a full
		// one might, so the client is told the same thing either way.
		return errorMsg(codeServerRestarting)
	}
	slog.Error("open room", "session", sessionID, "err", err)
	return errorMsg(codeRoomStartFailed)
}

// toRoom forwards one input to the room this connection is seated in.
//
// Every failure answers, because every input here is something the player
// is waiting to see the effect of: limiter, when set, is charged first and
// refuses with too_fast; notIn answers a connection seated nowhere; and
// dropped answers a room that would not take the input — finished, or with
// its inbox full. build receives the seat the connection holds, read at the
// same moment as the room so the two cannot disagree.
func (s *session) toRoom(limiter *bucket, notIn, dropped errCode, build func(game.PlayerID) any) {
	if limiter != nil && !limiter.allow(time.Now()) {
		s.send(errorMsg(codeTooFast))
		return
	}
	r, id := s.currentRoom()
	if r == nil {
		s.send(errorMsg(notIn))
		return
	}
	if !r.send(build(id)) {
		s.send(errorMsg(dropped))
	}
}

// toLobby forwards one lobby action.
//
// Rate-limited like a submission: every accepted action is broadcast to every
// seat, so an unbounded one lets a player flood the other's outbox until
// their session is closed for falling behind.
func (s *session) toLobby(in lobbyInput) {
	s.toRoom(s.submitLimiter, codeNotInARoom, codeNotInARoom, func(id game.PlayerID) any {
		in.sess, in.player = s, id
		return in
	})
}

// handleHello completes the handshake, resuming a prior game when the client
// presents a token that is still live.
func (s *session) handleHello(h *noituv1.Hello) error {
	if v := h.GetProtocolVersion(); v != ProtocolVersion {
		s.send(errorMsg(codeProtocolVersionMismatch))
		return errors.New("wsapi: protocol version mismatch")
	}

	// The handshake is a one-shot transition. A second Hello would re-register
	// the session and rewrite the nickname of a player already seated in a
	// game, which nothing downstream expects.
	if s.greeted {
		s.send(errorMsg(codeAlreadyGreeted))
		return errors.New("wsapi: repeated hello")
	}
	s.greeted = true

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
		s.send(errorMsg(codeSessionNotResumable))
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
		s.send(errorMsg(codeGameAlreadyOver))
		return
	}
	if !r.send(resumeInput{player: id, sess: s, prior: prior}) {
		s.send(errorMsg(codeGameAlreadyOver))
		return
	}
	// Deliberately no attach and no close here. The room has not decided yet,
	// and a refused resume that had already closed the old connection would end
	// the game it was trying to rejoin.
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
		s.send(errorMsg(codeTooFast))
		return
	}

	word, syllables, err := vietnamese.Normalize(sanitizeText(m.GetWord(), maxWordRunes, maxNicknameMarks))
	if err != nil || !vietnamese.HasEnoughSyllables(syllables) {
		s.send(errorMsg(codeWordReportRefused))
		return
	}

	if _, already := s.reportedWords[word]; !already {
		if len(s.reportedWords) >= maxWordReportsPerSession {
			s.send(errorMsg(codeWordReportLimit))
			return
		}
		s.reportedWords[word] = struct{}{}
	}

	// currentRoom's player id is not needed here: the log line is about the
	// word and the room's context, never about who filed it.
	if r, _ := s.currentRoom(); r != nil {
		if !r.send(reportWordInput{sess: s, word: word}) {
			s.send(errorMsg(codeBusy))
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
