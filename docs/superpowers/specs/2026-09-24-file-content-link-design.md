# File content links — design

Status: agreed in brainstorming 2026-09-24. v1 = this document; v2 (line focus)
is designed for but not built.

## Goal

From a file row's context menu, copy a `gg://` link that names **the file as
it is on disk in this worktree** — no commit, no diff — and let an agent open
that link in the user's gg, where it lands in the read-only content viewer.

## Rulings (do not re-ask)

1. A new menu row **"Copy file link"** sits beside the existing "Copy link" on
   file rows. Both frontends (TUI and web) get the row.
2. The link is commit-less. Before copying, gg checks the file exists **on
   disk** in the current worktree (not "in HEAD"). A missing file copies
   nothing and shows `<path> is not in the working tree` in the bottom bar /
   status line.
3. "Open" means the **content viewer** (the TUI's `contentPopup`, as used by
   "View file (content at this commit)"), never a diff.
4. Agents open it with the existing `gg open <link>` / `gg session navigate`.
5. The link carries an explicit marker `?view=content`; the plain
   `gg://<repo>/<path>` keeps its current meaning (working-tree diff).
6. Lines: the file-row link has no line. v2 lets the viewer copy
   `…/<path>:<line>?view=content` and lets an agent open a file focused on a
   line. v1 PARSES and carries the line but does not scroll to it.
7. Web: generates the link (menu row) but does not land it. A steered web page
   refuses a content link; `gg open --web` refuses it up front.

## Link grammar

```
gg://<repo>/<path>?view=content            v1
gg://<repo>/<path>:<line>?view=content     v2 (parsed in v1, line ignored)
```

- `view` joins the closed hint-kind set `model.linkHintKinds`
  (`internal/model/link.go`); `content` is its only accepted id. Any other id
  (`?view=blame`) is refused at parse, like an unknown kind today.
- Valid only on a **working-tree** link: no `@…` target. `?view=content`
  with a commit, `@staged`, `@ref:`, pair or preview target is a parse error
  (exit 2 from every CLI verb that takes a link), so one link never means two
  things.
- Also requires a path: `gg://<repo>?view=content` is refused.
- The `:<line>` side is always the new side; `old:` with `?view=content` is
  refused (a file on disk has one side).
- Round-trip: `ParseLink(l.String()) == l` for all of the above.

The hint slot's comment ("the UI surface a link was copied from") widens to
"the surface a link was copied from or lands on"; that is the only semantic
stretch.

## Domain

No new service methods: the two existing working-tree queries already answer
both questions without a git invocation, behind the path guard that refuses
anything escaping the tree.

- `WorktreeFilesPresent(ctx, []string{path})` — the existence check (lstat, so
  a dangling symlink counts as present; an escaping path is reported absent).
- `WorktreeFile(ctx, path)` — the disk bytes the viewer shows.
- `ResolveLink`: an address-less `?view=content` link (the local form only
  learns its path here) is refused — `a content link needs a file path`.
- Every link producer builds the link itself, the way it already builds
  every other link: the TUI with `buildLinkFor`, the CLI with `buildLink`,
  the web with `linkFor` (links.js). The hint is `model.ContentHint`.
- `compare` IGNORES the hint (spec §3.3 rule 1): a content link compared is
  simply the working-tree file.

## TUI

**Menu row.** `contextFileLinkRow` returns a row
`copy-file-link` / `i18n.T("Copy file link")`, spliced right after
"Copy link" by `insertCopyLinkRow` (extended to place both). Offered wherever
the `.` menu already offers "Copy link" on a **file** row: the commit files
view (commit, stash, shelf-commit lists), and the Files and Staged panels.
For a renamed row the new path is used. Not offered on commit rows or
directory rows.

Running the row fires the `WorktreeFilesPresent` check off the UI thread (a `tea.Cmd`); its
message either copies (existing copy path, status `Copied link: %s`) or sets
the status line to `%s is not in the working tree`. Both strings go into all
four i18n bundles.

**Steered landing.** `steerNavigate` gets a new case, ordered BEFORE the
status-file case: `c.HintKind == "view"`. It checks presence with a
deadlined `WorktreeFilesPresent` on the Update thread (a stat, no git — the
`updateThreadCtx` seam `steerNavigateRef` already uses); missing → the
navigate FAILS with `<path> is not in the working tree` and nothing moves.
Present → `steerToPanels`, push a `contentPopup` titled `View <path>` with
`(loading…)`, load `WorktreeFile` async (the existing `fileContentLayerMsg`
fills it), and answer `opened <path>` through `navigateLanded` (which gets a
silent `"view"` arm: the landing IS the hint). `c.Line` is ignored in v1.
esc closes the popup back to the panels.

If no TUI is running, `gg open` starts one positioned on the link (existing
behaviour) and the same landing runs.

## Web

