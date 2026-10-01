package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

func TestHostReloadSources(t *testing.T) {
	head := []string{"status", "commits", "branches", "reflog"}
	cases := []struct {
		args []string
		want []string
		fail bool // also after exit 1 (a conflict leaves the tree changed)
	}{
		{[]string{"status"}, nil, false},
		{[]string{"log"}, nil, false},
		{[]string{"branch", "ls"}, nil, false},
		{[]string{"branch", "create", "x"}, []string{"branches"}, false},
		{[]string{"branch", "delete", "x"}, []string{"branches", "commits", "notes"}, false},
		{[]string{"branch", "rename", "a", "b"}, []string{"status", "branches", "commits", "worktrees", "notes", "reflog"}, false},
		{[]string{"worktree", "list", "--json"}, nil, false},
		{[]string{"worktree", "claim", "/w"}, []string{"worktrees", "branches"}, false},
		{[]string{"worktree", "recycle", "/w", "b"}, []string{"worktrees", "branches", "commits", "notes"}, true},
		{[]string{"commit", "-m", "x"}, head, true},
		{[]string{"merge", "x"}, head, true},
		{[]string{"apply", "--am", "p"}, head, true},
		{[]string{"pull"}, []string{"status", "commits", "branches", "reflog", "remotes"}, true},
		{[]string{"push"}, []string{"branches", "remotes", "commits"}, false},
		{[]string{"add", "a.txt"}, []string{"status"}, false},
		{[]string{"switch", "main"}, []string{"status", "commits", "branches", "reflog", "worktrees"}, true},
		{[]string{"remote", "ls"}, nil, false},
		{[]string{"remote", "fetch"}, []string{"remotes"}, false},
		{[]string{"remote", "remove", "o/x"}, []string{"branches", "remotes", "commits", "notes"}, false},
		{[]string{"tag", "list"}, nil, false},
		{[]string{"tag", "rm", "v1"}, []string{"tags", "commits"}, false},
		{[]string{"tag", "co", "v1"}, []string{"status", "commits", "branches", "reflog", "worktrees"}, true},
		{[]string{"stash", "list"}, nil, false},
		{[]string{"stash", "pop"}, []string{"status"}, true},
		{[]string{"shelf", "list"}, nil, false},
		{[]string{"shelf", "add", "a.txt"}, nil, false}, // a copy into the store: the tree is untouched
		{[]string{"shelf", "restore", "1"}, []string{"status"}, false},
		{[]string{"shelf", "cherry-pick", "1"}, head, true},
		{[]string{"bookmark", "paste", "1"}, []string{"status"}, false},
		{[]string{"unlock", "--yes"}, []string{"status"}, false},
		{[]string{"versions", "restore", "x"}, []string{"status", "branches", "commits", "worktrees", "reflog"}, true},
		{[]string{"compare", "--list"}, nil, false},
		{[]string{"compare", "--save", "a", "b"}, []string{"previews"}, false},
		{[]string{"note", "add"}, nil, false}, // notes post their own reload
		{[]string{"agent", "start"}, nil, false},
		{[]string{"prefix", "resolve", "--bump", "x"}, nil, false},
	}
	for _, c := range cases {
		got, fail := hostReloadSources(c.args[0], c.args[1:])
		if !slices.Equal(got, c.want) || fail != c.fail {
			t.Errorf("%v → %v (after exit 1: %v), want %v (%v)", c.args, got, fail, c.want, c.fail)
		}
	}
}

// inboxReloads reads the reload commands posted to inbox.
func inboxReloads(t *testing.T, inbox string) []steer.Command {
	t.Helper()
	ents, _ := os.ReadDir(inbox)
	var out []steer.Command
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(inbox, e.Name()))
		if err != nil {
			continue
		}
		var c steer.Command
		if json.Unmarshal(b, &c) == nil && c.Cmd == "reload" {
			out = append(out, c)
		}
	}
	return out
}

// Inside a gg console a mutating verb tells the hosting TUI what changed,
// with the worktree it acted in; a read or a failure tells it nothing.
func TestMutatingVerbNudgesTheHostTUI(t *testing.T) {
	dir := newCLIRepo(t)
	inbox := t.TempDir()
	t.Setenv("GG_INBOX", inbox)
	if code, _, _ := runCLI(t, dir, "branch", "ls"); code != 0 {
		t.Fatal("branch ls")
	}
	if code, _, _ := runCLI(t, dir, "branch", "delete", "no-such-branch"); code == 0 {
		t.Fatal("deleting a missing branch must fail")
	}
	if n := len(inboxReloads(t, inbox)); n != 0 {
		t.Fatalf("a read and a failure posted %d reloads", n)
	}
	if code, _, errb := runCLI(t, dir, "branch", "create", "job"); code != 0 {
		t.Fatalf("branch create: %s", errb)
	}
	rs := inboxReloads(t, inbox)
	if len(rs) != 1 || !slices.Contains(rs[0].Sources, "branches") || !domain.SameCheckout(rs[0].Dir, dir) {
		t.Fatalf("reloads = %+v", rs)
	}
}

func TestNoNudgeOutsideAConsole(t *testing.T) {
	dir := newCLIRepo(t)
	t.Setenv("GG_INBOX", "")
	if code, _, errb := runCLI(t, dir, "branch", "create", "job"); code != 0 {
		t.Fatalf("branch create: %s", errb)
	}
}

// A merge that stops on conflicts exits 1 with MERGE_HEAD and markers in the
// tree: the TUI must hear of it like of a clean merge.
func TestConflictedMergeStillNudges(t *testing.T) {
	dir := newCLIRepo(t)
	inbox := t.TempDir()
	t.Setenv("GG_INBOX", inbox)
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("base\n"), 0o644)
	git("add", "c.txt")
	git("commit", "-qm", "base")
	git("checkout", "-qb", "other")
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("other\n"), 0o644)
	git("commit", "-qam", "other")
	git("checkout", "-q", "-")
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("mine\n"), 0o644)
	git("commit", "-qam", "mine")
	code, _, errb := runCLI(t, dir, "merge", "other")
	if code != 1 {
		t.Fatalf("a conflicted merge = %d %q, want exit 1", code, errb)
	}
	if rs := inboxReloads(t, inbox); len(rs) != 1 || !slices.Contains(rs[0].Sources, "status") {
		t.Fatalf("reloads after a conflicted merge = %+v", rs)
	}
}
