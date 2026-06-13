# TUI Layout Layer — Requirements Overview

**Status:** requirements gathering (pre-design). Feeds the layout-layer design spec.
**Date:** 2026-06-13

## Purpose

Today the TUI has no layout abstraction. "Which windows are shown and which one
owns input" is encoded implicitly as *which pointer fields on `Model` are
non-nil*, read in **three hand-synced places** — `render()` (`view.go`), the
key-dispatch chain in `Update()` (`model.go`), and the mouse arm — kept aligned
only by a "routing invariant" comment. Every new surface adds an `if` to all
three.

We want a **layout layer**: named, switchable arrangements of reusable windows,
driven by one mid-layer, so that adding a surface (history, blame, staging,
rebase editor, conflict editor, graph, …) is a local change, and so navigation
can stack and unwind (`esc` one hop) for cross-linked flows.

This document captures **all requirements** the layer must satisfy, from three
sources: (1) what the user asked for this session, (2) what current
functionality already does and must keep doing, (3) what backlog/roadmap
features will need. It deliberately does **not** pick an implementation
approach — it is the input to that decision.

---

## 1. User-provided requirements (this session)

The session began with a concrete feature request (history + blame) that then
generalized into the layout layer.

- **UR-1 — File history view.** Pressing `h` on a file opens a history view:
  **left column = the commits that touched the file** (newest first), **right =
  the file's diff at the selected commit** (that commit vs its first parent,
  path-scoped). Structurally the mirror of today's files-view (left tree → right
  diff).
- **UR-2 — Blame view.** Pressing `b` on a file opens a blame view: a single
  file with a per-area gutter showing the **last modifier** (author + relative
  age). **Grouped blocks**: consecutive lines from the same commit collapse into
  one gutter remark spanning the block (a `·` continuation marker beneath).
- **UR-3 — Entry points.** `h`/`b` must be triggerable from **(a)** Status-panel
  files, **(b)** the files-view tree, and **(c)** inside the diff view (pivot the
  file currently shown). All three reduce to a `(path, rev)` pair (see CR-7).
- **UR-4 — Cross-linked navigation.** History and blame are one coherent "explore
  this file" mode: from blame, `enter` on a block jumps to **that commit's**
  history/diff; from history, `b` opens blame at the selected commit. `esc`
  **unwinds one hop** (blame → history → diff → …), not straight to the main
  grid.
- **UR-5 — Layout layer (the meta-requirement).** A mid-layer that accepts a
  "switch to layout X" command and swaps which windows are shown / active.
  Layouts are **named** arrangements; **windows** are the reusable building
  blocks; key bindings trigger **actions**, and an action may include
  "switch-layout" as one step of its process. Must accommodate future features
  (§3), not just history/blame.

> Note on "action → process": the *git-mutation* half of this already exists as
> engine `Operation`s (emit `Event`s, resolve forks via a `Decider`), dispatched
> from the TUI by `startOp`. And Bubble Tea's `tea.Cmd` + `Update` already is a
> process runner. The gap the layout layer fills is the **navigation/layout**
> half, not a new command bus.

---

## 2. Requirements from current functionality (must preserve)

Any layout layer must keep everything the TUI does today working. Inventory of
surfaces and the behaviors they impose:

### 2.1 Surface inventory (what exists, and its compositing kind)

| Surface | Kind | Notes |
|---|---|---|
| Panel grid (Branches, Worktrees, Status, Commits) | **base** | The persistent workspace. Narrow (<40 cols) collapses to a single Commits column. |
| Files view | **reshape-base** | Swaps the left column for a commit's file tree while **Commits stays live on the right** (dual focus). Not a full overlay. |
| Diff view | **replace** | Full-screen side-by-side. Refuses to open below 60 cols. |
| Modal (engine decision) | **replace** | Highest priority; option-list pick that unblocks an op goroutine via a reply channel. |
| Worktree / repo / settings / branch / content(help) / pair-op popups | **overlay** | Centered, composited **over the live background** (`overlayCenter(bg, popup)`). |
| Tooltip | **overlay** | Positioned; suppressed while any popup is open. |

Three distinct compositing behaviors — **replace**, **reshape-base**,
**overlay** — must all be expressible. A flat "top of stack draws" model is
insufficient on its own because overlays render the layers beneath them.

### 2.2 Behavioral requirements (current)

- **CF-1 — Single input owner.** Exactly one surface owns keyboard at a time:
  the topmost visible one. Same for mouse. (The "routing invariant".)
- **CF-2 — Focus management in the base.** `tab`/`shift+tab` cycle panels;
  `left`/`right` move between columns; `lastLeftPanel` remembers the return
  target; per-panel focus styling (focused vs blurred borders).
- **CF-3 — Per-panel view state.** Each panel keeps independent **selection**
  (`sel[panel]`, with filtered-vs-full backing index), **sort mode** (`o`), and
  **filter** (`/` search with a typing-capture mode).
- **CF-4 — Nested focus within a reshape view.** Files view has its own
  inner focus (tree side vs Commits side) and its own key handler.
- **CF-5 — Async operations.** An op runs off-thread with: a `running` flag,
  a status line, streamed progress events, **mid-flight decisions routed to the
  modal** (reply channel), and a completion message that triggers a data reload.
- **CF-6 — Async reads with staleness gating.** Opening a diff/files-view fires
  a `tea.Cmd` loader returning a **tagged** message; fast `j/k` movement
  supersedes the tag so stale results are dropped.
- **CF-7 — Context-dependent `esc`.** `esc` does the least-destructive thing
  first: clear an active search → close the current view → … (per-surface).
