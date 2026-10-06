package gitexec

import (
	"context"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// A running git is listed (with its pid) while it runs and gone once it exits.
func TestInFlightListsARunningGit(t *testing.T) {
	r := NewExecRunner("git", t.TempDir(), nil)
	var seen []Proc
	_, err := r.Stream(context.Background(), "probe version", []string{"version"}, func(string) {
		if seen == nil {
			seen = InFlight()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(seen, func(p Proc) bool { return p.Name == "probe version" })
	if i < 0 || seen[i].PID == 0 || seen[i].Start.IsZero() {
		t.Fatalf("a running git must be listed with its pid: %+v", seen)
	}
	if slices.ContainsFunc(InFlight(), func(p Proc) bool { return p.Name == "probe version" }) {
		t.Fatal("an exited git must leave the list")
	}
	if _, err := r.Run(context.Background(), "probe run", []string{"version"}); err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(InFlight(), func(p Proc) bool { return p.Name == "probe run" }) {
		t.Fatal("Run must untrack its git on exit")
	}
}

// CancelInFlight ends a hung git (Run and Stream alike): the call returns a
// cancelled error and the list empties.
func TestCancelInFlightEndsAHungGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake git is a POSIX shell script")
	}
	dir := t.TempDir()
	git := writeFakeGit(t, dir, "echo started\nwhile :; do sleep 0.05; done\n")
	r := NewExecRunner(git, dir, nil)
	for _, stream := range []bool{false, true} {
		done := make(chan error, 1)
		go func() {
			var err error
			if stream {
				_, err = r.Stream(context.Background(), "hung", nil, func(string) {})
			} else {
				_, err = r.Run(context.Background(), "hung", nil)
			}
			done <- err
		}()
		deadline := time.Now().Add(5 * time.Second)
		for !slices.ContainsFunc(InFlight(), func(p Proc) bool { return p.Name == "hung" && p.PID != 0 }) {
			if time.Now().After(deadline) {
				t.Fatal("the hung git never showed in InFlight")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if n := CancelInFlight(); n < 1 {
			t.Fatalf("CancelInFlight signalled %d", n)
		}
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "cancelled") {
				t.Fatalf("stream=%v: want a cancelled error, got %v", stream, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("stream=%v: the hung git did not end", stream)
		}
		if slices.ContainsFunc(InFlight(), func(p Proc) bool { return p.Name == "hung" }) {
			t.Fatal("an ended git must leave the list")
		}
	}
}

// A git failing on its own is not reported as cancelled (the registry's
// derived context must stay live until the runner has classified the error).
func TestTrackedGitFailureIsNotCancelled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake git is a POSIX shell script")
	}
	dir := t.TempDir()
	r := NewExecRunner(writeFakeGit(t, dir, "echo boom >&2\nexit 3\n"), dir, nil)
	_, err := r.Run(context.Background(), "fails", nil)
	if err == nil || strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("want a plain failure, got %v", err)
	}
}
