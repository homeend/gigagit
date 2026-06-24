# Commit-Loading Improvements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Commits feed load deeper, on demand, and survive filtering — configurable page sizes, manual load keys (ctrl+l / Home / End), a filter-clear cache, and eager `/` search across unloaded history.

**Architecture:** Builds entirely on the existing `CommitFeed` paged read-model (`internal/domain/commitfeed.go`) and its gen/cancel/single-flight machinery. `domain` stays config-free: the TUI reads `m.cfg.UI.*` and injects page sizes via a new `SetPageSizes`. A scope-keyed accumulation cache in the feed makes `ApplyScope` (scope toggles) restore instantly, while `LoadInitial` (hard refresh) clears the whole cache. Eager search is an iterative Bubble Tea command chain over `LoadMore`.

**Tech Stack:** Go 1.26, Bubble Tea / lipgloss TUI, `pelletier/go-toml/v2` config, real `git` + `gitexec.FakeRunner` for tests.

## Global Constraints

- New settings live under `[ui]` (`UIConfig`), TOML snake_case, `<= 0` = unset → fallback. Defaults: `commit_initial_count = 300`, `commit_batch_size = 300`, `commit_search_max_pages = 5`.
- `internal/domain` MUST NOT import `internal/config` (archtest-guarded). Page sizes arrive as plain ints via `SetPageSizes`.
- `internal/tui` MUST NOT import `internal/git` (archtest-guarded); reach git through `domain`.
- Keys: `ctrl+l` = load next batch (Commits), `Home` = top of list, `End` = bottom + load (Commits), `ctrl+f` = eager `/` search (Commits). All must appear in BOTH `help.go` and the footer registry (the `help_test.go` drift guard enforces parity).
- Every `[ui]` field needs a `settingDoc` in `internal/config/template.go` (`TestSettingDocsCoverAllFields` fails otherwise).
- TDD: failing test first. Run `./test.sh unit` (or the named package) before each commit. Commit messages end with the repo's Co-Authored-By / Claude-Session trailer.
- Tests use a real `git` in `t.TempDir()` (helpers `newRepo`/`newTestRepo` in domain; `branchesPanelModel`/`newTestModelForReload` in tui) or `gitexec.NewFakeRunner` for argv assertions (`f.Calls[i].Argv`).

---

## File Structure

- `internal/config/config.go` — three new `UIConfig` fields + `Defaults` + `overlayUI`.
- `internal/config/template.go` — three `settingDoc` rows.
- `internal/domain/commitfeed.go` — page-size fields + `SetPageSizes`/`effInitial`/`effPage`, `CanLoadMore`, the scope cache + `ApplyScope`, `LoadInitial` refactor (`loadInitialWalk` + cache clear).
- `internal/domain/query.go` — `scopeKey` folds filter axes.
- `internal/tui/load.go` — `loadCmd` reorders to load config + `SetPageSizes` before the first walk.
- `internal/tui/model.go` — `ctrl+l` / `Home` / `End` / `ctrl+f` key cases; `commitsPagedMsg` re-enters eager search; `eager` model field.
- `internal/tui/commit_scope.go` — `reloadFeedCmd` uses `ApplyScope`.
- `internal/tui/commit_eager.go` (new) — eager-search engine + the cap-prompt popup.
- `internal/tui/help.go`, `internal/tui/footer.go` — advertise the new keys.
- `CHANGELOG.md`, `README.md` — user-facing docs (final task).

Tasks A1→A3 are the config/size foundation. B, C, D each depend only on A. Within C, C1 (scopeKey) precedes C2 (cache). Within D, D1 (engine) precedes D2 (keys) and D3 (dialog).

---

## Task A1: `[ui]` config fields for page sizes

**Files:**
- Modify: `internal/config/config.go` (UIConfig struct ~22-37, Defaults ~46-54, overlayUI ~111-145)
- Modify: `internal/config/template.go` (settingDocs ~24-40)
- Test: `internal/config/config_test.go`, `internal/config/template_test.go` (existing `TestSettingDocsCoverAllFields`)

**Interfaces:**
- Produces: `config.UIConfig.CommitInitialCount int`, `.CommitBatchSize int`, `.CommitSearchMaxPages int` (TOML `commit_initial_count` / `commit_batch_size` / `commit_search_max_pages`). Defaults 300 / 300 / 5.

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestCommitPageSizeDefaultsAndOverlay(t *testing.T) {
	// Defaults.
	d := Defaults().UI
	if d.CommitInitialCount != 300 || d.CommitBatchSize != 300 || d.CommitSearchMaxPages != 5 {
		t.Fatalf("defaults = %d/%d/%d, want 300/300/5",
			d.CommitInitialCount, d.CommitBatchSize, d.CommitSearchMaxPages)
	}
	// Repo file overrides; a 0 in a higher layer does NOT reset a lower layer.
	dir := t.TempDir()
	repo := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(repo, []byte("[ui]\ncommit_initial_count = 25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filepath.Join(dir, "no-global.toml"), repo)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.CommitInitialCount != 25 {
		t.Fatalf("initial = %d, want 25 (repo override)", cfg.UI.CommitInitialCount)
	}
	if cfg.UI.CommitBatchSize != 300 {
		t.Fatalf("batch = %d, want 300 (default kept)", cfg.UI.CommitBatchSize)
	}
}
```

Confirm `config_test.go` already imports `os`, `path/filepath`, `testing` (add any missing).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestCommitPageSizeDefaultsAndOverlay -v`
Expected: FAIL — `CommitInitialCount` undefined (compile error).

- [ ] **Step 3: Add the struct fields**

In `internal/config/config.go`, append to `UIConfig` (after the `CommitGraphMaxLanes` line):

```go
	CommitInitialCount   int `toml:"commit_initial_count"`    // commits walked on first paint; <=0 = unset (default 300)
	CommitBatchSize      int `toml:"commit_batch_size"`       // commits per later page (scroll / ctrl+l); <=0 = unset (default 300)
	CommitSearchMaxPages int `toml:"commit_search_max_pages"` // eager /-search page cap before re-prompting; <=0 = unset (default 5)
```

- [ ] **Step 4: Set the defaults**

In `Defaults()`, extend the `UI: UIConfig{...}` literal:

```go
		UI: UIConfig{WheelStep: 3, HScrollStep: 8, CommitGraphLanes: 8, CommitGraphMinLanes: 2, CommitGraphStep: 4,
			CommitInitialCount: 300, CommitBatchSize: 300, CommitSearchMaxPages: 5},
```

- [ ] **Step 5: Add the overlay guards**

In `overlayUI`, append:

```go
	if src.CommitInitialCount > 0 {
		dst.CommitInitialCount = src.CommitInitialCount
	}
	if src.CommitBatchSize > 0 {
		dst.CommitBatchSize = src.CommitBatchSize
	}
	if src.CommitSearchMaxPages > 0 {
		dst.CommitSearchMaxPages = src.CommitSearchMaxPages
	}
```

- [ ] **Step 6: Add the settingDocs**

In `internal/config/template.go`, append to `settingDocs` (after the `commit_graph_max_lanes` row):

```go
	{"ui", "commit_initial_count", 300, "commits loaded on first paint (raise to find more without scrolling)"},
	{"ui", "commit_batch_size", 300, "commits loaded per later page (scroll to the end, or ctrl+l)"},
	{"ui", "commit_search_max_pages", 5, "pages eager /-search scans before asking to search deeper"},
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS — including `TestSettingDocsCoverAllFields` and `TestCommitPageSizeDefaultsAndOverlay`.

- [ ] **Step 8: Commit**

```bash
git add internal/config/
git commit -m "feat(config): [ui] commit_initial_count / batch_size / search_max_pages"
```

---

## Task A2: `CommitFeed` honors injected page sizes

**Files:**
- Modify: `internal/domain/commitfeed.go` (const block ~13-17, struct ~22-35, `LoadInitial` ~103-146, `LoadMore` ~152-185)
- Test: `internal/domain/commitfeed_test.go`

**Interfaces:**
- Produces: `func (f *CommitFeed) SetPageSizes(initial, batch int)` — `<= 0` keeps the built-in fallback (`commitInitialPage` 50 / `commitPageSize` 200). After it, the next `LoadInitial` walks `-n initial`, later `LoadMore` walk `-n batch`.

- [ ] **Step 1: Write the failing test**

Add to `internal/domain/commitfeed_test.go` (mirror the FakeRunner argv style of `internal/git/log_test.go`):

```go
func TestCommitFeedSetPageSizesArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	feed := New(&git.Repo{Runner: f}).CommitFeed()
	feed.SetPageSizes(7, 9)

	if _, err := feed.LoadInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !hasLogArg(f, "-n", "7") {
		t.Fatalf("initial walk should pass -n 7, calls: %v", f.Calls)
	}
	f.Calls = nil
	// Exhausted=false only if the first page was full; force a non-exhausted feed
	// by serving a full 7-row page, then LoadMore must use -n 9.
}

