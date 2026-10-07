package domain

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// ErrNoReviewChange: a link that names no change a review can read.
var ErrNoReviewChange = errors.New("the link names no change to review; give a commit, a pair, a merge preview, a branch, or a file in the working tree or the index")

var hexSHA = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// CommitReviewTarget scopes a review to ONE commit's own change, sha^..sha;
// a root commit is reviewed alone. sha must be hex — no ref name ever reaches
// the tool's <range> token. The label is "<8-char sha> <subject>", read here.
func (s *Service) CommitReviewTarget(ctx context.Context, sha string) (ReviewTarget, error) {
	if !hexSHA.MatchString(sha) {
		return ReviewTarget{}, errors.New("invalid commit")
	}
	rng := sha + "^.." + sha
	if _, ok, err := s.ResolveRev(ctx, sha+"^"); err == nil && !ok {
		rng = sha // root commit
	}
	label := sha
	if len(label) > 8 {
		label = label[:8]
	}
	if msg, err := s.CommitMessage(ctx, sha); err == nil {
		if subj := strings.TrimSpace(strings.SplitN(msg, "\n", 2)[0]); subj != "" {
			label += " " + subj
		}
	}
	return ReviewTarget{Kind: ReviewRange, Range: rng, Label: label, Diff: model.DiffSpec{Rev: rng}, Commit: sha}, nil
}

// LinkReviewTarget is the review a resolved gg:// link names: a merge preview
// or a pair → its scope (ScopeReviewTarget); a branch/tag tip → the branch
// against the trunk (BranchReviewTarget); a commit → its own change; a file in
// the working tree or the index, or @staged → the working changes. A path or
// line in the link narrows nothing: the whole change is reviewed. Call it on
// the service of the link's own checkout (res.Checkout).
func (s *Service) LinkReviewTarget(ctx context.Context, res Resolved) (ReviewTarget, error) {
	switch {
	case res.Preview != nil:
		if !res.Preview.OK() {
			return ReviewTarget{}, ErrNoReviewChange
		}
		return ScopeReviewTarget(*res.Preview), nil
	case res.Pair != nil:
		set, err := s.PairNotes(ctx, res.Pair.A, res.Pair.B)
		if err != nil {
			return ReviewTarget{}, err
		}
		if !set.OK() {
			return ReviewTarget{}, ErrNoReviewChange
		}
		return ScopeReviewTarget(set), nil
	case res.Ref != "":
		return s.BranchReviewTarget(ctx, res.Ref)
	case res.Addr.State == model.StateCommitted && res.Commit != "":
		return s.CommitReviewTarget(ctx, res.Commit)
	case res.Addr.State == model.StateStaged,
		res.Addr.State == model.StateUnstaged && res.Addr.Path != "":
		return WorkingReviewTarget(), nil
	}
	return ReviewTarget{}, ErrNoReviewChange
}

// WorkingReviewFiles fingerprints the working changes now (engine
// FingerprintWorking): the Files a working review stored by `gg review save`
// carries, so it is current until a reviewed file changes — the lane records
// the same at its Prepare.
func (s *Service) WorkingReviewFiles(ctx context.Context) ([]model.NoteFile, error) {
	res, err := s.Execute(ctx, engine.FingerprintWorking{}, nil, nil)
	if err != nil {
		return nil, err
	}
	return res.ReviewFiles, nil
}
