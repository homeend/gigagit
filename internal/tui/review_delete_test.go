package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

func menuRowByID(t *testing.T, m Model, id string) (actionRow, bool) {
	t.Helper()
	for _, r := range availableActions(m) {
		if r.id == id {
			return r, true
		}
	}
	return actionRow{}, false
}

// The Branches review sub-row's "." menu deletes that review, after a
// confirmation that defaults to Cancel; the branch row itself offers no delete.
func TestBranchReviewRowMenuDeletesTheReview(t *testing.T) {
	t.Parallel()
	m := reviewBranchesModel(t)
	m.sel[panelBranches] = 1 // the branch
	if _, ok := menuRowByID(t, m, "delete-review"); ok {
		t.Fatal("the branch row offers Delete review")
	}
	m.sel[panelBranches] = 2 // its review
	r, ok := menuRowByID(t, m, "delete-review")
	if !ok || r.label != "Delete review" {
		t.Fatalf("review sub-row menu lacks Delete review: %+v %v", r, ok)
	}
	u, _ := r.run(m)
	m = u.(Model)
	if m.modal == nil || m.modal.req.ID != "review-remove" || m.modal.sel != 1 {
		t.Fatalf("no confirm defaulting to Cancel: %+v", m.modal)
	}
	if !strings.Contains(m.modal.req.Prompt, "Review: feature") {
		t.Fatalf("the confirm does not name the review: %q", m.modal.req.Prompt)
	}
	if _, cmd := m.modal.onResolve(m, "Cancel"); cmd != nil {
		t.Fatal("Cancel started a delete")
	}
}

// Deleting from inside the review view removes the review and closes the view.
func TestReviewViewMenuDeletesAndCloses(t *testing.T) {
	t.Parallel()
	m, id := openedReviewView(t)
	r, ok := menuRowByID(t, m, "delete-review")
	if !ok {
		t.Fatal("the review view's menu lacks Delete review")
	}
	u, _ := r.run(m)
	m = u.(Model)
	if m.modal == nil {
		t.Fatal("no confirm")
	}
	u, cmd := m.modal.onResolve(m, "Delete")
	m = u.(Model)
	m.modal = nil
	m = drainCmds(t, m, cmd)
	if m.filesReview != nil || m.filesView != nil {
		t.Fatal("the review view stayed open on a deleted review")
	}
	rs, _ := m.svc.Reviews(context.Background())
	for _, rv := range rs {
		if rv.ID == id {
			t.Fatal("the review is still stored")
		}
	}
}

// View all notes: ctrl+d on a review row asks, then deletes and reloads.
func TestAllNotesCtrlDDeletesAReview(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, reviewViewDoc)
	m, cmd := m.openAllNotes()
	m = drainCmds(t, m, cmd)
	p := layerOf[*allNotesPopup](m)
	found := false
	for i, r := range p.visible() {
		if r.kind == anReview && r.review.ID == id {
			p.sel, found = i, true
		}
	}
	if !found {
		t.Fatalf("no review row: %+v", p.visible())
	}
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = u.(Model)
	if m.modal == nil || m.modal.req.ID != "review-remove" {
		t.Fatalf("ctrl+d raised no review confirm: %+v", m.modal)
	}
	u, cmd = m.modal.onResolve(m, "Delete")
	m = u.(Model)
	m.modal = nil
	m = drainCmds(t, m, cmd)
	p = layerOf[*allNotesPopup](m)
	if p == nil {
		t.Fatal("the All notes popup closed")
	}
	for _, r := range p.rows {
		if r.kind == anReview {
			t.Fatal("the deleted review is still listed")
		}
	}
}

// ctrl+d on a plain note thread — here the list's last row — asks with the
// note's own question, deletes it, and leaves the cursor on a row.
func TestAllNotesCtrlDDeletesANoteThread(t *testing.T) {
	t.Parallel()
	m, _ := reviewViewModel(t, reviewViewDoc)
	sha := m.commits[0].Hash
	added, err := m.svc.NoteAdd(context.Background(), model.Note{Source: model.NoteSourceUser, Author: "me",
		Address: model.FileAddress{State: model.StateCommitted, Commit: sha, Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "zz stored note"})
	if err != nil {
		t.Fatal(err)
	}
	id := added.ID
	m, cmd := m.openAllNotes()
	m = drainCmds(t, m, cmd)
	p := layerOf[*allNotesPopup](m)
	vis := p.visible()
	p.sel = -1
	for i, r := range vis {
		if r.kind == anNote && r.note.Note.ID == id {
			p.sel = i
		}
	}
	if p.sel != len(vis)-1 {
		t.Fatalf("the note is row %d of %d, want the last", p.sel, len(vis))
	}
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = u.(Model)
	if m.modal == nil || m.modal.req.ID != "note-remove" || !strings.Contains(m.modal.req.Prompt, "Delete this note?") {
		t.Fatalf("ctrl+d on a note raised %+v", m.modal)
	}
	u, cmd = m.modal.onResolve(m, "Delete")
	m = u.(Model)
	m.modal = nil
	m = drainCmds(t, m, cmd)
	p = layerOf[*allNotesPopup](m)
	for _, r := range p.rows {
		if r.kind == anNote && r.note.Note.ID == id {
			t.Fatal("the deleted note is still listed")
		}
	}
	if n := len(p.visible()); p.sel < 0 || p.sel >= n {
		t.Fatalf("cursor %d fell off the %d re-read rows", p.sel, n)
	}
}
