# Compare Two Commit Entries (Bookmarks / Shelved Commits) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Compare two commit entries (commit bookmarks and/or shelved commits) as a whole-tree compare — live sha-vs-sha while both commits exist, falling back to a shelved entry's frozen tar when its sha is gc'd — surfaced in the TUI `g`/`G` switchers (`m` mark-two, `c` cross-picker) and the existing `gg compare` CLI command.

**Architecture:** A new `model.EndpointShelf` endpoint kind makes a frozen shelf entry a first-class side of the existing whole-tree compare (`domain.CompareFiles`, TUI `openCompareFiles`, CLI `gg compare`). A domain resolver (`ResolveCommitEntryEndpoint`) implements the hybrid live/frozen decision per side. Spec: `docs/superpowers/specs/2026-07-31-compare-commit-entries-design.md`.

**Tech Stack:** Go 1.26, real-git tests in `t.TempDir()`, Bubble Tea TUI, TOML i18n bundles.

## Global Constraints

- Work in the worktree `/mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries` on branch `feat/compare-commit-entries`. **Every** build/test command starts with `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries &&`. Use the worktree's absolute path for every file Write/Edit.
- `internal/tui` and `internal/cli` never import `internal/git` (archtest); they reach git through `internal/domain`.
- Every new user-visible TUI string must be an `i18n.T("<english literal>")` call AND have the key present in all four bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`). Removed English strings must also be removed from all four bundles (the orphan check fails otherwise).
- TDD: write the failing test first in every task.
- Frontends never call `internal/git` verbs or assemble `OpDeps`; reads go through `domain` queries.
- Do not wrap a gated domain query inside another `query(...)` closure — nested Read reservations can deadlock behind a queued writer (writer-preferring FIFO gate).
- Commit after every task (small commits; message style `feat(scope): ...` / `test(scope): ...` as in `git log`).

---

### Task 1: `model.EndpointShelf`

**Files:**
- Modify: `internal/model/model.go:236-295` (Endpoint kind/fields/methods)
- Test: create `internal/model/endpoint_shelf_test.go`

**Interfaces:**
- Produces: `model.EndpointShelf` (new `EndpointKind` const), `Endpoint.ShelfID string` field; `Endpoint.FileRef/CacheTag/Display/IsLive` behavior for the new kind. Later tasks construct `model.Endpoint{Kind: model.EndpointShelf, ShelfID: "<entry-id>"}`.

- [ ] **Step 1: Write the failing test**

Create `internal/model/endpoint_shelf_test.go`:

```go
package model

import "testing"

