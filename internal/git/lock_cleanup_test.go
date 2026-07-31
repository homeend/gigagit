package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestCancelledAddLeavesNoIndexLock is the end-to-end proof against a REAL
// git that cancelling an in-flight operation does not strand .git/index.lock.
//
// gg cancels git constantly — every user keypress preempts the background
// refresh lane (tui.startOp → bgCancel) — and with Go's default SIGKILL
// cancellation a git killed mid-write left its lock behind, so every
// subsequent operation failed with "Another git process seems to be running
// in this repository" until the user deleted the file by hand.
//
// The window is made deterministic with a slow clean filter: `git add` takes
// index.lock before it filters files, so the lock is provably held for the
// whole duration of the filter.
func TestCancelledAddLeavesNoIndexLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no SIGTERM on Windows: cancellation there can still strand a lock, which the stale-lock notice recovers")
	}
	dir, runner := newTestRepo(t)

	gitCfg := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitCfg("config", "filter.slow.clean", "sleep 10; cat")
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitattributes", "slow.txt filter=slow\n")
	write("slow.txt", "content\n")

	lock := filepath.Join(dir, ".git", "index.lock")
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel only once the lock is provably held — otherwise a fast git would
	// finish first and the test would pass without exercising anything.
	held := make(chan bool, 1)
	go func() {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(lock); err == nil {
				held <- true
				cancel()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		held <- false
		cancel()
	}()

	if _, err := runner.Run(ctx, "git add", []string{"add", "."}); err == nil {
		t.Fatal("cancelled add should return an error")
	}
	if !<-held {
		t.Fatal("git add never took index.lock — test would be vacuous")
	}

	// git's signal handler removes the lock on the way out. Give the process a
	// moment to finish unwinding, then assert the repo is usable again.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(lock); os.IsNotExist(err) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(lock); err == nil {
		t.Fatal("cancelled git left .git/index.lock behind — the next operation would fail with " +
			`"Another git process seems to be running in this repository"`)
	}

	// The real user-visible assertion: the next operation still works.
	gitCfg("config", "--unset", "filter.slow.clean")
	if _, err := runner.Run(context.Background(), "git add", []string{"add", "README.md"}); err != nil {
		t.Fatalf("follow-up operation failed after a cancelled one: %v", err)
	}
}
