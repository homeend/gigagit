package filewatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const debounce = 50 * time.Millisecond

// pair is a temp dir holding two files, a and b, and a watcher over it.
func pair(t *testing.T) (w *Watcher, a, b string) {
	t.Helper()
	dir := t.TempDir()
	a, b = filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	write(t, a, "a")
	write(t, b, "b")
	w, err := New(debounce)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w, a, b
}

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func expectEvent(t *testing.T, w *Watcher, want string) {
	t.Helper()
	select {
	case got := <-w.Events():
		if got != filepath.Clean(want) {
			t.Fatalf("event %q, want %q", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("no event for %s", want)
	}
}

func expectSilence(t *testing.T, w *Watcher) {
	t.Helper()
	select {
	case got := <-w.Events():
		t.Fatalf("unexpected event %q", got)
	case <-time.After(400 * time.Millisecond):
	}
}

func TestWriteToAWatchedFileEmitsItsPath(t *testing.T) {
	t.Parallel()
	w, a, _ := pair(t)
	w.Set([]string{a})
	write(t, a, "changed")
	expectEvent(t, w, a)
}

func TestUnwatchedSiblingIsSilent(t *testing.T) {
	t.Parallel()
	w, a, b := pair(t)
	w.Set([]string{a})
	write(t, b, "changed")
	expectSilence(t, w)
}

func TestSetDropsAPath(t *testing.T) {
	t.Parallel()
	w, a, _ := pair(t)
	w.Set([]string{a})
	w.Set(nil)
	write(t, a, "changed")
	expectSilence(t, w)
}

func TestRecreatedFileEmits(t *testing.T) {
	t.Parallel()
	w, a, _ := pair(t)
	w.Set([]string{a})
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	expectEvent(t, w, a) // the delete itself is a change
	write(t, a, "back")
	expectEvent(t, w, a)
}

func TestCloseClosesEvents(t *testing.T) {
	t.Parallel()
	w, a, _ := pair(t)
	w.Set([]string{a})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-w.Events(); ok {
		t.Fatal("events channel still open after Close")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	w.Set([]string{a}) // after Close: a no-op, never a panic
}
