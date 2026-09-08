// Command build-dictionary derives the game's wordlist from the upstream
// undertheseanlp/dictionary merged wordlist.
//
// The upstream is a 4.8 MB JSONL file: every word of three Vietnamese
// wordlists, tagged with which of them contain it. The game needs only word
// forms of at least two syllables from the wordlists whose license we accept,
// indexed by first and last syllable. This tool performs that reduction and
// records provenance in a meta table.
//
// The derived database is a modified version of CC BY-SA 3.0 licensed data.
// See data/ATTRIBUTION.md.
//
// Usage:
//
//	go run ./cmd/build-dictionary --merged ../data/undertheseanlp-words.jsonl --out ../data/noitu.db
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const builderVer = "2"

type config struct {
	// merged is the corpus: the upstream JSONL wordlist, read for the
	// wordlists named in sources.
	merged  string
	sources string
	// words is an alternative source: a plain list, one word per line, used to
	// build a small fixture database without the upstream download.
	words        string
	out          string
	maxSyllables int
	minWords     int
}

func main() {
	log.SetFlags(0)

	var cfg config
	flag.StringVar(&cfg.merged, "merged", "", "upstream merged JSONL wordlist to read")
	flag.StringVar(&cfg.sources, "sources", "wiktionary", "comma-separated upstream wordlists a word may come from (hongocduc, tudientv, wiktionary)")
	flag.StringVar(&cfg.words, "words", "", "read a plain word list instead of the upstream wordlist (one word per line, # comments)")
	flag.StringVar(&cfg.out, "out", "../data/noitu.db", "derived database to write")
	flag.IntVar(&cfg.maxSyllables, "max-syllables", 0, "reject words longer than this (0 = no limit)")
	flag.IntVar(&cfg.minWords, "min-words", 20000, "fail if fewer words survive filtering")
	flag.Parse()

	if err := run(cfg); err != nil {
		log.Fatalf("build-dictionary: %v", err)
	}
}

func run(cfg config) error {
	// Exactly one input. Picking silently between two would let a stray flag
	// ship a corpus nobody meant to build.
	switch {
	case cfg.merged == "" && cfg.words == "":
		return errors.New("no input given: pass --merged (the corpus) or --words (a plain list)")
	case cfg.merged != "" && cfg.words != "":
		return errors.New("--merged and --words are mutually exclusive")
	case cfg.merged != "":
		return runFromMergedList(cfg)
	default:
		return runFromWordList(cfg)
	}
}

// runFromMergedList derives the database from the upstream merged wordlist,
// keeping only words present in the wordlists named by --sources.
func runFromMergedList(cfg config) error {
	allowed, err := parseSources(cfg.sources)
	if err != nil {
		return err
	}
	if _, err := os.Stat(cfg.merged); err != nil {
		return fmt.Errorf("merged wordlist not found at %s — run 'make fetch-dict' first: %w", cfg.merged, err)
	}

	words, rejects, err := readMergedList(cfg.merged, allowed, cfg.maxSyllables)
	if err != nil {
		return err
	}
	logRejects(rejects)
	log.Printf("accepted %d distinct words from %s (sources: %s)", len(words), cfg.merged, cfg.sources)

	return finish(cfg, words, mergedProvenance(cfg.merged, allowed, cfg.maxSyllables))
}

// finish is the tail every input mode shares: the size floor, alias
// generation, the atomic write and the re-read verification. Keeping it in one
// place is what stops a fixture from drifting into a different shape from the
// database production loads.
func finish(cfg config, words map[string]entry, src sourceSpec) error {
	if len(words) < cfg.minWords {
		return fmt.Errorf("only %d words survived filtering, expected at least %d — "+
			"the source content may have changed", len(words), cfg.minWords)
	}

	aliases, collisions := buildAliases(words)
	log.Printf("generated %d spelling aliases (%d skipped as ambiguous or already real words)", len(aliases), collisions)

	if err := write(cfg.out, words, aliases, src); err != nil {
		return err
	}
	if err := verify(cfg.out, cfg.minWords); err != nil {
		return fmt.Errorf("output failed verification: %w", err)
	}

	log.Printf("wrote %s", cfg.out)
	return nil
}

