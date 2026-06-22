# Windowing: four bugs, one root cause (no single z-order / window-state SSoT)

**Status:** Architecture note / problem statement (input for a follow-up spec).
**Date:** 2026-06-22.
**Companion to:** `2026-06-22-key-routing-by-context.md` and
`2026-06-22-split-layer-windowing-investigation.md` (both currently on branch
`docs/windowing-investigation-verdict`), and the "Future (footnote)" of
`2026-06-20-layer-stack-unify-design.md`.
**All line citations are `internal/tui/` on `main`.**

---

## Thesis

A family of TUI bugs — the history/blame `.`-menu leak, mouse-wheel hitting a
hidden surface, `esc` from a full-screen diff not returning to the surface it was
opened from, and the files-view "reset choreography" bug class — are **the same
bug wearing different clothes**. The shared root cause:

> "How windows are affected by each other" (who is on top, who owns the keyboard,
> who owns the mouse, what is revealed when one closes, what a menu's contents
> reflect) is **not derived from one ordered structure**. It is **re-derived
> independently** by routing, rendering, mouse handling, the action-menu builder,
> and ad-hoc state resets — and those independent copies **drift**.

There is no single source of truth for **z-order** (which window is "in front"),
and no single source of truth for a **window's state**. Every place that needs
either fact computes it locally. The bugs live exactly where two of those local
computations disagree.

---

## Evidence: the same surfaces are ordered differently by each subsystem

The action menu, the layer stack, and the diff view are three "windows". Each
subsystem hard-codes its **own** order for them:

| Subsystem | Order (front → back) | Cite |
|---|---|---|
| **Keyboard dispatch** | modal → proc → **actionMenu → stack → diffView** → filterTyping → filesView → stash → base | `model.go:429,461,468,478,484,619,632` |
| **Mouse dispatch** | modal → proc → **stack → diffView → actionMenu** → filesView | `mouse.go` (modal/proc/topLayer:23/diffView:31/actionMenu:37/filesView:43) |
| **Render** | modal → **diffView (`view.go:119`) → stack (`view.go:182`)** | `view.go:119,182` |

Two concrete contradictions fall straight out of this table:

1. **diffView vs. the layer stack:** render draws `diffView` *in front of* the
   stack (`view.go:119` before `:182`), but keyboard and mouse route to the
   **stack first** (`model.go:478` before `:484`; `mouse.go:23` before `:31`). So
   a diff drawn "on top of" a layer has its keys and wheel stolen by the layer
   beneath it. This is why a full-screen diff **cannot** sit over the history
   layer — and why `clearLayers` exists (see below).
2. **actionMenu vs. the stack:** keyboard puts `actionMenu` **before** the stack
   (`model.go:468` < `:478`); mouse puts the stack **before** `actionMenu`
   (`mouse.go:23` < `:37`). Same two windows, opposite order in two subsystems.

`clearLayers` (`layer_stack.go`) documents the workaround in its own comment:

> "the diff view is the render base the stack walks over, so a lingering layer
> would composite on top and hide nothing — clearing makes the diff the sole
> visible surface."

