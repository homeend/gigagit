package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

func newRepoDir(t *testing.T) (string, *git.Repo) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")
	return dir, &git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
}

func driveOp(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; i < 50 && m.running; i++ {
		if cmd == nil {
			t.Fatal("ran out of commands before the operation finished")
		}
		msg := cmd()
		updated, next := m.Update(msg)
		m = updated.(Model)
		cmd = next
	}
	if m.running {
		t.Fatal("operation did not finish")
	}
	return m
}

// blockingOp parks until its context is cancelled — the shape of an op
// orphaned by the user quitting mid-run.
type blockingOp struct{}

func (blockingOp) Run(ctx context.Context, _ engine.OpDeps) (engine.Result, error) {
	<-ctx.Done()
	return engine.Result{}, ctx.Err()
}

func TestQuitCancelsRunningOp(t *testing.T) {
	m := New(domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()}))
	m2, _ := m.startOp(blockingOp{})
	if m2.opCancel == nil {
		t.Fatal("startOp did not arm opCancel")
	}
	m2.opCancel() // what run.go does when the program exits mid-op
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-m2.opMsgs:
			if fin, ok := msg.(opFinishedMsg); ok {
				if !errors.Is(fin.err, context.Canceled) {
					t.Fatalf("op finished with %v, want context.Canceled", fin.err)
				}
				return
			}
		case <-deadline:
			t.Fatal("cancelled op never finished")
		}
	}
}

func TestRunCommitOperationFinishesAndClearsRunning(t *testing.T) {
	dir, repo := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)

	m, cmd := m.startOp(engine.Commit{Message: "second", All: true})
	if !m.running {
		t.Fatal("expected running=true right after startOp")
	}
	m = driveOp(t, m, cmd)
	if m.statusMsg == "" {
		t.Fatal("expected a status message after the operation")
	}
}
