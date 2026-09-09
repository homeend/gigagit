package domain

import (
	"context"
	"errors"
	"fmt"

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
