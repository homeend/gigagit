package domain

import (
	"context"
	"strconv"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// OriginWorkingTree is a carried note's origin when it lives on uncommitted
// content.
const OriginWorkingTree = "working tree"

// carriedNotes (plan Task 8 rules), by path.
func (s *Service) carriedNotes(ctx context.Context, set PreviewNoteSet) map[string][]ResolvedNote {
	if _, ok := git.ParsePRRef(set.Source); !ok || !set.OK() {
		return nil
	}
	s.mu.Lock()
	key := set.Tip + ":" + set.Base + ":" + strconv.FormatUint(s.notesGen, 10)
	if c, ok := s.carriedCache[key]; ok {
		s.mu.Unlock()
		return c
	}
	gen := s.notesGen
	s.mu.Unlock()
	st := s.notesStore(ctx)
	if st == nil {
		return nil
	}
	all, err := st.LoadAll()
	if err != nil {
		return nil
	}
	files, err := s.DiffHunks(ctx, model.DiffSpec{Rev: set.Base + ".." + set.Tip, Unified: 3})
	if err != nil {
		return nil
	}
	changed := map[string]bool{}
	for _, f := range files {
		changed[f.Path] = true
	}
	in := set.commitSet()
	replies := map[string][]model.Note{}
	for _, n := range all {
		if n.IsReply() && !n.IsForgeReply() {
			replies[n.ParentID] = append(replies[n.ParentID], n)
		}
	}
	head := map[string][]string{} // path → the PR head's lines, read once
	out := map[string][]ResolvedNote{}
	for _, n := range all {
		a := n.Address
		if n.IsReply() || n.IsEntryLevel() || n.IsReviewNote() || n.ContextHash == "" || n.Side != model.NoteSideNew ||
			a.ShelfID != "" || in[a.Commit] || !changed[a.Path] {
			continue
		}
		lines, ok := head[a.Path]
		if !ok {
			if b, ferr := s.ShowFile(ctx, set.Tip, a.Path); ferr == nil {
				lines = splitLines(b)
			}
			head[a.Path] = lines
		}
		span := n.Range[1] - n.Range[0] + 1
		start := findAnchor(lines, n.ContextHash, max(span, 1), n.Range[0])
		if start == 0 {
			continue
		}
		rng := [2]int{start, start + max(span, 1) - 1}
		origin := OriginWorkingTree
		if a.Commit != "" {
			origin = shortSHA(a.Commit)
		}
		sync, serr := syncOf(n)
		rn := ResolvedNote{Note: n, Status: model.NoteActive, Range: rng, Sync: sync, SendErr: serr, Group: GroupMine, Origin: origin}
		for _, r := range replies[n.ID] {
			rs, re := syncOf(r)
			rn.Replies = append(rn.Replies, ResolvedNote{Note: r, Status: model.NoteActive, Range: rng, Sync: rs, SendErr: re, Group: GroupMine})
		}
		out[a.Path] = append(out[a.Path], rn)
	}
	s.mu.Lock()
	if s.notesGen == gen {
		if s.carriedCache == nil {
			s.carriedCache = map[string]map[string][]ResolvedNote{}
		}
		s.carriedCache[key] = out
	}
	s.mu.Unlock()
	return out
}
