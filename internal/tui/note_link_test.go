package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// worktreeNoteDiff: a note on a.txt:18 (unstaged) with the diff open on it.
func worktreeNoteDiff(t *testing.T) (Model, string) {
	t.Helper()
	m := loadedNavModel(t)
	m.svc.UseNotesDir(t.TempDir())
	n, err := m.svc.NoteAdd(context.Background(), model.Note{
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: m.currentWorktree, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{18, 18}, Summary: "on 18"})
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := m.applySteer(steer.Command{ID: "nl-1", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"}, Line: &steer.Line{Side: "new", No: 18}, Wait: true})
	return pumpDiff(t, m, cmd), n.ID
}

// belowTheBox moves the diff cursor to the first real line under the note's
// rows (the box rows are not cursor lines).
func belowTheBox(t *testing.T, m Model) Model {
	t.Helper()
	v := m.diffLayer()
	v.curLine++
	for v.curLine < len(v.lines) && v.lines[v.curLine].kind != lineBody {
		v.curLine++
	}
	if m.diffLayer().cursorOnNote() {
		t.Fatal("the cursor must be on the line below the box")
	}
	return m
}

// R13: the . menu offers Copy note link on the anchor line; it copies
// gg://…a.txt:18~<fp>?note=<id>.
func TestCopyNoteLinkOnTheAnchorLine(t *testing.T) {
	t.Parallel()
	m, id := worktreeNoteDiff(t)
	if !m.diffLayer().cursorOnNote() {
		t.Fatal("cursor must be on the note's line")
	}
	r, ok := rowByID(availableActions(m), "note-copy-link")
	if !ok || r.label != "Copy note link" {
		t.Fatalf("row %+v ok %v", r, ok)
	}
	u, cmd := r.run(m)
	m = drainCmds(t, u.(Model), cmd)
	if !strings.Contains(m.statusMsg, "?note="+id) || !strings.Contains(m.statusMsg, "a.txt:18~") {
		t.Fatalf("status %q", m.statusMsg)
	}
}

// R13: the first line below the box reaches the same thread.
func TestCopyNoteLinkBelowTheBox(t *testing.T) {
	t.Parallel()
	m, id := worktreeNoteDiff(t)
	m = belowTheBox(t, m)
	r, ok := rowByID(availableActions(m), "note-copy-link")
	if !ok {
		t.Fatalf("no Copy note link below the box; menu %v", menuIDs(m))
	}
	u, cmd := r.run(m)
	m = drainCmds(t, u.(Model), cmd)
	if !strings.Contains(m.statusMsg, "?note="+id) {
		t.Fatalf("status %q", m.statusMsg)
	}
}

// R13: L on the anchor line copies the note link.
func TestLCopiesTheNoteLinkOnTheAnchorLine(t *testing.T) {
	t.Parallel()
	m, id := worktreeNoteDiff(t)
	v := m.diffLayer()
	u, cmd := v.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	m = drainCmds(t, u, cmd)
	if !strings.Contains(m.statusMsg, "?note="+id) {
		t.Fatalf("L on the anchor line: %q", m.statusMsg)
	}
}

// Review Focus 5: L on the line below the box copies the LINE link.
func TestLBelowTheBoxCopiesTheLineLink(t *testing.T) {
	t.Parallel()
	m, id := worktreeNoteDiff(t)
	m = belowTheBox(t, m)
	v := m.diffLayer()
	u, cmd := v.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	m = drainCmds(t, u, cmd)
	if strings.Contains(m.statusMsg, "?note="+id) || !strings.Contains(m.statusMsg, "a.txt:19") {
		t.Fatalf("L below the box: %q", m.statusMsg)
	}
}

// A3: a remark keeps Copy remark link; no Copy note link is offered for it.
func TestRemarkKeepsItsOwnLinkRow(t *testing.T) {
	t.Parallel()
	m, _ := openedReviewView(t)
	m = openReviewDiff(t, m, "a.go")
	m, _ = m.landOnNote(1)
	ids := menuIDs(m)
	if !ids["copy-remark-link"] || ids["note-copy-link"] {
		t.Fatalf("menu %v", ids)
	}
}

// R13: the review view's diff offers the line's Copy link (the reviewed
// tip's address), which it never had.
func TestReviewDiffOffersTheLineLink(t *testing.T) {
	t.Parallel()
	m, _ := openedReviewView(t)
	tip := m.filesReview.tip
	m = openReviewDiff(t, m, "a.go")
	r, ok := rowByID(availableActions(m), "copy-link")
	if !ok {
		t.Fatalf("no Copy link in a review diff; menu %v", menuIDs(m))
	}
	if !strings.Contains(r.copyText, "/a.go@"+tip+":") {
		t.Fatalf("link %q", r.copyText)
	}
}

// R13: View all notes — ctrl+l on a thread row copies the note link; the
// key hint says so.
func TestAllNotesCtrlLOnAThread(t *testing.T) {
	t.Parallel()
	m, id := worktreeNoteDiff(t)
	m = m.popLayer() // the diff
	m, cmd := m.openAllNotes()
	m = drainCmds(t, m, cmd)
	p := layerOf[*allNotesPopup](m)
	found := false
	for i, r := range p.visible() {
		if r.kind == anNote && r.note.Note.ID == id {
			p.sel, found = i, true
		}
	}
	if !found {
		t.Fatalf("the note is not listed:\n%s", m.View())
	}
	if !strings.Contains(p.box(m), "[ctrl+l] copy link") {
		t.Fatal("hint")
	}
	u, c := p.update(m, tea.KeyMsg{Type: tea.KeyCtrlL})
	m = drainCmds(t, u, c)
	if !strings.Contains(m.statusMsg, "?note="+id) {
		t.Fatalf("status %q", m.statusMsg)
	}
}
