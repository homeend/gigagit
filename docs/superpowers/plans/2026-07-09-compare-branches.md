# Compare Branches Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A "Compare A ↔ B" row in the Branches pair-op popup that opens the existing compare files view with the full tip-to-tip diff, plus an `f`-key origin filter (all / only files A changed / only files B changed) computed from the merge base.

**Architecture:** A new domain read query `CompareOrigins` (merge-base + two `M..tip` numstat path lists) feeds a small TUI state struct (`comparePairState`) attached to the existing compare-mode files view. The pair-op row reuses the `pairOp.open` seam (the interactive-rebase pattern) and the existing `openCompareFiles`. No engine op, no CLI change, no config.

**Tech Stack:** Go 1.26, Bubble Tea TUI, real `git` in `t.TempDir()` for domain tests.

**Spec:** `docs/superpowers/specs/2026-07-08-compare-branches-design.md` (committed on this branch).

## Global Constraints

- **Work in the feature worktree**: `/mnt/t/others/gigagit/.claude/worktrees/compare-branches` (branch `feat/compare-branches`). Every file path below is relative to that root; `cd` there first and verify with `git branch --show-current`. Write/Edit tools MUST use the worktree absolute path.
- `internal/tui` never imports `internal/git` (archtest-guarded) — the TUI reaches git only through `internal/domain`.
- TUI `Model` is a **value receiver**; state that must persist across the value copy lives behind pointer fields (the new `comparePair *comparePairState` follows `filesView *contentPopup`).
- A git verb is one invocation; `CompareOrigins` reuses the existing `MergeBase` + `DiffNumstat` verbs — **no new git verb**.
- Domain reads go through the `query(ctx, s, key, fn)` helper (Read reservation + singleflight + failure recording).
- The filter exists **only for branch-pair compares**; every other compare (commits ◉, bookmarks, shelf, stash, WIP rows) is untouched.
- The compare view title must show **full branch names** — never `Endpoint.Display()` (it truncates to 7 chars).
- Scope-filter cycle order: all → only files A (left/marked) changed → only files B (right/selected) changed → all.
- Status notes (exact copy): `origin filter loading…`, `no common ancestor — filter unavailable`, `origin filter unavailable: <err>`.
- TDD: write the failing test first; run it; implement; re-run. Commit after each task.
- Advertise the feature in help.go AND the files-view footer hint (project convention).

---

### Task 1: `model.CompareOrigins` + domain query `CompareOrigins`

**Files:**
- Modify: `internal/model/model.go` (after `CommitFile`, ~line 182)
- Create: `internal/domain/compare_origins.go`
- Test: `internal/domain/compare_origins_test.go`

**Interfaces:**
- Consumes: `git.MergeBase(ctx, a, b) (string, error)` (`internal/git/mergebase.go`), `git.DiffNumstat(ctx, spec model.DiffSpec) (string, error)` + `git.ParseNumstat(out string) []model.DiffStat` (`internal/git/diff_raw.go`), the `query` helper (`internal/domain/query.go`).
- Produces (Task 2/3 rely on these exact names):
  - `model.CompareOrigins{APaths, BPaths map[string]bool}`
  - `domain.ErrNoMergeBase` (sentinel `error`)
  - `(*domain.Service).CompareOrigins(ctx context.Context, a, b string) (model.CompareOrigins, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/domain/compare_origins_test.go`. The package's `newRealRepo(t)` helper (in `compare_test.go`) builds a real repo with one commit on `main` and returns `(dir, *Service)`; `run` clones its inline git-runner pattern:

