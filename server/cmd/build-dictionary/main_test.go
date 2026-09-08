package main

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// fixtureSource writes a miniature stand-in for the kaikki export: the same
// JSONL shape, a handful of rows instead of 44k. Each row is a word and the
// language its Wiktionary entry is for. Tests never touch the real download.
func fixtureSource(t *testing.T, rows [][2]string) string {
	t.Helper()

	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		lines = append(lines, `{"word": "`+r[0]+`", "pos": "noun", "lang_code": "`+r[1]+`"}`)
	}
	return fixtureKaikki(t, lines...)
}

func defaultRows() [][2]string {
	return [][2]string{
		{"pháp luật", "vi"},
		{"pháp luật", "vi"}, // listed twice — must dedupe to one word
		{"luật lệ", "vi"},
		{"ngôn ngữ", "vi"},
		{"ngữ pháp", "vi"},
		{"hòa bình", "vi"},
		{"vô tuyến điện", "vi"}, // three syllables
		{"pháp", "vi"},          // single syllable — rejected
		{"covid 19", "vi"},      // digit — rejected
		{"hello world", "en"},   // another language's entry — never selected
	}
}

func buildFixture(t *testing.T, rows [][2]string, maxSyllables int) string {
	t.Helper()

	out := filepath.Join(t.TempDir(), "noitu.db")
	cfg := config{
		kaikki:       fixtureSource(t, rows),
		out:          out,
		maxSyllables: maxSyllables,
		minWords:     1,
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

func TestBuildProducesExpectedWords(t *testing.T) {
	db := openOut(t, buildFixture(t, defaultRows(), 0))

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM words`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if want := 6; count != want {
		t.Errorf("word count = %d, want %d", count, want)
	}

	// Every stored word must have at least two syllables.
	var short int
	if err := db.QueryRow(`SELECT COUNT(*) FROM words WHERE syllables < 2`).Scan(&short); err != nil {
		t.Fatal(err)
	}
	if short != 0 {
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
	db := openOut(t, buildFixture(t, defaultRows(), 0))

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
	db := openOut(t, buildFixture(t, defaultRows(), 0))

	var canonical string
	err := db.QueryRow(`SELECT canonical FROM aliases WHERE variant = ?`, "hoà bình").Scan(&canonical)
	if err != nil {
		t.Fatalf("alias for %q missing: %v", "hoà bình", err)
	}
	if canonical != "hòa bình" {
		t.Errorf("alias resolves to %q, want %q", canonical, "hòa bình")
	}

	// Every alias must point at a word that actually exists.
	var orphans int
	err = db.QueryRow(`SELECT COUNT(*) FROM aliases a
	                   LEFT JOIN words w ON w.word = a.canonical
	                   WHERE w.word IS NULL`).Scan(&orphans)
	if err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Errorf("%d aliases point at missing words, want 0", orphans)
	}
}

func TestBuildRecordsProvenance(t *testing.T) {
	db := openOut(t, buildFixture(t, defaultRows(), 0))

	for _, key := range []string{"source_url", "source_license", "attribution", "built_at", "word_count"} {
		var value string
		if err := db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&value); err != nil {
			t.Errorf("meta[%q] missing: %v", key, err)
			continue
		}
		if value == "" {
			t.Errorf("meta[%q] is empty", key)
		}
	}
}

// The floor exists so a schema change upstream fails the build loudly instead
// of silently shipping a near-empty dictionary.
func TestBuildFailsBelowMinWords(t *testing.T) {
	cfg := config{
		kaikki:   fixtureSource(t, defaultRows()),
		out:      filepath.Join(t.TempDir(), "noitu.db"),
		minWords: 1000,
	}
	if err := run(cfg); err == nil {
		t.Fatal("run succeeded with an unreachable min-words floor, want error")
	}
}

// A failed build must leave the previous good database untouched. Building in
// place would delete it and leave an empty file the server would happily open.
func TestFailedBuildPreservesPreviousOutput(t *testing.T) {
	out := buildFixture(t, defaultRows(), 0)

	before, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	// Same output path, but a floor no fixture can clear.
	cfg := config{
		kaikki:   fixtureSource(t, defaultRows()),
		out:      out,
		minWords: 1000,
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
