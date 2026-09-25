# Open files — plan 4: agent verbs — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (inline, THIS session — the repo forbids subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An agent can open files into the user's open-files list without touching the screen, list them, and bring one to the front — `gg open|session navigate --background`, `gg session files [--json]`, `gg session files focus <id|path>[:<line>]`, and `open_files` in the session snapshot.

**Architecture:** Additive steer protocol (`Command.Background`, `Command.FileID`, `Reply.Files`, the `steer.OpenFile` wire type, commands `files` / `file_focus`). The TUI answers them from the existing registry (`m.openFiles`) with the existing helpers (`registerDoc`, `bringToFront`, `loadDoc`); a load-riding reply generalises today's `contentLandedMsg`. The CLI gains a TUI-only post-and-await helper that returns the `Reply`, so `files` can print its list. The web frontend refuses the new shapes until stage 5.

**Tech Stack:** Go 1.26, Bubble Tea, the `internal/steer` file inbox.

**Spec:** `docs/superpowers/specs/2026-09-24-open-files-design.md` → "Stage 4 — agent verbs", "Error handling", "Testing", "Docs".

## Global Constraints

- Reply `Detail` / `Error` and every CLI line are English protocol prose — never `i18n.T`. Status-bar text IS `i18n.T` with a key in all four bundles (the AST gates fail otherwise).
- Snapshot stays `version: 1`; `open_files` is `omitempty` (additive).
- Paths in any TUI row are cut in the MIDDLE (`elidePath` / `winRow.elide`), never the file name; keys go in the app bottom bar. (No new rows are planned; this is a guard.)
- Every new test calls `t.Parallel()`.
- Work in `.claude/worktrees/open-files-agent-verbs` (branch `feat/open-files-agent-verbs`); `cd <abs>` in every shell command.
- Gates: `./test.sh` and `./test.sh race` (≈15 min each on /mnt; never edit the tree while one runs).

## Rulings (made while planning — the user reviews these)

| # | Ruling | Why | Cost if wrong |
|---|--------|-----|---------------|
| R1 | The agent-facing id is `f<seq>` (the document's instance number, stored as `openFile.seq`), not `d.tag` | the tag holds `:` and `#`, so `<id>:<line>` could not be split | an id rename |
| R2 | `file_focus` carries the id in a NEW field `Command.FileID` (`file_id`), the path in `File`, the line in `Line` (`{No: n}`, side "") | `Command.ID` is already the command id | a field rename |
| R3 | `files focus <x>`: the CLI strips a trailing `:<digits>` as the line; `x` matching `^f[0-9]+$` goes to `FileID`, anything else to `File`. The TUI looks `FileID` up by id and, when no id matches, as a path (so a file literally named `f12` is still reachable) | one argument, two lookups | none — both lookups are tried |
| R4 | A path naming both a working-tree and a commit/shelf version picks the MOST RECENTLY SHOWN (first in the list) | that is the one the user last looked at | the agent focuses by id instead |
| R5 | `files` and a background navigate BYPASS `steerRefusal` (typing, a window owns the keyboard, an op is running): they never move the screen. `file_focus` keeps the full refusal | the headline flow opens files while the user reads | a background load lands while an op runs — harmless, loads are read-only |
| R6 | A background navigate never parks a `pendingSteer` and is never refused as "a previous navigate is still loading" | three back-to-back background opens is the headline flow | none |
| R7 | A background navigate for ANOTHER worktree is refused — `gg is showing worktree <A>, not <B>` — with no switch prompt (`askSteerSwitch` is a notice: it touches the screen) | "without touching the screen" | the agent switches first (`gg session navigate` foreground asks the user) |
| R8 | `files` / `file_focus` are not worktree-bound: they act on the list of the worktree the TUI shows (with `$GG_INBOX` that is the user's worktree, which is what the agent must talk about) | the list the user sees is the one that matters | none |
| R9 | `--background` of a file already open AND on screen: nothing moves (a line is NOT applied), reply `<path> is already open on screen`. Already open in the background: it goes to the top of the list, a line becomes its `pendingLine`, and a working-tree file is re-read (reply rides the load as for a new one) | "without touching the screen" | the agent uses `files focus <id>:<line>` |
| R10 | A background open shows the existing status `%s is in the background — ctrl+\ lists open files` (no new i18n key); an eviction's `closed %s (20 files open)` status wins, as in `wtBackgroundRow` | the user should know an agent added a file; the status bar is not the viewing area | one status line |
| R11 | The reply for a background open and for `file_focus` RIDES THE LOAD when one is started (the clamp is only known then); a loaded commit/shelf document lands its line synchronously and answers at once | one reply shape, `contentLandedDetail`'s | none |
| R12 | Every content-link reply (foreground too) names an eviction: `…; closed <path> (20 files open)`. `registerDoc` becomes a wrapper over `registerDocEv(d) (Model, *openFile)` — the reply never scrapes `statusMsg` (it is i18n text) | spec: "the reply names a file evicted over the cap" | none |
| R13 | `gg open --background` / `gg session navigate --background` need a LIVE TUI: no TUI → exit 1 `no gg TUI session for this worktree` (never a launch — a launch is the screen). `--background --web` and `--background` on a non-content link → exit 2. The CLI posts the new shapes to the TUI inbox only, never to a live web page; a web-only session → exit 1 `gg web does not keep open files yet` | "without touching the screen"; the web stage is later | none |
| R14 | `gg session files` always waits (it has no `--no-wait`); `files focus` and `--background` accept `--no-wait` like every other steering verb | a list nobody waits for is nothing | none |
| R15 | `gg session files` plain text: one row per file, tab-separated `<id>\t<path>\t<source>\t<line>\t<state>` — source `worktree`, `commit <sha7>` or `shelf <id>`; line `:N` or `-` (not loaded); state `shown`/`background`. Empty list → nothing on stdout, exit 0. `--json` → a JSON array of `steer.OpenFile` (`[]` when empty) | greppable, like `gg links` | a format tweak |
| R16 | `gg web`'s `toSteerWire` gets an explicit `Background` refusal (`background opens are not supported in gg web yet`) and an explicit `files`/`file_focus` arm (`open files are not supported in gg web yet`) instead of "unknown command" | the spec says web refuses them; a named reason beats "unknown" | none |

## Review Focus

1. **Reply lost when the load's document is evicted before it lands** — 21 rapid background opens: each reply still arrives (the load's `fileContentMsg` finds no document and is dropped, but `contentLandedMsg` answers from its own copy of the lines). Pinned by Task 5's eviction test (the evicting open's reply names the evicted file).
2. **A background open while a foreground navigate is parked** — must not be refused (R6). Pinned in Task 5.
3. **`files focus` on a background document whose load never landed** (a load failure placeholder) — `bringToFront` re-reads it; the line lands on the fill. Pinned in Task 6 (worktree doc path).
4. **Snapshot churn** — `open_files[].line` follows the cursor, so a moving cursor rewrites the snapshot each heartbeat, exactly as `cursor.link` already does. No test; accepted.
5. **`gg session files` against a TUI too old to know `files`** — it replies `unknown command "files"`, the CLI prints it and exits 1. Pinned in Task 7 (fake consumer replying that error).

---

### Task 1: Steer wire additions

