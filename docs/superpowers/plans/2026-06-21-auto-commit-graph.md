# Auto-maintain the commit-graph — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use `- [ ]`.

**Goal:** Make TUI startup/paging fast on huge repos by ensuring a commit-graph exists, using `--date-order` only when it's cheap (a graph exists) and plain order otherwise, and writing the graph once in the background on first open.

**Architecture:** The feed picks log order once per generation from whether a commit-graph file exists (`HasCommitGraph`). On open, if missing and enabled, the TUI runs `git commit-graph write --reachable` in the background, then conditionally reloads to upgrade to `--date-order`. A `Lay`-renderer gate (Task 1) confirms plain order can't crash the graph drawing.

**Tech Stack:** Go 1.26, Bubble Tea, shells to system git via `gitcmd`/`gitexec`.

## Global Constraints

- **Order captured ONCE per feed generation** — `LoadInitial` stats once and stores `dateOrder`; `LoadMore` reuses it. Never re-stat mid-generation (a flip with the same `--skip=N` silently drops commits). The only transition is the explicit post-write reload (new generation).
- `--date-order` iff a commit-graph exists; otherwise plain order (omit `--date-order`).
- Auto-write only when the graph file is **missing**; config `auto_commit_graph` default **on**; a failed/killed write is **non-fatal** (clear notice, keep plain order).
- A git verb is one invocation, built with `gitcmd`, run via `r.Runner.Run`. Frontends reach git only through `internal/domain`.
- TDD, real git in `t.TempDir()` (`newTestRepo`) or `FakeRunner` for argv. Verify test exit explicitly (no `| tail`).
- Branch `perf-commit-graph` (this worktree). Human merges.

---

### Task 1: Gate — `commitgraph.Lay` survives non-topological input

**Files:**
- Test: `internal/commitgraph/lay_nontopo_test.go` (new)
- Possibly modify: `internal/commitgraph/*.go` (only if Lay panics)

**Why first:** plain order is now a first-class mode (every first launch; permanent when auto-write is off). If `Lay` panics on a parent appearing before its child, plain-order-default is unsafe and the order policy must narrow to single-branch scope. This task decides that.

- [ ] **Step 1: Write the test**

Create `internal/commitgraph/lay_nontopo_test.go`. (Inspect the existing `Lay` signature and the row/cell type in this package first — the call below uses `Lay([]Commit{...})` with `Hash`/`Parents`; match the actual exported names.)

```go
package commitgraph

import "testing"

// TestLayNonTopologicalDoesNotPanic feeds a sequence where a parent row appears
// BEFORE its child (what plain `git log` order can produce on skewed history).
// Lay must not panic or index out of range — at worst it draws a stub lane.
func TestLayNonTopologicalDoesNotPanic(t *testing.T) {
	// Parent "p" listed before its child "c" (reversed from topological order).
	commits := []Commit{
		{Hash: "p", Parents: []string{"g"}}, // parent first (wrong order)
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

- [ ] **Step 2: Run it**

Run: `cd /mnt/t/others/gg-commitgraph && go test ./internal/commitgraph/ -run TestLayNonTopological -v`

- [ ] **Step 3: Decide based on the result**

- **PASS** (no panic): plain-order default is safe. Proceed with the plan as written. Commit the test (a permanent guard).
- **PANIC / FAIL**: add a minimal guard in `Lay` so an unknown/forward parent reference is treated as a lane stub instead of indexing a not-yet-seen row (find where Lay looks up a parent's lane and guard the missing case). Re-run to green. **Then** note in Task 3 that the order policy is unchanged (the guard makes plain order safe). If a clean guard is not feasible, switch Task 3's policy to: omit `--date-order` **only when `len(scope.Branches) == 1`** (single branch — effectively topological), keep it otherwise.

- [ ] **Step 4: Commit**

```bash
cd /mnt/t/others/gg-commitgraph
git add internal/commitgraph/
git commit -m "test(commitgraph): Lay tolerates non-topological input (plain-order gate)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 2: git layer — `WriteCommitGraph` verb + `LogScoped` order param

**Files:**
- Create: `internal/git/commitgraph_verb.go`
- Modify: `internal/git/log.go` (`LogScoped` signature)
- Modify: `internal/domain/query.go` (`logPage` call — pass `true` for now, threaded in Task 3)
- Test: `internal/git/commitgraph_verb_test.go`

