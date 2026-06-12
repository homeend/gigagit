# Mark-and-Pair Operations + SmartMerge — Design Spec

**Date:** 2026-06-12
**Status:** Approved (design agreed in chat; this document records it)
**Scope:** A generic TUI "mark a line, pair it with another, pick an operation"
mechanism (`m` key + pair-op popup), with the Branches panel as the first
consumer, plus a real `engine.SmartMerge` operation (merge one branch into
another) wired through the TUI popup and a new `gg merge` CLI command.
Rebase appears in the popup but is **not implemented** (disabled entry).

## Goal

Two-argument git operations need two lines selected. The user marks one row
with `m`, moves the cursor to another row, presses `m` again, and a popup
offers the panel's pair-operations with fully-spelled direction labels
("Merge feat/x into main"). The mechanism is panel-generic — each panel
*registers* its pair-ops — but only Branches registers any in this feature:
**merge** (working, backed by SmartMerge) and **rebase** (listed, disabled).

## 1. Mark mechanism (TUI, generic)

### State

One mark for the whole Model (not per panel):

```go
// markState identifies a marked row by stable identity, not index, so it
// survives reloads, re-sorts, and filtering.
type markState struct {
    panel   panel
    key     string // panelList.Key of the marked backing element
    display string // short human label for the status bar / popup title
}
```

Stored as a plain value field `mark *markState` on Model (pointer so the
zero state is "no mark"; the pointer is replaced, never mutated, preserving
value-receiver semantics).

`panelList` gains a fourth method:

```go
Key(i int) string // stable identity of backing element i
```

| Panel     | Key            |
|-----------|----------------|
| Branches  | branch name    |
| Worktrees | worktree path  |
| Status    | file path      |
| Commits   | commit sha     |

Resolution: `markIndex(p panel) (int, bool)` scans the panel's backing list
for the stored key. If the row no longer exists (e.g. the branch was
deleted), the mark is treated as dead — cleared lazily on next use, and the
glyph simply doesn't render.

### Keys (normal-mode, after the existing popup/filter routing)

| Key | Condition | Effect |
|-----|-----------|--------|
| `m` | no mark | mark the selected row of the focused panel |
| `m` | mark set, cursor ON the marked row (same panel) | unmark (toggle) |
| `m` | mark set in a DIFFERENT panel | mark moves to the focused panel's selected row (replace) |
| `m` | mark set, cursor on a different row, same panel, panel HAS pair-ops | open the pair-op popup |
| `m` | mark set, cursor on a different row, same panel, panel has NO pair-ops | status message: "no pair operations for this panel" |
| `esc` | mark set | clear the mark (takes precedence over filter-clear; a second `esc` then clears the filter) |

`m` is gated on `!m.running && !m.loading` like every other action key.
Marking requires a valid `backingIndex` (empty panel → inert).

### Rendering

