package agentdocs

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/notemeta"
)

func TestMintIDShapeAndUniqueness(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 18, 14, 32, 1, 0, time.FixedZone("CET", 3600))
	id := MintID(now, bytes.NewReader([]byte{0x9f, 0x3a}))
	if id != "20261018T133201-9f3a" { // UTC
		t.Fatalf("MintID = %q", id)
	}
	if !ValidID(id) {
		t.Fatal("a minted id must be valid")
	}
	// Different random bytes, different ids; nil reads crypto/rand and keeps
	// the shape. (Four hex digits are the spec's shape, not a uniqueness
	// guarantee: a writer that needs certainty writes with O_EXCL.)
	if MintID(now, bytes.NewReader([]byte{0, 0})) == MintID(now, bytes.NewReader([]byte{0, 1})) {
		t.Fatal("ids with different random bytes must differ")
	}
	if x := MintID(now, nil); !ValidID(x) || !strings.HasPrefix(x, "20261018T133201-") || len(x) != len(id) {
		t.Fatalf("MintID(now, nil) = %q", x)
	}
}

func TestIDVectors(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "specs", "gg-session-docs", "id-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Valid   []string `json:"valid"`
		Invalid []string `json:"invalid"`
		Names   []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"names"`
		Ignored []string `json:"ignored"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	for _, id := range v.Valid {
		if !ValidID(id) {
			t.Errorf("%q must be valid", id)
		}
	}
	for _, id := range v.Invalid {
		if ValidID(id) {
			t.Errorf("%q must be invalid", id)
		}
	}
	for _, n := range v.Names {
		id, kind, ok := DocName(n.Name)
		if !ok || id != n.ID || kind != n.Kind {
			t.Errorf("DocName(%q) = %q %q %v, want %q %q", n.Name, id, kind, ok, n.ID, n.Kind)
		}
		if FileName(n.ID, n.Kind) != n.Name {
			t.Errorf("FileName(%q,%q) = %q", n.ID, n.Kind, FileName(n.ID, n.Kind))
		}
	}
	for _, n := range v.Ignored {
		if _, _, ok := DocName(n); ok {
			t.Errorf("%q must be ignored", n)
		}
	}
}

func TestOverviewFileRoundTrip(t *testing.T) {
	t.Parallel()
	in := OverviewFile{Title: "How scroll coalescing works", Author: "claude",
		Created: time.Date(2026, 10, 18, 14, 32, 1, 0, time.UTC),
		Text:    "Scroll events are coalesced.\n\n1. [The handler](internal/tui/scroll.go:40-58) …\n"}
	data, err := EncodeOverviewFile(in)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"+++\nformat = 1\n", "title = 'How scroll coalescing works'\n", "author = 'claude'\n", "created = 2026-10-18T14:32:01Z\n", "+++\nScroll events"} {
		if !strings.Contains(s, want) {
			t.Errorf("encoded file lacks %q:\n%s", want, s)
		}
	}
	out, err := ParseOverviewFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if out.Format != 1 || out.Title != in.Title || out.Author != in.Author || !out.Created.Equal(in.Created) || out.Text != in.Text {
		t.Fatalf("round trip = %+v", out)
	}
}

func TestOverviewFileDefaultsAndRefusals(t *testing.T) {
	t.Parallel()
	o, err := ParseOverviewFile([]byte("+++\ntitle = \"T\"\n+++\nbody\n"))
	if err != nil || o.Author != "agent" || !o.Created.IsZero() || o.Format != 1 || o.Text != "body\n" {
		t.Fatalf("minimal file: %+v %v", o, err)
	}
	o, err = ParseOverviewFile([]byte("+++\r\ntitle = \"T\"\r\ncreated = 2026-10-18T16:32:01+02:00\r\n+++\r\nbody"))
	if err != nil || !o.Created.Equal(time.Date(2026, 10, 18, 14, 32, 1, 0, time.UTC)) {
		t.Fatalf("CRLF + offset: %+v %v", o, err)
	}
	for in, want := range map[string]string{
		"title = \"T\"\nbody":                                          "an overview file starts with a +++ front matter block",
		"+++\ntitle = \"T\"\nbody":                                     "an overview file starts with a +++ front matter block",
		"+++\nauthor = \"a\"\n+++\nbody":                               "an overview needs a title",
		"+++\ntitle = \"" + strings.Repeat("x", 201) + "\"\n+++\nbody": "the title must be one line of at most 200 characters",
		"+++\ntitle = \"T\"\n+++\n   \n":                               "an overview needs text",
		"+++\ntitle = \"T\"\n+++\n" + strings.Repeat("x", 64<<10+1):    "the text is over 64 KiB",
		"+++\ntitle = \n+++\nbody":                                     "toml: ",
		"+++\ntitle = \"T\"\ncreated = \"yesterday\"\n+++\nbody":       "created must be an RFC 3339 date-time",
	} {
		_, err := ParseOverviewFile([]byte(in))
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%q: err = %v, want prefix %q", in[:min(30, len(in))], err, want)
		}
	}
}

