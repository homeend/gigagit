package domain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// ErrNotesDisabled means no state directory was resolvable.
var ErrNotesDisabled = errors.New("notes: no state directory available")

// ErrNoteNotFound means the named note is not in the store — a stale page after
// a sweep, another client's delete, or (the common CLI case) an id from a
// DIFFERENT repository's store. It UNWRAPS to the store's own
// notes.ErrNotFound, so code that already matches that keeps working, while a
// frontend (which archtest forbids from importing internal/notes) can match
// this one with errors.Is instead of grepping the message. It is a value of
// its own type rather than fmt.Errorf("note: %w", …) so the text reads
// "note not found", not the doubled "note: notes: not found".
var ErrNoteNotFound error = noteNotFoundError{}

type noteNotFoundError struct{}

func (noteNotFoundError) Error() string { return "note not found" }
func (noteNotFoundError) Unwrap() error { return notes.ErrNotFound }

// ResolvedNote is one note as it applies to an OPEN diff: the stored record,
// its computed status, the range it actually occupies now (the stored range
// when active in place, the found range when it moved, the clamped range when
// stale) and its replies, which share the root's anchor.
type ResolvedNote struct {
	Note    model.Note
	Status  model.NoteStatus
	Range   [2]int
	Replies []ResolvedNote
}

// NoteCounts are the row-painter badges: how many note THREADS (root notes,
// replies excluded) hang off each target. Unresolved by design — the Files and
// Commits painters must never touch the store or read file content.
//
// The maps are the cached instance, shared by every caller: READ-ONLY.
type NoteCounts struct {
	ByPath       map[string]int // working-tree notes, by repo-relative path
	ByCommit     map[string]int // commit notes, by sha
	ByCommitPath map[string]int // commit notes, by "<sha>:<path>"
}

// NoteAdd stores a new note, filling ID, Created/Updated and (when the caller
// left it empty) ContextHash — read from the note's own side text. Frontends
// that already display the anchored line pass the hash so it matches exactly
// what the user saw.
func (s *Service) NoteAdd(ctx context.Context, n model.Note) (model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return model.Note{}, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return model.Note{}, err
	}
	if n.ID == "" {
		n.ID = notes.NewID(all)
	}
	if n.Source == "" {
		n.Source = model.NoteSourceUser
	}
	if n.Side == "" {
		n.Side = model.NoteSideNew
	}
	now := notes.Now().UTC()
	if n.Created.IsZero() {
		n.Created = now
	}
	n.Updated = now
	// A commit note's target is its FULL COMMIT sha, so a CLI note on "HEAD"
	// and a TUI note on the same commit share one address (sameNoteTarget
	// compares Commit verbatim) — and an annotated tag's own object id (which
	// also happens to be 40 hex) must never be stored in its place. ResolveRev
	// peels to ^{commit}, so a tag resolves to the commit it points at.
	// Best-effort: a runner that cannot resolve (the FakeRunner suites, a
	// detached store) keeps the value as given rather than failing a write —
	// the CLI validates the rev up front in NoteTarget.
	if n.Address.State == model.StateCommitted && n.Address.Commit != "" && !isFullSHA(n.Address.Commit) {
		if full, found, ferr := s.ResolveRev(ctx, n.Address.Commit); ferr == nil && found {
			if full = strings.TrimSpace(full); isFullSHA(full) {
				n.Address.Commit = full
			}
		}
	}
	// Pin the checkout BEFORE the hash fill: noteSideLines reads the note's
	// own worktree, and every later match is made against this value.
	if worktreeScopedNote(n.Address) {
		wt, werr := s.noteWorktree(ctx, n.Address)
		if werr != nil {
			return model.Note{}, werr
		}
		n.Address.Worktree = wt
	}
	// The fingerprint is what re-anchors a note. Storing one WITHOUT it is
	// worse than refusing: findAnchor never matches an empty hash, so such a
	// note is born permanently stale and the next sweep deletes it — silently,
	// long after the caller was told the write succeeded. So a side that cannot
	// be read, or that is not there at all, is an error the caller sees now.
	if n.ContextHash == "" {
		lines, lerr := s.noteSideLines(ctx, n.Address, n.Side)
		if lerr != nil {
			return model.Note{}, fmt.Errorf("notes: cannot read the %s side of %s: %w", n.Side, n.Address.Path, lerr)
		}
		if lines == nil {
			return model.Note{}, fmt.Errorf("notes: the %s side of %s does not exist", n.Side, n.Address.Path)
		}
		if err := checkNoteRange(n.Range, n.Side, n.Address.Path, lines); err != nil {
			return model.Note{}, err
		}
		n.ContextHash = model.NoteContextHash(anchorLines(lines, n.Range))
	}
	if err := st.Put(n); err != nil {
		return model.Note{}, err
	}
	s.invalidateNoteCounts()
	return n, nil
}