```go
package domain

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitIn runs git in dir, failing the test on error (mirrors newRealRepo's run).
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeIn(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Two diverged branches: A changed a.txt (and renamed r-old.txt), B changed
// b.txt and also a shared.txt. Origin sets must attribute paths to the branch
// that touched them since the merge base, including both rename sides.
func TestCompareOriginsAttributesPaths(t *testing.T) {
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	// Base: add r-old.txt and shared.txt on main.
	writeIn(t, dir, "r-old.txt", "rename me\nlots of stable content\nso git sees a rename\n")
	writeIn(t, dir, "shared.txt", "shared\n")
	gitIn(t, dir, "add", "r-old.txt", "shared.txt")
	gitIn(t, dir, "commit", "-m", "base files")

	// Branch A: change a.txt, rename r-old.txt -> r-new.txt.
	gitIn(t, dir, "checkout", "-b", "feat/a")
	writeIn(t, dir, "a.txt", "a\n")
	gitIn(t, dir, "mv", "r-old.txt", "r-new.txt")
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-m", "a work")

	// Branch B (from main): change b.txt and shared.txt.
	gitIn(t, dir, "checkout", "main")
	gitIn(t, dir, "checkout", "-b", "feat/b")
	writeIn(t, dir, "b.txt", "b\n")
	writeIn(t, dir, "shared.txt", "shared, changed by b\n")
	gitIn(t, dir, "add", "b.txt", "shared.txt")
	gitIn(t, dir, "commit", "-m", "b work")

	got, err := svc.CompareOrigins(ctx, "feat/a", "feat/b")
	if err != nil {
		t.Fatalf("CompareOrigins: %v", err)
	}
	for _, p := range []string{"a.txt", "r-old.txt", "r-new.txt"} {
		if !got.APaths[p] {
			t.Errorf("APaths missing %q (have %v)", p, got.APaths)
		}
	}
	for _, p := range []string{"b.txt", "shared.txt"} {
		if !got.BPaths[p] {
			t.Errorf("BPaths missing %q (have %v)", p, got.BPaths)
		}
	}
	if got.APaths["b.txt"] || got.APaths["shared.txt"] {
		t.Errorf("APaths contains B-only paths: %v", got.APaths)
	}
	if got.BPaths["a.txt"] {
		t.Errorf("BPaths contains A-only path a.txt: %v", got.BPaths)
	}
}

// Unrelated histories (orphan branch) have no merge base: the typed sentinel
// lets the TUI show "filter unavailable" without string matching.
func TestCompareOriginsNoMergeBase(t *testing.T) {
	dir, svc := newRealRepo(t)
	ctx := context.Background()

	gitIn(t, dir, "checkout", "--orphan", "orphan")
	writeIn(t, dir, "o.txt", "o\n")
	gitIn(t, dir, "add", "o.txt")
	// The orphan index still holds README.md from main; commit everything.
	gitIn(t, dir, "commit", "-m", "orphan root")

	_, err := svc.CompareOrigins(ctx, "main", "orphan")
	if !errors.Is(err, ErrNoMergeBase) {
		t.Fatalf("err = %v, want ErrNoMergeBase", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/compare-branches
go test ./internal/domain/ -run 'TestCompareOrigins' -v
```
Expected: compile error — `svc.CompareOrigins` and `ErrNoMergeBase` undefined.

- [ ] **Step 3: Add the model type**

In `internal/model/model.go`, directly after the `CommitFile` struct (~line 182):

```go
// CompareOrigins attributes changed paths to each side of a branch
// comparison: APaths/BPaths hold every path the respective branch touched
// since the two diverged (diff merge-base..tip), keyed for membership tests.
// Renames contribute both their old and new path.
type CompareOrigins struct {
	APaths map[string]bool
	BPaths map[string]bool
}
```

- [ ] **Step 4: Implement the domain query**

Create `internal/domain/compare_origins.go`:

```go
package domain

import (
	"context"
	"errors"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// ErrNoMergeBase is returned by CompareOrigins when the two revisions share
// no common ancestor (unrelated histories), so per-branch origin sets are
// undefined. Callers detect it with errors.Is.
var ErrNoMergeBase = errors.New("no common ancestor")

// CompareOrigins attributes changed paths to each side of a branch
// comparison: for M = merge-base(a, b), APaths = paths touched by M..a and
// BPaths = paths touched by M..b (renames contribute both old and new path).
// Three git invocations under one Read reservation. Any merge-base failure
// maps to ErrNoMergeBase (wrapping the cause): callers pass refs taken from
// the branches list, so "bad ref" is not a distinct case worth surfacing.
func (s *Service) CompareOrigins(ctx context.Context, a, b string) (model.CompareOrigins, error) {
	return query(ctx, s, "compare-origins:"+a+":"+b, func(ctx context.Context) (model.CompareOrigins, error) {
		base, err := s.repo.MergeBase(ctx, a, b)
		if err != nil {
			return model.CompareOrigins{}, fmt.Errorf("%w: %v", ErrNoMergeBase, err)
		}
		aPaths, err := s.originPaths(ctx, base, a)
		if err != nil {
			return model.CompareOrigins{}, err
		}
		bPaths, err := s.originPaths(ctx, base, b)
		if err != nil {
			return model.CompareOrigins{}, err
		}
		return model.CompareOrigins{APaths: aPaths, BPaths: bPaths}, nil
	})
}

// originPaths returns the set of paths touched by base..tip, both rename
// sides included.
func (s *Service) originPaths(ctx context.Context, base, tip string) (map[string]bool, error) {
	out, err := s.repo.DiffNumstat(ctx, model.DiffSpec{Rev: base + ".." + tip})
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, st := range git.ParseNumstat(out) {
		set[st.Path] = true
		if st.OldPath != "" {
			set[st.OldPath] = true
		}
	}
	return set, nil
}
```

