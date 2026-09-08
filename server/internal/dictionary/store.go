// Package dictionary provides read-only lookups over the derived game
// wordlist built by cmd/build-dictionary.
//
// Everything above this layer treats words as opaque strings: the engine asks
// whether a word exists, which words start with a syllable, and how many
// continuations a syllable has. Nothing else needs to know the data was SQLite.
//
// The whole dictionary is loaded into memory at Open and the database is then
// closed. It is a read-only artifact of about 3 MB on disk and ~7 MB in maps,
// while a SQLite round-trip benchmarked at 55us against 13ns for a map hit.
// That gap matters twice: every submitted word is a lookup, and the hard bot's
// search explores hundreds of candidate moves inside a 150ms budget. Holding it
// in memory also removes the connection pool, the prepared statements and the
// per-query tail latency.
package dictionary

import (
	"database/sql"
	"errors"
	"fmt"
	"iter"
	"net/url"
	"os"
	"sort"
	"strconv"

	"math/rand/v2"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a syllable has no entry in the dictionary.
var ErrNotFound = errors.New("dictionary: syllable not found")

// wordInfo holds the two syllables the chain rule needs. Both ends are kept:
// canonicalization can move either one, so the engine must never re-derive
// them from what the player typed.
type wordInfo struct {
	first string
	last  string
}

// Store answers word and syllable queries against the derived dictionary.
//
// Every field is written once during Open and only read afterwards, and no
// method hands out a mutable reference to internal state, so a Store is safe
// for unsynchronized concurrent use by any number of goroutines.
type Store struct {
	words   map[string]wordInfo
	aliases map[string]string
	byFirst map[string][]string
	// outDegree covers every syllable, including those that start no word.
	outDegree map[string]int
	// openers holds words whose last syllable has at least one continuation,
	// sorted by that count descending so an eligible set is always a prefix.
	openers []opener

	license string
}

type opener struct {
	word          string
	lastOutDegree int
}

// Open loads the dictionary at path into memory.
//
// The file is opened read-only and closed again before Open returns: this
// process never writes to it and never reads it again.
func Open(path string) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("dictionary not found at %s — run 'make fetch-dict && make dict' first: %w", path, err)
	}

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open dictionary: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("open dictionary: %w", err)
	}

	s := &Store{
		words:     make(map[string]wordInfo),
		aliases:   make(map[string]string),
		byFirst:   make(map[string][]string),
		outDegree: make(map[string]int),
	}

	// Reading meta first also rejects an unrelated database before any bulk
	// loading happens.
	declaredWords, err := s.loadMeta(db)
	if err != nil {
		return nil, err
	}
	// Order matters: loadWords reads outDegree to decide which words are
	// eligible openers, so the syllable table must already be in memory.
	if err := s.loadSyllables(db); err != nil {
		return nil, err
	}
	if err := s.loadWords(db); err != nil {
		return nil, err
	}
	if err := s.loadAliases(db); err != nil {
		return nil, err
	}
	if err := s.validate(declaredWords); err != nil {
		return nil, err
	}

	sort.SliceStable(s.openers, func(i, j int) bool {
		return s.openers[i].lastOutDegree > s.openers[j].lastOutDegree
	})

	return s, nil
}

// dsn builds the SQLite URI. The path must be escaped: SQLite reads '#' as a
// URI fragment delimiter, so a bare path containing one silently opens a
// different (usually nonexistent) file and reports a confusing schema error.
func dsn(path string) string {
	u := url.URL{Scheme: "file", Opaque: (&url.URL{Path: path}).EscapedPath(), RawQuery: "mode=ro"}
	return u.String()
}

func (s *Store) loadMeta(db *sql.DB) (declaredWords int, err error) {
	// The data is CC BY-SA 3.0 and its provenance travels with it.
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'source_license'`).Scan(&s.license); err != nil {
		return 0, fmt.Errorf("read dictionary metadata (is this a noitu.db?): %w", err)
	}

	var raw string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'word_count'`).Scan(&raw); err != nil {
		return 0, fmt.Errorf("read dictionary word_count: %w", err)
	}
	declaredWords, err = strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("dictionary word_count %q is not a number: %w", raw, err)
	}

	return declaredWords, nil
}

