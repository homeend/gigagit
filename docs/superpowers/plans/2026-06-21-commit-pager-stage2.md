# Commit Pager — Stage 2 (graphPager + v1/v2 switch) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use `- [ ]`.

**Goal:** Add the `graphPager` strategy (ensure a commit-graph in the background; plain order until built, then `--date-order`) and the `GG_COMMIT_PAGER` switch, so the legacy and graph strategies are A/B-testable on the real repo.

**Architecture:** Behind the Stage-1 `CommitPager` seam. `graphPager` decides `--date-order` by commit-graph presence, captured once per feed generation (keyed on `gen`). `CommitFeed()` picks the pager from `GG_COMMIT_PAGER` (default `graph`). When the graph pager is active and no graph exists, the TUI writes the commit-graph in the background, shows a `(indexing…)` notice, and conditionally reloads.

**Tech Stack:** Go 1.26, Bubble Tea, system git via `gitcmd`/`gitexec`.

## Global Constraints

- **Order captured once per generation** — `graphPager` stats `HasCommitGraph` once when it first sees a new `gen`, caches the boolean, reuses it for all pages of that `gen`. A mid-generation flip with the same `--skip=N` drops commits. The only transition is the explicit post-write reload (new `gen`).
- `--date-order` iff a commit-graph exists; otherwise plain order.
- Switch = env var `GG_COMMIT_PAGER` (`graph` default | `date-order`), read in `CommitFeed()`. `graph` ⇒ auto-write; `date-order` ⇒ legacy, no commit-graph management.
- Background write only when the graph file is **missing**; **non-fatal** on failure (clear notice, keep plain order). git writes atomically.
- A git verb is one invocation. Frontends reach git only through `internal/domain`.
- TDD, real git or `FakeRunner` for argv. Verify test exit explicitly (no `| tail`). Branch `commit-pager-graph` (this worktree). Human merges.

---

### Task 1: Gate — `commitgraph.Lay` survives non-topological input

**Files:**
- Test: `internal/commitgraph/lay_nontopo_test.go` (new)
- Possibly modify: `internal/commitgraph/*.go` (only if Lay panics)

**Why first:** plain order (graph absent) can yield a parent row before its child. If `Lay` panics on that, plain-order is unsafe and the policy must narrow to single-branch scope. This decides it.

- [ ] **Step 1: Write the test** — inspect the real exported `Lay` signature + input type in this package first, then:

```go
package commitgraph

import "testing"

func TestLayNonTopologicalDoesNotPanic(t *testing.T) {
	commits := []Commit{
		{Hash: "p", Parents: []string{"g"}}, // parent listed before its child
		{Hash: "c", Parents: []string{"p"}}, // child references a row above it
		{Hash: "g", Parents: nil},
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Lay panicked on non-topological input: %v", r)
		}
	}()
	rows := Lay(commits)
	if len(rows) != len(commits) {
		t.Fatalf("Lay returned %d rows, want %d", len(rows), len(commits))
	}
}
```

(Match the actual `Lay` arg type — if it is `[]Commit{Hash,Parents}` use that; otherwise adapt the literal to the package's exported input type.)

- [ ] **Step 2: Run it** — `cd /mnt/t/others/gg-graphpager && go test ./internal/commitgraph/ -run TestLayNonTopological -v`

- [ ] **Step 3: Decide**
- **PASS:** plain-order default is safe; proceed. Commit the test as a guard.
- **PANIC:** add a minimal guard in `Lay` (treat a not-yet-seen parent as a lane stub instead of indexing a missing row); re-run green. If infeasible, note in Task 4 that `graphPager` must omit `--date-order` **only when `len(scope.Branches) == 1`** (single branch — effectively topological), keeping `--date-order` otherwise.

- [ ] **Step 4: Commit**

```bash
cd /mnt/t/others/gg-graphpager
git add internal/commitgraph/
git commit -m "test(commitgraph): Lay tolerates non-topological input (plain-order gate)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 2: git — `WriteCommitGraph` verb + `LogScoped` order param

**Files:**
- Create: `internal/git/commitgraph_verb.go`
- Modify: `internal/git/log.go` (`LogScoped` signature)
- Modify: `internal/domain/query.go` (`logPage`'s `LogScoped` call — pass `true` for now)
- Test: `internal/git/commitgraph_verb_test.go`

**Interfaces:**
- Produces: `func (r *Repo) WriteCommitGraph(ctx) error`; `func (r *Repo) LogScoped(ctx, limit, skip int, scope LogScope, dateOrder bool) ([]model.Commit, error)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/git/commitgraph_verb_test.go`:

```go
package git

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
)

