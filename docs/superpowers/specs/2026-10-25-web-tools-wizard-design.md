# gg web — external tools wizard + repo settings location (parity stage J2)

Two rows of docs/web-tui-parity.md, ported from the TUI as it is:

- **External tools: add / edit** — the TUI's Settings → External tools wizard
  (`internal/tui/settings_tools.go`, `settings_popup.go` toolsView). It only
  ADDS catalog templates for the tools detected on this machine; it never
  edits a command (the update offer is the one rewrite, already ported).
- **Repo settings location** — the TUI's Settings → Repo settings location
  popup (`internal/tui/repoconfig_popup.go`): copy/move the whole per-repo
  config between the committed `.gg.toml` and the private
  `$XDG_CONFIG_HOME/gg/projects/<key>/config.toml`.

## What the TUI does (the spec)

### Wizard

- Rows: one per detected tool × catalog template
  (`exttool.Detect` → `domain.InstallTemplates`). Text:
  `[x] <tool label> — <category>: <template name>`, a configured row adds
  ` (configured)`, or ` (update available — u)` when its block has an update
  offer not answered "keep mine", or ` (agent version unsupported)`.
- Default checks: a new row is checked, EXCEPT an OptIn (yolo) template and
  EVERY `conflict_complete` row; a configured row shows `[x]` and is always
  skipped.
- The focused row's detail: `writes to: <global config path>` or
  `already configured — skipped on apply`, then the generated command.
- Keys: ↑/↓ move, space toggles, `u` on a row with an update opens the
  update offer, enter writes, esc goes back. Hint:
  `[space] toggle  [u] review update  [enter] write to global config  [ctrl+w] mode  [esc] back`.
- Enter appends one `domain.NewToolBlock` per checked, not-configured row to
  the GLOBAL config (`config.AppendToolCommands`), reloads the config, leaves
  the wizard for the Settings menu and says on the status line:
  `external tools: N command(s) written to <path>` /
  `external tools: nothing to write (already configured or unchecked)` /
  `external tools: <err>`.
- No tool: `no known tools detected on this machine (looked for: claude, junie, meld)`.

### Repo settings location

- Title `Repo settings location`; rows `committed  present|absent  <path>` and
  `private    present|absent  <path>` (`(unavailable)` without a path). The
  committed path is the current worktree's `.gg.toml`; the private one is
  `config.PrivateRepoPath(<main worktree>)`.
- Actions (`repoConfigActions`): Copy/Move to private (user dir) when the
  committed file exists; Copy/Move to committed (.gg.toml) when the private
  file exists. None: `(nothing to move — no per-repo config here, or not in a repo)`.
- Hint: `active file = private if present; move deletes the source (may dirty a tracked .gg.toml)`;
  keys `[↑/↓] select  [enter] do  [esc] close`.
- Enter on an action whose destination is non-empty shows
  `Overwrite <dst> ?` / `[y] overwrite  [n/esc] cancel`; y/enter runs, any
  other key returns to the list.
- Run: copy (atomic), remove the source on a move, close the popup back to
  the Settings menu, status `repo settings: <action label> done` (or
  `repo settings: copied but source not removed: <err>`, or
  `repo settings: <err>` with the popup still open), rebind the write target
  and reload everything (config, feed, status).

## Side effects on screen (captured, fx90 / cap90 / cap91)

| Action | TUI screen effect | Web port |
|---|---|---|
| open wizard | wizard screen replaces the Settings menu; cursor on row 1 | the External tools overlay opens with the wizard rows on top; cursor on row 1 |
| ↑/↓ | `>` moves; detail block follows the focused row | `.sel` row moves; detail follows |
| space | the focused row's box flips | the same (a configured row flips too, as in the TUI; it is skipped anyway) |
| enter, N written | wizard closes → Settings menu (cursor stays on External tools); status `external tools: 8 command(s) written to <path>` | overlay closes (back to the settings panel when opened from it); op line says the same text |
| enter, nothing | the same close; status `external tools: nothing to write (already configured or unchecked)` | the same |
| reopen after a write | rows `(configured)`, detail `already configured — skipped on apply` | the same |
| u on an update row | the update offer popup | the overlay's offer panel opens for that block (review/take/keep, already ported) |
| esc | back to the Settings menu, nothing written | overlay closes |
| open location | popup over Settings; present/absent rows; actions; cursor on the first | a location overlay over the settings panel |
| enter, empty dst | popup closes → Settings menu (filter + cursor kept); status `repo settings: Copy to private (user dir) done`; full reload | overlay closes; op line says the same; the settings panel re-reads (the settings-file row names the new active file); commits + status reload |
| enter, dst non-empty | the body becomes `Overwrite <dst> ?` + `[y] overwrite  [n/esc] cancel` | the same text and two buttons; y/enter overwrite, n/esc cancel |
| n | back to the action list | the same |
| move | after reopen the source row reads `absent`, two actions left | the same |

