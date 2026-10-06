package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
)

// PullDirtyDecisionID is raised when git refuses a pull because uncommitted
// work in the pulled worktree is in the way (files the pull would overwrite,
// or a rebase pull refusing a dirty tree). Options: shelve, discard, abort —
// the recycle.dirty answers minus commit; the prompt carries the same file
// overview.
const PullDirtyDecisionID = "pull.dirty"

// pullDirt records how a pull.dirty answer cleared the way.
type pullDirt struct {
	asked     bool   // the question was raised
	shelved   string // the shelf set's label
	discarded bool
	cancelled bool
}

// settled reports that the pull's failure is final: the question was asked
// but the way was not cleared (no answer, or the shelve/discard failed), or
// git still refuses over local changes. Only an unsettled failure is the
// pull's own (a diverged branch) and may go on to the next question.
func (d pullDirt) settled(err error) bool {
	return (d.asked && d.shelved == "" && !d.discarded) || git.IsLocalChangesRefusal(err)
}

// annotate appends what the answer did to the pull's summary.
func (d pullDirt) annotate(res Result) Result {
	switch {
	case d.shelved != "":
		return res.AppendSummary("; shelved as %q", d.shelved)
	case d.discarded:
		return res.AppendSummary("; changes discarded")
	}
	return res
}

// pullClearingDirt runs pull. Only when git refuses it because of uncommitted
// work does it ask pull.dirty with wt's changes (dirt the pull does not touch
// never asks — git pulls over it), clear them as answered and run pull once
// more. abort returns cancelled with nothing touched and no error; any other
// failure is pull's own error, naming what the answer already did. A nil wt
// (no view of that worktree) leaves the refusal as it is.
func pullClearingDirt(ctx context.Context, deps OpDeps, wt GitOps, branch string, pull func() error) (pullDirt, error) {
	err := pull()
	if err == nil || wt == nil || !git.IsLocalChangesRefusal(err) {
		return pullDirt{}, err
	}
	st, serr := wt.Status(ctx)
	if serr != nil || len(st.Files) == 0 {
		return pullDirt{}, err
	}
	dir, _ := wt.TopLevel(ctx)
	resp, derr := deps.decide(ctx, PromptReq(PullDirtyDecisionID,
		"Pulling %s would overwrite local changes in %s:\n\n%s", []string{"shelve", "discard", "abort"}, branch, dir, recycleDirtyOverview(st.Files)))
	if derr != nil {
		return pullDirt{asked: true}, derr
	}
	d := pullDirt{asked: true}
	switch resp.Option {
	case "shelve":
		deps.emit(ctx, Progress{Step: "shelving", Detail: dir})
		// As in recycle: everything into the index (untracked too), since the
		// seam shelves the INDEX; nothing is discarded unless the set is stored.
		if err := wt.StageAll(ctx); err != nil {
			return d, err
		}
		e, err := deps.shelveStaged(ctx, dir, branch)
		if err != nil {
			return d, err
		}
		if err := discardAll(ctx, wt); err != nil {
			return d, fmt.Errorf("%w (changes shelved as %q)", err, e.Label)
		}
		d.shelved = e.Label
	case "discard":
		deps.emit(ctx, Progress{Step: "discarding", Detail: dir})
		if err := discardAll(ctx, wt); err != nil {
			return d, err
		}
		d.discarded = true
	default:
		return pullDirt{asked: true, cancelled: true}, nil
	}
	if err := pull(); err != nil {
		if d.shelved != "" {
			return d, fmt.Errorf("%w (changes shelved as %q)", err, d.shelved)
		}
		return d, fmt.Errorf("%w (local changes were discarded)", err)
	}
	return d, nil
}