func TestWriteCommitGraph(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	if err := repo.WriteCommitGraph(context.Background()); err != nil {
		t.Fatalf("WriteCommitGraph: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "objects", "info", "commit-graph")); err != nil {
		t.Fatalf("commit-graph not written: %v", err)
	}
}

func TestLogScopedDateOrderFlag(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	repo := &Repo{Runner: f}

	repo.LogScoped(context.Background(), 10, 0, LogScope{}, true)
	if !slices.Contains(f.Calls[len(f.Calls)-1].Argv, "--date-order") {
		t.Error("dateOrder=true must include --date-order")
	}
	f.Calls = nil
	repo.LogScoped(context.Background(), 10, 0, LogScope{}, false)
	if slices.Contains(f.Calls[len(f.Calls)-1].Argv, "--date-order") {
		t.Error("dateOrder=false must omit --date-order")
	}
}
```

- [ ] **Step 2: Run, verify it fails** — `cd /mnt/t/others/gg-graphpager && go test ./internal/git/ -run 'TestWriteCommitGraph|TestLogScopedDateOrderFlag' -v` → FAIL (undefined / wrong arity).

- [ ] **Step 3a: verb** — create `internal/git/commitgraph_verb.go`:

```go
package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// WriteCommitGraph writes/refreshes the commit-graph cache
// (`git commit-graph write --reachable`), letting later `git log --date-order`
// use generation numbers instead of parsing every commit. Atomic; safe beside reads.
func (r *Repo) WriteCommitGraph(ctx context.Context) error {
	argv := gitcmd.New("commit-graph").Arg("write", "--reachable").ToArgv()
	_, err := r.Runner.Run(ctx, "git commit-graph write", argv)
	return err
}
```

- [ ] **Step 3b: `LogScoped` param** — in `internal/git/log.go`:

```go
func (r *Repo) LogScoped(ctx context.Context, limit, skip int, scope LogScope, dateOrder bool) ([]model.Commit, error) {
	b := gitcmd.New("log").
		Arg("-n", strconv.Itoa(limit)).
		ArgIf(dateOrder, "--date-order").
		Arg("--decorate", "--source", "--format="+logFormat).
		ArgIf(skip > 0, "--skip="+strconv.Itoa(skip))
	if len(scope.Branches) == 0 {
		b = b.Arg("--branches", "HEAD")
	} else {
		b = b.Arg(scope.Branches...)
	}
	res, err := r.Runner.Run(ctx, "git log", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return ParseLog([]byte(res.Stdout))
}
```

- [ ] **Step 3c: keep build green** — in `internal/domain/query.go`, `logPage`'s call becomes (Task 4 threads it properly):

```go
		return s.repo.LogScoped(ctx, limit, skip, scope, true)
```

- [ ] **Step 4: Run, verify it passes** — `cd /mnt/t/others/gg-graphpager && go test ./internal/git/ -run 'TestWriteCommitGraph|TestLogScopedDateOrderFlag' -v && go build ./...` → PASS + clean build.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-graphpager
git add internal/git/commitgraph_verb.go internal/git/commitgraph_verb_test.go internal/git/log.go internal/domain/query.go
git commit -m "feat(git): WriteCommitGraph verb + LogScoped date-order toggle

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 3: domain — `HasCommitGraph`, `WriteCommitGraph`, `Snapshot.HasCommitGraph`

**Files:**
- Create: `internal/domain/commitgraph.go`
- Modify: `internal/domain/query.go` (`Snapshot` field + assembly)
- Test: `internal/domain/commitgraph_test.go`

**Interfaces:**
- Produces: `func (s *Service) HasCommitGraph(ctx) (bool, error)`; `func (s *Service) WriteCommitGraph(ctx) error`; `Snapshot.HasCommitGraph bool`.

- [ ] **Step 1: Write the failing test**

Create `internal/domain/commitgraph_test.go`:

```go
package domain

import (
	"context"
	"testing"
)

func TestHasCommitGraphFalseThenTrue(t *testing.T) {
	svc := realFeedRepo(t) // helper from commitpager_test.go (real repo)
	ctx := context.Background()
	if has, err := svc.HasCommitGraph(ctx); err != nil || has {
		t.Fatalf("fresh repo: has=%v err=%v, want false", has, err)
	}
	if err := svc.WriteCommitGraph(ctx); err != nil {
		t.Fatal(err)
	}
	if has, err := svc.HasCommitGraph(ctx); err != nil || !has {
		t.Fatalf("after write: has=%v err=%v, want true", has, err)
	}
}
```

- [ ] **Step 2: Run, verify it fails** — `go test ./internal/domain/ -run TestHasCommitGraph -v` → FAIL undefined.

- [ ] **Step 3a: helpers** — create `internal/domain/commitgraph.go`:

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
)

// HasCommitGraph reports whether the repo has a commit-graph cache (single file
// or split chain). When present, `git log --date-order` is cheap.
func (s *Service) HasCommitGraph(ctx context.Context) (bool, error) {
	dir, err := s.GitCommonDir(ctx)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(filepath.Join(dir, "objects", "info", "commit-graph")); err == nil {
		return true, nil
	}
	if _, err := os.Stat(filepath.Join(dir, "objects", "info", "commit-graphs", "commit-graph-chain")); err == nil {
		return true, nil
	}
	return false, nil
}

// WriteCommitGraph writes/refreshes the commit-graph cache under a Read
// reservation (it writes only a cache, never refs or the tree).
func (s *Service) WriteCommitGraph(ctx context.Context) error {
	return s.reserveRead(ctx, func() error { return s.repo.WriteCommitGraph(ctx) })
}
```

> Confirm the Read-reservation primitive name. Inspect how an existing gated read is run (`query(...)` wraps reads with a result; the gate's lower-level reserve call lives in `service.go`/`query.go`). If only `query[T]` exists, use it with a throwaway: `_, err := query(ctx, s, "commit-graph-write:"+strconv.Itoa(int(time.Now().UnixNano())), func(ctx)(struct{},error){ return struct{}{}, s.repo.WriteCommitGraph(ctx) }); return err` — but a unique key avoids singleflight-coalescing a write. Prefer the raw reservation if the gate exposes one.

- [ ] **Step 3b: `Snapshot.HasCommitGraph`** — in `internal/domain/query.go`:
- Add `HasCommitGraph bool` to the `Snapshot` struct.
- Where the snapshot sets `GitCommonDir` (best-effort block), add: `if has, err := s.HasCommitGraph(ctx); err == nil { snap.HasCommitGraph = has }`.

- [ ] **Step 4: Run, verify it passes** — `go test ./internal/domain/ -run TestHasCommitGraph -v && go build ./...` → PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-graphpager
git add internal/domain/commitgraph.go internal/domain/commitgraph_test.go internal/domain/query.go
git commit -m "feat(domain): HasCommitGraph + WriteCommitGraph + Snapshot.HasCommitGraph

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 4: `graphPager` + `logPage` order thread + `GG_COMMIT_PAGER` switch

**Files:**
- Modify: `internal/domain/commitpager.go` (`graphPager`; `dateOrderPager` passes `true`)
- Modify: `internal/domain/query.go` (`logPage` gains `dateOrder`)
- Modify: `internal/domain/commitfeed.go` (`CommitFeed()` env switch; `PagerName()`)
- Test: `internal/domain/commitpager_test.go` (extend)

**Interfaces:**
- Consumes: `HasCommitGraph` (Task 3), `LogScoped(...,dateOrder)` (Task 2).
- Produces: `*graphPager` (`Name()=="graph"`); `func (f *CommitFeed) PagerName() string`; `logPage(ctx, limit, skip, scope, gen, dateOrder bool)`.

- [ ] **Step 1: Write the failing tests** — append to `internal/domain/commitpager_test.go`:

```go
func TestGraphPagerOrderCapturedOncePerGeneration(t *testing.T) {
	f := gitexec.NewFakeRunner()
	common := t.TempDir()
	infoDir := filepath.Join(common, "objects", "info")
	os.MkdirAll(infoDir, 0o755)
	f.SetResponse("git rev-parse (common-dir)", gitexec.Result{Stdout: common + "\n"})
	var argvs [][]string
	f.SetHandler("git log", func(ctx context.Context, a []string) (gitexec.Result, error) {
		argvs = append(argvs, a)
		return gitexec.Result{Stdout: ""}, nil
	})
	svc := New(&git.Repo{Runner: f})
	p := &graphPager{svc: svc}

	// gen 1, no graph yet → plain order.
	p.Page(context.Background(), 10, 0, 1, LogScope{})
	os.WriteFile(filepath.Join(infoDir, "commit-graph"), []byte("x"), 0o644) // graph appears mid-generation
	p.Page(context.Background(), 10, 10, 1, LogScope{}) // same gen → MUST stay plain

	for i, a := range argvs {
		if slices.Contains(a, "--date-order") {
			t.Errorf("page %d used --date-order; gen-1 order must stay plain: %v", i, a)
		}
	}
	if p.Name() != "graph" {
		t.Errorf("Name()=%q", p.Name())
	}
}

func TestCommitFeedPicksPagerFromEnv(t *testing.T) {
	svc := New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	t.Setenv("GG_COMMIT_PAGER", "date-order")
	if got := svc.CommitFeed().PagerName(); got != "date-order" {
		t.Errorf("env date-order → %q", got)
	}
	t.Setenv("GG_COMMIT_PAGER", "")
	if got := svc.CommitFeed().PagerName(); got != "graph" {
		t.Errorf("default → %q, want graph", got)
	}
}
```

- [ ] **Step 2: Run, verify it fails** — `go test ./internal/domain/ -run 'TestGraphPager|TestCommitFeedPicksPager' -v` → FAIL undefined.

- [ ] **Step 3a: `logPage` order param** — in `internal/domain/query.go`:

```go
func (s *Service) logPage(ctx context.Context, limit, skip int, scope LogScope, gen int, dateOrder bool) ([]model.Commit, error) {
	key := "commits:" + scopeKey(scope) + ":" + strconv.Itoa(gen) + ":" + strconv.Itoa(limit) + ":" + strconv.Itoa(skip) + ":" + strconv.FormatBool(dateOrder)
	return query(ctx, s, key, func(ctx context.Context) ([]model.Commit, error) {
		return s.repo.LogScoped(ctx, limit, skip, scope, dateOrder)
	})
}
```

- [ ] **Step 3b: pagers** — in `internal/domain/commitpager.go`, update `dateOrderPager` and add `graphPager`:

```go
import (
	"context"
	"sync"

	"github.com/gigagit/gg/internal/model"
)

func (p dateOrderPager) Page(ctx context.Context, limit, skip, gen int, scope LogScope) ([]model.Commit, error) {
	return p.svc.logPage(ctx, limit, skip, scope, gen, true) // legacy: always --date-order
}

// graphPager uses --date-order only when a commit-graph exists (cheap there),
// else plain order. The order is captured ONCE per generation (keyed on gen) so
// pages of one generation share an order — a mid-generation flip with the same
// --skip would drop commits.
type graphPager struct {
	svc       *Service
	mu        sync.Mutex
	gen       int  // generation whose order is cached (0 = none)
	dateOrder bool // cached order for gen
}

func (p *graphPager) Page(ctx context.Context, limit, skip, gen int, scope LogScope) ([]model.Commit, error) {
	p.mu.Lock()
	if gen != p.gen {
		p.gen = gen
		has, _ := p.svc.HasCommitGraph(ctx)
		p.dateOrder = has
	}
	do := p.dateOrder
	p.mu.Unlock()
	return p.svc.logPage(ctx, limit, skip, scope, gen, do)
}

func (p *graphPager) Name() string { return "graph" }
```

- [ ] **Step 3c: env switch + `PagerName`** — in `internal/domain/commitfeed.go`:

```go
import "os" // add to the import block

func (s *Service) CommitFeed() *CommitFeed {
	var pager CommitPager = &graphPager{svc: s}
	if os.Getenv("GG_COMMIT_PAGER") == "date-order" {
		pager = dateOrderPager{svc: s}
	}
	return &CommitFeed{svc: s, hashes: map[string]bool{}, pager: pager}
}

// PagerName reports the active page strategy ("graph" | "date-order"), so the
// frontend can decide whether to auto-write the commit-graph.
func (f *CommitFeed) PagerName() string { return f.pager.Name() }
```

- [ ] **Step 4: Run, verify it passes** — `go test ./internal/domain/ -run 'TestGraphPager|TestCommitFeedPicksPager|TestDateOrderPager|TestFeed' -v && go build ./...` → PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-graphpager
git add internal/domain/commitpager.go internal/domain/query.go internal/domain/commitfeed.go internal/domain/commitpager_test.go
git commit -m "feat(domain): graphPager (per-gen order) + GG_COMMIT_PAGER switch

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 5: TUI — background write, indexing notice, conditional reload

**Files:**
- Modify: `internal/tui/op.go` (`writeCommitGraphCmd` + `commitGraphWrittenMsg`)
- Modify: `internal/tui/load.go` (`dataLoadedMsg.hasCommitGraph`)
- Modify: `internal/tui/model.go` (Model flags; `dataLoadedMsg` trigger; `commitGraphWrittenMsg` handler; `commitsPaged` in `commitsPagedMsg`)
- Modify: `internal/tui/viewstate.go` (`panelLabel` notice)
- Test: `internal/tui/commitgraph_test.go`

**Interfaces:**
- Consumes: `svc.WriteCommitGraph`, `Snapshot.HasCommitGraph`, `m.feed.PagerName()`, `startFeedReload`.
- Produces: Model `commitGraphIndexing bool`, `commitsPaged bool`; `writeCommitGraphCmd() tea.Cmd`; `commitGraphWrittenMsg{err error}`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/commitgraph_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

func TestCommitGraphWrittenMsgClearsAndReloadsNearTop(t *testing.T) {
	m := loadedModel(t)
	m.commitGraphIndexing = true
	m.commitsPaged = false
	u, cmd := m.Update(commitGraphWrittenMsg{})
	if u.(Model).commitGraphIndexing {
		t.Error("notice must clear on completion")
	}
	if cmd == nil {
		t.Error("near-top completion must reload the feed")
	}
}

func TestCommitGraphWrittenMsgSkipsReloadWhenPaged(t *testing.T) {
	m := loadedModel(t)
	m.commitGraphIndexing = true
	m.commitsPaged = true
	u, cmd := m.Update(commitGraphWrittenMsg{})
	if u.(Model).commitGraphIndexing {
		t.Error("notice must clear even when reload is skipped")
	}
	if cmd != nil {
		t.Error("a deep-scrolled feed must not be yanked to the top")
	}
}

func TestCommitGraphWriteFailureIsNonFatal(t *testing.T) {
	m := loadedModel(t)
	m.commitGraphIndexing = true
	u, cmd := m.Update(commitGraphWrittenMsg{err: cgErr("boom")})
	if u.(Model).commitGraphIndexing {
		t.Error("notice must clear on failure")
	}
	if cmd != nil {
		t.Error("a failed write must not reload")
	}
}

type cgErr string

func (e cgErr) Error() string { return string(e) }

func TestIndexingNoticeRendersInCommitsLabel(t *testing.T) {
	m := loadedModel(t)
	m.commitGraphIndexing = true
	if !strings.Contains(m.panelLabel(panelCommits, "Commits"), "indexing") {
		t.Error("Commits label must show the indexing notice")
	}
}
```

- [ ] **Step 2: Run, verify it fails** — `go test ./internal/tui/ -run 'TestCommitGraph|TestIndexingNotice' -v` → FAIL undefined.

- [ ] **Step 3a: Model fields** — in `internal/tui/model.go` near `commitsLoading`:

```go
	commitGraphIndexing bool // one-time background commit-graph write running → title notice
	commitsPaged        bool // user paged past the first page (suppresses the post-index reload)
```

- [ ] **Step 3b: write cmd + msg** — in `internal/tui/op.go` near `reloadRefsCmd` (ensure `context` imported):

```go
// commitGraphWrittenMsg reports the one-time background commit-graph write.
// err != nil is non-fatal (the feed keeps plain order).
type commitGraphWrittenMsg struct{ err error }

func (m Model) writeCommitGraphCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		return commitGraphWrittenMsg{err: svc.WriteCommitGraph(context.Background())}
	}
}
```

- [ ] **Step 3c: `dataLoadedMsg` carries the flag + startup trigger** — in `internal/tui/load.go` add `hasCommitGraph bool` to `dataLoadedMsg` and set `hasCommitGraph: snap.HasCommitGraph` in the `out` literal. In `internal/tui/model.go` `dataLoadedMsg` success block (after `m.cfg`/clamp loop, immediately before the `if m.proc != nil` check):

```go
			// First open under the graph pager on a repo without a commit-graph:
			// write it once in the background; the feed already shows plain-order
			// commits meanwhile.
			if !msg.hasCommitGraph && m.feed != nil && m.feed.PagerName() == "graph" {
				m.commitGraphIndexing = true
				return m, m.writeCommitGraphCmd()
			}
```

- [ ] **Step 3d: completion handler** — in `internal/tui/model.go` `Update` switch:

```go
	case commitGraphWrittenMsg:
		m.commitGraphIndexing = false
		if msg.err != nil {
			return m, nil // non-fatal
		}
		if !m.commitsPaged {
			return m.startFeedReload()
		}
		return m, nil
```

- [ ] **Step 3e: mark paged** — in the `commitsPagedMsg` handler, after applying a page, add `m.commitsPaged = true`.

- [ ] **Step 3f: notice** — in `internal/tui/viewstate.go` `panelLabel`, after the `commitsLoading` glyph block:

```go
			if m.commitGraphIndexing {
				base += " (indexing…)"
			}
```

- [ ] **Step 4: Run, verify it passes** — `go test ./internal/tui/ -run 'TestCommitGraph|TestIndexingNotice' -v` → PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-graphpager
git add internal/tui/op.go internal/tui/load.go internal/tui/model.go internal/tui/viewstate.go internal/tui/commitgraph_test.go
git commit -m "feat(tui): background commit-graph write + indexing notice + conditional reload

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 6: Docs + full gate + real-repo eyeball

**Files:** `CHANGELOG.md`, `README.md`

- [ ] **Step 1: CHANGELOG** — under `## [Unreleased]` → `### Added`:

```markdown
- **Faster startup on huge repos (commit-graph).** On opening a repo with no
  commit-graph, gg now writes one once in the background (Commits title shows
  *(indexing…)*) and lists commits in fast plain order meanwhile; once built it
  uses `--date-order`. Cuts first interaction on a 1.4M-commit repo from ~18 s to
  instant. Set `GG_COMMIT_PAGER=date-order` to use the legacy always-`--date-order`
  loader (the pre-change behavior).
```

- [ ] **Step 2: README** — document `GG_COMMIT_PAGER` (env vars / performance section): `graph` (default) = auto commit-graph + plain-order-until-built; `date-order` = legacy.

- [ ] **Step 3: Format + vet + full race gate**

```bash
cd /mnt/t/others/gg-graphpager
gofmt -l internal/ cmd/
go vet ./...
./test.sh race
```
Expected: gofmt silent, vet exit 0, `all green` exit 0.

- [ ] **Step 4: Real-repo eyeball (the whole point)** — build and A/B on linux:

```bash
cd /mnt/t/others/gg-graphpager && go build ./cmd/gg
rm -f /home/homeend/others/linux/.git/objects/info/commit-graph   # force the cold path
# v2 (default): first launch should paint commits immediately, title shows (indexing…), then a relaunch is instant
( cd /home/homeend/others/linux && /mnt/t/others/gg-graphpager/gg )   # quit with q after observing
# v1 (legacy): first launch blocks ~18s before painting
rm -f /home/homeend/others/linux/.git/objects/info/commit-graph
( cd /home/homeend/others/linux && GG_COMMIT_PAGER=date-order /mnt/t/others/gg-graphpager/gg )
```
Confirm: v2 paints fast + self-heals; v1 is the slow baseline. (Note observations for the merge report.)

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-graphpager
git add CHANGELOG.md README.md
git commit -m "docs: GG_COMMIT_PAGER + auto commit-graph for fast large-repo startup

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

## Self-Review

- Lay gate before plain-order reliance → Task 1. ✅
- `WriteCommitGraph` verb + `LogScoped` toggle → Task 2. ✅
- `HasCommitGraph` + `Snapshot.HasCommitGraph` → Task 3. ✅
- `graphPager` order captured once per generation (correctness) + `GG_COMMIT_PAGER` switch + `PagerName` → Task 4 (`TestGraphPagerOrderCapturedOncePerGeneration`, `TestCommitFeedPicksPagerFromEnv`). ✅
- Background write + notice + conditional reload + non-fatal failure → Task 5. ✅
- Docs + real-repo A/B → Task 6. ✅
- Names consistent: `HasCommitGraph`, `WriteCommitGraph`, `LogScoped(...,dateOrder)`, `logPage(...,dateOrder)`, `graphPager`, `PagerName`, `commitGraphWrittenMsg`, `commitGraphIndexing`, `commitsPaged`, `GG_COMMIT_PAGER`. ✅
- Adaptation points flagged: Read-reservation primitive (Task 3), `Lay` input type (Task 1), `dataLoadedMsg`/handler exact placement (Task 5).
