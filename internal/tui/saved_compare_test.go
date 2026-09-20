package tui

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

// savedCompareModel is mergePreviewModel (a merge preview "login", its store
// in a TEMP dir — never the user's real savedcompare.toml) with a fake
// clipboard. It returns the store dir and two local-form links that compare:
// main's whole tree and feat/x's.
func savedCompareModel(t *testing.T) (m Model, storeDir, left, right string) {
	t.Helper()
	m, dir, storeDir := mergePreviewModel(t)
	m.svc.UseLinkHistDir(t.TempDir())
	m.clipWrite = func(_ io.Writer, _ string) (string, error) { return "fake", nil }
	m.currentWorktree = dir
	m.width, m.height = 160, 40
	return m, storeDir, localLink(dir, "@ref:main"), localLink(dir, "@ref:feat/x")
}

// withSavedComparison stores left ↔ right and reloads the Previews tab.
func withSavedComparison(t *testing.T, m Model, left, right, label string) (Model, domain.SavedCompare) {
	t.Helper()
	c, err := m.svc.SavedCompareAdd(context.Background(), left, right, label)
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	return m.activateTab(panelPreviews), c
}

func selectPreviewRow(t *testing.T, m Model, id string) Model {
	t.Helper()
	for i, r := range m.previews {
		if r.id() == id {
			m.sel[panelPreviews] = i
			return m
		}
	}
	t.Fatalf("no Previews row %q in %+v", id, m.previews)
	return m
}

func menuRow(t *testing.T, m Model, id string) (actionRow, bool) {
	t.Helper()
	for _, r := range availableActions(m) {
		if r.id == id {
			return r, true
		}
	}
	return actionRow{}, false
}

// openedLinkCompare opens left ↔ right in the set-shaped view.
func openedLinkCompare(t *testing.T, m Model, left, right string) Model {
	t.Helper()
	m, cmd := m.startLinkCompare(left, right)
	m = pumpAll(t, m, cmd)
	if m.filesSets == nil {
		t.Fatalf("fixture: the link compare did not open (status %q)", m.statusMsg)
	}
	return m
}

func savedComparisons(t *testing.T, m Model) []domain.SavedCompare {
	t.Helper()
	all, err := m.svc.SavedCompareList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.SavedCompare
	for _, c := range all {
		if !c.IsSet() {
			out = append(out, c)
		}
	}
	return out
}