**Interfaces:**
- Produces: `func (r *Repo) WriteCommitGraph(ctx context.Context) error`; `func (r *Repo) LogScoped(ctx, limit, skip int, scope LogScope, dateOrder bool) ([]model.Commit, error)`.

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
		t.Fatalf("commit-graph file not written: %v", err)
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

- [ ] **Step 2: Run, verify it fails**

Run: `cd /mnt/t/others/gg-compare && go test ./internal/git/ -run 'TestWriteCommitGraph|TestLogScopedDateOrderFlag' -v`
Expected: FAIL — `WriteCommitGraph` undefined; `LogScoped` takes 4 args not 5.

(Note: path is `/mnt/t/others/gg-commitgraph`.)

- [ ] **Step 3a: Add the verb**

Create `internal/git/commitgraph_verb.go`:

```go
package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// WriteCommitGraph writes/refreshes the repo's commit-graph cache
// (`git commit-graph write --reachable`), which lets later `git log --date-order`
// use generation numbers instead of parsing every commit. Writes atomically;
// safe to run alongside reads.
func (r *Repo) WriteCommitGraph(ctx context.Context) error {
	argv := gitcmd.New("commit-graph").Arg("write", "--reachable").ToArgv()
	_, err := r.Runner.Run(ctx, "git commit-graph write", argv)
	return err
}
```

- [ ] **Step 3b: Add the `dateOrder` param to `LogScoped`**

In `internal/git/log.go`, change the signature and the `--date-order` arg:

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

Update the doc comment's "(newest-first, --date-order)" to "(newest-first; --date-order when a commit-graph exists)".

- [ ] **Step 3c: Keep the build green — update the one caller**

In `internal/domain/query.go`, `logPage`'s call (Task 3 threads it properly; for now pass `true` to preserve current behavior):

```go
		return s.repo.LogScoped(ctx, limit, skip, scope, true)
```

- [ ] **Step 4: Run, verify it passes**

Run: `cd /mnt/t/others/gg-commitgraph && go test ./internal/git/ -run 'TestWriteCommitGraph|TestLogScopedDateOrderFlag' -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-commitgraph
git add internal/git/commitgraph_verb.go internal/git/commitgraph_verb_test.go internal/git/log.go internal/domain/query.go
git commit -m "feat(git): WriteCommitGraph verb + LogScoped date-order toggle

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 3: domain — `HasCommitGraph`, `WriteCommitGraph`, feed order capture

**Files:**
- Create: `internal/domain/commitgraph.go`
- Modify: `internal/domain/query.go` (`Snapshot` adds `HasCommitGraph`; `logPage` gains `dateOrder`)
- Modify: `internal/domain/commitfeed.go` (capture `dateOrder` per generation)
- Test: `internal/domain/commitgraph_test.go`

**Interfaces:**
- Consumes: `(*git.Repo).WriteCommitGraph`, `LogScoped(...,dateOrder)` (Task 2); `s.GitCommonDir`.
- Produces: `func (s *Service) HasCommitGraph(ctx) (bool, error)`; `func (s *Service) WriteCommitGraph(ctx) error`; `Snapshot.HasCommitGraph bool`; `logPage(ctx, limit, skip, scope, gen, dateOrder bool)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/domain/commitgraph_test.go`:

```go
package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

func realRepoSvc(t *testing.T) (*Service, string) {
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
	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
	return New(repo), dir
}

