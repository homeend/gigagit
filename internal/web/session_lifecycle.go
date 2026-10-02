package web

// Session lifecycle on the web (web attach, plan 2): the commands a start
// can run, the start itself, kill and remove.

import (
	"context"
	"errors"
	"net/http"
	"os/exec"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/promptstate"
	"github.com/homeend/gigagit/internal/template"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/session-commands", s.handleSessionCommands)
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
