package promptstate

import (
	"path/filepath"
	"testing"
)

func TestBranchFilterSlotRoundTrip(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	if got := fs.BranchFilterSlot("/r/.git", BranchFilterListBranches); got != 0 {
		t.Fatalf("fresh store: %d", got)
	}
	if err := fs.SetBranchFilterSlot("/r/.git", BranchFilterListBranches, 2); err != nil {
		t.Fatal(err)
	}
	if err := fs.SetBranchFilterSlot("/r/.git", BranchFilterListRemotes, 5); err != nil {
		t.Fatal(err)
	}
	if err := fs.SetBranchFilterSlot("/other/.git", BranchFilterListBranches, 1); err != nil {
		t.Fatal(err)
	}
	if got := fs.BranchFilterSlot("/r/.git", BranchFilterListBranches); got != 2 {
		t.Errorf("branches = %d", got)
	}
	if got := fs.BranchFilterSlot("/r/.git", BranchFilterListRemotes); got != 5 {
		t.Errorf("remotes = %d", got)
	}
	if got := fs.BranchFilterSlot("/other/.git", BranchFilterListRemotes); got != 0 {
		t.Errorf("other remotes = %d", got)
	}
	// Clearing writes 0 and survives a reopen; sibling records survive too.
	if err := fs.SuppressPrompt("x"); err != nil {
		t.Fatal(err)
	}
	if err := fs.SetBranchFilterSlot("/r/.git", BranchFilterListBranches, 0); err != nil {
		t.Fatal(err)
	}
	fs2 := NewFileStore(fs.path)
	if got := fs2.BranchFilterSlot("/r/.git", BranchFilterListBranches); got != 0 {
		t.Errorf("cleared slot = %d", got)
	}
	if got := fs2.BranchFilterSlot("/r/.git", BranchFilterListRemotes); got != 5 {
		t.Errorf("remotes after clear = %d", got)
	}
	if !fs2.SuppressedPrompts()["x"] {
		t.Error("sibling record lost")
	}
	if err := fs.SetBranchFilterSlot("/r/.git", "tags", 1); err == nil {
		t.Error("unknown list accepted")
	}
	if err := fs.SetBranchFilterSlot("/r/.git", BranchFilterListBranches, 6); err == nil {
		t.Error("slot 6 accepted")
	}
}
