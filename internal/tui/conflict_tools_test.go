package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/promptstate"
)

// NOTE: keyRunes(s) already exists in this package (irebase_view_test.go) —
// use it, do NOT redeclare it.

// cleanupToolTemp snapshots the gg-tool-* / gg-context-* temp files already
// in os.TempDir() and registers a t.Cleanup that removes any new ones.
// Needed because execToolCmd writes the resolved command to a real temp
// script (toolScript) SYNCHRONOUSLY when it builds the tea.ExecProcess
// command — before that command ever runs — and buildToolRun writes a real
// per-run context file (toolContextFile) just as synchronously, on every
// call — so any test that drives the flow past "t"+enter leaks real files
// even though it never executes the returned tea.Cmd. Call this before the
// first keypress that starts a tool run.
func cleanupToolTemp(t *testing.T) {
	t.Helper()
	patterns := []string{"gg-tool-*", "gg-context-*"}
	seen := map[string]bool{}
	for _, pat := range patterns {
		before, _ := filepath.Glob(filepath.Join(os.TempDir(), pat))
		for _, p := range before {
			seen[p] = true
		}
	}
	t.Cleanup(func() {
		for _, pat := range patterns {
			after, _ := filepath.Glob(filepath.Join(os.TempDir(), pat))
			for _, p := range after {
				if !seen[p] {
					os.Remove(p)
				}
			}
		}
	})
}

func conflictModelWithTools(t *testing.T, cmds ...config.ToolCommand) (Model, *conflictProcess) {
	t.Helper()
	m := toolCfg(cmds...)
	// A temp-file prompt store, so approval tests never read or write the
	// developer's real <state>/gg/prompts.toml (see promptTestModel).
	m.promptStore = promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	m.conflict = domain.ConflictState{Op: "merge", Source: "feature", Target: "main"}
	m.status.Files = []model.FileStatus{{Path: "a.go", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'}}
	m2, _ := startConflictProcess(m)
	p, ok := m2.proc.(*conflictProcess)
	if !ok {
		t.Fatal("conflict process did not open")
	}
	return m2, p
}

func TestConflictTKeyOpensPicker(t *testing.T) {
	m, p := conflictModelWithTools(t,
		config.ToolCommand{Category: "conflict", Name: "Agent", Mode: "terminal", Command: "a <op>"})
	m, _ = p.update(m, keyRunes("t"))
	if p.st != confToolPick {
		t.Fatalf("state = %v, want confToolPick", p.st)
	}
	if len(p.toolChoices) != 1 || p.toolChoices[0].Name != "Agent" {
		t.Errorf("choices = %+v", p.toolChoices)
	}
	// esc returns to the list.
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if p.st != confListing {
		t.Errorf("esc: state = %v, want confListing", p.st)
	}
	_ = m
}

func TestConflictTKeyNoCommandsIsNoop(t *testing.T) {
	m, p := conflictModelWithTools(t) // no commands configured
	m, _ = p.update(m, keyRunes("t"))
	if p.st != confListing {
		t.Errorf("t with zero commands must stay in listing, got %v", p.st)
	}
	if m.statusMsg == "" {
		t.Error("expected a status hint about configuring tools")
	}
}

func TestConflictHintsAdvertiseTools(t *testing.T) {
	files := []model.FileStatus{{Path: "a.go", Staged: 'U', Unstaged: 'U'}}
	withTools := conflictHints(files, 0, "merge", 1)
	found := false
	for _, h := range withTools {
		if h == "[t] tools" {
			found = true
		}
	}
	if !found {
		t.Errorf("hints missing [t] tools: %v", withTools)
	}
	for _, h := range conflictHints(files, 0, "merge", 0) {
		if h == "[t] tools" {
			t.Error("[t] shown with zero commands")
		}
	}
}

func TestToolPickEnterResolvesAndAsksApproval(t *testing.T) {
	m, p := conflictModelWithTools(t,
		config.ToolCommand{Category: "conflict", Name: "Agent", Mode: "terminal", Command: `agent "<op> <conflicted-files>"`})
	m.currentWorktree = "/work/repo"
	m, _ = p.update(m, keyRunes("t"))
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if p.st != confToolApprove {
		t.Fatalf("state = %v, want confToolApprove (repo-level command needs no quartet)", p.st)
	}
	if p.pending == nil || p.pending.resolved != `agent "merge a.go"` {
		t.Fatalf("pending = %+v", p.pending)
	}
	if cmd != nil {
		t.Error("no async work expected for a repo-level command")
	}
	// Every run gets a per-run context file; its path rides in pending.cleanup.
	var ctxFile string
	for _, f := range p.pending.cleanup {
		if strings.Contains(filepath.Base(f), "gg-context-") {
			ctxFile = f
		}
	}
	if ctxFile == "" {
		t.Fatalf("pending.cleanup = %v, want a gg-context-* file", p.pending.cleanup)
	}
	if _, err := os.Stat(ctxFile); err != nil {
		t.Errorf("context file must exist while pending: %v", err)
	}
	// esc cancels without approving.
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if p.st != confListing || p.pending != nil {
		t.Errorf("esc must clear the pending run: st=%v pending=%v", p.st, p.pending)
	}
	if _, err := os.Stat(ctxFile); !os.IsNotExist(err) {
		t.Errorf("esc must remove the context file, stat err = %v", err)
	}
}

func TestToolApproveEnterReturnsExecCmd(t *testing.T) {
	m, p := conflictModelWithTools(t,
		config.ToolCommand{Category: "conflict", Name: "Agent", Mode: "terminal", Command: "true"})
	cleanupToolTemp(t) // the approve-enter below eagerly writes a real gg-context-* file and a gg-tool-* script
	m, _ = p.update(m, keyRunes("t"))
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // → approve
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("approving must return the ExecProcess command")
	}
	_ = m
}

// TestToolApprovedFastPathSkipsGate covers gateOrRun's already-approved
// branch: a command pre-approved for this repo key must skip confToolApprove
// entirely and go straight to the exec cmd (runPending). The unapproved path
// (→ confToolApprove) is already covered by
// TestToolPickEnterResolvesAndAsksApproval.
func TestToolApprovedFastPathSkipsGate(t *testing.T) {
	tc := config.ToolCommand{Category: "conflict", Name: "Agent", Mode: "terminal", Command: "true"}
	m, p := conflictModelWithTools(t, tc)
	cleanupToolTemp(t) // the approved fast path below eagerly writes a real gg-context-* file and a gg-tool-* script
	if err := m.promptStore.ApproveToolCommand(m.toolRepoKey(), toolCommandHash(tc.Command)); err != nil {
		t.Fatal(err)
	}
	m, _ = p.update(m, keyRunes("t"))
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if p.st == confToolApprove {
		t.Fatalf("pre-approved command must skip the gate, got state %v", p.st)
	}
	if cmd == nil {
		t.Fatal("approved fast path must return the exec cmd")
	}
	_ = m
}

func TestToolUserFillStepPrecedesApproval(t *testing.T) {
	m, p := conflictModelWithTools(t,
		config.ToolCommand{Category: "conflict", Name: "Agent", Mode: "terminal", Command: "agent <user:hint>"})
	cleanupToolTemp(t) // buildToolRun (after the fill below) eagerly writes a real gg-context-* file (and, once approved, a gg-tool-* script)
	m, _ = p.update(m, keyRunes("t"))
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if p.st != confToolFill || p.toolFill == nil {
		t.Fatalf("state = %v, want confToolFill", p.st)
	}
	for _, r := range "go" {
		m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter}) // last field → done
	if p.st != confToolApprove || p.pending == nil || p.pending.resolved != "agent go" {
		t.Fatalf("after fill: st=%v pending=%+v", p.st, p.pending)
	}
	_ = m
}

