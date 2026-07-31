# Worktree-from-commit (keep-changes modes) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a linked worktree anchored at a chosen commit, with two new "keep changes" variants that land the new branch on the commit's parent and leave the commit's own diff staged or unstaged in the new worktree — from the Commits panel and from `gg worktree add --from`.

**Architecture:** Extend the existing `engine.CreateWorktree` op with a `Keep` field (zero value = today's behavior) backed by a new dir-scoped `git.ResetInDir` verb; the TUI's existing `fromCommit` popup gains a mode line and a `<current-branch>_<short-sha>` prefill; the CLI gains `--from <commit> [--keep staged|unstaged] [<branch-name>]`. Root/merge commits are refused up front for the keep modes (pre-validated via `git.ParentCount` before anything is created).

**Tech Stack:** Go 1.26, Bubble Tea TUI, real-git tests in `t.TempDir()`, declarative e2e TOML scenarios.

**Spec:** `docs/superpowers/specs/2026-07-31-worktree-from-commit-design.md`

## Global Constraints

- **All work happens in the feature worktree** `/mnt/t/others/gigagit.worktrees/feat-worktree-from-commit` (branch `feat/worktree-from-commit`). Every Write/Edit uses that absolute path prefix; every build/test command is prefixed `cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit &&`. Subagents MUST cd there and verify `git branch --show-current` prints `feat/worktree-from-commit` before touching anything.
- Stage specific paths only (`gg add <paths>` / `git add <paths>`), NEVER `add -A`.
- Prefer `gg` for git operations (`gg add`, `gg commit -m`, `gg status`); raw git only where gg lacks the verb.
- Every user-visible TUI string goes through `i18n.T("<english literal>")` and the key must exist in ALL FOUR bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`) — AST gates (`i18n_scan_test.go`, `menu_labels_test.go`, `engine_prose_test.go`) fail the build otherwise. Engine `Result.Summary` English text is protocol (CLI/log surface) and additionally carries the localizable channel via the lockstep helpers (`WithSummary`/`AppendSummary`).
- Decision/option/op-name values stay English protocol; only rendered text is localized.
- Commit messages end with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01F6vE15h5QeiTe9BuvrJTCg`
- One git verb = one git invocation, argv built with `gitcmd`.

---

### Task 1: `git.ResetInDir` verb

**Files:**
- Modify: `internal/git/worktree.go` (append the verb)
- Test: `internal/git/worktree_verbs_test.go` (append)

**Interfaces:**
- Consumes: nothing new.
- Produces: `func (r *Repo) ResetInDir(ctx context.Context, dir, ref string, soft bool) error` — runs `git -C <dir> reset --soft|--mixed <ref>`. Task 2 adds it to `engine.GitOps` and calls it.

- [ ] **Step 1: Write the failing argv test** (FakeRunner pattern, mirroring `TestWorktreeRepairArgv` in the same file):

```go
func TestResetInDirArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git -C reset", gitexec.Result{})
	repo := &Repo{Runner: f}
	if err := repo.ResetInDir(context.Background(), "/x/wt", "abc123^", true); err != nil {
		t.Fatalf("ResetInDir soft: %v", err)
	}
	want := []string{"-C", "/x/wt", "reset", "--soft", "abc123^"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
	if err := repo.ResetInDir(context.Background(), "/x/wt", "abc123^", false); err != nil {
		t.Fatalf("ResetInDir mixed: %v", err)
	}
	want = []string{"-C", "/x/wt", "reset", "--mixed", "abc123^"}
	if !reflect.DeepEqual(f.Calls[1].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[1].Argv, want)
	}
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit && go test ./internal/git/ -run TestResetInDirArgv -v`
Expected: FAIL — `repo.ResetInDir undefined`.

- [ ] **Step 3: Implement the verb** (append to `internal/git/worktree.go`):

```go
// ResetInDir runs `git -C <dir> reset --soft|--mixed <ref>` — a reset scoped to
// another worktree's checkout (the ShowFileInDir -C precedent). Used by
// CreateWorktree's keep-changes modes right after the new worktree is created:
// --soft leaves the commit's changes staged there, --mixed leaves them
// unstaged. One invocation.
func (r *Repo) ResetInDir(ctx context.Context, dir, ref string, soft bool) error {
	mode := "--mixed"
	if soft {
		mode = "--soft"
	}
	argv := gitcmd.New("-C").Arg(dir, "reset", mode, ref).ToArgv()
	_, err := r.Runner.Run(ctx, "git -C reset", argv)
	return err
}
```

- [ ] **Step 4: Run the test, verify it passes**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit && go test ./internal/git/ -run TestResetInDirArgv -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit
gg add internal/git/worktree.go internal/git/worktree_verbs_test.go
gg commit -m "feat(git): ResetInDir dir-scoped reset verb

git -C <dir> reset --soft|--mixed <ref>, for resetting a freshly created
worktree without changing the Service's own cwd. Backs the coming
CreateWorktree keep-changes modes.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01F6vE15h5QeiTe9BuvrJTCg"
```

---

### Task 2: engine `CreateWorktree.Keep`

**Files:**
- Modify: `internal/engine/create_worktree.go`
- Modify: `internal/engine/gitops.go` (add `ParentCount` + `ResetInDir` to the interface)
- Modify: `internal/i18n/lang/ja.toml`, `ko.toml`, `zh.toml`, `ru.toml` (two summary-suffix keys)
- Test: `internal/engine/create_worktree_test.go` (append)

**Interfaces:**
- Consumes: `(*git.Repo).ResetInDir(ctx, dir, ref string, soft bool) error` (Task 1), existing `(*git.Repo).ParentCount(ctx, rev string) (int, error)` (`internal/git/format_patch.go:16`).
- Produces (Tasks 3–4 rely on these exact names):
  - `type WorktreeKeep int` with `KeepNone`/`KeepStaged`/`KeepUnstaged` consts (zero value `KeepNone` = today's behavior).
  - `CreateWorktree.Keep WorktreeKeep` field.
  - `type WorktreeKeepParentError struct { Sha string; Parents int }` with an `Error() string` method — returned (nothing created) when `Keep != KeepNone` and the start commit has 0 or ≥2 parents.

- [ ] **Step 1: Write the failing tests** (append to `internal/engine/create_worktree_test.go`; `newRepo` is the shared helper in `ops_basic_test.go` — it builds a real repo with an initial commit on `main` containing `README.md`). Helper + four tests:

```go
// addCommit writes path=content and commits it in dir, returning nothing; the
// keep-mode tests need a second commit so HEAD has exactly one parent.
func addCommit(t *testing.T, dir, path, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("add", path)
	run("commit", "-m", msg)
}

// gitOut runs git in dir and returns trimmed stdout.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func TestCreateWorktreeKeepStaged(t *testing.T) {
	dir, repo := newRepo(t)
	addCommit(t, dir, "a.txt", "two\n", "second")
	head := gitOut(t, dir, "rev-parse", "HEAD")
	parent := gitOut(t, dir, "rev-parse", "HEAD^")

	op := CreateWorktree{StartPoint: head, Branch: "redo/x", Path: "../wt-keep-staged", Keep: KeepStaged}
	res, err := op.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("CreateWorktree keep-staged: %v", err)
	}
	// The new branch must land on the PARENT, with the commit's diff staged.
	if got := gitOut(t, res.Path, "rev-parse", "HEAD"); got != parent {
		t.Fatalf("worktree HEAD = %s, want parent %s", got, parent)
	}
	status := gitOut(t, res.Path, "status", "--porcelain")
	if !strings.Contains(status, "A  a.txt") && !strings.Contains(status, "M  a.txt") {
		t.Fatalf("a.txt not staged in the new worktree; status:\n%s", status)
	}
	if !strings.Contains(res.Summary, "commit's changes staged") {
		t.Fatalf("Summary = %q, want the staged suffix", res.Summary)
	}
}

