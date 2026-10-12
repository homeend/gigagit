package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notemeta"
)

// cursorOnANote moves the diff's cursor to the first line a thread is in
// reach of.
func cursorOnANote(t *testing.T, m Model) Model {
	t.Helper()
	v := m.diffLayer()
	for i := range v.lines {
		v.setCursorLine(i, m.diffBodyRows())
		if len(m.notesAtCursor()) > 0 {
			return m
		}
	}
	t.Fatal("no note in reach anywhere")
	return m
}

func noteMenuRow(t *testing.T, m Model, id string) (actionRow, bool) {
	t.Helper()
	for _, r := range m.noteMenuRows() {
		if r.id == id {
			return r, true
		}
	}
	return actionRow{}, false
}

// mark runs the . menu's Mark… row and answers its modal with opt.
func mark(t *testing.T, m Model, opt string) Model {
	t.Helper()
	r, ok := noteMenuRow(t, m, "note-mark")
	if !ok {
		t.Fatal("no Mark… row")
	}
	nm, cmd := r.run(m)
	m = drainCmds(t, nm.(Model), cmd)
	if m.modal == nil || m.modal.req.ID != "note-mark" || !slices.Contains(m.modal.req.Options, opt) {
		t.Fatalf("modal = %+v", m.modal)
	}
	nm, cmd = m.modal.onResolve(m, opt)
	m = nm.(Model)
	m.modal = nil
	return drainCmds(t, m, cmd)
}

// storedNoteDiff is a commit diff of a.go with one stored agent note on it.
func storedNoteDiff(t *testing.T) (Model, string) {
	t.Helper()
	m, _ := reviewViewModel(t, reviewViewDoc)
	sha := m.commits[0].Hash
	n, err := m.svc.NoteAdd(context.Background(), model.Note{Source: model.NoteSourceAgent, Author: "Claude Code",
		Address: model.FileAddress{State: model.StateCommitted, Commit: sha, Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "stored"})
	if err != nil {
		t.Fatal(err)
	}
	nm, cmd := m.openChangedFiles(m.commits[0])
	m = drainCmds(t, nm, cmd)
	m.filesTreeFocused = true
	m = openReviewDiff(t, m, "a.go")
	return cursorOnANote(t, m), n.ID
}

func TestNoteMenuOffersMark(t *testing.T) {
	t.Parallel()
	m, _ := storedNoteDiff(t)
	r, ok := noteMenuRow(t, m, "note-mark")
	if !ok || r.label != i18n.T("Mark…") {
		t.Fatalf("row = %+v", r)
	}
}

func TestMarkWritesStatusAndReloads(t *testing.T) {
	t.Parallel()
	m, id := storedNoteDiff(t)
	m = mark(t, m, "disagreed")
	n, err := m.svc.NoteGet(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Added) != 1 || n.Added[0].Key != notemeta.StatusKey || n.Added[0].Value != "disagreed" || n.Added[0].By != m.identity.EffectiveName {
		t.Fatalf("added = %+v", n.Added)
	}
	v := m.diffLayer()
	if v.collapsed[id] {
		t.Fatal("a mark has no consequence: the thread does not fold")
	}
	var shown domain.ResolvedNote
	for _, r := range v.notes {
		if r.Note.ID == id {
			shown = r
		}
	}
	if shown.Resolution != nil {
		t.Fatal("a mark has no consequence: the thread is not resolved")
	}
	if rows := v.noteBoxLines(shown, 60, v); !strings.Contains(rows[0].badges, i18n.T("disagreed")) {
		t.Fatalf("the reloaded box lacks the mark: %q", rows[0].badges)
	}
	m = mark(t, m, "clear")
	if n, _ := m.svc.NoteGet(context.Background(), id); len(n.Added) != 0 {
		t.Fatalf("clear left %+v", n.Added)
	}
}

func TestMarkCancelWritesNothing(t *testing.T) {
	t.Parallel()
	m, id := storedNoteDiff(t)
	m = mark(t, m, "Cancel")
	if n, _ := m.svc.NoteGet(context.Background(), id); len(n.Added) != 0 || len(n.Meta) != 0 {
		t.Fatalf("Cancel wrote %+v %+v", n.Added, n.Meta)
	}
}

func TestMarkRemarkShowsAfterReload(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, reviewViewDoc)
	rm, cmd := m.openReview(id, "Review")
	m = drainCmds(t, rm, cmd)
	m = cursorOnANote(t, openReviewDiff(t, m, "a.go"))
	m = mark(t, m, "agreed")
	v := m.diffLayer()
	if len(v.notes) != 1 || !model.IsReviewNoteID(v.notes[0].Note.ID) {
		t.Fatalf("notes = %+v", v.notes)
	}
	if rows := v.noteBoxLines(v.notes[0], 60, v); !strings.Contains(rows[0].badges, i18n.T("agreed")) {
		t.Fatalf("the remark's box lacks the mark after the reload: %q (added %+v)", rows[0].badges, v.notes[0].Note.Added)
	}
}

func TestMarkNotOfferedOnForgeThread(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	v.notes = []domain.ResolvedNote{forgeRoot("C1", 5, "rename this", false)}
	v.relayout(0)
	v.setCursorLine(4, m.diffBodyRows())
	if _, ok := noteMenuRow(t, m, "note-mark"); ok {
		t.Fatal("a GitHub thread cannot be marked")
	}
}

// A review that counts its status: a mark changes the review view's counts
// once the mark lands.
func TestMarkRefreshesReviewCounts(t *testing.T) {
	t.Parallel()
	doc := `{"version":1,"summary":"s","display":{"status":{"show":["badge","count"]}},"files":[
 {"path":"a.go","annotations":[{"newRange":[3,3],"summary":"A is unused"}]}]}`
	m, id := reviewViewModel(t, doc)
	rm, cmd := m.openReview(id, "Review")
	m = drainCmds(t, rm, cmd)
	if strings.Contains(m.filesCountsLine(), "agreed") {
		t.Fatal("no mark yet")
	}
	m = cursorOnANote(t, openReviewDiff(t, m, "a.go"))
	m = mark(t, m, "agreed")
	if got := m.filesCountsLine(); !strings.Contains(got, "1 agreed") {
		t.Fatalf("header after the mark = %q", got)
	}
}
