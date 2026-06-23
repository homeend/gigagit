# TUI window architecture — abstraction levels & open-flows

**Status:** Architecture reference (living doc).
**Date:** 2026-06-23. Reflects windowing Stages 1a (diff → stack layer) and 1b
(files-view state machine).
**Scope:** how the `internal/tui` window system is layered, how the high-level and
middle-level abstractions interact, and the open-flow for each kind of window
(ranked by how often it is opened). All citations are `internal/tui/` on `main`.
**Companions:** `2026-06-22-key-routing-by-context.md` (the dispatch ladder per
context), `2026-06-22-windowing-zorder-root-cause.md`, the `adding-tui-windows`
skill (the build-time decision guide).

---

## 0. The two abstraction levels at a glance

```mermaid
flowchart TB
    subgraph HIGH["HIGH-LEVEL — the window *system* (how windows relate)"]
      direction TB
      DISP["Dispatch ladder<br/>(Model.Update, KeyMsg → one owner)"]
      REND["Render walk<br/>(Model.render → composite)"]
      STACK["layerStack + the layer contract<br/>(z-order, push/pop/removeLayer/layerOf)"]
      SLOTS["Priority slots<br/>(modal · proc · actionMenu)"]
      BASE["Base panel layout<br/>(layout(), m.focus enum)"]
    end
    subgraph MID["MIDDLE-LEVEL — the window *kinds* (self-contained units)"]
      direction LR
      POPUPS["popups<br/>(contentPopup, bookmarkPopup,<br/>commitPopup, …)"]
      SURF["surfaces<br/>(historyView, blameView,<br/>irebaseEditor, hunkPicker)"]
      DIFF["diffView<br/>(layer since 1a)"]
      FILES["files view<br/>(filesMode state machine)"]
      STASHW["stashView"]
      DECIDE["decisionState (modal)"]
      PROC["process / conflictProcess"]
      MENU["actionMenu (+ availableActions)"]
    end
    HIGH -->|"routes keys to / composites"| MID
    MID -->|"opening chooses a PLACEMENT;<br/>update() can push more layers"| HIGH
```

- **High-level = the *system*.** It owns the answers to "who is in front, who owns
  the keyboard, what is revealed when one closes." It does **not** know what any
  particular window *does*.
- **Middle-level = the *kinds*.** Each is a self-contained unit: its own state, its
  own `update` (key handling) and `render`. It does **not** know the dispatch order
  or what sits beneath it.
- **They meet at three placements** (next section): a window is opened *onto* the
  stack, *into* a `Model` field, or *into* a priority slot. That placement choice
  is the entire contract between the two levels.

---

## 1. High-level: the three placements

Every window lives in exactly one of three places. This is the central high-level
abstraction — it decides routing, render order, and close semantics.

```mermaid
flowchart TB
    K["key / event"] --> U["Model.Update"]
    U --> P1{"priority SLOT set?<br/>modal · proc · actionMenu"}
    P1 -- yes --> SLOT["slot owns input<br/>(above everything; fixed priority)"]
    P1 -- no --> P2{"stack non-empty?<br/>topLayer()"}
    P2 -- yes --> LAYER["top LAYER owns input<br/>(LIFO; push/pop; mutual z-order)"]
    P2 -- no --> P3{"a field WINDOW open?<br/>diffView*, filesView, stashView"}
    P3 -- yes --> FIELD["field window owns input<br/>(fixed rung in the ladder)"]
    P3 -- no --> BASE["base panels<br/>(m.focus; the floor)"]

    note["*diffView moved from FIELD to LAYER in Stage 1a"]
    LAYER -.- note
    style SLOT fill:#fdd,stroke:#c33
    style LAYER fill:#cde,stroke:#36c
    style FIELD fill:#dfd,stroke:#3a3
    style BASE fill:#ffd,stroke:#cc3
```

