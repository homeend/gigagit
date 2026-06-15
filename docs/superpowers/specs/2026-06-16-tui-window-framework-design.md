# TUI Window Framework + Panel Restructure — Design

**Status:** design (approved, pre-plan).
**Date:** 2026-06-16
**Requirements input:** [`2026-06-13-tui-layout-layer-requirements.md`](2026-06-13-tui-layout-layer-requirements.md)

## Goal

Give every list/tree/text window in the TUI a shared rendering primitive that
supports three switchable display modes (**wrap / cutoff / scroll**), then use
that foundation to restructure the left column: merge Branches + Worktrees into
one **tabbed** slot, and split today's Status panel into **Files** (working-tree
changes) and **Staged** (index) panels.

## Background

The TUI currently renders list content in **four** hand-rolled ways, all
hardcoded to truncate-only (`truncate` + `padRight` + `windowRows`):

- `renderPanel` — the base panel grid (Branches/Worktrees/Status/Commits).
- `renderListBox` — the stash list (replaces Commits in the right column).
- `renderFilesView` — the reshape-base file tree (replaces the left column).
- inline loops in `history_view.go` / `blame_view.go`.

Separately, the **diff view already has** the exact three modes we want, cycled
with the `w` key (`diffLong`/`diffPartial` state, `diff_view.go:446`). This
design generalizes that one good idea into a reusable primitive and retires the
four duplicated render paths.

The repo switcher popup (`repo_popup.go`) builds its body with a
`strings.Builder` and lets `modalStyle.Width(...)` **wrap** long paths, which
looks bad. Routing popups through the primitive (cutoff default) fixes it.

## Central principle: content vs. compositing

The reusable unit is a **content primitive**, not a compositing mechanism. It is
orthogonal to *how* a box is placed on screen. The requirements doc's surface
inventory (§2.1) and its four compositing kinds stay exactly as they are:

| Surface | Compositing kind | Mechanism (unchanged) | On `viewStack`? |
|---|---|---|---|
| Branches/Worktrees/Files/Staged/Commits | base grid | `renderPanel` + per-panel maps | no |
| Stash list | panel-slot replace (right col) | `m.stashView` ptr | no |
| Files tree | reshape-base (left col; Commits stays live) | `m.filesView` ptr | no |
| Diff pane | replace / embedded | own colored renderer | (`m.diffView` ptr) |
| History / Blame | replace surface | `surface` on the stack | **yes** |
| Repo/settings/branch/conflict/help/… popups | overlay | `overlayCenter`, ptr fields | no |
| Modal (engine decision) | overlay, top priority | reply channel | no |

`viewStack` is **only** for full-screen replace surfaces (history/blame today;
conflict editor / graph / rebase editor later). Popups are **not** put on the
stack; the stash and files-tree are **not** surfaces. What changes for all of
them is only that their *rows* render through the primitive. The diff pane keeps
its specialized colored renderer (it already has the modes); the modal keeps its
own path.

## The window primitive (`internal/tui/window.go`)

A stateless render function plus a small mode enum, matching the existing
`viewstate.go` idiom (helpers read state the caller owns; no hidden state).

```go
// dispMode is a window's text display mode, cycled with `w`.
type dispMode int

const (
    modeCutoff dispMode = iota // truncate each row to width (one line)
    modeWrap                   // wrap each row onto multiple lines
    modeScroll                 // keep rows full; reveal via horizontal scroll
    dispModeCount
)

func (d dispMode) next() dispMode { return (d + 1) % dispModeCount }

// winRow is one logical row before layout: its raw text plus an optional style
// applied AFTER truncation/wrapping (ANSI-safety: never style then slice).
type winRow struct {
    text  string
    style lipgloss.Style // zero value = no styling
}

// winOpts is everything the renderer needs that is not a row.
type winOpts struct {
    w, h      int      // inner box dimensions (already minus borders/labels)
    mode      dispMode
    sel       int      // selected logical row, -1 for none
    selStyle  lipgloss.Style
    vscroll   int      // top logical row (callers may let the primitive derive it from sel)
    hscroll   int      // horizontal offset, modeScroll only
}

// renderWindow lays rows out under opts and returns exactly opts.h display
// lines, each padded to opts.w. It applies row/selection styling only after
// truncating or wrapping. In modeCutoff it keeps the windowRows() behaviour
// (scroll the viewport to keep sel visible); in modeWrap it scrolls over the
// expanded wrapped-line set; in modeScroll it applies hscroll then truncates.
func renderWindow(rows []winRow, opts winOpts) []string
```

