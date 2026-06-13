package tui

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
)

func wtHeadTui(t *testing.T, wtPath string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", wtPath, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref in %s: %v", wtPath, err)
	}
	return strings.TrimSpace(string(out))
}

func TestWOpensExistingModePopup(t *testing.T) {
	m := modelWithConfig(t, "b/from-<parent-branch>", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	mm := updated.(Model)
	if mm.popup == nil {
		t.Fatal("w should open the worktree popup")
	}
	if !mm.popup.existing {
		t.Fatal("w must open the popup in existing-branch mode")
	}
	// The branch is fixed to the selection — not resolved from the template.
	if mm.popup.previewBranch != mm.popup.startPoint {
		t.Fatalf("previewBranch = %q, want the fixed selection %q", mm.popup.previewBranch, mm.popup.startPoint)
	}
	if strings.HasPrefix(mm.popup.previewBranch, "b/from-") {
		t.Fatal("existing mode must not run the branch template")
	}
	// Path resolves with <branch> = the fixed branch.
	if !strings.Contains(mm.popup.previewPath, mm.popup.startPoint) {
		t.Fatalf("previewPath = %q, want it to contain %q", mm.popup.previewPath, mm.popup.startPoint)
	}
}

func TestExistingModeIgnoresBranchTemplateUserFields(t *testing.T) {
	// The branch template's <user:...> must NOT be prompted in existing mode;
	// only the path template's fields are.
	m := modelWithConfig(t, "<user:who>/x", "wt/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	mm := updated.(Model)
	if len(mm.popup.labels) != 0 {
		t.Fatalf("labels = %v, want none (branch template bypassed, path has no fields)", mm.popup.labels)
	}
	if mm.popup.state != stAction {
		t.Fatalf("state = %v, want stAction with no fields", mm.popup.state)
	}
}

func TestExistingModeEditKeyInert(t *testing.T) {
	m := modelWithConfig(t, "b/x", "wt/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	if m.popup.state == stEdit {
		t.Fatal("e must be inert in existing mode — the branch is the point")
	}
}

func TestExistingModeCreateOpAndSeqs(t *testing.T) {
	m := modelWithConfig(t, "issue/<seq:issue>", "../<repo>.worktrees/<seq:wt>-<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)

	op, ok := m.popup.createOp().(engine.CreateWorktreeForBranch)
	if !ok {
		t.Fatalf("createOp = %T, want engine.CreateWorktreeForBranch", m.popup.createOp())
	}
	if op.Branch != m.popup.startPoint || op.Path != m.popup.previewPath {
		t.Fatalf("op {%q,%q} != {%q,%q}", op.Branch, op.Path, m.popup.startPoint, m.popup.previewPath)
	}
	// Only the PATH template's <seq> names are consumed in existing mode.
	if got := m.popup.consumedSeqNames(); len(got) != 1 || got[0] != "wt" {
		t.Fatalf("consumedSeqNames = %v, want [wt]", got)
	}
}

func TestExistingModeEndToEnd(t *testing.T) {
	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "feature/have")

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.cfg.Worktree.PathTemplate = "../<repo>.worktrees/<branch>"
	m.focus = panelBranches
	for vi := 0; vi < m.panelLen(panelBranches); vi++ {
		m.sel[panelBranches] = vi
		if bi, ok := m.backingIndex(panelBranches); ok && m.branches[bi].Name == "feature/have" {
			break
		}
	}

	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	if m.popup == nil || !m.popup.existing || m.popup.startPoint != "feature/have" {
		t.Fatalf("popup not in existing mode for feature/have: %+v", m.popup)
	}
	wantPath := m.popup.previewPath           // capture before the popup closes
	updated, cmd := m.Update(keyMsg("enter")) // create
	m = updated.(Model)
	m = driveOp(t, m, cmd)

	// The created worktree has the existing branch checked out.
	if !filepath.IsAbs(wantPath) {
		wantPath = filepath.Clean(filepath.Join(dir, wantPath))
	}
	if got := wtHeadTui(t, wantPath); got != "feature/have" {
		t.Fatalf("worktree HEAD = %q, want feature/have", got)
	}
}

func TestExistingModeRenderTitleAndHints(t *testing.T) {
	m := modelWithConfig(t, "b/x", "wt/<branch>")
	m.width, m.height = 80, 24
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	out := m.View()
	if !strings.Contains(out, "Create worktree for") {
		t.Errorf("existing-mode title missing:\n%s", out)
	}
	if strings.Contains(out, "edit name") {
		t.Errorf("existing mode must not offer the edit-name hint:\n%s", out)
	}
}
