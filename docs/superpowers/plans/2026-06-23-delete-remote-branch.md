# Delete Remote Branch (GitKraken parity, Bucket B.1) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete a remote branch (`git push <remote> --delete <branch>`) from the Remotes panel `.` menu (TUI, confirm modal) and the CLI (`gg remote rm <remote>/<branch>`).

**Architecture:** New thin git verb `PushDelete` → new engine op `DeleteRemoteBranch` (confirm via the `Decider`, like `DeleteBranch`; `RefWrite` lock + post-op refresh like `Prune`) → wired into the TUI Remotes menu and the CLI `remote` group. `OpName` is reflection-based (no registration). Frontends never touch git directly.

**Tech Stack:** Go 1.26, `internal/git` verbs, `internal/engine` ops + `Decider`, Bubble Tea TUI, CLI `cliDecider` policy, declarative e2e harness with a git server.

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26.
- A git verb is one invocation, built with `gitcmd`, run via `r.Runner.Run`.
- Operations never block on a human: confirm via `deps.decide`, the channel send selects on `ctx.Done`.
- Frontends run ops via `domain.Execute`/`startOp`/`runOperation`, never by assembling `OpDeps`. `internal/tui` and `internal/cli` MUST NOT import `internal/git`.
- TUI `Model` is a value receiver.
- Destructive + outward-facing: a single TUI keypress MUST NOT delete a remote ref unconfirmed; the CLI command itself is the confirmation (pre-answers the Decider).
- `<remote>/<branch>` parsing: split on the FIRST `/` (branch names contain `/`, remote names don't).
- Every commit message ends with these two trailers, verbatim:
  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro
  ```
- Run `./test.sh race` before merge (merge is the human's call). Build with `go build ./cmd/gg`.

---

### Task 1: `git.Repo.PushDelete` verb

**Files:**
- Modify: `internal/git/sync.go` (after `Push`/`PushTag`, ~line 122)
- Modify: `internal/git/sync_test.go`

**Interfaces:**
- Consumes: `gitcmd.New`, `r.Runner.Run`; the `newClonePair(t) (string, gitexec.Runner)` test helper.
- Produces: `func (r *Repo) PushDelete(ctx context.Context, remote, branch string) error` — runs `git push <remote> --delete <branch>`, span name `"git push delete"`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/git/sync_test.go`:

```go
func TestPushDeleteArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git push delete", gitexec.Result{})
	repo := &Repo{Runner: f}
	if err := repo.PushDelete(context.Background(), "origin", "feat/x"); err != nil {
		t.Fatalf("PushDelete: %v", err)
	}
	var argv []string
	for _, c := range f.Calls {
		if c.Name == "git push delete" {
			argv = c.Argv
		}
	}
	want := []string{"push", "origin", "--delete", "feat/x"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
}

func TestPushDeleteRemovesBranchOnRemote(t *testing.T) {
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}
	// Create a branch and push it to origin, then delete it via PushDelete.
	gitIn(t, clone, "branch", "doomed", "main")
	gitIn(t, clone, "push", "origin", "doomed")
	if err := repo.PushDelete(context.Background(), "origin", "doomed"); err != nil {
		t.Fatalf("PushDelete: %v", err)
	}
	// origin must no longer advertise the branch.
	if out := gitOut(t, clone, "ls-remote", "--heads", "origin", "doomed"); strings.TrimSpace(out) != "" {
		t.Fatalf("origin still has doomed branch: %q", out)
	}
}
```

If `sync_test.go` lacks a `gitOut(t, dir, ...)` string-returning helper, use the one this package already uses to read git output (grep `func gitOut` / `func revParse` in `internal/git/*_test.go`); both `gitIn` and a string-returning reader exist in this package.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/git/ -run TestPushDelete -v`
Expected: FAIL — `repo.PushDelete undefined`.

- [ ] **Step 3: Implement the verb**

In `internal/git/sync.go`, after `PushTag`:

```go
// PushDelete deletes branch on remote (git push <remote> --delete <branch>).
func (r *Repo) PushDelete(ctx context.Context, remote, branch string) error {
	argv := gitcmd.New("push").Arg(remote, "--delete", branch).ToArgv()
	_, err := r.Runner.Run(ctx, "git push delete", argv)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -run TestPushDelete -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/sync.go internal/git/sync_test.go
git commit  # "feat(git): PushDelete verb (git push --delete)" + trailers
```

---

### Task 2: `engine.DeleteRemoteBranch` op

**Files:**
- Create: `internal/engine/delete_remote_branch.go`
- Create: `internal/engine/delete_remote_branch_test.go`
- Modify: `internal/engine/gitops.go` (add `PushDelete` to the `GitOps` interface)

**Interfaces:**
- Consumes: `git.Repo.PushDelete` (Task 1) via the `GitOps` interface; `deps.decide`, `deps.emit`, `repogate.RefWrite`, `MapDecider` (test).
- Produces: `engine.DeleteRemoteBranch{Remote, Branch string}` with `Run` + `LockMode() repogate.Mode`.

- [ ] **Step 1: Add `PushDelete` to the `GitOps` interface**

In `internal/engine/gitops.go`, beside `Push`/`PushTag` (~line 33):

```go
	PushDelete(ctx context.Context, remote, branch string) error
```

- [ ] **Step 2: Write the failing tests**

Create `internal/engine/delete_remote_branch_test.go`:

```go
package engine

import (
	"context"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
)

func delRemoteFakeRepo() (*git.Repo, *gitexec.FakeRunner) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git push delete", gitexec.Result{}) // succeed
	return &git.Repo{Runner: f}, f
}

func pushDeleteCalled(f *gitexec.FakeRunner) (remote, branch string, ok bool) {
	for _, c := range f.Calls {
		if c.Name == "git push delete" && len(c.Argv) >= 4 {
			return c.Argv[1], c.Argv[3], true // ["push", remote, "--delete", branch]
		}
	}
	return "", "", false
}

func TestDeleteRemoteBranchConfirmDeletes(t *testing.T) {
	repo, f := delRemoteFakeRepo()
	res, err := DeleteRemoteBranch{Remote: "origin", Branch: "feat/x"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-remote-branch": "delete"}})
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if r, b, ok := pushDeleteCalled(f); !ok || r != "origin" || b != "feat/x" {
		t.Fatalf("push delete called with (%q,%q) ok=%v, want origin/feat/x", r, b, ok)
	}
}

func TestDeleteRemoteBranchAbortDoesNotDelete(t *testing.T) {
	repo, f := delRemoteFakeRepo()
	res, err := DeleteRemoteBranch{Remote: "origin", Branch: "feat/x"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-remote-branch": "abort"}})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Changed {
		t.Fatal("abort must not change anything")
	}
	if _, _, ok := pushDeleteCalled(f); ok {
		t.Fatal("abort must not call push --delete")
	}
}

func TestDeleteRemoteBranchRequiresFields(t *testing.T) {
	repo, _ := delRemoteFakeRepo()
	if _, err := (DeleteRemoteBranch{Branch: "x"}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("missing Remote must error")
	}
	if _, err := (DeleteRemoteBranch{Remote: "origin"}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("missing Branch must error")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run TestDeleteRemoteBranch -v`
Expected: FAIL — `DeleteRemoteBranch` undefined (and the `GitOps` interface gains a method, so the `FakeRunner`-backed `*git.Repo` still satisfies it — `*git.Repo` implements `PushDelete` from Task 1).

- [ ] **Step 4: Implement the op**

Create `internal/engine/delete_remote_branch.go`:

```go
package engine

import (
	"context"
	"fmt"

	"github.com/gigagit/gg/internal/repogate"
)

// DeleteRemoteBranch deletes a branch on a remote (git push <remote> --delete
// <branch>). Destructive and outward-facing, so it confirms via the Decider;
// the CLI pre-answers (the command is the confirmation). RefWrite: it mutates
// remote-tracking refs, like Prune.
type DeleteRemoteBranch struct {
	Remote string // required, e.g. "origin"
	Branch string // required, de-prefixed, e.g. "feat/x"
}

func (op DeleteRemoteBranch) LockMode() repogate.Mode { return repogate.RefWrite }

func (op DeleteRemoteBranch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Remote == "" || op.Branch == "" {
		return Result{}, fmt.Errorf("delete remote branch: Remote and Branch are required")
	}
	ref := op.Remote + "/" + op.Branch

	confirm, err := deps.decide(ctx, DecisionRequest{
		ID:      "delete-remote-branch",
		Prompt:  "Delete remote branch " + ref + "? This pushes a deletion to " + op.Remote + ".",
		Options: []string{"delete", "abort"},
	})
	if err != nil {
		return Result{}, err
	}
	if confirm.Option != "delete" {
		return Result{Summary: "cancelled", Changed: false}, nil
	}

	deps.emit(ctx, Progress{Step: "deleting remote branch", Detail: ref})
	if err := deps.Repo.PushDelete(ctx, op.Remote, op.Branch); err != nil {
		return Result{}, fmt.Errorf("delete remote branch: %w", err)
	}

	res := Result{Summary: "deleted " + ref, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = DeleteRemoteBranch{}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run TestDeleteRemoteBranch -v`
Expected: PASS. Then `go test ./internal/engine/` to confirm the interface change didn't break other ops/fakes.

- [ ] **Step 6: Commit**

```bash
git add internal/engine/delete_remote_branch.go internal/engine/delete_remote_branch_test.go internal/engine/gitops.go
git commit  # "feat(engine): DeleteRemoteBranch op (Decider confirm)" + trailers
```

---

### Task 3: TUI Remotes-row Delete

**Files:**
- Modify: `internal/tui/remote_actions.go` (+ `remoteDeleteRow`)
- Modify: `internal/tui/action_menu.go` (wire after the other remote rows)
- Modify: `internal/tui/remote_actions_test.go`

**Interfaces:**
- Consumes: `Model.selectedRemote() (model.RemoteBranch, bool)`, `Model.opsIdle()`, `Model.startOp`, `engine.DeleteRemoteBranch`, `model.RemoteBranch{Name,Remote,Branch}`.
- Produces: `remoteDeleteRow() (actionRow, bool)`, row id `"remote-delete"`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/remote_actions_test.go` (the `remoteModel()` helper already exists there from Bucket A):

```go
func TestRemoteDeleteRowPresent(t *testing.T) {
	m := remoteModel()
	got := ids(availableActions(m))
	if !got["remote-delete"] {
		t.Fatalf("expected remote-delete in menu; got %v", got)
	}
}

func TestRemoteDeleteRowAbsentWithoutSelection(t *testing.T) {
	m := remoteModel()
	m.remoteBranches = nil // empty list → no selection
	got := ids(availableActions(m))
	if got["remote-delete"] {
		t.Fatalf("remote-delete must be absent with no selection; got %v", got)
	}
}

func TestRemoteDeleteRowDispatches(t *testing.T) {
	m := remoteModel()
	row, ok := m.remoteDeleteRow()
	if !ok {
		t.Fatal("remoteDeleteRow not available")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("delete row run returned nil cmd")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestRemoteDelete -v`
Expected: FAIL — `m.remoteDeleteRow undefined`.

- [ ] **Step 3: Implement the row**

In `internal/tui/remote_actions.go`, add:

```go
// remoteDeleteRow offers "Delete <remote branch>" on the Remotes tab. The
// engine's Decider confirm (surfaced as the TUI modal) gates the actual push;
// a single keypress never deletes a remote ref unconfirmed.
func (m Model) remoteDeleteRow() (actionRow, bool) {
	rb, ok := m.selectedRemote()
	if !ok || !m.opsIdle() {
		return actionRow{}, false
	}
	return actionRow{
		id:    "remote-delete",
		label: "Delete " + rb.Name,
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.DeleteRemoteBranch{Remote: rb.Remote, Branch: rb.Branch})
		},
	}, true
}
```

- [ ] **Step 4: Wire it into `availableActions`**

In `internal/tui/action_menu.go`, after the `remoteRebaseRow` append block (added in Bucket A):

```go
	if r, ok := m.remoteDeleteRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run TestRemoteDelete -v`
Expected: PASS. Then `go test ./internal/tui/` for no regressions.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/remote_actions.go internal/tui/action_menu.go internal/tui/remote_actions_test.go
git commit  # "feat(tui): Delete remote branch on the Remotes . menu" + trailers
```

---

### Task 4: CLI `gg remote rm`

**Files:**
- Modify: `internal/cli/remote.go` (+ `cmdRemoteRm`, switch case, help string)
- Modify: `internal/cli/remote_test.go`

**Interfaces:**
- Consumes: `runOperation(ctx, svc, op, dec, progress)`, `cliDecider{policy, in, out, interactive}`, `stdinIsTerminal()`, `finish(res, err, stdout, stderr)`, `engine.DeleteRemoteBranch`.
- Produces: `cmdRemoteRm(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/remote_test.go`. Use the package's existing end-to-end helpers (confirmed present): `runCLI(t, workdir, args...) (code int, stdout, stderr string)` runs the full `Run` dispatch (threading stdin); `cloneWithRemoteFoo(t) string` returns a clone whose bare origin has a branch `foo`; `runGit(t, dir, args...) string` returns git output.

```go
func TestRemoteRmDeletesBranchOnOrigin(t *testing.T) {
	clone := cloneWithRemoteFoo(t)
	origin := runGit(t, clone, "config", "--get", "remote.origin.url")
	if code, _, errb := runCLI(t, clone, "remote", "rm", "origin/foo"); code != 0 {
		t.Fatalf("remote rm exit = %d (stderr: %s)", code, errb)
	}
	if out := runGit(t, origin, "branch", "--list", "foo"); strings.TrimSpace(out) != "" {
		t.Fatalf("origin still has foo: %q", out)
	}
}

func TestRemoteRmRejectsArgWithoutSlash(t *testing.T) {
	clone := cloneWithRemoteFoo(t)
	code, _, errb := runCLI(t, clone, "remote", "rm", "noslash")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errb, "usage") {
		t.Fatalf("stderr = %q, want a usage message", errb)
	}
}
```

These go through `Run`, so they exercise the real `cmdRemote` dispatch and stdin threading.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestRemoteRm -v`
Expected: FAIL — `rm` unhandled (exit 2 "unknown subcommand") and/or `cmdRemoteRm` undefined.

- [ ] **Step 3: Implement `cmdRemoteRm` + wire the switch**

`cmdRemote` is currently `cmdRemote(svc *domain.Service, args []string, stdout, stderr io.Writer) int` (`internal/cli/cli.go:87` calls `cmdRemote(svc, rest, stdout, stderr)`). Add a `stdin io.Reader` param (place it before `stdout`, matching `cmdBranch(svc, rest, stdin, stdout, stderr)`) and update that call site to `cmdRemote(svc, rest, stdin, stdout, stderr)` (the `Run` signature already has `stdin`). Then add the switch case:

```go
case args[0] == "rm" || args[0] == "remove":
	return cmdRemoteRm(svc, args[1:], stdin, stdout, stderr)
```

Update the default-case help to `"remote: unknown subcommand %q (try: ls, fetch, prune, rm)\n"`.

Add:

```go
// cmdRemoteRm implements `gg remote rm <remote>/<branch>` — delete a remote
// branch. The command is the confirmation: the delete-remote-branch decision is
// pre-answered. Splits on the FIRST '/' (branch names may contain '/').
func cmdRemoteRm(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: gg remote rm <remote>/<branch>")
		return 2
	}
	remote, branch, ok := strings.Cut(args[0], "/")
	if !ok || remote == "" || branch == "" {
		fmt.Fprintln(stderr, "usage: gg remote rm <remote>/<branch>")
		return 2
	}
	dec := cliDecider{policy: map[string]string{"delete-remote-branch": "delete"}, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.DeleteRemoteBranch{Remote: remote, Branch: branch}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
```

Add `"strings"` to the imports if not present.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run TestRemoteRm -v`
Expected: PASS. Then `go test ./internal/cli/` for no regressions.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/remote.go internal/cli/remote_test.go internal/cli/cli.go
git commit  # "feat(cli): gg remote rm <remote>/<branch>" + trailers
```

---

### Task 5: e2e scenario

**Files:**
- Create: `e2e/scenarios/s70_remote_rm.toml`

**Interfaces:** consumes the declarative harness (`[input.origin]` steps incl. `{ branch = ... }`; `[expect.origin] branches = [...]`).

- [ ] **Step 1: Write the scenario**

Create `e2e/scenarios/s70_remote_rm.toml`:

```toml
name = "remote rm: delete a branch on origin"

[input.origin]
steps = [
  { write = "f.txt", content = "v1\n" },
  { commit = "c1" },
  { branch = "doomed" },
]

[[run]]
cmd  = ["remote", "fetch"]
exit = 0

[[run]]
cmd  = ["remote", "rm", "origin/doomed"]
exit = 0

[expect.origin]
branches = ["main"]
```

Verify against the `writing-e2e-scenarios` skill: (a) that `{ branch = "doomed" }` in `[input.origin].steps` creates a branch on origin without switching origin's HEAD off `main`; (b) that `[expect.origin].branches` asserts the exact set. If the origin branch step needs a different spelling, or origin's default branch isn't `main`, adjust to match the harness — keep the assertion "origin no longer has `doomed`".

- [ ] **Step 2: Run the scenario**

Run: `./test.sh e2e` (or the e2e package test entrypoint). 
Expected: the new scenario passes; assert failure would read `origin branches: want [main], got [doomed main]`.

- [ ] **Step 3: Commit**

```bash
git add e2e/scenarios/s70_remote_rm.toml
git commit  # "test(e2e): s70 remote rm deletes a branch on origin" + trailers
```

---

### Task 6: Docs + agentskill

**Files:**
- Modify: `CHANGELOG.md` (Unreleased → Added)
- Modify: `README.md` (Remotes menu + CLI cheatsheet near `gg remote ls|fetch|prune`)
- Modify: `internal/agentskill/using-gg.md` (document `gg remote rm`)
- Modify: `internal/agentskill/agentskill.go` (bump `Version`)

**Interfaces:** none (docs + embedded skill).

- [ ] **Step 1: CHANGELOG**

Under `## [Unreleased]` → `### Added` in `CHANGELOG.md`, add:

```markdown
- **Delete a remote branch.** The Remotes panel `.` menu now offers **Delete `<remote>/<branch>`** (with a confirm prompt), and the CLI adds `gg remote rm <remote>/<branch>` — `git push <remote> --delete`. Destructive and outward-facing, so the TUI confirms; the CLI command is the confirmation.
```

If a concurrent branch already opened the Unreleased/Added block, append the bullet rather than duplicating headings.

- [ ] **Step 2: README**

In `README.md`: (a) add **Delete `<remote>/<branch>`** to the Remotes `.`-menu list (the line documenting Prune on the Remotes tab); (b) add `gg remote rm <remote>/<branch>   # delete a remote branch (git push --delete)` to the CLI cheatsheet beside `gg remote ls | fetch | prune`. Match the surrounding style.

- [ ] **Step 3: agentskill — document the command + bump Version**

In `internal/agentskill/using-gg.md`, find where `gg remote` subcommands are documented and add `gg remote rm <remote>/<branch>`. Then bump the version constant in `internal/agentskill/agentskill.go` by 1 (find `const Version =`).

- [ ] **Step 4: Refresh installed skill copies**

Run: `go run ./cmd/gg init --update`
Expected: regenerates `.claude/skills/using-gg/SKILL.md` so `TestDogfoodSkillCopyInSync` passes. Then run `go test ./internal/agentskill/` to confirm.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md internal/agentskill/ .claude/skills/using-gg/
git commit  # "docs: delete remote branch (menu + gg remote rm); agentskill bump" + trailers
```

---

## Self-review notes

- **Spec coverage:** git verb → T1; engine op + interface → T2; TUI → T3; CLI → T4; e2e → T5; docs + agentskill → T6. All spec sections covered.
- **Dropped from spec:** `opname.go` change — `OpName` is reflection-based (`fmt.Sprintf("%T")`), so `DeleteRemoteBranch` is named automatically; no registration. The engine real-bare-remote integration test is dropped in favour of the git-verb real-remote test (T1) + the e2e (T5); the engine tests stay FakeRunner-based (matching `push_tag_test.go`), avoiding a redundant heavy setup.
- **Post-op refresh (TUI):** none added — the default `opFinishedMsg` Snapshot already reloads `remoteBranches` (same as Prune/Fetch). Do NOT add a bespoke refresh path.
- **Type consistency:** op `DeleteRemoteBranch{Remote, Branch}`, verb `PushDelete(remote, branch)`, decision id `delete-remote-branch`, row id `remote-delete`, span name `"git push delete"` — identical across tasks and tests.
- **Verified before writing:** `git` test helpers `newClonePair`/`gitIn`; engine `MapDecider`/`OpDeps{Repo,Decider}` + `FakeRunner.Calls{.Name,.Argv}` (push_tag_test pattern); `GitOps` interface shape (Push/PushTag siblings); `OpName` reflection-based; `Prune` = `RefWrite` + emit/Done; CLI helpers `runCLI`/`cloneWithRemoteFoo`/`runGit` + `cmdRemote` signature (no stdin yet, call site `cli.go:87`); e2e `[input.origin]` `{ branch }` step + `[expect.origin].branches`.
- **Confirm at execution (adapt, don't redesign):** the exact `git` package string-output helper name in `sync_test.go` (`gitOut` vs `runGit`/`revParse` — Task 1 grep); the e2e origin-branch step spelling against the `writing-e2e-scenarios` skill (Task 5).
