package tui

import (
	"maps"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
)

// endGitCalls counts the (stubbed, see TestMain) alt+U git kills.
var endGitCalls atomic.Int64

// sourceMsgs runs a reload command and returns the messages it produced,
// without feeding them back (the caller lands them in the order it needs).
func sourceMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a reload command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range batch {
		if c != nil {
			out = append(out, c())
		}
	}
	return out
}

func landAll(m Model, msgs []tea.Msg) Model {
	for _, msg := range msgs {
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}
	return m
}

// A silent read (an agent session starting, a claim released, a steer
// refresh) that supersedes a manual read of the same source must not strand
// "⏳ reloading…": the manual result is dropped on the gen check before it
// clears srcLoading, so the silent read has to clear it — for every source,
// not only the previews read that once had a hand-written workaround.
func TestSilentReadSupersedingManualDoesNotStrandLoading(t *testing.T) {
	t.Parallel()
	for _, s := range []sourceKey{srcStatus, srcWorktrees, srcBranches, srcNotes} {
		m := newTestModel(t)
		m.loading = false
		m, manual := m.reloadSourcesCmd([]sourceKey{s}, reloadOpts{manual: true})
		if !m.loading {
			t.Fatalf("source %d: a manual read must lock actions", s)
		}
		m, silent := m.reloadSourcesCmd([]sourceKey{s}, reloadOpts{})
		manualMsgs, silentMsgs := sourceMsgs(t, manual), sourceMsgs(t, silent)
		m = landAll(m, manualMsgs) // stale now: dropped on the gen check
		m = landAll(m, silentMsgs)
		if m.loading || m.srcLoading[s] || !m.opsIdle() {
			t.Fatalf("source %d: the superseding read must clear the lock: loading=%v srcLoading=%v",
				s, m.loading, m.srcLoading)
		}
	}
}

// alt+U is the emergency unlock: a read that never comes back (a hung git
// on a monorepo) holds "⏳ reloading…" and every action gate forever. The key
// clears the lock from any screen, and the hung read landing late must not
// lock the interface again.
func TestEmergencyUnlockClearsAStuckReload(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.loading = false
	m, hung := m.reloadSourcesCmd([]sourceKey{srcStatus, srcFeed}, reloadOpts{manual: true})
	if m.opsIdle() {
		t.Fatal("precondition: a manual reload locks actions")
	}
	m = m.pushLayer(&compareLoadingPopup{tag: "x", subject: "y"}) // any screen
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U"), Alt: true})
	m = updated.(Model)
	if endGitCalls.Load() == 0 {
		t.Fatal("a locked alt+U must end the git processes a hung read holds")
	}
	if !m.opsIdle() || m.anySourceLoading() || m.anySourceInflight() {
		t.Fatalf("alt+U must unlock: loading=%v srcLoading=%v inflight=%v",
			m.loading, m.srcLoading, m.srcInflight)
	}
	if m.statusMsg == "" {
		t.Fatal("alt+U must say what it did")
	}
	m = landAll(m, sourceMsgs(t, hung)) // the hung reads finally return
	if !m.opsIdle() || m.anySourceLoading() {
		t.Fatal("a late read from before the unlock must not lock the interface again")
	}
}

// alt+U on a running operation asks it to stop (git gets SIGTERM and frees its
// lockfiles) rather than pretending it ended: the op still holds its repo
// reservation until its Done arrives.
func TestEmergencyUnlockCancelsARunningOp(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.loading = false
	cancelled := false
	m.running = true
	m.opCancel = func() { cancelled = true }
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U"), Alt: true})
	m = updated.(Model)
	if !cancelled {
		t.Fatal("alt+U must cancel the running operation")
	}
	if m.statusMsg == "" {
		t.Fatal("alt+U must say what it did")
	}
}

// With nothing held, alt+U only writes the state dump: no source generation
// moves, so no read in flight is thrown away.
func TestEmergencyUnlockIdleOnlyDumps(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.loading = false
	gens := maps.Clone(m.srcGen)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U"), Alt: true})
	m = updated.(Model)
	if !maps.Equal(gens, m.srcGen) {
		t.Fatalf("idle alt+U must not abandon reads: %v → %v", gens, m.srcGen)
	}
	if !strings.Contains(m.statusMsg, m.lastStateDump) {
		t.Fatalf("the status line must name the dump: %q", m.statusMsg)
	}
	data, err := os.ReadFile(m.lastStateDump)
	if err != nil {
		t.Fatalf("status %q must name a written dump: %v", m.statusMsg, err)
	}
	for _, want := range []string{"== tui ==", "== git subprocesses", "== repo reservations ==", "== goroutines ==", "sources ("} {
		if !strings.Contains(string(data), want) {
			t.Errorf("dump lacks %q", want)
		}
	}
}

