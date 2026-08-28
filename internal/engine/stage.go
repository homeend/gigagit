package engine

import (
	"context"
	"fmt"
	"strings"
)

// Stage stages (or, with Unstage, unstages) the given paths in the index.
// All stages everything including untracked files (git add -A) and is
// mutually exclusive with Paths and Unstage. It emits a single Progress;
// the default TreeWrite reservation applies (it writes .git/index).
//
// One fork: when git refuses explicit Paths because .gitignore excludes them,
// IgnoredPathsDecisionID offers "force-add" (retry with git add -f) or
// "abort". Every other outcome — including a decider that cannot answer —
// returns git's original refusal unchanged.
type Stage struct {
	Paths   []string
	All     bool
	Unstage bool
}

var _ Operation = Stage{}

func (op Stage) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.All {
		if op.Unstage {
			return Result{}, fmt.Errorf("stage: All cannot unstage")
		}
		if len(op.Paths) > 0 {
			return Result{}, fmt.Errorf("stage: All and explicit paths are mutually exclusive")
		}
		deps.emit(ctx, Progress{Step: "staged", Detail: "all changes"})
		if err := deps.Repo.StageAll(ctx); err != nil {
			return Result{}, fmt.Errorf("stage: %w", err)
		}
		return Result{Changed: true}.WithSummary("staged all changes"), nil
	}
	if len(op.Paths) == 0 {
		return Result{}, fmt.Errorf("stage: no paths")
	}
	var err error
	if op.Unstage {
		deps.emit(ctx, Progress{Step: "unstaged", Detail: strings.Join(op.Paths, " ")})
		err = deps.Repo.UnstagePaths(ctx, op.Paths)
	} else {
		deps.emit(ctx, Progress{Step: "staged", Detail: strings.Join(op.Paths, " ")})
		err = deps.Repo.StagePaths(ctx, op.Paths)
		if err != nil {
			err = op.offerForceAdd(ctx, deps, err)
		}
	}
	if err != nil {
		return Result{}, fmt.Errorf("stage: %w", err)
	}
	if op.Unstage {
		return Result{Changed: true}.WithSummary("unstaged %s", strings.Join(op.Paths, " ")), nil
	}
	return Result{Changed: true}.WithSummary("staged %s", strings.Join(op.Paths, " ")), nil
}

// IgnoredPathsDecisionID is the "git add refused: the paths are excluded by
// .gitignore" fork: options "force-add" (retry the same paths with git add -f)
// and "abort" (keep git's refusal; paths not covered by the ignore rules are
// already staged — git stages them before exiting 1).
const IgnoredPathsDecisionID = "stage.ignored"

// ignoredPathsMarker is the header git prints before listing the refused
// paths. It survives advice.addIgnoredFile=false (only the hint: lines are
// advice), so it is the stable detection anchor.
const ignoredPathsMarker = "paths are ignored by one of your .gitignore files"

// ignoredPathsFrom extracts the paths git listed after the refusal header
// ("The following paths are ignored by one of your .gitignore files:").
// Git names the deepest ignored ancestor (e.g. "docs/specs" for
// docs/specs/a.md), so the result labels the prompt but is never re-fed to
// git — the retry re-runs the caller's original paths. A nil return means
// err is not the ignored-paths refusal.
func ignoredPathsFrom(err error) []string {
	lines := strings.Split(err.Error(), "\n")
	var paths []string
	in := false
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		switch {
		case strings.Contains(ln, ignoredPathsMarker):
			in = true
		case !in || ln == "" || strings.HasPrefix(ln, "hint:"):
			if in {
				return paths
			}
		default:
			paths = append(paths, ln)
		}
	}
	return paths
}

// IgnoredPathsRefusal reports whether err is git's ignored-paths refusal
// from staging, and the paths git listed (possibly empty when the list
// cannot be parsed out). Exported for the TUI, whose synchronous stage path
// runs without a decider and re-raises the fork as a frontend modal.
func IgnoredPathsRefusal(err error) ([]string, bool) {
	if err == nil || !strings.Contains(err.Error(), ignoredPathsMarker) {
		return nil, false
	}
	return ignoredPathsFrom(err), true
}

// offerForceAdd reacts to a StagePaths refusal over gitignored paths: it
// forks IgnoredPathsDecisionID and on "force-add" retries all of op.Paths
// with git add -f (idempotent for the subset git already staged). Every
// other outcome — unrecognized error, "abort", or a decider that cannot
// answer (the web stage handler runs with a fail-loud decider) — returns
// addErr unchanged.
func (op Stage) offerForceAdd(ctx context.Context, deps OpDeps, addErr error) error {
	ignored, ok := IgnoredPathsRefusal(addErr)
	if !ok {
		return addErr
	}
	if len(ignored) == 0 {
		ignored = op.Paths // parse came up empty; label the prompt with the request
	}
	choice, derr := deps.decide(ctx, PromptReq(IgnoredPathsDecisionID,
		"%s is excluded by a .gitignore rule. Stage anyway?",
		[]string{"force-add", "abort"}, strings.Join(ignored, ", ")))
	if derr != nil || choice.Option != "force-add" {
		return addErr
	}
	return deps.Repo.StagePathsForce(ctx, op.Paths)
}
