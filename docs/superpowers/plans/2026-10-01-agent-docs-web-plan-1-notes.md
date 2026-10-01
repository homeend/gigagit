# Agent docs on the web — Plan 1: the shared store + notes in gg web

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (this repo forbids implementer subagents — the session that wrote the plan executes it). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An agent's temporary notes (`gg session note …`) live in one process-wide store that the TUI and the gg web page it hosts both read and write, and the web page shows, dismisses and steps through them; a standalone `gg web` answers the note verbs itself.

**Architecture:** A new DAG leaf `internal/agentdocs` owns notes (ids, alignment to file content, limits, reply prose, a change signal). The TUI keeps per-document COPIES for drawing and re-reads them on the store's signal; the web server keeps the same store (hosted: `agentdocs.Shared()`, standalone: its own), follows it with one goroutine (open noted files in the page's list, pin them, tell the tabs), serves the notes with `/api/file-content`, and answers `note_*` steer verbs. The page draws boxes under lines and handles `d r } {`.

**Tech Stack:** Go 1.26, Bubble Tea TUI, net/http + SSE, vanilla ES modules (node-run pure tests), real `git` in temp repos.

**Spec:** `docs/superpowers/specs/2026-10-01-agent-docs-web-design.md` (plan 1 = "store + notes"; overviews are plan 2).

## Global Constraints

