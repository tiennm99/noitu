package wsapi

import (
	"expvar"
	"testing"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
)

// mapValue reads one key of an expvar.Map as an int64, 0 for a key never
// written. The counters under test are shared package state — every wsapi
// test that ever ran in this process wrote to them — so every assertion below
// is a delta across one call, never an absolute value.
func mapValue(m *expvar.Map, key string) int64 {
	v := m.Get(key)
	if v == nil {
		return 0
	}
	iv, ok := v.(*expvar.Int)
	if !ok {
		return 0
	}
	return iv.Value()
}

// TestMetricsCountWordSubmissions covers both directions handleSubmit can
// go: an engine rejection counted under its game.RejectReason string, and an
// accepted move counted separately. wordsSubmitted rises either way, which is
// what makes it the denominator for an acceptance rate.
func TestMetricsCountWordSubmissions(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người thử")
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
		StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
	}})
	started := c.await("game_started").GetGameStarted()

	submittedBefore := metrics.wordsSubmitted.Value()
	acceptedBefore := metrics.wordsAccepted.Value()
	rejectedBefore := mapValue(metrics.wordsRejected, "not in dictionary")

	c.submit("khong co", started.GetTurnSeq())
	c.await("move_rejected")

	if got, want := metrics.wordsSubmitted.Value(), submittedBefore+1; got != want {
		t.Errorf("wordsSubmitted after a rejection = %d, want %d", got, want)
	}
	if got, want := metrics.wordsAccepted.Value(), acceptedBefore; got != want {
		t.Errorf("wordsAccepted moved on a rejection: %d, want unchanged at %d", got, want)
	}
	if got, want := mapValue(metrics.wordsRejected, "not in dictionary"), rejectedBefore+1; got != want {
		t.Errorf(`wordsRejected["not in dictionary"] = %d, want %d`, got, want)
	}

	submittedBefore = metrics.wordsSubmitted.Value()
	acceptedBefore = metrics.wordsAccepted.Value()

	// The turn never moved on the rejection above, so the same turn_seq still
	// answers the current position.
	c.submit("b c", started.GetTurnSeq())
	c.await("turn_update")

	if got, want := metrics.wordsSubmitted.Value(), submittedBefore+1; got != want {
		t.Errorf("wordsSubmitted after an acceptance = %d, want %d", got, want)
	}
	if got, want := metrics.wordsAccepted.Value(), acceptedBefore+1; got != want {
		t.Errorf("wordsAccepted = %d, want %d", got, want)
	}
}
