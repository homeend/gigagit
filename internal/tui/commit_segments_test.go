package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"

	"github.com/charmbracelet/lipgloss"
)

// seg helpers: c builds a commit, ref decorates one.
func segCommit(hash string, parents ...string) model.Commit {
	return model.Commit{Hash: hash, Parents: parents}
}

func withRef(c model.Commit, kind model.RefKind, name string) model.Commit {
	c.Refs = append(c.Refs, model.Ref{Name: name, Kind: kind})
	return c
}

func TestSegBoundaryPredicate(t *testing.T) {
	t.Parallel()
	m := Model{
		commitScopeBranches: []string{"feat/a"},
		branches: []model.Branch{
			{Name: "feat/a", Upstream: "origin/feat/a"},
			{Name: "main", Upstream: "origin/main"},
		},
	}
	boundary := m.segBoundary()
	cases := []struct {
		name string
		c    model.Commit
		want bool
	}{
		{"own tip", withRef(segCommit("x"), model.RefLocal, "feat/a"), false},
		{"own upstream", withRef(segCommit("x"), model.RefRemote, "origin/feat/a"), false},
		{"other local tip", withRef(segCommit("x"), model.RefLocal, "main"), true},
		{"other remote tip", withRef(segCommit("x"), model.RefRemote, "origin/main"), true},
		{"tag", withRef(segCommit("x"), model.RefTag, "v1.0"), false},
		{"detached head", withRef(segCommit("x"), model.RefHead, "HEAD"), false},
		{"undecorated", segCommit("x"), false},
	}
	for _, tc := range cases {
		if got := boundary(tc.c); got != tc.want {
			t.Errorf("%s: boundary = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSegBoundaryPredicateSoloedTag(t *testing.T) {
	t.Parallel()
	// A soloed tag: the tag decoration itself is never a boundary (deliberate),
	// while other branch tips inside its history still are.
	m := Model{commitScopeBranches: []string{"v1.0"}}
	boundary := m.segBoundary()
	if boundary(withRef(segCommit("x"), model.RefTag, "v1.0")) {
		t.Fatal("the soloed tag's own decoration must not be a boundary")
	}
	if !boundary(withRef(segCommit("x"), model.RefLocal, "main")) {
		t.Fatal("a branch tip under a soloed tag is a boundary")
	}
}

func TestRebuildCommitGraphComputesSegments(t *testing.T) {
	m := footerModel()
	m.commitScopeBranches = []string{"feat/x"}
	m.commits = []model.Commit{
		segCommit("aaaaaaa", "bbbbbbb"),
		withRef(segCommit("bbbbbbb", "ccccccc"), model.RefLocal, "main"),
		segCommit("ccccccc"),
	}
	m = m.rebuildCommitGraph()
	if want := []int{0, 1, 1}; !slices.Equal(m.commitSegs, want) {
		t.Fatalf("commitSegs = %v, want %v", m.commitSegs, want)
	}
	// Paging in older commits extends the slice incrementally.
	m.commits = append(m.commits, segCommit("ddddddd"))
	m = m.rebuildCommitGraph()
	if len(m.commitSegs) != 4 {
		t.Fatalf("append should extend commitSegs, got %v", m.commitSegs)
	}
}

// dotEscape renders ● in the given palette color and returns the leading ANSI
// escape lipgloss emits for it under the active profile.
func dotEscape(t *testing.T, seg int) string {
	t.Helper()
	probe := lipgloss.NewStyle().Foreground(laneColor(seg)).Render("●")
	i := strings.IndexRune(probe, '●')
	if i <= 0 {
		t.Fatalf("no escape in probe %q", probe)
	}
	return probe[:i]
}

// scopedLinearModel builds a soloed feed where feat/x sits 1 commit over main:
// linear history, all lane 0 — only segments can tell the rows apart.
func scopedLinearModel() Model {
	m := footerModel()
	m.focus = panelCommits
	m.commitScopeBranches = []string{"feat/x"}
	m.commits = []model.Commit{
		segCommit("aaaaaaa", "bbbbbbb"),
		withRef(segCommit("bbbbbbb", "ccccccc"), model.RefLocal, "main"),
		segCommit("ccccccc"),
	}
	m = m.rebuildCommitGraph()
	m.commitListMode = true
	m.sel[panelCommits] = -1 // no selected row: selection style would win
	return m
}

func TestScopedListDotUsesSegmentColor(t *testing.T) {
	forceColor(t)
	m := scopedLinearModel()
	rows, idx := m.panelView(panelCommits)
	decos := m.commitDecorators(rows, idx, -1)
	top := decos[0]("  ● aaaaaaa", 0, 0)
	base := decos[1]("  ● bbbbbbb", 0, 0)
	if !strings.Contains(top, dotEscape(t, 0)) {
		t.Fatalf("scoped tip commit should use segment-0 color: %q", top)
	}
	if !strings.Contains(base, dotEscape(t, 1)) {
		t.Fatalf("inherited history should use segment-1 color: %q", base)
	}
}

func TestUnscopedListDotKeepsLaneColor(t *testing.T) {
	forceColor(t)
	m := scopedLinearModel()
	m.commitScopeBranches = nil // unscoped → today's lane coloring
	rows, idx := m.panelView(panelCommits)
	decos := m.commitDecorators(rows, idx, -1)
	base := decos[1]("  ● bbbbbbb", 0, 0)
	if !strings.Contains(base, dotEscape(t, 0)) {
		t.Fatalf("unscoped list should keep lane-0 color on every linear row: %q", base)
	}
}

func TestScopedFilteredListFallsBackToLaneColor(t *testing.T) {
	forceColor(t)
	// A path/author/date-filtered walk lists non-contiguous commits: first-parent
	// chains break, so segment coloring must fall back to lane colors.
	m := scopedLinearModel()
	m.commitFilter = commitFilterFields{Author: "someone"}
	rows, idx := m.panelView(panelCommits)
	decos := m.commitDecorators(rows, idx, -1)
	base := decos[1]("  ● bbbbbbb", 0, 0)
	if !strings.Contains(base, dotEscape(t, 0)) {
		t.Fatalf("filtered scoped list should fall back to lane color: %q", base)
	}
}

func TestScopedListDotSegmentColorWithWipRows(t *testing.T) {
	forceColor(t)
	// commitSegs holds REAL commits only, while display rows carry a WIP prefix:
	// the decorator must index segs at ci-wipCount, not ci. A dirty status makes
	// wipCount ≥ 1; the boundary commit's dot must still take segment-1 color.
	m := scopedLinearModel()
	m.status.Files = []model.FileStatus{{Path: "f", Kind: model.KindTracked, Unstaged: 'M'}}
	m = m.graphLayerReset().rebuildCommitGraph() // re-derive wipRows + re-lay
	if len(m.wipRows) == 0 {
		t.Fatal("dirty status should derive a WIP row")
	}
	rows, idx := m.panelView(panelCommits)
	decos := m.commitDecorators(rows, idx, -1)
	wip := len(m.wipRows)
	base := decos[wip+1]("  ● bbbbbbb", 0, 0) // display row of the boundary commit
	if !strings.Contains(base, dotEscape(t, 1)) {
		t.Fatalf("WIP-offset feed: boundary commit should use segment-1 color: %q", base)
	}
}

func TestScopedGraphNodeUsesSegmentColor(t *testing.T) {
	forceColor(t)
	m := scopedLinearModel()
	m.commitListMode = false // graph mode: node dot takes the segment color too
	rows, idx := m.panelView(panelCommits)
	decos := m.commitDecorators(rows, idx, -1)
	base := decos[1]("  ● bbbbbbb", 0, 0)
	if !strings.Contains(base, dotEscape(t, 1)) {
		t.Fatalf("scoped graph node should use segment color: %q", base)
	}
}
