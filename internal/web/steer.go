package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// Live steering, the web half. `gg web` claims this worktree's web.json
// presence beside the TUI's tui.json and keeps its mtime fresh; a
// `gg session …` command that finds it POSTs the command here (the page is a
// browser, not a file-draining consumer — the web NEVER reads the inbox). The
// command is validated through the same allowlists the notes lane uses and
// then handed to every open tab on the live hub.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/session/steer", writeGuard(s.handleSteer))
	})
}

// steerPresenceTick is how often the page's presence mtime is refreshed.
// Liveness IS the mtime, and steer.LiveWindow is five seconds.
const steerPresenceTick = time.Second

// steerWire is the validated command the page receives on the live hub. Every
// field has passed the same allowlists the notes lane uses, so the page can
// hand these values straight to its openers.
type steerWire struct {
	Cmd     string   `json:"cmd"`
	File    string   `json:"file,omitempty"`
	State   string   `json:"state,omitempty"`
	Commit  string   `json:"commit,omitempty"`
	Source  string   `json:"source,omitempty"`
	Target  string   `json:"target,omitempty"`
	Side    string   `json:"side,omitempty"`
	Line    int      `json:"line,omitempty"`
	Step    string   `json:"step,omitempty"`
	Sources []string `json:"sources,omitempty"`
	Panel   string   `json:"panel,omitempty"`
	Start   int      `json:"start,omitempty"`
	End     int      `json:"end,omitempty"`
	Tone    string   `json:"tone,omitempty"`
}

// steerPanels is the protocol's panel vocabulary — internal/tui's
// panelProtoName, which a session snapshot writes and `gg session focus`
// carries. The page has only two panes and no-ops on the rest, but the
// ENDPOINT speaks the whole protocol: a name gg does not have is refused here
// so an agent hears about its typo instead of watching nothing happen.
var steerPanels = map[string]bool{
	"branches": true, "worktrees": true, "remotes": true, "files": true,
	"staged": true, "commits": true, "tags": true, "reflog": true, "previews": true,
}

