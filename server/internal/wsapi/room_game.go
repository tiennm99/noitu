package wsapi

import (
	"log/slog"
	"math/rand/v2"
	"slices"
	"time"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
	"github.com/tiennm99dev/noitu/server/internal/bot"
	"github.com/tiennm99dev/noitu/server/internal/dictionary"
	"github.com/tiennm99dev/noitu/server/internal/game"
	"github.com/tiennm99dev/noitu/server/internal/vietnamese"
)

// Everything that touches a running game: starting one, applying a move,
// scoring it, and reporting what an elimination or a game-over means to
// each seat.

// handleResign is one player giving up on their own turn. The seat, not the
// claimed id, is the authority, as everywhere a connection acts on a room.
//
// Only the player to act may give up. Giving up is a move — it is what is
// played instead of a word — and a seat that could spend it while somebody
// else was thinking would be deciding the turn of a player who had not
// finished theirs. Somebody who wants out of a game they are not on turn in
// leaves the room instead, which handleLobby answers.
func (r *room) handleResign(m resignInput) {
	if !r.occupies(m.sess, m.player) {
		m.sess.send(errorMsg("not_your_seat"))
		return
	}
	if r.engine == nil || r.engine.Over() {
		return
	}
	if r.engine.Turn() != m.player {
		m.sess.send(errorMsg("not_your_turn"))
		return
	}
	before := r.mark()
	if r.engine.Resign(m.player, time.Now()) {
		r.applyEliminations(before)
	}
}

// handleClaimDeadEnd is the player to act saying the syllable in play has no
// answer left, checked rather than trusted.
//
// A true claim takes them out at once with EndNoLegalMove — exactly what the
// clock would eventually rule, so the game's own outcome is unchanged and
// only the wait is gone. A false claim changes nothing at all: the clock
// keeps running and the claimant is simply told a word exists, which is hint
// enough to be the whole cost of asking wrongly.
func (r *room) handleClaimDeadEnd(m claimDeadEndInput) {
	if !r.occupies(m.sess, m.player) {
		m.sess.send(errorMsg("not_your_seat"))
		return
	}
	if r.engine == nil {
		m.sess.send(errorMsg("game_not_started"))
		return
	}
	if r.engine.Over() {
		return
	}
	if r.engine.Turn() != m.player {
		m.sess.send(errorMsg("not_your_turn"))
		return
	}
	if r.engine.HasLegalMove() {
		metrics.deadEndClaims.Add("false", 1)
		m.sess.send(errorMsg("not_a_dead_end"))
		return
	}

	metrics.deadEndClaims.Add("true", 1)
	before := r.mark()
	if r.engine.NoMove(time.Now()) {
		r.applyEliminations(before)
	}
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
	s := &seat{id: "p1", nickname: m.sess.nickname(), sess: m.sess, chatFrom: r.chatSeq}
	r.seats[0] = s
	r.seats[1] = &seat{id: botPlayerID, nickname: "Máy"}
	r.owner = "p1"
	m.sess.attach(r, "p1")
	r.hub.cancelQuickMatch(m.sess)

	if s.sess.ctx.Err() != nil {
		// A bot room has no lobby to fall back to and no idle timer covering it
		// while there is no engine yet (resetIdleTimer skips any room with a
		// strategy) — a grace window here would leave the bot's own seat
		// holding the room open forever with nothing left to vacate it. The
		// room ends now instead, the same way a failed bot.New or beginGame
		// above already does.
		r.cancel()
		return
	}

	if err := r.beginGame(); err != nil {
		slog.Error("could not start bot game", "room", r.code, "err", err)
		m.sess.send(errorMsg("game_start_failed"))
		r.cancel()
	}
}

