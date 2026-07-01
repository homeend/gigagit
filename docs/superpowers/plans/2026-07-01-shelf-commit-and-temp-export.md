# Shelf-a-commit + copy-to-temp-dir Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user shelf a commit (its changed files, frozen durably) and copy any shelf entry or bookmark — file or commit — into a fixed `<repoRoot>.tmp/` sibling directory.

**Architecture:** Domain owns resolution (freeze a commit's changed files as a tar blob in the shelf; extract shelf/bookmark content into `[]model.ExportFile`). A new `engine.ExportToDir` op writes those files to absolute paths **outside** the worktree. TUI adds a "Shelf this commit" menu row and a `[t]` "Copy to temp dir" action (editable-destination popup) to the shelf `G` and bookmark `g` switchers; CLI adds `gg shelf commit` / `gg shelf export`.

**Tech Stack:** Go 1.26, Bubble Tea (TUI), `archive/tar` (stdlib) for extraction, the existing `gitcmd`/`gitexec` verb stack, `go-toml/v2` (shelf index).

## Global Constraints

- Module `github.com/homeend/gigagit`, Go 1.26.
- **A git verb is one invocation.** Build argv with `gitcmd`, run via `r.Runner.Run`/`.Stream`. Never shell out directly.
- **Frontends run operations via `domain.Execute`** and reads via domain queries. `internal/tui` and `internal/cli` **never import `internal/git`** (archtest-guarded).
- **A commit = its changed files only** (not the full tree). Content only — no message/author/parents preserved.
- **`ShelfEntry.Kind` is stored explicitly**, never inferred from an empty path.
- Temp base dir is **fixed**: `mainWorktreeTopLevel + ".tmp"` (anchored on the main worktree, first element of `svc.Worktrees(ctx)`).
- Tests use a real `git` in a `t.TempDir()` (`newRepo`/`newTestRepo` helpers) or `gitexec.FakeRunner`. Follow TDD.
- Run `./test.sh` (vet+gofmt → unit → e2e) before declaring done; gofmt is a hard gate.

---

### Task 1: model — `ShelfKind`, `ShelfEntry.Kind`, `ShelfEntry.IsCommit`, `model.ExportFile`

**Files:**
- Modify: `internal/model/model.go` (ShelfEntry struct at 246-254; add types near it)
- Test: `internal/model/model_test.go` (create if absent)

**Interfaces:**
- Produces:
  - `type ShelfKind int` with `ShelfKindFile ShelfKind = iota` (zero value) and `ShelfKindCommit`.
  - `ShelfEntry.Kind ShelfKind` field.
  - `func (e ShelfEntry) IsCommit() bool` → `e.Kind == ShelfKindCommit`.
  - `type ExportFile struct { RelPath string; Data []byte }` — the unit produced by domain and consumed by `engine.ExportToDir`.

- [ ] **Step 1: Write the failing test**

Create `internal/model/model_test.go` (or append):

```go
package model

import "testing"

func TestShelfEntryKind(t *testing.T) {
	var e ShelfEntry // zero value
	if e.Kind != ShelfKindFile {
		t.Fatalf("zero-value Kind = %v, want ShelfKindFile", e.Kind)
	}
	if e.IsCommit() {
		t.Fatal("zero-value entry must not be IsCommit")
	}
	e.Kind = ShelfKindCommit
	if !e.IsCommit() {
		t.Fatal("Kind=ShelfKindCommit must be IsCommit")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/model/ -run TestShelfEntryKind -v`
Expected: FAIL — `e.Kind undefined` / `ShelfKindFile undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/model/model.go`, add above the `ShelfEntry` struct:

```go
// ShelfKind distinguishes a shelf entry's blob payload. A file entry's blob is
// raw file bytes; a commit entry's blob is a tar archive of the commit's
// changed files (extracted on copy-out). The kind is stored, never inferred.
type ShelfKind int

const (
	ShelfKindFile   ShelfKind = iota // blob = raw file bytes (default)
	ShelfKindCommit                  // blob = tar of the commit's changed files
)
```

Add `Kind ShelfKind` to `ShelfEntry` (after `Origin`):

```go
type ShelfEntry struct {
	ID      string
	Bucket  string
	Kind    ShelfKind   // file (raw bytes) vs commit (tar archive)
	Origin  FileAddress
	SHA     string
	Size    int64
	Created time.Time
}
```

Add the method and the export type near the shelf types:

```go
// IsCommit reports whether the entry is a shelved commit (tar payload) rather
// than a single file (raw bytes).
func (e ShelfEntry) IsCommit() bool { return e.Kind == ShelfKindCommit }

// ExportFile is one file to write during a copy-to-temp-dir export: a
// repo-relative path plus its bytes. Produced by domain, consumed by
// engine.ExportToDir.
type ExportFile struct {
	RelPath string
	Data    []byte
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/model/ -run TestShelfEntryKind -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/model/model.go internal/model/model_test.go
git commit -m "model: add ShelfKind/ExportFile for shelved commits + temp export"
```

---

### Task 2: shelf store — `PutCommit`, persist `Kind`

**Files:**
- Modify: `internal/shelf/store.go` (Store interface at 27-33; add `MaxCommitArchiveBytes` const)
- Modify: `internal/shelf/file_store.go` (Put at 105-158; refactor blob write; add PutCommit)
- Test: `internal/shelf/file_store_test.go`

**Interfaces:**
- Consumes: `model.ShelfKind`, `model.ShelfEntry.Kind`, `model.ExportFile` (Task 1).
- Produces:
  - `Store.PutCommit(bucket string, addr model.FileAddress, tar []byte) (model.ShelfEntry, error)` — stores a `ShelfKindCommit` entry (id `commit-<shortsha>-<blobsha8>`), capped at `MaxCommitArchiveBytes`.
  - `Put` now sets `Kind: ShelfKindFile` on the entry it returns/persists.
  - `const MaxCommitArchiveBytes = 200 << 20` in `internal/shelf/store.go`.

- [ ] **Step 1: Write the failing test**

Append to `internal/shelf/file_store_test.go`:

```go
func TestPutCommitStoresArchiveKind(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	tar := []byte("PK-not-really-a-tar-but-bytes")
	addr := model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6", Path: ""}
	e, err := fs.PutCommit("", addr, tar)
	if err != nil {
		t.Fatalf("PutCommit: %v", err)
	}
	if e.Kind != model.ShelfKindCommit {
		t.Fatalf("Kind = %v, want ShelfKindCommit", e.Kind)
	}
	if e.ID != "commit-a1b2c3d-"+e.SHA[:8] {
		t.Fatalf("ID = %q, want commit-a1b2c3d-<sha8>", e.ID)
	}
	got, err := fs.Get(e.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(tar) {
		t.Fatalf("Get returned %q, want the stored tar", got)
	}
	// A plain file Put must report ShelfKindFile.
	fe, err := fs.Put("", model.FileAddress{State: model.StateUnstaged, Path: "x.txt"}, []byte("hi"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if fe.Kind != model.ShelfKindFile {
		t.Fatalf("file Put Kind = %v, want ShelfKindFile", fe.Kind)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/shelf/ -run TestPutCommitStoresArchiveKind -v`
Expected: FAIL — `fs.PutCommit undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/shelf/store.go` add the cap constant and the interface method:

```go
// MaxCommitArchiveBytes caps a shelved commit's changed-files tar. Larger than
// MaxShelfBytes because a commit may touch many files, but still bounded.
const MaxCommitArchiveBytes = 200 << 20
```

```go
type Store interface {
	Put(bucket string, addr model.FileAddress, data []byte) (model.ShelfEntry, error)
	PutCommit(bucket string, addr model.FileAddress, tar []byte) (model.ShelfEntry, error)
	Get(entryID string) ([]byte, error)
	List(bucket string, skip, limit int) ([]model.ShelfEntry, error)
	Buckets() ([]model.ShelfBucket, error)
	Remove(entryID string) error
}
```

In `internal/shelf/file_store.go`, refactor the blob write out of `Put` into a helper, and add `PutCommit`. Replace the body of `Put` (105-158) so it computes the sha, writes the blob via the helper, and builds a `ShelfKindFile` entry; add `writeBlob` and `PutCommit`:

```go
// writeBlob content-addresses data and stores it under root/blobs/<sha> (atomic,
// deduplicated). Returns the sha.
func (fs *FileStore) writeBlob(data []byte) (string, error) {
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if err := os.MkdirAll(filepath.Join(fs.root, "blobs"), 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(fs.blobPath(sha)); err == nil {
		return sha, nil // already present
	}
	tmp, err := os.CreateTemp(filepath.Join(fs.root, "blobs"), "blob-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return "", err
	}
	tmp.Close()
	if err := os.Rename(name, fs.blobPath(sha)); err != nil {
		os.Remove(name)
		return "", err
	}
	return sha, nil
}

// putEntry appends-or-replaces e in the index (idempotent by ID) and persists.
func (fs *FileStore) putEntry(e model.ShelfEntry) (model.ShelfEntry, error) {
	idx := fs.read()
	fs.ensureBucket(&idx, e.Bucket)
	replaced := false
	for i := range idx.Entries {
		if idx.Entries[i].ID == e.ID {
			idx.Entries[i] = e
			replaced = true
			break
		}
	}
	if !replaced {
		idx.Entries = append(idx.Entries, e)
	}
	return e, fs.write(idx)
}

func (fs *FileStore) Put(bucket string, addr model.FileAddress, data []byte) (model.ShelfEntry, error) {
	if len(data) > MaxShelfBytes {
		return model.ShelfEntry{}, ErrTooLarge
	}
	bucket = normalizeBucket(bucket)
	sha, err := fs.writeBlob(data)
	if err != nil {
		return model.ShelfEntry{}, err
	}
	return fs.putEntry(model.ShelfEntry{
		ID:      fmt.Sprintf("%s-%s-%s", idSource(addr), slug(addr.Path), sha[:8]),
		Bucket:  bucket,
		Kind:    model.ShelfKindFile,
		Origin:  addr,
		SHA:     sha,
		Size:    int64(len(data)),
		Created: time.Now(),
	})
}

// PutCommit stores a commit's changed-files tar as a durable ShelfKindCommit
// entry (id: commit-<shortsha>-<blobsha8>).
func (fs *FileStore) PutCommit(bucket string, addr model.FileAddress, tar []byte) (model.ShelfEntry, error) {
	if len(tar) > MaxCommitArchiveBytes {
		return model.ShelfEntry{}, ErrTooLarge
	}
	bucket = normalizeBucket(bucket)
	sha, err := fs.writeBlob(tar)
	if err != nil {
		return model.ShelfEntry{}, err
	}
	short := addr.Commit
	if len(short) > 7 {
		short = short[:7]
	}
	return fs.putEntry(model.ShelfEntry{
		ID:      fmt.Sprintf("commit-%s-%s", short, sha[:8]),
		Bucket:  bucket,
		Kind:    model.ShelfKindCommit,
		Origin:  addr,
		SHA:     sha,
		Size:    int64(len(tar)),
		Created: time.Now(),
	})
}
```

(Delete the now-inlined blob-writing and entry-append code from the old `Put`; `ensureBucket`, `read`, `write`, `blobPath` stay.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/shelf/ -v`
Expected: PASS (new test + existing store tests).

- [ ] **Step 5: Commit**

```bash
git add internal/shelf/store.go internal/shelf/file_store.go internal/shelf/file_store_test.go
git commit -m "shelf: PutCommit stores a commit's changed-files tar (ShelfKindCommit)"
```

---

### Task 3: git verb — `ArchiveFiles`

**Files:**
- Create: `internal/git/archive.go`
- Test: `internal/git/archive_test.go`

**Interfaces:**
- Produces: `func (r *Repo) ArchiveFiles(ctx context.Context, rev string, paths []string) ([]byte, error)` — runs `git archive --format=tar <rev> -- <paths...>`, returns the raw tar bytes. Empty `paths` → returns an error (never archive the whole tree).

- [ ] **Step 1: Write the failing test**

Create `internal/git/archive_test.go`:

```go
package git

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"testing"
)

func TestArchiveFilesSubset(t *testing.T) {
	repo := newTestRepo(t) // real git repo helper used elsewhere in this package
	repo.writeFile(t, "keep.txt", "keep me\n")
	repo.writeFile(t, "drop.txt", "ignore me\n")
	repo.git(t, "add", ".")
	repo.git(t, "commit", "-m", "two files")
	head := repo.head(t)

	data, err := repo.repo.ArchiveFiles(context.Background(), head, []string{"keep.txt"})
	if err != nil {
		t.Fatalf("ArchiveFiles: %v", err)
	}
	names := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		b, _ := io.ReadAll(tr)
		names[h.Name] = string(b)
	}
	if names["keep.txt"] != "keep me\n" {
		t.Fatalf("keep.txt = %q, want %q", names["keep.txt"], "keep me\n")
	}
	if _, ok := names["drop.txt"]; ok {
		t.Fatal("drop.txt should not be in a keep.txt-only archive")
	}
}

