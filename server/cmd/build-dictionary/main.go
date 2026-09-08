// Command build-dictionary derives the game's wordlist and word meanings from
// the Wikimedia dump of Wiktionary tiếng Việt.
//
// The upstream is a ~61 MB bzip2-compressed XML file: every page of the wiki
// with its current wikitext, regenerated monthly. The game needs the
// Vietnamese word forms of at least two syllables, indexed by first and last
// syllable, and the plain text of each word's definitions. This tool performs
// that reduction and records provenance in a meta table — including the
// SHA-256 of the file it read, since the upstream is fetched fresh for every
// build rather than pinned.
//
// The derived database is a modified version of CC BY-SA 4.0 licensed data.
// See data/ATTRIBUTION.md.
//
// Usage:
//
//	go run ./cmd/build-dictionary --dump ../data/viwiktionary-latest-pages-articles.xml.bz2 --out ../data/noitu.db
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
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

// builderVer changes whenever the meta table's contract does, so two databases
// with different provenance rows never claim the same builder.
const builderVer = "5"

// minMeaningCoverage is the share of words a dump build must carry a meaning
// for. The 2026-09-01 dump measured well above it; the floor exists to catch a
// stripper or section scanner that suddenly returns nothing, not to demand
// quality. Fixture builds are exempt: their meanings are hand-written.
const minMeaningCoverage = 0.6

type config struct {
	// dump is the corpus: the Wikimedia pages-articles export.
	dump string
	// words is an alternative source: a plain list, one word per line with an
	// optional tab-separated meaning column, used to build a small fixture
	// database without the upstream download.
	words    string
	out      string
	minWords int
	// minPages is the floor on pages with a Vietnamese section. Distinct from
	// minWords so a scanner that silently misses a dialect is caught before
	// the word floor is.
	minPages int
}

func main() {
	log.SetFlags(0)

	var cfg config
	flag.StringVar(&cfg.dump, "dump", "", "upstream Wikimedia pages-articles.xml.bz2 dump to read")
	flag.StringVar(&cfg.words, "words", "", "read a plain word list instead of the dump (one word per line, optional tab-separated meanings, # comments)")
	flag.StringVar(&cfg.out, "out", "../data/noitu.db", "derived database to write")
	flag.IntVar(&cfg.minWords, "min-words", 30000, "fail if fewer words survive filtering")
	flag.IntVar(&cfg.minPages, "min-pages", 20000, "fail if the dump has fewer pages with a Vietnamese section")
	flag.Parse()

	if err := run(cfg); err != nil {
		log.Fatalf("build-dictionary: %v", err)
	}
}

func run(cfg config) error {
	// Exactly one input. Picking silently between two would let a stray flag
	// ship a corpus nobody meant to build.
	switch {
	case cfg.dump == "" && cfg.words == "":
		return errors.New("no input given: pass --dump (the corpus) or --words (a plain list)")
	case cfg.dump != "" && cfg.words != "":
		return errors.New("--dump and --words are mutually exclusive")
	case cfg.dump != "":
		return runFromDump(cfg)
	default:
		return runFromWordList(cfg)
	}
}

// runFromDump derives the database from the Wikimedia dump, keeping every
// page with a Vietnamese section and recording the hash of the bytes it read.
func runFromDump(cfg config) error {
	if _, err := os.Stat(cfg.dump); err != nil {
		return fmt.Errorf("dump not found at %s — run 'make fetch-dict' first: %w", cfg.dump, err)
	}

	started := time.Now()
	words, meanings, rejects, stats, prov, err := readDump(cfg.dump)
	if err != nil {
		return err
	}
	logDumpStats(stats)
	logRejects(rejects)
	if prov.pages < cfg.minPages {
		return fmt.Errorf("only %d pages have a Vietnamese section, expected at least %d — "+
			"the dump's markup may have changed", prov.pages, cfg.minPages)
	}
	log.Printf("accepted %d distinct words, %d with a meaning, from %s (%d pages, sha256 %s) in %s",
		len(words), len(meanings), cfg.dump, prov.pages, prov.sha256, time.Since(started).Round(time.Second))

	return finish(cfg, words, meanings, dumpSourceSpec(cfg.dump, prov), true)
}

// finish is the tail every input mode shares: the size floor, alias
// generation, the atomic write and the re-read verification. Keeping it in one
// place is what stops a fixture from drifting into a different shape from the
// database production loads. requireCoverage applies the meaning-coverage
// floor, which only a corpus build can be held to.
func finish(cfg config, words map[string]entry, meanings map[string][]sense, src sourceSpec, requireCoverage bool) error {
	if len(words) < cfg.minWords {
		return fmt.Errorf("only %d words survived filtering, expected at least %d — "+
			"the source content may have changed", len(words), cfg.minWords)
	}

	aliases, collisions := buildAliases(words)
	log.Printf("generated %d spelling aliases (%d skipped as ambiguous or already real words)", len(aliases), collisions)

	if err := write(cfg.out, words, meanings, aliases, src); err != nil {
		return err
	}
	if err := verify(cfg.out, cfg.minWords, requireCoverage); err != nil {
		return fmt.Errorf("output failed verification: %w", err)
	}

	log.Printf("wrote %s", cfg.out)
	return nil
}