// runFromWordList derives a database from a plain list of words instead of the
// upstream release.
//
// It exists so tests and CI have a real dictionary to play against without the
// upstream download. The filtering, alias generation, writing and verification
// below are the same functions the real build uses — only the source of the
// raw strings differs — so a fixture cannot drift into being shaped
// differently from what production loads.
func runFromWordList(cfg config) error {
	raw, err := os.ReadFile(cfg.words)
	if err != nil {
		return fmt.Errorf("read word list: %w", err)
	}

	words := make(map[string]entry)
	rejects := make(map[rejectReason]int)

	for line := range strings.Lines(string(raw)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		word, syllables, reason, ok := accept(line, cfg.maxSyllables)
		if !ok {
			rejects[reason]++
			continue
		}
		words[word] = entry{
			word:      word,
			first:     syllables[0],
			last:      syllables[len(syllables)-1],
			syllables: len(syllables),
		}
	}

	logRejects(rejects)
	log.Printf("accepted %d distinct words from %s", len(words), cfg.words)

	// The source spec is what lands in the meta table. Naming the list rather
	// than a table makes it obvious in the output which build produced a given
	// database — and a hand-written list carries no upstream licence, so the
	// fixture must not claim one.
	return finish(cfg, words, sourceSpec{
		table:       "wordlist:" + filepath.Base(cfg.words),
		license:     "none: hand-written fixture wordlist, no upstream data",
		attribution: "Fixture written by this project; no third-party attribution applies.",
	})
}

// verify re-opens the finished database and re-checks the invariants the game
// depends on. The in-memory checks above can only prove what the builder
// intended; this proves what actually landed on disk.
func verify(path string, minWords int) error {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()

	checks := []struct {
		desc  string
		query string
		want  func(int) bool
	}{
		{"words below the minimum", `SELECT COUNT(*) FROM words`, func(n int) bool { return n >= minWords }},
		{"words with fewer than 2 syllables", `SELECT COUNT(*) FROM words WHERE syllables < 2`, func(n int) bool { return n == 0 }},
		{"words with an empty first or last syllable", `SELECT COUNT(*) FROM words WHERE first = '' OR last = ''`, func(n int) bool { return n == 0 }},
		{"aliases pointing at a missing word",
			`SELECT COUNT(*) FROM aliases a LEFT JOIN words w ON w.word = a.canonical WHERE w.word IS NULL`,
			func(n int) bool { return n == 0 }},
		{"aliases that are themselves real words",
			`SELECT COUNT(*) FROM aliases a JOIN words w ON w.word = a.variant`,
			func(n int) bool { return n == 0 }},
		{"words whose first syllable is missing from the syllables table",
			`SELECT COUNT(*) FROM words w LEFT JOIN syllables s ON s.syllable = w.first WHERE s.syllable IS NULL`,
			func(n int) bool { return n == 0 }},
	}

	for _, c := range checks {
		var n int
		if err := db.QueryRow(c.query).Scan(&n); err != nil {
			return fmt.Errorf("check %q: %w", c.desc, err)
		}
		if !c.want(n) {
			return fmt.Errorf("%s: %d", c.desc, n)
		}
	}

	return nil
}

// entry is one accepted word with the parts the game engine indexes on.
type entry struct {
	word      string
	first     string
	last      string
	syllables int
}

// sourceSpec is what the meta table records about where the words came from.
type sourceSpec struct {
	// table names the input: "merged:<file>" for the corpus, "wordlist:<file>"
	// for a fixture, so the output says which build produced it.
	table        string
	maxSyllables int
	// url is the upstream artifact; empty for fixture builds.
	url string
	// license and attribution describe the data's licence obligations. The
	// server logs the licence at startup, so a build must state its own rather
	// than inherit a constant it may not deserve.
	license     string
	attribution string
	// extra holds provenance rows specific to one input mode.
	extra [][2]string
}

func logRejects(rejects map[rejectReason]int) {
	reasons := make([]string, 0, len(rejects))
	for r := range rejects {
		reasons = append(reasons, string(r))
	}
	sort.Strings(reasons)
	for _, r := range reasons {
		log.Printf("  rejected %6d: %s", rejects[rejectReason(r)], r)
	}
}

