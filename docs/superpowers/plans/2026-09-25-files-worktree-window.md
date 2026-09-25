# F → the working-tree files window — Implementation Plan

> **For agentic workers:** executed inline by the session that wrote it (NO subagents — project rule). Steps use `- [ ]`.

**Goal:** `F` opens a window over the whole left column listing the working tree's files (tracked, not deleted, plus untracked), filtered with `/` exactly like today's F popup (fuzzy, 200 cap). The right column shows a live, debounced preview of the selected file's working-tree version. `.`/enter opens the file's actions; `ctrl+t` makes the list full screen. The centred F popup goes away.

**Architecture:** a new files-view mode `filesModeWorktree`, not a new slot.
- It gets the left-column render, the right-column `m.filesPreview`, `closeFilesView` teardown, the esc return (`filesReturnFocus` / `handOffToFilesView`) and the `.` menu plumbing for free.
- The mode's own state lives in `m.wtFiles *worktreeFiles`: every path, the untracked set, the query and typing flag, loading.
- The fuzzy query is kept OUT of `filesView.query`: `visible()` substring-filters, and would drop fuzzy matches. Each query change rebuilds `filesView.lines` from `fuzzy.Rank`, through one `setQuery` chokepoint.
- Keys route to a dedicated `updateWorktreeFilesKey`, taken right after the preview's select/search hooks, so the commit-list side's key logic is untouched.
- The live preview is an **unregistered** `srcWorktree` document in `m.filesPreview`; `watchedDocs` also includes it.
- **View content** opens the full-screen viewer via `openFileViewer`, a separate registered doc.
- The preview load waits for the cursor to settle for 150 ms (a `tea.Tick` + a gen).

**Spec:** the design approved in chat on 2026-09-25 (sections 1–6, recorded in memory `open-files-feature.md`).

## Global Constraints

- The listed files are `svc.LsFiles` (tracked) minus the status's working-tree deletions (a tracked entry with `Unstaged == 'D'`), plus `KindUntracked` status entries. No other git call.
- The filter is `fuzzy.Rank(query, all, fileFinderLimit)` (200). The list is flat: no heading rows.
- The live preview is never in `m.openFiles`. Only View content adds a file to the list.
- Untracked rows offer View content / Copy name / Copy path / Copy absolute path / Copy file link / Open in editor. Tracked rows add Diff (HEAD ↔ working tree) / History / Blame / Commits touching this.
- Open in editor reads the DISK (`svc.WorktreeFile`), not HEAD.
- Strings go through `i18n.T` in all four bundles. Footer + help coverage (`TestHelpFooterCoverage`). `internal/tui` never imports `internal/git`. Tests call `t.Parallel()`.

## Review Focus

1. **A held arrow over many files** must not read each one. Only the settled row loads; a settle message for a row no longer selected is dropped.
2. **Scrolling past files** never adds them to the ctrl+\ list or evicts real open files.
3. **F from a full-screen layer** (diff, history, blame, stash list, palette) goes back there on esc (`handOffToFilesView`).
4. **A deleted tracked file** is not listed; a new untracked file is, marked `(untracked)`.
5. **Filter typing** never lands in the preview's in-view search, and `/` in the preview (when focused) still searches the preview.

---

### Task 1: the file list (pure)

**Files:** Create `internal/tui/files_worktree.go` and `internal/tui/files_worktree_test.go`.

**Produces:** `func worktreeFileList(tracked []string, st model.WorkingTreeStatus) (paths []string, untracked map[string]bool)`, sorted and deduped.

- [ ] Failing test `TestWorktreeFileListDropsDeletedAddsUntracked`:
  - tracked `[a.go, b.go, d.go]`;
  - status: `b.go` Kind tracked with `Unstaged 'D'`, `n.txt` `KindUntracked`, `a.go` modified (`Unstaged 'M'`);
  - → paths `[a.go, d.go, n.txt]` and `untracked {n.txt}`.
- [ ] Also `TestWorktreeFileListStagedDeletionIsGone`: a path in `tracked` with `Staged 'D'` is dropped too (a `git rm` removes it from the index, but a stale ls-files read must not bring it back).
- [ ] Implement; tests pass. Commit `feat(tui): the working-tree file list`.

### Task 2: the mode, its opener, render and filter

**Files:** Modify `files_worktree.go` (+test), `files_view.go` (the mode const, `closeFilesView` clears `wtFiles` and `filesFull`, the key route, the search/hint lines in `renderFilesView`), `model.go` (fields and the `lsFilesMsg` case), and the i18n bundles.

**Produces:**

```go
filesModeWorktree
type worktreeFiles struct { all []string; untracked map[string]bool; query string; typing, loading bool }
func (m Model) openWorktreeFiles() (Model, tea.Cmd)       // beginFilesView + mode + LsFiles load
func (m Model) wtSetQuery(q string)                        // rebuild filesView.lines, sel = 0
func (m Model) updateWorktreeFilesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd)
func (m Model) inWorktreeFiles() bool
```

- Rows are `contentLine{text: path (+ "  " + i18n.T("(untracked)")), path: path}`. An empty list shows `(no files)`; while loading, `(loading…)`.
- Title `Files (working tree)  %d/%d` (matches/all). The search line is `/query█` while typing, `/query` once committed. The hint is `[.] actions  [/] filter  [→] preview  [ctrl+t] full  [esc] close`.
- Keys:
  - While typing: the finder's recall (`scopeFiletree`); `filterMotion`; esc clears and leaves typing; enter keeps the query and records it; backspace/space/runes go through `wtSetQuery`.
  - Otherwise:
    - `/` starts typing; esc clears a committed query, else closes (focus + parked layers restored, as the tree does); `F` is inert.
    - `up/down/j/k/pgup/pgdown/home/end` move (and schedule the preview — Task 4).
    - `enter` and `.` → actions (Task 3); `right`/`tab` focus the preview when there is one; `left` → the list.
    - `ctrl+w` mode; `shift+←/→` pan; `ctrl+t` (Task 5); `g`/`G` the switchers; `ctrl+c` quits.