- [ ] **Step 5: Run the tests, vet, and the domain package suite**

```bash
go test ./internal/domain/ -run 'TestCompareOrigins' -v   # expect PASS (2 tests)
go vet ./internal/domain/ ./internal/model/
go test ./internal/domain/                                 # whole package still green
```

- [ ] **Step 6: Commit**

```bash
git add internal/model/model.go internal/domain/compare_origins.go internal/domain/compare_origins_test.go
git commit -m "feat(domain): CompareOrigins query — per-branch changed-path sets from the merge base"
```

---

### Task 2: pair-op "Compare A ↔ B" row + `openBranchCompare`

**Files:**
- Create: `internal/tui/branch_compare.go`
- Modify: `internal/tui/mark.go` (add 4th row in `pairOpsFor`, ~line 56)
- Modify: `internal/tui/model.go` (Model field near `compareTag` ~line 103; `compareFilesMsg` handler ~line 447; new `compareOriginsMsg` case beside it)
- Modify: `internal/tui/files_view.go` (`closeFilesView`, ~line 35)
- Test: `internal/tui/branch_compare_test.go`

**Interfaces:**
- Consumes (Task 1): `svc.CompareOrigins(ctx, a, b) (model.CompareOrigins, error)`, `domain.ErrNoMergeBase`. Existing: `openCompareFiles(left, right model.Endpoint) (Model, tea.Cmd)` (`files_view.go:247`), `pairOp.open` seam (`mark.go`), `model.Endpoint{Kind: model.EndpointCommit, Hash: <branch>}`.
- Produces (Task 3 relies on these exact names):
  - `type compareScope int` with `compareScopeAll`, `compareScopeLeft`, `compareScopeRight`
  - `type comparePairState struct { left, right string; files []model.CommitFile; origins model.CompareOrigins; originsLoaded bool; originsErr error; scope compareScope }`
  - Model field `comparePair *comparePairState`
  - `branchCompareTitle(left, right string, scope compareScope) string`
  - `(Model).openBranchCompare(marked, selected string) (Model, tea.Cmd)`
  - `compareOriginsMsg{tag string; origins model.CompareOrigins; err error}`

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/branch_compare_test.go`. Follow the `pairModel()` pattern from `pairop_popup_test.go` (a bare `Model{width, height}` — bubbletea cmds are plain funcs and are NOT invoked, so no `svc` is needed):

```go
package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// The Branches pair-op popup offers Compare as its 4th row, spelling out both
// names in ↔ form.
func TestPairOpsIncludeCompare(t *testing.T) {
	ops := pairOpsFor(panelBranches)
	if len(ops) != 4 {
		t.Fatalf("pairOpsFor(panelBranches) has %d ops, want 4", len(ops))
	}
	got := ops[3].label("feat/x", "main")
	if got != "Compare feat/x ↔ main" {
		t.Fatalf("compare label = %q", got)
	}
	if ops[3].open == nil || ops[3].build != nil {
		t.Fatal("compare row must use the open seam (no engine op)")
	}
}

// Enter on the Compare row opens the files view in compare mode with
// branch-name endpoints, a full-name title (Endpoint.Display would truncate
// long branch names to 7 chars), the pair state armed, popup gone, mark gone.
func TestCompareRowOpensBranchCompare(t *testing.T) {
	const marked, selected = "feature/long-branch-name", "main"
	m := Model{width: 120, height: 40}
	m.mark = &markState{panel: panelBranches, key: marked, display: marked}
	m = m.pushLayer(newPairOpPopup(m.width, marked, selected, pairOpsFor(panelBranches)))

	// Move to the 4th row (Compare) and run it.
	for range 3 {
		mm, _ := m.Update(keyMsg("j"))
		m = mm.(Model)
	}
	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)

	if m.filesView == nil || !m.inCompareMode() {
		t.Fatal("compare row should open the files view in compare mode")
	}
	if m.filesLeft.Hash != marked || m.filesRight.Hash != selected {
		t.Fatalf("endpoints = %q / %q, want %q / %q", m.filesLeft.Hash, m.filesRight.Hash, marked, selected)
	}
	if !strings.Contains(m.filesTitle, marked+" ↔ "+selected) {
		t.Fatalf("title %q must carry the FULL branch names", m.filesTitle)
	}
	if m.comparePair == nil || m.comparePair.left != marked || m.comparePair.right != selected {
		t.Fatalf("comparePair = %+v, want %s/%s", m.comparePair, marked, selected)
	}
	if m.comparePair.scope != compareScopeAll {
		t.Fatalf("scope = %v, want compareScopeAll", m.comparePair.scope)
	}
	if layerOf[*pairOpPopup](m) != nil {
		t.Fatal("pair-op popup should close")
	}
	if m.mark != nil {
		t.Fatal("the mark should clear")
	}
}

