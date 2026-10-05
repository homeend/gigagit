package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// ErrForgeResolved refuses a local resolve of a forge thread: GitHub owns
// that state until gg can publish (notes ↔ forge strategy).
var ErrForgeResolved = errors.New("forge threads are resolved on GitHub")

// ErrNoteLink is a note link that is neither a gg:// link nor a commit.
var ErrNoteLink = errors.New("link is neither a gg:// link nor a commit")

// NoteLink normalises a note's link: a gg:// link that parses stays as
// written, a revision becomes its full commit sha. "" stays "".
func (s *Service) NoteLink(ctx context.Context, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if strings.HasPrefix(v, "gg://") {
		if _, err := model.ParseLink(v); err != nil {
			return "", fmt.Errorf("%w: %v", ErrNoteLink, err)
		}
		return v, nil
	}
	full, ok, err := s.ResolveRev(ctx, v)
	if err != nil || !ok {
		return "", fmt.Errorf("%w: %q", ErrNoteLink, v)
	}
	return strings.TrimSpace(full), nil
}

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
		if model.IsReviewNoteID(root) && !s.remarkResolved(ctx, root) {
			return model.ThreadResolution{}, ErrNotResolved // an entry under this id may be an outdated thread's
		}
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
// fingerprint for a remark thread. A remark thread is keyed by the remark's
// CURRENT id: its review's entries are re-keyed first, so a re-save that
// moved remarks never lets one remark's write land on another's entry.
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
		rekeyRemarkResolutions(st, r)
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
		case n.IsRemarkReply(): // its remark's thread, wherever the remark is now
			r, err := s.Review(ctx, n.ParentID)
			if err != nil {
				return "", "", err
			}
			cur := remarkFor(r.docRemarks(), n.Remark, n.RemarkFP)
			if cur < 0 {
				return "", "", fmt.Errorf("%w: the remark %s answered is gone from the re-saved review", ErrNoSuchRemark, n.Remark)
			}
			rekeyRemarkResolutions(st, r)
			return remarkIDOf(r.ID, cur), n.RemarkFP, nil
		case n.IsReply():
			return n.ParentID, "", nil
		}
		return n.ID, "", nil
	}
	return "", "", ErrNoteNotFound
}

func remarkIDOf(reviewID string, n int) string {
	return fmt.Sprintf("%s%s:%d", model.ReviewNoteIDPrefix, reviewID, n)
}

// rekeyRemarkResolutions moves each of review r's remark resolutions to the
// id its remark has now (by fingerprint): all moves out first, then in, so
// two remarks that swapped places never overwrite each other. Best effort —
// a failed move leaves the entry where it was (readers join by fingerprint
// either way).
func rekeyRemarkResolutions(st notes.Store, r Review) {
	rs := r.docRemarks()
	var moved []model.ThreadResolution
	for _, e := range r.Resolutions {
		cur := remarkFor(rs, e.Root, e.RemarkFP)
		if cur < 0 || e.Root == remarkIDOf(r.ID, cur) {
			continue
		}
		if st.Unresolve(e.Root) == nil {
			e.Root = remarkIDOf(r.ID, cur)
			moved = append(moved, e)
		}
	}
	for _, e := range moved {
		_ = st.Resolve(e)
	}
}

// withResolutions stamps every stored root of rns with its resolution and
// every forge root with GitHub's flag. Remark roots carry theirs already
// (reviewDocNotes). One store read per call.
func (s *Service) withResolutions(ctx context.Context, rns []ResolvedNote) []ResolvedNote {
	if len(rns) == 0 {
		return rns
	}
	return stampResolutions(rns, s.resolutionIndex(ctx))
}

// resolutionIndex is every stored resolution by root id: ONE read, which a
// query over many files loads once and stamps with stampResolutions.
func (s *Service) resolutionIndex(ctx context.Context) map[string]model.ThreadResolution {
	byRoot := map[string]model.ThreadResolution{}
	if st := s.notesStore(ctx); st != nil {
		rs, _ := st.LoadAllResolved() // an unreadable part hides its resolutions, never fails a read
		for _, r := range rs {
			byRoot[r.Root] = r
		}
	}
	return byRoot
}

// stampResolutions is withResolutions over an index already loaded.
func stampResolutions(rns []ResolvedNote, byRoot map[string]model.ThreadResolution) []ResolvedNote {
	for i := range rns {
		n := rns[i].Note
		switch {
		case n.Source == model.NoteSourceForge:
			if model.NoteHasTag(n, model.NoteTagResolved) {
				rns[i].Resolution = &model.ThreadResolution{Root: n.ID}
			}
		case model.IsReviewNoteID(n.ID):
			// A remark's state comes from its review's fingerprint join
			// (reviewDocNotes), never from an id-keyed entry that a re-save
			// may have left on another remark.
		case rns[i].Resolution == nil:
			if r, ok := byRoot[n.ID]; ok {
				rns[i].Resolution = &r
			}
		}
	}
	return rns
}

// resolutionOf is the thread of id's current resolution (nil = open or
// unknown).
func (s *Service) resolutionOf(ctx context.Context, id string) *model.ThreadResolution {
	st := s.notesStore(ctx)
	if st == nil {
		return nil
	}
	root, _, err := s.threadRoot(ctx, st, id)
	if err != nil {
		return nil
	}
	rs, _ := st.LoadAllResolved()
	for _, r := range rs {
		if r.Root == root {
			return &r
		}
	}
	return nil
}

// restoreResolution puts the thread of id back to prev (nil = open). Best
// effort: it undoes a failed batch.
func (s *Service) restoreResolution(ctx context.Context, id string, prev *model.ThreadResolution) {
	st := s.notesStore(ctx)
	if st == nil {
		return
	}
	root, _, err := s.threadRoot(ctx, st, id)
	if err != nil {
		return
	}
	if prev == nil {
		_ = st.Unresolve(root)
		return
	}
	_ = st.Resolve(*prev)
}

// remarkResolved reports whether remark id (its current id) is resolved, by
// the review's fingerprint join.
func (s *Service) remarkResolved(ctx context.Context, id string) bool {
	rid, n, ok := model.ParseReviewNoteID(id)
	if !ok {
		return false
	}
	r, err := s.Review(ctx, rid)
	if err != nil {
		return false
	}
	th, _ := r.RemarkThreads()
	return n < len(th) && th[n].Resolution != nil
}
