# gg session docs — plan 4 of 4: the CLI writes the files, the steer verbs retire, the skills follow

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task, inline (this repo forbids implementer subagents; one read-only review subagent at the end). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `gg session note …` and `gg session overview …` write, read and unlink the FILES of the worktree's session-docs directory themselves — no live window needed — and an `add`/`set` ends by asking a live reader to show the new id; the nine `note_*` / `overview_*` steer verbs are retired in the TUI and gg web with the spec's refusal; the readers' `files` reply and `file_focus` catch up (author/created/anchors on the TUI; a focus right after a write reloads the directory before refusing); the skills, specs and the plugin's copies describe the file model.

**Architecture:** The CLI gets no second writer: a verb opens a throw-away `agentdocs.Store`, `Attach`es the caller's worktree to `steer.DocsDir(inbox)` and reuses the store's `AddNoteMeta` / `AddOverview` / `SetOverviewIn` / `RemoveNoteIn` / `RemoveOverviewIn` / `ClearPath` / `Overviews` / `AlignedNotes` / `CheckAnchorsIn` — the same validation prose, minted ids and temp+rename every reader already trusts — then `Close`s it. Showing is a separate step: `add`/`set` post one `file_focus` through `steerLive` unless `--background`, and say nothing when no window is live. The readers lose their note/overview handlers (`internal/tui/steer_file_notes.go`, `steer_overview.go`, `internal/web/steer_notes.go`, `steer_overviews.go`) and answer the retired verbs before any other check, so an old CLI reads the fix in the refusal. `file_focus` of an id the reader has not listed yet reloads the attached root once and retries, closing the write→focus race against the watch. The e2e `overview =` TUI step becomes a document written by hand.

**Tech Stack:** Go 1.26, `internal/agentdocs`, `internal/steer`, `internal/config`, `internal/domain`, the CLI's `runSessionIn` harness, `httptest` for the web, `tui.Headless` + the e2e harness, node for nothing (no page JS changes), the skills under `internal/agentskill`.

**Spec:** `docs/superpowers/specs/2026-10-18-gg-session-docs-design.md` — §3 (shapes, ids), §5 (the write), §6.2 (CLI), §6.3 (steer revision), §6.4 (readers), §7 (skills), §8 (plugin), §10 (rollout, versions), §12 (testing). Plans 1–3: `docs/superpowers/plans/2026-10-18-gg-session-docs-1-core.md`, `2026-10-19-gg-session-docs-2-tui.md`, `2026-10-19-gg-session-docs-3-web.md`.

## Global Constraints

- A verb acts on the directory of the worktree it runs in: `steer.DocsDir(inbox)` where `inbox` is the `sessDir.inbox` every session verb already receives (`config.SessionSteerDir(commonDir, top)`); the store root is `domain.CheckoutKey(top)` and the attached worktree is `top` (`callerWorktree(svc)`). `--worktree` does not exist today and is not added (spec §6.2 says "as today": there is no such flag).
- Ids are file names: `agentdocs.MintID` via the store's `writeNew`; `set`/`show`/`rm` take any `agentdocs.ValidID`. A memory id (`f<n>`, `t<n>`) is just an id that names no file: `no overview f1` / `no note t1`, exit 1.
- Validation prose is the store's (spec §6.2): `a line number is 1-based`, `the range ends before it starts`, `line N is past the end of <path> (M lines)`, `a note needs a summary`, `the summary is longer than 500 characters`, `<path> has no lines to note`, `the title must be one line of at most 200 characters`, `the text is over 64 KiB`. CLI misuse (bad flags, a terminal on stdin, `--title` missing on add) stays exit 2 with the current prose; a refused write is exit 1.
- The retired verbs answer `error: "<verb> is retired: write the file (gg session note add)"` for `note_*` and `"<verb> is retired: write the file (gg session overview add)"` for `overview_*` (the plugin's PLUGIN.md §4 is built against the per-kind text) — on the TUI BEFORE `steerEnumRefusal`, on the web BEFORE `toSteerWire`, with HTTP 200 and `{ok:false,error}` so `steer.PostHTTPReply` hands the text to the CLI.
- Readers never unlink on their own; the CLI unlinks only what the user named (`rm <id>`, `clear <path>`).
- `./test.sh race` must print `all green` before merge. Commit with `gg add <files>` then `git commit -F <msgfile>`; never `git add -A`. Worktree: `/work/gigagit/.claude/worktrees/session-docs-cli` (branch `feat/session-docs-cli`). Never push; no online artifacts.
- Skill versions at merge: `Version` 177 → 178, `GGOverviewVersion` 15 → 16, `GGOverviewV2Version` 2 → 3, `DelegateVersion` 8 → 9 (spec §10's numbers are stale; take the next free ones). `.claude/skills/` dogfood copies are tracked: regenerate them with the worktree's built binary and commit them on the branch.

## Review Focus

1. A write followed at once by a focus: the CLI's `add` posts `file_focus <id>` within microseconds of the rename; a reader whose watch has not fired must still answer `focused overview <id> "<title>"`, never `no open file <id>` — Task 4 pins it on both readers with a file written by hand and a focus sent before any wake.
2. An OLD gg (plan-3 CLI) against a NEW reader: every one of the nine verbs, on the TUI's inbox and over the web's `/api/steer`, must get the naming refusal (not `unknown command`, not `note_add needs a file`, not a 400) — Task 5's corpus cases and the web twin send each verb with its old fields filled.
3. `note add` on a file from a subdirectory of the worktree and on a CRLF file: the path is resolved against the worktree top, not the shell's cwd, and `lines` are the CRLF-stripped text so a reader places the note — Task 2.
4. `--background` with a window live: the file is written, nothing is posted, nothing is printed past the id and its warnings — Task 3 (a fake presence that would fail the test if any command were drained).
5. `docs path` creates nothing: on a fresh state home the directory does not exist afterwards — Task 3.

---

### Task 1: agentdocs — a note remembers when it was written

**Files:**
- Modify: `internal/agentdocs/notes.go:25-46` (the `Note` struct), `internal/agentdocs/attach.go:318-345` (`loadDocLocked`, the note branch)
- Test: `internal/agentdocs/note_created_test.go` (new)

**Interfaces:**
- Produces: `Note.Created time.Time` — the file's `created` (zero for a memory note or a file without one). The CLI's `--json` note carries it (Task 2).

- [ ] **Step 1: Write the failing test**

```go
package agentdocs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A note file's created stamp is kept on the loaded note; a replaced file
// with a new stamp updates it.
func TestANoteKeepsItsCreatedStamp(t *testing.T) {
	t.Parallel()
	s := New()
	defer s.Close()
	wt, dir := t.TempDir(), filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRaw(t, dir, "n.toml", "format = 1\npath = \"a.go\"\nstart = 1\nend = 1\nsummary = \"s\"\ncreated = 2026-01-02T03:04:05Z\nlines = [\"x\"]\n")
	if err := s.Attach("A", wt, dir); err != nil {
		t.Fatal(err)
	}
	n, ok := s.FindNoteIn("A", "n")
	if !ok {
		t.Fatal("note not loaded")
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); !n.Created.Equal(want) {
		t.Fatalf("Created = %v, want %v", n.Created, want)
	}
	writeRaw(t, dir, "n.toml", "format = 1\npath = \"a.go\"\nstart = 1\nend = 1\nsummary = \"s2\"\ncreated = 2026-01-02T03:04:06Z\nlines = [\"x\"]\n")
	s.Reload("A")
	n, _ = s.FindNoteIn("A", "n")
	if n.Summary != "s2" || n.Created.Second() != 6 {
		t.Fatalf("after replace: %+v", n)
	}
}
```

(`writeRaw` is the existing helper in `internal/agentdocs/attach_test.go`; use it as the plan 3 tests do.)

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/agentdocs -run TestANoteKeepsItsCreatedStamp`
Expected: FAIL — `n.Created undefined` (compile error) — the field does not exist.

- [ ] **Step 3: Add the field and fill it on load**

In `notes.go`, after `Author string`:

```go
	Author    string
	Created   time.Time // the file's stamp; zero for a memory note
	Outdated  bool // its lines are gone from the file
```

(add `"time"` to the imports). In `attach.go` `loadDocLocked`'s note branch, both places that copy the file's fields: the `same` branch becomes

```go
			on.Summary, on.Rationale, on.Author, on.Created, on.Meta, on.Display, on.Seq = nf.Summary, nf.Rationale, nf.Author, nf.Created, nf.Meta, nf.Display, seqOf(d)
```

and the new-note literal gains `Created: nf.Created,` after `Author: nf.Author,`.

- [ ] **Step 4: Run the package**

Run: `go test ./internal/agentdocs`
Expected: PASS (the new test and every existing one).

- [ ] **Step 5: Commit**

```bash
gg add internal/agentdocs/notes.go internal/agentdocs/attach.go internal/agentdocs/note_created_test.go
git commit -F <msgfile>   # "agentdocs: a loaded note keeps its created stamp"
```

---

### Task 2: CLI — `gg session note` acts on the files

**Files:**
- Rewrite: `internal/cli/session_note.go`
- Create: `internal/cli/session_docs_store.go` (the shared store opener)
- Modify: `internal/steer/steer.go:202-217` (`FileNote` gains `Created`), `docs/specs/gg-steer/reply.schema.json` (the `notes` item gains `created`, if the items are described — check; the schema is lenient)
- Rewrite: `internal/cli/session_note_test.go`; delete the note cases from `internal/cli/session_docs_ids_test.go` only if they no longer apply (they still do: keep the file).

**Interfaces:**
- Consumes: Task 1's `Note.Created`.
- Produces (used by Task 3):

```go
// docsSession is one verb's view of the caller's session-docs directory.
type docsSession struct {
	store *agentdocs.Store
	root  string // domain.CheckoutKey(top)
	top   string // the worktree on disk
	dir   string // steer.DocsDir(inbox)
}

// openDocs attaches a throw-away store to the caller's directory. Close it.
// An error names the verb's misuse (no worktree, no state home).
func openDocs(verb string, dir sessDir, svc *domain.Service, stderr io.Writer) (*docsSession, bool)
func (d *docsSession) Close()
// readLines reads a working-tree file as the store splits lines; nil when
// missing/unreadable/empty (the store then says "<path> has no lines to note").
func (d *docsSession) readLines(path string) []string
// noteWire is a note as the CLI prints it (the steer shape + created).
func (d *docsSession) noteWire(n agentdocs.Note, text []string) steer.FileNote
```

- `steer.FileNote.Created string json:"created,omitempty"` (RFC 3339 UTC).

- [ ] **Step 1: Write the failing tests**

Replace `internal/cli/session_note_test.go` with:

```go
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// docsCLI is a repo with a.go (3 lines, LF) and sub/b.go (CRLF) committed,
// its service, and a temp inbox whose docs dir the verbs write.
func docsCLI(t *testing.T) (svc *domain.Service, inbox, dir string) {
	t.Helper()
	repo := newCLIRepo(t)
	write := func(rel, content string) {
		p := filepath.Join(repo, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package a\n\nfunc gate() {}\n")
	write("sub/b.go", "one\r\ntwo\r\nthree\r\n")
	inbox = t.TempDir()
	return domain.Open(repo), inbox, steer.DocsDir(inbox)
}

func runNote(t *testing.T, svc *domain.Service, inbox string, args ...string) (code int, out, errb string) {
	t.Helper()
	var o, e bytes.Buffer
	code = runSessionIn(inbox, svc, append([]string{"note"}, args...), nil, &o, &e)
	return code, o.String(), e.String()
}

func TestSessionNoteAddWritesAFileAndNamesIt(t *testing.T) {
	t.Parallel()
	svc, inbox, dir := docsCLI(t)
	code, out, errb := runNote(t, svc, inbox, "add", "a.go:1-2", "--summary", "look", "--rationale", "why", "--author", "claude", "--meta", "kind=design")
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb)
	}
	if !strings.HasPrefix(out, "noted a.go:1-2 as ") {
		t.Fatalf("stdout = %q", out)
	}
	id := strings.TrimSpace(strings.TrimPrefix(out, "noted a.go:1-2 as "))
	if !agentdocs.ValidID(id) || strings.HasPrefix(id, "t") {
		t.Fatalf("id = %q, want a minted document id", id)
	}
	d, err := agentdocs.ReadDoc(dir, id, agentdocs.KindNote)
	if err != nil {
		t.Fatal(err)
	}
	n := d.Note
	if n.Path != "a.go" || n.Start != 1 || n.End != 2 || n.Summary != "look" || n.Rationale != "why" || n.Author != "claude" ||
		len(n.Lines) != 2 || n.Lines[0] != "package a" || n.Lines[1] != "" || len(n.Meta) != 1 || n.Meta[0].Key != "kind" {
		t.Fatalf("file = %+v", n)
	}
	if n.Created.IsZero() {
		t.Fatal("created missing")
	}
}

// The path is repo-relative, in slash form, whatever the shell's cwd; CRLF
// lines are stripped so a reader can place the note.
func TestSessionNoteAddFromASubdirectoryAndCRLF(t *testing.T) {
	t.Parallel()
	svc, inbox, dir := docsCLI(t)
	code, out, errb := runNote(t, svc, inbox, "add", "sub/b.go:2-3", "--summary", "s")
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb)
	}
	id := strings.TrimSpace(strings.TrimPrefix(out, "noted sub/b.go:2-3 as "))
	d, err := agentdocs.ReadDoc(dir, id, agentdocs.KindNote)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(d.Note.Lines, "|"); got != "two|three" {
		t.Fatalf("lines = %q", got)
	}
}

