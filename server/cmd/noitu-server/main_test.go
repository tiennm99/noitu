package main

import (
	"sync"
	"testing"
	"time"
)

// TestEnvFallsBackWhenUnsetOrBlank covers env's whole contract: an unset
// variable and a blank one are the same "nothing configured" signal, since an
// operator's empty override should not silently defeat a default.
func TestEnvFallsBackWhenUnsetOrBlank(t *testing.T) {
	const key = "NOITU_TEST_ENV_STRING"

	if got := env(key, "fallback"); got != "fallback" {
		t.Errorf("unset: env = %q, want the fallback", got)
	}

	t.Setenv(key, "   ")
	if got := env(key, "fallback"); got != "fallback" {
		t.Errorf("blank: env = %q, want the fallback", got)
	}

	t.Setenv(key, "  configured  ")
	if got := env(key, "fallback"); got != "configured" {
		t.Errorf("set: env = %q, want the trimmed value", got)
	}
}

// TestEnvIntRejectsInvalidAndNegative: a production misconfiguration here
// must fall back loudly rather than silently becoming zero or panicking.
func TestEnvIntRejectsInvalidAndNegative(t *testing.T) {
	const key = "NOITU_TEST_ENV_INT"

	if got := envInt(key, 7); got != 7 {
		t.Errorf("unset: envInt = %d, want the fallback", got)
	}

	tests := []struct {
		name, value string
		want        int
	}{
		{"valid", "42", 42},
		{"zero is a real value", "0", 0},
		{"negative rejected", "-1", 7},
		{"not a number rejected", "many", 7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(key, tc.value)
			if got := envInt(key, 7); got != tc.want {
				t.Errorf("envInt(%q) = %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

// TestEnvDurationRejectsInvalidAndZero: unlike envNonNegDuration, a plain
// timing variable rejects zero along with garbage — every one of today's
// durations (turn limit, grace) has to be positive to mean anything.
func TestEnvDurationRejectsInvalidAndZero(t *testing.T) {
	const key = "NOITU_TEST_ENV_DURATION"

	if got := envDuration(key, time.Second); got != time.Second {
		t.Errorf("unset: envDuration = %v, want the fallback", got)
	}

	tests := []struct {
		name, value string
		want        time.Duration
	}{
		{"valid", "30s", 30 * time.Second},
		{"zero rejected", "0s", time.Second},
		{"negative rejected", "-5s", time.Second},
		{"not a duration rejected", "soon", time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(key, tc.value)
			if got := envDuration(key, time.Second); got != tc.want {
				t.Errorf("envDuration(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestEnvNonNegDurationAcceptsZero is the one difference from envDuration:
// NOITU_DRAIN_TIMEOUT=0 is a deliberate "do not wait", not a typo, and must
// not be rejected the way a zero turn limit would be.
func TestEnvNonNegDurationAcceptsZero(t *testing.T) {
	const key = "NOITU_TEST_ENV_NONNEG_DURATION"

	tests := []struct {
		name, value string
		want        time.Duration
	}{
		{"valid", "10s", 10 * time.Second},
		{"zero accepted", "0s", 0},
		{"negative rejected", "-1s", 5 * time.Second},
		{"not a duration rejected", "soon", 5 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(key, tc.value)
			if got := envNonNegDuration(key, 5*time.Second); got != tc.want {
				t.Errorf("envNonNegDuration(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestEnvListSplitsAndTrims covers the shape NOITU_ALLOWED_ORIGINS and
// NOITU_TRUSTED_PROXIES both rely on: comma-separated, whitespace around each
// entry ignored, and empty entries dropped rather than becoming a stray "".
func TestEnvListSplitsAndTrims(t *testing.T) {
	const key = "NOITU_TEST_ENV_LIST"

	if got := envList(key); got != nil {
		t.Errorf("unset: envList = %v, want nil", got)
	}

	t.Setenv(key, " a , b ,, c")
	got := envList(key)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("envList = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("envList[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// fakeGameCounter answers LiveGameCount from a value a test can change while
// the drain loop is polling it, so the loop can be driven without a real
// server or rooms behind it.
type fakeGameCounter struct {
	mu   sync.Mutex
	live int64
}

func (f *fakeGameCounter) set(n int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.live = n
}

func (f *fakeGameCounter) LiveGameCount() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live
}

// TestWaitForGamesToFinishReturnsAsSoonAsTheCountReachesZero: the loop must
// not sit out its whole timeout once the last game has actually ended.
func TestWaitForGamesToFinishReturnsAsSoonAsTheCountReachesZero(t *testing.T) {
	counter := &fakeGameCounter{live: 1}

	done := make(chan struct{})
	go func() {
		waitForGamesToFinish(counter, time.Minute)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	counter.set(0)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waitForGamesToFinish did not return once the count reached zero")
	}
}

// TestWaitForGamesToFinishRespectsTheTimeout: a game that never ends must not
// hang the drain forever — it has to give up once its budget is spent.
func TestWaitForGamesToFinishRespectsTheTimeout(t *testing.T) {
	counter := &fakeGameCounter{live: 1}

	start := time.Now()
	waitForGamesToFinish(counter, 250*time.Millisecond)
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Errorf("returned after %v, want at least the 250ms timeout", elapsed)
	}
}

// TestWaitForGamesToFinishReturnsImmediatelyOnZeroTimeout: a zero or negative
// timeout is today's original behaviour — rooms are torn down with whatever
// they were doing rather than waited on at all.
func TestWaitForGamesToFinishReturnsImmediatelyOnZeroTimeout(t *testing.T) {
	counter := &fakeGameCounter{live: 1}

	start := time.Now()
	waitForGamesToFinish(counter, 0)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("a zero timeout took %v, want an immediate return", elapsed)
	}
}
