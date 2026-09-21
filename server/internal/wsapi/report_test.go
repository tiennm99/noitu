package wsapi

import (
	"fmt"
	"testing"

	noituv1 "github.com/tiennm99dev/noitu/server/gen/noitu/v1"
)

// Reporting a word the dictionary should have accepted.

// TestReportWordAcceptsAndEchoes needs no room at all: filing a report is a
// session-level fact, which is also what a direct wire client per the README
// would exercise.
func TestReportWordAcceptsAndEchoes(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người chơi")

	c.reportWord("bình tâm")
	got := c.await("word_reported").GetWordReported()
	if got.GetWord() != "bình tâm" {
		t.Errorf("echoed word = %q, want %q", got.GetWord(), "bình tâm")
	}
}

func TestReportWordRefusesOneSyllable(t *testing.T) {
	_, url := newTestServer(t, chainDict(), Config{})
	c := dial(t, url)
	c.hello("Người chơi")

	c.reportWord("một")
	if code := c.await("error").GetError().GetCode(); code != "word_report_refused" {
		t.Errorf("code = %q, want word_report_refused", code)
	}
}

// TestReportWordEnforcesPerSessionCap drives the cap directly against the
// session rather than through a real socket: 21 reports through the chat rate
// limiter (chatBurst=5) would be a test of two budgets fighting each other
// rather than of the cap itself.
func TestReportWordEnforcesPerSessionCap(t *testing.T) {
	s := offlineSession(t, maxWordReportsPerSession+8)

	for i := range maxWordReportsPerSession {
		s.handleReportWord(&noituv1.ReportWord{Word: fmt.Sprintf("từ số %d", i)})
	}
	accepted := queued(t, s)
	if len(accepted) != maxWordReportsPerSession {
		t.Fatalf("got %d replies for %d distinct reports, want one each", len(accepted), maxWordReportsPerSession)
	}
	for _, m := range accepted {
		if m.GetWordReported() == nil {
			t.Errorf("a report inside the cap was refused: %+v", m)
		}
	}

	s.handleReportWord(&noituv1.ReportWord{Word: "một từ khác nữa"})
	overflow := queued(t, s)
	if len(overflow) != 1 || overflow[0].GetError().GetCode() != "word_report_limit" {
		t.Fatalf("the report past the cap = %+v, want a single word_report_limit error", overflow)
	}
}
