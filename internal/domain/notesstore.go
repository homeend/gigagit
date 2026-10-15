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
	s.notesKey = ""
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
//
// The common dir is read straight from the repo, never through query(): the
// first use may come from inside an op holding a reservation (the send op's
// settle pass), and the gate is not re-entrant.
func (s *Service) notesStore(ctx context.Context) notes.Store {
	return s.notesStoreKeyed(func() (string, error) { return s.repo.GitCommonDir(ctx) })
}

// notesStoreKeyed is notesStore with the common-dir read supplied (see
// shelfStoreKeyed).
func (s *Service) notesStoreKeyed(commonDir func() (string, error)) notes.Store {
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

	root, key, ok := notesRoot(commonDir)
	if !ok {
		return nil
	}
	fs := openNotesStore(root)
	s.mu.Lock()
	fresh := s.notes == nil
	if fresh {
		s.notes = fs
		s.notesKey = key
	}
	st = s.notes
	max := notesEffective(s.notesMaxEntries, notesDefaults.MaxEntries)
	s.mu.Unlock()
	if fresh {
		st.SetPolicy(notes.Policy{MaxEntries: max})
	}
	return st
}

// notesRoot is the store directory gg resolves for this repository and its
// key (spec 2026-10-15-gg-notes-store-format §3): NotesStatePath when set
// (no key), else <state>/gg/notes/<repoKey(common dir)>. ok is false when
// no state dir is resolvable.
func notesRoot(commonDir func() (string, error)) (root, key string, ok bool) {
	if NotesStatePath != "" {
		return NotesStatePath, "", true
	}
	base := stateBaseDir("notes")
	if base == "" {
		return "", "", false
	}
	key = "unknown"
	if cd, err := commonDir(); err == nil {
		key = repoKey(strings.TrimSpace(cd)) // reuse shelfstore.go's repoKey
	}
	return filepath.Join(base, key), key, true
}

// NotesStoreInfo is where this repository's notes live — what `gg overview
// store` prints so a reader of the store format can check its own key
// derivation (spec 2026-10-15-gg-notes-store-format §9.3).
type NotesStoreInfo struct {
	Dir string // the store directory gg reads (holds commits.toml); "" = notes disabled
	Key string // the directory key (spec §3.2); "" when the dir was injected or overridden
}

// NotesStorePath resolves the store (as any read would) and reports it.
func (s *Service) NotesStorePath(ctx context.Context) NotesStoreInfo {
	st := s.notesStore(ctx)
	if st == nil {
		return NotesStoreInfo{}
	}
	fs, ok := st.(*notes.FileStore)
	if !ok {
		return NotesStoreInfo{}
	}
	s.mu.Lock()
	key := s.notesKey
	s.mu.Unlock()
	return NotesStoreInfo{Dir: fs.Root(), Key: key}
}

// openNotesStore opens the store rooted at root and converts a legacy
// notes.toml first. RunAutoMigrations covers the composition roots, but a
// TUI repo switch, a hosted-web switch and `gg mcp` open a repo without it;
// converting at resolution keeps their notes visible. Idempotent: the
// conversion is presence-based and merges by id. A failure (a corrupt file,
// now quarantined; a lock held past its budget) is left for the preflight
// path to report — the store still opens on whatever parts exist.
func openNotesStore(root string) *notes.FileStore {
	fs := notes.NewFileStore(root)
	if fs.LegacyPresent() {
		_, _ = fs.ConvertLegacy()
	}
	return fs
}
