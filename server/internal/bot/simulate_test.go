package bot

import (
	"iter"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/tiennm99dev/noitu/server/internal/game"
)

// simDict is a word graph large enough that skill matters: syllables differ
// widely in how many continuations they offer, so choosing well is possible
// and choosing badly is punished.
type simDict struct {
	words map[string][2]string
}

// newSimDict builds a seeded pseudo-random word graph.
//
// Size and irregularity both matter. A small, regular graph makes every game a
// race decided by who moves first, and lookahead cannot show an advantage over
// greedy play — measured at exactly 50% on a 7-syllable version. Real
// dictionaries have many syllables with wildly uneven out-degrees, and that is
// where searching ahead starts to pay.
func newSimDict() *simDict {
	d := &simDict{words: map[string][2]string{}}
	rng := rand.New(rand.NewPCG(20260904, 3))

	const syllableCount = 26
	syllables := make([]string, syllableCount)
	for i := range syllables {
		syllables[i] = string(rune('a' + i))
	}

	add := func(first, last string) {
		d.words[first+" "+last] = [2]string{first, last}
	}

	// Uneven out-degrees: a few hubs, many mid-sized syllables, some dead ends.
	for i, first := range syllables {
		var outDegree int
		switch {
		case i < 3:
			outDegree = 8 + rng.IntN(4) // hubs
		case i < syllableCount-4:
			outDegree = 1 + rng.IntN(5) // ordinary
		default:
			outDegree = rng.IntN(2) // near dead ends
		}
		for j := 0; j < outDegree; j++ {
			add(first, syllables[rng.IntN(syllableCount)])
		}
	}

	return d
}

func (d *simDict) Resolve(word string) (string, bool) {
	_, ok := d.words[word]
	return word, ok
}

func (d *simDict) FirstSyllable(word string) (string, bool) {
	e, ok := d.words[word]
	return e[0], ok
}

func (d *simDict) LastSyllable(word string) (string, bool) {
	e, ok := d.words[word]
	return e[1], ok
}

func (d *simDict) WordsStartingWith(syllable string) iter.Seq[string] {
	var out []string
	for w, e := range d.words {
		if e[0] == syllable {
			out = append(out, w)
		}
	}
	// Sorted for reproducibility under a fixed seed.
	slices.Sort(out)
	return slices.Values(out)
}

func (d *simDict) OutDegree(syllable string) (int, error) {
	n := 0
	for _, e := range d.words {
		if e[0] == syllable {
			n++
		}
	}
	return n, nil
}

// playGame runs one headless bot-vs-bot game and reports the winner.
// Every move goes through Engine.Submit, so the bots are held to the same
// rules a human is.
func playGame(t *testing.T, dict game.Dictionary, first, second Strategy) game.PlayerID {
	t.Helper()

	const p1, p2 = game.PlayerID("first"), game.PlayerID("second")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	e, err := game.New(dict, []game.PlayerID{p1, p2}, "a b", time.Minute, now)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	board := BoardFor(e)

	strategies := map[game.PlayerID]Strategy{p1: first, p2: second}

	// Bounded so a pathological loop cannot hang the suite.
	for turn := 0; turn < 500 && !e.Over(); turn++ {
		p := e.Turn()
		move, err := strategies[p].Choose(board)
		if err == ErrNoMove {
			// A dead end no longer ends the game on its own: a human keeps the
			// turn and loses it to the clock. Two bots have no clock, so this
			// is where the position is settled -- exactly as the room does it.
			if !e.NoMove() {
				t.Fatalf("bot had no move at %q but the engine says the position has one", e.Current())
			}
			break
		}
		if err != nil {
			t.Fatalf("Choose: %v", err)
		}

		now = now.Add(time.Second)
		if _, reason := e.Submit(p, move, now); reason != game.ReasonNone {
			t.Fatalf("bot %s played %q which the engine rejected: %s", p, move, reason)
		}
	}

	if !e.Over() {
		t.Fatal("game did not finish within the turn cap")
	}
	return e.Winner()
}