// A compareOriginsMsg for the live tag lands in the pair state; a stale tag
// (view closed or a different compare opened) is dropped.
func TestCompareOriginsMsgTagGate(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openBranchCompare("feat/x", "main")

	origins := model.CompareOrigins{APaths: map[string]bool{"a.txt": true}, BPaths: map[string]bool{}}
	mm, _ := m.Update(compareOriginsMsg{tag: m.compareTag, origins: origins})
	m = mm.(Model)
	if !m.comparePair.originsLoaded || !m.comparePair.origins.APaths["a.txt"] {
		t.Fatalf("live origins msg should land: %+v", m.comparePair)
	}

	// Stale: different tag must not clobber state.
	m.comparePair.originsLoaded = false
	mm, _ = m.Update(compareOriginsMsg{tag: "cmp:other:pair", origins: origins})
	m = mm.(Model)
	if m.comparePair.originsLoaded {
		t.Fatal("stale origins msg (tag mismatch) must be dropped")
	}
}

// The raw compare file list is retained on comparePair (Task 3 rebuilds rows
// from it when the scope changes); non-branch compares keep the old behavior.
func TestCompareFilesMsgRetainsRawListForBranchPair(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openBranchCompare("feat/x", "main")
	files := []model.CommitFile{{Status: "M", Path: "a.txt"}}
	mm, _ := m.Update(compareFilesMsg{tag: m.compareTag, files: files})
	m = mm.(Model)
	if len(m.comparePair.files) != 1 || m.comparePair.files[0].Path != "a.txt" {
		t.Fatalf("raw list not retained: %+v", m.comparePair.files)
	}
}

// closeFilesView must drop the pair state (it is compare-view-scoped).
func TestCloseFilesViewClearsComparePair(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openBranchCompare("feat/x", "main")
	m = m.closeFilesView()
	if m.comparePair != nil {
		t.Fatal("closeFilesView must clear comparePair")
	}
}

// Re-running the SAME branch pair keeps the showing view (the
// openCompareFiles same-tag convention) and does not re-arm state.
func TestOpenBranchCompareSamePairKeepsView(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openBranchCompare("feat/x", "main")
	m.comparePair.originsLoaded = true // pretend origins landed
	m, _ = m.openBranchCompare("feat/x", "main")
	if !m.comparePair.originsLoaded {
		t.Fatal("same-pair reopen must keep the existing state (no reset)")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/tui/ -run 'TestPairOpsIncludeCompare|TestCompareRowOpens|TestCompareOriginsMsg|TestCompareFilesMsgRetains|TestCloseFilesViewClearsComparePair|TestOpenBranchCompareSamePair' -v
```
Expected: compile error — `comparePair`, `openBranchCompare`, `compareOriginsMsg`, `compareScopeAll` undefined.

- [ ] **Step 3: Implement `branch_compare.go`**

Create `internal/tui/branch_compare.go`:

```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// compareScope selects which subset of a branch-pair comparison's files is
// listed: everything, or only the files one branch touched since the two
// diverged (the origin sets from domain.CompareOrigins).
type compareScope int

const (
	compareScopeAll   compareScope = iota // every tip-to-tip difference
	compareScopeLeft                      // only files the left (marked) branch changed
	compareScopeRight                     // only files the right (selected) branch changed
)

// comparePairState is the branch-pair extension of the compare files view:
// present only when the comparison was opened from the Branches pair-op
// Compare row. It retains the raw tip-to-tip file list (so scope changes
// rebuild rows locally) and the async-loaded origin sets. Pointer field on
// Model (value receiver): mutations persist across the value copy.
type comparePairState struct {
	left, right   string             // full branch names (display + origin labels)
	files         []model.CommitFile // raw tip-to-tip compare list (retained for filtering)
	origins       model.CompareOrigins
	originsLoaded bool
	originsErr    error
	scope         compareScope
}

// compareOriginsMsg delivers the async origin-set load; tag gates staleness
// against m.compareTag (the compareFilesMsg convention).
type compareOriginsMsg struct {
	tag     string
	origins model.CompareOrigins
	err     error
}

// branchCompareTitle is the compare-view title for a branch pair, spelling
// out FULL branch names (Endpoint.Display truncates to 7 chars) and the
// active scope.
func branchCompareTitle(left, right string, scope compareScope) string {
	t := left + " ↔ " + right
	switch scope {
	case compareScopeLeft:
		t += " — only files " + left + " changed"
	case compareScopeRight:
		t += " — only files " + right + " changed"
	}
	return t
}

// openBranchCompare opens the compare files view for two branches (full
// tip-to-tip diff, marked = left/older, selected = right/newer), arms the
// branch-pair state, and starts the origin-set load in the background.
func (m Model) openBranchCompare(marked, selected string) (Model, tea.Cmd) {
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: marked}
	right := model.Endpoint{Kind: model.EndpointCommit, Hash: selected}
	tag := "cmp:" + left.CacheTag() + ":" + right.CacheTag()
	// Same pair already showing: keep it (the openCompareFiles same-tag
	// convention), and keep its state — re-arming would drop loaded origins.
	if m.filesView != nil && m.inCompareMode() && m.compareTag == tag && m.comparePair != nil {
		return m, nil
	}
	var cmd tea.Cmd
	m, cmd = m.openCompareFiles(left, right) // clean slate: clears any prior comparePair
	m.comparePair = &comparePairState{left: marked, right: selected}
	m.filesTitle = branchCompareTitle(marked, selected, compareScopeAll)
	return m, tea.Batch(cmd, m.loadCompareOriginsCmd(marked, selected, tag))
}

// loadCompareOriginsCmd fetches the origin sets off the UI thread.
func (m Model) loadCompareOriginsCmd(a, b, tag string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		origins, err := svc.CompareOrigins(context.Background(), a, b)
		return compareOriginsMsg{tag: tag, origins: origins, err: err}
	}
}
```

- [ ] **Step 4: Add the pair-op row**

In `internal/tui/mark.go`, append to the slice in `pairOpsFor` (after the "Interactive rebase" entry, before the closing `}`):

```go
		{
			label:   func(marked, selected string) string { return "Compare " + marked + " ↔ " + selected },
			enabled: true,
			open: func(m Model, marked, selected string) (Model, tea.Cmd) {
				return m.openBranchCompare(marked, selected)
			},
		},
