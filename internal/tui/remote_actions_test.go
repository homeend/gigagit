package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
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
