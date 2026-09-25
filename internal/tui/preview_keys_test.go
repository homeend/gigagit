package tui

import (
	"strings"
	"testing"
)

// maximizedPreview reports whether the screen shows the preview alone: its
// title, and none of the list beside it.
func maximizedPreview(m Model, list string) bool {
	out := m.View()
	return strings.Contains(out, "View a.txt") && !strings.Contains(out, list)
}

func TestCtrlTMaximizesFsPreview(t *testing.T) {
	t.Parallel()
	m := settle(t, wtDiskWindow(t, map[string]string{"a.txt": "AAA\n"}))
	m.width, m.height = 120, 30
	m = fvKeys(t, m, keyMsg("right"), ctrlT())
	if !maximizedPreview(m, "Files (working tree)") {
		t.Fatalf("ctrl+t on F's preview did not fill the screen:\n%s", m.View())
	}
	m = fvKeys(t, m, ctrlT())
	if maximizedPreview(m, "Files (working tree)") {
		t.Fatal("a second ctrl+t did not bring the list back")
	}
	m = fvKeys(t, m, ctrlT(), keyMsg("left"))
	if maximizedPreview(m, "Files (working tree)") || !m.filesTreeFocused {
		t.Fatal("going back to the list must show it")
	}
}

func TestCtrlTMaximizesTheCommitTreePreview(t *testing.T) {
	t.Parallel()
	m := linkPreviewModel(t, "one\ntwo\n", 0)
	m.width, m.height = 120, 30
	m.filesTitle = "Files abcdef tree"
	m = fvKeys(t, m, ctrlT())
	if !maximizedPreview(m, "Files abcdef tree") {
		t.Fatalf("ctrl+t on the commit tree's preview did not fill the screen:\n%s", m.View())
	}
}