// toSteerWire validates one posted command. Untrusted values reach git argv
// through the page's own fetches, so `file` and `commit` go through
// isGitArgSafe and every enum through its allowlist — an unknown value is a
// 400, never a silent default. Fields that belong to ONE command (the band's
// start/end/tone) are copied only by that command's arm, so the page can never
// be handed a band to paint by a navigate.
func toSteerWire(c steer.Command) (steerWire, error) {
	w := steerWire{Cmd: c.Cmd, File: c.File, Commit: c.Commit, Step: c.Step,
		Sources: c.Sources, Panel: c.Panel}
	if c.File != "" && !isGitArgSafe(c.File) {
		return w, errors.New("unsafe file")
	}
	if c.Commit != "" && !isGitArgSafe(c.Commit) {
		return w, errors.New("unsafe commit")
	}
	if c.Target != nil {
		if c.Target.State == "preview" {
			// A preview target is NOT a note state: it names a branch pair, and
			// the page resolves the tip itself. Handled before noteState, whose
			// allowlist has no entry for it.
			if c.Target.Source == "" || c.Target.Target == "" {
				return w, errors.New("a preview target needs source and target")
			}
			if !isGitArgSafe(c.Target.Source) || !isGitArgSafe(c.Target.Target) {
				return w, errors.New("unsafe preview branch")
			}
			w.State, w.Source, w.Target = "preview", c.Target.Source, c.Target.Target
		} else {
			if _, ok := noteState(c.Target.State); !ok {
				return w, fmt.Errorf("unknown state %q", c.Target.State)
			}
			if c.Target.Commit != "" && !isGitArgSafe(c.Target.Commit) {
				return w, errors.New("unsafe target commit")
			}
			w.State = c.Target.State
			if w.State == "" {
				w.State = "unstaged"
			}
			if c.Target.Commit != "" {
				w.Commit = c.Target.Commit
			}
		}
	}
	if c.Line != nil {
		if _, ok := noteSide(c.Line.Side); !ok {
			return w, fmt.Errorf("unknown side %q", c.Line.Side)
		}
		if c.Line.No < 1 {
			return w, errors.New("line must be 1-based")
		}
		w.Side, w.Line = c.Line.Side, c.Line.No
		if w.Side == "" {
			w.Side = "new"
		}
	}
	switch c.Cmd {
	case "navigate":
		switch c.Step {
		case "", "next_note", "prev_note":
		default:
			return w, fmt.Errorf("unknown step %q", c.Step)
		}
		// A preview with no file is a REVEAL of the Previews entry — the one
		// navigate shape that names a place without naming a file or a commit.
		if c.File == "" && c.Commit == "" && c.Step == "" && w.State != "preview" {
			return w, errors.New("navigate needs a file, a commit or a step")
		}
		// state "commit" with no sha names no commit at all: the page would
		// open /api/commit/ and 404 on its own.
		if w.State == "commit" && w.Commit == "" {
			return w, errors.New("a commit target needs a commit")
		}
	case "reload":
		if len(w.Sources) == 0 {
			w.Sources = []string{"notes"}
		}
		for _, n := range w.Sources {
			switch n {
			case "notes", "status", "all":
			default:
				return w, fmt.Errorf("unknown reload source %q", n)
			}
		}
	case "focus":
		if c.Panel == "" {
			return w, errors.New("focus needs a panel")
		}
		if !steerPanels[c.Panel] {
			return w, fmt.Errorf("unknown panel %q", c.Panel)
		}
	case "highlight":
		if c.File == "" {
			return w, errors.New("highlight needs a file")
		}
		if c.Start < 1 || (c.End != 0 && c.End < c.Start) {
			return w, errors.New("highlight needs a 1-based start and an end at or after it")
		}
		w.Start, w.End, w.Tone = c.Start, c.End, c.Tone
		if _, ok := noteSide(c.Side); !ok {
			return w, fmt.Errorf("unknown side %q", c.Side)
		}
		w.Side = c.Side
		if w.Side == "" {
			w.Side = "new"
		}
		switch c.Tone {
		case "info", "warn", "error":
		default:
			return w, fmt.Errorf("unknown tone %q", c.Tone)
		}
		if w.End == 0 {
			w.End = w.Start
		}
	case "highlight_clear":
	default:
		return w, fmt.Errorf("unknown command %q", c.Cmd)
	}
	return w, nil
}

// handleSteer takes one command from `gg session` and hands it to every open
// page. It answers 202 at once — there is no page acknowledgement, and the CLI
// prints "web: sent".
func (s *Server) handleSteer(w http.ResponseWriter, r *http.Request) {
	if s.steerInbox() == "" {
		http.NotFound(w, r) // steering is off: the CLI must see no session here
		return
	}
	var c steer.Command
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, steer.MaxCommandBytes)).Decode(&c); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	wire, err := toSteerWire(c)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// The hub drops everything while an op is in flight. A steer must not
	// vanish that way, so refuse it out loud instead — the CLI prints
	// "operation in flight" and exits 1.
	if s.opInFlight() {
		writeErr(w, http.StatusConflict, errors.New("operation in flight"))
		return
	}
	if h := s.liveHubRef(); h != nil {
		h.emitSteer(liveMsg{Changed: []string{}, Reason: "steer", Steer: &wire})
	}
	w.WriteHeader(http.StatusAccepted)
}

// resolveSteerDir resolves a service's inbox and its worktree. (Named apart
// from internal/cli's steerDirFor(svc) and internal/tui's
// steerDirFor(commonDir, worktree): three packages, three shapes, one
// concept.) A "" dir means steering is off for this server (not a repo, no
// state home, or the [ui] agent_steering key turned off) — every consumer
// reads it that way.
func resolveSteerDir(ctx context.Context, svc *domain.Service) (dir, worktree string) {
	cd, err := svc.GitCommonDir(ctx)
	if err != nil {
		return "", ""
	}
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return "", ""
	}
	if cfg, cerr := svc.EffectiveConfig(ctx); cerr == nil && !cfg.UI.SteeringOn() {
		return "", ""
	}
	return config.SessionSteerDir(cd, top), top
}

