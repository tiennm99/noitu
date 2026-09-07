package game

import (
	"iter"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeDict is a hand-built word graph. Tests state the exact edges they need,
// so a rule can be exercised on a board small enough to reason about — no
// SQLite, no 48k-word corpus.
type fakeDict struct {
	words   map[string][2]string // canonical -> {first, last}
	aliases map[string]string
}

func newDict(words ...string) *fakeDict {
	d := &fakeDict{words: map[string][2]string{}, aliases: map[string]string{}}
	for _, w := range words {
		parts := strings.Fields(w)
		d.words[w] = [2]string{parts[0], parts[len(parts)-1]}
	}
	return d
}

func (d *fakeDict) alias(variant, canonical string) *fakeDict {
	d.aliases[variant] = canonical
	return d
}

func (d *fakeDict) Resolve(word string) (string, bool) {
	if _, ok := d.words[word]; ok {
		return word, true
	}
	c, ok := d.aliases[word]
	return c, ok
}

func (d *fakeDict) FirstSyllable(word string) (string, bool) {
	e, ok := d.words[word]
	return e[0], ok
}

func (d *fakeDict) LastSyllable(word string) (string, bool) {
	e, ok := d.words[word]
	return e[1], ok
}

func (d *fakeDict) WordsStartingWith(syllable string) iter.Seq[string] {
	var out []string
	for w, e := range d.words {
		if e[0] == syllable {
			out = append(out, w)
		}
	}
	slices.Sort(out)
	return slices.Values(out)
}

func (d *fakeDict) OutDegree(syllable string) (int, error) {
	n := 0
	for _, e := range d.words {
		if e[0] == syllable {
			n++
		}
	}
	return n, nil
}

const (
	alice = PlayerID("alice")
	bob   = PlayerID("bob")
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// standardDict gives both players room to move for several turns.
func standardDict() *fakeDict {
	return newDict(
		"ngôn ngữ",  // ngôn -> ngữ
		"ngữ pháp",  // ngữ  -> pháp
		"ngữ điệu",  // ngữ  -> điệu
		"pháp luật", // pháp -> luật
		"pháp lý",   // pháp -> lý
		"luật lệ",   // luật -> lệ
		"lý do",     // lý   -> do
		"vô tuyến điện",
	)
}

func newGame(t *testing.T, d Dictionary, opening string) *Engine {
	t.Helper()
	e, err := New(d, []PlayerID{alice, bob}, opening, 20*time.Second, t0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}

func TestNewSeedsStateFromOpening(t *testing.T) {
	e := newGame(t, standardDict(), "ngôn ngữ")

	if got := e.Current(); got != "ngữ" {
		t.Errorf("Current = %q, want %q", got, "ngữ")
	}
	if got := e.Turn(); got != alice {
		t.Errorf("Turn = %q, want %q", got, alice)
	}
	if !e.Used("ngôn ngữ") {
		t.Error("opening word is not marked used")
	}
	if got := e.ChainLength(); got != 1 {
		t.Errorf("ChainLength = %d, want 1", got)
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	d := standardDict()

	if _, err := New(nil, []PlayerID{alice, bob}, "ngôn ngữ", time.Second, t0); err == nil {
		t.Error("New accepted a nil dictionary")
	}
	if _, err := New(d, []PlayerID{alice}, "ngôn ngữ", time.Second, t0); err == nil {
		t.Error("New accepted a single player")
	}
	if _, err := New(d, []PlayerID{alice, bob}, "ngôn ngữ", 0, t0); err == nil {
		t.Error("New accepted a zero turn limit")
	}
	if _, err := New(d, []PlayerID{alice, bob}, "không tồn tại", time.Second, t0); err == nil {
		t.Error("New accepted an opening word outside the dictionary")
	}
}

func TestSubmitAcceptsLegalMove(t *testing.T) {
	e := newGame(t, standardDict(), "ngôn ngữ")

	move, reason := e.Submit(alice, "ngữ pháp", t0)
	if reason != ReasonNone {
		t.Fatalf("Submit rejected a legal move: %s", reason)
	}
	if move.Word != "ngữ pháp" || move.First != "ngữ" || move.Last != "pháp" {
		t.Errorf("move = %+v, want ngữ pháp / ngữ / pháp", move)
	}
	if got := e.Current(); got != "pháp" {
		t.Errorf("Current = %q, want %q", got, "pháp")
	}
	if got := e.Turn(); got != bob {
		t.Errorf("Turn = %q, want %q", got, bob)
	}
	if !e.Used("ngữ pháp") {
		t.Error("accepted word is not marked used")
	}
}

// One test per rejection reason: a boolean would not tell a player what to fix.
func TestSubmitRejectionReasons(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Engine)
		who   PlayerID
		word  string
		when  time.Time
		want  RejectReason
	}{
		{
			name: "not your turn",
			who:  bob, word: "ngữ pháp", when: t0,
			want: ReasonNotYourTurn,
		},
		{
			name: "single syllable",
			who:  alice, word: "ngữ", when: t0,
			want: ReasonTooFewSyllables,
		},
		{
			name: "empty input",
			who:  alice, word: "   ", when: t0,
			want: ReasonTooFewSyllables,
		},
		{
			name: "unknown word",
			who:  alice, word: "ngữ xyzzy", when: t0,
			want: ReasonNotInDictionary,
		},
		{
			name: "wrong link",
			who:  alice, word: "luật lệ", when: t0,
			want: ReasonWrongLink,
		},
		{
			name: "turn expired",
			who:  alice, word: "ngữ pháp", when: t0.Add(21 * time.Second),
			want: ReasonTimeout,
		},
	}

	// ReasonAlreadyUsed needs a played-out board, so it has its own tests:
	// TestSubmitBlocksReuseAcrossPlayers and TestSubmitBlocksReuseViaAlias.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newGame(t, standardDict(), "ngôn ngữ")
			if tc.setup != nil {
				tc.setup(e)
			}
			if _, got := e.Submit(tc.who, tc.word, tc.when); got != tc.want {
				t.Errorf("Submit = %s, want %s", got, tc.want)
			}
		})
	}
}

