package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// fakeList proves the pipeline is generic: it never inspects concrete types.
type fakeList struct {
	names []string
	dates []int64
}

func (l fakeList) Len() int          { return len(l.names) }
func (l fakeList) Row(i int) string  { return l.names[i] }
func (l fakeList) Name(i int) string { return l.names[i] }
func (l fakeList) Date(i int) int64  { return l.dates[i] }
func (l fakeList) Key(i int) string  { return l.names[i] }

func sortedNames(l fakeList, mode sortMode) []string {
	idx := make([]int, l.Len())
	for i := range idx {
		idx[i] = i
	}
	sortIndices(l, mode, idx)
	out := make([]string, len(idx))
	for n, i := range idx {
		out[n] = l.names[i]
	}
	return out
}

func TestGenericSortOrders(t *testing.T) {
	l := fakeList{
		names: []string{"Beta", "alpha", "gamma"},
		dates: []int64{200, 0, 100},
	}
	cases := []struct {
		mode sortMode
		want string
	}{
		{sortDefault, "Beta,alpha,gamma"},
		{sortNameAsc, "alpha,Beta,gamma"}, // case-insensitive
		{sortNameDesc, "gamma,Beta,alpha"},
		{sortDateAsc, "gamma,Beta,alpha"},  // 100,200; zero-date alpha LAST
		{sortDateDesc, "Beta,gamma,alpha"}, // 200,100; zero-date alpha LAST
	}
	for _, c := range cases {
		if got := strings.Join(sortedNames(l, c.mode), ","); got != c.want {
			t.Errorf("mode %v: got %s, want %s", c.mode, got, c.want)
		}
	}
}

func TestGenericSortStableOnTies(t *testing.T) {
	l := fakeList{names: []string{"b1", "b2", "a"}, dates: []int64{5, 5, 5}}
	if got := strings.Join(sortedNames(l, sortDateAsc), ","); got != "b1,b2,a" {
		t.Errorf("ties must keep backing order, got %s", got)
	}
}

func TestBranchesDefaultSortIsDateDesc(t *testing.T) {
	m := loadedModel(t)
	m.branches = []model.Branch{
		{Name: "alpha", UnixTime: 100},
		{Name: "zeta", UnixTime: 300},
		{Name: "mid", UnixTime: 200},
	}
	if m.sortModes[panelBranches] != sortDateDesc {
		t.Fatalf("New() should default Branches to date desc, got %v", m.sortModes[panelBranches])
	}
	_, idx := m.panelView(panelBranches)
	if len(idx) != 3 || m.branches[idx[0]].Name != "zeta" || m.branches[idx[2]].Name != "alpha" {
		t.Fatalf("idx = %v (newest-first expected)", idx)
	}
}

func TestOKeyCyclesModesAndLabelShowsThem(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelWorktrees
	if m.sortModes[panelWorktrees] != sortDefault {
		t.Fatalf("worktrees should start in default order")
	}
	u, _ := m.Update(keyMsg("o"))
	m = u.(Model)
	if m.sortModes[panelWorktrees] != sortNameAsc {
		t.Fatalf("after o: %v, want sortNameAsc", m.sortModes[panelWorktrees])
	}
	if got := m.panelLabel(panelWorktrees, "Worktrees"); got != "Worktrees ·name↑" {
		t.Fatalf("label = %q", got)
	}
	for i := 0; i < 4; i++ {
		u, _ = m.Update(keyMsg("o"))
		m = u.(Model)
	}
	if m.sortModes[panelWorktrees] != sortDefault {
		t.Fatalf("five presses must cycle back to default, got %v", m.sortModes[panelWorktrees])
	}
	if got := m.panelLabel(panelWorktrees, "Worktrees"); got != "Worktrees" {
		t.Fatalf("default mode must not decorate the label, got %q", got)
	}
}

func TestActionResolvesThroughSortedView(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-sorted")
	runGit(t, dir, "worktree", "add", "-b", "zzz-newest", wt, "main")
	m := New(domain.New(repo))
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	m.focus = panelWorktrees
	m.sortModes[panelWorktrees] = sortNameDesc // "zzz-newest" sorts before "main"
	m.sel[panelWorktrees] = 0
	bi, ok := m.backingIndex(panelWorktrees)
	if !ok {
		t.Fatal("backingIndex not ok")
	}
	if m.worktrees[bi].Branch != "zzz-newest" {
		t.Fatalf("selected backing row = %q, want zzz-newest (the visibly-first row)", m.worktrees[bi].Branch)
	}
}

func TestLayoutOrigins(t *testing.T) {
	// Wide terminal: the active tab slot (Branches by default) over Status, plus
	// the commits column. The inactive Worktrees tab has no box.
	m := Model{width: 90, height: 30, activeLeftTab: panelBranches}
	g := m.layout()
	if got, want := g.pos[panelBranches], (point{0, 1}); got != want {
		t.Errorf("active-tab origin = %v, want %v", got, want)
	}
	if _, visible := g.pos[panelWorktrees]; visible {
		t.Error("inactive Worktrees tab should have no origin")
	}
	if got, want := g.pos[panelFiles], (point{0, 1 + g.boxH[panelBranches]}); got != want {
		t.Errorf("status origin = %v, want %v", got, want)
	}
	if got, want := g.pos[panelCommits], (point{g.leftW, 1}); got != want {
		t.Errorf("commits origin = %v, want %v", got, want)
	}

	// Switching the active tab to Worktrees moves the slot there.
	m = Model{width: 90, height: 30, activeLeftTab: panelWorktrees}
	g = m.layout()
	if got, want := g.pos[panelWorktrees], (point{0, 1}); got != want {
		t.Errorf("active Worktrees origin = %v, want %v", got, want)
	}
	if _, visible := g.pos[panelBranches]; visible {
		t.Error("inactive Branches tab should have no origin")
	}

	// Narrow terminal: single commits column at the left edge.
	m = Model{width: 30, height: 24}
	g = m.layout()
	if got, want := g.pos[panelCommits], (point{0, 1}); got != want {
		t.Errorf("narrow commits origin = %v, want %v", got, want)
	}
}

func TestLoadPopulatesWorktreeHeadTimes(t *testing.T) {
	_, repo := newRepoDir(t)
	m := New(domain.New(repo))
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	if len(m.worktrees) == 0 {
		t.Fatal("expected the primary worktree")
	}
	if m.headTimes[m.worktrees[0].Head] == 0 {
		t.Fatalf("headTimes missing for %s: %v", m.worktrees[0].Head, m.headTimes)
	}
}
