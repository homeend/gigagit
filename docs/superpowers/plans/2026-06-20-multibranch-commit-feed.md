# Multi-branch Commit Feed (Phase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The Commits panel shows all local branches by default in date order with branch/HEAD labels; a branch can be soloed and un-soloed via the `.` menu — all paged, throttled, and supersede-cancellable for gigantic repos.

**Architecture:** A scoped `git log` verb (`--branches`/named refs, `--date-order`, `%D` decorations) feeds the existing paged `CommitFeed`, which gains a scope + supersede-cancellation. The TUI holds an included-branch list (empty = all), driven by `.`-menu actions, and renders labels in `commitRows`.

**Tech Stack:** Go 1.26, Bubble Tea. No new deps. No CLI/agentskill change.

## Global Constraints

- Work in the existing worktree on branch `worktree-commit-multibranch-feed` (off `main` tip `f81d37f`). Worktree-relative paths only.
- A git verb = one invocation built with `gitcmd`, run via `r.Runner`. `internal/tui`/`internal/cli` never import `internal/git` (archtest).
- Preserve existing batching (`commitInitialPage=50`, `commitPageSize=200`) and throttling (`LimitRunner` + feed `inFlight` ≤1). Rely on git's commit-graph; don't write it. **Defer** the cross-scope page cache (keep `logPage` cache-ready only).
- Menu-only actions get a help row (not a footer key). Run `./test.sh race` before done. Commit trailers as in the repo.

---

### Task 1: git + model — scoped log verb with decorations

Add `model.Ref`/`RefKind` + `Commit.Refs`; add `LogScope` + `LogScoped` (`--date-order`, `--branches`/names, `%D`); parse `%D`. Additive — the old `Log` stays until Task 2 migrates the caller.

**Files:**
- Modify: `internal/model/model.go`
- Modify: `internal/git/log.go`
- Test: `internal/git/log_test.go`, `internal/model/*` (parse covered in git test)

**Interfaces:**
- Produces: `model.RefKind` (`RefLocal/RefRemote/RefTag/RefHead`), `model.Ref{Name string; Kind RefKind}`, `model.Commit.Refs []Ref`; `git.LogScope{Branches []string}`; `(*Repo).LogScoped(ctx, limit, skip int, scope LogScope) ([]model.Commit, error)`.

- [ ] **Step 1: Write the failing model+parse test**

Add to `internal/git/log_test.go`:

```go
func TestParseLogDecorations(t *testing.T) {
	// %D field appended: "HEAD -> main, feature, tag: v1, origin/main" and an empty one.
	line1 := strings.Join([]string{"h1", "p1", "Ada", "1700000000", "subj one", "HEAD -> main, feature, tag: v1, origin/main"}, "\x1f")
	line2 := strings.Join([]string{"h2", "", "Bo", "1700000001", "subj two", ""}, "\x1f")
	cs, err := ParseLog([]byte(line1 + "\n" + line2 + "\n"))
	if err != nil || len(cs) != 2 {
		t.Fatalf("parse: %v len=%d", err, len(cs))
	}
	if cs[1].Refs != nil {
		t.Fatalf("undecorated commit should have nil Refs, got %+v", cs[1].Refs)
	}
	byName := map[string]model.Ref{}
	for _, r := range cs[0].Refs {
		byName[r.Name] = r
	}
	if byName["main"].Kind != model.RefLocal || !byName["main"].Head {
		t.Fatalf("main should be the head local branch: %+v", byName["main"])
	}
	if byName["feature"].Kind != model.RefLocal || byName["feature"].Head {
		t.Fatalf("feature should be a non-head local branch: %+v", byName["feature"])
	}
	if byName["v1"].Kind != model.RefTag || byName["origin/main"].Kind != model.RefRemote {
		t.Fatalf("tag/remote kinds wrong: %+v", cs[0].Refs)
	}
}

func TestLogScopedArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	r := &Repo{Runner: f}
	if _, err := r.LogScoped(context.Background(), 50, 0, LogScope{}); err != nil {
		t.Fatal(err)
	}
	all := f.LastArgv()
	if !contains(all, "--date-order") || !contains(all, "--decorate") || !contains(all, "--branches") || !contains(all, "HEAD") {
		t.Fatalf("all-scope argv missing --date-order/--decorate/--branches/HEAD: %v", all)
	}
	if _, err := r.LogScoped(context.Background(), 50, 20, LogScope{Branches: []string{"feat"}}); err != nil {
		t.Fatal(err)
	}
	one := f.LastArgv()
	if !contains(one, "feat") || !contains(one, "--skip=20") || contains(one, "--branches") {
		t.Fatalf("solo-scope argv wrong: %v", one)
	}
}
```

