# Stash surface — design

**Date:** 2026-06-15
**Status:** draft (brainstorm)
**Roadmap:** new feature (real `git stash`; distinct from F5 "shelve", which is a
gg-specific file-content store).

## Goal

Make stashing usable end-to-end from the TUI:

1. From the Status panel, mark files with `m` (multi-select) and press `s` to
   open a **stash-create popup** — a name field (default `WIP on <branch>`) plus
   a checklist of unstaged files — then `ctrl+s` stashes them.
2. Press `S` to open a **Stash list window** in the right column (the Commits
   panel's position) showing all stashes.
3. In that window, `l` lists the selected stash's files in the left-column file
   tree (identical behaviors to the commit-files view: diff, history `h`, blame
   `b`); `Enter` opens a **stash-action popup** (Apply / Pop / Drop).

This replaces today's blind whole-tree `S` stash (`engine.Stash{"gg stash"}`),
which errors on a clean/untracked-only tree and surfaces no result — the reason
stashing "doesn't appear to work".

## What already exists (build only the gap)

- **Verbs:** `internal/git/stash.go` has `StashPush(ctx, message)`,
  `StashPop(ctx)` (arg-less, pops `@{0}`), `StashList(ctx) []string`.
- **Op:** `engine.Stash{Message}` (`ops_basic.go`) → `StashPush`. On `GitOps`:
  `StashList`/`StashPush`/`StashPop`.
- **Keys (today):** `S` = whole-tree stash; `s` = SmartSwitch (fires only on
  `panelBranches` via `canSwitchBranch`); `m` = single mark (pair-ops on
  `panelBranches`, no-op set on Status).
- **Commit-files view** (`internal/tui/files_view.go`, `filesView
  *contentPopup`): `l` from the Commits panel renders a commit's file tree in the
  left column; keys `enter`→diff, `h`→history, `b`→blame, `/`→search,
  `l`/`esc`→close — all keyed off `m.filesHash` (a rev). Sourced by
  `git.CommitFiles(hash)` (`diff-tree -r --root --no-commit-id --name-status -M
  --first-parent -m <hash>`).
- **Popup exemplars:** `worktree_popup.go` / the F2 `commit_popup` (two-field,
  `ctrl+s` submit, key-swallowing routing).

The gap: multi-mark on Status, the stash-create popup, path/untracked support on
the push verb, apply/pop/drop verbs+ops, the stash list window, and the
stash-action popup. The file tree is **reused verbatim** by pointing
`filesHash` at the stash's resolved commit SHA.

## Key remapping (contextual)

| Key | Context | Action |
|-----|---------|--------|
| `m` | Status panel | toggle the current file into a multi-select **set** |
| `m` | Branches panel | unchanged (single mark + pair-op popup) |
| `s` | Status panel | open the **stash-create popup** |
| `s` | Branches panel | unchanged (SmartSwitch) |
| `S` | global | open the **Stash list window** (was: whole-tree stash) |

`s`/`m` are already contextual in the dispatch; the old `S` whole-tree stash is
removed in favor of the explicit popup flow.

## Component A — multi-mark in the Status panel

A new set on the Model, e.g. `fileMarks map[string]bool` keyed by file path
(stable across reloads/sorts/filtering, like the existing `markState.key`). Used
**only** when `m.focus == panelStatus`:

- `handleMarkKey` branches: on Status, toggle the selected file's path in
  `fileMarks`; elsewhere, the existing single-`mark` logic is unchanged.
- Marked rows render a marker in `statusRows()` (e.g. a leading `●`/`*`),
  independent of the single-mark highlight used on other panels.
- `fileMarks` is cleared on `reRoot` (repo change) and after a successful stash
  of those files.

This is additive (does not touch the delicate pair-op path) and is a natural
first step toward the F6 "generalize the mark primitive" work.

## Component B — stash-create popup (`s`)

New `stashPopup` (pointer field `m.stashPopup *stashPopup`), modeled on the F2
commit popup's conventions (state behind a pointer, hand-rolled input, key
swallowing, `esc` cancels):

```go
type stashFileItem struct {
	path     string
	included bool
}
type stashPopup struct {
	name  string          // default "WIP on <branch>"
	files []stashFileItem // every file with unstaged changes (incl. untracked)
	field int             // 0 = name, 1 = file list
	sel   int             // cursor in the file list
}
```

- **Candidates** = all files with unstaged content (`Unstaged != '.'`) **and**
  untracked files (`Kind == KindUntracked`). Staged-only files are excluded
  (this stashes working-tree changes).
- **Default selection:** if `fileMarks` is non-empty, pre-check exactly those
  paths; otherwise pre-check **all** candidates.
