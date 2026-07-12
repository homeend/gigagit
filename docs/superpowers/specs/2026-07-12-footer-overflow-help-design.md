# Footer shortcut overflow → help window

**Date:** 2026-07-12
**Status:** Approved design, pending implementation plan

## Problem

The footer (`internal/tui/footer.go`) advertises every available key for the
current window. `renderInterface` hard-truncates the joined line at terminal
width (`view.go:363`: `truncate(m.footerLine(), g.w)`), so on narrow terminals
labels are cut mid-word and every key past the edge is silently invisible —
the user has no signal that more keys exist, and no way to see which ones.

## Behavior (what changes)

- The footer never shows a partially cut label and never silently hides keys.
- When all labels fit the terminal width: **no change** — the line renders
  exactly as today (ends `[?] help [q] quit`, no ellipsis).
- When they don't fit: whole labels are dropped from the **end** until the
  line, plus a forced `… [?] help` tail, fits. Context (panel/row) bindings
  render first and globals last, so trimming naturally hides the stable
  muscle-memory globals (`r`, `R`, `q`, …) while keeping the panel-specific
  discoverable keys visible.
- The dropped bindings appear as the **first section** of the `?` help
  window, heading **"More keys (not shown in the footer)"**, one
  `key | footer-label` row each, in footer order. When nothing was dropped
  the section does not appear.
- Only `[?] help` is protected. `[.] actions` is trimmed like anything else
  (it survives whenever it fits; the help window documents it).

## Scope

Applies to the registry-driven footer only:

- the default two-group layout (`contextBindings` + `globalBindings`), and
- the `[ui] footer_actions` allowlist branch (its always-append-`[.] actions`
  rule still runs *before* fitting).

Out of scope (returned as-is, `hidden = nil`): the hand-written mode footers —
proc indicator, filter-typing, highlight-typing, files view, stash view. The
bookmark/shelf popups' own `?` key lists are also untouched.

## Design

### 1. `footerParts` refactor (`footer.go`)

The two registry loops in `footerLine()` are extracted into

```go
type footerPart struct {
    label      string
    binding    footerBinding
    groupStart bool // true on the first global part → "  •  " separator before it
}

func (m Model) footerParts() []footerPart
```

`footerLine()` becomes "join all parts" — signature and output unchanged, so
the many existing width-less tests (`footer_test.go`, `commit_space_test.go`,
…) stay untouched.

### 2. `fitFooter` (`footer.go`)

```go
func fitFooter(m Model, w int) (line string, hidden []footerBinding)
```

- Special-mode footers (proc, filter/highlight typing, files view, stash
  view): return `truncate(existing, w)` — today's behavior — `hidden = nil`.
- Otherwise build the parts (default or allowlist branch), then fit greedily
  left-to-right using `lipgloss.Width` (labels carry wide glyphs like `⇧←→`).
- The `help` binding is pulled out of the normal flow. First try the full
  untrimmed line (with `help` in its natural position): if it fits, return it
  with `hidden = nil`. If not, reserve room for the `… [?] help` tail
  (ellipsis + separator + label), take parts in order while they fit, then
  append the tail. Every part not taken (including `[q] quit`) goes into
  `hidden`, in order.
- The tail is `"… [?] help"`, joined to the last kept label with a single
  space; with zero kept labels the tail stands alone.
- Degenerate width (the tail alone doesn't fit): fall back to
  `truncate(footerLine(), w)`, `hidden = nil`.
- Separator rules: labels joined with a single space, the context→global
  boundary with `"  •  "`, exactly as today. A group whose parts were all
  dropped contributes no separator.

### 3. Render site (`view.go`)

`footer := truncate(m.footerLine(), g.w)` becomes
`footer, _ := fitFooter(m, g.w)`. (`fitFooter` output is already ≤ w; no
second truncate needed except via the degenerate-width fallback inside.)

### 4. Help-open site (`model.go` `?` handler)

```go
_, hidden := fitFooter(m, m.layout().w)
m = m.pushLayer(newContentPopup("Help — keys", helpWithHidden(hidden)))
```

`helpWithHidden` prepends, when `hidden` is non-empty, a heading contentLine
**"More keys (not shown in the footer)"** plus one row per hidden binding in
the existing help row style (`padRight(key, 16) + label`), then the static
`helpContent()` table. `g.w` is a pure function of `m.width` (`layout()`), so
the `?` handler and the renderer always agree on what was hidden.

`helpContent()` itself stays a pure static table — the
`TestHelpFooterCoverage` drift guard is unaffected.

## Edge cases

- **Duplicate-key bindings** (the three `m` variants, two `d`, two `space`)
  have mutually exclusive predicates — at most one of each is live per frame,
  so the hidden list cannot show duplicates.
- **Empty-key bindings** (e.g. the graph `[<>] graph [⇧←→] pan [=] center`
  cluster, `key == ""`): render with an empty key column, label as-is.
- **Allowlist mode**: fitting runs after the allowlist is materialized; the
  existing invariant "`[.] actions` always present in the allowlist line"
  holds at any width where it fits (the width-less `footerLine()` tests that
  assert it are unaffected).
- **While an op runs** everything gated on `opsIdle` drops out; the short
  line fits and nothing changes.

## Testing

Unit tests on `fitFooter` (table-driven, reusing `footerModel()` fixture):

- wide width → line identical to `footerLine()`, `hidden` empty;
- narrow width → line ends with `… [?] help`, `lipgloss.Width(line) ≤ w`, no
  partial label, `hidden` = exactly the dropped bindings in footer order;
- boundary: width exactly fitting the full line → no trim, no ellipsis;
- tiny width → equals `truncate(footerLine(), w)`, `hidden` empty;
- special-mode footers (filesView set, filterTyping) → passthrough,
  `hidden = nil`;
- allowlist mode → fits within width, `[.] actions` present when it fits.

Help test: `?` at narrow width → popup content starts with the "More keys"
heading and contains each hidden key; at wide width → no such heading.
