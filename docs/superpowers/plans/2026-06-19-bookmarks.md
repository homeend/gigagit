# Bookmarks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A persistent registry of richly-addressed file **bookmarks** — jump to / compare / paste a file from anywhere in the git world (commit, branch, staged, unstaged, another worktree, a shelf entry) — where identity is the full *address* and content is fetched by *blob SHA* (permanent) or *live by address* (working/index).

**Architecture:** A pure `internal/bookmark` record store (TOML, no blobs) parallel to `internal/shelf`, owned by `internal/domain`, which adds the commands plus a `BookmarkBytes` resolver that routes on the bookmark's state. Three tiny git verbs (`CatFileBlob`, `ShowFileInDir`, `BlobSHA`) back resolution; compare and paste reuse the already-shipped `domain.Differ` and `engine.WriteFile`. TUI adds a `.`-menu capture (per focused surface) and a keyed quick-switcher popup; CLI adds `gg bookmark`.

**Tech Stack:** Go 1.26, `github.com/pelletier/go-toml/v2` (already a dep), Bubble Tea TUI, the existing `gitcmd`/`gitexec`/`repogate`/`domain`/`engine` packages and the shipped `internal/shelf` as the structural reference.

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26. **No new third-party dependencies.**
- Cross-platform (no CGO); resolve state paths the way `internal/repos.DefaultStatePath` / the shelf store do.
- `internal/tui` and `internal/cli` must NOT import `internal/git` or `internal/bookmark` — go through `internal/domain` (archtest-guarded).
- A git verb is one invocation built with `gitcmd` and run via `r.Runner.Run`.
- Bookmark state is machine-local under `$XDG_STATE/gg/bookmark/<repo-key>/bookmarks.toml`, keyed by git common dir; a missing state dir disables bookmarks gracefully (`ErrBookmarksDisabled`).
- Identity is the **address** (`State + Worktree + Branch + Commit + ShelfID + Path`), not the SHA. SHA is the content determinator, set only for permanent states (committed/shelf).
- Single term **bookmark**; the write-to-working-tree verb is **paste** with a **mandatory** destination (required CLI positional; empty TUI popup).
- New CLI command registered in BOTH the `Run` switch AND `var commands` in `internal/cli/cli.go`.
- Every new TUI keybinding appears in BOTH `help.go` and the context-help footer.
- TDD: failing test → red → minimal impl → green → commit. Run `./test.sh` (and `./test.sh race` before merge).
- **Reference implementation:** `internal/shelf/*`, `internal/domain/shelf*.go`, `internal/cli/shelf.go`, `internal/tui/shelf*.go` are the shipped parallel feature — mirror their structure.

---

### Task 1: `model` types + `internal/bookmark` record store

**Files:**
- Modify: `internal/model/model.go` (add `BookmarkState`, `Bookmark`)
- Create: `internal/bookmark/store.go` (interface + errors)
- Create: `internal/bookmark/file_store.go` (toml impl + address-derived ID)
- Create: `internal/bookmark/file_store_test.go`

**Interfaces:**
- Produces:
  - `model.BookmarkState` consts `StateCommitted, StateShelf, StateStaged, StateUnstaged, StateUntracked`.
  - `model.Bookmark{Worktree, Branch, Commit, ShelfID, Path string; State BookmarkState; SHA string; ID string; Label string; Created time.Time}`.
  - `bookmark.ErrNotFound`.
  - `bookmark.Store` interface: `Add(model.Bookmark) (model.Bookmark, error)`, `Get(id string) (model.Bookmark, error)`, `List(skip, limit int) ([]model.Bookmark, error)`, `Remove(id string) error`.
  - `bookmark.NewFileStore(root string) *FileStore`.
  - `bookmark.AddressID(b model.Bookmark) string` (exported for domain to dedup before Add if needed).

- [ ] **Step 1: Add the model types**

In `internal/model/model.go` (which already imports `time` after the shelf feature), append:

```go
// BookmarkState is where in its git lifecycle a bookmarked file was taken from.
type BookmarkState int

const (
	StateCommitted BookmarkState = iota // a commit/branch file (permanent → SHA)
	StateShelf                          // a shelf entry (permanent → SHA)
	StateStaged                         // a worktree's index file (live)
	StateUnstaged                       // a worktree's working file, tracked-modified (live)
	StateUntracked                      // a worktree's working file, new (live)
)

// String renders the state word used in a bookmark's display string.
func (s BookmarkState) String() string {
	switch s {
	case StateCommitted:
		return "commit"
	case StateShelf:
		return "shelf"
	case StateStaged:
		return "staged"
	case StateUntracked:
		return "untracked"
	default:
		return "unstaged"
	}
}

// Bookmark is a richly-addressed reference to a file. The address fields are the
// identity and the display; SHA is the content determinator for permanent states
// (committed/shelf) only — "" means fetch live by the address.
type Bookmark struct {
	Worktree string // worktree top-level (staged/unstaged/untracked); "" otherwise
	Branch   string // branch name when known; "" otherwise
	Commit   string // commit sha (committed); "" otherwise
	ShelfID  string // shelf entry id (shelf); "" otherwise
	Path     string // path within the tree/worktree
	State    BookmarkState
	SHA      string // blob checksum; set ⇔ permanent
	ID       string // derived from the address
	Label    string // human label; defaults to the display string
	Created  time.Time
}
```

- [ ] **Step 2: Write the failing store test**

Create `internal/bookmark/file_store_test.go` (mirror `internal/shelf/file_store_test.go`):

```go
package bookmark

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func newStore(t *testing.T) *FileStore { t.Helper(); return NewFileStore(t.TempDir()) }

func committed(path, sha string) model.Bookmark {
	return model.Bookmark{State: model.StateCommitted, Commit: "c0ffee", Path: path, SHA: sha}
}

func TestAddGetRoundTrip(t *testing.T) {
	s := newStore(t)
	b, err := s.Add(committed("a/b.go", "deadbeef"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if b.ID == "" {
		t.Fatalf("Add did not assign an ID: %+v", b)
	}
	got, err := s.Get(b.ID)
	if err != nil || got.Path != "a/b.go" || got.SHA != "deadbeef" {
		t.Fatalf("Get = %+v err %v", got, err)
	}
}

func TestIDFromAddressNotSHA(t *testing.T) {
	s := newStore(t)
	// Same content (SHA) but different paths → different bookmarks.
	b1, _ := s.Add(committed("x/.gitignore", "empty"))
	b2, _ := s.Add(committed("y/.gitignore", "empty"))
	if b1.ID == b2.ID {
		t.Fatalf("same SHA at different paths must be different bookmarks")
	}
	// Same address re-added → idempotent (same ID, one entry).
	b3, _ := s.Add(committed("x/.gitignore", "empty"))
	if b3.ID != b1.ID {
		t.Fatalf("same address must be idempotent: %s vs %s", b3.ID, b1.ID)
	}
	list, _ := s.List(0, 0)
	if len(list) != 2 {
		t.Fatalf("expected 2 distinct bookmarks, got %d", len(list))
	}
}

func TestListPagingAndRemove(t *testing.T) {
	s := newStore(t)
	for _, p := range []string{"a", "b", "c"} {
		if _, err := s.Add(committed(p, "s"+p)); err != nil {
			t.Fatal(err)
		}
	}
	page, _ := s.List(0, 2)
	if len(page) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page))
	}
	rest, _ := s.List(2, 2)
	if len(rest) != 1 {
		t.Fatalf("page2 len = %d, want 1", len(rest))
	}
	if err := s.Remove(page[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(page[0].ID); err != ErrNotFound {
		t.Fatalf("removed Get err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 3: Run — expect compile failure**

Run: `go test ./internal/bookmark/ ./internal/model/ 2>&1 | head`
Expected: build error (`undefined: FileStore/NewFileStore/ErrNotFound`); model compiles.

- [ ] **Step 4: Write the interface + errors**

Create `internal/bookmark/store.go`:

```go
// Package bookmark is gigagit's persistent registry of richly-addressed file
// bookmarks: a record store of pointers (no byte content). The Store interface
// is the fixed API; the file-backed implementation is swappable.
package bookmark

