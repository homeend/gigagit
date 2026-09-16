package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/model"
)

func TestExemptBranches(t *testing.T) {
	t.Parallel()
	bs := []model.Branch{{Name: "main", IsHead: true}, {Name: "feat/a"}, {Name: "feat/b"}}
	wts := []model.Worktree{{Path: "/r", Branch: "main"}, {Path: "/r-b", Branch: "feat/b"}}
	got := ExemptBranches(bs, wts)
	want := []bool{true, false, true}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d (%s) exempt = %v; want %v", i, bs[i].Name, got[i], want[i])
		}
	}
}

func TestExemptRemoteBranches(t *testing.T) {
	t.Parallel()
	bs := []model.Branch{{Name: "main", IsHead: true, Upstream: "origin/main"}, {Name: "x", Upstream: "origin/x"}}
	rbs := []model.RemoteBranch{{Name: "origin/x", Branch: "x"}, {Name: "origin/main", Branch: "main"}}
	got := ExemptRemoteBranches(rbs, bs)
	if got[0] || !got[1] {
		t.Errorf("exempt = %v; want [false true]", got)
	}
	// Detached HEAD / no upstream: nothing exempt.
	if got := ExemptRemoteBranches(rbs, []model.Branch{{Name: "main", IsHead: true}}); got[0] || got[1] {
		t.Errorf("no upstream: %v", got)
	}
}

func TestRemoteBranchRowsUseBranchPart(t *testing.T) {
	t.Parallel()
	rows := RemoteBranchRows([]model.RemoteBranch{{Name: "origin/feat/x", Branch: "feat/x", UnixTime: 7}})
	if rows[0].Name != "feat/x" || rows[0].UnixTime != 7 {
		t.Errorf("rows = %+v", rows)
	}
}

func TestBranchFiltersFromEffectiveConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // keep the real global file out
	dir, svc := newRealRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte("[[branches.filter]]\nslot = 3\nname = \"wip\"\nsuffix = \"-wip\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	all, warnings, err := svc.BranchFilters(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %q", warnings)
	}
	if all[2].Name != "wip" || !all[2].Usable() || all[2].Mode != branchfilter.ModeHide {
		t.Errorf("slot 3 = %+v err=%v", all[2].Slot, all[2].Err)
	}
	if !all[0].Empty {
		t.Errorf("slot 1 should be empty")
	}
}

// TestRepoHealthCommonDirMatchesGitCommonDir guards the key the two frontends
// use to scope the stored active branch-filter slot: the TUI keys on
// RepoHealth.GitCommonDir, the web on GitCommonDir directly. Both come from
// `git rev-parse --git-common-dir`; this test keeps them from drifting apart.
func TestRepoHealthCommonDirMatchesGitCommonDir(t *testing.T) {
	_, svc := newRealRepo(t)
	ctx := context.Background()
	h, err := svc.RepoHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cd, err := svc.GitCommonDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if h.GitCommonDir == "" || h.GitCommonDir != cd {
		t.Errorf("RepoHealth.GitCommonDir = %q, GitCommonDir = %q — the branch-filter slot record is keyed by both", h.GitCommonDir, cd)
	}
}