- The marked row gets a `◆ ` prefix (rendered by the row pipeline when the
  display row's backing index equals `markIndex`; falls back to nothing when
  the mark is dead or in another panel's hidden view).
- The status/help line shows `marked: <display>` while a mark is set.

## 2. Pair-op popup (TUI, generic window)

A new popup type following the existing pointer-field pattern:

```go
type pairOpPopup struct {
    marked, selected string   // display names (for Branches: branch names)
    ops              []pairOp // the focused panel's registered ops
    sel              int
}

type pairOp struct {
    label   func(marked, selected string) string // "Merge feat/x into main"
    build   func(marked, selected string) engine.Operation // nil when disabled
    enabled bool
    note    string // suffix for disabled entries, e.g. "not implemented yet"
}
```

- Registered per panel in a function `pairOpsFor(p panel) []pairOp`.
  Branches returns two entries:
  - **Merge**: label `"Merge <marked> into <selected>"`, enabled,
    builds `engine.SmartMerge{Source: marked, Target: selected}`.
  - **Rebase**: label `"Rebase <selected> onto <marked>"`, disabled,
    note `"not implemented yet"`. Selecting it sets the status message and
    keeps the popup open.
  All other panels return nil.
- Routing precedence: modal → worktree popup → repo popup → settings →
  branch popup → **pair-op popup** → filterTyping → normal keys.
- Keys inside the popup: `up/k`/`down/j` move, `enter` runs the highlighted
  enabled op (close popup, **clear the mark**, `startOp`), `esc` closes the
  popup and keeps the mark (the user may want a different second row).
- Title: `"<marked> + <selected>"`.

The direction is encoded in each label — marked-vs-selected never carries
implicit meaning; every entry spells out which argument plays which role.

## 3. `engine.SmartMerge`

```go
// SmartMerge merges Source into Target (default: current branch).
type SmartMerge struct {
    Source string
    Target string
}
```

### Guards (fail fast — plain errors before any event or mutation)

1. `Source == ""` → error.
2. `Target` defaults to the current branch; **detached HEAD with defaulted
   Target** → error ("detached HEAD: specify a target branch"). An explicit
   Target works from detached HEAD.
3. `Source == Target` → error.
4. Source or Target not in the local branch list (one `Branches()` call) →
   error `no such branch: X`. Pre-checking forecloses git's remote-DWIM.

### Decision ladder (simplest correct path first, mirroring SmartPull)

1. **Target == current branch** → `git merge <source>` in place.
2. **Target checked out in another worktree** (`WorktreeForBranch`) →
   merge THERE (`MergeInWorktree`); the user stays where they are.
   Summary: `merged <source> into <target> in worktree <path>`.
3. **Otherwise** → autostash if dirty (`gg-autostash:<target>` message,
   like checkoutPull), `Switch` to Target, merge, **stay on Target**
   (consistent with PullAndStay), then `StashPop` with the existing
   `stash-pop-conflict` decision (`["keep","abort"]`) on pop failure.
   If the switch fails, pop the stash back and abort.

### Merge conflict (any rung)

When `git merge` fails with conflicts, decide:

```
ID:      "merge-conflict"
Prompt:  "Merging <source> into <target> hit conflicts"
Options: ["keep-conflicts", "abort"]
```

- `keep-conflicts` → leave the conflicted tree for manual resolution; the
  op returns `Result{Summary: "...conflicts left in tree"}` **and an error**
  (CLI exit 1, `in_progress = "merge"`) — the stash-pop-conflict shape.
- `abort` → `git merge --abort`, exit 0, summary
  `aborted: merging <source> into <target>`. (On rung 3 the user remains on
  Target after an abort; switching back is out of scope — the summary names
  the branch they're on.)

Conflict detection: a non-zero `git merge` with `MERGE_HEAD` present (or
equivalently conflict markers in the porcelain status). A non-conflict merge
failure (e.g. unrelated histories refusal) is a plain error, no decision.
In rung 2 (worktree), `keep-conflicts` leaves the conflicts in THAT worktree;
the summary names the worktree path.

Decision IDs and option lists are cross-frontend API: `merge-conflict` =
`["keep-conflicts","abort"]`, reused exactly by TUI modal, CLI policy, and
the future MCP MapDecider.

### New git verbs (`internal/git`, one invocation each)

```go
func (r *Repo) Merge(ctx context.Context, branch string, onLine func(string)) error
func (r *Repo) MergeAbort(ctx context.Context) error
func (r *Repo) MergeInWorktree(ctx context.Context, path, branch string, onLine func(string)) error // git -C <path> merge <branch>
func (r *Repo) MergeInProgress(ctx context.Context) (bool, error)            // MERGE_HEAD existence via rev-parse
```

(Exact signatures may follow the existing verb style — streaming via
`onLine` where SmartPull's verbs stream; `MergeInProgress` may reuse an
existing in-progress helper if one exists.)

Default git fast-forward behavior (ff when possible); no `--no-ff`/strategy
knobs (YAGNI).

## 4. CLI: `gg merge`

```
gg merge [--into <target>] [--on-conflict=keep|abort] <source>
```

- Flags before positionals (project convention).
- `--into` defaults to the current branch.
- `--on-conflict=keep` pre-answers `merge-conflict` with `keep-conflicts`;
  `--on-conflict=abort` with `abort`. Any other value → usage error, exit 2.
- Unanswered `merge-conflict` on a non-TTY → exit 1 with the options listed
  on stderr (existing cliDecider convention).
- Exit codes: success or chosen-abort = 0; guard failure, merge failure, or
  kept conflicts = 1; usage error = 2.

## 5. Docs, skills, e2e

- **e2e scenarios** (`e2e/scenarios/`): ff merge; true merge commit;
  conflict + `--on-conflict=abort` (exit 0, tree untouched); conflict +
  `--on-conflict=keep` (exit 1, `in_progress="merge"`, conflicted files);
  conflict unanswered non-TTY (exit 1, options on stderr; NOTE the decision
  fires only after the conflict already exists, so the unanswered-decision
  error leaves the conflicts in place — assert `in_progress="merge"`, not a
  clean tree); merge into a
  branch checked out in a linked worktree (merge lands THERE, you stay);
  guards (same branch, missing branch) exit 1.
  **Contract-table row** added to
  `.claude/skills/writing-e2e-scenarios/SKILL.md`:
  `merge [--into t] [--on-conflict=…] <s>` — merges s into t (default
  current); ends on t only when t wasn't checked out anywhere; conflict
  fork `merge-conflict` keep/abort; unanswered → exit 1 with merge in
  progress.
- **Agent skill**: `internal/agentskill/using-gg.md` gains the `gg merge`
  bullet; `agentskill.Version` 4 → **5**; `gg init --update`; commit the
  regenerated dogfood copy (drift-guard test enforces byte-identity).
- **adding-tui-windows skill**: add the pair-op popup + mark mechanism as a
  documented pattern (mark state, Key identity, registry) if the taxonomy
  section needs it.
- README (TUI keys table: `m`, `esc`; CLI: `gg merge`), CHANGELOG.

## Files touched (expected)

| File | Change |
|------|--------|
| `internal/tui/viewstate.go` | `Key(i)` on `panelList` + four impls; `markIndex`. |
| `internal/tui/mark.go` (new) | `markState`, mark key handling helpers, `pairOpsFor`. |
| `internal/tui/pairop_popup.go` (new) | `pairOpPopup`, key handling, rendering. |
| `internal/tui/model.go` | `mark` field, `pairPopup` field, `m`/`esc` key cases, routing entry. |
| `internal/tui/view.go` | `◆ ` prefix on the marked row; status-line `marked:` hint. |
| `internal/engine/smart_merge.go` (new) | The operation. |
| `internal/git/sync.go` (or merge.go, new) | `Merge`, `MergeAbort`, `MergeInWorktree`, `MergeInProgress`. |
| `internal/cli/merge.go` (new) | `gg merge` command. |
| `internal/cli/cli.go` (router) | route `merge`. |
| `e2e/scenarios/s26+_merge_*.toml` | Scenarios above. |
| `internal/agentskill/{using-gg.md, agentskill.go}` | Bullet + Version 5. |
| `.claude/skills/{using-gg,writing-e2e-scenarios,adding-tui-windows}/SKILL.md` | Regenerate / contract row / pattern. |
| `README.md`, `CHANGELOG.md` | Docs. |

## Testing

- **engine:** FakeRunner argv assertions for each rung; real-git repo tests
  for guards, in-place merge, worktree merge, switch-merge-stay with
  autostash, conflict keep/abort paths (build a real conflict).
- **git verbs:** argv via FakeRunner; `MergeInProgress` against a real
  conflicted repo.
- **tui:** mark toggle/move/replace semantics; mark survives reload and
  re-sort (identity, not index); dead-mark rendering; `esc` precedence
  (mark before filter); popup routing precedence; popup keys incl. disabled
  rebase entry; enter dispatches SmartMerge with the right Source/Target
  and clears the mark; `m` on a pair-op-less panel.
- **cli:** policy mapping for `--on-conflict`; usage errors; non-TTY
  unanswered decision exit 1.
- **e2e:** the scenarios in §5.

## Out of scope (YAGNI)

- Rebase implementation (popup entry is disabled).
- Pair-ops for Worktrees/Status/Commits panels (registry returns nil).
- Multi-mark (>1 marked row), cross-panel pairs.
- `--no-ff` / merge strategies / commit-message editing.
- Switching back after rung-3 merges; conflict resolution UI (M3).
