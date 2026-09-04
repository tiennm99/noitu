package bot

import (
	"iter"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeBoard is a hand-built position. Strategies are judged on boards small
// enough to work out the right move by hand.
type fakeBoard struct {
	words   map[string][2]string // word -> {first, last}
	used    map[string]bool
	current string
}

func board(current string, words ...string) *fakeBoard {
	b := &fakeBoard{words: map[string][2]string{}, used: map[string]bool{}, current: current}
	for _, w := range words {
		p := strings.Fields(w)
		b.words[w] = [2]string{p[0], p[len(p)-1]}
	}
	return b
}

func (b *fakeBoard) play(words ...string) *fakeBoard {
	for _, w := range words {
		b.used[w] = true
	}
	return b
}

func (b *fakeBoard) LegalMoves() []string {
	var out []string
	for w, e := range b.words {
		if e[0] == b.current && !b.used[w] {
			out = append(out, w)
		}
	}
	slices.Sort(out)
	return out
}

func (b *fakeBoard) Used(word string) bool { return b.used[word] }

func (b *fakeBoard) WordsStartingWith(syllable string) iter.Seq[string] {
	var out []string
	for w, e := range b.words {
		if e[0] == syllable {
			out = append(out, w)
		}
	}
	slices.Sort(out)
	return slices.Values(out)
}

func (b *fakeBoard) LastSyllable(word string) (string, bool) {
	e, ok := b.words[word]
	return e[1], ok
}

func seeded() *rand.Rand { return rand.New(rand.NewPCG(1, 2)) }

func mustStrategy(t *testing.T, d Difficulty) Strategy {
	t.Helper()
	s, err := New(d, seeded())
	if err != nil {
		t.Fatalf("New(%s): %v", d, err)
	}
	return s
}

func TestNewRejectsUnknownDifficulty(t *testing.T) {
	if _, err := New(Difficulty(99), seeded()); err == nil {
		t.Error("New accepted an unknown difficulty")
	}
}

// Every strategy dereferences the rng, so a nil one must fail at construction
// rather than panicking on the first move.
func TestNewRejectsNilRNG(t *testing.T) {
	for _, d := range []Difficulty{Easy, Medium, Hard} {
		if _, err := New(d, nil); err == nil {
			t.Errorf("New(%s, nil) succeeded, want error", d)
		}
	}
}

// Every strategy must report no move rather than inventing one.
func TestAllStrategiesReportNoMove(t *testing.T) {
	for _, d := range []Difficulty{Easy, Medium, Hard} {
		s := mustStrategy(t, d)
		b := board("lệ", "ngôn ngữ") // nothing starts with "lệ"
		if _, err := s.Choose(b); err != ErrNoMove {
			t.Errorf("%s.Choose on a dead end = %v, want ErrNoMove", d, err)
		}
	}
}

// Whatever a strategy returns must actually be playable.
func TestAllStrategiesChooseLegalMoves(t *testing.T) {
	for _, d := range []Difficulty{Easy, Medium, Hard} {
		s := mustStrategy(t, d)
		for i := 0; i < 50; i++ {
			b := board("ngữ",
				"ngữ pháp", "ngữ điệu", "ngữ nghĩa",
				"pháp luật", "điệu bộ", "nghĩa vụ",
			).play("ngữ nghĩa")

			move, err := s.Choose(b)
			if err != nil {
				t.Fatalf("%s.Choose: %v", d, err)
			}
			if !slices.Contains(b.LegalMoves(), move) {
				t.Fatalf("%s chose %q, which is not legal", d, move)
			}
		}
	}
}

// Hard must take a move that leaves the opponent with nothing. Rate-limited to
// hardKillRate, so this checks it happens most of the time rather than always.
func TestHardTakesTheKill(t *testing.T) {
	kills := 0
	const runs = 200

	for i := 0; i < runs; i++ {
		s, err := New(Hard, rand.New(rand.NewPCG(uint64(i), 7)))
		if err != nil {
			t.Fatal(err)
		}
		// "ngữ pháp" hands over "pháp", which starts nothing: an instant win.
		// "ngữ điệu" hands over "điệu", which still has a reply.
		b := board("ngữ", "ngữ pháp", "ngữ điệu", "điệu bộ", "bộ phận")
		move, err := s.Choose(b)
		if err != nil {
			t.Fatal(err)
		}
		if move == "ngữ pháp" {
			kills++
		}
	}

	rate := float64(kills) / runs
	// Band derived from the constant, so changing hardKillRate cannot make
	// this test lie. Seeds are fixed, so the result is deterministic.
	if rate < hardKillRate-0.15 || rate > hardKillRate+0.10 {
		t.Errorf("Hard took the kill %.0f%% of the time, want near %.0f%%", rate*100, hardKillRate*100)
	}
}

// Medium always takes a win it can see. Withholding it made Medium lose to the
// random bot 97% of the time; difficulty comes from lookahead, not from
// declining to play well.
func TestMediumTakesTheKill(t *testing.T) {
	for i := 0; i < 100; i++ {
		s, err := New(Medium, rand.New(rand.NewPCG(uint64(i), 11)))
		if err != nil {
			t.Fatal(err)
		}
		b := board("ngữ", "ngữ pháp", "ngữ điệu", "điệu bộ", "bộ phận")
		move, err := s.Choose(b)
		if err != nil {
			t.Fatal(err)
		}
		if move != "ngữ pháp" {
			t.Fatalf("Medium passed up the instant win, played %q on run %d", move, i)
		}
	}
}

// With only a kill available, Medium must still play it rather than fail.
func TestMediumPlaysKillWhenItIsTheOnlyMove(t *testing.T) {
	s := mustStrategy(t, Medium)
	b := board("ngữ", "ngữ pháp") // "pháp" starts nothing

	move, err := s.Choose(b)
	if err != nil {
		t.Fatalf("Choose: %v", err)
	}
	if move != "ngữ pháp" {
		t.Errorf("Choose = %q, want the only legal move", move)
	}
}

// Medium should prefer the tighter reply when the difference is stark.
func TestMediumPrefersTighterReply(t *testing.T) {
	tight := 0
	const runs = 100

	for i := 0; i < runs; i++ {
		s, err := New(Medium, rand.New(rand.NewPCG(uint64(i), 3)))
		if err != nil {
			t.Fatal(err)
		}
		// "ngữ điệu" -> "điệu" has one reply; "ngữ nghĩa" -> "nghĩa" has four.
		b := board("ngữ",
			"ngữ điệu", "ngữ nghĩa",
			"điệu bộ",
			"nghĩa vụ", "nghĩa quân", "nghĩa trang", "nghĩa hiệp",
		)
		move, err := s.Choose(b)
		if err != nil {
			t.Fatal(err)
		}
		if move == "ngữ điệu" {
			tight++
		}
	}

	if tight < runs*8/10 {
		t.Errorf("Medium chose the tighter reply %d/%d times, want most of them", tight, runs)
	}
}

// remainingOutDegree must count what is actually left, not the dictionary's
// static figure: a syllable whose words are all spent is a dead end.
func TestRemainingOutDegreeCountsOnlyUnusedWords(t *testing.T) {
	b := board("ngữ", "ngữ pháp", "pháp luật", "pháp lý")

	if got := remainingOutDegree(b, "pháp", ""); got != 2 {
		t.Errorf("remainingOutDegree = %d, want 2", got)
	}

	b.play("pháp luật")
	if got := remainingOutDegree(b, "pháp", ""); got != 1 {
		t.Errorf("after one word used, remainingOutDegree = %d, want 1", got)
	}

	// The move being considered counts as spent too.
	if got := remainingOutDegree(b, "pháp", "pháp lý"); got != 0 {
		t.Errorf("excluding the move itself, remainingOutDegree = %d, want 0", got)
	}
}

func TestThinkingDelayInRange(t *testing.T) {
	for _, d := range []Difficulty{Easy, Medium, Hard} {
		s := mustStrategy(t, d)
		for i := 0; i < 50; i++ {
			delay := s.ThinkingDelay()
			if delay < 300*time.Millisecond || delay > 2*time.Second {
				t.Errorf("%s.ThinkingDelay = %v, outside a human-looking range", d, delay)
			}
		}
	}
}

func TestDifficultyString(t *testing.T) {
	for _, d := range []Difficulty{Easy, Medium, Hard} {
		if s := d.String(); s == "" || s == "unknown" {
			t.Errorf("Difficulty(%d).String() = %q", d, s)
		}
	}
	if got := Difficulty(99).String(); got != "unknown" {
		t.Errorf("unknown difficulty string = %q", got)
	}
}

// A seeded strategy must replay identically, or the simulation below proves
// nothing and a reported bug cannot be reproduced.
func TestChooseIsDeterministicUnderSeed(t *testing.T) {
	pick := func() string {
		s, err := New(Hard, rand.New(rand.NewPCG(42, 42)))
		if err != nil {
			t.Fatal(err)
		}
		b := board("ngữ", "ngữ pháp", "ngữ điệu", "ngữ nghĩa",
			"pháp luật", "điệu bộ", "nghĩa vụ", "luật lệ")
		move, err := s.Choose(b)
		if err != nil {
			t.Fatal(err)
		}
		return move
	}

	if a, b := pick(), pick(); a != b {
		t.Errorf("same seed produced %q then %q", a, b)
	}
}