**Files:**
- Modify: `internal/steer/steer.go`
- Test: `internal/steer/steer_test.go`

**Interfaces:**
- Produces: `Command.Background bool` (`json:"background,omitempty"`), `Command.FileID string` (`json:"file_id,omitempty"`), `Reply.Files []OpenFile` (`json:"files,omitempty"`), and

```go
// OpenFile is one file open in a live TUI: a row of `gg session files` and of
// the session snapshot's open_files. Protocol data, never display text.
type OpenFile struct {
	ID     string `json:"id"`             // "f<n>" — what `gg session files focus` takes
	Path   string `json:"path"`           // repo-relative, git slash form
	Source string `json:"source"`         // "worktree" | "commit" | "shelf"
	Rev    string `json:"rev,omitempty"`  // the commit sha / the shelf entry id
	Line   int    `json:"line,omitempty"` // the cursor, 1-based; 0 = not loaded
	State  string `json:"state"`          // "shown" | "background"
}
```

- [ ] **Step 1: failing test** — append to `steer_test.go`:

```go
func TestOpenFilesVerbsRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	id, err := Post(dir, Command{Cmd: "file_focus", FileID: "f3", Line: &Line{No: 12}, Background: false})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Post(dir, Command{Cmd: "navigate", File: "a.txt", Background: true}); err != nil {
		t.Fatal(err)
	}
	cmds := Drain(dir)
	if len(cmds) != 2 || cmds[0].ID != id || cmds[0].FileID != "f3" || cmds[0].Line.No != 12 || !cmds[1].Background {
		t.Fatalf("drained %+v", cmds)
	}
	want := []OpenFile{{ID: "f1", Path: "a.txt", Source: "worktree", Line: 3, State: "shown"}}
	if err := PostReply(dir, Reply{ID: id, OK: true, Files: want}); err != nil {
		t.Fatal(err)
	}
	r, ok := AwaitReply(dir, id, time.Second)
	if !ok || len(r.Files) != 1 || r.Files[0] != want[0] {
		t.Fatalf("reply = %+v ok=%v, want the list back", r, ok)
	}
}
```

- [ ] **Step 2:** `cd <wt> && go test ./internal/steer/ -run TestOpenFilesVerbsRoundTrip` — Expected: build FAIL (`unknown field FileID`).
- [ ] **Step 3:** add the three fields and `OpenFile` (docs as in Interfaces; extend the `Cmd` comment to `… | "files" | "file_focus"`; `Background` doc: "navigate: load a content link into the open-files list without showing it").
- [ ] **Step 4:** re-run — Expected: PASS.
- [ ] **Step 5:** commit `feat(steer): Background, FileID, Reply.Files and OpenFile for the open-files verbs`.

---

### Task 2: Open-file ids, the list's wire form, and the snapshot's `open_files`

**Files:**
- Modify: `internal/tui/open_file.go` (add `seq int64`; set in `newOpenFile`; `id()`)
- Modify: `internal/tui/open_files.go` (add `openFilesProto`)
- Modify: `internal/tui/session_snapshot.go` (`OpenFiles []steer.OpenFile json:"open_files,omitempty"`)
- Modify: `internal/mcp/state.go` (tool description mentions open files)
- Test: `internal/tui/open_files_agent_test.go` (new)

**Interfaces:**
- Consumes: `steer.OpenFile` (Task 1).
- Produces: `func (d *openFile) id() string` → `"f" + strconv.FormatInt(d.seq, 10)`; `func (m Model) openFilesProto() []steer.OpenFile` (current worktree's list, most recently shown first; `Line = d.p.cur+1` iff `docLoaded(d)`; `State` from `m.docShown(d)`); `func (m Model) findOpenFile(id, path string) *openFile` (id first, then path — R3/R4).

- [ ] **Step 1: failing tests** — new file `open_files_agent_test.go`:

```go
package tui

import (
	"encoding/json"
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

// bgDoc registers a loaded background document in m's current worktree.
func bgDoc(m Model, src fileSource, path string, n int) *openFile {
	d := newOpenFile(src, path)
	lines := make([]contentLine, n)
	for i := range lines {
		lines[i] = contentLine{text: "x", src: true}
	}
	d.p.lines = lines
	m.openFiles.touch(m.currentWorktree, d, m.docShown)
	return d
}

func TestOpenFilesProtoListsIDsLinesAndState(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	a := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 10)
	a.p.cur = 4
	c := bgDoc(m, fileSource{kind: srcCommit, rev: "0123456789abcdef0123456789abcdef01234567"}, "b.txt", 3)
	m = m.pushLayer(&fileViewer{c})
	got := m.openFilesProto()
	if len(got) != 2 {
		t.Fatalf("got %+v, want 2 files", got)
	}
	if got[0].Path != "b.txt" || got[0].Source != "commit" || got[0].Rev == "" || got[0].State != "shown" || got[0].ID != c.id() {
		t.Errorf("first = %+v, want the shown commit version of b.txt", got[0])
	}
	if got[1].Path != "a.txt" || got[1].Source != "worktree" || got[1].Line != 5 || got[1].State != "background" {
		t.Errorf("second = %+v, want a.txt in the background at line 5", got[1])
	}
}

func TestFindOpenFileByIDThenPathMostRecentFirst(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	old := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 2)
	recent := bgDoc(m, fileSource{kind: srcCommit, rev: "abc"}, "a.txt", 2)
	if d := m.findOpenFile(old.id(), ""); d != old {
		t.Error("id lookup missed")
	}
	if d := m.findOpenFile("", "a.txt"); d != recent {
		t.Error("a path must pick the most recently shown version")
	}
	if d := m.findOpenFile("a.txt", ""); d != recent {
		t.Error("an id that matches no id must be tried as a path")
	}
	if d := m.findOpenFile("f999999", ""); d != nil {
		t.Error("an unknown id found a document")
	}
}

func TestSnapshotPublishesOpenFiles(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 3)
	data, err := json.Marshal(buildSessionSnapshot(m))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Version   int               `json:"version"`
		OpenFiles []steer.OpenFile `json:"open_files"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.Version != 1 || len(s.OpenFiles) != 1 || s.OpenFiles[0].Path != "a.txt" || s.OpenFiles[0].State != "background" {
		t.Fatalf("snapshot = %s", data)
	}
}
```

- [ ] **Step 2:** `go test ./internal/tui/ -run 'OpenFilesProto|FindOpenFile|SnapshotPublishesOpenFiles'` — Expected: build FAIL (`d.id undefined`).
- [ ] **Step 3:** implement:

```go
// open_file.go — in openFile: 
	// seq is the document's instance number: its tag's suffix and, as
	// f<seq>, the id an agent names it by (gg session files).
	seq int64
// newOpenFile:
	d.seq = openFileSeq.Add(1)
	d.tag = fmt.Sprintf("%s#%d", d.key(), d.seq)