// beginGame builds the engine and tells both seats the game is on.
func (r *room) beginGame() error {
	opening, err := r.dict.RandomOpeningWord(minOpeningOutDegree)
	if err != nil {
		return err
	}

	// Seat order is turn order, so a player's place at the table is the place
	// they took in the lobby and nothing has to be shuffled or announced.
	ids := make([]game.PlayerID, 0, maxPlayers)
	for _, s := range r.seats {
		if s != nil {
			ids = append(ids, s.id)
		}
	}
	// Who leads is drawn rather than owned. Opening the game is an advantage —
	// the first player picks from a whole syllable, everyone after them plays
	// what is left of it — and giving it to whoever happened to create the
	// room would make the same person favourite in every game of a series.
	//
	// Rotating rather than shuffling keeps the table intact: everybody still
	// plays in the order they sat down, the cycle just starts somewhere else.
	// A bot room is left alone; it has no table to be fair about, and the
	// human opens.
	if r.strategy == nil {
		lead := rand.IntN(len(ids))
		ids = slices.Concat(ids[lead:], ids[:lead])
	}

	engine, err := game.New(r.dict, ids, opening, r.turnLimit, time.Now())
	if err != nil {
		return err
	}
	r.engine = engine
	r.opening = opening
	// Fresh per game: an override from the last one would describe a player
	// who has since come back and is playing this one.
	r.outWire = make(map[game.PlayerID]noituv1.GameEndReason, len(ids))
	// Never restarts at 1. A rematch reuses the same connections, so a
	// submission still in flight from the previous game would otherwise be
	// able to match a turn in this one and be applied to it.
	r.turnSeq++
	// Every game is agreed on its own. The readiness that started this one is
	// spent, so the lobby they come back to asks again.
	for _, s := range r.seats {
		if s != nil {
			s.ready = false
		}
	}

	metrics.gamesStarted.Add(r.mode, 1)
	r.hub.gameStarted()
	r.liveCounted.Store(true)

	state := r.engine.Snapshot()
	for _, s := range r.seats {
		r.sendGameStarted(s, state)
	}
	r.maybeScheduleBot()
	return nil
}

