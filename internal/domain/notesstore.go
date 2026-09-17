package domain

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/notes"
)

// NotesStatePath overrides the notes root dir. "" uses the default XDG
// location. cmd/gg leaves it ""; tests point it at a temp dir.
var NotesStatePath string

// NotesDisabled turns the notes surface off process-wide: lazy resolution
// yields no store, so the startup sweep is a silent no-op and NotesFor /
// NoteCounts report ErrNotesDisabled.
//
// It is the TEST seam for packages that cannot import internal/notes
// (internal/tui and internal/web set it in TestMain). Without it every test in
// those suites that drives loadCmd/applyUIPolicies would share ONE notes.toml
// — and the first test to write a note would make every later parallel sweep
// read it and probe that test's Runner off-thread. Set it before m.Run(); it
// is a plain package var, never written once tests are running.
//
// An INJECTED store still wins: a test that actually exercises notes calls
// UseNotesDir on its own Service.
var NotesDisabled bool

// UseNotesDir points one Service at its own note store under dir — the
// per-test companion to NotesDisabled, for a test in a package that cannot
// import internal/notes:
//
//	svc.UseNotesDir(t.TempDir())
//
// It overrides NotesDisabled for that Service only, so a suite can keep notes
// globally off and still cover them where it means to.
func (s *Service) UseNotesDir(dir string) { s.SetNotesStore(notes.NewFileStore(dir)) }

// SetNotesStore injects a store (tests). A nil store re-arms lazy resolution.
// The effective policy is pushed here, once, for the same reason notesStore
// pushes it at resolution time: an injected store would otherwise run uncapped
// until a frontend happened to call SetNotesPolicy.
func (s *Service) SetNotesStore(st notes.Store) {
	s.mu.Lock()
	s.notes = st
	s.noteCounts = nil
	s.previewCounts = nil // same store, same invalidation
	s.notesGen++
	max := notesEffective(s.notesMaxEntries, notesDefaults.MaxEntries)
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
		return st // an injected store (UseNotesDir) outranks NotesDisabled
	}
	if NotesDisabled {
		return nil
	}

	root := NotesStatePath
	if root == "" {
		base := stateBaseDir("notes")
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
	max := notesEffective(s.notesMaxEntries, notesDefaults.MaxEntries)
	s.mu.Unlock()
	if fresh {
		st.SetPolicy(notes.Policy{MaxEntries: max})
	}
	return st
}
