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
	if m.steerDir != "" || m.steerWatch != nil || m.steerClaimed {
		t.Errorf("closeSteerInbox left steerDir=%q watch=%v claimed=%v", m.steerDir, m.steerWatch, m.steerClaimed)
	}
}

// A session that crashed or was SIGKILLed leaves its tui.json behind, and
// Touch only Chtimes an existing file — so without an explicit Remove first,
// the NEW session's presence would keep the DEAD one's pid and start time, and
// `gg session status` would print them as this session's.
func TestInitSteerInboxOverwritesACrashedSessionsPresence(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	if err := steer.Touch(dir, steer.TUIPresence, steer.Presence{
		PID: 424242, Worktree: "/gone", Started: "2020-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	m = m.initSteerInbox()
	p, ok := steer.Live(dir, steer.TUIPresence)
	if !ok {
		t.Fatal("no live presence after initSteerInbox")
	}
	if p.PID != os.Getpid() {
		t.Errorf("presence pid = %d, want this process (%d) — a crashed session's pid survived the claim", p.PID, os.Getpid())
	}
	if p.Started == "2020-01-01T00:00:00Z" || p.Worktree == "/gone" {
		t.Errorf("presence = %+v, want this session's own payload", p)
	}
}

// closeSteerInbox must also drop a parked navigate. The config-turned-off path
// (reconcileSteer) is the leak: after it, drainSteer is gated on steerActive()
// and can no longer expire the pending, so a parked status retry would fire on
// the next ORDINARY status refresh and move the user's view with no reply
// possible.
func TestCloseSteerInboxClearsAParkedPending(t *testing.T) {
	t.Parallel()
	m, _ := steerModel(t)
	m = m.initSteerInbox()
	m.pendingSteer = &pendingSteer{cmd: steer.Command{ID: "p-1", Cmd: "navigate", File: "a.txt"}, stage: steerStageStatusRetry, at: time.Now()}
	m.cfg.UI.AgentSteering = "off"
	m, _ = m.reconcileSteer()
	if m.pendingSteer != nil {
		t.Errorf("pending = %+v, want it dropped: nothing can expire it once steering is off", m.pendingSteer)
	}
}

// The TUI must refuse the same two wire enums the web endpoint's toSteerWire
// refuses, and refuse them ONCE, before dispatch — a silently defaulted
// target.state keys a band no diff can ever match (and answers ok:true for it),
// and a silently defaulted line.side makes an unknown side mean "new".
func TestSteerRefusesUnknownTargetStateAndLineSide(t *testing.T) {
	t.Parallel()
	base, dir := steerModel(t)
	base.ready = true

	for _, tc := range []struct {
		name string
		cmd  steer.Command
		want string
	}{
		{"highlight bad state", steer.Command{
			ID: "v-1", Cmd: "highlight", File: "a.txt", Target: &steer.Target{State: "index"},
			Side: "new", Start: 1, End: 2, Tone: "info", Wait: true,
		}, `unknown target state "index"`},
		{"navigate bad state", steer.Command{
			ID: "v-2", Cmd: "navigate", File: "a.txt", Target: &steer.Target{State: "HEAD"}, Wait: true,
		}, `unknown target state "HEAD"`},
		{"highlight_clear bad state", steer.Command{
			ID: "v-3", Cmd: "highlight_clear", File: "a.txt", Target: &steer.Target{State: "cached"}, Wait: true,
		}, `unknown target state "cached"`},
		{"navigate bad side", steer.Command{
			ID: "v-4", Cmd: "navigate", File: "a.txt", Line: &steer.Line{Side: "middle", No: 3}, Wait: true,
		}, `unknown side "middle"`},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, cmd := base.applySteer(tc.cmd)
			runSteerCmd(t, cmd)
			r, ok := steer.AwaitReply(dir, tc.cmd.ID, time.Second)
			if !ok || r.OK || r.Error != tc.want {
				t.Fatalf("reply = %+v ok=%v, want ok:false %q", r, ok, tc.want)
			}
			if len(m.attention) != 0 {
				t.Error("a refused command must record no band")
			}
		})
	}

	// The empty string is the documented default on both fields and must stay
	// accepted — the consumer already treats "" as unstaged / new.
	// Its own map: Model is a value but `attention` is a map, so a highlight
	// applied to `base` here would be visible to the parallel subtests above.
	okBase := base
	okBase.attention = map[attentionKey][]steerMark{}
	ok1 := steer.Command{ID: "v-5", Cmd: "highlight", File: "a.txt", Target: &steer.Target{}, Start: 1, Tone: "info", Wait: true}
	_, cmd := okBase.applySteer(ok1)
	runSteerCmd(t, cmd)
	if r, _ := steer.AwaitReply(dir, "v-5", time.Second); !r.OK {
		t.Errorf("reply = %+v, want ok:true — an empty target.state is the default, not an unknown value", r)
	}
}

