# Stash Surface Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make stashing usable from the TUI — `m` multi-marks Status files, `s` opens a stash-create popup, `S` opens a stash list window (right column) where `l` drills into the file tree and `Enter` opens an Apply/Pop/Drop action popup.

**Architecture:** Three merge-ready chunks. **A (Create):** extend the stash git verbs (paths/`-u`) + engine op, add a Status multi-mark set, build the stash-create popup. **B (View):** the stash list window in the right column, drilling into the *existing* commit-files view (`filesView`) pointed at the stash's resolved commit SHA — so diff/`h`/blame work unchanged (a stash is a commit). **C (Manage):** the Apply/Pop/Drop action popup + verbs/ops.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`, pointer popup fields), `gitcmd` argv builder, `gitexec.FakeRunner` for argv tests, real `git` in `t.TempDir()` for behavior tests.

**Spec:** `docs/superpowers/specs/2026-06-15-stash-surface-design.md`.

---

## Orientation (read once before Task 1)

Patterns this plan mirrors — open these to match style:

- **Stash verbs:** `internal/git/stash.go` (`StashPush`/`StashPop`/`StashList`). argv via `gitcmd.New("sub").Arg(...).ArgIf(cond,...)`; run via `r.Runner.Run(ctx, "git …", argv)`.
- **Engine op:** `internal/engine/ops_basic.go` (`Stash`, `Commit`, `Push`) — `Run` emits `Progress`, calls a verb, returns `Result{Summary, Changed:true}`, emits `Done`. Compile-time `var _ Operation = X{}`.
- **GitOps interface:** `internal/engine/gitops.go` — every verb an op calls is listed; `var _ GitOps = (*git.Repo)(nil)` guards drift.
- **Domain gated read:** `internal/domain/query.go` — the `query[T]` helper; `CommitFiles`/`CurrentBranch` are one-line examples.
- **Popup field (pointer, survives the value copy):** `internal/tui/worktree_popup.go` for the struct/open/update/render shape; typed-input handling (rune append / backspace) matches the `filterTyping` block in `internal/tui/model.go:279-319` and the `contentPopup` typing block in `internal/tui/files_view.go:106-127`.
- **Key routing chain:** `internal/tui/model.go:225-277` (`modal → stackTop → diffView → popup → … → pairPopup → filesView → filterTyping → normal`). New pointer-field surfaces are added to this chain.
- **Write path:** `internal/tui/op.go` (`startOp` at :80 → `opFinishedMsg` → full `loadCmd()`); `m.startOp(engine.X{})` is the standard way to run a mutating op. Used as-is for all stash mutations.
- **Commit-files view (the file tree we reuse):** opener in `internal/tui/model.go:484` (`case "l"`: sets `filesView`/`filesTitle`/`filesHash`/`filesTreeFocused`, fires a load cmd); key handler `internal/tui/files_view.go:101` (`updateFilesViewKey`: `enter`→diff, `h`→history, `b`→blame, `/`→search, `l`/`esc`→close — all keyed off `m.filesHash`); left-column render `view.go:204` (`case m.filesView != nil: left = m.renderFilesView(...)`).
- **Panel render + marker:** `internal/tui/view.go:236` (`renderPanel`); the single-mark `◆ ` prefix comes from `markDisplayIndex` at :260-271.

**Base-branch caveat:** the F2 `commit_popup.go` is on `feat/commit-amend`, **not** on `main`. This branch (`feat/stash-surface`) is off `main`, so **do not** import or reference `commit_popup`. Base the stash-create popup on `worktree_popup.go` conventions + the typing pattern above.

**Run tests:** `go test ./internal/git/ ./internal/engine/ ./internal/domain/ ./internal/tui/` for fast loops; `./test.sh race` before each chunk's final commit.

## File structure

| File | Responsibility |
|------|----------------|
| `internal/git/stash.go` (+`stash_test.go`) | extend `StashPush` (paths,`-u`); `StashPop(ref)`; add `StashApply`, `StashDrop`, `StashCommit` (rev-parse) |
| `internal/model/model.go` (+`model_test.go`) | `StashEntry{Ref, Subject}` |
| `internal/engine/gitops.go` | verb signatures |
| `internal/engine/ops_basic.go` (+`ops_basic_test.go`) | `Stash` gains `Paths`/`IncludeUntracked`; add `StashApply`/`StashPop`/`StashDrop` ops |
| `internal/domain/stash.go` (+`stash_test.go`) | `StashList` (parse → `[]model.StashEntry`), `StashCommit` gated reads; pure parser `parseStashList` |
| `internal/tui/mark.go`, `model.go`, `view.go` | `fileMarks` set, `m`-on-Status branch, multi-mark render |
| `internal/tui/stash_popup.go` (+test) | the stash-create popup |
| `internal/tui/stash_view.go` (+test) | the stash list window: struct, load, render, key routing, `l`→files |
| `internal/tui/stash_action.go` (+test) | the Apply/Pop/Drop action popup |
| `internal/tui/footer.go`, `help.go` | `[s] stash`, `[S] stashes`, window hints + help rows |
| `CHANGELOG.md`, `README.md` | entries |

---

# CHUNK A — Create path (Tasks 1–6)

Outcome: `m` multi-marks Status files; `s` opens a popup (name + file checklist); `ctrl+s` stashes the checked files. Stashes are real and visible via `git stash list`.

## Task 1: Extend the stash git verbs

**Files:**
- Modify: `internal/git/stash.go`
- Test: `internal/git/stash_test.go`

- [ ] **Step 1: Write the failing argv tests**

Add to `internal/git/stash_test.go` (use the existing `FakeRunner` test style already in this file):

```go
func TestStashPushArgv(t *testing.T) {
	fake := gitexec.NewFakeRunner()
	r := &Repo{Runner: fake}
	if err := r.StashPush(context.Background(), "WIP on main", []string{"a.go", "b.go"}, true); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(fake.Calls[0].Args, " ")
	want := "stash push -m WIP on main -u -- a.go b.go"
	if got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

func TestStashPushNoPathsNoUntracked(t *testing.T) {
	fake := gitexec.NewFakeRunner()
	r := &Repo{Runner: fake}
	_ = r.StashPush(context.Background(), "msg", nil, false)
	got := strings.Join(fake.Calls[0].Args, " ")
	if got != "stash push -m msg" {
		t.Errorf("argv = %q, want %q", got, "stash push -m msg")
	}
}

func TestStashPopRefArgv(t *testing.T) {
	fake := gitexec.NewFakeRunner()
	r := &Repo{Runner: fake}
	_ = r.StashPop(context.Background(), "stash@{2}")
	if got := strings.Join(fake.Calls[0].Args, " "); got != "stash pop stash@{2}" {
		t.Errorf("argv = %q", got)
	}
	fake2 := gitexec.NewFakeRunner()
	r2 := &Repo{Runner: fake2}
	_ = r2.StashPop(context.Background(), "")
	if got := strings.Join(fake2.Calls[0].Args, " "); got != "stash pop" {
		t.Errorf("empty-ref argv = %q, want %q", got, "stash pop")
	}
}

func TestStashApplyDropArgv(t *testing.T) {
	fake := gitexec.NewFakeRunner()
	r := &Repo{Runner: fake}
	_ = r.StashApply(context.Background(), "stash@{1}")
	_ = r.StashDrop(context.Background(), "stash@{1}")
	if got := strings.Join(fake.Calls[0].Args, " "); got != "stash apply stash@{1}" {
		t.Errorf("apply argv = %q", got)
	}
	if got := strings.Join(fake.Calls[1].Args, " "); got != "stash drop stash@{1}" {
		t.Errorf("drop argv = %q", got)
	}
}
```