func TestHasCommitGraphFalseThenTrue(t *testing.T) {
	svc, _ := realRepoSvc(t)
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

// TestFeedOrderCapturedOncePerGeneration proves the order is read once in
// LoadInitial and reused by LoadMore even if a commit-graph appears mid-walk.
func TestFeedOrderCapturedOncePerGeneration(t *testing.T) {
	f := gitexec.NewFakeRunner()
	common := t.TempDir()
	infoDir := filepath.Join(common, "objects", "info")
	os.MkdirAll(infoDir, 0o755)
	f.SetResponse("git rev-parse (common-dir)", gitexec.Result{Stdout: common + "\n"})
	var logArgvs [][]string
	f.SetHandler("git log", func(ctx context.Context, argv []string) (gitexec.Result, error) {
		logArgvs = append(logArgvs, argv)
		return gitexec.Result{Stdout: ""}, nil
	})
	svc := New(&git.Repo{Runner: f})
	feed := svc.CommitFeed()

	feed.LoadInitial(context.Background())             // no graph yet → plain order
	os.WriteFile(filepath.Join(infoDir, "commit-graph"), []byte("x"), 0o644) // graph appears mid-generation
	feed.LoadMore(context.Background())                // MUST reuse plain order

	if len(logArgvs) < 2 {
		t.Fatalf("expected ≥2 git log calls, got %d", len(logArgvs))
	}
	for i, av := range logArgvs[:2] {
		if slices.Contains(av, "--date-order") {
			t.Errorf("git log call %d used --date-order; order must stay plain for the generation: %v", i, av)
		}
	}
}
```

> If `LoadMore` no-ops (exhausted after an empty initial page), force a page: have the handler return one fake commit line on the first call so the feed isn't exhausted. Match `logFormat` (a single `%H\x1f…` line) — or assert on just the initial call plus a fresh `LoadInitial` with the graph now present yielding `--date-order`. Keep the core assertion: **no `--date-order` while the captured order is plain.**

- [ ] **Step 2: Run, verify it fails**

Run: `cd /mnt/t/others/gg-commitgraph && go test ./internal/domain/ -run 'TestHasCommitGraph|TestFeedOrderCaptured' -v`
Expected: FAIL — `HasCommitGraph`/`WriteCommitGraph` undefined.

- [ ] **Step 3a: domain commit-graph helpers**

Create `internal/domain/commitgraph.go`:

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
)

// HasCommitGraph reports whether the repo has a commit-graph cache (a single
// file or a split chain). When present, `git log --date-order` is cheap.
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
// reservation (it writes only a cache and never touches refs or the tree).
func (s *Service) WriteCommitGraph(ctx context.Context) error {
	return s.reserveRead(ctx, func() error { return s.repo.WriteCommitGraph(ctx) })
}
```

> Use the Service's existing Read-reservation helper. Find what `CommitFiles`/`Status` use (e.g. `query(...)` wraps reads; for a no-result action use the same gate primitive — inspect `query`/`reserveRead`/the gate call in `service.go`/`query.go` and match it). If only `query[T]` exists, wrap with a throwaway value: `_, err := query(ctx, s, "commit-graph-write", func(ctx) (struct{}, error) { return struct{}{}, s.repo.WriteCommitGraph(ctx) }); return err` — but a write shouldn't be singleflight-coalesced; prefer the raw reservation the gate exposes. Confirm the exact primitive while implementing.

- [ ] **Step 3b: `Snapshot.HasCommitGraph` + `logPage` order param**

In `internal/domain/query.go`:

- Add to the `Snapshot` struct: `HasCommitGraph bool`.
- In `Snapshot`'s assembly (where `GitCommonDir` is set, ~line 146), best-effort set `snap.HasCommitGraph` (ignore error): `if has, err := s.HasCommitGraph(ctx); err == nil { snap.HasCommitGraph = has }`.
- Change `logPage` to thread `dateOrder`:

```go
func (s *Service) logPage(ctx context.Context, limit, skip int, scope LogScope, gen int, dateOrder bool) ([]model.Commit, error) {
	key := "commits:" + scopeKey(scope) + ":" + strconv.Itoa(gen) + ":" + strconv.Itoa(limit) + ":" + strconv.Itoa(skip) + ":" + strconv.FormatBool(dateOrder)
	return query(ctx, s, key, func(ctx context.Context) ([]model.Commit, error) {
		return s.repo.LogScoped(ctx, limit, skip, scope, dateOrder)
	})
}
```

- [ ] **Step 3c: feed captures order once per generation**

In `internal/domain/commitfeed.go`:

- Add a field to `CommitFeed`: `dateOrder bool`.
- In `LoadInitial`, after `gen0 := f.gen` and before the `logPage` call, capture the order ONCE (outside the lock, since it does a git call + stat):

```go
	f.mu.Unlock()

	dateOrder, _ := f.svc.HasCommitGraph(ctx) // once per generation; LoadMore reuses
	f.mu.Lock()
	f.dateOrder = dateOrder
	f.mu.Unlock()

	page, err := f.svc.logPage(cctx, commitInitialPage, 0, scope, gen0, dateOrder)
```

> Adjust to the existing lock structure — the key requirement: stat once here, store `f.dateOrder`, pass it to `logPage`. Do the stat while NOT holding `f.mu` (it makes a git call).

- In `LoadMore`, read the stored value and pass it:

```go
	scope := f.scope
	dateOrder := f.dateOrder
	...
	page, err := f.svc.logPage(ctx, commitPageSize, skip, scope, gen0, dateOrder)
```

- [ ] **Step 4: Run, verify it passes**

Run: `cd /mnt/t/others/gg-commitgraph && go test ./internal/domain/ -run 'TestHasCommitGraph|TestFeedOrderCaptured' -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-commitgraph
git add internal/domain/commitgraph.go internal/domain/commitgraph_test.go internal/domain/query.go internal/domain/commitfeed.go
git commit -m "feat(domain): HasCommitGraph + WriteCommitGraph + per-generation feed order

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 4: config — `auto_commit_graph` (default on)

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `UIConfig.AutoCommitGraph *bool` (toml `auto_commit_graph`); `func (c Config) AutoCommitGraphOn() bool`.

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestAutoCommitGraphDefaultsOn(t *testing.T) {
	if !Defaults().AutoCommitGraphOn() {
		t.Error("auto_commit_graph must default to on")
	}
}

func TestAutoCommitGraphOverlayOff(t *testing.T) {
	c := Defaults()
	off := false
	overlayUI(&c.UI, UIConfig{AutoCommitGraph: &off})
	if c.AutoCommitGraphOn() {
		t.Error("auto_commit_graph=false must override the default")
	}
}
```

(Match the actual `overlayUI` name/signature in `config.go`.)

- [ ] **Step 2: Run, verify it fails**

Run: `cd /mnt/t/others/gg-commitgraph && go test ./internal/config/ -run TestAutoCommitGraph -v`
Expected: FAIL — `AutoCommitGraph`/`AutoCommitGraphOn` undefined.

- [ ] **Step 3: Implement**

In `internal/config/config.go`:

- Add to `UIConfig`: `AutoCommitGraph *bool `toml:"auto_commit_graph"` // nil = default (on)`.
- Add a helper near the top: `func boolPtr(b bool) *bool { return &b }` (skip if one already exists).
- In `Defaults()`, set it in the `UI:` literal: `AutoCommitGraph: boolPtr(true)`.
- In `overlayUI`, copy when set: `if src.AutoCommitGraph != nil { dst.AutoCommitGraph = src.AutoCommitGraph }`.
- Add the accessor:

```go
// AutoCommitGraphOn reports whether gg may write a commit-graph on open
// (default true; unset behaves as on).
func (c Config) AutoCommitGraphOn() bool {
	return c.UI.AutoCommitGraph == nil || *c.UI.AutoCommitGraph
}
```

- [ ] **Step 4: Run, verify it passes**

Run: `cd /mnt/t/others/gg-commitgraph && go test ./internal/config/ -run TestAutoCommitGraph -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-commitgraph
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): auto_commit_graph toggle (default on)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 5: TUI — startup trigger, notice, background write, conditional reload

**Files:**
- Modify: `internal/tui/op.go` (`writeCommitGraphCmd` + `commitGraphWrittenMsg`, near `reloadRefsCmd`)
- Modify: `internal/tui/model.go` (Model flags; `dataLoadedMsg` trigger; `commitGraphWrittenMsg` handler; `commitsPaged` in `commitsPagedMsg`)
- Modify: `internal/tui/load.go` (`dataLoadedMsg` carries `hasCommitGraph`)
- Modify: `internal/tui/viewstate.go` (`panelLabel` shows the indexing notice)
- Test: `internal/tui/commitgraph_test.go`

**Interfaces:**
- Consumes: `svc.WriteCommitGraph` (Task 3), `Snapshot.HasCommitGraph`, `cfg.AutoCommitGraphOn()` (Task 4), `startFeedReload`.
- Produces: Model fields `commitGraphIndexing bool`, `commitsPaged bool`; `writeCommitGraphCmd() tea.Cmd`; `commitGraphWrittenMsg{err error}`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/commitgraph_test.go`:

```go
package tui

import "testing"

func TestCommitGraphWrittenMsgClearsNoticeAndReloadsNearTop(t *testing.T) {
	m := loadedModel(t)
	m.commitGraphIndexing = true
	m.commitsPaged = false // still near the top
	u, cmd := m.Update(commitGraphWrittenMsg{})
	mm := u.(Model)
	if mm.commitGraphIndexing {
		t.Error("notice must clear on write completion")
	}
	if cmd == nil {
		t.Error("near-top completion must trigger a feed reload")
	}
}

func TestCommitGraphWrittenMsgSkipsReloadWhenPaged(t *testing.T) {
	m := loadedModel(t)
	m.commitGraphIndexing = true
	m.commitsPaged = true // user scrolled deep
	u, cmd := m.Update(commitGraphWrittenMsg{})
	if u.(Model).commitGraphIndexing {
		t.Error("notice must clear even when reload is skipped")
	}
	if cmd != nil {
		t.Error("a deep-scrolled feed must NOT be yanked back to the top")
	}
}

func TestCommitGraphWriteFailureIsNonFatal(t *testing.T) {
	m := loadedModel(t)
	m.commitGraphIndexing = true
	u, cmd := m.Update(commitGraphWrittenMsg{err: errFake})
	if u.(Model).commitGraphIndexing {
		t.Error("notice must clear on write failure")
	}
	if cmd != nil {
		t.Error("a failed write must not reload")
	}
}

var errFake = fakeErr("boom")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

func TestIndexingNoticeRendersInCommitsLabel(t *testing.T) {
	m := loadedModel(t)
	m.commitGraphIndexing = true
	if got := m.panelLabel(panelCommits, "Commits"); !contains(got, "indexing") {
		t.Errorf("Commits label missing the indexing notice: %q", got)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

(If a `strings` import is cleaner than the inline helpers, use `strings.Contains` — drop the `contains`/`indexOf` helpers.)

- [ ] **Step 2: Run, verify it fails**

Run: `cd /mnt/t/others/gg-commitgraph && go test ./internal/tui/ -run 'TestCommitGraph|TestIndexingNotice' -v`
Expected: FAIL — `commitGraphWrittenMsg`/`commitGraphIndexing` undefined.

- [ ] **Step 3a: Model fields**

In `internal/tui/model.go`, near `commitsLoading` (~line 78):

```go
	commitGraphIndexing bool // a one-time background `commit-graph write` is running → title notice
	commitsPaged        bool // the user has paged past the first page (suppresses the post-index reload)
```

- [ ] **Step 3b: background write cmd + msg**

In `internal/tui/op.go`, near `reloadRefsCmd`:

```go
// commitGraphWrittenMsg reports the result of the one-time background
// `git commit-graph write`. err != nil is non-fatal (the feed keeps plain order).
type commitGraphWrittenMsg struct{ err error }

// writeCommitGraphCmd runs `git commit-graph write --reachable` off the UI
// thread so later --date-order walks are fast.
func (m Model) writeCommitGraphCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		return commitGraphWrittenMsg{err: svc.WriteCommitGraph(context.Background())}
	}
}
```

(Ensure `op.go` imports `context`.)

- [ ] **Step 3c: `dataLoadedMsg` carries `hasCommitGraph` + startup trigger**

In `internal/tui/load.go`, add `hasCommitGraph bool` to `dataLoadedMsg` and set it from the snapshot:

```go
	out := dataLoadedMsg{
		...
		hasCommitGraph: snap.HasCommitGraph,
	}