func (s *Store) loadSyllables(db *sql.DB) error {
	rows, err := db.Query(`SELECT syllable, out_degree FROM syllables`)
	if err != nil {
		return fmt.Errorf("load syllables: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var syllable string
		var degree int
		if err := rows.Scan(&syllable, &degree); err != nil {
			return fmt.Errorf("scan syllable: %w", err)
		}
		s.outDegree[syllable] = degree
	}

	return rows.Err()
}

func (s *Store) loadWords(db *sql.DB) error {
	// Sorted here so byFirst lists come out in a stable order without a second
	// pass; a deterministic order keeps bot behaviour reproducible.
	rows, err := db.Query(`SELECT word, first, last FROM words ORDER BY word`)
	if err != nil {
		return fmt.Errorf("load words: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var word, first, last string
		if err := rows.Scan(&word, &first, &last); err != nil {
			return fmt.Errorf("scan word: %w", err)
		}
		s.words[word] = wordInfo{first: first, last: last}
		s.byFirst[first] = append(s.byFirst[first], word)
		if degree := s.outDegree[last]; degree > 0 {
			s.openers = append(s.openers, opener{word: word, lastOutDegree: degree})
		}
	}

	return rows.Err()
}

func (s *Store) loadAliases(db *sql.DB) error {
	rows, err := db.Query(`SELECT variant, canonical FROM aliases`)
	if err != nil {
		return fmt.Errorf("load aliases: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var variant, canonical string
		if err := rows.Scan(&variant, &canonical); err != nil {
			return fmt.Errorf("scan alias: %w", err)
		}
		s.aliases[variant] = canonical
	}

	return rows.Err()
}

// validate rejects a structurally valid but wrong dictionary.
//
// A truncated or empty database has the right schema and opens cleanly, and
// the server would then start, reject every word a player types, and fail
// every room creation. Checking the loaded rows against what the builder
// recorded turns that into a startup failure.
func (s *Store) validate(declaredWords int) error {
	if len(s.words) != declaredWords {
		return fmt.Errorf("dictionary is incomplete: metadata declares %d words, loaded %d",
			declaredWords, len(s.words))
	}
	if len(s.words) == 0 {
		return errors.New("dictionary contains no words")
	}

	// A stale syllables table would tell the bot a syllable has continuations
	// that WordsStartingWith cannot supply.
	for syllable, degree := range s.outDegree {
		if actual := len(s.byFirst[syllable]); actual != degree {
			return fmt.Errorf("dictionary is inconsistent: syllable %q claims out-degree %d but %d words start with it",
				syllable, degree, actual)
		}
	}

	for variant, canonical := range s.aliases {
		if _, ok := s.words[canonical]; !ok {
			return fmt.Errorf("dictionary is inconsistent: alias %q points at missing word %q", variant, canonical)
		}
	}

	return nil
}

// WordCount reports how many playable words the dictionary holds.
func (s *Store) WordCount() int { return len(s.words) }

// AliasCount reports how many alternative spellings are accepted.
func (s *Store) AliasCount() int { return len(s.aliases) }

// License reports the licence the dictionary data is distributed under.
// Callers are expected to state it at startup.
func (s *Store) License() string { return s.license }

// Resolve maps a normalized word to its canonical dictionary form.
//
// A player may type an accepted alternative spelling ("pháp lí" for "pháp lý").
// Callers must use the returned canonical form for everything that follows —
// the chain link, the used-word set, and what is displayed.
//
// Canonicalization can move EITHER end of the word. Of the aliases in the
// shipped dictionary about half differ in the last syllable and more than a
// third in the first ("sỹ hai" resolves to "sĩ hai"). An engine that checked
// the chain against the syllables the player typed would reject legal moves,
// so use FirstSyllable and LastSyllable on the canonical instead.
func (s *Store) Resolve(word string) (string, bool) {
	if _, ok := s.words[word]; ok {
		return word, true
	}
	if canonical, ok := s.aliases[word]; ok {
		return canonical, true
	}
	return "", false
}

// FirstSyllable returns the syllable a canonical word begins with: the syllable
// it must link from. Reports false for an alias, which is not a playable entry.
func (s *Store) FirstSyllable(word string) (string, bool) {
	info, ok := s.words[word]
	return info.first, ok
}

// LastSyllable returns the syllable a canonical word ends on: the syllable the
// next word must start from. Reports false for an alias.
func (s *Store) LastSyllable(word string) (string, bool) {
	info, ok := s.words[word]
	return info.last, ok
}

// WordsStartingWith iterates every playable word whose first syllable is the
// given one, in a stable order. Aliases are never included: they are accepted
// spellings, not entries a bot may play.
//
// An iterator rather than a slice, because the backing slice belongs to the
// Store. Returning it directly let a caller sort, shuffle or append into
// dictionary state — a data race across rooms, and one that silently destroyed
// the ordering guarantee above.
func (s *Store) WordsStartingWith(syllable string) iter.Seq[string] {
	words := s.byFirst[syllable]
	return func(yield func(string) bool) {
		for _, w := range words {
			if !yield(w) {
				return
			}
		}
	}
}

// OutDegree reports how many words start with the given syllable.
//
// Zero means a dead end: whoever is handed this syllable has no legal move.
// A syllable the dictionary has never seen returns ErrNotFound, which is a
// different situation from a known dead end.
func (s *Store) OutDegree(syllable string) (int, error) {
	degree, ok := s.outDegree[syllable]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrNotFound, syllable)
	}
	return degree, nil
}

// RandomOpeningWord picks a word to start a game with.
//
// minOutDegree guards against opening on a word whose last syllable has too few
// continuations, which would end the game almost immediately.
//
// openers is sorted by continuation count descending, so the eligible set is a
// prefix and the pick costs a binary search rather than a scan and a 720 KB
// allocation per room.
func (s *Store) RandomOpeningWord(minOutDegree int) (string, error) {
	n := sort.Search(len(s.openers), func(i int) bool {
		return s.openers[i].lastOutDegree < minOutDegree
	})
	if n == 0 {
		return "", fmt.Errorf("no word has a last syllable with at least %d continuations", minOutDegree)
	}

	return s.openers[rand.IntN(n)].word, nil
}