> Check the existing tests in `stash_test.go` for the exact `FakeRunner` accessor names (`fake.Calls[i].Args` vs a helper). If they differ, match the existing style — the assertions above are the contract, the accessor is whatever the file already uses.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/git/ -run TestStash`
Expected: FAIL — `StashPush` signature mismatch; `StashApply`/`StashDrop` undefined.

- [ ] **Step 3: Implement the verbs**

Rewrite the push/pop in `internal/git/stash.go` and add apply/drop/rev-parse:

```go
// StashPush saves the working-tree changes for the given paths (all changes
// when paths is empty) to a new stash with the message, leaving them reverted.
// includeUntracked adds -u so untracked paths are stashable.
func (r *Repo) StashPush(ctx context.Context, message string, paths []string, includeUntracked bool) error {
	b := gitcmd.New("stash").Arg("push", "-m", message).ArgIf(includeUntracked, "-u")
	if len(paths) > 0 {
		b = b.Arg("--").Arg(paths...)
	}
	_, err := r.Runner.Run(ctx, "git stash push", b.ToArgv())
	return err
}

// StashPop restores ref (newest when ref is "") and drops it. A conflict leaves
// the stash in place and returns an error (git's behavior).
func (r *Repo) StashPop(ctx context.Context, ref string) error {
	b := gitcmd.New("stash").Arg("pop").ArgIf(ref != "", ref)
	_, err := r.Runner.Run(ctx, "git stash pop", b.ToArgv())
	return err
}

// StashApply restores ref into the working tree, keeping the stash.
func (r *Repo) StashApply(ctx context.Context, ref string) error {
	_, err := r.Runner.Run(ctx, "git stash apply", gitcmd.New("stash").Arg("apply", ref).ToArgv())
	return err
}

// StashDrop deletes ref without applying it.
func (r *Repo) StashDrop(ctx context.Context, ref string) error {
	_, err := r.Runner.Run(ctx, "git stash drop", gitcmd.New("stash").Arg("drop", ref).ToArgv())
	return err
}

// StashCommit resolves a stash ref (e.g. stash@{0}) to its commit SHA so the
// file tree / diff can read it as an ordinary commit.
func (r *Repo) StashCommit(ctx context.Context, ref string) (string, error) {
	res, err := r.Runner.Run(ctx, "git rev-parse (stash)", gitcmd.New("rev-parse").Arg(ref).ToArgv())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
```

`StashList` is unchanged. Confirm `.Arg(paths...)` compiles (the builder's `Arg` is variadic `...string`); if the builder lacks slice expansion, append in a loop instead.

- [ ] **Step 4: Add the real-repo round-trip test**

```go
func TestStashPushByPathRoundTrip(t *testing.T) {
	r := newRepo(t) // existing helper: real git in t.TempDir() with an initial commit
	ctx := context.Background()
	// Both files must be TRACKED before a path-scoped stash without -u: git
	// stash push -- <untracked> errors ("did not match any file"). So commit a
	// baseline, then stash a MODIFICATION of one tracked file.
	writeFile(t, r, "keep.txt", "base\n")
	writeFile(t, r, "stashme.txt", "base\n")
	if err := r.AddAll(ctx); err != nil { // or: r.Runner.Run(ctx, "git add", gitcmd.New("add").Arg("-A").ToArgv())
		t.Fatal(err)
	}
	if err := r.Commit(ctx, "baseline", false); err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "keep.txt", "changed-keep\n")
	writeFile(t, r, "stashme.txt", "changed-stash\n")
	if err := r.StashPush(ctx, "WIP on main", []string{"stashme.txt"}, false); err != nil {
		t.Fatal(err)
	}
	// stashme reverted, keep.txt still dirty
	st, _ := r.Status(ctx)
	dirty := map[string]bool{}
	for _, f := range st.Files {
		dirty[f.Path] = true
	}
	if dirty["stashme.txt"] {
		t.Error("stashme.txt should have been stashed (reverted)")
	}
	if !dirty["keep.txt"] {
		t.Error("keep.txt should still be dirty")
	}
	list, _ := r.StashList(ctx)
	if len(list) != 1 {
		t.Fatalf("want 1 stash, got %d: %v", len(list), list)
	}
	if err := r.StashPop(ctx, ""); err != nil {
		t.Fatal(err)
	}
}
```

> Use whatever file-writing helper the package already has (grep `stash_test.go`/`mutate_test.go` for `writeFile`/`writeWorktreeFile`). If none, write with `os.WriteFile(filepath.Join(r.Dir, name), …)` and `git add` is not needed (working-tree change is enough).

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/git/ -run TestStash`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/git/stash.go internal/git/stash_test.go
git commit -m "feat(git): stash push by path, pop/apply/drop by ref, rev-parse"
```

## Task 2: GitOps interface + engine ops

**Files:**
- Modify: `internal/engine/gitops.go`, `internal/engine/ops_basic.go`
- Test: `internal/engine/ops_basic_test.go`

- [ ] **Step 1: Update the GitOps interface**

In `internal/engine/gitops.go`, replace the three stash lines (currently :33-35) with:

```go
	StashList(ctx context.Context) ([]string, error)
	StashPush(ctx context.Context, message string, paths []string, includeUntracked bool) error
	StashPop(ctx context.Context, ref string) error
	StashApply(ctx context.Context, ref string) error
	StashDrop(ctx context.Context, ref string) error
	StashCommit(ctx context.Context, ref string) (string, error)
```

(The `var _ GitOps = (*git.Repo)(nil)` line proves `*git.Repo` now satisfies it.)

- [ ] **Step 2: Fix the auto-stash call sites (compile fix)**

`StashPush`/`StashPop` are called by the smart ops. Update each call to the new signatures, **preserving today's behavior exactly**:
- `internal/engine/smart_merge.go`, `smart_pull.go`, `smart_rebase.go`, `smart_switch.go`: `StashPush(ctx, "gg-autostash:"+X)` → `StashPush(ctx, "gg-autostash:"+X, nil, false)`; `StashPop(ctx)` → `StashPop(ctx, "")`.

**Pass `false`, not `true`.** The original `StashPush` had no `-u`, so the auto-stash never included untracked files; passing `true` would silently change four shipped ops (and likely break their tests). `nil` paths = whole tree, matching the old call. Run `go build ./...` and fix exactly the reported call sites — there are 4 push and ~5 pop sites (see spec's "What already exists").

- [ ] **Step 3: Write the failing op tests**

Add to `internal/engine/ops_basic_test.go`:

```go
func TestStashOpPassesPathsAndUntracked(t *testing.T) {
	repo := &recordingRepo{} // see note below
	op := Stash{Message: "WIP on main", Paths: []string{"a.go"}, IncludeUntracked: true}
	if _, err := op.Run(context.Background(), newOpDeps(repo)); err != nil {
		t.Fatal(err)
	}
	if repo.pushMsg != "WIP on main" || !repo.pushUntracked || len(repo.pushPaths) != 1 {
		t.Errorf("StashPush got msg=%q paths=%v untracked=%v", repo.pushMsg, repo.pushPaths, repo.pushUntracked)
	}
}

func TestStashApplyPopDropOps(t *testing.T) {
	repo := &recordingRepo{}
	deps := newOpDeps(repo)
	if _, err := (StashApply{Ref: "stash@{1}"}).Run(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if _, err := (StashPop{Ref: "stash@{1}"}).Run(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if _, err := (StashDrop{Ref: "stash@{1}"}).Run(context.Background(), deps); err != nil {
		t.Fatal(err)
	}
	if repo.applyRef != "stash@{1}" || repo.popRef != "stash@{1}" || repo.dropRef != "stash@{1}" {
		t.Errorf("refs: apply=%q pop=%q drop=%q", repo.applyRef, repo.popRef, repo.dropRef)
	}
}
```

> `recordingRepo`/`newOpDeps`: the package already tests ops against a fake `GitOps`. Find the existing fake (grep `ops_basic_test.go` and neighbors for a struct implementing `GitOps`, e.g. `fakeRepo`/`stubOps`). Add the new fields (`pushMsg`, `pushPaths`, `pushUntracked`, `applyRef`, `popRef`, `dropRef`) and methods to it rather than creating a new one. If there is genuinely no shared fake, create a minimal one implementing only the methods these ops call (it can embed `*git.Repo`-less stubs returning nil).

- [ ] **Step 4: Run to verify failure**

Run: `go test ./internal/engine/ -run TestStash`
Expected: FAIL — `Stash` has no `Paths`/`IncludeUntracked`; `StashApply`/`StashPop`/`StashDrop` undefined.

- [ ] **Step 5: Implement the ops**

In `internal/engine/ops_basic.go`, replace the `Stash` block and add three ops:

```go
// Stash saves the working-tree changes for Paths (all when empty) to a new stash.
type Stash struct {
	Message          string
	Paths            []string
	IncludeUntracked bool
}

func (op Stash) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "stashing", Detail: op.Message})
	if err := deps.Repo.StashPush(ctx, op.Message, op.Paths, op.IncludeUntracked); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "stashed: " + op.Message, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// StashApply restores a stash, keeping it in the list.