```

In `internal/tui/model.go`, in the `dataLoadedMsg` success block, after `m.cfg = msg.cfg` and the selection-clamp loop, before/around the `m.proc` check, add:

```go
			// First open on a repo without a commit-graph: kick off a one-time
			// background write so later --date-order walks are fast. The feed is
			// already showing commits in plain order meanwhile.
			if !msg.hasCommitGraph && m.cfg.AutoCommitGraphOn() {
				m.commitGraphIndexing = true
				return m, m.writeCommitGraphCmd()
			}
```

(Place it so it doesn't bypass the `m.proc != nil` path — at startup `m.proc` is nil, so ordering is safe; put the commit-graph trigger immediately before the `if m.proc != nil` check, matching the existing return style.)

- [ ] **Step 3d: `commitGraphWrittenMsg` handler**

In `internal/tui/model.go`'s `Update` switch, add:

```go
	case commitGraphWrittenMsg:
		m.commitGraphIndexing = false
		if msg.err != nil {
			return m, nil // non-fatal: keep plain order
		}
		if !m.commitsPaged { // still near the top — upgrade to --date-order
			return m.startFeedReload()
		}
		return m, nil // scrolled deep: don't yank; next natural reload upgrades
```

- [ ] **Step 3e: mark `commitsPaged`**

In the `commitsPagedMsg` handler (~line 226), after a page is applied:

```go
		case commitsPagedMsg:
			if m.feed != nil && msg.gen == m.feed.Gen() {
				st := m.feed.Snapshot()
				m.commits = st.Commits
				m.commitsExhausted = st.Exhausted
				m.commitsLoading = false
				m.commitsPaged = true
				m = m.rebuildCommitGraph()
			}
			return m, nil