(Use the test helpers already in `log_test.go`/the package for `FakeRunner`/argv capture; if `contains`/`containsSeq`/`LastArgv` don't exist by those names, use the package's existing equivalents — grep `internal/git/*_test.go` and `internal/gitexec` for the argv-capture API.)

- [ ] **Step 2: Run it — expect FAIL**

Run: `go test ./internal/git/ -run 'TestParseLogDecorations|TestLogScopedArgv' 2>&1 | tail`
Expected: FAIL (build: `LogScoped`, `model.Ref`, `Commit.Refs` undefined).

- [ ] **Step 3: Add the model types**

In `internal/model/model.go`, add after `Commit`:

```go
// RefKind classifies a ref decoration on a commit.
type RefKind int

const (
	RefLocal  RefKind = iota // local branch
	RefRemote                // remote-tracking branch
	RefTag
	RefHead // HEAD marker (current branch / detached)
)

// Ref is one ref decoration pointing at a commit (from `git log %D`).
// Head marks the local branch that HEAD currently points at (the current branch).
type Ref struct {
	Name string
	Kind RefKind
	Head bool
}
```

And add `Refs []Ref` to the `Commit` struct (after `UnixTime`):

```go
type Commit struct {
	Hash     string
	Parents  []string
	Author   string
	Subject  string
	UnixTime int64
	Refs     []Ref // ref decorations (branch/tag/HEAD); nil when undecorated
}
```

- [ ] **Step 4: Extend the log format + parse, add LogScoped**

In `internal/git/log.go`, extend the format and parser, and add the scoped verb:

```go
// logFormat separates fields with \x1f (unit separator); one commit per line.
// Trailing %D carries ref decorations ("HEAD -> main, feature, tag: v1").
const logFormat = "%H%x1f%P%x1f%an%x1f%at%x1f%s%x1f%D"

// LogScope selects which refs the walk covers. Empty Branches → all local
// branches (--branches); otherwise exactly the listed branch names.
type LogScope struct {
	Branches []string
}

// LogScoped returns up to limit commits (newest-first, --date-order) reachable
// from the scope's refs, skipping the first skip. Replaces the HEAD-only Log.
func (r *Repo) LogScoped(ctx context.Context, limit, skip int, scope LogScope) ([]model.Commit, error) {
	b := gitcmd.New("log").
		Arg("-n", strconv.Itoa(limit), "--date-order", "--decorate", "--format="+logFormat).
		ArgIf(skip > 0, "--skip="+strconv.Itoa(skip))
	if len(scope.Branches) == 0 {
		// All local branches PLUS HEAD, so a detached HEAD's commits still show
		// (git dedupes HEAD when it's already on a branch).
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

`--decorate` (bare = short names; **not** `=full`, which would yield
`refs/heads/main` and break the short-name parser) forces `%D` to populate
across git versions.

Update `ParseLog`: bump the field guard to `len(f) < 6` and parse the 6th field:

```go
		f := strings.Split(line, "\x1f")
		if len(f) < 6 {
			continue
		}
		c := model.Commit{Hash: f[0], Author: f[2], Subject: f[4]}
		if p := strings.Fields(f[1]); len(p) > 0 {
			c.Parents = p
		}
		if t, err := strconv.ParseInt(f[3], 10, 64); err == nil {
			c.UnixTime = t
		}
		c.Refs = parseDecorations(f[5])
		out = append(out, c)