func TestSessionNoteAddRefusals(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	for _, tc := range []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"add", "a.go:9", "--summary", "s"}, 1, "line 9 is past the end of a.go (3 lines)"},
		{[]string{"add", "nope.go:1", "--summary", "s"}, 1, "nope.go has no lines to note"},
		{[]string{"add", "a.go:0", "--summary", "s"}, 2, "a line number is 1-based"},
		{[]string{"add", "a.go:3-2", "--summary", "s"}, 2, "the range ends before it starts"},
		{[]string{"add", "a.go:1"}, 2, "--summary is required"},
		{[]string{"add", "a.go:1", "--summary", strings.Repeat("x", 501)}, 1, "the summary is longer than 500 characters"},
		{[]string{"add", "f3:1", "--summary", "s"}, 1, "f3 has no lines to note"},
		{[]string{"add", "../x:1", "--summary", "s"}, 1, "path must be a relative slash path inside the worktree"},
	} {
		code, _, errb := runNote(t, svc, inbox, tc.args...)
		if code != tc.code || !strings.Contains(errb, tc.msg) {
			t.Errorf("%v: exit %d stderr %q, want %d %q", tc.args, code, errb, tc.code, tc.msg)
		}
	}
}

func TestSessionNoteListShowRmClear(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	_, out1, _ := runNote(t, svc, inbox, "add", "a.go:3", "--summary", "gate")
	_, out2, _ := runNote(t, svc, inbox, "add", "a.go:1", "--summary", "pkg")
	id1 := strings.TrimSpace(strings.TrimPrefix(out1, "noted a.go:3-3 as "))
	id2 := strings.TrimSpace(strings.TrimPrefix(out2, "noted a.go:1-1 as "))
	_, out, _ := runNote(t, svc, inbox, "list")
	want := id2 + "\ta.go\t1-1\tpkg\n" + id1 + "\ta.go\t3-3\tgate\n" // by path, then start
	if out != want {
		t.Fatalf("list = %q, want %q", out, want)
	}
	_, out, _ = runNote(t, svc, inbox, "list", "a.go", "--json")
	var notes []steer.FileNote
	if err := json.Unmarshal([]byte(out), &notes); err != nil || len(notes) != 2 || notes[0].ID != id2 || notes[0].Created == "" {
		t.Fatalf("list --json = %s (%v)", out, err)
	}
	_, out, _ = runNote(t, svc, inbox, "show", id1)
	if !strings.HasPrefix(out, id1+"\ta.go:3-3\tagent\ngate\n\n3\tfunc gate() {}\n") {
		t.Fatalf("show = %q", out)
	}
	code, out, _ := runNote(t, svc, inbox, "rm", id1)
	if code != 0 || out != "removed "+id1+"\n" {
		t.Fatalf("rm: %d %q", code, out)
	}
	code, _, errb := runNote(t, svc, inbox, "rm", id1)
	if code != 1 || !strings.Contains(errb, "no note "+id1) {
		t.Fatalf("rm again: %d %q", code, errb)
	}
	code, out, _ = runNote(t, svc, inbox, "clear", "a.go")
	if code != 0 || out != "removed 1 notes on a.go\n" {
		t.Fatalf("clear: %d %q", code, out)
	}
	code, _, errb = runNote(t, svc, inbox, "clear", "a.go")
	if code != 1 || !strings.Contains(errb, "no notes on a.go") {
		t.Fatalf("clear again: %d %q", code, errb)
	}
	_, out, _ = runNote(t, svc, inbox, "list", "--json")
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("list after clear = %q", out)
	}
}

