# Delete Tag From Remote (Tags-B) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete a tag on a remote — `git push <remote> --delete refs/tags/<tag>` — from the Tags `.` menu (TUI) and the CLI (`gg tag rm <tag> --remote [<name>]`).

**Architecture:** New `PushDeleteTag` git verb → `engine.DeleteRemoteTag` op (resolve the remote like `PushTag`: auto / `delete-remote-tag-remote` Decider, then a `delete-remote-tag` confirm) → wired into the Tags menu and the `gg tag rm` CLI. Mirrors the shipped `DeleteRemoteBranch`/`PushDelete` and `PushTag` patterns.

**Tech Stack:** Go 1.26, `internal/git` verbs, `internal/engine` ops + `Decider`, Bubble Tea TUI, CLI `cliDecider` policy, declarative e2e harness with a git server.

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26.
- A git verb is one invocation via `gitcmd`, run with `r.Runner.Run`.
- Operations confirm via `deps.decide`; never block on a human.
- `internal/tui` / `internal/cli` never import `internal/git` for production logic (tests may build `&git.Repo{Runner: fake}`).
- TUI `Model` is a value receiver.
- Destructive + outward-facing: a single TUI keypress must not delete a remote ref unconfirmed; the CLI command is the confirmation (pre-answers the confirm Decider).
- CLI flags precede positionals (repo convention).
- Every commit message ends with these two trailers, verbatim:
  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro
  ```
- Run `./test.sh race` before merge (human's call).

---

### Task 1: `git.Repo.PushDeleteTag` verb

**Files:**
- Modify: `internal/git/sync.go` (after `PushTag`/`PushDelete`)
- Modify: `internal/git/sync_test.go`

**Interfaces:**
- Consumes: `gitcmd.New`, `r.Runner.Run`; `newClonePair(t) (string, gitexec.Runner)` test helper; the package's `gitOut`/`runGit` string-returning git helper (grep `func gitOut\|func revParse` to confirm the name) + `gitIn`.
- Produces: `func (r *Repo) PushDeleteTag(ctx context.Context, remote, tag string) error` — runs `git push <remote> --delete refs/tags/<tag>`, span name `"git push delete tag"`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/git/sync_test.go`:

```go
func TestPushDeleteTagArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git push delete tag", gitexec.Result{})
	repo := &Repo{Runner: f}
	if err := repo.PushDeleteTag(context.Background(), "origin", "v1.0.0"); err != nil {
		t.Fatalf("PushDeleteTag: %v", err)
	}
	var argv []string
	for _, c := range f.Calls {
		if c.Name == "git push delete tag" {
			argv = c.Argv
		}
	}
	want := []string{"push", "origin", "--delete", "refs/tags/v1.0.0"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
}

func TestPushDeleteTagRemovesTagOnRemote(t *testing.T) {
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}
	gitIn(t, clone, "tag", "v1.0.0")
	gitIn(t, clone, "push", "origin", "v1.0.0")
	if err := repo.PushDeleteTag(context.Background(), "origin", "v1.0.0"); err != nil {
		t.Fatalf("PushDeleteTag: %v", err)
	}
	if out := gitOut(t, clone, "ls-remote", "--tags", "origin", "v1.0.0"); strings.TrimSpace(out) != "" {
		t.Fatalf("origin still has tag v1.0.0: %q", out)
	}
}
```

Use the actual string-returning git helper name from this package (grep first; likely `gitOut`).

- [ ] **Step 2: Run to verify fail**

Run: `go test ./internal/git/ -run TestPushDeleteTag -v`
Expected: FAIL — `repo.PushDeleteTag undefined`.

- [ ] **Step 3: Implement the verb**

In `internal/git/sync.go`, after `PushDelete`:

