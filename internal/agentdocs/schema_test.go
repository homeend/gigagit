package agentdocs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/notemeta"
)

func specPath(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", "docs", "specs", "gg-session-docs"}, parts...)...)
}

func loadSchema(t *testing.T, name string) *jsonschema.Resolved {
	t.Helper()
	data, err := os.ReadFile(specPath(name))
	if err != nil {
		t.Fatal(err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return rs
}

// tomlToJSON is the spec's TOML → JSON mapping: decode into any, re-encode.
func tomlToJSON(t *testing.T, data []byte) any {
	t.Helper()
	var v any
	if err := toml.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(v) // time.Time → RFC 3339 string
	if err != nil {
		t.Fatal(err)
	}
	var inst any
	if err := json.Unmarshal(raw, &inst); err != nil {
		t.Fatal(err)
	}
	return inst
}

// Everything gg writes validates against the published schemas (spec §9).
func TestSessionDocShapesMatchSchemas(t *testing.T) {
	t.Parallel()
	ov, note := loadSchema(t, "overview-frontmatter.schema.json"), loadSchema(t, "note.schema.json")
	now := time.Date(2026, 10, 18, 14, 32, 1, 0, time.UTC)
	for _, o := range []OverviewFile{
		{Title: "T", Text: "body\n"},
		{Title: "T", Author: "claude", Created: now, Text: "[a](a.go:1)\n"},
	} {
		data, err := EncodeOverviewFile(o)
		if err != nil {
			t.Fatal(err)
		}
		head, _, _ := splitFrontMatter(data)
		if err := ov.Validate(tomlToJSON(t, head)); err != nil {
			t.Errorf("overview %+v: %v", o, err)
		}
	}
	for _, n := range []NoteFile{
		{Path: "a.go", Start: 1, End: 1, Summary: "s", Lines: []string{"x"}},
		{Path: "dir/a.go", Start: 3, End: 4, Summary: "s", Rationale: "r", Author: "ada", Created: now, Lines: []string{"x", "y"},
			Meta: []notemeta.KV{{Key: "kind", Value: "design"}}, Display: notemeta.Display{"kind": {Show: []string{"badge"}}}},
	} {
		data, err := EncodeNoteFile(n)
		if err != nil {
			t.Fatal(err)
		}
		if err := note.Validate(tomlToJSON(t, data)); err != nil {
			t.Errorf("note %+v: %v", n, err)
		}
	}
	// What the schema refuses, gg's parser refuses too.
	for _, bad := range []string{"start = 1\nend = 1\nsummary = \"s\"\nlines = [\"x\"]\n", "path = \"a.go\"\nstart = 0\nend = 1\nsummary = \"s\"\nlines = [\"x\",\"y\"]\n"} {
		if err := note.Validate(tomlToJSON(t, []byte(bad))); err == nil {
			t.Errorf("schema accepted %q", bad)
		}
		if _, err := ParseNoteFile([]byte(bad)); err == nil {
			t.Errorf("parser accepted %q", bad)
		}
	}
}