// initSteerPresence claims this worktree's web presence once the listener's URL
// is known, and keeps its mtime fresh until ctx ends.
func (s *Server) initSteerPresence(ctx context.Context, url string) {
	dir, wt := resolveSteerDir(ctx, s.service())
	s.steerMu.Lock()
	s.steerURL, s.steerDir, s.steerWorktree = url, dir, wt
	s.steerMu.Unlock()
	if dir != "" {
		s.claimSteerPresence()
	}
	// The ticker starts even when steering resolved OFF here. A re-root can
	// hand this server an inbox later (rehomeSteerPresence fills steerDir),
	// and a presence nobody refreshes drops out of steer.LiveWindow five
	// seconds after it is written — `gg session status` would stop seeing the
	// page. An idle tick costs one mutex read: touchSteerPresence is a no-op
	// while the dir is empty.
	go s.steerPresenceLoop(ctx)
}

// steerPresenceLoop refreshes the presence every steerPresenceTick until ctx
// ends. It reads the dir on EVERY tick rather than closing over the one the
// server booted with, so a re-root is picked up without restarting anything.
func (s *Server) steerPresenceLoop(ctx context.Context) {
	t := time.NewTicker(steerPresenceTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.touchSteerPresence()
		}
	}
}

// claimSteerPresence takes the inbox's web.json for THIS run. The remove is
// not redundant: Touch only refreshes the mtime of a file that already exists,
// so a web.json left by a previous `gg web` would keep its payload — and the
// port in it is dead, because a new run binds a new one.
func (s *Server) claimSteerPresence() {
	s.steerMu.Lock()
	dir := s.steerDir
	s.steerMu.Unlock()
	if dir == "" {
		return
	}
	steer.Remove(dir, steer.WebPresence)
	s.touchSteerPresence()
}

// touchSteerPresence refreshes the page's presence: one os.Chtimes per second,
// which is the whole "is a gg web page open" protocol.
func (s *Server) touchSteerPresence() {
	s.steerMu.Lock()
	dir, url, wt := s.steerDir, s.steerURL, s.steerWorktree
	s.steerMu.Unlock()
	if dir == "" {
		return
	}
	_ = steer.Touch(dir, steer.WebPresence, steer.Presence{
		PID:      os.Getpid(),
		Worktree: wt, // what `gg session status` prints
		Started:  time.Now().UTC().Format(time.RFC3339),
		URL:      url,
	})
}

// removeSteerPresence ends the claim at once rather than letting it age out.
// The dir is cleared with it, so the ticker (bound to the outer context, which
// an interrupt does not cancel) cannot recreate the file behind the shutdown.
func (s *Server) removeSteerPresence() {
	s.steerMu.Lock()
	dir := s.steerDir
	s.steerDir = ""
	s.steerMu.Unlock()
	if dir == "" {
		return
	}
	steer.Remove(dir, steer.WebPresence)
}

// rehomeSteerPresence moves the presence to the new repo on POST /api/reroot.
func (s *Server) rehomeSteerPresence(ctx context.Context, svc *domain.Service) {
	s.removeSteerPresence()
	dir, wt := resolveSteerDir(ctx, svc)
	s.steerMu.Lock()
	s.steerDir, s.steerWorktree = dir, wt
	s.steerMu.Unlock()
	s.claimSteerPresence()
}

// steerInbox reads the current inbox under the lock — "" means steering is off.
func (s *Server) steerInbox() string {
	s.steerMu.Lock()
	defer s.steerMu.Unlock()
	return s.steerDir
}