import (
	"errors"

	"github.com/gigagit/gg/internal/model"
)

// ErrNotFound is returned by Get/Remove for an unknown id.
var ErrNotFound = errors.New("bookmark: not found")

// Store persists bookmark records. Safe for sequential use by one process;
// cross-process writes are last-writer-wins (atomic index rewrite).
type Store interface {
	Add(b model.Bookmark) (model.Bookmark, error)
	Get(id string) (model.Bookmark, error)
	List(skip, limit int) ([]model.Bookmark, error)
	Remove(id string) error
}
```

- [ ] **Step 5: Implement the file store**

Create `internal/bookmark/file_store.go` (mirror `internal/shelf/file_store.go`'s atomic-rewrite `read`/`write`):

```go
package bookmark

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/gigagit/gg/internal/model"
)

// FileStore keeps an atomic-rewrite TOML registry under root/bookmarks.toml.
type FileStore struct{ root string }

func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

type index struct {
	Bookmarks []model.Bookmark `toml:"bookmarks"`
}

func (fs *FileStore) path() string { return filepath.Join(fs.root, "bookmarks.toml") }

func (fs *FileStore) read() index {
	var idx index
	data, err := os.ReadFile(fs.path())
	if err != nil {
		return idx
	}
	if err := toml.Unmarshal(data, &idx); err != nil {
		return index{}
	}
	return idx
}

func (fs *FileStore) write(idx index) error {
	if err := os.MkdirAll(fs.root, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(idx)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "bookmarks-*.toml")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, fs.path()); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

var slugRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func slug(s string) string {
	return strings.Trim(strings.ToLower(slugRe.ReplaceAllString(s, "-")), "-")
}

// AddressID derives a stable id from the ADDRESS (not the SHA), so identical
// content at different places is distinct and the same place is idempotent.
func AddressID(b model.Bookmark) string {
	key := fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s\x00%s",
		b.State, b.Worktree, b.Branch, b.Commit, b.ShelfID, b.Path)
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%s-%s-%s", b.State.String(), slug(b.Path), hex.EncodeToString(sum[:])[:8])
}

func (fs *FileStore) Add(b model.Bookmark) (model.Bookmark, error) {
	b.ID = AddressID(b)
	if b.Created.IsZero() {
		b.Created = time.Now()
	}
	idx := fs.read()
	for i := range idx.Bookmarks {
		if idx.Bookmarks[i].ID == b.ID { // same address → idempotent replace
			idx.Bookmarks[i] = b
			return b, fs.write(idx)
		}
	}
	idx.Bookmarks = append(idx.Bookmarks, b)
	return b, fs.write(idx)
}

func (fs *FileStore) Get(id string) (model.Bookmark, error) {
	for _, b := range fs.read().Bookmarks {
		if b.ID == id {
			return b, nil
		}
	}
	return model.Bookmark{}, ErrNotFound
}

func (fs *FileStore) List(skip, limit int) ([]model.Bookmark, error) {
	bs := fs.read().Bookmarks
	sort.SliceStable(bs, func(a, b int) bool { return bs[a].Created.After(bs[b].Created) })
	if skip >= len(bs) {
		return nil, nil
	}
	end := skip + limit
	if limit <= 0 || end > len(bs) {
		end = len(bs)
	}
	return bs[skip:end], nil
}

func (fs *FileStore) Remove(id string) error {
	idx := fs.read()
	kept := idx.Bookmarks[:0]
	found := false
	for _, b := range idx.Bookmarks {
		if b.ID == id {
			found = true
			continue
		}
		kept = append(kept, b)
	}
	if !found {
		return ErrNotFound
	}
	idx.Bookmarks = kept
	return fs.write(idx)
}
```

- [ ] **Step 6: Run — expect green**

Run: `go test ./internal/bookmark/ ./internal/model/`
Expected: PASS. `gofmt -w` the new files.

- [ ] **Step 7: Commit**

```bash
git add internal/model/model.go internal/bookmark/
git commit -m "feat(bookmark): record store + model types (address-derived id)"
```

---

### Task 2: git verbs — `CatFileBlob`, `ShowFileInDir`, `BlobSHA`

**Files:**
- Create: `internal/git/bookmark_verbs.go`
- Create: `internal/git/bookmark_verbs_test.go`

**Interfaces:**
- Produces on `*git.Repo`:
  - `CatFileBlob(ctx context.Context, sha string) ([]byte, error)`
  - `ShowFileInDir(ctx context.Context, dir, rev, path string) ([]byte, error)`
  - `BlobSHA(ctx context.Context, rev, path string) (string, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/git/bookmark_verbs_test.go` (use the existing `newTestRepo` helper; for `ShowFileInDir` add a linked worktree with `git worktree add` and stage differing content there):

```go
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlobSHAAndCatFile(t *testing.T) {
	r := newTestRepo(t) // real git temp repo with an initial commit (README "hi\n")
	ctx := context.Background()
	sha, err := r.BlobSHA(ctx, "HEAD", "README.md")
	if err != nil || sha == "" {
		t.Fatalf("BlobSHA: %q err %v", sha, err)
	}
	data, err := r.CatFileBlob(ctx, sha)
	if err != nil {
		t.Fatalf("CatFileBlob: %v", err)
	}
	if string(data) != "hi\n" {
		t.Fatalf("blob = %q, want hi", data)
	}
}

func TestShowFileInDirReadsLinkedWorktreeIndex(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	top, _ := r.TopLevel(ctx)
	wt := filepath.Join(t.TempDir(), "wt")
	run := func(dir string, args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(top, "worktree", "add", "-b", "wtbr", wt)
	// Stage differing content in the linked worktree, leave the working file newer.
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("staged\n"), 0o644)
	run(wt, "add", "README.md")
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("working\n"), 0o644)

	staged, err := r.ShowFileInDir(ctx, wt, "", "README.md") // git -C wt show :README.md
	if err != nil {
		t.Fatalf("ShowFileInDir: %v", err)
	}
	if strings.TrimRight(string(staged), "\n") != "staged" {
		t.Fatalf("staged side = %q, want staged", staged)
	}
}
```

- [ ] **Step 2: Run — expect failure**

Run: `go test ./internal/git/ -run 'TestBlobSHA|TestShowFileInDir' 2>&1 | head`
Expected: FAIL (undefined methods).

- [ ] **Step 3: Implement the verbs**

Create `internal/git/bookmark_verbs.go`:

```go
package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// CatFileBlob returns a blob's raw bytes by its object id (`git cat-file blob
// <sha>`). Used to resolve a bookmark to permanent (committed/shelf) content.
func (r *Repo) CatFileBlob(ctx context.Context, sha string) ([]byte, error) {
	argv := gitcmd.New("cat-file").Arg("blob", sha).ToArgv()
	res, err := r.Runner.Run(ctx, "git cat-file blob", argv)
	if err != nil {
		return nil, err
	}
	return []byte(res.Stdout), nil
}

// ShowFileInDir runs `git -C <dir> show <rev>:<path>`, reading path at rev in the
// repo rooted at dir — used to read another worktree's index (rev == "" →
// `:path`). The -C global overrides cwd, so the Service's own workdir is
// irrelevant.
func (r *Repo) ShowFileInDir(ctx context.Context, dir, rev, path string) ([]byte, error) {
	argv := gitcmd.New("-C").Arg(dir, "show", rev+":"+path).ToArgv()
	res, err := r.Runner.Run(ctx, "git -C show", argv)
	if err != nil {
		return nil, err
	}
	return []byte(res.Stdout), nil
}

