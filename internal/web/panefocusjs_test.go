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
		{"index.html", `<span>tab focus</span>`, "the footer advertises the key"},
		{"index.html", `tab / shift+tab · ←/→`, "the ? help advertises the key"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
}