// NoteRangeCheck validates rng against addr's side WITHOUT storing anything —
// the same bounds NoteAdd enforces at write time (see the comment there).
// It lets a batch importer (gg note apply --stdin, the review importer, the
// MCP tool) reject every bad item in its VALIDATION pass, before the first
// write, rather than discovering a bad item mid-batch after earlier items are
// already stored.
func (s *Service) NoteRangeCheck(ctx context.Context, addr model.FileAddress, side model.NoteSide, rng [2]int) error {
	if worktreeScopedNote(addr) {
		wt, err := s.noteWorktree(ctx, addr)
		if err != nil {
			return err
		}
		addr.Worktree = wt
	}
	lines, err := s.noteSideLines(ctx, addr, side)
	if err != nil {
		return fmt.Errorf("notes: cannot read the %s side of %s: %w", side, addr.Path, err)
	}
	if lines == nil {
		return fmt.Errorf("notes: the %s side of %s does not exist", side, addr.Path)
	}
	return checkNoteRange(rng, side, addr.Path, lines)
}

// checkNoteRange is the ONE bound both NoteAdd (at write time) and
// NoteRangeCheck (a batch importer's pre-write validation) enforce:
//
//   - a structurally bad range (start < 1, or start > end) is always refused.
//   - against a side that HAS content, a range running past its end is
//     refused — --new-line 100 on a 4-line file must not silently clamp.
//   - against a side with NO content (len(lines) == 0 — a root commit's
//     empty old side is the one legitimate case, see
//     TestNoteAddOnARootCommitFillsAFingerprint), only the exact sentinel
//     range {1,1} is accepted; any other range (including another
//     out-of-range value like {500,500}) is refused. Without this narrower
//     rule ANY range on an empty side would pass silently and the note would
//     be dropped by the next sweep with no explanation.
func checkNoteRange(rng [2]int, side model.NoteSide, path string, lines []string) error {
	if rng[0] < 1 || rng[0] > rng[1] {
		return fmt.Errorf("notes: invalid range %d-%d for the %s side of %s", rng[0], rng[1], side, path)
	}
	if len(lines) == 0 {
		if rng == [2]int{1, 1} {
			return nil
		}
		return fmt.Errorf("notes: line %d is past the end of the %s side of %s (0 lines)", rng[1], side, path)
	}
	if rng[1] > len(lines) {
		return fmt.Errorf("notes: line %d is past the end of the %s side of %s (%d lines)", rng[1], side, path, len(lines))
	}
	return nil
}

// NoteEdit replaces one note's summary and rationale.
func (s *Service) NoteEdit(ctx context.Context, id, summary, rationale string) error {
	if model.IsForgeNoteID(id) {
		return ErrReadOnlyNote
	}
	st := s.notesStore(ctx)
	if st == nil {
		return ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return err
	}
	for _, n := range all {
		if n.ID != id {
			continue
		}
		n.Summary, n.Rationale, n.Updated = summary, rationale, notes.Now().UTC()
		if err := st.Put(n); err != nil {
			return err
		}
		s.invalidateNoteCounts()
		return nil
	}
	return ErrNoteNotFound
}

