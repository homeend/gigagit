package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/textdiff"
)

// USER RULING (2026-09-23), reversing plan 3's mapping: in a stack n/p walk
// CHANGE to CHANGE across the whole scroll — the stack is one document — and
// N/P step file to file, which is what they already mean in the single-file
// view. ctrl+↑/↓ keep their narrower meaning (inside the cursor's file).
//
// Plan 3 had made n/p the file step because v.blocks were the header indices;
// the blocks are now the change starts of every file's body, so the stepping,
// wrapping and ordinal machinery is reused exactly as before — one line stream,
// one block list.
func TestStackNStepsChangeToChangeAcrossFiles(t *testing.T) {
	t.Parallel()
	// two files, two changes each
	m, v := navStackModel(t, sameRowsTUI(20, 3, 12), sameRowsTUI(20, 4, 15))
	body := m.diffBodyRows()
	v.focusBlock(0, body)
	if v.curFile() != 0 {
		t.Fatalf("fixture: the first change is in file %d", v.curFile())
	}

	seen := []int{}
	for i := 0; i < 3; i++ {
		u, _ := m.Update(keyMsg("n"))
		m = u.(Model)
		v = m.diffLayer()
		seen = append(seen, v.curFile())
	}
	// changes 2, 3, 4: the second is still file 0, then n CROSSES into file 1
	if seen[0] != 0 || seen[1] != 1 || seen[2] != 1 {
		t.Fatalf("n walked files %v, want [0 1 1] — change to change across the stack", seen)
	}
	if got := v.cur; got != 3 {
		t.Fatalf("after three n the focused change is %d, want 3 (four changes in the stack)", got)
	}
}

// A one-file stack was the bug the user hit: n had nowhere to step, primed a
// wrap and the second press landed on the file header at the top of the
// scroll, dropping the line cursor. Now it walks that file's changes.
func TestStackNWorksInAOneFileStack(t *testing.T) {
	t.Parallel()
	m, v := navStackModel(t, sameRowsTUI(60, 10, 40))
	body := m.diffBodyRows()
	v.focusBlock(0, body)
	first := v.curLine

	u, _ := m.Update(keyMsg("n"))
	nv := u.(Model).diffLayer()
	if nv.curLine == first {
		t.Fatal("n did not move in a one-file stack")
	}
	if nv.wrapArm != wrapNone {
		t.Fatal("n primed a wrap instead of stepping to the file's second change")
	}
	if !nv.lines[nv.curLine].isBody() {
		t.Fatalf("n left the cursor on a non-body line (kind %v) — the header, not a change", nv.lines[nv.curLine].kind)
	}
}

// N/P are no longer inert in a stack: they are the file step n/p used to be.
func TestStackNPStepFiles(t *testing.T) {
	t.Parallel()
	m, v := navStackModel(t, sameRowsTUI(20, 3), sameRowsTUI(20, 4), sameRowsTUI(20, 5))
	body := m.diffBodyRows()
	v.focusBlock(0, body)

	u, _ := m.Update(keyMsg("N"))
	m = u.(Model)
	if f := m.diffLayer().curFile(); f != 1 {
		t.Fatalf("N landed in file %d, want 1", f)
	}
	u, _ = m.Update(keyMsg("N"))
	m = u.(Model)
	if f := m.diffLayer().curFile(); f != 2 {
		t.Fatalf("a second N landed in file %d, want 2", f)
	}
	u, _ = m.Update(keyMsg("P"))
	if f := u.(Model).diffLayer().curFile(); f != 1 {
		t.Fatalf("P landed in file %d, want 1", f)
	}
}

// The in-file walk keeps its own meaning: ctrl+↓ stops at the file's last
// change rather than crossing into the next file (plan 4a's rule).
func TestStackCtrlArrowsStayInsideTheFile(t *testing.T) {
	t.Parallel()
	m, v := navStackModel(t, sameRowsTUI(20, 3, 12), sameRowsTUI(20, 4))
	body := m.diffBodyRows()
	v.focusBlock(0, body)

	for i := 0; i < 4; i++ {
		u, _ := m.Update(keyMsg("ctrl+down"))
		m = u.(Model)
	}
	if f := m.diffLayer().curFile(); f != 0 {
		t.Fatalf("ctrl+↓ crossed into file %d; it walks the cursor's file only", f)
	}
}

// navStackModel opens a commit stack over the given files' rows, all loaded.
func navStackModel(t *testing.T, rows ...[]textdiff.Row) (Model, *diffView) {
	t.Helper()
	v := stackViewOf(t, rows...)
	m := diffModel()
	m.height, m.width = 24, 120
	m = m.pushLayer(v)
	return m, v
}

// A change in a file the stack has never read is still reachable: n unfolds
// and sends for that file and parks the step, exactly as ] does for a search
// hit. Without this, n would silently skip every folded or unfetched file.
func TestStackNStepsIntoAnUnreadFile(t *testing.T) {
	t.Parallel()
	// file 1 has no rows yet (nil = never fetched)
	m, v := navStackModel(t, sameRowsTUI(10, 2), nil)
	body := m.diffBodyRows()
	v.focusBlock(len(v.dispBlocks)-1, body) // the last change the stream holds

	u, cmd := m.Update(keyMsg("n"))
	m = u.(Model)
	v = m.diffLayer()
	if v.stk.hunt == nil || v.stk.hunt.file != 1 || v.stk.hunt.kind != huntChange {
		t.Fatalf("n must park a change hunt on file 1, got %+v", v.stk.hunt)
	}
	if v.stk.files[1].collapsed {
		t.Fatal("the hunted file must be unfolded so its rows can join the stream")
	}
	if cmd == nil {
		t.Fatal("n must send for the unread file")
	}

	// its rows arrive: the step lands on that file's first change
	u2, _ := m.Update(stackFileMsg{gen: v.stk.gen, idx: 1, view: diffViewWith(sameRowsTUI(10, 4), blocksOf(sameRowsTUI(10, 4)))})
	nv := u2.(Model).diffLayer()
	if nv.stk.hunt != nil {
		t.Fatal("the parked step must be consumed when the file arrives")
	}
	if nv.curFile() != 1 {
		t.Fatalf("the step landed in file %d, want the file it opened (1)", nv.curFile())
	}
	if !nv.lines[nv.curLine].isBody() {
		t.Fatal("the step must land on the file's change, not on its header")
	}
}

// Any other key abandons a parked change step — and ]/[ count as "other",
// because a hunt belongs to the gesture that parked it (design D5).
func TestAPendingChangeStepIsCancelledByAnyOtherKey(t *testing.T) {
	t.Parallel()
	m, v := navStackModel(t, sameRowsTUI(10, 2), nil)
	body := m.diffBodyRows()
	v.focusBlock(len(v.dispBlocks)-1, body)
	u, _ := m.Update(keyMsg("n"))
	m = u.(Model)
	if m.diffLayer().stk.hunt == nil {
		t.Fatal("fixture: n must have parked a hunt")
	}
	u2, _ := m.Update(keyMsg("]"))
	if u2.(Model).diffLayer().stk.hunt != nil {
		t.Fatal("] must abandon a change step parked by n")
	}
}
