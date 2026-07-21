# Branch versions (safe branches) — design

Date: 2026-07-21
Status: approved by user (brainstorm 2026-07-21); ready for planning

## Summary

Before any operation that rewrites or replaces a branch's history — and before a
merge into it — gg records the branch's current tip as a hidden, gg-owned git
ref. The user can later list a branch's previous versions, whole-tree-compare
any version against the branch's current state, and restore the branch to a
version. Always-on for every branch; time-based retention.

This replaces the original "shelve the branch into the shelf" idea: a hidden
ref preserves the branch's full commit history (messages, authors, shas) at
zero storage cost, pins the objects against `git gc`, and makes both compare
and restore exact. The shelf stays uninvolved.

## Prior art (validation)

- **GitButler** snapshots the whole project state into the git object database
  before every data-changing operation (operations history / `but oplog restore`).
- **Jujutsu** has the operation log (`jj op log`, `jj undo`, `jj op restore`)
  and `jj evolog` — the per-change evolution log, the closest analog to
  "previous versions of this branch".
- **git-branchless** has `git undo` backed by an operation log.
- Mainstream GUI clients (GitKraken, Fork, Tower, Sublime Merge) have nothing
  comparable — they lean on the reflog. Git's own reflog covers the need only
  partially: it expires (~90 days), dies with branch deletion, and is
  expert-hostile to browse.

## Decisions (from brainstorm Q&A)

| Question | Decision |
|---|---|
| Main job | Compare/audit AND restore, equally |
| Granularity | Full commit history (hidden ref to old tip), not a squashed snapshot |
| Opt-in | Always-on for all branches; no per-branch toggle |
| Triggers | History-changing ops + merge (+ DeleteBranch, see below) |
| Retention | Time-based prune (configurable age), lazy |

## Storage format

One version = one ref:

```
refs/gg/versions/<branch>/<unix-ts>-<op>   →  <old tip sha>
```

- `<branch>` is the branch's full name (may itself contain `/`). Parsing is
  from the END: the last path segment is always `<unix-ts>-<op>`; everything
  between `refs/gg/versions/` and it is the branch name. gg always appends
  exactly one such segment, so this is unambiguous even for a branch whose own
  last segment looks like `123-fix`.
- `<op>` is the English engine op name that caused the snapshot (protocol
  value, e.g. `rebase`, `merge`, `squash`, `delete-branch`) — rendered
  translated in the TUI via the existing `opDisplayName` seam.
- If the ref already exists (same branch + second + op), append `-2`, `-3`, …
  to the segment until free.
- The ref name IS the metadata (branch, when, why); the target sha carries the
  rest (subject, author, full ancestry). **No sidecar store.**
- `refs/gg/*` is outside `refs/heads`/`refs/tags`/`refs/remotes`: never pushed
  or fetched by normal git operations, shared by all worktrees of the repo
  (common dir), and pins its objects against gc — which is the point.

## Configuration

Section `[versions]` (new; both keys get `settingDocs` entries so
`gg config init`/`populate` emit them):

| Key | Type | Default | Meaning |
|---|---|---|---|
| `disabled` | bool | `false` | Kill-switch. **Inverted polarity on purpose** — the field-overlay rule treats a zero value as unset, so a plain `enabled=false` at the repo layer could never override a global `true`. Same precedent as `[refresh] disable_remote_tags_auto` and `[ui] disable_slow_op_confirm`. |
| `max_age_days` | int | `90` | Prune snapshots older than this. `-1` = keep forever. `0` means unset (falls back through overlay to 90) per the zero-is-unset rule, so the "forever" sentinel must be `-1`, not `0`. |

Policy reaches the engine the way `SetShowEOLOnlyChanges` does:
`Service.SetVersionsPolicy(enabled bool, maxAgeDays int)` stored on the domain
`Service`, passed into `OpDeps` by `Execute`.

Two new non-destructive line-edit writers (the `SetCommitSort` precedent, both
writing the ACTIVE repo `.gg.toml`): `SetVersionsMaxAgeDays(path, days)` and
`SetVersionsDisabled(path, disabled)` — backing the Settings "Operations
history" editor below.

## Engine

### New git verbs (`internal/git`)

- `UpdateRef(ctx, ref, sha)` — `git update-ref <ref> <sha>`.
- `DeleteRef(ctx, ref)` — `git update-ref -d <ref>`.
- `ForEachRef(ctx, pattern)` — `git for-each-ref --format=%(refname)%x1f%(objectname)%x1f%(subject) <pattern>`
  (one invocation; subject read in the same pass so listing needs no per-ref
  `log` calls).

