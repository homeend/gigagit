# Merge Preview Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A GitHub-PR-style "what would `source` bring into `target`" diff (`git diff target...source`), with saved (source, target) pairs in a new Previews tab (TUI), `gg preview` verbs (CLI), and a Previews sidebar group (web), all recomputed from the current branch tips.

**Architecture:** A new leaf store package `internal/preview` (TOML + lock under XDG state, the `notes` pattern) is owned by `internal/domain`, which adds CRUD, a cached per-pair `PreviewSummary` (two git calls, keyed by tip hashes) and `PreviewOpen` (merge-base + source tip as two `model.Endpoint`s). Every frontend then reuses the EXISTING compare pipeline (`CompareFiles`/`Differ`/the files view/`/api/compare?revs=1`) with those two hashes. The TUI adds a fourth left tab (`panelPreviews`, `srcPreviews`, chained after branches/remotes refreshes), an add form, rename/delete/swap, a pair-picker row with a once/save dialog, and an open-view re-arm when tips move.

**Tech Stack:** Go 1.26, Bubble Tea/lipgloss TUI, `go-toml/v2`, real `git` in tests, `gitexec.FakeRunner` for argv assertions, vanilla ES-module web client.

**Spec:** `docs/superpowers/specs/2026-09-09-merge-preview-design.md`

## Global Constraints

- Work in the worktree `/mnt/t/others/gigagit.worktrees/feat-merge-preview` (branch `feat/merge-preview`). Every shell command starts with `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview &&`; every Write/Edit uses an absolute path under it.
- Direction is `source → target` everywhere (source = GitHub "compare", target = GitHub "base"). The preview diff is `merge-base(target, source)` → `source` tip. Commits only; the working tree and index never take part.
- Names are resolved to full hashes in DOMAIN before any hash reaches a cache key, a compare tag, or a git argv. The TUI's `m.branches` list (locals only) is never used to resolve a preview side.
- Frontends (`tui`, `cli`, `web`, `mcp`) never import `internal/preview` (archtest-enforced in Task 1).
- Every user-visible TUI string is an `i18n.T("literal")` with the key present in `internal/i18n/lang/{ja,ko,ru,zh}.toml`. Decision-option VALUES stay English and get an `optionDisplayName` case. Run `go test ./internal/tui -run 'TestI18n|TestOptionsVocab|TestMenuLabels|TestHelpFooterCoverage|TestActionMenuLabelCoverage|TestEngineProse' ` after each TUI task.
- New tests call `t.Parallel()` unless they set env vars (`t.Setenv`) or a package-level seam.
- Commit message trailers on every commit:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01U5gJ1kJ3VCakAf9FtDxmrN
  ```
- Use `git -C /mnt/t/others/gigagit.worktrees/feat-merge-preview add <explicit paths>`; never `git add -A`.
- MCP tools, a commits-ahead list, ± line counts and a merge-tree conflict probe are OUT of scope (spec non-goals).

## File structure

| Path | Responsibility |
|---|---|
| `internal/model/preview.go` | `MergePreview` record (plain data, TOML tags). |
| `internal/preview/store.go`, `file_store.go` | `Store` interface + TOML file store with cross-process lock (notes pattern). `ID(source, target)`. |
| `internal/git/preview.go` | Two verbs: `CountLeftRight`, `DiffNameOnlyRange`. |
| `internal/domain/previewstore.go` | Lazy per-repo store resolution, `PreviewsDisabled`, `UsePreviewsDir`, `SetPreviewStore`. |
| `internal/domain/preview.go` | CRUD wrappers, `PreviewState`, `PreviewSummary`, `PreviewOpen`, the summary cache. |
| `internal/cli/preview.go` | `gg preview list|add|rm|rename|show|diff`. |
| `internal/tui/preview_panel.go` | Panel data (`previewRow`, `previewList`), rows builder, source read, arrival handling, state text. |
| `internal/tui/preview_open.go` | Enter → `PreviewOpen` → compare view in preview mode; re-arm on refresh; `previewOpenState`. |
| `internal/tui/preview_add_popup.go` | Two-field add form with branch-name completion + swap. |
| `internal/tui/preview_rename_popup.go` | Label rename popup. |
| `internal/tui/preview_actions.go` | Delete confirm, swap, `.`-menu rows, footer/avail predicates, pair-picker row + once/save dialog. |
| `internal/web/previews.go` | `/api/preview` routes, `emitPreviews`. |
| `internal/web/static/previews.js` | Sidebar section render, menus, add flow, open, live re-open. |
| `e2e/scenarios/s88_merge_preview.toml` | CLI end-to-end. |

---

### Task 1: `model.MergePreview` + `internal/preview` store

**Files:**
- Create: `internal/model/preview.go`
- Create: `internal/preview/store.go`
- Create: `internal/preview/file_store.go`
- Test: `internal/preview/file_store_test.go`
- Modify: `internal/archtest/import_guard_test.go:18` (add the forbidden import)

**Interfaces:**
- Produces: `model.MergePreview{ID, Source, Target, Label string; Created time.Time}`; `preview.ID(source, target string) string`; `preview.Store` (`Add/Get/List/Rename/Remove`); `preview.NewFileStore(root string) *FileStore`; `preview.ErrNotFound`, `preview.ErrExists`.

- [ ] **Step 1: Write the model type**

`internal/model/preview.go`:
```go
package model

import "time"

// MergePreview is one saved "what would Source bring into Target" pair. It
// stores branch NAMES, never hashes: every open resolves the current tips,
// which is what makes a saved preview follow the branches as they move. Plain
// data (like Bookmark), persisted by internal/preview as TOML.
type MergePreview struct {
	ID      string    `toml:"id"`      // derived from (Source, Target); direction-sensitive
	Source  string    `toml:"source"`  // e.g. "feat/login" or "origin/feat/login"
	Target  string    `toml:"target"`  // e.g. "main" or "origin/main"
	Label   string    `toml:"label"`   // human label; defaults to "<source> → <target>"
	Created time.Time `toml:"created"`
}

// DefaultLabel is the label a preview gets when the user gives none.
func (p MergePreview) DefaultLabel() string { return p.Source + " → " + p.Target }
```

- [ ] **Step 2: Write the failing store tests**

`internal/preview/file_store_test.go`:
```go
package preview

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

func rec(src, tgt string) model.MergePreview {
	return model.MergePreview{Source: src, Target: tgt, Created: time.Unix(1_700_000_000, 0).UTC()}
}

func TestIDIsDirectionSensitiveAndStable(t *testing.T) {
	t.Parallel()
	a, b := ID("feat/x", "main"), ID("main", "feat/x")
	if a == b {
		t.Fatal("swapping source and target must change the id")
	}
	if a != ID("feat/x", "main") || len(a) != 8 {
		t.Fatalf("id must be stable and 8 hex chars, got %q", a)
	}
}

func TestAddGetListRoundTrip(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	got, err := fs.Add(rec("feat/x", "main"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != ID("feat/x", "main") || got.Label != "feat/x → main" {
		t.Fatalf("Add must fill the id and the default label, got %+v", got)
	}
	back, err := fs.Get(got.ID)
	if err != nil || back != got {
		t.Fatalf("Get = %+v, %v; want %+v", back, err, got)
	}
	fs.Add(rec("feat/y", "main"))
	list, err := fs.List()
	if err != nil || len(list) != 2 || list[0].Source != "feat/x" {
		t.Fatalf("List = %+v, %v; want 2 in insertion order", list, err)
	}
}

func TestAddDuplicateIsErrExists(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	first, _ := fs.Add(rec("feat/x", "main"))
	again, err := fs.Add(rec("feat/x", "main"))
	if !errors.Is(err, ErrExists) || again.ID != first.ID {
		t.Fatalf("second Add = %+v, %v; want ErrExists with the existing record", again, err)
	}
	// The reverse direction is a different record.
	if _, err := fs.Add(rec("main", "feat/x")); err != nil {
		t.Fatalf("reverse pair must be addable: %v", err)
	}
}

func TestRenameAndRemove(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	r, _ := fs.Add(rec("feat/x", "main"))
	if err := fs.Rename(r.ID, "login fix"); err != nil {
		t.Fatal(err)
	}
	if back, _ := fs.Get(r.ID); back.Label != "login fix" {
		t.Fatalf("label = %q", back.Label)
	}
	if err := fs.Rename("nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename unknown = %v", err)
	}
	if err := fs.Remove(r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Get(r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Remove = %v", err)
	}
	if err := fs.Remove(r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Remove = %v", err)
	}
}

func TestMissingFileReadsEmptyAndCorruptFileErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := NewFileStore(dir)
	if list, err := fs.List(); err != nil || len(list) != 0 {
		t.Fatalf("empty store: %v %v", list, err)
	}
	os.WriteFile(filepath.Join(dir, "previews.toml"), []byte("not = [toml"), 0o644)
	if _, err := fs.List(); err == nil {
		t.Fatal("a corrupt file must be reported, not read as empty")
	}
}

func TestConcurrentWritersAllLand(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fs := NewFileStore(dir) // separate FileStore per goroutine = separate process mutex
			if _, err := fs.Add(rec("feat/"+string(rune('a'+i)), "main")); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	list, _ := NewFileStore(dir).List()
	if len(list) != 8 {
		t.Fatalf("got %d records, want 8 — a writer lost the file lock race", len(list))
	}
}

func TestUnremovableStaleLockStillGivesUp(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX directory permissions and a non-root user")
	}
	dir := t.TempDir()
	fs := NewFileStore(dir)
	lock := filepath.Join(dir, "previews.toml.lock")
	if err := os.WriteFile(lock, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * lockStale)
	os.Chtimes(lock, old, old)
	os.Chmod(dir, 0o555) // the stale lock cannot be removed
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	done := make(chan error, 1)
	go func() { _, err := fs.Add(rec("a", "b")); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Add must fail when the lock cannot be taken")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Add spun forever on an unremovable stale lock")
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/preview/ 2>&1 | head -5`
Expected: build failure (`undefined: NewFileStore`, `ID`, …).

- [ ] **Step 4: Write the store**

`internal/preview/store.go`:
```go
// Package preview is gigagit's machine-local registry of saved merge previews:
// (source, target) branch-name pairs whose diff is recomputed from the live
// tips every time one is opened. Records only, no blobs. Owned by
// internal/domain — frontends never import it (like notes/bookmark/shelf).
package preview

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/homeend/gigagit/internal/model"
)

// ErrNotFound is returned by Get/Rename/Remove for an unknown id.
var ErrNotFound = errors.New("preview: not found")

// ErrExists is returned by Add when the (source, target) pair is already
// stored; the existing record is returned alongside it.
var ErrExists = errors.New("preview: already exists")

// ID derives a record's id from its pair: sha256("source\x00target"), first 8
// hex chars. Direction-sensitive — main→feat/x and feat/x→main are two ids.
func ID(source, target string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + target))
	return hex.EncodeToString(sum[:4])
}

// Store persists preview records. Every mutation re-reads under a
// cross-process lock, applies, and rewrites atomically.
type Store interface {
	Add(p model.MergePreview) (model.MergePreview, error) // fills ID (+ Label when empty); ErrExists with the stored record
	Get(id string) (model.MergePreview, error)
	List() ([]model.MergePreview, error) // insertion order
	Rename(id, label string) error
	Remove(id string) error
}

var _ Store = (*FileStore)(nil)
```

`internal/preview/file_store.go` — copy `internal/notes/file_store.go`'s `lock`, `lockToken`, `releaseLock`, `write` and the constants verbatim (rename `notes-*.toml` → `previews-*.toml`, the error text `notes: notes.toml.lock is held` → `preview: previews.toml.lock is held`), then:
```go
// FileStore keeps previews.toml under root, rewritten atomically under a
// process-local mutex plus a previews.toml.lock file (TUI, CLI and gg web may
// all write). No entry cap: the list is short and user-curated.
type FileStore struct {
	root string
	mu   sync.Mutex
}

func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

type index struct {
	Previews []model.MergePreview `toml:"previews"`
}

func (fs *FileStore) path() string     { return filepath.Join(fs.root, "previews.toml") }
func (fs *FileStore) lockPath() string { return fs.path() + ".lock" }

// read parses the file; a MISSING file is empty, anything else is an error
// (swallowing a corrupt file would let the next write destroy the store).
func (fs *FileStore) read() ([]model.MergePreview, error) {
	data, err := os.ReadFile(fs.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx index
	if err := toml.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("preview: %s is corrupt: %w", fs.path(), err)
	}
	return idx.Previews, nil
}

func (fs *FileStore) List() ([]model.MergePreview, error) { return fs.read() }

func (fs *FileStore) Get(id string) (model.MergePreview, error) {
	ps, err := fs.read()
	if err != nil {
		return model.MergePreview{}, err
	}
	for _, p := range ps {
		if p.ID == id {
			return p, nil
		}
	}
	return model.MergePreview{}, ErrNotFound
}

// mutate is the ONE write path: mutex → file lock → fresh read → apply →
// atomic rewrite. apply returns the new list, or an error to write nothing.
func (fs *FileStore) mutate(apply func([]model.MergePreview) ([]model.MergePreview, error)) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	unlock, err := fs.lock()
	if err != nil {
		return err
	}
	defer unlock()
	before, err := fs.read()
	if err != nil {
		return err
	}
	after, err := apply(slices.Clone(before))
	if err != nil {
		return err
	}
	return fs.write(after)
}

func (fs *FileStore) Add(p model.MergePreview) (model.MergePreview, error) {
	p.ID = ID(p.Source, p.Target)
	if p.Label == "" {
		p.Label = p.DefaultLabel()
	}
	if p.Created.IsZero() {
		p.Created = time.Now().UTC()
	}
	var existing *model.MergePreview
	err := fs.mutate(func(ps []model.MergePreview) ([]model.MergePreview, error) {
		for _, q := range ps {
			if q.ID == p.ID {
				e := q
				existing = &e
				return nil, ErrExists
			}
		}
		return append(ps, p), nil
	})
	if errors.Is(err, ErrExists) {
		return *existing, err
	}
	if err != nil {
		return model.MergePreview{}, err
	}
	return p, nil
}

func (fs *FileStore) Rename(id, label string) error {
	return fs.mutate(func(ps []model.MergePreview) ([]model.MergePreview, error) {
		for i := range ps {
			if ps[i].ID == id {
				ps[i].Label = label
				return ps, nil
			}
		}
		return nil, ErrNotFound
	})
}

func (fs *FileStore) Remove(id string) error {
	return fs.mutate(func(ps []model.MergePreview) ([]model.MergePreview, error) {
		kept := ps[:0:0]
		found := false
		for _, p := range ps {
			if p.ID == id {
				found = true
				continue
			}
			kept = append(kept, p)
		}
		if !found {
			return nil, ErrNotFound
		}
		return kept, nil
	})
}
```
`write` marshals `index{Previews: ps}` via temp file + rename (the notes helper). Imports: `errors, fmt, os, path/filepath, slices, sync, time, crypto/rand, encoding/hex, strconv` + `github.com/pelletier/go-toml/v2`.

- [ ] **Step 5: Add the archtest entry**

In `internal/archtest/import_guard_test.go` after the `internal/notes` line add:
```go
		"github.com/homeend/gigagit/internal/preview":    "frontends must reach the preview store through internal/domain",