// buildAliases maps alternative spellings onto canonical words. A variant that
// happens to be a real word in its own right is dropped: accepting it would let
// one word be played under two names, or worse, silently rename it.
func buildAliases(words map[string]entry) (map[string]string, int) {
	aliases := make(map[string]string)
	// poisoned records variants claimed by more than one word. A plain delete
	// is not enough: a third claimant would find the key free and reinsert it,
	// leaving the winner decided by Go's randomized map iteration order.
	poisoned := make(map[string]bool)
	collisions := 0

	for word := range words {
		for _, variant := range variantsFor(word) {
			if poisoned[variant] {
				continue
			}
			if _, isRealWord := words[variant]; isRealWord {
				collisions++
				continue
			}
			if existing, taken := aliases[variant]; taken && existing != word {
				delete(aliases, variant)
				poisoned[variant] = true
				collisions++
				continue
			}
			aliases[variant] = word
		}
	}

	return aliases, collisions
}

// write builds the database beside the target and renames it into place only
// after the transaction commits. Writing in place would mean that a failure
// partway through -- a full disk, an interrupt -- leaves an empty but
// syntactically valid database where a good one used to be, which the server
// would happily open and find no words in.
func write(path string, words map[string]entry, aliases map[string]string, src sourceSpec) error {
	tmp := path + ".tmp"
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale temp file: %w", err)
	}
	// On any error path the half-built file must not survive.
	committed := false
	defer func() {
		if !committed {
			os.Remove(tmp)
		}
	}()

	if err := writeTo(tmp, words, aliases, src); err != nil {
		return err
	}

	// os.Rename replaces the destination atomically on POSIX; on Windows it
	// fails if the target exists, so clear it first.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove existing output: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("move temp database into place: %w", err)
	}
	committed = true

	return nil
}

func writeTo(path string, words map[string]entry, aliases map[string]string, src sourceSpec) error {
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer db.Close()

	schema := `
CREATE TABLE words (
  word      TEXT PRIMARY KEY,
  first     TEXT NOT NULL,
  last      TEXT NOT NULL,
  syllables INTEGER NOT NULL
) WITHOUT ROWID;
CREATE INDEX idx_words_first ON words(first);

CREATE TABLE syllables (
  syllable   TEXT PRIMARY KEY,
  out_degree INTEGER NOT NULL
) WITHOUT ROWID;

CREATE TABLE aliases (
  variant   TEXT PRIMARY KEY,
  canonical TEXT NOT NULL
) WITHOUT ROWID;

CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	insertWord, err := tx.Prepare(`INSERT INTO words (word, first, last, syllables) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insertWord.Close()

	outDegree := make(map[string]int)
	for _, e := range words {
		if _, err := insertWord.Exec(e.word, e.first, e.last, e.syllables); err != nil {
			return fmt.Errorf("insert word %q: %w", e.word, err)
		}
		outDegree[e.first]++
		// A syllable that ends a word but starts none is a dead end; record it
		// with zero so the engine can find it without a second query.
		if _, seen := outDegree[e.last]; !seen {
			outDegree[e.last] = 0
		}
	}

	insertSyllable, err := tx.Prepare(`INSERT INTO syllables (syllable, out_degree) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer insertSyllable.Close()
	for syllable, degree := range outDegree {
		if _, err := insertSyllable.Exec(syllable, degree); err != nil {
			return fmt.Errorf("insert syllable %q: %w", syllable, err)
		}
	}

	insertAlias, err := tx.Prepare(`INSERT INTO aliases (variant, canonical) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer insertAlias.Close()
	for variant, canonical := range aliases {
		if _, err := insertAlias.Exec(variant, canonical); err != nil {
			return fmt.Errorf("insert alias %q: %w", variant, err)
		}
	}

	insertMeta, err := tx.Prepare(`INSERT INTO meta (key, value) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer insertMeta.Close()
	meta := [][2]string{
		{"source_url", src.url},
		{"source_license", src.license},
		{"attribution", src.attribution},
		{"built_at", time.Now().UTC().Format(time.RFC3339)},
		{"builder_version", builderVer},
		{"word_count", fmt.Sprint(len(words))},
		{"alias_count", fmt.Sprint(len(aliases))},
		{"source_table", src.table},
		{"max_syllables", fmt.Sprint(src.maxSyllables)},
	}
	meta = append(meta, src.extra...)
	for _, kv := range meta {
		if _, err := insertMeta.Exec(kv[0], kv[1]); err != nil {
			return err
		}
	}

	return tx.Commit()
}
