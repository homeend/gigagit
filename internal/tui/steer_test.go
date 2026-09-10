package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/steer"
)

// steerModel is a Model wired to a real temp repo with its inbox injected —
// the seam that lets every steering test run with t.Parallel() and no
// t.Setenv (which panics in a parallel test).
func steerModel(t *testing.T) (Model, string) {
	t.Helper()
	m := newTestModel(t)
	dir := filepath.Join(t.TempDir(), "steer")
	m.steerDir = dir
	m.snapshotWorktree = t.TempDir() // run.go sets this via initSnapshotTarget; the presence file carries it
	m.cfg = config.Defaults()
	// New() sets loading:true and opsIdle() is !running && !loading — without
	// this every steering command would be refused with "an operation is
	// running" and no ok:true test could pass.
	m.loading = false
	return m, dir
}

func TestSteerActiveFollowsDirAndConfig(t *testing.T) {
	t.Parallel()
	m, _ := steerModel(t)
	if !m.steerActive() {
		t.Fatal("a resolved dir plus the default config must be active")
	}
	off := m
	off.cfg.UI.AgentSteering = "off"
	if off.steerActive() {
		t.Error(`agent_steering = "off" must disable the consumer`)
	}
	none := m
	none.steerDir = ""
	if none.steerActive() {
		t.Error("an unresolved dir must disable the consumer")
	}
}

func TestInitSteerInboxWritesPresenceAndDiscardsLeftovers(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	if _, err := steer.Post(dir, steer.Command{Cmd: "focus", Panel: "files"}); err != nil {
		t.Fatal(err)
	}
	m = m.initSteerInbox()
	if got := steer.Drain(dir); len(got) != 0 {
		t.Errorf("startup left %d command(s) behind; a crashed session's inbox must be swept", len(got))
	}
	p, ok := steer.Live(dir, steer.TUIPresence)
	if !ok {
		t.Fatal("no live presence after initSteerInbox")
	}
	if p.PID != os.Getpid() || p.Worktree == "" {
		t.Errorf("presence = %+v, want this pid and a worktree", p)
	}
	if p.Started == "" {
		t.Error("presence must carry an RFC3339 start time")
	}
	if _, err := time.Parse(time.RFC3339, p.Started); err != nil {
		t.Errorf("started = %q, not RFC3339: %v", p.Started, err)
	}
}

func TestInitSteerInboxIsANoOpWhenSteeringIsOff(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m.cfg.UI.AgentSteering = "off"
	m = m.initSteerInbox()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("steering off must not even create the inbox dir (err = %v)", err)
	}
}

func TestCloseSteerInboxRemovesPresence(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m = m.closeSteerInbox()
	if _, ok := steer.Live(dir, steer.TUIPresence); ok {
		t.Error("presence survived closeSteerInbox — a dead session must not look live")
	}
	if m.steerDir != "" || m.steerWatch != nil {
		t.Errorf("closeSteerInbox left steerDir=%q watch=%v", m.steerDir, m.steerWatch)
	}
}

func TestSteerRefusalRules(t *testing.T) {
	t.Parallel()
	base, _ := steerModel(t)
	base.ready = true

	cases := []struct {
		name string
		mut  func(m Model) Model
	}{
		{"op running", func(m Model) Model { m.running = true; return m }},
		{"loading", func(m Model) Model { m.loading = true; return m }},
		{"decision modal", func(m Model) Model { m.modal = &decisionState{}; return m }},
		{"action menu", func(m Model) Model { m.actionMenu = &actionMenu{}; return m }},
		{"panel / filter", func(m Model) Model { m.filterTyping = true; return m }},
		{"panel @ highlight", func(m Model) Model { m.highlightTyping = true; return m }},
		{"search-history dropdown", func(m Model) Model { m.recallOpen = true; return m }},
		{"files-view filter", func(m Model) Model { m.filesView = &contentPopup{typing: true}; return m }},
		{"stash-view filter", func(m Model) Model { m.stashView = &stashView{typing: true}; return m }},
		{"hunk picker", func(m Model) Model { return m.pushLayer(&hunkPicker{}) }},
		{"rebase editor", func(m Model) Model { return m.pushLayer(&irebaseEditor{}) }},
		{"repo switcher", func(m Model) Model { return m.pushLayer(&repoPopup{}) }},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := c.mut(base).steerRefusal(); got == "" {
				t.Errorf("%s must refuse a steering command", c.name)
			}
		})
	}

	if got := base.steerRefusal(); got != "" {
		t.Errorf("an idle model on the panels refused with %q", got)
	}
	// A diff view is POPPED to reach the panels, never a refusal.
	if got := base.pushLayer(&diffView{}).steerRefusal(); got != "" {
		t.Errorf("an open diff must not refuse a command, got %q", got)
	}
}

func TestDrainAnswersAnUnknownCommand(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	id, err := steer.Post(dir, steer.Command{Cmd: "teleport", Wait: true})
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := m.drainSteer()
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(dir, id, time.Second)
	if !ok {
		t.Fatal("no reply for an unknown command")
	}
	if r.OK || !strings.Contains(r.Error, "unknown command") {
		t.Fatalf("reply = %+v, want ok:false naming the unknown command (not a refusal for some other reason)", r)
	}
	_ = m
}

func TestDrainRefusesWhileAnOpRuns(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.running = true
	id, err := steer.Post(dir, steer.Command{Cmd: "focus", Panel: "files", Wait: true})
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := m.drainSteer()
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(dir, id, time.Second)
	if !ok {
		t.Fatal("a refusal must still be answered — never leave the CLI hanging")
	}
	if r.OK {
		t.Fatalf("reply = %+v, want ok:false while an op runs", r)
	}
	_ = m
}

func TestDrainWritesNoReplyForANoWaitCommand(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	id, err := steer.Post(dir, steer.Command{Cmd: "teleport"}) // Wait false
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := m.drainSteer()
	runSteerCmd(t, cmd)
	if _, err := os.Stat(filepath.Join(dir, "reply-"+id+".json")); !os.IsNotExist(err) {
		t.Errorf("a no-wait command must get no reply file (err = %v)", err)
	}
	_ = m
}

func TestHeartbeatTouchesPresenceAndDrains(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	p := filepath.Join(dir, steer.TUIPresence)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	id, err := steer.Post(dir, steer.Command{Cmd: "teleport", Wait: true})
	if err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.Update(heartbeatMsg{})
	if _, ok := tm.(Model); !ok {
		t.Fatalf("Update returned %T", tm)
	}
	runSteerCmd(t, cmd)
	if _, ok := steer.Live(dir, steer.TUIPresence); !ok {
		t.Error("the heartbeat must re-touch the presence file")
	}
	if _, ok := steer.AwaitReply(dir, id, 2*time.Second); !ok {
		t.Error("the heartbeat must drain the inbox (the poll is the watcher's safety net)")
	}
}

// runSteerCmd executes a tea.Cmd tree far enough to run the reply writes,
// which are ordinary off-thread commands.
func runSteerCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			runSteerCmd(t, c)
		}
	}
}

func TestSteerPresenceJSONShape(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	data, err := os.ReadFile(filepath.Join(dir, steer.TUIPresence))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("presence is not valid JSON: %v", err)
	}
	for _, k := range []string{"pid", "worktree", "started"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("presence missing %q: %s", k, data)
		}
	}
}
