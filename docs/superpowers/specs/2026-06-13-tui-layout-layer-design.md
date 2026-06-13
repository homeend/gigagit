# TUI Layout Layer + History View — Design

**Date:** 2026-06-13
**Status:** design (pending user review → writing-plans)
**Requirements:** see `2026-06-13-tui-layout-layer-requirements.md`
**Scope:** the layout machinery, **driven by the History view as its first real
consumer**. Blame and migration of remaining surfaces are explicit follow-ups.

---

## 1. Decisions locked in (from brainstorming)

| # | Decision | Rationale |
|---|---|---|
| D1 | **One dispatcher from day one.** A view stack replaces the three hand-synced `if`-chains (`render` / key / mouse) immediately. Existing surfaces are wrapped as stack entries by reusing their current render/update funcs verbatim — mechanical, behavior-identical. No "two dispatch systems" transition. | Kills the routing-invariant footgun without a risky internal rewrite. |
| D2 | **Input owner = top of stack, always. Compositing kind is render-only.** | Verified against `model.go:209`: when `filesView != nil`, the entry owns *all* input; the base only contributes render. The per-kind input-owner table is unnecessary. |
| D3 | **Typed surface structs that own their state.** No `WindowState map[string]any`. A *minimal* dispatch interface only (`render`/`update`/`kind`). | Preserves the codebase's compile-time typing; window reuse falls out of sharing a typed struct at different bounds. |
| D4 | **Key handling is Go code, not a string DSL.** A surface's `update` decides what each key does; no `When: "..."` evaluator. | A predicate interpreter is unjustified machinery for ~30 keys. |
| D5 | **Thin layout registry: `name → {kind, windows}`.** Each layout computes its own geometry in a small func (like today's `layout()`); no generic constraint solver. | FR-1 justifies *naming/centralizing* layouts, not a proportional-width solver for ~4 shapes. |
| D6 | **History is built on the layer first, end-to-end with tests; remaining surfaces migrate after.** | Validates the abstraction with working software, and forces the "diff surface as an embedded sized pane" case a pure refactor never exercises. |

Resolved open questions: OQ-1 reshapeBase = base-render-mutation (D2); OQ-2 thin
registry (D5); OQ-4 history-first then migrate (D6). OQ-3 (concurrent-op decision
routing) stays deferred to workspace-group-sync; the layer must not preclude it.

---

## 2. The machinery

### 2.1 Surface model

```go
// compositingKind is a render-only property (D2).
type compositingKind int
const (
    kindBase        compositingKind = iota // the panel grid; stack entry 0
    kindReshapeBase                        // renders some regions; base fills the rest
    kindReplace                            // full-screen; hides everything below
    kindOverlay                            // composited over the rendered background
)

// surface is the minimal dispatch interface (D3). Concrete types are the
// existing typed structs (*diffView, *filesView/contentPopup, *modalState, …)
// plus the new *historyView. Each owns its own state.
type surface interface {
    kind() compositingKind
    // render draws the surface. For kindOverlay/kindReshapeBase, `below` is the
    // already-composited image beneath it; replace/base ignore it.
    render(m Model, below string) string
    // update handles input while this surface is the stack top. It may mutate
    // and return Model (push/pop, set tags, fire loaders).
    update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
}
```

The stack lives on `Model` behind a pointer field (CF-12):

```go
type viewStack struct{ entries []surface } // entries[0] is always the base
func (s *viewStack) top() surface { return s.entries[len(s.entries)-1] }
func (m Model) push(s surface) Model { /* append; returns m */ }
func (m Model) pop() Model          { /* drop top if len>1; returns m */ }
```

### 2.2 The single dispatcher

```go
// render: walk bottom→top; a Replace entry overwrites the canvas, Overlay/
// ReshapeBase composite over it.
func (m Model) render() string {
    canvas := ""
    for _, e := range m.stack.entries {
        switch e.kind() {
        case kindReplace, kindBase:
            canvas = e.render(m, "")          // ignores below
        case kindReshapeBase, kindOverlay:
            canvas = e.render(m, canvas)      // composes over below
        }
    }
    return clipToHeight(canvas, m.height)
}

// key/mouse: top of stack owns input (D2); global keys (q, ctrl+c) handled first.
func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    if isGlobalQuit(msg) { return m, tea.Quit }
    return m.stack.top().update(m, msg)
}
```

`esc` default = pop, but each surface's `update` sees it first and may consume it
(e.g. files-view clears its search before closing) — D4 in action.

### 2.3 Thin registry (D5)

```go
type layoutDef struct {
    name    string
    kind    compositingKind
    windows []string // window ids placed by the layout's own geometry func
}
var registry = map[string]layoutDef{ /* "grid", "files", "diff", "history", … */ }
```

The registry is a **catalog/index** (for naming, help, future surfaces). Geometry
stays in code: each multi-pane surface has a `bounds(m Model) (left, right Rect)`
helper, mirroring today's `layout()`. No solver.

### 2.4 Windows vs surfaces

A **window** is a reusable render+update unit a surface composes (FR-3). The first
shared window is the **diff pane**: today's side-by-side renderer, extracted to
render into an arbitrary `Rect` rather than only full-screen. The full-screen
`diffView` surface and the `historyView` surface both drive the *same* diff-pane
window at different bounds. This extraction is the concrete payoff of D3/D6.

---

## 3. History view (the first consumer)

### 3.1 Behaviour (UR-1)

A `kindReplace` surface: **left column = commits that touched the file** (newest
first), **right = the file's diff at the selected commit** (commit vs first
parent, path-scoped), rendered via the shared diff-pane window. `j/k` moves the
commit selection and reloads the right pane (tag-gated, CF-6). `esc` pops back to
wherever history was opened from.

