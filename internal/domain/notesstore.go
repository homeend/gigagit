package domain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/homeend/gigagit/internal/notes"
)

// NotesStatePath overrides the notes root dir. "" uses the default XDG
// location. cmd/gg leaves it ""; tests point it at a temp dir.
var NotesStatePath string

// SetNotesStore injects a store (tests). A nil store re-arms lazy resolution.
// The effective policy is pushed here, once, for the same reason notesStore
// pushes it at resolution time: an injected store would otherwise run uncapped
// until a frontend happened to call SetNotesPolicy.
func (s *Service) SetNotesStore(st notes.Store) {
	s.mu.Lock()
	s.notes = st
	s.noteCounts = nil
	s.notesGen++
	max := notesEffective(s.notesMaxEntries, notesDefaultMaxEntries)
	s.mu.Unlock()
	if st != nil {
		st.SetPolicy(notes.Policy{MaxEntries: max})
	}
}

// disableNotesForTest forces the "no state directory" branch so the disabled
// path can be exercised without touching the caller's environment.
func (s *Service) disableNotesForTest() {
	s.mu.Lock()
	s.notesOff = true
	s.mu.Unlock()
}

// notesStore resolves (once) the per-repo note store, keyed by git common dir
// under the XDG state dir — the bookmarkStore shape. Returns nil (notes
// disabled) when no state dir is resolvable.
//
// The write-time policy is pushed exactly where the store is installed — here
// on first resolution, in SetNotesStore, and in SetNotesPolicy — never per
// call: FileStore.SetPolicy takes the same mutex its writer holds across the
// lock spin, so pushing it on every read would park every reader behind a
// contended write.
func (s *Service) notesStore(ctx context.Context) notes.Store {
	s.mu.Lock()
	if s.notesOff {
		s.mu.Unlock()
		return nil
	}
	st := s.notes
	s.mu.Unlock()
	if st != nil {
		return st
	}

	root := NotesStatePath
	if root == "" {
		base := notesBaseDir()
		if base == "" {
			return nil
		}
		key := "unknown"
		if cd, err := s.GitCommonDir(ctx); err == nil {
			key = repoKey(strings.TrimSpace(cd)) // reuse shelfstore.go's repoKey
		}
		root = filepath.Join(base, key)
	}
	fs := notes.NewFileStore(root)
	s.mu.Lock()
	fresh := s.notes == nil
	if fresh {
		s.notes = fs
	}
	st = s.notes
	max := notesEffective(s.notesMaxEntries, notesDefaultMaxEntries)
	s.mu.Unlock()
	if fresh {
		st.SetPolicy(notes.Policy{MaxEntries: max})
	}
	return st
}

// notesBaseDir resolves <state>/gg/notes cross-platform (mirrors
// bookmarkBaseDir). "" when no home/state dir exists.
func notesBaseDir() string {
	// An explicitly-set $XDG_STATE_HOME wins on every platform (it is a
	// deliberate override — and the only way tests can isolate state on
	// Windows); %LocalAppData% is the ambient Windows default.
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "notes")
	}
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "notes")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "notes")
}
