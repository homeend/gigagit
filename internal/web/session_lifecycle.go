package web

// Session lifecycle on the web (web attach, plan 2): the commands a start
// can run, the start itself, kill and remove.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/promptstate"
	"github.com/homeend/gigagit/internal/template"
	"github.com/homeend/gigagit/internal/worktree"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/session-commands", s.handleSessionCommands)
		mux.HandleFunc("POST /api/session-start", writeGuard(s.handleSessionStart))
		mux.HandleFunc("POST /api/session-kill", writeGuard(s.handleSessionKill))
		mux.HandleFunc("POST /api/session-remove", writeGuard(s.handleSessionRemove))
	})
}

type sessionCommandWire struct {
	Name     string `json:"name"`
	Command  string `json:"command"` // resolved for the worktree: what will run
	Approved bool   `json:"approved"`
	Found    bool   `json:"found"`
}

// sessionWorktree answers the worktree a wire path names — one of the
// repository's own, as git lists it. Anything else is refused: the path
// becomes a process's working directory.
func (s *Server) sessionWorktree(ctx context.Context, svc *domain.Service, path string) (string, error) {
	wts, err := svc.Worktrees(ctx)
	if err != nil {
		return "", err
	}
	for _, w := range wts {
		if w.Path != "" && w.Path == path && !w.Bare {
			return w.Path, nil
		}
	}
	return "", errors.New("not a worktree of this repository")
}

// sessionCommandsFor is the effective config's web session commands. With
// no session command configured at all it runs the first-run detect, which
// appends the installed agents' safe templates to the GLOBAL config, and
// reloads; added names what it wrote (nil = nothing written).
func (s *Server) sessionCommandsFor(ctx context.Context, svc *domain.Service) (config.Config, []config.ToolCommand, []string, error) {
	cfg, err := s.effectiveConfig(ctx, svc)
	if err != nil {
		return cfg, nil, nil, err
	}
	added, err := domain.EnsureSessionCommands(cfg, config.DefaultGlobalPath(), s.detections)
	if err != nil {
		return cfg, nil, nil, err
	}
	if added != nil {
		if cfg, err = s.effectiveConfig(ctx, svc); err != nil {
			return cfg, nil, nil, err
		}
	}
	return cfg, domain.SessionCommands(cfg, "web"), added, nil
}

func (s *Server) programFound(tc config.ToolCommand) bool {
	look := s.lookPath
	if look == nil {
		look = exec.LookPath
	}
	prog := domain.SessionProgram(tc)
	if prog == "" {
		return false
	}
	_, err := look(prog)
	return err == nil
}