// A word is spent for the whole game, not per player.
func TestSubmitBlocksReuseAcrossPlayers(t *testing.T) {
	d := newDict(
		"ngôn ngữ",
		"ngữ pháp",
		"pháp ngữ", // lets play return to "ngữ"
		"ngữ điệu",
	)
	e := newGame(t, d, "ngôn ngữ")

	if _, r := e.Submit(alice, "ngữ pháp", t0); r != ReasonNone {
		t.Fatalf("alice's move rejected: %s", r)
	}
	if _, r := e.Submit(bob, "pháp ngữ", t0); r != ReasonNone {
		t.Fatalf("bob's move rejected: %s", r)
	}
	// Back on "ngữ": alice cannot replay the word she already used.
	if _, r := e.Submit(alice, "ngữ pháp", t0); r != ReasonAlreadyUsed {
		t.Errorf("replaying a used word = %s, want %s", r, ReasonAlreadyUsed)
	}
}

// Canonicalization can move the first syllable, so the link must be checked
// against the canonical form. Checking the typed form rejects a legal move.
func TestSubmitAcceptsAliasWithFirstSyllableDrift(t *testing.T) {
	d := newDict("ngôn ngữ", "ngữ pháp").alias("ngử pháp", "ngữ pháp")
	e := newGame(t, d, "ngôn ngữ")

	move, reason := e.Submit(alice, "ngử pháp", t0)
	if reason != ReasonNone {
		t.Fatalf("alias with first-syllable drift rejected: %s", reason)
	}
	if move.Word != "ngữ pháp" {
		t.Errorf("move.Word = %q, want the canonical %q", move.Word, "ngữ pháp")
	}
	if move.Typed != "ngử pháp" {
		t.Errorf("move.Typed = %q, want what the player typed", move.Typed)
	}
	// The chain must continue from the canonical's last syllable.
	if got := e.Current(); got != "pháp" {
		t.Errorf("Current = %q, want %q", got, "pháp")
	}
}

