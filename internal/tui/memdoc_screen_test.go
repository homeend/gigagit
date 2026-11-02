package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
)

// memdocScreen is a Model on a wide terminal with a store and a worktree.
func memdocScreen(t *testing.T) Model {
	t.Helper()
	m, _ := memdocModel(t)
	m.width, m.height = 120, 40
	m.currentWorktree = t.TempDir()
	m.snapshotWorktree = m.currentWorktree
	return m
}

func addMem(t *testing.T, m Model, text string, plain bool) (Model, *openFile) {
	t.Helper()
	root := domain.CheckoutKey(m.currentWorktree)
	if _, err := m.docs.AddMemoryOverview(root, m.currentWorktree, memdocTestID, "Why", text, "agent", plain); err != nil {
		t.Fatal(err)
	}
	m = m.syncOverviews()
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if d.ov != nil && d.id() == memdocTestID {
			return m, d
		}
	}
	t.Fatal("not listed")
	return m, nil
}

func mdRowIDs(rows []actionRow) string {
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.id)
	}
	return strings.Join(ids, ",")
}

func mdType(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = nm.(Model)
	}
	return m
}

func mdPress(t *testing.T, m Model, k tea.KeyType) (Model, tea.Cmd) {
	t.Helper()
	nm, cmd := m.Update(tea.KeyMsg{Type: k})
	return nm.(Model), cmd
}

func TestTreeMemoryTag(t *testing.T) {
	t.Parallel()
	m := memdocScreen(t)
	m, d := addMem(t, m, "body", true)
	row := openFileRowText(d, false, d.ovCreated)
	if !strings.Contains(row, "memory") || strings.Contains(row, "anchors") {
		t.Fatalf("row = %q", row)
	}
	m2, d2 := addMem(t, memdocScreen(t), "[a](a.go:1)", false)
	_ = m2
	if row := openFileRowText(d2, false, d2.ovCreated); !strings.Contains(row, "memory") || !strings.Contains(row, "anchors") {
		t.Fatalf("markdown row = %q", row)
	}
}

func TestNNewMemdocCreatesAndOpens(t *testing.T) {
	t.Parallel()
	m := memdocScreen(t)
	m = m.pushLayer(&memdocPopup{})
	m = mdType(t, m, "Title")
	m, _ = mdPress(t, m, tea.KeyTab)
	m = mdType(t, m, "line one")
	m, _ = mdPress(t, m, tea.KeyEnter)
	m = mdType(t, m, "<b>two")
	m, _ = mdPress(t, m, tea.KeyCtrlS)
	list := m.docs.Overviews(domain.CheckoutKey(m.currentWorktree))
	if len(list) != 1 || !list[0].Memory || !list[0].Plain || list[0].Text != "line one\n<b>two" || list[0].Title != "Title" {
		t.Fatalf("docs = %+v", list)
	}
	if _, ok := m.topLayer().(*memdocPopup); ok {
		t.Fatal("window still open")
	}
	if _, ok := m.topLayer().(*fileViewer); !ok {
		t.Fatalf("top = %T, want the viewer", m.topLayer())
	}
}

func TestNEscAsksWhenTyped(t *testing.T) {
	t.Parallel()
	m := memdocScreen(t)
	m = m.pushLayer(&memdocPopup{})
	m = mdType(t, m, "x")
	m, _ = mdPress(t, m, tea.KeyEsc)
	if _, ok := m.topLayer().(*memdocPopup); !ok {
		t.Fatal("esc threw typed text away without asking")
	}
	if !strings.Contains(m.topLayer().render(m, ""), "esc") {
		t.Fatal("no question on screen")
	}
	m, _ = mdPress(t, m, tea.KeyEsc)
	if _, ok := m.topLayer().(*memdocPopup); ok {
		t.Fatal("second esc did not leave")
	}
	m = m.pushLayer(&memdocPopup{})
	m, _ = mdPress(t, m, tea.KeyEsc)
	if _, ok := m.topLayer().(*memdocPopup); ok {
		t.Fatal("esc on an empty window asks for nothing")
	}
}

func TestNEmptyRefusedStaysOpen(t *testing.T) {
	t.Parallel()
	m := memdocScreen(t)
	m = m.pushLayer(&memdocPopup{})
	m = mdType(t, m, "only a title")
	m, _ = mdPress(t, m, tea.KeyCtrlS)
	p, ok := m.topLayer().(*memdocPopup)
	if !ok || p.err == "" {
		t.Fatalf("top = %T", m.topLayer())
	}
	if len(m.docs.Overviews(domain.CheckoutKey(m.currentWorktree))) != 0 {
		t.Fatal("created")
	}
}

