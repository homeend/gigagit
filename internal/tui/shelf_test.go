package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestShelfRowsContent(t *testing.T) {
	m := New(nil)
	m.shelfEntries = []model.ShelfEntry{
		{ID: "unstaged-a-b-aabbccdd", Source: "unstaged", Path: "a/b.go", SHA: "aabbccddeeff"},
		{ID: "staged-readme-11223344", Source: "staged", Path: "README.md", SHA: "1122334455"},
	}
	rows := m.shelfRows()
	if len(rows) != 2 {
		t.Fatalf("shelfRows len = %d", len(rows))
	}
	if !strings.Contains(rows[0], "a/b.go") || !strings.Contains(rows[0], "unstaged") || !strings.Contains(rows[0], "aabbccdd") {
		t.Fatalf("row0 missing fields: %q", rows[0])
	}
	if !strings.Contains(rows[1], "README.md") || !strings.Contains(rows[1], "staged") {
		t.Fatalf("row1 missing fields: %q", rows[1])
	}
}

func TestTabBarLabelIncludesShelf(t *testing.T) {
	if got := tabBarLabel(panelShelf); !strings.Contains(got, "[Shelf]") {
		t.Fatalf("active Shelf: %q", got)
	}
	if got := tabBarLabel(panelBranches); !strings.HasSuffix(got, " S") {
		t.Fatalf("Shelf short label missing when inactive: %q", got)
	}
}

func TestAddToShelfRowOnFilesPanel(t *testing.T) {
	m := filesMenuModel() // panelFiles focused with one tracked file
	if _, ok := findRow(availableActions(m), "shelf-add"); !ok {
		t.Fatalf("Add to shelf missing from menu on Files panel")
	}
	ref, ok := m.focusedShelfRef()
	if !ok || ref.Source != model.SourceUnstaged || ref.Path != "dir/f.txt" {
		t.Fatalf("focusedShelfRef = %+v ok=%v, want unstaged dir/f.txt", ref, ok)
	}
}

func TestAddToShelfRowAbsentWhenNoFileFocused(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	if _, ok := m.focusedShelfRef(); ok {
		t.Fatalf("no file focused on Branches panel; focusedShelfRef should be false")
	}
	if _, ok := findRow(availableActions(m), "shelf-add"); ok {
		t.Fatalf("Add to shelf should not appear with no file focused")
	}
}

func TestShelfTabInCycle(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.focus = panelBranches
	send := func(k string) {
		u, _ := m.Update(keyMsg(k))
		m = u.(Model)
	}
	// Branches -> Remotes -> Worktrees -> Shelf -> Branches (wrap).
	send("ctrl+right")
	send("ctrl+right")
	send("ctrl+right")
	if m.activeLeftTab != panelShelf || m.focus != panelShelf {
		t.Fatalf("3x ctrl+right: tab=%v focus=%v, want Shelf", m.activeLeftTab, m.focus)
	}
	send("ctrl+right")
	if m.activeLeftTab != panelBranches {
		t.Fatalf("4x ctrl+right: tab=%v, want Branches (wrap)", m.activeLeftTab)
	}
}
