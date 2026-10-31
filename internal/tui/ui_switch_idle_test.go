package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gitwatch"
)

func TestBlockedNoRefresh(t *testing.T) {
	m, _ := blockedModel(t)
	m.cfg.Refresh.Enabled = true
	m.cfg.Refresh.Status = 1
	m.refreshLastRun = nil
	nm, cmd := m.refreshTick(time.Now().Add(time.Hour))
	if cmd != nil || len(nm.bgQueue) != 0 {
		t.Fatalf("a blocked terminal scheduled work: queue %v", nm.bgQueue)
	}
	if !nm.refreshSuppressed() {
		t.Fatal("refresh must be suppressed while blocked")
	}
}

func TestBlockedWatchEventRearmsWithoutReading(t *testing.T) {
	m, _ := blockedModel(t)
	m.cfg.Refresh.Enabled = true
	m.cfg.Refresh.BranchesWatch = true
	m.watchSupported = true
	w, err := gitwatch.New(gitwatch.Plan(t.TempDir(), t.TempDir(), []gitwatch.Source{gitwatch.Branches}), time.Millisecond)
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	m.watcher = w
	got, cmd := updated(t, m, watchEventMsg{gen: m.watchGen, source: srcBranches})
	if len(got.bgQueue) != 0 {
		t.Fatalf("queued while blocked: %v", got.bgQueue)
	}
	if cmd == nil {
		t.Fatal("the listener must be re-armed")
	}
}

func TestActivationRefreshes(t *testing.T) {
	m, _ := blockedModel(t)
	m, cmd := m.activateTUI()
	if cmd == nil || len(m.srcInflight) == 0 {
		t.Fatalf("activation must read everything: cmd %v inflight %v", cmd != nil, m.srcInflight)
	}
}

func TestBlockedTaskResultWaits(t *testing.T) {
	m, _ := blockedModel(t)
	var applied int
	applyTasksHookForTest = func() { applied++ }
	t.Cleanup(func() { applyTasksHookForTest = nil })
	m, cmd := updated(t, m, tasksChangedMsg{})
	if applied != 0 {
		t.Fatal("a result applied behind the block")
	}
	if cmd == nil {
		t.Fatal("the task wait must be re-armed")
	}
	m, _ = m.activateTUI()
	if applied != 1 {
		t.Fatalf("applied %d times on activation", applied)
	}
}

func TestWebLaunchBlockedFromFirstFrame(t *testing.T) {
	installFakeHost(t)
	m := loadedModel(t).applyWebLaunch(webLaunchOptions{Web: true})
	if !m.webActive || !strings.Contains(m.View(), "starting…") {
		t.Fatalf("active %v view:\n%s", m.webActive, m.View())
	}
	if cmd := m.startupWebCmd(); cmd == nil {
		t.Fatal("--web must start the page")
	}
}

func TestWebLaunchWithoutASeamStaysTUI(t *testing.T) {
	if m := loadedModel(t).applyWebLaunch(webLaunchOptions{Web: true}); m.webActive {
		t.Fatal("no page can ever start: the terminal must not block")
	}
}

func TestWebLaunchOpensBrowser(t *testing.T) {
	f := installFakeHost(t)
	m := loadedModel(t).applyWebLaunch(webLaunchOptions{Web: true})
	m = runOne(t, m, m.startupWebCmd())
	if f.opens != 1 || !f.active[len(f.active)-1] || !m.webActive {
		t.Fatalf("opens %d active %v", f.opens, f.active)
	}
}

func TestWebLaunchPortTaken(t *testing.T) {
	f := installFakeHost(t)
	f.startErr = errors.New("address already in use")
	m := loadedModel(t).applyWebLaunch(webLaunchOptions{Web: true})
	m = runOne(t, m, m.startupWebCmd())
	if m.webActive || strings.Contains(m.View(), "Web UI is active") {
		t.Fatal("a port in use must leave the terminal active")
	}
	if !strings.Contains(m.statusMsg, "address already in use") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestWebServeLeavesTUIActive(t *testing.T) {
	f := installFakeHost(t)
	m := loadedModel(t).applyWebLaunch(webLaunchOptions{})
	m.cfg.Web.Serve = true
	m = runOne(t, m, m.startupWebCmd())
	if m.webActive || f.starts != 1 || f.opens != 0 || f.active[len(f.active)-1] {
		t.Fatalf("active %v starts %d opens %d told %v", m.webActive, f.starts, f.opens, f.active)
	}
}

func TestLaunchPromptsWait(t *testing.T) {
	m, _ := blockedModel(t)
	var cmd tea.Cmd
	m.repoHealthKnown = false
	m, cmd = m.applyRepoHealth(repoHealthMsg{gen: m.noticeGen})
	if cmd != nil || m.topLayer() != nil || m.modal != nil || m.repoHealthKnown {
		t.Fatal("no prompt behind the block: the health result waits for activation")
	}
	m, cmd = m.activateTUI()
	sawHealth := false
	for _, msg := range runLeaves(cmd) {
		if _, ok := msg.(repoHealthMsg); ok {
			sawHealth = true
		}
	}
	if !sawHealth {
		t.Fatal("activation must probe again, which raises the prompts")
	}
}

func TestBlockedHidesAModal(t *testing.T) {
	m, _ := blockedModel(t)
	m, _ = m.confirmReleaseClaim("/x/y", "agent"), tea.Cmd(nil)
	if !strings.Contains(m.View(), "Web UI is active") {
		t.Fatal("a modal must never show over the block, where no key reaches it")
	}
}

func TestPageSwitchWhileBlocked(t *testing.T) {
	m, _ := blockedModel(t)
	m = m.openWebSettings() // a stale window under the block must not refuse the page
	reply := make(chan error, 1)
	m, _ = m.onWebSwitchRequest(webSwitchRequestMsg{path: m.currentWorktree, reply: reply})
	select {
	case err := <-reply:
		if err != nil {
			t.Fatalf("refused: %v", err)
		}
	default: // the answer waits on the host's follow: not refused
	}
	if !strings.Contains(m.View(), "Web UI is active") {
		t.Fatal("still blocked")
	}
}

func TestPageStartsSessionWhileBlocked(t *testing.T) {
	m, _ := blockedModel(t)
	reply := make(chan webSessionReply, 1)
	m, _ = updated(t, m, webSessionStartedMsg{id: domain.SessionID("s1"), name: "x", reply: reply})
	if r := <-reply; r.err != nil || r.id != "s1" {
		t.Fatalf("reply = %+v", r)
	}
	if !strings.Contains(m.View(), "Web UI is active") {
		t.Fatal("still blocked")
	}
}
