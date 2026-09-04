package dictionary

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/tiennm99dev/noitu/server/internal/vietnamese"

	_ "modernc.org/sqlite"
)

const fixtureSchema = `
CREATE TABLE words (word TEXT PRIMARY KEY, first TEXT NOT NULL, last TEXT NOT NULL, syllables INTEGER NOT NULL) WITHOUT ROWID;
CREATE INDEX idx_words_first ON words(first);
CREATE TABLE syllables (syllable TEXT PRIMARY KEY, out_degree INTEGER NOT NULL) WITHOUT ROWID;
CREATE TABLE aliases (variant TEXT PRIMARY KEY, canonical TEXT NOT NULL) WITHOUT ROWID;
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`

// fixtureAt builds a miniature dictionary with the same schema the builder
// emits. Tests never depend on the real 48k-word database, so they stay fast
// and run in CI without the 179 MB upstream download.
//
// Takes testing.TB so benchmarks get working cleanup: a zero-value testing.T
// never runs its Cleanup funcs, which leaks a temp directory per benchmark.
func fixtureAt(tb testing.TB, dir string) string {
	tb.Helper()

	path := filepath.Join(dir, "noitu.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		tb.Fatal(err)
	}
	defer db.Close()

	data := fixtureSchema + `
INSERT INTO meta VALUES ('source_license','CC BY-SA 4.0'),('word_count','7');
INSERT INTO words VALUES
  ('pháp luật','pháp','luật',2),
  ('pháp lý','pháp','lý',2),
  ('luật lệ','luật','lệ',2),
  ('lý do','lý','do',2),
  ('vô tuyến điện','vô','điện',3),
  ('công nghiệp hoá dầu','công','dầu',4),
  -- ends on "pháp", the only syllable with more than one continuation, so
  -- minOutDegree filtering has something to actually filter on.
  ('ngữ pháp','ngữ','pháp',2);
INSERT INTO syllables VALUES
  ('pháp',2),('luật',1),('lý',1),('lệ',0),('do',0),('vô',1),('điện',0),('công',1),('dầu',0),('ngữ',1);
-- "pháp lí" drifts in the LAST syllable, "luâto lệ" in the FIRST.
INSERT INTO aliases VALUES ('pháp lí','pháp lý'),('luâto lệ','luật lệ');
`
	if _, err := db.Exec(data); err != nil {
		tb.Fatal(err)
	}
	return path
}

func fixture(tb testing.TB) *Store {
	tb.Helper()

	store, err := Open(fixtureAt(tb, tb.TempDir()))
	if err != nil {
		tb.Fatalf("Open: %v", err)
	}
	return store
}

// writeDB creates a database from arbitrary SQL, for the malformed-input tests.
func writeDB(tb testing.TB, sqlText string) string {
	tb.Helper()

	path := filepath.Join(tb.TempDir(), "test.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		tb.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(sqlText); err != nil {
		tb.Fatal(err)
	}
	return path
}

func TestOpenMissingFile(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "absent.db")); err == nil {
		t.Fatal("Open succeeded on a missing file, want error")
	}
}

func TestOpenWrongSchema(t *testing.T) {
	path := writeDB(t, `CREATE TABLE unrelated (x TEXT)`)
	if _, err := Open(path); err == nil {
		t.Fatal("Open succeeded on a database with the wrong schema, want error")
	}
}

// A truncated dictionary has the right schema and opens cleanly. Left
// unchecked, the server starts, rejects every word a player types, and fails
// every room creation.
func TestOpenEmptyDictionary(t *testing.T) {
	path := writeDB(t, fixtureSchema+`
INSERT INTO meta VALUES ('source_license','CC BY-SA 4.0'),('word_count','48216');`)

	_, err := Open(path)
	if err == nil {
		t.Fatal("Open succeeded on an empty dictionary, want error")
	}
	if !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("error %q does not explain that the dictionary is incomplete", err)
	}
}

