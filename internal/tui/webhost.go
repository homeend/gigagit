package tui

import (
	"context"

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

func (m Model) ensureWeb() Model {
	if m.web == nil {
		m.web = &webHostState{}
	}
	return m
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

func startWebCmd(svc *domain.Service, addr string, open bool) tea.Cmd {
	return func() tea.Msg {
		h := NewWebHost(svc)
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
	return m, startWebCmd(m.svc, m.webAddr(), true)
}

// startupWebCmd serves at launch when [web] serve or --web asks for it (the
// browser is not opened). nil otherwise.
func (m Model) startupWebCmd() tea.Cmd {
	if NewWebHost == nil || !(m.cfg.Web.Serve || m.webOpts.Web) || m.web == nil || m.web.host != nil || m.web.starting {
		return nil
	}
	m.web.starting = true // a pointer: the flag survives the value copy
	return startWebCmd(m.svc, m.webAddr(), false)
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
	return m, startWebCmd(m.svc, m.webAddr(), false)
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
	if !m.webServing() {
		return nil
	}
	return rerootWebCmd(m.web.host, m.svc)
}

// closeWeb ends the hosted page (Run's exit path). The pages get their
// "shutdown" message and paint the server-down bar.
func (m Model) closeWeb() {
	if m.webServing() {
		m.web.host.Close()
	}
}
