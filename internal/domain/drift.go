package domain

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/changeset"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// DriftReport is the comparison between a frozen change set and the current
// one. Checked is false when there was nothing to compare.
type DriftReport struct {
	Ref     string
	Report  changeset.Report
	Checked bool
}

// DriftSince compares the change set a version froze against the branch's
// change set now.
//
//	before = Base..Ours   (what the branch contributed before the op)
//	after  = Other..newTip (what it contributes now, against the same other side)
//
// One rule for every op path. The tempting `oldTip..newTip` is WRONG for a
// pull-merge: the merge commit's first parent is the branch's own old tip, so
// that range is UPSTREAM's changes and every file upstream touched would be
// reported as drift.
func (s *Service) DriftSince(ctx context.Context, ref, newTip string) (DriftReport, error) {
	out := DriftReport{Ref: ref}
	branch, _, _, ok := git.ParseVersionRef(ref)
	if !ok {
		return out, fmt.Errorf("drift: not a version ref: %s", ref)
	}
	vs, err := s.BranchVersions(ctx, branch)
	if err != nil {
		return out, err
	}
	var rec *model.BranchVersion
	for i := range vs {
		if vs[i].Ref == ref {
			rec = &vs[i]
			break
		}
	}
	if rec == nil || rec.Base == "" || rec.Ours == "" || rec.Other == "" || newTip == "" {
		return out, nil // nothing recorded to compare against
	}

	before, err := s.repo.DiffNameStatus(ctx, rec.Base, rec.Ours)
	if err != nil {
		return out, err
	}
	// Skip a branch that contributed nothing — a fast-forward pull, or a branch
	// with nothing ahead. Covers those without op-path special-casing.
	if len(before) == 0 {
		return out, nil
	}
	after, err := s.repo.DiffNameStatus(ctx, rec.Other, newTip)
	if err != nil {
		return out, err
	}
	out.Report, out.Checked = changeset.Compare(before, after), true
	return out, nil
}
