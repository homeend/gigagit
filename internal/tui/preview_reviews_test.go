package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// reviewPreviewsModel: two saved rows — a merge preview "login" with two
// reviews (a current one and one of an older tip) and a comparison row,
// focused on the Previews tab.
func reviewPreviewsModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t)
	m.loading = false
	now := time.Now()
	m.previews = []previewRow{
		{rec: model.MergePreview{ID: "p1", Label: "login", Source: "feat/x", Target: "main", Created: now},
			sum: domain.PreviewSummary{State: domain.PreviewOK, Files: 1, Ahead: 1},
			reviews: []domain.ReviewHead{
				{ID: "r2", Agent: "Claude Code", Created: now.Add(-time.Hour), Remarks: 3, Resolved: 1, Preview: "main...feat/x"},
				{ID: "r1", Agent: "Codex", Created: now.Add(-48 * time.Hour), Preview: "main...feat/x", Older: true},
			}},
		{kind: rowCompare, cmp: domain.SavedCompare{ID: "c1", Label: "cmp", Created: now.Add(-time.Minute)},
			cmpDesc: [2]string{"a", "b"}},
	}
	m = m.activateTab(panelPreviews)
	return m
}

func TestPreviewRowsShowReviewSubRows(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	rows, _ := m.panelView(panelPreviews)
	if len(rows) != 4 {
		t.Fatalf("rows %q, want login, its two reviews, cmp", rows)
	}
	if !strings.Contains(rows[0], "login") || !strings.Contains(rows[3], "cmp") {
		t.Fatalf("parent rows out of place: %q", rows)
	}
	cur := reviewStampAt(m.previews[0].reviews[0].Created, time.Now())
	if !strings.Contains(rows[1], "└ Review: "+cur+" Claude Code") || strings.Contains(rows[1], "older tip") {
		t.Fatalf("current review row %q", rows[1])
	}
	if !strings.Contains(rows[2], "Codex") || !strings.HasSuffix(strings.TrimRight(rows[2], " "), "· older tip") {
		t.Fatalf("older review row %q, want it to end with · older tip", rows[2])
	}
}

func TestPreviewSubRowsDoNotShiftRowActions(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	m.sel[panelPreviews] = 1 // a review sub-row
	if r, ok := m.selectedPreview(); ok {
		t.Fatalf("a sub-row resolved to preview %q", r.id())
	}
	if h, ok := m.selectedPreviewReview(); !ok || h.ID != "r2" {
		t.Fatalf("selectedPreviewReview = %+v %v", h, ok)
	}
	m.sel[panelPreviews] = 3 // the comparison row, after two sub-rows
	if r, ok := m.selectedPreview(); !ok || r.id() != "c1" {
		t.Fatalf("row after the sub-rows = %+v %v, want c1", r.id(), ok)
	}
	if a, b := m.rowKeyAt(panelPreviews, 0), m.rowKeyAt(panelPreviews, 1); a != "p1" || a == b {
		t.Fatalf("keys %q %q: a preview row keys by its id, a sub-row distinctly", a, b)
	}
	// Marks address preview rows only, by id, in display order.
	m.previewCompareSet = map[string]bool{"c1": true}
	if got := m.previewMarkedSubjects(); got != "cmp" {
		t.Fatalf("marked subjects %q", got)
	}
}

func TestPreviewSubRowsFollowTheirRowUnderAFilter(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	m.filterQuery, m.filterPanel = "login", panelPreviews
	rows, _ := m.panelView(panelPreviews)
	if len(rows) != 3 || !strings.Contains(rows[1], "└ Review:") {
		t.Fatalf("filter on the preview: %q, want it and both reviews", rows)
	}
	m.filterQuery = "Codex"
	rows, _ = m.panelView(panelPreviews)
	if len(rows) == 0 || !strings.Contains(rows[0], "login") {
		t.Fatalf("filter on a review's agent: %q, want its preview kept", rows)
	}
}

func TestPreviewSteerLandingSkipsSubRows(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	if di, ok := m.previewDisplayIndex(1); !ok || di != 3 {
		t.Fatalf("display index of the comparison row = %d %v, want 3", di, ok)
	}
}

