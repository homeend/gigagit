package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// statusRun is what a shift+click on the status file list marks: the paths
// between two rows, either direction, across sections, without the rows a
// filter hides or the conflict rows no batch action may touch.
const fileMarksHarness = `
import { statusRun } from "./marks.mjs";
const rows = [
  { path: "a", section: "changes" },
  { path: "b", section: "changes" },
  { path: "c", section: "conflicts" },
  { path: "d", section: "staged" },
  { path: "e", section: "untracked" },
];
console.log(JSON.stringify({
  down: statusRun(rows, 0, 1, () => true),
  up: statusRun(rows, 1, 0, () => true),
  same: statusRun(rows, 3, 3, () => true),
  across: statusRun(rows, 0, 4, () => true),
  conflict: statusRun(rows, 2, 2, () => true),
  filtered: statusRun(rows, 0, 4, (f) => f.path !== "b"),
  clamped: statusRun(rows, -1, 9, () => true),
}));
`

// The row just clicked is under the pointer: its mark must beat polish.css's
// #files-list li:hover:not(.sel):not(.sect), or the click looks like nothing.
func TestMarkedFileRowBeatsHover(t *testing.T) {
	t.Parallel()
	css, err := os.ReadFile(filepath.Join("static", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), "#files-list li.marked:hover:not(.sel):not(.sect)") {
		t.Error("style.css lost the marked row's hover form")
	}
}

func TestFileMarkRun(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	mod := jsFunc(t, "files.js", "statusRun") + "\nexport { statusRun };\n"
	dir := t.TempDir()
	for name, body := range map[string]string{"marks.mjs": mod, "run.mjs": fileMarksHarness} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got map[string][]string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("parse %q: %v", out, err)
	}
	want := map[string][]string{
		"down":     {"a", "b"},
		"up":       {"a", "b"},
		"same":     {"d"},
		"across":   {"a", "b", "d", "e"},
		"conflict": {},
		"filtered": {"a", "d", "e"},
		"clamped":  {"a", "b", "d", "e"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("statusRun:\n got %v\nwant %v", got, want)
	}
}
