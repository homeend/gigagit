package tui

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/template"
)

func testCtx() template.Ctx {
	return template.Ctx{
		ParentBranch: "main",
		Repo:         "aaa",
		Seqs:         map[string]int{"issue": 7},
		Now:          func() time.Time { return time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC) },
		Rand:         rand.New(rand.NewPCG(1, 2)),
	}
}

func TestResolveWorktreeNamesTwoPhase(t *testing.T) {
	branch, path, err := resolveWorktreeNames("issue/<seq:issue>", "../<repo>.worktrees/<branch>", "", nil, testCtx())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if branch != "issue/7" {
		t.Fatalf("branch = %q, want issue/7", branch)
	}
	if path != "../aaa.worktrees/issue-7" {
		t.Fatalf("path = %q, want ../aaa.worktrees/issue-7", path)
	}
}

func TestResolveWorktreeNamesFixedBranch(t *testing.T) {
	branch, path, err := resolveWorktreeNames("ignored/<seq:issue>", "wt/<branch>", "hand/edited", nil, testCtx())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if branch != "hand/edited" {
		t.Fatalf("branch = %q, want hand/edited", branch)
	}
	if path != "wt/hand-edited" {
		t.Fatalf("path = %q, want wt/hand-edited", path)
	}
}

func TestResolveWorktreeNamesPropagatesError(t *testing.T) {
	_, _, err := resolveWorktreeNames("b-<bogus>", "p/<branch>", "", nil, testCtx())
	if err == nil {
		t.Fatal("expected unknown-token error to propagate")
	}
}

func TestResolveWorktreeNamesUserInput(t *testing.T) {
	branch, _, err := resolveWorktreeNames("issue/<user:id>", "p/<branch>", "", map[string]string{"id": "42"}, testCtx())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if branch != "issue/42" {
		t.Fatalf("branch = %q, want issue/42", branch)
	}
}

func modelWithConfig(t *testing.T, branchTmpl, pathTmpl string) Model {
	t.Helper()
	m := loadedModel(t)
	m.cfg = config.Config{Worktree: config.WorktreeConfig{
		DefaultBranchTemplate: branchTmpl,
		PathTemplate:          pathTmpl,
	}}
	return m
}

func TestOpenPopupOnW(t *testing.T) {
	m := modelWithConfig(t, "b/from-<parent-branch>", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	mm := updated.(Model)
	if mm.popup == nil {
		t.Fatal("pressing w should open the worktree popup")
	}
	if mm.popup.startPoint == "" {
		t.Error("popup startPoint (selected branch) should be set")
	}
	if mm.popup.state != stAction {
		t.Errorf("state = %v, want stAction when no user fields", mm.popup.state)
	}
	if mm.popup.previewBranch == "" {
		t.Error("preview should be computed on open")
	}
}

func TestPopupSwallowsGlobalKeys(t *testing.T) {
	m := modelWithConfig(t, "b/x", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("s"))
	m = updated.(Model)
	if m.running {
		t.Error("global keys must not fire while the popup is open")
	}
	if m.popup == nil {
		t.Error("popup should still be open after an inert key")
	}
}

func TestPopupEscCancels(t *testing.T) {
	m := modelWithConfig(t, "b/x", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("esc"))
	if updated.(Model).popup != nil {
		t.Error("esc should cancel the popup")
	}
}