```

- [ ] **Step 5: Wire Model state + messages**

In `internal/tui/model.go`:

a) Add the field right after `compareTag` (~line 103):

```go
	comparePair       *comparePairState // branch-pair compare extension (origin filter); nil for every other compare
```

b) In the `case compareFilesMsg:` handler (~line 447), retain the raw list for a branch pair — insert immediately before `m.filesView.lines = commitFileLines(msg.files)`:

```go
		if m.comparePair != nil {
			m.comparePair.files = msg.files
		}
```

c) Add a new case directly after the `case compareFilesMsg:` block:

```go
	case compareOriginsMsg:
		if m.filesView == nil || !m.inCompareMode() || m.comparePair == nil || msg.tag != m.compareTag {
			return m, nil // stale or closed
		}
		m.comparePair.origins = msg.origins
		m.comparePair.originsErr = msg.err
		m.comparePair.originsLoaded = msg.err == nil
		return m, nil
```

d) In `internal/tui/files_view.go`, `closeFilesView` (~line 35), add beside `m.compareTag = ""`:

```go
	m.comparePair = nil
```

- [ ] **Step 6: Run the tests, vet, build**

```bash
go test ./internal/tui/ -run 'TestPairOpsIncludeCompare|TestCompareRowOpens|TestCompareOriginsMsg|TestCompareFilesMsgRetains|TestCloseFilesViewClearsComparePair|TestOpenBranchCompareSamePair' -v
go vet ./internal/tui/
go build ./cmd/gg
go test ./internal/tui/    # whole package still green (pair-op popup tests count ops)
```
Expected: all PASS. If any pre-existing test asserts the pair-op popup has exactly 3 rows, update it to 4 — the new row is plan-mandated.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/branch_compare.go internal/tui/branch_compare_test.go internal/tui/mark.go internal/tui/model.go internal/tui/files_view.go
git commit -m "feat(tui): Compare A <-> B row in the Branches pair-op popup"
```

---

### Task 3: `f` origin filter — cycle, filter, hint, help