// handleSessionCommands lists what Start agent can run in ?worktree=. The
// first call on a machine with no session command detects the installed
// agents inside the request — the page shows its busy notice while it waits.
func (s *Server) handleSessionCommands(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	dir, err := s.sessionWorktree(r.Context(), svc, r.URL.Query().Get("worktree"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	_, cmds, added, err := s.sessionCommandsFor(r.Context(), svc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	approved := map[string]bool{}
	if store := s.promptStore(); store != nil {
		approved = store.ApprovedToolCommands(s.toolRepoKey(r.Context(), svc))
	}
	out := make([]sessionCommandWire, 0, len(cmds))
	for _, tc := range cmds {
		resolved, rerr := template.ResolveCommand(tc.Command, nil, template.CmdCtx{Repo: dir})
		if rerr != nil {
			continue // inert, as in every tool lane
		}
		out = append(out, sessionCommandWire{Name: tc.Name, Command: resolved,
			Approved: approved[promptstate.CommandHash(tc.Command)], Found: s.programFound(tc)})
	}
	writeJSON(w, map[string]any{"commands": out, "added": added, "config_path": config.DefaultGlobalPath()})
}

type sessionStartReq struct {
	Worktree string `json:"worktree"`
	Tool     string `json:"tool"`
	Terminal bool   `json:"terminal"`
	Approve  bool   `json:"approve"` // the user just approved this command
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
}

// startSize clamps the page's measured grid; a start that sends none gets a
// roomy default rather than the clamp's minimum.
func startSize(cols, rows int) (int, int) {
	if cols <= 0 || rows <= 0 {
		return 100, 30
	}
	return domain.ClampConsoleSize(cols, rows)
}

// sessionPlace is the TUI's sessionPlace, wire-shaped: where a session for
// dir runs. cwd "" = in dir; a worktree recorded under the other
// environment's notation runs in its translated path; an unreachable one is
// refused.
func (s *Server) sessionPlace(dir string) (cwd string, err error) {
	stat, goos := s.placeStat, s.placeGOOS
	if stat == nil {
		stat = func(p string) error { _, err := os.Stat(p); return err }
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	switch verdict, translated := worktree.CheckSwitchTarget(stat, goos, dir); verdict {
	case worktree.SwitchOK:
		return "", nil
	case worktree.SwitchRepairable:
		return translated, nil
	}
	return "", fmt.Errorf("cannot start here: %s is not reachable from here", dir)
}

// startSessionHere is the standalone server's own start: the child gets this
// page's steer inbox and no agent channel (no terminal hosts one).
func (s *Server) startSessionHere(svc *domain.Service, cfg config.Config, req domain.SessionStartRequest, cwd string) (domain.SessionID, error) {
	var env []string
	if inbox := s.steerInbox(); inbox != "" {
		env = []string{"GG_INBOX=" + inbox}
	}
	// The session outlives the request: never the request's context.
	ctx := context.Background()
	if req.Terminal {
		sess, err := svc.StartTerminal(ctx, cfg.Console.Shell, req.Worktree, cwd, req.Cols, req.Rows, env)
		if err != nil {
			return "", err
		}
		return sess.Info().ID, nil
	}
	sess, _, err := svc.StartAgentSession(ctx, req.Command, req.Worktree, cwd, req.Cols, req.Rows, env, "", domain.SpawnRecord{}, "")
	if err != nil {
		return "", err
	}
	return sess.Info().ID, nil
}

// handleSessionStart starts an agent (tool) or a terminal in a worktree. An
// unapproved command is refused with 403 + needs_approval and the resolved
// text; the page asks, then repeats with approve=true. A terminal runs the
// user's own shell and needs no approval, as in the TUI.
func (s *Server) handleSessionStart(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	var q sessionStartReq
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	dir, err := s.sessionWorktree(r.Context(), svc, q.Worktree)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// Reachability first: nobody should approve a command and then be told
	// the worktree is not there (the TUI's startAgentFor order).
	cwd, err := s.sessionPlace(dir)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	// Never the first-run detect here: that is the dialog's GET, where the
	// page says what was added. A start only runs what is configured.
	cfg, err := s.effectiveConfig(r.Context(), svc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	cmds := domain.SessionCommands(cfg, "web")
	req := domain.SessionStartRequest{Worktree: dir, Terminal: q.Terminal}
	req.Cols, req.Rows = startSize(q.Cols, q.Rows)
	if !q.Terminal {
		found := false
		for _, tc := range cmds {
			if tc.Name == q.Tool {
				req.Command, found = tc, true
				break
			}
		}
		if !found {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("no session command named %q", q.Tool))
			return
		}
		resolved, rerr := template.ResolveCommand(req.Command.Command, nil, template.CmdCtx{Repo: dir})
		if rerr != nil {
			writeErr(w, http.StatusBadRequest, rerr)
			return
		}
		key, store := s.toolRepoKey(r.Context(), svc), s.promptStore()
		hash := promptstate.CommandHash(req.Command.Command)
		if approved := store != nil && store.ApprovedToolCommands(key)[hash]; !approved {
			if !q.Approve {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": "this command has not been approved yet", "needs_approval": true,
					"tool": req.Command.Name, "command": resolved,
				})
				return
			}
			if store != nil {
				_ = store.ApproveToolCommand(key, hash) // best-effort: only costs another prompt
			}
		}
	}
	var id domain.SessionID
	if start := s.sessionStarter(); start != nil {
		// Bounded: a wedged terminal must not hang the page's request.
		d := s.startTimeout
		if d == 0 {
			d = 30 * time.Second
		}
		ctx, cancel := context.WithTimeout(r.Context(), d)
		id, err = start(ctx, req)
		cancel()
		if errors.Is(err, context.DeadlineExceeded) {
			err = errors.New("the terminal did not answer in time — the session list shows whether it started")
		}
	} else {
		id, err = s.startSessionHere(svc, cfg, req, cwd)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for _, sw := range s.sessionsNow() {
		if sw.ID == string(id) {
			out := map[string]any{"session": sw}
			if warn := domain.SessionRulesWarning(req.Command); warn != "" {
				out["warning"] = warn // the built-in rules apply; the page says so once
			}
			writeJSON(w, out)
			return
		}
	}
	writeErr(w, http.StatusInternalServerError, errors.New("the session ended before it could be shown"))
}

// handleSessionKill ends a running session; remove also forgets it once its
// exit is recorded (the TUI's X). The list change reaches every tab through
// the manager's signal.
func (s *Server) handleSessionKill(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ID     string `json:"id"`
		Remove bool   `json:"remove"`
	}
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sess, ok := sessionByID(w, q.ID)
	if !ok {
		return
	}
	if sess.Info().State != domain.SessionRunning {
		writeErr(w, http.StatusConflict, errors.New("the session has already exited"))
		return
	}
	id := domain.SessionID(q.ID)
	var err error
	if q.Remove {
		err = domain.Sessions().KillAndRemove(id)
	} else {
		err = domain.Sessions().Kill(id)
	}
	if err != nil {
		writeErr(w, http.StatusNotFound, err) // removed between the lookup and the kill
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleSessionRemove forgets an exited session.
func (s *Server) handleSessionRemove(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sess, ok := sessionByID(w, q.ID)
	if !ok {
		return
	}
	if sess.Info().State == domain.SessionRunning {
		writeErr(w, http.StatusConflict, errors.New("only an exited session can be removed — kill it first"))
		return
	}
	if err := domain.Sessions().Remove(domain.SessionID(q.ID)); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
