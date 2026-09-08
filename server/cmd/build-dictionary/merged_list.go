package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The upstream is undertheseanlp/dictionary: one JSONL file listing every word
// of three merged Vietnamese wordlists together with which of them contain it.
//
// Pinned by commit rather than branch so the URL and the checksum in the
// Makefile can never disagree.
const (
	mergedSourceCommit = "2c078cfc373b06e2980d324ce1d7bd13740c3319"
	mergedSourceURL    = "https://raw.githubusercontent.com/undertheseanlp/dictionary/" + mergedSourceCommit + "/dictionary/words.txt"
)

// knownSources are the three wordlists merged upstream. Only names in this
// list may be passed to --sources; a typo must fail loudly rather than quietly
// select nothing.
var knownSources = []string{"hongocduc", "tudientv", "wiktionary"}

// mergedRow is one line of the upstream file.
type mergedRow struct {
	Text   string   `json:"text"`
	Source []string `json:"source"`
}

// parseSources turns the --sources flag into the set of upstream wordlists a
// word may come from.
func parseSources(list string) (map[string]bool, error) {
	allowed := make(map[string]bool)
	for _, name := range strings.Split(list, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !slices.Contains(knownSources, name) {
			return nil, fmt.Errorf("unknown source %q in --sources; known: %s", name, strings.Join(knownSources, ", "))
		}
		allowed[name] = true
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("--sources selects nothing; known: %s", strings.Join(knownSources, ", "))
	}
	return allowed, nil
}

// readMergedList reads the upstream file and returns the words that belong in
// the game dictionary.
//
// A row is considered only if one of its sources is allowed; rows from other
// wordlists are never read. Capitalization is not a filter: "Hà Nội" and
// "hà nội" are the same word to the game and both land as the lowercase form.
// Rows then pass through the same accept() every other input mode uses.
func readMergedList(path string, allowed map[string]bool, maxSyllables int) (map[string]entry, map[rejectReason]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read merged list: %w", err)
	}
	defer f.Close()

	words := make(map[string]entry)
	rejects := make(map[rejectReason]int)

	scanner := bufio.NewScanner(f)
	// The longest upstream line is well under a kilobyte, but a scanner that
	// hits its limit stops with an error rather than truncating, and a generous
	// buffer keeps that error from ever being the reason a build fails.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var row mergedRow
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, nil, fmt.Errorf("%s:%d: malformed line: %w", path, lineNo, err)
		}
		if !anyAllowed(row.Source, allowed) {
			continue
		}

		word, syllables, reason, ok := accept(row.Text, maxSyllables)
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
	if err := scanner.Err(); err != nil {
		// lineNo counts lines already returned; the failure is on the next one.
		return nil, nil, fmt.Errorf("%s:%d: %w", path, lineNo+1, err)
	}

	return words, rejects, nil
}

func anyAllowed(sources []string, allowed map[string]bool) bool {
	for _, s := range sources {
		if allowed[s] {
			return true
		}
	}
	return false
}

// mergedProvenance describes a merged-list build for the meta table: which
// upstream wordlists were kept and which were excluded. knownSources is
// already sorted, so kept and excluded come out sorted too.
func mergedProvenance(path string, allowed map[string]bool, maxSyllables int) sourceSpec {
	var kept, excluded []string
	for _, name := range knownSources {
		if allowed[name] {
			kept = append(kept, name)
		} else {
			excluded = append(excluded, name)
		}
	}

	return sourceSpec{
		table:        "merged:" + filepath.Base(path),
		url:          mergedSourceURL,
		license:      "CC BY-SA 3.0 (https://creativecommons.org/licenses/by-sa/3.0/)",
		attribution:  "See data/ATTRIBUTION.md for required attribution and the list of modifications.",
		maxSyllables: maxSyllables,
		extra: [][2]string{
			{"source_commit", mergedSourceCommit},
			{"sources_kept", strings.Join(kept, ",")},
			{"sources_excluded", strings.Join(excluded, ",")},
		},
	}
}
