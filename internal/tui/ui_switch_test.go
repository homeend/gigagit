package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// uiServedModel is a loaded model whose page is running, terminal active.
func uiServedModel(t *testing.T) (Model, *fakeWebHost) {
	t.Helper()
	f := installFakeHost(t)
	m := loadedModel(t).ensureWeb()
	m = runOne(t, m, startWebCmd(m.homeSvc(), m.web, "", false))
	return m, f
}

// blockedModel is uiServedModel after a confirmed switch.
func blockedModel(t *testing.T) (Model, *fakeWebHost) {
	t.Helper()
	m, f := uiServedModel(t)
	m, _ = m.activateWeb()
	if !m.webActive {
		t.Fatal("the switch did not flip")
	}
	return m, f
}

func updated(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	nm, cmd := m.Update(msg)
	return nm.(Model), cmd
}

func resolves(t *testing.T, cmd tea.Cmd, want tea.Msg) bool {
	t.Helper()
	for _, msg := range runLeaves(cmd) {
		if msg == want {
			return true
		}
	}
	return false
}

func TestSwitchToWebConfirms(t *testing.T) {
	m, f := uiServedModel(t)
	m, _ = m.askSwitchToWeb()
	if m.modal == nil || m.modal.req.ID != "ui-switch-web" {
		t.Fatalf("modal = %+v", m.modal)
	}
	if !strings.Contains(m.modal.req.Prompt, "Switch to the web UI? The terminal stays blocked until you switch back.") {
		t.Fatalf("prompt = %q", m.modal.req.Prompt)
	}
	resolve := m.modal.onResolve
	m.modal = nil // as the decision's own resolve does
	told := len(f.active)
	nm, _ := resolve(m, "Cancel")
	if nm.(Model).webActive || len(f.active) != told {
		t.Fatalf("cancel flipped: %+v", f.active)
	}
	nm, _ = resolve(m, "Switch")
	if !nm.(Model).webActive {
		t.Fatal("Switch did not flip")
	}
}

func TestPaletteDropsOpenWebUI(t *testing.T) {
	m := loadedModel(t)
	if paletteHas(m, "Switch to web UI") {
		t.Fatal("no seam: no entry")
	}
	installFakeHost(t)
	if !paletteHas(m, "Switch to web UI") {
		t.Fatal("seam set: entry present")
	}
	if paletteHas(m, "Open web UI in browser") {
		t.Fatal("the old entry must be gone")
	}
}

func TestSettingsRowSwitches(t *testing.T) {
	m, _ := uiServedModel(t)
	m = m.openWebSettings()
	p := m.topLayer().(*webSettingsPopup)
	p.sel = webRowSwitch
	if !strings.Contains(p.render(m, ""), "Switch to web UI") {
		t.Fatal("row label")
	}
	m, _ = updated(t, m, keyMsg("enter"))
	if m.modal == nil || m.modal.req.ID != "ui-switch-web" {
		t.Fatalf("modal = %+v", m.modal)
	}
}

func TestSwitchRefusedWhileBusy(t *testing.T) {
	m, f := uiServedModel(t)
	m.running = true
	m, _ = m.askSwitchToWeb()
	if m.modal != nil || m.webActive {
		t.Fatalf("busy: modal %+v active %v", m.modal, m.webActive)
	}
	if !strings.Contains(m.statusMsg, "an operation is running") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	m, _ = m.activateWeb()
	if m.webActive || f.opens != 0 {
		t.Fatal("activateWeb must refuse too")
	}
}

func TestSwitchClosesWindows(t *testing.T) {
	m, _ := uiServedModel(t)
	m = m.openWebSettings()
	m.filterTyping = true
	m, _ = m.activateWeb()
	if m.topLayer() != nil || m.filterTyping {
		t.Fatalf("layer %T typing %v", m.topLayer(), m.filterTyping)
	}
}

func TestSwitchAdoptsLook(t *testing.T) {
	m, _ := uiServedModel(t)
	m.home = "/somewhere/else"
	m.viewed = "/looked/at"
	m, _ = m.activateWeb()
	if m.home != "/looked/at" {
		t.Fatalf("home = %q", m.home)
	}
}

func TestSwitchOpensBrowserOnlyWithNoTab(t *testing.T) {
	m, f := uiServedModel(t)
	f.clients = 1
	m, _ = m.activateWeb()
	if f.opens != 0 {
		t.Fatalf("a connected tab flips in place: opens %d", f.opens)
	}
	if got := f.active[len(f.active)-1]; !got {
		t.Fatal("host not told")
	}
	m, _ = m.activateTUI()
	f.clients = 0
	m, _ = m.activateWeb()
	if f.opens != 1 || !m.webActive {
		t.Fatalf("no tab: opens %d", f.opens)
	}
}

