package notes

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/homeend/gigagit/internal/model"
)

// ErrPartChange refuses a Put that would move a stored note to another part:
// an address is fixed at creation, so this is a caller bug, never a move.
var ErrPartChange = errors.New("notes: a note cannot move to another part")

// FileStore is the note store: one partFile per Part under root
// (commits.toml, previews.toml, shelf.toml, worktrees/<key>.toml). Every
// mutation locks only the file it changes; reads name the parts they need.
type FileStore struct {
	root string

	mu    sync.Mutex // guards parts and pol
	parts map[Part]*partFile
	pol   Policy
}

// NewFileStore roots a store at the per-repo directory (caller-supplied).
func NewFileStore(root string) *FileStore {
	return &FileStore{root: root, parts: map[Part]*partFile{}}
}

// SetPolicy sets the write-time entry cap of EVERY part (spec §4).
func (fs *FileStore) SetPolicy(p Policy) {
	fs.mu.Lock()
	fs.pol = p
	files := make([]*partFile, 0, len(fs.parts))
	for _, f := range fs.parts {
		files = append(files, f)
	}
	fs.mu.Unlock()
	for _, f := range files {
		f.SetPolicy(p)
	}
}

// file returns (creating once) the partFile behind p.
func (fs *FileStore) file(p Part) *partFile {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	f, ok := fs.parts[p]
	if !ok {
		f = &partFile{path: p.file(fs.root), pol: fs.pol}
		fs.parts[p] = f
	}
	return f
}

// Parts lists the parts that have a file, fixed parts first, then the
// worktrees sorted. A missing root or worktrees/ dir is simply empty.
func (fs *FileStore) Parts() ([]Part, error) {
	var out []Part
	for _, p := range []Part{PartCommits, PartPreviews, PartShelf} {
		if _, err := os.Stat(p.file(fs.root)); err == nil {
			out = append(out, p)
		}
	}
	ents, err := os.ReadDir(filepath.Join(fs.root, "worktrees"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var wts []Part
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		if p, ok := partOfFile(e.Name()); ok {
			wts = append(wts, p)
		}
	}
	sort.Slice(wts, func(a, b int) bool { return wts[a] < wts[b] })
	return append(out, wts...), nil
}

// Load returns one part's notes. NEVER writes.
func (fs *FileStore) Load(p Part) ([]model.Note, error) { return fs.file(p).Load() }

// LoadAll returns every part's notes. A corrupt part does not hide the
// others' notes, but its error is returned (joined), so a caller that fails
// on a corrupt store keeps failing exactly as before the split.
func (fs *FileStore) LoadAll() ([]model.Note, error) {
	parts, err := fs.Parts()
	if err != nil {
		return nil, err
	}
	var all []model.Note
	var errs []error
	for _, p := range parts {
		ns, lerr := fs.Load(p)
		if lerr != nil {
			errs = append(errs, lerr)
			continue
		}
		all = append(all, ns...)
	}
	return all, errors.Join(errs...)
}

// where maps each stored id to its part, skipping unreadable parts (their
// own operations fail on their own); unread is the first such read error.
func (fs *FileStore) where() (at map[string]Part, unread error, err error) {
	parts, err := fs.Parts()
	if err != nil {
		return nil, nil, err
	}
	at = map[string]Part{}
	for _, p := range parts {
		ns, lerr := fs.Load(p)
		if lerr != nil {
			if unread == nil {
				unread = lerr
			}
			continue
		}
		for _, n := range ns {
			at[n.ID] = p
		}
	}
	return at, unread, nil
}

// Put adds n, or replaces the record with the same ID, in n's part. A reply
// goes to its root's part (a reply carries no Preview of its own); a reply
// whose root is gone falls back to PartOf and the orphan prune drops it.
func (fs *FileStore) Put(n model.Note) error {
	at, unread, err := fs.where()
	if err != nil {
		return err
	}
	want := PartOf(n)
	if n.IsReply() {
		p, ok := at[n.ParentID]
		switch {
		case ok:
			want = p
		case unread != nil:
			// The root may sit in the part that could not be read: guessing
			// would file the reply where the orphan prune silently drops it.
			return unread
		}
	}
	if p, ok := at[n.ID]; ok && p != want {
		return fmt.Errorf("%w: %s is in %s, not %s", ErrPartChange, n.ID, p, want)
	}
	return fs.file(want).Put(n)
}

// Remove deletes one note wherever it is stored; a root takes its replies
// (they share its part). A corrupt part is skipped unless nothing else held
// the id, in which case its error is reported instead of ErrNotFound.
func (fs *FileStore) Remove(id string) error {
	parts, err := fs.Parts()
	if err != nil {
		return err
	}
	var firstErr error
	for _, p := range parts {
		rerr := fs.file(p).Remove(id)
		switch {
		case rerr == nil:
			return nil
		case errors.Is(rerr, ErrNotFound):
		default:
			if firstErr == nil {
				firstErr = rerr
			}
		}
	}
	if firstErr != nil {
		return firstErr
	}
	return ErrNotFound
}

// Sweep applies keep to every part, each under its own lock, and reports
// the total shrinkage. It stops at the first failing part.
func (fs *FileStore) Sweep(keep func(model.Note) bool) (int, error) {
	parts, err := fs.Parts()
	if err != nil {
		return 0, err
	}
	total := 0
	for _, p := range parts {
		n, serr := fs.file(p).Sweep(keep)
		total += n
		if serr != nil {
			return total, serr
		}
	}
	return total, nil
}

// Quarantine moves every CORRUPT part aside (<file>.corrupt-<unix>) and
// reports where they went, ", "-joined ("" when none was corrupt). Healthy
// parts are never touched.
func (fs *FileStore) Quarantine() (string, error) {
	parts, err := fs.Parts()
	if err != nil {
		return "", err
	}
	var moved []string
	for _, p := range parts {
		if _, lerr := fs.Load(p); !errors.Is(lerr, ErrCorrupt) {
			continue
		}
		dst, qerr := fs.file(p).Quarantine()
		if qerr != nil {
			return strings.Join(moved, ", "), qerr
		}
		if dst != "" {
			moved = append(moved, dst)
		}
	}
	return strings.Join(moved, ", "), nil
}
