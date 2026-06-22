# Delete Remote Branch (GitKraken parity, Bucket B.1) — Design

**Date:** 2026-06-23
**Status:** Approved, ready for plan

## Goal

Let a user delete a remote branch — `git push <remote> --delete <branch>` — from
the Remotes panel `.` menu (TUI) and from the CLI (`gg remote rm
<remote>/<branch>`). This is the first genuinely new capability in the GitKraken
remote-menu gap analysis (Bucket B): gg today can only *prune* stale local
tracking refs, never delete the branch on the remote.

## Background

`gg`'s existing ref-deletion is `engine.DeleteBranch` (local only). Remote
deletion is missing end to end: no git verb, no engine op, no frontend. The
operation is **destructive and outward-facing** — it pushes a deletion to a
shared remote — so it requires confirmation, and the engine's `Decider` is the
established way gg confirms destructive single-keypress actions (see
`DeleteBranch`).

## Decisions (from brainstorming)

- **Confirmation:** a simple yes/no `Decider` confirm modal naming the branch and
  remote — same shape as the local-branch delete. (Not typed confirmation.)
- **CLI surface:** `gg remote rm <remote>/<branch>`, fitting the existing `gg
  remote ls|fetch|prune` group. The command itself is the confirmation (the CLI
  pre-answers the Decider), mirroring `gg branch rm`.

## Architecture & components

A new git verb, a new engine op, and thin frontend wiring on both the TUI menu
and the CLI `remote` group — following the exact patterns of `DeleteBranch`
(engine confirm) and `Prune` (remote-ref-mutating op + post-op refresh).

### Git verb — `git.Repo.PushDelete`

`internal/git/sync.go` (beside `Push`/`PushTag`):

```go
// PushDelete deletes branch on remote (git push <remote> --delete <branch>).
func (r *Repo) PushDelete(ctx context.Context, remote, branch string) error {
	argv := gitcmd.New("push").Arg(remote, "--delete", branch).ToArgv()
	_, err := r.Runner.Run(ctx, "git push delete", argv)
	return err
}
```

### Engine op — `engine.DeleteRemoteBranch`

`internal/engine/delete_remote_branch.go`:

- Fields `Remote, Branch string` (both required; error if empty).
- Decision `delete-remote-branch` with options `["delete", "abort"]`, prompt:
  `Delete remote branch <remote>/<branch>? This pushes a deletion to <remote>.`
- On `delete`: `deps.Repo.PushDelete(ctx, op.Remote, op.Branch)`; return
  `Result{Summary: "deleted <remote>/<branch>", Changed: true}`. On `abort`:
  `Result{Summary: "cancelled", Changed: false}`.
- `OpName()` returns `"delete-remote-branch"` (or the package's naming
  convention — match the sibling ops in `opname.go`).
- `LockMode()` mirrors `Prune` (both mutate remote-tracking refs). If `Prune`
  does not declare a non-default `LockMode`, neither does this op.

`PushDelete` is added to the `engine.GitOps` interface
(`internal/engine/gitops.go`) so ops can call it; `*git.Repo` satisfies it.

### TUI — Remotes `.` menu row

`internal/tui/remote_actions.go`:

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

Wired in `availableActions` after the other remote rows. Post-op refresh: the
remote-branch list changes, so the completion path must refresh refs the same
way `Prune` does (whatever `Prune`'s `opFinishedMsg` handling triggers —
full `Snapshot` or the targeted refs refresh; match it exactly, do not invent a
new refresh path).

### CLI — `gg remote rm`

`internal/cli/remote.go`, extend `cmdRemote`'s switch:

```go
case args[0] == "rm" || args[0] == "remove":
	return cmdRemoteRm(svc, args[1:], stdout, stderr)
```

`cmdRemoteRm` parses `<remote>/<branch>` (split on the FIRST `/`; error with a
usage message if there's no `/` or either side is empty), then:

```go
policy := map[string]string{"delete-remote-branch": "delete"}
res, err := runOperationWith(ctx, svc, engine.DeleteRemoteBranch{Remote: remote, Branch: branch}, policy, stderr)
```

Use the same decider-with-policy helper `gg branch rm` uses (see
`internal/cli/branch.go`'s `policy` + `runOperation*` call — match its exact
helper name). Update the default-case help string to `ls, fetch, prune, rm`.

Note on `<remote>/<branch>` parsing: branch names may contain `/`
(`feat/x`), and remote names do not, so split on the first `/` — left =
remote, right = branch.

## Error handling

- **Missing Remote/Branch** (engine) → error before any decision.
- **Bad CLI arg** (no `/`, empty side) → usage error, exit 2.
- **Push failure** (no such branch upstream, no permission, network) → the
  `git push --delete` error propagates as the op error; the TUI shows it in the
  status/modal, the CLI prints it and exits non-zero. No special handling.
- **Confirm declined** (TUI `abort`) → `Result{Changed: false}`, no push.

## Testing

- **git verb** (`internal/git/sync_test.go` or a new test): FakeRunner asserts
  the argv `push <remote> --delete <branch>`; a real-git integration creating a
  bare "remote" + a branch, then `PushDelete`, asserts the branch is gone from
  the bare repo (mirror how `delete_branch_test.go` uses a real repo).
- **engine** (`internal/engine/delete_remote_branch_test.go`): FakeRunner +
  scripted Decider — `delete` runs the push, `abort` does not, missing fields
  error; a real bare-remote integration proving the branch is actually deleted.
- **CLI** (`internal/cli/remote_test.go`): `remote rm origin/foo` against a real
  repo with a bare origin deletes the branch and exits 0; `remote rm foo` (no
  slash) exits 2 with usage; unknown-subcommand help lists `rm`.
- **TUI** (`internal/tui/remote_actions_test.go`): `remote-delete` row present
  when a remote row is selected + idle, absent otherwise; dispatch starts the op
  (fake svc); the confirm modal is requested (assert via the op/decider path used
  by existing destructive-op tests).
- **e2e** (`e2e/scenarios/s70_remote_rm.toml`): git-server harness — origin has
  a branch; `remote rm origin/<branch>` exits 0; assert the branch no longer
  exists on origin. (Confirm the exact origin-branch assertion key with the
  `writing-e2e-scenarios` skill; if no "branch absent" assertion exists, assert
  via the remote branch listing instead.)

## Files

- Modify: `internal/git/sync.go` (+ `PushDelete`), `internal/git/sync_test.go`.
- Create: `internal/engine/delete_remote_branch.go`, `internal/engine/delete_remote_branch_test.go`.
- Modify: `internal/engine/gitops.go` (add `PushDelete` to the interface), `internal/engine/opname.go` (if op names are registered there).
- Modify: `internal/tui/remote_actions.go` (+ `remoteDeleteRow`), `internal/tui/action_menu.go` (wire it), `internal/tui/remote_actions_test.go`.
- Modify: `internal/cli/remote.go` (+ `cmdRemoteRm`), `internal/cli/remote_test.go`.
- Modify: `internal/agentskill/using-gg.md` + bump `internal/agentskill/agentskill.go` `Version`, then `gg init --update` to refresh installed copies.
- Create: `e2e/scenarios/s70_remote_rm.toml`.
- Modify: `CHANGELOG.md`, `README.md`.

## Non-goals

- Copy web links (Bucket B.2 — separate feature).
- Deleting the *remote itself* (`git remote remove`), as opposed to a remote
  branch.
- Force/typed confirmation, multi-branch batch delete.
