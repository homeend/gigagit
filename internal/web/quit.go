package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// The TUI's quit guard (internal/tui/quit_guard.go) for a standalone gg web:
// quitting while agents or AI tasks are live asks first. A standalone server
// quits on Ctrl+C in its terminal, so the first Ctrl+C with live work is
// held — the terminal says so and every open page shows the ask in its
// switcher — and a second Ctrl+C, or the page's Q, ends the work: tasks
// first, then sessions, within killAllGrace. A hosted page never asks: its
// TUI owns quitting.

// killAllGrace bounds how long quitting waits for the work to end (the
// TUI's own bound).
const killAllGrace = 5 * time.Second

type quitGuard struct {
	live    func() int
	kill    func(context.Context)
	say     func(string)
	changed func()

	mu       sync.Mutex
	asking   bool
	quitting bool
	stopping bool // the work is being ended (end ran): every tab says so
	confirm  chan struct{}
	once     sync.Once
}

func newQuitGuard(live func() int, kill func(context.Context), say func(string), changed func()) *quitGuard {
	return &quitGuard{live: live, kill: kill, say: say, changed: changed, confirm: make(chan struct{}, 1)}
}

// defaultQuitGuard ends the process's real work and tells srv's pages.
func defaultQuitGuard(srv *Server) *quitGuard {
	return newQuitGuard(domain.LiveWork, killAllWork,
		func(s string) { fmt.Fprintln(os.Stderr, s) },
		func() {
			if h := srv.liveHubRef(); h != nil {
				h.fanOut(liveMsg{Changed: []string{}, Reason: "quit"})
			}
		})
}

// killAllWork: tasks first (their records say cancelled), then sessions —
// the TUI's killAllAndQuitCmd and its run.go safety net.
func killAllWork(ctx context.Context) {
	domain.Tasks().KillAll(ctx)
	domain.Sessions().KillAll(ctx)
}

// run waits for a quit and returns once it may proceed (the work ended when
// it was asked about). A cancelled ctx returns at once without asking; the
// caller's safety net ends what is left.
func (g *quitGuard) run(ctx context.Context, sig <-chan os.Signal) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-g.confirm:
			g.end()
			return
		case <-sig:
			g.mu.Lock()
			if g.asking || g.quitting {
				g.quitting = true
				g.mu.Unlock()
				g.end()
				return
			}
			n := g.live()
			if n == 0 {
				g.quitting = true
				g.mu.Unlock()
				return
			}
			g.asking = true
			g.mu.Unlock()
			g.say(fmt.Sprintf("gg web: agents and AI tasks still running: %d — press Ctrl+C again to end them and quit", n))
			g.changed()
		}
	}
}

// end ends the live work once, however many confirmations raced.
func (g *quitGuard) end() {
	g.once.Do(func() {
		g.mu.Lock()
		g.stopping = true
		g.mu.Unlock()
		g.say("gg web: ending agent sessions…")
		g.changed() // every tab leaves quit mode: a cancel now would be refused

		ctx, cancel := context.WithTimeout(context.Background(), killAllGrace)
		defer cancel()
		g.kill(ctx)
	})
}

// state is what a page shows: whether the ask is up, and how much is live.
func (g *quitGuard) state() (asking bool, live int) {
	g.mu.Lock()
	asking = g.asking && !g.quitting
	g.mu.Unlock()
	if asking {
		live = g.live()
	}
	return asking, live
}

// ending reports a confirmed quit whose work is being ended.
func (g *quitGuard) ending() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stopping
}

var (
	errNotAsking = errors.New("gg web is not asking to quit")
	errQuitting  = errors.New("gg web is already quitting")
)

// request is the page's own quit, confirmed on the page: it ends the work
// and quits as a confirmed ask does, whether or not an ask is up.
func (g *quitGuard) request() error {
	g.mu.Lock()
	if g.quitting {
		g.mu.Unlock()
		return errQuitting
	}
	g.quitting = true
	g.mu.Unlock()
	g.say("gg web: quit from the web page")
	g.confirm <- struct{}{} // buffered: quitting is set once, so one send
	return nil
}

// answer is the page's reply: confirm ends the work and quits; cancel takes
// the ask down (the next Ctrl+C asks again — the TUI's esc).
func (g *quitGuard) answer(confirm bool) error {
	g.mu.Lock()
	if !g.asking || g.quitting {
		g.mu.Unlock()
		return errNotAsking
	}
	if confirm {
		g.quitting = true
		g.mu.Unlock()
		g.confirm <- struct{}{} // buffered: quitting is set once, so one send
		return nil
	}
	g.asking = false
	g.mu.Unlock()
	g.changed()
	return nil
}

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/quit", s.handleQuitGet)
		mux.HandleFunc("POST /api/quit", writeGuard(s.handleQuitPost))
	})
}

// handleQuitGet: the Ctrl+C ask (asking/live/ending), plus what the ☰ quit
// would end (running: the process's live agents and AI tasks) and who quits
// with it (hosted: the terminal UI).
func (s *Server) handleQuitGet(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"asking": false, "live": 0, "ending": false, "hosted": s.hosted, "running": domain.LiveWork()}
	if s.quit != nil && !s.hosted {
		a, n := s.quit.state()
		out["asking"], out["live"], out["ending"] = a, n, s.quit.ending()
		out["running"] = s.quit.live()
	}
	writeJSON(w, out)
}

// handleQuitPost answers the ask ({"confirm":…}): refused unless it is up,
// and always on a hosted page. {"now":true} is the page's own quit, already
// confirmed there: a standalone server quits, a hosted page asks its
// terminal to quit (refused without one).
func (s *Server) handleQuitPost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm bool `json:"confirm"`
		Now     bool `json:"now"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Now {
		s.handleQuitNow(w, r)
		return
	}
	if s.hosted || s.quit == nil {
		writeErr(w, http.StatusConflict, errors.New("this page cannot quit gg — quit from the terminal"))
		return
	}
	if err := s.quit.answer(req.Confirm); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleQuitNow(w http.ResponseWriter, r *http.Request) {
	var err error
	switch {
	case s.hosted:
		s.mu.Lock()
		quitter := s.quitter
		s.mu.Unlock()
		if quitter == nil {
			err = errors.New("this page cannot quit gg — quit from the terminal")
		} else {
			err = quitter(r.Context())
		}
	case s.quit == nil:
		err = errors.New("this page cannot quit gg — quit from the terminal")
	default:
		err = s.quit.request()
	}
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
