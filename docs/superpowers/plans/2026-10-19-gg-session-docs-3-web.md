# gg session docs — plan 3 of 4: gg web reads the directory

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task, inline (this repo forbids implementer subagents; one read-only review subagent at the end). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** gg web is a session-docs READER like the TUI: a hosted page reads the TUI's attached store, a standalone `gg web` attaches its own store to the served worktree's docs directory (and re-attaches on a re-root), the page's open-files list shows overview documents with author, age and anchor count, closing is local, and DELETE is a confirmed row action (overview row, note box, all notes on a file) that unlinks the file.

**Architecture:** Plan 2 left `internal/agentdocs` complete (attach, reconcile, `…In(root,id)` variants) and the TUI wired. This plan wires `internal/web`: every store call on a document names the served root (`s.docsRoot`), every id gate accepts a document id (`agentdocs.ValidID`, which still matches the memory ids `f<n>`/`t<n>`), the standalone server attaches at `Host.Start` / `adoptService` and closes its store at `Server.Close` — never when hosted, where the store is the TUI's. The open-files `close` op stops touching the store (close is local; a pinned entry backgrounds); a new `POST /api/session-docs` carries the three deletes behind the page's usual local confirm. The page's switcher rows, note boxes and overview footers gain the delete affordances; the CLI's id checks on `gg session overview|note` widen to document ids so the e2e harness can drive the web with one (plan 4 reshapes those verbs).

**Tech Stack:** Go 1.26, `net/http` + `httptest`, `internal/agentdocs`, `internal/steer`, `internal/config`, the page's ES modules (`openfiles.js`, `viewer.js`, `ops.js`'s `showLocalConfirm`, `layers.js`'s `showCtxMenu`), node for the pure-JS tests (`runPureJS`), the e2e harness (`[input] web = true`).

**Spec:** `docs/superpowers/specs/2026-10-18-gg-session-docs-design.md` — §4.2 (reconcile), §4.4 (what a reader shows; close vs delete), §6.1 (In variants), §6.4 (TUI and gg web), §12 (testing). Plans 1 and 2: `docs/superpowers/plans/2026-10-18-gg-session-docs-1-core.md`, `2026-10-19-gg-session-docs-2-tui.md`.

## Global Constraints

- Ids are unique PER WORKTREE: every store call on a document the web serves goes through the `…In(root, id)` form with `s.docsRoot(ctx)` (spec §6.1). An overview document's id is its file name; `f<n>` stays for open FILES (`NextFileSeq`).
- Lifetime: a document lives until the user or the writer deletes it. The web never unlinks on its own: not on close (esc or x), not on a re-root, not at shutdown (spec §4.4 "A reader deletes only what the user asked for").
- Close is LOCAL. DELETE is a separate action behind the page's usual local confirm (`showLocalConfirm`, options `cancel` / `delete`, cancel first), on an overview row, a note box, and "all notes on this file"; a refused unlink says so (HTTP 409, the TUI's words).
- Attach / detach / `docs.Close()` happen ONLY on a standalone server (`!s.hosted`): a hosted page's store is `agentdocs.Shared()`, which the TUI attaches and closes. The docs directory is `steer.DocsDir(config.SessionSteerDir(commonDir, top))`, NOT `resolveSteerDir` (that returns "" when `[ui] steering` is off; the TUI attaches regardless). Tests set the `Server.docsDirOf` seam; `New()` attaches nothing, so no test touches the real state home.
- Page strings are English (no i18n gate on the web). Steer reply prose stays the TUI's: `removed <id>` / `could not remove <id>`.
- `./test.sh race` must print `all green` before merge. Commit with `gg add <files>` then `git commit -F <msgfile>`; never `git add -A`. Worktree: `/work/gigagit/.claude/worktrees/session-docs-web` (branch `feat/session-docs-web`).
- Unattached roots keep today's memory behaviour; existing tests built on `New(domain.Open(…))` without `Attach` stay green unchanged except where this plan changes a behaviour on purpose (listed in Task 4).

## Review Focus

1. A session document id through every web gate — `GET /api/overview?id=`, `GET /api/overview-export?id=`, `POST /api/session-docs`, `file_focus` on the steer wire — must be accepted; a memory id must still be; an id with a slash or a dot must still be refused — Task 2 pins each endpoint with a document id written by hand.
2. The hosted page must never detach, re-attach or close the shared store: `NewHost(…, hosted=true)` → `Start` → `Reroot` → `Close` leaves a root the TUI attached still `Attached` — Task 3.
3. A re-root of a standalone server to another worktree: the old root's documents leave the page, the new root's appear, and the old directory's files are untouched — Task 3.
4. `x` (close everywhere) on a noted file or an overview document: the row stays listed (backgrounds), nothing is unlinked, no tab is told it "closed"; `esc` on a plain file still closes it — Task 4.
5. Delete from the split view (an overview beside a file): the split's own overview is the one deleted, the viewer returns to the file and says the overview was deleted — Task 5 (the plan 2 review caught the split missing a delete row).

---

### Task 1: agentdocs — root-scoped stamp and note text

**Files:**
- Modify: `internal/agentdocs/overviews.go` (beside `OverviewStamp`), `internal/agentdocs/notes.go` (beside `NoteText`)
- Test: `internal/agentdocs/stamp_in_test.go` (new)

**Interfaces:**
- Produces: `func (s *Store) OverviewStampIn(root, id string) string`, `func (s *Store) NoteTextIn(root, id string) []string` — root-scoped twins; the id-only forms become `…In("", id)` wrappers (an ambiguous id resolves nothing, as every other id-only form does).

- [ ] **Step 1: Write the failing test**

```go
package agentdocs

import (
	"os"
	"path/filepath"
	"testing"
)

// Two attached worktrees hold the same ids: the root-scoped stamp and note
// text read exactly their worktree's document; the id-only forms find
// nobody (ambiguous).
func TestStampAndNoteTextAreRootScoped(t *testing.T) {
	t.Parallel()
	s := New()
	defer s.Close()
	wtA, wtB := t.TempDir(), t.TempDir()
	dirA, dirB := filepath.Join(t.TempDir(), "docs"), filepath.Join(t.TempDir(), "docs")
	for i, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		writeRaw(t, d, "same.md", "+++\ntitle = \"T"+string(rune('A'+i))+"\"\n+++\nbody\n")
		writeRaw(t, d, "n.toml", "format = 1\npath = \"a.go\"\nstart = 1\nend = 1\nsummary = \"s\"\nlines = [\"line "+string(rune('A'+i))+"\"]\n")
	}
	if err := s.Attach("A", wtA, dirA); err != nil {
		t.Fatal(err)
	}
	if err := s.Attach("B", wtB, dirB); err != nil {
		t.Fatal(err)
	}
	if s.OverviewStampIn("A", "same") == "" || s.OverviewStampIn("B", "same") == "" {
		t.Fatal("a held overview has a stamp")
	}
	if s.OverviewStampIn("A", "same") == s.OverviewStampIn("B", "same") {
		t.Fatal("two different documents must not share a stamp")
	}
	if s.OverviewStamp("same") != "" {
		t.Fatal("an ambiguous id must stamp nothing")
	}
	if got := s.NoteTextIn("B", "n"); len(got) != 1 || got[0] != "line B" {
		t.Fatalf("NoteTextIn(B) = %q", got)
	}
	if got := s.NoteText("n"); got != nil {
		t.Fatalf("an ambiguous id must read nothing, got %q", got)
	}
	if got := s.NoteTextIn("A", "zz"); got != nil {
		t.Fatalf("unknown id reads nothing, got %q", got)
	}
}
```

`writeRaw` exists in the package's tests (plan 1); if its name differs, use the helper `anchors_in_test.go` uses.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/session-docs-web && go test ./internal/agentdocs -run TestStampAndNoteTextAreRootScoped`
Expected: FAIL to compile — `s.OverviewStampIn undefined`, `s.NoteTextIn undefined`.

- [ ] **Step 3: Implement**

In `overviews.go`, replace `OverviewStamp`:

```go
// OverviewStamp is OverviewStampIn("", id): the stamp of the one root
// holding id, "" when none or several do.
func (s *Store) OverviewStamp(id string) string { return s.OverviewStampIn("", id) }

// OverviewStampIn is a value that changes whenever overview id of root
// changes (text, anchors, a check's flags): a tab holding a copy compares
// it. "" when the overview is not there.
func (s *Store) OverviewStampIn(root, id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.ovResolveLocked(root, id)
	if err != nil {
		return ""
	}
	<the body OverviewStamp has today, over e>
}
```

Keep whatever the current body computes (read it first; move it verbatim under the resolve). In `notes.go`, the same for `NoteText`:

```go
// NoteText is NoteTextIn("", id).
func (s *Store) NoteText(id string) []string { return s.NoteTextIn("", id) }

// NoteTextIn is the lines note id of root sits on now (its anchor copy),
// nil when there is no such note.
func (s *Store) NoteTextIn(root, id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n *note
	if root == "" {
		_, n = s.findLocked(id)
	} else {
		_, n = s.findInLocked(root, id)
	}
	if n == nil {
		return nil
	}
	<the body NoteText has today, over n>
}
```

`findLocked(id)` already returns nothing for an id in several roots (plan 1); confirm by reading it before relying on it.

- [ ] **Step 4: Run the package tests**

