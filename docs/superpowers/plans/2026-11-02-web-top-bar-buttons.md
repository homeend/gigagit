# gg web custom top bar buttons — Implementation Plan

> **For agentic workers:** executed by the session that wrote it, per the repo's
> CLAUDE.md (no implementer subagents), using superpowers:executing-plans. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Up to 10 configured palette commands appear as buttons after `refresh` in
gg web's top bar, overflowing into a `⋯` menu so the worktree path stays whole.

**Architecture:** `[web] toolbar` is a global-only config list reaching the page
through `/api/repo` and `/api/settings`. `palette.js` becomes the one catalog of
commands. A new `topbar.js` renders the buttons and calls a pure, import-free fit
function in `toolbarfit.js`. Settings edits the list.

**Tech Stack:** Go config + web handlers; ES modules with no bundler; Go tests that
run JS under node (see `diffpaintjs_test.go`); Playwright browser probe in the
scratchpad.

**Spec:** `docs/superpowers/specs/2026-11-02-web-top-bar-buttons-design.md`

## Global Constraints

- At most 10 buttons; the server rejects a longer list.
- `toolbar` is honoured from the global config only.
- With no custom button shown, the top bar's markup, geometry and tooltip are as today.
- The worktree path is cut only when every custom button is inside `⋯`.
- The row never wraps to a second line.
- `pull`, `push`, `refresh` can't be picked; a config entry naming one counts as unknown.
- Commands are matched by palette label; entries are only ever looked up, never executed.
- Probes run with scratch `XDG_CONFIG_HOME`/`XDG_STATE_HOME`.

## Review Focus

1. A single label wider than the free room: it goes to `⋯`; no later, narrower button
   jumps ahead of it, because order is kept (fit test, Task 3).
2. Every configured command unavailable or unknown: no separator, no `⋯`, today's bar
   (fit test + probe, Tasks 3, 6).
3. Availability changes after boot, such as opening or leaving a workspace: the bar
   re-renders without a reload (probe, Task 6).
4. A branch or worktree switch changes the row's width with no window resize: buttons
   re-fit and the path shows the new full path (probe, Task 6).
5. A hand-written multi-line `toolbar = [...]` survives a Settings save without
   breaking the config (test, Task 1).

---

### Task 1: Config key and writer

**Files:**
- Modify: `internal/config/config.go` (`WebConfig`, `overlayWeb`, `Load`)
- Modify: `internal/config/write.go`
- Modify: `internal/config/template.go` (`settingDocs`)
- Test: `internal/config/write_test.go`, `internal/config/config_test.go`

**Interfaces:**
- Produces: `WebConfig.Toolbar []string` (`toml:"toolbar"`);
  `func SetWebToolbar(path string, labels []string) error`.

- [ ] **Step 1: Write failing tests**
  - `TestLoadToolbarIsGlobalOnly`: global `toolbar=["a"]`, repo `toolbar=["b"]` → `["a"]`;
    only repo set → empty.
  - `TestSetWebToolbarRoundTrip`: quotes survive (`"view all notes…"`); empty list removes
    the key; other `[web]` keys and comments stay.
  - `TestSetWebToolbarReplacesMultiLineArray`: the file holds a multi-line array followed
    by `addr = "x"`; after the write, `Load` succeeds, returns the new list and keeps `addr`.
- [ ] **Step 2: Run** `go test ./internal/config -run Toolbar`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement.** In `Load`, keep `Toolbar` from layer 0 only (the `agents.spawn`
  pattern). `SetWebToolbar` first removes the key's whole value, from the key line to the
  line closing its `]`, inside `[web]` only, then writes one line through
  `setScalarLine` (`strconv.Quote` each). Add the `settingDocs` row, saying it is web-only,
  global-only and capped at 10.
- [ ] **Step 4: Run** `go test ./internal/config`. Expected: PASS, including the template
  coverage tests.
- [ ] **Step 5: Commit.**

### Task 2: Server wire

**Files:**
- Modify: `internal/web/settings.go`, the `/api/repo` handler
- Test: `internal/web/settings_test.go`

**Interfaces:**
- Consumes: `SetWebToolbar`, `WebConfig.Toolbar`.
- Produces: `toolbar: string[]` (never null) on `GET /api/repo` and `GET /api/settings`;
  `POST /api/settings {toolbar: string[]}`, a pointer field so absent ≠ empty, written to
  `config.DefaultGlobalPath()`.

