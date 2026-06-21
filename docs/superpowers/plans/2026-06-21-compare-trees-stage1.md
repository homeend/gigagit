# Compare Trees — Stage 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add whole-tree comparison between two endpoints (commit / index / working tree), and ship the headline slice: a Commits-panel `.`-menu "Compare against working tree / staged" that opens the files view as a commit-vs-working-tree (or vs-staged) diff.

**Architecture:** A comparison is a `(left, right)` pair of `model.Endpoint`. A new git verb `DiffTreeFiles` yields the changed-file list; per-file diffs reuse the existing `ResolveBytes` + `Differ`. The TUI files view gains a parallel "compare mode" (endpoint pair) alongside its existing commit-vs-parent mode — the proven single-commit path (`CommitFiles`, `-m --first-parent`) is left untouched.

**Tech Stack:** Go 1.26, Bubble Tea TUI, shells out to system `git` via `gitcmd`/`gitexec`.

## Global Constraints

- **A git verb is one invocation.** Build argv with `gitcmd`, run via `r.Runner.Run`. Doc comment names the git command.
- **Frontends reach git only through `internal/domain`** (archtest-guarded). The TUI calls `svc.CompareFiles` / `svc.ResolveBytes`, never `internal/git`.
- **TUI `Model` is a value receiver** with pointer fields; mutate the copy and return it.
- **TDD with a real `git`** in `t.TempDir()` (`newTestRepo`) or `FakeRunner`. Follow red→green per step.
- **Live endpoints are never cached.** A per-file diff whose left or right is WorkTree or Index must pass `Request.Key = ""`; only commit↔commit is cached.
- **Verify test exit explicitly** — never `go test … | tail && commit` (the pipe masks the exit code).
- **Do not break the existing single-commit files view** — it uses `CommitFiles` (`-m --first-parent`), not `DiffTreeFiles`.
- Branch: `compare-trees` (already the worktree branch). The human merges.

---

### Task 1: `model.Endpoint`

**Files:**
- Modify: `internal/model/model.go` (add after the `FileRef` block, ~line 167)
- Test: `internal/model/endpoint_test.go` (new — note: filename must NOT end in a GOOS/GOARCH token before `_test.go`)

**Interfaces:**
- Produces:
  - `model.EndpointKind` with consts `EndpointWorkTree`, `EndpointIndex`, `EndpointCommit`
  - `model.Endpoint{Kind EndpointKind; Hash string}`
  - `(Endpoint) Display() string`
  - `(Endpoint) FileRef(path string) FileRef`
  - `(Endpoint) IsLive() bool`
  - `(Endpoint) CacheTag() string`

- [ ] **Step 1: Write the failing test**

Create `internal/model/endpoint_test.go`:

```go
package model

import "testing"

func TestEndpointDisplay(t *testing.T) {
	cases := []struct {
		e    Endpoint
		want string
	}{
		{Endpoint{Kind: EndpointWorkTree}, "Working Tree"},
		{Endpoint{Kind: EndpointIndex}, "Staged"},
		{Endpoint{Kind: EndpointCommit, Hash: "0123456789abcdef"}, "0123456"},
		{Endpoint{Kind: EndpointCommit, Hash: "abc"}, "abc"},
	}
	for _, c := range cases {
		if got := c.e.Display(); got != c.want {
			t.Errorf("Display(%+v) = %q, want %q", c.e, got, c.want)
		}
	}
}

func TestEndpointFileRef(t *testing.T) {
	if got := (Endpoint{Kind: EndpointWorkTree}).FileRef("a.go"); got != (FileRef{Source: SourceUnstaged, Path: "a.go"}) {
		t.Errorf("worktree FileRef = %+v", got)
	}
	if got := (Endpoint{Kind: EndpointIndex}).FileRef("a.go"); got != (FileRef{Source: SourceStaged, Path: "a.go"}) {
		t.Errorf("index FileRef = %+v", got)
	}
	if got := (Endpoint{Kind: EndpointCommit, Hash: "deadbeef"}).FileRef("a.go"); got != (FileRef{Source: SourceCommit, Locator: "deadbeef", Path: "a.go"}) {
		t.Errorf("commit FileRef = %+v", got)
	}
}

func TestEndpointIsLiveAndCacheTag(t *testing.T) {
	if !(Endpoint{Kind: EndpointWorkTree}).IsLive() || !(Endpoint{Kind: EndpointIndex}).IsLive() {
		t.Error("worktree/index must be live")
	}
	if (Endpoint{Kind: EndpointCommit, Hash: "x"}).IsLive() {
		t.Error("commit must not be live")
	}
	if got := (Endpoint{Kind: EndpointCommit, Hash: "x"}).CacheTag(); got != "x" {
		t.Errorf("commit CacheTag = %q", got)
	}
	if got := (Endpoint{Kind: EndpointWorkTree}).CacheTag(); got != "worktree" {
		t.Errorf("worktree CacheTag = %q", got)
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `cd /mnt/t/others/gg-compare && go test ./internal/model/ -run TestEndpoint -v`
Expected: FAIL — `undefined: Endpoint` / `EndpointWorkTree`.

- [ ] **Step 3: Implement**

In `internal/model/model.go`, after the `FileRef` struct (~line 167) add:

```go
// EndpointKind names the kind of a comparison Endpoint.
type EndpointKind int

