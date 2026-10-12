package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

func TestAllNotesListsOverviews(t *testing.T) {
	t.Parallel()
	ov := domain.NotesOverview{Commits: []domain.NoteCommitNotes{{Hash: "abc1234deadbeef", Subject: "gate",
		Overviews: []domain.Overview{{ID: "o1", Commit: "abc1234deadbeef", Agent: "Claude Code", Title: "How the gate works"}}}}}
	m := footerModel()
	m.currentWorktree = "/repo"
	m, _ = m.openAllNotes()
	u, _ := m.Update(allNotesMsg{ov: ov, gen: m.allNotesGen})
	m = u.(Model)
	p := layerOf[*allNotesPopup](m)
	var found bool
	for _, r := range p.rows {
		if r.kind == anOverview && r.overview != nil && r.overview.ID == "o1" {
			found = true
		}
	}
	if !found {
		t.Fatal("no overview row")
	}
	if s := strings.Join(allNotesScreen(m), "\n"); !strings.Contains(s, "Overviews") || !strings.Contains(s, "How the gate works") {
		t.Fatalf("screen:\n%s", s)
	}
}

// On the overview row: ctrl+d asks overview-remove; enter opens the split
// over the popup.
func TestAllNotesOverviewRowKeys(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	ov, err := m.svc.NotesOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m, _ = m.openAllNotes()
	u, _ := m.Update(allNotesMsg{ov: ov, gen: m.allNotesGen})
	m = u.(Model)
	p := layerOf[*allNotesPopup](m)
	p.sel = -1
	for i, r := range p.visible() {
		if r.kind == anOverview && r.overview.ID == id {
			p.sel = i
		}
	}
	if p.sel < 0 {
		t.Fatal("no overview row")
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if mm := u.(Model); mm.modal == nil || mm.modal.req.ID != "overview-remove" {
		t.Fatalf("ctrl+d asks overview-remove: %+v", mm.modal)
	}
	m = pressDrain(t, m, "enter")
	if m.splitLayer() == nil || layerOf[*allNotesPopup](m) == nil {
		t.Fatalf("enter opens the overview over the popup; top %T status %q", m.topLayer(), m.statusMsg)
	}
}
