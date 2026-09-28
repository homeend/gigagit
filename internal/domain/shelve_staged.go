package domain

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// RecyclePlaceholder is the one empty member a recycle-shelve stores when
// every change in the worktree was a deletion: a shelf set cannot be empty
// (the .gitkeep idea); the entry's note says what it stands for.
const RecyclePlaceholder = "delete.me"

// RecycleShelfLabel names the set a recycle shelves ("WIP on <branch>", the
// stash's own default).
func RecycleShelfLabel(branch string) string { return "WIP on " + branch }

// shelveStagedIn is the engine's ShelveStaged seam: it freezes the INDEX of
// the worktree at dir — every path that differs from HEAD, which after the
// op's `git add -A` is all of its work — into ONE files entry.
//
// It runs INSIDE an op that holds this Service's TreeWrite reservation, so it
// reads through the `git -C <dir>` repo directly: a domain query would ask the
// same gate for a Read reservation and wait forever.
//
// A set carries file contents only. What it cannot carry — a deletion, the
// old side of a rename, and the placeholder standing in for an all-deletions
// change — goes into a note on the entry. A note that cannot be written takes
// the entry back out: the caller discards the worktree next, and must not do
// so with the record half-written.
func (s *Service) shelveStagedIn(ctx context.Context, dir, branch string) (model.ShelfEntry, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return model.ShelfEntry{}, ErrShelfDisabled
	}
	wt := s.repo.InDir(dir)
	head, err := wt.RevParse(ctx, "HEAD")
	if err != nil {
		return model.ShelfEntry{}, fmt.Errorf("shelve %s: %w", dir, err)
	}
	from, err := model.CommitEndpoint(head)
	if err != nil {
		return model.ShelfEntry{}, err
	}
	files, err := wt.DiffTreeFiles(ctx, from, model.IndexEndpoint())
	if err != nil {
		return model.ShelfEntry{}, fmt.Errorf("shelve %s: %w", dir, err)
	}
	var members []shelfMember
	var deleted, renamed []string
	for _, f := range files {
		switch f.Status {
		case "D":
			deleted = append(deleted, f.Path)
			continue
		case "R":
			renamed = append(renamed, f.OldPath+" → "+f.Path)
		}
		data, err := wt.ShowFile(ctx, "", f.Path) // ":<path>" = the index blob
		if err != nil {
			return model.ShelfEntry{}, fmt.Errorf("shelve %s: %s: %w", dir, f.Path, err)
		}
		members = append(members, shelfMember{name: f.Path, data: data})
	}
	placeholder := len(members) == 0
	if placeholder {
		members = []shelfMember{{name: RecyclePlaceholder}}
	}
	tarball, err := buildShelfTar(members)
	if err != nil {
		return model.ShelfEntry{}, err
	}
	origin := model.FileAddress{Worktree: dir, Branch: branch, State: model.StateStaged}
	e, err := st.PutFiles("", origin, tarball, RecycleShelfLabel(branch))
	if err != nil {
		return model.ShelfEntry{}, err
	}
	if !placeholder && len(deleted) == 0 && len(renamed) == 0 {
		return e, nil
	}
	var b strings.Builder
	if placeholder {
		b.WriteString(RecyclePlaceholder + " is an empty placeholder: every change in this worktree was a deletion, and a shelf set cannot be empty.\n")
	}
	if len(deleted) > 0 {
		b.WriteString("Deleted (not in this set):\n")
		for _, p := range deleted {
			b.WriteString("  " + p + "\n")
		}
	}
	if len(renamed) > 0 {
		b.WriteString("Renamed (stored under the new path):\n")
		for _, r := range renamed {
			b.WriteString("  " + r + "\n")
		}
	}
	if _, err := s.NoteAdd(ctx, model.Note{
		Source:    model.NoteSourceAgent,
		Author:    "gg",
		Address:   ShelfEntryNote(e.ID),
		Summary:   fmt.Sprintf("Recycled from %s (%s)", dir, branch),
		Rationale: strings.TrimRight(b.String(), "\n"),
	}); err != nil {
		_ = st.Remove(e.ID)
		return model.ShelfEntry{}, fmt.Errorf("shelve %s: writing the entry's note: %w", dir, err)
	}
	return e, nil
}