// hasLogArg reports whether a "git log" call passed the flag/value pair adjacently
// (e.g. -n 7) OR the combined "-n=7" form, scanning all recorded calls.
func hasLogArg(f *gitexec.FakeRunner, flag, val string) bool {
	for _, c := range f.Calls {
		if c.Name != "git log" {
			continue
		}
		for i, a := range c.Argv {
			if a == flag && i+1 < len(c.Argv) && c.Argv[i+1] == val {
				return true
			}
			if a == flag+"="+val {
				return true
			}
		}
	}
	return false
}
```

(Check whether the pager passes `-n` as `-n 7` or `-n=7`; `internal/git/log.go` uses `.Arg("-n", strconv.Itoa(limit))` → two args `-n` then `7`. The first `hasLogArg` branch matches. Keep both branches for safety.)

Add a fallback test:

```go
func TestCommitFeedDefaultPageSizeWhenUnset(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	feed := New(&git.Repo{Runner: f}).CommitFeed() // no SetPageSizes
	if _, err := feed.LoadInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !hasLogArg(f, "-n", "50") {
		t.Fatalf("unset → fallback -n 50, calls: %v", f.Calls)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain/ -run 'TestCommitFeedSetPageSizesArgv|TestCommitFeedDefaultPageSizeWhenUnset' -v`
Expected: FAIL — `SetPageSizes` undefined.

- [ ] **Step 3: Add fields + setter + effective getters**

In `internal/domain/commitfeed.go`, add to the `CommitFeed` struct (after `pager`):

```go
	initialPage int // configured first-paint size; <=0 → commitInitialPage
	pageSize    int // configured later-page size; <=0 → commitPageSize
```

Add the methods (near `SetScope`):

```go
// SetPageSizes sets the first-paint and later-page commit counts (0 or negative
// keeps the built-in fallback). Apply before the next LoadInitial.
func (f *CommitFeed) SetPageSizes(initial, batch int) {
	f.mu.Lock()
	f.initialPage = initial
	f.pageSize = batch
	f.mu.Unlock()
}

// effInitial / effPage resolve the configured size or the constant fallback.
// Callers hold f.mu.
func (f *CommitFeed) effInitial() int {
	if f.initialPage > 0 {
		return f.initialPage
	}
	return commitInitialPage
}

func (f *CommitFeed) effPage() int {
	if f.pageSize > 0 {
		return f.pageSize
	}
	return commitPageSize
}
```

- [ ] **Step 4: Use the effective sizes in LoadInitial**

In `LoadInitial`, capture the size under the lock and use it for the walk and the exhausted check. Change the locked prelude to add `initial := f.effInitial()` (alongside `scope := f.scope`), then:
- the walk call from `f.pager.Page(cctx, commitInitialPage, 0, gen0, scope)` → `f.pager.Page(cctx, initial, 0, gen0, scope)`
- the exhausted line from `f.exhausted = len(page) < commitInitialPage` → `f.exhausted = len(page) < initial`

- [ ] **Step 5: Use the effective size in LoadMore**

In `LoadMore`, in the locked prelude add `size := f.effPage()` (alongside `skip := f.skip`), then:
- `f.pager.Page(ctx, commitPageSize, skip, gen0, scope)` → `f.pager.Page(ctx, size, skip, gen0, scope)`
- `f.exhausted = len(page) < commitPageSize` → `f.exhausted = len(page) < size`

- [ ] **Step 6: Run to verify pass**

Run: `go test ./internal/domain/ -run 'TestCommitFeed' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/commitfeed.go internal/domain/commitfeed_test.go
git commit -m "feat(domain): CommitFeed.SetPageSizes overrides the page-size constants"
```

---

## Task A3: TUI injects configured sizes before the first walk

**Files:**
- Modify: `internal/tui/load.go` (`loadCmd` ~58-114)
- Test: `internal/tui/load_test.go` (new file)

**Interfaces:**
- Consumes: `config.UIConfig.CommitInitialCount` / `.CommitBatchSize` (Task A1), `feed.SetPageSizes` (Task A2), `svc.TopLevel(ctx) (string, error)` (`internal/domain/query.go:300`).

- [ ] **Step 1: Write the failing test**

Create `internal/tui/load_test.go`. This is a real-git test: a repo with 4 commits and a `.gg.toml` capping the initial page at 2; running the `loadCmd` closure must produce a feed walked at 2.

```go
package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/domain"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestLoadCmdHonorsConfiguredInitialCount(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	for i := 0; i < 4; i++ {
		f := filepath.Join(dir, "f")
		os.WriteFile(f, []byte{byte('a' + i)}, 0o644)
		gitRun(t, dir, "add", ".")
		gitRun(t, dir, "commit", "-q", "-m", "c")
	}
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"),
		[]byte("[ui]\ncommit_initial_count = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := domain.OpenTUI(dir)
	m := Model{svc: svc, feed: svc.CommitFeed(), sel: map[panel]int{}}
	msg := m.loadCmd()() // run the load closure synchronously
	dl, ok := msg.(dataLoadedMsg)
	if !ok {
		t.Fatalf("want dataLoadedMsg, got %T", msg)
	}
	if dl.err != nil {
		t.Fatal(dl.err)
	}
	if len(dl.commits) != 2 {
		t.Fatalf("first page = %d commits, want 2 (commit_initial_count)", len(dl.commits))
	}
	if dl.cfg.UI.CommitInitialCount != 2 {
		t.Fatalf("cfg not threaded: initial = %d", dl.cfg.UI.CommitInitialCount)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestLoadCmdHonorsConfiguredInitialCount -v`
Expected: FAIL — `len(dl.commits)` = 50 (or 4), config loaded after the walk.

- [ ] **Step 3: Reorder `loadCmd` to load config + set sizes before the walk**

Replace the body of `loadCmd`'s returned closure in `internal/tui/load.go` with:

```go
	return func() tea.Msg {
		ctx := context.Background()
		// Resolve config BEFORE the feed's first walk so commit_initial_count
		// governs the first paint. config.Load needs the repo toplevel; fetch it
		// up front (cheap; the gated Snapshot reads its own toplevel too).
		cfg := config.Defaults()
		top, topErr := svc.TopLevel(ctx)
		if topErr == nil && top != "" {
			if c, cfgErr := config.Load(config.DefaultGlobalPath(), filepath.Join(top, ".gg.toml")); cfgErr == nil {
				cfg = c
			}
		}
		feed.SetPageSizes(cfg.UI.CommitInitialCount, cfg.UI.CommitBatchSize)

		var (
			snap    domain.Snapshot
			snapErr error
			fs      domain.FeedState
			feedErr error
			wg      sync.WaitGroup
		)
		wg.Add(2)
		go func() { defer wg.Done(); snap, snapErr = svc.Snapshot(ctx) }()
		go func() { defer wg.Done(); fs, feedErr = feed.LoadInitial(ctx) }()
		wg.Wait()
		if snapErr != nil {
			return dataLoadedMsg{gen: gen, err: snapErr}
		}
		out := dataLoadedMsg{
			gen:              gen,
			status:           snap.Status,
			branches:         snap.Branches,
			remoteBranches:   snap.RemoteBranches,
			worktrees:        snap.Worktrees,
			tags:             snap.Tags,
			reflog:           snap.Reflog,
			currentWorktree:  snap.CurrentWorktree,
			gitCommonDir:     snap.GitCommonDir,
			headTimes:        snap.HeadTimes,
			conflict:         snap.Conflict,
			commits:          fs.Commits,
			commitsExhausted: fs.Exhausted,
			commitErr:        feedErr,
			cfg:              cfg,
		}
		// MRU touch + reflog re-read are not git-status reads; do them after the
		// gated snapshot, keyed off the toplevel it reported.
		if snap.CurrentWorktree != "" {
			_ = repos.Touch(statePath, snap.CurrentWorktree, time.Now())
			if n := cfg.UI.ReflogLimit; n > 0 {
				if rl, err := svc.Reflog(ctx, n); err == nil {
					out.reflog = rl
				}
			}
		}
		return out
	}
```

Imports are unchanged (`config`, `filepath`, `sync`, `time`, `repos`, `domain` already imported).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/tui/ -run TestLoadCmdHonorsConfiguredInitialCount -v`
Expected: PASS.

- [ ] **Step 5: Full TUI package still green**

Run: `go test ./internal/tui/`
Expected: ok.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/load.go internal/tui/load_test.go
git commit -m "feat(tui): load config before the first commit walk so initial_count applies"
```

---

## Task B1: `ctrl+l` loads the next batch

**Files:**
- Modify: `internal/domain/commitfeed.go` (add `CanLoadMore`)
- Modify: `internal/tui/model.go` (main key switch, near the `pgdown` case ~1000)
- Test: `internal/domain/commitfeed_test.go`, `internal/tui/model_test.go`

**Interfaces:**
- Produces: `func (f *CommitFeed) CanLoadMore() bool` — `!exhausted && !inFlight`. Consumed by the `ctrl+l` handler.

- [ ] **Step 1: Write the failing domain test**

Add to `internal/domain/commitfeed_test.go`:

```go
func TestCanLoadMore(t *testing.T) {
	f := gitexec.NewFakeRunner()
	feed := New(&git.Repo{Runner: f}).CommitFeed()
	feed.SetPageSizes(2, 2)
	// A full first page (2 rows) leaves more to load.
	f.SetResponse("git log", gitexec.Result{Stdout: logRows(2)})
	if _, err := feed.LoadInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !feed.CanLoadMore() {
		t.Fatal("full page → CanLoadMore true")
	}
	// A short next page (1 < 2) exhausts the feed.
	f.SetResponse("git log", gitexec.Result{Stdout: logRows(1)})
	if _, _, err := feed.LoadMore(context.Background()); err != nil {
		t.Fatal(err)
	}
	if feed.CanLoadMore() {
		t.Fatal("short page → exhausted → CanLoadMore false")
	}
}

// logRows builds n valid newest-first log lines for the default format
// (%H%x1f%P%x1f%an%x1f%at%x1f%s%x1f%D%x1f%S). Hashes are unique so dedupe keeps all.
func logRows(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		h := fmt.Sprintf("h%04d", i)
		b.WriteString(h + "\x1f\x1fAda\x1f0\x1fsubj\x1f\x1f\n")
	}
	return b.String()
}
```

Ensure the test file imports `fmt` and `strings`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain/ -run TestCanLoadMore -v`
Expected: FAIL — `CanLoadMore` undefined.

- [ ] **Step 3: Add `CanLoadMore`**

In `internal/domain/commitfeed.go` (near `NeedsMore`):

```go
// CanLoadMore reports whether a LoadMore would do work (not exhausted, not
// already in flight) — independent of cursor position. Drives the ctrl+l key.
func (f *CommitFeed) CanLoadMore() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.exhausted && !f.inFlight
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/domain/ -run TestCanLoadMore -v`
Expected: PASS.

- [ ] **Step 5: Write the failing TUI test**

Add to `internal/tui/model_test.go`:

```go
func TestCtrlLForcesLoadOnCommits(t *testing.T) {
	m := newTestModelForReload(t) // real svc+feed on a FakeRunner (see commit_scope_test.go)
	m.focus = panelCommits
	// Fresh feed: exhausted=false, inFlight=false → CanLoadMore true.
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if cmd == nil {
		t.Fatal("ctrl+l on Commits should dispatch a load")
	}
	if !nm.(Model).commitsLoading {
		t.Fatal("ctrl+l should set commitsLoading")
	}
}

func TestCtrlLNoopWhenExhausted(t *testing.T) {
	m := newTestModelForReload(t)
	m.focus = panelCommits
	// Exhaust the feed: a short initial page.
	m.feed.SetPageSizes(50, 50)
	if _, err := m.feed.LoadInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	// newTestModelForReload's fake serves a single row → 1 < 50 → exhausted.
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if cmd != nil || nm.(Model).commitsLoading {
		t.Fatal("ctrl+l must be a no-op on an exhausted feed")
	}
}
```

Confirm `context` and `tea` are imported in `model_test.go`.

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/tui/ -run 'TestCtrlL' -v`
Expected: FAIL — no `ctrl+l` handling (cmd nil / commitsLoading false in the first test).

- [ ] **Step 7: Add the `ctrl+l` case**

In `internal/tui/model.go`, in the main `tea.KeyMsg` switch (alongside the `pgdown`/`pgup` cases, ~1000), add:

```go
		case "ctrl+l":
			// Load the next batch regardless of cursor position (the auto-page path
			// only fires near the end). Commits panel only; the feed guards exhausted/
			// in-flight via CanLoadMore.
			if m.focus == panelCommits && m.feed != nil && m.feed.CanLoadMore() {
				m.commitsLoading = true
				return m, m.loadMoreCmd()
			}
```

- [ ] **Step 8: Run to verify pass**

Run: `go test ./internal/tui/ -run 'TestCtrlL' -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/domain/commitfeed.go internal/domain/commitfeed_test.go internal/tui/model.go internal/tui/model_test.go
git commit -m "feat(tui): ctrl+l loads the next commit batch on demand"
```

---

## Task B2: Home / End navigation (End loads deeper)

**Files:**
- Modify: `internal/tui/model.go` (main key switch, near the `pgdown` case ~1000)
- Test: `internal/tui/model_test.go`

**Interfaces:**
- Consumes: `m.panelLen(panel)`, `m.maybeLoadMoreCommits()` (existing).

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/model_test.go`:

```go
func TestHomeEndCommitsNav(t *testing.T) {
	m := newTestModelForReload(t)
	m.focus = panelCommits
	// Give the panel several rows so home/end have somewhere to go.
	m.commits = []model.Commit{{Hash: "a"}, {Hash: "b"}, {Hash: "c"}}
	m = m.rebuildCommitGraph()
	m.sel[panelCommits] = 1

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if nm.(Model).sel[panelCommits] != 0 {
		t.Fatalf("home → sel 0, got %d", nm.(Model).sel[panelCommits])
	}

	nm2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	want := m.panelLen(panelCommits) - 1
	if nm2.(Model).sel[panelCommits] != want {
		t.Fatalf("end → sel %d, got %d", want, nm2.(Model).sel[panelCommits])
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestHomeEndCommitsNav -v`
Expected: FAIL — Home/End unhandled, `sel` unchanged.

- [ ] **Step 3: Add the `home` / `end` cases**

In the same switch as Task B1, add:

```go
		case "home":
			m.sel[m.focus] = 0
		case "end":
			if n := m.panelLen(m.focus); n > 0 {
				m.sel[m.focus] = n - 1
			}
			// On Commits, landing at the true end triggers the existing auto-page
			// path (NeedsMore is satisfied), so End also loads a new batch; press
			// again to walk deeper. maybeLoadMoreCommits no-ops under a commit filter.
			if m.focus == panelCommits {
				if mm, cmd := m.maybeLoadMoreCommits(); cmd != nil {
					return mm, cmd
				}
			}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/tui/ -run TestHomeEndCommitsNav -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat(tui): Home/End jump to top/bottom of a panel; End loads deeper on Commits"
```

---

## Task C1: `scopeKey` folds the filter axes

**Files:**
- Modify: `internal/domain/query.go` (`scopeKey` ~189-196)
- Test: `internal/domain/scopekey_test.go` (existing) or `query_test.go`

**Interfaces:**
- Produces: `scopeKey(LogScope)` now distinguishes scopes that differ only by `Paths`/`Author`/`Grep`/`Since`/`Until`. (`LogScope = git.LogScope`; `filtered()` is unexported in `git`, so inline the check.)

- [ ] **Step 1: Write the failing test**

Add to `internal/domain/scopekey_test.go` (or `query_test.go`):

```go
func TestScopeKeyFoldsFilterAxes(t *testing.T) {
	base := scopeKey(LogScope{Branches: []string{"main"}})
	grep := scopeKey(LogScope{Branches: []string{"main"}, Grep: "fix"})
	path := scopeKey(LogScope{Branches: []string{"main"}, Paths: []string{"a"}})
	if base == grep {
		t.Fatal("a message filter must change the scope key")
	}
	if base == path || grep == path {
		t.Fatal("a path filter must change the scope key")
	}
	if scopeKey(LogScope{Grep: "fix"}) != scopeKey(LogScope{Grep: "fix"}) {
		t.Fatal("scopeKey must be stable for equal scopes")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain/ -run TestScopeKeyFoldsFilterAxes -v`
Expected: FAIL — `base == grep` (filters not folded).

- [ ] **Step 3: Fold the filter axes**

Replace `scopeKey` in `internal/domain/query.go` with:

```go
// scopeKey is the stable cache/singleflight discriminator for a scope. It folds
// ref selection (branches + upstreams) AND the content filters, so two scopes
// that differ only by filter never collide.
func scopeKey(scope LogScope) string {
	base := "all"
	if len(scope.Branches) > 0 {
		base = strings.Join(scope.Branches, ",")
	}
	if len(scope.Upstreams) > 0 {
		base += "|up:" + strings.Join(scope.Upstreams, ",")
	}
	if len(scope.Paths) > 0 || scope.Author != "" || scope.Grep != "" || scope.Since != "" || scope.Until != "" {
		base += "|f:" + strings.Join(scope.Paths, ",") +
			"|a:" + scope.Author + "|g:" + scope.Grep +
			"|s:" + scope.Since + "|u:" + scope.Until
	}
	return base
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/domain/ -run 'TestScopeKey' -v`
Expected: PASS. Also run `go test ./internal/domain/` to confirm no existing scopeKey test regressed.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/query.go internal/domain/scopekey_test.go
git commit -m "fix(domain): scopeKey folds path/author/grep/date filters"
```

---

## Task C2: Scope accumulation cache + `ApplyScope`

**Files:**
- Modify: `internal/domain/commitfeed.go` (struct, `CommitFeed()` ctor ~38-48, `LoadInitial` refactor, add cache + `ApplyScope`)
- Test: `internal/domain/commitfeed_test.go`

**Interfaces:**
- Consumes: `scopeKey` (Task C1), `effInitial` (Task A2).
- Produces: `func (f *CommitFeed) ApplyScope(ctx context.Context, scope LogScope) (FeedState, error)` — restores a cached accumulation for `scope` (no git call) or walks page 0; stashes the current scope first. `LoadInitial` now clears the entire cache (hard refresh).

- [ ] **Step 1: Write the failing tests**

Add to `internal/domain/commitfeed_test.go`:

```go
func TestApplyScopeRestoresWithoutRewalk(t *testing.T) {
	f := gitexec.NewFakeRunner()
	feed := New(&git.Repo{Runner: f}).CommitFeed()
	feed.SetPageSizes(50, 50)

	f.SetResponse("git log", gitexec.Result{Stdout: logRows(3)}) // base: 3 commits
	if _, err := feed.LoadInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.SetResponse("git log", gitexec.Result{Stdout: logRows(1)}) // filtered: 1 commit
	if _, err := feed.ApplyScope(context.Background(), LogScope{Grep: "x"}); err != nil {
		t.Fatal(err)
	}
	calls := len(f.Calls)
	// Clear the filter back to base → must restore from cache, no new git log.
	st, err := feed.ApplyScope(context.Background(), LogScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Calls) != calls {
		t.Fatalf("restore re-walked: calls %d → %d", calls, len(f.Calls))
	}
	if len(st.Commits) != 3 {
		t.Fatalf("restored base = %d commits, want 3", len(st.Commits))
	}
}

func TestLoadInitialInvalidatesCacheAcrossScopes(t *testing.T) {
	f := gitexec.NewFakeRunner()
	feed := New(&git.Repo{Runner: f}).CommitFeed()
	feed.SetPageSizes(50, 50)

	f.SetResponse("git log", gitexec.Result{Stdout: logRows(3)}) // base: 3
	feed.LoadInitial(context.Background())
	f.SetResponse("git log", gitexec.Result{Stdout: logRows(1)}) // filtered: 1
	feed.ApplyScope(context.Background(), LogScope{Grep: "x"})

	// A write happens → post-op hard refresh re-walks the current (filtered) scope
	// and must clear EVERY cached scope, base included.
	f.SetResponse("git log", gitexec.Result{Stdout: logRows(4)}) // history grew to 4
	feed.LoadInitial(context.Background())

	// Clearing the filter must now re-walk the base (cache invalidated) → 4 commits.
	st, err := feed.ApplyScope(context.Background(), LogScope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Commits) != 4 {
		t.Fatalf("base after refresh = %d, want 4 (re-walked, not stale 3)", len(st.Commits))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain/ -run 'TestApplyScope|TestLoadInitialInvalidates' -v`
Expected: FAIL — `ApplyScope` undefined.

- [ ] **Step 3: Add the cache types + fields**

In `internal/domain/commitfeed.go`, add near the top (after the const block):

```go
// cachedScope is a feed accumulation remembered for a scope key, so toggling back
// to that scope (e.g. clearing a filter) restores instantly with no re-walk.
type cachedScope struct {
	commits   []model.Commit
	hashes    map[string]bool
	skip      int
	exhausted bool
}

// commitScopeCacheCap bounds the cache by ENTRY COUNT (remembered scopes), not
// bytes — one large base accumulation dominates memory.
const commitScopeCacheCap = 4
```

Add to the `CommitFeed` struct:

```go
	cache      map[string]cachedScope // scopeKey → remembered accumulation
	cacheOrder []string               // LRU order, oldest first
```

In `CommitFeed()` (the constructor), initialize the map:

```go
	return &CommitFeed{svc: s, hashes: map[string]bool{}, cache: map[string]cachedScope{}, pager: pager}
```

- [ ] **Step 4: Add the cache helpers**

```go
// stashCurrentLocked saves the current scope's accumulation under its key.
// Caller holds f.mu. A no-op when nothing is loaded.
func (f *CommitFeed) stashCurrentLocked() {
	if len(f.commits) == 0 {
		return
	}
	key := scopeKey(f.scope)
	if _, ok := f.cache[key]; !ok {
		f.cacheOrder = append(f.cacheOrder, key)
	}
	hs := make(map[string]bool, len(f.hashes))
	for h := range f.hashes {
		hs[h] = true
	}
	cp := make([]model.Commit, len(f.commits))
	copy(cp, f.commits)
	f.cache[key] = cachedScope{commits: cp, hashes: hs, skip: f.skip, exhausted: f.exhausted}
	for len(f.cacheOrder) > commitScopeCacheCap {
		delete(f.cache, f.cacheOrder[0])
		f.cacheOrder = f.cacheOrder[1:]
	}
}

// clearCacheLocked drops every remembered scope (hard-refresh invalidation).
// Caller holds f.mu.
func (f *CommitFeed) clearCacheLocked() {
	f.cache = map[string]cachedScope{}
	f.cacheOrder = nil
}
```

- [ ] **Step 5: Refactor `LoadInitial` into a cache-clearing wrapper + shared walk**

Replace the existing `LoadInitial` with:

```go
// LoadInitial is the HARD REFRESH: it re-walks the current scope from page 0 and
// invalidates the entire scope cache, so post-operation reloads never restore a
// stale accumulation for some other scope. (Scope TOGGLES use ApplyScope.)
func (f *CommitFeed) LoadInitial(ctx context.Context) (FeedState, error) {
	f.mu.Lock()
	f.clearCacheLocked()
	f.mu.Unlock()
	return f.loadInitialWalk(ctx)
}

// loadInitialWalk resets the current scope's accumulation and walks page 0. It is
// the body shared by LoadInitial and ApplyScope's cache-miss path; it does NOT
// touch the cache.
func (f *CommitFeed) loadInitialWalk(ctx context.Context) (FeedState, error) {
	f.mu.Lock()
	if f.cancel != nil {
		f.cancel()
	}
	cctx, cancel := context.WithCancel(ctx)
	f.cancel = cancel
	f.gen++
	gen0 := f.gen
	scope := f.scope
	initial := f.effInitial()
	f.commits = nil
	f.hashes = map[string]bool{}
	f.skip = 0
	f.exhausted = false
	f.inFlight = true
	f.mu.Unlock()

	page, err := f.pager.Page(cctx, initial, 0, gen0, scope)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.inFlight = false
	if f.gen == gen0 {
		f.cancel = nil
	}
	if f.gen != gen0 {
		st := f.snapshotLocked()
		st.Gen = gen0
		return st, nil
	}
	if err != nil {
		return f.snapshotLocked(), err
	}
	for _, c := range page {
		if !f.hashes[c.Hash] {
			f.commits = append(f.commits, c)
			f.hashes[c.Hash] = true
		}
	}
	f.skip = len(page)
	f.exhausted = len(page) < initial
	return f.snapshotLocked(), nil
}
```

- [ ] **Step 6: Add `ApplyScope`**

```go
// ApplyScope switches the feed to scope. If that scope's accumulation is cached
// (a prior toggle with no hard refresh since), it is restored with NO git call;
// otherwise the feed re-walks page 0. The current scope's accumulation is stashed
// first so toggling back is instant. Scope toggles (filter / solo / show-all /
// upstreams) call this; LoadInitial remains the cache-clearing hard refresh.
func (f *CommitFeed) ApplyScope(ctx context.Context, scope LogScope) (FeedState, error) {
	f.mu.Lock()
	f.stashCurrentLocked()
	if cs, ok := f.cache[scopeKey(scope)]; ok {
		if f.cancel != nil {
			f.cancel() // a cached restore supersedes any in-flight walk
		}
		f.cancel = nil
		f.gen++
		f.scope = scope
		f.commits = append([]model.Commit(nil), cs.commits...)
		f.hashes = make(map[string]bool, len(cs.hashes))
		for h := range cs.hashes {
			f.hashes[h] = true
		}
		f.skip = cs.skip
		f.exhausted = cs.exhausted
		f.inFlight = false
		st := f.snapshotLocked()
		f.mu.Unlock()
		return st, nil
	}
	f.scope = scope
	f.mu.Unlock()
	return f.loadInitialWalk(ctx)
}
```

- [ ] **Step 7: Run to verify pass**

Run: `go test ./internal/domain/ -run 'TestApplyScope|TestLoadInitial|TestCommitFeed|TestCanLoadMore' -v`
Expected: PASS (existing feed tests still green — `LoadInitial` behavior is unchanged except the added cache clear).

- [ ] **Step 8: Commit**

```bash
git add internal/domain/commitfeed.go internal/domain/commitfeed_test.go
git commit -m "feat(domain): CommitFeed scope cache + ApplyScope (restore on toggle, clear on refresh)"
```

---

## Task C3: TUI scope toggles use `ApplyScope`

**Files:**
- Modify: `internal/tui/commit_scope.go` (`reloadFeedCmd` ~132-144)
- Test: `internal/tui/commit_scope_test.go`

**Interfaces:**
- Consumes: `feed.ApplyScope` (Task C2). The `feed.SetScope` method stays (public; ApplyScope sets the scope internally, so the explicit call here is dropped).

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/commit_scope_test.go` (real svc+FakeRunner; assert a filter→clear round-trip restores without a re-walk):

```go
func TestReloadFeedRestoresOnFilterClear(t *testing.T) {
	f := gitexec.NewFakeRunner()
	svc := domain.New(&git.Repo{Runner: f})
	m := branchesPanelModel("main")
	m.svc = svc
	m.feed = svc.CommitFeed()
	m.feed.SetPageSizes(50, 50)
	m.sel = map[panel]int{}

	f.SetResponse("git log", gitexec.Result{Stdout: "h1\x1f\x1fAda\x1f0\x1fa\x1f\x1f\nh2\x1f\x1fAda\x1f0\x1fb\x1f\x1f\nh3\x1f\x1fAda\x1f0\x1fc\x1f\x1f\n"})
	m.feed.LoadInitial(context.Background()) // base: 3

	f.SetResponse("git log", gitexec.Result{Stdout: "h1\x1f\x1fAda\x1f0\x1fa\x1f\x1f\n"})
	m.commitFilter = commitFilterFields{Grep: "a"}
	m.feed.ApplyScope(context.Background(), m.feedScope()) // filtered: 1
	calls := len(f.Calls)

	// Clear the filter and run reloadFeedCmd → it must ApplyScope back to base and
	// restore from cache (no new git log).
	m.commitFilter = commitFilterFields{}
	msg := m.reloadFeedCmd()()
	rm, ok := msg.(commitsReloadedMsg)
	if !ok {
		t.Fatalf("want commitsReloadedMsg, got %T", msg)
	}
	if len(f.Calls) != calls {
		t.Fatalf("clear re-walked: calls %d → %d", calls, len(f.Calls))
	}
	if len(rm.state.Commits) != 3 {
		t.Fatalf("restored base = %d, want 3", len(rm.state.Commits))
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestReloadFeedRestoresOnFilterClear -v`
Expected: FAIL — `reloadFeedCmd` still calls `SetScope`+`LoadInitial`, which re-walks (calls increases) and clears the cache.

- [ ] **Step 3: Switch `reloadFeedCmd` to `ApplyScope`**

Replace the closure body in `internal/tui/commit_scope.go`:

```go
// reloadFeedCmd applies the model's scope to the feed off the UI thread. It uses
// ApplyScope so toggling a filter/solo back to a previously-walked scope restores
// the cached accumulation instantly; a genuinely new scope walks page 0. (A hard
// data refresh goes through loadCmd → feed.LoadInitial, which clears the cache.)
func (m Model) reloadFeedCmd() tea.Cmd {
	feed := m.feed
	scope := m.feedScope()
	return func() tea.Msg {
		st, _ := feed.ApplyScope(context.Background(), scope)
		return commitsReloadedMsg{gen: st.Gen, state: st}
	}
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/tui/ -run 'TestReloadFeed|TestCommitSolo|TestDataLoaded' -v`
Expected: PASS (solo/show-all and the upstream-reload tests still green).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/commit_scope.go internal/tui/commit_scope_test.go
git commit -m "feat(tui): scope toggles restore the loaded feed via ApplyScope"
```

---

## Task D1: Eager `/`-search engine

**Files:**
- Create: `internal/tui/commit_eager.go`
- Modify: `internal/tui/model.go` (add `eager` field to the struct ~129; extend the `commitsPagedMsg` handler ~310-318)
- Test: `internal/tui/commit_eager_test.go` (new)

**Interfaces:**
- Produces: `type eagerSearch struct { active bool; query string; budget int }`; `func (m Model) startEagerSearch(query string) (Model, tea.Cmd)`; `func (m Model) eagerAdvance() (Model, tea.Cmd)`; `func (m Model) firstCommitMatch(query string) (int, bool)`; `func (m Model) commitSearchMaxPages() int`.
- Consumes: `m.displayIndices(panelCommits)`, `m.commitHaystackAt(i)`, `m.feed.CanLoadMore()`, `m.loadMoreCmd()`, `m.commitsTotal()`, `m.cfg.UI.CommitSearchMaxPages`. The cap-prompt popup `eagerPrompt` is added in Task D3.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/commit_eager_test.go`:

```go
package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gigagit/gg/internal/model"
)

// eagerModel builds a Commits-focused model with the given loaded commits and a
// real (FakeRunner-backed) feed in a known state.
func eagerModel(t *testing.T, commits []model.Commit) Model {
	t.Helper()
	m := newTestModelForReload(t)
	m.focus = panelCommits
	m.commits = commits
	m = m.rebuildCommitGraph()
	return m
}

func TestEagerAdvanceJumpsOnMatch(t *testing.T) {
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix bug"}, {Hash: "b", Subject: "write docs"}})
	m.eager = eagerSearch{active: true, query: "docs", budget: 5}
	nm, _ := m.eagerAdvance()
	got := nm
	if got.eager.active {
		t.Fatal("a match must end the search")
	}
	if got.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want 1 (the matching row)", got.sel[panelCommits])
	}
}

func TestEagerAdvancePagesWhenNoMatch(t *testing.T) {
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	// Fresh feed → CanLoadMore true.
	m.eager = eagerSearch{active: true, query: "zzz", budget: 5}
	nm, cmd := m.eagerAdvance()
	if cmd == nil {
		t.Fatal("no match + budget + loadable → should dispatch a page load")
	}
	if !nm.commitsLoading {
		t.Fatal("paging should set commitsLoading")
	}
	if nm.eager.budget != 4 {
		t.Fatalf("budget = %d, want 4 (decremented)", nm.eager.budget)
	}
}

func TestEagerAdvanceReportsExhausted(t *testing.T) {
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	// Exhaust the feed: short initial page (the fake serves 1 row < 50).
	m.feed.SetPageSizes(50, 50)
	m.feed.LoadInitial(context.Background())
	m.commits = m.feed.Snapshot().Commits
	m = m.rebuildCommitGraph()
	m.eager = eagerSearch{active: true, query: "zzz", budget: 5}
	nm, cmd := m.eagerAdvance()
	if cmd != nil || nm.eager.active {
		t.Fatal("exhausted feed with no match → stop, no load")
	}
	if nm.statusMsg == "" {
		t.Fatal("should report 'not found in full history'")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestEagerAdvance -v`
Expected: FAIL — `eagerSearch` / `eagerAdvance` undefined.

- [ ] **Step 3: Add the `eager` model field**

In `internal/tui/model.go`, add to the `Model` struct (near the filter/highlight fields ~129):

```go
	eager eagerSearch // /-search-into-history paging state; eager.active gates the chain
```

- [ ] **Step 4: Create the engine**

Create `internal/tui/commit_eager.go`:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// eagerSearch is the state of a /-search that pages unloaded history looking for
// a match. budget is the number of pages left to scan before re-prompting.
type eagerSearch struct {
	active bool
	query  string
	budget int
}

// commitSearchMaxPages is the configured per-pass page cap for eager search.
func (m Model) commitSearchMaxPages() int {
	if n := m.cfg.UI.CommitSearchMaxPages; n > 0 {
		return n
	}
	return 5
}

// firstCommitMatch returns the DISPLAY position (same space as m.sel[panelCommits])
// of the first Commits row whose haystack contains query (case-insensitive), or
// (0, false). Mirrors scanHighlightMatch's display→backing mapping so it is correct
// under a non-default sort.
func (m Model) firstCommitMatch(query string) (int, bool) {
	if query == "" {
		return 0, false
	}
	idx := m.displayIndices(panelCommits)
	q := strings.ToLower(query)
	for d, bi := range idx {
		if strings.Contains(strings.ToLower(m.commitHaystackAt(bi)), q) {
			return d, true
		}
	}
	return 0, false
}

// startEagerSearch commits query as the active /-filter and begins scanning
// history for it (up to commitSearchMaxPages pages before asking to go deeper).
func (m Model) startEagerSearch(query string) (Model, tea.Cmd) {
	if query == "" {
		return m, nil
	}
	m.filterTyping = false
	m.filterPanel = panelCommits
	m.filterQuery = query
	m.eager = eagerSearch{active: true, query: query, budget: m.commitSearchMaxPages()}
	return m.eagerAdvance()
}

// eagerAdvance is the step function, re-entered after each loaded page: jump on a
// match; page while the budget allows and the feed can load; prompt at the cap;
// report exhaustion.
func (m Model) eagerAdvance() (Model, tea.Cmd) {
	if !m.eager.active {
		return m, nil
	}
	if d, ok := m.firstCommitMatch(m.eager.query); ok {
		m.sel[panelCommits] = d
		m.focus = panelCommits
		m.eager = eagerSearch{}
		return m, nil
	}
	if m.feed == nil || !m.feed.CanLoadMore() {
		m.statusMsg = "'" + m.eager.query + "' not found in full history"
		m.eager = eagerSearch{}
		return m, nil
	}
	if m.eager.budget <= 0 {
		// Cap reached with no match: pause and ask (Task D3 supplies eagerPrompt).
		q := m.eager.query
		m.eager.active = false
		return m.pushLayer(&eagerPrompt{query: q, scanned: m.commitsTotal()}), nil
	}
	m.eager.budget--
	m.commitsLoading = true
	return m, m.loadMoreCmd()
}
```

- [ ] **Step 5: Re-enter eager search when a page arrives**

In `internal/tui/model.go`, extend the `commitsPagedMsg` case so a loaded page advances an active eager search:

```go
	case commitsPagedMsg:
		if m.feed != nil && msg.gen == m.feed.Gen() {
			st := m.feed.Snapshot()
			m.commits = st.Commits
			m.commitsExhausted = st.Exhausted
			m.commitsLoading = false // this page's load (the latest) finished
			m = m.rebuildCommitGraph()
			if m.eager.active {
				return m.eagerAdvance()
			}
		}
		return m, nil
```

- [ ] **Step 6: Run to verify pass**

Note: Task D3 adds `eagerPrompt`. To keep D1 compiling/green on its own, the budget-cap branch references `eagerPrompt`; add a minimal stub now and flesh it out in D3, OR sequence D3 immediately after. Add this stub to `commit_eager.go` (replaced in D3):

```go
// eagerPrompt is the "search deeper?" dialog (Task D3 implements update/render).
type eagerPrompt struct {
	query   string
	scanned int
	sel     int
}
```

Then run: `go test ./internal/tui/ -run TestEagerAdvance -v`
Expected: PASS. (The stub satisfies the type reference; `pushLayer` needs the `layer` interface — if `eagerPrompt` must satisfy `layer` to compile, proceed directly to D3 and run D1+D3 tests together. If `pushLayer` takes `layer`, give the stub no-op `update`/`render` here and replace in D3.)

To be safe, give the stub no-op layer methods now:

```go
func (p *eagerPrompt) update(m Model, _ tea.KeyMsg) (Model, tea.Cmd) { return m.popLayer(), nil }
func (p *eagerPrompt) render(m Model, below string) string           { return below }
```

- [ ] **Step 7: Commit**

```bash
git add internal/tui/commit_eager.go internal/tui/commit_eager_test.go internal/tui/model.go
git commit -m "feat(tui): eager /-search engine (page history to the first match)"
```

---

## Task D2: Eager-search trigger keys + discoverability

**Files:**
- Modify: `internal/tui/model.go` (filter-typing switch ~570; main key switch ~1000)
- Modify: `internal/tui/help.go`, `internal/tui/footer.go`
- Test: `internal/tui/commit_eager_test.go`

**Interfaces:**
- Consumes: `m.startEagerSearch` (Task D1), `m.recordSearch(scopePanel, query)` (existing, returns `(Model, tea.Cmd)`).

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/commit_eager_test.go`:

```go
func TestCtrlFFromCommittedFilterStartsEager(t *testing.T) {
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	m.filterPanel = panelCommits
	m.filterQuery = "zzz" // committed /-filter, no loaded match
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	got := nm.(Model)
	// No match + fresh feed (CanLoadMore) → eager active and a load dispatched.
	if !got.eager.active || cmd == nil {
		t.Fatalf("ctrl+f should start eager search (active=%v cmd=%v)", got.eager.active, cmd != nil)
	}
}

func TestCtrlFWhileTypingCommitsAndSearches(t *testing.T) {
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	m.filterTyping = true
	m.filterPanel = panelCommits
	m.filterQuery = "zzz"
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	got := nm.(Model)
	if got.filterTyping {
		t.Fatal("ctrl+f should commit the /-filter (stop typing)")
	}
	if !got.eager.active {
		t.Fatal("ctrl+f while typing should start eager search")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run 'TestCtrlF' -v`
Expected: FAIL — ctrl+f unhandled.

- [ ] **Step 3: Trigger from the committed-filter (main switch)**

In `internal/tui/model.go`, in the main `tea.KeyMsg` switch (with the other Commits keys), add:

```go
		case "ctrl+f":
			// Eager /-search: scan unloaded history for the active Commits /-filter.
			if m.focus == panelCommits && m.filterPanel == panelCommits && m.filterQuery != "" {
				return m.startEagerSearch(m.filterQuery)
			}
```

- [ ] **Step 4: Trigger while typing a `/` query**

In the `if m.filterTyping {` block's `switch msg.Type {` (~570), add a case (before `tea.KeyEnter`):

```go
				case tea.KeyCtrlF:
					if m.filterPanel == panelCommits {
						var recCmd tea.Cmd
						m, recCmd = m.recordSearch(scopePanel, m.filterQuery)
						var cmd tea.Cmd
						m, cmd = m.startEagerSearch(m.filterQuery)
						return m, tea.Batch(recCmd, cmd)
					}
					return m, nil
```

(`scopePanel` is already in scope in this block — it is used by the surrounding `recallUpdate`/`recordSearch` calls.)

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/tui/ -run 'TestCtrlF' -v`
Expected: PASS.

- [ ] **Step 6: Advertise the keys (help + footer)**

In `internal/tui/help.go`, under the Commits/Global grouping, add rows (place near the existing `/` and `ctrl+p` rows):

```go
		r("ctrl+l", "Commits: load the next batch of commits now (without scrolling to the end)"),
		r("home/end", "jump to the top / bottom of the focused list; End on Commits also loads the next batch"),
		r("ctrl+f", "Commits: eager /-search — if the / query isn't in the loaded commits, page history to find it (asks before scanning deeper)"),
```

In `internal/tui/footer.go`, add footer entries mirroring the existing registry rows (`{id, key, label, availability, scope}` — match the exact struct shape at footer.go ~97). Use a Commits-scope availability where the registry supports it; otherwise `Model.opsIdle` like neighbors:

```go
	{"load-batch", "ctrl+l", "[ctrl+l] more", Model.opsIdle, scopeGlobal},
	{"eager-find", "ctrl+f", "[ctrl+f] find deeper", Model.opsIdle, scopeGlobal},
```

(Confirm the exact field names/types and `scope*` constants from the surrounding entries; Home/End are conventional and may be omitted from the footer if space is tight, but MUST stay in help. The `help_test.go` drift guard checks footer keys appear in help — keep them consistent.)

- [ ] **Step 7: Run the help/footer drift guard**

Run: `go test ./internal/tui/ -run 'Help|Footer' -v`
Expected: PASS (every footer key is present in help).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/help.go internal/tui/footer.go internal/tui/commit_eager_test.go
git commit -m "feat(tui): ctrl+f triggers eager /-search; advertise ctrl+l/ctrl+f/home/end"
```

---

## Task D3: The "search deeper?" cap dialog

**Files:**
- Modify: `internal/tui/commit_eager.go` (replace the `eagerPrompt` stub with a real popup)
- Test: `internal/tui/commit_eager_test.go`

**Interfaces:**
- Consumes: the `layer` interface (`update(Model, tea.KeyMsg) (Model, tea.Cmd)`, `render(Model, string) string`), `m.pushLayer`/`m.popLayer`, `m.overlayDims`, `popupInnerWidth`, `popupTextWidth`, `popupBox`, `overlayCenter`, `clipToHeight`, `selectedRow`, `padRight` (all used by `command_palette.go`).

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/commit_eager_test.go`:

```go
func TestEagerAdvanceOpensPromptAtCap(t *testing.T) {
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	// budget exhausted, no match, feed loadable → prompt.
	m.eager = eagerSearch{active: true, query: "zzz", budget: 0}
	nm, _ := m.eagerAdvance()
	got := nm
	if got.eager.active {
		t.Fatal("at the cap the search pauses (inactive) pending the dialog")
	}
	if _, ok := got.topLayer().(*eagerPrompt); !ok {
		t.Fatalf("expected an eagerPrompt on top, got %T", got.topLayer())
	}
}

func TestEagerPromptSearchMoreResumes(t *testing.T) {
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	m = m.pushLayer(&eagerPrompt{query: "zzz", scanned: 1, sel: 0})
	p := m.topLayer().(*eagerPrompt)
	nm, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	got := nm
	if _, ok := got.topLayer().(*eagerPrompt); ok {
		t.Fatal("choosing 'search more' should pop the prompt")
	}
	if !got.eager.active || cmd == nil {
		t.Fatal("'search more' should resume eager search with a fresh budget")
	}
}

func TestEagerPromptCancelStops(t *testing.T) {
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "fix"}})
	m = m.pushLayer(&eagerPrompt{query: "zzz", scanned: 1, sel: 1}) // sel 1 = Cancel
	p := m.topLayer().(*eagerPrompt)
	nm, _ := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if nm.eager.active {
		t.Fatal("cancel must not resume the search")
	}
	if _, ok := nm.topLayer().(*eagerPrompt); ok {
		t.Fatal("cancel should pop the prompt")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run 'TestEagerAdvanceOpensPromptAtCap|TestEagerPrompt' -v`
Expected: FAIL — the stub `update` pops unconditionally and never resumes.

- [ ] **Step 3: Replace the stub with the real popup**

In `internal/tui/commit_eager.go`, replace the `eagerPrompt` stub (type + the two no-op methods) with:

```go
// eagerPrompt is the "search deeper?" dialog shown when an eager /-search reaches
// its page cap with no match. enter on "Search N more" resumes with a fresh
// budget; Cancel/esc stops, leaving the /-filter active on the loaded set.
type eagerPrompt struct {
	query   string
	scanned int
	sel     int // 0 = search more, 1 = cancel
}

func (p *eagerPrompt) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case "esc":
		return m.popLayer(), nil
	case "up", "k":
		p.sel = 0
	case "down", "j":
		p.sel = 1
	case "enter":
		if p.sel == 1 {
			return m.popLayer(), nil
		}
		m = m.popLayer()
		m.eager = eagerSearch{active: true, query: p.query, budget: m.commitSearchMaxPages()}
		return m.eagerAdvance()
	}
	return m, nil
}

func (p *eagerPrompt) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *eagerPrompt) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	textW := popupTextWidth(inner)
	parts := []string{
		"Search deeper?",
		"",
		"Searched " + strconv.Itoa(p.scanned) + " commits, no match for \"" + p.query + "\".",
		"",
	}
	opts := []string{"Search " + strconv.Itoa(m.commitSearchMaxPages()) + " more pages", "Cancel"}
	for i, o := range opts {
		prefix, st := "  ", lipgloss.NewStyle()
		if i == p.sel {
			prefix, st = "> ", selectedRow
		}
		parts = append(parts, st.Render(padRight(prefix+o, textW)))
	}
	parts = append(parts, "", "[enter] choose  [esc] cancel")
	return popupBox(inner, strings.Join(parts, "\n"))
}
```

Update the imports of `commit_eager.go` to add `strconv` and `github.com/charmbracelet/lipgloss` (keep `strings`, `tea`).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/tui/ -run 'TestEager' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/commit_eager.go internal/tui/commit_eager_test.go
git commit -m "feat(tui): 'search deeper?' dialog for eager /-search at the page cap"
```