// A real store: a review saved on the preview shows as its sub-row after
// a previews read; once the source moves on it is an older tip.
func TestPreviewSubRowFromTheStore(t *testing.T) {
	t.Parallel()
	m, dir := storedPreviewReviewModel(t)
	rows, _ := m.panelView(panelPreviews)
	if len(rows) != 2 || !strings.Contains(rows[1], "└ Review:") || strings.Contains(rows[1], "older tip") {
		t.Fatalf("rows %q, want the preview and its current review", rows)
	}
	runGit(t, dir, "checkout", "-q", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "more")
	runGit(t, dir, "checkout", "-q", "main")
	m = readPreviewsNow(t, m)
	rows, _ = m.panelView(panelPreviews)
	if len(rows) != 2 || !strings.Contains(rows[1], "older tip") {
		t.Fatalf("rows %q, want the review marked older tip", rows)
	}
}

const previewReviewTestDoc = `{"version":1,"summary":"## Summary\nok","files":[{"path":"a.txt","annotations":[{"newRange":[1,1],"summary":"one"}]}]}`

// storedPreviewReviewModel: mergePreviewModel's repo with a note store and
// one review saved on the "login" preview (main...feat/x).
func storedPreviewReviewModel(t *testing.T) (Model, string) {
	t.Helper()
	dir, repo := newRepoDir(t)
	runGit(t, dir, "checkout", "-q", "-b", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "add a")
	runGit(t, dir, "checkout", "-q", "main")
	svc := domain.New(repo)
	svc.UsePreviewsDir(t.TempDir())
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	if _, err := svc.PreviewAdd(ctx, "feat/x", "main", "login"); err != nil {
		t.Fatal(err)
	}
	set, err := svc.PreviewNotes(ctx, "feat/x", "main")
	if err != nil || !set.OK() {
		t.Fatalf("PreviewNotes: %+v %v", set, err)
	}
	if _, _, err := svc.SaveReview(ctx, domain.SaveReview{Target: domain.ScopeReviewTarget(set), Agent: "Claude Code", Text: previewReviewTestDoc}); err != nil {
		t.Fatal(err)
	}
	m := New(svc)
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)
	m = readPreviewsNow(t, m)
	return m.activateTab(panelPreviews), dir
}

// readPreviewsNow runs one srcPreviews read to completion.
func readPreviewsNow(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ := m.Update(cmd())
	return updated.(Model)
}

func TestPreviewReviewSubRowEnterOpensTheReview(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	m.sel[panelPreviews] = 1
	_, cmd := updateKey(m, "enter")
	if cmd == nil {
		t.Fatal("enter on a review sub-row started nothing")
	}
	if msg, ok := cmd().(reviewViewMsg); !ok || msg.id != "r2" {
		t.Fatalf("enter read %#v, want the review r2", msg)
	}
}

// R6: Open + Copy gg link + Delete, and none of the preview row's actions.
func TestPreviewReviewSubRowMenu(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	m.sel[panelPreviews] = 2
	ids := menuIDs(m)
	for _, want := range []string{"open-review", "copy-gg-link", "delete-review"} {
		if !ids[want] {
			t.Errorf("sub-row menu lacks %q (%v)", want, ids)
		}
	}
	for _, not := range []string{"preview-open", "preview-rename", "preview-delete", "preview-swap", "preview-review", "preview-show-review", "copy-link"} {
		if ids[not] {
			t.Errorf("sub-row menu offers the preview's %q", not)
		}
	}
	if !strings.Contains(m.footerLine(), "open review") {
		t.Errorf("footer %q does not advertise enter on a review row", m.footerLine())
	}
}

func TestPreviewReviewRowGating(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "echo hi")}
	m.sel[panelPreviews] = 0
	if r, ok := m.previewReviewRow(); !ok || r.id != "preview-review" || r.label != "Review (AI)" {
		t.Fatalf("an ok preview: %+v %v", r, ok)
	}
	if !strings.Contains(m.footerLine(), "review (AI)") {
		t.Errorf("footer %q does not advertise Review (AI)", m.footerLine())
	}
	m.sel[panelPreviews] = 1 // a sub-row
	if _, ok := m.previewReviewRow(); ok {
		t.Fatal("offered on a review sub-row")
	}
	m.sel[panelPreviews] = 3 // a saved comparison
	if _, ok := m.previewReviewRow(); ok {
		t.Fatal("offered on a comparison row")
	}
	m.sel[panelPreviews] = 0
	m.previews[0].sum.State = domain.PreviewMerged
	if _, ok := m.previewReviewRow(); ok {
		t.Fatal("offered on a merged preview")
	}
	m.previews[0].sum.State = domain.PreviewOK
	m.cfg.Tools.Command = nil
	if _, ok := m.previewReviewRow(); ok {
		t.Fatal("offered with no review tool")
	}
}

