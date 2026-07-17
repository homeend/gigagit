package engine

import (
	"context"
	"fmt"
	"os"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/rebaseplan"
)

// InteractiveRebase drives `git rebase -i Onto` for Branch through the
// gg-as-editor protocol: the plan is written to a temp file and
// GIT_SEQUENCE_EDITOR points at `GGBin __rebase-seq <plan>`. It mirrors
// SmartRebase's rungs (here / other worktree / stash+switch), wraps the rebase
// in a stash so the working tree + index split survive (message-only intent),
// and forks on conflict via "rebase-conflict" (keep-conflicts / abort).
type InteractiveRebase struct {
	Branch string
	Onto   string
	Plan   rebaseplan.Plan
	GGBin  string // path to the gg binary (the sequence editor)
}

var _ Operation = InteractiveRebase{}

func (op InteractiveRebase) Run(ctx context.Context, deps OpDeps) (Result, error) {
	switch {
	case op.Branch == "":
		return Result{}, fmt.Errorf("interactive rebase: Branch is required")
	case op.Onto == "":
		return Result{}, fmt.Errorf("interactive rebase: Onto is required")
	case op.Branch == op.Onto:
		return Result{}, fmt.Errorf("interactive rebase: branch and base are both %s", op.Branch)
	case op.GGBin == "":
		return Result{}, fmt.Errorf("interactive rebase: GGBin is required")
	case len(op.Plan.Entries) == 0:
		return Result{}, fmt.Errorf("interactive rebase: empty plan")
	}

	branches, err := deps.Repo.Branches(ctx)
	if err != nil {
		return Result{}, err
	}
	have := make(map[string]bool, len(branches))
	for _, b := range branches {
		have[b.Name] = true
	}
	if !have[op.Branch] {
		return Result{}, fmt.Errorf("interactive rebase: no such branch: %s", op.Branch)
	}
	// Onto may be a branch OR any resolvable commit-ish: the single-commit
	// move/drop callers base onto a parent revspec like "<sha>~1".
	if !have[op.Onto] {
		ok, err := deps.Repo.CommitExists(ctx, op.Onto)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			return Result{}, fmt.Errorf("interactive rebase: no such commit: %s", op.Onto)
		}
	}

	hasMerges, err := deps.Repo.HasMergeCommits(ctx, "", op.Onto, op.Branch)
	if err != nil {
		return Result{}, err
	}
	if hasMerges {
		return Result{}, fmt.Errorf("interactive rebase: %s..%s contains merge commits (not supported yet)", op.Onto, op.Branch)
	}

	planPath, err := writePlanFile(op.Plan)
	if err != nil {
		return Result{}, err
	}
	env := []string{"GIT_SEQUENCE_EDITOR=" + op.GGBin + " __rebase-seq " + planPath}

	cur, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		os.Remove(planPath)
		return Result{}, err
	}

	var (
		res    Result
		paused bool
		runErr error
	)
	switch {
	case op.Branch == cur:
		// Rung 1: checked out here.
		res, paused, runErr = op.wrapped(ctx, deps, "", env)
	default:
		wt, werr := deps.Repo.WorktreeForBranch(ctx, op.Branch)
		if werr != nil {
			os.Remove(planPath)
			return Result{}, werr
		}
		if wt != nil {
			// Rung 2: branch lives in another worktree — rebase there, stay put.
			res, paused, runErr = op.irebaseAt(ctx, deps, wt.Path, env)
		} else {
			// Rung 3: stash + switch + rebase, stay on branch.
			res, paused, runErr = op.wrapped(ctx, deps, op.Branch, env)
		}
	}

	// The plan's exec steps are still needed by `git rebase --continue` when
	// paused; only remove it once the rebase is fully done.
	if !paused {
		os.Remove(planPath)
	}
	return res, runErr
}

