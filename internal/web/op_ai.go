package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/promptstate"
	"github.com/homeend/gigagit/internal/template"
)

// The commit box's two missing lanes: have an agent write the message, and
// amend the last commit.
//
// The generate lane is review.go's third sibling (review, conflict-complete,
// and now commit_message) and keeps both of that file's load-bearing
// properties: the command text NEVER comes off the wire — the client names a
// TOOL and the server resolves the command from the effective config — and an
// unapproved command is refused until the user has seen it in full and said
// yes, remembered per repo against a hash of the config TEMPLATE text in the
// same store the TUI's lanes use.
//
// Nothing here commits. A generated message lands in the message box for the
// human to read, edit, and send — the TUI's ctrl+g does exactly that, and an
// agent that could commit on its own would be a different (and much larger)
// promise than "help me write this message".
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/commit-message/tools", s.handleCommitMessageTools)
		mux.HandleFunc("GET /api/commit-message/head", s.handleHeadCommitMessage)
		mux.HandleFunc("POST /api/commit-message/generate", writeGuard(s.handleGenerateMessage))
	})
	RegisterOp("commit-amend", buildCommitAmend)
}

// genToolRow is one configured commit_message tool with the command exactly as
// it would run, so the approval box shows the truth rather than a template.
type genToolRow struct {
	Name     string `json:"name"`
	Command  string `json:"command"`
	Approved bool   `json:"approved"`
}

// handleCommitMessageTools answers the staged count plus every usable
// commit_message tool — one round trip before anything runs, which is what
// lets the client show the TUI's two refusals ("nothing staged to describe",
// "no commit-message tool configured") without starting a lane.
func (s *Server) handleCommitMessageTools(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	staged := 0
	if st, err := svc.Status(readCtx(r)); err == nil {
		staged = st.Counts().Staged
	}
	rows, err := s.genToolRows(readCtx(r), svc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"staged": staged, "tools": rows})
}

// handleHeadCommitMessage answers HEAD's full message, for the amend prefill.
// /api/commit-message takes a hex sha and the browser has no honest way to
// name HEAD as one before the feed has loaded — and asking the server for "the
// last commit" is the question the TUI's amend asks too (LastCommitMessage).
func (s *Server) handleHeadCommitMessage(w http.ResponseWriter, r *http.Request) {
	msg, err := s.service().LastCommitMessage(readCtx(r))
	if err != nil {
		// No commit yet: there is nothing to amend, and that is the honest
		// answer rather than a 500.
		writeErr(w, http.StatusConflict, errors.New("no commit to amend"))
		return
	}
	writeJSON(w, map[string]any{"message": msg})
}

type generateRequest struct {
	Tool    string `json:"tool"`
	Approve bool   `json:"approve"` // the user just approved this command
}

// handleGenerateMessage runs a commit_message agent headlessly over the staged
// diff and returns 202 {op_id}; the captured subject/body ride the terminal
// done event. An unapproved command is refused with 403 + needs_approval and
// the resolved text — the client shows that before asking again with approve.
func (s *Server) handleGenerateMessage(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	var req generateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	// The TUI's first gate: GenerateMessage describes the STAGED diff, so an
	// empty index has nothing to describe and the run would waste an agent.
	st, err := svc.Status(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if st.Counts().Staged == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("nothing staged to describe"))
		return
	}
	cmd, err := s.pickGenCommand(r.Context(), svc, req.Tool)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	top, err := svc.TopLevel(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	resolved, err := template.ResolveCommand(cmd.Command, nil, template.CmdCtx{Repo: top})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	key, store := s.toolRepoKey(r.Context(), svc), s.promptStore()
	approved := store != nil && store.ApprovedToolCommands(key)[promptstate.CommandHash(cmd.Command)]
	if !approved {
		if !req.Approve {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":          "this command has not been approved yet",
				"needs_approval": true,
				"tool":           cmd.Name,
				"command":        resolved,
			})
			return
		}
		if store != nil {
			// Best-effort: failing to REMEMBER an approval must not block the run
			// the user just approved — it only costs another prompt next time.
			_ = store.ApproveToolCommand(key, promptstate.CommandHash(cmd.Command))
		}
	}
	run, err := s.startRun("commit_message", func(ctx context.Context, svc *domain.Service, _ chan<- engine.Event, _ engine.Decider) (engine.Result, map[string]any, error) {
		res, rerr := svc.Execute(ctx, engine.GenerateMessage{
			Command: resolved,
			Dir:     top,
			Env:     []string{"GG_TASK=commit_message"},
		}, nil, nil)
		if rerr != nil {
			return engine.Result{}, nil, rerr
		}
		// The capture contract ($GG_MESSAGE_FILE wins over stdout) belongs to
		// the op; splitting subject from body belongs to exttool, and both
		// frontends use the same split so a tool behaves identically in each.
		subject, body, perr := exttool.ParseCaptureMessage([]byte(res.Captured))
		if perr != nil {
			return engine.Result{}, nil, perr
		}
		if strings.TrimSpace(subject) == "" {
			return engine.Result{}, nil, errors.New("the tool returned no message")
		}
		return engine.Result{Summary: "message generated"},
			map[string]any{"subject": subject, "body": body},
			nil
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"op_id": run.id, "tool": cmd.Name})
}