**Files:**
- Modify: `internal/tui/branch_compare.go` (filter helpers + the key handler's logic)
- Modify: `internal/tui/files_view.go` (`case "f":` in `updateFilesViewKey`, ~after the `case "b":` block; hint line ~748)
- Modify: `internal/tui/help.go` (files-view section ~line 204; Branches `m` line 27)
- Test: `internal/tui/branch_compare_test.go` (extend)

**Interfaces:**
- Consumes (Task 2): `comparePairState` (all fields), `compareScope` constants, `branchCompareTitle`, Model field `comparePair`. Task 1: `domain.ErrNoMergeBase`. Existing: `commitFileLines(files []model.CommitFile) []contentLine` (`files_view.go:124`), `m.statusMsg`.
- Produces:
  - `(*comparePairState).pathSet() map[string]bool` (nil for `compareScopeAll`)
  - `filterCompareFiles(files []model.CommitFile, set map[string]bool) []model.CommitFile`
  - `(Model).cycleCompareScope() Model` — the whole `f` behavior (notes included)

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/branch_compare_test.go`:

```go
// filterCompareFiles keeps rows whose new OR old path is in the set (a
// rename matches from either side); a nil set means "all".
func TestFilterCompareFiles(t *testing.T) {
	files := []model.CommitFile{
		{Status: "M", Path: "a.txt"},
		{Status: "M", Path: "b.txt"},
		{Status: "R", Path: "r-new.txt", OldPath: "r-old.txt"},
	}
	if got := filterCompareFiles(files, nil); len(got) != 3 {
		t.Fatalf("nil set should keep all rows, got %d", len(got))
	}
	set := map[string]bool{"a.txt": true, "r-old.txt": true}
	got := filterCompareFiles(files, set)
	if len(got) != 2 || got[0].Path != "a.txt" || got[1].Path != "r-new.txt" {
		t.Fatalf("filtered = %+v, want a.txt + the rename (matched via old path)", got)
	}
}

// f cycles all -> left-only -> right-only -> all, rebuilding rows and title.
func TestFKeyCyclesScope(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openBranchCompare("feat/x", "main")
	files := []model.CommitFile{
		{Status: "M", Path: "a.txt"},
		{Status: "M", Path: "b.txt"},
	}
	mm, _ := m.Update(compareFilesMsg{tag: m.compareTag, files: files})
	m = mm.(Model)
	origins := model.CompareOrigins{
		APaths: map[string]bool{"a.txt": true},
		BPaths: map[string]bool{"b.txt": true},
	}
	mm, _ = m.Update(compareOriginsMsg{tag: m.compareTag, origins: origins})
	m = mm.(Model)

	mm, _ = m.Update(keyMsg("f")) // -> left only
	m = mm.(Model)
	if m.comparePair.scope != compareScopeLeft {
		t.Fatalf("scope = %v, want left", m.comparePair.scope)
	}
	if got := len(m.filesView.lines); got != 1 {
		t.Fatalf("left-only rows = %d, want 1 (a.txt)", got)
	}
	if !strings.Contains(m.filesTitle, "only files feat/x changed") {
		t.Fatalf("title = %q", m.filesTitle)
	}

	mm, _ = m.Update(keyMsg("f")) // -> right only
	m = mm.(Model)
	if m.comparePair.scope != compareScopeRight {
		t.Fatalf("scope = %v, want right", m.comparePair.scope)
	}
	if !strings.Contains(m.filesTitle, "only files main changed") {
		t.Fatalf("title = %q", m.filesTitle)
	}

	mm, _ = m.Update(keyMsg("f")) // -> all
	m = mm.(Model)
	if m.comparePair.scope != compareScopeAll {
		t.Fatalf("scope = %v, want all", m.comparePair.scope)
	}
	if got := len(m.filesView.lines); got != 2 {
		t.Fatalf("all rows = %d, want 2", got)
	}
	if strings.Contains(m.filesTitle, "only files") {
		t.Fatalf("all-scope title should carry no filter suffix: %q", m.filesTitle)
	}
}

// f before the origin sets land: status note, scope unchanged.
func TestFKeyBeforeOriginsLoaded(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openBranchCompare("feat/x", "main")
	mm, _ := m.Update(keyMsg("f"))
	m = mm.(Model)
	if m.comparePair.scope != compareScopeAll {
		t.Fatal("scope must stay all while origins are loading")
	}
	if m.statusMsg != "origin filter loading…" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

// No merge base: the typed sentinel maps to the unavailable note.
func TestFKeyNoMergeBase(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openBranchCompare("feat/x", "main")
	mm, _ := m.Update(compareOriginsMsg{tag: m.compareTag, err: fmt.Errorf("%w: exit 1", domain.ErrNoMergeBase)})
	m = mm.(Model)
	mm, _ = m.Update(keyMsg("f"))
	m = mm.(Model)
	if m.comparePair.scope != compareScopeAll {
		t.Fatal("scope must stay all without a merge base")
	}
	if m.statusMsg != "no common ancestor — filter unavailable" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

// f is inert in a NON-branch compare (comparePair == nil).
func TestFKeyInertInNonBranchCompare(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openCompareFiles(
		model.Endpoint{Kind: model.EndpointCommit, Hash: "abc1234"},
		model.Endpoint{Kind: model.EndpointWorkTree})
	mm, _ := m.Update(keyMsg("f"))
	m = mm.(Model)
	if m.statusMsg != "" {
		t.Fatalf("f in a non-branch compare must be inert, got note %q", m.statusMsg)
	}
}

// The filtered view renders (guards the green-unit/broken-render class) and
// the footer hint advertises [f] filter for a branch pair.
func TestBranchCompareRendersWithFilter(t *testing.T) {
	m := loadedModel(t) // real repo + svc (nav_test.go); cmds are not invoked
	mm0, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = mm0.(Model)
	m, _ = m.openBranchCompare("feat/x", "main")
	files := []model.CommitFile{{Status: "M", Path: "a.txt"}, {Status: "M", Path: "b.txt"}}
	mm, _ := m.Update(compareFilesMsg{tag: m.compareTag, files: files})
	m = mm.(Model)
	origins := model.CompareOrigins{APaths: map[string]bool{"a.txt": true}, BPaths: map[string]bool{"b.txt": true}}
	mm, _ = m.Update(compareOriginsMsg{tag: m.compareTag, origins: origins})
	m = mm.(Model)
	mm, _ = m.Update(keyMsg("f"))
	m = mm.(Model)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "a.txt") || strings.Contains(out, "b.txt") {
		t.Fatalf("left-only render should list a.txt and not b.txt:\n%s", out)
	}
	if !strings.Contains(out, "[f] filter") {
		t.Fatalf("footer hint missing [f] filter:\n%s", out)
	}
}
```

Add the needed imports to the test file: `fmt`, `tea "github.com/charmbracelet/bubbletea"`, `github.com/charmbracelet/x/ansi`, `github.com/homeend/gigagit/internal/domain`. (`loadedModel` lives in `nav_test.go`; `keyMsg` in `model_test.go` — both already in this package.)

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/tui/ -run 'TestFilterCompareFiles|TestFKey|TestBranchCompareRenders' -v
```
Expected: compile error — `filterCompareFiles`, `cycleCompareScope` undefined; `f` key not handled.

- [ ] **Step 3: Implement the filter logic**

Append to `internal/tui/branch_compare.go`:

```go
// pathSet returns the active scope's origin set; nil means "no filtering"
// (compareScopeAll).
func (p *comparePairState) pathSet() map[string]bool {
	switch p.scope {
	case compareScopeLeft:
		return p.origins.APaths
	case compareScopeRight:
		return p.origins.BPaths
	}
	return nil
}

// filterCompareFiles keeps the rows whose path (or rename old-path) is in
// set; a nil set keeps everything.
func filterCompareFiles(files []model.CommitFile, set map[string]bool) []model.CommitFile {
	if set == nil {
		return files
	}
	out := make([]model.CommitFile, 0, len(files))
	for _, f := range files {
		if set[f.Path] || (f.OldPath != "" && set[f.OldPath]) {
			out = append(out, f)
		}
	}
	return out
}

// cycleCompareScope is the f key: advance the origin-filter scope and rebuild
// the tree rows from the retained raw list. Origins not usable yet: a status
// note, scope unchanged.
func (m Model) cycleCompareScope() Model {
	p := m.comparePair
	if p == nil {
		return m // not a branch-pair compare: f is inert
	}
	if p.originsErr != nil {
		if errors.Is(p.originsErr, domain.ErrNoMergeBase) {
			m.statusMsg = "no common ancestor — filter unavailable"
		} else {
			m.statusMsg = "origin filter unavailable: " + p.originsErr.Error()
		}
		return m
	}
	if !p.originsLoaded {
		m.statusMsg = "origin filter loading…"
		return m
	}
	p.scope = (p.scope + 1) % 3
	m.filesView.lines = commitFileLines(filterCompareFiles(p.files, p.pathSet()))
	m.filesView.sel = 0
	m.filesTitle = branchCompareTitle(p.left, p.right, p.scope)
	return m
}
```

Extend the file's imports with `errors` and `github.com/homeend/gigagit/internal/domain`.

- [ ] **Step 4: Wire the key and the hint**

a) In `internal/tui/files_view.go`, `updateFilesViewKey`, add after the `case "b":` block:

```go
	case "f": // branch-pair compare: cycle the origin filter (all / left / right)
		if !m.inCompareMode() || m.comparePair == nil {
			return m, nil
		}
		return m.cycleCompareScope(), nil
```

b) In the same file's tree-render hint line (~748), make the hint conditional:

