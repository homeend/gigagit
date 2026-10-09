package domain

import (
	"context"
	"errors"
	"fmt"
	"sort"

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
	repo, err := s.LinkRepo(ctx)
	if err != nil {
		return "", err
	}
	l := model.Link{Repo: repo, Path: n.Address.Path, Side: n.Side, Line: n.Range[0],
		Hint: model.LinkHint{Kind: model.NoteHintKind, ID: n.ID}}
	if n.Range[1] > n.Range[0] {
		l.End = n.Range[1]
	}
	switch n.Address.State {
	case model.StateCommitted:
		l.Target = model.LinkTarget{State: model.StateCommitted, Commit: n.Address.Commit}
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
			if l.End > l.Line && l.End <= len(lines) {
				l.Fingerprint = model.BlockFingerprint(lines[l.Line-1 : l.End])
			} else {
				l.Fingerprint = model.LineFingerprint(lines[l.Line-1])
			}
		}
	}
	return linkTextIn(repo, l)
}

// checkNoteHint refuses a ?note= link whose note the chosen checkout's
// store does not hold (a deleted note): the line would open, the thread
// the user meant would not be there.
func checkNoteHint(ctx context.Context, svc *Service, res Resolved) error {
	byID, err := svc.storedNotes(ctx)
	if err != nil {
		return nil // no store here: the address still opens, the hint is dropped with a notice
	}
	if _, ok := byID[res.Hint.ID]; !ok {
		return fmt.Errorf("%w: %w: note %s is not here", model.ErrLink, ErrNoteLinkGone, res.Hint.ID)
	}
	return nil
}

// NoteThread is note id's thread: the root (id itself, or its parent when
// id is a reply), the replies in creation order, and the thread's
// resolution (nil = open). ErrNoteLinkGone when nothing holds id.
func (s *Service) NoteThread(ctx context.Context, id string) (root model.Note, replies []model.Note, resolved *model.ThreadResolution, err error) {
	byID, err := s.storedNotes(ctx)
	if err != nil {
		return model.Note{}, nil, nil, err
	}
	n, ok := byID[id]
	if !ok {
		return model.Note{}, nil, nil, fmt.Errorf("%w: %s", ErrNoteLinkGone, id)
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