// A syllables table that disagrees with the words table would tell the bot a
// syllable has continuations that cannot be supplied.
func TestOpenInconsistentOutDegree(t *testing.T) {
	path := writeDB(t, fixtureSchema+`
INSERT INTO meta VALUES ('source_license','CC BY-SA 4.0'),('word_count','1');
INSERT INTO words VALUES ('pháp luật','pháp','luật',2);
INSERT INTO syllables VALUES ('pháp',7),('luật',0);`)

	if _, err := Open(path); err == nil {
		t.Fatal("Open succeeded with a stale syllables table, want error")
	}
}

func TestOpenOrphanAlias(t *testing.T) {
	path := writeDB(t, fixtureSchema+`
INSERT INTO meta VALUES ('source_license','CC BY-SA 4.0'),('word_count','1');
INSERT INTO words VALUES ('pháp luật','pháp','luật',2);
INSERT INTO syllables VALUES ('pháp',1),('luật',0);
INSERT INTO aliases VALUES ('phap luat','không tồn tại');`)

	if _, err := Open(path); err == nil {
		t.Fatal("Open succeeded with an alias pointing at a missing word, want error")
	}
}

// SQLite reads '#' as a URI fragment delimiter, so an unescaped path
// containing one opens a different file and reports a misleading schema error.
func TestOpenPathWithHash(t *testing.T) {
	dir := t.TempDir()
	src := fixtureAt(t, dir)

	hashed := filepath.Join(dir, "dict#1.db")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hashed, data, 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Open(hashed)
	if err != nil {
		t.Fatalf("Open on a path containing '#': %v", err)
	}
	if s.WordCount() != 7 {
		t.Errorf("WordCount = %d, want 7", s.WordCount())
	}
}

// The store must never write to the dictionary: production runs it from a
// read-only filesystem, and a stray journal file would break that.
func TestOpenIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := fixtureAt(t, dir)

	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	s := mustOpen(t, path)
	s.Resolve("pháp luật")
	_, _ = s.RandomOpeningWord(1)

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Error("dictionary file changed after read-only use")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		switch filepath.Ext(e.Name()) {
		case ".db-wal", ".db-shm", ".db-journal":
			t.Errorf("read-only open created side file %q", e.Name())
		}
	}
}

func mustOpen(tb testing.TB, path string) *Store {
	tb.Helper()
	s, err := Open(path)
	if err != nil {
		tb.Fatalf("Open: %v", err)
	}
	return s
}

func TestResolveExactWord(t *testing.T) {
	s := fixture(t)

	canonical, ok := s.Resolve("pháp luật")
	if !ok || canonical != "pháp luật" {
		t.Errorf("Resolve = (%q, %v), want (%q, true)", canonical, ok, "pháp luật")
	}
}

// A player typing an accepted variant must reach the canonical entry, and the
// caller must receive the canonical spelling so the chain links correctly.
func TestResolveAlias(t *testing.T) {
	s := fixture(t)

	canonical, ok := s.Resolve("pháp lí")
	if !ok {
		t.Fatal("alias did not resolve")
	}
	if canonical != "pháp lý" {
		t.Errorf("Resolve(%q) = %q, want the canonical %q", "pháp lí", canonical, "pháp lý")
	}
}

// Canonicalization can move the FIRST syllable too. An engine that link-checked
// against what the player typed would reject this legal move.
func TestResolveAliasDriftsFirstSyllable(t *testing.T) {
	s := fixture(t)

	canonical, ok := s.Resolve("luâto lệ")
	if !ok {
		t.Fatal("alias did not resolve")
	}

	first, ok := s.FirstSyllable(canonical)
	if !ok {
		t.Fatal("canonical has no first syllable")
	}
	if first == "luâto" {
		t.Error("FirstSyllable returned the typed syllable, not the canonical one")
	}
	if first != "luật" {
		t.Errorf("FirstSyllable(%q) = %q, want %q", canonical, first, "luật")
	}
}

func TestResolveUnknown(t *testing.T) {
	s := fixture(t)

	if canonical, ok := s.Resolve("không tồn tại"); ok {
		t.Errorf("Resolve returned %q for an unknown word, want not found", canonical)
	}
}

