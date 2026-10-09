package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/repos"
)

// emergencyUnlockKey is the one key that reaches the interface from any
// surface, while anything holds it: a read that never came back keeps
// "⏳ reloading…" — and every action gate on m.loading — up for good, and an
// operation whose git hangs keeps m.running. Sits beside ctrl+o, above the
// process and layer routing, with no opsIdle gate (that is the point).
const emergencyUnlockKey = "alt+A"

// endGitProcesses is domain.EndGitProcesses behind a seam: the tui tests run
// in parallel in one process, so TestMain swaps in a counter that kills no
// other test's git.
var endGitProcesses = domain.EndGitProcesses

// locked reports whether the interface is held: a reload, an initial or
// repo-switch load, or an operation. The footer advertises alt+A only then.
func (m Model) locked() bool {
	return m.running || m.loading || m.anySourceLoading()
}

// unlockHintAfter is how long a lock lasts before the status line offers
// alt+A: long enough that an ordinary reload or op never flashes it.
const (
	unlockHintAfterReload = 5 * time.Second
	unlockHintAfterOp     = 10 * time.Second
)

// unlockHint is the status line's pointer at alt+A once a lock has outlived
// its normal span ("" before that, and when nothing holds the interface).
// The perpetual heartbeat repaints the line, so it appears on its own.
func (m Model) unlockHint(now time.Time) string {
	if m.quiet {
		return "" // headless golden screens pin rendering, not wall-clock waits
	}
	if m.running {
		if !m.opStart.IsZero() && clock.Since(m.opStart) >= unlockHintAfterOp {
			return i18n.T("[alt+A] stop it")
		}
		return ""
	}
	if !m.anySourceLoading() {
		return ""
	}
	for s, on := range m.srcLoading {
		if t := m.srcSince[s]; on && !t.IsZero() && now.Sub(t) >= unlockHintAfterReload {
			return i18n.T("[alt+A] unlock")
		}
	}
	return ""
}

// emergencyUnlock writes a state dump first (what was stuck, while it is
// still stuck), then — only when something holds the interface — releases it:
//   - every source read in flight is abandoned — its generation bumped, so the
//     late result is dropped rather than re-locking — and the background lane
//     is preempted the way a user op preempts it;
//   - a pending full load (startup, repo switch) is abandoned the same way;
//   - a running operation is asked to stop and its open decision answered
//     abort. It is NOT marked finished here: it still holds its repo
//     reservation, so its Done is what ends it;
//   - every git subprocess still running is ended (SIGTERM, so git frees its
//     lockfiles): a hung read keeps its coalesced flight, its git slot and its
//     repo reservation, so without this the next r would only join it.
//
// With nothing held it only writes the dump.
func (m Model) emergencyUnlock() (Model, tea.Cmd) {
	now := time.Now()
	path, derr := m.writeStateDump(now)

	held := m.locked() || m.anySourceInflight()
	reads := 0
	if m.loading || m.anySourceLoading() || m.anySourceInflight() {
		if m.srcGen == nil {
			m.srcGen = map[sourceKey]int{}
		}
		for s := sourceKey(0); s < srcCount; s++ {
			if m.srcLoading[s] || m.srcInflight[s] {
				m.srcGen[s]++
				reads++
			}
		}
		m.srcLoading = map[sourceKey]bool{}
		m.srcInflight = map[sourceKey]bool{}
		if m.loading && reads == 0 {
			m.loadGen++ // the full load (dataLoadedMsg) is what held it
			reads++
		}
		m.loading = false
		m.commitsLoading = false
	}
	stopping := false
	if m.running {
		if m.modal != nil && m.modal.onResolve == nil {
			// The op's own decision: answer it abort (reply is buffered, so
			// this never blocks even if the op is already gone).
			m.modal.reply <- engine.DecisionResponse{Option: abortOption(m.modal.req.Options)}
			m.modal = nil
		}
		if m.opCancel != nil {
			m.opCancel()
			stopping = true
		}
	}
	gits := 0
	if held {
		if m.bgCancel != nil {
			m.bgCancel()
			m.bgCancel = nil
		}
		m.bgBusy = false
		m.bgQueue = nil
		gits = endGitProcesses()
	}

	var parts []string
	if reads > 0 {
		parts = append(parts, i18n.T("unlocked — abandoned %d stuck reads (r reloads)", reads))
	}
	switch {
	case stopping:
		parts = append(parts, i18n.T("asked the running operation to stop"))
	case m.running:
		parts = append(parts, i18n.T("the running operation cannot be stopped from here — it ends when its work does"))
	}
	if gits > 0 {
		parts = append(parts, i18n.T("ended %d git processes", gits))
	}
	if derr != nil {
		parts = append(parts, i18n.T("state dump failed: %s", derr.Error()))
	} else {
		m.lastStateDump = path
		parts = append(parts, i18n.T("state dump: %s", path))
	}
	m.statusMsg = strings.Join(parts, " · ")
	// [E] opens the untruncated line: the dump path comes last, and the bar
	// cuts from the back.
	m.lastError = m.statusMsg
	return m, nil
}

