package domain

import (
	"context"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/prcache"
)

// PRPreviewResult is everything a pull request's diff opens with.
type PRPreviewResult struct {
	Pair      PRPair
	Endpoints PreviewEndpoints
	Set       PreviewNoteSet     // zero unless Endpoints.Summary.State == PreviewOK
	Files     []model.CommitFile // the compare file list (nil unless PreviewOK)
	Cached    bool               // the commit list + file list came from the disk cache
}

// PRPreview is a pull request's whole open: its pair, the endpoints, the
// note set and the changed files. The merge base and the ahead count are
// computed live (two cheap calls — and the merge base is the cache key);
// the slow parts (the commit list, the file list, the file count) come from
// the disk cache when it holds THIS (merge base, head) pair and are seeded
// into the memory caches the compare view reads next; otherwise they are
// computed once and saved. Each ref resolves once (spec §2.2).
func (s *Service) PRPreview(ctx context.Context, pr model.PullRequest) (PRPreviewResult, error) {
	pair := s.PRPair(ctx, pr)
	out := PRPreviewResult{Pair: pair}
	src, okS, err := s.ResolveRev(ctx, pair.Head)
	if err != nil {
		return out, err
	}
	if !okS {
		out.Endpoints.Summary = PreviewSummary{State: PreviewMissingSource}
		return out, nil
	}
	tgt, okT, err := s.ResolveRev(ctx, pair.Base)
	if err != nil {
		return out, err
	}
	if !okT {
		out.Endpoints.Summary = PreviewSummary{State: PreviewMissingTarget, SourceHash: src}
		return out, nil
	}
	sum := PreviewSummary{SourceHash: src, TargetHash: tgt}
	base, err := s.repo.MergeBase(ctx, tgt, src)
	if err != nil {
		if ctx.Err() != nil {
			return out, err
		}
		sum.State = PreviewNoBase
		out.Endpoints.Summary = sum
		return out, nil
	}
	_, ahead, err := s.repo.CountLeftRight(ctx, tgt, src)
	if err != nil {
		return out, err
	}
	if ahead == 0 {
		sum.State = PreviewMerged
		out.Endpoints.Summary = sum
		return out, nil
	}
	sum.base, sum.Ahead = base, ahead
	if e, ok := s.prCacheEntry(ctx, pr.Number); ok {
		if d, ok := e.DerivedFor(base, src); ok && s.fresh(d.ComputedAt) {
			sum.Files = d.Files
			return s.fromDerived(pair, sum, d)
		}
	}
	// Miss: the generic path computes (and memory-caches) the slow parts.
	if sum, err = s.previewSummaryHashes(ctx, src, tgt); err != nil || sum.State != PreviewOK {
		out.Endpoints.Summary = sum
		return out, err
	}
	if out.Endpoints, err = endpointsFor(sum); err != nil {
		return out, err
	}
	if out.Set, err = s.previewNoteSetFor(ctx, pair.Head, pair.Base, sum); err != nil {
		return out, err
	}
	if out.Files, err = s.CompareFiles(ctx, out.Endpoints.Left, out.Endpoints.Right); err != nil {
		return out, err
	}
	d := prcache.Derived{MergeBase: sum.base, Source: src, Files: sum.Files,
		Commits: out.Set.Commits, FileList: out.Files, ComputedAt: s.forgeClock()}
	s.persistPR(ctx, pr.Number, func(e *prcache.Entry) { e.PutDerived(d) })
	return out, nil
}

// fromDerived serves a hit: seeds the memory caches under the keys the
// generic preview readers use, then answers from d.
func (s *Service) fromDerived(pair PRPair, sum PreviewSummary, d prcache.Derived) (PRPreviewResult, error) {
	sum.State = PreviewOK
	s.seedPreviewSummary(sum)
	s.seedPreviewRevList(sum.SourceHash, sum.TargetHash, d.Commits)
	out := PRPreviewResult{Pair: pair, Cached: true}
	var err error
	if out.Endpoints, err = endpointsFor(sum); err != nil {
		return out, err
	}
	s.seedCompareFiles(out.Endpoints.Left, out.Endpoints.Right, d.FileList)
	out.Set = PreviewNoteSet{Source: pair.Head, Target: pair.Base, Tip: sum.SourceHash, Base: sum.base, Commits: d.Commits}
	out.Files = d.FileList
	return out, nil
}

// prCacheEntry is PR n's disk entry REGARDLESS of the entry's own read time:
// derived data carries its own ComputedAt (the forge data and the git data
// age separately).
func (s *Service) prCacheEntry(ctx context.Context, n int) (prcache.Entry, bool) {
	st := s.prCacheStore(ctx)
	if st == nil {
		return prcache.Entry{}, false
	}
	return st.Load(n)
}
