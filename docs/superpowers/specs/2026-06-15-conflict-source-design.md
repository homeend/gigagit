# Conflict source attribution — design

**Date:** 2026-06-15
**Status:** approved
**Roadmap:** F4 conflict resolution, follow-up.

## Goal

When the repository is conflicted, tell the user *what produced the conflict* —
"merging `feature` into `main`" or "rebasing `feature` onto `main`" — in both the
status-bar notice and the resolution popup. Today the notice only says how many
conflicts there are, not where they came from.

## What exists today

- `model.WorkingTreeStatus.Conflicts()` lists unmerged files; the status bar
  shows `⚠ N conflict — press [x] to resolve` (`internal/tui/view.go`).
- The resolve popup (`internal/tui/conflict_popup.go`) renders a header and a
  per-file list; it loads `merge`/`rebase`/`""` asynchronously via
  `domain.InProgressOp` only to gate the continue/abort keys.
- `domain.Snapshot` (`internal/domain/query.go`, `loadSnapshot`) is the TUI's
  single startup/refresh read; it already calls `s.repo.Status` etc. directly on
  the concrete `*git.Repo`.

## Source of truth (verified, git 2.43)

- **Merge in progress** (`MERGE_HEAD` resolves):
  - target = current branch = `git symbolic-ref --short HEAD` (also already in
    `Status.Branch`).
  - source = `git name-rev --name-only --refs=refs/heads/* MERGE_HEAD` → e.g.
    `feature` (falls back to a commit-ish name / short SHA when no branch).
- **Rebase in progress** (merge backend, the default since git 2.26):
  - `<git-dir>/rebase-merge/head-name` holds `refs/heads/<branch>` → strip the
    prefix → the branch being rebased.
  - `<git-dir>/rebase-merge/onto` holds the onto SHA → resolve with
    `git name-rev --name-only --refs=refs/heads/* <sha>` → the onto branch.
  - `<git-dir>` via `git rev-parse --absolute-git-dir` (per-worktree, where the
    rebase state lives).
- **No op in progress** (e.g. a stash-pop conflict — `MERGE_HEAD` absent, no
  rebase dir): there is no git-tracked source. The notice stays unchanged.

## Design

### 1 — `git` read verbs (`internal/git/conflict.go`)

- `MergeHeadName(ctx, dir) (string, error)` — `name-rev` of `MERGE_HEAD`,
  restricted to `refs/heads/*`, returns the trimmed name (or "" on error).
- `RebaseParties(ctx, dir) (branch, onto string, err error)` — resolves the
  absolute git dir, reads `rebase-merge/head-name` (strip `refs/heads/`) and
  `rebase-merge/onto` (name-rev to a branch). Missing files → `("","",nil)`
  (e.g. the `am`/`rebase-apply` backend we don't model) so the caller degrades
  to "rebase" with no parties rather than erroring.

These are reads on `*git.Repo` used by the domain read path. They do **not** go
on the `engine.GitOps` interface (that is for engine operations).

### 2 — domain conflict state (`internal/domain/conflict.go`)

```go
type ConflictState struct {
	Op     string // "merge" | "rebase" | ""
	Source string // branch being merged / rebased ("" if unknown)
	Target string // branch merged-into / rebased-onto ("" if unknown)
}

func (c ConflictState) Describe() string {
	switch {
	case c.Op == "merge" && c.Source != "" && c.Target != "":
		return "merging " + c.Source + " into " + c.Target
	case c.Op == "rebase" && c.Source != "" && c.Target != "":
		return "rebasing " + c.Source + " onto " + c.Target
	}
	return ""
}
```

A new unexported helper `conflictState(ctx, st)` computes it:
- only when `st.Counts().Conflicted > 0` (otherwise zero value — no extra git
  calls on clean repos);
- merge first (probe `MergeInProgress`), then rebase (`RebaseInProgress`), else
  zero value.

`Snapshot` gains a `Conflict ConflictState` field, populated in `loadSnapshot`
after `Status` is known. The existing `InProgressOp` query is left untouched
(the popup keeps using it to gate continue/abort, which must work after the last
conflict is resolved — when `Conflict` is intentionally empty).

### 3 — TUI wiring

- `dataLoadedMsg` carries `conflict ConflictState`; `Update` stores it as
  `m.conflict` (cleared to zero when `msg.err != nil` path leaves it; it is
  recomputed every load).
- **Status bar** (`view.go`): when conflicts exist and `m.conflict.Describe()`
  is non-empty, fold it into the notice:
  > `⚠ 1 conflict merging feature into main — press [x] to resolve`

  When `Describe()` is "" (stash-pop / unknown), the notice is unchanged.
- **Popup** (`conflict_popup.go` `renderConflictPopup`): when
  `m.conflict.Describe()` is non-empty, render it as a dim subtitle line under
  the "Resolve conflicts" header.

The popup reads `m.conflict` from the model (available because the popup only
opens when conflicts exist, and reopen-after-resolve runs off a fresh reload).

## Out of scope

- Source attribution for stash-pop / cherry-pick / revert conflicts (no
  git-tracked "into/onto" parties; cherry-pick/revert could be added later from
  `CHERRY_PICK_HEAD`/`REVERT_HEAD` if wanted).
- The `am`/`rebase-apply` backend (we degrade to "rebase" with no parties).
- Any change to resolution behavior — this is presentation only.

## Testing

- `internal/git`: real-repo tests — a merge conflict yields `MergeHeadName ==
  "feature"`; a rebase conflict yields `RebaseParties() == ("feature","main")`;
  a clean repo yields empty/no-error.
- `internal/domain`: `conflictState` over a real merge-conflict repo →
  `{merge, feature, main}` and `Describe()=="merging feature into main"`; over a
  rebase-conflict repo → rebase form; over a clean repo → zero value; `Snapshot`
  surfaces it.
- `internal/tui`: status bar shows the merge phrase with a merge conflict; the
  popup renders the subtitle; the no-op case (conflicts but `Op==""`) shows the
  bare notice and no subtitle.

## Files touched

| File | Change |
|------|--------|
| `internal/git/conflict.go` (+test) | `MergeHeadName`, `RebaseParties` |
| `internal/domain/conflict.go` (+test) | `ConflictState`, `conflictState`, `Describe` |
| `internal/domain/query.go` | `Snapshot.Conflict` + populate in `loadSnapshot` |
| `internal/tui/load.go` | carry `conflict` on `dataLoadedMsg` |
| `internal/tui/model.go` | `m.conflict` field + set on load |
| `internal/tui/view.go` | status-bar source phrase |
| `internal/tui/conflict_popup.go` (+test) | popup subtitle |
| `CHANGELOG.md`, `README.md` | entries |
