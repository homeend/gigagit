# Tags — Stage 4a (Checkout) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans.

**Goal:** Check out a tag from the Tags `.` menu and `gg tag checkout` — either **detached** at the tag's commit, or by **creating a new branch** at the tag and switching to it.

**Design note (deviation from spec):** the spec framed the fork as an engine `Decider`, but the "create a branch" arm needs a free-text branch **name**, which a mid-flight option-list Decider cannot collect (same reason create-branch resolves its name in a popup first). So `CheckoutTag{Name, Branch}` is **decision-free** (Branch `""` = detached); the "ask each time" fork is a TUI **modal** (Detached / Create branch… / Cancel), with a name **popup** for the branch arm; the CLI selects the arm with `--branch <name>`. Dirty-tree handling is git's native `switch` behavior (carries non-conflicting changes, else refuses — no data loss); autostash is deferred.

**Tech Stack:** Go 1.26, Bubble Tea, gitcmd/gitexec, real-git tests, TOML e2e.

## Global Constraints
- One git invocation per verb; tui/cli never import internal/git.
- Worktree `/mnt/t/others/gg-tags4` (branch `tags-stage4a-checkout`).
- CLI surface change ⇒ bump `agentskill.Version` 20→21 + `gg init --update`.
- Check test exit codes directly (don't `go test | tail && commit` — the pipe masks failures).

---

### Task 1: git `SwitchDetach` verb + GitOps

**Files:** `internal/git/mutate.go`, `internal/engine/gitops.go`, `internal/git/tag_checkout_test.go`.

- [ ] **Step 1: Failing test** — `internal/git/tag_checkout_test.go`:
```go
package git

import (
	"context"
	"testing"
)

func TestRepoSwitchDetach(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	gitIn(t, dir, "tag", "v1.0.0")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c2")
	if err := repo.SwitchDetach(context.Background(), "v1.0.0"); err != nil {
		t.Fatalf("detach: %v", err)
	}
	if b := gitOutIn(t, dir, "branch", "--show-current"); b != "" {
		t.Fatalf("expected detached HEAD, on branch %q", b)
	}
	if h := gitOutIn(t, dir, "rev-parse", "--short", "HEAD"); h != gitOutIn(t, dir, "rev-parse", "--short", "v1.0.0") {
		t.Fatalf("HEAD not at the tag")
	}
}
```
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Verb** — in `internal/git/mutate.go` after `Switch`:
```go
// SwitchDetach checks out ref with a detached HEAD (git switch --detach).
func (r *Repo) SwitchDetach(ctx context.Context, ref string) error {
	argv := gitcmd.New("switch").Arg("--detach", ref).ToArgv()
	_, err := r.Runner.Run(ctx, "git switch --detach", argv)
	return err
}
```
- [ ] **Step 4: GitOps** — add after `Switch(ctx, branch string) error` in `internal/engine/gitops.go`:
```go
	SwitchDetach(ctx context.Context, ref string) error
```
(grep the interface for the existing `Switch(` line; place it adjacent.)
- [ ] **Step 5: Run — PASS.** **Step 6: Commit** `feat(git): SwitchDetach verb + GitOps`.

---

### Task 2: engine `CheckoutTag` op

**Files:** `internal/engine/checkout_tag.go`, `internal/engine/checkout_tag_test.go`.

- [ ] **Step 1: Failing test** — `internal/engine/checkout_tag_test.go`:
```go
package engine

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func TestCheckoutTagDetached(t *testing.T) {
	dir, repo := newRepo(t)
	if err := repo.CreateTag(context.Background(), "v1.0.0", "", ""); err != nil {
		t.Fatal(err)
	}
	ch := make(chan Event, 16)
	res, err := CheckoutTag{Name: "v1.0.0"}.Run(context.Background(), OpDeps{Repo: repo, Events: ch})
	close(ch)
	if err != nil || !res.Changed {
		t.Fatalf("detached checkout: res=%+v err=%v", res, err)
	}
	if b := gitOut(t, dir, "branch", "--show-current"); b != "" {
		t.Fatalf("expected detached HEAD, on %q", b)
	}
}

func TestCheckoutTagCreatesBranch(t *testing.T) {
	dir, repo := newRepo(t)
	if err := repo.CreateTag(context.Background(), "v1.0.0", "", ""); err != nil {
		t.Fatal(err)
	}
	ch := make(chan Event, 16)
	if _, err := (CheckoutTag{Name: "v1.0.0", Branch: "rel"}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err != nil {
		t.Fatalf("branch checkout: %v", err)
	}
	close(ch)
	if b := gitOut(t, dir, "branch", "--show-current"); b != "rel" {
		t.Fatalf("on branch %q, want rel", b)
	}
}

func TestCheckoutTagRequiresName(t *testing.T) {
	_, repo := newRepo(t)
	ch := make(chan Event, 4)
	if _, err := (CheckoutTag{}).Run(context.Background(), OpDeps{Repo: repo, Events: ch}); err == nil {
		t.Fatal("empty name must error")
	}
}
```
NOTE: if `gitOut` already exists in this package's tests, drop the local definition. (Stage 2 added `engineCatType`; check for a `gitOut`/`engineRevParse` first — reuse it.)
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Op** — `internal/engine/checkout_tag.go`:
```go
package engine

import (
	"context"
	"fmt"
)

// CheckoutTag checks out a tag: detached at the tag's commit (Branch == "") or by
// creating Branch at the tag and switching to it. Decision-free; the frontend
// resolves the detached-vs-branch fork (and any branch name) before calling.
type CheckoutTag struct {
	Name   string // tag (required)
	Branch string // "" = detached HEAD; else new branch created at the tag
}

func (op CheckoutTag) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Name == "" {
		return Result{}, fmt.Errorf("checkout tag: Name is required")
	}
	if op.Branch != "" {
		deps.emit(ctx, Progress{Step: "creating branch", Detail: op.Branch + " at " + op.Name})
		if err := deps.Repo.CreateBranch(ctx, op.Branch, op.Name); err != nil {
			return Result{}, fmt.Errorf("checkout tag: %w", err)
		}
		deps.emit(ctx, Progress{Step: "switching", Detail: op.Branch})
		if err := deps.Repo.Switch(ctx, op.Branch); err != nil {
			return Result{}, fmt.Errorf("checkout tag: %w", err)
		}
		res := Result{Summary: "created branch " + op.Branch + " at " + op.Name + " and switched", Changed: true}
		deps.emit(ctx, Done{Result: res})
		return res, nil
	}
	deps.emit(ctx, Progress{Step: "checking out", Detail: op.Name})
	if err := deps.Repo.SwitchDetach(ctx, op.Name); err != nil {
		return Result{}, fmt.Errorf("checkout tag: %w", err)
	}
	res := Result{Summary: "checked out " + op.Name + " (detached HEAD)", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = CheckoutTag{}
```
- [ ] **Step 4: Run — PASS.** **Step 5: Commit** `feat(engine): CheckoutTag op (detached or new branch)`.

---

### Task 3: TUI — `tagCheckoutRow` (modal) + `tagCheckoutPopup`

**Files:** `internal/tui/tags_actions.go` (row + popup OR a new `tag_checkout_popup.go`), `internal/tui/action_menu.go` (register), `internal/tui/tags_actions_test.go`.

**Flow:** `tagCheckoutRow` (gated `focus==panelTags && opsIdle`) → modal Options `["Detached", "Create branch…", "Cancel"]`. onResolve: Detached → `startOp(CheckoutTag{Name})`; "Create branch…" → `m.pushLayer(&tagCheckoutPopup{tag: name})`; Cancel → `m,nil`. `tagCheckoutPopup` is a single-field name popup (mirror `tagPopup`/`branchPopup`) whose enter → `startOp(CheckoutTag{Name: tag, Branch: typed})`.

- [ ] **Step 1: Failing tests** — append to `internal/tui/tags_actions_test.go`:
```go
func TestTagCheckoutRowDetached(t *testing.T) {
	dir, repo := newRepoDir(t)
	gitIn(t, dir, "tag", "v1.0.0")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c2")
	m := loadModel(t, repo)
	m.focus = panelTags
	m.activeFilesTab = panelTags
	m.sel[panelTags] = 0
	row, ok := m.tagCheckoutRow()
	if !ok {
		t.Fatal("checkout row must appear on the Tags panel")
	}
	u, _ := row.run(m)
	m = u.(Model)
	if m.modal == nil {
		t.Fatal("checkout must open the detached/branch modal")
	}
	um, cmd := m.modal.onResolve(m, "Detached")
	m = um.(Model)
	for i := 0; i < 100 && m.running; i++ {
		uu, next := m.Update(cmd())
		m = uu.(Model)
		cmd = next
	}
	if b := gitCurrentBranch(t, dir); b != "" {
		t.Fatalf("expected detached HEAD, on %q", b)
	}
}

func TestTagCheckoutPopupCreatesBranch(t *testing.T) {
	dir, repo := newRepoDir(t)
	gitIn(t, dir, "tag", "v1.0.0")
	m := loadModel(t, repo)
	m = m.pushLayer(&tagCheckoutPopup{tag: "v1.0.0"})
	for _, r := range "rel" {
		u, _ := m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(Model)
	for i := 0; i < 100 && m.running; i++ {
		uu, next := m.Update(cmd())
		m = uu.(Model)
		cmd = next
	}
	if b := gitCurrentBranch(t, dir); b != "rel" {
		t.Fatalf("on branch %q, want rel", b)
	}
}
```
Add a helper `gitCurrentBranch(t, dir) string` using `exec.Command("git","-C",dir,"branch","--show-current")` (import `os/exec` if not already). NOTE: confirm `decisionState` resolution clears `m.modal` before `onResolve` (it does — model.go ~357), so the popup push is visible.
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Add `tagCheckoutRow`** in `internal/tui/tags_actions.go`:
```go
// tagCheckoutRow offers "Check out tag" on the Tags panel: ask detached vs a new
// branch, then check out.
func (m Model) tagCheckoutRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-checkout",
		label: "Check out tag",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.modal = &decisionState{
				req: engine.DecisionRequest{
					ID:      "checkout-tag",
					Prompt:  "Check out " + name + ":",
					Options: []string{"Detached", "Create branch…", "Cancel"},
				},
				onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
					switch opt {
					case "Detached":
						return m.startOp(engine.CheckoutTag{Name: name})
					case "Create branch…":
						return m.pushLayer(&tagCheckoutPopup{tag: name}), nil
					}
					return m, nil
				},
			}
			return m, nil
		},
	}, true
}
```
- [ ] **Step 4: Add `tagCheckoutPopup`** — new file `internal/tui/tag_checkout_popup.go` (mirror `tagPopup`'s single-field shape; on enter `startOp(CheckoutTag{Name: p.tag, Branch: p.name})`; esc pops; ctrl+c quits; reject space; title `"New branch at " + p.tag`; hint `[type] name  [enter] checkout  [esc] cancel`).
- [ ] **Step 5: Register** — in `availableActions`, after `tagDeleteRow`:
```go
	if r, ok := m.tagCheckoutRow(); ok {
		out = append(out, r)
	}
```
- [ ] **Step 6: Run — PASS** (`go test ./internal/tui/ -run 'TestTagCheckout'`). **Step 7: full TUI.** **Step 8: Commit** `feat(tui): Tags .-menu Check out tag (detached / new branch)`.

---

### Task 4: CLI `gg tag checkout`

**Files:** `internal/cli/tag.go`, `internal/cli/tag_test.go`.

**Surface:** `gg tag checkout <name> [--branch <newname>]` — `--branch` ⇒ create branch + switch; else detached.

- [ ] **Step 1: Failing tests** — append:
```go
func TestTagCheckoutDetached(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "tag", "v1.0.0")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "c2")
	if code, _, errb := runCLI(t, dir, "tag", "checkout", "v1.0.0"); code != 0 {
		t.Fatalf("checkout exit %d: %s", code, errb)
	}
	out, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("expected detached HEAD, on %q", out)
	}
}

func TestTagCheckoutToBranch(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "tag", "v1.0.0")
	if code, _, errb := runCLI(t, dir, "tag", "checkout", "--branch", "rel", "v1.0.0"); code != 0 {
		t.Fatalf("checkout --branch exit %d: %s", code, errb)
	}
	out, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if strings.TrimSpace(string(out)) != "rel" {
		t.Fatalf("on %q, want rel", out)
	}
}
```
(import `os/exec` in the test file if needed.)
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Add the subcommand** — in `internal/cli/tag.go` add a `checkout` case + `cmdTagCheckout` (flags-first):
```go
	case args[0] == "checkout" || args[0] == "co":
		return cmdTagCheckout(svc, args[1:], stdout, stderr)
```
```go
// cmdTagCheckout implements `gg tag checkout [--branch <name>] <tag>`. With
// --branch it creates that branch at the tag and switches; otherwise detached.
func cmdTagCheckout(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tag checkout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	branch := fs.String("branch", "", "create this branch at the tag and switch to it")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 1 || rest[0] == "" {
		fmt.Fprintln(stderr, "usage: gg tag checkout [--branch <name>] <tag>")
		return 2
	}
	res, err := runOperation(context.Background(), svc,
		engine.CheckoutTag{Name: rest[0], Branch: *branch}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}
```
Update the unknown-subcommand hint to `(try: ls, create, rm, checkout)`.
- [ ] **Step 4: Run — PASS** (verify exit directly). **Step 5: Commit** `feat(cli): gg tag checkout [--branch <name>] <tag>`.

---

### Task 5: e2e + docs + agentskill v21

**Files:** `e2e/scenarios/s66_tag_checkout.toml`, agentskill (md + Version 20→21), `.claude/skills/using-gg/SKILL.md`, CHANGELOG, README.

- [ ] **Step 1: Scenario** — pick the next free `sNN` (ls `e2e/scenarios` first). `s66_tag_checkout.toml`:
```toml
name = "tag checkout: detached and to a new branch"

[input]
steps = [
  { write = "f.txt", content = "v1\n" },
  { commit = "c1" },
  { tag = "v1.0.0" },
  { write = "f.txt", content = "v2\n" },
  { commit = "c2" },
]

[[run]]
cmd  = ["tag", "checkout", "--branch", "rel", "v1.0.0"]
exit = 0

[expect]
branch = "rel"
clean  = true
```
- [ ] **Step 2: Run** the scenario (`go test ./e2e/ -run TestScenarios/sNN`).
- [ ] **Step 3: agentskill** — extend the tag line to `ls | create | rm | checkout` documenting `checkout [--branch <name>] <tag>`; bump Version 20→21.
- [ ] **Step 4:** `gg init --update` + `go test ./internal/agentskill/`.
- [ ] **Step 5: Docs** — CHANGELOG Added (Check out a tag — detached or as a new branch — from the Tags `.` menu + `gg tag checkout`); README CLI list `gg tag checkout [--branch <name>] <tag>`; help.go Tags section adds the `.`-menu **Check out tag** line.
- [ ] **Step 6: Commit** `feat(tags): checkout — e2e + docs + agentskill v21`.

---

### Final gate
- [ ] `./test.sh race` → all green. Hand back for merge.

## Self-Review
- Spec Stage 4 (checkout half): detached + new-branch arms ✅ (op Tasks 1-2; TUI modal+popup Task 3; CLI Task 4). Deviation from the spec's engine-`Decider` is documented above (free-text branch name forces a frontend-resolved fork); "ask each time" preserved as the TUI modal.
- Types: `CheckoutTag{Name, Branch}` consistent across op/row/popup/CLI; `SwitchDetach` in verb + GitOps.
- Push is Stage 4b (separate branch). Autostash deferred (git-native dirty handling; noted).
