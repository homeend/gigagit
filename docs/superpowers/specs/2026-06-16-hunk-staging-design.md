# Hunk/line staging — design

**Date:** 2026-06-16
**Status:** approved (brainstorm), pending spec review
**Sub-project of:** GitKraken-style hunk/line selection. This is sub-project 2
of 2; it reuses the pure core `internal/hunkpick` and the picker surface built
by sub-project 1 (the conflict hunk picker, merged 2026-06-16).

## Goal

Stage a modified file at the **region and line level** in the TUI, GitKraken-
style: for each changed hunk take the whole **working-tree** side (stage it),
keep the whole **index** side (leave it unstaged), or assemble the staged
content **line by line** from either side — with picked lines landing in the
result in the order toggled. Whole-file `space` staging stays as the fast path;
this is what you get when you press **`H`** to drill into a file's hunks.

## Entry, scope, and the shared picker

- **Entry:** `H` on the **Files** panel opens the staging hunk picker for the
  selected file. `enter` keeps its current meaning (the read-only side-by-side
  diff). `space` keeps whole-file staging.
- **Scope (v1): staging only.** Unstaging hunks from the Staged panel is the
  mechanically symmetric follow-up (left = HEAD, right = index) and is
  explicitly deferred.
- **One shared picker.** The conflict resolver's `conflictPicker` surface is
  generalized into a shared `hunkPicker`; staging is its second consumer. The
  interaction (2D cursor, `←/→` side, `↑/↓` line, `space` line-pick, `c`/`i`
  whole-side, `C`/`I` take-all, `n`/`p` jump, `esc` cancel) is identical to the
  conflict resolver, so the two features operate the same way.

## Architecture

### 1. Generalize the picker surface → `hunkPicker`

Rename `internal/tui/conflict_picker.go`'s `conflictPicker` to `hunkPicker` and
inject what differs between the two consumers:

```go
type hunkPicker struct {
    title      string        // "Resolve conflicts: f" / "Stage hunks: f"
    leftLabel  string        // "current" / "index"
    rightLabel string        // "incoming" / "working tree"
    requireAll bool          // conflicts: true (gate enter on Pending==0); staging: false
    apply      func(m Model, content []byte) (Model, tea.Cmd)
    // unchanged: doc *hunkpick.Doc, blocks, bi, side, line (the 2D cursor)
}
```

- `leftLabel`/`rightLabel` drive the badges (`✓ <label>`) and the footer hint
  (`[c] <leftLabel>  [i] <rightLabel>`). The keys stay `c`/`i`/`C`/`I`/`space`
  regardless of labels.
- `requireAll` replaces the hard-coded conflict gate: conflicts require every
  region decided; staging does not (an untouched hunk just stays unstaged).
- `apply(content)` is the `enter` action when the gate passes:
  - **conflicts:** `m.popSurface()` + clear `conflictPopup` + `reopenConflict =
    true` + `startOp(ResolveConflictHunks{path, content})` (today's behavior).
  - **staging:** `m.popSurface()` + `startOp(StageHunks{path, content})` + a
    status refresh so the Files/Staged panels update.

`newConflictPicker(path, doc)` becomes a thin constructor that wires the
conflict params (`requireAll=true`, current/incoming labels, the conflict
apply). A new `newStagePicker(path, doc, apply)` wires the staging params. The
shared `update`/`render`/`cell` logic and all key handling are unchanged. The
existing conflict tests keep working; only their `*conflictPicker` type
assertions become `*hunkPicker`.

### 2. `hunkpick.FromDiff(left, right []string) *Doc`

A second `Doc` constructor (alongside `ParseConflict`) that builds the
region/literal split from a line diff instead of conflict markers:

- Run `textdiff.Compare` over `left` and `right`.
- Equal runs → `Item{Literal}`; changed runs → `Item{Block{Current:left-lines,
  Incoming:right-lines}}`.
- `FinalNewline` is taken from the working-tree (right) side.

`hunkpick` already has no git/TUI imports; it may import `textdiff` (also pure,
no reverse dependency), keeping the package dependency-free in spirit. The
staging entry calls `Doc.SetAll(TakeCurrent)` after `FromDiff` so the **default
is "nothing staged"** — the assembled result equals the current index, making
`enter` with no edits a safe no-op. Picking the working side (`i`/`space`) on a
hunk stages it.

### 3. Index-set op (working tree untouched)

The assembled result is the desired **index** content for the file. Setting the
index blob without touching the working tree needs no patch construction:

1. Read the file's current index mode (`git ls-files -s -- <path>` → the mode
   field, e.g. `100644`/`100755`).