**Menu row.** `copy file link` beside `copy gg link` in every file-row menu
(the one `registerRows("file", …)` contributor in `static/links.js`). It asks
a new `GET /api/worktree-present?path=<p>` (`{"present": bool}`, backed by
`WorktreeFilesPresent`; no git), then builds the link with `linkFor(…,
{state: "unstaged", hint: {kind: "view", id: "content"}})` and hands it to
the one `copyLink` helper. Missing → `opLine("<path> is not in the working
tree", true)`, the same status line every copy reports on.
`/api/link-base`'s `origin` is not set for a `view` hint (it is not a saved
surface).

**Landing.** A steered navigate carrying `hint_kind: "view"` answers a
failure `content links are not supported in gg web yet`. `gg open --web
<content link>` exits 2 with the same text before touching any page or
starting a server.

## CLI / agents

- `gg link --content <path>` prints the content link; exit 1 +
  `<path> is not in the working tree` when missing. Mutually exclusive with
  `--cached`, `--rev`, `--preview`, `--ref`, `--pair`, `--bookmark`,
  `--shelf` (exit 2). A `:<line>` suffix is refused in v1 (exit 2) so v2 can
  add it without a behaviour change on existing input.
- `gg open <content link>` and `gg session navigate <content link>` need no
  new flags; the hint travels in the existing steer `HintKind`/`HintID`.
- `gg link resolve` resolves it like any working-tree file link (its JSON
  carries `hint_kind: "view"`).
- Every other link-taking verb (`gg diff`, `gg note add|list|apply|clear`,
  `gg session highlight add`, `gg show`) refuses a content link with exit 2 —
  it names a file's content, not a diff.
- `internal/agentskill/using-gg.md`: document the link form, `--content`,
  and "ask for a file → `gg link --content <path>` → `gg open <link>`".
  Bump `agentskill.Version`.

## v2 (line focus) — not built, designed for

- The content viewer gets a line cursor (like the diff view's) and a
  "Copy file link" row in its own `.` menu that copies
  `…/<path>:<line>?view=content`.
- The steered landing scrolls to and highlights `c.Line`.
- `gg link --content <path>:<line>` stops refusing.
- Web: a content viewer overlay + landing (its own design).

## Error handling

| Case | Result |
|------|--------|
| file missing on copy (TUI/web) | status `<path> is not in the working tree`, clipboard untouched |
| file missing on `gg link --content` | exit 1, same text |
| file missing on steered landing | navigate fails with same text; CLI exit 1 |
| path escapes worktree | refused (error, not "missing") |
| `?view=content` with a target / `old:` / no path / other id | parse error, exit 2 |
| content link to web | refused, exit 2 (`gg open --web`) / navigate failure (steer) |
| content link to diff/note/show verbs | exit 2 |

## Testing

- `model`: parse/print round-trip incl. `:<line>`; every refusal row above.
- `domain`: `ResolveLink` refuses an address-less content link.
- `cli`: `gg link --content` output, flag conflicts, missing exit 1, `:<line>`
  refused; `gg open --web` refusal; other verbs refuse the link.
- `tui`: row placement after "Copy link" in each surface; copy path and
  missing-file status; steered landing opens the popup with disk content of a
  modified file; missing-file landing fails and leaves no popup; i18n gates.
- `web`: handler test (200 link / 404 missing / escape refused); steer refusal;
  browser probe asserting the row is VISIBLE and the copied text is right
  (run against the unfixed build first).
- `e2e`: scenario `gg link --content` → the printed link parses with
  `gg link resolve`.

## Docs

CHANGELOG, README (menu row + `gg link --content`), `docs/CLAUDE-details.md`
(the `view` hint kind and its target restriction), `using-gg.md` + version.

## Addendum (2026-09-24): land in the View-file PREVIEW machinery

User testing: the landing opened the file finder's plain `contentPopup` — no
syntax colouring, no line cursor, no select/copy — while "the viewer" the user
means is the files view's **View file** preview (`openPreview`). Ruling (user
chose B): the landing reuses that preview machinery.

- **Refactor, no behaviour change.** The preview's functions
  (`movePreviewCursor`, `previewSelectKey`, `previewSearchKey`,
  `previewCopyLineRows`) stop reading `m.filesPreview` + the right-column
  geometry directly and ask one accessor, `m.activePreview()`, for the
  focused preview and its size (rows, inner width). The files view answers
  exactly as today.
- **A full-screen window, `fileViewer`** (a layer, registered in
  `isFullScreenLayer`): title `View <path> (working tree)`, the same box the
  preview draws (`renderPreviewBox`, extracted from `renderFilePreview`), the
  same loader (`loadFileContentSrcCmd` over `WorktreeFile` — syntax colouring
  honouring `[ui] diff_syntax`), and the same keys: alt+↑/↓ line cursor,
  ↑/↓/pg scroll, space/space/enter select + copy, `/ @ ] [` search, ctrl+w
  display mode, shift+←/→ pan, `.` menu (Copy line / Copy selected lines /
  the file's copy rows / Copy link / Copy file link), esc closes (back to
  whatever was beneath). Every other key is swallowed.
- Content links (`steerNavigateContent`) open `fileViewer`; the plain-popup
  path added earlier is removed.
- v2 unchanged in intent: `:<line>` sets the viewer's cursor; the viewer's
  Copy file link then carries the cursor line.