// NoteReply stores a reply that inherits its parent's anchor (address, side,
// range, fingerprint) so the thread re-anchors as one.
//
// Threads are FLAT: replying to a reply attaches to that reply's root. Nesting
// is not merely unrendered — the store's orphan-reply prune builds its root set
// from non-replies, so a note whose parent is itself a reply would be dropped
// inside Put while this call reported success.
func (s *Service) NoteReply(ctx context.Context, parentID string, n model.Note) (model.Note, error) {
	if model.IsForgeNoteID(parentID) {
		return model.Note{}, ErrReadOnlyNote
	}
	st := s.notesStore(ctx)
	if st == nil {
		return model.Note{}, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return model.Note{}, err
	}
	byID := make(map[string]model.Note, len(all))
	for _, p := range all {
		byID[p.ID] = p
	}
	root, ok := byID[parentID]
	if !ok {
		return model.Note{}, ErrNoteNotFound
	}
	// Walk up to the root, bounded by the record count so corrupt data (a
	// parent cycle) cannot spin here.
	for i := 0; root.IsReply() && i < len(all); i++ {
		p, found := byID[root.ParentID]
		if !found {
			break
		}
		root = p
	}
	n.ParentID = root.ID
	n.Address, n.Side, n.Range, n.ContextHash = root.Address, root.Side, root.Range, root.ContextHash
	return s.NoteAdd(ctx, n)
}

// NoteRemove deletes a note; a root takes its replies with it.
func (s *Service) NoteRemove(ctx context.Context, id string) error {
	if model.IsForgeNoteID(id) {
		return ErrReadOnlyNote
	}
	st := s.notesStore(ctx)
	if st == nil {
		return ErrNotesDisabled
	}
	if err := st.Remove(id); err != nil {
		if errors.Is(err, notes.ErrNotFound) {
			return ErrNoteNotFound // the sentinel frontends can match
		}
		return err
	}
	s.invalidateNoteCounts()
	return nil
}

// NotesClear removes every note stored at addr — roots AND replies — and
// reports how many records went. Nothing else is touched: the store is shared
// by the whole repo, so the sameNoteTarget rule (path + commit + shelf, plus
// the worktree for live content) is what bounds the write.
//
// It is one store write: Sweep's predicate is the bulk primitive, and since a
// reply inherits its root's Address (NoteReply copies it) the one predicate
// takes the whole thread. Nothing else matches, so an address with no notes
// costs a single Load and never creates the file.
func (s *Service) NotesClear(ctx context.Context, addr model.FileAddress) (int, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return 0, ErrNotesDisabled
	}
	// Scope to THIS checkout, exactly as NotesFor/NotesAt do — otherwise a
	// clear in worktree A would take worktree B's notes on the same path.
	if worktreeScopedNote(addr) {
		wt, err := s.noteWorktree(ctx, addr)
		if err != nil {
			return 0, err
		}
		addr.Worktree = wt
	}
	dropped, err := st.Sweep(func(n model.Note) bool { return !sameNoteTarget(n.Address, addr) })
	if dropped > 0 {
		s.invalidateNoteCounts()
	}
	return dropped, err
}

// NotesFor returns the notes that apply to addr, resolved against the OPEN
// diff d and threaded (roots carry their replies), sorted new-side-first then
// by line. Orphaned notes are omitted — they are hidden until the startup
// sweep drops them. Reads never rewrite the store.
func (s *Service) NotesFor(ctx context.Context, addr model.FileAddress, d Diff) ([]ResolvedNote, error) {
	mine, err := s.loadNotesAt(ctx, addr)
	if err != nil {
		return nil, err
	}
	oldLines, newLines := diffSideLines(d)
	return keepResolved(resolveNotes(mine, oldLines, newLines)), nil
}