- **Keys:** opens on the file list. `tab`/`shift+tab` switch name↔list; in the
  list `↑↓`/`j`/`k` move and `space` toggles `included`; in the name field,
  runes edit the name. **`ctrl+s` stashes** the checked files; `esc` cancels
  (`m.stashPopup = nil`); `ctrl+c` quits. The handler swallows every key.
- **Guards:** no candidates → `statusMsg = "nothing to stash"`, popup does not
  open. `ctrl+s` with zero files checked → inline hint "select at least one
  file", do not close. Empty/whitespace name → fall back to `WIP on <branch>`.
- **Fast path:** `s` → `ctrl+s` stashes all candidates under the default name.

On submit: `m.stashPopup = nil` → `m.startOp(engine.Stash{Message: name, Paths:
checkedPaths, IncludeUntracked: anyUntrackedChecked})`. Standard full reload
(stash empties the working tree for those paths *and* the stash list changes).
On success, clear the stashed paths from `fileMarks`.

## Component C — git verbs + engine ops

`internal/git/stash.go`:

- Extend push:
  `StashPush(ctx, message string, paths []string, includeUntracked bool) error`
  → `gitcmd.New("stash").Arg("push").Arg("-m", message).ArgIf(includeUntracked,
  "-u")` then, when `len(paths) > 0`, `.Arg("--").Arg(paths...)`. `--` guards
  flag-like paths.
- Generalize pop: `StashPop(ctx, ref string) error` →
  `git stash pop [<ref>]` (empty ref = `@{0}`, preserving current behavior).
- Add `StashApply(ctx, ref string) error` → `git stash apply <ref>`.
- Add `StashDrop(ctx, ref string) error` → `git stash drop <ref>`.

`engine.GitOps`: update the three signatures / add the two new verbs.

`internal/engine/ops_basic.go`:
- `engine.Stash` gains `Paths []string`, `IncludeUntracked bool`; `Run` passes
  them through. `Result.Summary` = `stashed <n> file(s): <name>`.
- New `engine.StashApply{Ref}`, `engine.StashPop{Ref}`, `engine.StashDrop{Ref}`
  ops (TreeWrite reservation; emit a `Progress`, return). Summaries:
  `applied <ref>` / `popped <ref>` / `dropped <ref>`.

## Component D — domain reads

`internal/domain`:
- `StashList(ctx) ([]model.StashEntry, error)` — a gated read (Read reservation,
  singleflight) over `git.StashList`, parsing each
  `stash@{N}: WIP on <branch>: <sha> <subject>` line into
  `model.StashEntry{Ref, Subject}` (`Ref = "stash@{N}"`, `Subject` = the text
  after the first `: `). Parser is a pure function with table tests.
- `StashCommit(ctx, ref) (string, error)` — `git rev-parse <ref>` to resolve a
  stash entry to its commit SHA for the file tree / diff. (A thin verb +
  gated query, mirroring `CurrentBranch`.)

New model type `model.StashEntry{Ref, Subject string}`.

## Component E — Stash list window (`S`, right column)

`stashView` pointer field (`m.stashView *stashView`), holding `entries
[]model.StashEntry`, `sel int`, `loading bool`, and a stale-gating tag.

- **Open:** `S` (gated `!running && !loading`) fires an async `StashList`; while
  loading the window shows "(loading…)". An empty list shows "(no stashes)".
- **Render** (`view.go`): when `stashView != nil`, the **right column** draws the
  stash list (`stash@{N}` index + subject, newest first) instead of the Commits
  panel — same geometry/border, selection highlighted. The three left panels
  render normally (until `l` opens the file tree there).
- **Keys** (a routing layer like `filesView`'s, scoped — swallows non-handled
  keys):
  | Key | Action |
  |-----|--------|
  | `j/k/↑↓` | move stash selection |
  | `pgup/pgdn` | page the list |
  | `l` | resolve the stash SHA (`StashCommit`) → load `filesView` with `filesHash = SHA`, `filesTitle = "Stash <ref> <subject>"`; files render in the left column |
  | `enter` | open the **stash-action popup** for the selected stash |
  | `S` / `esc` | close the window (if `filesView` is open, `esc` closes that first) |
  | `q`/`ctrl+c` | quit (unchanged) |

  Inside the file tree (`filesView`) the keys are the commit-files set unchanged
  (`enter`→diff, `h`→history, `b`→blame, `/`→search, `l`/`esc`→close) — they
  operate on the resolved stash SHA, so **blame works** (a stash is a commit).

- **Refresh:** after an action-popup op completes, reload `StashList` (the list
  changed) and re-resolve; a now-missing selection clamps. `reRoot` clears
  `stashView` and `filesView`.

## Component F — stash-action popup (`Enter`)

A small menu popup (`stashActionPopup`, pointer field) listing **Apply** /
**Pop** / **Drop** for the selected `stash@{N}`, title = the stash subject.

