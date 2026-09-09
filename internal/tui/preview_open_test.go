package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// drainMsgs feeds up to n command results (expanding tea.BatchMsg) into m.
// Shared by the preview tests in Tasks 7–9.
func drainMsgs(t *testing.T, m Model, cmd tea.Cmd, n int) Model {
	t.Helper()
	for i := 0; i < n && cmd != nil; i++ {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			var rest []tea.Cmd
			for _, c := range batch {
				if c == nil {
					continue
				}
				updated, next := m.Update(c())
				m = updated.(Model)
				if next != nil {
					rest = append(rest, next)
				}
			}
			cmd = nil
			if len(rest) > 0 {
				cmd = tea.Batch(rest...)
			}
			continue
		}
		updated, next := m.Update(msg)
		m = updated.(Model)
		cmd = next
	}
	return m
}

// openMergePreview presses enter on the first Previews row and drains the open.
// (openPreview is taken by the files-view content preview helper.)
func openMergePreview(t *testing.T, m Model) Model {
	t.Helper()
	m = m.activateTab(panelPreviews)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("enter must start the open")
	}
	updated, cmd = m.Update(cmd()) // previewOpenMsg
	m = updated.(Model)
	if cmd != nil { // compareFilesMsg
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}
	return m
}

func TestEnterOpensPreviewInCompareMode(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m = openMergePreview(t, m)
	if m.filesView == nil || !m.inCompareMode() || m.previewOpen == nil {
		t.Fatal("enter must open the compare files view in preview mode")
	}
	if m.filesTitle != "Merge preview: feat/x → main" || m.comparePair != nil {
		t.Fatalf("title = %q, comparePair = %v (f must be inert)", m.filesTitle, m.comparePair)
	}
	var paths []string
	for _, l := range m.filesView.lines {
		if l.path != "" {
			paths = append(paths, l.path)
		}
	}
	if len(paths) != 1 || paths[0] != "a.txt" {
		t.Fatalf("files = %v, want only a.txt", paths)
	}
	// f is inert in preview mode.
	updated, _ := m.Update(keyMsg("f"))
	if updated.(Model).filesTitle != m.filesTitle {
		t.Fatal("f must not change a preview")
	}
}

// TestPreviewEscClosesAndReturnsFocus pins the routing: the files view owns the
// keyboard while it is open (updateFilesViewKey is not focus-gated), so esc
// closes the preview and hands focus back to the Previews tab it was opened
// from — the user is never left steering a panel hidden behind the view.
func TestPreviewEscClosesAndReturnsFocus(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m = openMergePreview(t, m)
	if m.filesReturnFocus != panelPreviews || !m.filesTreeFocused {
		t.Fatalf("open must remember Previews and land in the tree: return=%v tree=%v",
			m.filesReturnFocus, m.filesTreeFocused)
	}
	updated, _ := m.Update(keyMsg("esc"))
	m = updated.(Model)
	if m.filesView != nil || m.previewOpen != nil || m.focus != panelPreviews {
		t.Fatalf("esc must close the preview and return focus: view=%v open=%v focus=%v",
			m.filesView != nil, m.previewOpen != nil, m.focus)
	}
}

func TestEnterOnMergedRowShowsNotice(t *testing.T) {
	t.Parallel()
	m, dir, _ := mergePreviewModel(t)
	runGit(t, dir, "merge", "-q", "--no-edit", "feat/x")
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{})
	updated, _ := m.Update(cmd())
	m = updated.(Model).activateTab(panelPreviews)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd != nil {
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}
	if m.filesView != nil || !strings.Contains(m.statusMsg, "merged") {
		t.Fatalf("a merged row must not open; status = %q", m.statusMsg)
	}
}

func TestOpenPreviewReArmsWhenSourceMoves(t *testing.T) {
	t.Parallel()
	m, dir, _ := mergePreviewModel(t)
	m = openMergePreview(t, m)
	oldTag := m.compareTag
	// 0b.txt sorts BEFORE a.txt, so the cursor only stays on a.txt if the
	// re-arm carried its path across the reload (keepPath).
	runGit(t, dir, "checkout", "-q", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "0b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "add b")
	runGit(t, dir, "checkout", "-q", "main")
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcBranches}, reloadOpts{})
	updated, chain := m.Update(cmd())
	m = updated.(Model)
	if chain == nil {
		t.Fatal("a branches refresh must chain a previews read")
	}
	// Drain: previews msg → re-arm open cmd → previewOpenMsg → compareFilesMsg.
	m = drainMsgs(t, m, chain, 6)
	if m.compareTag == oldTag {
		t.Fatal("the open preview must re-open with the new tips")
	}
	var paths []string
	for _, l := range m.filesView.lines {
		if l.path != "" {
			paths = append(paths, l.path)
		}
	}
	if len(paths) != 2 || !strings.Contains(m.statusMsg, "feat/x") {
		t.Fatalf("files = %v status = %q; want 0b.txt a.txt and a 'moved' notice", paths, m.statusMsg)
	}
	if got := m.previewSelectedPath(); got != "a.txt" {
		t.Fatalf("cursor = %q after the re-arm, want the file it was on (a.txt)", got)
	}
}