func TestFirstAndLastSyllable(t *testing.T) {
	s := fixture(t)

	tests := []struct{ word, first, last string }{
		{"pháp luật", "pháp", "luật"},
		{"vô tuyến điện", "vô", "điện"},
		{"công nghiệp hoá dầu", "công", "dầu"},
	}
	for _, tc := range tests {
		first, ok := s.FirstSyllable(tc.word)
		if !ok || first != tc.first {
			t.Errorf("FirstSyllable(%q) = (%q, %v), want (%q, true)", tc.word, first, ok, tc.first)
		}
		last, ok := s.LastSyllable(tc.word)
		if !ok || last != tc.last {
			t.Errorf("LastSyllable(%q) = (%q, %v), want (%q, true)", tc.word, last, ok, tc.last)
		}
	}

	// An alias is an accepted spelling, not a playable entry.
	if _, ok := s.LastSyllable("pháp lí"); ok {
		t.Error("LastSyllable succeeded on an alias, want false")
	}
	if _, ok := s.FirstSyllable("không tồn tại"); ok {
		t.Error("FirstSyllable succeeded on an unknown word, want false")
	}
}

func TestWordsStartingWith(t *testing.T) {
	s := fixture(t)

	got := slices.Collect(s.WordsStartingWith("pháp"))
	// Asserted in the documented order, not sorted first: the order is a
	// guarantee the bot relies on for reproducibility.
	want := []string{"pháp luật", "pháp lý"}
	if !slices.Equal(got, want) {
		t.Errorf("WordsStartingWith(%q) = %v, want %v in this order", "pháp", got, want)
	}

	if got := slices.Collect(s.WordsStartingWith("lệ")); len(got) != 0 {
		t.Errorf("WordsStartingWith(%q) = %v, want none", "lệ", got)
	}
	if got := slices.Collect(s.WordsStartingWith("không-có")); len(got) != 0 {
		t.Errorf("WordsStartingWith on an unknown syllable = %v, want none", got)
	}
}

// The iterator must not expose Store state: a caller collecting and sorting
// used to reorder the dictionary's own slice.
func TestWordsStartingWithIsolatesStoreState(t *testing.T) {
	s := fixture(t)

	collected := slices.Collect(s.WordsStartingWith("pháp"))
	slices.Reverse(collected)
	collected[0] = "MUTATED"

	again := slices.Collect(s.WordsStartingWith("pháp"))
	if !slices.Equal(again, []string{"pháp luật", "pháp lý"}) {
		t.Errorf("store state changed after a caller mutated its collected slice: %v", again)
	}
}

// A consumer that stops early must not keep iterating.
func TestWordsStartingWithEarlyExit(t *testing.T) {
	s := fixture(t)

	seen := 0
	for range s.WordsStartingWith("pháp") {
		seen++
		break
	}
	if seen != 1 {
		t.Errorf("iterated %d words after break, want 1", seen)
	}
}

func TestWordsStartingWithExcludesAliases(t *testing.T) {
	s := fixture(t)

	for w := range s.WordsStartingWith("pháp") {
		if w == "pháp lí" {
			t.Error("alias appeared among playable words")
		}
	}
}

func TestOutDegree(t *testing.T) {
	s := fixture(t)

	for syllable, want := range map[string]int{"pháp": 2, "luật": 1, "lệ": 0, "do": 0} {
		got, err := s.OutDegree(syllable)
		if err != nil {
			t.Errorf("OutDegree(%q): %v", syllable, err)
			continue
		}
		if got != want {
			t.Errorf("OutDegree(%q) = %d, want %d", syllable, got, want)
		}
	}
}

// A syllable with no continuations and one the dictionary has never seen are
// different situations, and the engine treats them differently.
func TestOutDegreeUnknownSyllable(t *testing.T) {
	s := fixture(t)

	if _, err := s.OutDegree("xyzzy"); !errors.Is(err, ErrNotFound) {
		t.Errorf("OutDegree of an unknown syllable = %v, want ErrNotFound", err)
	}
}

