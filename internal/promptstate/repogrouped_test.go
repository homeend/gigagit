package promptstate

import (
	"path/filepath"
	"testing"
)

// The TUI repo switcher's grouping pref round-trips and survives a sibling
// write.
func TestRepoGroupedRoundTrips(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	if fs.RepoGrouped() {
		t.Fatal("a fresh store must read false")
	}
	if err := fs.SetRepoGrouped(true); err != nil {
		t.Fatal(err)
	}
	if err := fs.SuppressPrompt("x"); err != nil { // a sibling write read-merges
		t.Fatal(err)
	}
	if !fs.RepoGrouped() {
		t.Fatal("set true must read back true, even after a sibling write")
	}
	if err := fs.SetRepoGrouped(false); err != nil {
		t.Fatal(err)
	}
	if fs.RepoGrouped() {
		t.Fatal("set false must read back false")
	}
}