// TestOpenPreviewTargetOnlyMoveReconcilesSilently: commits on the target that
// are off the fork point move neither merge-base nor the source tip, so the
// target…source diff — everything the user sees — is unchanged. The open
// state's hashes must still be reconciled and nothing announced; otherwise
// every later previews refresh sees them differ, says "main moved" and spends
// another PreviewOpen, forever.
func TestOpenPreviewTargetOnlyMoveReconcilesSilently(t *testing.T) {
	t.Parallel()
	m, dir, _ := mergePreviewModel(t)
	m = openMergePreview(t, m)
	oldTag, oldTgt := m.compareTag, m.previewOpen.tgtHash
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "unrelated work on main")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "main").Output()
	if err != nil {
		t.Fatal(err)
	}
	newTip := strings.TrimSpace(string(out))

	m, cmd := m.reloadSourcesCmd([]sourceKey{srcBranches}, reloadOpts{})
	updated, chain := m.Update(cmd())
	m = updated.(Model)
	if chain == nil {
		t.Fatal("a branches refresh must chain a previews read")
	}
	m = drainMsgs(t, m, chain, 6)

	var paths []string
	for _, l := range m.filesView.lines {
		if l.path != "" {
			paths = append(paths, l.path)
		}
	}
	if len(paths) != 1 || paths[0] != "a.txt" || m.compareTag != oldTag {
		t.Fatalf("the diff must not change: files = %v, tag changed = %v", paths, m.compareTag != oldTag)
	}
	if m.statusMsg != "" {
		t.Fatalf("nothing the user sees changed; status = %q", m.statusMsg)
	}
	if m.previewOpen.tgtHash == oldTgt || m.previewOpen.tgtHash != newTip {
		t.Fatalf("target hash = %q, want the new main tip %q", m.previewOpen.tgtHash, newTip)
	}

	// The next refresh must be a no-op: no notice, no further resolve.
	m, cmd = m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{})
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.statusMsg != "" {
		t.Fatalf("a reconciled preview must stay quiet; status = %q", m.statusMsg)
	}
	if _, again := m.afterPreviewsRefresh(); again != nil {
		t.Fatal("a reconciled preview must not re-resolve on every refresh")
	}
}

func TestOpenPreviewClosesWhenSourceDeleted(t *testing.T) {
	t.Parallel()
	m, dir, _ := mergePreviewModel(t)
	m = openMergePreview(t, m)
	runGit(t, dir, "branch", "-D", "feat/x")
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{})
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	if m.filesView != nil || m.previewOpen != nil || !strings.Contains(m.statusMsg, "missing") {
		t.Fatalf("view must close with a missing notice; status = %q", m.statusMsg)
	}
}

// TestPreviewOpenMsgIsGenGated: a resolve that lands after the user closed the
// view (esc) must be dropped, not re-open the compare view behind their back.
func TestPreviewOpenMsgIsGenGated(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m = m.activateTab(panelPreviews)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("enter must start the open")
	}
	msg := cmd() // the resolve landed, but the user moved on first:
	m = m.closeFilesView()
	updated, _ = m.Update(msg)
	if mm := updated.(Model); mm.filesView != nil || mm.previewOpen != nil {
		t.Fatal("a resolve from before the close must not open the view")
	}
}

// TestReRootDropsInFlightReads pins the repo-switch ruling: a read chained off
// repo B's snapshot must not write B's rows (or flip the ready/loading gate)
// into repo C's model. reRoot bumps every source generation, so an in-flight
// read from the old repo is dropped by the arrival handler's gen check.
func TestReRootDropsInFlightReads(t *testing.T) {
	t.Parallel()
	m, dir, _ := mergePreviewModel(t)
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{})
	da, ok := previewsMsgFrom(t, cmd) // the read runs against the OLD repo
	if !ok {
		t.Fatal("want a previews dataAvailableMsg")
	}
	wt := filepath.Join(filepath.Dir(dir), "wt-drop")
	runGit(t, dir, "worktree", "add", "-q", "-b", "feat/drop", wt, "main")
	updated, _ := m.reRoot(wt)
	m = updated.(Model)
	t.Cleanup(func() {
		if m.watcher != nil {
			_ = m.watcher.Close()
		}
	})
	updated, _ = m.Update(da)
	m = updated.(Model)
	if len(m.previews) != 0 {
		t.Fatalf("the old repo's read wrote %d rows into the new model", len(m.previews))
	}
	if !m.loading || m.ready {
		t.Fatalf("a stale read must not lift the blank-screen gate: loading=%v ready=%v", m.loading, m.ready)
	}
}

// TestManualRefreshSurvivesChainedPreviewsRead: on a manual r the chained
// previews read supersedes the manual one still in flight. It must inherit the
// manual flag — otherwise the stale message early-returns on the gen check
// before clearing srcLoading and m.loading sticks true, deadlocking every
// action guard.
func TestManualRefreshSurvivesChainedPreviewsRead(t *testing.T) {
	t.Parallel()
	m, _, _ := mergePreviewModel(t)
	m, cmd := m.reloadAllCmd(reloadOpts{manual: true})
	var msgs []tea.Msg
	for _, c := range batchCmds(cmd) {
		msgs = append(msgs, c())
	}
	// Branches first: its arrival chains the superseding previews read.
	var chain tea.Cmd
	var rest []tea.Msg
	for _, msg := range msgs {
		if da, ok := msg.(dataAvailableMsg); ok && da.source == srcBranches {
			updated, c := m.Update(msg)
			m = updated.(Model)
			chain = c
			continue
		}
		rest = append(rest, msg)
	}
	if chain == nil {
		t.Fatal("a branches refresh must chain a previews read")
	}
	var extra []tea.Cmd        // the other arms' own follow-ups (remotes chains one too)
	for _, msg := range rest { // includes the now-stale manual previews read
		updated, c := m.Update(msg)
		m = updated.(Model)
		if c != nil {
			extra = append(extra, c)
		}
	}
	m = drainMsgs(t, m, chain, 4)
	for _, c := range extra {
		m = drainMsgs(t, m, c, 4)
	}
	if m.loading {
		t.Fatalf("the chained read must clear the manual loading flag: srcLoading = %v", m.srcLoading)
	}
}
