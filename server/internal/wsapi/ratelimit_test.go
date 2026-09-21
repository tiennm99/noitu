package wsapi

import (
	"testing"
	"time"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
)

// The rate limiters themselves, and the actions that spend them.

// TestSubmitRateLimit checks the token bucket refuses a burst well past what a
// person types, since each submit costs a dictionary lookup.
func TestSubmitRateLimit(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người thử")
	c.send(&noituv1.ClientMessage{Payload: &noituv1.ClientMessage_StartBotGame{
		StartBotGame: &noituv1.StartBotGame{Difficulty: noituv1.Difficulty_DIFFICULTY_EASY},
	}})
	started := c.await("game_started").GetGameStarted()

	for range submitBurst + 5 {
		c.submit("khong co", started.GetTurnSeq())
	}

	for range 40 {
		m := c.recv()
		if payloadCase(m) == "error" && m.GetError().GetCode() == "too_fast" {
			return
		}
	}
	t.Error("never rate-limited despite submitting past the burst")
}

func TestBucketRefills(t *testing.T) {
	now := time.Now()
	b := newBucket(5, 2, now)

	for i := range 2 {
		if !b.allow(now) {
			t.Fatalf("burst should cover the first two, call %d refused", i+1)
		}
	}
	if b.allow(now) {
		t.Fatal("third call should exhaust the bucket")
	}

	// 5/sec means one token back after 200ms.
	if !b.allow(now.Add(220 * time.Millisecond)) {
		t.Error("bucket did not refill")
	}
}

func TestKeyedLimiterIsPerKey(t *testing.T) {
	now := time.Now()
	l := newKeyedLimiter(1, 1, time.Minute)

	if !l.allow("a", now) {
		t.Fatal("first call for a key should pass")
	}
	if l.allow("a", now) {
		t.Fatal("second call for the same key should be refused")
	}
	if !l.allow("b", now) {
		t.Error("a different key must have its own bucket")
	}
}

func TestKeyedLimiterSweepsIdleBuckets(t *testing.T) {
	now := time.Now()
	l := newKeyedLimiter(10, 5, time.Minute)
	l.allow("gone", now)

	l.sweep(now.Add(2 * time.Minute))

	l.mu.Lock()
	n := len(l.buckets)
	l.mu.Unlock()
	if n != 0 {
		t.Errorf("%d buckets survived the sweep, want 0", n)
	}
}

// TestLobbyActionsAreRateLimited: every accepted action is broadcast to both
// seats, so an unbounded one lets a player fill the opponent's outbox until
// the server closes their session for falling behind.
func TestLobbyActionsAreRateLimited(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	_, guest, _ := pvpLobby(t, url)

	for i := range submitBurst + 5 {
		guest.setReady(i%2 == 0)
	}

	// The limiter answers before the room does, so a refusal has to appear in
	// the stream rather than an unbroken run of room states.
	for range 30 {
		if payloadCase(guest.recv()) == "error" {
			return
		}
	}
	t.Error("a burst of lobby actions was never refused")
}