Run: `go test ./internal/agentdocs`
Expected: PASS (all, including the new test).

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/session-docs-web
gg add internal/agentdocs/overviews.go internal/agentdocs/notes.go internal/agentdocs/stamp_in_test.go
git commit -F <msgfile>   # "agentdocs: OverviewStampIn and NoteTextIn — the root-scoped twins the web needs"
```

---

### Task 2: web — document ids pass every gate; every store call names the served root

**Files:**
- Modify: `internal/web/overview_http.go` (`fileIDRE`, `handleOverview`), `internal/web/overview_export_http.go:38`, `internal/web/file_notes.go`, `internal/web/steer.go:126` (`openFileID`), `internal/web/steer_notes.go` (`noteIDRE`, `steerNoteShow`, `steerNoteRm`), `internal/web/steer_overviews.go`, `internal/web/agentdocs_follow.go` (`followDocs` stamps, `overviewKey`), `internal/web/agenttour_http.go:56`
- Test: `internal/web/session_docs_web_test.go` (new), `internal/web/agentdocs_overview_web_test.go` (`TestSteerOverviewVerbsOnTheWeb`: `"closed "` → `"removed "`)

**Interfaces:**
- Consumes: `OverviewStampIn`, `NoteTextIn` (Task 1); `OverviewIn`, `FindNoteIn`, `SetOverviewIn`, `RemoveOverviewIn`, `RemoveNoteIn`, `CheckAnchorsIn` (plan 1/2).
- Produces: the test fixture `attachedSrv(t) (s *Server, root, dir string)` — `noteSrv` plus `s.docs.Attach(root, s.service().Root(), dir)` with `t.Cleanup(s.docs.Close)`; `writeDoc(t, dir, name, content string)` (raw write, then `s.docs.Reload(root)` is the caller's); every later task's HTTP test uses them.

- [ ] **Step 1: Write the failing tests**

`internal/web/session_docs_web_test.go`:

```go
package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// attachedSrv is noteSrv with the served worktree attached to a temp docs
// directory: what a standalone gg web has after Start, what a hosted page
// has from the TUI.
func attachedSrv(t *testing.T) (s *Server, root, dir string) {
	t.Helper()
	s, root = noteSrv(t)
	dir = filepath.Join(t.TempDir(), "docs")
	if err := s.docs.Attach(root, s.service().Root(), dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.docs.Close)
	return s, root, dir
}

// writeDoc writes a document by hand — no gg verb — the way an agent does.
func writeDoc(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	tourDoc = "+++\nformat = 1\ntitle = \"Tour of f\"\nauthor = \"agent\"\ncreated = 2026-01-01T22:00:00Z\n+++\n# Tour\n\nSee [one](f.txt:1).\n"
	noteDoc = "format = 1\npath = \"f.txt\"\nstart = 1\nend = 1\nsummary = \"why one?\"\nauthor = \"agent\"\nlines = [\"content 1\"]\n"
)

// Review Focus 1: a document id (its file name) passes every gate a memory
// id passed; an id that is not a document id is still refused.
func TestDocumentIDsPassTheWebGates(t *testing.T) {
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	writeDoc(t, dir, "20260101T220100-cd34.toml", noteDoc)
	s.docs.Reload(root)
	s.followDocs()
	ts := serve(t, s)
	var ov struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Anchors []any  `json:"anchors"`
	}
	if code := getJSON(t, ts, "/api/overview?id=20260101T220000-ab12", &ov); code != http.StatusOK || ov.Title != "Tour of f" || len(ov.Anchors) != 1 {
		t.Fatalf("overview: %d %+v", code, ov)
	}
	if code := getJSON(t, ts, "/api/overview?id=../etc", &ov); code != http.StatusBadRequest {
		t.Fatalf("a path is not an id: %d", code)
	}
	if code := getJSON(t, ts, "/api/overview?id=a.b", &ov); code != http.StatusBadRequest {
		t.Fatalf("a dot is not an id: %d", code)
	}
	res, err := http.Get(ts.URL + "/api/overview-export?id=20260101T220000-ab12")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("export: %d", res.StatusCode)
	}
	_, rep := steerAsk(t, s, `{"id":"1","cmd":"file_focus","file_id":"20260101T220000-ab12"}`)
	if !rep.OK || !strings.HasPrefix(rep.Detail, `focused overview 20260101T220000-ab12 "Tour of f"`) {
		t.Fatalf("file_focus: %+v", rep)
	}
	if _, rep = steerAsk(t, s, `{"id":"2","cmd":"overview_show","file_id":"20260101T220000-ab12"}`); !rep.OK || rep.Overviews[0].Title != "Tour of f" {
		t.Fatalf("overview_show: %+v", rep)
	}
	if _, rep = steerAsk(t, s, `{"id":"3","cmd":"note_show","note_id":"20260101T220100-cd34"}`); !rep.OK || len(rep.Notes) != 1 || rep.Notes[0].Summary != "why one?" {
		t.Fatalf("note_show: %+v", rep)
	}
}

// The follow pass lists a document written by hand, keyed by its id, with
// a stamp per id; a hand-removed one leaves the list.
func TestAHandWrittenOverviewIsListedAndAHandRemovedOneLeaves(t *testing.T) {
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	s.docs.Reload(root)
	s.followDocs()
	wt := s.service().Root()
	if _, ok := s.ofs.resolve(wt, "20260101T220000-ab12", ""); !ok {
		t.Fatal("the document is not in the page's list")
	}
	if k, _, _ := s.ofs.pinnedEntry(wt, "20260101T220000-ab12"); k.Path != "overview-20260101T220000-ab12.md" {
		t.Fatalf("display name = %q", k.Path)
	}
	if err := os.Remove(filepath.Join(dir, "20260101T220000-ab12.md")); err != nil {
		t.Fatal(err)
	}
	s.docs.Reload(root)
	s.followDocs()
	if _, ok := s.ofs.resolve(wt, "20260101T220000-ab12", ""); ok {
		t.Fatal("a removed document stays listed")
	}
}

// overview_rm on the web deletes the document (the writer takes it back)
// and says so in the TUI's words.
func TestSteerOverviewRmDeletesTheDocument(t *testing.T) {
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	s.docs.Reload(root)
	s.followDocs()
	_, rep := steerAsk(t, s, `{"id":"1","cmd":"overview_rm","file_id":"20260101T220000-ab12"}`)
	if !rep.OK || rep.Detail != "removed 20260101T220000-ab12" {
		t.Fatalf("rm: %+v", rep)
	}
	if _, err := os.Stat(filepath.Join(dir, "20260101T220000-ab12.md")); !os.IsNotExist(err) {
		t.Fatal("the file is still there")
	}
	if _, ok := s.ofs.resolve(s.service().Root(), "20260101T220000-ab12", ""); ok {
		t.Fatal("rm left the entry listed")
	}
	_ = context.Background
}
```

Drop the `_ = context.Background` line and the import if nothing else in the file needs `context`. In `agentdocs_overview_web_test.go`, `TestSteerOverviewVerbsOnTheWeb`: the `rm` assertion `rep.Detail != "closed "+id` becomes `rep.Detail != "removed "+id`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/web -run 'TestDocumentIDsPassTheWebGates|TestAHandWrittenOverviewIsListedAndAHandRemovedOneLeaves|TestSteerOverviewRmDeletesTheDocument|TestSteerOverviewVerbsOnTheWeb'`
Expected: FAIL — `/api/overview` answers 400 for the document id ("want an overview id"); `file_focus` is refused (`unknown file id`); display name is `overview-<seq>.md`; `rm` says `closed`.

- [ ] **Step 3: Implement**

1. `overview_http.go`: delete `fileIDRE`; `handleOverview` checks `agentdocs.ValidID(id)`; `s.docs.Overview(id)` → `s.docs.OverviewIn(root, id)` (both reads); `s.docs.CheckAnchors(id)` → `CheckAnchorsIn(root, id)`; `s.docs.OverviewStamp(id)` → `OverviewStampIn(root, id)`; `s.docs.FindNote(a.Note)` → `FindNoteIn(root, a.Note)` (the `n.Root == root` check can go).
2. `overview_export_http.go:38`: `fileIDRE.MatchString(id)` → `agentdocs.ValidID(id)` (import `internal/agentdocs` if the file lacks it).
3. `steer.go`: `openFileID` → delete the var; the check becomes `if c.FileID != "" && !agentdocs.ValidID(c.FileID)`.
4. `steer_notes.go`: delete `noteIDRE` (move the id check in `file_notes.go` to `agentdocs.ValidID`; Task 4 reshapes that handler anyway); `steerNoteShow`: `s.docs.FindNoteIn(root, c.NoteID)`, `s.docs.NoteTextIn(root, n.ID)`; `steerNoteRm`: `FindNoteIn(root, …)` + `RemoveNoteIn(root, c.NoteID)`.
5. `steer_overviews.go`: `servedOverview` → `s.docs.OverviewIn(s.docsRoot(ctx), id)`; `overview_set` → `SetOverviewIn(root, c.FileID, …)`, `CheckAnchorsIn(root, o.ID)`, `OverviewIn`; `overview_add` → `CheckAnchorsIn(root, o.ID)`, `OverviewIn(root, o.ID)`; `overview_rm`:
   ```go
   // overview_rm: the writer takes its document back — the file goes.
   root := s.docsRoot(ctx)
   if _, ok := s.servedOverview(ctx, c.FileID); !ok {
   	return steerFail(c, "no overview "+c.FileID)
   }
   if !s.docs.RemoveOverviewIn(root, c.FileID) {
   	return steerFail(c, "could not remove "+c.FileID)
   }
   s.followDocs()
   return steerOK(c, "removed "+c.FileID)
   ```