- [ ] **Step 1: Write failing tests** (serial, since they read process XDG config):
  `TestSettingsToolbarRoundTrip` (POST then GET on both endpoints, and the file written is
  the global one, repo file untouched); `TestSettingsToolbarRejectsOverTen` (11 → 400,
  nothing written); `TestSettingsToolbarEmptyClears`.
- [ ] **Step 2: Run** `go test ./internal/web -run Toolbar`. Expected: FAIL.
- [ ] **Step 3: Implement.** Validate up front with the other fields. An over-long list in
  a hand-edited file is cut to the first 10 on GET; duplicates are dropped there too.
- [ ] **Step 4: Run** `go test ./internal/web -run 'Toolbar|Settings'`. Expected: PASS.
- [ ] **Step 5: Commit.**

### Task 3: One command catalog and the fit function

**Files:**
- Create: `internal/web/static/toolbarfit.js`
- Modify: `internal/web/static/palette.js`
- Test: `internal/web/toolbarfitjs_test.go`

**Interfaces:**
- Produces, `toolbarfit.js`:
  `resolveToolbar(labels, catalog) → [{label, status: "ok"|"unavailable"|"unknown", row}]`
  (`catalog` rows carry `available: bool`; `pull`/`push`/`refresh` resolve `unknown`;
  duplicates dropped, first 10 kept);
  `fitToolbar({room, fixed, sep, more, path, buttons}) → {shown, cutPath}`, all px: `shown`
  is how many leading buttons stay, `cutPath` whether the path must shorten.
- Produces, `palette.js`: `commandCatalog() → [{label, detail, run, available}]`;
  `runCommand(row)`, where `switch repo…` opens the repo list with no palette to return to;
  `openCommandPicker(rows, onPick)`.

- [ ] **Step 1: Write failing node tests:**
  - all fit → `{shown: n, cutPath: false}`, with no `more` counted;
  - one too many → `shown` drops until `fixed+sep+more+Σshown+path ≤ room`;
  - a wide middle button → `shown` stops before it, later narrow ones don't jump ahead;
  - nothing fits → `{shown: 0, cutPath: false}` when path+more fits, `cutPath: true` otherwise;
  - `buttons: []` → `{shown: 0, cutPath: false}` even when `fixed+path > room`;
  - `resolveToolbar`: statuses, `pull` unknown, duplicates, cap 10.
- [ ] **Step 2: Run** `go test ./internal/web -run ToolbarFit`. Expected: FAIL.
- [ ] **Step 3: Implement `toolbarfit.js`** (import-free).
- [ ] **Step 4: Refactor `palette.js`.** `paletteCommands()` becomes
  `commandCatalog().filter((r) => r.available)` and no row conditional is lost: `Leave
  workspace` is `workspaceOpen()`, `Switch to TUI` is `state.hosted`, `branch versions…` is
  `!featureDisabled("versions")`. Add a `"pick"` mode to `openPalette`/`runPaletteRow`
  (filter and keys as `"cmd"`, enter calls `onPick(label)`, closes through `closePalette()`).
- [ ] **Step 5: Run** `go test ./internal/web`. Expected: PASS.
- [ ] **Step 6: Commit.**

### Task 4: The top bar

**Files:**
- Create: `internal/web/static/topbar.js`
- Modify: `internal/web/static/index.html` (`<span id="top-custom">` between `#refresh-btn`
  and `#repo-worktree`; a `?` help row), `app.js` (import), `ops.js` (`loadRepo` sets
  `state.toolbar`), `polish.css`, wherever a workspace opens or closes

**Interfaces:**
- Consumes: `resolveToolbar`, `fitToolbar`, `commandCatalog`, `runCommand`, `showCtxMenu`,
  `elidePath`, `charWidth`.
- Produces: `renderTopBar()` (rebuild from `state.toolbar` + availability, then fit);
  `fitTopBar()`.

- [ ] **Step 1: Write the failing source pin** in `internal/web/toolbarfitjs_test.go`:
  `index.html` has `id="top-custom"` after `refresh-btn`; `app.js` imports `topbar.js`; the
  CSS has a `#top-custom` rule.
