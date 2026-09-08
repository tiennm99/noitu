package main

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// buildFixture runs the whole pipeline on the committed mini dump: twelve
// pages, both dialects, a redirect, an English-only page and two pages that
// accept() rejects. Tests never touch the real download.
func buildFixture(t *testing.T) string {
	t.Helper()

	out := filepath.Join(t.TempDir(), "noitu.db")
	cfg := config{
		dump:     miniDump,
		out:      out,
		minWords: 1,
		minPages: 1,
	}
	if err := run(cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	return out
}

func openOut(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestBuildProducesExpectedWords(t *testing.T) {
	db := openOut(t, buildFixture(t))

	if got := count(t, db, `SELECT COUNT(*) FROM words`); got != 6 {
		t.Errorf("word count = %d, want 6", got)
	}
	// Every stored word must have at least two syllables.
	if short := count(t, db, `SELECT COUNT(*) FROM words WHERE syllables < 2`); short != 0 {
		t.Errorf("%d words have fewer than 2 syllables, want 0", short)
	}

	// Three-syllable words are kept and split on first/last, not on a pair.
	var first, last string
	var syllables int
	err := db.QueryRow(`SELECT first, last, syllables FROM words WHERE word = ?`, "vô tuyến điện").
		Scan(&first, &last, &syllables)
	if err != nil {
		t.Fatalf("three-syllable word missing: %v", err)
	}
	if first != "vô" || last != "điện" || syllables != 3 {
		t.Errorf("got first=%q last=%q syllables=%d, want vô/điện/3", first, last, syllables)
	}
}

func TestBuildComputesOutDegree(t *testing.T) {
	db := openOut(t, buildFixture(t))

	// "pháp luật" and "pháp" (rejected) mean exactly one word starts with "pháp".
	assertOutDegree(t, db, "pháp", 1)
	// "luật lệ" starts with "luật".
	assertOutDegree(t, db, "luật", 1)
	// "bình" ends a word but starts none — a dead end, recorded as zero.
	assertOutDegree(t, db, "bình", 0)
}

func assertOutDegree(t *testing.T, db *sql.DB, syllable string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT out_degree FROM syllables WHERE syllable = ?`, syllable).Scan(&got); err != nil {
		t.Fatalf("out_degree for %q: %v", syllable, err)
	}
	if got != want {
		t.Errorf("out_degree(%q) = %d, want %d", syllable, got, want)
	}
}

func TestBuildWritesAliases(t *testing.T) {
	db := openOut(t, buildFixture(t))

	var canonical string
	err := db.QueryRow(`SELECT canonical FROM aliases WHERE variant = ?`, "hoà bình").Scan(&canonical)
	if err != nil {
		t.Fatalf("alias for %q missing: %v", "hoà bình", err)
	}
	if canonical != "hòa bình" {
		t.Errorf("alias resolves to %q, want %q", canonical, "hòa bình")
	}

	// Every alias must point at a word that actually exists.
	orphans := count(t, db, `SELECT COUNT(*) FROM aliases a LEFT JOIN words w ON w.word = a.canonical WHERE w.word IS NULL`)
	if orphans != 0 {
		t.Errorf("%d aliases point at missing words, want 0", orphans)
	}
}

func TestBuildWritesMeanings(t *testing.T) {
	db := openOut(t, buildFixture(t))

	rows, err := db.Query(`SELECT ord, pos, gloss FROM meanings WHERE word = ? ORDER BY ord`, "pháp luật")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []sense
	for rows.Next() {
		var ord int
		var s sense
		if err := rows.Scan(&ord, &s.pos, &s.gloss); err != nil {
			t.Fatal(err)
		}
		if ord != len(got) {
			t.Errorf("ord = %d, want %d (0-based, dense)", ord, len(got))
		}
		got = append(got, s)
	}
	assertSenses(t, got, []sense{
		{"danh từ", "Hệ thống các quy tắc xử sự do nhà nước đặt ra."},
		{"danh từ", "(nghĩa rộng) Kỷ cương nói chung."},
		{"động từ", "(hiếm) Xử theo luật."},
	})

	// A word whose only definition stripped to nothing has no rows, and the
	// meta counts describe the table.
	if n := count(t, db, `SELECT COUNT(*) FROM meanings WHERE word = ?`, "luật lệ"); n != 0 {
		t.Errorf("luật lệ has %d meanings, want 0", n)
	}
	total := count(t, db, `SELECT COUNT(*) FROM meanings`)
	withMeaning := count(t, db, `SELECT COUNT(DISTINCT word) FROM meanings`)
	for key, want := range map[string]int{"meaning_count": total, "words_with_meaning": withMeaning} {
		var raw string
		if err := db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&raw); err != nil {
			t.Fatalf("meta[%q]: %v", key, err)
		}
		if raw != strconv.Itoa(want) {
			t.Errorf("meta[%q] = %s, want %d", key, raw, want)
		}
	}
	if withMeaning != 5 {
		t.Errorf("words with a meaning = %d, want 5 of 6", withMeaning)
	}
}

func TestBuildRecordsProvenance(t *testing.T) {
	db := openOut(t, buildFixture(t))

	want := map[string]string{"builder_version": builderVer, "source_url": dumpSourceURL, "source_pages": "9"}
	for _, key := range []string{"source_url", "source_license", "attribution", "built_at", "word_count",
		"builder_version", "source_sha256", "source_pages", "source_fetched_at", "meaning_count", "words_with_meaning"} {
		var value string
		if err := db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value); err != nil {
			t.Errorf("meta[%q] missing: %v", key, err)
			continue
		}
		if value == "" {
			t.Errorf("meta[%q] is empty", key)
		}
		if w, ok := want[key]; ok && value != w {
			t.Errorf("meta[%q] = %q, want %q", key, value, w)
		}
	}
	if n := count(t, db, `SELECT COUNT(*) FROM meta WHERE key = 'source_rows'`); n != 0 {
		t.Error("source_rows belonged to the previous source format and must be gone")
	}
}

// The floor exists so a markup change upstream fails the build loudly instead
// of silently shipping a near-empty dictionary.
func TestBuildFailsBelowMinWords(t *testing.T) {
	cfg := config{
		dump:     miniDump,
		out:      filepath.Join(t.TempDir(), "noitu.db"),
		minWords: 1000,
		minPages: 1,
	}
	if err := run(cfg); err == nil {
		t.Fatal("run succeeded with an unreachable min-words floor, want error")
	}
}

func TestRunRequiresExactlyOneInput(t *testing.T) {
	if err := run(config{}); err == nil {
		t.Error("run with no input succeeded")
	}
	if err := run(config{dump: miniDump, words: "x.txt"}); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("run with both inputs: %v", err)
	}
}

// A failed build must leave the previous good database untouched. Building in
// place would delete it and leave an empty file the server would happily open.
func TestFailedBuildPreservesPreviousOutput(t *testing.T) {
	out := buildFixture(t)

	before, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	// Same output path, but a floor no fixture can clear.
	cfg := config{
		dump:     miniDump,
		out:      out,
		minWords: 1000,
		minPages: 1,
	}
	if err := run(cfg); err == nil {
		t.Fatal("run succeeded with an unreachable floor, want error")
	}

	after, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("previous output was destroyed by the failed build: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("previous output was modified by the failed build")
	}
	if _, err := os.Stat(out + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Error("temp database left behind after a failed build")
	}
}

// --- the fixture word list --------------------------------------------------

func writeWordList(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWordListCarriesTabSeparatedMeanings(t *testing.T) {
	list := writeWordList(t, "# comment\n"+
		"học sinh\tdanh từ|Người học ở trường.\tđộng từ|Đi học.\n"+
		"sinh viên\tNgười học ở trường đại học.\n"+
		"sinh hoạt\n"+
		"sinh sản\t\t\n")
	out := filepath.Join(t.TempDir(), "fixture.db")
	if err := run(config{words: list, out: out, minWords: 1}); err != nil {
		t.Fatal(err)
	}
	db := openOut(t, out)

	rows, err := db.Query(`SELECT word, ord, pos, gloss FROM meanings ORDER BY word, ord`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type row struct {
		word string
		ord  int
		s    sense
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.word, &r.ord, &r.s.pos, &r.s.gloss); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	want := []row{
		{"học sinh", 0, sense{"danh từ", "Người học ở trường."}},
		{"học sinh", 1, sense{"động từ", "Đi học."}},
		{"sinh viên", 0, sense{"", "Người học ở trường đại học."}},
	}
	if len(got) != len(want) {
		t.Fatalf("meanings = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if n := count(t, db, `SELECT COUNT(*) FROM words`); n != 4 {
		t.Errorf("words = %d, want 4 (a line without a tab is still a word)", n)
	}
	// Fixture builds carry no upstream licence and are exempt from the
	// coverage floor: two of four words have a meaning here.
	var license string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'source_license'`).Scan(&license); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(license, "none") {
		t.Errorf("fixture licence = %q, want a statement that no upstream data applies", license)
	}
}

// --- verify -----------------------------------------------------------------

// brokenDB writes a database that passes every schema check and then breaks
// one invariant, to prove verify() reads what is on disk.
func brokenDB(t *testing.T, extraSQL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "broken.db")
	words := map[string]entry{"pháp luật": {"pháp luật", "pháp", "luật", 2}}
	meanings := map[string][]sense{"pháp luật": {{"danh từ", "Luật."}}}
	if err := writeTo(path, words, meanings, nil, sourceSpec{table: "test"}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(extraSQL); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerifyRejectsBrokenMeanings(t *testing.T) {
	cases := map[string]string{
		"orphan meaning row": `INSERT INTO meanings VALUES ('không có', 0, '', 'Một nghĩa.')`,
		"empty gloss":        `INSERT INTO meanings VALUES ('pháp luật', 1, '', '')`,
		"over the cap":       `INSERT INTO meanings VALUES ('pháp luật', 1, '', '` + strings.Repeat("a", maxGlossRunes+1) + `')`,
		"more senses than the cap": `INSERT INTO meanings VALUES ('pháp luật', 1, '', 'b'), ('pháp luật', 2, '', 'c'),
			('pháp luật', 3, '', 'd'), ('pháp luật', 4, '', 'e'), ('pháp luật', 5, '', 'f')`,
	}
	for name, sqlText := range cases {
		t.Run(name, func(t *testing.T) {
			path := brokenDB(t, sqlText)
			if err := verify(path, 1, false); err == nil {
				t.Error("verify passed a database that breaks a meanings invariant")
			}
		})
	}
	if err := verify(brokenDB(t, `SELECT 1`), 1, false); err != nil {
		t.Errorf("verify rejected a sound database: %v", err)
	}
}

func TestVerifyCoverageFloorAppliesToCorpusBuildsOnly(t *testing.T) {
	// One word with a meaning, one without: 50%, under the floor.
	path := brokenDB(t, `INSERT INTO words VALUES ('luật lệ', 'luật', 'lệ', 2);
		INSERT INTO syllables VALUES ('lệ', 0); UPDATE syllables SET out_degree = 1 WHERE syllable = 'luật'`)
	if err := verify(path, 1, true); err == nil || !strings.Contains(err.Error(), "have a meaning") {
		t.Errorf("corpus verify with 50%% coverage: %v, want the coverage floor named", err)
	}
	if err := verify(path, 1, false); err != nil {
		t.Errorf("fixture verify applied the coverage floor: %v", err)
	}
}
