package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/notebatch"
)

// allNotesAt opens View all notes on m's store with the cursor on the first
// row pick accepts.
func allNotesAt(t *testing.T, m Model, pick func(anRow) bool) Model {
	t.Helper()
	ov, err := m.svc.NotesOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m.filesView, m.filesReview = nil, nil
	m, _ = m.openAllNotes()
	u, _ := m.Update(allNotesMsg{ov: ov, gen: m.allNotesGen})
	m = u.(Model)
	p := layerOf[*allNotesPopup](m)
	p.sel = -1
	for i, r := range p.visible() {
		if pick(r) {
			p.sel = i
			break
		}
	}
	if p.sel < 0 {
		t.Fatal("no such row")
	}
	return m
}

// ctrlE presses ctrl+e and returns the export's resolved message (nil cmd:
// ok false).
func ctrlE(t *testing.T, m Model) (patchResolvedMsg, bool) {
	t.Helper()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if cmd == nil {
		return patchResolvedMsg{}, false
	}
	msg, ok := cmd().(patchResolvedMsg)
	return msg, ok
}

// ctrl+e on a permanent overview row exports it (the footer says so).
func TestAllNotesExportsAnOverview(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m = allNotesAt(t, m, func(r anRow) bool { return r.kind == anOverview && r.overview.ID == id })
	if s := strings.Join(allNotesScreen(m), "\n"); !strings.Contains(s, "[ctrl+e] export html") {
		t.Fatalf("no ctrl+e hint:\n%s", s)
	}
	msg, ok := ctrlE(t, m)
	if !ok || msg.err != nil || filepath.Base(msg.defaultPath) != "overview-tour-of-a.html" {
		t.Fatalf("export = %v err %v path %q", ok, msg.err, msg.defaultPath)
	}
	// The path prompt opens over the popup; esc comes back to it.
	u, _ := m.Update(msg)
	m = u.(Model)
	if _, top := m.topLayer().(*exportPatchPopup); !top {
		t.Fatalf("top = %T, want the export path prompt", m.topLayer())
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if top := u.(Model).topLayer(); top != layerOf[*allNotesPopup](u.(Model)) || top == nil {
		t.Fatalf("esc: top = %T, want View all notes back", top)
	}
}

// ctrl+e on a review row exports the review's stored overview.
func TestAllNotesExportsAReviewOverview(t *testing.T) {
	t.Parallel()
	m, sha := reviewWithOverview(t)
	m = allNotesAt(t, m, func(r anRow) bool { return r.kind == anReview })
	msg, ok := ctrlE(t, m)
	if !ok || msg.err != nil || filepath.Base(msg.defaultPath) != "review-overview-"+sha[:7]+".html" {
		t.Fatalf("export = %v err %v path %q", ok, msg.err, msg.defaultPath)
	}
}

// A review without an overview and a note thread have nothing to export:
// no hint, and the key does nothing.
func TestAllNotesExportOnlyWhereThereIsAnOverview(t *testing.T) {
	t.Parallel()
	with := domain.Review{ID: "r1", Doc: &notebatch.ReviewDoc{Overview: "see [a](a.go:1)"}}
	without := domain.Review{ID: "r2", Doc: &notebatch.ReviewDoc{Overview: "  "}}
	for _, c := range []struct {
		row  anRow
		want bool
	}{
		{anRow{kind: anReview, review: &with}, true},
		{anRow{kind: anReview, review: &without}, false},
		{anRow{kind: anReview, review: &domain.Review{ID: "r3"}}, false},
		{anRow{kind: anOverview, overview: &domain.Overview{ID: "o1"}}, true},
		{anRow{kind: anNote}, false},
	} {
		if _, ok := c.row.exportSource(); ok != c.want {
			t.Errorf("%+v: exportable %v, want %v", c.row, ok, c.want)
		}
	}
}
