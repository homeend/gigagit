package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

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
}

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
		if c != nil {
			out = append(out, c())
		}
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