// A note whose lines moved is listed at its new place; one whose lines are
// gone says (outdated).
func TestSessionNoteListAlignsToTheFileNow(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	_, out, _ := runNote(t, svc, inbox, "add", "a.go:3", "--summary", "gate")
	id := strings.TrimSpace(strings.TrimPrefix(out, "noted a.go:3-3 as "))
	top := callerWorktree(svc)
	if err := os.WriteFile(filepath.Join(top, "a.go"), []byte("// new\npackage a\n\nfunc gate() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out, _ = runNote(t, svc, inbox, "list")
	if out != id+"\ta.go\t4-4\tgate\n" {
		t.Fatalf("moved: %q", out)
	}
	if err := os.WriteFile(filepath.Join(top, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out, _ = runNote(t, svc, inbox, "list")
	if !strings.HasSuffix(out, "gate (outdated)\n") {
		t.Fatalf("outdated: %q", out)
	}
}

// No window needs to be live, and a live one is never asked: the verbs are
// file operations.
func TestSessionNoteNeedsNoWindowAndPostsNothing(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	livePresence(t, inbox)
	posted := make(chan steer.Command, 4)
	go func() { // a consumer that would see anything posted
		for i := 0; i < 30; i++ {
			for _, c := range steer.Drain(inbox) {
				posted <- c
			}
			timeSleep(50)
		}
		close(posted)
	}()
	if code, _, errb := runNote(t, svc, inbox, "add", "a.go:1", "--summary", "s"); code != 0 {
		t.Fatalf("exit = %d (%q)", code, errb)
	}
	for c := range posted {
		t.Fatalf("posted %+v", c)
	}
}
```

Add `func timeSleep(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }` to the test file (or inline `time.Sleep`) — keep the import list honest.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/cli -run 'TestSessionNote' 2>&1 | tail -20`
Expected: FAIL — the add posts `note_add` and dies with `no gg session for this worktree` (exit 1) on every test; `notes[0].Created` is a compile error until `FileNote.Created` exists.

- [ ] **Step 3: The store opener**

Create `internal/cli/session_docs_store.go`:

```go
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// The session docs verbs (gg session note|overview|docs) are FILE
// operations on the caller's worktree directory (spec
// 2026-10-18-gg-session-docs §6.2): no window needs to be live. A verb
// attaches a throw-away agentdocs store to the directory — the same
// validation, minted ids and temp+rename every reader trusts — and closes
// it when done.

type docsSession struct {
	store *agentdocs.Store
	root  string
	top   string
	dir   string
}

// openDocs attaches a store to the caller's session-docs directory; false
// (reason printed) without a worktree or a state home.
func openDocs(verb string, dir sessDir, svc *domain.Service, stderr io.Writer) (*docsSession, bool) {
	if svc == nil {
		fmt.Fprintln(stderr, "session "+verb+": not in a git worktree")
		return nil, false
	}
	top := callerWorktree(svc)
	if top == "" {
		fmt.Fprintln(stderr, "session "+verb+": not in a git worktree")
		return nil, false
	}
	if dir.inbox == "" {
		fmt.Fprintln(stderr, "session "+verb+": no state directory for the session docs")
		return nil, false
	}
	d := &docsSession{store: agentdocs.New(), root: domain.CheckoutKey(top), top: top, dir: steer.DocsDir(dir.inbox)}
	if err := d.store.Attach(d.root, top, d.dir); err != nil {
		fmt.Fprintln(stderr, "session "+verb+": "+err.Error())
		return nil, false
	}
	return d, true
}

func (d *docsSession) Close() { d.store.Close() }

// readLines is a working-tree file as the store splits it; nil when it
// cannot be read (the store then refuses with "<path> has no lines to note").
func (d *docsSession) readLines(path string) []string {
	data, err := os.ReadFile(filepath.Join(d.top, filepath.FromSlash(path)))
	if err != nil {
		return nil
	}
	return agentdocs.Lines(data)
}

// alignedNotes is path's notes placed on the file as it is now.
func (d *docsSession) alignedNotes(path string) []agentdocs.Note {
	if lines := d.readLines(path); len(lines) > 0 {
		return d.store.AlignedNotes(d.root, path, lines)
	}
	ns, _ := d.store.Notes(d.root, path)
	return ns
}

// noteWire is a note as the CLI prints it.
func (d *docsSession) noteWire(n agentdocs.Note, text []string) steer.FileNote {
	w := agentdocs.NoteWire(n, "", text)
	if !n.Created.IsZero() {
		w.Created = n.Created.UTC().Format(time.RFC3339)
	}
	return w
}

var _ = context.Background
```

(drop the `context` import if nothing uses it; `agentdocs.NoteWire` is the existing wire builder in `internal/agentdocs/reply.go` — read it first: it takes `(n Note, fileID string, text []string)`.) Add to `steer.FileNote`:

```go
	Author    string   `json:"author,omitempty"`
	Created   string   `json:"created,omitempty"` // the document's stamp, RFC 3339 UTC (session docs)
```

- [ ] **Step 4: Rewrite `session_note.go`**

```go
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/notemeta"
)

const noteUsage = "usage: gg session note add <path>:<start>[-<end>] --summary \"…\" [--rationale \"…\"] [--author <name>] [--meta k=v]… [--display <json>|@file] [--json]\n" +
	"       gg session note list [<path>] [--json]\n" +
	"       gg session note show <id> [--json]\n" +
	"       gg session note rm <id>\n" +
	"       gg session note clear <path>"

var noteRange = regexp.MustCompile(`^(.+):([0-9]+)(?:-([0-9]+))?$`)

// sessionNote is `gg session note …`: an agent's temporary notes on
// working-tree files, as FILES in the worktree's session-docs directory
// (spec 2026-10-18-gg-session-docs §6.2). Every gg reader of the worktree
// shows them; no window needs to be live.
func sessionNote(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	switch args[0] {
	case "add":
		return sessionNoteAdd(dir, svc, args[1:], stdout, stderr)
	case "list":
		return sessionNoteList(dir, svc, args[1:], stdout, stderr)
	case "show":
		return sessionNoteShow(dir, svc, args[1:], stdout, stderr)
	case "rm":
		return sessionNoteRm(dir, svc, args[1:], stdout, stderr)
	case "clear":
		return sessionNoteClear(dir, svc, args[1:], stdout, stderr)
	}
	fmt.Fprintln(stderr, noteUsage)
	return 2
}

func sessionNoteAdd(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	summary := fs.String("summary", "", "the remark, one short paragraph")
	rationale := fs.String("rationale", "", "the longer explanation")
	author := fs.String("author", "", "who is speaking (default: agent)")
	asJSON := fs.Bool("json", false, "print the new note as JSON")
	var meta metaFlag
	fs.Var(&meta, "meta", "meta key=value (repeatable)")
	display := fs.String("display", "", "display declaration: JSON or @file")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	mt := noteRange.FindStringSubmatch(pos[0])
	if mt == nil {
		fmt.Fprintln(stderr, "session note add: name the lines — <path>:<start>[-<end>]")
		return 2
	}
	start, _ := strconv.Atoi(mt[2])
	end := start
	if mt[3] != "" {
		end, _ = strconv.Atoi(mt[3])
	}
	switch {
	case start < 1:
		fmt.Fprintln(stderr, "session note add: a line number is 1-based")
		return 2
	case end < start:
		fmt.Fprintln(stderr, "session note add: the range ends before it starts")
		return 2
	case strings.TrimSpace(*summary) == "":
		fmt.Fprintln(stderr, "session note add: --summary is required")
		return 2
	}
	kv, ok := checkedMeta("session note add", meta, stderr)
	if !ok {
		return 2
	}
	disp, ok := displayArg("session note add", *display, stderr)
	if !ok {
		return 2
	}
	d, ok := openDocs("note add", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	path := mt[1]
	t := agentdocs.NoteText{Summary: *summary, Rationale: *rationale, Author: *author, Meta: kv}
	if len(disp) > 0 {
		data, _ := json.Marshal(disp)
		if err := json.Unmarshal(data, &t.Display); err != nil {
			fmt.Fprintln(stderr, "session note add: display: "+err.Error())
			return 2
		}
	}
	n, err := d.store.AddNoteMeta(d.root, path, d.readLines(path), start, end, t)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if *asJSON {
		data, _ := json.MarshalIndent(d.noteWire(n, nil), "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintln(stdout, agentdocs.NotedDetail(n, ""))
	return 0
}
```

Read `checkedMeta` and `displayArg` (they exist in `internal/cli`; `displayArg` returns the parsed display as whatever type it has today — if it is already a `notemeta.Display`, pass it straight into `t.Display` and drop the marshal round-trip above; keep the `notemeta` import only if used). `kv` is `[]notemeta.KV` today (the old code iterated `e.Key`/`e.Value`): pass it as `Meta`.

```go
func sessionNoteList(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the notes as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) > 1 {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	d, ok := openDocs("note list", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	paths := d.store.NotedPaths(d.root)
	if len(pos) == 1 {
		paths = []string{pos[0]}
	}
	sort.Strings(paths)
	notes := []steer.FileNote{}
	for _, p := range paths {
		for _, n := range d.alignedNotes(p) {
			notes = append(notes, d.noteWire(n, nil))
		}
	}
	if *asJSON {
		data, _ := json.MarshalIndent(notes, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	for _, n := range notes {
		sum := n.Summary
		if n.Outdated {
			sum += " (outdated)"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%d-%d\t%s\n", n.ID, n.Path, n.Start, n.End, sum)
	}
	return 0
}

func sessionNoteShow(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the note as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || !agentdocs.ValidID(pos[0]) {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	d, ok := openDocs("note show", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	n, found := d.store.FindNoteIn(d.root, pos[0])
	if !found {
		fmt.Fprintln(stderr, "no note "+pos[0])
		return 1
	}
	if lines := d.readLines(n.Path); len(lines) > 0 {
		d.store.Align(d.root, n.Path, lines) // place it on the file as it is now
		n, _ = d.store.FindNoteIn(d.root, pos[0])
	}
	w := d.noteWire(n, d.store.NoteTextIn(d.root, pos[0]))
	if *asJSON {
		data, _ := json.MarshalIndent(w, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	head := fmt.Sprintf("%s\t%s:%d-%d\t%s", w.ID, w.Path, w.Start, w.End, w.Author)
	if w.Outdated {
		head += "\t(outdated)"
	}
	fmt.Fprintln(stdout, head)
	fmt.Fprintln(stdout, w.Summary)
	if w.Rationale != "" {
		fmt.Fprintf(stdout, "\n%s\n", w.Rationale)
	}
	if len(w.Text) > 0 {
		fmt.Fprintln(stdout)
		for i, l := range w.Text {
			fmt.Fprintf(stdout, "%d\t%s\n", w.Start+i, l)
		}
	}
	return 0
}

func sessionNoteRm(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || !agentdocs.ValidID(pos[0]) {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	d, ok := openDocs("note rm", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	if !d.store.RemoveNoteIn(d.root, pos[0]) {
		fmt.Fprintln(stderr, "no note "+pos[0])
		return 1
	}
	fmt.Fprintln(stdout, "removed "+pos[0])
	return 0
}

// sessionNoteClear unlinks every note on one path.
func sessionNoteClear(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note clear", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	d, ok := openDocs("note clear", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	n := d.store.ClearPath(d.root, pos[0])
	if n == 0 {
		fmt.Fprintln(stderr, "no notes on "+pos[0])
		return 1
	}
	fmt.Fprintf(stdout, "removed %d notes on %s\n", n, pos[0])
	return 0
}
```

Add `"sort"` and the `steer` import. Remove `fileTarget` and `steerNotes` (Task 3 removes `steerOverviews`; `fileIDPattern` stays in `session_files.go` for `files focus`). Check `RemoveNoteIn`'s return on an attached root (plan 1: true when the file was unlinked) and `ClearPath`'s count (the number it unlinked).

- [ ] **Step 5: Run the note tests**

Run: `go test ./internal/cli -run 'TestSessionNote|TestSessionVerbsAcceptDocumentIDs' 2>&1 | tail -20`
Expected: PASS. If `TestSessionNoteAddRefusals`'s `f3:1` case says something other than `f3 has no lines to note`, read what the store said and fix the test's expectation only if the store's prose is the spec's (the file does not exist → "has no lines to note").

- [ ] **Step 6: Run the whole CLI package and the steer schema test**

Run: `go test ./internal/cli ./internal/steer 2>&1 | tail -20`
Expected: PASS except tests that still drive `note_add`/`overview_*` through a fake consumer (`session_ide_test.go`, `session_to_test.go`, `session_overview_test.go`): those are Task 3's and Task 5's; list the failing names in the ledger and move on only if every failure is one of them.

- [ ] **Step 7: Commit**

```bash
gg add internal/cli/session_note.go internal/cli/session_docs_store.go internal/cli/session_note_test.go internal/steer/steer.go
git commit -F <msgfile>   # "cli: gg session note writes, lists and unlinks the session-docs files"
```

---

### Task 3: CLI — `gg session overview` on the files, `export` from the directory, `gg session docs path|ls`

**Files:**
- Rewrite: `internal/cli/session_overview.go`
- Create: `internal/cli/session_docs.go` (`docs path|ls`)
- Modify: `internal/cli/session.go:63-110` (dispatch `docs`; usage line), `internal/steer/steer.go:191-198` (`Overview` gains `Author`, `Created`)
- Rewrite: `internal/cli/session_overview_test.go`; fix `internal/cli/session_to_test.go` and `session_ide_test.go` cases that posted `overview_add`/`note_add` through `--to` (they become `file_focus`-based or use `navigate`; read each case: the point of those tests is routing, so switch the verb to `files focus` with a fake reply).
- Delete: `internal/cli/session_overview_tty_test.go` only if it tests the stdin-terminal refusal through a steer; keep it if it only checks `readerIsTerminal` (read it).

**Interfaces:**
- Consumes: Task 2's `openDocs`, `docsSession`.
- Produces: the `focusAfterWrite(dir sessDir, id string, stdout, stderr io.Writer)` helper (below); `steer.Overview.Author string json:"author,omitempty"`, `Created string json:"created,omitempty"`.

- [ ] **Step 1: Write the failing tests**

Replace `internal/cli/session_overview_test.go` with:

```go
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

func runOverview(t *testing.T, svc *domain.Service, inbox, stdin string, args ...string) (code int, out, errb string) {
	t.Helper()
	var o, e bytes.Buffer
	var in io.Reader
	if stdin != "" {
		in = strings.NewReader(stdin)
	}
	code = runSessionIn(inbox, svc, append([]string{"overview"}, args...), in, &o, &e)
	return code, o.String(), e.String()
}

// add writes <id>.md with the front matter gg's encoder spells, prints the
// id, the unresolved anchors and the backtick/no-anchor warnings; with no
// window live it prints nothing more and exits 0.
func TestSessionOverviewAddWritesAFile(t *testing.T) {
	t.Parallel()
	svc, inbox, dir := docsCLI(t)
	code, out, errb := runOverview(t, svc, inbox, "See [gate](a.go:3) and [gone](nope.go) and `[x](a.go:1)`.\n", "add", "--title", "Tour")
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	id := lines[0]
	if !agentdocs.ValidID(id) || strings.HasPrefix(id, "f") {
		t.Fatalf("first line = %q, want a minted id", id)
	}
	if lines[1] != "unresolved: nope.go" || !strings.HasPrefix(lines[2], "in backticks: ") || len(lines) != 3 {
		t.Fatalf("stdout = %q", out)
	}
	d, err := agentdocs.ReadDoc(dir, id, agentdocs.KindOverview)
	if err != nil {
		t.Fatal(err)
	}
	if d.Over.Title != "Tour" || d.Over.Author != "agent" || d.Over.Created.IsZero() || !strings.Contains(d.Over.Text, "[gate](a.go:3)") {
		t.Fatalf("file = %+v", d.Over)
	}
}

// With a window live, add ends by asking it to show the id (file_focus) and
// prints the reply's detail; --background writes the same file and posts
// nothing.
func TestSessionOverviewAddFocusesTheLiveWindowUnlessBackground(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	livePresence(t, inbox)
	seen := answer(t, inbox, func(c steer.Command) steer.Reply {
		return steer.Reply{ID: c.ID, OK: true, Detail: "focused overview " + c.FileID + " \"T\""}
	})
	code, out, errb := runOverview(t, svc, inbox, "[a](a.go:1)\n", "add", "--title", "T")
	if code != 0 {
		t.Fatalf("exit = %d (%q)", code, errb)
	}
	c := <-seen
	id := strings.Split(out, "\n")[0]
	if c.Cmd != "file_focus" || c.FileID != id || !c.Wait {
		t.Fatalf("posted %+v", c)
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "focused overview "+id+" \"T\"") {
		t.Fatalf("stdout = %q", out)
	}
	seen = answer(t, inbox, func(c steer.Command) steer.Reply { t.Errorf("posted %+v", c); return steer.Reply{ID: c.ID, OK: true} })
	code, out, _ = runOverview(t, svc, inbox, "[a](a.go:1)\n", "add", "--title", "T", "--background")
	if code != 0 || len(strings.Split(strings.TrimSpace(out), "\n")) != 1 {
		t.Fatalf("background: %d %q", code, out)
	}
	select {
	case c, ok := <-seen:
		if ok {
			t.Fatalf("background posted %+v", c)
		}
	case <-time.After(3500 * time.Millisecond):
	}
}

func TestSessionOverviewSetListShowRm(t *testing.T) {
	t.Parallel()
	svc, inbox, dir := docsCLI(t)
	_, out, _ := runOverview(t, svc, inbox, "[a](a.go:1)\n", "add", "--title", "First", "--background")
	id1 := strings.TrimSpace(out)
	_, out, _ = runOverview(t, svc, inbox, "[a](a.go:1) [b](a.go:2)\n", "add", "--title", "Second", "--background")
	id2 := strings.TrimSpace(out)
	code, out, _ := runOverview(t, svc, inbox, "[c](a.go:3)\n", "set", id1, "--background")
	if code != 0 || strings.TrimSpace(out) != id1 {
		t.Fatalf("set: %d %q", code, out)
	}
	d, _ := agentdocs.ReadDoc(dir, id1, agentdocs.KindOverview)
	if d.Over.Title != "First" || !strings.Contains(d.Over.Text, "[c](a.go:3)") {
		t.Fatalf("set kept the title and replaced the text: %+v", d.Over)
	}
	code, _, errb := runOverview(t, svc, inbox, "x\n", "set", "20260101T000000-0000", "--background")
	if code != 1 || !strings.Contains(errb, "no overview 20260101T000000-0000") {
		t.Fatalf("set missing: %d %q", code, errb)
	}
	_, out, _ = runOverview(t, svc, inbox, "", "list")
	if out != id2+"\t2 anchors\tSecond\n"+id1+"\t1 anchors\tFirst\n" { // newest first
		t.Fatalf("list = %q", out)
	}
	_, out, _ = runOverview(t, svc, inbox, "", "list", "--json")
	var ovs []steer.Overview
	if err := json.Unmarshal([]byte(out), &ovs); err != nil || len(ovs) != 2 || ovs[0].Author != "agent" || ovs[0].Created == "" || ovs[0].Anchors != 2 {
		t.Fatalf("list --json = %s (%v)", out, err)
	}
	_, out, _ = runOverview(t, svc, inbox, "", "show", id1)
	if !strings.HasPrefix(out, id1+"\tFirst\tagent\t") || !strings.HasSuffix(out, "\n\n[c](a.go:3)\n") {
		t.Fatalf("show = %q", out)
	}
	code, out, _ = runOverview(t, svc, inbox, "", "rm", id1)
	if code != 0 || out != "removed "+id1+"\n" {
		t.Fatalf("rm: %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, id1+".md")); !os.IsNotExist(err) {
		t.Fatal("rm left the file")
	}
	code, _, errb = runOverview(t, svc, inbox, "", "show", id1)
	if code != 1 || !strings.Contains(errb, "no overview "+id1) {
		t.Fatalf("show after rm: %d %q", code, errb)
	}
}

func TestSessionOverviewMisuse(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	for _, tc := range []struct {
		stdin string
		args  []string
		msg   string
	}{
		{"x\n", []string{"add"}, "--title is required"},
		{"", []string{"add", "--title", "T"}, "give the text with --file or on stdin"},
		{"\n", []string{"add", "--title", "T"}, "the text is empty"},
		{"x\n", []string{"add", "--title", strings.Repeat("t", 201)}, "the title must be one line"},
		{"x\n", []string{"set", "a/b"}, "usage: gg session overview"},
		{"", []string{"show", "a.md"}, "usage: gg session overview"},
	} {
		code, _, errb := runOverview(t, svc, inbox, tc.stdin, tc.args...)
		if code != 2 || !strings.Contains(errb, tc.msg) {
			t.Errorf("%v: exit %d stderr %q, want 2 %q", tc.args, code, errb, tc.msg)
		}
	}
}

// export reads the overview and the notes from the directory (no window).
func TestSessionOverviewExportReadsTheDirectory(t *testing.T) {
	t.Parallel()
	svc, inbox, _ := docsCLI(t)
	_, out, _ := runNote(t, svc, inbox, "add", "a.go:3", "--summary", "gate")
	nid := strings.TrimSpace(strings.TrimPrefix(out, "noted a.go:3-3 as "))
	_, out, _ = runOverview(t, svc, inbox, "[why](note:"+nid+")\n", "add", "--title", "Tour", "--background")
	id := strings.TrimSpace(out)
	code, out, errb := runOverview(t, svc, inbox, "", "export", id, "--out", "-")
	if code != 0 {
		t.Fatalf("exit = %d (%q)", code, errb)
	}
	if !strings.Contains(out, "<html") || !strings.Contains(out, "Tour") || !strings.Contains(out, "func gate() {}") {
		t.Fatalf("export = %.200q…", out)
	}
}

func TestSessionDocsPathAndLs(t *testing.T) {
	t.Parallel()
	svc, inbox, dir := docsCLI(t)
	var o, e bytes.Buffer
	if code := runSessionIn(inbox, svc, []string{"docs", "path"}, nil, &o, &e); code != 0 || strings.TrimSpace(o.String()) != dir {
		t.Fatalf("path: %d %q %q", code, o.String(), e.String())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("docs path created the directory")
	}
	o.Reset()
	if code := runSessionIn(inbox, svc, []string{"docs", "ls"}, nil, &o, &e); code != 0 || o.String() != "" {
		t.Fatalf("ls of nothing: %d %q", code, o.String())
	}
	_, out, _ := runNote(t, svc, inbox, "add", "a.go:1", "--summary", "pkg")
	nid := strings.TrimSpace(strings.TrimPrefix(out, "noted a.go:1-1 as "))
	_, out, _ = runOverview(t, svc, inbox, "[a](a.go:1)\n", "add", "--title", "T", "--background")
	oid := strings.TrimSpace(out)
	o.Reset()
	if code := runSessionIn(inbox, svc, []string{"docs", "ls"}, nil, &o, &e); code != 0 {
		t.Fatalf("ls: %d %q", code, e.String())
	}
	want := "overview\t" + oid + "\t1 anchors\tT\nnote\t" + nid + "\ta.go\t1-1\tpkg\n"
	if o.String() != want {
		t.Fatalf("ls = %q, want %q", o.String(), want)
	}
	o.Reset()
	if code := runSessionIn(inbox, svc, []string{"docs", "ls", "--json"}, nil, &o, &e); code != 0 {
		t.Fatalf("ls --json: %d", code)
	}
	var got struct {
		Dir       string           `json:"dir"`
		Overviews []steer.Overview `json:"overviews"`
		Notes     []steer.FileNote `json:"notes"`
	}
	if err := json.Unmarshal(o.Bytes(), &got); err != nil || got.Dir != dir || len(got.Overviews) != 1 || len(got.Notes) != 1 {
		t.Fatalf("ls --json = %s (%v)", o.String(), err)
	}
}
```

(add `"io"` to the imports.) The `--to` routing tests (`session_to_test.go`, `session_ide_test.go`): read each case that posts `overview_add`/`note_add`; change the verb to `files focus f1` with the same fake-reply shape so the routing assertion survives (the point is where the command lands, not which verb).

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/cli -run 'TestSessionOverview|TestSessionDocs' 2>&1 | tail -30`
Expected: FAIL — `add` posts `overview_add` and exits 1 (`no gg session`), `docs` is `session: unknown subcommand "docs"`, `ovs[0].Author` does not compile.

- [ ] **Step 3: `steer.Overview` fields**

```go
type Overview struct {
	ID         string   `json:"id"` // a document id (its file name); a memory overview's "f<n>"
	Title      string   `json:"title"`
	State      string   `json:"state,omitempty"` // "shown" | "background" — a reader's answer; the CLI's listing has none
	Anchors    int      `json:"anchors"`
	Unresolved []string `json:"unresolved,omitempty"`
	Text       string   `json:"text,omitempty"`
	Author     string   `json:"author,omitempty"`  // the document's front matter
	Created    string   `json:"created,omitempty"` // RFC 3339 UTC
}
```

(`state` becoming `omitempty` changes no reader: both fill it.) Check `TestWireShapesMatchSchemas` in `internal/steer` still passes (the schema is lenient).

- [ ] **Step 4: Rewrite `session_overview.go`**

```go
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

const overviewUsage = "usage: gg session overview add --title \"…\" [--file <path>] [--background] [--json]   (text from --file, else stdin)\n" +
	"       gg session overview set <id> [--title \"…\"] [--file <path>] [--background] [--json]\n" +
	"       gg session overview list [--json]\n" +
	"       gg session overview show <id> [--json]\n" +
	"       gg session overview rm <id>\n" +
	"       gg session overview export <id> [--out <path>|-] [--force]   (one self-contained HTML file)"

const overviewMaxBytes = agentdocs.MaxOverviewBytes

// sessionOverview is `gg session overview …`: an agent's markdown overview
// whose links are anchors, as a FILE in the worktree's session-docs
// directory (spec 2026-10-18-gg-session-docs §6.2). add and set end by
// asking a live window to show it (file_focus) unless --background.
func sessionOverview(dir sessDir, svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	switch args[0] {
	case "add", "set":
		return sessionOverviewWrite(dir, svc, args[0], args[1:], stdin, stdout, stderr)
	case "list":
		return sessionOverviewList(dir, svc, args[1:], stdout, stderr)
	case "show":
		return sessionOverviewShow(dir, svc, args[1:], stdout, stderr)
	case "rm":
		return sessionOverviewRm(dir, svc, args[1:], stdout, stderr)
	case "export":
		return sessionOverviewExport(dir, svc, args[1:], stdout, stderr)
	}
	fmt.Fprintln(stderr, overviewUsage)
	return 2
}

// overviewWire is an overview as the CLI prints it (anchors checked by the
// caller; unresolved = the destinations marked missing).
func overviewWire(o agentdocs.Overview, text bool) steer.Overview {
	w := steer.Overview{ID: o.ID, Title: o.Title, Anchors: len(o.Anchors), Author: o.Author}
	for _, a := range o.Anchors {
		if a.Missing {
			w.Unresolved = append(w.Unresolved, a.Dest)
		}
	}
	if !o.Created.IsZero() {
		w.Created = o.Created.UTC().Format(time.RFC3339)
	}
	if text {
		w.Text = o.Text
	}
	return w
}

// sessionOverviewWrite is add and set: the text from --file, else stdin.
func sessionOverviewWrite(dir sessDir, svc *domain.Service, verb string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session overview "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", "", "the overview's name, one line")
	file := fs.String("file", "", "read the markdown from this file (default: stdin)")
	background := fs.Bool("background", false, "write it without showing it")
	asJSON := fs.Bool("json", false, "print the overview as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	misuse := func(msg string) int {
		fmt.Fprintln(stderr, "session overview "+verb+": "+msg)
		return 2
	}
	if (verb == "add" && len(pos) != 0) || (verb == "set" && (len(pos) != 1 || !agentdocs.ValidID(pos[0]))) {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	if verb == "add" && strings.TrimSpace(*title) == "" {
		return misuse("--title is required")
	}
	if strings.ContainsAny(*title, "\r\n") || utf8.RuneCountInString(*title) > agentdocs.MaxOverviewTitle {
		return misuse("the title must be one line of at most 200 characters")
	}
	var src io.Reader = stdin
	if *file != "" {
		f, err := os.Open(*file)
		if err != nil {
			return misuse(err.Error())
		}
		defer f.Close()
		src = f
	}
	if src == nil || readerIsTerminal(src) {
		return misuse("give the text with --file or on stdin")
	}
	data, err := io.ReadAll(io.LimitReader(src, overviewMaxBytes+1))
	switch {
	case err != nil:
		return misuse(err.Error())
	case len(data) > overviewMaxBytes:
		return misuse("the text is over 64 KiB")
	case strings.TrimSpace(string(data)) == "":
		return misuse("the text is empty")
	}
	d, ok := openDocs("overview "+verb, dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	var o agentdocs.Overview
	if verb == "add" {
		o, err = d.store.AddOverview(d.root, d.top, *title, string(data))
	} else {
		o, err = d.store.SetOverviewIn(d.root, pos[0], *title, string(data))
	}
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	d.store.CheckAnchorsIn(d.root, o.ID)
	o, _ = d.store.OverviewIn(d.root, o.ID)
	w := overviewWire(o, false)
	if *asJSON {
		data, _ := json.MarshalIndent(w, "", "  ")
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprintln(stdout, w.ID)
		for _, u := range w.Unresolved {
			fmt.Fprintln(stdout, "unresolved: "+u)
		}
		backticked, anchors := domain.OverviewTextProblems(string(data))
		for _, b := range backticked {
			fmt.Fprintln(stdout, "in backticks: "+b)
		}
		if anchors == 0 {
			fmt.Fprintln(stdout, noAnchorWarning)
		}
	}
	if *background {
		return 0
	}
	focusAfterWrite(dir, w.ID, stdout, stderr)
	return 0
}

// focusAfterWrite asks the live window to show the document just written
// (spec §6.2 "Showing it"): a file_focus routed as any bare verb, its reply
// printed. No live window: nothing is printed, the file waits for the next
// reader. A refusal is printed but never fails the write (the file is there).
func focusAfterWrite(dir sessDir, id string, stdout, stderr io.Writer) {
	if _, _, why := dir.target(); why != "" {
		return
	}
	rep, _, ok := steerLive(dir, steer.Command{Cmd: "file_focus", FileID: id}, false, false, stdout, stderr)
	switch {
	case !ok:
	case !rep.OK:
		fmt.Fprintln(stderr, rep.Error)
	case rep.Detail != "":
		fmt.Fprintln(stdout, rep.Detail)
	}
}

func sessionOverviewList(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session overview list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the overviews as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 0 {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	d, ok := openDocs("overview list", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	list := overviewsNewestFirst(d)
	if *asJSON {
		data, _ := json.MarshalIndent(list, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	for _, o := range list {
		fmt.Fprintf(stdout, "%s\t%d anchors\t%s\n", o.ID, o.Anchors, o.Title)
	}
	return 0
}

// overviewsNewestFirst is the directory's overviews, anchors checked, newest
// first (Overviews lists oldest first).
func overviewsNewestFirst(d *docsSession) []steer.Overview {
	ovs := d.store.Overviews(d.root)
	list := make([]steer.Overview, 0, len(ovs))
	for i := len(ovs) - 1; i >= 0; i-- {
		d.store.CheckAnchorsIn(d.root, ovs[i].ID)
		o, _ := d.store.OverviewIn(d.root, ovs[i].ID)
		list = append(list, overviewWire(o, false))
	}
	return list
}

func sessionOverviewShow(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session overview show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the overview as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || !agentdocs.ValidID(pos[0]) {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	d, ok := openDocs("overview show", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	o, found := d.store.OverviewIn(d.root, pos[0])
	if !found {
		fmt.Fprintln(stderr, "no overview "+pos[0])
		return 1
	}
	d.store.CheckAnchorsIn(d.root, o.ID)
	o, _ = d.store.OverviewIn(d.root, o.ID)
	w := overviewWire(o, true)
	if *asJSON {
		data, _ := json.MarshalIndent(w, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n\n%s\n", w.ID, w.Title, w.Author, w.Created, strings.TrimRight(w.Text, "\n"))
	return 0
}

func sessionOverviewRm(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session overview rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || !agentdocs.ValidID(pos[0]) {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	d, ok := openDocs("overview rm", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	if !d.store.RemoveOverviewIn(d.root, pos[0]) {
		fmt.Fprintln(stderr, "no overview "+pos[0])
		return 1
	}
	fmt.Fprintln(stdout, "removed "+pos[0])
	return 0
}

// sessionOverviewExport writes an overview as one self-contained HTML file:
// its text and its note anchors' spans from the directory, the files read
// from this worktree.
func sessionOverviewExport(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session overview export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "write here (default: the export dir); - = stdout")
	force := fs.Bool("force", false, "overwrite an existing file")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || !agentdocs.ValidID(pos[0]) || svc == nil {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	d, ok := openDocs("overview export", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	o, found := d.store.OverviewIn(d.root, pos[0])
	if !found {
		fmt.Fprintln(stderr, "no overview "+pos[0])
		return 1
	}
	src := domain.OverviewSource{Title: o.Title, Text: o.Text, NoteSpans: map[string]domain.NoteSpan{}}
	for _, p := range d.store.NotedPaths(d.root) {
		for _, n := range d.alignedNotes(p) {
			src.NoteSpans[n.ID] = domain.NoteSpan{Path: n.Path, Start: n.Start, End: n.End}
		}
	}
	ctx := context.Background()
	page, name, err := svc.ExportOverviewHTML(ctx, src)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return writeExport(ctx, svc, "session overview export", page, name, *out, *force, stdout, stderr)
}
```

Remove `overviewCaller` and `steerOverviews`. `dir.target()` is the existing `sessDir` method `steerLive` calls (`dir, r, why := d.target()`): check its signature and adapt the discard pattern.

- [ ] **Step 5: `gg session docs path|ls`**

Create `internal/cli/session_docs.go`:

```go
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

const docsUsage = "usage: gg session docs path\n" +
	"       gg session docs ls [--json]"

// sessionDocs is `gg session docs …`: the worktree's session-docs
// directory itself (spec 2026-10-18-gg-session-docs §6.2) — path for an
// agent that writes the files directly, ls for everything in it.
func sessionDocs(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, docsUsage)
		return 2
	}
	switch args[0] {
	case "path":
		if len(args) != 1 {
			fmt.Fprintln(stderr, docsUsage)
			return 2
		}
		if dir.inbox == "" {
			fmt.Fprintln(stderr, "session docs path: no state directory for the session docs")
			return 1
		}
		fmt.Fprintln(stdout, steer.DocsDir(dir.inbox)) // creates nothing
		return 0
	case "ls":
		return sessionDocsLs(dir, svc, args[1:], stdout, stderr)
	}
	fmt.Fprintln(stderr, docsUsage)
	return 2
}

func sessionDocsLs(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session docs ls", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the directory as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 0 {
		fmt.Fprintln(stderr, docsUsage)
		return 2
	}
	d, ok := openDocs("docs ls", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	ovs := overviewsNewestFirst(d)
	paths := d.store.NotedPaths(d.root)
	sort.Strings(paths)
	notes := []steer.FileNote{}
	for _, p := range paths {
		for _, n := range d.alignedNotes(p) {
			notes = append(notes, d.noteWire(n, nil))
		}
	}
	if *asJSON {
		data, _ := json.MarshalIndent(struct {
			Dir       string           `json:"dir"`
			Overviews []steer.Overview `json:"overviews"`
			Notes     []steer.FileNote `json:"notes"`
		}{d.dir, ovs, notes}, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	for _, o := range ovs {
		fmt.Fprintf(stdout, "overview\t%s\t%d anchors\t%s\n", o.ID, o.Anchors, o.Title)
	}
	for _, n := range notes {
		sum := n.Summary
		if n.Outdated {
			sum += " (outdated)"
		}
		fmt.Fprintf(stdout, "note\t%s\t%s\t%d-%d\t%s\n", n.ID, n.Path, n.Start, n.End, sum)
	}
	return 0
}
```

In `session.go` `runSessionIn`: usage line gains `docs`; add `case "docs": return sessionDocs(dir, svc, args[1:], stdout, stderr)`. Check `Attach` on a missing directory: `openDocs` for `ls` on a fresh state home must not create the directory either — if `Attach` MkdirAll's (plan 3 said "makes the dir"), `docs ls` of nothing still prints nothing; the "creates nothing" guarantee is `path`'s (the test pins `path`, then `ls`, in that order — `ls` after the stat). If `Attach` creates the dir, that is acceptable (ledger it) because `ls` is a reader like any other.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/cli ./internal/steer 2>&1 | tail -30`
Expected: PASS for every `TestSessionOverview*`, `TestSessionDocs*`, `TestSessionNote*`, the `--to` and ide routing tests you rewrote; remaining failures must be none (list any and fix before the commit).

- [ ] **Step 7: Commit**

```bash
gg add internal/cli/session_overview.go internal/cli/session_docs.go internal/cli/session.go internal/cli/session_overview_test.go internal/cli/session_to_test.go internal/cli/session_ide_test.go internal/steer/steer.go
git commit -F <msgfile>   # "cli: gg session overview writes the files and focuses the live window; gg session docs path|ls"
```

---

### Task 4: readers — `file_focus` of an id the watch has not delivered reloads first

**Files:**
- Modify: `internal/tui/steer_files.go:86-95` (`steerFileFocus`), `internal/web/steer_files.go:100-110` (`steerFileFocus`)
- Test: `internal/tui/session_docs_focus_test.go` (new), `internal/web/session_docs_focus_test.go` (new)

**Interfaces:**
- Consumes: `agentdocs.Store.Reload(root)`, the TUI's `syncOverviews()` (`agentdocs_track.go:64`), the web's `followDocs()` (`agentdocs_follow.go:87`), the fixtures `attachedDocsModel(t)` (tui) and `attachedSrv(t)` + `writeDoc` (web).

- [ ] **Step 1: Write the failing tests**

`internal/tui/session_docs_focus_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/steer"
)

// gg session overview add writes the file and posts file_focus at once:
// the focus must find the document before the store's watch has fired.
func TestFileFocusOfAJustWrittenOverviewReloadsFirst(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	inbox := t.TempDir()
	m.steerDir = inbox // the field steerModel(t) sets; read steer_test.go
	content := "+++\ntitle = \"Fresh\"\nauthor = \"agent\"\ncreated = 2026-01-01T00:00:00Z\n+++\nSee [a](a.txt:1).\n"
	if err := os.WriteFile(filepath.Join(dir, "20260101T000000-ffff.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	c := steer.Command{ID: "c1", Cmd: "file_focus", FileID: "20260101T000000-ffff", Wait: true}
	_, cmd := m.applySteer(c)
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(inbox, "c1", time.Second)
	if !ok || !r.OK || r.Detail != "focused overview 20260101T000000-ffff \"Fresh\"" {
		t.Fatalf("reply = %+v (ok %v)", r, ok)
	}
}
```

(Read `attachedDocsModel` and `steerModel`: the former may already give a model whose `answerSteer` writes into a dir; use whichever field/dir they set, and keep the assertion on the reply. The exact detail prose is `steerFileFocus`'s `"focused " + agentdocs.OverviewName(id, title)`.)

`internal/web/session_docs_focus_test.go`:

```go
package web

import (
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

func TestWebFileFocusOfAJustWrittenOverviewReloadsFirst(t *testing.T) {
	t.Parallel()
	s, _, dir := attachedSrv(t)
	writeDoc(t, dir, "20260101T000000-ffff.md", "+++\ntitle = \"Fresh\"\nauthor = \"agent\"\ncreated = 2026-01-01T00:00:00Z\n+++\nSee [a](a.txt:1).\n")
	r := postSteer(t, s, steer.Command{ID: "c1", Cmd: "file_focus", FileID: "20260101T000000-ffff", Wait: true})
	if !r.OK || !strings.HasPrefix(r.Detail, "focused overview 20260101T000000-ffff \"Fresh\"") {
		t.Fatalf("reply = %+v", r)
	}
}
```

(`postSteer` — use the helper plan 3's `session_docs_web_test.go` posts `/api/steer` with; if it is named differently, use that name. `attachedSrv` must not have run a follow pass between `writeDoc` and the post: `writeDoc` writes by hand and the test posts at once; if the fixture's follow goroutine races it, the test may pass without the fix — so ALSO run it with the fix removed (Step 2) and make sure it fails at least 3 of 5 runs; if it never fails, write the file with the server's docs watch stopped (`s.docsStop` or whichever handle `startDocsFollow` keeps) and ledger the fixture.)

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/tui -run TestFileFocusOfAJustWrittenOverviewReloadsFirst -count=3; go test ./internal/web -run TestWebFileFocusOfAJustWrittenOverviewReloadsFirst -count=5`
Expected: FAIL — `no open file 20260101T000000-ffff` (the TUI deterministically: nothing delivered `agentDocsChangedMsg`; the web in most runs).

- [ ] **Step 3: Reload-and-retry on both readers**

TUI `steerFileFocus`, replacing the `if d == nil {` refusal:

```go
	d := m.findOpenFile(c.FileID, c.File)
	if d == nil && m.docs != nil {
		// A document written a moment ago (gg session overview add posts
		// file_focus right after the rename): the watch may not have fired.
		if id := c.FileID; id == "" {
			id = c.File
		}
		if agentdocs.ValidID(id) {
			m.docs.Reload(domain.CheckoutKey(m.currentWorktree))
			m = m.syncOverviews()
			d = m.findOpenFile(c.FileID, c.File)
		}
	}
	if d == nil {
		…unchanged refusal…
	}
```

(fix the shadowed `id` — write it as two statements.) Web `steerFileFocus`:

```go
	f, ok := s.ofs.resolve(wt, c.FileID, c.File)
	if !ok {
		name := c.FileID
		if name == "" {
			name = c.File
		}
		if agentdocs.ValidID(name) {
			s.docs.Reload(s.docsRoot(ctx))
			s.followDocs()
			f, ok = s.ofs.resolve(wt, c.FileID, c.File)
		}
	}
	if !ok {
		…unchanged refusal…
	}
```

`followDocs` takes `followMu`; make sure `steerFileFocus` does not already hold it (it does not — it is an HTTP handler).

- [ ] **Step 4: Run the tests and both packages**

Run: `go test ./internal/tui -run 'TestFileFocus|TestSteer' ; go test ./internal/web -run 'TestWebFileFocus|TestSteer|TestSessionDocs'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui/steer_files.go internal/web/steer_files.go internal/tui/session_docs_focus_test.go internal/web/session_docs_focus_test.go
git commit -F <msgfile>   # "readers: file_focus of a document the watch has not delivered reloads the directory first"
```

---

### Task 5: the steer verbs retire

**Files:**
- Modify: `internal/tui/steer.go:354-382, 425-429` (refusal + dispatch), `internal/tui/model.go:1096-1098` (the two msg cases), `internal/tui/headless.go:128-135` (delete `AddOverview`), `internal/tui/open_file_notes.go` (one `note_` mention — read it; probably prose)
- Delete: `internal/tui/steer_file_notes.go`, `internal/tui/steer_overview.go`, `internal/tui/steer_file_notes_test.go`, `internal/tui/steer_overview_test.go`
- Modify: `internal/web/steer.go:357-368` (replace the two `is*Verb` gates with the refusal), `internal/web/notes.go`, `internal/web/reviews.go` (read the mentions; likely MCP/review prose, leave if so)
- Delete: `internal/web/steer_notes.go`, `internal/web/steer_overviews.go`; rewrite or delete the tests that drove them: `internal/web/agentdocs_web_test.go` (16 mentions), `internal/web/agentdocs_overview_web_test.go` (14), `internal/web/session_docs_web_test.go` (8), `internal/web/overviews_http_test.go` (1), `prnotes_reviews_test.go` (1), `reviews_test.go` (1); TUI: `session_docs_test.go` (1), `steer_files_test.go` (1)
- Create: `internal/steer/retired.go` (the one place the verb list and the prose live), `internal/web/steer_retired_test.go`
- Modify: `internal/steer/steer.go:106` (the `Cmd` comment), `internal/steer/steer_test.go` + `schema_test.go` mentions, `docs/specs/gg-steer/command.schema.json` (`cmd` description), `docs/specs/gg-steer/steer-cases.json` (the "note on nothing" case → `retired`; add an overview one), `docs/specs/gg-steer/README.md` (a line on `retired` cases)
- Modify: `e2e/scenario.go:44` + `e2e/tui.go:41-46` (the `overview =` step goes; `tui_overview_anchor_bands.toml` uses a `doc =` step; regenerate its golden screens with `-update` and READ them)

**Interfaces:**
- Produces: `steer.RetiredVerb(cmd string) (refusal string, ok bool)` — `ok` for the nine verbs; `refusal` = `<verb> is retired: write the file (gg session note add)` / `(gg session overview add)`.

- [ ] **Step 1: Write the failing tests**

`internal/steer/retired_test.go`:

```go
package steer

import "testing"

func TestRetiredVerbs(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"note_add", "note_list", "note_show", "note_rm"} {
		if got, ok := RetiredVerb(v); !ok || got != v+" is retired: write the file (gg session note add)" {
			t.Errorf("%s: %q %v", v, got, ok)
		}
	}
	for _, v := range []string{"overview_add", "overview_set", "overview_list", "overview_show", "overview_rm"} {
		if got, ok := RetiredVerb(v); !ok || got != v+" is retired: write the file (gg session overview add)" {
			t.Errorf("%s: %q %v", v, got, ok)
		}
	}
	for _, v := range []string{"navigate", "files", "file_focus", "note", "overview", ""} {
		if _, ok := RetiredVerb(v); ok {
			t.Errorf("%q retired", v)
		}
	}
}
```

`docs/specs/gg-steer/steer-cases.json`: replace the "note on nothing" case with

```json
{
 "name": "a retired note verb names the fix",
 "sync": true,
 "retired": true,
 "command": {"id": "c-8", "cmd": "note_add", "file": "a.go", "start": 1, "end": 1, "summary": "s", "wait": true},
 "reply": {"ok": false, "error": "note_add is retired: write the file (gg session note add)"}
}
```

and add, right after it:

```json
{
 "name": "a retired overview verb names the fix",
 "sync": true,
 "retired": true,
 "command": {"id": "c-8b", "cmd": "overview_add", "title": "T", "text": "[a](a.go:1)", "wait": true},
 "reply": {"ok": false, "error": "overview_add is retired: write the file (gg session overview add)"}
}
```

(and in `about`: one sentence — "`retired` cases are verbs the session-docs spec retired (2026-10-18 §6.3): every consumer answers the recorded refusal".) The TUI's `TestSteerCases` already runs every `sync` case: these two now run there. The web twin, `internal/web/steer_retired_test.go`:

```go
package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

// The web answers the corpus's retired cases with the recorded refusal
// (HTTP 200, ok=false), so an old gg session CLI prints the fix.
func TestWebAnswersRetiredSteerVerbs(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "specs", "gg-steer", "steer-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Name    string        `json:"name"`
			Retired bool          `json:"retired"`
			Command steer.Command `json:"command"`
			Reply   struct {
				OK    bool   `json:"ok"`
				Error string `json:"error"`
			} `json:"reply"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	s, _, _ := attachedSrv(t)
	ran := 0
	for _, c := range corpus.Cases {
		if !c.Retired {
			continue
		}
		ran++
		r := postSteer(t, s, c.Command)
		if r.OK != c.Reply.OK || r.Error != c.Reply.Error {
			t.Errorf("%s: reply %+v, want error %q", c.Name, r, c.Reply.Error)
		}
	}
	if ran < 2 {
		t.Fatalf("only %d retired cases", ran)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/steer -run TestRetiredVerbs; go test ./internal/tui -run TestSteerCases; go test ./internal/web -run TestWebAnswersRetiredSteerVerbs`
Expected: FAIL — `RetiredVerb` undefined; the TUI answers `note_add` with `noted …` (ok=true) and the web answers from its store.

- [ ] **Step 3: `internal/steer/retired.go`**

```go
package steer

import "strings"

// RetiredVerb says whether cmd is one of the note_*/overview_* verbs the
// session-docs spec retired (2026-10-18 §6.3): an agent writes the file
// instead. The refusal names the fix; every consumer answers it the same way.
func RetiredVerb(cmd string) (refusal string, ok bool) {
	switch cmd {
	case "note_add", "note_list", "note_show", "note_rm":
		return cmd + " is retired: write the file (gg session note add)", true
	case "overview_add", "overview_set", "overview_list", "overview_show", "overview_rm":
		return cmd + " is retired: write the file (gg session overview add)", true
	}
	_ = strings.TrimSpace
	return "", false
}
```

(drop the `strings` filler.) Reword the `Cmd` comment at `steer.go:106`: `"navigate" | "reload" | "focus" | "highlight" | "highlight_clear" | "files" | "file_focus" | "serve" (TUI only …); note_*/overview_* are RETIRED (RetiredVerb)`. The note/overview fields' comments (`NoteID`, `Summary`…, `Title`, `Text`, `Background`): "kept so an old CLI's command still decodes; a reader refuses the verb (RetiredVerb)". `Background`'s comment: navigate only.

- [ ] **Step 4: TUI**

`applySteer`: first lines become

```go
func (m Model) applySteer(c steer.Command) (Model, tea.Cmd) {
	if why, retired := steer.RetiredVerb(c.Cmd); retired {
		return m, m.answerSteer(c, steerFail(c, why))
	}
	if why := steerEnumRefusal(c); why != "" {
```

Delete from `steerEnumRefusal` the `note_add`/`note_show`/`note_rm` switch and simplify the background check to

```go
	if c.Background {
		if c.Cmd != "navigate" {
			return "background applies to navigate only"
		}
		if c.HintKind != model.ContentHintKind {
			return "background needs a content link"
		}
	}
```

Delete the two `HasPrefix` dispatch cases and the comment that names them; delete `steer_file_notes.go`, `steer_overview.go`, their tests; in `model.go` delete the `noteLandedMsg` and `anchorsCheckedMsg` cases (and anything only they reached — `go vet` will say). Delete `Headless.AddOverview`. Any test in `session_docs_test.go` / `steer_files_test.go` that used `overview_add` to seed an overview writes the file by hand instead (`writeSessionOverview`). `open_file_notes.go`'s mention: read it; if it is the i18n status text for `note_add`, remove the dead branch.

- [ ] **Step 5: Web**

In `steer.go` replace the two gates:

```go
	if why, retired := steer.RetiredVerb(c.Cmd); retired {
		writeJSON(w, steerFail(c, why))
		return
	}
```

Delete `steer_notes.go`, `steer_overviews.go`. Tests: `agentdocs_web_test.go` and `agentdocs_overview_web_test.go` are plan-era tests of the note/overview verbs over HTTP — delete those tests whose subject is the verb's own behaviour; KEEP (rewritten to seed by `writeDoc` + a `Reload`) the ones that pin a page-facing behaviour the verbs merely set up: the `/api/file-notes`/`/api/overview` reads, the follow pass listing, deletes, `x` backgrounds. For each kept test note in the ledger what it pins. `session_docs_web_test.go`'s 8 mentions: the `overview_rm` test (`TestSteerOverviewVerbsOnTheWeb`) becomes a retired-refusal assertion or goes (the corpus twin covers it); the listing/cold `note_show` tests seed by file and read through `/api/…` instead of the verb (`note_show` was the way the CLI read a note; the page reads `/api/file-notes` — check the route name in `session_docs_http.go`).

- [ ] **Step 6: e2e harness**

`e2e/scenario.go`: delete `Overview *TUIOverview` and the `TUIOverview` type; `e2e/tui.go`: delete the `if ov := st.Overview` block. `e2e/scenarios/tui_overview_anchor_bands.toml`: the step that had `overview = { title = "Tour", text = "…" }` becomes

```toml
doc = { name = "20260101T220000-ab12.md", content = "+++\nformat = 1\ntitle = \"Tour\"\nauthor = \"agent\"\ncreated = 2026-01-01T22:00:00Z\n+++\n# Tour\n\n- [three](a.txt:3)\n- [ten to twelve](a.txt:10-12)\n- [twenty](a.txt:20)\n" }
```

(the header comment: "An agent's overview, written as a session document"). The scenario's later steps that pressed keys to open the overview: a document joins the list in the BACKGROUND (plan 2), while `overview_add` showed it — add the key step that opens it (ctrl+\ then enter on its row, as `tui_session_docs.toml` does) before the first screen that expects the split. Run `go test ./e2e -run 'TestScenarios/tui_overview_anchor_bands' -update`, then READ every regenerated `.screens/*.txt` and confirm the split, the bands and the n/p walk are what the scenario's comment promises.

- [ ] **Step 7: Run the packages**

Run: `go vet ./... && go test ./internal/steer ./internal/tui ./internal/web ./e2e 2>&1 | tail -30`
Expected: PASS (`TestSteerCases` with the two retired cases, the web twin, the bands scenario).

- [ ] **Step 8: Commit**

```bash
gg add internal/steer/retired.go internal/steer/retired_test.go internal/steer/steer.go internal/tui/steer.go internal/tui/model.go internal/tui/headless.go internal/web/steer.go docs/specs/gg-steer/steer-cases.json docs/specs/gg-steer/command.schema.json docs/specs/gg-steer/README.md e2e/scenario.go e2e/tui.go e2e/scenarios/tui_overview_anchor_bands.toml e2e/scenarios/tui_overview_anchor_bands.screens <every test file touched> 
gg add -u internal/tui internal/web   # the deletions — check `git status` first: ONLY deleted files
git commit -F <msgfile>   # "steer: note_*/overview_* retired — a reader names the file verb; handlers removed"
```

(`gg add -u <dir>` stages deletions and modifications under the dir; verify with `git status --short` that no bin/ or stray file is staged.)

---

### Task 6: the TUI's `files` reply carries author, created, anchors

**Files:**
- Modify: `internal/tui/open_files.go:370-396` (`openFilesProto`), `docs/specs/gg-steer/reply.schema.json` (files item: `author`, `created`, `anchors`; notes item: `created`; overviews item: `author`, `created` — only where the items are described)
- Test: `internal/tui/session_docs_files_reply_test.go` (new)

- [ ] **Step 1: Write the failing test**

```go
package tui

import (
	"testing"
)

// gg session files on the TUI lists a session overview's author, created
// stamp and anchor count, as the web does.
func TestFilesReplyCarriesAnOverviewsFrontMatter(t *testing.T) {
	t.Parallel()
	m, _, dir := attachedDocsModel(t)
	writeSessionOverview(t, m, dir, "20260101T220000-ab12", "Tour of a", "2026-01-01T22:00:00Z", "See [two](a.txt:2) and [one](a.txt:1).\n")
	m, _ = m.syncAgentDocs()
	var row *steer.OpenFile
	for _, f := range m.openFilesProto() {
		if f.ID == "20260101T220000-ab12" {
			f := f
			row = &f
		}
	}
	if row == nil {
		t.Fatal("overview not listed")
	}
	if row.Source != "overview" || row.Author != "agent" || row.Created != "2026-01-01T22:00:00Z" || row.Anchors != 2 {
		t.Fatalf("row = %+v", *row)
	}
}
```

(`writeSessionOverview`'s author is whatever the helper writes — read it; adjust the expectation to the helper, or pass the author if it takes one.)

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/tui -run TestFilesReplyCarriesAnOverviewsFrontMatter`
Expected: FAIL — `Author = ""`.

- [ ] **Step 3: Fill the fields**

In `openFilesProto`, inside the `case srcOverview, srcReviewOverview, srcCommitOverview:` branch add after the Source/Title line:

```go
			if d.ov != nil && m.docs != nil {
				if o, ok := m.docs.OverviewIn(d.root, d.id()); ok {
					f.Author, f.Anchors = o.Author, len(o.Anchors)
					if !o.Created.IsZero() {
						f.Created = o.Created.UTC().Format(time.RFC3339)
					}
				}
			}
```

(`d.root` is the field `agentdocs_track.go:76` reads; add `"time"`.) In `reply.schema.json`'s files item `properties` add `"author": {"type": "string"}, "created": {"type": "string", "format": "date-time"}, "anchors": {"type": "integer"}`; if `notes`/`overviews` items list properties, add `created` / `author`+`created` there too.

- [ ] **Step 4: Run the TUI and steer packages**

Run: `go test ./internal/tui -run 'TestFilesReply|TestSteer' ; go test ./internal/steer`
Expected: PASS (`TestWireShapesMatchSchemas` still validates what gg writes).

- [ ] **Step 5: Commit**

```bash
gg add internal/tui/open_files.go internal/tui/session_docs_files_reply_test.go docs/specs/gg-steer/reply.schema.json
git commit -F <msgfile>   # "tui: the files reply carries an overview document's author, created and anchors"
```

---

### Task 7: e2e scenarios for the file verbs

**Files:**
- Rewrite: `e2e/scenarios/s104_session_note_web.toml`, `s105_session_overview_web.toml`
- Modify: `e2e/scenarios/s106_session_docs_web.toml` (the rows' shapes)
- Create: `e2e/scenarios/s107_session_docs_cli.toml` (no window live)

- [ ] **Step 1: Write the scenarios**

`s104_session_note_web.toml`:

```toml
name = "session note with only gg web live: the CLI writes the note file, the page lists it"

# The verbs are file operations: add writes <id>.toml into the worktree's
# docs directory (the page's store watches it); list reads the directory.
# The ids are minted (<UTC second>-<4 hex>), so the runs match around them.
[input]
web = true
steps = [
  { write = "a.txt", content = "alpha\nbravo\ncharlie\n" },
  { commit = "seed" },
]

[[run]]
cmd             = ["session", "note", "add", "a.txt:2", "--summary", "look at bravo"]
exit            = 0
stdout_contains = ["noted a.txt:2-2 as "]

[[run]]
cmd             = ["session", "note", "list"]
exit            = 0
stdout_contains = ["\ta.txt\t2-2\tlook at bravo"]

[[run]]
cmd             = ["session", "note", "add", "a.txt:9", "--summary", "too far"]
exit            = 1
stderr_contains = ["line 9 is past the end of a.txt (3 lines)"]

[[run]]
cmd             = ["session", "note", "rm", "t1"]
exit            = 1
stderr_contains = ["no note t1"]

[[run]]
cmd             = ["session", "note", "clear", "a.txt"]
exit            = 0
stdout_contains = ["removed 1 notes on a.txt"]

[expect]
branch = "main"
```

`s105_session_overview_web.toml`:

```toml
name = "session overview with only gg web live: the CLI writes the file, then asks the page to show it"

# add writes <id>.md and ends with a file_focus to the live page, whose
# reply is printed (the page attached the same directory; its reload-before-
# refuse makes the focus land before the watch fires). --background skips
# the focus. Minted ids: the runs match around them.
[input]
web = true
steps = [
  { write = "a.txt", content = "alpha\nbravo\ncharlie\n" },
  { commit = "seed" },
]

[[run]]
cmd             = ["session", "overview", "add", "--title", "T"]
stdin           = "[a](a.txt:2) and [gone](nope.txt)\n"
exit            = 0
stdout_contains = ["unresolved: nope.txt", "focused overview ", " \"T\"; no gg web tab is open to show it"]

[[run]]
cmd             = ["session", "overview", "add", "--title", "Quiet", "--background"]
stdin           = "[a](a.txt:1)\n"
exit            = 0
stdout_not_contains = ["focused"]

[[run]]
cmd             = ["session", "overview", "list"]
exit            = 0
stdout_contains = ["\t1 anchors\tQuiet", "\t2 anchors\tT"]

[[run]]
cmd             = ["session", "files"]
exit            = 0
stdout_contains = ["\t\"T\"\toverview", "\t\"Quiet\"\toverview"]

[[run]]
cmd             = ["session", "overview", "rm", "f1"]
exit            = 1
stderr_contains = ["no overview f1"]

[expect]
branch = "main"
```

(check the harness supports `stdout_not_contains`; if not, drop that run's assertion to `stdout_contains = ["\n"]`-free form — read `e2e/scenario.go`'s run fields and use what exists, or add `stdout_not_contains` with a one-line check beside `stdout_contains` and a `TestRunValidation` case.) `s106`: the `overview list` runs lose `\tbackground` (`"20260101T220000-ab12\t1 anchors\tTour of a"`, `"…\t2 anchors\tT2"`); `overview rm` expects `removed 20260101T220000-ab12`; `note rm` expects `removed …`; `overview show` after rm still `no overview`. `s107_session_docs_cli.toml` (no `web`): `docs path` exit 0; `note add a.txt:1 --summary s` → `noted a.txt:1-1 as `; `overview add --title T --background` + stdin `[a](a.txt:1)\n` → exit 0, no `focused`; `docs ls` → `overview\t`, `note\t…\ta.txt\t1-1\ts`; `note show` by the id is not possible (minted) — skip; `overview add --title T` WITHOUT `--background` and no window → exit 0, stdout has no `focused` and stderr is empty (`stderr_contains = []` or whatever the harness offers for "empty": read `scenario.go`; if nothing, assert exit 0 only).

- [ ] **Step 2: Run them**

Run: `go test ./e2e -run 'TestScenarios/(s104|s105|s106|s107)' 2>&1 | tail -30`
Expected: PASS. A `focused overview … "T"` that does not appear in s105: the write→focus reload of Task 4 did not land on the web — fix the code, not the scenario.

- [ ] **Step 3: Commit**

```bash
gg add e2e/scenarios/s104_session_note_web.toml e2e/scenarios/s105_session_overview_web.toml e2e/scenarios/s106_session_docs_web.toml e2e/scenarios/s107_session_docs_cli.toml <e2e/scenario.go e2e/scenario_test.go if touched>
git commit -F <msgfile>   # "e2e: the session docs file verbs with gg web live and with no window"
```

---

### Task 8: skills, specs, docs, the plugin's copies

**Files:**
- Modify: `internal/agentskill/using-gg.md` (lines ~470–590: "Temporary notes", "Overviews"), `gg-overview.md` (the header's "TEMPORARY" paragraph, "Temporary (in-memory) overview" → "Session overview — notes and overview as files", "As a worker agent"), `gg-overview-v2.md` (the same passages, lines ~20–25 and 213–287), `gg-delegate.md` (one line near 68–71 and 254), `agentskill.go` (the four version constants)
- Modify: `docs/superpowers/specs/2026-10-15-gg-steer-ide-consumer-design.md` (a dated "Revision 2026-10-19 — session docs" section after §8: the nine verbs retired, the refusal, `file_focus` by document id, `files` rows with author/created/anchors), `docs/specs/gg-steer/README.md` (done in Task 5), `docs/CLAUDE-details.md` (append "### Part 4 — the CLI writes, the verbs retire" under the session-docs section), `CHANGELOG.md` (a `## Session docs, part 4: …` entry above part 3), `README.md` (the `gg session overview` / notes passages at ~515–521 and the agent-steering paragraph at ~323: files, no window needed, `docs path|ls`), `CLAUDE.md` (the `steer` row: mention `RetiredVerb`; the `cli` row: `gg session docs`)
- Regenerate: `.claude/skills/gg-overview/SKILL.md`, `.claude/skills/using-gg/SKILL.md`, `.claude/skills/gg-delegate/SKILL.md` (and the v2 copies if `gg init --experimental` installed them here — `ls .claude/skills`) with the worktree's binary: `./build.sh` in the worktree, then `<worktree>/bin/gg init --update` from the worktree root
- Refresh (uncommitted, for the user): `/work/gg-notes/docs/gg-session-docs/SPEC.md` (the header: branch merged; body = the spec), `/work/gg-notes/docs/gg-steer/SPEC.md` (header + the new revision section), `/work/gg-notes/docs/gg-steer/steer-cases.json`, `command.schema.json`, `reply.schema.json`, `README.md` — copy from gigagit with the provenance header the copies carry (read one first); verify PLUGIN.md §4's refusal prose equals `steer.RetiredVerb`'s (it does today: per kind) and §5.4 needs no second migration section.
- Test: `internal/agentskill/agentskill_test.go` (if a test pins text such as "Temporary notes", update it; versions are read from the constants)

- [ ] **Step 1: using-gg.md** — replace the two sections. The notes section's new text:

```markdown
**Session notes — explain on the code itself.** When the user says "show me
on the files", put short remarks on the lines you are talking about. A note
is a FILE in the worktree's session-docs directory (`gg session docs path`
prints it); every gg reader of that worktree — the TUI, the gg web page, the
gg-notes IDE plugin — shows it in a box under its lines, and it stays until
someone deletes it. No window needs to be live to add one. Write them in
ASD-STE100 (see "Review notes").

- `gg session note add <path>:<start>[-<end>] --summary "…" [--rationale "…"]
  [--author <name>] [--meta k=v ...] [--display <json|@file>] [--json]` — put a
  note on those lines of the file AS IT IS ON DISK (`<path>` is relative to
  the worktree, slash form; the file is read now and its lines are stored
  with the note, so add notes AFTER your edits). Answers `noted
  <path>:<start>-<end> as <id>` — the id is the note's file name
  (`20261018T143205-c0d1`). Refused (exit 1), never cut: `<path> has no lines
  to note`, `line N is past the end of <path> (M lines)`, 50 notes per file,
  summary over 500 characters, rationale over 4000.
- `gg session note list [<path>] [--json]` —
  `<id>\t<path>\t<start>-<end>\t<summary>` by path then line, placed on the
  file as it is now; `(outdated)` marks a note whose lines are gone.
- `gg session note show <id> [--json]` — the note and the lines it sits on now.
- `gg session note rm <id>` → `removed <id>` (`no note <id>` otherwise);
  `gg session note clear <path>` → `removed N notes on <path>`.

The readers follow the file: a note moves with its lines when the file is
edited; the user deletes a note from the box (D), and `rm` deletes its file.
Closing the file in a reader does not delete anything. These are not `gg
note` review notes and never reach the notes store.

The walkthrough: `gg session note add` on each place, then the overview
(below) that links them, then one `gg session files focus <id>:<line>` at a
time as you explain. Keep a summary to one sentence; the reasoning goes in
`--rationale`. A note taller than half the user's window is cut in place.

When the user's message holds `gg note <id> <path>:<lines>`, they copied a
reference to one of your notes: `gg session note show <id>` says which
remark they mean (`no note <id>` = they deleted it; the path and lines
still say where it was).
```

The overview section: keep the anchor-grammar bullet (note anchors are `(note:<id>)` with the id the CLI printed), then

```markdown
- `gg session overview add --title "…" [--file <md>] [--background] [--json]`
  — the markdown from `--file` or piped stdin (a terminal on stdin is
  refused; ≤ 64 KiB, ≤ 100 anchors). Writes `<id>.md` into the worktree's
  session-docs directory and prints the id (its file name), one
  `unresolved: <dest>` line per anchor whose file or note was not found
  (fix with `set`), `in backticks: …` for a link you wrapped in backticks,
  and a warning when it has no anchor. Then, unless `--background`, it asks
  the live window to show it and prints that answer (`focused overview <id>
  "<title>"`; on gg web `; no gg web tab is open to show it` when none is).
  With no window live it prints nothing more and exits 0: the file waits for
  the next reader (say so in one line; do NOT start gg web).
- `gg session overview set <id> [--title "…"] [--file <md>] [--background]
  [--json]` — rewrite the same file (the title is kept when omitted; `no
  overview <id>` when there is none); shows it like add.
- `gg session overview list [--json]` — `<id>\t<n> anchors\t<title>`, newest
  first; `show <id> [--json]` — `<id>\t<title>\t<author>\t<created>`, a blank
  line, the text; `rm <id>` — `removed <id>`. `gg session files focus <id>`
  brings one to the front in the live window.
- `gg session overview export <id> [--out <path>|-] [--force]` — unchanged
  (one self-contained HTML file; run it in the overview's worktree).
- `gg session docs path` prints the directory (for writing files yourself —
  the gg-overview skill has the shapes); `gg session docs ls [--json]` lists
  every overview and note in it (`<kind>\t<id>\t…`).
- Exit 1 answers: `no overview <id>` (deleted — add a new one); exit 2 is
  misuse: `--title is required`, `the text is empty`, `give the text with
  --file or on stdin`, `the text is over 64 KiB`.

A session overview lives until someone deletes it: the user from a reader's
row (D), you with `rm`. Closing it in a reader hides it there only. The
steer verbs `note_*` / `overview_*` of older gg versions are retired: a
reader answers `<verb> is retired: write the file (gg session note add)`.

A worker agent started by gg runs in its OWN worktree: its notes and
overview files land in that worktree's directory, which the user's gg shows
only while looking at that worktree — a worker's overview is its final
`agent_report` (the gg-delegate skill).
```

(Rewrite the surrounding paragraphs that still say "memory", "gone on X", "needs a live gg", "`f<n>`" for an overview id, "20 overviews per worktree", "`added f<n> in the background`".) Then `grep -n 't<n>\|f<n>\|in-memory\|gone when\|closed <id>' internal/agentskill/using-gg.md` and fix every hit that is about session notes/overviews (`f<n>` for an OPEN FILE stays).

- [ ] **Step 2: gg-overview.md and gg-overview-v2.md** — the header paragraph: "Make it a **SESSION** overview (files in the worktree's session-docs directory, shown by every gg reader of that worktree, kept until deleted) only when the user says "session", "temporary" or "don't keep it"". The recipe section, renamed `## Session overview — notes and overview as files — only when asked`:

```markdown
Notes and overviews made this way are FILES in the worktree's session-docs
directory (`gg session docs path`). Every gg reader of that worktree shows
them — the TUI, a gg web page, the gg-notes IDE plugin — and they stay until
the user or you delete them. No window needs to be live.

1. **Finish first.** A note stores the file's lines as they are on disk at
   that moment — make every edit before you add a single note or anchor.
2. **Work in the worktree you present.** `cd` to it; paths in notes and
   anchors are relative to it, in slash form.
3. **Notes on the lines.** `gg session note add <path>:<start>[-<end>]
   --summary "…" [--rationale "…"] --author "<your name>"` — one per remark,
   on the exact lines. Summary: one sentence; reasoning in `--rationale`.
   Several short notes beat one long one. A note may carry `--meta k=v` and
   `--display <json>` (reviewing-with-gg's "Meta and display"). Each answers
   `noted … as <id>` — keep the ids for step 4.
4. **The overview.** The walkthrough of "What the overview says", as
   markdown; its links are the anchors: [the parser](internal/x/parse.go:40-60),
   [this check](src/a.go:120), [why this lock](note:<id>) (opens the file
   at your note), [the config](config.toml). `gg session overview add
   --title "<short title>" --file <md>` (or the markdown on stdin) writes it
   and prints its id; each `unresolved: <dest>` line is an anchor whose file
   or note was not found — fix the text and `gg session overview set <id>
   --file <md>`. It then asks the live window to show it (`focused overview
   <id> "<title>"`); with no window live it prints nothing more: tell the
   user in one line that no gg window is open and that the files are in
   `<directory>` — whichever reader opens next shows them. Never start gg
   web or the TUI for this. `--background` writes without showing.
5. **Reply** in chat with the overview's id and title and a three-line
   summary. Do not paste the overview into the chat — it is in gg (in an
   IDE: the plugin's tool window lists it under "This session").

**Writing the files yourself** (an old gg, or no gg on the machine): the
directory is `gg session docs path`, or
`<state>/gg/sessions/<repo key>/steer/<worktree key>/docs/` (`<state>` =
`$XDG_STATE_HOME` or `~/.local/state`, `%LOCALAPPDATA%` on Windows,
`~/Library/Application Support` on macOS; the keys are the path with every
character outside `[A-Za-z0-9_-]` replaced by `-` — `gg session status`
prints the inbox, the parent of `docs/`). An id matches
`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`; mint it as `<UTC stamp>-<4 hex>`
(`date -u +%Y%m%dT%H%M%S`-`$RANDOM`). Write to a temp name that starts with
`.` and ends in `.tmp` in that directory, then `mv` it over the final name.

An overview, `<id>.md`:

    +++
    format = 1
    title = 'How scroll coalescing works'
    author = 'claude'
    created = 2026-10-18T14:32:01Z
    +++
    Scroll events are coalesced per frame, so [one flush](internal/tui/scroll.go:102-104) …

A note, `<id>.toml` — `lines` MUST be the text of lines `start..end` exactly
as you read them from the file after your last edit, one string per line,
no line terminator; that is how a reader places the note:

    format = 1
    path = 'internal/tui/scroll.go'
    start = 102
    end = 104
    summary = 'One lock around flush: the wheel handler and the tick both reach it.'
    rationale = 'Without it a tick between two deltas …'
    author = 'claude'
    created = 2026-10-18T14:32:05Z
    lines = ['func (s *scroller) flush() {', '	s.mu.Lock()', '	defer s.mu.Unlock()']
```

(the EncodeRepoKey rule above must match `docs/specs/gg-steer/repokey-vectors.json` — read the vectors and the steer spec §2 before writing the sentence; if the rule is more involved than one replacement, say "the `gg session status` inbox" only and drop the hand rule.) "As a worker agent": the worker's notes/overview files go into its own worktree's directory, which the user sees when their gg looks at that worktree; the final report stays `agent_report`. The "no window → start gg web" paragraph in "Where it shows" (step 3 of the old recipe) goes; the "queued:" paragraph keeps `navigate`/`files focus` only. The `gg overview f<n>` / `gg note t<n>` paste rule: `<id>`. Apply the same edits to `gg-overview-v2.md` (its lines 20–25, 55–70, 213–287 carry the same text).

- [ ] **Step 3: gg-delegate.md** — at ~69–71: "gg files it as a tour of the worker's worktree: an overview FILE in that worktree's session-docs directory (`tour-report-<proc>-<n>.md`), which the worker's own IDE window sees too"; at ~254 the same clause. Versions in `agentskill.go`: `Version = 178`, `DelegateVersion = 9`, `GGOverviewVersion = 16`, `GGOverviewV2Version = 3`.

- [ ] **Step 4: Specs and docs** — the steer spec's new section (after §8):

```markdown
## Revision 2026-10-19 — session docs

The session-docs spec (`2026-10-18-gg-session-docs-design.md` §6.3) retires
the verbs `note_add`, `note_list`, `note_show`, `note_rm`, `overview_add`,
`overview_set`, `overview_list`, `overview_show`, `overview_rm`: notes and
overviews are files in `<inbox>/docs/`, written by `gg session note|overview`
or by the agent. A consumer that meets a retired verb answers `error:
"<verb> is retired: write the file (gg session note add)"` (`… (gg session
overview add)` for an overview verb) — the shape of any unknown verb;
`steer-cases.json` carries them as `retired` cases. Unchanged: `files`
(whose overview rows now carry `author`, `created` and `anchors`),
`file_focus` (`file_id` may be a document id: the overview's file name),
`navigate`, `highlight`, `highlight_clear`. A consumer that is asked to
focus a document id it has not listed yet re-reads the directory once
before refusing (`gg session overview add` posts the focus right after the
rename).
```

CHANGELOG entry (above "Session docs, part 3"): what changed for a user and an agent — the verbs write files, no window needed, `add`/`set` focus the live window, `--background`, `docs path|ls`, `list` columns, `rm` → `removed`, the retired steer verbs and their refusal, the TUI's files rows, the focus-after-write reload, skills v178/16/3/9, `gg init --update`. CLAUDE-details Part 4: the `docsSession` design (a throw-away store per verb and why), `focusAfterWrite`'s silence rule, `RetiredVerb` as the one list, the reload-before-refuse on both readers, the e2e `overview =` step's removal. README: the two passages. CLAUDE.md: the `steer` and `cli` rows (one line each, no detail).

- [ ] **Step 5: Regenerate the dogfood skills and the plugin copies**

```bash
cd /work/gigagit/.claude/worktrees/session-docs-cli && ./build.sh && ./bin/gg init --update
git status --short .claude/skills   # only the three (or six) skill files changed
```

Then copy to gg-notes (uncommitted): read `/work/gg-notes/docs/gg-steer/SPEC.md`'s first 6 lines for the provenance header shape; write the header + the steer spec; same for `gg-session-docs/SPEC.md` with "merged into main (plans 1–4)"; `cp` the three JSON files and README from `docs/specs/gg-steer/`; `diff -r` both directories against gigagit's (header lines aside) to confirm.

- [ ] **Step 6: The gates**

Run: `go test ./internal/agentskill ./internal/agentinit 2>&1 | tail; cd <worktree> && ./test.sh race > /tmp/claude-1000/-work-gigagit/3f942bc5-9389-49a0-a00f-fef0425e1a09/scratchpad/race-4.log 2>&1; tail -5 …/race-4.log`
Expected: the log ends with `all green` (read the log, never the exit code of a pipe).

- [ ] **Step 7: Commit**

```bash
gg add internal/agentskill/using-gg.md internal/agentskill/gg-overview.md internal/agentskill/gg-overview-v2.md internal/agentskill/gg-delegate.md internal/agentskill/agentskill.go .claude/skills docs/superpowers/specs/2026-10-15-gg-steer-ide-consumer-design.md docs/CLAUDE-details.md CHANGELOG.md README.md CLAUDE.md docs/superpowers/plans/2026-10-19-gg-session-docs-4-cli.md
git commit -F <msgfile>   # "skills + docs: session docs part 4 — the file model in using-gg/gg-overview/gg-delegate (v178/16/3/9), steer spec revision"
```

---

## Self-review notes (done while writing)

- Spec coverage: §6.2 verbs → Tasks 2–3 (`note add|list|show|rm|clear`, `overview add|set|list|show|rm|export`, `docs path|ls`, `--json`, "Showing it"); §6.3 → Task 5; §6.4 readers' rows → Task 6 (TUI parity; the web had it); §7 → Task 8; §8 → PLUGIN.md already carries §4 refusals and §5.4 migration — Task 8 only verifies and refreshes the copies; §10 versions → Task 8; §12 CLI/TUI/web/e2e tests → Tasks 2–7.
- Deviations to ledger when executing: no `--worktree` flag (none exists); the note JSON keeps `text` for the lines (the published `steer.FileNote` wire) and gains `created`, instead of the spec's `lines`; `note add` takes a path only (`f<n>:…` goes); `note add` opens nothing in a reader (the spec is silent; the readers follow the file); `clear` of nothing is exit 1 `no notes on <path>` (the web's 404 wording); `show` prints `<id>\t<title>\t<author>\t<created>` as the "front matter fields" line; the focus reply keeps the shipped `focused overview <id> "<title>"` prose rather than the spec's `opened overview <id>`.
- Type consistency: `docsSession` (Task 2) is what Task 3 consumes; `overviewWire`/`overviewsNewestFirst` (Task 3) are what `docs ls` uses; `steer.RetiredVerb` (Task 5) is what both readers call; `steer.FileNote.Created` (Task 2) and `steer.Overview.Author/Created` (Task 3) are what the tests decode.
