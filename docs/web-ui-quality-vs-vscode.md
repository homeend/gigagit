# gg web vs a VS Code-class web UI — what code-server does, what gg would need

Investigation, **2026-10-22**, against `main` `bf689f8e`. Research input for a
brainstorm, not a spec: nothing here is decided.

Reference: code-server `main` (`github.com/coder/code-server`), VS Code `main`
(`github.com/microsoft/vscode`), both read from shallow clones on this date.

## 1. The short answer

**code-server contributes no UI.** It is an Express + WebSocket server, a
login page, an error page and 30 patch files applied to VS Code itself, which
it pulls in as the git submodule `lib/vscode` and builds with VS Code's own
`vscode-reh-web` gulp target ("remote extension host, web"). Everything the
user sees after login is VS Code's workbench, served as-is.

| what | size |
|---|---|
| code-server's own browser CSS (`src/browser/pages/*.css`) | 3.5 KB |
| code-server's own browser HTML (login, error) | 4.1 KB |
| code-server patches over VS Code (`patches/*.diff`) | 30 files, mostly server-side: base path, auth, storage, marketplace, telemetry, service worker |
| VS Code base widget library (`src/vs/base/browser/ui`) | 1.7 MB, 37 widget packages |
| VS Code workbench shell parts (`src/vs/workbench/browser/parts`) | 2.4 MB |

So "the same class of interface as code-server" means "the same class of
interface as VS Code's workbench". Section 2 lists what that class consists
of, as the user sees it; section 3 the mechanisms behind each; section 5 what
gg would introduce.

## 2. What the user sees — VS Code vs gg web today

Screens compared: gg web at 1440×900 on a fixture repo (solo list, commit
clicked, diff, palette, commit context menu, help). Ordered by how much each
item contributes to the "looks like an IDE" vs "looks like a terminal"
impression.

| | VS Code workbench | gg web today |
|---|---|---|
| **Typeface** | System UI font for all chrome (`-apple-system` / `Segoe UI` / `system-ui`), 13px / 1.4; monospace **only** inside editors and terminals | `JetBrains Mono` 13px / 1.5 for **everything**: sidebar, buttons, menus, help, status strip |
| **Colour** | ~250 registered colour tokens, each with light / dark / high-contrast values; themes are JSON files; every widget paints from tokens only | 12 root tokens + 68 distinct hardcoded hex values (153 occurrences in `style.css`); one dark theme; no light theme, no theme switch |
| **Icons** | Codicon font (hundreds of glyphs) on every row, button, tab, section header, status item, menu row | No icon set at all (0 SVGs in `index.html`); a few Unicode glyphs (⎇ ⟳ ☰ ◆) and text labels |
| **Shell** | Title bar · 48px activity bar · sidebar with view containers · editor area with tabs · panel · 22px status bar; sashes 4px with hover highlight; shadow + corner-radius tokens per surface | Top bar (repo · branch · pull / push / refresh · path) · 3-column grid (sidebar · commits · files/diff) with 5px resizers · bottom strip of key hints (the TUI footer) |
| **Lists / trees** | One virtualised list widget: 22px rows, twisties, indent guides, hover row, selection + focus + inactive-selection tokens, inline action toolbar on hover, count badges, ARIA roles | Plain `<ul>` / `<table>` with every row in the DOM; selection by a class; actions in the right-click menu only; section headers are text with a chip |
| **Command palette** | Quick input: 600px, large corner radius, xl shadow; rows show icon, label, description, **keybinding label**, fuzzy-match highlight; grouped by separators | Centred box with input + plain rows; no shortcut column, no match highlight, no groups |
| **Menus** | Context menus with icons, shortcut column, submenus, disabled state, separators; hover tokens | Text rows with separators; destructive rows in red; no shortcuts, no icons, no submenus |
| **Scrollbars** | Custom overlay scrollbar: thin slider, fades out after 800ms, inset shadow at a scrolled edge | Native scrollbars (Firefox overlay on the user's machine); per-side horizontal bars in the diff |
| **Motion / focus** | 100ms opacity + background transitions, a `disable-animations` switch; one `focusBorder` token drawn as an outline on every focusable widget | 0 `transition` rules; 6 `focus-visible` rules; focused pane = 2px inset accent line |
| **Code / diff** | Monaco editor: minimap, folding, bracket colours, inline + side-by-side diff with a shared gutter, word-wrap, sticky scroll | Hand-rolled side-by-side table with chroma tokens, hunk picker, notes, stacked and symmetric views — functionally richer than Monaco's diff for gg's purposes, visually a table |
| **Hovers** | Rich hover widgets with markdown, delayed show, keyboard-reachable | `title=` attributes |
| **Dialogs / notifications** | Dialog widget with icon, primary / secondary buttons, keyboard focus trap; notification toasts bottom-right with progress + actions | Layer-stack prompts and a "Problem" panel at the bottom of the commits pane |

What gg web does **not** lack: a command palette, context menus, a layer
stack, resizable panes, persisted UI state (`/api/uistate`), syntax tokens, a
markdown renderer, a complete keyboard model, live refresh. The gap is
presentation, not features (`docs/web-tui-parity.md` covers features).

## 3. How VS Code achieves each item

1. **Colour tokens.** `registerColor(id, {dark, light, hcDark, hcLight})` in
   `src/vs/platform/theme/common/colors/*.ts` (1265 lines, 253 registrations).
   `asCssVariableName` turns `editorSuggestWidget.background` into
   `--vscode-editorSuggestWidget-background`; the theme service writes one
   stylesheet of those variables when the theme changes. Widget CSS never
   contains a hex value. Shadows and corner radii are tokens too
   (`--vscode-shadow-sm…xl`, `--vscode-cornerRadius-medium/xLarge`).
2. **Typeface split.** `src/vs/workbench/browser/media/style.css` sets a
   per-OS UI font on `.monaco-workbench`; only the editor and terminal set a
   monospace family.
3. **Icons.** `@vscode/codicons`: one TTF font + `.codicon-<name>` classes,
   16px/1 line box, generated from the icon registry so themes can remap an
   icon. Licence: icons CC-BY-4.0, code MIT.
4. **Widgets.** `src/vs/base/browser/ui`: `list` (virtualised through
   `RangeMap`, keyboard + ARIA), `tree`, `sash` + `splitview` + `grid`
   (resizable layout with persisted sizes), `scrollbar` (overlay), `contextview`
   + `menu`, `inputbox` / `findinput`, `button`, `dropdown`, `hover`, `dialog`,
   `toolbar` / `actionbar`, `progressbar`, `breadcrumbs`, `countBadge`,
   `iconLabel`, `highlightedlabel`, `keybindingLabel`. **Not published as a
   package** — only the editor ships (as `monaco-editor`), so these cannot be
   imported; a project either vendors VS Code's source tree (what code-server
   does) or writes its own.
