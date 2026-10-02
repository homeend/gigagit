package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// The Range reviews group reads before the Notes and the files; its rows are
// not files (no path), so the stack, N/P and every file action pass them by.
func TestWithScopeLines(t *testing.T) {
	t.Parallel()
	scopes := []domain.NoteScopeCount{{Scope: "aaaaaaa..bbbbbbb", N: 1}, {Scope: "main...feature", N: 2}}
	lines := withScopeLines(scopes, commitFileLines([]model.CommitFile{{Path: "c.txt", Status: "A"}}))
	if len(lines) != 4 || !lines[0].heading || lines[3].path != "c.txt" {
		t.Fatalf("lines = %+v", lines)
	}
	if l := lines[1]; l.noteScope != "aaaaaaa..bbbbbbb" || l.path != "" || !strings.Contains(l.text, "aaaaaaa..bbbbbbb") {
		t.Fatalf("pair row = %+v", l)
	}
	// A merge preview reads the Previews panel's way round.
	if l := lines[2]; l.noteScope != "main...feature" || !strings.Contains(l.text, "feature → main") {
		t.Fatalf("preview row = %+v", l)
	}
	if got := withScopeLines(scopes, commitFileLines(nil)); len(got) != 3 {
		t.Fatalf("(no files) must give way to the rows: %+v", got)
	}
	if got := withScopeLines(nil, commitFileLines(nil)); len(got) != 1 {
		t.Fatalf("no scopes: the list is unchanged: %+v", got)
	}
}

// scopeReviewModel: main, then feat/x = "add a" + "add b". A note written in
// the merge preview feat/x → main sits on the tip, on a.txt — a file the tip
// commit ("add b") does not change. The tip's files view is open, tree side.
func scopeReviewModel(t *testing.T) (m Model, base, tip string) {
	t.Helper()
	dir, repo := newRepoDir(t)
	base = strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "-q", "-b", "feat/x")
	for _, f := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, f+".txt"), []byte(f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", ".")
		runGit(t, dir, "commit", "-q", "-m", "add "+f)
	}
	tip = strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "-q", "main")
	svc := domain.New(repo)
	svc.UsePreviewsDir(t.TempDir())
	svc.UseNotesDir(t.TempDir())
	if _, err := svc.NoteAdd(context.Background(), model.Note{
		Source: model.NoteSourceAgent, Author: "ada", Preview: "main...feat/x",
		Address: model.FileAddress{State: model.StateCommitted, Commit: tip, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "why a?",
	}); err != nil {
		t.Fatal(err)
	}
	m = New(svc)
	m.width, m.height = 160, 40
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcNotes}, reloadOpts{manual: true})
	m = drainMsgs(t, m, cmd, 4)
	m.focus = panelCommits
	m, cmd = m.openChangedFiles(model.Commit{Hash: tip})
	m = drainMsgs(t, m, cmd, 6)
	m = m.focusTree()
	return m, base, tip
}

func rangeRowIndex(t *testing.T, m Model) int {
	t.Helper()
	for i, l := range m.filesView.visible() {
		if l.noteScope != "" {
			return i
		}
	}
	t.Fatalf("no Range review row in %+v", m.filesView.lines)
	return -1
}

// A range review's notes are one row of the commit that holds them — never
// loose Notes rows for the files that commit does not change.
func TestCommitFilesListARangeReviewRow(t *testing.T) {
	t.Parallel()
	m, _, _ := scopeReviewModel(t)
	rangeRowIndex(t, m)
	for _, l := range m.filesView.lines {
		if l.notedPath != "" {
			t.Fatalf("a range review's note listed as a loose Notes row: %+v", l)
		}
	}
	v := m.View()
	if !strings.Contains(v, "Range reviews") || !strings.Contains(v, "feat/x → main  ◆ 1") {
		t.Fatalf("the row must name the range and count its notes:\n%s", v)
	}
}

// enter opens the range frozen at this commit, notes on their files; esc
// comes back to the commit's files with the cursor on the row.
func TestRangeReviewRowOpensTheRangeAndEscReturns(t *testing.T) {
	t.Parallel()
	m, base, tip := scopeReviewModel(t)
	m.filesView.sel = rangeRowIndex(t, m)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainMsgs(t, updated.(Model), cmd, 8)
	if !m.showsCommitPair(base, tip) {
		t.Fatalf("enter must open %s..%s (status %q, mode %v)", base[:7], tip[:7], m.statusMsg, m.filesMode)
	}
	if !strings.Contains(m.filesTitle, "feat/x → main") {
		t.Fatalf("title = %q", m.filesTitle)
	}
	if m.filesPreviewSet == nil || m.filesPreviewCounts["a.txt"] != 1 {
		t.Fatalf("the range's notes must badge their files: set %v counts %v", m.filesPreviewSet, m.filesPreviewCounts)
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = drainMsgs(t, updated.(Model), cmd, 6)
	if m.filesView == nil || m.inCompareMode() || m.filesHash != tip {
		t.Fatalf("esc must return to the commit's files: view %v hash %q", m.filesView != nil, m.filesHash)
	}
	if got := m.filesView.visible()[m.filesView.sel]; got.noteScope != "main...feat/x" {
		t.Fatalf("the cursor must land on the row it left: %+v", got)
	}
	// esc again closes the view as it always did.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).filesView != nil {
		t.Fatal("a second esc must close the files view")
	}
}

// A range that can no longer be worked out says so and stays put.
func TestRangeReviewRowUnresolvableSaysSo(t *testing.T) {
	t.Parallel()
	m, _, tip := scopeReviewModel(t)
	m, _ = m.handleScopeOpenMsg(scopeOpenMsg{scope: "gone...feat/x", gen: m.previewGen, err: context.Canceled})
	if m.inCompareMode() || m.filesHash != tip || m.statusMsg == "" {
		t.Fatalf("a failed open must keep the commit's files and say why: status %q", m.statusMsg)
	}
}

