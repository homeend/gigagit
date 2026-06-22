package tui

import (
	"testing"
)

func TestShelfRestoreCursorEdit(t *testing.T) {
	p := &shelfRestorePopup{entryID: "e1", origin: "a/b.txt"}
	m := Model{}
	m, _ = p.update(m, keyMsg("dir/file"))
	m, _ = p.update(m, keyMsg("left"))
	m, _ = p.update(m, keyMsg("left"))
	m, _ = p.update(m, keyMsg("X")) // insert two from end -> "dir/fiXle"
	_ = m
	if got := p.dest.Value(); got != "dir/fiXle" {
		t.Fatalf("dest = %q, want dir/fiXle", got)
	}
}