```go
// PushDeleteTag deletes tag on remote (git push <remote> --delete
// refs/tags/<tag>). The full refs/tags/ ref disambiguates from a same-named
// branch.
func (r *Repo) PushDeleteTag(ctx context.Context, remote, tag string) error {
	argv := gitcmd.New("push").Arg(remote, "--delete", "refs/tags/"+tag).ToArgv()
	_, err := r.Runner.Run(ctx, "git push delete tag", argv)
	return err
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/git/ -run TestPushDeleteTag -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/sync.go internal/git/sync_test.go
git commit  # "feat(git): PushDeleteTag verb (git push --delete refs/tags/<tag>)" + trailers
```

---

### Task 2: `engine.DeleteRemoteTag` op

**Files:**
- Create: `internal/engine/delete_remote_tag.go`, `internal/engine/delete_remote_tag_test.go`
- Modify: `internal/engine/gitops.go` (add `PushDeleteTag` beside `PushDelete`, ~line 34)

**Interfaces:**
- Consumes: `deps.Repo.RemoteNames`, `deps.Repo.PushDeleteTag` (Task 1, via GitOps), `deps.decide`, `deps.emit`, `repogate.RefWrite`, `MapDecider` (test).
- Produces: `engine.DeleteRemoteTag{Tag, Remote string}` with `Run` + `LockMode()`.

- [ ] **Step 1: Add `PushDeleteTag` to the `GitOps` interface**

In `internal/engine/gitops.go`, after the `PushDelete` line (~34):

```go
	PushDeleteTag(ctx context.Context, remote, tag string) error
```

- [ ] **Step 2: Write the failing tests**

Create `internal/engine/delete_remote_tag_test.go`:

```go
package engine

import (
	"context"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
)

func delTagFakeRepo(remotes string) (*git.Repo, *gitexec.FakeRunner) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git remote", gitexec.Result{Stdout: remotes})
	f.SetResponse("git push delete tag", gitexec.Result{})
	return &git.Repo{Runner: f}, f
}

func deleteTagCalled(f *gitexec.FakeRunner) (remote, tag string, ok bool) {
	for _, c := range f.Calls {
		if c.Name == "git push delete tag" && len(c.Argv) >= 4 {
			return c.Argv[1], c.Argv[3], true // ["push", remote, "--delete", "refs/tags/<tag>"]
		}
	}
	return "", "", false
}

func TestDeleteRemoteTagSingleRemoteConfirm(t *testing.T) {
	repo, f := delTagFakeRepo("origin\n")
	res, err := DeleteRemoteTag{Tag: "v1.0.0"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-remote-tag": "delete"}})
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	r, ref, ok := deleteTagCalled(f)
	if !ok || r != "origin" || ref != "refs/tags/v1.0.0" {
		t.Fatalf("push delete tag called with (%q,%q) ok=%v", r, ref, ok)
	}
}

func TestDeleteRemoteTagAbortDoesNotPush(t *testing.T) {
	repo, f := delTagFakeRepo("origin\n")
	res, err := DeleteRemoteTag{Tag: "v1.0.0"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-remote-tag": "abort"}})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Changed {
		t.Fatal("abort must not change anything")
	}
	if _, _, ok := deleteTagCalled(f); ok {
		t.Fatal("abort must not push a deletion")
	}
}

func TestDeleteRemoteTagMultiRemotePick(t *testing.T) {
	repo, f := delTagFakeRepo("origin\nbackup\n")
	_, err := DeleteRemoteTag{Tag: "v1.0.0"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-remote-tag-remote": "backup", "delete-remote-tag": "delete"}})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if r, _, ok := deleteTagCalled(f); !ok || r != "backup" {
		t.Fatalf("pushed to %q ok=%v, want backup", r, ok)
	}
}

func TestDeleteRemoteTagRequiresTag(t *testing.T) {
	repo, _ := delTagFakeRepo("origin\n")
	if _, err := (DeleteRemoteTag{}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("missing Tag must error")
	}
}
```

- [ ] **Step 3: Run to verify fail**

Run: `go test ./internal/engine/ -run TestDeleteRemoteTag -v`
Expected: FAIL — `DeleteRemoteTag` undefined.