// stateDumpDir is <state>/gg/dumps, beside errors.log and operations.log.
func stateDumpDir() string {
	sp := repos.DefaultStatePath()
	if sp == "" {
		return os.TempDir()
	}
	return filepath.Join(filepath.Dir(sp), "dumps")
}

// writeStateDump writes the TUI's own flags, then the process-wide state
// (running gits, repo reservations, failures, goroutines) to a new file.
func (m Model) writeStateDump(now time.Time) (string, error) {
	dir := stateDumpDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "state-"+now.Format("20060102-150405.000")+".txt")
	var b strings.Builder
	fmt.Fprintf(&b, "gg emergency state dump · %s\n", now.Format(time.RFC3339))
	m.writeTUIState(&b, now)
	domain.WriteProcessState(&b, now)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// writeTUIState is the dump's TUI section: what gates actions and why.
func (m Model) writeTUIState(b *strings.Builder, now time.Time) {
	fmt.Fprintln(b, "\n== tui ==")
	fmt.Fprintf(b, "worktree: %s\n", m.currentWorktree)
	fmt.Fprintf(b, "home: %s\nviewed: %s\npending return: %s\n", m.homeWorktree(), m.viewPath(m.viewed), m.viewPath(m.pendingReturnView))
	fmt.Fprintf(b, "running: %v  op: %q", m.running, m.opName)
	if m.running && !m.opStart.IsZero() {
		fmt.Fprintf(b, "  for %s", now.Sub(m.opStart).Round(100*time.Millisecond))
	}
	fmt.Fprintln(b)
	fmt.Fprintf(b, "loading: %v  ready: %v  loadGen: %d  commitsLoading: %v\n", m.loading, m.ready, m.loadGen, m.commitsLoading)
	fmt.Fprintf(b, "background lane: busy=%v item=%+v queued=%d\n", m.bgBusy, m.bgActiveItem, len(m.bgQueue))
	fmt.Fprintf(b, "modal: %v  proc: %v  action menu: %v  top layer: %s\n",
		m.modal != nil, m.proc != nil, m.actionMenu != nil, typeName(m.topLayer()))
	fmt.Fprintf(b, "status: %s\n", m.statusMsg)
	fmt.Fprintln(b, "sources (gen · in flight · manual ⏳ · age):")
	for s := sourceKey(0); s < srcCount; s++ {
		age := ""
		if m.srcInflight[s] {
			if t := m.srcSince[s]; !t.IsZero() {
				age = now.Sub(t).Round(100 * time.Millisecond).String()
			}
		}
		fmt.Fprintf(b, "  %-10s gen %-4d inflight %-5v loading %-5v %s\n",
			sourceNames[s], m.srcGen[s], m.srcInflight[s], m.srcLoading[s], age)
	}
}

func typeName(v any) string {
	if v == nil {
		return "none"
	}
	return reflect.TypeOf(v).String()
}
