# gg session docs — plan 1 of 4: the file layer in `agentdocs` + conformance material

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo: NO implementer subagents; one read-only review subagent at the end). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `internal/agentdocs` can read, watch and write the per-worktree
`docs/` directory of session documents (overview `.md` with TOML front
matter, note `.toml` with anchor lines) behind its existing store API, and
the format is published as conformance material — with NO change to any
frontend yet.

**Architecture:** The one `agentdocs.Store` stays. A root (the store's
per-worktree key) may be ATTACHED to a docs directory: the store loads the
files, watches them (fsnotify + 2 s poll), reconciles by id on every wake,
and on an attached root every mutation (`AddNote*`, `AddOverview`,
`SetOverview`, `Remove*`, `ClearPath`, `FileTour`) writes a file first and
then reconciles. Unattached roots behave exactly as today (memory), so every
existing test and every frontend keeps working until plans 2–4 attach them.
Pure pieces first (ids, parse/encode, cold anchoring, directory I/O), the
store last.

**Tech Stack:** Go 1.26, `github.com/pelletier/go-toml/v2` (already a dep),
`fsnotify` (already), `github.com/google/jsonschema-go` (already, tests
only). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-10-18-gg-session-docs-design.md`
(§2 directory, §3 documents, §3.4 limits, §4.2 reconcile, §4.3 cold
anchoring, §5 writers, §6.1 store, §9 conformance).

**The series:** plan 1 (this) core + conformance · plan 2 TUI reader (session
overview rows, close vs delete, tours as files) · plan 3 gg web reader ·
plan 4 CLI direct write, `docs path|ls`, steer verb retirement, skills,
plugin docs. Each plan merges on its own; after plan 1 nothing user-visible
changes.

## Global Constraints

- Worktree `/work/gigagit/.claude/worktrees/gg-session-docs`, branch
  `spec/gg-session-docs` (off `main` at `ebeffae8`). Every command runs
  there with absolute paths. Commit with `gg add <files>` then
  `git commit -F <msgfile>`; never `git add -A`.
- `internal/agentdocs` stays a DAG leaf: imports `markdown`, `steer`,
  `textdiff`, `notemeta`, stdlib, `go-toml/v2`. Never `config`, `domain`,
  `git`.
- Error strings are English protocol prose, lower case, no trailing period,
  exactly as written in this plan (the CLI and the frontends print them).
- Limits (spec §3.4): id `^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`; a document file
  ≤ 512 KiB (`steer.MaxCommandBytes`); overview body ≤ 64 KiB
  (`MaxOverviewBytes`), title ≤ 200 chars one line, ≤ 100 anchors; summary
  ≤ 500, rationale ≤ 4000; 50 notes shown per path (`MaxNotesPerFile`); no
  cap on overviews of an attached root.
- Reader ignore rules (spec §2, §3.4): name starts with `.`, ends `.tmp`,
  extension not `.md`/`.toml`, a directory, over 512 KiB, unparseable, a
  missing required key, an invalid id → the entry is skipped; the store
  records `(name, reason)` in `Problems(root)` for debugging and never
  deletes, renames or rewrites it.
- Write (spec §5.1): `MkdirAll(dir, 0o755)`; temp `.<id>.<rand>.tmp` in
  the SAME directory; `os.Rename` over the final name. Never in place.
- Tests use `t.TempDir()` and `t.Parallel()`; the watch tests wait with a
  deadline (≤ 5 s), never a fixed sleep; the poll fallback is forced by a
  test hook, not by breaking fsnotify.
- `./test.sh race` must print "all green" before the final review.

## Review Focus

1. A note file whose `lines` count does not match `end - start + 1` must be
   refused on write and skipped on read (never shown with a wrong span) —
   Task 2's parse test and Task 3's list test pin it.
2. A directory replaced under a running store (rm -rf + recreate, as a
   state-dir cleanup does) must not crash the watcher or leave ghosts: after
   the next poll the root shows exactly what the directory holds — Task 4's
   "directory recreated" test.
3. Two writers minting ids in the same millisecond must not collide:
   `MintID` draws 4 random hex digits per call and a `Write` of a NEW
   document uses `O_EXCL` on the final name, retrying the suffix — Task 3's
   collision test.
4. A note on CRLF content must round-trip to the same `lines` as LF content
   (`Lines` strips `\r`) and place at the same span — Task 2's CRLF test
   and the CRLF example in Task 5.
5. `created` written by a writer in a non-UTC zone must parse and order
   correctly; a file without `created` orders by mtime and never panics —
   Task 2's parse tests (offset time, missing created) and Task 4's order
   test.

---

### Task 1: `steer.DocsDir` and a filtered inbox watcher

**Files:**
- Modify: `internal/steer/steer.go` (next to `IDEDir`, line 244)
- Modify: `internal/steer/watch.go`
- Test: `internal/steer/docs_test.go` (new), `internal/steer/watch_test.go` (new)

**Interfaces:**
- Produces: `const DocsInbox = "docs"`; `func DocsDir(inbox string) string`;
  `func WatchFilter(dir string, keep func(name string) bool) (*Watcher, error)`
  — `Watch(dir)` becomes `WatchFilter(dir, isCommandFile)` and keeps its
  signature. A `Watcher` from `WatchFilter` wakes on Create, Write, Rename
  AND Remove of a kept name (a document removed is an event too; a command
  file removed was never one, so `Watch`'s behaviour for commands is kept
  by `isCommandFile` only — the extra Remove wake on a `cmd-` file is
  harmless: the consumer drains and finds nothing).

- [ ] **Step 1: Write the failing tests**

```go
// internal/steer/docs_test.go
package steer

import (
	"path/filepath"
	"testing"
)

func TestDocsDirIsTheDocsSubdirectoryOfTheInbox(t *testing.T) {
	t.Parallel()
	if got := DocsDir(filepath.Join("x", "inbox")); got != filepath.Join("x", "inbox", "docs") {
		t.Fatalf("DocsDir = %q", got)
	}
	if DocsInbox != "docs" {
		t.Fatalf("DocsInbox = %q", DocsInbox)
	}
}
```

```go
// internal/steer/watch_test.go
package steer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func wakes(t *testing.T, w *Watcher) bool {
	t.Helper()
	select {
	case <-w.Events():
		return true
	case <-time.After(3 * time.Second):
		return false
	}
}

func TestWatchFilterWakesOnKeptNamesOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := WatchFilter(dir, func(name string) bool { return strings.HasSuffix(name, ".md") })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if wakes(t, w) {
		t.Fatal("a name the filter drops must not wake")
	}
	if err := os.WriteFile(filepath.Join(dir, ".tmp-1"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, ".tmp-1"), filepath.Join(dir, "a.md")); err != nil {
		t.Fatal(err)
	}
	if !wakes(t, w) {
		t.Fatal("a rename onto a kept name must wake")
	}
	if err := os.Remove(filepath.Join(dir, "a.md")); err != nil {
		t.Fatal(err)
	}
	if !wakes(t, w) {
		t.Fatal("removing a kept name must wake")
	}
}

func TestWatchKeepsCommandFilesOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w, err := Watch(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := os.WriteFile(filepath.Join(dir, "tui.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if wakes(t, w) {
		t.Fatal("a presence file must not wake the command watcher")
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd-1.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !wakes(t, w) {
		t.Fatal("a command file must wake")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/gg-session-docs && go test ./internal/steer/ -run 'TestDocsDir|TestWatchFilter|TestWatchKeeps' 2>&1 | head -20`
Expected: FAIL to compile — `undefined: DocsDir`, `undefined: DocsInbox`, `undefined: WatchFilter`.

- [ ] **Step 3: Implement**

In `internal/steer/steer.go`, after the `IDEInbox` constant:

```go
	// DocsInbox is the sub-directory of session documents (spec
	// 2026-10-18-gg-session-docs §2): <id>.md overviews and <id>.toml notes
	// an agent writes and every reader of the worktree shows.
	DocsInbox = "docs"
```

and after `IDEDir`:

```go
// DocsDir is the worktree's session-documents directory, under its inbox.
func DocsDir(inbox string) string { return filepath.Join(inbox, DocsInbox) }
```

In `internal/steer/watch.go` replace `Watch` and the loop's filter:

```go
// Watcher wakes a consumer when a file it cares about changes in one
// directory. It is the FAST path only — every consumer also polls the dir
// on its own tick, so a Watch that fails (no inotify watches left, an
// exotic filesystem) degrades to a one- or two-second latency rather than
// to a broken feature.
type Watcher struct {
	fsw  *fsnotify.Watcher
	keep func(name string) bool
	out  chan struct{}
	done chan struct{}

	mu     sync.Mutex
	closed bool
}

// Watch creates dir if needed and starts watching it for command files.
func Watch(dir string) (*Watcher, error) { return WatchFilter(dir, isCommandFile) }

// isCommandFile is Watch's filter: presence files are re-touched once a
// second by every session on an inbox; only a command is worth a wake.
func isCommandFile(name string) bool { return strings.HasPrefix(name, cmdPrefix) }

// WatchFilter creates dir if needed and wakes on every create, write,
// rename or remove of a name keep accepts (the base name). The session-docs
// store watches its directory with it.
func WatchFilter(dir string, keep func(name string) bool) (*Watcher, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fsw.Add(dir); err != nil {
		fsw.Close()
		return nil, err
	}
	w := &Watcher{fsw: fsw, keep: keep, out: make(chan struct{}, 1), done: make(chan struct{})}
	go w.loop()
	return w, nil
}
```

and in `loop()`:

```go
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			if !w.keep(filepath.Base(ev.Name)) {
				continue
			}
			w.wake()
```

- [ ] **Step 4: Run the package tests**

Run: `cd /work/gigagit/.claude/worktrees/gg-session-docs && go test ./internal/steer/ 2>&1 | tail -5`
Expected: `ok  	github.com/homeend/gigagit/internal/steer`.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/gg-session-docs && gg add internal/steer/steer.go internal/steer/watch.go internal/steer/docs_test.go internal/steer/watch_test.go && git commit -q -m "steer: DocsDir and a filtered directory watcher for the session-docs store" && git log --oneline -1
```

---

### Task 2: Document shapes — ids, overview front matter, note TOML, cold anchoring

**Files:**
- Create: `internal/agentdocs/docfile.go`
- Create: `internal/agentdocs/cold.go`
- Create: `docs/specs/gg-session-docs/cold-anchor-cases.json`
- Create: `docs/specs/gg-session-docs/id-vectors.json`
- Test: `internal/agentdocs/docfile_test.go`, `internal/agentdocs/cold_test.go`

**Interfaces:**
- Produces (all in `package agentdocs`):

```go
// Kinds of session document.
const (
	KindOverview = "overview" // <id>.md
	KindNote     = "note"     // <id>.toml
)

const (
	MaxDocBytes   = steer.MaxCommandBytes // 512 KiB: a larger file is ignored unread
	DocFormat     = 1
	idMaxLen      = 64
)

// ValidID reports whether id matches ^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$.
func ValidID(id string) bool

// MintID is "<UTC yyyymmddThhmmss>-<4 hex>" from now and 2 random bytes.
func MintID(now time.Time, rnd io.Reader) string

// DocName splits a directory entry: id and kind for "<id>.md" / "<id>.toml"
// with a valid id; ok=false for everything a reader ignores (dot files,
// *.tmp, other extensions, invalid ids).
func DocName(name string) (id, kind string, ok bool)

// FileName is the inverse: "<id>.md" or "<id>.toml".
func FileName(id, kind string) string

// OverviewFile is a parsed <id>.md.
type OverviewFile struct {
	Format  int
	Title   string
	Author  string    // "" in the file = "agent" after parse
	Created time.Time // zero when the file has none
	Text    string    // the body, as written
}

// NoteFile is a parsed <id>.toml.
type NoteFile struct {
	Format    int
	Path      string
	Start     int
	End       int
	Summary   string
	Rationale string
	Author    string
	Created   time.Time
	Lines     []string
	Meta      []notemeta.KV
	Display   notemeta.Display
}

// ParseOverviewFile / ParseNoteFile validate as spec §3; the error is protocol prose.
func ParseOverviewFile(data []byte) (OverviewFile, error)
func ParseNoteFile(data []byte) (NoteFile, error)

// EncodeOverviewFile / EncodeNoteFile write the canonical spelling (what the
// examples fixture proves). Author "" is written as "agent"; a zero Created
// is omitted; Format is always written.
func EncodeOverviewFile(o OverviewFile) ([]byte, error)
func EncodeNoteFile(n NoteFile) ([]byte, error)

// ColdPlace places anchor lines in file content never seen before (spec
// §4.3): at start when they are there, else at their one occurrence, else
// outdated at start..end clamped to the file.
func ColdPlace(file []string, start, end int, anchor []string) (s, e int, outdated bool)
```

- Front matter grammar: line 1 is exactly `+++` (a trailing `\r` allowed);
  the next line that is exactly `+++` closes it; the TOML is between; the
  body is everything after the closing line's `\n`. Missing opener or
  closer → `an overview file starts with a +++ front matter block`.
- Parse errors (exact strings): `an overview needs a title`; `the title must
  be one line of at most 200 characters`; `an overview needs text`; `the
  text is over 64 KiB`; `a note needs a path`; `path must be a relative
  slash path inside the worktree` (leading `/`, `\`, `:`, a `.` or `..`
  segment, empty segment); `a line number is 1-based`; `the range ends
  before it starts`; `a note needs a summary`; `the summary is longer than
  500 characters`; `the rationale is longer than 4000 characters`; `lines
  must hold end-start+1 entries (got N, want M)`; a TOML syntax error →
  `toml: <go-toml's message>`; `created` that is not a date-time → `created
  must be an RFC 3339 date-time`.
- Meta: `notemeta.CheckMeta` (the part-2 limits) on parse — the checked
  list is kept, problems dropped (a reader shows what it can); on encode
  the list is written as given.

- [ ] **Step 1: Write the failing tests**

```go
// internal/agentdocs/docfile_test.go
package agentdocs

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/notemeta"
)

func TestMintIDShapeAndUniqueness(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 18, 14, 32, 1, 0, time.FixedZone("CET", 3600))
	id := MintID(now, bytes.NewReader([]byte{0x9f, 0x3a}))
	if id != "20261018T133201-9f3a" { // UTC
		t.Fatalf("MintID = %q", id)
	}
	if !ValidID(id) {
		t.Fatal("a minted id must be valid")
	}
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		x := MintID(now, nil) // nil = crypto/rand
		if seen[x] {
			t.Fatalf("collision %q", x)
		}
		seen[x] = true
	}
}

func TestIDVectors(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "specs", "gg-session-docs", "id-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Valid   []string `json:"valid"`
		Invalid []string `json:"invalid"`
		Names   []struct {
			Name string `json:"name"`
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"names"`
		Ignored []string `json:"ignored"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	for _, id := range v.Valid {
		if !ValidID(id) {
			t.Errorf("%q must be valid", id)
		}
	}
	for _, id := range v.Invalid {
		if ValidID(id) {
			t.Errorf("%q must be invalid", id)
		}
	}
	for _, n := range v.Names {
		id, kind, ok := DocName(n.Name)
		if !ok || id != n.ID || kind != n.Kind {
			t.Errorf("DocName(%q) = %q %q %v, want %q %q", n.Name, id, kind, ok, n.ID, n.Kind)
		}
		if FileName(n.ID, n.Kind) != n.Name {
			t.Errorf("FileName(%q,%q) = %q", n.ID, n.Kind, FileName(n.ID, n.Kind))
		}
	}
	for _, n := range v.Ignored {
		if _, _, ok := DocName(n); ok {
			t.Errorf("%q must be ignored", n)
		}
	}
}

func TestOverviewFileRoundTrip(t *testing.T) {
	t.Parallel()
	in := OverviewFile{Title: "How scroll coalescing works", Author: "claude",
		Created: time.Date(2026, 10, 18, 14, 32, 1, 0, time.UTC),
		Text:    "Scroll events are coalesced.\n\n1. [The handler](internal/tui/scroll.go:40-58) …\n"}
	data, err := EncodeOverviewFile(in)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"+++\nformat = 1\n", "title = 'How scroll coalescing works'\n", "author = 'claude'\n", "created = 2026-10-18T14:32:01Z\n", "+++\nScroll events"} {
		if !strings.Contains(s, want) {
			t.Errorf("encoded file lacks %q:\n%s", want, s)
		}
	}
	out, err := ParseOverviewFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if out.Format != 1 || out.Title != in.Title || out.Author != in.Author || !out.Created.Equal(in.Created) || out.Text != in.Text {
		t.Fatalf("round trip = %+v", out)
	}
}

func TestOverviewFileDefaultsAndRefusals(t *testing.T) {
	t.Parallel()
	o, err := ParseOverviewFile([]byte("+++\ntitle = \"T\"\n+++\nbody\n"))
	if err != nil || o.Author != "agent" || !o.Created.IsZero() || o.Format != 1 || o.Text != "body\n" {
		t.Fatalf("minimal file: %+v %v", o, err)
	}
	o, err = ParseOverviewFile([]byte("+++\r\ntitle = \"T\"\r\ncreated = 2026-10-18T16:32:01+02:00\r\n+++\r\nbody"))
	if err != nil || !o.Created.Equal(time.Date(2026, 10, 18, 14, 32, 1, 0, time.UTC)) {
		t.Fatalf("CRLF + offset: %+v %v", o, err)
	}
	for in, want := range map[string]string{
		"title = \"T\"\nbody":                                  "an overview file starts with a +++ front matter block",
		"+++\ntitle = \"T\"\nbody":                             "an overview file starts with a +++ front matter block",
		"+++\nauthor = \"a\"\n+++\nbody":                       "an overview needs a title",
		"+++\ntitle = \"" + strings.Repeat("x", 201) + "\"\n+++\nbody": "the title must be one line of at most 200 characters",
		"+++\ntitle = \"T\"\n+++\n   \n":                      "an overview needs text",
		"+++\ntitle = \"T\"\n+++\n" + strings.Repeat("x", 64<<10+1): "the text is over 64 KiB",
		"+++\ntitle = \n+++\nbody":                             "toml: ",
		"+++\ntitle = \"T\"\ncreated = \"yesterday\"\n+++\nbody": "created must be an RFC 3339 date-time",
	} {
		_, err := ParseOverviewFile([]byte(in))
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%q: err = %v, want prefix %q", in[:min(30, len(in))], err, want)
		}
	}
}

func TestNoteFileRoundTripWithMetaAndDisplay(t *testing.T) {
	t.Parallel()
	in := NoteFile{Path: "internal/tui/scroll.go", Start: 102, End: 104,
		Summary: "One lock around flush.", Rationale: "Without it …\nsecond line", Author: "claude",
		Created: time.Date(2026, 10, 18, 14, 32, 5, 0, time.UTC),
		Lines:   []string{"func (s *scroller) flush() {", "\ts.mu.Lock()", "\tdefer s.mu.Unlock()"},
		Meta:    []notemeta.KV{{Key: "kind", Value: "design"}},
		Display: notemeta.Display{"kind": {Show: []string{"badge"}, Values: map[string]notemeta.Value{"design": {Color: "blue"}}}}}
	data, err := EncodeNoteFile(in)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{"format = 1\n", "path = 'internal/tui/scroll.go'\n", "start = 102\n", "end = 104\n", "lines = [", "[[meta]]\nkey = 'kind'\nvalue = 'design'\n", "[display.kind]\nshow = ['badge']\n", "[display.kind.values.design]\ncolor = 'blue'\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("encoded note lacks %q:\n%s", want, s)
		}
	}
	out, err := ParseNoteFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if out.Path != in.Path || out.Start != 102 || out.End != 104 || out.Summary != in.Summary || out.Rationale != in.Rationale ||
		out.Author != "claude" || !out.Created.Equal(in.Created) || strings.Join(out.Lines, "|") != strings.Join(in.Lines, "|") ||
		len(out.Meta) != 1 || out.Meta[0].Value != "design" || out.Display["kind"].Values["design"].Color != "blue" {
		t.Fatalf("round trip = %+v", out)
	}
}

