package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/steer"
)

// WebHost is the gg web page served from THIS process. cmd/gg sets
// NewWebHost to internal/web's Host (the two frontends never import each
// other); nil = unavailable, and the palette entry is absent. Sharing the
// process is the point: the page and the terminal read ONE agent-session
// manager, so a console in the browser shows the sessions started here.
type WebHost interface {
	Start(ctx context.Context, addr string) (string, error)
	Reroot(ctx context.Context, svc *domain.Service) error
	// SetSwitcher installs the page's way to switch the terminal: the
	// page's own re-root calls fn, which re-roots the TUI (and, through
	// Reroot, the page) before returning, or refuses with a reason.
	SetSwitcher(fn func(ctx context.Context, path string) error)
	// SetSessionStarter installs the page's way to start a session: the
	// terminal starts it (its inbox, its agent channel) and returns the id.
	SetSessionStarter(fn func(ctx context.Context, req domain.SessionStartRequest) (domain.SessionID, error))
	URL() string
	OpenBrowser()
	Close()
}

// NewWebHost builds a host over the TUI's Service. Set by cmd/gg.
var NewWebHost func(svc *domain.Service) WebHost

// webHostState lives on a pointer field: a start in flight and a running
// host must survive the value-receiver copy.
type webHostState struct {
	host     WebHost
	url      string
	starting bool
	// pendingServe: `serve` inbox commands waiting for the start in flight
	// (steer.go); answered by onWebStarted.
	pendingServe []steer.Command
	// switches carries the page's switch requests from the host's HTTP
	// goroutine into Update (waitWebSwitchCmd); stop ends that wait and
	// refuses late requests once the page is closed.
	switches chan webSwitchRequestMsg
	// sessions carries the page's start requests, HTTP goroutine → Update
	// (waitWebSessionCmd, websession.go).
	sessions chan webSessionRequestMsg
	stop     chan struct{}
	// pendingSwitch: answers owed to page switches whose re-root is done
	// but whose host follow (webRerootMsg) has not landed yet.
	pendingSwitch []chan error
}

// webSwitchRequestMsg is the page asking the terminal to switch to path.
// reply is buffered: Update never blocks answering it.
type webSwitchRequestMsg struct {
	path  string
	reply chan error
}

// webLaunchOptions carries `gg --web` / `--web-addr` for this run.
type webLaunchOptions struct {
	Web     bool
	WebAddr string
}

// webStartedMsg is the host's answer to startWebCmd.
type webStartedMsg struct {
	host WebHost
	url  string
	err  error
	open bool // open the browser once serving (the palette path; startup does not)
}

// webRerootMsg reports the host following a re-root.
type webRerootMsg struct{ err error }

func newWebHostState() *webHostState {
	return &webHostState{switches: make(chan webSwitchRequestMsg), sessions: make(chan webSessionRequestMsg), stop: make(chan struct{})}
}

func (m Model) ensureWeb() Model {
	if m.web == nil {
		m.web = newWebHostState()
	}
	return m
}

