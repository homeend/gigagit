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

**Width** is **fixed** at `identW = 16` (the user picked "wider ~16"). A fixed
column avoids reflow jitter as commits page in (a dynamic max-width would shift
the column and every subject when a longer name loads mid-scroll). Names wider
than 16 trim with `…`; the token is left-padded/truncated to 16 so the subject
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

- **Lineage** rows (`tip == false`, `name != ""`): the identity column's rune
  range is **dimmed** (gray foreground, e.g. `"240"`). Width preserved.
- **Tip** rows: default (bright) — no extra styling.
- The existing lane-color `●` node still colors. Both the dot color and the
  ident dim are emitted by a **single** `commitLineDecorator` that walks the
  ORIGINAL runes once and wraps the dot rune + the ident range in their styles
  in one pass. (Composing two independent decorators by re-indexing
  `[]rune(visible)` is unsafe: the first one inserts ANSI escapes that shift the
  second's column math — and a zero-style `Render` can strip inner escapes. One
  pass over the unstyled string avoids both.) It is hscroll-aware like the
  current dot decorator.

The dim coloring applies in ALL display states (it is not gated on
`commitGraphOn()` the way the lane-dot color is), so lineage stays gray under
filter/sort too.

## Tooltip when the name is trimmed

The existing reveal-tooltip fires only when the whole rendered row exceeds the
panel width (`tooltip()` early-returns unless the row `rowTruncated`), so a
trimmed identity inside an otherwise-fitting row would not trigger it — and even
when the row IS overall-truncated, the tooltip shows `rows[sel]`, which still
contains the *trimmed* name. So generalize the reveal: a panel may supply an
optional parallel **`fullRows []string`**; the tooltip shows `fullRows[sel]`
whenever it differs from the rendered row (and fires when EITHER the row is
truncated OR `fullRows[sel] != rows[sel]`). The Commits panel supplies a
`fullRows` whose identity token is the **untrimmed** branch name (rest of the
row identical), so the strip reveals the full name. This also fixes the
both-truncated precedence case in one move.

## Status bar

`commitBranchHint()` (status line, shown when the Commits panel is focused)
gains the selected commit's **short id**: `⎇ <source-branch> · # <a1b2c3d>`.
The `⎇ <branch>` hint stays (it is the authoritative readout for the selected
row); the id is appended. (Copying the full id is still available via the
`.`-menu Copy commit id.)

## Search haystack (don't silently drop hash filtering)

The commit filter matches `panelList.Row(i)` — which is also the display text.
Two changes to that string would otherwise regress search: the **hash leaves the
row** (commit-id-prefix filtering would vanish — a capability removal, not a
degradation) and the **name is trimmed** (filter-by-full-branch-name >16 chars
would break; the full name used to live in the pill). So **decouple the
haystack from display**: add an optional `Haystack(i) string` method; `panelView`
prefers it over `Row(i)` when present. `commitList.Haystack(i)` returns the
**full hash + full (untrimmed) branch name + subject** (lowercased match as
today), so both id-prefix and full-branch-name filtering keep working even
though neither is shown verbatim in the row. Other panels don't implement it →
unchanged.

## Single-sourcing

The row STRING contains the plain trimmed identity name + subject; color is the
only thing added at render time (never baked in). The identity name, its `tip`
flag, the trimmed display token, the untrimmed `fullRows` token, and the
`Haystack` text are all computed once from `(Refs, Source, Hash, Subject)` in
one builder, so display, the decorator, the tooltip, and the filter share one
source and cannot drift.

## Out of scope (v1)

Remote/tag identity in the column (local branches only, as today); a `+N`
marker for multi-tip commits (the extra branches keep their pills); coloring the
tip name by lane; configurable column width.
