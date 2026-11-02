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

// The selection gestures of the status file list, step by step.
const markClickHarness = `
import { markClick } from "./marks.mjs";
const rows = ["a", "b", "c", "d", "e"].map((path) => ({ path, section: "changes" }));
rows[3] = { path: "d", section: "conflicts" };
const all = () => true;
const fresh = () => ({ marked: new Set(), cursor: 0, anchor: null, off: null });
const steps = (s, ...cs) => {
  const out = [];
  for (const [i, m] of cs) {
    markClick(s, rows, i, m, all);
    out.push([...s.marked].sort().join("") + "@" + s.cursor + (s.off === s.cursor ? "off" : ""));
  }
  return out;
};
const C = { ctrl: true }, S = { shift: true }, P = {};
console.log(JSON.stringify({
  plainClears: steps(fresh(), [0, P], [1, C], [4, P]),
  ctrlAdds: steps(fresh(), [0, P], [1, C], [2, C]),
  ctrlRemoves: steps(fresh(), [0, P], [1, C], [1, C], [0, C]),
  ctrlSelfOffOn: steps(fresh(), [0, P], [0, C], [0, C]),
  ctrlAfterOff: steps(fresh(), [0, P], [0, C], [1, C]),
  ctrlConflict: steps(fresh(), [0, P], [3, C]),
  shiftRun: steps(fresh(), [0, P], [2, S]),
  shiftShrinks: steps(fresh(), [0, P], [2, S], [1, S]),
  shiftOverturns: steps(fresh(), [1, P], [2, S], [0, S]),
  shiftReplaces: steps(fresh(), [0, P], [4, C], [1, P], [2, S]),
  shiftSkipsConflict: steps(fresh(), [2, P], [4, S]),
  shiftFromCtrl: steps(fresh(), [0, P], [2, C], [4, S]),
  ctrlShiftAdds: steps(fresh(), [0, P], [1, C], [4, { ctrl: true, shift: true }]),
}));
`

func TestFileMarkClicks(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	mod := jsFunc(t, "files.js", "statusRun") + "\n" + jsFunc(t, "files.js", "markClick") + "\nexport { markClick };\n"
	dir := t.TempDir()
	for name, body := range map[string]string{"marks.mjs": mod, "run.mjs": markClickHarness} {
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
		"plainClears":        {"@0", "ab@1", "@4"},
		"ctrlAdds":           {"@0", "ab@1", "abc@2"},
		"ctrlRemoves":        {"@0", "ab@1", "a@1", "@0off"},
		"ctrlSelfOffOn":      {"@0", "@0off", "a@0"},
		"ctrlAfterOff":       {"@0", "@0off", "b@1"},
		"ctrlConflict":       {"@0", "@0"},
		"shiftRun":           {"@0", "abc@2"},
		"shiftShrinks":       {"@0", "abc@2", "ab@1"},
		"shiftOverturns":     {"@1", "bc@2", "ab@0"},
		"shiftReplaces":      {"@0", "ae@4", "@1", "bc@2"},
		"shiftSkipsConflict": {"@2", "ce@4"},
		"shiftFromCtrl":      {"@0", "ac@2", "ce@4"},
		"ctrlShiftAdds":      {"@0", "ab@1", "abce@4"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("markClick:\n got %v\nwant %v", got, want)
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
