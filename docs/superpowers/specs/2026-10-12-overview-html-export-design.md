# Overview HTML export — design

Date: 2026-10-12 · Branch: `feat/overview-html-export`

## Amendments (from planning, 2026-10-12 — these win over the text below)

1. No `data:` module imports: each reused module is inlined as a block-scoped
   classic script (`scopeModule`); the page loads no module at all.
2. Dark only, like gg web (style.css has no light set) — no `prefers-color-scheme`.
3. CLI flags `--out <path>|-` and `--force` (like `gg commit export-patch`), not `-o`.
4. TUI entry = the overview's `.` menu row (single and split pane), not a palette row.
5. `OverviewSource` has no `Dir`: the service is the agent overview's worktree;
   `domain.AgentOverviewSource(docs, id, root)` is the web/TUI door.
6. Web key `E` (x closes an overview) + an `E export html` foot chip.
7. Stored-review title: `Review overview: <label>` (the TUI's reviewLabel words).

## Goal

Export any overview as ONE self-contained `.html` file the user can hand to a
third party (to share an investigation). Opened from disk (`file://`), with no
gg and no network, it shows the same split view gg web shows: the overview on
the left, the code it links to on the right, anchors clickable.

Overviews covered (both already share one markdown + anchor grammar):

- **Agent overviews** — `agentdocs` in-memory documents (`gg session overview`,
  agent tours). Anchors point into the live working tree.
- **Stored review overviews** — the `overview` key of a stored review on a
  commit, range, preview, branch or pull request (`domain.ReviewOverview`).
  A "PR overview" is this kind, scoped to a PR set.

## User rulings (brainstorm 2026-10-12)

- **R1 Content = anchored files + their diffs.** Every file an anchor names is
  included in full at its frozen version, plus its diff; the reader toggles
  file ↔ diff like gg web's `D`. Files no anchor names are NOT included.
- **R2 Entry points: web, CLI and TUI**, all over one domain builder.
- **R3 No notes.** Review summaries, line notes, remarks and temporary agent
  notes are left out. A `note:t<n>` anchor still links to the note's lines,
  but the note's text is not exported.
- **R4 Approach: Go-built data blob + gg web's own pure renderers**, inlined
  into one page (not a Go re-render of markdown, not a stubbed SPA).

## What is frozen

| Overview kind | File version | Diff |
|---|---|---|
| Stored review (commit/range/preview/branch/PR) | the review's tip (`overviewFileLines` rules) | `ReviewFileDiff`: stored base → tip |
| Stored working review | the file on disk | HEAD → working tree |
| Agent overview | the file on disk in the overview's worktree | `WorkingFileDiff`: HEAD → working tree |

A file whose range leaves it unchanged (`Changed == false`) exports with no
diff; its `D` toggle says "unchanged in this range".

## Architecture

```
web button ─┐
CLI verb ───┼─► domain.ExportOverviewHTML(ctx, src) ──► []byte
TUI row ────┘        │  builds OverviewExport (data)
                     └─► overviewhtml.Render(OverviewExport) ([]byte, error)
                              └─ embeds internal/web/static (shared assets)
```

### 1. `internal/web/static` becomes a Go package

`internal/web/static.go`'s `//go:embed static` moves into a new
`internal/web/static/embed.go` (`package static`, `//go:embed *.js *.css
*.html` + any asset subdirs, `var FS embed.FS`). `internal/web` reads the same
files through it — served paths and bytes are unchanged. This is the only way
a package outside `internal/web` can embed the same `markdown.js`,
`splitdiff.js` and `style.css` (`go:embed` cannot reach a parent directory).
Archtest: `static` is a DAG leaf (stdlib `embed` only).

### 2. `internal/overviewhtml` (new, DAG leaf: stdlib + `web/static`)

- `type Export struct` — the page's data, JSON-tagged (the blob shape):
  - `Title`, `Repo` (remote repo NAME, no paths), `Kind` (`agent` |
    `review`), `Range` (`Left`/`Right` labels, e.g. short hashes or
    `HEAD`/`working tree`), `ExportedAt` (from `clock`, passed in), `Version`.
  - `Blocks` — the overview's `markdown.Parse` block tree (same wire shape as
    `/api/overview`'s `blocks`).
  - `Anchors []Anchor{Dest, Path, Start, End, Label, Missing}` — index-aligned
    with the `md-anchor` `data-a` keys the renderer emits.
  - `Files []File{Path, Status, OldPath, Lines []Line{Text, Tok}, Diff
    *DiffData, Placeholder string}` — `Tok` = the same `[[start,end,"cls"]]`
    runs `/api/file-content` sends; `DiffData` = the same row shape
    `/api/diff` sends (`left/right` text + `left_tok/right_tok` + kinds), so
    `splitdiff.js` reads it unchanged. `Placeholder` is set (and lines/diff
    empty) for a binary, image or too-large file.
- `Render(Export) ([]byte, error)` — one `html/template` page:
  - `<style>`: `static/style.css` inlined, plus a small `export.css`
    (layout only).
  - `<script type="application/json" id="gg-data">`: the blob
    (`json.Marshal` then `<` / `>` / `&` / U+2028/9 escaped so the blob
    cannot close its script tag).
  - The ES modules are inlined as `data:text/javascript;base64,…` imports:
    `markdown.js`, `splitdiff.js`, and the new page script `export.js`
    (owned by this package, not served by gg web). `export.js` imports the
    other two by their data URLs (the renderer rewrites the import
    specifiers, which are fixed strings checked by a test).
  - No other `<script src>`, `<link href>`, `fetch`, or `http(s)://` in the
    output (test-enforced; the only URLs are `data:`).
- Errors: `ErrTooLarge` when the rendered page exceeds `MaxBytes` (30 MiB; was 16, raised 2026-10-12 by the user),
  carrying the three largest files' paths and sizes.

### 3. The page (`export.js`, ~300 lines, vanilla ES module)

- Header: title · repo · range · "exported <date> by gg <version>".
- Two panes, overview left, code right, draggable divider; under 900 px wide
  (gg web's `splitFits`) the panes stack (overview above code).
- Overview pane: `mdHTML(blocks, esc, {anchors:true})`; a missing anchor
  renders as plain text exactly as in gg web.
- Clicking an anchor (or `n` / `p`, which walk the anchors in order) opens the
  anchor's file in the code pane, scrolls to its line and draws its range as
  a band (`.vline.vrange`, gg web's classes). A file-level anchor opens the
  file at its top.
- Code pane: a file header (path, status) and the file's lines with syntax
  tokens (`<span class="tk-…">`, the `renderCell` token loop, without the
  intraline/search parts). `D` (and a header button) toggles file ↔ diff;
  the diff uses `splitdiff.js`'s `unifiedRows` and `rowOfLine` so the
  anchor's band survives the toggle.
- A file list (the anchored files, in first-anchor order) above the code
  pane lets the reader open any included file directly.
- Theme follows `prefers-color-scheme` (style.css's existing token sets);
  no persisted state, no storage, no network.

### 4. Domain: `domain.ExportOverviewHTML(ctx, src OverviewSource) ([]byte, error)`

`OverviewSource` is one of:

- `{ReviewID}` — a stored review; text and anchors from `ReviewOverview`.
- `{Title, Text, Dir, NoteSpans}` — an agent overview: its markdown, the
  worktree it belongs to, and the `note:t<n>` → `{Path, Start, End}` spans
  the caller resolved (the note store lives in the live TUI / web process).

Steps (a Read under the repo gate):

1. Parse with `agentdocs.ParseOverview` (+ `markdown.Parse` for `Blocks`);
   resolve `note:` anchors through `NoteSpans` (unresolved → `Missing`).
2. Collect the distinct anchored paths (first-anchor order).
3. Per path: the frozen lines (table above) through the cached `Differ`'s
   syntax runs, and the diff (`ReviewFileDiff` / `WorkingFileDiff`). Binary,
   image or over gg web's per-file read cap → `Placeholder`. A path that no
   longer exists → its anchors are `Missing`, no file entry.
4. Fill header fields (`clock.Now()`, `buildinfo`, the remote repo name);
   no absolute or worktree paths enter the blob.
5. `overviewhtml.Render`.

An overview with no anchors exports fine (overview pane only, empty code pane
with "this overview links to no code").

### 5. Frontends

**Web** — an "Export HTML" button in the split view's overview header (agent
and stored overviews; also in the stored overview's single-pane mode).
`GET /api/overview-export?id=<f…>` (agent) or `?review=<id>` (stored) answers
the file as an attachment (`Content-Disposition: attachment;
filename="<slug>.html"`, slug from the title). The web host resolves agent
note spans from its own `agentdocs` store. Loopback/Origin guards as every
other route. The button routes through `runOnce`.

**CLI**

- `gg review export <id> -o <file.html>` — a stored review's overview (beside the existing `gg review show` / `save`).
- `gg session overview export <f-id> -o <file.html>` — an agent overview:
  the CLI fetches the text via the existing `overview_show` steer verb and the
  note spans via the existing note-list verb, then calls the domain builder
  in-process against its own worktree.
- Writes through `engine.ExportFile` via `domain.Execute` (asks before
  overwriting different bytes; `--force` answers yes). `-o -` writes to
  stdout. A review with no overview → exit 1, "review <id> has no overview".

**TUI** — an "Export overview as HTML" row in the overview viewer's menu and
the palette (when an overview — agent or stored — is the current view). It
opens the existing export-path popup pattern (`export_patch.go`), prefilled
with `<svc.ExportDefaultDir>/<slug>.html`, and writes through `engine.ExportFile`
(overwrite asks). All strings via `i18n.T` in the four bundles.

## Error handling

- Review gone / overview gone → the frontend's normal not-found message.
- `ErrTooLarge` → "overview export is <N> MB, over the 30 MB cap; largest
  files: a (x MB), b, c".
- A file read failing mid-export → that file becomes a `Placeholder`
  ("could not read: <err>"), the export still succeeds.

## Testing

- `overviewhtml`: render a fixture `Export`; assert one document, the data
  blob round-trips (`json.Unmarshal` of the extracted script), no
  `http://`/`https://`/`/api/`/`<script src`/`<link` in the output, the
  blob escaping (a `</script>` in file text stays inert), the module
  specifier rewrite found every import.
- `domain`: a real repo + a stored review with an overview anchoring two
  files and a missing path → two files, diffs base→tip, missing anchor
  marked, no note text anywhere in the blob, no absolute path in the blob;
  an agent overview with a `note:` span resolves to the note's lines.
- Web: handler test for both query forms (attachment header, 404s).
- CLI: e2e scenario — store a review with an overview, `gg review export`,
  assert the file exists and contains the anchored file names.
- TUI: the menu row exists only on an overview view; golden/i18n gates.
- Browser check (manual + headless CDP): open the exported file over
  `file://` in Chromium, click an anchor, assert the code pane is VISIBLE and
  shows the band; press `D`, assert the diff is visible. Firefox/Safari
  `data:`-module import support verified once at the start of implementation
  (fallback if not: inline the three modules as one concatenated module with
  `export`/`import` lines stripped by the renderer).

## Out of scope

- Notes of any kind (R3); files no anchor names; any write-back, comments or
  live refresh in the exported page; the TUI viewing an exported file;
  exporting several overviews into one file.
