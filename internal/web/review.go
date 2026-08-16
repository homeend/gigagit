package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/promptstate"
	"github.com/homeend/gigagit/internal/template"
)

// AI review: run a configured `review`-category external tool over a branch's
// work or the uncommitted changes, and show the report it produces.
//
// This is the first web surface that executes something other than git, so
// two properties are load-bearing:
//
//   - The command text NEVER comes off the wire. The client sends a tool NAME
//     and a target; the server looks the command up in the effective config
//     and resolves it itself. A client that could post a command string would
//     be a remote shell behind a loopback port.
//   - A command runs only after an explicit approval, shown in full and
//     remembered per repo against a hash of the config TEMPLATE text — the
//     same store and the same key the TUI's lanes use, so approving in either
//     frontend covers the other and an edited command re-prompts in both.

// reviewToolRow is one configured review tool, with the command exactly as it
// would run (resolved for this target) so the approval box shows the truth
// rather than a template.
type reviewToolRow struct {
	Name     string `json:"name"`
	Command  string `json:"command"`
	Approved bool   `json:"approved"`
}

// handleReviewTools answers GET /api/review/tools?target=&branch= with the
// resolved target plus every usable review tool. The client uses it for the
// chooser, the approval box, and the "nothing configured" message — one round
// trip before anything runs.
func (s *Server) handleReviewTools(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	target, err := s.reviewTarget(r.Context(), svc, r.URL.Query().Get("target"), r.URL.Query().Get("branch"), r.URL.Query().Get("sha"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rows, err := s.reviewToolRows(r.Context(), svc, target)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"label": target.DisplayLabel(), "range": target.Range, "tools": rows})
}

type reviewStartRequest struct {
	Target  string `json:"target"` // "branch" (default) | "working" | "commit"
	Branch  string `json:"branch"`
	Sha     string `json:"sha"` // target "commit": the commit to review, hex only
	Tool    string `json:"tool"`
	Approve bool   `json:"approve"` // the user just approved this command
}

// handleReviewStart begins a review run and returns 202 {op_id}. An
// unapproved command is refused with 403 + needs_approval and the resolved
// text, which is what the client shows before asking again with approve=true.
func (s *Server) handleReviewStart(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	var req reviewStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	target, err := s.reviewTarget(r.Context(), svc, req.Target, req.Branch, req.Sha)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	cmd, err := s.pickReviewCommand(r.Context(), svc, req.Tool)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	top, err := svc.TopLevel(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	resolved, err := template.ResolveCommand(cmd.Command, nil, template.CmdCtx{Range: target.Range, Repo: top})
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
			// Best-effort: failing to REMEMBER an approval must not block the
			// run the user just approved — it only costs another prompt.
			_ = store.ApproveToolCommand(key, promptstate.CommandHash(cmd.Command))
		}
	}
	label := target.DisplayLabel()
	run, err := s.startRun("review", func(ctx context.Context, svc *domain.Service, _ chan<- engine.Event, _ engine.Decider) (engine.Result, map[string]any, error) {
		// ReviewReport runs the tool and persists the report; it takes no
		// events or decider (the TUI shows a bare spinner for the same
		// reason), so this lane's only wire traffic is the terminal done.
		res, rerr := svc.ReviewReport(ctx, target, resolved, []string{"GG_TASK=review"}, time.Now())
		if rerr != nil {
			return engine.Result{}, nil, rerr
		}
		return engine.Result{Summary: "review written to " + res.Path},
			map[string]any{"report": res.Content, "path": res.Path, "label": res.Label},
			nil
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"op_id": run.id, "tool": cmd.Name, "label": label})
}

// handleOpCancel cancels a live agent run (review or conflict_complete).
// Restricted to those lanes on purpose: an agent can hang for minutes holding
// the single lane, whereas interrupting a git operation half-way is a
// separate design question.
func (s *Server) handleOpCancel(w http.ResponseWriter, r *http.Request) {
	run := s.opByID(r.PathValue("id"))
	if run == nil {
		writeErr(w, http.StatusNotFound, errors.New("unknown operation"))
		return
	}
	// commit_message joins the two agent lanes here (op_ai.go): it is the same
	// kind of run — an external tool that can hang for minutes holding the one
	// lane — and its cancel is the browser's equivalent of the TUI's esc
	// during a generate.
	if run.kind != "review" && run.kind != "conflict_complete" && run.kind != "commit_message" {
		writeErr(w, http.StatusConflict, errors.New("this operation cannot be cancelled"))
		return
	}
	if err := run.requestCancel(); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, map[string]any{})
}

// reviewTarget resolves the wire's target description. "working" is the
// uncommitted diff; anything else means a branch, defaulting to HEAD.
//
// The branch name is guarded by isGitArgSafe only — no allowlist against the
// live branch list (the /api/versions precedent, not /api/compare's): an
// unknown name fails in ResolveCommit with git's own message rather than
// breaking something downstream, and BranchReviewTarget resolves BOTH
// endpoints to hex before they reach the tool's <range> token, so no ref name
// is ever spliced into a command.
func (s *Server) reviewTarget(ctx context.Context, svc *domain.Service, kind, branch, sha string) (domain.ReviewTarget, error) {
	if kind == "working" {
		return domain.WorkingReviewTarget(), nil
	}
	if kind == "commit" {
		return s.commitReviewTarget(ctx, svc, sha)
	}
	if branch == "" {
		branch = "HEAD"
	}
	if !isGitArgSafe(branch) {
		return domain.ReviewTarget{}, errors.New("invalid branch")
	}
	return svc.BranchReviewTarget(ctx, branch)
}