// An alias must be spent along with its canonical: the same word cannot be
// played twice under two spellings.
func TestSubmitBlocksReuseViaAlias(t *testing.T) {
	// "ngữ điệu" keeps a move available after play returns to "ngữ", so the
	// game does not end on a dead end before the reuse can be attempted.
	d := newDict("ngôn ngữ", "ngữ pháp", "pháp ngữ", "ngữ điệu").alias("ngử pháp", "ngữ pháp")
	e := newGame(t, d, "ngôn ngữ")

	e.Submit(alice, "ngữ pháp", t0)
	e.Submit(bob, "pháp ngữ", t0)

	if _, r := e.Submit(alice, "ngử pháp", t0); r != ReasonAlreadyUsed {
		t.Errorf("replaying via an alias = %s, want %s", r, ReasonAlreadyUsed)
	}
}

func TestSubmitScoring(t *testing.T) {
	d := newDict("ngôn ngữ", "ngữ pháp", "pháp vô tuyến điện")
	e := newGame(t, d, "ngôn ngữ")

	// Spec: 10 + 2*chainLength + 5*(syllables-2), where chainLength counts the
	// words already down, opening word included. Asserted as literals so the
	// test pins the specified formula rather than whatever the code computes.
	two, _ := e.Submit(alice, "ngữ pháp", t0)
	if want := 12; two.Points != want { // 10 + 2*1 + 5*0
		t.Errorf("two-syllable word at chain 1 scored %d, want %d", two.Points, want)
	}

	four, r := e.Submit(bob, "pháp vô tuyến điện", t0)
	if r != ReasonNone {
		t.Fatalf("four-syllable word rejected: %s", r)
	}
	if want := 24; four.Points != want { // 10 + 2*2 + 5*2
		t.Errorf("four-syllable word at chain 2 scored %d, want %d", four.Points, want)
	}
	if four.Points <= two.Points {
		t.Error("longer word did not score more than a shorter one")
	}
}

func TestLegalMovesAndHasLegalMove(t *testing.T) {
	e := newGame(t, standardDict(), "ngôn ngữ")

	got := e.LegalMoves()
	want := []string{"ngữ pháp", "ngữ điệu"}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("LegalMoves = %v, want %v", got, want)
	}
	if !e.HasLegalMove() {
		t.Error("HasLegalMove = false with moves available")
	}

	// Once both continuations are spent there is nothing left from "ngữ".
	e.Submit(alice, "ngữ pháp", t0)
	if got := e.Current(); got != "pháp" {
		t.Fatalf("Current = %q, want pháp", got)
	}
}

// A dead end does not end the game by itself. The player who walked into one
// keeps the turn they were given, and it is the clock that takes it from them.
func TestDeadEndLeavesTheTurnWithThePlayer(t *testing.T) {
	e := deadEndGame(t)

	if e.Over() {
		t.Fatal("game ended on the move into a dead end instead of leaving the turn to be played")
	}
	if e.Turn() != alice {
		t.Errorf("Turn = %q, want %q", e.Turn(), alice)
	}
	if e.HasLegalMove() {
		t.Error("HasLegalMove = true in a position with nothing to play")
	}
}

// Losing a turn nobody could have answered is reported as the dead end it was,
// not as time spent thinking.
func TestDeadEndEndsOnTheClockAsNoLegalMove(t *testing.T) {
	e := deadEndGame(t)

	if !e.Timeout(e.Deadline().Add(time.Nanosecond)) {
		t.Fatal("Timeout did not end an expired turn")
	}
	if e.Winner() != bob {
		t.Errorf("Winner = %q, want %q (alice had no move)", e.Winner(), bob)
	}
	if got := e.Snapshot().EndReason; got != EndNoLegalMove {
		t.Errorf("EndReason = %s, want %s", got, EndNoLegalMove)
	}
}