---

## Task E1: Docs + manual eyeball build

**Files:**
- Modify: `CHANGELOG.md`, `README.md`

- [ ] **Step 1: CHANGELOG**

Add under `## [Unreleased]` → `### Added` in `CHANGELOG.md`:

```markdown
- **Deeper, on-demand commit loading.** The Commits panel now loads more history
  before you hit the bottom and lets you reach it directly. New `[ui]` settings
  set the counts: `commit_initial_count` (first paint, default 300, up from 50),
  `commit_batch_size` (per later page, default 300), and
  `commit_search_max_pages` (default 5). `ctrl+l` loads the next batch on demand;
  **Home**/**End** jump to the top/bottom of any list, and End on Commits also
  loads the next batch (press again to walk deeper). Applying then clearing a `\`
  commit filter now restores the commits you had already loaded instead of
  re-walking from the top. Eager `/`-search: when a `/` query isn't among the
  loaded commits, `ctrl+f` pages history to find it, jumping to the first hit and
  asking before it scans deeper.
```

- [ ] **Step 2: README**

In `README.md`, add the keys to the Commits keybinding list (match the surrounding format) and mention the three `[ui]` settings in the config section.

- [ ] **Step 3: Full race suite**

Run: `./test.sh race`
Expected: `all green`.

- [ ] **Step 4: Build the eyeball binary**

