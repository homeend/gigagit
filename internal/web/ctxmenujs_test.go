package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A right-click menu is placed at the pointer in viewport coordinates: once
// anything under it scrolls (the mouse wheel over a diff), the row it was
// opened for moves away and the menu would float over unrelated lines. Any
// scroll outside the menu closes it — capture phase, since scroll does not
// bubble — but the menu's own scroll (a tall menu, max-height) does not.
func TestCtxMenuClosesOnScroll(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "layers.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		`document.addEventListener("scroll", closeCtxMenuOnScroll, true);`,
		`window.addEventListener("resize", closeCtxMenuOnScroll);`,
		`if (!menu._items) return; // closed: nothing to do`,
		`if (e.target instanceof Node && menu.contains(e.target)) return; // the menu's own scroll`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("layers.js lacks %q", want)
		}
	}
}