| Placement | Mechanism | Z-order semantics | Close | Who lives here |
|---|---|---|---|---|
| **Priority slot** | `m.modal` / `m.proc` / `m.actionMenu` | **Above** the stack, fixed priority `modal > proc > actionMenu` | set the field `nil` | decision dialogs, the conflict process, the `.` menu |
| **Layer stack** | `pushLayer` / `popLayer` / `removeLayer` | **LIFO, mutual** — anything can sit over anything; order is chosen at runtime | `popLayer` reveals the parent intact | popups, full-screen surfaces, **the diff** |
| **Model field** | `m.filesView` / `m.stashView` | **Fixed rung** in the dispatch ladder; cannot be z-ordered against the stack | set the field `nil` (`closeFilesView`) | the files view, the stash list |

**Why three and not one?** The slots have genuine "always-on-top regardless of
push order" semantics (a modal must beat even a process that raised it) that a LIFO
stack cannot express — so they are *correctly* outside the stack. The field windows
(files view, stash) are entangled with the base panel layout (they share the
left/right columns and the `m.focus` enum), so they are not yet stack citizens.
**Unifying the field windows onto the stack is the deferred Stage 3** — see the
investigation doc.

### The layer contract (the stack's whole vocabulary)

```mermaid
classDiagram
    class layer {
      <<interface>>
      +update(m, msg) ModelAndCmd
      +render(m, below) string
    }
    class layerStack {
      +entries listOfLayer
    }
    class Model {
      +layers layerStack_ptr
      +modal decisionState_ptr
      +proc process
      +actionMenu actionMenu_ptr
      +filesView contentPopup_ptr
      +stashView stashView_ptr
      +filesMode filesMode
    }
    layerStack "1" o-- "*" layer
    Model --> layerStack
    layer <|.. contentPopup
    layer <|.. diffView
    layer <|.. historyView
    layer <|.. blameView
    layer <|.. irebaseEditor
    layer <|.. hunkPicker
    layer <|.. bookmarkPopup
    layer <|.. commitPopup
```

The stack API (`layer_stack.go`): `pushLayer` (40), `popLayer` (50), `removeLayer`
(96, by-identity — for a window that may not be top, e.g. the diff on resize),
`layerOf[T]` (72, find the live window of a type), `topLayer` (31), `isFullScreenLayer`
(22, render-only: surface vs centered popup).

### Render is the mirror of dispatch

```mermaid
flowchart TB
    R["Model.render"] --> M{"m.modal?"} 
    M -- yes --> MR["overlay modal box over renderInterface"]
    M -- no --> PR{"m.proc?"}
    PR -- yes --> PRR["proc.render over renderInterface"]
    PR -- no --> AM{"m.actionMenu?"}
    AM -- yes --> AMR["overlay menu over renderLayers()"]
    AM -- no --> LZ{"stack non-empty OR diff?"}
    LZ -- yes --> LR["renderLayers(): walk bottom→top over layerBase()=renderInterface()"]
    LZ -- no --> BR["renderInterface() + tooltip"]
```

Render order mirrors dispatch priority: `modal → proc → actionMenu → layers/diff →
base`. `renderLayers` (`view.go:133`) walks the stack bottom-to-top; full-screen
surfaces own the frame, centered popups composite over the surface beneath. Since
1a the diff is just another layer in that walk, so `layerBase()` is simply
`renderInterface()` (the old diff special-case is gone).

---

## 2. Middle-level: the window kinds

```mermaid
classDiagram
    class contentPopup {
      lines
      sel
      typing
      query
      +update()
      +render()
      +move()
    }
    class diffView {
      rows_lockstep
      offset
      partial
      +update()
      +render()
    }
    class filesViewSM {
      filesMode
      tree_contentPopup
      preview_contentPopup
      +transitions()
    }
    class decisionState {
      req
      onResolve
      reply_chan
    }
    class conflictProcess {
      state_machine
      +update()
      +render()
    }
    class actionMenu {
      rows_from_availableActions
    }
```

> `contentPopup` is reused **four ways**: a stack popup (help/cheat-sheet), the
> files-view tree (field), the file preview (field), and switcher bodies.
> `filesViewSM` is the `filesMode` machine + its fields (not a single struct — the
> "transitions are the only mutators" property is the abstraction). `diffView`
> scrolls both columns in **lockstep** (single logical focus).

The middle-level taxonomy by placement:

