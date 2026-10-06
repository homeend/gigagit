package tui

import (
	"maps"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

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

// alt+u is the emergency unlock: a read that never comes back (a hung git
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
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u"), Alt: true})
	m = updated.(Model)
	if !m.opsIdle() || m.anySourceLoading() || m.anySourceInflight() {
		t.Fatalf("alt+u must unlock: loading=%v srcLoading=%v inflight=%v",
			m.loading, m.srcLoading, m.srcInflight)
	}
	if m.statusMsg == "" {
		t.Fatal("alt+u must say what it did")
	}
	m = landAll(m, sourceMsgs(t, hung)) // the hung reads finally return
	if !m.opsIdle() || m.anySourceLoading() {
		t.Fatal("a late read from before the unlock must not lock the interface again")
	}
}

// alt+u on a running operation asks it to stop (git gets SIGTERM and frees its
// lockfiles) rather than pretending it ended: the op still holds its repo
// reservation until its Done arrives.
func TestEmergencyUnlockCancelsARunningOp(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.loading = false
	cancelled := false
	m.running = true
	m.opCancel = func() { cancelled = true }
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u"), Alt: true})
	m = updated.(Model)
	if !cancelled {
		t.Fatal("alt+u must cancel the running operation")
	}
	if m.statusMsg == "" {
		t.Fatal("alt+u must say what it did")
	}
}

// With nothing held, alt+u only writes the state dump: no source generation
// moves, so no read in flight is thrown away.
func TestEmergencyUnlockIdleOnlyDumps(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.loading = false
	gens := maps.Clone(m.srcGen)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u"), Alt: true})
	m = updated.(Model)
	if !maps.Equal(gens, m.srcGen) {
		t.Fatalf("idle alt+u must not abandon reads: %v → %v", gens, m.srcGen)
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
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u"), Alt: true})
	m = updated.(Model)
	data, err := os.ReadFile(m.lastStateDump)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`status\s+gen \d+\s+inflight true\s+loading true`).Match(data) {
		t.Fatalf("dump must show the stuck status read:\n%s", data[:min(len(data), 1500)])
	}
}

// A reload that outlives its normal span points at alt+u; a short one never
// flashes the hint.
func TestUnlockHintAppearsOnlyOnAStuckReload(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.loading = false
	m, _ = m.reloadSourcesCmd([]sourceKey{srcStatus}, reloadOpts{manual: true})
	now := time.Now()
	if h := m.unlockHint(now); h != "" {
		t.Fatalf("a fresh reload must not offer alt+u: %q", h)
	}
	if h := m.unlockHint(now.Add(unlockHintAfterReload)); h == "" {
		t.Fatal("a reload stuck past the threshold must offer alt+u")
	}
	m.srcSince[srcStatus] = now.Add(-time.Minute)
	if !strings.Contains(m.footerLine(), "alt+u") {
		t.Fatalf("the footer must offer alt+u on a stuck reload: %q", m.footerLine())
	}
}
