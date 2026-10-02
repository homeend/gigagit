package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Where the note key writes with a range marked, run under node: the range's
// side and lines (the note under the last one), and no range at all for one
// line — that is the ordinary note on the clicked row.
const rangeNoteHarness = `
import { rangeAnchor, noteAnchorLabel } from "./rn.mjs";
console.log(JSON.stringify([
  rangeAnchor({ side: "new", first: 3, last: 5 }),
  rangeAnchor({ side: "old", first: 7, last: 8 }),
  rangeAnchor({ side: "new", first: 4, last: 4 }),
  rangeAnchor(null),
  noteAnchorLabel({ side: "new", first: 3, no: 5 }),
  noteAnchorLabel({ side: "old", no: 5 }),
  noteAnchorLabel({ side: "new", first: 5, no: 5 }),
]));
`

func TestRangeNoteAnchor(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	names := []string{"rangeAnchor", "noteAnchorLabel"}
	var mod strings.Builder
	for _, n := range names {
		mod.WriteString(jsFunc(t, "files.js", n) + "\n")
	}
	mod.WriteString("export { " + strings.Join(names, ", ") + " };\n")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rn.mjs"), []byte(mod.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "h.mjs"), []byte(rangeNoteHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "h.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := `[{"side":"new","first":3,"no":5},{"side":"old","first":7,"no":8},null,null,` +
		`"new lines 3-5","old line 5","new line 5"]`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// The wiring the helpers cannot see: the prompt reads the file that HOLDS the
// band (not the stack's cursor slot), posts the range's first line, refuses a
// preview's old side without falling forward, and drops the band only from
// the write's success hook.
func TestRangeNoteWiring(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	for _, pin := range []string{
		"const ranged = rangeAnchor(rd && rd.range);",
		"const ad = ranged ? rd : activeDiff();",
		"let at = ranged || ad.row || firstChangedRow(scope);",
		"const fwd = !ranged && !ad.row && firstNewSideRow(scope);",
		"first: ranged ? at.first : 0,",
		"ranged ? clearDiffRange : null,",
		"if (saved) saved();",
	} {
		if !strings.Contains(files, pin) {
			t.Errorf("files.js lost %q", pin)
		}
	}
	if !strings.Contains(readStatic(t, "stackview.js"), "const s = st.slots.find((o) => o.range);") {
		t.Error("stackview.js: rangeDiff no longer looks for the slot that holds the range")
	}
}