type StashApply struct{ Ref string }

func (op StashApply) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "applying stash", Detail: op.Ref})
	if err := deps.Repo.StashApply(ctx, op.Ref); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "applied " + op.Ref, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// StashPop restores a stash and drops it.
type StashPop struct{ Ref string }

func (op StashPop) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "popping stash", Detail: op.Ref})
	if err := deps.Repo.StashPop(ctx, op.Ref); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "popped " + op.Ref, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// StashDrop deletes a stash without applying it.
type StashDrop struct{ Ref string }

func (op StashDrop) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "dropping stash", Detail: op.Ref})
	if err := deps.Repo.StashDrop(ctx, op.Ref); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "dropped " + op.Ref, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

Extend the compile-time block:

```go
var (
	_ Operation = Commit{}
	_ Operation = Push{}
	_ Operation = Stash{}
	_ Operation = StashApply{}
	_ Operation = StashPop{}
	_ Operation = StashDrop{}
)
```

- [ ] **Step 6: Run to verify pass**

Run: `go test ./internal/engine/`
Expected: PASS (all ops, smart-op tests still green with updated call sites).

- [ ] **Step 7: Commit**

```bash
git add internal/engine/
git commit -m "feat(engine): Stash by path; StashApply/StashPop/StashDrop ops"
```

## Task 3: model.StashEntry + domain reads

**Files:**
- Modify: `internal/model/model.go`
- Create: `internal/domain/stash.go`, `internal/domain/stash_test.go`
- Test: `internal/model/model_test.go`

- [ ] **Step 1: Add the model type + a parser test**

In `internal/model/model.go` add:

```go
// StashEntry is one stash list row: its ref (stash@{N}) and human description.
type StashEntry struct {
	Ref     string // "stash@{0}"
	Subject string // text after the ref, e.g. "On main: WIP on main"
}
```

- [ ] **Step 2: Write the failing domain parser test**

Create `internal/domain/stash_test.go`:

```go
package domain

import (
	"reflect"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestParseStashList(t *testing.T) {
	in := []string{
		"stash@{0}: On main: WIP on main",
		"stash@{1}: WIP on feat: 1a2b3c add api",
		"   ", // blank-ish line ignored
	}
	got := parseStashList(in)
	want := []model.StashEntry{
		{Ref: "stash@{0}", Subject: "On main: WIP on main"},
		{Ref: "stash@{1}", Subject: "WIP on feat: 1a2b3c add api"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseStashList = %+v, want %+v", got, want)
	}
}

func TestParseStashListEmpty(t *testing.T) {
	if got := parseStashList(nil); len(got) != 0 {
		t.Errorf("nil → %+v, want empty", got)
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/domain/ -run TestParseStashList`
Expected: FAIL — `parseStashList` undefined.

- [ ] **Step 4: Implement the parser + gated reads**

Create `internal/domain/stash.go`:

```go
package domain

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/model"
)

// parseStashList splits each "stash@{N}: <subject>" line into a StashEntry.
// Lines without the "<ref>: " shape (e.g. blanks) are skipped.
func parseStashList(lines []string) []model.StashEntry {
	var out []model.StashEntry
	for _, ln := range lines {
		ref, subject, ok := strings.Cut(ln, ": ")
		if !ok || !strings.HasPrefix(strings.TrimSpace(ref), "stash@{") {
			continue
		}
		out = append(out, model.StashEntry{Ref: strings.TrimSpace(ref), Subject: subject})
	}
	return out
}

// StashList returns parsed stash entries (newest first) under a Read reservation.
func (s *Service) StashList(ctx context.Context) ([]model.StashEntry, error) {
	return query(ctx, s, "stash-list", func(ctx context.Context) ([]model.StashEntry, error) {
		lines, err := s.repo.StashList(ctx)
		if err != nil {
			return nil, err
		}
		return parseStashList(lines), nil
	})
}

// StashCommit resolves a stash ref to its commit SHA, under a Read reservation.
func (s *Service) StashCommit(ctx context.Context, ref string) (string, error) {
	return query(ctx, s, "stash-commit:"+ref, func(ctx context.Context) (string, error) {
		return s.repo.StashCommit(ctx, ref)
	})
}
```

> `s.repo` must expose `StashList`/`StashCommit`. Check the interface `s.repo` is typed against (grep `type … interface` in `internal/domain`, likely `repoReader` or it is `*git.Repo`/`engine.GitOps`). Add `StashList(ctx)([]string,error)` and `StashCommit(ctx,ref)(string,error)` to that interface if it is a narrow one. `StashList` already exists on `git.Repo`; `StashCommit` was added in Task 1.

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/domain/ ./internal/model/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/model/model.go internal/model/model_test.go internal/domain/stash.go internal/domain/stash_test.go
git commit -m "feat(domain): StashList/StashCommit gated reads + parser"
```

## Task 4: Status multi-mark

**Files:**
- Modify: `internal/tui/model.go` (Model struct + `handleMarkKey` routing + `reRoot`), `internal/tui/mark.go`, `internal/tui/view.go` (`renderPanel`)
- Test: `internal/tui/mark_test.go` (or `stash_test.go` in tui)

- [ ] **Step 1: Add the field**

In the `Model` struct (`internal/tui/model.go`, near the `mark *markState` field), add:

```go
	fileMarks map[string]bool // multi-selected Status file paths (keyed by path)
```

Initialize lazily (a nil map reads fine; write through a helper). In `reRoot` (model.go:652), add `m.fileMarks = nil` next to `m.mark = nil`.

- [ ] **Step 2: Write the failing test**

Create `internal/tui/stash_mark_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func statusModel() Model {
	m := Model{width: 100, height: 30, focus: panelStatus, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "a.go", Unstaged: 'M'},
		{Path: "b.go", Unstaged: 'M'},
	}}
	return m
}

func TestStatusMMultiMarks(t *testing.T) {
	m := statusModel()
	m.sel[panelStatus] = 0
	mm, _ := m.handleMarkKey()
	m = mm.(Model)
	m.sel[panelStatus] = 1
	mm, _ = m.handleMarkKey()
	m = mm.(Model)
	if !m.fileMarks["a.go"] || !m.fileMarks["b.go"] {
		t.Fatalf("both files should be marked, got %v", m.fileMarks)
	}
	// toggling a.go off leaves b.go
	m.sel[panelStatus] = 0
	mm, _ = m.handleMarkKey()
	m = mm.(Model)
	if m.fileMarks["a.go"] || !m.fileMarks["b.go"] {
		t.Fatalf("a.go should toggle off, b.go stay, got %v", m.fileMarks)
	}
}

func TestBranchesMUnchanged(t *testing.T) {
	m := Model{width: 100, height: 30, focus: panelBranches, sel: map[panel]int{}}
	m.branches = []model.Branch{{Name: "main"}, {Name: "feat"}}
	m.sel[panelBranches] = 0
	mm, _ := m.handleMarkKey()
	if mm.(Model).mark == nil {
		t.Fatal("Branches m must still set the single mark")
	}
}
```

> Match `m.branches`/`m.status` field names and `model.Branch`/`FileStatus` shapes to the package (grep an existing tui test such as `blame_view_test.go` which uses `model.WorkingTreeStatus{Files: …}`). Adjust if `backingIndex(panelStatus)`/`listFor(panelStatus).Key(i)` need a populated panel list to resolve — if the test can't resolve the selected row, set up via the same helper other Status tests use.

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestStatusMMultiMarks|TestBranchesMUnchanged'`
Expected: FAIL — Status `m` does not populate `fileMarks`.

