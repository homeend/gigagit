package tui

import (
	"strings"
	"testing"
)

// Strict identity after the overlay/surface stacks merged: a child popup stacked
// over a parent popup draws ONLY the child over the panel base — the parent
// popup's box is NOT composited beneath it. The parent stays live on the stack
// (esc returns to it), but is not painted. This preserves the pre-merge
// "top popup replaces" visual; the merge changed routing, not compositing.
func TestChildPopupDoesNotRenderParentPopupBox(t *testing.T) {
	m := bookmarkPopupModel() // bookmark switcher listing the "a.go" bookmark

	// Sanity: the lone switcher paints its bookmark row.
	if !strings.Contains(m.render(), "a.go") {
		t.Fatalf("the lone bookmark switcher should list its row:\n%s", m.render())
	}

	// Open the cheat sheet (a contentPopup) over the switcher.
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	if layerOf[*contentPopup](m) == nil || m.bookmarkSwitcher() == nil {
		t.Fatal("? must open the cheat sheet over the still-open switcher")
	}

	out := m.render()
	// The child (cheat sheet) is drawn.
	if !strings.Contains(out, "Bookmark switcher") {
		t.Fatalf("the cheat sheet must be drawn over the switcher:\n%s", out)
	}
	// The parent switcher's row must NOT appear behind it.
	if strings.Contains(out, "a.go") {
		t.Fatalf("the parent switcher's box must not render behind the child popup:\n%s", out)
	}
}
