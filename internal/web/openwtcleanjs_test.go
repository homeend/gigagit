package web

import (
	"strings"
	"testing"
)

// The palette's "open working tree" on a clean checkout used to do nothing
// at all: openWorkingTree found no working-tree row and returned silently.
// It now reports whether it opened the list, and the palette says why not.
func TestPaletteOpenWorkingTreeSaysWhenClean(t *testing.T) {
	t.Parallel()
	open := jsFunc(t, "files.js", "openWorkingTree")
	if !strings.Contains(open, "return false;") || !strings.Contains(open, "return true;") {
		t.Error("openWorkingTree must report whether it opened the working-tree list")
	}
	pal := readStatic(t, "palette.js")
	if !strings.Contains(pal, `if (!(await openWorkingTree(0))) toast("No changes in the working tree — nothing to open");`) {
		t.Error("the palette's open working tree must say so when the working tree is clean")
	}
}
