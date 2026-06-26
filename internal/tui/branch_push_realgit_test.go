package tui

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
)

func pushRealGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// TestPushBranchRowPushesSelectedNotCurrent is the regression guard for the
// reported bug: pushing a HIGHLIGHTED non-current branch must push THAT branch,
// not the checked-out one. (The row-gating tests only check the label, which is
// a proxy — this drives a real git push end to end.)
func TestPushBranchRowPushesSelectedNotCurrent(t *testing.T) {
	bare := t.TempDir()
	pushRealGit(t, bare, "init", "--bare")

	dir := t.TempDir()
	pushRealGit(t, dir, "init")
	pushRealGit(t, dir, "config", "user.email", "a@b.c")
	pushRealGit(t, dir, "config", "user.name", "a")
	pushRealGit(t, dir, "remote", "add", "origin", bare)
	pushRealGit(t, dir, "commit", "--allow-empty", "-m", "init") // on master
	pushRealGit(t, dir, "checkout", "-b", "feature")
	pushRealGit(t, dir, "commit", "--allow-empty", "-m", "feature commit")
	pushRealGit(t, dir, "checkout", "master") // HEAD is NOT feature

	svc := domain.New(&git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))})
	m := New(svc)
	m.loading = false
	m.focus = panelBranches
	m.status.Branch = "master"
	m.branches = []model.Branch{
		{Name: "master", IsHead: true},
		{Name: "feature"}, // non-current, never pushed
	}
	m.sel = map[panel]int{panelBranches: 1} // highlight feature

	row, ok := m.pushBranchRow()
	if !ok {
		t.Fatal("pushBranchRow absent on the Branches panel")
	}
	tm, cmd := row.run(m)
	m = tm.(Model)

	deadline := time.Now().Add(10 * time.Second)
	for cmd != nil {
		if time.Now().After(deadline) {
			t.Fatal("timed out pumping op messages")
		}
		msg := cmd()
		var x tea.Model
		x, cmd = m.Update(msg)
		m = x.(Model)
		if _, done := msg.(opFinishedMsg); done {
			break
		}
	}

	out, err := exec.Command("git", "ls-remote", bare).CombinedOutput()
	if err != nil {
		t.Fatalf("ls-remote: %v\n%s", err, out)
	}
	remote := string(out)
	if !strings.Contains(remote, "refs/heads/feature") {
		t.Fatalf("the selected branch 'feature' did NOT land on the remote (the bug). statusMsg=%q remote=%s", m.statusMsg, remote)
	}
	if strings.Contains(remote, "refs/heads/master") {
		t.Fatalf("'master' was pushed instead of the selected 'feature' — selection ignored. remote=%s", remote)
	}
}
