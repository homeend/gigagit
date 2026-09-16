package domain

import (
	"context"
	"errors"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// ErrNoPreview means the record is from a one-branch op (amend, reset,
// undo-commit, delete-branch, restore) and records no endpoints, so there is
// nothing to render as a preview. Callers show the commit view instead.
var ErrNoPreview = errors.New("this version records no preview")

// VersionPreview returns the frozen PR-style endpoints for one version ref:
// left = the merge base recorded at snapshot time, right = the contribution.
// It feeds the existing compare pipeline, which takes commit hashes — so the
// frozen view is a lens over that pipeline, not a second renderer.
//
// The returned PreviewEndpoints.Summary is INTENTIONALLY the zero value, and
// callers must not route this result through the live-preview reconcile path.
// A Summary describes a LIVE source→target pair — its State, SourceHash and
// TargetHash are read back to decide whether the pair still exists and
// whether the tips have moved since. A frozen record has no live pair: both
// endpoints are shas that by construction can never move, and the branches
// they came from may since have been deleted or rewritten. Filling Summary in
// would hand the reconcile path a PreviewOK-looking pair whose hashes are
// historical, so it would report the frozen view as drifted the moment the
// branch advanced — and PreviewMissingSource/Target are equally wrong, since
// nothing is missing. Three consumers read Summary (the TUI previews pane,
// the web previews group, and reopenPreviewIfMoved); each must treat a
// version preview as a plain two-hash compare and skip reconciliation.
func (s *Service) VersionPreview(ctx context.Context, ref string) (PreviewEndpoints, error) {
	if err := s.FeatureDisabledError(ctx, FeatureVersions); err != nil {
		return PreviewEndpoints{}, err
	}
	branch, _, _, ok := git.ParseVersionRef(ref)
	if !ok {
		return PreviewEndpoints{}, fmt.Errorf("version preview: not a version ref: %s", ref)
	}
	vs, err := s.BranchVersions(ctx, branch)
	if err != nil {
		return PreviewEndpoints{}, err
	}
	for _, v := range vs {
		if v.Ref != ref {
			continue
		}
		if v.Base == "" || v.Ours == "" {
			return PreviewEndpoints{}, ErrNoPreview
		}
		left, err := model.CommitEndpoint(v.Base)
		if err != nil {
			return PreviewEndpoints{}, err
		}
		right, err := model.CommitEndpoint(v.Ours)
		if err != nil {
			return PreviewEndpoints{}, err
		}
		return PreviewEndpoints{
			Left:  left,
			Right: right,
		}, nil
	}
	return PreviewEndpoints{}, fmt.Errorf("version preview: no such version: %s", ref)
}