// wrapped runs the rebase against the current worktree (dir ""), stashing the
// working tree first when dirty (and switching to switchTo first when non-empty),
// then restoring the stash + the staged/unstaged split on success.
func (op InteractiveRebase) wrapped(ctx context.Context, deps OpDeps, switchTo string, env []string) (Result, bool, error) {
	stashed, staged, err := op.stashBegin(ctx, deps)
	if err != nil {
		return Result{}, false, err
	}
	if switchTo != "" {
		deps.emit(ctx, Progress{Step: "switching", Detail: switchTo})
		if err := deps.Repo.Switch(ctx, switchTo); err != nil {
			if stashed {
				_ = deps.Repo.StashPop(ctx, "")
			}
			return Result{}, false, err
		}
	}
	res, paused, rebaseErr := op.irebaseAt(ctx, deps, "", env)
	if rebaseErr != nil {
		if stashed && res.Summary != "" {
			res = res.AppendSummary(" (your changes remain stashed)")
		}
		return res, paused, rebaseErr
	}
	if stashed {
		deps.emit(ctx, Progress{Step: "restoring changes"})
		if err := deps.Repo.StashPop(ctx, ""); err != nil {
			deps.emit(ctx, DecisionNeeded{Request: PromptReq("stash-pop-conflict", "Restoring your changes conflicted", []string{"keep", "abort"})})
			return res.AppendSummary("; restore conflicted (changes preserved in stash)"),
				false, fmt.Errorf("stash pop conflict after rebasing %s: %w", op.Branch, err)
		}
		if len(staged) > 0 {
			if err := deps.Repo.StagePaths(ctx, staged); err != nil {
				return res.AppendSummary("; could not restore the staged index"), false, err
			}
		}
	}
	return res, false, nil
}

// stashBegin stashes the current worktree (tracked + untracked) when dirty,
// recording which tracked paths were staged so the index split can be restored.
func (op InteractiveRebase) stashBegin(ctx context.Context, deps OpDeps) (stashed bool, staged []string, err error) {
	dirty, err := deps.Repo.IsDirty(ctx)
	if err != nil || !dirty {
		return false, nil, err
	}
	st, err := deps.Repo.Status(ctx)
	if err != nil {
		return false, nil, err
	}
	for _, f := range st.Files {
		if f.Kind == model.KindUntracked || f.Kind == model.KindUnmerged {
			continue
		}
		if f.Staged != '.' && f.Staged != 0 {
			staged = append(staged, f.Path)
		}
	}
	deps.emit(ctx, Progress{Step: "stashing"})
	if err := deps.Repo.StashPush(ctx, "gg-irebase:"+op.Branch, nil, true); err != nil {
		return false, nil, err
	}
	return true, staged, nil
}

// irebaseAt drives the interactive rebase in dir ("" = current worktree),
// returning paused=true when a conflict left it for `git rebase --continue`.
func (op InteractiveRebase) irebaseAt(ctx context.Context, deps OpDeps, dir string, env []string) (Result, bool, error) {
	if dir == "" {
		deps.emit(ctx, Progressf("rebasing", "%s onto %s", op.Branch, op.Onto))
	} else {
		deps.emit(ctx, Progressf("rebasing", "%s onto %s in worktree %s", op.Branch, op.Onto, dir))
	}
	rebaseErr := deps.Repo.RebaseInteractive(ctx, dir, op.Onto, env)
	if rebaseErr == nil {
		res := Result{Changed: true}.WithSummary("rebased %s onto %s", op.Branch, op.Onto)
		if dir != "" {
			res = res.AppendSummary(" in worktree %s", dir)
		}
		return res, false, nil
	}
	inRebase, stateErr := deps.Repo.RebaseInProgress(ctx, dir)
	if stateErr != nil {
		return Result{}, false, fmt.Errorf("interactive rebase: %s onto %s: %v (state check: %w)", op.Branch, op.Onto, rebaseErr, stateErr)
	}
	if !inRebase {
		return Result{}, false, fmt.Errorf("interactive rebase: %s onto %s: %w", op.Branch, op.Onto, rebaseErr)
	}
	choice, derr := deps.decide(ctx, PromptReq("rebase-conflict", "Rebasing %s onto %s hit conflicts", []string{"keep-conflicts", "abort"}, op.Branch, op.Onto))
	if derr != nil {
		return Result{}, false, derr
	}
	if choice.Option == "keep-conflicts" {
		res := Result{Changed: true}.WithSummary("rebase of %s onto %s", op.Branch, op.Onto)
		if dir != "" {
			res = res.AppendSummary(" in worktree %s", dir)
		}
		res = res.AppendSummary(" paused on a conflict (resolve, then `git rebase --continue`)")
		return res, true, fmt.Errorf("rebase conflict: %s onto %s", op.Branch, op.Onto)
	}
	if err := deps.Repo.RebaseAbort(ctx, dir); err != nil {
		return Result{}, false, fmt.Errorf("interactive rebase: abort failed: %w", err)
	}
	return Result{Changed: false}.WithSummary("aborted: interactive rebase of %s onto %s", op.Branch, op.Onto), false, nil
}

// writePlanFile serializes the plan to a temp JSON file and returns its path.
// The caller removes it (except when the rebase pauses — the exec steps still
// need it).
func writePlanFile(p rebaseplan.Plan) (string, error) {
	b, err := rebaseplan.Marshal(p)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "gg-rebase-plan-*.json")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