// BlobSHA resolves the blob object id of path at rev (`git rev-parse
// <rev>:<path>`), captured when bookmarking a committed file.
func (r *Repo) BlobSHA(ctx context.Context, rev, path string) (string, error) {
	argv := gitcmd.New("rev-parse").Arg(rev + ":" + path).ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-parse blob", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
```

- [ ] **Step 4: Run — expect green**

Run: `go test ./internal/git/ -run 'TestBlobSHA|TestShowFileInDir'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/bookmark_verbs.go internal/git/bookmark_verbs_test.go
git commit -m "feat(git): CatFileBlob / ShowFileInDir / BlobSHA verbs for bookmarks"
```

---

### Task 3: domain — commands, store wiring, `BookmarkBytes` resolver

**Files:**
- Create: `internal/domain/bookmark.go`
- Create: `internal/domain/bookmarkstore.go`
- Create: `internal/domain/bookmark_test.go`
- Modify: `internal/domain/service.go` (add a `bookmark bookmark.Store` field + import)

**Interfaces:**
- Consumes: `bookmark.Store`/`bookmark.NewFileStore` (Task 1); `repo.CatFileBlob/ShowFileInDir/BlobSHA` (Task 2); `s.shelfStore(ctx)` + `repoKey` (shipped shelf); the `query`/Read pattern.
- Produces:
  - `var BookmarkStatePath string`; `func (s *Service) SetBookmarkStore(bookmark.Store)`.
  - `func (s *Service) BookmarkAdd(ctx, b model.Bookmark) (model.Bookmark, error)` — fills `SHA` for committed (`BlobSHA`), stores.
  - `func (s *Service) BookmarkList(ctx, skip, limit int) ([]model.Bookmark, error)`
  - `func (s *Service) BookmarkGet(ctx, id string) (model.Bookmark, error)`
  - `func (s *Service) BookmarkRemove(ctx, id string) error`
  - `func (s *Service) BookmarkBytes(ctx, b model.Bookmark) ([]byte, error)`
  - `var ErrBookmarksDisabled error`

- [ ] **Step 1: Add the store field + wiring**

In `internal/domain/service.go`, add the import `"github.com/gigagit/gg/internal/bookmark"` and a struct field next to `shelf`:

```go
	bookmark bookmark.Store // lazily resolved; nil disables bookmarks
```

Create `internal/domain/bookmarkstore.go` (mirror `shelfstore.go`; **reuse the existing `repoKey`**, do not redefine it):

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gigagit/gg/internal/bookmark"
)

// BookmarkStatePath overrides the bookmark root dir. "" uses the default XDG
// location. cmd/gg leaves it ""; tests point it at a temp dir.
var BookmarkStatePath string

// SetBookmarkStore injects a store (tests).
func (s *Service) SetBookmarkStore(st bookmark.Store) {
	s.mu.Lock()
	s.bookmark = st
	s.mu.Unlock()
}

func (s *Service) bookmarkStore(ctx context.Context) bookmark.Store {
	s.mu.Lock()
	if s.bookmark != nil {
		st := s.bookmark
		s.mu.Unlock()
		return st
	}
	s.mu.Unlock()

	root := BookmarkStatePath
	if root == "" {
		base := bookmarkBaseDir()
		if base == "" {
			return nil
		}
		key := "unknown"
		if cd, err := s.GitCommonDir(ctx); err == nil {
			key = repoKey(strings.TrimSpace(cd)) // reuse shelfstore.go's repoKey
		}
		root = filepath.Join(base, key)
	}
	st := bookmark.NewFileStore(root)
	s.mu.Lock()
	s.bookmark = st
	s.mu.Unlock()
	return st
}

func bookmarkBaseDir() string {
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "bookmark")
		}
	}
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "bookmark")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "bookmark")
}
```

- [ ] **Step 2: Write the failing commands+resolver test**

Create `internal/domain/bookmark_test.go`:

```go
package domain

import (
	"context"
	"testing"

	"github.com/gigagit/gg/internal/bookmark"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

func bmSvc(t *testing.T) (*Service, *gitexec.FakeRunner) {
	t.Helper()
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	svc.SetBookmarkStore(bookmark.NewFileStore(t.TempDir()))
	return svc, f
}

func TestBookmarkAddFillsCommittedSHA(t *testing.T) {
	svc, f := bmSvc(t)
	f.SetResponse("git rev-parse blob", gitexec.Result{Stdout: "abc123sha\n"})
	b, err := svc.BookmarkAdd(context.Background(), model.Bookmark{
		State: model.StateCommitted, Commit: "c0ffee", Path: "a/b.go",
	})
	if err != nil {
		t.Fatalf("BookmarkAdd: %v", err)
	}
	if b.SHA != "abc123sha" {
		t.Fatalf("committed SHA not filled via BlobSHA: %+v", b)
	}
	if b.ID == "" {
		t.Fatalf("no id assigned")
	}
}

func TestBookmarkBytesCommittedUsesCatFile(t *testing.T) {
	svc, f := bmSvc(t)
	f.SetResponse("git cat-file blob", gitexec.Result{Stdout: "frozen\n"})
	got, err := svc.BookmarkBytes(context.Background(), model.Bookmark{
		State: model.StateCommitted, SHA: "abc123", Path: "a/b.go",
	})
	if err != nil || string(got) != "frozen\n" {
		t.Fatalf("BookmarkBytes committed = %q err %v", got, err)
	}
	if !sawArg(f, "git cat-file blob", "abc123") {
		t.Fatalf("expected cat-file of abc123, calls: %+v", f.Calls)
	}
}

func TestBookmarkBytesStagedUsesShowInDir(t *testing.T) {
	svc, f := bmSvc(t)
	f.SetResponse("git -C show", gitexec.Result{Stdout: "idx\n"})
	if _, err := svc.BookmarkBytes(context.Background(), model.Bookmark{
		State: model.StateStaged, Worktree: "/wt", Path: "a/b.go",
	}); err != nil {
		t.Fatal(err)
	}
	if !sawArg(f, "git -C show", ":a/b.go") || !sawArg(f, "git -C show", "/wt") {
		t.Fatalf("expected `git -C /wt show :a/b.go`, calls: %+v", f.Calls)
	}
}

func TestBookmarkListAndRemove(t *testing.T) {
	svc, _ := bmSvc(t)
	b, _ := svc.BookmarkAdd(context.Background(), model.Bookmark{
		State: model.StateUnstaged, Worktree: "/wt", Path: "p.go",
	})
	if list, _ := svc.BookmarkList(context.Background(), 0, 10); len(list) != 1 {
		t.Fatalf("list len = %d", len(list))
	}
	if err := svc.BookmarkRemove(context.Background(), b.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.BookmarkList(context.Background(), 0, 10); len(list) != 0 {
		t.Fatalf("after remove len = %d", len(list))
	}
}
```

> `sawArg` already exists in `internal/domain/fileref_test.go` (same package).

- [ ] **Step 3: Run — expect failure**

Run: `go test ./internal/domain/ -run TestBookmark 2>&1 | head`
Expected: FAIL (`BookmarkAdd`/`BookmarkBytes` undefined).

- [ ] **Step 4: Implement commands + resolver**

Create `internal/domain/bookmark.go`:

```go
package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/gigagit/gg/internal/model"
)

// ErrBookmarksDisabled means no state directory was resolvable.
var ErrBookmarksDisabled = errors.New("bookmark: no state directory available")

// BookmarkAdd stores a bookmark, filling SHA for permanent states: a committed
// file gets its blob sha via BlobSHA; a shelf bookmark carries the entry's SHA
// already. The store derives the address ID.
func (s *Service) BookmarkAdd(ctx context.Context, b model.Bookmark) (model.Bookmark, error) {
	st := s.bookmarkStore(ctx)
	if st == nil {
		return model.Bookmark{}, ErrBookmarksDisabled
	}
	if b.State == model.StateCommitted && b.SHA == "" {
		sha, err := s.repo.BlobSHA(ctx, b.Commit, b.Path)
		if err != nil {
			return model.Bookmark{}, err
		}
		b.SHA = sha
	}
	return st.Add(b)
}

func (s *Service) BookmarkList(ctx context.Context, skip, limit int) ([]model.Bookmark, error) {
	st := s.bookmarkStore(ctx)
	if st == nil {
		return nil, ErrBookmarksDisabled
	}
	return st.List(skip, limit)
}

func (s *Service) BookmarkGet(ctx context.Context, id string) (model.Bookmark, error) {
	st := s.bookmarkStore(ctx)
	if st == nil {
		return model.Bookmark{}, ErrBookmarksDisabled
	}
	return st.Get(id)
}

func (s *Service) BookmarkRemove(ctx context.Context, id string) error {
	st := s.bookmarkStore(ctx)
	if st == nil {
		return ErrBookmarksDisabled
	}
	return st.Remove(id)
}

// BookmarkBytes resolves a bookmark to bytes, routing on state: permanent →
// the blob (cat-file / shelf store); live → the named worktree's index or
// working file.
func (s *Service) BookmarkBytes(ctx context.Context, b model.Bookmark) ([]byte, error) {
	switch b.State {
	case model.StateCommitted:
		return s.repo.CatFileBlob(ctx, b.SHA)
	case model.StateShelf:
		return s.ShelfBlob(ctx, b.ShelfID)
	case model.StateStaged:
		return s.repo.ShowFileInDir(ctx, b.Worktree, "", b.Path)
	case model.StateUnstaged, model.StateUntracked:
		return os.ReadFile(filepath.Join(b.Worktree, filepath.FromSlash(b.Path)))
	default:
		return nil, errors.New("bookmark: unknown state")
	}
}
```

> Reads here touch the repo directly via `s.repo` (like `ShowFile`'s verb); a follow-up could wrap them in the Read `query` reservation, but resolution is a read and the shipped `ShowFile`/`WorktreeFile` set the precedent. Keep it direct for v1.

- [ ] **Step 5: Run — expect green**

Run: `go test ./internal/domain/ -run TestBookmark`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/bookmark.go internal/domain/bookmarkstore.go internal/domain/bookmark_test.go internal/domain/service.go
git commit -m "feat(domain): bookmark commands + BookmarkBytes resolver + per-repo store"
```

---

### Task 4: CLI `gg bookmark {add,list,rm,paste}` + e2e

**Files:**
- Create: `internal/cli/bookmark.go`
- Create: `internal/cli/bookmark_test.go`
- Modify: `internal/cli/cli.go` (switch case + `commands` map)
- Create: `e2e/scenarios/s44_bookmark_live.toml`

**Interfaces:**
- Consumes: `domain.BookmarkAdd/List/Get/Remove/BookmarkBytes` (Task 3); `engine.WriteFile` via `runOperation` + a policy `cliDecider` (see `internal/cli/shelf.go`'s `shelfRestore`); `model.Bookmark`/state consts; `svc.TopLevel(ctx)` for the default worktree path.
- Produces: `func cmdBookmark(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int`.

- [ ] **Step 1: Register the command (both places)**

In `internal/cli/cli.go`, add after `case "shelf":`:

```go
	case "bookmark":
		return cmdBookmark(svc, rest, stdin, stdout, stderr)
```

and to `var commands`: add `"bookmark": true,`.

- [ ] **Step 2: Write the failing CLI test**

Create `internal/cli/bookmark_test.go` (mirror `internal/cli/shelf_test.go`; `shelfRepo`-style hermetic helper but its own name to avoid collision):

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func bookmarkRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	return newRepoDir(t)
}

func TestBookmarkLivePointerRoundTrip(t *testing.T) {
	dir := bookmarkRepo(t)
	// README.md committed as "hi\n"; make a working edit.
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("v1\n"), 0o644)

	code, out, errb := runCLI(t, dir, "bookmark", "add", "README.md")
	if code != 0 {
		t.Fatalf("add exit %d: %s", code, errb)
	}
	id := strings.TrimSpace(out)

	// Edit AGAIN after bookmarking — a live pointer must reflect the new bytes.
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644)

	code, _, errb = runCLI(t, dir, "bookmark", "paste", id, "out.txt")
	if code != 0 {
		t.Fatalf("paste exit %d: %s", code, errb)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "out.txt")); string(got) != "v2\n" {
		t.Fatalf("pasted = %q, want v2 (live)", got)
	}
}

func TestBookmarkPasteRequiresDest(t *testing.T) {
	dir := bookmarkRepo(t)
	if code, _, _ := runCLI(t, dir, "bookmark", "paste", "some-id"); code != 2 {
		t.Fatalf("missing dest should exit 2, got %d", code)
	}
}

func TestBookmarkUsageErrors(t *testing.T) {
	dir := bookmarkRepo(t)
	if code, _, _ := runCLI(t, dir, "bookmark"); code != 2 {
		t.Fatalf("bare bookmark should exit 2, got %d", code)
	}
	if code, _, _ := runCLI(t, dir, "bookmark", "add"); code != 2 {
		t.Fatalf("add without paths should exit 2, got %d", code)
	}
}
```

- [ ] **Step 3: Run — expect failure**

Run: `go test ./internal/cli/ -run TestBookmark 2>&1 | head`
Expected: FAIL (`cmdBookmark` undefined).

- [ ] **Step 4: Implement `cmdBookmark`**

Create `internal/cli/bookmark.go` (model `paste` on `shelfRestore` in `internal/cli/shelf.go`):

```go
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

func cmdBookmark(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg bookmark <add|list|rm|paste> ...")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return bookmarkAdd(svc, rest, stdout, stderr)
	case "list":
		return bookmarkList(svc, rest, stdout, stderr)
	case "rm":
		return bookmarkRemove(svc, rest, stdout, stderr)
	case "paste":
		return bookmarkPaste(svc, rest, stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "bookmark: unknown subcommand %q\n", sub)
		return 2
	}
}

func bookmarkAdd(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bookmark add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rev := fs.String("rev", "", "bookmark a committed file at this commit/branch")
	staged := fs.Bool("staged", false, "bookmark the index (staged) side")
	wt := fs.String("worktree", "", "worktree top-level to target (default: this repo)")
	label := fs.String("label", "", "human label (default: the display string)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	paths := fs.Args()
	if len(paths) == 0 {
		fmt.Fprintln(stderr, "usage: gg bookmark add [--rev <commit>] [--staged] [--worktree <path>] [--label <l>] <path>...")
		return 2
	}
	if *rev != "" && (*staged || *wt != "") {
		fmt.Fprintln(stderr, "bookmark add: --rev is mutually exclusive with --staged/--worktree")
		return 2
	}
	ctx := context.Background()
	worktree := *wt
	if *rev == "" && worktree == "" {
		top, err := svc.TopLevel(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		worktree = top
	}
	for _, p := range paths {
		b := model.Bookmark{Path: p, Label: *label}
		switch {
		case *rev != "":
			b.State, b.Commit = model.StateCommitted, *rev
		case *staged:
			b.State, b.Worktree = model.StateStaged, worktree
		default:
			b.State, b.Worktree = model.StateUnstaged, worktree
		}
		stored, err := svc.BookmarkAdd(ctx, b)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprintln(stdout, stored.ID)
	}
	return 0
}

func bookmarkList(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if err := flag.NewFlagSet("bookmark list", flag.ContinueOnError).Parse(args); err != nil {
		return 2
	}
	bs, err := svc.BookmarkList(context.Background(), 0, 0)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, b := range bs {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", b.ID, b.State.String(), b.Path)
	}
	return 0
}

func bookmarkRemove(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bookmark rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg bookmark rm <id>")
		return 2
	}
	if err := svc.BookmarkRemove(context.Background(), fs.Arg(0)); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func bookmarkPaste(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bookmark paste", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "overwrite an existing destination")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: gg bookmark paste [--force] <id> <dest>")
		return 2
	}
	id, dest := fs.Arg(0), fs.Arg(1)
	bm, err := svc.BookmarkGet(context.Background(), id)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	blob, err := svc.BookmarkBytes(context.Background(), bm)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	policy := map[string]string{"overwrite": "cancel"}
	if *force {
		policy["overwrite"] = "overwrite"
	}
	res, err := runOperation(context.Background(), svc,
		engine.WriteFile{Path: dest, Data: blob}, cliDecider{policy: policy}, stderr)
	if errors.Is(err, engine.ErrWriteCancelled) {
		fmt.Fprintf(stderr, "bookmark paste: %s already exists; pass --force to overwrite\n", dest)
		return 2
	}
	return finish(res, err, stdout, stderr)
}
```

- [ ] **Step 5: Run — expect green**

Run: `go test ./internal/cli/ -run TestBookmark`
Expected: PASS.

- [ ] **Step 6: e2e scenario**

Create `e2e/scenarios/s44_bookmark_live.toml` (the e2e `TestMain` already isolates `XDG_STATE_HOME`, shipped with the shelf). The bookmark id is deterministic from the address; compute the slug + sha8 of the address key and hardcode (mirror how `s43_shelf_roundtrip.toml` hardcodes the id). To keep it robust, prefer the **discard-then-paste** structure used by s43, but for the LIVE proof you instead edit between add and paste:

```toml
name = "bookmark paste: a live pointer reflects edits after bookmarking"

[input]
steps = [
  { write = "README.md", content = "hi\n" },
  { commit = "initial" },
  { write = "README.md", content = "v1\n" },   # working edit, then bookmark it
]

[[run]]
cmd  = ["bookmark", "add", "README.md"]
exit = 0

# Edit AGAIN after bookmarking — the live pointer must see the new bytes.
[[run]]
cmd  = ["write-file"]   # if the harness lacks a write step between runs, see note
exit = 0
```

> **Harness note:** if `e2e` `[[run]]` steps cannot edit a file between commands (only `gg` commands run), achieve the same proof by **two bookmarks**: add the working bookmark, then run a `gg` command that changes the file is not available — instead assert the live read directly: capture the id by computing it, and structure the scenario as `add` → (input step already wrote "v1") → `paste id out.txt` and assert `out.txt == "v1"`, then ALSO assert that bookmarking is a pointer by a unit/CLI test (the `TestBookmarkLivePointerRoundTrip` test already proves the after-edit behavior). Read `e2e/scenario.go` + `e2e/scenarios/s43_shelf_roundtrip.toml` first and pick the structure the schema supports; the load-bearing live-edit proof lives in the CLI unit test, and the e2e proves `add`→`paste`→content end-to-end. Name the scenario subtest `s44_bookmark_live`.

Compute the id: address key for `add README.md` (default) = `State=Unstaged, Worktree=<sandbox local path>` — the worktree path is sandbox-specific, so the **address id is NOT stable across machines**. Therefore the e2e must NOT hardcode the id; capture it instead. Since `Run` has no per-run output assertion and no value threading, the e2e for paste needs the id. **Resolution:** add `bookmark paste --last <dest>` is out of scope; instead the e2e covers only `add` + `list` (asserting the path appears), and the **paste round-trip is covered by the CLI unit test** (`TestBookmarkLivePointerRoundTrip`), which is hermetic and deterministic. The e2e scenario:

```toml
name = "bookmark add/list: a working-tree file is bookmarked and listed"

[input]
steps = [
  { write = "README.md", content = "hi\n" },
  { commit = "initial" },
  { write = "README.md", content = "v1\n" },
]

[[run]]
cmd  = ["bookmark", "add", "README.md"]
exit = 0

[[run]]
cmd  = ["bookmark", "list"]
exit = 0
```

Add an `[[run]]` `expect`-style assertion only if `Run` gained `stdout_contains` (the remotes-tab chunk-3 work added `stdout_contains` to the e2e harness — check `e2e/scenario.go`; if present, assert `bookmark list` stdout contains `README.md`). If not present, the exit-0 of `add`+`list` plus the CLI unit tests are the coverage.

- [ ] **Step 7: Run the e2e + CLI**

Run: `./test.sh e2e 2>&1 | tail -15` and `go test ./internal/cli/ -run TestBookmark`
Expected: the `s44_bookmark_live` (or `_add_list`) scenario PASSES; CLI tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/cli/bookmark.go internal/cli/bookmark_test.go internal/cli/cli.go e2e/scenarios/s44_bookmark_live.toml
git commit -m "feat(cli): gg bookmark {add,list,rm,paste} + e2e"
```

---

### Task 5: TUI capture — `.`-menu "Bookmark this file"

**Files:**
- Create: `internal/tui/bookmark.go` (focusedBookmark + add cmd + msg)
- Modify: `internal/tui/action_menu.go` (inject the row in `availableActions`)
- Modify: `internal/tui/model.go` (handle `bookmarkAddedMsg`)
- Create: `internal/tui/bookmark_test.go`

**Interfaces:**
- Consumes: `domain.BookmarkAdd` (Task 3); `model.Bookmark`/state consts; the focused-file discipline from `internal/tui/shelf.go`'s `focusedShelfRef` (mirror precedence); `m.currentWorktree`, `m.status.Branch`, `m.filesHash`, history/blame `navContext`, the Shelf tab's selected entry.
- Produces: `func (m Model) focusedBookmark() (model.Bookmark, bool)`; `actionRow{id:"bookmark-add",...}` in `availableActions`; `bookmarkAddedMsg`.

- [ ] **Step 1: Write the failing capture test**

Add to a new `internal/tui/bookmark_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestBookmarkRowOnFilesPanel(t *testing.T) {
	m := filesMenuModel() // panelFiles focused with one tracked file "dir/f.txt"
	m.currentWorktree = "/wt"
	if _, ok := findRow(availableActions(m), "bookmark-add"); !ok {
		t.Fatalf("Bookmark this file missing on Files panel")
	}
	b, ok := m.focusedBookmark()
	if !ok || b.State != model.StateUnstaged || b.Worktree != "/wt" || b.Path != "dir/f.txt" {
		t.Fatalf("focusedBookmark = %+v ok=%v", b, ok)
	}
}

func TestBookmarkRowAbsentWhenNoFile(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	if _, ok := m.focusedBookmark(); ok {
		t.Fatalf("no file focused → focusedBookmark should be false")
	}
}
```

- [ ] **Step 2: Run — expect failure**

Run: `go test ./internal/tui/ -run TestBookmarkRow 2>&1 | head`
Expected: FAIL (`focusedBookmark` undefined).

- [ ] **Step 3: Implement focusedBookmark + add cmd**

Create `internal/tui/bookmark.go` (mirror `focusedShelfRef` precedence in `shelf.go`):

```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

// focusedBookmark builds a Bookmark for the file under focus, mirroring the
// shelf-capture precedence. The two-sided diff view is excluded.
func (m Model) focusedBookmark() (model.Bookmark, bool) {
	switch s := m.stackTop().(type) {
	case *historyView:
		if s.ctx.path == "" || s.ctx.rev == "" {
			return model.Bookmark{}, false
		}
		return model.Bookmark{State: model.StateCommitted, Commit: s.ctx.rev, Path: s.ctx.path}, true
	case *blameView:
		if s.ctx.path == "" || s.ctx.rev == "" {
			return model.Bookmark{}, false
		}
		return model.Bookmark{State: model.StateCommitted, Commit: s.ctx.rev, Path: s.ctx.path}, true
	}
	if m.diffView != nil {
		return model.Bookmark{}, false
	}
	if v := m.filesView; v != nil {
		if m.filesTreeFocused && m.filesHash != "" {
			if vis := v.visible(); v.sel >= 0 && v.sel < len(vis) && vis[v.sel].path != "" {
				return model.Bookmark{State: model.StateCommitted, Commit: m.filesHash, Path: vis[v.sel].path}, true
			}
		}
		return model.Bookmark{}, false
	}
	switch m.focus {
	case panelFiles:
		if bi, ok := m.backingIndex(panelFiles); ok {
			f := m.status.Files[bi]
			st := model.StateUnstaged
			if f.Kind == model.KindUntracked {
				st = model.StateUntracked
			}
			return model.Bookmark{State: st, Worktree: m.currentWorktree, Branch: m.status.Branch, Path: f.Path}, true
		}
	case panelStaged:
		if bi, ok := m.backingIndex(panelStaged); ok {
			return model.Bookmark{State: model.StateStaged, Worktree: m.currentWorktree, Branch: m.status.Branch, Path: m.status.Files[bi].Path}, true
		}
	case panelShelf:
		if bi, ok := m.backingIndex(panelShelf); ok {
			e := m.shelfEntries[bi]
			return model.Bookmark{State: model.StateShelf, ShelfID: e.ID, SHA: e.SHA, Path: e.Path}, true
		}
	}
	return model.Bookmark{}, false
}

type bookmarkAddedMsg struct {
	bm  model.Bookmark
	err error
}

func (m Model) bookmarkAddCmd(b model.Bookmark) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		stored, err := svc.BookmarkAdd(context.Background(), b)
		return bookmarkAddedMsg{bm: stored, err: err}
	}
}

func (m Model) bookmarkAddRow() (actionRow, bool) {
	b, ok := m.focusedBookmark()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "bookmark-add",
		label: "Bookmark this file",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.bookmarkAddCmd(b)
		},
	}, true
}
```

- [ ] **Step 4: Inject the row + handle the message**

In `internal/tui/action_menu.go`, in `availableActions`, append after the shelf rows (both the content-window branch and the panel branch — mirror how `shelfAddRow`/`shelfTabRows` are appended):

```go
	if r, ok := m.bookmarkAddRow(); ok {
		out = append(out, r)
	}
