package tui

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
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
