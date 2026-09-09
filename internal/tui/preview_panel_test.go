package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// mergePreviewModel is a loaded model over a repo where feat/x brings a.txt into
// main, with one saved preview.
func mergePreviewModel(t *testing.T) (Model, string) {
	t.Helper()
	dir, repo := newRepoDir(t)
	runGit(t, dir, "checkout", "-q", "-b", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "add a")
	runGit(t, dir, "checkout", "-q", "main")
	svc := domain.New(repo)
	svc.UsePreviewsDir(t.TempDir())
	if _, err := svc.PreviewAdd(context.Background(), "feat/x", "main", "login"); err != nil {
		t.Fatal(err)
	}
	m := New(svc)
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)
	// The tab's source rides on its own read (previews are not in Snapshot).
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ = m.Update(cmd())
	return updated.(Model), dir
}

func TestPreviewsTabRendersRowsWithSummary(t *testing.T) {
	t.Parallel()
	m, _ := mergePreviewModel(t)
	m = m.activateTab(panelPreviews)
	m.width, m.height = 160, 40
	out := m.View()
	if !strings.Contains(out, "[Previews]") {
		t.Fatalf("tab bar must show the active Previews tab:\n%s", out)
	}
	if !strings.Contains(out, "login") || !strings.Contains(out, "feat/x → main") || !strings.Contains(out, "1 file  ↑1") {
		t.Fatalf("row must show label, pair, files and ahead:\n%s", out)
	}
}

func TestPreviewsTabCyclesAndClicks(t *testing.T) {
	t.Parallel()
	m, _ := mergePreviewModel(t)
	m = m.activateTab(panelWorktrees)
	updated, _ := m.Update(keyMsg("ctrl+right"))
	if m = updated.(Model); m.activeLeftTab != panelPreviews || m.focus != panelPreviews {
		t.Fatalf("ctrl+→ from Worktrees must land on Previews, got %v", m.activeLeftTab)
	}
	updated, _ = m.Update(keyMsg("ctrl+right"))
	if m = updated.(Model); m.activeLeftTab != panelBranches {
		t.Fatal("ctrl+→ from Previews must wrap to Branches")
	}
	if p, ok := tabSegAt(topTabSegs(panelBranches), len("[Branches] R W ")); !ok || p != panelPreviews {
		t.Fatalf("clicking the P marker must select Previews, got %v %v", p, ok)
	}
}

func TestPreviewRowStates(t *testing.T) {
	t.Parallel()
	m, dir := mergePreviewModel(t)
	runGit(t, dir, "merge", "-q", "--no-edit", "feat/x")
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	if rows := m.previewRows(); len(rows) != 1 || !strings.Contains(rows[0], "merged") {
		t.Fatalf("rows = %v, want merged", rows)
	}
	runGit(t, dir, "branch", "-D", "feat/x")
	m, cmd = m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if rows := m.previewRows(); !strings.Contains(rows[0], "missing: feat/x") {
		t.Fatalf("rows = %v, want missing: feat/x", rows)
	}
}

func TestPreviewSelectionKeyIsID(t *testing.T) {
	t.Parallel()
	m, _ := mergePreviewModel(t)
	m = m.activateTab(panelPreviews)
	if key := m.rowKeyAt(panelPreviews, 0); key != m.previews[0].rec.ID {
		t.Fatalf("rowKeyAt = %q, want the record id", key)
	}
}

// TestPreviewsDisabledReadsEmpty pins the disabled posture: a Service with no
// preview store (the TestMain default) yields an empty tab and no status-line
// error, never an "previews: no state directory available" line on every r.
func TestPreviewsDisabledReadsEmpty(t *testing.T) {
	t.Parallel()
	_, repo := newRepoDir(t)
	svc := domain.New(repo) // no UsePreviewsDir: PreviewsDisabled wins
	m := New(svc)
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if len(m.previews) != 0 {
		t.Fatalf("disabled previews must read empty, got %d rows", len(m.previews))
	}
	if m.statusMsg != "" {
		t.Fatalf("disabled previews must not write a status line, got %q", m.statusMsg)
	}
}