```
(and in the `inContentWindow()` early-return branch, append it to `rows` too, exactly like `shelfAddRow`.)

In `internal/tui/model.go`, add a message case next to `shelfAddedMsg`:

```go
	case bookmarkAddedMsg:
		if msg.err != nil {
			m.statusMsg = "bookmark: " + msg.err.Error()
		} else {
			m.statusMsg = "bookmarked " + msg.bm.Path + " → " + msg.bm.ID
		}
		return m, nil
```

- [ ] **Step 5: Run — expect green**

Run: `go test ./internal/tui/ -run TestBookmarkRow` then `go test ./internal/tui/`
Expected: PASS (full package — capture injection must not break menu tests).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/bookmark.go internal/tui/bookmark_test.go internal/tui/action_menu.go internal/tui/model.go
git commit -m "feat(tui): \"Bookmark this file\" menu capture per focused surface"
```

---

### Task 6: TUI bookmark quick-switcher popup — open / render / filter / jump

**Files:**
- Create: `internal/tui/bookmark_popup.go`
- Modify: `internal/tui/model.go` (popup pointer field; open key; key routing; load message; render hook)
- Modify: `internal/tui/view.go` (render the popup)
- Modify: `internal/tui/footer.go`, `internal/tui/help.go` (open key + in-popup keys)
- Modify: `internal/tui/bookmark_test.go`