// commitReviewTarget scopes a review to ONE commit's own change, sha^..sha —
// the TUI's reviewTargetForCommit. A root commit has no parent, so ^.. would
// fail and the commit is reviewed alone. Both shapes are built here from a hex
// id, so no ref name ever reaches the tool's <range> token; the label (the
// report's title and filename) is read server-side rather than taken from the
// wire for the same reason.
func (s *Server) commitReviewTarget(ctx context.Context, svc *domain.Service, sha string) (domain.ReviewTarget, error) {
	if !isHexSha(sha) {
		return domain.ReviewTarget{}, errors.New("invalid commit")
	}
	rng := sha + "^.." + sha
	if _, ok, err := svc.ResolveRev(ctx, sha+"^"); err == nil && !ok {
		rng = sha // root commit
	}
	label := sha
	if len(label) > 8 {
		label = label[:8]
	}
	if msg, err := svc.CommitMessage(ctx, sha); err == nil {
		if subj := strings.TrimSpace(strings.SplitN(msg, "\n", 2)[0]); subj != "" {
			label += " " + subj
		}
	}
	return domain.ReviewTarget{Kind: domain.ReviewRange, Range: rng, Label: label, Diff: model.DiffSpec{Rev: rng}}, nil
}

// reviewToolRows resolves every usable review command for target.
func (s *Server) reviewToolRows(ctx context.Context, svc *domain.Service, target domain.ReviewTarget) ([]reviewToolRow, error) {
	cmds, err := s.reviewCommands(ctx, svc)
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
	rows := make([]reviewToolRow, 0, len(cmds))
	for _, tc := range cmds {
		resolved, rerr := template.ResolveCommand(tc.Command, nil, template.CmdCtx{Range: target.Range, Repo: top})
		if rerr != nil {
			continue // an unresolvable template is inert, as it is for the CLI
		}
		rows = append(rows, reviewToolRow{
			Name:     tc.Name,
			Command:  resolved,
			Approved: approved[promptstate.CommandHash(tc.Command)],
		})
	}
	return rows, nil
}

// pickReviewCommand chooses the command to run: by name, or the sole
// candidate when the client sent none (mirrors `gg review --tool`).
func (s *Server) pickReviewCommand(ctx context.Context, svc *domain.Service, name string) (config.ToolCommand, error) {
	cmds, err := s.reviewCommands(ctx, svc)
	if err != nil {
		return config.ToolCommand{}, err
	}
	if len(cmds) == 0 {
		return config.ToolCommand{}, errors.New(`no review tool configured (see [[tools.command]] category="review")`)
	}
	if name == "" {
		if len(cmds) > 1 {
			return config.ToolCommand{}, errors.New("more than one review tool configured; name one")
		}
		return cmds[0], nil
	}
	for _, tc := range cmds {
		if tc.Name == name {
			return tc, nil
		}
	}
	return config.ToolCommand{}, fmt.Errorf("no review tool named %q", name)
}

// reviewCommands lists the structurally valid review-category commands from
// the effective config — the same filter `gg review` applies, so the two
// frontends offer the same set.
func (s *Server) reviewCommands(ctx context.Context, svc *domain.Service) ([]config.ToolCommand, error) {
	cfg, err := s.effectiveConfig(ctx, svc)
	if err != nil {
		return nil, err
	}
	var out []config.ToolCommand
	for _, tc := range cfg.Tools.Command {
		if tc.Category != string(exttool.CatReview) {
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

// activeRepoConfigPath resolves the per-repo config file gg actually reads
// and writes — the machine-local private file when one exists, else the
// committed .gg.toml (the TUI Settings writers' target).
func (s *Server) activeRepoConfigPath(ctx context.Context, svc *domain.Service) (string, error) {
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return "", err
	}
	privatePath := ""
	if wts, werr := svc.Worktrees(ctx); werr == nil && len(wts) > 0 && wts[0].Path != "" {
		privatePath = config.PrivateRepoPath(wts[0].Path)
	}
	return config.ActiveRepoConfigPath(filepath.Join(top, ".gg.toml"), privatePath), nil
}

// effectiveConfig loads global + the ACTIVE per-repo config (the machine-local
// private file when one exists, else the committed .gg.toml) — the resolution
// `gg review` uses, so a tool configured for one frontend is visible to the
// other. Distinct from postCreateHook's narrower committed-only probe.
func (s *Server) effectiveConfig(ctx context.Context, svc *domain.Service) (config.Config, error) {
	active, err := s.activeRepoConfigPath(ctx, svc)
	if err != nil {
		return config.Config{}, err
	}
	return config.Load(config.DefaultGlobalPath(), active)
}

// promptStore opens the machine-global UX-memory store that holds tool
// approvals, beside the MRU registry in the same state dir (which is also
// what makes the test seam a single knob). nil when there is no state dir:
// approvals then can't persist, so every run asks — fail-closed.
func (s *Server) promptStore() promptstate.Store {
	path := s.reposStatePath()
	if path == "" {
		return nil
	}
	return promptstate.NewFileStore(filepath.Join(filepath.Dir(path), "prompts.toml"))
}

// toolRepoKey scopes approvals per repo by git common dir — the TUI's key, so
// the two frontends read each other's approvals. Falls back to the worktree
// path, as the TUI does before its health probe has resolved.
func (s *Server) toolRepoKey(ctx context.Context, svc *domain.Service) string {
	if common, err := svc.GitCommonDir(ctx); err == nil && common != "" {
		return common
	}
	top, _ := svc.TopLevel(ctx)
	return top
}