5. **Quick input.** `src/vs/platform/quickinput` (15 KB CSS alone): the
   palette, pickers and prompts are one widget fed by the command registry,
   so every command shows its keybinding and fuzzy highlights for free.
6. **One registry for commands + keybindings + menus.** A command is declared
   once with its id, title, icon and default keybinding; the palette, every
   menu, every keybinding label and the status bar read that registry. gg web
   has the same idea split across `palette.js`, `menus.js`, `keys.js` and
   `help.html` rows.
7. **Shell parts.** `workbench/browser/parts/{titlebar,activitybar,sidebar,
   editor,panel,auxiliarybar,statusbar}` laid out by the grid widget; the
   status bar is 22px / 12px font, the activity bar 48px.
8. **Editor.** Monaco. Also the one piece that *is* packaged: `monaco-editor`
   0.57.0, MIT, 101 MB unpacked, needs a module loader and web workers.

## 4. gg web's starting point (facts that size the work)

- 1.5 MB of hand-written ES modules and CSS, no bundler (a standing rule),
  embedded in the Go binary; `style.css` 157 KB, `files.js` 250 KB,
  `viewer.js` 128 KB, `sidebar.js` 70 KB; 44 `z-index` values.
- The TUI already owns a theme system: `internal/theme` has 55 named roles
  (`roleFields` in `override.go`), `Dark` / `Light` / `Terminal` catalogues
  and `[themes.<name>]` per-role overrides in config. **The web does not
  read it** — no theme endpoint exists in `internal/web/*.go`; the 12 root
  variables in `style.css` are hand-copied from the TUI's Dark values and
  pinned by `style_groups_test.go`.
- The web was built as "the TUI in a browser": same keys, same footer hint
  strip, same monospace look, screens ported exactly. That is why it reads
  as a terminal; it is a design choice, not neglect.

## 5. What gg would introduce, by payoff

Ordered so each step changes the look on its own and none depends on a
later one. A visual redo replaces the previous round's CSS (standing
ruling), so each step is a rewrite of the rules it touches, not an overlay.

### 5.1 Theme bridge (small, largest colour effect)

Serve `internal/theme`'s resolved roles to the page (inline in
`index.html` or `GET /api/theme`) and emit them as CSS variables; rewrite
`style.css` so every colour is a `var()`. Outcomes: the 68 hardcoded values
disappear, the TUI's light theme and the user's `[themes.<name>]`
overrides apply to the web for free, and a theme switch becomes one
stylesheet swap. The TUI's 55 roles are coarser than VS Code's 250; new
web-only roles (hover row, inactive selection, widget shadow, focus
border, badge, input border) would be added to the Go catalogue, not to
the CSS, so both frontends keep one source. `style_groups_test.go` then
pins the bridge rather than copied hex.

### 5.2 Typeface split (small, largest "not a terminal" effect)