// id is the document's agent-facing id (gg session files focus <id>).
func (d *openFile) id() string { return "f" + strconv.FormatInt(d.seq, 10) }
```

```go
// open_files.go
// openFilesProto is the current worktree's open files in their wire form,
// most recently shown first — `gg session files` and the snapshot.
func (m Model) openFilesProto() []steer.OpenFile {
	var out []steer.OpenFile
	for _, d := range m.openFiles.list(m.currentWorktree) {
		f := steer.OpenFile{ID: d.id(), Path: d.path, Source: "worktree", State: "background"}
		switch d.src.kind {
		case srcCommit:
			f.Source, f.Rev = "commit", d.src.rev
		case srcShelf:
			f.Source, f.Rev = "shelf", d.src.rev
		}
		if docLoaded(d) {
			f.Line = d.p.cur + 1
		}
		if m.docShown(d) {
			f.State = "shown"
		}
		out = append(out, f)
	}
	return out
}

// findOpenFile is the current worktree's open file an agent named: by id,
// else — or when no id matches — by path, the most recently shown version.
func (m Model) findOpenFile(id, path string) *openFile {
	l := m.openFiles.list(m.currentWorktree)
	if id != "" {
		for _, d := range l {
			if d.id() == id {
				return d
			}
		}
		path = id
	}
	for _, d := range l {
		if d.path == path {
			return d
		}
	}
	return nil
}
```

Snapshot: field `OpenFiles []steer.OpenFile \`json:"open_files,omitempty"\`` after `Conflict`; in `buildSessionSnapshot`, `s.OpenFiles = m.openFilesProto()`. MCP description: append `, the files open in the TUI (open_files: id, path, source, line, shown/background)` before `. session is null`.

- [ ] **Step 4:** re-run — Expected: PASS; also `go test ./internal/tui/ -run 'Snapshot|OpenFile'` PASS and `go test ./internal/mcp/` PASS.
- [ ] **Step 5:** commit `feat(tui): open-file ids and open_files in the session snapshot`.

---

### Task 3: Eviction-aware registration and a generalised landing reply

**Files:**
- Modify: `internal/tui/open_files.go` (`registerDocEv`)
- Modify: `internal/tui/file_viewer.go` (`openFileViewerEv`)
- Modify: `internal/tui/steer_nav.go` (`contentLandedMsg`, `contentLandedDetail`, `steerNavigateContent`)
- Modify: `internal/tui/model.go` (the `contentLandedMsg` arm)
- Test: `internal/tui/open_files_agent_test.go`, existing `steer_content_test.go` stays green

**Interfaces:**
- Produces:
  - `func (m Model) registerDocEv(d *openFile) (Model, *openFile)` — `registerDoc` = `m, _ = m.registerDocEv(d)`.
  - `func (m Model) openFileViewerEv(path string, line int) (Model, tea.Cmd, *openFile)`; `openFileViewer` wraps it.
  - `contentLandedMsg{load fileContentMsg; cmd steer.Command; line int; lead string; evicted string}` — `lead` is the reply's opening ("opened a.txt", "opened a.txt in the background", "focused a.txt"); `evicted` a path or "".
  - `func landedDetail(lead string, line int, lines []contentLine, evicted string) string` (replaces `contentLandedDetail`): `lead` + (` at line N` | ` at line N (line L is past the end, N lines)`) + (`; closed <evicted> (20 files open)` when set).

- [ ] **Step 1: failing tests** (append):

```go
func TestLandedDetailNamesTheLineClampAndEviction(t *testing.T) {
	t.Parallel()
	lines := []contentLine{{text: "a", src: true}, {text: "b", src: true}}
	for _, tc := range []struct {
		lead    string
		line    int
		evicted string
		want    string
	}{
		{"opened a.txt", 0, "", "opened a.txt"},
		{"opened a.txt in the background", 2, "", "opened a.txt in the background at line 2"},
		{"focused a.txt", 9, "", "focused a.txt at line 2 (line 9 is past the end, 2 lines)"},
		{"opened a.txt", 1, "old.go", "opened a.txt at line 1; closed old.go (20 files open)"},
	} {
		if got := landedDetail(tc.lead, tc.line, lines, tc.evicted); got != tc.want {
			t.Errorf("landedDetail(%q,%d,%q) = %q, want %q", tc.lead, tc.line, tc.evicted, got, tc.want)
		}
	}
}

// fill20 registers 20 background documents f0.txt … f19.txt (f0 oldest).
func fill20(m Model) {
	for i := 0; i < maxOpenFiles; i++ {
		bgDoc(m, fileSource{kind: srcWorktree}, fmt.Sprintf("f%d.txt", i), 1)
	}
}

func TestForegroundContentNavigateNamesTheEvictedFile(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	fill20(m)
	c := steer.Command{ID: "c-ev", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"}, HintKind: model.ContentHintKind, HintID: model.ContentHintID, Wait: true}
	nm, cmd := m.applySteer(c)
	nm = pumpAll(t, nm, cmd)
	r, ok := steer.AwaitReply(nm.steerDir, "c-ev", time.Second)
	if !ok || !r.OK || r.Detail != "opened a.txt; closed f0.txt (20 files open)" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}
```

(imports: `fmt`, `time`, `model`.)

- [ ] **Step 2:** run `-run 'LandedDetail|ForegroundContentNavigateNames'` — Expected: build FAIL (`landedDetail undefined`).
- [ ] **Step 3:** implement. `registerDocEv`:

```go
// registerDocEv puts d first in the current worktree's open files — call it
// once d is in its frame — and returns the file dropped over the cap (named
// in the status too), or nil.
func (m Model) registerDocEv(d *openFile) (Model, *openFile) {
	if m.openFiles == nil {
		return m, nil
	}
	ev := m.openFiles.touch(m.currentWorktree, d, m.docShown)
	if ev != nil {
		m.statusMsg = i18n.T("closed %s (%d files open)", ev.path, maxOpenFiles)
	}
	return m, ev
}
```

`openFileViewerEv` is today's `openFileViewer` body with `m, ev = m.registerDocEv(d)` and returns `ev`. `steerNavigateContent` uses it and sends `contentLandedMsg{…, lead: "opened " + c.File, evicted: evictedPath(ev)}` where `func evictedPath(d *openFile) string` returns `""` for nil. The `model.go` arm calls `landedDetail(msg.lead, msg.line, msg.load.lines, msg.evicted)`; on `msg.load.err` the failure text stays `reading <file>: <err>`. Delete `contentLandedDetail`; update its callers/tests (`grep -rn contentLandedDetail internal/tui`).

- [ ] **Step 4:** `go test ./internal/tui/ -run 'LandedDetail|ForegroundContent|SteerContent|OpenFile|FileViewer'` — Expected: PASS.
- [ ] **Step 5:** commit `feat(tui): a content-link reply names a file closed over the cap`.

---

### Task 4: `files` command and the dispatch rules

**Files:**
- Modify: `internal/tui/steer.go` (`applySteer`, `steerEnumRefusal`)
- Create: `internal/tui/steer_files.go` (`steerFiles`, later `steerNavigateBackground`, `steerFileFocus`)
- Test: `internal/tui/steer_files_test.go` (new)

**Interfaces:**
- Consumes: `openFilesProto` (Task 2).
- Produces: `func (m Model) steerFiles(c steer.Command) (Model, tea.Cmd)`; `applySteer` routing:

