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
	"github.com/homeend/gigagit/internal/model"
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
		mux.HandleFunc("GET /api/session/start-at", s.handleStartAt)
	})
}

// startAtBody is GET /api/session/start-at's answer: the pending command, or
// nothing. Always a JSON object — the page's getJSON decodes every answer.
type startAtBody struct {
	Steer *steerWire `json:"steer,omitempty"`
}

// setStartAt records the command `gg open --web` started this server with. It
// goes through toSteerWire like a posted steer — the link was resolved by the
// CLI, but the page must never be handed a value the allowlists would refuse
// — so a bad command is a startup error, not a silent no-op in the browser.
func (s *Server) setStartAt(c steer.Command) error {
	w, err := toSteerWire(c)
	if err != nil {
		return err
	}
	s.freezePair(context.Background(), &w)
	s.startAtMu.Lock()
	s.startAt = &w
	s.startAtMu.Unlock()
	return nil
}

// handleStartAt hands the start-at to the page ONCE. The first tab to finish
// its full load lands on it; a reload or a second tab gets {} and stays where
// it is (the TUI's --at fires once too). It needs no inbox: this is the
// server's own command, not an agent's.
func (s *Server) handleStartAt(w http.ResponseWriter, r *http.Request) {
	s.startAtMu.Lock()
	at := s.startAt
	s.startAt = nil
	s.startAtMu.Unlock()
	// A GET that consumes state must never be served from a cache: a
	// back/forward restore replaying the first answer would land the page
	// twice.
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, startAtBody{Steer: at})
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
	Ref     string   `json:"ref,omitempty"`
	A       string   `json:"a,omitempty"`
	B       string   `json:"b,omitempty"`
	Side    string   `json:"side,omitempty"`
	Line    int      `json:"line,omitempty"`
	Step    string   `json:"step,omitempty"`
	Sources []string `json:"sources,omitempty"`
	Panel   string   `json:"panel,omitempty"`
	Start   int      `json:"start,omitempty"`
	End     int      `json:"end,omitempty"`
	Tone    string   `json:"tone,omitempty"`
	// HintKind/HintID name the UI surface a navigate's link was copied from
	// (spec §3.3) — "bookmark", "shelf" or "stash", the closed set
	// model.LinkHint's grammar already validated, so the page can reveal it
	// (or degrade with a notice for "stash", which has no producer) exactly
	// as steer.Command carries it.
	HintKind string `json:"hint_kind,omitempty"`
	HintID   string `json:"hint_id,omitempty"`
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

// freezePair turns a pair target's halves into FULL commit ids wherever they
// resolve. The link resolver already sends ids (ruling R2), but a hand-written
// `gg session navigate` may name a branch for one half — and the page lands
// only a two-id pair through the server's one door (/api/compare-links), where
// a `-u` stash's untracked file is a member and the pair's review notes arm. A
// MIXED pair used to fall to the endpoint-shaped lane and lose both. A half
// that does not resolve is left as it came: the page's own lane reports it,
// exactly as before. Best-effort by design — a steer is never refused here.
func (s *Server) freezePair(ctx context.Context, w *steerWire) {
	if w.State != "pair" {
		return
	}
	svc := s.service()
	for _, half := range []*string{&w.A, &w.B} {
		if isFullSha(*half) {
			continue
		}
		if sha, ok, err := svc.ResolveRev(ctx, *half); err == nil && ok {
			*half = sha
		}
	}
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
		switch c.Target.State {
		case "preview":
			// A preview target is NOT a note state: it names a branch pair, and
			// the page resolves the tip itself. Handled before noteState, whose
			// allowlist has no entry for it.
			if c.Target.Source == "" || c.Target.Target == "" {
				return w, errors.New("a preview target needs source and target")
			}
			if !isGitArgSafe(c.Target.Source) || !isGitArgSafe(c.Target.Target) {
				return w, errors.New("unsafe preview branch")
			}
			if c.Commit != "" {
				return w, errors.New("a preview target cannot also carry a commit")
			}
			w.State, w.Source, w.Target = "preview", c.Target.Source, c.Target.Target
		case "ref":
			// A ref target is not a note state either: it names a branch or tag
			// TIP, and the page resolves it itself (ruling R2) rather than
			// receiving a sha that might already be stale by the time it lands.
			// Emptiness is refused in the navigate case below, beside the commit
			// arm's identical rule — this is only the argv-injection guard, which
			// must run whether or not Ref is later found empty.
			if c.Target.Ref != "" && !isGitArgSafe(c.Target.Ref) {
				return w, errors.New("unsafe ref")
			}
			w.State, w.Ref = "ref", c.Target.Ref
		case "pair":
			// A pair target names a CHANGE-SET's two halves, resolved by the page
			// itself for the same reason a ref is. Emptiness is refused in the
			// navigate case below, beside the ref and commit arms' identical rule.
			if c.Target.A != "" && !isGitArgSafe(c.Target.A) {
				return w, errors.New("unsafe pair half")
			}
			if c.Target.B != "" && !isGitArgSafe(c.Target.B) {
				return w, errors.New("unsafe pair half")
			}
			w.State, w.A, w.B = "pair", c.Target.A, c.Target.B
		default:
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
	// The hint's closed set (model.LinkHint's grammar already closed it:
	// "bookmark", "shelf" or "stash") is validated regardless of Cmd, like
	// Target/Line above — an unknown kind is a wire refusal, never a notice,
	// because the grammar already closed that set (S13 point 2).
	if c.HintKind != "" {
		if !model.LinkHintKindOK(c.HintKind) {
			return w, fmt.Errorf("unknown hint kind %q", c.HintKind)
		}
		if c.HintID == "" {
			return w, errors.New("a hint needs an id")
		}
		if c.HintKind == model.ContentHintKind {
			// The page has no content viewer yet (spec: v1 lands content
			// links in the TUI only).
			return w, errors.New("content links are not supported in gg web yet")
		}
		w.HintKind, w.HintID = c.HintKind, c.HintID
	}
	switch c.Cmd {
	case "navigate":
		switch c.Step {
		case "", "next_note", "prev_note":
		default:
			return w, fmt.Errorf("unknown step %q", c.Step)
		}
		// A preview/ref/pair with no file is a REVEAL — one navigate shape
		// that names a place (the Previews entry, a branch tip, a change-set)
		// without naming a file or a commit. A hint-only navigate is another
		// (S13): no File, no Commit, no Target at all — the reveal IS the
		// landing.
		if c.File == "" && c.Commit == "" && c.Step == "" && c.HintKind == "" &&
			w.State != "preview" && w.State != "ref" && w.State != "pair" {
			return w, errors.New("navigate needs a file, a commit or a step")
		}
		// state "commit" with no sha names no commit at all: the page would
		// open /api/commit/ and 404 on its own.
		if w.State == "commit" && w.Commit == "" {
			return w, errors.New("a commit target needs a commit")
		}
		// The NAME is load-bearing: steerNavigate's resolveRefTip resolves it at
		// apply time (ruling R2), so an empty one has nothing to resolve.
		if w.State == "ref" && w.Ref == "" {
			return w, errors.New("a ref target needs ref")
		}
		// Both halves are load-bearing: a half-filled pair would silently
		// degrade into "some commit", exactly the preview target's rule.
		if w.State == "pair" && (w.A == "" || w.B == "") {
			return w, errors.New("a pair target needs a and b")
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
	s.freezePair(readCtx(r), &wire)
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
