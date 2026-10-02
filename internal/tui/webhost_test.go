package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// fakeWebHost records what the TUI asks of its web page.
type fakeWebHost struct {
	starts, opens, closes int
	addr                  string
	reroots               []*domain.Service
	url                   string
	startErr              error
	switcher              func(ctx context.Context, path string) error
	starter               func(ctx context.Context, req domain.SessionStartRequest) (domain.SessionID, error)
}

func (f *fakeWebHost) SetSessionStarter(fn func(ctx context.Context, req domain.SessionStartRequest) (domain.SessionID, error)) {
	f.starter = fn
}

func (f *fakeWebHost) SetSwitcher(fn func(ctx context.Context, path string) error) { f.switcher = fn }

func (f *fakeWebHost) Start(_ context.Context, addr string) (string, error) {
	f.starts++
	f.addr = addr
	if f.startErr != nil {
		return "", f.startErr
	}
	f.url = "http://127.0.0.1:4242"
	return f.url, nil
}
func (f *fakeWebHost) Reroot(_ context.Context, svc *domain.Service) error {
	f.reroots = append(f.reroots, svc)
	return nil
}
func (f *fakeWebHost) URL() string  { return f.url }
func (f *fakeWebHost) OpenBrowser() { f.opens++ }
func (f *fakeWebHost) Close()       { f.closes++ }

func installFakeHost(t *testing.T) *fakeWebHost {
	t.Helper()
	f := &fakeWebHost{}
	NewWebHost = func(*domain.Service) WebHost { return f }
	t.Cleanup(func() { NewWebHost = nil })
	return f
}

// runOne executes cmd and feeds its message back into the model.
func runOne(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("the command produced no message")
	}
	nm, _ := m.Update(msg)
	return nm.(Model)
}

func paletteHas(m Model, label string) bool {
	for _, c := range m.availablePaletteCommands() {
		if c.label == label {
			return true
		}
	}
	return false
}

func TestOpenInBrowserStartsOnceThenReopens(t *testing.T) {
	f := installFakeHost(t)
	m := loadedModel(t)
	m, cmd := m.openInBrowser()
	m = runOne(t, m, cmd) // webStartedMsg
	if f.starts != 1 || f.opens != 1 || m.statusMsg != "web page: serving http://127.0.0.1:4242" {
		t.Fatalf("starts=%d opens=%d status=%q", f.starts, f.opens, m.statusMsg)
	}
	if f.addr != "" {
		t.Fatalf("addr = %q, want the random-port default", f.addr)
	}
	m, cmd = m.openInBrowser()
	if cmd != nil {
		t.Fatal("a second use only opens the browser")
	}
	if f.starts != 1 || f.opens != 2 {
		t.Fatalf("starts=%d opens=%d", f.starts, f.opens)
	}
}

func TestOpenInBrowserBindFailureIsReportedAndRetriable(t *testing.T) {
	f := installFakeHost(t)
	f.startErr = errors.New("listen tcp 127.0.0.1:7777: bind: address already in use")
	m := loadedModel(t)
	m, cmd := m.openInBrowser()
	m = runOne(t, m, cmd)
	if m.webServing() || !strings.Contains(m.statusMsg, "address already in use") {
		t.Fatalf("serving=%v status=%q", m.webServing(), m.statusMsg)
	}
	f.startErr = nil
	m, cmd = m.openInBrowser()
	m = runOne(t, m, cmd)
	if f.starts != 2 || !m.webServing() {
		t.Fatal("the palette entry retries")
	}
}

func TestOpenInBrowserWithoutASeamSaysSo(t *testing.T) {
	m := loadedModel(t)
	m, cmd := m.openInBrowser()
	if cmd != nil || m.statusMsg != "this gg cannot serve a web page" {
		t.Fatalf("cmd=%v status=%q", cmd != nil, m.statusMsg)
	}
}

func TestWebAddrFlagBeatsConfig(t *testing.T) {
	m := loadedModel(t)
	if got := m.webAddr(); got != "" {
		t.Fatalf("default addr = %q", got)
	}
	m.cfg.Web = config.WebConfig{Addr: "127.0.0.1:1111"}
	if got := m.webAddr(); got != "127.0.0.1:1111" {
		t.Fatalf("config addr = %q", got)
	}
	m.webOpts.WebAddr = "127.0.0.1:2222"
	if got := m.webAddr(); got != "127.0.0.1:2222" {
		t.Fatalf("flag addr = %q", got)
	}
}

func TestServeAtStartup(t *testing.T) {
	f := installFakeHost(t)
	m := loadedModel(t)
	if m.startupWebCmd() != nil {
		t.Fatal("neither [web] serve nor --web: nothing starts")
	}
	m.cfg.Web.Serve = true
	cmd := m.startupWebCmd()
	if cmd == nil {
		t.Fatal("[web] serve starts the host at launch")
	}
	m = runOne(t, m, cmd)
	if f.starts != 1 || f.opens != 0 || !m.webServing() {
		t.Fatalf("starts=%d opens=%d serving=%v — startup serving never opens the browser", f.starts, f.opens, m.webServing())
	}
	m2 := loadedModel(t)
	m2.webOpts.Web = true
	if m2.startupWebCmd() == nil {
		t.Fatal("--web starts the host at launch")
	}
}

// runLeaves executes cmd and, for a batch, each of its leaf commands ONCE,
// returning their messages without feeding them back (reRoot's batch holds
// loads whose results would re-arm blocking waiters).
func runLeaves(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range batch {
		out = append(out, runLeaves(c)...) // nested batches too
	}
	return out
}