```go
func (m Model) applySteer(c steer.Command) (Model, tea.Cmd) {
	if why := steerEnumRefusal(c); why != "" {
		return m, m.answerSteer(c, steerFail(c, why))
	}
	// The open-files list verbs never move the screen (R5): no refusal for a
	// user who is typing or a window that owns the keyboard.
	switch {
	case c.Cmd == "files":
		return m.steerFiles(c)
	case c.Cmd == "navigate" && c.Background:
		return m.steerNavigateBackground(c)
	}
	if why := m.steerRefusal(); why != "" {
		return m, m.answerSteer(c, steerFail(c, why))
	}
	… (unchanged; add `case "file_focus": return m.steerFileFocus(c)`)
}
```

NOTE the order change: enum refusal now runs before `steerRefusal`. Both only answer; no existing reply depends on which fires first except a command that is both malformed AND arrives while the user types — it now hears the malformation. Accepted (ledger it).

`steerEnumRefusal` additions (English):
- `c.Background && c.Cmd != "navigate"` → `background applies to navigate only`
- `c.Background && c.HintKind != model.ContentHintKind` → `background needs a content link`
- `c.Cmd == "file_focus" && c.FileID == "" && c.File == ""` → `file_focus needs an id or a path`
- `c.Line != nil && c.Line.No < 0` → `a line number is 1-based`

Until Tasks 5/6 land, `steerNavigateBackground` / `steerFileFocus` are stubs replying `steerFail(c, "not implemented")` — Task 5/6 replace them (both in this plan; no stub survives the branch).

- [ ] **Step 1: failing tests**:

```go
package tui

import (
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/steer"
)

func TestSteerFilesRepliesWithTheListEvenWhileTheUserTypes(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 3)
	m.filterTyping = true // steerRefusal would say "the user is typing"
	nm, cmd := m.applySteer(steer.Command{ID: "c-f", Cmd: "files", Wait: true})
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(nm.steerDir, "c-f", time.Second)
	if !ok || !r.OK || len(r.Files) != 1 || r.Files[0].Path != "a.txt" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestSteerFilesEmptyListIsOK(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	nm, cmd := m.applySteer(steer.Command{ID: "c-e", Cmd: "files", Wait: true})
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(nm.steerDir, "c-e", time.Second)
	if !ok || !r.OK || len(r.Files) != 0 || r.Detail != "no open files" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestSteerOpenFilesMalformedCommandsAreRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		c    steer.Command
		want string
	}{
		{steer.Command{Cmd: "focus", Panel: "files", Background: true}, "background applies to navigate only"},
		{steer.Command{Cmd: "navigate", File: "a.txt", Background: true}, "background needs a content link"},
		{steer.Command{Cmd: "file_focus"}, "file_focus needs an id or a path"},
		{steer.Command{Cmd: "file_focus", FileID: "f1", Line: &steer.Line{No: -1}}, "a line number is 1-based"},
	} {
		if got := steerEnumRefusal(tc.c); got != tc.want {
			t.Errorf("steerEnumRefusal(%+v) = %q, want %q", tc.c, got, tc.want)
		}
	}
}
```

- [ ] **Step 2:** run `-run 'SteerFiles|SteerOpenFilesMalformed'` — Expected: FAIL (`unknown command "files"` / wrong refusal text).
- [ ] **Step 3:** implement `steer_files.go`:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/steer"
)

// steerFiles answers `gg session files`: the current worktree's open files,
// most recently shown first. Read-only — it never touches the screen.
func (m Model) steerFiles(c steer.Command) (Model, tea.Cmd) {
	r := steerOK(c, "")
	r.Files = m.openFilesProto()
	if len(r.Files) == 0 {
		r.Detail = "no open files"
	}
	return m, m.answerSteer(c, r)
}
```

plus the two stubs and the `applySteer`/`steerEnumRefusal` edits above.

- [ ] **Step 4:** `go test ./internal/tui/ -run 'Steer'` — Expected: PASS (whole steer family, catches the reorder).
- [ ] **Step 5:** commit `feat(tui): the files steer command lists open files`.

---

### Task 5: Background navigate

**Files:**
- Modify: `internal/tui/steer_files.go` (`steerNavigateBackground`)
- Test: `internal/tui/steer_files_test.go`

**Interfaces:**
- Consumes: `registerDocEv`, `contentLandedMsg{lead, evicted}`, `evictedPath` (Task 3); `m.svc.WorktreeFilesPresent`; `loadDoc`.
- Produces: `func (m Model) steerNavigateBackground(c steer.Command) (Model, tea.Cmd)`.

- [ ] **Step 1: failing tests**:

```go
func bgNav(id, file string, line int) steer.Command {
	c := steer.Command{ID: id, Cmd: "navigate", File: file, Background: true,
		Target: &steer.Target{State: "unstaged"}, HintKind: model.ContentHintKind, HintID: model.ContentHintID, Wait: true}
	if line > 0 {
		c.Line = &steer.Line{Side: "new", No: line}
	}
	return c
}