// loadNotesAt is the STORE half of a note read, shared by NotesFor and
// NotesAt: load once, scope a worktree-state query to this checkout, keep
// what sameNoteTarget claims. Splitting it out is what keeps ONE resolver in
// the codebase — every caller below this line hands its notes to
// resolveNotes and nothing else.
func (s *Service) loadNotesAt(ctx context.Context, addr model.FileAddress) ([]model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return nil, err
	}
	// Scope the query to this checkout so a sibling worktree's notes on the
	// same path never surface here.
	if worktreeScopedNote(addr) {
		wt, werr := s.noteWorktree(ctx, addr)
		if werr != nil {
			return nil, werr
		}
		addr.Worktree = wt
	}
	mine := make([]model.Note, 0, len(all))
	for _, n := range all {
		if sameNoteTarget(n.Address, addr) {
			mine = append(mine, n)
		}
	}
	return mine, nil
}

// keepResolved drops the orphans every note read hides (§4.4).
func keepResolved(res []ResolvedNote) []ResolvedNote {
	kept := res[:0]
	for _, r := range res {
		if r.Status != model.NoteOrphaned {
			kept = append(kept, r)
		}
	}
	return kept
}

// NotesAt resolves the notes for addr WITHOUT a caller-supplied diff, reading
// both sides itself (the noteSideLines table). It is the stateless callers'
// door — the web handlers and, in phase 2, the CLI — while NotesFor stays the
// zero-extra-read path for a frontend that already holds the diff.
//
// Like NotesFor it scopes a worktree-state query to THIS checkout: the store
// is shared by every worktree of the repo, and a caller that reaches this
// method has no business naming someone else's (the HTTP handlers must never
// take a Worktree from the wire).
func (s *Service) NotesAt(ctx context.Context, addr model.FileAddress) ([]ResolvedNote, error) {
	mine, err := s.loadNotesAt(ctx, addr)
	if err != nil {
		return nil, err
	}
	if len(mine) == 0 {
		return nil, nil
	}
	// Only now are the two sides worth reading: the reads shell out to git,
	// and an address with no notes at all must cost nothing.
	oldLines, _ := s.noteSideLines(ctx, addr, model.NoteSideOld)
	newLines, _ := s.noteSideLines(ctx, addr, model.NoteSideNew)
	return keepResolved(resolveNotes(mine, oldLines, newLines)), nil
}

// NoteCounts returns the badge counts, cached until the next mutation. The
// returned maps are the cache itself — callers must treat them as read-only.
func (s *Service) NoteCounts(ctx context.Context) (NoteCounts, error) {
	s.mu.Lock()
	if s.noteCounts != nil {
		c := *s.noteCounts
		s.mu.Unlock()
		return c, nil
	}
	gen := s.notesGen
	s.mu.Unlock()

	st := s.notesStore(ctx)
	if st == nil {
		return NoteCounts{}, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return NoteCounts{}, err
	}
	// ByPath badges the CURRENT checkout's Files panel, so a worktree note
	// belonging to a sibling worktree of the same repo must not appear in it.
	// A checkout that cannot be resolved (a bare repo, a stale cwd) must NOT
	// fail the query: commit badges need no worktree at all, so an empty cur
	// simply leaves ByPath empty.
	cur, _ := s.TopLevel(ctx)
	c := NoteCounts{ByPath: map[string]int{}, ByCommit: map[string]int{}, ByCommitPath: map[string]int{}}
	for _, n := range all {
		if n.IsReply() { // a badge counts THREADS
			continue
		}
		if n.Address.State == model.StateCommitted && n.Address.Commit != "" {
			c.ByCommit[n.Address.Commit]++
			if n.Address.Path != "" {
				c.ByCommitPath[n.Address.Commit+":"+n.Address.Path]++
			}
			continue
		}
		// A shelf note carries a Path but lives in no checkout: it must not
		// badge the same path in the working tree.
		if n.Address.ShelfID != "" || n.Address.Path == "" {
			continue
		}
		if !sameWorktreePath(n.Address.Worktree, cur) {
			continue
		}
		c.ByPath[n.Address.Path]++
	}
	s.mu.Lock()
	if s.notesGen == gen { // a mutation raced this computation: drop it
		s.noteCounts = &c
	}
	s.mu.Unlock()
	return c, nil
}

