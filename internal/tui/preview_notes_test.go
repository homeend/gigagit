package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
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

	m := previewDiffModel(t, []domain.ResolvedNote{note("n1", tip, 1), note("n2", older, 0)})
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
	// Both sides of "(N of M)" count NOTES, never notes against threads: the
	// tip's root and its reply are removable, out of the 3 notes the preview
	// shows (tip root + reply, plus the older commit's root).
	if p.total != 3 {
		t.Fatalf("the total counts every NOTE the preview shows, got %d", p.total)
	}
	if p.tip != shortHash(tip) {
		t.Fatalf("the popup names the tip it is scoped to, got %q", p.tip)
	}
	if body := p.box(tm.(Model)); !contains(body, "(2 of 3)") {
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
// the preview's per-path counts, not the tip-keyed map — a note written on an
// older commit of the branch is the preview's, and ByCommitPath would miss it.
func TestPreviewNotedFileStepUsesThePreviewCounts(t *testing.T) {
	t.Parallel()
	const tip = "1111111111111111111111111111111111111111"
	m := previewDiffModel(t, nil)
	m.filesPreviewCounts = map[string]int{"b.txt": 2}
	m.noteCounts = domain.NoteCounts{
		ByPath:       map[string]int{"tiponly.txt": 1},
		ByCommitPath: map[string]int{tip + ":tiponly.txt": 1},
		ByCommit:     map[string]int{},
	}
	if !m.notedFilePath("b.txt") {
		t.Fatal("a path the preview counts carries notes")
	}
	if m.notedFilePath("tiponly.txt") {
		t.Fatal("the preview steps by ITS counts, never the tip-keyed map")
	}

	// Outside a preview the tip-keyed map still decides, exactly as before.
	// The scope is the OPEN VIEW's stamp, so clearing the Model field alone
	// must not change the answer.
	m.filesPreviewSet = nil
	if !m.notedFilePath("b.txt") {
		t.Fatal("the open preview diff's own stamp is what scopes the step")
	}
	m.diffLayer().previewSet = nil
	if !m.notedFilePath("tiponly.txt") {
		t.Fatal("a plain commit diff still steps by the commit-keyed counts")
	}
	if m.notedFilePath("b.txt") {
		t.Fatal("the preview counts must not leak into a plain commit diff")
	}
}

// Ruling 2: a file DELETED at the tip still counts for the panel badge (spec
// §1.2's retired file), but it has no new side, so its notes can never render.
// `}`/`{` must not park the user on a diff that shows nothing.
func TestPreviewNotedFileStepSkipsFilesDeletedAtTheTip(t *testing.T) {
	t.Parallel()
	m := previewDiffModel(t, nil)
	m.filesPreviewCounts = map[string]int{"gone.txt": 1, "kept.txt": 1}
	m.filesView = &contentPopup{lines: []contentLine{
		{path: "gone.txt", status: "D"},
		{path: "kept.txt", status: "M"},
	}}
	if m.notedFilePath("gone.txt") {
		t.Fatal("a file deleted at the tip can show no new-side note: step past it")
	}
	if !m.notedFilePath("kept.txt") {
		t.Fatal("a modified file with a preview note is still a step target")
	}
}

// Ruling 4 / gate 8: steering may not mark the OLD side of a preview — the old
// side is the merge base, which notes cannot anchor on, the same refusal the
// TUI's own `c` gives there. The new side of the very same view must still
// mark, or the guard would just be "previews refuse steering".
func TestSteerHighlightRefusesTheOldSideOfAPreview(t *testing.T) {
	t.Parallel()
	const tip = "1111111111111111111111111111111111111111"
	m := previewDiffModel(t, nil)
	// resolveAttnKey normalises a commit target onto the FEED's hash, so the
	// preview's tip has to be a loaded commit or every command below would be
	// refused with "commit not loaded in the feed" and the test would pass
	// without the guard.
	m.commits = []model.Commit{{Hash: tip, Subject: "seed"}}
	m = m.rebuildCommitGraph()
	dir := filepath.Join(t.TempDir(), "steer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	m.steerDir = dir
	cmdFor := func(id, side string) steer.Command {
		return steer.Command{
			ID: id, Cmd: "highlight", File: "a.txt",
			Target: &steer.Target{State: "commit", Commit: tip},
			Side:   side, Start: 1, Tone: "info", Wait: true,
		}
	}

	mNew, okCmd := m.steerHighlight(cmdFor("ps-1", "new"))
	runSteerCmd(t, okCmd)
	if r, ok := steer.AwaitReply(dir, "ps-1", time.Second); !ok || !r.OK {
		t.Fatalf("a new-side mark on a preview must still land, reply=%+v ok=%v", r, ok)
	}
	if len(mNew.attention) != 1 {
		t.Fatalf("the new-side mark must be stored, got %+v", mNew.attention)
	}

	m2, badCmd := m.steerHighlight(cmdFor("ps-2", "old"))
	runSteerCmd(t, badCmd)
	r, ok := steer.AwaitReply(dir, "ps-2", time.Second)
	if !ok || r.OK || r.Error != "notes in a preview anchor on the new side" {
		t.Fatalf("want the old-side refusal with the protocol text, reply=%+v ok=%v", r, ok)
	}
	if len(m2.attention) != 0 {
		t.Fatalf("an old-side mark must be refused on a preview view, got %+v", m2.attention)
	}

	// The guard is scoped to THIS view's address, not to "a preview is on the
	// layer stack": an old-side mark aimed at some other file/state is the
	// business of the view that will paint it, and must still land.
	other := cmdFor("ps-3", "old")
	other.ID, other.File, other.Target = "ps-3", "b.txt", nil // b.txt, unstaged
	m3, otherCmd := m.steerHighlight(other)
	runSteerCmd(t, otherCmd)
	if r, ok := steer.AwaitReply(dir, "ps-3", time.Second); !ok || !r.OK {
		t.Fatalf("an old-side mark on ANOTHER file must not be refused, reply=%+v ok=%v", r, ok)
	}
	if len(m3.attention) != 1 {
		t.Fatalf("the other file's old-side mark must be stored, got %+v", m3.attention)
	}
}

// The Previews panel row carries the SAME ◆N badge every other note-bearing
// row uses (never a new glyph).
func TestPreviewRowCarriesANoteBadge(t *testing.T) {
	t.Parallel()
	m := Model{previews: []previewRow{{
		rec:   model.MergePreview{ID: "p1", Label: "login", Source: "feat", Target: "main"},
		sum:   domain.PreviewSummary{State: domain.PreviewOK, Files: 2, Ahead: 3},
		notes: 4,
	}}}
	rows := m.previewRows()
	if len(rows) != 1 || !contains(rows[0], noteBadge(4)) {
		t.Fatalf("want the ◆4 badge on the preview row, got %q", rows)
	}
}

// Ruling 6: a non-ok pair shows no badge at all.
func TestPreviewRowWithoutNotesHasNoBadge(t *testing.T) {
	t.Parallel()
	m := Model{previews: []previewRow{{
		rec: model.MergePreview{ID: "p1", Label: "merged", Source: "feat", Target: "main"},
		sum: domain.PreviewSummary{State: domain.PreviewMerged},
	}}}
	if rows := m.previewRows(); contains(rows[0], "◆") {
		t.Fatalf("a merged pair carries no note badge, got %q", rows[0])
	}
}

// After a note write the pair's HASHES are unchanged, so afterPreviewsRefresh
// takes its early-return branch — which must still take the fresh counts, or
// the file-list badges and }/{ go stale in the only case that matters.
func TestUnchangedPreviewRefreshStillMovesTheCounts(t *testing.T) {
	t.Parallel()
	m := previewDiffModel(t, nil)
	m.filesView = &contentPopup{}
	m.filesPreviewCounts = map[string]int{}
	m.previewOpen = &previewOpenState{id: "p1", source: "feat", target: "main",
		srcHash: "src", tgtHash: "tgt"}
	m.previews = []previewRow{{
		rec:    model.MergePreview{ID: "p1", Source: "feat", Target: "main"},
		sum:    domain.PreviewSummary{State: domain.PreviewOK, SourceHash: "src", TargetHash: "tgt"},
		notes:  1,
		byPath: map[string]int{"a.txt": 1},
	}}
	m2, cmd := m.afterPreviewsRefresh()
	if cmd != nil {
		t.Fatal("an unchanged pair must not re-resolve or re-open anything")
	}
	if m2.filesPreviewCounts["a.txt"] != 1 {
		t.Fatalf("an unchanged-hash refresh must still take the fresh counts, got %v", m2.filesPreviewCounts)
	}
}

// Ruling 7 + 8: a notes arrival re-reads the previews (the badges count the
// same store), but ONLY while a preview is on screen — and always through
// chainPreviewsRead, never a plain reloadSourcesCmd.
func TestNotesArrivalChainsThePreviewsRead(t *testing.T) {
	t.Parallel()
	arrive := func(m Model) Model {
		m, _ = m.reloadSourcesCmd([]sourceKey{srcNotes}, reloadOpts{})
		tm, _ := m.Update(dataAvailableMsg{source: srcNotes, gen: m.srcGen[srcNotes],
			value: domain.NoteCounts{}})
		return tm.(Model)
	}

	// A preview diff is open: the srcNotes arm returns loadNotesCmd early, so
	// the chain has to be armed INSIDE it or the previews never refresh.
	open := previewDiffModel(t, nil)
	open.previewOpen = &previewOpenState{id: "p1", source: "feat", target: "main"}
	if got := arrive(open); !got.srcInflight[srcPreviews] {
		t.Fatal("a notes arrival with a preview open must chain a previews read")
	}

	// The Previews tab is showing, no preview open: still chained.
	tab := Model{activeLeftTab: panelPreviews}
	if got := arrive(tab); !got.srcInflight[srcPreviews] {
		t.Fatal("a notes arrival while the Previews tab is active must chain a previews read")
	}

	// Neither: a note write in any repo must not spend a resolve per saved pair.
	if got := arrive(Model{}); got.srcInflight[srcPreviews] {
		t.Fatal("no preview on screen ⇒ no previews read")
	}
}

// The open preview's file list carries the same ◆N badge, from the preview's
// per-path counts. A plain compare (no preview set) is byte-identical to before.
func TestPreviewFileListRowsCarryTheBadge(t *testing.T) {
	t.Parallel()
	const tip = "1111111111111111111111111111111111111111"
	set := &domain.PreviewNoteSet{Source: "feat", Target: "main", Tip: tip}
	m := Model{width: 100, height: 40}
	m.filesMode = filesModeCompare
	m.filesTitle = "Merge preview: feat → main"
	m.filesView = &contentPopup{lines: []contentLine{
		{text: "M  a.txt", path: "a.txt"},
		{text: "M  b.txt", path: "b.txt"},
		{text: "sub/", heading: true},
	}}
	m.filesPreviewSet = set
	m.filesPreviewCounts = map[string]int{"a.txt": 2}

	out := m.renderFilesView(60, 20)
	if !contains(out, "a.txt"+noteBadge(2)) {
		t.Fatalf("the preview file row wants the ◆2 badge:\n%s", out)
	}
	// A file the preview counts at 0 carries NO badge: exactly one ◆ on screen.
	if n := strings.Count(out, "◆"); n != 1 {
		t.Fatalf("want exactly one badge (a.txt's), got %d:\n%s", n, out)
	}
	// The badge is DISPLAY only: the / filter still matches the bare row text,
	// so typing a digit never selects a file by its note count.
	m.filesView.query = "2"
	if len(m.filesView.visible()) != 0 {
		t.Fatal("the note count must not be part of the filter haystack")
	}

	// No preview: the rows are exactly what they were.
	plain := m
	plain.filesPreviewSet = nil
	if contains(plain.renderFilesView(60, 20), "◆") {
		t.Fatal("a plain compare file list carries no preview badges")
	}
}