- [ ] **Step 4: Implement the op**

Create `internal/engine/delete_remote_tag.go`:

```go
package engine

import (
	"context"
	"fmt"

	"github.com/gigagit/gg/internal/repogate"
)

// DeleteRemoteTag deletes a tag on a remote (git push <remote> --delete
// refs/tags/<tag>). The remote is resolved like PushTag (auto for one, else a
// Decider pick); then a confirm gates the push (destructive + outward-facing).
// The CLI pre-answers the confirm. RefWrite: mutates remote refs.
type DeleteRemoteTag struct {
	Tag    string // required
	Remote string // "" = resolve (auto when one remote, else Decider)
}

func (op DeleteRemoteTag) LockMode() repogate.Mode { return repogate.RefWrite }

func (op DeleteRemoteTag) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Tag == "" {
		return Result{}, fmt.Errorf("delete remote tag: Tag is required")
	}
	remote := op.Remote
	if remote == "" {
		names, err := deps.Repo.RemoteNames(ctx)
		if err != nil {
			return Result{}, fmt.Errorf("delete remote tag: %w", err)
		}
		switch len(names) {
		case 0:
			return Result{}, fmt.Errorf("delete remote tag: no remotes configured")
		case 1:
			remote = names[0]
		default:
			choice, derr := deps.decide(ctx, DecisionRequest{
				ID:      "delete-remote-tag-remote",
				Prompt:  "Delete " + op.Tag + " from which remote?",
				Options: append(append([]string{}, names...), "abort"),
			})
			if derr != nil {
				return Result{}, derr
			}
			if choice.Option == "abort" {
				return Result{Changed: false}, nil
			}
			remote = choice.Option
		}
	}

	confirm, err := deps.decide(ctx, DecisionRequest{
		ID:      "delete-remote-tag",
		Prompt:  "Delete tag " + op.Tag + " from " + remote + "? This pushes a deletion to " + remote + ".",
		Options: []string{"delete", "abort"},
	})
	if err != nil {
		return Result{}, err
	}
	if confirm.Option != "delete" {
		return Result{Summary: "cancelled", Changed: false}, nil
	}

	deps.emit(ctx, Progress{Step: "deleting remote tag", Detail: op.Tag + " ← " + remote})
	if err := deps.Repo.PushDeleteTag(ctx, remote, op.Tag); err != nil {
		return Result{}, fmt.Errorf("delete remote tag: %w", err)
	}
	res := Result{Summary: "deleted " + op.Tag + " from " + remote, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = DeleteRemoteTag{}
```

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/engine/ -run TestDeleteRemoteTag -v`
Expected: PASS. Then `go test ./internal/engine/` for no interface-break regressions.

- [ ] **Step 6: Commit**

```bash
git add internal/engine/delete_remote_tag.go internal/engine/delete_remote_tag_test.go internal/engine/gitops.go
git commit  # "feat(engine): DeleteRemoteTag op (resolve remote + confirm)" + trailers
```

---

### Task 3: TUI Tags-row "Delete from remote"

**Files:**
- Modify: `internal/tui/tags_actions.go` (+ `tagDeleteRemoteRow`)
- Modify: `internal/tui/action_menu.go` (wire beside the other tag rows, ~lines 245-253)
- Modify: `internal/tui/tags_actions_test.go`

**Interfaces:**
- Consumes: `Model.opsIdle`, `Model.backingIndex(panelTags)`, `Model.startOp`, `engine.DeleteRemoteTag`, `model.Tag{Name}`. Tests: `footerModel`, `ids`, `availableActions`, `domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})`.
- Produces: `tagDeleteRemoteRow() (actionRow, bool)` (id `tag-delete-remote`).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/tags_actions_test.go` (imports `domain`/`git`/`gitexec`/`model` are already present from the Tags-A work):