func TestPreviewReviewRowTargetsTheScope(t *testing.T) {
	t.Parallel()
	m, _ := storedPreviewReviewModel(t)
	m.cfg.Tools.Command = []config.ToolCommand{captureCmd(exttool.CatReview, "echo hi")}
	m.sel[panelPreviews] = 0
	r, ok := m.previewReviewRow()
	if !ok {
		t.Fatal("row not offered")
	}
	_, cmd := r.run(m)
	msg, ok := cmd().(reviewTargetReadyMsg)
	if !ok || msg.err != nil {
		t.Fatalf("target hop = %#v", msg)
	}
	if msg.target.Preview != "main...feat/x" || !strings.Contains(msg.target.Range, "..") || msg.target.Kind != domain.ReviewRange {
		t.Fatalf("target %+v, want the preview's scope", msg.target)
	}
}

func TestPreviewShowReviewIsTheNewestCurrent(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	m.sel[panelPreviews] = 0
	r, ok := m.previewShowReviewRow()
	if !ok || r.id != "preview-show-review" {
		t.Fatalf("row %+v %v", r, ok)
	}
	_, cmd := r.run(m)
	if msg, ok := cmd().(reviewViewMsg); !ok || msg.id != "r2" {
		t.Fatalf("Show review opened %#v, want r2", msg)
	}
	m.previews[0].reviews = m.previews[0].reviews[1:] // only the older one left
	if _, ok := m.previewShowReviewRow(); ok {
		t.Fatal("Show review offered with no current review")
	}
}

// A finished review re-reads the Previews rows too: a preview review is a
// sub-row, and the run may end while the user is on another tab.
func TestReviewResultRereadsThePreviews(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	m = m.activateTab(panelBranches)
	nm, _ := m.applyReviewResult(domain.TaskInfo{Key: "review — x", NoteID: "r9"})
	if !nm.srcInflight[srcNotes] || !nm.srcInflight[srcPreviews] {
		t.Fatalf("in flight: notes %v previews %v", nm.srcInflight[srcNotes], nm.srcInflight[srcPreviews])
	}
}

// drainUntil runs cmd and every command its messages return, breadth-first,
// until done(m) holds or the queue is empty (tea.BatchMsg unwrapped). A
// command that does not answer within 2 s (a tick, a never-ending wait) is
// skipped.
func drainUntil(t *testing.T, m Model, cmd tea.Cmd, done func(Model) bool) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0 && !done(m) && steps < 500; steps++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		ch := make(chan tea.Msg, 1)
		go func() { ch <- c() }()
		var msg tea.Msg
		select {
		case msg = <-ch:
		case <-time.After(2 * time.Second):
			continue
		}
		if b, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, b...)
			continue
		}
		if msg == nil {
			continue
		}
		updated, next := m.Update(msg)
		m = updated.(Model)
		queue = append(queue, next)
	}
	if !done(m) {
		t.Fatalf("drainUntil: condition never held (status %q)", m.statusMsg)
	}
	return m
}

// drainUntilFilesListed drains until a compare file list with a Reviews
// block on top is on screen (no review view).
func drainUntilFilesListed(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	return drainUntil(t, m, cmd, func(m Model) bool {
		return m.filesView != nil && m.filesReview == nil && len(m.filesView.lines) > 0 &&
			!isLoadingPlaceholder(m.filesView.lines[0].text) && m.filesView.lines[0].heading
	})
}

// openStoredPreview opens Previews row 0 and lands its file list.
func openStoredPreview(t *testing.T, m Model) Model {
	t.Helper()
	m.sel[panelPreviews] = 0
	m, cmd := updateKey(m, "enter")
	return drainUntilFilesListed(t, m, cmd)
}

func TestOpenPreviewShowsItsReviewsOnTop(t *testing.T) {
	t.Parallel()
	m, _ := storedPreviewReviewModel(t)
	m = openStoredPreview(t, m)
	lines := m.filesView.lines
	if len(lines) < 3 || !lines[0].heading || lines[0].text != "Reviews" || lines[1].noteID == "" || !strings.Contains(lines[1].text, "└ Review:") {
		t.Fatalf("file list %+v, want a Reviews block on top", lines)
	}
	if lines[len(lines)-1].path != "a.txt" {
		t.Fatalf("the files follow the block: %+v", lines)
	}
}

