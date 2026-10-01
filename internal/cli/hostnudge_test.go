package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

func TestHostReloadSources(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"status"}, nil},
		{[]string{"log"}, nil},
		{[]string{"branch", "ls"}, nil},
		{[]string{"branch", "create", "x"}, []string{"branches", "worktrees"}},
		{[]string{"worktree", "list", "--json"}, nil},
		{[]string{"worktree", "recycle", "/w", "b"}, []string{"worktrees", "branches"}},
		{[]string{"worktree", "claim", "/w"}, []string{"worktrees", "branches"}},
		{[]string{"commit", "-m", "x"}, []string{"status", "commits", "branches"}},
		{[]string{"add", "a.txt"}, []string{"status"}},
		{[]string{"switch", "main"}, []string{"status", "commits", "branches", "worktrees"}},
		{[]string{"remote", "ls"}, nil},
		{[]string{"remote", "fetch"}, []string{"remotes", "branches", "commits"}},
		{[]string{"tag", "list"}, nil},
		{[]string{"tag", "create", "v1"}, []string{"tags"}},
		{[]string{"stash", "list"}, nil},
		{[]string{"stash", "-m", "x"}, []string{"status"}},
		{[]string{"shelf", "list"}, nil},
		{[]string{"shelf", "restore", "1"}, []string{"status"}},
		{[]string{"compare", "--list"}, nil},
		{[]string{"compare", "--save", "a", "b"}, []string{"previews"}},
		{[]string{"note", "add"}, nil}, // notes post their own reload
		{[]string{"agent", "start"}, nil},
		{[]string{"prefix", "resolve", "--bump", "x"}, nil},
	}
	for _, c := range cases {
		if got := hostReloadSources(c.args[0], c.args[1:]); !slices.Equal(got, c.want) {
			t.Errorf("%v → %v, want %v", c.args, got, c.want)
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
