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

- `WorktreeFileExists(ctx, path) (bool, error)` — lstat at the worktree root
  through the existing worktree-path guard (a path escaping the tree is an
  error, not `false`). A directory is `false`; a symlink counts as present
  (lstat). No git invocation.
- `WorktreeFileContent(ctx, path) ([]byte, error)` — reads the file from disk
  via the same guard; missing file → a typed not-found error whose message is
  `<path> is not in the working tree`. Content over the existing viewer cap
  is truncated the way the HEAD viewer truncates.
- `ContentLink(path) (string, error)` — builds the link (repo name resolution
  shared with the existing `linkFor`/`gg link` builders) after the existence
  check. Frontends call this; nobody assembles `?view=content` by hand.

## TUI

**Menu row.** `contextFileLinkRow` returns a row
`copy-file-link` / `i18n.T("Copy file link")`, spliced right after
"Copy link" by `insertCopyLinkRow` (extended to place both). Offered wherever
the `.` menu already offers "Copy link" on a **file** row: the commit files
view (commit, stash, shelf-commit lists), and the Files and Staged panels.
For a renamed row the new path is used. Not offered on commit rows or
directory rows.

Running the row fires `ContentLink` off the UI thread (a `tea.Cmd`); its
message either copies (existing copy path, status `Copied link: %s`) or sets
the status line to `%s is not in the working tree`. Both strings go into all
four i18n bundles.

**Steered landing.** `steerNavigate` gets a new case, ordered BEFORE the
status-file case: `c.HintKind == "view"` (with `c.File != ""`, no target).
It calls `steerToPanels`, pushes a `contentPopup` titled `View <path>` with
`(loading…)`, loads `WorktreeFileContent` async, and answers
`opened <path>` when the content arrives (or the not-in-working-tree failure,
which removes the loading popup). `c.Line` is carried in the pending state
for v2 and unused in v1. A content popup opened this way behaves like any
other: esc closes it back to the panels.

If no TUI is running, `gg open` starts one positioned on the link (existing
behaviour) and the same landing runs.

## Web

**Menu row.** `copy file link` beside `copy gg link` in the file-row menu of
the commit file list and the working-tree list. It calls a new
`GET /api/content-link?path=<p>` (domain `ContentLink`; runs no git beyond
the cached repo name) and hands the result to the one `copyLink` helper in
`static/links.js`. A 404 (`not in the working tree`) shows in the status line
the way other copy failures do. Wire path goes through the existing allowlist
resolution.

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
- `gg link resolve` describes it as `file <path> (working tree content)`.
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
- `domain`: `WorktreeFileExists` / `WorktreeFileContent` — present, missing,
  directory, symlink, escaping path, dirty file returns DISK bytes (not HEAD).
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