// NoMove settles a dead end without a clock, which is what the room does for
// the bot: it has nothing to wait for.
func TestNoMoveEndsADeadEndImmediately(t *testing.T) {
	e := deadEndGame(t)

	if !e.NoMove() {
		t.Fatal("NoMove = false in a position with nothing to play")
	}
	if e.Winner() != bob {
		t.Errorf("Winner = %q, want %q", e.Winner(), bob)
	}
	if got := e.Snapshot().EndReason; got != EndNoLegalMove {
		t.Errorf("EndReason = %s, want %s", got, EndNoLegalMove)
	}

	if e.NoMove() {
		t.Error("NoMove ended an already finished game a second time")
	}
}

// NoMove is not a resignation button: a player who still has words to play
// cannot use it to end the game.
func TestNoMoveRefusesAPlayablePosition(t *testing.T) {
	e := newGame(t, standardDict(), "ngôn ngữ")

	if e.NoMove() {
		t.Error("NoMove = true with legal moves available")
	}
	if e.Over() {
		t.Error("game ended from a playable position")
	}
}

// deadEndGame leaves alice on turn with nothing to play: "lệ" starts no word.
func deadEndGame(t *testing.T) *Engine {
	t.Helper()

	d := newDict("ngôn ngữ", "ngữ luật", "luật lệ")
	e := newGame(t, d, "ngôn ngữ")

	if _, r := e.Submit(alice, "ngữ luật", t0); r != ReasonNone {
		t.Fatalf("alice's move rejected: %s", r)
	}
	if _, r := e.Submit(bob, "luật lệ", t0); r != ReasonNone {
		t.Fatalf("bob's move rejected: %s", r)
	}
	return e
}

// What a losing player is shown: a few of the words the position still had,
// and nothing at all when it had none.
func TestSuggestions(t *testing.T) {
	e := newGame(t, standardDict(), "ngôn ngữ")

	if got, want := e.Suggestions(3), []string{"ngữ pháp", "ngữ điệu"}; !slices.Equal(got, want) {
		t.Errorf("Suggestions(3) = %v, want %v", got, want)
	}
	// Capped, and stable: the same position answers the same way every time.
	if got := e.Suggestions(1); !slices.Equal(got, []string{"ngữ pháp"}) {
		t.Errorf("Suggestions(1) = %v, want [ngữ pháp]", got)
	}
	if got := e.Suggestions(0); got != nil {
		t.Errorf("Suggestions(0) = %v, want nil", got)
	}

	// A played word is no longer a suggestion.
	e.Submit(alice, "ngữ pháp", t0)
	for _, word := range e.Suggestions(3) {
		if word == "ngữ pháp" {
			t.Error("Suggestions offered a word that had already been played")
		}
	}

	if got := deadEndGame(t).Suggestions(3); len(got) != 0 {
		t.Errorf("Suggestions in a dead end = %v, want none", got)
	}
}

func TestIsExpiredBoundary(t *testing.T) {
	e := newGame(t, standardDict(), "ngôn ngữ")
	deadline := e.Deadline()

	if e.IsExpired(deadline.Add(-time.Nanosecond)) {
		t.Error("expired just before the deadline")
	}
	if e.IsExpired(deadline) {
		t.Error("expired exactly on the deadline")
	}
	if !e.IsExpired(deadline.Add(time.Nanosecond)) {
		t.Error("not expired just after the deadline")
	}
}

func TestTimeoutAwardsOpponent(t *testing.T) {
	e := newGame(t, standardDict(), "ngôn ngữ")

	if e.Timeout(t0) {
		t.Error("Timeout fired before the deadline")
	}
	if !e.Timeout(t0.Add(21 * time.Second)) {
		t.Fatal("Timeout did not fire after the deadline")
	}
	if e.Winner() != bob {
		t.Errorf("Winner = %q, want %q (alice ran out of time)", e.Winner(), bob)
	}
	if e.Timeout(t0.Add(30 * time.Second)) {
		t.Error("Timeout fired twice")
	}
}

func TestResign(t *testing.T) {
	e := newGame(t, standardDict(), "ngôn ngữ")

	if !e.Resign(alice) {
		t.Fatal("Resign returned false")
	}
	if e.Winner() != bob {
		t.Errorf("Winner = %q, want %q", e.Winner(), bob)
	}
	if e.Snapshot().EndReason != EndResigned {
		t.Errorf("EndReason = %s, want %s", e.Snapshot().EndReason, EndResigned)
	}
	if e.Resign(bob) {
		t.Error("Resign succeeded on a finished game")
	}
}