```go
	hint := "[enter] diff  [h] history  [b] blame  [/] search  [esc] close"
	if m.comparePair != nil {
		hint = "[enter] diff  [f] filter  [h] history  [b] blame  [/] search  [esc] close"
	}
```

- [ ] **Step 5: Update help.go**

a) Branches section, line 27 — replace:

```go
		r("m", "mark a row; m on a second row opens the pair-op picker"),
```
with:
```go
		r("m", "mark a row; m on a second row opens the pair-op picker (merge / rebase / interactive rebase / compare branches)"),
```

b) "Commit files view (l)" section — add after the `r("b", "blame of the selected file (tree side)")` row:

```go
		r("f", "branch-pair compare only: cycle the origin filter — all differences / only files the marked branch changed / only files the selected branch changed (computed from the merge base; the diff shown per file is always the tip-to-tip difference)"),
```

- [ ] **Step 6: Run the tests, vet, build, package suite**

```bash
go test ./internal/tui/ -run 'TestFilterCompareFiles|TestFKey|TestBranchCompareRenders' -v
go vet ./internal/tui/
go build ./cmd/gg
go test ./internal/tui/
```
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/branch_compare.go internal/tui/branch_compare_test.go internal/tui/files_view.go internal/tui/help.go
git commit -m "feat(tui): f-key origin filter for branch-pair compares"
```

---

### Task 4: Docs

**Files:**
- Modify: `CHANGELOG.md` (top, under the current unreleased/latest heading pattern — follow the existing entry style)
- Modify: `README.md` (the Branches-panel / compare feature area — follow the surrounding style)
- Modify: `CLAUDE.md` (package-map rows for `domain` and `tui`)

**Interfaces:**
- Consumes: the shipped behavior from Tasks 1–3. No code.

- [ ] **Step 1: CHANGELOG.md**

Add an entry (match the file's existing format — read the top entries first):

```markdown
- **Compare branches (Branches panel)**: mark a branch with `m`, `m` on a second —
  the pair-op picker now offers *Compare A ↔ B*: the full tip-to-tip diff in the
  compare files view (full branch names in the title). `f` cycles an origin
  filter — all differences / only files A changed / only files B changed —
  computed from the merge base (`no common ancestor` disables it). TUI-only;
  `gg compare A B` already covers the CLI.
