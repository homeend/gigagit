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
	p := layerOf[*worktreePopup](mm)
	if p == nil {
		t.Fatal("w should open the worktree popup")
	}
	if !p.existing {
		t.Fatal("w must open the popup in existing-branch mode")
	}
	// The branch is fixed to the selection — not resolved from the template.
	if p.previewBranch != p.startPoint {
		t.Fatalf("previewBranch = %q, want the fixed selection %q", p.previewBranch, p.startPoint)
	}
	if strings.HasPrefix(p.previewBranch, "b/from-") {
		t.Fatal("existing mode must not run the branch template")
	}
	// Path resolves with <branch> = the fixed branch.
	if !strings.Contains(p.previewPath, p.startPoint) {
		t.Fatalf("previewPath = %q, want it to contain %q", p.previewPath, p.startPoint)
	}
}

func TestExistingModeIgnoresBranchTemplateUserFields(t *testing.T) {
	// The branch template's <user:...> must NOT be prompted in existing mode;
	// only the path template's fields are.
	m := modelWithConfig(t, "<user:who>/x", "wt/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	mm := updated.(Model)
	p := layerOf[*worktreePopup](mm)
	if len(p.labels) != 0 {
		t.Fatalf("labels = %v, want none (branch template bypassed, path has no fields)", p.labels)
	}
	if p.state != stAction {
		t.Fatalf("state = %v, want stAction with no fields", p.state)
	}
}

func TestExistingModeEditKeyInert(t *testing.T) {
	m := modelWithConfig(t, "b/x", "wt/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	if layerOf[*worktreePopup](m).state == stEdit {
		t.Fatal("e must be inert in existing mode — the branch is the point")
	}
}

func TestExistingModeCreateOpAndSeqs(t *testing.T) {
	m := modelWithConfig(t, "issue/<seq:issue>", "../<repo>.worktrees/<seq:wt>-<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)

	p := layerOf[*worktreePopup](m)
	op, ok := p.createOp().(engine.CreateWorktreeForBranch)
	if !ok {
		t.Fatalf("createOp = %T, want engine.CreateWorktreeForBranch", p.createOp())
	}
	if op.Branch != p.startPoint || op.Path != p.previewPath {
		t.Fatalf("op {%q,%q} != {%q,%q}", op.Branch, op.Path, p.startPoint, p.previewPath)
	}
	// Only the PATH template's <seq> names are consumed in existing mode.
	if got := p.consumedSeqNames(); len(got) != 1 || got[0] != "wt" {
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
	p := layerOf[*worktreePopup](m)
	if p == nil || !p.existing || p.startPoint != "feature/have" {
		t.Fatalf("popup not in existing mode for feature/have: %+v", p)
	}
	wantPath := p.previewPath                 // capture before the popup closes
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
