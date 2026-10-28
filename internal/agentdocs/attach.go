package agentdocs

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/notemeta"
	"github.com/homeend/gigagit/internal/steer"
)

// Attached roots (spec 2026-10-18-gg-session-docs §4.2, §6.1): for a root
// bound to its docs directory the store is a CACHE of that directory. One
// goroutine per root wakes on the watch or the poll and reconciles by id;
// every mutation writes a file, then reloads. A root that is not attached
// keeps the memory-only behaviour (its ids t<n>/f<n>, its caps).

// pollEvery is the watch's fallback tick; a test shortens it.
var pollEvery = 2 * time.Second

// attachedRoot is one root bound to its docs directory.
type attachedRoot struct {
	dir, worktree string
	w             *steer.Watcher // nil when the watch could not start
	stop          chan struct{}
	once          sync.Once
	loadMu        sync.Mutex // one Reload at a time: a stale listing must not undo a newer one
	problems      []Problem
	seen          map[string]docStamp // kind/id → what was loaded (or why it was not)
}

type docStamp struct {
	id, kind string
	size     int64
	modTime  time.Time
	problem  string // "" = loaded; else the reason the entry is not shown
}

func stampKey(kind, id string) string { return kind + "/" + id }

func (r *attachedRoot) close() {
	r.once.Do(func() {
		close(r.stop)
		if r.w != nil {
			r.w.Close()
		}
	})
}

// Attach binds root (the store's worktree key) to its docs directory: loads
// it, watches it, and from now on every mutation on root writes a file and
// reloads. worktreeDir is where file anchors are stat'ed. Attaching an
// attached root with the same dir is a no-op; with another dir it detaches
// first.
func (s *Store) Attach(root, worktreeDir, docsDir string) error {
	s.mu.Lock()
	if r := s.roots[root]; r != nil {
		if r.dir == docsDir {
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()
		s.Detach(root)
		s.mu.Lock()
	}
	r := &attachedRoot{dir: docsDir, worktree: worktreeDir, stop: make(chan struct{}), seen: map[string]docStamp{}}
	// gg's readers are writers too (tours, the CLI's verbs), so the directory
	// is made here; an empty one costs nothing and the watch needs it.
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		s.mu.Unlock()
		return err
	}
	if !s.noWatch {
		if w, err := steer.WatchFilter(docsDir, func(name string) bool { _, _, ok := DocName(name); return ok }); err == nil {
			r.w = w
		}
	}
	s.roots[root] = r
	// The root's memory documents have no file and could never be deleted
	// once the directory is the source of truth: they go (review finding D).
	s.dropRootLocked(root)
	s.mu.Unlock()
	s.Reload(root)
	go s.follow(root, r)
	return nil
}

// dropRootLocked forgets every document of root (s.mu held).
func (s *Store) dropRootLocked(root string) {
	for k := range s.overviews {
		if k.root == root {
			delete(s.overviews, k)
		}
	}
	for k := range s.files {
		if k.root == root {
			delete(s.files, k)
		}
	}
	for key, id := range s.tours {
		if s.ovLocked(id) == nil {
			delete(s.tours, key)
		}
	}
}

func (s *Store) follow(root string, r *attachedRoot) {
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	var wake <-chan struct{}
	if r.w != nil {
		wake = r.w.Events()
	}
	for {
		select {
		case <-r.stop:
			return
		case <-wake:
			s.Reload(root)
		case <-t.C:
			s.Reload(root)
		}
	}
}

// Detach stops watching root and drops its documents from memory.
func (s *Store) Detach(root string) {
	s.mu.Lock()
	r := s.roots[root]
	delete(s.roots, root)
	if r == nil {
		s.mu.Unlock()
		return
	}
	r.close()
	s.dropRootLocked(root)
	s.mu.Unlock()
	s.b.signal()
}

// Attached reports root's docs directory ("" = memory only).
func (s *Store) Attached(root string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.roots[root]; r != nil {
		return r.dir
	}
	return ""
}