func TestCreateWorktreeKeepUnstaged(t *testing.T) {
	dir, repo := newRepo(t)
	addCommit(t, dir, "a.txt", "two\n", "second")
	head := gitOut(t, dir, "rev-parse", "HEAD")
	parent := gitOut(t, dir, "rev-parse", "HEAD^")

	op := CreateWorktree{StartPoint: head, Branch: "redo/y", Path: "../wt-keep-unstaged", Keep: KeepUnstaged}
	res, err := op.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("CreateWorktree keep-unstaged: %v", err)
	}
	if got := gitOut(t, res.Path, "rev-parse", "HEAD"); got != parent {
		t.Fatalf("worktree HEAD = %s, want parent %s", got, parent)
	}
	// --mixed: nothing staged, a.txt untracked-or-modified in the working tree.
	status := gitOut(t, res.Path, "status", "--porcelain")
	if strings.Contains(status, "A  a.txt") || strings.Contains(status, "M  a.txt") {
		t.Fatalf("a.txt unexpectedly staged; status:\n%s", status)
	}
	if !strings.Contains(status, "a.txt") {
		t.Fatalf("a.txt missing from the new worktree's status:\n%s", status)
	}
}

func TestCreateWorktreeKeepRefusesRootCommit(t *testing.T) {
	dir, repo := newRepo(t) // exactly one commit — HEAD is the root
	op := CreateWorktree{StartPoint: "HEAD", Branch: "redo/root", Path: "../wt-keep-root", Keep: KeepStaged}
	_, err := op.Run(context.Background(), OpDeps{Repo: repo})
	var wantErr WorktreeKeepParentError
	if !errors.As(err, &wantErr) || wantErr.Parents != 0 {
		t.Fatalf("err = %v, want WorktreeKeepParentError{Parents: 0}", err)
	}
	// Refusal happens BEFORE anything is created.
	if _, statErr := os.Stat(filepath.Join(dir, "..", "wt-keep-root")); statErr == nil {
		t.Fatal("worktree directory must not exist after the refusal")
	}
	if out := gitOut(t, dir, "branch", "--list", "redo/root"); out != "" {
		t.Fatalf("branch must not exist after the refusal, got %q", out)
	}
}