// sendGameStarted renders the opening position for one seat. my_turn and is_me
// are per-recipient, which is why this is built per seat rather than broadcast.
func (r *room) sendGameStarted(s *seat, state game.State) {
	if s == nil || s.sess == nil {
		return
	}
	s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_GameStarted{
		GameStarted: &noituv1.GameStarted{
			OpeningWord:     r.opening,
			OpeningMeanings: Senses(r.dict.Meanings(r.opening)),
			CurrentSyllable: state.Current,
			MyTurn:          state.Turn == s.id,
			DeadlineUnixMs:  state.Deadline.UnixMilli(),
			TurnSeq:         r.turnSeq,
			TurnLimitMs:     uint32(r.turnLimit.Milliseconds()),
			Players:         r.scoreRows(r.engine.Players(), state, s.id, nil),
			TurnPlayerId:    string(state.Turn),
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
	metrics.wordsSubmitted.Add(1)

	if m.turnSeq != r.turnSeq {
		r.sendTo(m.player, moveRejectedMsg(noituv1.RejectReason_REJECT_REASON_NOT_YOUR_TURN, m.word, r.turnSeq, ""))
		r.recordRejection(game.ReasonNotYourTurn, m.word)
		return
	}

	// The typed text is echoed back to every seat as PlayedWord.typed, so it
	// crosses the same trust boundary a chat line does and gets the same
	// filter. The engine's own normalization only lowercases and collapses
	// whitespace; it does not drop format characters.
	word := sanitizeText(m.word, maxWordRunes, maxNicknameMarks)

	before := r.mark()
	move, reason := r.engine.Submit(m.player, word, time.Now())
	if reason != game.ReasonNone {
		r.sendTo(m.player, moveRejectedMsg(RejectReason(reason), word, m.turnSeq, r.nearMissFor(reason, word)))
		r.recordRejection(reason, word)
		// A rejection for an expired turn also took this player out of the
		// game, and everybody has to be told which.
		r.applyEliminations(before)
		return
	}
	metrics.wordsAccepted.Add(1)

	// An accepted move never ends a game: a dead end is left for whoever
	// inherits it, which is what Submit's own comment explains.
	r.turnSeq++
	r.broadcastTurn(&move)
	r.maybeScheduleBot()
}

// nearMissFor finds a diacritic-typo suggestion for a word the dictionary
// refused. Only for REJECT_REASON_NOT_IN_DICTIONARY: every other rejection
// means the word IS in the dictionary and was refused for some other reason,
// where a spelling suggestion would be misleading rather than helpful.
func (r *room) nearMissFor(reason game.RejectReason, raw string) string {
	if reason != game.ReasonNotInDictionary {
		return ""
	}
	normalized, _, err := vietnamese.Normalize(raw)
	if err != nil {
		return ""
	}
	suggestion, ok := r.dict.NearMiss(normalized)
	if !ok {
		return ""
	}
	// The dictionary check comes before the link and reuse checks in Submit,
	// so a real word can be a near miss and still be unplayable here. Offering
	// it would send the player straight into a second refusal.
	if first, ok := r.dict.FirstSyllable(suggestion); !ok || first != r.engine.Current() || r.engine.Used(suggestion) {
		return ""
	}
	return suggestion
}

// recordRejection counts one rejected submission and logs it at Info.
//
// This is the corpus feedback loop the improvement report calls the input to
// every decision about the dictionary: which words players actually type that
// the game does not accept, and why. The word logged is never the raw typed
// text — it is normalized the same way the engine would have matched it
// (NFC, lowercase, single-spaced) and capped, so the line is useful for corpus
// review without ever logging what a player literally typed into the box.
func (r *room) recordRejection(reason game.RejectReason, raw string) {
	metrics.wordsRejected.Add(reason.String(), 1)

	// sanitizeText first: raw may be the untouched client payload (the
	// not-your-turn path never reaches the sanitizer below it in
	// handleSubmit), and Normalize alone does not drop control or format
	// characters.
	word, _, err := vietnamese.Normalize(sanitizeText(raw, maxWordRunes, maxNicknameMarks))
	if err != nil {
		word = ""
	}
	if runes := []rune(word); len(runes) > maxWordRunes {
		word = string(runes[:maxWordRunes])
	}

	slog.Info("word_rejected",
		"reason", reason.String(),
		"word", word,
		"link", r.engine.Current(),
		"mode", r.mode,
		"room", r.code,
	)
}

// handleReportWord logs one report with this room's context.
//
// The session has already checked the word is long enough and within its own
// per-session cap before routing it here — this is only about what to log,
// and the syllable in play, this room's mode and its code are all room
// goroutine state that only the room may read. Never the reporting player's
// seat or name: recordRejection keeps the same information out of the corpus
// feedback loop for the same reason.
func (r *room) handleReportWord(m reportWordInput) {
	link := ""
	if r.engine != nil {
		link = r.engine.Current()
	}
	metrics.wordsReported.Add(1)
	slog.Info("word_reported", "word", m.word, "link", link, "mode", r.mode, "room", r.code)
	m.sess.send(wordReportedMsg(m.word))
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
	metrics.botMoves.Add(r.strategy.Difficulty().String(), 1)

	now := time.Now()
	before := r.mark()

	if m.err != nil {
		// The bot has nothing to play. A human in this position keeps their
		// turn and loses it to the clock; the bot has no clock to spend, so
		// the position is settled now and reported for what it is rather than
		// as a resignation it never chose.
		if !r.engine.NoMove(now) {
			r.engine.Resign(botPlayerID, now)
		}
		r.applyEliminations(before)
		return
	}

	move, reason := r.engine.Submit(botPlayerID, m.word, now)
	if reason != game.ReasonNone {
		// The bot searched the same dictionary the engine validates against,
		// so this means the two disagree — a bug worth seeing, not a move to
		// retry.
		slog.Error("bot move rejected by engine", "room", r.code, "word", m.word, "reason", reason.String())
		r.engine.Resign(botPlayerID, now)
		r.applyEliminations(before)
		return
	}

	r.turnSeq++
	r.broadcastTurn(&move)
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
	board := freezeBoard(r.engine)
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

// broadcastTurn sends the position to every seat, rendered for each.
//
// move is nil when the turn moved without a word being played, which is what
// an elimination does: the syllable and the used set survive the player who
// could not answer them, and everybody still needs the new deadline and the
// new player to act.
func (r *room) broadcastTurn(move *game.Move) {
	state := r.engine.Snapshot()
	meanings := r.moveMeanings(move)
	for _, s := range r.seats {
		r.sendTurnUpdate(s, state, move, meanings)
	}
}

// moveMeanings looks up a played word's senses once per move; they are the
// same for every recipient. nil for no move.
func (r *room) moveMeanings(move *game.Move) []dictionary.Sense {
	if move == nil {
		return nil
	}
	return r.dict.Meanings(move.Word)
}

// sendTurnUpdate renders one position for one seat. by_me, my_turn and is_me
// are all per-recipient, which is why there is no single shared frame; the
// move's meanings are not, and arrive looked up.
func (r *room) sendTurnUpdate(s *seat, state game.State, move *game.Move, meanings []dictionary.Sense) {
	if s == nil || s.sess == nil {
		return
	}
	update := &noituv1.TurnUpdate{
		CurrentSyllable: state.Current,
		MyTurn:          state.Turn == s.id,
		DeadlineUnixMs:  state.Deadline.UnixMilli(),
		TurnSeq:         r.turnSeq,
		ChainLength:     uint32(state.ChainLength),
		Players:         r.scoreRows(r.engine.Players(), state, s.id, nil),
		TurnPlayerId:    string(state.Turn),
	}
	if move != nil {
		update.Played = PlayedWord(*move, move.Player == s.id, meanings)
	}
	s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_TurnUpdate{TurnUpdate: update}})
}

// inputMark is what the game looked like before an input: how many players
// were out, and who was to act. Remembered across the input so
// applyEliminations can tell that input's doing from what was already true,
// and whether it moved the turn.
type inputMark struct {
	out  int
	turn game.PlayerID
}

// mark reads the current game, or the zero mark when there is no game.
func (r *room) mark() inputMark {
	if r.engine == nil {
		return inputMark{}
	}
	return inputMark{out: r.engine.EliminatedCount(), turn: r.engine.Turn()}
}

// applyEliminations reports everybody the last input knocked out, then whatever
// the game became: finished, or one turn further on.
//
// Every path that takes a player out of a game ends here — a timeout, a
// resignation, a bot with nothing to play, a reconnect window running out — so
// there is one place that decides what the room says about it.
func (r *room) applyEliminations(before inputMark) {
	if r.engine == nil {
		return
	}
	state := r.engine.Snapshot()
	if len(state.Eliminated) == before.out {
		return
	}

	// An elimination does not move the position, so one lookup describes it
	// for everybody who went out on this input.
	suggestions := r.engine.Suggestions(maxSuggestions)
	for _, id := range state.Eliminated[before.out:] {
		r.broadcastElimination(id, suggestions)
	}

	if r.engine.Over() {
		r.broadcastGameOver(state)
		return
	}
	// A new turn nobody played into, and the sequence moves with it: a
	// submission already in flight was answering the position the player who
	// just went out was looking at.
	//
	// It moves only when the turn does. Somebody forfeiting out of turn — a
	// player who left the room, or whose reconnect window ran out — leaves the
	// syllable, the deadline and the player to act exactly as they were, so
	// the word that player is already sending still answers the board it was
	// typed for. Bumping the sequence there would refuse it for something
	// somebody else did.
	if state.Turn != before.turn {
		r.turnSeq++
	}
	r.broadcastTurn(nil)
}

// broadcastElimination tells the room one player is out.
//
// The suggestions go only to that player. They are what the position still had
// to offer, and the people who could still answer it are not the ones who
// needed to be told — an empty list is the answer for whoever was stuck, and
// noise for everybody else.
func (r *room) broadcastElimination(id game.PlayerID, suggestions []string) {
	name := ""
	if out := r.seatOf(id); out != nil {
		name = out.nickname
	}
	reason := r.wireEndReason(id)
	metrics.eliminations.Add(reason.String(), 1)

	for _, s := range r.seats {
		if s == nil || s.sess == nil {
			continue
		}
		msg := &noituv1.PlayerEliminated{
			PlayerId: string(id),
			Name:     name,
			IsMe:     s.id == id,
			Reason:   reason,
		}
		if s.id == id {
			msg.Suggestions = suggestions
		}
		s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_PlayerEliminated{
			PlayerEliminated: msg,
		}})
	}
}

