package agentdocs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The published cold-anchoring cases (spec §4.3 / §9): a reader that never
// saw the writer's file places a note from its lines alone.
func TestColdAnchorCases(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "specs", "gg-session-docs", "cold-anchor-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name string   `json:"name"`
		File []string `json:"file"`
		Note struct {
			Start, End int
			Lines      []string `json:"lines"`
		} `json:"note"`
		Expect struct {
			Start, End int
			Outdated   bool `json:"outdated"`
		} `json:"expect"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 7 {
		t.Fatalf("%d cases; the spec names at least 7", len(cases))
	}
	for _, c := range cases {
		s, e, out := ColdPlace(c.File, c.Note.Start, c.Note.End, c.Note.Lines)
		if s != c.Expect.Start || e != c.Expect.End || out != c.Expect.Outdated {
			t.Errorf("%s: got %d-%d outdated=%v, want %d-%d outdated=%v", c.Name, s, e, out, c.Expect.Start, c.Expect.End, c.Expect.Outdated)
		}
	}
}
