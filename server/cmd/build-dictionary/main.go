// Command build-dictionary derives the game's wordlist from the upstream
// minhqnd/dictionary SQLite database.
//
// The upstream database is ~179 MB and covers 1,500+ language pairs with full
// definitions. The game needs none of that: only Vietnamese word forms of at
// least two syllables, indexed by first and last syllable. This tool performs
// that reduction and records provenance in a meta table.
//
// The derived database is a modified version of CC BY-SA 4.0 licensed data.
// See data/ATTRIBUTION.md.
//
// Usage:
//
//	go run ./cmd/build-dictionary --in ../data/dictionary.db --out ../data/noitu.db
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	sourceURL     = "https://github.com/minhqnd/dictionary/releases/download/v2.0.0/dictionary.db"
	sourceLicense = "CC BY-SA 4.0 (https://creativecommons.org/licenses/by-sa/4.0/)"
	builderVer    = "1"
)

type config struct {
	in           string
	out          string
	maxSyllables int
	minWords     int
	table        string
	wordCol      string
	langCol      string
	lang         string
}

func main() {
	log.SetFlags(0)

	var cfg config
	flag.StringVar(&cfg.in, "in", "../data/dictionary.db", "upstream dictionary.db to read")
	flag.StringVar(&cfg.out, "out", "../data/noitu.db", "derived database to write")
	flag.IntVar(&cfg.maxSyllables, "max-syllables", 0, "reject words longer than this (0 = no limit)")
	flag.IntVar(&cfg.minWords, "min-words", 40000, "fail if fewer words survive filtering")
	flag.StringVar(&cfg.table, "table", "", "source table (default: auto-detect)")
	flag.StringVar(&cfg.wordCol, "word-col", "", "source word column (default: auto-detect)")
	flag.StringVar(&cfg.langCol, "lang-col", "", "source language column (default: auto-detect)")
	flag.StringVar(&cfg.lang, "lang", "vi", "language code to keep")
	flag.Parse()

	if err := run(cfg); err != nil {
		log.Fatalf("build-dictionary: %v", err)
	}
}