const (
	EndpointWorkTree EndpointKind = iota // the working tree (unstaged)
	EndpointIndex                        // the index (staged)
	EndpointCommit                       // a commit, by Hash
)

// Endpoint names one side of a whole-tree comparison.
type Endpoint struct {
	Kind EndpointKind
	Hash string // commit hash when Kind == EndpointCommit; "" otherwise
}

// Display is the human label for an endpoint.
func (e Endpoint) Display() string {
	switch e.Kind {
	case EndpointWorkTree:
		return "Working Tree"
	case EndpointIndex:
		return "Staged"
	default:
		if len(e.Hash) > 7 {
			return e.Hash[:7]
		}
		return e.Hash
	}
}

// FileRef maps the endpoint to a resolvable file reference for path.
func (e Endpoint) FileRef(path string) FileRef {
	switch e.Kind {
	case EndpointWorkTree:
		return FileRef{Source: SourceUnstaged, Path: path}
	case EndpointIndex:
		return FileRef{Source: SourceStaged, Path: path}
	default:
		return FileRef{Source: SourceCommit, Locator: e.Hash, Path: path}
	}
}

// IsLive reports whether the endpoint's content can change on disk (working
// tree or index) and therefore must never be cached.
func (e Endpoint) IsLive() bool {
	return e.Kind == EndpointWorkTree || e.Kind == EndpointIndex
}

// CacheTag is a stable cache-key fragment for the endpoint (only meaningful
// when !IsLive()).
func (e Endpoint) CacheTag() string {
	switch e.Kind {
	case EndpointWorkTree:
		return "worktree"
	case EndpointIndex:
		return "index"
	default:
		return e.Hash
	}
}
```

- [ ] **Step 4: Run the test, verify it passes**

Run: `cd /mnt/t/others/gg-compare && go test ./internal/model/ -run TestEndpoint -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-compare
git add internal/model/model.go internal/model/endpoint_test.go
git commit -m "feat(model): Endpoint — one side of a whole-tree comparison

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 2: `git.DiffTreeFiles` verb

**Files:**
- Create: `internal/git/compare.go`
- Test: `internal/git/compare_test.go`

**Interfaces:**
- Consumes: `model.Endpoint`, `model.EndpointKind` consts (Task 1); existing `ParseNameStatus`, `gitcmd.New`, `r.Runner.Run`.
- Produces: `func (r *Repo) DiffTreeFiles(ctx context.Context, left, right model.Endpoint) ([]model.CommitFile, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/git/compare_test.go`:

```go
package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// setStr returns the set of "<status> <path>" strings for easy comparison.
func setStr(files []model.CommitFile) map[string]bool {
	m := map[string]bool{}
	for _, f := range files {
		m[f.Status+" "+f.Path] = true
	}
	return m
}

func TestDiffTreeFilesAllForwardForms(t *testing.T) {
	dir, runner := newTestRepo(t) // one commit: README.md
	repo := &Repo{Runner: runner}
	ctx := context.Background()
	git := func(args ...string) { gitRun(t, dir, args...) }

	// commit A (initial) hash
	a := revParse(t, dir, "HEAD")

	// second commit B: modify README, add b.txt
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-m", "B")
	b := revParse(t, dir, "HEAD")

	// stage a change to README, leave an unstaged change to b.txt
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "README.md")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("worktree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	commit := func(h string) model.Endpoint { return model.Endpoint{Kind: model.EndpointCommit, Hash: h} }
	index := model.Endpoint{Kind: model.EndpointIndex}
	work := model.Endpoint{Kind: model.EndpointWorkTree}

	// commit A → commit B: README modified, b.txt added
	got, err := repo.DiffTreeFiles(ctx, commit(a), commit(b))
	if err != nil {
		t.Fatal(err)
	}
	s := setStr(got)
	if !s["M README.md"] || !s["A b.txt"] {
		t.Errorf("A→B = %v", s)
	}

	// commit B → index: README staged-modified
	got, err = repo.DiffTreeFiles(ctx, commit(b), index)
	if err != nil {
		t.Fatal(err)
	}
	if !setStr(got)["M README.md"] {
		t.Errorf("B→index = %v", setStr(got))
	}

	// commit B → worktree: README + b.txt both differ from B
	got, err = repo.DiffTreeFiles(ctx, commit(b), work)
	if err != nil {
		t.Fatal(err)
	}
	s = setStr(got)
	if !s["M README.md"] || !s["M b.txt"] {
		t.Errorf("B→worktree = %v", s)
	}

	// index → worktree: only b.txt is unstaged
	got, err = repo.DiffTreeFiles(ctx, index, work)
	if err != nil {
		t.Fatal(err)
	}
	s = setStr(got)
	if !s["M b.txt"] || s["M README.md"] {
		t.Errorf("index→worktree = %v (README should be absent — it's staged)", s)
	}
}

func TestDiffTreeFilesRejectsReversePair(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	_, err := repo.DiffTreeFiles(context.Background(),
		model.Endpoint{Kind: model.EndpointWorkTree},
		model.Endpoint{Kind: model.EndpointCommit, Hash: "HEAD"})
	if err == nil {
		t.Fatal("worktree→commit (reverse) must error")
	}
}
```