func TestBackgroundNavigateLoadsWithoutAFrame(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.filterTyping = true // R5: not refused
	nm, cmd := m.applySteer(bgNav("b-1", "a.txt", 30))
	nm = pumpAll(t, nm, cmd)
	if layerOf[*fileViewer](nm) != nil || nm.filesPreview != nil {
		t.Fatal("a background open put the file on screen")
	}
	d := nm.openFiles.find(nm.currentWorktree, docKey(fileSource{kind: srcWorktree}, "a.txt"))
	if d == nil || !docLoaded(d) || d.p.cur != 29 {
		t.Fatalf("doc = %+v, want a.txt loaded with the cursor on line 30", d)
	}
	r, ok := steer.AwaitReply(nm.steerDir, "b-1", time.Second)
	if !ok || !r.OK || r.Detail != "opened a.txt in the background at line 30" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestBackgroundNavigateClampsAndNamesTheEviction(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	fill20(m)
	nm, cmd := m.applySteer(bgNav("b-2", "a.txt", 99))
	nm = pumpAll(t, nm, cmd)
	r, ok := steer.AwaitReply(nm.steerDir, "b-2", time.Second)
	want := "opened a.txt in the background at line 40 (line 99 is past the end, 40 lines); closed f0.txt (20 files open)"
	if !ok || !r.OK || r.Detail != want {
		t.Fatalf("reply = %+v ok=%v, want %q", r, ok, want)
	}
}

func TestBackgroundNavigateIsNotBlockedByAParkedNavigate(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.pendingSteer = &pendingSteer{cmd: steer.Command{ID: "other"}, stage: steerStageDiff, at: time.Now()}
	nm, cmd := m.applySteer(bgNav("b-3", "a.txt", 0))
	nm = pumpAll(t, nm, cmd)
	r, ok := steer.AwaitReply(nm.steerDir, "b-3", time.Second)
	if !ok || !r.OK || r.Detail != "opened a.txt in the background" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestBackgroundNavigateOfAShownFileMovesNothing(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	d := bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 40)
	d.p.cur = 2
	m = m.pushLayer(&fileViewer{d})
	nm, cmd := m.applySteer(bgNav("b-4", "a.txt", 30))
	nm = pumpAll(t, nm, cmd)
	if d.p.cur != 2 {
		t.Errorf("cursor moved to %d on a file the user is reading", d.p.cur+1)
	}
	r, ok := steer.AwaitReply(nm.steerDir, "b-4", time.Second)
	if !ok || !r.OK || r.Detail != "a.txt is already open on screen" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestBackgroundNavigateRefusals(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	for _, tc := range []struct {
		c    steer.Command
		want string
	}{
		{bgNav("r-1", "gone.txt", 0), "gone.txt is not in the working tree"},
		{func() steer.Command { c := bgNav("r-2", "a.txt", 0); c.Worktree = "/elsewhere"; return c }(),
			"gg is showing worktree " + m.snapshotWorktree + ", not /elsewhere"},
	} {
		nm, cmd := m.applySteer(tc.c)
		runSteerCmd(t, cmd)
		r, ok := steer.AwaitReply(nm.steerDir, tc.c.ID, time.Second)
		if !ok || r.OK || r.Error != tc.want {
			t.Errorf("%s: reply = %+v ok=%v, want %q", tc.c.ID, r, ok, tc.want)
		}
		if nm.steerAsk != nil {
			t.Errorf("%s: a background open asked the user to switch", tc.c.ID)
		}
	}
}
```

(If `m.snapshotWorktree` is empty in `loadedNavModel`, set it to `m.currentWorktree` at the top of the refusal test.)

- [ ] **Step 2:** run `-run BackgroundNavigate` — Expected: FAIL (`not implemented`).
- [ ] **Step 3:** implement:

```go
// steerNavigateBackground loads a content link into the open-files list
// without showing it (gg open --background): no frame, no panel move, no
// parked navigate. The reply rides the load — only the lines say where the
// cursor landed.
func (m Model) steerNavigateBackground(c steer.Command) (Model, tea.Cmd) {
	if c.Worktree != "" && !domain.SameCheckout(c.Worktree, m.snapshotWorktree) {
		return m, m.answerSteer(c, steerFail(c, "gg is showing worktree "+m.snapshotWorktree+", not "+c.Worktree))
	}
	ctx, cancel := updateThreadCtx(updateThreadGitTimeout)
	defer cancel()
	present, err := m.svc.WorktreeFilesPresent(ctx, []string{c.File})
	if err := busyOr(err); err != nil {
		return m, m.answerSteer(c, steerFail(c, "checking "+c.File+": "+err.Error()))
	}
	if !present[c.File] {
		return m, m.answerSteer(c, steerFail(c, c.File+" is not in the working tree"))
	}
	src := fileSource{kind: srcWorktree}
	d := m.openFiles.find(m.currentWorktree, docKey(src, c.File))
	if d != nil && m.docShown(d) || d == nil && m.filesPreview != nil && m.filesPreview.path == c.File && m.filesPreview.src == src {
		return m, m.answerSteer(c, steerOK(c, c.File+" is already open on screen"))
	}
	line := 0
	if c.Line != nil {
		line = c.Line.No
	}
	if d == nil {
		d = newOpenFile(src, c.File)
	} else if line == 0 {
		d.keepPlace()
	}
	d.pendingLine = line
	m.statusMsg = ""
	m, ev := m.registerDocEv(d)
	if m.statusMsg == "" {
		m.statusMsg = i18n.T("%s is in the background — ctrl+\\ lists open files", d.path)
	}
	load := m.loadDoc(d)
	lead, evicted := "opened "+c.File+" in the background", evictedPath(ev)
	return m, func() tea.Msg {
		return contentLandedMsg{load: load().(fileContentMsg), cmd: c, line: line, lead: lead, evicted: evicted}
	}
}
```

(A document already `loading` is re-read anyway — `fill` is idempotent and `d.loading` is set again; ledger if the watcher test objects.)

- [ ] **Step 4:** `go test ./internal/tui/ -run 'BackgroundNavigate|Steer|OpenFile'` — Expected: PASS.
- [ ] **Step 5:** commit `feat(tui): a background content navigate loads into the open-files list`.

---

### Task 6: `file_focus`

**Files:**
- Modify: `internal/tui/steer_files.go` (`steerFileFocus`)
- Test: `internal/tui/steer_files_test.go`

**Interfaces:**
- Consumes: `findOpenFile` (Task 2), `bringToFront`, `landPendingLine`, `viewerGeom`, `contentLandedMsg`/`landedDetail` (Task 3).
- Produces: `func (m Model) steerFileFocus(c steer.Command) (Model, tea.Cmd)`.

- [ ] **Step 1: failing tests**:

```go
func TestFileFocusBringsACommitVersionToTheFrontAtALine(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	d := bgDoc(m, fileSource{kind: srcCommit, rev: "abc"}, "b.txt", 10)
	nm, cmd := m.applySteer(steer.Command{ID: "ff-1", Cmd: "file_focus", FileID: d.id(), Line: &steer.Line{No: 7}, Wait: true})
	nm = pumpAll(t, nm, cmd)
	fv, ok := nm.topLayer().(*fileViewer)
	if !ok || fv.openFile != d || d.p.cur != 6 {
		t.Fatalf("top = %T cur=%d, want b.txt's viewer on line 7", nm.topLayer(), d.p.cur+1)
	}
	r, ok := steer.AwaitReply(nm.steerDir, "ff-1", time.Second)
	if !ok || !r.OK || r.Detail != "focused b.txt at line 7" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestFileFocusWorktreeFileReloadsAndLandsTheLine(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	nm, cmd := m.applySteer(bgNav("b-5", "a.txt", 0))
	nm = pumpAll(t, nm, cmd)
	steer.AwaitReply(nm.steerDir, "b-5", time.Second)
	nm, cmd = nm.applySteer(steer.Command{ID: "ff-2", Cmd: "file_focus", File: "a.txt", Line: &steer.Line{No: 12}, Wait: true})
	nm = pumpAll(t, nm, cmd)
	fv, ok := nm.topLayer().(*fileViewer)
	if !ok || fv.p.cur != 11 {
		t.Fatalf("top = %T, want a.txt's viewer on line 12", nm.topLayer())
	}
	r, ok := steer.AwaitReply(nm.steerDir, "ff-2", time.Second)
	if !ok || !r.OK || r.Detail != "focused a.txt at line 12" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
}

func TestFileFocusUnknownAndRefused(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	nm, cmd := m.applySteer(steer.Command{ID: "ff-3", Cmd: "file_focus", FileID: "f424242", Wait: true})
	runSteerCmd(t, cmd)
	if r, ok := steer.AwaitReply(nm.steerDir, "ff-3", time.Second); !ok || r.OK || r.Error != "no open file f424242" {
		t.Fatalf("reply = %+v ok=%v", r, ok)
	}
	bgDoc(m, fileSource{kind: srcWorktree}, "a.txt", 3)
	m.filterTyping = true
	nm, cmd = m.applySteer(steer.Command{ID: "ff-4", Cmd: "file_focus", File: "a.txt", Wait: true})
	runSteerCmd(t, cmd)
	if r, ok := steer.AwaitReply(nm.steerDir, "ff-4", time.Second); !ok || r.OK || r.Error != "the user is typing" {
		t.Fatalf("reply = %+v ok=%v, want the typing refusal", r, ok)
	}
}
```

- [ ] **Step 2:** run `-run FileFocus` — Expected: FAIL (`not implemented`).
- [ ] **Step 3:** implement:

```go
// steerFileFocus brings an open file to the front (gg session files focus):
// the switcher's enter, optionally at a line. A load it starts carries the
// reply; a loaded commit/shelf version lands the line at once.
func (m Model) steerFileFocus(c steer.Command) (Model, tea.Cmd) {
	d := m.findOpenFile(c.FileID, c.File)
	if d == nil {
		name := c.FileID
		if name == "" {
			name = c.File
		}
		return m, m.answerSteer(c, steerFail(c, "no open file "+name))
	}
	m = m.steerToPanels()
	line := 0
	if c.Line != nil {
		line = c.Line.No
	}
	d.pendingLine = line
	m, load := m.bringToFront(d)
	lead := "focused " + d.path
	if load == nil {
		rows, _ := m.viewerGeom()
		if n := d.landPendingLine(rows); n != "" {
			m.statusMsg = n
		}
		return m, m.answerSteer(c, steerOK(c, landedDetail(lead, line, d.p.lines, "")))
	}
	return m, func() tea.Msg {
		return contentLandedMsg{load: load().(fileContentMsg), cmd: c, line: line, lead: lead}
	}
}
```

Check while implementing: `steerToPanels` must not detach the document (it pops poppable layers — a covered viewer of `d` would be popped; `bringToFront` then pushes a new frame, which is right). If `bringToFront`'s early `filesPreview` arm fires (d is the preview, nothing on top) it returns no load and the line lands synchronously — correct. If `bringToFront` pushes a frame and keepPlace/reload runs with `pendingLine` set, `fill` skips `keep` and lands the line.

- [ ] **Step 4:** `go test ./internal/tui/ -run 'FileFocus|Steer|OpenFile|Switcher|Sessions'` — Expected: PASS.
- [ ] **Step 5:** commit `feat(tui): file_focus brings an open file to the front`.

---

### Task 7: CLI — `gg session files [--json]` and `files focus`

**Files:**
- Modify: `internal/cli/session.go` (dispatch, usage line, `steerTUI`, `sessionFiles`)
- Create: `internal/cli/session_files.go`
- Test: `internal/cli/session_files_test.go` (new)

**Interfaces:**
- Consumes: `steer.OpenFile`, `Reply.Files`, `Command.FileID` (Task 1).
- Produces:
  - `func steerTUI(dir string, c steer.Command, noWait bool, stdout, stderr io.Writer) (steer.Reply, int, bool)` — TUI-only post-and-await: no TUI live → `no gg TUI session for this worktree` (or, when only a web page is live, `gg web does not keep open files yet`), exit 1; `noWait` prints the id (caller) and returns `ok=false`; a timeout prints `queued: no answer from the TUI within 2s` and exits 0 like `sendSteer`. Returns the reply when one came.
  - `func printSteerReply(r steer.Reply, stdout, stderr io.Writer) int` — `Detail` to stdout exit 0 / `Error` to stderr exit 1 (sendSteer's tail, extracted and reused there).
  - `func parseFileTarget(s string) (id, path string, line int)` — R3.
  - `func sessionFiles(dir string, args []string, stdout, stderr io.Writer) int`.

- [ ] **Step 1: failing tests** (`session_files_test.go`):

```go
package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

var twoFiles = []steer.OpenFile{
	{ID: "f3", Path: "src/a.go", Source: "worktree", Line: 12, State: "shown"},
	{ID: "f1", Path: "b.txt", Source: "commit", Rev: "0123456789abcdef0123456789abcdef01234567", State: "background"},
}

func TestSessionFilesPrintsRows(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	seen := answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Files: twoFiles} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"files"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	want := "f3\tsrc/a.go\tworktree\t:12\tshown\nf1\tb.txt\tcommit 0123456\t-\tbackground\n"
	if out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if c := <-seen; c.Cmd != "files" || !c.Wait {
		t.Errorf("posted %+v, want a waited files command", c)
	}
}

func TestSessionFilesJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Files: twoFiles} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"files", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	var got []steer.OpenFile
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || len(got) != 2 || got[0] != twoFiles[0] {
		t.Fatalf("stdout = %s err=%v", out.String(), err)
	}
}