// The dump of a stuck reload names the stuck sources — captured BEFORE the
// unlock clears them.
func TestEmergencyDumpRecordsTheStuckState(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.loading = false
	m, _ = m.reloadSourcesCmd([]sourceKey{srcStatus}, reloadOpts{manual: true})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U"), Alt: true})
	m = updated.(Model)
	data, err := os.ReadFile(m.lastStateDump)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`status\s+gen \d+\s+inflight true\s+loading true`).Match(data) {
		t.Fatalf("dump must show the stuck status read:\n%s", data[:min(len(data), 1500)])
	}
}

// A reload that outlives its normal span points at alt+U; a short one never
// flashes the hint.
func TestUnlockHintAppearsOnlyOnAStuckReload(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.loading = false
	m, _ = m.reloadSourcesCmd([]sourceKey{srcStatus}, reloadOpts{manual: true})
	now := time.Now()
	if h := m.unlockHint(now); h != "" {
		t.Fatalf("a fresh reload must not offer alt+U: %q", h)
	}
	if h := m.unlockHint(now.Add(unlockHintAfterReload)); h == "" {
		t.Fatal("a reload stuck past the threshold must offer alt+U")
	}
	m.srcSince[srcStatus] = now.Add(-time.Minute)
	if !strings.Contains(m.footerLine(), "alt+U") {
		t.Fatalf("the footer must offer alt+U on a stuck reload: %q", m.footerLine())
	}
}

// alt+U on an op parked on its own decision answers it abort, so the dead op
// never leaves a modal behind (the reply is buffered: this cannot block).
func TestEmergencyUnlockAbortsTheOpsDecision(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.loading = false
	m.running = true
	m.opCancel = func() {}
	reply := make(chan engine.DecisionResponse, 1)
	m.modal = &decisionState{req: engine.DecisionRequest{Options: []string{"continue", "abort"}}, reply: reply}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U"), Alt: true})
	m = updated.(Model)
	if m.modal != nil {
		t.Fatal("the op's decision must close")
	}
	select {
	case r := <-reply:
		if r.Option != "abort" {
			t.Fatalf("answered %q, want abort", r.Option)
		}
	default:
		t.Fatal("the op's decision must be answered")
	}
}

// An op without a cancel cannot be stopped; alt+U says so rather than imply
// the interface is free.
func TestEmergencyUnlockSaysAnUnstoppableOpRuns(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.loading = false
	m.running = true
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U"), Alt: true})
	m = updated.(Model)
	if !strings.Contains(m.statusMsg, "cannot be stopped") || m.lastError != m.statusMsg {
		t.Fatalf("status = %q, lastError = %q", m.statusMsg, m.lastError)
	}
}

// The unlock is alt+shift+a: a lowercase alt+a (the agent cycle) never fires
// it, so a stray keypress cannot abandon reads or end gits.
func TestLowercaseAltANeverUnlocks(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.loading = false
	m, _ = m.reloadSourcesCmd([]sourceKey{srcStatus}, reloadOpts{manual: true})
	before := endGitCalls.Load()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a"), Alt: true})
	m = updated.(Model)
	if m.opsIdle() || m.lastStateDump != "" || endGitCalls.Load() != before {
		t.Fatal("alt+a must not unlock")
	}
}

// alt+A no longer unlocks: it walks the viewed worktree's agents (the
// session window keys, 2026-10-10). alt+U is the unlock.
func TestAltShiftAIsNotTheUnlockAnyMore(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.loading = false
	m, _ = m.reloadSourcesCmd([]sourceKey{srcStatus}, reloadOpts{manual: true})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A"), Alt: true})
	m = updated.(Model)
	if m.lastStateDump != "" {
		t.Fatal("alt+A wrote a state dump: it is no longer the emergency unlock")
	}
}
