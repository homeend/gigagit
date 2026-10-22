package tui

import (
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// otherRepoView is a second member built from a fresh real repository: its
// own service, slot map and a few distinguishable repository-wide values.
func otherRepoView(t *testing.T) *repoView {
	t.Helper()
	repo := newRepo(t)
	svc := domain.New(repo)
	r := newRepoView("/work/other", svc)
	r.branches = []model.Branch{{Name: "other-main"}}
	r.worktrees = []model.Worktree{{Path: "/work/other", Branch: "other-main"}}
	r.notices = []notice{{id: "other-notice"}}
	r.branchFilterSlot = map[panel]int{panelBranches: 2}
	return r
}

// saveRepo then loadRepo is a round trip of the repository-wide state: the
// lists, the feed, the notices, the branch-filter slot, the service and the
// config, with the worktree slot map following its repository.
func TestSaveLoadRepoRoundTrip(t *testing.T) {
	m := loadedModel(t)
	m.branches = []model.Branch{{Name: "home-main"}}
	m.worktrees = []model.Worktree{{Path: m.currentWorktree, Branch: "home-main"}}
	m.notices = []notice{{id: "home-notice"}}
	m.branchFilterSlot[panelBranches] = 1
	homeFeed, homeSvc, homeViews := m.feed, m.svc, m.views
	m = m.saveRepo()
	a := m.homeRepo()
	if a == nil || a.svc != homeSvc || len(a.branches) != 1 || a.branches[0].Name != "home-main" {
		t.Fatalf("saveRepo did not fill the home slot: %+v", a)
	}
	b := otherRepoView(t)
	m = m.loadRepo(b)
	if m.svc != b.svc || m.feed != b.feed || m.views == nil || len(m.branches) != 1 || m.branches[0].Name != "other-main" ||
		len(m.notices) != 1 || m.notices[0].id != "other-notice" || m.branchFilterSlot[panelBranches] != 2 ||
		m.worktrees[0].Path != "/work/other" {
		t.Fatalf("after loadRepo(B): svc ok=%v branches=%v notices=%v slot=%v", m.svc == b.svc, m.branches, m.notices, m.branchFilterSlot)
	}
	if reflect.ValueOf(m.views).Pointer() == reflect.ValueOf(homeViews).Pointer() {
		t.Fatal("loadRepo(B) kept A's worktree slot map")
	}
	m = m.loadRepo(a)
	if m.svc != homeSvc || m.feed != homeFeed || len(m.branches) != 1 || m.branches[0].Name != "home-main" ||
		m.notices[0].id != "home-notice" || m.branchFilterSlot[panelBranches] != 1 || m.worktrees[0].Path != m.currentWorktree {
		t.Fatalf("A not restored: branches=%v notices=%v slot=%v", m.branches, m.notices, m.branchFilterSlot)
	}
	if len(m.views) != len(homeViews) {
		t.Fatalf("A's worktree slots not restored: %d, want %d", len(m.views), len(homeViews))
	}
}

// The first load seeds a home repository slot even with no workspace open:
// one member, sharing the live service and the worktree slot map.
func TestSeedHomeSeedsRepo(t *testing.T) {
	m := loadedModel(t)
	if m.inWorkspace() {
		t.Fatal("a plain load must not open a workspace")
	}
	r := m.homeRepo()
	if r == nil || r.svc != m.svc {
		t.Fatalf("homeRepo = %+v, want the live service", r)
	}
	if r.key != model.KeyOf(r.mainPath) || r.mainPath == "" {
		t.Fatalf("home repo identity: key=%q main=%q", r.key, r.mainPath)
	}
	if m.liveRepo() != r {
		t.Fatal("liveRepo must be the home repo when no workspace is open")
	}
}

// reRoot leaves any workspace behind: the member slots go with the old root.
func TestReRootDropsWorkspace(t *testing.T) {
	m := loadedModel(t)
	m.ws = &workspaceState{name: "shop", members: []*repoView{m.homeRepo(), otherRepoView(t)}}
	dir, _ := newRepoDir(t)
	nm, _ := m.reRoot(dir)
	m = nm.(Model)
	if m.inWorkspace() {
		t.Fatal("reRoot kept the workspace")
	}
}
