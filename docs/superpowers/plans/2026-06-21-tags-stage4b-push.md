# Tags — Stage 4b (Push) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans.

**Goal:** Push a tag to a remote from the Tags `.` menu and `gg tag push`. Remote selection: one remote → auto; multiple → an option-list **Decider** (the TUI renders it in the modal, the CLI answers via the `<remote>` arg).

**Architecture:** `PushTag` git verb (`git push <remote> refs/tags/<name>`) → `engine.PushTag{Name, Remote}` op that resolves the remote (Decider `push-tag-remote` over `RemoteNames` + `"abort"`) → Tags-tab `.`-menu row that just `startOp`s (the modal handles the fork) → `gg tag push <name> [<remote>]`. This is the *correct* Decider use: remote names are a fixed option list (no free text), unlike the checkout branch name.

**Tech Stack:** Go 1.26, Bubble Tea, gitcmd/gitexec, real-git tests, TOML e2e with the git-server transport.

## Global Constraints
- One git invocation per verb; tui/cli never import internal/git.
- Worktree `/mnt/t/others/gg-tags4b` (branch `tags-stage4b-push`).
- CLI surface change ⇒ bump `agentskill.Version` 21→22 + `gg init --update`.
- `Done` is success-only; the abort branch returns `Result{Changed:false}` with no `Done`.
- Check test exit codes directly (no `go test | tail && commit`).

---

### Task 1: git `PushTag` verb + GitOps

**Files:** `internal/git/sync.go` (after `Push`), `internal/engine/gitops.go`, `internal/git/tag_push_test.go`.

- [ ] **Step 1: Failing real-git test** — `internal/git/tag_push_test.go` (use this package's clone helper — `newClonePair` exists in `sync_test.go`, returns a clone with an `origin` remote; confirm its signature):
```go
package git

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestRepoPushTag(t *testing.T) {
	dir, runner := newClonePair(t) // (cloneDir, runner) with an "origin" remote — verify
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "tag", "v1.0.0")
	if err := repo.PushTag(context.Background(), "origin", "v1.0.0"); err != nil {
		t.Fatalf("push tag: %v", err)
	}
	// The tag now exists on origin (ls-remote --tags from the clone).
	out, err := exec.Command("git", "-C", dir, "ls-remote", "--tags", "origin").Output()
	if err != nil || !strings.Contains(string(out), "refs/tags/v1.0.0") {
		t.Fatalf("tag not on origin: out=%q err=%v", out, err)
	}
}
```
NOTE: confirm the clone helper's name/return shape in `internal/git/sync_test.go` (it builds a bare origin + clone). Adjust the call. If it returns `(dir string, runner Runner)` use as written; if `(origin, clone, runner)` adapt.
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Verb** — in `internal/git/sync.go` after `Push`:
```go
// PushTag pushes a single tag to remote (git push <remote> refs/tags/<name>).
// The explicit refs/tags/ refspec avoids a branch/tag name ambiguity.
func (r *Repo) PushTag(ctx context.Context, remote, name string) error {
	argv := gitcmd.New("push").Arg(remote, "refs/tags/"+name).ToArgv()
	_, err := r.Runner.Run(ctx, "git push (tag)", argv)
	return err
}
```
- [ ] **Step 4: GitOps** — add after `Push(...)` in `internal/engine/gitops.go`:
```go
	PushTag(ctx context.Context, remote, name string) error
```
- [ ] **Step 5: Run — PASS.** **Step 6: Commit** `feat(git): PushTag verb + GitOps`.

---

### Task 2: engine `PushTag` op (remote-selection Decider)

**Files:** `internal/engine/push_tag.go`, `internal/engine/push_tag_test.go`.

- [ ] **Step 1: Failing test** — `internal/engine/push_tag_test.go`. Cover: explicit remote (no decision); single-remote auto; multi-remote Decider via `MapDecider`; abort. Use the engine package's decider test helper (grep `MapDecider`/`stubDecider` in `internal/engine/*_test.go`; SmartMerge's tests show the pattern). Pseudostructure:
```go
func TestPushTagExplicitRemote(t *testing.T) {
	_, repo := newRepo(t) // need a repo with a fake/real remote — see NOTE
	// fakerunner asserting `git push (tag)` argv may be simpler than a real remote.
}
```
NOTE: the cleanest engine test here uses a `FakeRunner` (assert the `git push (tag)` argv and the RemoteNames response) rather than a real remote, because the decision branches matter more than the network. Mirror an engine op test that uses `gitexec.NewFakeRunner()` + `SetResponse`. Drive the multi-remote case with `MapDecider{"push-tag-remote": "origin"}` and assert the push argv targeted origin; drive abort with `MapDecider{"push-tag-remote": "abort"}` and assert `Changed:false` + no push call.
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Op** — `internal/engine/push_tag.go`:
```go
package engine

import (
	"context"
	"fmt"
)

// PushTag pushes Name to a remote. Remote "" is resolved: one remote is used
// automatically; multiple remotes fork through the push-tag-remote Decider
// (remote names are a fixed option list). An "abort" choice cancels.
type PushTag struct {
	Name   string // tag (required)
	Remote string // "" = resolve (auto when one remote, else Decider)
}

func (op PushTag) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Name == "" {
		return Result{}, fmt.Errorf("push tag: Name is required")
	}
	remote := op.Remote
	if remote == "" {
		names, err := deps.Repo.RemoteNames(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("push tag: %w", err)
		}
		switch len(names) {
		case 0:
			return Result{}, fmt.Errorf("push tag: no remotes configured")
		case 1:
			remote = names[0]
		default:
			choice, derr := deps.decide(ctx, DecisionRequest{
				ID:      "push-tag-remote",
				Prompt:  "Push " + op.Name + " to which remote?",
				Options: append(append([]string{}, names...), "abort"),
			})
			if derr != nil {
				return Result{}, derr
			}
			if choice == "abort" {
				return Result{Changed: false}, nil
			}
			remote = choice
		}
	}
	deps.emit(ctx, Progress{Step: "pushing tag", Detail: op.Name + " → " + remote})
	if err := deps.Repo.PushTag(ctx, remote, op.Name); err != nil {
		return Result{}, fmt.Errorf("push tag: %w", err)
	}
	res := Result{Summary: "pushed " + op.Name + " to " + remote, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = PushTag{}
```
- [ ] **Step 4: Run — PASS.** **Step 5: Commit** `feat(engine): PushTag op (remote-selection Decider)`.