// InvalidateNoteCounts drops the cached badge counts so the next NoteCounts
// call recomputes them from the store. Mutations invalidate on their own; this
// is for an EXPLICIT refresh (the TUI's r / a srcNotes reload, the web's counts
// endpoint), which must also see writes this process did not make: the startup
// sweep's rewrite, another gg's note, a phase-2 `gg note add`. srcNotes is
// never polled, so the extra Load costs one file read per deliberate refresh.
func (s *Service) InvalidateNoteCounts() { s.invalidateNoteCounts() }

func (s *Service) invalidateNoteCounts() {
	s.mu.Lock()
	s.noteCounts = nil
	s.previewCounts = nil // preview badges count the same store
	s.notesGen++
	s.mu.Unlock()
}

// sameNoteTarget decides whether a stored note belongs to the diff at addr.
// Path + Commit + ShelfID are the identity: a working-tree note (Commit == "")
// shows on the unstaged AND the staged diff of the same file — the stored
// State only names the OLD-side base for the sweep, and re-anchoring absorbs
// the line-number difference between index and working tree.
//
// Worktree-state notes additionally match on the worktree. The store is keyed
// by the git COMMON dir and therefore shared by every worktree of the repo,
// but their content (index, HEAD, working file) is per-worktree: without this,
// a note taken in worktree A would be listed — and, worse, resolved and swept
// — against worktree B's copy of the same path.
func sameNoteTarget(a, b model.FileAddress) bool {
	if a.Path != b.Path || a.Commit != b.Commit || a.ShelfID != b.ShelfID {
		return false
	}
	if worktreeScopedNote(a) {
		return sameWorktreePath(a.Worktree, b.Worktree)
	}
	return true // commit and shelf notes are worktree-agnostic
}

// worktreeScopedNote reports whether an address names live worktree content
// (as opposed to a commit or a shelf entry, which every worktree shares).
func worktreeScopedNote(a model.FileAddress) bool {
	return a.Commit == "" && a.ShelfID == ""
}

