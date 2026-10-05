package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

// Review links (spec 2026-10-05-review-links): a stored review's gg:// link is
// the change it compared plus ?review=<id>; its remarks each carry their own
// line link, so an agent reads the exact code (gg link text) and a second
// agent can check the first one's review.

// ErrReviewLinkMismatch refuses a review link whose address is not the change
// its review compared: opening some other review's commit under this id
// would mislead whoever follows the link.
var ErrReviewLinkMismatch = errors.New("the link does not match the review")

// ReviewID turns an id, or "latest" (the newest stored review), into a stored
// review's id. ErrReviewNotFound when there is none.
func (s *Service) ReviewID(ctx context.Context, idOrLatest string) (string, error) {
	if idOrLatest != "latest" {
		if _, err := s.Review(ctx, idOrLatest); err != nil {
			return "", err
		}
		return idOrLatest, nil
	}
	rs, err := s.Reviews(ctx)
	if err != nil {
		return "", err
	}
	var best Review
	for _, r := range rs {
		if best.ID == "" || r.Created.After(best.Created) {
			best = r
		}
	}
	if best.ID == "" {
		return "", ErrReviewNotFound
	}
	return best.ID, nil
}

// reviewTarget is the link target of review r: its commit, or its range as
// two full shas.
func (s *Service) reviewTarget(ctx context.Context, r Review) (model.LinkTarget, error) {
	if r.Kind == ReviewOnWorktree {
		// A review of uncommitted changes compared HEAD with the working
		// tree: its address is this checkout's working tree.
		return model.LinkTarget{State: model.StateUnstaged}, nil
	}
	base, tip, isRange := s.ReviewRevs(ctx, r)
	full := func(rev string) (string, error) {
		h, ok, err := s.ResolveRev(ctx, rev)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("review %s: %s does not resolve here", r.ID, rev)
		}
		return strings.TrimSpace(h), nil
	}
	t, err := full(tip)
	if err != nil {
		return model.LinkTarget{}, err
	}
	if !isRange {
		return model.LinkTarget{State: model.StateCommitted, Commit: t}, nil
	}
	b, err := full(base)
	if err != nil {
		return model.LinkTarget{}, err
	}
	return model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: b, B: t}}, nil
}

// linkText spells l for this repository and proves it reparses: a link
// nobody can open is worse than none.
func (s *Service) linkText(ctx context.Context, l model.Link) (string, error) {
	repo, err := s.LinkRepo(ctx)
	if err != nil {
		return "", err
	}
	return linkTextIn(repo, l)
}

// linkTextIn is linkText with the repository already resolved — a caller
// spelling many links (a review's remarks) pays for LinkRepo once.
func linkTextIn(repo model.LinkRepo, l model.Link) (string, error) {
	l.Repo = repo
	if l.Side == "" {
		l.Side = model.NoteSideNew
	}
	text := l.String()
	if _, err := model.ParseLink(text); err != nil {
		return "", fmt.Errorf("cannot express %s as a gg link: %w", text, err)
	}
	return text, nil
}

// ReviewLink is review id's gg:// link.
func (s *Service) ReviewLink(ctx context.Context, id string) (string, error) {
	r, err := s.Review(ctx, id)
	if err != nil {
		return "", err
	}
	t, err := s.reviewTarget(ctx, r)
	if err != nil {
		return "", err
	}
	return s.linkText(ctx, model.Link{Target: t, Hint: model.LinkHint{Kind: model.ReviewHintKind, ID: r.ID}})
}

// ScopeLinkText is a commit's Range review row's link: the commit pair the
// scope names at that commit, as gg://<repo>@<a>..<b> (full shas).
func (s *Service) ScopeLinkText(ctx context.Context, scope, commit string) (string, error) {
	a, b, err := s.ScopeAtCommit(ctx, scope, commit)
	if err != nil {
		return "", err
	}
	return s.linkText(ctx, model.Link{Target: model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: a, B: b}}})
}

// CommitFileLinkText is a commit's Notes row's link: the file at the commit.
func (s *Service) CommitFileLinkText(ctx context.Context, commit, path string) (string, error) {
	return s.linkText(ctx, model.Link{Path: path, Target: model.LinkTarget{State: model.StateCommitted, Commit: commit}})
}

// ReviewRemark is one note of a review document with its own line link.
type ReviewRemark struct {
	N                  int
	Path               string
	Side               model.NoteSide
	Start, End         int
	Summary, Rationale string
	Meta               []notebatch.MetaKV
	Link               string
}

// ReviewRemarks are r's document notes in document order. N is the index the
// read-time note ids use (<ReviewNoteIDPrefix><id>:<n>), stable for answers.
// A prose review has none (an empty, non-nil list).
func (s *Service) ReviewRemarks(ctx context.Context, r Review) ([]ReviewRemark, error) {
	if r.Doc == nil {
		return []ReviewRemark{}, nil
	}
	t, err := s.reviewTarget(ctx, r)
	if err != nil {
		return nil, err
	}
	repo, err := s.LinkRepo(ctx)
	if err != nil {
		return nil, err
	}
	return reviewRemarksIn(r, t, repo), nil
}

