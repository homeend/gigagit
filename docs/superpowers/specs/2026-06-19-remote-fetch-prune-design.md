# Remotes-tab fetch + prune — design

Date: 2026-06-19
Status: approved (brainstorm); ready for plan
Branch/worktree: `worktree-remote-fetch-prune`
Related: chunk 4 of the remotes effort (`2026-06-18-remotes-tab-design.md`,
`-remote-checkout-design.md`, `2026-06-19-e2e-remote-checkout-design.md`)

## Summary

Two distinct, network-backed actions that refresh the repo's remote-tracking
state, surfaced on the Remotes tab and as `gg remote` subcommands:

- **Fetch** (`git fetch --all`) — update every remote's tracking refs (no
  prune). The expensive, object-downloading op.
- **Prune** (`git remote prune <remotes…>`) — drop tracking refs for branches
  deleted upstream. Cheap ref-only cleanup, kept **separate** from fetch so a
  large-repo fetch and a quick prune aren't coupled.

Both refresh the Remotes tab and the local Branches `(↓N)` behind-indicator on
completion. This makes the behind-indicator meaningful (it is only as fresh as
the last fetch) and the Remotes list clean.

## Goals

- A `Fetch` engine op (`git fetch --all`) and a `Prune` engine op, both
  RefWrite, run through `domain.Execute`.
- TUI: **`f`** fetches on the Remotes tab; **Prune** is a `.`-menu action.
- CLI: `gg remote fetch` and `gg remote prune`.
- e2e coverage of both, via a new `stdout_excludes` `[[run]]` assertion for the
  prune (absence) case.

## Non-goals

- No combined "fetch+prune" single action (they are deliberately separate).
- No per-remote / selected-row targeting — both ops act on **all** remotes.
- No live byte-level transfer UI; Fetch streams git's own progress lines, Prune
  emits a single progress step.
- No fetch of tags-only / specific refspecs; plain `--all`.

## Architecture

### git verbs (`internal/git/sync.go`)

```go
// FetchAll updates tracking refs for every configured remote. Streams git's
// progress lines via onLine (network op; can be slow on a monorepo).
func (r *Repo) FetchAll(ctx context.Context, onLine func(string)) error
//   git fetch --all --no-write-fetch-head   (Stream)

// RemoteNames lists configured remote names, one per line.
func (r *Repo) RemoteNames(ctx context.Context) ([]string, error)
//   git remote

// PruneRemotes removes tracking refs for branches deleted on the named remotes,
// in one invocation. No-op (no error) when names is empty.
func (r *Repo) PruneRemotes(ctx context.Context, names ...string) error
//   git remote prune <names…>
```

`git remote prune` accepts multiple names, so pruning all remotes is one
invocation once the names are known. Both prune steps are ref-only (no object
download). Add all three to `engine.GitOps`.

### engine ops (`internal/engine/fetch.go`, `prune.go`)

```go
// Fetch updates all remotes' tracking refs. RefWrite (refs only, no worktree).
type Fetch struct{}
func (op Fetch) LockMode() repogate.Mode { return repogate.RefWrite }
func (op Fetch) Run(ctx, deps) (Result, error)
//   emit Progress{Step:"fetching"}; FetchAll(ctx, onLine→GitLine events)
//   (mirror CreateWorktree's onLine→GitLine forwarding)

// Prune drops tracking refs for upstream-deleted branches on all remotes.
type Prune struct{}
func (op Prune) LockMode() repogate.Mode { return repogate.RefWrite }
func (op Prune) Run(ctx, deps) (Result, error)
//   names := RemoteNames; if len==0 → Result{Summary:"no remotes"}, nil
//   emit Progress{Step:"pruning"}; PruneRemotes(ctx, names...)
```

Neither op takes a decision (no `Decider` fork) — failures surface as the op
error. RefWrite (not TreeWrite) because nothing in the working tree changes;
this matches SmartPull's background-fetch reservation choice.

### domain

No new query. Ops run via `Execute`; the existing snapshot reload after an op
refreshes `RemoteBranches` and the Branches `(↓N)` counts. (The standalone
`svc.RemoteBranches` from chunk 3 already backs `gg remote ls`.)

### TUI (`internal/tui/`)

- **`f`** on the Remotes tab (`m.focus == panelRemotes`, ops idle) starts
  `engine.Fetch{}` via `m.startOp`. Footer `scopeRow`/`scopeWindow` entry
  (`{"fetch","f","[f]etch",…}`) gated on `m.focus == panelRemotes && m.opsIdle()`,
  plus a help line. `f` is free outside the diff view.
- **Prune** is a `.`-action-menu row scoped to the Remotes tab, with a direct
  `run` handler that starts `engine.Prune{}` (the menu supports `run` handlers —
  action_menu.go). No dedicated key (occasional cleanup).