// wireEndReason says how one player left the game.
//
// The engine's answer, unless the room overrode it: a reconnect window running
// out is a resignation to the engine, because that is the only shape it has
// for a player who stops playing, and somebody who left to everybody in the
// room.
func (r *room) wireEndReason(p game.PlayerID) noituv1.GameEndReason {
	if code, overridden := r.outWire[p]; overridden {
		return code
	}
	return EndReason(r.engine.OutReason(p))
}

// broadcastGameOver reports the result from each seat's point of view.
func (r *room) broadcastGameOver(state game.State) {
	metrics.gamesFinished.Add(r.mode, 1)
	if r.liveCounted.CompareAndSwap(true, false) {
		r.hub.gameFinished()
	}

	// The reason the game ended is the reason the last player went out, which
	// with two seats is the only elimination there was.
	reason := noituv1.GameEndReason_GAME_END_REASON_UNSPECIFIED
	if n := len(state.Eliminated); n > 0 {
		reason = r.wireEndReason(state.Eliminated[n-1])
	}

	// Credited before anything is sent, so the RoomState the run loop
	// broadcasts after a finished game already carries the game just won.
	if s := r.seatOf(state.Winner); s != nil {
		s.wins++
	}

	ranks := make(map[game.PlayerID]int, len(state.Standings))
	order := make([]game.PlayerID, 0, len(state.Standings))
	for _, standing := range state.Standings {
		ranks[standing.Player] = standing.Rank
		order = append(order, standing.Player)
	}

	for _, s := range r.seats {
		if s == nil || s.sess == nil {
			continue
		}
		s.sess.send(&noituv1.ServerMessage{Payload: &noituv1.ServerMessage_GameOver{
			GameOver: &noituv1.GameOver{
				IWon:        state.Winner == s.id,
				Reason:      reason,
				ChainLength: uint32(state.ChainLength),
				Standings:   r.scoreRows(order, state, s.id, ranks),
			},
		}})
	}
	// A finished game is a return to the lobby, and the run loop reports the
	// state they are returning to.
	r.lobbyChanged = true
}

// scoreRows renders the players table for one recipient.
//
// order is the sequence to report them in — turn order while a game runs,
// finishing order once one has ended — and ranks is empty until there is a
// result, which is what makes a rank of zero mean "still playing" rather than
// needing a field of its own to say so.
func (r *room) scoreRows(order []game.PlayerID, state game.State, me game.PlayerID, ranks map[game.PlayerID]int) []*noituv1.PlayerScore {
	rows := make([]*noituv1.PlayerScore, 0, len(order))
	for _, id := range order {
		row := &noituv1.PlayerScore{
			PlayerId: string(id),
			IsMe:     id == me,
			Score:    uint32(state.Scores[id]),
			// A player the engine no longer knows is a seat that was vacated
			// mid-game, which only happens to somebody already out.
			Eliminated: !state.Alive[id],
			// The bot has no socket to lose, so it is never the one keeping
			// the room waiting.
			Connected: id == botPlayerID,
			Rank:      uint32(ranks[id]),
		}
		if s := r.seatOf(id); s != nil {
			row.Name = s.nickname
			row.Connected = row.Connected || s.sess != nil
		}
		rows = append(rows, row)
	}
	return rows
}
