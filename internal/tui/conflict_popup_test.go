package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/observ"
)

func TestConflictPopupZCyclesMode(t *testing.T) {
	m := Model{width: 100, height: 30}
	m.conflictPopup = &conflictPopup{files: []model.FileStatus{
		{Path: "a.go", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'},
	}}
	u, _ := m.updateConflictPopupKey(keyMsg("z"))
	mm := u.(Model)
	if mm.conflictPopup.mode != modeWrap {
		t.Fatalf("after z, mode = %v, want modeWrap", mm.conflictPopup.mode)
	}
}

func conflictModel() Model {
	m := Model{width: 120, height: 30, focus: panelFiles, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Branch: "zzz", Files: []model.FileStatus{
		{Path: "uu.txt", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'},
		{Path: "md.txt", Kind: model.KindUnmerged, Staged: 'D', Unstaged: 'U'},
	}}
	return m
}

// conflictRepoTUI builds a real repo paused on a merge with a UU and a DU
// conflict and returns a Model with a live service over it, status loaded.
func conflictRepoTUI(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil && args[0] != "merge" {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) { os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644) }
	run("init", "-q", "-b", "main")
	write("uu.txt", "base\n")
	write("md.txt", "base\n")
	run("add", "-A")
	run("commit", "-qm", "base")
	run("checkout", "-q", "-b", "feature")
	write("uu.txt", "theirs\n")
	write("md.txt", "theirs-mod\n")
	run("add", "-A")
	run("commit", "-qm", "feature")
	run("checkout", "-q", "main")
	write("uu.txt", "ours\n")
	run("add", "-A")
	run("rm", "-q", "md.txt")
	run("commit", "-qm", "main")
	run("merge", "feature") // conflicts (exit 1) — tolerated above
	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
	m := New(domain.New(repo))
	m.width, m.height = 120, 30
	m.loading = false
	st, err := repo.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m.status = st
	return m
}

func TestStatusBarShowsConflictNotice(t *testing.T) {
	m := conflictModel()
	out := m.View()
	if !strings.Contains(out, "2 conflicts") || !strings.Contains(out, "[x]") {
		t.Errorf("status bar should announce conflicts:\n%s", out)
	}
}

func conflictModelWithSource() Model {
	m := conflictModel()
	m.conflict = domain.ConflictState{Op: "merge", Source: "feature", Target: "main"}
	return m
}

func TestStatusBarShowsConflictSource(t *testing.T) {
	m := conflictModelWithSource()
	out := m.View()
	if !strings.Contains(out, "merging feature into main") {
		t.Errorf("status bar should name the source:\n%s", out)
	}
}

func TestConflictPopupShowsSourceSubtitle(t *testing.T) {
	m := conflictModelWithSource()
	mm, _ := m.Update(keyMsg("x"))
	m = mm.(Model)
	out := m.View()
	if !strings.Contains(out, "merging feature into main") {
		t.Errorf("popup should show the source subtitle:\n%s", out)
	}
}

func TestXOpensConflictPopup(t *testing.T) {
	m := conflictModel()
	mm, _ := m.Update(keyMsg("x"))
	if mm.(Model).conflictPopup == nil {
		t.Fatal("x should open the conflict popup when conflicts exist")
	}
}

func TestXNoOpWithoutConflicts(t *testing.T) {
	m := Model{width: 120, height: 30, sel: map[panel]int{}}
	mm, _ := m.Update(keyMsg("x"))
	if mm.(Model).conflictPopup != nil {
		t.Fatal("x must do nothing when there are no conflicts")
	}
}

// selectConflict points the popup at the file with the given path.
func selectConflict(t *testing.T, p *conflictPopup, path string) {
	t.Helper()
	for i, f := range p.files {
		if f.Path == path {
			p.sel = i
			return
		}
	}
	t.Fatalf("conflict %q not in popup: %+v", path, p.files)
}

func TestConflictPopupKeepIncomingDispatches(t *testing.T) {
	m := conflictRepoTUI(t)
	mm, _ := m.Update(keyMsg("x"))
	m = mm.(Model)
	selectConflict(t, m.conflictPopup, "uu.txt") // both-sides
	mm, cmd := m.updateConflictPopupKey(keyMsg("i"))
	got := mm.(Model)
	if !got.running || cmd == nil {
		t.Fatal("i should dispatch a ResolveConflict op (keep incoming)")
	}
	if got.conflictPopup != nil {
		t.Error("popup should close while the op runs (reopens on refresh)")
	}
	driveOp(t, got, cmd) // drain the op so the goroutine finishes cleanly
}

func TestConflictPopupModifyDeleteKeys(t *testing.T) {
	m := conflictRepoTUI(t)
	mm, _ := m.Update(keyMsg("x"))
	m = mm.(Model)
	selectConflict(t, m.conflictPopup, "md.txt") // modify/delete (DU)
	// 'o' (ours) is NOT a valid key here; 'k' keep-modified IS.
	mm, _ = m.updateConflictPopupKey(keyMsg("o"))
	if mm.(Model).running {
		t.Error("ours must be inert on a modify/delete file")
	}
	mm, cmd := m.updateConflictPopupKey(keyMsg("k"))
	got := mm.(Model)
	if !got.running || cmd == nil {
		t.Error("k (keep modified) should dispatch on a modify/delete file")
	}
	driveOp(t, got, cmd)
}

func TestConflictPopupReopensAfterResolve(t *testing.T) {
	m := conflictModel()
	m.reopenConflict = true
	// Simulate the post-op reload arriving with one conflict already cleared.
	one := model.WorkingTreeStatus{Branch: "zzz", Files: []model.FileStatus{
		{Path: "md.txt", Kind: model.KindUnmerged, Staged: 'D', Unstaged: 'U'},
	}}
	mm, cmd := m.Update(dataLoadedMsg{gen: m.loadGen, status: one})
	got := mm.(Model)
	if got.conflictPopup == nil || len(got.conflictPopup.files) != 1 {
		t.Fatalf("popup should reopen with the remaining conflict, got %+v", got.conflictPopup)
	}
	if got.reopenConflict {
		t.Error("reopenConflict should be cleared after reopening")
	}
	_ = cmd
}

func TestConflictPopupOpMapping(t *testing.T) {
	// keep-modified on a DU file resolves to KeepTheirs (the present side).
	du := model.FileStatus{Path: "md.txt", Kind: model.KindUnmerged, Staged: 'D', Unstaged: 'U'}
	if got := keepModifiedAction(du); got != engine.KeepTheirs {
		t.Errorf("DU keep-modified = %v, want KeepTheirs", got)
	}
	ud := model.FileStatus{Path: "x", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'D'}
	if got := keepModifiedAction(ud); got != engine.KeepOurs {
		t.Errorf("UD keep-modified = %v, want KeepOurs", got)
	}
}
