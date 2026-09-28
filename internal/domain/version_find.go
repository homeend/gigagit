package domain

import (
	"context"
	"errors"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// FindVersion answers a ?version=<id> hint. An id (<unix>-<op>) is unique
// only per branch — a pull records both of its branches in the same second
// under the same token — and the hint carries no branch, so the link's own
// pair (base, ours) breaks the tie. The ladder, in order:
//
//  1. a record with this id whose (Base, Ours) equal the pair
//  2. a record with this id (a colliding or foreign id)
//  3. the newest record freezing this pair (the id was minted elsewhere)
//
// ok=false is a miss — an unknown id, a deleted record, or the versions
// store disabled — never an error: the hint degrades, the link landed.
// One for-each-ref over the whole store, like AllVersionBranches.
func (s *Service) FindVersion(ctx context.Context, id, base, ours string) (branch string, v model.BranchVersion, ok bool, err error) {
	if err := s.FeatureDisabledError(ctx, FeatureVersions); err != nil {
		var disabled *ErrFeatureDisabled
		if errors.As(err, &disabled) {
			return "", model.BranchVersion{}, false, nil
		}
		return "", model.BranchVersion{}, false, err
	}
	all, err := s.repo.VersionRefs(ctx, strings.TrimSuffix(git.VersionRefPrefix, "/"))
	if err != nil {
		return "", model.BranchVersion{}, false, err
	}
	samePair := func(r model.BranchVersion) bool {
		return base != "" && ours != "" && r.Base == base && r.Ours == ours
	}
	var byID, byPair *model.BranchVersion
	for i := range all {
		r := &all[i]
		b, _, _, parsed := git.ParseVersionRef(r.Ref)
		if !parsed {
			continue
		}
		if r.ID() == id {
			if samePair(*r) {
				return b, *r, true, nil
			}
			if byID == nil {
				byID = r
			}
		} else if samePair(*r) && (byPair == nil || r.Unix > byPair.Unix) {
			byPair = r
		}
	}
	pick := byID
	if pick == nil {
		pick = byPair
	}
	if pick == nil {
		return "", model.BranchVersion{}, false, nil
	}
	b, _, _, _ := git.ParseVersionRef(pick.Ref)
	return b, *pick, true, nil
}
