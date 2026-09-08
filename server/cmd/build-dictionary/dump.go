package main

import (
	"bufio"
	"compress/bzip2"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The upstream is the Wikimedia dump of Wiktionary tiếng Việt: every page's
// current wikitext, as one bzip2-compressed XML file regenerated monthly.
// `latest/` is a rolling pointer, fetched fresh for every build and not
// pinned, so the builder records the SHA-256 of the bytes it actually read and
// that hash is what identifies a build. Dated directories exist should
// reproducibility ever be wanted.
const dumpSourceURL = "https://dumps.wikimedia.org/viwiktionary/latest/viwiktionary-latest-pages-articles.xml.bz2"

// dumpPage is the part of a <page> element the builder reads. Everything
// else — contributor, timestamp, sha1 — is skipped by the decoder.
type dumpPage struct {
	Title    string `xml:"title"`
	Ns       int    `xml:"ns"`
	Redirect *struct {
		Title string `xml:"title,attr"`
	} `xml:"redirect"`
	Revisions []struct {
		Text string `xml:"text"`
	} `xml:"revision"`
}

// dumpProvenance identifies the bytes a build was made from.
type dumpProvenance struct {
	sha256 string
	// pages is the number of pages with a Vietnamese section, redirects
	// excluded: the count of entries the corpus was derived from.
	pages     int
	fetchedAt time.Time
}

// dumpStats is what the build log reports about the dump beyond the reject
// tally: enough to see a month where a dialect vanished or a stripper rule
// started dropping everything.
type dumpStats struct {
	pages        int
	ns0          int
	redirects    int
	noVietnamese int
	legacy       int
	newDialect   int
	bothDialects int
	merged       int // pages whose title normalized to a word already seen
	// enders counts the {{-code-}} that closed each legacy section. Language
	// codes are expected here; a heading code is one the maps are missing.
	enders  map[string]int
	section *sectionStats
}

// readDump streams the dump once: hashes the compressed bytes, decodes one
// page at a time, hands every Vietnamese-section title to accept() and every
// definition line to the stripper.
func readDump(path string) (map[string]entry, map[string][]sense, map[rejectReason]int, *dumpStats, dumpProvenance, error) {
	var prov dumpProvenance
	stats := &dumpStats{section: newSectionStats(), enders: make(map[string]int)}

	f, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, stats, prov, fmt.Errorf("read dump: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		// A provenance row must be right or absent, never a plausible zero.
		return nil, nil, nil, stats, prov, fmt.Errorf("stat dump: %w", err)
	}
	prov.fetchedAt = info.ModTime().UTC()

	hash := sha256.New()
	compressed := bufio.NewReaderSize(io.TeeReader(f, hash), 1<<20)
	if magic, err := compressed.Peek(3); err != nil || string(magic) != "BZh" {
		return nil, nil, nil, stats, prov, fmt.Errorf("%s is not a bzip2 file (expected a BZh header)", path)
	}
	dec := xml.NewDecoder(bzip2.NewReader(compressed))

	words := make(map[string]entry)
	meanings := make(map[string][]sense)
	rejects := make(map[rejectReason]int)
	lastTitle := ""

	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, nil, nil, stats, prov, dumpError(path, lastTitle, dec.InputOffset(), err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "page" {
			continue
		}
		var page dumpPage
		if err := dec.DecodeElement(&page, &start); err != nil {
			return nil, nil, nil, stats, prov, dumpError(path, lastTitle, dec.InputOffset(), err)
		}
		lastTitle = page.Title
		stats.pages++
		if page.Ns != 0 {
			continue
		}
		stats.ns0++
		if page.Redirect != nil {
			// The target page is read on its own and lowercased by accept(),
			// so a case-only redirect adds nothing and any other redirect is
			// an alternative title the wiki itself does not define.
			stats.redirects++
			continue
		}
		if len(page.Revisions) == 0 {
			return nil, nil, nil, stats, prov, fmt.Errorf("%s: page %q has no revision text", path, page.Title)
		}
		text := page.Revisions[len(page.Revisions)-1].Text

		section, dialect, both, ender := vietnameseSection(text)
		if dialect == "" {
			stats.noVietnamese++
			rejects[rejectNotVietnamese]++
			continue
		}
		if both {
			stats.bothDialects++
		}
		if dialect == "legacy" {
			stats.legacy++
		} else {
			stats.newDialect++
		}
		prov.pages++
		if ender != "" {
			stats.enders[ender]++
		}

		word, syllables, reason, ok := accept(page.Title)
		if !ok {
			rejects[reason]++
			continue
		}
		// After accept, so the definition counters describe words that land.
		senses := definitions(section, stats.section)
		if _, seen := words[word]; seen {
			// Two pages whose titles normalize to one word (Việt Nam and
			// việt nam): one entry, senses in page order, one cap.
			stats.merged++
		}
		words[word] = entry{
			word:      word,
			first:     syllables[0],
			last:      syllables[len(syllables)-1],
			syllables: len(syllables),
		}
		if len(senses) > 0 {
			merged := append(meanings[word], senses...)
			if len(merged) > maxSenses {
				merged = merged[:maxSenses]
			}
			meanings[word] = merged
		}
	}

	// The XML decoder stops at the root's close tag; the hash must cover the
	// whole file, trailing bytes included.
	if _, err := io.Copy(io.Discard, compressed); err != nil {
		return nil, nil, nil, stats, prov, fmt.Errorf("%s: %w", path, err)
	}
	prov.sha256 = hex.EncodeToString(hash.Sum(nil))

	return words, meanings, rejects, stats, prov, nil
}

// dumpError names where a stream failed: the last page fully read and the
// decompressed offset, so a truncated download and a malformed page are told
// apart by the message alone.
func dumpError(path, lastTitle string, offset int64, err error) error {
	where := "before the first page"
	if lastTitle != "" {
		where = fmt.Sprintf("after page %q", lastTitle)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(err.Error(), "unexpected EOF") {
		return fmt.Errorf("%s: stream ends %s (decompressed offset %d): truncated download? %w", path, where, offset, err)
	}
	return fmt.Errorf("%s: %s (decompressed offset %d): %w", path, where, offset, err)
}

// logDumpStats writes the build log lines that describe what the dump held.
func logDumpStats(stats *dumpStats) {
	s := stats.section
	logf := log.Printf
	logf("pages %d, in the main namespace %d, redirects skipped %d, without a Vietnamese section %d",
		stats.pages, stats.ns0, stats.redirects, stats.noVietnamese)
	logf("Vietnamese sections: legacy {{-vie-}} %d, new == {{langname|vi}} == %d, pages with both %d, titles merged %d",
		stats.legacy, stats.newDialect, stats.bothDialects, stats.merged)
	logf("parts of speech: %s", formatTally(s.pos, 0))
	if len(s.unmappedPos) > 0 {
		logf("headings without a label: %s", formatTally(s.unmappedPos, 20))
	}
	// Language codes belong here. A heading code in this list is one the maps
	// do not know, and it has been cutting sections short.
	logf("codes that ended a legacy section, commonest: %s", formatTally(stats.enders, 15))
	logf("definitions kept %d (cut at %d characters: %d), dropped as empty after stripping %d",
		s.defsKept, maxGlossRunes, s.defsCut, s.defsEmpty)
	if len(s.dropped) > 0 {
		logf("templates dropped whole, commonest: %s", formatTally(s.dropped, 10))
	}
}

// formatTally renders counts on one log line, largest first, cut to the top
// n entries when n is positive.
func formatTally(counts map[string]int, n int) string {
	type kv struct {
		name  string
		count int
	}
	tally := make([]kv, 0, len(counts))
	for name, count := range counts {
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
	if n > 0 && len(tally) > n {
		tally = tally[:n]
	}
	parts := make([]string, len(tally))
	for i, t := range tally {
		parts[i] = fmt.Sprintf("%s %d", t.name, t.count)
	}
	return strings.Join(parts, ", ")
}

// dumpSourceSpec describes a dump build for the meta table. With nothing
// pinned upstream, the hash and page count of the bytes read are the
// provenance.
func dumpSourceSpec(path string, prov dumpProvenance) sourceSpec {
	return sourceSpec{
		table:       "dump:" + filepath.Base(path),
		url:         dumpSourceURL,
		license:     "CC BY-SA 4.0 (https://creativecommons.org/licenses/by-sa/4.0/)",
		attribution: "See data/ATTRIBUTION.md for required attribution and the list of modifications.",
		extra: [][2]string{
			{"source_sha256", prov.sha256},
			{"source_pages", fmt.Sprint(prov.pages)},
			{"source_fetched_at", prov.fetchedAt.Format(time.RFC3339)},
		},
	}
}
