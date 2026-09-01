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

func noBoundary(model.Commit) bool { return false }

func TestSegmentsLinearIsOneSegment(t *testing.T) {
	t.Parallel()
	commits := []model.Commit{
		segCommit("a3", "a2"), segCommit("a2", "a1"), segCommit("a1"),
	}
	got := newSegLayer().append(commits, noBoundary)
	if want := []int{0, 0, 0}; !slices.Equal(got, want) {
		t.Fatalf("linear history segments = %v, want %v", got, want)
	}
}

func TestSegmentsBranchOverBase(t *testing.T) {
	t.Parallel()
	// Solo of A: a3..a1 are A's own commits, b2 carries branch B's tip.
	commits := []model.Commit{
		segCommit("a3", "a2"),
		segCommit("a2", "a1"),
		segCommit("a1", "b2"),
		withRef(segCommit("b2", "b1"), model.RefLocal, "B"),
		segCommit("b1"),
	}
	boundary := func(c model.Commit) bool { return len(c.Refs) > 0 }
	got := newSegLayer().append(commits, boundary)
	if want := []int{0, 0, 0, 1, 1}; !slices.Equal(got, want) {
		t.Fatalf("A-over-B segments = %v, want %v", got, want)
	}
}

func TestSegmentsStackedBoundaries(t *testing.T) {
	t.Parallel()
	// A over B over C: three territories.
	commits := []model.Commit{
		segCommit("a1", "b1"),
		withRef(segCommit("b1", "c1"), model.RefLocal, "B"),
		withRef(segCommit("c1", "r"), model.RefLocal, "C"),
		segCommit("r"),
	}
	boundary := func(c model.Commit) bool { return len(c.Refs) > 0 }
	got := newSegLayer().append(commits, boundary)
	if want := []int{0, 1, 2, 2}; !slices.Equal(got, want) {
		t.Fatalf("stacked territories = %v, want %v", got, want)
	}
}

func TestSegmentsMergeSideLine(t *testing.T) {
	t.Parallel()
	// m merges side (s2,s1) into mainline (c2,c1); the fork point c1 stays
	// mainline-colored (min claim wins), the side line gets its own segment.
	commits := []model.Commit{
		segCommit("m", "c2", "s2"),
		segCommit("c2", "c1"),
		segCommit("s2", "s1"),
		segCommit("s1", "c1"),
		segCommit("c1"),
	}
	got := newSegLayer().append(commits, noBoundary)
	if want := []int{0, 0, 1, 1, 0}; !slices.Equal(got, want) {
		t.Fatalf("merge segments = %v, want %v", got, want)
	}
}

func TestSegmentsMergeForkPointMinClaimWinsEitherOrder(t *testing.T) {
	t.Parallel()
	// Same topology but the side line pages in BEFORE the mainline child of the
	// fork point — the fork point must still take the smaller (mainline) id.
	commits := []model.Commit{
		segCommit("m", "c2", "s2"),
		segCommit("s2", "s1"),
		segCommit("s1", "c1"),
		segCommit("c2", "c1"),
		segCommit("c1"),
	}
	got := newSegLayer().append(commits, noBoundary)
	if want := []int{0, 1, 1, 0, 0}; !slices.Equal(got, want) {
		t.Fatalf("fork point should keep the mainline segment: %v, want %v", got, want)
	}
}

func TestSegmentsBoundaryOverridesClaim(t *testing.T) {
	t.Parallel()
	// b1 is claimed by a1's first-parent edge, but carries another branch's tip:
	// the boundary wins and starts a fresh segment.
	commits := []model.Commit{
		segCommit("a1", "b1"),
		withRef(segCommit("b1"), model.RefLocal, "B"),
	}
	boundary := func(c model.Commit) bool { return len(c.Refs) > 0 }
	got := newSegLayer().append(commits, boundary)
	if want := []int{0, 1}; !slices.Equal(got, want) {
		t.Fatalf("boundary should override the inherited claim: %v, want %v", got, want)
	}
}

func TestSegmentsNonTopologicalInput(t *testing.T) {
	t.Parallel()
	// A parent listed before its child (the feed is not guaranteed topo order)
	// must not panic; the orphaned parent simply starts a fresh segment.
	commits := []model.Commit{
		segCommit("p", "g"), // parent listed before its child
		segCommit("c", "p"), // child references a row above it
		segCommit("g"),
	}
	got := newSegLayer().append(commits, noBoundary)
	if len(got) != 3 {
		t.Fatalf("got %d segments, want 3", len(got))
	}
	if got[0] != 0 || got[2] != 0 {
		t.Fatalf("p and g chain one line: %v", got)
	}
	if got[1] == got[0] {
		t.Fatalf("orphaned child c should start its own segment: %v", got)
	}
}

func TestSegmentsIncrementalAppendMatchesFull(t *testing.T) {
	t.Parallel()
	commits := []model.Commit{
		segCommit("m", "c2", "s2"),
		segCommit("c2", "c1"),
		segCommit("s2", "s1"),
		segCommit("s1", "c1"),
		withRef(segCommit("c1", "c0"), model.RefLocal, "B"),
		segCommit("c0"),
	}
	boundary := func(c model.Commit) bool { return len(c.Refs) > 0 }
	full := newSegLayer().append(commits, boundary)
	l := newSegLayer()
	paged := l.append(commits[:2], boundary)
	paged = append(paged, l.append(commits[2:], boundary)...)
	if !slices.Equal(full, paged) {
		t.Fatalf("paged append %v differs from full walk %v", paged, full)
	}
}

func TestSegmentsClaimsPruned(t *testing.T) {
	t.Parallel()
	l := newSegLayer()
	l.append([]model.Commit{
		segCommit("a3", "a2"), segCommit("a2", "a1"), segCommit("a1"),
	}, noBoundary)
	// Every listed commit's claim must be consumed; only never-listed parents
	// (none here — a1 is a root) may remain.
	if len(l.claims) != 0 {
		t.Fatalf("claims not pruned after processing: %v", l.claims)
	}
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
