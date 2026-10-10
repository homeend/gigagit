package domain

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// ErrNoteLinkGone: a ?note= link whose note this store no longer holds.
var ErrNoteLinkGone = errors.New("note is not here")

// NoteLinkText is note id's gg:// link (spec §7.1): the note's own anchor
// — its commit, index or working-tree target, path and first line, with
// the line's fingerprint when uncommitted — carrying ?note=<id>. A reply
// links its own id at its thread's anchor. A review remark id yields the
// remark's review link (one row serves both); a GitHub comment has none.
func (s *Service) NoteLinkText(ctx context.Context, id string) (string, error) {
	switch {
	case model.IsReviewNoteID(id):
		return s.ReviewRemarkLink(ctx, id)
	case model.IsForgeNoteID(id):
		return "", fmt.Errorf("%s is a GitHub comment: it has no local link", id)
	}
	byID, err := s.storedNotes(ctx)
	if err != nil {
		return "", err
	}
	n, ok := byID[id]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNoteLinkGone, id)
	}
	if n.IsRemarkReply() { // its place is the remark's: the review's own link there
		remark, err := s.currentRemarkID(ctx, n)
		if err != nil {
			return "", err
		}
		return s.ReviewRemarkLink(ctx, remark)
	}
	repo, err := s.LinkRepo(ctx)
	if err != nil {
		return "", err
	}
	// A reply's place is its thread's: the root's address and scope.
	scope := n.Preview
	if n.ParentID != "" {
		if root, ok := byID[n.ParentID]; ok {
			n.Address, n.Side, n.Range, scope = root.Address, root.Side, root.Range, root.Preview
		}
	}
	l := model.Link{Repo: repo, Path: n.Address.Path, Side: n.Side, Line: n.Range[0],
		Hint: model.LinkHint{Kind: model.NoteHintKind, ID: id}}
	if n.Range[1] > n.Range[0] {
		l.End = n.Range[1]
	}
	switch n.Address.State {
	case model.StateShelf:
		return "", fmt.Errorf("note %s is on a shelf entry: it has no link", id)
	case model.StateCommitted:
		l.Target = model.LinkTarget{State: model.StateCommitted, Commit: n.Address.Commit}
		if scope != "" {
			// A note written in a scope (a pull request, a merge preview,
			// a commit pair) is shown by that scope's view and hidden by
			// the bare commit's own diff: the link names the scope.
			l.Target = scopeLinkTarget(scope)
		}
	case model.StateStaged:
		l.Target = model.LinkTarget{State: model.StateStaged}
	default:
		l.Target = model.LinkTarget{State: model.StateUnstaged}
	}
	if l.Side == "" {
		l.Side = model.NoteSideNew
	}
	if n.Address.State != model.StateCommitted && l.Line > 0 {
		if lines, ok := linkSideLines(ctx, s, l, n.Address.Path); ok && l.Line <= len(lines) {
			switch {
			case l.End > l.Line && l.End <= len(lines):
				l.Fingerprint = model.BlockFingerprint(lines[l.Line-1 : l.End])
			case l.End > l.Line:
				// The block runs past the file's end: a range carries a
				// block fingerprint or none — a line's would read as stale.
			default:
				l.Fingerprint = model.LineFingerprint(lines[l.Line-1])
			}
		}
	}
	return linkTextIn(repo, l)
}

// scopeLinkTarget is the link target of a note scope (Note.Preview):
// "<target>...<source>" is a merge preview or pull request, "<a>..<b>" a
// commit pair — the two spellings the link grammar already carries.
func scopeLinkTarget(scope string) model.LinkTarget {
	if target, source, ok := strings.Cut(scope, "..."); ok {
		return model.LinkTarget{State: model.StateCommitted, Preview: &model.LinkPreview{Source: source, Target: target}}
	}
	a, b, _ := strings.Cut(scope, "..")
	return model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: a, B: b}}
}

// checkNoteHint refuses a ?note= link whose note the chosen checkout's
// store does not hold (a deleted note): the line would open, the thread
// the user meant would not be there.
func checkNoteHint(ctx context.Context, svc *Service, res Resolved) error {
	byID, err := svc.storedNotes(ctx)
	if err != nil {
		return nil // no store here: the address still opens; the consumer finds no thread
	}
	if _, ok := byID[res.Hint.ID]; !ok {
		return fmt.Errorf("%w: %w: note %s is not here", model.ErrLink, ErrNoteLinkGone, res.Hint.ID)
	}
	return nil
}

