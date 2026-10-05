package domain

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// Which store parts a read needs (spec 2026-10-04 §5). Ids, NewID and the
// sweep read every part (LoadAll); everything else names its parts here, so
// a working-tree read never parses commit, preview or other worktrees' notes.

// loadParts reads the named parts and concatenates them.
func loadParts(st notes.Store, parts ...notes.Part) ([]model.Note, error) {
	var all []model.Note
	for _, p := range parts {
		ns, err := st.Load(p)
		if err != nil {
			return nil, err
		}
		all = append(all, ns...)
	}
	return all, nil
}

// addrParts names the parts that can hold notes for addr. A live addr must
// already carry its worktree (noteWorktree). A commit's notes include the
// ones written in a scope — PlainNotes filters them later — so a commit
// address reads previews too.
func addrParts(addr model.FileAddress) []notes.Part {
	switch {
	case addr.ShelfID != "":
		return []notes.Part{notes.PartShelf}
	case worktreeScopedNote(addr):
		return []notes.Part{notes.WorktreePart(addr.Worktree)}
	}
	return []notes.Part{notes.PartCommits, notes.PartPreviews}
}

// visibleParts is what this checkout can see: every shared part plus its
// own worktree's — never a sibling worktree's file.
func (s *Service) visibleParts(ctx context.Context) []notes.Part {
	parts := []notes.Part{notes.PartCommits, notes.PartPreviews, notes.PartShelf}
	if cur, err := s.TopLevel(ctx); err == nil && strings.TrimSpace(cur) != "" {
		parts = append(parts, notes.WorktreePart(strings.TrimSpace(cur)))
	}
	return parts
}

// scopeParts names the parts a preview set reads: a merge preview's own
// notes, a commit pair's (stored with commits), or — a pull request, scope
// "" — every note on its commits.
func scopeParts(scope string) []notes.Part {
	switch {
	case IsPreviewScope(scope):
		return []notes.Part{notes.PartPreviews}
	case scope != "":
		return []notes.Part{notes.PartCommits}
	}
	return []notes.Part{notes.PartCommits, notes.PartPreviews}
}
