package tui

import (
	"testing"
)

func TestStashPopupNameCursorEdit(t *testing.T) {
	p := &stashPopup{name: newTextField("WIP"), field: 0}
	m := Model{}
	m, _ = p.update(m, keyMsg(" fix")) // space allowed in stash name -> "WIP fix"
	m, _ = p.update(m, keyMsg("home")) // cursor to start
	m, _ = p.update(m, keyMsg("X"))    // -> "XWIP fix"
	_ = m
	if got := p.name.Value(); got != "XWIP fix" {
		t.Fatalf("name = %q, want 'XWIP fix'", got)
	}
}