func TestPlainOpensFullScreenNoSplit(t *testing.T) {
	t.Parallel()
	m := memdocScreen(t)
	m, d := addMem(t, m, "body", true)
	m, _ = m.bringToFront(d)
	if _, ok := m.topLayer().(*fileViewer); !ok {
		t.Fatalf("top = %T", m.topLayer())
	}
	m2, d2 := addMem(t, memdocScreen(t), "[a](a.go:1)", false)
	m2, _ = m2.bringToFront(d2)
	if m2.splitShowing(d2) == nil {
		t.Fatal("a markdown memory document should split")
	}
}

func TestPlainShowsMarkupLiterally(t *testing.T) {
	t.Parallel()
	const text = "# not a heading\n\n<b>[a](a.go:1)</b> `x`\n"
	lines := plainOverviewLines(text, 80, modeWrap)
	var got []string
	for _, l := range lines {
		got = append(got, l.text)
	}
	if s := strings.Join(got, "\n"); !strings.Contains(s, "# not a heading") || !strings.Contains(s, "<b>[a](a.go:1)</b> `x`") {
		t.Fatalf("rows = %q", s)
	}
	if len(got) < 3 || got[1] != "" {
		t.Fatalf("blank line lost: %q", got)
	}
	for _, l := range plainOverviewLines("a\x1b[31mb\tc", 80, modeWrap) {
		if strings.ContainsRune(l.text, 0x1b) {
			t.Fatalf("escape on screen: %q", l.text)
		}
	}
	wrapped := plainOverviewLines(strings.Repeat("word ", 40), 20, modeWrap)
	if len(wrapped) < 2 || !wrapped[1].cont {
		t.Fatalf("not wrapped: %+v", wrapped)
	}
}

func TestPlainMenuHasCopyAndLinkNoExport(t *testing.T) {
	t.Parallel()
	m := memdocScreen(t)
	m, d := addMem(t, m, "body", true)
	m, _ = m.bringToFront(d)
	ids := mdRowIDs(m.overviewRows())
	for _, want := range []string{"overview-copy", "overview-copy-link", "overview-delete"} {
		if !strings.Contains(ids, want) {
			t.Fatalf("no %s in %s", want, ids)
		}
	}
	for _, not := range []string{"overview-export", "overview-next"} {
		if strings.Contains(ids, not) {
			t.Fatalf("%s in %s", not, ids)
		}
	}
}

func TestCopyOverviewByteExact(t *testing.T) {
	t.Parallel()
	const text = "a\x1b[31m\t<b>\n\n  tail  \n"
	m := memdocScreen(t)
	m, d := addMem(t, m, text, true)
	if d.ov.text != text {
		t.Fatalf("copy source = %q", d.ov.text)
	}
	_ = m
}

func TestMemoryMarkdownSplitNoExport(t *testing.T) {
	t.Parallel()
	m, d := addMem(t, memdocScreen(t), "[a](a.go:1)", false)
	m, _ = m.bringToFront(d)
	sv := m.splitShowing(d)
	if sv == nil {
		t.Fatal("no split")
	}
	if ids := mdRowIDs(m.splitOverviewRows(sv, d)); strings.Contains(ids, "overview-export") {
		t.Fatalf("rows = %s", ids)
	}
}

func TestMemoryLinkOnUnattachedRoot(t *testing.T) {
	t.Parallel()
	m, d := addMem(t, memdocScreen(t), "x", true)
	if m.docs.AttachedWorktree(d.root) != "" {
		t.Fatal("this test wants an unattached root")
	}
	r := m.overviewLinkRow(d)
	if r.id != "overview-copy-link" {
		t.Fatal(r.id)
	}
	wt := m.memdocWorktree(d)
	if wt != m.currentWorktree {
		t.Fatalf("worktree = %q", wt)
	}
}

func TestDeleteMemoryAsksAndRemoves(t *testing.T) {
	t.Parallel()
	m, d := addMem(t, memdocScreen(t), "x", true)
	u, _ := m.confirmSessionOverviewDelete(d)
	m = u.(Model)
	if m.modal == nil {
		t.Fatal("no question")
	}
	m, ok := m.deleteDoc(d)
	if !ok {
		t.Fatal("delete failed")
	}
	if _, held := m.docs.OverviewIn(d.root, memdocTestID); held {
		t.Fatal("still held")
	}
}

func TestRepoSwitchKeepsMemoryDoc(t *testing.T) {
	t.Parallel()
	m, d := addMem(t, memdocScreen(t), "x", true)
	m.detachDocs(m.currentWorktree)
	if _, ok := m.docs.OverviewIn(d.root, memdocTestID); !ok {
		t.Fatal("a repo switch lost the memory document")
	}
}

var _ = agentdocs.MaxOverviewBytes
