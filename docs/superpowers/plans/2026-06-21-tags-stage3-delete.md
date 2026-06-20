# Tags — Stage 3 (Delete) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans.

**Goal:** Delete a tag from the Tags-tab `.` menu (behind a confirm modal) and via `gg tag rm <name>`.

**Architecture:** git verb `DeleteTag` → decision-free engine op `DeleteTag{Name}` → Tags-tab `.`-menu row that opens the existing confirm `decisionState` modal (never-trap: Cancel) → `gg tag rm` CLI (typing the command is the confirmation, like `gg branch delete`). Post-op reload refreshes the Tags list.

**Tech Stack:** Go 1.26, Bubble Tea, gitcmd/gitexec, real-git tests, TOML e2e.

## Global Constraints
- One git invocation per verb; tui/cli never import internal/git.
- Confirm lives in the TUI modal (mirrors the discard `d` flow); the engine op is decision-free; the CLI deletes directly.
- Worktree `/mnt/t/others/gg-tags3` (branch `tags-stage3-delete`).
- CLI surface change ⇒ bump `agentskill.Version` 19→20 + `gg init --update`.

---

### Task 1: git `DeleteTag` verb + GitOps

**Files:** `internal/git/mutate.go` (add after `CreateTag`), `internal/engine/gitops.go` (interface), `internal/git/tag_delete_test.go`.

- [ ] **Step 1: Failing test** — `internal/git/tag_delete_test.go`:
```go
package git

import (
	"context"
	"testing"
)

func TestRepoDeleteTag(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	gitIn(t, dir, "tag", "v1.0.0")
	if err := repo.DeleteTag(context.Background(), "v1.0.0"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if out := gitOutIn(t, dir, "tag", "-l"); out != "" {
		t.Fatalf("tag still present: %q", out)
	}
}
```
- [ ] **Step 2: Run — FAIL** (`repo.DeleteTag undefined`).
- [ ] **Step 3: Verb** — in `internal/git/mutate.go` after `CreateTag`:
```go
// DeleteTag deletes a tag (git tag -d). git errors if it does not exist.
func (r *Repo) DeleteTag(ctx context.Context, name string) error {
	_, err := r.Runner.Run(ctx, "git tag -d", gitcmd.New("tag").Arg("-d", name).ToArgv())
	return err
}
```
- [ ] **Step 4: GitOps** — in `internal/engine/gitops.go` after `CreateTag`:
```go
	DeleteTag(ctx context.Context, name string) error
```
- [ ] **Step 5: Run — PASS.**
- [ ] **Step 6: Commit** `feat(git): DeleteTag verb + GitOps entry`.

---

### Task 2: engine `DeleteTag` op

**Files:** `internal/engine/delete_tag.go`, `internal/engine/delete_tag_test.go`.

- [ ] **Step 1: Failing test** — `internal/engine/delete_tag_test.go`:
```go
package engine

import (
	"context"
	"strings"
	"testing"
)

func TestDeleteTag(t *testing.T) {
	dir, repo := newRepo(t)
	if err := repo.CreateTag(context.Background(), "v1.0.0", "", ""); err != nil {
		t.Fatal(err)
	}
	ch := make(chan Event, 16)
	res, err := DeleteTag{Name: "v1.0.0"}.Run(context.Background(), OpDeps{Repo: repo, Events: ch})
	close(ch)
	if err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "v1.0.0") {
		t.Fatalf("result = %+v", res)
	}
	if out := engineCatType(t, dir, "v1.0.0"); out != "" {
		t.Fatalf("tag still resolvable: %q", out)
	}
}

func TestDeleteTagRequiresName(t *testing.T) {
	_, repo := newRepo(t)
	ch := make(chan Event, 4)
	if _, err := (DeleteTag{}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err == nil {
		t.Fatal("empty name must error")
	}
}
```
NOTE: `engineCatType` exists from Stage 2's `create_tag_test.go` (same package). For a missing ref `git cat-file -t` errors → it `t.Fatalf`s; instead assert deletion via `exec` rev-parse returning non-zero. Simplest: replace the cat-type check with `if exec.Command("git","-C",dir,"rev-parse","--verify","refs/tags/v1.0.0").Run()==nil { t.Fatal("tag ref still present") }` and import `os/exec`.
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Op** — `internal/engine/delete_tag.go`:
```go
package engine

import (
	"context"
	"fmt"
)

// DeleteTag deletes a tag. Decision-free: a missing tag surfaces as a git error.
type DeleteTag struct{ Name string }

func (op DeleteTag) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Name == "" {
		return Result{}, fmt.Errorf("delete tag: Name is required")
	}
	deps.emit(ctx, Progress{Step: "deleting tag", Detail: op.Name})
	if err := deps.Repo.DeleteTag(ctx, op.Name); err != nil {
		return Result{}, fmt.Errorf("delete tag: %w", err)
	}
	res := Result{Summary: "deleted tag " + op.Name, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = DeleteTag{}
```
- [ ] **Step 4: Run — PASS.**
- [ ] **Step 5: Commit** `feat(engine): DeleteTag op (decision-free)`.

