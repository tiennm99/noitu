package wsapi

import "expvar"

// metricSet is the process's answer to "how is the game actually being
// played" — the question plans/reports/brainstormer-260921-0016 names as
// unanswerable with nothing but a dozen scattered slog calls. It is exposed
// at GET /debug/vars, but only when the operator opts in by setting
// NOITU_DEBUG_ADDR to a separate listen address: see cmd/noitu-server/main.go.
// Nothing here is read anywhere in the game logic — a counter that fed a
// decision back into play would need its own tests for correctness, not just
// for visibility.
//
// expvar's registry is a process-wide singleton by design (see the stdlib
// package doc), so these are package-level rather than server-instance
// fields: a second *Server in the same process — every wsapi test — shares
// one set of counters rather than fighting to register a second one under the
// same name.
type metricSet struct {
	connectionsOpen  *expvar.Int
	connectionsTotal *expvar.Int

	// roomsLive and roomsTotal are keyed "bot" or "pvp".
	roomsLive  *expvar.Map
	roomsTotal *expvar.Map

	// gamesStarted and gamesFinished are keyed the same way.
	gamesStarted  *expvar.Map
	gamesFinished *expvar.Map

	wordsSubmitted *expvar.Int
	wordsAccepted  *expvar.Int
	// wordsRejected is keyed by game.RejectReason.String(), which is also
	// what the word_rejected log line's reason field carries — one reason
	// vocabulary rather than two.
	wordsRejected *expvar.Map

	// eliminations is keyed by the wire GameEndReason string, since that is
	// the one place "the reconnect window ran out" is told apart from "they
	// resigned" — both are game.EndResigned to the engine underneath.
	eliminations *expvar.Map

	chatLines *expvar.Int

	// joinsRefused is keyed by the same UI error code the client is sent —
	// "too_many_attempts", "room_not_found" or "room_full" — so a reader
	// checking this counter against the frontend copy is checking against the
	// same string, not a second name for it.
	joinsRefused *expvar.Map

	// botMoves is keyed by bot.Difficulty.String().
	botMoves *expvar.Map

	resumesAttempted *expvar.Int
	resumesSucceeded *expvar.Int

	// quickMatchQueued, quickMatchCancelled and quickMatchMatched count the
	// three things that can happen to a QuickMatch: it waits, it is withdrawn
	// (by CancelQuickMatch, a disconnect, or entering a room another way), or
	// it is paired. Matched counts pairings, not players, so it rises by one
	// per room a quick match opened.
	quickMatchQueued    *expvar.Int
	quickMatchCancelled *expvar.Int
	quickMatchMatched   *expvar.Int
}

// metrics is the one instance every call site writes through. Built at
// package init rather than lazily, so every counter exists — reading at zero
// — from the moment the process starts, whether or not NOITU_DEBUG_ADDR is
// ever set.
var metrics = newMetricSet()

func newMetricSet() *metricSet {
	return &metricSet{
		connectionsOpen:  expvar.NewInt("noitu_connections_open"),
		connectionsTotal: expvar.NewInt("noitu_connections_total"),
		roomsLive:        expvar.NewMap("noitu_rooms_live"),
		roomsTotal:       expvar.NewMap("noitu_rooms_total"),
		gamesStarted:     expvar.NewMap("noitu_games_started"),
		gamesFinished:    expvar.NewMap("noitu_games_finished"),
		wordsSubmitted:   expvar.NewInt("noitu_words_submitted"),
		wordsAccepted:    expvar.NewInt("noitu_words_accepted"),
		wordsRejected:    expvar.NewMap("noitu_words_rejected"),
		eliminations:     expvar.NewMap("noitu_eliminations"),
		chatLines:        expvar.NewInt("noitu_chat_lines"),
		joinsRefused:     expvar.NewMap("noitu_joins_refused"),
		botMoves:         expvar.NewMap("noitu_bot_moves"),
		resumesAttempted: expvar.NewInt("noitu_resumes_attempted"),
		resumesSucceeded: expvar.NewInt("noitu_resumes_succeeded"),

		quickMatchQueued:    expvar.NewInt("noitu_quick_match_queued"),
		quickMatchCancelled: expvar.NewInt("noitu_quick_match_cancelled"),
		quickMatchMatched:   expvar.NewInt("noitu_quick_match_matched"),
	}
}
