package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// viewedOther puts the model on a second worktree's slot with its status
// landed, the kick consumed.
func viewedOther(t *testing.T, m Model) (Model, string) {
	t.Helper()
	m, other := addWorktree(t, m, "wt2")
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("switchView(%s) refused: %s", other, m.statusMsg)
	}
	m = landView(t, m)
	m.viewKick = false
	return m, other
}

func dropWorktreeFromList(m Model, path string) Model {
	var kept []model.Worktree
	for _, w := range m.worktrees {
		if filepath.Clean(w.Path) != filepath.Clean(path) {
			kept = append(kept, w)
		}
	}
	m.worktrees = kept
	return m
}

// --- 6: the drop paths sleep the gone slot like switchView does ---

func TestPruneViewsSleepsTheGoneSlot(t *testing.T) {
	m := loadedModel(t)
	m, other := viewedOther(t, m)
	wg, lg, dg, rg := m.watchGen, m.loadGen, m.docWatch.gen, m.workingReviewsGen
	m = dropWorktreeFromList(m, other)
	m = m.pruneViews()
	if m.viewed != m.home || !m.viewKick {
		t.Fatalf("viewed=%q kick=%v", m.viewed, m.viewKick)
	}
	if m.watchGen == wg || m.loadGen == lg || m.docWatch.gen == dg || m.workingReviewsGen == rg {
		t.Fatalf("gone slot not put to sleep: watch %d→%d load %d→%d doc %d→%d reviews %d→%d", wg, m.watchGen, lg, m.loadGen, dg, m.docWatch.gen, rg, m.workingReviewsGen)
	}
}

func TestAbandonGoneViewSleepsTheGoneSlot(t *testing.T) {
	m := loadedModel(t)
	m, other := viewedOther(t, m)
	wg, lg, dg, rg := m.watchGen, m.loadGen, m.docWatch.gen, m.workingReviewsGen
	if err := os.RemoveAll(other); err != nil {
		t.Fatal(err)
	}
	m, ok := m.abandonGoneView()
	if !ok || m.viewed != m.home {
		t.Fatalf("ok=%v viewed=%q", ok, m.viewed)
	}
	if m.watchGen == wg || m.loadGen == lg || m.docWatch.gen == dg || m.workingReviewsGen == rg {
		t.Fatalf("gone slot not put to sleep: watch %d→%d load %d→%d doc %d→%d reviews %d→%d", wg, m.watchGen, lg, m.loadGen, dg, m.docWatch.gen, rg, m.workingReviewsGen)
	}
}

// --- 5: a queued return drains after a staging round, not only after an op ---

func TestQueuedReturnDrainsAfterAStagingRound(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.showConsole(id, false)
	m.running = true // a stage in flight
	m = m.closeConsole()
	if m.viewed != filepath.Clean(other) || m.pendingReturnView != m.home {
		t.Fatalf("viewed=%q pending=%q", m.viewed, m.pendingReturnView)
	}
	nm, _ := m.Update(statusRefreshedMsg{status: m.status})
	m = nm.(Model)
	if m.viewed != m.home || m.pendingReturnView != "" {
		t.Fatalf("after the staging round: viewed=%q pending=%q", m.viewed, m.pendingReturnView)
	}
}

// --- 2: no swap under a popup, a decision or a process; the return queues ---

func TestSwitchViewRefusesUnderADecision(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m.modal = &decisionState{}
	if nm, ok := m.switchView(other); ok || nm.viewed != m.home {
		t.Fatalf("switched under a decision: ok=%v viewed=%q", ok, nm.viewed)
	}
}

func TestConsoleCloseUnderADecisionQueuesTheReturn(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.showConsole(id, false)
	m.modal = &decisionState{} // a commit question over the console's worktree
	m = m.closeConsole()
	if m.viewed != filepath.Clean(other) || m.pendingReturnView != m.home {
		t.Fatalf("swapped under the decision: viewed=%q pending=%q", m.viewed, m.pendingReturnView)
	}
	m.modal = nil
	nm, _ := m.Update(struct{}{}) // any message once the surface is clear
	m = nm.(Model)
	if m.viewed != m.home || m.pendingReturnView != "" {
		t.Fatalf("after the decision: viewed=%q pending=%q", m.viewed, m.pendingReturnView)
	}
}