// switcherFor is the function the host calls when the page switches: it
// hands the request to Update and waits for the answer (bounded by the
// page's request context).
func switcherFor(w *webHostState) func(ctx context.Context, path string) error {
	switches, stop := w.switches, w.stop
	return func(ctx context.Context, path string) error {
		reply := make(chan error, 1)
		select {
		case switches <- webSwitchRequestMsg{path: path, reply: reply}:
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

// waitWebSwitchCmd waits for the page's next switch request; nil once the
// page is closed.
func waitWebSwitchCmd(w *webHostState) tea.Cmd {
	switches, stop := w.switches, w.stop
	return func() tea.Msg {
		select {
		case req := <-switches:
			return req
		case <-stop:
			return nil
		}
	}
}

// onWebSwitchRequest is the page switching the terminal: refused (with the
// steer refusal's English reason, shown on the page) while the terminal is
// busy, else the ordinary reRoot — which moves the page along — with the
// answer held until the host has followed (webRerootMsg).
func (m Model) onWebSwitchRequest(msg webSwitchRequestMsg) (Model, tea.Cmd) {
	if !m.webServing() {
		msg.reply <- errors.New("the terminal is not serving this page")
		return m, nil
	}
	rearm := waitWebSwitchCmd(m.web)
	m = m.closeIdleSettings()
	if why := m.steerRefusal(); why != "" {
		// A window or a prompt goes away with esc; a running operation or an
		// interactive process does not — no such advice for those.
		hint := ""
		if m.opsIdle() && m.proc == nil {
			hint = " — press esc in the terminal, then switch again"
		}
		msg.reply <- errors.New("the terminal is busy: " + why + hint)
		return m, rearm
	}
	m.web.pendingSwitch = append(m.web.pendingSwitch, msg.reply)
	home, status := m.home, m.statusMsg
	nm, cmd := m.guardedReRoot(msg.path, false) // a worktree of this repo: the fast path
	m = nm.(Model)
	if !m.loading && m.home == home {
		// Neither a reroot nor an adopt: the worktree already on screen (nothing
		// for the host to follow), or a refusal said on the status line.
		var err error
		if m.statusMsg != status {
			err = errors.New(m.statusMsg)
		}
		m.web.pendingSwitch = m.web.pendingSwitch[:len(m.web.pendingSwitch)-1]
		msg.reply <- err
		return m, tea.Batch(cmd, rearm)
	}
	m.statusMsg = i18n.T("switched from the web page")
	return m, tea.Batch(cmd, rearm)
}

// closeIdleSettings pops the Settings windows (the menu and its Web page
// popup) off the top of the layer stack before a page's switch, unless a
// field is being typed into there. The user typically read the page's URL in
// Settings and went to the browser: that window must not refuse the page.
// Every other window still does (steerRefusal).
func (m Model) closeIdleSettings() Model {
	for {
		switch l := m.topLayer().(type) {
		case *webSettingsPopup:
			if l.editing {
				return m
			}
		case *settingsPopup:
			if l.ratesEditing || l.opsHistEditing {
				return m
			}
		default:
			return m
		}
		m = m.popLayer()
	}
}

// onWebReroot is the host having followed a re-root: answer the page
// switches waiting on it.
func (m Model) onWebReroot(msg webRerootMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("web page: %s", msg.err.Error())
	}
	if m.web != nil {
		for _, r := range m.web.pendingSwitch {
			r <- msg.err
		}
		m.web.pendingSwitch = nil
	}
	return m, nil
}

// webAddr is the address to bind: the launch flag, else [web] addr, else ""
// (the host's random loopback port).
func (m Model) webAddr() string {
	if m.webOpts.WebAddr != "" {
		return m.webOpts.WebAddr
	}
	return m.cfg.Web.Addr
}

// webServing reports a running hosted page.
func (m Model) webServing() bool { return m.web != nil && m.web.host != nil }

func startWebCmd(svc *domain.Service, w *webHostState, addr string, open bool) tea.Cmd {
	switcher, starter := switcherFor(w), sessionStarterFor(w)
	return func() tea.Msg {
		h := NewWebHost(svc)
		h.SetSwitcher(switcher)
		h.SetSessionStarter(starter)
		url, err := h.Start(context.Background(), addr)
		if err != nil {
			return webStartedMsg{err: err, open: open}
		}
		return webStartedMsg{host: h, url: url, open: open}
	}
}

// openInBrowser is the palette's "Open in browser": start serving on the
// first use, reopen the browser afterwards.
func (m Model) openInBrowser() (Model, tea.Cmd) {
	m = m.ensureWeb()
	switch {
	case NewWebHost == nil:
		m.statusMsg = i18n.T("this gg cannot serve a web page")
		return m, nil
	case m.web.host != nil:
		m.web.host.OpenBrowser()
		m.statusMsg = i18n.T("web page: %s", m.web.url)
		return m, nil
	case m.web.starting:
		m.statusMsg = i18n.T("web page: starting…")
		return m, nil
	}
	m.web.starting = true
	m.statusMsg = i18n.T("web page: starting…")
	return m, startWebCmd(m.homeSvc(), m.web, m.webAddr(), true)
}

// startupWebCmd serves at launch when [web] serve or --web asks for it (the
// browser is not opened). nil otherwise.
func (m Model) startupWebCmd() tea.Cmd {
	if m.quiet {
		return nil // headless: never-ending (headless.go)
	}
	if NewWebHost == nil || !(m.cfg.Web.Serve || m.webOpts.Web) || m.web == nil || m.web.host != nil || m.web.starting {
		return nil
	}
	m.web.starting = true // a pointer: the flag survives the value copy
	return startWebCmd(m.homeSvc(), m.web, m.webAddr(), false)
}

func (m Model) onWebStarted(msg webStartedMsg) (Model, tea.Cmd) {
	m = m.ensureWeb()
	m.web.starting = false
	pending := m.web.pendingServe
	m.web.pendingServe = nil
	var cmds []tea.Cmd
	if msg.err != nil {
		m.statusMsg = i18n.T("web page: %s", msg.err.Error())
		for _, c := range pending {
			cmds = append(cmds, m.answerSteer(c, steerFail(c, msg.err.Error())))
		}
		return m, tea.Batch(cmds...)
	}
	m.web.host, m.web.url = msg.host, msg.url
	m.statusMsg = i18n.T("web page: serving %s", msg.url)
	if msg.open {
		msg.host.OpenBrowser()
	}
	for _, c := range pending {
		cmds = append(cmds, m.answerSteer(c, steerOK(c, msg.url)))
	}
	cmds = append(cmds, waitWebSwitchCmd(m.web))  // the page may now switch the terminal
	cmds = append(cmds, waitWebSessionCmd(m.web)) // …and start sessions in it
	return m, tea.Batch(cmds...)
}

// steerServe is the "serve" inbox command (`gg open --web` with a live TUI):
// start the hosted page if needed and answer with its URL. Never refused
// for the user's state — it moves nothing on screen. A start already in
// flight parks the command; onWebStarted answers it.
func (m Model) steerServe(c steer.Command) (Model, tea.Cmd) {
	m = m.ensureWeb()
	switch {
	case NewWebHost == nil:
		return m, m.answerSteer(c, steerFail(c, "this gg cannot serve a web page"))
	case m.web.host != nil:
		return m, m.answerSteer(c, steerOK(c, m.web.url))
	}
	m.web.pendingServe = append(m.web.pendingServe, c)
	if m.web.starting {
		return m, nil
	}
	m.web.starting = true
	return m, startWebCmd(m.homeSvc(), m.web, m.webAddr(), false)
}

// webStatusText is the Settings row value.
func (m Model) webStatusText() string {
	switch {
	case m.webServing():
		return m.web.url
	case m.web != nil && m.web.starting:
		return i18n.T("starting…")
	}
	return i18n.T("not running")
}

// rerootWebCmd hands the TUI's new Service to the page (reRoot).
func rerootWebCmd(h WebHost, svc *domain.Service) tea.Cmd {
	return func() tea.Msg { return webRerootMsg{err: h.Reroot(context.Background(), svc)} }
}

// webRerootCmd is reRoot's hook: nil when no page is served.
func (m Model) webRerootCmd() tea.Cmd {
	if m.quiet {
		return nil // headless: never-ending (headless.go)
	}
	if !m.webServing() {
		return nil
	}
	return rerootWebCmd(m.web.host, m.homeSvc()) // the page follows gg's own worktree, never a console's view
}

// closeWeb ends the hosted page (Run's exit path). The pages get their
// "shutdown" message and paint the server-down bar.
func (m Model) closeWeb() {
	if m.webServing() {
		m.web.host.Close()
	}
	if m.web != nil && m.web.stop != nil {
		select {
		case <-m.web.stop:
		default:
			close(m.web.stop)
		}
	}
}