func TestSaveComparisonStoresTheTwoTexts(t *testing.T) {
	t.Parallel()
	m, storeDir, left, right := savedCompareModel(t)
	m = openedLinkCompare(t, m, left, right)
	row, ok := menuRow(t, m, "save-comparison")
	if !ok {
		t.Fatal("a link compare's . menu must offer Save comparison…")
	}
	tm, _ := row.run(m)
	m = tm.(Model)
	if layerOf[*saveComparePopup](m) == nil {
		t.Fatal("the row must ask for a label")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // empty label = the store's default
	m = pumpAll(t, updated.(Model), cmd)
	got := savedComparisons(t, m)
	if len(got) != 1 || got[0].Left != left || got[0].Right != right {
		t.Fatalf("stored %+v, want exactly the view's two TEXTS %q ↔ %q", got, left, right)
	}
	if got[0].Label == "" {
		t.Error("an empty label must come back as the store's default, not empty")
	}
	if !strings.Contains(m.statusMsg, got[0].Label) {
		t.Errorf("status = %q, want it to name the saved entry", m.statusMsg)
	}
	// The fixture's isolation, pinned: the write landed in the temp dir.
	found := false
	filepath.WalkDir(storeDir, func(p string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() && strings.HasSuffix(p, ".toml") {
			found = true
		}
		return nil
	})
	if !found {
		t.Errorf("no store file under %s — the save went somewhere else", storeDir)
	}
}

func TestSavingTwiceIsANoticeNotAnError(t *testing.T) {
	t.Parallel()
	m, _, left, right := savedCompareModel(t)
	m, c := withSavedComparison(t, m, left, right, "mine")
	m = openedLinkCompare(t, m, left, right)
	m = pumpAll(t, m, m.saveCompareCmd(left, right, "another name"))
	if !strings.Contains(m.statusMsg, "already saved as "+c.Label) {
		t.Errorf("status = %q, want the existing entry named", m.statusMsg)
	}
	if n := len(savedComparisons(t, m)); n != 1 {
		t.Errorf("%d comparisons stored, want 1", n)
	}
}

func TestSaveComparisonIsAbsentFromAnOrdinaryCompare(t *testing.T) {
	t.Parallel()
	m, _, left, right := savedCompareModel(t)
	m = openedLinkCompare(t, m, left, right)
	l, r := m.filesLeft, m.filesRight
	m = m.closeFilesView()
	m, _ = m.openCompareFiles(l, r) // the same two endpoints, no links
	if _, ok := menuRow(t, m, "save-comparison"); ok {
		t.Error("an endpoint compare has no link texts to save")
	}
}

// Three kinds in ONE panel: the merge preview, a commit pair, a comparison.
func TestThePanelListsAComparisonBesideTheSetRows(t *testing.T) {
	t.Parallel()
	m, _, left, right := savedCompareModel(t)
	if _, err := m.svc.PairAdd(context.Background(), "main", "feat/x", "attempt"); err != nil {
		t.Fatal(err)
	}
	m, c := withSavedComparison(t, m, left, right, "trees")
	kinds := map[previewRowKind]int{}
	for _, r := range m.previews {
		kinds[r.kind]++
	}
	if len(m.previews) != 3 || kinds[rowMerge] != 1 || kinds[rowPair] != 1 || kinds[rowCompare] != 1 {
		t.Fatalf("rows = %+v, want one of each kind — a SET row listed twice means the non-set filter is gone", m.previews)
	}
	out := m.View()
	if !strings.Contains(out, "trees") || !strings.Contains(out, "branch: main ↔ branch: feat/x") {
		t.Errorf("the comparison row must show its label and both descriptions:\n%s", out)
	}
	l := previewList{rows: m.previews}
	if l.Key(2) != c.ID || l.Name(2) != "trees" || l.Date(2) != c.Created.Unix() {
		t.Errorf("list identity = %q %q %d", l.Key(2), l.Name(2), l.Date(2))
	}
}

func TestEnterOnAComparisonRowReopensIt(t *testing.T) {
	t.Parallel()
	m, _, left, right := savedCompareModel(t)
	m, c := withSavedComparison(t, m, left, right, "trees")
	m = selectPreviewRow(t, m, c.ID)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = pumpAll(t, updated.(Model), cmd)
	if m.filesSets == nil || m.compareTag != linkCompareTag(left, right) {
		t.Fatalf("sets=%v tag=%q status=%q, want the stored comparison re-run", m.filesSets != nil, m.compareTag, m.statusMsg)
	}
	if got := strings.Join(rowTexts(m), "|"); got != "A a.txt" {
		t.Errorf("rows = %q", got)
	}
}

// Rename and remove route by SHAPE. Each half is a separate lie a wrong route
// could tell, so both directions are checked on one fixture.
func TestRenameAndRemoveRouteByShape(t *testing.T) {
	t.Parallel()
	m, _, left, right := savedCompareModel(t)
	m, c := withSavedComparison(t, m, left, right, "trees")
	ctx := context.Background()
	rename := func(m Model, id, suffix string) Model {
		m = selectPreviewRow(t, m, id)
		updated, _ := m.Update(keyMsg("e"))
		m = typeString(t, updated.(Model), suffix)
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		return drainMsgs(t, updated.(Model), cmd, 4)
	}
	previewLabel := func() (string, string) {
		ps, _ := m.svc.PreviewList(ctx)
		if len(ps) != 1 {
			t.Fatalf("merge previews = %+v", ps)
		}
		return ps[0].ID, ps[0].Label
	}
	pid, plabel := previewLabel()

	m = rename(m, c.ID, " 2")
	if got, _ := m.svc.SavedCompareGet(ctx, c.ID); got.Label != "trees 2" {
		t.Errorf("comparison label = %q (status %q)", got.Label, m.statusMsg)
	}
	if _, l := previewLabel(); l != plabel {
		t.Errorf("renaming the comparison moved the merge preview's label: %q → %q", plabel, l)
	}
	m = rename(m, pid, "!")
	if _, l := previewLabel(); l != plabel+"!" {
		t.Errorf("merge preview label = %q (status %q)", l, m.statusMsg)
	}
	if got, _ := m.svc.SavedCompareGet(ctx, c.ID); got.Label != "trees 2" {
		t.Errorf("renaming the merge preview moved the comparison's label: %q", got.Label)
	}

	m = selectPreviewRow(t, m, c.ID)
	updated, _ := m.Update(keyMsg("d"))
	m = updated.(Model)
	if m.modal == nil {
		t.Fatal("d must confirm")
	}
	tm, cmd := m.modal.onResolve(m, "Remove")
	m = drainMsgs(t, tm.(Model), cmd, 4)
	if n := len(savedComparisons(t, m)); n != 0 {
		t.Errorf("the comparison survived its remove (status %q)", m.statusMsg)
	}
	if ps, _ := m.svc.PreviewList(ctx); len(ps) != 1 {
		t.Errorf("removing the comparison took the merge preview with it: %+v", ps)
	}
}

// A saved comparison outlives what it names: the row stays, says why it will
// not open, and can still be removed.
func TestADeadComparisonStaysListedAndCanBeRemoved(t *testing.T) {
	t.Parallel()
	m, _, left, _ := savedCompareModel(t)
	dir := m.currentWorktree
	runGit(t, dir, "branch", "doomed", "feat/x")
	m, c := withSavedComparison(t, m, left, localLink(dir, "@ref:doomed"), "doomed")
	runGit(t, dir, "branch", "-D", "doomed")

	m = selectPreviewRow(t, m, c.ID)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = pumpAll(t, updated.(Model), cmd)
	if m.filesView != nil || m.statusMsg == "" {
		t.Fatalf("view=%v status=%q, want a refusal that says why", m.filesView != nil, m.statusMsg)
	}
	m, reload := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ = m.Update(reload())
	m = selectPreviewRow(t, updated.(Model), c.ID) // still listed
	m = pumpAll(t, m, m.previewRemoveCmd(rowCompare, c.ID))
	if n := len(savedComparisons(t, m)); n != 0 {
		t.Errorf("a dead comparison could not be removed (status %q)", m.statusMsg)
	}
}

func TestCopyLinkOnAComparisonRowDeclines(t *testing.T) {
	t.Parallel()
	m, _, left, right := savedCompareModel(t)
	m, c := withSavedComparison(t, m, left, right, "trees")
	m = selectPreviewRow(t, m, c.ID)
	if text, ok := m.contextLinkText(); ok {
		t.Errorf("a comparison is two links, yet Copy link offered %q", text)
	}
	// The merge preview beside it still has one — the refusal is the row's.
	ps, _ := m.svc.PreviewList(context.Background())
	m = selectPreviewRow(t, m, ps[0].ID)
	if _, ok := m.contextLinkText(); !ok {
		t.Error("the merge preview row lost its Copy link")
	}
}

func TestCopyLeftAndRightLinkCopyTheirOwnHalf(t *testing.T) {
	t.Parallel()
	m, _, left, right := savedCompareModel(t)
	var wrote []string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { wrote = append(wrote, s); return "fake", nil }
	m, c := withSavedComparison(t, m, left, right, "trees")
	m = selectPreviewRow(t, m, c.ID)
	for _, id := range []string{"copy-left-link", "copy-right-link"} {
		row, ok := menuRow(t, m, id)
		if !ok {
			t.Fatalf("a comparison row's . menu must offer %s", id)
		}
		_, cmd := row.run(m)
		runCmds(cmd)
	}
	if len(wrote) != 2 || wrote[0] != left || wrote[1] != right {
		t.Errorf("copied %q, want [%q %q]", wrote, left, right)
	}
	ps, _ := m.svc.PreviewList(context.Background())
	m = selectPreviewRow(t, m, ps[0].ID)
	if _, ok := menuRow(t, m, "copy-left-link"); ok {
		t.Error("a merge preview row must not offer a comparison's rows")
	}
}

// s on a comparison saves the two links the other way round — what s does for
// the other two kinds — and never reaches a SET row's swap with empty fields.
func TestSwapOnAComparisonSavesTheReversedComparison(t *testing.T) {
	t.Parallel()
	m, _, left, right := savedCompareModel(t)
	m, c := withSavedComparison(t, m, left, right, "trees")
	m = selectPreviewRow(t, m, c.ID)
	updated, cmd := m.Update(keyMsg("s"))
	m = pumpAll(t, updated.(Model), cmd)
	got := savedComparisons(t, m)
	if len(got) != 2 || got[1].Left != right || got[1].Right != left {
		t.Fatalf("stored %+v, want the reversed comparison added (status %q)", got, m.statusMsg)
	}
	if ps, _ := m.svc.PairList(context.Background()); len(ps) != 0 {
		t.Errorf("s on a comparison saved a commit pair: %+v", ps)
	}
}
