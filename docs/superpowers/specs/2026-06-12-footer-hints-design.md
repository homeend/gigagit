# Contextual footer key hints — design

Date: 2026-06-12
Status: approved (brainstorm with user; approach A of three)

## Goal

Replace the static footer line with a context-sensitive one: it shows only the
keys that are actually available given the focused panel, the selected row,
and the current mode. The active window/element declares what it supports
through a declarative binding registry; availability conditions are shared
with `Update` so the footer can never advertise a dead key.

## Current state

- `internal/tui/view.go` renders `const footerText` (one long line, truncated
  to width) regardless of state.
- Key availability already varies: `b`/`B` only when Branches is focused; `d`
  only on Branches/Worktrees; `enter` only on Worktrees and only when the
  cursor is not on the current worktree; most ops gate on
  `!m.running && !m.loading`; `s`/`w`/`W` act on the Branches selection
  regardless of focus.
- `TestHelpFooterCoverage` (help_test.go) regex-extracts `[x]` keys from the
  `footerText` const and requires a matching row in `helpContent()`.

## Decisions (from brainstorming)

1. **Granularity:** focused panel + selected row (e.g. `[s]witch` disappears
   when the cursor is on the current branch).
2. **Layout:** context actions first, then a separator (`•`) and a global
   tail. One line, truncated to width as today — context-first ordering means
   narrow terminals drop the least relevant keys.
3. **Popups/modal:** out of scope. Overlays keep their inline hint rows; the
   footer behind an overlay is background and may be stale.
4. **Approach:** declarative binding registry with predicates shared between
   `Update` and the footer (approach A). No keymap/dispatch refactor.

## Governing rule

**The footer never shows an unavailable key; it may omit available ones for
brevity.** Examples of deliberate omission: `s`/`w` technically act on the
Branches selection from any panel, but are advertised only when Branches is
focused; `W`, `B`, `shift+tab`, `pgup/pgdown` are usable but not advertised
(the `?` help window documents everything).

## Design

### Binding registry (`internal/tui/footer.go`, new)

```go
// footerBinding is one advertised key: a canonical key name (for the help
// drift guard), the rendered label, and the availability predicate.
type footerBinding struct {
	key   string           // "s", "d", "enter", ...
	label string           // "[s]witch"
	when  func(Model) bool // true when the key would do something right now
}
```

Two ordered slices:

- `contextBindings` — panel/row-specific actions, rendered first:

  | Binding | label | when |
  |---|---|---|
  | switch | `[s]witch` | Branches focused && `canSwitchBranch()` |
  | branch | `[b]ranch` | Branches focused && `canOpenBranchPopup()` |
  | worktree | `[w]orktree` | Branches focused && `canOpenWorktreePopup()` |
  | delete (branch) | `[d]elete` | Branches focused && `canDeleteBranch()` |
  | mark | `[m]ark` | `canMark()` && Branches focused && no live mark on this panel |
  | unmark | `[m] unmark` | Branches focused && live mark in this panel && cursor on the marked row |
  | pair | `[m] pair` | Branches focused && live mark in this panel && cursor on a different row (pair ops exist only for Branches) |
  | enter | `[enter] switch` | Worktrees focused && `canEnterWorktree()` |
  | delete (worktree) | `[d]elete` | Worktrees focused && `canDeleteWorktree()` |

  Status and Commits panels contribute no context bindings (their generic
  keys — sort, filter, navigation — live in the global tail or are
  undocumented-but-available).

  The two `d` rows and three `m` rows are separate bindings with mutually
  exclusive predicates; at most one of each renders at a time.

- `globalBindings` — the tail, rendered after `•`, each still predicated:

  | label | when |
  |---|---|
  | `[p]ull` | `opsIdle()` |
  | `[P]ush` | `opsIdle()` && `m.status.Branch != ""` |
  | `[S]tash` | `opsIdle()` |
  | `[u]ndo` | `opsIdle()` |
  | `[o]rder` | `opsIdle()` |
  | `[/]filter` | `opsIdle()` |
  | `[R]epo` | `opsIdle()` |
  | `[,] settings` | `opsIdle()` |
  | `[tab] focus` | always |
  | `[r] reload` | `!m.running` |
  | `[?] help` | always |
  | `[q] quit` | always |

### Renderer

`func (m Model) footerLine() string` in footer.go:

1. **Filter-typing override:** when `m.filterTyping`, return
   `"filter: type to search  [enter] keep  [esc] cancel"` — that mode
   captures every key, so nothing else applies.
2. Otherwise collect labels of `contextBindings` whose `when(m)` is true,
   then labels of passing `globalBindings`. Join each group with a space;
   join the groups with `"  •  "` (omit the separator when the context group
   is empty).
3. `view.go` calls `truncate(m.footerLine(), g.w)` where it used
   `truncate(footerText, g.w)`. The `footerText` const is deleted.

