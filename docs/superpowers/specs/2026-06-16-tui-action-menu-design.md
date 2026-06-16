# TUI `.` Action Menu + Configurable Footer/Menu — Design

**Status:** design (approved, pre-plan).
**Date:** 2026-06-16
**Origin:** captured follow-up of the window-framework work
(`2026-06-16-tui-window-framework-design.md` §"Captured follow-up"). The window
framework (Stages 1a/2/3 + Plan 1b) is complete; this is the last remaining item.

## Goal

Add a **`.` action menu**: an overlay popup listing every action currently
available (given focus and repo state) so any of them can be invoked in one or
two keystrokes — and make the **footer** and the **menu** independently
configurable, so the user chooses which actions clutter the always-visible
footer versus living only in the menu.

## Background

The TUI footer (`internal/tui/footer.go`) is a display-only registry of
`footerBinding{key, label, when}` — `contextBindings` (panel/row-specific) and
`globalBindings` (the always-relevant tail). `footerLine()` renders the bindings
whose `when(m)` predicate is true; the **actual action** lives in the big
`Update` key switch. The footer has grown long (the global tail alone is ~15
keys), and a user can't tailor it.

This feature turns that registry into the single catalog behind three consumers
— the footer, the new `.` menu, and a keypress-replay executor — and adds two
config allowlists to control footer/menu membership.

## Action ids

Every actionable binding gains a stable **id** (`footerBinding.id`). Ids are the
addressing scheme for both config lists and decouple config from the literal
key (keys are reused across panels — `s`, `m`, `d`, `enter`, `space` — so a key
cannot unambiguously name a context action; an id can).

Catalog (id → key, source):

| id | key | context |
|---|---|---|
| `switch` | `s` | Branches |
| `branch` | `b` | Branches |
| `worktree` | `w` | Branches |
| `delete-branch` | `d` | Branches |
| `mark` / `unmark` / `pair` | `m` | Branches (mutually exclusive states) |
| `switch-worktree` | `enter` | Worktrees |
| `delete-worktree` | `d` | Worktrees |
| `file-diff` | `enter` | Files / Staged |
| `stage` | `space` | Files |
| `unstage` | `space` | Staged |
| `stash` | `s` | Files |
| `mark-file` | `m` | Files / Staged |
| `commit-files` | `l` | Commits |
| `resolve` | `x` | global (conflicts) |
| `commit` | `c` | global |
| `amend` | `C` | global |
| `pull` | `p` | global |
| `push` | `P` | global |
| `stashes` | `S` | global |
| `undo` | `u` | global |
| `order` | `o` | global |
| `view` | `z` | global |
| `filter` | `/` | global |
| `repo` | `R` | global |
| `settings` | `,` | global |
| `reload` | `r` | global |
| `help` | `?` | global |
| `quit` | `q` | global |
| `actions` | `.` | global (opens this menu) |

**Navigation keys get no id and are never menu/config eligible:** `tab`,
`shift+tab`, `ctrl+←/→` (focus/tab movement, not single actions, and not
single-key-replayable). The `actions` id opens the menu; it is **always shown in
the footer** (so the menu is discoverable) and is **never a row inside the menu**
(no "open the menu" item within the menu).

A drift-guard test asserts: every non-navigation binding has a non-empty id, and
all ids are unique.

## The `.` action menu

A new overlay popup (`internal/tui/action_menu.go`), built on the Plan 1b
window-primitive popup pattern — same look/feel as the repo and settings popups.

- **Open:** `.` from the **base layout only** (suppressed while a modal, any
  popup, the stash/files view, or a full-screen view owns the keyboard). Mouse
  and op state do not gate it; it is a read-only list of whatever is available.
- **Content:** the rows are the registry bindings whose `when(m)` is true,
  filtered/ordered by `menu_actions` (see Config). Each row shows its key and
  label, e.g. `[p] pull`, `[enter] diff`, `[space] stage`. `actions` and
  navigation are excluded.
- **Interaction:**
  - Press an action's **key directly** to run it (`p` runs pull).
  - `↑`/`↓` (or `j`/`k`) + `enter` runs the highlighted row.
  - `/` filters by label (type-to-filter, like the help popup); while filtering,
    letter keys narrow rather than execute, `enter` runs the highlighted row,
    `esc` cancels the filter.
  - `z` cycles the display mode, `shift+←/→` pans (window primitive).
  - `esc` closes.
- **Render:** `renderWindow` body + `overlayCenter`, a `*actionMenu` pointer
  field on `Model` (`sel`, `mode`, `hscroll`, `filter` typing state), mirroring
  `repoPopup`.

### Execution model (keypress replay, no logic duplication)

Selecting a row does **not** re-implement the action. It closes the menu and
re-dispatches the action's key through the normal `Update`:

```go
m.actionMenu = nil
return m.Update(synthKey(key)) // reaches the base-layout handler
```

Closing the menu first means the synthesized `tea.KeyMsg` is routed to the base
dispatch (not back into the menu). `synthKey(name)` maps the registry key to a
`KeyMsg`: `"enter"→KeyEnter`, `"space"→KeySpace`, everything else (single runes
including `/`, `,`, `?`, `.`) → `KeyRunes{[]rune(name)...}`. Only ids exist for
single-key-replayable bindings, so the map stays tiny. `synthKey` is unit-tested
(its `String()`/`Type` matches what the base handler switches on) and the menu→
action path is tested end-to-end (`.` then `p` starts SmartPull; `.` then `space`
on a Files row stages).

## Config: two symmetric allowlists

Two new `[ui]` keys, each a **list of action ids**, each **unset/empty = the
full default set** (no surprising "empty means none"):

