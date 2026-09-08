package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureMerged writes a miniature stand-in for the upstream JSONL: the same
// shape, a handful of rows.
func fixtureMerged(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "words.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func defaultMergedLines() []string {
	return []string{
		`{"text": "Hà Nội", "source": ["wiktionary"]}`,   // capitalized upstream — kept, lowercased
		`{"text": "cánh diều", "source": ["hongocduc"]}`, // excluded source — never read
		`{"text": "tủ lạnh", "source": ["tudientv"]}`,    // excluded source — never read
		`{"text": "học sinh", "source": ["hongocduc", "tudientv", "wiktionary"]}`,
		`{"text": "Học sinh", "source": ["wiktionary"]}`, // same word twice — kept once
		`{"text": "pháp", "source": ["wiktionary"]}`,     // single syllable — rejected downstream
		``,
	}
}

func wiktionaryOnly(t *testing.T) map[string]bool {
	t.Helper()
	allowed, err := parseSources("wiktionary")
	if err != nil {
		t.Fatal(err)
	}
	return allowed
}

func TestMergedListKeepsAllowedSourcesOnly(t *testing.T) {
	words, rejects, err := readMergedList(fixtureMerged(t, defaultMergedLines()...), wiktionaryOnly(t), 0)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for w := range words {
		got = append(got, w)
	}
	assertSameStrings(t, got, []string{"hà nội", "học sinh"})

	if n := rejects[rejectTooShort]; n != 1 {
		t.Errorf("too-short rejects = %d, want 1 (pháp)", n)
	}
}

func TestMergedListWidensWithMoreSources(t *testing.T) {
	allowed, err := parseSources("hongocduc,wiktionary")
	if err != nil {
		t.Fatal(err)
	}
	words, _, err := readMergedList(fixtureMerged(t, defaultMergedLines()...), allowed, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hà nội", "học sinh", "cánh diều"} {
		if _, ok := words[want]; !ok {
			t.Errorf("%q missing", want)
		}
	}
	if _, ok := words["tủ lạnh"]; ok {
		t.Error("tudientv-only word leaked into the output")
	}
}

func TestMergedListNamesMalformedLine(t *testing.T) {
	path := fixtureMerged(t,
		`{"text": "học sinh", "source": ["wiktionary"]}`,
		`{"text": "broken"`,
	)
	_, _, err := readMergedList(path, wiktionaryOnly(t), 0)
	if err == nil {
		t.Fatal("malformed line was skipped, want error")
	}
	if !strings.Contains(err.Error(), ":2:") {
		t.Errorf("error does not name line 2: %v", err)
	}
}

func TestParseSourcesRejectsUnknownNames(t *testing.T) {
	for _, bad := range []string{"", "wiktionary,soha", "Wiktionary"} {
		if _, err := parseSources(bad); err == nil {
			t.Errorf("parseSources(%q) succeeded, want error", bad)
		}
	}
	allowed, err := parseSources(" hongocduc , wiktionary ")
	if err != nil {
		t.Fatal(err)
	}
	if !allowed["hongocduc"] || !allowed["wiktionary"] || allowed["tudientv"] {
		t.Errorf("allowed = %v, want hongocduc and wiktionary only", allowed)
	}
}

func TestMergedBuildRecordsProvenance(t *testing.T) {
	out := filepath.Join(t.TempDir(), "noitu.db")
	cfg := config{
		merged:   fixtureMerged(t, defaultMergedLines()...),
		sources:  "wiktionary",
		out:      out,
		minWords: 1,
	}
	if err := run(cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	db := openOut(t, out)

	want := map[string]string{
		"source_url":       mergedSourceURL,
		"source_commit":    mergedSourceCommit,
		"sources_kept":     "wiktionary",
		"sources_excluded": "hongocduc,tudientv",
		"word_count":       "2",
	}
	for key, value := range want {
		var got string
		if err := db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&got); err != nil {
			t.Errorf("meta[%q] missing: %v", key, err)
			continue
		}
		if got != value {
			t.Errorf("meta[%q] = %q, want %q", key, got, value)
		}
	}
}

func TestRunInputSelection(t *testing.T) {
	merged := fixtureMerged(t, defaultMergedLines()...)
	words := fixtureMerged(t, "học sinh")
	out := filepath.Join(t.TempDir(), "noitu.db")

	cases := []struct {
		name    string
		cfg     config
		wantErr string
	}{
		{"both inputs", config{merged: merged, words: words, sources: "wiktionary", out: out, minWords: 1}, "mutually exclusive"},
		{"neither input", config{sources: "wiktionary", out: out, minWords: 1}, "no input given"},
		{"missing merged file", config{merged: filepath.Join(t.TempDir(), "absent.jsonl"), sources: "wiktionary", out: out, minWords: 1}, "make fetch-dict"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := run(tc.cfg)
			if err == nil {
				t.Fatal("run succeeded, want error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// A fixture is hand-written data; its database must not claim the upstream's
// licence, because the server logs whatever the meta table says.
func TestWordListBuildRecordsNoUpstreamLicense(t *testing.T) {
	out := filepath.Join(t.TempDir(), "noitu.db")
	if err := run(config{words: fixtureMerged(t, "học sinh", "bánh mì"), out: out, minWords: 1}); err != nil {
		t.Fatalf("run: %v", err)
	}
	var license, url string
	db := openOut(t, out)
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'source_license'`).Scan(&license); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'source_url'`).Scan(&url); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(license, "CC BY-SA") || url != "" {
		t.Errorf("fixture build claims upstream provenance: license=%q url=%q", license, url)
	}
}