func TestNoteFileDefaultsAndRefusals(t *testing.T) {
	t.Parallel()
	min := "path = \"a.go\"\nstart = 2\nend = 2\nsummary = \"s\"\nlines = [\"x\"]\n"
	n, err := ParseNoteFile([]byte(min))
	if err != nil || n.Author != "agent" || n.Format != 1 || n.Rationale != "" || len(n.Meta) != 0 || n.Display != nil {
		t.Fatalf("minimal note: %+v %v", n, err)
	}
	// CRLF inside lines is stripped on encode so LF and CRLF content agree.
	data, _ := EncodeNoteFile(NoteFile{Path: "a.go", Start: 1, End: 2, Summary: "s", Lines: []string{"a\r", "b\r"}})
	if n, err := ParseNoteFile(data); err != nil || strings.Join(n.Lines, "|") != "a|b" {
		t.Fatalf("CRLF lines: %+v %v", n, err)
	}
	for name, tc := range map[string]struct{ in, want string }{
		"no path":      {"start = 1\nend = 1\nsummary = \"s\"\nlines = [\"x\"]\n", "a note needs a path"},
		"abs path":     {"path = \"/a.go\"\nstart = 1\nend = 1\nsummary = \"s\"\nlines = [\"x\"]\n", "path must be a relative slash path inside the worktree"},
		"dotdot":       {"path = \"a/../b.go\"\nstart = 1\nend = 1\nsummary = \"s\"\nlines = [\"x\"]\n", "path must be a relative slash path inside the worktree"},
		"backslash":    {"path = \"a\\\\b.go\"\nstart = 1\nend = 1\nsummary = \"s\"\nlines = [\"x\"]\n", "path must be a relative slash path inside the worktree"},
		"zero start":   {"path = \"a.go\"\nstart = 0\nend = 1\nsummary = \"s\"\nlines = [\"x\", \"y\"]\n", "a line number is 1-based"},
		"end<start":    {"path = \"a.go\"\nstart = 3\nend = 2\nsummary = \"s\"\nlines = [\"x\"]\n", "the range ends before it starts"},
		"no summary":   {"path = \"a.go\"\nstart = 1\nend = 1\nsummary = \"  \"\nlines = [\"x\"]\n", "a note needs a summary"},
		"long summary": {"path = \"a.go\"\nstart = 1\nend = 1\nsummary = \"" + strings.Repeat("s", 501) + "\"\nlines = [\"x\"]\n", "the summary is longer than 500 characters"},
		"long rationale": {"path = \"a.go\"\nstart = 1\nend = 1\nsummary = \"s\"\nrationale = \"" + strings.Repeat("r", 4001) + "\"\nlines = [\"x\"]\n", "the rationale is longer than 4000 characters"},
		"lines count":  {"path = \"a.go\"\nstart = 1\nend = 2\nsummary = \"s\"\nlines = [\"x\"]\n", "lines must hold end-start+1 entries (got 1, want 2)"},
		"no lines":     {"path = \"a.go\"\nstart = 1\nend = 1\nsummary = \"s\"\n", "lines must hold end-start+1 entries (got 0, want 1)"},
		"bad toml":     {"path = \n", "toml: "},
		"bad created":  {"path = \"a.go\"\nstart = 1\nend = 1\nsummary = \"s\"\nlines = [\"x\"]\ncreated = \"now\"\n", "created must be an RFC 3339 date-time"},
	} {
		_, err := ParseNoteFile([]byte(tc.in))
		if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want prefix %q", name, err, tc.want)
		}
	}
}
```

```go
// internal/agentdocs/cold_test.go
package agentdocs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The published cold-anchoring cases (spec §4.3 / §9): a reader that never
// saw the writer's file places a note from its lines alone.
func TestColdAnchorCases(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "specs", "gg-session-docs", "cold-anchor-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name string   `json:"name"`
		File []string `json:"file"`
		Note struct {
			Start, End int
			Lines      []string `json:"lines"`
		} `json:"note"`
		Expect struct {
			Start, End int
			Outdated   bool `json:"outdated"`
		} `json:"expect"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 7 {
		t.Fatalf("%d cases; the spec names at least 7", len(cases))
	}
	for _, c := range cases {
		s, e, out := ColdPlace(c.File, c.Note.Start, c.Note.End, c.Note.Lines)
		if s != c.Expect.Start || e != c.Expect.End || out != c.Expect.Outdated {
			t.Errorf("%s: got %d-%d outdated=%v, want %d-%d outdated=%v", c.Name, s, e, out, c.Expect.Start, c.Expect.End, c.Expect.Outdated)
		}
	}
}
```

- [ ] **Step 2: Write the two JSON files**

`docs/specs/gg-session-docs/cold-anchor-cases.json`:

```json
[
  {"name": "lines at start: current", "file": ["a", "b", "c", "d"], "note": {"start": 2, "end": 3, "lines": ["b", "c"]}, "expect": {"start": 2, "end": 3, "outdated": false}},
  {"name": "moved down, unique: follows", "file": ["x", "a", "b", "c", "d"], "note": {"start": 2, "end": 3, "lines": ["b", "c"]}, "expect": {"start": 3, "end": 4, "outdated": false}},
  {"name": "moved up, unique: follows", "file": ["b", "c", "d"], "note": {"start": 2, "end": 3, "lines": ["b", "c"]}, "expect": {"start": 1, "end": 2, "outdated": false}},
  {"name": "at start AND elsewhere: start wins", "file": ["b", "a", "b", "c"], "note": {"start": 3, "end": 3, "lines": ["b"]}, "expect": {"start": 3, "end": 3, "outdated": false}},
  {"name": "twice, neither at start: outdated, numbers kept", "file": ["b", "a", "b", "c"], "note": {"start": 2, "end": 2, "lines": ["b"]}, "expect": {"start": 2, "end": 2, "outdated": true}},
  {"name": "edited: outdated, numbers kept", "file": ["a", "B", "c"], "note": {"start": 2, "end": 2, "lines": ["b"]}, "expect": {"start": 2, "end": 2, "outdated": true}},
  {"name": "partial match of a block: outdated", "file": ["a", "b", "x", "c"], "note": {"start": 2, "end": 3, "lines": ["b", "c"]}, "expect": {"start": 2, "end": 3, "outdated": true}},
  {"name": "file shorter than the note: clamped", "file": ["a"], "note": {"start": 3, "end": 4, "lines": ["c", "d"]}, "expect": {"start": 1, "end": 1, "outdated": true}},
  {"name": "file shorter than end only: clamped end", "file": ["a", "b", "x"], "note": {"start": 3, "end": 4, "lines": ["c", "d"]}, "expect": {"start": 3, "end": 3, "outdated": true}},
  {"name": "empty file: outdated at 1-1", "file": [], "note": {"start": 5, "end": 6, "lines": ["e", "f"]}, "expect": {"start": 1, "end": 1, "outdated": true}}
]
```

`docs/specs/gg-session-docs/id-vectors.json`:

```json
{
  "about": "spec 2026-10-18-gg-session-docs §3.1: ids match ^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$; the file name is <id>.md (overview) or <id>.toml (note); everything in `ignored` is skipped by a reader",
  "valid": ["20261018T143201-9f3a", "a", "A-b_c", "tour-report-gg-12345-7", "0123456789012345678901234567890123456789012345678901234567890123"],
  "invalid": ["", "-a", "_a", ".a", "a.b", "a b", "a/b", "a\\b", "ä", "01234567890123456789012345678901234567890123456789012345678901234"],
  "names": [
    {"name": "20261018T143201-9f3a.md", "id": "20261018T143201-9f3a", "kind": "overview"},
    {"name": "20261018T143205-c0d1.toml", "id": "20261018T143205-c0d1", "kind": "note"},
    {"name": "tour-report-gg-12345-7.md", "id": "tour-report-gg-12345-7", "kind": "overview"}
  ],
  "ignored": [".x.md", ".20261018T143201-9f3a.ab12.tmp", "x.md.tmp", "x.txt", "x.MD", "x.json", "x", "-x.md", "a.b.md", "x.toml.bak"]
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/gg-session-docs && go test ./internal/agentdocs/ -run 'TestMintID|TestIDVectors|TestOverviewFile|TestNoteFile|TestColdAnchor' 2>&1 | head`
Expected: FAIL to compile — `undefined: MintID`, `ValidID`, `DocName`, `FileName`, `OverviewFile`, `NoteFile`, `ColdPlace`, ….

- [ ] **Step 4: Implement `cold.go`**

```go
package agentdocs

// Cold anchoring (spec 2026-10-18-gg-session-docs §4.3): a reader meets a
// note file without ever having seen the writer's copy of the file, so it
// places the note from its anchor lines alone. After this first placement
// the reader keeps its own content and re-anchors with reanchor as before.

// ColdPlace places anchor in file: at start when it is there, else at its
// one occurrence, else outdated at start..end clamped into the file (an
// empty file clamps to 1..1).
func ColdPlace(file []string, start, end int, anchor []string) (s, e int, outdated bool) {
	if at := relocate(file, anchor, start); at > 0 {
		return at, at + len(anchor) - 1, false
	}
	s, e = start, end
	if e > len(file) {
		e = len(file)
	}
	if s > e {
		s = e
	}
	if s < 1 {
		s, e = 1, 1
	}
	return s, e, true
}
```

(`relocate` returns 0 when the anchor is at two places and not at `start`,
which is the "twice, neither at start" case; when it IS at start it returns
start even if it also occurs elsewhere — the "start wins" case.) Check the
ambiguous case against `relocate`'s loop: it returns 0 on the second find
only when `at(start)` was false — exactly the cases above.

- [ ] **Step 5: Implement `docfile.go`**

```go
package agentdocs

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/notemeta"
	"github.com/homeend/gigagit/internal/steer"
)

// Session documents on disk (spec 2026-10-18-gg-session-docs §3): an
// overview is <id>.md — a +++ TOML front matter block, then markdown whose
// links are anchors — and a note is <id>.toml. The file name is the id. The
// encoders write the canonical spelling the examples fixture proves.

const (
	KindOverview = "overview"
	KindNote     = "note"

	// MaxDocBytes caps one document file; a larger one is ignored unread
	// (the same figure as a steer command file, for the same reason).
	MaxDocBytes = steer.MaxCommandBytes
	// DocFormat is the format this code writes and knows.
	DocFormat = 1

	frontMatterFence = "+++"
	tmpSuffix        = ".tmp"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// ValidID reports whether id may name a document.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// MintID is a fresh id: the UTC second plus four random hex digits. rnd nil
// reads crypto/rand.
func MintID(now time.Time, rnd io.Reader) string {
	if rnd == nil {
		rnd = rand.Reader
	}
	var b [2]byte
	if _, err := io.ReadFull(rnd, b[:]); err != nil {
		// crypto/rand failing is a broken platform; a time-only suffix keeps
		// the id unique per second per writer, which is the documented floor.
		b[0], b[1] = byte(now.Nanosecond()>>16), byte(now.Nanosecond()>>8)
	}
	return now.UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b[:])
}

// DocName reads a directory entry's name: the id and kind of a document, or
// ok=false for anything a reader ignores.
func DocName(name string) (id, kind string, ok bool) {
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, tmpSuffix) {
		return "", "", false
	}
	switch {
	case strings.HasSuffix(name, ".md"):
		id, kind = strings.TrimSuffix(name, ".md"), KindOverview
	case strings.HasSuffix(name, ".toml"):
		id, kind = strings.TrimSuffix(name, ".toml"), KindNote
	default:
		return "", "", false
	}
	if !ValidID(id) {
		return "", "", false
	}
	return id, kind, true
}

// FileName is the entry name of a document.
func FileName(id, kind string) string {
	if kind == KindNote {
		return id + ".toml"
	}
	return id + ".md"
}

// OverviewFile is a parsed <id>.md.
type OverviewFile struct {
	Format  int
	Title   string
	Author  string
	Created time.Time
	Text    string
}

// overviewHead is the front matter's TOML shape.
type overviewHead struct {
	Format  int        `toml:"format,omitempty"`
	Title   string     `toml:"title"`
	Author  string     `toml:"author,omitempty"`
	Created *time.Time `toml:"created,omitempty"`
}

// NoteFile is a parsed <id>.toml.
type NoteFile struct {
	Format    int
	Path      string
	Start     int
	End       int
	Summary   string
	Rationale string
	Author    string
	Created   time.Time
	Lines     []string
	Meta      []notemeta.KV
	Display   notemeta.Display
}

// noteWire is the file's TOML shape; the key order is the spec's example.
type noteWire struct {
	Format    int              `toml:"format,omitempty"`
	Path      string           `toml:"path"`
	Start     int              `toml:"start"`
	End       int              `toml:"end"`
	Summary   string           `toml:"summary"`
	Rationale string           `toml:"rationale,omitempty"`
	Author    string           `toml:"author,omitempty"`
	Created   *time.Time       `toml:"created,omitempty"`
	Lines     []string         `toml:"lines"`
	Meta      []notemeta.KV    `toml:"meta,omitempty"`
	Display   notemeta.Display `toml:"display,omitempty"`
}

var errNoFrontMatter = errors.New("an overview file starts with a +++ front matter block")

// splitFrontMatter returns the TOML between the fences and the body after
// the closing fence's line break.
func splitFrontMatter(data []byte) (head, body []byte, err error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	first, rest, ok := strings.Cut(text, "\n")
	if !ok || first != frontMatterFence {
		return nil, nil, errNoFrontMatter
	}
	lines := strings.Split(rest, "\n")
	for i, l := range lines {
		if l == frontMatterFence {
			return []byte(strings.Join(lines[:i], "\n")), []byte(strings.Join(lines[i+1:], "\n")), nil
		}
	}
	return nil, nil, errNoFrontMatter
}

func tomlErr(err error) error {
	var de *toml.DecodeError
	if errors.As(err, &de) {
		return errors.New("toml: " + de.Error())
	}
	var se *toml.StrictMissingError
	if errors.As(err, &se) {
		return errors.New("toml: " + se.Error())
	}
	if strings.Contains(err.Error(), "time") || strings.Contains(err.Error(), "Time") {
		return errors.New("created must be an RFC 3339 date-time")
	}
	return errors.New("toml: " + err.Error())
}

// ParseOverviewFile reads and validates an <id>.md (spec §3.2).
func ParseOverviewFile(data []byte) (OverviewFile, error) {
	head, body, err := splitFrontMatter(data)
	if err != nil {
		return OverviewFile{}, err
	}
	var h overviewHead
	if err := toml.Unmarshal(head, &h); err != nil {
		return OverviewFile{}, tomlErr(err)
	}
	if err := overviewRefusal(h.Title, string(body), true); err != nil {
		return OverviewFile{}, err
	}
	o := OverviewFile{Format: h.Format, Title: strings.TrimSpace(h.Title), Author: h.Author, Text: string(body)}
	if o.Format == 0 {
		o.Format = DocFormat
	}
	if o.Author == "" {
		o.Author = "agent"
	}
	if h.Created != nil {
		o.Created = h.Created.UTC()
	}
	return o, nil
}

// EncodeOverviewFile is the canonical <id>.md text.
func EncodeOverviewFile(o OverviewFile) ([]byte, error) {
	if err := overviewRefusal(o.Title, o.Text, true); err != nil {
		return nil, err
	}
	h := overviewHead{Format: DocFormat, Title: strings.TrimSpace(o.Title), Author: o.Author}
	if h.Author == "" {
		h.Author = "agent"
	}
	if !o.Created.IsZero() {
		c := o.Created.UTC()
		h.Created = &c
	}
	head, err := toml.Marshal(h)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(frontMatterFence + "\n")
	b.Write(head)
	if !bytes.HasSuffix(head, []byte("\n")) {
		b.WriteByte('\n')
	}
	b.WriteString(frontMatterFence + "\n")
	b.WriteString(o.Text)
	if !strings.HasSuffix(o.Text, "\n") {
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}

// validPath is a slash-form path inside the worktree: relative, no empty,
// "." or ".." segment, no backslash, no drive colon.
func validPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\:") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// noteRefusal validates a note's fields with the store's prose.
func noteRefusal(n NoteFile) error {
	switch {
	case n.Path == "":
		return errors.New("a note needs a path")
	case !validPath(n.Path):
		return errors.New("path must be a relative slash path inside the worktree")
	case n.Start < 1:
		return errors.New("a line number is 1-based")
	case n.End < n.Start:
		return errors.New("the range ends before it starts")
	case strings.TrimSpace(n.Summary) == "":
		return errors.New("a note needs a summary")
	case utf8.RuneCountInString(n.Summary) > MaxNoteSummary:
		return fmt.Errorf("the summary is longer than %d characters", MaxNoteSummary)
	case utf8.RuneCountInString(n.Rationale) > MaxNoteRationale:
		return fmt.Errorf("the rationale is longer than %d characters", MaxNoteRationale)
	case len(n.Lines) != n.End-n.Start+1:
		return fmt.Errorf("lines must hold end-start+1 entries (got %d, want %d)", len(n.Lines), n.End-n.Start+1)
	}
	return nil
}

// stripBreaks drops a line terminator a writer left on an anchor line.
func stripBreaks(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(l, "\r\n")
	}
	return out
}

// ParseNoteFile reads and validates an <id>.toml (spec §3.3).
func ParseNoteFile(data []byte) (NoteFile, error) {
	var w noteWire
	if err := toml.Unmarshal(data, &w); err != nil {
		return NoteFile{}, tomlErr(err)
	}
	n := NoteFile{Format: w.Format, Path: w.Path, Start: w.Start, End: w.End, Summary: strings.TrimSpace(w.Summary),
		Rationale: w.Rationale, Author: w.Author, Lines: stripBreaks(w.Lines), Display: w.Display}
	if err := noteRefusal(n); err != nil {
		return NoteFile{}, err
	}
	if n.Format == 0 {
		n.Format = DocFormat
	}
	if n.Author == "" {
		n.Author = "agent"
	}
	if w.Created != nil {
		n.Created = w.Created.UTC()
	}
	n.Meta, _ = notemeta.CheckMeta(w.Meta)
	return n, nil
}

// EncodeNoteFile is the canonical <id>.toml text.
func EncodeNoteFile(n NoteFile) ([]byte, error) {
	n.Lines = stripBreaks(n.Lines)
	n.Summary = strings.TrimSpace(n.Summary)
	if err := noteRefusal(n); err != nil {
		return nil, err
	}
	w := noteWire{Format: DocFormat, Path: n.Path, Start: n.Start, End: n.End, Summary: n.Summary, Rationale: n.Rationale,
		Author: n.Author, Lines: n.Lines, Meta: n.Meta, Display: n.Display}
	if w.Author == "" {
		w.Author = "agent"
	}
	if !n.Created.IsZero() {
		c := n.Created.UTC()
		w.Created = &c
	}
	return toml.Marshal(w)
}
```

Check `notemeta.CheckMeta`'s signature (`grep -n "func CheckMeta" internal/notemeta/*.go`) and adapt the call; check go-toml's error type names (`go doc github.com/pelletier/go-toml/v2 DecodeError`) and that `[]notemeta.KV` marshals as `[[meta]]` with `key`/`value` (KV already carries `toml:"key"`/`toml:"value"` tags). If go-toml writes `title = 'How …'` with single quotes (its default for strings without `'`), the round-trip test's `title = 'How scroll coalescing works'` expectation holds; if it writes double quotes, change the test's expected strings to match the encoder's real output — the spec fixes the keys, not the quote style.

- [ ] **Step 6: Run the tests until green**

Run: `cd /work/gigagit/.claude/worktrees/gg-session-docs && go test ./internal/agentdocs/ 2>&1 | tail -15`
Expected: `ok  	github.com/homeend/gigagit/internal/agentdocs` (every pre-existing test still green).

- [ ] **Step 7: Commit**

```bash
cd /work/gigagit/.claude/worktrees/gg-session-docs && gg add internal/agentdocs/docfile.go internal/agentdocs/cold.go internal/agentdocs/docfile_test.go internal/agentdocs/cold_test.go docs/specs/gg-session-docs/cold-anchor-cases.json docs/specs/gg-session-docs/id-vectors.json && git commit -q -m "agentdocs: session document shapes (overview front matter, note TOML), ids, cold anchoring + published cases" && git log --oneline -1
```

---

### Task 3: Directory I/O — list, write (temp + rename, O_EXCL for new), remove

**Files:**
- Create: `internal/agentdocs/docdir.go`
- Test: `internal/agentdocs/docdir_test.go`

**Interfaces:**
- Produces:

```go
// Doc is one directory entry a reader accepted.
type Doc struct {
	ID      string
	Kind    string
	ModTime time.Time
	Size    int64
	Over    *OverviewFile // Kind == KindOverview
	Note    *NoteFile     // Kind == KindNote
}

// Problem is an entry a reader skipped and why (never shown; for debugging).
type Problem struct{ Name, Reason string }

// ListDocs reads dir: documents in name order, plus the skipped entries. A
// missing dir is an empty list, not an error.
func ListDocs(dir string) ([]Doc, []Problem, error)

// ReadDoc reads one document by id and kind.
func ReadDoc(dir, id, kind string) (Doc, error)   // os.ErrNotExist when absent

// WriteDoc writes data as <id>.<ext> through a temp file in dir; replace
// says whether an existing file may be replaced (false = O_EXCL on the
// final name: os.ErrExist when it is there).
func WriteDoc(dir, id, kind string, data []byte, replace bool) error

// RemoveDoc unlinks one document; false when it was not there.
func RemoveDoc(dir, id, kind string) (bool, error)
```

- `Problem.Reason` values: `not a document name`, `a directory`,
  `over 512 KiB`, `unreadable: <os error>`, and the parse error's text.
- O_EXCL rename: Go's `os.Rename` replaces. For `replace == false` use
  `os.Link(tmp, final)` then `os.Remove(tmp)`; `os.Link` fails with
  `EEXIST` when final exists (Linux, macOS, Windows NTFS). On a filesystem
  without hard links (`ENOTSUP`/`EPERM`), fall back to a `Lstat` check
  then `Rename` (documented small window).

- [ ] **Step 1: Write the failing tests**

```go
// internal/agentdocs/docdir_test.go
package agentdocs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeRaw(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

const okOverview = "+++\ntitle = \"T\"\n+++\nbody\n"
const okNote = "path = \"a.go\"\nstart = 1\nend = 1\nsummary = \"s\"\nlines = [\"x\"]\n"

func TestListDocsAcceptsDocumentsAndReportsTheRest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeRaw(t, dir, "b-ov.md", okOverview)
	writeRaw(t, dir, "a-note.toml", okNote)
	writeRaw(t, dir, ".hidden.md", okOverview)
	writeRaw(t, dir, "x.md.tmp", okOverview)
	writeRaw(t, dir, "readme.txt", "no")
	writeRaw(t, dir, "bad.md", "title = 1\n")
	writeRaw(t, dir, "bad-note.toml", "path = \"a.go\"\n")
	writeRaw(t, dir, "big.md", "+++\ntitle = \"T\"\n+++\n"+strings.Repeat("x", MaxDocBytes+1))
	if err := os.Mkdir(filepath.Join(dir, "sub.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	docs, probs, err := ListDocs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[0].ID != "a-note" || docs[0].Kind != KindNote || docs[0].Note == nil || docs[1].ID != "b-ov" || docs[1].Over == nil || docs[1].Over.Title != "T" {
		t.Fatalf("docs = %+v", docs)
	}
	got := map[string]string{}
	for _, p := range probs {
		got[p.Name] = p.Reason
	}
	want := map[string]string{
		".hidden.md": "not a document name", "x.md.tmp": "not a document name", "readme.txt": "not a document name",
		"bad.md": "an overview file starts with a +++ front matter block", "bad-note.toml": "a line number is 1-based",
		"big.md": "over 512 KiB", "sub.md": "a directory",
	}
	for name, reason := range want {
		if !strings.HasPrefix(got[name], reason) {
			t.Errorf("%s: reason %q, want prefix %q", name, got[name], reason)
		}
	}
	if len(got) != len(want) {
		t.Errorf("problems = %v", got)
	}
	// Nothing was touched.
	if entries, _ := os.ReadDir(dir); len(entries) != 9 {
		t.Fatalf("a reader changed the directory: %d entries", len(entries))
	}
}

func TestListDocsOfAMissingDirIsEmpty(t *testing.T) {
	t.Parallel()
	docs, probs, err := ListDocs(filepath.Join(t.TempDir(), "none"))
	if err != nil || len(docs) != 0 || len(probs) != 0 {
		t.Fatalf("%v %v %v", docs, probs, err)
	}
}

func TestWriteDocIsAtomicAndCreatesTheDirectory(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "inbox", "docs")
	if err := WriteDoc(dir, "one", KindOverview, []byte(okOverview), false); err != nil {
		t.Fatal(err)
	}
	d, err := ReadDoc(dir, "one", KindOverview)
	if err != nil || d.Over.Title != "T" || d.Size != int64(len(okOverview)) {
		t.Fatalf("ReadDoc = %+v %v", d, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
	// A NEW write refuses an existing name; a replace does not.
	if err := WriteDoc(dir, "one", KindOverview, []byte(okOverview), false); !errors.Is(err, os.ErrExist) {
		t.Fatalf("second new write: %v", err)
	}
	if err := WriteDoc(dir, "one", KindOverview, []byte("+++\ntitle = \"U\"\n+++\nbody\n"), true); err != nil {
		t.Fatal(err)
	}
	if d, _ := ReadDoc(dir, "one", KindOverview); d.Over.Title != "U" {
		t.Fatalf("replace did not land: %+v", d)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("temp file left behind after replace: %v", entries)
	}
}

func TestWriteDocRefusesABadIDAndBadData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteDoc(dir, "a.b", KindOverview, []byte(okOverview), false); err == nil || err.Error() != "invalid document id \"a.b\"" {
		t.Fatalf("bad id: %v", err)
	}
	if err := WriteDoc(dir, "ok", KindNote, []byte("path = 1\n"), false); err == nil || !strings.HasPrefix(err.Error(), "toml: ") {
		t.Fatalf("bad data must be refused before any file lands: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a refused write left %v", entries)
	}
}

func TestRemoveDoc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeRaw(t, dir, "one.toml", okNote)
	if ok, err := RemoveDoc(dir, "one", KindNote); !ok || err != nil {
		t.Fatalf("remove: %v %v", ok, err)
	}
	if ok, err := RemoveDoc(dir, "one", KindNote); ok || err != nil {
		t.Fatalf("second remove: %v %v", ok, err)
	}
	if _, err := ReadDoc(dir, "one", KindNote); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ReadDoc after remove: %v", err)
	}
}

func TestDocModTimeIsTheFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeRaw(t, dir, "one.md", okOverview)
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, "one.md"), old, old); err != nil {
		t.Fatal(err)
	}
	docs, _, _ := ListDocs(dir)
	if len(docs) != 1 || !docs[0].ModTime.Equal(old) {
		t.Fatalf("docs = %+v", docs)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /work/gigagit/.claude/worktrees/gg-session-docs && go test ./internal/agentdocs/ -run 'TestListDocs|TestWriteDoc|TestRemoveDoc|TestDocModTime' 2>&1 | head`
Expected: FAIL to compile — `undefined: ListDocs`, `WriteDoc`, `ReadDoc`, `RemoveDoc`, `Doc`.

- [ ] **Step 3: Implement `docdir.go`**

```go
package agentdocs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// The docs directory (spec 2026-10-18-gg-session-docs §2, §5): a reader
// lists it and never changes it; a writer lands a file by temp + rename.

// Doc is one directory entry a reader accepted.
type Doc struct {
	ID      string
	Kind    string
	ModTime time.Time
	Size    int64
	Over    *OverviewFile
	Note    *NoteFile
}

// Problem is an entry a reader skipped, and why.
type Problem struct{ Name, Reason string }

// ListDocs reads dir: the documents in name order plus the skipped entries.
// A missing directory is an empty list.
func ListDocs(dir string) ([]Doc, []Problem, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var docs []Doc
	var probs []Problem
	for _, e := range entries {
		id, kind, ok := DocName(e.Name())
		if !ok {
			probs = append(probs, Problem{e.Name(), "not a document name"})
			continue
		}
		d, err := readEntry(dir, id, kind)
		if err != nil {
			probs = append(probs, Problem{e.Name(), err.Error()})
			continue
		}
		docs = append(docs, d)
	}
	sort.Slice(docs, func(i, j int) bool { return FileName(docs[i].ID, docs[i].Kind) < FileName(docs[j].ID, docs[j].Kind) })
	return docs, probs, nil
}

// ReadDoc reads one document; os.ErrNotExist when absent.
func ReadDoc(dir, id, kind string) (Doc, error) {
	if !ValidID(id) {
		return Doc{}, fs.ErrNotExist
	}
	return readEntry(dir, id, kind)
}

func readEntry(dir, id, kind string) (Doc, error) {
	name := FileName(id, kind)
	st, err := os.Lstat(filepath.Join(dir, name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Doc{}, fs.ErrNotExist
		}
		return Doc{}, fmt.Errorf("unreadable: %v", err)
	}
	if st.IsDir() {
		return Doc{}, errors.New("a directory")
	}
	if st.Size() > MaxDocBytes {
		return Doc{}, errors.New("over 512 KiB")
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Doc{}, fs.ErrNotExist
		}
		return Doc{}, fmt.Errorf("unreadable: %v", err)
	}
	d := Doc{ID: id, Kind: kind, ModTime: st.ModTime(), Size: st.Size()}
	switch kind {
	case KindOverview:
		o, err := ParseOverviewFile(data)
		if err != nil {
			return Doc{}, err
		}
		d.Over = &o
	default:
		n, err := ParseNoteFile(data)
		if err != nil {
			return Doc{}, err
		}
		d.Note = &n
	}
	return d, nil
}

// WriteDoc lands data as the document (id, kind): parsed first (a refused
// document never reaches the directory), then a temp file in dir, then a
// rename over the final name — or, with replace false, a link that fails
// when the name is taken.
func WriteDoc(dir, id, kind string, data []byte, replace bool) error {
	if !ValidID(id) {
		return fmt.Errorf("invalid document id %q", id)
	}
	if kind == KindOverview {
		if _, err := ParseOverviewFile(data); err != nil {
			return err
		}
	} else if _, err := ParseNoteFile(data); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var rnd [4]byte
	_, _ = rand.Read(rnd[:])
	tmp := filepath.Join(dir, "."+id+"."+hex.EncodeToString(rnd[:])+tmpSuffix)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	final := filepath.Join(dir, FileName(id, kind))
	if replace {
		if err := os.Rename(tmp, final); err != nil {
			os.Remove(tmp)
			return err
		}
		return nil
	}
	err := os.Link(tmp, final)
	switch {
	case err == nil:
		os.Remove(tmp)
		return nil
	case errors.Is(err, fs.ErrExist):
		os.Remove(tmp)
		return fs.ErrExist
	}
	// No hard links here: the best this filesystem offers.
	if _, serr := os.Lstat(final); serr == nil {
		os.Remove(tmp)
		return fs.ErrExist
	}
	if rerr := os.Rename(tmp, final); rerr != nil {
		os.Remove(tmp)
		return rerr
	}
	return nil
}

// RemoveDoc unlinks one document; false when it was not there.
func RemoveDoc(dir, id, kind string) (bool, error) {
	if !ValidID(id) {
		return false, nil
	}
	err := os.Remove(filepath.Join(dir, FileName(id, kind)))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
```

- [ ] **Step 4: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/gg-session-docs && go test ./internal/agentdocs/ 2>&1 | tail -5`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/gg-session-docs && gg add internal/agentdocs/docdir.go internal/agentdocs/docdir_test.go && git commit -q -m "agentdocs: the docs directory — list with skip reasons, temp+rename write (O_EXCL for a new id), remove" && git log --oneline -1
```

---

### Task 4: Attach a root to its directory — load, watch, reconcile, write-through

**Files:**
- Create: `internal/agentdocs/attach.go`
- Modify: `internal/agentdocs/store.go` (new fields), `internal/agentdocs/notes.go` (ids, cold flag, write-through, sort by created), `internal/agentdocs/overviews.go` (ids, cap, write-through, FileTour names)
- Test: `internal/agentdocs/attach_test.go`

**Interfaces:**
- Produces:

```go
// Attach binds root (the store's worktree key) to its docs directory: loads
// it, watches it, and from now on every mutation on root writes a file and
// reloads. worktreeDir is where file anchors are stat'ed (CheckAnchors).
// Attaching an attached root with the same dir is a no-op; with another dir
// it detaches first.
func (s *Store) Attach(root, worktreeDir, docsDir string) error

// Detach stops watching root and drops its documents from memory.
func (s *Store) Detach(root string)

// Attached reports root's docs directory ("" = memory only).
func (s *Store) Attached(root string) string

// Reload re-reads root's directory now (the watch and the poll call it).
func (s *Store) Reload(root string)

// Problems is what the last reload of root skipped.
func (s *Store) Problems(root string) []Problem

// Close detaches every root.
func (s *Store) Close()

// pollEvery is the watch's fallback tick; a test shortens it.
var pollEvery = 2 * time.Second
```

- Behaviour on an ATTACHED root:
  - `AddNoteMeta(root, path, lines, start, end, t)`: validates as today,
    then `WriteDoc(dir, MintID(...), KindNote, EncodeNoteFile(NoteFile{Path, Start, End, Summary, Rationale, Author, Created: now, Lines: lines[start-1:end], Meta, Display}), false)`
    (retry a new id on `ErrExist`, 3 times), then `Reload(root)` and return
    the note with the minted id. The `MaxNotesPerFile` refusal counts the
    path's loaded notes. `lines` is still required (the writer's file
    content): `path has no lines to note` when empty.
  - `AddOverview(root, dir, title, text)`: validates, writes
    `MintID` `.md`, reloads, returns it. No `MaxOverviewsPerRoot` refusal
    on an attached root.
  - `SetOverview(id, title, text)`: finds the id's root; attached → rewrite
    with `replace=true` keeping `Author`/`Created` from the loaded file (a
    blank title keeps the old title), reload.
  - `RemoveNote(id)`, `RemoveOverview(id)`, `ClearPath(root, path)`:
    `RemoveDoc`, reload.
  - `FileTour(key, root, dir, title, text)` on an attached root: the id is
    `tourID(key)` = `"tour-" + sanitize(key)` where `sanitize` replaces
    every byte outside `[A-Za-z0-9_-]` with `-` (so `brief:gg-1/7` →
    `tour-brief-gg-1-7`), truncated to 64; write with `replace=true`;
    `added` = the file did not exist before. `TourID(key)` returns the id
    when the file is loaded.
  - Ids of loaded documents are their file names; `Seq` for ordering is
    `Created.UnixNano()` (or `ModTime` when `Created` is zero); ties by id.
    `Overviews(root)` stays "oldest first" by that Seq; `fileNotes.sort`
    stays by start then Seq.
  - Cold anchoring: a loaded note carries `anchor = Lines` and `cold = true`
    with `Start/End` as written. The root's `fileNotes` for that path may
    already hold `lines` (a frontend aligned before): then `ColdPlace` runs
    at load. Otherwise the first `Align`/`AlignedNotes` call with content
    runs `ColdPlace` for every cold note instead of `reanchor`, clears
    `cold`, and the usual path follows.
  - Reconcile (`Reload`): list → by id: new → insert; same id, changed
    `(Size, ModTime)` → replace in place (an overview keeps `Missing`
    flags for unchanged anchor dests, as `SetOverview` does; a note keeps
    its current placement when `Path/Start/End/Lines` are unchanged, else
    goes cold again); gone → delete (and the `fileNotes` entry when its
    last note goes). Signal once per reload that changed anything.
  - Watch: `steer.WatchFilter(docsDir, func(name) bool { _, _, ok := DocName(name); return ok || strings.HasPrefix(name, ".") == false && strings.HasSuffix(name, tmpSuffix) == false })` — simpler: wake on any name `DocName` accepts; a rename from `.x.tmp` to `x.md` fires with the final name. Plus a ticker every `pollEvery` → `Reload`. One goroutine per attached root, stopped by `Detach`/`Close`. A `Watch` error (no inotify) is not fatal: the poll alone runs.