func TestReviewFromPreviewEscReturnsToThePreview(t *testing.T) {
	t.Parallel()
	m, _ := storedPreviewReviewModel(t)
	m = openStoredPreview(t, m)
	id := m.filesView.lines[1].noteID
	m.filesView.sel = 1
	m, cmd := updateKey(m, "enter")
	m = drainUntil(t, m, cmd, func(m Model) bool { return m.filesReview != nil })
	if st := m.filesReview; st.backPreview == nil || st.backPreview.source != "feat/x" {
		t.Fatalf("review opened without a way back to its preview: %+v", st)
	}
	m, cmd = updateKey(m, "esc")
	m = drainUntilFilesListed(t, m, cmd)
	if m.filesReview != nil || m.previewOpen == nil || m.previewOpen.source != "feat/x" {
		t.Fatalf("esc did not return to the preview (review %v, preview %+v)", m.filesReview != nil, m.previewOpen)
	}
	vis := m.filesView.visible()
	if m.filesView.sel < 0 || m.filesView.sel >= len(vis) || vis[m.filesView.sel].noteID != id {
		t.Fatalf("cursor not back on the review's row (sel %d)", m.filesView.sel)
	}
}

// The way back is one-shot: a preview that no longer opens leaves the
// review on screen, and the next esc closes it — never an esc loop.
func TestReviewFromPreviewEscTwiceWhenThePreviewIsGone(t *testing.T) {
	t.Parallel()
	m, dir := storedPreviewReviewModel(t)
	m = openStoredPreview(t, m)
	m.filesView.sel = 1
	m, cmd := updateKey(m, "enter")
	m = drainUntil(t, m, cmd, func(m Model) bool { return m.filesReview != nil })
	runGit(t, dir, "branch", "-D", "feat/x")
	m, cmd = updateKey(m, "esc")
	m = drainUntil(t, m, cmd, func(m Model) bool { return m.statusMsg != "" })
	if m.filesReview == nil {
		t.Fatal("the review closed although its preview could not open")
	}
	m, cmd = updateKey(m, "esc")
	m = pumpAll(t, m, cmd)
	if m.filesView != nil {
		t.Fatalf("the second esc did not close the view (review %v)", m.filesReview != nil)
	}
}

func TestPreviewReviewLinesReplaceInPlace(t *testing.T) {
	t.Parallel()
	files := []contentLine{{text: "M  a.txt", path: "a.txt"}}
	h := domain.ReviewHead{ID: "r1", Agent: "A"}
	once := previewReviewLines([]domain.ReviewHead{h}, files)
	twice := previewReviewLines([]domain.ReviewHead{h, {ID: "r2", Agent: "B"}}, once)
	if len(once) != 3 || len(twice) != 4 || twice[3].path != "a.txt" {
		t.Fatalf("once %d lines, twice %d: %+v", len(once), len(twice), twice)
	}
	if none := previewReviewLines(nil, twice); len(none) != 1 || none[0].path != "a.txt" {
		t.Fatalf("no heads left a block: %+v", none)
	}
}

func TestOpenPreviewReviewsBlockFollowsARefresh(t *testing.T) {
	t.Parallel()
	m, _ := storedPreviewReviewModel(t)
	m = openStoredPreview(t, m)
	m = m.setPreviewReviews(nil)
	if m.filesView.lines[0].heading {
		t.Fatalf("an emptied scope kept its block: %+v", m.filesView.lines)
	}
	// A previews read whose tips did not move re-applies the row's heads.
	m = readPreviewsNow(t, m)
	if l := m.filesView.lines; !l[0].heading || l[1].noteID == "" {
		t.Fatalf("a refresh did not restore the block: %+v", l)
	}
}

// storedPairReviewModel: a saved commit pair main..feat/x ("pair one") with
// one review saved on it, the Previews tab read.
func storedPairReviewModel(t *testing.T) Model {
	t.Helper()
	dir, repo := newRepoDir(t)
	base := gitOut(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "checkout", "-q", "-b", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "add a")
	tip := gitOut(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "checkout", "-q", "main")
	svc := domain.New(repo)
	svc.UsePreviewsDir(t.TempDir())
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	if _, err := svc.PairAdd(ctx, base, tip, "pair one"); err != nil {
		t.Fatal(err)
	}
	set, err := svc.PairNotes(ctx, base, tip)
	if err != nil || !set.OK() {
		t.Fatalf("PairNotes: %+v %v", set, err)
	}
	if _, _, err := svc.SaveReview(ctx, domain.SaveReview{Target: domain.ScopeReviewTarget(set), Agent: "Claude Code", Text: previewReviewTestDoc}); err != nil {
		t.Fatal(err)
	}
	m := New(svc)
	updated, _ := m.Update(m.loadCmd()())
	m = readPreviewsNow(t, updated.(Model))
	return m.activateTab(panelPreviews)
}