- [ ] **Step 4: Branch handleMarkKey for Status**

In `internal/tui/mark.go`, at the top of `handleMarkKey` (after resolving `bi`/`key`), add the Status branch before the single-mark logic:

```go
	if m.focus == panelStatus {
		if m.fileMarks == nil {
			m.fileMarks = map[string]bool{}
		}
		if m.fileMarks[key] {
			delete(m.fileMarks, key)
		} else {
			m.fileMarks[key] = true
		}
		return m, nil
	}
```

`key` is `m.listFor(m.focus).Key(bi)`, which for Status is the file path (same stable key the single mark uses). The existing single-mark code below stays for the other panels.

- [ ] **Step 5: Render the multi-marks**

In `internal/tui/view.go` `renderPanel` (:258-278), generalize the marker. Replace the `markedInWin := -1 … if i == markedInWin` logic with a set lookup:

```go
		marked := m.markedDisplayIndices(p)
		win, selInWin, start := windowRows(rows, rowsCap, m.sel[p])
		for i, row := range win {
			focused := i == selInWin && m.panelFocused(p)
			prefix := "  "
			if marked[start+i] {
				prefix = "◆ "
			} else if focused {
				prefix = "> "
			}
			line := padRight(truncate(prefix+row, innerW), innerW)
			if focused {
				line = selectedRow.Render(line)
			}
			lines = append(lines, line)
		}
```

Add the helper in `internal/tui/mark.go`:

```go
// markedDisplayIndices returns the set of display-row indices in panel p that
// carry a marker: the single mark, plus every Status fileMark.
func (m Model) markedDisplayIndices(p panel) map[int]bool {
	out := map[int]bool{}
	if md := m.markDisplayIndex(p); md >= 0 {
		out[md] = true
	}
	if p == panelStatus && len(m.fileMarks) > 0 {
		l := m.listFor(p)
		_, idx := m.panelView(p)
		for n, i := range idx {
			if m.fileMarks[l.Key(i)] {
				out[n] = true
			}
		}
	}
	return out
}
```

- [ ] **Step 6: Run to verify pass**

Run: `go test ./internal/tui/ -run 'TestStatusMMultiMarks|TestBranchesMUnchanged'` then `go test ./internal/tui/`
Expected: PASS (existing single-mark render tests still green — the helper returns the same single index for non-Status panels).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/mark.go internal/tui/model.go internal/tui/view.go internal/tui/stash_mark_test.go
git commit -m "feat(tui): multi-mark Status files with m (fileMarks set)"
```

## Task 5: Stash-create popup (`s`)

**Files:**
- Create: `internal/tui/stash_popup.go`, `internal/tui/stash_popup_test.go`
- Modify: `internal/tui/model.go` (`s` dispatch + routing + ctrl+s handling), `internal/tui/view.go` (render the popup)

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/stash_popup_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

func TestSOpensStashPopupWithCandidates(t *testing.T) {
	m := statusModel() // from stash_mark_test.go: a.go, b.go both unstaged 'M'
	m.status.Branch = "main"
	mm, _ := m.Update(keyMsg("s"))
	got := mm.(Model)
	if got.stashPopup == nil {
		t.Fatal("s on Status should open the stash popup")
	}
	if got.stashPopup.name != "WIP on main" {
		t.Errorf("default name = %q, want %q", got.stashPopup.name, "WIP on main")
	}
	if len(got.stashPopup.files) != 2 {
		t.Fatalf("want 2 candidate files, got %d", len(got.stashPopup.files))
	}
	for _, f := range got.stashPopup.files { // nothing marked → all checked
		if !f.included {
			t.Errorf("%s should default to included", f.path)
		}
	}
}

func TestStashPopupPrechecksMarks(t *testing.T) {
	m := statusModel()
	m.status.Branch = "main"
	m.fileMarks = map[string]bool{"a.go": true}
	mm, _ := m.Update(keyMsg("s"))
	p := mm.(Model).stashPopup
	inc := map[string]bool{}
	for _, f := range p.files {
		inc[f.path] = f.included
	}
	if !inc["a.go"] || inc["b.go"] {
		t.Errorf("only a.go should be pre-checked, got %v", inc)
	}
}

func TestStashPopupCtrlSStashesCheckedPaths(t *testing.T) {
	m := statusModel()
	m.status.Branch = "main"
	m.fileMarks = map[string]bool{"a.go": true}
	mm, _ := m.Update(keyMsg("s"))
	m = mm.(Model)
	captured := captureStartOp(t, &m) // see note
	mm, _ = m.updateStashPopupKey(keyMsg("ctrl+s"))
	op, ok := captured().(engine.Stash)
	if !ok {
		t.Fatalf("ctrl+s should start engine.Stash, got %T", captured())
	}
	if op.Message != "WIP on main" || len(op.Paths) != 1 || op.Paths[0] != "a.go" {
		t.Errorf("stash op = %+v", op)
	}
	if mm.(Model).stashPopup != nil {
		t.Error("popup should close on submit")
	}
}

func TestStashPopupEmptySelectionRefuses(t *testing.T) {
	m := statusModel()
	m.status.Branch = "main"
	mm, _ := m.Update(keyMsg("s"))
	m = mm.(Model)
	for i := range m.stashPopup.files { // uncheck all
		m.stashPopup.files[i].included = false
	}
	mm, _ = m.updateStashPopupKey(keyMsg("ctrl+s"))
	if mm.(Model).stashPopup == nil {
		t.Fatal("empty selection must not submit/close")
	}
}

func TestSNoCandidatesNoOp(t *testing.T) {
	m := Model{width: 100, height: 30, focus: panelStatus, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Branch: "main"} // no files
	mm, _ := m.Update(keyMsg("s"))
	if mm.(Model).stashPopup != nil {
		t.Fatal("s with nothing to stash should not open the popup")
	}
}
```

> `keyMsg` is the package's existing key-event helper (used in `blame_view_test.go`). For `captureStartOp`: if the package already has a way to inspect the op a `tea.Cmd` carries, use it. Otherwise, assert indirectly — have the test call `updateStashPopupKey` and check `m.running == true` and `m.stashPopup == nil`, and add a separate unit test on a pure helper `m.stashPopup.op(branch)` returning the `engine.Stash` value (recommended: implement `op()` below and test it directly, dropping the cmd-capture).

**Recommended:** make the op assembly a pure method and test it directly (simplest, no cmd capture):

```go
func TestStashPopupOpAssembly(t *testing.T) {
	p := &stashPopup{name: "WIP on main", files: []stashFileItem{
		{path: "a.go", included: true, untracked: false},
		{path: "b.go", included: false},
		{path: "c.txt", included: true, untracked: true},
	}}
	op, ok := p.op()
	if !ok {
		t.Fatal("op should be ok with ≥1 included")
	}
	if op.Message != "WIP on main" || len(op.Paths) != 2 || !op.IncludeUntracked {
		t.Errorf("op = %+v (want a.go,c.txt + untracked)", op)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run Stash`
Expected: FAIL — `stashPopup`/`stashFileItem`/`updateStashPopupKey` undefined.

- [ ] **Step 3: Implement the popup type, open, op assembly**

Create `internal/tui/stash_popup.go`:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

// stashFileItem is one candidate file in the stash-create popup.
type stashFileItem struct {
	path      string
	included  bool
	untracked bool
}

// stashPopup is the create-stash dialog: a name field plus a checklist of the
// working tree's unstaged/untracked files.
type stashPopup struct {
	name  string
	files []stashFileItem
	field int // 0 = name, 1 = file list
	sel   int // cursor in the file list
}

// op assembles the engine.Stash for the currently-checked files. ok is false
// when nothing is checked (caller refuses to submit).
func (p *stashPopup) op() (engine.Stash, bool) {
	var paths []string
	untracked := false
	for _, f := range p.files {
		if f.included {
			paths = append(paths, f.path)
			untracked = untracked || f.untracked
		}
	}
	if len(paths) == 0 {
		return engine.Stash{}, false
	}
	return engine.Stash{Message: p.name, Paths: paths, IncludeUntracked: untracked}, true
}

