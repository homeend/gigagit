package web

import (
	"strings"
	"testing"
)

// The range follow-ups the helpers cannot see, pinned in the source.
func TestRangeMinorsWiring(t *testing.T) {
	t.Parallel()
	files := readStatic(t, "files.js")
	view := readStatic(t, "stackview.js")
	live := readStatic(t, "live.js")
	viewer := readStatic(t, "viewer.js")
	for _, c := range []struct{ src, pin, why string }{
		{files, "rngRows.has(r) && (!only || only === rng.side)", "unified: a one-side row wears the band only on the band's side"},
		{files, "`<tr class=\"same${curClsBoth(r)}${attnClsBoth(r)}${markCls(r)}\"${anchor(\"new\", r.right_no)}", "unified: a context row keeps an old-side mark across a repaint"},
		{files, "repaintStackSlots(touched);", "a stack repaints only the files whose band changed"},
		{files, "are not all in this diff", "c refuses a range the diff holds only in part"},
		{files, "\"notes in a compare anchor on the new side\"", "a commit pair's old side is refused in compare words"},
		{view, "if (slot.diff) slot.range = null;", "a re-read slot diff drops its band"},
		{view, "at = firstHeldLine((s.diff || {}).rows, side, line, end);", "a stack lands a range on the first line it holds"},
		{live, "at = firstHeldLine((state.lastDiff || {}).rows, side, line, s.end_line);", "a range link lands on the first line the diff holds"},
		{viewer, "if (!getSelection().isCollapsed) return;", "viewer: a text drag is not a click"},
		{viewer, "if (view.range) view.range = viewerRange(view.range.start, view.range.end, view.lines.length);", "viewer: the band is clamped when the file shrinks"},
	} {
		if !strings.Contains(c.src, c.pin) {
			t.Errorf("%s: lost %q", c.why, c.pin)
		}
	}
	// The drag guard sits BEFORE the cursor moves and the band is cleared.
	guard, clear := strings.Index(viewer, "if (!getSelection().isCollapsed) return;"), strings.Index(viewer, "if (clearViewerRange()) return;")
	if guard < 0 || clear < guard {
		t.Error("viewer.js: the drag guard must come before the band is cleared")
	}
}
