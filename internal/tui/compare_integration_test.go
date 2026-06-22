package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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

// TestCompareUntrackedFileDiffRenders proves an UNTRACKED file (which `git diff`
// omits, and which CompareFiles now adds as an "A" row) both appears in the
// compared list AND resolves to a real diff when opened — the worktree-new-side /
// nil-old-side path.
func TestCompareUntrackedFileDiffRenders(t *testing.T) {
	dir, repo := newRepoDir(t)
	head := headHashTUI(t, dir)

	untracked := "timing — kopia.log" // space + em-dash, like the reported file
	if err := os.WriteFile(filepath.Join(dir, untracked), []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	v := openCompareDiffOnFile(t, dir, domain.New(repo), head, untracked)
	if len(v.full) == 0 {
		t.Fatal("untracked file compare diff has no rows — its bytes were not resolved")
	}
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

// TestCompareDiffRendersContentNotLoading drives the FULL compare path through
// Update (file list → enter → the diffMsg routed through the handler) and
// renders the screen, proving the body shows real diff content rather than
// being stuck on "(loading…)". openCompareDiffOnFile reads the diffMsg cmd
// directly and so cannot catch a handler that fails to clear loading; this
// renders the assembled view, which is where the original bug was visible.
func TestCompareDiffRendersContentNotLoading(t *testing.T) {
	dir, repo := newRepoDir(t)
	head := headHashTUI(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\nNEW LINE\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New(domain.New(repo))
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = mm.(Model)
	m.loading = false // past the full-screen startup snapshot overlay

	m, lcmd := m.openCompareFiles(
		model.Endpoint{Kind: model.EndpointCommit, Hash: head},
		model.Endpoint{Kind: model.EndpointWorkTree})
	if lcmd == nil {
		t.Fatal("openCompareFiles returned no load command")
	}
	// Load the changed-file list.
	u, _ := m.Update(lcmd())
	m = u.(Model)

	sel := -1
	for i, l := range m.filesView.lines {
		if l.path == "README.md" {
			sel = i
		}
	}
	if sel < 0 {
		t.Fatalf("README.md not in compared list: %+v", m.filesView.lines)
	}
	m.filesView.sel = sel
	m.filesTreeFocused = true

	// enter opens the loading view and returns the load command.
	u, dcmd := m.Update(keyMsg("enter"))
	m = u.(Model)
	if dcmd == nil {
		t.Fatal("enter returned no diff load command")
	}
	// Route the completed diffMsg through the handler, as the runtime would.
	u, _ = m.Update(dcmd())
	m = u.(Model)

	if m.diffView.loading {
		t.Error("diffView still loading after the diffMsg landed")
	}
	out := ansi.Strip(m.View())
	if strings.Contains(out, "(loading…)") {
		t.Errorf("rendered compare diff still shows \"(loading…)\":\n%s", out)
	}
	if !strings.Contains(out, "NEW LINE") {
		t.Errorf("rendered compare diff missing the changed content:\n%s", out)
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
