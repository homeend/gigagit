# Squash Selected Commits — Stage 2 (reorder-then-squash) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** When a squash selection has gaps (non-adjacent commits), offer to reorder the commits adjacent first, then squash — instead of Stage 1's refusal.

**Architecture:** A new pure `rebaseplan.BuildSquashReorder` places the selected commits consecutively (oldest selected = Pick, rest = Squash) followed by the skipped in-between commits (Pick), all in range order. The Stage-1 dispatch handler, on a non-adjacency error (now a typed `ErrNotAdjacent`), opens a TUI `decisionState` confirm modal ("Reorder & squash" / "Cancel"); on confirm it builds the reorder plan and dispatches the same `engine.InteractiveRebase`.

**Tech Stack:** Go 1.26, existing `rebaseplan`, `engine.InteractiveRebase`, the TUI `decisionState` modal.

## Global Constraints

- `rebaseplan` is pure: no git/os/exec/TUI imports.
- Reorder placement (approved in the spec §Stage 2): selecting c1 and c3 from range `[c1,c2,c3,c4]` → todo `[c1 pick, c3 squash, c2 pick, c4 pick]` → result history `(c1+c3), c2, c4`. Selected collapse into the oldest selected's slot; skipped in-between commits float to just after the squash, preserving relative order.
- Order/adjacency/placement come from the loaded `onto..HEAD` range (`model.RangeCommit`), never the feed.
- Conflicts ride `InteractiveRebase`'s existing `rebase-conflict` decision (no new handling).

---

### Task 1: `ErrNotAdjacent` sentinel + `BuildSquashReorder`

**Files:**
- Modify: `internal/rebaseplan/squash.go`
- Test: `internal/rebaseplan/squash_test.go`

**Interfaces:**
- Consumes: `model.RangeCommit`, `rebaseplan.{Plan, Entry, Pick, Squash}`.
- Produces:
  - `var ErrNotAdjacent = errors.New("selected commits are not adjacent")` — `BuildSquash` returns it (via `fmt.Errorf("...: %w", ErrNotAdjacent)` or bare) so callers can `errors.Is`.
  - `func BuildSquashReorder(commits []model.RangeCommit, targets []string) (Plan, error)` — no adjacency requirement. Entries = the targets in range order (oldest = `Pick`, the rest `Squash`), followed by every non-target commit in range order (all `Pick`), each `Orig` carried from the range message. Errors on <2 targets or a target not in range (reuses the same validation as `BuildSquash`).

- [ ] **Step 1: Write the failing tests**

```go
func TestBuildSquashReturnsErrNotAdjacent(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "A"), rc("b", "B"), rc("c", "C")}
	_, err := BuildSquash(commits, []string{"a", "c"})
	if !errors.Is(err, ErrNotAdjacent) {
		t.Fatalf("err = %v, want ErrNotAdjacent", err)
	}
}

func TestBuildSquashReorderPlacement(t *testing.T) {
	// c1=a, c2=b (skipped), c3=c, c4=d. Select a and c.
	commits := []model.RangeCommit{rc("a", "A"), rc("b", "B"), rc("c", "C"), rc("d", "D")}
	p, err := BuildSquashReorder(commits, []string{"a", "c"})
	if err != nil {
		t.Fatalf("BuildSquashReorder: %v", err)
	}
	// Expect todo order: a(pick), c(squash), b(pick), d(pick).
	var order []string
	for _, e := range p.Entries {
		order = append(order, e.Sha+":"+string(e.Action))
	}
	want := []string{"a:pick", "c:squash", "b:pick", "d:pick"}
	if strings.Join(order, " ") != strings.Join(want, " ") {
		t.Fatalf("plan = %v, want %v", order, want)
	}
	// Message of the squash group (target index 0) concatenates a and c.
	if got := p.Message(0); got != "A\n\nC" {
		t.Fatalf("Message(0) = %q, want %q", got, "A\n\nC")
	}
}

func TestBuildSquashReorderTooFew(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "A"), rc("b", "B")}
	if _, err := BuildSquashReorder(commits, []string{"a"}); err == nil {
		t.Fatal("want error for fewer than 2 targets")
	}
}

func TestBuildSquashReorderMissingTarget(t *testing.T) {
	commits := []model.RangeCommit{rc("a", "A"), rc("b", "B")}
	if _, err := BuildSquashReorder(commits, []string{"a", "z"}); err == nil {
		t.Fatal("want error for target not in range")
	}
}
```