- Every rule of the notes spec (`2026-09-30-open-file-notes-design.md`) holds: memory only; working-tree files only; ≤ 50 notes per file, summary ≤ 500 runes, rationale ≤ 4000; esc backgrounds a noted file, only X closes (and drops the notes); a noted file is never evicted by the 20-file cap; the list grows past 20 when nothing is evictable.
- `agentdocs` imports no gigagit package but `steer` in plan 1 (`markdown` joins in plan 2); archtest pins it.
- The store is injected: `tui.New` and `web.NewHost(…, hosted=true)` use `agentdocs.Shared()`; `web.New` builds `agentdocs.New()`; every other test builds `agentdocs.New()` or isolates by temp-dir root.
- Roots are `domain.CheckoutKey(<toplevel>)`. The web computes its root from `svc.TopLevel` (cached per service), never from `svc.Root()` (the opened workdir may be a subdirectory).
- Note line numbers are over CANONICAL lines (`agentdocs.Lines`: CRLF and lone CR break lines, trailing breaks dropped — the TUI's `fileContentLinesTok` rule). The web's display split differs only for files with bare CRs (documented limit).
- Steer reply text is English protocol prose and identical between TUI and web except the other-worktree refusal (`gg is showing worktree …` / `gg web is showing worktree …`) and "closed <path> (20 files open)" leads.
- Every new user-visible TUI string goes through `i18n.T` with keys in all four bundles (plan 1 adds none if the existing keys are reused — check `i18n_scan_test`).
- Web: `fanOut`, never `emit`, for the agentdocs live message. Browser checks assert VISIBILITY and are run against the unfixed build first.
- Commits: `gg add <paths>` + `git commit -F <msgfile>`; never `git add -A` (a built `bin/gg` lives in the worktree). Message ends with the Co-Authored-By / Claude-Session lines.
- Race gate `./test.sh race` → green only when the log says "all green".

## Review Focus

1. **Two sides aligning different content** — TUI and web read the same file around an edit; the store must converge on the newest content and neither side may draw boxes at positions computed for content it is not showing.
2. **The hosted page and the TUI share one store** — a dismiss in either removes the note from the other; X in the TUI on a noted file clears it from the page; the page's x (close everywhere) clears it from the TUI. Root keys must match (toplevel, CheckoutKey) or nothing is shared.
3. **esc on the web** — a file that got a note AFTER the page loaded it must still be backgrounded by esc, not closed (server-side decision on `pinned`).
4. **Steer dispatch order on the web** — `note_*` must be answered before `toSteerWire` (400 "unknown command") and before the `c.Background` branch.
5. **Parallel tests** — web open-files tests keep their literal `f1` ids; no test depends on another test's notes.

---

## File structure

| File | Responsibility |
|------|----------------|
| `internal/agentdocs/store.go` (new) | `Store`, `New`, `Shared`, `NextFileSeq`, `Subscribe`, the broadcaster copy |
| `internal/agentdocs/notes.go` (new) | `Note`, `Lines`, `Print`, `AddNote`, `Align`, `Notes`, `NoteCount`, `NotedPaths`, `FindNote`, `NoteText`, `RemoveNote`, `ClearPath`, the alignment rule |
| `internal/agentdocs/reply.go` (new) | `NoteReference`, `NoteWire`, reply sentences |
| `internal/agentdocs/*_test.go` (new) | moved alignment tests + store tests |
| `internal/domain/linkresolve.go` | `CheckoutKey` |
| `internal/archtest/import_guard_test.go` | `TestAgentdocsIsALeaf` |
| `internal/tui/open_file_notes.go` | notes become `[]agentdocs.Note` copies; store-backed add/remove/clear/sync |
| `internal/tui/open_file_note_keys.go`, `steer_file_notes.go`, `open_file.go`, `open_files.go`, `model.go`, `file_preview.go`, `sessions_popup.go` | follow the copy type; align before fill; ClearPath on close; subscription |
| `internal/tui/agentdocs_track.go` (new) | `docsTrack`, `waitDocsCmd`, `onAgentDocsChanged` |
| `internal/cli/session_note.go` | drop the "need a gg TUI" refusal |
| `internal/web/server.go`, `host.go` | `docs` field; hosted → `Shared()`; start the follow goroutine |
| `internal/web/openfiles.go`, `openfiles_http.go` | seq from the store, `pinned`, server-decided close, `ofList` with note counts |
| `internal/web/agentdocs_follow.go` (new) | `docsRoot`, `followDocs`, `startDocsFollow` |
| `internal/web/filecontent.go` | align + `notes` in the body |
| `internal/web/file_notes.go` (new) | `POST /api/file-notes` |
| `internal/web/steer_notes.go` (new) | `note_add|list|show|rm` answers |
| `internal/web/steer.go`, `reroot.go`, `live.go` | dispatch; follow pass after adopt; `Reason: "agentdocs"` |
| `internal/web/static/viewer.js`, `openfiles.js`, `live.js`, `style.css` | boxes, gutter, keys, buttons, switcher count, refresh |
| `e2e/scenarios/s1xx_session_note_web.toml` (new) | web-only note scenario |
| docs | CHANGELOG, README, `docs/CLAUDE-details.md`, `internal/agentskill/using-gg.md` (v108), CLAUDE.md row |

---

### Task 1: `internal/agentdocs` — the notes store

**Files:**
- Create: `internal/agentdocs/store.go`, `internal/agentdocs/notes.go`, `internal/agentdocs/reply.go`
- Create: `internal/agentdocs/notes_test.go`, `internal/agentdocs/store_test.go`
- Modify: `internal/domain/linkresolve.go` (add `CheckoutKey`), `internal/domain/linkresolve_test.go` (or a new `checkoutkey_test.go`)
- Modify: `internal/archtest/import_guard_test.go`

**Interfaces:**
- Produces:
  - `agentdocs.New() *Store`, `agentdocs.Shared() *Store`
  - `(*Store).NextFileSeq() int64`, `(*Store).Subscribe() (<-chan struct{}, func())`
  - `type Note struct{ ID string; Seq int64; Root, Path string; Start, End int; Summary, Rationale, Author string; Outdated bool }` (the anchor text stays private to the store)
  - `type Fingerprint [32]byte`; `Lines(data []byte) []string`; `Print(lines []string) Fingerprint`
  - `(*Store).AddNote(root, path string, lines []string, start, end int, summary, rationale, author string) (Note, error)`
  - `(*Store).Align(root, path string, lines []string) bool`
  - `(*Store).Notes(root, path string) ([]Note, Fingerprint)`, `(*Store).NoteCount(root, path string) int`, `(*Store).NotedPaths(root string) []string`
  - `(*Store).FindNote(id string) (Note, bool)`, `(*Store).NoteText(id string) []string`, `(*Store).RemoveNote(id string) bool`, `(*Store).ClearPath(root, path string) int`
  - `NoteReference(n Note) string`, `NoteWire(n Note, fileID string, text []string) steer.FileNote`, `NotedDetail(n Note, lead string) string`
  - consts `MaxNotesPerFile = 50`, `MaxNoteSummary = 500`, `MaxNoteRationale = 4000`
  - `domain.CheckoutKey(p string) string`

- [ ] **Step 1: Write the failing tests** — `internal/agentdocs/notes_test.go`

```go
package agentdocs

import (
	"strings"
	"testing"
)

const root = "/r"

func addOn(t *testing.T, s *Store, lines []string, start, end int) Note {
	t.Helper()
	n, err := s.AddNote(root, "f.go", lines, start, end, "s", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func note(t *testing.T, s *Store, id string) Note {
	t.Helper()
	n, ok := s.FindNote(id)
	if !ok {
		t.Fatalf("note %s is gone", id)
	}
	return n
}

func abc() []string { return []string{"a", "b", "c", "d", "e"} }

func TestAddNoteKeepsTheRangeAndDefaultsTheAuthor(t *testing.T) {
	t.Parallel()
	s := New()
	n, err := s.AddNote(root, "f.go", abc(), 3, 4, "  look here  ", "because", "")
	if err != nil {
		t.Fatal(err)
	}
	if n.ID != "t1" || n.Start != 3 || n.End != 4 || n.Summary != "look here" || n.Author != "agent" || n.Root != root || n.Path != "f.go" {
		t.Fatalf("note = %+v", n)
	}
	if got := s.NoteText(n.ID); strings.Join(got, ",") != "c,d" {
		t.Fatalf("text = %q", got)
	}
}

func TestAddNoteRefusals(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", MaxNoteSummary+1)
	for _, tc := range []struct {
		name       string
		lines      []string
		start, end int
		summary    string
		rationale  string
		want       string
	}{
		{"no lines", nil, 1, 1, "s", "", "f.go has no lines to note"},
		{"zero line", abc(), 0, 1, "s", "", "a line number is 1-based"},
		{"backwards", abc(), 4, 3, "s", "", "the range ends before it starts"},
		{"past the end", abc(), 4, 6, "s", "", "line 6 is past the end of f.go (5 lines)"},
		{"no summary", abc(), 1, 1, "   ", "", "a note needs a summary"},
		{"long summary", abc(), 1, 1, long, "", "the summary is longer than 500 characters"},
		{"long rationale", abc(), 1, 1, "s", strings.Repeat("y", MaxNoteRationale+1), "the rationale is longer than 4000 characters"},
	} {
		s := New()
		if _, err := s.AddNote(root, "f.go", tc.lines, tc.start, tc.end, tc.summary, tc.rationale, ""); err == nil || err.Error() != tc.want {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
		if s.NoteCount(root, "f.go") != 0 {
			t.Errorf("%s: a refused add left a note", tc.name)
		}
	}
}

func TestNoteCapPerFile(t *testing.T) {
	t.Parallel()
	s := New()
	for i := 0; i < MaxNotesPerFile; i++ {
		addOn(t, s, abc(), 1, 1)
	}
	if _, err := s.AddNote(root, "f.go", abc(), 1, 1, "s", "", ""); err == nil || err.Error() != "f.go already carries 50 notes" {
		t.Fatalf("err = %v", err)
	}
}

func TestNotesStayOrderedByLineThenAge(t *testing.T) {
	t.Parallel()
	s := New()
	a := addOn(t, s, abc(), 5, 5)
	b := addOn(t, s, abc(), 2, 3)
	c := addOn(t, s, abc(), 2, 2)
	ns, _ := s.Notes(root, "f.go")
	if len(ns) != 3 || ns[0].ID != b.ID || ns[1].ID != c.ID || ns[2].ID != a.ID {
		t.Fatalf("order = %+v", ns)
	}
	if !s.RemoveNote(b.ID) || s.RemoveNote(b.ID) || s.NoteCount(root, "f.go") != 2 {
		t.Fatal("RemoveNote must remove exactly once")
	}
	if n := s.ClearPath(root, "f.go"); n != 2 || s.NoteCount(root, "f.go") != 0 {
		t.Fatalf("ClearPath = %d", n)
	}
	if len(s.NotedPaths(root)) != 0 {
		t.Fatal("a cleared path is still listed")
	}
}

// The alignment rule (moved from tui/open_file_notes_test.go).

func TestNoteMovesWhenLinesAreInsertedAbove(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	s.Align(root, "f.go", []string{"NEW", "a", "b", "c", "d", "e"})
	if g := note(t, s, n.ID); g.Start != 4 || g.End != 5 || g.Outdated {
		t.Fatalf("note = %+v, want 4-5 live", g)
	}
}

func TestNoteMovesWhenLinesAreDeletedAbove(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	s.Align(root, "f.go", []string{"b", "c", "d", "e"})
	if g := note(t, s, n.ID); g.Start != 2 || g.End != 3 || g.Outdated {
		t.Fatalf("note = %+v, want 2-3 live", g)
	}
}

func TestNoteGoesOutdatedWhenEditedAndRecoversOnRevert(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	s.Align(root, "f.go", []string{"a", "b", "C-EDITED", "d", "e"})
	if g := note(t, s, n.ID); !g.Outdated || g.Start != 3 || g.End != 4 {
		t.Fatalf("note = %+v, want 3-4 outdated", g)
	}
	s.Align(root, "f.go", abc())
	if g := note(t, s, n.ID); g.Outdated || g.Start != 3 || g.End != 4 {
		t.Fatalf("after the revert: %+v, want 3-4 live", g)
	}
}

func TestOutdatedNoteIsClampedWhenTheFileShrinks(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	s.Align(root, "f.go", []string{"a"})
	if g := note(t, s, n.ID); !g.Outdated || g.Start != 1 || g.End != 1 {
		t.Fatalf("note = %+v, want 1-1 outdated", g)
	}
}

func TestOutdatedNoteDoesNotJumpToAnAmbiguousCopy(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, []string{"x", "}", "y", "}", "z"}, 2, 2)
	s.Align(root, "f.go", []string{"x", "}!", "y", "}", "z"})
	if g := note(t, s, n.ID); !g.Outdated || g.Start != 2 {
		t.Fatalf("note = %+v, want outdated on line 2", g)
	}
}

func TestNoteFollowsAUniqueMovedBlockAfterGoingOutdated(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	s.Align(root, "f.go", []string{"a", "b", "e"})
	s.Align(root, "f.go", []string{"a", "b", "e", "c", "d"})
	if g := note(t, s, n.ID); g.Outdated || g.Start != 4 || g.End != 5 {
		t.Fatalf("note = %+v, want 4-5 live", g)
	}
}

func TestAlignWithNoLinesTouchesNothing(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	if s.Align(root, "f.go", nil) {
		t.Fatal("an empty read (a placeholder) must not align")
	}
	if g := note(t, s, n.ID); g.Start != 3 || g.Outdated {
		t.Fatalf("note = %+v", g)
	}
}

func TestAlignIsIdempotentAndReportsTheFingerprint(t *testing.T) {
	t.Parallel()
	s := New()
	addOn(t, s, abc(), 3, 4)
	next := []string{"NEW", "a", "b", "c", "d", "e"}
	if !s.Align(root, "f.go", next) {
		t.Fatal("new content must align")
	}
	if s.Align(root, "f.go", next) {
		t.Fatal("the same content a second time must be a no-op")
	}
	if _, fp := s.Notes(root, "f.go"); fp != Print(next) {
		t.Fatal("Notes must report the fingerprint of the content they sit on")
	}
}

// Review Focus 1: an old read aligning after a newer one is undone by the
// next read of the newest content.
func TestOutOfOrderAlignsConverge(t *testing.T) {
	t.Parallel()
	s := New()
	n := addOn(t, s, abc(), 3, 4)
	newer := []string{"NEW", "a", "b", "c", "d", "e"}
	s.Align(root, "f.go", newer)
	s.Align(root, "f.go", abc()) // a stale read lands late
	s.Align(root, "f.go", newer) // the side that saw the mismatch re-reads
	if g := note(t, s, n.ID); g.Start != 4 || g.End != 5 || g.Outdated {
		t.Fatalf("note = %+v, want 4-5 live", g)
	}
}

func TestLinesIsTheTUIsSplit(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"a\nb\n":       "a|b",
		"a\r\nb\r\n":   "a|b",
		"a\rb":         "a|b",
		"a\n\n\n":      "a",
		"":             "",
		"\n\n":         "",
		"x\n\ny\n":     "x||y",
	} {
		if got := strings.Join(Lines([]byte(in)), "|"); got != want {
			t.Errorf("Lines(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNotedPathsAndRootsAreSeparate(t *testing.T) {
	t.Parallel()
	s := New()
	if _, err := s.AddNote("/r", "b.go", abc(), 1, 1, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNote("/r", "a.go", abc(), 1, 1, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNote("/other", "c.go", abc(), 1, 1, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	// In the order their first notes were placed (the TUI's } order: by seq).
	if got := strings.Join(s.NotedPaths("/r"), ","); got != "b.go,a.go" {
		t.Fatalf("NotedPaths = %s", got)
	}
}
```

`internal/agentdocs/store_test.go`:

```go
package agentdocs

import (
	"sync"
	"testing"
	"time"
)

func TestNextFileSeqCountsPerStore(t *testing.T) {
	t.Parallel()
	a, b := New(), New()
	if a.NextFileSeq() != 1 || a.NextFileSeq() != 2 || b.NextFileSeq() != 1 {
		t.Fatal("each store counts from 1, alone")
	}
	if Shared() != Shared() {
		t.Fatal("Shared must be one store")
	}
}

func TestEveryChangeSignalsAndANoOpDoesNot(t *testing.T) {
	t.Parallel()
	s := New()
	ch, cancel := s.Subscribe()
	defer cancel()
	got := func() bool {
		select {
		case <-ch:
			return true
		case <-time.After(50 * time.Millisecond):
			return false
		}
	}
	n := addOn(t, s, abc(), 1, 1)
	if !got() {
		t.Fatal("AddNote did not signal")
	}
	s.Align(root, "f.go", abc())
	if got() {
		t.Fatal("a no-op Align signalled")
	}
	s.RemoveNote(n.ID)
	if !got() {
		t.Fatal("RemoveNote did not signal")
	}
	if s.RemoveNote(n.ID); got() {
		t.Fatal("removing nothing signalled")
	}
}

func TestStoreIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()
	s := New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				n, _ := s.AddNote(root, "f.go", abc(), 1+j%5, 1+j%5, "s", "", "")
				s.Align(root, "f.go", []string{"x", "a", "b", "c", "d", "e"}[i%2:])
				s.Notes(root, "f.go")
				s.RemoveNote(n.ID)
			}
		}(i)
	}
	wg.Wait()
}

func TestReplyHelpers(t *testing.T) {
	t.Parallel()
	n := Note{ID: "t7", Path: "a/b.go", Start: 12, End: 14, Summary: "s", Author: "agent"}
	if got := NoteReference(n); got != "gg note t7 a/b.go:12-14" {
		t.Errorf("reference = %q", got)
	}
	n1 := n
	n1.End = 12
	if got := NoteReference(n1); got != "gg note t7 a/b.go:12" {
		t.Errorf("one-line reference = %q", got)
	}
	if got := NotedDetail(n, "; closed x.go (20 files open)"); got != "noted a/b.go:12-14 as t7; closed x.go (20 files open)" {
		t.Errorf("detail = %q", got)
	}
	w := NoteWire(n, "f3", []string{"l"})
	if w.ID != "t7" || w.FileID != "f3" || w.Path != "a/b.go" || w.Start != 12 || w.End != 14 || w.Text[0] != "l" {
		t.Errorf("wire = %+v", w)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/agentdocs/ 2>&1 | tail -5`
Expected: FAIL — `package … agentdocs: no non-test Go files` / undefined: New.

- [ ] **Step 3: Implement** — `internal/agentdocs/store.go`

```go
// Package agentdocs holds what an agent shows the user beside the code: the
// temporary notes it puts on lines of open working-tree files (and, plan 2,
// its overview documents). Memory only. One Store per process is shared by
// the TUI and the gg web page it hosts, so the two show one set of notes;
// a standalone gg web keeps its own. Callers file everything under a
// worktree root they normalised with domain.CheckoutKey. Reply prose is
// English protocol text — the TUI and the web answer an agent word for word.
package agentdocs

import "sync"

// Store is the shared state. All of it sits behind one mutex and leaves only
// as copies; a change signals every subscriber after the lock is released.
type Store struct {
	mu      sync.Mutex
	fileSeq int64
	noteSeq int64
	files   map[fileKey]*fileNotes
	b       broadcaster
}

type fileKey struct{ root, path string }

// New is an empty store (a standalone gg web, every test).
func New() *Store { return &Store{files: map[fileKey]*fileNotes{}} }

var (
	sharedOnce sync.Once
	shared     *Store
)

// Shared is the process's store: the TUI and the page it hosts both use it.
// Read only at the composition points (tui.New, web.NewHost).
func Shared() *Store {
	sharedOnce.Do(func() { shared = New() })
	return shared
}

// NextFileSeq numbers open files ("f<n>") for every list drawing from this
// store, so an id the store hands out can never collide with another.
func (s *Store) NextFileSeq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fileSeq++
	return s.fileSeq
}

// Subscribe returns a channel that receives after every change since the
// last receive (coalesced: re-read everything you show), and a cancel.
func (s *Store) Subscribe() (<-chan struct{}, func()) { return s.b.subscribe() }

// broadcaster is agentsession.Broadcaster's shape (frontends may not import
// that PTY package): a buffered-1 channel per subscriber, never blocking.
type broadcaster struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func (b *broadcaster) subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	if b.subs == nil {
		b.subs = map[chan struct{}]struct{}{}
	}
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
}

func (b *broadcaster) signal() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
```

`internal/agentdocs/notes.go`:

```go
package agentdocs

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/homeend/gigagit/internal/textdiff"
)

const (
	MaxNotesPerFile  = 50
	MaxNoteSummary   = 500
	MaxNoteRationale = 4000
)

// Note is one remark, as a copy. Start/End are 1-based lines of the content
// the notes were last aligned to (Notes reports its fingerprint).
type Note struct {
	ID        string // "t<n>"
	Seq       int64
	Root      string
	Path      string
	Start     int
	End       int
	Summary   string
	Rationale string
	Author    string
	Outdated  bool // its lines are gone from the file
}

type note struct {
	Note
	anchor []string // the lines' text when placed / last re-anchored
}

// fileNotes is one file's notes and the content they sit on.
type fileNotes struct {
	lines []string
	print Fingerprint
	notes []*note
}

// Fingerprint names one content: sha256 over the canonical lines.
type Fingerprint [32]byte

// Print fingerprints lines (already canonical: Lines' output, or the TUI's
// raw lines, which are split by the same rule).
func Print(lines []string) Fingerprint {
	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l))
		h.Write([]byte{'\n'})
	}
	var f Fingerprint
	copy(f[:], h.Sum(nil))
	return f
}

// Lines splits a file the TUI's way (fileContentLinesTok): CRLF and a lone
// CR are line breaks, trailing breaks are dropped. No lines = nothing to note.
func Lines(data []byte) []string {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// canon is Lines over lines a caller split itself.
func canon(lines []string) []string { return Lines([]byte(strings.Join(lines, "\n"))) }

// AddNote puts a remark on lines start..end of path as lines holds it NOW
// (aligning the path's notes to lines first). The errors are the TUI's.
func (s *Store) AddNote(root, path string, lines []string, start, end int, summary, rationale, author string) (Note, error) {
	lines = canon(lines)
	summary = strings.TrimSpace(summary)
	s.mu.Lock()
	k := fileKey{root, path}
	f := s.files[k]
	aligned := f != nil && len(lines) > 0 && f.align(lines)
	count := 0
	if f != nil {
		count = len(f.notes)
	}
	var err error
	switch {
	case len(lines) == 0:
		err = fmt.Errorf("%s has no lines to note", path)
	case start < 1:
		err = errors.New("a line number is 1-based")
	case end < start:
		err = errors.New("the range ends before it starts")
	case end > len(lines):
		err = fmt.Errorf("line %d is past the end of %s (%d lines)", end, path, len(lines))
	case summary == "":
		err = errors.New("a note needs a summary")
	case utf8.RuneCountInString(summary) > MaxNoteSummary:
		err = fmt.Errorf("the summary is longer than %d characters", MaxNoteSummary)
	case utf8.RuneCountInString(rationale) > MaxNoteRationale:
		err = fmt.Errorf("the rationale is longer than %d characters", MaxNoteRationale)
	case count >= MaxNotesPerFile:
		err = fmt.Errorf("%s already carries %d notes", path, MaxNotesPerFile)
	}
	if err != nil {
		s.mu.Unlock()
		if aligned {
			s.b.signal()
		}
		return Note{}, err
	}
	if f == nil {
		f = &fileNotes{lines: lines, print: Print(lines)}
		s.files[k] = f
	}
	if author == "" {
		author = "agent"
	}
	s.noteSeq++
	n := &note{Note: Note{ID: "t" + strconv.FormatInt(s.noteSeq, 10), Seq: s.noteSeq, Root: root, Path: path,
		Start: start, End: end, Summary: summary, Rationale: rationale, Author: author},
		anchor: append([]string(nil), lines[start-1:end]...)}
	f.notes = append(f.notes, n)
	f.sort()
	out := n.Note
	s.mu.Unlock()
	s.b.signal()
	return out, nil
}

// Align re-anchors path's notes to lines, the content a side just read from
// disk. The same content as last time is a no-op; no lines (a placeholder:
// deleted, empty, too large) never align. Reports whether anything changed.
func (s *Store) Align(root, path string, lines []string) bool {
	lines = canon(lines)
	if len(lines) == 0 {
		return false
	}
	s.mu.Lock()
	f := s.files[fileKey{root, path}]
	changed := f != nil && f.align(lines)
	s.mu.Unlock()
	if changed {
		s.b.signal()
	}
	return changed
}

func (f *fileNotes) align(cur []string) bool {
	p := Print(cur)
	if p == f.print {
		return false
	}
	reanchor(f.notes, f.lines, cur)
	f.lines, f.print = cur, p
	f.sort()
	return true
}

// Notes is a copy of path's notes in reading order and the fingerprint of
// the content they sit on (zero when there are none).
func (s *Store) Notes(root, path string) ([]Note, Fingerprint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.files[fileKey{root, path}]
	if f == nil {
		return nil, Fingerprint{}
	}
	out := make([]Note, len(f.notes))
	for i, n := range f.notes {
		out[i] = n.Note
	}
	return out, f.print
}

// NoteCount is how many notes path carries.
func (s *Store) NoteCount(root, path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.files[fileKey{root, path}]; f != nil {
		return len(f.notes)
	}
	return 0
}

// NotedPaths is root's paths with notes, by their oldest note.
func (s *Store) NotedPaths(root string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	type p struct {
		path string
		seq  int64
	}
	var ps []p
	for k, f := range s.files {
		if k.root != root || len(f.notes) == 0 {
			continue
		}
		first := f.notes[0].Seq
		for _, n := range f.notes {
			first = min(first, n.Seq)
		}
		ps = append(ps, p{k.path, first})
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].seq < ps[j].seq })
	out := make([]string, len(ps))
	for i, x := range ps {
		out[i] = x.path
	}
	return out
}

// FindNote is the note with that id, in any root.
func (s *Store) FindNote(id string) (Note, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, n := s.findLocked(id); n != nil {
		return n.Note, true
	}
	return Note{}, false
}

// NoteText is the lines the note sits on now (nil when it is gone).
func (s *Store) NoteText(id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, n := s.findLocked(id)
	if n == nil {
		return nil
	}
	var out []string
	for i := n.Start; i <= n.End && i <= len(f.lines); i++ {
		out = append(out, f.lines[i-1])
	}
	return out
}

func (s *Store) findLocked(id string) (*fileNotes, *note) {
	for _, f := range s.files {
		for _, n := range f.notes {
			if n.ID == id {
				return f, n
			}
		}
	}
	return nil, nil
}

// RemoveNote drops one note; false when it is gone already.
func (s *Store) RemoveNote(id string) bool {
	s.mu.Lock()
	removed := false
	for k, f := range s.files {
		for i, n := range f.notes {
			if n.ID == id {
				f.notes = append(f.notes[:i], f.notes[i+1:]...)
				if len(f.notes) == 0 {
					delete(s.files, k)
				}
				removed = true
				break
			}
		}
		if removed {
			break
		}
	}
	s.mu.Unlock()
	if removed {
		s.b.signal()
	}
	return removed
}

// ClearPath drops every note of path (X on the file) and says how many.
func (s *Store) ClearPath(root, path string) int {
	s.mu.Lock()
	k := fileKey{root, path}
	n := 0
	if f := s.files[k]; f != nil {
		n = len(f.notes)
		delete(s.files, k)
	}
	s.mu.Unlock()
	if n > 0 {
		s.b.signal()
	}
	return n
}

func (f *fileNotes) sort() {
	sort.SliceStable(f.notes, func(i, j int) bool {
		a, b := f.notes[i], f.notes[j]
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		return a.Seq < b.Seq
	})
}

// reanchor is the TUI's rule (moved from tui/open_file_notes.go): a live
// note follows the line alignment when every one of its lines survived
// unchanged and still sits together; otherwise it goes outdated and keeps
// its numbers, clamped. An outdated note comes back only when its text is at
// its old place again or at exactly ONE place in the file.
func reanchor(notes []*note, old, cur []string) {
	to := sameLineMap(old, cur)
	for _, n := range notes {
		if !n.Outdated {
			if s, ok := mapRange(to, n.Start, n.End); ok {
				n.Start, n.End = s, s+(n.End-n.Start)
				n.anchor = append(n.anchor[:0:0], cur[n.Start-1:n.End]...)
				continue
			}
		} else if s := relocate(cur, n.anchor, n.Start); s > 0 {
			n.Start, n.End, n.Outdated = s, s+len(n.anchor)-1, false
			continue
		}
		n.Outdated = true
		if n.End > len(cur) {
			n.End = len(cur)
		}
		if n.Start > n.End {
			n.Start = n.End
		}
		if n.Start < 1 {
			n.Start, n.End = 1, 1
		}
	}
}
```

Then move `sameLineMap`, `mapRange`, `relocate` verbatim from `internal/tui/open_file_notes.go:198-255` into `notes.go` (they use `textdiff.Compare`). NOTE the one deliberate change in `reanchor`: the TUI code never refreshed `anchor` on a live move (identical text anyway); keep it identical — drop the `n.anchor = …` line if any moved test disagrees.

`internal/agentdocs/reply.go`:

```go
package agentdocs

import (
	"strconv"

	"github.com/homeend/gigagit/internal/steer"
)

// span is "path:start" or "path:start-end".
func span(n Note) string {
	s := n.Path + ":" + strconv.Itoa(n.Start)
	if n.End != n.Start {
		s += "-" + strconv.Itoa(n.End)
	}
	return s
}

// NoteReference is what the user pastes to the agent about a note (r).
func NoteReference(n Note) string { return "gg note " + n.ID + " " + span(n) }

// NotedDetail is note_add's answer; lead is "; closed <path> (20 files
// open)" when the open pushed a file out.
func NotedDetail(n Note, lead string) string { return "noted " + span(n) + " as " + n.ID + lead }

// NoteWire is the note in its protocol form; text (note_show) is the lines
// it sits on.
func NoteWire(n Note, fileID string, text []string) steer.FileNote {
	return steer.FileNote{ID: n.ID, FileID: fileID, Path: n.Path, Start: n.Start, End: n.End,
		Summary: n.Summary, Rationale: n.Rationale, Author: n.Author, Outdated: n.Outdated, Text: text}
}
```

NOTE: `textdiff` is a gigagit package — the archtest allowlist is `{steer, textdiff}` in plan 1 (`markdown` joins in plan 2). The spec's "stdlib + markdown + steer" omitted it; ledger the ruling.

`internal/domain/linkresolve.go` — next to `SameCheckout`:

```go
// CheckoutKey is the key SameCheckout compares: a checkout path cleaned,
// slash-normalised and case-folded where the filesystem is. agentdocs files
// notes under it, so the TUI and a page it hosts meet on one key.
func CheckoutKey(p string) string { return linkPathKey(filepath.Clean(p)) }
```

Test (in `internal/domain`, a new `checkoutkey_test.go`):

```go
func TestCheckoutKeyAgreesWithSameCheckout(t *testing.T) {
	t.Parallel()
	for _, p := range [][2]string{{"/a/b", "/a/b/"}, {"/a/b", "/a/./b"}, {"/a/b", "/a/c"}, {"/A/b", "/a/b"}} {
		if got, want := CheckoutKey(p[0]) == CheckoutKey(p[1]), SameCheckout(p[0], p[1]); got != want {
			t.Errorf("%q vs %q: keys equal=%v, SameCheckout=%v", p[0], p[1], got, want)
		}
	}
}
```

archtest — after `TestSteerIsAStdlibLeaf`:

```go
// TestAgentdocsIsALeaf pins internal/agentdocs: the TUI and the web share
// it, so it may reach no gigagit layer but the protocol and two pure
// helpers.
func TestAgentdocsIsALeaf(t *testing.T) {
	t.Parallel()
	ok := map[string]bool{
		"github.com/homeend/gigagit/internal/steer":    true,
		"github.com/homeend/gigagit/internal/textdiff": true,
	}
	for _, imp := range directImports(t, "github.com/homeend/gigagit/internal/agentdocs") {
		if strings.HasPrefix(imp, "github.com/homeend/gigagit/") && !ok[imp] {
			t.Errorf("internal/agentdocs imports %s — only steer and textdiff are allowed", imp)
		}
	}
}
```

- [ ] **Step 4: Run them to see them pass**

Run: `go test ./internal/agentdocs/ ./internal/domain/ -run 'CheckoutKey|.' -count=1 2>&1 | tail -5 && go test ./internal/archtest/ -run Agentdocs -count=1`
Expected: `ok` for all three.

- [ ] **Step 5: Commit**

```bash
gg add internal/agentdocs internal/domain/linkresolve.go internal/domain/checkoutkey_test.go internal/archtest/import_guard_test.go
git commit -F <msg>   # feat(agentdocs): the shared notes store — ids, alignment, limits, reply prose, change signal
```

---

### Task 2: The TUI moves its notes onto the store

**Files:**
- Modify: `internal/tui/open_file_notes.go`, `open_file_note_keys.go`, `steer_file_notes.go`, `open_file.go`, `open_files.go`, `model.go`, `file_preview.go`, `sessions_popup.go`
- Create: `internal/tui/agentdocs_track.go`, `internal/tui/agentdocs_track_test.go`
- Modify tests: `open_file_notes_test.go` (alignment tests deleted — moved in Task 1), `open_file_note_keys_test.go`, `open_file_notes_render_test.go`, `open_file_notes_review_test.go`, `steer_file_notes_test.go`

**Interfaces:**
- Consumes: Task 1's store API.
- Produces: `Model.docs *agentdocs.Store`, `Model.docsSub *docsTrack`; `openFile.docs *agentdocs.Store`, `openFile.root string`, `openFile.notes []agentdocs.Note`; `agentDocsChangedMsg`; `(Model).waitDocsCmd() tea.Cmd`; `(Model).onAgentDocsChanged() (Model, tea.Cmd)`; `(*openFile).syncNotes() (stale bool)`.

- [ ] **Step 1: Write the failing tests** — `internal/tui/agentdocs_track_test.go`

```go
package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
)

func TestNewUsesTheSharedStore(t *testing.T) {
	t.Parallel()
	if m := newTestModel(t); m.docs != agentdocs.Shared() {
		t.Fatal("tui.New must hold agentdocs.Shared() — the store a hosted page shares")
	}
}

// A dismiss made elsewhere (the browser) leaves the TUI's copy on the next
// change message; the document stays open.
func TestANoteRemovedElsewhereLeavesTheDocument(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.docs = agentdocs.New()
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	n, err := d.addNote(2, 2, "s", "", "")
	if err != nil {
		t.Fatal(err)
	}
	m.docs.RemoveNote(n.ID)
	m, _ = m.onAgentDocsChanged()
	if len(d.notes) != 0 || m.openFiles.find(m.currentWorktree, d.key()) != d {
		t.Fatalf("notes = %d, doc listed = %v", len(d.notes), m.openFiles.find(m.currentWorktree, d.key()) == d)
	}
}

func TestXOnANotedFileClearsItsNotesInTheStore(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.docs = agentdocs.New()
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	if _, err := d.addNote(2, 2, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	m = m.closeDoc(d)
	if got := m.docs.NoteCount(domain.CheckoutKey(m.currentWorktree), "a.txt"); got != 0 {
		t.Fatalf("store still holds %d notes after X", got)
	}
}

// Review Focus 1: notes aligned to content the document does not show are
// not adopted; the document is re-read instead.
func TestNotesAlignedToOtherContentTriggerAReRead(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.docs = agentdocs.New()
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	if _, err := d.addNote(2, 2, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	m.docs.Align(d.root, d.path, append([]string{"NEW"}, rawOf(d.p.lines)...)) // the web read a newer file
	m, cmd := m.onAgentDocsChanged()
	if d.notes[0].Start != 2 {
		t.Fatalf("adopted positions for content not shown: start = %d", d.notes[0].Start)
	}
	if cmd == nil || !d.loading {
		t.Fatal("want a re-read of the document")
	}
}
```

Rewrite `open_file_notes_test.go`'s helper and remaining tests onto the copy type:

```go
// notedDoc is a loaded working-tree document of n lines ("line 1"…) filed
// in a private store.
func notedDoc(t *testing.T, n int) *openFile {
	t.Helper()
	d := newOpenFile(fileSource{kind: srcWorktree}, "f.go")
	d.docs, d.root = agentdocs.New(), "/r"
	d.fill(fileContentMsg{lines: docLines(n)}, 10, 80)
	return d
}
```

…and in every remaining test: `n.id` → `n.ID`, `n.start` → `n.Start`, `n.end` → `n.End`, `n.summary` → `n.Summary`, `n.author` → `n.Author`, `n.outdated` → `n.Outdated`; pointer identity checks (`got[0] != b`) become ID checks; the `anchor` assertion becomes `d.docs.NoteText(n.ID)`. Delete `TestFileNoteMovesWhenLinesAreInsertedAbove` … `TestFileNotesSurviveAnEmptiedAndRestoredFile` (moved to agentdocs) and `abcDoc`/`reload` if no other test uses them; keep ONE integration test through the Model:

```go
// The fill path aligns through the store: a reload with a line inserted
// above moves the note.
func TestAReloadAlignsTheNotesThroughTheStore(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.docs = agentdocs.New()
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 5)
	n, _ := d.addNote(3, 4, "s", "", "")
	m.layers = nil
	m.filesPreview = d // a frame shows it, so liveDoc finds it
	tm, _ := m.Update(fileContentMsg{tag: d.tag, lines: textLines(append([]string{"NEW"}, rawOf(d.p.lines)...)...), reload: true})
	m = tm.(Model)
	if g := d.notes[0]; g.ID != n.ID || g.Start != 4 || g.End != 5 {
		t.Fatalf("note = %+v, want 4-5", g)
	}
}
```

(`bgDoc`'s docs come from the Model when it registers the document — Step 3 sets them in `registerDocEv`. If `bgDoc` registers before `m.docs` is swapped, swap `m.docs` first as above.)

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/tui/ -run 'SharedStore|RemovedElsewhere|XOnANoted|OtherContent|ThroughTheStore|FileNote' -count=1 2>&1 | tail -15`
Expected: build failure — `m.docs undefined`, `d.root undefined`, `n.ID undefined`.

- [ ] **Step 3: Implement**

`open_file_notes.go`: delete `fileNote`, `fileNoteSeq`, `reanchorNotes`, `sameLineMap`, `mapRange`, `relocate`, `sortNotes`, the `maxFileNote*` consts (use `agentdocs.MaxNotesPerFile` etc. where referenced). The document keeps copies:

```go
// addNote puts a remark on lines start..end through the store. The errors
// are English protocol prose: they go back to the agent that asked.
func (d *openFile) addNote(start, end int, summary, rationale, author string) (*agentdocs.Note, error) {
	switch {
	case d.src.kind != srcWorktree:
		return nil, errors.New("temporary notes go on working-tree files only")
	case !docLoaded(d) || d.p.img != nil:
		return nil, fmt.Errorf("%s has no lines to note", d.path)
	case d.docs == nil:
		return nil, fmt.Errorf("%s is not an open file", d.path) // never: note_add registers first
	}
	n, err := d.docs.AddNote(d.root, d.path, rawOf(d.p.lines), start, end, summary, rationale, author)
	if err != nil {
		return nil, err
	}
	d.backgrounded = true // esc steps aside; only X closes (and drops the notes)
	d.syncNotes()
	return d.noteByID(n.ID), nil
}

// syncNotes re-reads d's notes from the store. Notes aligned to content d
// does not show are NOT adopted (their lines would be wrong here): stale is
// true and the caller re-reads d, whose fill aligns and syncs again.
func (d *openFile) syncNotes() (stale bool) {
	var ns []agentdocs.Note
	var fp agentdocs.Fingerprint
	if d.docs != nil && d.src.kind == srcWorktree {
		ns, fp = d.docs.Notes(d.root, d.path)
	}
	if len(ns) > 0 && docLoaded(d) && d.p.img == nil && fp != agentdocs.Print(rawOf(d.p.lines)) {
		return true
	}
	d.notes = ns
	d.syncNoteRows()
	return false
}

func (d *openFile) noteByID(id string) *agentdocs.Note {
	for i := range d.notes {
		if d.notes[i].ID == id {
			return &d.notes[i]
		}
	}
	return nil
}

func (d *openFile) removeNote(id string) bool {
	if d.docs == nil || d.noteByID(id) == nil {
		return false
	}
	ok := d.docs.RemoveNote(id)
	d.syncNotes()
	return ok
}

func (d *openFile) clearNotes() int {
	if d.docs == nil {
		return 0
	}
	n := d.docs.ClearPath(d.root, d.path)
	d.syncNotes()
	return n
}

// noteAt is the first note covering the 1-based line, or nil.
func (d *openFile) noteAt(line int) *agentdocs.Note {
	for i := range d.notes {
		if n := &d.notes[i]; n.Start <= line && line <= n.End {
			return n
		}
	}
	return nil
}
```

`findFileNote` returns `(*openFile, *agentdocs.Note)`; `title`/`boxLines`/`reference` become funcs over `agentdocs.Note` (`noteTitle(n)`, `noteBoxLines(n, innerW, maxRows)`, reference = `agentdocs.NoteReference(n)`); `openFileNote(n agentdocs.Note)`. Field renames in `noteRowsUnder` (`n.End`), `landPendingLine` (`nt.Start`, `nt.End`), `stepFileNote` (`n.Start`, `e.notes[0].Start`), `sessions_popup.go:188`, `file_preview.go:534`.

`open_file.go`: in `openFile` add

```go
	// docs/root are the store the document's notes live in and the root
	// they are filed under (set when it joins the open-files list); nil for
	// a document no list holds (F's live preview) — it carries no notes.
	docs *agentdocs.Store
	root string
```

change `notes []*fileNote` → `notes []agentdocs.Note` (a copy; View never reads the store), delete `openFileSeq` and use the shared counter:

```go
	d.seq = agentdocs.Shared().NextFileSeq()
```

and in `fill` replace the `reanchor`/`before` block with one line after the lines are set:

```go
	d.syncNotes() // the Model aligned the store to these lines before the fill
```

`open_files.go` `registerDocEv`: before `touch`,

```go
	d.docs, d.root = m.docs, domain.CheckoutKey(m.currentWorktree)
	d.syncNotes()
```

and `closeDoc`: before `detachDoc`,

```go
	if len(d.notes) > 0 {
		d.clearNotes() // X drops the notes — everywhere the store is shown
	}
```

`model.go` `fileContentMsg` case, before `d.fill`:

```go
			if d.docs != nil && d.src.kind == srcWorktree && msg.err == nil && msg.img == nil && len(msg.lines) > 0 && msg.lines[0].src {
				d.docs.Align(d.root, d.path, rawOf(msg.lines))
			}
```

`New`: `docs: agentdocs.Shared(),` and `m.docsSub = newDocsTrack(m.docs)`; `Init` batch gains `m.waitDocsCmd()`; `dispatch` gains `case agentDocsChangedMsg: return m.onAgentDocsChanged()`.

`agentdocs_track.go`:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
)

// docsTrack is the TUI's one subscription to the agent-docs store; it lives
// on a pointer so the value-copied Model shares it.
type docsTrack struct {
	ch     <-chan struct{}
	cancel func()
}

func newDocsTrack(s *agentdocs.Store) *docsTrack {
	ch, cancel := s.Subscribe()
	return &docsTrack{ch: ch, cancel: cancel}
}

// agentDocsChangedMsg: something in the store changed — maybe in the
// browser. onAgentDocsChanged re-arms the wait.
type agentDocsChangedMsg struct{}

func (m Model) waitDocsCmd() tea.Cmd {
	if m.docsSub == nil {
		return nil // a Model built as a literal (tests)
	}
	ch := m.docsSub.ch
	return func() tea.Msg {
		<-ch
		return agentDocsChangedMsg{}
	}
}

// onAgentDocsChanged refreshes the current worktree's documents from the
// store; one whose notes sit on content it does not show is re-read.
func (m Model) onAgentDocsChanged() (Model, tea.Cmd) {
	cmds := []tea.Cmd{m.waitDocsCmd()}
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if d.syncNotes() && !d.loading {
			cmds = append(cmds, m.reloadDocCmd(d))
		}
	}
	return m, tea.Batch(cmds...)
}
```

NOTE: `reRoot` must refresh the new worktree's documents: after the re-root lands, call `m.onAgentDocsChanged()`'s loop body once (extract `m.syncDocNotes() tea.Cmd` and call it from both). Check `reRoot` (`model.go` ~4602) for the spot where `currentWorktree` becomes the new one.

`steer_file_notes.go`: `fileNoteProto` → `agentdocs.NoteWire(*n, d.id(), text)` with `text = d.rawLines(n.Start, n.End)` when `text && docLoaded(d)`; `finishNoteAdd` detail → `agentdocs.NotedDetail(*n, lead)`; loops over `d.notes` use the value type.

- [ ] **Step 4: Run them to see them pass**

Run: `go vet ./internal/tui/ && go test ./internal/tui/ -run 'SharedStore|RemovedElsewhere|XOnANoted|OtherContent|ThroughTheStore|FileNote|Note|Eviction|Esc|Overview' -count=1 2>&1 | tail -15`
Expected: `ok`.

- [ ] **Step 5: Whole TUI package**

Run: `go test ./internal/tui/ -count=1 > <ws>/tui.log 2>&1; tail -5 <ws>/tui.log`
Expected: `ok  github.com/homeend/gigagit/internal/tui` (5–6 min).

- [ ] **Step 6: Commit** — `refactor(tui): open-file notes live in the shared agentdocs store; the TUI draws copies and follows its signal`

---

### Task 3: CLI — note verbs reach a web-only session

**Files:**
- Modify: `internal/cli/session_note.go` (`steerNotes`)
- Modify: `internal/cli/session_note_test.go` (`TestSessionNoteNeedsALiveTUI` → `TestSessionNoteGoesToAWebOnlySession`)

- [ ] **Step 1: Write the failing test**

```go
func TestSessionNoteGoesToAWebOnlySession(t *testing.T) {
	t.Parallel()
	var out, errb bytes.Buffer
	if code := runSession(t.TempDir(), nil, []string{"note", "list"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "no gg session for this worktree") {
		t.Fatalf("nothing live: exit=%d stderr=%q", code, errb.String())
	}
	srv, ts := newSteerServer(t, http.StatusOK, `{"id":"x","ok":true,"notes":[{"id":"t1","file_id":"f1","path":"a.go","start":2,"end":2,"summary":"hi"}]}`)
	dir := t.TempDir()
	liveWebPresence(t, dir, ts.URL)
	out.Reset()
	errb.Reset()
	if code := runSession(dir, nil, []string{"note", "list"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "t1\ta.go\t2-2\thi") {
		t.Fatalf("web only: exit=%d out=%q err=%q", code, out.String(), errb.String())
	}
	if got := srv.commands(); len(got) != 1 || got[0].Cmd != "note_list" {
		t.Fatalf("posted %+v", got)
	}
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/cli/ -run SessionNoteGoesToAWebOnly -count=1`
Expected: FAIL — exit 1, stderr "temporary notes need a gg TUI".

- [ ] **Step 3: Implement** — `steerNotes` keeps only the "nothing live" refusal:

```go
// steerNotes posts a note command to the live session and waits for its
// answer: the TUI when one is live, else gg web, which answers from its own
// store (steerLive's routing).
func steerNotes(dir string, c steer.Command, stdout, stderr io.Writer) (steer.Reply, int, bool) {
	rep, code, ok := steerLive(dir, c, false, false, stdout, stderr)
	if ok && !rep.OK {
		fmt.Fprintln(stderr, rep.Error)
		return rep, 1, false
	}
	return rep, code, ok
}
```

- [ ] **Step 4: Run it to see it pass** — `go test ./internal/cli/ -run 'SessionNote' -count=1` → `ok`.
- [ ] **Step 5: Commit** — `feat(cli): gg session note reaches a web-only session`

---

### Task 4: Web server — the store, pinned entries, the follow pass, the endpoints, the steer verbs

**Files:**
- Modify: `internal/web/server.go` (field `docs *agentdocs.Store`; `New` → `agentdocs.New()`; `ofs` seq from it), `host.go` (`NewHost` hosted → `Shared()`; `Start` → `startDocsFollow`)
- Modify: `internal/web/openfiles.go` (`next func() int64`, `pinned`, `ensureOpen`, `setPinned`, `pinnedEntry`, eviction), `openfiles_http.go` (server-decided close, `ofList`)
- Create: `internal/web/agentdocs_follow.go`, `internal/web/file_notes.go`, `internal/web/steer_notes.go`
- Modify: `internal/web/filecontent.go`, `steer.go`, `steer_files.go` (`ofList`), `reroot.go` (`followDocs` after adopt)
- Test: `internal/web/agentdocs_web_test.go` (new)

**Interfaces:**
- Consumes: Task 1 (`AddNote`, `Align`, `Notes`, `NoteCount`, `NotedPaths`, `FindNote`, `NoteText`, `RemoveNote`, `ClearPath`, `NoteWire`, `NotedDetail`, `NoteReference`, `Lines`, `Subscribe`, `NextFileSeq`), `domain.CheckoutKey`.
- Produces: `(*Server).docsRoot(ctx) string`, `(*Server).followDocs()`, `(*Server).ofList(wt string) []steer.OpenFile`, live `Reason: "agentdocs"` (carries `Files`), `fileContentBody.Notes []noteRow` (`{id,start,end,summary,rationale,author,outdated,ref}`), `POST /api/file-notes`.

- [ ] **Step 1: Write the failing tests** — `internal/web/agentdocs_web_test.go`

```go
package web

import (
	"context"
	"net/http"
	"testing"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
)

// noteSrv is a steering server over a one-file repo (f.txt, one line) with
// its own store.
func noteSrv(t *testing.T) (*Server, string) {
	t.Helper()
	s := newSteerServer(t)
	return s, s.docsRoot(context.Background())
}

func TestNewHasItsOwnStoreAndAHostedPageShares(t *testing.T) {
	t.Parallel()
	if New(domain.Open(newRepoDir(t, 1))).docs == agentdocs.Shared() {
		t.Fatal("a standalone server must not use the shared store")
	}
	h := NewHost(domain.Open(newRepoDir(t, 1)), nil, true)
	if h.srv.docs != agentdocs.Shared() {
		t.Fatal("a hosted page must share the TUI's store")
	}
}

func TestSteerNoteAddOpensTheFileAndAnswersLikeTheTUI(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	code, rep := steerAsk(t, s, `{"id":"1","cmd":"note_add","file":"f.txt","start":1,"end":1,"summary":"look"}`)
	if code != http.StatusOK || !rep.OK || rep.Detail != "noted f.txt:1 as t1" || len(rep.Notes) != 1 || rep.Notes[0].FileID != "f1" {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
	if s.docs.NoteCount(root, "f.txt") != 1 {
		t.Fatal("the store has no note")
	}
	if _, ok := s.ofs.lookup(s.service().Root(), ofKey{Src: "worktree", Path: "f.txt"}); !ok {
		t.Fatal("the noted file is not in the page's list")
	}
	_, rep = steerAsk(t, s, `{"id":"2","cmd":"note_add","file":"nope.txt","start":1,"end":1,"summary":"x"}`)
	if rep.OK || rep.Error != "nope.txt is not in the working tree" {
		t.Fatalf("missing file: %+v", rep)
	}
	_, rep = steerAsk(t, s, `{"id":"3","cmd":"note_add","file":"f.txt","start":1,"end":9,"summary":"x"}`)
	if rep.OK || rep.Error != "line 9 is past the end of f.txt (1 lines)" {
		t.Fatalf("past the end: %+v", rep)
	}
}

// Review Focus 4.
func TestSteerNoteVerbsAreDispatchedBeforeTheWireCheckAndTheGate(t *testing.T) {
	t.Parallel()
	s, _ := noteSrv(t)
	s.cur = &opRun{} // an op in flight
	if code, rep := steerAsk(t, s, `{"id":"1","cmd":"note_list"}`); code != http.StatusOK || !rep.OK || rep.Detail != "no notes" {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
}

func TestSteerNoteShowRmAndClear(t *testing.T) {
	t.Parallel()
	s, _ := noteSrv(t)
	steerAsk(t, s, `{"id":"1","cmd":"note_add","file":"f.txt","start":1,"end":1,"summary":"a"}`)
	steerAsk(t, s, `{"id":"2","cmd":"note_add","file":"f.txt","start":1,"end":1,"summary":"b"}`)
	if _, rep := steerAsk(t, s, `{"id":"3","cmd":"note_show","note_id":"t1"}`); !rep.OK || len(rep.Notes) != 1 || len(rep.Notes[0].Text) != 1 {
		t.Fatalf("show: %+v", rep)
	}
	if _, rep := steerAsk(t, s, `{"id":"4","cmd":"note_rm","note_id":"t1"}`); !rep.OK || rep.Detail != "removed t1" {
		t.Fatalf("rm: %+v", rep)
	}
	if _, rep := steerAsk(t, s, `{"id":"5","cmd":"note_rm","note_id":"t1"}`); rep.OK || rep.Error != "no note t1" {
		t.Fatalf("rm again: %+v", rep)
	}
	if _, rep := steerAsk(t, s, `{"id":"6","cmd":"note_rm","file":"f.txt"}`); !rep.OK || rep.Detail != "removed 1 notes from f.txt" {
		t.Fatalf("clear: %+v", rep)
	}
	if _, rep := steerAsk(t, s, `{"id":"7","cmd":"note_rm","file":"zz.txt"}`); rep.OK || rep.Error != "no open file zz.txt" {
		t.Fatalf("clear unknown: %+v", rep)
	}
}

func TestFileContentCarriesTheNotesAndAligns(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	if _, err := s.docs.AddNote(root, "f.txt", []string{"OLD", "line"}, 2, 2, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Notes []struct {
			ID    string `json:"id"`
			Start int    `json:"start"`
			Ref   string `json:"ref"`
		} `json:"notes"`
	}
	ts := serve(t, s)
	if code := getJSON(t, ts, "/api/file-content?path=f.txt", &body); code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	// newRepoDir's f.txt is one line; the note was placed on content with an
	// extra line above — the read aligns it (line 2 → gone or moved).
	if len(body.Notes) != 1 || body.Notes[0].Ref == "" {
		t.Fatalf("notes = %+v", body.Notes)
	}
}

func TestDismissEndpoint(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	n, _ := s.docs.AddNote(root, "f.txt", agentdocs.Lines([]byte("x\n")), 1, 1, "s", "", "")
	ts := serve(t, s)
	if code := postJSON(t, ts, "/api/file-notes", `{"op":"dismiss","id":"`+n.ID+`"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("dismiss: %d", code)
	}
	if code := postJSON(t, ts, "/api/file-notes", `{"op":"dismiss","id":"`+n.ID+`"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Fatalf("dismiss again: %d, want 404", code)
	}
}

// Review Focus 3: esc (a plain close) on a noted entry backgrounds it; x
// (close everywhere) removes it and clears the notes.
func TestCloseOfAPinnedEntryBackgroundsAndEverywhereClears(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	steerAsk(t, s, `{"id":"1","cmd":"note_add","file":"f.txt","start":1,"end":1,"summary":"a"}`)
	s.followDocs()
	ts := serve(t, s)
	wt := s.service().Root()
	f, _ := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"})
	postJSON(t, ts, "/api/open-files", `{"op":"close","id":"`+f.ID+`","tab":"t1"}`, "application/json", "", nil)
	if _, ok := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"}); !ok {
		t.Fatal("esc closed a noted file")
	}
	postJSON(t, ts, "/api/open-files", `{"op":"close","id":"`+f.ID+`","tab":"t1","everywhere":true}`, "application/json", "", nil)
	if _, ok := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: "f.txt"}); ok || s.docs.NoteCount(root, "f.txt") != 0 {
		t.Fatal("x must remove the file and clear its notes")
	}
}

// Review Focus 2 (the hosted case without a TUI): a note the TUI files in
// the shared store opens in the page's list, pinned, and the tabs hear it.
func TestFollowPassOpensANotedFileAndTellsTheTabs(t *testing.T) {
	isolateGlobal(t)
	s, root := noteSrv(t)
	s.startLive(context.Background())
	t.Cleanup(s.Close)
	ts := serve(t, s)
	next, stop := eventsFor(t, ts, "t1")
	defer stop()
	next() // hello
	if _, err := s.docs.AddNote(root, "f.txt", []string{"x"}, 1, 1, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	s.followDocs()
	m := next()
	if m.Reason != "agentdocs" || len(m.Files) != 1 || m.Files[0].Path != "f.txt" || m.Files[0].Notes != 1 {
		t.Fatalf("event = %+v", m)
	}
}

func TestPinnedEntriesAreNeverEvicted(t *testing.T) {
	t.Parallel()
	r := newOpenFiles(func() int64 { return 0 })
	n := int64(0)
	r.next = func() int64 { n++; return n }
	r.ensureOpen("/w", ofKey{Src: "worktree", Path: "noted.txt"}, true)
	for i := 0; i < maxOpenFiles+3; i++ {
		r.open("/w", ofKey{Src: "worktree", Path: "p" + string(rune('a'+i))}, "", 0)
	}
	if _, ok := r.lookup("/w", ofKey{Src: "worktree", Path: "noted.txt"}); !ok {
		t.Fatal("a pinned entry was evicted")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/web/ -run 'Store|SteerNote|FileContentCarries|Dismiss|Pinned|FollowPass' -count=1 2>&1 | tail -10`
Expected: build failure — `s.docsRoot undefined`, `s.followDocs undefined`, `ensureOpen undefined`.

- [ ] **Step 3: Implement**

`openfiles.go`:

```go
type ofEntry struct {
	id      string
	key     ofKey
	line    int
	disk    diskStat
	checked time.Time
	pinned  bool // an agent's notes are on it: never evicted, esc backgrounds
}

type openFiles struct {
	mu       sync.Mutex
	next     func() int64 // the store's NextFileSeq: one counter per store
	byWT     map[string][]*ofEntry
	tabShows map[string]string
	streams  map[string]int
}

func newOpenFiles(next func() int64) *openFiles {
	return &openFiles{next: next, byWT: map[string][]*ofEntry{}, tabShows: map[string]string{}, streams: map[string]int{}}
}
```

`open`: `e = &ofEntry{id: "f" + strconv.FormatInt(r.next(), 10), key: k}`; `frontLocked`: `if !r.shownLocked(l[i].id) && !l[i].pinned`.

```go
// ensureOpen adds k in the background when it is not listed (pinned before
// any eviction is weighed) and reports whether it was added.
func (r *openFiles) ensureOpen(wt string, k ofKey, pinned bool) (steer.OpenFile, string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.byWT[wt] {
		if e.key == k {
			e.pinned = e.pinned || pinned
			return r.wireLocked(e), "", false
		}
	}
	e := &ofEntry{id: "f" + strconv.FormatInt(r.next(), 10), key: k, pinned: pinned}
	l := append([]*ofEntry{}, r.byWT[wt]...)
	l = append(l, e) // a background open joins at the END: it was not shown
	ev := ""
	if len(l) > maxOpenFiles {
		for i := len(l) - 1; i >= 0; i-- {
			if o := l[i]; o != e && !r.shownLocked(o.id) && !o.pinned {
				ev = o.key.Path
				l = append(l[:i], l[i+1:]...)
				break
			}
		}
	}
	r.byWT[wt] = l
	return r.wireLocked(e), ev, true
}

// setPinned pins wt's working-tree entries whose path is in paths and
// unpins the rest; true when anything changed.
func (r *openFiles) setPinned(wt string, paths map[string]bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	for _, e := range r.byWT[wt] {
		want := e.key.Src == "worktree" && paths[e.key.Path]
		if e.pinned != want {
			e.pinned, changed = want, true
		}
	}
	return changed
}

// pinnedEntry reports id's pinned flag and key.
func (r *openFiles) pinnedEntry(wt, id string) (ofKey, bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, e := r.findLocked(wt, id); e != nil {
		return e.key, e.pinned, true
	}
	return ofKey{}, false, false
}
```

(Whether a background `ensureOpen` joins at the front or the end: the TUI's `registerDocEv` puts every registered doc first via `touch`. Match it — use `frontLocked(wt, e)` with `e.pinned` set — unless a test shows a background add must not reorder the switcher. Ledger the choice.)

`server.go` `New`:

```go
func New(svc *domain.Service) *Server {
	s := &Server{closing: make(chan struct{}), sessStop: make(chan struct{}), feeds: newScreenFeeds(), docs: agentdocs.New()}
	s.ofs = newOpenFiles(func() int64 { return s.docs.NextFileSeq() })
	…
```

(field doc: `// docs is the agent-docs store: its own for a standalone gg web, agentdocs.Shared() for a page a TUI hosts (NewHost) — set before Start, never after.`)

`host.go` `NewHost`: `if hosted { s.docs = agentdocs.Shared() }`; `Start`: after `startOpenFilesWatch()`, `h.srv.startDocsFollow()`.

`agentdocs_follow.go`:

```go
package web

import (
	"context"
	"sync"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// The agent-docs store, followed (spec 2026-10-01-agent-docs-web): the
// page's list holds every noted working-tree file of the served worktree,
// pinned, and the tabs hear every change as "agentdocs".

// docsRootCache keys the store by the served worktree's TOPLEVEL — the
// TUI's key — computed once per service (svc.Root() may be a subdirectory).
type docsRootCache struct {
	mu   sync.Mutex
	svc  *domain.Service
	root string
}

func (s *Server) docsRoot(ctx context.Context) string {
	svc := s.service()
	s.rootc.mu.Lock()
	defer s.rootc.mu.Unlock()
	if s.rootc.svc != svc {
		top, err := svc.TopLevel(ctx)
		if err != nil {
			top = svc.Root()
		}
		s.rootc.svc, s.rootc.root = svc, domain.CheckoutKey(top)
	}
	return s.rootc.root
}

// ofList is wt's open files with each working-tree file's note count.
func (s *Server) ofList(wt string) []steer.OpenFile {
	l := s.ofs.list(wt)
	root := s.docsRoot(context.Background())
	for i := range l {
		if l[i].Source == "worktree" {
			l[i].Notes = s.docs.NoteCount(root, l[i].Path)
		}
	}
	return l
}

// followDocs is one pass: open every noted file the list lacks, pin exactly
// the noted ones, tell the tabs (fanOut: never dropped by the op gate).
func (s *Server) followDocs() {
	wt := s.service().Root()
	paths := s.docs.NotedPaths(s.docsRoot(context.Background()))
	set := make(map[string]bool, len(paths))
	for _, p := range paths {
		set[p] = true
		k := ofKey{Src: "worktree", Path: p}
		if f, _, added := s.ofs.ensureOpen(wt, k, true); added {
			s.baseline(wt, f.ID, k)
		}
	}
	s.ofs.setPinned(wt, set)
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: "agentdocs", Files: s.ofList(wt)})
	}
}

// startDocsFollow runs a pass now and on every store change until Close.
func (s *Server) startDocsFollow() {
	ch, cancel := s.docs.Subscribe()
	stop := s.ofStop
	go func() {
		defer cancel()
		s.followDocs()
		for {
			select {
			case <-stop:
				return
			case <-s.closing:
				return
			case <-ch:
				s.followDocs()
			}
		}
	}()
}
```

(`Server` gains `rootc docsRootCache`.) Replace `s.ofs.list(wt)` with `s.ofList(wt)` in `handleOpenFilesGet`, `ans.Files`, `broadcastOpened`, `steerFiles`.

`reroot.go` `adoptService`: after `s.restartLive(ctx)`, `s.followDocs()`.

`openfiles_http.go` `close`:

```go
	case "close":
		k, pinned, found := s.ofs.pinnedEntry(wt, q.ID)
		switch {
		case !found:
			ok = false
		case pinned && !q.Everywhere: // esc on a noted file steps aside (TUI parity)
			ok = s.ofs.background(wt, q.ID, q.Tab, q.Line)
		default:
			ok = s.ofs.close(wt, q.ID, q.Tab, q.Everywhere)
			if ok && q.Everywhere && k.Src == "worktree" {
				s.docs.ClearPath(s.docsRoot(r.Context()), k.Path)
			}
		}
```

`filecontent.go`: `fileContentBody` gains

```go
	// Notes are the agent's notes on a working-tree file, aligned to the
	// lines just read (agentdocs).
	Notes []noteRow `json:"notes,omitempty"`
```

```go
type noteRow struct {
	ID        string `json:"id"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
	Summary   string `json:"summary"`
	Rationale string `json:"rationale,omitempty"`
	Author    string `json:"author"`
	Outdated  bool   `json:"outdated,omitempty"`
	Ref       string `json:"ref"`
}

// notesFor aligns path's notes to data and returns them for the page.
func (s *Server) notesFor(ctx context.Context, path string, data []byte) []noteRow {
	root := s.docsRoot(ctx)
	if s.docs.NoteCount(root, path) == 0 {
		return nil
	}
	s.docs.Align(root, path, agentdocs.Lines(data))
	ns, _ := s.docs.Notes(root, path)
	out := make([]noteRow, len(ns))
	for i, n := range ns {
		out[i] = noteRow{ID: n.ID, Start: n.Start, End: n.End, Summary: n.Summary, Rationale: n.Rationale,
			Author: n.Author, Outdated: n.Outdated, Ref: agentdocs.NoteReference(n)}
	}
	return out
}
```

and the last line of `handleFileContent` becomes:

```go
	body := fileContentBody{Lines: contentRows(path, data, svc.SyntaxHighlighting()), Stamp: stamp}
	if src == "" || src == "worktree" {
		body.Notes = s.notesFor(ctx, path, data)
	}
	writeJSON(w, body)
```

`file_notes.go`:

```go
package web

import (
	"encoding/json"
	"errors"
	"net/http"
)

// POST /api/file-notes {op:"dismiss", id}: the page's d / dismiss button.
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/file-notes", writeGuard(s.handleFileNotes))
	})
}