```

- [ ] **Step 3f: indexing notice in the title**

In `internal/tui/viewstate.go`'s `panelLabel`, after the `commitsLoading` glyph block:

```go
			if m.commitGraphIndexing {
				base += " (indexing…)"
			}
```

- [ ] **Step 4: Run, verify it passes**

Run: `cd /mnt/t/others/gg-commitgraph && go test ./internal/tui/ -run 'TestCommitGraph|TestIndexingNotice' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-commitgraph
git add internal/tui/op.go internal/tui/model.go internal/tui/load.go internal/tui/viewstate.go internal/tui/commitgraph_test.go
git commit -m "feat(tui): background commit-graph write on open + indexing notice + reload

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 6: Docs + full gate

**Files:**
- Modify: `CHANGELOG.md`, `README.md`

- [ ] **Step 1: CHANGELOG**

Under `## [Unreleased]` → `### Added`:

```markdown
- **Faster startup on huge repos (auto commit-graph).** On opening a repo with
  no commit-graph, gg writes one once in the background (Commits title shows
  *(indexing…)*) and meanwhile lists commits in fast plain order; once built it
  uses `--date-order` for a correct graph. Cuts first-paint on a 1.4M-commit
  repo from ~18 s to instant. Disable with `auto_commit_graph = false` under
  `[ui]` in `.gg.toml`.
```