```

Add the decoration parser:

```go
// parseDecorations splits a `%D` value ("HEAD -> main, feature, tag: v1,
// origin/main") into refs. Empty → nil.
func parseDecorations(d string) []model.Ref {
	d = strings.TrimSpace(d)
	if d == "" {
		return nil
	}
	var refs []model.Ref
	for _, tok := range strings.Split(d, ", ") {
		tok = strings.TrimSpace(tok)
		switch {
		case tok == "":
			continue
		case strings.HasPrefix(tok, "HEAD -> "):
			// HEAD points at this local branch (the current branch).
			refs = append(refs, model.Ref{Name: strings.TrimPrefix(tok, "HEAD -> "), Kind: model.RefLocal, Head: true})
		case tok == "HEAD":
			// detached HEAD
			refs = append(refs, model.Ref{Name: "HEAD", Kind: model.RefHead})
		case strings.HasPrefix(tok, "tag: "):
			refs = append(refs, model.Ref{Name: strings.TrimPrefix(tok, "tag: "), Kind: model.RefTag})
		case strings.Contains(tok, "/"): // Phase-1 simplification: slashy ⇒ remote-tracking
			refs = append(refs, model.Ref{Name: tok, Kind: model.RefRemote})
		default:
			refs = append(refs, model.Ref{Name: tok, Kind: model.RefLocal})
		}
	}
	return refs
}
```

Keep the existing `Log` method as-is for now (Task 2 removes it).

- [ ] **Step 5: Run git tests**

Run: `go test ./internal/git/ ./internal/model/ 2>&1 | tail -5`
Expected: PASS (the existing `Log`/`ParseLog` tests must still pass — they now also populate `Refs`; if an existing test builds a 5-field log line, it will skip under the `< 6` guard — update those fixtures to append a trailing `\x1f` empty decoration field).

- [ ] **Step 6: Real-git decoration test (proves `%D` actually populates)**

The argv/parse tests use canned stdout; they do NOT prove real `git log` emits
decorations under our flags. Add a real-git test using the package's repo helper
(grep `internal/git/*_test.go` for `newRepo`/`newTestRepo`/`t.TempDir` + a commit
helper, and mirror it):

```go
func TestLogScopedRealDecorations(t *testing.T) {
	r := newTestRepo(t) // makes a real repo in t.TempDir(); adjust to the helper's name
	// one commit on main, a second branch, and a tag (use the helper's commit/branch API)
	writeCommit(t, r, "a.txt", "1", "first")
	runGit(t, r, "branch", "feature")
	runGit(t, r, "tag", "v1")
	cs, err := r.LogScoped(context.Background(), 10, 0, LogScope{})
	if err != nil || len(cs) == 0 {
		t.Fatalf("LogScoped: %v len=%d", err, len(cs))
	}
	byName := map[string]model.Ref{}
	for _, r := range cs[0].Refs {
		byName[r.Name] = r
	}
	if !byName["main"].Head || byName["main"].Kind != model.RefLocal {
		t.Fatalf("expected main as head local branch, got refs %+v", cs[0].Refs)
	}
	if byName["feature"].Kind != model.RefLocal || byName["v1"].Kind != model.RefTag {
		t.Fatalf("expected feature(local)+v1(tag), got %+v", cs[0].Refs)
	}
}
```

Run: `go test ./internal/git/ -run TestLogScopedRealDecorations -v 2>&1 | tail`
Expected: PASS — confirms `--decorate`+`%D` yields short-name decorations with
the `Head` flag. (If `Refs` come back empty, the `--decorate` flag is the fix;
if names are `refs/heads/main`, you used `=full` — use bare `--decorate`.)

- [ ] **Step 7: Commit**

```bash
git add internal/model/model.go internal/git/log.go internal/git/log_test.go
git commit -m "feat(git): scoped date-ordered log with ref decorations

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 2: domain — feed scope + supersede-cancellation

Thread a scope through `logPage`/`CommitFeed`, migrate to `LogScoped`, and cancel a superseded in-flight load.

**Files:**
- Modify: `internal/domain/query.go` (`logPage`)
- Modify: `internal/domain/commitfeed.go` (scope field, SetScope, cancellation)
- Modify: `internal/git/log.go` (remove the now-unused `Log`)
- Modify: `internal/git/log_test.go` (remove/migrate the old `Log` test — it won't compile once `Log` is gone)
- Modify (maybe): `internal/gitexec/fake.go` (add a blocking per-call handler if the fake lacks one — see Step 1)
- Test: `internal/domain/commitfeed_test.go`, `internal/domain/query_test.go`

**Interfaces:**
- Consumes: `git.LogScope`, `(*Repo).LogScoped`.
- Produces: `domain.LogScope` (alias/own type), `(*CommitFeed).SetScope(scope LogScope)`, `logPage(ctx, limit, skip int, scope LogScope)`.

- [ ] **Step 0: Verify (or add) a blocking fake handler**

Grep `internal/gitexec/fake*.go` for the fake API. The cancel test needs a
per-call hook that receives the `ctx` and can **block** (the existing
`SetResponse` returns canned output immediately). If no blocking hook exists, add
a minimal one:

```go
// SetHandler registers a per-name callback invoked for matching Run calls; it
// receives the call ctx and may block (for ctx-cancellation tests).
func (f *FakeRunner) SetHandler(name string, h func(ctx context.Context, argv []string) (Result, error)) { /* store; Run dispatches to it before falling back to SetResponse */ }
```

Wire `Run`/`RunEnv` to call a matching handler first. Keep it tiny; run
`go test ./internal/gitexec/` to confirm nothing breaks.

- [ ] **Step 1: Write the failing scope/cancel tests**

Add to `internal/domain/commitfeed_test.go` (mirror existing feed tests' setup — a Service over a FakeRunner):

```go
func TestFeedScopeChangesRefspec(t *testing.T) {
	svc, f := commitFeedSvc(t) // existing helper; else build per the file's pattern
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	feed := svc.CommitFeed()
	feed.SetScope(LogScope{Branches: []string{"feat"}})
	_, _ = feed.LoadInitial(context.Background())
	if argv := f.LastArgv(); !argvContains(argv, "feat") || argvContains(argv, "--branches") {
		t.Fatalf("solo scope should walk 'feat', not --branches: %v", argv)
	}
}

