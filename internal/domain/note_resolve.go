package domain

import (
	"context"
	"errors"
	"fmt"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// ErrForgeResolved refuses a local resolve of a forge thread: GitHub owns
// that state until gg can publish (notes ↔ forge strategy).
var ErrForgeResolved = errors.New("forge threads are resolved on GitHub")

// ErrNotResolved is an unresolve of an open thread.
var ErrNotResolved = errors.New("thread is not resolved")

// NoteResolve marks the thread of id resolved (or open again). id may be a
// thread root, a reply (its thread), or a review remark.
func (s *Service) NoteResolve(ctx context.Context, id string, resolved bool, by string) (model.ThreadResolution, error) {
	if model.IsForgeNoteID(id) {
		return model.ThreadResolution{}, ErrForgeResolved
	}
	st := s.notesStore(ctx)
	if st == nil {
		return model.ThreadResolution{}, ErrNotesDisabled
	}
	root, fp, err := s.threadRoot(ctx, st, id)
	if err != nil {
		return model.ThreadResolution{}, err
	}
	if !resolved {
		if err := st.Unresolve(root); err != nil {
			if errors.Is(err, notes.ErrNotFound) {
				return model.ThreadResolution{}, ErrNotResolved
			}
			return model.ThreadResolution{}, err
		}
		s.invalidateNoteCounts()
		return model.ThreadResolution{}, nil
	}
	e := model.ThreadResolution{Root: root, By: by, At: notes.Now().UTC(), RemarkFP: fp}
	if err := st.Resolve(e); err != nil {
		if errors.Is(err, notes.ErrNotFound) {
			return model.ThreadResolution{}, ErrNoteNotFound
		}
		return model.ThreadResolution{}, err
	}
	s.invalidateNoteCounts()
	return e, nil
}

// threadRoot is the root id a resolution is keyed by, and the remark
// fingerprint for a remark thread.
func (s *Service) threadRoot(ctx context.Context, st notes.Store, id string) (string, string, error) {
	if rid, n, ok := model.ParseReviewNoteID(id); ok {
		r, err := s.Review(ctx, rid)
		if err != nil {
			return "", "", err
		}
		fps := r.remarkFPs()
		if n >= len(fps) {
			return "", "", fmt.Errorf("%w: review %s has no remark %d", ErrNoSuchRemark, rid, n)
		}
		return id, fps[n], nil
	}
	all, err := st.LoadAll()
	if err != nil {
		return "", "", err
	}
	for _, n := range all {
		if n.ID != id {
			continue
		}
		switch {
		case n.IsRemarkReply(): // its remark's thread, keyed as made
			return n.ParentID, n.RemarkFP, nil
		case n.IsReply():
			return n.ParentID, "", nil
		}
		return n.ID, "", nil
	}
	return "", "", ErrNoteNotFound
}

// withResolutions stamps every stored root of rns with its resolution and
// every forge root with GitHub's flag. Remark roots carry theirs already
// (reviewDocNotes). One store read per call.
func (s *Service) withResolutions(ctx context.Context, rns []ResolvedNote) []ResolvedNote {
	if len(rns) == 0 {
		return rns
	}
	var byRoot map[string]model.ThreadResolution
	if st := s.notesStore(ctx); st != nil {
		if rs, err := st.LoadAllResolved(); err == nil || len(rs) > 0 {
			byRoot = make(map[string]model.ThreadResolution, len(rs))
			for _, r := range rs {
				byRoot[r.Root] = r
			}
		}
	}
	for i := range rns {
		n := rns[i].Note
		switch {
		case n.Source == model.NoteSourceForge:
			if model.NoteHasTag(n, model.NoteTagResolved) {
				rns[i].Resolution = &model.ThreadResolution{Root: n.ID}
			}
		case rns[i].Resolution == nil:
			if r, ok := byRoot[n.ID]; ok {
				rns[i].Resolution = &r
			}
		}
	}
	return rns
}