**Interfaces:**
- Consumes: `domain.BookmarkList`, `domain.BookmarkBytes` (Task 3); the popup/list patterns in `internal/tui/stash_view.go` + `internal/tui/repo_popup.go` (filterable list popup) and the shelf compare diff construction in `internal/tui/shelf_actions.go` (`loadShelfCompareCmd`).
- Produces: `bookmarkPopup` struct + `m.bookmarkPopup *bookmarkPopup`; `bookmarksLoadedMsg`; a global open key; `enter`→jump diff.

- [ ] **Step 1: Choose the open key + add the popup field**

Pick a free global key (verify against `internal/tui/model.go`'s key switch — `g` and `k` are unused; use **`g`** = "go to bookmark", or fall back to a free key the implementer confirms). Add to the `Model` struct (next to `shelfRestorePopup`):

```go
	bookmarkPopup *bookmarkPopup // bookmark quick-switcher; nil = closed
```

- [ ] **Step 2: Write the failing popup test**

Add to `internal/tui/bookmark_test.go`:

```go
func TestBookmarkPopupOpensAndRenders(t *testing.T) {
	m := footerModel()
	m.bookmarkPopup = &bookmarkPopup{items: []model.Bookmark{
		{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "a/b.go"},
	}}
	out := m.renderBookmarkPopup()
	if !strings.Contains(out, "a/b.go") {
		t.Fatalf("popup render missing path:\n%s", out)
	}
}

func TestBookmarkDisplayString(t *testing.T) {
	got := bookmarkDisplay(model.Bookmark{State: model.StateCommitted, Commit: "a1b2c3d4e5", Path: "src/x.go", Branch: "feat"})
	if !strings.Contains(got, "src/x.go") || !strings.Contains(got, "a1b2c3d") {
		t.Fatalf("display = %q", got)
	}
}
```

(add `"strings"` + the model import to the test file)

- [ ] **Step 3: Run — expect failure**

Run: `go test ./internal/tui/ -run 'TestBookmarkPopup|TestBookmarkDisplay' 2>&1 | head`
Expected: FAIL (undefined `bookmarkPopup`/`renderBookmarkPopup`/`bookmarkDisplay`).

- [ ] **Step 4: Implement the popup (struct, display, load, render, open)**

Create `internal/tui/bookmark_popup.go` (model the filterable list on `repo_popup.go`; the diff-jump on `shelf_actions.go`):

```go
package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

type bookmarkPopup struct {
	items  []model.Bookmark
	rows   []string // display strings, parallel to items
	sel    int
	filter string
	mode   dispMode
}

// bookmarkDisplay builds "<container> / <commit-or-state> / <path>".
func bookmarkDisplay(b model.Bookmark) string {
	container := "?"
	switch b.State {
	case model.StateCommitted:
		container = b.Branch
		if container == "" {
			container = "commit"
		}
	case model.StateShelf:
		container = "shelf"
	default:
		container = "wt:" + filepath.Base(b.Worktree)
	}
	mid := b.State.String()
	if b.State == model.StateCommitted && len(b.Commit) >= 7 {
		mid = b.Commit[:7]
	}
	return fmt.Sprintf("%s / %s / %s", container, mid, b.Path)
}

type bookmarksLoadedMsg struct {
	items []model.Bookmark
	err   error
}

func (m Model) loadBookmarksCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		bs, err := svc.BookmarkList(context.Background(), 0, 0)
		return bookmarksLoadedMsg{items: bs, err: err}
	}
}

func newBookmarkPopup(items []model.Bookmark) *bookmarkPopup {
	p := &bookmarkPopup{items: items}
	for _, b := range items {
		p.rows = append(p.rows, bookmarkDisplay(b))
	}
	return p
}

// visibleIdx returns item indices matching the filter (case-insensitive).
func (p *bookmarkPopup) visibleIdx() []int {
	var idx []int
	q := strings.ToLower(p.filter)
	for i, row := range p.rows {
		if q == "" || strings.Contains(strings.ToLower(row), q) {
			idx = append(idx, i)
		}
	}
	return idx
}

func (m Model) renderBookmarkPopup() string {
	p := m.bookmarkPopup
	var b strings.Builder
	b.WriteString("Bookmarks  (type to filter)\n\n")
	vis := p.visibleIdx()
	if len(vis) == 0 {
		b.WriteString("  (none)\n")
	}
	for n, i := range vis {
		cursor := "  "
		if n == p.sel {
			cursor = "> "
		}
		b.WriteString(cursor + p.rows[i] + "\n")
	}
	b.WriteString("\n[↑↓] move  [enter] jump  [p] paste  [m] mark/compare  [x] remove  [esc] close")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
```

In `internal/tui/model.go`: add the open key to the global key switch (the chosen free key, e.g. `g`), gated on `m.opsIdle()` and no other overlay:

```go
		case "g":
			if m.opsIdle() && m.bookmarkPopup == nil {
				return m, m.loadBookmarksCmd()
			}
```

Handle the load + route keys. Add the message case:

```go
	case bookmarksLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "bookmarks: " + msg.err.Error()
			return m, nil
		}
		m.bookmarkPopup = newBookmarkPopup(msg.items)
		return m, nil
```

Add popup-key routing high in `Update` (next to the `shelfRestorePopup`/`branchPopup` routing):

```go
		if m.bookmarkPopup != nil {
			return m.updateBookmarkPopupKey(msg)
		}
```

Add `updateBookmarkPopupKey` to `bookmark_popup.go` — `esc` closes, `↑/↓` move within `visibleIdx`, `enter` jumps, runes/backspace edit the filter, `z` cycles `mode` (paste/mark/remove come in Task 7, stubbed to no-op here or added now):

```go
func (m Model) updateBookmarkPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.bookmarkPopup
	switch msg.Type {
	case tea.KeyEsc:
		m.bookmarkPopup = nil
	case tea.KeyEnter:
		return m.bookmarkJump()
	case tea.KeyUp:
		if p.sel > 0 {
			p.sel--
		}
	case tea.KeyDown:
		if p.sel < len(p.visibleIdx())-1 {
			p.sel++
		}
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(p.filter); len(r) > 0 {
			p.filter = string(r[:len(r)-1])
			p.sel = 0
		}
	case tea.KeyRunes:
		p.filter += string(msg.Runes)
		p.sel = 0
	}
	return m, nil
}

// selectedBookmark returns the bookmark under the popup cursor.
func (m Model) selectedBookmark() (model.Bookmark, bool) {
	p := m.bookmarkPopup
	vis := p.visibleIdx()
	if p.sel < 0 || p.sel >= len(vis) {
		return model.Bookmark{}, false
	}
	return p.items[vis[p.sel]], true
}

// bookmarkJump opens a diff of the bookmark's bytes vs the current working file.
func (m Model) bookmarkJump() (tea.Model, tea.Cmd) {
	b, ok := m.selectedBookmark()
	if !ok {
		return m, nil
	}
	m.bookmarkPopup = nil
	width, _ := m.overlayDims()
	m.diffView = &diffView{title: b.Path, context: bookmarkDisplay(b) + " → working tree", rev: "", loading: true, partial: m.diffPartial, long: m.diffLong, width: width}
	m.diffTag = "bookmark:" + b.ID
	return m, m.loadBookmarkCompareCmd(b)
}
```

Add `loadBookmarkCompareCmd` (mirror `loadShelfCompareCmd` in `shelf_actions.go`, but Old = `svc.BookmarkBytes(b)`, New = working-tree file at `b.Path` rooted at `m.currentWorktree`, nil if absent):

```go
func (m Model) loadBookmarkCompareCmd(b model.Bookmark) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	root := m.currentWorktree
	body := m.diffBodyRows()
	tag := "bookmark:" + b.ID
	v := &diffView{title: b.Path, context: bookmarkDisplay(b) + " → working tree", rev: "", partial: m.diffPartial, long: m.diffLong}
	v.width, _ = m.overlayDims()
	full := filepath.Join(root, b.Path)
	return func() tea.Msg {
		oldSrc := func(ctx context.Context) ([]byte, error) { return svc.BookmarkBytes(ctx, b) }
		var newSrc domainByteSource // see note
		// new side: working-tree file, nil on absent (mirror loadShelfCompareCmd's os.Stat/os.ReadFile guard)
		// ... build newSrc exactly as loadShelfCompareCmd does ...
		out, err := differ.Diff(context.Background(), domainRequest(tag, oldSrc, newSrc))
		_ = full
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
}
```

> Implement `loadBookmarkCompareCmd` by **copying `loadShelfCompareCmd` from `internal/tui/shelf_actions.go` verbatim** and changing only: the `oldSrc` to `svc.BookmarkBytes(ctx, b)`, the tag to `"bookmark:"+b.ID`, and the title/context to `bookmarkDisplay(b)`. The `os.Stat`/`os.ReadFile` new-side guard (using `domain.ByteSource`, `domain.Request`, `domain.MaxDiffBytes`, `errors`, `io/fs`, `os`) is identical — the pseudo-types `domainByteSource`/`domainRequest` above are placeholders for the real `domain.ByteSource`/`domain.Request{Key,Old,New}` exactly as `loadShelfCompareCmd` uses them.

Render hook in `internal/tui/view.go` (next to the `shelfRestorePopup` render):

```go
	if m.bookmarkPopup != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderBookmarkPopup(), w, h)
	}
```

- [ ] **Step 5: Footer + help for the open key**

In `footer.go` add a global binding `{"bookmarks", "g", "[g] bookmarks", func(m Model) bool { return m.opsIdle() }, scopeGlobal}`. In `help.go` add a "Bookmarks (g)" section documenting `g` (open), `enter` (jump), `p` (paste), `m` (mark/compare), `x` (remove), `/`-style type-to-filter, `esc`. The `g` key must appear in help (the `TestHelpFooterCoverage` guard).

- [ ] **Step 6: Run — expect green**

Run: `go test ./internal/tui/`
Expected: PASS (incl. help/footer coverage). Fix drift until green.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): bookmark quick-switcher popup (open/filter/jump)"
```

---

### Task 7: TUI popup — paste / remove / compare-two

**Files:**
- Modify: `internal/tui/bookmark_popup.go` (paste path-popup, remove confirm, mark+compare)
- Modify: `internal/tui/model.go` (popup key cases for `p`/`x`/`m`; reuse the restore path-popup or a bookmark one)
- Modify: `internal/tui/bookmark_test.go`

**Interfaces:**
- Consumes: `domain.BookmarkBytes`/`BookmarkRemove` (Task 3); `engine.WriteFile` via `startOp`; the `decisionState` confirm modal; the mark/`pairOp` machinery (`internal/tui/mark.go`) for compare; the restore path-popup pattern (`shelf_actions.go`).
- Produces: in-popup `p` (paste), `x` (remove-confirm), `m` (mark→compare) handlers.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/bookmark_test.go`:

