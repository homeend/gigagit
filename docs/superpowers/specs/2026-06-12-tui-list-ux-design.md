# TUI List UX — Design Spec

**Date:** 2026-06-12
**Status:** Approved
**Scope:** Four list-interaction improvements to the TUI. No engine ops, no CLI
changes; one git-verb extension and one new verb.

## Goal

Make every list panel pleasant at monorepo scale: reverse focus cycling,
viewport-relative paging, selectable per-panel sort order, and `/` filtering —
all without ever letting an action key act on the wrong backing row.

## The keystone: one view pipeline per panel

Sorting and filtering both change *display order/membership* while every action
handler today indexes the backing slice directly
(`m.branches[m.sel[panelBranches]]`, `m.worktrees[m.sel[panelWorktrees]]`, …).
Done naively, each feature independently corrupts those indexes — the same bug
class the delete-worktree selection clamp fixed.

So both features share one mechanism: a per-panel **view pipeline**

```
backing slice → filter (substring) → sort (mode) → visible []int
```

where `visible` maps display row → backing index.

The pipeline is **generic**: it is written once against a small per-panel
contract, and each panel instance implements its own rules for what "name"
and "date" mean:

```go
// panelList is what a panel must provide for generic filtering and sorting.
// Each panel implements its own semantics; the pipeline never inspects
// concrete types.
type panelList interface {
	Len() int
	Row(i int) string  // display text — also the filter-match target
	Name(i int) string // what "sort by name" means for THIS panel
	Date(i int) int64  // what "sort by date" means for THIS panel (unix; 0 = unknown)
}
```

Adding sort/filter to a future panel = implementing `panelList`; the pipeline,
key handling, label rendering, and clamping need no changes.

`visible` is the single source of truth consumed by:

- `panelLen(p)` → `len(visible(p))` — selection bounds, ↑/↓, paging, and the
  post-load clamp all inherit correctness for free;
- `renderPanel` — rows are built in visible order;
- every action handler — `m.branches[m.visibleIdx(p, m.sel[p])]` style
  resolution replaces direct indexing (touches `s`, `d`, `enter`, `w` once each).

The pipeline is pure (computed from Model fields on demand, no cached state to
invalidate); filter/sort state lives in Model fields and survives reloads —
fresh data simply flows through the same transforms, then the existing clamp
bounds the selection.

## Feature 1 — Shift+Tab cycles focus backwards

`case "shift+tab":` beside the existing `tab` case:
`m.focus = (m.focus - 1 + panelCount) % panelCount`. Nothing else.

## Feature 2 — PageUp/PageDown: move by 25% of the panel viewport

- `pgup` / `pgdown` move the focused panel's selection by
  **`max(1, rowsCap/4)`** rows, clamped to `[0, panelLen-1]`.
- `rowsCap` is the panel's *actual* visible row count. To know it outside
  `View`, extract the layout's height math from `renderInterface` into a shared
  helper (e.g. `panelRowsCap(p panel) int` derived from `m.width`/`m.height`
  and the same `bodyH` breakpoints), used by both the renderer and the key
  handler so they cannot drift. A panel not visible in the current layout
  (or unknown size) falls back to a step of 1.

## Feature 3 — Selectable per-panel sort

### UX

- **`o`** (unbound today) on a focused panel cycles its sort mode:
  `default → name ↑ → name ↓ → date ↑ → date ↓ → default`.
- The panel label shows the active mode, e.g. `Branches ·date↓`
  (nothing shown for `default`).
- Sort state is **per-panel**, lives on the Model, survives reloads.
- Applies to **all four list panels** — each implements `panelList` with its
  own name/date semantics (table below).
- **Defaults:** Branches starts in `date ↓` (newest first — the original ask);
  Worktrees, Status, and Commits start in `default` (git's emission order;
  Commits is already newest-first from git). `default` always remains
  reachable as the escape hatch.

### Per-panel `panelList` semantics

| Panel | `Name(i)` | `Date(i)` |
|-------|-----------|-----------|
| Branches | branch name | last commit time (`committerdate`) |
| Worktrees | branch name; detached/bare fall back to path; path is the tiebreaker | HEAD commit time |
| Status | file path | file **mtime** via `os.Stat` (no git call); stat errors (e.g. deleted files) report 0 and sort last in both directions |
| Commits | subject | commit `UnixTime` (already in `model.Commit`) |

Name comparisons are case-insensitive; ties break by backing order (stable).

### Data plumbing

- **Branches:** extend the `Branches` verb's `for-each-ref` format with
  `%(committerdate:unix)` and add `UnixTime int64` to `model.Branch`
  (parser + table-driven parse tests extend; tolerate a missing/empty field
  as 0).
