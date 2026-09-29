package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// remoteRecycleModel: the Remotes tab over recycleModel's branches and
// worktrees. origin/foo has no local branch, origin/feature's local is in a
// linked worktree, origin/loose's local is checked out nowhere.
func remoteRecycleModel(sel int) Model {
	m := recycleModel()
	m.focus = panelRemotes
	m.remoteBranches = []model.RemoteBranch{
		{Name: "origin/foo", Remote: "origin", Branch: "foo"},
		{Name: "origin/feature", Remote: "origin", Branch: "feature"},
		{Name: "origin/loose", Remote: "origin", Branch: "loose"},
	}
	m.sel[panelRemotes] = sel
	return m
}

func TestRemoteRecycleRowGating(t *testing.T) {
	t.Parallel()
	for sel, want := range []bool{true, false, true} {
		m := remoteRecycleModel(sel)
		if got := ids(availableActions(m))["recycle-worktree"]; got != want {
			t.Errorf("%s: recycle-worktree offered = %v, want %v", m.remoteBranches[sel].Name, got, want)
		}
	}
	m := remoteRecycleModel(0)
	m.worktrees = []model.Worktree{{Path: "/repo", Branch: "main"}}
	if ids(availableActions(m))["recycle-worktree"] {
		t.Error("no candidate worktree → no row")
	}
}

func TestRemoteRecyclePickerStartsTheOpWithRemoteRef(t *testing.T) {
	t.Parallel()
	m := remoteRecycleModel(0)
	row, ok := rowByID(availableActions(m), "recycle-worktree")
	if !ok {
		t.Fatal("row not offered")
	}
	nm, _ := row.run(m)
	m = nm.(Model)
	if m.recycleBranch != "foo" || m.recycleRemote != "origin/foo" {
		t.Fatalf("captured branch=%q remote=%q, want foo / origin/foo", m.recycleBranch, m.recycleRemote)
	}
	pick, ok := rowByID(m.actionMenu.rows, "recycle-into:/repo-wt/other")
	if !ok {
		t.Fatal("picker row missing")
	}
	nm, cmd := pick.run(m)
	m = nm.(Model)
	if cmd == nil || !m.running || m.opName != engine.OpName(engine.RecycleWorktree{}) {
		t.Fatalf("running=%v opName=%q, want a running RecycleWorktree", m.running, m.opName)
	}
	if op := recycleOpFor("/repo-wt/other", m.recycleBranch, m.recycleRemote); op.RemoteRef != "origin/foo" || op.Branch != "foo" {
		t.Fatalf("recycleOpFor = %+v", op)
	}
	if pc := m.pendingCheckout; pc.remoteRef != "origin/foo" || pc.base != "foo" || pc.recycleDir != "/repo-wt/other" {
		t.Fatalf("pendingCheckout = %+v; the diverged recovery must be armed for the recycle", pc)
	}
}

// A local branch picked on the Branches tab carries no remote, whatever the
// Remotes tab captured before.
func TestLocalRecycleClearsCapturedRemote(t *testing.T) {
	t.Parallel()
	m := recycleModel()
	m.recycleRemote = "origin/stale"
	row, _ := rowByID(availableActions(m), "recycle-worktree")
	nm, _ := row.run(m)
	if got := nm.(Model).recycleRemote; got != "" {
		t.Fatalf("recycleRemote = %q, want empty for a local branch", got)
	}
}

func TestRemoteRecycleDivergedRenameRecycles(t *testing.T) {
	t.Parallel()
	m := remoteRecycleModel(0)
	m.branches = append(m.branches, model.Branch{Name: "foo"})
	m.pendingCheckout = pendingCheckout{remoteRef: "origin/foo", base: "foo", intent: engine.CheckoutStay, recycleDir: "/repo-wt/other"}
	nm, _ := m.Update(opFinishedMsg{err: engine.CheckoutDivergedError{Local: "foo", RemoteRef: "origin/foo"}})
	rm := nm.(Model)
	if rm.modal == nil || rm.modal.req.ID != "checkout-diverged" {
		t.Fatalf("expected checkout-diverged modal; modal=%+v", rm.modal)
	}
	nm, _ = rm.resolveModal("check out as different name…")
	rm = nm.(Model)
	p, ok := rm.topLayer().(*checkoutAsPopup)
	if !ok {
		t.Fatalf("expected checkoutAsPopup; got %T", rm.topLayer())
	}
	if p.recycleDir != "/repo-wt/other" {
		t.Fatalf("popup recycleDir = %q; the rename must keep recycling", p.recycleDir)
	}
	if view := p.render(rm, ""); !strings.Contains(view, "recycle") {
		t.Fatalf("popup must say enter recycles:\n%s", view)
	}
	rm2, cmd := p.update(rm, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !rm2.running || rm2.opName != engine.OpName(engine.RecycleWorktree{}) {
		t.Fatalf("running=%v opName=%q, want the recycle re-dispatched", rm2.running, rm2.opName)
	}
	if pc := rm2.pendingCheckout; pc.base != "foo-2" || pc.recycleDir != "/repo-wt/other" {
		t.Fatalf("pendingCheckout = %+v; a re-collision must recover the recycle again", pc)
	}
}
