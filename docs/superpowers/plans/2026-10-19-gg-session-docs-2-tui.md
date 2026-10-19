# gg session docs — plan 2 of 4: the TUI reader

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task, inline (this repo forbids implementer subagents; one read-only review subagent at the end). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The TUI attaches every worktree it shows to that worktree's session-docs directory, lists the directory's overviews in the open-files window, draws its notes on open files, closes locally and deletes only on an explicit confirmed action, and files agent tours as documents in the worker's worktree.

**Architecture:** Plan 1 made `internal/agentdocs` a cache of `<steer inbox>/docs/` behind `Store.Attach(root, worktreeDir, docsDir)`. This plan wires the TUI to it: the Model attaches the home worktree at start and every view slot as it loads, detaches a pruned slot, and reads documents exactly as it reads the store today (`syncOverviews` already lists every store overview as an open-files row; `syncNotes` already copies a file's notes). What changes in the TUI is identity (an overview document's id is its file name, carried on `openFile.ovID`), the close/delete split (`closeDoc` stops removing from the store; `deleteDoc`/`clearNotes` are the only unlinks, each behind the usual Delete/Cancel modal), the open-files rows (author and age), and the tours (attach the worker's root before `FileTour`). The steer `note_*`/`overview_*` verbs keep working and now write files through the attached store; plan 4 retires them.

**Tech Stack:** Go 1.26, Bubble Tea, `internal/agentdocs`, `internal/steer`, `internal/clock`, i18n bundles (ja/ko/zh/ru), the e2e harness (`tui.Headless`, golden screens).

**Spec:** `docs/superpowers/specs/2026-10-18-gg-session-docs-design.md` — §4.2 (reconcile), §4.4 (what a reader shows; close vs delete), §6.1 (In variants), §6.4 (TUI), §6.5 (tours), §12 (testing). Plan 1: `docs/superpowers/plans/2026-10-18-gg-session-docs-1-core.md`.

## Global Constraints

- Ids are unique PER WORKTREE: every store call on a document the TUI holds goes through the `…In(root, id)` form with `d.root` (spec §6.1). Memory `f<n>` ids stay for open FILES (`NextFileSeq`); an overview document's id is its file name.
- Lifetime: a document lives until the user or the writer deletes it. The TUI never unlinks on its own: not on close, not on quit, not on a slot swap, not on a cap eviction (spec §4.4 "A reader deletes only what the user asked for").
- Close is LOCAL. DELETE is a separate action behind the usual `Delete`/`Cancel` decision modal (default highlight Cancel), on an overview row, a note box, and "all notes on this file".
- Every user-visible string is an `i18n.T` literal key present in ALL FOUR bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`), inserted near related keys, never re-sorted. Steer reply prose stays English.
- Drawn ages use `clock.Now()`, never `time.Now()` (the e2e clock is frozen at 2026-01-02T00:00:00Z).
- No never-ending `tea.Cmd` escapes `m.quiet` (headless): the attach goroutine signals the store's broadcaster only; `waitDocsCmd` already returns nil when quiet.
- `./test.sh race` must print `all green` before merge. Commit with `gg add <files>` then `git commit -F <msgfile>`; never `git add -A`. Worktree: `/work/gigagit/.claude/worktrees/session-docs-tui` (branch `feat/session-docs-tui`).
- Unattached roots keep today's memory behaviour; every existing test built on `agentdocs.New()` without `Attach` must stay green unchanged except where this plan changes a behaviour on purpose (listed in Task 3).

## Review Focus

1. The same document id in two attached worktrees (two slots): every TUI action on one must leave the other's file untouched — Task 2 pins `d.id()`/`d.root` and the In sweep; Task 3 pins delete.
2. A session overview deleted from disk while it is on screen (in a split or a viewer): the TUI returns the user to where they were and says the overview was deleted; nothing else closes — Task 3.
3. `D` in the open-files window on a row that has nothing to delete (a plain file without notes, an AI result): no modal, a one-line status — Task 3.
4. A note file for a path that is NOT open: no file opens by itself; the files view's count shows it (`NoteCount`), and opening the file shows the box — Task 4.
5. The docs directory cannot be created or watched (read-only state dir): the TUI runs, the open-files window simply has no session rows, no status spam at every poll — Task 4.

---

### Task 1: agentdocs — root-scoped anchor checks

**Files:**
- Modify: `internal/agentdocs/overviews.go` (beside `CheckAnchors`, `SetAnchorMissing`)
- Test: `internal/agentdocs/anchors_in_test.go` (new)

**Interfaces:**
- Produces: `func (s *Store) CheckAnchorsIn(root, id string) bool`, `func (s *Store) SetAnchorMissingIn(root, id string, i int, missing bool) bool` — the root-scoped twins of `CheckAnchors`/`SetAnchorMissing`; `root == ""` means "the one root holding id" (today's behaviour), so the id-only forms become thin wrappers.

- [ ] **Step 1: Write the failing test**

```go
package agentdocs

import (
	"os"
	"path/filepath"
	"testing"
)

// Two attached worktrees hold the same overview id: the id-only check finds
// nobody (ambiguous), the root-scoped one checks exactly its worktree.
func TestAnchorChecksAreRootScoped(t *testing.T) {
	t.Parallel()
	s := New()
	defer s.Close()
	wtA, wtB := t.TempDir(), t.TempDir()
	dirA, dirB := filepath.Join(t.TempDir(), "docs"), filepath.Join(t.TempDir(), "docs")
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		writeRaw(t, d, "same.md", "+++\ntitle = \"T\"\n+++\n[a](a.go:1)\n")
	}
	if err := os.WriteFile(filepath.Join(wtB, "a.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err) // only B has the file
	}
	if err := s.Attach("A", wtA, dirA); err != nil {
		t.Fatal(err)
	}
	if err := s.Attach("B", wtB, dirB); err != nil {
		t.Fatal(err)
	}
	if s.CheckAnchors("same") {
		t.Fatal("an ambiguous id must check nothing")
	}
	if !s.CheckAnchorsIn("A", "same") {
		t.Fatal("A's anchor is missing: the check must change its flag")
	}
	a, _ := s.OverviewIn("A", "same")
	b, _ := s.OverviewIn("B", "same")
	if !a.Anchors[0].Missing || b.Anchors[0].Missing {
		t.Fatalf("A missing=%v B missing=%v", a.Anchors[0].Missing, b.Anchors[0].Missing)
	}
	if !s.SetAnchorMissingIn("B", "same", 0, true) || s.SetAnchorMissingIn("B", "same", 0, true) {
		t.Fatal("SetAnchorMissingIn must report the change exactly once")
	}
	if s.SetAnchorMissing("same", 0, false) {
		t.Fatal("an ambiguous id must set nothing")
	}
	if b, _ = s.OverviewIn("B", "same"); !b.Anchors[0].Missing {
		t.Fatal("B's flag was not set")
	}
}
```

(`writeRaw` exists in `attach_test.go`.)

- [ ] **Step 2: Run it**

Run: `cd /work/gigagit/.claude/worktrees/session-docs-tui && go test ./internal/agentdocs -run TestAnchorChecksAreRootScoped`
Expected: FAIL — `s.CheckAnchorsIn undefined`.

- [ ] **Step 3: Implement**

In `overviews.go`, turn `CheckAnchors`/`SetAnchorMissing` into wrappers and move the bodies into root-scoped forms that resolve with `ovResolveLocked(root, id)` (already handles `root == ""` as "the one root"):

```go
// CheckAnchors is CheckAnchorsIn for the one root holding id (an id two
// attached worktrees share checks nothing — a frontend showing several
// roots uses CheckAnchorsIn).
func (s *Store) CheckAnchors(id string) bool { return s.CheckAnchorsIn("", id) }

// CheckAnchorsIn marks the anchors of root's overview id that do not
// resolve: … (the existing doc comment)
func (s *Store) CheckAnchorsIn(root, id string) bool {
	s.mu.Lock()
	e, err := s.ovResolveLocked(root, id)
	if err != nil {
		s.mu.Unlock()
		return false
	}
	key := ovKey{e.Root, id}
	// … the existing body from `text, dir, anchors := …` unchanged
}

func (s *Store) SetAnchorMissing(id string, i int, missing bool) bool {
	return s.SetAnchorMissingIn("", id, i, missing)
}

// SetAnchorMissingIn records what an open found on root's overview id: …
func (s *Store) SetAnchorMissingIn(root, id string, i int, missing bool) bool {
	s.mu.Lock()
	e, err := s.ovResolveLocked(root, id)
	if err != nil || i < 0 || i >= len(e.Anchors) || e.Anchors[i].Missing == missing {
		s.mu.Unlock()
		return false
	}
	// … unchanged
}
```

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/agentdocs`
Expected: PASS (the new test and every existing one).

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/session-docs-tui
gg add internal/agentdocs/overviews.go internal/agentdocs/anchors_in_test.go
git commit -F /tmp/claude-1000/-work-gigagit/3f942bc5-9389-49a0-a00f-fef0425e1a09/scratchpad/c1.txt
```
Message: `agentdocs: CheckAnchorsIn / SetAnchorMissingIn — anchor checks scoped to a root (ids are per worktree)`.

---

### Task 2: the TUI's overview document carries the store's id, author and created

**Files:**
- Modify: `internal/tui/open_file.go` (struct + `id()`), `internal/tui/overview.go` (`newOverviewDocFrom`, `adoptOverview`), `internal/tui/agentdocs_track.go:75`, `internal/tui/steer_overview.go:116,128-133,141`, `internal/tui/agent_tours.go` (`checkTourCmd`), `internal/tui/agent_tours_open.go` (its `checkTourCmd` calls), `internal/tui/overview_keys.go:127,154,226`, `internal/tui/split_overview.go:197`, `internal/tui/open_file_notes.go:104`
- Test: `internal/tui/session_docs_test.go` (new; the fixtures every later task reuses)

**Interfaces:**
- Produces: `openFile.ovID string` (the store's overview id; `id()` returns it when set), `openFile.ovAuthor string`, `openFile.ovCreated time.Time`; test fixtures `attachedDocsModel(t) (Model, root, dir string)`, `writeSessionOverview(t, dir, id, title, created, text string)`, `writeSessionNote(t, dir, id, path string, start, end int, summary string, lines []string)`.
- Consumes: Task 1's `CheckAnchorsIn`, `SetAnchorMissingIn`.

- [ ] **Step 1: Write the fixtures and the failing tests**

```go
package tui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
)

