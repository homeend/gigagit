package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/wtguard"
	"github.com/homeend/gigagit/internal/sessionreg"
)

func inventoryRepo(t *testing.T) (main string, svc *Service, reg string) {
	t.Helper()
	main, svc = newRealRepo(t)
	reg = t.TempDir()
	svc.UseSessionRegistryDir(reg)
	return
}

func addWT(t *testing.T, main, branch string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), branch)
	if out, err := exec.Command("git", "-C", main, "worktree", "add", "-b", branch, wt).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	return wt
}

func find(t *testing.T, infos []WorktreeInfo, path string) WorktreeInfo {
	t.Helper()
	for _, w := range infos {
		if SameCheckout(w.Path, path) {
			return w
		}
	}
	t.Fatalf("%s not in inventory", path)
	return WorktreeInfo{}
}

func pol() InventoryPolicy { return InventoryPolicy{StaleAfter: 14 * 24 * time.Hour} }

func TestInventoryReasons(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	clean := addWT(t, main, "clean")
	fresh := addWT(t, main, "fresh")
	os.WriteFile(filepath.Join(fresh, "new.txt"), []byte("x"), 0o644)
	stale := addWT(t, main, "stale")
	f := filepath.Join(stale, "old.txt")
	os.WriteFile(f, []byte("x"), 0o644)
	old := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(f, old, old)
	det := addWT(t, main, "det")
	exec.Command("git", "-C", det, "checkout", "--detach").Run()
	locked := addWT(t, main, "locked")
	os.WriteFile(filepath.Join(git.GitDirAt(locked), "index.lock"), nil, 0o644)

	p := pol()
	p.Reserved = []string{clean}
	infos, err := svc.WorktreeInventory(context.Background(), p, false)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]string{
		main: {"main"}, clean: {"reserved"}, fresh: {"dirty-recent"},
		det: {"detached"}, locked: {"git-lock"},
	}
	for path, want := range cases {
		if got := find(t, infos, path).BlockedBy; !slices.Equal(got, want) {
			t.Errorf("%s blocked_by = %v, want %v", filepath.Base(path), got, want)
		}
	}
	s := find(t, infos, stale)
	if !s.Free || s.Recycle != "shelve" || s.Dirty() == nil || s.Dirty().Untracked != 1 {
		t.Fatalf("stale = %+v", s)
	}
	if find(t, infos, locked).Facts["git_lock"] != true {
		t.Fatal("git_lock fact")
	}
}

func TestInventoryReservedRelativeToMain(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "rel")
	rel, _ := filepath.Rel(main, wt)
	p := pol()
	p.Reserved = []string{rel}
	infos, _ := svc.WorktreeInventory(context.Background(), p, false)
	if w := find(t, infos, wt); w.Facts["reserved"] != true || w.Free {
		t.Fatalf("rel reserved = %+v", w)
	}
}

func TestInventoryPausedOp(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "paused")
	os.WriteFile(filepath.Join(git.GitDirAt(wt), "MERGE_HEAD"), []byte("0000000000000000000000000000000000000000\n"), 0o644)
	infos, _ := svc.WorktreeInventory(context.Background(), pol(), false)
	if got := find(t, infos, wt).BlockedBy; !slices.Equal(got, []string{"paused-op"}) {
		t.Fatalf("blocked_by = %v", got)
	}
}

func TestInventorySessionAndTUIFromRegistry(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	a := addWT(t, main, "a")
	b := addWT(t, main, "b")
	sessionreg.Write(reg, "other", sessionreg.Registry{PID: 9, Worktree: b,
		Sessions: []sessionreg.Entry{{ID: "other/s1", Dir: a, Agent: "claude", State: "running"}}})
	infos, _ := svc.WorktreeInventory(context.Background(), pol(), false)
	if got := find(t, infos, a).BlockedBy; !slices.Equal(got, []string{"session"}) {
		t.Fatalf("a blocked_by = %v", got)
	}
	if got := find(t, infos, b).BlockedBy; !slices.Equal(got, []string{"tui"}) {
		t.Fatalf("b blocked_by = %v", got)
	}
	if v, ok := find(t, infos, a).Facts["dirty"]; !ok || v != nil {
		t.Fatalf("status must be skipped for a blocked worktree (dirty fact %v, present %v)", v, ok)
	}
}

