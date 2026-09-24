# Open files — Plan 3: watching working-tree files (TUI)

> **For agentic workers:** executed inline by the session that wrote it (NO subagents — project rule). Steps use `- [ ]`.

**Goal:** an open working-tree file reloads by itself when the file changes on disk, keeping the reader's cursor line, scroll top and live search. The file on screen is checked every ~1 s, background files every ~5 s. A deleted file shows `(file deleted on disk)` and comes back when the file does.

**Architecture:** every change goes through ONE path: **stat → compare → reload**.
- The existing 1 s `heartbeatMsg` runs `m.openFilesTick(now)`, which picks the due working-tree docs of the current worktree. One off-thread cmd `os.Stat`s them all and returns `openFilesStatMsg`. Update compares each result with the doc's last-seen `diskStat` and dispatches a reload through the new `m.loadDoc(d)`.
- fsnotify never reloads anything. On a filesystem that supports it (`m.watchSupported`), an event only makes a doc due and runs the tick at once. A new leaf package `internal/filewatch` watches the files' directories, filters by exact path, and debounces.
- `loadDoc` stats **before** it reads. The load's `fileContentMsg` carries that stat, so a doc's baseline is the state its bytes came from. Without this, an edit made between the read and the first poll would be missed.
- A doc's in-flight flag `loading` keeps a poll from racing a load.
- The reload keeps the reader's place at FILL time (`msg.reload`), not when the reload is sent. A cursor moved during a slow /mnt read is not snapped back.

**Spec:** `docs/superpowers/specs/2026-09-24-open-files-design.md` → Stage 3.

## Global Constraints

- Only `srcWorktree` docs are watched; commit and shelf docs never are.
- Only the CURRENT worktree's list is polled, and nothing is polled while `m.loading` (a repo/worktree switch in flight: `svc` already points at the new tree while `currentWorktree` is still the old one).
- No I/O on the Update thread: every stat, fsnotify `Add`/`Remove`/`Close` and read runs in a `tea.Cmd`.
- The poll is a plain `os.Stat` of `filepath.Join(m.currentWorktree, filepath.FromSlash(d.path))`: stdlib only, never a git process (the git process count is the cost on /mnt). Only a reload goes through `svc`.
- Cadence: the file on screen (`docShown`, covered viewers included) on every heartbeat; background files when `now.Sub(d.checked) >= 5*time.Second`.
- A change = size or mtime differs, or presence flipped. Known limit (documented): an edit that keeps the byte length inside one mtime tick is missed until the next edit.
- New strings go through `i18n.T` in all four bundles. `internal/tui` never imports `internal/git`. Tests call `t.Parallel()`.
- `internal/filewatch` is a DAG leaf (stdlib + fsnotify) and gets an archtest row.

## Review Focus

1. **Two loads in flight for one doc.** A poll reload landing between a viewer's open and its own load must not eat `pendingLine` or reset the cursor. This is the `loading` guard (Task 3 test).
2. **Delete, then recreate.** The reader's place survives the placeholder, including a `bringToFront` while the placeholder shows (`keepPlace` must not overwrite a saved place with line 1). Task 2 and Task 3 tests.
3. **Worktree switch mid-poll.** A stat result for the old worktree is dropped. Nothing polls while `m.loading`. Docs of the old worktree keep their `loading` state correct: `findTag` searches every worktree's list. Task 3 tests.
4. **Unchanged file = no reload.** A tick over an untouched file sends no load; the test counts loader calls. Otherwise every shown file re-reads (and re-lexes) every second.
5. **A background doc's reload lands.** Before this plan, `liveDoc` dropped a result for a doc in no frame. It now falls back to the registry by tag, and a closed doc (out of the registry) still drops. Task 2 test.

---

### Task 1: `internal/filewatch` — a file-set watcher

**Files:** Create `internal/filewatch/filewatch.go`, `internal/filewatch/filewatch_test.go`. Modify `internal/archtest/import_guard_test.go` (a leaf row) and `CLAUDE.md` (one map row).

**Interfaces — Produces:**

```go
func New(debounce time.Duration) (*Watcher, error)
func (w *Watcher) Set(paths []string) // absolute file paths; reconciles dir watches; safe off-thread
func (w *Watcher) Events() <-chan string // a changed watched path (filepath.Clean form), debounced
func (w *Watcher) Close() error // idempotent; closes Events
```