6. `agentdocs_follow.go`: `stamps[o.ID] = s.docs.OverviewStampIn(root, o.ID)`; `overviewKey(o)` → `ofKey{Src: "overview", Path: "overview-" + o.ID + ".md"}` (the TUI's display name; drop the `strconv` import if unused).
7. `agenttour_http.go:56`: `s.docs.CheckAnchorsIn(o.Root, o.ID)` (the tour may live in another worktree).
8. `file_notes.go`: `FindNoteIn(root, q.ID)` / `RemoveNoteIn(root, q.ID)` with `root := s.docsRoot(r.Context())`, id check `agentdocs.ValidID(q.ID)`.

`grep -n "s\.docs\.\(Overview\|FindNote\|SetOverview\|RemoveOverview\|RemoveNote\|CheckAnchors\|OverviewStamp\|NoteText\)(" internal/web/*.go | grep -v _test | grep -v "In("` must print nothing when done.

- [ ] **Step 4: Run the web tests**

Run: `go test ./internal/web 2>&1 | tail -5`
Expected: PASS. (`TestXOnAnOverviewEntryRemovesItFromTheStore` and `TestCloseOfAPinnedEntryBackgroundsAndEverywhereClears` still pass here; Task 4 rewrites them.)

- [ ] **Step 5: Commit**

```bash
gg add internal/web/overview_http.go internal/web/overview_export_http.go internal/web/file_notes.go internal/web/steer.go internal/web/steer_notes.go internal/web/steer_overviews.go internal/web/agentdocs_follow.go internal/web/agenttour_http.go internal/web/session_docs_web_test.go internal/web/agentdocs_overview_web_test.go
git commit -F <msgfile>   # "web: document ids pass every gate; every store call names the served root; overview_rm deletes and says removed"
```

---

### Task 3: standalone gg web attaches its own store; a hosted page never touches the shared one

**Files:**
- Create: `internal/web/session_docs.go`
- Modify: `internal/web/server.go` (field `docsDirOf`), `internal/web/host.go` (`Start`), `internal/web/reroot.go` (`adoptService`), `internal/web/live.go` (`Server.Close`)
- Test: `internal/web/session_docs_attach_test.go` (new)

**Interfaces:**
- Produces: `func (s *Server) attachDocs(ctx context.Context)` (standalone only; a hosted server returns at once), `func (s *Server) detachDocs()` (detaches the root `rootc` holds), field `docsDirOf func(commonDir, top string) string` (nil = `steer.DocsDir(config.SessionSteerDir(commonDir, top))`; tests set it).

- [ ] **Step 1: Write the failing tests**

```go
package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
)

// A standalone server attaches the served worktree at Start and lists what
// its directory already holds; Close detaches.
func TestStandaloneAttachesAtStartAndClosesItsStore(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	dir := filepath.Join(t.TempDir(), "docs")
	s.docsDirOf = func(_, _ string) string { return dir }
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	h := newHostOver(s, false)
	if _, err := h.Start(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	root := s.docsRoot(context.Background())
	if s.docs.Attached(root) != dir {
		t.Fatalf("Attached(%s) = %q, want %q", root, s.docs.Attached(root), dir)
	}
	if _, ok := s.ofs.resolve(s.service().Root(), "20260101T220000-ab12", ""); !ok {
		t.Fatal("the directory's document is not listed after Start")
	}
	h.Close()
	if s.docs.Attached(root) != "" {
		t.Fatal("Close left the root attached")
	}
	if _, err := os.Stat(filepath.Join(dir, "20260101T220000-ab12.md")); err != nil {
		t.Fatal("Close unlinked a document")
	}
}

// Review Focus 3: a re-root detaches the old worktree and attaches the new
// one; the old directory's files are untouched.
func TestRerootDetachesTheOldRootAndAttachesTheNew(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	dirs := map[string]string{}
	s.docsDirOf = func(_, top string) string {
		d, ok := dirs[domain.CheckoutKey(top)]
		if !ok {
			d = filepath.Join(t.TempDir(), "docs")
			dirs[domain.CheckoutKey(top)] = d
		}
		return d
	}
	s.attachDocs(context.Background())
	rootA := s.docsRoot(context.Background())
	dirA := s.docs.Attached(rootA)
	if dirA == "" {
		t.Fatal("not attached")
	}
	writeDoc(t, dirA, "20260101T220000-ab12.md", tourDoc)
	s.docs.Reload(rootA)
	s.followDocs()
	other := domain.Open(newRepoDir(t, 2))
	if err := s.adoptService(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	rootB := s.docsRoot(context.Background())
	if rootB == rootA {
		t.Fatal("the test needs two worktrees")
	}
	if s.docs.Attached(rootA) != "" {
		t.Fatal("the old root is still attached")
	}
	if s.docs.Attached(rootB) == "" {
		t.Fatal("the new root is not attached")
	}
	if _, ok := s.ofs.resolve(other.Root(), "20260101T220000-ab12", ""); ok {
		t.Fatal("the old worktree's document is listed under the new one")
	}
	if _, err := os.Stat(filepath.Join(dirA, "20260101T220000-ab12.md")); err != nil {
		t.Fatal("the re-root unlinked a document")
	}
}

// Review Focus 2: a hosted page shares the TUI's store and never attaches,
// detaches or closes it — Start, Reroot and Close leave a root the TUI
// attached as it was.
func TestHostedPageNeverTouchesTheSharedStore(t *testing.T) {
	// not parallel: agentdocs.Shared() is process-global
	svc := domain.Open(newRepoDir(t, 1))
	root := domain.CheckoutKey(mustTop(t, svc))
	dir := filepath.Join(t.TempDir(), "docs")
	if err := agentdocs.Shared().Attach(root, svc.Root(), dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { agentdocs.Shared().Detach(root) })
	h := NewHost(svc, nil, true)
	h.srv.steerDir = t.TempDir()
	h.srv.docsDirOf = func(_, _ string) string { t.Fatal("a hosted page resolved a docs dir"); return "" }
	if _, err := h.Start(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if err := h.Reroot(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	h.Close()
	if agentdocs.Shared().Attached(root) != dir {
		t.Fatal("the hosted page detached the TUI's root")
	}
}
```

`mustTop(t, svc)` = `svc.TopLevel(context.Background())` with a fatal on error; add it to this file if no test helper exists (grep `func mustTop\|TopLevel(context` in `internal/web/*_test.go` first). If `newSteerServer` does not set `steerDir` to a temp dir for a hosted host, set it as shown so `Start`'s presence check does not read the real state home (read `Host.Start`'s `refuseForeign` branch: it resolves the steer dir through `resolveSteerDir`, which reads the config — a temp repo has steering on by default; if `Start` writes a presence into the real state home for a temp repo, set `h.srv.docsDirOf` AND confirm `resolveSteerDir` honours `XDG_STATE_HOME`; `t.Setenv` is incompatible with `t.Parallel`, which this test does not call).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/web -run 'TestStandaloneAttachesAtStartAndClosesItsStore|TestRerootDetachesTheOldRootAndAttachesTheNew|TestHostedPageNeverTouchesTheSharedStore'`
Expected: FAIL to compile — `s.docsDirOf undefined`, `s.attachDocs undefined`.

- [ ] **Step 3: Implement**

`internal/web/session_docs.go`:

```go
package web

// gg web as a session-docs READER (spec 2026-10-18-gg-session-docs §4,
// §6.4). A standalone server attaches the served worktree's root to that
// worktree's docs directory — under the same session dir the steer inbox
// lives in — at Start and on every re-root, and closes its store at Close.
// A HOSTED page's store is agentdocs.Shared(), which the terminal attaches
// and closes: the page never attaches, detaches or closes anything.

import (
	"context"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/steer"
)

// docsDirFor is the docs directory of the worktree at top whose git common
// dir is commonDir; "" when the session dir cannot be resolved (then
// nothing attaches).
func (s *Server) docsDirFor(commonDir, top string) string {
	if s.docsDirOf != nil {
		return s.docsDirOf(commonDir, top) // tests
	}
	return steer.DocsDir(config.SessionSteerDir(commonDir, top))
}

// attachDocs binds the served worktree's root to its docs directory. Only a
// standalone server; only on a resolved toplevel (docsDirs's fallback key
// would file the directory under the wrong root). A directory that cannot
// be made or read attaches nothing and says nothing: the page runs without
// session rows.
func (s *Server) attachDocs(ctx context.Context) {
	if s.hosted {
		return
	}
	svc := s.service()
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return
	}
	cd, err := svc.GitCommonDir(ctx)
	if err != nil {
		return
	}
	root, _ := s.docsDirs(ctx)
	if dir := s.docsDirFor(cd, top); dir != "" {
		_ = s.docs.Attach(root, top, dir)
	}
}

// detachDocs forgets the served worktree's documents (a re-root). Only a
// standalone server.
func (s *Server) detachDocs() {
	if s.hosted {
		return
	}
	s.rootc.mu.Lock()
	root := s.rootc.root
	s.rootc.mu.Unlock()
	if root != "" {
		s.docs.Detach(root)
	}
}
```

`server.go`: add the field beside `docs`:

```go
	// docsDirOf resolves a worktree's session-docs directory (session_docs.go);
	// nil = the real one under the state home. Tests set it.
	docsDirOf func(commonDir, top string) string
```

`host.go` `Start`: before `h.srv.startDocsFollow()`, add `h.srv.attachDocs(ctx)` (so the first follow pass lists the directory). `reroot.go` `adoptService`: call `s.detachDocs()` BEFORE `s.svc.Store(svc)` (inside the `opMu` critical section is fine: Detach takes the store's lock only) and `s.attachDocs(ctx)` right before the existing `s.followDocs()`. `live.go` `Server.Close`: after `s.stopOpenFilesWatch()`, add

```go
	if !s.hosted {
		s.docs.Close() // a standalone server's own store; a hosted page's is the terminal's
	}
