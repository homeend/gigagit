# Diff-view `N`/`P` file-stepping from the change boundary

**Date:** 2026-06-30
**Surface:** `internal/tui` diff view (full-screen diff layer)

## Problem

In the diff view (opened from the files-view tree, the Status panel, or the
Staged panel) the user navigates changes within a file with `n`/`p`. Reaching
the last/first change already supports two boundary gestures:

- `n`/`p` pressed **again** at the boundary wraps around (last→first, first→last).
- `End`/`Home` at the file's bottom/top steps to the next/previous **file** in
  the source list (after a priming press).

Stepping to the next/previous file is therefore only discoverable via
`End`/`Home`, and only from the scroll edge. The user wants a change-relative
way to move between files: when sitting on the **last** change they should be
prompted that capital `N` jumps to the **next file**, and on the **first**
change capital `P` jumps to the **previous file**.

## Behavior

Scope: the diff view only, and only when it was opened from a stepping source
list (`diffNav` is `diffNavTree` / `diffNavStatus` / `diffNavStaged`; a
bookmark/shelf picker compare has `diffNavNone` and no file list).

### At the last change block (`v.cur == len(v.dispBlocks)-1`)

| Keys      | Action                                   | Status   |
|-----------|------------------------------------------|----------|
| `n` `n`   | wrap to the first change                 | existing |
| `N` `N`   | step to the **next file** in the list    | new      |

`N` is **boundary-gated**: pressed on any block other than the last, it does
nothing (and disarms any pending prime — consistent with every other key).

### At the first change block (`v.cur == 0`)

| Keys      | Action                                   | Status   |
|-----------|------------------------------------------|----------|
| `p` `p`   | wrap to the last change                  | existing |
| `P` `P`   | step to the **previous file** in the list| new      |

A single-block file has `v.cur == 0` as both first and last, so both `NN` and
`PP` are available there (no wrap, since there is nothing to wrap to).

### Double-press mechanic

`N`/`P` reuse the **existing `fileArm` prime** that `End`/`Home` already use
(both gestures mean "go to the next/previous file"). First press arms
`fileArmNext`/`fileArmPrev` and shows a cue; the second same-direction press
performs `stepDiffFile(±1)`. Any other key disarms (the arm is captured and
cleared at the top of `updateDiffViewKey`, exactly like the `n`/`p` wrap arm).

Because the arm is shared with `End`/`Home`, a prime from one key can be
confirmed by the other (e.g. `End` at the bottom then `N`) — acceptable and
consistent, since both resolve to the same next-file step.

If no next/previous file exists, the first `N`/`P` press posts the transient
`▸ no next file` / `▸ no previous file` notice instead of arming (mirrors the
existing `End`/`Home` boundary handling).

## The "advertise both" hint

A new **proactive** bottom-left notice is shown whenever the focused block is a
boundary block — so the user is "prompted" simply by being on the last/first
change, without needing a priming press:

- last block:  `▸ nn → top · NN → next file`
- first block: `▸ pp → bottom · PP → prev file`
- single block: `▸ PP → prev file · NN → next file`

Segments are conditional:

- the wrap segment (`nn → top` / `pp → bottom`) appears only when
  `len(v.dispBlocks) > 1` (a wrap target exists);
- the file segment (`NN → next file` / `PP → prev file`) appears only when
  `peekDiffFile(±1)` reports a neighbor (and thus never when `diffNav` is
  `diffNavNone`).

If neither segment applies, no proactive cue is shown.

### Notice priority (`withDiffFileNotice`)

1. transient `m.diffNotice` (file-arrival `▸ now: <path>`, `▸ no next file`)
2. `fileArmCue(fileArm)` — shown while a file-step is primed
3. **new** proactive boundary cue (`boundaryCue`)

The existing top-right wrap cue (`wrapCue`, shown only while `wrapArm` is
primed) is unchanged.

## Implementation outline

- `diff_view.go` — add `case "N"` and `case "P"` to `updateDiffViewKey`,
  boundary-gated on `v.cur`, reusing the captured `fileArmed` and the
  `peekDiffFile`/`stepDiffFile`/`fileArm` machinery (parallel to the existing
  `end`/`home` cases).
- `diff_filenav.go` — add a `boundaryCue(...)` helper that builds the proactive
  hint string; reword `fileArmCue` to advertise both keys
  (`▸ N/end again → next file`, `▸ P/home again → previous file`).
- `diff_render.go` — extend `withDiffFileNotice`'s fallback chain to fall
  through to `boundaryCue` after `fileArmCue`.
- `footer.go` / `help.go` — advertise `N`/`P` next/prev file in the diff-view
  footer hint and help text.

No new model/state fields: `fileArm`, `diffNav`, `stepDiffFile`, `peekDiffFile`
are all reused as-is.

## Testing

Table/unit tests in `internal/tui` (real-model `Update` with synthetic key
messages), covering:

- `N` on the last block: first press arms (no file change), second press opens
  the next file's diff; `diffNav` carried through.
- `P` on the first block mirrors to the previous file.
- `N` on a non-last block is inert (no arm, no step).
- `N` with no next file posts `▸ no next file` rather than stepping.
- the proactive `boundaryCue` string renders the right segments for: last block,
  first block, single block, and `diffNavNone` (no file segments).
- the existing `n`/`p` wrap and `End`/`Home` file-step behaviors are unchanged
  (regression).

## Out of scope

- Making `N`/`P` work mid-file (explicitly boundary-gated per design).
- Changing the top-right wrap cue or the `n`/`p` wrap semantics.
- Any source list other than tree / Status / Staged.