// AttachedWorktree is the worktree on disk root was attached for ("" =
// memory only): where a link to one of its documents points.
func (s *Store) AttachedWorktree(root string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.roots[root]; r != nil {
		return r.worktree
	}
	return ""
}

// Problems is what the last reload of root skipped.
func (s *Store) Problems(root string) []Problem {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.roots[root]; r != nil {
		return append([]Problem(nil), r.problems...)
	}
	return nil
}

// Close detaches every root.
func (s *Store) Close() {
	s.mu.Lock()
	var roots []string
	for root := range s.roots {
		roots = append(roots, root)
	}
	s.mu.Unlock()
	for _, root := range roots {
		s.Detach(root)
	}
}

// rootOf is root's attachment, or nil.
func (s *Store) rootOf(root string) *attachedRoot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.roots[root]
}

// Reload re-reads root's directory now and reconciles by id: new → added,
// changed (size or mtime) → replaced in place, gone → dropped. Only an entry
// whose stamp changed is read and parsed. A directory that cannot be listed
// keeps what is shown and records the error. One Reload per root runs at a
// time, so a listing taken before a write cannot undo the write's own reload.
func (s *Store) Reload(root string) {
	r := s.rootOf(root)
	if r == nil {
		return
	}
	r.loadMu.Lock()
	defer r.loadMu.Unlock()
	entries, err := os.ReadDir(r.dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.mu.Lock()
		if s.roots[root] == r {
			r.problems = []Problem{{Name: ".", Reason: "unreadable: " + err.Error()}}
		}
		s.mu.Unlock()
		return
	}
	// Stat first (under no lock); read only what changed.
	type found struct {
		st  docStamp
		doc *Doc
	}
	var probs []Problem
	present := map[string]found{}
	s.mu.Lock()
	seen := make(map[string]docStamp, len(r.seen))
	for k, v := range r.seen {
		seen[k] = v
	}
	s.mu.Unlock()
	for _, e := range entries {
		name := e.Name()
		id, kind, ok := DocName(name)
		if !ok {
			if !strings.HasPrefix(name, ".") { // a writer's own temp file is not worth a word
				probs = append(probs, Problem{name, "not a document name"})
			}
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			probs = append(probs, Problem{name, "unreadable: " + ierr.Error()})
			continue
		}
		key := stampKey(kind, id)
		st := docStamp{id: id, kind: kind, size: info.Size(), modTime: info.ModTime()}
		if old, ok := seen[key]; ok && old.id == st.id && old.size == st.size && old.modTime.Equal(st.modTime) {
			present[key] = found{st: old}
			if old.problem != "" {
				probs = append(probs, Problem{name, old.problem})
			}
			continue
		}
		d, rerr := readEntry(r.dir, id, kind)
		if rerr != nil {
			st.problem = rerr.Error()
			probs = append(probs, Problem{name, st.problem})
			present[key] = found{st: st}
			continue
		}
		present[key] = found{st: st, doc: &d}
	}
	s.mu.Lock()
	if s.roots[root] != r {
		s.mu.Unlock()
		return // detached meanwhile
	}
	changed := false
	for key, f := range present {
		old, had := r.seen[key]
		r.seen[key] = f.st
		switch {
		case f.doc != nil:
			if s.loadDocLocked(root, r, *f.doc) {
				changed = true
			}
		case f.st.problem != "" && had && old.problem == "":
			// It was shown and its file went bad: no longer shown.
			if s.dropDocLocked(root, f.st.id, f.st.kind) {
				changed = true
			}
		}
	}
	for key, st := range r.seen {
		if _, ok := present[key]; ok {
			continue
		}
		delete(r.seen, key)
		if s.dropDocLocked(root, st.id, st.kind) {
			changed = true
		}
	}
	r.problems = probs
	s.mu.Unlock()
	if changed {
		s.b.signal()
	}
}

// seqOf orders documents: created, else the file's mtime.
func seqOf(d Doc) int64 {
	switch {
	case d.Over != nil && !d.Over.Created.IsZero():
		return d.Over.Created.UnixNano()
	case d.Note != nil && !d.Note.Created.IsZero():
		return d.Note.Created.UnixNano()
	}
	return d.ModTime.UnixNano()
}