```
┌ History: src/auth.go ──────────────┬──────────────────────────────┐
│ @ a1b2  ada  2d  fix login guard   │  (diff of src/auth.go at a1b2 │
│   c3d4  bob  3w  extract token     │   vs its first parent, shown  │
│   e5f6  ada  6w  initial auth      │   in the shared diff pane)    │
│ …                                  │                               │
└────────────────────────────────────┴──────────────────────────────┘
 [↑↓] commit  [b] blame  [esc] back        (n/p/f diff sub-modes apply)
```

### 3.2 Navigation context (CR-7)

```go
type navContext struct {
    path string // repo-root-relative
    rev  string // "" = working tree (status entry); else a commit-ish
}
```

The three entry points (UR-3) all build a `navContext` and `push(newHistoryView(ctx))`:

| Source surface | Key | `navContext` |
|---|---|---|
| Status panel file | `h` | `{path, rev: ""}` (history from HEAD) |
| Files-view tree row | `h` | `{path, rev: <that commit>}` |
| Diff view | `h` | the diff's existing context (status → HEAD; commit → that commit) |

Wiring the three sources means those three surfaces gain an `h` case in their
`update` that pushes history — the first cross-surface use of `push`.

### 3.3 Git verbs (one invocation each, `internal/git`)

- **`FileLog(ctx, rev, path string, limit int) ([]model.FileCommit, error)`** —
  `git log [rev] --follow -M --name-status --format=<logFormat> -n <limit> -- <path>`.
  Returns per-commit `{Commit, Status, OldPath}` so the right-pane diff knows the
  file's name/status **at that commit** (rename-correct — the trap the advisor
  flagged). Needs a new combined parser (format line + interleaved name-status).
- Right-pane diff reuses the existing per-commit path/status diff logic
  (`loadCommitDiffCmd` pattern + `ShowFile` + `fillDiff`), fed the `FileCommit`'s
  status/oldPath — no new diff code.

### 3.4 Model type (`internal/model`)

```go
type FileCommit struct {
    Commit            // embeds Hash/Author/UnixTime/Subject/Parents
    Status  string    // "M","A","D","R",… of the file at this commit
    OldPath string    // pre-rename name, when Status=="R"
}
```

### 3.5 Performance guards (monorepo)

- History list capped via `-n` (e.g. 200) with a "more…" affordance deferred.
- Right-pane diff reuses the existing `maxDiffBytes` / binary / truncation guards.
- Loads are async `tea.Cmd`s with tag-gated staleness (CF-6), tag =
  `"history:" + rev + ":" + path` for the list, `"histdiff:" + hash + ":" + path`
  for the pane.

### 3.6 Responsive (CR-8, D5)

History's geometry func: below ~60 cols, show the commit list only (diff pane
collapses); `enter` on a commit then opens the existing full-screen diff surface.
At/above 60, split left|right. Policy lives in history's `bounds()` func.

---

## 4. Build order (D6)

1. **Stack + dispatcher + surface interface.** Wrap *existing* surfaces (base
   grid, diff, files-view, each popup, modal) as entries by reusing their current
   render/update funcs. Behavior-identical; full `./test.sh` stays green. This is
   the one-dispatcher move (D1) — mechanical, no logic change.
2. **Extract the diff-pane window** to render into a `Rect`; full-screen
   `diffView` becomes a thin caller. Tests unchanged.
3. **Git verb + parser + model type** (`FileLog`, `FileCommit`) — TDD with a real
   repo fixture and rename coverage.
4. **History surface** (`historyView`) on the stack, driving the diff-pane window;
   list + right pane + responsive collapse.
5. **Wire the three `h` entry points** (status / files / diff) → `push(history)`.
6. **Help + docs:** `?` help line, `CHANGELOG.md`, `README.md` if surface-facing.

Each step is independently testable and leaves the tree shippable.

---

## 5. Non-goals / follow-ups

- **Blame (`b`)** — the next consumer on the layer; its grouped-block gutter and
  the blame↔history cross-link (UR-4 second half) are a separate spec.
- **Migrating remaining surfaces' *internals*** beyond the mechanical wrapping in
  step 1 — they already work; revisit only as cleanup.
- **Concurrent-op decision routing** (OQ-3) — owned by workspace-group-sync.
- **CLI/MCP** history — TUI-only for now (FR-4); a `gg log <file>` CLI verb is a
  plausible later add but out of scope.
- **No layout DSL, no `map[string]any`, no constraint solver** (D3/D4/D5).

---

## 6. Testing strategy

- **Dispatcher:** table tests over stack configurations asserting (render order /
  input owner) — proves D1/D2 and the routing-invariant collapse.
- **Compositing:** golden-ish assertions for overlay-over-reshape-over-base (case
  b from the trace).
- **`FileLog` parser:** real-repo fixtures incl. a rename, a delete, the root
  commit; assert per-commit status/oldPath.
- **History surface:** `j/k` reload + tag staleness; `esc` pop returns to source;
  narrow-width collapse; `h` from each of the three sources lands the right
  `navContext`.
- **Regression:** existing TUI tests must pass unchanged after step 1 (proof the
  wrap is behavior-identical).