func TestReRootHandsTheNewServiceToTheHost(t *testing.T) {
	f := installFakeHost(t)
	m := loadedModel(t)
	m, cmd := m.openInBrowser()
	m = runOne(t, m, cmd)
	nm, cmd := m.reRoot(m.currentWorktree)
	m = nm.(Model)
	got := false
	for _, msg := range runLeaves(cmd) {
		if r, ok := msg.(webRerootMsg); ok {
			got = true
			if r.err != nil {
				t.Fatal(r.err)
			}
		}
	}
	if !got || len(f.reroots) != 1 || f.reroots[0] != m.svc {
		t.Fatalf("reroot msg=%v reroots=%d, want exactly the new service", got, len(f.reroots))
	}
}

func TestReRootWithoutAPageAsksNothing(t *testing.T) {
	f := installFakeHost(t)
	m := loadedModel(t)
	nm, cmd := m.reRoot(m.currentWorktree)
	runLeaves(cmd)
	_ = nm
	if len(f.reroots) != 0 || f.starts != 0 {
		t.Fatal("no page, no re-root call")
	}
}

func TestPaletteHasOpenInBrowserOnlyWithASeam(t *testing.T) {
	m := loadedModel(t)
	if paletteHas(m, "Open in browser") {
		t.Fatal("no seam: no entry")
	}
	installFakeHost(t)
	if !paletteHas(m, "Open in browser") {
		t.Fatal("seam set: entry present")
	}
}

func TestCloseWebEndsTheHost(t *testing.T) {
	f := installFakeHost(t)
	m := loadedModel(t)
	m, cmd := m.openInBrowser()
	m = runOne(t, m, cmd)
	m.closeWeb()
	if f.closes != 1 {
		t.Fatalf("closes = %d", f.closes)
	}
}

// askSwitch plays the page: the host calls the TUI's switcher on its own
// goroutine and waits for the answer.
func askSwitch(t *testing.T, f *fakeWebHost, path string) <-chan error {
	t.Helper()
	if f.switcher == nil {
		t.Fatal("the TUI installs a switcher before the host serves")
	}
	done := make(chan error, 1)
	go func() { done <- f.switcher(context.Background(), path) }()
	return done
}

func servingModel(t *testing.T, f *fakeWebHost) Model {
	t.Helper()
	m := loadedModel(t)
	m, cmd := m.openInBrowser()
	return runOne(t, m, cmd)
}

// A switch asked from the page re-roots the TUI, and the page's answer waits
// until the host has followed.
func TestPageSwitchReRootsTheTUI(t *testing.T) {
	f := installFakeHost(t)
	m := servingModel(t, f)
	done := askSwitch(t, f, m.currentWorktree)
	msg := waitWebSwitchCmd(m.web)()
	req, ok := msg.(webSwitchRequestMsg)
	if !ok || req.path != m.currentWorktree {
		t.Fatalf("msg = %#v, want the page's switch request", msg)
	}
	old := m.svc
	nm, cmd := m.Update(req)
	m = nm.(Model)
	if m.svc == old || m.switchTarget != m.currentWorktree {
		t.Fatal("the TUI must re-root on the page's request")
	}
	if m.statusMsg != "switched from the web page" {
		t.Fatalf("status = %q", m.statusMsg)
	}
	select {
	case err := <-done:
		t.Fatalf("answered (%v) before the host followed", err)
	default:
	}
	// The re-armed wait is one of the leaves: feed it a throwaway request so
	// runLeaves does not block on it (closing stop would refuse the switch).
	go func() { m.web.switches <- webSwitchRequestMsg{path: "unused", reply: make(chan error, 1)} }()
	var rr tea.Msg
	for _, lm := range runLeaves(cmd) {
		if r, ok := lm.(webRerootMsg); ok {
			rr = r
		}
	}
	if rr == nil || len(f.reroots) != 1 || f.reroots[0] != m.svc {
		t.Fatalf("host reroots = %d, want the new service", len(f.reroots))
	}
	nm, _ = m.Update(rr)
	m = nm.(Model)
	if err := <-done; err != nil {
		t.Fatalf("switch = %v", err)
	}
	if m.statusMsg != "switched from the web page" {
		t.Fatalf("a clean follow keeps the status, got %q", m.statusMsg)
	}
}

// A busy terminal refuses with its reason and stays where it is.
func TestPageSwitchRefusedWhileTheTUIIsBusy(t *testing.T) {
	f := installFakeHost(t)
	m := servingModel(t, f)
	m.modal = &decisionState{}
	done := askSwitch(t, f, m.currentWorktree)
	req := waitWebSwitchCmd(m.web)()
	old := m.svc
	nm, cmd := m.Update(req)
	m = nm.(Model)
	err := <-done
	if err == nil || !strings.Contains(err.Error(), "a decision is waiting") {
		t.Fatalf("switch = %v, want the busy reason", err)
	}
	if m.svc != old || len(f.reroots) != 0 {
		t.Fatal("a refused switch must not re-root")
	}
	if cmd == nil {
		t.Fatal("the wait must re-arm after a refusal")
	}
}

// Quit ends the wait: the armed command returns instead of blocking.
func TestCloseWebEndsTheSwitchWait(t *testing.T) {
	f := installFakeHost(t)
	m := servingModel(t, f)
	m.closeWeb()
	got := make(chan tea.Msg, 1)
	go func() { got <- waitWebSwitchCmd(m.web)() }()
	select {
	case msg := <-got:
		if msg != nil {
			t.Fatalf("msg = %#v, want nil after close", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the switch wait outlived the page")
	}
	// A page asking after the close gets an answer too, never a hang.
	if err := <-askSwitch(t, f, m.currentWorktree); err == nil {
		t.Fatal("a closed terminal must refuse")
	}
}