Also add these test helpers to `internal/git/compare_test.go` (they wrap `git` for the test):

```go
import "os/exec" // add to the import block above

func gitRun(t *testing.T, dir string, args ...string) {
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

func revParse(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return string(out[:len(out)-1]) // strip trailing newline
}
```

(Place the imports correctly in one `import (...)` block: `context`, `os`, `os/exec`, `path/filepath`, `testing`, and the model package.)

- [ ] **Step 2: Run the test, verify it fails**

Run: `cd /mnt/t/others/gg-compare && go test ./internal/git/ -run TestDiffTreeFiles -v`
Expected: FAIL — `repo.DiffTreeFiles undefined`.

- [ ] **Step 3: Implement**

Create `internal/git/compare.go`:

```go
package git

import (
	"context"
	"fmt"

	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/model"
)

// DiffTreeFiles lists the files that differ between two endpoints, with left as
// the older side and right the newer. It supports only the four forward kind
// pairs the UI ever produces; any other pair is a programming error.
//
//	commit A → commit B : git diff --name-status -M A B
//	commit A → index    : git diff --cached --name-status -M A
//	commit A → worktree : git diff --name-status -M A
//	index   → worktree  : git diff --name-status -M
func (r *Repo) DiffTreeFiles(ctx context.Context, left, right model.Endpoint) ([]model.CommitFile, error) {
	b := gitcmd.New("diff").Arg("--name-status", "-M")
	switch {
	case left.Kind == model.EndpointCommit && right.Kind == model.EndpointCommit:
		b = b.Arg(left.Hash, right.Hash)
	case left.Kind == model.EndpointCommit && right.Kind == model.EndpointIndex:
		b = b.Arg("--cached", left.Hash)
	case left.Kind == model.EndpointCommit && right.Kind == model.EndpointWorkTree:
		b = b.Arg(left.Hash)
	case left.Kind == model.EndpointIndex && right.Kind == model.EndpointWorkTree:
		// bare `git diff` already compares index → working tree.
	default:
		return nil, fmt.Errorf("DiffTreeFiles: unsupported endpoint pair %d → %d", left.Kind, right.Kind)
	}
	res, err := r.Runner.Run(ctx, "git diff (compare files)", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return ParseNameStatus([]byte(res.Stdout)), nil
}
```

- [ ] **Step 4: Run the test, verify it passes**

Run: `cd /mnt/t/others/gg-compare && go test ./internal/git/ -run TestDiffTreeFiles -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-compare
git add internal/git/compare.go internal/git/compare_test.go
git commit -m "feat(git): DiffTreeFiles — changed files between two endpoints

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 3: `domain.CompareFiles` query

**Files:**
- Modify: `internal/domain/query.go` (add after `CommitFiles`, ~line 219)
- Test: `internal/domain/compare_test.go`

**Interfaces:**
- Consumes: `(*git.Repo).DiffTreeFiles` (Task 2), the existing `query(ctx, s, key, fn)` Read-reservation helper.
- Produces: `func (s *Service) CompareFiles(ctx context.Context, left, right model.Endpoint) ([]model.CommitFile, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/domain/compare_test.go`. First inspect an existing domain test (e.g. `internal/domain/query_test.go`) for the repo-building helper it uses (commonly `newRepo(t)` returning a `*Service` over a temp git repo). Use that same helper; the test below assumes a helper `newServiceRepo(t) (*Service, string)` that returns a Service and the repo dir — **if the existing helper has a different name/shape, adapt the first two lines only.**

```go
package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestCompareFilesCommitVsWorktree(t *testing.T) {
	svc, dir := newServiceRepo(t) // ADAPT to the existing domain test helper
	ctx := context.Background()

	head := revParseDir(t, dir, "HEAD")
	// dirty the working tree
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := svc.CompareFiles(ctx,
		model.Endpoint{Kind: model.EndpointCommit, Hash: head},
		model.Endpoint{Kind: model.EndpointWorkTree})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f.Path == "README.md" && f.Status == "M" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected README.md modified, got %+v", files)
	}
}