Run: `go build -o ./gg ./cmd/gg`
Report the absolute worktree path of `./gg` to the user for a live check.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md
git commit -m "docs: changelog + README for commit-loading improvements"
```

---

## Self-Review

**Spec coverage:**
- Part 1 (config sizes) → A1 (config), A2 (feed honors), A3 (TUI wires + first-paint ordering). ✓
- Part 2 (ctrl+l, Home, End) → B1 (ctrl+l + CanLoadMore), B2 (Home/End). ✓
- Part 3 (filter-clear restore + scopeKey fix + whole-cache invalidation) → C1 (scopeKey), C2 (cache+ApplyScope+LoadInitial clears all), C3 (TUI wiring). ✓ Cross-scope staleness test in C2. ✓
- Part 4 (eager search, ctrl+f, repeat dialog) → D1 (engine + paged re-entry), D2 (keys + help/footer), D3 (cap dialog). ✓
- Docs → E1. ✓

**Type consistency:** `SetPageSizes(initial, batch int)`, `CanLoadMore() bool`, `ApplyScope(ctx, scope) (FeedState, error)`, `eagerSearch{active,query,budget}`, `eagerAdvance`/`startEagerSearch`/`firstCommitMatch`/`commitSearchMaxPages`, `eagerPrompt{query,scanned,sel}` — names match across tasks. `LogScope = git.LogScope` alias; `scopeKey` inlines the filter check (git's `filtered()` is unexported). ✓

**Known follow-up to confirm during execution (not placeholders):** footer registry exact field names/`scope*` constants (Task D2 Step 6) and the `recordSearch` return arity (used as `(Model, tea.Cmd)` per model.go:578) — verify against the live files when wiring; the surrounding code shows the shapes.