```go
func TestTagDeleteRemoteRowPresent(t *testing.T) {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	got := ids(availableActions(m))
	if !got["tag-delete-remote"] {
		t.Fatalf("expected tag-delete-remote; got %v", got)
	}
}

func TestTagDeleteRemoteRowInertOffTagsPanel(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	if _, ok := m.tagDeleteRemoteRow(); ok {
		t.Fatal("tag-delete-remote must be inert off the Tags panel")
	}
}

func TestTagDeleteRemoteRowDispatches(t *testing.T) {
	m := footerModel()
	m.focus = panelTags
	m.tags = []model.Tag{{Name: "v1.0.0", Target: "abc1234"}}
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	row, ok := m.tagDeleteRemoteRow()
	if !ok {
		t.Fatal("tagDeleteRemoteRow not available")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("delete-remote row run returned nil cmd")
	}
}
```

- [ ] **Step 2: Run to verify fail**

Run: `go test ./internal/tui/ -run TestTagDeleteRemote -v`
Expected: FAIL — `m.tagDeleteRemoteRow undefined`.

- [ ] **Step 3: Implement the row**

In `internal/tui/tags_actions.go`, add:

```go
// tagDeleteRemoteRow offers "Delete <tag> from remote" on the Tags panel. The
// engine resolves the remote (auto/pick) and confirms via the Decider (surfaced
// as the TUI modal); a single keypress never deletes a remote ref unconfirmed.
func (m Model) tagDeleteRemoteRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-delete-remote",
		label: "Delete " + name + " from remote",
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.startOp(engine.DeleteRemoteTag{Tag: name}) },
	}, true
}
```

- [ ] **Step 4: Wire it into `availableActions`**

In `internal/tui/action_menu.go`, beside the other tag-row appends (after `tagDeleteRow`), add:

```go
	if r, ok := m.tagDeleteRemoteRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/tui/ -run TestTagDeleteRemote -v`
Expected: PASS. Then `go test ./internal/tui/` for no regressions.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/tags_actions.go internal/tui/action_menu.go internal/tui/tags_actions_test.go
git commit  # "feat(tui): Delete tag from remote on the Tags . menu" + trailers
```

---

### Task 4: CLI `gg tag rm --remote`

**Files:**
- Modify: `internal/cli/tag.go` (`cmdTag` gains `stdin`; `cmdTagDelete` gains `--remote`)
- Modify: `internal/cli/cli.go` (call site ~line 89)
- Modify: `internal/cli/tag_test.go`

**Interfaces:**
- Consumes: `runOperation`, `cliDecider{policy, in, out, interactive}`, `stdinIsTerminal()`, `finish`, `engine.DeleteTag`, `engine.DeleteRemoteTag`, `flag`.
- Produces: `cmdTagDelete(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int` (signature gains `stdin`); `cmdTag` gains `stdin io.Reader`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/tag_test.go`. Use the package's clone+bare-origin helper (`cloneWithRemoteFoo(t) string`) and `runGit(t, dir, ...) string` (grep to confirm names; both exist in this package's tests):

```go
func TestTagRmRemoteDeletesTagOnOrigin(t *testing.T) {
	clone := cloneWithRemoteFoo(t)
	origin := runGit(t, clone, "config", "--get", "remote.origin.url")
	runGit(t, clone, "tag", "v1.0.0")
	runGit(t, clone, "push", "origin", "v1.0.0")

	if code, _, errb := runCLI(t, clone, "tag", "rm", "--remote", "v1.0.0", "origin"); code != 0 {
		t.Fatalf("tag rm --remote exit = %d (stderr: %s)", code, errb)
	}
	if out := runGit(t, origin, "tag", "-l", "v1.0.0"); strings.TrimSpace(out) != "" {
		t.Fatalf("origin still has tag v1.0.0: %q", out)
	}
}

func TestTagRmLocalStillLocalOnly(t *testing.T) {
	clone := cloneWithRemoteFoo(t)
	origin := runGit(t, clone, "config", "--get", "remote.origin.url")
	runGit(t, clone, "tag", "v1.0.0")
	runGit(t, clone, "push", "origin", "v1.0.0")

	if code, _, errb := runCLI(t, clone, "tag", "rm", "v1.0.0"); code != 0 {
		t.Fatalf("tag rm exit = %d (stderr: %s)", code, errb)
	}
	// local gone, origin untouched
	if out := runGit(t, clone, "tag", "-l", "v1.0.0"); strings.TrimSpace(out) != "" {
		t.Fatalf("local tag v1.0.0 not deleted: %q", out)
	}
	if out := runGit(t, origin, "tag", "-l", "v1.0.0"); strings.TrimSpace(out) == "" {
		t.Fatal("local rm must not touch the origin tag")
	}
}
```