```

- [ ] **Step 6: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/preview/ ./internal/archtest/ ./internal/model/ && gofmt -l internal/preview internal/model`
Expected: PASS, no gofmt output.

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && git add internal/model/preview.go internal/preview internal/archtest/import_guard_test.go && git commit -m "feat(preview): saved merge-preview record store (TOML + cross-process lock)" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01U5gJ1kJ3VCakAf9FtDxmrN"
```

---

### Task 2: git verbs `CountLeftRight` and `DiffNameOnlyRange`

**Files:**
- Create: `internal/git/preview.go`
- Test: `internal/git/preview_test.go`

**Interfaces:**
- Produces: `func (r *Repo) CountLeftRight(ctx, left, right string) (leftOnly, rightOnly int, err error)` — `git rev-list --left-right --count <left>...<right>`; `func (r *Repo) DiffNameOnlyRange(ctx, base, tip string) ([]string, error)` — `git diff --name-only -z <base>...<tip>`.

- [ ] **Step 1: Write the failing tests** (`internal/git/preview_test.go`)

```go
package git

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestCountLeftRightArgvAndParse(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-list --left-right --count", gitexec.Result{Stdout: "2\t5\n"})
	r := &Repo{Runner: f}
	l, rr, err := r.CountLeftRight(context.Background(), "aaaa", "bbbb")
	if err != nil || l != 2 || rr != 5 {
		t.Fatalf("got %d %d %v", l, rr, err)
	}
	want := []string{"rev-list", "--left-right", "--count", "aaaa...bbbb"}
	if got := f.LastArgv(); !equalArgv(got, want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}

func TestCountLeftRightBadOutput(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-list --left-right --count", gitexec.Result{Stdout: "garbage\n"})
	if _, _, err := (&Repo{Runner: f}).CountLeftRight(context.Background(), "a", "b"); err == nil {
		t.Fatal("unparseable output must error")
	}
}

func TestDiffNameOnlyRangeArgvAndSplit(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff --name-only (range)", gitexec.Result{Stdout: "a.go\x00dir/b.txt\x00"})
	r := &Repo{Runner: f}
	paths, err := r.DiffNameOnlyRange(context.Background(), "base", "tip")
	if err != nil || len(paths) != 2 || paths[1] != "dir/b.txt" {
		t.Fatalf("paths = %v, %v", paths, err)
	}
	want := []string{"diff", "--name-only", "-z", "base...tip"}
	if got := f.LastArgv(); !equalArgv(got, want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	f.SetResponse("git diff --name-only (range)", gitexec.Result{Stdout: ""})
	if paths, _ := r.DiffNameOnlyRange(context.Background(), "base", "tip"); len(paths) != 0 {
		t.Fatalf("empty diff must yield no paths, got %v", paths)
	}
}
```
If `equalArgv` / `f.LastArgv()` do not exist under those names, use whatever helper the neighbouring `divergence_test.go` uses to assert argv (grep `Argv` in `internal/git/*_test.go` and `internal/gitexec/fake.go`) and adapt the two assertions; the label strings passed to `Run` are the ones below.

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/git/ -run 'CountLeftRight|DiffNameOnlyRange' 2>&1 | head -5`
Expected: `undefined: (*Repo).CountLeftRight`.

- [ ] **Step 3: Implement** (`internal/git/preview.go`)

```go
package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// CountLeftRight counts the commits only on each side of a symmetric range
// (`git rev-list --left-right --count <left>...<right>`): leftOnly is on left
// but not right, rightOnly the reverse. For a merge preview left = target,
// right = source, so rightOnly is "commits ahead". One invocation.
func (r *Repo) CountLeftRight(ctx context.Context, left, right string) (leftOnly, rightOnly int, err error) {
	argv := gitcmd.New("rev-list").Arg("--left-right", "--count", left+"..."+right).ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-list --left-right --count", argv)
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("rev-list --left-right --count: %q", strings.TrimSpace(res.Stdout))
	}
	if leftOnly, err = strconv.Atoi(fields[0]); err != nil {
		return 0, 0, fmt.Errorf("rev-list --left-right --count: %q: %w", fields[0], err)
	}
	if rightOnly, err = strconv.Atoi(fields[1]); err != nil {
		return 0, 0, fmt.Errorf("rev-list --left-right --count: %q: %w", fields[1], err)
	}
	return leftOnly, rightOnly, nil
}

// DiffNameOnlyRange lists the paths changed from merge-base(base, tip) to tip
// (`git diff --name-only -z <base>...<tip>` — git computes the merge base
// itself, so this is the GitHub "files changed" set in one invocation). -z
// keeps non-ASCII paths raw.
func (r *Repo) DiffNameOnlyRange(ctx context.Context, base, tip string) ([]string, error) {
	argv := gitcmd.New("diff").Arg("--name-only", "-z", base+"..."+tip).ToArgv()
	res, err := r.Runner.Run(ctx, "git diff --name-only (range)", argv)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range strings.Split(res.Stdout, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/git/ -run 'CountLeftRight|DiffNameOnlyRange' && gofmt -l internal/git`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && git add internal/git/preview.go internal/git/preview_test.go && git commit -m "feat(git): CountLeftRight and DiffNameOnlyRange verbs for merge previews" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01U5gJ1kJ3VCakAf9FtDxmrN"
```

---

### Task 3: domain store resolution + CRUD

**Files:**
- Create: `internal/domain/previewstore.go`
- Create: `internal/domain/preview.go` (CRUD half; Task 4 appends the summary half)
- Modify: `internal/domain/service.go:42-48` (add `preview preview.Store`)
- Test: `internal/domain/preview_test.go`

**Interfaces:**
- Consumes: `preview.Store`, `preview.NewFileStore`, `preview.ErrNotFound`, `preview.ErrExists`, `repoKey` (shelfstore.go), `s.GitCommonDir`, `s.ResolveRev`.
- Produces:
  ```go
  var PreviewStatePath string
  var PreviewsDisabled bool
  func (s *Service) UsePreviewsDir(dir string)
  func (s *Service) SetPreviewStore(st preview.Store)
  var ErrPreviewsDisabled, ErrPreviewNotFound, ErrPreviewExists error
  func (s *Service) PreviewAdd(ctx, source, target, label string) (model.MergePreview, error) // ErrPreviewExists + existing record on a duplicate
  func (s *Service) PreviewList(ctx) ([]model.MergePreview, error)
  func (s *Service) PreviewGet(ctx, idOrLabel string) (model.MergePreview, error)
  func (s *Service) PreviewRename(ctx, id, label string) error
  func (s *Service) PreviewRemove(ctx, id string) error
  ```

- [ ] **Step 1: Write the failing tests** (`internal/domain/preview_test.go`)

```go
package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/gittest"
)

// previewRepo is a real repo: main has c1; feat/x branches off c1 and adds
// two commits touching a.txt and b.txt; main then adds c2 touching m.txt
// (so the tips have diverged and the three-dot diff is exactly {a.txt, b.txt}).
func previewRepo(t *testing.T) (string, *Service) {
	t.Helper()
	dir, svc := newRealRepo(t)
	svc.UsePreviewsDir(t.TempDir())
	gittest.Run(t, dir, "checkout", "-q", "-b", "feat/x")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gittest.Run(t, dir, "add", "a.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "add a")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gittest.Run(t, dir, "add", "b.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "add b")
	gittest.Run(t, dir, "checkout", "-q", "main")
	os.WriteFile(filepath.Join(dir, "m.txt"), []byte("m\n"), 0o644)
	gittest.Run(t, dir, "add", "m.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "main moves")
	return dir, svc
}

func TestPreviewAddListGetRenameRemove(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	p, err := svc.PreviewAdd(ctx, "feat/x", "main", "")
	if err != nil || p.Label != "feat/x → main" || p.ID == "" {
		t.Fatalf("add = %+v, %v", p, err)
	}
	if _, err := svc.PreviewAdd(ctx, "feat/x", "main", ""); !errors.Is(err, ErrPreviewExists) {
		t.Fatalf("duplicate add = %v, want ErrPreviewExists", err)
	}
	if got, err := svc.PreviewGet(ctx, "feat/x → main"); err != nil || got.ID != p.ID {
		t.Fatalf("get by label = %+v, %v", got, err)
	}
	if err := svc.PreviewRename(ctx, p.ID, "login"); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.PreviewGet(ctx, "login"); got.ID != p.ID {
		t.Fatal("get by new label failed")
	}
	if err := svc.PreviewRemove(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PreviewGet(ctx, p.ID); !errors.Is(err, ErrPreviewNotFound) {
		t.Fatalf("get after remove = %v", err)
	}
	if l, _ := svc.PreviewList(ctx); len(l) != 0 {
		t.Fatalf("list after remove = %v", l)
	}
}

func TestPreviewAddRefusesUnknownBranchAndSamePair(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	if _, err := svc.PreviewAdd(ctx, "nope", "main", ""); err == nil || !contains(err.Error(), "nope") {
		t.Fatalf("unknown source must be refused naming it, got %v", err)
	}
	if _, err := svc.PreviewAdd(ctx, "main", "main", ""); err == nil {
		t.Fatal("source == target must be refused")
	}
}

func TestPreviewsDisabledSeamAndUsePreviewsDir(t *testing.T) {
	prev := PreviewsDisabled
	PreviewsDisabled = true
	defer func() { PreviewsDisabled = prev }()
	ctx := context.Background()
	off := New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	if _, err := off.PreviewList(ctx); !errors.Is(err, ErrPreviewsDisabled) {
		t.Fatalf("disabled list = %v", err)
	}
	_, on := previewRepo(t) // UsePreviewsDir outranks the global switch
	if _, err := on.PreviewList(ctx); err != nil {
		t.Fatalf("UsePreviewsDir must override PreviewsDisabled: %v", err)
	}
}

func contains(s, sub string) bool { return len(sub) == 0 || (len(s) >= len(sub) && index(s, sub) >= 0) }
func index(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```
(If `gittest.Run` is not the helper's name, use whatever `internal/gittest/template.go` exports to run git in a dir — `BasicRepo` calls it at line 88.)

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/domain/ -run 'TestPreview' 2>&1 | head -5`
Expected: `undefined: (*Service).UsePreviewsDir`.

- [ ] **Step 3: Store resolution** (`internal/domain/previewstore.go`) — model on `bookmarkstore.go` (no policy):

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/homeend/gigagit/internal/preview"
)

// PreviewStatePath overrides the previews root dir ("" = XDG default).
var PreviewStatePath string

// PreviewsDisabled turns the surface off process-wide — the TEST seam for
// packages that cannot import internal/preview (tui/web TestMain). An
// injected store (UsePreviewsDir) still wins for that Service.
var PreviewsDisabled bool

// UsePreviewsDir points one Service at its own store under dir.
func (s *Service) UsePreviewsDir(dir string) { s.SetPreviewStore(preview.NewFileStore(dir)) }

// SetPreviewStore injects a store (tests); nil re-arms lazy resolution.
func (s *Service) SetPreviewStore(st preview.Store) {
	s.mu.Lock()
	s.preview = st
	s.mu.Unlock()
}

// previewStore resolves (once) the per-repo store, keyed by git common dir
// under <state>/gg/previews. nil = disabled (no state dir, or PreviewsDisabled).
func (s *Service) previewStore(ctx context.Context) preview.Store {
	s.mu.Lock()
	st := s.preview
	s.mu.Unlock()
	if st != nil {
		return st
	}
	if PreviewsDisabled {
		return nil
	}
	root := PreviewStatePath
	if root == "" {
		base := previewBaseDir()
		if base == "" {
			return nil
		}
		key := "unknown"
		if cd, err := s.GitCommonDir(ctx); err == nil {
			key = repoKey(strings.TrimSpace(cd))
		}
		root = filepath.Join(base, key)
	}
	fs := preview.NewFileStore(root)
	s.mu.Lock()
	if s.preview == nil {
		s.preview = fs
	}
	st = s.preview
	s.mu.Unlock()
	return st
}

// previewBaseDir mirrors notesBaseDir with a "previews" leaf.
func previewBaseDir() string {
	// An explicitly-set $XDG_STATE_HOME wins on every platform (it is a
	// deliberate override — and the only way tests can isolate state on
	// Windows); %LocalAppData% is the ambient Windows default.
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "previews")
	}
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "previews")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "previews")
}
```
Add `preview preview.Store // lazily resolved; nil disables previews` to the `Service` struct next to `notes`.

- [ ] **Step 4: CRUD** (`internal/domain/preview.go`):

```go
package domain

import (
	"context"
	"errors"
	"fmt"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/preview"
)

var ErrPreviewsDisabled = errors.New("previews: no state directory available")

// ErrPreviewNotFound / ErrPreviewExists WRAP the store's errors so frontends
// (which cannot import internal/preview) can errors.Is them.
var (
	ErrPreviewNotFound = fmt.Errorf("%w", preview.ErrNotFound) // message stays "preview: not found"
	ErrPreviewExists   = fmt.Errorf("%w", preview.ErrExists)
)

// PreviewAdd validates both sides resolve to commits and that they differ,
// then stores the pair. A duplicate returns the EXISTING record with
// ErrPreviewExists so frontends can focus it instead of failing.
func (s *Service) PreviewAdd(ctx context.Context, source, target, label string) (model.MergePreview, error) {
	st := s.previewStore(ctx)
	if st == nil {
		return model.MergePreview{}, ErrPreviewsDisabled
	}
	if source == target {
		return model.MergePreview{}, errors.New("preview: source and target are the same branch")
	}
	for _, name := range []string{source, target} {
		if _, ok, err := s.ResolveRev(ctx, name); err != nil {
			return model.MergePreview{}, err
		} else if !ok {
			return model.MergePreview{}, fmt.Errorf("preview: %q is not a branch or commit", name)
		}
	}
	p, err := st.Add(model.MergePreview{Source: source, Target: target, Label: label})
	if errors.Is(err, preview.ErrExists) {
		return p, ErrPreviewExists
	}
	return p, err
}

func (s *Service) PreviewList(ctx context.Context) ([]model.MergePreview, error) {
	st := s.previewStore(ctx)
	if st == nil {
		return nil, ErrPreviewsDisabled
	}
	return st.List()
}

// PreviewGet finds a record by id, else by exact label (first match).
func (s *Service) PreviewGet(ctx context.Context, idOrLabel string) (model.MergePreview, error) {
	ps, err := s.PreviewList(ctx)
	if err != nil {
		return model.MergePreview{}, err
	}
	for _, p := range ps {
		if p.ID == idOrLabel {
			return p, nil
		}
	}
	for _, p := range ps {
		if p.Label == idOrLabel {
			return p, nil
		}
	}
	return model.MergePreview{}, ErrPreviewNotFound
}

func (s *Service) PreviewRename(ctx context.Context, id, label string) error {
	st := s.previewStore(ctx)
	if st == nil {
		return ErrPreviewsDisabled
	}
	if err := st.Rename(id, label); errors.Is(err, preview.ErrNotFound) {
		return ErrPreviewNotFound
	} else {
		return err
	}
}

func (s *Service) PreviewRemove(ctx context.Context, id string) error {
	st := s.previewStore(ctx)
	if st == nil {
		return ErrPreviewsDisabled
	}
	if err := st.Remove(id); errors.Is(err, preview.ErrNotFound) {
		return ErrPreviewNotFound
	} else {
		return err
	}
}
```

- [ ] **Step 5: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/domain/ -run 'TestPreview' && go vet ./internal/domain/ && gofmt -l internal/domain`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && git add internal/domain/previewstore.go internal/domain/preview.go internal/domain/preview_test.go internal/domain/service.go && git commit -m "feat(domain): merge-preview store resolution and CRUD" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01U5gJ1kJ3VCakAf9FtDxmrN"
```

---

### Task 4: domain `PreviewSummary` + `PreviewOpen` with the hash-pair cache

**Files:**
- Modify: `internal/domain/preview.go` (append)
- Test: `internal/domain/preview_test.go` (append)

**Interfaces:**
- Consumes: `s.repo.CountLeftRight`, `s.repo.DiffNameOnlyRange`, `s.repo.MergeBase`, `s.ResolveRev`, `s.factory.Cache("preview")`, `query`.
- Produces:
  ```go
  type PreviewState int
  const ( PreviewOK PreviewState = iota; PreviewMerged; PreviewMissingSource; PreviewMissingTarget; PreviewNoBase )
  func (st PreviewState) String() string // "ok","merged","missing-source","missing-target","no-base" (wire values; English)
  type PreviewSummary struct { State PreviewState; SourceHash, TargetHash string; Files, Ahead int }
  func (s *Service) PreviewSummary(ctx, source, target string) (PreviewSummary, error)
  type PreviewEndpoints struct { Summary PreviewSummary; Left, Right model.Endpoint }
  func (s *Service) PreviewOpen(ctx, source, target string) (PreviewEndpoints, error)
  ```

- [ ] **Step 1: Write the failing tests** (append to `preview_test.go`)

```go
func TestPreviewSummaryStates(t *testing.T) {
	t.Parallel()
	dir, svc := previewRepo(t)
	ctx := context.Background()
	sum, err := svc.PreviewSummary(ctx, "feat/x", "main")
	if err != nil || sum.State != PreviewOK || sum.Files != 2 || sum.Ahead != 2 {
		t.Fatalf("ok pair = %+v, %v; want 2 files, 2 ahead", sum, err)
	}
	if len(sum.SourceHash) != 40 || len(sum.TargetHash) != 40 {
		t.Fatalf("hashes must be full shas: %+v", sum)
	}
	// Reverse: main brings m.txt into feat/x.
	if rev, _ := svc.PreviewSummary(ctx, "main", "feat/x"); rev.Files != 1 || rev.Ahead != 1 {
		t.Fatalf("reverse = %+v", rev)
	}
	if m, _ := svc.PreviewSummary(ctx, "nope", "main"); m.State != PreviewMissingSource {
		t.Fatalf("missing source = %+v", m)
	}
	if m, _ := svc.PreviewSummary(ctx, "main", "nope"); m.State != PreviewMissingTarget {
		t.Fatalf("missing target = %+v", m)
	}
	gittest.Run(t, dir, "merge", "-q", "--no-edit", "feat/x") // main now contains feat/x
	if m, _ := svc.PreviewSummary(ctx, "feat/x", "main"); m.State != PreviewMerged || m.Ahead != 0 {
		t.Fatalf("merged = %+v", m)
	}
	gittest.Run(t, dir, "checkout", "-q", "--orphan", "lonely")
	gittest.Run(t, dir, "commit", "-q", "--allow-empty", "-m", "unrelated root")
	if m, _ := svc.PreviewSummary(ctx, "lonely", "main"); m.State != PreviewNoBase {
		t.Fatalf("no base = %+v", m)
	}
}

func TestPreviewSummaryCachedByHashPair(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse verify commit (resolve)", gitexec.Result{Stdout: "1111111111111111111111111111111111111111\n"})
	f.SetResponse("git rev-list --left-right --count", gitexec.Result{Stdout: "1\t3\n"})
	f.SetResponse("git diff --name-only (range)", gitexec.Result{Stdout: "a\x00b\x00"})
	svc := New(&git.Repo{Runner: f})
	ctx := context.Background()
	first, err := svc.PreviewSummary(ctx, "feat", "main")
	if err != nil || first.Ahead != 3 || first.Files != 2 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	n := f.CallCount("git rev-list --left-right --count")
	if _, err := svc.PreviewSummary(ctx, "feat", "main"); err != nil {
		t.Fatal(err)
	}
	if f.CallCount("git rev-list --left-right --count") != n {
		t.Fatal("unchanged tips must be served from the cache (no rev-list call)")
	}
}

func TestPreviewOpenEndpoints(t *testing.T) {
	t.Parallel()
	dir, svc := previewRepo(t)
	ctx := context.Background()
	eps, err := svc.PreviewOpen(ctx, "feat/x", "main")
	if err != nil || eps.Summary.State != PreviewOK {
		t.Fatalf("open = %+v, %v", eps, err)
	}
	base := gittest.Output(t, dir, "merge-base", "main", "feat/x")
	if eps.Left != (model.Endpoint{Kind: model.EndpointCommit, Hash: base}) || eps.Right.Hash != eps.Summary.SourceHash {
		t.Fatalf("endpoints = %+v, want left=merge-base %s right=source tip", eps, base)
	}
	files, _ := svc.CompareFiles(ctx, eps.Left, eps.Right)
	if len(files) != 2 || files[0].Path != "a.txt" {
		t.Fatalf("compare over the endpoints = %+v, want a.txt b.txt only (never m.txt)", files)
	}
	if eps, _ := svc.PreviewOpen(ctx, "nope", "main"); eps.Summary.State != PreviewMissingSource || eps.Left != (model.Endpoint{}) {
		t.Fatalf("missing side must yield zero endpoints: %+v", eps)
	}
}
```
`f.CallCount(label)` — if `FakeRunner` has no call counter, use its recorded-calls slice (grep `Calls` in `internal/gitexec/fake.go`) and count entries with that label. `gittest.Output` — if absent, read `merge-base` via `exec.Command("git","-C",dir,"merge-base","main","feat/x")`.

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/domain/ -run 'TestPreviewSummary|TestPreviewOpen' 2>&1 | head -5`
Expected: `undefined: PreviewOK`.

- [ ] **Step 3: Implement** (append to `internal/domain/preview.go`; add imports `strings`, `github.com/homeend/gigagit/internal/cache` only if `Sized` is used — it is not):

```go
// PreviewState is a saved pair's live condition.
type PreviewState int

const (
	PreviewOK            PreviewState = iota
	PreviewMerged                     // source is already contained in target (0 ahead)
	PreviewMissingSource              // the source name no longer resolves
	PreviewMissingTarget              // the target name no longer resolves
	PreviewNoBase                     // no common ancestor
)

// String is the wire/CLI value (English protocol, not for TUI display).
func (st PreviewState) String() string {
	switch st {
	case PreviewMerged:
		return "merged"
	case PreviewMissingSource:
		return "missing-source"
	case PreviewMissingTarget:
		return "missing-target"
	case PreviewNoBase:
		return "no-base"
	}
	return "ok"
}

// PreviewSummary is the row summary of one pair. Two rev-parse calls resolve
// the names every time (that is how tip movement is detected); the three
// summary calls (merge-base, rev-list --left-right --count, diff --name-only)
// run only when the hash pair is not in the cache.
type PreviewSummary struct {
	State      PreviewState
	SourceHash string // "" when missing
	TargetHash string // "" when missing
	Files      int    // changed paths merge-base..source; 0 unless PreviewOK
	Ahead      int    // commits on source not in target; 0 unless PreviewOK
	base       string // merge-base(target, source); "" unless PreviewOK (PreviewOpen reuses it)
}

// PreviewSummary resolves both names (missing → the matching Missing state)
// then computes the base, ahead and files. `git merge-base` failing on two
// resolvable tips means unrelated histories (rev-list --left-right would
// happily report every commit on both sides), so the base is probed FIRST.
func (s *Service) PreviewSummary(ctx context.Context, source, target string) (PreviewSummary, error) {
	srcHash, ok, err := s.ResolveRev(ctx, source)
	if err != nil {
		return PreviewSummary{}, err
	}
	if !ok {
		return PreviewSummary{State: PreviewMissingSource}, nil
	}
	tgtHash, ok, err := s.ResolveRev(ctx, target)
	if err != nil {
		return PreviewSummary{}, err
	}
	if !ok {
		return PreviewSummary{State: PreviewMissingTarget, SourceHash: srcHash}, nil
	}
	key := "preview-summary:" + srcHash + ":" + tgtHash
	v, err := s.factory.Cache("preview").GetOrLoad(key, func() (any, error) {
		return query(ctx, s, key, func(ctx context.Context) (PreviewSummary, error) {
			sum := PreviewSummary{SourceHash: srcHash, TargetHash: tgtHash}
			base, err := s.repo.MergeBase(ctx, tgtHash, srcHash)
			if err != nil {
				if ctx.Err() != nil {
					return PreviewSummary{}, err // cancelled: cache nothing (the ResolveRev pattern)
				}
				sum.State = PreviewNoBase
				return sum, nil
			}
			sum.base = strings.TrimSpace(base)
			_, ahead, err := s.repo.CountLeftRight(ctx, tgtHash, srcHash)
			if err != nil {
				return PreviewSummary{}, err
			}
			if ahead == 0 {
				sum.State, sum.base = PreviewMerged, ""
				return sum, nil
			}
			paths, err := s.repo.DiffNameOnlyRange(ctx, tgtHash, srcHash)
			if err != nil {
				return PreviewSummary{}, err
			}
			sum.Ahead, sum.Files = ahead, len(paths)
			return sum, nil
		})
	})
	if err != nil {
		return PreviewSummary{}, err
	}
	return v.(PreviewSummary), nil
}

// PreviewEndpoints is what a frontend opens the compare view with: left =
// merge-base(target, source), right = source tip. Both zero unless PreviewOK.
type PreviewEndpoints struct {
	Summary     PreviewSummary
	Left, Right model.Endpoint
}

// PreviewOpen is PreviewSummary shaped as the two endpoints the compare
// pipeline takes (both hashes, so the diff cache stays correct).
func (s *Service) PreviewOpen(ctx context.Context, source, target string) (PreviewEndpoints, error) {
	sum, err := s.PreviewSummary(ctx, source, target)
	if err != nil || sum.State != PreviewOK {
		return PreviewEndpoints{Summary: sum}, err
	}
	return PreviewEndpoints{
		Summary: sum,
		Left:    model.Endpoint{Kind: model.EndpointCommit, Hash: sum.base},
		Right:   model.Endpoint{Kind: model.EndpointCommit, Hash: sum.SourceHash},
	}, nil
}
```
In `TestPreviewSummaryCachedByHashPair` also set `f.SetResponse("git merge-base", gitexec.Result{Stdout: "2222222222222222222222222222222222222222\n"})` before the first call, and assert the `git merge-base` count stays flat too.

- [ ] **Step 4: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/domain/ -run 'TestPreview' -race && gofmt -l internal/domain`
Expected: PASS (three git calls per OK pair on a cold cache: rev-list, merge-base, diff; zero on a warm one).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && git add internal/domain/preview.go internal/domain/preview_test.go && git commit -m "feat(domain): PreviewSummary (cached by tip hashes) and PreviewOpen endpoints" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01U5gJ1kJ3VCakAf9FtDxmrN"
```

---

### Task 5: CLI `gg preview` + e2e + agent skill

**Files:**
- Create: `internal/cli/preview.go`
- Modify: `internal/cli/cli.go` (switch arm + `commands` map)
- Modify: `cmd/gg/main.go:104` (add `preview` to the commands string)
- Create: `e2e/scenarios/s88_merge_preview.toml`
- Modify: `internal/agentskill/using-gg.md` (after the `gg compare` bullets, line ~152), `internal/agentskill/agentskill.go` (`Version = 62`), then regenerate `.claude/skills/using-gg/SKILL.md`
- Test: `internal/cli/preview_test.go`

**Interfaces:**
- Consumes: `svc.PreviewAdd/List/Get/Rename/Remove/Summary/Open`, `svc.CompareFiles`, `svc.ComparePatch`, `domain.ErrPreviewExists`.
- Produces: verbs
  ```
  gg preview list                                   # id\tlabel\tsource\ttarget\tstate\tfiles\tahead
  gg preview add [--label <text>] <source> <target> # prints the id
  gg preview rm <id|label>
  gg preview rename <id|label> <text>
  gg preview show [--patch] <id|label>              # <status>\t<path> lines (or unified diff); non-ok state → stderr, exit 1
  gg preview diff [--patch] <source> <target>       # one-off, no record
  ```

- [ ] **Step 1: Write the failing tests** (`internal/cli/preview_test.go`; NOT parallel — `t.Setenv`)

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func previewRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := newCLIRepo(t)
	gitRun(t, dir, "checkout", "-q", "-b", "feat/x")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gitRun(t, dir, "add", "."); gitRun(t, dir, "commit", "-q", "-m", "add a")
	gitRun(t, dir, "checkout", "-q", "main")
	os.WriteFile(filepath.Join(dir, "m.txt"), []byte("m\n"), 0o644)
	gitRun(t, dir, "add", "."); gitRun(t, dir, "commit", "-q", "-m", "main moves")
	return dir
}

func TestPreviewAddListShow(t *testing.T) {
	dir := previewRepo(t)
	code, id, errb := runCLI(t, dir, "preview", "add", "--label", "login", "feat/x", "main")
	if code != 0 {
		t.Fatalf("add: %d %s", code, errb)
	}
	id = strings.TrimSpace(id)
	_, out, _ := runCLI(t, dir, "preview", "list")
	if !strings.Contains(out, id+"\tlogin\tfeat/x\tmain\tok\t1\t1") {
		t.Fatalf("list = %q", out)
	}
	_, out, _ = runCLI(t, dir, "preview", "show", "login")
	if !strings.Contains(out, "A\ta.txt") || strings.Contains(out, "m.txt") {
		t.Fatalf("show must list only what feat/x brings: %q", out)
	}
	_, out, _ = runCLI(t, dir, "preview", "show", "--patch", id)
	if !strings.Contains(out, "+a") {
		t.Fatalf("patch = %q", out)
	}
	if code, _, errb := runCLI(t, dir, "preview", "add", "feat/x", "main"); code != 1 || !strings.Contains(errb, id) {
		t.Fatalf("duplicate add: %d %q (must name the existing id)", code, errb)
	}
	if code, _, _ := runCLI(t, dir, "preview", "rename", id, "renamed"); code != 0 {
		t.Fatal("rename")
	}
	if code, _, _ := runCLI(t, dir, "preview", "rm", "renamed"); code != 0 {
		t.Fatal("rm by label")
	}
	if _, out, _ := runCLI(t, dir, "preview", "list"); strings.TrimSpace(out) != "" {
		t.Fatalf("list after rm = %q", out)
	}
}

func TestPreviewDiffOnceAndMergedState(t *testing.T) {
	dir := previewRepo(t)
	if code, out, _ := runCLI(t, dir, "preview", "diff", "feat/x", "main"); code != 0 || !strings.Contains(out, "a.txt") {
		t.Fatalf("diff: %d %q", code, out)
	}
	runCLI(t, dir, "preview", "add", "feat/x", "main")
	gitRun(t, dir, "merge", "-q", "--no-edit", "feat/x")
	code, out, errb := runCLI(t, dir, "preview", "show", "feat/x → main")
	if code != 1 || out != "" || !strings.Contains(errb, "merged") {
		t.Fatalf("show merged: %d %q %q", code, out, errb)
	}
	if _, out, _ := runCLI(t, dir, "preview", "list"); !strings.Contains(out, "\tmerged\t0\t0") {
		t.Fatalf("list merged = %q", out)
	}
}

func TestPreviewUsageErrors(t *testing.T) {
	dir := previewRepo(t)
	if code, _, _ := runCLI(t, dir, "preview"); code != 2 {
		t.Fatal("no subcommand → 2")
	}
	if code, _, errb := runCLI(t, dir, "preview", "add", "nope", "main"); code != 1 || !strings.Contains(errb, "nope") {
		t.Fatalf("unknown branch: %d %q", code, errb)
	}
	if code, _, _ := runCLI(t, dir, "preview", "show", "missing"); code != 1 {
		t.Fatal("unknown id → 1")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/cli/ -run TestPreview 2>&1 | grep -m1 'unknown command'`
Expected: the CLI prints `unknown command "preview"`.

- [ ] **Step 3: Implement** (`internal/cli/preview.go`)

```go
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// cmdPreview implements `gg preview <list|add|rm|rename|show|diff> …`: saved
// merge previews — "what would <source> bring into <target>", the GitHub PR
// files-changed diff (merge-base(target, source)..source). Records store
// branch NAMES; every show recomputes from the current tips.
func cmdPreview(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg preview <list|add|rm|rename|show|diff> ...")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return previewList(svc, rest, stdout, stderr)
	case "add":
		return previewAdd(svc, rest, stdout, stderr)
	case "rm":
		return previewRemove(svc, rest, stdout, stderr)
	case "rename":
		return previewRename(svc, rest, stdout, stderr)
	case "show":
		return previewShow(svc, rest, stdout, stderr)
	case "diff":
		return previewDiff(svc, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "preview: unknown subcommand %q (use list, add, rm, rename, show, or diff)\n", sub)
		return 2
	}
}

func previewList(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if err := flag.NewFlagSet("preview list", flag.ContinueOnError).Parse(args); err != nil {
		return 2
	}
	ctx := context.Background()
	ps, err := svc.PreviewList(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, p := range ps {
		sum, err := svc.PreviewSummary(ctx, p.Source, p.Target)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\t%d\t%d\n", p.ID, p.Label, p.Source, p.Target, sum.State, sum.Files, sum.Ahead)
	}
	return 0
}

func previewAdd(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("preview add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	label := fs.String("label", "", "human label (default: \"<source> → <target>\")")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: gg preview add [--label <text>] <source> <target>")
		return 2
	}
	p, err := svc.PreviewAdd(context.Background(), fs.Arg(0), fs.Arg(1), *label)
	if errors.Is(err, domain.ErrPreviewExists) {
		fmt.Fprintf(stderr, "preview add: %s → %s already saved as %s (%s)\n", p.Source, p.Target, p.ID, p.Label)
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintln(stdout, p.ID)
	return 0
}

func previewRemove(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: gg preview rm <id|label>")
		return 2
	}
	ctx := context.Background()
	p, err := svc.PreviewGet(ctx, args[0])
	if err == nil {
		err = svc.PreviewRemove(ctx, p.ID)
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func previewRename(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: gg preview rename <id|label> <text>")
		return 2
	}
	ctx := context.Background()
	p, err := svc.PreviewGet(ctx, args[0])
	if err == nil {
		err = svc.PreviewRename(ctx, p.ID, args[1])
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func previewShow(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("preview show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	patch := fs.Bool("patch", false, "print unified diffs instead of the changed-file list")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg preview show [--patch] <id|label>")
		return 2
	}
	p, err := svc.PreviewGet(context.Background(), fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return printPreview(svc, p.Source, p.Target, *patch, stdout, stderr)
}

func previewDiff(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("preview diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	patch := fs.Bool("patch", false, "print unified diffs instead of the changed-file list")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: gg preview diff [--patch] <source> <target>")
		return 2
	}
	return printPreview(svc, fs.Arg(0), fs.Arg(1), *patch, stdout, stderr)
}

// printPreview resolves the pair and prints the three-dot file list or
// patch. A non-ok state is reported on stderr with exit 1 (stdout stays
// empty so a script never mistakes "merged" for "no changes").
func printPreview(svc *domain.Service, source, target string, patch bool, stdout, stderr io.Writer) int {
	ctx := context.Background()
	eps, err := svc.PreviewOpen(ctx, source, target)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	switch eps.Summary.State {
	case domain.PreviewOK:
	case domain.PreviewMissingSource:
		fmt.Fprintf(stderr, "preview: missing: %s\n", source)
		return 1
	case domain.PreviewMissingTarget:
		fmt.Fprintf(stderr, "preview: missing: %s\n", target)
		return 1
	default:
		fmt.Fprintf(stderr, "preview: %s → %s: %s\n", source, target, eps.Summary.State)
		return 1
	}
	if patch {
		diff, err := svc.ComparePatch(ctx, eps.Left, eps.Right)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprint(stdout, diff)
		return 0
	}
	files, err := svc.CompareFiles(ctx, eps.Left, eps.Right)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	printCompareFiles(stdout, files)
	return 0
}

// printCompareFiles is `gg compare`'s line format (extracted so both verbs
// stay byte-identical).
func printCompareFiles(stdout io.Writer, files []model.CommitFile) {
	for _, f := range files {
		if f.OldPath != "" {
			fmt.Fprintf(stdout, "%s\t%s -> %s\n", f.Status, f.OldPath, f.Path)
			continue
		}
		fmt.Fprintf(stdout, "%s\t%s\n", f.Status, f.Path)
	}
}
```
In `compare.go`, replace the inline loop at the end of `cmdCompare` with `printCompareFiles(stdout, files)`. In `cli.go` add `case "preview": return cmdPreview(svc, rest, stdout, stderr)` next to `compare` and `"preview": true` to the `commands` map. In `cmd/gg/main.go` insert `preview` after `compare` in the commands string.

- [ ] **Step 4: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/cli/ -run 'TestPreview|TestEverySwitchCaseIsRegistered|TestCompare' && gofmt -l internal/cli cmd`
Expected: PASS.

- [ ] **Step 5: e2e scenario** (`e2e/scenarios/s88_merge_preview.toml`)

```toml
name = "preview: a saved merge preview lists what the source brings and turns merged after the merge"

[input]
steps = [
  { write = "README.md", content = "hi\n" },
  { commit = "initial" },
  { branch = "feat/x" },
  { switch = "feat/x" },
  { write = "a.txt", content = "a\n" },
  { commit = "add a" },
  { switch = "main" },
  { write = "m.txt", content = "m\n" },
  { commit = "main moves" },
]

[[run]]
cmd  = ["preview", "add", "--label", "login", "feat/x", "main"]
exit = 0

[[run]]
cmd             = ["preview", "list"]
exit            = 0
stdout_contains = ["login\tfeat/x\tmain\tok\t1\t1"]

[[run]]
cmd             = ["preview", "show", "login"]
exit            = 0
stdout_contains = ["A\ta.txt"]
stdout_excludes = ["m.txt"]

[[run]]
cmd  = ["merge", "feat/x"]
exit = 0

[[run]]
cmd             = ["preview", "list"]
exit            = 0
stdout_contains = ["merged\t0\t0"]

[[run]]
cmd  = ["preview", "show", "login"]
exit = 1

[expect]
branch = "main"
```
Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./e2e -run 'TestScenarios/preview' -v 2>&1 | tail -5`. If the harness does not set `XDG_STATE_HOME` per sandbox, check `e2e/env_test.go` (it does isolate state for bookmarks) and follow that.

- [ ] **Step 6: Agent skill**

In `internal/agentskill/using-gg.md`, after the second `gg compare` bullet (ends "…instead of the file list."), add:
```
- `gg preview add [--label <text>] <source> <target>` — save a MERGE PREVIEW:
  "what would <source> bring into <target>", i.e. the GitHub pull-request
  files-changed diff (`git diff target...source`, from their merge base to
  the source tip — NOT the tip-to-tip diff `gg compare` prints). Names are
  stored, not hashes, so every later `show` reflects the current tips.
  `gg preview list` prints `<id>\t<label>\t<source>\t<target>\t<state>\t<files>\t<ahead>`
  (state: `ok`, `merged`, `missing-source`, `missing-target`, `no-base`);
  `gg preview show [--patch] <id|label>` prints the file list (or unified
  diff; a non-ok state goes to stderr with exit 1); `gg preview diff
  [--patch] <source> <target>` is the one-off form with no record;
  `gg preview rename <id|label> <text>`; `gg preview rm <id|label>`.
```
Set `const Version = 62`. Then: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go build -o /tmp/gg-preview ./cmd/gg && /tmp/gg-preview init --update` and confirm `git status --short .claude/skills/using-gg/SKILL.md` shows it modified. Run `go test ./internal/agentskill/`.

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && git add internal/cli/preview.go internal/cli/preview_test.go internal/cli/cli.go internal/cli/compare.go cmd/gg/main.go e2e/scenarios/s88_merge_preview.toml internal/agentskill/using-gg.md internal/agentskill/agentskill.go .claude/skills/using-gg/SKILL.md && git commit -m "feat(cli): gg preview list/add/rm/rename/show/diff + e2e + agent skill" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01U5gJ1kJ3VCakAf9FtDxmrN"
```

---

### Task 6: TUI Previews tab (panel, source, rows, tab wiring)

**Files:**
- Create: `internal/tui/preview_panel.go`
- Modify: `internal/tui/model.go` (panel enum, `leftTabs`, field, `activateTab`, `leftReturnTarget`, `dataAvailableMsg` arm, reRoot batch), `internal/tui/source.go` (enum, `srcConsumers`, `sourceNames`, `readSourceCmd` arm), `internal/tui/i18n_display.go` (`sourceDisplayName`), `internal/tui/view.go` (`topTabSegs`), `internal/tui/viewstate.go` (`listFor`, `tabSegsFor`), `internal/tui/selection.go` (`rowKeyAt`), `internal/tui/tab_click_test.go`, `internal/tui/main_test.go` or wherever `TestMain` sets `domain.NotesDisabled` (add `domain.PreviewsDisabled = true`), the four bundles.
- Test: `internal/tui/preview_panel_test.go`

**Interfaces:**
- Consumes: `svc.PreviewList`, `svc.PreviewSummary`, `domain.PreviewSummary`, `domain.PreviewState*`.
- Produces: `panelPreviews`, `srcPreviews`, `type previewRow struct{ rec model.MergePreview; sum domain.PreviewSummary; err error }`, `previewsPayload{rows []previewRow}`, `Model.previews []previewRow`, `previewList` (panelList), `func (m Model) previewRows() []string`, `func previewStateText(r previewRow) string`, `func (m Model) selectedPreview() (previewRow, bool)`.

- [ ] **Step 1: Write the failing tests** (`internal/tui/preview_panel_test.go`)

```go
package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// previewModel is a loaded model over a repo where feat/x brings a.txt into
// main, with one saved preview.
func previewModel(t *testing.T) (Model, string) {
	t.Helper()
	dir, repo := newRepoDir(t)
	runGit(t, dir, "checkout", "-q", "-b", "feat/x")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	runGit(t, dir, "add", "."); runGit(t, dir, "commit", "-q", "-m", "add a")
	runGit(t, dir, "checkout", "-q", "main")
	svc := domain.New(repo)
	svc.UsePreviewsDir(t.TempDir())
	if _, err := svc.PreviewAdd(context.Background(), "feat/x", "main", "login"); err != nil {
		t.Fatal(err)
	}
	m := New(svc)
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)
	// The tab's source rides on its own read (previews are not in Snapshot).
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ = m.Update(cmd())
	return updated.(Model), dir
}

func TestPreviewsTabRendersRowsWithSummary(t *testing.T) {
	t.Parallel()
	m, _ := previewModel(t)
	m = m.activateTab(panelPreviews)
	m.width, m.height = 120, 40
	out := m.View()
	if !strings.Contains(out, "[Previews]") {
		t.Fatalf("tab bar must show the active Previews tab:\n%s", out)
	}
	if !strings.Contains(out, "login") || !strings.Contains(out, "feat/x → main") || !strings.Contains(out, "1 file  ↑1") {
		t.Fatalf("row must show label, pair, files and ahead:\n%s", out)
	}
}

func TestPreviewsTabCyclesAndClicks(t *testing.T) {
	t.Parallel()
	m, _ := previewModel(t)
	m = m.activateTab(panelWorktrees)
	updated, _ := m.Update(keyMsg("ctrl+right"))
	if m = updated.(Model); m.activeLeftTab != panelPreviews || m.focus != panelPreviews {
		t.Fatalf("ctrl+→ from Worktrees must land on Previews, got %v", m.activeLeftTab)
	}
	updated, _ = m.Update(keyMsg("ctrl+right"))
	if m = updated.(Model); m.activeLeftTab != panelBranches {
		t.Fatal("ctrl+→ from Previews must wrap to Branches")
	}
	if p, ok := tabSegAt(topTabSegs(panelBranches), len("[Branches] R W ")); !ok || p != panelPreviews {
		t.Fatalf("clicking the P marker must select Previews, got %v %v", p, ok)
	}
}

func TestPreviewRowStates(t *testing.T) {
	t.Parallel()
	m, dir := previewModel(t)
	runGit(t, dir, "merge", "-q", "--no-edit", "feat/x")
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	if rows := m.previewRows(); len(rows) != 1 || !strings.Contains(rows[0], "merged") {
		t.Fatalf("rows = %v, want merged", rows)
	}
	runGit(t, dir, "branch", "-D", "feat/x")
	m, cmd = m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if rows := m.previewRows(); !strings.Contains(rows[0], "missing: feat/x") {
		t.Fatalf("rows = %v, want missing: feat/x", rows)
	}
}

func TestPreviewSelectionKeyIsID(t *testing.T) {
	t.Parallel()
	m, _ := previewModel(t)
	m = m.activateTab(panelPreviews)
	if key := m.rowKeyAt(panelPreviews, 0); key != m.previews[0].rec.ID {
		t.Fatalf("rowKeyAt = %q, want the record id", key)
	}
}
```
If `keyMsg("ctrl+right")` is not a known case in `model_test.go`'s `keyMsg`, use `tea.KeyMsg{Type: tea.KeyCtrlRight}` directly.

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/tui/ -run TestPreview 2>&1 | head -5`
Expected: `undefined: srcPreviews`.

- [ ] **Step 3: Wire the enum, source, and tab**

- `model.go` panel enum: add `panelPreviews` after `panelReflog` (before `panelCount`). `leftTabs = []panel{panelBranches, panelRemotes, panelWorktrees, panelPreviews}`. Field: `previews []previewRow // saved merge previews + live summaries (srcPreviews)`. `activateTab` first case: add `panelPreviews`. `leftReturnTarget` line 3229: `(p == panelBranches || p == panelWorktrees || p == panelRemotes || p == panelPreviews) && p != m.activeLeftTab`. In the `dataAvailableMsg` switch add:
  ```go
  case srcPreviews:
      key := m.panelSelKey(panelPreviews)
      m.previews = msg.value.(previewsPayload).rows
      m = m.restorePanelSel(panelPreviews, key)
      return m.afterPreviewsRefresh() // Task 7 (re-arm); until then: return m, nil
  ```
  and in the error branch of the handler, when `msg.source == srcPreviews && errors.Is(msg.err, domain.ErrPreviewsDisabled)`, treat it as empty (`m.previews = nil`) with no status line.
  Chain the previews read off the `dataLoadedMsg` SUCCESS arm (after the snapshot fields and `m.loading`/`m.ready` are set): `m, previewsCmd = m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{})` batched with that arm's return. Do NOT put it in `reRoot`'s batch: a previews read beats the Snapshot and its arrival flips `m.ready`/`m.loading`, dropping the repo-switch blank gate (review finding, Task 6). The helper below is therefore unused for reRoot; keep it only if another caller needs it:
  ```go
  // previewsReloadCmd re-reads the previews source (a reRoot's loadCmd comes
  // from Snapshot, which does not carry previews).
  func (m Model) previewsReloadCmd() tea.Cmd { _, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{}); return cmd }
  ```
  (Note `reloadSourcesCmd` returns a Model too; because `reRoot` needs the gen bump to persist, call it as `m, previewsCmd = m.reloadSourcesCmd(...)` inside reRoot before the batch instead of through the helper.)
- `source.go`: `srcPreviews` before `srcCount`; `srcConsumers[srcPreviews] = {panelPreviews}`; `sourceNames[srcPreviews] = "previews"`; `readSourceCmd` arm:
  ```go
  case srcPreviews:
      out.value, out.err = readPreviews(ctx, svc)
  ```
- `i18n_display.go` `sourceDisplayName`: `case srcPreviews: return i18n.T("previews")`.
- `view.go` `topTabSegs`: append `{panelPreviews, mark(panelPreviews, i18n.T("Previews"), "P")}`.
- `viewstate.go` `listFor`: `case panelPreviews: return previewList{rows: m.previews, text: m.previewRows()}`; `tabSegsFor` first case: add `panelPreviews`.
- `selection.go` `rowKeyAt`: `case panelPreviews: return m.previews[u].rec.ID`.
- `tab_click_test.go`: `"[Branches] R W P"`, `"B [Remotes] W P"`, `"B R [Worktrees] P"`, add `{tabBarLabel(panelPreviews), "B R W [Previews]"}` and `topTabSegs(panelPreviews)` to the slots list.
- `TestMain` for `internal/tui` (grep `NotesDisabled = true`): add `domain.PreviewsDisabled = true` beside it. Same in `internal/web`'s TestMain (Task 10 will need it; do it now).
- `refresh.go`: leave untouched — `srcPreviews` is never interval-polled (the `srcNotes` posture); it is chained in Task 7.

- [ ] **Step 4: The panel file** (`internal/tui/preview_panel.go`)

```go
package tui

import (
	"context"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// previewRow is one saved merge preview with its live summary. A row whose
// summary read failed keeps err (rendered as the state text).
type previewRow struct {
	rec model.MergePreview
	sum domain.PreviewSummary
	err error
}

// previewsPayload is srcPreviews' dataAvailableMsg value.
type previewsPayload struct{ rows []previewRow }

// readPreviews lists the records and summarises each. An unchanged pair
// costs two rev-parse calls (name → hash is how movement is detected) and
// no diff work; only a moved pair runs the three summary calls.
func readPreviews(ctx context.Context, svc *domain.Service) (previewsPayload, error) {
	ps, err := svc.PreviewList(ctx)
	if err != nil {
		return previewsPayload{}, err
	}
	rows := make([]previewRow, 0, len(ps))
	for _, p := range ps {
		sum, err := svc.PreviewSummary(ctx, p.Source, p.Target)
		rows = append(rows, previewRow{rec: p, sum: sum, err: err})
	}
	return previewsPayload{rows: rows}, nil
}

// previewList is the panelList behind the Previews tab: Key is the record id
// (stable across renames and refreshes), Name the label, Date the creation.
type previewList struct {
	rows []previewRow
	text []string
}

func (l previewList) Len() int          { return len(l.rows) }
func (l previewList) Row(i int) string  { return l.text[i] }
func (l previewList) Name(i int) string { return l.rows[i].rec.Label }
func (l previewList) Date(i int) int64  { return l.rows[i].rec.Created.Unix() }
func (l previewList) Key(i int) string  { return l.rows[i].rec.ID }

// previewStateText is the right-hand cell: counts when ok, else the state.
func previewStateText(r previewRow) string {
	if r.err != nil {
		return i18n.T("error: %s", r.err.Error())
	}
	switch r.sum.State {
	case domain.PreviewMerged:
		return i18n.T("merged")
	case domain.PreviewMissingSource:
		return i18n.T("missing: %s", r.rec.Source)
	case domain.PreviewMissingTarget:
		return i18n.T("missing: %s", r.rec.Target)
	case domain.PreviewNoBase:
		return i18n.T("no common base")
	}
	if r.sum.Files == 1 {
		return i18n.T("1 file  ↑%d", r.sum.Ahead)
	}
	return i18n.T("%d files  ↑%d", r.sum.Files, r.sum.Ahead)
}

// previewRows renders "<label>  <source → target>  <state>" with the label
// column padded to the widest label (display width, CJK-safe).
func (m Model) previewRows() []string {
	labels := make([]string, len(m.previews))
	for i, r := range m.previews {
		labels[i] = r.rec.Label
	}
	w := maxLabelWidth(8, labels...)
	out := make([]string, 0, len(m.previews))
	for _, r := range m.previews {
		pair := r.rec.Source + " → " + r.rec.Target
		out = append(out, padCell(r.rec.Label, w)+"  "+pair+"  "+previewStateText(r))
	}
	return out
}

// selectedPreview is the focused row when the Previews tab has one.
func (m Model) selectedPreview() (previewRow, bool) {
	i, ok := m.backingIndex(panelPreviews)
	if !ok || i >= len(m.previews) {
		return previewRow{}, false
	}
	return m.previews[i], true
}
```
(Imports: `context`, `domain`, `i18n`, `model` only; `i18n.T` takes the format args directly — the verb-agreement gate reads them off the `T` call.)

Bundle keys to add to all four TOML files (translate each): `"Previews"`, `"previews"`, `"merged"`, `"missing: %s"`, `"no common base"`, `"1 file  ↑%d"`, `"%d files  ↑%d"`, `"error: %s"` (check whether `"error: %s"` already exists; reuse if so).

- [ ] **Step 5: Run the tests and gates**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/tui/ -run 'TestPreview|TestTab|TestI18n|TestFit|TestSourceNames' && gofmt -l internal/tui`
Expected: PASS. If a fit test (`fit_test.go`) asserts the old three-tab header substrings, update them.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && git add internal/tui internal/i18n/lang && git commit -m "feat(tui): Previews tab — fourth left tab with live row summaries" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01U5gJ1kJ3VCakAf9FtDxmrN"
```

---

### Task 7: TUI open a preview + refresh chain + open-view re-arm

**Files:**
- Create: `internal/tui/preview_open.go`
- Modify: `internal/tui/model.go` (enter on `panelPreviews`; `compareFilesMsg` handler path restore; `srcBranches`/`srcRemotes` arrival chain; `closeFilesView` clears `previewOpen`), `internal/tui/files_view.go` (`closeFilesView`), `internal/tui/footer.go` (files-view footer: no `[f]`, already absent), the four bundles.
- Test: `internal/tui/preview_open_test.go`

**Interfaces:**
- Consumes: `svc.PreviewOpen`, `m.openCompareFiles`, `m.filesViewSelectedLine`, `commitFileLines`, `compareTagFor`.
- Produces:
  ```go
  type previewOpenState struct { id, source, target, srcHash, tgtHash, tag, keepPath string }
  Model.previewOpen *previewOpenState
  type previewOpenMsg struct { id, source, target string; eps domain.PreviewEndpoints; err error; keepPath string }
  func (m Model) openPreviewCmd(id, source, target, keepPath string) tea.Cmd
  func (m Model) handlePreviewOpenMsg(msg previewOpenMsg) (Model, tea.Cmd)
  func (m Model) afterPreviewsRefresh() (Model, tea.Cmd)   // re-arm / close the open view
  func previewTitle(source, target string) string
  ```

- [ ] **Step 1: Write the failing tests** (`internal/tui/preview_open_test.go`)

```go
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// drainMsgs feeds up to n command results (expanding tea.BatchMsg) into m.
// Shared by the preview tests in Tasks 7–9.
func drainMsgs(t *testing.T, m Model, cmd tea.Cmd, n int) Model {
	t.Helper()
	for i := 0; i < n && cmd != nil; i++ {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			var rest []tea.Cmd
			for _, c := range batch {
				if c == nil {
					continue
				}
				updated, next := m.Update(c())
				m = updated.(Model)
				if next != nil {
					rest = append(rest, next)
				}
			}
			cmd = nil
			if len(rest) > 0 {
				cmd = tea.Batch(rest...)
			}
			continue
		}
		updated, next := m.Update(msg)
		m = updated.(Model)
		cmd = next
	}
	return m
}

// openPreview presses enter on the first Previews row and drains the open.
func openPreview(t *testing.T, m Model) Model {
	t.Helper()
	m = m.activateTab(panelPreviews)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("enter must start the open")
	}
	updated, cmd = m.Update(cmd()) // previewOpenMsg
	m = updated.(Model)
	if cmd != nil { // compareFilesMsg
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}
	return m
}

func TestEnterOpensPreviewInCompareMode(t *testing.T) {
	t.Parallel()
	m, _ := previewModel(t)
	m = openPreview(t, m)
	if m.filesView == nil || !m.inCompareMode() || m.previewOpen == nil {
		t.Fatal("enter must open the compare files view in preview mode")
	}
	if m.filesTitle != "Merge preview: feat/x → main" || m.comparePair != nil {
		t.Fatalf("title = %q, comparePair = %v (f must be inert)", m.filesTitle, m.comparePair)
	}
	var paths []string
	for _, l := range m.filesView.lines {
		if l.path != "" {
			paths = append(paths, l.path)
		}
	}
	if len(paths) != 1 || paths[0] != "a.txt" {
		t.Fatalf("files = %v, want only a.txt", paths)
	}
	// f is inert in preview mode.
	updated, _ := m.Update(keyMsg("f"))
	if updated.(Model).filesTitle != m.filesTitle {
		t.Fatal("f must not change a preview")
	}
}

func TestEnterOnMergedRowShowsNotice(t *testing.T) {
	t.Parallel()
	m, dir := previewModel(t)
	runGit(t, dir, "merge", "-q", "--no-edit", "feat/x")
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{})
	updated, _ := m.Update(cmd())
	m = updated.(Model).activateTab(panelPreviews)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd != nil {
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}
	if m.filesView != nil || !strings.Contains(m.statusMsg, "merged") {
		t.Fatalf("a merged row must not open; status = %q", m.statusMsg)
	}
}

func TestOpenPreviewReArmsWhenSourceMoves(t *testing.T) {
	t.Parallel()
	m, dir := previewModel(t)
	m = openPreview(t, m)
	oldTag := m.compareTag
	// Add b.txt on feat/x from outside, then refresh branches → previews chain.
	runGit(t, dir, "checkout", "-q", "feat/x")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	runGit(t, dir, "add", "."); runGit(t, dir, "commit", "-q", "-m", "add b")
	runGit(t, dir, "checkout", "-q", "main")
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcBranches}, reloadOpts{})
	updated, chain := m.Update(cmd())
	m = updated.(Model)
	if chain == nil {
		t.Fatal("a branches refresh must chain a previews read")
	}
	// Drain: previews msg → re-arm open cmd → previewOpenMsg → compareFilesMsg.
	m = drainMsgs(t, m, chain, 6)
	if m.compareTag == oldTag {
		t.Fatal("the open preview must re-open with the new tips")
	}
	var paths []string
	for _, l := range m.filesView.lines {
		if l.path != "" {
			paths = append(paths, l.path)
		}
	}
	if len(paths) != 2 || !strings.Contains(m.statusMsg, "feat/x") {
		t.Fatalf("files = %v status = %q; want a.txt b.txt and a 'moved' notice", paths, m.statusMsg)
	}
}

func TestOpenPreviewClosesWhenSourceDeleted(t *testing.T) {
	t.Parallel()
	m, dir := previewModel(t)
	m = openPreview(t, m)
	runGit(t, dir, "branch", "-D", "feat/x")
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{})
	updated, _ := m.Update(cmd())
	m = updated.(Model)
	if m.filesView != nil || m.previewOpen != nil || !strings.Contains(m.statusMsg, "missing") {
		t.Fatalf("view must close with a missing notice; status = %q", m.statusMsg)
	}
}
```
`drainMsgs` above handles `tea.BatchMsg` (a slice of cmds) as well as plain messages; Tasks 8 and 9 reuse it.

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/tui/ -run 'TestEnterOpensPreview|TestOpenPreview|TestEnterOnMerged' 2>&1 | head -5`
Expected: compile error on `m.previewOpen`.

- [ ] **Step 3: Implement** (`internal/tui/preview_open.go`)

```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// previewOpenState records which preview the compare view is showing, so a
// previews refresh can tell whether the tips moved (re-arm) or the pair
// stopped being previewable (close). id is "" for a one-off "show once".
type previewOpenState struct {
	id, source, target string
	srcHash, tgtHash   string
	tag                string // the compare tag opened with
	keepPath           string // path to re-select after a re-arm ("" = top)
}

type previewOpenMsg struct {
	id, source, target string
	keepPath           string
	eps                domain.PreviewEndpoints
	err                error
}

func previewTitle(source, target string) string {
	return i18n.T("Merge preview: %s → %s", source, target)
}

// openPreviewCmd resolves the pair off the UI thread.
func (m Model) openPreviewCmd(id, source, target, keepPath string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		eps, err := svc.PreviewOpen(context.Background(), source, target)
		return previewOpenMsg{id: id, source: source, target: target, keepPath: keepPath, eps: eps, err: err}
	}
}

// previewStateNotice is the status line for a pair that cannot open.
func previewStateNotice(source, target string, st domain.PreviewState) string {
	switch st {
	case domain.PreviewMerged:
		return i18n.T("%s is already merged into %s", source, target)
	case domain.PreviewMissingSource:
		return i18n.T("missing: %s", source)
	case domain.PreviewMissingTarget:
		return i18n.T("missing: %s", target)
	case domain.PreviewNoBase:
		return i18n.T("no common base")
	}
	return ""
}

// handlePreviewOpenMsg opens (or re-opens) the compare view for the pair.
func (m Model) handlePreviewOpenMsg(msg previewOpenMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("error: %s", msg.err.Error())
		return m, nil
	}
	if msg.eps.Summary.State != domain.PreviewOK {
		m.statusMsg = previewStateNotice(msg.source, msg.target, msg.eps.Summary.State)
		if m.previewOpen != nil && m.filesView != nil {
			m = m.closePreviewView() // was open: the pair stopped being previewable
		}
		return m, nil
	}
	tag := compareTagFor(msg.eps.Left, msg.eps.Right)
	if m.previewOpen != nil && m.filesView != nil && m.compareTag == tag {
		// Same diff already showing (e.g. only the target moved, base and
		// source tip unchanged): reconcile the hashes so the next refresh does
		// not re-fire, and say nothing — nothing the user sees changed.
		m.previewOpen.srcHash, m.previewOpen.tgtHash = msg.eps.Summary.SourceHash, msg.eps.Summary.TargetHash
		return m, nil
	}
	m.compareTag = "" // never reuse a view that merely shares the tag (a branch compare whose base IS the tip)
	var cmd tea.Cmd
	m, cmd = m.openCompareFiles(msg.eps.Left, msg.eps.Right)
	m.filesTitle = previewTitle(msg.source, msg.target)
	m.filesContext = m.filesTitle
	m.previewOpen = &previewOpenState{
		id: msg.id, source: msg.source, target: msg.target,
		srcHash: msg.eps.Summary.SourceHash, tgtHash: msg.eps.Summary.TargetHash,
		tag: tag, keepPath: msg.keepPath,
	}
	return m, cmd
}

// afterPreviewsRefresh runs after srcPreviews lands: an open preview whose
// tips moved re-opens itself (keeping the selected file when it still
// exists); one whose pair vanished or stopped being ok closes with a notice.
func (m Model) afterPreviewsRefresh() (Model, tea.Cmd) {
	po := m.previewOpen
	if po == nil || m.filesView == nil {
		return m, nil
	}
	if po.id != "" {
		found := false
		for _, r := range m.previews {
			if r.rec.ID == po.id {
				found = true
				if r.sum.State != domain.PreviewOK {
					m = m.closePreviewView()
					m.statusMsg = previewStateNotice(po.source, po.target, r.sum.State)
					return m, nil
				}
				if r.sum.SourceHash == po.srcHash && r.sum.TargetHash == po.tgtHash {
					return m, nil // unchanged
				}
				moved := po.source
				if r.sum.SourceHash == po.srcHash {
					moved = po.target
				}
				m.statusMsg = i18n.T("preview updated: %s moved", moved)
			}
		}
		if !found {
			m = m.closePreviewView()
			m.statusMsg = i18n.T("preview removed")
			return m, nil
		}
	}
	// A transient ("show once", id == "") preview has no row: re-resolve
	// and let handlePreviewOpenMsg's same-tag check decide (unchanged tips
	// build the same tag and are a no-op).
	keep := ""
	if l, ok := m.filesViewSelectedLine(); ok {
		keep = l.path
	}
	return m, m.openPreviewCmd(po.id, po.source, po.target, keep)
}

// closePreviewView closes the compare view the way esc does: focus returns
// to the panel that opened it (filesReturnFocus), falling back to the active
// left tab when that panel is not visible (a "show once" opened from the
// Branches tab must not strand focus on a hidden tab).
func (m Model) closePreviewView() Model {
	ret := m.filesReturnFocus
	m = m.closeFilesView()
	if m.layout().boxH[ret] <= 0 {
		ret = m.activeLeftTab
	}
	m.focus = ret
	return m
}
```
(Compare with the esc branch at `files_view.go:553-560`; if it does more bookkeeping than restoring focus — e.g. `lastLeftPanel` — mirror that here.)
Wiring in `model.go`:
- `Update`: `case previewOpenMsg: return m.handlePreviewOpenMsg(msg)`.
- Enter on `panelPreviews` (in the enter handler's panel switch, alongside the Worktrees/Branches cases): 
  ```go
  case panelPreviews:
      if r, ok := m.selectedPreview(); ok && m.opsIdle() {
          if r.sum.State != domain.PreviewOK {
              m.statusMsg = previewStateNotice(r.rec.Source, r.rec.Target, r.sum.State)
              return m, nil
          }
          return m, m.openPreviewCmd(r.rec.ID, r.rec.Source, r.rec.Target, "")
      }
  ```
- `compareFilesMsg` handler: after `m.filesView.sel = 0`, add
  ```go
  if po := m.previewOpen; po != nil && po.keepPath != "" {
      for i, l := range m.filesView.lines {
          if l.path == po.keepPath { m.filesView.sel = i; break }
      }
      po.keepPath = ""
  }
  ```
- `closeFilesView` (files_view.go): add `m.previewOpen = nil`.
- Chain: in the `dataAvailableMsg` handler's `srcBranches` and `srcRemotes` arms, after their existing work, `m, chain = m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{})` and return `tea.Batch(existingCmd, chain)` (skip when `msg.startup`, the startup fan-out already reads every source). For the one-off case (`po.id == ""`, Task 9's "show once") the chain still re-arms via the fresh `PreviewOpen` in `afterPreviewsRefresh`.
- `srcPreviews` arm (Task 6 placeholder): `return m.afterPreviewsRefresh()`.

Bundle keys: `"Merge preview: %s → %s"`, `"%s is already merged into %s"`, `"preview updated: %s moved"`, `"preview removed"`. Add `"preview:"` is NOT needed in `statusErrorPrefixes` (`error:` already is).

- [ ] **Step 4: Run tests + gates**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/tui/ -run 'TestPreview|TestEnter|TestOpenPreview|TestI18n|TestCompare' && gofmt -l internal/tui`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && git add internal/tui internal/i18n/lang && git commit -m "feat(tui): open a merge preview in the compare view; re-arm when tips move" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01U5gJ1kJ3VCakAf9FtDxmrN"
```

---

### Task 8: TUI add form, rename, delete, swap, footer/help/menu

**Files:**
- Create: `internal/tui/preview_add_popup.go`, `internal/tui/preview_rename_popup.go`, `internal/tui/preview_actions.go`
- Modify: `internal/tui/model.go` (keys on `panelPreviews`: `a` add, `e` rename (the Worktrees convention — `r` is the global reload key), `d` delete, `s` swap), `internal/tui/footer.go`, `internal/tui/avail.go`, `internal/tui/help.go`, `internal/tui/action_menu.go` (`actionMenuLabel` cases + rows registration), `internal/tui/i18n_display.go` (`optionDisplayName`), bundles.
- Test: `internal/tui/preview_actions_test.go`

**Interfaces:**
- Consumes: `svc.PreviewAdd/Rename/Remove`, `fuzzy.Rank`, `newTextField`, `viewField`, `decisionState`.
- Produces: `previewMutatedMsg{err error; focusID string; open, fromTab bool; source, target string}`; `func (m Model) previewAddCmd(source, target, label string, open, fromTab bool) tea.Cmd`; `func (m Model) branchNameCandidates() []string` (locals + remote-tracking); `canAddPreview/canEditPreview/canSwapPreview` predicates; `.`-menu rows `preview-add`, `preview-rename`, `preview-delete`, `preview-swap`, `preview-open`.

- [ ] **Step 1: Write the failing tests** (`internal/tui/preview_actions_test.go`)

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// drainMsgs comes from preview_open_test.go (Task 7).
func typeString(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	return m
}

func TestAddFormCompletesNamesSwapsAndSaves(t *testing.T) {
	t.Parallel()
	m, _ := previewModel(t)
	m = m.activateTab(panelPreviews)
	updated, _ := m.Update(keyMsg("a"))
	m = updated.(Model)
	p := layerOf[*previewAddPopup](m)
	if p == nil {
		t.Fatal("a must open the add form")
	}
	m = typeString(t, m, "fea")
	if got := p.suggestions(m)[0]; got != "feat/x" {
		t.Fatalf("suggestion = %q", got)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab}) // accept + move to target
	m = updated.(Model)
	if p.source.Value() != "feat/x" || !p.onTarget {
		t.Fatalf("tab must accept the suggestion and move to target: %q %v", p.source.Value(), p.onTarget)
	}
	m = typeString(t, m, "main")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS}) // swap
	m = updated.(Model)
	if p.source.Value() != "main" || p.target.Value() != "feat/x" {
		t.Fatal("ctrl+s must swap the fields")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if layerOf[*previewAddPopup](m) != nil || cmd == nil {
		t.Fatal("enter on target must close the form and start the add")
	}
	m = drainMsgs(t, m, cmd, 6)
	if len(m.previews) != 2 || m.previews[m.sel[panelPreviews]].rec.Source != "main" {
		t.Fatalf("the new pair must be saved and focused: %+v sel=%d", m.previews, m.sel[panelPreviews])
	}
}

func TestAddFormRefusesUnknownName(t *testing.T) {
	t.Parallel()
	m, _ := previewModel(t)
	m = m.activateTab(panelPreviews)
	updated, _ := m.Update(keyMsg("a"))
	m = typeString(t, updated.(Model), "zzz")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = typeString(t, updated.(Model), "main")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainMsgs(t, updated.(Model), cmd, 3)
	if !strings.Contains(m.statusMsg, "zzz") || len(m.previews) != 1 {
		t.Fatalf("status = %q previews = %d", m.statusMsg, len(m.previews))
	}
}

func TestRenameDeleteSwap(t *testing.T) {
	t.Parallel()
	m, _ := previewModel(t)
	m = m.activateTab(panelPreviews)
	updated, _ := m.Update(keyMsg("e"))
	m = updated.(Model)
	if layerOf[*previewRenamePopup](m) == nil {
		t.Fatal("e must open rename")
	}
	m = typeString(t, m, " fix")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainMsgs(t, updated.(Model), cmd, 4)
	if m.previews[0].rec.Label != "login fix" {
		t.Fatalf("label = %q", m.previews[0].rec.Label)
	}
	updated, cmd = m.Update(keyMsg("s"))
	m = drainMsgs(t, updated.(Model), cmd, 4)
	if len(m.previews) != 2 || m.previews[1].rec.Source != "main" {
		t.Fatalf("s must save the reversed pair: %+v", m.previews)
	}
	updated, _ = m.Update(keyMsg("d"))
	m = updated.(Model)
	if m.modal == nil || m.modal.req.ID != "preview-remove" {
		t.Fatal("d must open the remove confirm")
	}
	updated, cmd = m.modal.onResolve(m, "Remove")
	m = drainMsgs(t, updated.(Model), cmd, 4)
	if len(m.previews) != 1 {
		t.Fatalf("after remove: %+v", m.previews)
	}
}

func TestPreviewsFooterAndMenu(t *testing.T) {
	t.Parallel()
	m, _ := previewModel(t)
	m = m.activateTab(panelPreviews)
	m.width, m.height = 120, 40
	out := m.View()
	for _, want := range []string{"[a]dd", "[e] rename", "[d]elete", "[s]wap", "[enter] open"} {
		if !strings.Contains(out, want) {
			t.Errorf("footer lacks %q:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/tui/ -run 'TestAddForm|TestRenameDeleteSwap|TestPreviewsFooter' 2>&1 | head -3`
Expected: `undefined: previewAddPopup`.

- [ ] **Step 3: Add form** (`internal/tui/preview_add_popup.go`)

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/fuzzy"
	"github.com/homeend/gigagit/internal/i18n"
)

// previewAddPopup collects a (source, target) pair. Each field completes
// against the local + remote-tracking branch names (fuzzy); tab/enter accept
// the top suggestion when the typed text is not itself a branch. ctrl+s swaps
// the two fields. Enter on the target submits.
type previewAddPopup struct {
	popupMax
	source, target textfield
	onTarget       bool
}

const previewSuggestLimit = 5

// branchNameCandidates is every name a preview side may be: local branches
// then remote-tracking ones (origin/main, …).
func (m Model) branchNameCandidates() []string {
	out := make([]string, 0, len(m.branches)+len(m.remoteBranches))
	for _, b := range m.branches {
		out = append(out, b.Name)
	}
	for _, r := range m.remoteBranches {
		out = append(out, r.Name)
	}
	return out
}

func (p *previewAddPopup) field() *textfield {
	if p.onTarget {
		return &p.target
	}
	return &p.source
}

// suggestions ranks the candidates against the focused field's text.
func (p *previewAddPopup) suggestions(m Model) []string {
	q := strings.TrimSpace(p.field().Value())
	ms := fuzzy.Rank(q, m.branchNameCandidates(), previewSuggestLimit)
	out := make([]string, 0, len(ms))
	for _, x := range ms {
		out = append(out, x.Text) // fuzzy.Match's candidate field — check fuzzy.go (Str/Text/Value)
	}
	return out
}

// accept replaces the focused field with the top suggestion unless the
// typed text already names a branch exactly.
func (p *previewAddPopup) accept(m Model) {
	v := strings.TrimSpace(p.field().Value())
	for _, c := range m.branchNameCandidates() {
		if c == v {
			return
		}
	}
	if s := p.suggestions(m); len(s) > 0 && v != "" {
		*p.field() = newTextField(s[0])
	}
}

func (p *previewAddPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyCtrlS:
		p.source, p.target = p.target, p.source
	case tea.KeyTab:
		p.accept(m)
		p.onTarget = !p.onTarget
	case tea.KeyEnter:
		p.accept(m)
		if !p.onTarget {
			p.onTarget = true
			return m, nil
		}
		src, tgt := strings.TrimSpace(p.source.Value()), strings.TrimSpace(p.target.Value())
		if src == "" || tgt == "" {
			return m, nil
		}
		m = m.popLayer()
		return m, m.previewAddCmd(src, tgt, "", true, true)
	case tea.KeySpace:
		// branch names cannot contain spaces
	default:
		p.field().HandleEditKey(msg)
	}
	return m, nil
}

func (p *previewAddPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *previewAddPopup) box(m Model) string {
	w, _ := m.overlayDims()
	cw := popupContentWidth(w)
	srcMark, tgtMark := "> ", "  "
	if p.onTarget {
		srcMark, tgtMark = "  ", "> "
	}
	var b strings.Builder
	b.WriteString(i18n.T("New merge preview") + "\n\n")
	b.WriteString(viewField(srcMark+i18n.T("source: "), p.source, !p.onTarget, cw) + "\n")
	b.WriteString(viewField(tgtMark+i18n.T("target: "), p.target, p.onTarget, cw) + "\n")
	if s := p.suggestions(m); len(s) > 0 {
		b.WriteString("\n" + i18n.T("matches: ") + strings.Join(s, "  ") + "\n")
	}
	b.WriteString("\n" + i18n.T("[tab] next field  [ctrl+s] swap  [enter] save & open  [esc] cancel"))
	return modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}
```
Check `fuzzy.Match`'s field name for the matched string in `internal/fuzzy/fuzzy.go` and use it.

- [ ] **Step 4: Rename popup** (`internal/tui/preview_rename_popup.go`) — clone `rename_branch_popup.go`'s shape:

```go
type previewRenamePopup struct {
	popupMax
	id    string
	label textfield
}

func (p *previewRenamePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		v := strings.TrimSpace(p.label.Value())
		m = m.popLayer()
		if v == "" {
			return m, nil
		}
		return m, m.previewRenameCmd(p.id, v)
	default:
		p.label.HandleEditKey(msg)
	}
	return m, nil
}
// render/box: title i18n.T("Rename preview"), field i18n.T("label: "), hint i18n.T("[enter] rename  [esc] cancel")
```

- [ ] **Step 5: Actions, commands, predicates, rows** (`internal/tui/preview_actions.go`)

```go
// previewMutatedMsg is the result of any store mutation from the TUI: the
// tab reloads; focusID selects a row after the reload; open then opens it.
// fromTab is true for the tab's own keys (a/e/d/s) and false for the pair
// dialog: only the former moves focus onto the Previews tab afterwards.
type previewMutatedMsg struct {
	err            error
	focusID        string
	open           bool
	fromTab        bool
	source, target string
}

func (m Model) previewAddCmd(source, target, label string, open, fromTab bool) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		p, err := svc.PreviewAdd(context.Background(), source, target, label)
		if errors.Is(err, domain.ErrPreviewExists) {
			err = nil // focus the existing row instead
		}
		return previewMutatedMsg{err: err, focusID: p.ID, open: open, fromTab: fromTab, source: source, target: target}
	}
}

func (m Model) previewRenameCmd(id, label string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		return previewMutatedMsg{err: svc.PreviewRename(context.Background(), id, label), focusID: id, fromTab: true}
	}
}

func (m Model) previewRemoveCmd(id string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		return previewMutatedMsg{err: svc.PreviewRemove(context.Background(), id), fromTab: true}
	}
}

// handlePreviewMutatedMsg reports errors, else reloads the tab and remembers
// what to focus/open once the fresh rows land.
func (m Model) handlePreviewMutatedMsg(msg previewMutatedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("error: %s", msg.err.Error())
		return m, nil
	}
	m.previewFocusID = msg.focusID
	m.previewFocusTab = msg.fromTab
	var open tea.Cmd
	if msg.open {
		open = m.openPreviewCmd(msg.focusID, msg.source, msg.target, "")
	}
	var reload tea.Cmd
	m, reload = m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{})
	return m, tea.Batch(reload, open)
}
```
Add `previewFocusID string` and `previewFocusTab bool` to `Model`. In the `srcPreviews` arrival arm (Task 6), after `restorePanelSel`: if `m.previewFocusID != ""` select the row whose `rec.ID` matches; if `m.previewFocusTab` also set `m.activeLeftTab, m.focus, m.lastLeftPanel = panelPreviews, panelPreviews, panelPreviews`; then clear both fields. The pair dialog (Task 9) passes `fromTab=false`; the tab's keys pass `true`.

Predicates (`avail.go` or this file):
```go
func (m Model) canAddPreview() bool  { return m.focus == panelPreviews && m.opsIdle() }
func (m Model) canEditPreview() bool { _, ok := m.selectedPreview(); return m.focus == panelPreviews && ok && m.opsIdle() }
```
Keys in `model.go`'s key switch, gated on `m.focus == panelPreviews`: `a` → `m.pushLayer(&previewAddPopup{source: newTextField(""), target: newTextField("")})`; `e` → push `&previewRenamePopup{id: r.rec.ID, label: newTextField(r.rec.Label)}`; `s` → `m.previewAddCmd(r.rec.Target, r.rec.Source, "", false, true)`; `d` → 
```go
m.modal = &decisionState{
	req: engine.DecisionRequest{ID: "preview-remove", Prompt: i18n.T("Remove preview %s?", r.rec.Label), Options: []string{"Remove", "Cancel"}},
	sel: 1,
	onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
		if opt == "Remove" { return m, m.previewRemoveCmd(r.rec.ID) }
		return m, nil
	},
}
```
Check the existing `d`/`e`/`s`/`a` arms: `s` on Branches is switch, `a` in the files view is full-tree — each existing arm switches on `m.focus`, so add a `case panelPreviews:` to each. `optionDisplayName`: add `case "Remove": return i18n.T("Remove")` if absent.

Footer (`footer.go` `contextBindings`):
```go
{"preview-open", "enter", i18n.T("[enter] open"), func(m Model) bool { return m.canEditPreview() }, scopeRow},
{"preview-add", "a", i18n.T("[a]dd"), func(m Model) bool { return m.canAddPreview() }, scopeWindow},
{"preview-rename", "e", i18n.T("[e] rename"), func(m Model) bool { return m.canEditPreview() }, scopeRow},
{"preview-delete", "d", i18n.T("[d]elete"), func(m Model) bool { return m.canEditPreview() }, scopeRow},
{"preview-swap", "s", i18n.T("[s]wap"), func(m Model) bool { return m.canEditPreview() }, scopeRow},
```
`actionMenuLabel` cases: `"preview-open"` → `i18n.T("Open merge preview")`, `"preview-add"` → `i18n.T("New merge preview…")`, `"preview-rename"` → `i18n.T("Rename preview…")`, `"preview-delete"` → `i18n.T("Remove preview…")`, `"preview-swap"` → `i18n.T("Save reversed preview")`. `help.go`: a `h(i18n.T("Previews"))` section with `r` rows for `enter`, `a`, `e`, `d`, `s`, plus a note line that a remote target (`origin/main`) is added via `a`.

Bundle keys: all `i18n.T` literals above (`"New merge preview"`, `"source: "`, `"target: "`, `"matches: "`, the hint lines, `"Rename preview"`, `"label: "`, `"Remove preview %s?"`, `"Remove"`, `"[enter] open"`, `"[a]dd"`, `"[e] rename"`, `"[s]wap"`, the menu labels, the help rows). `"[d]elete"` and `"Cancel"` exist.

- [ ] **Step 6: Run tests + gates**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go test ./internal/tui/ -run 'TestPreview|TestAddForm|TestRenameDeleteSwap|TestI18n|TestOptionsVocab|TestMenuLabels|TestHelpFooterCoverage|TestActionMenuLabelCoverage' && gofmt -l internal/tui`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && git add internal/tui internal/i18n/lang && git commit -m "feat(tui): previews add form, rename, remove, swap; footer, menu and help rows" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01U5gJ1kJ3VCakAf9FtDxmrN"
```

---

### Task 9: TUI pair-picker row + once/save dialog

**Files:**
- Modify: `internal/tui/mark.go` (`pairOpsFor` Branches list), `internal/tui/preview_actions.go` (dialog), `internal/tui/i18n_display.go` (`optionDisplayName`), bundles.
- Test: `internal/tui/preview_pair_test.go`

**Interfaces:**
- Consumes: `pairOp{label, enabled, open}`, `decisionState`, `m.openPreviewCmd`, `m.previewAddCmd`.
- Produces: `func (m Model) openPreviewPairDialog(source, target string) (Model, tea.Cmd)`; option values `"show once"`, `"show and save"`, `"swap direction"`, `"cancel"`.

- [ ] **Step 1: Write the failing test** (`internal/tui/preview_pair_test.go`)

```go
package tui

import (
	"strings"
	"testing"
)

func TestPairPickerOffersMergePreviewDialog(t *testing.T) {
	t.Parallel()
	m, _ := previewModel(t)
	m = m.activateTab(panelBranches)
	// Mark feat/x, move to main, pair.
	m.sel[panelBranches] = indexOfBranch(m, "feat/x")
	m = pressRune(t, m, "m")
	m.sel[panelBranches] = indexOfBranch(m, "main")
	m = pressPair(t, m)
	p := layerOf[*pairOpPopup](m)
	if p == nil {
		t.Fatal("pair popup")
	}
	var row *pairOp
	for i := range p.ops {
		if strings.HasPrefix(p.ops[i].label("feat/x", "main"), "Merge preview feat/x → main") {
			row = &p.ops[i]
		}
	}
	if row == nil {
		t.Fatal("pair popup must offer the merge preview row")
	}
	m = m.popLayer()
	m, _ = row.open(m, "feat/x", "main")
	if m.modal == nil || m.modal.req.ID != "preview-pair" || !strings.Contains(m.modal.req.Prompt, "feat/x into main") {
		t.Fatalf("dialog = %+v", m.modal)
	}
	updated, _ := m.modal.onResolve(m, "swap direction")
	m = updated.(Model)
	if !strings.Contains(m.modal.req.Prompt, "main into feat/x") {
		t.Fatal("swap must re-render the dialog reversed")
	}
	updated, _ = m.modal.onResolve(m, "swap direction")
	m = updated.(Model)
	updated, cmd := m.modal.onResolve(m, "show and save")
	m = drainMsgs(t, updated.(Model), cmd, 6)
	if m.filesView == nil || m.previewOpen == nil || m.previewOpen.id == "" {
		t.Fatal("show and save must save the pair and open it")
	}
	if len(m.previews) != 1 { // the fixture's pair IS feat/x → main: saving again focuses it, no duplicate
		t.Fatalf("previews = %+v", m.previews)
	}
	// show once: opens with an empty id and saves nothing.
	m = m.closeFilesView()
	m, _ = row.open(m, "feat/x", "main")
	updated, cmd = m.modal.onResolve(m, "show once")
	m = drainMsgs(t, updated.(Model), cmd, 4)
	if m.previewOpen == nil || m.previewOpen.id != "" {
		t.Fatal("show once must open transiently")
	}
}

func indexOfBranch(m Model, name string) int {
	for i := 0; i < m.panelLen(panelBranches); i++ {
		if m.rowKeyAt(panelBranches, i) == name {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/tui/ -run TestPairPicker`; expected: the merge preview row is missing.

- [ ] **Step 3: Implement**

In `mark.go` `pairOpsFor`, append to the Branches list (after Compare):
```go
{
	label:   func(marked, selected string) string { return i18n.T("Merge preview %s → %s…", marked, selected) },
	enabled: true,
	open: func(m Model, marked, selected string) (Model, tea.Cmd) {
		return m.openPreviewPairDialog(marked, selected)
	},
},
```
In `preview_actions.go`:
```go
// openPreviewPairDialog is the once/save/swap dialog behind the pair-picker
// row. Option values are protocol (English); optionDisplayName renders them.
func (m Model) openPreviewPairDialog(source, target string) (Model, tea.Cmd) {
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "preview-pair",
			Prompt:  i18n.T("Preview merging %s into %s", source, target),
			Options: []string{"show once", "show and save", "swap direction", "cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			switch opt {
			case "show once":
				return m, m.openPreviewCmd("", source, target, "")
			case "show and save":
				return m, m.previewAddCmd(source, target, "", true, false)
			case "swap direction":
				return m.openPreviewPairDialog(target, source)
			}
			return m, nil
		},
	}
	return m, nil
}
```
`optionDisplayName` cases for the four values; bundle keys for them plus `"Merge preview %s → %s…"` and `"Preview merging %s into %s"`. Check `escapeOption`/the modal's esc mapping picks `"cancel"` (grep `abort` in `modal_*.go`; if esc requires an `"abort"` option, rename `"cancel"` → `"abort"` here and in the test).

- [ ] **Step 4: Run tests + gates** — `go test ./internal/tui/ -run 'TestPairPicker|TestPreview|TestI18n|TestOptionsVocab|TestMark' && gofmt -l internal/tui`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && git add internal/tui internal/i18n/lang && git commit -m "feat(tui): branch pair picker 'Merge preview A → B…' with show once / save / swap" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01U5gJ1kJ3VCakAf9FtDxmrN"
```

---

### Task 10: web `/api/preview` routes

**Files:**
- Create: `internal/web/previews.go`
- Modify: `internal/web/uistate.go:19` (`uiSections` + `"previews"`), `internal/web` TestMain (`domain.PreviewsDisabled = true` if not done in Task 6)
- Test: `internal/web/previews_test.go`

**Interfaces:**
- Consumes: `RegisterRoutes`, `writeGuard`, `writeJSON`, `writeErr`, `readCtx`, `isGitArgSafe`, `svc.Branches`, `svc.RemoteBranches`, `svc.Preview*`, `s.liveHubRef`.
- Produces:
  ```
  GET    /api/preview                 → {"entries":[{id,label,source,target,state,files,ahead,source_hash,target_hash}], "disabled"?:true}
  POST   /api/preview  {source,target,label?}   (writeGuard) → {"entry":…}; 409 {"error":…,"id":…} on duplicate
  POST   /api/preview/rename {id,label}         (writeGuard) → {"ok":true}
  DELETE /api/preview?id=              (writeGuard; the client sends Content-Type) → {"ok":true}
  GET    /api/preview/open?id=         → {"state","source","target","label","left","right"}   (left/right = hashes, "" unless ok)
  GET    /api/preview/diff?source=&target=  → same shape, no record (names allowlisted against live branch lists)
  func (s *Server) emitPreviews()      → SSE changed:["previews"]
  ```

- [ ] **Step 1: Write the failing tests** (`internal/web/previews_test.go`)

```go
package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func previewRepo(t *testing.T) string {
	t.Helper()
	isolateState(t)
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-q", "-b", "feat/x")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gitRun(t, dir, "add", "-A"); gitRun(t, dir, "commit", "-m", "add a")
	gitRun(t, dir, "checkout", "-q", "main")
	return dir
}

func deleteWithJSON(t *testing.T, ts *httptest.Server, path string) int {
	t.Helper()
	req, _ := http.NewRequest("DELETE", ts.URL+path, nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

type pvList struct {
	Entries []struct {
		ID, Label, Source, Target, State string
		Files, Ahead                     int
		SourceHash                       string `json:"source_hash"`
	} `json:"entries"`
}

func TestPreviewCRUDAndOpen(t *testing.T) {
	dir := previewRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	var added struct{ Entry struct{ ID string } }
	if code := postJSON(t, ts, "/api/preview", `{"source":"feat/x","target":"main","label":"login"}`, "application/json", "", &added); code != http.StatusOK {
		t.Fatalf("add: %d", code)
	}
	if code := postJSON(t, ts, "/api/preview", `{"source":"feat/x","target":"main"}`, "application/json", "", nil); code != http.StatusConflict {
		t.Fatalf("duplicate: %d, want 409", code)
	}
	if code := postJSON(t, ts, "/api/preview", `{"source":"feat/x","target":"main"}`, "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Fatalf("writeGuard: %d", code)
	}
	var got pvList
	getJSON(t, ts, "/api/preview", &got)
	if len(got.Entries) != 1 || got.Entries[0].State != "ok" || got.Entries[0].Files != 1 || got.Entries[0].Ahead != 1 || len(got.Entries[0].SourceHash) != 40 {
		t.Fatalf("list = %+v", got)
	}
	var open struct{ State, Left, Right, Source, Target string }
	if code := getJSON(t, ts, "/api/preview/open?id="+added.Entry.ID, &open); code != http.StatusOK || open.State != "ok" || open.Left == "" || open.Right != got.Entries[0].SourceHash {
		t.Fatalf("open = %d %+v", code, open)
	}
	if code := postJSON(t, ts, "/api/preview/rename", `{"id":"`+added.Entry.ID+`","label":"renamed"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("rename: %d", code)
	}
	if code := deleteWithJSON(t, ts, "/api/preview?id="+added.Entry.ID); code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	got = pvList{}
	getJSON(t, ts, "/api/preview", &got)
	if len(got.Entries) != 0 {
		t.Fatal("not removed")
	}
}

func TestPreviewDiffOnceAllowlistsNames(t *testing.T) {
	dir := previewRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	var open struct{ State, Left, Right string }
	if code := getJSON(t, ts, "/api/preview/diff?source=feat/x&target=main", &open); code != http.StatusOK || open.State != "ok" {
		t.Fatalf("diff = %d %+v", code, open)
	}
	if code := getJSON(t, ts, "/api/preview/diff?source=nope&target=main", nil); code != http.StatusNotFound {
		t.Fatalf("unknown name = %d, want 404", code)
	}
	if code := getJSON(t, ts, "/api/preview/diff?source=--exec&target=main", nil); code != http.StatusBadRequest {
		t.Fatalf("flag-shaped name = %d, want 400", code)
	}
	if code := postJSON(t, ts, "/api/preview", `{"source":"nope","target":"main"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Fatalf("add unknown = %d", code)
	}
}

func TestPreviewUISectionPersists(t *testing.T) {
	t.Parallel()
	if !strings.Contains(strings.Join(uiSections, ","), "previews") {
		t.Fatal("previews must be an allowlisted sidebar section or its fold state is dropped")
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/web/ -run TestPreview`; expected 404s / compile error.

- [ ] **Step 3: Implement** (`internal/web/previews.go`)

```go
package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// Merge previews: saved (source → target) pairs whose diff is
// merge-base(target, source)..source — the GitHub PR files-changed view.
// Names are resolved to hashes HERE; the client opens the existing compare
// page with the two hashes (revs=1), never with the names.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/preview", s.handlePreviews)
		mux.HandleFunc("POST /api/preview", writeGuard(s.handlePreviewAdd))
		mux.HandleFunc("POST /api/preview/rename", writeGuard(s.handlePreviewRename))
		mux.HandleFunc("DELETE /api/preview", writeGuard(s.handlePreviewRemove))
		mux.HandleFunc("GET /api/preview/open", s.handlePreviewOpen)
		mux.HandleFunc("GET /api/preview/diff", s.handlePreviewDiff)
	})
}

type previewRow struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Source     string `json:"source"`
	Target     string `json:"target"`
	State      string `json:"state"`
	Files      int    `json:"files"`
	Ahead      int    `json:"ahead"`
	SourceHash string `json:"source_hash,omitempty"`
	TargetHash string `json:"target_hash,omitempty"`
}

func previewRowFrom(p model.MergePreview, sum domain.PreviewSummary) previewRow {
	return previewRow{ID: p.ID, Label: p.Label, Source: p.Source, Target: p.Target,
		State: sum.State.String(), Files: sum.Files, Ahead: sum.Ahead,
		SourceHash: sum.SourceHash, TargetHash: sum.TargetHash}
}

func (s *Server) handlePreviews(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ctx := readCtx(r)
	ps, err := svc.PreviewList(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrPreviewsDisabled) {
			writeJSON(w, map[string]any{"entries": []previewRow{}, "disabled": true})
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]previewRow, 0, len(ps))
	for _, p := range ps {
		sum, err := svc.PreviewSummary(ctx, p.Source, p.Target)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		rows = append(rows, previewRowFrom(p, sum))
	}
	writeJSON(w, map[string]any{"entries": rows})
}

// knownRefName resolves a wire name against the live local + remote-tracking
// lists (the /api/compare allowlist posture: an unknown name must be a 404,
// not an empty preview). code is 0 when ok.
func (s *Server) knownRefName(r *http.Request, name string) (int, error) {
	if !isGitArgSafe(name) {
		return http.StatusBadRequest, errors.New("invalid branch")
	}
	svc := s.service()
	bs, err := svc.Branches(r.Context())
	if err != nil {
		return http.StatusInternalServerError, err
	}
	for _, b := range bs {
		if b.Name == name {
			return 0, nil
		}
	}
	rs, err := svc.RemoteBranches(r.Context())
	if err != nil {
		return http.StatusInternalServerError, err
	}
	for _, b := range rs {
		if b.Name == name {
			return 0, nil
		}
	}
	return http.StatusNotFound, fmt.Errorf("unknown branch %q", name)
}

func (s *Server) handlePreviewAdd(w http.ResponseWriter, r *http.Request) {
	var req struct{ Source, Target, Label string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	for _, n := range []string{req.Source, req.Target} {
		if code, err := s.knownRefName(r, n); code != 0 {
			writeErr(w, code, err)
			return
		}
	}
	svc := s.service()
	p, err := svc.PreviewAdd(readCtx(r), req.Source, req.Target, req.Label)
	if errors.Is(err, domain.ErrPreviewExists) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "already saved", "id": p.ID})
		return
	}
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	sum, _ := svc.PreviewSummary(readCtx(r), p.Source, p.Target)
	s.emitPreviews()
	writeJSON(w, map[string]any{"entry": previewRowFrom(p, sum)})
}

func (s *Server) handlePreviewRename(w http.ResponseWriter, r *http.Request) {
	var req struct{ ID, Label string }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" || req.Label == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id and label required"))
		return
	}
	if err := s.service().PreviewRename(readCtx(r), req.ID, req.Label); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	s.emitPreviews()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handlePreviewRemove(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id required"))
		return
	}
	if err := s.service().PreviewRemove(readCtx(r), id); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	s.emitPreviews()
	writeJSON(w, map[string]any{"ok": true})
}

// writePreviewOpen resolves the pair and answers the open/diff shape.
func (s *Server) writePreviewOpen(w http.ResponseWriter, r *http.Request, label, source, target string) {
	eps, err := s.service().PreviewOpen(r.Context(), source, target)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{
		"state": eps.Summary.State.String(), "label": label,
		"source": source, "target": target,
		"left": eps.Left.Hash, "right": eps.Right.Hash,
		"source_hash": eps.Summary.SourceHash, "target_hash": eps.Summary.TargetHash,
	})
}

func (s *Server) handlePreviewOpen(w http.ResponseWriter, r *http.Request) {
	p, err := s.service().PreviewGet(r.Context(), r.URL.Query().Get("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	s.writePreviewOpen(w, r, p.Label, p.Source, p.Target)
}

func (s *Server) handlePreviewDiff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	source, target := q.Get("source"), q.Get("target")
	for _, n := range []string{source, target} {
		if code, err := s.knownRefName(r, n); code != 0 {
			writeErr(w, code, err)
			return
		}
	}
	s.writePreviewOpen(w, r, source+" → "+target, source, target)
}

// emitPreviews tells every open page the previews changed ("previews" is
// not a ticker source; mutations are its only producer, like notes).
func (s *Server) emitPreviews() {
	if h := s.liveHubRef(); h != nil {
		h.emit(liveMsg{Changed: []string{"previews"}, Reason: "previews"})
	}
}
```
`uistate.go`: append `"previews"` to `uiSections`.

- [ ] **Step 4: Run tests** — `go test ./internal/web/ -run 'TestPreview|TestUIState' && gofmt -l internal/web`. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && git add internal/web/previews.go internal/web/previews_test.go internal/web/uistate.go && git commit -m "feat(web): /api/preview list/add/rename/remove/open/diff" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01U5gJ1kJ3VCakAf9FtDxmrN"
```

---

### Task 11: web client — Previews sidebar section, menus, open, live re-open

**Files:**
- Create: `internal/web/static/previews.js`
- Modify: `internal/web/static/index.html` (section after shelf), `core.js:73` (`SECTIONS`), `sidebar.js` (`COLLAPSED_DEFAULT`, `applySection` header `+` control for `previews` — NOT `fetchBranches`: `files.js` and `ops.js` already import `sidebar.js`, so sidebar → previews → files would be a cycle with top-level code on both ends), `menus.js` (`MENUS` + `"preview"`), `live.js` (`SIDEBAR` + `"previews"`; `fetchPreviews()` + re-open hook), `ops.js` (`manualRefresh` also calls `fetchPreviews()`), `app.js` (import + boot fetch)
- Test: `internal/web/previewsjs_test.go` (source-pin test)

**Interfaces:**
- Consumes: `getJSON`, `postJSON`, `openPrompt`, `showLocalConfirm`, `showCtxMenu`, `registerRows`, `extraRows`, `openCompare(a, b, {revs, aLabel, bLabel})`, `state`, `opLine`, `fetchBranches`.
- Produces: `renderPreviews()`, `openPreviewEntry(e)`, `openPreviewPair(source, target, save)`, `state.previews`, `state.previewOpen = {id, sourceHash, targetHash}`.

- [ ] **Step 1: Write the source-pin test** (`internal/web/previewsjs_test.go`)

```go
package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The previews section touches five lists; a missed one half-works silently.
func TestPreviewsJSIsWiredEverywhere(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"core.js", `"previews"`, "SECTIONS must include previews (header click wiring)"},
		{"sidebar.js", `"previews"]`, "COLLAPSED_DEFAULT must fold previews on a first run"},
		{"previews.js", `getJSON("/api/preview")`, "previews.js owns its fetch"},
		{"live.js", `fetchPreviews()`, "an SSE sidebar refresh must reload previews"},
		{"ops.js", `fetchPreviews()`, "manual refresh must reload previews"},
		{"app.js", `fetchPreviews()`, "boot must load previews"},
		{"live.js", `"previews"`, "SIDEBAR must include previews so SSE re-fetches it"},
		{"menus.js", `"preview"`, "MENUS must accept preview rows"},
		{"app.js", `./previews.js`, "the module must be imported"},
		{"index.html", `id="previews-list"`, "the section markup"},
		{"previews.js", `revs: 1`, "open must use the hash form of openCompare"},
		{"previews.js", `"Content-Type": "application/json"`, "DELETE goes through writeGuard"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure** — `go test ./internal/web/ -run TestPreviewsJS`; expected: every check fails.

- [ ] **Step 3: Implement**

`index.html` after the shelf `<ul>`:
```html
    <div id="previews-header" class="side-header">previews</div>
    <ul id="previews-list"></ul>
```
Before writing `previews.js`, confirm every imported name is exported: `grep -n '^export' internal/web/static/{core,layers,ops,files,menus}.js` (as of this plan: `$ esc getJSON postJSON state` from core.js; `openPrompt showCtxMenu` from layers.js; `opLine showLocalConfirm` from ops.js; `openCompare` from files.js; `extraRows registerHelp registerRows` from menus.js — a name that is not exported is a load-time SyntaxError for the WHOLE page, which no Go test catches). `previews.js` imports only those five modules and never `sidebar.js`; `ops.js`/`files.js` importing `previews.js` for a function called at run time is a safe cycle (no top-level use of the binding).

`core.js`: `SECTIONS = [..., "shelf", "previews"]`. `sidebar.js`: `COLLAPSED_DEFAULT = ["tags", "stashes", "reflog", "bookmarks", "shelf", "previews"]`. In `applySection`, mirror the `locate` control: `const add = name === "previews" ? `<span class="locate" title="new merge preview (a in the TUI)">+</span>` : "";` and include it after the name; in the header click handler, when the click target has class `locate` inside `previews-header`, call `window.__ggAddPreview()` (set by previews.js) instead of folding. `menus.js`: add `"preview"` to `MENUS`. `live.js`: `SIDEBAR` add `"previews"`; in `refreshSources`, when `sidebar` is true push `fetchPreviews()` into `jobs`, and after `await Promise.all(jobs)` call `reopenPreviewIfMoved()` (both imported from previews.js). `ops.js` `manualRefresh`: call `fetchPreviews()` beside `fetchBranches()`. `app.js`: `import { fetchPreviews } from "./previews.js";` and call `fetchPreviews()` where `fetchBranches()` is first called at boot.

`previews.js`:
```js
// previews.js — saved merge previews: the GitHub-PR "files changed" view for
// a (source → target) pair, recomputed from the live tips. The sidebar list,
// its menus, the add flow, and the open page (the existing compare screen
// over the two hashes /api/preview/open resolves).
import { $, esc, getJSON, postJSON, state } from "./core.js";
import { openPrompt, showCtxMenu } from "./layers.js";
import { opLine, showLocalConfirm } from "./ops.js";
import { openCompare } from "./files.js";
import { extraRows, registerRows, registerHelp } from "./menus.js";

// fetchPreviews loads the list and renders it. previews.js owns this (it
// cannot ride fetchBranches: sidebar.js is imported by files.js/ops.js, and a
// sidebar → previews → files edge would close an import cycle).
export async function fetchPreviews() {
  try { state.previews = (await getJSON("/api/preview")).entries || []; }
  catch (e) { state.previews = []; }
  renderPreviews();
}
const refresh = () => fetchPreviews();

function stateText(e) {
  switch (e.state) {
    case "merged": return "merged";
    case "missing-source": return "missing: " + e.source;
    case "missing-target": return "missing: " + e.target;
    case "no-base": return "no common base";
  }
  return e.files + " files ↑" + e.ahead;
}

export function renderPreviews() {
  $("previews-list").innerHTML = (state.previews || [])
    .map((e) => `<li data-id="${esc(e.id)}" title="${esc(e.source + " → " + e.target)}"><span class="mk"></span>` +
      `${esc(e.label)} <span class="dim">${esc(e.source)} → ${esc(e.target)}</span> <span class="dim">${esc(stateText(e))}</span></li>`)
    .join("");
}

// openPreviewBody opens the compare page over the resolved hashes and
// remembers what is showing so a live refresh can re-open it when a tip moves.
async function openPreviewBody(body) {
  if (body.state !== "ok") { opLine("merge preview " + body.source + " → " + body.target + ": " + stateText(body), true); state.previewOpen = null; return; }
  await openCompare(body.left, body.right, { revs: 1, aLabel: "merge-base(" + body.target + ")", bLabel: body.source });
  $("files-title").textContent = "merge preview: " + body.source + " → " + body.target;
  // The origin-filter buttons are meaningless over merge-base → tip ("a only" is always empty): hide them the way a missing merge base does. Check applyCompareFilter in files.js for the field it keys off (originsError) and set it here, then re-run applyCompareFilter().
  state.previewOpen = { id: body.id || "", source: body.source, target: body.target, sourceHash: body.source_hash, targetHash: body.target_hash };
}

export async function openPreviewEntry(e) {
  try { const body = await getJSON("/api/preview/open?id=" + encodeURIComponent(e.id)); body.id = e.id; await openPreviewBody(body); }
  catch (err) { opLine("preview: " + (err.message || err), true); }
}

async function openOnce(source, target) {
  try { await openPreviewBody(await getJSON("/api/preview/diff?source=" + encodeURIComponent(source) + "&target=" + encodeURIComponent(target))); }
  catch (err) { opLine("preview: " + (err.message || err), true); }
}

async function savePreview(source, target, label, open) {
  let entry;
  try { entry = (await postJSON("/api/preview", { source, target, label: label || "" })).entry; }
  catch (err) {
    if (err.data && err.data.id) entry = { id: err.data.id }; // already saved: open that one
    else { opLine("preview: " + (err.message || err), true); return; }
  }
  refresh();
  if (open) openPreviewEntry(entry);
}

// openPreviewPair is the once/save dialog (the TUI's pair-picker row).
export function openPreviewPair(source, target) {
  showLocalConfirm("Preview merging " + source + " into " + target + "?", ["show once", "show and save", "swap direction", "abort"], (o) => {
    if (o === "show once") openOnce(source, target);
    else if (o === "show and save") savePreview(source, target, "", true);
    else if (o === "swap direction") openPreviewPair(target, source);
  });
}

function knownName(n) {
  return (state.branches || []).some((b) => b.name === n) || (state.remotes || []).some((r) => r.name === n);
}

// addPreviewFlow: two prompts (source, then target), validated against the
// loaded branch lists so a typo is refused here rather than as a 404.
function addPreviewFlow() {
  openPrompt({ title: "Merge preview — source branch:", value: "", onSubmit: (source) => {
    if (!knownName(source)) { opLine("unknown branch " + source, true); return; }
    openPrompt({ title: "Merge preview — merge " + source + " into:", value: "main", onSubmit: (target) => {
      if (!knownName(target)) { opLine("unknown branch " + target, true); return; }
      if (target === source) { opLine("source and target are the same branch", true); return; }
      savePreview(source, target, "", true);
    } });
  } });
}
window.__ggAddPreview = addPreviewFlow;

async function removePreview(e) {
  try { await fetch("/api/preview?id=" + encodeURIComponent(e.id), { method: "DELETE", headers: { "Content-Type": "application/json" } }); }
  catch (err) { opLine("preview: " + (err.message || err), true); return; }
  if (state.previewOpen && state.previewOpen.id === e.id) state.previewOpen = null;
  refresh();
}

function showPreviewMenu(e, x, y) {
  const items = [
    { label: "open merge preview", act: () => openPreviewEntry(e) },
    { label: "rename…", act: () => openPrompt({ title: "Rename " + e.label + " to:", value: e.label, onSubmit: async (label) => {
      try { await postJSON("/api/preview/rename", { id: e.id, label }); } catch (err) { opLine("preview: " + (err.message || err), true); return; }
      refresh();
    } }) },
    { label: "save reversed (" + e.target + " → " + e.source + ")", act: () => savePreview(e.target, e.source, "", false) },
  ];
  items.push(...extraRows("preview", e));
  items.push({ sep: true });
  items.push({ label: "remove preview", danger: true, act: () =>
    showLocalConfirm("Remove the preview " + e.label + "?", ["remove", "abort"], (o) => { if (o === "remove") removePreview(e); }) });
  showCtxMenu(items, x, y);
}

$("previews-list").addEventListener("click", (ev) => {
  const li = ev.target.closest("li"); if (!li || !li.dataset.id) return;
  const e = (state.previews || []).find((x) => x.id === li.dataset.id); if (e) openPreviewEntry(e);
});
$("previews-list").addEventListener("contextmenu", (ev) => {
  const li = ev.target.closest("li"); if (!li || !li.dataset.id) return;
  ev.preventDefault();
  const e = (state.previews || []).find((x) => x.id === li.dataset.id); if (e) showPreviewMenu(e, ev.clientX, ev.clientY);
});

// Branch row: preview merging THIS branch into the current one.
registerRows("branch", (b) => {
  const cur = (state.branches || []).find((x) => x.is_head);
  if (!b || !b.name || !cur || b.is_head) return [];
  return [{ sep: true }, { label: "merge preview " + b.name + " → " + cur.name + "…", act: () => openPreviewPair(b.name, cur.name) }];
});
registerRows("menu", () => [{ label: "new merge preview…", act: addPreviewFlow }]);

// reopenPreviewIfMoved: after a sidebar refresh, an open preview whose tips
// changed re-opens itself (the TUI's re-arm); one that stopped being ok closes
// to a notice.
export async function reopenPreviewIfMoved() {
  const po = state.previewOpen;
  if (!po || state.filesMode !== "compare") return;
  const row = po.id ? (state.previews || []).find((e) => e.id === po.id) : null;
  if (po.id && !row) { state.previewOpen = null; return; }
  if (row && row.source_hash === po.sourceHash && row.target_hash === po.targetHash) return;
  const moved = row && row.source_hash === po.sourceHash ? po.target : po.source;
  opLine("preview updated: " + moved + " moved");
  if (po.id) openPreviewEntry(row); else openOnce(po.source, po.target);
}

registerHelp && registerHelp("previews", "saved merge previews — what a source branch would bring into a target (GitHub's files-changed diff); right-click a row for rename / reverse / remove, + to add");
```
Check `registerHelp`'s real signature in `menus.js` and adapt (or drop the call). Check `state.remotes` rows carry `name` as the full `origin/x` form (sidebar.js `renderRemotes`); if the field differs, use it in `knownName`. Add a `.dim { opacity: .7 }` rule to `style.css` if none exists.

- [ ] **Step 4: Run tests** — `go test ./internal/web/ && gofmt -l internal/web`. Expected: PASS (the pin test and all handler tests).

- [ ] **Step 5: Browser probe (executor = the main session with claude-in-chrome; a subagent without browser tools reports this step as "left for the controller")**

Build and run against a fixture: `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && go build -o /tmp/gg-preview ./cmd/gg && cd $(mktemp -d) && git init -q -b main && echo hi > README.md && git add . && git commit -qm init && git checkout -qb feat/x && echo a > a.txt && git add . && git commit -qm "add a" && git checkout -q main && echo m > m.txt && git add . && git commit -qm "main moves" && /tmp/gg-preview web --port 0`  (use whatever flag `gg web --help` shows for the port; note the printed URL). Then in the browser: curl `/api/repo` first to prove the port is this run's; unfold `previews`; click `+`, type `feat/x`, enter, `main`, enter; assert a row `feat/x → main … 1 files ↑1` is VISIBLE (`getBoundingClientRect().height > 0`); click it; assert the files list shows exactly `a.txt` and the title reads `merge preview: feat/x → main`; in the fixture shell commit `b.txt` on feat/x; wait for the SSE refresh (or press `r`); assert the row now says `2 files ↑2` and the open list shows `b.txt` without a reload. Record the probe output in the commit message body.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && git add internal/web/static internal/web/previewsjs_test.go && git commit -m "feat(web): Previews sidebar group, add/rename/remove, open over hashes, live re-open" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01U5gJ1kJ3VCakAf9FtDxmrN"
```

---

### Task 12: docs, headless TUI snapshot, full gates, verify binary

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md` (one package-map row for `preview`), `docs/CLAUDE-details.md` (preview section: decision id `preview-pair`, the names-never-reach-a-cache-key rule, the chained `srcPreviews` refresh), `docs/web-tui-parity.md` (row for previews).

- [ ] **Step 1: CHANGELOG entry** (top of the unreleased section):

```markdown
### Merge previews (GitHub-PR-style diff of `source → target`)

- **What it is:** the diff `git diff target...source` — what merging `source`
  into `target` would bring in — instead of the tip-to-tip compare. Saved
  pairs store branch NAMES and recompute from the live tips.
- **TUI:** a fourth left tab **Previews** (ctrl+←/→, click) with live rows
  `label  source → target  N files ↑M` (or `merged` / `missing: x` / `no
  common base`); `enter` opens it in the compare view, `a` adds (branch-name
  completion, ctrl+s swaps), `e` renames, `d` removes, `s` saves the reversed
  pair. The Branches pair picker (`m`+`m`) gained **Merge preview A → B…**
  with show once / show and save / swap direction. An open preview re-opens
  itself when either tip moves and closes with a notice when the pair is
  merged or a side disappears.
- **CLI:** `gg preview list|add|rm|rename|show [--patch]|diff [--patch]`.
- **Web:** a **previews** sidebar group (`+` to add, right-click for
  rename / reverse / remove), a **merge preview … → current** row on branch
  rows, `/api/preview*`; the open page follows moved tips over SSE.
```

- [ ] **Step 2: README** — add the Previews tab to the TUI panel list and the `gg preview` verbs to the CLI section (mirror the compare bullets' style).

- [ ] **Step 3: CLAUDE.md row**

```
| `preview`    | Machine-local registry of saved merge previews (`source → target` branch-name pairs; records only, TOML + lock under XDG state). Owned by `domain`; frontends never import it. |
```

- [ ] **Step 4: Headless TUI snapshot** — `cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && ./build.sh linux` (or `go build -o ./gg ./cmd/gg`), then follow the `driving-tui-headless` skill: in a fixture repo with one saved preview (`gg preview add feat/x main`), run `./tui-capture.sh` with a keyscript that presses ctrl+→ three times and captures; confirm the capture shows `[Previews]` and the row. Attach the capture path in the commit body.

- [ ] **Step 5: Full gates**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && ./test.sh 2>&1 | tail -15
```
Expected: vet+gofmt, unit, e2e all green. Then `nohup ./test.sh race > /tmp/gg-preview-race.log 2>&1 &` and poll `tail -3 /tmp/gg-preview-race.log` until `EXIT=0` (a quiet machine; a 10-minute timeout names an innocent test — rerun rather than "fix").

- [ ] **Step 6: Commit docs + deliver the verify binary**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-merge-preview && git add CHANGELOG.md README.md CLAUDE.md docs/CLAUDE-details.md docs/web-tui-parity.md && git commit -m "docs: merge previews (tab, gg preview verbs, web group)" -m "Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01U5gJ1kJ3VCakAf9FtDxmrN"
```
Build `./build.sh linux` in the worktree and send the resulting binary path to the user (SendUserFile + absolute path) for hands-on verification. Do NOT merge; the user merges (memory: ask first, `--no-ff`, run `./build.sh install` after).

---

## Self-review

**Spec coverage:** semantics (T2/T4), store (T1/T3), domain queries incl. five states + cache (T4), TUI tab + rows + states (T6), open + re-arm + f inert + title (T7), keys a/e/d/s + footer/menu/help (T8; the spec's `r` rename became `e` because `r` is the global reload key — stated in the plan), pair-picker dialog (T9), dynamic refresh chain (T7), CLI (T5), web API + client + live (T10/T11), i18n (every TUI task), error handling (duplicate → focus; unknown name → refused; disabled store → empty tab), docs (T12). Out-of-scope items untouched.

**Type consistency:** `previewRow{rec, sum, err}` (T6) is what T7/T8 read; `previewOpenState`/`previewOpenMsg`/`openPreviewCmd(id, source, target, keepPath)` used identically in T7/T8/T9; `previewMutatedMsg`/`previewAddCmd(source, target, label, open, fromTab)` in T8/T9; domain names `PreviewAdd/List/Get/Rename/Remove/Summary/Open`, `PreviewSummary{State, SourceHash, TargetHash, Files, Ahead}`, `PreviewEndpoints{Summary, Left, Right}`, `PreviewState.String()` values match across T4/T5/T10/T11.

**Known judgment calls for the executor:** `fuzzy.Match` field name, `FakeRunner` call-count helper name, `gittest.Run/Output` helper names, `keyMsg` coverage of ctrl+arrows, the modal's esc→option mapping (`cancel` vs `abort`), `registerHelp`'s signature. Each is a one-grep check called out inline.
