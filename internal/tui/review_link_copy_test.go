package tui

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// captureClip points m's clipboard at *got.
func captureClip(m Model, got *string) Model {
	m.clipWrite = func(_ io.Writer, s string) (string, error) { *got = s; return "fake", nil }
	return m
}

// runMenuRow runs the "." menu row id and lands what it starts.
func runMenuRow(t *testing.T, m Model, id string) Model {
	t.Helper()
	r, ok := menuRowByID(t, m, id)
	if !ok {
		t.Fatalf("no %s row in %q", id, menuLabels(m))
	}
	u, cmd := r.run(m)
	return drainCmds(t, u.(Model), cmd)
}

// Each note row copies its own link: the review link, the pair the range
// review was written in, the file at the commit.
func TestNoteRowCopyLinks(t *testing.T) {
	t.Parallel()
	m, id := noteRowsModel(t)
	var got string
	m = captureClip(m, &got)
	for _, c := range []struct {
		name string
		row  func(contentLine) bool
		want string
	}{
		{"review", isReviewRow, "?review=" + id},
		{"range review", isScopeRow, ".."},
		{"notes", isNotedRow, "/a.txt@" + m.filesHash},
	} {
		got = ""
		runMenuRow(t, selectRow(t, m, c.row), "copy-gg-link")
		if !strings.HasPrefix(got, "gg://") || !strings.Contains(got, c.want) {
			t.Fatalf("%s row copied %q, want …%s…", c.name, got, c.want)
		}
	}
}

// The opened review's "." menu copies the review link.
func TestReviewViewCopiesItsLink(t *testing.T) {
	t.Parallel()
	m, id := openedReviewView(t)
	var got string
	m = captureClip(m, &got)
	runMenuRow(t, m, "copy-review-link")
	if !strings.Contains(got, "?review="+id) {
		t.Fatalf("copied %q", got)
	}
}

// A Branches review sub-row copies its review's link.
func TestBranchReviewRowCopiesItsLink(t *testing.T) {
	t.Parallel()
	// A STORED review (reviewBranchesModel's head has no store behind it),
	// listed under its branch the way NoteCounts lists a branch review.
	m, id := reviewViewModel(t, reviewViewDoc)
	tip := m.commits[0]
	m.loading = false
	if m.filesView != nil {
		m = m.closeFilesView() // the Branches panel's own menu, no content window over it
	}
	m.branches = []model.Branch{{Name: "main", IsHead: true, Hash: tip.Hash, UnixTime: time.Now().Unix()}}
	m.noteCounts.Reviews = []domain.ReviewHead{{ID: id, Branch: "main", Commit: tip.Hash, Agent: "Claude", Created: time.Now()}}
	m.focus = panelBranches
	m.sel[panelBranches] = 1 // the review sub-row under main
	h, ok := m.selectedBranchReview()
	if !ok {
		t.Fatal("no review sub-row selected")
	}
	var got string
	m = captureClip(m, &got)
	runMenuRow(t, m, "copy-gg-link")
	if !strings.Contains(got, "?review="+h.ID) {
		t.Fatalf("copied %q", got)
	}
}

// View all notes: ctrl+l on a review row copies its link.
func TestAllNotesCtrlLCopiesAReviewLink(t *testing.T) {
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
	var got string
	m = captureClip(m, &got)
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	drainCmds(t, u.(Model), cmd)
	if !strings.Contains(got, "?review="+id) {
		t.Fatalf("copied %q", got)
	}
}
