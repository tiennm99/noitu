package wsapi

import (
	"log/slog"
	"sync/atomic"
	"time"
)

// Corpus log lines are word_rejected and word_reported: the feedback loop the
// dictionary is tuned by. Each session is already bounded on how many it can
// cause, but a session is free to reconnect, so the per-session bounds do not
// bound the process. This is the bound that does.
const (
	// corpusLogPerSecond and corpusLogBurst are the process-wide budget for
	// those lines. Far above what real play produces — a rejection is a typo
	// or a missing word, a few a minute per room — so only a flood is sampled.
	corpusLogPerSecond = 20
	corpusLogBurst     = 100
)

// corpusLogger writes the corpus lines through one process-wide bucket. The
// counters that mirror them stay exact; only the log is sampled, so a flood
// cannot fill the disk or rotate the real signal out.
type corpusLogger struct {
	bucket     *bucket
	suppressed atomic.Int64
	// out writes one line that got through; slog.Info outside of tests.
	out func(msg string, args ...any)
}

func newCorpusLogger(now time.Time) *corpusLogger {
	return &corpusLogger{
		bucket: newBucket(corpusLogPerSecond, corpusLogBurst, now),
		out:    slog.Info,
	}
}

// corpusLog is the one instance every corpus line goes through.
var corpusLog = newCorpusLogger(time.Now())

// info logs one corpus line, or counts it as suppressed when the budget is
// spent. The first line to get through after a suppressed run carries how many
// were dropped, so the gap is visible in the log itself and not only in the
// noitu_corpus_log_suppressed counter.
func (c *corpusLogger) info(now time.Time, msg string, args ...any) {
	if !c.bucket.allow(now) {
		c.suppressed.Add(1)
		metrics.corpusLogSuppressed.Add(1)
		return
	}
	if n := c.suppressed.Swap(0); n > 0 {
		args = append(args, "suppressed_before", n)
	}
	c.out(msg, args...)
}