- `↑↓`/`j`/`k` move; `enter` runs the highlighted action; `esc` cancels.
- **Apply** → `engine.StashApply{Ref}` (keeps the stash). **Pop** →
  `engine.StashPop{Ref}` (apply + drop). **Drop** → a confirm step
  ("drop stash@{N}? y/n") then `engine.StashDrop{Ref}`.
- On completion, full reload (working tree and/or stash list changed) and the
  stash window's list refreshes.

## Refresh strategy

All stash mutations (create/apply/pop/drop) change both the working tree and the
stash list, so the existing `startOp → opFinishedMsg → loadCmd()` full reload is
correct (these are one-shot actions, not hot loops like `space` staging).

## Out of scope (YAGNI / later)

- Partial-hunk stashing (whole-file paths only).
- "Branch from stash" (`git stash branch`).
- A CLI `gg stash` surface (TUI-only for now).
- Showing untracked-only files in the stash file tree — they live in the stash's
  3rd parent, not the first-parent diff the tree uses; the changes are still
  stashed, just not listed. (v1 limitation, documented.)
- `--keep-index` semantics / staged-only vs unstaged-only nuance — v1 stashes the
  working-tree changes for the selected paths.

## Testing

- `internal/git`: `FakeRunner` argv for `StashPush` (paths, `-u`, `--` guard),
  `StashPop(ref)`, `StashApply`, `StashDrop`; a real-repo (`newRepo`) round-trip
  — create two files, stash a subset by path, assert the tree reverts and
  `StashList` grows; pop and assert restoration.
- `internal/domain`: `StashList` parse table tests (normal, branch with colons in
  the subject, empty); `StashCommit` resolves a ref to a SHA.
- `internal/engine`: `Stash{Paths, IncludeUntracked}` stashes only the named
  paths; `StashApply`/`StashPop`/`StashDrop` against a seeded stash; Summary
  text.
- `internal/tui`:
  - multi-mark: `m` on Status toggles a path in `fileMarks`; a second `m`
    elsewhere adds, not replaces; reload preserves marks; Branches `m` unchanged.
  - stash popup: `s` opens with all unstaged candidates; marked subset
    pre-checked; `space` toggles; `ctrl+s` dispatches `engine.Stash` with the
    checked paths; empty-selection refusal; "nothing to stash" no-op; key-swallow.
  - stash window: `S` opens and the right column shows fed stash entries while
    left panels stay; `j/k` moves; `l` loads `filesView` for the resolved SHA
    (title shows the stash); `enter` opens the action popup; `esc` closes
    files-then-window; action popup Apply/Pop/Drop dispatch the right ops; Drop
    confirms; `reRoot` clears.

## Files touched

| File | Change |
|------|--------|
| `internal/git/stash.go` (+test) | extend `StashPush`; `StashPop(ref)`; add `StashApply`, `StashDrop` |
| `internal/model/model.go` | `StashEntry{Ref, Subject}` |
| `internal/engine/gitops.go` | verb signatures |
| `internal/engine/ops_basic.go` (+test) | `Stash` gains `Paths`/`IncludeUntracked`; add `StashApply`/`StashPop`/`StashDrop` ops |
| `internal/domain/query.go` (+test) | `StashList`, `StashCommit` gated queries + parser |
| `internal/tui/mark.go`, `model.go` | `fileMarks` set + `m`-on-Status branch |
| `internal/tui/stash_popup.go` (+test) | the stash-create popup |
| `internal/tui/stash_view.go` (+test) | the stash list window + routing |
| `internal/tui/stash_action_popup.go` (+test) | the Apply/Pop/Drop popup |
| `internal/tui/files_view.go` | source `filesView` from a stash SHA (title/hash set by the stash window) |
| `internal/tui/view.go` | render stash list in the right column; marked-row marker in Status |
| `internal/tui/model.go` | `s`/`S` dispatch + popup/window routing |
| `internal/tui/footer.go`, `help.go` | `[s] stash`, `[S] stashes`, window hints + help rows |
| `CHANGELOG.md`, `README.md` | entries |

## Decomposition (for the plan — L feature, three chunks)

1. **Create path:** multi-mark on Status + stash-create popup + extended
   `StashPush` verb/op. (Stashes work and are visible via `git stash list`.)
2. **View path:** the Stash list window (`S`, right column) + `l` drill into the
   reused file tree (`StashList`/`StashCommit` reads).
3. **Manage path:** the stash-action popup (`Enter`) + `StashApply`/`StashPop`/
   `StashDrop` verbs/ops.

## Open decisions to confirm at review

1. Marker glyph for multi-marked Status rows (recommend a leading `●`).
2. Stash-popup toggle key — `space` (recommended; matches staging muscle memory).
3. Action set in the `Enter` popup — Apply/Pop/Drop (confirmed); "Branch from
   stash" deferred.
