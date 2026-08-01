package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/promptstate"
	"github.com/homeend/gigagit/internal/template"
)

// AI conflict-complete: run a configured `conflict_complete`-category external
// agent headlessly against the currently paused sequencer op (merge/rebase/
// cherry-pick/revert). Unlike the TUI's terminal-handover variants, the web
// lane only ever offers headless (mode="capture") commands — there is no
// terminal to hand over a browser tab.
//
// This shares review.go's two load-bearing properties: the command text
// never comes off the wire (the client sends a tool NAME; the server resolves
// it from the effective config), and a command runs only after an explicit
// approval remembered per repo against a hash of the config TEMPLATE text —
// the same promptstate store and key the review lane and the TUI's lanes use.

// handleConflictTools answers GET /api/conflict/tools — 409 when nothing is
// paused; else the paused-op facts plus every runnable headless
// conflict_complete command, resolved for display with the real
// op/source/target/paths and the literal $GG_CONTEXT_FILE placeholder (the
// real path exists only at run time; catalog rows reference env vars and are
// unaffected).
func (s *Server) handleConflictTools(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	st, err := svc.Status(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	cs := svc.Conflict(r.Context(), st)
	if cs.Op == "" {
		writeErr(w, http.StatusConflict, errors.New("nothing is paused"))
		return
	}
	cmds, err := s.conflictCompleteCommands(r.Context(), svc, cs.Op)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	top, err := svc.TopLevel(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	files := unmergedStatusPaths(st)
	var approved map[string]bool
	if store := s.promptStore(); store != nil {
		approved = store.ApprovedToolCommands(s.toolRepoKey(r.Context(), svc))
	}
	rows := make([]reviewToolRow, 0, len(cmds))
	for _, tc := range cmds {
		resolved, rerr := template.ResolveCommand(tc.Command, nil, template.CmdCtx{
			Op: cs.Op, Source: cs.Source, Target: cs.Target,
			ConflictedFiles: files, Repo: top, ContextFile: "$GG_CONTEXT_FILE",
		})
		if rerr != nil {
			continue // a <user:...>-token row etc. is inert here, as it is for the review lane
		}
		rows = append(rows, reviewToolRow{Name: tc.Name, Command: resolved,
			Approved: approved[promptstate.CommandHash(tc.Command)]})
	}
	writeJSON(w, map[string]any{
		"op": cs.Op, "source": cs.Source, "target": cs.Target,
		"desc": cs.Describe(), "conflicted": len(files), "tools": rows,
	})
}

type conflictCompleteRequest struct {
	Tool    string `json:"tool"`
	Approve bool   `json:"approve"` // the user just approved this command
}

// handleConflictComplete begins a conflict-complete run and returns 202
// {op_id, tool}. An unapproved command is refused with 403 + needs_approval
// and the resolved text — the review lane's approval gate, copied verbatim.
func (s *Server) handleConflictComplete(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	var req conflictCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	st, err := svc.Status(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	cs := svc.Conflict(r.Context(), st)
	if cs.Op == "" {
		writeErr(w, http.StatusConflict, errors.New("nothing is paused"))
		return
	}
	cmds, err := s.conflictCompleteCommands(r.Context(), svc, cs.Op)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var tc config.ToolCommand
	found := false
	for _, c := range cmds {
		if c.Name == req.Tool {
			tc, found = c, true
			break
		}
	}
	if !found {
		names := make([]string, len(cmds))
		for i, c := range cmds {
			names[i] = c.Name
		}
		writeErr(w, http.StatusBadRequest, fmt.Errorf("no conflict-complete tool named %q (have %v)", req.Tool, names))
		return
	}
	top, err := svc.TopLevel(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	files := unmergedStatusPaths(st)
	resolved, err := template.ResolveCommand(tc.Command, nil, template.CmdCtx{
		Op: cs.Op, Source: cs.Source, Target: cs.Target,
		ConflictedFiles: files, Repo: top, ContextFile: "$GG_CONTEXT_FILE",
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	key, store := s.toolRepoKey(r.Context(), svc), s.promptStore()
	approved := store != nil && store.ApprovedToolCommands(key)[promptstate.CommandHash(tc.Command)]
	if !approved {
		if !req.Approve {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":          "this command has not been approved yet",
				"needs_approval": true,
				"tool":           tc.Name,
				"command":        resolved,
			})
			return
		}
		if store != nil {
			// Best-effort: failing to REMEMBER an approval must not block the
			// run the user just approved — it only costs another prompt.
			_ = store.ApproveToolCommand(key, promptstate.CommandHash(tc.Command))
		}
	}
	run, err := s.startRun("conflict_complete", func(ctx context.Context, svc *domain.Service, _ chan<- engine.Event, _ engine.Decider) (engine.Result, map[string]any, error) {
		res, rerr := svc.CompleteConflictReport(ctx, tc.Command, nil)
		if rerr != nil {
			return engine.Result{}, nil, rerr
		}
		return engine.Result{Summary: "conflict agent finished"},
			map[string]any{"report": res.Overview, "tool": tc.Name, "op": res.Op, "still_paused": res.StillPaused},
			nil
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"op_id": run.id, "tool": tc.Name})
}

// conflictCompleteCommands lists the structurally valid, web-visible
// conflict_complete commands applicable to pausedOp from the effective
// config — the filter that keeps a terminal-mode (TUI-only, no headless
// capture) or wrongly-tagged block out of the web tools listing.
func (s *Server) conflictCompleteCommands(ctx context.Context, svc *domain.Service, pausedOp string) ([]config.ToolCommand, error) {
	cfg, err := s.effectiveConfig(ctx, svc)
	if err != nil {
		return nil, err
	}
	var out []config.ToolCommand
	for _, tc := range cfg.Tools.Command {
		if tc.Category != string(exttool.CatConflictComplete) {
			continue
		}
		if tc.Mode != "capture" {
			continue // terminal mode has no browser tab to hand over
		}
		if !config.ToolVisibleIn(tc, "web") {
			continue
		}
		if tc.WhenOp != "" && tc.WhenOp != pausedOp {
			continue
		}
		if config.ValidateToolCommand(tc) != nil || template.ValidateCommandTokens(tc.Command, tc.PerFile) != nil {
			continue
		}
		out = append(out, tc)
	}
	return out, nil
}

// unmergedStatusPaths lists the conflicted paths from a status already read,
// in status order — the web-package sibling of domain's private
// unmergedPaths (internal/domain/complete_conflict.go), which the web
// package cannot reach directly.
func unmergedStatusPaths(st model.WorkingTreeStatus) []string {
	cs := st.Conflicts()
	out := make([]string, len(cs))
	for i, f := range cs {
		out[i] = f.Path
	}
	return out
}
