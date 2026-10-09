package domain

import (
	"context"
	"errors"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/markdown"
	"github.com/homeend/gigagit/internal/model"
)

// ErrNoOverview: the review document carries no "overview".
var ErrNoOverview = errors.New("the review has no overview")

// OverviewAnchor is one anchor of a stored overview with its verdict: OK
// when its path is one of the review's files and its lines exist in that
// file at the reviewed tip (the working tree for a working review). A note
// anchor (note:t<n>) means nothing in a stored review: never OK.
type OverviewAnchor struct {
	agentdocs.Anchor
	OK bool
}

// OverviewDoc is a review's stored overview (spec §2): the same document
// kind as a temporary overview — the same grammar, parsed by the same
// parser — resolved against the reviewed change instead of the files on
// disk. Every frontend draws it with the temporary overview's renderer;
// an anchor that is not OK draws plain.
type OverviewDoc struct {
	Text    string
	Doc     markdown.Doc
	Anchors []OverviewAnchor
}

// Unresolved lists the destinations drawn plain, in document order: what
// `gg review save` reports so the agent can fix the text.
func (d OverviewDoc) Unresolved() []string {
	var out []string
	for _, a := range d.Anchors {
		if !a.OK {
			out = append(out, a.Dest)
		}
	}
	return out
}

// ReviewOverview parses and resolves review id's stored overview.
func (s *Service) ReviewOverview(ctx context.Context, id string) (OverviewDoc, error) {
	r, err := s.Review(ctx, id)
	if err != nil {
		return OverviewDoc{}, err
	}
	if r.Doc == nil || r.Doc.Overview == "" {
		return OverviewDoc{}, ErrNoOverview
	}
	doc, anchors := agentdocs.ParseOverview(r.Doc.Overview)
	out := OverviewDoc{Text: r.Doc.Overview, Doc: doc}
	files, err := s.ReviewFiles(ctx, r)
	if err != nil {
		return OverviewDoc{}, err
	}
	inReview := make(map[string]bool, len(files))
	for _, f := range files {
		inReview[f.Path] = true
	}
	lineCount := map[string]int{} // path → lines at the tip (-1 = unreadable)
	count := func(path string) int {
		if n, ok := lineCount[path]; ok {
			return n
		}
		n := -1
		if lines, ok := s.overviewFileLines(ctx, r, path); ok {
			n = len(lines)
		}
		lineCount[path] = n
		return n
	}
	for _, a := range anchors {
		oa := OverviewAnchor{Anchor: a}
		switch {
		case a.Note != "" || a.Path == "" || !inReview[a.Path]:
			// plain
		case a.Start == 0:
			oa.OK = true
		default:
			n := count(a.Path)
			last := max(a.End, a.Start)
			oa.OK = n >= 0 && last <= n
		}
		out.Anchors = append(out.Anchors, oa)
	}
	return out, nil
}

// overviewFileLines reads path as the review's tip has it: the working file
// for a working review, else the file at the tip commit.
func (s *Service) overviewFileLines(ctx context.Context, r Review, path string) ([]string, bool) {
	ref := model.FileRef{Source: model.SourceUnstaged, Path: path}
	if r.Kind != ReviewOnWorktree {
		t, err := s.reviewTarget(ctx, r)
		if err != nil {
			return nil, false
		}
		tip := t.Commit
		if t.Pair != nil {
			tip = t.Pair.B
		}
		ref = model.FileRef{Source: model.SourceCommit, Locator: tip, Path: path}
	}
	data, err := s.ResolveBytes(ctx, ref)
	if err != nil {
		return nil, false
	}
	return splitTextLines(data)
}
