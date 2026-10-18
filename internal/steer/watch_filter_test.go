package steer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func wakes(t *testing.T, w *Watcher) bool {
	t.Helper()
	select {
	case <-w.Events():
		return true
	case <-time.After(3 * time.Second):
		return false
	}
}

func TestWatchFilterWakesOnKeptNamesOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := WatchFilter(dir, func(name string) bool { return strings.HasSuffix(name, ".md") })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if wakes(t, w) {
		t.Fatal("a name the filter drops must not wake")
	}
	if err := os.WriteFile(filepath.Join(dir, ".tmp-1"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, ".tmp-1"), filepath.Join(dir, "a.md")); err != nil {
		t.Fatal(err)
	}
	if !wakes(t, w) {
		t.Fatal("a rename onto a kept name must wake")
	}
	if err := os.Remove(filepath.Join(dir, "a.md")); err != nil {
		t.Fatal(err)
	}
	if !wakes(t, w) {
		t.Fatal("removing a kept name must wake")
	}
}

func TestWatchKeepsCommandFilesOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := Watch(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := os.WriteFile(filepath.Join(dir, "tui.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if wakes(t, w) {
		t.Fatal("a presence file must not wake the command watcher")
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd-1.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !wakes(t, w) {
		t.Fatal("a command file must wake")
	}
}
