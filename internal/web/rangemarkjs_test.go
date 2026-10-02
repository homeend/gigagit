package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The decisions behind the marked range of a diff, run under node: how a
// shift+click extends the mark, which rows a range bands (and what its block
// reads), and which lines get a ranged note's bar.
const rangeMarkHarness = `
import { extendRange, rangeRows, noteBars, firstHeldLine } from "./rm.mjs";
const rows = [
  { left_no: 1, right_no: 1, left: "a", right: "a" },
  { left_no: 2, right_no: 2, left: "b", right: "b" },
  { left_no: 3, right_no: 0, left: "gone", right: "" },
  { left_no: 0, right_no: 3, left: "", right: "added" },
  { left_no: 4, right_no: 4, left: "d", right: "d" },
  { left_no: 5, right_no: 5, left: "e", right: "e" },
];
const bars = noteBars([
  { side: "new", line: 2, range: [2, 2] },
  { side: "new", line: 5, range: [3, 5] },
  { side: "old", line: 2, range: [1, 2] },
  { side: "new", line: 0, range: [0, 0], file_level: true },
  { side: "new", line: 9 },
]);
console.log(JSON.stringify([
  extendRange(null, { side: "new", no: 4 }),
  extendRange({ side: "new", no: 2 }, { side: "new", no: 5 }),
  extendRange({ side: "new", no: 5 }, { side: "new", no: 2 }),
  extendRange({ side: "old", no: 2 }, { side: "new", no: 5 }),
  rangeRows(rows, "new", 2, 4),
  rangeRows(rows, "old", 2, 4),
  rangeRows(rows, "new", 5, 7),
  rangeRows(rows, "new", 8, 9),
  rangeRows(rows, "old", 3, 3),
  { old: [...bars.old], new: [...bars.new] },
  [firstHeldLine(rows, "new", 0, 9), firstHeldLine(rows, "new", 3, 4), firstHeldLine(rows, "old", 3, 9),
   firstHeldLine(rows, "new", 6, 9), firstHeldLine(null, "new", 1, 2)],
]));
`

func TestRangeMarkDecisions(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	names := []string{"extendRange", "rangeRows", "noteBars", "firstHeldLine"}
	var mod strings.Builder
	for _, n := range names {
		mod.WriteString(jsFunc(t, "files.js", n) + "\n")
	}
	mod.WriteString("export { " + strings.Join(names, ", ") + " };\n")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rm.mjs"), []byte(mod.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(rangeMarkHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := `[` +
		// no mark: the one line
		`{"side":"new","first":4,"last":4},` +
		`{"side":"new","first":2,"last":5},` +
		// backwards
		`{"side":"new","first":2,"last":5},` +
		// a mark on the other side does not extend
		`{"side":"new","first":5,"last":5},` +
		// the band is contiguous over rows: the del-only row sits inside it
		`{"idx":[1,2,3,4],"block":["b","added","d"]},` +
		`{"idx":[1,2,3,4],"block":["b","gone","d"]},` +
		// lines 6 and 7 are not in the diff: a short block
		`{"idx":[5],"block":["e"]},` +
		`{"idx":[],"block":[]},` +
		// a line only the old side has
		`{"idx":[2],"block":["gone"]},` +
		// one-line and file-level notes have no bar
		`{"old":[1,2],"new":[3,4,5]},` +
		// a range link lands on the first of its lines the diff holds
		`[1,3,3,0,0]` +
		`]`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Errorf("range decisions:\n got %s\nwant %s", got, want)
	}
}

// The viewer's band for a range link: none for one line or a range starting
// past the end, the end clamped to the file's last line.
func TestViewerRangeDecisions(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	dir := t.TempDir()
	src := jsFunc(t, "viewer.js", "viewerRange") + "\nconsole.log(JSON.stringify([viewerRange(2, 4, 10), viewerRange(2, 2, 10), viewerRange(9, 14, 10), viewerRange(11, 14, 10), viewerRange(0, 3, 10)]));\n"
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	if got, want := strings.TrimSpace(string(out)), `[{"start":2,"end":4},null,{"start":9,"end":10},null,null]`; got != want {
		t.Errorf("viewerRange: got %s, want %s", got, want)
	}
}

// The gestures and their guards, pinned at the source: shift+click on a line
// number is the range gesture and nothing else — the note anchor and the
// staging selection both stand aside — and esc drops the range first.
func TestRangeGestureGuards(t *testing.T) {
	t.Parallel()
	files, keys, viewer, live := readStatic(t, "files.js"), readStatic(t, "keys.js"), readStatic(t, "viewer.js"), readStatic(t, "live.js")
	for _, c := range []struct{ src, want, why string }{
		{files, `return e.shiftKey && !e.ctrlKey && !e.metaKey && !!e.target.closest && !!e.target.closest("table.diff td.no");`, "the range gesture is shift+click on a line number"},
		{files, "if (rangeGesture(e)) return; // a line-number shift+click marks a range, never a staging row", "the staging click must skip the range gesture"},
		{files, "if (rangeGesture(e)) return; // marking a range of lines leaves the staging selection alone", "the outside-click clear must skip the range gesture"},
		{files, "  if (rangeGesture(e)) return;\n  const handle = e.target.closest(\".notetitle[data-collapse]\");", "the note-anchor click must skip the range gesture"},
		{files, "if (!keepRange) clearDiffRange();", "a plain mark drops the range"},
		{files, "$(\"diff-body\").addEventListener(\"dblclick\", (e) => {\n  if (rangeGesture(e)) return;", "a shift+double-click on a number must not stage"},
		{files, "const rng = notesOn ? nc.range || null : null;", "the band is the open diff's: the history overlay renders through diffHTML too"},
		{files, "for (const t of $(\"diff-body\").querySelectorAll(\"tr.rng-l, tr.rng-r\")) t.classList.remove(\"rng-l\", \"rng-r\");", "dropping the band must not repaint: callers hold rows"},
		{files, "const unified = tr.dataset.lno === undefined;", "one text column: the range stays on the mark's side"},
		{files, "`copy gg link to lines ${rg.first}-${rg.last}`", "the menu names the range"},
		{keys, "if (e.key === \"Escape\" && clearDiffRange()) return;\n  if (e.key === \"Escape\" && clearRowSelection()) return;", "esc drops the range before the staging selection"},
		{viewer, `case "Escape": if (!clearViewerRange()) closeViewer(`, "esc drops the viewer's range before closing"},
		{viewer, `"copy file link (lines " + rg.start + "-" + rg.end + ")"`, "the viewer's menu names the range"},
		{viewer, `const block = view.src === "worktree" ? view.lines.slice(rg.start - 1, rg.end).map((l) => l.text) : null;`, "the viewer fingerprints only the disk's text"},
		{live, "if (r && r.ok && s.end_line > s.line) markViewerRange(s.line, s.end_line);", "a content range link bands the viewer"},
		{live, "setDiffRange(tr, { side, first: line, last: s.end_line })", "a range link bands the diff"},
		{live, "landStackLine(s.file, side, s.line, s.end_line || 0)", "a range link bands the stack"},
	} {
		if !strings.Contains(c.src, c.want) {
			t.Errorf("%s — missing %q", c.why, c.want)
		}
	}
}
