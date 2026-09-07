package bot

import (
	"math/rand/v2"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/tiennm99dev/noitu/server/internal/dictionary"
	"github.com/tiennm99dev/noitu/server/internal/game"
)

// realDictPath is the derived dictionary. It is a build artifact, not in git,
// so every test here skips when it is absent — CI runs without it.
const realDictPath = "../../../data/noitu.db"

func realDict(tb testing.TB) *dictionary.Store {
	tb.Helper()

	if _, err := os.Stat(realDictPath); err != nil {
		tb.Skipf("real dictionary not built (run 'make fetch-dict && make dict'): %v", err)
	}
	store, err := dictionary.Open(realDictPath)
	if err != nil {
		tb.Fatalf("open real dictionary: %v", err)
	}
	return store
}

// playRealGame runs one bot-vs-bot game on the real corpus and reports the
// winner plus how many moves it took.
func playRealGame(tb testing.TB, dict game.Dictionary, first, second Strategy, seed uint64) (game.PlayerID, int) {
	tb.Helper()

	const p1, p2 = game.PlayerID("first"), game.PlayerID("second")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	store := dict.(*dictionary.Store)
	var e *game.Engine
	// RandomOpeningWord can still return a word the engine refuses, so retry.
	for attempt := 0; attempt < 20 && e == nil; attempt++ {
		opening, err := store.RandomOpeningWord(5)
		if err != nil {
			tb.Fatalf("RandomOpeningWord: %v", err)
		}
		e, _ = game.New(dict, []game.PlayerID{p1, p2}, opening, time.Minute, now)
	}
	if e == nil {
		tb.Fatal("could not find a playable opening word")
	}

	board := BoardFor(e)
	strategies := map[game.PlayerID]Strategy{p1: first, p2: second}

	moves := 0
	for turn := 0; turn < 2000 && !e.Over(); turn++ {
		p := e.Turn()
		move, err := strategies[p].Choose(board)
		if err == ErrNoMove {
			// The dead end the bot walked into. The engine leaves a stuck
			// player their turn, so the harness settles it the way the room
			// settles a bot's: immediately, against the player to act.
			if !e.NoMove() {
				tb.Fatalf("bot had no move at %q but the engine says the position has one", e.Current())
			}
			break
		}
		if err != nil {
			tb.Fatalf("Choose: %v", err)
		}

		now = now.Add(time.Second)
		if _, reason := e.Submit(p, move, now); reason != game.ReasonNone {
			tb.Fatalf("bot %s played %q which the engine rejected: %s", p, move, reason)
		}
		moves++
	}

	if !e.Over() {
		tb.Fatal("game did not finish within the turn cap")
	}
	return e.Winner(), moves
}