// stashCandidates returns the files eligible for stashing: untracked files and
// files with unstaged content (a fully-staged file is excluded). Order follows
// the Status list.
func stashCandidates(st model.WorkingTreeStatus) []stashFileItem {
	var out []stashFileItem
	for _, f := range st.Files {
		untracked := f.Kind == model.KindUntracked
		hasUnstaged := untracked || (f.Unstaged != '.' && f.Unstaged != 0)
		if f.Kind == model.KindUnmerged || !hasUnstaged {
			continue
		}
		out = append(out, stashFileItem{path: f.Path, untracked: untracked})
	}
	return out
}

// openStashPopup builds the popup. Returns (model, false) when nothing is
// eligible to stash.
func (m Model) openStashPopup() (Model, bool) {
	cand := stashCandidates(m.status)
	if len(cand) == 0 {
		return m, false
	}
	anyMarked := len(m.fileMarks) > 0
	for i := range cand {
		cand[i].included = !anyMarked || m.fileMarks[cand[i].path]
	}
	name := "WIP on " + m.status.Branch
	m.stashPopup = &stashPopup{name: name, files: cand, field: 1}
	return m, true
}

// updateStashPopupKey routes one key while the popup is open. It swallows every
// key (no fallthrough), per the popup checklist.
func (m Model) updateStashPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.stashPopup
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case "esc":
		m.stashPopup = nil
		return m, nil
	case "ctrl+s":
		op, ok := p.op()
		if !ok {
			m.statusMsg = "select at least one file"
			return m, nil
		}
		if op.Message == "" {
			op.Message = "WIP on " + m.status.Branch
		}
		stashed := map[string]bool{}
		for _, path := range op.Paths {
			stashed[path] = true
		}
		for path := range m.fileMarks { // clear marks we just stashed
			if stashed[path] {
				delete(m.fileMarks, path)
			}
		}
		m.stashPopup = nil
		return m.startOp(op)
	case "tab", "shift+tab":
		p.field = 1 - p.field
		return m, nil
	}
	if p.field == 1 { // file list
		switch msg.String() {
		case "up", "k":
			if p.sel > 0 {
				p.sel--
			}
		case "down", "j":
			if p.sel < len(p.files)-1 {
				p.sel++
			}
		case " ", "space":
			if p.sel >= 0 && p.sel < len(p.files) {
				p.files[p.sel].included = !p.files[p.sel].included
			}
		}
		return m, nil
	}
	// name field
	switch msg.Type {
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(p.name); len(r) > 0 {
			p.name = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		p.name += " "
	case tea.KeyRunes:
		p.name += string(msg.Runes)
	}
	return m, nil
}
```

Add the `stashPopup *stashPopup` field to the `Model` struct (next to `pairPopup`).

- [ ] **Step 4: Wire the `s` key and routing**

In `internal/tui/model.go`, the `case "s":` (currently :342, the SmartSwitch arm) — extend it to handle the Status panel:

```go
		case "s":
			if m.focus == panelStatus && m.opsIdle() {
				if mm, ok := m.openStashPopup(); ok {
					return mm, nil
				}
				m.statusMsg = "nothing to stash"
				return m, nil
			}
			if m.canSwitchBranch() {
				b, _ := m.selectedBranch()
				return m.startOp(engine.SmartSwitch{Branch: b.Name})
			}
```

Add to the routing chain (model.go ~:271, alongside the other popups — place before `pairPopup`):

```go
			if m.stashPopup != nil {
				return m.updateStashPopupKey(msg)
			}
```

> `m.opsIdle()` is the existing gate (used by staging). If it doesn't exist by that name, use the same guard the `S` arm uses today: `!m.running && !m.loading`.

- [ ] **Step 5: Render the popup**

The popup is composited exactly like the pair-op popup: the render method returns a `modalStyle`-framed body string, and `render()` overlays it via `overlayCenter`. First, wire it in `internal/tui/view.go` `render()` — find the existing pair-op overlay (`if m.pairPopup != nil { w,h := m.overlayDims(); return overlayCenter(bg, m.renderPairOpPopup(), w, h) }`) and add a sibling **before** it (action popup is added in Chunk C):

```go
	if m.stashPopup != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderStashPopup(), w, h)
	}
