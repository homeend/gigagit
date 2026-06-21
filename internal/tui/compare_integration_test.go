package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/model"
)

// headHashTUI returns the HEAD commit hash of the repo at dir.
func headHashTUI(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// openCompareDiffOnFile opens a commit↔worktree comparison, drives the file
// list, selects path in the tree, presses enter, and drives the diff load —
// returning the resolved diffView. This is the full real-git path:
// CompareFiles → endpoint→FileRef→ResolveBytes → Differ.
func openCompareDiffOnFile(t *testing.T, repoDir string, repo *domain.Service, head, path string) *diffView {
	t.Helper()
	m := New(repo)
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: head}
	right := model.Endpoint{Kind: model.EndpointWorkTree}

	m, cmd := m.openCompareFiles(left, right)
	if cmd == nil {
		t.Fatal("openCompareFiles returned no load command")
	}
	cm, ok := cmd().(compareFilesMsg)
	if !ok {
		t.Fatalf("expected compareFilesMsg")
	}
	if cm.err != nil {
		t.Fatalf("file-list load err: %v", cm.err)
	}
	mm, _ := m.Update(cm)
	m = mm.(Model)

	// Find the file's row and select it in the tree.
	sel := -1
	for i, l := range m.filesView.lines {
		if l.path == path {
			sel = i
		}
	}
	if sel < 0 {
		t.Fatalf("%s not in the compared file list: %+v", path, m.filesView.lines)
	}
	m.filesView.sel = sel
	m.filesTreeFocused = true

	u, dcmd := m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.diffView == nil || dcmd == nil {
		t.Fatal("enter did not open + load the diff")
	}
	dmsg, ok := dcmd().(diffMsg)
	if !ok {
		t.Fatalf("expected diffMsg")
	}
	if dmsg.view.err != nil {
		t.Fatalf("diff load err: %v", dmsg.view.err)
	}
	return dmsg.view
}

// TestCompareCommitVsWorktreeRealDiff drives the whole headline path against a
// real git repo: a dirtied working tree, the changed-file list, and an actual
// rendered per-file diff with non-empty rows. (The wiring-only tests cannot
// catch a broken ResolveBytes path; loadedModel's repo is clean, so its
// comparison is empty.)
func TestCompareCommitVsWorktreeRealDiff(t *testing.T) {
	dir, repo := newRepoDir(t)
	head := headHashTUI(t, dir)

	// Dirty the working tree.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\nNEW LINE\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The changed-file list must name README.md as modified.
	m := New(domain.New(repo))
	m2, cmd := m.openCompareFiles(
		model.Endpoint{Kind: model.EndpointCommit, Hash: head},
		model.Endpoint{Kind: model.EndpointWorkTree})
	cm := cmd().(compareFilesMsg)
	if cm.err != nil {
		t.Fatal(cm.err)
	}
	m3, _ := m2.Update(cm)
	found := false
	for _, l := range m3.(Model).filesView.lines {
		if l.path == "README.md" && l.status == "M" {
			found = true
		}
	}
	if !found {
		t.Fatalf("README.md M missing from %+v", m3.(Model).filesView.lines)
	}

	// The per-file diff must have real, non-empty rows.
	v := openCompareDiffOnFile(t, dir, domain.New(repo), head, "README.md")
	if len(v.full) == 0 {
		t.Fatal("compare diff has no rows — endpoint bytes were not resolved")
	}
}

// TestCompareDiffBypassesCacheForWorktree proves a re-opened commit↔worktree
// diff reflects the latest on-disk bytes (never a stale cached diff): after a
// second, larger edit the same (commit, worktree, path) diff has strictly more
// rows. A cached diff would serve the first result.
func TestCompareDiffBypassesCacheForWorktree(t *testing.T) {
	dir, repo := newRepoDir(t)
	head := headHashTUI(t, dir)
	svc := domain.New(repo)

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\nNEW1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v1 := openCompareDiffOnFile(t, dir, svc, head, "README.md")
	rows1 := len(v1.full)

	// A larger change to the SAME path — same diff key — must recompute.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\nNEW1\nNEW2\nNEW3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v2 := openCompareDiffOnFile(t, dir, svc, head, "README.md")
	rows2 := len(v2.full)

	if rows2 <= rows1 {
		t.Fatalf("re-opened worktree diff did not reflect new bytes (rows %d → %d) — cache was not bypassed", rows1, rows2)
	}
}