```

`docsDirs` caches `root` for the current `svc` — `detachDocs` reads the cached root of the OLD service, so it must run before `s.svc.Store(svc)` moves the cache on. Read `docsRootCache` once more before writing this.

- [ ] **Step 4: Run the web tests**

Run: `go test ./internal/web 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/web/session_docs.go internal/web/server.go internal/web/host.go internal/web/reroot.go internal/web/live.go internal/web/session_docs_attach_test.go
git commit -F <msgfile>   # "web: a standalone gg web attaches its own store to the served worktree's docs directory (Start, re-root, Close); a hosted page never touches the shared one"
```

---

### Task 4: close is local; `POST /api/session-docs` deletes; rows carry author, age, anchors

**Files:**
- Modify: `internal/web/openfiles_http.go` (`case "close"`), `internal/web/file_notes.go` (becomes the session-docs endpoint), `internal/web/agentdocs_follow.go` (`ofList`), `internal/steer/steer.go` (`OpenFile`)
- Test: `internal/web/session_docs_delete_test.go` (new); rewrite `TestXOnAnOverviewEntryRemovesItFromTheStore` → `TestXOnAnOverviewEntryBackgroundsIt`, `TestCloseOfAPinnedEntryBackgroundsAndEverywhereClears` → `TestCloseOfAPinnedEntryBackgroundsEscAndX`, `TestDismissEndpoint` → `TestSessionDocsDeleteNote` (in `agentdocs_web_test.go`); keep `TestXOnAnAttachedOverviewKeepsTheDocument` as the regression.

**Interfaces:**
- Produces: `POST /api/session-docs` JSON `{op: "delete-overview", id}` | `{op: "delete-note", id}` | `{op: "delete-notes", path}` → `200 {"ok":true,"deleted":N}`; `400` bad op/id/path; `404 "no overview <id>"` / `"no note <id>"` / `"no notes on <path>"`; `409 "could not delete overview <id>"` / `"could not delete note <id>"` / `"could not delete the notes on <path>"` (the TUI's words). `steer.OpenFile` gains `Author string json:"author,omitempty"`, `Created string json:"created,omitempty"` (RFC3339 UTC), `Anchors int json:"anchors,omitempty"` — filled for overview rows by `ofList`.
- Behaviour change (on purpose, Global Constraints): the open-files `close` op never touches the store; a pinned entry (noted file, overview) backgrounds on esc AND on x; `POST /api/file-notes {op:"dismiss"}` is retired (404 route).

- [ ] **Step 1: Write the failing tests**

`internal/web/session_docs_delete_test.go`:

```go
package web

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Review Focus 4: x on a document row backgrounds it — the row stays, the
// file stays; esc too. Nothing is told "closed".
func TestCloseIsLocalOnADocumentRow(t *testing.T) {
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	writeDoc(t, dir, "20260101T220100-cd34.toml", noteDoc)
	s.docs.Reload(root)
	s.followDocs()
	ts := serve(t, s)
	wt := s.service().Root()
	f, _ := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"})
	for _, body := range []string{
		`{"op":"close","id":"20260101T220000-ab12","tab":"t1"}`,
		`{"op":"close","id":"20260101T220000-ab12","tab":"t1","everywhere":true}`,
		`{"op":"close","id":"` + f.ID + `","tab":"t1"}`,
		`{"op":"close","id":"` + f.ID + `","tab":"t1","everywhere":true}`,
	} {
		if code := postJSON(t, ts, "/api/open-files", body, "application/json", "", nil); code != http.StatusOK {
			t.Fatalf("%s: %d", body, code)
		}
	}
	if _, ok := s.ofs.resolve(wt, "20260101T220000-ab12", ""); !ok {
		t.Fatal("x took the overview row away")
	}
	if _, ok := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"}); !ok {
		t.Fatal("x took the noted file away")
	}
	for _, n := range []string{"20260101T220000-ab12.md", "20260101T220100-cd34.toml"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Fatalf("%s was unlinked", n)
		}
	}
	if s.docs.NoteCount(root, "f.txt") != 1 {
		t.Fatal("x dropped the note")
	}
}

func TestSessionDocsDeleteOverviewUnlinksAndDelists(t *testing.T) {
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	s.docs.Reload(root)
	s.followDocs()
	ts := serve(t, s)
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"delete-overview","id":"20260101T220000-ab12"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "20260101T220000-ab12.md")); !os.IsNotExist(err) {
		t.Fatal("the file is still there")
	}
	if _, ok := s.ofs.resolve(s.service().Root(), "20260101T220000-ab12", ""); ok {
		t.Fatal("the row is still listed")
	}
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"delete-overview","id":"20260101T220000-ab12"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Fatalf("delete again: %d, want 404", code)
	}
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"drop","id":"x"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Fatalf("unknown op: %d, want 400", code)
	}
}

func TestSessionDocsDeleteNotesOnAFile(t *testing.T) {
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220100-cd34.toml", noteDoc)
	writeDoc(t, dir, "20260101T220200-ef56.toml", noteDoc)
	s.docs.Reload(root)
	s.followDocs()
	ts := serve(t, s)
	var ans struct {
		Deleted int `json:"deleted"`
	}
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"delete-note","id":"20260101T220100-cd34"}`, "application/json", "", &ans); code != http.StatusOK || ans.Deleted != 1 {
		t.Fatalf("delete one: %d %+v", code, ans)
	}
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"delete-notes","path":"f.txt"}`, "application/json", "", &ans); code != http.StatusOK || ans.Deleted != 1 {
		t.Fatalf("delete all: %d %+v", code, ans)
	}
	if s.docs.NoteCount(root, "f.txt") != 0 {
		t.Fatal("notes left in the store")
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 0 {
		t.Fatalf("files left: %v", ents)
	}
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"delete-notes","path":"f.txt"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Fatalf("delete all again: %d, want 404", code)
	}
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"delete-notes","path":"../x"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Fatalf("bad path: %d, want 400", code)
	}
}

// A refused unlink keeps the row and the file and says so (409).
func TestSessionDocsDeleteRefusedSaysSo(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a directory mode does not refuse an unlink here")
	}
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	s.docs.Reload(root)
	s.followDocs()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	ts := serve(t, s)
	code, body := postJSONRaw(t, ts, "/api/session-docs", `{"op":"delete-overview","id":"20260101T220000-ab12"}`)
	if code != http.StatusConflict || body["error"] != "could not delete overview 20260101T220000-ab12" {
		t.Fatalf("refused delete: %d %v", code, body)
	}
	if _, ok := s.ofs.resolve(s.service().Root(), "20260101T220000-ab12", ""); !ok {
		t.Fatal("a refused delete took the row away")
	}
}

// An overview row carries its document's author, creation time and anchor
// count (the TUI's row).
func TestOverviewRowsCarryAuthorCreatedAndAnchors(t *testing.T) {
	t.Parallel()
	s, root, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T220000-ab12.md", tourDoc)
	s.docs.Reload(root)
	s.followDocs()
	var f struct {
		ID, Author, Created string
		Anchors             int
	}
	for _, x := range s.ofList(s.service().Root()) {
		if x.Source == "overview" {
			f.ID, f.Author, f.Created, f.Anchors = x.ID, x.Author, x.Created, x.Anchors
		}
	}
	if f.ID != "20260101T220000-ab12" || f.Author != "agent" || f.Created != "2026-01-01T22:00:00Z" || f.Anchors != 1 {
		t.Fatalf("row = %+v", f)
	}
}
```

`postJSONRaw` lives in `opdiscard_test.go` (returns `code, map[string]string`); confirm the error key it decodes is `"error"` by reading it.

Rewrites in `agentdocs_overview_web_test.go` / `agentdocs_web_test.go`:

```go
// esc and x on an overview entry both background it: a document is held
// by the store, never closed by the list (spec §4.4; close is local).
func TestXOnAnOverviewEntryBackgroundsIt(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	o, _ := s.docs.AddOverview(root, s.service().Root(), "T", "x")
	s.followDocs()
	ts := serve(t, s)
	for _, body := range []string{`{"op":"close","id":"` + o.ID + `","tab":"t1"}`, `{"op":"close","id":"` + o.ID + `","tab":"t1","everywhere":true}`} {
		postJSON(t, ts, "/api/open-files", body, "application/json", "", nil)
		if _, ok := s.docs.Overview(o.ID); !ok {
			t.Fatalf("%s removed the overview", body)
		}
		if _, ok := s.ofs.resolve(s.service().Root(), o.ID, ""); !ok {
			t.Fatalf("%s closed the overview entry", body)
		}
	}
}

// esc and x on a noted entry both background it; the notes stay.
func TestCloseOfAPinnedEntryBackgroundsEscAndX(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	steerAsk(t, s, `{"id":"1","cmd":"note_add","file":"f.txt","start":1,"end":1,"summary":"a"}`)
	s.followDocs()
	ts := serve(t, s)
	wt := s.service().Root()
	f, _ := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"})
	for _, body := range []string{`{"op":"close","id":"` + f.ID + `","tab":"t1"}`, `{"op":"close","id":"` + f.ID + `","tab":"t1","everywhere":true}`} {
		postJSON(t, ts, "/api/open-files", body, "application/json", "", nil)
		if _, ok := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"}); !ok || s.docs.NoteCount(root, "f.txt") != 1 {
			t.Fatalf("%s must keep the file and its note", body)
		}
	}
}

