package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/steer"
)

// One active interface (spec 2026-10-31-one-active-interface): this process
// shows either the terminal or its hosted page. The Model owns which
// (m.webActive) and pushes a copy to the host, which blocks the page.

const (
	blockRowActivate = iota
	blockRowQuit
	blockRowCount
)

// webActivateRequestMsg is the page asking the terminal to make a side active
// ("web" or "tui"). reply is buffered: Update never blocks answering it.
type webActivateRequestMsg struct {
	side  string
	reply chan error
}

// activatorFor is the function the host calls for a flip: it hands the request
// to Update and waits for the answer (bounded by the page's request context).
func activatorFor(w *webHostState) func(ctx context.Context, side string) error {
	activations, stop := w.activations, w.stop
	return func(ctx context.Context, side string) error {
		reply := make(chan error, 1)
		select {
		case activations <- webActivateRequestMsg{side: side, reply: reply}:
		case <-stop:
			return errors.New("the terminal is closing")
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case err := <-reply:
			return err
		case <-stop:
			return errors.New("the terminal is closing")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// waitWebActivateCmd waits for the page's next flip request; nil once the page
// is closed.
func waitWebActivateCmd(w *webHostState) tea.Cmd {
	activations, stop := w.activations, w.stop
	return func() tea.Msg {
		select {
		case req := <-activations:
			return req
		case <-stop:
			return nil
		}
	}
}

// uiSwitchRefusal is why the terminal may not give up control now, in English
// (the page shows it too); "" when it may. Unlike steerRefusal, a window or a
// half-typed box does not refuse: the switch closes them.
func (m Model) uiSwitchRefusal() string {
	switch {
	case !m.opsIdle():
		return "an operation is running"
	case m.modal != nil:
		return "a decision is waiting for the user"
	case m.proc != nil:
		return "an interactive process owns the screen"
	}
	return ""
}

// uiRefusalText is a refusal for the terminal's own screen.
func uiRefusalText(why string) string {
	switch why {
	case "an operation is running":
		return i18n.T("an operation is running")
	case "a decision is waiting for the user":
		return i18n.T("a decision is waiting for the user")
	case "an interactive process owns the screen":
		return i18n.T("an interactive process owns the screen")
	}
	return why
}

// askSwitchToWeb is the palette's and Settings' Switch to web UI.
func (m Model) askSwitchToWeb() (Model, tea.Cmd) {
	if why := m.uiSwitchRefusal(); why != "" {
		m.statusMsg = i18n.T("can't switch: %s", uiRefusalText(why))
		return m, nil
	}
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "ui-switch-web",
			Prompt:  i18n.T("Switch to the web UI? The terminal stays blocked until you switch back."),
			Options: []string{"Switch", "Cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt != "Switch" {
				return m, nil
			}
			return m.activateWeb()
		},
	}
	return m, nil
}

// closeForSwitch closes every window and cancels anything being typed: what
// esc would lose, and nothing more. Sessions and consoles are not windows.
func (m Model) closeForSwitch() Model {
	m = m.clearLayers()
	m.filesView, m.filesPreview, m.stashView, m.actionMenu = nil, nil, nil, nil
	m.filterTyping, m.highlightTyping, m.recallOpen = false, false, false
	return m
}

// flipToWeb makes the page active, or says why not (English). A page not
// running yet is started with the block already up; onWebStarted finishes the
// flip, or takes it back when the page cannot start.
func (m Model) flipToWeb() (Model, tea.Cmd, string) {
	m = m.ensureWeb()
	if NewWebHost == nil {
		return m, nil, "this gg cannot serve a web page"
	}
	if m.webActive {
		if m.webServing() && m.web.host.Clients() == 0 {
			m.web.host.OpenBrowser()
		}
		return m, nil, ""
	}
	m = m.launchPopupToQuestion()
	if why := m.uiSwitchRefusal(); why != "" {
		return m.launchQuestionToPopup(), nil, why
	}
	m.flipConsole = ""
	if m.console != nil {
		m.flipConsole = m.console.id // the agent window on screen goes with the switch
	}
	m = m.hideConsoleHere() // first: what it had parked closes with the rest
	m = m.closeForSwitch()
	m, adopt := m.adoptView() // the page opens where the user was looking
	m.webActive, m.blockSel, m.blockReason = true, blockRowActivate, ""
	m = m.rewritePresence()
	switch {
	case m.webServing():
		m.web.host.SetShownConsole(m.flipConsole)
		m.web.host.SetActive(true)
		if m.web.host.Clients() == 0 {
			m.web.host.OpenBrowser()
		}
	case !m.web.starting:
		m.web.starting = true
		return m, tea.Batch(adopt, startWebCmd(m.homeSvc(), m.web, m.webAddr(), true)), ""
	}
	return m, adopt, ""
}

// activateWeb is a confirmed Switch to web UI.
func (m Model) activateWeb() (Model, tea.Cmd) {
	if NewWebHost == nil {
		m.statusMsg = i18n.T("this gg cannot serve a web page")
		return m, nil
	}
	m, cmd, why := m.flipToWeb()
	if why != "" {
		m.statusMsg = i18n.T("can't switch: %s", uiRefusalText(why))
	}
	return m, cmd
}

// activateTUI is the blocked screen's Activate TUI and the page's Switch to
// TUI. A page with an operation running keeps control; the reason shows under
// the buttons.
func (m Model) activateTUI() (Model, tea.Cmd) {
	if !m.webActive {
		return m, nil
	}
	if m.webServing() {
		if why := m.web.host.Busy(); why != "" {
			m.blockReason = why
			return m, nil
		}
		m.web.host.SetActive(false)
	}
	m.webActive, m.blockReason = false, ""
	m = m.launchQuestionToPopup()
	m = m.rewritePresence()
	m, shown := m.takeConsoleFromPage()
	m = m.syncConsoleSize() // the page may have resized the session
	m, catchUp := m.catchUpAfterBlock()
	return m, tea.Batch(shown, catchUp)
}

// takeConsoleFromPage shows the agent window the page was showing, and none
// when it showed none: the window on screen follows a switch. A session gone
// since is no news here; the sessions list says so.
func (m Model) takeConsoleFromPage() (Model, tea.Cmd) {
	if !m.webServing() {
		return m, nil
	}
	id := m.web.host.ShownConsole()
	if m.console != nil && m.console.id == id {
		return m, nil
	}
	m = m.hideConsoleHere()
	if _, ok := domain.Sessions().Get(id); id == "" || !ok {
		return m, nil
	}
	return m.openConsole(id)
}

// catchUpAfterBlock is what a terminal skipped while blocked: every source, the
// sleeping members whose watch events it dropped, what finished behind the
// block, and the health probe that raises the launch prompts that waited.
func (m Model) catchUpAfterBlock() (Model, tea.Cmd) {
	m, reload := m.reloadAllCmd(reloadOpts{})
	m, tasks := m.applyTasks()
	cmds := []tea.Cmd{reload, tasks, m.repoHealthCmd(m.noticeGen)}
	if m.ws != nil {
		for _, r := range m.ws.members {
			if r != m.liveRepo() && !r.missing {
				cmds = append(cmds, memberLoadCmd(r))
			}
		}
	}
	return m, tea.Batch(cmds...)
}

// tuiPresence is this terminal's presence file. While the page is active it
// says so and names the page, so `gg session` reaches the browser the user is
// in from any inbox this terminal still answers.
func (m Model) tuiPresence() steer.Presence {
	p := steer.Presence{
		PID:      os.Getpid(),
		Worktree: m.snapshotWorktree,
		Started:  time.Now().UTC().Format(time.RFC3339),
		MCP:      m.agentURL(),
		Inactive: m.webActive,
	}
	if m.webActive && m.webServing() {
		p.URL = m.web.url
	}
	return p
}

// rewritePresence writes the presence files anew after a flip: Touch alone only
// refreshes the mtime of a file that exists.
func (m Model) rewritePresence() Model {
	// The own inbox may be closed: an adopt just moved home, and the inbox a
	// running agent holds is a kept one now.
	if m.steerActive() {
		releaseOwnPresence(m.steerDir)
		m.touchSteerPresence()
	}
	for dir := range m.keptSteer {
		releaseOwnPresence(dir)
	}
	return m.tendKeptInboxes()
}

// applyWebLaunch takes `gg --web`: the page is active from the first frame,
// when this gg can serve one at all.
func (m Model) applyWebLaunch(opts webLaunchOptions) Model {
	m.webOpts = opts
	m.webActive = opts.Web && NewWebHost != nil && !m.quiet
	return m.ensureWeb()
}

// onWebActivateRequest is the page asking for a flip.
func (m Model) onWebActivateRequest(msg webActivateRequestMsg) (Model, tea.Cmd) {
	rearm := waitWebActivateCmd(m.ensureWeb().web)
	var cmd tea.Cmd
	var err error
	if msg.side == "tui" {
		m, cmd = m.activateTUI()
		if m.webActive {
			err = errors.New(m.blockReason)
		}
	} else {
		var why string
		m, cmd, why = m.flipToWeb()
		if why != "" {
			err = errors.New("the terminal is busy: " + why)
		}
	}
	msg.reply <- err
	return m, tea.Batch(cmd, rearm)
}

// quitPopupUp is the one window that may show over the block: quitting with
// live work asks through the quit-mode sessions popup (quitFilter).
func (m Model) quitPopupUp() *sessionsPopup {
	if p, ok := m.topLayer().(*sessionsPopup); ok && p.quitMode {
		return p
	}
	return nil
}

// blockedMsg is the gate in front of a blocked terminal: keys and the mouse
// work the two buttons and reach nothing else. esc does nothing, so a stray
// press cannot take the interface from someone in the browser. alt+U keeps
// working, as it does over any window.
func (m Model) blockedMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if !m.webActive {
		return m, nil, false
	}
	if m.quitPopupUp() != nil {
		// The popup decides about quitting. Opening a session from it could
		// re-root the terminal and move the page under the browser's user.
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "enter" {
				return m, nil, true
			}
		case tea.MouseMsg:
			return m, nil, true
		}
		return m, nil, false
	}
	pick := func(row int) (tea.Model, tea.Cmd, bool) {
		if row == blockRowQuit {
			return m, tea.Quit, true
		}
		nm, cmd := m.activateTUI()
		return nm, cmd, true
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "alt+U":
			return m, nil, false
		case "up", "down", "tab", "shift+tab", "k", "j":
			m.blockSel = (m.blockSel + 1) % blockRowCount
			return m, nil, true
		case "enter":
			return pick(m.blockSel)
		case "q", "ctrl+c":
			return pick(blockRowQuit)
		}
		return m, nil, true
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			lay := m.blockLayout()
			switch msg.Y {
			case lay.activateY:
				return pick(blockRowActivate)
			case lay.quitY:
				return pick(blockRowQuit)
			}
		}
		return m, nil, true
	}
	return m, nil, false
}

