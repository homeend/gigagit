# Create worktree from a commit — design

**Pipeline #6.** Create a worktree based at the selected commit, on a **new branch
whose name the user types** (no template). Per the user: "do not populate the
branch name, make the user provide it; create the branch from the commit and
create the worktree."

## Reuse + the one new behavior

`engine.CreateWorktree{StartPoint, Branch, Path}` already creates a branch at a
start-point and the worktree in one step, and the TUI `worktreePopup` already
drives it (with a template-resolved branch + path, a state machine
stInput→stAction with a stEdit free-edit branch mode). The CLI path also exists.

The new requirement is that the branch name is **user-provided, not templated**.

## The action + the open helper

- `commitCreateWorktreeRow` (`commit_scope.go`): Commits-panel `.`-menu action,
  `m.focus == panelCommits && opsIdle()`, id `commit-create-worktree`, label
  "Create worktree here". Its `run` calls `openWorktreeFromCommit(hash)`.
- `openWorktreeFromCommit(hash)` (`worktree_popup.go`): builds the popup like
  `openWorktreePopup` but with `startPoint = hash`, `fromCommit = true`, and
  **opens directly in `stEdit` with an empty `editBuf`** — so the user types the
  branch name immediately instead of seeing a templated default. The path still
  resolves from the typed branch via the existing `recompute()`.

## Enforcing "user must provide the name"

A new `fromCommit bool` on the popup. `startCreateFromPopup` refuses to launch
when `fromCommit && strings.TrimSpace(branchOverride) == ""` (`statusMsg =
"branch name required"`). Because `recompute()` sets `previewBranch ==
branchOverride` verbatim once the user confirms a name, the created branch is
exactly what they typed; an empty name can never fall through to the template.

## Display

The popup title uses `displayStart(p.startPoint)` (the helper from #5) so a 40-hex
SHA shows as 7 chars; branch start-points are unaffected. The op still receives
the full hash (`StartPoint` → `CreateWorktree`), unambiguous in a huge repo.

## Testing (TDD)

- `commitCreateWorktreeRow` present on the Commits panel; running it opens a
  `worktreePopup` with `fromCommit == true`, `startPoint ==` the full hash, and
  `state == stEdit` with an empty buffer.
- `startCreateFromPopup` with `fromCommit && branchOverride == ""` sets a
  "branch name required" `statusMsg` and launches no op (nil cmd).

The engine op + git + CLI are already covered by their own tests; the worktree is
created through the proven `worktreePopup` → `startOp` → `engine.CreateWorktree`
path.

## Out of scope / known v1 limits

- Opening in `stEdit` skips the `<user:LABEL>` field collection (stInput), so a
  path template that references `<user:…>` resolves those to empty in commit
  mode. Typical path templates derive from `<branch>`/`<repo>`/`<date>`, which
  still resolve. Acceptable v1.
- A `<parent-branch>` token in the path template resolves to the commit hash
  (the start point) — a minor cosmetic oddity, not a failure.
- "Create + switch" (`W`) still works; the empty-name guard applies to it too.
