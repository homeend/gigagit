# Fast-Forward Current Branch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Advance the current branch to a descendant commit selected in the Commits panel (and via `gg fast-forward <commit>`), using `git merge --ff-only`.

**Architecture:** A new `engine.FastForward` operation over a new thin `git.MergeFFOnly` verb, wired to a Commits `.`-menu row and a CLI command (the standard engine→TUI→CLI path). The menu row is gated by a pure in-memory walk of the loaded commit feed's parent DAG (no git call); the op's `IsAncestor` guard is the correctness backstop.

**Tech Stack:** Go 1.26, the existing `gitcmd`/`gitexec`/`engine`/`domain`/`tui`/`cli` layers. No new dependencies.

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26. `internal/tui` and `internal/cli` must NOT import `internal/git` (archtest-guarded) — they reach git via `internal/domain`/`engine`.
- A git verb is ONE invocation built with `gitcmd` and run via `r.Runner.Run`.
- Ops run via `domain.Execute`; frontends never assemble `OpDeps`. `engine.OpName` is reflection-based — no registry to edit for a new op.
- TDD: failing test → watch fail → minimal impl → watch pass → commit. Real `git` in a `t.TempDir()` (`newTestRepo`/`newRepoDir`/`gitIn` helpers) for behavior; `FakeRunner` for argv assertions.
- Commit message footer (every commit):
  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj
  ```
- `IsAncestor(ctx, a, b)` reports "a is an ancestor of b". Fast-forwardable ⇔ `IsAncestor("HEAD", target)` is true (target is ahead of HEAD).
- Run `./test.sh` per task; `./test.sh race` before the final commit.

---

### Task 1: `git.MergeFFOnly` verb + GitOps interface entry

**Files:**
- Modify: `internal/git/merge.go` (add the verb next to `Merge`)
- Modify: `internal/engine/gitops.go` (add to the interface near `Merge`, line ~66)
- Test: `internal/git/merge_test.go`

**Interfaces:**
- Produces: `func (r *Repo) MergeFFOnly(ctx context.Context, dir, commit string) error`; same signature added to the `engine.GitOps` interface.

- [ ] **Step 1: Write the failing test**

Append to `internal/git/merge_test.go` (use the existing `newRepoDir`/`gitIn` helpers; check the file's existing helper names and imports — `os/exec` and `path/filepath` are already used elsewhere in the package):

```go
func TestMergeFFOnly(t *testing.T) {
	dir, repo := newRepoDir(t) // initial commit on the default branch
	ctx := context.Background()

	// main (current) at C0; create feat ahead by one commit, then return to main.
	gitIn(t, dir, "branch", "feat")
	gitIn(t, dir, "switch", "feat")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-m", "ahead")
	featTip := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	gitIn(t, dir, "switch", "-")

	// Fast-forward main to feat's tip.
	if err := repo.MergeFFOnly(ctx, "", featTip); err != nil {
		t.Fatalf("MergeFFOnly: %v", err)
	}
	if got := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD")); got != featTip {
		t.Fatalf("HEAD = %s, want %s (advanced)", got, featTip)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatalf("worktree not updated: %v", err)
	}

	// A non-descendant target must be refused (divergent commit on a new root).
	gitIn(t, dir, "switch", "--orphan", "other")
	os.WriteFile(filepath.Join(dir, "z.txt"), []byte("z\n"), 0o644)
	gitIn(t, dir, "add", "z.txt")
	gitIn(t, dir, "commit", "-m", "orphan")
	other := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	gitIn(t, dir, "switch", "feat")
	if err := repo.MergeFFOnly(ctx, "", other); err == nil {
		t.Fatal("MergeFFOnly to a non-descendant must error")
	}
}
```

If `gitOut` does not already exist in the `git` test package, add this helper near the top of `merge_test.go`:

```go
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}
```

(First grep `internal/git/*_test.go` for an existing `gitOut`/`revParse` helper and reuse it instead of redeclaring.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestMergeFFOnly`
Expected: FAIL — `repo.MergeFFOnly undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/git/merge.go`, add after `Merge`:

```go
// MergeFFOnly fast-forwards the branch checked out at dir ("" = this repo's own
// worktree) to commit. --ff-only refuses (non-zero exit) when commit is not a
// descendant of HEAD; --no-edit keeps it non-interactive. One invocation.
func (r *Repo) MergeFFOnly(ctx context.Context, dir, commit string) error {
	b := gitcmd.New("merge").Arg("--ff-only", "--no-edit", commit)
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git merge --ff-only", b.ToArgv())
	return err
}
```

In `internal/engine/gitops.go`, add to the interface next to `Merge(...)`:

```go
	MergeFFOnly(ctx context.Context, dir, commit string) error
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ -run TestMergeFFOnly && go build ./...`
Expected: PASS and a clean build (confirms `*git.Repo` still satisfies `engine.GitOps`).

- [ ] **Step 5: Commit**

```bash
git add internal/git/merge.go internal/git/merge_test.go internal/engine/gitops.go
git commit -m "feat(git): MergeFFOnly verb (git merge --ff-only)" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

### Task 2: `engine.FastForward` operation

**Files:**
- Create: `internal/engine/fast_forward.go`
- Test: `internal/engine/fast_forward_test.go`

**Interfaces:**
- Consumes: `GitOps.CurrentBranch`, `.RevParse`, `.IsAncestor`, `.MergeFFOnly` (Task 1); `OpDeps`, `Operation`, `Result`, `Progress`, `Done`.
- Produces: `type FastForward struct { Commit string }` implementing `Operation`.

- [ ] **Step 1: Write the failing test**

Create `internal/engine/fast_forward_test.go`. Mirror the real-git setup used by `reset_test.go` (grep it for the exact repo helper — e.g. `newOpRepo`/`newRepoDir` returning a dir + something satisfying `GitOps`, plus a no-op decider). Use that helper; the shape below assumes a `dir, ops := <helper>(t)` returning a `GitOps` and a `nullDecider`/`autoDecider` already present in the package:

```go
func TestFastForwardAdvancesToDescendant(t *testing.T) {
	dir, ops := newEngineRepo(t) // real repo, current branch = main at C0
	ctx := context.Background()

	// feat ahead by one commit; back on main.
	gitIn(t, dir, "branch", "feat")
	gitIn(t, dir, "switch", "feat")
	writeFile(t, dir, "a.txt", "a\n")
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-m", "ahead")
	featTip := revParse(t, dir, "HEAD")
	gitIn(t, dir, "switch", "-")

	res, err := FastForward{Commit: featTip}.Run(ctx, OpDeps{Repo: ops, emit: noEmit, decide: noDecide})
	if err != nil {
		t.Fatalf("FastForward: %v", err)
	}
	if !res.Changed {
		t.Fatal("Changed must be true on a real advance")
	}
	if got := revParse(t, dir, "HEAD"); got != featTip {
		t.Fatalf("HEAD = %s, want %s", got, featTip)
	}
}

func TestFastForwardRefusesNonDescendant(t *testing.T) {
	dir, ops := newEngineRepo(t)
	ctx := context.Background()
	gitIn(t, dir, "switch", "--orphan", "other")
	writeFile(t, dir, "z.txt", "z\n")
	gitIn(t, dir, "add", "z.txt")
	gitIn(t, dir, "commit", "-m", "orphan")
	other := revParse(t, dir, "HEAD")
	gitIn(t, dir, "switch", "main")
	before := revParse(t, dir, "HEAD")

	if _, err := (FastForward{Commit: other}).Run(ctx, OpDeps{Repo: ops, emit: noEmit, decide: noDecide}); err == nil {
		t.Fatal("non-descendant fast-forward must error")
	}
	if revParse(t, dir, "HEAD") != before {
		t.Fatal("HEAD must not move on a refused fast-forward")
	}
}

func TestFastForwardAlreadyUpToDate(t *testing.T) {
	dir, ops := newEngineRepo(t)
	ctx := context.Background()
	head := revParse(t, dir, "HEAD")
	res, err := FastForward{Commit: head}.Run(ctx, OpDeps{Repo: ops, emit: noEmit, decide: noDecide})
	if err != nil {
		t.Fatalf("up-to-date FF: %v", err)
	}
	if res.Changed {
		t.Fatal("Changed must be false when already at the target")
	}
}

func TestFastForwardDetachedHeadErrors(t *testing.T) {
	dir, ops := newEngineRepo(t)
	ctx := context.Background()
	head := revParse(t, dir, "HEAD")
	gitIn(t, dir, "switch", "--detach", head)
	if _, err := (FastForward{Commit: head}).Run(ctx, OpDeps{Repo: ops, emit: noEmit, decide: noDecide}); err == nil {
		t.Fatal("detached HEAD must error")
	}
}
```

> Before writing these, open `internal/engine/reset_test.go` and reuse its exact helpers: the repo constructor, `revParse`, `writeFile`/file helper, `gitIn`, and the no-op `emit`/`decide` values (names may be `noEmit`/`noDecide` or inline funcs). Match them — do NOT invent new helper names if equivalents exist.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestFastForward`
Expected: FAIL — `undefined: FastForward`.

- [ ] **Step 3: Write the implementation**

Create `internal/engine/fast_forward.go`:

```go
package engine

import (
	"context"
	"fmt"
)

// FastForward advances the current branch to Commit when Commit is a descendant
// of HEAD (git merge --ff-only). It never rewrites history and never creates a
// merge commit; it refuses if the target is not strictly ahead. Non-destructive,
// so there is no Decider prompt. The Commits panel is the multi-branch feed, so
// Commit is typically a commit on a child branch built atop the current one.
type FastForward struct {
	Commit string
}

var _ Operation = FastForward{}

func (op FastForward) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Commit == "" {
		return Result{}, fmt.Errorf("fast-forward: Commit is required")
	}

	branch, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	if branch == "" {
		return Result{}, fmt.Errorf("fast-forward needs a checked-out branch (HEAD is detached)")
	}

	target, err := deps.Repo.RevParse(ctx, op.Commit)
	if err != nil {
		return Result{}, fmt.Errorf("fast-forward: %w", err)
	}
	head, err := deps.Repo.RevParse(ctx, "HEAD")
	if err != nil {
		return Result{}, err
	}
	if target == head {
		return Result{Summary: branch + " already up to date", Changed: false}, nil
	}

	ahead, err := deps.Repo.IsAncestor(ctx, "HEAD", target)
	if err != nil {
		return Result{}, err
	}
	if !ahead {
		return Result{}, fmt.Errorf("cannot fast-forward %s: %s is not ahead of it", branch, shortSHA(target))
	}

	deps.emit(ctx, Progress{Step: "fast-forwarding", Detail: branch + " → " + shortSHA(target)})
	if err := deps.Repo.MergeFFOnly(ctx, "", target); err != nil {
		return Result{}, fmt.Errorf("fast-forward %s to %s: %w", branch, shortSHA(target), err)
	}
	res := Result{Summary: "fast-forwarded " + branch + " to " + shortSHA(target), Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

If a `shortSHA`/`short` helper does not already exist in `internal/engine`, add it in this file:

```go
// shortSHA abbreviates a 40-char object id to 7 chars for messages.
func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
```

(First grep `internal/engine/*.go` for an existing `short`/`shortSHA`/`abbrev` and reuse it.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run TestFastForward`
Expected: PASS (all four cases).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/fast_forward.go internal/engine/fast_forward_test.go
git commit -m "feat(engine): FastForward operation (ff-only to a descendant)" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

### Task 3: pure feed-descendant walk helper (TUI gating logic)

**Files:**
- Create: `internal/tui/fast_forward_gate.go`
- Test: `internal/tui/fast_forward_gate_test.go`

**Interfaces:**
- Consumes: `model.Commit` (`Hash`, `Parents`, `UnixTime`).
- Produces: `func feedDescendant(commits []model.Commit, selHash, tipHash string) (descendant, conclusive bool)`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/fast_forward_gate_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// linear C0(t10) <- C1(t20) <- C2(t30), plus a sibling S(t25) off C0.
func gateFeed() []model.Commit {
	return []model.Commit{
		{Hash: "C2", Parents: []string{"C1"}, UnixTime: 30},
		{Hash: "S", Parents: []string{"C0"}, UnixTime: 25},
		{Hash: "C1", Parents: []string{"C0"}, UnixTime: 20},
		{Hash: "C0", Parents: nil, UnixTime: 10},
	}
}

func TestFeedDescendantAhead(t *testing.T) {
	// tip C0, selected C2 → C2 is a descendant of C0.
	d, c := feedDescendant(gateFeed(), "C2", "C0")
	if !d || !c {
		t.Fatalf("C2 vs tip C0: got descendant=%v conclusive=%v, want true,true", d, c)
	}
}

func TestFeedDescendantBehind(t *testing.T) {
	// tip C2, selected C0 → C0 is behind, not ahead.
	d, c := feedDescendant(gateFeed(), "C0", "C2")
	if d || !c {
		t.Fatalf("C0 vs tip C2: got descendant=%v conclusive=%v, want false,true", d, c)
	}
}

func TestFeedDescendantDivergent(t *testing.T) {
	// tip C1, selected sibling S → not a descendant (share only C0).
	d, c := feedDescendant(gateFeed(), "S", "C1")
	if d || !c {
		t.Fatalf("S vs tip C1: got descendant=%v conclusive=%v, want false,true", d, c)
	}
}

func TestFeedDescendantSelfIsTip(t *testing.T) {
	d, c := feedDescendant(gateFeed(), "C1", "C1")
	if d || !c {
		t.Fatalf("tip itself: got descendant=%v conclusive=%v, want false,true", d, c)
	}
}

func TestFeedDescendantTipNotLoaded(t *testing.T) {
	d, c := feedDescendant(gateFeed(), "C2", "MISSING")
	if c {
		t.Fatalf("tip not loaded must be inconclusive, got conclusive=%v", c)
	}
	_ = d
}

func TestFeedDescendantParentOffWindow(t *testing.T) {
	// C2's parent C1 is NOT loaded → the walk from C2 toward tip C0 is inconclusive.
	feed := []model.Commit{
		{Hash: "C2", Parents: []string{"C1"}, UnixTime: 30},
		{Hash: "C0", Parents: nil, UnixTime: 10},
	}
	d, c := feedDescendant(feed, "C2", "C0")
	if c {
		t.Fatalf("parent off-window must be inconclusive, got conclusive=%v (d=%v)", c, d)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestFeedDescendant`
Expected: FAIL — `undefined: feedDescendant`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/fast_forward_gate.go`:

```go
package tui

import "github.com/gigagit/gg/internal/model"

// feedDescendant reports whether selHash is a strict descendant of tipHash using
// ONLY the loaded commit feed's parent pointers (no git call) — the same DAG that
// draws the commit graph. descendant is true only when a parent path from selHash
// reaches tipHash. conclusive is false when the walk leaves the loaded window
// (an unknown parent, or tipHash not loaded), in which case the caller should
// fall back to showing the action and letting the op's IsAncestor guard decide.
//
// Pruning: a descendant of the tip is newer-or-equal in commit time, so any
// parent older than the tip cannot lead back to it and is skipped — this bounds
// the walk to the tip's generation.
func feedDescendant(commits []model.Commit, selHash, tipHash string) (descendant, conclusive bool) {
	if selHash == tipHash {
		return false, true // the tip itself is not "ahead" of itself
	}
	byHash := make(map[string]model.Commit, len(commits))
	for _, c := range commits {
		byHash[c.Hash] = c
	}
	tip, ok := byHash[tipHash]
	if !ok {
		return false, false // tip not in the loaded feed → inconclusive
	}
	if _, ok := byHash[selHash]; !ok {
		return false, false // selected not loaded (shouldn't happen) → inconclusive
	}

	seen := map[string]bool{}
	stack := []string{selHash}
	conclusive = true
	for len(stack) > 0 {
		h := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if h == tipHash {
			return true, true
		}
		if seen[h] {
			continue
		}
		seen[h] = true
		c := byHash[h] // present: selHash is in-map and we only push in-map parents
		for _, p := range c.Parents {
			pc, ok := byHash[p]
			if !ok {
				conclusive = false // ran off the loaded window
				continue
			}
			if pc.UnixTime < tip.UnixTime {
				continue // older than the tip → cannot lead back to it
			}
			stack = append(stack, p)
		}
	}
	return false, conclusive
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestFeedDescendant`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/fast_forward_gate.go internal/tui/fast_forward_gate_test.go
git commit -m "feat(tui): in-memory feed-descendant walk for ff gating" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

### Task 4: Commits `.`-menu row `commitFastForwardRow`

**Files:**
- Modify: `internal/tui/commit_scope.go` (add `commitFastForwardRow`, near `commitResetRow` ~line 547)
- Modify: `internal/tui/action_menu.go` (wire it into `appendCommitContextRows`, near the reset row ~line 206)
- Test: `internal/tui/fast_forward_row_test.go`

**Interfaces:**
- Consumes: `feedDescendant` (Task 3), `engine.FastForward` (Task 2), `m.startOp`, `m.backingIndex(panelCommits)`, `m.commits`, `m.status.Branch`, `commitHasLocalRef(c, name)`.
- Produces: `func (m Model) commitFastForwardRow() (actionRow, bool)`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/fast_forward_row_test.go`. Build a loaded model with the current branch behind a child branch in the feed (reuse the existing loaded-model helper — grep `internal/tui/*_test.go` for `loadedModel`/`twoBranch`/a helper that sets `m.commits`, `m.status.Branch`, `m.focus`, `m.sel`; mirror its construction):

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// ffModel: current branch "main" tip = C1; child "feat" ahead at C2.
func ffModel() Model {
	commits := []model.Commit{
		{Hash: "c2c2c2c", Parents: []string{"c1c1c1c"}, UnixTime: 30,
			Refs: []model.Ref{{Name: "feat", Kind: model.RefLocal}}},
		{Hash: "c1c1c1c", Parents: []string{"c0c0c0c"}, UnixTime: 20,
			Refs: []model.Ref{{Name: "main", Kind: model.RefLocal, Head: true}}},
		{Hash: "c0c0c0c", Parents: nil, UnixTime: 10},
	}
	m := Model{
		commits:   commits,
		sel:       map[panel]int{panelCommits: 0}, // select C2 (the ahead commit)
		sortModes: map[panel]sortMode{},
		focus:     panelCommits,
	}
	m.status.Branch = "main"
	return m
}

func TestFastForwardRowShownOnAheadCommit(t *testing.T) {
	m := ffModel() // selected = C2, ahead of main's tip C1
	row, ok := m.commitFastForwardRow()
	if !ok {
		t.Fatal("row must be offered when the selected commit is ahead of the current branch")
	}
	if row.label != "Fast-forward main to here" {
		t.Fatalf("label = %q", row.label)
	}
}

func TestFastForwardRowHiddenOnTip(t *testing.T) {
	m := ffModel()
	m.sel[panelCommits] = 1 // select C1 = main's own tip
	if _, ok := m.commitFastForwardRow(); ok {
		t.Fatal("row must be hidden on the current branch tip itself")
	}
}

func TestFastForwardRowHiddenOnBehindCommit(t *testing.T) {
	m := ffModel()
	m.sel[panelCommits] = 2 // select C0 = behind main's tip
	if _, ok := m.commitFastForwardRow(); ok {
		t.Fatal("row must be hidden on a commit behind the current branch")
	}
}

func TestFastForwardRowHiddenWhenDetached(t *testing.T) {
	m := ffModel()
	m.status.Branch = "" // detached
	if _, ok := m.commitFastForwardRow(); ok {
		t.Fatal("row must be hidden when HEAD is detached")
	}
}

func TestFastForwardRowRunsOp(t *testing.T) {
	m := ffModel()
	row, ok := m.commitFastForwardRow()
	if !ok {
		t.Fatal("row expected")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("running the row must start the fast-forward op")
	}
}
```

> Verify the `model.Ref` field/const names against `internal/model` (e.g. `RefLocal` vs `KindBranch`) before finalizing — grep `internal/model/*.go` for the `RefKind` constants and the `commit_ident.go` usage. Adjust the fixture's `Refs` to the real names. If a ready loaded-model helper exists, prefer it over the hand-built `ffModel`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestFastForwardRow`
Expected: FAIL — `m.commitFastForwardRow undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/tui/commit_scope.go`, add near `commitResetRow`:

```go
// commitFastForwardRow offers "Fast-forward <branch> to here" on the Commits
// panel: advance the current branch to the selected commit when that commit is a
// descendant of the branch's tip (git merge --ff-only, non-destructive). Gating
// is computed in-memory from the loaded feed's parent DAG (feedDescendant); when
// the walk leaves the loaded window the row is still offered and the op's
// IsAncestor guard decides. Hidden when HEAD is detached.
func (m Model) commitFastForwardRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	branch := m.status.Branch
	if branch == "" {
		return actionRow{}, false // detached HEAD: no current branch to move
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	selHash := m.commits[bi].Hash

	// Find the current branch's tip in the loaded feed (its decorated commit).
	tipHash := ""
	for _, c := range m.commits {
		if commitHasLocalRef(c, branch) {
			tipHash = c.Hash
			break
		}
	}
	if tipHash != "" {
		if descendant, conclusive := feedDescendant(m.commits, selHash, tipHash); conclusive && !descendant {
			return actionRow{}, false // conclusively not ahead → hide
		}
	}
	// descendant, or inconclusive, or tip not loaded → offer it; the op guards.

	return actionRow{
		id:    "commit-fast-forward",
		label: "Fast-forward " + branch + " to here",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.FastForward{Commit: selHash})
		},
	}, true
}
```

In `internal/tui/action_menu.go`, inside `appendCommitContextRows`, add immediately before the reset row block (line ~206) so fast-forward sits next to reset:

```go
	if r, ok := m.commitFastForwardRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run 'TestFastForward' && go build ./cmd/gg`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/commit_scope.go internal/tui/action_menu.go internal/tui/fast_forward_row_test.go
git commit -m "feat(tui): Fast-forward to here row in the Commits . menu" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

### Task 5: CLI `gg fast-forward <commit>`

**Files:**
- Create: `internal/cli/fast_forward.go`
- Modify: `internal/cli/cli.go` (add a `case "fast-forward"` in the dispatch switch ~line 80, and `"fast-forward": true` in the `commands` map ~line 103)
- Test: `internal/cli/fast_forward_test.go`

**Interfaces:**
- Consumes: `engine.FastForward` (Task 2), `runOperation`, `finish`, `cliDecider`, `stdinIsTerminal`.
- Produces: `func cmdFastForward(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int`.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/fast_forward_test.go`. Reuse the CLI test harness (grep `internal/cli/*_test.go` for how `cmdReset`/`cmdRevert` tests build a real repo + `*domain.Service` — e.g. a `newCLIRepo`/`runCLI` helper):

```go
func TestFastForwardCLIAdvances(t *testing.T) {
	dir, svc := newCLIRepo(t) // real repo, current branch main at C0
	gitIn(t, dir, "branch", "feat")
	gitIn(t, dir, "switch", "feat")
	writeFile(t, dir, "a.txt", "a\n")
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-m", "ahead")
	featTip := revParse(t, dir, "HEAD")
	gitIn(t, dir, "switch", "main")

	var out, errb bytes.Buffer
	code := cmdFastForward(svc, []string{featTip}, strings.NewReader(""), &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errb.String())
	}
	if got := revParse(t, dir, "HEAD"); got != featTip {
		t.Fatalf("HEAD = %s, want %s", got, featTip)
	}
}

func TestFastForwardCLIUsage(t *testing.T) {
	_, svc := newCLIRepo(t)
	var out, errb bytes.Buffer
	if code := cmdFastForward(svc, nil, strings.NewReader(""), &out, &errb); code != 2 {
		t.Fatalf("missing arg exit = %d, want 2", code)
	}
}

func TestFastForwardCLIRegistered(t *testing.T) {
	if !IsCommand("fast-forward") {
		t.Fatal("fast-forward must be in the commands map")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestFastForwardCLI`
Expected: FAIL — `undefined: cmdFastForward`.

- [ ] **Step 3: Write the implementation**

Create `internal/cli/fast_forward.go`:

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
)

// cmdFastForward implements `gg fast-forward <commit>`: advance the current
// branch to <commit> when it is a descendant of HEAD (git merge --ff-only). No
// flags and no decisions — a non-fast-forward target exits non-zero with the
// engine's error on stderr.
func cmdFastForward(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fast-forward", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg fast-forward <commit>")
		return 2
	}
	dec := cliDecider{policy: map[string]string{}, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.FastForward{Commit: fs.Arg(0)}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
```

In `internal/cli/cli.go`, add to the dispatch switch (next to `case "reset":`):

```go
	case "fast-forward":
		return cmdFastForward(svc, rest, stdin, stdout, stderr)
```

and to the `commands` map (on the `cherry-pick`/`revert`/`reset` line):

```go
	"cherry-pick": true, "revert": true, "reset": true, "fast-forward": true,
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestFastForwardCLI|TestEverySwitchCaseIsRegistered' && go build ./cmd/gg`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/fast_forward.go internal/cli/cli.go internal/cli/fast_forward_test.go
git commit -m "feat(cli): gg fast-forward <commit>" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

### Task 6: e2e scenario, agentskill, docs, race suite

**Files:**
- Create: `e2e/scenarios/<NN>-fast-forward.toml` (next free number — `ls e2e/scenarios/` and pick the next index)
- Modify: `internal/agentskill/using-gg.md` (+ bump `internal/agentskill/version.go` `Version`)
- Modify: `CHANGELOG.md`, `README.md`

**Interfaces:** none (integration + docs).

- [ ] **Step 1: Write the e2e scenario**

Read the `writing-e2e-scenarios` skill and an existing commit-op scenario (e.g. the reset/revert one) for the exact schema. Author `e2e/scenarios/<NN>-fast-forward.toml`: build `main` with a base commit, create `feat` ahead by one commit, switch back to `main`, run `gg fast-forward <feat-tip-or-ref>`, and assert `main` now points at `feat`'s tip (e.g. a `git rev-parse main` equals `feat` assertion, per the harness's state-assertion vocabulary). Use a ref the harness can resolve (a branch name `feat` works: `gg fast-forward feat`).

- [ ] **Step 2: Run the e2e stage**

Run: `./test.sh e2e`
Expected: PASS including the new scenario.

- [ ] **Step 3: Update the agent skill**

Add a `gg fast-forward <commit>` line to the command reference in `internal/agentskill/using-gg.md` (next to `gg reset`), then bump `Version` in `internal/agentskill/version.go`. Rebuild and refresh the dogfood copy:

```bash
go build ./cmd/gg && ./gg init --update
```

Run: `go test ./internal/agentskill/ -run TestDogfoodSkillCopyInSync`
Expected: PASS (the embedded + installed copies match).

- [ ] **Step 4: Update CHANGELOG and README**

`CHANGELOG.md` (under `### Added`):

```markdown
- **Fast-forward the current branch to a commit.** When another branch is built
  on top of your current branch, the Commits panel `.` menu now offers
  **Fast-forward `<branch>` to here** on any commit ahead of your branch's tip —
  advancing the branch with no merge commit (`git merge --ff-only`). Also
  available as `gg fast-forward <commit>`. The action only appears when the
  selected commit is actually ahead, and refuses (with a clear message) if it is
  not a fast-forward.
```

`README.md`: add a row to the Commits `.`-menu / CLI tables describing **Fast-forward `<branch>` to here** and `gg fast-forward <commit>`.

- [ ] **Step 5: Full race suite + commit**

Run: `./test.sh race`
Expected: PASS (vet+gofmt → unit → e2e, all green under `-race`).

```bash
git add e2e/scenarios CHANGELOG.md README.md internal/agentskill
git commit -m "docs+e2e: gg fast-forward (scenario, agentskill, changelog)" -m "Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_018bxAFvuaSjJw7NfpJe1rsj"
```

---

## Self-Review

- **Spec coverage:** git verb (Task 1) ✓; engine op with detached/not-ahead/already-at guards (Task 2) ✓; in-memory feed-descendant gating, no git call (Task 3) ✓; Commits `.`-menu row with show-when-ahead-or-inconclusive / hide-when-conclusively-behind (Task 4) ✓; `gg fast-forward` registered in both switch + map (Task 5) ✓; agentskill bump + e2e + CHANGELOG/README (Task 6) ✓. Branch-moved = current only, target from Commits panel, no confirm — all honored.
- **Placeholder scan:** every code step shows full code. The "grep the existing helper and reuse it" notes (test harness names in Tasks 1/2/4/5, `model.Ref` const names in Task 4, e2e schema in Task 6) are bounded reuse instructions with the concrete fallback code inline — not deferred work. They exist because exact helper/const names must be confirmed against the tree, and inventing parallel helpers would be wrong.
- **Type consistency:** `MergeFFOnly(ctx, dir, commit)`, `FastForward{Commit}`, `feedDescendant(commits, selHash, tipHash) (descendant, conclusive bool)`, `commitFastForwardRow() (actionRow, bool)`, `cmdFastForward(...)` are used identically across tasks. `IsAncestor("HEAD", target)` direction matches the Global Constraints note.