---

### Task 3: TUI `tagPushRow`

**Files:** `internal/tui/tags_actions.go`, `internal/tui/action_menu.go`, `internal/tui/tags_actions_test.go`.

The row just `startOp`s — the Decider renders in the existing modal automatically (zero extra UI).

- [ ] **Step 1: Failing test** — append to `internal/tui/tags_actions_test.go`:
```go
func TestTagPushRowGating(t *testing.T) {
	m := footerModel()
	m.tags = []model.Tag{{Name: "v1.0.0"}}
	m.focus = panelBranches
	if _, ok := m.tagPushRow(); ok {
		t.Fatal("push row inert off the Tags panel")
	}
	m.focus = panelTags
	m.activeFilesTab = panelTags
	m.sel[panelTags] = 0
	if _, ok := m.tagPushRow(); !ok {
		t.Fatal("push row must appear on the Tags panel")
	}
}
```
(A real-git push-through is covered by the engine + e2e; the TUI test asserts gating + that `run` starts an op. Optionally assert `row.run` sets `m.running` with a single-remote clone via `loadModel`.)
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Add `tagPushRow`** in `internal/tui/tags_actions.go`:
```go
// tagPushRow offers "Push tag" on the Tags panel. The engine resolves the remote
// (auto when one, else a modal pick).
func (m Model) tagPushRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-push",
		label: "Push tag",
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.startOp(engine.PushTag{Name: name}) },
	}, true
}
```
- [ ] **Step 4: Register** — in `availableActions`, after `tagCheckoutRow`:
```go
	if r, ok := m.tagPushRow(); ok {
		out = append(out, r)
	}
```
- [ ] **Step 5: Run — PASS.** **Step 6: full TUI.** **Step 7: Commit** `feat(tui): Tags .-menu Push tag`.

---

### Task 4: CLI `gg tag push`