| Kind | Placement | Notes |
|---|---|---|
| **`contentPopup`** | layer *and* field (both) | the workhorse list/pager: help popup (layer), files tree + preview (fields), switcher bodies |
| **`diffView`** | layer (since 1a) | side-by-side, single logical focus (lockstep scroll) |
| **surfaces** (`historyView`/`blameView`/`irebaseEditor`/`hunkPicker`) | layer | full-screen (`isFullScreenLayer`) |
| **popups** (`bookmarkPopup`/`commitPopup`/`worktreePopup`/…) | layer | centered boxes |
| **files view** (`filesMode` + transitions) | field | the most complex: internal focus split + preview overlay; state is a machine (1b) |
| **`stashView`** | field | right-column list, coexists with the base panels |
| **`decisionState`** | slot (`m.modal`) | option-list decision from an engine op |
| **`process`/`conflictProcess`** | slot (`m.proc`) | exclusive interface lock; a state machine |
| **`actionMenu`** | slot (`m.actionMenu`) | rows built by the `availableActions` registry |

> **The `contentPopup` double life** is the one cross-cutting subtlety: the same
> type is both a stack layer (help) and a `Model` field (`filesView`/`filesPreview`).
> Its `update` branches on `m.filesView == p` to know which role it is playing.

---

## 3. How high and middle affect each other

Two directions, one contract (the placement):

```mermaid
sequenceDiagram
    participant K as keypress
    participant H as HIGH (dispatch/render)
    participant M as MIDDLE (a window)
    K->>H: Model.Update(KeyMsg)
    H->>H: walk placements (slot? layer? field? base)
    H->>M: the active window's update(m, msg)
    M-->>H: (Model, Cmd) — may pushLayer / popLayer / set a field
    Note over M,H: opening another window = MIDDLE asking HIGH to place it
    H->>M: render(): renderLayers / slot overlay calls the window's render
```

- **High → middle:** the ladder picks exactly one owner and hands it the key; the
  render walk calls that window's `render`. The system never inspects window
  internals.
- **Middle → high:** a window *opens* another by choosing a placement (`pushLayer`
  for a layer, set a field, set a slot). E.g. the diff pushes a `historyView` over
  itself; a switcher pushes a compare diff; the files view sets `m.diffView`… no,
  *pushes* a diff (since 1a). Closing calls `popLayer`/`removeLayer`/`closeFilesView`.

This is why the **return target is automatic for layers** (pop reveals the parent)
but **manual for fields** (you must nil the field, and `closeFilesView` is the
single chokepoint that does it completely — Stage 1b).

---

## 4. Open-flows, ranked by frequency

The flows you hit most. Each shows: trigger → open function → placement → async
load → message populates → render. **Tier 1 are the hot paths.**

### Frequency ranking

| Tier | Window | Trigger | Placement | Open fn |
|---|---|---|---|---|
| **1 (hottest)** | **Action menu** | `.` (any context) | slot | `openActionMenu` (`action_menu.go:440`) |
| **1** | **Files view** | `l` on a commit | field | `openChangedFiles` (`files_view.go:54`) |
| **1** | **Diff** | `enter` on a file | layer | build `diffView` + `pushLayer` |
| **2** | **Switchers** | `g` / `G` | layer | `openBookmarkSwitcher` / `openShelfSwitcher` |
| **2** | **File preview** | `.`→View file (full-tree) | field | `openPreview` (`file_preview.go`) |
| **2** | **Compare** | `.`→Compare / mark | field→layer | `openCompareFiles` → diff |
| **3** | **Surfaces** | `h` / `b` | layer | `newHistoryView` / `newBlameView` + push |
| **3** | **Modal** | engine op decision | slot | `m.modal = &decisionState{}` |
| **3 (rare)** | **Process** | merge/rebase conflict | slot | `m.proc = &conflictProcess{}` |

---

### Tier 1 — Action menu (`.`)  · most-opened of all

A priority slot built by a **context-predicated registry**: the same `.` resolves
to different rows depending on the focused window/side.

```mermaid
sequenceDiagram
    participant U as user
    participant CTX as active context (diff / files / base)
    participant R as availableActions registry
    U->>CTX: "." 
    CTX->>R: availableActions(m)
    R->>R: pick rows by context<br/>(focused side, mode, row type)
    R-->>CTX: []actionRow
    CTX->>CTX: m.actionMenu = &actionMenu{rows}
    Note over CTX: slot now owns the keyboard, above the stack
    U->>CTX: ↑/↓/enter → row.run(m) · esc → m.actionMenu = nil
```

