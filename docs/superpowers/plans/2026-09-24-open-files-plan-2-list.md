# Open files — Plan 2: the open-files list (TUI)

> **For agentic workers:** executed inline by the session that wrote it (NO subagents — project rule). Steps use `- [ ]`.

**Goal:** files opened in the full-screen viewer or the files view's preview join a per-worktree list (cap 20, oldest dropped); ctrl+] sends a file to the background, esc closes it; the ctrl+\ popup lists and restores them; opening an open file reuses it.

**Architecture:** a pointer-field registry `openFiles` on Model (worktree → docs, most recently shown first). A document lives in at most ONE frame; "shown" = it is `m.filesPreview` or some `fileViewer` on the stack (`liveDoc`). A document leaves the list ONLY by an explicit close (esc on it, `x` in the switcher) or eviction — every other teardown (files view closed, `clearLayers`, a new preview) leaves it in the background, so nothing the user was reading is lost.

**Spec:** `docs/superpowers/specs/2026-09-24-open-files-design.md` → Stage 2 (+ rulings 2–5, 7–9).

## Global Constraints

- Cap = 20 per worktree (constant `maxOpenFiles`).
- Every new string through `i18n.T`, all four bundles; new global key text → `helpContent()`.
- `internal/tui` never imports `internal/git`; tests `t.Parallel()`.

## Review Focus

1. Eviction never drops a document on screen (a covered viewer counts as shown).
2. A document never sits in two frames (reuse of the preview's doc as a viewer detaches it from the preview first).
3. Reusing a working-tree doc reloads it but keeps its cursor line and scroll (a `:<line>` asked for wins).
4. The quit-mode sessions popup (quit guard) shows no file rows.
5. Switching worktree shows that worktree's list; switching back shows the first list intact.

---

### Task 1: the registry

**Files:** Create `internal/tui/open_files.go`, `internal/tui/open_files_test.go`.

**Produces:**
```go
const maxOpenFiles = 20
type openFilesReg struct{ byWT map[string][]*openFile } // most recently shown first
func (r *openFilesReg) list(wt string) []*openFile
func (r *openFilesReg) find(wt, key string) *openFile
// touch puts d first (adding it if new) and, over the cap, drops the least
// recently shown document for which shown(d) is false; it returns it (nil = none).
func (r *openFilesReg) touch(wt string, d *openFile, shown func(*openFile) bool) (evicted *openFile)
func (r *openFilesReg) remove(wt string, d *openFile)
```
- [ ] Tests (RED): order after touches; `find` by key; the 21st evicts the oldest; eviction skips a shown doc; `remove`; two worktrees independent; nil-safe on an empty registry.
- [ ] Implement; green; commit.

### Task 2: reload that keeps the reader's place

**Files:** `internal/tui/open_file.go`, `open_file_test.go`.

**Produces:** `func (d *openFile) keepPlace()` — records `keepLine = cur+1`, `keepTop = sel` for the next `fill`; `fill` then restores cursor (clamped to the new length) and top (clamped), no notice. A `pendingLine` wins over a kept place.
- [ ] Tests (RED): keep across a reload of the same text; keep clamped after the file shrank; pendingLine wins.
- [ ] Implement; green; commit.

### Task 3: every open goes through the list (+ reuse, eviction)

**Files:** `internal/tui/model.go` (field `openFiles *openFilesReg`, init in `New`), `file_viewer.go` (`openFileViewer`), `file_preview.go` (`openPreviewSrc`), new helpers in `open_files.go`: `m.docShown(d) bool`, `m.detachDoc(d) Model` (remove its viewer frame from the stack / clear `m.filesPreview` when it is that doc), `m.registerDoc(d) Model` (touch + eviction status `closed %s (20 files open)`).

- [ ] Tests (RED), in `open_files_test.go` over `loadedNavModel`: `openFileViewer` twice for one path → ONE list entry, one viewer frame on the stack, cursor kept; with a line → cursor on it; 21 opens → first path gone + status; preview of commit X then `openFileViewer`-free reuse: opening the same preview (same sha+path) again keeps its cursor and issues NO load; a doc on screen as the preview, re-opened as a viewer (same key) → detached from the preview; per-worktree (`m.currentWorktree` swapped) lists.
- [ ] `openFileViewer(path, line)`: key → `find` or `newOpenFile`; `detachDoc`; push `&fileViewer{doc}`; `line>0` → `pendingLine`, else `keepPlace()` for a reused doc; `registerDoc`; load (worktree docs always reload). `openPreviewSrc`: reuse → set `m.filesPreview = d` (after `detachDoc`), no load for commit/shelf; new → as today; `registerDoc`.
- [ ] Green; commit.

### Task 4: ctrl+], esc, "Send to background"

**Files:** `file_viewer.go` (update), `files_view.go` (preview keys, `closePreview`), `action_menu.go` (viewer rows use `d.path` + the doc's rev; new row), `file_link.go` (`focusedDoc` generalises `focusedFilesPreview`: the disk-match rule applies to ANY non-working-tree doc on screen, viewer included).

- [ ] Tests (RED): viewer ctrl+] → frame gone, doc still listed, status `%s is in the background — ctrl+\ lists open files`; viewer esc → frame gone, doc removed; preview ctrl+] → preview gone, tree focused, doc listed; preview esc (`closePreview`) → doc removed; the `.` menu of viewer and preview has `file-background` "Send to background" and running it = ctrl+]; a commit-version viewer's Copy file link applies the disk-match rule.
- [ ] Implement; the viewer's hint line gains `[ctrl+]] background` (fileViewer only — pass a flag to `renderPreviewBox`); green; commit.

### Task 5: the switcher group

**Files:** `sessions_popup.go`, `sessions_popup_test.go`.

- [ ] Tests (RED): popup opens with files and no sessions; rows: bold `Open files` header, then `● <path>  :<line>  <version>` (● shown / ○ background; version `working tree` / `@ <sha7>` / `shelf`); `/` filters files too; `enter` on a background file → a viewer frame on top, popup closed; `enter` on the preview's doc → preview focused; `enter` on a covered viewer's doc → that frame moved to the top; `x` closes (removed, frame gone); quit mode shows no file rows; j/k walk across both groups.
- [ ] `sessionsPopup` gains `files []*openFile` parallel to `rows` (nil = not a file row); `nextSelectable`/`current` honour it; title `Agents & open files` when the group shows; hint `[enter] open  [k] kill  [x] remove/close  [/] filter  [z] mode  [esc] close`. `m.bringToFront(d)`.
- [ ] Green; commit.

### Task 6: footer, help, docs, gates

- [ ] Footer `agent-sessions` entry shows when sessions OR open files exist; label `[ctrl+\] agents & files` when files are open. Help rows: ctrl+\ text mentions open files; ctrl+] text adds "in a file viewer: send the file to the background". `TestHelpFooterCoverage` + i18n gates green.
- [ ] CHANGELOG, README (key table rows for ctrl+]/ctrl+\), `docs/CLAUDE-details.md` (registry rules: explicit close only, one frame per doc, eviction skips shown).
- [ ] `./test.sh`, `./test.sh race`; verify binary; ask before merging.