---

### Task 3: TUI — `tagDeleteRow` + confirm modal

**Files:** `internal/tui/tags_actions.go` (add `tagDeleteRow`), `internal/tui/action_menu.go` (register), `internal/tui/tags_actions_test.go` (add tests).

**Interfaces:** Produces `(Model).tagDeleteRow() (actionRow, bool)` — gated on `focus==panelTags && opsIdle()`, whose `run` opens a `decisionState` confirm; "Delete" → `startOp(engine.DeleteTag{Name})`.

- [ ] **Step 1: Failing test** — append to `internal/tui/tags_actions_test.go`:
```go
func TestTagDeleteRowOpensConfirmThenDeletes(t *testing.T) {
	dir, repo := newRepoDir(t)
	gitRunTUI(t, dir, "tag", "v1.0.0") // raw-git helper; see NOTE
	m := loadModel(t, repo)
	m.focus = panelTags
	m.activeFilesTab = panelTags
	m.sel[panelTags] = 0

	row, ok := m.tagDeleteRow()
	if !ok {
		t.Fatal("delete row must appear on the Tags panel with a selection")
	}
	u, _ := row.run(m)
	m = u.(Model)
	if m.modal == nil {
		t.Fatal("delete must open a confirm modal")
	}
	// Resolve "Delete".
	um, cmd := m.modal.onResolve(m, "Delete")
	m = um.(Model)
	for i := 0; i < 100 && m.running; i++ {
		uu, next := m.Update(cmd())
		m = uu.(Model)
		cmd = next
	}
	if exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/tags/v1.0.0").Run() == nil {
		t.Fatal("tag should be gone after confirm")
	}
}

func TestTagDeleteRowInertOffTagsPanel(t *testing.T) {
	_, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches
	if _, ok := m.tagDeleteRow(); ok {
		t.Fatal("delete row must be inert off the Tags panel")
	}
}
```
NOTE: add a raw-git helper if the tui test package lacks one — `gitRunTUI(t, dir, args...)` = `exec.Command("git", append([]string{"-C",dir}, args...)...).Run()` with a fatal on error; import `os/exec`. Check whether `loadModel` loads the snapshot tags (it runs `loadCmd()` → Snapshot, which includes Tags from Stage 1) so `m.tags` is populated; if the tag is created AFTER loadModel, reload or set `m.tags` directly. Simplest: create the tag BEFORE `loadModel`.
- [ ] **Step 2: Run — FAIL** (`m.tagDeleteRow undefined`).
- [ ] **Step 3: Add `tagDeleteRow`** — in `internal/tui/tags_actions.go`:
```go
// tagDeleteRow offers "Delete tag" on the Tags panel: confirm, then delete.
func (m Model) tagDeleteRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-delete",
		label: "Delete tag",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.modal = &decisionState{
				req: engine.DecisionRequest{
					ID:      "delete-tag",
					Prompt:  "Delete tag " + name + "?",
					Options: []string{"Delete", "Cancel"},
				},
				onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
					if opt == "Delete" {
						return m.startOp(engine.DeleteTag{Name: name})
					}
					return m, nil
				},
			}
			return m, nil
		},
	}, true
}
```
(Confirm `tags_actions.go`'s imports include `tea` and `engine`; the file already imports `tea` from Stage 1's `tagJumpToCommit` — add `engine`.)
- [ ] **Step 4: Register** — in `internal/tui/action_menu.go` `availableActions`, after the commit rows block:
```go
	if r, ok := m.tagDeleteRow(); ok {
		out = append(out, r)
	}
```
- [ ] **Step 5: Run — PASS** (`go test ./internal/tui/ -run 'TestTagDelete'`).
- [ ] **Step 6: Full TUI package.** `go test ./internal/tui/`.
- [ ] **Step 7: Commit** `feat(tui): Tags .-menu Delete tag (confirm modal)`.

