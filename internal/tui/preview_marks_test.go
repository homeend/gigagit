package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// previewMarksModel is the Previews tab holding a merge preview (row 0,
// feat/x → main) and a commit pair (row 1, main..feat/x), with the checkout
// set so both rows' links are buildable. It returns the two link TEXTS the
// rows stand for — the ones a hand-run `gg compare <link> <link>` would take.
func previewMarksModel(t *testing.T) (m Model, mergeLink, pairLink string) {
	t.Helper()
	m, p := savedPairModel(t)
	if len(m.worktrees) == 0 {
		t.Fatal("fixture: no worktree to address links by")
	}
	m.currentWorktree = m.worktrees[0].Path
	mergeLink, ok := m.previewLinkFor("feat/x", "main", "", 0)
	if !ok || !strings.Contains(mergeLink, "main...feat/x") {
		t.Fatalf("fixture: merge preview link = %q, %v", mergeLink, ok)
	}
	pairLink, ok = m.pairLinkFor(p.A, p.B)
	if !ok || !strings.Contains(pairLink, "@"+p.A+".."+p.B) {
		t.Fatalf("fixture: pair link = %q, %v", pairLink, ok)
	}
	return m, mergeLink, pairLink
}

func previewSpace(t *testing.T, m Model, row int) (Model, tea.Cmd) {
	t.Helper()
	m.sel[panelPreviews] = row
	return send(m, keyType(tea.KeySpace))
}

func TestPreviewSecondSpaceComparesTheTwoRowsLinks(t *testing.T) {
	t.Parallel()
	m, mergeLink, pairLink := previewMarksModel(t)
	m, cmd := previewSpace(t, m, 0)
	if cmd != nil || m.linkCompareWant != "" || !m.previewCompareSet[m.previews[0].id()] {
		t.Fatalf("the first space only marks: cmd=%v want=%q set=%v", cmd, m.linkCompareWant, m.previewCompareSet)
	}
	if !strings.Contains(m.View(), "◉ ") {
		t.Fatalf("a marked row wears ◉:\n%s", m.View())
	}
	m, cmd = previewSpace(t, m, 1)
	if got, want := m.linkCompareWant, linkCompareTag(mergeLink, pairLink); got != want {
		t.Fatalf("second space must compare the rows' links\n got %q\nwant %q", got, want)
	}
	m = pumpAll(t, m, cmd)
	if m.filesSets == nil || m.filesSets.LeftText != mergeLink || m.filesSets.RightText != pairLink {
		t.Fatalf("the link comparison did not open (status %q, sets %+v)", m.statusMsg, m.filesSets)
	}
	if len(m.previewCompareSet) != 2 {
		t.Fatalf("marks persist across the open: %v", m.previewCompareSet)
	}
}