func TestInventoryFreeOrder(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	s1 := addWT(t, main, "s1")
	s2 := addWT(t, main, "s2")
	c := addWT(t, main, "c")
	for i, wt := range []string{s1, s2} {
		f := filepath.Join(wt, "d.txt")
		os.WriteFile(f, []byte("x"), 0o644)
		at := time.Now().Add(-time.Duration(20+i*10) * 24 * time.Hour) // s2 older
		os.Chtimes(f, at, at)
	}
	infos, _ := svc.WorktreeInventory(context.Background(), pol(), true)
	var got []string
	for _, w := range infos {
		got = append(got, filepath.Base(w.Path))
	}
	if want := []string{filepath.Base(c), "s2", "s1"}; !slices.Equal(got, want) {
		t.Fatalf("free order = %v, want %v", got, want)
	}
}

func TestInventoryLastChangeSkipsDeletedPaths(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "del")
	tracked := firstTrackedFile(t, wt)
	os.Remove(filepath.Join(wt, tracked))
	f := filepath.Join(wt, "o.txt")
	os.WriteFile(f, []byte("x"), 0o644)
	old := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(f, old, old)
	infos, err := svc.WorktreeInventory(context.Background(), pol(), false)
	if err != nil {
		t.Fatal(err)
	}
	d := find(t, infos, wt).Dirty()
	if d == nil || d.LastChange == nil || d.LastChange.After(time.Now().Add(-29*24*time.Hour)) {
		t.Fatalf("dirty = %+v", d)
	}
}

func firstTrackedFile(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "ls-files").Output()
	if err != nil || len(out) == 0 {
		t.Fatalf("ls-files: %v", err)
	}
	return strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
}

func TestInventoryMissingWorktree(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	wt := addWT(t, main, "gone")
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	infos, err := svc.WorktreeInventory(context.Background(), pol(), false)
	if err != nil {
		t.Fatal(err)
	}
	w := find(t, infos, wt)
	if w.Free || !slices.Equal(w.BlockedBy, []string{"missing"}) {
		t.Fatalf("missing worktree = %+v", w)
	}
}

func TestInventoryOnlyOneWorktree(t *testing.T) {
	t.Parallel()
	main, svc, _ := inventoryRepo(t)
	a := addWT(t, main, "a")
	addWT(t, main, "b")
	infos, err := svc.inventory(context.Background(), pol(), a, "")
	if err != nil || len(infos) != 1 || !SameCheckout(infos[0].Path, a) {
		t.Fatalf("inventory(only) = %+v %v", infos, err)
	}
}

// A typo in stale_after must not fail every inventory and recycle: a clean
// worktree stays free, and only a dirty one is held back (as recent, with
// the reason), however old its changes.
func TestPolicyFromConfigBadAgeBlocksOnlyDirty(t *testing.T) {
	t.Parallel()
	p := PolicyFromConfig(config.AgentsConfig{StaleAfter: "soon"})
	main, svc, _ := inventoryRepo(t)
	clean := addWT(t, main, "clean")
	stale := addWT(t, main, "stale")
	f := filepath.Join(stale, "old.txt")
	os.WriteFile(f, []byte("x"), 0o644)
	old := time.Now().Add(-30 * 24 * time.Hour)
	os.Chtimes(f, old, old)
	infos, err := svc.WorktreeInventory(context.Background(), p, false)
	if err != nil {
		t.Fatal(err)
	}
	if c := find(t, infos, clean); !c.Free {
		t.Fatalf("clean = %+v", c)
	}
	s := find(t, infos, stale)
	if s.Free || !slices.Equal(s.BlockedBy, []string{"dirty-recent"}) || !strings.Contains(s.Blockers[0].Detail, "stale_after") {
		t.Fatalf("stale under a bad stale_after = %+v", s)
	}
	// The recycle op's guard report reads the repo config itself.
	os.WriteFile(filepath.Join(main, ".gg.toml"), []byte("[agents]\nstale_after = \"soon\"\n"), 0o644)
	if r, err := svc.GuardReport(context.Background(), wtguard.Target{Dir: clean, Branch: "clean"}); err != nil || len(r.Blockers) != 0 {
		t.Fatalf("guard report under a bad stale_after = %+v %v", r, err)
	}
}
