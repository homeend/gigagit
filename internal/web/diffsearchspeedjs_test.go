package web

import (
	"strings"
	"testing"
)

// Typing a query in the diff pane re-rendered the whole table on every key
// (a single diff) or every file that held a hit (a stack), and folded every
// line again: CHANGELOG.md's 13 000 rows took 2–4 s a key. The blame fix's
// recipe: the folds are cached per row, a key only re-finds, and the hits
// are painted cell by cell near the screen (paintwin.js windowPainter).
func TestDiffSearchPaintsNearTheScreen(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	stack := readStatic(t, "stackview.js")
	fnBody := func(src, sig string) string {
		i := strings.Index(src, sig)
		if i < 0 {
			t.Fatalf("%s is gone", sig)
		}
		body := src[i:]
		if j := strings.Index(body, "\n}\n"); j > 0 {
			body = body[:j]
		}
		return body
	}
	for _, c := range []struct{ src, want, why string }{
		{files, `const diffPaint = windowPainter({`, "the diff pane paints hits only near the screen"},
		{files, `render: () => (conflictPick ? cfSearchRender() : state.stack ? refindStack() : refindDiffHits()),`, "a key re-finds and repaints the hits; it never re-renders the diff"},
		{files, `fold: foldOf(`, "each searched line carries its cached fold"},
		{files, `paintHitRows(i);`, "a step paints the hit's row first, wherever it is"},
		{files, `takeRecords()`, "a repaint right after a re-render sees the new rows (the observer is async)"},
		{readStatic(t, "paintwin.js"), `markPainted(i) {`, "rows kept from before a rebuild keep their paint on record"},
	} {
		if !strings.Contains(c.src, c.want) {
			t.Errorf("missing %q — %s", c.want, c.why)
		}
	}
	body := fnBody(stack, "function refindStack() {")
	if strings.Contains(body, "repaintSlot(") {
		t.Error("refindStack re-renders whole files on a key; it must repaint the hits near the screen")
	}
	if !strings.Contains(body, "diffPaint") && !strings.Contains(body, "repaintDiffHits(") {
		t.Error("refindStack does not repaint through the near-screen painter")
	}
}
