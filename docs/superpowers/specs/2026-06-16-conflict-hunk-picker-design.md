# Conflict hunk picker — design

**Date:** 2026-06-16
**Status:** approved (brainstorm), pending spec review
**Sub-project of:** GitKraken-style hunk/line selection. This is sub-project 1
of 2 (picker + conflict resolution). Sub-project 2 (hunk/line **staging**)
reuses the pure core built here and is specced separately.

## Goal

Resolve a merge/rebase conflict at the **region and line level** in the TUI,
GitKraken-style: for each conflict region pick the whole **current** side, the
whole **incoming** side, or assemble the region **line by line** from either
side — with the picked lines landing in the result in the exact order they were
toggled on. The existing whole-file conflict resolver (`x` popup) stays as the
fast path; this is what you get when you drill into a both-modified file.

## Terminology: current / incoming (not ours/theirs)

The two pickable sides are **current** and **incoming**:

- **merge B into A** — git stage `:2:` = A, `:3:` = B ⇒ A is *current*, B is *incoming*.
- **rebase B onto A** — git checks out A and replays B, so stage `:2:` = A,
  `:3:` = B ⇒ A is *current*, B is *incoming*.

So **current = git `:2:` (its "ours"), incoming = git `:3:` (its "theirs")** in
*both* cases — the mapping never flips. "ours/theirs" invert against intuition
during a rebase; "current/incoming" stay stable, so they are the more correct
labels. This spec also **relabels the existing whole-file conflict popup** (the
current `o` keep-ours / `t` keep-theirs entries) to current/incoming so the
vocabulary is consistent across both resolvers.

## Architecture

Mirrors the existing `textdiff` (pure) + `diffView` (TUI) split:

1. **`internal/hunkpick`** — pure, dependency-free (no git, no TUI): the
   decision model, the conflict-marker parser, and byte-faithful assembly.
   **This package is reused by the hunk-staging sub-project** (which will add a
   different `Doc` constructor; the decision model and assembly are identical).
2. **`internal/tui/conflict_picker.go`** — the interactive view-stack surface.
3. **`internal/engine/conflict_hunks.go`** — a thin op that writes the assembled
   bytes to the working tree and stages the file.
4. One new git verb — `WriteWorktreeFile` — on `*git.Repo`.

Rejected alternatives: (B) re-deriving regions by reading the three index blobs
(`:1/:2/:3`) and running `textdiff.Compare` — more machinery, and conflicts
already carry explicit regions from the markers; (C) shelling out to a mergetool
— abandons the "we assemble the result" model.

## Section 1 — the pure core (`internal/hunkpick`)

```go
package hunkpick

type Side int
const ( Current Side = iota; Incoming )

type Mode int
const ( Undecided Mode = iota; TakeCurrent; TakeIncoming; LineByLine )

// Pick is one line chosen in line-by-line mode: side + index into that side.
type Pick struct { Side Side; Line int }

// Block is one decidable region: the two candidate versions + the decision.
type Block struct {
    Current  []string
    Incoming []string
    Mode     Mode
    Picks    []Pick   // ordered; only meaningful when Mode == LineByLine
}

// Item is exactly one of: literal passthrough text, or a decidable block.
type Item struct {
    Literal []string  // non-nil ⇒ passthrough
    Block   *Block    // non-nil ⇒ region
}

// Doc is the whole file as an ordered mix of passthrough text and blocks.
type Doc struct {
    Items        []Item
    FinalNewline bool   // preserve the file's exact trailing-newline state
}

func ParseConflict(content []byte) (*Doc, error)
func (d *Doc) Resolved() (out []byte, ok bool)   // ok=false if any block Undecided
func (d *Doc) Pending() int                       // count of Undecided blocks
func (d *Doc) SetAll(m Mode)                       // bulk take-all
func (b *Block) ToggleLine(s Side, line int)        // append if absent, remove if present (preserves order)
```

Behaviors:

- **`ParseConflict`** turns the conflicted working-tree file into ordered
  `Item`s: text outside markers → `Literal`; each `<<<<<<< / ======= />>>>>>>`
  region → a `Block{Current, Incoming}`. diff3 `||||||| base` lines are skipped
  in v1. Unbalanced/malformed markers → error.
- **`ToggleLine`** is the line-by-line mechanism: toggling a line *on* appends a
  `Pick` to the end (result follows toggle order); toggling *off* removes that
  pick, the rest keep order. Setting `Mode = LineByLine` starts from empty
  `Picks` (an empty result you build up).
- **`Resolved`** walks items: literal → emit verbatim; block → emit `Current`,
  `Incoming`, or the ordered `Picks`. Byte-faithful, preserving `FinalNewline`.
- **`SetAll`** backs the file-level take-all-current / take-all-incoming keys.

## Section 2 — the picker surface (TUI)

A view-stack surface (`*conflictPicker`, pointer receiver, owns the whole screen
and renders its own footer inline, like the irebase editor). Entered by pressing
`enter` on a **both-modified text file** in the existing `x` conflict popup.
Binary and modify/delete conflicts keep the whole-file actions only (nothing to
line-pick).