// blockLayout is the blocked screen, rows and their screen lines: the one
// source renderBlock and the mouse share.
type blockLayout struct {
	lines            []string
	activateY, quitY int
}

func (m Model) blockLayout() blockLayout {
	w, h := m.overlayDims()
	addr := i18n.T("starting…")
	if m.webServing() {
		addr = elidePath(m.web.url, max(w-4, 8))
	}
	row := func(i int, text string) string {
		if m.blockSel == i {
			return st().selectedRow.Render("> " + text)
		}
		return "  " + text
	}
	rows := []string{
		st().titleStyle.Render(i18n.T("Web UI is active")),
		"",
		addr,
		"",
		row(blockRowActivate, i18n.T("Activate TUI")),
		row(blockRowQuit, i18n.T("Quit")),
	}
	const activateAt, quitAt = 4, 5
	if m.blockReason != "" {
		rows = append(rows, "", uiRefusalText(m.blockReason))
	}
	top := max((h-len(rows))/2, 0)
	lay := blockLayout{activateY: top + activateAt, quitY: top + quitAt}
	for range top {
		lay.lines = append(lay.lines, "")
	}
	for _, r := range rows {
		if lipgloss.Width(r) > w {
			r = lipgloss.NewStyle().MaxWidth(w).Render(r)
		}
		pad := max((w-lipgloss.Width(r))/2, 0)
		lay.lines = append(lay.lines, strings.Repeat(" ", pad)+r)
	}
	return lay
}

func (m Model) renderBlock() string { return strings.Join(m.blockLayout().lines, "\n") + "\n" }