func TestSessionFilesEmptyJSONIsAnArray(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Detail: "no open files"} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"files", "--json"}, &out, &errb); code != 0 || strings.TrimSpace(out.String()) != "[]" {
		t.Fatalf("exit=%d stdout=%q", code, out.String())
	}
}

func TestSessionFilesFocusPostsIDOrPathAndLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		arg        string
		id, path   string
		line       int
	}{
		{"f3", "f3", "", 0},
		{"f3:40", "f3", "", 40},
		{"src/a.go", "", "src/a.go", 0},
		{"src/a.go:7", "", "src/a.go", 7},
		{"dir:x/a.go", "", "dir:x/a.go", 0},
	} {
		dir := t.TempDir()
		livePresence(t, dir)
		seen := answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Detail: "focused"} })
		var out, errb bytes.Buffer
		if code := runSession(dir, nil, []string{"files", "focus", tc.arg}, &out, &errb); code != 0 {
			t.Fatalf("%s: exit = %d (stderr %q)", tc.arg, code, errb.String())
		}
		c := <-seen
		gotLine := 0
		if c.Line != nil {
			gotLine = c.Line.No
		}
		if c.Cmd != "file_focus" || c.FileID != tc.id || c.File != tc.path || gotLine != tc.line {
			t.Errorf("%s: posted %+v", tc.arg, c)
		}
	}
}

func TestSessionFilesFocusUnknownExitsOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, Error: "no open file f9"} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"files", "focus", "f9"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "no open file f9") {
		t.Fatalf("exit=%d stderr=%q", code, errb.String())
	}
}

func TestSessionFilesNeedsALiveTUI(t *testing.T) {
	t.Parallel()
	var out, errb bytes.Buffer
	if code := runSession(t.TempDir(), nil, []string{"files"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "no gg TUI session for this worktree") {
		t.Fatalf("exit=%d stderr=%q", code, errb.String())
	}
	dir := t.TempDir()
	srv, url := newSteerServer(t, 202, "")
	_ = srv
	liveWebPresence(t, dir, url)
	errb.Reset()
	if code := runSession(dir, nil, []string{"files"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "gg web does not keep open files yet") {
		t.Fatalf("web only: exit=%d stderr=%q", code, errb.String())
	}
	if len(srv.commands()) != 0 {
		t.Error("files was posted to the web page")
	}
}

func TestSessionFilesUsageErrorsExitTwo(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"files", "extra"},
		{"files", "focus"},
		{"files", "focus", "a", "b"},
		{"files", "--no-wait"},
		{"files", "focus", "f1:0"},
	} {
		var out, errb bytes.Buffer
		if code := runSession(t.TempDir(), nil, args, &out, &errb); code != 2 {
			t.Errorf("%v: exit = %d (stderr %q), want 2", args, code, errb.String())
		}
	}
}

func TestSessionFilesOldTUIUnknownCommandExitsOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, Error: `unknown command "files"`} })
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"files"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), `unknown command "files"`) {
		t.Fatalf("exit=%d stderr=%q", code, errb.String())
	}
}
```

(Check `newSteerServer`'s real signature in `session_test.go:410` and adapt the call; `runSession` with a nil svc must not touch svc on the `files` path.)

- [ ] **Step 2:** `go test ./internal/cli/ -run SessionFiles` — Expected: FAIL (`unknown subcommand "files"`).
- [ ] **Step 3:** implement `session_files.go`:

```go
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strconv"

	"github.com/homeend/gigagit/internal/steer"
)

