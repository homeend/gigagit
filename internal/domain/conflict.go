package domain

import (
	"context"

	"github.com/gigagit/gg/internal/model"
)

// ConflictState attributes the current conflict to the operation that produced
// it. Zero value (Op == "") means no in-progress merge/rebase (e.g. a stash-pop
// conflict) — there is no source to show.
type ConflictState struct {
	Op     string // "merge" | "rebase" | "cherry-pick" | "revert" | ""
	Source string // branch being merged / rebased, or the picked/reverted commit
	Target string // branch merged-into / rebased-onto
}

// Describe renders the human phrase, or "" when there is nothing to attribute.
func (c ConflictState) Describe() string {
	switch {
	case c.Op == "merge" && c.Source != "" && c.Target != "":
		return "merging " + c.Source + " into " + c.Target
	case c.Op == "rebase" && c.Source != "" && c.Target != "":
		return "rebasing " + c.Source + " onto " + c.Target
	case c.Op == "cherry-pick" && c.Source != "":
		return "cherry-picking " + c.Source
	case c.Op == "revert" && c.Source != "":
		return "reverting " + c.Source
	}
	return ""
}

// conflictState attributes st's conflicts to a merge/rebase in progress. It runs
// git probes only when st actually has unmerged files, so clean repos pay
// nothing. During a rebase HEAD is detached, so the rebase target comes from the
// rebase state (RebaseParties), not st.Branch.
func (s *Service) conflictState(ctx context.Context, st model.WorkingTreeStatus) ConflictState {
	if st.Counts().Conflicted == 0 {
		return ConflictState{}
	}
	if ok, err := s.repo.MergeInProgress(ctx, ""); err == nil && ok {
		src, _ := s.repo.MergeHeadName(ctx, "")
		return ConflictState{Op: "merge", Source: src, Target: st.Branch}
	}
	if ok, err := s.repo.RebaseInProgress(ctx, ""); err == nil && ok {
		branch, onto, _ := s.repo.RebaseParties(ctx, "")
		return ConflictState{Op: "rebase", Source: branch, Target: onto}
	}
	// Cherry-pick/revert probed last: a paused rebase pick also sets
	// CHERRY_PICK_HEAD.
	if ok, err := s.repo.CherryPickInProgress(ctx, ""); err == nil && ok {
		src, _ := s.repo.CherryPickHeadSummary(ctx, "")
		return ConflictState{Op: "cherry-pick", Source: src}
	}
	if ok, err := s.repo.RevertInProgress(ctx, ""); err == nil && ok {
		src, _ := s.repo.RevertHeadSummary(ctx, "")
		return ConflictState{Op: "revert", Source: src}
	}
	return ConflictState{}
}

// InProgressOp reports "merge", "rebase", "cherry-pick", "revert", or "" for the
// current working tree. Cherry-pick/revert are probed LAST (a paused rebase pick
// also sets CHERRY_PICK_HEAD, so rebase must win).
func (s *Service) InProgressOp(ctx context.Context) (string, error) {
	return query(ctx, s, "inprogress", func(ctx context.Context) (string, error) {
		if ok, err := s.repo.MergeInProgress(ctx, ""); err == nil && ok {
			return "merge", nil
		}
		if ok, err := s.repo.RebaseInProgress(ctx, ""); err == nil && ok {
			return "rebase", nil
		}
		if ok, err := s.repo.CherryPickInProgress(ctx, ""); err == nil && ok {
			return "cherry-pick", nil
		}
		if ok, err := s.repo.RevertInProgress(ctx, ""); err == nil && ok {
			return "revert", nil
		}
		return "", nil
	})
}