- [ ] **Step 2: README**

Add `auto_commit_graph` to the config/`[ui]` documentation (mirror how `wheel_step`/`commit_graph_lanes` are listed): "`auto_commit_graph` (default true) — write a commit-graph on first open for fast `--date-order` on large repos."

- [ ] **Step 3: Format + vet + full race gate**

```bash
cd /mnt/t/others/gg-commitgraph
gofmt -l internal/ cmd/
go vet ./...
./test.sh race
```
Expected: `gofmt` silent, `vet` exit 0, `./test.sh race` → `all green` exit 0 (read the status directly).

- [ ] **Step 4: Manual eyeball (recommended)**

Build and run against the linux repo; confirm: first launch shows commits immediately, the Commits title shows `(indexing…)` briefly, and after it disappears a relaunch is instant.

```bash
cd /mnt/t/others/gg-commitgraph && go build ./cmd/gg
# (the linux repo already has a commit-graph from earlier measurement; to re-test
#  the cold path: rm /home/homeend/others/linux/.git/objects/info/commit-graph first)
```

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-commitgraph
git add CHANGELOG.md README.md
git commit -m "docs: auto commit-graph for fast startup on large repos

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

## Self-Review

- **Order once per generation** (spec's blocking correctness point) → Task 3 feed capture + `TestFeedOrderCapturedOncePerGeneration`. ✅
- **`--date-order` iff graph exists** → Task 2 `LogScoped` param + Task 3 feed/logPage threading. ✅
- **Lay gate before relying on plain order** → Task 1 (decides safety; contingency = single-branch-only policy). ✅
- **Auto-write when missing, background, non-fatal** → Task 5 trigger + `commitGraphWrittenMsg` (err non-fatal). ✅
- **Conditional reload (don't yank deep scroll)** → Task 5 `commitsPaged` + handler. ✅
- **Config default on** → Task 4. ✅
- **Detection: file or chain; atomic** → Task 3 `HasCommitGraph`. ✅
- **Known limitations / non-goals** (core.commitGraph=false, no staleness, no --changed-paths) → unchanged; not implemented (correct). ✅
- Type/name consistency: `HasCommitGraph`, `WriteCommitGraph`, `LogScoped(...,dateOrder)`, `logPage(...,dateOrder)`, `commitGraphWrittenMsg`, `commitGraphIndexing`, `commitsPaged`, `AutoCommitGraphOn` used identically across tasks. ✅

**Adaptation points flagged inline (not placeholders):** the Read-reservation primitive name in Task 3 (`reserveRead` vs `query`-wrap); the exact lock placement in `LoadInitial`; `commitgraph.Lay` exported type names in Task 1; `overlayUI` signature in Task 4.