func TestFeedSupersedeCancelsPriorLoad(t *testing.T) {
	svc, f := commitFeedSvc(t)
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	f.SetHandler("git log", func(ctx context.Context, argv []string) (gitexec.Result, error) {
		started <- ctx
		<-release // block until released
		return gitexec.Result{}, ctx.Err()
	})
	feed := svc.CommitFeed()
	go func() { _, _ = feed.LoadInitial(context.Background()) }()
	ctx1 := <-started
	// A second LoadInitial supersedes the first; its ctx must be cancelled.
	go func() { _, _ = feed.LoadInitial(context.Background()) }()
	select {
	case <-ctx1.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("prior load ctx was not cancelled on supersede")
	}
	close(release)
}
```

(If `FakeRunner` lacks `SetHandler`/`argvContains`/`commitFeedSvc`, add a minimal handler hook to `FakeRunner` or use the package's existing fake API — grep `internal/gitexec/fake*.go` and `commitfeed_test.go`.)

- [ ] **Step 2: Run — expect FAIL**

Run: `go test ./internal/domain/ -run 'TestFeedScope|TestFeedSupersede' 2>&1 | tail`
Expected: FAIL (`SetScope`/`LogScope` undefined; no cancellation).

- [ ] **Step 3: Scope the domain logPage**

In `internal/domain/query.go`, alias the git scope and thread it:

```go
// LogScope re-exports git.LogScope for the feed's scope.
type LogScope = git.LogScope

// logPage is the gated, singleflighted commit-page read the CommitFeed uses.
// The singleflight key includes the scope and skip so pages/scopes don't collapse.
func (s *Service) logPage(ctx context.Context, limit, skip int, scope LogScope) ([]model.Commit, error) {
	key := "commits:" + scopeKey(scope) + ":" + strconv.Itoa(limit) + ":" + strconv.Itoa(skip)
	return query(ctx, s, key, func(ctx context.Context) ([]model.Commit, error) {
		return s.repo.LogScoped(ctx, limit, skip, scope)
	})
}

