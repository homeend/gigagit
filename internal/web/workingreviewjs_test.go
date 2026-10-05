package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The web lists working reviews like the TUI: under the working tree, a
// Reviews sub with one row each, the outdated one saying so; a working
// review's file rows say which files it never read or that changed since.
const workingReviewHarness = `
import { anBuildRows, reviewCells, workingStateHTML } from "./wr.mjs";
const ov = { worktree: "repo", unstaged: [], staged: [], untracked: [], commits: [], shelves: [],
  working_reviews: [
    { id: "a", kind: "working", agent: "Claude", summary: "Review: working changes", outdated: false },
    { id: "b", kind: "working", agent: "Claude", summary: "Review: working changes", outdated: true } ] };
const rows = anBuildRows(ov);
const shape = rows.map((r) => r.kind + ":" + r.depth + ":" + (r.kind === "review" ? r.review.id : r.text));
const where = rows.filter((r) => r.kind === "review").map((r) => reviewCells(r, Date.now()).where);
const d = { working: true, states: { "a.txt": "matches", "c.txt": "changed" } };
const marks = ["a.txt", "c.txt", "n.txt"].map((p) => workingStateHTML(d, p));
const commit = workingStateHTML({ states: {} }, "x");
console.log(JSON.stringify({ shape, where, marks, commit }));
`

func TestWorkingReviewsInTheWebAllNotesAndReviewView(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	mod := jsFunc(t, "allnotes.js", "anBuildRows") + "\n" + jsFunc(t, "allnotes.js", "anAgo") + "\n" +
		jsFunc(t, "allnotes.js", "reviewCells") + "\n" + jsFunc(t, "reviews.js", "workingStateHTML") +
		"\nexport { anBuildRows, reviewCells, workingStateHTML };\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wr.mjs"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(workingReviewHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct {
		Shape  []string `json:"shape"`
		Where  []string `json:"where"`
		Marks  []string `json:"marks"`
		Commit string   `json:"commit"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
		t.Fatalf("not the harness JSON: %v\n%s", err, out)
	}
	if strings.Join(got.Shape, "|") != "group:0:Working tree  (repo)|sub:1:Reviews|review:2:a|review:2:b" {
		t.Errorf("shape = %v", got.Shape)
	}
	if strings.Join(got.Where, "|") != "working changes|outdated" {
		t.Errorf("where = %v", got.Where)
	}
	if got.Marks[0] != "" || !strings.Contains(got.Marks[1], "changed since the review") ||
		!strings.Contains(got.Marks[2], "not reviewed") || got.Commit != "" {
		t.Errorf("marks = %q commit = %q", got.Marks, got.Commit)
	}
}

// The ✎ on a working-list file row comes from EVERY current review, as in
// the TUI (reviewedPaths): two current reviews that each match a different
// file mark both; an outdated review marks nothing.
const workingReviewedHarness = `
import { workingReviewedPaths } from "./wrp.mjs";
const rs = [
  { id: "new", current: true, matches: ["a.txt"] },
  { id: "old", current: true, matches: ["b.txt"] },
  { id: "gone", current: false, matches: ["c.txt"] } ];
console.log(JSON.stringify([...workingReviewedPaths(rs)].sort()));
`

func TestWebWorkingReviewMarksUnionCurrentReviews(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	mod := jsFunc(t, "reviews.js", "workingReviewedPaths") + "\nexport { workingReviewedPaths };\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wrp.mjs"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(workingReviewedHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != `["a.txt","b.txt"]` {
		t.Errorf("reviewed = %s, want a.txt and b.txt", got)
	}
	// renderFiles draws its ✎ from that set, not from the newest review alone.
	src, err := os.ReadFile(filepath.Join("static", "files.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "workingReviewedPaths(state.noteCounts.working_reviews)") {
		t.Error("files.js renderFiles does not take its ✎ set from workingReviewedPaths")
	}
}