// sameWorktreePath compares two checkout roots in native notation. Both sides
// are stored cleaned (NoteAdd fills them from TopLevel), so this is a plain
// comparison; Clean is re-applied because a caller-supplied query address may
// carry a trailing separator.
func sameWorktreePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// noteWorktree resolves the checkout a worktree-state note belongs to: the
// address's own root when it has one, else this Service's. A note that cannot
// be scoped is not storable, so callers propagate the error.
func (s *Service) noteWorktree(ctx context.Context, addr model.FileAddress) (string, error) {
	if addr.Worktree != "" {
		return filepath.Clean(addr.Worktree), nil
	}
	top, err := s.TopLevel(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Clean(top), nil
}

// diffSideLines projects a diff's aligned rows back into per-side line text,
// indexed so lines[no-1] is line `no`. A side with no numbered row at all is
// ABSENT (nil) — a pure add has no old side, a deleted file no new side — and
// notes anchored there resolve as orphaned.
func diffSideLines(d Diff) (oldLines, newLines []string) {
	maxL, maxR := 0, 0
	for _, r := range d.Result.Rows {
		if r.LeftNo > maxL {
			maxL = r.LeftNo
		}
		if r.RightNo > maxR {
			maxR = r.RightNo
		}
	}
	if maxL > 0 {
		oldLines = make([]string, maxL)
		for _, r := range d.Result.Rows {
			if r.LeftNo > 0 {
				oldLines[r.LeftNo-1] = r.Left
			}
		}
	}
	if maxR > 0 {
		newLines = make([]string, maxR)
		for _, r := range d.Result.Rows {
			if r.RightNo > 0 {
				newLines[r.RightNo-1] = r.Right
			}
		}
	}
	return oldLines, newLines
}

// anchorLines returns the (1-based, inclusive) slice a range names, clamped
// into lines. Empty when the range is entirely outside.
func anchorLines(lines []string, rng [2]int) []string {
	lo, hi := rng[0], rng[1]
	if lo < 1 {
		lo = 1
	}
	if hi > len(lines) {
		hi = len(lines)
	}
	if lo > hi || lo > len(lines) {
		return nil
	}
	return lines[lo-1 : hi]
}

// resolveOne computes one note's status against its side's text (nil = the
// side is absent). The four outcomes of §4.4, in order: found where it was;
// found again elsewhere (scanning outward from the stored start); not found
// but the file is there (stale, clamped); side absent (orphaned).
func resolveOne(n model.Note, lines []string) (model.NoteStatus, [2]int) {
	if lines == nil {
		return model.NoteOrphaned, n.Range
	}
	span := n.Range[1] - n.Range[0] + 1
	if span < 1 {
		span = 1
	}
	if got := anchorLines(lines, n.Range); len(got) == span &&
		model.NoteContextHash(got) == n.ContextHash {
		return model.NoteActive, n.Range
	}
	if start := findAnchor(lines, n.ContextHash, span, n.Range[0]); start > 0 {
		return model.NoteActive, [2]int{start, start + span - 1}
	}
	return model.NoteStale, clampRange(n.Range, len(lines))
}

// findAnchor scans OUTWARD from `from` (1-based) for a window of `span` lines
// whose fingerprint is hash, and returns its 1-based start (0 = not found).
// Outward means the nearest match to where the note used to be wins.
func findAnchor(lines []string, hash string, span, from int) int {
	last := len(lines) - span + 1
	if last < 1 || hash == "" {
		return 0
	}
	if from < 1 {
		from = 1
	}
	for d := 0; ; d++ {
		lo, hi := from-d, from+d
		loOK, hiOK := lo >= 1 && lo <= last, hi >= 1 && hi <= last
		if !loOK && !hiOK && lo < 1 && hi > last {
			return 0
		}
		if loOK && model.NoteContextHash(lines[lo-1:lo-1+span]) == hash {
			return lo
		}
		if d > 0 && hiOK && model.NoteContextHash(lines[hi-1:hi-1+span]) == hash {
			return hi
		}
	}
}

// clampRange pulls a range into a side of n lines (a stale note is still drawn
// somewhere sensible). A zero-line side clamps to {0,0}.
func clampRange(rg [2]int, n int) [2]int {
	if n <= 0 {
		return [2]int{0, 0}
	}
	lo, hi := rg[0], rg[1]
	if lo < 1 {
		lo = 1
	}
	if lo > n {
		lo = n
	}
	if hi < lo {
		hi = lo
	}
	if hi > n {
		hi = n
	}
	return [2]int{lo, hi}
}

// resolveNotes resolves and threads a note set against one diff's two sides.
// Roots sort NEW side first (where review happens), then by resolved start
// line, then by creation time; replies keep creation order under their root
// and inherit the root's status and range.
func resolveNotes(ns []model.Note, oldLines, newLines []string) []ResolvedNote {
	linesFor := func(side model.NoteSide) []string {
		if side == model.NoteSideOld {
			return oldLines
		}
		return newLines
	}
	roots := make([]ResolvedNote, 0, len(ns))
	byID := map[string]int{}
	for _, n := range ns {
		if n.IsReply() {
			continue
		}
		st, rg := resolveOne(n, linesFor(n.Side))
		byID[n.ID] = len(roots)
		roots = append(roots, ResolvedNote{Note: n, Status: st, Range: rg})
	}
	for _, n := range ns {
		if !n.IsReply() {
			continue
		}
		i, ok := byID[n.ParentID]
		if !ok {
			continue // an orphaned reply: the sweep drops it
		}
		roots[i].Replies = append(roots[i].Replies, ResolvedNote{
			Note: n, Status: roots[i].Status, Range: roots[i].Range,
		})
	}
	for i := range roots {
		sort.SliceStable(roots[i].Replies, func(a, b int) bool {
			return roots[i].Replies[a].Note.Created.Before(roots[i].Replies[b].Note.Created)
		})
	}
	sort.SliceStable(roots, func(a, b int) bool {
		if roots[a].Note.Side != roots[b].Note.Side {
			return roots[a].Note.Side == model.NoteSideNew
		}
		if roots[a].Range[0] != roots[b].Range[0] {
			return roots[a].Range[0] < roots[b].Range[0]
		}
		return roots[a].Note.Created.Before(roots[b].Note.Created)
	})
	return roots
}

// noteSideLines reads one side's text for an address. The OLD side is the base
// the diff that created the note compared against (§4.4 "Old-side base"),
// which in this codebase is:
//
//	StateUnstaged  old = index blob      new = working file   (Files panel)
//	StateStaged    old = HEAD blob       new = index blob     (Staged panel)
//	StateUntracked old = absent          new = working file
//	StateCommitted old = <sha>^:path     new = <sha>:path
//	StateShelf     old = absent          new = the shelf entry's bytes
//
// A nil result (with a nil error) means the side is legitimately absent; an
// error means the target could not be read at all — the caller treats both as
// "gone".
//
// The live rows read the NOTE'S worktree, not this Service's: the store is
// shared by every worktree of the repo, so a sweep running in worktree B must
// still read worktree A's index and working file for A's notes. A worktree
// that no longer exists makes the read fail, i.e. the note is orphaned. This
// is the BookmarkBytes routing (`git -C <wt> show` for tracked content, a
// direct file read for the working copy).
func (s *Service) noteSideLines(ctx context.Context, addr model.FileAddress, side model.NoteSide) ([]string, error) {
	old := side == model.NoteSideOld
	switch addr.State {
	case model.StateCommitted:
		rev := addr.Commit
		if old {
			rev += "^"
		}
		b, err := s.ShowFile(ctx, rev, addr.Path)
		if err != nil {
			// A ROOT commit has no first parent, so its old side is
			// legitimately EMPTY rather than unreadable — without this the
			// note's fingerprint never fills and it is stale forever. A parent
			// that exists but lacks the path (a file this commit added) stays
			// an error: that side really is absent.
			if old {
				if _, found, lerr := s.CommitLookup(ctx, addr.Commit+"^"); lerr == nil && !found {
					return []string{}, nil
				}
			}
			return nil, err
		}
		return splitLines(b), nil
	case model.StateShelf:
		if old {
			return nil, nil
		}
		b, err := s.ResolveBytes(ctx, addr.FileRef())
		if err != nil {
			return nil, err
		}
		return splitLines(b), nil
	}

	wt, err := s.noteWorktree(ctx, addr)
	if err != nil {
		return nil, err
	}
	switch addr.State {
	case model.StateStaged:
		rev := "" // "" = the index blob (git -C <wt> show :path)
		if old {
			rev = "HEAD"
		}
		b, serr := s.showFileIn(ctx, wt, rev, addr.Path)
		if serr != nil {
			return nil, serr
		}
		return splitLines(b), nil
	case model.StateUntracked:
		if old {
			return nil, nil
		}
		b, ferr := s.worktreeFileIn(ctx, wt, addr.Path)
		if ferr != nil {
			return nil, ferr
		}
		return splitLines(b), nil
	default: // StateUnstaged
		if old {
			b, serr := s.showFileIn(ctx, wt, "", addr.Path)
			if serr != nil {
				return nil, serr
			}
			return splitLines(b), nil
		}
		b, ferr := s.worktreeFileIn(ctx, wt, addr.Path)
		if ferr != nil {
			return nil, ferr
		}
		return splitLines(b), nil
	}
}

// showFileIn reads path at rev inside worktree wt (`git -C <wt> show
// <rev>:<path>`), under a Read reservation — the BookmarkBytes precedent for
// reaching a sibling worktree's index or HEAD.
func (s *Service) showFileIn(ctx context.Context, wt, rev, path string) ([]byte, error) {
	return query(ctx, s, "note-showindir:"+wt+":"+rev+":"+path, func(ctx context.Context) ([]byte, error) {
		return s.repo.ShowFileInDir(ctx, wt, rev, path)
	})
}

// worktreeFileIn reads the working copy of path inside worktree wt. Path is in
// git slash form and is converted before it touches the filesystem.
func (s *Service) worktreeFileIn(ctx context.Context, wt, path string) ([]byte, error) {
	full, err := worktreeJoin(wt, path)
	if err != nil {
		return nil, err
	}
	return query(ctx, s, "note-file:"+wt+":"+path, func(ctx context.Context) ([]byte, error) {
		return os.ReadFile(full)
	})
}

// worktreeJoin resolves a git-slash, repo-relative path inside a checkout
// root, rejecting one that climbs out of it. internal/git applies this guard
// to its own worktree I/O; domain reaches SIBLING worktrees directly (a note
// or a bookmark names its own checkout), so it needs the same check here — a
// stored record is data, and data must not be able to address /etc/shadow.
func worktreeJoin(root, path string) (string, error) {
	full := filepath.Join(root, filepath.FromSlash(path))
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the working tree", path)
	}
	return full, nil
}

