package tui

import (
	"testing"
)

func TestWorktreeEditBufCursor(t *testing.T) {
	p := &worktreePopup{state: stEdit, editBuf: newTextField("feat")}
	m := Model{}
	m, _ = p.update(m, keyMsg("left"))
	m, _ = p.update(m, keyMsg("left"))
	m, _ = p.update(m, keyMsg("X")) // "feXat"
	_ = m
	if got := p.editBuf.Value(); got != "feXat" {
		t.Fatalf("editBuf = %q, want feXat", got)
	}
}