func revParseDir(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return string(out[:len(out)-1])
}
```

> If no `newServiceRepo`-style helper exists, build the Service inline the way the nearest existing domain test does (it already constructs a `*git.Repo` over a `t.TempDir()` and calls `domain.New(repo)`), and have that setup write+commit a `README.md` so `HEAD` exists.

- [ ] **Step 2: Run the test, verify it fails**

Run: `cd /mnt/t/others/gg-compare && go test ./internal/domain/ -run TestCompareFiles -v`
Expected: FAIL — `svc.CompareFiles undefined`.

- [ ] **Step 3: Implement**

In `internal/domain/query.go`, after `CommitFiles` (~line 219) add:

```go
// CompareFiles returns the files that differ between two endpoints (left =
// older, right = newer), under a Read reservation. Not coalesced: live
// endpoints (working tree / index) change underfoot, so each call re-reads.
func (s *Service) CompareFiles(ctx context.Context, left, right model.Endpoint) ([]model.CommitFile, error) {
	return query(ctx, s, "compare-files:"+left.CacheTag()+":"+right.CacheTag(), func(ctx context.Context) ([]model.CommitFile, error) {
		return s.repo.DiffTreeFiles(ctx, left, right)
	})
}
```

> Note: `query` here is the existing Read-reservation helper used by `CommitFiles`. The singleflight key includes both endpoints; that is acceptable for stage 1 (a brief in-flight coalesce). If a later stage needs strict freshness for live endpoints, switch to the non-coalescing read primitive then — out of scope now.

- [ ] **Step 4: Run the test, verify it passes**

Run: `cd /mnt/t/others/gg-compare && go test ./internal/domain/ -run TestCompareFiles -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-compare
git add internal/domain/query.go internal/domain/compare_test.go
git commit -m "feat(domain): CompareFiles query over two endpoints

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 4: TUI compare mode — open + file list

**Files:**
- Modify: `internal/tui/model.go` (Model fields ~line 55–57; `compareFilesMsg` handler near the `commitFilesMsg` handler ~line 185; reset on close)
- Modify: `internal/tui/files_view.go` (add `openCompareFiles`, `loadCompareFilesCmd`, `compareFilesMsg`; reset compare state in the esc/`l` close handlers ~line 158 and ~line 165)
- Test: `internal/tui/compare_view_test.go`

**Interfaces:**
- Consumes: `svc.CompareFiles` (Task 3); `model.Endpoint`; existing `contentPopup`, `commitFileLines`.
- Produces (used by Tasks 5–6):
  - Model fields `filesCompare bool`, `filesLeft model.Endpoint`, `filesRight model.Endpoint`, `compareTag string`
  - `func (m Model) openCompareFiles(left, right model.Endpoint) (Model, tea.Cmd)`
  - `type compareFilesMsg struct{ tag string; files []model.CommitFile; err error }`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/compare_view_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestOpenCompareFilesPopulatesView(t *testing.T) {
	m := loadedModel(t)
	if len(m.commits) == 0 {
		t.Skip("no commits loaded")
	}
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[0].Hash}
	right := model.Endpoint{Kind: model.EndpointWorkTree}

	m2, cmd := m.openCompareFiles(left, right)
	if !m2.filesCompare || m2.filesView == nil {
		t.Fatal("compare mode + filesView must be set")
	}
	if m2.filesTitle != left.Display()+" ↔ "+right.Display() {
		t.Errorf("title = %q", m2.filesTitle)
	}
	if cmd == nil {
		t.Fatal("expected a load command")
	}
	// drive the async load
	msg := cmd()
	cm, ok := msg.(compareFilesMsg)
	if !ok {
		t.Fatalf("expected compareFilesMsg, got %T", msg)
	}
	if cm.err != nil {
		t.Fatalf("load err: %v", cm.err)
	}
	m3, _ := m2.Update(cm)
	mm := m3.(Model)
	if len(mm.filesView.lines) == 0 || mm.filesView.lines[0].text == "(loading…)" {
		t.Errorf("file list not applied: %+v", mm.filesView.lines)
	}
}

