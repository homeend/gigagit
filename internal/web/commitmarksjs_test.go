package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The marked-commit helpers are pure: what Compare selection diffs (the TUI's
// compareSelectionEndpoints — 2 marks = older..newer, 3+ = oldest^..newest,
// refused from a root) and which rows a shift-gesture marks (a contiguous run
// between two feed rows, either direction).
const commitMarksHarness = `
import { markedCompareEnds, runBetween } from "./marks.mjs";
const rows = [
  { hash: "d4", parent_ids: ["c3"] },
  { hash: "c3", parent_ids: ["b2"] },
  { hash: "b2", parent_ids: ["a1"] },
  { hash: "a1", parent_ids: [] },
];
console.log(JSON.stringify({
  two: markedCompareEnds(rows, ["d4", "b2"]),
  three: markedCompareEnds(rows, ["d4", "c3", "b2"]),
  root: markedCompareEnds(rows, ["c3", "b2", "a1"]),
  one: markedCompareEnds(rows, ["c3"]),
  down: runBetween(rows, 0, 2),
  up: runBetween(rows, 3, 1),
  same: runBetween(rows, 1, 1),
  filtered: runBetween(rows, 0, 3, new Set([0, 3])),
}));
`

func TestCommitMarkHelpers(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	mod := jsFunc(t, "commits.js", "markedCompareEnds") + "\n" + jsFunc(t, "commits.js", "runBetween") +
		"\nexport { markedCompareEnds, runBetween };\n"
	dir := t.TempDir()
	for name, body := range map[string]string{"marks.mjs": mod, "run.mjs": commitMarksHarness} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct {
		Two, Three, Root, One struct {
			Base, Tip, Oldest, Newest, Err string
		}
		Down, Up, Same, Filtered []string
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
		t.Fatalf("not the harness JSON: %v\n%s", err, out)
	}
	if got.Two.Base != "b2" || got.Two.Tip != "d4" || got.Two.Err != "" {
		t.Errorf("two marks = %+v, want b2..d4", got.Two)
	}
	if got.Three.Base != "a1" || got.Three.Tip != "d4" || got.Three.Oldest != "b2" {
		t.Errorf("three marks = %+v, want a1..d4 (the oldest's parent), oldest b2", got.Three)
	}
	if got.Root.Err == "" {
		t.Errorf("a 3+ range from the root = %+v, want a refusal", got.Root)
	}
	if got.One.Err == "" {
		t.Errorf("one mark = %+v, want a refusal", got.One)
	}
	if strings.Join(got.Down, " ") != "d4 c3 b2" || strings.Join(got.Up, " ") != "c3 b2 a1" || strings.Join(got.Same, " ") != "c3" {
		t.Errorf("runs = %v / %v / %v", got.Down, got.Up, got.Same)
	}
	// Under the / filter a run covers only the rows on screen: the hidden
	// commits between two matches are never marked (a drop would delete them).
	if strings.Join(got.Filtered, " ") != "d4 a1" {
		t.Errorf("filtered run = %v, want only the shown rows [d4 a1]", got.Filtered)
	}
}

// The commit menu offers every marked-commit action the TUI's . menu does, each
// over its own endpoint; shift+↑/↓ and shift+click grow the marks.
func TestCommitMarksMenuRows(t *testing.T) {
	t.Parallel()
	src := staticSrc(t, "commits.js")
	for _, want := range []string{
		`"save to previews"`, `"save reversed to previews"`, `"/api/saved-compares", { a, b, label`,
		`"compare range of " + marked.length + " commits (combined diff)"`,
		`"review marked range (AI)…"`, `startReview("range", "", "", "", markedInFeedOrder())`,
		`"drop " + marked.length + " marked commits…"`, `"/api/commit-drop"`,
		`"/api/commit-squash", { shas: m, reorder }`, `e.data.not_adjacent`, `"reorder & squash"`,
		`"/api/commit-cherry-pick"`, `" marked commits onto "`,
		`"rename branch " + ref.name + "…"`, `"delete branch " + ref.name`,
		`if (e.shiftKey && !(state.wt && i === 0))`,
		// both run gestures honour the / filter
		`runBetween(state.rows, at - wtCount(), i - wtCount(), shownRows())`,
		`if (c && (!shown || shown.has(state.cursor - wtCount()))) state.cmarks.add(c.hash)`,
		// rows that can only fail are hidden: delete for a branch checked out in
		// any worktree, squash/drop with no branch checked out
		`(state.worktrees || []).some((w) => w.branch === ref.name)`,
		`if (marked.length >= 2 && cur && cur.name)`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("commits.js lacks %q", want)
		}
	}
	if keys := staticSrc(t, "keys.js"); !strings.Contains(keys, `markRunStep(e.key === "ArrowDown" ? 1 : -1)`) {
		t.Error("keys.js does not route shift+↑/↓ on the Commits pane to markRunStep")
	}
	if rv := staticSrc(t, "review.js"); !strings.Contains(rv, `openLaunch({ kind: "review", target, branch, sha, preview, shas })`) {
		t.Error("review.js does not hand the marks to the launch dialog")
	}
}
