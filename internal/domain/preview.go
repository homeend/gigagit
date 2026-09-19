package domain

import (
	"context"
	"errors"
	"fmt"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/savedcompare"
)

var ErrPreviewsDisabled = errors.New("previews: no state directory available")

// ErrPreviewNotFound / ErrPreviewExists WRAP the store's errors so frontends
// (which cannot import internal/preview) can errors.Is them.
var (
	ErrPreviewNotFound = fmt.Errorf("%w", savedcompare.ErrNotFound)
	ErrPreviewExists   = fmt.Errorf("%w", savedcompare.ErrExists)
)

// A merge preview IS a saved comparison whose right half is absent: one
// bounded set, "everything feat/x would bring into main" (spec §4.5). The two
// functions below are the whole of that equivalence, and they are two arms
// that look alike and must undo each other EXACTLY.
//
// TARGET FIRST, both ways. PreviewAdd(source, target) means "what source
// brings into target", which renders merge-base(target, source)..source and
// is spelled `@target...source` — the order `git diff target...source` reads.
// A swap in either arm is invisible to a round-trip test, because a DOUBLE
// swap round-trips perfectly; only asserting the link TEXT between them, on a
// fixture whose two names differ, can see it.

// entryFromPreview renders a saved merge preview as a set-shaped entry.
func entryFromPreview(repo model.LinkRepo, p model.MergePreview) (savedcompare.Entry, error) {
	l, err := model.ParseLink(model.Link{Repo: repo, Target: model.LinkTarget{
		State:   model.StateCommitted,
		Preview: &model.LinkPreview{Source: p.Source, Target: p.Target},
	}}.String())
	if err != nil {
		return savedcompare.Entry{}, err
	}
	return savedcompare.Entry{ID: p.ID, Left: l, Label: p.Label, Created: p.Created}, nil
}

// previewFromEntry reads a set-shaped entry back as a merge preview, and
// reports false for everything that is not one: a PAIR-shaped entry (a saved
// comparison) and a set whose left half is not a three-dot preview link (a
// saved change-set). Both are legitimate savedcompare rows that the preview
// surfaces must not show — and must not delete.
func previewFromEntry(e savedcompare.Entry) (model.MergePreview, bool) {
	if e.Right != nil {
		return model.MergePreview{}, false
	}
	pv := e.Left.Target.Preview
	if pv == nil {
		return model.MergePreview{}, false
	}
	return model.MergePreview{
		ID: e.ID, Source: pv.Source, Target: pv.Target,
		Label: e.Label, Created: e.Created,
	}, true
}

// errPreviewPairShape is a three-dot --preview argument missing one side.
var errPreviewPairShape = errors.New("preview: expected <target>...<source>")

// PreviewAdd validates both sides resolve to commits and that they differ,
// then stores the pair. A duplicate returns the EXISTING record with
// ErrPreviewExists so frontends can focus it instead of failing.
func (s *Service) PreviewAdd(ctx context.Context, source, target, label string) (model.MergePreview, error) {
	st := s.savedCompareStore(ctx)
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
	repo, err := s.LinkRepo(ctx)
	if err != nil {
		return model.MergePreview{}, err
	}
	e, err := entryFromPreview(repo, model.MergePreview{Source: source, Target: target, Label: label})
	if err != nil {
		return model.MergePreview{}, err
	}
	if e.Label == "" {
		// The preview vocabulary's own default ("feat/x → main"), not the
		// store's generic one: this label is what the Previews surfaces show.
		e.Label = model.MergePreview{Source: source, Target: target}.DefaultLabel()
	}
	stored, err := st.Add(e)
	p, ok := previewFromEntry(stored)
	if !ok {
		return model.MergePreview{}, fmt.Errorf("preview: stored entry %q is not a merge preview", stored.ID)
	}
	if errors.Is(err, savedcompare.ErrExists) {
		return p, ErrPreviewExists
	}
	return p, err
}

// PreviewList returns the merge previews among the saved comparisons, in
// insertion order. A PAIR-shaped entry is a saved comparison, not a preview,
// and is filtered out here rather than rendered as a half-empty preview.
func (s *Service) PreviewList(ctx context.Context) ([]model.MergePreview, error) {
	st := s.savedCompareStore(ctx)
	if st == nil {
		return nil, ErrPreviewsDisabled
	}
	es, err := st.List()
	if err != nil {
		return nil, err
	}
	out := make([]model.MergePreview, 0, len(es))
	for _, e := range es {
		if p, ok := previewFromEntry(e); ok {
			out = append(out, p)
		}
	}
	return out, nil
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
	st := s.savedCompareStore(ctx)
	if st == nil {
		return ErrPreviewsDisabled
	}
	if _, err := s.PreviewGet(ctx, id); err != nil {
		return err // not a merge preview: never rename a saved comparison here
	}
	if err := st.Rename(id, label); errors.Is(err, savedcompare.ErrNotFound) {
		return ErrPreviewNotFound
	} else {
		return err
	}
}

func (s *Service) PreviewRemove(ctx context.Context, id string) error {
	st := s.savedCompareStore(ctx)
	if st == nil {
		return ErrPreviewsDisabled
	}
	// Look it up as a PREVIEW first: without this a saved comparison could be
	// deleted through a preview surface that never shows it, by an id the
	// user only has because the two shapes share one store.
	if _, err := s.PreviewGet(ctx, id); err != nil {
		return err
	}
	if err := st.Remove(id); errors.Is(err, savedcompare.ErrNotFound) {
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

// Base is the merge base the summary computed ("" unless PreviewOK). It rides
// the SAME cache entry as the rest of the summary, so a preview's note set
// costs no extra git call while the tips are unchanged.
func (s PreviewSummary) Base() string { return s.base }

// PreviewSummary resolves both names (missing → the matching Missing state)
// then computes the base, ahead and files. `git merge-base` failing on two
// resolvable tips means unrelated histories (rev-list --left-right would
// happily report every commit on both sides), so the base is probed FIRST —
// but only a genuine refusal means PreviewNoBase; a context cancellation
// propagates as an error (nothing is cached), mirroring ResolveRev.
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
				if ctx.Err() != nil {
					return PreviewSummary{}, err // cancelled: cache nothing, let the caller retry
				}
				sum.State = PreviewNoBase
				return sum, nil
			}
			sum.base = base
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
	left, err := model.CommitEndpoint(sum.base)
	if err != nil {
		return PreviewEndpoints{Summary: sum}, err
	}
	right, err := model.CommitEndpoint(sum.SourceHash)
	if err != nil {
		return PreviewEndpoints{Summary: sum}, err
	}
	return PreviewEndpoints{
		Summary: sum,
		Left:    left,
		Right:   right,
	}, nil
}
