package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// exportFixture builds the mini dump with --export and returns both the
// database and the corpus it wrote.
func exportFixture(t *testing.T) (db, corpus string) {
	t.Helper()
	dir := t.TempDir()
	db, corpus = filepath.Join(dir, "from-dump.db"), filepath.Join(dir, "dictionary.txt")
	if err := run(config{dump: miniDump, export: corpus, out: db, minWords: 1, minPages: 1}); err != nil {
		t.Fatalf("run: %v", err)
	}
	return db, corpus
}

// The corpus is only worth committing if building from it gives back the
// database the dump gave: same words, syllables, aliases and meanings, row
// for row.
func TestCorpusRoundTripsTheDumpBuild(t *testing.T) {
	fromDump, corpus := exportFixture(t)
	fromCorpus := filepath.Join(t.TempDir(), "from-corpus.db")
	if err := run(config{corpus: corpus, out: fromCorpus, minWords: 1}); err != nil {
		t.Fatalf("run from corpus: %v", err)
	}

	db := openOut(t, fromDump)
	if _, err := db.Exec(`ATTACH DATABASE ? AS c`, "file:"+fromCorpus+"?mode=ro"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"words", "syllables", "aliases", "meanings"} {
		if n := count(t, db, `SELECT COUNT(*) FROM main.`+table); n == 0 && table != "aliases" {
			t.Errorf("%s is empty, so the comparison proves nothing", table)
		}
		for _, q := range []string{
			`SELECT COUNT(*) FROM (SELECT * FROM main.` + table + ` EXCEPT SELECT * FROM c.` + table + `)`,
			`SELECT COUNT(*) FROM (SELECT * FROM c.` + table + ` EXCEPT SELECT * FROM main.` + table + `)`,
		} {
			if n := count(t, db, q); n != 0 {
				t.Errorf("%s differs between the dump and corpus builds: %d rows", table, n)
			}
		}
	}
}

// A corpus build names the dump it came from and carries its licence, not
// the fixture's "no upstream data".
func TestCorpusBuildKeepsTheDumpProvenance(t *testing.T) {
	fromDump, corpus := exportFixture(t)
	fromCorpus := filepath.Join(t.TempDir(), "from-corpus.db")
	if err := run(config{corpus: corpus, out: fromCorpus, minWords: 1}); err != nil {
		t.Fatalf("run from corpus: %v", err)
	}

	meta := func(path, key string) string {
		var v string
		if err := openOut(t, path).QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v); err != nil {
			t.Fatalf("meta %s in %s: %v", key, path, err)
		}
		return v
	}
	for _, key := range []string{"source_url", "source_license", "attribution", "source_sha256", "source_pages", "source_fetched_at"} {
		if got, want := meta(fromCorpus, key), meta(fromDump, key); got != want {
			t.Errorf("meta %s = %q, want the dump build's %q", key, got, want)
		}
	}
	if got := meta(fromCorpus, "source_table"); got != "corpus:dictionary.txt" {
		t.Errorf("source_table = %q, want the corpus named", got)
	}
}

// Sorted, so a monthly refresh diffs as the words that changed rather than a
// reshuffle.
func TestCorpusIsSorted(t *testing.T) {
	_, corpus := exportFixture(t)
	raw, err := os.ReadFile(corpus)
	if err != nil {
		t.Fatal(err)
	}
	var prev string
	for line := range strings.Lines(string(raw)) {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		word, _, _ := strings.Cut(strings.TrimRight(line, "\n"), "\t")
		if word <= prev {
			t.Fatalf("%q follows %q", word, prev)
		}
		prev = word
	}
}

// A plain word list is not a corpus: without the provenance header the build
// would stamp a licence and a hash it cannot vouch for.
func TestCorpusBuildRequiresTheProvenanceHeader(t *testing.T) {
	list := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(list, []byte("học sinh\tdanh từ|người đi học\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := run(config{corpus: list, out: filepath.Join(t.TempDir(), "noitu.db"), minWords: 1})
	if err == nil || !strings.Contains(err.Error(), "not an exported corpus") {
		t.Fatalf("err = %v, want the missing header named", err)
	}
}

func TestExportNeedsADump(t *testing.T) {
	err := run(config{words: "x.txt", export: "out.txt"})
	if err == nil || !strings.Contains(err.Error(), "--export needs --dump") {
		t.Fatalf("err = %v, want --export refused without --dump", err)
	}
}

// A gloss the line format cannot carry fails the export instead of reading
// back as a different meaning.
func TestCorpusRefusesAnUnwritableGloss(t *testing.T) {
	words := map[string]entry{"học sinh": {word: "học sinh", first: "học", last: "sinh", syllables: 2}}
	for _, gloss := range []string{"one\ttwo", "line\nbreak", " padded"} {
		meanings := map[string][]sense{"học sinh": {{pos: "danh từ", gloss: gloss}}}
		path := filepath.Join(t.TempDir(), "dictionary.txt")
		if err := writeCorpus(path, words, meanings, dumpProvenance{sha256: "x", pages: 1, fetchedAt: time.Now()}); err == nil {
			t.Errorf("gloss %q was exported", gloss)
		}
		if _, err := os.Stat(path); err == nil {
			t.Errorf("a failed export of %q left a corpus behind", gloss)
		}
	}
}
