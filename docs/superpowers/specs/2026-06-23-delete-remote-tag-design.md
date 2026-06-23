# Delete Tag From Remote (GitKraken parity, Tags-B) — Design

**Date:** 2026-06-23
**Status:** Approved, ready for plan

## Goal

Let a user delete a tag on a remote — `git push <remote> --delete refs/tags/<tag>`
— from the Tags panel `.` menu (TUI) and the CLI (`gg tag rm <tag> --remote
[<name>]`). gg today can only delete a tag locally (`engine.DeleteTag`); the
remote side is missing. This is the tag-parallel of the shipped delete-remote-
branch feature, reusing `PushTag`'s remote-resolution pattern.

## Background

`engine.DeleteTag` deletes a local tag (no confirm — typing the command/menu row
is the confirmation). Remote tag deletion is missing end to end. Like the remote-
branch delete it is **destructive and outward-facing** (it pushes a deletion to a
shared remote), so it confirms via the `Decider`. The remote is resolved exactly
as `PushTag` resolves it: auto when one remote is configured, else a Decider pick.

## Decisions (from brainstorming)

- **CLI:** extend the existing `gg tag rm` — `gg tag rm [--remote] <tag>
  [<remote>]`. Default deletes locally (unchanged); `--remote` deletes on the
  remote. The optional positional `<remote>` pins the remote; omitted, it
  auto-resolves.
- **Flow:** resolve the remote first (auto / Decider pick), THEN confirm — so the
  confirm names the exact remote ("Delete v1.0.0 from origin?").

## Architecture & components

A new git verb, a new engine op, and frontend wiring — mirroring
`DeleteRemoteBranch` (confirm) and `PushTag` (remote resolution).

### Git verb — `git.Repo.PushDeleteTag`

`internal/git/sync.go` (beside `PushTag`/`PushDelete`):

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

Added to the `engine.GitOps` interface; `*git.Repo` satisfies it.

### Engine op — `engine.DeleteRemoteTag`

`internal/engine/delete_remote_tag.go`. Fields `Tag, Remote string` (Tag
required). `LockMode()` returns `repogate.RefWrite` (mirrors `Prune` /
`DeleteRemoteBranch`).

Run:
1. Guard `Tag != ""`.
2. **Resolve remote** (copy `PushTag`'s logic): if `Remote == ""` →
   `RemoteNames` → 0 = error "no remotes configured", 1 = auto, multiple = decide
   `delete-remote-tag-remote` (options = remote names + `abort`; `abort` →
   `Result{Changed:false}`).
3. **Confirm** (remote now known): decide `delete-remote-tag`, options
   `["delete", "abort"]`, prompt `Delete tag <tag> from <remote>? This pushes a
   deletion to <remote>.`; non-`delete` → `Result{Changed:false}`.
4. `deps.emit(Progress{...})`; `deps.Repo.PushDeleteTag(ctx, remote, op.Tag)`;
   `Result{Summary: "deleted <tag> from <remote>", Changed: true}`; emit `Done`.

### TUI — Tags `.` menu

`internal/tui/tags_actions.go` — `tagDeleteRemoteRow`:

```go
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

The existing local `tagDeleteRow` ("Delete tag") stays — two delete entries,
mirroring GitKraken's "Delete locally" / "Delete from origin". Wired in
`availableActions` beside the other tag rows. The remote-pick (multi-remote) and
confirm surface as the existing TUI modal. Post-op refresh: the default
`opFinishedMsg` Snapshot reloads tags (same as the local delete / `PushTag`).

### CLI — `gg tag rm [--remote] <tag> [<remote>]`

`internal/cli/tag.go`. `cmdTag` gains a `stdin io.Reader` param (threaded from
`Run`, call site `cli.go:89`), passed to `cmdTagDelete`. `cmdTagDelete` parses a
`--remote` bool flag (flags precede positionals, per the repo convention):

- Not `--remote`: `engine.DeleteTag{Name: tag}` with `cliDecider{}` (unchanged
  local path).
- `--remote`: `engine.DeleteRemoteTag{Tag: tag, Remote: <Arg(1) or "">}` with
  `cliDecider{policy: {"delete-remote-tag": "delete"}, in: stdin, ...}`. An
  explicit `<remote>` skips the pick; omitted with multiple remotes, the op's
  remote Decider returns its "decision required" error (CLI can't interactively
  pick) → non-zero exit with a clear message; with one remote it auto-resolves.

Usage string updated to `gg tag rm [--remote] <name> [<remote>]`.

## Error handling

- **Missing tag** (engine/CLI) → error / usage, exit 2.
- **No remotes / multiple remotes without a name on the CLI** → the op errors;
  CLI prints it and exits non-zero.
- **Push failure** (tag absent upstream, no permission, network) → propagates as
  the op error; TUI shows it, CLI exits non-zero.
- **Confirm/remote-pick declined** → `Result{Changed:false}`, no push.

## Testing

- **git verb** (`internal/git/sync_test.go`): FakeRunner asserts argv `push
  <remote> --delete refs/tags/<tag>`; real bare-remote integration creating a tag
  on origin then `PushDeleteTag`, asserting it's gone (mirror the `PushDelete`
  test).
- **engine** (`internal/engine/delete_remote_tag_test.go`): FakeRunner + scripted
  Decider — single-remote auto + confirm runs the push; `abort` at confirm does
  not push; multi-remote pick routes to the chosen remote; missing Tag errors.
  (Inspect `FakeRunner.Calls` for the `git push delete tag` argv, like the
  push-tag tests.)
- **TUI** (`internal/tui/tags_actions_test.go`): `tag-delete-remote` row present
  when a tag is selected + idle, absent off the Tags panel; dispatch starts the
  op (fake svc).
- **CLI** (`internal/cli/tag_test.go`): `gg tag rm --remote <tag> origin` against
  a real repo with a bare origin holding the tag deletes it on origin (exit 0);
  `gg tag rm <tag>` still deletes only locally.
- **e2e** (`e2e/scenarios/s73_tag_rm_remote.toml`): origin set up with a tag;
  `gg tag rm --remote <tag> origin` exits 0; `[expect.origin] tags = []`.

## Files

- Modify: `internal/git/sync.go` (+ `PushDeleteTag`), `internal/git/sync_test.go`.
- Create: `internal/engine/delete_remote_tag.go`, `internal/engine/delete_remote_tag_test.go`.
- Modify: `internal/engine/gitops.go` (add `PushDeleteTag`).
- Modify: `internal/tui/tags_actions.go` (+ `tagDeleteRemoteRow`), `internal/tui/action_menu.go` (wire it), `internal/tui/tags_actions_test.go`.
- Modify: `internal/cli/tag.go` (+ `--remote` in `cmdTagDelete`, stdin threading), `internal/cli/cli.go` (call site), `internal/cli/tag_test.go`.
- Modify: `internal/agentskill/using-gg.md` + bump `internal/agentskill/agentskill.go` `Version`, then `gg init --update`.
- Create: `e2e/scenarios/s73_tag_rm_remote.toml`.
- Modify: `CHANGELOG.md`, `README.md`.

`OpName` is reflection-based — no registration needed.

## Non-goals

- Annotate an existing tag (Tags-C — separate feature).
- Copy link to tag on remote, hide/solo tags.
- Deleting multiple tags at once.
