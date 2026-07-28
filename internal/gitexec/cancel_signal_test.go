package gitexec

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// writeFakeGit drops an executable shell script at <dir>/fake-git and returns
// its path. ExecRunner takes gitPath as a parameter, so a script standing in
// for git is the cleanest way to observe which signal a cancel delivers.
func writeFakeGit(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "fake-git")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	return p
}

// TestCancelDeliversSIGTERM is the regression test for stale .git/index.lock
// files. Go's default cancel action for exec.CommandContext is Process.Kill()
// — SIGKILL — which git cannot trap, so a cancelled `git add`/`git status`
// leaves its lockfile behind forever. git DOES clean up on SIGTERM (its
// sigchain handler removes every lockfile and re-raises), so the runner must
// terminate gracefully and only escalate if git ignores it.
func TestCancelDeliversSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signals; Windows can only Kill (see terminate in signal_windows.go)")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "got-term")
	// Traps TERM, records that it arrived (git's lockfile cleanup happens
	// here), then exits. The sleep loop keeps the shell able to run the trap.
	git := writeFakeGit(t, dir, "trap 'echo yes > \""+marker+"\"; exit 143' TERM\nwhile :; do sleep 0.05; done\n")

	r := NewExecRunner(git, dir, nil)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	if _, err := r.Run(ctx, "fake", nil); err == nil {
		t.Fatal("cancelled run should return an error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("cancel took %v — escalated to kill instead of terminating gracefully", elapsed)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("subprocess never received SIGTERM — a killed git leaves index.lock behind")
	}
}

// TestCancelStreamDeliversSIGTERM covers the second invocation path: long
// operations (fetch, checkout, worktree add) all run through Stream.
func TestCancelStreamDeliversSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signals")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "got-term")
	git := writeFakeGit(t, dir, "trap 'echo yes > \""+marker+"\"; exit 143' TERM\necho started\nwhile :; do sleep 0.05; done\n")

	r := NewExecRunner(git, dir, nil)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	if _, err := r.Stream(ctx, "fake", nil, func(string) {}); err == nil {
		t.Fatal("cancelled stream should return an error")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("streamed subprocess never received SIGTERM")
	}
}

// TestCancelEscalatesToKill proves the graceful signal is bounded: a git that
// ignores SIGTERM (or wedges inside its handler) must still die, or a
// cancelled background read would hold the repo gate forever.
func TestCancelEscalatesToKill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signals")
	}
	dir := t.TempDir()
	git := writeFakeGit(t, dir, "trap '' TERM\nwhile :; do sleep 0.05; done\n")

	r := NewExecRunner(git, dir, nil)
	r.waitDelay = 200 * time.Millisecond // production uses waitDelay (2s)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	done := make(chan struct{})
	go func() {
		_, _ = r.Run(ctx, "fake", nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a SIGTERM-ignoring subprocess was never escalated to kill")
	}
}
