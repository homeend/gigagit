package tui

import (
	"testing"
)

func TestBookmarkPasteCursorEdit(t *testing.T) {
	t.Parallel()
	p := &bookmarkPastePopup{origin: "a/b.txt", dest: newTextField("dir/file")}
	m := Model{}
	m, _ = p.update(m, keyMsg("left"))
	m, _ = p.update(m, keyMsg("left"))
	m, _ = p.update(m, keyMsg("X")) // insert two from end -> "dir/fiXle"
	_ = m
	if got := p.dest.Value(); got != "dir/fiXle" {
		t.Fatalf("dest = %q, want dir/fiXle", got)
	}
}