func TestSteerRefusalRules(t *testing.T) {
	t.Parallel()
	base, _ := steerModel(t)
	base.ready = true

	// The reason string is protocol prose an agent reads, so every case pins the
	// EXACT text — a refusal that silently changed wording would still be a
	// refusal, and a non-empty check would never notice.
	cases := []struct {
		name string
		mut  func(m Model) Model
		want string
	}{
		{"op running", func(m Model) Model { m.running = true; return m }, "an operation is running"},
		{"loading", func(m Model) Model { m.loading = true; return m }, "an operation is running"},
		{"decision modal", func(m Model) Model { m.modal = &decisionState{}; return m }, "a decision is waiting for the user"},
		{"interactive process", func(m Model) Model { m.proc = &conflictProcess{st: confListing}; return m }, "an interactive process owns the screen"},
		{"action menu", func(m Model) Model { m.actionMenu = &actionMenu{}; return m }, "the action menu is open"},
		{"panel / filter", func(m Model) Model { m.filterTyping = true; return m }, "the user is typing"},
		{"panel @ highlight", func(m Model) Model { m.highlightTyping = true; return m }, "the user is typing"},
		{"search-history dropdown", func(m Model) Model { m.recallOpen = true; return m }, "the user is typing"},
		{"files-view filter", func(m Model) Model { m.filesView = &contentPopup{typing: true}; return m }, "the user is typing"},
		{"files-preview filter", func(m Model) Model { m.filesPreview = &contentPopup{typing: true}; return m }, "the user is typing"},
		{"stash-view filter", func(m Model) Model { m.stashView = &stashView{typing: true}; return m }, "the user is typing"},
		{"content popup filter", func(m Model) Model { return m.pushLayer(&contentPopup{typing: true}) }, "the user is typing"},
		{"hunk picker", func(m Model) Model { return m.pushLayer(&hunkPicker{}) }, "a window is open that owns the keyboard"},
		{"rebase editor", func(m Model) Model { return m.pushLayer(&irebaseEditor{}) }, "a window is open that owns the keyboard"},
		{"repo switcher", func(m Model) Model { return m.pushLayer(&repoPopup{}) }, "a window is open that owns the keyboard"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := c.mut(base).steerRefusal(); got != c.want {
				t.Errorf("%s refused with %q, want %q", c.name, got, c.want)
			}
		})
	}

	// A content popup with no filter focus is poppable, like a diff.
	if got := base.pushLayer(&contentPopup{}).steerRefusal(); got != "" {
		t.Errorf("an idle content popup must not refuse a command, got %q", got)
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
	if r.OK || r.Error != "an operation is running" {
		t.Fatalf("reply = %+v, want ok:false with %q", r, "an operation is running")
	}
	_ = m
}

// TestSteerReArmsWhenConfigTurnsItOn covers the repo switch from a
// steering-OFF repo into a steering-ON one. snapshotTargetMsg resolves the new
// inbox before the new repo's config lands, so initSteerInbox runs while m.cfg
// still says "off" and is inert: no Discard, no presence, no watcher. The cfg
// apply has to notice that and claim the inbox then. (The handler's own
// SessionSteerDir resolution goes through the XDG state home, which a parallel
// test cannot redirect without t.Setenv, so the test reproduces exactly the
// state that handler leaves behind: a resolved dir that was never claimed.)
func TestSteerReArmsWhenConfigTurnsItOn(t *testing.T) {
	t.Parallel()
	on := config.Defaults()
	sites := []struct {
		name string
		msg  func(m Model) tea.Msg
	}{
		{"configReadyMsg", func(m Model) tea.Msg { return configReadyMsg{cfg: on} }},
		{"dataLoadedMsg", func(m Model) tea.Msg { return dataLoadedMsg{gen: m.loadGen, cfg: on} }},
	}
	for _, s := range sites {
		s := s
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			m, dir := steerModel(t)
			m.cfg.UI.AgentSteering = "off"
			// A crashed session's leftover, waiting in the NEW repo's inbox.
			if _, err := steer.Post(dir, steer.Command{Cmd: "focus", Panel: "files"}); err != nil {
				t.Fatal(err)
			}
			// snapshotTargetMsg's half: the dir is resolved, the claim is inert.
			m = m.initSteerInbox()
			if m.steerClaimed {
				t.Fatal("steering was off — initSteerInbox must not claim the inbox")
			}

			tm, cmd := m.Update(s.msg(m))
			nm, ok := tm.(Model)
			if !ok {
				t.Fatalf("Update returned %T", tm)
			}
			if !nm.steerClaimed {
				t.Error("the config turning steering on must claim the inbox")
			}
			if cmd == nil {
				t.Error("the re-arm must batch a watcher-start command")
			}
			if got := steer.Drain(dir); len(got) != 0 {
				t.Errorf("the claim left %d leftover command(s) behind — they must be discarded, not replayed", len(got))
			}
			if _, live := steer.Live(dir, steer.TUIPresence); !live {
				t.Error("no live presence after the config turned steering on")
			}
		})
	}
}

// TestSteerCfgApplyDropsTheInboxWhenTurnedOff is the other direction: a repo
// whose config says off must not keep a presence a previous repo claimed.
func TestSteerCfgApplyDropsTheInboxWhenTurnedOff(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	if !m.steerClaimed {
		t.Fatal("initSteerInbox must claim the inbox when steering is on")
	}
	off := config.Defaults()
	off.UI.AgentSteering = "off"
	tm, _ := m.Update(configReadyMsg{cfg: off})
	nm, ok := tm.(Model)
	if !ok {
		t.Fatalf("Update returned %T", tm)
	}
	if nm.steerDir != "" || nm.steerClaimed {
		t.Errorf("steering off left steerDir=%q claimed=%v", nm.steerDir, nm.steerClaimed)
	}
	if _, live := steer.Live(dir, steer.TUIPresence); live {
		t.Error("presence survived a config that turned steering off")
	}
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