```toml
[ui]
footer_actions = ["pull", "commit", "filter", "file-diff"]  # unset → default footer
menu_actions   = []                                         # unset/empty → all actions
```

- **`footer_actions`** — when non-empty, the footer shows exactly these ids, in
  list order, among those currently available (`when(m)` still predicates). When
  unset/empty, the footer keeps today's behavior: the context group then the
  global group in registry order. `actions` (`[.]`) always shows regardless.
- **`menu_actions`** — when non-empty, the `.` menu shows exactly these ids, in
  list order, among those available. When unset/empty, the menu shows **all**
  available actions in registry order (the default).
- An id may appear in both lists, one, or neither. In neither: the action is
  still reachable by pressing its key directly, just unadvertised.

### Config plumbing

`internal/config/config.go` `UIConfig` gains:

```go
FooterActions []string `toml:"footer_actions"`
MenuActions   []string `toml:"menu_actions"`
```

Defaults: nil (unset). `overlayUI` copies each when the source is non-empty
(`if len(src.FooterActions) > 0 { dst.FooterActions = src.FooterActions }`),
matching the existing zero-value-is-unset rule — a repo `.gg.toml` can override
the global list but "clear to empty" means "inherit/default," not "show none"
(YAGNI: no need for an explicitly-empty footer). `Model` reads them via
`m.cfg.UI.FooterActions` / `.MenuActions`, like `wheelStep()`/`hscrollStep()`.

Unknown ids in a list are ignored (a malformed config never hides a key from its
own keystroke; the action still works). A debug-friendly note: ignored-unknown
is silent (consistent with the tolerant config overlay elsewhere).

## Architecture / files

| File | Change |
|---|---|
| `internal/config/config.go` | `UIConfig.FooterActions`/`MenuActions` + `overlayUI` |
| `internal/tui/footer.go` | `footerBinding.id`; ids on every binding; add `actions` binding; `footerLine()` filters/orders the registry by `footer_actions` |
| `internal/tui/action_menu.go` (new) | `actionMenu` struct, `openActionMenu`, `updateActionMenuKey`, `renderActionMenu`, `availableActions(m)` builder (shared by menu + footer), `synthKey` |
| `internal/tui/model.go` | `actionMenu *actionMenu` field; `.` opens it from the base layout; route keys to `updateActionMenuKey` while open (precedence above panels, below modal) |
| `internal/tui/view.go` | composite `renderActionMenu` via `overlayCenter` in the render cascade |
| `internal/tui/mouse.go` | menu swallows mouse like the other popups (precedence) |
| `internal/tui/help.go` | new "Action menu (.)" section; `.` row |
| `README.md`, `CHANGELOG.md` | `.` key + `footer_actions`/`menu_actions` config |
| `.claude/skills/adding-config-entries/SKILL.md` | (optional) note the two list entries as examples |

The registry stays the single source of truth: footer, menu, and executor all
read it. `availableActions(m)` returns the predicated, ordered rows once; the
footer and menu each apply their own allowlist over that.

## Testing strategy

Follow TDD.

- **Registry integrity:** every non-navigation binding has a unique non-empty
  id; navigation keys have none (drift guard).
- **availableActions:** for a given focus/state, returns exactly the expected
  ids (e.g. Files panel with a modified file → `file-diff`, `stage`, `mark-file`,
  `stash`, + globals); excludes unavailable ones and navigation.
- **synthKey:** table test — `enter`→KeyEnter, `space`→KeySpace, runes
  (`p`,`/`,`,`,`?`,`.`) → KeyRunes with matching `String()`.
- **Menu execution (end-to-end):** `.` then `p` starts SmartPull (assert via the
  same op hooks the footer tests use); `.` then `enter` on a Files row opens the
  diff; `.` then `space` on a Files row stages; `↑/↓`+`enter` runs the
  highlighted row.
- **Menu open/close/filter:** `.` opens only from the base layout (no-op under a
  popup/modal/view); `esc` closes; `/` filters by label; `z` cycles mode.
- **footer_actions:** set → footer shows exactly the listed ids in order (+ always
  `actions`); unset/empty → today's footer (a snapshot/contains assertion reusing
  the footer-test harness).
- **menu_actions:** set → menu shows exactly the listed ids in order; unset/empty
  → all available.
- **Config overlay:** repo list overrides global; `len==0` = unset/inherit;
  unknown id ignored (its key still works).
- **Docs drift:** the `?` help and footer include `.`; `TestHelpFooterCoverage`
  passes.

Gate: `./test.sh race` green before merge.

## Risks / constraints

- **Re-dispatch ordering:** the menu MUST clear `m.actionMenu` before calling
  `m.Update(synthKey(...))`, or the synthesized key is re-captured by the menu.
  Tested.
- **synthKey fidelity:** `enter`/`space` are the only non-rune keys an id can
  carry; verified against the live base dispatch end-to-end, not just by
  `String()`.
- **Footer ordering when `footer_actions` is set:** the explicit list replaces
  the default two-group (context-then-global) layout — the list IS the order.
  Unset preserves today's grouping exactly (regression-pinned).
- **Value-receiver `Model`:** `actionMenu` is a pointer field (survives the copy),
  like the other popups.
- **Routing invariant:** the menu joins the modal → menu → popups → views → base
  precedence in `Update`, `handleMouse`, and `render` together (the standing
  TUI routing-invariant hazard).

## Non-goals / YAGNI

- No per-context or per-panel allowlists (one footer list, one menu list).
- No explicit "empty footer/menu" (empty = full set).
- No custom labels/icons or grouping/headings inside the menu.
- No reordering of the context group when `footer_actions` is unset.
- No new engine/domain/CLI/MCP surface — TUI + config only.
- Navigation keys (`tab`, `ctrl+←/→`) are never actions in the menu.