- **CF-8 — Global keys.** `q` and `ctrl+c` quit from anywhere; `?` help; `,`
  settings; `R` repo switcher.
- **CF-9 — Responsive reflow.** `WindowSizeMsg` re-lays-out; very narrow
  terminals collapse columns and may force-close the diff/files view.
- **CF-10 — Mouse.** Click-to-focus a panel; wheel-over-panel scrolls that
  panel (`handleMouse`), routed to the same owner as keys.
- **CF-11 — Overlay-over-live-background.** Popups must see and composite over
  the current background render, including whatever base/reshape state is active.
- **CF-12 — Transient state across the value-receiver `Model`.** `Model` is a
  value receiver; surfaces that persist across the copy live behind **pointer
  fields**. Any layout state must respect this (pointer-held where it must
  survive).
- **CF-13 — Marks.** A "marked item" indicator (`◆ marked`) can coexist with the
  status line and running indicator.

---

## 3. Requirements from backlog / future features

The roadmap (CLAUDE.md Status, CHANGELOG, memory) adds many surfaces. The layout
layer should host these **without re-architecting** each time. This is the main
argument for formalizing now rather than adding three more `if`s per surface.

- **FR-1 — Many more surfaces (M3).** Staging / hunk view, **interactive-rebase
  todo editor**, **3-pane conflict editor**, **visual commit graph**,
  **sparse-checkout tree selector**, plus history/blame from this session, and a
  commit-feed surface (CQRS stage 3). Each is a new layout built from windows.
- **FR-2 — Concurrent background operations + decision routing.** Workspace group
  sync (named repo groups, parallel background-pull) and SmartPull's background
  ref-write mean an **op can run while the user interacts with another surface**,
  and **more than one** op may need a decision surfaced. The "routing invariant"
  comment already anticipates this ("background ops will rely on it"). The layout
  layer must allow: a foreground surface owning input **while** a background op
  progresses, and a way to **queue/surface pending decisions** without stealing
  the user's current context destructively. (Shared with the MCP frontend.)
- **FR-3 — Window reuse across layouts.** The side-by-side **diff pane** is
  already used by both the diff view and the files-view right side; history will
  be a third consumer. The diff window also has its own **sub-modes** (partial /
  full, `n`/`p` navigation — the `feat/diff-modes` work). Windows must be
  composable units reusable across layouts, and may carry internal sub-state.
- **FR-4 — Frontend-agnostic intent.** The engine/domain layers are deliberately
  frontend-agnostic (TUI / CLI / future MCP from one contract). The layout layer
  is **TUI-only** and must not leak into engine/domain; navigation/layout is a
  view concern, mutations stay in `Operation`s.

---

## 4. Derived cross-cutting requirements (the shape the design must satisfy)

Synthesizing §1–§3, the layout layer must provide:

- **CR-1 — Three compositing kinds:** replace, reshape-base, overlay (§2.1).
- **CR-2 — A navigation stack with one-hop unwind** so cross-linked flows
  (UR-4) and "open diff from files-view, esc back to files-view" both work.
- **CR-3 — A single dispatcher** mapping the current stack/state to (what
  renders) **and** (who owns keys/mouse) — collapsing today's triple-synced
  `if`-chains into one place (kills the routing-invariant footgun).
- **CR-4 — Named layouts** as the user-facing/Devel-facing unit ("switch to
  History"), built from **reusable windows** (CR + FR-3).
- **CR-5 — Per-surface retained view state** (selection, scroll, filter, inner
  focus) that survives being pushed under another surface and restored on pop.
- **CR-6 — Background-op compatibility:** a running (possibly multiple) op must
  not require owning the screen; decisions surface without destroying foreground
  context (FR-2).
- **CR-7 — A `(path, rev)` navigation context** so history/blame/diff entry
  points (UR-3) and cross-links (UR-4) are uniform: status file → working tree;
  files-view row → that commit; diff view → its existing context.
- **CR-8 — Responsive + mouse parity** preserved across all layouts (CF-9,
  CF-10).
- **CR-9 — Fits the codebase idioms:** value-receiver `Model`, pointer-held
  persistent state, `tea.Cmd` loaders with tag-gated staleness, engine
  `Operation`s for mutations (CF-12, CF-5/6, FR-4).

---

## 5. Non-goals / YAGNI boundaries

- **No new command/action bus for processes.** `tea.Cmd` + `Update` + engine
  `Operation`s already run processes; "switch-layout" is a Model mutation/helper,
  not a new orchestration engine.
- **No tiling/window-manager generality.** Windows are placed by named layouts,
  not freely resized/dragged/split by the user.
- **No engine/domain/CLI/MCP changes** for this layer — TUI-only (FR-4).
- **History/blame feature itself is out of scope of the layout-layer spec** — it
  is the first *consumer* built on top, in a follow-up.

---

## 6. Open questions (to resolve in the design spec)

- **OQ-1 — Is "reshape-base" (files-view style) a base *mode*, or a stack entry
  that re-renders the full interface?** Affects how Commits stays live.
- **OQ-2 — How declarative should "named layouts" be?** A data-driven registry
  of window placements (closest to the user's vision) vs. typed
  push/pop entries with imperative render — the A-vs-C decision, to be made with
  this requirements set in hand.
- **OQ-3 — Decision routing for concurrent ops (FR-2/CR-6):** queue + badge,
  or a dedicated "pending decisions" surface? May be deferred to the workspace-
  group-sync feature, but the layer must not preclude it.
- **OQ-4 — Migration scope:** big-bang migrate all surfaces onto the layer, or
  introduce the layer and migrate surfaces incrementally?
