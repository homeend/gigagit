package agentdocs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// docs/specs/gg-steer/reanchor-cases.json: gg's re-anchoring rule as cases a
// reader ports (spec 2026-10-15-gg-steer-ide-consumer §5, §7). A failing
// case means the CASE (or the spec's wording) is wrong, never this code.
func TestReanchorCases(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "specs", "gg-steer", "reanchor-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name string   `json:"name"`
		Old  []string `json:"old"`
		Cur  []string `json:"cur"`
		Note struct {
			Start, End int
			Outdated   bool
			Anchor     []string
		} `json:"note"`
		Expect struct {
			Start, End int
			Outdated   bool
		} `json:"expect"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		n := &note{Note: Note{Start: c.Note.Start, End: c.Note.End, Outdated: c.Note.Outdated}, anchor: c.Note.Anchor}
		if n.anchor == nil {
			// AddNote records the lines the note was placed on.
			n.anchor = append([]string(nil), c.Old[c.Note.Start-1:c.Note.End]...)
		}
		reanchor([]*note{n}, c.Old, c.Cur)
		if n.Start != c.Expect.Start || n.End != c.Expect.End || n.Outdated != c.Expect.Outdated {
			t.Errorf("%s: got %d-%d outdated=%v, want %d-%d outdated=%v", c.Name, n.Start, n.End, n.Outdated, c.Expect.Start, c.Expect.End, c.Expect.Outdated)
		}
	}
	if len(cases) < 8 {
		t.Fatalf("%d cases, want the spec's eight", len(cases))
	}
}