func TestToolMarkResolvedOffer(t *testing.T) {
	// A finished per-file run whose merged file changed (mtime moved past the
	// snapshot) must offer mark-resolved; an unchanged one must reload instead.
	m, p := conflictModelWithTools(t,
		config.ToolCommand{Category: "conflict", Name: "Meld", Mode: "terminal", PerFile: true, Command: "true"})
	f := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(f)
	// preMtime deliberately BEFORE the file's mtime = "the tool wrote it".
	pre := fi.ModTime().Add(-2 * time.Second)
	pending := &pendingToolRun{tc: config.ToolCommand{PerFile: true}, file: "a.go", merged: f}
	m2, _ := p.toolFinished(m, toolFinishedMsg{pending: pending, preMtime: pre})
	if p.st != confToolMark {
		t.Fatalf("changed merged file must offer mark-resolved, got %v", p.st)
	}
	_ = m2

	// Unchanged file (preMtime after the mtime): no offer, reload command instead.
	m3, p3 := conflictModelWithTools(t,
		config.ToolCommand{Category: "conflict", Name: "Meld", Mode: "terminal", PerFile: true, Command: "true"})
	post := fi.ModTime().Add(2 * time.Second)
	_, cmd := p3.toolFinished(m3, toolFinishedMsg{pending: pending, preMtime: post})
	if p3.st == confToolMark {
		t.Fatal("unchanged merged file must not offer mark-resolved")
	}
	if cmd == nil {
		t.Error("unchanged path must reload status")
	}
}
