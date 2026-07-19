package tui

import (
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// setGuardSeams fabricates stat/GOOS results for guardedReRoot: only the
// listed paths "exist". Restored on cleanup. The repairable verdict cannot
// be produced with a real stat on a Linux CI box (it needs a foreign-
// notation path that exists), hence the seam.
func setGuardSeams(t *testing.T, goos string, existing ...string) {
	t.Helper()
	set := map[string]bool{}
	for _, p := range existing {
		set[p] = true
	}
	oldStat, oldGOOS := guardStat, guardGOOS
	guardStat = func(p string) error {
		if set[p] {
			return nil
		}
		return errors.New("stat: missing")
	}
	guardGOOS = goos
	t.Cleanup(func() { guardStat, guardGOOS = oldStat, oldGOOS })
}

func TestGuardedReRootReachableSwitches(t *testing.T) {
	m := newTestModel(t)
	setGuardSeams(t, "linux", "/ok/path")
	u, cmd := m.guardedReRoot("/ok/path", false)
	got := u.(Model)
	if got.switchTarget != "/ok/path" || !got.loading {
		t.Fatalf("reachable target must reRoot: switchTarget=%q loading=%v", got.switchTarget, got.loading)
	}
	if cmd == nil {
		t.Fatal("reRoot must return its reload command")
	}
}

func TestGuardedReRootUnreachableRefuses(t *testing.T) {
	m := newTestModel(t)
	m.loading = false         // newTestModel starts in the app-bootstrap loading state; isolate the refusal path
	setGuardSeams(t, "linux") // nothing exists
	u, cmd := m.guardedReRoot("/gone", true)
	got := u.(Model)
	if got.loading || got.switchTarget != "" {
		t.Fatal("unreachable target must not start a switch")
	}
	if got.modal != nil {
		t.Fatal("unreachable (untranslatable) target must not offer repair")
	}
	if want := i18n.T("cannot switch: %s is not reachable from here", "/gone"); got.statusMsg != want {
		t.Fatalf("statusMsg = %q, want %q", got.statusMsg, want)
	}
	if cmd != nil {
		t.Fatal("refusal returns no command")
	}
}

func TestGuardedReRootRepairableWithoutOfferRefuses(t *testing.T) {
	m := newTestModel(t)
	m.loading = false // newTestModel starts in the app-bootstrap loading state; isolate the refusal path
	setGuardSeams(t, "windows", `T:\x`)
	u, _ := m.guardedReRoot("/mnt/t/x", false)
	got := u.(Model)
	if got.modal != nil || got.loading {
		t.Fatal("repairable without offerRepair must plain-refuse")
	}
	if want := i18n.T("cannot switch: %s is not reachable from here", "/mnt/t/x"); got.statusMsg != want {
		t.Fatalf("statusMsg = %q, want %q", got.statusMsg, want)
	}
}

func TestGuardedReRootRepairableOffersModal(t *testing.T) {
	m := newTestModel(t)
	m.loading = false // newTestModel starts in the app-bootstrap loading state; isolate the "offer itself must not switch" check
	setGuardSeams(t, "windows", `T:\x`)
	u, cmd := m.guardedReRoot("/mnt/t/x", true)
	got := u.(Model)
	if cmd != nil || got.loading {
		t.Fatal("the offer itself must not switch")
	}
	if got.modal == nil {
		t.Fatal("repairable + offerRepair must push the modal")
	}
	if got.modal.req.ID != "worktree-cross-env-repair" {
		t.Fatalf("modal ID = %q", got.modal.req.ID)
	}
	wantOpts := []string{"repair", "cancel"}
	if len(got.modal.req.Options) != 2 || got.modal.req.Options[0] != wantOpts[0] || got.modal.req.Options[1] != wantOpts[1] {
		t.Fatalf("options = %v, want %v (cancel LAST: esc maps to the last option)", got.modal.req.Options, wantOpts)
	}

	// cancel: stays put, nothing pending, no op.
	u2, cmd2 := got.modal.onResolve(got, "cancel")
	c := u2.(Model)
	if cmd2 != nil || c.running || c.pendingRepairSwitch != "" {
		t.Fatal("cancel must not dispatch anything")
	}

	// repair: arms the chain with the TRANSLATED path and starts the op.
	u3, cmd3 := got.modal.onResolve(got, "repair")
	r := u3.(Model)
	if r.pendingRepairSwitch != `T:\x` {
		t.Fatalf("pendingRepairSwitch = %q, want %q", r.pendingRepairSwitch, `T:\x`)
	}
	if !r.running {
		t.Fatal("repair must start the RepairWorktree op")
	}
	// Drain the (failing — T:\x isn't a real worktree here) op so nothing
	// leaks. The dispatched-op shape (RepairWorktree on the TRANSLATED path)
	// is pinned by pendingRepairSwitch above plus the engine op's own tests.
	driveOp(t, r, cmd3)
}

func TestOpFinishedChainsRepairSwitch(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	m.pendingRepairSwitch = "/repaired/path"
	setGuardSeams(t, "linux", "/repaired/path")

	u, cmd := m.Update(opFinishedMsg{res: engine.Result{Changed: true}})
	got := u.(Model)

	if got.pendingRepairSwitch != "" {
		t.Fatalf("pendingRepairSwitch = %q after success, want cleared", got.pendingRepairSwitch)
	}
	if got.switchTarget != "/repaired/path" || !got.loading {
		t.Fatalf("must reRoot to the repaired path: switchTarget=%q loading=%v", got.switchTarget, got.loading)
	}
	if cmd == nil {
		t.Fatal("the chained reRoot must return its reload command")
	}
}

func TestOpFinishedErrorClearsRepairSwitch(t *testing.T) {
	m := newTestModel(t)
	m.loading = false                // newTestModel starts in the app-bootstrap loading state; isolate the no-switch assertion
	m.pendingSources = []sourceKey{} // this test bypasses startOp, so pendingSources would default to nil (= reload every
	// source); an explicit empty slice keeps the fallthrough reloadSourcesCmd from flipping
	// loading=true on its own, which would otherwise pollute the "did we switch" assertion below
	// with an unrelated, ordinary post-op source refresh.
	m.running = true
	m.pendingRepairSwitch = "/repaired/path"
	setGuardSeams(t, "linux", "/repaired/path")

	u, _ := m.Update(opFinishedMsg{err: errors.New("boom")})
	got := u.(Model)

	if got.pendingRepairSwitch != "" {
		t.Fatalf("pendingRepairSwitch = %q after error, want cleared", got.pendingRepairSwitch)
	}
	if got.switchTarget != "" || got.loading {
		t.Fatal("a failed repair must not switch")
	}
}

func TestAbortedOpDoesNotChainRepairSwitch(t *testing.T) {
	m := newTestModel(t)
	m.loading = false                // newTestModel starts in the app-bootstrap loading state; isolate the no-switch assertion
	m.pendingSources = []sourceKey{} // this test bypasses startOp, so pendingSources would default to nil (= reload every
	// source); an explicit empty slice keeps the fallthrough reloadSourcesCmd from flipping
	// loading=true on its own, which would otherwise pollute the "did we switch" assertion below
	// with an unrelated, ordinary post-op source refresh.
	m.running = true
	m.pendingRepairSwitch = "/repaired/path"
	setGuardSeams(t, "linux", "/repaired/path")

	// Changed:false, err:nil — an aborted/cancelled op must not chain.
	u, _ := m.Update(opFinishedMsg{res: engine.Result{Changed: false}})
	got := u.(Model)

	if got.pendingRepairSwitch != "" {
		t.Fatalf("pendingRepairSwitch = %q after abort, want cleared", got.pendingRepairSwitch)
	}
	if got.switchTarget != "" || got.loading {
		t.Fatal("an aborted op must not switch")
	}
}

func TestReRootClearsPendingRepairSwitch(t *testing.T) {
	m := newTestModel(t)
	m.pendingRepairSwitch = "/stale"
	u, _ := m.reRoot(t.TempDir())
	if got := u.(Model); got.pendingRepairSwitch != "" {
		t.Fatal("reRoot must clear pendingRepairSwitch")
	}
}

func TestOpAffectedSourcesRepairWorktree(t *testing.T) {
	got := opAffectedSources(engine.RepairWorktree{})
	if len(got) != 1 || got[0] != srcWorktrees {
		t.Fatalf("opAffectedSources(RepairWorktree) = %v, want [srcWorktrees]", got)
	}
}

func TestGoToWorktreeOffersRepairForForeignNotation(t *testing.T) {
	// The Branches-panel s-switch route ("<branch> is checked out in another
	// worktree" → go to worktree) targets a worktree of the CURRENT repo —
	// exactly the repairable case — so it must offer the repair modal like
	// the Worktrees-panel enter site, not the plain refusal (user-reported
	// gap: the natural Windows-side flow goes through Branches, not the
	// Worktrees tab).
	m := footerModel()
	m.worktrees[1].Path = "/mnt/t/repo/wt-x" // recorded in WSL notation
	m.sel[panelBranches] = 1                 // feat/x, checked out in wt-x
	m.focus = panelBranches
	setGuardSeams(t, "windows", `T:\repo\wt-x`) // only the translation exists here

	u, _ := m.Update(keyMsg("s"))
	got := u.(Model)
	if got.modal == nil || got.modal.req.ID != "switch-to-worktree" {
		t.Fatalf("s on a branch checked out elsewhere must open the jump modal, got %+v", got.modal)
	}
	u2, _ := got.modal.onResolve(got, "go to worktree")
	r := u2.(Model)
	if r.modal == nil || r.modal.req.ID != "worktree-cross-env-repair" {
		t.Fatalf("go to worktree on a foreign-notation worktree must offer repair; modal=%+v status=%q", r.modal, r.statusMsg)
	}
	if r.loading || r.switchTarget != "" {
		t.Fatal("the offer itself must not switch")
	}
}