If `cloneWithRemoteFoo`/`runGit` have different names in this package, grep (`func cloneWith`, `func runGit`) and use the real ones; do not invent helpers.

- [ ] **Step 2: Run to verify fail**

Run: `go test ./internal/cli/ -run 'TestTagRm' -v`
Expected: FAIL — `--remote` unrecognized (the current `cmdTagDelete` rejects extra args / the flag) so the remote test exits non-zero, and/or a compile error once you change the signature.

- [ ] **Step 3: Thread `stdin` + add `--remote`**

In `internal/cli/cli.go`, change the call site (~line 89) from `cmdTag(svc, rest, stdout, stderr)` to `cmdTag(svc, rest, stdin, stdout, stderr)`.

In `internal/cli/tag.go`, change `cmdTag`'s signature to `func cmdTag(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int` and update its `rm`/`delete` case to `return cmdTagDelete(svc, args[1:], stdin, stdout, stderr)`. (Other cases keep their existing signatures.) Replace `cmdTagDelete` with:

```go
// cmdTagDelete implements `gg tag rm [--remote] <name> [<remote>]` (alias
// delete). Default deletes the tag locally (typing the command is the
// confirmation). --remote deletes it on the remote instead: the remote is the
// optional second positional, else auto-resolved; the confirm is pre-answered.
func cmdTagDelete(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tag rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	remote := fs.Bool("remote", false, "delete the tag on the remote (git push --delete) instead of locally")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg tag rm [--remote] <name> [<remote>]")
		return 2
	}
	name := fs.Arg(0)

	if !*remote {
		res, err := runOperation(context.Background(), svc,
			engine.DeleteTag{Name: name}, cliDecider{}, stderr)
		return finish(res, err, stdout, stderr)
	}

	rem := ""
	if fs.NArg() >= 2 {
		rem = fs.Arg(1)
	}
	dec := cliDecider{policy: map[string]string{"delete-remote-tag": "delete"}, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.DeleteRemoteTag{Tag: name, Remote: rem}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
```

Add `"flag"` to the imports if not already present. (`cmdTagCreate` already uses `flag`, so it likely is.)

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/cli/ -run 'TestTagRm' -v`
Expected: PASS. Then `go test ./internal/cli/` for no regressions.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/tag.go internal/cli/cli.go internal/cli/tag_test.go
git commit  # "feat(cli): gg tag rm --remote [<remote>] (delete a tag upstream)" + trailers
```

---

### Task 5: e2e scenario

**Files:**
- Create: `e2e/scenarios/s73_tag_rm_remote.toml`

- [ ] **Step 1: Write the scenario**

Create `e2e/scenarios/s73_tag_rm_remote.toml`:

```toml
name = "tag rm --remote: delete a tag on origin"

[input.origin]
steps = [
  { write = "f.txt", content = "v1\n" },
  { commit = "c1" },
  { tag = "v1.0.0" },
]

[[run]]
cmd  = ["tag", "rm", "--remote", "v1.0.0", "origin"]
exit = 0

[expect.origin]
tags = []
```

