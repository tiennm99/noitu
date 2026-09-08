package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The upstream is kaikki.org's wiktextract export of Wiktionary tiếng Việt:
// one JSON object per entry, refreshed from the monthly Wikimedia dump about
// once a week. The file is fetched fresh for every build and is not pinned —
// there is no archived snapshot to pin to — so the builder records the SHA-256
// of the bytes it actually read, and that hash is what identifies a build.
//
// The URL stays percent-encoded: the path has a space in it, and both make
// and sh would otherwise split it.
const kaikkiSourceURL = "https://kaikki.org/viwiktionary/Ti%E1%BA%BFng%20Vi%E1%BB%87t/kaikki.org-dictionary-Ti%E1%BA%BFngVi%E1%BB%87t.jsonl"

// kaikkiRow is the part of a wiktextract entry the game cares about. Every
// other field — senses, translations, categories — is skipped by the decoder.
type kaikkiRow struct {
	Word     string `json:"word"`
	Pos      string `json:"pos"`
	LangCode string `json:"lang_code"`
}

// kaikkiProvenance identifies the bytes a build was made from.
type kaikkiProvenance struct {
	sha256    string
	rows      int
	fetchedAt time.Time
}

// readKaikkiList streams the export, keeps Vietnamese-language entries and
// hands their word forms to accept(). Part of speech is tallied for the build
// log but never filters: the owner's decision that capitalization removes no
// word applies equally to the "name" tag.
//
// Lines are read with bufio.Reader rather than bufio.Scanner because a row
// carries every sense and translation of its entry and can run to hundreds of
// kilobytes; a scanner's fixed cap would be a guess that eventually fails.
func readKaikkiList(path string) (map[string]entry, map[rejectReason]int, map[string]int, kaikkiProvenance, error) {
	var prov kaikkiProvenance

	f, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, prov, fmt.Errorf("read kaikki export: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		// A provenance row must be right or absent, never a plausible zero.
		return nil, nil, nil, prov, fmt.Errorf("stat kaikki export: %w", err)
	}
	prov.fetchedAt = info.ModTime().UTC()

	hash := sha256.New()
	reader := bufio.NewReaderSize(io.TeeReader(f, hash), 1<<20)

	words := make(map[string]entry)
	rejects := make(map[rejectReason]int)
	pos := make(map[string]int)

	lineNo := 0
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineNo++
			if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
				// A bare literal such as null would decode into an empty row
				// and be miscounted as a foreign-language entry; only objects
				// are entries.
				if trimmed[0] != '{' {
					return nil, nil, nil, prov, fmt.Errorf("%s:%d: malformed line: not a JSON object", path, lineNo)
				}
				var row kaikkiRow
				if err := json.Unmarshal(trimmed, &row); err != nil {
					return nil, nil, nil, prov, fmt.Errorf("%s:%d: malformed line: %w", path, lineNo, err)
				}
				prov.rows++
				if row.LangCode != "vi" {
					rejects[rejectNotVietnamese]++
				} else {
					pos[row.Pos]++
					if word, syllables, reason, ok := accept(row.Word); !ok {
						rejects[reason]++
					} else {
						words[word] = entry{
							word:      word,
							first:     syllables[0],
							last:      syllables[len(syllables)-1],
							syllables: len(syllables),
						}
					}
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			// The failure is on the line being read: the one just counted if
			// a partial line came back with the error, otherwise the next.
			failed := lineNo + 1
			if len(line) > 0 {
				failed = lineNo
			}
			return nil, nil, nil, prov, fmt.Errorf("%s:%d: %w", path, failed, err)
		}
	}
	prov.sha256 = hex.EncodeToString(hash.Sum(nil))

	return words, rejects, pos, prov, nil
}

// formatPosTally renders the part-of-speech counts on one log line, largest
// first, so the build log says what kind of entries the export held.
func formatPosTally(pos map[string]int) string {
	type kv struct {
		name  string
		count int
	}
	tally := make([]kv, 0, len(pos))
	for name, count := range pos {
		if name == "" {
			name = "(none)"
		}
		tally = append(tally, kv{name, count})
	}
	sort.Slice(tally, func(i, j int) bool {
		if tally[i].count != tally[j].count {
			return tally[i].count > tally[j].count
		}
		return tally[i].name < tally[j].name
	})
	parts := make([]string, len(tally))
	for i, t := range tally {
		parts[i] = fmt.Sprintf("%s %d", t.name, t.count)
	}
	return strings.Join(parts, ", ")
}

// kaikkiSourceSpec describes a kaikki build for the meta table. With no commit
// or checksum pinned upstream, the hash and row count of the bytes read are the
// provenance.
func kaikkiSourceSpec(path string, prov kaikkiProvenance) sourceSpec {
	return sourceSpec{
		table:       "kaikki:" + filepath.Base(path),
		url:         kaikkiSourceURL,
		license:     "CC BY-SA 4.0 (https://creativecommons.org/licenses/by-sa/4.0/)",
		attribution: "See data/ATTRIBUTION.md for required attribution and the list of modifications.",
		extra: [][2]string{
			{"source_sha256", prov.sha256},
			{"source_rows", fmt.Sprint(prov.rows)},
			{"source_fetched_at", prov.fetchedAt.Format(time.RFC3339)},
		},
	}
}
