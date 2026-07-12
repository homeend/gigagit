# Cherry-pick a bookmarked / shelved commit — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One key — `a` — in the `g` (bookmarks) and `G` (shelf) switchers that applies the highlighted commit entry onto the current branch: a true `git cherry-pick` while the commit exists, a `git am` replay of a patch snapshot (newly stored at shelve time) after a gc, and a clear notice otherwise.

**Architecture:** Three layers, bottom-up. (1) `internal/shelf`: a commit entry gains an optional second content-addressed blob — the commit's `format-patch` mailbox. (2) `internal/domain`: `ShelfAddCommit` captures that patch best-effort via the existing `CommitPatch`; two new service methods (`CommitLookup`, `ShelfPatchFile`) give the TUI a gated existence probe and a temp-file materialization. (3) `internal/tui`: a new `pick_commit.go` owns the gen-guarded async probe, the three lanes' confirm modals, dispatch to the existing `engine.CherryPick` / `engine.ApplyPatch` ops, and temp-file cleanup; both switcher popups wire the `a` key into it. No engine, git-verb, or CLI changes.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver Model), real-git tests in `t.TempDir()`, TDD.

**Spec:** `docs/superpowers/specs/2026-07-12-cherry-pick-shelf-bookmark-design.md`

## Global Constraints

- **Work ONLY in the feature worktree:** `/mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark` (branch `feat/cherry-pick-shelf-bookmark`). Never touch the shared checkout at `/mnt/t/others/gigagit`. All `Write`/`Edit` calls must use the worktree's absolute paths; every shell command must `cd` into the worktree first. Verify with `git branch --show-current` → `feat/cherry-pick-shelf-bookmark` before the first change.
- `internal/tui` and `internal/cli` never import `internal/git` or `internal/shelf` in **non-test** files (archtest-guarded; `_test.go` files are exempt — `go list .Imports` skips them).
- TDD: write the failing test first, watch it fail, then implement. Frequent commits (one per task, message style `feat(scope): …` / `test(scope): …`).
- Every commit message ends with the two trailer lines:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9`
- Patch snapshots are **best-effort**: a merge commit, an oversized patch (> `shelf.MaxCommitArchiveBytes`), or a `format-patch` failure must never fail the shelve.
- The user-visible strings in this plan (modal prompts, notices, hint labels, cheat rows) are exact copy — do not reword.

---

### Task 1: Shelf store — patch blob support

**Files:**
- Modify: `internal/model/model.go` (ShelfEntry struct, ~line 290)
- Modify: `internal/shelf/store.go` (Store interface, ErrNoPatch)
- Modify: `internal/shelf/file_store.go` (PutCommit, GetPatch, Remove)
- Modify: `internal/domain/shelf.go:76` (mechanical call-site update — pass `nil` patch for now)
- Test: `internal/shelf/file_store_test.go`

**Interfaces:**
- Consumes: existing `writeBlob`, `blobPath`, `putEntry`, `fsRoot` (test helper), `model.FileAddress`.
- Produces (later tasks rely on these exact names):
  - `model.ShelfEntry.PatchSHA string` / `model.ShelfEntry.PatchSize int64` (zero values = no patch)
  - `shelf.ErrNoPatch` (sentinel error)
  - `Store.PutCommit(bucket string, addr model.FileAddress, tar, patch []byte, label string) (model.ShelfEntry, error)`
  - `Store.GetPatch(entryID string) ([]byte, error)` — `ErrNotFound` unknown entry, `ErrNoPatch` entry without patch

- [ ] **Step 1: Write the failing tests**

Append to `internal/shelf/file_store_test.go` (it already imports `os`, `strings`, `testing`, `model`; add `"errors"` to its imports if missing):

```go
func TestPutCommitStoresPatchBlob(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	addr := model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6", Path: ""}
	patch := []byte("From a1b2c3d4e5f6 Mon Sep 17 00:00:00 2001\nSubject: x\n")
	e, err := fs.PutCommit("", addr, []byte("tarbytes"), patch, "fix")
	if err != nil {
		t.Fatalf("PutCommit: %v", err)
	}
	if e.PatchSHA == "" || e.PatchSize != int64(len(patch)) {
		t.Fatalf("patch fields = %q / %d, want set / %d", e.PatchSHA, e.PatchSize, len(patch))
	}
	got, err := fs.GetPatch(e.ID)
	if err != nil || string(got) != string(patch) {
		t.Fatalf("GetPatch = %q, %v", got, err)
	}
	// The tar payload is untouched by the patch blob.
	tar, err := fs.Get(e.ID)
	if err != nil || string(tar) != "tarbytes" {
		t.Fatalf("Get = %q, %v", tar, err)
	}
	// Patch fields survive the TOML index round-trip.
	fs2 := NewFileStore(fsRoot(fs))
	e2, err := fs2.Find(e.ID)
	if err != nil || e2.PatchSHA != e.PatchSHA || e2.PatchSize != e.PatchSize {
		t.Fatalf("reloaded patch fields = %q / %d, %v", e2.PatchSHA, e2.PatchSize, err)
	}
}

func TestPutCommitWithoutPatch(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	addr := model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6", Path: ""}
	e, err := fs.PutCommit("", addr, []byte("tarbytes"), nil, "")
	if err != nil {
		t.Fatalf("PutCommit: %v", err)
	}
	if e.PatchSHA != "" || e.PatchSize != 0 {
		t.Fatalf("no-patch entry carries patch fields: %q / %d", e.PatchSHA, e.PatchSize)
	}
	if _, err := fs.GetPatch(e.ID); !errors.Is(err, ErrNoPatch) {
		t.Fatalf("GetPatch = %v, want ErrNoPatch", err)
	}
	if _, err := fs.GetPatch("no-such-entry"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPatch(unknown) = %v, want ErrNotFound", err)
	}
}