Key point: `availableActions` (`action_menu.go`) is already the
"keymap-predicated-on-focus" registry — it branches on `inContentWindow`,
`filesTreeFocused`, `filesHash`, the top layer, etc. Extending it to raw keys (not
just the menu) is the conceptual heart of the deferred routing unification.

### Tier 1 — Files view (`l`)  · the most-opened *content* window

A **field** window whose state is the `filesMode` machine (Stage 1b). Opening is a
single transition; content arrives async.

```mermaid
sequenceDiagram
    participant U as user
    participant H as updateBaseKey
    participant FV as openChangedFiles (transition)
    participant G as svc.CommitFiles (off-thread)
    U->>H: "l" on a Commits row
    H->>FV: openChangedFiles(c)
    FV->>FV: closeFilesView() → clean slate
    FV->>FV: filesMode=Changed · filesView=loading · filesReadInflight=true
    FV-->>H: (m, loadCommitFilesCmd(c))
    H->>G: CommitFiles(c.Hash)
    G-->>H: commitFilesMsg{hash, files}
    H->>H: if filesView!=nil && hash matches → filesView.lines = commitFileLines(files)
    Note over H: render: renderFilesView (left col) ‖ Commits (right col)
```

Closing is the single chokepoint `closeFilesView()` (zeroes the whole cluster);
`a` → `toggleFullTree`, `←/→/tab` → `focusTree`/`focusRight`, `.`→View file →
`openPreview` (a field overlay on the right column). See §"File tree" of the
key-routing doc for the internal focus split.

### Tier 1 — Diff (`enter` on a file)  · a LAYER since 1a

```mermaid
sequenceDiagram
    participant U as user
    participant FV as updateFilesViewKey
    participant ST as layer stack
    participant D as Differ (off-thread)
    U->>FV: "enter" on a tree row
    FV->>FV: build &diffView{loading}
    FV->>ST: pushLayer(diffView)
    FV-->>FV: (m, loadCommitDiffCmd(hash, line))
    FV->>D: diff hash:path
    D-->>FV: diffMsg{tag, view}
    FV->>FV: dv = layerOf[*diffView]; *dv = *view; loading=false  (in place — may not be top)
    Note over ST: render: renderLayers → diff full-screen over the files view beneath
    U->>ST: esc → popLayer → back to the files view (state intact)
```

The same `diffMsg` / `openPickerDiff` / `pushLayer` path serves every diff opener
(status panel, files view, history, bookmark/shelf compare) — `openPickerDiff`
(`bookmark_popup.go:434`) is the single seam for picker-launched diffs.

---

### Tier 2 — Switcher (`g` bookmark / `G` shelf)  · a centered popup LAYER

```mermaid
sequenceDiagram
    participant U as user
    participant H as any context (global g/G)
    participant ST as layer stack
    U->>H: "g"
    H->>ST: openBookmarkSwitcher → pushLayer(&bookmarkPopup{})
    H-->>H: (m, load bookmarks cmd)
    Note over ST: render: centered box composited over the surface/panels beneath
    U->>ST: enter on a pair → openPickerDiff → pushLayer(diff) OVER the switcher
    U->>ST: esc on the diff → popLayer → back to the switcher (intact)
    U->>ST: esc on the switcher → popLayer → back to whatever launched it
```

This nested push/pop (switcher → diff → back) is exactly what the unified stack
buys: each `esc` reveals the prior scene, no special-casing.

### Tier 2 — File preview / Compare

- **Preview** (`.`→View file in full-tree): `openPreview` sets `m.filesPreview`
  (a second `contentPopup` field) + focuses right; it is an *overlay within the
  files-view field*, not a stack layer. `esc` → `closePreview` (back to the tree).
- **Compare** (`.`→Compare / mark two): `openCompareFiles` (a files-view transition
  → `filesMode=Compare`); `enter` on a file then pushes a compare **diff layer**.

---

