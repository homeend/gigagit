# gg web: custom top bar buttons

## Why

The commands a user reaches for often sit behind `ctrl+k` / `ctrl+p` or the ☰
menu, while the top bar has empty room right after `refresh`. The user wants to
put their own commands there, one click away, without losing sight of the
worktree path.

## What the user sees

```
☰ gigagit ⎇ main  [pull] [push] [refresh] │ [fetch all remotes] [view all notes…] [⋯]      /work/gigagit
                  └── always there ─────┘ ↑ └── custom, in the configured order ──┘ ↑      └ never cut
                                    separator line                         the rest, when short of room
```

- **Fixed buttons.** `pull`, `push` and `refresh` stay where they are and are not
  configurable.
- **Separator.** A thin vertical line follows `refresh`, shown only when at least
  one custom button is configured and available. With none, the top bar looks
  exactly as it does today.
- **Custom buttons.** Up to 10, in the configured order, each showing the command's
  palette label and running exactly what the palette row runs.
- **Short of room.** The worktree path matters more than the buttons. When the row
  doesn't fit, custom buttons leave from the end of the list, one at a time, into
  a `⋯` button that follows the buttons still shown. Clicking it opens a vertical
  menu directly below it, listing the hidden commands in the configured order.
  This re-fits live on window resize and whenever anything in the row changes
  width (a branch or worktree switch, the solo chip, the notice chip).
- **Very narrow windows.** Only when every custom button is already inside `⋯` and
  the row still doesn't fit does the path get shorter, cut in the middle with the
  last segment kept (`elidePath`), never at the end. The tooltip gains the full path
  alongside its copy hint, and copying still takes the full path. If the row is
  still too wide, the branch name shortens with an ellipsis, and the path is never
  clipped. With no custom buttons configured, none of this applies and the row
  behaves as it does today.

## Choosing the buttons

A **Top bar buttons** section in the Settings overlay:

- Lists the chosen commands in order, each with move up, move down and remove.
- **add…** opens the command palette in a picking mode: same filter and keys,
  listing every command not yet chosen; enter adds it and returns to Settings.
  Disabled at 10, with a note saying so.
- Every change applies at once, the way other Settings buttons do, and the top bar
  updates immediately, with no reload.
- The caption says this setting is global: it applies to every repository.

## Configuration

Global config only:

```toml
[web]
toolbar = ["fetch all remotes", "view all notes…"]
```

- A `toolbar` in a repository's `.gg.toml` is ignored, like `[agents] spawn`. A
  cloned repo must not be able to put commands one click from the user.
- Absent or empty means no custom buttons.
- Web only. The terminal UI has no button row and is unchanged; the key has a
  `settingDocs` entry saying so.

## Behaviour rules

| Case | Result |
|------|--------|
| Command not available right now (`Leave workspace` with no workspace open, `Switch to TUI` on a standalone page, `branch versions…` with the feature off) | Not shown in the bar or in `⋯`; takes no space. Settings keeps it in the list, marked "not available here". |
| Entry matching no command (hand-edited, or a command renamed later) | Not shown in the bar. Settings shows it marked "unknown command", removable. Never an error. |
| More than 10 entries in the file | The first 10 are used; Settings shows only those, and the next save drops the rest. |
| Duplicate entries in the file | The first is used. |
| An operation is running | A custom button behaves as its palette row does (the command reports busy itself). It isn't greyed out like `pull`/`push`/`refresh`. |

## Decisions made by the author (to check while reading)

1. **A command is identified by its palette label text**, because it reads well in
   the config file. A later rename turns that entry into "unknown command" rather
   than failing silently.
2. **`pull`, `push` and `refresh` can't be picked**, since they are always on the
   bar already. A hand-edited entry naming one counts as unknown.
3. **`switch repo…` can be picked**, and opens the repository list. From a button,
   esc closes that list, since no palette was open to return to.
4. **Labels are never shortened.** A long label takes its full width and moves to
   `⋯` sooner.
5. **Reordering is by move up / move down**, not drag.
6. **Text only, no icons.** The palette has no icons to reuse. The tooltip shows the
   label plus its key, if it has one.
7. **`⋯` uses the existing context menu** (`showCtxMenu`), so j/k, arrows, enter and
   esc already work, and it closes on a click elsewhere.
8. **Only the top bar is added.** No keyboard shortcut per button and no per-repo
   or per-workspace list.
9. **The narrow-window order is buttons, then path, then branch name.** Users with
   no custom buttons see no change at all.

## Non-goals

- Terminal UI changes.
- Per-repository lists, button groups, custom labels, icons or arguments.
- Making `pull`, `push` or `refresh` removable.
- Running anything other than a palette command. Config entries are names looked
  up in the palette's own list, never commands to execute.

## Anti-goals

- The worktree path being cut while a button could have moved to `⋯`.
- The row wrapping to a second line, or `⋯` showing when nothing is hidden.
- Fit-related flicker: buttons visibly jumping during a repaint or a refresh.
- A second list of commands that drifts from the palette.

## Notes for the builder

- `paletteCommands()` in `internal/web/static/palette.js` is the single source. It
  filters conditional rows out, so split it: every command plus an availability
  check, with the palette showing the available ones. Settings needs the whole list
  to label "not available here".
- The `switch repo…` row has `run: null` and is special-cased in `runPaletteRow`;
  a button needs the same behaviour through one shared path.
- Palette and picker share the layer and keys; picking mode must go through
  `closePalette()` like every other close path.
- Backend: `Web.Toolbar []string` in `internal/config`, dropped from the repo layer
  in `Load`. It reaches the page through `/api/settings`, and is written to the
  global path only through `POST /api/settings`, rejecting more than 10 entries. The
  page also needs the list at boot without opening Settings.
- `SetWebToolbar` can't simply copy `SetAgentsReserved`: `setScalarLine` replaces
  only the line holding the key, so a hand-edited multi-line `toolbar = [...]` would
  leave stray lines and break the global config, and then every `Load` fails. It must
  replace the whole array, with a test for the multi-line case.
- The `⋯` click needs the same `stopPropagation` and second-click toggle as
  `#menu-btn`, or the outside-click closer shuts the menu as soon as it opens.
- A hosted page's terminal re-reads config through `Host.SetConfigChanged`.
- Style the buttons as the fixed ones (`polish.css`, `#top #pull-btn…`), without
  their mask icons. `polish.css` is due to fold into `style.css`; add to it, as a
  new rule set beside its siblings.
- `#top` is a flex row; `#repo-worktree` holds the right edge with `margin-left:
  auto`. It must not shrink before the buttons have gone.
- The `?` help (`index.html`) gets a row for the buttons and for `⋯`. Update
  `CHANGELOG.md`, `README.md` and the parity doc's web-only list.
- Verify with a browser probe in Firefox and Chromium, using scratch
  `XDG_CONFIG_HOME`/`XDG_STATE_HOME`: measure geometry at several window widths
  (path untruncated while any button remains, row height unchanged, separator
  absent with no buttons, menu below `⋯`), not class names.