// attachedDocsModel is privateDocsModel with its worktree attached to a
// session-docs directory of the test's own (spec 2026-10-18-gg-session-docs).
func attachedDocsModel(t *testing.T) (m Model, root, dir string) {
	t.Helper()
	m = privateDocsModel(t)
	root = domain.CheckoutKey(m.currentWorktree)
	dir = filepath.Join(t.TempDir(), "docs")
	m.docsDirOf = func(string) string { return dir } // Task 4 reads this hook; until then Attach directly
	if err := m.docs.Attach(root, m.currentWorktree, dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.docs.Close)
	return m, root, dir
}

// writeSessionOverview lands an overview document as an agent would (whole
// file, gg's own encoder) and has the store read the directory again.
func writeSessionOverview(t *testing.T, m Model, dir, id, title, created, text string) {
	t.Helper()
	var ts time.Time
	if created != "" {
		var err error
		if ts, err = time.Parse(time.RFC3339, created); err != nil {
			t.Fatal(err)
		}
	}
	data, err := agentdocs.EncodeOverviewFile(agentdocs.OverviewFile{Title: title, Author: "agent", Created: ts, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	if err := agentdocs.WriteDoc(dir, id, agentdocs.KindOverview, data, true); err != nil {
		t.Fatal(err)
	}
	m.docs.Reload(domain.CheckoutKey(m.currentWorktree))
}

// writeSessionNote lands a note document on path's lines start..end.
func writeSessionNote(t *testing.T, m Model, dir, id, path string, start, end int, summary string, lines []string) {
	t.Helper()
	data, err := agentdocs.EncodeNoteFile(agentdocs.NoteFile{Path: path, Start: start, End: end, Summary: summary, Author: "agent", Lines: lines})
	if err != nil {
		t.Fatal(err)
	}
	if err := agentdocs.WriteDoc(dir, id, agentdocs.KindNote, data, true); err != nil {
		t.Fatal(err)
	}
	m.docs.Reload(domain.CheckoutKey(m.currentWorktree))
}

// A session overview joins the open-files list under its FILE id, carrying
// its author and created stamp; a memory overview keeps f<n>.
func TestASessionOverviewIsListedUnderItsFileID(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "20261019T000000-ab12", "Tour", "2026-01-01T22:00:00Z", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	d, why := m.findOverview("20261019T000000-ab12")
	if d == nil {
		t.Fatal(why)
	}
	if d.id() != "20261019T000000-ab12" || d.ovAuthor != "agent" || d.ovCreated.IsZero() || d.key() != docKey(fileSource{kind: srcOverview}, "overview-20261019T000000-ab12.md") {
		t.Fatalf("id=%q author=%q created=%v key=%q", d.id(), d.ovAuthor, d.ovCreated, d.key())
	}
	files := m.openFilesProto()
	if len(files) != 1 || files[0].ID != "20261019T000000-ab12" || files[0].Source != "overview" {
		t.Fatalf("files = %+v", files)
	}
	// A memory root (no Attach) still numbers its overviews.
	pm := privateDocsModel(t)
	pm, pd := addOverviewViaSteer(t, pm, "Mem", "[a](a.txt:1)")
	if pd.id() != "f"+itoa(pd.seq) {
		t.Fatalf("memory id = %q", pd.id())
	}
}

// A replaced file keeps the document (same id, new text) and refreshes the
// author it declares.
func TestAReplacedSessionOverviewIsAdoptedInPlace(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	d, _ := m.findOverview("ov-1")
	writeSessionOverview(t, m, dir, "ov-1", "Tour 2", "", "[a](a.txt:1) [b](a.txt:2)")
	m, _ = m.onAgentDocsChanged()
	if d2, _ := m.findOverview("ov-1"); d2 != d || d.title != "Tour 2" || len(d.ov.anchors) != 2 {
		t.Fatalf("doc=%v title=%q anchors=%d", d2 == d, d.title, len(d.ov.anchors))
	}
}
```

(`itoa` = `strconv.FormatInt(…, 10)`; write it inline or import strconv.) The `m.docsDirOf` line will not compile until Task 4 — for this task write the fixture WITHOUT that line and add it in Task 4.

- [ ] **Step 2: Run them**

Run: `go test ./internal/tui -run 'TestASessionOverviewIsListedUnderItsFileID|TestAReplacedSessionOverviewIsAdoptedInPlace'`
Expected: FAIL — `d.ovAuthor undefined` (compile), then, once the fields exist, `id=f1760…` (the UnixNano seq).

- [ ] **Step 3: Implement**

`open_file.go`:
```go
	// ovID is an overview DOCUMENT's id — its file name in the session-docs
	// directory (spec 2026-10-18-gg-session-docs §3.1), the store's id for a
	// memory overview ("f<n>"); "" for a file. id() returns it when set.
	ovID string
	// ovAuthor / ovCreated are the document's front matter (zero for a
	// memory overview): the open-files row shows them.
	ovAuthor  string
	ovCreated time.Time
```
```go
// id is the document's agent-facing id: an overview document's own id, else
// f<seq> (gg session files focus <id>).
func (d *openFile) id() string {
	if d.ovID != "" {
		return d.ovID
	}
	return "f" + strconv.FormatInt(d.seq, 10)
}
```
`overview.go`:
```go
func newOverviewDocFrom(o agentdocs.Overview) *openFile {
	d := newOpenFileSeq(fileSource{kind: srcOverview}, "overview-"+o.ID+".md", o.Seq) // named by its id; seq only tags the instance
	d.ovID, d.ovAuthor, d.ovCreated = o.ID, o.Author, o.Created
	d.title, d.backgrounded = o.Title, true
	…
}
```
In `adoptOverview`, first line: `d.ovAuthor, d.ovCreated = o.Author, o.Created`.

The In sweep (each a one-line change):
- `agentdocs_track.go:75` → `m.docs.OverviewIn(d.root, d.id())`
- `steer_overview.go:116` → `m.docs.SetOverviewIn(d.root, d.id(), c.Title, c.Text)`; `:141` → `OverviewIn(d.root, d.id())`; `checkAnchorsCmd`: capture `root := d.root` and call `docs.CheckAnchorsIn(root, id)`
- `agent_tours.go`: `checkTourCmd(ids []string)` → `checkTourCmd(tours []agentdocs.Overview)` calling `docs.CheckAnchorsIn(o.Root, o.ID)`; `fileBriefTour` passes `[]agentdocs.Overview{o}`; `fileReportTours` collects `filed = append(filed, o)`; `agent_tours_open.go` `openTour` passes `[]agentdocs.Overview{o}`
- `overview_keys.go:127,154,226`, `split_overview.go:197` → `SetAnchorMissingIn(ov.root, ov.id(), …)`
- `open_file_notes.go:104` → `d.docs.RemoveNoteIn(d.root, id)`

- [ ] **Step 4: Run the TUI package**

Run: `go test ./internal/tui 2>&1 | tail -5`
Expected: PASS. (`TestOverviewListShowRm` still passes: memory ids unchanged.)

- [ ] **Step 5: Commit**

Message: `tui: an overview document is identified by its file id (openFile.ovID), carries author and created; every store call on a held document uses its root`.

---

### Task 3: close is local; delete unlinks after a confirm

**Files:**
- Modify: `internal/tui/open_files.go` (`closeDoc`), `internal/tui/agentdocs_track.go` (`syncOverviews` prose), `internal/tui/steer_overview.go` (`overview_rm`), `internal/tui/sessions_popup.go` (`D` key, hint), `internal/tui/open_file_note_keys.go` (`d` key, "Dismiss note" row, `noteHint`, new "Delete all notes on this file" row), `internal/tui/overview_keys.go` (`overviewRows`: "Delete overview" for `srcOverview`), `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Create: `internal/tui/session_docs_delete.go` (the confirms and `deleteDoc`)
- Test: `internal/tui/session_docs_test.go` (append), update `internal/tui/agentdocs_track_test.go:158-170`, `internal/tui/agentdocs_overview_track_test.go:26-41`, `internal/tui/steer_overview_test.go:133`

**Interfaces:**
- Produces: `func (m Model) deleteDoc(d *openFile) Model` (unlinks an overview document through the store, then drops it from screen and list); `func (m Model) confirmSessionOverviewDelete(d *openFile) (tea.Model, tea.Cmd)`; `func (m Model) confirmFileNoteDelete(d *openFile, n agentdocs.Note) (tea.Model, tea.Cmd)`; `func (m Model) confirmFileNotesDelete(d *openFile) (tea.Model, tea.Cmd)`; `func (m Model) sessionsDeleteRow(d *openFile) (Model, tea.Cmd)` (the `D` key's dispatch).

- [ ] **Step 1: Write the failing tests**

Append to `session_docs_test.go`:

```go
// X on a shown session overview is local: it leaves the screen, stays in
// the list and in the store, and its file stays.
func TestClosingASessionOverviewIsLocal(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	d, _ := m.findOverview("ov-1")
	m, _ = m.showOverview(d)
	if !m.docShown(d) {
		t.Fatal("fixture: not shown")
	}
	m = m.closeDoc(d)
	if m.docShown(d) || !m.openFiles.has(m.currentWorktree, d) || !d.backgrounded {
		t.Fatalf("shown=%v listed=%v bg=%v", m.docShown(d), m.openFiles.has(m.currentWorktree, d), d.backgrounded)
	}
	if _, err := os.Stat(filepath.Join(dir, "ov-1.md")); err != nil {
		t.Fatal("close unlinked the file")
	}
}

// Closing a file with notes leaves the notes in the store (the files on
// disk): a reader deletes only what the user asked for.
func TestClosingAFileKeepsItsNotes(t *testing.T) {
	t.Parallel()
	m, root, dir := attachedDocsModel(t)
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	writeSessionNote(t, m, dir, "n-1", "a.txt", 2, 2, "s", []string{"x2"})
	m, _ = m.onAgentDocsChanged()
	if len(d.notes) != 1 {
		t.Fatalf("notes = %+v", d.notes)
	}
	m = m.closeDoc(d)
	if m.openFiles.has(m.currentWorktree, d) || m.docs.NoteCount(root, "a.txt") != 1 {
		t.Fatalf("listed=%v notes=%d", m.openFiles.has(m.currentWorktree, d), m.docs.NoteCount(root, "a.txt"))
	}
	if _, err := os.Stat(filepath.Join(dir, "n-1.toml")); err != nil {
		t.Fatal("close unlinked the note")
	}
}

// deleteDoc unlinks the overview's file; the row and the screen follow.
func TestDeletingASessionOverviewUnlinksItsFile(t *testing.T) {
	t.Parallel()
	m, root, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	d, _ := m.findOverview("ov-1")
	m, _ = m.showOverview(d)
	m = m.deleteDoc(d)
	if _, err := os.Stat(filepath.Join(dir, "ov-1.md")); !os.IsNotExist(err) {
		t.Fatal("the file is still there")
	}
	if _, ok := m.docs.OverviewIn(root, "ov-1"); ok || m.openFiles.has(m.currentWorktree, d) || m.docShown(d) {
		t.Fatal("the overview is still held")
	}
}

// The D key in the open-files window asks first; Delete unlinks, Cancel
// keeps. A row with nothing to delete only says so.
func TestDInTheOpenFilesWindowAsksThenDeletes(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	plain := bgDoc(m, fileSource{kind: srcWorktree}, "b.txt", 3)
	m, _ = m.onAgentDocsChanged()
	d, _ := m.findOverview("ov-1")
	m, _ = m.openSessionsPopup(false)
	p := m.topLayer().(*sessionsPopup)
	p.tab = tabFiles
	p.refresh(m)
	// Cursor on the plain file: nothing to delete.
	for i, f := range p.files {
		if f == plain {
			p.sel = i
		}
	}
	m, _ = p.update(m, keyMsg("D"))
	if m.modal != nil || m.statusMsg != "nothing to delete on this row" {
		t.Fatalf("modal=%v status=%q", m.modal != nil, m.statusMsg)
	}
	for i, f := range p.files {
		if f == d {
			p.sel = i
		}
	}
	m, _ = p.update(m, keyMsg("D"))
	if m.modal == nil || m.modal.req.ID != "session-overview-delete" || m.modal.sel != 1 {
		t.Fatalf("modal = %+v", m.modal)
	}
	tm, _ := m.modal.onResolve(m, "Cancel")
	m = tm.(Model)
	if _, err := os.Stat(filepath.Join(dir, "ov-1.md")); err != nil {
		t.Fatal("Cancel unlinked the file")
	}
	m, _ = p.update(m, keyMsg("D"))
	tm, _ = m.modal.onResolve(m, "Delete")
	m = tm.(Model)
	if _, err := os.Stat(filepath.Join(dir, "ov-1.md")); !os.IsNotExist(err) {
		t.Fatal("Delete left the file")
	}
}

// d on a note box asks, then unlinks that one note; "delete all notes on
// this file" unlinks every note file with that path.
func TestNoteDeletionAsksThenUnlinks(t *testing.T) {
	t.Parallel()
	m, root, dir := attachedDocsModel(t)
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	writeSessionNote(t, m, dir, "n-1", "a.txt", 2, 2, "one", []string{"x2"})
	writeSessionNote(t, m, dir, "n-2", "a.txt", 4, 4, "two", []string{"x4"})
	m, _ = m.onAgentDocsChanged()
	tm, _ := m.confirmFileNoteDelete(d, d.notes[0])
	m = tm.(Model)
	if m.modal == nil || m.modal.req.ID != "session-note-delete" {
		t.Fatalf("modal = %+v", m.modal)
	}
	tm, _ = m.modal.onResolve(m, "Delete")
	m = tm.(Model)
	if m.docs.NoteCount(root, "a.txt") != 1 || m.statusMsg != "note n-1 deleted" {
		t.Fatalf("count=%d status=%q", m.docs.NoteCount(root, "a.txt"), m.statusMsg)
	}
	tm, _ = m.confirmFileNotesDelete(d)
	m = tm.(Model)
	tm, _ = m.modal.onResolve(m, "Delete")
	m = tm.(Model)
	if m.docs.NoteCount(root, "a.txt") != 0 || m.statusMsg != "deleted the note" {
		t.Fatalf("count=%d status=%q", m.docs.NoteCount(root, "a.txt"), m.statusMsg)
	}
	if _, err := os.Stat(filepath.Join(dir, "n-2.toml")); !os.IsNotExist(err) {
		t.Fatal("n-2.toml is still there")
	}
}

// A session overview whose file disappears closes on screen with a word; a
// background one leaves quietly.
func TestADeletedSessionOverviewClosesWithAWord(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	d, _ := m.findOverview("ov-1")
	m, _ = m.showOverview(d)
	if err := os.Remove(filepath.Join(dir, "ov-1.md")); err != nil {
		t.Fatal(err)
	}
	m.docs.Reload(domain.CheckoutKey(m.currentWorktree))
	m, _ = m.onAgentDocsChanged()
	if m.docShown(d) || m.openFiles.has(m.currentWorktree, d) || m.statusMsg != `overview ov-1 "Tour" was deleted` {
		t.Fatalf("shown=%v listed=%v status=%q", m.docShown(d), m.openFiles.has(m.currentWorktree, d), m.statusMsg)
	}
}

// The agent's overview_rm removes its document (the file goes), and says so.
func TestOverviewRmRemovesTheDocument(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "", "[a](a.txt:1)")
	m, _ = m.onAgentDocsChanged()
	m, r := applyOverview(t, m, steer.Command{ID: "rm-1", Cmd: "overview_rm", FileID: "ov-1", Wait: true})
	if !r.OK || r.Detail != "removed ov-1" {
		t.Fatalf("reply = %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "ov-1.md")); !os.IsNotExist(err) {
		t.Fatal("the file is still there")
	}
}
```

(`keyMsg` — find the existing test helper that builds a `tea.KeyMsg` from a string, e.g. in `sessions_popup_test.go` or `keys_test.go`; use its name.)

Update the three existing tests:
- `agentdocs_track_test.go` `TestXDuringAReReadClearsTheStoresNotes` → rename `TestXDuringAReReadKeepsTheStoresNotes`, assert `NoteCount == 1` after `closeDoc` and the document is out of the list.
- `agentdocs_overview_track_test.go` `TestAnOverviewRemovedElsewhereClosesInTheTUI`: expected status `overview <id> "Tour" was deleted`.
- `steer_overview_test.go:133`: `x.Detail != "removed "+id`.

- [ ] **Step 2: Run them**

Run: `go test ./internal/tui -run 'Session|Deleting|DInTheOpenFiles|NoteDeletion|OverviewRm|TestXDuringAReRead|RemovedElsewhere|TestOverviewListShowRm'`
Expected: FAIL — `m.deleteDoc undefined`, then behaviour failures.

- [ ] **Step 3: Implement**

`open_files.go` — `closeDoc`:
```go
// closeDoc closes d (X or esc on it, x in the switcher). A FILE leaves the
// screen and the list; its notes stay in the store — the documents in the
// session-docs directory are deleted only on purpose (deleteDoc,
// clearNotes). A session OVERVIEW the store still holds closes LOCALLY: it
// leaves the screen and stays in the list (spec 2026-10-18-gg-session-docs
// §4.4); one the store no longer holds leaves the list too.
func (m Model) closeDoc(d *openFile) Model {
	if d.ov != nil && d.src.kind == srcOverview && m.docs != nil {
		if _, held := m.docs.OverviewIn(d.root, d.id()); held {
			return m.backgroundDoc(d)
		}
	}
	if d.ov != nil {
		d.ov.forgetDiff()
	}
	m = m.detachDoc(d)
	m.openFiles.remove(m.currentWorktree, d)
	return m
}
```
`session_docs_delete.go`:
```go
package tui

// Deleting session documents (spec 2026-10-18-gg-session-docs §4.4): close
// is local, DELETE unlinks the file — after the usual Delete/Cancel modal.

// deleteDoc removes overview d's document through the store (its file is
// unlinked) and takes it off the screen and out of the list.
func (m Model) deleteDoc(d *openFile) Model {
	if d.ov != nil && d.src.kind == srcOverview && m.docs != nil {
		m.docs.RemoveOverviewIn(d.root, d.id())
	}
	if d.ov != nil {
		d.ov.forgetDiff()
	}
	m = m.detachDoc(d)
	m.openFiles.remove(m.currentWorktree, d)
	return m
}

// confirmSessionOverviewDelete asks before an agent's overview document goes.
func (m Model) confirmSessionOverviewDelete(d *openFile) (tea.Model, tea.Cmd) {
	m.modal = &decisionState{
		req: engine.DecisionRequest{ID: "session-overview-delete", Prompt: i18n.T("Delete this overview?") + "\n" + strconv.Quote(d.title),
			Options: []string{"Delete", "Cancel"}},
		sel: 1, // default highlight = Cancel
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt != "Delete" {
				return m, nil
			}
			m = m.deleteDoc(d)
			m.statusMsg = i18n.T("overview %s deleted", d.overviewLabel())
			return m, nil
		},
	}
	return m, nil
}

// confirmFileNoteDelete asks before one note on d goes.
func (m Model) confirmFileNoteDelete(d *openFile, n agentdocs.Note) (tea.Model, tea.Cmd) {
	m.modal = &decisionState{
		req: engine.DecisionRequest{ID: "session-note-delete", Prompt: i18n.T("Delete this note?") + "\n" + n.Summary,
			Options: []string{"Delete", "Cancel"}},
		sel: 1,
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt != "Delete" {
				return m, nil
			}
			d.removeNote(n.ID)
			m.statusMsg = i18n.T("note %s deleted", n.ID)
			return m, nil
		},
	}
	return m, nil
}

// confirmFileNotesDelete asks before every note on d goes.
func (m Model) confirmFileNotesDelete(d *openFile) (tea.Model, tea.Cmd) {
	n := len(d.notes)
	prompt := i18n.T("Delete the %d notes on %s?", n, d.path)
	if n == 1 {
		prompt = i18n.T("Delete the note on %s?", d.path)
	}
	m.modal = &decisionState{
		req:  engine.DecisionRequest{ID: "session-notes-delete", Prompt: prompt, Options: []string{"Delete", "Cancel"}},
		sel:  1,
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt != "Delete" {
				return m, nil
			}
			k := d.clearNotes()
			m.statusMsg = i18n.T("deleted %d notes", k)
			if k == 1 {
				m.statusMsg = i18n.T("deleted the note")
			}
			return m, nil
		},
	}
	return m, nil
}

// sessionsDeleteRow is D on an open-files row: an overview document asks to
// be deleted, a file with notes asks to lose them, anything else only says.
func (m Model) sessionsDeleteRow(d *openFile) (Model, tea.Cmd) {
	switch {
	case d.ov != nil && d.src.kind == srcOverview && m.docs != nil:
		if _, held := m.docs.OverviewIn(d.root, d.id()); held {
			tm, cmd := m.confirmSessionOverviewDelete(d)
			return tm.(Model), cmd
		}
	case d.hasNotes():
		tm, cmd := m.confirmFileNotesDelete(d)
		return tm.(Model), cmd
	}
	m.statusMsg = i18n.T("nothing to delete on this row")
	return m, nil
}
```
(`"deleted %d notes"`, `"deleted the note"`, `"Delete this overview?"`, `"Delete overview"` are existing keys.)

`sessions_popup.go` `update`: add
```go
	case "D":
		if d, ok := p.currentFile(); ok {
			m, cmd := m.sessionsDeleteRow(d)
			p.refresh(m)
			return m, cmd
		}
```
and the Open files hint becomes `i18n.T("[enter] open  [x] close  [D] delete  [/] filter  [z] mode  [tab] agents  [ctrl+t] full  [esc] close")`.

`agentdocs_track.go` `syncOverviews`: status `i18n.T("overview %s was deleted", d.overviewLabel())`; the `gone` loop calls `m.closeDoc(d)` — which now backgrounds a held overview, but a gone one is not held, so it leaves the list; keep the call.

`steer_overview.go` `overview_rm`: `m = m.deleteDoc(d)` and reply `steerOK(c, "removed "+d.id())`.

`open_file_note_keys.go`: the `d` key → `return m.confirmFileNoteDelete(d, *n)` (with `tm.(Model)` unwrap); the row `note-dismiss` → id `note-delete`, label `i18n.T("Delete note")`, run → `m.confirmFileNoteDelete(d, n)`; append a row `{id: "notes-delete-all", label: i18n.T("Delete all notes on this file"), run: … m.confirmFileNotesDelete(d)}` (only when `!d.readOnlyNote(&n)`); `noteHint` → `"file: [}/{] notes  [d] delete  [r] reference  [esc] background  [X] close  [enter] full note  [alt+↑↓] line  [/] find  [←/tab] back to tree"`.

`overview_keys.go` `overviewRows`: after the export row, for `d.src.kind == srcOverview && m.docs != nil` (and held): `rows = append(rows, actionRow{id: "overview-delete", label: i18n.T("Delete overview"), run: func(m Model) (tea.Model, tea.Cmd) { return m.confirmSessionOverviewDelete(d) }})`.

Bundles (all four, near the existing neighbours; remove the keys no call site uses any more: `"Dismiss note"`, `"note %s dismissed"`, `"overview %s was closed in the browser"`, the old Open-files hint, the old `file: [}/{] notes  [d] dismiss …` hint): new keys
`"overview %s was deleted"`, `"overview %s deleted"`, `"Delete note"`, `"note %s deleted"`, `"Delete this note?"`, `"Delete all notes on this file"`, `"Delete the note on %s?"`, `"Delete the %d notes on %s?"`, `"nothing to delete on this row"`, the new Open-files hint, the new file hint.

- [ ] **Step 4: Run the TUI package and the i18n gates**

Run: `go test ./internal/tui ./internal/i18n 2>&1 | tail -5`
Expected: PASS (the AST gates accept the new keys; no stale key reported).

- [ ] **Step 5: Commit**

Message: `tui: close is local, delete unlinks — D in the open-files window, Delete note / Delete all notes on this file / Delete overview behind the Delete-Cancel modal; overview_rm removes the document`.

---

### Task 4: the TUI attaches every worktree it shows

**Files:**
- Create: `internal/tui/session_docs.go`
- Modify: `internal/tui/model.go` (field `docsDirOf`; `snapshotTargetMsg` case; `reRoot`), `internal/tui/run.go:107` (after `initSteerInbox`), `internal/tui/headless.go` (`NewHeadless` after `m.docs = agentdocs.New()`; `Close`), `internal/tui/worktree_view.go` (`loadView`, `pruneViews`), `internal/steer/steer.go` (`DocsDir("") == ""`)
- Test: `internal/tui/session_docs_test.go` (append), `internal/steer/docs_test.go` (append)

**Interfaces:**
- Produces: `Model.docsDirOf func(worktree string) string` (nil = the real layout; tests inject), `func (m Model) docsDirFor(worktree string) string`, `func (m Model) attachDocs(worktree string)`, `func (m Model) attachShownRoots() Model` (attach home + every slot, then `syncOverviews`), `func (m Model) detachDocs(worktree string)`.
- Consumes: `steer.DocsDir`, `steerDirFor`, `agentdocs.Store.Attach/Detach/Close`.

- [ ] **Step 1: Write the failing tests**

`internal/steer/docs_test.go` (append):
```go
// No inbox, no docs directory: a caller that has not resolved its inbox yet
// must not attach to a relative "docs".
func TestDocsDirOfNoInboxIsEmpty(t *testing.T) {
	t.Parallel()
	if got := DocsDir(""); got != "" {
		t.Fatalf("DocsDir(\"\") = %q", got)
	}
}
```
`internal/tui/session_docs_test.go` (append; and add the `m.docsDirOf` line to `attachedDocsModel`, replacing the direct `Attach` with `m = m.attachShownRoots()`):
```go
// Without a resolved inbox nothing attaches; once the snapshot target lands
// (the inbox's beat) the home worktree is attached and its documents listed.
func TestTheHomeWorktreeAttachesWhenTheInboxResolves(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	root := domain.CheckoutKey(m.currentWorktree)
	m.snapshotCommonDir, m.snapshotWorktree = "", ""
	if m.docsDirFor(m.currentWorktree) != "" || m.docs.Attached(root) != "" {
		t.Fatal("attached with no inbox")
	}
	dir := filepath.Join(t.TempDir(), "docs")
	m.docsDirOf = func(wt string) string {
		if model.SamePath(wt, m.currentWorktree) {
			return dir
		}
		return ""
	}
	writeRawDoc(t, dir, "ov-1.md", "+++\ntitle = \"Tour\"\n+++\n[a](a.txt:1)\n")
	tm, _ := m.Update(snapshotTargetMsg{svc: m.svc, commonDir: "/c", worktree: m.currentWorktree})
	m = tm.(Model)
	t.Cleanup(m.docs.Close)
	if m.docs.Attached(root) != dir {
		t.Fatalf("attached = %q", m.docs.Attached(root))
	}
	if d, why := m.findOverview("ov-1"); d == nil {
		t.Fatal(why)
	}
}

// A note file for a path that is not open opens nothing; the store counts it
// and the file shows the box once opened.
func TestANoteOnAClosedFileWaitsForTheFile(t *testing.T) {
	t.Parallel()
	m, root, dir := attachedDocsModel(t)
	writeSessionNote(t, m, dir, "n-1", "a.txt", 2, 2, "s", []string{"x2"})
	m, _ = m.onAgentDocsChanged()
	if len(m.openFiles.list(m.currentWorktree)) != 0 || m.docs.NoteCount(root, "a.txt") != 1 {
		t.Fatalf("open=%d count=%d", len(m.openFiles.list(m.currentWorktree)), m.docs.NoteCount(root, "a.txt"))
	}
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	if len(d.notes) != 1 || d.notes[0].Start != 2 {
		t.Fatalf("notes = %+v", d.notes)
	}
}

// An unwritable docs directory attaches nothing and says nothing: the TUI
// runs without session rows.
func TestAnUnusableDocsDirIsSilent(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	file := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.docsDirOf = func(string) string { return filepath.Join(file, "docs") } // MkdirAll fails: a file in the way
	m.statusMsg = "quiet"
	m = m.attachShownRoots()
	if m.docs.Attached(domain.CheckoutKey(m.currentWorktree)) != "" || m.statusMsg != "quiet" {
		t.Fatalf("attached=%q status=%q", m.docs.Attached(domain.CheckoutKey(m.currentWorktree)), m.statusMsg)
	}
}
```
(`writeRawDoc(t, dir, name, text)`: `os.MkdirAll(dir)` + `os.WriteFile` — write it in the test file; `snapshotTargetMsg`'s field names: read `session_snapshot.go` and use the real ones.)

- [ ] **Step 2: Run them**

Run: `go test ./internal/steer -run TestDocsDirOfNoInboxIsEmpty; go test ./internal/tui -run 'TestTheHomeWorktreeAttaches|TestANoteOnAClosedFile|TestAnUnusableDocsDir'`
Expected: FAIL — `DocsDir("") = "docs"`; `m.docsDirOf undefined`.

- [ ] **Step 3: Implement**

`steer.go`: `func DocsDir(inbox string) string { if inbox == "" { return "" }; return filepath.Join(inbox, DocsInbox) }`.

`session_docs.go`:
```go
package tui

// The TUI as a session-docs READER (spec 2026-10-18-gg-session-docs §4,
// §6.4): every worktree it shows — the home one and every view slot — is
// attached to that worktree's docs directory, under the same session dir
// the steer inbox lives in. Attach reloads synchronously, so the rows are
// there the moment a slot shows. Detach only when a slot is pruned or the
// repository changes; a swap keeps its documents.

// docsDirFor is worktree's session-docs directory, "" while the session dir
// is not resolved yet (the snapshot target lands it) — then nothing attaches.
func (m Model) docsDirFor(worktree string) string {
	if m.docsDirOf != nil {
		return m.docsDirOf(worktree) // tests
	}
	return steer.DocsDir(steerDirFor(m.snapshotCommonDir, worktree))
}

// attachDocs binds worktree's root to its docs directory. A directory that
// cannot be made or read attaches nothing and says nothing: the TUI runs
// without session rows (review focus 5).
func (m Model) attachDocs(worktree string) {
	dir := m.docsDirFor(worktree)
	if dir == "" || worktree == "" || m.docs == nil {
		return
	}
	_ = m.docs.Attach(domain.CheckoutKey(worktree), worktree, dir)
}

// attachShownRoots attaches the home worktree and every slot, then lists
// what their directories hold.
func (m Model) attachShownRoots() Model {
	for _, v := range m.views {
		m.attachDocs(v.path)
	}
	m.attachDocs(m.currentWorktree)
	return m.syncOverviews()
}

// detachDocs forgets worktree's documents (a pruned slot, a repo switch).
func (m Model) detachDocs(worktree string) {
	if m.docs != nil && worktree != "" {
		m.docs.Detach(domain.CheckoutKey(worktree))
	}
}
```
Model field (model.go, beside `docs`): `docsDirOf func(worktree string) string // nil = steer.DocsDir(steerDirFor(…)); tests inject a directory of their own`.

Call sites:
- `run.go:107`: after `m = m.initSteerInbox()` → `m = m.attachShownRoots()`; in Run's teardown (where `closeSteerInbox` runs at exit) → `for _, v := range m.views { m.detachDocs(v.path) }; m.detachDocs(m.currentWorktree)`.
- `headless.go` `NewHeadless`: after `m.docs = agentdocs.New()` → `m = m.attachShownRoots()`; `Close`: `m.docs.Close()` (its own store).
- `model.go` `snapshotTargetMsg` case: after `m = m.initSteerInbox()` → `m = m.attachShownRoots()`.
- `worktree_view.go` `loadView`: after `m = m.closeGoneDocs()` → `m.attachDocs(v.path); m = m.syncOverviews()`.
- `worktree_view.go` `pruneViews`: beside `m.openFiles.drop(v.path)` → `m.detachDocs(v.path)`.
- `model.go` `reRoot`: before `m.views = map[…]{}` → `for _, v := range m.views { m.detachDocs(v.path) }; m.detachDocs(m.currentWorktree)`.

- [ ] **Step 4: Run the TUI and steer packages**

Run: `go test ./internal/tui ./internal/steer ./e2e 2>&1 | tail -5`
Expected: PASS. (e2e: the headless now attaches under the sandbox's `XDG_STATE_HOME`; no golden changes expected — read any diff.)

- [ ] **Step 5: Commit**

Message: `tui: attach every shown worktree to its session-docs directory (start, snapshot target, view slots); detach on prune and re-root; headless attaches too`.

---

### Task 5: open-files rows show author and age

**Files:**
- Modify: `internal/tui/sessions_popup.go` (`openFileRowText`, its caller), `internal/tui/open_file_row_test.go` or wherever `openFileRowText` is tested today (grep `openFileRowText(` in `*_test.go`)
- Test: `internal/tui/session_docs_test.go` (append)

**Interfaces:**
- Produces: `func openFileRowText(d *openFile, shown bool, now time.Time) string`.

- [ ] **Step 1: Write the failing test**

```go
// A session overview's row names its author and age; a memory overview's
// row is as before.
func TestASessionOverviewRowShowsAuthorAndAge(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "ov-1", "Tour", "2026-01-01T22:00:00Z", "[a](a.txt:1) [b](a.txt:2)")
	m, _ = m.onAgentDocsChanged()
	d, _ := m.findOverview("ov-1")
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	if got := openFileRowText(d, false, now); got != "○ Tour  overview · 2 anchors · agent · 2h" {
		t.Fatalf("row = %q", got)
	}
	pm := privateDocsModel(t)
	_, pd := addOverviewViaSteer(t, pm, "Mem", "[a](a.txt:1)")
	if got := openFileRowText(pd, true, now); got != "● Mem  overview · 1 anchors" {
		t.Fatalf("memory row = %q", got)
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test ./internal/tui -run TestASessionOverviewRowShowsAuthorAndAge`
Expected: FAIL — too many arguments / row lacks ` · agent · 2h`.

- [ ] **Step 3: Implement**

```go
// openFileRowText is "● path  :12  working tree" — ● on screen, ○ in the
// background; the line is the cursor's. An overview document adds its
// author and age ("· agent · 2h"); now is the drawn clock.
func openFileRowText(d *openFile, shown bool, now time.Time) string {
	…
	if d.ov != nil {
		row := mark + d.title + "  " + i18n.T("overview · %d anchors", len(d.ov.anchors))
		if d.ovAuthor != "" {
			row += " · " + d.ovAuthor
		}
		if !d.ovCreated.IsZero() {
			row += " · " + blameAge(now, d.ovCreated)
		}
		return row
	}
```
Caller in `refresh`: `now := clock.Now()` once before the loop; `openFileRowText(d, m.docShown(d), now)`. Fix the other callers the compiler names (tests pass `time.Time{}` or `clock.Now()`).

- [ ] **Step 4: Run the TUI package**

Run: `go test ./internal/tui 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Commit**

Message: `tui: an overview document's open-files row shows its author and age`.

---

### Task 6: tours are documents in the worker's worktree

**Files:**
- Modify: `internal/tui/agent_tours.go` (`fileTour`, `fileReportTours`)
- Test: `internal/tui/agent_tours_test.go` (read it first: it fakes sessions through `domain.Sessions()`; follow its fixture) or `session_docs_test.go`

**Interfaces:**
- Consumes: Task 4's `attachDocs`; `agentdocs.Store.FileTour` (fixed names `tour-<key>` on an attached root, author `gg`).

- [ ] **Step 1: Write the failing test**

Using `agent_tours_test.go`'s fixture for a session whose `Dir` is `m.currentWorktree` (or a sibling worktree listed in `m.worktrees`), with `m.docsDirOf` returning a temp dir for that worktree:
```go
// A worker's brief and report are written as overview documents in the
// WORKER's worktree directory, author gg, fixed names; a new report replaces
// the report file in place.
func TestToursAreFiledAsDocumentsInTheWorkersWorktree(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	id := <the fixture's spawned session in m.currentWorktree with a brief and one report>
	m, _ = m.fileBriefTour(id)
	m, _ = m.fileReportTours()
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	want := []string{"tour-brief-" + sanitized(id) + ".md", "tour-report-" + sanitized(id) + ".md"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("files = %v, want %v", names, want)
	}
	doc, err := agentdocs.ReadDoc(dir, want[1][:len(want[1])-3], agentdocs.KindOverview)
	if err != nil || doc.Over.Author != "gg" {
		t.Fatalf("report = %+v %v", doc, err)
	}
}
```
(`sanitized` = `agentdocs`'s tour-id rule: every byte outside `[A-Za-z0-9_-]` → `-`; the key is `"brief:" + full id`, so the name is `tour-brief-<full id with ':'→'-'>`. Compute the expected name with a small helper in the test, or expose `agentdocs.TourFileID(key)` if the rule is not reachable — prefer exporting `TourID` is already exported as a method on Store: `m.docs.TourID("brief:"+full)` after filing returns the id; use that.)

- [ ] **Step 2: Run it**

Run: `go test ./internal/tui -run TestToursAreFiledAsDocuments`
Expected: FAIL — the directory is empty (the worker's root was attached by the fixture here, so check instead that WITHOUT `attachedDocsModel` (plain `privateDocsModel` + `docsDirOf`) `fileTour` does not attach; make the test build the model with `privateDocsModel` + `m.docsDirOf = …` and NO prior `Attach` — then it fails because `fileTour` never attaches the worker's root).

- [ ] **Step 3: Implement**

In `agent_tours.go`:
```go
// fileTour composes id's tour of kind and files (or replaces) it — as a
// document in the WORKER's worktree directory (spec §6.5), which is
// attached first when it is a worktree of this repository.
func (m Model) fileTour(id domain.SessionID, kind string) (agentdocs.Overview, error) {
	d, err := domain.AgentTour(domain.FullSessionID(id), kind)
	if err != nil {
		return agentdocs.Overview{}, err
	}
	m.attachTourRoot(d.Dir)
	o, _, err := m.docs.FileTour(d.Key, d.Root, d.Dir, d.Title, d.Text)
	return o, err
}

// attachTourRoot attaches a worker's worktree before its tour is filed: a
// worktree of this repository (its docs directory is under this session
// dir); a worker in another repository keeps today's memory tour.
func (m Model) attachTourRoot(dir string) {
	if model.SamePath(dir, m.currentWorktree) || m.isRepoWorktree(dir) {
		m.attachDocs(dir)
	}
}
```
and in `fileReportTours`, before `m.docs.FileTour(…)`: `m.attachTourRoot(d.Dir)`.

- [ ] **Step 4: Run the TUI package**

Run: `go test ./internal/tui 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Commit**

Message: `tui: agent tours are written as overview documents in the worker's worktree directory (author gg, fixed names)`.

---

### Task 7: headless hooks and the e2e scenario

**Files:**
- Modify: `internal/tui/headless.go` (`DocsDir`, `SyncDocs`), `e2e/scenario.go` (`TUIStep.Doc`, `TUIStep.RemoveDoc`, validation), `e2e/tui.go` (run the new step kinds)
- Create: `e2e/scenarios/tui_session_docs.toml`, `e2e/scenarios/tui_session_docs.screens/*.txt` (golden, via `-update`, then READ)
- Test: `e2e/scenario_test.go` (validation of the new fields), the scenario itself

**Interfaces:**
- Produces: `func (h *Headless) DocsDir() string` (the current worktree's attached docs directory), `func (h *Headless) SyncDocs() error` (reload the current root and deliver `agentDocsChangedMsg{}`); scenario fields `doc = { name = "<file name>", content = "<whole file>" }` and `remove_doc = "<file name>"` on a `[[tui.step]]`.

- [ ] **Step 1: Write the failing scenario and the validation test**

`e2e/scenarios/tui_session_docs.toml`:
```toml
name = "tui: documents an agent writes by hand into the session-docs directory show in the TUI (spec 2026-10-18-gg-session-docs)"

# a.txt is committed; an agent writes an overview and a note on a.txt:2
# straight into the docs directory (no gg verb). The open-files window lists
# the overview (author agent, age 2h under the frozen clock); opening a.txt
# shows the note's box; D deletes the overview after a confirm; the agent
# removing the note file takes the box away.
[input]
steps = [
  { write = "a.txt", content = "one\ntwo\nthree\nfour\nfive\n" },
  { commit = "add a" },
]

[tui]
size = "160x40"

[[tui.step]]
name = "overview-listed"
doc = { name = "20260101T220000-ab12.md", content = "+++\nformat = 1\ntitle = \"Tour of a\"\nauthor = \"agent\"\ncreated = 2026-01-01T22:00:00Z\n+++\n# Tour\n\nSee [two](a.txt:2).\n" }
keys = ["ctrl+\\"]
screen_contains = ["Tour of a", "overview · 1 anchors · agent · 2h"]

[[tui.step]]
name = "note-box"
doc = { name = "20260101T220100-cd34.toml", content = "format = 1\npath = \"a.txt\"\nstart = 2\nend = 2\nsummary = \"why two?\"\nauthor = \"agent\"\nlines = [\"two\"]\n" }
keys = ["esc", "right", "enter"]
wait = true
screen_contains = ["why two?"]

[[tui.step]]
name = "delete-asks"
keys = ["esc", "ctrl+\\", "D"]
screen_contains = ["Delete this overview?", "Tour of a"]

[[tui.step]]
name = "deleted"
keys = ["left", "enter"]
screen_excludes = ["Tour of a"]

[[tui.step]]
name = "note-gone"
remove_doc = "20260101T220100-cd34.toml"
keys = ["esc", "enter"]
wait = true
screen_excludes = ["why two?"]

[expect]
branch = "main"
```
(Key names: check `keyMsgFor` for the spelling of ctrl+backslash and the decision modal's option keys — the modal's default is Cancel; `left` moves to Delete, `enter` picks; confirm against an existing scenario that answers a `Delete`/`Cancel` modal, e.g. grep `Delete this` in `e2e/scenarios/*.toml`.) The files-view steps (`right`, `enter` to open a.txt) copy `tui_note_link.toml`'s pattern; adjust once the first run shows the real screen.

`e2e/scenario_test.go`: a validation test that a step with `doc` whose `name` is not a document name (`DocName` false) is refused, and that `doc` and `remove_doc` on one step are refused.

- [ ] **Step 2: Run them**

Run: `go test ./e2e -run 'TestScenario.*/tui_session_docs|TestTUIStepDoc' 2>&1 | tail -20`
Expected: FAIL — unknown field `doc` (TOML strict decode) / validation test cannot compile.

- [ ] **Step 3: Implement**

`headless.go`:
```go
// DocsDir is the current worktree's session-docs directory (the harness
// writes documents there by hand; "" when nothing is attached).
func (h *Headless) DocsDir() string { return h.m.docs.Attached(domain.CheckoutKey(h.m.currentWorktree)) }

// SyncDocs has the store read the directory again and delivers the change to
// the model — quiet mode has no waiter on the store's signal.
func (h *Headless) SyncDocs() error {
	h.m.docs.Reload(domain.CheckoutKey(h.m.currentWorktree))
	return h.deliver(agentDocsChangedMsg{})
}
```
`scenario.go`: `Doc *TUIDoc \`toml:"doc"\``, `RemoveDoc string \`toml:"remove_doc"\``, `type TUIDoc struct{ Name, Content string }`; validation: a step may have `doc` or `remove_doc` (not both); `Name` must satisfy `agentdocs.DocName`; a step with only `doc`/`remove_doc` needs no keys (relax the "needs keys or wait" rule for it).
`tui.go` loop, before the keys:
```go
		if d := st.Doc; d != nil {
			if err := os.WriteFile(filepath.Join(hd.DocsDir(), d.Name), []byte(d.Content), 0o644); err != nil { t.Fatalf(…) }
			if err := hd.SyncDocs(); err != nil { t.Fatalf(…) }
			keys = append(keys, "<doc "+d.Name+">")
		}
		if n := st.RemoveDoc; n != "" {
			if err := os.Remove(filepath.Join(hd.DocsDir(), n)); err != nil { t.Fatalf(…) }
			if err := hd.SyncDocs(); err != nil { t.Fatalf(…) }
			keys = append(keys, "<remove_doc "+n+">")
		}
```
(A write by `os.WriteFile` is not temp+rename, but the store reloads only on `SyncDocs`, after the write completed, so the partial-file window does not matter here; say so in a comment.)

- [ ] **Step 4: Generate and READ the goldens, then run e2e**

Run: `go test ./e2e -run 'TestScenario.*/tui_session_docs' -update 2>&1 | tail -5` then `cat e2e/scenarios/tui_session_docs.screens/*.txt` — read every screen: the row text, the box, the modal, the empty list. Fix the scenario's keys if a screen is not what the step claims. Then `go test ./e2e 2>&1 | tail -5`.
Expected: PASS, five goldens that show what their names say.

- [ ] **Step 5: Commit**

Message: `e2e: tui_session_docs — documents written by hand show in the TUI; headless DocsDir/SyncDocs; doc / remove_doc tui steps`.

---

### Task 8: docs, changelog, help, memory

**Files:**
- Modify: `CHANGELOG.md` (top entry), `docs/CLAUDE-details.md` (the "Session docs" section: a "Part 2 — the TUI reader" paragraph: attach points, close vs delete, D key, tours, headless hooks), `CLAUDE.md` `tui` row ONLY if a one-line fact changed (likely none — the `agentdocs` row already says TUI imports it), `internal/tui/help.go` / `popup_help.go` (the ctrl+\ window's key list, if one is documented there: add `D delete`), `README.md` (the open-files window keys, if listed)
- Memory: `/home/homeend/.claude/projects/-work-gigagit/memory/gg-session-docs-feature.md` (plan 2 state) — after the merge, not in the worktree

- [ ] **Step 1: Write the docs**

CHANGELOG (top): "Session docs, part 2: the TUI reads the session-docs directory — every shown worktree is attached; the open-files window (ctrl+\) lists the directory's overviews with author and age; close is local, `D` (and the `.` menu's Delete overview / Delete note / Delete all notes on this file) unlinks after a confirm; overview_rm removes the document; agent tours are files in the worker's worktree." CLAUDE-details: the paragraph above plus the test hooks (`docsDirOf`, `Headless.DocsDir/SyncDocs`, the `doc`/`remove_doc` steps). Help: grep `"\[x\] close"` and `ctrl+\\` in `help.go`/`popup_help.go`; add the D key where the window's keys are listed.

- [ ] **Step 2: Gates**

Run: `go vet ./... && gofmt -l internal e2e` → empty; `go test ./internal/tui ./internal/i18n ./e2e 2>&1 | tail -3` → PASS.

- [ ] **Step 3: Commit**

Message: `docs: session docs part 2 — changelog, details, help`.

---

### Final: review, fix pass, race gate

- Build the review package (`review-package`), dispatch ONE read-only review subagent on opus with this plan's Review Focus and the ledger's rulings; fix Critical/Important with RED→GREEN tests; ledger minors.
- `./test.sh race` must print `all green` (start it early; check the log for the words, not the exit code).
- Ask before merging; then `gg merge -F <msgfile> --into main feat/session-docs-tui`, `./build.sh install`, remove the worktree and branch, update the memory note.