### Tier 3 — Surfaces (`h` history / `b` blame)  · full-screen LAYERS

```mermaid
sequenceDiagram
    participant U as user
    participant CTX as files view / diff
    participant ST as layer stack
    U->>CTX: "h" on a file (or from the diff)
    CTX->>ST: pushLayer(newHistoryView(ctx))
    CTX-->>CTX: (m, loadHistoryListCmd)
    Note over ST: isFullScreenLayer → owns the whole frame (ignores backdrop)
    U->>ST: enter on a commit → push a diff layer over history
    U->>ST: esc → popLayer → back to history; esc → back to the opener
```

### Tier 3 — Modal (engine-op decision)  · top-priority SLOT

```mermaid
sequenceDiagram
    participant OP as engine Operation
    participant H as Model.Update
    participant U as user
    OP->>H: emits DecisionNeeded (option list)
    H->>H: m.modal = &decisionState{req, onResolve / reply}
    Note over H: modal owns ALL input — above the stack and everything else
    U->>H: ↑/↓ + enter → chosen option
    H->>OP: onResolve(m, opt) or reply channel
    H->>H: m.modal = nil → reveal whatever was beneath, untouched
```

The modal sits at the very top because an operation may raise a decision *while a
process is running* — the decision must beat the process. Decisions are
**option-lists only** (no free text mid-flight), the same contract the CLI/MCP
deciders satisfy.

### Tier 3 — Process (conflict resolution)  · exclusive-lock SLOT

```mermaid
stateDiagram-v2
    [*] --> Listing: merge/rebase hits a conflict → m.proc set
    Listing --> Picking: choose a file
    Picking --> Working: resolve (ours/theirs/edit)
    Working --> Reporting: all resolved
    Reporting --> [*]: m.proc = nil (lock released)
    note right of Listing
      while m.proc != nil the process owns
      ALL input; every other window/command
      below is unreachable (the interface lock)
    end note
```

---

## 5. Choosing a placement for a NEW window (the decision guide)

When you add a window, the placement is the only architectural decision. (The
`adding-tui-windows` skill is the build-time companion to this.)

```mermaid
flowchart TD
    Q0["new window"] --> Q1{"must it block ALL input<br/>until resolved?"}
    Q1 -- "yes, a decision" --> MODAL["→ modal slot (decisionState)"]
    Q1 -- "yes, a long job" --> PROC["→ process slot"]
    Q1 -- no --> Q2{"opens OVER content and<br/>returns to it on close?"}
    Q2 -- yes --> Q3{"single keyboard owner<br/>(no internal focus split)?"}
    Q3 -- yes --> LAYER["→ stack LAYER (push/pop) ✅ default"]
    Q3 -- "no, two co-resident<br/>focus-split panes" --> DEFER["→ needs the deferred<br/>pane primitive (Stage 3) — spike it here"]
    Q2 -- "no, it's part of the<br/>left/right column layout" --> FIELD["→ Model field (like filesView/stashView)<br/>— last resort; entangles with base layout"]
    style LAYER fill:#cde,stroke:#36c
    style DEFER fill:#fdd,stroke:#c33
```

**Default to a stack layer.** Slots are only for true interrupts (decision / lock).
Fields are a legacy shape (files view, stash) that the unification work is
gradually retiring — prefer not to add new ones. A genuinely *focus-split* surface
(two independently-focused panes) is the trigger for the deferred pane primitive;
build that primitive on the new self-contained surface, not by retrofitting the
files view.

---

## 6. Summary

- **High-level** owns *relationships* (z-order, input ownership, return target) via
  **three placements**: priority slot, stack layer, `Model` field. **Middle-level**
  owns *behavior* (each window's state + `update` + `render`). The placement is the
  whole contract between them.
- **The stack is the unifying direction.** 1a moved the diff onto it; the slots stay
  out by design; the field windows (files view, stash) + the base panel layout are
  the remaining un-unified region (deferred Stage 3, gated on a second focus-split
  surface).
- **Hot open-paths:** `.` action menu (registry slot), `l` files view (field
  transition + async), `enter` diff (push layer + async-in-place). These three are
  worth knowing cold; everything else is a variation on push-a-layer-then-load.