func scopeKey(scope LogScope) string {
	if len(scope.Branches) == 0 {
		return "all"
	}
	return strings.Join(scope.Branches, ",")
}
```

Add the `internal/git` import if not present, plus `strings`.

- [ ] **Step 4: Feed scope + cancellation**

In `internal/domain/commitfeed.go`, add fields and methods:

```go
type CommitFeed struct {
	svc *Service

	mu        sync.Mutex
	scope     LogScope
	commits   []model.Commit
	hashes    map[string]bool
	skip      int
	exhausted bool
	gen       int
	inFlight  bool
	cancel    context.CancelFunc // cancels the in-flight page's ctx on supersede
}

// SetScope sets the refspec for subsequent loads. Callers then LoadInitial.
func (f *CommitFeed) SetScope(scope LogScope) {
	f.mu.Lock()
	f.scope = scope
	f.mu.Unlock()
}
```

In `LoadInitial`, cancel any prior in-flight load and derive a cancelable ctx:

```go
func (f *CommitFeed) LoadInitial(ctx context.Context) (FeedState, error) {
	f.mu.Lock()
	if f.cancel != nil {
		f.cancel() // stop a superseded in-flight walk
	}
	cctx, cancel := context.WithCancel(ctx)
	f.cancel = cancel
	f.gen++
	gen0 := f.gen
	scope := f.scope
	f.commits = nil
	f.hashes = map[string]bool{}
	f.skip = 0
	f.exhausted = false
	f.inFlight = true
	f.mu.Unlock()

	page, err := f.svc.logPage(cctx, commitInitialPage, 0, scope)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.inFlight = false
	if f.gen == gen0 {
		f.cancel = nil // our load finished; nothing to cancel
	}
	if err != nil {
		return f.snapshotLocked(), err
	}
	if f.gen != gen0 {
		return f.snapshotLocked(), nil
	}
	// ... (existing dedupe append, skip, exhausted) unchanged ...
}
```

`LoadMore` passes `f.scope` to `logPage` (read it under the lock alongside `skip`). It does NOT manage `f.cancel` (only reloads supersede).

**Gen-stamp on the superseded path (double-reload correctness):** on the
`if f.gen != gen0` early return, stamp the returned `FeedState.Gen` with **gen0**
(this load's own generation), not the current `f.gen` — e.g. return
`FeedState{Commits: …, Exhausted: …, Gen: gen0}` rather than `snapshotLocked()`.
Then `reloadFeedCmd` tags `commitsReloadedMsg` with `st.Gen` and the handler drops
it when `st.Gen != feed.Gen()`, so a superseded reload (A) that finishes after a
newer one (B) reset the feed can't paint A's empty/stale state. Add a test:
fire two `LoadInitial`s; the first's returned state carries the older gen and is
droppable.

- [ ] **Step 5: Remove the dead HEAD-only `Log`**

In `internal/git/log.go` delete the `Log` method (now unused — `LogScoped` replaces it), and in `internal/git/log_test.go` delete/migrate the old `Log`-specific test (e.g. `TestLog`) so the package still compiles — fold any unique assertions into `TestLogScopedArgv`. Run `grep -rn '\.Log(' internal | grep -v _test | grep -v LogScoped | grep -v file_log` → expect no matches, and `grep -rn 'r\.Log(\|\.Log(ctx' internal/git/log_test.go` → none.

- [ ] **Step 6: Run domain + git tests**

Run: `go test ./internal/git/ ./internal/domain/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/query.go internal/domain/commitfeed.go internal/domain/commitfeed_test.go internal/git/log.go
git commit -m "feat(domain): commit feed scope + supersede-cancellation

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 3: TUI — scope state, reload, solo/show-all menu actions

**Files:**
- Modify: `internal/tui/model.go` (state field, reload cmd + msg handler, header label)
- Create: `internal/tui/commit_scope.go` (the menu rows + reload command)
- Modify: `internal/tui/action_menu.go` (inject the new rows)
- Modify: `internal/tui/view.go` (Commits header label + Branches solo marker)
- Test: `internal/tui/commit_scope_test.go` (create)