- [ ] **Step 1: failing tests** (`filewatch_test.go`, each `t.Parallel()`, bounded `select` with a 3 s timeout):
  - `TestWriteToAWatchedFileEmitsItsPath`: temp dir with `a.txt` and `b.txt`; `Set([a])`; write `a` → `Events()` yields `filepath.Clean(a)`.
  - `TestUnwatchedSiblingIsSilent`: `Set([a])`; write `b` → no event within 400 ms (debounce 50 ms).
  - `TestSetDropsAPath`: `Set([a])`, then `Set(nil)`; write `a` → no event within 400 ms.
  - `TestRecreatedFileEmits`: `Set([a])`; remove `a`, then write `a` again → an event for `a` (the watch is on the dir, so it survives the delete).
  - `TestCloseClosesEvents`: `Close()` → the channel is closed; a second `Close()` returns nil.
- [ ] **Step 2:** `go test ./internal/filewatch/` → FAIL (package missing).
- [ ] **Step 3: implement.**
  - The struct holds `fsw`, a `mu sync.Mutex`, `files map[string]bool` (clean abs paths), `dirs map[string]int` (a ref count per dir), per-path debounce timers, `out chan string` (buffer 16), `done`, and `closed`.
  - `Set` diffs the new set against the old. It `fsw.Add`s each newly needed dir (an error is ignored: fail-open, polling still covers the file) and `fsw.Remove`s each dir whose count drops to 0.
  - The loop goroutine keeps an event only when `filepath.Clean(ev.Name)` is in `files` (checked under `mu`), then arms that path's timer. The timer sends non-blocking under `mu` only when `!closed`.
  - `Close` follows the gitwatch pattern: under `mu` it sets `closed`, stops the timers and closes `out`; then it closes `done` and `fsw`.
- [ ] **Step 4:** `go test ./internal/filewatch/ -race -count=3` → PASS.
- [ ] **Step 5: archtest + map.**
  - Add `"filewatch": {"config", "model", "git", "engine", "domain", "tui", "cli", "mcp", "web", "app", "gitwatch", "i18n"}` to `TestLayeringDAG`.
  - Add the CLAUDE.md row: `| \`filewatch\` | Pure fsnotify watcher over a SET of files (dir watches, exact-path filter, per-path debounce) behind open-file reloads; the TUI's stat poll stays the source of truth. DAG leaf. |`
  - Run `go test ./internal/archtest/` → PASS.
- [ ] **Step 6: commit** `feat(filewatch): a debounced watcher over a set of files`.

### Task 2: document disk state, `loadDoc`, and routing loads to background docs

**Files:** Modify `internal/tui/open_file.go`, `internal/tui/open_files.go`, `internal/tui/file_viewer.go`, `internal/tui/file_preview.go`, `internal/tui/model.go` (the `fileContentMsg` case) and `internal/i18n/lang/{ja,ko,ru,zh}.toml`. Create `internal/tui/open_files_watch.go` and `internal/tui/open_files_watch_test.go`.

**Interfaces — Produces:**

```go
type diskStat struct { size int64; mod time.Time; missing, known bool }
func statDisk(abs string) diskStat                     // os.Stat → known=true; ENOENT → missing
func (st diskStat) same(o diskStat) bool               // size, mod (Equal), missing
// openFile gains: disk diskStat; checked time.Time; loading bool
// fileContentMsg gains: disk diskStat; reload bool
func (m Model) docAbs(d *openFile) string              // "" unless a worktree doc with currentWorktree set
func (m Model) loadDoc(d *openFile) tea.Cmd            // = loadDocWith(d, m.docLoader(d))
func (m Model) loadDocWith(d *openFile, load func(context.Context) ([]byte, error)) tea.Cmd
func (r *openFilesReg) findTag(tag string) *openFile   // across every worktree
func (m Model) viewerGeom() (rows, innerW int)         // fileViewer.geom's math without a viewer
```

Behaviour:
- `loadDocWith` sets `d.loading = true`.
  - For a doc with `docAbs != ""`, the cmd does `st := statDisk(abs)` first.
  - `st.missing` → it returns `fileContentMsg{tag, lines: [{text: i18n.T("(file deleted on disk)")}], disk: st}` and does not read.
  - Otherwise it runs `loadFileContentSrcCmd(...)()` and sets `msg.disk = st`.
