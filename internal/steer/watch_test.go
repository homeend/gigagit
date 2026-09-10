package steer

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWatchWakesOnAPostedCommand(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "inbox")
	w, err := Watch(dir) // must create the dir it watches
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()
	if _, err := Post(dir, Command{Cmd: "reload", Sources: []string{"notes"}}); err != nil {
		t.Fatalf("Post: %v", err)
	}
	select {
	case <-w.Events():
	case <-time.After(3 * time.Second):
		t.Fatal("no wake within 3s of a posted command")
	}
}

func TestWatchIgnoresPresenceTouches(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "inbox")
	w, err := Watch(dir)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()
	// A presence re-touch happens every second in every session sharing this
	// inbox; waking the consumer for it would defeat the whole point.
	if err := Touch(dir, WebPresence, Presence{PID: 1, Worktree: "/w"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.Events():
		t.Fatal("a presence write must not wake the consumer")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestWatchCloseIsIdempotentAndClosesEvents(t *testing.T) {
	t.Parallel()
	w, err := Watch(t.TempDir())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	w.Close()
	w.Close() // a second Close must not panic on a closed channel
	select {
	case _, open := <-w.Events():
		if open {
			t.Error("Events delivered a value after Close")
		}
	case <-time.After(time.Second):
		t.Error("Events was not closed by Close")
	}
}
