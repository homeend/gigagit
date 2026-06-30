package tui

import (
	"testing"
)

// treeDiffModelBlocks opens a diff over the three-file tree (treeDiffModel) but
// with a real two-change diff so cur can sit on the first/last change block.
// body = 10.
func treeDiffModelBlocks(sel int) Model {
	m := treeDiffModel(sel)
	m.height = 12
	v := diffViewWith(sameRowsTUI(60, 10, 50), []int{10, 50})
	*m.diffLayer() = *v
	return m
}

// TestDiffNStepsNextFileFromLastBlock: on the last change, the first N primes
// (no nav), the second steps to the next file in the tree.
func TestDiffNStepsNextFileFromLastBlock(t *testing.T) {
	m := treeDiffModelBlocks(1) // on a.go
	m.diffLayer().focusBlock(1, m.diffBodyRows())

	u, cmd := m.Update(keyMsg("N")) // first N: prime
	mm := u.(Model)
	if mm.diffLayer().fileArm != fileArmNext {
		t.Fatalf("first N must prime fileArmNext, got %d", mm.diffLayer().fileArm)
	}
	if mm.filesView.sel != 1 || mm.diffTag != "commit:abc:start" || cmd != nil {
		t.Fatal("first N must not navigate")
	}

	u2, cmd2 := mm.Update(keyMsg("N")) // second N: step
	mm2 := u2.(Model)
	if mm2.filesView.sel != 2 || mm2.diffTag != "commit:abc:b.go" {
		t.Fatalf("second N must step to b.go, sel=%d tag=%q", mm2.filesView.sel, mm2.diffTag)
	}
	if cmd2 == nil {
		t.Fatal("stepping must return the loader cmd")
	}
}

// TestDiffPStepsPrevFileFromFirstBlock mirrors N for the previous file.
func TestDiffPStepsPrevFileFromFirstBlock(t *testing.T) {
	m := treeDiffModelBlocks(2) // on b.go
	m.diffLayer().focusBlock(0, m.diffBodyRows())

	u, _ := m.Update(keyMsg("P")) // first P: prime
	mm := u.(Model)
	if mm.diffLayer().fileArm != fileArmPrev {
		t.Fatalf("first P must prime fileArmPrev, got %d", mm.diffLayer().fileArm)
	}
	if mm.filesView.sel != 2 {
		t.Fatal("first P must not navigate")
	}

	u2, _ := mm.Update(keyMsg("P")) // second P: step
	mm2 := u2.(Model)
	if mm2.filesView.sel != 1 || mm2.diffTag != "commit:abc:a.go" {
		t.Fatalf("second P must step to a.go, sel=%d tag=%q", mm2.filesView.sel, mm2.diffTag)
	}
}

// TestDiffNInertOffLastBlock: N anywhere but the last change does nothing.
func TestDiffNInertOffLastBlock(t *testing.T) {
	m := treeDiffModelBlocks(1)
	m.diffLayer().focusBlock(0, m.diffBodyRows()) // first change of two

	u, cmd := m.Update(keyMsg("N"))
	mm := u.(Model)
	if mm.diffLayer().fileArm != fileArmNone || mm.filesView.sel != 1 || cmd != nil || mm.diffNotice != "" {
		t.Fatalf("N off the last change must be inert: arm=%d sel=%d notice=%q",
			mm.diffLayer().fileArm, mm.filesView.sel, mm.diffNotice)
	}
}

// TestDiffNNoNextFileNotice: on the last file, N posts a notice and does not prime.
func TestDiffNNoNextFileNotice(t *testing.T) {
	m := treeDiffModelBlocks(3) // c.go: the last file
	m.diffLayer().focusBlock(1, m.diffBodyRows())

	u, _ := m.Update(keyMsg("N"))
	mm := u.(Model)
	if mm.diffNotice != "▸ no next file" || mm.diffLayer().fileArm != fileArmNone {
		t.Fatalf("no next file: notice=%q arm=%d", mm.diffNotice, mm.diffLayer().fileArm)
	}
}

