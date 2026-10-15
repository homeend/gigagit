package notebatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/pelletier/go-toml/v2"
)

// specDir is docs/specs/gg-notes-store relative to this package.
func specDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "docs", "specs", "gg-notes-store")
}

func loadSchema(t *testing.T, name string) *jsonschema.Resolved {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(specDir(t), name))
	if err != nil {
		t.Fatal(err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("%s: resolve: %v", name, err)
	}
	return rs
}

// validateJSON decodes raw as generic JSON (the schema validator's instance
// form) and validates it.
func validateJSON(rs *jsonschema.Resolved, raw []byte) error {
	var inst any
	if err := json.Unmarshal(raw, &inst); err != nil {
		return err
	}
	return rs.Validate(inst)
}

// Every canonical document gg writes validates against the strict schema —
// the drift guard of spec 2026-10-15-gg-notes-store-format §9.1.
func TestCanonicalOverviewMatchesStrictSchema(t *testing.T) {
	t.Parallel()
	strict := loadSchema(t, "overview-document.schema.json")
	lenient := loadSchema(t, "overview-document.lenient.schema.json")
	docs := []string{
		`{"version":1,"title":"Only text","overview":"No anchors here."}`,
		`{"version":1,"title":" Trimmed ","overview":"[gate](a.go:3)","files":[{"path":"a.go","annotations":[{"newRange":[3,4],"summary":"the gate","rationale":"why"}]}]}`,
		`{"version":1,"title":"No rationale","overview":"[x](b.go:1-2)","files":[{"path":"b.go","annotations":[{"newRange":[1,2],"summary":"s"}]}]}`,
		`{"version":1,"title":"Empty files","overview":"text","files":[]}`,
		// an agent's input form: a summary-less file with no annotations
		`{"version":1,"title":"Bare file","overview":"[f](c.go)","files":[{"path":"c.go"}]}`,
	}
	for _, in := range docs {
		d, err := ParseOverview([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		canon := d.Canonical()
		if err := validateJSON(strict, canon); err != nil {
			t.Errorf("strict schema rejects gg's own canonical form:\n%s\n%v", canon, err)
		}
		if err := validateJSON(lenient, canon); err != nil {
			t.Errorf("lenient schema rejects gg's own canonical form:\n%s\n%v", canon, err)
		}
	}
}

// Review focus 1: the strict schema rejects a key gg does not store; the
// lenient one tolerates it (spec §8.3 / §9.1).
func TestSchemaStrictRejectsLenientAccepts(t *testing.T) {
	t.Parallel()
	strict := loadSchema(t, "overview-document.schema.json")
	lenient := loadSchema(t, "overview-document.lenient.schema.json")
	extra := []byte(`{"version":1,"title":"t","overview":"o","future":true,"files":[{"path":"a.go","meta":{"k":"v"},"annotations":[{"newRange":[1,1],"summary":"s","confidence":0.5}]}]}`)
	if err := validateJSON(strict, extra); err == nil {
		t.Fatal("strict schema accepted unknown keys")
	}
	if err := validateJSON(lenient, extra); err != nil {
		t.Fatalf("lenient schema rejected unknown keys: %v", err)
	}
	// Both still refuse the shapes gg refuses.
	for _, bad := range []string{
		`{"version":2,"title":"t","overview":"o"}`,
		`{"version":1,"title":"","overview":"o"}`,
		`{"version":1,"title":"t","overview":""}`,
		`{"version":1,"title":"t","overview":"o","files":[{"path":""}]}`,
		`{"version":1,"title":"t","overview":"o","files":[{"path":"a","annotations":[{"newRange":[0,1],"summary":"s"}]}]}`,
		`{"version":1,"title":"t","overview":"o","files":[{"path":"a","annotations":[{"newRange":[1],"summary":"s"}]}]}`,
		`{"version":1,"title":"t","overview":"o","files":[{"path":"a","annotations":[{"newRange":[1,1]}]}]}`,
	} {
		if err := validateJSON(strict, []byte(bad)); err == nil {
			t.Errorf("strict schema accepted %s", bad)
		}
		if err := validateJSON(lenient, []byte(bad)); err == nil {
			t.Errorf("lenient schema accepted %s", bad)
		}
	}
}

// The fixture store's documents are canonical: each validates strictly and
// round-trips through Canonical() unchanged.
func TestFixtureDocumentsAreCanonical(t *testing.T) {
	t.Parallel()
	strict := loadSchema(t, "overview-document.schema.json")
	data, err := os.ReadFile(filepath.Join(specDir(t), "fixture", "commits.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Notes []struct {
			Tags      []string `toml:"tags"`
			Rationale string   `toml:"rationale"`
		} `toml:"notes"`
	}
	if err := toml.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, n := range file.Notes {
		if !slices.Contains(n.Tags, "overview") {
			continue
		}
		seen++
		if err := validateJSON(strict, []byte(n.Rationale)); err != nil {
			t.Errorf("fixture document fails the strict schema: %v\n%s", err, n.Rationale)
		}
		d, err := ParseOverview([]byte(n.Rationale))
		if err != nil {
			t.Fatal(err)
		}
		if string(d.Canonical()) != n.Rationale {
			t.Errorf("fixture document is not in canonical form; gg would store:\n%s\ngot:\n%s", d.Canonical(), n.Rationale)
		}
	}
	if seen != 2 {
		t.Fatalf("fixture holds %d overview records, want 2", seen)
	}
}