func TestNoteFileRoundTripWithMetaAndDisplay(t *testing.T) {
	t.Parallel()
	in := NoteFile{Path: "internal/tui/scroll.go", Start: 102, End: 104,
		Summary: "One lock around flush.", Rationale: "Without it …\nsecond line", Author: "claude",
		Created: time.Date(2026, 10, 18, 14, 32, 5, 0, time.UTC),
		Lines:   []string{"func (s *scroller) flush() {", "\ts.mu.Lock()", "\tdefer s.mu.Unlock()"},
		Meta:    []notemeta.KV{{Key: "kind", Value: "design"}},
		Display: notemeta.Display{"kind": {Show: []string{"badge"}, Values: map[string]notemeta.Value{"design": {Color: "blue"}}}}}
	data, err := EncodeNoteFile(in)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"format = 1\n", "path = 'internal/tui/scroll.go'\n", "start = 102\n", "end = 104\n", "lines = [", "[[meta]]\nkey = 'kind'\nvalue = 'design'\n", "[display.kind]\nshow = ['badge']\n", "[display.kind.values.design]\ncolor = 'blue'\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("encoded note lacks %q:\n%s", want, s)
		}
	}
	out, err := ParseNoteFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if out.Path != in.Path || out.Start != 102 || out.End != 104 || out.Summary != in.Summary || out.Rationale != in.Rationale ||
		out.Author != "claude" || !out.Created.Equal(in.Created) || strings.Join(out.Lines, "|") != strings.Join(in.Lines, "|") ||
		len(out.Meta) != 1 || out.Meta[0].Value != "design" || out.Display["kind"].Values["design"].Color != "blue" {
		t.Fatalf("round trip = %+v", out)
	}
}

func TestNoteFileDefaultsAndRefusals(t *testing.T) {
	t.Parallel()
	minimal := "path = \"a.go\"\nstart = 2\nend = 2\nsummary = \"s\"\nlines = [\"x\"]\n"
	n, err := ParseNoteFile([]byte(minimal))
	if err != nil || n.Author != "agent" || n.Format != 1 || n.Rationale != "" || len(n.Meta) != 0 || n.Display != nil {
		t.Fatalf("minimal note: %+v %v", n, err)
	}
	// CRLF inside lines is stripped on encode so LF and CRLF content agree.
	data, _ := EncodeNoteFile(NoteFile{Path: "a.go", Start: 1, End: 2, Summary: "s", Lines: []string{"a\r", "b\r"}})
	if n, err := ParseNoteFile(data); err != nil || strings.Join(n.Lines, "|") != "a|b" {
		t.Fatalf("CRLF lines: %+v %v", n, err)
	}
	for name, tc := range map[string]struct{ in, want string }{
		"no path":        {"start = 1\nend = 1\nsummary = \"s\"\nlines = [\"x\"]\n", "a note needs a path"},
		"abs path":       {"path = \"/a.go\"\nstart = 1\nend = 1\nsummary = \"s\"\nlines = [\"x\"]\n", "path must be a relative slash path inside the worktree"},
		"dotdot":         {"path = \"a/../b.go\"\nstart = 1\nend = 1\nsummary = \"s\"\nlines = [\"x\"]\n", "path must be a relative slash path inside the worktree"},
		"backslash":      {"path = \"a\\\\b.go\"\nstart = 1\nend = 1\nsummary = \"s\"\nlines = [\"x\"]\n", "path must be a relative slash path inside the worktree"},
		"zero start":     {"path = \"a.go\"\nstart = 0\nend = 1\nsummary = \"s\"\nlines = [\"x\", \"y\"]\n", "a line number is 1-based"},
		"end<start":      {"path = \"a.go\"\nstart = 3\nend = 2\nsummary = \"s\"\nlines = [\"x\"]\n", "the range ends before it starts"},
		"no summary":     {"path = \"a.go\"\nstart = 1\nend = 1\nsummary = \"  \"\nlines = [\"x\"]\n", "a note needs a summary"},
		"long summary":   {"path = \"a.go\"\nstart = 1\nend = 1\nsummary = \"" + strings.Repeat("s", 501) + "\"\nlines = [\"x\"]\n", "the summary is longer than 500 characters"},
		"long rationale": {"path = \"a.go\"\nstart = 1\nend = 1\nsummary = \"s\"\nrationale = \"" + strings.Repeat("r", 4001) + "\"\nlines = [\"x\"]\n", "the rationale is longer than 4000 characters"},
		"lines count":    {"path = \"a.go\"\nstart = 1\nend = 2\nsummary = \"s\"\nlines = [\"x\"]\n", "lines must hold end-start+1 entries (got 1, want 2)"},
		"no lines":       {"path = \"a.go\"\nstart = 1\nend = 1\nsummary = \"s\"\n", "lines must hold end-start+1 entries (got 0, want 1)"},
		"bad toml":       {"path = \n", "toml: "},
		"bad created":    {"path = \"a.go\"\nstart = 1\nend = 1\nsummary = \"s\"\nlines = [\"x\"]\ncreated = \"now\"\n", "created must be an RFC 3339 date-time"},
	} {
		_, err := ParseNoteFile([]byte(tc.in))
		if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want prefix %q", name, err, tc.want)
		}
	}
}
