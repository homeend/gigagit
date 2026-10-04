package tui

import (
	"context"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// noteRowsModel is scopeReviewModel's tip with all three kinds of note row in
// its Files view: a Reviews row (an AI review of the tip), a Range review row
// (the pair's note on a.txt) and a Notes row (a plain note on a.txt, a file
// the tip does not change). The tree side has the keys.
func noteRowsModel(t *testing.T) (m Model, reviewID string) {
	t.Helper()
	m, _, tip := scopeReviewModel(t)
	ctx := context.Background()
	if _, err := m.svc.NoteAdd(ctx, model.Note{
		Source: model.NoteSourceUser, Author: "me",
		Address: model.FileAddress{State: model.StateCommitted, Commit: tip, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "plain a",
	}); err != nil {
		t.Fatal(err)
	}
	tg := domain.ReviewTarget{Kind: domain.ReviewRange, Range: tip + "^.." + tip, Commit: tip}
	reviewID, _, err := m.svc.SaveReview(ctx, domain.SaveReview{Target: tg, Agent: "Claude Code", Text: "# Verdict\nship it"})
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcNotes}, reloadOpts{manual: true})
	m = drainMsgs(t, m, cmd, 4)
	m, cmd = m.openChangedFiles(model.Commit{Hash: tip})
	m = drainMsgs(t, m, cmd, 6)
	return m.focusTree(), reviewID
}

// selectRow puts the tree cursor on the first row ok accepts.
func selectRow(t *testing.T, m Model, ok func(contentLine) bool) Model {
	t.Helper()
	for i, l := range m.filesView.visible() {
		if ok(l) {
			m.filesView.sel = i
			return m
		}
	}
	t.Fatalf("no such row in %+v", m.filesView.lines)
	return m
}

func isReviewRow(l contentLine) bool { return l.noteID != "" }
func isScopeRow(l contentLine) bool  { return l.noteScope != "" }
func isNotedRow(l contentLine) bool  { return l.notedPath != "" }

func menuLabels(m Model) []string {
	var out []string
	for _, r := range availableActions(m) {
		out = append(out, r.label)
	}
	return out
}

// A note row is not a file: its "." menu opens it or deletes it — no file
// copies, no bookmark/shelf/compare, no editor, no commit id (user ruling
// 2026-10-05).
func TestNoteRowMenusAreOpenAndDelete(t *testing.T) {
	t.Parallel()
	m, _ := noteRowsModel(t)
	for _, c := range []struct {
		name string
		row  func(contentLine) bool
		want []string
	}{
		{"review", isReviewRow, []string{"Open review", "Delete review"}},
		{"range review", isScopeRow, []string{"Open range review", "Delete range review"}},
		{"notes", isNotedRow, []string{"Open notes", "Delete notes"}},
	} {
		got := menuLabels(selectRow(t, m, c.row))
		if !slices.Equal(got, c.want) {
			t.Errorf("%s row menu = %q, want %q", c.name, got, c.want)
		}
	}
}

// The review row has no path: every file action and the session snapshot's
// file (all behind focusedBookmark) pass it by, so its hotkeys do nothing.
func TestReviewRowIsNoFile(t *testing.T) {
	t.Parallel()
	m, _ := noteRowsModel(t)
	m = selectRow(t, m, isReviewRow)
	if l := m.filesView.visible()[m.filesView.sel]; l.path != "" || l.status != "" {
		t.Fatalf("the review row carries a file's path/status: %+v", l)
	}
	if b, ok := m.focusedBookmark(); ok {
		t.Fatalf("the review row addresses a file: %+v", b)
	}
	if _, ok := m.filesViewSelectedLine(); ok {
		t.Fatal("the review row reads as the selected file")
	}
}