(`rc` already exists in `edit_test.go`; `strings` and `errors` imports may need adding to `squash_test.go`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go -C <worktree> test ./internal/rebaseplan/ -run 'ErrNotAdjacent|Reorder'`
Expected: FAIL — `ErrNotAdjacent` / `BuildSquashReorder` undefined.

- [ ] **Step 3: Implement**

Refactor the shared validation out of `BuildSquash`, add the sentinel, and add `BuildSquashReorder`:

```go
// ErrNotAdjacent is returned by BuildSquash when the selected commits have gaps.
// Callers (Stage 2) detect it with errors.Is to offer reorder-then-squash.
var ErrNotAdjacent = errors.New("selected commits are not adjacent")

// squashTargets validates targets against the range and returns, in range
// (oldest-first) order, the target indices plus an isTarget set.
func squashTargets(commits []model.RangeCommit, targets []string) (idxs []int, isTarget map[string]bool, err error) {
	if len(targets) < 2 {
		return nil, nil, fmt.Errorf("select at least 2 commits to squash")
	}
	pos := make(map[string]int, len(commits))
	for i, c := range commits {
		pos[c.Hash] = i
	}
	isTarget = make(map[string]bool, len(targets))
	for _, t := range targets {
		i, ok := pos[t]
		if !ok {
			return nil, nil, fmt.Errorf("commit %s is not on the current branch", shortSquashSHA(t))
		}
		if !isTarget[t] {
			isTarget[t] = true
			idxs = append(idxs, i)
		}
	}
	sort.Ints(idxs)
	return idxs, isTarget, nil
}
```

Rewrite `BuildSquash` to use it and return `ErrNotAdjacent`:

```go
func BuildSquash(commits []model.RangeCommit, targets []string) (Plan, error) {
	idxs, isTarget, err := squashTargets(commits, targets)
	if err != nil {
		return Plan{}, err
	}
	lo, hi := idxs[0], idxs[len(idxs)-1]
	if hi-lo+1 != len(idxs) {
		return Plan{}, ErrNotAdjacent
	}
	entries := make([]Entry, len(commits))
	for i, c := range commits {
		action := Pick
		if isTarget[c.Hash] && i != lo {
			action = Squash
		}
		entries[i] = Entry{Sha: c.Hash, Action: action, Orig: c.Message}
	}
	return Plan{Entries: entries}, nil
}

// BuildSquashReorder builds a plan that first replays the target commits
// consecutively (oldest = Pick, the rest Squash) and then the skipped in-between
// commits (Pick), all in range order. It does NOT require adjacency: it reorders
// the skipped commits to just after the squashed commit. Conflicts surface
// through the normal rebase-conflict path.
func BuildSquashReorder(commits []model.RangeCommit, targets []string) (Plan, error) {
	idxs, isTarget, err := squashTargets(commits, targets)
	if err != nil {
		return Plan{}, err
	}
	byHash := make(map[string]model.RangeCommit, len(commits))
	for _, c := range commits {
		byHash[c.Hash] = c
	}
	entries := make([]Entry, 0, len(commits))
	// Targets first, oldest-first; oldest = Pick, rest = Squash.
	for n, i := range idxs {
		c := commits[i]
		action := Squash
		if n == 0 {
			action = Pick
		}
		entries = append(entries, Entry{Sha: c.Hash, Action: action, Orig: c.Message})
		_ = byHash
	}
	// Then the skipped/newer non-targets, in range order, all Pick.
	for _, c := range commits {
		if !isTarget[c.Hash] {
			entries = append(entries, Entry{Sha: c.Hash, Action: Pick, Orig: c.Message})
		}
	}
	return Plan{Entries: entries}, nil
}
```

Add `"errors"` and `"sort"` to the import block. Remove the now-unused `_ = byHash`/`byHash` scaffold if not needed (it is not — drop it).

- [ ] **Step 4: Run to verify pass**

Run: `go -C <worktree> test ./internal/rebaseplan/`
Expected: PASS (new + existing, including Stage 1's `TestBuildSquashNonAdjacent` which still expects an error — `ErrNotAdjacent` satisfies it).

- [ ] **Step 5: Commit**

```bash
git add internal/rebaseplan/squash.go internal/rebaseplan/squash_test.go
git commit -m "feat(rebaseplan): BuildSquashReorder + ErrNotAdjacent sentinel"
```

---

### Task 2: Reorder confirm modal on non-adjacent squash

**Files:**
- Modify: `internal/tui/model.go` (the `squashRangeLoadedMsg` case)
- Test: `internal/tui/squash_test.go`

**Interfaces:**
- Consumes: `rebaseplan.{BuildSquash, BuildSquashReorder, ErrNotAdjacent}`, `errors.Is`, `decisionState`, `engine.{DecisionRequest, InteractiveRebase}`, `os.Executable`.
- Produces: on a non-adjacent selection the handler sets `m.modal` to a `decisionState` whose options are `["Reorder & squash", "Cancel"]`; choosing "Reorder & squash" builds the reorder plan and dispatches `InteractiveRebase`.

- [ ] **Step 1: Write the failing test**

```go
func TestSquashNonAdjacentOpensReorderModal(t *testing.T) {
	m := loadedModelLinearCommits(t, 4) // commits[0..3], newest-first
	m.focus = panelCommits
	// Select the newest and the third-newest (a gap at commits[1]).
	m.commitCompareSet = selectionSet(m.commits[0].Hash, m.commits[2].Hash)

	// Drive the loaded-range handler directly with the real range.
	onto := m.commits[2].Hash + "^"
	cs, err := m.svc.CommitRange(context.Background(), onto, m.status.Branch)
	if err != nil {
		t.Fatalf("CommitRange: %v", err)
	}
	u, _ := m.Update(squashRangeLoadedMsg{
		branch:  m.status.Branch,
		onto:    onto,
		targets: []string{m.commits[0].Hash, m.commits[2].Hash},
		commits: cs,
	})
	m = u.(Model)
	if m.modal == nil {
		t.Fatal("a non-adjacent squash must open the reorder confirm modal")
	}
	opts := m.modal.req.Options
	if len(opts) == 0 || !strings.Contains(strings.ToLower(opts[0]), "reorder") {
		t.Fatalf("modal options = %v, want a Reorder option first", opts)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go -C <worktree> test ./internal/tui/ -run TestSquashNonAdjacentOpensReorderModal`
Expected: FAIL — `m.modal` is nil (Stage 1 set a refuse note instead).

- [ ] **Step 3: Update the `squashRangeLoadedMsg` handler**

Replace the Stage-1 build/refuse branch with adjacency-aware handling:

```go
	case squashRangeLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "squash: " + msg.err.Error()
			return m, nil
		}
		plan, perr := rebaseplan.BuildSquash(msg.commits, msg.targets)
		if errors.Is(perr, rebaseplan.ErrNotAdjacent) {
			branch, onto := msg.branch, msg.onto
			commits, targets := msg.commits, msg.targets
			m.modal = &decisionState{
				req: engine.DecisionRequest{
					ID:      "squash-reorder",
					Prompt:  "Selected commits aren't adjacent. Reorder them adjacent, then squash?",
					Options: []string{"Reorder & squash", "Cancel"},
				},
				onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
					if opt != "Reorder & squash" {
						return m, nil
					}
					rp, err := rebaseplan.BuildSquashReorder(commits, targets)
					if err != nil {
						m.statusMsg = "squash: " + err.Error()
						return m, nil
					}
					ggBin, err := os.Executable()
					if err != nil {
						m.statusMsg = "squash: " + err.Error()
						return m, nil
					}
					m.commitCompareSet = nil
					return m.startOp(engine.InteractiveRebase{Branch: branch, Onto: onto, Plan: rp, GGBin: ggBin})
				},
			}
			return m, nil
		}
		if perr != nil {
			m.statusMsg = "squash: " + perr.Error()
			return m, nil
		}
		ggBin, err := os.Executable()
		if err != nil {
			m.statusMsg = "squash: " + err.Error()
			return m, nil
		}
		m.commitCompareSet = nil
		return m.startOp(engine.InteractiveRebase{Branch: msg.branch, Onto: msg.onto, Plan: plan, GGBin: ggBin})
```

Add `"errors"` to `model.go`'s imports if not present.

- [ ] **Step 4: Run the test + full tui suite**

Run: `go -C <worktree> test ./internal/tui/ -run TestSquash` then `go -C <worktree> test ./internal/tui/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/squash_test.go
git commit -m "feat(tui): non-adjacent squash offers Reorder & squash confirm"
```

---

### Task 3: End-to-end reorder-squash integration test

**Files:**
- Test: `internal/engine/commit_squash_integration_test.go`

**Interfaces:**
- Consumes: `buildGG`, `fourCommitBranch`, `subjects`, `gitOut`, `shaOf`, `rebaseplan.BuildSquashReorder`, `InteractiveRebase`.

- [ ] **Step 1: Write the test**

```go
// Selecting a (oldest) and c (skipping b) reorders b after the squash:
// result history oldest→newest = (a+c), b, d.
func TestSquashReorderEndToEnd(t *testing.T) {
	gg := buildGG(t)
	dir, repo := fourCommitBranch(t) // main -> a -> b -> c -> d
	aSha := shaOf(t, dir, "work~3")
	cSha := shaOf(t, dir, "work~1")
	onto := aSha + "^"
	commits, err := repo.LogRangeMessages(context.Background(), onto, "work")
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	plan, err := rebaseplan.BuildSquashReorder(commits, []string{aSha, cSha})
	if err != nil {
		t.Fatalf("BuildSquashReorder: %v", err)
	}
	if _, err := (InteractiveRebase{Branch: "work", Onto: onto, Plan: plan, GGBin: gg}).
		Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("reorder-squash: %v", err)
	}
	got := subjects(t, dir, "main..work") // newest-first
	if want := []string{"d", "b", "a"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("after reorder-squash a+c: %v, want %v", got, want)
	}
	// The squashed commit (now work~2) carries both a and c.
	msg := gitOut(t, dir, "log", "-1", "--format=%B", "work~2")
	if !strings.Contains(msg, "a") || !strings.Contains(msg, "c") {
		t.Fatalf("squashed message = %q, want both a and c", msg)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go -C <worktree> test ./internal/engine/ -run TestSquashReorderEndToEnd -v`
Expected: PASS — `main..work` is `[d b a]` (4 commits → 3) and the squashed commit holds a + c.

- [ ] **Step 3: Commit**

```bash
git add internal/engine/commit_squash_integration_test.go
git commit -m "test(engine): end-to-end reorder-then-squash collapses non-adjacent commits"
```

---

### Task 4: Help + CHANGELOG

**Files:**
- Modify: `internal/tui/help.go`, `CHANGELOG.md`

- [ ] **Step 1: Update help**

In `help.go`, change the Squash help line so it no longer says non-adjacent is refused; instead: non-adjacent selections prompt **Reorder & squash** (reorders the in-between commits after the squash), with conflicts pausing for `git rebase --continue`.

- [ ] **Step 2: Update CHANGELOG**

Under `## [Unreleased]` → `### Added` (or amend the existing Squash entry), note that non-adjacent selections now offer to reorder adjacent first, then squash.

- [ ] **Step 3: Verify build + full unit suite**

Run: `go -C <worktree> build ./... && (cd <worktree> && ./test.sh unit)`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/help.go CHANGELOG.md
git commit -m "docs: help + changelog for reorder-then-squash"
```

---

## Self-Review Notes

- Spec §Stage 2 placement → Task 1 (`BuildSquashReorder` + test) and Task 3 (e2e proves the resulting history).
- Confirm modal → Task 2, reusing `decisionState{req, onResolve}` exactly as `reflogCheckoutRow` does.
- `ErrNotAdjacent` is the seam letting the handler tell "has gaps" (→ modal) from other failures (→ note). Stage 1's `TestBuildSquashNonAdjacent` still passes (it only asserts `err != nil`).
- Conflicts: no new code — `InteractiveRebase`'s `rebase-conflict` decision already covers a conflicting reorder (shared with move/drop).
- Range-derived placement, never feed: `BuildSquashReorder` works purely on `commits` (the range) + `targets`.
