# Commits-window ref decorations (tags + multi-branch tips) — design

**Date:** 2026-06-28
**Status:** approved (Option B + parens style chosen)

## Problem

The Commits panel renders only ONE ref well — the left "identity column"
(primary branch tip / lineage via `%S`). Everything else is under-served:

- **Tags** are parsed into `model.Commit.Refs` (`RefTag`) but **never shown** in
  the Commits panel (only in the `i` commit-message popup).
- **Multiple branch tips at one commit** (e.g. create `branch1`,`branch2` off
  `main` → 3 tips on one commit) are shown only as `‹name›` pills rendered
  AFTER the subject, where they are routinely truncated off a narrow panel and
  the `■` marker looks identical whether 1 or 5 branches point there.

## Solution (Option B — git-decorate style)

Render a **decoration group before the subject** listing every ref at the commit
beyond the primary identity, in `git log --decorate` parenthesized style, plus a
**count badge** on the tip marker.

Row layout:

```
<prefix> <marker+badge> <identity column>  (<extra-branches>, <⊙tags>)  <subject>
  ● /│      ■³               main            (branch1, branch2, ⊙v1.0.0)   Initial commit
```

- **Identity column** — unchanged (primary branch tip, BRIGHT; lineage branch,
  GRAY via `%S`). Still shown on every row, including non-tip lineage rows.
- **Marker + count badge** — the existing 3-cell marker area (`■`/`▲` + 1) gains
  a superscript count when **≥2 local branches** tip the commit: `■²`, `■▲³`.
  The count is the number of local-branch tips at the commit (the user's "3
  branches" = `■³`). Tags do not contribute to the count. Absent for a single
  tip (`■ `) or a remote-only tip.
- **Decoration group** — `(label, label, …)` before the subject, containing:
  - **extra local branches** (every local tip except the one in the identity
    column), default foreground;
  - **tags** (every `RefTag` at the commit), each as `⊙<name>` in **yellow**
    (color 220), the git tag convention.
  - Order: extra branches first (feed/`%D` order), then tags. Separator `, `.
  - The group appears whenever there is ≥1 extra branch OR ≥1 tag — **including
    on non-tip rows** (a tag on a commit that is no branch's tip still shows).
- **Narrow collapse** — when the full group would not fit the Commits panel's
  content width (leaving a sensible minimum for the subject), the whole group
  collapses to `(+N)`, where N = number of refs in the group (extra branches +
  tags). The subject is still window-truncated as today.

Lineage rows with no extra refs and no tags render exactly as today.

## Architecture

The row STRING and its STYLING are produced in two separate passes that must
agree on the group's exact layout and the rune positions of the colored tag
labels:

- `commitIdentRowAt` (`view.go:1128`) builds the row string.
- `commitDecoratorsRange` (`view.go:1180`) builds per-row `commitLineDecorator`s
  that recolor fragments in a single ANSI-drift-free pass.

To keep them in sync, a **single pure helper** owns the group layout:

```
commitDecoGroup(id commitIdent, budget int) (group string, tagSpans []decoSpan, collapsed bool)
```

- `group` is the rendered group string (`" (…)"`, or `" (+N)"`, or `""`).
- `tagSpans` are `{offset, length}` rune ranges (relative to the start of
  `group`) of each `⊙tag` label, for the decorator to color yellow. Empty when
  collapsed.
- `budget` is the max display width the group may occupy before collapsing.

Both passes call `commitDecoGroup` with the SAME budget (derived from one
`m.commitContentWidth()` source), so the string and the color spans always
match. The decorator converts each `tagSpans` offset to an absolute column by
adding the group's base column (prefix + marker(3) + identity width + 1).

### Data

`commitIdent` (`commit_ident.go:71`) gains:
- `tags []string` — tag names at the commit (from `RefTag` refs).
- `count int` — number of local-branch tips at the commit (for the badge).

`commitIdentOf` populates them. The existing `extra []string` (extra branch
tips) is reused for the group's branch labels; the after-subject `pills()` path
is REMOVED from row assembly (its information now lives in the before-subject
group).

### Styling

- Tag labels: `tagDecoStyle = lipgloss.NewStyle().Foreground("220")` (yellow),
  applied via an extended `commitLineDecorator` that accepts a list of
  `(absoluteColumn, length, style)` spans in addition to its current dot+dim
  work — same single-pass approach, no inline ANSI, no rune-index drift.
- Branch labels in the group: default foreground (distinct from the gray lineage
  identity and the yellow tags).
- Count badge: superscript digit `²`–`⁹`; `⁺` for ≥10. The marker area stays
  exactly 3 display cells (a width-assertion test guards this).

### Search

`commitHaystacks` (`view.go:1255`) already includes branch names + subject; it
gains **tag names** so tags are findable via the Commits `/` filter and `@`
highlight.

## Out of scope (v1)

- Remote-tracking refs (`origin/x`) as group labels — still represented by the
  `▲` marker.
- Progressive partial elide (show some labels + `+k`); v1 is all-or-`(+N)`.
- Annotated vs lightweight tag distinction in the glyph (would require
  cross-referencing `m.tags`; the `%D` ref carries no annotated flag).
- A config toggle (the decoration is a core readability improvement, always on).

## Testing

- `commit_ident.go`: `commitIdentOf` captures `extra`, `tags`, `count` from a
  commit with multiple `RefLocal` + `RefTag` refs; the marker area renders the
  badge and stays width-3 (`lipgloss.Width` assertion); single-tip/remote-only
  cases unchanged.
- `commitDecoGroup`: full group string + tag spans for branches+tags; `(+N)`
  collapse when over budget; empty when no extra refs/tags; tag span offsets
  land exactly on the `⊙name` substrings.
- Row level (`commitIdentRowAt`): group renders before subject; lineage rows
  unchanged; haystack includes tags.
- Decorator: tag spans colored yellow, lineage dim + lane dot still correct, no
  width change, only first visual line decorated.