While an operation runs, no special-casing is needed: every gated predicate
returns false and the footer naturally collapses to
`[tab] focus  [?] help  [q] quit` (the `⏳` indicator already lives in the
status line). The esc hints for clearing a mark or a committed filter stay
out of the footer (status line / help window cover them).

### Shared predicates (`internal/tui/avail.go`, new)

Methods on `Model`, used by BOTH `Update`'s key switch and the registry —
this is what makes the footer honest by construction:

```go
func (m Model) opsIdle() bool             // !m.running && !m.loading
func (m Model) canSwitchBranch() bool     // opsIdle && Branches row resolves && !branch.IsHead
func (m Model) canOpenBranchPopup() bool  // opsIdle && Branches row resolves
func (m Model) canOpenWorktreePopup() bool// opsIdle && Branches row resolves
func (m Model) canDeleteBranch() bool     // opsIdle && Branches row resolves && !branch.IsHead
func (m Model) canDeleteWorktree() bool   // opsIdle && Worktrees row resolves && wt.Path != m.currentWorktree
func (m Model) canEnterWorktree() bool    // opsIdle && Worktrees row resolves && wt.Path != "" && wt.Path != m.currentWorktree
func (m Model) canMark() bool             // opsIdle && focused row resolves
```

"Row resolves" means `m.backingIndex(panel)` returns ok. `Update`'s cases for
`s`, `b`/`B`, `w`/`W`, `d`, `enter`, `m` are rewritten to call these
predicates instead of inlining the conditions. Footer `when` funcs may add a
focus check on top of a predicate (stricter than `Update` is allowed; looser
never is).

### Deliberate behavior tightening

Sharing predicates makes three cases no-ops that today spawn an operation git
then rejects:

- `s` on the current (HEAD) branch — previously started a `SmartSwitch` to
  itself.
- `d` on the current branch — previously started a `DeleteBranch` that git
  refuses ("checked out at ...").
- `d` on the current worktree — previously started a `RemoveWorktree` that
  git refuses ("is the current working tree").

These become silent no-ops, consistent with the other unavailable-key cases.
`b`/`B`, `w`/`W` keep working from any panel (they act on the Branches
selection); only their footer visibility is Branches-scoped.

### Help window and drift guard

`helpContent()` is unchanged (it documents everything, context-free).
`TestHelpFooterCoverage` is rewritten: instead of regexing the deleted
`footerText` const, it iterates `contextBindings` + `globalBindings` and
requires each `binding.key` to appear as a help-row key column (same
whole-field-then-/-split matching as today). Same guarantee, structural
source. The skill `.claude/skills/adding-tui-windows/SKILL.md` footer note is
updated to point at the registry.

## Testing

All tests are pure-Model fixture tests (no real git needed beyond existing
helpers):

1. **Per-context footer tests** (`footer_test.go`): build models with
   specific focus/selection/state and assert the footer contains and omits
   the right labels — Branches focus on a non-HEAD branch (full context set),
   cursor on the HEAD branch (`[s]witch`/`[d]elete` absent), Worktrees focus
   on the current worktree (`[enter]`/`[d]elete` absent) vs another worktree
   (present), Status/Commits focus (no context segment, no stray `•`),
   mark states (mark/unmark/pair labels), running (collapses to
   tab/help/quit), filter-typing (override line), empty panels (no row →
   row-dependent keys absent).
2. **Drift guard**: rewritten `TestHelpFooterCoverage` over the registry.
3. **Honesty tests**: for the row-gated keys (`s`, `d`, `enter`, `m`), when
   the shared *predicate* (`canSwitchBranch()`, `canDeleteBranch()`,
   `canDeleteWorktree()`, `canEnterWorktree()`, `canMark()`) is false,
   sending the key leaves the model unchanged and returns no command (no op
   started) — pins the predicate-sharing contract and the behavior
   tightening. (Predicates, not binding `when` funcs: a binding may be
   hidden by its focus check while the key still legitimately works from
   another panel.)
4. **Width**: footer still truncated to terminal width (existing `truncate`).

## Out of scope

- Popup/decision-modal hint rework (inline hints stay).
- Mouse interaction with the footer.
- Advertising every available key (W, B, shift+tab, pgup/pgdn stay
  help-window-only).
- Any CLI surface change (no agentskill bump needed).

## Files

| File | Change |
|---|---|
| `internal/tui/footer.go` | new: binding type, registries, `footerLine()` |
| `internal/tui/avail.go` | new: shared `can*` predicates |
| `internal/tui/footer_test.go` | new: per-context + honesty tests |
| `internal/tui/view.go` | render `m.footerLine()`; delete `footerText` |
| `internal/tui/model.go` | `Update` cases call the predicates |
| `internal/tui/help_test.go` | drift guard reads the registry |
| `.claude/skills/adding-tui-windows/SKILL.md` | footer note → registry |
| `CHANGELOG.md`, `README.md` | document the contextual footer |
