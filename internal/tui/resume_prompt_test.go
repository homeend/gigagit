package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// pausedResolvedModel is a Model observing "rebase paused, zero conflicted
// files" — the state the one-shot prompt fires on. Zero-value running/loading
// means opsIdle() is true; zero-value status has no conflicts.
func pausedResolvedModel() Model {
	return Model{conflict: domain.ConflictState{Op: "rebase", Source: "feature", Target: "main"}}
}

func TestMaybeResumePromptFiresOncePerPause(t *testing.T) {
	t.Parallel()
	m := pausedResolvedModel()
	m = m.maybeResumePrompt()
	if _, ok := m.topLayer().(*resumePromptPopup); !ok {
		t.Fatalf("expected resumePromptPopup on top, got %T", m.topLayer())
	}
	if !m.resumePromptShown {
		t.Fatal("one-shot flag not set when the prompt was shown")
	}
	m = m.popLayer() // user chose Not now
	m = m.maybeResumePrompt()
	if m.topLayer() != nil {
		t.Fatal("re-prompted for the same pause after Not now")
	}
}

func TestMaybeResumePromptSkipsWhileBusyThenRetries(t *testing.T) {
	t.Parallel()
	m := pausedResolvedModel()
	m.running = true // an op is in flight
	m = m.maybeResumePrompt()
	if m.topLayer() != nil || m.resumePromptShown {
		t.Fatal("prompted (or burned the one-shot flag) while not idle")
	}
	m.running = false
	m = m.maybeResumePrompt()
	if _, ok := m.topLayer().(*resumePromptPopup); !ok {
		t.Fatal("did not retry once idle — the flag must only be set when actually shown")
	}
}

func TestMaybeResumePromptSkipsWhileLayerOpen(t *testing.T) {
	t.Parallel()
	m := pausedResolvedModel()
	m = m.pushLayer(&commitPopup{}) // any other window owns the screen
	m = m.maybeResumePrompt()
	if m.resumePromptShown {
		t.Fatal("burned the one-shot flag under another layer")
	}
	if _, ok := m.topLayer().(*resumePromptPopup); ok {
		t.Fatal("prompted on top of another popup")
	}
}

func TestMaybeResumePromptNotWhileConflictsRemain(t *testing.T) {
	t.Parallel()
	m := pausedResolvedModel()
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "f.txt", Kind: model.KindUnmerged}}}
	m = m.maybeResumePrompt()
	if m.topLayer() != nil {
		t.Fatal("prompted while unmerged files remain — that is the conflict notice's job")
	}
}

func TestMaybeResumePromptReArmsOnStateChange(t *testing.T) {
	t.Parallel()
	m := pausedResolvedModel()
	m = m.maybeResumePrompt()
	m = m.popLayer()                    // Not now
	m.conflict = domain.ConflictState{} // op finished/aborted outside
	m = m.maybeResumePrompt()
	if m.resumePromptShown {
		t.Fatal("flag not re-armed when the paused op cleared")
	}
	m.conflict = domain.ConflictState{Op: "merge"} // a NEW pause
	m = m.maybeResumePrompt()
	if _, ok := m.topLayer().(*resumePromptPopup); !ok {
		t.Fatal("no prompt for a new pause instance")
	}
}

func TestResumePromptEscClosesWithoutOp(t *testing.T) {
	t.Parallel()
	m := pausedResolvedModel()
	m = m.maybeResumePrompt()
	p := m.topLayer().(*resumePromptPopup)
	m2, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m2.topLayer() != nil {
		t.Fatal("esc did not close the prompt")
	}
	if cmd != nil || m2.running {
		t.Fatal("esc must not start anything")
	}
}

// pausedResolvedRepoDir builds a real repo mid-rebase whose conflict has been
// resolved and staged but not continued.
func pausedResolvedRepoDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(tolerate string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil && args[0] != tolerate {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("", "init", "-q", "-b", "main")
	write("base\n")
	run("", "add", "-A")
	run("", "commit", "-qm", "base")
	run("", "checkout", "-q", "-b", "feature")
	write("theirs\n")
	run("", "add", "-A")
	run("", "commit", "-qm", "feature")
	run("", "checkout", "-q", "main")
	write("ours\n")
	run("", "add", "-A")
	run("", "commit", "-qm", "main")
	run("", "checkout", "-q", "feature")
	run("rebase", "rebase", "main") // pauses on the f.txt conflict
	write("resolved\n")
	run("", "add", "-A") // resolved + staged, NOT continued
	return dir
}

func TestResumePromptContinueDispatchesOp(t *testing.T) {
	t.Parallel()
	dir := pausedResolvedRepoDir(t)
	m := Model{svc: domain.OpenTUI(dir), conflict: domain.ConflictState{Op: "rebase"}}
	m = m.maybeResumePrompt()
	p, ok := m.topLayer().(*resumePromptPopup)
	if !ok {
		t.Fatalf("expected resumePromptPopup, got %T", m.topLayer())
	}
	m2, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // sel 0 = Continue
	if m2.topLayer() != nil {
		t.Fatal("prompt did not close on enter")
	}
	if !m2.running || cmd == nil {
		t.Fatal("Continue did not dispatch an operation")
	}
	// Drain the real op to completion so the temp dir isn't torn down under a
	// live git process.
	for {
		if _, done := (<-m2.opMsgs).(opFinishedMsg); done {
			break
		}
	}
}

func TestCanEnterConflictGates(t *testing.T) {
	t.Parallel()
	if (Model{}).canEnterConflict() {
		t.Fatal("x available on a clean repo")
	}
	paused := Model{conflict: domain.ConflictState{Op: "rebase"}}
	if !paused.canEnterConflict() {
		t.Fatal("x unavailable while a rebase is paused with zero conflicts")
	}
	conflicted := Model{status: model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "f", Kind: model.KindUnmerged}}}}
	if !conflicted.canEnterConflict() {
		t.Fatal("x unavailable while conflicts exist (regression)")
	}
	busy := paused
	busy.running = true
	if busy.canEnterConflict() {
		t.Fatal("x must stay opsIdle-gated")
	}
}

