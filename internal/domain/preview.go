package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/preview"
)

var ErrPreviewsDisabled = errors.New("previews: no state directory available")

// ErrPreviewNotFound / ErrPreviewExists WRAP the store's errors so frontends
// (which cannot import internal/preview) can errors.Is them.
var (
	ErrPreviewNotFound = fmt.Errorf("%w", preview.ErrNotFound) // message stays "preview: not found"
	ErrPreviewExists   = fmt.Errorf("%w", preview.ErrExists)
)

// PreviewAdd validates both sides resolve to commits and that they differ,
// then stores the pair. A duplicate returns the EXISTING record with
// ErrPreviewExists so frontends can focus it instead of failing.
func (s *Service) PreviewAdd(ctx context.Context, source, target, label string) (model.MergePreview, error) {
	st := s.previewStore(ctx)
	if st == nil {
		return model.MergePreview{}, ErrPreviewsDisabled
	}
	if source == target {
		return model.MergePreview{}, errors.New("preview: source and target are the same branch")
	}
	for _, name := range []string{source, target} {
		if _, ok, err := s.ResolveRev(ctx, name); err != nil {
			return model.MergePreview{}, err
		} else if !ok {
			return model.MergePreview{}, fmt.Errorf("preview: %q is not a branch or commit", name)
		}
	}
	p, err := st.Add(model.MergePreview{Source: source, Target: target, Label: label})
	if errors.Is(err, preview.ErrExists) {
		return p, ErrPreviewExists
	}
	return p, err
}

func (s *Service) PreviewList(ctx context.Context) ([]model.MergePreview, error) {
	st := s.previewStore(ctx)
	if st == nil {
		return nil, ErrPreviewsDisabled
	}
	return st.List()
}

// PreviewGet finds a record by id, else by exact label (first match).
func (s *Service) PreviewGet(ctx context.Context, idOrLabel string) (model.MergePreview, error) {
	ps, err := s.PreviewList(ctx)
	if err != nil {
		return model.MergePreview{}, err
	}
	for _, p := range ps {
		if p.ID == idOrLabel {
			return p, nil
		}
	}
	for _, p := range ps {
		if p.Label == idOrLabel {
			return p, nil
		}
	}
	return model.MergePreview{}, ErrPreviewNotFound
}

func (s *Service) PreviewRename(ctx context.Context, id, label string) error {
	st := s.previewStore(ctx)
	if st == nil {
		return ErrPreviewsDisabled
	}
	if err := st.Rename(id, label); errors.Is(err, preview.ErrNotFound) {
		return ErrPreviewNotFound
	} else {
		return err
	}
}

func (s *Service) PreviewRemove(ctx context.Context, id string) error {
	st := s.previewStore(ctx)
	if st == nil {
		return ErrPreviewsDisabled
	}
	if err := st.Remove(id); errors.Is(err, preview.ErrNotFound) {
		return ErrPreviewNotFound
	} else {
		return err
	}
}

// PreviewState is a saved pair's live condition.
type PreviewState int

const (
	PreviewOK            PreviewState = iota
	PreviewMerged                     // source is already contained in target (0 ahead)
	PreviewMissingSource              // the source name no longer resolves
	PreviewMissingTarget              // the target name no longer resolves
	PreviewNoBase                     // no common ancestor
)

// String is the wire/CLI value (English protocol, not for TUI display).
func (st PreviewState) String() string {
	switch st {
	case PreviewMerged:
		return "merged"
	case PreviewMissingSource:
		return "missing-source"
	case PreviewMissingTarget:
		return "missing-target"
	case PreviewNoBase:
		return "no-base"
	}
	return "ok"
}

// PreviewSummary is the row summary of one pair. Two rev-parse calls resolve
// the names every time (that is how tip movement is detected); the three
// summary calls (merge-base, rev-list --left-right --count, diff --name-only)
// run only when the hash pair is not in the cache.
type PreviewSummary struct {
	State      PreviewState
	SourceHash string // "" when missing
	TargetHash string // "" when missing
	Files      int    // changed paths merge-base..source; 0 unless PreviewOK
	Ahead      int    // commits on source not in target; 0 unless PreviewOK
	base       string // merge-base(target, source); "" unless PreviewOK (PreviewOpen reuses it)
}

// PreviewSummary resolves both names (missing → the matching Missing state)
// then computes the base, ahead and files. `git merge-base` failing on two
// resolvable tips means unrelated histories (rev-list --left-right would
// happily report every commit on both sides), so the base is probed FIRST.
func (s *Service) PreviewSummary(ctx context.Context, source, target string) (PreviewSummary, error) {
	srcHash, ok, err := s.ResolveRev(ctx, source)
	if err != nil {
		return PreviewSummary{}, err
	}
	if !ok {
		return PreviewSummary{State: PreviewMissingSource}, nil
	}
	tgtHash, ok, err := s.ResolveRev(ctx, target)
	if err != nil {
		return PreviewSummary{}, err
	}
	if !ok {
		return PreviewSummary{State: PreviewMissingTarget, SourceHash: srcHash}, nil
	}
	key := "preview-summary:" + srcHash + ":" + tgtHash
	v, err := s.factory.Cache("preview").GetOrLoad(key, func() (any, error) {
		return query(ctx, s, key, func(ctx context.Context) (PreviewSummary, error) {
			sum := PreviewSummary{SourceHash: srcHash, TargetHash: tgtHash}
			base, err := s.repo.MergeBase(ctx, tgtHash, srcHash)
			if err != nil {
				sum.State = PreviewNoBase
				return sum, nil
			}
			sum.base = strings.TrimSpace(base)
			_, ahead, err := s.repo.CountLeftRight(ctx, tgtHash, srcHash)
			if err != nil {
				return PreviewSummary{}, err
			}
			if ahead == 0 {
				sum.State, sum.base = PreviewMerged, ""
				return sum, nil
			}
			paths, err := s.repo.DiffNameOnlyRange(ctx, tgtHash, srcHash)
			if err != nil {
				return PreviewSummary{}, err
			}
			sum.Ahead, sum.Files = ahead, len(paths)
			return sum, nil
		})
	})
	if err != nil {
		return PreviewSummary{}, err
	}
	return v.(PreviewSummary), nil
}

// PreviewEndpoints is what a frontend opens the compare view with: left =
// merge-base(target, source), right = source tip. Both zero unless PreviewOK.
type PreviewEndpoints struct {
	Summary     PreviewSummary
	Left, Right model.Endpoint
}

// PreviewOpen is PreviewSummary shaped as the two endpoints the compare
// pipeline takes (both hashes, so the diff cache stays correct).
func (s *Service) PreviewOpen(ctx context.Context, source, target string) (PreviewEndpoints, error) {
	sum, err := s.PreviewSummary(ctx, source, target)
	if err != nil || sum.State != PreviewOK {
		return PreviewEndpoints{Summary: sum}, err
	}
	return PreviewEndpoints{
		Summary: sum,
		Left:    model.Endpoint{Kind: model.EndpointCommit, Hash: sum.base},
		Right:   model.Endpoint{Kind: model.EndpointCommit, Hash: sum.SourceHash},
	}, nil
}
