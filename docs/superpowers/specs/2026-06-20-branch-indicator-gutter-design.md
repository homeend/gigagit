# Dynamic branch-indicator gutter — design

**Feature 1 of the commit-ops pipeline backlog.** Small, self-contained TUI
change to the Branches panel.

## Problem

`branchRows()` (`internal/tui/view.go`) appends the selected-set/solo marker `◉`
on the **right** of each branch row (`row += " ◉"`). In a narrow Branches panel
(cutoff display mode truncates the right), `◉` is the first thing cut off — so
the user can't see which branch is soloed/selected. The head marker `*` is on the
left and stays visible; the set marker should too.

## Goal

Render the branch status indicators in a **left gutter** before the branch name,
with a **dynamic width** equal to the number of indicator *types* currently in
play. Indicators never get truncated; the common case keeps today's tight indent.

## Indicators

Two types today, ordered left-to-right in the gutter:

1. **set** — `◉` when the branch is in `commitScopeBranches` (soloed / in the
   Commits-feed selected set), else blank.
2. **head** — `*` when `b.IsHead`, else blank.

A type's column is **in play** iff *some* branch in the current list triggers it
(its glyph would be non-blank for at least one branch). The gutter contains one
column per in-play type, in the order above, followed by a single space before
the name. Width therefore adapts:

- No branch soloed/selected (the common case): only the head type is in play →
  **1 column** + space → `* main` / `  other` (identical to today).
- A set is active: head + set both in play → **2 columns** (+1 separator space) →
  `◉* feat` (in set & head), `◉  feat-y` (in set, not head), ` * main` (head
  only), `   plain` (neither — two blank columns + the separator).
- Detached HEAD and no set: neither in play → **0 columns** → no gutter, name
  starts immediately (renderPanel's `>`/`◆` cursor prefix still applies).

The design is **extensible**: a future indicator type is one more entry in the
ordered list with an `inPlay`/`glyph` rule; the width math is generic.

`(↓N)` behind-indicator and the worktree-path suffix are unchanged — they stay on
the right (the user flagged only the set marker as getting cut).

## Implementation sketch

Rewrite `branchRows()` around an ordered indicator list. Each indicator maps a
branch to its glyph rune (or `' '`); a column is rendered only when at least one
branch yields a non-space glyph for it:

```go
func (m Model) branchRows() []string {
	inScope := func(b model.Branch) bool { return slices.Contains(m.commitScopeBranches, b.Name) }
	// Ordered left-to-right. Each returns the branch's glyph or ' '.
	indicators := []func(model.Branch) rune{
		func(b model.Branch) rune { if inScope(b) { return '◉' }; return ' ' }, // set
		func(b model.Branch) rune { if b.IsHead { return '*' }; return ' ' },   // head
	}
	// A column is in play iff some branch yields a non-space glyph for it.
	active := make([]bool, len(indicators))
	for i, ind := range indicators {
		for _, b := range m.branches {
			if ind(b) != ' ' {
				active[i] = true
				break
			}
		}
	}
	out := make([]string, 0, len(m.branches))
	for _, b := range m.branches {
		gutter := make([]rune, 0, len(indicators)+1)
		for i, ind := range indicators {
			if active[i] {
				gutter = append(gutter, ind(b))
			}
		}
		if len(gutter) > 0 {
			gutter = append(gutter, ' ') // one separator before the name
		}
		row := string(gutter) + b.Name
		if b.Behind > 0 {
			row += " (↓" + strconv.Itoa(b.Behind) + ")"
		}
		if path, ok := m.worktreePathOf(b.Name); ok {
			row += " (" + path + ")"
		}
		out = append(out, row)
	}
	return out
}
```

## Invariants preserved

- **Single-sourced row** — `branchRows()` feeds display + the filter haystack
  (`panelView`→`Row(i)`) + the reveal tooltip. The gutter glyphs (`◉`/`*`/spaces)
  were already in the row string before; moving them left changes their position,
  not their presence, so filtering behavior is unchanged.
- **Selection reads the model, not the string** — `selectedBranch()` indexes
  `m.branches`, never parses the row, so the gutter change can't break selection,
  jump, or any branch action.

## Testing (TDD)

Unit tests on `branchRows()`:

1. **No set active** → gutter is the 1-column head form: the head branch row
   starts `"* "`, a non-head branch starts `"  "` (today's layout, regression
   guard).
2. **Set active** → 2-column gutter: a soloed non-head branch row starts `"◉ "`
   (set glyph in col 0, blank head col, then the name after the separator); the
   head-and-not-in-set branch row starts `" *"`; alignment — every row's name
   begins at the same column.
3. **Set marker is on the LEFT** (the bug fix): with a set active, the `◉`
   appears before the branch name, not after — assert the row does **not** end
   with `◉` and that `◉`'s index precedes the name's first rune.
4. **Detached / no indicators** → with no head and empty scope, rows have no
   gutter (name first).

No git/argv change → no real-git or e2e scenario.

## Out of scope

- Moving `(↓N)` or the worktree path into the gutter (right-side, unchanged).
- New indicator types (the structure is ready for them; none added now).