```go
func TestBookmarkRemoveConfirms(t *testing.T) {
	m := footerModel()
	m.bookmarkPopup = newBookmarkPopup([]model.Bookmark{{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"}})
	mm, _ := m.updateBookmarkPopupKey(keyMsg("x"))
	m = mm.(Model)
	if m.modal == nil {
		t.Fatalf("x should open a remove-confirm modal")
	}
}

func TestBookmarkPasteOpensPathPopup(t *testing.T) {
	m := footerModel()
	m.bookmarkPopup = newBookmarkPopup([]model.Bookmark{{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"}})
	mm, _ := m.updateBookmarkPopupKey(keyMsg("p"))
	m = mm.(Model)
	if m.shelfRestorePopup == nil { // reused path-input popup
		t.Fatalf("p should open the destination path popup")
	}
}
```

> If reusing `shelfRestorePopup` is undesirable, introduce a small shared `pathInputPopup` and assert on it instead — but reuse keeps it minimal; the popup just carries bytes + dest. The plan reuses `shelfRestorePopup` by generalising its `entryID` into a `pendingBytes []byte` (rename to `pathInputPopup` with `data []byte`); update Task 6's `shelf_actions.go` restore accordingly OR add a parallel `bookmarkPastePopup`. **Choose at implementation time; the test asserts whichever field holds the pending paste.**

