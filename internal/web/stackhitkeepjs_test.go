package web

import (
	"strings"
	"testing"
)

// A file read in a stack (the commit's stacked diff, an overview's stacked
// files) re-finds the query over the new rows. It re-found from where
// TYPING began: a backward search wraps to the stack's last hit, so each
// arriving file moved the current hit into itself — off screen, unpainted.
// The TUI re-finds from the cursor (split_stack_pane.go: refindFrom(lines,
// searchPos)); a rebuild now keeps the hit the reader is on, and a section
// redrawn above it keeps it in place on screen.
func TestStackRebuildKeepsTheCurrentHit(t *testing.T) {
	t.Parallel()
	stack, viewer := readStatic(t, "stackview.js"), readStatic(t, "viewer.js")
	for _, c := range []struct{ src, want, why string }{
		{readStatic(t, "inviewsearch.js"), `refind(lines, pos = this.anchor()) {`, "a host can re-find from the hit it keeps"},
		{stack, `s.refind(lines, on ? hitPos(s.hits[s.cur]) : undefined);`, "the diff stack keeps the current hit across a file read"},
		{readStatic(t, "files.js"), `state.stack ? refindStack(false) : refindDiffHits()`, "…only a typed key re-finds from where typing began"},
		{stack, `const pin = curHitBelow(el) ||`, "a section redrawn above the current hit keeps it on screen"},
		{viewer, `viewerSearch.refind(viewerSearch.query ? stackSearchLines() : [], (rebuilt && stackKeep) || undefined);`, "the overview stack keeps it too"},
		{viewer, `if (hit) body.scrollTop += hit.getBoundingClientRect().top - hitTop;`, "…and pins it on screen"},
	} {
		if !strings.Contains(c.src, c.want) {
			t.Errorf("missing %q — %s", c.want, c.why)
		}
	}
}
