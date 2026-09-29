package domain

import (
	"context"
	"sort"

	"github.com/homeend/gigagit/internal/model"
)

// ShelfEntryNote is the address of a note about a whole shelf entry — the
// entry id and no path (model.Note.IsShelfLevel).
func ShelfEntryNote(id string) model.FileAddress {
	return model.FileAddress{State: model.StateShelf, ShelfID: id}
}

// isShelfLevelAddr reports whether addr names a whole shelf entry.
func isShelfLevelAddr(addr model.FileAddress) bool { return model.Note{Address: addr}.IsShelfLevel() }

// ShelfNotes returns the notes on shelf entry id itself (not on its files),
// oldest first, replies attached. An entry-level note has no lines, so it is
// never resolved against text: it is always active.
func (s *Service) ShelfNotes(ctx context.Context, id string) ([]ResolvedNote, error) {
	mine, err := s.loadNotesAt(ctx, ShelfEntryNote(id))
	if err != nil {
		return nil, err
	}
	return entryNotes(mine), nil
}

// entryNotes is resolveNotes for notes that anchor on no line: every root is
// active with a zero range, replies hang off their root.
func entryNotes(ns []model.Note) []ResolvedNote {
	roots := make([]ResolvedNote, 0, len(ns))
	byID := map[string]int{}
	for _, n := range ns {
		if n.IsReply() {
			continue
		}
		byID[n.ID] = len(roots)
		roots = append(roots, ResolvedNote{Note: n, Status: model.NoteActive})
	}
	for _, n := range ns {
		if i, ok := byID[n.ParentID]; ok && n.IsReply() {
			roots[i].Replies = append(roots[i].Replies, ResolvedNote{Note: n, Status: model.NoteActive})
		}
	}
	sort.SliceStable(roots, func(a, b int) bool { return roots[a].Note.Created.Before(roots[b].Note.Created) })
	for i := range roots {
		sort.SliceStable(roots[i].Replies, func(a, b int) bool {
			return roots[i].Replies[a].Note.Created.Before(roots[i].Replies[b].Note.Created)
		})
	}
	return roots
}
