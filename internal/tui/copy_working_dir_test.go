package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// On a focused stash file tree (filesHash holds the stash's resolved SHA), the
// "Copy to working dir" row is offered.
func TestCopyToWorkingDirRowPresentOnStashFile(t *testing.T) {
	m := footerModel()
	m.filesView = &contentPopup{lines: []contentLine{{text: "dir/f.go", path: "dir/f.go"}}}
	m.filesTreeFocused = true
	m.filesHash = "0123456789abcdef0123456789abcdef01234567" // stash's resolved commit SHA
	m.filesMode = filesModeStash
	m.filesStashTag = "stash@{0}"
	if _, ok := findRow(availableActions(m), "copy-working-dir"); !ok {
		t.Fatal("Copy to working dir missing on a stash file")
	}
}

// A plain working-tree (unstaged) file is already local — the row is absent.
func TestCopyToWorkingDirRowAbsentOnWorkingFile(t *testing.T) {
	m := filesMenuModel() // panelFiles focused, one tracked file (Source = Unstaged)
	m.currentWorktree = "/wt"
	if _, ok := findRow(availableActions(m), "copy-working-dir"); ok {
		t.Fatal("Copy to working dir must be absent for a working-tree file")
	}
}

// A file deleted in the commit/stash has no content to copy — the row is absent.
func TestCopyToWorkingDirRowAbsentOnDeletion(t *testing.T) {
	m := footerModel()
	m.filesView = &contentPopup{lines: []contentLine{{text: "dir/f.go", path: "dir/f.go", status: "D"}}}
	m.filesTreeFocused = true
	m.filesHash = "0123456789abcdef0123456789abcdef01234567"
	if _, ok := findRow(availableActions(m), "copy-working-dir"); ok {
		t.Fatal("Copy to working dir must be absent for a deletion")
	}
}

// End to end: a file that exists at an old commit but not in the working tree is
// written back. (A stash file takes the identical SourceCommit resolve path; an
// absent destination keeps the test free of the Overwrite modal, which is
// covered by engine.WriteFile's own tests.)
func TestCopyToWorkingDirWritesFile(t *testing.T) {
	dir, repo := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add a")
	sha := gitOut(t, dir, "rev-parse", "HEAD") // commit where a.txt = "v1"
	gitRun(t, dir, "rm", "a.txt")
	gitRun(t, dir, "commit", "-m", "drop a") // a.txt now absent from the working tree

	// Load first (matches the working real-op precedent
	// TestRunCommitOperationFinishesAndClearsRunning, so Execute/repogate has the
	// gitCommonDir/toplevel it needs). The dataLoadedMsg handler does not touch
	// filesView/filesHash, so the tree setup below survives the load.
	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.filesView = &contentPopup{lines: []contentLine{{text: "a.txt", path: "a.txt"}}}
	m.filesTreeFocused = true
	m.filesHash = sha

	row, ok := findRow(availableActions(m), "copy-working-dir")
	if !ok {
		t.Fatal("Copy to working dir row missing")
	}
	tm, cmd := row.run(m)
	m = driveOp(t, tm.(Model), cmd)

	got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatalf("a.txt not written: %v", err)
	}
	if string(got) != "v1\n" {
		t.Fatalf("a.txt = %q, want %q", got, "v1\n")
	}
}

// A resolve failure (bogus commit) surfaces as statusMsg, synchronously, with no
// op dispatched and no panic.
func TestCopyToWorkingDirResolveErrorSetsStatus(t *testing.T) {
	_, repo := newRepoDir(t)
	m := New(domain.New(repo))
	m.filesView = &contentPopup{lines: []contentLine{{text: "nope.txt", path: "nope.txt"}}}
	m.filesTreeFocused = true
	m.filesHash = "0123456789abcdef0123456789abcdef01234567" // nonexistent commit

	row, ok := findRow(availableActions(m), "copy-working-dir")
	if !ok {
		t.Fatal("Copy to working dir row missing")
	}
	tm, _ := row.run(m)
	mm := tm.(Model)
	if mm.running {
		t.Fatal("a failed resolve must not start an op")
	}
	if !strings.Contains(mm.statusMsg, "copy to working dir") {
		t.Fatalf("statusMsg = %q, want a copy-to-working-dir error", mm.statusMsg)
	}
}
