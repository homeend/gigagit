package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The F preview with the keys (spec 2026-10-25-finder-preview-focus): the
// TUI's line marks and line cursor, ported as pure functions, plus the
// wiring that hands the keys over.

func runNode(t *testing.T, script string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	out, err := exec.Command(node, "-e", script).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// space marks as the TUI's lineSel.press does: start at the cursor, then
// freeze the far end, then start again; the range follows the cursor only
// while it is loose.
func TestWtSelMarksLikeTheTUIJS(t *testing.T) {
	t.Parallel()
	script := jsFunc(t, "wtfinder.js", "wtSelPress") + "\n" + jsFunc(t, "wtfinder.js", "wtSelBounds") + `
const out = [];
let s = null;
out.push(wtSelBounds(s, 4));        // nothing marked
s = wtSelPress(s, 4);               // start at 4
out.push(wtSelBounds(s, 7));        // loose: follows the cursor down
out.push(wtSelBounds(s, 1));        // …and up, in either order
s = wtSelPress(s, 9);               // freeze at 9
out.push(wtSelBounds(s, 0));        // frozen: the cursor no longer matters
s = wtSelPress(s, 2);               // a third space starts over at 2
out.push(wtSelBounds(s, 2));
console.log(JSON.stringify(out));
`
	if got, want := runNode(t, script), "[null,[4,7],[1,4],[4,9],[2,2]]"; got != want {
		t.Fatalf("marks = %s, want %s", got, want)
	}
}

// alt+↑/↓ moves the line cursor; a cursor scrolled out of the window comes
// back at the window's nearest edge first, without moving (the TUI's
// movePreviewCursor).
func TestWtCursorStepJS(t *testing.T) {
	t.Parallel()
	script := jsFunc(t, "wtfinder.js", "wtCursorStep") + `
console.log(JSON.stringify([
  wtCursorStep(5, 1, 0, 20, 100),   // inside: one down
  wtCursorStep(5, -1, 0, 20, 100),  // inside: one up
  wtCursorStep(0, -1, 0, 20, 100),  // the first line stays
  wtCursorStep(99, 1, 80, 99, 100), // the last line stays
  wtCursorStep(3, 1, 40, 60, 100),  // above the window: lands on its top row
  wtCursorStep(90, -1, 40, 60, 100),// below: lands on its bottom row
  wtCursorStep(0, 1, 0, 0, 0),      // an empty file has no cursor to move
]));
`
	if got, want := runNode(t, script), "[6,4,0,99,40,60,0]"; got != want {
		t.Fatalf("cursor steps = %s, want %s", got, want)
	}
}

func TestFinderPreviewFocusIsWired(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	fj := read("wtfinder.js")
	checks := []struct{ file, want, why string }{
		{"wtfinder.js", `if (pv.focus) return previewKey(e);`, "a focused preview owns every key the list would take"},
		{"wtfinder.js", `case "Enter": focusPreview(); break;`, "enter on the list hands the keys to the preview (the menu is . alone)"},
		{"wtfinder.js", `case ".": menuAtCursor(); break;`, ". keeps the actions"},
		{"wtfinder.js", `case "ArrowRight": case "Tab": focusPreview(); break;`, "→ and tab hand the keys over, as in the TUI"},
		{"wtfinder.js", `preview.addEventListener("mousedown"`, "a click in the preview hands it the keys"},
		{"wtfinder.js", `bindSearchBar("wtf-psearch"`, "the preview mounts the shared search bar"},
		{"wtfinder.js", `lineSearchHost({`, "hits are painted near the screen only (the blame rule, linesearch.js)"},
		{"wtfinder.js", `openGotoLine(`, ": goes to a line"},
		{"wtfinder.js", `<button data-wact="menu">. actions</button>`, "the list's bottom bar: . is the menu"},
		{"wtfinder.js", `<button data-wact="preview">enter → preview</button>`, "the list's bottom bar: enter / → go to the preview"},
		{"layers.js", `function updateFoot(`, "a bar replaced in place, never moved up over a surface on top of F"},
		{"wtfinder.js", `updateFoot("wtf", `, "F repaints its bar in place: a late preview read must not push it over the viewer"},
		{"wtfinder.js", `if (!f) {
    focusList(); // nothing to preview: the list takes the keys back (the TUI's wtCursorMoved)`, "an empty preview never keeps the keys"},
		{"wtfinder.js", `const path = pv.focus && previewPath ? previewPath : (selected() || {}).path;`, "ctrl+] in the preview backgrounds the file shown (the TUI's wtBackgroundPreview)"},
		{"wtfinder.js", `$("wtf-body").blur();`, "the list's keys must not scroll a still-focused preview body"},
		{"style.css", `#wtf-psearch.hidden { display: none; }`, "per-ID hidden rule — the bar would never hide"},
		{"style.css", `#wtf-body .hit`, "hits must paint in the preview (hit CSS is scoped per host)"},
		{"style.css", `#panes.wtf-pv #wtf-ptitle`, "the side holding the keys is shown"},
		{"style.css", `#panes.wtf.wtf.wtf.wtf-full`, "m fills the screen with the preview"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	// enter must no longer open the menu from the list.
	if strings.Contains(fj, `case "Enter": case ".": menuAtCursor(); break;`) {
		t.Error("wtfinder.js: enter still opens the actions menu")
	}
}
