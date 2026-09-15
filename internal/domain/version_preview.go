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
		return PreviewEndpoints{
			Left:  model.Endpoint{Kind: model.EndpointCommit, Hash: v.Base},
			Right: model.Endpoint{Kind: model.EndpointCommit, Hash: v.Ours},
		}, nil
	}
	return PreviewEndpoints{}, fmt.Errorf("version preview: no such version: %s", ref)
}
