package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The UI moves FIRST: before the server answers, the diff already shows what
// staging (or unstaging) the chosen rows does to it. optimisticRows is that
// prediction, and it has to be exact — a wrong guess would jump when the real
// diff lands. In the unstaged diff (index → working tree) a staged modified or
// added row becomes context, a staged removal disappears, and the index side
// (left) renumbers; in the staged diff (HEAD → index) it is the mirror.
const optimisticHarness = `
import * as O from "./opt.mjs";
const rows = [
  { kind: "same",   left: "a",  right: "a",  left_no: 1, right_no: 1 },
  { kind: "change", left: "b",  right: "B",  left_no: 2, right_no: 2, hunk: 0, hr: 0 },
  { kind: "del",    left: "c",  left_no: 3, hunk: 0, hr: 1 },
  { kind: "add",    right: "D", right_no: 3, hunk: 0, hr: 2 },
  { kind: "same",   left: "e",  right: "e",  left_no: 4, right_no: 4 },
];
const show = (rs) => rs.map((r) => [r.kind, r.left ?? "", r.left_no ?? 0, r.right ?? "", r.right_no ?? 0, r.hunk ?? "-"].join(",")).join(" | ");
const out = {};
out.stageChange = show(O.optimisticRows(rows, "unstaged", new Set(["0:0"])));
out.stageDel    = show(O.optimisticRows(rows, "unstaged", new Set(["0:1"])));
out.stageAdd    = show(O.optimisticRows(rows, "unstaged", new Set(["0:2"])));
out.unstageChange = show(O.optimisticRows(rows, "staged", new Set(["0:0"])));
out.unstageDel    = show(O.optimisticRows(rows, "staged", new Set(["0:1"])));
out.unstageAdd    = show(O.optimisticRows(rows, "staged", new Set(["0:2"])));
console.log(JSON.stringify(out));
`

func TestOptimisticRowsPredictTheDiffAfterTheAction(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	mod := jsFunc(t, "files.js", "optimisticRows") + "\nexport { optimisticRows };\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opt.mjs"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(optimisticHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
		t.Fatalf("not the harness JSON: %v\n%s", err, out)
	}
	// Every row loses its tags (they name the OLD bytes; the re-read re-tags).
	want := map[string]string{
		// the index takes B: the row is context, numbers unchanged
		"stageChange": "same,a,1,a,1,- | same,B,2,B,2,- | del,c,3,,0,- | add,,0,D,3,- | same,e,4,e,4,-",
		// the index drops c: the row is gone and the index side renumbers
		"stageDel": "same,a,1,a,1,- | change,b,2,B,2,- | add,,0,D,3,- | same,e,3,e,4,-",
		// the index gains D: context, and every later index line moves down one
		"stageAdd": "same,a,1,a,1,- | change,b,2,B,2,- | del,c,3,,0,- | same,D,4,D,3,- | same,e,5,e,4,-",
		// HEAD → index: unstaging puts HEAD's b back in the index
		"unstageChange": "same,a,1,a,1,- | same,b,2,b,2,- | del,c,3,,0,- | add,,0,D,3,- | same,e,4,e,4,-",
		// HEAD's c returns to the index: context, the index side renumbers
		"unstageDel": "same,a,1,a,1,- | change,b,2,B,2,- | same,c,3,c,3,- | add,,0,D,4,- | same,e,4,e,5,-",
		// the index drops D: gone, the index side renumbers
		"unstageAdd": "same,a,1,a,1,- | change,b,2,B,2,- | del,c,3,,0,- | same,e,4,e,3,-",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s =\n  %q\nwant\n  %q", k, got[k], w)
		}
	}
}