System UI stack for chrome (sidebar, bars, menus, palette, help, buttons,
inputs); monospace kept only in the diff, blame, file preview, console and
commit-message box. Row height becomes a token (22px in VS Code) instead
of falling out of line-height.

### 5.3 Icon set (small–medium)

One inline SVG sprite, used on sidebar section headers, row kinds (branch,
remote, worktree, tag, stash, bookmark, shelf, PR), file status letters,
toolbar buttons, menu rows, palette rows and status items. Candidates:
Codicons (CC-BY-4.0, attribution goes into `./build.sh notices`), Lucide
(ISC) or Tabler (MIT). Icons must keep the East-Asian-width-neutral rule
only in the TUI; the web has no such constraint.

### 5.4 Shell (medium)

- A 22px status bar replacing the key-hint strip: branch, worktree, counts,
  live-refresh state, running op, notices. The key hints move to tooltips
  and to a keybinding column in the palette and menus.
- Sidebar section headers with a hover action toolbar (the `+`, `git`,
  `date↓` chips become icon buttons that appear on hover).
- Shadow and corner-radius tokens on every floating surface (palette,
  menus, prompts, help); sash hover highlight on the resizers; focus ring
  token on every focusable element; 100ms transitions with a
  reduced-motion opt-out.

### 5.5 Widgets (large — the real work)

- One list/tree component used by the sidebar sections, the commit list and
  the file lists: fixed row rhythm, hover / selection / focus tokens, inline
  actions, badges, ARIA roles, and virtualisation for the commit list (the
  web already pages ~250 commits at boot; a virtual list keeps a 100k-commit
  repo scrolling).
- Quick input restyle: keybinding labels, match highlights, groups,
  description column — fed from a single command table that `palette.js`,
  `menus.js`, `keys.js` and the help rows all read.
- Context menus with icons, shortcut column and submenus; hover widgets
  instead of `title=`; a dialog widget for prompts and decisions;
  notification toasts for op results instead of the Problem panel.
- Overlay scrollbars are optional: `scrollbar-width: thin` +
  `scrollbar-color` tokens + edge shadows give most of the effect without
  a custom scroller.

### 5.6 Diff (restyle only)

Keep gg's own diff engine (hunk picker, notes, stacked, symmetric, link
landing are all built on it). Restyle the table: gutter tokens, hunk
headers as rows with fold twisties, sticky file header, change markers in a
side rail. **Monaco is not recommended**: 101 MB unpacked, a loader plus
workers against the no-bundler rule, and it would replace a diff that
already does more for gg than Monaco's.

### What not to do

- Vendor VS Code's widget tree the way code-server does: it is the whole
  of VS Code (TypeScript build, gulp, Electron-shaped services) and the
  project rule is no outside code; MIT would allow copying CSS, but
  reimplementing against gg's own tokens is the only path that keeps one
  theme source for TUI and web.
- Add a bundler or a framework to get components: the standing rule is
  plain ES modules, and the component needs above are few enough (list,
  menu, quick input, hover, dialog) to write once.

## 6. The decision this needs first

The web is a port of the TUI; its terminal look (monospace chrome, footer
key strip, chips) is the port being faithful. A VS Code-class look means
the web **diverges visually from the TUI** while keeping functional parity.
Two readings of "the same class of interface":

- **IDE chrome.** Everything in §5: UI font, icons, status bar, widgets.
  The web stops looking like the TUI and starts looking like a git IDE.
- **Terminal-chic polish.** Monospace stays; §5.1 tokens, §5.3 icons,
  §5.4 shadows / focus / motion, §5.5 list rhythm are added. The web keeps
  reading as the TUI's twin but gains the finish.

Which one, and whether the footer key strip survives, is the user's call
before any widget is designed; §5.1 and §5.3 are the same under both.

## Appendix — how this was measured

```bash
# code-server: own UI vs VS Code
git clone --depth 1 https://github.com/coder/code-server.git
cat .gitmodules; ls patches; wc -c src/browser/pages/*
# VS Code: tokens, widgets, parts
git clone --depth 1 --filter=blob:none --sparse https://github.com/microsoft/vscode.git
git sparse-checkout set src/vs/base/browser/ui src/vs/platform/theme/common src/vs/workbench/browser/parts src/vs/workbench/browser/media
grep -c 'registerColor(' src/vs/platform/theme/common/colors/*.ts
# gg web
grep -c -- '^\s*--' internal/web/static/style.css                     # 12 tokens
grep -oE '#[0-9a-fA-F]{3,8}\b' internal/web/static/style.css | sort -u | wc -l   # 68 hex
grep -c 'transition' internal/web/static/style.css                    # 0
grep -c '^\t{"' internal/theme/override.go                            # 55 TUI roles
```

Screens: gg web on a fixture repo, Playwright Chromium 1440×900 — solo
list, commit clicked, diff, palette (`ctrl+k`), commit context menu, help.
