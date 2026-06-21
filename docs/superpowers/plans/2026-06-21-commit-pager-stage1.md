# Commit Pager — Stage 1 (refactor to the seam) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use `- [ ]`.

**Goal:** Introduce a `CommitPager` strategy interface, extract today's commit-feed behavior into `dateOrderPager`, and make `CommitFeed` fetch pages through the pager — with **zero behavior change**.

**Architecture:** The feed's only git interaction is page-fetching. Add a `CommitPager` interface, wrap the current `logPage` call as `dateOrderPager`, store a `pager` on the feed, and replace the two `logPage` calls with `pager.Page(...)`. `CommitFeed()` constructs a `dateOrderPager`, so output is identical. Stage 2 adds the second implementation + switch behind this seam.

**Tech Stack:** Go 1.26, domain layer (`internal/domain`).

## Global Constraints

- **No behavior change in Stage 1.** `dateOrderPager.Page` calls the existing `logPage` (which keeps `--date-order` hardcoded in `LogScoped`); `CommitFeed()` uses `dateOrderPager`.
- The pager lives in `internal/domain` so it can call the unexported `s.logPage`.
- Interface signature must be the one Stage 2 reuses: `Page(ctx, limit, skip, gen int, scope LogScope) ([]model.Commit, error)` + `Name() string`.
- TDD, real git in `t.TempDir()` or `FakeRunner` for argv. Verify test exit explicitly (no `| tail`).
- Branch `perf-commit-graph` (this worktree). Human merges.

---

### Task 1: `CommitPager` interface + `dateOrderPager`

**Files:**
- Create: `internal/domain/commitpager.go`
- Test: `internal/domain/commitpager_test.go`

**Interfaces:**
- Consumes: the existing unexported `func (s *Service) logPage(ctx, limit, skip int, scope LogScope, gen int) ([]model.Commit, error)`.
- Produces: `type CommitPager interface { Page(ctx, limit, skip, gen int, scope LogScope) ([]model.Commit, error); Name() string }`; `type dateOrderPager struct{ svc *Service }`.

- [ ] **Step 1: Write the failing test**

Create `internal/domain/commitpager_test.go`:

```go
package domain

import (
	"context"
	"slices"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
)

func TestDateOrderPagerUsesDateOrder(t *testing.T) {
	f := gitexec.NewFakeRunner()
	var argv []string
	f.SetHandler("git log", func(ctx context.Context, a []string) (gitexec.Result, error) {
		argv = a
		return gitexec.Result{Stdout: ""}, nil
	})
	svc := New(&git.Repo{Runner: f})
	p := dateOrderPager{svc: svc}

	if p.Name() != "date-order" {
		t.Errorf("Name() = %q, want date-order", p.Name())
	}
	if _, err := p.Page(context.Background(), 10, 0, 1, LogScope{}); err != nil {
		t.Fatalf("Page: %v", err)
	}
	if !slices.Contains(argv, "--date-order") {
		t.Errorf("git log argv missing --date-order: %v", argv)
	}
}
```

- [ ] **Step 2: Run, verify it fails**

Run: `cd /mnt/t/others/gg-commitgraph && go test ./internal/domain/ -run TestDateOrderPager -v`
Expected: FAIL — `dateOrderPager` / `CommitPager` undefined.

- [ ] **Step 3: Implement**

Create `internal/domain/commitpager.go`:

```go
package domain

import (
	"context"

	"github.com/gigagit/gg/internal/model"
)

// CommitPager fetches one page of commits for a feed generation. Implementations
// decide ordering and any acceleration (e.g. ensuring a commit-graph). The feed
// delegates page-fetching here so the loading strategy is swappable.
type CommitPager interface {
	Page(ctx context.Context, limit, skip, gen int, scope LogScope) ([]model.Commit, error)
	Name() string
}

// dateOrderPager is the legacy strategy: a plain `git log --date-order` walk via
// logPage. It is behavior-identical to the pre-refactor feed.
type dateOrderPager struct{ svc *Service }

func (p dateOrderPager) Page(ctx context.Context, limit, skip, gen int, scope LogScope) ([]model.Commit, error) {
	return p.svc.logPage(ctx, limit, skip, scope, gen)
}

func (p dateOrderPager) Name() string { return "date-order" }
```

- [ ] **Step 4: Run, verify it passes**

Run: `cd /mnt/t/others/gg-commitgraph && go test ./internal/domain/ -run TestDateOrderPager -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-commitgraph
git add internal/domain/commitpager.go internal/domain/commitpager_test.go
git commit -m "refactor(domain): CommitPager seam + dateOrderPager (legacy strategy)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 2: `CommitFeed` delegates page-fetching to the pager

**Files:**
- Modify: `internal/domain/commitfeed.go` (struct field, `CommitFeed()`, the two `logPage` calls)
- Test: `internal/domain/commitpager_test.go` (extend)

**Interfaces:**
- Consumes: `CommitPager`, `dateOrderPager` (Task 1).
- Produces: `CommitFeed.pager CommitPager`; the feed loads via `f.pager.Page(...)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/commitpager_test.go`:

```go
import (
	"os"
	"os/exec"
	"path/filepath"
	// (merge with the existing import block; also: "github.com/gigagit/gg/internal/observ")
)