func TestStartFailureStaysTUI(t *testing.T) {
	f := installFakeHost(t)
	f.startErr = errors.New("address already in use")
	m := loadedModel(t).ensureWeb()
	m, cmd := m.activateWeb()
	if !m.webActive || !strings.Contains(m.View(), "starting…") {
		t.Fatal("the terminal shows the block while the page starts")
	}
	m = runOne(t, m, cmd)
	if m.webActive {
		t.Fatal("a page that cannot start must never block")
	}
	if !strings.Contains(m.statusMsg, "address already in use") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestBlockedSwallowsKeys(t *testing.T) {
	m, _ := blockedModel(t)
	before := m.View()
	for _, k := range []string{"x", "?", "esc", "c", "ctrl+p"} {
		var cmd tea.Cmd
		m, cmd = updated(t, m, keyMsg(k))
		if cmd != nil || !m.webActive || m.topLayer() != nil {
			t.Fatalf("%s reached the panels: layer %T", k, m.topLayer())
		}
	}
	if m.View() != before {
		t.Fatal("a swallowed key changed the screen")
	}
}

func TestBlockedButtons(t *testing.T) {
	m, _ := blockedModel(t)
	got, _ := updated(t, m, keyMsg("enter"))
	if got.webActive {
		t.Fatal("enter on the first row activates the TUI")
	}
	got, cmd := updated(t, m, keyMsg("down"))
	got, cmd = updated(t, got, keyMsg("enter"))
	if !got.webActive || !resolves(t, cmd, tea.Quit()) {
		t.Fatal("down, enter is Quit")
	}
	got, cmd = updated(t, m, keyMsg("q"))
	if !got.webActive || !resolves(t, cmd, tea.Quit()) {
		t.Fatal("q is Quit")
	}
	lay := m.blockLayout()
	click := func(y int) tea.MouseMsg {
		return tea.MouseMsg{X: 1, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	}
	got, cmd = updated(t, m, click(lay.quitY))
	if !resolves(t, cmd, tea.Quit()) {
		t.Fatal("click on Quit")
	}
	got, _ = updated(t, m, click(lay.activateY))
	if got.webActive {
		t.Fatal("click on Activate TUI")
	}
	got, cmd = updated(t, m, click(0))
	if !got.webActive || cmd != nil {
		t.Fatal("a click elsewhere does nothing")
	}
}

func TestBlockedActivateRefused(t *testing.T) {
	m, f := blockedModel(t)
	f.busy = "an operation is running"
	m, _ = updated(t, m, keyMsg("enter"))
	if !m.webActive || !strings.Contains(m.View(), "an operation is running") {
		t.Fatal("the block stays, with the reason under the buttons")
	}
}

func TestBlockedViewShowsURL(t *testing.T) {
	m, _ := blockedModel(t)
	v := m.View()
	for _, want := range []string{"Web UI is active", "http://127.0.0.1:4242", "Activate TUI", "Quit"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q in:\n%s", want, v)
		}
	}
	m.width, m.height = 12, 10
	for _, l := range m.blockLayout().lines {
		if w := lipgloss.Width(l); w > 12 {
			t.Fatalf("line %q is %d wide at 12 columns", l, w)
		}
	}
}

func TestBlockedWithNoTabsCanActivate(t *testing.T) {
	m, f := blockedModel(t)
	f.clients = 0
	m, _ = updated(t, m, keyMsg("enter"))
	if m.webActive {
		t.Fatal("no tab connected: the terminal must still come back")
	}
	if f.active[len(f.active)-1] {
		t.Fatal("host not told")
	}
}

func TestQuitHeldShowsOverBlock(t *testing.T) {
	m, _ := blockedModel(t)
	var cmd tea.Cmd
	m, cmd = m.openSessionsPopup(true)
	_ = cmd
	if strings.Contains(m.View(), "Web UI is active") && m.topLayer() == nil {
		t.Fatal("the quit popup did not open")
	}
	if _, ok := m.topLayer().(*sessionsPopup); !ok {
		t.Fatalf("top = %T", m.topLayer())
	}
	if m.quitPopupUp() == nil {
		t.Fatal("the quit-mode popup must be recognised over the block")
	}
	m, _ = updated(t, m, keyMsg("esc"))
	if m.topLayer() != nil || !strings.Contains(m.View(), "Web UI is active") {
		t.Fatal("esc returns to the block")
	}
}

func TestActivatorFromPage(t *testing.T) {
	m, f := uiServedModel(t)
	reply := make(chan error, 1)
	m, _ = m.onWebActivateRequest(webActivateRequestMsg{side: "web", reply: reply})
	if err := <-reply; err != nil || !m.webActive {
		t.Fatalf("err %v active %v", err, m.webActive)
	}
	m, _ = m.onWebActivateRequest(webActivateRequestMsg{side: "tui", reply: reply})
	if err := <-reply; err != nil || m.webActive {
		t.Fatalf("err %v active %v", err, m.webActive)
	}
	m.running = true
	m, _ = m.onWebActivateRequest(webActivateRequestMsg{side: "web", reply: reply})
	if err := <-reply; err == nil || m.webActive {
		t.Fatalf("err %v active %v", err, m.webActive)
	}
	m.running = false
	m, _ = m.activateWeb()
	f.busy = "an operation is running"
	m, _ = m.onWebActivateRequest(webActivateRequestMsg{side: "tui", reply: reply})
	if err := <-reply; err == nil || !m.webActive {
		t.Fatalf("err %v active %v", err, m.webActive)
	}
}

func TestActivatorForAnswers(t *testing.T) {
	m, _ := uiServedModel(t)
	fn := activatorFor(m.web)
	done := make(chan error, 1)
	go func() { done <- fn(context.Background(), "web") }()
	msg := waitWebActivateCmd(m.web)().(webActivateRequestMsg)
	msg.reply <- nil
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestActivateResendsConsoleSize(t *testing.T) {
	m, _ := uiServedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	m.webActive = true
	_ = s.Resize(100, 30) // the page owned the size while it was active
	m, _ = m.activateTUI()
	w, h := m.consoleBox()
	cols, rows := consoleInner(w, h)
	if sc := s.Screen(); sc.Cols != cols || sc.Rows != rows {
		t.Fatalf("session is %dx%d, want %dx%d", sc.Cols, sc.Rows, cols, rows)
	}
}