2. `git hash-object -w --path=<path> --stdin` the assembled bytes → blob sha
   (`--path` so any clean filters / attributes apply as git would).
3. `git update-index --cacheinfo <mode>,<sha>,<path>` → the index entry now
   holds that blob; the working tree is unchanged.

Wrapped as a new verb chain on `*git.Repo` (`internal/git/stage_blob.go`):
`StageBlob(ctx, path string, content []byte) error` doing the three steps.
Engine op `internal/engine/stage_hunks.go`:

```go
type StageHunks struct { Path string; Content []byte }
```

`Run` calls `deps.Repo.StageBlob(...)` and emits `Progress`/`Done` under the
default exclusive (TreeWrite) reservation. `StageBlob` is added to the `GitOps`
interface (the op's only new dependency).

### 4. Reading the two sides

Both sides are already available via domain reads (the read-only Files diff uses
exactly these):

- **left = index:** `svc.ShowFile(ctx, "", path)` (`git show :path`).
- **right = working tree:** the worktree-file read added in sub-project 1. It is
  currently named `domain.ConflictedFile`; **rename it to `domain.WorktreeFile`**
  (one caller — the conflict-picker load — to update) since it is now general.

A `loadStageHunksCmd(path)` reads both off the UI thread, builds the `Doc` via
`hunkpick.FromDiff`, `SetAll(TakeCurrent)`, and pushes the staging `hunkPicker`.

## Wiring & guards

`H` on the Files panel dispatches `loadStageHunksCmd` only for a **tracked,
modified, non-binary** file. Otherwise a status message and no surface:

- **untracked** → "stage hunks: use space to add a new file" (untracked partial
  staging is deferred — left side has no index blob).
- **binary** (`textdiff.IsBinary` on either side) → "stage hunks: binary file".
- **no changes / no regions** → "stage hunks: nothing to stage".
- **conflicted** → skip (resolve via `x` first).

After `StageHunks` applies, a status refresh updates the Files and Staged panels
(the file may become partially or fully staged). `help.go` gains an `H` row on
the Files panel and the picker help section is generalized (it now serves both
the conflict resolver and staging). The footer advertises `H` on the Files panel
per the drift-guard convention.

## Edge cases

- **Mode preservation:** the index mode comes from `ls-files -s`; executables
  keep `100755`.
- **Partially-staged files:** the baseline left side is the *current index*
  (not HEAD), so staging more hunks composes correctly with what's already
  staged.
- **Working tree never changes:** `update-index --cacheinfo` only rewrites the
  index entry; verified by tests asserting the on-disk file is byte-identical
  after staging.
- **CRLF:** lines split on `\n`, byte-faithful round-trip; CRLF normalization is
  a noted watch-item, not v1 scope.

## Testing

- `hunkpick.FromDiff` pure tests: equal/changed runs → literal/block split,
  final-newline carry, an all-add (empty left) case.
- `git.StageBlob` against real git in a `t.TempDir()`: stage assembled content →
  `git diff --cached` shows the staged delta, `git diff` shows the remainder,
  the working-tree file is unchanged on disk, mode preserved.
- `engine.StageHunks` op test (real repo): partial stage of a modified file
  leaves the file partially staged with the working tree intact.
- Shared-picker surface tests with staging params (labels, no-gate `enter`
  applies, default-TakeCurrent no-op), plus the existing conflict tests still
  green after the rename.
- Wiring test: `H` on a tracked modified file pushes the staging `hunkPicker`;
  untracked/binary are no-ops with a status message.
- No e2e scenario — the picker is TUI-only and `internal/tui` cannot import
  `internal/git` (archtest), so the cross-layer flow has no single automated
  test; an `engine`-level round-trip test (`FromDiff` → `StageHunks` on a real
  repo) plus a manual TUI eyeball cover it, mirroring the conflict picker.

## Out of scope (v1)

- Unstaging hunks from the Staged panel (symmetric follow-up: left = HEAD,
  right = index).
- Untracked-file partial staging (whole-file `space` add stays).
- CRLF normalization; CLI/MCP hunk staging (whole-file staging stays scriptable).
