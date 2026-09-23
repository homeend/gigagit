package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Change-to-change navigation inside a STACK (the user's 2026-09-23 ruling,
// reversing plan 3's mapping). The stack is one document, so n/p walk its
// change blocks straight through — spliceStack puts every file's change starts
// in v.blocks, which is all the ordinary stepping code needs. What a stack adds
// is the part of the document that is not in the stream: a FOLDED file's
// changes, and those of a file that has never been fetched. Those are reached
// exactly as ] / [ reach a search hit (diff_stack_search.go): the file is
// unfolded, sent for, and the step is PARKED until its lines exist.

// blockIn is the index into v.blocks of the first (dir>0) / last (dir<0) change
// block whose line lies in [lo, hi] — one file's edge change.
func (v *diffView) blockIn(lo, hi, dir int) (int, bool) {
	best, found := -1, false
	for i, b := range v.blocks {
		if b < lo || b > hi {
			continue
		}
		if !found || (dir > 0 && i < best) || (dir < 0 && i > best) {
			best, found = i, true
		}
	}
	return best, found
}

// canStepBlock reports whether an ordinary step in dir has somewhere to go
// inside the stream.
func (v *diffView) canStepBlock(dir int) bool {
	if len(v.dispBlocks) == 0 {
		return false
	}
	if dir > 0 {
		return v.cur < len(v.dispBlocks)-1
	}
	return v.cur > 0
}

// changeFile is the file the change walk is currently IN: the focused block's
// file, else the cursor's. It is where a hunt counts from.
func (v *diffView) changeFile() int {
	if v.cur >= 0 && v.cur < len(v.blocks) {
		if r := v.blocks[v.cur]; r >= 0 && r < len(v.lines) {
			return v.lines[r].file
		}
	}
	return v.curFile()
}

// stackChangeStep gives n / p a chance to open a file the stream does not hold
// before the ordinary step runs. It answers handled == true ONLY when it has
// something to open: a change further on in the stream (or one visible in the
// pane) is the ordinary step's business, and so is the wrap when nothing is
// left to open at all.
func (m Model) stackChangeStep(v *diffView, dir, body int) (Model, tea.Cmd, bool) {
	if v.stk == nil || v.stk.hunt != nil {
		return m, nil, false // a hunt is already parked: let its own drain land it
	}
	if v.canStepBlock(dir) {
		return m, nil, false
	}
	// The focus may simply have scrolled out of the pane — that re-seat is a
	// step of its own and must win over opening another file.
	if at := v.focusedBlockRow(); at >= 0 && (at < v.offset || at >= v.offset+body) {
		return m, nil, false
	}
	i, ok := v.huntTarget(v.changeFile(), dir)
	if !ok {
		return m, nil, false // nothing unread ahead: the wrap stands
	}
	return m.openHunt(v, i, dir, huntChange, body)
}

// focusedBlockRow is the display row of the focused change, or -1.
func (v *diffView) focusedBlockRow() int {
	if v.cur < 0 || v.cur >= len(v.dispBlocks) {
		return -1
	}
	return v.dispBlocks[v.cur]
}