func run(cfg config) error {
	if _, err := os.Stat(cfg.in); err != nil {
		return fmt.Errorf("source database not found at %s — run 'make fetch-dict' first: %w", cfg.in, err)
	}

	src, err := sql.Open("sqlite", "file:"+cfg.in+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer src.Close()

	source, err := resolveSource(src, cfg)
	if err != nil {
		return err
	}
	log.Printf("source: %s.%s filtered by %s = %q", source.table, source.wordCol, source.langCol, cfg.lang)

	words, rejects, err := extract(src, source, cfg)
	if err != nil {
		return err
	}
	logRejects(rejects)
	log.Printf("accepted %d distinct words", len(words))

	if len(words) < cfg.minWords {
		return fmt.Errorf("only %d words survived filtering, expected at least %d — "+
			"the source schema or content may have changed", len(words), cfg.minWords)
	}

	aliases, collisions := buildAliases(words)
	log.Printf("generated %d spelling aliases (%d skipped as ambiguous or already real words)", len(aliases), collisions)

	if err := write(cfg.out, words, aliases, source); err != nil {
		return err
	}
	if err := verify(cfg.out, cfg.minWords); err != nil {
		return fmt.Errorf("output failed verification: %w", err)
	}

	log.Printf("wrote %s", cfg.out)
	return nil
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

type sourceSpec struct {
	table        string
	wordCol      string
	langCol      string
	maxSyllables int
}

// quoteIdent renders a SQLite identifier. Go's %q escapes an embedded quote as
// \" but SQLite requires it doubled, so fmt.Sprintf("%q") is not correct here.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// validateSource confirms operator-supplied identifiers actually exist.
//
// SQLite accepts a double-quoted string that matches no column as a string
// literal rather than erroring, so a typo in --word-col silently yields one
// row per distinct value of that literal. Checking up front turns a confusing
// near-empty build into a clear message.
func validateSource(db *sql.DB, spec sourceSpec) error {
	tables, err := listTables(db)
	if err != nil {
		return err
	}
	if !contains(tables, spec.table) {
		return fmt.Errorf("table %q not found; available: %s", spec.table, strings.Join(tables, ", "))
	}

	cols, err := listColumns(db, spec.table)
	if err != nil {
		return fmt.Errorf("read columns of %q: %w", spec.table, err)
	}
	for _, c := range []struct{ role, name string }{{"--word-col", spec.wordCol}, {"--lang-col", spec.langCol}} {
		if !contains(cols, c.name) {
			return fmt.Errorf("%s %q not found in table %q; available: %s",
				c.role, c.name, spec.table, strings.Join(cols, ", "))
		}
	}

	return nil
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// resolveSource finds the table holding word forms. The upstream schema is not
// contractual — it is someone else's release artifact — so rather than hardcode
// it, look for a table carrying both a word-like and a language-like column.
// Explicit flags override detection entirely.
func resolveSource(db *sql.DB, cfg config) (sourceSpec, error) {
	given := 0
	for _, f := range []string{cfg.table, cfg.wordCol, cfg.langCol} {
		if f != "" {
			given++
		}
	}
	switch {
	case given == 3:
		spec := sourceSpec{table: cfg.table, wordCol: cfg.wordCol, langCol: cfg.langCol, maxSyllables: cfg.maxSyllables}
		if err := validateSource(db, spec); err != nil {
			return sourceSpec{}, err
		}
		return spec, nil
	case given > 0:
		return sourceSpec{}, fmt.Errorf(
			"--table, --word-col and --lang-col must be given together (got %d of 3); "+
				"omit all three to auto-detect", given)
	}

	tables, err := listTables(db)
	if err != nil {
		return sourceSpec{}, err
	}

	wordNames := []string{"word", "term", "headword", "text", "lemma", "entry"}
	langNames := []string{"lang_code", "language", "lang", "lang_name"}

	for _, t := range tables {
		cols, err := listColumns(db, t)
		if err != nil {
			log.Printf("warning: could not read columns of %q: %v", t, err)
			continue
		}
		wordCol := pickColumn(cols, wordNames)
		langCol := pickColumn(cols, langNames)
		if wordCol != "" && langCol != "" {
			return sourceSpec{table: t, wordCol: wordCol, langCol: langCol, maxSyllables: cfg.maxSyllables}, nil
		}
	}

	// Detection failed. Dump the schema so the fix is a single flag away rather
	// than a debugging session.
	var b strings.Builder
	b.WriteString("could not auto-detect the source table.\n")
	b.WriteString("Pass --table, --word-col and --lang-col explicitly. Schema found:\n")
	for _, t := range tables {
		cols, err := listColumns(db, t)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "  %s(%s)\n", t, strings.Join(cols, ", "))
	}
	return sourceSpec{}, errors.New(b.String())
}

func listTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func listColumns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", quoteIdent(table)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var (
			cid         int
			name, ctype string
			notNull, pk int
			dflt        sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func pickColumn(cols []string, candidates []string) string {
	for _, want := range candidates {
		for _, c := range cols {
			if strings.EqualFold(c, want) {
				return c
			}
		}
	}
	return ""
}

// extract streams every candidate word past the filters. Results are deduped by
// normalized form, since the source lists a word once per sense.
func extract(db *sql.DB, src sourceSpec, cfg config) (map[string]entry, map[rejectReason]int, error) {
	query := fmt.Sprintf("SELECT DISTINCT %s FROM %s WHERE %s = ?",
		quoteIdent(src.wordCol), quoteIdent(src.table), quoteIdent(src.langCol))
	rows, err := db.Query(query, cfg.lang)
	if err != nil {
		return nil, nil, fmt.Errorf("query source (%s): %w", query, err)
	}
	defer rows.Close()

	words := make(map[string]entry)
	rejects := make(map[rejectReason]int)

	for rows.Next() {
		var raw sql.NullString
		if err := rows.Scan(&raw); err != nil {
			return nil, nil, err
		}
		if !raw.Valid {
			rejects[rejectEmpty]++
			continue
		}

		word, syllables, reason, ok := accept(raw.String, cfg.maxSyllables)
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

	return words, rejects, rows.Err()
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
		{"source_url", sourceURL},
		{"source_license", sourceLicense},
		{"attribution", "See data/ATTRIBUTION.md for required attribution and the list of modifications."},
		{"built_at", time.Now().UTC().Format(time.RFC3339)},
		{"builder_version", builderVer},
		{"word_count", fmt.Sprint(len(words))},
		{"alias_count", fmt.Sprint(len(aliases))},
		{"source_table", src.table},
		{"source_word_column", src.wordCol},
		{"source_lang_column", src.langCol},
		{"max_syllables", fmt.Sprint(src.maxSyllables)},
	}
	for _, kv := range meta {
		if _, err := insertMeta.Exec(kv[0], kv[1]); err != nil {
			return err
		}
	}

	return tx.Commit()
}
