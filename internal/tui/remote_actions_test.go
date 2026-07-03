package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

func TestCopyRowsBranchesHaveIdAndSha(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	m.branches = []model.Branch{{Name: "main", Hash: "abc1234"}}
	rows := m.contextCopyRows()
	if r, ok := findRow(rows, "copy-branch-name"); !ok || r.copyText != "main" {
		t.Fatalf("missing copy-branch-name=main; rows=%v", rows)
	}
	if r, ok := findRow(rows, "copy-commit-id"); !ok || r.copyText != "abc1234" {
		t.Fatalf("missing copy-commit-id=abc1234; rows=%v", rows)
	}
	if _, ok := findRow(rows, "copy-commit-sha"); !ok {
		t.Fatalf("missing copy-commit-sha; rows=%v", rows)
	}
}

func TestCopyRowsRemotesHaveIdAndSha(t *testing.T) {
	m := footerModel()
	m.focus = panelRemotes
	m.remoteBranches = []model.RemoteBranch{{Name: "origin/foo", Remote: "origin", Branch: "foo", Hash: "dead111"}}
	rows := m.contextCopyRows()
	if r, ok := findRow(rows, "copy-branch-name"); !ok || r.copyText != "origin/foo" {
		t.Fatalf("missing copy-branch-name=origin/foo; rows=%v", rows)
	}
	if r, ok := findRow(rows, "copy-commit-id"); !ok || r.copyText != "dead111" {
		t.Fatalf("missing copy-commit-id=dead111; rows=%v", rows)
	}
	if _, ok := findRow(rows, "copy-commit-sha"); !ok {
		t.Fatalf("missing copy-commit-sha; rows=%v", rows)
	}
}

func TestCopyShaRowFallsBackWithoutService(t *testing.T) {
	m := footerModel() // no svc set
	row := m.copyShaRow("origin/foo", "dead111")
	if row.run == nil {
		t.Fatal("copyShaRow must carry a run handler")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("copyShaRow run returned nil cmd")
	}
}