Companion helpers:

- `selectedRowTruncated(rows []winRow, opts winOpts) bool` — true in `modeCutoff`
  when the selected row is wider than `w` (drives the reveal).
- A per-window state holder the callers embed/own:
  - **panels:** new `dispModes map[panel]dispMode` and `hscroll map[panel]int`
    on `Model` (siblings of `sortModes`, `sel`).
  - **popups / surfaces:** a `dispMode` + `hscroll` field on the popup/surface
    struct (pointer-held, survives the value-receiver copy).

### Display-mode key

`w` cycles the **focused** window's `dispMode`. The diff view keeps its existing
`w` (same concept, same three modes). To free `w` in the base grid, the existing
"create worktree for the selected branch" action **moves from `w` to `W`**
(`model.go:419`, and the worktree popup's confirm key advertises accordingly).

### Truncated-row reveal (tooltip) generalization

Today `tooltip()` shows the full text of the focused **panel's** selected row
when it is truncated, keyed on focus+selection (not mouse). It is suppressed
while a popup is open. This design:

- Keeps the panel behaviour.
- Adds the same reveal for the **focused windowed popup** (e.g. the repo popup),
  so a cut-off path can be read. The "suppress while popup open" rule is relaxed
  to "suppress the *panel* tooltip while a popup owns focus; the popup provides
  its own reveal for its selected row."
- The reveal fires only in `modeCutoff` (in `wrap`/`scroll` the row is already
  fully visible).

## Stage 1 — Window primitive + content unification

**Outcome:** all list/tree/text windows render through `renderWindow`; `w`
cycles modes everywhere; repo popup no longer wraps; worktree-create is `W`.
Diff pane and modal untouched. No layout/geometry change.

Convert these consumers (each keeps its current look because `modeCutoff` ==
today's behaviour):

1. `renderPanel` body → `renderWindow`.
2. `renderListBox` (stash) → `renderWindow`.
3. `renderFilesView` tree body → `renderWindow` (headings carried as
   `winRow.style = titleStyle`, applied after layout; cursor style still wins).
4. `history_view.go` + `blame_view.go` inline list loops → `renderWindow`.
5. All popups that list rows (repo, settings, branch, worktree, conflict,
   content/help, pairop, stash-action) → `renderWindow` for their bodies.
6. Add `dispModes`/`hscroll` panel maps + popup/surface `dispMode` fields; wire
   `w` in each focused-window key handler to cycle and re-clamp scroll.
7. Move worktree-create `w` → `W`.
8. Generalize the reveal to the focused popup.

**Docs:** CHANGELOG (modes + `W` remap), README (keys), `help.go` + footer
(`w` cycle, `W` worktree).

## Stage 2 — Tabbed Branches/Worktrees

**Outcome:** Branches and Worktrees share one left slot with a tab bar; Ctrl+←/→
switches and focuses the active tab; each keeps its own selection/sort/filter.

- New `Model` field `activeLeftTab panel` (one of `panelBranches`/`panelWorktrees`).
- The tabbed slot renders a `[ Branches │ Worktrees ]` header (active tab
  emphasized) above the active tab's panel body (via `renderWindow`).
- `panelBranches` and `panelWorktrees` remain **distinct** panels: `sel`,
  `sortModes`, `dispModes`, filter, and marks stay keyed by the `panel` enum.
  The tab only chooses which renders in the slot.
- **Ctrl+←/→** sets `activeLeftTab` to the other tab and moves `m.focus` to it.
  (Plain `left`/`right` still hop left-column ↔ Commits.)
- Focus order for `tab`/`shift+tab` becomes a computed sequence:
  `[activeLeftTab] → Files → Staged → Commits` (Files/Staged land in Stage 3;
  until then `[activeLeftTab] → Status → Commits`).
- Mouse: clicking the inactive tab header switches the tab; clicking the body
  focuses the active tab (extend `panelAt`).

**Docs:** CHANGELOG, README, help/footer (Ctrl+←/→ tabs).

## Stage 3 — Files / Staged split

**Outcome:** the old Status panel becomes two panels — Files (working tree) and
Staged (index) — so staged content is visible at a glance.

- Rename `panelStatus` → `panelFiles`; add `panelStaged`; bump `panelCount`.
- Classification (pure TUI filter over the existing `m.status.Files`; the
  porcelain-v2 XY bytes are already on `model.FileStatus`):
  - **Files** = `Unstaged != '.'` (worktree differs from index) OR untracked
    (`'?'`). Unmerged/conflict entries appear here.
  - **Staged** = `Staged != '.'` AND `Staged != '?'` (index differs from HEAD).
  - A partially-staged file (e.g. `MM`) appears in **both** — intended.
- `listFor`/`panelView` gain a `panelStaged` case; the `statusList` row builder
  is parameterized by which side it represents (or split into two builders).
  Sorting/filtering/marks work unchanged via the `panelList` contract.
- Geometry (`viewstate.go` `layout()`): left column stays **3 stacked slots** —
  `[Tabbed Br│Wt] / Files / Staged` (was Branches/Worktrees/Status). A clean
  swap of slot identities, not a new row count.
- **Short-terminal collapse** (`bodyH < 9`) gets an explicit rule: drop **Staged**
  first (Tabbed-slot over Files); the existing very-narrow (`w < 40`) single
  Commits column is unchanged.
- `lastLeftPanel` return-target and the panel-cycle logic updated for the new
  set; selection-clamp loops (`model.go`) iterate the new `panelCount`.
- Action keys that targeted `panelStatus` (diff on a file, stage/unstage,
  history/blame entry points) now target the correct panel: stage acts from
  Files, unstage from Staged; `enter`/`h`/`b` work from both.

**Docs:** CHANGELOG, README, help/footer (Files/Staged, which panel stages vs.
unstages). No `agentskill`/CLI change — this is TUI-only (FR-4 preserved).

## Testing strategy

Follow TDD; tests precede each conversion.

- **Primitive (`window_test.go`):** per mode — `modeCutoff` truncates and keeps
  sel visible; `modeWrap` expands and scrolls; `modeScroll` applies hscroll;
  exactly `h` lines out, each `w` wide; selection style applied after layout
  (assert via `ansi.Strip` that text survives, and that no style bleeds across a
  wrap boundary); `selectedRowTruncated` boundary at exact width.
- **Reveal:** repo popup with a path longer than the box → reveal lines present
  for the selected row; not present in `wrap`/`scroll`.
- **Mode key:** `w` cycles the focused window's mode and is independent per
  window; `W` now creates the worktree (and `w` no longer does on Branches).
- **Tabs:** Ctrl+←/→ flips `activeLeftTab` and focus; each tab retains its own
  `sel`/sort/filter across switches; focus order walks the computed sequence.
- **Files/Staged:** a snapshot containing `MM`, `M.`, `.M`, `??`, and a `UU`
  conflict lands each entry in the right panel(s); partial-stage in both.
- **Geometry:** 3-slot layout positions; `bodyH < 9` drops Staged; `w < 40`
  single column unchanged. Reuse the existing `fit_test.go`/`reroot_test.go`
  style harnesses.
- Existing panel/stash/files-view/history/blame tests must keep passing after
  the conversion (cutoff default == prior output).

Gate: `./test.sh race` green before each stage merges.

## Risks / constraints

- **ANSI-safety:** `truncate` slices runes by `lipgloss.Width`; styling must be
  applied only after truncation/wrapping. `modeWrap` is the sharp edge — wrap
  the raw text, then style each produced line.
- **Value-receiver `Model`:** per-window mode/scroll for panels lives in
  pointer-backed maps on `Model`; for popups/surfaces on their pointer structs.
- **Focus/routing:** Stage 2/3 change the focus order and `lastLeftPanel`; the
  triple-synced render/key/mouse arms must move together (the routing-invariant
  hazard from the requirements doc). Keep changes local and test the walk.

## Non-goals / YAGNI

- No declarative "named layouts" registry (the big-bang the user declined).
- No tiling/window-manager generality.
- No engine/domain/CLI/MCP changes (TUI-only).
- Diff pane and modal are **not** converted onto the primitive in this effort
  (diff already has the modes; modal is an option-list with its own path).