// reviewRemarksIn lists r's remarks against its resolved target and
// repository: no git at all, whatever the review's size.
func reviewRemarksIn(r Review, t model.LinkTarget, repo model.LinkRepo) []ReviewRemark {
	out := []ReviewRemark{}
	if r.Doc == nil {
		return out
	}
	n := 0
	for _, f := range r.Doc.Files {
		for _, dn := range f.Notes {
			side := model.NoteSideNew
			if dn.Side == "old" {
				side = model.NoteSideOld
			}
			path := reviewPath(f.Path) // as every reader of a review document does
			rm := ReviewRemark{N: n, Path: path, Side: side, Start: dn.Range[0], End: dn.Range[1],
				Summary: dn.Summary, Rationale: dn.Rationale, Meta: dn.Meta}
			n++
			l := model.Link{Path: path, Target: t, Side: side, Line: rm.Start}
			if rm.End > rm.Start {
				l.End = rm.End
			}
			if text, err := linkTextIn(repo, l); err == nil {
				rm.Link = text // a path a link cannot spell keeps no link; it never fails the list
			}
			out = append(out, rm)
		}
	}
	return out
}

// checkReviewHint refuses a resolved review link whose address is not the
// change its review compared — and ONLY that, a proven mismatch. A review this
// store no longer holds, or a store that cannot be read here, passes: the
// address still means something, and the consumer says what it finds.
func checkReviewHint(ctx context.Context, svc *Service, res Resolved) error {
	r, err := svc.Review(ctx, res.Hint.ID)
	if err != nil {
		return nil
	}
	t, err := svc.reviewTarget(ctx, r)
	if err != nil {
		return nil
	}
	match := t.Pair == nil && res.Pair == nil && res.Commit == t.Commit ||
		t.Pair != nil && res.Pair != nil && *res.Pair == *t.Pair
	if r.Kind == ReviewOnWorktree { // its working tree, never a commit
		match = res.Pair == nil && res.Commit == ""
	}
	if !match {
		return fmt.Errorf("%w: %w (review %s compared %s)", model.ErrLink, ErrReviewLinkMismatch, r.ID, targetText(t))
	}
	return nil
}

func targetText(t model.LinkTarget) string {
	if t.Pair != nil {
		return shortRev(t.Pair.A) + ".." + shortRev(t.Pair.B)
	}
	return shortRev(t.Commit)
}

func shortRev(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// ReviewShowRemark is one remark as `gg review show --json` and the MCP
// gg_review_show tool print it.
type ReviewShowRemark struct {
	N         int               `json:"n"`
	Path      string            `json:"path"`
	Side      string            `json:"side"`
	Start     int               `json:"start"`
	End       int               `json:"end"`
	Summary   string            `json:"summary"`
	Rationale string            `json:"rationale,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
	Link      string            `json:"link,omitempty"`
}

// ReviewShow is a stored review as an agent reads it: who, what it compared
// (Base is empty for one commit), its link, its overview (a prose review's
// whole text) and its remarks (never nil).
type ReviewShow struct {
	ID      string    `json:"id"`
	Agent   string    `json:"agent"`
	Created time.Time `json:"created"`
	Branch  string    `json:"branch,omitempty"`
	Base    string    `json:"base"`
	Tip     string    `json:"tip"`
	Link    string    `json:"link"`
	// Working: a review of uncommitted changes (HEAD ↔ the working tree);
	// Base and Tip are then empty.
	Working  bool               `json:"working,omitempty"`
	Overview string             `json:"overview"`
	Meta     map[string]string  `json:"meta,omitempty"`
	Remarks  []ReviewShowRemark `json:"remarks"`
}

// ReviewShow reads review id for an agent (gg review show, gg_review_show).
func (s *Service) ReviewShow(ctx context.Context, id string) (ReviewShow, error) {
	r, err := s.Review(ctx, id)
	if err != nil {
		return ReviewShow{}, err
	}
	// The reviewed change and the repository are resolved ONCE here; the
	// link and every remark's link are spelled from them with no more git.
	t, err := s.reviewTarget(ctx, r)
	if err != nil {
		return ReviewShow{}, err
	}
	repo, err := s.LinkRepo(ctx)
	if err != nil {
		return ReviewShow{}, err
	}
	link, err := linkTextIn(repo, model.Link{Target: t, Hint: model.LinkHint{Kind: model.ReviewHintKind, ID: r.ID}})
	if err != nil {
		return ReviewShow{}, err
	}
	out := ReviewShow{ID: r.ID, Agent: r.Agent, Created: r.Created, Branch: r.Branch, Tip: t.Commit,
		Link: link, Overview: r.Text, Remarks: []ReviewShowRemark{}, Working: r.Kind == ReviewOnWorktree}
	if t.Pair != nil {
		out.Base, out.Tip = t.Pair.A, t.Pair.B
	}
	if r.Doc != nil {
		out.Overview, out.Meta = r.Doc.Overview, metaMap(r.Doc.Meta)
	}
	for _, x := range reviewRemarksIn(r, t, repo) {
		out.Remarks = append(out.Remarks, ReviewShowRemark{N: x.N, Path: x.Path, Side: string(x.Side), Start: x.Start, End: x.End,
			Summary: x.Summary, Rationale: x.Rationale, Meta: metaMap(x.Meta), Link: x.Link})
	}
	return out, nil
}

func metaMap(kv []notebatch.MetaKV) map[string]string {
	if len(kv) == 0 {
		return nil
	}
	m := make(map[string]string, len(kv))
	for _, e := range kv {
		m[e.Key] = e.Value
	}
	return m
}
