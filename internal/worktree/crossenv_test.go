package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// statOnly fakes reachability: only paths in the set "exist".
func statOnly(ok ...string) func(string) error {
	set := map[string]bool{}
	for _, p := range ok {
		set[p] = true
	}
	return func(p string) error {
		if set[p] {
			return nil
		}
		return errors.New("missing")
	}
}

func TestNormalizeWorktreeLinkRewritesForeignPointer(t *testing.T) {
	wt := t.TempDir()
	// a WSL-created worktree seen from Windows: the pointer is /mnt notation
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /mnt/q/repo/.git/worktrees/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stat := statOnly(`Q:\repo\.git\worktrees\x`)
	if !NormalizeWorktreeLink(stat, "windows", wt) {
		t.Fatal("expected a rewrite")
	}
	b, _ := os.ReadFile(filepath.Join(wt, ".git"))
	if strings.TrimSpace(string(b)) != `gitdir: Q:\repo\.git\worktrees\x` {
		t.Errorf("link = %q", b)
	}
}

func TestNormalizeWorktreeLinkLeavesGoodAndUntranslatable(t *testing.T) {
	wt := t.TempDir()
	orig := "gitdir: /mnt/q/repo/.git/worktrees/x\n"
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	// already reachable → untouched
	if NormalizeWorktreeLink(statOnly("/mnt/q/repo/.git/worktrees/x"), "windows", wt) {
		t.Error("reachable pointer must not be rewritten")
	}
	// translated target missing → untouched
	if NormalizeWorktreeLink(statOnly(), "windows", wt) {
		t.Error("missing translation must not be rewritten")
	}
	// not a gitdir file at all → untouched
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if NormalizeWorktreeLink(statOnly(), "windows", wt) {
		t.Error("non-gitdir content must not be rewritten")
	}
	// no .git file (a main worktree dir) → untouched, no error
	if NormalizeWorktreeLink(statOnly(), "windows", t.TempDir()) {
		t.Error("missing .git file must not report a rewrite")
	}
}

// The translation table itself (moved from internal/tui — kept close to the
// implementation; the TUI aliases these symbols).
func TestTranslatePathTable(t *testing.T) {
	cases := []struct {
		goos, in, want string
		ok             bool
	}{
		{"windows", "/mnt/t/others/repo", `T:\others\repo`, true},
		// Windows filepath.Clean turns git's /mnt/t/… into \mnt\t\… before it gets here.
		{"windows", `\mnt\t\others\repo`, `T:\others\repo`, true},
		{"windows", `\mnt\t`, `T:\`, true},
		{"windows", "/mnt/c", `C:\`, true},
		{"windows", "/mnt/tt/x", "", false},
		{"windows", "/home/u/repo", "", false},
		{"linux", `T:\others\repo`, "/mnt/t/others/repo", true},
		{"linux", "T:/others/repo", "/mnt/t/others/repo", true},
		{"linux", "c:", "/mnt/c", true},
		{"linux", "1:/x", "", false},
		{"linux", "/mnt/t/x", "", false},
		{"darwin", "/mnt/t/x", "", false},
	}
	for _, c := range cases {
		got, ok := TranslatePath(c.goos, c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("TranslatePath(%s, %q) = %q,%v want %q,%v", c.goos, c.in, got, ok, c.want, c.ok)
		}
	}
}

// A worktree Windows gg created, seen from WSL: its admin record holds
// "T:/others/repo.wt/x/.git", which Linux git reads as RELATIVE and lists as
// <repo>/.git/worktrees/x/T:/others/repo.wt/x. The tail from the drive
// letter is the path the other environment meant — repairable, not lost.
func TestCheckSwitchTargetGitJoinedForeignPath(t *testing.T) {
	t.Parallel()
	joined := "/mnt/t/others/test-1/.git/worktrees/c-1/T:/others/test-1.worktrees/c-1"
	stat := statOnly("/mnt/t/others/test-1.worktrees/c-1")
	if v, p := CheckSwitchTarget(stat, "linux", joined); v != SwitchRepairable || p != "/mnt/t/others/test-1.worktrees/c-1" {
		t.Errorf("joined forward-slash record = %v %q, want repairable at the translated path", v, p)
	}
	back := `/mnt/t/others/test-1/.git/worktrees/c-1/T:\others\test-1.worktrees\c-1`
	if v, p := CheckSwitchTarget(stat, "linux", back); v != SwitchRepairable || p != "/mnt/t/others/test-1.worktrees/c-1" {
		t.Errorf("joined backslash record = %v %q", v, p)
	}
	if v, _ := CheckSwitchTarget(statOnly(), "linux", joined); v != SwitchUnreachable {
		t.Errorf("a translation that does not exist here stays unreachable: %v", v)
	}
	if v, _ := CheckSwitchTarget(stat, "windows", joined); v != SwitchUnreachable {
		t.Errorf("only Linux git glues a drive path on: %v", v)
	}
}
