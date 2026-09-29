package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// While a files-view CommitFiles read is in flight, a further j/k must not issue
// a second read or move the selection (pure-drop); the completion clears the gate.
func TestFilesViewDropsReadWhileInflight(t *testing.T) {
	t.Parallel()
	m := openFilesView(t, filesModel())
	m.filesReadInflight = true
	before := m.sel[panelCommits]

	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	mm := u.(Model)
	if cmd != nil {
		t.Fatal("j must not issue a read while one is in flight")
	}
	if mm.sel[panelCommits] != before {
		t.Fatalf("selection moved to %d while a read is in flight (want %d)", mm.sel[panelCommits], before)
	}

	// The read's completion clears the gate so the next keypress advances again.
	u2, _ := mm.Update(commitFilesMsg{hash: mm.filesHash, subject: "x"})
	if u2.(Model).filesReadInflight {
		t.Fatal("commitFilesMsg must clear filesReadInflight")
	}
}

// When no read is in flight, j both moves the selection and issues the reload,
// and that reload marks a read in flight (so the next held j is paced).
func TestFilesViewMoveMarksReadInflight(t *testing.T) {
	t.Parallel()
	m := openFilesView(t, filesModel())
	if m.filesReadInflight {
		t.Fatal("after the open load completes, no read should be in flight")
	}
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	mm := u.(Model)
	if cmd == nil {
		t.Fatal("j on a settled view must issue the follow-live reload")
	}
	if !mm.filesReadInflight {
		t.Fatal("issuing a per-commit reload must mark a read in flight")
	}
}

// Moving along commits reads the file list at once; the commit's reviews
// come after the cursor rests, on top of the list already shown.
func TestFilesViewMoveReadsReviewsAfterTheFiles(t *testing.T) {
	t.Parallel()
	m, _ := reviewViewModel(t, reviewViewDoc) // the review sits on commits[0]
	for s := 0; s < 4; s++ {
		m.sel[panelCommits] = s
		if bi, ok := m.backingIndex(panelCommits); ok && bi == 1 {
			break
		}
	}
	m, cmd := m.openChangedFiles(m.commits[1])
	m = drainCmds(t, m, cmd)
	m.focus, m.filesTreeFocused = panelCommits, false
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = u.(Model)
	if m.filesHash != m.commits[0].Hash || cmd == nil {
		t.Fatalf("k did not move to the reviewed commit and read it (hash %q)", m.filesHash)
	}
	files, ok := cmd().(commitFilesMsg)
	if !ok || len(files.reviews) != 0 {
		t.Fatalf("the move's read was %T with %d reviews: want the files alone", files, len(files.reviews))
	}
	u, later := m.Update(files)
	m = u.(Model)
	for _, l := range m.filesView.lines {
		if l.noteID != "" {
			t.Fatal("reviews were listed before the cursor rested")
		}
	}
	if len(m.filesView.lines) == 0 || later == nil {
		t.Fatal("the files landed but no reviews read was scheduled")
	}
	m = drainCmds(t, m, later)
	found := false
	for _, l := range m.filesView.lines {
		found = found || l.noteID != ""
	}
	if !found {
		t.Fatal("the rested cursor's reviews never joined the list")
	}
}

// A landed file list warms the file lists of the commits around the cursor,
// so the next steps of a held arrow are cache hits and show at once.
func TestFilesViewLandingPrefetchesTheNeighbours(t *testing.T) {
	t.Parallel()
	m := filesModel()
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = u.(Model)
	if m.svc.CommitFilesCached("2222222bbbb") {
		t.Fatal("the neighbour was cached before anything landed")
	}
	m = drainCmds(t, m, cmd) // the open's read lands, and its prefetch runs
	if !m.svc.CommitFilesCached("2222222bbbb") {
		t.Fatal("the landing did not warm the next commit's file list")
	}
}