func TestRandomOpeningWord(t *testing.T) {
	s := fixture(t)

	// Only words ending on a syllable with a continuation are eligible.
	eligible := map[string]bool{"pháp luật": true, "pháp lý": true, "ngữ pháp": true}
	for i := 0; i < 50; i++ {
		word, err := s.RandomOpeningWord(1)
		if err != nil {
			t.Fatal(err)
		}
		if !eligible[word] {
			t.Fatalf("opening word %q ends on a dead end", word)
		}
	}
}

// The minimum must actually filter, not just be accepted.
func TestRandomOpeningWordRespectsMinimum(t *testing.T) {
	s := fixture(t)

	for i := 0; i < 50; i++ {
		word, err := s.RandomOpeningWord(2)
		if err != nil {
			t.Fatal(err)
		}
		last, _ := s.LastSyllable(word)
		degree, err := s.OutDegree(last)
		if err != nil {
			t.Fatal(err)
		}
		if degree < 2 {
			t.Fatalf("opening word %q ends on %q with out-degree %d, want >= 2", word, last, degree)
		}
	}
}

func TestRandomOpeningWordImpossible(t *testing.T) {
	s := fixture(t)

	if _, err := s.RandomOpeningWord(99); err == nil {
		t.Fatal("RandomOpeningWord succeeded with an unreachable minimum, want error")
	}
}

func TestCountsAndLicense(t *testing.T) {
	s := fixture(t)

	if got := s.WordCount(); got != 7 {
		t.Errorf("WordCount = %d, want 7", got)
	}
	if got := s.AliasCount(); got != 2 {
		t.Errorf("AliasCount = %d, want 2", got)
	}
	if got := s.License(); got != "CC BY-SA 4.0" {
		t.Errorf("License = %q, want %q", got, "CC BY-SA 4.0")
	}
}

// The corpus and player input must normalize identically, or a word in the
// dictionary stops matching the same string typed by a player.
func TestNormalizeRoundTrip(t *testing.T) {
	s := fixture(t)

	for _, raw := range []string{"PHÁP LUẬT", "  pháp   luật  ", "Pháp\tLuật", "pháp luật"} {
		word, syllables, err := vietnamese.Normalize(raw)
		if err != nil {
			t.Fatalf("Normalize(%q): %v", raw, err)
		}
		if !vietnamese.HasEnoughSyllables(syllables) {
			t.Errorf("Normalize(%q) produced too few syllables", raw)
		}
		if _, ok := s.Resolve(word); !ok {
			t.Errorf("Normalize(%q) = %q, which the dictionary does not resolve", raw, word)
		}
	}
}

// The server serves every room from one Store, so concurrent reads must be
// safe. Meaningful only under -race.
func TestConcurrentReads(t *testing.T) {
	s := fixture(t)

	const goroutines = 100
	const iterations = 200

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if _, ok := s.Resolve("pháp luật"); !ok {
					errs <- errors.New("resolve failed")
					return
				}
				// Collect, so each goroutine holds its own slice — the shape a
				// real caller uses.
				if got := slices.Collect(s.WordsStartingWith("pháp")); len(got) != 2 {
					errs <- errors.New("unexpected candidate count")
					return
				}
				if _, err := s.OutDegree("pháp"); err != nil {
					errs <- err
					return
				}
				if _, err := s.RandomOpeningWord(1); err != nil {
					errs <- err
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent read failed: %v", err)
	}
}

// Latency is measured by benchmark, not by wrapping time.Now() around a ~13ns
// map lookup: the platform clock quantizes to ~1ms, so such a test measures
// timer resolution rather than the code.
func BenchmarkResolve(b *testing.B) {
	s := fixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := s.Resolve("pháp luật"); !ok {
			b.Fatal("resolve failed")
		}
	}
}

func BenchmarkWordsStartingWith(b *testing.B) {
	s := fixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for range s.WordsStartingWith("pháp") {
		}
	}
}

func BenchmarkOutDegree(b *testing.B) {
	s := fixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.OutDegree("pháp"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRandomOpeningWord(b *testing.B) {
	s := fixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.RandomOpeningWord(1); err != nil {
			b.Fatal(err)
		}
	}
}