func TestReviewFromPairEscReturnsToThePair(t *testing.T) {
	t.Parallel()
	m := storedPairReviewModel(t)
	rows, _ := m.panelView(panelPreviews)
	if len(rows) != 2 || !strings.Contains(rows[1], "└ Review:") {
		t.Fatalf("rows %q, want the pair and its review", rows)
	}
	m = openStoredPreview(t, m) // enter on row 0: the pair
	id := m.filesView.lines[1].noteID
	m.filesView.sel = 1
	m, cmd := updateKey(m, "enter")
	m = drainUntil(t, m, cmd, func(m Model) bool { return m.filesReview != nil })
	if bp := m.filesReview.backPreview; bp == nil || bp.pair == nil || bp.pair.Label != "pair one" {
		t.Fatalf("review opened without a way back to its pair: %+v", m.filesReview.backPreview)
	}
	m, cmd = updateKey(m, "esc")
	m = drainUntilFilesListed(t, m, cmd)
	if m.filesReview != nil || m.filesPreviewSet == nil || !m.filesPreviewSet.IsPair() || !strings.Contains(m.filesTitle, "pair one") {
		t.Fatalf("esc did not return to the pair (title %q)", m.filesTitle)
	}
	vis := m.filesView.visible()
	if m.filesView.sel < 0 || m.filesView.sel >= len(vis) || vis[m.filesView.sel].noteID != id {
		t.Fatalf("cursor not back on the review's row (sel %d)", m.filesView.sel)
	}
}

func TestReviewViewHeaderNamesThePreview(t *testing.T) {
	t.Parallel()
	if got := reviewLabel(domain.Review{Preview: "main...feat/x", Commit: strings.Repeat("a", 40)}); got != "feat/x → main" {
		t.Fatalf("reviewLabel = %q", got)
	}
	m, dir := storedPreviewReviewModel(t)
	id := m.previews[0].reviews[0].ID
	m, cmd := m.openReview(id, "x")
	m = drainUntil(t, m, cmd, func(m Model) bool { return m.filesReview != nil })
	if m.filesTitle != "Review: feat/x → main" {
		t.Fatalf("title %q", m.filesTitle)
	}
	runGit(t, dir, "checkout", "-q", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "more")
	runGit(t, dir, "checkout", "-q", "main")
	m, cmd = m.openReview(id, "x")
	m = drainUntil(t, m, cmd, func(m Model) bool { return m.filesReview != nil && m.filesReview.older })
	if m.filesTitle != "Review: feat/x → main · older tip" {
		t.Fatalf("title %q, want it to say older tip", m.filesTitle)
	}
}

// An agent saves a review out of band and navigates to it: the review opens,
// and the note counts and the Previews rows are re-read on the way, so its
// sub-row is there when the user looks.
func TestSteerToAFreshPreviewReviewReloadsTheRows(t *testing.T) {
	t.Parallel()
	m, _ := storedPreviewReviewModel(t)
	id := m.previews[0].reviews[0].ID
	m.previews[0].reviews = nil // the TUI has not seen it yet
	// On another tab: the srcNotes arrival chains a previews read only while
	// the Previews tab (or a preview) is on screen.
	m = m.activateTab(panelBranches)
	nm, cmd := m.onReviewHint(reviewHintMsg{svc: m.svc, cmd: steer.Command{Cmd: "navigate", HintKind: "review", HintID: id},
		commit: strings.Repeat("0", 40), preview: "main...feat/x", found: true})
	if !nm.srcInflight[srcNotes] {
		t.Fatal("a review hint did not re-read the note counts")
	}
	nm = drainUntil(t, nm, cmd, func(m Model) bool {
		return len(m.previews) > 0 && len(m.previews[0].reviews) == 1 && m.filesReview != nil
	})
	if st := nm.filesReview; st.back.Hash != "" {
		t.Fatalf("a preview review opened with a commit to return to: %+v", st.back)
	}
}