func TestCreateWorktreeKeepRefusesMergeCommit(t *testing.T) {
	dir, repo := newRepo(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("checkout", "-b", "side")
	addCommit(t, dir, "s.txt", "side\n", "side work")
	run("checkout", "main")
	addCommit(t, dir, "m.txt", "main\n", "main work")
	run("merge", "--no-ff", "-m", "merge side", "side")

	op := CreateWorktree{StartPoint: "HEAD", Branch: "redo/merge", Path: "../wt-keep-merge", Keep: KeepUnstaged}
	_, err := op.Run(context.Background(), OpDeps{Repo: repo})
	var wantErr WorktreeKeepParentError
	if !errors.As(err, &wantErr) || wantErr.Parents != 2 {
		t.Fatalf("err = %v, want WorktreeKeepParentError{Parents: 2}", err)
	}
}
```

Plus the hook-ordering test — the post-create hook must observe the ALREADY-RESET state (spec: "hook runs AFTER the reset"). The hook needs an approving decider (`HookDecisionID` → `"run"`); mirror `TestCreateWorktreeForBranchRunsHook` in `create_worktree_for_branch_test.go` for the exact decider/OpDeps shape used there and adapt:

```go
func TestCreateWorktreeKeepHookSeesResetState(t *testing.T) {
	dir, repo := newRepo(t)
	addCommit(t, dir, "a.txt", "two\n", "second")
	head := gitOut(t, dir, "rev-parse", "HEAD")

	// The hook snapshots status at hook time; if it ran before the reset the
	// tree would be clean and the file would be empty.
	op := CreateWorktree{StartPoint: head, Branch: "redo/hook", Path: "../wt-keep-hook",
		Keep: KeepStaged, PostCreateHook: "git status --porcelain > hook-status.txt"}
	res, err := op.Run(context.Background(), opDepsApprovingHook(repo)) // same deps shape TestCreateWorktreeForBranchRunsHook uses
	if err != nil {
		t.Fatalf("CreateWorktree keep+hook: %v", err)
	}
	out, rerr := os.ReadFile(filepath.Join(res.Path, "hook-status.txt"))
	if rerr != nil {
		t.Fatalf("hook did not run: %v", rerr)
	}
	if !strings.Contains(string(out), "a.txt") {
		t.Fatalf("hook ran before the reset — status at hook time:\n%s", out)
	}
}
```

(`opDepsApprovingHook` stands for however that existing test wires the run-approval decider + `ShellHookRunner` — reuse its exact construction rather than inventing a new one; extract a small shared helper if it is inline there.)

Add `"errors"`, `"os/exec"` to the test file's imports if missing.

- [ ] **Step 2: Run them, verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit && go test ./internal/engine/ -run 'TestCreateWorktreeKeep' -v`
Expected: FAIL — `KeepStaged`/`WorktreeKeepParentError` undefined.

- [ ] **Step 3: Implement.** In `internal/engine/gitops.go`, add to the `GitOps` interface (near `AddWorktree`, gitops.go:83):

```go
	// ParentCount reports how many parents rev has (0 root, 1 normal, ≥2
	// merge) — the keep-modes pre-check before any worktree is created.
	ParentCount(ctx context.Context, rev string) (int, error)
	// ResetInDir resets another worktree's checkout (git -C dir reset).
	ResetInDir(ctx context.Context, dir, ref string, soft bool) error
```

In `internal/engine/create_worktree.go`:

```go
// WorktreeKeep selects what the new worktree holds relative to StartPoint.
type WorktreeKeep int

const (
	KeepNone     WorktreeKeep = iota // branch at StartPoint (default)
	KeepStaged                       // branch at StartPoint^, commit's changes staged (reset --soft)
	KeepUnstaged                     // branch at StartPoint^, commit's changes unstaged (reset --mixed)
)

// WorktreeKeepParentError refuses a keep mode on a commit whose parent is
// missing (root) or ambiguous (merge). Returned BEFORE anything is created.
type WorktreeKeepParentError struct {
	Sha     string
	Parents int
}

func (e WorktreeKeepParentError) Error() string {
	if e.Parents == 0 {
		return fmt.Sprintf("create worktree: %s is a root commit — there is no parent to keep its changes against", e.Sha)
	}
	return fmt.Sprintf("create worktree: %s is a merge commit (%d parents) — its changes are ambiguous", e.Sha, e.Parents)
}
```

Add `Keep WorktreeKeep` to the `CreateWorktree` struct (document: "KeepStaged/KeepUnstaged land the branch on StartPoint's parent with the commit's diff left staged/unstaged in the new worktree; StartPoint must then be a non-root, non-merge commit").

In `Run`, after the required-fields check and before branch validation:

```go
	if op.Keep != KeepNone {
		n, err := deps.Repo.ParentCount(ctx, op.StartPoint)
		if err != nil {
			return Result{}, fmt.Errorf("create worktree: %w", err)
		}
		if n != 1 {
			return Result{}, WorktreeKeepParentError{Sha: op.StartPoint, Parents: n}
		}
	}
```

After the successful `AddWorktree` call and BEFORE `runPostCreateHook` (the hook must see the final state), replace the current result construction:

```go
	base := Result{Changed: true, Path: abs}.WithSummary("worktree created: %s", abs)
	if op.Keep != KeepNone {
		soft := op.Keep == KeepStaged
		mode := "--mixed"
		if soft {
			mode = "--soft"
		}
		deps.emit(ctx, Progress{Step: "resetting", Detail: mode + " → " + op.StartPoint + "^"})
		if err := deps.Repo.ResetInDir(ctx, abs, op.StartPoint+"^", soft); err != nil {
			// The parent count was pre-validated, so this is a near-impossible
			// failure; name the created path so the user knows what exists.
			return Result{}, fmt.Errorf("create worktree: created at %s but reset failed: %w", abs, err)
		}
		if soft {
			base = base.AppendSummary(" (commit's changes staged)")
		} else {
			base = base.AppendSummary(" (commit's changes unstaged)")
		}
	}
	res := runPostCreateHook(ctx, deps, base, abs, op.Branch, op.PostCreateHook)
```

(The plain `Progress{}` with a flag+rev Detail mirrors `reset.go:74` — data, not prose, so no new Step key; `"resetting"` is already in all bundles.)

- [ ] **Step 4: Add the two suffix keys to all four bundles** (`engine_prose_test.go` will fail without them). Insert each alphabetically near similar keys:

| key | ja | ko | zh | ru |
|---|---|---|---|---|
| `" (commit's changes staged)"` | `" (コミットの変更はステージ済み)"` | `" (커밋의 변경 사항은 스테이지됨)"` | `"（提交的更改已暂存）"` | `" (изменения коммита в индексе)"` |
| `" (commit's changes unstaged)"` | `" (コミットの変更は未ステージ)"` | `" (커밋의 변경 사항은 스테이지되지 않음)"` | `"（提交的更改未暂存）"` | `" (изменения коммита не в индексе)"` |

- [ ] **Step 5: Run engine + i18n + tui gates**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit && go test ./internal/engine/ -run 'TestCreateWorktree' -v && go test ./internal/tui/ -run 'TestEngineProse' && go build ./...`
Expected: all PASS (existing `CreateWorktree` tests still green — `Keep` zero value changes nothing).

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit
gg add internal/engine/create_worktree.go internal/engine/create_worktree_test.go internal/engine/gitops.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
gg commit -m "feat(engine): CreateWorktree keep-changes modes

Keep=KeepStaged/KeepUnstaged lands the new branch on StartPoint's parent
with the commit's own diff left staged/unstaged in the new worktree
(ResetInDir --soft/--mixed after creation, before the post-create hook).
Root and merge commits are refused up front via ParentCount — typed
WorktreeKeepParentError, nothing created. Summary carries a localized
suffix naming the mode.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01F6vE15h5QeiTe9BuvrJTCg"
```

---

### Task 3: TUI — popup mode line + branch prefill + row relabel

**Files:**
- Modify: `internal/tui/worktree_popup.go` (popup fields, `m` key, render, `createOp`, `openWorktreeAtCommit`)
- Modify: `internal/tui/commit_scope.go:747-763` (`commitCreateWorktreeRow`)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (new keys, one key renamed)
- Test: `internal/tui/worktree_popup_test.go`, `internal/tui/commit_scope_test.go` (append/adjust)

**Interfaces:**
- Consumes: `engine.WorktreeKeep` / `engine.KeepNone` / `engine.KeepStaged` / `engine.KeepUnstaged` (Task 2).
- Produces: `worktreePopup.keep engine.WorktreeKeep`, `keepOffered bool`, `keepLocked bool`; `func (m Model) openWorktreeAtCommit(hash, prefillBranch string, parents int) Model`. `createOp` passes `Keep: p.keep`.

- [ ] **Step 1: Write the failing tests.**

Append to `internal/tui/worktree_popup_test.go`:

```go
// TestWorktreeKeepModeCycles proves [m] cycles the three keep modes in
// stAction and is inert when the commit is root/merge (keepLocked).
func TestWorktreeKeepModeCycles(t *testing.T) {
	var m Model
	p := &worktreePopup{fromCommit: true, keepOffered: true, state: stAction}
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}}
	p.update(m, key)
	if p.keep != engine.KeepStaged {
		t.Fatalf("after one m: keep = %v, want KeepStaged", p.keep)
	}
	p.update(m, key)
	if p.keep != engine.KeepUnstaged {
		t.Fatalf("after two m: keep = %v, want KeepUnstaged", p.keep)
	}
	p.update(m, key)
	if p.keep != engine.KeepNone {
		t.Fatalf("after three m: keep = %v, want KeepNone (wrapped)", p.keep)
	}
	locked := &worktreePopup{fromCommit: true, keepOffered: true, keepLocked: true, state: stAction}
	locked.update(m, key)
	if locked.keep != engine.KeepNone {
		t.Fatalf("locked popup must ignore m; keep = %v", locked.keep)
	}
}

// TestWorktreeCreateOpCarriesKeep proves the chosen mode reaches the engine op.
func TestWorktreeCreateOpCarriesKeep(t *testing.T) {
	p := &worktreePopup{previewBranch: "redo/x", previewPath: "/tmp/x", keep: engine.KeepUnstaged}
	op, ok := p.createOp("").(engine.CreateWorktree)
	if !ok {
		t.Fatalf("op = %T, want engine.CreateWorktree", p.createOp(""))
	}
	if op.Keep != engine.KeepUnstaged {
		t.Fatalf("Keep = %v, want KeepUnstaged", op.Keep)
	}
}
```

In `internal/tui/commit_scope_test.go`, adjust `TestCommitCreateWorktreeRowOpensInEdit` (line ~650): the row's commit now needs `Parents` and the model a current branch, and the popup opens with a PREFILLED buffer and the keep fields set:

```go
	m.commits = []model.Commit{{Hash: full, Subject: "x", Parents: []string{"aaaaaaaa"}}}
	m.status.Branch = "main"
```

and replace the final empty-buffer assertion with:

```go
	if p.state != stEdit || p.editBuf.Value() != "main_"+full[:7] {
		t.Fatalf("should open in branch-edit prefilled; state=%v buf=%q", p.state, p.editBuf.Value())
	}
	if !p.keepOffered || p.keepLocked {
		t.Fatalf("keepOffered=%v keepLocked=%v, want offered and unlocked", p.keepOffered, p.keepLocked)
	}
```

Add a sibling test for the locked/detached case:

```go
func TestCommitCreateWorktreeRowRootCommitLocksKeep(t *testing.T) {
	m := footerModel()
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	m.focus = panelCommits
	full := "dddddddddddddddddddddddddddddddddddddddd"
	m.commits = []model.Commit{{Hash: full, Subject: "root"}} // no parents
	m.sel[panelCommits] = 0
	m.status.Branch = "(detached)"
	r, _ := findRow(availableActions(m), "commit-create-worktree")
	mm, _ := r.run(m)
	m = mm.(Model)
	p := m.topLayer().(*worktreePopup)
	if !p.keepLocked {
		t.Fatal("a root commit must lock the keep mode")
	}
	if p.editBuf.Value() != "wt_"+full[:7] {
		t.Fatalf("detached prefill = %q, want wt_%s", p.editBuf.Value(), full[:7])
	}
}
```

- [ ] **Step 2: Run them, verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit && go test ./internal/tui/ -run 'TestWorktreeKeep|TestWorktreeCreateOpCarriesKeep|TestCommitCreateWorktree' -v`
Expected: FAIL — `keepOffered` undefined etc.

- [ ] **Step 3: Implement.**

In `worktreePopup` struct (worktree_popup.go, after `runHook`):

```go
	// keep-changes mode (Commits-panel entrance only). keepOffered shows the
	// mode line; keepLocked (root/merge commit) pins it to "at this commit".
	keep        engine.WorktreeKeep
	keepOffered bool
	keepLocked  bool
```

Split `openWorktreeAt` so the Commits entrance can set the fields before the push — the existing body moves verbatim into `worktreeAtPopup` (returning `p` instead of pushing), then:

```go
func (m Model) openWorktreeAt(startPoint, prefillBranch string) Model {
	return m.pushLayer(m.worktreeAtPopup(startPoint, prefillBranch))
}

// openWorktreeAtCommit is the Commits-panel entrance: same popup, plus the
// keep-changes mode line. parents is the commit's parent count from the feed
// (no git call); != 1 locks the mode to "at this commit".
func (m Model) openWorktreeAtCommit(hash, prefillBranch string, parents int) Model {
	p := m.worktreeAtPopup(hash, prefillBranch)
	p.keepOffered = true
	p.keepLocked = parents != 1
	return m.pushLayer(p)
}
```

In `update`, `stAction` switch, add:

```go
		case "m":
			if p.keepOffered && !p.keepLocked {
				p.keep = (p.keep + 1) % 3
			}
			return m, nil
```

In `box`, right after the hook-toggle block (before the `switch p.state` hints):

```go
	if p.state == stAction && p.keepOffered {
		line := i18n.T("start:  ") + keepModeLabel(p.keep)
		if p.keepLocked {
			line += "  " + i18n.T("(root/merge commit — at this commit only)")
		} else {
			line += "  " + i18n.T("([m] change)")
		}
		b.WriteString(line + "\n")
	}
```

with the label helper (same file):

```go
// keepModeLabel renders one keep mode for the popup's mode line.
func keepModeLabel(k engine.WorktreeKeep) string {
	switch k {
	case engine.KeepStaged:
		return i18n.T("at parent, changes staged")
	case engine.KeepUnstaged:
		return i18n.T("at parent, changes unstaged")
	}
	return i18n.T("at this commit")
}
```

In `createOp`, add `Keep: p.keep` to the `engine.CreateWorktree` literal.

In `commit_scope.go` `commitCreateWorktreeRow`, capture the parents and prefill, relabel the row:

```go
	hash := m.commits[bi].Hash
	parents := len(m.commits[bi].Parents)
	short := hash
	if len(short) > 7 {
		short = short[:7]
	}
	prefill := "wt_" + short
	if b := m.status.Branch; b != "" && b != "(detached)" {
		prefill = b + "_" + short
	}
	return actionRow{
		id:    "commit-create-worktree",
		label: i18n.T("Create worktree from this commit"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openWorktreeAtCommit(hash, prefill, parents), nil
		},
	}, true
```

- [ ] **Step 4: Update the four bundles.** RENAME the key `"Create worktree here"` → `"Create worktree from this commit"` (delete the old key — the orphan gate flags unused keys) and ADD the new keys:

| key | ja | ko | zh | ru |
|---|---|---|---|---|
| `"Create worktree from this commit"` | `"このコミットからワークツリーを作成"` | `"이 커밋에서 워크트리 생성"` | `"从此提交创建工作树"` | `"Создать рабочее дерево из этого коммита"` |
| `"start:  "` | `"開始:  "` | `"시작:  "` | `"起点:  "` | `"старт:  "` |
| `"at this commit"` | `"このコミットで"` | `"이 커밋에서"` | `"在此提交"` | `"на этом коммите"` |
| `"at parent, changes staged"` | `"親コミットで、変更はステージ済み"` | `"부모 커밋에서, 변경 사항 스테이지됨"` | `"在父提交，更改已暂存"` | `"на родителе, изменения в индексе"` |
| `"at parent, changes unstaged"` | `"親コミットで、変更は未ステージ"` | `"부모 커밋에서, 변경 사항 스테이지 안 됨"` | `"在父提交，更改未暂存"` | `"на родителе, изменения не в индексе"` |
| `"([m] change)"` | `"([m] 変更)"` | `"([m] 변경)"` | `"（[m] 更改）"` | `"([m] изменить)"` |
| `"(root/merge commit — at this commit only)"` | `"(ルート/マージコミット — このコミットのみ)"` | `"(루트/병합 커밋 — 이 커밋에서만)"` | `"（根/合并提交 — 仅限此提交）"` | `"(корневой или merge-коммит — только на этом коммите)"` |

- [ ] **Step 5: Run the TUI package (all gates live there)**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit && go test ./internal/tui/`
Expected: PASS — including `i18n_scan_test`, `menu_labels_test`, `options_vocab_test`. If the orphan check names other stale keys, only touch the one this task renamed.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit
gg add internal/tui/worktree_popup.go internal/tui/worktree_popup_test.go internal/tui/commit_scope.go internal/tui/commit_scope_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml
gg commit -m "feat(tui): worktree-from-commit keep modes + branch prefill

The Commits-panel row (now 'Create worktree from this commit') prefills
the branch as <current-branch>_<short-sha> (wt_<short-sha> detached) and
the popup gains an [m]-cycled mode line: at this commit / at parent
staged / at parent unstaged — locked to 'at this commit' for root and
merge commits (parent count from the feed, no git call).

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01F6vE15h5QeiTe9BuvrJTCg"
```

---

### Task 4: CLI `--from`/`--keep` + e2e scenarios

**Files:**
- Modify: `internal/cli/worktree.go` (`cmdWorktreeAdd`, worktree.go:60-120; also the `worktree` usage/help lines — grep `"worktree add"` in `internal/cli` and update every usage string that lists its flags)
- Create: `e2e/scenarios/s85_worktree_from_commit.toml`
- Create: `e2e/scenarios/s86_worktree_from_commit_errors.toml`

**Interfaces:**
- Consumes: `engine.CreateWorktree{... Keep}` (Task 2), `svc.CommitLookup(ctx, rev) (model.LogLine, bool, error)` (`domain/query.go:518`; `LogLine.Hash` is the SHORT sha from `%h`), `svc.CurrentBranch`.
- Produces: `gg worktree add --from <commit> [--keep staged|unstaged] [<branch-name>]` — flags precede the positional; in `--from` mode the positional is the NEW BRANCH NAME (not a start-point).

- [ ] **Step 1: Write the failing e2e scenarios.** First invoke the `writing-e2e-scenarios` skill and verify the exact `[[expect.log]]` / `[expect.worktree."path".status]` field syntax against it and existing scenarios (e.g. `grep -l "expect.log" e2e/scenarios/`); adjust the sketches below to match, keeping the asserted FACTS identical.

`e2e/scenarios/s85_worktree_from_commit.toml`:

```toml
name = "worktree add --from --keep: branch at parent, changes staged"

[input]
steps = [
  { write = "a.txt", content = "one\n" },
  { commit = "first" },
  { write = "a.txt", content = "two\n" },
  { commit = "second" },
]

[[run]]
cmd  = ["worktree", "add", "--from", "HEAD", "--keep", "staged", "redo"]
exit = 0

[expect]
branch    = "main"                # the current checkout is untouched
branches  = ["main", "redo"]
worktrees = ["wt/redo"]

[[expect.log]]                    # the new branch's tip is the PARENT
branch   = "redo"
subjects = ["first"]

[expect.worktree."wt/redo".status]
staged = ["a.txt"]

[expect.worktree."wt/redo".files]
"a.txt" = "two\n"                 # the commit's content, kept in the tree
```

`e2e/scenarios/s86_worktree_from_commit_errors.toml`:

```toml
name = "worktree add --from/--keep: usage and root-commit refusals"

[input]
steps = [
  { write = "a.txt", content = "one\n" },
  { commit = "only" },
]

[[run]]                            # --keep without --from: usage error
cmd  = ["worktree", "add", "--keep", "staged"]
exit = 2

[[run]]                            # bad --keep value: usage error
cmd  = ["worktree", "add", "--from", "HEAD", "--keep", "later"]
exit = 2

[[run]]                            # --from + --branch: usage error
cmd  = ["worktree", "add", "--from", "HEAD", "--branch", "main"]
exit = 2

[[run]]                            # keep mode on the root commit: refused, nothing created
cmd  = ["worktree", "add", "--from", "HEAD", "--keep", "unstaged"]
exit = 1

[expect]
branch    = "main"
branches  = ["main"]               # no branch was created by any failed run
worktrees = []
```

- [ ] **Step 2: Run them, verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit && ./test.sh e2e 2>&1 | tail -20`
Expected: s85/s86 FAIL (`flag provided but not defined: -from`); every other scenario still green.

- [ ] **Step 3: Implement in `cmdWorktreeAdd`.** Add the flags next to the existing ones:

```go
	fromRev := fs.String("from", "", "create the worktree from this commit (new branch named <current-branch>_<short-sha> unless a name is given)")
	keepFlag := fs.String("keep", "", "with --from: leave the commit's changes in the new worktree ('staged' or 'unstaged'); the branch lands on the commit's parent")
```

After `fs.Parse` / the existing mutual-exclusion checks, add validation:

```go
	if *keepFlag != "" && *keepFlag != "staged" && *keepFlag != "unstaged" {
		fmt.Fprintf(stderr, "worktree add: --keep must be 'staged' or 'unstaged', got %q\n", *keepFlag)
		return 2
	}
	if *keepFlag != "" && *fromRev == "" {
		fmt.Fprintln(stderr, "worktree add: --keep requires --from")
		return 2
	}
	if *fromRev != "" && *forBranch != "" {
		fmt.Fprintln(stderr, "worktree add: --from and --branch are mutually exclusive (--from always creates a new branch)")
		return 2
	}
```

In the start-point/branch resolution: when `*fromRev != ""`, the start point is the resolved commit, the branch template is bypassed (the `--branch` pattern), and the positional (if any) is the branch name:

```go
	fromBranch := ""
	if *fromRev != "" {
		if len(args) > 1 {
			fmt.Fprintln(stderr, "worktree add: at most one branch name after --from")
			return 2
		}
		line, ok, err := svc.CommitLookup(ctxBg, *fromRev)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if !ok {
			fmt.Fprintf(stderr, "worktree add: unknown revision %q\n", *fromRev)
			return 1
		}
		startPoint = line.Hash // short sha — stable even if the ref moves
		fromBranch = ""
		if len(args) > 0 {
			fromBranch = args[0]
		}
		if fromBranch == "" {
			if cur, cerr := svc.CurrentBranch(ctxBg); cerr == nil && cur != "" && cur != "(detached)" {
				fromBranch = cur + "_" + line.Hash
			} else {
				fromBranch = "wt_" + line.Hash
			}
		}
	}
```

(Reorder so this replaces the "explicit arg = start point" branch when `--from` is set; the plain positional-start-point path is untouched when `--from` is empty.) Bypass the branch template like `--branch` does:

```go
	if *forBranch != "" || fromBranch != "" {
		tm = worktree.Templates{Path: cfg.Worktree.PathTemplate}
	}
```

and resolve with the fixed name (the existing `worktree.Resolve(tm, *forBranch, inputs, ctx)` call becomes):

```go
	fixed := *forBranch
	if fromBranch != "" {
		fixed = fromBranch
	}
	branch, path, err := worktree.Resolve(tm, fixed, inputs, ctx)
```

Finally the op:

```go
	keep := engine.KeepNone
	switch *keepFlag {
	case "staged":
		keep = engine.KeepStaged
	case "unstaged":
		keep = engine.KeepUnstaged
	}
	var op engine.Operation = engine.CreateWorktree{StartPoint: startPoint, Branch: branch, Path: path, PostCreateHook: hook, Keep: keep}
```

Update the worktree usage/help strings that enumerate `worktree add` flags to mention `--from`/`--keep` and the branch-name positional.

- [ ] **Step 4: Run the full staged gate**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit && ./test.sh 2>&1 | tail -15`
Expected: vet+gofmt, unit, and e2e all PASS (s85/s86 included).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit
gg add internal/cli/worktree.go e2e/scenarios/s85_worktree_from_commit.toml e2e/scenarios/s86_worktree_from_commit_errors.toml
gg commit -m "feat(cli): gg worktree add --from <commit> [--keep staged|unstaged]

--from creates the worktree on a new branch at the commit (default name
<current-branch>_<short-sha>, positional overrides); --keep lands the
branch on the parent with the commit's changes left staged/unstaged.
Usage errors for --keep without --from, bad --keep values, and
--from+--branch; root/merge commits fail loud via the engine's typed
refusal. e2e s85/s86 cover the happy path and the refusals.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01F6vE15h5QeiTe9BuvrJTCg"
```

---

### Task 5: Docs + using-gg v57

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`
- Modify: `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (Version 56 → 57)
- Modify: `.claude/skills/using-gg/SKILL.md` (regenerated copy)

**Interfaces:** none — prose only. CLI flag names exactly as shipped in Task 4.

- [ ] **Step 1: CHANGELOG.md** — add an entry under the current unreleased/top section describing: Commits-panel "Create worktree from this commit" with the three start modes and the `<current-branch>_<short-sha>` prefill; `gg worktree add --from <commit> [--keep staged|unstaged] [<branch-name>]`; root/merge refusal for keep modes.

- [ ] **Step 2: README.md** — extend the worktree feature blurb/CLI reference with the same surface (mirror the CHANGELOG content, README voice).

- [ ] **Step 3: CLAUDE.md** — package-map touch-ups: `engine` row (CreateWorktree's `Keep` modes + `WorktreeKeepParentError` + pre-validated ParentCount + reset-before-hook), `git` row (`ResetInDir`), `cli` row (`--from`/`--keep`). Keep each addition to a sentence or two in the existing style.

- [ ] **Step 4: using-gg.md + version.** In `internal/agentskill/using-gg.md` extend the worktree bullet (line ~275) to document `gg worktree add --from <commit> [--keep staged|unstaged] [<branch-name>]`. Bump `internal/agentskill/agentskill.go` `Version = 56` → `57`. ALSO: read the memory file `/home/homeend/.claude/projects/-mnt-t-others-gigagit/memory/conflict-complete-feature.md` — it records a known `when_op` doc drift in using-gg.md earmarked "next v57"; fix that drift in this same bump.

- [ ] **Step 5: Regenerate `.claude/skills/using-gg/SKILL.md`** — it is the committed installed copy: 5-line frontmatter (unchanged) + blank line + `<!-- gg:using-gg:v57 -->` + blank line + the full new `using-gg.md` body. Rebuild it exactly that way and verify: `head -8 .claude/skills/using-gg/SKILL.md` shows the v57 marker and `diff <(tail -n +8 .claude/skills/using-gg/SKILL.md) internal/agentskill/using-gg.md` is empty (adjust the tail offset if the header layout differs — the rule is frontmatter+marker, then byte-identical body).

- [ ] **Step 6: Build + commit**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit && go build ./... && go test ./internal/agentskill/`

```bash
cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit
gg add CHANGELOG.md README.md CLAUDE.md internal/agentskill/using-gg.md internal/agentskill/agentskill.go .claude/skills/using-gg/SKILL.md
gg commit -m "docs: worktree-from-commit surface; using-gg v57

CHANGELOG/README/CLAUDE.md for the keep-changes worktree modes; using-gg
documents worktree add --from/--keep (and picks up the pending when_op
doc-drift fix), Version 56 → 57 with the SKILL.md copy regenerated.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01F6vE15h5QeiTe9BuvrJTCg"
```

---

### Task 6: Final gates

- [ ] **Step 1: Full staged suite**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit && ./test.sh 2>&1 | tail -15`
Expected: vet+gofmt → unit → e2e all PASS.

- [ ] **Step 2: Race gate** (quiet machine — run detached and poll, per project convention):

```bash
cd /mnt/t/others/gigagit.worktrees/feat-worktree-from-commit
nohup ./test.sh race > /tmp/claude-1000/-mnt-t-others-gigagit/d7b0d0f1-d30d-4a04-a493-1b2e0c512867/scratchpad/race.log 2>&1 &
```

Poll the log until it finishes; a 10-minute package timeout usually names an innocent test — rerun on a quieter machine rather than chasing it.

- [ ] **Step 3: Stop and report.** Do NOT merge — the human owns merging into `main`. Report the branch, the commits, and the race-gate result, and offer the built binary path if a manual TUI check is wanted (`cd <worktree> && go build ./cmd/gg`).
