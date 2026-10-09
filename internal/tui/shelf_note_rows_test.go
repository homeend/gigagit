package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// recycleNote is the note a recycle writes on the set it shelved.
func recycleNote() domain.ResolvedNote {
	return domain.ResolvedNote{Status: model.NoteActive, Note: model.Note{
		ID: "n1", Source: model.NoteSourceAgent, Author: "gg",
		Summary:   "Recycled from /x/wt-a (feat)",
		Rationale: "Deleted (not in this set):\n  " + strings.Repeat("deep/", 30) + "gone.go",
		Created:   time.Date(2026, 9, 29, 14, 2, 0, 0, time.UTC),
	}}
}

// shelfFilesModelWithNotes is shelfFilesModel whose member list arrives with
// the entry's notes.
func shelfFilesModelWithNotes(t *testing.T, notes []domain.ResolvedNote, paths ...string) Model {
	t.Helper()
	m := shelfPopModel(shCommitEntry("ce"))
	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	var files []model.CommitFile
	for _, p := range paths {
		files = append(files, model.CommitFile{Path: p, Status: "M"})
	}
	mm, _ = m.Update(shelfFilesMsg{id: "ce", files: files, notes: notes})
	return mm.(Model)
}

// selectNoteRow puts the files-view cursor on the first note row.
func selectNoteRow(t *testing.T, m Model) Model {
	t.Helper()
	for i, l := range m.filesView.visible() {
		if l.shelfNote != "" {
			m.filesView.sel = i
			return m
		}
	}
	t.Fatal("no note row in the list")
	return m
}

// The set's notes read ABOVE its files, under a Notes heading — the way a
// commit's AI reviews read above its files.
func TestShelfFilesViewListsEntryNotes(t *testing.T) {
	t.Parallel()
	m := shelfFilesModelWithNotes(t, []domain.ResolvedNote{recycleNote()}, "a.go")
	ls := m.filesView.lines
	if len(ls) < 3 || !ls[0].heading || ls[0].text != "Notes" {
		t.Fatalf("want a Notes heading first, got %+v", ls)
	}
	if ls[1].shelfNote != "n1" || !strings.Contains(ls[1].text, "Recycled from /x/wt-a (feat)") || ls[1].path != "" {
		t.Fatalf("want the note row (no path) under the heading, got %+v", ls[1])
	}
	var files bool
	for _, l := range ls[2:] {
		files = files || l.path == "a.go"
	}
	if !files {
		t.Fatalf("the members still follow the notes: %+v", ls)
	}

	plain := shelfFilesModelWithNotes(t, nil, "a.go")
	for _, l := range plain.filesView.lines {
		if l.shelfNote != "" || l.text == "Notes" {
			t.Fatalf("an entry without notes lists no Notes section: %+v", plain.filesView.lines)
		}
	}
}

// enter on a note row reads that note; esc comes back to the list.
func TestShelfNoteRowEnterOpensTheNote(t *testing.T) {
	t.Parallel()
	m := selectNoteRow(t, shelfFilesModelWithNotes(t, []domain.ResolvedNote{recycleNote()}, "a.go"))
	m.width, m.height = 120, 40
	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	cp, ok := m.topLayer().(*contentPopup)
	if !ok {
		t.Fatalf("enter on a note row opens the note popup, got %T", m.topLayer())
	}
	var all []string
	for _, l := range cp.lines {
		all = append(all, l.text)
	}
	if joined := strings.Join(all, "\n"); !strings.Contains(joined, "Deleted (not in this set):") || !strings.Contains(joined, "gone.go") {
		t.Fatalf("the popup shows the note's text:\n%s", joined)
	}
	if !cp.fitContent {
		t.Fatal("the note popup fits its content (ctrl+t must not stretch it)")
	}
	mm, _ = m.Update(keyMsg("esc"))
	m = mm.(Model)
	if m.topLayer() != nil || m.filesView == nil || !m.inShelfFiles() {
		t.Fatalf("esc returns to the shelved set's list, top=%T", m.topLayer())
	}
}

// A note row is not a file: blame, history, copy path, bookmark and the
// view/open rows have nothing to act on.
func TestShelfNoteRowIsNotAFile(t *testing.T) {
	t.Parallel()
	m := selectNoteRow(t, shelfFilesModelWithNotes(t, []domain.ResolvedNote{recycleNote()}, "a.go"))
	if _, ok := m.filesViewSelectedLine(); ok {
		t.Fatal("a note row must not read as a selected file")
	}
	if _, ok := m.focusedBookmark(); ok {
		t.Fatal("a note row cannot be bookmarked")
	}
}