- A successful op reloads the snapshot, so the Remotes list and behind-counts
  update automatically.

### CLI (`internal/cli/remote.go`)

Extend `cmdRemote`'s subcommand switch (next to `ls`):

```go
case "fetch":
    return runOperation(ctx, svc, engine.Fetch{}, cliDecider{}, stderr) → finish
case "prune":
    return runOperation(ctx, svc, engine.Prune{}, cliDecider{}, stderr) → finish
```

(`gg remote ls | fetch | prune`.) Unknown subcommand still errors.

### e2e — `stdout_excludes` + two scenarios

Add a symmetric assertion to the `[[run]]` step (mirrors `stdout_contains` from
chunk 3):

```go
// scenario.go
StdoutExcludes []string `toml:"stdout_excludes"`
func (r Run) PresentExcluded(out string) []string // substrings that must be ABSENT but appear
// harness_test.go run loop: fail if any PresentExcluded(stdout) is non-empty
```

Scenarios (`e2e/scenarios/`, numbered after the current max):

- **fetch**: origin's `foo` advances *after* the clone (`[input.origin].after`
  adds a commit to `foo`); `gg remote fetch`; then `gg checkout origin/foo -s`
  lands on the new commit — `[[expect.log]] subjects=[<new>, …]` proves the
  tracking ref moved.
- **prune**: origin **deletes** `foo` after the clone (`after = [{ "git" … }]`
  or a delete step — see Risks); `gg remote prune`; `gg remote ls` with
  `stdout_excludes=["origin/foo"]` (and `stdout_contains=["origin/main"]`) proves
  the stale ref is gone while live refs remain.

### Docs

`internal/agentskill/using-gg.md` (+ `Version` bump, dogfood `SKILL.md`
refresh), `README.md` CLI verbs, `CHANGELOG.md`. Update the Remotes-tab help
copy to mention `f` fetch + the Prune menu action.

## Testing (TDD)

- **git verbs** (real-git): `FetchAll` updates a behind tracking ref to the
  origin tip; `RemoteNames` returns `origin`; `PruneRemotes` removes a tracking
  ref whose upstream branch was deleted, leaves live ones.
- **engine**: `Fetch{}` against a real origin+clone moves `refs/remotes/origin/foo`;
  `Prune{}` removes a deleted upstream branch's ref; `Prune{}` on a repo with no
  remotes returns a clean no-op.
- **harness**: `Run.PresentExcluded` unit test (twin of `MissingStdout`);
  `LoadScenario` parses `stdout_excludes`.
- **cli**: `gg remote fetch` / `gg remote prune` exit 0 and have the right effect
  on a real clone; `gg remote bogus` still errors.
- **tui**: `f` on `panelRemotes` starts `Fetch` (running=true), not elsewhere;
  the Prune menu row appears on the Remotes tab and its `run` starts `Prune`.
- **e2e**: the two scenarios above.

## Risks / watch-items

- **Deleting an origin branch in a scenario**: the builder's step vocabulary is
  write/rm/commit/branch/switch/stash/worktree — no "delete branch" step. The
  prune scenario needs origin to drop `foo` after the clone. Options: extend the
  builder with a `{ "branch-delete" = "foo" }` step, OR add a generic
  `{ git = ["branch","-D","foo"] }` escape hatch. Pick the smallest that fits the
  schema; confirm against `e2e/builder.go` `runSteps` at plan time. (A generic
  `git` step is the more reusable; a targeted delete step is narrower.)
- **`git remote prune` needs the remote reachable**: scenarios use the
  http-backend server (already running for the clone), so prune can query it.
- **`FetchAll` streaming**: forward `onLine` to `GitLine` events like
  `CreateWorktree`; keep it non-fatal if a line is empty.
- **`f` key scope**: ensure the Remotes-tab `f` doesn't shadow anything when the
  Shelf tab or another panel is focused — gate strictly on `panelRemotes`.
- **agentskill dogfood test**: bump `Version` and run `gg init --update`, else
  `TestDogfoodSkillCopyInSync` fails (as in chunk 3).
- **Test-file naming**: avoid `_GOOS`/`_GOARCH` tokens before `_test.go`.

## Slicing for the plan

1. git verbs (`FetchAll`, `RemoteNames`, `PruneRemotes`) + `GitOps` + tests.
2. `Fetch` and `Prune` engine ops + tests.
3. e2e `stdout_excludes` harness assertion + tests.
4. CLI `gg remote fetch` / `gg remote prune` + cli tests + the two e2e scenarios
   (+ the builder delete-step from Risks).
5. TUI `f` fetch + Prune menu action + footer/help.
6. agentskill + README + CHANGELOG; full race gate.