func TestEndpointShelf(t *testing.T) {
	e := Endpoint{Kind: EndpointShelf, ShelfID: "commit-1a2b3c4-deadbeef"}

	if e.IsLive() {
		t.Error("a frozen shelf endpoint must not be live (it is immutable and cacheable)")
	}
	if got, want := e.CacheTag(), "shelf:commit-1a2b3c4-deadbeef"; got != want {
		t.Errorf("CacheTag = %q, want %q (prefixed: must never collide with a sha)", got, want)
	}
	ref := e.FileRef("dir/file.go")
	if ref.Source != SourceShelf || ref.Locator != "commit-1a2b3c4-deadbeef" || ref.Path != "dir/file.go" {
		t.Errorf("FileRef = %+v, want SourceShelf/entry-id/path", ref)
	}
	if got, want := e.Display(), "shelf #commit-1a (frozen)"; got != want {
		t.Errorf("Display = %q, want %q", got, want)
	}
	// A short id is not truncated.
	short := Endpoint{Kind: EndpointShelf, ShelfID: "ab"}
	if got, want := short.Display(), "shelf #ab (frozen)"; got != want {
		t.Errorf("Display = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && go test ./internal/model/ -run TestEndpointShelf -v`
Expected: FAIL (undefined: `EndpointShelf`, unknown field `ShelfID`)

- [ ] **Step 3: Implement**

In `internal/model/model.go`, extend the const block:

```go
const (
	EndpointWorkTree EndpointKind = iota // the working tree (unstaged)
	EndpointIndex                        // the index (staged)
	EndpointCommit                       // a commit, by Hash
	EndpointShelf                        // a shelved commit's frozen changed-file set, by ShelfID
)
```

Extend the struct:

```go
// Endpoint names one side of a whole-tree comparison.
type Endpoint struct {
	Kind    EndpointKind
	Hash    string // commit hash when Kind == EndpointCommit; "" otherwise
	ShelfID string // shelf entry id when Kind == EndpointShelf; "" otherwise
}
```

In `Display()` add a case BEFORE the default:

```go
	case EndpointShelf:
		id := e.ShelfID
		if len(id) > 9 {
			id = id[:9]
		}
		return "shelf #" + id + " (frozen)"
```

In `FileRef(path)` add a case BEFORE the default:

```go
	case EndpointShelf:
		return FileRef{Source: SourceShelf, Locator: e.ShelfID, Path: path}
```

`IsLive()` is already correct (shelf is neither worktree nor index). In `CacheTag()` add a case BEFORE the default:

```go
	case EndpointShelf:
		return "shelf:" + e.ShelfID
	```

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && go test ./internal/model/ -v -run TestEndpoint`
Expected: PASS (including any pre-existing Endpoint tests)

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && git add internal/model/ && git commit -m "feat(model): EndpointShelf — a frozen shelf entry as a compare side"
```

---

### Task 2: Domain — `ResolveCommitEntryEndpoint` + shelf-aware `CompareFiles`

**Files:**
- Create: `internal/domain/compare_entries.go`
- Modify: `internal/domain/query.go:361-387` (`CompareFiles` branch)
- Test: create `internal/domain/compare_entries_test.go`

**Interfaces:**
- Consumes: `model.EndpointShelf`/`ShelfID` (Task 1); existing `CommitLookup`, `ShelfCommitFiles`, `TreeFiles`, `ShowFile`, `ResolveBytes`.
- Produces:
  - `func (s *Service) ResolveCommitEntryEndpoint(ctx context.Context, sha, shelfID string) (model.Endpoint, error)`
  - `type CommitGoneError struct{ SHA string }` (pointer receiver `Error()`)
  - `CompareFiles` accepting shelf endpoints (any mix of shelf/commit).

- [ ] **Step 1: Write the failing tests**

Create `internal/domain/compare_entries_test.go`. Reuse the package's helpers: `newRealRepo(t)`, `gitRun(t, dir, ...)`, `headHash(t, dir)`, `commitTwoFiles(t, dir)` (defined in `shelf_files_test.go`), and `shelf.NewFileStore(t.TempDir())` via `svc.SetShelfStore`.

```go
package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/shelf"
)

// writeAndCommit overwrites paths (contents keyed by path) and commits them
// as one commit, returning its sha.
func writeAndCommit(t *testing.T, dir, msg string, files map[string]string) string {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", msg)
	return headHash(t, dir)
}

func TestResolveCommitEntryEndpoint(t *testing.T) {
	dir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()
	sha := commitTwoFiles(t, dir)

	// Live sha → EndpointCommit carrying the FULL stored sha.
	ep, err := svc.ResolveCommitEntryEndpoint(ctx, sha, "some-shelf-id")
	if err != nil {
		t.Fatalf("live resolve: %v", err)
	}
	if ep.Kind != model.EndpointCommit || ep.Hash != sha {
		t.Fatalf("live resolve = %+v, want EndpointCommit with full sha %s", ep, sha)
	}

	// Gone sha + shelf id → frozen fallback.
	gone := "0123456789abcdef0123456789abcdef01234567"
	ep, err = svc.ResolveCommitEntryEndpoint(ctx, gone, "entry-1")
	if err != nil {
		t.Fatalf("frozen resolve: %v", err)
	}
	if ep.Kind != model.EndpointShelf || ep.ShelfID != "entry-1" {
		t.Fatalf("frozen resolve = %+v, want EndpointShelf entry-1", ep)
	}

	// Gone sha, no shelf id (a bookmark) → typed error naming the short sha.
	_, err = svc.ResolveCommitEntryEndpoint(ctx, gone, "")
	var cg *CommitGoneError
	if !errors.As(err, &cg) {
		t.Fatalf("bookmark gone: err = %v, want *CommitGoneError", err)
	}
	if cg.SHA != gone {
		t.Fatalf("CommitGoneError.SHA = %q, want the full sha", cg.SHA)
	}
}

func TestCompareFilesShelfShelf(t *testing.T) {
	dir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()

	// Commit A changes shared.txt + only-a.txt; commit B changes shared.txt
	// (different bytes), same.txt (same bytes as A's version? no — same.txt is
	// identical in both) + only-b.txt.
	shaA := writeAndCommit(t, dir, "A", map[string]string{
		"shared.txt": "from-a\n",
		"same.txt":   "identical\n",
		"only-a.txt": "a\n",
	})
	shaB := writeAndCommit(t, dir, "B", map[string]string{
		"shared.txt": "from-b\n",
		"same.txt":   "identical\n",
		"only-b.txt": "b\n",
	})

	ea, err := svc.ShelfAddCommit(ctx, shaA, "")
	if err != nil {
		t.Fatal(err)
	}
	eb, err := svc.ShelfAddCommit(ctx, shaB, "")
	if err != nil {
		t.Fatal(err)
	}

	files, err := svc.CompareFiles(ctx,
		model.Endpoint{Kind: model.EndpointShelf, ShelfID: ea.ID},
		model.Endpoint{Kind: model.EndpointShelf, ShelfID: eb.ID})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = f.Status
	}
	// B's commit changed shared.txt+same.txt+only-b.txt, so its tar holds
	// those three. A's tar holds shared.txt+same.txt+only-a.txt.
	want := map[string]string{
		"shared.txt": "M", // in both tars, different bytes
		"only-a.txt": "D", // only in left tar
		"only-b.txt": "A", // only in right tar
		// same.txt: in both tars with identical bytes → omitted
	}
	if len(got) != len(want) {
		t.Fatalf("files = %v, want exactly %v", got, want)
	}
	for p, s := range want {
		if got[p] != s {
			t.Errorf("%s = %q, want %q", p, got[p], s)
		}
	}
}

func TestCompareFilesShelfVsCommit(t *testing.T) {
	dir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()

	shaA := writeAndCommit(t, dir, "A", map[string]string{
		"changed.txt": "old\n",
		"gone.txt":    "will vanish\n",
	})
	ea, err := svc.ShelfAddCommit(ctx, shaA, "")
	if err != nil {
		t.Fatal(err)
	}
	// The newer commit rewrites changed.txt and deletes gone.txt.
	gitRun(t, dir, "rm", "gone.txt")
	shaB := writeAndCommit(t, dir, "B", map[string]string{"changed.txt": "new\n"})

	// shelf (left/older) vs commit (right/newer): scoped to the shelf members.
	files, err := svc.CompareFiles(ctx,
		model.Endpoint{Kind: model.EndpointShelf, ShelfID: ea.ID},
		model.Endpoint{Kind: model.EndpointCommit, Hash: shaB})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = f.Status
	}
	if got["changed.txt"] != "M" || got["gone.txt"] != "D" || len(got) != 2 {
		t.Fatalf("files = %v, want changed.txt=M gone.txt=D only", got)
	}

	// Reversed order (commit older, shelf newer): the vanished member reads as added.
	files, err = svc.CompareFiles(ctx,
		model.Endpoint{Kind: model.EndpointCommit, Hash: shaB},
		model.Endpoint{Kind: model.EndpointShelf, ShelfID: ea.ID})
	if err != nil {
		t.Fatal(err)
	}
	got = map[string]string{}
	for _, f := range files {
		got[f.Path] = f.Status
	}
	if got["changed.txt"] != "M" || got["gone.txt"] != "A" || len(got) != 2 {
		t.Fatalf("reversed files = %v, want changed.txt=M gone.txt=A only", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && go test ./internal/domain/ -run 'TestResolveCommitEntryEndpoint|TestCompareFilesShelf' -v`
Expected: FAIL (undefined: `ResolveCommitEntryEndpoint`, `CommitGoneError`; `CompareFiles` errors on the shelf kind)

- [ ] **Step 3: Implement**

Create `internal/domain/compare_entries.go`:

```go
package domain

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	"github.com/homeend/gigagit/internal/model"
)

// CommitGoneError reports a commit-entry compare side whose sha no longer
// resolves and which has no frozen fallback (a bookmark stores no blobs).
// Frontends show a precise notice from it.
type CommitGoneError struct{ SHA string }

func (e *CommitGoneError) Error() string {
	sha := e.SHA
	if len(sha) > 7 {
		sha = sha[:7]
	}
	return fmt.Sprintf("commit %s no longer exists", sha)
}

// ResolveCommitEntryEndpoint turns one side of a commit-entry comparison into
// a compare endpoint (hybrid semantics): the live sha while it resolves, the
// frozen tar (EndpointShelf) when a shelved side's sha is gone, and a
// CommitGoneError when a bookmark's sha is gone. sha must be the FULL sha the
// entry stores — CommitLookup serves only as the existence probe (it returns
// a short sha, which must not leak into the endpoint). Resolution is strictly
// per side, so mixed states compose: a shelf↔shelf pair with one gc'd sha
// becomes frozen↔live and lands in the shelf↔commit compare lane.
func (s *Service) ResolveCommitEntryEndpoint(ctx context.Context, sha, shelfID string) (model.Endpoint, error) {
	_, found, err := s.CommitLookup(ctx, sha)
	if err != nil {
		return model.Endpoint{}, err
	}
	if found {
		return model.Endpoint{Kind: model.EndpointCommit, Hash: sha}, nil
	}
	if shelfID != "" {
		return model.Endpoint{Kind: model.EndpointShelf, ShelfID: shelfID}, nil
	}
	return model.Endpoint{}, &CommitGoneError{SHA: sha}
}

// shelfCompareFiles lists the files that differ when at least one side is a
// frozen shelf entry (left = older, right = newer, tree-diff conventions:
// only-in-left → D, only-in-right → A, differing bytes → M, identical →
// omitted). shelf↔commit is scoped to the shelf's member paths — the frozen
// tar cannot speak for paths the shelved commit never changed. Deliberately
// NOT wrapped in one query(): each underlying read (ShelfCommitFiles,
// TreeFiles, ShowFile, ResolveBytes) takes its own Read reservation, and
// nesting a gated read inside a held reservation can deadlock behind a
// queued writer.
func (s *Service) shelfCompareFiles(ctx context.Context, left, right model.Endpoint) ([]model.CommitFile, error) {
	if left.Kind == model.EndpointShelf && right.Kind == model.EndpointShelf {
		return s.shelfShelfCompare(ctx, left.ShelfID, right.ShelfID)
	}
	if left.Kind == model.EndpointShelf {
		return s.shelfCommitCompare(ctx, left.ShelfID, right.Hash, false)
	}
	return s.shelfCommitCompare(ctx, right.ShelfID, left.Hash, true)
}

func (s *Service) shelfShelfCompare(ctx context.Context, leftID, rightID string) ([]model.CommitFile, error) {
	lf, err := s.ShelfCommitFiles(ctx, leftID)
	if err != nil {
		return nil, err
	}
	rf, err := s.ShelfCommitFiles(ctx, rightID)
	if err != nil {
		return nil, err
	}
	inRight := make(map[string]bool, len(rf))
	for _, f := range rf {
		inRight[f.Path] = true
	}
	inLeft := make(map[string]bool, len(lf))
	var out []model.CommitFile
	for _, f := range lf {
		inLeft[f.Path] = true
		if !inRight[f.Path] {
			out = append(out, model.CommitFile{Status: "D", Path: f.Path})
			continue
		}
		lb, err := s.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: leftID, Path: f.Path})
		if err != nil {
			return nil, err
		}
		rb, err := s.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: rightID, Path: f.Path})
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(lb, rb) {
			out = append(out, model.CommitFile{Status: "M", Path: f.Path})
		}
	}
	for _, f := range rf {
		if !inLeft[f.Path] {
			out = append(out, model.CommitFile{Status: "A", Path: f.Path})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// shelfCommitCompare compares a frozen shelf entry against a live commit,
// scoped to the shelf's members. shelfIsRight names the direction: false =
// shelf is the left/older side (a member missing from the commit tree reads
// as deleted), true = shelf is the right/newer side (missing reads as added).
func (s *Service) shelfCommitCompare(ctx context.Context, shelfID, commitHash string, shelfIsRight bool) ([]model.CommitFile, error) {
	members, err := s.ShelfCommitFiles(ctx, shelfID)
	if err != nil {
		return nil, err
	}
	tree, err := s.TreeFiles(ctx, commitHash)
	if err != nil {
		return nil, err
	}
	inTree := make(map[string]bool, len(tree))
	for _, f := range tree {
		inTree[f.Path] = true
	}
	missing := "D"
	if shelfIsRight {
		missing = "A"
	}
	var out []model.CommitFile
	for _, f := range members {
		if !inTree[f.Path] {
			out = append(out, model.CommitFile{Status: missing, Path: f.Path})
			continue
		}
		sb, err := s.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: shelfID, Path: f.Path})
		if err != nil {
			return nil, err
		}
		cb, err := s.ShowFile(ctx, commitHash, f.Path)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(sb, cb) {
			out = append(out, model.CommitFile{Status: "M", Path: f.Path})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
```

In `internal/domain/query.go`, add the branch at the TOP of `CompareFiles` (before the `query(...)` call — see the deadlock note in the Global Constraints):

```go
func (s *Service) CompareFiles(ctx context.Context, left, right model.Endpoint) ([]model.CommitFile, error) {
	if left.Kind == model.EndpointShelf || right.Kind == model.EndpointShelf {
		return s.shelfCompareFiles(ctx, left, right)
	}
	return query(ctx, s, "compare-files:"+left.CacheTag()+":"+right.CacheTag(), func(ctx context.Context) ([]model.CommitFile, error) {
		// ... existing body unchanged ...
	})
}
```

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && go test ./internal/domain/ -run 'TestResolveCommitEntryEndpoint|TestCompareFilesShelf|TestCompareFiles' -v`
Expected: PASS (new tests plus the pre-existing `TestCompareFilesIncludesUntracked`/`TestCompareFilesGatedQuery`)

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && git add internal/domain/ && git commit -m "feat(domain): hybrid commit-entry endpoint resolver + shelf-aware CompareFiles"
```

---

### Task 3: Domain — `ComparePatch` (unified diff for any endpoint pair)

**Files:**
- Modify: `internal/domain/compare_entries.go` (add `ComparePatch` + frozen patch generation)
- Modify: `internal/mcp/compare.go:138-166` (move `relabelDiff` to domain, keep MCP behavior identical)
- Test: extend `internal/domain/compare_entries_test.go`; MCP tests must stay green.

**Interfaces:**
- Consumes: `shelfCompareFiles` (Task 2), existing `DiffPatch(ctx, model.DiffSpec)`, `DiffNoIndex(ctx, a, b string)`.
- Produces:
  - `func (s *Service) ComparePatch(ctx context.Context, left, right model.Endpoint) (string, error)`
  - `func RelabelNoIndexDiff(diff, leftDisplay, rightDisplay string) string` (exported from domain; MCP now calls it)

- [ ] **Step 1: Write the failing test**

Append to `internal/domain/compare_entries_test.go`:

```go
func TestComparePatchFrozen(t *testing.T) {
	dir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()

	shaA := writeAndCommit(t, dir, "A", map[string]string{"f.txt": "old\n"})
	ea, err := svc.ShelfAddCommit(ctx, shaA, "")
	if err != nil {
		t.Fatal(err)
	}
	shaB := writeAndCommit(t, dir, "B", map[string]string{"f.txt": "new\n"})

	patch, err := svc.ComparePatch(ctx,
		model.Endpoint{Kind: model.EndpointShelf, ShelfID: ea.ID},
		model.Endpoint{Kind: model.EndpointCommit, Hash: shaB})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--- a/f.txt", "+++ b/f.txt", "-old", "+new"} {
		if !strings.Contains(patch, want) {
			t.Errorf("patch missing %q:\n%s", want, patch)
		}
	}
	if strings.Contains(patch, os.TempDir()) {
		t.Errorf("patch leaks temp paths:\n%s", patch)
	}
}

func TestComparePatchLiveCommits(t *testing.T) {
	dir, svc := newRealRepo(t)
	ctx := context.Background()
	shaA := writeAndCommit(t, dir, "A", map[string]string{"f.txt": "old\n"})
	shaB := writeAndCommit(t, dir, "B", map[string]string{"f.txt": "new\n"})

	patch, err := svc.ComparePatch(ctx,
		model.Endpoint{Kind: model.EndpointCommit, Hash: shaA},
		model.Endpoint{Kind: model.EndpointCommit, Hash: shaB})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "-old") || !strings.Contains(patch, "+new") {
		t.Errorf("live patch wrong:\n%s", patch)
	}
}
```

Add `"os"` and `"strings"` to the test file's imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && go test ./internal/domain/ -run TestComparePatch -v`
Expected: FAIL (undefined: `ComparePatch`)

- [ ] **Step 3: Implement**

Append to `internal/domain/compare_entries.go` (add `"os"`, `"path/filepath"`, `"strings"` imports):

```go
// ComparePatch renders a unified diff for an endpoint pair. Live pairs go
// through git directly (one invocation); a pair involving a frozen shelf
// side is materialized per differing file into temp files and diffed with
// git diff --no-index, headers relabelled to a/<path> b/<path> (the MCP
// gg_compare_file precedent — git cannot see the tar).
func (s *Service) ComparePatch(ctx context.Context, left, right model.Endpoint) (string, error) {
	if left.Kind != model.EndpointShelf && right.Kind != model.EndpointShelf {
		return s.DiffPatch(ctx, livePairSpec(left, right))
	}
	files, err := s.shelfCompareFiles(ctx, left, right)
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "gg-compare-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	var b strings.Builder
	for i, f := range files {
		lb := s.sideBytes(ctx, left, f.Path)   // nil = absent on that side
		rb := s.sideBytes(ctx, right, f.Path)
		lp := filepath.Join(tmp, fmt.Sprintf("l%d", i))
		rp := filepath.Join(tmp, fmt.Sprintf("r%d", i))
		if err := os.WriteFile(lp, lb, 0o600); err != nil {
			return "", err
		}
		if err := os.WriteFile(rp, rb, 0o600); err != nil {
			return "", err
		}
		diff, err := s.DiffNoIndex(ctx, lp, rp)
		if err != nil {
			return "", err
		}
		b.WriteString(RelabelNoIndexDiff(diff, "a/"+f.Path, "b/"+f.Path))
	}
	return b.String(), nil
}

// sideBytes resolves one endpoint's bytes for path, treating any miss as
// absent (empty) — the file list already told us which side holds the file,
// and a diff against empty renders the full add/delete correctly.
func (s *Service) sideBytes(ctx context.Context, e model.Endpoint, path string) []byte {
	data, err := s.ResolveBytes(ctx, e.FileRef(path))
	if err != nil {
		return nil
	}
	return data
}

// livePairSpec maps a non-shelf endpoint pair onto the DiffSpec vocabulary.
// Callers guarantee the pair is one of the forward forms validComparePair
// accepts (commit↔commit, commit→index, commit→worktree, index→worktree).
func livePairSpec(left, right model.Endpoint) model.DiffSpec {
	switch {
	case left.Kind == model.EndpointCommit && right.Kind == model.EndpointCommit:
		return model.DiffSpec{Rev: left.Hash + ".." + right.Hash}
	case left.Kind == model.EndpointCommit && right.Kind == model.EndpointIndex:
		return model.DiffSpec{Cached: true, Rev: left.Hash}
	case left.Kind == model.EndpointCommit: // → worktree
		return model.DiffSpec{Rev: left.Hash}
	default: // index → worktree
		return model.DiffSpec{}
	}
}

// RelabelNoIndexDiff strips the temp-path noise from git diff --no-index
// output: drops the "diff --git"/"index" header lines and rewrites ---/+++
// to the given display labels. Header rewriting stops at the first @@ hunk
// line so body lines that merely look like headers (e.g. a removed SQL
// comment "-- foo" renders as "--- foo") are never touched. Shared by the
// MCP gg_compare_file tool and ComparePatch's frozen lane.
func RelabelNoIndexDiff(diff, leftDisplay, rightDisplay string) string {
	lines := strings.Split(diff, "\n")
	out := make([]string, 0, len(lines))
	inHeader := true
	for _, ln := range lines {
		if inHeader {
			switch {
			case strings.HasPrefix(ln, "@@"):
				inHeader = false
			case strings.HasPrefix(ln, "diff --git "), strings.HasPrefix(ln, "index "):
				continue
			case strings.HasPrefix(ln, "--- "):
				out = append(out, "--- "+leftDisplay)
				continue
			case strings.HasPrefix(ln, "+++ "):
				out = append(out, "+++ "+rightDisplay)
				continue
			}
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}
```

Note: `RelabelNoIndexDiff` processes ONE file's diff per call (each `DiffNoIndex` call diffs one temp-file pair), so the single `inHeader` latch is correct.

In `internal/mcp/compare.go`: delete the local `relabelDiff` function and replace its call site (`out.UnifiedDiff = relabelDiff(diff, ld, rd)`) with `out.UnifiedDiff = domain.RelabelNoIndexDiff(diff, ld, rd)`. Move any `relabelDiff` unit tests from `internal/mcp` to `internal/domain` (keep the test cases verbatim, renamed to `TestRelabelNoIndexDiff`); if the MCP tests reference it indirectly only, just leave them — they must stay green either way.

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && go test ./internal/domain/ ./internal/mcp/ -v -run 'TestComparePatch|TestRelabel|Compare'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && git add internal/domain/ internal/mcp/ && git commit -m "feat(domain): ComparePatch — unified diff incl. frozen shelf lanes; share RelabelNoIndexDiff with MCP"
```

---

### Task 4: CLI — extend `gg compare` with entry specs and `--patch`

**Files:**
- Modify: `internal/cli/compare.go` (whole file shown below)
- Test: create `internal/cli/compare_entries_test.go`

**Interfaces:**
- Consumes: `ResolveCommitEntryEndpoint`, `ComparePatch`, `CommitGoneError` (Tasks 2-3); existing `BookmarkGet`, `ShelfFind`, `CompareFiles`; CLI harness `Run(workdir, args, stdin, stdout, stderr, cwdFile) int` and test helper `newRepoDir(t)`.
- Produces: `gg compare [--patch] <spec> [<spec>]` accepting `bookmark:<id>` / `shelf:<id>`.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/compare_entries_test.go`. Pattern it on the existing `internal/cli` tests (`newRepoDir(t)` builds a real repo; drive everything through `Run`):

```go
package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// run invokes the CLI in dir and returns (exit, stdout, stderr).
func runCompare(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(dir, args, strings.NewReader(""), &out, &errb, "")
	return code, out.String(), errb.String()
}

func gitc(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// shelfCommitID shelves HEAD and returns the new entry's id, parsed from
// `gg shelf list` (commit entries render with their commit-<short>-<hash8> id).
func shelfCommitID(t *testing.T, dir, sha string) string {
	t.Helper()
	code, _, errb := runCompare(t, dir, "shelf", "commit", sha)
	if code != 0 {
		t.Fatalf("shelf commit: exit %d, stderr %s", code, errb)
	}
	_, out, _ := runCompare(t, dir, "shelf", "list")
	m := regexp.MustCompile(`commit-[0-9a-f]+-[0-9a-f]{8}`).FindString(out)
	if m == "" {
		t.Fatalf("no commit entry id in shelf list output:\n%s", out)
	}
	return m
}

func headSha(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestCompareShelfEntryLive(t *testing.T) {
	dir := newRepoDir(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // isolate the shelf store

	writeFile(t, dir, "f.txt", "old\n")
	gitc(t, dir, "add", "."); gitc(t, dir, "commit", "-m", "A")
	shaA := headSha(t, dir)
	id := shelfCommitID(t, dir, shaA)
	writeFile(t, dir, "f.txt", "new\n")
	gitc(t, dir, "add", "."); gitc(t, dir, "commit", "-m", "B")
	shaB := headSha(t, dir)

	// Live lane: entry sha still exists → plain tree compare, no stderr note.
	code, out, errb := runCompare(t, dir, "compare", "shelf:"+id, shaB)
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, errb)
	}
	if !strings.Contains(out, "M\tf.txt") {
		t.Errorf("stdout = %q, want M\\tf.txt line", out)
	}
	if strings.Contains(errb, "frozen") {
		t.Errorf("live compare must not print the frozen note, got %q", errb)
	}
}

func TestCompareShelfEntryFrozenAndPatch(t *testing.T) {
	dir := newRepoDir(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	writeFile(t, dir, "f.txt", "base\n")
	gitc(t, dir, "add", "."); gitc(t, dir, "commit", "-m", "base")
	baseSha := headSha(t, dir)

	writeFile(t, dir, "f.txt", "doomed\n")
	gitc(t, dir, "add", "."); gitc(t, dir, "commit", "-m", "doomed")
	doomedSha := headSha(t, dir)
	id := shelfCommitID(t, dir, doomedSha)

	// Erase the shelved commit: rewind, expire reflogs, gc.
	gitc(t, dir, "reset", "--hard", baseSha)
	gitc(t, dir, "reflog", "expire", "--expire=now", "--all")
	gitc(t, dir, "gc", "--prune=now")

	// Frozen fallback: list lane.
	code, out, errb := runCompare(t, dir, "compare", "shelf:"+id, baseSha)
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, errb)
	}
	if !strings.Contains(out, "M\tf.txt") {
		t.Errorf("stdout = %q, want M\\tf.txt", out)
	}
	if !strings.Contains(errb, "frozen compare") {
		t.Errorf("stderr = %q, want the frozen note", errb)
	}

	// Frozen fallback: --patch lane (flags precede positionals).
	code, out, _ = runCompare(t, dir, "compare", "--patch", "shelf:"+id, baseSha)
	if code != 0 {
		t.Fatalf("--patch exit %d", code)
	}
	for _, want := range []string{"--- a/f.txt", "+++ b/f.txt", "-doomed", "+base"} {
		if !strings.Contains(out, want) {
			t.Errorf("--patch stdout missing %q:\n%s", want, out)
		}
	}
}

func TestCompareSpecErrors(t *testing.T) {
	dir := newRepoDir(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// Unknown shelf id → usage-level error (exit 2).
	code, _, _ := runCompare(t, dir, "compare", "shelf:nope", "HEAD")
	if code != 2 {
		t.Errorf("unknown shelf id: exit %d, want 2", code)
	}

	// A FILE shelf entry is not a commit entry → exit 2.
	writeFile(t, dir, "plain.txt", "x\n")
	code, _, _ = runCompare(t, dir, "shelf", "add", "plain.txt")
	if code != 0 {
		t.Fatal("shelf add failed")
	}
	_, list, _ := runCompare(t, dir, "shelf", "list")
	fileID := regexp.MustCompile(`unstaged-[0-9a-z-]+-[0-9a-f]{8}`).FindString(list)
	if fileID == "" {
		t.Fatalf("no file entry id in %q", list)
	}
	code, _, errb := runCompare(t, dir, "compare", "shelf:"+fileID, "HEAD")
	if code != 2 || !strings.Contains(errb, "not a commit") {
		t.Errorf("file entry: exit %d stderr %q, want 2 + 'not a commit'", code, errb)
	}
}
```

Note for the implementer: check the actual `gg shelf list` output format first (`go test` failures will show it); adjust the two ID regexes if the real format differs — the IDs come from `internal/shelf/file_store.go` (`commit-<short>-<hash8>` and `<state>-<slug>-<hash8>`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && go test ./internal/cli/ -run TestCompare -v`
Expected: FAIL (`compare` rejects `--patch` / the `shelf:` spec resolves as a commit-ish and errors)

- [ ] **Step 3: Implement**

Replace `cmdCompare` in `internal/cli/compare.go` and add the spec resolver (add imports `"flag"`, `"strings"`, `"github.com/homeend/gigagit/internal/model"` is already there):

```go
// cmdCompare prints the changed-file list (or, with --patch, unified diffs)
// between two endpoints:
//
//	gg compare [--patch] <left> [<right>]
//
// where each endpoint is a commit-ish, @staged, @worktree, or a stored commit
// entry: bookmark:<id> / shelf:<id> (hybrid — the live sha while it exists, a
// shelved entry's frozen tar after a gc; the fallback is noted on stderr).
// <right> defaults to @worktree. List output is one "<status>\t<path>" line
// per changed file.
func cmdCompare(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	patch := fs.Bool("patch", false, "print unified diffs instead of the changed-file list")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	args = fs.Args()
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg compare [--patch] <left> [<right>]   (endpoints: a commit, @staged, @worktree, bookmark:<id>, shelf:<id>; right defaults to @worktree)")
		return 2
	}
	left, code := resolveCompareSpec(svc, args[0], stderr)
	if code != 0 {
		return code
	}
	right := model.Endpoint{Kind: model.EndpointWorkTree}
	if len(args) > 1 {
		if right, code = resolveCompareSpec(svc, args[1], stderr); code != 0 {
			return code
		}
	}
	if !validComparePair(left, right) {
		fmt.Fprintln(stderr, "compare: order endpoints oldest→newest (a commit, then @staged, then @worktree); a frozen shelf entry pairs only with a commit or another shelf entry")
		return 2
	}
	if *patch {
		diff, err := svc.ComparePatch(context.Background(), left, right)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprint(stdout, diff)
		return 0
	}
	files, err := svc.CompareFiles(context.Background(), left, right)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, f := range files {
		if f.OldPath != "" {
			fmt.Fprintf(stdout, "%s\t%s -> %s\n", f.Status, f.OldPath, f.Path)
			continue
		}
		fmt.Fprintf(stdout, "%s\t%s\n", f.Status, f.Path)
	}
	return 0
}

// resolveCompareSpec turns one CLI token into an endpoint. bookmark:<id> and
// shelf:<id> address a stored commit entry and resolve hybrid (live sha while
// it exists, frozen tar for a gc'd shelved commit — noted on stderr so stdout
// stays parseable); anything else is the existing vocabulary
// (@worktree/@staged/commit-ish). The int is an exit code: 0 = resolved,
// 1 = failure (gone bookmark), 2 = usage (unknown id / not a commit entry).
func resolveCompareSpec(svc *domain.Service, tok string, stderr io.Writer) (model.Endpoint, int) {
	ctx := context.Background()
	switch {
	case strings.HasPrefix(tok, "bookmark:"):
		id := strings.TrimPrefix(tok, "bookmark:")
		b, err := svc.BookmarkGet(ctx, id)
		if err != nil {
			fmt.Fprintf(stderr, "compare: bookmark %q: %v\n", id, err)
			return model.Endpoint{}, 2
		}
		if !b.IsCommit() {
			fmt.Fprintf(stderr, "compare: bookmark %q is a file bookmark, not a commit\n", id)
			return model.Endpoint{}, 2
		}
		ep, err := svc.ResolveCommitEntryEndpoint(ctx, b.Commit, "")
		if err != nil {
			fmt.Fprintln(stderr, "compare:", err)
			return model.Endpoint{}, 1
		}
		return ep, 0
	case strings.HasPrefix(tok, "shelf:"):
		id := strings.TrimPrefix(tok, "shelf:")
		e, err := svc.ShelfFind(ctx, id)
		if err != nil {
			fmt.Fprintf(stderr, "compare: shelf %q: %v\n", id, err)
			return model.Endpoint{}, 2
		}
		if !e.IsCommit() {
			fmt.Fprintf(stderr, "compare: shelf entry %q is a file entry, not a commit\n", id)
			return model.Endpoint{}, 2
		}
		ep, err := svc.ResolveCommitEntryEndpoint(ctx, e.Origin.Commit, e.ID)
		if err != nil {
			fmt.Fprintln(stderr, "compare:", err)
			return model.Endpoint{}, 1
		}
		if ep.Kind == model.EndpointShelf {
			sha := e.Origin.Commit
			if len(sha) > 7 {
				sha = sha[:7]
			}
			fmt.Fprintf(stderr, "# frozen compare: commit %s no longer exists\n", sha)
		}
		return ep, 0
	default:
		return parseEndpoint(tok), 0
	}
}
```

Extend `validComparePair` — insert at the top of the function:

```go
	// A frozen shelf side pairs with a commit or another shelf endpoint only:
	// the tar snapshots a commit's changes, so diffing it against the live
	// index/worktree would mix a frozen past with a moving target.
	if left.Kind == model.EndpointShelf || right.Kind == model.EndpointShelf {
		pairable := func(e model.Endpoint) bool {
			return e.Kind == model.EndpointCommit || e.Kind == model.EndpointShelf
		}
		return pairable(left) && pairable(right)
	}
```

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && go test ./internal/cli/ -run TestCompare -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && git add internal/cli/ && git commit -m "feat(cli): gg compare accepts bookmark:/shelf: commit-entry specs + --patch"
```

---

### Task 5: TUI — `m` mark-two on commit entries (both switchers)

**Files:**
- Create: `internal/tui/entry_compare.go`
- Modify: `internal/tui/bookmark_popup.go` (the `case "m":` guard ~line 297-304 and `bookmarkMark` ~line 451-469)
- Modify: `internal/tui/shelf_popup.go` (the `case "m":` guard ~line 274-281 and `shelfPopupMark` ~line 390-413)
- Modify: `internal/tui/model.go` (Model field + msg handler + `reRoot` gen bump)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (new keys — see Step 3)
- Test: create `internal/tui/entry_compare_test.go`

**Interfaces:**
- Consumes: `ResolveCommitEntryEndpoint` (Task 2), existing `openCompareFiles(left, right model.Endpoint)`, `bookmarkDisplay(b)`, `shortShelf(e)`, `m.shelfEntryByID(id)`, popup `byID(id)`.
- Produces (used by Task 6):
  - `type entrySide struct{ sha, shelfID, label string }`
  - `func bookmarkEntrySide(b model.Bookmark) entrySide`
  - `func shelfEntrySide(e model.ShelfEntry) entrySide`
  - `func (m Model) startEntryCompare(left, right entrySide) (Model, tea.Cmd)`
  - `type entryCompareMsg struct{ gen int; left, right model.Endpoint; err error }`
  - Model field `entryCompareGen int`

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/entry_compare_test.go`. Follow the package's existing test conventions (`shelf_popup_test.go` / `bookmark_test.go` construct a `Model` directly and feed `tea.KeyMsg`s; mirror their Model setup — copy the minimal fields those tests set):

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// Two commit bookmarks marked with m must dispatch an entry-compare resolve
// (a tea.Cmd), not a notice.
func TestBookmarkMarkTwoCommitEntries(t *testing.T) {
	m := newTestModel(t) // use the package's existing model-constructor helper; if none exists, mirror bookmark_test.go's setup
	a := model.Bookmark{ID: "b1", Commit: "1111111111111111111111111111111111111111", State: model.StateCommitted}
	b := model.Bookmark{ID: "b2", Commit: "2222222222222222222222222222222222222222", State: model.StateCommitted}
	p := newBookmarkPopup([]model.Bookmark{a, b})
	p.markID = "b1"
	p.sel = 1
	m = m.pushLayer(p)

	mm, cmd := m.bookmarkMark()
	if cmd == nil {
		t.Fatal("marking a second commit bookmark must dispatch the resolve cmd")
	}
	if mm.statusMsg != "" {
		t.Fatalf("unexpected notice: %q", mm.statusMsg)
	}
}

// A mixed pair (file mark + commit second) is a notice, not a compare.
func TestBookmarkMarkMixedKindsRefused(t *testing.T) {
	m := newTestModel(t)
	a := model.Bookmark{ID: "b1", Path: "f.go", State: model.StateUnstaged}
	b := model.Bookmark{ID: "b2", Commit: "2222222222222222222222222222222222222222", State: model.StateCommitted}
	p := newBookmarkPopup([]model.Bookmark{a, b})
	p.markID = "b1"
	p.sel = 1
	m = m.pushLayer(p)

	mm, cmd := m.bookmarkMark()
	if cmd != nil {
		t.Fatal("a mixed pair must not dispatch a compare")
	}
	if mm.statusMsg == "" {
		t.Fatal("a mixed pair must set a notice")
	}
}

// A stale entryCompareMsg (gen mismatch) is dropped.
func TestEntryCompareGenGuard(t *testing.T) {
	m := newTestModel(t)
	m.entryCompareGen = 5
	upd, _ := m.Update(entryCompareMsg{gen: 4, left: model.Endpoint{Kind: model.EndpointCommit, Hash: "a"}, right: model.Endpoint{Kind: model.EndpointCommit, Hash: "b"}})
	mm := upd.(Model)
	if mm.filesView != nil {
		t.Fatal("a stale resolve must not open the compare view")
	}
}

// Same commit on both sides (and not two distinct shelf entries) is a notice.
func TestEntryCompareSelfCompareNotice(t *testing.T) {
	m := newTestModel(t)
	side := entrySide{sha: "3333333333333333333333333333333333333333", label: "x"}
	mm, cmd := m.startEntryCompare(side, side)
	if cmd != nil {
		t.Fatal("self-compare must not dispatch")
	}
	if mm.statusMsg == "" {
		t.Fatal("self-compare must set a notice")
	}
}
```

Before writing, check whether a shared model-constructor helper exists (`grep -rn "func newTestModel" internal/tui/`); if not, mirror the Model construction used at the top of `bookmark_test.go`'s first test and inline it.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && go test ./internal/tui/ -run 'TestBookmarkMarkTwoCommit|TestBookmarkMarkMixed|TestEntryCompare' -v`
Expected: FAIL (undefined `entrySide`/`entryCompareMsg`/`startEntryCompare`; mark-two on commits currently returns the "not available for a commit bookmark" notice path — the m-key guard — or compares bytes)

- [ ] **Step 3: Implement**

Create `internal/tui/entry_compare.go`:

```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// entrySide is one side of a commit-entry comparison: the full sha the entry
// stores, the shelf entry id when the side is a shelved commit ("" for a
// bookmark), and the human label used in notices.
type entrySide struct {
	sha     string
	shelfID string
	label   string
}

func bookmarkEntrySide(b model.Bookmark) entrySide {
	return entrySide{sha: b.Commit, label: bookmarkDisplay(b)}
}

func shelfEntrySide(e model.ShelfEntry) entrySide {
	return entrySide{sha: e.Origin.Commit, shelfID: e.ID, label: i18n.T("shelf #%s", shortShelf(e))}
}

// entryCompareMsg carries both resolved endpoints (or the failure) back to
// the UI thread; gen-guarded by Model.entryCompareGen.
type entryCompareMsg struct {
	gen         int
	left, right model.Endpoint
	err         error
}

// startEntryCompare resolves both sides off the UI thread (hybrid: the live
// sha while it exists, a shelved side's frozen tar after a gc) and then opens
// the whole-tree compare files view. First pick = left/older. Gen-guarded so
// a resolve landing after switcher close or reRoot is dropped.
func (m Model) startEntryCompare(left, right entrySide) (Model, tea.Cmd) {
	// Same commit on both sides is a non-compare — except two DIFFERENT shelf
	// entries of the same commit, whose frozen sets may legitimately differ.
	distinctShelves := left.shelfID != "" && right.shelfID != "" && left.shelfID != right.shelfID
	if left.sha == right.sha && !distinctShelves {
		m.statusMsg = i18n.T("select a different commit to compare against")
		return m, nil
	}
	m.entryCompareGen++
	gen := m.entryCompareGen
	svc := m.svc
	return m, func() tea.Msg {
		ctx := context.Background()
		l, err := svc.ResolveCommitEntryEndpoint(ctx, left.sha, left.shelfID)
		if err != nil {
			return entryCompareMsg{gen: gen, err: err}
		}
		r, err := svc.ResolveCommitEntryEndpoint(ctx, right.sha, right.shelfID)
		if err != nil {
			return entryCompareMsg{gen: gen, err: err}
		}
		return entryCompareMsg{gen: gen, left: l, right: r}
	}
}
```

In `internal/tui/model.go`:

1. Add the Model field next to `pickGen` (find it with `grep -n "pickGen" internal/tui/model.go`):

```go
	entryCompareGen int // drops stale commit-entry compare resolves (the pickGen pattern)
```

2. Add the msg handler in `Update`'s message switch (near the `shelfLoadedMsg` case):

```go
	case entryCompareMsg:
		if msg.gen != m.entryCompareGen {
			return m, nil
		}
		if msg.err != nil {
			m.statusMsg = i18n.T("compare: %s", msg.err.Error())
			return m, nil
		}
		m = m.clearLayers() // the files view is not a layer; the switchers must not draw over it
		return m.openCompareFiles(msg.left, msg.right)
```

(`"compare: %s"` already exists in all four bundles — no bundle change for it.)

3. In `reRoot`, alongside the existing `pickGen` bump (`grep -n "pickGen++" internal/tui/*.go`), add `m.entryCompareGen++`. Also add `m.entryCompareGen++` right next to every `m.pickGen++` inside `bookmark_popup.go` and `shelf_popup.go` (the esc/enter cases) so closing a switcher also invalidates an in-flight resolve.

4. In `bookmark_popup.go`, the `case "m":` handler — REMOVE the `commitBookmarkNotice` guard (keep the compare-mode guard):

```go
		case "m":
			if p.compareRef != nil {
				return m, nil
			}
			return m.bookmarkMark()
```

5. Rewrite the tail of `bookmarkMark` (after the toggle branch):

```go
	a, okA := p.byID(p.markID)
	b2, okB := p.byID(b.ID)
	if !okA || !okB {
		return m, nil
	}
	switch {
	case a.IsCommit() && b2.IsCommit():
		return m.startEntryCompare(bookmarkEntrySide(a), bookmarkEntrySide(b2))
	case a.IsCommit() != b2.IsCommit():
		m.statusMsg = i18n.T("marked entries are different kinds — mark two files or two commits")
		return m, nil
	}
	return m.openBookmarkCompareTwo(p.markID, b.ID)
```

(Check `byID`'s exact name/signature on the popup — `grep -n "func (p \*bookmarkPopup) byID" internal/tui/bookmark_popup.go` — and adapt.)

6. In `shelf_popup.go`, the `case "m":` handler — REMOVE the `commitShelfNotice` guard the same way; rewrite the tail of `shelfPopupMark`:

```go
	a, okA := m.shelfEntryByID(p.markID)
	b, okB := m.shelfEntryByID(e.ID)
	if !okA || !okB {
		return m, nil
	}
	switch {
	case a.IsCommit() && b.IsCommit():
		return m.startEntryCompare(shelfEntrySide(a), shelfEntrySide(b))
	case a.IsCommit() != b.IsCommit():
		m.statusMsg = i18n.T("marked entries are different kinds — mark two files or two commits")
		return m, nil
	}
	return m.openShelfCompareTwoEntries(a, b)
```

7. i18n: add ONE new key to all four bundles (alphabetical/nearby-key placement, matching each file's ordering convention):

- `ja.toml`: `"marked entries are different kinds — mark two files or two commits" = "マークした項目は種類が異なります — ファイル2つ、またはコミット2つをマークしてください"`
- `ko.toml`: `"marked entries are different kinds — mark two files or two commits" = "표시한 항목의 종류가 서로 다릅니다 — 파일 2개 또는 커밋 2개를 표시하세요"`
- `zh.toml`: `"marked entries are different kinds — mark two files or two commits" = "标记的条目类型不同 — 请标记两个文件或两个提交"`
- `ru.toml`: `"marked entries are different kinds — mark two files or two commits" = "отмеченные записи разного вида — отметьте два файла или два коммита"`

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && go test ./internal/tui/ ./internal/i18n/ -run 'TestBookmarkMark|TestEntryCompare|TestShelf|I18n|Scan' -v`
Expected: PASS (new tests + the pre-existing mark/i18n-scan tests; the scan test verifies bundle coverage)

- [ ] **Step 5: Run the full TUI package**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && go test ./internal/tui/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && git add internal/tui/ internal/i18n/ && git commit -m "feat(tui): m mark-two compares two commit entries (bookmarks/shelved commits)"
```

---

### Task 6: TUI — cross-picker `c` (commit bookmark ↔ shelved commit)

**Files:**
- Modify: `internal/tui/bookmark_compare.go` (pendingCompare commit flavor)
- Modify: `internal/tui/bookmark_popup.go` (fields, `case "c":`, compare-mode `enter`, compare-mode guards)
- Modify: `internal/tui/shelf_popup.go` (fields, `shelfCompareAgainstBookmark`, compare-mode `enter`, guards)
- Modify: `internal/tui/model.go` (compareEntry stamping in `shelfLoadedMsg`/`bookmarksLoadedMsg`)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (one new key)
- Test: extend `internal/tui/entry_compare_test.go`

**Interfaces:**
- Consumes: `entrySide`, `bookmarkEntrySide`, `shelfEntrySide`, `startEntryCompare` (Task 5).
- Produces: `pendingCompare.entry *entrySide`; `bookmarkPopup.compareEntry *entrySide` + `func (p *bookmarkPopup) inCompareMode() bool`; same pair on `shelfPopup`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/entry_compare_test.go`:

```go
// c on a commit bookmark must arm a commit-flavored pendingCompare targeting
// the shelf picker.
func TestBookmarkCommitCrossCompareArm(t *testing.T) {
	m := newTestModel(t)
	b := model.Bookmark{ID: "b1", Commit: "1111111111111111111111111111111111111111", State: model.StateCommitted}
	p := newBookmarkPopup([]model.Bookmark{b})
	m = m.pushLayer(p)

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}) // keys route to the top layer through Model.Update
	got := mm.(Model)
	if got.pendingCompare == nil || got.pendingCompare.entry == nil {
		t.Fatalf("pendingCompare = %+v, want a commit-flavored arm", got.pendingCompare)
	}
	if got.pendingCompare.target != compareShelf {
		t.Errorf("target = %v, want compareShelf", got.pendingCompare.target)
	}
	if cmd == nil {
		t.Error("c must dispatch the shelf load")
	}
}

// In commit-flavored compare mode, enter on a FILE entry is a notice.
func TestShelfCompareModeCommitVsFileRefused(t *testing.T) {
	m := newTestModel(t)
	fileEntry := model.ShelfEntry{ID: "s1", Origin: model.FileAddress{State: model.StateUnstaged, Path: "f.go"}}
	p := newShelfPopup([]model.ShelfEntry{fileEntry})
	side := entrySide{sha: "1111111111111111111111111111111111111111", label: "bm"}
	p.compareEntry = &side
	m = m.pushLayer(p)

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := mm.(Model)
	if cmd != nil {
		t.Fatal("commit-vs-file must not dispatch a compare")
	}
	if got.statusMsg == "" {
		t.Fatal("commit-vs-file must set a notice")
	}
}
```

If `m.Update` doesn't reach the popup in the test harness (compare with how `bookmark_test.go` sends keys — `grep -n 'KeyRunes' internal/tui/bookmark_test.go`), mirror the existing tests' dispatch exactly.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && go test ./internal/tui/ -run 'TestBookmarkCommitCross|TestShelfCompareModeCommit' -v`
Expected: FAIL (no `entry` field on pendingCompare; no `compareEntry` on the popups; `c` on a commit bookmark currently sets the "not available" notice)

- [ ] **Step 3: Implement**

1. `internal/tui/bookmark_compare.go` — extend the struct:

```go
type pendingCompare struct {
	ref    model.FileRef
	entry  *entrySide // non-nil = the first pick is a commit entry (ref is then unused)
	label  string
	target comparePopupKind
}
```

2. Popup fields + helper, in `bookmark_popup.go` (next to `compareRef`):

```go
	compareEntry *entrySide // commit-entry compare mode: the first pick (nil = none)
```

```go
// inCompareMode reports whether this switcher was opened to pick the second
// side of a comparison (file- or commit-flavored); action keys are inert then.
func (p *bookmarkPopup) inCompareMode() bool { return p.compareRef != nil || p.compareEntry != nil }
```

Mirror both on `shelfPopup` in `shelf_popup.go`.

3. Replace EVERY `if p.compareRef != nil {` guard in both popups' key handlers (the x/p/m/c/e/t/a/y cases and the `?` help argument) with `if p.inCompareMode() {`, and pass `p.inCompareMode()` to `bookmarkSwitcherHelp(...)`/`shelfSwitcherHelp(...)`. Find them all: `grep -n "compareRef != nil" internal/tui/bookmark_popup.go internal/tui/shelf_popup.go internal/tui/shelf_actions.go`. The compare-mode `enter` branches keep their explicit `compareRef`/`compareEntry` routing (next point). Also check the popups' render functions for a compare-mode banner conditioned on `compareRef` and switch it to `inCompareMode()` (the banner text uses `compareLabel`, which both flavors set).

4. Compare-mode `enter` in `bookmark_popup.go` (insert BEFORE the existing `if p.compareRef != nil` block):

```go
		if p.compareEntry != nil {
			b, ok := p.selected()
			if !ok {
				return m, nil
			}
			if !b.IsCommit() {
				m.statusMsg = i18n.T("cannot compare a commit against a file")
				return m, nil
			}
			return m.startEntryCompare(*p.compareEntry, bookmarkEntrySide(b))
		}
```

Mirror in `shelf_popup.go`'s `enter` (before its `p.compareRef != nil` block):

```go
		if p.compareEntry != nil {
			if !e.IsCommit() {
				m.statusMsg = i18n.T("cannot compare a commit against a file")
				return m, nil
			}
			return m.startEntryCompare(*p.compareEntry, shelfEntrySide(e))
		}
```

5. Arming `c` on a commit entry. In `bookmark_popup.go`'s `case "c":` — remove the `commitBookmarkNotice` guard and branch:

```go
		case "c":
			if p.inCompareMode() {
				return m, nil
			}
			b, ok := p.selected()
			if !ok {
				return m, nil
			}
			if b.IsCommit() {
				side := bookmarkEntrySide(b)
				m.pendingCompare = &pendingCompare{entry: &side, label: side.label, target: compareShelf}
				return m, m.loadShelfCmd(true)
			}
			m.pendingCompare = &pendingCompare{ref: bookmarkToFileRef(b), label: bookmarkDisplay(b), target: compareShelf}
			return m, m.loadShelfCmd(true)
```

In `shelf_actions.go`'s `shelfCompareAgainstBookmark` (and remove the `commitShelfNotice` guard from `shelf_popup.go`'s `case "c":`):

```go
	if e.IsCommit() {
		side := shelfEntrySide(e)
		m.pendingCompare = &pendingCompare{entry: &side, label: side.label, target: compareBookmark}
		return m, m.loadBookmarksCmd()
	}
```

(placed before the existing `ref := model.FileRef{...}` tail, which stays for file entries).

6. Stamping in `model.go` — both loaded-msg handlers gain the entry flavor. `shelfLoadedMsg` (~line 665):

```go
			if pc := m.pendingCompare; pc != nil && pc.target == compareShelf {
				if pc.entry != nil {
					p.compareEntry = pc.entry
				} else {
					p.compareRef = &pc.ref
				}
				p.compareLabel = pc.label
				m.pendingCompare = nil
			}
```

`bookmarksLoadedMsg` (~line 722): same shape with `compareBookmark` and the bookmark popup.

7. i18n — one new key in all four bundles:

- `ja.toml`: `"cannot compare a commit against a file" = "コミットとファイルは比較できません"`
- `ko.toml`: `"cannot compare a commit against a file" = "커밋과 파일은 비교할 수 없습니다"`
- `zh.toml`: `"cannot compare a commit against a file" = "无法将提交与文件进行比较"`
- `ru.toml`: `"cannot compare a commit against a file" = "нельзя сравнить коммит с файлом"`

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && go test ./internal/tui/ ./internal/i18n/`
Expected: PASS (incl. the pre-existing compare-mode tests in `shelf_popup_test.go`/`bookmark_test.go`, which exercise the file flavor)

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && git add internal/tui/ internal/i18n/ && git commit -m "feat(tui): c cross-picker compare for commit entries (bookmark ↔ shelved commit)"
```

---

### Task 7: TUI — cheat sheets + headless verification

**Files:**
- Modify: `internal/tui/popup_help.go:29-88` (four changed rows + two compare-mode enter rows)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (add the new keys, REMOVE the replaced ones from all four — the orphan check fails on leftovers)
- Test: existing `internal/tui` i18n scan tests are the gate

- [ ] **Step 1: Update the help rows**

In `bookmarkSwitcherHelp` (non-compare branch), replace the `m` and `c` rows:

```go
		cheatRow("m", i18n.T("mark one, then a second bookmark to compare the two (two files, or two commit bookmarks as a whole-tree compare)")),
		cheatRow("c", i18n.T("compare the highlighted bookmark against a shelf entry (file vs file, or commit vs shelved commit)")),
```

In its compare branch, replace the `enter` row:

```go
			cheatRow("enter", i18n.T("compare the first pick against the highlighted bookmark (file vs file, or commit vs commit)")),
```

In `shelfSwitcherHelp` (non-compare branch), replace the `m` and `c` rows:

```go
		cheatRow("m", i18n.T("mark one, then a second entry to compare the two (two files, or two shelved commits as a whole-tree compare)")),
		cheatRow("c", i18n.T("compare the highlighted entry against a bookmark (file vs file, or shelved commit vs commit bookmark)")),
```

In its compare branch, replace the `enter` row:

```go
			cheatRow("enter", i18n.T("compare the first pick against the highlighted entry (file vs file, or commit vs commit)")),
```

- [ ] **Step 2: Update the bundles**

Remove these SIX now-orphaned English keys from ALL FOUR bundle files:
- `"mark one, then a second bookmark to compare the two (file bookmarks only)"`
- `"compare the highlighted bookmark against a shelf entry (file bookmarks only)"`
- `"mark one, then a second entry to compare the two (file entries only)"`
- `"compare the highlighted entry against a bookmark (file entries only)"`
- `"compare the focused file against the highlighted bookmark"`
- `"compare the focused file against the highlighted entry"`

Add the six new keys with translations:

ja.toml:
```toml
"mark one, then a second bookmark to compare the two (two files, or two commit bookmarks as a whole-tree compare)" = "1つ目をマークし、2つ目のブックマークで両者を比較（ファイル2つ、またはコミットブックマーク2つはツリー全体の比較）"
"compare the highlighted bookmark against a shelf entry (file vs file, or commit vs shelved commit)" = "選択中のブックマークをシェルフ項目と比較（ファイル対ファイル、またはコミット対シェルフのコミット）"
"mark one, then a second entry to compare the two (two files, or two shelved commits as a whole-tree compare)" = "1つ目をマークし、2つ目の項目で両者を比較（ファイル2つ、またはシェルフのコミット2つはツリー全体の比較）"
"compare the highlighted entry against a bookmark (file vs file, or shelved commit vs commit bookmark)" = "選択中の項目をブックマークと比較（ファイル対ファイル、またはシェルフのコミット対コミットブックマーク)"
"compare the first pick against the highlighted bookmark (file vs file, or commit vs commit)" = "最初に選んだものを選択中のブックマークと比較（ファイル対ファイル、またはコミット対コミット）"
"compare the first pick against the highlighted entry (file vs file, or commit vs commit)" = "最初に選んだものを選択中の項目と比較（ファイル対ファイル、またはコミット対コミット）"
```

ko.toml:
```toml
"mark one, then a second bookmark to compare the two (two files, or two commit bookmarks as a whole-tree compare)" = "하나를 표시한 뒤 두 번째 북마크에서 둘을 비교 (파일 2개, 또는 커밋 북마크 2개는 전체 트리 비교)"
"compare the highlighted bookmark against a shelf entry (file vs file, or commit vs shelved commit)" = "선택한 북마크를 선반 항목과 비교 (파일 대 파일, 또는 커밋 대 선반 커밋)"
"mark one, then a second entry to compare the two (two files, or two shelved commits as a whole-tree compare)" = "하나를 표시한 뒤 두 번째 항목에서 둘을 비교 (파일 2개, 또는 선반 커밋 2개는 전체 트리 비교)"
"compare the highlighted entry against a bookmark (file vs file, or shelved commit vs commit bookmark)" = "선택한 항목을 북마크와 비교 (파일 대 파일, 또는 선반 커밋 대 커밋 북마크)"
"compare the first pick against the highlighted bookmark (file vs file, or commit vs commit)" = "먼저 고른 것을 선택한 북마크와 비교 (파일 대 파일, 또는 커밋 대 커밋)"
"compare the first pick against the highlighted entry (file vs file, or commit vs commit)" = "먼저 고른 것을 선택한 항목과 비교 (파일 대 파일, 또는 커밋 대 커밋)"
```

zh.toml:
```toml
"mark one, then a second bookmark to compare the two (two files, or two commit bookmarks as a whole-tree compare)" = "标记一个，再在第二个书签上按此键比较两者（两个文件，或两个提交书签作整树比较）"
"compare the highlighted bookmark against a shelf entry (file vs file, or commit vs shelved commit)" = "将选中的书签与搁置条目比较（文件对文件，或提交对搁置的提交）"
"mark one, then a second entry to compare the two (two files, or two shelved commits as a whole-tree compare)" = "标记一个，再在第二个条目上按此键比较两者（两个文件，或两个搁置的提交作整树比较）"
"compare the highlighted entry against a bookmark (file vs file, or shelved commit vs commit bookmark)" = "将选中的条目与书签比较（文件对文件，或搁置的提交对提交书签）"
"compare the first pick against the highlighted bookmark (file vs file, or commit vs commit)" = "将先选的一项与选中的书签比较（文件对文件，或提交对提交）"
"compare the first pick against the highlighted entry (file vs file, or commit vs commit)" = "将先选的一项与选中的条目比较（文件对文件，或提交对提交）"
```

ru.toml:
```toml
"mark one, then a second bookmark to compare the two (two files, or two commit bookmarks as a whole-tree compare)" = "отметьте одну, затем на второй закладке — сравнить обе (два файла, или две закладки-коммита как сравнение всего дерева)"
"compare the highlighted bookmark against a shelf entry (file vs file, or commit vs shelved commit)" = "сравнить выбранную закладку с записью полки (файл с файлом, или коммит с коммитом на полке)"
"mark one, then a second entry to compare the two (two files, or two shelved commits as a whole-tree compare)" = "отметьте одну, затем на второй записи — сравнить обе (два файла, или два коммита с полки как сравнение всего дерева)"
"compare the highlighted entry against a bookmark (file vs file, or shelved commit vs commit bookmark)" = "сравнить выбранную запись с закладкой (файл с файлом, или коммит с полки с закладкой-коммитом)"
"compare the first pick against the highlighted bookmark (file vs file, or commit vs commit)" = "сравнить первый выбор с выделенной закладкой (файл с файлом, или коммит с коммитом)"
"compare the first pick against the highlighted entry (file vs file, or commit vs commit)" = "сравнить первый выбор с выделенной записью (файл с файлом, или коммит с коммитом)"
```

- [ ] **Step 3: Check the switchers' inline hint lines**

The popups render a bottom hint row of their own (separate from the `?`
sheet). Find it: `grep -n '\[m\]\|\[c\]' internal/tui/bookmark_popup.go internal/tui/shelf_popup.go`. If a hint qualifies `m`/`c` with "file … only"
wording, drop the qualifier (the keys now work for commit entries too); if
the hints are bare key lists, no change. Any changed string follows the same
bundle add/remove rules as Step 2.

- [ ] **Step 4: Run the gates**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && go test ./internal/tui/ ./internal/i18n/`
Expected: PASS (scan test = coverage + orphan + verb checks)

- [ ] **Step 5: Headless smoke test**

Run the real TUI under the tmux harness against a scratch repo (see the `driving-tui-headless` project skill; use `./tui-capture.sh`) to verify: `G` → `m` on a shelved commit → `m` on a second one opens the compare files view titled `<short-sha> ↔ <short-sha>` (or `shelf #… (frozen)` when applicable). Capture at least one screen showing the compare view.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && git add internal/tui/ internal/i18n/ && git commit -m "feat(tui): switcher cheat sheets cover commit-entry compares"
```

---

### Task 8: Docs, agent skill, full gates

**Files:**
- Modify: `CHANGELOG.md` (new entry at top)
- Modify: `README.md` (CLI `gg compare` section + switcher keys, wherever those surfaces are documented)
- Modify: `CLAUDE.md` (package-map rows: `model` — EndpointShelf; `domain` — ResolveCommitEntryEndpoint/shelf-aware CompareFiles/ComparePatch; `cli` — gg compare entry specs; `shelf`/`bookmark` — commit entries comparable)
- Modify: `internal/agentskill/using-gg.md` (document `gg compare bookmark:<id>`/`shelf:<id>` + `--patch`) and bump `agentskill.Version` (find it: `grep -rn "Version" internal/agentskill/*.go`)

- [ ] **Step 1: Update the docs**

CHANGELOG entry (match the file's existing entry style):

```markdown
## compare two commit entries (bookmarks / shelved commits) — 2026-07-31

The g/G switchers compare two commit entries as a whole-tree compare: `m`
mark-two within a picker, `c` across pickers (commit bookmark ↔ shelved
commit). Live sha-vs-sha while both commits exist; a gc'd shelved commit
falls back to its frozen tar (labelled "frozen" in the compare title; the
cross fallback scopes to the frozen side's members). `gg compare` gains
`bookmark:<id>` / `shelf:<id>` specs and `--patch`; a frozen fallback is
noted on stderr.
```

README and CLAUDE.md: fold the same content into the existing sections/rows (keep each row's style; CLAUDE.md package-map rows are inline prose).

using-gg.md: add to the compare/diff section:

```markdown
- `gg compare [--patch] <spec> [<spec>]` — specs may also be `bookmark:<id>`
  or `shelf:<id>` (a stored commit entry; find ids via `gg bookmark list` /
  `gg shelf list`). While the entry's commit exists this is a live tree
  compare; a gc'd shelved commit falls back to its frozen snapshot (noted on
  stderr, scoped to the files that commit changed). `--patch` prints unified
  diffs instead of the file list.
```

Bump `agentskill.Version` by one.

- [ ] **Step 2: Run the staged test script**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && ./test.sh`
Expected: vet+gofmt, unit, e2e all green.

- [ ] **Step 3: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && git add CHANGELOG.md README.md CLAUDE.md internal/agentskill/ && git commit -m "docs: compare-commit-entries feature docs + using-gg bump"
```

- [ ] **Step 4: Race gate (before any merge)**

Start detached (quiet machine; see the race-gate memory):

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/compare-commit-entries && nohup ./test.sh race > /tmp/claude-1000/-mnt-t-others-gigagit/08b57168-dc0a-4d3c-8c17-41cb8ade5fdf/scratchpad/race-compare-entries.log 2>&1 &
```

Poll the log until it finishes; report the result. Do NOT merge — the human decides the merge (ask first).

- [ ] **Step 5: Report**

Summarize: what shipped per task, test/gate status, and that `feat/compare-commit-entries` is ready for the user's merge decision. After the user merges: run `gg init --update` to refresh installed agent-skill copies.