func TestArchiveFilesEmptyPathsErrors(t *testing.T) {
	repo := newTestRepo(t)
	if _, err := repo.repo.ArchiveFiles(context.Background(), "HEAD", nil); err == nil {
		t.Fatal("empty paths must error, not archive the whole tree")
	}
}
```

> NOTE: match the real test helpers in `internal/git` (e.g. `newTestRepo`,
> `writeFile`, `git`, `head`, `.repo`). Inspect an existing `internal/git/*_test.go`
> and adapt the helper calls; the assertions above are the contract.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestArchiveFiles -v`
Expected: FAIL — `repo.ArchiveFiles undefined`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/git/archive.go`:

```go
package git

import (
	"context"
	"errors"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// ArchiveFiles returns a tar archive of the given repo-relative paths as they
// exist at rev (`git archive --format=tar <rev> -- <paths>`). One invocation.
// It refuses an empty path list so a caller can never accidentally archive the
// whole tree (a monorepo hazard). The tar bytes are captured raw; converting
// gitexec's Result.Stdout (a bytes.Buffer.String()) back to []byte is
// byte-preserving.
func (r *Repo) ArchiveFiles(ctx context.Context, rev string, paths []string) ([]byte, error) {
	if len(paths) == 0 {
		return nil, errors.New("git archive: no paths")
	}
	b := gitcmd.New("archive").Arg("--format=tar", rev, "--")
	for _, p := range paths {
		b = b.Arg(p)
	}
	res, err := r.Runner.Run(ctx, "git archive (changed files)", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return []byte(res.Stdout), nil
}
```

> Verify `gitcmd.Builder.Arg` returns the builder (fluent) — the existing verbs
> chain `.Arg(...)`. If `Arg` is variadic-and-fluent, `b = b.Arg(p)` works; if the
> builder is a value type reassigned differently, mirror `CommitFiles` in `log.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ -run TestArchiveFiles -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/archive.go internal/git/archive_test.go
git commit -m "git: ArchiveFiles verb (tar of changed files at a rev)"
```

---

### Task 4: domain — `ShelfAddCommit`, `TempExportBase`, `ExportShelfEntry`, `ExportBookmark`, tar extraction

**Files:**
- Create: `internal/domain/export.go`
- Modify: `internal/domain/shelf.go` (add `ShelfAddCommit`)
- Test: `internal/domain/export_test.go`

**Interfaces:**
- Consumes: `Store.PutCommit` (Task 2), `git.Repo.ArchiveFiles` (Task 3), existing `s.repo` (concrete `*git.Repo`), `s.CommitFiles`/`s.repo.CommitFiles`, `s.Worktrees`, `s.ShelfBlob`, `s.BookmarkBytes`, `s.ShelfList`, `model.ExportFile`.
- Produces:
  - `func (s *Service) ShelfAddCommit(ctx context.Context, sha string) (model.ShelfEntry, error)`
  - `func (s *Service) TempExportBase(ctx context.Context) (string, error)` → `mainWorktreeTop + ".tmp"`.
  - `func (s *Service) ExportShelfEntry(ctx context.Context, e model.ShelfEntry) (files []model.ExportFile, name string, err error)`
  - `func (s *Service) ExportBookmark(ctx context.Context, b model.Bookmark) (files []model.ExportFile, name string, err error)`
  - unexported: `commitChangedPaths(ctx, sha) ([]string, error)`, `extractTar([]byte) ([]model.ExportFile, error)`, `sanitizeName(string) string`.

**How `s.repo` git verbs are reached:** domain queries already call git verbs on `s.repo` under a reservation (see `s.CommitFiles` / `ShowFile` in `query.go`). Follow the existing gated pattern — resolve changed paths and archive under a Read reservation, mirroring how `ShelfAdd` calls `ResolveBytes`.

- [ ] **Step 1: Write the failing test**

Create `internal/domain/export_test.go`:

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShelfAddCommitAndExportRoundTrip(t *testing.T) {
	svc, repoDir := newServiceRepo(t) // domain test helper: Service on a real repo
	ctx := context.Background()

	writeCommit(t, repoDir, "src/a.txt", "alpha\n", "add a")
	sha := headSHA(t, repoDir)
	writeCommit(t, repoDir, "src/a.txt", "alpha2\n", "edit a") // move HEAD past sha

	e, err := svc.ShelfAddCommit(ctx, sha)
	if err != nil {
		t.Fatalf("ShelfAddCommit: %v", err)
	}
	if !e.IsCommit() {
		t.Fatal("entry must be IsCommit")
	}

	files, name, err := svc.ExportShelfEntry(ctx, e)
	if err != nil {
		t.Fatalf("ExportShelfEntry: %v", err)
	}
	if !strings.HasPrefix(name, "commit-") {
		t.Fatalf("name = %q, want commit-<sha> prefix", name)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.RelPath] = string(f.Data)
	}
	if got["src/a.txt"] != "alpha\n" {
		t.Fatalf("exported src/a.txt = %q, want alpha\\n (content AT the commit)", got["src/a.txt"])
	}

	// Durability: the export reads the stored tar, so gc'ing the commit must not
	// break it. Prune it out of git, then export again.
	runGit(t, repoDir, "reflog", "expire", "--expire=all", "--all")
	runGit(t, repoDir, "gc", "--prune=now")
	files2, _, err := svc.ExportShelfEntry(ctx, e)
	if err != nil {
		t.Fatalf("ExportShelfEntry after gc: %v", err)
	}
	if len(files2) != len(files) {
		t.Fatalf("after gc got %d files, want %d (durable)", len(files2), len(files))
	}
}

func TestTempExportBaseIsSiblingDotTmp(t *testing.T) {
	svc, repoDir := newServiceRepo(t)
	base, err := svc.TempExportBase(context.Background())
	if err != nil {
		t.Fatalf("TempExportBase: %v", err)
	}
	want := filepath.Clean(repoDir) + ".tmp"
	if filepath.Clean(base) != want {
		t.Fatalf("base = %q, want %q", base, want)
	}
	_ = os.Stat // keep import if helper unused
}
```

> NOTE: reuse the domain package's existing real-git test helpers (grep
> `internal/domain/*_test.go` for the Service+repo constructor, commit, and head
> helpers) instead of `newServiceRepo`/`writeCommit`/`headSHA`/`runGit`/`runGit`
> literally — adapt names, keep the assertions.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run 'TestShelfAddCommit|TestTempExportBase' -v`
Expected: FAIL — `svc.ShelfAddCommit undefined`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/domain/shelf.go`:

```go
// ShelfAddCommit freezes commit sha's changed files into a durable, path-less
// ShelfKindCommit entry: it archives just the paths the commit touched (content
// AT sha) so the entry restores even after the commit leaves git. Content only —
// no message/author/parents.
func (s *Service) ShelfAddCommit(ctx context.Context, sha string) (model.ShelfEntry, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return model.ShelfEntry{}, ErrShelfDisabled
	}
	paths, err := s.commitChangedPaths(ctx, sha)
	if err != nil {
		return model.ShelfEntry{}, err
	}
	if len(paths) == 0 {
		return model.ShelfEntry{}, fmt.Errorf("shelf: commit %s changes no files", sha)
	}
	tar, err := s.archiveFiles(ctx, sha, paths)
	if err != nil {
		return model.ShelfEntry{}, err
	}
	addr := model.FileAddress{State: model.StateCommitted, Commit: sha, Path: ""}
	return st.PutCommit("", addr, tar)
}
```

(Add `"fmt"` to `shelf.go` imports.)

Create `internal/domain/export.go`:

```go
package domain

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// TempExportBase is the fixed sibling directory copy-to-temp-dir writes under:
// the MAIN worktree root plus ".tmp" (e.g. /a/x/repo -> /a/x/repo.tmp), anchored
// on the main worktree so it is the repo's sibling even from a linked worktree.
func (s *Service) TempExportBase(ctx context.Context) (string, error) {
	wts, err := s.Worktrees(ctx)
	if err != nil {
		return "", err
	}
	if len(wts) == 0 || wts[0].Path == "" {
		return "", fmt.Errorf("temp export: no main worktree")
	}
	return filepath.Clean(wts[0].Path) + ".tmp", nil
}

// commitChangedPaths returns the repo-relative paths a commit adds or modifies
// (deletions are dropped: they have no content at the commit to archive). Renames
// and copies contribute their NEW path (CommitFile.Path).
func (s *Service) commitChangedPaths(ctx context.Context, sha string) ([]string, error) {
	files, err := s.commitFiles(ctx, sha) // gated CommitFiles read (see below)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		if f.Status == "D" { // deleted at this commit: no content to archive
			continue
		}
		if f.Path != "" {
			out = append(out, f.Path)
		}
	}
	return out, nil
}

// extractTar unpacks a tar archive into ExportFiles (regular files only).
func extractTar(data []byte) ([]model.ExportFile, error) {
	tr := tar.NewReader(bytes.NewReader(data))
	var out []model.ExportFile
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			continue
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		out = append(out, model.ExportFile{RelPath: filepath.Clean(h.Name), Data: b})
	}
	return out, nil
}

// ExportShelfEntry resolves a shelf entry into the files to write plus the
// default target subdir name. A commit entry extracts its stored tar (durable,
// no git); a file entry is a single ExportFile at its origin path.
func (s *Service) ExportShelfEntry(ctx context.Context, e model.ShelfEntry) ([]model.ExportFile, string, error) {
	if e.IsCommit() {
		blob, err := s.ShelfBlob(ctx, e.ID)
		if err != nil {
			return nil, "", err
		}
		files, err := extractTar(blob)
		if err != nil {
			return nil, "", err
		}
		return files, commitDirName(e.Origin.Commit), nil
	}
	data, err := s.ShelfBlob(ctx, e.ID)
	if err != nil {
		return nil, "", err
	}
	return []model.ExportFile{{RelPath: e.Origin.Path, Data: data}}, sanitizeName(e.ID), nil
}

// ExportBookmark resolves a bookmark into files + default subdir name. A commit
// bookmark archives the commit's changed files live (bookmarks are
// live-by-address); a file bookmark is one ExportFile.
func (s *Service) ExportBookmark(ctx context.Context, b model.Bookmark) ([]model.ExportFile, string, error) {
	if b.IsCommit() {
		paths, err := s.commitChangedPaths(ctx, b.Commit)
		if err != nil {
			return nil, "", err
		}
		if len(paths) == 0 {
			return nil, "", fmt.Errorf("export: commit %s changes no files", b.Commit)
		}
		tar, err := s.archiveFiles(ctx, b.Commit, paths)
		if err != nil {
			return nil, "", err
		}
		files, err := extractTar(tar)
		if err != nil {
			return nil, "", err
		}
		return files, commitDirName(b.Commit), nil
	}
	data, err := s.BookmarkBytes(ctx, b)
	if err != nil {
		return nil, "", err
	}
	name := "bookmark-" + sanitizeName(firstNonEmpty(b.Label, b.ID))
	return []model.ExportFile{{RelPath: b.Path, Data: data}}, name, nil
}

func commitDirName(sha string) string {
	if len(sha) > 7 {
		sha = sha[:7]
	}
	return "commit-" + sha
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// sanitizeName reduces a label/id to a safe single path segment.
func sanitizeName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, s)
	s = strings.Trim(s, "-")
	if s == "" {
		return "unshelf"
	}
	return s
}
```

Add the two gated git-verb wrappers domain needs. In `internal/domain/query.go` (near the other gated reads such as `CommitFiles`/`ShowFile`), add:

```go
// commitFiles is the gated changed-files read used by commit export.
func (s *Service) commitFiles(ctx context.Context, sha string) ([]model.CommitFile, error) {
	// If a public Service.CommitFiles already exists, call it and delete this
	// wrapper. Otherwise mirror an existing gated read (Read reservation) that
	// calls s.repo.CommitFiles(ctx, sha).
	return s.repo.CommitFiles(ctx, sha)
}

// archiveFiles is the gated tar-of-changed-files read used by commit export.
func (s *Service) archiveFiles(ctx context.Context, rev string, paths []string) ([]byte, error) {
	return s.repo.ArchiveFiles(ctx, rev, paths)
}
```

> IMPORTANT: match how other domain reads acquire a reservation. Grep
> `internal/domain/query.go` for `CommitFiles(` / `ShowFile(` and wrap
> `commitFiles`/`archiveFiles` with the SAME `gateFor(ctx).Acquire(ctx,
> repogate.Read, …)` boilerplate they use (and record failures via the standard
> seam). If `Service.CommitFiles` is already public, reuse it and drop the
> wrapper. Do not call `s.repo` ungated.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/ -run 'TestShelfAddCommit|TestTempExportBase' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/export.go internal/domain/shelf.go internal/domain/query.go internal/domain/export_test.go
git commit -m "domain: ShelfAddCommit + temp-export resolution (base dir, extract, per-source files)"
```

---

### Task 5: engine — `ExportToDir` op (writes outside the worktree)

**Files:**
- Create: `internal/engine/export_to_dir.go`
- Test: `internal/engine/export_to_dir_test.go`

**Interfaces:**
- Consumes: `model.ExportFile` (Task 1), `OpDeps`, `Progress`, `Done`, `DecisionRequest`, `repogate.Read`.
- Produces: `type ExportToDir struct { Dir string; Files []model.ExportFile }` implementing `Operation`; `func (op ExportToDir) LockMode() repogate.Mode { return repogate.Read }`. Existing-`Dir` → `overwrite`/`cancel` decision (reuses the same option words as `WriteFile`). `ErrExportCancelled`.

- [ ] **Step 1: Write the failing test**

Create `internal/engine/export_to_dir_test.go`:

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestExportToDirWritesNestedFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "commit-abc1234")
	op := ExportToDir{
		Dir: dir,
		Files: []model.ExportFile{
			{RelPath: "src/a.txt", Data: []byte("alpha\n")},
			{RelPath: "b.txt", Data: []byte("bee\n")},
		},
	}
	res, err := op.Run(context.Background(), OpDeps{}) // no decider: dir is absent
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Changed {
		t.Fatal("expected Changed")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "src", "a.txt")); string(got) != "alpha\n" {
		t.Fatalf("src/a.txt = %q, want alpha\\n", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(got) != "bee\n" {
		t.Fatalf("b.txt = %q", got)
	}
}

func TestExportToDirExistingDirCancels(t *testing.T) {
	dir := t.TempDir() // already exists
	op := ExportToDir{Dir: dir, Files: []model.ExportFile{{RelPath: "x", Data: []byte("y")}}}
	dec := deciderFunc(func(ctx context.Context, r DecisionRequest) (DecisionResponse, error) {
		return DecisionResponse{Option: "cancel"}, nil
	})
	if _, err := op.Run(context.Background(), OpDeps{Decider: dec}); err != ErrExportCancelled {
		t.Fatalf("err = %v, want ErrExportCancelled", err)
	}
}
```

> NOTE: `deciderFunc` — reuse the engine test package's existing decider stub
> (grep `internal/engine/*_test.go`); adapt the name if different.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestExportToDir -v`
Expected: FAIL — `ExportToDir undefined`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/engine/export_to_dir.go`:

```go
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repogate"
)

// ExportToDir writes Files under Dir at absolute paths OUTSIDE the working tree
// (the copy-to-temp-dir primitive). Each file's RelPath is joined onto Dir with
// parent dirs created. If Dir already exists it asks the Decider to overwrite or
// cancel. Read reservation: it touches neither git refs nor the working tree.
type ExportToDir struct {
	Dir   string
	Files []model.ExportFile
}

var _ Operation = ExportToDir{}

// ErrExportCancelled is returned when the user declines to overwrite an existing
// target directory.
var ErrExportCancelled = errors.New("export cancelled")

func (op ExportToDir) LockMode() repogate.Mode { return repogate.Read }

func (op ExportToDir) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if _, err := os.Stat(op.Dir); err == nil {
		choice, derr := deps.decide(ctx, DecisionRequest{
			ID:      "overwrite",
			Prompt:  "Directory exists: " + op.Dir,
			Options: []string{writeOverwrite, writeCancel},
		})
		if derr != nil {
			return Result{}, derr
		}
		if choice.Option != writeOverwrite {
			return Result{}, ErrExportCancelled
		}
	}
	for i, f := range op.Files {
		full := filepath.Join(op.Dir, filepath.Clean(f.RelPath))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return Result{}, err
		}
		if err := os.WriteFile(full, f.Data, 0o644); err != nil {
			return Result{}, err
		}
		deps.emit(ctx, Progress{Message: fmt.Sprintf("wrote %s (%d/%d)", f.RelPath, i+1, len(op.Files))})
	}
	res := Result{
		Summary: fmt.Sprintf("exported %d file(s) to %s", len(op.Files), op.Dir),
		Changed: true,
		Path:    op.Dir,
	}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

> Verify the `Progress` field name — `internal/engine/event.go:12` defines it.
> If the field is not `Message`, match the real field (e.g. `Line`/`Text`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run TestExportToDir -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/export_to_dir.go internal/engine/export_to_dir_test.go
git commit -m "engine: ExportToDir op (write files to an out-of-worktree dir)"
```

---

### Task 6: TUI — "Shelf this commit" menu row

**Files:**
- Modify: `internal/tui/shelf.go` (add `shelfAddCommitCmd`, `commitShelfRow`, `reflogShelfRow`)
- Modify: `internal/tui/action_menu.go` (register in `appendCommitContextRows` after `commitBookmarkRow`; and after `reflogBookmarkRow` at ~150)
- Modify: `internal/tui/help.go:84`
- Test: `internal/tui/shelf_commit_test.go`

**Interfaces:**
- Consumes: `svc.ShelfAddCommit` (Task 4), existing `shelfAddedMsg`, `m.commitBookmarkRow` pattern (`bookmark.go:105`), `m.backingIndex(panelCommits)`, `m.commits`, `m.reflog`.
- Produces: `commitShelfRow`/`reflogShelfRow` `(actionRow, bool)` and `shelfAddCommitCmd(sha string) tea.Cmd`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/shelf_commit_test.go` (mirror `bookmark_commit_test.go`):

```go
package tui

import "testing"

func TestCommitShelfRowPresentOnCommitsPanel(t *testing.T) {
	m := newCommitsTestModel(t) // reuse the helper bookmark_commit_test.go uses
	m.focus = panelCommits
	r, ok := m.commitShelfRow()
	if !ok {
		t.Fatal("commitShelfRow should be available on the Commits panel")
	}
	if r.label != "Shelf this commit" {
		t.Fatalf("label = %q", r.label)
	}
}
```

> NOTE: reuse whatever constructor `bookmark_commit_test.go` uses to get a Model
> focused on the Commits panel with a selected commit; adapt `newCommitsTestModel`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestCommitShelfRow -v`
Expected: FAIL — `m.commitShelfRow undefined`.

- [ ] **Step 3: Write minimal implementation**

In `internal/tui/shelf.go`:

```go
// shelfAddCommitCmd freezes commit sha's changed files into the shelf off the UI
// thread (reuses shelfAddedMsg).
func (m Model) shelfAddCommitCmd(sha string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		e, err := svc.ShelfAddCommit(context.Background(), sha)
		return shelfAddedMsg{entry: e, err: err}
	}
}

// commitShelfRow offers "Shelf this commit" on the Commits panel — freeze the
// selected commit's changed files durably. Mirrors commitBookmarkRow.
func (m Model) commitShelfRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	sha := m.commits[bi].Hash
	return actionRow{
		id:    "commit-shelf",
		label: "Shelf this commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.shelfAddCommitCmd(sha)
		},
	}, true
}

// reflogShelfRow is the reflog-panel variant.
func (m Model) reflogShelfRow() (actionRow, bool) {
	if m.focus != panelReflog || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelReflog)
	if !ok {
		return actionRow{}, false
	}
	sha := m.reflog[bi].Hash
	return actionRow{
		id:    "reflog-shelf",
		label: "Shelf this commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.shelfAddCommitCmd(sha)
		},
	}, true
}
```

In `internal/tui/action_menu.go`, after the `commitBookmarkRow` block (208-210):

```go
	if r, ok := m.commitShelfRow(); ok {
		out = append(out, r)
	}
```

And after the `reflogBookmarkRow` block (~150):

```go
	if r, ok := m.reflogShelfRow(); ok {
		rows = append(rows, r)
	}
```

In `internal/tui/help.go:84`, extend the line:

```go
		r(".", "Copy SHA / Bookmark this commit / Shelf this commit (compare via the g switcher)"),
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestCommitShelfRow -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/shelf.go internal/tui/action_menu.go internal/tui/help.go internal/tui/shelf_commit_test.go
git commit -m "tui: Shelf this commit row on Commits/Reflog panels"
```

---

### Task 7: TUI — `[t]` Copy to temp dir (shelf + bookmark switchers)

**Files:**
- Create: `internal/tui/temp_export.go` (resolve cmd, msg, prefilled popup)
- Modify: `internal/tui/shelf_popup.go` (add `case "t"` at ~256; hint at 118)
- Modify: `internal/tui/bookmark_popup.go` (add `case "t"`; hint at 146)
- Modify: `internal/tui/help.go` (switcher help lines, if listed)
- Test: `internal/tui/temp_export_test.go`

**Interfaces:**
- Consumes: `svc.ExportShelfEntry`/`svc.ExportBookmark`/`svc.TempExportBase` (Task 4), `engine.ExportToDir` (Task 5), `m.startOp`, `m.pushLayer`/`m.popLayer`, `viewField`, `m.overlayDims`, textfield type used by `bookmarkPastePopup.dest`.
- Produces:
  - `type tempExportPopup struct { dest <textfield>; files []model.ExportFile }` with `update`/`render`.
  - `tempExportResolvedMsg struct { dir string; files []model.ExportFile; err error }`.
  - `func (m Model) startTempExportShelf(e model.ShelfEntry) (Model, tea.Cmd)` and `startTempExportBookmark(b model.Bookmark)` — dispatch the resolve cmd.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/temp_export_test.go`:

```go
package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestTempExportPopupEnterStartsOp(t *testing.T) {
	p := &tempExportPopup{files: []model.ExportFile{{RelPath: "a.txt", Data: []byte("x")}}}
	p.dest = newPathField("/tmp/repo.tmp/commit-abc1234") // reuse the field ctor the paste popup uses
	m := newPopupTestModel(t)                             // minimal Model with svc + layers
	_, cmd := p.update(m, keyEnter())                     // helpers used elsewhere in tui tests
	if cmd == nil {
		t.Fatal("enter with files + dest should start the export op")
	}
}

func TestTempExportResolvedMsgOpensPrefilledPopup(t *testing.T) {
	m := newPopupTestModel(t)
	m2, _ := m.update(tempExportResolvedMsg{
		dir:   "/tmp/repo.tmp/commit-abc1234",
		files: []model.ExportFile{{RelPath: "a.txt", Data: []byte("x")}},
	})
	if _, ok := frontLayer(m2.(Model)).(*tempExportPopup); !ok {
		t.Fatal("resolved msg should push a tempExportPopup")
	}
}
```

> NOTE: adapt `newPathField`, `newPopupTestModel`, `keyEnter`, `frontLayer` to the
> real helpers/ctors in `internal/tui` (grep `bookmark_popup*_test.go` and the
> paste popup for the textfield constructor). Keep the two assertions.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestTempExport -v`
Expected: FAIL — `tempExportPopup undefined`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tui/temp_export.go`:

```go
package tui

import (
	"context"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// tempExportResolvedMsg carries the resolved export payload back to the UI
// thread so the prefilled destination popup can open.
type tempExportResolvedMsg struct {
	dir   string
	files []model.ExportFile
	err   error
}

// startTempExportShelf resolves a shelf entry's files + target dir off-thread,
// then (via tempExportResolvedMsg) opens the editable destination popup.
func (m Model) startTempExportShelf(e model.ShelfEntry) (Model, tea.Cmd) {
	svc := m.svc
	return m, func() tea.Msg {
		ctx := context.Background()
		base, err := svc.TempExportBase(ctx)
		if err != nil {
			return tempExportResolvedMsg{err: err}
		}
		files, name, err := svc.ExportShelfEntry(ctx, e)
		if err != nil {
			return tempExportResolvedMsg{err: err}
		}
		return tempExportResolvedMsg{dir: filepath.Join(base, name), files: files}
	}
}

// startTempExportBookmark is the bookmark variant.
func (m Model) startTempExportBookmark(b model.Bookmark) (Model, tea.Cmd) {
	svc := m.svc
	return m, func() tea.Msg {
		ctx := context.Background()
		base, err := svc.TempExportBase(ctx)
		if err != nil {
			return tempExportResolvedMsg{err: err}
		}
		files, name, err := svc.ExportBookmark(ctx, b)
		if err != nil {
			return tempExportResolvedMsg{err: err}
		}
		return tempExportResolvedMsg{dir: filepath.Join(base, name), files: files}
	}
}

// tempExportPopup is the editable-destination confirmation before writing to the
// temp dir. dest is prefilled with <base>/<name>; enter runs engine.ExportToDir.
type tempExportPopup struct {
	dest  editField // same textfield type as bookmarkPastePopup.dest
	files []model.ExportFile
}

func (p *tempExportPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
	case tea.KeyEnter:
		dir := strings.TrimSpace(p.dest.Value())
		if dir == "" || len(p.files) == 0 {
			return m, nil
		}
		files := p.files
		m = m.popLayer() // switcher stays visible during the write
		return m.startOp(engine.ExportToDir{Dir: dir, Files: files})
	default:
		p.dest.HandleEditKey(msg)
	}
	return m, nil
}

func (p *tempExportPopup) render(m Model, below string) string {
	w, _ := m.overlayDims()
	var b strings.Builder
	b.WriteString("Copy to temp dir\n\n")
	b.WriteString(viewField("dir: ", p.dest, true, popupContentWidth(w)) + "\n\n")
	b.WriteString("[type] dir  [enter] write  [esc] cancel")
	return b.String()
}
```

> `editField`/`newPathField`/`viewField`/`popupContentWidth` — use the exact
> textfield type and constructor `bookmarkPastePopup` uses (grep
> `bookmark_popup.go` for its `dest` field type and where it is initialized). Make
> `tempExportPopup` satisfy the same popup/layer interface the paste popup does
> (match its method set — `update`/`render` signatures and any `overlay`
> marker method).

Add the resolved-msg handler to the main `Update` switch (where other `…Msg`
cases live, e.g. next to `shelfAddedMsg`):

```go
	case tempExportResolvedMsg:
		if msg.err != nil {
			return m.withStatusErr(msg.err), nil // use the model's error-status helper
		}
		p := &tempExportPopup{files: msg.files}
		p.dest = newPathField(msg.dir) // prefilled, editable
		return m.pushLayer(p)
```

In `internal/tui/shelf_popup.go`, add a `case "t"` in the runes switch (after `"e"`, ~267):

```go
			case "t":
				if p.compareRef != nil {
					return m, nil
				}
				e, ok := p.selected()
				if !ok {
					return m, nil
				}
				return m.startTempExportShelf(e)
```

And extend the hint (line 118) to include `"[t] temp dir"`:

```go
	hint := []string{"[?] keys", "[enter] diff", "[e] editor", "[p] restore", "[t] temp dir", "[m] mark/compare", "[x] remove", "[c] vs bookmark", "[/] filter", "[z] mode", "[esc] close"}
```

In `internal/tui/bookmark_popup.go`, add the mirrored `case "t"` → `m.startTempExportBookmark(b)` (resolve the selected bookmark the same way that popup's other actions do), and add `"[t] temp dir"` to its hint (line 146).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestTempExport -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/temp_export.go internal/tui/shelf_popup.go internal/tui/bookmark_popup.go internal/tui/help.go internal/tui/temp_export_test.go
git commit -m "tui: [t] Copy to temp dir on shelf + bookmark switchers"
```

---

### Task 8: CLI — `gg shelf commit <sha>` and `gg shelf export <entry-id> [--dir]`

**Files:**
- Modify: `internal/cli/shelf.go` (cmdShelf switch at 18-37; add subcommands)
- Test: `internal/cli/shelf_test.go`

**Interfaces:**
- Consumes: `svc.ShelfAddCommit`, `svc.ExportShelfEntry`, `svc.TempExportBase`, `svc.ShelfList` (to resolve an entry id), `engine.ExportToDir` run via `svc.Execute` with a `cliDecider`.
- Produces: `shelfCommit(...)` and `shelfExport(...)` returning an int exit code; wired into `cmdShelf`.

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/shelf_test.go` (mirror the existing shelf CLI test harness — real repo, in-process CLI run):

```go
func TestShelfCommitThenExport(t *testing.T) {
	env := newShelfCLIEnv(t) // reuse the harness the other shelf_test cases use
	env.writeCommit("src/a.txt", "alpha\n", "add a")
	sha := env.head()

	if code := env.run("shelf", "commit", sha); code != 0 {
		t.Fatalf("shelf commit exit=%d", code)
	}
	id := env.firstShelfID(t) // list + take the newest entry id

	dir := filepath.Join(t.TempDir(), "out")
	if code := env.run("shelf", "export", id, "--dir", dir); code != 0 {
		t.Fatalf("shelf export exit=%d", code)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "src", "a.txt")); string(got) != "alpha\n" {
		t.Fatalf("exported a.txt = %q, want alpha\\n", got)
	}
}
```

> NOTE: adapt `newShelfCLIEnv`/`writeCommit`/`head`/`run`/`firstShelfID` to the
> real CLI test helpers in `internal/cli` (grep `shelf_test.go`,
> `bookmark_test.go`). Keep the flow: commit → shelf commit → shelf export --dir.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestShelfCommitThenExport -v`
Expected: FAIL — unknown subcommand `commit`.

- [ ] **Step 3: Write minimal implementation**

In `internal/cli/shelf.go`, extend the `cmdShelf` switch:

```go
	case "commit":
		return shelfCommit(svc, rest, stdout, stderr)
	case "export":
		return shelfExport(svc, rest, stdin, stdout, stderr)
```

Add the two functions (mirror `shelfAdd`/`shelfRestore` for flag parsing and the
`svc.Execute` + `cliDecider` pattern — grep `shelfRestore` for how it runs
`engine.WriteFile`):

```go
func shelfCommit(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("shelf commit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg shelf commit <sha>")
		return 2
	}
	e, err := svc.ShelfAddCommit(context.Background(), fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "shelf commit: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "shelved commit as %s\n", e.ID)
	return 0
}

func shelfExport(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("shelf export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "target directory (default: <repo>.tmp/<name>)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg shelf export <entry-id> [--dir <path>]")
		return 2
	}
	ctx := context.Background()
	e, ok := shelfEntryByID(svc, ctx, fs.Arg(0)) // list + match id (see helper below)
	if !ok {
		fmt.Fprintf(stderr, "shelf export: no entry %q\n", fs.Arg(0))
		return 1
	}
	files, name, err := svc.ExportShelfEntry(ctx, e)
	if err != nil {
		fmt.Fprintf(stderr, "shelf export: %v\n", err)
		return 1
	}
	target := *dir
	if target == "" {
		base, err := svc.TempExportBase(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "shelf export: %v\n", err)
			return 1
		}
		target = filepath.Join(base, name)
	}
	dec := newCLIDecider(stdin, stdout) // reuse the shelf/bookmark CLI decider ctor
	_, err = svc.Execute(ctx, engine.ExportToDir{Dir: target, Files: files}, nil, dec)
	if err != nil {
		fmt.Fprintf(stderr, "shelf export: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "exported %d file(s) to %s\n", len(files), target)
	return 0
}

// shelfEntryByID scans shelf pages for an entry id (default bucket).
func shelfEntryByID(svc *domain.Service, ctx context.Context, id string) (model.ShelfEntry, bool) {
	for skip := 0; ; skip += 100 {
		page, err := svc.ShelfList(ctx, "", skip, 100)
		if err != nil || len(page) == 0 {
			return model.ShelfEntry{}, false
		}
		for _, e := range page {
			if e.ID == id {
				return e, true
			}
		}
		if len(page) < 100 {
			return model.ShelfEntry{}, false
		}
	}
}
```

> Add imports (`flag`, `context`, `path/filepath`, `engine`, `model`) as needed.
> Use the SAME decider constructor the existing shelf/bookmark CLI commands use
> (grep for `Decider` in `internal/cli/*.go`); with no terminal it must default
> the overwrite decision to `cancel`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestShelfCommitThenExport -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/shelf.go internal/cli/shelf_test.go
git commit -m "cli: gg shelf commit + gg shelf export"
```

---

### Task 9: Docs + full test sweep

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`
- Modify: `internal/agentskill/using-gg.md` + bump `internal/agentskill` version marker
- Then: `gg init --update` (refresh installed agent skill copies)

- [ ] **Step 1: Update CHANGELOG.md**

Add an entry describing: shelf a commit (durable changed-files snapshot), copy shelf/bookmark files & commits to `<repo>.tmp/`, `[t]` switcher action + "Shelf this commit" menu row, `gg shelf commit`/`gg shelf export`.

- [ ] **Step 2: Update README.md**

Document the `[t]` Copy-to-temp-dir action, "Shelf this commit", the fixed `<repo>.tmp/` layout and per-type subdir names, and the two new CLI subcommands.

- [ ] **Step 3: Update CLAUDE.md**

Extend the `shelf`, `bookmark`, and `engine` package-map rows: shelf now stores commit archives (`ShelfKindCommit`, `PutCommit`); `engine.ExportToDir` is the first out-of-worktree writer; `domain.TempExportBase`/`ExportShelfEntry`/`ExportBookmark`.

- [ ] **Step 4: Update the agent skill**

Add `gg shelf commit`/`gg shelf export` to `internal/agentskill/using-gg.md`; bump `agentskill.Version` (grep the version const). After building, run `./gg init --update`.

- [ ] **Step 5: Full test sweep**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit + e2e tests PASS.

Then build and smoke-test the binary (absolute path per convention):

```bash
go build -o ./gg ./cmd/gg
/mnt/t/others/gigagit/.claude/worktrees/shelf-commit-temp-export/gg --version
```

- [ ] **Step 6: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md internal/agentskill/
git commit -m "docs: shelf-a-commit + copy-to-temp-dir (CHANGELOG/README/CLAUDE/agentskill)"
```

---

## Self-Review

**Spec coverage:**
- Shelf a commit → Tasks 1-4 (model Kind, store PutCommit, ArchiveFiles verb, ShelfAddCommit) + Task 6 (menu) + Task 8 (CLI). ✓
- Durable (restore without git) → changed-files tar stored in blob; Task 4 test gc's the commit and re-exports. ✓
- Copy files/commits to temp dir → Task 5 (ExportToDir) + Task 7 (`[t]` popup) + Task 8 (`export`). ✓
- Fixed base `<repo>.tmp` anchored on main worktree → `TempExportBase` (Task 4) + test. ✓
- Per-type subdir names (`commit-<sha>`, entry id, `bookmark-<label>`, `unshelf-…`) → `commitDirName`/`sanitizeName`/`ExportShelfEntry`/`ExportBookmark` (Task 4). ✓
- Editable destination popup → `tempExportPopup` prefilled (Task 7). ✓
- Available in shelf + bookmark context menus → Task 7 (both switchers). ✓

**Type consistency:** `model.ShelfKind`/`ShelfKindFile`/`ShelfKindCommit`, `ShelfEntry.Kind`, `ShelfEntry.IsCommit()`, `model.ExportFile{RelPath,Data}`, `Store.PutCommit`, `git.Repo.ArchiveFiles`, `Service.ShelfAddCommit`/`TempExportBase`/`ExportShelfEntry`/`ExportBookmark`, `engine.ExportToDir{Dir,Files}`, `ErrExportCancelled`, `tempExportResolvedMsg{dir,files,err}`, `tempExportPopup{dest,files}` — used consistently across tasks. ✓

**Adapt-to-real-helpers flags:** Tasks 3/4/6/7/8 call out that test helpers and a few TUI/CLI constructors (textfield type, decider ctor, gated-read boilerplate, `Progress` field name) must be matched to the real code by grepping the neighboring file — the assertions and public signatures are the fixed contract.
