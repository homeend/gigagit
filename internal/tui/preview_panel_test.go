package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
)

// mergePreviewModel is a loaded model over a repo where feat/x brings a.txt into
// main, with one saved preview. It returns the repo dir and the previews store
// dir (a reRoot builds a fresh Service, which needs pointing back at the store).
func mergePreviewModel(t *testing.T) (Model, string, string) {
	t.Helper()
	dir, repo := newRepoDir(t)
	runGit(t, dir, "checkout", "-q", "-b", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "add a")
	runGit(t, dir, "checkout", "-q", "main")
	previewsDir := t.TempDir()
	svc := domain.New(repo)
	svc.UsePreviewsDir(previewsDir)
	if _, err := svc.PreviewAdd(context.Background(), "feat/x", "main", "login"); err != nil {
		t.Fatal(err)
	}
	m := New(svc)
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)
	// The tab's source rides on its own read (previews are not in Snapshot).
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ = m.Update(cmd())
	return updated.(Model), dir, previewsDir
}

// previewsMsgFrom runs cmd (unwrapping one level of tea.BatchMsg) and returns
// the srcPreviews dataAvailableMsg it produced, if any.
func previewsMsgFrom(t *testing.T, cmd tea.Cmd) (dataAvailableMsg, bool) {
	t.Helper()
	if cmd == nil {
		return dataAvailableMsg{}, false
	}
	var msgs []tea.Msg
	switch v := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range v {
			if c != nil {
				msgs = append(msgs, c())
			}
		}
	default:
		msgs = append(msgs, v)
	}
	for _, msg := range msgs {
		if da, ok := msg.(dataAvailableMsg); ok && da.source == srcPreviews {
			return da, true
		}
	}
	return dataAvailableMsg{}, false
}

func TestPreviewsTabRendersRowsWithSummary(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
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
	m, _, _ := mergePreviewModel(t)
	m = m.activateTab(panelWorktrees)
	updated, _ := m.Update(keyMsg("ctrl+right"))
	if m = updated.(Model); m.activeLeftTab != panelPreviews || m.focus != panelPreviews {
		t.Fatalf("ctrl+→ from Worktrees must land on Previews, got %v", m.activeLeftTab)
	}
	updated, _ = m.Update(keyMsg("ctrl+right"))
	if m = updated.(Model); m.activeLeftTab != panelBranches {
		t.Fatal("ctrl+→ from Previews must wrap to Branches")
	}
	if p, ok := tabSegAt(topTabSegs(panelBranches), lipgloss.Width("[Branches] R W ")); !ok || p != panelPreviews {
		t.Fatalf("clicking the P marker must select Previews, got %v %v", p, ok)
	}
}

func TestPreviewRowStates(t *testing.T) {
	t.Parallel()
	m, dir, _ := mergePreviewModel(t)
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
	m, _, _ := mergePreviewModel(t)
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

// TestReRootPreviewsReadCannotDropLoadingGate is the ordering guard. A previews
// read started by reRoot itself would land BEFORE the snapshot (a state-dir
// read plus a couple of rev-parse calls beat a full Snapshot), and the
// dataAvailableMsg arrival unconditionally sets m.ready = true and recomputes
// m.loading — dropping the blank-screen gate and reopening the !m.loading
// action guards while the OLD repo's data is still in the model. So reRoot
// must start no previews read at all; the dataLoadedMsg success arm chains it.
func TestReRootPreviewsReadCannotDropLoadingGate(t *testing.T) {
	t.Parallel()
	m, dir, previewsDir := mergePreviewModel(t)
	m.width, m.height = 160, 40
	wt := filepath.Join(filepath.Dir(dir), "wt-previews")
	runGit(t, dir, "worktree", "add", "-q", "-b", "feat/wt", wt, "main")

	updated, _ := m.reRoot(wt)
	m = updated.(Model)
	if !m.loading || m.ready {
		t.Fatalf("reRoot must keep the blank-screen gate: loading=%v ready=%v", m.loading, m.ready)
	}
	// reRoot bumps every source generation to drop the OLD repo's in-flight
	// reads (see TestReRootDropsInFlightReads), so the generation itself says
	// nothing here; the in-flight flag does — a read STARTED by reRoot would
	// have set it after the reset.
	if m.srcInflight[srcPreviews] {
		t.Fatal("reRoot started a previews read")
	}
	if len(m.previews) != 0 {
		t.Fatalf("reRoot must drop the old repo's previews, got %d rows", len(m.previews))
	}
	if out := m.View(); !strings.Contains(out, "loading") {
		t.Fatalf("reRoot must still show the loading screen, got:\n%s", out)
	}

	// The new repo's Service is built by reRoot, so point it back at the same
	// store — the chained read must find the record through the NEW service.
	m.svc.UsePreviewsDir(previewsDir)

	// The snapshot lands: only now is the previews read chained.
	updated, cmd := m.Update(m.loadCmd()())
	m = updated.(Model)
	if m.loading || !m.ready {
		t.Fatalf("the snapshot must lift the gate: loading=%v ready=%v", m.loading, m.ready)
	}
	da, ok := previewsMsgFrom(t, cmd)
	if !ok {
		t.Fatal("the dataLoadedMsg success arm must chain a previews read")
	}
	updated, _ = m.Update(da)
	m = updated.(Model)
	m = m.activateTab(panelPreviews)
	if out := m.View(); !strings.Contains(out, "[Previews]") || !strings.Contains(out, "login") {
		t.Fatalf("the chained read must fill the tab:\n%s", out)
	}
}

// TestLeftReturnsToPreviewsWhenRemotesIsStale covers the leftReturnTarget
// widening: a remembered pointer at a now-hidden Remotes tab must redirect to
// the visible tab (Previews), not focus a panel with no box on screen.
func TestLeftReturnsToPreviewsWhenRemotesIsStale(t *testing.T) {
	t.Parallel()
	m := New(nil)
	m.width, m.height = 80, 24
	m.lastLeftPanel = panelRemotes // stale: Remotes is not the active tab
	m.activeLeftTab = panelPreviews
	m.focus = panelCommits
	updated, _ := m.Update(keyMsg("left"))
	if got := updated.(Model).focus; got != panelPreviews {
		t.Fatalf("← with a stale Remotes pointer: focus=%v, want Previews", got)
	}
}
