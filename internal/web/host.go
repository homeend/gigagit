package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// ErrPageLive: another gg web page already serves this worktree (the wrapped
// error names its URL). A TUI host never overwrites a live presence — the
// standalone command keeps its old "a new run replaces the old file" rule.
var ErrPageLive = errors.New("another gg web page is already serving this worktree")

// Host is one served page over a caller-owned Service: the reusable core of
// gg web. Serve wraps it for the standalone command; the TUI hosts it
// in-process (cmd/gg wires tui.NewWebHost to NewHost), so the browser and
// the terminal read ONE agent-session manager — the reason this exists.
type Host struct {
	srv *Server
	// refuseForeign: a hosted page refuses to start over another live page's
	// web.json; standalone gg web replaces it (a second `gg web` in one
	// worktree was always allowed).
	refuseForeign bool

	mu      sync.Mutex
	url     string
	httpSrv *http.Server
	cancel  context.CancelFunc // ends the presence ticker and the hub's context
	done    chan struct{}      // closed when the listener goroutine returns
	once    sync.Once
}

// NewHost builds the server over svc. opener opens a Service for a path the
// server chooses itself (nil = domain.Open; a TUI passes domain.OpenTUI so
// an ssh prompt can never reach its raw-mode terminal). hosted marks the
// page as terminal-owned (the page's own re-root is refused, its switch
// affordances hide) and turns on the foreign-presence refusal.
func NewHost(svc *domain.Service, opener func(string) *domain.Service, hosted bool) *Host {
	s := New(svc)
	s.opener = opener
	s.hosted = hosted
	if hosted {
		s.docs = agentdocs.Shared() // the terminal's store: one set of agent notes
	}
	return newHostOver(s, hosted)
}

// newHostOver wraps an already-configured server (Serve builds its own).
func newHostOver(s *Server, refuseForeign bool) *Host {
	return &Host{srv: s, refuseForeign: refuseForeign, done: make(chan struct{})}
}

// Start binds addr ("" = 127.0.0.1:0, loopback only), starts the live hub,
// the open-files watch and the steering presence (web.json with the URL),
// and serves on a goroutine until Close. The hosted server does NOT apply
// the config's UI policies here: the TUI pushes them onto the shared
// Service itself.
func (h *Host) Start(ctx context.Context, addr string) (string, error) {
	if h.refuseForeign {
		if dir, _ := resolveSteerDir(ctx, h.srv.service()); dir != "" {
			if p, live := steer.Live(dir, steer.WebPresence); live && p.PID != os.Getpid() {
				return "", fmt.Errorf("%w at %s", ErrPageLive, p.URL)
			}
		}
	}
	ln, url, err := listen(addr)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithCancel(ctx)
	h.mu.Lock()
	h.url, h.cancel = url, cancel
	h.httpSrv = &http.Server{Handler: h.srv.Handler()}
	httpSrv := h.httpSrv
	h.mu.Unlock()
	h.srv.startLive(ctx)        // watcher + interval ticker behind GET /api/events
	h.srv.startOpenFilesWatch() // the open files follow the disk (openfiles_watch.go)
	h.srv.attachDocs(ctx)       // a standalone server reads the worktree's docs directory (session_docs.go)
	h.srv.startDocsFollow()     // …and the agent-docs store (agentdocs_follow.go)
	// The live-steering claim: web.json carries THIS run's URL, so a
	// `gg session …`/`gg open --web` in any shell on this worktree can reach
	// the page.
	h.srv.initSteerPresence(ctx, url)
	go func() {
		defer close(h.done)
		_ = httpSrv.Serve(ln) // http.ErrServerClosed after Close
	}()
	return url, nil
}

// Reroot points the page at svc — the TUI's new repository after a switch.
// The old repo's web.json moves with it.
func (h *Host) Reroot(ctx context.Context, svc *domain.Service) error {
	return h.srv.adoptService(ctx, svc)
}

// SetSwitcher installs the terminal's switch: the page's own re-root
// (palette, worktree menu, locks) resolves and preflights the target, then
// calls fn, which re-roots the terminal and — through Reroot — this page
// before returning, or refuses with a reason the page shows. Without it a
// hosted page's re-root is refused.
func (h *Host) SetSwitcher(fn func(ctx context.Context, path string) error) {
	h.srv.SetSwitcher(fn)
}