// NoteThread is note id's thread: the root (id itself, or its parent when
// id is a reply), the replies in creation order, and the thread's
// resolution (nil = open). A review remark's thread — asked by the remark
// id or by one of its replies — has the remark as its root (a note built
// at read time, as the review's diff shows it). ErrNoteLinkGone when
// nothing holds id.
func (s *Service) NoteThread(ctx context.Context, id string) (root model.Note, replies []model.Note, resolved *model.ThreadResolution, err error) {
	if model.IsReviewNoteID(id) {
		return s.remarkThread(ctx, id)
	}
	byID, err := s.storedNotes(ctx)
	if err != nil {
		return model.Note{}, nil, nil, err
	}
	n, ok := byID[id]
	if !ok {
		return model.Note{}, nil, nil, fmt.Errorf("%w: %s", ErrNoteLinkGone, id)
	}
	if n.IsRemarkReply() {
		return s.remarkReplyThread(ctx, n)
	}
	root = n
	if n.ParentID != "" {
		if p, ok := byID[n.ParentID]; ok {
			root = p
		}
	}
	for _, x := range byID {
		if x.ParentID == root.ID {
			replies = append(replies, x)
		}
	}
	sort.Slice(replies, func(i, j int) bool { return replies[i].Created.Before(replies[j].Created) })
	if st := s.notesStore(ctx); st != nil {
		if all, err := st.LoadAllResolved(); err == nil {
			for i := range all {
				if all[i].Root == root.ID {
					resolved = &all[i]
					break
				}
			}
		}
	}
	return root, replies, resolved, nil
}

// currentRemarkID is the id of the remark reply n answers as its review
// holds it NOW: a re-save may have moved the remark (the reply's Remark is
// the index it was written under; its fingerprint finds it again), or
// dropped it — then the reply has no remark, so no link.
func (s *Service) currentRemarkID(ctx context.Context, n model.Note) (string, error) {
	rid := n.ParentID
	r, err := s.Review(ctx, rid)
	if errors.Is(err, ErrReviewNotFound) {
		return "", reviewGone(rid)
	}
	if err != nil {
		return "", err
	}
	i := remarkFor(r.docRemarks(), n.Remark, n.RemarkFP)
	if i < 0 {
		return "", fmt.Errorf("reply %s answers a remark review %s no longer has (%s)", n.ID, rid, cutLabel(n.RemarkSummary))
	}
	return fmt.Sprintf("%s%s:%d", model.ReviewNoteIDPrefix, rid, i), nil
}

// remarkReplyThread is NoteThread for a reply to a remark: the remark's
// thread where the review holds the remark now, or — the re-save dropped
// it — the outdated thread: a root of the summary the replies answered, at
// no line, with those replies and their resolution.
func (s *Service) remarkReplyThread(ctx context.Context, n model.Note) (model.Note, []model.Note, *model.ThreadResolution, error) {
	remark, err := s.currentRemarkID(ctx, n)
	if err == nil {
		return s.remarkThread(ctx, remark)
	}
	r, rerr := s.Review(ctx, n.ParentID)
	if rerr != nil {
		return model.Note{}, nil, nil, err
	}
	_, outdated := r.RemarkThreads()
	for _, o := range outdated {
		for _, rep := range o.Replies {
			if rep.ID != n.ID {
				continue
			}
			root := model.Note{ID: o.Root, Source: model.NoteSourceAgent, Author: r.Agent, Summary: o.Summary,
				Address: reviewRootAddress(r, ""), Side: model.NoteSideNew, Created: r.Created, Updated: r.Updated}
			return root, o.Replies, o.Resolution, nil
		}
	}
	return model.Note{}, nil, nil, err
}

// reviewRootAddress is where a review's remark lives: the reviewed commit,
// or — a working review — the worktree's uncommitted file.
func reviewRootAddress(r Review, path string) model.FileAddress {
	if r.Kind == ReviewOnWorktree {
		return model.FileAddress{State: model.StateUnstaged, Worktree: r.Worktree, Path: path}
	}
	return model.FileAddress{State: model.StateCommitted, Commit: r.Commit, Path: path}
}

// remarkThread is NoteThread for a review remark: the remark as a note (the
// id the review's diff gives it, at the review's commit) and the replies
// and resolution its review keeps for it.
func (s *Service) remarkThread(ctx context.Context, remarkID string) (model.Note, []model.Note, *model.ThreadResolution, error) {
	rm, rid, err := s.reviewRemark(ctx, remarkID)
	if err != nil {
		return model.Note{}, nil, nil, err
	}
	r, err := s.Review(ctx, rid)
	if err != nil {
		return model.Note{}, nil, nil, err
	}
	root := model.Note{ID: remarkID, Source: model.NoteSourceAgent, Author: r.Agent, Preview: r.Preview,
		Address: reviewRootAddress(r, rm.Path),
		Side:    rm.Side, Range: [2]int{rm.Start, max(rm.End, rm.Start)},
		Summary: rm.Summary, Rationale: rm.Rationale, Created: r.Created, Updated: r.Updated}
	for _, kv := range rm.Meta {
		root.Tags = append(root.Tags, kv.Key+": "+kv.Value)
	}
	th, _ := r.RemarkThreads()
	if rm.N < len(th) {
		return root, th[rm.N].Replies, th[rm.N].Resolution, nil
	}
	return root, nil, nil, nil
}
