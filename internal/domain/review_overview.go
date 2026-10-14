package domain

import (
	"context"
	"errors"
	"strings"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/markdown"
	"github.com/homeend/gigagit/internal/model"
)

// ErrNoOverview: the review document carries no "overview".
var ErrNoOverview = errors.New("the review has no overview")

// HasOverview reports whether r's document carries an overview (a blank one
// is none): what offers ≡ Overview and its export.
func (r Review) HasOverview() bool {
	return r.Doc != nil && strings.TrimSpace(r.Doc.Overview) != ""
}

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
	if !r.HasOverview() {
		return OverviewDoc{}, ErrNoOverview
	}
	files, err := s.ReviewFiles(ctx, r)
	if err != nil {
		return OverviewDoc{}, err
	}
	inReview := make(map[string]bool, len(files))
	for _, f := range files {
		inReview[f.Path] = true
	}
	return resolveOverviewDoc(r.Doc.Overview, func(p string) bool { return inReview[p] }, func(p string) int {
		if lines, ok := s.overviewFileLines(ctx, r, p); ok {
			return len(lines)
		}
		return -1
	}), nil
}

// resolveOverviewDoc parses text and judges each anchor: OK when inScope
// says its path counts and its lines exist (lines < 0 = unreadable). A note
// anchor (note:t<n>) is never OK in a stored overview.
func resolveOverviewDoc(text string, inScope func(path string) bool, lines func(path string) int) OverviewDoc {
	doc, anchors := agentdocs.ParseOverview(text)
	out := OverviewDoc{Text: text, Doc: doc}
	count := map[string]int{} // path → lines at the tip (-1 = unreadable)
	for _, a := range anchors {
		oa := OverviewAnchor{Anchor: a}
		switch {
		case a.Note != "" || a.Path == "" || !inScope(a.Path):
			// plain
		case a.Start == 0:
			oa.OK = true
		default:
			n, seen := count[a.Path]
			if !seen {
				n = lines(a.Path)
				count[a.Path] = n
			}
			oa.OK = n >= 0 && max(a.End, a.Start) <= n
		}
		out.Anchors = append(out.Anchors, oa)
	}
	return out
}

// overviewFileBytes reads path as the review's tip has it: the working file
// for a working review, else the file at the tip commit.
func (s *Service) overviewFileBytes(ctx context.Context, r Review, path string) ([]byte, error) {
	ref := model.FileRef{Source: model.SourceUnstaged, Path: path}
	if r.Kind != ReviewOnWorktree {
		t, err := s.reviewTarget(ctx, r)
		if err != nil {
			return nil, err
		}
		tip := t.Commit
		if t.Pair != nil {
			tip = t.Pair.B
		}
		ref = model.FileRef{Source: model.SourceCommit, Locator: tip, Path: path}
	}
	return s.ResolveBytes(ctx, ref)
}

// overviewFileLines is overviewFileBytes split in lines.
func (s *Service) overviewFileLines(ctx context.Context, r Review, path string) ([]string, bool) {
	data, err := s.overviewFileBytes(ctx, r, path)
	if err != nil {
		return nil, false
	}
	return splitTextLines(data)
}