// SetSessionStarter installs the terminal's session start: the page's Start
// agent / Open terminal is validated and approved here, then fn starts it
// the way the terminal does (its inbox, its agent channel) and returns the
// new session's id. Without it the page's server starts sessions itself.
func (h *Host) SetSessionStarter(fn func(ctx context.Context, req domain.SessionStartRequest) (domain.SessionID, error)) {
	h.srv.SetSessionStarter(fn)
}

// SetQuitter installs the terminal's quit: the page's ☰ quit (confirmed on
// the page) calls fn, which answers and then quits the terminal — closing
// this page with it — or refuses with a reason. Without it a hosted page
// cannot quit.
func (h *Host) SetQuitter(fn func(ctx context.Context) error) {
	h.srv.SetQuitter(fn)
}

// SetActive tells the page whether it is the active interface. The terminal
// owns the state; call it before Start so no request slips through.
func (h *Host) SetActive(active bool) { h.srv.SetUIActive(active) }

// SetShownConsole names the session whose agent window the page opens when it
// becomes active ("" = none).
func (h *Host) SetShownConsole(id domain.SessionID) { h.srv.SetShownConsole(string(id)) }

// ShownConsole is the session whose agent window the page last showed.
func (h *Host) ShownConsole() domain.SessionID { return domain.SessionID(h.srv.ShownConsole()) }

// SetActivator installs the terminal's flip: a blocked page's Activate web UI
// and the page's Switch to TUI call fn, which flips (calling SetActive) or
// refuses with a reason the page shows.
func (h *Host) SetActivator(fn func(ctx context.Context, side string) error) {
	h.srv.SetActivator(fn)
}

// SetWorkspace tells the page what is open (nil = no workspace).
func (h *Host) SetWorkspace(info *domain.WorkspaceInfo) { h.srv.SetWorkspace(info) }

// SetWorkspaceQuestion holds the pending launch question for the page (nil = none).
func (h *Host) SetWorkspaceQuestion(q *domain.WorkspaceQuestion) { h.srv.SetWorkspaceQuestion(q) }

// MemberChanged tells the page a sleeping member's rows moved.
func (h *Host) MemberChanged(path string) { h.srv.MemberChanged(path) }

// SetWorkspaceActor installs the owner's answer to the page's workspace requests.
func (h *Host) SetWorkspaceActor(fn func(ctx context.Context, req domain.WorkspaceRequest) (domain.WorkspaceReply, error)) {
	h.srv.SetWorkspaceActor(fn)
}

// Clients is the number of connected tabs.
func (h *Host) Clients() int { return h.srv.uiClients() }

// Busy is why the page may not give up control now, "" when it may.
func (h *Host) Busy() string { return h.srv.uiBusy() }

// SetConfigChanged installs the terminal's config follow: after the page
// writes config (tools added, the per-repo file relocated) it calls fn, and
// the terminal re-reads what it caches — or a later terminal Settings write
// would go to a moved file. Fire-and-forget.
func (h *Host) SetConfigChanged(fn func(relocated bool)) {
	h.srv.SetConfigChanged(fn)
}

// URL is the served address ("" before Start).
func (h *Host) URL() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.url
}

// OpenBrowser opens the system browser at the URL (best-effort, no-op
// before Start).
func (h *Host) OpenBrowser() {
	if u := h.URL(); u != "" {
		openBrowser(u)
	}
}

// Close tells every tab the server is going (their last /api/events
// message is "shutdown", so they paint the server-down bar at once), shuts
// the listener down within shutdownGrace, releases the hub and the watchers,
// and removes web.json. Idempotent; safe before Start.
func (h *Host) Close() {
	h.once.Do(func() {
		h.mu.Lock()
		httpSrv, cancel := h.httpSrv, h.cancel
		h.mu.Unlock()
		if httpSrv != nil {
			h.srv.announceShutdown()
			sctx, scancel := context.WithTimeout(context.Background(), shutdownGrace)
			defer scancel()
			_ = httpSrv.Shutdown(sctx)
			<-h.done
		}
		h.srv.Close()
		h.srv.removeSteerPresence()
		if cancel != nil {
			cancel()
		}
	})
}