// A commit holding a range review reads as REVIEWED in the Commits list — ✎,
// the marker an AI review has — whichever form the range took (a merge
// preview: "branch review"; a commit pair). Its range notes are the review,
// so they are not counted again as ◆ N; a note written outside any range
// still is, and earns no ✎.
func TestCommitRowMarksARangeReview(t *testing.T) {
	t.Parallel()
	m, base, tip := scopeReviewModel(t) // a merge-preview note on the tip
	ctx := context.Background()
	mid, _, err := m.svc.ResolveRev(ctx, tip+"~1")
	if err != nil {
		t.Fatal(err)
	}
	mid = strings.TrimSpace(mid)
	add := func(commit, preview string) {
		t.Helper()
		if _, err := m.svc.NoteAdd(ctx, model.Note{
			Source: model.NoteSourceAgent, Author: "ada", Preview: preview,
			Address: model.FileAddress{State: model.StateCommitted, Commit: commit, Path: "a.txt"},
			Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "s",
		}); err != nil {
			t.Fatal(err)
		}
	}
	add(mid, base[:7]+".."+mid[:7]) // a commit-pair note on "add a"
	add(mid, "")                    // …and a plain note beside it
	m.svc.InvalidateNoteCounts()
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcNotes}, reloadOpts{manual: true})
	m = drainMsgs(t, m, cmd, 4)

	rows := map[string]string{}
	for i, c := range m.commits {
		rows[c.Hash] = m.commitIdentRowAt(i+m.wipCount(), m.commitIdentWidth(), false, -1)
	}
	for name, tc := range map[string]struct {
		hash       string
		mark       bool
		badge, not string
	}{
		"merge preview on the tip": {tip, true, "", "◆"},
		"commit pair + plain note": {mid, true, "◆ 1", "◆ 2"},
		"no notes":                 {base, false, "", "◆"},
	} {
		row := rows[tc.hash]
		if got := m.commitReviewed(tc.hash); got != tc.mark {
			t.Errorf("%s: commitReviewed = %v, want %v", name, got, tc.mark)
		}
		if got := strings.Contains(row, markerReview); got != tc.mark {
			t.Errorf("%s: ✎ in the row = %v, want %v: %q", name, got, tc.mark, row)
		}
		if tc.badge != "" && !strings.Contains(row, tc.badge) {
			t.Errorf("%s: row must carry %q: %q", name, tc.badge, row)
		}
		if strings.Contains(row, tc.not) {
			t.Errorf("%s: row must not carry %q: %q", name, tc.not, row)
		}
	}
}

// A range review's notes belong to the review alone: on a file the commit
// itself changes they are neither counted on the file's row nor drawn in the
// commit's own diff of it. Only the note written outside any range is; the
// review's notes show when the range is opened from its row.
func TestRangeNotesStayOutOfTheCommitsOwnView(t *testing.T) {
	t.Parallel()
	m, _, tip := scopeReviewModel(t) // tip = "add b": it changes b.txt
	ctx := context.Background()
	add := func(preview, summary string) {
		t.Helper()
		if _, err := m.svc.NoteAdd(ctx, model.Note{
			Source: model.NoteSourceAgent, Author: "ada", Preview: preview,
			Address: model.FileAddress{State: model.StateCommitted, Commit: tip, Path: "b.txt"},
			Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: summary,
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("main...feat/x", "review: why b?")
	add("", "plain: b is terse")
	m.svc.InvalidateNoteCounts()
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcNotes}, reloadOpts{manual: true})
	m = drainMsgs(t, m, cmd, 4)

	v := m.View()
	if !strings.Contains(v, "b.txt  ◆ 1") || strings.Contains(v, "b.txt  ◆ 2") {
		t.Fatalf("the file row must count the plain note only:\n%s", v)
	}
	if !strings.Contains(v, "feat/x → main  ◆ 2") {
		t.Fatalf("the review row counts both of its notes:\n%s", v)
	}
	// The commit's own diff of b.txt.
	for i, l := range m.filesView.visible() {
		if l.path == "b.txt" {
			m.filesView.sel = i
		}
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainMsgs(t, updated.(Model), cmd, 8)
	dv := m.diffLayer()
	if dv == nil {
		t.Fatalf("enter must open b.txt's diff (status %q)", m.statusMsg)
	}
	if len(dv.notes) != 1 || dv.notes[0].Note.Summary != "plain: b is terse" {
		t.Fatalf("the commit's diff must show the plain note only: %+v", dv.notes)
	}
	// The same diff opened from View all notes (rangeNotes) draws both: there
	// a note opens where it is stored.
	dv.rangeNotes = true
	updated, _ = m.Update(m.loadNotesCmd()())
	m = updated.(Model)
	if dv = m.diffLayer(); len(dv.notes) != 2 {
		t.Fatalf("a View-all-notes diff must draw the range note too: %+v", dv.notes)
	}
	dv.rangeNotes = false
	// Back to the commit, into the review: there the file carries the review's note.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	m.filesView.sel = rangeRowIndex(t, m)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainMsgs(t, updated.(Model), cmd, 8)
	// …and only ITS notes: the plain one stays the commit's.
	if m.filesPreviewSet == nil || m.filesPreviewCounts["b.txt"] != 1 || m.filesPreviewCounts["a.txt"] != 1 {
		t.Fatalf("the review must carry its own notes, per file: %v", m.filesPreviewCounts)
	}
	if got := m.filesPreviewSet.Pair(); got != "main...feat/x" {
		t.Fatalf("a note written in the opened review must join it: scope %q", got)
	}
}