All three added to the `GitOps` interface (`*git.Repo` already satisfies the
rest).

### Snapshot helper

`snapshotBranchTip(ctx, deps, branch, opName)` in `internal/engine`:

1. No-op when policy-disabled, when `branch` is empty (detached HEAD), or when
   the branch has no commits yet.
2. Resolve the branch tip, write the version ref.
3. Prune: `ForEachRef` over `refs/gg/versions/<branch>/*`, parse timestamps
   from ref names, `DeleteRef` any older than `max_age_days` (skip when `-1`).
4. **Best-effort**: any failure emits a `GitLine` note ("could not record
   branch version: …") and returns nil — a snapshot failure must never block
   the real operation.

Timestamps come from `OpDeps`' clock/now source consistent with existing ops
(plain `time.Now()` at the call site is acceptable; ops are not replayed).

### Trigger ops (call the helper before mutating)

| Op | Snapshot of | Notes |
|---|---|---|
| `SmartMerge` | the branch being merged INTO (current branch) | the user's explicit case; old tip stays an ancestor but the labeled version makes it findable |
| `SmartRebase` | the rebased branch | |
| `SmartPull` | current branch, only on its rewriting lanes (rebase, reset-to-remote, merge-commit lane); NOT on fast-forward | decided per-path inside the op |
| Squash (rebaseplan) | the rewritten branch | |
| Commit move/drop (rebaseplan) | the rewritten branch | |
| `Commit` with amend | current branch | plain commit does NOT snapshot |
| `UndoLastCommit` | current branch | |
| Reset-to-remote-tip | the reset branch | |
| `DeleteBranch` | the deleted branch | addition beyond the brainstorm list, approved: deletion is the ultimate history loss and this yields "restore deleted branch" for free |
| `RestoreBranchVersion` (new, below) | the branch being restored | restore is itself undoable (GitButler's restore-from-snapshot pattern) |

Non-triggers (old tip remains reachable as an ancestor): plain `Commit`,
fast-forward pull, `CherryPick`, `Push`, `Stash`, checkout/switch, worktree ops.

### Restore op

`RestoreBranchVersion{Branch, Ref}` (new engine op, default `TreeWrite`):

1. `snapshotBranchTip` first (op name `restore`).
2. Cases, mirroring the existing reset-to-remote-tip logic:
   - `Branch` is the current branch → `git reset --hard <sha>`; a dirty
     working tree forks a keep/discard/cancel decision first.
   - `Branch` is checked out in ANOTHER worktree → typed refusal (git itself
     refuses to move such a ref; surface a clear error naming the worktree).
   - Otherwise → `git branch -f <branch> <sha>`.
3. The TUI restore flow additionally offers "create a new branch at this
   version" as a non-destructive alternative; that lane is just the existing
   `CreateBranch` op pointed at the version sha — no new engine code.

## Domain

- `BranchVersions(ctx, branch) ([]model.BranchVersion, error)` — one
  `ForEachRef` under the branch's namespace; parse ref names; newest first.
- `AllVersionBranches(ctx) ([]model.VersionedBranch, error)` — group
  `refs/gg/versions/*` by branch; mark branches that no longer exist
  (deleted-branch recovery entry point).
- Deleting one snapshot goes through a tiny engine op
  `DeleteBranchVersion{Ref}` (`LockMode()` = `RefWrite`; refuses refs outside
  `refs/gg/versions/`) dispatched via `Execute` like every other mutation.
- `model.BranchVersion{Ref, Sha, Subject, Time time.Time, Op string}`;
  `model.VersionedBranch{Branch string, Deleted bool, Count int, Latest time.Time}`.
- Reads run under the usual Read reservation, singleflight-coalesced like the
  other queries.

## TUI

- **Branches panel `.` menu**: new row "Previous versions…" (self-gating:
  hidden when the branch has none). Opens `branchVersionsPopup` (repoPopup
  pattern, `popupMax`-embedding): rows
  `2026-07-21 14:03 · rebase · a1b2c3d <subject>`.
  - `enter` — whole-tree compare version vs current tip via the existing
    compare-trees view; both endpoints resolved to tip HASHES before opening
    (the mutable-ref-names-poison-the-diff-LRU gotcha).
  - `r` — restore: decision modal `reset branch` / `new branch at version` /
    `cancel` (esc → cancel; option list includes an explicit abort option per
    the apply-patch convention). Reset lane dispatches `RestoreBranchVersion`;
    new-branch lane opens the existing create-branch popup pre-filled with the
    version sha as the start point.
  - `d` — delete this snapshot (confirm first).
  - `y` — copy the version sha (existing clipboard path).
- **Command palette**: "Branch versions…" — branch picker over
  `AllVersionBranches` (deleted branches marked, e.g. dim `(deleted)`), then
  the same popup. This is the recovery path for deleted branches.
- **Settings (`,`) menu**: new row "Operations history" opening a small popup
  with the recording machinery's config:
  - "Retention" — shows the effective value (`90 days` / `keep forever`);
    `enter` opens a numeric textfield (the `saveRefreshInterval` flow), `-1` =
    keep forever, saved via `SetVersionsMaxAgeDays` to the active repo
    `.gg.toml`, then the in-memory config and `Service` policy are updated.
  - "Recording" — on/off toggle saved via `SetVersionsDisabled` (writes the
    inverted `disabled` key).
  Esc closes back to Settings. Labels/hints through `i18n.T` like every other
  Settings row.
- `opAffectedSources`: `RestoreBranchVersion` → `{status, branches, feed,
  worktrees}` (a reset of the current branch changes the working tree).
- i18n: every new label/status/option through `i18n.T` with keys in all four
  bundles; decision option values stay English protocol; the op name in rows
  renders via `opDisplayName`.
- Footer/help: advertise the new `.`-menu row where the Branches panel help
  lists menu-worthy actions (advertise-features convention).

## CLI

- `gg versions [<branch>]` — list versions of a branch (default: current
  branch): `<ts-op-id>  <short-sha>  <ISO time>  <op>  <subject>`, terse,
  agent-friendly. With no versions: exit 0, "(no versions)".
- `gg versions restore <branch> <ts-op-id>` — dispatches
  `RestoreBranchVersion`; the dirty-tree decision maps to the standard
  stdin/flag decider (non-interactive default: abort).
- Compare needs no new CLI: listed shas feed the existing `gg diff <sha>..<branch>`.
- Routed through `runOne` so `gg batch` drives both.
- `internal/agentskill/using-gg.md` gains a section; bump `agentskill.Version`;
  `gg init --update` refresh per convention.

## Known gotchas (handle in implementation)

1. **Feed decoration pollution**: after a merge the old tip is still in the
   feed, so `%D` would show `refs/gg/versions/...`. Add
   `--decorate-refs-exclude=refs/gg/*` to the CommitFeed log invocation (and
   any other `%D` consumer).
2. **Zero-is-unset overlay**: `disabled` inverted, `max_age_days` forever
   sentinel is `-1` (see Configuration).
3. **Ref-name collision** within one second: `-2` suffix loop.
4. **Best-effort snapshot**: never fail the parent op; note via `GitLine`.
5. **Checked-out-elsewhere restore**: typed refusal naming the worktree.
6. **e2e**: scenario builds a repo, rebases, asserts `gg versions` lists one
   entry with op `rebase`, restores, asserts the tip moved back.
7. **Engine op summaries**: new prose goes through the `msg.go` lockstep
   helpers (`WithSummary`/`Progressf`/`PromptReq`) with bundle keys — the
   engine-prose AST gates will fail otherwise.

## Out of scope (v1)

- MCP tools for versions (future MCP surface).
- Snapshots for operations done OUTSIDE gg (raw git rebase) — the reflog still
  covers those; a later stage could import reflog entries.
- Any shelf involvement.
- Cross-repo / workspace-group interactions.

## Testing

- **Engine (real git in t.TempDir)**: snapshot ref exists after
  rebase/merge/squash/amend/undo/reset/delete-branch; absent after plain
  commit/cherry-pick/ff-pull; prune honors `max_age_days` and `-1`; collision
  suffixing; disabled policy writes nothing; restore matrix (current branch
  clean/dirty, other-worktree refusal, plain branch).
- **Domain**: BranchVersions ordering/parsing incl. slashed branch names;
  AllVersionBranches deleted-branch marking.
- **TUI**: popup list rendering, compare dispatch resolves hashes, restore
  decision wiring; Settings "Operations history" editor writes
  `[versions] max_age_days`/`disabled` and refreshes the live policy; i18n
  gates (menu_labels, options_vocab, engine_prose) pass.
- **Config**: writer tests for `SetVersionsMaxAgeDays`/`SetVersionsDisabled`
  (create-if-missing, preserve other lines, `-1` accepted).
- **CLI/e2e**: `gg versions` + restore scenario per gotcha 6.