- [ ] **Step 2: Run — expect failure**

Run: `go test ./internal/tui/ -run 'TestBookmarkRemove|TestBookmarkPaste' 2>&1 | head`
Expected: FAIL (the `p`/`x` cases not handled).

- [ ] **Step 3: Implement the `p`/`x`/`m` cases**

In `updateBookmarkPopupKey`, add to the `switch msg.Type`/string handling (use `msg.String()` for letter keys):

```go
	case tea.KeyRunes:
		switch msg.String() {
		case "x":
			b, ok := m.selectedBookmark()
			if !ok {
				return m, nil
			}
			m.bookmarkPopup = nil
			m.modal = &decisionState{
				req: engine.DecisionRequest{ID: "bookmark-remove", Prompt: "Remove bookmark " + b.Path + "?", Options: []string{"Remove", "Cancel"}},
				onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
					if opt == "Remove" {
						return m, m.bookmarkRemoveCmd(b.ID)
					}
					return m, nil
				},
			}
			return m, nil
		case "p":
			b, ok := m.selectedBookmark()
			if !ok {
				return m, nil
			}
			m.bookmarkPopup = nil
			return m.openBookmarkPaste(b)
		case "m":
			return m.bookmarkMark()
		default:
			p.filter += string(msg.Runes)
			p.sel = 0
		}
		return m, nil
```

