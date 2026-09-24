# Open files — Plan 1: the open-file document (refactor)

> **For agentic workers:** executed inline by the session that wrote it (NO subagents — project rule). Steps use `- [ ]`.

**Goal:** one `openFile` document type behind both the files view's right-column preview and the full-screen `fileViewer`, with load results routed to their document by tag — no user-visible behaviour change except one bug fix (below).

**Architecture:** `openFile{src, path, p *contentPopup, tag, pendingLine}` + ONE `fill`. `fileViewer` embeds `*openFile` (so `fv.p`, `fv.tag`, `fv.pendingLine` keep compiling); `m.filesPreview` becomes `*openFile` (call sites `m.filesPreview.X` → `m.filesPreview.p.X`); `m.filesPreviewTag` is deleted (the doc carries its tag). `fileContentMsg` finds its document via `m.liveDoc(tag)` — every `fileViewer` on the stack, then the preview.

**Spec:** `docs/superpowers/specs/2026-09-24-open-files-design.md` → Stage 1.

## Global Constraints

- No new user-visible strings except the two viewer titles for commit/shelf sources (i18n: all four bundles).
- `internal/tui` never imports `internal/git`.
- Tests call `t.Parallel()`.

## Review Focus

1. A covered `fileViewer`'s late load must fill IT, not be dropped (today `layerOf` returns only the topmost viewer — two agent opens in a row leave the first stuck on "(loading…)"). Task 2 pins it.
2. A stale preview load (user opened another file) must still be dropped.
3. The preview's search re-find after a load uses the PREVIEW's geometry, the viewer's uses the viewer's.
4. `pendingLine` still lands (and still reports past-EOF) through the unified fill.
5. Tags are unique per document even for the same path (a counter), so two documents of one file never cross-fill.

---

### Task 1: `openFile` + unified fill

**Files:** Create `internal/tui/open_file.go`, `internal/tui/open_file_test.go`.

**Produces:**
```go
type fileSourceKind int // srcWorktree, srcCommit, srcShelf
type fileSource struct{ kind fileSourceKind; rev string } // rev: sha (commit) | shelf id
type openFile struct {
	src         fileSource
	path        string
	p           *contentPopup
	tag         string // unique per document: "<kind>:<rev>:<path>#<n>"
	pendingLine int    // 1-based line to land once lines arrive; 0 = none
}
func newOpenFile(src fileSource, path string) *openFile // p = "(loading…)" placeholder, fresh tag
func (d *openFile) fill(msg fileContentMsg, rows, innerW int) (notice string)
func (d *openFile) key() string // src + path: the reuse key (Stage 2)
```

- [ ] Tests (RED): `TestOpenFileTagsAreUnique` (two docs, same src+path → different tags, same key); `TestOpenFileFillSetsLinesAndResetsCursor` (cur/sel/lsel reset); `TestOpenFileFillError` (the `(load failed: …)` line); `TestOpenFileFillLandsPendingLine` (60 lines, line 40 → cur 39, centred; consumed); `TestOpenFileFillPastEOFNotice` (5 lines, line 99 → cur 4, notice `line 99 is past the end of f (5 lines)`); `TestOpenFileFillPlaceholderIgnoresLine`; `TestOpenFileFillRefindsSearch` (active search → cur on the first hit).
- [ ] Implement: `fill` = today's viewer arm (lines or error line; cur/sel 0; lsel clear; pending line — moved from `fileViewer.landPendingLine`, returning the notice instead of writing `m.statusMsg`; search refind + snapHit with the given geometry). Tag counter: a package `atomic.Int64`.
- [ ] `go test ./internal/tui/ -run OpenFile` green; commit.

### Task 2: `fileViewer` over a document; route loads by tag

**Files:** `internal/tui/file_viewer.go`, `internal/tui/model.go` (the `fileContentMsg` arm), `internal/tui/open_file.go`; i18n bundles; Test `internal/tui/file_viewer_test.go`.

**Consumes:** Task 1. **Produces:** `type fileViewer struct{ *openFile }`; `func (m Model) liveDoc(tag string) (d *openFile, rows, innerW int, ok bool)`; `openFileViewer(path string, line int)` unchanged signature (working-tree doc).

- [ ] Test (RED): `TestCoveredViewerStillFills` — push viewer A (`openFileViewer("a.txt",0)`), then viewer B; deliver A's `fileContentMsg` → A's lines filled, B untouched. (Today: dropped.)
- [ ] `fileViewer` embeds `*openFile`; delete its own `p/tag/pendingLine` fields and `landPendingLine`. Title: `View %s (working tree)` / `View %s @ %s` (sha7) / `View %s (shelf)` — two new i18n keys.
- [ ] `liveDoc`: walk `m.layers.entries` top→bottom for `*fileViewer` with that tag (geometry `fv.geom(m)`), then `m.filesPreview` (geometry `filePreviewRowsCap`/`filePreviewInnerW`). The `fileContentMsg` arm becomes: `d, rows, inner, ok := m.liveDoc(msg.tag); if !ok { return m, nil }; if n := d.fill(msg, rows, inner); n != "" { m.statusMsg = n }`.
- [ ] `go test ./internal/tui/ -run 'FileViewer|ContentLink|OpenFile|Covered|I18n'` green; commit.

### Task 3: the right-column preview holds a document

**Files:** `internal/tui/model.go` (field), `file_preview.go` (`openPreview`, `openPreviewSrc`, `viewFileRow`), `files_view.go` (4 teardown sites), and every `m.filesPreview.` site (≈60, mechanical: `.p.`), tests likewise.

- [ ] `filesPreview *openFile`; delete `filesPreviewTag`. `openPreviewSrc(src fileSource, path string, load …)` builds `newOpenFile(src, path)`; commit preview → `fileSource{srcCommit, hash}`, shelf member → `fileSource{srcShelf, id}`.
- [ ] Mechanical rewrite (`m.filesPreview.` → `m.filesPreview.p.`, `activePreview` returns `m.filesPreview.p`, tests' `&contentPopup{…}` → `&openFile{p: &contentPopup{…}}`, `m.filesPreviewTag` → `m.filesPreview.tag`); `go vet` until clean.
- [ ] `TestStalePreviewLoadIsDropped` (open preview X, open preview Y, deliver X's msg → Y unchanged) — passes before and after (guard test; prove it by breaking `liveDoc`'s tag compare).
- [ ] `go test ./internal/tui/` green; commit.

### Task 4: docs + gates

- [ ] `docs/CLAUDE-details.md` (Content links section: the document/frame split, `liveDoc` routing); CHANGELOG `### Fixed`: a second file opened by an agent no longer leaves the first stuck on "(loading…)".
- [ ] `./test.sh`, `./test.sh race`; verify binary; ask before merging.
