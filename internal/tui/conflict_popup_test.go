package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
)

// conflictModel builds a Model whose status holds two unmerged files (a
// both-sides UU and a one-sided DU), with no live service.
func conflictModel() Model {
	m := Model{width: 120, height: 30, focus: panelFiles, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Branch: "zzz", Files: []model.FileStatus{
		{Path: "uu.txt", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'},
		{Path: "md.txt", Kind: model.KindUnmerged, Staged: 'D', Unstaged: 'U'},
	}}
	return m
}

func conflictModelWithSource() Model {
	m := conflictModel()
	m.conflict = domain.ConflictState{Op: "merge", Source: "feature", Target: "main"}
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

// selectConflictProc points the conflict process at the file with the given path.
func selectConflictProc(t *testing.T, cp *conflictProcess, path string) {
	t.Helper()
	for i, f := range cp.files {
		if f.Path == path {
			cp.sel = i
			return
		}
	}
	t.Fatalf("conflict %q not in process: %+v", path, cp.files)
}

// --- the notice (unchanged: a passive affordance to enter the process) ---

func TestStatusBarShowsConflictNotice(t *testing.T) {
	m := conflictModel()
	out := m.View()
	if !strings.Contains(out, "2 conflicts") || !strings.Contains(out, "[x]") {
		t.Errorf("status bar should announce conflicts + the [x] affordance:\n%s", out)
	}
}

func TestStatusBarShowsConflictSource(t *testing.T) {
	out := conflictModelWithSource().View()
	if !strings.Contains(out, "merging feature into main") {
		t.Errorf("status bar should name the source:\n%s", out)
	}
}

// --- entering / leaving the process via x ---

func TestXStartsConflictProcess(t *testing.T) {
	m := conflictModel()
	mm, _ := m.Update(keyMsg("x"))
	if _, ok := mm.(Model).proc.(*conflictProcess); !ok {
		t.Fatal("x must start the conflict process when conflicts exist")
	}
}

func TestXNoOpWithoutConflicts(t *testing.T) {
	m := Model{width: 120, height: 30, sel: map[panel]int{}}
	mm, _ := m.Update(keyMsg("x"))
	if mm.(Model).proc != nil {
		t.Fatal("x must do nothing when there are no conflicts")
	}
}

func TestConflictProcessShowsSourceSubtitle(t *testing.T) {
	m := conflictModelWithSource()
	mm, _ := m.Update(keyMsg("x"))
	out := mm.(Model).View()
	if !strings.Contains(out, "merging feature into main") {
		t.Errorf("the conflict process must show the source subtitle:\n%s", out)
	}
}

func TestConflictProcessZCyclesMode(t *testing.T) {
	m := conflictModel()
	m, _ = startConflictProcess(m)
	u, _ := m.Update(keyMsg("z"))
	m = u.(Model)
	if m.proc.(*conflictProcess).mode != modeWrap {
		t.Fatalf("after z, mode = %v, want modeWrap", m.proc.(*conflictProcess).mode)
	}
}

// The hint is longer than the box, so it must WRAP across lines (not truncate) —
// every key must remain visible.
func TestConflictProcessHintNotTruncated(t *testing.T) {
	m := Model{width: 80, height: 30}
	cp := &conflictProcess{
		st:         confListing,
		files:      []model.FileStatus{{Path: "a.go", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'}},
		inProgress: "merge",
	}
	out := ansi.Strip(cp.render(m, ""))
	for _, tok := range []string{"[enter]", "[C]", "[i]", "[m]", "[A]", "[a]", "[L]", "[z]"} {
		if !strings.Contains(out, tok) {
			t.Errorf("hint key %q missing (truncated, not wrapped?):\n%s", tok, out)
		}
	}
}

// --- real-git integration: per-file resolve actions actually dispatch ---

func TestConflictProcessKeepCurrentDispatches(t *testing.T) {
	m := conflictRepoTUI(t)
	mm, _ := m.Update(keyMsg("x"))
	m = mm.(Model)
	cp := m.proc.(*conflictProcess)
	selectConflictProc(t, cp, "uu.txt") // both-sides
	mm, cmd := m.Update(keyMsg("C"))
	got := mm.(Model)
	if !got.running || cmd == nil {
		t.Fatal("C should dispatch a ResolveConflict op (keep current)")
	}
	if cp.st != confWorking {
		t.Fatalf("a resolve must enter Working, got %d", cp.st)
	}
	driveOp(t, got, cmd)
}

func TestConflictProcessKeepIncomingDispatches(t *testing.T) {
	m := conflictRepoTUI(t)
	mm, _ := m.Update(keyMsg("x"))
	m = mm.(Model)
	cp := m.proc.(*conflictProcess)
	selectConflictProc(t, cp, "uu.txt") // both-sides
	mm, cmd := m.Update(keyMsg("i"))
	got := mm.(Model)
	if !got.running || cmd == nil {
		t.Fatal("i should dispatch a ResolveConflict op (keep incoming)")
	}
	driveOp(t, got, cmd)
}

func TestConflictProcessModifyDeleteKeys(t *testing.T) {
	m := conflictRepoTUI(t)
	mm, _ := m.Update(keyMsg("x"))
	m = mm.(Model)
	cp := m.proc.(*conflictProcess)
	selectConflictProc(t, cp, "md.txt") // modify/delete (DU)
	// 'C' (keep current) is NOT valid here; 'k' keep-modified IS.
	mm, _ = m.Update(keyMsg("C"))
	if mm.(Model).running {
		t.Error("keep-current must be inert on a modify/delete file")
	}
	mm, cmd := m.Update(keyMsg("k"))
	got := mm.(Model)
	if !got.running || cmd == nil {
		t.Error("k (keep modified) should dispatch on a modify/delete file")
	}
	driveOp(t, got, cmd)
}

// driveChain runs the reactive cmd chain (op events, reloads, in-progress
// probes) until it quiesces, so a full conflict flow can be exercised through
// real git — unlike driveOp it does not stop when the op finishes, because the
// release path continues across several more messages.
func driveChain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; i < 200 && cmd != nil; i++ {
		msg := cmd()
		if msg == nil {
			break
		}
		u, next := m.Update(msg)
		m = u.(Model)
		cmd = next
	}
	return m
}

// The whole point of the feature wired together: enter → resolve both files →
// continue → the slot releases. This is the chain (opFinished→finished→reload→
// refreshed→probe→inProgress) where a routing bug would hide.
func TestConflictProcessEndToEndResolveContinueReleases(t *testing.T) {
	m := conflictRepoTUI(t)
	mm, _ := m.Update(keyMsg("x"))
	m = mm.(Model)

	// resolve uu.txt (both-sides) — keep ours
	selectConflictProc(t, m.proc.(*conflictProcess), "uu.txt")
	mm, cmd := m.Update(keyMsg("C"))
	m = driveChain(t, mm.(Model), cmd)
	if m.proc == nil {
		t.Fatal("md.txt still conflicts — the process must stay active after one resolve")
	}

	// resolve md.txt (modify/delete) — keep modified
	selectConflictProc(t, m.proc.(*conflictProcess), "md.txt")
	mm, cmd = m.Update(keyMsg("k"))
	m = driveChain(t, mm.(Model), cmd)
	if m.proc == nil {
		t.Fatal("all files resolved but not continued — process must stay (offering continue)")
	}
	if cp := m.proc.(*conflictProcess); len(cp.files) != 0 || cp.inProgress != "merge" {
		t.Fatalf("expected all-resolved + merge in progress, got %d files, inProgress=%q", len(cp.files), cp.inProgress)
	}

	// continue the merge → the repo goes clean → the slot releases
	mm, cmd = m.Update(keyMsg("c"))
	m = driveChain(t, mm.(Model), cmd)
	if m.proc != nil {
		t.Fatalf("continue must complete the merge and release the slot, got %T", m.proc)
	}
	if n := len(m.status.Conflicts()); n != 0 {
		t.Fatalf("no conflicts should remain after continue, got %d", n)
	}
}

func TestConflictKeepModifiedMapping(t *testing.T) {
	du := model.FileStatus{Path: "md.txt", Kind: model.KindUnmerged, Staged: 'D', Unstaged: 'U'}
	if got := keepModifiedAction(du); got != engine.KeepTheirs {
		t.Errorf("DU keep-modified = %v, want KeepTheirs", got)
	}
	ud := model.FileStatus{Path: "x", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'D'}
	if got := keepModifiedAction(ud); got != engine.KeepOurs {
		t.Errorf("UD keep-modified = %v, want KeepOurs", got)
	}
}