```

- [ ] **Step 2: README.md**

Find the section describing the Branches panel pair operations (search for "pair-op" or "Merge marked"); extend it with one sentence:

```markdown
Marking two branches also offers **Compare A ↔ B** — the whole-tree diff between
the tips, with an `f`-key filter to show only the files either branch changed
since they diverged.
```

- [ ] **Step 3: CLAUDE.md**

In the package map:
- `domain` row: append after the `ConflictFileVersions` sentence: `` `CompareOrigins(ctx, a, b)` — per-branch changed-path sets from the merge base (`MergeBase` + two `M..tip` numstat path lists; `ErrNoMergeBase` when histories are unrelated), backing the TUI branch-compare origin filter. ``
- `tui` row: append a sentence: `` **Compare branches** (`branch_compare.go`): 4th Branches pair-op row "Compare A ↔ B" → `openBranchCompare` (full tip-to-tip compare view, full-name title override, async `CompareOrigins` load gen-gated by `compareTag`); `f` cycles the origin filter (all / left-only / right-only) rebuilding rows from the retained raw list; `comparePairState` cleared in `closeFilesView`. ``

- [ ] **Step 4: Full suite + commit**

```bash
./test.sh
git add CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: compare-branches feature (CHANGELOG, README, CLAUDE.md)"
```
Expected: all stages green (vet+gofmt → unit → e2e).

---

## Self-Review Notes

- **Spec coverage:** pair-op row + open (T2); tip-to-tip semantics via existing `openCompareFiles` (T2); full-name title (T2); `f` cycle + filtering + notes (T3); origin sets incl. rename both-sides (T1); `ErrNoMergeBase` sentinel (T1); non-branch compares untouched (T3 inert test); hint + help (T3); no engine/CLI/config change (no task touches them); docs (T4). Testing section of the spec maps 1:1 onto the task tests.
- **Type consistency:** `comparePairState` fields, `compareScope` constants, `branchCompareTitle`, `filterCompareFiles`, `cycleCompareScope`, `compareOriginsMsg` are used with identical names/signatures across T2/T3.
- **Known judgment calls encoded above:** any `MergeBase` error maps to `ErrNoMergeBase` (refs come from the branches list, so bad-ref is not a real case); `f` with a failed origins load re-shows the note rather than retrying (re-open the compare to retry); the raw-list retention happens in the generic `compareFilesMsg` handler but only when `comparePair != nil`.