// TestDiffNPInertWithNoSourceList: a picker compare (diffNavNone) ignores N/P.
func TestDiffNPInertWithNoSourceList(t *testing.T) {
	for _, k := range []string{"N", "P"} {
		m := footerModel()
		m.height = 12
		m.diffNav = diffNavNone
		v := diffViewWith(sameRowsTUI(60, 10), []int{10})
		m = m.pushLayer(v)
		m.diffTag = "bookmark2:x:y"
		u, cmd := m.Update(keyMsg(k))
		mm := u.(Model)
		if mm.diffTag != "bookmark2:x:y" || cmd != nil ||
			mm.diffLayer().fileArm != fileArmNone || mm.diffNotice != "" {
			t.Fatalf("%s with no source list must be inert", k)
		}
	}
}

// TestDiffNArmClearedByOtherKey: a primed N step is cancelled by any other key.
func TestDiffNArmClearedByOtherKey(t *testing.T) {
	m := treeDiffModelBlocks(1)
	m.diffLayer().focusBlock(1, m.diffBodyRows())
	u, _ := m.Update(keyMsg("N")) // prime
	if u.(Model).diffLayer().fileArm != fileArmNext {
		t.Fatal("setup: N must prime")
	}
	u2, _ := u.(Model).Update(keyMsg("j")) // any other key
	if u2.(Model).diffLayer().fileArm != fileArmNone {
		t.Fatal("j must cancel the primed N step")
	}
}

// TestBoundaryCueSegments: the proactive cue advertises both gestures, scoped to
// which boundary the focused change is on.
func TestBoundaryCueSegments(t *testing.T) {
	m := treeDiffModelBlocks(2) // b.go: both a prev (a.go) and a next (c.go) file

	m.diffLayer().focusBlock(1, m.diffBodyRows()) // last change
	if got := m.boundaryCue(); got != "▸ nn → top · NN → next file" {
		t.Fatalf("last-change cue = %q", got)
	}

	m.diffLayer().focusBlock(0, m.diffBodyRows()) // first change
	if got := m.boundaryCue(); got != "▸ pp → bottom · PP → prev file" {
		t.Fatalf("first-change cue = %q", got)
	}
}

// TestBoundaryCueSingleBlock: one change block is both first and last, so both
// file steps are offered and there is no wrap segment.
func TestBoundaryCueSingleBlock(t *testing.T) {
	m := treeDiffModel(2) // b.go: prev a.go, next c.go
	m.height = 12
	v := diffViewWith(sameRowsTUI(60, 10), []int{10})
	*m.diffLayer() = *v
	m.diffLayer().focusBlock(0, m.diffBodyRows())
	if got := m.boundaryCue(); got != "▸ PP → prev file · NN → next file" {
		t.Fatalf("single-change cue = %q", got)
	}
}

// TestBoundaryCueWrapOnlyNoSourceList: a picker compare still advertises the
// wrap (n/p works without a file list) but never a file step.
func TestBoundaryCueWrapOnlyNoSourceList(t *testing.T) {
	m := footerModel()
	m.height = 12
	m.diffNav = diffNavNone
	v := diffViewWith(sameRowsTUI(60, 10, 50), []int{10, 50})
	m = m.pushLayer(v)
	m.diffLayer().focusBlock(1, m.diffBodyRows()) // last change
	if got := m.boundaryCue(); got != "▸ nn → top" {
		t.Fatalf("picker-compare cue = %q (want wrap only)", got)
	}
}

// TestBoundaryCueEmptyMidFile: off any boundary, no proactive cue.
func TestBoundaryCueEmptyMidFile(t *testing.T) {
	m := footerModel()
	m.height = 12
	m.diffNav = diffNavTree
	v := diffViewWith(sameRowsTUI(90, 10, 40, 70), []int{10, 40, 70})
	m = m.pushLayer(v)
	m.diffLayer().focusBlock(1, m.diffBodyRows()) // middle change of three
	if got := m.boundaryCue(); got != "" {
		t.Fatalf("mid-file cue must be empty, got %q", got)
	}
}