**Files:** `internal/cli/tag.go`, `internal/cli/tag_test.go`.

**Surface:** `gg tag push <name> [<remote>]`. With a remote arg, no decision; without one, the single-remote case auto-resolves and the multi-remote case errors non-interactively (standard cliDecider behavior).

- [ ] **Step 1: Failing test** — append (clone helper with one `origin` — check `internal/cli/*_test.go` for `cloneWithRemoteFoo` or similar):
```go
func TestTagPushToOrigin(t *testing.T) {
	clone := cloneWithRemoteFoo(t) // a clone with an origin remote (from ops_test.go)
	gitRun(t, clone, "tag", "v1.0.0")
	if code, _, errb := runCLI(t, clone, "tag", "push", "v1.0.0", "origin"); code != 0 {
		t.Fatalf("push exit %d: %s", code, errb)
	}
	out, _ := exec.Command("git", "-C", clone, "ls-remote", "--tags", "origin").Output()
	if !strings.Contains(string(out), "refs/tags/v1.0.0") {
		t.Fatalf("tag not pushed:\n%s", out)
	}
}

func TestTagPushRequiresName(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "tag", "push"); code == 0 {
		t.Fatal("push with no name must fail")
	}
}
```
NOTE: reuse whatever clone-with-remote helper the cli tests already have (the remote-ls/fetch tests use one). If none, build a bare origin + clone inline.
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Add the subcommand** — in `internal/cli/tag.go`:
```go
	case args[0] == "push":
		return cmdTagPush(svc, args[1:], stdout, stderr)
```
```go
// cmdTagPush implements `gg tag push <name> [<remote>]`. With no remote and a
// single configured remote it pushes there; with multiple it errors (specify one).
func cmdTagPush(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || len(args) > 2 || args[0] == "" {
		fmt.Fprintln(stderr, "usage: gg tag push <name> [<remote>]")
		return 2
	}
	remote := ""
	if len(args) == 2 {
		remote = args[1]
	}
	res, err := runOperation(context.Background(), svc,
		engine.PushTag{Name: args[0], Remote: remote}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}
```
Update the unknown-subcommand hint to `(try: ls, create, rm, checkout, push)`.
- [ ] **Step 4: Run — PASS.** **Step 5: Commit** `feat(cli): gg tag push <name> [<remote>]`.

---

### Task 5: e2e + docs + agentskill v22

**Files:** `e2e/scenarios/sNN_tag_push.toml`, agentskill (md + 21→22), SKILL.md, CHANGELOG, README, help.go.

- [ ] **Step 1: Scenario** — use the git-server/clone transport (see `s49_remote_fetch.toml` for an `[input.origin]` clone scenario). Local creates a tag, `gg tag push v1.0.0 origin`, then a later `[[run]]` or `[expect]` proving the tag reached origin. Confirm how the remote-side assertion is expressed (the remotes scenarios show it — possibly an `[[run]]` of `remote ls`/a stdout check, or an origin-side step). Pick the next free `sNN`.
- [ ] **Step 2: Run the scenario.**
- [ ] **Step 3: agentskill** — extend the tag line to `… | push`; document `push <name> [<remote>]`; bump 21→22.
- [ ] **Step 4:** `gg init --update` + `go test ./internal/agentskill/`.
- [ ] **Step 5: Docs** — CHANGELOG Added (Push a tag from the Tags `.` menu — auto remote or pick — + `gg tag push`); README CLI list; help.go Tags **Push tag** line.
- [ ] **Step 6: Commit** `feat(tags): push — e2e + docs + agentskill v22`.

---

### Final gate
- [ ] `./test.sh race` → all green. Hand back for merge. **Tags arc complete** (read → create → delete → checkout → push).

## Self-Review
- Spec Stage 4 (push half): `PushTag` op + remote auto/picker ✅ (Decider is the correct option-list use); TUI `.`-menu + CLI ✅; e2e against git-server ✅.
- Types: `PushTag{Name, Remote}` consistent across op/row/CLI; `PushTag` verb + GitOps. Decision ID `push-tag-remote` matches between engine and any CLI policy.
- `Done` success-only; abort returns `Changed:false`.