// loadDocLocked puts a listed document into the store (s.mu held).
func (s *Store) loadDocLocked(root string, r *attachedRoot, d Doc) bool {
	if d.Over != nil {
		_, anchors := ParseOverview(d.Over.Text)
		if e := s.overviews[ovKey{root, d.ID}]; e != nil {
			was := map[string]bool{}
			for _, a := range e.Anchors {
				was[a.Dest] = a.Missing
			}
			for i := range anchors {
				anchors[i].Missing = was[anchors[i].Dest]
			}
			e.Title, e.Text, e.Anchors, e.Seq, e.Author, e.Created = d.Over.Title, d.Over.Text, anchors, seqOf(d), d.Over.Author, d.Over.Created
			return true
		}
		s.overviews[ovKey{root, d.ID}] = &ovEntry{Overview: Overview{ID: d.ID, Seq: seqOf(d), Root: root, Title: d.Over.Title, Text: d.Over.Text,
			Anchors: anchors, Author: d.Over.Author, Created: d.Over.Created}, dir: r.worktree}
		return true
	}
	nf := d.Note
	k := fileKey{root, nf.Path}
	f := s.files[k]
	if of, on := s.findInLocked(root, d.ID); on != nil {
		same := of == f && on.wStart == nf.Start && on.wEnd == nf.End && strings.Join(on.anchor, "\n") == strings.Join(nf.Lines, "\n")
		if same { // the text changed, the place did not: keep the placement
			on.Summary, on.Rationale, on.Author, on.Created, on.Meta, on.Display, on.Seq = nf.Summary, nf.Rationale, nf.Author, nf.Created, nf.Meta, nf.Display, seqOf(d)
			return true
		}
		s.removeNoteLocked(of, on)
		f = s.files[k] // the remove may have dropped an emptied entry
	}
	if f == nil {
		f = &fileNotes{}
		s.files[k] = f
	}
	n := &note{Note: Note{ID: d.ID, Seq: seqOf(d), Root: root, Path: nf.Path, Start: nf.Start, End: nf.End,
		Summary: nf.Summary, Rationale: nf.Rationale, Author: nf.Author, Created: nf.Created, Meta: nf.Meta, Display: nf.Display},
		anchor: append([]string(nil), nf.Lines...), wStart: nf.Start, wEnd: nf.End, cold: true}
	if len(f.lines) > 0 {
		n.Start, n.End, n.Outdated = ColdPlace(f.lines, nf.Start, nf.End, n.anchor)
		n.cold = false
	}
	f.notes = append(f.notes, n)
	f.sort()
	return true
}

// dropDocLocked removes a document whose file is gone (s.mu held).
func (s *Store) dropDocLocked(root, id, kind string) bool {
	if kind == KindOverview {
		if _, ok := s.overviews[ovKey{root, id}]; ok {
			delete(s.overviews, ovKey{root, id})
			return true
		}
		return false
	}
	f, n := s.findInLocked(root, id)
	if n == nil {
		return false
	}
	s.removeNoteLocked(f, n)
	return true
}

// removeNoteLocked takes n out of f, dropping an emptied f (s.mu held).
func (s *Store) removeNoteLocked(f *fileNotes, n *note) {
	for i, x := range f.notes {
		if x == n {
			f.notes = append(f.notes[:i], f.notes[i+1:]...)
			break
		}
	}
	if len(f.notes) == 0 {
		for k, v := range s.files {
			if v == f {
				delete(s.files, k)
			}
		}
	}
}