func TestCompareFilesMsgStaleDropped(t *testing.T) {
	m := loadedModel(t)
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: "abc"}
	m2, _ := m.openCompareFiles(left, model.Endpoint{Kind: model.EndpointWorkTree})
	before := len(m2.filesView.lines)
	m3, _ := m2.Update(compareFilesMsg{tag: "stale", files: nil})
	if got := len(m3.(Model).filesView.lines); got != before {
		t.Errorf("stale msg must not mutate the view (%d → %d)", before, got)
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `cd /mnt/t/others/gg-compare && go test ./internal/tui/ -run TestOpenCompareFiles -v`
Expected: FAIL — `m.openCompareFiles undefined`, `compareFilesMsg undefined`, `filesCompare undefined`.

- [ ] **Step 3a: Add Model fields**

In `internal/tui/model.go`, alongside the files-view fields (~line 55–57), after `filesHash string`:

```go
	filesCompare bool           // true = compare mode (filesLeft/Right) vs legacy commit-vs-parent
	filesLeft    model.Endpoint // compare mode: older side
	filesRight   model.Endpoint // compare mode: newer side
	compareTag   string         // gates stale compareFilesMsg results
```

(Confirm `internal/tui/model.go` already imports `github.com/gigagit/gg/internal/model`; it does — `model.Commit` is used.)

- [ ] **Step 3b: Add `openCompareFiles` + `loadCompareFilesCmd` + msg type**

In `internal/tui/files_view.go`, add near `loadCommitFilesCmd` (~line 70):

```go
// compareFilesMsg carries a whole-tree comparison's changed files, tagged so a
// superseded load (fast re-open) can be dropped.
type compareFilesMsg struct {
	tag   string
	files []model.CommitFile
	err   error
}

// openCompareFiles opens the files view in compare mode for the endpoint pair
// (left = older, right = newer), e.g. a commit vs the working tree. The proven
// single-commit path is untouched; this is a parallel mode keyed off
// filesCompare.
func (m Model) openCompareFiles(left, right model.Endpoint) (Model, tea.Cmd) {
	m.filesView = &contentPopup{lines: []contentLine{{text: "(loading…)"}}}
	m.filesTitle = left.Display() + " ↔ " + right.Display()
	m.filesCompare = true
	m.filesLeft = left
	m.filesRight = right
	// h/b (history/blame) context: prefer a commit side; "" means working tree.
	switch {
	case right.Kind == model.EndpointCommit:
		m.filesHash = right.Hash
	case left.Kind == model.EndpointCommit:
		m.filesHash = left.Hash
	default:
		m.filesHash = ""
	}
	m.compareTag = "cmp:" + left.CacheTag() + ":" + right.CacheTag()
	m.filesTreeFocused = false
	return m, m.loadCompareFilesCmd(left, right, m.compareTag)
}

// loadCompareFilesCmd fetches the changed-file list off the UI thread.
func (m Model) loadCompareFilesCmd(left, right model.Endpoint, tag string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		files, err := svc.CompareFiles(context.Background(), left, right)
		return compareFilesMsg{tag: tag, files: files, err: err}
	}
}
```

- [ ] **Step 3c: Handle `compareFilesMsg`**

In `internal/tui/model.go`, add a case in `Update` near the `commitFilesMsg` handler (~line 185). Mirror its stale-guard shape:

```go
	case compareFilesMsg:
		if m.filesView == nil || !m.filesCompare || msg.tag != m.compareTag {
			return m, nil // stale or closed
		}
		if msg.err != nil {
			if len(m.filesView.lines) == 1 && m.filesView.lines[0].text == "(loading…)" {
				m.filesView.lines = []contentLine{{text: "(load failed)"}}
			}
			return m, nil
		}
		m.filesView.lines = commitFileLines(msg.files)
		m.filesView.sel = 0
		return m, nil
```

- [ ] **Step 3d: Reset compare state on close**

In `internal/tui/files_view.go`, the esc handler (~line 158, after `m.filesView = nil`) and the `l` handler (~line 165, after `m.filesView = nil`) each gain:

```go
		m.filesCompare = false
		m.compareTag = ""
```

Also in `internal/tui/model.go` where the repo switch clears the files view (~line 1336, `m.filesView = nil`), add `m.filesCompare = false`.

- [ ] **Step 4: Run the test, verify it passes**

Run: `cd /mnt/t/others/gg-compare && go test ./internal/tui/ -run 'TestOpenCompareFiles|TestCompareFilesMsgStale' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-compare
git add internal/tui/model.go internal/tui/files_view.go internal/tui/compare_view_test.go
git commit -m "feat(tui): files-view compare mode — open + changed-file list

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 5: TUI compare mode — per-file diff (cache-correct)

**Files:**
- Modify: `internal/tui/files_view.go` (the `enter` per-file case ~line 223–233 — branch on `filesCompare`)
- Modify: `internal/tui/diff_view.go` (add `loadCompareDiffCmd` near `loadCommitDiffCmd` ~line 409)
- Test: `internal/tui/compare_diff_test.go`

**Interfaces:**
- Consumes: `m.filesLeft/filesRight` (Task 4); `svc.ResolveBytes`; `m.diffDiffer()`; `domain.Request`, `domain.ByteSource`; `(Endpoint).IsLive/CacheTag/FileRef`.
- Produces: `func (m Model) loadCompareDiffCmd(left, right model.Endpoint, line contentLine) tea.Cmd`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/compare_diff_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// compareDiffKey is exposed for the test to assert the cache-bypass rule.
func TestCompareDiffCacheKeyRule(t *testing.T) {
	commit := model.Endpoint{Kind: model.EndpointCommit, Hash: "aaa"}
	commit2 := model.Endpoint{Kind: model.EndpointCommit, Hash: "bbb"}
	work := model.Endpoint{Kind: model.EndpointWorkTree}

	// commit↔commit → cached (non-empty key)
	if k := compareDiffKey(commit, commit2, "a.go"); k == "" {
		t.Error("commit↔commit must be cached (non-empty key)")
	}
	// any live side → bypass (empty key)
	if k := compareDiffKey(commit, work, "a.go"); k != "" {
		t.Errorf("live endpoint must bypass cache, got key %q", k)
	}
	if k := compareDiffKey(work, commit, "a.go"); k != "" {
		t.Errorf("live endpoint must bypass cache, got key %q", k)
	}
}

func TestCompareEnterOpensDiff(t *testing.T) {
	m := loadedModel(t)
	if len(m.commits) == 0 {
		t.Skip("no commits")
	}
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[0].Hash}
	m, _ = m.openCompareFiles(left, model.Endpoint{Kind: model.EndpointWorkTree})
	// apply the file list synchronously
	m.filesView.lines = []contentLine{{text: "M README.md", path: "README.md", status: "M"}}
	m.filesView.sel = 0
	m.filesTreeFocused = true

	u, cmd := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.diffView == nil {
		t.Fatal("enter in compare mode must open the diff view")
	}
	if !strings.Contains(mm.diffTag, "README.md") {
		t.Errorf("diffTag = %q", mm.diffTag)
	}
	if cmd == nil {
		t.Fatal("expected a diff load command")
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `cd /mnt/t/others/gg-compare && go test ./internal/tui/ -run 'TestCompareDiffCacheKeyRule|TestCompareEnterOpensDiff' -v`
Expected: FAIL — `compareDiffKey undefined`, and `enter` does not open the diff in compare mode.

- [ ] **Step 3a: Add `compareDiffKey` + `loadCompareDiffCmd`**

In `internal/tui/diff_view.go`, after `loadCommitDiffCmd` (~line 439):

```go
// compareDiffKey is the per-file diff cache key for a comparison. It returns ""
// (cache bypass) whenever either side is a live endpoint (working tree/index),
// whose bytes change on disk; commit↔commit is immutable and stays cached.
func compareDiffKey(left, right model.Endpoint, path string) string {
	if left.IsLive() || right.IsLive() {
		return ""
	}
	return left.CacheTag() + ".." + right.CacheTag() + ":" + path
}

// loadCompareDiffCmd computes one file's diff between two endpoints. Each side
// resolves through ResolveBytes (commit / staged / unstaged); a "A" status has
// no old side, a "D" status no new side.
func (m Model) loadCompareDiffCmd(left, right model.Endpoint, line contentLine) tea.Cmd {
	svc := m.svc
	differ := m.diffDiffer()
	body := m.diffBodyRows()
	tag := "cmp:" + left.CacheTag() + ":" + right.CacheTag() + ":" + line.path
	v := m.diffView // already constructed by the caller
	key := compareDiffKey(left, right, line.path)

	oldP := line.path
	if line.oldPath != "" {
		oldP = line.oldPath
	}
	var oldSrc, newSrc domain.ByteSource
	if line.status != "A" {
		ref := left.FileRef(oldP)
		oldSrc = func(ctx context.Context) ([]byte, error) { return svc.ResolveBytes(ctx, ref) }
	}
	if line.status != "D" {
		ref := right.FileRef(line.path)
		newSrc = func(ctx context.Context) ([]byte, error) { return svc.ResolveBytes(ctx, ref) }
	}
	return func() tea.Msg {
		out, err := differ.Diff(context.Background(), domain.Request{Key: key, Old: oldSrc, New: newSrc})
		if err != nil {
			v.err = err
			return diffMsg{tag: tag, view: v}
		}
		applyDiff(v, out, body)
		return diffMsg{tag: tag, view: v}
	}
}
```

(`internal/tui/diff_view.go` already imports `context`, `domain`, `model` — verify the import block; `model` is used by `loadCompareDiffCmd`. If `model` is not yet imported there, add `"github.com/gigagit/gg/internal/model"`.)

- [ ] **Step 3b: Branch the `enter` per-file case on compare mode**

In `internal/tui/files_view.go`, the `enter` case (~line 223–233), replace the diff-open block with a compare-aware version:

```go
		l := vis[p.sel]
		m.diffView = &diffView{
			title:   l.path,
			context: m.filesTitle,
			rev:     m.filesHash,
			loading: true,
			partial: m.diffPartial,
			long:    m.diffLong,
		}
		if m.filesCompare {
			m.diffTag = "cmp:" + m.filesLeft.CacheTag() + ":" + m.filesRight.CacheTag() + ":" + l.path
			return m, m.loadCompareDiffCmd(m.filesLeft, m.filesRight, l)
		}
		m.diffView.context = "@ " + strings.TrimPrefix(m.filesTitle, "Files ")
		m.diffTag = "commit:" + m.filesHash + ":" + l.path
		return m, m.loadCommitDiffCmd(m.filesHash, l)
```

(This preserves the legacy commit path's `context`/`diffTag`/loader exactly; compare mode uses the new loader.)

- [ ] **Step 4: Run the tests, verify they pass**

Run: `cd /mnt/t/others/gg-compare && go test ./internal/tui/ -run 'TestCompareDiffCacheKeyRule|TestCompareEnterOpensDiff' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-compare
git add internal/tui/files_view.go internal/tui/diff_view.go internal/tui/compare_diff_test.go
git commit -m "feat(tui): per-file compare diff — cache-bypass for live endpoints

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 6: Context-menu rows + help

**Files:**
- Modify: `internal/tui/action_menu.go` (new row funcs + register in the `panelCommits` block ~line 115)
- Modify: `internal/tui/help.go` (advertise the two menu actions)
- Test: `internal/tui/compare_menu_test.go`

**Interfaces:**
- Consumes: `m.backingIndex(panelCommits)`, `m.commits`, `m.openCompareFiles` (Task 4), `actionRow`.
- Produces: `commitCompareWorktreeRow`, `commitCompareStagedRow` (both `(actionRow, bool)`).

- [ ] **Step 1: Write the failing test**

Create `internal/tui/compare_menu_test.go`:

```go
package tui

import "testing"

func TestCommitCompareRowsPresentOnCommit(t *testing.T) {
	m := loadedModel(t)
	if len(m.commits) == 0 {
		t.Skip("no commits")
	}
	m.focus = panelCommits

	if _, ok := m.commitCompareWorktreeRow(); !ok {
		t.Error("Compare against working tree row must be available on a commit")
	}
	if _, ok := m.commitCompareStagedRow(); !ok {
		t.Error("Compare against staged row must be available on a commit")
	}

	// run the worktree row → opens compare mode
	r, _ := m.commitCompareWorktreeRow()
	u, cmd := r.run(m)
	if !u.(Model).filesCompare {
		t.Error("running the row must enter compare mode")
	}
	if cmd == nil {
		t.Error("running the row must kick off the file-list load")
	}
}

func TestCommitCompareRowsAbsentOffCommits(t *testing.T) {
	m := loadedModel(t)
	m.focus = panelBranches
	if _, ok := m.commitCompareWorktreeRow(); ok {
		t.Error("compare row must not appear off the Commits panel")
	}
}
```

- [ ] **Step 2: Run the test, verify it fails**

Run: `cd /mnt/t/others/gg-compare && go test ./internal/tui/ -run TestCommitCompareRows -v`
Expected: FAIL — `m.commitCompareWorktreeRow undefined`.

- [ ] **Step 3a: Add the row funcs**

In `internal/tui/action_menu.go` (anywhere among the commit row funcs, e.g. after `commitCreateTagRow`):

```go
// commitCompareWorktreeRow / commitCompareStagedRow open the files view as a
// whole-tree comparison of the selected commit against the working tree / the
// index. No marking needed — the common "what does my working copy look like
// vs this commit" case.
func (m Model) commitCompareWorktreeRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash
	return actionRow{
		id:    "commit-compare-worktree",
		label: "Compare against working tree",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openCompareFiles(
				model.Endpoint{Kind: model.EndpointCommit, Hash: hash},
				model.Endpoint{Kind: model.EndpointWorkTree})
		},
	}, true
}

func (m Model) commitCompareStagedRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash
	return actionRow{
		id:    "commit-compare-staged",
		label: "Compare against staged",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openCompareFiles(
				model.Endpoint{Kind: model.EndpointCommit, Hash: hash},
				model.Endpoint{Kind: model.EndpointIndex})
		},
	}, true
}
```

(Confirm `internal/tui/action_menu.go` imports `model`; it references `panelCommits` and other tui types — add `"github.com/gigagit/gg/internal/model"` to its import block if not present.)

- [ ] **Step 3b: Register the rows**

In `internal/tui/action_menu.go`, in the commit-context block (after `commitCreateTagRow`, ~line 117):

```go
	if r, ok := m.commitCompareWorktreeRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.commitCompareStagedRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 3c: Advertise in help**

In `internal/tui/help.go`, find the Commits-panel `.`-menu section and add two lines describing **Compare against working tree** and **Compare against staged** (match the surrounding format; these are menu-only, no keybinding — mirror how `commitCreateTagRow`'s action is listed). If the help groups menu actions generically, add a single line: `compare a commit against the working tree or the index (. menu)`.

- [ ] **Step 4: Run the tests, verify they pass**

Run: `cd /mnt/t/others/gg-compare && go test ./internal/tui/ -run TestCommitCompareRows -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-compare
git add internal/tui/action_menu.go internal/tui/help.go internal/tui/compare_menu_test.go
git commit -m "feat(tui): . menu — compare a commit against working tree / staged

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 7: Docs + full gate

**Files:**
- Modify: `CHANGELOG.md`
- (No README/CLAUDE change in stage 1 — no CLI surface yet, package map unchanged.)

- [ ] **Step 1: Update the CHANGELOG**

In `CHANGELOG.md`, under `## [Unreleased]`, add an `### Added` entry (or extend the existing one):

```markdown
- **Compare a commit against your working tree or staged changes.** In the
  Commits panel, the `.` menu now offers *Compare against working tree* and
  *Compare against staged*, opening the files view as a whole-tree diff
  (commit ↔ working copy). First slice of GitKraken-style commit comparison;
  closes commit-ops backlog #2b.
```

- [ ] **Step 2: Format + vet**

Run:
```bash
cd /mnt/t/others/gg-compare
gofmt -l internal/ cmd/
go vet ./...
```
Expected: `gofmt -l` prints nothing; `go vet` exits 0.

- [ ] **Step 3: Full race gate**

Run: `cd /mnt/t/others/gg-compare && ./test.sh race`
Expected: every package `ok`, exit 0. (Do **not** pipe through `tail`; read the final exit status directly.)

- [ ] **Step 4: Manual eyeball (optional but recommended)**

Build and run in a real repo; on a commit press `.` → *Compare against working tree*; confirm the files view header reads `<short-hash> ↔ Working Tree`, the changed-file list is the commit-vs-worktree delta, and `enter` on a file opens its diff.

```bash
cd /mnt/t/others/gg-compare && go build ./cmd/gg
```

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gg-compare
git add CHANGELOG.md
git commit -m "docs(changelog): commit-vs-working-tree/staged comparison

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

## Self-Review

**Spec coverage (stage 1 scope):**
- Core `Endpoint` → Task 1. ✅
- `DiffTreeFiles` verb, four forward forms only → Task 2. ✅
- Per-file content via `ResolveBytes` → Task 5. ✅
- Cache-key rule (bypass for live endpoints) → Task 5 (`compareDiffKey`). ✅
- Generalized files view (parallel compare mode, single-commit path preserved) → Tasks 4–5. ✅
- Entry point A (direct context menu, commit vs working/staged) → Task 6. ✅
- Stage 1 explicitly excludes: mark/compare (stage 2), WIP rows (stage 3), multi-select (stage 4), CLI (stage 5). ✅

**Type consistency:** `model.Endpoint` / `EndpointKind` / `EndpointWorkTree|EndpointIndex|EndpointCommit`, `Display()`, `FileRef()`, `IsLive()`, `CacheTag()` are used identically across Tasks 1–6. `compareFilesMsg{tag,files,err}`, Model fields `filesCompare/filesLeft/filesRight/compareTag`, and `openCompareFiles`/`loadCompareFilesCmd`/`loadCompareDiffCmd`/`compareDiffKey` names match between definition (Tasks 4–5) and use (Tasks 5–6). ✅

**Known adaptation points (flagged inline, not placeholders):**
- Task 3 test helper name (`newServiceRepo`) — adapt to the actual domain test helper.
- Task 5/6 import blocks — add `model` import if a file doesn't already import it.
- Task 6 help.go formatting — match the existing menu-action listing style.
