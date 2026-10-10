package promptstate

import (
	"path/filepath"
	"testing"
)

// The TUI's stacked-diff pref round-trips, survives a sibling write, and never
// touches the web's own record (spec R7: the two frontends are independent).
func TestStackedDiffRoundTripsAndIsIndependentOfWebUI(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	if fs.StackedDiff() {
		t.Fatal("a fresh store must read false")
	}
	if err := fs.SetStackedDiff(true); err != nil {
		t.Fatal(err)
	}
	if !fs.StackedDiff() {
		t.Fatal("set true must read back true")
	}
	if _, ok := fs.WebUIState(); ok {
		t.Fatal("the TUI pref must not create a web_ui record (R7)")
	}
	if err := fs.SuppressPrompt("x"); err != nil { // a sibling write read-merges
		t.Fatal(err)
	}
	if !fs.StackedDiff() {
		t.Fatal("a sibling write dropped the pref")
	}
	if err := fs.SetStackedDiff(false); err != nil {
		t.Fatal(err)
	}
	if fs.StackedDiff() {
		t.Fatal("set false must read back false")
	}
}

// The review split's own stacked pref round-trips and never touches the
// diff view's (spec §4.3: two keys, two postures).
func TestStackedReviewFilesIsItsOwnPref(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	if fs.StackedReviewFiles() {
		t.Fatal("a fresh store must read false")
	}
	if err := fs.SetStackedReviewFiles(true); err != nil {
		t.Fatal(err)
	}
	if !fs.StackedReviewFiles() || fs.StackedDiff() {
		t.Fatalf("review %v diff %v: the review pref must not set the diff's", fs.StackedReviewFiles(), fs.StackedDiff())
	}
	if err := fs.SetStackedDiff(true); err != nil {
		t.Fatal(err)
	}
	if err := fs.SetStackedReviewFiles(false); err != nil {
		t.Fatal(err)
	}
	if fs.StackedReviewFiles() || !fs.StackedDiff() {
		t.Fatal("clearing the review pref must leave the diff's alone")
	}
}