// A finished game is not a turn-order problem, and the distinction reaches the
// player as copy.
func TestSubmitAfterGameOver(t *testing.T) {
	e := newGame(t, standardDict(), "ngôn ngữ")
	e.Resign(alice)

	if _, r := e.Submit(bob, "ngữ pháp", t0); r != ReasonGameOver {
		t.Errorf("Submit after the game ended = %s, want %s", r, ReasonGameOver)
	}
}

// An opening whose last syllable starts nothing hands the first player a game
// they have already lost, with no move and no reason -- it would resolve only
// on the turn timer, reported as a timeout. New must refuse it.
func TestNewRejectsDeadEndOpening(t *testing.T) {
	// "lệ" starts no word.
	d := newDict("luật lệ", "ngôn ngữ", "ngữ pháp")

	_, err := New(d, []PlayerID{alice, bob}, "luật lệ", 20*time.Second, t0)
	if err == nil {
		t.Fatal("New accepted an opening that leaves the first player no move")
	}
	if !strings.Contains(err.Error(), "starts no other word") {
		t.Errorf("error %q does not explain the dead end", err)
	}
}

func TestNewRejectsDuplicatePlayers(t *testing.T) {
	if _, err := New(standardDict(), []PlayerID{alice, alice}, "ngôn ngữ", time.Second, t0); err == nil {
		t.Error("New accepted the same player twice")
	}
}

func TestEndReasonStrings(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range []EndReason{EndNone, EndTimeout, EndNoLegalMove, EndResigned} {
		s := r.String()
		if s == "" || s == "unknown" {
			t.Errorf("EndReason(%d).String() = %q", r, s)
		}
		if seen[s] {
			t.Errorf("duplicate description %q", s)
		}
		seen[s] = true
	}
	if got := EndReason(99).String(); got != "unknown" {
		t.Errorf("unknown EndReason string = %q", got)
	}
}

// Move.Typed must carry what the player actually sent, so the UI can show that
// a correction happened.
func TestMoveRecordsRawInput(t *testing.T) {
	e := newGame(t, standardDict(), "ngôn ngữ")

	const raw = "  NGỮ   Pháp  "
	move, r := e.Submit(alice, raw, t0)
	if r != ReasonNone {
		t.Fatalf("Submit rejected %q: %s", raw, r)
	}
	if move.Typed != raw {
		t.Errorf("move.Typed = %q, want the raw input %q", move.Typed, raw)
	}
	if move.Word != "ngữ pháp" {
		t.Errorf("move.Word = %q, want the canonical form", move.Word)
	}
}

// Snapshot must not hand out engine state.
func TestSnapshotIsACopy(t *testing.T) {
	e := newGame(t, standardDict(), "ngôn ngữ")
	e.Submit(alice, "ngữ pháp", t0)

	snap := e.Snapshot()
	snap.History[0].Word = "MUTATED"
	snap.Scores[alice] = 9999

	fresh := e.Snapshot()
	if fresh.History[0].Word == "MUTATED" {
		t.Error("mutating a snapshot's history changed engine state")
	}
	if fresh.Scores[alice] == 9999 {
		t.Error("mutating a snapshot's scores changed engine state")
	}
}

func TestRejectReasonStrings(t *testing.T) {
	// Every reason needs a distinct, non-empty description: these become
	// player-facing messages.
	seen := map[string]bool{}
	for _, r := range []RejectReason{
		ReasonNone, ReasonNotYourTurn, ReasonTooFewSyllables,
		ReasonNotInDictionary, ReasonWrongLink, ReasonAlreadyUsed, ReasonTimeout,
	} {
		s := r.String()
		if s == "" || s == "unknown" {
			t.Errorf("RejectReason(%d).String() = %q", r, s)
		}
		if seen[s] {
			t.Errorf("duplicate description %q", s)
		}
		seen[s] = true
	}
}
