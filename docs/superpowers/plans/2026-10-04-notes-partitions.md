# Note Store Partitions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo forbids implementer subagents — CLAUDE.md "Workflow"). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the per-repo `notes.toml` into one file per anchor kind (`commits`, `previews`, `shelf`, one per worktree) behind the unchanged notes engine, and migrate existing stores losslessly.

**Architecture:** `internal/notes` gains a pure router (`PartOf`/`WorktreePart`), the old single-file store becomes the unexported `partFile`, and the exported `FileStore` composes one `partFile` per part behind a `Store` interface whose `Load()` becomes `Load(part)` + `LoadAll()`. `internal/domain` names the parts each reader needs, the sweep drops notes of deleted worktrees, and a lossless preflight migration (`split-notes`) converts the legacy file.

**Tech Stack:** Go 1.26, `github.com/pelletier/go-toml/v2`, `internal/filelock`, real `git` in tests (`gittest.Run`).

**Spec:** `docs/superpowers/specs/2026-10-04-notes-partitions-design.md`

## Global Constraints

- Worktree: `/work/gigagit/.claude/worktrees/notes-partitions`, branch `feat/notes-partitions`. Every command runs there (`cd` it or `git -C`); the shell cwd resets to the main checkout between calls.
- Commits: `gg add <paths>` then `git -C <worktree> commit -F <msgfile>` (gg commit has no `-F`). Never `git add -A`. Every message ends with the two attribution lines:
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01AMREn9YaHAb1mRQm9uPcor`.
- Part file names, verbatim: `commits.toml`, `previews.toml`, `shelf.toml`, `worktrees/<key>.toml`, `worktrees/unscoped.toml`. `<key>` = hex of the first 8 bytes of sha256 over `filepath.Clean(top)`.
- Legacy file `notes.toml`; backup `notes.toml.migrated-<unix>`; quarantine `<file>.corrupt-<unix>` (unchanged naming).
- Feature/store ids: `FeatureNotes = "notes"`, `StoreNotes = "notes"`, `NotesFormat = 2`, action `"split-notes"`, `Lossless: true`, `Silent: true`.
- `[notes] max_entries` applies to EACH part; entry-level notes stay cap-exempt (`capOldestFirst` unchanged).
- TUI/CLI/web/MCP code is not touched; their tests must pass unchanged.
- New tests call `t.Parallel()` unless they swap a package-level seam.
- Gates: `./test.sh` before the docs commit, `./test.sh race` before handing over; green ONLY when the log says all green.

## Review Focus

1. **A reply to a merge-preview note** (reply carries `Preview == ""`) must land in `previews.toml` with its root, not in `commits.toml` — pinned in Task 2 (`TestReplyFollowsItsRootsPart`) and Task 3 (migration grouping).
2. **One corrupt part file** must not stop reads/removes of notes in the other parts — pinned in Task 2 (`TestCorruptPartLeavesOtherPartsWorking`).
3. **An older gg recreating `notes.toml` after the split** (an installed `gg mcp`) must lose nothing on the next start — pinned in Task 3 (`TestConvertLegacyMergesARecreatedFile`).
4. **A failed or empty `git worktree list`** during the sweep must delete nothing — pinned in Task 5 (`TestSweepKeepsLiveNotesWhenWorktreeListFails`).
5. **The same worktree path spelled with a trailing slash / `..` segment** must map to the same file — pinned in Task 1 (`TestWorktreePartCleansThePath`).

---

### Task 1: Partition router

**Files:**
- Create: `internal/notes/part.go`
- Test: `internal/notes/part_test.go`

**Interfaces:**
- Produces: `type Part string`; consts `PartCommits = "commits"`, `PartPreviews = "previews"`, `PartShelf = "shelf"`; `func WorktreePart(top string) Part`; `func PartOf(n model.Note) Part`; `func (p Part) IsWorktree() bool`; unexported `func (p Part) file(root string) string`, `func partOfFile(name string) (Part, bool)` (a `worktrees/` entry name → part).

- [ ] **Step 1: Write the failing tests**

```go
package notes

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// Every FileState and Preview shape routes to exactly one part. A new
// FileState without a row here is a routing decision nobody made.
func TestPartOfRoutesEveryKind(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 40)
	wt := WorktreePart("/repo")
	cases := []struct {
		name string
		addr model.FileAddress
		prev string
		want Part
	}{
		{"unstaged", model.FileAddress{State: model.StateUnstaged, Worktree: "/repo", Path: "a.go"}, "", wt},
		{"staged", model.FileAddress{State: model.StateStaged, Worktree: "/repo", Path: "a.go"}, "", wt},
		{"untracked", model.FileAddress{State: model.StateUntracked, Worktree: "/repo", Path: "a.go"}, "", wt},
		{"unscoped live", model.FileAddress{State: model.StateUnstaged, Path: "a.go"}, "", "wt-unscoped"},
		{"shelf", model.FileAddress{State: model.StateShelf, ShelfID: "e1", Path: "a.go"}, "", PartShelf},
		{"shelf entry", model.FileAddress{State: model.StateShelf, ShelfID: "e1"}, "", PartShelf},
		{"commit line", model.FileAddress{State: model.StateCommitted, Commit: sha, Path: "a.go"}, "", PartCommits},
		{"commit review", model.FileAddress{State: model.StateCommitted, Commit: sha}, "", PartCommits},
		{"pair note", model.FileAddress{State: model.StateCommitted, Commit: sha, Path: "a.go"}, "aaaaaaa..bbbbbbb", PartCommits},
		{"merge preview", model.FileAddress{State: model.StateCommitted, Commit: sha, Path: "a.go"}, "main...feat/x", PartPreviews},
	}
	for _, c := range cases {
		if got := PartOf(model.Note{Address: c.addr, Preview: c.prev}); got != c.want {
			t.Errorf("%s: PartOf = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestWorktreePartCleansThePath(t *testing.T) {
	t.Parallel()
	a := WorktreePart("/repo/wt")
	for _, spelling := range []string{"/repo/wt/", "/repo/x/../wt", "/repo//wt"} {
		if got := WorktreePart(spelling); got != a {
			t.Errorf("WorktreePart(%q) = %q, want %q", spelling, got, a)
		}
	}
	if WorktreePart("/repo/other") == a {
		t.Fatal("two worktrees share a part")
	}
	if !a.IsWorktree() || PartCommits.IsWorktree() {
		t.Fatal("IsWorktree misclassifies")
	}
	if len(strings.TrimPrefix(string(a), "wt-")) != 16 {
		t.Fatalf("key %q is not 8 hex bytes", a)
	}
}

func TestPartFileNamesRoundTrip(t *testing.T) {
	t.Parallel()
	root := filepath.FromSlash("/state/notes/k")
	if got := PartCommits.file(root); got != filepath.Join(root, "commits.toml") {
		t.Fatalf("commits file = %q", got)
	}
	wt := WorktreePart("/repo")
	f := wt.file(root)
	if filepath.Dir(f) != filepath.Join(root, "worktrees") {
		t.Fatalf("worktree file %q not under worktrees/", f)
	}
	back, ok := partOfFile(filepath.Base(f))
	if !ok || back != wt {
		t.Fatalf("partOfFile(%q) = %q, %v; want %q", filepath.Base(f), back, ok, wt)
	}
	if _, ok := partOfFile(".notes-123.tmp"); ok {
		t.Fatal("a temp file is not a part")
	}
	if _, ok := partOfFile("x.toml.lock"); ok {
		t.Fatal("a lock file is not a part")
	}
	if _, ok := partOfFile("x.toml.corrupt-17"); ok {
		t.Fatal("a quarantined file is not a part")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/notes-partitions && go test ./internal/notes/ -run 'TestPartOf|TestWorktreePart|TestPartFileNames'`
Expected: FAIL — `undefined: WorktreePart` / `PartOf`.

- [ ] **Step 3: Implement `internal/notes/part.go`**

```go
package notes

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// Part names one file of the store. The store keeps one file per anchor
// kind so a read that cares about one kind — the working tree, on every
// status refresh — never parses the others (spec 2026-10-04 §3).
type Part string

const (
	PartCommits  Part = "commits"  // every committed note but a merge preview's
	PartPreviews Part = "previews" // notes written in a merge preview ("a...b")
	PartShelf    Part = "shelf"    // notes on shelf entries

	worktreePrefix      = "wt-"
	partUnscoped   Part = "wt-unscoped" // live notes that never recorded a worktree
)

// WorktreePart is the part holding one worktree's live notes: the first 8
// bytes of sha256 over the cleaned top-level path (domain's repoKey formula).
// "" is the unscoped part: such notes match no checkout today and stay
// stored, invisible, until the sweep ages them out.
func WorktreePart(top string) Part {
	if strings.TrimSpace(top) == "" {
		return partUnscoped
	}
	sum := sha256.Sum256([]byte(filepath.Clean(top)))
	return Part(worktreePrefix + hex.EncodeToString(sum[:8]))
}

// PartOf routes a ROOT note. A reply belongs with its root, whose part the
// store looks up (a reply carries no Preview of its own) — see FileStore.Put.
// The live-vs-shared test is domain's worktreeScopedNote: no commit and no
// shelf id.
func PartOf(n model.Note) Part {
	a := n.Address
	switch {
	case a.ShelfID != "":
		return PartShelf
	case a.Commit == "":
		return WorktreePart(a.Worktree)
	case strings.Contains(n.Preview, "..."): // domain.IsPreviewScope
		return PartPreviews
	}
	return PartCommits
}

// IsWorktree reports whether p is one worktree's part.
func (p Part) IsWorktree() bool { return strings.HasPrefix(string(p), worktreePrefix) }

// file is p's path under the store root.
func (p Part) file(root string) string {
	if p.IsWorktree() {
		return filepath.Join(root, "worktrees", strings.TrimPrefix(string(p), worktreePrefix)+".toml")
	}
	return filepath.Join(root, string(p)+".toml")
}

// partOfFile maps an entry of the worktrees/ directory back to its part;
// temp, lock and quarantined files are not parts.
func partOfFile(name string) (Part, bool) {
	key, ok := strings.CutSuffix(name, ".toml")
	if !ok || key == "" || strings.HasPrefix(key, ".") {
		return "", false
	}
	return Part(worktreePrefix + key), true
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/notes-partitions && go test ./internal/notes/ -run 'TestPartOf|TestWorktreePart|TestPartFileNames'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/notes-partitions
gg add internal/notes/part.go internal/notes/part_test.go
git commit -F <msgfile>   # "feat(notes): route every note to a store part"
```

---

### Task 2: Partitioned FileStore (one engine, several files)

**Files:**
- Rename: `internal/notes/file_store.go` → `internal/notes/part_file.go` (the single-file store becomes `partFile`)
- Rename: `internal/notes/file_store_test.go` → `internal/notes/part_file_test.go`
- Create: `internal/notes/file_store.go` (the partitioned `FileStore`)
- Create: `internal/notes/file_store_test.go`
- Modify: `internal/notes/store.go` (interface)
- Modify: `internal/domain/*.go` — every `all, err := st.Load()` on the note store → `st.LoadAll()` (mechanical; behaviour identical)
- Modify: `internal/domain/notes_test.go` (`hookStore`), any domain test calling `.Load()` on a note store

**Interfaces:**
- Consumes: Task 1 (`Part`, `PartOf`, `WorktreePart`, `Part.file`, `partOfFile`).
- Produces:
  ```go
  type Store interface {
  	Load(p Part) ([]model.Note, error)
  	LoadAll() ([]model.Note, error)
  	Put(n model.Note) error
  	Remove(id string) error
  	Sweep(keep func(model.Note) bool) (dropped int, err error)
  	SetPolicy(p Policy)
  }
  func NewFileStore(root string) *FileStore          // unchanged signature
  func (fs *FileStore) Parts() ([]Part, error)       // the parts on disk
  func (fs *FileStore) Quarantine() (string, error)  // every corrupt part; ", "-joined
  var ErrPartChange = errors.New("notes: a note cannot move to another part")
  ```
  `partFile` (unexported): `Load() ([]model.Note, error)`, `Put`, `Remove`, `Sweep`, `SetPolicy`, `Quarantine`, `mutateCap(apply, capped bool) (int, error)`, `read()`, `lock()`, `lockPath()`.

- [ ] **Step 1: Move the single-file store and its tests**

```bash
cd /work/gigagit/.claude/worktrees/notes-partitions
git mv internal/notes/file_store.go internal/notes/part_file.go
git mv internal/notes/file_store_test.go internal/notes/part_file_test.go
```

- [ ] **Step 2: Turn `FileStore` in `part_file.go` into `partFile`**

Apply exactly these edits in `internal/notes/part_file.go`:

1. Replace the type comment, struct, constructor and `SetPolicy`:

```go
// partFile is ONE file of the store, rewritten atomically (temp+rename, the
// bookmark pattern) under two locks: a process-local mutex (the sweep
// goroutine vs a `c` keypress in the same process) and a <file>.lock file
// (this gg vs another gg, or a `gg note add`). FileStore composes one per
// part; every rule of the pre-split notes.toml lives here unchanged.
type partFile struct {
	path string // the .toml file

	mu  sync.Mutex // process-local: guards every mutation AND pol
	pol Policy
}

// SetPolicy sets the write-time entry cap. Safe to call at any time.
func (fs *partFile) SetPolicy(p Policy) {
	fs.mu.Lock()
	fs.pol = p
	fs.mu.Unlock()
}
```

2. Replace `path()`/`lockPath()`:

```go
func (fs *partFile) lockPath() string { return fs.path + ".lock" }
```
and change every `fs.path()` in the file to `fs.path`.

3. Rename every receiver `(fs *FileStore)` → `(fs *partFile)`.

4. In `write`, delete an emptied file and keep temp files out of part listings:

```go
// write persists ns via temp-file + rename. A part left with no notes is
// removed instead of written empty, so a removed worktree leaves no file.
func (fs *partFile) write(ns []model.Note) error {
	if len(ns) == 0 {
		if err := os.Remove(fs.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := toml.Marshal(index{Notes: ns})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(fs.path), ".notes-*.tmp")
	// … the rest of the existing body unchanged …
```

5. Split `mutate` so the migration can merge without the cap:

```go
func (fs *partFile) mutate(apply func([]model.Note) ([]model.Note, error)) (int, error) {
	return fs.mutateCap(apply, true)
}

// mutateCap is mutate with the entry cap optional: the legacy conversion
// merges uncapped, because a lossless migration must not drop a thread.
func (fs *partFile) mutateCap(apply func([]model.Note) ([]model.Note, error), capped bool) (int, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	// … existing body of mutate, with
	//     ns = capOldestFirst(ns, fs.pol.MaxEntries)
	// replaced by
	//     if capped { ns = capOldestFirst(ns, fs.pol.MaxEntries) }
}
```

6. Update the `ErrCorrupt` read message and `Quarantine` to use `fs.path` (the `dst` is `fs.path + ".corrupt-" + …`, doc comment "moves the file aside to <file>.corrupt-<unix>").

7. Rename `Load` doc to "Load returns every note of this part. NEVER writes." Keep `NewID` in this file unchanged.

8. Remove `var _ Store = (*FileStore)(nil)` from `store.go` for now (re-added in Step 6).

- [ ] **Step 3: Point the moved tests at `partFile`**

In `internal/notes/part_file_test.go`:

```bash
cd /work/gigagit/.claude/worktrees/notes-partitions
sed -i 's/NewFileStore(t\.TempDir())/newTestFile(t.TempDir())/; s/NewFileStore(dir)/newTestFile(dir)/; s/NewFileStore(root)/newTestFile(root)/' internal/notes/part_file_test.go
sed -i 's/_, err := NewFileStore(dir)\.Load()/_, err := newTestFile(dir).Load()/' internal/notes/part_file_test.go
grep -n "NewFileStore" internal/notes/part_file_test.go   # expect no output
```

Add the helper below `noteAt`:

```go
// newTestFile is one part file named like the pre-split store, so these
// single-file tests keep their file names (notes.toml, notes.toml.lock).
func newTestFile(dir string) *partFile { return &partFile{path: filepath.Join(dir, "notes.toml")} }
```

Add one test pinning the new emptied-file rule:

```go
func TestRemovingTheLastNoteDeletesTheFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := newTestFile(dir)
	if err := fs.Put(noteAt("aaaaaaaa", 1)); err != nil {
		t.Fatal(err)
	}
	if err := fs.Remove("aaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.toml")); !os.IsNotExist(err) {
		t.Fatalf("an emptied part must be removed, stat err = %v", err)
	}
	if ns, err := fs.Load(); err != nil || len(ns) != 0 {
		t.Fatalf("Load after removal = %v, %v", ns, err)
	}
}
```

Run: `go test ./internal/notes/ -run 'Test' 2>&1 | head -30` — expect compile errors only from `store.go`/missing `FileStore` users (none in this package once Step 4 lands). Continue.

- [ ] **Step 4: Write the failing partitioned-store tests**

Create `internal/notes/file_store_test.go`:

```go
package notes

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

var sha40 = strings.Repeat("c", 40)

func liveNote(id, wt string, sec int) model.Note {
	n := noteAt(id, sec)
	n.Address = model.FileAddress{State: model.StateUnstaged, Worktree: wt, Path: "a.go"}
	return n
}

func commitNote(id, preview string, sec int) model.Note {
	n := noteAt(id, sec)
	n.Address = model.FileAddress{State: model.StateCommitted, Commit: sha40, Path: "a.go"}
	n.Preview = preview
	return n
}

func TestPutRoutesEachNoteToItsPartFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fs := NewFileStore(root)
	shelf := noteAt("s0000000", 4)
	shelf.Address = model.FileAddress{State: model.StateShelf, ShelfID: "e1", Path: "a.go"}
	for _, n := range []model.Note{
		liveNote("w0000000", "/repo", 1), commitNote("c0000000", "", 2),
		commitNote("p0000000", "main...feat", 3), shelf,
	} {
		if err := fs.Put(n); err != nil {
			t.Fatalf("Put %s: %v", n.ID, err)
		}
	}
	for part, id := range map[Part]string{
		WorktreePart("/repo"): "w0000000", PartCommits: "c0000000",
		PartPreviews: "p0000000", PartShelf: "s0000000",
	} {
		got, err := fs.Load(part)
		if err != nil || len(got) != 1 || got[0].ID != id {
			t.Errorf("Load(%s) = %+v, %v; want only %s", part, got, err, id)
		}
		if _, err := os.Stat(part.file(root)); err != nil {
			t.Errorf("%s: file missing: %v", part, err)
		}
	}
	all, err := fs.LoadAll()
	if err != nil || len(all) != 4 {
		t.Fatalf("LoadAll = %d notes, %v; want 4", len(all), err)
	}
	if _, err := os.Stat(filepath.Join(root, "notes.toml")); !os.IsNotExist(err) {
		t.Fatal("the split store must never write notes.toml")
	}
}

// A reply carries no Preview of its own: it must still land with its
// merge-preview root, or the preview would lose its threads.
func TestReplyFollowsItsRootsPart(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	root := commitNote("r0000000", "main...feat", 1)
	reply := commitNote("p0000000", "", 2)
	reply.ParentID = root.ID
	for _, n := range []model.Note{root, reply} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	prev, _ := fs.Load(PartPreviews)
	if len(prev) != 2 {
		t.Fatalf("previews part = %+v, want root + reply", prev)
	}
	if com, _ := fs.Load(PartCommits); len(com) != 0 {
		t.Fatalf("the reply leaked into commits: %+v", com)
	}
}

func TestRemoveFindsTheIdInAnyPart(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	root := liveNote("r0000000", "/repo", 1)
	reply := liveNote("p0000000", "/repo", 2)
	reply.ParentID = root.ID
	for _, n := range []model.Note{root, reply, commitNote("c0000000", "", 3)} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := fs.Remove(root.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	all, _ := fs.LoadAll()
	if len(all) != 1 || all[0].ID != "c0000000" {
		t.Fatalf("left %+v, want only the commit note", all)
	}
	if err := fs.Remove("nosuch00"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Remove(unknown) = %v, want ErrNotFound", err)
	}
}

func TestPutRefusesToMoveANoteToAnotherPart(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	n := liveNote("m0000000", "/repo", 1)
	if err := fs.Put(n); err != nil {
		t.Fatal(err)
	}
	n.Address = model.FileAddress{State: model.StateCommitted, Commit: sha40, Path: "a.go"}
	if err := fs.Put(n); !errors.Is(err, ErrPartChange) {
		t.Fatalf("Put across parts = %v, want ErrPartChange", err)
	}
}

func TestCorruptPartLeavesOtherPartsWorking(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fs := NewFileStore(root)
	if err := fs.Put(commitNote("c0000000", "", 1)); err != nil {
		t.Fatal(err)
	}
	if err := fs.Put(liveNote("w0000000", "/repo", 2)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(PartCommits.file(root), []byte("notes = [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := fs.Load(WorktreePart("/repo")); err != nil || len(got) != 1 {
		t.Fatalf("a healthy part must stay readable: %+v, %v", got, err)
	}
	if _, err := fs.LoadAll(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("LoadAll = %v, want ErrCorrupt naming the bad part", err)
	}
	if err := fs.Remove("w0000000"); err != nil {
		t.Fatalf("removing from a healthy part must not fail on a corrupt one: %v", err)
	}
	moved, err := fs.Quarantine()
	if err != nil || !strings.Contains(moved, "commits.toml.corrupt-") {
		t.Fatalf("Quarantine = %q, %v", moved, err)
	}
	if _, err := fs.LoadAll(); err != nil {
		t.Fatalf("after quarantine LoadAll = %v", err)
	}
}

func TestCapAppliesPerPart(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	fs.SetPolicy(Policy{MaxEntries: 2})
	for i, n := range []model.Note{
		commitNote("c1000000", "", 1), commitNote("c2000000", "", 2),
		liveNote("w1000000", "/repo", 3), liveNote("w2000000", "/repo", 4),
	} {
		if err := fs.Put(n); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}
	if all, _ := fs.LoadAll(); len(all) != 4 {
		t.Fatalf("a cap of 2 per part must keep 2+2, got %d", len(all))
	}
	if err := fs.Put(commitNote("c3000000", "", 5)); err != nil {
		t.Fatal(err)
	}
	com, _ := fs.Load(PartCommits)
	if len(com) != 2 || com[0].ID == "c1000000" || com[1].ID == "c1000000" {
		t.Fatalf("commits part = %+v, want the two newest", com)
	}
}

func TestSweepVisitsEveryPart(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fs := NewFileStore(root)
	for _, n := range []model.Note{commitNote("c0000000", "", 1), liveNote("w0000000", "/gone", 2)} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	dropped, err := fs.Sweep(func(n model.Note) bool { return n.ID != "w0000000" })
	if err != nil || dropped != 1 {
		t.Fatalf("Sweep = %d, %v", dropped, err)
	}
	if _, err := os.Stat(WorktreePart("/gone").file(root)); !os.IsNotExist(err) {
		t.Fatal("the emptied worktree file must be removed")
	}
}

func TestEmptyStoreHasNoPartsAndCreatesNothing(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "state")
	fs := NewFileStore(root)
	if ps, err := fs.Parts(); err != nil || len(ps) != 0 {
		t.Fatalf("Parts = %v, %v", ps, err)
	}
	if ns, err := fs.LoadAll(); err != nil || len(ns) != 0 {
		t.Fatalf("LoadAll = %v, %v", ns, err)
	}
	if n, err := fs.Sweep(func(model.Note) bool { return false }); err != nil || n != 0 {
		t.Fatalf("Sweep = %d, %v", n, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("reads and an empty sweep must not create the state directory")
	}
}
```

Run: `cd /work/gigagit/.claude/worktrees/notes-partitions && go test ./internal/notes/`
Expected: FAIL to compile — `NewFileStore`, `FileStore.Load(Part)`, `ErrPartChange` undefined.

- [ ] **Step 5: Implement the partitioned `FileStore`**

Create `internal/notes/file_store.go`:

```go
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
// own operations fail on their own).
func (fs *FileStore) where() (map[string]Part, error) {
	parts, err := fs.Parts()
	if err != nil {
		return nil, err
	}
	at := map[string]Part{}
	for _, p := range parts {
		ns, lerr := fs.Load(p)
		if lerr != nil {
			continue
		}
		for _, n := range ns {
			at[n.ID] = p
		}
	}
	return at, nil
}

// Put adds n, or replaces the record with the same ID, in n's part. A reply
// goes to its root's part (a reply carries no Preview of its own); a reply
// whose root is gone falls back to PartOf and the orphan prune drops it.
func (fs *FileStore) Put(n model.Note) error {
	at, err := fs.where()
	if err != nil {
		return err
	}
	want := PartOf(n)
	if n.IsReply() {
		if p, ok := at[n.ParentID]; ok {
			want = p
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
```

Replace the interface in `internal/notes/store.go`:

```go
// Store persists note records in parts (spec 2026-10-04): Load reads one
// part, LoadAll every part; neither writes. Put routes by PartOf (a reply by
// its root); every other method serialises against other processes and
// other goroutines, per part.
type Store interface {
	Load(p Part) ([]model.Note, error)
	LoadAll() ([]model.Note, error)
	Put(n model.Note) error // add, or replace by ID
	Remove(id string) error // a root takes its replies
	Sweep(keep func(model.Note) bool) (dropped int, err error)
	SetPolicy(p Policy) // the write-time budget lives ON the store (§4.4)
}

var _ Store = (*FileStore)(nil)
```

Also update the package doc in `store.go`: "The store keeps one file per anchor kind (part.go); Load never writes, every mutation re-reads ITS part under a cross-process lock, applies, enforces the entry cap and rewrites atomically."

- [ ] **Step 6: Run the notes package tests**

Run: `cd /work/gigagit/.claude/worktrees/notes-partitions && go test ./internal/notes/`
Expected: PASS (all moved single-file tests + the new partitioned ones).

- [ ] **Step 7: Switch domain to the new interface (mechanical, behaviour identical)**

```bash
cd /work/gigagit/.claude/worktrees/notes-partitions
grep -ln 'all, err := st.Load()' internal/domain/*.go | grep -v _test | xargs sed -i 's/all, err := st\.Load()/all, err := st.LoadAll()/'
grep -n 'st\.Load()' internal/domain/*.go | grep -v _test     # expect no output
grep -n '\.Load()' internal/domain/*_test.go | grep -iv 'atomic\|showEOL\|syntaxOff\|versionsPolicy\|Runs\.'
```

For each test hit on a NOTE store (e.g. `store.Load()`, `fs.Load()` in `notes_sweep_test.go` / `notes_worktree_test.go`), replace `.Load()` with `.LoadAll()`.

In `internal/domain/notes_test.go` replace the `hookStore` method:

```go
func (h *hookStore) fire() {
	if h.onLoad != nil {
		h.onLoad()
	}
}

func (h *hookStore) LoadAll() ([]model.Note, error) { h.fire(); return h.Store.LoadAll() }

func (h *hookStore) Load(p notes.Part) ([]model.Note, error) { h.fire(); return h.Store.Load(p) }
```

`internal/domain/notesstore.go` keeps `notes.NewFileStore(root)` unchanged. In `review_notes.go`, the `Quarantine` type assertion (`st.(interface{ Quarantine() (string, error) })`) still matches `*notes.FileStore`.

- [ ] **Step 8: Build and run the affected suites**

Run: `cd /work/gigagit/.claude/worktrees/notes-partitions && go build ./... && go vet ./internal/notes/ ./internal/domain/ && go test ./internal/notes/ ./internal/domain/`
Expected: PASS. (Any domain test that read `notes.toml` by name must now read the part file: replace `filepath.Join(dir, "notes.toml")` with the part's path, e.g. `filepath.Join(dir, "commits.toml")` or `filepath.Join(dir, "worktrees")` listing.)

- [ ] **Step 9: Commit**

```bash
cd /work/gigagit/.claude/worktrees/notes-partitions
gg add internal/notes internal/domain
git commit -F <msgfile>   # "feat(notes): one engine over per-part files (commits, previews, shelf, worktrees)"
```

---

### Task 3: Legacy conversion (`notes.toml` → parts)

**Files:**
- Create: `internal/notes/legacy.go`
- Test: `internal/notes/legacy_test.go`

**Interfaces:**
- Consumes: Task 2 (`FileStore.file`, `partFile.read/lock/mutateCap`, `PartOf`).
- Produces: `const LegacyFile = "notes.toml"`; `func (fs *FileStore) LegacyPresent() bool`; `func (fs *FileStore) ConvertLegacy() (int, error)` (notes converted).

- [ ] **Step 1: Write the failing tests**

```go
package notes

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/model"
)

func writeLegacy(t *testing.T, root string, ns []model.Note) {
	t.Helper()
	data, err := toml.Marshal(index{Notes: ns})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, LegacyFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConvertLegacySplitsEveryKindLosslessly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	prevRoot := commitNote("r0000000", "main...feat", 1)
	prevReply := commitNote("p0000000", "", 2) // a reply carries no Preview
	prevReply.ParentID = prevRoot.ID
	shelf := noteAt("s0000000", 3)
	shelf.Address = model.FileAddress{State: model.StateShelf, ShelfID: "e1"}
	legacy := []model.Note{
		liveNote("w0000000", "/repo", 0), prevRoot, prevReply, shelf,
		commitNote("c0000000", "", 4), liveNote("u0000000", "", 5),
	}
	writeLegacy(t, root, legacy)
	fs := NewFileStore(root)
	fs.SetPolicy(Policy{MaxEntries: 1}) // the conversion must ignore the cap
	if !fs.LegacyPresent() {
		t.Fatal("LegacyPresent = false with notes.toml on disk")
	}

	n, err := fs.ConvertLegacy()
	if err != nil || n != len(legacy) {
		t.Fatalf("ConvertLegacy = %d, %v; want %d", n, err, len(legacy))
	}
	all, err := fs.LoadAll()
	if err != nil || len(all) != len(legacy) {
		t.Fatalf("LoadAll after conversion = %d, %v", len(all), err)
	}
	if prev, _ := fs.Load(PartPreviews); len(prev) != 2 {
		t.Fatalf("previews part = %+v, want the root AND its reply", prev)
	}
	if un, _ := fs.Load(WorktreePart("")); len(un) != 1 {
		t.Fatalf("unscoped part = %+v", un)
	}
	if fs.LegacyPresent() {
		t.Fatal("notes.toml must be renamed away")
	}
	backups, _ := filepath.Glob(filepath.Join(root, LegacyFile+".migrated-*"))
	if len(backups) != 1 {
		t.Fatalf("want one backup, got %v", backups)
	}
}

// An older gg (an installed gg mcp) may recreate notes.toml after the split:
// the next conversion merges it — the newer Updated wins, nothing is lost.
func TestConvertLegacyMergesARecreatedFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fs := NewFileStore(root)
	kept := commitNote("c0000000", "", 1)
	kept.Updated = time.Unix(1_800_000_000, 0).UTC()
	kept.Summary = "newer, in the part"
	if err := fs.Put(kept); err != nil {
		t.Fatal(err)
	}
	stale := kept
	stale.Summary = "older, from the old gg"
	stale.Updated = kept.Updated.Add(-time.Hour)
	writeLegacy(t, root, []model.Note{stale, commitNote("n0000000", "", 2)})

	if _, err := fs.ConvertLegacy(); err != nil {
		t.Fatal(err)
	}
	com, _ := fs.Load(PartCommits)
	if len(com) != 2 {
		t.Fatalf("commits = %+v, want the existing note + the new one", com)
	}
	for _, n := range com {
		if n.ID == kept.ID && n.Summary != "newer, in the part" {
			t.Fatalf("an older copy overwrote a newer note: %+v", n)
		}
	}
}

func TestConvertLegacyRefusesACorruptFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, LegacyFile), []byte("notes = [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := NewFileStore(root)
	if _, err := fs.ConvertLegacy(); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("ConvertLegacy = %v, want ErrCorrupt", err)
	}
	if !fs.LegacyPresent() {
		t.Fatal("a corrupt legacy file must be left where it is")
	}
}

func TestConvertLegacyWithNothingToDo(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "absent")
	fs := NewFileStore(root)
	if n, err := fs.ConvertLegacy(); err != nil || n != 0 {
		t.Fatalf("ConvertLegacy on no file = %d, %v", n, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("nothing to convert must create nothing")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/notes-partitions && go test ./internal/notes/ -run TestConvertLegacy`
Expected: FAIL — `undefined: LegacyFile`.

- [ ] **Step 3: Implement `internal/notes/legacy.go`**

```go
package notes

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/homeend/gigagit/internal/model"
)

// LegacyFile is the single-file layout every gg before the split wrote.
const LegacyFile = "notes.toml"

// LegacyPresent reports whether a pre-split notes.toml sits in the root —
// the preflight probe behind the split-notes migration.
func (fs *FileStore) LegacyPresent() bool {
	_, err := os.Stat(filepath.Join(fs.root, LegacyFile))
	return err == nil
}

// ConvertLegacy moves every note of notes.toml into its part and renames
// the file to notes.toml.migrated-<unix> (a backup, never deleted). It holds
// notes.toml.lock — the lock every older gg takes — so an old writer cannot
// interleave. Merging is by id, the newer Updated winning, and ignores the
// entry cap: re-running after an older gg recreated the file loses nothing.
// A corrupt file is left in place and reported (ErrCorrupt).
func (fs *FileStore) ConvertLegacy() (int, error) {
	if !fs.LegacyPresent() {
		return 0, nil
	}
	legacy := &partFile{path: filepath.Join(fs.root, LegacyFile)}
	unlock, err := legacy.lock()
	if err != nil {
		return 0, err
	}
	defer unlock()
	old, err := legacy.read()
	if err != nil {
		return 0, err
	}
	byID := make(map[string]model.Note, len(old))
	for _, n := range old {
		byID[n.ID] = n
	}
	groups := map[Part][]model.Note{}
	for _, n := range old {
		p := PartOf(n)
		if n.IsReply() {
			if root, ok := byID[n.ParentID]; ok {
				p = PartOf(root)
			}
		}
		groups[p] = append(groups[p], n)
	}
	for p, in := range groups {
		if _, err := fs.file(p).mutateCap(func(cur []model.Note) ([]model.Note, error) {
			return mergeNewer(cur, in), nil
		}, false); err != nil {
			return 0, err
		}
	}
	dst := legacy.path + ".migrated-" + strconv.FormatInt(Now().Unix(), 10)
	if err := os.Rename(legacy.path, dst); err != nil {
		return 0, err
	}
	return len(old), nil
}

// mergeNewer folds in into cur by id: a new id is appended, a known one is
// replaced only by a strictly newer Updated.
func mergeNewer(cur, in []model.Note) []model.Note {
	at := make(map[string]int, len(cur))
	for i, n := range cur {
		at[n.ID] = i
	}
	for _, n := range in {
		if i, ok := at[n.ID]; ok {
			if n.Updated.After(cur[i].Updated) {
				cur[i] = n
			}
			continue
		}
		at[n.ID] = len(cur)
		cur = append(cur, n)
	}
	return cur
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/notes-partitions && go test ./internal/notes/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/notes-partitions
gg add internal/notes/legacy.go internal/notes/legacy_test.go
git commit -F <msgfile>   # "feat(notes): convert a legacy notes.toml into parts, idempotently"
```

---

### Task 4: Domain readers read only their parts

**Files:**
- Create: `internal/domain/notes_parts.go`
- Modify: `internal/domain/notes.go` (`loadNotesAt`, `NoteCounts`, `NoteAddresses`), `internal/domain/notes_overview.go` (`NotesOverview`), `internal/domain/review_notes.go` (`reviewNotes`, the branch delete/rename follow-up at the `st.LoadAll()` near `case engine.RenameBranch`), `internal/domain/previewnotes.go` (`loadPreviewNotes`)
- Test: `internal/domain/notes_parts_test.go`

**Interfaces:**
- Consumes: Task 2 (`notes.Store.Load(p)`, `notes.WorktreePart`, `notes.PartCommits/PartPreviews/PartShelf`).
- Produces (domain, unexported): `func loadParts(st notes.Store, parts ...notes.Part) ([]model.Note, error)`; `func addrParts(addr model.FileAddress) []notes.Part`; `func (s *Service) visibleParts(ctx context.Context) []notes.Part`; `func previewParts(set PreviewNoteSet) []notes.Part`.

Reader → parts (spec §5): `NoteAdd`/`NoteEdit`/`NoteReply`/`NoteGet`/`putReview`/sweep keep `LoadAll()` (ids, NewID, housekeeping).

- [ ] **Step 1: Write the failing test**

```go
package domain

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// partsStore records which parts every read touched.
type partsStore struct {
	notes.Store
	mu    sync.Mutex
	parts []notes.Part
	all   int
}

func (p *partsStore) Load(part notes.Part) ([]model.Note, error) {
	p.mu.Lock()
	p.parts = append(p.parts, part)
	p.mu.Unlock()
	return p.Store.Load(part)
}

func (p *partsStore) LoadAll() ([]model.Note, error) {
	p.mu.Lock()
	p.all++
	p.mu.Unlock()
	return p.Store.LoadAll()
}

func (p *partsStore) take() ([]notes.Part, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ps, all := p.parts, p.all
	p.parts, p.all = nil, 0
	return ps, all
}

func TestNoteReadersLoadOnlyTheirParts(t *testing.T) {
	t.Parallel()
	dir := noteSideRepo(t)
	svc := svcIn(t, dir)
	ps := &partsStore{Store: notes.NewFileStore(t.TempDir())}
	svc.SetNotesStore(ps)
	ctx := context.Background()
	top, err := svc.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wt := notes.WorktreePart(top)
	live := model.FileAddress{State: model.StateUnstaged, Worktree: top, Path: "a.go"}
	commit := model.FileAddress{State: model.StateCommitted, Commit: headSHA(t, dir), Path: "a.go"}
	for _, a := range []model.FileAddress{live, commit} {
		if _, err := svc.NoteAdd(ctx, model.Note{Address: a, Side: model.NoteSideNew, Range: [2]int{1, 1}, Summary: "x"}); err != nil {
			t.Fatalf("NoteAdd %v: %v", a, err)
		}
	}
	ps.take()

	check := func(name string, want []notes.Part) {
		t.Helper()
		got, all := ps.take()
		if all != 0 {
			t.Errorf("%s read every part (LoadAll ×%d)", name, all)
		}
		slices.Sort(got)
		got = slices.Compact(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s loaded %v, want %v", name, got, want)
		}
	}
	if _, err := svc.NotesAt(ctx, live); err != nil {
		t.Fatal(err)
	}
	check("NotesAt(live)", []notes.Part{wt})
	if _, err := svc.NotesAt(ctx, commit); err != nil {
		t.Fatal(err)
	}
	check("NotesAt(commit)", []notes.Part{notes.PartCommits, notes.PartPreviews})
	svc.InvalidateNoteCounts()
	if c, err := svc.NoteCounts(ctx); err != nil || c.ByPath["a.go"] != 1 {
		t.Fatalf("NoteCounts = %+v, %v", c, err)
	}
	visible := []notes.Part{notes.PartCommits, notes.PartPreviews, notes.PartShelf, wt}
	check("NoteCounts", visible)
	if _, err := svc.NoteAddresses(ctx); err != nil {
		t.Fatal(err)
	}
	check("NoteAddresses", visible)
	if _, err := svc.reviewNotes(ctx); err != nil {
		t.Fatal(err)
	}
	check("reviewNotes", []notes.Part{notes.PartCommits})
}

func TestPreviewPartsByScope(t *testing.T) {
	t.Parallel()
	cases := []struct {
		scope string
		want  []notes.Part
	}{
		{"main...feat", []notes.Part{notes.PartPreviews}},
		{"aaaaaaa..bbbbbbb", []notes.Part{notes.PartCommits}},
		{"", []notes.Part{notes.PartCommits, notes.PartPreviews}},
	}
	for _, c := range cases {
		if got := scopeParts(c.scope); !slices.Equal(got, c.want) {
			t.Errorf("scopeParts(%q) = %v, want %v", c.scope, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/notes-partitions && go test ./internal/domain/ -run 'TestNoteReadersLoadOnlyTheirParts|TestPreviewPartsByScope'`
Expected: FAIL — `undefined: scopeParts`, and (once that compiles) readers report `LoadAll ×1`.

- [ ] **Step 3: Implement `internal/domain/notes_parts.go`**

```go
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
```

- [ ] **Step 4: Point each reader at its parts**

`internal/domain/notes.go`, `loadNotesAt` — move the load AFTER the worktree pinning:

```go
func (s *Service) loadNotesAt(ctx context.Context, addr model.FileAddress) ([]model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, ErrNotesDisabled
	}
	// Scope the query to this checkout so a sibling worktree's notes on the
	// same path never surface here — and so the right part is read.
	if worktreeScopedNote(addr) {
		wt, werr := s.noteWorktree(ctx, addr)
		if werr != nil {
			return nil, werr
		}
		addr.Worktree = wt
	}
	all, err := loadParts(st, addrParts(addr)...)
	if err != nil {
		return nil, err
	}
	mine := make([]model.Note, 0, len(all))
	for _, n := range all {
		if sameNoteTarget(n.Address, addr) {
			mine = append(mine, n)
		}
	}
	return mine, nil
}
```

`NoteCounts` and `NoteAddresses` in `notes.go`, `NotesOverview` in `notes_overview.go`: replace `all, err := st.LoadAll()` with

```go
	all, err := loadParts(st, s.visibleParts(ctx)...)
```

`review_notes.go`: in `reviewNotes` AND in the branch delete/rename follow-up (the function containing `case engine.RenameBranch:`), replace `all, err := st.LoadAll()` with

```go
	all, err := st.Load(notes.PartCommits)
```

`previewnotes.go`, `loadPreviewNotes`: replace `all, err := st.LoadAll()` with

```go
	all, err := loadParts(st, scopeParts(set.scope())...)
```

Leave `LoadAll()` in `NoteAdd`, `NoteEdit`, `NoteReply`, `NoteGet`, `putReview`, `sweepNotes`.

- [ ] **Step 5: Run the domain suite**

Run: `cd /work/gigagit/.claude/worktrees/notes-partitions && go test ./internal/domain/`
Expected: PASS, including every pre-existing notes / preview / review / overview test (they assert the visible behaviour this task must not change).

- [ ] **Step 6: Commit**

```bash
cd /work/gigagit/.claude/worktrees/notes-partitions
gg add internal/domain/notes_parts.go internal/domain/notes_parts_test.go internal/domain/notes.go internal/domain/notes_overview.go internal/domain/review_notes.go internal/domain/previewnotes.go
git commit -F <msgfile>   # "feat(domain): each note read loads only the parts it needs"
```

---

### Task 5: Sweep drops notes of deleted worktrees

**Files:**
- Modify: `internal/domain/notes_sweep.go` (`sweepNotes`)
- Test: `internal/domain/notes_sweep_test.go`

**Interfaces:**
- Consumes: `s.Worktrees(ctx) ([]model.Worktree, error)`, `worktreeScopedNote`, Task 2's emptied-file removal.
- Produces: `func normWorktree(p string) string` (domain, unexported).

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/notes_sweep_test.go`:

```go
// A live note of a worktree git no longer lists is dropped by the sweep, and
// the worktree's emptied part file goes with it.
func TestSweepDropsNotesOfARemovedWorktree(t *testing.T) {
	t.Parallel()
	dir := noteSideRepo(t)
	other := filepath.Join(t.TempDir(), "wt2")
	gittest.Run(t, dir, "worktree", "add", "-b", "side", other)
	root := t.TempDir()
	store := notes.NewFileStore(root)
	main, side := svcIn(t, dir), svcIn(t, other)
	main.SetNotesStore(store)
	side.SetNotesStore(store)
	ctx := context.Background()
	sideTop, err := side.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := side.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: sideTop, Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "on the side worktree",
	}); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "worktree", "remove", "--force", other)

	dropped, err := main.sweepNotes(ctx)
	if err != nil || dropped != 1 {
		t.Fatalf("sweepNotes = %d, %v; want 1", dropped, err)
	}
	if ents, _ := os.ReadDir(filepath.Join(root, "worktrees")); len(ents) != 0 {
		t.Fatalf("the removed worktree's file survived: %v", ents)
	}
}

// Timid like every sweep rule: no worktree list, no deletion. The second arm
// proves the note was otherwise droppable — the same note goes once git
// lists worktrees that do not include its checkout. The note's file exists
// and matches, so resolution alone would keep it in both arms.
func TestSweepKeepsLiveNotesWhenWorktreeListFails(t *testing.T) {
	t.Parallel()
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, "a.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(listing *gitexec.Result) int {
		t.Helper()
		f := gitexec.NewFakeRunner()
		f.SetResponse("git rev-parse (toplevel)", gitexec.Result{Stdout: "/main\n"})
		if listing != nil {
			f.SetResponse("git worktree list", *listing)
		} // else: the fake errors on the unconfigured command
		svc := New(&git.Repo{Runner: f})
		store := notes.NewFileStore(t.TempDir())
		svc.SetNotesStore(store)
		n := model.Note{ID: "w0000000", Source: model.NoteSourceUser, Side: model.NoteSideNew,
			Address: model.FileAddress{State: model.StateUnstaged, Worktree: wt, Path: "a.go"},
			Range:   [2]int{1, 1}, ContextHash: model.NoteContextHash([]string{"x"}), Created: notes.Now().UTC()}
		if err := store.Put(n); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.sweepNotes(context.Background()); err != nil {
			t.Fatal(err)
		}
		all, _ := store.LoadAll()
		return len(all)
	}
	if left := run(nil); left != 1 {
		t.Fatalf("a failed worktree list must delete nothing, %d left", left)
	}
	if left := run(&gitexec.Result{Stdout: ""}); left != 1 {
		t.Fatalf("an empty worktree list must delete nothing, %d left", left)
	}
	only := gitexec.Result{Stdout: "worktree /main\nHEAD " + strings.Repeat("a", 40) + "\nbranch refs/heads/main\n\n"}
	if left := run(&only); left != 0 {
		t.Fatalf("a note of an unlisted worktree must go, %d left", left)
	}
}
```

(Add any missing imports: `os`, `path/filepath`, `strings`, `gittest`.) If the porcelain parser in `internal/git/repo.go` (`Worktrees`) expects other fields, copy the exact shape from `internal/git/repo_test.go:TestRepoWorktrees`.

- [ ] **Step 2: Run the tests to verify the first fails**

Run: `cd /work/gigagit/.claude/worktrees/notes-partitions && go test ./internal/domain/ -run 'TestSweepDropsNotesOfARemovedWorktree|TestSweepKeepsLiveNotesWhenWorktreeListFails'`
Expected: both FAIL — `dropped = 0` in the first, and the third arm ("a note of an unlisted worktree must go") in the second. The first two arms of the second test already pass: they pin the guard.

- [ ] **Step 3: Implement the rule in `sweepNotes`**

After the `cutoff` computation and before `// Phase 1`, add:

```go
	// Live notes of a worktree git no longer lists go: their checkout is
	// gone. A failed — or impossible, empty — list removes nothing.
	var liveWT map[string]bool
	if wts, werr := s.Worktrees(ctx); werr == nil && len(wts) > 0 {
		liveWT = make(map[string]bool, len(wts))
		for _, w := range wts {
			liveWT[normWorktree(w.Path)] = true
		}
	} else if cerr := ctx.Err(); cerr != nil {
		return 0, cerr
	}
```

Inside the Phase-1 loop, right after the `IsShelfLevel` block and before the `cutoff` check:

```go
		if liveWT != nil && worktreeScopedNote(n.Address) && n.Address.Worktree != "" &&
			!liveWT[normWorktree(n.Address.Worktree)] {
			drop[n.ID] = true
			continue
		}
```

At the bottom of the file:

```go
// normWorktree compares checkout roots the way git and filepath may each
// spell them: cleaned (which also converts / to \ on Windows) and, on
// Windows, case-folded.
func normWorktree(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}
```

(Add `path/filepath`, `runtime`, `strings` imports as needed.)

- [ ] **Step 4: Run the sweep tests**

Run: `cd /work/gigagit/.claude/worktrees/notes-partitions && go test ./internal/domain/ -run 'Sweep'`
Expected: PASS (new and existing sweep tests).

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/notes-partitions
gg add internal/domain/notes_sweep.go internal/domain/notes_sweep_test.go
git commit -F <msgfile>   # "feat(domain): the note sweep drops notes of removed worktrees"
```

---

### Task 6: The `split-notes` lossless migration

**Files:**
- Modify: `internal/domain/features.go` (ids, `NotesFormat`, feature entry)
- Modify: `internal/domain/preflight.go` (two Legacy probe maps, `migrationAction`, `legacyNotesPresent`)
- Modify: `internal/domain/migrationactions.go` (`splitNotes`)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (the Describe text)
- Test: `internal/domain/notesmigration_test.go`

**Interfaces:**
- Consumes: Task 3 (`(*notes.FileStore).LegacyPresent`, `ConvertLegacy`, `notes.LegacyFile`).
- Produces: `FeatureNotes`, `StoreNotes`, `NotesFormat`; `type splitNotes struct{ Store *notes.FileStore; After func() }`.

- [ ] **Step 1: Write the failing test**

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// A legacy notes.toml is split WITHOUT asking, and every note is still found
// by the reader that showed it before.
func TestRunAutoMigrationsSplitsTheNoteStore(t *testing.T) {
	t.Parallel()
	dir := noteSideRepo(t)
	svc := svcIn(t, dir)
	root := t.TempDir()
	svc.UseNotesDir(root)
	ctx := context.Background()
	top, err := svc.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sha := headSHA(t, dir)
	live := model.Note{ID: "w0000000", Source: model.NoteSourceUser, Side: model.NoteSideNew, Range: [2]int{1, 1},
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: top, Path: "a.go"}, Summary: "live"}
	review := model.Note{ID: "r0000000", Source: model.NoteSourceAgent, Side: model.NoteSideNew,
		Address: model.FileAddress{State: model.StateCommitted, Commit: sha}, Tags: []string{model.ReviewTag},
		Summary: "Review: main", Rationale: "looks fine"}
	data, err := toml.Marshal(struct {
		Notes []model.Note `toml:"notes"`
	}{[]model.Note{live, review}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, notes.LegacyFile), data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.RunAutoMigrations(ctx); err != nil {
		t.Fatalf("RunAutoMigrations: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, notes.LegacyFile)); !os.IsNotExist(err) {
		t.Fatalf("notes.toml survived the split (stat err = %v)", err)
	}
	if got, err := svc.NoteGet(ctx, live.ID); err != nil || got.Summary != "live" {
		t.Fatalf("NoteGet(live) = %+v, %v", got, err)
	}
	if rs, err := svc.reviewNotes(ctx); err != nil || len(rs) != 1 || rs[0].ID != review.ID {
		t.Fatalf("reviewNotes = %+v, %v", rs, err)
	}
	svc.InvalidateNoteCounts()
	if c, err := svc.NoteCounts(ctx); err != nil || len(c.Reviews) != 1 {
		t.Fatalf("NoteCounts = %+v, %v", c, err)
	}
	// Nothing left to do: a second run is a no-op, not an error.
	if err := svc.RunAutoMigrations(ctx); err != nil {
		t.Fatalf("second RunAutoMigrations: %v", err)
	}
}

func TestNotesFeatureDeclaresALosslessSilentMigration(t *testing.T) {
	t.Parallel()
	for _, f := range Features() {
		if f.ID != FeatureNotes {
			continue
		}
		m := f.Migrate
		if m == nil || m.Store != StoreNotes || m.From != 1 || m.To != NotesFormat || !m.Lossless || m.Action != "split-notes" {
			t.Fatalf("notes migration = %+v", m)
		}
		if !f.Silent {
			t.Fatal("a recreated notes.toml mid-session must not raise a notice")
		}
		if m.Describe == nil || m.Describe().Format == "" {
			t.Fatal("Describe is required")
		}
		return
	}
	t.Fatal("no notes feature registered")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/notes-partitions && go test ./internal/domain/ -run 'TestRunAutoMigrationsSplitsTheNoteStore|TestNotesFeatureDeclares'`
Expected: FAIL — `undefined: FeatureNotes`.

- [ ] **Step 3: Register the feature**

`internal/domain/features.go` — add to the const block:

```go
	FeatureNotes = "notes"

	StoreNotes = "notes"
```

below `PreviewsFormat`:

```go
// NotesFormat is the note-store layout this build writes. Format 1 was the
// single notes.toml; format 2 splits it into one file per part (spec
// 2026-10-04). Like PreviewsFormat it only labels the marker: the
// requirement asks presence of the legacy file (preflight.LegacyStore).
const NotesFormat = 2
```

and to `Features()` after the previews entry:

```go
		{
			ID:          FeatureNotes,
			Criticality: preflight.Optional,
			// An older gg (an installed gg mcp) may recreate notes.toml
			// mid-session; the next start merges it. Never worth a notice.
			Silent: true,
			Requires: []preflight.Requirement{
				preflight.LegacyStore{Store: StoreNotes},
			},
			// LOSSLESS: every note's address names its file, the merge is by
			// id with the newer copy winning, and the old file is kept as a
			// backup — there is nothing to confess, so nothing to ask.
			Migrate: &preflight.Migration{
				Store: StoreNotes, From: 1, To: NotesFormat,
				Action: "split-notes", Lossless: true,
				Describe: func() preflight.Text {
					return preflight.Text{
						Format: "Splits the note store into one file per worktree plus files for commits, previews and shelf entries. Nothing is lost: every note keeps its id, and the old file is kept as a backup.",
					}
				},
			},
		},
```

- [ ] **Step 4: Wire the probe and the action**

`internal/domain/preflight.go` — add `StoreNotes: {Present: s.legacyNotesPresent(ctx)},` to BOTH `Legacy:` maps (the one in the probes builder next to `StorePreviews`, and the one in `RunAutoMigrations`). Add below `legacyPreviewsPresent`:

```go
// legacyNotesPresent reports a pre-split notes.toml in this repo's note
// store. An injected non-file store (tests) has no legacy layout.
func (s *Service) legacyNotesPresent(ctx context.Context) bool {
	fs, ok := s.notesStore(ctx).(*notes.FileStore)
	return ok && fs.LegacyPresent()
}
```

In `migrationAction`, add a case before `"discard-refs"`:

```go
	case "split-notes":
		fs, ok := s.notesStore(ctx).(*notes.FileStore)
		if !ok {
			return nil, fmt.Errorf("preflight: no note file store to split")
		}
		return splitNotes{Store: fs, After: s.invalidateNoteCounts}, nil
```

`internal/domain/migrationactions.go`:

```go
// splitNotes moves a legacy notes.toml into the per-part files
// (notes.FileStore.ConvertLegacy) and drops the cached badge counts.
type splitNotes struct {
	Store *notes.FileStore
	After func()
}

var _ engine.MigrationAction = splitNotes{}

func (a splitNotes) Describe() string { return "splitting the note store" }

func (a splitNotes) Apply(ctx context.Context, deps engine.OpDeps) (int, error) {
	n, err := a.Store.ConvertLegacy()
	if err == nil && a.After != nil {
		a.After()
	}
	return n, err
}
```

(Add the `notes` import to both files.)

- [ ] **Step 5: Translate the Describe text**

Append to each bundle (key verbatim, the English sentence):

`internal/i18n/lang/ja.toml`:
```toml
"Splits the note store into one file per worktree plus files for commits, previews and shelf entries. Nothing is lost: every note keeps its id, and the old file is kept as a backup." = "ノートストアを、ワークツリーごとのファイルと、コミット・プレビュー・シェルフ用のファイルに分割します。失われるものはありません。すべてのノートは ID を保持し、古いファイルはバックアップとして残ります。"
```
`internal/i18n/lang/ko.toml`:
```toml
"Splits the note store into one file per worktree plus files for commits, previews and shelf entries. Nothing is lost: every note keeps its id, and the old file is kept as a backup." = "노트 저장소를 워크트리별 파일과 커밋·프리뷰·보관함용 파일로 나눕니다. 잃는 것은 없습니다. 모든 노트는 ID를 유지하며, 이전 파일은 백업으로 보관됩니다."
```
`internal/i18n/lang/zh.toml`:
```toml
"Splits the note store into one file per worktree plus files for commits, previews and shelf entries. Nothing is lost: every note keeps its id, and the old file is kept as a backup." = "将笔记存储拆分为每个工作树一个文件，以及提交、预览和搁置区各自的文件。不会丢失任何内容：每条笔记都保留其 ID，旧文件会作为备份保留。"
```
`internal/i18n/lang/ru.toml`:
```toml
"Splits the note store into one file per worktree plus files for commits, previews and shelf entries. Nothing is lost: every note keeps its id, and the old file is kept as a backup." = "Разделяет хранилище заметок на отдельный файл для каждого рабочего дерева и файлы для коммитов, превью и полки. Ничего не теряется: каждая заметка сохраняет свой id, а старый файл остаётся резервной копией."
```

Place each line where the bundle keeps the previews Describe line (grep `Folds your saved merge previews`) if the bundle is sorted; otherwise append.

- [ ] **Step 6: Run domain, preflight, i18n and TUI gate tests**

Run: `cd /work/gigagit/.claude/worktrees/notes-partitions && go test ./internal/domain/ ./internal/preflight/ ./internal/i18n/ ./internal/tui/ -run 'Migrat|Preflight|Feature|I18n|Engine|Bundle|Notes'`
Expected: PASS. Then the full packages: `go test ./internal/domain/ ./internal/tui/`.

- [ ] **Step 7: Commit**

```bash
cd /work/gigagit/.claude/worktrees/notes-partitions
gg add internal/domain/features.go internal/domain/preflight.go internal/domain/migrationactions.go internal/domain/notesmigration_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
git commit -F <msgfile>   # "feat(domain): split-notes — a lossless migration of notes.toml into parts"
```

---

### Task 7: Docs and gates

**Files:**
- Modify: `CHANGELOG.md`, `docs/CLAUDE-details.md`, `CLAUDE.md` (the `notes` map row only)

- [ ] **Step 1: Update the docs**

`CLAUDE.md` — replace the `notes` row with:

```markdown
| `notes`      | Machine-local review-note store split in PARTS (`commits.toml`, `previews.toml`, `shelf.toml`, `worktrees/<key>.toml`; `PartOf` routes, a reply follows its root): `Load(part)`/`LoadAll`, per-part O_EXCL lock + cap + quarantine, lossless `ConvertLegacy` of the old `notes.toml`; records only. Owned by `domain`; frontends never import it. |
```

`docs/CLAUDE-details.md` — add a "Note store partitions" section: the routing table, reader→parts table (spec §5), reply routing via the parent, emptied files deleted, the deleted-worktree sweep rule, the `split-notes` migration (Silent, lossless, idempotent, backup name).

`CHANGELOG.md` — under the unreleased section: "Notes: the note store is split into one file per worktree plus commits, previews and shelf files; working-tree reads no longer parse every note in the repo, one corrupt file no longer disables every note, and notes of a removed worktree are swept. Existing notes are converted automatically (the old `notes.toml` is kept as `notes.toml.migrated-<unix>`)."

- [ ] **Step 2: Run the full staged gate**

Run: `cd /work/gigagit/.claude/worktrees/notes-partitions && ./test.sh 2>&1 | tee /tmp/claude-1000/-work-gigagit/16a75175-37a1-474a-b3b0-5c9fd449e5eb/scratchpad/test.log | tail -5`
Expected: the log ends with all green. Fix anything red before continuing.

- [ ] **Step 3: Commit the docs**

```bash
cd /work/gigagit/.claude/worktrees/notes-partitions
gg add CHANGELOG.md docs/CLAUDE-details.md CLAUDE.md
git commit -F <msgfile>   # "docs: note store partitions"
```

- [ ] **Step 4: Race gate**

Run: `cd /work/gigagit/.claude/worktrees/notes-partitions && ./test.sh race 2>&1 | tee /tmp/claude-1000/-work-gigagit/16a75175-37a1-474a-b3b0-5c9fd449e5eb/scratchpad/race.log | tail -5`
Expected: "all green" in the log (a `| tail` exit code proves nothing — read the log).

- [ ] **Step 5: Final whole-branch review**

Dispatch ONE read-only review subagent on the most capable model over `main..feat/notes-partitions` with the spec and this plan; fix Critical/Important findings test-first, then hand the branch to the user for merge (never merge without asking).