// Left is the UPPER row whichever was marked first.
func TestPreviewCompareOrderIsDisplayOrder(t *testing.T) {
	t.Parallel()
	m, mergeLink, pairLink := previewMarksModel(t)
	m, _ = previewSpace(t, m, 1)
	m, _ = previewSpace(t, m, 0)
	if got, want := m.linkCompareWant, linkCompareTag(mergeLink, pairLink); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPreviewSpaceTogglesOffAndRefusesAThird(t *testing.T) {
	t.Parallel()
	m, _, _ := previewMarksModel(t)
	if _, err := m.svc.PreviewAdd(context.Background(), "main", "feat/x", "back"); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	if len(m.previews) != 3 {
		t.Fatalf("fixture: %d rows", len(m.previews))
	}
	m, _ = previewSpace(t, m, 0)
	m, _ = previewSpace(t, m, 0)
	if len(m.previewCompareSet) != 0 {
		t.Fatalf("space on a marked row unmarks: %v", m.previewCompareSet)
	}
	m, _ = previewSpace(t, m, 0)
	m, _ = previewSpace(t, m, 1)
	m.linkCompareWant, m.statusMsg = "", ""
	m, cmd = previewSpace(t, m, 2)
	if cmd != nil || len(m.previewCompareSet) != 2 || m.linkCompareWant != "" || m.statusMsg == "" {
		t.Fatalf("a third mark is refused with a notice: cmd=%v set=%v status=%q", cmd, m.previewCompareSet, m.statusMsg)
	}
}

func TestSavedComparisonRowCannotBeMarked(t *testing.T) {
	t.Parallel()
	m, _, left, right := savedCompareModel(t)
	m, c := withSavedComparison(t, m, left, right, "cmp")
	m = selectPreviewRow(t, m, c.ID)
	for _, k := range []tea.KeyMsg{keyType(tea.KeySpace), keyMsg("m")} {
		m.statusMsg = ""
		m, _ = send(m, k)
		if len(m.previewCompareSet) != 0 || m.statusMsg == "" {
			t.Fatalf("%v: a saved comparison is two links — refuse and say so: set=%v status=%q", k, m.previewCompareSet, m.statusMsg)
		}
	}
}

func TestPreviewMKeyMarksAndTheMenuCompares(t *testing.T) {
	t.Parallel()
	m, mergeLink, pairLink := previewMarksModel(t)
	if _, ok := menuRow(t, m, "preview-compare-marked"); ok {
		t.Fatal("no compare row before two marks")
	}
	for i := 0; i < 2; i++ {
		m.sel[panelPreviews] = i
		var cmd tea.Cmd
		m, cmd = send(m, keyMsg("m"))
		if cmd != nil || m.linkCompareWant != "" {
			t.Fatalf("m never opens a diff (row %d)", i)
		}
	}
	r, ok := menuRow(t, m, "preview-compare-marked")
	if !ok {
		t.Fatal("two marks: the . menu offers the compare")
	}
	tm, _ := r.run(m)
	if got, want := tm.(Model).linkCompareWant, linkCompareTag(mergeLink, pairLink); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if _, ok := menuRow(t, m, "preview-marks-clear"); !ok {
		t.Fatal("marks must be menu-clearable")
	}
}

// A mark whose row was removed neither eats a slot nor feeds the compare.
func TestPreviewGhostMarkIsIgnored(t *testing.T) {
	t.Parallel()
	m, mergeLink, pairLink := previewMarksModel(t)
	m.previewCompareSet = map[string]bool{"gone": true}
	m, _ = previewSpace(t, m, 0)
	if m.linkCompareWant != "" {
		t.Fatal("a ghost must not count as the first mark")
	}
	m, _ = previewSpace(t, m, 1)
	if got, want := m.linkCompareWant, linkCompareTag(mergeLink, pairLink); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPreviewEscClearsMarks(t *testing.T) {
	t.Parallel()
	m, _, _ := previewMarksModel(t)
	m, _ = previewSpace(t, m, 0)
	m, _ = send(m, keyType(tea.KeyEsc))
	if len(m.previewCompareSet) != 0 {
		t.Fatalf("esc drops the marks: %v", m.previewCompareSet)
	}
}

// A mark the / filter hides still holds its slot and still feeds the compare.
func TestPreviewMarkSurvivesTheFilter(t *testing.T) {
	t.Parallel()
	m, mergeLink, pairLink := previewMarksModel(t)
	m, _ = previewSpace(t, m, 0)
	m.filterPanel, m.filterQuery = panelPreviews, "attempt" // the pair row's label: hides row 0
	if idx := m.displayIndices(panelPreviews); len(idx) != 1 || idx[0] != 1 {
		t.Fatalf("fixture: the filter must leave only the pair row, got %v", idx)
	}
	m, _ = previewSpace(t, m, 0)
	if got, want := m.linkCompareWant, linkCompareTag(pairLink, mergeLink); got != want {
		t.Fatalf("visible row first, hidden mark second\n got %q\nwant %q", got, want)
	}
}

// The load takes seconds on a slow mount and the view opens only when it
// lands: the key must SAY it is working, and a second space meanwhile must not
// unmark the row (the reported "I have to press it again" bug).
func TestPreviewCompareInFlightSaysSoAndSwallowsSpace(t *testing.T) {
	t.Parallel()
	m, _, _ := previewMarksModel(t)
	m, _ = previewSpace(t, m, 0)
	m, cmd := previewSpace(t, m, 1)
	if cmd == nil || m.statusMsg != "comparing…" {
		t.Fatalf("the second mark must announce the load: cmd=%v status=%q", cmd != nil, m.statusMsg)
	}
	m, again := previewSpace(t, m, 1)
	if again != nil || len(m.previewCompareSet) != 2 {
		t.Fatalf("space while loading is swallowed: cmd=%v set=%v", again != nil, m.previewCompareSet)
	}
	m = pumpAll(t, m, cmd)
	if m.filesSets == nil {
		t.Fatalf("the first load still opens the view (status %q)", m.statusMsg)
	}
}

func TestPreviewEscCancelsALoadingCompare(t *testing.T) {
	t.Parallel()
	m, _, _ := previewMarksModel(t)
	m, _ = previewSpace(t, m, 0)
	m, cmd := previewSpace(t, m, 1)
	m, _ = send(m, keyType(tea.KeyEsc))
	m = pumpAll(t, m, cmd)
	if m.filesSets != nil || len(m.previewCompareSet) != 0 {
		t.Fatalf("esc drops the marks and the load: sets=%v set=%v", m.filesSets != nil, m.previewCompareSet)
	}
}