// Open review is enter: the review view, opened from this commit.
func TestReviewRowOpenIsEnter(t *testing.T) {
	t.Parallel()
	m, id := noteRowsModel(t)
	m = selectRow(t, m, isReviewRow)
	r, ok := menuRowByID(t, m, "open-review")
	if !ok {
		t.Fatal("no Open review row")
	}
	u, cmd := r.run(m)
	m = drainCmds(t, u.(Model), cmd)
	if v := viewerText(m); m.filesReview == nil && v == "" {
		t.Fatalf("Open review opened nothing (review %s)", id)
	}
}

// deleteFromRow runs the row's delete, answers Delete and lands the result.
func deleteFromRow(t *testing.T, m Model, id, decision string) Model {
	t.Helper()
	r, ok := menuRowByID(t, m, id)
	if !ok {
		t.Fatalf("no %s row", id)
	}
	u, _ := r.run(m)
	m = u.(Model)
	if m.modal == nil || m.modal.req.ID != decision || m.modal.sel != 1 {
		t.Fatalf("no %s confirm defaulting to Cancel: %+v", decision, m.modal)
	}
	u, cmd := m.modal.onResolve(m, "Delete")
	m = u.(Model)
	m.modal = nil
	return drainCmds(t, m, cmd)
}

func listsRow(m Model, ok func(contentLine) bool) bool {
	return slices.ContainsFunc(m.filesView.lines, ok)
}

// Delete review on the row asks, removes the review and drops its row; the
// other note rows stay.
func TestReviewRowDeleteDropsTheRow(t *testing.T) {
	t.Parallel()
	m, id := noteRowsModel(t)
	m = deleteFromRow(t, selectRow(t, m, isReviewRow), "delete-review", "review-remove")
	if listsRow(m, isReviewRow) || !listsRow(m, isScopeRow) || !listsRow(m, isNotedRow) {
		t.Fatalf("after the delete: %+v", m.filesView.lines)
	}
	if _, err := m.svc.Review(context.Background(), id); err == nil {
		t.Fatal("the review is still stored")
	}
}

// Delete range review removes the scope's notes at the commit and drops the
// row; the plain note's row stays.
func TestRangeReviewRowDeleteDropsTheRow(t *testing.T) {
	t.Parallel()
	m, _ := noteRowsModel(t)
	m = deleteFromRow(t, selectRow(t, m, isScopeRow), "delete-range-review", "range-review-remove")
	if listsRow(m, isScopeRow) || !listsRow(m, isNotedRow) || !listsRow(m, isReviewRow) {
		t.Fatalf("after the delete: %+v", m.filesView.lines)
	}
	if n := len(m.noteCounts.ScopesByCommit[m.filesHash]); n != 0 {
		t.Fatalf("the scope still counts %d at the commit", n)
	}
}

// Delete notes removes the plain notes on the row's path and drops the row;
// the range review's note on the same path stays.
func TestNotesRowDeleteDropsTheRow(t *testing.T) {
	t.Parallel()
	m, _ := noteRowsModel(t)
	m = deleteFromRow(t, selectRow(t, m, isNotedRow), "delete-notes", "notes-remove")
	if listsRow(m, isNotedRow) || !listsRow(m, isScopeRow) {
		t.Fatalf("after the delete: %+v", m.filesView.lines)
	}
	// The emptied group's heading goes too — b.txt is a ROOT file (no
	// directory heading of its own), so a stale "Notes" would claim it.
	if listsRow(m, func(l contentLine) bool { return l.heading && l.text == "Notes" }) {
		t.Fatalf("the emptied Notes heading stayed over the files: %+v", m.filesView.lines)
	}
	if m.statusMsg != "deleted the note" {
		t.Fatalf("status = %q, want one thread deleted (the reply goes with it)", m.statusMsg)
	}
	if n := m.noteCounts.PlainByCommitPath[m.filesHash+":a.txt"]; n != 0 {
		t.Fatalf("a.txt still counts %d plain notes", n)
	}
	if n := len(m.noteCounts.ScopesByCommit[m.filesHash]); n != 1 {
		t.Fatalf("the range review's note went too: %d scopes left", n)
	}
}
