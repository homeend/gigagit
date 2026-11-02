package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const paneFocusPureStart = "// --- pane focus (pure; guarded against Go) ---"
const paneFocusPureEnd = "// --- end pane focus ---"

// tab wraps round the controls on screen, ←/→ stop at the edges, and a control
// that left the screen hands over to the first one.
func TestPaneFocusStopsJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "panefocus.js", paneFocusPureStart, paneFocusPureEnd, `
const o = ["side", "commits", "files"];
console.log([
  nextStop(o, "side", 1), nextStop(o, "files", 1), nextStop(o, "side", -1), nextStop(o, "diff", 1), nextStop([], "side", 1),
  edgeStop(o, "side", -1), edgeStop(o, "commits", 1), edgeStop(o, "files", 1),
  sideTarget("ArrowDown", 0, 3, 2), sideTarget("k", 0, 3, 2), sideTarget("End", 0, 3, 2), sideTarget("PageDown", 2, 3, 2), sideTarget("x", 1, 3, 2),
].join("|"));
`)
	want := "commits|side|files|side|side|side|files|files|1|0|2|2|-1"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// ↑ on a context menu's first row goes to its last, ↓ on the last to the first.
func TestMenuStepWrapsJS(t *testing.T) {
	t.Parallel()
	src := jsFunc(t, "layers.js", "menuStep") + `
console.log([menuStep(0, 3, -1), menuStep(2, 3, 1), menuStep(0, 3, 1), menuStep(2, 3, -1), menuStep(-1, 3, 1), menuStep(-1, 3, -1), menuStep(0, 1, 1), menuStep(0, 1, -1)].join(" "));
`
	if got, want := strings.TrimSpace(runNode(t, src)), "2 0 1 1 0 2 0 0"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// alt+letter never opens the browser's menu, and AltGr or option still types.
func TestAltStaysHereJS(t *testing.T) {
	t.Parallel()
	src := jsFunc(t, "keys.js", "altStaysHere") + `
const field = { closest: () => ({}) };
const page = { closest: () => null };
const ev = (code, key, target, o = {}) => ({ altKey: true, ctrlKey: false, metaKey: false, code, key, target, getModifierState: () => !!o.altGr, ...o });
console.log([
  altStaysHere(ev("KeyB", "b", page)),                     // alt+b: Bookmarks
  altStaysHere(ev("KeyB", "B", page)),                     // alt+shift+b
  altStaysHere(ev("KeyB", "b", field)),                    // in a field too
  altStaysHere(ev("Digit1", "1", page)),                   // alt+1
  altStaysHere(ev("KeyB", "и", page)),                     // another layout's letter, on the page
  altStaysHere(ev("KeyA", "ą", field, { altGr: true })),   // AltGr types
  altStaysHere(ev("KeyA", "å", field)),                    // macOS option types
  altStaysHere(ev("KeyA", "a", page, { ctrlKey: true })),  // AltGr reported as ctrl+alt
  altStaysHere(ev("KeyA", "a", page, { altKey: false })),  // no alt
  altStaysHere(ev("ArrowLeft", "ArrowLeft", page)),        // not a letter or digit
].join(" "));
`
	want := "true true true true true false false false false false"
	if got := strings.TrimSpace(runNode(t, src)); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// The wiring the bug was: tab and the keys follow the app's focus, a page
// button never holds the browser's, a layer keeps tab to itself.
func TestPaneFocusWiring(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"keys.js", `cycleFocus(e.shiftKey ? -1 : 1);`, "tab moves the app's focus, not the browser's"},
		{"keys.js", `pageButtonKey(e);`, "enter never presses a page button that holds the browser's focus"},
		{"keys.js", `addEventListener("mousedown", keepButtonsUnfocused, true)`, "a mouse press leaves no button focused"},
		{"keys.js", `addEventListener("pointerdown", (e) => {`, "a click focuses the control under it"},
		{"keys.js", `if (target === "side" && sideKey(e)) return;`, "a focused sidebar takes its keys first"},
		{"keys.js", "if (diffKeys) {\n    if (diffScrollKey(e)) return;", "the file list beside a diff takes the arrows when focused"},
		{"layers.js", `if (e.key === "Tab") return layerTabKey(top, e);`, "tab stays inside the top layer"},
		{"panefocus.js", `const on = state.consoleUp ? "" : STOP_PANE[focusTarget()];`, "a shown console has the keys: no control behind it looks focused"},
		{"panefocus.js", `if (landed) setSideRow(landed);`, "the landing mark and the sidebar cursor are one row"},
		{"keys.js", `!e.altKey && e.code === "KeyP") e.preventDefault();`, "ctrl+p never reaches the browser's print, on any surface or layout"},
		{"layers.js", `to = menuStep(cur, rows.length, -1);`, "↑ wraps round a context menu"},
		{"panefocus.js", `new ResizeObserver(paintRing)`, "the focus ring follows its control"},
		{"style.css", `#focus-ring { position: fixed; z-index: 3;`, "the ring paints over a pane's sticky headers (z 2) and under overlays (10+)"},
		{"console.js", "focusBranches();\n  landWorktree(dir);", "alt+q's plain stop gives the keys to the Branches list"},
		{"console.js", "focusBranches();\n  if (pathKey(st.page)", "alt+s's plain stop gives the keys to the Branches list"},
		{"console.js", `!!top || state.layout !== "list" || !here`, "alt+w's first press shows the worktree in Branches, as the TUI's worktreeShownInBranches"},
		{"keys.js", `if (altStaysHere(e)) e.preventDefault();`, "alt+letter never opens the browser's menu, on any surface"},
		{"index.html", `<span>tab focus</span>`, "the footer advertises the key"},
		{"index.html", `tab / shift+tab · ←/→`, "the ? help advertises the key"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
}

// Every walk goes down the Branches list (the TUI's worktreeOrder), not the
// Worktrees section's own sort; what the list does not show comes last.
func TestBranchesWalkOrderJS(t *testing.T) {
	t.Parallel()
	src := jsFunc(t, "walk.js", "branchesWalkOrder") + `
const rows = [
  { path: "/r", branch: "main" }, { path: "/r/a", branch: "alpha" }, { path: "/r/z", branch: "zeta" },
  { path: "/r/d", branch: "" }, { path: "/r/b", branch: "", bare: true },
];
console.log(branchesWalkOrder(["zeta", "x", "alpha", "main"], rows).join(" "));
console.log(branchesWalkOrder(["alpha"], rows).join(" "));
`
	got := strings.TrimSpace(runNode(t, src))
	want := "/r/z /r/a /r /r/d\n/r/a /r /r/z /r/d"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// A console's selection survives the frames, and ctrl+c copies it rather than
// interrupting the program; with nothing selected ctrl+c stays the program's.
func TestConsoleSelectionWiring(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "console.js"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"if (selDrag || gridSelection()) {", "if (copyKey(e)) return true;", `e.code !== "KeyC"`, "dropSelection(); // typing goes back to the live screen"} {
		if !strings.Contains(s, want) {
			t.Errorf("console.js: missing %q", want)
		}
	}
}
