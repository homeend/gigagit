package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A drag over a diff copies ONE version (plan 4d, D2): the side the drag
// started on. The other side's cells drop out, and so does the empty half of
// a row that has no line on this side; a cell with no side (a unified context
// row, a one-pane row) belongs to both. Cells arrive in document order with
// their text already clipped to the selection; the row kind is the <tr>'s
// class (same / add / del / change).
const sideCopyHarness = `
import { sideCopyText } from "./sidecopy.mjs";
const sbs = [
  { side: "l", kind: "same", text: "ctx" }, { side: "r", kind: "same", text: "ctx" },
  { side: "l", kind: "change", text: "old" }, { side: "r", kind: "change", text: "new" },
  { side: "l", kind: "add", text: "" }, { side: "r", kind: "add", text: "added" },
  { side: "l", kind: "del", text: "gone" }, { side: "r", kind: "del", text: "" },
];
const unified = [
  { side: "", kind: "same", text: "ctx" },
  { side: "l", kind: "del", text: "old" },
  { side: "r", kind: "add", text: "new" },
  { side: "", kind: "same", text: "tail" },
];
const onePane = [{ side: "r", kind: "add", text: "a" }, { side: "r", kind: "add", text: "b" }];
console.log(JSON.stringify({
  sbsNew: sideCopyText(sbs, "r"),
  sbsOld: sideCopyText(sbs, "l"),
  uniNew: sideCopyText(unified, "r"),
  uniOld: sideCopyText(unified, "l"),
  onePane: sideCopyText(onePane, "r"),
  empty: sideCopyText([], "r"),
}));
`

func TestSideCopyTextIsOneVersion(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	mod := jsFunc(t, "files.js", "sideCopyText") + "\nexport { sideCopyText };\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sidecopy.mjs"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(sideCopyHarness), 0o644); err != nil {
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
	want := map[string]string{
		"sbsNew":  "ctx\nnew\nadded",
		"sbsOld":  "ctx\nold\ngone",
		"uniNew":  "ctx\nnew\ntail",
		"uniOld":  "ctx\nold\ntail",
		"onePane": "a\nb",
		"empty":   "",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: got %q, want %q", k, got[k], w)
		}
	}
}
