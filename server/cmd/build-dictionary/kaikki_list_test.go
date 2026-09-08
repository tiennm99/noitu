package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureKaikki writes a miniature stand-in for the kaikki export: the same
// JSONL shape, a handful of rows.
func fixtureKaikki(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kaikki.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func defaultKaikkiLines() []string {
	return []string{
		`{"word": "Hà Nội", "pos": "name", "lang_code": "vi", "senses": [{"glosses": ["thủ đô"]}]}`, // capitalized, name POS — kept, lowercased
		`{"word": "học sinh", "pos": "noun", "lang_code": "vi"}`,
		`{"word": "học sinh", "pos": "verb", "lang_code": "vi"}`, // same word, second POS — kept once
		`{"word": "student", "pos": "noun", "lang_code": "en"}`,  // not Vietnamese-language — rejected and counted
		`{"word": "pháp", "pos": "noun", "lang_code": "vi"}`,     // single syllable — rejected downstream
		``,
	}
}

func TestKaikkiListKeepsVietnameseEntries(t *testing.T) {
	words, rejects, pos, prov, err := readKaikkiList(fixtureKaikki(t, defaultKaikkiLines()...))
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for w := range words {
		got = append(got, w)
	}
	assertSameStrings(t, got, []string{"hà nội", "học sinh"})

	if n := rejects[rejectNotVietnamese]; n != 1 {
		t.Errorf("non-Vietnamese rejects = %d, want 1", n)
	}
	if n := rejects[rejectTooShort]; n != 1 {
		t.Errorf("too-short rejects = %d, want 1 (pháp)", n)
	}
	if pos["noun"] != 2 || pos["verb"] != 1 || pos["name"] != 1 {
		t.Errorf("pos tally = %v, want noun 2, verb 1, name 1 (en row excluded)", pos)
	}
	if prov.rows != 5 {
		t.Errorf("rows = %d, want 5", prov.rows)
	}
}

func TestKaikkiListHashesTheBytesItRead(t *testing.T) {
	path := fixtureKaikki(t, defaultKaikkiLines()...)
	_, _, _, prov, err := readKaikkiList(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if want := hex.EncodeToString(sum[:]); prov.sha256 != want {
		t.Errorf("sha256 = %s, want %s", prov.sha256, want)
	}
	if prov.fetchedAt.IsZero() {
		t.Error("fetchedAt is zero, want the file's modification time")
	}
}

// fixtureKaikkiRaw writes exact bytes, for the shapes fixtureKaikki's trailing
// newline would hide.
func fixtureKaikkiRaw(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kaikki.jsonl")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestKaikkiListHandlesDownloadShapes(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantWords int
		wantRows  int
		wantErr   string
	}{
		{"final line without newline",
			`{"word": "học sinh", "pos": "noun", "lang_code": "vi"}` + "\n" + `{"word": "bánh mì", "pos": "noun", "lang_code": "vi"}`,
			2, 2, ""},
		{"HTTP error page instead of JSONL", "<html><body>503</body></html>\n", 0, 0, ":1: malformed"},
		{"cut mid-line", `{"word": "học sinh", "pos": "noun", "lang_code": "vi"}` + "\n" + `{"word": "bánh`, 0, 0, ":2: malformed"},
		{"bare JSON literal", "null\n", 0, 0, ":1: malformed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			words, _, _, prov, err := readKaikkiList(fixtureKaikkiRaw(t, tc.raw))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(words) != tc.wantWords || prov.rows != tc.wantRows {
				t.Errorf("words=%d rows=%d, want %d/%d", len(words), prov.rows, tc.wantWords, tc.wantRows)
			}
		})
	}
}

func TestKaikkiListNamesMalformedLine(t *testing.T) {
	path := fixtureKaikki(t,
		`{"word": "học sinh", "pos": "noun", "lang_code": "vi"}`,
		`{"word": "broken"`,
	)
	_, _, _, _, err := readKaikkiList(path)
	if err == nil {
		t.Fatal("malformed line was skipped, want error")
	}
	if !strings.Contains(err.Error(), ":2:") {
		t.Errorf("error does not name line 2: %v", err)
	}
}

func TestKaikkiListReadsLongLines(t *testing.T) {
	// A real row carries every sense and translation and can exceed any
	// scanner buffer; the reader must not have a line cap.
	padding := strings.Repeat("x", 2<<20)
	path := fixtureKaikki(t, `{"word": "học sinh", "pos": "noun", "lang_code": "vi", "note": "`+padding+`"}`)
	words, _, _, _, err := readKaikkiList(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := words["học sinh"]; !ok {
		t.Error("word on a 2 MB line was lost")
	}
}

func TestFormatPosTally(t *testing.T) {
	got := formatPosTally(map[string]int{"verb": 2, "noun": 5, "": 1})
	if want := "noun 5, verb 2, (none) 1"; got != want {
		t.Errorf("tally = %q, want %q", got, want)
	}
}

func TestKaikkiBuildRecordsProvenance(t *testing.T) {
	out := filepath.Join(t.TempDir(), "noitu.db")
	path := fixtureKaikki(t, defaultKaikkiLines()...)
	if err := run(config{kaikki: path, out: out, minWords: 1}); err != nil {
		t.Fatalf("run: %v", err)
	}
	db := openOut(t, out)

	raw, _ := os.ReadFile(path)
	sum := sha256.Sum256(raw)
	want := map[string]string{
		"source_url":     kaikkiSourceURL,
		"source_sha256":  hex.EncodeToString(sum[:]),
		"source_rows":    "5",
		"source_license": "CC BY-SA 4.0 (https://creativecommons.org/licenses/by-sa/4.0/)",
		"word_count":     "2",
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
	for _, gone := range []string{"source_commit", "sources_kept", "sources_excluded"} {
		var got string
		if err := db.QueryRow(`SELECT value FROM meta WHERE key = ?`, gone).Scan(&got); err == nil {
			t.Errorf("meta[%q] = %q, want absent", gone, got)
		}
	}
	var fetched string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key = 'source_fetched_at'`).Scan(&fetched); err != nil || fetched == "" {
		t.Errorf("meta[source_fetched_at] missing or empty: %v", err)
	}
}

func TestRunInputSelection(t *testing.T) {
	kaikki := fixtureKaikki(t, defaultKaikkiLines()...)
	words := fixtureKaikki(t, "học sinh")
	out := filepath.Join(t.TempDir(), "noitu.db")

	cases := []struct {
		name    string
		cfg     config
		wantErr string
	}{
		{"both inputs", config{kaikki: kaikki, words: words, out: out, minWords: 1}, "mutually exclusive"},
		{"neither input", config{out: out, minWords: 1}, "no input given"},
		{"missing kaikki file", config{kaikki: filepath.Join(t.TempDir(), "absent.jsonl"), out: out, minWords: 1}, "make fetch-dict"},
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
	if err := run(config{words: fixtureKaikki(t, "học sinh", "bánh mì"), out: out, minWords: 1}); err != nil {
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
