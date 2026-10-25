package web

import (
	"strings"
	"testing"
)

const edgePureStart = "// --- diff edge step (pure; guarded against Go) ---"
const edgePureEnd = "// --- end diff edge step ---"

// home/end at a diff's edge arm a file step and the next press takes it (the
// TUI's fileArm): away from the edge they scroll there; with no file that way
// they say so.
func TestEdgeStepJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "files.js", edgePureStart, edgePureEnd, `
console.log(JSON.stringify([
  edgeStep("End", false, 0, true), edgeStep("End", true, 0, true), edgeStep("End", true, 1, true),
  edgeStep("Home", true, 1, true), edgeStep("Home", true, -1, true), edgeStep("End", true, 0, false),
]));
`)
	want := `[{"scroll":true,"arm":0},{"arm":1,"notice":"end again → next file"},{"step":1,"arm":0},` +
		`{"arm":-1,"notice":"home again → previous file"},{"step":-1,"arm":0},{"arm":0,"notice":"no next file"}]`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// Keys act on the focused widget (the TUI's routing): w cycles long lines
// only where a diff shows them; A searches pull requests only from the Pull
// requests section; j/k in a diff move its line cursor, not the file list.
func TestFocusLocalKeys2(t *testing.T) {
	t.Parallel()
	checks := []struct{ file, want, why string }{
		{"keys.js", `if (state.layout !== "diff") opLine("w changes long lines in a diff`, "w acts on the diff only"},
		{"keys.js", `case "textmode": if (state.layout !== "diff") opLine(`, "the footer chip follows the key"},
		{"keys.js", `if (state.prFocus && window.__ggFocusPRSearch && window.__ggFocusPRSearch()) e.preventDefault();`, "A only from the Pull requests section"},
		{"prs.js", `state.prFocus = !!(e.target.closest && e.target.closest("#prs-header, #pr-search-box, #prs-list"));`, "a click there focuses the section"},
		{"keys.js", `if (lineCursorKey(e)) return;`, "j/k in a diff move the line cursor"},
		{"files.js", `function lineCursorKey(e) {`, "the diff's line cursor"},
		{"files.js", `const a = edgeStep(e.key, atEdge, fileArm, hasFile);`, "home/end at the edge step files"},
		{"keys.js", `if (e.key !== "Home" && e.key !== "End") disarmFileStep();`, "any other key drops the arm"},
	}
	for _, c := range checks {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	k := readStatic(t, "keys.js")
	l, j := strings.Index(k, "if (lineCursorKey(e)) return;"), strings.Index(k, `if (e.key === "j" || e.key === "ArrowDown") {`)
	if l < 0 || j < 0 || l > j {
		t.Error("keys.js: lineCursorKey must run before j/k walk the focused list")
	}
}
