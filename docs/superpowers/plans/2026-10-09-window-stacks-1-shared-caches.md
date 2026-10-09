# Per-worktree window stacks — phase 1: shared caches (domain)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo's CLAUDE.md forbids implementer subagents; the one session that wrote the plan executes it). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every worktree slot's `domain.Service` shares the home service's six content-keyed caches (diff, blame, sha-file, commit-files, compare-files, preview), so a repository has one cache budget however many of its worktrees were viewed.

**Architecture:** `cache.Factory` is already the one object a Service vends caches from (`Service.factory`). A new constructor builds a Service over ANOTHER service's factory; `openWith` takes the factory as a parameter. Nothing global: the slots and their services are dropped by a repo switch and the caches go with them. The TUI's `ensureView` is switched to the sharing constructor in this phase too (one line), since the domain change is inert without a caller.

**Tech Stack:** Go 1.26, the repo's own `internal/domain` test helpers (`newRealRepo`, `newRealRepoAt`, `gittest`), real `git` in `t.TempDir()`.

**Spec:** `docs/superpowers/specs/2026-10-09-per-worktree-window-stacks.md` ("Memory", "Phases" 1).

## Global Constraints

- Work only in `/work/gigagit/.claude/worktrees/fast-worktree-switch-2` (branch `feat/fast-worktree-switch-2`); every command runs there (`git -C`, absolute paths).
- Working-tree diffs stay uncached (`Key: ""`): nothing in this phase changes a cache KEY.
- TDD: write the test, watch it fail, then the code. A test that passes at once is probed by removing the change.
- `gg add <files>` then `git commit -F <msgfile>`; messages end with the session's attribution lines. Never `git add -A`, never push.
- `./test.sh race` before asking to merge; green only on the "all green" line.

## Review Focus

1. Two services sharing a factory must NOT share the `Differ` value itself: the differ carries per-service options (syntax on/off), only the cache behind it is shared (Task 1's test builds the differ from each service).
2. `cache.memFactory.Cache` is called from two services concurrently: it locks (`f.mu`), the LRU locks too; the race gate proves it with `TestSharedCachesFromTwoServicesRace`.
3. A plain `OpenTUI` / `New` keeps a private factory (the CLI, MCP, web and tests see no change): `TestNewServiceDoesNotShareCaches`.

---

### Task 1: `NewSharing` and `OpenTUISharing`

**Files:**
- Modify: `internal/domain/service.go` (`New` ~303, `OpenTUI` ~245, `openWith` ~257)
- Create: `internal/domain/sharedcache_test.go`
- Modify: `internal/tui/worktree_view.go` (`ensureView` ~115)

**Interfaces:**
- `func NewSharing(repo *git.Repo, from *Service) *Service` — a Service over `repo` that vends its caches from `from`'s factory.
- `func OpenTUISharing(workdir string, from *Service) *Service` — `OpenTUI` whose caches are `from`'s (the TUI's slot services).
- `openWith(workdir string, sshBatch bool, ring *observ.Ring, factory cache.Factory) *Service` — `factory == nil` means a private one.

- [ ] **Step 1: Write the failing tests**

```go
// internal/domain/sharedcache_test.go
package domain

import (
	"context"
	"sync"
	"testing"
)

// A diff computed through worktree A's service is a HIT through B's: the two
// services vend their "diff" cache from one factory. The second request
// deliberately differs in content under the same key, so a miss would show.
func TestSharedServicesHitOneDiffCache(t *testing.T) {
	t.Parallel()
	dir, a := newRealRepo(t)
	_, repoB := newRealRepoAt(t, dir)
	b := NewSharing(repoB.repo, a)
	first, err := a.Differ().Diff(context.Background(), Request{Key: "shared:k", Path: "a.go", Old: "x\n", New: "y\n"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.Differ().Diff(context.Background(), Request{Key: "shared:k", Path: "a.go", Old: "x\n", New: "z\n"})
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatal("B computed its own diff: the caches are not shared")
	}
}

// A commit's file list read through A is cached for B.
func TestSharedServicesShareTheCommitFilesCache(t *testing.T) {
	t.Parallel()
	dir, a := newRealRepo(t)
	_, repoB := newRealRepoAt(t, dir)
	b := NewSharing(repoB.repo, a)
	head := headHash(t, dir)
	if _, err := a.CommitFiles(context.Background(), head); err != nil {
		t.Fatal(err)
	}
	if !b.CommitFilesCached(head) {
		t.Fatal("B does not see A's commit-files entry")
	}
}

// Plain constructors keep a private factory.
func TestNewServiceDoesNotShareCaches(t *testing.T) {
	t.Parallel()
	dir, a := newRealRepo(t)
	_, b := newRealRepoAt(t, dir)
	first, _ := a.Differ().Diff(context.Background(), Request{Key: "private:k", Path: "a.go", Old: "x\n", New: "y\n"})
	second, _ := b.Differ().Diff(context.Background(), Request{Key: "private:k", Path: "a.go", Old: "x\n", New: "z\n"})
	if second == first {
		t.Fatal("two plain services share a cache")
	}
}

// Two services filling one factory concurrently: -race must stay quiet.
func TestSharedCachesFromTwoServicesRace(t *testing.T) {
	t.Parallel()
	dir, a := newRealRepo(t)
	_, repoB := newRealRepoAt(t, dir)
	b := NewSharing(repoB.repo, a)
	var wg sync.WaitGroup
	for _, s := range []*Service{a, b} {
		wg.Add(1)
		go func(s *Service) {
			defer wg.Done()
			for i := range 50 {
				_, _ = s.Differ().Diff(context.Background(), Request{Key: "race:" + string(rune('a'+i%7)), Path: "a.go", Old: "x\n", New: "y\n"})
			}
		}(s)
	}
	wg.Wait()
}
```

