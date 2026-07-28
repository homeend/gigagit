package engine

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/homeend/gigagit/internal/rebaseplan"
)

// Reword changes a single commit's message. Rewording the tip with a clean
// tree is a cheap `git commit --amend`; a mid-branch commit (or a dirty HEAD)
// replays its branch onto the commit's parent with a one-entry reword plan,
// reusing InteractiveRebase's stash-wrapped internals. The root commit of a
// multi-commit repo is refused (it needs `rebase -i --root`, unsupported here).
type Reword struct {
	Commit string // commit to reword (any rev)
	NewMsg string // the new full message
	GGBin  string // path to the gg binary (rebase sequence editor); needed only for the rebase path
}

var _ Operation = Reword{}

func (op Reword) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Commit == "" || op.NewMsg == "" {
		return Result{}, fmt.Errorf("reword: Commit and NewMsg are required")
	}
	target, err := deps.Repo.RevParse(ctx, op.Commit)
	if err != nil {
		return Result{}, fmt.Errorf("reword: %w", err)
	}
	head, err := deps.Repo.RevParse(ctx, "HEAD")
	if err != nil {
		return Result{}, err
	}
	parent, parentErr := deps.Repo.RevParse(ctx, target+"^") // err ⇒ root commit

	// Cheap path: rewording the tip with a clean tree (or a single-commit repo
	// whose root IS HEAD) is a message-only amend — no rebase, no stash.
	if target == head {
		dirty, derr := deps.Repo.IsDirty(ctx)
		if derr != nil {
			return Result{}, derr
		}
		if !dirty || parentErr != nil {
			deps.emit(ctx, Progress{Step: "rewording", Detail: shortSHA(target)})
			if err := deps.Repo.Commit(ctx, op.NewMsg, false, true); err != nil {
				return Result{}, fmt.Errorf("reword (amend): %w", err)
			}
			res := Result{Changed: true}.WithSummary("reworded %s", shortSHA(target))
			deps.emit(ctx, Done{Result: res})
			return res, nil
		}
	}

	if parentErr != nil {
		return Result{}, fmt.Errorf("reword: cannot reword the root commit (needs rebase -i --root, unsupported)")
	}

	cur, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	if cur == "" {
		return Result{}, fmt.Errorf("reword: detached HEAD — check out a branch first")
	}
	if op.GGBin == "" {
		return Result{}, fmt.Errorf("reword: GGBin is required for a non-HEAD reword")
	}
	hasMerges, err := deps.Repo.HasMergeCommits(ctx, "", parent, cur)
	if err != nil {
		return Result{}, err
	}
	if hasMerges {
		return Result{}, fmt.Errorf("reword: range %s..%s contains merge commits (not supported)", shortSHA(parent), cur)
	}

	commits, err := deps.Repo.LogRangeMessages(ctx, parent, cur)
	if err != nil {
		return Result{}, err
	}
	entries := make([]rebaseplan.Entry, 0, len(commits))
	found := false
	for _, c := range commits { // oldest-first, exactly git todo order
		e := rebaseplan.Entry{Sha: c.Hash, Action: rebaseplan.Pick, Orig: c.Message}
		if c.Hash == target {
			e.Action = rebaseplan.Reword
			e.NewMsg = op.NewMsg
			found = true
		}
		entries = append(entries, e)
	}
	if !found {
		// target isn't an ancestor of cur — without this guard we'd replay cur
		// onto an unrelated base and corrupt the branch.
		return Result{}, fmt.Errorf("reword: commit %s is not on the current branch %s", shortSHA(target), cur)
	}
	plan := rebaseplan.Plan{Entries: entries}

	planPath, err := writePlanFile(plan)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(planPath) // a pure reword cannot conflict, so it never pauses
	env := []string{"GIT_SEQUENCE_EDITOR=" + rebaseplan.SequenceEditor(op.GGBin, planPath, runtime.GOOS)}

	deps.emit(ctx, Progress{Step: "rewording", Detail: shortSHA(target)})
	// wrapped reads only Branch/Onto (it never touches Plan — Run writes the file).
	ir := InteractiveRebase{Branch: cur, Onto: parent, GGBin: op.GGBin}
	res, _, rerr := ir.wrapped(ctx, deps, "", env)
	if rerr != nil {
		return res, rerr
	}
	res = Result{Changed: true}.WithSummary("reworded %s", shortSHA(target))
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// shortSHA abbreviates a full object id for human-facing summaries.
func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