func TestSessionDocsDeleteNote(t *testing.T) {   // was TestDismissEndpoint
	t.Parallel()
	s, root := noteSrv(t)
	n, err := s.docs.AddNote(root, "f.txt", []string{"content 1"}, 1, 1, "s", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ts := serve(t, s)
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"delete-note","id":"`+n.ID+`"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	if s.docs.NoteCount(root, "f.txt") != 0 {
		t.Fatal("the note is still in the store")
	}
	if code := postJSON(t, ts, "/api/session-docs", `{"op":"delete-note","id":"`+n.ID+`"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Fatalf("delete again: %d, want 404", code)
	}
	if code := postJSON(t, ts, "/api/file-notes", `{"op":"dismiss","id":"t1"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Fatalf("the dismiss route is retired: %d, want 404", code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/web -run 'TestCloseIsLocalOnADocumentRow|TestSessionDocsDelete|TestOverviewRowsCarry|TestXOnAnOverviewEntryBackgroundsIt|TestCloseOfAPinnedEntryBackgroundsEscAndX'`
Expected: FAIL — x removes the rows (`x took the overview row away`); `/api/session-docs` is 404; `x.Author` does not compile.

- [ ] **Step 3: Implement**

`internal/steer/steer.go` `OpenFile`, after `Notes`:

```go
	// An overview document's front matter (session docs): who wrote it, when
	// (RFC 3339, UTC) and how many anchors it has. Empty on every other row.
	Author  string `json:"author,omitempty"`
	Created string `json:"created,omitempty"`
	Anchors int    `json:"anchors,omitempty"`
```

`agentdocs_follow.go` `ofList`:

```go
	for i := range l {
		switch l[i].Source {
		case "worktree":
			l[i].Notes = s.docs.NoteCount(root, l[i].Path)
		case "overview":
			if o, ok := s.docs.OverviewIn(root, l[i].ID); ok {
				l[i].Author, l[i].Anchors = o.Author, len(o.Anchors)
				if !o.Created.IsZero() {
					l[i].Created = o.Created.UTC().Format(time.RFC3339)
				}
			}
		}
	}
```

`openfiles_http.go` `case "close"` becomes:

```go
	case "close":
		// Close is LOCAL (spec 2026-10-18-gg-session-docs §4.4): the list
		// never touches a document. A pinned entry — an agent's notes are on
		// it, or it is an overview — backgrounds on esc and on x alike: its
		// document holds it in the list; the follow pass would relist it.
		_, pinned, found := s.ofs.pinnedEntry(wt, q.ID)
		switch {
		case !found:
			ok = false
		case pinned:
			ok = s.ofs.background(wt, q.ID, q.Tab, q.Line)
		default:
			ok = s.ofs.close(wt, q.ID, q.Tab, q.Everywhere)
		}
```

`file_notes.go` becomes `session_docs_http.go` (git mv, then rewrite):

```go
package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/homeend/gigagit/internal/agentdocs"
)

// POST /api/session-docs: the page's DELETES of session documents (spec
// 2026-10-18-gg-session-docs §4.4) — an overview document, one note, every
// note on a file — after the page's own confirm. The store's signal tells
// every tab (and a hosting TUI) after; a refused unlink is 409 in the
// TUI's words.
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/session-docs", writeGuard(s.handleSessionDocs))
	})
}

func (s *Server) handleSessionDocs(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Op   string `json:"op"`
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	root := s.docsRoot(r.Context())
	deleted := 0
	switch q.Op {
	case "delete-overview":
		if !agentdocs.ValidID(q.ID) {
			writeErr(w, http.StatusBadRequest, errors.New("want an overview id"))
			return
		}
		if _, ok := s.docs.OverviewIn(root, q.ID); !ok {
			writeErr(w, http.StatusNotFound, errors.New("no overview "+q.ID))
			return
		}
		if !s.docs.RemoveOverviewIn(root, q.ID) {
			writeErr(w, http.StatusConflict, errors.New("could not delete overview "+q.ID))
			return
		}
		deleted = 1
		s.followDocs() // the row goes now, in every tab
	case "delete-note":
		if !agentdocs.ValidID(q.ID) {
			writeErr(w, http.StatusBadRequest, errors.New("want a note id"))
			return
		}
		if _, ok := s.docs.FindNoteIn(root, q.ID); !ok {
			writeErr(w, http.StatusNotFound, errors.New("no note "+q.ID))
			return
		}
		if !s.docs.RemoveNoteIn(root, q.ID) {
			writeErr(w, http.StatusConflict, errors.New("could not delete note "+q.ID))
			return
		}
		deleted = 1
	case "delete-notes":
		if !isGitArgSafe(q.Path) || !filepath.IsLocal(filepath.FromSlash(q.Path)) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
			return
		}
		want := s.docs.NoteCount(root, q.Path)
		if want == 0 {
			writeErr(w, http.StatusNotFound, errors.New("no notes on "+q.Path))
			return
		}
		if deleted = s.docs.ClearPath(root, q.Path); deleted < want {
			writeErr(w, http.StatusConflict, errors.New("could not delete the notes on "+q.Path))
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, errors.New("unknown op "+q.Op))
		return
	}
	writeJSON(w, map[string]any{"ok": true, "deleted": deleted})
}
```

(The old `POST /api/file-notes` route goes with the file; the page's `dismissNote` is rewritten in Task 5 — until then the page's dismiss button 404s, which Task 5's commit fixes in the same branch.)

- [ ] **Step 4: Run the web and steer tests**

Run: `go test ./internal/web ./internal/steer 2>&1 | tail -5`
Expected: PASS. `TestXOnAnAttachedOverviewKeepsTheDocument` passes for the right reason now (no store call at all).

- [ ] **Step 5: Commit**

```bash
gg add internal/steer/steer.go internal/web/agentdocs_follow.go internal/web/openfiles_http.go internal/web/session_docs_http.go internal/web/session_docs_delete_test.go internal/web/agentdocs_overview_web_test.go internal/web/agentdocs_web_test.go
git commit -F <msgfile>   # "web: close is local (a pinned entry backgrounds on esc and x; the interim gate goes); POST /api/session-docs deletes an overview, a note or a file's notes; overview rows carry author, created, anchors"
```

(`gg add` after a `git mv` stages the rename; if `file_notes.go` still shows as deleted, `gg add internal/web/file_notes.go` too.)

---

### Task 5: the page — rows with author and age, D / row menu / note box / overview footer deletes behind the confirm

**Files:**
- Modify: `internal/web/static/openfiles.js`, `internal/web/static/viewer.js`
- Test: `internal/web/openfilesjs_test.go` (update `TestSwitcherModelJS`; add `TestSwitcherOverviewRowMetaAndDeleteActionJS`), `internal/web/viewerjs_test.go` (add `TestViewerDeletePromptJS`)

**Interfaces:**
- Consumes: `POST /api/session-docs` (Task 4), the row fields `author`/`created`/`anchors` (Task 4).
- Produces (pure block of `openfiles.js`): `switcherRows(files, mine, now = Date.now())` rows gain `meta` (an overview row's `"overview · N anchors · author · age"`, else `""`); `rowAge(iso, nowMs)` (`""` for none; `s`/`m`/`h`/`d`/`y`); `deleteAction(f)` → `"overview"` | `"notes"` | `""`; `deletePrompt(f)` → `"Delete this overview?\n<title>"` | `"Delete the note on <path>?"` | `"Delete the N notes on <path>?"` | `""`. Pure block of `viewer.js` (note model): `notePrompt(n)` → `"Delete this note?\n<summary>"`, `notesPrompt(path, count)`.

- [ ] **Step 1: Write the failing tests**

In `openfilesjs_test.go`, change `TestSwitcherModelJS`'s `want` so both rows carry `"meta":""` after `"notes"`, and add:

```go
// An overview row's meta is the TUI's: "overview · N anchors · author ·
// age"; D deletes the overview, or a noted file's notes, or nothing.
func TestSwitcherOverviewRowMetaAndDeleteActionJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const now = Date.parse("2026-01-02T00:00:00Z");
const ov = {id: "20260101T220000-ab12", path: "overview-20260101T220000-ab12.md", source: "overview", title: "Tour of f", author: "agent", created: "2026-01-01T22:00:00Z", anchors: 1};
const noted = {id: "f2", path: "a/b.go", source: "worktree", notes: 2};
const plain = {id: "f3", path: "c.txt", source: "worktree"};
const rows = switcherRows([ov, noted, plain], "", now);
console.log([rows[0].meta, rows[1].meta, rowAge("", now), rowAge("2025-01-01T00:00:00Z", now),
  deleteAction(ov), deleteAction(noted), deleteAction(plain),
  deletePrompt(ov), deletePrompt(noted), deletePrompt({id: "f4", path: "d.go", source: "worktree", notes: 1}), deletePrompt(plain)].join("|"));
`)
	want := "overview · 1 anchors · agent · 2h||" + "|1y|overview|notes||Delete this overview?\nTour of f|Delete the 2 notes on a/b.go?|Delete the note on d.go?|"
	if out != want {
		t.Fatalf("got  %q\nwant %q", out, want)
	}
}
```

In `viewerjs_test.go` (read its existing pure-block constants for the note model first — `--- note model (pure; guarded against Go) ---` / `--- end note model ---`):

```go
func TestViewerDeletePromptJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", noteModelStart, noteModelEnd, `
console.log([notePrompt({id: "20260101T220100-cd34", summary: "why one?"}), notesPrompt("f.txt", 1), notesPrompt("f.txt", 3)].join("|"));
`)
	if want := "Delete this note?\nwhy one?|Delete the note on f.txt?|Delete the 3 notes on f.txt?"; out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}
```