// runFromWordList derives a database from a plain list of words instead of the
// dump.
//
// It exists so tests and CI have a real dictionary to play against without the
// upstream download. The filtering, alias generation, writing and verification
// below are the same functions the real build uses — only the source of the
// raw strings differs — so a fixture cannot drift into being shaped
// differently from what production loads.
//
// A line is `word`, or `word<TAB>sense<TAB>sense…` where a sense is
// `pos|gloss` or just `gloss`. The pipe never survives the stripper, so it is
// a safe separator for hand-written meanings.
func runFromWordList(cfg config) error {
	raw, err := os.ReadFile(cfg.words)
	if err != nil {
		return fmt.Errorf("read word list: %w", err)
	}

	words := make(map[string]entry)
	meanings := make(map[string][]sense)
	rejects := make(map[rejectReason]int)

	for line := range strings.Lines(string(raw)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cells := strings.Split(line, "\t")
		word, syllables, reason, ok := accept(cells[0])
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
		if senses := parseSenses(cells[1:]); len(senses) > 0 {
			meanings[word] = senses
		}
	}

	logRejects(rejects)
	log.Printf("accepted %d distinct words, %d with a meaning, from %s", len(words), len(meanings), cfg.words)

	// The source spec is what lands in the meta table. Naming the list rather
	// than a table makes it obvious in the output which build produced a given
	// database — and a hand-written list carries no upstream licence, so the
	// fixture must not claim one.
	return finish(cfg, words, meanings, sourceSpec{
		table:       "wordlist:" + filepath.Base(cfg.words),
		license:     "none: hand-written fixture wordlist, no upstream data",
		attribution: "Fixture written by this project; no third-party attribution applies.",
	}, false)
}

// parseSenses reads the tab-separated meaning cells of a fixture line. A cell
// is `pos|gloss` or a bare gloss; empty cells are skipped and the cap applies
// as it does to the dump.
func parseSenses(cells []string) []sense {
	var senses []sense
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			continue
		}
		s := sense{gloss: cell}
		if pos, gloss, ok := strings.Cut(cell, "|"); ok {
			s = sense{pos: strings.TrimSpace(pos), gloss: strings.TrimSpace(gloss)}
		}
		if s.gloss == "" {
			continue
		}
		s.gloss, _ = capGloss(s.gloss)
		if len(senses) < maxSenses {
			senses = append(senses, s)
		}
	}
	return senses
}

// verify re-opens the finished database and re-checks the invariants the game
// depends on. The in-memory checks above can only prove what the builder
// intended; this proves what actually landed on disk.
func verify(path string, minWords int, requireCoverage bool) error {
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
		{"meanings whose word is missing from the words table",
			`SELECT COUNT(*) FROM meanings m LEFT JOIN words w ON w.word = m.word WHERE w.word IS NULL`,
			func(n int) bool { return n == 0 }},
		{"meanings with an empty gloss", `SELECT COUNT(*) FROM meanings WHERE gloss = ''`, func(n int) bool { return n == 0 }},
		{"meanings over the length cap", fmt.Sprintf(`SELECT COUNT(*) FROM meanings WHERE LENGTH(gloss) > %d`, maxGlossRunes),
			func(n int) bool { return n == 0 }},
		{"words with more meanings than the cap",
			fmt.Sprintf(`SELECT COUNT(*) FROM (SELECT word FROM meanings GROUP BY word HAVING COUNT(*) > %d)`, maxSenses),
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

	if requireCoverage {
		var wordCount, withMeaning int
		if err := db.QueryRow(`SELECT COUNT(*) FROM words`).Scan(&wordCount); err != nil {
			return err
		}
		if err := db.QueryRow(`SELECT COUNT(DISTINCT word) FROM meanings`).Scan(&withMeaning); err != nil {
			return err
		}
		if float64(withMeaning) < minMeaningCoverage*float64(wordCount) {
			return fmt.Errorf("only %d of %d words have a meaning, expected at least %.0f%% — "+
				"the dump's definition markup may have changed", withMeaning, wordCount, minMeaningCoverage*100)
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
	// table names the input: "dump:<file>" for the corpus, "wordlist:<file>"
	// for a fixture, so the output says which build produced it.
	table string
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
func write(path string, words map[string]entry, meanings map[string][]sense, aliases map[string]string, src sourceSpec) error {
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

	if err := writeTo(tmp, words, meanings, aliases, src); err != nil {
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

func writeTo(path string, words map[string]entry, meanings map[string][]sense, aliases map[string]string, src sourceSpec) error {
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

-- One row per sense, in page order. pos is the Vietnamese part-of-speech
-- label of the heading the definition sat under, '' when the heading was one
-- the builder does not know. No foreign key pragma: verify() checks the join.
CREATE TABLE meanings (
  word  TEXT NOT NULL,
  ord   INTEGER NOT NULL,
  pos   TEXT NOT NULL,
  gloss TEXT NOT NULL,
  PRIMARY KEY (word, ord)
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

	insertMeaning, err := tx.Prepare(`INSERT INTO meanings (word, ord, pos, gloss) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insertMeaning.Close()
	meaningCount := 0
	for word, senses := range meanings {
		if _, isWord := words[word]; !isWord {
			return fmt.Errorf("meaning for %q, which is not a word", word)
		}
		for ord, s := range senses {
			if s.gloss == "" || utf8.RuneCountInString(s.gloss) > maxGlossRunes {
				return fmt.Errorf("meaning %d of %q is empty or over the cap", ord, word)
			}
			if _, err := insertMeaning.Exec(word, ord, s.pos, s.gloss); err != nil {
				return fmt.Errorf("insert meaning %d of %q: %w", ord, word, err)
			}
			meaningCount++
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
		{"meaning_count", fmt.Sprint(meaningCount)},
		{"words_with_meaning", fmt.Sprint(len(meanings))},
		{"source_table", src.table},
	}
	meta = append(meta, src.extra...)
	for _, kv := range meta {
		if _, err := insertMeta.Exec(kv[0], kv[1]); err != nil {
			return err
		}
	}

	return tx.Commit()
}