- Unattached roots: exactly today's code paths (ids `t<n>`, `f<n>`, caps).

- [ ] **Step 1: Write the failing tests**

```go
// internal/agentdocs/attach_test.go
package agentdocs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func within(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func attached(t *testing.T) (*Store, string, string) {
	t.Helper()
	wt := t.TempDir()
	dir := filepath.Join(t.TempDir(), "docs")
	s := New()
	if err := s.Attach(root, wt, dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, wt, dir
}

func TestAttachLoadsWhatIsThereAndIgnoresTheRest(t *testing.T) {
	t.Parallel()
	wt := t.TempDir()
	dir := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRaw(t, dir, "ov-1.md", "+++\ntitle = \"One\"\ncreated = 2026-10-18T10:00:00Z\n+++\n[a](a.go:1)\n")
	writeRaw(t, dir, "ov-0.md", "+++\ntitle = \"Zero\"\ncreated = 2026-10-18T09:00:00Z\n+++\nbody\n")
	writeRaw(t, dir, "n-1.toml", "path = \"a.go\"\nstart = 2\nend = 2\nsummary = \"s\"\nlines = [\"b\"]\n")
	writeRaw(t, dir, "junk.txt", "x")
	s := New()
	if err := s.Attach(root, wt, dir); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Attached(root) != dir || s.Attached("/other") != "" {
		t.Fatal("Attached")
	}
	ovs := s.Overviews(root)
	if len(ovs) != 2 || ovs[0].ID != "ov-0" || ovs[1].ID != "ov-1" || ovs[1].Title != "One" || len(ovs[1].Anchors) != 1 {
		t.Fatalf("overviews = %+v", ovs)
	}
	n, ok := s.FindNote("n-1")
	if !ok || n.Path != "a.go" || n.Start != 2 || n.End != 2 || n.Root != root {
		t.Fatalf("note = %+v %v", n, ok)
	}
	if p := s.Problems(root); len(p) != 1 || p[0].Name != "junk.txt" {
		t.Fatalf("problems = %+v", p)
	}
}

func TestColdNoteIsPlacedOnFirstAlign(t *testing.T) {
	t.Parallel()
	s, _, dir := attached(t)
	writeRaw(t, dir, "n-1.toml", "path = \"a.go\"\nstart = 2\nend = 3\nsummary = \"s\"\nlines = [\"b\", \"c\"]\n")
	s.Reload(root)
	// The file gained a line above: the note follows its text.
	notes := s.AlignedNotes(root, "a.go", []string{"x", "a", "b", "c"})
	if len(notes) != 1 || notes[0].Start != 3 || notes[0].End != 4 || notes[0].Outdated {
		t.Fatalf("placed = %+v", notes)
	}
	// From here on the usual re-anchoring runs.
	notes = s.AlignedNotes(root, "a.go", []string{"x", "a", "b", "C"})
	if len(notes) != 1 || !notes[0].Outdated || notes[0].Start != 3 || notes[0].End != 4 {
		t.Fatalf("after edit = %+v", notes)
	}
	// A note loaded for a path whose content the store already holds is
	// placed at load.
	writeRaw(t, dir, "n-2.toml", "path = \"a.go\"\nstart = 1\nend = 1\nsummary = \"t\"\nlines = [\"a\"]\n")
	s.Reload(root)
	n, _ := s.FindNote("n-2")
	if n.Start != 2 || n.Outdated {
		t.Fatalf("n-2 = %+v", n)
	}
}

func TestAddNoteOnAnAttachedRootWritesAFile(t *testing.T) {
	t.Parallel()
	s, _, dir := attached(t)
	n, err := s.AddNoteMeta(root, "a.go", []string{"a", "b", "c"}, 2, 3, NoteText{Summary: "look", Rationale: "why", Author: "ada"})
	if err != nil {
		t.Fatal(err)
	}
	if !ValidID(n.ID) || strings.HasPrefix(n.ID, "t") && len(n.ID) < 10 {
		t.Fatalf("id = %q", n.ID)
	}
	d, err := ReadDoc(dir, n.ID, KindNote)
	if err != nil || d.Note.Summary != "look" || d.Note.Author != "ada" || strings.Join(d.Note.Lines, "|") != "b|c" || d.Note.Created.IsZero() {
		t.Fatalf("file = %+v %v", d, err)
	}
	if got, _ := s.FindNote(n.ID); got.Start != 2 || got.End != 3 || got.Outdated {
		t.Fatalf("loaded back = %+v", got)
	}
	if !s.RemoveNote(n.ID) {
		t.Fatal("remove")
	}
	if _, err := ReadDoc(dir, n.ID, KindNote); err == nil {
		t.Fatal("remove must unlink the file")
	}
}

func TestAddOverviewOnAnAttachedRootHasNoCapAndSetRewrites(t *testing.T) {
	t.Parallel()
	s, wt, dir := attached(t)
	var last Overview
	for i := 0; i < MaxOverviewsPerRoot+1; i++ {
		o, err := s.AddOverview(root, wt, "T", "[a](a.go:1)\n")
		if err != nil {
			t.Fatalf("overview %d: %v", i, err)
		}
		last = o
	}
	if len(s.Overviews(root)) != MaxOverviewsPerRoot+1 {
		t.Fatal("an attached root has no overview cap")
	}
	before, _ := ReadDoc(dir, last.ID, KindOverview)
	if _, err := s.SetOverview(last.ID, "", "new text\n"); err != nil {
		t.Fatal(err)
	}
	after, _ := ReadDoc(dir, last.ID, KindOverview)
	if after.Over.Text != "new text\n" || after.Over.Title != "T" || !after.Over.Created.Equal(before.Over.Created) {
		t.Fatalf("set = %+v", after.Over)
	}
	if !s.RemoveOverview(last.ID) {
		t.Fatal("remove")
	}
	if _, err := ReadDoc(dir, last.ID, KindOverview); err == nil {
		t.Fatal("remove must unlink")
	}
}

func TestExternalWritesAppearAndRemovalsDisappear(t *testing.T) {
	t.Parallel()
	s, _, dir := attached(t)
	ch, cancel := s.Subscribe()
	defer cancel()
	// A writer that is not this process: temp + rename.
	writeRaw(t, dir, ".ov-9.ab.tmp", "+++\ntitle = \"Nine\"\n+++\nbody\n")
	if err := os.Rename(filepath.Join(dir, ".ov-9.ab.tmp"), filepath.Join(dir, "ov-9.md")); err != nil {
		t.Fatal(err)
	}
	within(t, 5*time.Second, func() bool { _, ok := s.Overview("ov-9"); return ok })
	select {
	case <-ch:
	default:
		t.Fatal("a reload that changed something must signal")
	}
	// Replaced in place: same id, new text, Missing flags of unchanged dests kept.
	writeRaw(t, dir, ".ov-9.cd.tmp", "+++\ntitle = \"Nine\"\n+++\nnew body\n")
	if err := os.Rename(filepath.Join(dir, ".ov-9.cd.tmp"), filepath.Join(dir, "ov-9.md")); err != nil {
		t.Fatal(err)
	}
	within(t, 5*time.Second, func() bool { o, _ := s.Overview("ov-9"); return o.Text == "new body\n" })
	if err := os.Remove(filepath.Join(dir, "ov-9.md")); err != nil {
		t.Fatal(err)
	}
	within(t, 5*time.Second, func() bool { _, ok := s.Overview("ov-9"); return !ok })
}

func TestPollFallbackFindsChangesWithoutAWatch(t *testing.T) {
	t.Parallel()
	old := pollEvery
	pollEvery = 50 * time.Millisecond
	t.Cleanup(func() { pollEvery = old })
	wt := t.TempDir()
	dir := filepath.Join(t.TempDir(), "docs")
	s := New()
	s.noWatch = true // test hook: the poll alone
	if err := s.Attach(root, wt, dir); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	writeRaw(t, dir, "ov-1.md", okOverview)
	within(t, 3*time.Second, func() bool { _, ok := s.Overview("ov-1"); return ok })
	// The whole directory replaced underneath: no crash, no ghosts.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	within(t, 3*time.Second, func() bool { _, ok := s.Overview("ov-1"); return !ok })
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRaw(t, dir, "ov-2.md", okOverview)
	within(t, 3*time.Second, func() bool { _, ok := s.Overview("ov-2"); return ok })
}

func TestFileTourUsesAFixedNameAndReplaces(t *testing.T) {
	t.Parallel()
	s, wt, dir := attached(t)
	o, added, err := s.FileTour("report:gg-12345/7", root, wt, "Report", "first\n")
	if err != nil || !added || o.ID != "tour-report-gg-12345-7" {
		t.Fatalf("%+v %v %v", o, added, err)
	}
	o2, added, err := s.FileTour("report:gg-12345/7", root, wt, "Report", "second\n")
	if err != nil || added || o2.ID != o.ID {
		t.Fatalf("%+v %v %v", o2, added, err)
	}
	if d, _ := ReadDoc(dir, o.ID, KindOverview); d.Over.Text != "second\n" {
		t.Fatalf("file = %+v", d.Over)
	}
	if id, ok := s.TourID("report:gg-12345/7"); !ok || id != o.ID {
		t.Fatal("TourID")
	}
	s.RemoveOverview(o.ID)
	if _, ok := s.TourID("report:gg-12345/7"); ok {
		t.Fatal("a deleted tour has no id")
	}
}

func TestDetachDropsTheRootAndUnattachedRootsKeepMemoryIDs(t *testing.T) {
	t.Parallel()
	s, wt, dir := attached(t)
	writeRaw(t, dir, "ov-1.md", okOverview)
	s.Reload(root)
	s.Detach(root)
	if len(s.Overviews(root)) != 0 || s.Attached(root) != "" {
		t.Fatal("detach must drop the root's documents")
	}
	o, err := s.AddOverview("/memory", wt, "T", "body\n")
	if err != nil || o.ID != "f1" {
		t.Fatalf("an unattached root numbers as before: %+v %v", o, err)
	}
	n, err := s.AddNote("/memory", "a.go", []string{"a"}, 1, 1, "s", "", "")
	if err != nil || n.ID != "t1" {
		t.Fatalf("%+v %v", n, err)
	}
}

func TestOverviewsOrderByCreatedThenID(t *testing.T) {
	t.Parallel()
	s, _, dir := attached(t)
	writeRaw(t, dir, "b.md", "+++\ntitle = \"B\"\ncreated = 2026-10-18T10:00:00Z\n+++\nx\n")
	writeRaw(t, dir, "a.md", "+++\ntitle = \"A\"\ncreated = 2026-10-18T10:00:00Z\n+++\nx\n")
	writeRaw(t, dir, "c.md", "+++\ntitle = \"C\"\ncreated = 2026-10-18T09:00:00+02:00\n+++\nx\n") // 07:00Z
	writeRaw(t, dir, "d.md", okOverview)                                                              // no created: mtime (now) → last
	s.Reload(root)
	var ids []string
	for _, o := range s.Overviews(root) {
		ids = append(ids, o.ID)
	}
	if strings.Join(ids, ",") != "c,a,b,d" {
		t.Fatalf("order = %v", ids)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd /work/gigagit/.claude/worktrees/gg-session-docs && go test ./internal/agentdocs/ -run 'TestAttach|TestCold|TestAddNoteOnAn|TestAddOverviewOnAn|TestExternal|TestPoll|TestFileTourUses|TestDetach|TestOverviewsOrder' 2>&1 | head`
