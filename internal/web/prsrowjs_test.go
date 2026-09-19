package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// prsrow.js is import-free so it can run under node: the pull-request row's
// status cell, state word and tooltip are pinned here against the WIRE values
// the server really sends. review_state is lower-case on the wire — the TUI's
// first tests used upper-case and stayed green against a build that painted
// no mark at all, so an upper-case value is asserted to yield nothing.
func TestPRRowPartsJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "prsrow.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prsrow.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import { prRowParts } from "./prsrow.mjs";
const now = Date.parse("2026-09-19T12:00:00Z");
const base = { number: 7, title: "Add a thing", author: "ann", state: "open", draft: false,
  review_state: "", source: "feat/x", target: "main", updated: "2026-09-19T09:00:00Z" };
const cases = [
  { ...base, review_state: "approved" },
  { ...base, review_state: "changes_requested" },
  { ...base, review_state: "review_required" },
  { ...base, review_state: "APPROVED" },
  { ...base, draft: true },
  { ...base, state: "merged", review_state: "approved" },
  { ...base, state: "closed" },
  { ...base, state: "unavailable", title: "", author: "", source: "", target: "", updated: "" },
];
console.log(JSON.stringify(cases.map((c) => prRowParts(c, now))));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got []struct {
		Mark, Word, Title, Tip string
		Dim                    bool
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	want := []struct {
		mark, word, title, tip string
		dim                    bool
	}{
		{"✓", "", "Add a thing", "ann · feat/x → main · 3h ago", false},
		{"✗", "", "Add a thing", "ann · feat/x → main · 3h ago", false},
		{"●", "", "Add a thing", "ann · feat/x → main · 3h ago", false},
		{"", "", "Add a thing", "ann · feat/x → main · 3h ago", false},
		{"", "draft", "Add a thing", "ann · feat/x → main · 3h ago", false},
		{"✓", "merged", "Add a thing", "ann · feat/x → main · 3h ago", true},
		{"", "closed", "Add a thing", "ann · feat/x → main · 3h ago", true},
		{"", "unavailable", "(no longer on the forge)", "", true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Mark != w.mark || g.Word != w.word || g.Title != w.title || g.Tip != w.tip || g.Dim != w.dim {
			t.Errorf("case %d: got %+v, want %+v", i, g, w)
		}
	}
}
