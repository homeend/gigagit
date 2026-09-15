package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// previewDiffModel builds a Model whose top layer is a preview diff view: a
// two-sided compare stamped with a tip address and a preview set.
func previewDiffModel(t *testing.T, notes []domain.ResolvedNote) Model {
	t.Helper()
	const tip = "1111111111111111111111111111111111111111"
	set := &domain.PreviewNoteSet{Source: "feat", Target: "main", Tip: tip, Commits: []string{tip}}
	v := &diffView{
		title: "a.txt", compare: true, rev: tip, width: 100,
		noteAddr:   model.FileAddress{State: model.StateCommitted, Commit: tip, Path: "a.txt"},
		previewSet: set,
		notes:      notes,
		full: []textdiff.Row{
			{Kind: textdiff.Same, Left: "alpha", Right: "alpha", LeftNo: 1, RightNo: 1},
			{Kind: textdiff.Del, Left: "gone", LeftNo: 2},
			{Kind: textdiff.Add, Right: "added", RightNo: 2},
		},
	}
	v.rebuild()
	m := Model{width: 100, height: 40}
	m.filesPreviewSet = set
	m = m.pushLayer(v) // pushLayer returns Model, not tea.Model — no assertion
	return m
}

// Gate 4 + 5: the cursor on an old-side-only row offers no anchor at all, and
// `c` there posts the refusal notice instead of opening the form.
func TestPreviewDiffRefusesAnOldSideNote(t *testing.T) {
	t.Parallel()
	m := previewDiffModel(t, nil)
	v := m.diffLayer()
	// Put the cursor on the Del row (old side only).
	for i, ln := range v.lines {
		if ln.Row.LeftNo == 2 && ln.Row.RightNo == 0 {
			v.curLine = i
		}
	}
	if as := m.noteAnchorsAtCursor(); len(as) != 0 {
		t.Fatalf("a preview offers no old-side anchor, got %+v", as)
	}
	tm, _ := m.openNotePopup(noteAdd)
	m2 := tm.(Model)
	if _, ok := m2.topLayer().(*notePopup); ok {
		t.Fatal("the note form must not open on an old-side preview row")
	}
	if m2.statusMsg != "notes in a preview anchor on the new side" {
		t.Fatalf("want the refusal notice, got %q", m2.statusMsg)
	}
}

// Gate 4: a row that exists on both sides offers ONLY the new side.
func TestPreviewDiffOffersOnlyTheNewSide(t *testing.T) {
	t.Parallel()
	m := previewDiffModel(t, nil)
	v := m.diffLayer()
	for i, ln := range v.lines {
		if ln.Row.RightNo == 1 && ln.Row.LeftNo == 1 {
			v.curLine = i
		}
	}
	as := m.noteAnchorsAtCursor()
	if len(as) != 1 || as[0].side != model.NoteSideNew {
		t.Fatalf("want exactly one new-side anchor, got %+v", as)
	}
}

// Gate 7: a stale note renders as outdated, with the ⊘ marker, in a preview.
func TestPreviewDiffTitleSaysOutdated(t *testing.T) {
	t.Parallel()
	r := domain.ResolvedNote{
		Note:   model.Note{ID: "n1", Source: model.NoteSourceAgent, Author: "ada", Side: model.NoteSideNew, Summary: "why"},
		Status: model.NoteStale, Range: [2]int{1, 1},
	}
	m := previewDiffModel(t, []domain.ResolvedNote{r})
	title := m.diffLayer().noteBoxTitle(r)
	if !contains(title, "(outdated)") || !contains(title, "⊘") {
		t.Fatalf("a preview marks a stale note outdated with ⊘, got %q", title)
	}
	if contains(title, "(stale)") {
		t.Fatalf("a preview must not say stale, got %q", title)
	}
	// A NON-preview view keeps the old word.
	plain := &diffView{noteAddr: model.FileAddress{Path: "a.txt"}}
	if !contains(plain.noteBoxTitle(r), "(stale)") {
		t.Fatal("a plain commit diff still says (stale)")
	}
}

// The compare loader builds a FRESH diffView and diffMsg then does
// `*dv = *msg.view` — so anything the opener stamped is lost unless the loader
// inherits it. This is the one trap in the whole task, so it is tested on the
// inheritance step itself, not on a view the test already filled in.
func TestCompareLoaderInheritsThePreviewStamp(t *testing.T) {
	t.Parallel()
	opener := previewDiffModel(t, nil).diffLayer()
	fresh := &diffView{title: "a.txt"} // what loadCompareDiffCmd starts from
	fresh.inheritIdentity(opener)
	if fresh.previewSet != opener.previewSet {
		t.Fatal("the loader must carry the preview set across the rebuild")
	}
	if fresh.noteAddr != opener.noteAddr {
		t.Fatalf("the loader must carry the note address, got %+v", fresh.noteAddr)
	}
	if fresh.rev != opener.rev || fresh.context != opener.context {
		t.Fatal("the loader must keep carrying rev and context, as it did before")
	}
	// A nil opener (a fresh open, no layer yet) must be a no-op, not a panic.
	(&diffView{}).inheritIdentity(nil)
}