(`newRealRepoAt` returns `(dir, *Service)`; the test reaches the repo through `.repo` — a package-internal field — to build the sharing service. If `CommitFiles`' signature differs, use the one `query.go` ~446 exposes.)

- [ ] **Step 2: Run them, watch them fail to compile** (`NewSharing` undefined):

```bash
cd /work/gigagit/.claude/worktrees/fast-worktree-switch-2 && go test ./internal/domain -run 'TestShared|TestNewServiceDoesNotShare' 2>&1 | head
```

- [ ] **Step 3: Implement**

```go
// New wraps an existing repo (tests, callers with their own runner wiring).
func New(repo *git.Repo) *Service {
	return &Service{repo: repo, factory: cache.NewFactory(0, 0)}
}

// NewSharing is New over FROM's caches: a Service for another worktree of
// the same repository, so a commit diff, a blame, a file list or a pair's
// file set cached through one worktree is a hit from the other, and the
// repository has one cache budget however many of its worktrees are open.
// Every cache the factory vends is keyed by content (a commit, a pair, a
// rev + path); working-tree diffs are never cached. The differ itself is
// still per Service: it carries the Service's own options.
func NewSharing(repo *git.Repo, from *Service) *Service {
	return &Service{repo: repo, factory: from.factory}
}
```

`OpenTUI`/`Open`/`OpenTUIWithRing` pass `nil`; add

```go
// OpenTUISharing is OpenTUI for a worktree slot of the repository FROM
// serves: its caches are from's (NewSharing).
func OpenTUISharing(workdir string, from *Service) *Service {
	return openWith(workdir, true, observ.NewRing(200), from.factory)
}
```

and in `openWith`: `s := New(repo); if factory != nil { s.factory = factory }`.

Then `ensureView`: `svc: domain.OpenTUISharing(path, m.views[m.home].svc)` when the home slot exists, else `domain.OpenTUI(path)` (home is seeded by the first load; a slot is never made before it, but the fallback keeps the function total).

- [ ] **Step 4: Run the tests green; run the package**

```bash
cd /work/gigagit/.claude/worktrees/fast-worktree-switch-2 && go test -race ./internal/domain -run 'TestShared|TestNewServiceDoesNotShare' && go test ./internal/domain ./internal/tui 2>&1 | tail -3
```

- [ ] **Step 5: Probe** — revert `NewSharing` to `New`'s body, see `TestSharedServicesHitOneDiffCache` fail, restore.

- [ ] **Step 6: TUI test** — in `internal/tui/worktree_view_test.go` add `TestSlotServicesShareHomesCaches`: `loadedModel`, `addWorktree`, `v := m.ensureView(other)`, diff a key through `m.svc.Differ()`, the same key with other content through `v.svc.Differ()` → equal. Watch it fail first by temporarily using `OpenTUI` in `ensureView`.

- [ ] **Step 7: Docs + commit**

- `CHANGELOG.md` "Fast worktree switch (TUI)" → Changed: "Worktree slots share the repository's caches (diff, blame, file lists, compare sets, previews): one budget per repository, and a diff viewed from one worktree is a hit from another."
- `docs/CLAUDE-details.md` fast-switch section: one sentence on `OpenTUISharing`.
- `gg add` the files, `git commit -F` with message `feat(domain,tui): worktree slots share the home service's caches`.

### Task 2: Race gate and merge request

- [ ] `./test.sh race > <scratchpad>/race-p1.log 2>&1; grep -n "all green\|FAIL" <scratchpad>/race-p1.log`
- [ ] `go build -o bin/gg ./cmd/gg` in the -2 worktree.
- [ ] Update memory (`fast-switch-per-worktree-stacks.md`: phase 1 done, commit hash).
- [ ] ASK the user before `gg merge -F <msg> --into feat/fast-worktree-switch feat/fast-worktree-switch-2` (from the parent worktree, with its bin/gg); then `git merge --ff-only` in -2 and rebuild the parent's bin/gg.
