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

// ErrNotInReview refuses a link to a file the review does not hold.
var ErrNotInReview = errors.New("not in the review")

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

// ReviewFileLink is the link to one file of review id: the review's change
// with path, carrying the ?review= hint, so opening it opens the review on
// that file.
func (s *Service) ReviewFileLink(ctx context.Context, id, path string) (string, error) {
	r, err := s.Review(ctx, id)
	if err != nil {
		return "", err
	}
	t, err := s.reviewTarget(ctx, r)
	if err != nil {
		return "", err
	}
	if ok, err := s.reviewHolds(ctx, r, path); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("%w: %s is not in review %s", ErrNotInReview, path, r.ID)
	}
	return s.linkText(ctx, model.Link{Path: path, Target: t, Side: model.NoteSideNew,
		Hint: model.LinkHint{Kind: model.ReviewHintKind, ID: r.ID}})
}

// reviewHolds reports whether path is one of review r's files: named by its
// document or a working review's fingerprint (no git), else listed in the
// change the review view opens (ReviewFiles).
func (s *Service) reviewHolds(ctx context.Context, r Review, path string) (bool, error) {
	if r.Doc != nil {
		for _, f := range r.Doc.Files {
			if reviewPath(f.Path) == path {
				return true, nil
			}
		}
	}
	for _, f := range r.Files {
		if f.Path == path {
			return true, nil
		}
	}
	files, err := s.ReviewFiles(ctx, r)
	if err != nil {
		return false, err
	}
	for _, f := range files {
		if f.Path == path || f.OldPath == path { // a rename's old name too
			return true, nil
		}
	}
	return false, nil
}

// ReviewRemarkLink is the review-aware link of one remark, named by its id
// review:<review id>:<n> (the id gg note reply / resolve take).
func (s *Service) ReviewRemarkLink(ctx context.Context, remarkID string) (string, error) {
	rm, rid, err := s.reviewRemark(ctx, remarkID)
	if err != nil {
		return "", err
	}
	if rm.ReviewLink == "" {
		return "", fmt.Errorf("remark %d of review %s has no link (its path cannot be spelled)", rm.N, rid)
	}
	return rm.ReviewLink, nil
}

// ReviewRemarkID is remarkID while it names a remark — its review still
// stored and holding remark n — so a copied id is one gg note reply takes.
func (s *Service) ReviewRemarkID(ctx context.Context, remarkID string) (string, error) {
	if _, _, err := s.reviewRemark(ctx, remarkID); err != nil {
		return "", err
	}
	return remarkID, nil
}

// reviewRemark finds the remark remarkID names, and its review's id.
func (s *Service) reviewRemark(ctx context.Context, remarkID string) (ReviewRemark, string, error) {
	rid, n, ok := model.ParseReviewNoteID(remarkID)
	if !ok {
		return ReviewRemark{}, "", fmt.Errorf("not a review remark id: %q", remarkID)
	}
	r, err := s.Review(ctx, rid)
	if errors.Is(err, ErrReviewNotFound) {
		return ReviewRemark{}, rid, fmt.Errorf("%w: review %s no longer exists", ErrReviewNotFound, rid)
	}
	if err != nil {
		return ReviewRemark{}, rid, err
	}
	rms, err := s.ReviewRemarks(ctx, r)
	if err != nil {
		return ReviewRemark{}, rid, err
	}
	for _, rm := range rms {
		if rm.N == n {
			return rm, rid, nil
		}
	}
	return ReviewRemark{}, rid, fmt.Errorf("%w: review %s has no remark %d", ErrNoSuchRemark, rid, n)
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
	// ReviewLink is Link with the ?review= hint: it reopens THIS review at
	// the remark (Link names only the change's line).
	ReviewLink string
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
			l.Hint = model.LinkHint{Kind: model.ReviewHintKind, ID: r.ID}
			if text, err := linkTextIn(repo, l); err == nil {
				rm.ReviewLink = text
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
	// ID is the remark's thread id ("review:<review id>:<n>"): what
	// gg note reply / resolve take.
	ID        string            `json:"id"`
	N         int               `json:"n"`
	Path      string            `json:"path"`
	Side      string            `json:"side"`
	Start     int               `json:"start"`
	End       int               `json:"end"`
	Summary   string            `json:"summary"`
	Rationale string            `json:"rationale,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
	Link      string            `json:"link,omitempty"`
	// ReviewLink reopens this review at the remark (Link + ?review=<id>).
	ReviewLink string `json:"review_link,omitempty"`
	// The remark's thread: resolved (by whom) and its replies.
	Resolved   bool              `json:"resolved"`
	ResolvedBy string            `json:"resolved_by,omitempty"`
	Replies    []ReviewShowReply `json:"replies,omitempty"`
}

// ReviewShowReply is one reply in a remark's thread.
type ReviewShowReply struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Summary   string    `json:"summary"`
	Rationale string    `json:"rationale,omitempty"`
	Link      string    `json:"link,omitempty"`
	Created   time.Time `json:"created"`
}

// ReviewShowOutdated is a thread whose remark the re-saved review no longer
// has. It carries no remark id: the one it was made under now names another
// remark (or none), so nothing may be addressed through it.
type ReviewShowOutdated struct {
	Summary  string            `json:"summary"`
	Resolved bool              `json:"resolved"`
	Replies  []ReviewShowReply `json:"replies,omitempty"`
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
	// Resolved counts the resolved remarks; Outdated lists the threads
	// whose remark is gone from the re-saved review.
	Resolved int                  `json:"resolved"`
	Outdated []ReviewShowOutdated `json:"outdated,omitempty"`
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
	th, outdated := r.RemarkThreads()
	for _, x := range reviewRemarksIn(r, t, repo) {
		rm := ReviewShowRemark{ID: fmt.Sprintf("%s%s:%d", model.ReviewNoteIDPrefix, r.ID, x.N),
			N: x.N, Path: x.Path, Side: string(x.Side), Start: x.Start, End: x.End,
			Summary: x.Summary, Rationale: x.Rationale, Meta: metaMap(x.Meta), Link: x.Link, ReviewLink: x.ReviewLink}
		if x.N < len(th) {
			if res := th[x.N].Resolution; res != nil {
				rm.Resolved, rm.ResolvedBy = true, res.By
				out.Resolved++
			}
			rm.Replies = showReplies(th[x.N].Replies)
		}
		out.Remarks = append(out.Remarks, rm)
	}
	for _, o := range outdated {
		out.Outdated = append(out.Outdated, ReviewShowOutdated{Summary: o.Summary,
			Resolved: o.Resolution != nil, Replies: showReplies(o.Replies)})
	}
	return out, nil
}

func showReplies(ns []model.Note) []ReviewShowReply {
	var out []ReviewShowReply
	for _, n := range ns {
		out = append(out, ReviewShowReply{ID: n.ID, Author: n.Author, Summary: n.Summary,
			Rationale: n.Rationale, Link: n.Link, Created: n.Created})
	}
	return out
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
