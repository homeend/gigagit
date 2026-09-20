package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// pairWithNote is savedPairModel with the notes store opted in and one
// new-side note on the pair's tip (b), on a.txt line 1.
func pairWithNote(t *testing.T) (Model, domain.CommitPair) {
	t.Helper()
	m, p := savedPairModel(t)
	m.svc.UseNotesDir(t.TempDir())
	pairTestNote(t, m.svc, p.B, "why a.txt exists")
	return m, p
}

func pairTestNote(t *testing.T, svc *domain.Service, commit, summary string) {
	t.Helper()
	if _, err := svc.NoteAdd(context.Background(), model.Note{
		Source: model.NoteSourceAgent, Author: "ada",
		Address: model.FileAddress{State: model.StateCommitted, Commit: commit, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: summary,
	}); err != nil {
		t.Fatal(err)
	}
}

// openPairRow presses enter on the pair row and pumps every message.
func openSavedPair(t *testing.T, m Model) Model {
	t.Helper()
	m.sel[panelPreviews] = 1
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return pumpAll(t, updated.(Model), cmd)
}

func TestPairOpenArmsTheNoteScope(t *testing.T) {
	t.Parallel()
	m, p := pairWithNote(t)
	m = openSavedPair(t, m)
	set := m.filesPreviewSet
	if set == nil || !set.IsPair() || set.Tip != p.B || set.Base != p.A {
		t.Fatalf("the pair's note scope must be armed, got %+v", set)
	}
	if m.previewOpen != nil {
		t.Fatal("a frozen pair follows no tips: previewOpen stays nil")
	}
	if m.filesPreviewCounts["a.txt"] != 1 {
		t.Fatalf("counts = %v", m.filesPreviewCounts)
	}
	if out := m.View(); !strings.Contains(out, noteBadge(1)) {
		t.Fatalf("the file row must carry its badge:\n%s", out)
	}
	// enter on the file: the diff is stamped at b and shows the note.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = pumpAll(t, updated.(Model), cmd)
	v := m.diffLayer()
	if v == nil {
		t.Fatalf("the file's diff must open (status %q)", m.statusMsg)
	}
	want := model.FileAddress{State: model.StateCommitted, Commit: p.B, Path: "a.txt"}
	if v.noteAddr != want || v.previewSet == nil || !v.previewSet.IsPair() {
		t.Fatalf("the diff must be stamped at b: addr=%+v set=%+v", v.noteAddr, v.previewSet)
	}
	if len(v.notes) != 1 || !strings.Contains(m.View(), "why a.txt exists") {
		t.Fatalf("the note must render, notes=%d\n%s", len(v.notes), m.View())
	}
}

// The scope is gated on the two commits the view SHOWS and on the generation
// it was resolved under — a result for another view must never arm this one.
func TestPairNotesMsgForAnotherViewIsDropped(t *testing.T) {
	t.Parallel()
	m, p := pairWithNote(t)
	m = openSavedPair(t, m)
	good := *m.filesPreviewSet
	m.filesPreviewSet, m.filesPreviewCounts = nil, nil

	reversed := domain.PreviewNoteSet{Tip: p.A, Base: p.B, Commits: []string{p.A}}
	m2, _ := m.handlePairNotesMsg(pairNotesMsg{set: reversed, gen: m.previewGen})
	if m2.filesPreviewSet != nil {
		t.Fatal("a scope for OTHER endpoints must be dropped")
	}
	m2, _ = m.handlePairNotesMsg(pairNotesMsg{set: good, gen: m.previewGen - 1})
	if m2.filesPreviewSet != nil {
		t.Fatal("a scope from an older generation must be dropped")
	}
	m2, _ = m.handlePairNotesMsg(pairNotesMsg{gen: m.previewGen})
	if m2.filesPreviewSet != nil {
		t.Fatal("a zero set arms nothing")
	}
	m2, _ = m.handlePairNotesMsg(pairNotesMsg{set: good, gen: m.previewGen})
	if m2.filesPreviewSet == nil {
		t.Fatal("the matching scope must arm")
	}
	// A merge preview over the same two commits owns the scope.
	m.previewOpen = &previewOpenState{id: "x"}
	m2, _ = m.handlePairNotesMsg(pairNotesMsg{set: good, gen: m.previewGen})
	if m2.filesPreviewSet != nil {
		t.Fatal("a pair scope must never replace an open merge preview's")
	}
}

// A steered landing opens the file's diff the moment the list arrives, which
// can beat the scope: the late scope must stamp that diff and load its notes.
func TestPairNotesMsgStampsAnAlreadyOpenDiff(t *testing.T) {
	t.Parallel()
	m, p := pairWithNote(t)
	m = openSavedPair(t, m)
	good := *m.filesPreviewSet
	m.filesPreviewSet, m.filesPreviewCounts = nil, nil
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // opened UNSTAMPED
	m = pumpAll(t, updated.(Model), cmd)
	if v := m.diffLayer(); v == nil || v.noteAddr.Path != "" {
		t.Fatalf("fixture: want an unstamped diff, got %+v", v)
	}
	m, cmd = m.handlePairNotesMsg(pairNotesMsg{set: good, gen: m.previewGen})
	if cmd == nil {
		t.Fatal("the late scope must load the open diff's notes")
	}
	m = pumpAll(t, m, cmd)
	v := m.diffLayer()
	if v.noteAddr.Commit != p.B || v.noteAddr.Path != "a.txt" || len(v.notes) != 1 {
		t.Fatalf("stamp = %+v notes = %d", v.noteAddr, len(v.notes))
	}
}

func TestPairCountsRefreshAfterANoteWrite(t *testing.T) {
	t.Parallel()
	m, p := pairWithNote(t)
	m = openSavedPair(t, m)
	pairTestNote(t, m.svc, p.B, "a second thread")
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcNotes}, reloadOpts{})
	m = pumpAll(t, m, cmd)
	if m.filesPreviewCounts["a.txt"] != 2 {
		t.Fatalf("the badge must follow the store, counts = %v", m.filesPreviewCounts)
	}
	if m.pairNotesRefreshCmd() == nil {
		t.Fatal("an armed pair scope refreshes")
	}
	m = m.closeFilesView()
	if m.filesPreviewSet != nil || m.pairNotesRefreshCmd() != nil {
		t.Fatal("closing the view clears the scope and stops the refresh")
	}
}