const filesUsage = "usage: gg session files [--json]\n" +
	"       gg session files focus <id|path>[:<line>] [--no-wait]"

var fileIDPattern = regexp.MustCompile(`^f[0-9]+$`)
var trailingLine = regexp.MustCompile(`^(.+):([0-9]+)$`)

// parseFileTarget splits `files focus`'s argument (ruling R3): a trailing
// :<digits> is the line; f<digits> is an id, anything else a path.
func parseFileTarget(s string) (id, path string, line int) {
	if m := trailingLine.FindStringSubmatch(s); m != nil {
		s = m[1]
		line, _ = strconv.Atoi(m[2])
	}
	if fileIDPattern.MatchString(s) {
		return s, "", line
	}
	return "", s, line
}

// sessionFiles is `gg session files [--json]` and `gg session files focus`.
func sessionFiles(dir string, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "focus" {
		return sessionFilesFocus(dir, args[1:], stdout, stderr)
	}
	fs := flag.NewFlagSet("session files", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the list as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 0 {
		fmt.Fprintln(stderr, filesUsage)
		return 2
	}
	r, code, ok := steerTUI(dir, steer.Command{Cmd: "files"}, false, stdout, stderr)
	if !ok {
		return code
	}
	if !r.OK {
		fmt.Fprintln(stderr, r.Error)
		return 1
	}
	if *asJSON {
		files := r.Files
		if files == nil {
			files = []steer.OpenFile{}
		}
		data, _ := json.MarshalIndent(files, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	for _, f := range r.Files {
		src := f.Source
		switch f.Source {
		case "commit":
			src = "commit " + shortSHA(f.Rev)
		case "shelf":
			src = "shelf " + f.Rev
		}
		line := "-"
		if f.Line > 0 {
			line = ":" + strconv.Itoa(f.Line)
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", f.ID, f.Path, src, line, f.State)
	}
	return 0
}

func sessionFilesFocus(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session files focus", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noWait := fs.Bool("no-wait", false, "post the command and exit without waiting for an answer")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, filesUsage)
		return 2
	}
	id, path, line := parseFileTarget(pos[0])
	if trailingLine.MatchString(pos[0]) && line < 1 {
		fmt.Fprintln(stderr, "session files focus: a line number is 1-based")
		return 2
	}
	c := steer.Command{Cmd: "file_focus", FileID: id, File: path}
	if line > 0 {
		c.Line = &steer.Line{No: line}
	}
	r, code, ok := steerTUI(dir, c, *noWait, stdout, stderr)
	if !ok {
		return code
	}
	return printSteerReply(r, stdout, stderr)
}
```

`shortSHA`: reuse an existing CLI helper if there is one (`grep -rn "func short" internal/cli`), else `s[:min(7,len(s))]`. `steerTUI` in `session.go`, factored from `sendSteer` (TUI half only, never the web POST; `stdout` gets the id on `--no-wait`); `sendSteer`'s reply tail becomes `printSteerReply`. Dispatch `case "files": return sessionFiles(dir, args[1:], stdout, stderr)`; usage line gains `files`.

Note `parseSteerFlags` must reject `--no-wait` on `files` (it is not defined there → flag error → exit 2, as the usage test expects).

- [ ] **Step 4:** `go test ./internal/cli/ -run 'Session'` — Expected: PASS.
- [ ] **Step 5:** commit `feat(cli): gg session files lists and focuses the TUI's open files`.

---

### Task 8: CLI — `--background` on `gg session navigate` and `gg open`

**Files:**
- Modify: `internal/cli/session.go` (`sessionNavigate`), `internal/cli/open.go` (`cmdOpen`, `openUsage`)
- Test: `internal/cli/session_files_test.go`, `internal/cli/link_content_test.go`

**Interfaces:**
- Consumes: `steerTUI`, `printSteerReply` (Task 7); `Command.Background` (Task 1).
- Produces: `func sendBackground(dir string, c steer.Command, noWait bool, stdout, stderr io.Writer) int` — sets `c.Background = true`, `steerTUI` + `printSteerReply`.

- [ ] **Step 1: failing tests**:

```go
// session_files_test.go
func TestSessionNavigateBackgroundPostsToTheTUI(t *testing.T) {
	t.Parallel()
	repo := newCLIRepo(t)
	dir := t.TempDir()
	livePresence(t, dir)
	seen := answer(t, dir, func(c steer.Command) steer.Reply {
		return steer.Reply{ID: c.ID, OK: true, Detail: "opened README.md in the background"}
	})
	link := "gg://" + filepath.ToSlash(repo) + "/README.md?view=content"
	var out, errb bytes.Buffer
	code := runSession(dir, domain.Open(repo), []string{"navigate", "--background", link}, &out, &errb)
	if code != 0 || !strings.Contains(out.String(), "opened README.md in the background") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, out.String(), errb.String())
	}
	if c := <-seen; !c.Background || c.File != "README.md" || c.HintKind != model.ContentHintKind {
		t.Errorf("posted %+v", c)
	}
}

func TestSessionNavigateBackgroundRefusals(t *testing.T) {
	t.Parallel()
	repo := newCLIRepo(t)
	svc := domain.Open(repo)
	diffLink := "gg://" + filepath.ToSlash(repo) + "/README.md"
	for _, args := range [][]string{
		{"navigate", "--background", diffLink},       // not a content link
		{"navigate", "--background", "--file", "README.md", "--new-line", "1"}, // flags, no link
	} {
		var out, errb bytes.Buffer
		if code := runSession(t.TempDir(), svc, args, &out, &errb); code != 2 {
			t.Errorf("%v: exit=%d stderr=%q, want 2", args, code, errb.String())
		}
	}
	content := "gg://" + filepath.ToSlash(repo) + "/README.md?view=content"
	var out, errb bytes.Buffer
	if code := runSession(t.TempDir(), svc, []string{"navigate", "--background", content}, &out, &errb); code != 1 ||
		!strings.Contains(errb.String(), "no gg TUI session for this worktree") {
		t.Fatalf("no TUI: exit=%d stderr=%q", code, errb.String())
	}
}

// link_content_test.go
func TestOpenBackgroundRefusals(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	content := "gg://" + filepath.ToSlash(dir) + "/README.md?view=content"
	for _, args := range [][]string{
		{"--background", "gg://" + filepath.ToSlash(dir) + "/README.md"}, // not content
		{"--background", "--web", content},
	} {
		var out, errb bytes.Buffer
		if code := cmdOpen(domain.Open(dir), args, &out, &errb); code != 2 {
			t.Errorf("%v: exit=%d stderr=%q, want 2", args, code, errb.String())
		}
	}
}
```

(Also: `gg open --background <content>` with no live TUI is exit 1 and never calls `LaunchTUI` — covered by the e2e row in Task 10 since `cmdOpen`'s inbox is the real state dir; if a test seam for `steerDirFor` exists, add the unit test too.)

- [ ] **Step 2:** `go test ./internal/cli/ -run 'Background'` — Expected: FAIL (`flag provided but not defined: -background`).
- [ ] **Step 3:** implement. `sessionNavigate`: `background := fs.Bool("background", false, "load a content link into the open-files list without showing it")`; in the link arm after `resolveLinkArg`: `if *background && res.Hint.Kind != model.ContentHintKind { stderr "session navigate: --background needs a content link (gg link --content <path>)"; return 2 }`, and at the end `if *background { return sendBackground(dir, c, *noWait, stdout, stderr) }`; outside the link arm, `if *background { "session navigate: --background needs a content link"; return 2 }` before any other flag check. `cmdOpen`: the same flag; `--background --web` → exit 2 `open: --background and --web do not mix (gg web keeps no open files yet)`; non-content → exit 2 as above; after `navigateCommandFor` + `c.Worktree = res.Checkout`, `if *background { return sendBackground(dir, c, *noWait, stdout, stderr) }` (before the launch branch — never launches, R13). `openUsage` gains `[--background]`.

- [ ] **Step 4:** `go test ./internal/cli/` — Expected: PASS.
- [ ] **Step 5:** commit `feat(cli): --background opens a content link into the open-files list`.

---

### Task 9: `gg web` refuses the new shapes

**Files:**
- Modify: `internal/web/steer.go` (`toSteerWire`)
- Test: `internal/web/steer_test.go` (or wherever `toSteerWire` is tested — `grep -ln toSteerWire internal/web/*_test.go`)

- [ ] **Step 1: failing test**:

```go
func TestToSteerWireRefusesOpenFilesVerbs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		c    steer.Command
		want string
	}{
		{steer.Command{Cmd: "files"}, "open files are not supported in gg web yet"},
		{steer.Command{Cmd: "file_focus", FileID: "f1"}, "open files are not supported in gg web yet"},
		{steer.Command{Cmd: "navigate", File: "a.txt", Background: true}, "background opens are not supported in gg web yet"},
	} {
		if _, err := toSteerWire(tc.c); err == nil || err.Error() != tc.want {
			t.Errorf("toSteerWire(%+v) = %v, want %q", tc.c, err, tc.want)
		}
	}
}
```

- [ ] **Step 2:** `go test ./internal/web/ -run ToSteerWireRefusesOpenFiles` — Expected: FAIL (`unknown command "files"`).
- [ ] **Step 3:** at the top of `toSteerWire`: `if c.Background { return w, errors.New("background opens are not supported in gg web yet") }`; in the switch, `case "files", "file_focus": return w, errors.New("open files are not supported in gg web yet")`.
- [ ] **Step 4:** `go test ./internal/web/` — Expected: PASS.
- [ ] **Step 5:** commit `feat(web): refuse the open-files verbs until the web stage`.

---

### Task 10: e2e — no live TUI, usage errors

**Files:**
- Modify: `e2e/scenarios/s89_session_cli.toml`

- [ ] **Step 1:** append (check the harness supports `stderr_contains`; the key name is in `e2e/*.go` — use it, else `stdout_excludes` + exit only):

```toml
# Open files (plan 4): with no live TUI every open-files verb says so.
[[run]]
cmd             = ["session", "files"]
exit            = 1
stderr_contains = ["no gg TUI session for this worktree"]

[[run]]
cmd             = ["session", "files", "--json"]
exit            = 1
stderr_contains = ["no gg TUI session for this worktree"]

[[run]]
cmd  = ["session", "files", "focus", "f1", "--no-wait"]
exit = 1

# Usage errors come first (exit 2).
[[run]]
cmd  = ["session", "files", "focus"]
exit = 2

[[run]]
cmd  = ["session", "navigate", "--background", "--file", "a.txt", "--new-line", "1"]
exit = 2
```

Plus a `gg open --background <content link>` row with exit 1 (build the link the way s92 builds one, e.g. via a captured `link --content a.txt` output if the harness can template it; if it cannot, skip and ledger it — Task 8's unit tests cover the refusal paths).

- [ ] **Step 2:** `./test.sh e2e` (or `go test ./e2e/ -run 's89'`) — Expected: PASS after Tasks 7–8 (these rows would fail with exit 2 `unknown subcommand` before them).
- [ ] **Step 3:** commit `test(e2e): open-files verbs without a live TUI`.

---

### Task 11: Skill, docs, memory

**Files:**
- Modify: `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (`Version` 95 → 96)
- Regenerate: `.claude/skills/using-gg/SKILL.md` via `go run ./cmd/gg init --update` from the worktree
- Modify: `CHANGELOG.md`, `README.md` (the *Open files* section, line ≈1166, + the CLI table if one lists `gg session` verbs), `docs/CLAUDE-details.md` → "Content links", `docs/superpowers/specs/2026-09-24-open-files-design.md` (mark stage 4 done, link this plan)
- Memory: `open-files-feature.md` (plan 4 status) + `MEMORY.md` line

- [ ] **Step 1:** using-gg.md — after the content-link paragraph (≈line 192) add:

```markdown
**Open files — let the user read along.** The TUI keeps up to 20 files open per
worktree (the ctrl+\ switcher lists them). An agent can use that list:

- `gg open <content-link> --background` (or `gg session navigate <link> --background`)
  loads the file WITHOUT touching the screen and answers
  `opened <path> in the background [at line N]`; a file already on screen is
  left alone (`<path> is already open on screen`). Needs a live TUI (exit 1
  otherwise; never launches one); exit 2 for a link that is not a content link.
  When a 21st file pushes one out, the answer ends `; closed <path> (20 files open)`.
- `gg session files [--json]` — the open files: `<id> <path> <source> <:line|-> <shown|background>`
  (`--json`: `id, path, source, rev, line, state`). Also in `gg_ui_state` as `open_files`.
- `gg session files focus <id|path>[:<line>]` — bring one to the front, optionally
  at a line; exit 1 `no open file <x>`. A foreground `gg open` of a working-tree
  file already reuses its open copy; `files focus` is how you reach commit/shelf
  versions the user opened.

Walking the user through several files: open A, B and C with `--background`
first (they load while you think), then `gg session files focus <id>:<line>`
each one in turn as you explain it.
```

Bump `Version` to 96; run `go test ./internal/agentskill/`; regenerate SKILL.md.

- [ ] **Step 2:** CHANGELOG entry (Unreleased, Added): the three verbs, `open_files`, web refusal. README *Open files*: an "Agents" paragraph (same content, user-facing). CLAUDE-details "Content links": the wire additions, R5/R6/R7/R9/R12 in one paragraph each line. Spec: stage 4 → done.
- [ ] **Step 3:** `./test.sh` then `./test.sh race` (sequential, nothing edited meanwhile) — Expected: both green; read the tails.
- [ ] **Step 4:** verify binary: `go build -o /tmp/claude-1000/…/scratchpad/gg-plan4 ./cmd/gg` from the worktree; tmux smoke: start the TUI in the worktree (`tmux new-session -d -s claude-plan4 …`), then `gg-plan4 open --background <content link>` ×2, `gg-plan4 session files`, `gg-plan4 session files focus f<id>:10`, capture the pane; kill only `claude-plan4`.
- [ ] **Step 5:** commit docs `docs: open files plan 4 — agent verbs`; update memory; then ASK the user before merging (merge `--no-ff` with the custom message, `./build.sh install`, remove the worktree).