Verify against the `writing-e2e-scenarios` skill: that `{ tag = "v1.0.0" }` in `[input.origin].steps` creates a tag on origin, and that `[expect.origin] tags = []` asserts origin has no tags. If the local sandbox needs the tag locally first for the command, note that `gg tag rm --remote` operates directly on the remote (the verb pushes `--delete refs/tags/...`), so the local clone does not need the tag — but if the harness's origin-tag step or assertion needs adjusting, match what makes the scenario genuinely prove the tag is gone from origin (do not weaken the assertion).

- [ ] **Step 2: Run the scenario**

Run: `go test ./e2e/ -run 'TestScenarios/s73_tag_rm_remote' -v`
Expected: PASS. A no-op delete would leave `v1.0.0` on origin → `origin tags: want [], got [v1.0.0]`.

- [ ] **Step 3: Commit**

```bash
git add e2e/scenarios/s73_tag_rm_remote.toml
git commit  # "test(e2e): s73 tag rm --remote deletes a tag on origin" + trailers
```

---

### Task 6: Docs + agentskill

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go`

- [ ] **Step 1: CHANGELOG**

Under `## [Unreleased]` → `### Added`, add:

```markdown
- **Delete a tag from a remote.** The Tags panel `.` menu now offers **Delete `<tag>` from remote** (with a confirm prompt), and the CLI extends `gg tag rm <tag> --remote [<remote>]` — `git push <remote> --delete refs/tags/<tag>`. The local `gg tag rm <tag>` is unchanged.
```

If a concurrent branch already opened the Added block, append rather than duplicate.

- [ ] **Step 2: README**

In `README.md`: (a) add **Delete `<tag>` from remote** to the Tags `.`-menu description (the line listing the tag menu actions, added in Tags-A); (b) update the CLI cheatsheet `gg tag ...` line so the `rm` entry shows `gg tag rm [--remote] <name> [<remote>]`. Match the surrounding style.

- [ ] **Step 3: agentskill — document + bump Version**

In `internal/agentskill/using-gg.md`, update the `gg tag rm` documentation to include `--remote [<remote>]`. Bump the `Version` constant in `internal/agentskill/agentskill.go` by exactly 1.

- [ ] **Step 4: Refresh installed skill copies**

Run: `go run ./cmd/gg init --update`
Then: `go test ./internal/agentskill/` — `TestDogfoodSkillCopyInSync` must pass.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md internal/agentskill/ .claude/skills/using-gg/
git commit  # "docs: delete tag from remote (menu + gg tag rm --remote); agentskill bump" + trailers
```

---

## Self-review notes

- **Spec coverage:** verb → T1; op + interface → T2; TUI → T3; CLI → T4; e2e → T5; docs + agentskill → T6. All covered.
- **Verified before writing:** `git` test helpers `newClonePair`/`gitIn`; `FakeRunner.Calls{.Name,.Argv}` + `"git remote"` span for `RemoteNames` (push_tag pattern); `GitOps` has `RemoteNames`/`PushTag`/`PushDelete`/`CommitExists` (add `PushDeleteTag` beside `PushDelete`); `OpName` reflection-based; CLI helpers `runCLI`/`cloneWithRemoteFoo`/`runGit`/`newRepoDir`; `cmdTag` signature (no stdin yet, call site `cli.go:89`); current `cmdTagDelete` is `(svc, args, stdout, stderr)`; e2e `[input.origin]` `{ tag }` step + `[expect.origin] tags`.
- **Type consistency:** op `DeleteRemoteTag{Tag, Remote}`, verb `PushDeleteTag(remote, tag)`, decision ids `delete-remote-tag-remote` (pick) + `delete-remote-tag` (confirm), row id `tag-delete-remote`, span `"git push delete tag"` — identical across tasks/tests.
- **Confirm at execution (adapt, don't redesign):** the `git` package string-output helper name in `sync_test.go` (`gitOut` vs `runGit`/`revParse`); the e2e origin-tag step/assertion spelling vs the `writing-e2e-scenarios` skill.