// The agent hand-off: `gg open <pair link>` lands through a steered navigate,
// and the notes must be there too — saved entry or not.
func TestSteeredPairLandingArmsTheNoteScope(t *testing.T) {
	t.Parallel()
	dir, c1, _, c3 := refPairRepo(t)
	m := refPairModel(t, dir)
	c := steer.Command{ID: "pn-1", Cmd: "navigate", File: "c.txt",
		Target: &steer.Target{State: "pair", A: c1, B: "feat/x"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, cmd := m.applySteer(c)
	m = pumpAll(t, m, cmd)
	set := m.filesPreviewSet
	if set == nil || !set.IsPair() || set.Base != c1 || set.Tip != c3 {
		t.Fatalf("the landing must arm the pair's scope, got %+v", set)
	}
	v := m.diffLayer()
	if v == nil || v.noteAddr.Commit != c3 || v.noteAddr.Path != "c.txt" {
		t.Fatalf("the landed diff must be stamped at b, got %+v", v)
	}
}

func TestPairRowCarriesItsNoteBadge(t *testing.T) {
	t.Parallel()
	m, _ := savedPairModel(t)
	m.svc.UseNotesDir(t.TempDir())
	pairTestNote(t, m.svc, m.previews[1].pair.B, "counted")
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	m = pumpAll(t, m, cmd)
	r := m.previews[1]
	if r.kind != rowPair || r.notes != 1 || r.byPath["a.txt"] != 1 {
		t.Fatalf("pair row notes = %d %v", r.notes, r.byPath)
	}
	l := previewList{rows: m.previews, text: m.previewRows()}
	if !strings.HasSuffix(l.Row(1), noteBadge(1)) || strings.Contains(l.Haystack(1), noteBadge(1)) {
		t.Fatalf("row %q / haystack %q", l.Row(1), l.Haystack(1))
	}
}

// ONE fixture, two scopes over the very same commits (main has not moved, so
// merge-base == main's tip): the merge preview links by BRANCH names, the
// pair by its two frozen shas — at the file row and at a diff line alike.
func TestScopeLinkDisagreesOnOneFixture(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		row  int
		want func(p domain.CommitPair) string
	}{
		{"merge preview", 0, func(domain.CommitPair) string { return "@main...feat/x" }},
		{"commit pair", 1, func(p domain.CommitPair) string { return "@" + p.A + ".." + p.B }},
	} {
		m, p := pairWithNote(t)
		m.sel[panelPreviews] = c.row
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = pumpAll(t, updated.(Model), cmd)
		if m.filesPreviewSet == nil {
			t.Fatalf("%s: no note scope armed", c.name)
		}
		fileLink, ok := m.contextLinkText()
		if !ok || !strings.Contains(fileLink, "/a.txt"+c.want(p)) {
			t.Fatalf("%s: file row link = %q %v", c.name, fileLink, ok)
		}
		updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = pumpAll(t, updated.(Model), cmd)
		if m.diffLayer() == nil {
			t.Fatalf("%s: the diff must open", c.name)
		}
		lineLink, ok := m.contextLinkText()
		if !ok || !strings.Contains(lineLink, "/a.txt"+c.want(p)+":1") {
			t.Fatalf("%s: diff line link = %q %v", c.name, lineLink, ok)
		}
		for _, text := range []string{fileLink, lineLink} {
			l, err := model.ParseLink(text)
			if err != nil {
				t.Fatalf("%s: %q does not parse: %v", c.name, text, err)
			}
			if (l.Target.Pair != nil) != (c.row == 1) || (l.Target.Preview != nil) != (c.row == 0) {
				t.Fatalf("%s: %q parsed to the wrong kind: %+v", c.name, text, l.Target)
			}
		}
	}
}
