package wsapi

import (
	"sync"
	"time"
)

// bucket is a token bucket.
//
// Time is a parameter rather than read from the clock inside, so the tests
// exercise refill behaviour directly instead of sleeping through it.
type bucket struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	perSec   float64
	last     time.Time
}

func newBucket(perSec float64, capacity int, now time.Time) *bucket {
	return &bucket{
		tokens:   float64(capacity),
		capacity: float64(capacity),
		perSec:   perSec,
		last:     now,
	}
}

// allow spends a token if one is available.
func (b *bucket) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if elapsed := now.Sub(b.last); elapsed > 0 {
		b.tokens = min(b.capacity, b.tokens+elapsed.Seconds()*b.perSec)
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// keyedLimiter is one bucket per key, used for per-IP limits where the set of
// keys is open-ended and outlives any single connection.
//
// Idle buckets are swept rather than kept forever: without that, the map is a
// slow memory leak driven by whoever connects, which is exactly the wrong
// party to let control its size.
type keyedLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	perSec   float64
	capacity int
	idleFor  time.Duration
}

func newKeyedLimiter(perSec float64, capacity int, idleFor time.Duration) *keyedLimiter {
	return &keyedLimiter{
		buckets:  map[string]*bucket{},
		perSec:   perSec,
		capacity: capacity,
		idleFor:  idleFor,
	}
}

func (l *keyedLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	b, ok := l.buckets[key]
	if !ok {
		b = newBucket(l.perSec, l.capacity, now)
		l.buckets[key] = b
	}
	l.mu.Unlock()

	return b.allow(now)
}

// sweep drops buckets untouched for idleFor. A full bucket carries no state
// worth keeping, so recreating it later is equivalent.
func (l *keyedLimiter) sweep(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	for key, b := range l.buckets {
		// Idleness alone is the test. Tokens refill lazily — only when allow
		// is called — so a bucket that was ever used still reads as partly
		// spent no matter how long ago that was. Requiring a full bucket here
		// meant nothing was ever collected, which is precisely the leak this
		// sweep exists to prevent.
		b.mu.Lock()
		idle := now.Sub(b.last) > l.idleFor
		b.mu.Unlock()
		if idle {
			delete(l.buckets, key)
		}
	}
}
