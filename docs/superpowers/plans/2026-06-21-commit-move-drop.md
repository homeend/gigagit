# Move / drop a commit (Commits panel) — Plan

> **For agentic workers:** TDD. Steps use `- [ ]`.

**Goal:** One-shot `.`-menu Move up / Move down / Drop on a current-branch,
non-merge Commits-panel commit, via `engine.InteractiveRebase` with `Onto`
derived as `<sha>~1` / `<sha>~2`.

**Architecture:** Relax `InteractiveRebase` to accept a commit-ish `Onto`
(+ a `CommitExists` verb). Pure `ontoFor` + `buildSingleEdit` build the plan; a
two-step async TUI command loads the range and runs the op.

## Global Constraints

- `Onto`: `ontoFor(sha, e)` → `sha+"~1"` (drop/move-up) or `sha+"~2"` (move-down).
- Todo order = `CommitRange` order (oldest-first, `git log --reverse`).
- move-up = swap target with the **next** entry; move-down = swap with the
  **previous**; drop = set target entry `Drop`. Others stay `Pick`, `Orig` =
  `RangeCommit.Message`.
- Sync gate: `panelCommits && opsIdle && m.status.Branch != "" && len(C.Parents)==1`.
- Commit trailers: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` +
  `Claude-Session:`.

---

### Task 1: `CommitExists` git verb + GitOps

**Files:** Create `internal/git/commit_exists.go`, `internal/git/commit_exists_test.go`;
Modify `internal/engine/gitops.go`.

- [ ] **Step 1: Failing real-git test** (use the git package's repo helper —
  check `internal/git/*_test.go` for the temp-repo helper, e.g. `newTestRepo`):
  `CommitExists(ctx, "HEAD")` → true; a real commit SHA → true; `"deadbeef"` /
  `"nope"` → false (no error); `"HEAD~1"` on a 2-commit repo → true.

- [ ] **Step 2: Run — fail.** `go test ./internal/git/ -run TestCommitExists` → undefined.

- [ ] **Step 3: Implement** `internal/git/commit_exists.go`:

```go
package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// CommitExists reports whether ref resolves to a commit object (any commit-ish:
// branch, tag, SHA, or revspec like "abc123~2"). rev-parse exit code 1 is the
// clean "no such commit" signal.
func (r *Repo) CommitExists(ctx context.Context, ref string) (bool, error) {
	b := gitcmd.New("rev-parse").Arg("-q", "--verify", ref+"^{commit}")
	res, err := r.Runner.Run(ctx, "git rev-parse verify commit", b.ToArgv())
	if err == nil {
		return true, nil
	}
	if res.ExitCode == 1 {
		return false, nil
	}
	return false, err
}
```

- [ ] **Step 4: Add to the `GitOps` interface** (`internal/engine/gitops.go`),
  near `IsAncestor`:

```go
	CommitExists(ctx context.Context, ref string) (bool, error)
```

- [ ] **Step 5: Run — pass.** `go test ./internal/git/ -run TestCommitExists` + `go build ./...`.

- [ ] **Step 6: Commit.** `feat(git): CommitExists verb (rev-parse --verify commit-ish)`

---

### Task 2: Relax `InteractiveRebase` Onto to a commit-ish

**Files:** Modify `internal/engine/interactive_rebase.go`;
Test `internal/engine/interactive_rebase_test.go`.

- [ ] **Step 1: Failing real-git test.** Reuse `threeCommitBranch(t)` (existing
  helper). Drive a drop via a **SHA** `Onto`:
  - Build a plan that drops the middle commit; `Onto = "<tip>~2"` (resolves to a
    commit, not a branch); run `InteractiveRebase{Branch, Onto, Plan, GGBin}`;
    assert success + the middle commit gone from `git log`.
  - `Onto = "nope"` → error containing `no such commit`.
  (If a fixture for ggBin is needed, mirror the existing irebase tests' GGBin
  setup.)

- [ ] **Step 2: Run — fail** (current validation rejects a non-branch Onto).

- [ ] **Step 3: Implement** — replace the both-must-be-branches loop:

```go
	have := make(map[string]bool, len(branches))
	for _, b := range branches {
		have[b.Name] = true
	}
	if !have[op.Branch] {
		return Result{}, fmt.Errorf("interactive rebase: no such branch: %s", op.Branch)
	}
	// Onto may be a branch OR any resolvable commit-ish (single-commit
	// move/drop bases onto a parent SHA like "<sha>~1").
	if !have[op.Onto] {
		ok, err := deps.Repo.CommitExists(ctx, op.Onto)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			return Result{}, fmt.Errorf("interactive rebase: no such commit: %s", op.Onto)
		}
	}
```

- [ ] **Step 4: Run — pass.** `go test ./internal/engine/ -run 'Rebase'` (the existing
  branch→branch irebase tests must still pass — they hit the `have[op.Onto]` true path).

- [ ] **Step 5: Commit.** `feat(engine): allow InteractiveRebase Onto to be any commit-ish`

---

### Task 3: `ontoFor` + `buildSingleEdit` pure transforms

**Files:** Create `internal/tui/commit_rebase_ops.go`,
`internal/tui/commit_rebase_ops_test.go`.

**Produces:** `type commitEdit int` (`editDrop`, `editMoveUp`, `editMoveDown`),
`ontoFor(sha string, e commitEdit) string`,
`buildSingleEdit(commits []model.RangeCommit, target string, e commitEdit) (rebaseplan.Plan, error)`.

- [ ] **Step 1: Failing tests:**

```go
func rc(h, s string) model.RangeCommit { return model.RangeCommit{Hash: h, Subject: s, Message: s + "\n"} }

func TestOntoFor(t *testing.T) {
	if ontoFor("abc", editDrop) != "abc~1" || ontoFor("abc", editMoveUp) != "abc~1" {
		t.Fatal("drop/up should be ~1")
	}
	if ontoFor("abc", editMoveDown) != "abc~2" {
		t.Fatal("down should be ~2")
	}
}

func planSubjects(p rebaseplan.Plan) (out []string) {
	for _, e := range p.Entries {
		s := e.Sha
		if e.Action == rebaseplan.Drop {
			s += "(drop)"
		}
		out = append(out, s)
	}
	return
}

func TestBuildSingleEdit(t *testing.T) {
	// oldest-first range [a,b,c,d]
	commits := []model.RangeCommit{rc("a", "a"), rc("b", "b"), rc("c", "c"), rc("d", "d")}

	drop, err := buildSingleEdit(commits, "b", editDrop)
	if err != nil {
		t.Fatal(err)
	}
	if got := planSubjects(drop); !eq(got, []string{"a", "b(drop)", "c", "d"}) {
		t.Fatalf("drop = %v", got)
	}
	// every entry carries Orig
	for _, e := range drop.Entries {
		if e.Orig == "" {
			t.Fatalf("entry %s missing Orig", e.Sha)
		}
	}

	up, _ := buildSingleEdit(commits, "b", editMoveUp) // swap b,c
	if got := planSubjects(up); !eq(got, []string{"a", "c", "b", "d"}) {
		t.Fatalf("moveUp = %v", got)
	}
	down, _ := buildSingleEdit(commits, "c", editMoveDown) // swap b,c
	if got := planSubjects(down); !eq(got, []string{"a", "c", "b", "d"}) {
		t.Fatalf("moveDown = %v", got)
	}

	if _, err := buildSingleEdit(commits, "zzz", editDrop); err == nil {
		t.Fatal("missing target should error")
	}
	if _, err := buildSingleEdit(commits, "d", editMoveUp); err == nil {
		t.Fatal("move up the tip should error")
	}
	if _, err := buildSingleEdit(commits, "a", editMoveDown); err == nil {
		t.Fatal("move down the oldest should error")
	}
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run — fail.** `go test ./internal/tui/ -run 'TestOntoFor|TestBuildSingleEdit'` → undefined.

- [ ] **Step 3: Implement** in `commit_rebase_ops.go`:

```go
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/rebaseplan"
)

type commitEdit int

const (
	editDrop commitEdit = iota
	editMoveUp
	editMoveDown
)

func ontoFor(sha string, e commitEdit) string {
	if e == editMoveDown {
		return sha + "~2"
	}
	return sha + "~1"
}

// buildSingleEdit builds the rebase plan for a single-commit edit over an
// oldest-first range. move up swaps the target with the next (newer) entry;
// move down with the previous (older); drop marks the target Drop.
func buildSingleEdit(commits []model.RangeCommit, target string, e commitEdit) (rebaseplan.Plan, error) {
	entries := make([]rebaseplan.Entry, len(commits))
	idx := -1
	for i, c := range commits {
		entries[i] = rebaseplan.Entry{Sha: c.Hash, Action: rebaseplan.Pick, Orig: c.Message}
		if c.Hash == target {
			idx = i
		}
	}
	if idx == -1 {
		return rebaseplan.Plan{}, fmt.Errorf("commit is not on the current branch")
	}
	switch e {
	case editDrop:
		entries[idx].Action = rebaseplan.Drop
	case editMoveUp:
		if idx+1 >= len(entries) {
			return rebaseplan.Plan{}, fmt.Errorf("already the newest commit")
		}
		entries[idx], entries[idx+1] = entries[idx+1], entries[idx]
	case editMoveDown:
		if idx-1 < 0 {
			return rebaseplan.Plan{}, fmt.Errorf("already the oldest commit in range")
		}
		entries[idx], entries[idx-1] = entries[idx-1], entries[idx]
	}
	return rebaseplan.Plan{Entries: entries}, nil
}

var _ = tea.Batch // keep tea imported for the rows added in Task 4
```

  (Remove the `tea` placeholder once Task 4 adds the rows that use it.)

- [ ] **Step 4: Run — pass.**

- [ ] **Step 5: Commit.** `feat(tui): ontoFor + buildSingleEdit pure rebase-plan transforms`

---

### Task 4: Rows + async command + wiring + integration test

**Files:** Modify `internal/tui/commit_rebase_ops.go`, `internal/tui/op.go`,
`internal/tui/model.go`, `internal/tui/action_menu.go`; Test
`internal/tui/commit_rebase_ops_test.go` (predicates) +
`internal/engine/commit_move_drop_integration_test.go` (real-git).

**Produces:** `commitDropRow`/`commitMoveUpRow`/`commitMoveDownRow (actionRow, bool)`,
`m.startCommitEditCmd(sha string, e commitEdit) (Model, tea.Cmd)`,
`loadRebaseRangeCmd(...) tea.Cmd`, `rebaseRangeLoadedMsg`.

- [ ] **Step 1: Failing predicate tests** (build a Commits Model like the
  existing commit-row tests — set `m.commits`, `m.sel[panelCommits]`,
  `m.status.Branch="main"`):
  - all three rows present on a non-merge commit (`Parents: ["p"]`).
  - absent on a merge (`Parents: ["p1","p2"]`), a root (`Parents: nil`), a
    detached HEAD (`m.status.Branch == ""`), off the Commits panel, while running.
  - `availableActions` contains `commit-drop`, `commit-move-up`, `commit-move-down`.

- [ ] **Step 2: Failing integration test** (`internal/engine`, real git): build
  `a→b→c→d` on `main`; for each case compute `onto := ...` via the SAME logic as
  the TUI (call a shared `ontoFor`-equivalent or replicate `sha+"~1"`/`"~2"` —
  prefer importing is impossible across packages, so the engine test computes
  `onto` with the same `~1`/`~2` rule and a comment pointing at `ontoFor`), then
  `CommitRange`→plan→`InteractiveRebase`, asserting `git log --format=%s` order:
  drop c → `d,b,a`; move c down → `d,b,c,a`; move b up → `d,b,c,a` (newest-first
  `git log`). Plus a conflict case asserting the op pauses via
  `MapDecider{"rebase-conflict":"keep-conflicts"}`.
  (NB: `buildSingleEdit` lives in `internal/tui`; the engine integration test
  builds the plan inline with the same swap rule, or move `buildSingleEdit` +
  `ontoFor` + `commitEdit` into `internal/rebaseplan` so both packages share
  them — DECIDE in Step 3 and keep one source of truth.)

- [ ] **Step 3: Decide placement & implement.** To keep ONE source of truth for
  the integration test and the TUI, **move `commitEdit` + `ontoFor` +
  `buildSingleEdit` into `internal/rebaseplan`** (pure, no tui/git deps; it
  already owns `Plan`). Update Task 3's tests to the new package. Then implement
  in `commit_rebase_ops.go`:

```go
func (m Model) commitEditRow(id, label string, e commitEdit) (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() || m.status.Branch == "" {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok || len(m.commits[bi].Parents) != 1 {
		return actionRow{}, false
	}
	sha := m.commits[bi].Hash
	branch := m.status.Branch
	return actionRow{id: id, label: label, run: func(m Model) (tea.Model, tea.Cmd) {
		return m.startCommitEditCmd(branch, sha, e)
	}}, true
}

func (m Model) commitDropRow() (actionRow, bool) {
	return m.commitEditRow("commit-drop", "Drop commit", rebaseplan.EditDrop)
}
func (m Model) commitMoveUpRow() (actionRow, bool) {
	return m.commitEditRow("commit-move-up", "Move commit up (newer)", rebaseplan.EditMoveUp)
}
func (m Model) commitMoveDownRow() (actionRow, bool) {
	return m.commitEditRow("commit-move-down", "Move commit down (older)", rebaseplan.EditMoveDown)
}

func (m Model) startCommitEditCmd(branch, sha string, e commitEdit) (Model, tea.Cmd) {
	return m, m.loadRebaseRangeCmd(branch, rebaseplan.OntoFor(sha, e), sha, e)
}
```

  `op.go`: `rebaseRangeLoadedMsg{branch, onto, target string; edit commitEdit; commits []model.RangeCommit; err error}` + `loadRebaseRangeCmd` calling `svc.CommitRange`.
  `model.go` handler: on err → status; else `plan, perr := rebaseplan.BuildSingleEdit(msg.commits, msg.target, msg.edit)`; on perr → status; else `ggBin,_ := os.Executable()` → `m.startOp(engine.InteractiveRebase{Branch: msg.branch, Onto: msg.onto, Plan: plan, GGBin: ggBin})`.
  `action_menu.go`: append the three rows after `m.commitBranchRows()`.

- [ ] **Step 4: Run — pass.** `go test ./internal/tui/ ./internal/engine/` green.

- [ ] **Step 5: Commit.** `feat(tui): one-shot Move/Drop commit actions on the Commits panel`

---

### Task 5: Docs

- [ ] **help.go** — Commits-panel `.`-menu line: add Move commit up/down + Drop
  commit (current branch, non-merge). Update any help test asserting text.
- [ ] **CHANGELOG** — `### Added` bullet describing one-shot move/drop.
- [ ] Run `go test ./internal/tui/ -run TestHelp`. Commit
  `docs: changelog + help for commit move/drop`.

---

## Final verification

- [ ] `./test.sh race` green.
- [ ] `gofmt -l internal/ | head` empty.
- [ ] Merge; verify merged tree; clean up worktree; update memory.

## Self-review notes

- Single source of truth: `commitEdit`/`OntoFor`/`BuildSingleEdit` live in
  `internal/rebaseplan`, shared by the TUI rows and the engine integration test.
- The integration test computes `Onto` via `rebaseplan.OntoFor` (the real
  derivation), and asserts full `git log` order — the discriminating proof for
  `~2`/move-down.
- Type consistency: `rebaseRangeLoadedMsg.edit` is `commitEdit`; rows map
  drop→`~1`, down→`~2` via `OntoFor`.