// The stacked view carries each note as a prose element above the files, the
// way it carries a review's overview: labelled with the note's summary, its
// text as rows, a long path cut in the MIDDLE.
func TestShelfNoteInTheStack(t *testing.T) {
	t.Parallel()
	m := shelfFilesModelWithNotes(t, []domain.ResolvedNote{recycleNote()}, "a.go", "b.go")
	m.width, m.height = 100, 40
	fs := m.buildTreeStack()
	if len(fs) != 3 {
		t.Fatalf("want the note + 2 files, got %d: %+v", len(fs), fs)
	}
	nf := fs[0]
	if !nf.summary || nf.label != "Recycled from /x/wt-a (feat)" {
		t.Fatalf("the note leads the stack as a prose element labelled by its summary, got %+v", nf)
	}
	var prose []string
	for _, r := range nf.prose {
		prose = append(prose, r.text)
	}
	joined := strings.Join(prose, "\n")
	if !strings.Contains(joined, "gg · 2026-09-29") || !strings.Contains(joined, "Deleted (not in this set):") {
		t.Fatalf("the element holds the note's meta and text:\n%s", joined)
	}
	var gone string
	for _, p := range prose {
		if strings.Contains(p, "gone.go") {
			gone = p
		}
	}
	if !strings.Contains(gone, "…") {
		t.Fatalf("a long path is cut in the middle, keeping its file name: %q", gone)
	}
	if fs[1].path != "a.go" || fs[2].path != "b.go" {
		t.Fatalf("files follow the note in list order: %+v", fs[1:])
	}

	// The header names the note, not "Summary".
	stk := &diffStack{files: fs}
	v := &diffView{stk: stk}
	v.rebuild()
	hdr := m.stackRow(v, dRow{line: fs[0].hdr, kind: lineHeader}, 100, false)
	if !strings.Contains(hdr, "wt-a (feat)") || strings.Contains(hdr, "Summary") {
		t.Fatalf("the note's header names its summary: %q", hdr)
	}
}

// The header names a worktree path: a narrow screen cuts its MIDDLE, so the
// directory and branch at the end survive.
func TestShelfNoteStackHeaderElidesInTheMiddle(t *testing.T) {
	t.Parallel()
	n := recycleNote()
	n.Note.Summary = "Recycled from /very/long/prefix/that/does/not/fit/at/all/wt-a (feature/x)"
	m := shelfFilesModelWithNotes(t, []domain.ResolvedNote{n}, "a.go")
	m.width, m.height = 100, 40
	fs := m.buildTreeStack()
	v := &diffView{stk: &diffStack{files: fs}}
	v.rebuild()
	hdr := strings.TrimRight(m.stackRow(v, dRow{line: fs[0].hdr, kind: lineHeader}, 40, false), " ")
	if !strings.Contains(hdr, "…") || !strings.HasSuffix(hdr, "wt-a (feature/x)") || !strings.HasPrefix(hdr, "▾ ≡ Recycled from ") {
		t.Fatalf("header must keep its end and lose its middle, got %q", hdr)
	}
}

// A narrow file list cuts a note row's path in the MIDDLE, never its end.
func TestShelfNoteRowElidesInTheMiddle(t *testing.T) {
	t.Parallel()
	n := recycleNote()
	n.Note.Summary = "Recycled from /very/long/prefix/that/does/not/fit/at/all/wt-a (feature/x)"
	m := shelfFilesModelWithNotes(t, []domain.ResolvedNote{n}, "a.go")
	m.width, m.height = 100, 40
	out := m.renderFilesView(38, 20)
	var row string
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "└") {
			row = l
		}
	}
	if !strings.Contains(row, "…") || !strings.Contains(row, "wt-a") || strings.Contains(row, "x)") || !strings.Contains(row, "└ 2026-09-29") {
		t.Fatalf("note row must keep its end and lose its middle, got %q\n%s", row, out)
	}
}

// A recycle's summary ends in "(<branch>)" and a branch holds slashes: a tight
// cut keeps that group whole and cuts the path in front of it, never leaving
// "…/x)" (the tail of "feature/x") as if it were a file name.
func TestElideNoteSummaryKeepsTheBranchGroup(t *testing.T) {
	t.Parallel()
	s := "Recycled from /very/long/prefix/that/does/not/fit/at/all/wt-a (feature/x)"
	for _, n := range []int{16, 20, 30} {
		got := elideNoteSummary(s, n)
		// At the tightest width elidePath keeps the bare name ("wt-a").
		if !strings.HasSuffix(got, "wt-a (feature/x)") || (n > 16 && !strings.Contains(got, "…")) || lipgloss.Width(got) > n {
			t.Errorf("n=%d: want the branch group whole and a middle cut, got %q", n, got)
		}
	}
	if got := elideNoteSummary(s, 10); strings.Contains(got, "x)") || !strings.HasSuffix(got, "wt-a") {
		t.Errorf("too tight for the group: the worktree's name, got %q", got)
	}
	if got := elideNoteSummary("short (x)", 30); got != "short (x)" {
		t.Errorf("a fitting summary stays whole, got %q", got)
	}
	if got := elideNoteSummary("no/group/here/at/all/file.go", 12); !strings.HasSuffix(got, "file.go") {
		t.Errorf("without a group it is an ordinary path cut, got %q", got)
	}
}