(If `viewerjs_test.go` names those markers differently, use its names.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/web -run 'TestSwitcherModelJS|TestSwitcherOverviewRowMetaAndDeleteActionJS|TestViewerDeletePromptJS'`
Expected: FAIL — `rowAge is not defined` / `notePrompt is not defined`; `TestSwitcherModelJS` wants `meta`.

- [ ] **Step 3: Implement `openfiles.js`**

Pure block (replace `ageOf` with `rowAge`, keep `ageOf`'s callers on `rowAge`):

```js
// rowAge is a compact age for a row ("2h", "3d"); "" for an unknown time.
function rowAge(iso, now) {
  const t = Date.parse(iso || "");
  if (!t) return "";
  const s = Math.max(0, Math.floor((now - t) / 1000));
  if (s < 60) return s + "s";
  if (s < 3600) return Math.floor(s / 60) + "m";
  if (s < 86400) return Math.floor(s / 3600) + "h";
  if (s < 86400 * 365) return Math.floor(s / 86400) + "d";
  return Math.floor(s / (86400 * 365)) + "y";
}

// overviewRowMeta is an overview row's meta (the TUI's openFileRowText):
// "overview · N anchors · author · age"; author and age only when known.
function overviewRowMeta(f, now) {
  const parts = ["overview · " + (f.anchors || 0) + " anchors"];
  if (f.author) parts.push(f.author);
  const age = rowAge(f.created, now);
  if (age) parts.push(age);
  return parts.join(" · ");
}

// deleteAction is what D (and the row menu's Delete) does on a row: the
// overview document, a noted file's notes, or nothing.
function deleteAction(f) {
  if (f.source === "overview") return "overview";
  if (f.source === "worktree" && f.notes > 0) return "notes";
  return "";
}

// deletePrompt is the confirm's prompt for deleteAction(f) ("" = nothing).
function deletePrompt(f) {
  switch (deleteAction(f)) {
    case "overview": return "Delete this overview?\n" + (f.title || f.path);
    case "notes": return f.notes === 1 ? "Delete the note on " + f.path + "?" : "Delete the " + f.notes + " notes on " + f.path + "?";
  }
  return "";
}
```

`switcherRows(files, mine, now = Date.now())` adds `meta: f.source === "overview" ? overviewRowMeta(f, now) : ""` to each row. Outside the pure block:

- `renderSwitcher` files branch: `const meta = r.source === "overview" ? "  " + r.meta : r.line + "  " + versionLabel(r.source, r.rev) + (r.notes ? …)` — an overview row shows its meta instead of `versionLabel`'s "overview".
- `FOOT_FILES` gains `<button data-oact="D">D delete</button>` after `x close file`; the foot click handler maps `"D"` → `deleteSelected()`; `switcherKey` gains `case "D": if (sw.tab === "files") deleteSelected(); break;`.
- Right-click on a files row (`root.addEventListener("contextmenu", …)`: find `.ofrow[data-i]` while `sw.tab === "files"`, `preventDefault`, select it, `showCtxMenu([{label: "open", act: bringBack}, {label: "close in every tab", act: closeSelected}, …(deleteAction(f) ? [{sep: true}, {label: deleteAction(f) === "overview" ? "Delete overview" : "Delete all notes on this file", danger: true, act: deleteSelected}] : [])], e.clientX, e.clientY)`). Import `showCtxMenu` from `./layers.js` and `showLocalConfirm` from `./ops.js`.
- `deleteSelected()`:

```js
// deleteSelected: D / the row menu's Delete. The switcher closes first (one
// layer asks at a time, as a kill does), the confirm's cancel is the
// default, and the live agentdocs event takes the row away.
function deleteSelected() {
  if (sw.tab !== "files") return;
  const f = sw.files[sw.sel];
  if (!f) return;
  const what = deleteAction(f);
  if (!what) return opLine("nothing to delete on this row", false);
  closeSwitcher();
  showLocalConfirm(deletePrompt(f), ["cancel", "delete"], async (opt) => {
    if (opt !== "delete") return;
    const body = what === "overview" ? { op: "delete-overview", id: f.id } : { op: "delete-notes", path: f.path };
    let ans;
    try {
      ans = await postJSON("/api/session-docs", body);
    } catch (e) {
      return opLine("delete failed: " + (e.message || e), true);
    }
    opLine(what === "overview" ? "overview " + JSON.stringify(f.title || f.path) + " deleted" : ans.deleted === 1 ? "deleted the note" : "deleted " + ans.deleted + " notes", false);
  });
}
```

(`postJSON` rejects with `e.message` holding the server's error text — read `core.js`'s `postJSON` to confirm, so a 409 shows "could not delete overview …".) Update the `registerHelp` text: "… <b>x</b> closes it in every tab (an agent's overview or a noted file goes to the background instead: its document holds it), <b>D</b> deletes an agent's overview document or every note on the file, after asking; right-click a row for the same."

- [ ] **Step 4: Implement `viewer.js`**

Note model pure block gains:

```js
// notePrompt / notesPrompt are the delete confirms' prompts (the TUI's words).
function notePrompt(n) {
  return "Delete this note?\n" + (n.summary || n.id);
}
function notesPrompt(path, count) {
  return count === 1 ? "Delete the note on " + path + "?" : "Delete the " + count + " notes on " + path + "?";
}
```

Then:

- `noteBoxHTML`: the button `data-nact="dismiss"` → `data-nact="delete"` labelled `delete`; the click handler at the `button[data-nact]` branch: `act.dataset.nact === "delete" ? deleteNote(n) : copyNoteRef(n)`.
- `dismissNote(id)` → `deleteNote(n)`:

```js
// deleteNote asks, then unlinks the note's file everywhere (every tab, and
// a TUI hosting this page, hear it from the store).
function deleteNote(n) {
  showLocalConfirm(notePrompt(n), ["cancel", "delete"], async (opt) => {
    if (opt !== "delete") return;
    try {
      await postJSON("/api/session-docs", { op: "delete-note", id: n.id });
    } catch (e) {
      return opLine("delete failed: " + (e.message || e), true);
    }
    view.notes = view.notes.filter((x) => x.id !== n.id);
    rerenderKeepingScroll();
    swapFoot(true);
    opLine("note " + n.id + " deleted", false);
  });
}

// deleteFileNotes asks, then unlinks every note on the shown file.
function deleteFileNotes() {
  const path = view.path, count = view.notes.length;
  if (!count) return opLine("no notes on this file", false);
  showLocalConfirm(notesPrompt(path, count), ["cancel", "delete"], async (opt) => {
    if (opt !== "delete") return;
    let ans;
    try {
      ans = await postJSON("/api/session-docs", { op: "delete-notes", path });
    } catch (e) {
      return opLine("delete failed: " + (e.message || e), true);
    }
    view.notes = [];
    rerenderKeepingScroll();
    swapFoot(true);
    opLine(ans.deleted === 1 ? "deleted the note" : "deleted " + ans.deleted + " notes", false);
  });
}

// deleteOverview asks, then unlinks overview id's file; the follow pass
// closes it where it is open and says it was deleted.
function deleteOverview(id, title) {
  showLocalConfirm("Delete this overview?\n" + title, ["cancel", "delete"], async (opt) => {
    if (opt !== "delete") return;
    try {
      await postJSON("/api/session-docs", { op: "delete-overview", id });
    } catch (e) {
      return opLine("delete failed: " + (e.message || e), true);
    }
  });
}
```

  `showLocalConfirm` must be imported from `./ops.js` (check the import line; `opLine` is already there).
- Every `dismissNote(n.id)` call site (the `d` key in `fileKey`, the foot's `case "dismiss"`) → `deleteNote(n)`; the foot chips `d dismiss` → `d delete` (both the split and the plain file foot); the foot `data-vact="dismiss"` → `"delete"`.
- `openViewerMenu`: before `showCtxMenu`, when `view.src === "worktree" && view.notes.length`: `const n = noteAtCursor(); items.push({ sep: true }); if (n && !n.readonly) items.push({ label: "Delete note", danger: true, act: () => deleteNote(n) }); items.push({ label: "Delete all notes on this file", danger: true, act: deleteFileNotes });`.
- `overviewKey`: `case "D": if (!view.ov.stored) deleteOverview(view.id, view.ov.title); break;`; `viewerFoot`'s `view.ov` (not stored) branch gains `<button data-vact="odel">D delete</button>` before `esc background`; the `split && split.left` branch gains the same when `!split.ov.stored`; the foot click handler: `case "odel": if (split && split.left) { if (!split.ov.stored) deleteOverview(split.id, split.ov.title); } else if (view.ov && !view.ov.stored) deleteOverview(view.id, view.ov.title); break;`; `splitKey`: when the left column has focus, `D` → the same (read `splitKey` to find where the left column's keys are handled).
- Prose: `splitOverviewGone`'s and `viewerAgentDocs`'s `name + " was closed"` → `" was deleted"`; `anchorBack`'s two `"the overview was closed"` (lines ≈1082, 1812) → `"the overview was deleted"`. Leave `"was closed in another tab"` (the open_files path).

- [ ] **Step 5: Run the JS tests and the whole web package**

Run: `go test ./internal/web 2>&1 | tail -5`
Expected: PASS (the pure-JS tests need `node`; `runPureJS` skips without it — if it skips on this machine, say so in the ledger and run `node --version` to confirm).

- [ ] **Step 6: Browser check (manual, one pass; a `curl /api/repo` first, per the probe lessons)**

Run: `cd /work/gigagit/.claude/worktrees/session-docs-web && go build -o bin/gg ./cmd/gg && ./bin/gg web --open` in a scratch repo under `/tmp/claude-1000/…/scratchpad` (never the real repo), write `20260101T220000-ab12.md` (tourDoc) and `20260101T220100-cd34.toml` (noteDoc, path = a file of that repo) by hand into the docs dir printed by `go run ./cmd/gg … ` — compute it with a one-line Go test or from `XDG_STATE_HOME`: `<state>/gg/sessions/<EncodeRepoKey(commonDir)>/steer/<EncodeRepoKey(wt)>/docs/`. Expect, in the browser: the Open files tab row `Tour of f  overview · 1 anchors · agent · <age>`; enter opens it; esc keeps the row; the note box has `delete`; `D` on the row asks `Delete this overview?`, delete removes the row and the file. Record the outcome (seen / not seen) in the ledger; a mismatch is a finding to fix before the commit.

- [ ] **Step 7: Commit**

```bash
gg add internal/web/static/openfiles.js internal/web/static/viewer.js internal/web/openfilesjs_test.go internal/web/viewerjs_test.go
git commit -F <msgfile>   # "web page: overview rows show author and age; D, the row menu, the note box, the . menu and the overview footer delete after the confirm; close stays local"
```

---

### Task 6: the CLI accepts document ids on `gg session overview|note`

**Files:**
- Modify: `internal/cli/session_overview.go` (`set`, `show`, `rm`, `export` id checks), `internal/cli/session_note.go` (`show`, `rm` id checks — `noteIDPattern`)
- Test: `internal/cli/session_docs_ids_test.go` (new)

**Interfaces:**
- Consumes: `agentdocs.ValidID`.
- Produces: `gg session overview set|show|rm|export <id>` and `gg session note show|rm <note-id>` take any document id (`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`); `files focus <id|path>` and `note clear <path>|<file-id>` keep `f<n>` (a bare word there is a path — plan 4 revisits the verbs).

- [ ] **Step 1: Write the failing test**

Read `internal/cli/session_overview_test.go` / `session_note_test.go` for how a misuse (exit 2 + usage on stderr) is asserted and which helper runs a verb with no live session (the refusal before the id check vs after). The test asserts only the usage gate:

```go
package cli

import (
	"bytes"
	"strings"
	"testing"
)

// A document id — a session document's file name — passes the verbs' id
// check; a path-like word still does not.
func TestSessionVerbsAcceptDocumentIDs(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		args []string
		ok   bool
	}{
		{[]string{"overview", "show", "20260101T220000-ab12"}, true},
		{[]string{"overview", "rm", "tour-brief-123-4"}, true},
		{[]string{"overview", "show", "f1"}, true},
		{[]string{"overview", "show", "a/b"}, false},
		{[]string{"overview", "show", "a.md"}, false},
		{[]string{"note", "show", "20260101T220100-cd34"}, true},
		{[]string{"note", "rm", "t1"}, true},
		{[]string{"note", "rm", "../x"}, false},
	} {
		var stderr bytes.Buffer
		code := <run gg session with c.args in a temp repo with no live session, stderr captured>
		usage := strings.Contains(stderr.String(), "usage: gg session")
		if c.ok && usage {
			t.Errorf("%v: refused as misuse: %s", c.args, stderr.String())
		}
		if !c.ok && !usage {
			t.Errorf("%v: not a misuse (code %d): %s", c.args, code, stderr.String())
		}
	}
}
```

Fill the `<run …>` from the existing tests' helper (e.g. the function that calls `sessionOverview(dir, args, stdout, stderr)` directly with a `sessDir` over a temp repo: with no presence the verb exits 1 "no live gg session" AFTER the id check, which is what distinguishes the two outcomes).

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli -run TestSessionVerbsAcceptDocumentIDs`
Expected: FAIL — the document ids print the usage.

