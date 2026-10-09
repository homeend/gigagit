package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// reviewWithOverview saves a review on the repo's tip commit whose document
// holds an overview with one resolving anchor (a.go:1), one into a file the
// review did not touch (zzz.go:1) and one note: anchor, and opens its review
// view.
func reviewWithOverview(t *testing.T) (Model, string) {
	t.Helper()
	doc := `{"version":1,"summary":"## Summary\nfine","overview":"Start at [the change](a.go:1), then [outside](zzz.go:1) and [a note](note:t1).","files":[{"path":"a.go","annotations":[{"newRange":[1,1],"summary":"first line"}]}]}`
	m, id := reviewViewModel(t, doc) // review_view_test.go: a review of stackRepoModel's tip commit
	sha := m.commits[0].Hash
	m, cmd := m.openReview(id, "review")
	m = drainCmds(t, m, cmd)
	if m.filesReview == nil || m.filesReview.id != id {
		t.Fatalf("review view not open: %+v", m.filesReview)
	}
	return m, sha
}

func overviewRow(t *testing.T, m Model) contentLine {
	t.Helper()
	for _, l := range m.filesView.visible() {
		if l.overviewDoc {
			return l
		}
	}
	t.Fatal("no ≡ Overview row")
	return contentLine{}
}

// R12: a review with a stored overview draws "≡ Overview" under "≡ Summary";
// enter opens it in the overview viewer with the first anchor current.
func TestStoredOverviewRowOpensTheViewer(t *testing.T) {
	t.Parallel()
	m, _ := reviewWithOverview(t)
	vis := m.filesView.visible()
	if !vis[0].summary || vis[1].text != "≡ Overview" || !vis[1].overviewDoc {
		t.Fatalf("rows = %q / %q", vis[0].text, vis[1].text)
	}
	u, cmd := m.openDiffForFileLine(vis[1])
	m = drainCmds(t, u.(Model), cmd)
	d := topDoc(m)
	if d == nil || d.ov == nil || d.src.kind != srcReviewOverview {
		t.Fatalf("top = %T, want the overview viewer", m.topLayer())
	}
	if d.ov.tip == "" || d.ov.sel != 0 {
		t.Fatalf("tip %q sel %d: the reviewed tip is stamped and the first anchor is current", d.ov.tip, d.ov.sel)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "the change") || !strings.Contains(view, "Overview: ") {
		t.Fatalf("view:\n%s", view)
	}
}

// Review Focus 4 + R3: an anchor outside the review's files (and a note:
// anchor) is drawn plain — not the gone colour — tab skips it, enter on it
// says why.
func TestStoredOverviewUnresolvedAnchorIsPlainAndSkipped(t *testing.T) {
	t.Parallel()
	m, _ := reviewWithOverview(t)
	u, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u.(Model), cmd)
	d := topDoc(m)
	if len(d.ov.anchors) != 3 || d.ov.anchors[0].plain || !d.ov.anchors[1].plain || !d.ov.anchors[2].plain {
		t.Fatalf("anchors = %+v", d.ov.anchors)
	}
	if d.ov.anchors[1].missing {
		t.Fatal("unresolved is plain, never 'missing'")
	}
	rows, _ := m.viewerGeom()
	d.stepAnchor(1, rows)
	if d.ov.sel != 0 {
		t.Fatalf("tab wrapped to %d: the only resolved anchor is 0", d.ov.sel)
	}
	m, _ = m.openAnchor(d, 1)
	if topDoc(m) != d || !strings.Contains(m.statusMsg, "zzz.go:1") {
		t.Fatalf("enter on a plain anchor opened %v / said %q", topDoc(m) != d, m.statusMsg)
	}
}

// R5 + A1: enter on an anchor opens the file AT THE REVIEWED TIP in the
// viewer, the cursor on the line, the overview's band drawn; backspace
// returns to the overview at the same anchor.
func TestStoredOverviewAnchorOpensTheFileAtTheTip(t *testing.T) {
	t.Parallel()
	m, sha := reviewWithOverview(t)
	u, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u.(Model), cmd)
	ov := topDoc(m)
	fv := m.topLayer().(*fileViewer)
	m, cmd = fv.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // the viewer's enter → openAnchor
	m = drainCmds(t, m, cmd)
	d := topDoc(m)
	if d == nil || d == ov || d.src.kind != srcCommit || d.src.rev != sha || d.path != "a.go" {
		t.Fatalf("opened %+v", d)
	}
	if d.from != ov || d.anchorCur != "a.go:1" || len(d.bands()) != 1 || d.p.cur != 0 {
		t.Fatalf("from %v cur %q bands %d line %d", d.from == ov, d.anchorCur, len(d.bands()), d.p.cur)
	}
	m, _, ok := m.anchorBack(d)
	if !ok || topDoc(m) != ov || ov.ov.sel != 0 {
		t.Fatalf("backspace: ok %v top %v sel %d", ok, topDoc(m) == ov, ov.ov.sel)
	}
}

// The stored overview is not an agent's: the store sync must not close it,
// gg session overview must not find it, and y copies its markdown.
func TestStoredOverviewIsNotAStoreOverview(t *testing.T) {
	t.Parallel()
	m, _ := reviewWithOverview(t)
	u, cmd := m.openDiffForFileLine(overviewRow(t, m))
	m = drainCmds(t, u.(Model), cmd)
	d := topDoc(m)
	m = m.syncOverviews()
	if topDoc(m) != d {
		t.Fatal("syncOverviews closed the stored overview")
	}
	if f, _ := m.findOverview(d.id()); f != nil {
		t.Fatal("findOverview must not answer a review's overview")
	}
	for _, r := range m.overviewRows() {
		if r.id == "overview-ref" {
			t.Fatal("no anchor reference for a stored overview (A9)")
		}
	}
	_, c := m.topLayer().(*fileViewer).update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if c == nil {
		t.Fatal("y copies the overview's markdown")
	}
}