**Interfaces:**
- Consumes: `m.feed *domain.CommitFeed`, `domain.LogScope`, `m.selectedBranch()`, `m.backingIndex(panelBranches)`, `actionRow`, `m.feed.SetScope`/`LoadInitial`.
- Produces: `m.commitScopeBranches []string`; `commitSoloRow`/`commitShowAllRow() (actionRow, bool)`; `reloadFeedCmd() tea.Cmd`; `commitsReloadedMsg`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/commit_scope_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func branchesPanelModel(names ...string) Model {
	m := footerModel()
	for _, n := range names {
		m.branches = append(m.branches, model.Branch{Name: n})
	}
	m.focus = panelBranches
	m.sel[panelBranches] = 0
	return m
}

func TestCommitSoloSetsAndClearsScope(t *testing.T) {
	m := branchesPanelModel("feat", "main")
	r, ok := findRow(availableActions(m), "commits-solo")
	if !ok {
		t.Fatalf("Solo this branch missing on Branches panel")
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 1 || m.commitScopeBranches[0] != "feat" {
		t.Fatalf("solo should scope to feat, got %v", m.commitScopeBranches)
	}
	// Re-solo the same branch → un-solo (back to all).
	r2, _ := findRow(availableActions(m), "commits-solo")
	mm, _ = r2.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 0 {
		t.Fatalf("re-solo should clear scope, got %v", m.commitScopeBranches)
	}
}

func TestCommitShowAllVisibilityAndClear(t *testing.T) {
	m := branchesPanelModel("feat")
	if _, ok := findRow(availableActions(m), "commits-showall"); ok {
		t.Fatalf("Show all should be absent in all-mode")
	}
	m.commitScopeBranches = []string{"feat"}
	r, ok := findRow(availableActions(m), "commits-showall")
	if !ok {
		t.Fatalf("Show all should be present when scoped")
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	if len(m.commitScopeBranches) != 0 {
		t.Fatalf("show-all should clear scope")
	}
}

func TestCommitShowAllOnCommitsPanel(t *testing.T) {
	m := footerModel()
	m.focus = panelCommits
	m.commitScopeBranches = []string{"feat"}
	if _, ok := findRow(availableActions(m), "commits-showall"); !ok {
		t.Fatalf("Show all should be offered from the Commits panel menu when scoped")
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `go test ./internal/tui/ -run 'TestCommitSolo|TestCommitShowAll' 2>&1 | tail`
Expected: FAIL (rows undefined).

- [ ] **Step 3: Add the state field + reload + header label**

In `internal/tui/model.go`, add to the Model struct (near `commits`):

```go
	commitScopeBranches []string // included branches for the feed; empty = all local branches
```

Add a `commitsReloadedMsg` and its handler. Near `commitsPagedMsg` handling (model.go ~202), add a case:

```go
	case commitsReloadedMsg:
		if msg.gen != m.feed.Gen() {
			return m, nil // superseded
		}
		m.commits = msg.state.Commits
		m.commitsExhausted = msg.state.Exhausted
		if m.sel[panelCommits] >= len(m.commits) {
			m.sel[panelCommits] = 0
		}
		return m, nil
```

Add `commitScopeLabel()`:

```go
// commitScopeLabel describes the Commits feed mode for the panel header.
func (m Model) commitScopeLabel() string {
	switch len(m.commitScopeBranches) {
	case 0:
		return "all"
	case 1:
		return "solo: " + m.commitScopeBranches[0]
	default:
		return fmt.Sprintf("%d branches", len(m.commitScopeBranches))
	}
}
```

- [ ] **Step 4: The rows + reload command**

Create `internal/tui/commit_scope.go`:

```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
)

type commitsReloadedMsg struct {
	gen   int
	state domain.FeedState
}

// reloadFeedCmd applies the model's scope to the feed and reloads page 0 off the
// UI thread. SetScope+LoadInitial bumps the feed gen (dropping stale pages) and
// cancels any superseded in-flight walk.
func (m Model) reloadFeedCmd() tea.Cmd {
	feed := m.feed
	scope := domain.LogScope{Branches: append([]string(nil), m.commitScopeBranches...)}
	return func() tea.Msg {
		feed.SetScope(scope)
		st, _ := feed.LoadInitial(context.Background())
		// st.Gen is THIS load's generation (gen0). The handler drops it when a
		// newer reload has since bumped feed.Gen() — see the gen-stamp note in Task 2.
		return commitsReloadedMsg{gen: st.Gen, state: st}
	}
}

// commitSoloRow offers "Solo this branch" on the Branches panel: scope the
// Commits feed to the selected branch, or un-solo if it is already the sole one.
func (m Model) commitSoloRow() (actionRow, bool) {
	if m.focus != panelBranches || !m.opsIdle() {
		return actionRow{}, false
	}
	b, ok := m.selectedBranch()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commits-solo",
		label: "Solo this branch",
		run: func(m Model) (tea.Model, tea.Cmd) {
			if len(m.commitScopeBranches) == 1 && m.commitScopeBranches[0] == b.Name {
				m.commitScopeBranches = nil // re-solo → un-solo
			} else {
				m.commitScopeBranches = []string{b.Name}
			}
			return m, m.reloadFeedCmd()
		},
	}, true
}

// commitShowAllRow offers "Show all branches" — present only when the feed is
// scoped — from either the Branches or the Commits panel menu.
func (m Model) commitShowAllRow() (actionRow, bool) {
	if !m.opsIdle() || len(m.commitScopeBranches) == 0 {
		return actionRow{}, false
	}
	if m.focus != panelBranches && m.focus != panelCommits {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commits-showall",
		label: "Show all branches",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitScopeBranches = nil
			return m, m.reloadFeedCmd()
		},
	}, true
}
```

- [ ] **Step 5: Inject the rows**

In `internal/tui/action_menu.go`, in the non-content-window branch (after `renameBranchRow`/`rewordRow` injections), add:

```go
	if r, ok := m.commitSoloRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.commitShowAllRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 6: Header label + Branches solo marker**

In `internal/tui/view.go`, change both `m.panelLabel(panelCommits, "Commits")` calls to include the scope, e.g. `m.panelLabel(panelCommits, "Commits ("+m.commitScopeLabel()+")")`.

For the Branches solo marker: in the branch row builder (`branchRows()`), prefix the soloed branch's row with a marker (e.g. `"◉ "`) when `len(m.commitScopeBranches)==1 && name==m.commitScopeBranches[0]`. (Note the single-sourced-row gotcha: the marker becomes part of the filter haystack — acceptable.) Add a focused test only if `branchRows` is straightforward; otherwise keep the marker minimal.

- [ ] **Step 7: Run tests**

Run: `go build ./... && go test ./internal/tui/ -run 'TestCommit' 2>&1 | tail -8 && go test ./internal/tui/ 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/commit_scope.go internal/tui/commit_scope_test.go internal/tui/model.go internal/tui/action_menu.go internal/tui/view.go
git commit -m "feat(tui): commit-feed scope state + solo/show-all menu actions

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 4: TUI — branch labels in commit rows

**Files:**
- Modify: `internal/tui/view.go` (`commitRows`)
- Test: `internal/tui/commit_scope_test.go` (add)

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/commit_scope_test.go`:

```go
import "strings"

func TestCommitRowsRenderLabels(t *testing.T) {
	m := footerModel()
	m.commits = []model.Commit{
		{Hash: "a1b2c3d4", Subject: "do a thing", Refs: []model.Ref{
			{Name: "main", Kind: model.RefLocal, Head: true},
			{Name: "feature", Kind: model.RefLocal},
			{Name: "origin/main", Kind: model.RefRemote},
		}},
		{Hash: "ffff0000", Subject: "plain"},
	}
	rows := m.commitRows()
	if !strings.Contains(rows[0], "a1b2c3d") || !strings.Contains(rows[0], "*main") || !strings.Contains(rows[0], "feature") {
		t.Fatalf("row0 should show local branch labels with *head: %q", rows[0])
	}
	if strings.Contains(rows[0], "origin/main") {
		t.Fatalf("remote labels not rendered in Phase 1: %q", rows[0])
	}
	if strings.Contains(rows[1], "(") {
		t.Fatalf("undecorated row should have no labels: %q", rows[1])
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `go test ./internal/tui/ -run TestCommitRowsRenderLabels 2>&1 | tail`
Expected: FAIL (labels not rendered).

- [ ] **Step 3: Render the labels**

Replace `commitRows` in `internal/tui/view.go`:

```go
func (m Model) commitRows() []string {
	out := make([]string, 0, len(m.commits))
	for _, c := range m.commits {
		h := c.Hash
		if len(h) > 7 {
			h = h[:7]
		}
		row := h + " " + commitRefLabels(c.Refs) + c.Subject
		out = append(out, row)
	}
	return out
}

// commitRefLabels renders local-branch pills as a "‹*head›‹branch› " prefix,
// the current branch (Ref.Head) prefixed with "*". Remote/tag kinds are captured
// but not rendered in Phase 1. Empty when there are no local labels.
func commitRefLabels(refs []model.Ref) string {
	var b strings.Builder
	for _, r := range refs {
		if r.Kind != model.RefLocal {
			continue
		}
		if r.Head {
			b.WriteString("‹*" + r.Name + "› ")
		} else {
			b.WriteString("‹" + r.Name + "› ")
		}
	}
	return b.String()
}
```

The current branch is `Ref.Head` (set by `parseDecorations` from `HEAD -> <branch>` in Task 1) — a clean explicit signal, no positional heuristic.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui/ 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/view.go internal/tui/commit_scope_test.go
git commit -m "feat(tui): branch labels on commit rows (HEAD emphasized)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 5: help, docs, race gate

**Files:**
- Modify: `internal/tui/help.go`, `CHANGELOG.md`, `README.md`, `CLAUDE.md`

- [ ] **Step 1: help.go**

Add rows describing the Commits panel now showing all local branches in date order with branch labels, and the `.`-menu **Solo this branch** (Branches) / **Show all branches** (Branches or Commits). (Menu-only actions need a help row, not a footer key — `TestHelpFooterCoverage` keys off footer bindings, so no footer entry is required.)

- [ ] **Step 2: CHANGELOG / README / CLAUDE**

- CHANGELOG `### Changed`: the Commits panel now shows **all local branches** by default in date order with branch/HEAD labels; **Solo this branch** / **Show all branches** via the `.` menu; paged + supersede-cancellable.
- README: update the Commits-panel description (all-branches default, labels, solo/show-all).
- CLAUDE.md: `domain` row / Commits note — `CommitFeed` now carries a `LogScope` (all local branches by default, or named branches), supersede-cancels superseded loads, `model.Commit.Refs` carries `%D` decorations.

- [ ] **Step 3: Full race gate**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit tests pass, e2e green.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "docs: multi-branch commit feed (Phase 1); help

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

## Self-Review

**Spec coverage:**
- Default all local branches, date-order → Task 1 (`LogScoped` empty scope → `--branches --date-order`). ✓
- `%D` decorations → `model.Ref`/`Commit.Refs` → Task 1. ✓
- Feed scope + singleflight key separation → Task 2. ✓
- Supersede-cancellation → Task 2 Step 4 (+ test). ✓
- Batching/throttling preserved (paging + LimitRunner + inFlight) → unchanged by design. ✓
- Cache-ready, cache deferred → `scopeKey` in the singleflight key; no `cache.Factory` page cache added. ✓
- Solo / Show-all `.`-menu actions (Branches + Commits), conditional visibility, re-solo un-solos → Task 3 (+ tests). ✓
- Header mode label + Branches solo marker → Task 3 Step 6. ✓
- Branch labels on commit rows, HEAD emphasized, filterable (single-sourced row) → Task 4. ✓
- help/docs → Task 5. ✓

**Placeholder scan:** none. The HEAD-branch signal is explicit (`Ref.Head` from `parseDecorations`), so the renderer needs no heuristic.

**Type consistency:** `model.Ref{Name,Kind}`/`RefKind`/`Commit.Refs`; `git.LogScope{Branches}`/`LogScoped`; `domain.LogScope` alias/`logPage(…,scope)`/`scopeKey`/`SetScope`; `commitScopeBranches`/`commitScopeLabel`/`reloadFeedCmd`/`commitsReloadedMsg`/`commitSoloRow`(`commits-solo`)/`commitShowAllRow`(`commits-showall`) — consistent across tasks.