// writeNew lands a new document under a minted id, trying another id when
// the name is taken.
func writeNew(dir, kind string, data []byte) (string, error) {
	var err error
	for i := 0; i < 3; i++ {
		id := MintID(clock.Now(), nil)
		if err = WriteDoc(dir, id, kind, data, false); err == nil {
			return id, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", err
}

// addNoteFile is AddNoteMeta on an attached root: the note as a file.
func (s *Store) addNoteFile(r *attachedRoot, root, path string, lines []string, start, end int, t NoteText) (Note, error) {
	author := t.Author
	if author == "" {
		author = "agent"
	}
	meta, _ := notemeta.CheckMeta(t.Meta)
	data, err := EncodeNoteFile(NoteFile{Path: path, Start: start, End: end, Summary: t.Summary, Rationale: t.Rationale,
		Author: author, Created: clock.Now(), Lines: lines[start-1 : end], Meta: meta, Display: t.Display})
	if err != nil {
		return Note{}, err
	}
	id, err := writeNew(r.dir, KindNote, data)
	if err != nil {
		return Note{}, err
	}
	s.Reload(root)
	n, _ := s.FindNoteIn(root, id)
	return n, nil
}

// addOverviewFile is AddOverview on an attached root.
func (s *Store) addOverviewFile(r *attachedRoot, root, title, text string) (Overview, error) {
	data, err := EncodeOverviewFile(OverviewFile{Title: title, Author: "agent", Created: clock.Now(), Text: text})
	if err != nil {
		return Overview{}, err
	}
	id, err := writeNew(r.dir, KindOverview, data)
	if err != nil {
		return Overview{}, err
	}
	s.Reload(root)
	o, _ := s.OverviewIn(root, id)
	return o, nil
}

// setOverviewFile rewrites an attached root's overview in place, keeping
// its author and creation time (and its title when title is blank).
func (s *Store) setOverviewFile(r *attachedRoot, root, id, title, text string) (Overview, error) {
	cur, err := ReadDoc(r.dir, id, KindOverview)
	if err != nil {
		return Overview{}, errors.New("no overview " + id)
	}
	if strings.TrimSpace(title) == "" {
		title = cur.Over.Title
	}
	data, err := EncodeOverviewFile(OverviewFile{Title: title, Author: cur.Over.Author, Created: cur.Over.Created, Text: text})
	if err != nil {
		return Overview{}, err
	}
	if err := WriteDoc(r.dir, id, KindOverview, data, true); err != nil {
		return Overview{}, err
	}
	s.Reload(root)
	o, ok := s.OverviewIn(root, id)
	if !ok {
		return Overview{}, errors.New("no overview " + id)
	}
	return o, nil
}

// tourID is the fixed file name of a keyed tour: "tour-" + the key with
// every character outside [A-Za-z0-9_-] replaced by "-", at most 64 bytes.
func tourID(key string) string {
	var b strings.Builder
	b.WriteString("tour-")
	for _, c := range key {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
			b.WriteRune(c)
		default:
			b.WriteByte('-')
		}
	}
	id := b.String()
	if len(id) > 64 {
		id = id[:64]
	}
	return id
}

// fileTourFile is FileTour on an attached root: a fixed name, replaced in
// place; added says whether the file was not there before.
func (s *Store) fileTourFile(r *attachedRoot, root, key, title, text string) (Overview, bool, error) {
	id := tourID(key)
	created, author := clock.Now(), "gg"
	added := true
	if cur, err := ReadDoc(r.dir, id, KindOverview); err == nil {
		created, author, added = cur.Over.Created, cur.Over.Author, false
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Overview{}, false, err
	}
	data, err := EncodeOverviewFile(OverviewFile{Title: title, Author: author, Created: created, Text: text})
	if err != nil {
		return Overview{}, false, err
	}
	if err := WriteDoc(r.dir, id, KindOverview, data, true); err != nil {
		return Overview{}, false, err
	}
	s.Reload(root)
	o, _ := s.OverviewIn(root, id)
	return o, added, nil
}

// removeDocFile unlinks a document of an attached root and reloads.
func (s *Store) removeDocFile(r *attachedRoot, root, id, kind string) bool {
	ok, _ := RemoveDoc(r.dir, id, kind)
	if _, err := os.Lstat(r.dir); err == nil || ok {
		s.Reload(root)
	}
	return ok
}