- `fill`:
  - clears `d.loading`;
  - `if msg.disk.known { d.disk = msg.disk }`;
  - `if msg.reload && d.pendingLine == 0 { d.keepPlace() }` goes first;
  - `keep` is consumed only when the new lines are real (`p.lines[0].src`); a placeholder leaves it for the next fill.
- `keepPlace` is a no-op when `!docLoaded(d)`, so a place saved before a placeholder is never overwritten with line 1.
- The `fileContentMsg` case: after `liveDoc` misses, `m.openFiles.findTag(msg.tag)` fills the doc at `m.viewerGeom()`. A background doc comes back in the full-screen viewer (ruling a).
- Call sites move to the new helpers:
  - `openFileViewer` uses `m.loadDoc(d)`;
  - `bringToFront` uses `m.loadDoc(d)`;
  - `openPreviewSrc` uses `m.loadDocWith(d, load)`.
  - `steerNavigateContent` is unchanged: it calls the returned cmd and gets a `fileContentMsg`.

- [ ] **Step 1: failing tests** (`open_files_watch_test.go`; a real file under `loadedNavModel(t).currentWorktree`):
  - `TestLoadDocStampsTheDiskState`: open `w.txt` via `openFileViewer` + `pumpAll` → `d.disk.known && !d.disk.missing && d.disk.size == len(content)`; `d.loading == false`.
  - `TestLoadDocOfAMissingFileShowsDeleted`: open, remove the file, `pumpAll(m.loadDoc(d))` → the lines are the one placeholder `(file deleted on disk)`; `d.disk.missing`.
  - `TestPlaceholderFillKeepsTheSavedPlace`: a doc with 50 real lines, cursor at line 30 (`p.cur = 29`), `top = 20`. Fill a placeholder with `reload: true`, then a real 50-line msg (`reload: true`) → `p.cur == 29`, `p.sel == 20`.
  - `TestKeepPlaceIsANoOpOnAPlaceholder`: after the placeholder above, `d.keepPlace()` → `d.keep.line == 30` is unchanged.
  - `TestBackgroundDocReceivesItsLoad`: open `w.txt`, ctrl+] (background), overwrite the file, `m.Update(m.loadDoc(d)())` → the doc's lines hold the new text. Then close it via the switcher's `x` (`closeDoc`), and a further load result changes nothing (no panic, lines unchanged).
  - `TestLoadingFlagSetUntilFill`: `cmd := m.loadDoc(d)` → `d.loading`; after `Update(cmd())` → `!d.loading`.
  - `TestFindTagSearchesEveryWorktree`: registry with docs under `wt1`/`wt2` → `findTag` finds both; an unknown tag gives nil.
- [ ] **Step 2:** `go test ./internal/tui/ -run 'LoadDoc|Placeholder|KeepPlaceIsANoOp|BackgroundDocReceives|LoadingFlag|FindTag'` → FAIL (undefined).
- [ ] **Step 3: implement** as the Interfaces and Behaviour above say. Add the key `"(file deleted on disk)"` to all four bundles:
  - ja `"(ディスク上で削除されました)"`
  - ko `"(디스크에서 삭제됨)"`
  - ru `"(файл удалён с диска)"`
  - zh `"(文件已从磁盘删除)"`
- [ ] **Step 4:** rerun the Step 2 command → PASS. Then run `go test ./internal/tui/ -run 'OpenFile|FileViewer|ContentLink|Switcher|Preview|I18n|Scan'` → PASS (plan 1/2 behaviour and the i18n gates intact).
- [ ] **Step 5: commit** `feat(tui): open files carry their disk state; loads reach background files`.

### Task 3: the poll

**Files:** Modify `internal/tui/open_files_watch.go` (+test), `internal/tui/model.go` (a `docWatch` field, the `heartbeatMsg` case, the new msg case, `reRoot`).

**Interfaces:**
- Consumes (Task 2): `diskStat`, `statDisk`, `same`, `docAbs`, `loadDoc`, `findTag`, `viewerGeom`, `openFile.{disk,checked,loading}`, `fileContentMsg.reload`.
- Produces:

```go
const backgroundPollEvery = 5 * time.Second
type docWatchState struct { gen int; polling, building bool; w *filewatch.Watcher; paths []string } // Model field docWatch (value; w is a pointer; building/w/paths used from Task 4)
type openFilesStatMsg struct { gen int; wt string; stats []docStatResult }
type docStatResult struct { tag string; disk diskStat }
func (m Model) watchedDocs() []*openFile               // current wt, srcWorktree, in list order
func (m Model) openFilesTick(now time.Time) (Model, tea.Cmd)
func (m Model) applyDocStats(msg openFilesStatMsg) (Model, tea.Cmd)
func (m Model) reloadDocCmd(d *openFile) tea.Cmd       // loadDoc with msg.reload = true
```

Behaviour:
- `openFilesTick` returns `(m, nil)` when `m.openFiles == nil`, `m.currentWorktree == ""`, `m.loading` or `m.docWatch.polling`.
- The due set is every watched doc with `!d.loading` and either `docShown(d)` or `now.Sub(d.checked) >= backgroundPollEvery`. When it is empty, return nil.
- Each due doc gets `d.checked = now`; `polling = true`. One cmd stats all their `docAbs` paths and returns `openFilesStatMsg{gen, wt: m.currentWorktree, ...}`.
- `applyDocStats` sets `polling = false` and drops the msg when `gen != docWatch.gen || wt != currentWorktree`. Each result is looked up with `findTag` and skipped when nil, loading or not a worktree doc. Then:
  - `!d.disk.known` → record it (a baseline; no reload);
  - `d.disk.same(st)` → nothing;
  - otherwise → `d.disk = st` and `cmds = append(cmds, m.reloadDocCmd(d))`.
  - A newly missing file needs no special branch: `loadDoc` itself yields the deleted placeholder, and `reload` keeps the place.
- The `heartbeatMsg` case batches `m.openFilesTick(time.Now())` in.
- `reRoot` does `m.docWatch.gen++` and `m.docWatch.polling = false` (the watcher's close is Task 4).

- [ ] **Step 1: failing tests.** `t0 := time.Now()`; "tick" means `nm, cmd := m.openFilesTick(t); m = pumpAll(t, nm, cmd)`. Reloads are observed, not counted: distinct content proves a reload, and a sentinel written into `d.p.lines` that survives a tick proves there was none.
  - `TestShownFileReloadsAndKeepsPlace`: 60-line file in the viewer; cursor line 40, top 30; a live search for `line 45`. Append 10 lines (the length changes) → tick → 70 lines, `p.cur == 39`, `p.sel == 30`, `lsel` empty, and the search still active and on its hit.
  - `TestUnchangedFileDoesNotReload`: open, tick once (baseline recorded), replace `d.p.lines[0].text = "SENTINEL"`, tick again with no edit → the sentinel survives.
  - `TestBackgroundFilePollsEveryFiveSeconds`: open, ctrl+], edit the file. A tick at `t0+1s` leaves the lines old. A tick at `t0+6s` gives the new lines.
  - `TestCommitAndShelfDocsAreNotWatched`: a `srcCommit` doc in the registry → `watchedDocs()` excludes it and a tick sends no stat cmd.
  - `TestDeletedThenRecreated`: 30-line file, cursor line 20; remove → tick → the placeholder; `bringToFront` → still the placeholder with the place saved; recreate the file (length differs) → tick → lines back, `p.cur == 19`.
  - `TestInFlightDocIsSkipped`: `d.loading = true`; edit; tick → no stat cmd for it (nil cmd), and `d.disk` is unchanged.
  - `TestStaleStatMsgDropped`: get a tick's msg, change `m.currentWorktree` (or bump `docWatch.gen`), apply it → no reload and `polling == false`.
  - `TestNoPollWhileSwitching`: `m.loading = true` → the tick cmd is nil.
  - `TestOtherWorktreesDocsNotPolled`: a worktree doc registered under `"/other"` → not in `watchedDocs()`.
  - `TestPendingLineSurvivesAPoll`: `openFileViewer(path, 25)` without pumping (the load is in flight) → tick → nil (loading). Then pump the open's load → `p.cur == 24`.
- [ ] **Step 2:** `go test ./internal/tui/ -run 'ShownFileReloads|UnchangedFile|BackgroundFilePolls|CommitAndShelfDocs|DeletedThenRecreated|InFlightDoc|StaleStatMsg|NoPollWhileSwitching|OtherWorktreesDocs|PendingLineSurvives'` → FAIL.
- [ ] **Step 3: implement** as the Behaviour above says.
- [ ] **Step 4:** rerun → PASS. Run `go test ./internal/tui/` → PASS.
- [ ] **Step 5: commit** `feat(tui): open working-tree files reload when the disk changes`.