Expected: FAIL to compile — `s.Attach undefined`, `s.noWatch undefined`, ….

- [ ] **Step 3: Implement**

`store.go` additions:

```go
type Store struct {
	// … existing fields …
	roots   map[string]*attachedRoot // attached roots by key
	noWatch bool                     // test hook: poll only
}

// attachedRoot is one root bound to its docs directory.
type attachedRoot struct {
	dir, worktree string
	w             *steer.Watcher // nil when the watch could not start
	stop          chan struct{}
	problems      []Problem
	seen          map[string]docStamp // id → what was loaded (for reconcile)
}

type docStamp struct {
	kind    string
	size    int64
	modTime time.Time
}
```

(`New()` initialises `roots: map[string]*attachedRoot{}`.)

`attach.go` (the shape; fill in with the behaviour list above):

```go
package agentdocs

// Attached roots (spec 2026-10-18-gg-session-docs §4.2, §6.1): the store
// is a cache of the directory. One goroutine per root wakes on the watch
// or the poll and reconciles by id; mutations write a file, then reload.

var pollEvery = 2 * time.Second

func (s *Store) Attach(root, worktreeDir, docsDir string) error {
	s.mu.Lock()
	if r := s.roots[root]; r != nil {
		if r.dir == docsDir {
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()
		s.Detach(root)
		s.mu.Lock()
	}
	r := &attachedRoot{dir: docsDir, worktree: worktreeDir, stop: make(chan struct{}), seen: map[string]docStamp{}}
	if !s.noWatch {
		if w, err := steer.WatchFilter(docsDir, func(name string) bool { _, _, ok := DocName(name); return ok }); err == nil {
			r.w = w
		}
	}
	s.roots[root] = r
	s.mu.Unlock()
	s.Reload(root)
	go s.follow(root, r)
	return nil
}

func (s *Store) follow(root string, r *attachedRoot) {
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	var wake <-chan struct{}
	if r.w != nil {
		wake = r.w.Events()
	}
	for {
		select {
		case <-r.stop:
			return
		case <-wake:
			s.Reload(root)
		case <-t.C:
			s.Reload(root)
		}
	}
}
```