func TestCopyShaRowResolvesFullViaService(t *testing.T) {
	fr := gitexec.NewFakeRunner()
	fr.SetResponse("git rev-parse", gitexec.Result{Stdout: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\n"})
	m := footerModel()
	m.svc = domain.New(&git.Repo{Runner: fr})
	row := m.copyShaRow("origin/foo", "dead111")
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("expected a copy cmd")
	}
}

func remoteModel() Model {
	m := footerModel()
	m.focus = panelRemotes
	m.remoteBranches = []model.RemoteBranch{{Name: "origin/foo", Remote: "origin", Branch: "foo", Hash: "dead111"}}
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m.status.Branch = "main"
	return m
}

func TestRemoteOpRowsPresentWhenAttached(t *testing.T) {
	m := remoteModel()
	got := ids(availableActions(m))
	for _, id := range []string{"remote-worktree", "remote-merge", "remote-rebase"} {
		if !got[id] {
			t.Fatalf("expected %s in remote menu; got %v", id, got)
		}
	}
}

func TestRemoteMergeRebaseHiddenOnDetachedHEAD(t *testing.T) {
	m := remoteModel()
	m.status.Branch = "" // detached
	got := ids(availableActions(m))
	if got["remote-merge"] || got["remote-rebase"] {
		t.Fatalf("merge/rebase must be hidden on detached HEAD; got %v", got)
	}
	if !got["remote-worktree"] {
		t.Fatalf("worktree-from-remote should still be offered on detached HEAD; got %v", got)
	}
}

func TestRemoteMergeRowDispatchesSmartMerge(t *testing.T) {
	m := remoteModel()
	m.cfg.UI.DisableSlowOpConfirm = true // test op wiring, not confirm UX
	row, ok := m.remoteMergeRow()
	if !ok {
		t.Fatal("remoteMergeRow not available")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("merge row run returned nil cmd")
	}
}

func TestRemoteRebaseRowDispatchesSmartRebase(t *testing.T) {
	m := remoteModel()
	m.cfg.UI.DisableSlowOpConfirm = true // test op wiring, not confirm UX
	row, ok := m.remoteRebaseRow()
	if !ok {
		t.Fatal("remoteRebaseRow not available")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("rebase row run returned nil cmd")
	}
}

func TestRemoteWorktreeRowOpensPopup(t *testing.T) {
	m := remoteModel()
	row, ok := m.remoteCreateWorktreeRow()
	if !ok {
		t.Fatal("remoteCreateWorktreeRow not available")
	}
	nm, _ := row.run(m)
	if _, isWt := nm.(Model).topLayer().(*worktreePopup); !isWt {
		t.Fatalf("expected worktreePopup on top after run; got %T", nm.(Model).topLayer())
	}
}

func TestRemoteDeleteRowPresent(t *testing.T) {
	m := remoteModel()
	got := ids(availableActions(m))
	if !got["remote-delete"] {
		t.Fatalf("expected remote-delete in menu; got %v", got)
	}
}

func TestRemoteDeleteRowAbsentWithoutSelection(t *testing.T) {
	m := remoteModel()
	m.remoteBranches = nil // empty list → no selection
	got := ids(availableActions(m))
	if got["remote-delete"] {
		t.Fatalf("remote-delete must be absent with no selection; got %v", got)
	}
}

func TestRemoteDeleteRowDispatches(t *testing.T) {
	m := remoteModel()
	row, ok := m.remoteDeleteRow()
	if !ok {
		t.Fatal("remoteDeleteRow not available")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("delete row run returned nil cmd")
	}
}

// remote-reset appears only when the selected remote branch is the remote
// counterpart of the CHECKED-OUT branch (rb.Branch == cur), because git reset
// moves HEAD's branch. remoteModel() is on "main" with origin/foo selected, so
// the row is absent there and present once foo is checked out.
func TestRemoteResetRowGatedToCurrentBranch(t *testing.T) {
	m := remoteModel() // on "main", origin/foo selected → mismatch
	if got := ids(availableActions(m)); got["remote-reset"] {
		t.Fatalf("remote-reset must be absent when the remote is not the current branch; got %v", got)
	}
	m.status.Branch = "foo" // now origin/foo IS the current branch's remote
	if got := ids(availableActions(m)); !got["remote-reset"] {
		t.Fatalf("remote-reset must appear when the remote matches the current branch; got %v", got)
	}
}

func TestRemoteResetRowHiddenOnDetachedHEAD(t *testing.T) {
	m := remoteModel()
	m.status.Branch = "" // detached
	if got := ids(availableActions(m)); got["remote-reset"] {
		t.Fatalf("remote-reset must be hidden on detached HEAD; got %v", got)
	}
}

// The remote reset always prompts — even with slow-op confirms disabled — because
// it is a one-click hard reset whose engine Mode:"hard" preset suppresses the
// engine's own reset modals. Running the row opens the confirm modal (no op yet);
// answering Yes starts the reset.
func TestRemoteResetRowAlwaysConfirms(t *testing.T) {
	m := remoteModel()
	m.status.Branch = "foo"
	m.cfg.UI.DisableSlowOpConfirm = true // disabled confirms must NOT bypass this reset
	row, ok := m.remoteResetRow()
	if !ok {
		t.Fatal("remoteResetRow not available")
	}
	nm, cmd := row.run(m)
	rm := nm.(Model)
	if rm.modal == nil || rm.modal.req.ID != "confirm-slow-op" {
		t.Fatalf("reset must always open a confirm modal even with confirms disabled; modal=%v", rm.modal)
	}
	if cmd != nil {
		t.Fatal("opening the confirm modal must not start the op yet")
	}
	if _, cmd2 := rm.resolveModal("Yes"); cmd2 == nil {
		t.Fatal("confirming Yes must start the reset op")
	}
	if _, cmd3 := rm.resolveModal("No"); cmd3 != nil {
		t.Fatal("answering No must not start any op")
	}
}

// The remote-branch rows must NOT leak onto the menu when another left tab
// (Branches/Worktrees) is focused, even though the Remotes panel still holds a
// stored selection. Regression for the bug where the . menu offered "Rebase
// current onto origin/…" while the Branches tab was active.
func TestRemoteRowsAbsentWhenBranchesTabFocused(t *testing.T) {
	m := remoteModel() // populates m.remoteBranches + a Remotes selection
	m.focus = panelBranches
	got := ids(availableActions(m))
	for _, id := range []string{"remote-worktree", "remote-merge", "remote-rebase", "remote-delete"} {
		if got[id] {
			t.Fatalf("%s must be absent while the Branches tab is focused; got %v", id, got)
		}
	}
}

func TestRemoteCheckoutAsRowsPresent(t *testing.T) {
	m := remoteModel()
	got := ids(availableActions(m))
	for _, id := range []string{"remote-checkout-as", "remote-switch-as"} {
		if !got[id] {
			t.Fatalf("expected %s in remote menu; got %v", id, got)
		}
	}
}

func TestRemoteCheckoutAsRowsAbsentWhenBranchesTabFocused(t *testing.T) {
	m := remoteModel()
	m.focus = panelBranches // Remotes still holds a stored selection — must not leak
	got := ids(availableActions(m))
	if got["remote-checkout-as"] || got["remote-switch-as"] {
		t.Fatalf("checkout-as rows must be absent off the Remotes tab; got %v", got)
	}
}

func TestRemoteCheckoutAsRowOpensPrefilledPopup(t *testing.T) {
	m := remoteModel()
	row, ok := m.remoteCheckoutAsRow()
	if !ok {
		t.Fatal("remoteCheckoutAsRow not available")
	}
	nm, _ := row.run(m)
	p, isPopup := nm.(Model).topLayer().(*checkoutAsPopup)
	if !isPopup {
		t.Fatalf("expected checkoutAsPopup on top; got %T", nm.(Model).topLayer())
	}
	if p.remoteRef != "origin/foo" || p.name.Value() != "foo" || p.intent != engine.CheckoutStay {
		t.Fatalf("popup = ref %q prefill %q intent %v, want origin/foo foo stay", p.remoteRef, p.name.Value(), p.intent)
	}
}

func TestRemoteSwitchAsRowCarriesSwitchIntent(t *testing.T) {
	m := remoteModel()
	row, ok := m.remoteSwitchAsRow()
	if !ok {
		t.Fatal("remoteSwitchAsRow not available")
	}
	nm, _ := row.run(m)
	p, isPopup := nm.(Model).topLayer().(*checkoutAsPopup)
	if !isPopup {
		t.Fatalf("expected checkoutAsPopup on top; got %T", nm.(Model).topLayer())
	}
	if p.intent != engine.CheckoutSwitch {
		t.Fatalf("intent = %v, want CheckoutSwitch", p.intent)
	}
}