I.e. the diff is treated as the **base** the stack renders *over*, yet is routed
**below** the stack. The only way to make it both visible and input-owning is to
**evict the entire stack** (`clearLayers`) — losing the very relationship ("I was
opened over history") that a return key would need.

---

## The bug family (all the same shape)

| Bug | Which two facts disagreed / what was missing |
|---|---|
| **`esc` from history→full-diff lands on base/file-tree, not history** (`history_view.go` enter → `openPickerDiff` → `clearLayers`) | Render says diff-on-top; keyboard says stack-on-top → diff can't sit over the history layer → `clearLayers` erases "I came from history" → `esc` has no return target and falls to the next surviving field (`filesView`). |
| **`.`-menu showed Commits actions in history/blame** (fixed in `action_menu.go`, the `frontIsFilesView` guard) | The menu builder re-derived "what is front" from the underlying `filesView`/`filesHash` instead of the top surface, so a layer on top inherited the files-view/commit rows. |
| **Mouse wheel scrolls the hidden list** (the trap avoided in that same fix) | Mouse orders stack-before-diff (`mouse.go:23<31`), so a diff over a layer sends the wheel to the hidden layer. |
| **Files-view "reset choreography"** (verdict's primary finding) | ~10 scattered `Model` scalars (`filesHash`, `filesAllFiles`, `filesPreview`, `filesCompare`, … `model.go:58-70`) hand-reset at every open/close/mode-switch site → no single window-state object → transitions desync. |
| **Files-view internal focus split + preview overlay** | `filesTreeFocused` is a per-keypress flag re-checked in nearly every case (`files_view.go`), and `filesPreview` pre-empts keys — focus-within-a-window with no structural "pane" notion, re-derived case by case. |

Every row is the same failure: a **relationship between windows** (on-top-of /
owns-input / owns-mouse / returns-to / menu-reflects / contains) that should be
**one declared fact** is instead **recomputed independently** and the copies drift.

---

## What "stitching windows / how they affect each other" decomposes into

To compose (stitch) windows you need each window to declare, and one ordered
structure to hold:

1. **Stacking (z-order):** its position, where "in front of" means *one* thing for
   keyboard, mouse, render, and return-target.
2. **Occlusion:** full-screen surface (hides what's below) vs. popup (composites
   over the backdrop). Today this is `isFullScreenLayer` (`layer_stack.go:22`,
   listing `historyView, blameView, irebaseEditor, hunkPicker`) — but **only
   render consults it**; input has its own separate ladder.
3. **Input policy:** owns all keys/mouse (popup/surface) vs. pass-through
   (panels). Today this is implicit in the hard-coded rung order, not a per-window
   property.
4. **Return target:** what is revealed on close. Today: `popLayer` (correct, for
   stack members) vs. `clearLayers`/field-nil (lossy, for off-stack windows).

The reason these are not expressible today is that **only some windows live on the
one stack** (history, blame, irebase, hunkPicker), while **`diffView`, `filesView`,
`stashView` are sibling `Model` fields outside it**, and `modal`/`proc`/`actionMenu`
are yet other slots. With no common structure, every relationship is hand-coded
pairwise and drifts.

Note the key asymmetry: **the diff view is the only full-screen, single-focus
surface that is a field instead of a stack layer.** Its peers
(`historyView/blameView/irebaseEditor/hunkPicker`) are already layers. The diff is
a historical holdout — not a focus-split surface, so it needs no new "pane"
machinery to join the stack.

---

## How the fixes ladder toward one z-order (and where the verdict landed)

The investigation verdict (`2026-06-22-split-layer-windowing-investigation.md`)
**deferred the full scene-stack/tiling framework** because its *focus-split* half
has exactly one client (the files view) and no second one on the horizon — and the
diff is explicitly a **bad** proving ground for it (lockstep scroll = single
logical focus, `diff_view.go`). That deferral is about the focus-split half, **not**
the single-z-order half. The progression:

1. **`filesViewState` extraction** (verdict Phase 1, build now): a single source of
   truth for *one window's state* — kills the reset-desync class. Independent of
   any z-order decision.
2. **Promote `diffView` to a layer** (the `layer-stack-unify` "Future (footnote)";
   the change **this `esc` bug forces**): put the lone holdout full-screen surface
   onto the **one** stack so its z-order/input/mouse/render/return finally agree.
   `enter` in history → `pushLayer(diff)` over history; `esc` → `popLayer` →
   history revealed, state intact. **Retires** `clearLayers`, the `frontIsFilesView`
   menu guard, and the `diffNav` field — all of which exist *only* to compensate for
   the diff being off-stack. No focus-split machinery needed (single focus).
3. **Full scene-stack** (deferred): every window on one stack with declared
   occlusion + input policy, so "how windows affect each other" is *read from the
   stack*, not re-derived. Parked only on the focus-split half's lack of a second
   client — not because the single-z-order direction is wrong.

---

## Concrete next step (proposed): promote `diffView` from a field to a layer

Bounded, mechanical, single-focus — does **not** require the deferred tiling work.

**Touch points (everything that special-cases the `m.diffView` field today):**
- Routing: remove the `diffView` rung from `model.go` and `mouse.go`; the diff
  becomes a normal top-layer (its `update` already exists as `updateDiffViewKey`,
  `diff_view.go:490`; wrap as a `layer`).
- Render: drop `view.go:119`; the diff renders through the layer walk like its
  peers; add it to `isFullScreenLayer`.
- Async: `diffMsg` handler (`model.go:205`) currently sets `m.diffView`; retarget to
  the diff layer on the stack (tag-gated as today).
- `esc`: `updateDiffViewKey`'s `m.diffView=nil` (`diff_view.go:603`) → `popLayer`.
- Open sites (set `m.diffView` + `m.diffTag` today): `diff_view.go` status/staged
  (`openStatusDiff` ~`:368`) and `loadCommitDiffCmd`; `history_view.go` enter;
  `bookmark_popup.go`/`bookmark_compare.go`/`shelf_actions.go` via `openPickerDiff`
  (`bookmark_popup.go:436`). Each `pushLayer(diffLayer)` instead of setting the field.
- Retire: `clearLayers` (`layer_stack.go`), the `frontIsFilesView` branch in
  `availableActions` (`action_menu.go`), and `diffNav` if it no longer earns its keep.

**Acceptance (the bugs this closes):**
- `enter` on a history commit → full-screen diff; `esc` → back to the **history
  list** with selection intact.
- The diff's `.` menu reflects the diff (no underlying files-view/commit rows);
  mouse wheel scrolls the diff, never a hidden surface.
- Keyboard / mouse / render agree on which window is in front in every case
  (no subsystem-specific ordering for the diff).

**Risk:** broad (every diff-open site) but mechanical; single-focus so no new
abstraction. Land alongside (not blocked on) the `filesViewState` extraction.