`Reload(root)`: `ListDocs(r.dir)`; under `s.mu`: build the id set; for each
doc: new or stamp changed → `s.loadDoc(root, r, d)` (overview: `ParseOverview`
→ `ovEntry{Overview{ID: d.ID, Seq: seqOf(d), Root: root, Title, Text,
Anchors}, dir: r.worktree}` keeping `Missing` for unchanged dests when
replacing; note: a `*note` with `anchor: d.Note.Lines`, `cold: true`, placed
with `ColdPlace` when the path's `fileNotes.lines` exist, inserted into
`s.files[fileKey{root, path}]`, replacing a same-id note — keep its current
`Start/End/Outdated/cold` when `Path/Start/End/Lines` of the file are
unchanged); ids in `r.seen` but not listed → remove (overviews map; notes
from their `fileNotes`, deleting an emptied entry); `r.problems = probs`;
signal when anything changed. `seqOf(d)`: `d.Over.Created` / `d.Note.Created`
UnixNano, or `d.ModTime.UnixNano()` when zero.

In `notes.go`: add `cold bool` to `note`; in `fileNotes.align(cur)` run
cold placement first:

```go
func (f *fileNotes) align(cur []string) bool {
	p := Print(cur)
	placed := f.placeCold(cur)
	if p == f.print && !placed {
		return false
	}
	if len(f.lines) > 0 {
		reanchor(f.live(), f.lines, cur)
	}
	f.lines, f.print = cur, p
	f.sort()
	return true
}

// placeCold gives every note loaded from a file its first position in cur
// (spec §4.3) and says whether there was one.
func (f *fileNotes) placeCold(cur []string) bool {
	placed := false
	for _, n := range f.notes {
		if n.cold {
			n.Start, n.End, n.Outdated = ColdPlace(cur, n.Start, n.End, n.anchor)
			n.cold, placed = false, true
		}
	}
	return placed
}
```

where `f.live()` is the notes that were not just cold-placed (a note placed
cold against `cur` must not then be re-anchored from `f.lines` to `cur`;
simplest: `placeCold` runs AFTER `reanchor` of the non-cold notes — order
the two calls so: `reanchor(nonCold, f.lines, cur)` then `placeCold(cur)`).
A `fileNotes` created by `Reload` for a path with no content yet has
`lines == nil`, `print == Fingerprint{}`; `Notes(root, path)` returns the
cold notes with their written numbers and `Outdated == false` until the
first align — acceptable, the spec says a reader places on first content.

