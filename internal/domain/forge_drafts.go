package domain

import (
	"context"
	"errors"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// GroupMine is the implicit group of a PR's hand-written local notes
// ("my draft review", spec 2026-10-07 §1.3). A review's group is
// "review:<id>", a GitHub review's "github:<review id>".
const GroupMine = "mine"

// ErrUnknownForgeComment: a reply names a forge comment no cached PR holds.
var ErrUnknownForgeComment = errors.New("that GitHub comment is not in any pull request gg has read (open the PR first)")

// syncOf is a stored note's sync state and its last send error.
func syncOf(n model.Note) (model.SyncState, string) {
	st := n.Send.State()
	if st == model.SyncFailed {
		return st, n.Send.Err
	}
	return st, ""
}

// forgeCommentByID finds a cached forge comment's thread root (the thread's
// comment with no parent) across every PR read this session.
func (s *Service) forgeCommentByID(id string) (pr int, root model.ForgeComment, ok bool) {
	s.forgeMu.Lock()
	defer s.forgeMu.Unlock()
	for n, e := range s.forgeComments {
		for _, bucket := range [][]model.ForgeComment{e.c.Inline, e.c.Outdated} {
			var hit *model.ForgeComment
			for i := range bucket {
				if bucket[i].ID == id {
					hit = &bucket[i]
				}
			}
			if hit == nil {
				continue
			}
			for _, c := range bucket {
				if c.ThreadID != "" && c.ThreadID == hit.ThreadID && c.ParentID == "" {
					return n, c, true
				}
			}
			return n, *hit, true
		}
	}
	return 0, model.ForgeComment{}, false
}

// forgeReplyDraft stores n as a local draft answer to the GitHub thread
// holding comment parentID: rooted on the thread's FIRST comment, addressed
// at the PR's head and the thread's path and lines, so it is drawn and sent
// with that thread.
func (s *Service) forgeReplyDraft(ctx context.Context, parentID string, n model.Note) (model.Note, error) {
	pr, root, ok := s.forgeCommentByID(strings.TrimPrefix(parentID, model.ForgeNoteIDPrefix))
	if !ok {
		return model.Note{}, ErrUnknownForgeComment
	}
	p, _, ok := s.PRDetailsCached(pr)
	if !ok || p.HeadSHA == "" {
		return model.Note{}, ErrUnknownForgeComment
	}
	st := s.notesStore(ctx)
	if st == nil {
		return model.Note{}, ErrNotesDisabled
	}
	all, err := st.LoadAll()
	if err != nil {
		return model.Note{}, err
	}
	rn := forgeNote(root, p.HeadSHA)
	now := notes.Now().UTC()
	n.ID = notes.NewID(all)
	n.ParentID = model.ForgeNoteIDPrefix + root.ID
	n.Address = model.FileAddress{State: model.StateCommitted, Commit: p.HeadSHA, Path: root.Path}
	n.Side, n.Range = rn.Note.Side, rn.Note.Range
	if n.Source == "" {
		n.Source = model.NoteSourceUser
	}
	n.Created, n.Updated = now, now
	if err := st.Put(n); err != nil {
		return model.Note{}, err
	}
	s.invalidateNoteCounts()
	return n, nil
}

// forgeDrafts is every stored draft reply, by the forge root comment id it
// answers, in creation order.
func (s *Service) forgeDrafts(ctx context.Context) map[string][]model.Note {
	st := s.notesStore(ctx)
	if st == nil {
		return nil
	}
	all, err := st.Load(notes.PartCommits)
	if err != nil {
		return nil
	}
	out := map[string][]model.Note{}
	for _, n := range all {
		if n.IsForgeReply() {
			id := strings.TrimPrefix(n.ParentID, model.ForgeNoteIDPrefix)
			out[id] = append(out[id], n)
		}
	}
	return out
}
