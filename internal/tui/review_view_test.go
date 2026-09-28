package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

const reviewViewDoc = `{"version":1,"summary":"## Overview\nLooks fine, see ` + "`A`" + `.","meta":{"verdict":"approve"},"files":[
 {"path":"a.go","summary":"adds A","annotations":[{"newRange":[3,3],"summary":"A is unused","rationale":"nothing calls it","meta":{"severity":"nit"}}]},
 {"path":"zzz.go","annotations":[{"newRange":[1,1],"summary":"not in this commit"}]}]}`

// reviewViewModel is stackRepoModel with a review of its tip commit saved as
// text; it returns the model and the review's id.
func reviewViewModel(t *testing.T, text string) (Model, string) {
	t.Helper()
	m := stackRepoModel(t)
	m.svc.UseNotesDir(t.TempDir())
	sha := m.commits[0].Hash
	tg := domain.ReviewTarget{Kind: domain.ReviewRange, Range: sha + "^.." + sha, Label: shortHash(sha)}
	id, _, err := m.svc.SaveReview(context.Background(), domain.SaveReview{Target: tg, Agent: "Claude", Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return m, id
}

func openedReviewView(t *testing.T) (Model, string) {
	t.Helper()
	m, id := reviewViewModel(t, reviewViewDoc)
	m, cmd := m.openReview(id, "Review")
	return drainCmds(t, m, cmd), id
}

func filesLine(t *testing.T, m Model, path string) (contentLine, int) {
	t.Helper()
	for i, l := range m.filesView.visible() {
		if l.path == path {
			return l, i
		}
	}
	t.Fatalf("no %s row in the files view", path)
	return contentLine{}, -1
}

func TestReviewViewTree(t *testing.T) {
	t.Parallel()
	m, _ := openedReviewView(t)
	if m.filesView == nil || m.filesReview == nil || m.filesMode != filesModeChanged {
		t.Fatalf("review view not open: filesView=%v review=%v mode=%v", m.filesView != nil, m.filesReview, m.filesMode)
	}
	vis := m.filesView.visible()
	if !vis[0].overview || !strings.Contains(vis[0].text, "Overview") {
		t.Fatalf("first row %+v, want the Overview row", vis[0])
	}
	a, i := filesLine(t, m, "a.go")
	if !strings.Contains(a.text, "◆1") {
		t.Fatalf("a.go row %q, want ◆1", a.text)
	}
	if !strings.Contains(vis[i+1].text, "adds A") || vis[i+1].path != "" || !vis[i+1].dim || vis[i+1].heading {
		t.Fatalf("row after a.go %+v, want its summary (dim, not a heading: the sticky line names directories)", vis[i+1])
	}
	// The Overview row leads the box itself: no "." directory line above it.
	view := ansi.Strip(m.View())
	if strings.Contains(view, "│ .  ") || strings.Contains(view, "│ . ") {
		t.Fatalf("a root-dir sticky line sits above the Overview:\n%s", view)
	}
	for _, l := range vis {
		if l.noteID != "" {
			t.Fatal("the review view lists no @notes/ entries")
		}
	}
	if !strings.HasPrefix(m.filesTitle, "Review") {
		t.Fatalf("title %q", m.filesTitle)
	}
	if got := m.filesMetaLineFor(); !strings.Contains(got, "Claude") || !strings.Contains(got, "2 notes on 2 files") {
		t.Fatalf("meta line %q", got)
	}
}

func TestReviewOverviewPopupRendersMarkdown(t *testing.T) {
	t.Parallel()
	m, _ := openedReviewView(t)
	u, _ := m.openDiffForFileLine(m.filesView.visible()[0])
	m = u.(Model)
	p := layerOf[*reviewOverviewPopup](m)
	if p == nil {
		t.Fatalf("no overview popup; top %T", m.topLayer())
	}
	var b strings.Builder
	for _, l := range p.lines {
		b.WriteString(l.text + "\n")
	}
	got := b.String()
	for _, want := range []string{"Overview", "Looks fine, see A.", "verdict: approve", "Other notes", "zzz.go:1 — not in this commit"} {
		if !strings.Contains(got, want) {
			t.Errorf("overview lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "##") || strings.Contains(got, "`") {
		t.Errorf("markdown markers left in:\n%s", got)
	}
}

func openReviewDiff(t *testing.T, m Model, path string) Model {
	t.Helper()
	l, _ := filesLine(t, m, path)
	u, cmd := m.openDiffForFileLine(l)
	return drainCmds(t, u.(Model), cmd)
}

func TestReviewDiffShowsOnlyReviewNotes(t *testing.T) {
	t.Parallel()
	m, _ := reviewViewModel(t, reviewViewDoc)
	sha := m.commits[0].Hash
	if _, err := m.svc.NoteAdd(context.Background(), model.Note{Source: model.NoteSourceUser, Author: "me",
		Address: model.FileAddress{State: model.StateCommitted, Commit: sha, Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "stored"}); err != nil {
		t.Fatal(err)
	}
	var id string
	rs, _ := m.svc.ReviewsForCommit(context.Background(), sha)
	id = rs[0].ID
	rm, cmd := m.openReview(id, "Review")
	rm = drainCmds(t, rm, cmd)
	rm = openReviewDiff(t, rm, "a.go")
	v := rm.diffLayer()
	if v == nil || len(v.notes) != 1 || !model.IsReviewNoteID(v.notes[0].Note.ID) {
		t.Fatalf("review diff notes %+v", v.notes)
	}
	if title := v.noteBoxTitle(v.notes[0]); !strings.Contains(title, "severity: nit") || !strings.Contains(title, "Claude") {
		t.Fatalf("title %q", title)
	}
	// The same file from the normal commit view shows only the stored note.
	nm, cmd := m.openChangedFiles(m.commits[0])
	nm = drainCmds(t, nm, cmd)
	nm.filesTreeFocused = true
	nm = openReviewDiff(t, nm, "a.go")
	nv := nm.diffLayer()
	if nv == nil || len(nv.notes) != 1 || nv.notes[0].Note.Summary != "stored" {
		t.Fatalf("commit diff notes %+v", nv.notes)
	}
}

func TestReviewNoteCannotBeReplied(t *testing.T) {
	t.Parallel()
	m, _ := openedReviewView(t)
	m = openReviewDiff(t, m, "a.go")
	v := m.diffLayer()
	li, _ := v.noteAnchorLine(v.notes[0])
	v.setCursorLine(li, m.diffBodyRows())
	for _, key := range []string{"R", "E", "c"} {
		nm, _ := v.update(m, synthKey(key))
		if _, open := nm.topLayer().(*notePopup); open {
			t.Fatalf("%s opened a note form in the review view", key)
		}
		if !strings.Contains(nm.diffNotice, "read-only") {
			t.Fatalf("%s: notice %q", key, nm.diffNotice)
		}
	}
}

func TestAtNotesEntryOpensTheReviewView(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, reviewViewDoc)
	m, cmd := m.openChangedFiles(m.commits[0])
	m = drainCmds(t, m, cmd)
	var entry contentLine
	for _, l := range m.filesView.visible() {
		if l.noteID == id {
			entry = l
		}
	}
	if entry.noteID == "" {
		t.Fatal("no @notes/ entry in the commit's files")
	}
	u, cmd := m.openDiffForFileLine(entry)
	m = drainCmds(t, u.(Model), cmd)
	if m.filesReview == nil || m.filesReview.id != id || m.diffLayer() != nil {
		t.Fatalf("review view not open (review %+v, diff %v)", m.filesReview, m.diffLayer() != nil)
	}
}

func TestUnstructuredReviewOpensMarkdownViewer(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, "# Findings\nfour `bugs`")
	m, cmd := m.openReview(id, "Review")
	m = drainCmds(t, m, cmd)
	if m.filesReview != nil {
		t.Fatal("a prose review opens no review view")
	}
	v := layerOf[*fileViewer](m)
	if v == nil || v.src.kind != srcNote || !v.markdown {
		t.Fatalf("viewer %+v", v)
	}
	got := viewerText(m)
	if !strings.Contains(got, "Findings") || strings.Contains(got, "# Findings") || strings.Contains(got, "`") {
		t.Fatalf("viewer text:\n%s", got)
	}
}

func TestReviewViewEscReturns(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, reviewViewDoc)
	m, _ = m.openAllNotes()
	if m.topLayer() == nil {
		t.Fatal("setup: no popup")
	}
	m, cmd := m.openReview(id, "Review")
	m = drainCmds(t, m, cmd)
	if m.filesReview == nil || m.topLayer() != nil {
		t.Fatalf("review view not in front (top %T)", m.topLayer())
	}
	m, _ = updateKey(m, "esc")
	if m.filesView != nil || m.filesReview != nil {
		t.Fatal("esc must close the review view")
	}
	if _, ok := m.topLayer().(*allNotesPopup); !ok {
		t.Fatalf("esc must return to View all notes, top %T", m.topLayer())
	}
}

func TestReviewViewNPStepsFilesWithNotes(t *testing.T) {
	t.Parallel()
	m, _ := openedReviewView(t)
	_, ai := filesLine(t, m, "a.go")
	m.filesView.sel = 0
	m, _ = updateKey(m, "n")
	if m.filesView.sel != ai {
		t.Fatalf("n: sel %d, want the a.go row %d", m.filesView.sel, ai)
	}
	m, _ = updateKey(m, "n") // no later file with notes: stays
	if m.filesView.sel != ai {
		t.Fatalf("n at the last: sel %d", m.filesView.sel)
	}
	m.filesView.sel = len(m.filesView.visible()) - 1
	m, _ = updateKey(m, "p")
	if m.filesView.sel != ai {
		t.Fatalf("p: sel %d, want %d", m.filesView.sel, ai)
	}
}

// Stacked, the review's overview is the first element: its own header, then
// the rendered markdown, then the files; N from it lands on the first file.
func TestReviewStackPutsTheOverviewFirst(t *testing.T) {
	t.Parallel()
	m, _ := openedReviewView(t)
	m = m.setStackedPref(true)
	l, _ := filesLine(t, m, "a.go")
	u, cmd := m.openDiffForFileLine(l)
	m = drainCmds(t, u.(Model), cmd)
	v := m.diffLayer()
	if v == nil || v.stk == nil || len(v.stk.files) == 0 || !v.stk.files[0].overview {
		t.Fatalf("stack %+v, want the overview first", v)
	}
	w, _ := m.overlayDims()
	hdr := ansi.Strip(m.stackRow(v, dRow{line: v.stk.files[0].hdr, kind: lineHeader}, w, false))
	if !strings.Contains(hdr, "Overview") {
		t.Fatalf("overview header %q", hdr)
	}
	var prose []string
	for i, ln := range v.lines {
		if ln.kind == lineProse {
			prose = append(prose, ansi.Strip(m.stackRow(v, dRow{line: i, kind: lineProse}, w, false)))
		}
	}
	text := strings.Join(prose, "\n")
	if !strings.Contains(text, "Overview") || !strings.Contains(text, "Looks fine, see A.") || strings.Contains(text, "##") {
		t.Fatalf("overview rows:\n%s", text)
	}
	for _, f := range v.stk.files[1:] {
		if f.overview || f.path == "" {
			t.Fatalf("only the first element is the overview: %+v", f)
		}
	}
	v.goToStackFile(0, m.diffBodyRows())
	m, _ = updateKey(m, "N")
	if got := m.diffLayer().curFile(); got != 1 {
		t.Fatalf("N from the overview: file %d, want 1", got)
	}
}

// Through the real key path: enter on ≡ Overview opens the overview.
func TestReviewOverviewOpensOnEnterKey(t *testing.T) {
	t.Parallel()
	m, _ := openedReviewView(t)
	m.filesView.sel = 0
	m, _ = updateKey(m, "enter")
	if layerOf[*reviewOverviewPopup](m) == nil {
		t.Fatalf("enter on the Overview row: top %T", m.topLayer())
	}
}

// Opened from a commit's @notes/ entry, esc goes back to that commit's files,
// not past them.
func TestReviewFromNotesEntryEscReturnsToTheCommitFiles(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, reviewViewDoc)
	m, cmd := m.openChangedFiles(m.commits[0])
	m = drainCmds(t, m, cmd)
	m.filesTreeFocused = true
	for i, l := range m.filesView.visible() {
		if l.noteID == id {
			m.filesView.sel = i
		}
	}
	m, cmd = updateKey(m, "enter")
	m = drainCmds(t, m, cmd)
	if m.filesReview == nil {
		t.Fatal("setup: the review view did not open")
	}
	m, cmd = updateKey(m, "esc")
	m = drainCmds(t, m, cmd)
	if m.filesView == nil || m.filesReview != nil {
		t.Fatalf("esc: files view open=%v review=%v, want the commit's files", m.filesView != nil, m.filesReview)
	}
	found := false
	for _, l := range m.filesView.visible() {
		found = found || l.noteID == id
	}
	if !found {
		t.Fatal("esc did not return to the commit's file list (no @notes/ entry)")
	}
	m, _ = updateKey(m, "esc")
	if m.filesView != nil {
		t.Fatal("a second esc closes the commit's files")
	}
}