```

Then implement the body in `stash_popup.go`, mirroring `renderPairOpPopup` (`internal/tui/pairop_popup.go`) — `modalStyle.Width(popupInnerWidth(w)).Render(body) + "\n"`, with `selectedRow.Render` on the cursor row:

```go
func (m Model) renderStashPopup() string {
	p := m.stashPopup
	var b strings.Builder
	nameCursor := ""
	if p.field == 0 {
		nameCursor = "▏"
	}
	b.WriteString("Stash changes\n\nname: " + p.name + nameCursor + "\n\n")
	for i, f := range p.files {
		box := "[ ]"
		if f.included {
			box = "[x]"
		}
		row := box + " " + f.path
		if p.field == 1 && i == p.sel {
			b.WriteString(selectedRow.Render("> "+row) + "\n")
		} else {
			b.WriteString("  " + row + "\n")
		}
	}
	b.WriteString("\n[space] toggle  [tab] name/files  [ctrl+s] stash  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
```

(`modalStyle`, `popupInnerWidth`, `overlayCenter`, `overlayDims`, `selectedRow` all already exist — see `renderPairOpPopup`. Add `"strings"` to the file's imports.)

- [ ] **Step 6: Run to verify pass**

Run: `go test ./internal/tui/ -run Stash` then `go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/stash_popup.go internal/tui/stash_popup_test.go internal/tui/model.go internal/tui/view.go
git commit -m "feat(tui): stash-create popup (s) with file checklist + default name"
```

## Task 6: Chunk A docs + footer/help + merge gate

**Files:** `internal/tui/footer.go`, `internal/tui/help.go`, `CHANGELOG.md`

- [ ] **Step 1: Footer + help rows**

In `internal/tui/footer.go` `contextBindings`, add (next to the `[space] stage` entry):

```go
	{"s", "[s] stash", func(m Model) bool { return m.focus == panelStatus && m.opsIdle() && len(stashCandidates(m.status)) > 0 }},
	{"m", "[m] mark", func(m Model) bool { return m.focus == panelStatus && m.canStage() }},
```

In `internal/tui/help.go`, add a Status-panel row for `s` ("stash marked/unstaged files") and `m` ("mark a file for stashing"). If `TestHelpFooterCoverage` enforces footer↔help parity, ensure every new footer key has a help row.

- [ ] **Step 2: CHANGELOG**

Under `## [Unreleased]` → `### Added`:
```
- TUI: `m` multi-marks files in the Status panel; `s` opens a stash-create popup (name defaults to `WIP on <branch>`, file checklist, `ctrl+s` stashes).
```

- [ ] **Step 3: Verify the whole suite with race**

Run: `./test.sh race`
Expected: PASS. (This is the Chunk A merge gate — a reviewer can merge here: stashing works and is visible via `git stash list`, even before the viewer lands.)

- [ ] **Step 4: Commit**

```bash
git add internal/tui/footer.go internal/tui/help.go CHANGELOG.md
git commit -m "docs(tui): advertise s=stash, m=mark in footer/help + CHANGELOG"
```

---

# CHUNK B — View path (Tasks 7–10)

Outcome: `S` opens the stash list in the right column; `j/k` moves; `l` drills into the file tree (left column) reusing the commit-files view, so diff/`h`/blame work; `esc`/`S` close.

## Task 7: stashView state + load + `S` opener

**Files:**
- Create: `internal/tui/stash_view.go`, `internal/tui/stash_view_test.go`
- Modify: `internal/tui/model.go` (`Model` field, `S` dispatch, msg handling, `reRoot`)

- [ ] **Step 1: Write the failing test**

Create `internal/tui/stash_view_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestCapitalSOpensStashView(t *testing.T) {
	m := Model{width: 100, height: 30, focus: panelStatus, sel: map[panel]int{}}
	mm, cmd := m.Update(keyMsg("S"))
	got := mm.(Model)
	if got.stashView == nil {
		t.Fatal("S should open the stash view")
	}
	if cmd == nil {
		t.Error("opening the stash view should fire its load cmd")
	}
}

func TestStashListAppliedToView(t *testing.T) {
	m := Model{width: 100, height: 30, sel: map[panel]int{}}
	m.stashView = &stashView{loading: true, tag: "stash"}
	entries := []model.StashEntry{{Ref: "stash@{0}", Subject: "On main: WIP on main"}}
	mm, _ := m.Update(stashListMsg{tag: "stash", entries: entries})
	got := mm.(Model)
	if got.stashView.loading || len(got.stashView.entries) != 1 {
		t.Fatalf("entries not applied: %+v", got.stashView)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'StashView|StashListApplied'`
Expected: FAIL — `stashView`/`stashListMsg` undefined.

- [ ] **Step 3: Implement state, load cmd, opener, msg handling**

Create `internal/tui/stash_view.go`:

```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

// stashView is the stash list, rendered in the right column (over Commits).
type stashView struct {
	entries []model.StashEntry
	sel     int
	loading bool
	err     error
	tag     string // gates stale loads
}

type stashListMsg struct {
	tag     string
	entries []model.StashEntry
	err     error
}

func (m Model) loadStashListCmd(tag string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		es, err := svc.StashList(context.Background())
		return stashListMsg{tag: tag, entries: es, err: err}
	}
}

// openStashView opens (or refreshes) the stash list window.
func (m Model) openStashView() (Model, tea.Cmd) {
	m.stashView = &stashView{loading: true, tag: "stash"}
	return m, m.loadStashListCmd(m.stashView.tag)
}
```

Add the field to `Model` (next to `filesView`): `stashView *stashView`.

In `model.go`, add the `S` arm (replace the current `case "S":` whole-tree stash at :347):

```go
		case "S":
			if m.opsIdle() {
				return m.openStashView()
			}
```

Add the `stashListMsg` handler in `Update` (next to `statusRefreshedMsg`, model.go:577):

```go
	case stashListMsg:
		if m.stashView == nil || msg.tag != m.stashView.tag {
			return m, nil
		}
		m.stashView.loading = false
		if msg.err != nil {
			m.stashView.err = msg.err
			return m, nil
		}
		m.stashView.entries = msg.entries
		if m.stashView.sel >= len(msg.entries) {
			m.stashView.sel = max(0, len(msg.entries)-1)
		}
		return m, nil
```

In `reRoot` (model.go:652), add `m.stashView = nil` next to `m.filesView = nil`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/tui/ -run 'StashView|StashListApplied'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/stash_view.go internal/tui/stash_view_test.go internal/tui/model.go
git commit -m "feat(tui): stash list state + S opener + async load"
```

## Task 8: Render the stash list in the right column

**Files:** `internal/tui/stash_view.go`, `internal/tui/view.go`
**Test:** `internal/tui/stash_view_test.go`

- [ ] **Step 1: Write the failing render test**

```go
func TestStashViewRendersInRightColumn(t *testing.T) {
	m := Model{width: 100, height: 30, sel: map[panel]int{}, status: model.WorkingTreeStatus{Branch: "main"}}
	m.stashView = &stashView{entries: []model.StashEntry{
		{Ref: "stash@{0}", Subject: "On main: WIP on main"},
		{Ref: "stash@{1}", Subject: "On feat: sketch"},
	}}
	out := m.View()
	if !contains(out, "Stashes") {
		t.Errorf("right column should be titled Stashes:\n%s", out)
	}
	if !contains(out, "WIP on main") || !contains(out, "sketch") {
		t.Errorf("stash subjects missing:\n%s", out)
	}
}
```

> `contains`/`View()` are the package's existing test helpers (see `blame_view_test.go`).

- [ ] **Step 2: Run to verify failure** — FAIL (no "Stashes" box).

- [ ] **Step 3: Implement render + wire into the column switch**

In `internal/tui/stash_view.go`:

```go
// renderStashList renders the stash entries as a bordered right-column box of
// boxW×boxH, mirroring renderPanel's framing.
func (m Model) renderStashList(boxW, boxH int) string {
	rows := make([]string, len(m.stashView.entries))
	for i, e := range m.stashView.entries {
		rows[i] = e.Ref + "  " + e.Subject
	}
	switch {
	case m.stashView.loading:
		rows = []string{"(loading…)"}
	case m.stashView.err != nil:
		rows = []string{"error: " + m.stashView.err.Error()}
	case len(m.stashView.entries) == 0:
		rows = []string{"(no stashes)"}
	}
	return m.renderListBox("Stashes", rows, m.stashView.sel, boxW, boxH, m.filesView == nil)
}
```

`renderPanel` is panel-keyed (`m.sel[p]`, `m.panelFocused(p)`) so it can't render a non-panel list directly. Add this sibling helper next to `renderPanel` in `internal/tui/view.go`, reusing the same primitives (`windowRows`, `truncate`, `padRight`, `focusedPanel`/`bluredPanel`, `selectedRow`):

```go
// renderListBox draws a bordered boxW×boxH list that is not backed by a panel
// (used by the stash window). focused selects the border + highlight styles.
func (m Model) renderListBox(label string, rows []string, sel, boxW, boxH int, focused bool) string {
	contentH := boxH - 2
	if contentH < 1 {
		contentH = 1
	}
	innerW := boxW - 4
	if innerW < 1 {
		innerW = 1
	}
	rowsCap := contentH - 1
	if rowsCap < 0 {
		rowsCap = 0
	}
	lines := []string{padRight(truncate(label, innerW), innerW)}
	if rowsCap >= 1 && len(rows) > 0 {
		win, selInWin, _ := windowRows(rows, rowsCap, sel)
		for i, row := range win {
			prefix := "  "
			if i == selInWin && focused {
				prefix = "> "
			}
			line := padRight(truncate(prefix+row, innerW), innerW)
			if i == selInWin && focused {
				line = selectedRow.Render(line)
			}
			lines = append(lines, line)
		}
	}
	for len(lines) < contentH {
		lines = append(lines, padRight("", innerW))
	}
	style := bluredPanel
	if focused {
		style = focusedPanel
	}
	return style.Render(strings.Join(lines, "\n"))
}
```

In `internal/tui/view.go` `render()`, change the right-column line (:218) to:

```go
	var right string
	if m.stashView != nil {
		right = m.renderStashList(g.rightW, g.boxH[panelCommits])
	} else {
		right = m.renderPanel(panelCommits, m.panelLabel(panelCommits, "Commits"), cmRows, g.rightW, g.boxH[panelCommits])
	}
```

(The left column's existing `case m.filesView != nil` already shows the file tree when `l` is pressed — so the stash list on the right and the file tree on the left coexist.)

- [ ] **Step 4: Run to verify pass** — `go test ./internal/tui/ -run StashView` PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/stash_view.go internal/tui/view.go internal/tui/stash_view_test.go
git commit -m "feat(tui): render the stash list in the right column"
```

## Task 9: stashView key routing + `l` drills into the file tree

**Files:** `internal/tui/stash_view.go`, `internal/tui/model.go`
**Test:** `internal/tui/stash_view_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestStashViewNavAndClose(t *testing.T) {
	m := Model{width: 100, height: 30, sel: map[panel]int{}}
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}"}, {Ref: "stash@{1}"}}}
	mm, _ := m.updateStashViewKey(keyMsg("j"))
	if mm.(Model).stashView.sel != 1 {
		t.Fatal("j should move stash selection")
	}
	mm, _ = mm.(Model).updateStashViewKey(keyMsg("S"))
	if mm.(Model).stashView != nil {
		t.Fatal("S should close the stash view")
	}
}

func TestStashViewLLoadsFiles(t *testing.T) {
	m := Model{width: 100, height: 30, sel: map[panel]int{}}
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}", Subject: "On main: WIP"}}}
	mm, cmd := m.updateStashViewKey(keyMsg("l"))
	got := mm.(Model)
	if got.filesView == nil {
		t.Fatal("l should open the file tree for the stash")
	}
	if !got.filesTreeFocused {
		t.Error("the stash file tree should open focused (unlike commit follow-live)")
	}
	if cmd == nil {
		t.Error("l should fire the stash-files load cmd")
	}
}
```

- [ ] **Step 2: Run to verify failure** — FAIL (`updateStashViewKey` undefined).

- [ ] **Step 3: Implement routing + the file-tree drill**

In `internal/tui/stash_view.go`:

```go
type stashFilesMsg struct {
	tag   string // the stash ref
	sha   string
	lines []contentLine
	err   error
}

// loadStashFilesCmd resolves the stash ref to a SHA, then loads its changed
// files, tagged by ref for stale-gating.
func (m Model) loadStashFilesCmd(ref string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		sha, err := svc.StashCommit(context.Background(), ref)
		if err != nil {
			return stashFilesMsg{tag: ref, err: err}
		}
		files, err := svc.CommitFiles(context.Background(), sha)
		if err != nil {
			return stashFilesMsg{tag: ref, sha: sha, err: err}
		}
		return stashFilesMsg{tag: ref, sha: sha, lines: commitFileLines(files)}
	}
}

func (m Model) updateStashViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	v := m.stashView
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.String() {
	case "S", "esc":
		m.stashView = nil
		return m, nil
	case "down", "j":
		if v.sel < len(v.entries)-1 {
			v.sel++
		}
		return m, nil
	case "up", "k":
		if v.sel > 0 {
			v.sel--
		}
		return m, nil
	case "pgdown":
		v.sel += m.pageStep()
		if v.sel > len(v.entries)-1 {
			v.sel = len(v.entries) - 1
		}
		return m, nil
	case "pgup":
		if v.sel -= m.pageStep(); v.sel < 0 {
			v.sel = 0
		}
		return m, nil
	case "l":
		if v.sel < 0 || v.sel >= len(v.entries) {
			return m, nil
		}
		if m.width > 0 && m.width < 40 {
			m.statusMsg = "terminal too narrow for the files view"
			return m, nil
		}
		e := v.entries[v.sel]
		m.filesView = &contentPopup{lines: []contentLine{{text: "(loading…)"}}}
		m.filesTitle = "Files " + e.Ref + " " + e.Subject
		m.filesHash = "" // set when the SHA resolves
		m.filesTreeFocused = true
		m.filesStashTag = e.Ref
		return m, m.loadStashFilesCmd(e.Ref)
	case "enter":
		// stash-action popup — implemented in Chunk C; no-op until then.
		return m, nil
	}
	return m, nil
}
```

Add the `stashFilesMsg` apply-handler in `Update` (next to `stashListMsg`):

```go
	case stashFilesMsg:
		if m.stashView == nil || m.filesView == nil || msg.tag != m.filesStashTag {
			return m, nil
		}
		if msg.err != nil {
			m.statusMsg = "error: " + msg.err.Error()
			return m, nil
		}
		m.filesHash = msg.sha
		m.filesView.lines = msg.lines
		if m.filesView.sel >= len(msg.lines) {
			m.filesView.sel = 0
		}
		return m, nil
```

Add `filesStashTag string` to the `Model` struct (near `filesHash`). Clear it in `reRoot` and wherever `filesView` is set nil from the stash path.

Add routing in `model.go` — place **after** the `m.filesView != nil` check (model.go:275-277) so the file tree owns keys while open, and the stash list owns them when the tree is closed:

```go
			if m.stashView != nil {
				return m.updateStashViewKey(msg)
			}
```

> `pageStep()` is the existing paging helper (used by the filter block). `commitFileLines` and `contentPopup`/`contentLine` are the existing file-tree builders in `files_view.go`. Because `filesHash` becomes the resolved SHA, the existing `updateFilesViewKey` `enter`→diff / `h`→history / `b`→blame all operate on the stash commit unchanged — no new file-tree code.

- [ ] **Step 4: Run to verify pass** — `go test ./internal/tui/ -run StashView` PASS, then `go test ./internal/tui/`.

- [ ] **Step 5: Manual smoke (optional but recommended)**

```bash
go build ./cmd/gg && (cd $(mktemp -d) && git init -q && echo hi > a.txt && git add . && git -c user.email=t@t -c user.name=t commit -qm init && echo change >> a.txt && echo new > b.txt)
```
Then in a real dirty repo: `m m` on two files → `s` → `ctrl+s`; `S` shows the stash; `l` shows its files; `enter` on a file shows the diff; `b` blames. (Blame works because the stash is a commit.)

- [ ] **Step 6: Commit**

```bash
git add internal/tui/stash_view.go internal/tui/model.go internal/tui/stash_view_test.go
git commit -m "feat(tui): stash list nav + l drills into the file tree (reused view)"
```

## Task 10: Chunk B docs + footer/help + merge gate

**Files:** `internal/tui/footer.go`, `internal/tui/help.go`, `CHANGELOG.md`, `README.md`

- [ ] **Step 1: Footer + help**

Add a global footer binding `{"S", "[S] stashes", func(m Model) bool { return m.opsIdle() }}` (or gate on `m.stashView == nil`). Add a `help.go` "Stash window (S)" section: `l` files, `enter` actions, `j/k`/`pgup/pgdn` move, `esc`/`S` close. Keep `TestHelpFooterCoverage` green.

- [ ] **Step 2: CHANGELOG + README**

CHANGELOG `### Added`: "TUI: `S` opens a stash list window (right column); `l` shows a stash's files in the tree (diff/history/blame), like commit files." README: document `S`/`l` in the keybinding section.

- [ ] **Step 3: Merge gate** — `./test.sh race` PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/footer.go internal/tui/help.go CHANGELOG.md README.md
git commit -m "docs(tui): advertise S stash window + l files; CHANGELOG/README"
```

---

# CHUNK C — Manage path (Tasks 11–13)

Outcome: `Enter` on a stash opens an Apply / Pop / Drop popup; Drop confirms; the list refreshes after.

## Task 11: stash-action popup

**Files:**
- Create: `internal/tui/stash_action.go`, `internal/tui/stash_action_test.go`
- Modify: `internal/tui/model.go` (field + routing), `internal/tui/stash_view.go` (`enter` opens it), `internal/tui/view.go` (render)

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/stash_action_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

func TestEnterOpensActionPopup() {} // placeholder removed below

func TestStashEnterOpensActions(t *testing.T) {
	m := Model{width: 100, height: 30, sel: map[panel]int{}}
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}", Subject: "WIP"}}}
	mm, _ := m.updateStashViewKey(keyMsg("enter"))
	if mm.(Model).stashAction == nil {
		t.Fatal("enter should open the stash-action popup")
	}
	if mm.(Model).stashAction.ref != "stash@{0}" {
		t.Errorf("action popup ref = %q", mm.(Model).stashAction.ref)
	}
}

func TestStashActionApplyDispatches(t *testing.T) {
	m := Model{width: 100, height: 30, sel: map[panel]int{}}
	m.stashAction = &stashActionPopup{ref: "stash@{0}", sel: 0} // 0 = Apply
	mm, _ := m.updateStashActionKey(keyMsg("enter"))
	if mm.(Model).stashAction != nil {
		t.Error("apply should close the popup")
	}
	if !mm.(Model).running {
		t.Error("apply should start an op")
	}
}

func TestStashActionDropConfirms(t *testing.T) {
	m := Model{width: 100, height: 30, sel: map[panel]int{}}
	m.stashAction = &stashActionPopup{ref: "stash@{0}", sel: 2} // 2 = Drop
	mm, _ := m.updateStashActionKey(keyMsg("enter"))
	got := mm.(Model)
	if got.stashAction == nil || !got.stashAction.confirming {
		t.Fatal("drop should enter a confirm state, not run immediately")
	}
	mm, _ = got.updateStashActionKey(keyMsg("y"))
	if mm.(Model).stashAction != nil || !mm.(Model).running {
		t.Error("y should confirm drop and run the op")
	}
}
```

(Delete the empty `TestEnterOpensActionPopup` stub — it's only here to flag the rename; do not keep it.)

- [ ] **Step 2: Run to verify failure** — FAIL (`stashAction`/`stashActionPopup`/`updateStashActionKey` undefined).

- [ ] **Step 3: Implement the popup**

Create `internal/tui/stash_action.go`:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// stashActionPopup is the Apply/Pop/Drop menu for one stash.
type stashActionPopup struct {
	ref        string
	subject    string
	sel        int  // 0 Apply, 1 Pop, 2 Drop
	confirming bool // Drop awaiting y/n
}

var stashActions = []string{"Apply", "Pop", "Drop"}

func (m Model) updateStashActionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	a := m.stashAction
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if a.confirming {
		switch msg.String() {
		case "y":
			m.stashAction = nil
			return m.startOp(engine.StashDrop{Ref: a.ref})
		case "n", "esc":
			a.confirming = false
		}
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.stashAction = nil
		return m, nil
	case "up", "k":
		if a.sel > 0 {
			a.sel--
		}
	case "down", "j":
		if a.sel < len(stashActions)-1 {
			a.sel++
		}
	case "enter":
		switch a.sel {
		case 0:
			m.stashAction = nil
			return m.startOp(engine.StashApply{Ref: a.ref})
		case 1:
			m.stashAction = nil
			return m.startOp(engine.StashPop{Ref: a.ref})
		case 2:
			a.confirming = true
		}
	}
	return m, nil
}

func (m Model) renderStashActionPopup() string {
	a := m.stashAction
	w, _ := m.overlayDims()
	var b strings.Builder
	if a.confirming {
		b.WriteString("Drop " + a.ref + "?\n\n" + a.subject + "\n\n[y] drop   [n] cancel")
		return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
	}
	b.WriteString("Stash " + a.ref + "\n" + a.subject + "\n\n")
	for i, name := range stashActions {
		if i == a.sel {
			b.WriteString(selectedRow.Render("> "+name) + "\n")
		} else {
			b.WriteString("  " + name + "\n")
		}
	}
	b.WriteString("\n[enter] do  [esc] cancel")
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
```

(Add `"strings"` to the imports. Same `modalStyle`/`popupInnerWidth`/`selectedRow` pattern as `renderStashPopup`/`renderPairOpPopup`.)

Add `stashAction *stashActionPopup` to `Model`. Wire it into `render()` (`internal/tui/view.go`) as a centered overlay **before** the `stashPopup` branch (so an open action popup composites on top): `if m.stashAction != nil { w, h := m.overlayDims(); return overlayCenter(bg, m.renderStashActionPopup(), w, h) }`. In `internal/tui/stash_view.go`, replace the `enter` no-op:

```go
	case "enter":
		if v.sel < 0 || v.sel >= len(v.entries) {
			return m, nil
		}
		e := v.entries[v.sel]
		m.stashAction = &stashActionPopup{ref: e.Ref, subject: e.Subject}
		return m, nil
```

Add routing in `model.go` (before `stashPopup`/`stashView`, so the action popup is modal over the list):

```go
			if m.stashAction != nil {
				return m.updateStashActionKey(msg)
			}
```

Render it in `view.go` `render()` overlay section: `if m.stashAction != nil { … m.renderStashActionPopup() … }`.

- [ ] **Step 4: Run to verify pass** — `go test ./internal/tui/ -run StashAction` PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/stash_action.go internal/tui/stash_action_test.go internal/tui/stash_view.go internal/tui/model.go internal/tui/view.go
git commit -m "feat(tui): stash-action popup (Apply/Pop/Drop, Drop confirms)"
```

## Task 12: Refresh the stash list after an action

**Files:** `internal/tui/op.go` or `internal/tui/model.go` (the `opFinishedMsg` handler), `internal/tui/stash_view_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestStashListRefreshesAfterOp(t *testing.T) {
	m := Model{width: 100, height: 30, sel: map[panel]int{}}
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}"}}}
	// Simulate an op finishing while the stash window is open.
	mm, cmd := m.Update(opFinishedMsg{}) // use the real opFinishedMsg fields; summary optional
	_ = mm
	if cmd == nil {
		t.Fatal("op finishing with the stash window open should refresh the list")
	}
}
```

> Inspect `opFinishedMsg`'s real fields (`internal/tui/op.go:46`) and construct it accordingly. The assertion is: when `m.stashView != nil`, the post-op handler must also issue `loadStashListCmd`.

- [ ] **Step 2: Run to verify failure** — FAIL (no stash refresh on op finish).

- [ ] **Step 3: Implement**

In the `opFinishedMsg` handler (model.go, the block ending at :575 with `return m, m.loadCmd()`), when `m.stashView != nil`, batch the stash reload with the normal reload:

```go
	m.loadGen++
	if m.stashView != nil {
		m.stashView.loading = true
		return m, tea.Batch(m.loadCmd(), m.loadStashListCmd(m.stashView.tag))
	}
	return m, m.loadCmd()
```

(After an Apply/Pop/Drop the working tree changed → `loadCmd()` refreshes Status; the stash list changed → `loadStashListCmd` refreshes the window. If an open file tree now points at a dropped stash, the next `l`/selection re-resolves; a stale tree is harmless read-only.)

- [ ] **Step 4: Run to verify pass** — `go test ./internal/tui/` PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/stash_view_test.go
git commit -m "feat(tui): refresh the stash list after a stash op completes"
```

## Task 13: Chunk C docs + final verification

**Files:** `internal/tui/help.go`, `CHANGELOG.md`, `README.md`

- [ ] **Step 1: Help + docs**

`help.go` "Stash window" section: add `enter` = Apply/Pop/Drop actions. CHANGELOG `### Added`: "TUI: `Enter` in the stash window applies, pops, or drops the selected stash." README: note the action popup.

- [ ] **Step 2: Full verification**

Run: `./test.sh race`
Expected: PASS (vet+gofmt → unit → e2e).

- [ ] **Step 3: Commit**

```bash
git add internal/tui/help.go CHANGELOG.md README.md
git commit -m "docs(tui): stash-action popup in help/CHANGELOG/README"
```

- [ ] **Step 4: Finish the branch**

Use `superpowers:finishing-a-development-branch` to verify tests and present merge options.

---

## Self-Review

**Spec coverage:**
- Key remap (`m`/`s`/`S`) → Tasks 4, 5, 7. ✓
- Multi-mark Status → Task 4. ✓
- Stash-create popup (name default, checklist, marked pre-check, `ctrl+s`, fast path, empty/no-candidate guards) → Task 5. ✓
- Verbs (push paths/`-u`, pop/apply/drop by ref, rev-parse) → Task 1; ops → Task 2; auto-stash call-site fixes → Task 2 Step 2. ✓
- Domain `StashList`/`StashCommit` + parser → Task 3. ✓
- Stash list window in right column → Tasks 7–8; nav + `l` file tree reuse (diff/`h`/blame via resolved SHA) → Task 9. ✓
- Action popup Apply/Pop/Drop + Drop confirm → Task 11; refresh after → Task 12. ✓
- Footer/help/CHANGELOG/README → Tasks 6, 10, 13. ✓
- v1 limitation (untracked-only files absent from first-parent tree) → carried in the spec; no task needed (behavioral note, documented in README via Task 10).

**Placeholder scan:** No TBD/TODO. Every code step has real code. The few "match the existing helper" notes point at a named exemplar with a path:line — they resolve a naming detail, not the logic.

**Type consistency:** `StashPush(ctx, message, paths, includeUntracked)`, `StashPop(ctx, ref)`, `StashApply/Drop/Commit(ctx, ref)` used identically across git/GitOps/domain. `engine.Stash{Message,Paths,IncludeUntracked}`, `StashApply/StashPop/StashDrop{Ref}` consistent in ops + popups. `stashView`/`stashPopup`/`stashActionPopup`/`stashFileItem`/`model.StashEntry{Ref,Subject}` and the msgs (`stashListMsg`, `stashFilesMsg`) named consistently. `fileMarks`, `filesStashTag`, `filesHash`, `filesTreeFocused` match across tasks.
