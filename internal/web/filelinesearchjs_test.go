package web

import (
	"strings"
	"testing"
)

// The F preview and the file viewer show a file as line rows through two
// renderers; their search is ONE host (linesearch.js, the TUI's contentPopup
// search): a key re-finds over cached folds and repaints near the screen.
// The viewer used to re-render the whole file on every key (seconds on
// CHANGELOG.md). Both show one file at half the width, centred.
func TestFileLineSearchIsShared(t *testing.T) {
	t.Parallel()
	viewer := readStatic(t, "viewer.js")
	for _, c := range []struct{ src, want, why string }{
		{readStatic(t, "linesearch.js"), `function lineSearchHost(`, "the one search host over line rows"},
		{readStatic(t, "wtfinder.js"), `const pvLines = lineSearchHost({`, "the F preview searches through it"},
		{viewer, `const viewerLines = lineSearchHost({`, "the file viewer searches through it too"},
		{viewer, `viewerLines.render(); // the hits alone, near the screen: the lines stay`, "a key repaints the hits, never the file"},
		{viewer, "const box = $(\"viewer-box\"), one = !split && !view.ov, file = !view.ov;\n  box.classList.toggle(\"vhalf\", one);", "one file: half width, centred"},
		{readStatic(t, "style.css"), `#viewer-box.vhalf { width: min(max(50vw, min(94vw, 760px)), `, "…the width rule"},
		{readStatic(t, "style.css"), `#panes.wtf.wtf.wtf:not(.wtf-full) #diff-pane > #wtf-preview:not(.hidden) {`, "the F preview: half the area, centred (m: the whole width)"},
	} {
		if !strings.Contains(c.src, c.want) {
			t.Errorf("missing %q — %s", c.want, c.why)
		}
	}
	i := strings.Index(viewer, "function boxWidth() {")
	if i < 0 {
		t.Fatal("boxWidth is gone")
	}
	if body := viewer[i : i+300]; strings.Contains(body, `$("viewer-box").clientWidth`) {
		t.Error("boxWidth measures the box: a half-width file would stop overviews from splitting")
	}
	// The title is cut to the box's width: the width must be set first.
	if j := strings.Index(viewer, "function paintTitle() {"); j < 0 || !strings.Contains(viewer[j:j+200], `fitBox(); // the box's width first`) {
		t.Error("paintTitle measures the title before the box takes its half width (a path cut at the end)")
	}
	if !strings.Contains(viewer, `case "w": cycleTextMode(); viewerLines.queue(); break;`) {
		t.Error("w re-wraps the rows without repainting the hits that come near")
	}
	if strings.Contains(viewer, "function goToViewerHit(") {
		t.Error("the viewer keeps its own hit landing; it must be linesearch.js's")
	}
}
