package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
)

// privateSessions installs a private session manager. Serial tests only:
// the manager is process-global.
func privateSessions(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { domain.Sessions().KillAll(context.Background()); restore() })
}

// modelTop is the fixture repository's worktree.
func modelTop(t *testing.T, m Model) string {
	t.Helper()
	top, err := m.svc.TopLevel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return top
}

// The page's start reaches Update, starts the session the terminal's way,
// answers with its id, records the child's inbox and never opens a console.
func TestWebSessionRequestStartsLikeTheTerminal(t *testing.T) {
	privateSessions(t)
	installFakeHost(t)
	m := loadedModel(t)
	m = m.ensureWeb()
	dir := modelTop(t, m)
	reply := make(chan webSessionReply, 1)
	req := domain.SessionStartRequest{Worktree: dir, Cols: 90, Rows: 30,
		Command: config.ToolCommand{Name: "Shell", Category: "session", Mode: "session", Command: "sh -c 'sleep 30'"}}
	m, leaf, why := m.webSessionStartLeaf(webSessionRequestMsg{req: req, reply: reply})
	if why != "" {
		t.Fatal(why)
	}
	m, _ = m.onWebSessionStarted(leaf().(webSessionStartedMsg))
	r := <-reply
	if r.err != nil || r.id == "" {
		t.Fatalf("reply = %+v", r)
	}
	s, ok := domain.Sessions().Get(r.id)
	if !ok || s.Info().Label != "Shell" || s.Info().Dir != dir {
		t.Fatalf("session = %+v", s)
	}
	if m.console != nil {
		t.Fatal("a web start opened the terminal's console")
	}
	if got := m.childInbox[r.id]; got != m.childInboxDir() {
		t.Fatalf("childInbox = %q, want the terminal's inbox %q", got, m.childInboxDir())
	}
	if !strings.Contains(m.statusMsg, "Shell") || !strings.Contains(m.statusMsg, "web page") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

// A page start with a name starts the session named, and the terminal's own
// alt+↓ ring learns the name (the page's server already wrote it to disk).
func TestWebSessionRequestCarriesTheName(t *testing.T) {
	privateSessions(t)
	installFakeHost(t)
	m := loadedModel(t)
	m = m.ensureWeb()
	reply := make(chan webSessionReply, 1)
	req := domain.SessionStartRequest{Worktree: modelTop(t, m), Cols: 90, Rows: 30, Name: " viewer ",
		Command: config.ToolCommand{Name: "Shell", Category: "session", Mode: "session", Command: "sh -c 'sleep 30'"}}
	m, leaf, why := m.webSessionStartLeaf(webSessionRequestMsg{req: req, reply: reply})
	if why != "" {
		t.Fatal(why)
	}
	m, _ = m.onWebSessionStarted(leaf().(webSessionStartedMsg))
	r := <-reply
	s, ok := domain.Sessions().Get(r.id)
	if !ok || s.Info().Name != "viewer" {
		t.Fatalf("session = %+v", s)
	}
	if ring := m.searchHist[scopeAgentName]; len(ring) == 0 || ring[0] != "viewer" {
		t.Fatalf("ring = %v", ring)
	}
	if !strings.Contains(m.statusMsg, "Shell [viewer]") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestWebSessionRequestRefusesAnUnreachableWorktree(t *testing.T) {
	m := loadedModel(t).ensureWeb()
	old := guardStat
	guardStat = func(string) error { return errors.New("gone") }
	t.Cleanup(func() { guardStat = old })
	reply := make(chan webSessionReply, 1)
	m, _ = m.onWebSessionRequest(webSessionRequestMsg{req: domain.SessionStartRequest{Worktree: "/nowhere", Terminal: true}, reply: reply})
	if r := <-reply; r.err == nil || !strings.Contains(r.err.Error(), "not reachable") {
		t.Fatalf("reply = %+v", r)
	}
}

// The page's first-run detect wrote session commands the terminal has not
// loaded: a web start reloads the terminal's config, so its own Start agent
// lists them instead of detecting (and appending) again.
func TestWebSessionRequestReloadsAStaleConfig(t *testing.T) {
	privateSessions(t)
	m := loadedModel(t).ensureWeb()
	global := filepath.Join(t.TempDir(), "config.toml")
	old := agentGlobalConfigPath
	agentGlobalConfigPath = func() string { return global }
	t.Cleanup(func() { agentGlobalConfigPath = old })
	tc := config.ToolCommand{Name: "Shell", Category: "session", Mode: "session", Command: "sh -c 'sleep 30'"}
	if err := config.AppendToolCommands(global, []config.ToolCommand{tc}); err != nil {
		t.Fatal(err)
	}
	if n := len(domain.SessionCommands(m.cfg, "tui")); n != 0 {
		t.Fatalf("fixture: the model already has %d session commands", n)
	}
	reply := make(chan webSessionReply, 1)
	m, leaf, why := m.webSessionStartLeaf(webSessionRequestMsg{req: domain.SessionStartRequest{Worktree: modelTop(t, m), Command: tc, Cols: 80, Rows: 24}, reply: reply})
	if why != "" {
		t.Fatal(why)
	}
	m, _ = m.onWebSessionStarted(leaf().(webSessionStartedMsg))
	if n := len(domain.SessionCommands(m.cfg, "tui")); n != 1 {
		t.Fatalf("the terminal still sees %d session commands", n)
	}
}

// Review focus 5: a closing or wedged terminal ends the page's request.
func TestSessionStarterEndsWhenTheTerminalCloses(t *testing.T) {
	t.Parallel()
	w := newWebHostState()
	start := sessionStarterFor(w)
	close(w.stop)
	if _, err := start(context.Background(), domain.SessionStartRequest{}); err == nil || !strings.Contains(err.Error(), "closing") {
		t.Fatalf("err = %v", err)
	}
	w2 := newWebHostState()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := sessionStarterFor(w2)(ctx, domain.SessionStartRequest{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a wedged terminal: err = %v", err)
	}
}

// The host gets the starter with the switcher, and the wait is armed once
// the page serves.
func TestWebStartInstallsTheSessionStarter(t *testing.T) {
	f := installFakeHost(t)
	m := loadedModel(t)
	m, cmd := m.openInBrowser()
	_ = runOne(t, m, cmd)
	if f.starter == nil {
		t.Fatal("the host never got a session starter")
	}
}

// The hosted page's dialog ran the first-run detect (the global file holds
// the commands) while the terminal's config is stale: the terminal's own
// Start agent must list them — not report "no agent found", not append again.
func TestStartAgentAfterThePageDetected(t *testing.T) {
	m := loadedModel(t)
	global := filepath.Join(t.TempDir(), "config.toml")
	old, oldDetect := agentGlobalConfigPath, agentDetect
	agentGlobalConfigPath = func() string { return global }
	agentDetect = func() []exttool.Detection { t.Error("detected again"); return nil }
	t.Cleanup(func() { agentGlobalConfigPath, agentDetect = old, oldDetect })
	tc := config.ToolCommand{Name: "Shell", Category: "session", Mode: "session", Command: "sh -c 'sleep 30'"}
	if err := config.AppendToolCommands(global, []config.ToolCommand{tc}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(global)
	msg := m.ensureAgentsCmd(modelTop(t, m))().(agentEnsureMsg)
	if msg.err != nil || msg.added != nil || !msg.loaded || len(domain.SessionCommands(msg.cfg, "tui")) != 1 {
		t.Fatalf("ensure = %+v", msg)
	}
	if after, _ := os.ReadFile(global); string(after) != string(before) {
		t.Fatal("the global config was appended to again")
	}
}

// servedModel is a loaded model whose fake page is serving.
func servedModel(t *testing.T) Model {
	t.Helper()
	installFakeHost(t)
	m := loadedModel(t)
	m, cmd := m.openInBrowser()
	return runOne(t, m, cmd)
}

// The page switches repository while the terminal still shows Settings (the
// user read the URL there and went to the browser): Settings closes and the
// switch goes through.
func TestWebSwitchClosesAnIdleSettingsWindow(t *testing.T) {
	m := servedModel(t)
	m, other := addWorktree(t, m, "wt2") // a real switch: the one on screen is answered at once
	m = m.pushLayer(&settingsPopup{})
	m = m.pushLayer(&webSettingsPopup{})
	reply := make(chan error, 1)
	m, _ = m.onWebSwitchRequest(webSwitchRequestMsg{path: other, reply: reply})
	select {
	case err := <-reply:
		t.Fatalf("the switch was answered at once (refused?): %v", err)
	default:
	}
	if len(m.web.pendingSwitch) != 1 {
		t.Fatalf("pendingSwitch = %d, want the switch in flight", len(m.web.pendingSwitch))
	}
	switch m.topLayer().(type) {
	case *settingsPopup, *webSettingsPopup:
		t.Fatal("Settings is still open")
	}
}

// …but never while a field is being typed into there: the refusal names the
// way out.
func TestWebSwitchKeepsASettingsFieldBeingEdited(t *testing.T) {
	m := servedModel(t)
	m = m.pushLayer(&settingsPopup{})
	m = m.pushLayer(&webSettingsPopup{editing: true})
	reply := make(chan error, 1)
	m, _ = m.onWebSwitchRequest(webSwitchRequestMsg{path: modelTop(t, m), reply: reply})
	select {
	case err := <-reply:
		if err == nil || !strings.Contains(err.Error(), "press esc in the terminal") {
			t.Fatalf("refusal = %v", err)
		}
	default:
		t.Fatal("a switch over a field being edited was not refused")
	}
	if _, ok := m.topLayer().(*webSettingsPopup); !ok {
		t.Fatal("the edited popup was closed")
	}
}
