package web

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/promptstate"
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

// reviewTarget resolves the wire's target description. "working" is the
// uncommitted diff; anything else means a branch, defaulting to HEAD.
//
// The branch name is guarded by isGitArgSafe only — no allowlist against the
// live branch list (the /api/versions precedent, not /api/compare's): an
// unknown name fails in ResolveCommit with git's own message rather than
// breaking something downstream, and BranchReviewTarget resolves BOTH
// endpoints to hex before they reach the tool's <range> token, so no ref name
// is ever spliced into a command.
func (s *Server) reviewTarget(ctx context.Context, svc *domain.Service, kind, branch, sha, preview string, shas []string) (domain.ReviewTarget, error) {
	if kind == "range" {
		return markedRangeReviewTarget(ctx, svc, shas)
	}
	if kind == "working" {
		return domain.WorkingReviewTarget(), nil
	}
	if kind == "preview" {
		return s.previewReviewTarget(ctx, svc, preview)
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
	return svc.CommitReviewTarget(ctx, sha)
}

// previewReviewTarget is the review of a SAVED merge preview or commit pair,
// named by its row ID. The ID is looked up in the two lists — never resolved
// as a label or an a...b literal (NoteScopeResolve would) — so no ref name
// from the wire reaches the target; the range is the scope's hex pair
// (ScopeReviewTarget). A preview that is not previewable (merged, a side
// missing, no base) is refused like the CLI refuses it.
func (s *Server) previewReviewTarget(ctx context.Context, svc *domain.Service, id string) (domain.ReviewTarget, error) {
	if id == "" {
		return domain.ReviewTarget{}, errors.New("no preview named")
	}
	ps, err := svc.PreviewList(ctx)
	if err != nil {
		return domain.ReviewTarget{}, err
	}
	for _, p := range ps {
		if p.ID != id {
			continue
		}
		set, err := svc.PreviewNotes(ctx, p.Source, p.Target)
		if err != nil {
			return domain.ReviewTarget{}, err
		}
		if !set.OK() {
			return domain.ReviewTarget{}, fmt.Errorf("preview %s is not previewable (merged, a side missing, or no common base)", p.Label)
		}
		return domain.ScopeReviewTarget(set), nil
	}
	pairs, err := svc.PairList(ctx)
	if err != nil {
		return domain.ReviewTarget{}, err
	}
	for _, p := range pairs {
		if p.ID != id {
			continue
		}
		set, err := svc.PairNotes(ctx, p.A, p.B)
		if err != nil {
			return domain.ReviewTarget{}, err
		}
		if !set.OK() {
			return domain.ReviewTarget{}, fmt.Errorf("pair %s: a commit is missing", p.Label)
		}
		return domain.ScopeReviewTarget(set), nil
	}
	return domain.ReviewTarget{}, fmt.Errorf("unknown preview %q", id)
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
// other, and the hook a worktree create runs (postCreateHook).
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

func reviewMetaText(meta []notebatch.MetaKV) string {
	parts := make([]string, len(meta))
	for i, kv := range meta {
		parts[i] = kv.Key + ": " + kv.Value
	}
	return strings.Join(parts, ", ")
}

// splitShas reads the tools query's comma-separated marked commits.
func splitShas(q string) []string {
	if q == "" {
		return nil
	}
	return strings.Split(q, ",")
}

// markedRangeReviewTarget is the TUI's markedRangeReviewRow: a review of
// EXACTLY what Compare selection shows for the marks — older..newer for two,
// oldest^..newest for three or more (the oldest's parent resolved here; a root
// is refused). The marks arrive newest first (feed order) and hex only, so the
// range carries no ref name; the label "<old7>..<new7> — <newest subject>" is
// built from the commits themselves.
func markedRangeReviewTarget(ctx context.Context, svc *domain.Service, shas []string) (domain.ReviewTarget, error) {
	marks, err := markedShas(shas)
	if err != nil {
		return domain.ReviewTarget{}, err
	}
	if len(marks) < 2 {
		return domain.ReviewTarget{}, errors.New("mark at least 2 commits to review a range")
	}
	newest, oldest := marks[0], marks[len(marks)-1]
	base := oldest
	if len(marks) >= 3 {
		parent, ok, perr := svc.ResolveRev(ctx, oldest+"^")
		if perr != nil {
			return domain.ReviewTarget{}, perr
		}
		if !ok {
			return domain.ReviewTarget{}, errors.New("can't review a range from the root commit")
		}
		base = parent
	}
	rng := base + ".." + newest
	label := oldest[:7] + ".." + newest[:7]
	if msg, merr := svc.CommitMessage(ctx, newest); merr == nil {
		if subj := strings.TrimSpace(strings.SplitN(msg, "\n", 2)[0]); subj != "" {
			label += " — " + subj
		}
	}
	return domain.ReviewTarget{Kind: domain.ReviewRange, Range: rng, Label: label, Diff: model.DiffSpec{Rev: rng}}, nil
}