// realFeedRepo builds a Service over a real 2-commit repo.
func realFeedRepo(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	run := func(a ...string) {
		c := exec.Command("git", a...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "c1")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("2\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "c2")
	return New(&git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))})
}

// TestFeedLoadsViaPager: the feed still returns the repo's commits after the
// refactor (behavior-identical load through the pager).
func TestFeedLoadsViaPager(t *testing.T) {
	feed := realFeedRepo(t).CommitFeed()
	st, err := feed.LoadInitial(context.Background())
	if err != nil {
		t.Fatalf("LoadInitial: %v", err)
	}
	if len(st.Commits) < 2 {
		t.Fatalf("loaded %d commits, want ≥2", len(st.Commits))
	}
}

// TestFeedStillUsesDateOrder: the feed's page fetch still passes --date-order
// (the default dateOrderPager), proving no behavior change.
func TestFeedStillUsesDateOrder(t *testing.T) {
	f := gitexec.NewFakeRunner()
	var argv []string
	f.SetHandler("git log", func(ctx context.Context, a []string) (gitexec.Result, error) {
		argv = a
		return gitexec.Result{Stdout: ""}, nil
	})
	feed := New(&git.Repo{Runner: f}).CommitFeed()
	feed.LoadInitial(context.Background())
	if !slices.Contains(argv, "--date-order") {
		t.Errorf("feed git log argv missing --date-order: %v", argv)
	}
}
```

- [ ] **Step 2: Run, verify it fails**

Run: `cd /mnt/t/others/gg-commitgraph && go test ./internal/domain/ -run 'TestFeedLoadsViaPager|TestFeedStillUsesDateOrder' -v`
Expected: FAIL to compile or fail assertion — `pager` field absent / feed not yet using it (the argv test still passes since logPage already uses --date-order; the compile of the feed change is what this drives). If both pass before any change, that is fine — proceed to wire the pager and keep them green.

> Note: these tests pass *before* the change too (the feed already uses `--date-order`). That is intentional — they are **characterization tests** locking the behavior so the refactor in Step 3 is provably safe. Run them green before and after.

- [ ] **Step 3: Wire the feed to the pager**

In `internal/domain/commitfeed.go`:

1. Add a field to `CommitFeed` (after `cancel context.CancelFunc`):

```go
	pager CommitPager // page-fetch strategy; default dateOrderPager (legacy)
```

2. Set it in the constructor:

```go
func (s *Service) CommitFeed() *CommitFeed {
	return &CommitFeed{svc: s, hashes: map[string]bool{}, pager: dateOrderPager{svc: s}}
}
```

3. Replace the `logPage` call in `LoadInitial`:

```go
	page, err := f.pager.Page(cctx, commitInitialPage, 0, gen0, scope)
```

4. Replace the `logPage` call in `LoadMore`:

```go
	page, err := f.pager.Page(ctx, commitPageSize, skip, gen0, scope)
```

(Confirm `LoadMore`'s in-scope variable names: it uses `ctx`, `skip`, `scope`, `gen0` — match them exactly. The signature order is `Page(ctx, limit, skip, gen, scope)`.)

- [ ] **Step 4: Run, verify it passes**

Run: `cd /mnt/t/others/gg-commitgraph && go test ./internal/domain/ -run 'TestFeedLoadsViaPager|TestFeedStillUsesDateOrder|TestDateOrderPager' -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 5: Full gate**

```bash
cd /mnt/t/others/gg-commitgraph
gofmt -l internal/ cmd/
go vet ./...
./test.sh race
```
Expected: `gofmt` silent, `vet` exit 0, `./test.sh race` → `all green` exit 0 (read the status directly). The TUI/commit-feed integration tests passing unchanged is the real proof of no behavior change.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gg-commitgraph
git add internal/domain/commitfeed.go internal/domain/commitpager_test.go
git commit -m "refactor(domain): CommitFeed fetches pages through CommitPager

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

## Self-Review

- Spec Stage 1 (CommitPager interface, dateOrderPager extraction, feed delegation, behavior-identical) → Tasks 1–2. ✅
- No config / git verb / TUI change in Stage 1 (those are Stage 2) — none present. ✅
- Interface signature `Page(ctx, limit, skip, gen, scope)` + `Name()` matches what Stage 2's `graphPager` will implement. ✅
- No CHANGELOG entry: Stage 1 is an internal refactor with no user-facing change (Stage 2 carries the user-facing entry). ✅
- Characterization tests (`TestFeedStillUsesDateOrder`) lock behavior across the refactor. ✅
- Adaptation point: `LoadMore`'s exact local variable names (`gen0` vs another) — match them when wiring Step 3.
