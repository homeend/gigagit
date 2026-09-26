package domain

import (
	"context"
	"sort"

	"github.com/homeend/gigagit/internal/model"
)

// NoteFileNotes is one file's threads in the all-notes overview. Notes are
// the resolved ROOTS (replies attached), orphaned ones included: the overview
// is an inventory of what is stored, not of what a diff can still show.
type NoteFileNotes struct {
	Addr model.FileAddress
	// Status/OldPath are the commit's own change to the path (A/M/D/R…, the
	// CommitFiles letter), so a frontend can open that file's diff. "" when
	// the commit does not change the path (a merge-preview note, which hangs
	// off the source tip) or the address is not a commit.
	Status  string
	OldPath string
	Notes   []ResolvedNote
}

// NoteCommitNotes is one commit's files with notes. Missing: the commit no
// longer exists (rewritten or gc'd), so Subject and When are empty.
type NoteCommitNotes struct {
	Hash     string
	Subject  string
	UnixTime int64
	Missing  bool
	Files    []NoteFileNotes
}

// NoteShelfNotes is one shelf entry's files with notes. Missing: the entry
// was removed.
type NoteShelfNotes struct {
	ID      string
	Label   string
	Missing bool
	Files   []NoteFileNotes
}

// NotesOverview is every note this checkout can see, grouped by what it hangs
// off: this worktree's unstaged/staged/untracked files, then commits (newest
// first, missing ones last), then shelf entries. A sibling worktree's
// working-tree notes are not listed — they are that checkout's.
type NotesOverview struct {
	Unstaged  []NoteFileNotes
	Staged    []NoteFileNotes
	Untracked []NoteFileNotes
	Commits   []NoteCommitNotes
	Shelves   []NoteShelfNotes
}

// Count is the number of threads (root notes) in the overview.
func (o NotesOverview) Count() int {
	n := 0
	count := func(fs []NoteFileNotes) {
		for _, f := range fs {
			n += len(f.Notes)
		}
	}
	count(o.Unstaged)
	count(o.Staged)
	count(o.Untracked)
	for _, c := range o.Commits {
		count(c.Files)
	}
	for _, s := range o.Shelves {
		count(s.Files)
	}
	return n
}