---

### Task 4: CLI `gg tag rm`

**Files:** `internal/cli/tag.go` (add `rm`/`delete`), `internal/cli/tag_test.go`.

- [ ] **Step 1: Failing test** — append:
```go
func TestTagRmDeletes(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "tag", "v1.0.0")
	if code, _, errb := runCLI(t, dir, "tag", "rm", "v1.0.0"); code != 0 {
		t.Fatalf("rm exit %d: %s", code, errb)
	}
	_, out, _ := runCLI(t, dir, "tag", "ls")
	if strings.Contains(out, "v1.0.0") {
		t.Fatalf("tag still listed:\n%s", out)
	}
}

func TestTagRmRequiresName(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "tag", "rm"); code == 0 {
		t.Fatal("rm with no name must fail")
	}
}
```
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Add `rm`** — in `internal/cli/tag.go` dispatcher add a case and `cmdTagDelete`:
```go
	case args[0] == "rm" || args[0] == "delete":
		return cmdTagDelete(svc, args[1:], stdout, stderr)
```
```go
// cmdTagDelete implements `gg tag rm <name>` (alias delete). Typing the command
// is the confirmation; there is no extra prompt.
func cmdTagDelete(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] == "" {
		fmt.Fprintln(stderr, "usage: gg tag rm <name>")
		return 2
	}
	res, err := runOperation(context.Background(), svc,
		engine.DeleteTag{Name: args[0]}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}
```
Also update the unknown-subcommand hint to `(try: ls, create, rm)`.
- [ ] **Step 4: Run — PASS** (check exit directly, not via a pipe).
- [ ] **Step 5: Commit** `feat(cli): gg tag rm <name>`.

---

### Task 5: e2e + docs + agentskill v20

**Files:** `e2e/scenarios/s62_tag_delete.toml`, `internal/agentskill/{using-gg.md,agentskill.go}`, `.claude/skills/using-gg/SKILL.md`, `CHANGELOG.md`, `README.md`.

- [ ] **Step 1: Scenario** — `e2e/scenarios/s62_tag_delete.toml`:
```toml
name = "tag rm: delete a tag"

[input]
steps = [
  { write = "f.txt", content = "v1\n" },
  { commit = "c1" },
  { tag = "v1.0.0" },
]

[[run]]
cmd  = ["tag", "rm", "v1.0.0"]
exit = 0

[[run]]
cmd             = ["tag", "ls"]
exit            = 0
stdout_excludes = ["v1.0.0"]

[expect]
branch = "main"
clean  = true
```
NOTE: confirm the harness key is `stdout_excludes` (the remotes work added it — grep `e2e/scenario.go`); adjust if named differently.
- [ ] **Step 2: Run — PASS** (`go test ./e2e/ -run TestScenarios/s62`).
- [ ] **Step 3: agentskill** — extend the tag line to `ls | create | rm` and document `rm <name>`; bump `Version` 19→20.
- [ ] **Step 4: `gg init --update`** + `go test ./internal/agentskill/`.
- [ ] **Step 5: Docs** — CHANGELOG Added entry (Delete a tag from the Tags `.` menu with a confirm + `gg tag rm`); README CLI list `gg tag rm <name>`.
- [ ] **Step 6: Commit** `feat(tags): delete — e2e + docs + agentskill v20`.

---

### Final gate
- [ ] `./test.sh race` → all green.
- [ ] Hand back for merge.

## Self-Review
- Spec Stage 3: `DeleteTag` op ✅ (Tasks 1-2); Tags `.`-menu + confirm modal ✅ (Task 3, never-trap Cancel); `gg tag rm` ✅ (Task 4); e2e ✅ (Task 5). Confirm is TUI-side (mirrors discard `d`); engine op decision-free; CLI deletes directly (mirrors `gg branch delete`).
- Types: `DeleteTag{Name}` consistent across verb/GitOps/op/row/CLI. `tagDeleteRow` gating mirrors `commitCreateTagRow`.
- Placeholder scan: test NOTEs point at exact sibling helpers to confirm (`engineCatType`, raw-git helper, `stdout_excludes`); no logic left unwritten.
