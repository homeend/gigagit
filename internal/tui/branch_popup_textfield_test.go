package tui

import (
	"testing"
)

func TestBranchPopupCursorEditNoSpace(t *testing.T) {
	t.Parallel()
	p := &branchPopup{startPoint: "main"}
	m := Model{}
	m, _ = p.update(m, keyMsg("feat"))
	m, _ = p.update(m, keyMsg("left"))
	m, _ = p.update(m, keyMsg("X")) // insert before 't' -> "feaXt"
	m, _ = p.update(m, keyMsg("space"))
	_ = m
	if got := p.name.Value(); got != "feaXt" {
		t.Fatalf("name = %q, want feaXt (space dropped)", got)
	}
}

func TestRenameBranchPrefilledCursorAtEnd(t *testing.T) {
	t.Parallel()
	p := &renameBranchPopup{old: "old", name: newTextField("old")}
	m := Model{}
	m, _ = p.update(m, keyMsg("er")) // append -> "older"
	_ = m
	if got := p.name.Value(); got != "older" {
		t.Fatalf("name = %q, want older", got)
	}
}
