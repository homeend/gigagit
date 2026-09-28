# Recycle shelve + shelf-entry notes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (this repo: NEVER subagents). Steps use checkbox (`- [ ]`) syntax.

**Goal:** a `shelve` answer for `recycle.dirty` that parks the target worktree's work as one shelf file set, with deletions/renames recorded in a new shelf-entry note that the TUI/web/CLI can show.

**Architecture:** part 1 adds entry-level notes on shelf entries (address `{StateShelf, ShelfID, Path:""}`) through model → notes store → domain → CLI/TUI/web read surfaces. Part 2 adds an injected `OpDeps.ShelveStaged` seam, wired by `domain.Execute`, which the `RecycleWorktree` op calls after `git add -A` and before the discard.

**Tech Stack:** Go 1.26, Bubble Tea TUI, vanilla-JS web SPA, TOML stores.

**Spec:** `docs/superpowers/specs/2026-09-28-recycle-shelve-design.md`

## Global Constraints

- Option values stay English protocol words, order `commit`, `shelve`, `discard`, `abort`.
- Engine prose literals (`WithSummary`/`AppendSummary`/`PromptReq`/`Progress{Step}`) must be string literals present in all four bundles (`engine_prose_test.go`).
- Every TUI string goes through `i18n.T` with a literal key present in ja/ko/zh/ru (`i18n_scan_test.go`, `options_vocab_test.go`, `menu_labels_test.go`).
- `internal/tui`/`internal/cli`/`web` never import `internal/git`.
- Seam reads inside an op must NOT go through domain queries (Read reservation vs the op's TreeWrite → deadlock).
- Placeholder member name `delete.me` (empty); label `WIP on <branch>` (`detached` when detached HEAD).
- Note: source `agent`, author `gg`, summary `Recycled from <dir> (<branch>)`.
- New tests call `t.Parallel()` where the package does.

## Review Focus

1. A shelf-level note must never go stale/orphaned through the line resolver (empty ContextHash) — `NotesAt`, overview and sweep must route entry-level notes around `resolveNotes`.
2. `ShelveStaged` called under the op's TreeWrite must not deadlock (no domain query inside) — test runs it through `domain.Execute`.
3. Paths with spaces / non-ASCII in the target (name-status `-z` parsing, `show :<path>`).
4. Detached-HEAD target → label `WIP on detached`.
5. Removing a shelf entry leaves no dangling shelf-level note (cascade + sweep).

---

### Task 1: model + store — entry-level notes

**Files:** Modify `internal/model/note.go`, `internal/notes/file_store.go`. Test `internal/model/note_test.go` (create if absent), `internal/notes/file_store_test.go`.

**Produces:** `func (n Note) IsShelfLevel() bool`, `func (n Note) IsEntryLevel() bool`.

- [ ] Step 1 (RED): tests
  ```go
  func TestShelfLevelNote(t *testing.T) {
      n := Note{Address: FileAddress{State: StateShelf, ShelfID: "e1"}}
      if !n.IsShelfLevel() || !n.IsEntryLevel() { t.Fatal("shelf entry note must be shelf-level and entry-level") }
      n.Address.Path = "a.go"
      if n.IsShelfLevel() || n.IsEntryLevel() { t.Fatal("a file note on a shelf entry is not entry-level") }
      c := Note{Address: FileAddress{State: StateCommitted, Commit: "abc"}}
      if !c.IsEntryLevel() { t.Fatal("commit-level is entry-level") }
  }
  ```
  and in notes: `TestCapExemptsShelfLevelNotes` — max 1, store one old shelf-level root + two file roots; after a Put the shelf-level root survives and one file root remains.
- [ ] Step 2: run `go test ./internal/model ./internal/notes -run 'ShelfLevel|CapExempts'` → FAIL (undefined / dropped).
- [ ] Step 3: implement
  ```go
  // IsShelfLevel reports a note about a whole shelf entry: an entry id and no path.
  func (n Note) IsShelfLevel() bool {
      return n.Address.State == StateShelf && n.Address.ShelfID != "" && n.Address.Path == ""
  }
  // IsEntryLevel reports a note about a whole object (a commit or a shelf
  // entry): it has no line to re-anchor.
  func (n Note) IsEntryLevel() bool { return n.IsCommitLevel() || n.IsShelfLevel() }
  ```
  `capOldestFirst`: `n.IsCommitLevel()` → `n.IsEntryLevel()` (+ comment).
- [ ] Step 4: rerun → PASS; `go test ./internal/model ./internal/notes`.
- [ ] Step 5: commit `feat(notes): shelf-level notes (a note about a whole shelf entry)`.

### Task 2: domain — add, read, count, sweep, cascade, overview

**Files:** Modify `internal/domain/notes.go`, `notes_sweep.go`, `notes_overview.go`, `shelf.go`. Test `internal/domain/notes_shelf_entry_test.go` (new).

**Consumes:** Task 1. **Produces:**
- `func (s *Service) ShelfNotes(ctx, id string) ([]ResolvedNote, error)` — shelf-level roots (+ replies attached as `resolveNotes` would), `Status: model.NoteActive`, oldest first.
- `NoteCounts.ByShelf map[string]int`.
- `NoteShelfNotes.Entry []ResolvedNote`.
- `func ShelfEntryNote(id string) model.FileAddress` helper → `{State: StateShelf, ShelfID: id}`.

Behaviour:
- `NoteAdd`: when `n.IsShelfLevel()` → `ShelfFind(id)` must succeed (else `fmt.Errorf("shelf entry %s not found", id)`), skip hash/range fill, `Range=[2]int{}`.
- `NotesAt(addr)` with a shelf-level addr → `ShelfNotes`.
- `NoteCounts`: shelf-level root → `ByShelf[id]++` (before the existing ShelfID skip).
- Sweep: skip `IsCommitLevel`; for `IsShelfLevel` drop when `ShelfFind` returns not-found (any other error keeps it); never age out.
- `ShelfRemove`: after `st.Remove` succeeds, drop notes whose address ShelfID == id and Path == "" (and their replies) via `notes.Store.Sweep` predicate; ignore/log the error; `invalidateNoteCounts`.
- Overview: a bucket whose addr is shelf-level goes to `sh.Entry` (built from the notes with Active status, no side reads); `Count` adds `len(s.Entry)`.

- [ ] Step 1 (RED): tests with a real repo + shelf dir (use the helpers the existing `notes_worktree_test.go` uses to build a Service with notes + shelf stores):
  - `TestShelfNoteAddAndRead` — shelve a file set, `NoteAdd` a shelf-level note with Rationale → `ShelfNotes` returns it Active; `NotesAt(ShelfEntryNote(id))` same; `NoteCounts().ByShelf[id]==1`.
  - `TestShelfNoteAddUnknownEntry` — error containing "not found".
  - `TestShelfRemoveDropsItsNotes` — remove entry → `ShelfNotes` empty, store has no note with that ShelfID.
  - `TestSweepKeepsShelfNoteWhileEntryExists` + drops it once the entry is gone (remove the entry from the shelf store directly, bypassing the cascade).
  - `TestOverviewListsShelfEntryNotes` — `ov.Shelves[0].Entry` has 1, `ov.Count()==1`.
- [ ] Step 2: `go test ./internal/domain -run 'ShelfNote|ShelfRemoveDrops|SweepKeepsShelf|OverviewListsShelf'` → FAIL.
- [ ] Step 3: implement as above.
- [ ] Step 4: rerun → PASS; `go test ./internal/domain`.
- [ ] Step 5: commit `feat(domain): read, count, sweep and cascade shelf-entry notes`.

### Task 3: CLI — `gg note list --shelf <id>`

**Files:** `internal/cli/note.go`; test `internal/cli/note_shelf_test.go`.

- `noteList`: `shelf := fs.String("shelf", "", "list the notes on a shelf entry")`. Combined with a link / --file / --cached / --rev / --preview → usage error 2 (`note list: --shelf names the target (drop …)`). Else `svc.ShelfNotes`.
- `renderNoteLine`: shelf-level root → `"%s [%s] shelf %s  %s\n"` then each non-empty rationale line indented by 4 spaces (the deletions list is the point of the note). JSON path unchanged (wire note has rationale).
- [ ] RED: `TestNoteListShelf` — set up a shelf entry + note through the domain in a temp repo, run `gg note list --shelf <id>` → stdout contains `shelf <id>`, the summary and `    a.go` from the rationale; `TestNoteListShelfWithFileIsUsage` → exit 2.
- [ ] Run → FAIL; implement; run `go test ./internal/cli -run NoteListShelf` → PASS; `go test ./internal/cli`.
- [ ] Commit `feat(cli): gg note list --shelf <id>`.

### Task 4: TUI — ◆N on shelf rows + `n` read-only note viewer

**Files:** `internal/tui/shelf.go` (load counts with the list), `shelf_popup.go` (badge, key, hint), new `internal/tui/shelf_note_view.go`, `help.go` (if the shelf keys are listed there), bundles. Test `internal/tui/shelf_note_view_test.go`.

- `shelfLoadedMsg` gains `notes map[string]int`; `loadShelfCmd`/`loadShelfForHintCmd` fill it from `svc.NoteCounts(ctx).ByShelf` (best effort, copy the map). `shelfPopup.noteCounts map[string]int` set where the popup is built/refreshed.
- Row text: `p.rows[i] + noteBadge(p.noteCounts[id])` (the existing `◆N` helper).
- Key `n` (not filtering, not compare mode): selected entry has notes → `m.shelfNoteCmd(id)` → `shelfNotesMsg{id, notes, err}` → `m.pushLayer(newContentPopup(i18n.T("Notes on %s", label), lines))`. Lines per note: heading = summary; plain `author · 2006-01-02 15:04`; rationale lines (`noWrap` true, they are lists); blank between notes. No notes → status message `i18n.T("This shelf entry has no notes")`.
- Hint: `i18n.T("[n] note")` added after `[enter] diff/browse` only when the selected entry has notes.
- Bundles: "Notes on %s", "[n] note", "This shelf entry has no notes".
- [ ] RED: `TestShelfRowShowsNoteBadge` (popup with noteCounts → rendered box contains `◆1` on that row only); `TestShelfNKeyOpensReadOnlyNoteView` (feed `shelfNotesMsg` → top layer is `*contentPopup` whose lines include the summary and a rationale line; esc returns to the shelf popup); `TestShelfNKeyInertWithoutNotes`.
- [ ] Run → FAIL; implement; `go test ./internal/tui -run 'ShelfRowShowsNote|ShelfNKey'` → PASS; then `go test ./internal/tui` (i18n gates).
- [ ] Headless check: `./tui-capture.sh` on a repo with a noted shelf entry: `G` then `n`.
- [ ] Commit `feat(tui): shelf rows mark entries with notes; n reads them`.

### Task 5: Web — badge + "View note" dialog

**Files:** `internal/web/shelf.go`, `server.go` (route), `static/sidebar.js`; test `internal/web/shelf_notes_test.go`.

- `shelfRow.Notes int \`json:"notes,omitempty"\``; `handleShelf` fills it from `svc.NoteCounts(ctx).ByShelf` (best effort).
- `GET /api/shelf/notes?id=` → `{"notes":[{id,source,author,summary,rationale,created}]}` from `svc.ShelfNotes`; unknown id → empty list 200; missing id → 400.
- `renderShelf`: append `noteBadgeHTML`-style `<span class="notebadge">◆N</span>` (same class as files.js; the function lives in files.js — reuse via the existing import or a tiny local copy if sidebar.js cannot import it without a cycle).
- Shelf context menu: row "View note" when `e.notes > 0` → fetch → read-only dialog (the existing dialog/openInfo helper the sidebar uses for text; summary heading, author · date, `<pre>` rationale).
- [ ] RED: Go test: shelve + note → `GET /api/shelf` row has `notes:1`; `GET /api/shelf/notes?id=` returns the rationale; no id → 400.
- [ ] Run → FAIL; implement; `go test ./internal/web -run ShelfNotes` → PASS; `node --check internal/web/static/sidebar.js`; `go test ./internal/web`.
- [ ] Browser probe (playwright, per memory: rebuild + restart + hard reload; assert the badge and dialog are VISIBLE).
- [ ] Commit `feat(web): shelf rows mark entries with notes; View note dialog`.

### Task 6: domain — `shelveStagedIn` + the `ShelveStaged` seam

**Files:** `internal/engine/operation.go` (seam + `ErrNoShelve` + nil-safe `shelveStaged`), `internal/domain/shelf.go` (tar builder refactor + `shelveStagedIn`), `internal/domain/service.go` (wire in Execute). Test `internal/domain/shelve_staged_test.go`.

**Produces:**
```go
// engine/operation.go
// ShelveStaged freezes the INDEX of the worktree at dir (every path that
// differs from HEAD) into one shelf file set labelled "WIP on <branch>" and
// annotates it with a shelf-level note when deletions or renames cannot be
// carried by the set. Nil = ErrNoShelve.
ShelveStaged func(ctx context.Context, dir, branch string) (model.ShelfEntry, error)
var ErrNoShelve = errors.New("this repository handle cannot shelve")
func (d OpDeps) shelveStaged(ctx context.Context, dir, branch string) (model.ShelfEntry, error)
```
```go
// domain/shelf.go
func RecycleShelfLabel(branch string) string // "WIP on " + branch
const RecyclePlaceholder = "delete.me"
type shelfMember struct{ name string; data []byte }
func buildShelfTar(ms []shelfMember) ([]byte, error)      // used by ShelfAddFiles too
func (s *Service) shelveStagedIn(ctx context.Context, dir, branch string) (model.ShelfEntry, error)
```
`shelveStagedIn`:
1. `wt := s.repo.InDir(dir)`; `files, err := wt.DiffTreeFiles(ctx, model.CommitEndpoint("HEAD"…), model.IndexEndpoint())` — use whatever constructors `model.Endpoint` has for "commit HEAD" and "index" (resolve HEAD to a sha first with `wt.ResolveRev`/`RevParse` if the endpoint needs a hash).
2. For each file: `D` → deleted; `R*` → renamed (old→new) + member new path; else member path. Member bytes: `wt.ShowFile(ctx, "", path)` (index blob; `git -C dir show :path`).
3. No members → one `{delete.me, nil}` member, placeholder flag.
4. `buildShelfTar` → `st.PutFiles("", model.FileAddress{Worktree: dir, Branch: branch, State: model.StateStaged}, tar, RecycleShelfLabel(branch))`.
5. deleted/renamed/placeholder → `s.NoteAdd(ctx, model.Note{Source: agent, Author: "gg", Address: ShelfEntryNote(e.ID), Summary: fmt.Sprintf("Recycled from %s (%s)", dir, branch), Rationale: …})`; on error `st.Remove(e.ID)` and return the error.
Rationale text (exact):
```
delete.me is an empty placeholder: every change in this worktree was a deletion, and a shelf set cannot be empty.
Deleted (not in this set):
  a.go
Renamed (stored under the new path):
  old.go → new.go
```
(only the applicable parts, in that order).
Wire in `Execute`: `ShelveStaged: s.shelveStagedIn`.

- [ ] RED (real git, `t.TempDir()`, second worktree via `git worktree add`):
  - `TestShelveStagedStoresIndexAsOneSet` — modified + untracked (staged via `git -C wt add -A`) + deleted + renamed → one entry, label `WIP on feat`, tar members = modified, untracked, new rename path (content = index bytes); note rationale lists the deletion and `old → new`.
  - `TestShelveStagedNoNoteWhenOnlyModifications` — `ShelfNotes` empty.
  - `TestShelveStagedAllDeletionsAddsPlaceholder` — members == [`delete.me`] size 0; rationale starts with the placeholder sentence.
  - `TestShelveStagedSpaceAndUnicodePaths` — `a b.txt`, `ünï.txt` shelved.
  - `TestShelveStagedThroughExecuteDoesNotDeadlock` — a tiny test op calling `deps.ShelveStaged` run via `svc.Execute` with a 10 s context → returns.
- [ ] Run → FAIL; implement; `go test ./internal/domain -run ShelveStaged` → PASS; `go test ./internal/domain ./internal/engine`.
- [ ] Commit `feat(domain): ShelveStaged seam shelves another worktree's index as one set`.

### Task 7: engine — the `shelve` answer

**Files:** `internal/engine/recycle_worktree.go`; test `internal/engine/recycle_worktree_test.go` (extend).

- Options `[]string{"commit", "shelve", "discard", "abort"}`.
- `case "shelve"`: `Progress{Step: "shelving", Detail: target}`; `wt.StageAll`; `e, err := deps.shelveStaged(ctx, target, old)`; error → return; then the discard block (extract `discardAll(ctx, wt) error` shared with `case "discard"`); `shelved = e.Label`.
- Summary: `case shelved != "": res = res.AppendSummary("; shelved as %q", shelved)` (literal).
- [ ] RED: `TestRecycleShelveCallsSeamThenCleansAndSwitches` (real repo + worktree; decider answers `shelve`; fake seam records (dir, branch) and inspects that the target index holds the untracked file at call time; after run: target clean, on the new branch, summary contains `shelved as "WIP on`); `TestRecycleShelveSeamErrorLeavesChanges` (seam returns error → op errors, target still has the file, branch unchanged); `TestRecycleShelveWithoutSeam` (`ErrNoShelve`); option-list test asserts the new order.
- [ ] Run → FAIL; implement; `go test ./internal/engine -run Recycle` → PASS; `go test ./internal/engine` (prose gate).
- [ ] Commit `feat(engine): recycle.dirty gains shelve`.

### Task 8: TUI + CLI wiring, bundles, e2e, docs

**Files:** `internal/tui/i18n_display.go` (`case "shelve"`), `internal/tui/source.go` (RecycleWorktree adds the shelf source key — find its name next to srcBranches), `internal/cli/worktree.go` (`--on-dirty` accepts `shelve`, usage/help/error text), bundles (`shelve`, `; shelved as %q`, `shelving`), `e2e/scenarios/s102_worktree_recycle_shelve.toml`, CHANGELOG, README, `internal/agentskill/using-gg.md` + `Version` bump + regenerate `.claude/skills/using-gg/SKILL.md` (`gg init --update` from the worktree binary or the existing generator), `docs/CLAUDE-details.md`, `CLAUDE.md` engine row seam list (`ShelveStaged`).

- [ ] RED: `TestRecycleCLIOnDirtyShelve` in `internal/cli/worktree_recycle_test.go` (target with modified+deleted → exit 0, stdout `shelved as "WIP on`, `gg note list --shelf <id>` shows the deletion); bad value message lists shelve; TUI `optionDisplayName("shelve")` via the options vocab gate.
- [ ] e2e s102: steps write/commit README + `gone.txt` + `old.txt`, branch loose + feature/x, worktree wt-x; in wt-x: modify README, write new.txt, delete gone.txt (e2e has a delete/rm step? — use it if present, else `git rm` via a run step), rename old.txt → moved.txt. Run `worktree recycle --on-dirty=shelve ../wt-x loose` → exit 0, stdout contains `shelved as "WIP on feature/x"`; expect wt-x status clean. Second run `note list --shelf` is not possible without the id → assert via `shelf list` stdout contains `WIP on feature/x`.
- [ ] Run `go test ./internal/cli ./internal/tui ./e2e/...` → PASS.
- [ ] Docs + skill; `./test.sh race` → green.
- [ ] Commit `feat: gg worktree recycle --on-dirty=shelve; docs`.

## Final

- Self-review of the whole branch (no subagents) against the spec; fix Critical/Important with RED→GREEN.
- Deliver a stripped verify binary (`go build -ldflags="-s -w" -o ./gg ./cmd/gg`, SendUserFile).
- Ask before merging.