- [ ] **Step 2: Run it.** Expected: FAIL.
- [ ] **Step 3: Implement `topbar.js`:**
  - Render `.top-sep`, the buttons (label text, `title` = label + key), and `#top-more` (`⋯`).
    All absent when no button resolves `ok`; then also clear `#top`'s `has-custom` class and
    restore the path text and tooltip.
  - `fitTopBar`: show every button to measure, call `fitToolbar`, hide the rest, show `⋯`
    only if some are hidden. Path text comes from `state.worktree`, through
    `elidePath(full, cols)` when `cutPath`. Tooltip = full path + copy hint.
  - Re-fit synchronously from a `ResizeObserver` on `#top` and a `MutationObserver` on its
    subtree, discarding its own mutations with `takeRecords()`, so nothing paints twice.
  - `#top-more` click: `stopPropagation`, a second click toggles closed (as `#menu-btn`),
    `showCtxMenu` at the button's bottom-left, hidden commands in configured order.
  - Button click: `runCommand(row)`, then `blur()` so enter can't re-fire it.
- [ ] **Step 4: CSS.** `#top-custom { display: contents }` or a flex child; buttons match
  the fixed ones without mask icons. Under `#top.has-custom`: `#repo-worktree` no-shrink,
  no-wrap; `#repo-branch` `min-width: 0` + ellipsis.
- [ ] **Step 5: Call `renderTopBar()`** after the boot `loadRepo`, and where a workspace opens
  or closes. Disabled features are known by then, since `preflightGate` runs before `loadRepo`.
- [ ] **Step 6: Run** `go test ./internal/web`. Expected: PASS.
- [ ] **Step 7: Commit.**

### Task 5: Settings section

**Files:**
- Modify: `internal/web/static/settings.js`, `internal/web/static/index.html` (help)
- Test: `internal/web/settingsjs_test.go`

**Interfaces:**
- Consumes: `POST /api/settings {toolbar}`, `resolveToolbar`, `commandCatalog`,
  `openCommandPicker`, `renderTopBar`.

- [ ] **Step 1: Write the failing pin:** `settings.js` calls `renderTopBar(` and
  `openCommandPicker(` (a saved list must reach the bar).
- [ ] **Step 2: Run it.** Expected: FAIL.
- [ ] **Step 3: Implement** a **top bar buttons** section: rows with label, the status note
  ("not available here" / "unknown command"), ↑ ↓ ✕ (first ↑ and last ↓ disabled), **add…**
  (disabled at 10 with a note). Picker rows exclude chosen commands and the fixed three,
  showing unavailable ones with that note. Each change: POST the whole list, update
  `state.toolbar`, `renderTopBar()`, re-render keeping pending text edits (the `setOpt`
  path). A refused save shows in `.serr` and changes nothing. Caption: "global — applies to
  every repository".
- [ ] **Step 4: Run** `go test ./internal/web`. Expected: PASS.
- [ ] **Step 5: Commit.**

### Task 6: Browser probe, docs, gate

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `docs/web-tui-parity.md`, `docs/CLAUDE-details.md`
- Scratchpad only: fixture repo + `probe.mjs`

- [ ] **Step 1: Build** `<worktree>/bin/gg`; serve a fixture with scratch XDG dirs and a
  config holding 10 long and short labels.
- [ ] **Step 2: Probe in Firefox and Chromium**, measuring geometry at widths 1900, 1200, 900,
  700, 500:
  - `#top` height is constant and the row doesn't wrap;
  - the path is whole while any button shows, and cut in the middle only with all in `⋯`;
  - `⋯` is present exactly when something is hidden, and its menu sits below it in order;
  - no toolbar: the separator is absent and geometry matches the same build with `toolbar`
    unset;
  - all unavailable: as no toolbar;
  - button click runs the command (`view all notes…` opens its window), `switch repo…` esc
    closes;
  - Settings: add, move, remove update the bar at once, and the global file holds the list;
  - repo `.gg.toml` toolbar has no effect;
  - alt+w to a longer-path worktree re-fits; workspace open/leave shows or hides `Leave
    workspace`.
- [ ] **Step 3: Run the probe once with the guard removed** (fit always shows every button)
  and see the path assertion fail, then restore.
- [ ] **Step 4: Docs:** CHANGELOG entry, README web section, parity doc's web-only list, one
  CLAUDE-details paragraph. Commit.
- [ ] **Step 5: Gate.** `gofmt -l internal cmd e2e`, then `./test.sh race` from a clean tree,
  logged to a file. Expected: log says `all green` with zero FAIL lines.
- [ ] **Step 6: Whole-branch review** by a fresh read-only subagent on the most capable model;
  fix findings, re-run the gate if code changed.
- [ ] **Step 7: Sync.** Merge `main` into the branch if it moved and re-run the gate. Build
  `bin/gg`, give its absolute path, and ask to merge.