`AddNoteMeta`, `AddOverview`, `SetOverview`, `RemoveNote`, `RemoveOverview`,
`ClearPath`, `FileTour`: at the top, `if r := s.rootOf(root)` (or
`s.rootOfID(id)` for id-keyed calls, found by scanning overviews/notes for
the id's `Root`) `!= nil { return s.<x>File(...) }` where the `File`
variants do the write-through described above, then `s.Reload(root)`, then
return the loaded copy. Keep the existing memory paths untouched below.

`Detach(root)`: close `stop` (once), `r.w.Close()`, delete `s.roots[root]`,
drop `s.overviews` entries with that root, `s.files` keys with that root,
`s.tours` whose id is gone; signal. `Close()`: `Detach` every root.

`TourID(key)` on an attached root: `tourID(key)` if `s.overviews[id]`
exists (no `tours` map needed; keep the map for memory roots).

- [ ] **Step 4: Run the whole package until green, then the race detector on it**

Run: `cd /work/gigagit/.claude/worktrees/gg-session-docs && go test ./internal/agentdocs/ 2>&1 | tail -8 && go test -race ./internal/agentdocs/ 2>&1 | tail -3`
Expected: `ok` twice; no `DATA RACE`.

- [ ] **Step 5: Run every package that imports agentdocs (nothing attached yet, so nothing may change)**

Run: `cd /work/gigagit/.claude/worktrees/gg-session-docs && go build ./... && go test ./internal/tui/ ./internal/web/ ./internal/cli/ 2>&1 | tail -5`
Expected: `ok` for all three.

- [ ] **Step 6: Commit**

```bash
cd /work/gigagit/.claude/worktrees/gg-session-docs && gg add internal/agentdocs/attach.go internal/agentdocs/store.go internal/agentdocs/notes.go internal/agentdocs/overviews.go internal/agentdocs/attach_test.go && git commit -q -m "agentdocs: Attach a root to its docs directory — load, watch + poll, reconcile by id, cold anchoring, write-through mutations, fixed tour names" && git log --oneline -1
```

---

### Task 5: Conformance material — schemas, the examples directory, README

**Files:**
- Create: `docs/specs/gg-session-docs/README.md`
- Create: `docs/specs/gg-session-docs/overview-frontmatter.schema.json`
- Create: `docs/specs/gg-session-docs/note.schema.json`
- Create: `docs/specs/gg-session-docs/examples/` (generated: `tree/`, two `.md`, three `.toml`, `ids.json`, `README.md`)
- Test: `internal/agentdocs/schema_test.go`, `internal/agentdocs/examples_golden_test.go`

**Interfaces:**
- Consumes: Task 2's `EncodeOverviewFile`/`EncodeNoteFile`/`ParseNoteFile`/`ParseOverviewFile`, Task 3's `WriteDoc`/`ListDocs`, Task 4's `Attach`.
- Produces: the two schemas (lenient: `additionalProperties: true`), the fixture, and a `-update-examples` flag on the golden test.

- [ ] **Step 1: Write the schemas**

`overview-frontmatter.schema.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://gigagit.dev/specs/gg-session-docs/overview-frontmatter.schema.json",
  "title": "gg session document — overview front matter (parsed TOML)",
  "description": "The TOML between the +++ fences of <id>.md, as JSON: tables are objects, date-times are RFC 3339 strings. Lenient: unknown keys allowed. The body after the fence is markdown (notes-store spec part 1's subset) whose links are anchors; it is not part of this schema.",
  "type": "object",
  "additionalProperties": true,
  "required": ["title"],
  "properties": {
    "format": {"type": "integer", "minimum": 1, "description": "default 1"},
    "title": {"type": "string", "minLength": 1, "maxLength": 200, "pattern": "^[^\\r\\n]*$"},
    "author": {"type": "string", "description": "default \"agent\""},
    "created": {"type": "string", "format": "date-time"}
  }
}
```

`note.schema.json`:

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://gigagit.dev/specs/gg-session-docs/note.schema.json",
  "title": "gg session document — note (parsed TOML)",
  "description": "<id>.toml as JSON: tables are objects, arrays of tables are arrays, date-times are RFC 3339 strings. Lenient: unknown keys allowed. Not expressible here: lines.length == end - start + 1, start <= end, the path's segment rules (see the spec §3.3).",
  "type": "object",
  "additionalProperties": true,
  "required": ["path", "start", "end", "summary", "lines"],
  "properties": {
    "format": {"type": "integer", "minimum": 1},
    "path": {"type": "string", "minLength": 1, "pattern": "^[^/\\\\:][^\\\\:]*$"},
    "start": {"type": "integer", "minimum": 1},
    "end": {"type": "integer", "minimum": 1},
    "summary": {"type": "string", "minLength": 1, "maxLength": 500},
    "rationale": {"type": "string", "maxLength": 4000},
    "author": {"type": "string"},
    "created": {"type": "string", "format": "date-time"},
    "lines": {"type": "array", "minItems": 1, "items": {"type": "string"}},
    "meta": {"type": "array", "items": {"type": "object", "required": ["key", "value"], "properties": {"key": {"type": "string", "maxLength": 64}, "value": {"type": "string", "maxLength": 256}}, "additionalProperties": true}},
    "display": {"type": "object", "additionalProperties": {"type": "object", "additionalProperties": true, "properties": {"show": {"type": "array", "items": {"type": "string"}}, "label": {"type": "string"}, "order": {"type": "integer"}, "style": {"type": "string"}, "as": {"type": "string"}, "values": {"type": "object", "additionalProperties": {"type": "object", "additionalProperties": true}}, "other": {"type": "object", "additionalProperties": true}}}}
  }
}
```

- [ ] **Step 2: Write the failing schema test**

```go
// internal/agentdocs/schema_test.go
package agentdocs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/notemeta"
)

func specPath(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", "docs", "specs", "gg-session-docs"}, parts...)...)
}