(Remove the earlier bare `case tea.KeyRunes` filter-append — it is now the `default` above.)

Add the helpers to `bookmark_popup.go`:

```go
func (m Model) bookmarkRemoveCmd(id string) tea.Cmd {
	svc := m.svc
	reopen := m.loadBookmarksCmd()
	return func() tea.Msg {
		if err := svc.BookmarkRemove(context.Background(), id); err != nil {
			return bookmarksLoadedMsg{err: err}
		}
		return reopen()
	}
}

// openBookmarkPaste fetches bytes, then opens the mandatory-dest path popup that
// runs engine.WriteFile on submit (its Overwrite/Cancel fork is the modal).
func (m Model) openBookmarkPaste(b model.Bookmark) (tea.Model, tea.Cmd) {
	data, err := m.svc.BookmarkBytes(context.Background(), b)
	if err != nil {
		m.statusMsg = "bookmark paste: " + err.Error()
		return m, nil
	}
	m.shelfRestorePopup = &shelfRestorePopup{origin: b.Path, data: data} // see note in Task 7 Step 1
	return m, nil
}
```

> The path popup's Enter handler must `startOp(engine.WriteFile{Path: dest, Data: <pending bytes>})`. If `shelfRestorePopup` currently holds an `entryID` + re-fetches via `ShelfBlob`, generalise it to also carry pre-fetched `data []byte` (used when set), so both shelf restore and bookmark paste share one path popup. Adjust `updateShelfRestoreKey` to prefer `data` when present.

For compare, add `bookmarkMark` mirroring the shelf mark→pair-op: marking sets `m.mark` keyed by bookmark ID within the popup, and a second mark opens a `pairOpPopup` whose Compare `open` calls `m.openBookmarkCompareTwo(aID, bID)` → a diff of `BookmarkBytes(a)` vs `BookmarkBytes(b)`:

```go
func (m Model) openBookmarkCompareTwo(aID, bID string) (Model, tea.Cmd) {
	a, okA := m.bookmarkByID(aID)
	b, okB := m.bookmarkByID(bID)
	if !okA || !okB {
		return m, nil
	}
	m.bookmarkPopup, m.mark = nil, nil
	width, _ := m.overlayDims()
	m.diffView = &diffView{title: a.Path + " ↔ " + b.Path, context: bookmarkDisplay(a) + " → " + bookmarkDisplay(b), loading: true, partial: m.diffPartial, long: m.diffLong, width: width}
	m.diffTag = "bookmark2:" + aID + ":" + bID
	return m, m.loadBookmarkCompareTwoCmd(a, b)
}
```

> `bookmarkByID` scans `m.bookmarkPopup.items` (or re-list). `loadBookmarkCompareTwoCmd` copies `loadShelfCompareTwoCmd` (both sides via `BookmarkBytes`). `bookmarkMark` may, for v1 simplicity, instead implement compare as: press `m` to mark the cursor bookmark (store its id), then `enter`-on-another shows a "compare with marked?" — **but prefer the proven `pairOpPopup` path used by the shelf**: register `pairOpsFor`-style ops is panel-scoped; the popup is not a panel, so implement a tiny in-popup two-mark state directly (first `m` records `p.markID`; second `m` on a different row opens the compare diff). Keep it self-contained to the popup.

- [ ] **Step 4: Run — expect green**

Run: `go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): bookmark popup paste / remove (confirm) / compare-two"
```

---

### Task 8: docs, agentskill, archtest guard, final gate

**Files:**
- Modify: `internal/archtest/import_guard_test.go` (forbid `internal/bookmark` in tui/cli)
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`
- Modify: `internal/agentskill/using-gg.md` (+ `agentskill.go` `Version` 11 → 12)
- Regenerate: `.claude/skills/using-gg/SKILL.md` via `gg init --update`

- [ ] **Step 1: Archtest guard**

In `internal/archtest/import_guard_test.go`, add to the `forbidden` map:

```go
		"github.com/gigagit/gg/internal/bookmark": "frontends must reach the bookmark store through internal/domain",
```

Run: `go test ./internal/archtest/` — expected PASS (frontends import bookmark only transitively via domain).

- [ ] **Step 2: CHANGELOG + README + CLAUDE.md**

- `CHANGELOG.md`: a `### Added` entry — the Bookmarks quick-switcher popup (`g`), `.`-menu capture, and `gg bookmark {add,list,rm,paste}`; explain address-identity + live/frozen.
- `README.md`: a TUI key row for `g` (bookmark switcher) + the `.` capture; a CLI block `gg bookmark add/list/rm/paste`.
- `CLAUDE.md`: add `bookmark` to the package map table ("persistent record registry of richly-addressed file bookmarks behind a fixed `Store` interface; identity = address, content = blob SHA (permanent) or live-by-address"); note the 3 new git verbs.

- [ ] **Step 3: agentskill + version bump**

Add a `gg bookmark` section to `internal/agentskill/using-gg.md`; bump `agentskill.Version` 11 → 12.

- [ ] **Step 4: Regenerate dogfood skill**

```bash
go build -o /tmp/gg-bookmark ./cmd/gg
/tmp/gg-bookmark init --update
```
Then `go test ./internal/agentskill/` (the `TestDogfoodSkillCopyInSync` guard). Stage only the in-repo `.claude/skills/using-gg/SKILL.md`.

- [ ] **Step 5: Full race gate**

Run: `./test.sh race 2>&1 | tail -30`
Expected: vet+gofmt clean, all unit tests + e2e PASS with `-race`.

- [ ] **Step 6: Commit**

```bash
git add internal/archtest internal/agentskill CHANGELOG.md README.md CLAUDE.md .claude/skills/using-gg/SKILL.md
git commit -m "docs(bookmark): changelog/readme/CLAUDE + agentskill v12 + archtest guard"
```

---

## Self-Review

**Spec coverage:** model+store (address-id, no blobs) → Task 1; the 3 git verbs → Task 2; domain commands + `BookmarkBytes` state routing + per-repo store → Task 3; CLI `gg bookmark` + e2e → Task 4; `.`-menu capture per surface → Task 5; quick-switcher popup + jump → Task 6; paste/remove(confirm)/compare-two → Task 7; docs + agentskill v12 + archtest → Task 8. Every spec section maps to a task.

**Placeholder scan:** The only non-literal spots are Task 6/7's diff-load commands and the path-popup reuse, which explicitly say "copy `loadShelfCompareCmd`/`loadShelfCompareTwoCmd` verbatim, changing only the byte source/tag/title" and "reuse `shelfRestorePopup` generalised to carry `data []byte`" — concrete shipped references, not invented APIs. The e2e (Task 4 Step 6) honestly documents the harness limitation (no id threading) and puts the load-bearing live-edit proof in the deterministic CLI unit test, with the e2e covering `add`+`list` (and `stdout_contains` if the harness has it). The open key `g` is flagged "verify free at implementation time."

**Type consistency:** `model.Bookmark` fields + `BookmarkState` consts are identical across Tasks 1/3/4/5/6/7. `BookmarkAdd/List/Get/Remove/BookmarkBytes` signatures match Task 3 (def) and Tasks 4/5/6/7 (use). The 3 git verbs match Task 2 (def) and Task 3 (use). `engine.WriteFile{Path,Data}` + `ErrWriteCancelled` reused as shipped. `bookmarkDisplay`/`selectedBookmark`/`loadBookmarkCompareCmd` are defined and used within Tasks 6–7.

**Known cross-task note:** Task 7 may generalise `shelfRestorePopup` into a shared path-input popup carrying pre-fetched `data []byte`; if so, Task 6's restore path stays working because the field is additive (re-fetch when `data == nil`). Flagged in Task 7 Steps 1 & 3.