func (s *Server) handleFileNotes(w http.ResponseWriter, r *http.Request) {
	var q struct{ Op, ID string }
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if q.Op != "dismiss" || !noteIDRE.MatchString(q.ID) {
		writeErr(w, http.StatusBadRequest, errors.New("want op dismiss and a note id"))
		return
	}
	n, ok := s.docs.FindNote(q.ID)
	if !ok || n.Root != s.docsRoot(r.Context()) {
		writeErr(w, http.StatusNotFound, errors.New("no note "+q.ID))
		return
	}
	s.docs.RemoveNote(q.ID)
	writeJSON(w, map[string]bool{"ok": true})
}
```

(`var noteIDRE = regexp.MustCompile(`^t[0-9]{1,18}$`)` in `steer_notes.go`.)

`steer_notes.go`:

```go
package web

import (
	"context"
	"regexp"
	"strconv"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// The temporary-note verbs on the web (gg session note …): answered by the
// server from its store when no TUI is live — the TUI's steer_file_notes.go,
// word for word, except the other-worktree refusal (steer_files.go's words).

var noteIDRE = regexp.MustCompile(`^t[0-9]{1,18}$`)

func isNoteVerb(cmd string) bool {
	switch cmd {
	case "note_add", "note_list", "note_show", "note_rm":
		return true
	}
	return false
}

func (s *Server) steerNote(ctx context.Context, c steer.Command) steer.Reply {
	switch c.Cmd {
	case "note_add":
		return s.steerNoteAdd(ctx, c)
	case "note_list":
		return s.steerNoteList(ctx, c)
	case "note_show":
		return s.steerNoteShow(ctx, c)
	}
	return s.steerNoteRm(ctx, c)
}

// noteFile resolves the open file a verb names (id or path); "" name = none.
func (s *Server) noteFile(c steer.Command) (steer.OpenFile, string, bool) {
	name := c.FileID
	if name == "" {
		name = c.File
	}
	f, ok := s.ofs.resolve(s.service().Root(), c.FileID, c.File)
	return f, name, ok
}

func (s *Server) steerNoteAdd(ctx context.Context, c steer.Command) steer.Reply {
	s.steerMu.Lock()
	shown := s.steerWorktree
	s.steerMu.Unlock()
	if c.Worktree != "" && shown != "" && !domain.SameCheckout(c.Worktree, shown) {
		return steerFail(c, "gg web is showing worktree "+shown+", not "+c.Worktree)
	}
	svc := s.service()
	wt := svc.Root()
	path := c.File
	if c.FileID != "" {
		f, ok := s.ofs.resolve(wt, c.FileID, "")
		if !ok || f.ID != c.FileID {
			return steerFail(c, "no open file "+c.FileID)
		}
		if f.Source != "worktree" {
			return steerFail(c, "temporary notes go on working-tree files only")
		}
		path = f.Path
	} else {
		present, err := svc.WorktreeFilesPresent(ctx, []string{path})
		if err != nil {
			return steerFail(c, "checking "+path+": "+err.Error())
		}
		if !present[path] {
			return steerFail(c, path+" is not in the working tree")
		}
	}
	data, err := readVersion(ctx, svc, "worktree", "", path)
	if err != nil {
		return steerFail(c, "reading "+path+": "+err.Error())
	}
	k := ofKey{Src: "worktree", Path: path}
	f, ev, added := s.ofs.ensureOpen(wt, k, false) // the follow pass pins it once the note exists
	if added {
		s.baseline(wt, f.ID, k)
	}
	var lines []string
	if len(data) <= domain.MaxDiffBytes && !domain.IsBinary(data) {
		lines = agentdocs.Lines(data)
	}
	n, err := s.docs.AddNote(s.docsRoot(ctx), path, lines, c.Start, c.End, c.Summary, c.Rationale, c.Author)
	if err != nil {
		return steerFail(c, err.Error())
	}
	lead := ""
	if ev != "" {
		lead = "; closed " + ev + " (" + strconv.Itoa(maxOpenFiles) + " files open)"
	}
	r := steerOK(c, agentdocs.NotedDetail(n, lead))
	r.Notes = []steer.FileNote{agentdocs.NoteWire(n, f.ID, nil)}
	return r
}

func (s *Server) steerNoteList(ctx context.Context, c steer.Command) steer.Reply {
	root, wt := s.docsRoot(ctx), s.service().Root()
	paths := s.docs.NotedPaths(root)
	if c.FileID != "" || c.File != "" {
		f, name, ok := s.noteFile(c)
		if !ok {
			return steerFail(c, "no open file "+name)
		}
		paths = []string{f.Path}
	}
	r := steerOK(c, "")
	for _, p := range paths {
		f, _ := s.ofs.lookup(wt, ofKey{Src: "worktree", Path: p})
		ns, _ := s.docs.Notes(root, p)
		for _, n := range ns {
			r.Notes = append(r.Notes, agentdocs.NoteWire(n, f.ID, nil))
		}
	}
	if len(r.Notes) == 0 {
		r.Detail = "no notes"
	}
	return r
}

func (s *Server) steerNoteShow(ctx context.Context, c steer.Command) steer.Reply {
	n, ok := s.docs.FindNote(c.NoteID)
	if !ok || n.Root != s.docsRoot(ctx) {
		return steerFail(c, "no note "+c.NoteID)
	}
	f, _ := s.ofs.lookup(s.service().Root(), ofKey{Src: "worktree", Path: n.Path})
	r := steerOK(c, "")
	r.Notes = []steer.FileNote{agentdocs.NoteWire(n, f.ID, s.docs.NoteText(n.ID))}
	return r
}

func (s *Server) steerNoteRm(ctx context.Context, c steer.Command) steer.Reply {
	root := s.docsRoot(ctx)
	if c.NoteID != "" {
		if n, ok := s.docs.FindNote(c.NoteID); !ok || n.Root != root || !s.docs.RemoveNote(c.NoteID) {
			return steerFail(c, "no note "+c.NoteID)
		}
		return steerOK(c, "removed "+c.NoteID)
	}
	f, name, ok := s.noteFile(c)
	if !ok {
		return steerFail(c, "no open file "+name)
	}
	return steerOK(c, "removed "+strconv.Itoa(s.docs.ClearPath(root, f.Path))+" notes from "+f.Path)
}
```

`steer.go` `handleSteer`: right after the decode, BEFORE `toSteerWire`:

```go
	// The note verbs are answered HERE from the store (agentdocs): they never
	// move a screen, so neither the wire check (which knows only the hub's
	// verbs) nor the op gate applies.
	if isNoteVerb(c.Cmd) {
		writeJSON(w, s.steerNote(readCtx(r), c))
		return
	}
```

- [ ] **Step 4: Run them to see them pass**

Run: `go test ./internal/web/ -run 'Store|SteerNote|FileContentCarries|Dismiss|Pinned|FollowPass|OpenFiles|SteerFiles|SteerBackground' -count=1 2>&1 | tail -10`
Expected: `ok`.

- [ ] **Step 5: Whole web package** — `go test ./internal/web/ -count=1 > <ws>/web.log 2>&1; tail -5 <ws>/web.log` → `ok`.
- [ ] **Step 6: Commit** — `feat(web): gg web follows the agent-docs store — noted files pinned in the list, notes on the file read, dismiss, the note verbs answered`

---

### Task 5: The page — note boxes, gutter, keys, buttons, switcher count

**Files:**
- Modify: `internal/web/static/viewer.js`, `openfiles.js`, `live.js`, `style.css`
- Test: `internal/web/viewernotesjs_test.go` (new), `internal/web/openfilesjs_test.go` (switcher rows carry `notes`)

- [ ] **Step 1: Write the failing tests** — `internal/web/viewernotesjs_test.go`

```go
package web

import "testing"

const vnPureStart = "// --- note model (pure; guarded against Go) ---"
const vnPureEnd = "// --- end note model ---"

func TestViewerNoteModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", vnPureStart, vnPureEnd, `
const ns = [
  {id: "t2", start: 3, end: 4, author: "agent", outdated: false},
  {id: "t5", start: 9, end: 9, author: "claude", outdated: true},
];
console.log([
  noteBoxTitle(ns[0]), noteBoxTitle(ns[1]),
  (noteAtLine(ns, 4) || {}).id, String(noteAtLine(ns, 5)),
  [...boxesAfter(ns, 3)].map((n) => n.id).join("+"), boxesAfter(ns, 2).length,
  nextNoteLine(ns, 1, 1), nextNoteLine(ns, 3, 1), nextNoteLine(ns, 9, 1),
  nextNoteLine(ns, 9, -1), nextNoteLine(ns, 3, -1),
  nextNotedFile([{id: "f7", notes: 1}, {id: "f2", notes: 0}, {id: "f3", notes: 2}], "f3", 1),
  nextNotedFile([{id: "f7", notes: 1}, {id: "f3", notes: 2}], "f3", -1),
  nextNotedFile([{id: "f3", notes: 2}], "f3", 1),
].join("|"));
`)
	want := "note t2 · agent · lines 3–4|note t5 · claude · line 9 · outdated|t2|null|t2|0|3|9|0|3|0|f7|f7|"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
```

(`boxesAfter(notes, i)` = the notes whose box hangs under 0-based line `i` — those ending on line `i+1`. `nextNoteLine(notes, cur, dir)` = the start of the next (dir > 0) / previous note strictly after/before `cur`, 0 = none. `nextNotedFile(files, mine, dir)` = the id of the next/previous OTHER file with notes by id number, wrapping; "" = none.)

Update `TestSwitcherModelJS`'s fixture with `notes: 2` on the first file and its expected JSON with `"notes":2` / `"notes":0`.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/web/ -run 'ViewerNoteModelJS|SwitcherModelJS' -count=1`
Expected: FAIL — "the guarded section markers are gone" / JSON mismatch.

- [ ] **Step 3: Implement** — viewer.js, inside a new guarded section after the viewer model:

```js
// --- note model (pure; guarded against Go) ---
function noteBoxTitle(n) {
  const at = n.start === n.end ? "line " + n.start : "lines " + n.start + "–" + n.end;
  return "note " + n.id + " · " + n.author + " · " + at + (n.outdated ? " · outdated" : "");
}

function noteAtLine(notes, line) {
  return notes.find((n) => n.start <= line && line <= n.end) || null;
}

function boxesAfter(notes, i) {
  return notes.filter((n) => n.end - 1 === i);
}

function nextNoteLine(notes, cur, dir) {
  let hit = 0;
  for (const n of notes) {
    if (dir > 0 && n.start > cur) return n.start;
    if (dir < 0 && n.start < cur) hit = n.start;
  }
  return hit;
}

function nextNotedFile(files, mine, dir) {
  const num = (id) => Number(String(id).slice(1));
  const others = files.filter((f) => f.notes > 0 && f.id !== mine).sort((a, b) => num(a.id) - num(b.id));
  if (!others.length) return "";
  if (dir > 0) return (others.find((f) => num(f.id) > num(mine)) || others[0]).id;
  const before = others.filter((f) => num(f.id) < num(mine));
  return (before.length ? before[before.length - 1] : others[others.length - 1]).id;
}
// --- end note model ---
```

`view` gains `notes: []`; `openViewer` and `viewerFileChanged` set `view.notes = view.src === "worktree" ? body.notes || [] : []`; `renderViewer`'s line loop:

```js
    view.lines.forEach((l, i) => {
      const noted = noteAtLine(view.notes, i + 1) ? " vnoted" : "";
      html +=
        `<div class="vline${i + 1 === view.cur ? " vcur" : ""}${noted}" data-i="${i}"><span class="vno">${i + 1}</span>` +
        `<span class="vtext">${renderCell(l.text, null, l.tok, "", viewerSearch.query ? viewerSearch.hitsOn(i, 0) : null) || " "}</span></div>`;
      for (const n of boxesAfter(view.notes, i)) html += noteBoxHTML(n);
    });
```

```js
function noteBoxHTML(n) {
  return (
    `<div class="vnote${n.outdated ? " outdated" : ""}" data-note="${esc(n.id)}">` +
    `<div class="vnote-title">${esc(noteBoxTitle(n))}</div>` +
    `<div class="vnote-sum">${esc(n.summary)}</div>` +
    (n.rationale ? `<div class="vnote-why">${esc(n.rationale)}</div>` : "") +
    `<div class="vnote-acts"><button data-nact="dismiss">dismiss</button><button data-nact="ref">copy reference</button></div></div>`
  );
}

async function dismissNote(id) {
  try {
    await postJSON("/api/file-notes", { op: "dismiss", id });
  } catch (e) {
    return opLine("dismiss failed: " + (e.message || e), true);
  }
  view.notes = view.notes.filter((n) => n.id !== id);
  rerenderKeepingScroll();
  swapFoot(true);
  opLine("note " + id + " dismissed", false);
}

function copyNoteRef(n) {
  copyText(n.ref, "note reference " + n.id);
}

async function stepNote(dir) {
  const line = nextNoteLine(view.notes, view.cur, dir);
  if (line) {
    view.cur = line;
    paintCursor();
    cursorRow()?.nextElementSibling?.scrollIntoView({ block: "nearest" }); // the box too
    return;
  }
  let files = [];
  try {
    files = (await getJSON("/api/open-files")).files || [];
  } catch {}
  const id = nextNotedFile(files, view.id, dir);
  if (!id) return opLine(view.notes.length ? "no more notes" : "no notes", false);
  const r = await openViewer({ id });
  const t = dir > 0 ? view.notes[0] : view.notes[view.notes.length - 1];
  if (r.ok && t) {
    view.cur = t.start;
    paintCursor();
    centerCursor();
  }
}
```

(`rerenderKeepingScroll` = the save/restore of `scrollTop`/`scrollLeft` around `renderViewer()` that the search bar's `render` already does — extract it and reuse in both.)

Clicks in `#viewer-body`: before the `.vline` lookup,

```js
  const act = e.target.closest("button[data-nact]");
  if (act) {
    const n = view.notes.find((x) => x.id === act.closest(".vnote")?.dataset.note);
    if (n) act.dataset.nact === "dismiss" ? dismissNote(n.id) : copyNoteRef(n);
    return;
  }
```

`viewerKey` switch gains:

```js
    case "d": { const n = noteAtLine(view.notes, view.cur); if (!n) return false; dismissNote(n.id); break; }
    case "r": { const n = noteAtLine(view.notes, view.cur); if (!n) return false; copyNoteRef(n); break; }
    case "}": stepNote(1); break;
    case "{": stepNote(-1); break;
    case "Escape": closeViewer(view.notes.length ? "background" : "close"); break;
```

(The server makes the same call on a plain close — Task 4 — so a note that arrived after this read is covered too.)

Footer: `VIEWER_FOOT` becomes a function `viewerFoot()` that inserts `<span>} { notes</span><button data-vact="dismiss">d dismiss</button><button data-vact="ref">r reference</button>` after the find chip while `view.notes.length`; `swapFoot(true)` pushes `viewerFoot()`; call `swapFoot(true)` after every notes change (open, refresh, dismiss). Foot clicks: `case "dismiss"` / `case "ref"` act on `noteAtLine(view.notes, view.cur)`. KEEP the literal `ctrl+\\ open files` (TestViewerFootChipKeepsTheBackslash).

Refresh: export `viewerAgentDocs(files)` = `viewerOpenFiles(files); if (viewerFileId()) viewerFileChanged(viewerFileId());` and in `live.js`:

```js
    if (msg.reason === "agentdocs") {
      viewerAgentDocs(msg.files || []);
      switcherOpenFiles(msg.files || []);
      return;
    }
```

`viewerFileChanged` re-renders with `swapFoot(true)` when the viewer is open.

openfiles.js `switcherRows` adds `notes: f.notes || 0`; the files row meta: `r.line + "  " + versionLabel(r.source, r.rev) + (r.notes ? "  · " + r.notes + (r.notes === 1 ? " note" : " notes") : "")`.

Help: the viewer's `registerHelp` html gains `, <b>} {</b> agent notes, <b>d</b> dismiss, <b>r</b> copy reference`.

style.css (next to `.vline`):

```css
.vline.vnoted .vno { box-shadow: inset -3px 0 0 var(--accent); }
.vnote { margin: 4px 0 6px 4.6em; padding: 6px 10px; border: 1px solid var(--accent); border-radius: 4px; background: var(--bg-alt); white-space: pre-wrap; }
.vnote.outdated { opacity: .6; }
.vnote-title { color: var(--accent); font-size: .9em; margin-bottom: 4px; }
.vnote-why { margin-top: 6px; color: var(--dim); }
.vnote-acts { margin-top: 6px; display: flex; gap: 8px; }
```

(Use whatever accent variable `style.css` already defines — check `:root` for `--accent`; reuse the review note box colour if one exists.)

- [ ] **Step 4: Run them to see them pass** — `go test ./internal/web/ -run 'JS' -count=1` → `ok`.
- [ ] **Step 5: Browser check (Review Focus 2/3, visibility asserted)** — rebuild `bin/gg` in the worktree; run `gg web --addr 127.0.0.1:0` on a scratch repo; `gg session note add a.txt:2 --summary hi`; playwright: open the viewer on a.txt, assert `.vnote` is VISIBLE (not just present) under line 2 and `.vline.vnoted` exists; press `d`, assert the box is gone; add again, press esc, assert the switcher still lists a.txt with "· 1 note". Run the same script against `~/go/bin/gg` (unfixed) first and see it fail.
- [ ] **Step 6: Commit** — `feat(web): the file viewer shows an agent's notes — boxes, gutter, d/r/}/{, dismiss and copy buttons, the switcher's count`

---

### Task 6: e2e scenario, docs, skill

**Files:**
- Create: `e2e/scenarios/s1NN_session_note_web.toml` (next free number)
- Modify: `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, `internal/agentskill/using-gg.md` + `agentskill.Version` 107 → 108, `.claude/skills/using-gg/SKILL.md` (regenerate), `CLAUDE.md` (package-map row `agentdocs`)

- [ ] **Step 1: Write the scenario**

```toml
name = "session note with only gg web live: the web server answers from its store"

[input]
web = true
steps = [
  { write = "a.txt", content = "alpha\nbravo\ncharlie\n" },
  { commit = "seed" },
]

[[run]]
cmd             = ["session", "note", "add", "a.txt:2", "--summary", "look at bravo"]
exit            = 0
stdout_contains = ["noted a.txt:2 as t1"]

[[run]]
cmd             = ["session", "note", "list"]
exit            = 0
stdout_contains = ["t1\ta.txt\t2-2\tlook at bravo"]

[[run]]
cmd             = ["session", "files"]
exit            = 0
stdout_contains = ["a.txt"]

[[run]]
cmd             = ["session", "note", "show", "t1"]
exit            = 0
stdout_contains = ["2\tbravo"]

[[run]]
cmd             = ["session", "note", "rm", "t1"]
exit            = 0
stdout_contains = ["removed t1"]

[[run]]
cmd             = ["session", "note", "rm", "t1"]
exit            = 1
stderr_contains = ["no note t1"]

[expect]
branch = "main"
```

- [ ] **Step 2: Run it** — `go test ./e2e/ -run 'Scenarios/s1NN' -count=1` → `ok` (fails before Tasks 3–4).
- [ ] **Step 3: Docs** — CHANGELOG entry on top ("Agent notes in gg web"); README (notes show in the web viewer, keys); CLAUDE-details (the store, routing, `pinned`, the known limits: a TUI + a separate-process gg web; bare-CR files); skill: the note verbs work with only gg web live, bump to 108; regenerate the dogfood `SKILL.md` (throwaway generator printing `agentskill.SkillFile()`, removed after) and check it equals `SkillFile()`; CLAUDE.md row:

`| \`agentdocs\` | Memory-only store of what an agent shows beside the code (temporary notes on open working-tree files; overviews in plan 2): ids, alignment to file content, limits, reply prose, a change signal. One per process (\`Shared()\`) is shared by the TUI and the page it hosts; a standalone gg web keeps its own. DAG leaf (steer, textdiff). |`

- [ ] **Step 4: Full gate** — `./test.sh race > <ws>/race.log 2>&1; tail -3 <ws>/race.log` → must say "all green".
- [ ] **Step 5: Commit** — `docs: agent notes in gg web — changelog, readme, details, skill v108, package map`

---

## Rulings made while writing the plan (deviations from the spec)

- **No `GET /api/file-notes`.** The page refreshes by re-reading `/api/file-content`, which aligns and returns lines + notes in one answer — the page can then never pair notes with lines from another read (Review Focus 1). Cost if wrong: one extra read per change of the shown file.
- **`agentdocs` imports `textdiff`** (the alignment engine the rule needs). The spec's allowlist omitted it.
- **The TUI reads `agentdocs.Shared()` for open-file ids even when a test swaps `m.docs`** — `newOpenFile` has no Model. Overview ids (plan 2) come from the same counter.
- **The web root is the served worktree's TOPLEVEL** (cached per service), not `svc.Root()`.
- **Canonical lines** (`agentdocs.Lines`) are the TUI's split; the web page's own display split differs for bare-CR files only.