func loadSchema(t *testing.T, name string) *jsonschema.Resolved {
	t.Helper()
	data, err := os.ReadFile(specPath(name))
	if err != nil {
		t.Fatal(err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return rs
}

// tomlToJSON is the spec's TOML → JSON mapping: decode into any, re-encode.
func tomlToJSON(t *testing.T, data []byte) any {
	t.Helper()
	var v any
	if err := toml.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(v) // time.Time → RFC 3339 string
	if err != nil {
		t.Fatal(err)
	}
	var inst any
	if err := json.Unmarshal(raw, &inst); err != nil {
		t.Fatal(err)
	}
	return inst
}

// Everything gg writes validates against the published schemas (spec §9).
func TestSessionDocShapesMatchSchemas(t *testing.T) {
	t.Parallel()
	ov, note := loadSchema(t, "overview-frontmatter.schema.json"), loadSchema(t, "note.schema.json")
	now := time.Date(2026, 10, 18, 14, 32, 1, 0, time.UTC)
	for _, o := range []OverviewFile{
		{Title: "T", Text: "body\n"},
		{Title: "T", Author: "claude", Created: now, Text: "[a](a.go:1)\n"},
	} {
		data, err := EncodeOverviewFile(o)
		if err != nil {
			t.Fatal(err)
		}
		head, _, _ := splitFrontMatter(data)
		if err := ov.Validate(tomlToJSON(t, head)); err != nil {
			t.Errorf("overview %+v: %v", o, err)
		}
	}
	for _, n := range []NoteFile{
		{Path: "a.go", Start: 1, End: 1, Summary: "s", Lines: []string{"x"}},
		{Path: "dir/a.go", Start: 3, End: 4, Summary: "s", Rationale: "r", Author: "ada", Created: now, Lines: []string{"x", "y"},
			Meta: []notemeta.KV{{Key: "kind", Value: "design"}}, Display: notemeta.Display{"kind": {Show: []string{"badge"}}}},
	} {
		data, err := EncodeNoteFile(n)
		if err != nil {
			t.Fatal(err)
		}
		if err := note.Validate(tomlToJSON(t, data)); err != nil {
			t.Errorf("note %+v: %v", n, err)
		}
	}
	// What the schema refuses, gg's parser refuses too.
	for _, bad := range []string{"start = 1\nend = 1\nsummary = \"s\"\nlines = [\"x\"]\n", "path = \"a.go\"\nstart = 0\nend = 1\nsummary = \"s\"\nlines = [\"x\",\"y\"]\n"} {
		if err := note.Validate(tomlToJSON(t, []byte(bad))); err == nil {
			t.Errorf("schema accepted %q", bad)
		}
		if _, err := ParseNoteFile([]byte(bad)); err == nil {
			t.Errorf("parser accepted %q", bad)
		}
	}
}
```

- [ ] **Step 3: Write the failing golden test**

```go
// internal/agentdocs/examples_golden_test.go
package agentdocs

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/notemeta"
)

var updateExamples = flag.Bool("update-examples", false, "rewrite docs/specs/gg-session-docs/examples/ from exampleDocs() (read the diff before committing)")

// The example tree the documents anchor (examples/tree/).
var exampleTree = map[string]string{
	"internal/tui/scroll.go": "package tui\n\n// scroller coalesces wheel ticks.\ntype scroller struct{ mu sync.Mutex; delta int }\n\nfunc (s *scroller) push(d int) {\n\ts.mu.Lock()\n\ts.delta += d\n\ts.mu.Unlock()\n}\n\nfunc (s *scroller) flush() {\n\ts.mu.Lock()\n\tdefer s.mu.Unlock()\n\tif s.delta != 0 {\n\t\tapply(s.delta)\n\t\ts.delta = 0\n\t}\n}\n",
	"README.md":              "# demo\r\n\r\nA tree with CRLF endings.\r\nSecond paragraph.\r\n",
}

type exampleDoc struct {
	id, kind string
	over     *OverviewFile
	note     *NoteFile
	about    string // for ids.json
}

func exampleDocs() []exampleDoc {
	at := func(s string) time.Time { t, _ := time.Parse(time.RFC3339, s); return t.UTC() }
	return []exampleDoc{
		{id: "20261018T143201-9f3a", kind: KindOverview, about: "an overview with a file anchor, a range anchor, a note anchor and one UNRESOLVED anchor (missing.go)",
			over: &OverviewFile{Title: "How scroll coalescing works", Author: "claude", Created: at("2026-10-18T14:32:01Z"),
				Text: "Scroll events are coalesced per frame so a burst of wheel ticks costs one layout.\n\n## How it works\n1. [The wheel handler](internal/tui/scroll.go:6-10) adds the tick's delta under the lock.\n2. [flush](internal/tui/scroll.go:12-19) runs once per tick and applies the sum.\n3. [Why one lock](note:20261018T143205-c0d1) — the handler and the tick both reach the field.\n\n## Not covered\n- [the renderer](internal/tui/missing.go) is not in this tree: an unresolved anchor.\n- [the readme](README.md) has CRLF endings.\n"}},
		{id: "20261018T143301-0001", kind: KindOverview, about: "a minimal overview: title only, no author (reads as agent), no created (readers use the file's mtime)",
			over: &OverviewFile{Title: "Minimal", Text: "Just text, [one anchor](README.md:1).\n"}},
		{id: "20261018T143205-c0d1", kind: KindNote, about: "a note with meta and a display declaration; its lines match the tree exactly",
			note: &NoteFile{Path: "internal/tui/scroll.go", Start: 13, End: 14, Summary: "One lock around flush: the wheel handler and the tick both reach delta.", Rationale: "Without it a tick between two pushes could apply a partial sum.", Author: "claude", Created: at("2026-10-18T14:32:05Z"),
				Lines: []string{"\ts.mu.Lock()", "\tdefer s.mu.Unlock()"},
				Meta:  []notemeta.KV{{Key: "kind", Value: "design"}}, Display: notemeta.Display{"kind": {Show: []string{"badge"}, Values: map[string]notemeta.Value{"design": {Color: "blue"}}}}}},
		{id: "20261018T143210-77aa", kind: KindNote, about: "a note on CRLF content: lines carry no \\r; readers place it at 3-4",
			note: &NoteFile{Path: "README.md", Start: 3, End: 4, Summary: "Two lines of a CRLF file.", Author: "claude", Created: at("2026-10-18T14:32:10Z"), Lines: []string{"A tree with CRLF endings.", "Second paragraph."}}},
		{id: "20261018T143220-beef", kind: KindNote, about: "an OUTDATED note: its lines are not in the tree (the writer's file changed); readers keep 6-7 and mark it outdated",
			note: &NoteFile{Path: "internal/tui/scroll.go", Start: 6, End: 7, Summary: "Stale: the handler used to clamp here.", Author: "claude", Created: at("2026-10-18T14:32:20Z"), Lines: []string{"func (s *scroller) push(d int) {", "\tif d > 100 { d = 100 }"}}},
	}
}

func examplesDir() string { return filepath.Join("..", "..", "docs", "specs", "gg-session-docs", "examples") }

func TestExamplesAreWhatGGWrites(t *testing.T) {
	dir := examplesDir()
	if *updateExamples {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		for p, text := range exampleTree {
			full := filepath.Join(dir, "tree", filepath.FromSlash(p))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		ids := map[string]string{}
		for _, d := range exampleDocs() {
			var data []byte
			var err error
			if d.kind == KindOverview {
				data, err = EncodeOverviewFile(*d.over)
			} else {
				data, err = EncodeNoteFile(*d.note)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := WriteDoc(dir, d.id, d.kind, data, false); err != nil {
				t.Fatal(err)
			}
			ids[FileName(d.id, d.kind)] = d.about
		}
		raw, _ := json.MarshalIndent(ids, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "ids.json"), append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(examplesReadme), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Every example re-encodes to the bytes on disk (the spelling is gg's).
	for _, d := range exampleDocs() {
		want, err := os.ReadFile(filepath.Join(dir, FileName(d.id, d.kind)))
		if err != nil {
			t.Fatalf("%s: %v (run with -update-examples)", d.id, err)
		}
		var got []byte
		if d.kind == KindOverview {
			got, _ = EncodeOverviewFile(*d.over)
		} else {
			got, _ = EncodeNoteFile(*d.note)
		}
		if string(got) != string(want) {
			t.Errorf("%s differs from the fixture; run -update-examples and read the diff", d.id)
		}
	}
	// A store attached to the examples reads them back: placements as ids.json says.
	wt := t.TempDir()
	for p, text := range exampleTree {
		full := filepath.Join(wt, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(text), 0o644)
	}
	s := New()
	s.noWatch = true
	if err := s.Attach(root, wt, dir); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(s.Overviews(root)) != 2 || len(s.Problems(root)) != 0 {
		t.Fatalf("overviews %d problems %+v", len(s.Overviews(root)), s.Problems(root))
	}
	data, _ := os.ReadFile(filepath.Join(wt, "internal", "tui", "scroll.go"))
	notes := s.AlignedNotes(root, "internal/tui/scroll.go", Lines(data))
	if len(notes) != 2 || notes[0].ID != "20261018T143220-beef" || !notes[0].Outdated || notes[0].Start != 6 || notes[0].End != 7 ||
		notes[1].ID != "20261018T143205-c0d1" || notes[1].Outdated || notes[1].Start != 13 || notes[1].End != 14 {
		t.Fatalf("scroll.go notes = %+v", notes)
	}
	data, _ = os.ReadFile(filepath.Join(wt, "README.md"))
	notes = s.AlignedNotes(root, "README.md", Lines(data))
	if len(notes) != 1 || notes[0].Outdated || notes[0].Start != 3 || notes[0].End != 4 {
		t.Fatalf("README notes = %+v", notes)
	}
	o, _ := s.Overview("20261018T143201-9f3a")
	s.CheckAnchors(o.ID)
	o, _ = s.Overview(o.ID)
	var missing []string
	for _, a := range o.Anchors {
		if a.Missing {
			missing = append(missing, a.Dest)
		}
	}
	if strings.Join(missing, ",") != "internal/tui/missing.go" {
		t.Fatalf("unresolved = %v", missing)
	}
}

const examplesReadme = `# gg session docs — examples

Written by gigagit itself (` + "`internal/agentdocs` `TestExamplesAreWhatGGWrites -update-examples`" + `):
the spelling here is what gg writes, so a reader that reads these reads gg.

- ` + "`tree/`" + ` — the worktree the documents anchor (two files; README.md has CRLF endings).
- ` + "`*.md`" + ` — overviews, ` + "`*.toml`" + ` — notes. ` + "`ids.json`" + ` says what each one shows.
- Expected placements against ` + "`tree/`" + `: see ` + "`ids.json`" + ` (one note is outdated on purpose,
  one overview has one unresolved anchor on purpose).
`
```

- [ ] **Step 4: Run to verify failure**

Run: `cd /work/gigagit/.claude/worktrees/gg-session-docs && go test ./internal/agentdocs/ -run 'TestSessionDocShapes|TestExamples' 2>&1 | head`
Expected: `TestSessionDocShapesMatchSchemas` FAIL (schema files not found) until Step 1's files exist; `TestExamplesAreWhatGGWrites` FAIL with `… (run with -update-examples)`.

- [ ] **Step 5: Generate the examples, then run again**

Run: `cd /work/gigagit/.claude/worktrees/gg-session-docs && go test ./internal/agentdocs/ -run TestExamples -update-examples 2>&1 | tail -3 && go test ./internal/agentdocs/ 2>&1 | tail -3 && ls docs/specs/gg-session-docs/examples/ && cat docs/specs/gg-session-docs/examples/20261018T143205-c0d1.toml`
Expected: `ok` twice; the listing shows `README.md ids.json tree/ 20261018T143201-9f3a.md 20261018T143301-0001.md 20261018T143205-c0d1.toml 20261018T143210-77aa.toml 20261018T143220-beef.toml`; the printed note shows `[[meta]]` and `[display.kind]` tables in the spec's spelling. READ the generated files; if a spelling differs from spec §3's examples (e.g. quote style), update the spec's examples to the real output — the fixture is the proof.

- [ ] **Step 6: Write the README**

`docs/specs/gg-session-docs/README.md`:

```markdown
# gg session docs — conformance material for a reader or writer

Spec: `../../superpowers/specs/2026-10-18-gg-session-docs-design.md`
(files an agent writes into a worktree's `<inbox>/docs/`, shown by every
gg reader of that worktree: the TUI, gg web, the gg-notes IDEA plugin).
gigagit's `internal/agentdocs` is tested against these files; a reader that
is not gg runs them against its parser, placer and lister.

- `overview-frontmatter.schema.json`, `note.schema.json` — the PARSED shapes
  (TOML → JSON: tables are objects, arrays of tables are arrays, date-times
  are RFC 3339 strings), lenient: unknown keys allowed. What they cannot say
  — `lines.length == end - start + 1`, `start <= end`, the path segment
  rules — is in the spec §3.3 and in gg's parser
  (`TestSessionDocShapesMatchSchemas`, `TestNoteFileDefaultsAndRefusals`).
- `examples/` — a directory written by gg's own encoder plus the `tree/` it
  anchors; `ids.json` names what each document demonstrates and the
  placements a reader must reach (`TestExamplesAreWhatGGWrites`).
- `cold-anchor-cases.json` — first placement of a note from its `lines`
  alone (spec §4.3): `{name, file, note: {start, end, lines}, expect:
  {start, end, outdated}}` (`TestColdAnchorCases`). After that, re-anchoring
  on edits is `../gg-steer/reanchor-cases.json`.
- `id-vectors.json` — valid/invalid ids, file name ↔ (id, kind), and the
  entry names a reader ignores (`TestIDVectors`,
  `TestListDocsAcceptsDocumentsAndReportsTheRest`).
- The inbox path the directory lives under: `../gg-steer/repokey-vectors.json`.
```

- [ ] **Step 7: Commit**

```bash
cd /work/gigagit/.claude/worktrees/gg-session-docs && gg add docs/specs/gg-session-docs internal/agentdocs/schema_test.go internal/agentdocs/examples_golden_test.go && git commit -q -m "gg-session-docs conformance: schemas, gg-written examples with their tree, README" && git log --oneline -1
```

---

### Task 6: Docs, package map, changelog, plugin copies

**Files:**
- Modify: `CLAUDE.md` (the `steer` and `agentdocs` rows of the package map, one line each)
- Modify: `CHANGELOG.md` (a new Unreleased section at the top)
- Modify: `docs/CLAUDE-details.md` (a short "session docs" entry: the attach model, cold anchoring, ignore rules)
- Create: `/work/gg-notes/docs/gg-session-docs/` (copies with provenance header; the user commits there)

- [ ] **Step 1: Package map rows**

`agentdocs` row: replace "Memory-only store of what an agent shows beside the code" with: "Store of what an agent shows beside the code (temporary notes on open working-tree files, overview documents with anchors): memory for an unattached root, and — `Attach(root, worktree, docsDir)` — a cache of the worktree's session-docs DIRECTORY (`<inbox>/docs/`: `<id>.md` overviews with TOML front matter, `<id>.toml` notes carrying their anchor lines; spec 2026-10-18-gg-session-docs) that it loads, watches (fsnotify + poll) and writes through (temp + rename, writer-minted ids). Ids, alignment (`Align`, cold placement from `lines` then exact-line re-anchoring), the anchor grammar + `ParseOverview`, limits, reply prose, a change signal; keyed tours by fixed names. `Shared()` is the one per process the TUI and the page it hosts share. DAG leaf (markdown, steer, textdiff, notemeta, go-toml)."

`steer` row: add "…; `DocsDir(inbox)` and `WatchFilter` serve the session-docs store." before "stdlib + fsnotify only".

- [ ] **Step 2: CHANGELOG**

Under the top `## ` heading insert a new section:

```markdown
## Session docs, part 1: the file layer (no user-visible change yet)

### Added
- `internal/agentdocs` can attach a worktree to its session-documents
  directory (`<steer inbox>/docs/`): `<id>.md` overviews (a `+++` TOML front
  matter, then markdown whose links are anchors) and `<id>.toml` notes that
  carry the lines they sit on. The store loads the directory, watches it
  (fsnotify with a 2 s poll fallback), reconciles by file name, places a
  loaded note from its lines (cold anchoring) and then re-anchors as before,
  and writes every mutation of an attached root as a file (temp + rename,
  writer-minted ids, no lock). Nothing attaches yet: the TUI (part 2), gg web
  (part 3) and the CLI/skills (part 4) follow. Spec:
  `docs/superpowers/specs/2026-10-18-gg-session-docs-design.md`; conformance
  material for the IDEA plugin in `docs/specs/gg-session-docs/`.
```

- [ ] **Step 3: Plugin copies**

```bash
mkdir -p /work/gg-notes/docs/gg-session-docs && cd /work/gigagit/.claude/worktrees/gg-session-docs && cp -r docs/specs/gg-session-docs/. /work/gg-notes/docs/gg-session-docs/ && { printf '%s\n' "# gg session docs — files an agent writes, every reader shows" "" "> Copy of gigagit's \`docs/superpowers/specs/2026-10-18-gg-session-docs-design.md\`" "> (branch \`spec/gg-session-docs\`, not yet merged). gigagit is the source of truth; refresh" "> this copy when that file changes. The \`*.json\` files, \`examples/\` and \`README.md\` beside it" "> are gigagit's conformance material (\`docs/specs/gg-session-docs/\`)." ""; tail -n +2 docs/superpowers/specs/2026-10-18-gg-session-docs-design.md; } > /work/gg-notes/docs/gg-session-docs/SPEC.md && ls /work/gg-notes/docs/gg-session-docs/
```

Expected: the listing shows `README.md SPEC.md cold-anchor-cases.json examples id-vectors.json note.schema.json overview-frontmatter.schema.json`. Do NOT commit in `/work/gg-notes` (the user does).

Add to `/work/gg-notes/AGENTS.md` a §8 "Session docs (coming)": three lines — what the directory is, that the plugin's reader for it is plan-4 work on the gigagit side and a "read `docs/gg-session-docs/SPEC.md`" pointer; the plugin agent may start a parser against `examples/` and the cases now.

- [ ] **Step 4: Full test gate**

Run: `cd /work/gigagit/.claude/worktrees/gg-session-docs && ./test.sh race > /tmp/claude-1000/-work-gigagit/3f942bc5-9389-49a0-a00f-fef0425e1a09/scratchpad/race-plan1.log 2>&1; tail -3 /tmp/claude-1000/-work-gigagit/3f942bc5-9389-49a0-a00f-fef0425e1a09/scratchpad/race-plan1.log`
Expected: the log ends with `all green`. (A `| tail` exit code says nothing; read the words.)

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/gg-session-docs && gg add CLAUDE.md CHANGELOG.md docs/CLAUDE-details.md && git commit -q -m "docs: session docs part 1 — package map, changelog, details" && git log --oneline -1
```

---

## Self-review notes (done while writing)

- Spec coverage for THIS plan: §2 directory (Task 1 `DocsDir`, Task 3 ignore rules), §3.1 ids (Task 2), §3.2/§3.3 shapes (Task 2), §3.4 limits (Tasks 2–4), §4.2 reconcile (Task 4), §4.3 cold anchoring (Tasks 2, 4), §5 write (Task 3), §6.1 store (Task 4), §6.5 tour names (Task 4), §9 conformance (Tasks 2, 5). §4.4 reader UI, §6.2 CLI, §6.3 steer revision, §6.4 frontends, §7 skills, §8 plugin are plans 2–4 by design.
- Type consistency: `Doc{ID, Kind, ModTime, Size, Over, Note}`, `Problem{Name, Reason}`, `OverviewFile`, `NoteFile`, `ColdPlace(file, start, end, anchor) (s, e, outdated)`, `WriteDoc(dir, id, kind, data, replace)`, `RemoveDoc(dir, id, kind) (bool, error)`, `Attach(root, worktreeDir, docsDir)`, `pollEvery`, `noWatch` are used with the same names in every task.
- Known soft spot, ledger it if it bites: go-toml's exact error type names in `tomlErr` and its string quote style in the round-trip expectations (Task 2 Step 5 says what to do).
