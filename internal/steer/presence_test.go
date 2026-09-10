package steer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTouchThenLive(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "inbox")
	want := Presence{PID: 4242, Worktree: "/abs/wt", Started: "2026-09-10T09:00:00Z"}
	if err := Touch(dir, TUIPresence, want); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got, ok := Live(dir, TUIPresence)
	if !ok {
		t.Fatal("Live = false right after Touch")
	}
	if got.PID != want.PID || got.Worktree != want.Worktree || got.Started != want.Started {
		t.Errorf("Live = %+v, want %+v", got, want)
	}
}

func TestTouchRefreshesMtimeWithoutRewriting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := Touch(dir, TUIPresence, Presence{PID: 1, Worktree: "/a"}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, TUIPresence)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	// The re-touch carries a DIFFERENT payload; the file on disk keeps the
	// original because a live session only refreshes the clock.
	if err := Touch(dir, TUIPresence, Presence{PID: 2, Worktree: "/b"}); err != nil {
		t.Fatalf("re-Touch: %v", err)
	}
	got, ok := Live(dir, TUIPresence)
	if !ok {
		t.Fatal("Live = false after a re-Touch")
	}
	if got.PID != 1 {
		t.Errorf("PID = %d, want the original 1 — a re-touch is os.Chtimes, not a rewrite", got.PID)
	}
}

func TestTouchRecreatesAMissingPresence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A CLI Live check swept the file while this session stalled past the
	// window; the next tick must bring it back, not fail on os.Chtimes.
	if err := Touch(dir, TUIPresence, Presence{PID: 9, Worktree: "/w"}); err != nil {
		t.Fatalf("Touch on an empty dir: %v", err)
	}
	os.Remove(filepath.Join(dir, TUIPresence))
	if err := Touch(dir, TUIPresence, Presence{PID: 9, Worktree: "/w"}); err != nil {
		t.Fatalf("Touch after the file vanished: %v", err)
	}
	if _, ok := Live(dir, TUIPresence); !ok {
		t.Error("Live = false: Touch must recreate a presence that is gone")
	}
}

func TestLiveIsFalseAndSweepsAStalePresence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := Touch(dir, WebPresence, Presence{PID: 5, Worktree: "/w", URL: "http://127.0.0.1:7777"}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, WebPresence)
	old := time.Now().Add(-LiveWindow - time.Second)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	if got, ok := Live(dir, WebPresence); ok {
		t.Fatalf("Live = %+v true, want false for an mtime older than %v", got, LiveWindow)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("a stale presence must be swept by the Live check that finds it (err = %v)", err)
	}
}

func TestLiveOnAMissingDirAndRemove(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, ok := Live(filepath.Join(dir, "nope"), TUIPresence); ok {
		t.Error("Live on a missing dir must be false, not a panic")
	}
	if err := Touch(dir, TUIPresence, Presence{PID: 3, Worktree: "/w"}); err != nil {
		t.Fatal(err)
	}
	Remove(dir, TUIPresence)
	if _, ok := Live(dir, TUIPresence); ok {
		t.Error("Live = true after Remove")
	}
	Remove(dir, TUIPresence) // a second Remove is a silent no-op
}