### Task 4: fsnotify on top (supported filesystems only)

**Files:** Modify `internal/tui/open_files_watch.go` (+test) and `internal/tui/model.go` (the new msg cases; `reRoot` closes the watcher).

**Interfaces — Produces:**

```go
type docWatchReadyMsg struct { gen int; w *filewatch.Watcher }
type docWatchEventMsg struct { gen int; path string }
type docWatchClosedMsg struct{ gen int }
func docWatchListenCmd(w *filewatch.Watcher, gen int) tea.Cmd
func (m Model) syncDocWatch() (Model, tea.Cmd) // called at the end of openFilesTick
```

Behaviour of `syncDocWatch`:
- `paths` = the sorted `docAbs` of `watchedDocs()`.
- `!m.watchSupported` → nil.
- `len(paths) == 0` and a watcher exists → a cmd closes it off-thread; set `docWatch.w = nil` and `paths = nil`.
- No watcher and paths → a cmd builds `filewatch.New(150ms)`, runs `Set(paths)` and returns `docWatchReadyMsg`. A `building` flag (in `docWatchState`) prevents a second build.
- A watcher whose `paths` differ → a cmd runs `w.Set(paths)`; record `paths`.

Message handling:
- `docWatchReadyMsg`: a stale gen → close the watcher. Otherwise store it and start `docWatchListenCmd`.
- `docWatchEventMsg`: a stale gen → drop. Otherwise every watched doc whose `docAbs == path` gets `checked = time.Time{}`; then return `openFilesTick(time.Now())` batched with a re-armed listen.
- `docWatchClosedMsg`: end the listen loop.
- `reRoot` closes `docWatch.w` off-thread and nils it.

- [ ] **Step 1: failing tests.** These drive msgs by hand; NEVER `pumpAll` a listen cmd, because it blocks.
  - `TestDocWatchBuildsOnSupportedFS`: `m.watchSupported = true`; open a file; take `syncDocWatch`'s cmd; run it → `docWatchReadyMsg` with a non-nil `w`; Update → `m.docWatch.w != nil`. Then write the file and read one event from `w.Events()` within 3 s.
  - `TestDocWatchEventPollsABackgroundFileNow`: a background doc with `checked = now`; Update `docWatchEventMsg{path: abs}` → the returned batch includes a stat cmd (the doc is due at once).
  - `TestDocWatchOffWhenUnsupported`: `watchSupported = false` → `syncDocWatch` gives a nil cmd.
  - `TestDocWatchClosesWhenNoFilesLeft`: with a watcher stored, close the only doc (esc) → the next tick's sync nils `docWatch.w`.
  - `TestReRootClosesDocWatch`: covered by calling `reRoot` on a model with a stored watcher → `docWatch.w == nil`, and `gen` bumped.
- [ ] **Step 2:** `go test ./internal/tui/ -run DocWatch` → FAIL.
- [ ] **Step 3: implement.**
- [ ] **Step 4:** `go test ./internal/tui/ -race -run 'DocWatch|OpenFile|ShownFile|Background'` → PASS.
- [ ] **Step 5: commit** `feat(tui): fsnotify wakes open-file polls on supported filesystems`.

### Task 5: docs, gates, smoke

- [ ] `CHANGELOG.md`: an "Open files: watching" entry (the cadence, the deleted placeholder, commit/shelf never watched, the mtime-granularity limit).
- [ ] `README.md` "Open files": one paragraph on auto-reload.
- [ ] `docs/CLAUDE-details.md` "Content links": the stat→compare→reload path; stat-before-read; `loading`; reload-at-fill place; `findTag` routing; fsnotify only as a wake-up and only when `watchSupported`.
- [ ] `./test.sh` and `./test.sh race` (output to the workspace; read the tails).
- [ ] Headless smoke with `./tui-capture.sh`: open a file through a content link, edit it from outside, wait 2 s → the capture shows the new text. Then delete it → the placeholder.
- [ ] Build the verify binary `go build -o /tmp/claude-1000/gg-watch ./cmd/gg` and give the user that path.
- [ ] Commit `docs: open files watching`. Update memory `open-files-feature.md`. Ask before merging.
