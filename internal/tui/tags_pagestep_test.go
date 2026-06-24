package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// The active middle tab gets the box geometry, so the Tags tab (when active) has
// the same row capacity the Files tab has when IT is active — its page step
// (pgup/pgdn jump) must not collapse to 1.
func TestTagsTabRowsCapMatchesFiles(t *testing.T) {
	mFiles := footerModel() // Files active by default
	mTags := footerModel()
	mTags.activeFilesTab = panelTags
	if got, want := mTags.panelRowsCap(panelTags), mFiles.panelRowsCap(panelFiles); got != want {
		t.Fatalf("active Tags rowsCap = %d, want active Files rowsCap %d", got, want)
	}
}

// A click in the middle box must resolve to whichever tab is on screen — not
// always Files (panelFiles precedes panelTags in the panelAt scan order).
func TestPanelAtMiddleBoxResolvesActiveTab(t *testing.T) {
	mt := footerModel()
	mt.activeFilesTab = panelTags
	g := mt.layout()
	if p, ok := mt.panelAt(g.pos[panelTags].x+1, g.pos[panelTags].y+1); !ok || p != panelTags {
		t.Fatalf("middle-box click with Tags active = (%v,%v), want panelTags", p, ok)
	}
	mf := footerModel() // Files active by default
	gf := mf.layout()
	if p, ok := mf.panelAt(gf.pos[panelFiles].x+1, gf.pos[panelFiles].y+1); !ok || p != panelFiles {
		t.Fatalf("middle-box click with Files active = (%v,%v), want panelFiles", p, ok)
	}
}

func TestTagsPgDownPagesByMoreThanOne(t *testing.T) {
	m := footerModel()
	m.activeFilesTab = panelTags
	m.focus = panelTags
	m.tags = make([]model.Tag, 50)
	for i := range m.tags {
		m.tags[i] = model.Tag{Name: fmt.Sprintf("v%d", i)}
	}
	m.sel[panelTags] = 0

	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = u.(Model)
	if m.sel[panelTags] <= 1 {
		t.Fatalf("pgdown moved to row %d, want a multi-row page jump", m.sel[panelTags])
	}
}