- **Worktrees:** worktree porcelain gives only the HEAD sha. New verb
  `CommitTimes(ctx, shas []string) (map[string]int64, error)` →
  one invocation: `git log --no-walk --format=%H%x00%ct <sha…>` (NUL-separated
  to stay parse-safe). Empty input → empty map, no git call. The TUI loads it
  in `loadCmd` alongside worktrees (non-fatal: on error, dates are 0 and date
  sort degrades to backing order).
- **Status:** mtimes read lazily at sort time via `os.Stat` against the repo
  root — only when a date mode is active on the Status panel.

## Feature 4 — `/` filter on every list panel

### UX

- **`/`** on a focused panel enters **filter-input mode**: typed runes build
  the query live; `backspace` edits; **enter** commits (keeps the filter,
  returns to normal keys); **esc** while typing cancels and clears.
- With a committed filter active, **esc** in normal mode clears it.
- The query renders in the panel label: `Branches /fix` (combined with a sort
  mode: `Branches ·date↓ /fix`).
- One filter at a time, bound to the panel it was started on; pressing `/` on
  another panel moves the filter there (clearing the old one). Focus changes
  (tab) do not clear it.
- Matching: **case-insensitive substring** over the row's display text
  (branch name, worktree branch+path, file path, commit subject). Fuzzy
  matching is out of scope (YAGNI).
- Filtering applies to all four list panels, Commits included.
- While filter-input mode is active, *all* keys are swallowed by the input
  handler (no `q` quit, no action keys); `ctrl+c` still quits.

### Key routing precedence (in `Update`'s `tea.KeyMsg`)

`modal` → `popup` → **filter input** → normal keys.

Filter state is plain Model fields (`filterPanel panel`, `filterQuery string`,
`filterTyping bool`) — inline input, not a popup; nothing needs pointer
semantics because every mutation happens on the returned Model copy.

### Interaction rules

- Selection, ↑/↓, pgup/pgdn, and the post-load clamp all operate on the
  filtered+sorted view (free, via the pipeline).
- Action keys resolve through `visible` — filtering then pressing `d`/`enter`/`s`
  acts on the row the user sees. This is the load-bearing correctness rule.
- Empty filter result renders the existing `(none)` row; action keys no-op via
  the `panelLen > 0` guards.
- On reload, the filter re-applies over fresh data; if the selected row
  disappears, the clamp bounds the selection.

## Files touched (expected shape)

| File | Change |
|------|--------|
| `internal/git/repo.go` | `Branches` format + `--sort` untouched (sorting is TUI-side); add `%(committerdate:unix)` field. New `CommitTimes` verb (same file or `log.go`, wherever log verbs live). |
| `internal/git/branch_parse_*.go` | Parse the new field; tests. |
| `internal/model/model.go` | `Branch.UnixTime int64`. |
| `internal/tui/model.go` | shift+tab, pgup/pgdn, `o`, `/` + filter-input routing, sort/filter Model fields, action handlers resolve via `visible`. |
| `internal/tui/viewstate.go` (new) | The `panelList` interface; the generic pipeline (filter, sort, `visible(p) []int`, `visibleIdx`, mode cycling); the four per-panel `panelList` implementations; `panelRowsCap` sizing helper (extracted from `renderInterface`). |
| `internal/tui/view.go` | `renderPanel` consumes visible order; label shows sort/filter; `renderInterface` uses the shared sizing helper. |
| `internal/tui/load.go` | Load worktree HEAD commit times (non-fatal). |
| Docs | `CHANGELOG.md`; `README.md` key table (`shift+tab`, `pgup/pgdn`, `o`, `/`, `esc`). |

## Testing

- **git:** `Branches` returns commit times (real repo, two branches, distinct
  commit timestamps); `CommitTimes` batch verb (multiple shas, one call —
  assert via FakeRunner argv too); parse tests for the extended format.
- **Pipeline (pure-function tests):** driven through the `panelList` interface
  with a fake implementation (proves genericity), plus the four real
  implementations: filter narrows + maps indices; name/date both directions
  per panel; stability on ties; stat-error files sort last.
- **TUI integration:** shift+tab full reverse cycle; pgup/pgdn step =
  max(1, rowsCap/4) and clamps at both ends; `o` cycles modes and label
  renders; `/` typing/backspace/enter/esc lifecycle; input mode swallows
  globals (press `p` while typing → nothing); **filtered-action correctness**
  (filter Worktrees, press `enter`/`d` → acts on the visible row's backing
  worktree); filter + reload → re-applied, selection clamped; filter + sort
  combined label.

## Out of scope (YAGNI)

- Fuzzy matching; persistent (cross-run) sort/filter state; CLI-side
  sort/filter flags; multiple simultaneous filters.