Layout: the file scrolls top-to-bottom; passthrough text is dimmed context (long
runs collapse with a fold marker, like the diff view's partial mode); each
conflict region renders side-by-side. A **2D cursor** selects a side and a line;
the region under it is the focused region. A line-by-line region shows a live
**result preview** in pick order.

Focus / cursor markers (three levels):

1. **Focused region** — accent border + `▶` in its header (`▶ region 2/3`);
   unfocused regions get a dim border.
2. **Focused side** — the column header of the cursor's side (current *or*
   incoming) is lit, the other dimmed — so `space` is unambiguous.
3. **Cursor line** — reverse-video (`selectedRow` style) with a `>` gutter.

Each region also shows a resolution badge: `✓ current` / `✓ incoming` /
`line-by-line` / `· undecided`. The header shows counts: `3 regions · 2 left`.

```
Resolve conflicts: src/app.go          3 regions · 2 left
──────────────────────────────────────────────────────────
   1  package app
   ⋮  (4 unchanged lines)
   6  func handler() {
 ▶ region 2/3 ───────────────────────── line-by-line ─┐
 │  current                  ║  INCOMING   (focused)   │
 │  [x] a := 1               ║  [ ] a := 2             │
 │  [ ] b := 2               ║ >[x] b := 9             │
 │  result:  a := 1 / b := 9                           │
 └─────────────────────────────────────────────────────┘
```

Keys (the surface owns all of them):

| Key | Action |
|-----|--------|
| `←`/`→` | switch the focused **side** (current ↔ incoming column) within the focused region; clamps the cursor line when sides differ in length |
| `↑`/`↓` `j`/`k` | move within the focused side's lines; past the top/bottom steps to the adjacent region on the same side |
| `n`/`p` | jump to next / previous region |
| `space` | toggle the cursor's line in/out of the result — flips the region into **line-by-line** and appends/removes in pick order |
| `c` / `i` | resolve the focused region to whole **current** / whole **incoming** |
| `C` / `I` | **take all current** / **take all incoming** (every region at once) |
| `enter` | apply when nothing is pending; otherwise jump to the next undecided region and report "N left" |
| `esc` | cancel — back to the conflict popup, file untouched |
| `ctrl+c` | quit |

New keys land in `help.go` under a "Conflict hunk picker" section. The footer is
rendered inline by the surface (a stack surface owns the screen, so the global
`footerLine` is not reached — same pattern as the irebase editor).

## Section 3 — engine op, wiring, edge cases, testing

**Engine op** (`internal/engine/conflict_hunks.go`):

```go
type ResolveConflictHunks struct { Path string; Content []byte }
```

`Run` writes `Content` to the working-tree file via the new verb, then
`StagePaths([Path])` so the unmerged index entry clears. Runs under a TreeWrite
reservation; emits `Progress`/`Done`.

**New git verb** — `WriteWorktreeFile(ctx, path string, content []byte) error`
on `*git.Repo` (it knows the repo top-level), added to the `GitOps` interface
and the compile-time assertion. Not a git invocation — a filesystem write — but
kept as an op/verb so it serializes through repogate like other mutations.

**Wiring** — in the `x` conflict popup, `enter` on a both-modified text file
reads the conflicted working-tree blob (a `domain` read), runs
`hunkpick.ParseConflict`, and pushes `*conflictPicker`. Parse error or a
non-both-modified file → status message, stay on whole-file actions. On `enter`
with zero pending, the surface hands `Doc.Resolved()` bytes to the op; control
returns to the conflict popup, which reopens on the now-smaller conflict set
(its existing reopen-until-clean behavior).

**Edge cases:**

- Malformed/unbalanced markers → parse error → message, no picker.
- Region with one empty side → handled (whole-current may be empty = "take
  nothing from current").
- Byte-faithfulness: split on `\n`, preserve the file's exact trailing-newline
  state via `Doc.FinalNewline`.
- Concurrency: the op takes TreeWrite, serializing with other mutations.

**Testing:**

- `hunkpick` pure tests: parse (2-way, diff3, multiple regions, malformed,
  no-final-newline), assembly orderings, `ToggleLine` add/remove order,
  `SetAll`.
- Surface tests: 2D cursor movement (incl. side switch with unequal lengths),
  space-toggle flips to line-by-line, `c`/`i`/`C`/`I`, the pending-gate on
  `enter`, routing through `Model.Update`.
- Engine op test against real git in a `t.TempDir()`: real conflict → resolve →
  the index entry for that path is clean.
- No e2e scenario — the harness runs the CLI in-process with no TUI surface
  (same constraint as the irebase editor).

## Out of scope (v1)

- The merge **base** as a third pick-from column (used only to detect conflicts).
- CRLF normalization (operate on `\n`-split lines; watch-item).
- CLI / MCP hunk resolution (the whole-file CLI path stays).
- Hunk/line **staging** — the next sub-project, which reuses `internal/hunkpick`.