## Web design

- **Wizard = the External tools overlay** (`static/exttools.js`, opened from
  ☰, the palette, the notice's "Open external tools" and — new — the settings
  panel's "external tools" row, the TUI's first Settings row). Its "detected on
  this machine" section becomes the wizard: the TUI's title
  `External tools — detected`, rows, detail, hint, and a
  `write to global config` button (enter). The configured-commands inventory
  and the update offers stay below it (web extras).
- **Wire:** `GET /api/exttools` gains `wizard` = the TUI's row list in its
  order: `{tool, label, category, name, configured, checked (default),
  command (the GenerateCommand preview), status: "update"|"unsupported"|"",
  offer_id, declined}`. `POST /api/exttools/add` (writeGuard) takes
  `{rows: [{tool, category, name}]}` — never a command or a path — resolves
  each against a FRESH detection + `InstallTemplates(ctx, det, true)` (an
  unknown row → 400), skips configured ones, appends to
  `config.DefaultGlobalPath()` and replies `{written, path}`.
- **Location = a new overlay** `#repoloc` (`static/repoloc.js`) opened from the
  settings panel's "repo settings location" button, the TUI popup's texts.
  `GET /api/settings` gains `repo_location: {committed: {path, exists},
  private: {path, exists}, actions: [...]}`. `POST /api/settings/repo-location`
  (writeGuard) takes `{action, overwrite}`: action is one of
  `copy-to-private|move-to-private|copy-to-committed|move-to-committed`, paths
  are the server's own; an action not applicable now → 400; a non-empty
  destination without `overwrite` → 409 `{confirm: "Overwrite <dst> ?"}`. Reply
  `{msg}` with the TUI's status text.
- **Shared rules move to domain** (the J1 pattern): `domain.DefaultToolChecked`,
  `domain.RepoConfigActions`, `domain.RepoConfigEndpoints`,
  `domain.RepoConfigPaths` (committed + private for a Service), the action
  ids. The TUI calls through; its labels stay i18n.
- **Server after a write:** tools → `forgetToolStatuses()`; location →
  `resetFeed()`, `applyUIPolicies`, `forgetToolStatuses()` (block paths
  moved).
- **Hosted page (the terminal serves it):** the TUI caches its config and its
  repo-config write target; a page write would leave the terminal stale — after
  a page move-to-private a terminal Settings toggle would recreate the moved
  `.gg.toml`, and the terminal's wizard would re-append the page's blocks as
  duplicates. New `Host.SetConfigChanged(fn func(relocated bool))`: both POSTs
  call it after writing; the TUI then does what its own paths do — tools:
  `reloadToolConfig`; relocation: rebind `repoConfigPath`
  (`ActiveRepoConfigPath`, the workspace rule of `repoConfigPopup.run`) and
  reload (`loadGen++`/`loadCmd`, or `reloadOwnConfig` in a workspace).
  Fire-and-forget (a buffered channel; a full one already carries a reload).

## Design differences (documented)

- No `ctrl+w` display mode: the browser wraps.
- The wizard closes the whole overlay (the TUI returns to its Settings menu;
  the overlay's parent is whatever opened it).
- The empty-list literal is the TUI's verbatim (it names three tools while the
  catalog has six — a TUI minor, deferred).
- English only (the web is not localised).

## Out of scope

Editing a configured command (the TUI never does); the TUI's own staleness when
a separate `gg web` process writes (two processes; unchanged).