func TestRemoveReclaimsPatchBlob(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	addr := model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6", Path: ""}
	e, err := fs.PutCommit("", addr, []byte("tarbytes"), []byte("patchbytes"), "")
	if err != nil {
		t.Fatalf("PutCommit: %v", err)
	}
	tarBlob, patchBlob := fs.blobPath(e.SHA), fs.blobPath(e.PatchSHA)
	if _, err := os.Stat(patchBlob); err != nil {
		t.Fatalf("patch blob missing after PutCommit: %v", err)
	}
	if err := fs.Remove(e.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(tarBlob); !os.IsNotExist(err) {
		t.Fatalf("tar blob not reclaimed: %v", err)
	}
	if _, err := os.Stat(patchBlob); !os.IsNotExist(err) {
		t.Fatalf("patch blob not reclaimed: %v", err)
	}
}

func TestRemoveKeepsSharedPatchBlob(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	a := model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6"}
	b := model.FileAddress{State: model.StateCommitted, Commit: "f6e5d4c3b2a1"}
	e1, err := fs.PutCommit("", a, []byte("tar-one"), []byte("same-patch"), "")
	if err != nil {
		t.Fatalf("PutCommit e1: %v", err)
	}
	e2, err := fs.PutCommit("", b, []byte("tar-two"), []byte("same-patch"), "")
	if err != nil {
		t.Fatalf("PutCommit e2: %v", err)
	}
	if e1.PatchSHA != e2.PatchSHA {
		t.Fatal("content-addressing must dedup identical patch blobs")
	}
	if err := fs.Remove(e1.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := fs.GetPatch(e2.ID); err != nil {
		t.Fatalf("survivor's patch must remain readable: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark && go test ./internal/shelf/ -run 'TestPutCommit|TestRemove' 2>&1 | head -20`
Expected: compile FAILURE — `too many arguments in call to fs.PutCommit`, `fs.GetPatch undefined`, `undefined: ErrNoPatch`, `e.PatchSHA undefined`.

- [ ] **Step 3: Implement**

**3a.** `internal/model/model.go` — in the `ShelfEntry` struct, after the `Size int64` field, add:

```go
	// PatchSHA/PatchSize describe an optional second blob for a commit entry:
	// the commit's format-patch mailbox, snapshotted at shelve time so the
	// entry can be re-applied as a commit (git am) even after the commit
	// object is gc'd. "" = none (a file entry, an old entry, a merge commit,
	// or an oversized/failed patch).
	PatchSHA  string
	PatchSize int64
```

**3b.** `internal/shelf/store.go` — add the sentinel next to `ErrNotFound`, and update the interface:

```go
// ErrNoPatch is returned by GetPatch for an entry that exists but has no
// stored patch snapshot (a file entry, a pre-patch-support commit entry, or
// a merge commit).
var ErrNoPatch = errors.New("shelf: entry has no stored patch")
```

In the `Store` interface, replace the `PutCommit` line and add `GetPatch` after `Get`:

```go
	PutCommit(bucket string, addr model.FileAddress, tar, patch []byte, label string) (model.ShelfEntry, error)
	Get(entryID string) ([]byte, error)
	GetPatch(entryID string) ([]byte, error)
```

**3c.** `internal/shelf/file_store.go` — replace `PutCommit` with (only the signature, the size guard, the patch blob write, and the two new struct fields change; keep the existing doc comment and extend it):

```go
// PutCommit stores a commit's changed-files tar as a durable ShelfKindCommit
// entry (id: commit-<shortsha>-<blobsha8>). A non-empty patch (the commit's
// format-patch mailbox) is stored as a second content-addressed blob so the
// entry can be re-applied as a commit after the original is gc'd; nil = no
// snapshot (merge commit, oversized, or shelved before patch support).
func (fs *FileStore) PutCommit(bucket string, addr model.FileAddress, tar, patch []byte, label string) (model.ShelfEntry, error) {
	if len(tar) > MaxCommitArchiveBytes || len(patch) > MaxCommitArchiveBytes {
		return model.ShelfEntry{}, ErrTooLarge
	}
	bucket = normalizeBucket(bucket)
	sha, err := fs.writeBlob(tar)
	if err != nil {
		return model.ShelfEntry{}, err
	}
	var patchSHA string
	var patchSize int64
	if len(patch) > 0 {
		if patchSHA, err = fs.writeBlob(patch); err != nil {
			return model.ShelfEntry{}, err
		}
		patchSize = int64(len(patch))
	}
	short := addr.Commit
	if len(short) > 7 {
		short = short[:7]
	}
	return fs.putEntry(model.ShelfEntry{
		ID:        fmt.Sprintf("commit-%s-%s", short, sha[:8]),
		Bucket:    bucket,
		Kind:      model.ShelfKindCommit,
		Origin:    addr,
		Label:     label,
		SHA:       sha,
		Size:      int64(len(tar)),
		PatchSHA:  patchSHA,
		PatchSize: patchSize,
		Created:   time.Now(),
	})
}
```

Add `GetPatch` directly after `Get`:

```go
// GetPatch returns an entry's stored format-patch mailbox — Get's sibling for
// the second (patch) blob. ErrNoPatch when the entry has no snapshot.
func (fs *FileStore) GetPatch(entryID string) ([]byte, error) {
	idx := fs.read()
	for _, e := range idx.Entries {
		if e.ID == entryID {
			if e.PatchSHA == "" {
				return nil, ErrNoPatch
			}
			return os.ReadFile(fs.blobPath(e.PatchSHA))
		}
	}
	return nil, ErrNotFound
}
```

Replace `Remove` (the reclaim now covers both of the removed entry's blobs, each checked against both fields of every survivor):

```go
func (fs *FileStore) Remove(entryID string) error {
	idx := fs.read()
	var removed []string
	kept := idx.Entries[:0]
	for _, e := range idx.Entries {
		if e.ID == entryID {
			removed = append(removed, e.SHA)
			if e.PatchSHA != "" {
				removed = append(removed, e.PatchSHA)
			}
			continue
		}
		kept = append(kept, e)
	}
	idx.Entries = kept
	if len(removed) == 0 {
		return ErrNotFound
	}
	// Reclaim each blob only when no surviving entry references it (as either
	// its tar/file blob or its patch blob — content-addressing may share).
	for _, sha := range removed {
		stillUsed := false
		for _, e := range idx.Entries {
			if e.SHA == sha || e.PatchSHA == sha {
				stillUsed = true
				break
			}
		}
		if !stillUsed {
			os.Remove(fs.blobPath(sha))
		}
	}
	return fs.write(idx)
}
```

**3d.** Mechanical call-site updates so the build stays green:
- `internal/domain/shelf.go:76`: `return st.PutCommit("", addr, tar, nil, label)` (real patch capture is Task 2).
- `internal/shelf/file_store_test.go`: the two pre-existing `PutCommit(` calls (in `TestPutCommitStoresArchiveKind` and `TestPutCommitPersistsLabel`) gain a `nil` patch argument: `fs.PutCommit("", addr, tar, nil, "")` / `fs.PutCommit("", addr, []byte("tarbytes"), nil, "my fix")`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark && go build ./... && go test ./internal/shelf/ ./internal/domain/ ./internal/model/`
Expected: PASS (all packages).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark
git add internal/model/model.go internal/shelf/ internal/domain/shelf.go
git commit -m "feat(shelf): optional patch-snapshot blob on commit entries

PutCommit gains a patch []byte (format-patch mailbox, second
content-addressed blob); GetPatch reads it back (ErrNoPatch sentinel);
Remove reclaims both blobs reference-counted across both fields.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9"
```

---

### Task 2: Domain — patch capture, CommitLookup, ShelfPatchFile

**Files:**
- Modify: `internal/domain/shelf.go` (ShelfAddCommit; new ShelfPatchFile; add imports `os`, `internal/shelf`)
- Modify: `internal/domain/query.go` (new CommitLookup, place after `CommitTimes` ~line 510)
- Test: `internal/domain/export_test.go`

**Interfaces:**
- Consumes (from Task 1): `Store.PutCommit(bucket, addr, tar, patch, label)`, `Store.GetPatch(entryID)`, `shelf.ErrNoPatch`, `shelf.MaxCommitArchiveBytes`, `ShelfEntry.PatchSHA`.
- Consumes (existing): `s.CommitPatch(ctx, sha) ([]byte, string, error)` (refuses merges via `refuseMerge`, gated `FormatPatch`), `queryQuiet`, `s.repo.CommitLine(ctx, rev) (model.LogLine, error)`, test helpers `newRealRepo`, `writeCommit`, `gitRun`, `headHash`, `svc.SetShelfStore`.
- Produces (Task 3 relies on these exact signatures):
  - `func (s *Service) CommitLookup(ctx context.Context, rev string) (model.LogLine, bool, error)` — `(line, true, nil)` found; `(zero, false, nil)` missing; err only for context cancellation.
  - `func (s *Service) ShelfPatchFile(ctx context.Context, entryID string) (string, error)` — temp-file path; caller owns deletion; `shelf.ErrNoPatch` passthrough.

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/export_test.go` (imports there already include `os`, `context`, `testing`, `shelf`; add `"bytes"` and `"errors"` if missing):

```go
func TestShelfAddCommitStoresPatch(t *testing.T) {
	repoDir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()

	writeCommit(t, repoDir, "src/a.txt", "alpha\n", "add a")
	sha := headHash(t, repoDir)

	e, err := svc.ShelfAddCommit(ctx, sha, "")
	if err != nil {
		t.Fatalf("ShelfAddCommit: %v", err)
	}
	if e.PatchSHA == "" {
		t.Fatal("a non-merge commit entry must carry a patch snapshot")
	}
	path, err := svc.ShelfPatchFile(ctx, e.ID)
	if err != nil {
		t.Fatalf("ShelfPatchFile: %v", err)
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("From ")) {
		t.Fatalf("patch must be a format-patch mailbox, got head %q", data[:min(20, len(data))])
	}
	if !bytes.Contains(data, []byte("add a")) {
		t.Fatal("patch must carry the commit subject")
	}
}

func TestShelfAddCommitMergeSkipsPatch(t *testing.T) {
	repoDir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()

	writeCommit(t, repoDir, "base.txt", "base\n", "base")
	gitRun(t, repoDir, "checkout", "-b", "side")
	writeCommit(t, repoDir, "side.txt", "side\n", "side change")
	gitRun(t, repoDir, "checkout", "-")
	writeCommit(t, repoDir, "main.txt", "main\n", "main change")
	gitRun(t, repoDir, "merge", "--no-ff", "-m", "merge side", "side")
	sha := headHash(t, repoDir)

	e, err := svc.ShelfAddCommit(ctx, sha, "")
	if err != nil {
		t.Fatalf("shelving a merge commit must still succeed: %v", err)
	}
	if e.PatchSHA != "" {
		t.Fatal("a merge commit must not store a patch snapshot")
	}
	if _, err := svc.ShelfPatchFile(ctx, e.ID); !errors.Is(err, shelf.ErrNoPatch) {
		t.Fatalf("ShelfPatchFile = %v, want shelf.ErrNoPatch", err)
	}
}

func TestCommitLookup(t *testing.T) {
	repoDir, svc := newRealRepo(t)
	ctx := context.Background()

	writeCommit(t, repoDir, "a.txt", "a\n", "subject here")
	sha := headHash(t, repoDir)

	line, found, err := svc.CommitLookup(ctx, sha)
	if err != nil || !found {
		t.Fatalf("CommitLookup(%s): found=%v err=%v", sha, found, err)
	}
	if line.Subject != "subject here" || line.Hash == "" {
		t.Fatalf("line = %+v", line)
	}

	_, found, err = svc.CommitLookup(ctx, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if err != nil || found {
		t.Fatalf("missing commit: found=%v err=%v, want false, nil", found, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark && go test ./internal/domain/ -run 'TestShelfAddCommitStoresPatch|TestShelfAddCommitMergeSkipsPatch|TestCommitLookup' 2>&1 | head -15`
Expected: compile FAILURE — `svc.ShelfPatchFile undefined`, `svc.CommitLookup undefined` (and `TestShelfAddCommitStoresPatch` would fail on empty `PatchSHA` once they exist).

- [ ] **Step 3: Implement**

**3a.** `internal/domain/shelf.go` — in `ShelfAddCommit`, between the `archiveFiles` call and the `addr :=` line, insert the patch capture, and change the final call:

```go
	// Best-effort patch snapshot: lets the entry be re-applied as a commit
	// (git am) even after the commit object is gc'd. A merge commit (refused
	// by CommitPatch), an oversized patch, or a format-patch failure just
	// skips the snapshot — shelving must never fail over it.
	patch, _, perr := s.CommitPatch(ctx, sha)
	if perr != nil || len(patch) > shelf.MaxCommitArchiveBytes {
		patch = nil
	}
	addr := model.FileAddress{State: model.StateCommitted, Commit: sha, Path: ""}
	return st.PutCommit("", addr, tar, patch, label)
```

Add at the end of the file:

```go
// ShelfPatchFile materializes entryID's stored format-patch mailbox to a temp
// file (engine.ApplyPatch takes a disk path) and returns that path. The caller
// owns deletion once the op that consumed it finishes. shelf.ErrNoPatch when
// the entry has no snapshot.
func (s *Service) ShelfPatchFile(ctx context.Context, entryID string) (string, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return "", ErrShelfDisabled
	}
	data, err := st.GetPatch(entryID)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "gg-shelf-*.patch")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
```

Add `"os"` and `"github.com/homeend/gigagit/internal/shelf"` to `internal/domain/shelf.go`'s imports.

**3b.** `internal/domain/query.go` — after `CommitTimes`:

```go
// CommitLookup resolves rev to its short-sha + subject, reporting found=false
// when no such commit exists. Missing is an EXPECTED state here (a bookmarked
// or shelved commit may have been gc'd), so it is not an error and is never
// recorded to the failure log (queryQuiet); only a context cancellation
// propagates as err. Backs the TUI's cherry-pick lane probe.
func (s *Service) CommitLookup(ctx context.Context, rev string) (model.LogLine, bool, error) {
	line, err := queryQuiet(ctx, s, "commitLookup:"+rev, func(ctx context.Context) (model.LogLine, error) {
		return s.repo.CommitLine(ctx, rev)
	})
	if err != nil {
		if ctx.Err() != nil {
			return model.LogLine{}, false, ctx.Err()
		}
		return model.LogLine{}, false, nil
	}
	return line, true, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark && go test ./internal/domain/ -run 'TestShelfAddCommit|TestCommitLookup' -v 2>&1 | tail -15`
Expected: PASS for `TestShelfAddCommitStoresPatch`, `TestShelfAddCommitMergeSkipsPatch`, `TestCommitLookup`, `TestShelfAddCommitAndExportRoundTrip`.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark
git add internal/domain/shelf.go internal/domain/query.go internal/domain/export_test.go
git commit -m "feat(domain): shelve-time patch capture + CommitLookup/ShelfPatchFile

ShelfAddCommit snapshots the commit's format-patch mailbox best-effort
(merge/oversized/failed -> tar-only). CommitLookup is the quiet gated
existence probe; ShelfPatchFile materializes the snapshot for git am.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9"
```

---

### Task 3: TUI — pick machinery (probe, lanes, modals, cleanup)

**Files:**
- Create: `internal/tui/pick_commit.go`
- Modify: `internal/tui/model.go` (2 struct fields near `pushCheckGen` ~line 55; `case pickProbeMsg` in Update's msg switch near the `pushTagCheckMsg` case ~line 1867; cleanup call in the `opFinishedMsg` case right after `m.opMsgs = nil` ~line 1910; gen bump + cleanup in `reRoot` next to `m.pushCheckGen++` ~line 2918)
- Test: `internal/tui/pick_commit_test.go`

**Interfaces:**
- Consumes (Task 2): `svc.CommitLookup(ctx, rev) (model.LogLine, bool, error)`, `svc.ShelfPatchFile(ctx, entryID) (string, error)`.
- Consumes (existing): `decisionState{req engine.DecisionRequest, onResolve func(Model, string) (tea.Model, tea.Cmd)}`, `m.startOp(engine.Operation)`, `m.clearLayers()`, `engine.CherryPick{Commit}`, `engine.ApplyPatch{Path, Mode}` with `engine.ApplyModeCommits`, `m.status.Branch`, test helpers `footerModel()`, `keyMsg(string)`, `shelfPopModel(...)`.
- Produces (Task 4 relies on): `pickTarget{sha, label, shelfID string, hasPatch bool}`, `func (m Model) startPickCommit(t pickTarget) (Model, tea.Cmd)`, Model fields `pickGen int` / `pickPatchTemp string`, `func (m Model) cleanupPickPatchTemp() Model`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/pick_commit_test.go`:

```go
package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/shelf" // test-only import; archtest checks non-test imports
)

// pickModel is a bare model (no popup) for probe-result handling tests.
func pickModel() Model {
	return footerModel()
}

func TestPickProbeStaleGenDropped(t *testing.T) {
	m := pickModel()
	m.pickGen = 5
	mm, cmd := m.Update(pickProbeMsg{gen: 4, target: pickTarget{sha: "abc"}, found: true})
	m = mm.(Model)
	if cmd != nil || m.modal != nil {
		t.Fatalf("stale probe must be dropped (cmd=%v modal=%v)", cmd, m.modal)
	}
}

func TestPickProbeFoundOpensCherryPickModal(t *testing.T) {
	m := pickModel()
	msg := pickProbeMsg{
		gen:    m.pickGen,
		target: pickTarget{sha: "a1b2c3d4e5f6a7b8"},
		line:   model.LogLine{Hash: "a1b2c3d", Subject: "fix thing"},
		found:  true,
	}
	mm, _ := m.Update(msg)
	m = mm.(Model)
	if m.modal == nil {
		t.Fatal("found probe must open the confirm modal")
	}
	want := "Cherry-pick a1b2c3d fix thing onto main?"
	if m.modal.req.Prompt != want {
		t.Fatalf("prompt = %q, want %q", m.modal.req.Prompt, want)
	}
	res, cmd := m.modal.onResolve(m, "Cherry-pick")
	m = res.(Model)
	if !m.running || cmd == nil {
		t.Fatalf("confirming must dispatch the cherry-pick op (running=%v cmd=%v)", m.running, cmd)
	}
	if m.topLayer() != nil {
		t.Fatal("layers must be cleared before the op so conflicts land in the main view")
	}
}

func TestPickProbeCancelDoesNothing(t *testing.T) {
	m := pickModel()
	mm, _ := m.Update(pickProbeMsg{gen: m.pickGen, target: pickTarget{sha: "abc"},
		line: model.LogLine{Hash: "abc1234", Subject: "s"}, found: true})
	m = mm.(Model)
	res, cmd := m.modal.onResolve(m, "Cancel")
	m = res.(Model)
	if m.running || cmd != nil {
		t.Fatalf("Cancel must not dispatch (running=%v cmd=%v)", m.running, cmd)
	}
}

func TestPickProbeMissingWithPatchAppliesPatch(t *testing.T) {
	m := pickModel()
	st := shelf.NewFileStore(t.TempDir())
	e, err := st.PutCommit("",
		model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6a7b8"},
		[]byte("tarbytes"), []byte("From a1b2 Mon Sep 17 00:00:00 2001\nSubject: x\n"), "fix")
	if err != nil {
		t.Fatal(err)
	}
	m.svc.SetShelfStore(st)

	msg := pickProbeMsg{
		gen:    m.pickGen,
		target: pickTarget{sha: "a1b2c3d4e5f6a7b8", label: "fix", shelfID: e.ID, hasPatch: true},
		found:  false,
	}
	mm, _ := m.Update(msg)
	m = mm.(Model)
	if m.modal == nil || !strings.Contains(m.modal.req.Prompt, "no longer in the repo") {
		t.Fatalf("missing+patch must open the patch modal, got %+v", m.modal)
	}
	res, cmd := m.modal.onResolve(m, "Apply patch")
	m = res.(Model)
	if !m.running || cmd == nil {
		t.Fatalf("confirming must dispatch the apply op (running=%v cmd=%v)", m.running, cmd)
	}
	if m.pickPatchTemp == "" {
		t.Fatal("the temp patch path must be remembered for cleanup")
	}
	data, err := os.ReadFile(m.pickPatchTemp)
	if err != nil || !strings.HasPrefix(string(data), "From ") {
		t.Fatalf("temp patch file bad: %q, %v", data, err)
	}
	// The opFinishedMsg cleanup removes the temp file.
	tmp := m.pickPatchTemp
	mm, _ = m.Update(opFinishedMsg{})
	m = mm.(Model)
	if m.pickPatchTemp != "" {
		t.Fatal("pickPatchTemp must clear when the op finishes")
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("temp file must be removed, stat err=%v", err)
	}
}

func TestPickProbeMissingNoPatchNotices(t *testing.T) {
	m := pickModel()
	// Shelf entry without a patch.
	mm, _ := m.Update(pickProbeMsg{gen: m.pickGen,
		target: pickTarget{sha: "abc", shelfID: "commit-abc-11112222", hasPatch: false}, found: false})
	m = mm.(Model)
	if m.modal != nil || !strings.Contains(m.statusMsg, "no stored patch") {
		t.Fatalf("shelf no-patch notice missing: modal=%v msg=%q", m.modal, m.statusMsg)
	}
	// Bookmark (no shelfID).
	m = pickModel()
	mm, _ = m.Update(pickProbeMsg{gen: m.pickGen, target: pickTarget{sha: "abc"}, found: false})
	m = mm.(Model)
	if m.modal != nil || !strings.Contains(m.statusMsg, "a bookmark stores no snapshot") {
		t.Fatalf("bookmark notice missing: modal=%v msg=%q", m.modal, m.statusMsg)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark && go test ./internal/tui/ -run TestPickProbe 2>&1 | head -10`
Expected: compile FAILURE — `undefined: pickProbeMsg`, `undefined: pickTarget`, `m.pickGen undefined`, `m.pickPatchTemp undefined`.

- [ ] **Step 3: Implement**

**3a.** Create `internal/tui/pick_commit.go`:

```go
package tui

import (
	"context"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// pickTarget identifies the commit entry `a` was pressed on in a switcher and
// carries everything the lanes need to dispatch without re-reading the popup
// (which may already be closed when the probe returns).
type pickTarget struct {
	sha      string // full commit sha (Bookmark.Commit / ShelfEntry.Origin.Commit)
	label    string // display fallback when the commit is gone (entry label/subject)
	shelfID  string // non-empty = shelf entry (the patch lane is possible)
	hasPatch bool   // the shelf entry carries a stored patch blob
}

// pickProbeMsg is the async result of the commit-existence probe.
type pickProbeMsg struct {
	gen    int
	target pickTarget
	line   model.LogLine // short sha + subject when found
	found  bool
	err    error
}

// startPickCommit dispatches the gen-guarded existence probe for t. The lanes
// resolve on the probe's return (handlePickProbe); pickGen drops a result that
// arrives after the switcher was closed or the repo was switched.
func (m Model) startPickCommit(t pickTarget) (Model, tea.Cmd) {
	m.pickGen++
	gen := m.pickGen
	svc := m.svc
	return m, func() tea.Msg {
		line, found, err := svc.CommitLookup(context.Background(), t.sha)
		return pickProbeMsg{gen: gen, target: t, line: line, found: found, err: err}
	}
}

// handlePickProbe forks the three lanes: a live cherry-pick (commit exists), a
// stored-patch replay (shelf entry, commit gone), or a notice (nothing to do).
func (m Model) handlePickProbe(msg pickProbeMsg) (Model, tea.Cmd) {
	if msg.gen != m.pickGen || m.running {
		return m, nil // stale (switcher closed / repo switched) or an op raced in
	}
	if msg.err != nil {
		m.statusMsg = "cherry-pick: " + msg.err.Error()
		return m, nil
	}
	t := msg.target
	branch := m.status.Branch
	if branch == "" {
		branch = "the current branch"
	}
	if msg.found {
		sha := t.sha
		m.modal = &decisionState{
			req: engine.DecisionRequest{
				ID:      "pick-commit",
				Prompt:  "Cherry-pick " + msg.line.Hash + " " + msg.line.Subject + " onto " + branch + "?",
				Options: []string{"Cherry-pick", "Cancel"},
			},
			onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
				if opt != "Cherry-pick" {
					return m, nil
				}
				// Close the switcher: a conflicted pick must land in the main
				// view, where the status refresh feeds the conflict process.
				m = m.clearLayers()
				return m.startOp(engine.CherryPick{Commit: sha})
			},
		}
		return m, nil
	}
	if t.shelfID != "" && t.hasPatch {
		short := t.sha
		if len(short) > 7 {
			short = short[:7]
		}
		id := t.shelfID
		m.modal = &decisionState{
			req: engine.DecisionRequest{
				ID:      "pick-commit-patch",
				Prompt:  "Commit " + short + " is no longer in the repo. Re-apply the shelved patch as a new commit?",
				Options: []string{"Apply patch", "Cancel"},
			},
			onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
				if opt != "Apply patch" {
					return m, nil
				}
				// Local blob read + temp write — fast, no git; the
				// bookmarkPastePrompt precedent for sync resolution in update.
				path, err := m.svc.ShelfPatchFile(context.Background(), id)
				if err != nil {
					m.statusMsg = "apply patch: " + err.Error()
					return m, nil
				}
				m.pickPatchTemp = path
				m = m.clearLayers()
				return m.startOp(engine.ApplyPatch{Path: path, Mode: engine.ApplyModeCommits})
			},
		}
		return m, nil
	}
	if t.shelfID != "" {
		m.statusMsg = "commit no longer exists and this entry has no stored patch (shelved before patch support, or a merge commit)"
	} else {
		m.statusMsg = "commit no longer exists — a bookmark stores no snapshot (shelve commits to keep them applyable)"
	}
	return m, nil
}

// cleanupPickPatchTemp removes the patch lane's temp file, if one is pending.
// Called when the op that consumed it finishes, and on reRoot.
func (m Model) cleanupPickPatchTemp() Model {
	if m.pickPatchTemp != "" {
		_ = os.Remove(m.pickPatchTemp)
		m.pickPatchTemp = ""
	}
	return m
}
```

**3b.** `internal/tui/model.go` — four small insertions:

1. Struct fields, next to `pushCheckGen` (~line 55):

```go
	pickGen       int    // generation guard for the async cherry-pick commit probe
	pickPatchTemp string // patch lane's temp file; removed when its op finishes
```

2. In `Update`'s message switch, near the `pushTagCheckMsg` case:

```go
	case pickProbeMsg:
		return m.handlePickProbe(msg)
```

3. In the `opFinishedMsg` case, immediately after `m.opMsgs = nil`:

```go
		m = m.cleanupPickPatchTemp()
```

4. In `reRoot`, next to `m.pushCheckGen++`:

```go
	m.pickGen++ // drop any in-flight cherry-pick probe from the old repo
	m = m.cleanupPickPatchTemp()
```

(If `reRoot` mutates `m` through a different pattern there, follow the surrounding code — the two effects that matter are the gen bump and the temp cleanup.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark && go test ./internal/tui/ -run TestPickProbe -v 2>&1 | tail -12`
Expected: PASS ×5.

Also run: `go test ./internal/tui/ 2>&1 | tail -3` — full package must stay green.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark
git add internal/tui/pick_commit.go internal/tui/pick_commit_test.go internal/tui/model.go
git commit -m "feat(tui): cherry-pick lanes for commit entries (probe + modals)

Gen-guarded CommitLookup probe forks three lanes: live CherryPick,
git-am replay of the shelf's stored patch, or a notice. Temp patch
file cleaned on opFinishedMsg and reRoot.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9"
```

---

### Task 4: TUI — wire `a` into both switchers + hints + cheat sheets

**Files:**
- Modify: `internal/tui/bookmark_popup.go` (KeyRunes switch in `update`, ~line 267; hint slice in `renderBookmarkPopupBox` ~line 149; `pickGen` bump in the nav-mode `KeyEsc` branch ~line 241)
- Modify: `internal/tui/shelf_popup.go` (same three spots: KeyRunes switch, hint slice ~line 130, nav-mode `KeyEsc`)
- Modify: `internal/tui/popup_help.go` (both non-compare cheat sheets)
- Test: `internal/tui/pick_commit_test.go` (append key-wiring tests)

**Interfaces:**
- Consumes (Task 3): `m.startPickCommit(pickTarget{...})`, Model field `m.pickGen`.
- Consumes (existing): `p.selected()`, `p.compareRef`, `model.Bookmark.IsCommit()`, `model.ShelfEntry.IsCommit()`, `ShelfEntry.PatchSHA`, test helpers `shelfPopModel`, `shEntry`, `keyMsg`, `newBookmarkPopup`, `footerModel`.
- Produces: the user-visible `a` key. No new exported names.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/pick_commit_test.go`:

```go
func commitShelfEntry(id, sha, label string) model.ShelfEntry {
	return model.ShelfEntry{
		ID: id, Kind: model.ShelfKindCommit, Label: label,
		Origin: model.FileAddress{State: model.StateCommitted, Commit: sha},
		SHA:    id + "0000", PatchSHA: "p" + id,
	}
}

func bookmarkPopModel(items ...model.Bookmark) Model {
	m := footerModel()
	m.width, m.height = 100, 30
	m = m.pushLayer(newBookmarkPopup(items))
	return m
}

func commitBookmark(id, sha, label string) model.Bookmark {
	return model.Bookmark{ID: id, State: model.StateCommitted, Commit: sha, Label: label}
}

func TestShelfPopupAOnCommitEntryProbes(t *testing.T) {
	m := shelfPopModel(commitShelfEntry("e1", "a1b2c3d4e5f6a7b8", "fix"))
	gen0 := m.pickGen
	mm, cmd := m.Update(keyMsg("a"))
	m = mm.(Model)
	if cmd == nil || m.pickGen != gen0+1 {
		t.Fatalf("a on a commit entry must dispatch the probe (cmd=%v gen=%d)", cmd, m.pickGen)
	}
}

func TestShelfPopupAOnFileEntryNotices(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, cmd := m.Update(keyMsg("a"))
	m = mm.(Model)
	if cmd != nil || !strings.Contains(m.statusMsg, "only for a shelved commit") {
		t.Fatalf("a on a file entry must notice (cmd=%v msg=%q)", cmd, m.statusMsg)
	}
}

func TestBookmarkPopupAOnCommitBookmarkProbes(t *testing.T) {
	m := bookmarkPopModel(commitBookmark("b1", "a1b2c3d4e5f6a7b8", "fix thing"))
	gen0 := m.pickGen
	mm, cmd := m.Update(keyMsg("a"))
	m = mm.(Model)
	if cmd == nil || m.pickGen != gen0+1 {
		t.Fatalf("a on a commit bookmark must dispatch the probe (cmd=%v gen=%d)", cmd, m.pickGen)
	}
}

func TestBookmarkPopupAOnFileBookmarkNotices(t *testing.T) {
	m := bookmarkPopModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Path: "x.go"})
	mm, cmd := m.Update(keyMsg("a"))
	m = mm.(Model)
	if cmd != nil || !strings.Contains(m.statusMsg, "only for a commit bookmark") {
		t.Fatalf("a on a file bookmark must notice (cmd=%v msg=%q)", cmd, m.statusMsg)
	}
}

func TestSwitcherEscBumpsPickGen(t *testing.T) {
	m := shelfPopModel(commitShelfEntry("e1", "a1b2c3d4e5f6a7b8", "fix"))
	gen0 := m.pickGen
	mm, _ := m.Update(keyMsg("esc"))
	m = mm.(Model)
	if m.pickGen != gen0+1 {
		t.Fatalf("closing the switcher must invalidate an in-flight probe (gen=%d)", m.pickGen)
	}
}

func TestSwitcherCompareModeIgnoresA(t *testing.T) {
	m := shelfPopModel(commitShelfEntry("e1", "a1b2c3d4e5f6a7b8", "fix"))
	ref := model.FileRef{Source: model.SourceUnstaged, Path: "x.go"}
	m.shelfSwitcher().compareRef = &ref
	gen0 := m.pickGen
	mm, cmd := m.Update(keyMsg("a"))
	m = mm.(Model)
	if cmd != nil || m.pickGen != gen0 {
		t.Fatalf("compare mode must ignore a (cmd=%v gen=%d)", cmd, m.pickGen)
	}
}

func TestSwitcherHintsAdvertiseCherryPick(t *testing.T) {
	sm := shelfPopModel(commitShelfEntry("e1", "a1b2c3d4e5f6a7b8", "fix"))
	if out := sm.renderShelfPopupBox(sm.shelfSwitcher()); !strings.Contains(out, "[a] cherry-pick") {
		t.Fatalf("shelf hint line missing [a] cherry-pick:\n%s", out)
	}
	bm := bookmarkPopModel(commitBookmark("b1", "a1b2c3d4e5f6a7b8", "fix"))
	if out := bm.renderBookmarkPopupBox(bm.bookmarkSwitcher()); !strings.Contains(out, "[a] cherry-pick") {
		t.Fatalf("bookmark hint line missing [a] cherry-pick:\n%s", out)
	}
	for _, lines := range [][]contentLine{bookmarkSwitcherHelp(false), shelfSwitcherHelp(false)} {
		joined := ""
		for _, l := range lines {
			joined += l.text + "\n"
		}
		if !strings.Contains(joined, "cherry-pick") {
			t.Fatalf("cheat sheet missing the cherry-pick row:\n%s", joined)
		}
	}
}
```

Note: `keyMsg("esc")` — check the helper's mapping; if it only builds rune keys, send `tea.KeyMsg{Type: tea.KeyEsc}` directly instead.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark && go test ./internal/tui/ -run 'TestShelfPopupA|TestBookmarkPopupA|TestSwitcherEsc|TestSwitcherHints' 2>&1 | head -12`
Expected: FAIL — `a` currently does nothing (no cmd, no gen bump, no notice), hints missing.

- [ ] **Step 3: Implement**

**3a.** `internal/tui/bookmark_popup.go`:

In the nav-mode `case tea.KeyEsc:` branch (the one that runs `m = m.popLayer()`), add the gen bump first:

```go
	case tea.KeyEsc:
		m.pickGen++ // invalidate an in-flight cherry-pick probe
		m = m.popLayer()
```

In the KeyRunes action switch (alongside `"t"`, `"y"` …), add:

```go
		case "a":
			if p.compareRef != nil {
				return m, nil
			}
			b, ok := p.selected()
			if !ok {
				return m, nil
			}
			if !b.IsCommit() {
				m.statusMsg = "cherry-pick: only for a commit bookmark"
				return m, nil
			}
			return m.startPickCommit(pickTarget{sha: b.Commit, label: b.Label})
```

In `renderBookmarkPopupBox`, the hint slice gains `"[a] cherry-pick"` after `"[t] temp dir"`:

```go
	hint := []string{"[?] keys", "[enter] jump", "[e] editor", "[p] paste", "[t] temp dir", "[a] cherry-pick", "[y] copy", "[m] mark/compare", "[x] remove", "[c] vs shelf", "[/] filter", "[z] mode", "[ctrl+t] full", "[esc] close"}
```

**3b.** `internal/tui/shelf_popup.go` — same three changes:

Nav-mode esc:

```go
	case tea.KeyEsc:
		m.pickGen++ // invalidate an in-flight cherry-pick probe
		m = m.popLayer()
```

KeyRunes switch:

```go
		case "a":
			if p.compareRef != nil {
				return m, nil
			}
			e, ok := p.selected()
			if !ok {
				return m, nil
			}
			if !e.IsCommit() {
				m.statusMsg = "cherry-pick: only for a shelved commit"
				return m, nil
			}
			return m.startPickCommit(pickTarget{
				sha: e.Origin.Commit, label: e.Label,
				shelfID: e.ID, hasPatch: e.PatchSHA != "",
			})
```

Hint slice:

```go
	hint := []string{"[?] keys", "[enter] diff/browse", "[e] editor", "[p] restore", "[t] temp dir", "[a] cherry-pick", "[y] copy", "[m] mark/compare", "[x] remove", "[c] vs bookmark", "[/] filter", "[z] mode", "[ctrl+t] full", "[esc] close"}
```

**3c.** `internal/tui/popup_help.go` — in `bookmarkSwitcherHelp`'s non-compare list, after the `"t"` row:

```go
		cheatRow("a", "cherry-pick a commit bookmark onto the current branch (confirms; the commit must still exist)"),
```

In `shelfSwitcherHelp`'s non-compare list, after the `"t"` row:

```go
		cheatRow("a", "cherry-pick a shelved commit onto the current branch (confirms; falls back to its stored patch after a gc)"),
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark && go test ./internal/tui/ 2>&1 | tail -3`
Expected: the whole `internal/tui` package PASSES (the new tests and every pre-existing popup/help test — if a help drift-guard test flags the new rows, update its fixture as its comments instruct).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark
git add internal/tui/bookmark_popup.go internal/tui/shelf_popup.go internal/tui/popup_help.go internal/tui/pick_commit_test.go
git commit -m "feat(tui): a cherry-picks a commit entry from the g/G switchers

Commit entries only (file entries notice); esc invalidates the
in-flight probe; hint lines + ? cheat sheets advertise the key.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9"
```

---

### Task 5: Full verification + docs

**Files:**
- Modify: `CHANGELOG.md` (new entry at top)
- Modify: `README.md` (bookmark/shelf switcher key lists — find the section documenting the `g`/`G` switcher keys and add `a`)
- Modify: `CLAUDE.md` (package-map: `shelf` row — patch snapshot; `tui` row — the `a` pick lanes)
- Modify: `docs/superpowers/specs/2026-07-12-cherry-pick-shelf-bookmark-design.md` (Status line → implemented)

**Interfaces:** none — documentation and verification only.

- [ ] **Step 1: Run the full staged suite**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark && ./test.sh 2>&1 | tail -15`
Expected: vet+gofmt clean, unit tests pass, e2e pass.

- [ ] **Step 2: Run with the race detector**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark && ./test.sh race 2>&1 | tail -8`
Expected: PASS (required before merge).

- [ ] **Step 3: Update the docs**

- `CHANGELOG.md`: add under a new/current unreleased heading:

```markdown
- Cherry-pick a bookmarked or shelved commit: `a` in the `g`/`G` switchers
  applies the highlighted commit entry onto the current branch (confirm
  modal). While the commit exists it is a true `git cherry-pick`; a shelved
  commit whose object was gc'd is re-applied from a patch snapshot
  (`git format-patch` mailbox) now stored alongside the tar at shelve time
  (`git am --3way`, atomic). A bookmark or a pre-patch/merge shelf entry
  whose commit is gone gets a clear notice instead of a git error.
```

- `README.md`: add the `a` key to the bookmark and shelf switcher key documentation (match the surrounding phrasing; mention the patch fallback on the shelf side).
- `CLAUDE.md`: in the `shelf` package-map row, note `PutCommit` now also stores an optional format-patch blob (`PatchSHA`/`GetPatch`/`ErrNoPatch`; reclaim covers both blobs) captured best-effort by `ShelfAddCommit` (merge/oversized/failed → tar-only); in the `tui` row, note the switchers' `a` cherry-pick lanes (`pick_commit.go`: gen-guarded `CommitLookup` probe → CherryPick / ApplyPatch-with-`ShelfPatchFile` / notice).
- Spec: flip the `**Status:**` line to `implemented on feat/cherry-pick-shelf-bookmark`.

- [ ] **Step 4: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark
git add CHANGELOG.md README.md CLAUDE.md docs/superpowers/specs/2026-07-12-cherry-pick-shelf-bookmark-design.md
git commit -m "docs: cherry-pick bookmarked/shelved commits (a in g/G switchers)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9"
```

- [ ] **Step 5: Build the binary for manual testing and report**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark && go build -o ./gg ./cmd/gg && ls -la ./gg`
Expected: binary built. Deliver its ABSOLUTE path (`/mnt/t/others/gigagit/.claude/worktrees/cherry-pick-shelf-bookmark/gg`) to the user via SendUserFile. Do NOT merge — the human owns the trunk.

---

## Deferred (next task, do NOT implement now)

CLI lane: `gg shelf cherry-pick <entry-id>` driving the same two-lane logic non-interactively (recorded in the spec's Deferred-work section; needs an `agentskill.Version` bump + regenerated dogfood skill + committed regenerated SKILL.md when it lands).