- [ ] **Step 3: Implement**

In `session_overview.go`: every `fileIDPattern.MatchString(pos[0])` in `set`, `show`, `rm` and `export` → `agentdocs.ValidID(pos[0])` (import `github.com/homeend/gigagit/internal/agentdocs`); leave `fileIDPattern` where `files focus` / `note clear` tell an id from a path. In `session_note.go`: `noteIDPattern.MatchString(pos[0])` in `show` and `rm` → `agentdocs.ValidID(pos[0])`; delete `noteIDPattern` if nothing else uses it. Update `using-gg.md`? No — plan 4 rewrites these verbs and bumps the skill; this task changes acceptance only.

- [ ] **Step 4: Run the CLI tests**

Run: `go test ./internal/cli 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/cli/session_overview.go internal/cli/session_note.go internal/cli/session_docs_ids_test.go
git commit -F <msgfile>   # "cli: gg session overview|note take a document id (the file name) — the web and the TUI answer for session documents now"
```

---

### Task 7: e2e — a document written by hand shows through a standalone gg web

**Files:**
- Modify: `e2e/scenario.go` (`Step.Doc`, `kind()`, validation), `e2e/builder.go` (`case "doc"`), `e2e/scenarios/s104_session_note_web.toml`, `e2e/scenarios/s105_session_overview_web.toml`
- Create: `e2e/scenarios/s106_session_docs_web.toml`
- Test: `e2e/scenario_test.go` (`TestStepDocValidation`)

**Interfaces:**
- Produces: `[input] steps = [{ doc = "<name>", content = "…" }]` — writes `<name>` into the LOCAL repo's session-docs directory (`steer.DocsDir(config.SessionSteerDir(<common dir>, <toplevel>))` under the suite's `XDG_STATE_HOME`); `<name>` must pass `agentdocs.DocName`; only valid with `web = true` (the TUI harness has its own `[[tui.step]] doc`).

- [ ] **Step 1: Write the failing validation test**

In `e2e/scenario_test.go`, beside `TestTUIStepDocValidation`:

```go
func TestStepDocValidation(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		toml string
		err  string
	}{
		{"[input]\nweb = true\nsteps = [{ doc = \"20260101T220000-ab12.md\", content = \"x\" }, { write = \"a\", content = \"b\" }, { commit = \"c\" }]\n[expect]\nbranch = \"main\"\n", ""},
		{"[input]\nsteps = [{ doc = \"20260101T220000-ab12.md\", content = \"x\" }, { write = \"a\", content = \"b\" }, { commit = \"c\" }]\n[expect]\nbranch = \"main\"\n", "doc steps need web = true"},
		{"[input]\nweb = true\nsteps = [{ doc = \"bad name.md\", content = \"x\" }, { write = \"a\", content = \"b\" }, { commit = \"c\" }]\n[expect]\nbranch = \"main\"\n", "is not a document name"},
	} {
		_, err := parseScenario(t, "name = \"t\"\n"+c.toml)
		switch {
		case c.err == "" && err != nil:
			t.Errorf("%q: unexpected error %v", c.toml, err)
		case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
			t.Errorf("%q: error = %v, want %q", c.toml, err, c.err)
		}
	}
}
```

Use the parse helper `TestTUIStepDocValidation` uses (read it; the name above is a guess).

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./e2e -run TestStepDocValidation`
Expected: FAIL — the `doc` key is unknown to the TOML decoder (strict) or the step has "no action".

- [ ] **Step 3: Implement the step**

`scenario.go` `Step`: `Doc string \`toml:"doc"\` // a session document's file name; Content holds it — written into the local repo's docs directory (web scenarios)`. `kind()`: `if s.Doc != "" { kinds = append(kinds, "doc") }`; the `Content` rule becomes `k != "write" && k != "doc"`; in `kind()` after `k` is known: `if k == "doc" { if _, _, ok := agentdocs.DocName(s.Doc); !ok { return "", fmt.Errorf("step %+v: %q is not a document name", s, s.Doc) } }`. In the scenario-level validation (where `Input.Steps` are walked — find it with `grep -n "Input.Steps" e2e/scenario.go`): a `doc` step with `!sc.Input.Web` → `errors.New("doc steps need web = true")`.

`builder.go` `runSteps`:

```go
		case "doc":
			p := filepath.Join(b.docsDir(t), st.Doc)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(ExpandText(st.Content)), 0o644); err != nil {
				t.Fatal(err)
			}
```

and

```go
// docsDir is the local repo's session-docs directory — what a standalone
// gg web attaches at Start (web.go), under the suite's XDG_STATE_HOME.
func (b *Sandbox) docsDir(t *testing.T) string {
	t.Helper()
	cd := filepath.Join(b.LocalDir, strings.TrimSpace(b.gitOut(t, b.LocalDir, "rev-parse", "--git-common-dir")))
	if filepath.IsAbs(strings.TrimSpace(b.gitOut(t, b.LocalDir, "rev-parse", "--git-common-dir"))) { … }
	top := strings.TrimSpace(b.gitOut(t, b.LocalDir, "rev-parse", "--show-toplevel"))
	return steer.DocsDir(config.SessionSteerDir(cd, top))
}
```

Write it properly: `git rev-parse --path-format=absolute --git-common-dir` gives an absolute path in one call (git ≥ 2.31); use that and drop the `IsAbs` dance. `b.gitOut` = whatever the sandbox's "run git, return stdout" helper is called (grep `func (b \*Sandbox) git`). The key must equal what the server computes: `config.SessionSteerDir` encodes the paths it is given — the server passes `svc.GitCommonDir` / `svc.TopLevel` results, so pass the same spellings (absolute, cleaned; on Windows the drive letter case must match `git`'s — a `filepath.Clean` on both is what `EncodeRepoKey` does; read it).

- [ ] **Step 4: Write the scenario and update the two it changes**

`e2e/scenarios/s106_session_docs_web.toml`:

```toml
name = "session docs through a standalone gg web: documents written by hand show, a delete unlinks (spec 2026-10-18-gg-session-docs)"

# a.txt is committed. An agent writes an overview and a note on a.txt:2
# straight into the worktree's docs directory — no gg verb — before the page
# starts; the page lists both at Start. The CLI reads them through the web's
# steer reply; rm deletes the overview's file; the note's file goes with
# note rm.
[input]
web = true
steps = [
  { write = "a.txt", content = "one\ntwo\nthree\n" },
  { commit = "add a" },
  { doc = "20260101T220000-ab12.md", content = "+++\nformat = 1\ntitle = \"Tour of a\"\nauthor = \"agent\"\ncreated = 2026-01-01T22:00:00Z\n+++\n# Tour\n\nSee [two](a.txt:2).\n" },
  { doc = "20260101T220100-cd34.toml", content = "format = 1\npath = \"a.txt\"\nstart = 2\nend = 2\nsummary = \"why two?\"\nauthor = \"agent\"\nlines = [\"two\"]\n" },
]

[[run]]
cmd             = ["session", "files"]
exit            = 0
stdout_contains = ["20260101T220000-ab12\t\"Tour of a\"\toverview", "a.txt\tworktree"]

[[run]]
cmd             = ["session", "overview", "list"]
exit            = 0
stdout_contains = ["20260101T220000-ab12\tbackground\t1 anchors\tTour of a"]

[[run]]
cmd             = ["session", "overview", "show", "20260101T220000-ab12"]
exit            = 0
stdout_contains = ["20260101T220000-ab12\tTour of a", "[two](a.txt:2)"]

[[run]]
cmd             = ["session", "note", "list"]
exit            = 0
stdout_contains = ["20260101T220100-cd34\ta.txt\t2-2\twhy two?"]

[[run]]
cmd             = ["session", "note", "show", "20260101T220100-cd34"]
exit            = 0
stdout_contains = ["2\ttwo"]

[[run]]
cmd             = ["session", "overview", "rm", "20260101T220000-ab12"]
exit            = 0
stdout_contains = ["removed 20260101T220000-ab12"]

[[run]]
cmd             = ["session", "overview", "show", "20260101T220000-ab12"]
exit            = 1
stderr_contains = ["no overview 20260101T220000-ab12"]

[[run]]
cmd             = ["session", "note", "rm", "20260101T220100-cd34"]
exit            = 0
stdout_contains = ["removed 20260101T220100-cd34"]

[[run]]
cmd             = ["session", "note", "show", "20260101T220100-cd34"]
exit            = 1
stderr_contains = ["no note 20260101T220100-cd34"]

[expect]
branch = "main"
```

(Read `internal/cli/session_files.go` / `session_note.go` / `session_overview.go` for the exact tab layouts before trusting these `stdout_contains`; s104/s105 show them.)

`s104`: the ids are minted now (`<UTC second>-<4 hex>`): `"noted a.txt:2 as t1"` → `"noted a.txt:2 as "`; `"t1\ta.txt\t2-2\tlook at bravo"` → `"\ta.txt\t2-2\tlook at bravo"`; the `show t1` / `rm t1` / `rm t1 again` runs go (s106 covers them with a known id); keep the `a.txt:9` refusal. `s105`: `"f1"` in the add's `stdout_contains` → drop it, keep `"unresolved: nope.txt"` and `"in the background"`; list → `"\tbackground\t2 anchors\tT"`; files → `"\"T\"\toverview"`; the `show f1` / `set f1` / second list / `rm f1` / `rm f1 again` runs go (s106 covers show/rm; `set` with a known id: add to s106 a run `["session", "overview", "set", "20260101T220000-ab12", "--title", "T2"]` with stdin `[b](a.txt:3)\n`, `stdout_contains = ["20260101T220000-ab12"]`, then a list expecting `"\tbackground\t1 anchors\tT2"` — place it before the rm). Also `s100_session_files_web.toml`: read it; if it names `f1` for a worktree file, that still holds (files keep `f<n>`).

- [ ] **Step 5: Run the e2e scenarios**

Run: `go test ./e2e -run 'TestScenarios/(s104|s105|s106|s100)' 2>&1 | tail -15`
Expected: PASS. A `stdout_contains` miss prints the run's stdout — fix the expectation to what the CLI prints, never the other way round, unless the CLI is wrong.

- [ ] **Step 6: Commit**

```bash
gg add e2e/scenario.go e2e/builder.go e2e/scenario_test.go e2e/scenarios/s104_session_note_web.toml e2e/scenarios/s105_session_overview_web.toml e2e/scenarios/s106_session_docs_web.toml
git commit -F <msgfile>   # "e2e: s106_session_docs_web — documents written by hand show through a standalone gg web; doc input step; s104/s105 follow the minted ids"
```

---

### Task 8: docs, gofmt, the race gate

**Files:**
- Modify: `CHANGELOG.md` (a "Session docs, part 3: gg web reads the directory" section ABOVE "Session docs, part 2"), `docs/CLAUDE-details.md` ("### Part 3 — the web reader" after Part 2), `README.md` (the two passages: notes "dismiss one in the browser…" → delete after a confirm; overviews "gg web shows overviews too …" gains the row's author/age and `D`), `CLAUDE.md` `web` row — only if a responsibility changed (it did not: leave it).

- [ ] **Step 1: CHANGELOG**

Under a new `## Session docs, part 3: gg web reads the directory`:

```markdown
### Added
- gg web is a session-docs reader (spec `docs/superpowers/specs/2026-10-18-gg-session-docs-design.md`):
  a page the TUI hosts reads the terminal's attached store; a standalone
  `gg web` attaches its own store to the served worktree's docs directory
  at start and on every repository switch, and closes it on exit. A
  document an agent writes there by hand appears within the watch's wake:
  an overview as a row of the `ctrl+\` switcher's Open files tab (title,
  `overview · N anchors · author · age`), a note as a box on the file.
- Deleting is a separate, confirmed action on the page: `D` on an Open
  files row (or its right-click menu) deletes an agent's overview document
  or every note on a file; the note box's **delete** button, the viewer's
  `d` key and its `.` menu's **Delete note** / **Delete all notes on this
  file**; `D` in an open overview (the split too). A delete the directory
  refuses says so and leaves the row.
- `POST /api/session-docs` (`delete-overview` / `delete-note` /
  `delete-notes`); the open-files rows carry `author`, `created`, `anchors`
  for an overview (`gg session files --json` too).
- e2e: `{ doc = "<name>", content = "…" }` input steps write into the local
  repo's docs directory for `web = true` scenarios; `s106_session_docs_web`.

### Changed
- Closing is local on the page: `esc` and `x` (close in every tab) on an
  agent's overview or a noted file put it in the background — the document
  holds it in the list. `x` no longer removes notes or overviews; the
  interim gate of part 2 is gone. `POST /api/file-notes {op: dismiss}` is
  retired. An overview that left the directory is reported as "deleted",
  not "closed".
- The web's steer `overview_rm` deletes the document and answers
  `removed <id>` (the TUI's words); every web endpoint and the steer wire
  accept a document id (a file name) where they took `f<n>` / `t<n>`;
  `gg session overview set|show|rm|export <id>` and `gg session note
  show|rm <id>` accept one too. Overviews a standalone `gg web` files for
  an agent get minted ids (`<UTC second>-<4 hex>`), as the TUI's do.
```

- [ ] **Step 2: CLAUDE-details**

After Part 2, in the same style (attach points, identity, close vs delete, endpoint, page, CLI, e2e): name `internal/web/session_docs.go` (`docsDirFor` / `attachDocs` / `detachDocs`, the `docsDirOf` seam, the call sites `Host.Start` → before `startDocsFollow`, `adoptService` → detach before the swap and attach before `followDocs`, `Server.Close` → `docs.Close()` when `!hosted`); `session_docs_http.go` (the three ops, 404 vs 409, `ClearPath < NoteCount` = refused); `openfiles_http.go` close (pinned → background on esc and x); `ofList` fills `Author/Created/Anchors`; `overviewKey` = `overview-<id>.md`; the page's `deleteSelected` / `deleteNote` / `deleteFileNotes` / `deleteOverview` and the "was deleted" prose; the CLI id widening and what `fileIDPattern` still gates; the e2e `doc` step and `Sandbox.docsDir`.

- [ ] **Step 3: README**

Notes passage: "… and **dismiss** and **copy reference** buttons" → "… a **delete** button (it asks first; `d` too) and **copy reference**"; "dismiss one in the browser and it leaves the terminal" → "delete one in the browser and it leaves the terminal". Overviews passage: after "gg web shows overviews too:" add "its Open files tab lists each one with its author and age, `D` (or the row's right-click menu) deletes the document after asking,".

- [ ] **Step 4: gofmt, vet, the race gate**

Run: `cd /work/gigagit/.claude/worktrees/session-docs-web && gofmt -l ./internal ./e2e ./cmd && go vet ./... 2>&1 | tail -3`
Expected: no files listed, vet clean.

Run: `./test.sh race > /tmp/claude-1000/-work-gigagit/3f942bc5-9389-49a0-a00f-fef0425e1a09/scratchpad/race3.log 2>&1; grep -n "all green\|FAIL\|DATA RACE" /tmp/claude-1000/-work-gigagit/3f942bc5-9389-49a0-a00f-fef0425e1a09/scratchpad/race3.log | tail -5`
Expected: a line containing `all green`, no `FAIL`, no `DATA RACE`.

- [ ] **Step 5: Commit**

```bash
gg add CHANGELOG.md docs/CLAUDE-details.md README.md
git commit -F <msgfile>   # "docs: session docs part 3 — changelog, details, README"
```

---

## Self-review notes

- Spec coverage: §4.4 rows (Task 4 wire + Task 5 rows), close vs delete (Tasks 4, 5), §6.4 hosted/standalone/re-root (Task 3), ids = file names through every gate (Tasks 2, 6), §12 "internal/web: the same over HTTP" (Tasks 2–4) and the e2e (Task 7). The fast switch's per-slot stores are the TUI's (plan 2). Steer verb retirement, the CLI's write verbs and the skills are plan 4.
- Not in this plan (rulings carried from plan 2): a reader never writes; the TUI's `files` steer reply does not fill the new row fields (the web fills them in `ofList`; plan 4 may add them to the TUI's reply) — ledger it.
- Review Focus 5 is the only one without a Go test: the split's delete is page code; Task 5's manual check covers it and the reviewer reads `splitKey`.