// splitLines is the shared byte→line projection for side text read from git
// (the sweep and NoteAdd's hash fill). A trailing newline does not create a
// phantom last line.
func splitLines(b []byte) []string {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\n")
}

// isFullSHA reports whether s is a 40-character lowercase hex object id — the
// only form worth storing as a note's commit target.
func isFullSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// NoteGet returns one stored note by id, replies included. It is the batch
// importer's parent check: `note apply` validates every replyTo BEFORE the
// first write, so a bad batch stores nothing.
func (s *Service) NoteGet(ctx context.Context, id string) (model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return model.Note{}, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return model.Note{}, err
	}
	for _, n := range all {
		if n.ID == id {
			return n, nil
		}
	}
	return model.Note{}, ErrNoteNotFound
}

// NoteAddresses lists the distinct addresses that carry at least one ROOT note
// and are visible from this checkout: every commit and shelf note (those are
// worktree-agnostic) plus the worktree-state notes taken in THIS checkout. It
// is the enumeration door for `gg note list` and `gg note clear --all`, which
// have no path to resolve.
//
// Addresses are deduplicated by the identity sameNoteTarget uses — worktree,
// commit, shelf id and path, NOT State — so a path with both a staged and an
// unstaged note yields ONE address (whose State is the first note's, i.e. the
// side pair NotesAt will read). Sorted by path, then commit, for stable output.
func (s *Service) NoteAddresses(ctx context.Context) ([]model.FileAddress, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return nil, err
	}
	cur, _ := s.TopLevel(ctx) // "" simply hides worktree notes, never fails the query
	seen := map[string]bool{}
	var out []model.FileAddress
	for _, n := range all {
		if n.IsReply() {
			continue
		}
		a := n.Address
		if worktreeScopedNote(a) && !sameWorktreePath(a.Worktree, cur) {
			continue
		}
		key := a.Worktree + "\x00" + a.Commit + "\x00" + a.ShelfID + "\x00" + a.Path
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Commit < out[j].Commit
	})
	return out, nil
}
