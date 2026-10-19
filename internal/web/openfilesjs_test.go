package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ofPureStart = "// --- switcher model (pure; guarded against Go) ---"
const ofPureEnd = "// --- end switcher model ---"

func TestSwitcherModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const rows = switcherRows([
  {id: "f2", path: "a/b.go", line: 12, source: "worktree", state: "shown", notes: 2},
  {id: "f1", path: "c.txt", line: 0, source: "commit", rev: "0123456789", state: "shown"},
], "f2");
console.log(JSON.stringify(rows) + "|" + [clampSel(5, 2), clampSel(-1, 2), clampSel(3, 0)].join(","));
`)
	want := `[{"id":"f2","mark":"●","path":"a/b.go","line":":12","source":"worktree","rev":"","notes":2,"meta":""},{"id":"f1","mark":"○","path":"c.txt","line":"","source":"commit","rev":"0123456789","notes":0,"meta":""}]|1,0,0`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// The viewer's footer is built in a template literal, where "\ " is an escape
// for a plain space: the chip must write "\\" or the page shows "ctrl+ open
// files" (found by the 5c browser check).
// An overview's row names it by its title (its path is the TUI's display
// name, overview-<n>.md).
func TestSwitcherOverviewRowJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const r = switcherRows([{id: "f4", path: "overview-4.md", source: "overview", title: "The tour", line: 0}], "")[0];
console.log(r.path + "|" + r.source);
`)
	if out != "The tour|overview" {
		t.Fatalf("got %s", out)
	}
}

// An overview row's meta is the TUI's: "overview · N anchors · author ·
// age"; D deletes the overview, or a noted file's notes, or nothing, and
// the confirm's prompt follows (the TUI's words).
func TestSwitcherOverviewRowMetaAndDeleteActionJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const now = Date.parse("2026-01-02T00:00:00Z");
const ov = {id: "20260101T220000-ab12", path: "overview-20260101T220000-ab12.md", source: "overview", title: "Tour of f", author: "agent", created: "2026-01-01T22:00:00Z", anchors: 1};
const noted = {id: "f2", path: "a/b.go", source: "worktree", notes: 2};
const plain = {id: "f3", path: "c.txt", source: "worktree"};
const rows = switcherRows([ov, noted, plain], "", now);
console.log([rows[0].meta, rows[1].meta, rowAge("", now), rowAge("2025-01-01T00:00:00Z", now),
  deleteAction(ov), deleteAction(noted), deleteAction(plain),
  deletePrompt(ov), deletePrompt(noted), deletePrompt({id: "f4", path: "d.go", source: "worktree", notes: 1}), deletePrompt(plain)].join("|"));
`)
	want := "overview · 1 anchors · agent · 2h||" + "|1y|overview|notes||Delete this overview?\nTour of f|Delete the 2 notes on a/b.go?|Delete the note on d.go?|"
	if out != want {
		t.Fatalf("got  %q\nwant %q", out, want)
	}
}

func TestViewerFootChipKeepsTheBackslash(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "viewer.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if strings.Contains(src, `>ctrl+\ open files<`) || !strings.Contains(src, `>ctrl+\\ open files<`) {
		t.Error(`viewer.js: the "ctrl+\ open files" chip loses its backslash in the template literal`)
	}
}