// pickGenCommand chooses the command to run: by name, or the sole candidate
// when the client sent none (pickReviewCommand's rule, and `gg review --tool`'s).
func (s *Server) pickGenCommand(ctx context.Context, svc *domain.Service, name string) (config.ToolCommand, error) {
	cmds, err := s.genCommands(ctx, svc)
	if err != nil {
		return config.ToolCommand{}, err
	}
	if len(cmds) == 0 {
		return config.ToolCommand{}, errors.New(`no commit-message tool configured (see [[tools.command]] category="commit_message")`)
	}
	if name == "" {
		if len(cmds) > 1 {
			return config.ToolCommand{}, errors.New("more than one commit-message tool configured; name one")
		}
		return cmds[0], nil
	}
	for _, tc := range cmds {
		if tc.Name == name {
			return tc, nil
		}
	}
	return config.ToolCommand{}, fmt.Errorf("no commit-message tool named %q", name)
}

// genCommands lists the structurally valid commit_message commands the web may
// run. Headless only: a terminal-mode command expects a terminal to hand over
// and there is no browser tab to give it (the conflict lane's rule).
func (s *Server) genCommands(ctx context.Context, svc *domain.Service) ([]config.ToolCommand, error) {
	cfg, err := s.effectiveConfig(ctx, svc)
	if err != nil {
		return nil, err
	}
	var out []config.ToolCommand
	for _, tc := range cfg.Tools.Command {
		if tc.Category != string(exttool.CatCommitMessage) {
			continue
		}
		if tc.Mode != "capture" {
			continue
		}
		if !config.ToolVisibleIn(tc, "web") {
			continue
		}
		if config.ValidateToolCommand(tc) != nil || template.ValidateCommandTokens(tc.Command, tc.PerFile) != nil {
			continue
		}
		out = append(out, tc)
	}
	return out, nil
}

// genToolRows resolves every usable commit-message command for display.
func (s *Server) genToolRows(ctx context.Context, svc *domain.Service) ([]genToolRow, error) {
	cmds, err := s.genCommands(ctx, svc)
	if err != nil {
		return nil, err
	}
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return nil, err
	}
	var approved map[string]bool
	if store := s.promptStore(); store != nil {
		approved = store.ApprovedToolCommands(s.toolRepoKey(ctx, svc))
	}
	rows := make([]genToolRow, 0, len(cmds))
	for _, tc := range cmds {
		resolved, rerr := template.ResolveCommand(tc.Command, nil, template.CmdCtx{Repo: top})
		if rerr != nil {
			continue // an unresolvable template is inert, as it is for the CLI
		}
		rows = append(rows, genToolRow{
			Name:     tc.Name,
			Command:  resolved,
			Approved: approved[promptstate.CommandHash(tc.Command)],
		})
	}
	return rows, nil
}

// buildCommitAmend rewrites the last commit with a new message — the TUI's C.
//
// Nothing here checks that there IS a commit to amend: git says
// "You have nothing to amend" better than a re-implementation of the rule
// would, and the op surfaces git's own words. The browser gates the button on
// a non-empty feed (the TUI's canAmend) so the refusal is rare rather than
// routine.
func buildCommitAmend(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return nil, nil, http.StatusBadRequest, errors.New("an amended commit still needs a message")
	}
	return engine.Commit{Message: req.Message, Amend: true}, nil, 0, nil
}