func TestStartConflictProcessOpensForPausedOpWithZeroFiles(t *testing.T) {
	t.Parallel()
	m := Model{conflict: domain.ConflictState{Op: "rebase"}}
	m2, cmd := startConflictProcess(m)
	if m2.proc == nil {
		t.Fatal("process did not open for a paused op with zero conflicted files")
	}
	if cmd == nil {
		t.Fatal("expected the in-progress probe cmd")
	}
	if m3, _ := startConflictProcess(Model{}); m3.proc != nil {
		t.Fatal("process opened on a clean repo")
	}
}

func TestResolveFooterBindingAdvertisesPausedOp(t *testing.T) {
	t.Parallel()
	m := Model{conflict: domain.ConflictState{Op: "rebase"}}
	for _, b := range globalBindings() {
		if b.id == "resolve" {
			if !b.when(m) {
				t.Fatal("[x] resolve not advertised while a rebase is paused")
			}
			return
		}
	}
	t.Fatal("resolve binding not found in globalBindings")
}

// An op continued/aborted OUTSIDE gg: x opens the process on stale
// m.conflict, the probe reports nothing in progress, and the release must
// clear the stale attribution so the ⏸ notice doesn't linger on a clean repo.
func TestInProgressProbeClearsStaleConflict(t *testing.T) {
	t.Parallel()
	m := Model{conflict: domain.ConflictState{Op: "rebase"}}
	m, _ = startConflictProcess(m)
	if m.proc == nil {
		t.Fatal("precondition: process should open on the stale paused op")
	}
	next, _ := m.Update(inProgressMsg{op: ""})
	m2 := next.(Model)
	if m2.proc != nil {
		t.Fatal("slot not released when probe found nothing in progress")
	}
	if m2.conflict.Op != "" {
		t.Fatal("stale m.conflict not cleared — phantom ⏸ notice would linger")
	}
}

// A stale-clear via the in-progress probe is an Op=="" transition: the spec
// mandates the one-shot flag re-arms so a FRESH pause prompts again.
func TestInProgressProbeReArmsResumePrompt(t *testing.T) {
	t.Parallel()
	m := Model{conflict: domain.ConflictState{Op: "rebase"}, resumePromptShown: true}
	m, _ = startConflictProcess(m)
	if m.proc == nil {
		t.Fatal("precondition: process should open on the stale paused op")
	}
	next, _ := m.Update(inProgressMsg{op: ""})
	m2 := next.(Model)
	if m2.resumePromptShown {
		t.Fatal("one-shot flag not re-armed on the probe's Op==\"\" transition — a new pause would never prompt")
	}
	// A fresh pause B observed next must prompt.
	m2.conflict = domain.ConflictState{Op: "merge"}
	m2 = m2.maybeResumePrompt()
	if _, ok := m2.topLayer().(*resumePromptPopup); !ok {
		t.Fatal("new pause after stale-clear did not prompt")
	}
}

// The probe reporting a LIVE op must not clear attribution or the flag.
func TestInProgressProbeKeepsLiveConflict(t *testing.T) {
	t.Parallel()
	m := Model{conflict: domain.ConflictState{Op: "rebase"}, resumePromptShown: true}
	m, _ = startConflictProcess(m)
	next, _ := m.Update(inProgressMsg{op: "rebase"})
	m2 := next.(Model)
	if m2.proc == nil {
		t.Fatal("live op must keep the process open")
	}
	if m2.conflict.Op != "rebase" {
		t.Fatal("live op must not clear m.conflict")
	}
}

func TestMaybeResumePromptSkipsWhileActionMenuOpen(t *testing.T) {
	t.Parallel()
	m := Model{conflict: domain.ConflictState{Op: "rebase"}}
	m.actionMenu = &actionMenu{rows: []actionRow{{id: "a"}}}
	m = m.maybeResumePrompt()
	if m.resumePromptShown {
		t.Fatal("burned the one-shot flag under an open action menu")
	}
	if _, ok := m.topLayer().(*resumePromptPopup); ok {
		t.Fatal("prompted under the action menu overlay")
	}
}