// winRate plays n games and reports how often the challenger wins.
//
// Sides alternate every game. On a small graph moving first is an advantage in
// its own right, and a fixed seating order would report that advantage as
// skill.
func winRate(t *testing.T, challenger, defender Difficulty, n int) float64 {
	t.Helper()

	const p1 = game.PlayerID("first")
	dict := newSimDict()
	wins := 0

	for i := 0; i < n; i++ {
		// Fresh seeded strategies per game: reproducible, but not identical
		// play every game.
		c, err := New(challenger, rand.New(rand.NewPCG(uint64(i), 1)))
		if err != nil {
			t.Fatal(err)
		}
		d, err := New(defender, rand.New(rand.NewPCG(uint64(i), 2)))
		if err != nil {
			t.Fatal(err)
		}

		challengerMovesFirst := i%2 == 0
		var winner game.PlayerID
		if challengerMovesFirst {
			winner = playGame(t, dict, c, d)
		} else {
			winner = playGame(t, dict, d, c)
		}

		if (winner == p1) == challengerMovesFirst {
			wins++
		}
	}
	return float64(wins) / float64(n)
}

// The whole point of three difficulties is that they are actually different.
// If Hard does not beat Easy, the ladder is decoration.
func TestDifficultyLadder(t *testing.T) {
	if testing.Short() {
		t.Skip("simulation skipped in short mode")
	}

	const games = 100

	hardVsEasy := winRate(t, Hard, Easy, games)
	mediumVsEasy := winRate(t, Medium, Easy, games)
	hardVsMedium := winRate(t, Hard, Medium, games)

	t.Logf("win rates over %d games: hard-vs-easy %.0f%%, medium-vs-easy %.0f%%, hard-vs-medium %.0f%%",
		games, hardVsEasy*100, mediumVsEasy*100, hardVsMedium*100)

	if hardVsEasy <= 0.70 {
		t.Errorf("Hard beat Easy only %.0f%% of the time, want > 70%%", hardVsEasy*100)
	}
	if mediumVsEasy <= 0.50 {
		t.Errorf("Medium beat Easy only %.0f%% of the time, want > 50%%", mediumVsEasy*100)
	}
	// Hard vs Medium head to head, not their scores against Easy: both beat a
	// random opponent nearly always, so those numbers saturate and cannot order
	// the two.
	//
	// The threshold is deliberately loose. This is one synthetic graph, and how
	// much lookahead is worth is a property of the graph: regenerating it with
	// a different seed moved this figure between 49% and 78%. The real corpus
	// is the load-bearing measurement -- see TestDifficultyLadderRealCorpus --
	// and this test only guards against a gross regression.
	if hardVsMedium < 0.45 {
		t.Errorf("Hard beat Medium only %.0f%% of the time, want no worse than parity", hardVsMedium*100)
	}
}

// Bots must never hand the engine an illegal move; playGame fails the test if
// Submit rejects anything, so this is really a rules-conformance check.
func TestSimulatedGamesAlwaysTerminate(t *testing.T) {
	dict := newSimDict()
	for i := 0; i < 20; i++ {
		a, _ := New(Hard, rand.New(rand.NewPCG(uint64(i), 5)))
		b, _ := New(Hard, rand.New(rand.NewPCG(uint64(i), 6)))
		if got := playGame(t, dict, a, b); got == "" {
			t.Fatalf("game %d ended with no winner", i)
		}
	}
}

func BenchmarkHardChoose(b *testing.B) {
	dict := newSimDict()
	s, err := New(Hard, rand.New(rand.NewPCG(1, 1)))
	if err != nil {
		b.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e, err := game.New(dict, []game.PlayerID{"p1", "p2"}, "a b", time.Minute, now)
	if err != nil {
		b.Fatal(err)
	}
	board := BoardFor(e)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Choose(board); err != nil {
			b.Fatal(err)
		}
	}
}
