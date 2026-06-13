package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/domain"
)

func loadedModel(t *testing.T) Model {
	t.Helper()
	repo := newRepo(t)
	m := New(domain.New(repo))
	updated, _ := m.Update(m.loadCmd()())
	return updated.(Model)
}

func TestTabCyclesFocus(t *testing.T) {
	m := loadedModel(t)
	start := m.focus
	updated, _ := m.Update(keyMsg("tab"))
	if updated.(Model).focus == start {
		t.Fatal("tab should change the focused panel")
	}
}

func TestDownClampsSelectionInFocusedPanel(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelBranches
	updated, _ := m.Update(keyMsg("down"))
	mm := updated.(Model)
	// With a single branch, selection must clamp at 0 (no out-of-range).
	if mm.sel[panelBranches] != 0 {
		t.Fatalf("selection = %d, want clamped 0 with one item", mm.sel[panelBranches])
	}
}

func TestViewRendersPanelsWithoutPanic(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	out := m.View()
	if !strings.Contains(out, "main") {
		t.Fatalf("view should mention branch 'main':\n%s", out)
	}
	for _, label := range []string{"Branches", "Status", "Commits"} {
		if !strings.Contains(out, label) {
			t.Fatalf("view missing panel label %q:\n%s", label, out)
		}
	}
}