func TestPruneViewsHoldsUnderADecision(t *testing.T) {
	m := loadedModel(t)
	m, other := viewedOther(t, m)
	m.modal = &decisionState{}
	m = dropWorktreeFromList(m, other)
	m = m.pruneViews()
	if m.viewed != filepath.Clean(other) || m.pendingReturnView != m.home || m.views[filepath.Clean(other)] == nil {
		t.Fatalf("swapped under the decision: viewed=%q pending=%q slot kept=%v", m.viewed, m.pendingReturnView, m.views[filepath.Clean(other)] != nil)
	}
}

// --- 3: a result computed through another slot's service is dropped ---

func TestStatusFromAnotherSlotIsDropped(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	v := m.ensureView(other)
	was := m.status.Branch
	nm, _ := m.Update(statusRefreshedMsg{svc: v.svc, status: model.WorkingTreeStatus{Branch: "ghost"}})
	m = nm.(Model)
	if m.status.Branch != was {
		t.Fatalf("another slot's status landed: branch %q, want %q", m.status.Branch, was)
	}
}

func TestHunkPickerFromAnotherSlotIsDropped(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	v := m.ensureView(other)
	nm, _ := m.Update(stageHunksLoadedMsg{svc: v.svc, path: "f", index: []byte("a\n"), work: []byte("b\n")})
	m = nm.(Model)
	if m.topLayer() != nil {
		t.Fatalf("a picker opened for another slot's hunks: %T", m.topLayer())
	}
}

// --- 1: working-tree windows of the leaving worktree close on a swap ---

func TestSwitchViewDropsWorkingTreeLayers(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m = m.pushLayer(&diffView{title: "f", rev: ""})     // HEAD → working tree
	m = m.pushLayer(&diffView{title: "g", rev: "abc1"}) // a commit's diff: shared
	m = m.pushLayer(&fileViewer{openFile: &openFile{}})
	m.filesView = &contentPopup{}
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if n := len(m.layers.entries); n != 1 {
		t.Fatalf("%d layers survive, want only the commit diff", n)
	}
	if d, _ := m.topLayer().(*diffView); d == nil || d.rev != "abc1" {
		t.Fatalf("surviving layer = %T %+v, want the commit diff", m.topLayer(), m.topLayer())
	}
	if m.filesView != nil {
		t.Fatal("the F window survived the swap")
	}
}

func TestConsoleCloseDropsParkedWorkingLayersOverAnotherWorktree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m = m.pushLayer(&diffView{title: "f", rev: ""}) // home's working diff
	m, _ = m.showConsole(id, false)                 // parks it, shows other
	m.console.ret.view = filepath.Clean(other)      // alt+w's first hit: the panels stay on other
	m = m.closeConsole()
	if m.viewed != filepath.Clean(other) {
		t.Fatalf("viewed=%q", m.viewed)
	}
	if m.topLayer() != nil {
		t.Fatalf("home's working diff restored over %s: %T", other, m.topLayer())
	}
}

// --- 4: steering compares with the worktree on SCREEN ---

func TestSteerFileCommandsCompareWithTheViewedWorktree(t *testing.T) {
	m := loadedModel(t)
	m, other := viewedOther(t, m)
	if why, mismatch := m.steerShownMismatch(steer.Command{Worktree: m.home}); !mismatch || !strings.Contains(why, filepath.Clean(other)) {
		t.Fatalf("home's agent while %s is shown: mismatch=%v why=%q", other, mismatch, why)
	}
	if why, mismatch := m.steerShownMismatch(steer.Command{Worktree: other}); mismatch {
		t.Fatalf("the shown worktree's agent refused: %q", why)
	}
}