// The difficulty ladder that actually matters. The synthetic ladder measures
// one hand-made graph, and how much lookahead is worth turns out to be a
// property of the graph; this measures the dictionary players will face.
func TestDifficultyLadderRealCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("real-corpus simulation skipped in short mode")
	}
	dict := realDict(t)

	rate := func(challenger, defender Difficulty, n int) (float64, float64) {
		const p1 = game.PlayerID("first")
		wins, totalMoves := 0, 0
		for i := 0; i < n; i++ {
			c, err := New(challenger, rand.New(rand.NewPCG(uint64(i), 1)))
			if err != nil {
				t.Fatal(err)
			}
			d, err := New(defender, rand.New(rand.NewPCG(uint64(i), 2)))
			if err != nil {
				t.Fatal(err)
			}

			// Alternate sides: moving first is an advantage in its own right,
			// and fixed seating reports that advantage as skill.
			challengerFirst := i%2 == 0
			var winner game.PlayerID
			var moves int
			if challengerFirst {
				winner, moves = playRealGame(t, dict, c, d, uint64(i))
			} else {
				winner, moves = playRealGame(t, dict, d, c, uint64(i))
			}
			if (winner == p1) == challengerFirst {
				wins++
			}
			totalMoves += moves
		}
		return float64(wins) / float64(n), float64(totalMoves) / float64(n)
	}

	const games = 60

	hardVsEasy, hardEasyLen := rate(Hard, Easy, games)
	mediumVsEasy, _ := rate(Medium, Easy, games)
	hardVsMedium, hardMediumLen := rate(Hard, Medium, games)
	_, easyLen := rate(Easy, Easy, games)

	t.Logf("real corpus, %d games each: hard-vs-easy %.0f%% (%.1f moves), medium-vs-easy %.0f%%, hard-vs-medium %.0f%% (%.1f moves), easy-vs-easy %.1f moves",
		games, hardVsEasy*100, hardEasyLen, mediumVsEasy*100, hardVsMedium*100, hardMediumLen, easyLen)

	if hardVsEasy <= 0.70 {
		t.Errorf("Hard beat Easy %.0f%% of the time, want > 70%%", hardVsEasy*100)
	}
	if mediumVsEasy <= 0.50 {
		t.Errorf("Medium beat Easy %.0f%% of the time, want > 50%%", mediumVsEasy*100)
	}
	if hardVsMedium <= 0.50 {
		t.Errorf("Hard beat Medium %.0f%% of the time, want > 50%%", hardVsMedium*100)
	}
}

// The success criterion is a decision within 150ms on the real dictionary, so
// measure it there rather than on a synthetic graph.
func TestHardChooseLatencyRealCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("latency check skipped in short mode")
	}
	dict := realDict(t)

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var durations []time.Duration

	for g := 0; g < 40; g++ {
		opening, err := dict.RandomOpeningWord(20)
		if err != nil {
			t.Fatal(err)
		}
		e, err := game.New(dict, []game.PlayerID{"p1", "p2"}, opening, time.Minute, now)
		if err != nil {
			continue // dead-end opening; try another
		}
		board := BoardFor(e)
		hard, _ := New(Hard, rand.New(rand.NewPCG(uint64(g), 1)))
		easy, _ := New(Easy, rand.New(rand.NewPCG(uint64(g), 2)))

		for turn := 0; turn < 40 && !e.Over(); turn++ {
			s, timed := hard, true
			if e.Turn() == "p2" {
				s, timed = easy, false
			}
			start := time.Now()
			move, err := s.Choose(board)
			elapsed := time.Since(start)
			if err != nil {
				break
			}
			if timed {
				durations = append(durations, elapsed)
			}
			now = now.Add(time.Second)
			if _, r := e.Submit(e.Turn(), move, now); r != game.ReasonNone {
				t.Fatalf("engine rejected bot move %q: %s", move, r)
			}
		}
	}

	if len(durations) == 0 {
		t.Fatal("no Hard decisions were measured")
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	worst := durations[len(durations)-1]

	t.Logf("Hard decisions on the real corpus: n=%d p50=%v p95=%v max=%v",
		len(durations), durations[len(durations)/2],
		durations[int(float64(len(durations))*0.95)], worst)

	if worst > 150*time.Millisecond {
		t.Errorf("slowest Hard decision %v, want <= 150ms", worst)
	}
}

// BenchmarkHardChooseRealCorpus is the benchmark the success criteria name.
// The synthetic one measures a graph a tenth the size.
func BenchmarkHardChooseRealCorpus(b *testing.B) {
	dict := realDict(b)

	s, err := New(Hard, rand.New(rand.NewPCG(1, 1)))
	if err != nil {
		b.Fatal(err)
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var e *game.Engine
	for attempt := 0; attempt < 20 && e == nil; attempt++ {
		opening, err := dict.RandomOpeningWord(50)
		if err != nil {
			b.Fatal(err)
		}
		e, _ = game.New(dict, []game.PlayerID{"p1", "p2"}, opening, time.Minute, now)
	}
	if e == nil {
		b.Fatal("could not find a playable opening word")
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
