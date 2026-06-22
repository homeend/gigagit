# Cursor-Aware Text Fields — Design

**Date:** 2026-06-22
**Status:** Approved (brainstorm)

## Goal

Every editable text field in the TUI (commit title/description, branch name,
etc.) is currently **append-only**: the popup keeps a bare `string`, runes are
appended at the end, and backspace trims the last rune. There is no cursor — the
user cannot move into the middle of what they typed to fix a mistake, and they
cannot see where they are typing.

Give all **true editable fields** a visible cursor with full line editing:
`←/→` to move, `Home/End`, insert/delete at the cursor, and word-jumps. The
**search/filter** inputs (the quick-switcher filters) are explicitly **out of
scope** and keep their append-only behavior.

## Editing capability (settled)

Word-jump level:

- `←` / `→` — move cursor one rune (clamped to `[0, len]`).
- `Home` / `End` — to start / end of the current visual line.
- `Backspace` (`KeyCtrlH` too) — delete the rune **before** the cursor.
- `Delete` — delete the rune **at** the cursor (forward delete).
- Rune / space insert — at the cursor (not appended at end).
- `Alt+←/→` and `Ctrl+←/→` — word-left / word-right.
- `Ctrl+W` — delete the word before the cursor.

A "word" is a maximal run of non-space runes; word-left/right and delete-word
skip over intervening spaces in the natural readline sense.

## The component — `internal/tui/textfield.go`

A pure value type, in the same idiom as `internal/textdiff` and
`internal/commitgraph` (no git/Bubble Tea-model deps beyond the `tea.KeyMsg`
type for `HandleEditKey`):

```go
type textfield struct {
    runes  []rune
    cursor int // rune index in [0, len(runes)]
}
```

### Key contract — who owns which keys

`textfield` owns **only the editing keys**. Navigation and submit semantics stay
with the popup, because different popups bind Enter/Tab/Up/Down differently
(field switching, submit, list navigation, newline insertion).

```go
// HandleEditKey applies one editing key. Returns true if it consumed the key,
// false for any key the popup must handle (Enter, Tab, Esc, Up, Down, Ctrl+S, …).
func (f *textfield) HandleEditKey(msg tea.KeyMsg) bool
```

Consumed (returns true): `KeyRunes`, `KeySpace`, `KeyBackspace`, `KeyCtrlH`,
`KeyDelete`, `KeyLeft`, `KeyRight`, `KeyHome`, `KeyEnd`, the word-jump variants
(alt/ctrl + left/right), and `KeyCtrlW`.

Not consumed (returns false): everything else — the popup's `update` falls
through to its existing Enter/Tab/Esc/Ctrl+S handling unchanged.

### Multi-line support (commit description)

The commit **description** is multi-line. Rather than introduce a separate
textarea, the *same* `textfield` treats `\n` as an ordinary rune in `runes`.
`←/→` cross newlines naturally; `Home/End` snap to the current visual line's
bounds. The popup drives the line-specific actions explicitly (because it owns
Enter and Up/Down):

```go
func (f *textfield) InsertNewline()  // popup calls on Enter when field == desc
func (f *textfield) Up()             // best-effort same-column move to prev line
func (f *textfield) Down()           // best-effort same-column move to next line
```

Single-line fields never contain a `\n`, so `Up/Down/InsertNewline` are simply
never called for them — their popups route Up/Down to field switching as today.

### Pre-fill and read-out

```go
func (f *textfield) Value() string       // current buffer as a string
func (f *textfield) SetValue(s string)    // replace buffer, cursor → end
```

`SetValue` is used by amend pre-fill (`splitMessage` → title/desc) and
rename-branch pre-fill (existing name), placing the cursor at the end so the
user can immediately keep typing.

### Rendering

```go
func (f *textfield) View(focused bool) string
```

- **focused:** the buffer with a reverse-video cursor cell at `cursor`. When the
  cursor is at end-of-buffer (or end-of-line), a trailing reverse-video block (a
  space) marks the insertion point so it is always visible.
- **unfocused:** plain text, no cursor cell.

`View` returns only the field's own text; the popup keeps composing the label
and layout around it (`"name: " + f.View(focused)`), including any multi-line
description's continuation lines. The reverse-video cell is built with the
existing lipgloss styling approach; because lipgloss emits no color in non-TTY
test runs, the cursor-placement tests assert on **rune position / structure**,
not ANSI bytes (an end-to-end render test can force `termenv.TrueColor` if we
want to prove the styled cell survives — same lesson as the commit-lane-color
work).

## Migration (which fields adopt it)

Replace the bare `string` with a `textfield` in each editable popup. Pattern per
popup: `update` calls `field.HandleEditKey(msg)` first; on `false`, fall through
to the existing key switch. Render swaps the bare string for `field.View(focused)`.

| File | Field(s) |
|------|----------|
| `commit_popup.go` | `title`, `desc` (desc multi-line) |
| `branch_popup.go` | `name` |
| `rename_branch_popup.go` | `name` (pre-filled) |
| `tag_popup.go` | `name`, `message` |
| `stash_popup.go` | `name` |
| `tag_checkout_popup.go` | `name` |
| `shelf_actions.go` | `dest` |
| `worktree_popup.go` | the `inputs` map values **and** `editBuf` |

`commit_popup.applyEditKey` is shared with the interactive-rebase **reword**
sub-mode, so that path inherits the cursor for free.

## Out of scope (unchanged)

The quick-switcher **filters** keep append-only behavior:

- `bookmark_popup.go` `filter`
- `repo_popup.go` `query`
- `content_popup.go` `query` (files-view file-tree search, stash file list, help/blame)

These are transient search filters, not composed text the user edits in place.
(If we later want cursor there too, the same `textfield` drops in — but the user
explicitly excluded "search".)

## Testing

TDD, pure UI — no real git needed.

1. **`textfield_test.go`** — a table covering: insert at cursor, insert in the
   middle, backspace-before / delete-at, `←/→` clamping at both ends, `Home/End`
   on a single line and on a multi-line buffer, word-left/right and `Ctrl+W`
   across spaces, `InsertNewline` + `Up/Down` column tracking, `SetValue` cursor
   placement, and `View` cursor position including the at-end block.
2. **Per-popup integration tests** — route a key sequence through the popup's
   `update` (e.g. type `"abc"`, `←`, `←`, type `"X"`, backspace) and assert
   `Value()` and that non-edit keys (Enter/Tab/Esc/Ctrl+S) still behave. Reuse
   the existing loaded-model test harness.

## Process

Work in a git worktree under `.claude/worktrees/` (repo convention — keeps
session worktrees out of `git status`). One feature branch off `main`; the human
merges. Update `CHANGELOG.md` and, if the documented key help changes,
`README.md`. No CLI surface change, so `internal/agentskill` is untouched.