// Gate 6: "Remove all notes…" is scoped to the TIP. NotesClear takes ONE
// address, so a note gathered from an older commit on the branch cannot be
// removed by it — it must not be counted as if it were, and when NOTHING on
// the tip is removable the row itself must not be offered.
func TestPreviewRemoveAllIsScopedToTheTip(t *testing.T) {
	t.Parallel()
	const tip = "1111111111111111111111111111111111111111"
	const older = "2222222222222222222222222222222222222222"
	note := func(id, commit string, replies int) domain.ResolvedNote {
		r := domain.ResolvedNote{
			Note:   model.Note{ID: id, Side: model.NoteSideNew, Address: model.FileAddress{State: model.StateCommitted, Commit: commit, Path: "a.txt"}},
			Status: model.NoteActive, Range: [2]int{1, 1},
		}
		for i := 0; i < replies; i++ {
			r.Replies = append(r.Replies, domain.ResolvedNote{Note: model.Note{ID: id + "-r"}})
		}
		return r
	}

	m := previewDiffModel(t, []domain.ResolvedNote{note("n1", tip, 1), note("n2", older, 2)})
	if _, ok := m.noteRemoveAllRow(); !ok {
		t.Fatal("the row is offered while a tip note is visible")
	}
	tm, _ := m.openNoteRemoveAll()
	p, ok := tm.(Model).topLayer().(*noteRemoveAllPopup)
	if !ok {
		t.Fatal("openNoteRemoveAll must push the confirmation popup")
	}
	if p.roots != 1 || p.replies != 1 {
		t.Fatalf("only the tip's thread is removable, got roots=%d replies=%d", p.roots, p.replies)
	}
	if p.total != 2 {
		t.Fatalf("the total names every thread the preview shows, got %d", p.total)
	}
	if p.tip != shortHash(tip) {
		t.Fatalf("the popup names the tip it is scoped to, got %q", p.tip)
	}
	if body := p.box(tm.(Model)); !contains(body, "(2 of 2)") {
		t.Fatalf("the preview lead names the removable slice of the total, got %q", body)
	}

	// Nothing on the tip: offering a gesture that removes nothing is worse
	// than not offering it.
	m2 := previewDiffModel(t, []domain.ResolvedNote{note("n2", older, 0)})
	if _, ok := m2.noteRemoveAllRow(); ok {
		t.Fatal("no tip note ⇒ no row: NotesClear(tip) would remove nothing")
	}

	// A plain commit diff is unchanged: every visible thread is the address's.
	m3 := previewDiffModel(t, []domain.ResolvedNote{note("n2", older, 1)})
	m3.diffLayer().previewSet = nil
	m3.filesPreviewSet = nil
	if _, ok := m3.noteRemoveAllRow(); !ok {
		t.Fatal("a non-preview diff still offers the row")
	}
	tm3, _ := m3.openNoteRemoveAll()
	p3 := tm3.(Model).topLayer().(*noteRemoveAllPopup)
	if p3.roots != 1 || p3.replies != 1 || p3.tip != "" {
		t.Fatalf("a non-preview counts every visible thread and names no tip, got %+v", p3)
	}
}

// Gate 3 (spec §1.6, the `}`/`{` step): in a preview the noted-file test reads
// the preview's per-path counts. Those counts come from PreviewNoteCounts,
// which only counts notes attributable to this preview — a note left behind on
// a commit the branch no longer contains is hidden, so the step passes its file
// by even though the tip-keyed map still knows about it.
func TestPreviewNotedFileStepUsesThePreviewCounts(t *testing.T) {
	t.Parallel()
	const tip = "1111111111111111111111111111111111111111"
	m := previewDiffModel(t, nil)
	m.filesPreviewCounts = map[string]int{"b.txt": 2}
	m.noteCounts = domain.NoteCounts{
		ByPath:       map[string]int{"orphan.txt": 1},
		ByCommitPath: map[string]int{tip + ":orphan.txt": 1},
		ByCommit:     map[string]int{},
	}
	if !m.notedFilePath("b.txt") {
		t.Fatal("a path the preview counts carries notes")
	}
	if m.notedFilePath("orphan.txt") {
		t.Fatal("a note hidden from this preview must not stop the }/{ step")
	}

	// Outside a preview the tip-keyed map still decides, exactly as before.
	m.filesPreviewSet = nil
	if !m.notedFilePath("orphan.txt") {
		t.Fatal("a plain commit diff still steps by the commit-keyed counts")
	}
	if m.notedFilePath("b.txt") {
		t.Fatal("the preview counts must not leak into a plain commit diff")
	}
}
