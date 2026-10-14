package domain

import (
	"context"
	"slices"
	"strings"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

// The overview split's reads for a permanent overview (spec 2026-10-12
// §6): the overview is presented to the review readers as a review of its
// commit's own change (a PR's: merge base..head), so remarks, diffs and the
// export are the review code's, never a copy.

// overviewReview is overview o as a review-shaped value: its commit, the
// range its D diff shows in Scope, its remarks as the document's files.
func (s *Service) overviewReview(ctx context.Context, o Overview) Review {
	r := Review{ID: o.ID, Kind: ReviewOnCommit, Commit: o.Commit, Agent: o.Agent, Summary: o.Title,
		Created: o.Created, Updated: o.Updated}
	if o.Doc != nil {
		r.Doc = &notebatch.ReviewDoc{Overview: o.Doc.Overview, Files: o.Doc.Files}
	}
	if target, _, ok := strings.Cut(o.Scope, "..."); ok && strings.TrimSpace(target) != "" {
		if base, err := s.repo.MergeBase(ctx, strings.TrimSpace(target), o.Commit); err == nil && isFullSHA(strings.TrimSpace(base)) {
			r.Scope = strings.TrimSpace(base) + ".." + o.Commit
		}
	}
	return r
}

// OverviewDocAt is overview id's document with each anchor judged against
// the WHOLE tree at its commit: any path the commit holds, lines within the
// file. One ls-tree for the anchors' paths, one read per file.
func (s *Service) OverviewDocAt(ctx context.Context, id string) (OverviewDoc, error) {
	o, err := s.Overview(ctx, id)
	if err != nil {
		return OverviewDoc{}, err
	}
	return s.overviewDocOf(ctx, o)
}

// overviewDocOf is OverviewDocAt for an overview already read.
func (s *Service) overviewDocOf(ctx context.Context, o Overview) (OverviewDoc, error) {
	if o.Doc == nil {
		return OverviewDoc{}, ErrNoOverview
	}
	_, anchors := agentdocs.ParseOverview(o.Doc.Overview)
	var paths []string
	seen := map[string]bool{}
	for _, a := range anchors {
		if a.Path != "" && !seen[a.Path] && cleanRepoPath(a.Path) {
			seen[a.Path] = true
			paths = append(paths, a.Path)
		}
	}
	inTree := map[string]bool{}
	if len(paths) > 0 {
		if found, err := s.TreePaths(ctx, o.Commit, paths); err == nil {
			for _, p := range found {
				inTree[p] = true
			}
		}
	}
	return resolveOverviewDoc(o.Doc.Overview, func(p string) bool { return inTree[p] }, func(p string) int {
		data, err := s.ShowFile(ctx, o.Commit, p)
		if err != nil {
			return -1
		}
		ls, ok := splitTextLines(data)
		if !ok {
			return -1
		}
		return len(ls)
	}), nil
}

// OverviewFileNotes are overview id's remarks on path, placed on the file at
// its commit (whose text is lines): read-only boxes, ids "review:<id>:<n>".
func (s *Service) OverviewFileNotes(ctx context.Context, id, path string, lines []string) ([]ResolvedNote, error) {
	o, err := s.Overview(ctx, id)
	if err != nil || o.Doc == nil {
		return nil, err
	}
	r := s.overviewReview(ctx, o)
	addr := model.FileAddress{State: model.StateCommitted, Commit: o.Commit, Path: path}
	return reviewDocNotes(r, path, addr, nil, lines, true), nil
}

// OverviewFileDiff is path's change in overview id's commit (a PR
// overview's: merge base..head) with the remarks on its new side — what D
// shows in the split. Changed is false when the change leaves path alone.
func (s *Service) OverviewFileDiff(ctx context.Context, id, path string) (ReviewFileDiff, error) {
	o, err := s.Overview(ctx, id)
	if err != nil {
		return ReviewFileDiff{}, err
	}
	return s.reviewFileDiffOf(ctx, s.overviewReview(ctx, o), path)
}

// PROverviews are the overviews stored on set's pull request (matched by
// number, as its notes are), newest first; Older when the PR's head moved
// past one's commit.
func (s *Service) PROverviews(ctx context.Context, set PreviewNoteSet) ([]OverviewHead, error) {
	c, err := s.NoteCounts(ctx)
	if err != nil {
		return nil, err
	}
	return set.Overviews(c), nil
}

// Overviews are the overviews among counts c stored on set's pull request
// (PROverviews without a read: a frontend holding the counts asks this).
func (set PreviewNoteSet) Overviews(c NoteCounts) []OverviewHead {
	var out []OverviewHead
	for scope, hs := range c.PreviewOverviews {
		if !set.OK() || !set.owns(scope) {
			continue
		}
		for _, h := range hs {
			h.Older = h.Commit != set.Tip
			out = append(out, h)
		}
	}
	slices.SortStableFunc(out, func(a, b OverviewHead) int { return b.Created.Compare(a.Created) })
	return out
}
