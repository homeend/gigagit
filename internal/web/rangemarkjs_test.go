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
import { extendRange, rangeRows, noteBars } from "./rm.mjs";
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
  { old: [...bars.old], new: [...bars.new] },
]));
`

func TestRangeMarkDecisions(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	names := []string{"extendRange", "rangeRows", "noteBars"}
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
		// one-line and file-level notes have no bar
		`{"old":[1,2],"new":[3,4,5]}` +
		`]`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Errorf("range decisions:\n got %s\nwant %s", got, want)
	}
}
