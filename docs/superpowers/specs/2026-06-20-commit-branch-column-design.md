# Commit list: branch-name identity column (replaces the hash) — design

## Goal

Make each commit's branch membership obvious at a glance in the Commits panel:
the left column (today a 7-char commit id) becomes a **branch-name column** —
**bright** when the commit is that branch's tip ("the last commit for a given
branch"), **grayed** when the commit merely belongs to that branch's lineage.
The commit id leaves the row and appears in the **status bar** for the selected
commit. Long names trim with `…` and a tooltip reveals the full name.

## Current state

`commitRows()` builds each row as `<7-hash> <ref-pills><subject>` (graph mode
prefixes the lane glyphs; list mode prefixes `● `). The `‹*branch›`/`‹branch›`
ref pills (from `commitRefLabels`) render only on a branch's tip (a branch ref
decorates only its tip). `model.Commit` already carries `Refs []Ref` (with
`RefLocal` + `Head`) and `Source string` (the branch the commit was reached from
in the walk, via `git log --source`/`%S`). Color is applied at render time via
the `rowDecorator` hook (never baked into the row string, which feeds the filter
haystack and tooltip) — see `commitDotDecorator`.

## The identity token (replaces the hash)

Per commit, compute `(name string, tip bool)`:
- **Tip** — the commit has ≥1 local branch ref (`RefLocal`): `name` = the
  preferred branch (the current/`Head` branch if it is among them, else the
  first `RefLocal`), `tip = true`. The current branch is prefixed with `*`.
- **Lineage** — no local ref: `name` = `Source` (the branch it belongs to),
  `tip = false`.
- **Neither** (no ref, empty Source — e.g. detached walks): `name = ""`. The
  column is blank (the id is intentionally dropped, per the request).

**Width** is dynamic, capped: `identW = min(16, max(displayed name widths))`,
floor a small minimum so the column never collapses. Names wider than `identW`
trim with `…`. The token is left-padded/truncated to `identW` so the subject
column stays aligned. This replaces the `h[:7]` token in BOTH graph and list
mode. The lane glyphs (graph mode) and `● ` (list mode) still precede it.

The `‹branch›` ref **pills are dropped** — the bright name in the column now
carries that information (the request reframed the column to always show a
branch, bright/gray). A commit that is the tip of **multiple** branches shows
the preferred one in the column and the **remaining** branches still render as
pills (no info loss for the rare multi-tip case).

## Coloring (render-time decorator, not in the row string)

Following the lane-color constraint (color never in the row string; applied
post-slice by a `rowDecorator`, proven by an end-to-end `renderPanel` test with
`termenv.TrueColor` forced):

- **Lineage** rows (`tip == false`, `name != ""`): a decorator **dims** the
  identity column's rune range (gray foreground, e.g. `"240"`). Width preserved.
- **Tip** rows: default (bright) — no extra styling.
- The existing lane-color `●` decorator still applies; when a row needs both,
  the two decorators compose into one (chained) `rowDecorator`.

The dim coloring applies in ALL display states (it is not gated on
`commitGraphOn()` the way the lane-dot color is), so lineage stays gray under
filter/sort too.

## Tooltip when the name is trimmed

The existing reveal-tooltip fires only when the whole rendered row exceeds the
panel width; a trimmed identity inside an otherwise-fitting row would not
trigger it. So `tooltip()` gains a Commits-panel case: when the selected
commit's identity name is **trimmed** in the column, the tooltip shows the
**full** branch name (a one-line strip), reusing the same overlay geometry.
Tracked via a parallel `identFull []string` (full untrimmed name per backing
commit) computed alongside the rows.

## Status bar

`commitBranchHint()` (status line, shown when the Commits panel is focused)
gains the selected commit's **short id**: `⎇ <source-branch> · # <a1b2c3d>`.
The `⎇ <branch>` hint stays (it is the authoritative readout for the selected
row); the id is appended. (Copying the full id is still available via the
`.`-menu Copy commit id.)

## Single-sourcing

The row STRING contains the plain trimmed identity name + subject (so filtering
the Commits panel by a branch name works, and the tooltip/width calc see real
text). Color is the only thing added at render time. The identity name and its
`tip`/trim flags are computed once into parallel slices
(`commitIdent`/`identFull`) cached like `commitGraphRows`, so display, the dim
decorator, and the tooltip share one source and cannot drift.

## Out of scope (v1)

Remote/tag identity in the column (local branches only, as today); a `+N`
marker for multi-tip commits (the extra branches keep their pills); coloring the
tip name by lane; configurable column width.