- [ ] Failing tests:
  - `TestFOpensTheWorktreeFilesWindow`: from `loadedNavModel`, F → `m.filesView != nil`, `inWorktreeFiles`, no `fileFinderPopup` layer. Feed `lsFilesMsg` → the rows are the list.
  - `TestWorktreeFilterIsFuzzy`: `/` then type `fvg` over `[internal/foo/view.go, a.txt]` → one row `internal/foo/view.go`. enter keeps it, esc clears it, a second esc closes the view and focus returns.
  - `TestWorktreeFilesRenderFits`: `View()` at 80×24 and 200×50 — every line ≤ width, the title shows `Files (working tree)`.
- [ ] Implement; pass. Commit `feat(tui): F opens the working-tree files window`.

### Task 3: the actions

**Files:** Modify `files_worktree.go` (+test). `file_finder.go` shrinks to the row builder, renamed `worktreeFileRows(path string, untracked bool) []actionRow`.

- Rows, in order: `ff-view` View file content (`openFileViewer(path, 0)`), `ff-copy-name` Copy file name (`path.Base`), `ff-copy-path`, `ff-copy-abspath`, `contextFileLinkRow()` (it already reads `fileListRowPath` → the tree row), `ff-editor` Open in editor (`svc.WorktreeFile`). Tracked only: `ff-diff`, `ff-history`, `ff-blame`, `ff-commits-touching`.
- The rows no longer `popLayer` (the view is not a layer). History/blame/diff push over the view, so esc returns to it. Commits touching this closes the view (`closeFilesView`), then filters Commits.
- enter and `.` set `m.actionMenu = &actionMenu{rows: …}`.
- [ ] Failing tests: `TestWorktreeActionsTrackedVsUntracked` (the id sets); `TestWorktreeViewContentOpensTheViewer` (the top layer is a `fileViewer` with the disk bytes, registered; esc → the view is still open); `TestWorktreeEditorReadsTheDisk` (the resolve returns the disk bytes of a modified file); `TestCopyFileNameIsTheBaseName`.
- [ ] Rewrite `file_finder_actions_test.go` against the new builder.
- [ ] Implement; pass. Commit `feat(tui): per-file actions in the working-tree files window`.

### Task 4: live preview

**Files:** Modify `files_worktree.go` (+test), `open_files_watch.go` (`watchedDocs` adds an unregistered worktree `m.filesPreview`) and `model.go` (the new msg case).

**Produces:** `type wtPreviewMsg struct{ gen int; path string }`, `m.wtPreviewGen int`, and `func (m Model) scheduleWtPreview() tea.Cmd` (a 150 ms `tea.Tick` for the selected path).

- On the msg: stale gen, a different selected path or the mode gone → drop. When `m.filesPreview` already shows that path, do nothing. Otherwise `d := newOpenFile(srcWorktree, path)`, `m.filesPreview = d`, `m.loadDoc(d)`, with focus left on the list. It is NOT registered.
- ctrl+] on the live preview is inert (there is nothing to background; View content is the way to keep a file).
- [ ] Failing tests:
  - `TestWorktreePreviewFollowsTheSettledCursor`: move down twice quickly; the first msg (old gen) is dropped; the second loads that row's disk bytes into `m.filesPreview`.
  - `TestWorktreePreviewIsNotAnOpenFile`: after a preview, `m.openFiles.list(wt)` is empty.
  - `TestWorktreePreviewIsWatched`: `watchedDocs` includes it; an edit + tick reloads it.
  - `TestWorktreeRightFocusesPreview`: `right`, then `/` → the preview's in-view search (the list's query is unchanged).
- [ ] Implement; pass. Commit `feat(tui): live working-tree preview in the files window`.

### Task 5: full screen, the F entry points, footer/help, docs

- `m.filesFull bool` (cleared by `closeFilesView`). `ctrl+t` in worktree mode toggles it. `layout()`: when `m.filesView != nil && m.filesFull`, set `leftW = w`, `rightW = 0` and delete the Commits box (the render already skips a right column with `boxH[panelCommits] <= 0`).
- Every `openFileFinder` call site (`model.go` base F, `files_view.go`, `diff_view.go`, `history_view.go`, `blame_view.go`, `stash_view.go`, `command_palette.go`) → `openWorktreeFiles`, wrapped in `handOffToFilesView` when a layer is on top. Delete `fileFinderPopup` and its `lsFilesMsg` branch; `lsFilesMsg` now fills `wtFiles`.
- The footer binding and help row for F: "find file (working tree)". i18n for all new strings; remove bundle keys that are no longer used ONLY if a test demands it.
- [ ] Failing tests: `TestWorktreeFilesCtrlTFullScreen` (the rendered left box spans the width; no Commits title); `TestFFromDiffReturnsToDiff` (open the diff layer, F, esc → the diff layer is back on top).
- [ ] Rewrite `file_finder_test.go` (the popup assertions become window assertions).
- [ ] Docs: CHANGELOG, README (the F section + "Open files"), CLAUDE-details ("Content links"/files view: the worktree mode, the unregistered live preview), memory.
- [ ] `./test.sh`, `./test.sh race`, a headless smoke (`./tui-capture.sh "F ; / view ; enter ; down ; ."`), the verify binary `/tmp/claude-1000/gg-fwin`. Ask before merging.