// NotesOverview builds the all-notes inventory: one store load, then per
// address the two side reads resolveNotes needs (one git show each), plus one
// metadata and one changed-files read per commit. Read-only.
func (s *Service) NotesOverview(ctx context.Context) (NotesOverview, error) {
	var ov NotesOverview
	st := s.notesStore(ctx)
	if st == nil {
		return ov, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return ov, err
	}
	cur, _ := s.TopLevel(ctx) // "" hides every worktree note, never fails the query

	// Group the store by address (the sameNoteTarget identity), replies with
	// their root's address — resolveNotes attaches them.
	type bucket struct {
		addr  model.FileAddress
		notes []model.Note
	}
	var buckets []*bucket
	byKey := map[string]*bucket{}
	rootAddr := map[string]model.FileAddress{}
	for _, n := range all {
		if !n.IsReply() {
			rootAddr[n.ID] = n.Address
		}
	}
	for _, n := range all {
		a := n.Address
		if n.IsReply() {
			ra, ok := rootAddr[n.ParentID]
			if !ok {
				continue // an orphaned reply: the sweep drops it
			}
			a = ra
		}
		if worktreeScopedNote(a) && !sameWorktreePath(a.Worktree, cur) {
			continue
		}
		key := a.Worktree + "\x00" + a.Commit + "\x00" + a.ShelfID + "\x00" + a.Path
		if worktreeScopedNote(a) {
			key += "\x00" + a.State.String()
		}
		b := byKey[key]
		if b == nil {
			b = &bucket{addr: a}
			byKey[key] = b
			buckets = append(buckets, b)
		}
		b.notes = append(b.notes, n)
	}

	commits := map[string]*NoteCommitNotes{}
	shelves := map[string]*NoteShelfNotes{}
	for _, b := range buckets {
		if err := ctx.Err(); err != nil {
			return NotesOverview{}, err
		}
		oldLines, _ := s.noteSideLines(ctx, b.addr, model.NoteSideOld)
		newLines, _ := s.noteSideLines(ctx, b.addr, model.NoteSideNew)
		f := NoteFileNotes{Addr: b.addr, Notes: resolveNotes(b.notes, oldLines, newLines)}
		if len(f.Notes) == 0 {
			continue
		}
		switch {
		case b.addr.Commit != "":
			c := commits[b.addr.Commit]
			if c == nil {
				c = s.overviewCommit(ctx, b.addr.Commit)
				commits[b.addr.Commit] = c
			}
			c.Files = append(c.Files, f)
		case b.addr.ShelfID != "":
			sh := shelves[b.addr.ShelfID]
			if sh == nil {
				sh = &NoteShelfNotes{ID: b.addr.ShelfID}
				if e, ferr := s.ShelfFind(ctx, b.addr.ShelfID); ferr == nil {
					sh.Label = e.Label
				} else {
					sh.Missing = true
				}
				shelves[b.addr.ShelfID] = sh
			}
			sh.Files = append(sh.Files, f)
		case b.addr.State == model.StateStaged:
			ov.Staged = append(ov.Staged, f)
		case b.addr.State == model.StateUntracked:
			ov.Untracked = append(ov.Untracked, f)
		default:
			ov.Unstaged = append(ov.Unstaged, f)
		}
	}

	for _, c := range commits {
		s.fillCommitFileStatus(ctx, c)
		sortNoteFiles(c.Files)
		ov.Commits = append(ov.Commits, *c)
	}
	sort.SliceStable(ov.Commits, func(i, j int) bool {
		a, b := ov.Commits[i], ov.Commits[j]
		if a.Missing != b.Missing {
			return !a.Missing
		}
		if a.UnixTime != b.UnixTime {
			return a.UnixTime > b.UnixTime
		}
		return a.Hash < b.Hash
	})
	for _, sh := range shelves {
		sortNoteFiles(sh.Files)
		ov.Shelves = append(ov.Shelves, *sh)
	}
	sort.SliceStable(ov.Shelves, func(i, j int) bool { return ov.Shelves[i].ID < ov.Shelves[j].ID })
	sortNoteFiles(ov.Unstaged)
	sortNoteFiles(ov.Staged)
	sortNoteFiles(ov.Untracked)
	return ov, nil
}

// overviewCommit reads one commit's heading: subject and author time, or
// Missing when git no longer knows the sha.
func (s *Service) overviewCommit(ctx context.Context, sha string) *NoteCommitNotes {
	c := &NoteCommitNotes{Hash: sha}
	if _, found, _ := s.CommitLookup(ctx, sha); !found {
		c.Missing = true
		return c
	}
	if meta, err := s.CommitMeta(ctx, sha); err == nil {
		c.Subject, c.UnixTime = meta.Subject, meta.UnixTime
	}
	return c
}

// fillCommitFileStatus stamps each file with the commit's own change to it,
// so a frontend opens the same parent → commit diff the files view does.
func (s *Service) fillCommitFileStatus(ctx context.Context, c *NoteCommitNotes) {
	if c.Missing {
		return
	}
	changed, err := s.CommitFiles(ctx, c.Hash)
	if err != nil {
		return
	}
	byPath := map[string]model.CommitFile{}
	for _, cf := range changed {
		byPath[cf.Path] = cf
	}
	for i := range c.Files {
		if cf, ok := byPath[c.Files[i].Addr.Path]; ok {
			c.Files[i].Status, c.Files[i].OldPath = cf.Status, cf.OldPath
		}
	}
}

func sortNoteFiles(fs []NoteFileNotes) {
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Addr.Path < fs[j].Addr.Path })
}
