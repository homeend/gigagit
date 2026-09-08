# Review Notes Core Implementation Plan (phase 1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist per-repo review notes anchored to diff lines, resolve them against an open diff (active / moved / stale / orphaned), and render them as inline rows in the TUI diff view and the web diff — with the store, the config, the domain surface, the housekeeping sweep and the two frontends. No CLI, MCP, `--hunk N`, `gg review --notes` or agent skill: those are phase 2 and consume this phase's domain surface unchanged.

**Architecture:** A note is a plain `model.Note` record (engine-free) addressed by `model.FileAddress` + side + line range + a fingerprint of the anchored lines. `internal/notes` is a shelf/bookmark-style TOML store owned by `domain` (frontends never import it), with a cross-process file lock so a `c` keypress and the startup sweep cannot clobber each other. `domain` owns resolution: a pure `resolveNotes(notes, oldLines, newLines)` that re-anchors by fingerprint, plus `NotesFor`/`NoteCounts` queries and four mutation methods (domain methods like bookmarks, NOT engine ops). The TUI turns resolved notes into synthetic display rows in `diffView.relayout` — the cached `textdiff.Row`s are never touched — and refreshes them through a new `srcNotes` refresh source. The web mirrors the same rows over `/api/notes` with an SSE `notes` event.

**Tech Stack:** Go 1.26, `github.com/pelletier/go-toml/v2`, Bubble Tea + lipgloss, `internal/textdiff`, `internal/config`, `internal/i18n` (four bundles: `internal/i18n/lang/{ja,ko,zh,ru}.toml`), vanilla-ES-module web client.

**Spec:** `docs/superpowers/specs/2026-09-08-hunk-parity-roadmap.md` §4.4 (the binding design), §3.2 (motivation), §4 items 2 and 4 (cross-file hopping, sidecar rule), §4.1 (the cursor contract this anchors on).

**Worktree:** `/mnt/t/others/gigagit.worktrees/feat-notes-core` (branch `feat/notes-core`, off `main` at `58dc0a3b`). Every command below runs there. Commit trailers on every commit:

```
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
```

---

## Rulings

Decisions §4.4 left open, made here. Decided items in §4.4 are NOT changed; the two rows marked **(touches decided text)** extend a decided shape additively and are called out in the self-review.

1. **`NoteCounts` gains a third map — `ByCommitPath map[string]int` keyed `sha + ":" + path`. (touches decided text)** §4 item 2 requires `}`/`{` to step to the *next file that carries notes*; `ByPath`/`ByCommit` alone cannot answer "does file X of commit Y carry notes". `ByPath` and `ByCommit` keep their decided meaning; the third map is additive. (Alternative rejected: falling back to the plain adjacent-file step on commit diffs — a nav key that means two different things is worse.)
2. **Old-side base table. (touches decided text)** §4.4's "`FileStateModified` etc. → HEAD" names a state this codebase does not have, and the Files panel actually diffs **index → working tree** (`loadStatusDiffCmd`'s own comment). The binding table used everywhere in this plan (`domain.noteSideLines`):

   | `Address.State` | old side | new side |
   |---|---|---|
   | `StateUnstaged` | `ShowFile(ctx, "", path)` (index blob) | `WorktreeFile(ctx, path)` |
   | `StateStaged` | `ShowFile(ctx, "HEAD", path)` | `ShowFile(ctx, "", path)` (index blob) |
   | `StateUntracked` | absent | `WorktreeFile(ctx, path)` |
   | `StateCommitted` | `ShowFile(ctx, sha+"^", path)` | `ShowFile(ctx, sha, path)` |
   | `StateShelf` | absent | `ResolveBytes(ctx, addr.FileRef())` |
3. **Address matching is state-insensitive within the working tree.** A note is shown for an address when `Path` and `Commit` match (`sameNoteTarget`), so a note added on the Files-panel (unstaged) diff also shows on the Staged diff of the same file; the stored `State` only names the old-side base for the sweep. Re-anchoring by fingerprint absorbs the line-number difference.
4. **`NotesFor` takes the caller's `domain.Diff`; the TUI wraps its own rows.** The TUI does not retain the `domain.Diff`, so it passes `domain.Diff{Result: textdiff.Result{Rows: v.full}}` — a value wrapper around the shared cached rows, never a mutation of them.
5. **Notes reach the diff view through a `notesLoadedMsg` command**, fired from the `diffMsg` handler (`model.go:372`) and after every note mutation / `srcNotes` arrival — not inside the six `applyDiff` loaders.
6. **`dRow.note` points at a `*noteLine`, not a `*ResolvedNote`.** One resolved note renders as one to three display rows (summary, optional rationale, replies), so the pointer names the *display row*. The owning `ResolvedNote`'s id rides on `noteLine.id`/`rootID`.
7. **The agent-layer flag lives on the view (`diffView.hideAgent`), mirrored from `Model.notesAgentOff`** — `relayout(width)` has no `Model`, and every rebuild path (`f`, `ctrl+w`, resize) calls it. The loaders copy it in exactly like `partial: m.diffPartial`.
8. **The sweep is started explicitly, never from `New()`.** `SetNotesPolicy(maxAgeDays, maxEntries)` + `StartNotesSweep()` are called where the frontends already push config-derived policy (`internal/tui/load.go:90`, `internal/tui/source.go:440`, `internal/web/settings.go` `applyUIPolicies`). `New()` is used by dozens of tests, which must not spawn goroutines against the user's real state dir. The `sync.Once` is per-`Service` (a re-root builds a fresh Service for a *different* repo, which deserves its own sweep).
9. **Anchor row for a range = the line carrying `Range[1]`** (the range end) on the note's side, clamped into the side's line count when stale. Phase-1 ranges are single lines; phase-2 hunk ranges then read correctly.
10. **`NoteCounts` is unresolved** — it counts store records (root notes only, so `◆2` means two threads), never a resolution pass. A badge may therefore outlive an orphaned note until the next sweep; that is the cheap-read trade §4.4 asks for ("the Files and Commits row painters read it and never touch the store").
11. **Note ids are 8 hex chars from `crypto/rand`**, retried against the loaded set on collision.
12. **The lock**: `notes.toml.lock`, `O_CREATE|O_EXCL`, polled every 20 ms for up to 2 s; a lock file whose mtime is older than 30 s is removed and re-taken.
13. **Web has no line cursor**: clicking a diff row marks it `tr.cur`, and `c` anchors there. `E`/`R` act on the nearest note at or above the marked row; `}`/`{` scroll between note rows.
14. **`srcNotes` is never polled.** It is not added to `refresh.go`'s `scheduledItems` (like `srcIdentity`), has no `[refresh]` key, and rides only on explicit refreshes and note mutations.
15. **Empty summary cancels** the note popup (spec), and a whitespace-only summary counts as empty.

---

## Global Constraints

- **XDG first, everywhere.** `notesBaseDir()` resolves `$XDG_STATE_HOME` → `%LocalAppData%` (Windows) → `~/.local/state`, leaf `gg/notes`; an explicitly-set `$XDG_STATE_HOME` wins on every platform (the only way tests isolate state on Windows). Mirror `bookmarkBaseDir()` exactly.
- **Reads never rewrite.** `Load` and every query path are read-only; pruning happens only in the startup sweep, and the entry cap only on a write.
- **The cap is enforced on write.** Every mutation re-reads under the lock, applies, drops oldest-`Created` roots first (a dropped root takes its replies), then rewrites.
- **`<= 0` means forever / uncapped.** `notes.max_age_days <= 0` keeps notes forever; `notes.max_entries <= 0` is uncapped. `-1` is the only value that can overlay "forever" onto the default (zero-is-unset overlay), exactly like `versions.max_age_days`.
- **Frontends never import `internal/notes`.** It is owned by `domain`, like `shelf`/`bookmark`/`prefix`; `internal/tui`, `internal/cli`, `internal/mcp`, `internal/web` never import `internal/git` either (guarded by `internal/archtest`).
- **Cached diff rows are shared and read-only.** Note markers live in sidecar slices/pointers on `dRow`, never as fields on `textdiff.Row`; `textdiff` stays pure.
- **Every user-visible TUI string goes through `i18n.T`** with a literal key present in ALL FOUR bundles (`ja`, `ko`, `zh`, `ru`); the gate tests in `internal/tui` (`i18n_scan_test.go`, `options_vocab_test.go`, `menu_labels_test.go`, `engine_prose_test.go`) must stay green after every task. Engine/CLI prose stays English.
- **New tests call `t.Parallel()`** unless they touch global state (the `notes.Now` clock seam, `domain.NotesStatePath`, the lipgloss colour profile) — those stay serial.
- **Tests use a real `git` in `t.TempDir()`** (`newRepo`/`newRepoDir`) or `gitexec.FakeRunner` for argv assertions (`bmSvc`-style for domain).
- **Windows: git paths use `/`.** `Note.Address.Path` is always in git slash form; never string-compare it against `filepath` output.
- **Rendering is byte-identical with no notes.** A view with an empty `notes` slice must produce exactly today's `diffPaneLines` output (`TestRenderDiffViewPanes` stays green).
- Run `gofmt -l internal/ && go vet ./internal/...` before each commit; `./test.sh unit` before the final task's commit and `./test.sh race` before the merge request.

---

### Task 1: `model.Note`, the fingerprint helper, and the three enums

**Files:**
- Create: `internal/model/note.go`
- Create: `internal/model/note_test.go`

**Interfaces:**
- Produces:
  - `type NoteSource string` with `NoteSourceUser NoteSource = "user"`, `NoteSourceAgent NoteSource = "agent"`
  - `type NoteSide string` with `NoteSideOld NoteSide = "old"`, `NoteSideNew NoteSide = "new"`
  - `type NoteStatus string` with `NoteActive NoteStatus = "active"`, `NoteStale NoteStatus = "stale"`, `NoteOrphaned NoteStatus = "orphaned"`
  - `type Note struct{ ID, ParentID string; Source NoteSource; Author string; Address FileAddress; Side NoteSide; Range [2]int; ContextHash, Summary, Rationale string; Tags []string; Confidence float64; Created, Updated time.Time }`
  - `func NoteContextHash(lines []string) string`
  - `func (n Note) IsReply() bool`
- Consumes: `model.FileAddress` (`internal/model/model.go:418`).

- [ ] **Step 1: Write the failing tests**

`internal/model/note_test.go`:

```go
package model

import "testing"

func TestNoteContextHashTrimsAndJoins(t *testing.T) {
	t.Parallel()
	a := NoteContextHash([]string{"  foo(bar)  ", "\tbaz"})
	b := NoteContextHash([]string{"foo(bar)", "baz"})
	if a != b {
		t.Fatalf("leading/trailing whitespace must not change the hash: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("hash = %q, want 64 hex chars (sha256)", a)
	}
	if NoteContextHash([]string{"foo", "bar"}) == NoteContextHash([]string{"bar", "foo"}) {
		t.Fatal("line order must matter")
	}
	if NoteContextHash(nil) != NoteContextHash([]string{}) {
		t.Fatal("nil and empty must hash alike")
	}
	// A single line is the phase-1 shape and must not carry a trailing newline
	// into the digest — phase 2's multi-line hunk anchors reuse this exact rule.
	if NoteContextHash([]string{"x"}) == NoteContextHash([]string{"x", ""}) {
		t.Fatal("a trailing empty line must change the hash")
	}
}

func TestNoteIsReply(t *testing.T) {
	t.Parallel()
	if (Note{}).IsReply() {
		t.Fatal("a root note has no ParentID")
	}
	if !(Note{ParentID: "abc"}).IsReply() {
		t.Fatal("a note with a ParentID is a reply")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/model/ -run TestNote -count=1`
Expected: build failure — `undefined: NoteContextHash`, `undefined: Note`.

- [ ] **Step 3: Add the model**

`internal/model/note.go`:

```go
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// NoteSource is who wrote a note. The TUI's `a` key hides the agent layer;
// user notes are always visible (hunk's policy).
type NoteSource string

const (
	NoteSourceUser  NoteSource = "user"
	NoteSourceAgent NoteSource = "agent"
)

// NoteSide is the diff side a note's line range indexes.
type NoteSide string

const (
	NoteSideOld NoteSide = "old"
	NoteSideNew NoteSide = "new"
)

// NoteStatus is a note's resolution against the diff it is drawn over. It is
// COMPUTED at open/refresh time and never stored: active (found where it was,
// or found again elsewhere), stale (the anchored text is gone but the file is
// not), orphaned (the file/commit itself is gone).
type NoteStatus string

const (
	NoteActive   NoteStatus = "active"
	NoteStale    NoteStatus = "stale"
	NoteOrphaned NoteStatus = "orphaned"
)

// Note is one review note: machine-local working material anchored to a range
// of lines on one side of one file, at one address. It is engine-free (plain
// data, like Bookmark) and persisted by internal/notes as TOML.
//
// A reply (ParentID != "") inherits its parent's Address, Side, Range and
// ContextHash at creation time and is re-anchored with the parent.
type Note struct {
	ID          string      `toml:"id"`
	ParentID    string      `toml:"parent_id,omitempty"`
	Source      NoteSource  `toml:"source"`
	Author      string      `toml:"author,omitempty"`
	Address     FileAddress `toml:"address"`
	Side        NoteSide    `toml:"side"`
	Range       [2]int      `toml:"range"` // 1-based, inclusive
	ContextHash string      `toml:"context_hash"`
	Summary     string      `toml:"summary"`
	Rationale   string      `toml:"rationale,omitempty"`
	Tags        []string    `toml:"tags,omitempty"`
	Confidence  float64     `toml:"confidence,omitempty"`
	Created     time.Time   `toml:"created"`
	Updated     time.Time   `toml:"updated"`
}

// IsReply reports whether n hangs off another note.
func (n Note) IsReply() bool { return n.ParentID != "" }

// NoteContextHash fingerprints the anchored lines: each line trimmed of
// leading/trailing whitespace, joined with "\n" (no trailing newline), hex
// sha256. Trimming makes re-indentation a non-event; the join makes phase-2
// multi-line hunk anchors reuse this function unchanged.
func NoteContextHash(lines []string) string {
	trimmed := make([]string, len(lines))
	for i, l := range lines {
		trimmed[i] = strings.TrimSpace(l)
	}
	sum := sha256.Sum256([]byte(strings.Join(trimmed, "\n")))
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/model/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/model/ && go vet ./internal/model/
git add internal/model/note.go internal/model/note_test.go
git commit -m "feat(notes): model.Note record, side/source/status types, context fingerprint

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 2: `[notes]` config section

**Files:**
- Modify: `internal/config/config.go` (struct, `Config`, `Defaults`, `Load`, `overlayNotes`)
- Modify: `internal/config/template.go` (two `settingDoc` rows, the section loop)
- Modify: `internal/config/config_test.go` (add `TestNotesLayers`)

**Interfaces:**
- Produces:
  - `type NotesConfig struct{ MaxAgeDays int \`toml:"max_age_days"\`; MaxEntries int \`toml:"max_entries"\` }`
  - field `Config.Notes NotesConfig` (`toml:"notes"`)
  - `Defaults().Notes == NotesConfig{MaxAgeDays: 30, MaxEntries: 2000}`
  - `func overlayNotes(dst *NotesConfig, src NotesConfig)` — any nonzero value overlays (so `-1` = forever/uncapped can be set from a layer).
- Consumes: the existing `VersionsConfig` overlay precedent (`config.go:395`), `settingDocs` (`template.go:25`).

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestNotesLayers(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.toml")

	cfg, err := Load(missing, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Notes.MaxAgeDays != 30 || cfg.Notes.MaxEntries != 2000 {
		t.Errorf("defaults = %+v, want {30 2000}", cfg.Notes)
	}

	g := filepath.Join(dir, "global.toml")
	writeFile(t, g, "[notes]\nmax_age_days = 7\nmax_entries = 50\n")
	cfg, err = Load(g, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Notes.MaxAgeDays != 7 || cfg.Notes.MaxEntries != 50 {
		t.Errorf("global layer = %+v, want {7 50}", cfg.Notes)
	}

	r := filepath.Join(dir, "repo.toml")
	writeFile(t, r, "[notes]\nmax_age_days = -1\n")
	cfg, err = Load(g, r)
	if err != nil {
		t.Fatal(err)
	}
	// -1 is the ONLY way a layer can say "keep forever" under the
	// zero-is-unset overlay (the versions.max_age_days precedent).
	if cfg.Notes.MaxAgeDays != -1 {
		t.Errorf("repo -1 must win over global 7, got %d", cfg.Notes.MaxAgeDays)
	}
	if cfg.Notes.MaxEntries != 50 {
		t.Errorf("an unset repo key must not clear the global one, got %d", cfg.Notes.MaxEntries)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/ -run 'TestNotesLayers|TestSettingDocsCoverAllFields' -count=1`
Expected: build failure — `cfg.Notes undefined`.

- [ ] **Step 3: Add the section**

`internal/config/config.go`, after `VersionsConfig` (line ~160):

```go
// NotesConfig configures review-note housekeeping. TOML keys snake_case under
// [notes]. Both use nonzero-is-set so -1 (keep forever / uncapped) can overlay
// the default; 0 means unset (→ the default).
type NotesConfig struct {
	MaxAgeDays int `toml:"max_age_days"` // drop notes older than this in the startup sweep; <=0 = keep forever
	MaxEntries int `toml:"max_entries"`  // cap enforced on every write, oldest root first; <=0 = uncapped
}
```

In `Config`, after `Versions`:

```go
	Notes    NotesConfig    `toml:"notes"`
```

In `Defaults()`, after the `Versions:` line:

```go
		Notes: NotesConfig{MaxAgeDays: 30, MaxEntries: 2000},
```

In `Load`, after `overlayVersions(...)`:

```go
			overlayNotes(&cfg.Notes, layer.Notes)
```

After `overlayVersions` (line ~397):

```go
// overlayNotes copies each set field of src onto dst. Any nonzero value
// (including -1 = forever/uncapped) overlays; 0 is "unset".
func overlayNotes(dst *NotesConfig, src NotesConfig) {
	if src.MaxAgeDays != 0 {
		dst.MaxAgeDays = src.MaxAgeDays
	}
	if src.MaxEntries != 0 {
		dst.MaxEntries = src.MaxEntries
	}
}
```

`internal/config/template.go` — add after the two `versions` rows:

```go
	{"notes", "max_age_days", 30, "prune review notes older than this many days (the sweep at every gg start also drops notes whose anchor is gone); -1 = keep forever"},
	{"notes", "max_entries", 2000, "cap on stored review notes, enforced on every write (oldest thread dropped first); -1 = uncapped"},
```

and add the section to the render loop (`template.go:101`) — order matters, `notes` sits between `versions` and `tools`:

```go
	for _, section := range []string{"worktree", "ui", "debug", "refresh", "versions", "notes", "tools"} {
```

`TestSettingDocsCoverAllFields` (`template_test.go`) reflects over the config structs by section; add the new check line next to the `versions` one:

```go
	check("notes", reflect.TypeOf(NotesConfig{}))
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/config/ -count=1`
Expected: PASS (including `TestSettingDocsCoverAllFields` and the template golden, if any — re-run with `-run TestTemplate` and update no goldens: the template is generated, not stored).

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/config/ && go vet ./internal/config/
git add internal/config/
git commit -m "feat(notes): [notes] max_age_days / max_entries config section

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 3: `internal/notes` — the locked TOML store

**Files:**
- Create: `internal/notes/store.go`
- Create: `internal/notes/file_store.go`
- Create: `internal/notes/file_store_test.go`

**Interfaces:**
- Produces:
  - `var Now = time.Now` (clock seam, mirrors `snapshotNow`)
  - `var ErrNotFound = errors.New("notes: not found")`
  - `type Policy struct{ MaxEntries int }`
  - `type Store interface { Load() ([]model.Note, error); Put(model.Note) error; Remove(id string) error; Sweep(keep func(model.Note) bool) (int, error); SetPolicy(Policy) }`
  - `func NewFileStore(root string) *FileStore`
  - `func (fs *FileStore) SetPolicy(p Policy)`
  - `func NewID(existing []model.Note) string` — 8 hex chars, collision-checked
- Consumes: `model.Note`, `github.com/pelletier/go-toml/v2`.

- [ ] **Step 1: Write the failing tests**

`internal/notes/file_store_test.go`:

```go
package notes

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

// noteAt builds a root note created at t0+d seconds.
func noteAt(id string, sec int) model.Note {
	return model.Note{
		ID: id, Source: model.NoteSourceUser, Side: model.NoteSideNew,
		Range: [2]int{sec + 1, sec + 1}, Summary: "s" + id,
		Address: model.FileAddress{State: model.StateUnstaged, Path: "a/b.go"},
		Created: time.Unix(int64(1_700_000_000+sec), 0).UTC(),
	}
}

func TestPutLoadRoundTrip(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	n := noteAt("aaaaaaaa", 1)
	n.Rationale = "because"
	n.Tags = []string{"perf", "api"}
	n.Confidence = 0.5
	if err := fs.Put(n); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := fs.Load()
	if err != nil || len(got) != 1 {
		t.Fatalf("Load = %v (%d), err %v", got, len(got), err)
	}
	if got[0].Summary != "saaaaaaaa" || got[0].Rationale != "because" ||
		got[0].Tags[1] != "api" || got[0].Confidence != 0.5 ||
		got[0].Address.Path != "a/b.go" || got[0].Side != model.NoteSideNew ||
		got[0].Range != [2]int{2, 2} || !got[0].Created.Equal(n.Created) {
		t.Fatalf("round trip lost data: %+v", got[0])
	}
	// Put by an existing ID REPLACES.
	n.Summary = "edited"
	if err := fs.Put(n); err != nil {
		t.Fatal(err)
	}
	got, _ = fs.Load()
	if len(got) != 1 || got[0].Summary != "edited" {
		t.Fatalf("Put must replace by ID, got %+v", got)
	}
}

func TestRemoveRootTakesReplies(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	root := noteAt("rrrrrrrr", 1)
	reply := noteAt("pppppppp", 2)
	reply.ParentID = root.ID
	other := noteAt("oooooooo", 3)
	for _, n := range []model.Note{root, reply, other} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := fs.Remove(root.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got, _ := fs.Load()
	if len(got) != 1 || got[0].ID != other.ID {
		t.Fatalf("removing a root must take its replies, left %+v", got)
	}
	if err := fs.Remove("nosuch"); err != ErrNotFound {
		t.Fatalf("Remove(unknown) = %v, want ErrNotFound", err)
	}
}

func TestCapDropsOldestRootsWithTheirReplies(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	fs.SetPolicy(Policy{MaxEntries: 3})
	oldRoot := noteAt("old00000", 1)
	oldReply := noteAt("oldreply", 2)
	oldReply.ParentID = oldRoot.ID
	for _, n := range []model.Note{oldRoot, oldReply, noteAt("mid00000", 5)} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	// The 4th record trips the cap: the OLDEST ROOT goes, and its reply with it.
	if err := fs.Put(noteAt("new00000", 9)); err != nil {
		t.Fatal(err)
	}
	got, _ := fs.Load()
	if len(got) != 2 {
		t.Fatalf("cap 3 with a 2-record oldest thread must leave 2, got %d: %+v", len(got), got)
	}
	for _, n := range got {
		if n.ID == oldRoot.ID || n.ID == oldReply.ID {
			t.Fatalf("the oldest thread must be dropped whole, got %+v", got)
		}
	}
}

func TestCapUncappedWhenNonPositive(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	fs.SetPolicy(Policy{MaxEntries: -1})
	for i := 0; i < 12; i++ {
		if err := fs.Put(noteAt(string(rune('a'+i))+"0000000", i)); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := fs.Load()
	if len(got) != 12 {
		t.Fatalf("MaxEntries <= 0 must be uncapped, got %d", len(got))
	}
}

func TestSweepKeepsWhatThePredicateKeeps(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	root := noteAt("rrrrrrrr", 1)
	reply := noteAt("pppppppp", 2)
	reply.ParentID = root.ID
	keep := noteAt("kkkkkkkk", 3)
	for _, n := range []model.Note{root, reply, keep} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	dropped, err := fs.Sweep(func(n model.Note) bool { return n.ID != root.ID })
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	// The root is dropped by the predicate; its reply goes with it => 2.
	if dropped != 2 {
		t.Fatalf("dropped = %d, want 2 (root + orphaned reply)", dropped)
	}
	got, _ := fs.Load()
	if len(got) != 1 || got[0].ID != keep.ID {
		t.Fatalf("Sweep left %+v", got)
	}
}

func TestLoadNeverWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := NewFileStore(dir)
	if got, err := fs.Load(); err != nil || len(got) != 0 {
		t.Fatalf("Load on an empty dir = %v, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.toml")); !os.IsNotExist(err) {
		t.Fatal("Load must not create the file")
	}
}

func TestStaleLockIsTakenOver(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fs := NewFileStore(dir)
	lock := filepath.Join(dir, "notes.toml.lock")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * lockStale)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := fs.Put(noteAt("aaaaaaaa", 1)); err != nil {
		t.Fatalf("a stale lock must be broken, got %v", err)
	}
	if time.Since(start) > lockWait {
		t.Fatal("a stale lock must be broken without waiting out the full retry budget")
	}
	if got, _ := fs.Load(); len(got) != 1 {
		t.Fatalf("write under a broken stale lock lost the note: %+v", got)
	}
}

func TestConcurrentPutsSerialize(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			n := noteAt(string(rune('a'+i))+"0000000", i)
			if err := fs.Put(n); err != nil {
				t.Errorf("Put %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	got, _ := fs.Load()
	if len(got) != 8 {
		t.Fatalf("concurrent writers lost records: %d of 8", len(got))
	}
}

func TestNewIDAvoidsCollisions(t *testing.T) {
	t.Parallel()
	taken := []model.Note{}
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := NewID(taken)
		if len(id) != 8 {
			t.Fatalf("id %q is not 8 hex chars", id)
		}
		if seen[id] {
			t.Fatalf("NewID returned a taken id %q", id)
		}
		seen[id] = true
		taken = append(taken, model.Note{ID: id})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/notes/ -count=1`
Expected: build failure — the package does not exist yet.

- [ ] **Step 3: Write the store**

`internal/notes/store.go`:

```go
// Package notes is gigagit's machine-local store of review notes: short,
// expiring records anchored to a range of lines on one side of one file at
// one address. It is owned by internal/domain — frontends never import it,
// exactly like shelf/bookmark/prefix.
//
// The store is deliberately narrow: Load never writes, every mutation
// re-reads under a cross-process lock, applies, enforces the entry cap and
// rewrites atomically. Housekeeping (expiry, orphan pruning) is the caller's
// policy, expressed through Sweep's predicate.
package notes

import (
	"errors"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

// ErrNotFound is returned by Remove for an unknown id.
var ErrNotFound = errors.New("notes: not found")

// Now is the clock seam (the snapshotNow pattern): tests override it to make
// expiry deterministic. Package-level, so tests that set it run SERIALLY.
var Now = time.Now

// Policy is the write-time budget. MaxEntries <= 0 means uncapped.
type Policy struct{ MaxEntries int }

// Store persists note records. Load is read-only; every other method
// serialises against other processes and other goroutines.
type Store interface {
	Load() ([]model.Note, error)
	Put(n model.Note) error // add, or replace by ID
	Remove(id string) error // a root takes its replies
	Sweep(keep func(model.Note) bool) (dropped int, err error)
	SetPolicy(p Policy) // the write-time budget lives ON the store (§4.4)
}
```

`internal/notes/file_store.go`:

```go
package notes

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/model"
)

const (
	lockWait  = 2 * time.Second        // total retry budget for the cross-process lock
	lockPoll  = 20 * time.Millisecond  // retry interval
	lockStale = 30 * time.Second       // a lock older than this is a crashed writer's
)

// FileStore keeps notes.toml under root, rewritten atomically (temp+rename,
// the bookmark pattern) under two locks: a process-local mutex (the sweep
// goroutine vs a `c` keypress in the same process) and a notes.toml.lock file
// (this gg vs another gg, or a phase-2 `gg note add`).
type FileStore struct {
	root string

	mu  sync.Mutex // process-local: guards every mutation AND pol
	pol Policy
}

// NewFileStore roots a store at the per-repo directory (caller-supplied).
func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

// SetPolicy sets the write-time entry cap. Safe to call at any time.
func (fs *FileStore) SetPolicy(p Policy) {
	fs.mu.Lock()
	fs.pol = p
	fs.mu.Unlock()
}

type index struct {
	Notes []model.Note `toml:"notes"`
}

func (fs *FileStore) path() string     { return filepath.Join(fs.root, "notes.toml") }
func (fs *FileStore) lockPath() string { return fs.path() + ".lock" }

// read parses the file. A missing or corrupt file reads as empty — a note
// store is working material, never worth failing a whole session over.
func (fs *FileStore) read() []model.Note {
	data, err := os.ReadFile(fs.path())
	if err != nil {
		return nil
	}
	var idx index
	if err := toml.Unmarshal(data, &idx); err != nil {
		return nil
	}
	return idx.Notes
}

// Load returns every stored note. NEVER writes (not even to prune): the
// startup sweep is the only pruner, so a read-heavy session cannot rewrite
// the file under a concurrent writer.
func (fs *FileStore) Load() ([]model.Note, error) { return fs.read(), nil }

// lock takes the cross-process lock, breaking one that is older than
// lockStale (a crashed writer). Returns a release func.
func (fs *FileStore) lock() (func(), error) {
	if err := os.MkdirAll(fs.root, 0o755); err != nil {
		return nil, err
	}
	// The retry budget uses the REAL clock, never the Now seam: a test that
	// freezes Now for expiry must not spin here forever on a held lock.
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(fs.lockPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return func() { os.Remove(fs.lockPath()) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if fi, statErr := os.Stat(fs.lockPath()); statErr == nil && time.Since(fi.ModTime()) > lockStale {
			os.Remove(fs.lockPath()) // stale: the writer died holding it
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("notes: notes.toml.lock is held; try again")
		}
		time.Sleep(lockPoll)
	}
}

// write persists ns via temp-file + rename (the bookmark/seq-state pattern).
func (fs *FileStore) write(ns []model.Note) error {
	data, err := toml.Marshal(index{Notes: ns})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "notes-*.toml")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, fs.path()); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// mutate is the ONE write path: process mutex → file lock → fresh read →
// apply → drop orphaned replies → cap → atomic rewrite. Re-reading under the
// lock is what makes the startup sweep and a concurrent `gg note add` safe.
func (fs *FileStore) mutate(apply func([]model.Note) ([]model.Note, error)) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	unlock, err := fs.lock()
	if err != nil {
		return err
	}
	defer unlock()
	ns, err := apply(fs.read())
	if err != nil {
		return err
	}
	ns = dropOrphanReplies(ns)
	ns = capOldestFirst(ns, fs.pol.MaxEntries)
	return fs.write(ns)
}

// Put adds n, or replaces the record with the same ID.
func (fs *FileStore) Put(n model.Note) error {
	return fs.mutate(func(ns []model.Note) ([]model.Note, error) {
		for i := range ns {
			if ns[i].ID == n.ID {
				ns[i] = n
				return ns, nil
			}
		}
		return append(ns, n), nil
	})
}

// Remove deletes one note; removing a root removes its replies too.
func (fs *FileStore) Remove(id string) error {
	return fs.mutate(func(ns []model.Note) ([]model.Note, error) {
		kept := ns[:0]
		found := false
		for _, n := range ns {
			if n.ID == id || n.ParentID == id {
				found = found || n.ID == id
				continue
			}
			kept = append(kept, n)
		}
		if !found {
			return nil, ErrNotFound
		}
		return kept, nil
	})
}

// Sweep keeps every note the predicate accepts and reports how many records
// went (including replies orphaned by a dropped root).
func (fs *FileStore) Sweep(keep func(model.Note) bool) (int, error) {
	dropped := 0
	err := fs.mutate(func(ns []model.Note) ([]model.Note, error) {
		before := len(ns)
		kept := ns[:0]
		for _, n := range ns {
			if keep(n) {
				kept = append(kept, n)
			}
		}
		dropped = before - len(dropOrphanReplies(kept))
		return kept, nil
	})
	if err != nil {
		return 0, err
	}
	return dropped, nil
}

// dropOrphanReplies removes replies whose root is gone (dropped by a Remove,
// a Sweep predicate, or the cap).
func dropOrphanReplies(ns []model.Note) []model.Note {
	roots := make(map[string]bool, len(ns))
	for _, n := range ns {
		if !n.IsReply() {
			roots[n.ID] = true
		}
	}
	kept := make([]model.Note, 0, len(ns))
	for _, n := range ns {
		if n.IsReply() && !roots[n.ParentID] {
			continue
		}
		kept = append(kept, n)
	}
	return kept
}

// capOldestFirst enforces max records by dropping whole threads, oldest root
// (by Created) first. max <= 0 is uncapped.
func capOldestFirst(ns []model.Note, max int) []model.Note {
	if max <= 0 || len(ns) <= max {
		return ns
	}
	roots := make([]model.Note, 0, len(ns))
	for _, n := range ns {
		if !n.IsReply() {
			roots = append(roots, n)
		}
	}
	sort.SliceStable(roots, func(a, b int) bool { return roots[a].Created.Before(roots[b].Created) })
	doomed := map[string]bool{}
	size := len(ns)
	for _, r := range roots {
		if size <= max {
			break
		}
		doomed[r.ID] = true
		size-- // the root
		for _, n := range ns {
			if n.ParentID == r.ID {
				size--
			}
		}
	}
	kept := make([]model.Note, 0, len(ns))
	for _, n := range ns {
		if doomed[n.ID] || doomed[n.ParentID] {
			continue
		}
		kept = append(kept, n)
	}
	return kept
}

// NewID mints an 8-hex-char id not present in existing.
func NewID(existing []model.Note) string {
	taken := make(map[string]bool, len(existing))
	for _, n := range existing {
		taken[n.ID] = true
	}
	var b [4]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			// crypto/rand cannot fail in practice; fall back to the clock so a
			// note is still storable rather than lost.
			return hex.EncodeToString([]byte{
				byte(Now().UnixNano()), byte(Now().UnixNano() >> 8),
				byte(Now().UnixNano() >> 16), byte(Now().UnixNano() >> 24)})
		}
		id := hex.EncodeToString(b[:])
		if !taken[id] {
			return id
		}
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/notes/ -count=1`
Expected: PASS (9 tests).

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/notes/ && go vet ./internal/notes/
git add internal/notes/
git commit -m "feat(notes): locked TOML note store — put/remove/sweep, oldest-first cap

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 4: domain — store wiring, the four mutations, resolution, `NotesFor`, `NoteCounts`

**Files:**
- Create: `internal/domain/notesstore.go`
- Create: `internal/domain/notes.go`
- Create: `internal/domain/notes_test.go`
- Modify: `internal/domain/service.go` (five fields on `Service`, the `notes` import)

**Interfaces:**
- Produces:
  - `var NotesStatePath string` — test/override root (`BookmarkStatePath` twin)
  - `func (s *Service) SetNotesStore(st notes.Store)`
  - `func (s *Service) notesStore(ctx context.Context) notes.Store`
  - `func notesBaseDir() string`
  - `var ErrNotesDisabled = errors.New("notes: no state directory available")`
  - `type ResolvedNote struct{ Note model.Note; Status model.NoteStatus; Range [2]int; Replies []ResolvedNote }`
  - `type NoteCounts struct{ ByPath, ByCommit, ByCommitPath map[string]int }`
  - `func (s *Service) NoteAdd(ctx context.Context, n model.Note) (model.Note, error)`
  - `func (s *Service) NoteEdit(ctx context.Context, id, summary, rationale string) error`
  - `func (s *Service) NoteReply(ctx context.Context, parentID string, n model.Note) (model.Note, error)`
  - `func (s *Service) NoteRemove(ctx context.Context, id string) error`
  - `func (s *Service) NotesFor(ctx context.Context, addr model.FileAddress, d Diff) ([]ResolvedNote, error)`
  - `func (s *Service) NoteCounts(ctx context.Context) (NoteCounts, error)`
  - pure: `func resolveNotes(ns []model.Note, oldLines, newLines []string) []ResolvedNote`, `func resolveOne(n model.Note, lines []string) (model.NoteStatus, [2]int)`, `func diffSideLines(d Diff) (old, new []string)`, `func sameNoteTarget(a, b model.FileAddress) bool`
- Consumes: `notes.Store`/`notes.NewFileStore`/`notes.NewID`/`notes.Now`/`notes.Policy`, `model.Note*`, `repoKey` (`shelfstore.go:79`), `Service.GitCommonDir`, `Diff` (`differ.go:44`), `textdiff.Row`.

- [ ] **Step 1: Write the failing tests**

`internal/domain/notes_test.go`:

```go
package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
	"github.com/homeend/gigagit/internal/textdiff"
)

func notesSvc(t *testing.T) (*Service, *gitexec.FakeRunner) {
	t.Helper()
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	return svc, f
}

// wtAddr is the working-tree address every test note hangs off.
func wtAddr(path string) model.FileAddress {
	return model.FileAddress{State: model.StateUnstaged, Worktree: "/wt", Path: path}
}

// sideDiff builds a Diff whose NEW side is the given lines (numbered from 1)
// and whose OLD side is the same lines — enough for resolution tests.
func sideDiff(lines ...string) Diff {
	rows := make([]textdiff.Row, len(lines))
	for i, l := range lines {
		rows[i] = textdiff.Row{Kind: textdiff.Same, Left: l, Right: l, LeftNo: i + 1, RightNo: i + 1}
	}
	return Diff{Result: textdiff.Result{Rows: rows}}
}

func TestNoteAddFillsIDTimesAndSource(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	got, err := svc.NoteAdd(context.Background(), model.Note{
		Address: wtAddr("a/b.go"), Side: model.NoteSideNew, Range: [2]int{2, 2},
		Summary: "off by one", ContextHash: model.NoteContextHash([]string{"b"}),
	})
	if err != nil {
		t.Fatalf("NoteAdd: %v", err)
	}
	if len(got.ID) != 8 || got.Created.IsZero() || got.Updated.IsZero() {
		t.Fatalf("NoteAdd must fill ID/Created/Updated: %+v", got)
	}
	if got.Source != model.NoteSourceUser {
		t.Fatalf("default Source = %q, want user", got.Source)
	}
}

func TestNoteEditReplyRemoveThread(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	ctx := context.Background()
	root, err := svc.NoteAdd(ctx, model.Note{
		Address: wtAddr("a/b.go"), Side: model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "first", ContextHash: model.NoteContextHash([]string{"a"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.NoteEdit(ctx, root.ID, "edited", "why"); err != nil {
		t.Fatalf("NoteEdit: %v", err)
	}
	rep, err := svc.NoteReply(ctx, root.ID, model.Note{Summary: "agreed", Source: model.NoteSourceAgent, Author: "bot"})
	if err != nil {
		t.Fatalf("NoteReply: %v", err)
	}
	if rep.ParentID != root.ID || rep.Side != root.Side || rep.Range != root.Range ||
		rep.ContextHash != root.ContextHash || rep.Address.Path != root.Address.Path {
		t.Fatalf("a reply must inherit the parent's anchor: %+v", rep)
	}
	res, err := svc.NotesFor(ctx, wtAddr("a/b.go"), sideDiff("a", "b"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Note.Summary != "edited" || res[0].Note.Rationale != "why" ||
		len(res[0].Replies) != 1 || res[0].Replies[0].Note.Summary != "agreed" {
		t.Fatalf("threading/edit lost: %+v", res)
	}
	if err := svc.NoteRemove(ctx, root.ID); err != nil {
		t.Fatal(err)
	}
	res, _ = svc.NotesFor(ctx, wtAddr("a/b.go"), sideDiff("a", "b"))
	if len(res) != 0 {
		t.Fatalf("removing a root must take its replies, left %+v", res)
	}
}

func TestResolveOneFourOutcomes(t *testing.T) {
	t.Parallel()
	lines := []string{"alpha", "beta", "gamma", "delta"}
	base := model.Note{Side: model.NoteSideNew, Range: [2]int{2, 2}}

	// 1. hash matches where it was → active, same range.
	n := base
	n.ContextHash = model.NoteContextHash([]string{"beta"})
	if st, rg := resolveOne(n, lines); st != model.NoteActive || rg != [2]int{2, 2} {
		t.Fatalf("unchanged = %v %v, want active {2 2}", st, rg)
	}

	// 2. the anchored line moved (two lines inserted above) → active, moved.
	moved := []string{"x", "y", "alpha", "beta", "gamma"}
	if st, rg := resolveOne(n, moved); st != model.NoteActive || rg != [2]int{4, 4} {
		t.Fatalf("moved = %v %v, want active {4 4}", st, rg)
	}
	// Re-indentation must not break the match (the hash trims).
	indented := []string{"alpha", "    beta", "gamma"}
	if st, rg := resolveOne(n, indented); st != model.NoteActive || rg != [2]int{2, 2} {
		t.Fatalf("re-indented = %v %v, want active {2 2}", st, rg)
	}

	// 3. the line's text is gone but the file is not → stale, clamped.
	if st, rg := resolveOne(n, []string{"alpha"}); st != model.NoteStale || rg != [2]int{1, 1} {
		t.Fatalf("gone-text = %v %v, want stale clamped {1 1}", st, rg)
	}

	// 4. the side itself is absent (file/commit gone) → orphaned.
	if st, _ := resolveOne(n, nil); st != model.NoteOrphaned {
		t.Fatalf("absent side = %v, want orphaned", st)
	}
}

func TestNotesForHidesOrphansAndSortsByLine(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	ctx := context.Background()
	add := func(path string, line int, text string) {
		if _, err := svc.NoteAdd(ctx, model.Note{
			Address: wtAddr(path), Side: model.NoteSideNew, Range: [2]int{line, line},
			Summary: text, ContextHash: model.NoteContextHash([]string{text}),
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("a/b.go", 3, "gamma")
	add("a/b.go", 1, "alpha")
	add("other.go", 1, "alpha")

	res, err := svc.NotesFor(ctx, wtAddr("a/b.go"), sideDiff("alpha", "beta", "gamma"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Range[0] != 1 || res[1].Range[0] != 3 {
		t.Fatalf("notes must be filtered by address and sorted by line: %+v", res)
	}
	// An address whose side is empty (deleted file) hides its notes entirely.
	res, _ = svc.NotesFor(ctx, wtAddr("a/b.go"), Diff{})
	if len(res) != 0 {
		t.Fatalf("orphaned notes must be hidden, got %+v", res)
	}
}

func TestNotesForIsStateInsensitiveWithinTheWorkingTree(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	ctx := context.Background()
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address: wtAddr("a/b.go"), Side: model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "x", ContextHash: model.NoteContextHash([]string{"alpha"}),
	}); err != nil {
		t.Fatal(err)
	}
	staged := model.FileAddress{State: model.StateStaged, Worktree: "/wt", Path: "a/b.go"}
	res, err := svc.NotesFor(ctx, staged, sideDiff("alpha"))
	if err != nil || len(res) != 1 {
		t.Fatalf("a working-tree note must show on the staged diff too: %+v %v", res, err)
	}
	commit := model.FileAddress{State: model.StateCommitted, Commit: "deadbee", Path: "a/b.go"}
	res, _ = svc.NotesFor(ctx, commit, sideDiff("alpha"))
	if len(res) != 0 {
		t.Fatalf("a working-tree note must NOT show on a commit diff: %+v", res)
	}
}

func TestNoteCountsCachedAndInvalidated(t *testing.T) {
	t.Parallel()
	svc, _ := notesSvc(t)
	ctx := context.Background()
	root, err := svc.NoteAdd(ctx, model.Note{
		Address: wtAddr("a/b.go"), Side: model.NoteSideNew, Range: [2]int{1, 1}, Summary: "x",
		ContextHash: "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateCommitted, Commit: "c0ffee", Path: "z.go"},
		Side:    model.NoteSideNew, Range: [2]int{2, 2}, Summary: "y", ContextHash: "h",
	}); err != nil {
		t.Fatal(err)
	}
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c.ByPath["a/b.go"] != 1 || c.ByCommit["c0ffee"] != 1 || c.ByCommitPath["c0ffee:z.go"] != 1 {
		t.Fatalf("counts = %+v", c)
	}
	// A reply must NOT bump the count (a badge counts THREADS).
	if _, err := svc.NoteReply(ctx, root.ID, model.Note{Summary: "r"}); err != nil {
		t.Fatal(err)
	}
	if c, _ = svc.NoteCounts(ctx); c.ByPath["a/b.go"] != 1 {
		t.Fatalf("replies must not count: %+v", c)
	}
	// Removal invalidates the cache.
	if err := svc.NoteRemove(ctx, root.ID); err != nil {
		t.Fatal(err)
	}
	if c, _ = svc.NoteCounts(ctx); c.ByPath["a/b.go"] != 0 {
		t.Fatalf("count cache must be invalidated by a mutation: %+v", c)
	}
}

func TestNotesDisabledWithoutAStore(t *testing.T) {
	t.Parallel()
	svc := New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	svc.disableNotesForTest() // never write NotesStatePath here: package var, parallel test
	if _, err := svc.NoteCounts(context.Background()); err != ErrNotesDisabled {
		t.Fatalf("NoteCounts without a store = %v, want ErrNotesDisabled", err)
	}
}
```

> The last test needs a way to force "no state dir" without touching the user's
> environment: add `func (s *Service) disableNotesForTest()` in `notes.go`
> (sets an unexported `notesOff bool` that `notesStore` checks first). Keep it
> in the non-test file next to `notesStore` so the guard is one branch, with a
> comment saying it exists for the disabled-path test.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/domain/ -run 'TestNote|TestResolveOne' -count=1`
Expected: build failure — `svc.SetNotesStore undefined`, `resolveOne undefined`.

- [ ] **Step 3: Wire the store**

`internal/domain/service.go` — add to the import block: `"github.com/homeend/gigagit/internal/notes"`. Add to the `Service` struct next to `bookmark`:

```go
	notes      notes.Store      // lazily resolved; nil disables notes
	notesOff   bool             // hard "no store" (the disabled-path test)
	noteCounts *NoteCounts      // cached badge counts; nil = cold, invalidated by every mutation

	// notesMaxAgeDays / notesMaxEntries carry [notes] into the store and the
	// sweep. Set by SetNotesPolicy before StartNotesSweep; 0 = the built-in
	// defaults (30 / 2000), <=0 after an explicit set = forever / uncapped.
	notesMaxAgeDays int
	notesMaxEntries int
	notesSweepOnce  sync.Once
```

`internal/domain/notesstore.go`:

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/homeend/gigagit/internal/notes"
)

// NotesStatePath overrides the notes root dir. "" uses the default XDG
// location. cmd/gg leaves it ""; tests point it at a temp dir.
var NotesStatePath string

// SetNotesStore injects a store (tests). A nil store re-arms lazy resolution.
func (s *Service) SetNotesStore(st notes.Store) {
	s.mu.Lock()
	s.notes = st
	s.noteCounts = nil
	s.mu.Unlock()
}

// disableNotesForTest forces the "no state directory" branch so the disabled
// path can be exercised without touching the caller's environment.
func (s *Service) disableNotesForTest() {
	s.mu.Lock()
	s.notesOff = true
	s.mu.Unlock()
}

// notesStore resolves (once) the per-repo note store, keyed by git common dir
// under the XDG state dir — the bookmarkStore shape. Returns nil (notes
// disabled) when no state dir is resolvable. The write-time policy is pushed
// on every call so a later SetNotesPolicy reaches an already-resolved store.
func (s *Service) notesStore(ctx context.Context) notes.Store {
	s.mu.Lock()
	if s.notesOff {
		s.mu.Unlock()
		return nil
	}
	st, max := s.notes, s.notesMaxEntries
	s.mu.Unlock()
	if st != nil {
		st.SetPolicy(notes.Policy{MaxEntries: max})
		return st
	}

	root := NotesStatePath
	if root == "" {
		base := notesBaseDir()
		if base == "" {
			return nil
		}
		key := "unknown"
		if cd, err := s.GitCommonDir(ctx); err == nil {
			key = repoKey(strings.TrimSpace(cd)) // reuse shelfstore.go's repoKey
		}
		root = filepath.Join(base, key)
	}
	fs := notes.NewFileStore(root)
	s.mu.Lock()
	if s.notes == nil {
		s.notes = fs
	}
	st, max = s.notes, s.notesMaxEntries
	s.mu.Unlock()
	st.SetPolicy(notes.Policy{MaxEntries: max})
	return st
}

// notesBaseDir resolves <state>/gg/notes cross-platform (mirrors
// bookmarkBaseDir). "" when no home/state dir exists.
func notesBaseDir() string {
	// An explicitly-set $XDG_STATE_HOME wins on every platform (it is a
	// deliberate override — and the only way tests can isolate state on
	// Windows); %LocalAppData% is the ambient Windows default.
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "notes")
	}
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "notes")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "notes")
}
```

- [ ] **Step 4: Write the domain surface**

`internal/domain/notes.go`:

```go
package domain

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// ErrNotesDisabled means no state directory was resolvable.
var ErrNotesDisabled = errors.New("notes: no state directory available")

// ResolvedNote is one note as it applies to an OPEN diff: the stored record,
// its computed status, the range it actually occupies now (the stored range
// when active in place, the found range when it moved, the clamped range when
// stale) and its replies, which share the root's anchor.
type ResolvedNote struct {
	Note    model.Note
	Status  model.NoteStatus
	Range   [2]int
	Replies []ResolvedNote
}

// NoteCounts are the row-painter badges: how many note THREADS (root notes,
// replies excluded) hang off each target. Unresolved by design — the Files and
// Commits painters must never touch the store or read file content.
type NoteCounts struct {
	ByPath       map[string]int // working-tree notes, by repo-relative path
	ByCommit     map[string]int // commit notes, by sha
	ByCommitPath map[string]int // commit notes, by "<sha>:<path>"
}

// NoteAdd stores a new note, filling ID, Created/Updated and (when the caller
// left it empty) ContextHash — read from the note's own side text. Frontends
// that already display the anchored line pass the hash so it matches exactly
// what the user saw.
func (s *Service) NoteAdd(ctx context.Context, n model.Note) (model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return model.Note{}, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return model.Note{}, err
	}
	if n.ID == "" {
		n.ID = notes.NewID(all)
	}
	if n.Source == "" {
		n.Source = model.NoteSourceUser
	}
	if n.Side == "" {
		n.Side = model.NoteSideNew
	}
	now := notes.Now().UTC()
	if n.Created.IsZero() {
		n.Created = now
	}
	n.Updated = now
	if n.ContextHash == "" {
		if lines, lerr := s.noteSideLines(ctx, n.Address, n.Side); lerr == nil && lines != nil {
			n.ContextHash = model.NoteContextHash(anchorLines(lines, n.Range))
		}
	}
	if err := st.Put(n); err != nil {
		return model.Note{}, err
	}
	s.invalidateNoteCounts()
	return n, nil
}

// NoteEdit replaces one note's summary and rationale.
func (s *Service) NoteEdit(ctx context.Context, id, summary, rationale string) error {
	st := s.notesStore(ctx)
	if st == nil {
		return ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return err
	}
	for _, n := range all {
		if n.ID != id {
			continue
		}
		n.Summary, n.Rationale, n.Updated = summary, rationale, notes.Now().UTC()
		if err := st.Put(n); err != nil {
			return err
		}
		s.invalidateNoteCounts()
		return nil
	}
	return notes.ErrNotFound
}

// NoteReply stores a reply that inherits its parent's anchor (address, side,
// range, fingerprint) so the thread re-anchors as one.
func (s *Service) NoteReply(ctx context.Context, parentID string, n model.Note) (model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return model.Note{}, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return model.Note{}, err
	}
	for _, p := range all {
		if p.ID != parentID {
			continue
		}
		n.ParentID = p.ID
		n.Address, n.Side, n.Range, n.ContextHash = p.Address, p.Side, p.Range, p.ContextHash
		return s.NoteAdd(ctx, n)
	}
	return model.Note{}, notes.ErrNotFound
}

// NoteRemove deletes a note; a root takes its replies with it.
func (s *Service) NoteRemove(ctx context.Context, id string) error {
	st := s.notesStore(ctx)
	if st == nil {
		return ErrNotesDisabled
	}
	if err := st.Remove(id); err != nil {
		return err
	}
	s.invalidateNoteCounts()
	return nil
}

// NotesFor returns the notes that apply to addr, resolved against the OPEN
// diff d and threaded (roots carry their replies), sorted new-side-first then
// by line. Orphaned notes are omitted — they are hidden until the startup
// sweep drops them. Reads never rewrite the store.
func (s *Service) NotesFor(ctx context.Context, addr model.FileAddress, d Diff) ([]ResolvedNote, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return nil, err
	}
	mine := make([]model.Note, 0, len(all))
	for _, n := range all {
		if sameNoteTarget(n.Address, addr) {
			mine = append(mine, n)
		}
	}
	oldLines, newLines := diffSideLines(d)
	res := resolveNotes(mine, oldLines, newLines)
	kept := res[:0]
	for _, r := range res {
		if r.Status != model.NoteOrphaned {
			kept = append(kept, r)
		}
	}
	return kept, nil
}

// NoteCounts returns the badge counts, cached until the next mutation.
func (s *Service) NoteCounts(ctx context.Context) (NoteCounts, error) {
	s.mu.Lock()
	if s.noteCounts != nil {
		c := *s.noteCounts
		s.mu.Unlock()
		return c, nil
	}
	s.mu.Unlock()

	st := s.notesStore(ctx)
	if st == nil {
		return NoteCounts{}, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return NoteCounts{}, err
	}
	c := NoteCounts{ByPath: map[string]int{}, ByCommit: map[string]int{}, ByCommitPath: map[string]int{}}
	for _, n := range all {
		if n.IsReply() { // a badge counts THREADS
			continue
		}
		if n.Address.State == model.StateCommitted && n.Address.Commit != "" {
			c.ByCommit[n.Address.Commit]++
			if n.Address.Path != "" {
				c.ByCommitPath[n.Address.Commit+":"+n.Address.Path]++
			}
			continue
		}
		if n.Address.Path != "" {
			c.ByPath[n.Address.Path]++
		}
	}
	s.mu.Lock()
	s.noteCounts = &c
	s.mu.Unlock()
	return c, nil
}

func (s *Service) invalidateNoteCounts() {
	s.mu.Lock()
	s.noteCounts = nil
	s.mu.Unlock()
}

// sameNoteTarget decides whether a stored note belongs to the diff at addr.
// Path + Commit are the identity: a working-tree note (Commit == "") shows on
// the unstaged AND the staged diff of the same file — the stored State only
// names the OLD-side base for the sweep, and re-anchoring absorbs the
// line-number difference between index and working tree.
func sameNoteTarget(a, b model.FileAddress) bool {
	return a.Path == b.Path && a.Commit == b.Commit && a.ShelfID == b.ShelfID
}

// diffSideLines projects a diff's aligned rows back into per-side line text,
// indexed so lines[no-1] is line `no`. A side with no numbered row at all is
// ABSENT (nil) — a pure add has no old side, a deleted file no new side — and
// notes anchored there resolve as orphaned.
func diffSideLines(d Diff) (oldLines, newLines []string) {
	maxL, maxR := 0, 0
	for _, r := range d.Result.Rows {
		if r.LeftNo > maxL {
			maxL = r.LeftNo
		}
		if r.RightNo > maxR {
			maxR = r.RightNo
		}
	}
	if maxL > 0 {
		oldLines = make([]string, maxL)
		for _, r := range d.Result.Rows {
			if r.LeftNo > 0 {
				oldLines[r.LeftNo-1] = r.Left
			}
		}
	}
	if maxR > 0 {
		newLines = make([]string, maxR)
		for _, r := range d.Result.Rows {
			if r.RightNo > 0 {
				newLines[r.RightNo-1] = r.Right
			}
		}
	}
	return oldLines, newLines
}

// anchorLines returns the (1-based, inclusive) slice a range names, clamped
// into lines. Empty when the range is entirely outside.
func anchorLines(lines []string, rng [2]int) []string {
	lo, hi := rng[0], rng[1]
	if lo < 1 {
		lo = 1
	}
	if hi > len(lines) {
		hi = len(lines)
	}
	if lo > hi || lo > len(lines) {
		return nil
	}
	return lines[lo-1 : hi]
}

// resolveOne computes one note's status against its side's text (nil = the
// side is absent). The four outcomes of §4.4, in order: found where it was;
// found again elsewhere (scanning outward from the stored start); not found
// but the file is there (stale, clamped); side absent (orphaned).
func resolveOne(n model.Note, lines []string) (model.NoteStatus, [2]int) {
	if lines == nil {
		return model.NoteOrphaned, n.Range
	}
	span := n.Range[1] - n.Range[0] + 1
	if span < 1 {
		span = 1
	}
	if got := anchorLines(lines, n.Range); len(got) == span &&
		model.NoteContextHash(got) == n.ContextHash {
		return model.NoteActive, n.Range
	}
	if start := findAnchor(lines, n.ContextHash, span, n.Range[0]); start > 0 {
		return model.NoteActive, [2]int{start, start + span - 1}
	}
	return model.NoteStale, clampRange(n.Range, len(lines))
}

// findAnchor scans OUTWARD from `from` (1-based) for a window of `span` lines
// whose fingerprint is hash, and returns its 1-based start (0 = not found).
// Outward means the nearest match to where the note used to be wins.
func findAnchor(lines []string, hash string, span, from int) int {
	last := len(lines) - span + 1
	if last < 1 || hash == "" {
		return 0
	}
	if from < 1 {
		from = 1
	}
	for d := 0; ; d++ {
		lo, hi := from-d, from+d
		loOK, hiOK := lo >= 1 && lo <= last, hi >= 1 && hi <= last
		if !loOK && !hiOK && lo < 1 && hi > last {
			return 0
		}
		if loOK && model.NoteContextHash(lines[lo-1:lo-1+span]) == hash {
			return lo
		}
		if d > 0 && hiOK && model.NoteContextHash(lines[hi-1:hi-1+span]) == hash {
			return hi
		}
	}
}

// clampRange pulls a range into a side of n lines (a stale note is still drawn
// somewhere sensible). A zero-line side clamps to {0,0}.
func clampRange(rg [2]int, n int) [2]int {
	if n <= 0 {
		return [2]int{0, 0}
	}
	lo, hi := rg[0], rg[1]
	if lo < 1 {
		lo = 1
	}
	if lo > n {
		lo = n
	}
	if hi < lo {
		hi = lo
	}
	if hi > n {
		hi = n
	}
	return [2]int{lo, hi}
}

// resolveNotes resolves and threads a note set against one diff's two sides.
// Roots sort NEW side first (where review happens), then by resolved start
// line, then by creation time; replies keep creation order under their root
// and inherit the root's status and range.
func resolveNotes(ns []model.Note, oldLines, newLines []string) []ResolvedNote {
	linesFor := func(side model.NoteSide) []string {
		if side == model.NoteSideOld {
			return oldLines
		}
		return newLines
	}
	roots := make([]ResolvedNote, 0, len(ns))
	byID := map[string]int{}
	for _, n := range ns {
		if n.IsReply() {
			continue
		}
		st, rg := resolveOne(n, linesFor(n.Side))
		byID[n.ID] = len(roots)
		roots = append(roots, ResolvedNote{Note: n, Status: st, Range: rg})
	}
	for _, n := range ns {
		if !n.IsReply() {
			continue
		}
		i, ok := byID[n.ParentID]
		if !ok {
			continue // an orphaned reply: the sweep drops it
		}
		roots[i].Replies = append(roots[i].Replies, ResolvedNote{
			Note: n, Status: roots[i].Status, Range: roots[i].Range,
		})
	}
	for i := range roots {
		sort.SliceStable(roots[i].Replies, func(a, b int) bool {
			return roots[i].Replies[a].Note.Created.Before(roots[i].Replies[b].Note.Created)
		})
	}
	sort.SliceStable(roots, func(a, b int) bool {
		if roots[a].Note.Side != roots[b].Note.Side {
			return roots[a].Note.Side == model.NoteSideNew
		}
		if roots[a].Range[0] != roots[b].Range[0] {
			return roots[a].Range[0] < roots[b].Range[0]
		}
		return roots[a].Note.Created.Before(roots[b].Note.Created)
	})
	return roots
}

// splitLines is the shared byte→line projection for side text read from git
// (the sweep and NoteAdd's hash fill). A trailing newline does not create a
// phantom last line.
func splitLines(b []byte) []string {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\n")
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/domain/ -run 'TestNote|TestResolve|TestNotes' -count=1`
Expected: PASS. (`noteSideLines` is added in Task 5; until then `NoteAdd`'s hash-fill branch calls it — write the Task-5 function stub `noteSideLines` in Task 5 and, to keep this task self-contained, add it here as the real implementation and let Task 5 only add the sweep. **Do it here**: move `noteSideLines` into `notes.go` as written in Task 5 Step 3 and Task 5 then only adds `notes_sweep.go`.)

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/domain/ && go vet ./internal/domain/
git add internal/domain/notes.go internal/domain/notesstore.go internal/domain/notes_test.go internal/domain/service.go
git commit -m "feat(notes): domain surface — add/edit/reply/remove, resolution, NotesFor, NoteCounts

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 5: the startup sweep

**Files:**
- Create: `internal/domain/notes_sweep.go`
- Create: `internal/domain/notes_sweep_test.go`
- Modify: `internal/tui/load.go` (~line 91, after `SetVersionsPolicy`)
- Modify: `internal/tui/source.go` (~line 441, the `configReadyMsg` path)
- Modify: `internal/web/settings.go` (`applyUIPolicies`)

**Interfaces:**
- Produces:
  - `func (s *Service) SetNotesPolicy(maxAgeDays, maxEntries int)`
  - `func (s *Service) StartNotesSweep()` — per-Service `sync.Once`, goroutine, never blocks
  - `func (s *Service) sweepNotes(ctx context.Context) (dropped int, err error)`
  - `func (s *Service) noteSideLines(ctx context.Context, addr model.FileAddress, side model.NoteSide) ([]string, error)` (written in Task 4, see its Step 5 note)
- Consumes: `notes.Now`, `Store.Sweep`, `ShowFile`/`WorktreeFile`/`ResolveBytes`, `observ.NoteFailure`.

- [ ] **Step 1: Write the failing tests**

`internal/domain/notes_sweep_test.go` (SERIAL — it moves the `notes.Now` clock):

```go
package domain

import (
	"context"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// NOTE: no t.Parallel() anywhere in this file — notes.Now is a package var.

func TestSweepDropsExpiredStaleAndOrphaned(t *testing.T) {
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	svc.SetNotesPolicy(30, 2000)
	ctx := context.Background()

	real := notes.Now
	base := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	notes.Now = func() time.Time { return base }
	defer func() { notes.Now = real }()

	// The index blob the ACTIVE note anchors on. The span name is "git show"
	// (repo.ShowFile); "git -C show" is ShowFileInDir, which this path never uses.
	f.SetResponse("git show", gitexec.Result{Stdout: "alpha\nbeta\n"})

	keep, err := svc.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateStaged, Worktree: "/wt", Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{2, 2}, Summary: "live",
		ContextHash: model.NoteContextHash([]string{"beta"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	stale, err := svc.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateStaged, Worktree: "/wt", Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{2, 2}, Summary: "text is gone",
		ContextHash: model.NoteContextHash([]string{"vanished"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	// An EXPIRED note: created 40 days ago, max_age_days = 30.
	notes.Now = func() time.Time { return base.AddDate(0, 0, -40) }
	old, err := svc.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateStaged, Worktree: "/wt", Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{2, 2}, Summary: "ancient",
		ContextHash: model.NoteContextHash([]string{"beta"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	notes.Now = func() time.Time { return base }

	dropped, err := svc.sweepNotes(ctx)
	if err != nil {
		t.Fatalf("sweepNotes: %v", err)
	}
	if dropped != 2 {
		t.Fatalf("dropped = %d, want 2 (stale + expired)", dropped)
	}
	left, _ := svc.notesStore(ctx).Load()
	if len(left) != 1 || left[0].ID != keep.ID {
		t.Fatalf("sweep left %+v (stale %s, expired %s)", left, stale.ID, old.ID)
	}
}

func TestSweepKeepsEverythingWhenAgeIsNonPositive(t *testing.T) {
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	svc.SetNotesPolicy(-1, 2000) // keep forever
	ctx := context.Background()
	f.SetResponse("git show", gitexec.Result{Stdout: "alpha\n"})

	real := notes.Now
	notes.Now = func() time.Time { return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) }
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateStaged, Worktree: "/wt", Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "ancient but kept",
		ContextHash: model.NoteContextHash([]string{"alpha"}),
	}); err != nil {
		t.Fatal(err)
	}
	notes.Now = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
	defer func() { notes.Now = real }()

	if dropped, err := svc.sweepNotes(ctx); err != nil || dropped != 0 {
		t.Fatalf("max_age_days <= 0 must keep everything: dropped %d err %v", dropped, err)
	}
}

func TestStartNotesSweepRunsOnce(t *testing.T) {
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	svc.SetNotesPolicy(30, 2000)
	svc.StartNotesSweep()
	svc.StartNotesSweep()
	svc.waitNotesSweepForTest() // blocks until the goroutine(s) finish
	if n := svc.notesSweepRunsForTest(); n != 1 {
		t.Fatalf("sweep ran %d times, want 1", n)
	}
}
```

> `waitNotesSweepForTest`/`notesSweepRunsForTest` are two tiny helpers in
> `notes_sweep.go` (a `sync.WaitGroup` and an `atomic.Int32` on `Service`),
> written next to `StartNotesSweep` with a comment that they exist for the
> once-semantics test. Keeping them in the non-test file avoids exporting
> anything.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/domain/ -run 'TestSweep|TestStartNotes' -count=1`
Expected: build failure — `svc.SetNotesPolicy undefined`.

- [ ] **Step 3: Write the sweep**

`internal/domain/notes_sweep.go`:

```go
package domain

import (
	"context"
	"strconv"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
	"github.com/homeend/gigagit/internal/observ"
)

// notesSweepTimeout bounds the background housekeeping pass: it reads file
// content per annotated target, and a huge repo on a slow mount must never
// leave a goroutine reading forever behind a quit.
const notesSweepTimeout = 30 * time.Second

// SetNotesPolicy pushes [notes] onto the Service: the sweep's expiry window
// and the store's write-time entry cap. Call it before StartNotesSweep, from
// the same place a frontend applies SetVersionsPolicy.
func (s *Service) SetNotesPolicy(maxAgeDays, maxEntries int) {
	s.mu.Lock()
	s.notesMaxAgeDays, s.notesMaxEntries = maxAgeDays, maxEntries
	st := s.notes
	s.mu.Unlock()
	if st != nil {
		st.SetPolicy(notes.Policy{MaxEntries: maxEntries})
	}
}

// StartNotesSweep runs housekeeping ONCE per Service, in the background: it
// drops notes past [notes] max_age_days and notes whose anchor no longer
// resolves (stale or orphaned), then rewrites the file once. It never blocks a
// read and never surfaces an error — failures go to the session error ring.
// A re-root builds a fresh Service for a different repo, which sweeps its own
// store.
func (s *Service) StartNotesSweep() {
	s.notesSweepOnce.Do(func() {
		s.notesSweepWG.Add(1)
		go func() {
			defer s.notesSweepWG.Done()
			s.notesSweepRuns.Add(1)
			ctx, cancel := context.WithTimeout(context.Background(), notesSweepTimeout)
			defer cancel()
			if _, err := s.sweepNotes(ctx); err != nil {
				observ.NoteFailure("notes sweep", err)
			}
		}()
	})
}

// waitNotesSweepForTest / notesSweepRunsForTest exist for the once-semantics
// test (the goroutine is fire-and-forget in production).
func (s *Service) waitNotesSweepForTest()    { s.notesSweepWG.Wait() }
func (s *Service) notesSweepRunsForTest() int { return int(s.notesSweepRuns.Load()) }

// sweepNotes is the housekeeping pass: keep a note only when it has not
// expired AND still resolves as active. Side text is read once per (address,
// side) pair, so a file with twenty notes costs one read per side.
func (s *Service) sweepNotes(ctx context.Context) (int, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return 0, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return 0, err
	}
	if len(all) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	maxAge := s.notesMaxAgeDays
	s.mu.Unlock()
	var cutoff time.Time
	if maxAge > 0 {
		cutoff = notes.Now().UTC().AddDate(0, 0, -maxAge)
	}

	cache := map[string][]string{}
	sideOf := func(n model.Note) []string {
		// State is part of the key: a staged and an unstaged note on the same
		// path read DIFFERENT old sides (HEAD vs the index).
		key := string(n.Side) + "\x00" + strconv.Itoa(int(n.Address.State)) + "\x00" +
			n.Address.Commit + "\x00" + n.Address.Path + "\x00" + n.Address.ShelfID
		if lines, ok := cache[key]; ok {
			return lines
		}
		lines, lerr := s.noteSideLines(ctx, n.Address, n.Side)
		if lerr != nil {
			lines = nil // unreadable target = gone = orphaned
		}
		cache[key] = lines
		return lines
	}

	keep := func(n model.Note) bool {
		if !cutoff.IsZero() && n.Created.Before(cutoff) {
			return false
		}
		if n.IsReply() {
			return true // a reply lives or dies with its root (dropOrphanReplies)
		}
		status, _ := resolveOne(n, sideOf(n))
		return status == model.NoteActive
	}
	return st.Sweep(keep)
}

// noteSideLines reads one side's text for an address. The OLD side is the base
// the diff that created the note compared against (§4.4 "Old-side base"),
// which in this codebase is:
//
//	StateUnstaged  old = index blob      new = working file   (Files panel)
//	StateStaged    old = HEAD blob       new = index blob     (Staged panel)
//	StateUntracked old = absent          new = working file
//	StateCommitted old = <sha>^:path     new = <sha>:path
//	StateShelf     old = absent          new = the shelf entry's bytes
//
// A nil result (with a nil error) means the side is legitimately absent; an
// error means the target could not be read at all — the caller treats both as
// "gone".
func (s *Service) noteSideLines(ctx context.Context, addr model.FileAddress, side model.NoteSide) ([]string, error) {
	old := side == model.NoteSideOld
	switch addr.State {
	case model.StateCommitted:
		rev := addr.Commit
		if old {
			rev += "^"
		}
		b, err := s.ShowFile(ctx, rev, addr.Path)
		if err != nil {
			return nil, err
		}
		return splitLines(b), nil
	case model.StateShelf:
		if old {
			return nil, nil
		}
		b, err := s.ResolveBytes(ctx, addr.FileRef())
		if err != nil {
			return nil, err
		}
		return splitLines(b), nil
	case model.StateStaged:
		rev := "" // "" = the index blob (git show :path)
		if old {
			rev = "HEAD"
		}
		b, err := s.ShowFile(ctx, rev, addr.Path)
		if err != nil {
			return nil, err
		}
		return splitLines(b), nil
	case model.StateUntracked:
		if old {
			return nil, nil
		}
		b, err := s.WorktreeFile(ctx, addr.Path)
		if err != nil {
			return nil, err
		}
		return splitLines(b), nil
	default: // StateUnstaged
		if old {
			b, err := s.ShowFile(ctx, "", addr.Path)
			if err != nil {
				return nil, err
			}
			return splitLines(b), nil
		}
		b, err := s.WorktreeFile(ctx, addr.Path)
		if err != nil {
			return nil, err
		}
		return splitLines(b), nil
	}
}
```

Add the two test-support fields to `Service` (next to `notesSweepOnce`, Task 4 Step 3):

```go
	notesSweepWG   sync.WaitGroup
	notesSweepRuns atomic.Int32
```

- [ ] **Step 4: Start it from the three frontends**

`internal/tui/load.go`, immediately after `svc.SetVersionsPolicy(versionsPolicyFromConfig(cfg))`:

```go
		svc.SetNotesPolicy(cfg.Notes.MaxAgeDays, cfg.Notes.MaxEntries)
		svc.StartNotesSweep() // once per Service; drops expired/dangling notes off-thread
```

`internal/tui/source.go` (the `configReadyMsg` path, after the same `SetVersionsPolicy` line):

```go
		svc.SetNotesPolicy(cfg.Notes.MaxAgeDays, cfg.Notes.MaxEntries)
		svc.StartNotesSweep()
```

`internal/web/settings.go` in `applyUIPolicies`, after `svc.SetSyntaxHighlighting(cfg.UI.SyntaxOn())`:

```go
	svc.SetNotesPolicy(cfg.Notes.MaxAgeDays, cfg.Notes.MaxEntries)
	svc.StartNotesSweep()
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/domain/ ./internal/tui/ ./internal/web/ -run 'TestSweep|TestStartNotes|TestLoad|TestApplyUI' -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/ && go vet ./internal/domain/ ./internal/tui/ ./internal/web/
git add internal/domain/notes_sweep.go internal/domain/notes_sweep_test.go internal/domain/service.go internal/tui/load.go internal/tui/source.go internal/web/settings.go
git commit -m "feat(notes): startup sweep — expired, stale and orphaned notes pruned off-thread

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 6: TUI — note rows in `relayout`, rendering, cursor and fold markers

**Files:**
- Create: `internal/tui/diff_notes.go`
- Create: `internal/tui/diff_notes_test.go`
- Modify: `internal/tui/diff_view.go` (two `diffView` fields, two `dRow` fields, `relayout`)
- Modify: `internal/tui/diff_cursor.go` (`cursorDispRange` stops before note rows)
- Modify: `internal/tui/diff_render.go` (two styles, the note-row branch, `foldSeparator` marker)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (one key)

**Interfaces:**
- Produces:
  - fields `diffView.notes []domain.ResolvedNote`, `diffView.hideAgent bool`
  - fields `dRow.note *noteLine`, `dRow.noteMark bool`
  - `type noteLine struct{ id, rootID string; depth int; text string; stale bool }`
  - `func (v *diffView) noteRowIndex() (byLine map[int][]noteLine, foldMark map[int]bool)`
  - `func (v *diffView) noteAnchorLine(n domain.ResolvedNote) (line int, visible bool)`
  - `func noteLinesOf(r domain.ResolvedNote) []noteLine`
  - `func noteRowText(nl noteLine, w int) string`
  - `func foldSeparator(n, w int, marked bool) string` (signature change; one caller)
- Consumes: `domain.ResolvedNote`, `textdiff.Line`, `model.NoteSide*`, `model.NoteStale`, `model.NoteSourceAgent`.

- [ ] **Step 1: Write the failing tests**

`internal/tui/diff_notes_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// notedView builds a 40-row view carrying the given resolved notes.
func notedView(ns []domain.ResolvedNote, changed ...int) *diffView {
	v := diffViewWith(cursorRows(40, changed...), changed)
	v.notes = ns
	v.relayout(0)
	return v
}

func rootNote(id string, line int, summary, rationale string, src model.NoteSource, status model.NoteStatus) domain.ResolvedNote {
	return domain.ResolvedNote{
		Note: model.Note{ID: id, Source: src, Author: "ada", Summary: summary,
			Rationale: rationale, Side: model.NoteSideNew, Range: [2]int{line, line}},
		Status: status, Range: [2]int{line, line},
	}
}

func TestRelayoutAppendsNoteRowsUnderTheirLine(t *testing.T) {
	t.Parallel()
	n := rootNote("n1", 5, "off by one", "the loop runs one short", model.NoteSourceUser, model.NoteActive)
	n.Replies = []domain.ResolvedNote{{
		Note:   model.Note{ID: "r1", ParentID: "n1", Author: "bot", Summary: "agreed", Source: model.NoteSourceAgent},
		Status: model.NoteActive, Range: [2]int{5, 5},
	}}
	v := notedView([]domain.ResolvedNote{n})
	// Line 5 is RightNo 5 => logical line index 4.
	start := v.lineStart[4]
	if v.disp[start].note != nil {
		t.Fatal("the content row must come first")
	}
	got := []string{}
	for i := start + 1; i < len(v.disp) && v.disp[i].note != nil; i++ {
		got = append(got, v.disp[i].note.text)
	}
	if len(got) != 3 {
		t.Fatalf("want summary + rationale + reply rows, got %q", got)
	}
	if !strings.Contains(got[0], "ada") || !strings.Contains(got[0], "off by one") || !strings.HasPrefix(got[0], "◆") {
		t.Fatalf("summary row = %q", got[0])
	}
	if !strings.Contains(got[1], "the loop runs one short") {
		t.Fatalf("rationale row = %q", got[1])
	}
	if !strings.Contains(got[2], "agreed") || v.disp[start+3].note.depth != 1 {
		t.Fatalf("reply row = %q depth %d", got[2], v.disp[start+3].note.depth)
	}
	// The NEXT logical line must start after the note rows.
	if v.lineStart[5] != start+4 {
		t.Fatalf("lineStart[5] = %d, want %d (content + 3 note rows)", v.lineStart[5], start+4)
	}
}

func TestNoteRowsHiddenWhenAgentLayerOff(t *testing.T) {
	t.Parallel()
	user := rootNote("u", 5, "mine", "", model.NoteSourceUser, model.NoteActive)
	agent := rootNote("a", 6, "bot's", "", model.NoteSourceAgent, model.NoteActive)
	v := notedView([]domain.ResolvedNote{user, agent})
	v.hideAgent = true
	v.relayout(0)
	for _, dr := range v.disp {
		if dr.note != nil && strings.Contains(dr.note.text, "bot's") {
			t.Fatal("agent notes must be hidden while the agent layer is off")
		}
	}
	found := false
	for _, dr := range v.disp {
		if dr.note != nil && strings.Contains(dr.note.text, "mine") {
			found = true
		}
	}
	if !found {
		t.Fatal("user notes must stay visible with the agent layer off")
	}
}

func TestCursorRangeStopsBeforeNoteRows(t *testing.T) {
	t.Parallel()
	v := notedView([]domain.ResolvedNote{rootNote("n1", 5, "s", "", model.NoteSourceUser, model.NoteActive)})
	v.setCursorLine(4, 10)
	s, e := v.cursorDispRange()
	if e != s+1 {
		t.Fatalf("cursor range = [%d,%d), want the content row only", s, e)
	}
	// A click on the note row still lands on the OWNING line.
	v.setCursorDisp(s+1, 10)
	if v.curLine != 4 {
		t.Fatalf("clicking a note row put the cursor on line %d, want 4", v.curLine)
	}
}

func TestFoldSeparatorMarkedWhenItHidesANote(t *testing.T) {
	t.Parallel()
	// Changes at 10 and 30 => partial mode folds the rest; line 20 is hidden.
	v := diffViewWith(cursorRows(40, 10, 30), []int{10, 30})
	v.notes = []domain.ResolvedNote{rootNote("n1", 21, "hidden note", "", model.NoteSourceUser, model.NoteActive)}
	v.partial = true
	v.rebuild()
	marked := 0
	for _, dr := range v.disp {
		if dr.fold > 0 && dr.noteMark {
			marked++
		}
		if dr.note != nil {
			t.Fatal("a note whose line is folded away must not get its own row")
		}
	}
	if marked != 1 {
		t.Fatalf("exactly one fold must carry the ◆ marker, got %d", marked)
	}
}

func TestNoteRowRendersStaleDimmedAndFitsWidth(t *testing.T) {
	t.Parallel()
	stale := noteLine{id: "n", text: "◆ ada: gone", stale: true}
	long := noteLine{id: "n", text: "◆ ada: " + strings.Repeat("x", 200)}
	if got := noteRowText(long, 40); lipgloss.Width(got) > 40 {
		t.Fatalf("a note row must be truncated to the width, got %d cols", lipgloss.Width(got))
	}
	if noteRowText(stale, 40) == noteRowText(noteLine{id: "n", text: "◆ ada: gone"}, 40) {
		t.Fatal("a stale note must render differently from an active one")
	}
}

func TestRenderWithoutNotesIsUnchanged(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, sameRowsTUI(60, 10, 50), []int{10, 50})
	m.width = 140
	before := m.View()
	v := m.diffLayer()
	v.notes = nil
	v.relayout(v.width)
	if got := m.View(); got != before {
		t.Fatal("a view with no notes must render byte-identically")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestRelayoutAppendsNote|TestNoteRows|TestCursorRangeStops|TestFoldSeparatorMarked|TestNoteRowRenders' -count=1`
Expected: build failure — `v.notes undefined`, `noteRowText undefined`.

- [ ] **Step 3: Add the fields**

`internal/tui/diff_view.go`, in `diffView` after `zCycle`:

```go
	// notes are the resolved review notes for this view's address, loaded
	// asynchronously (notesLoadedMsg) and re-loaded after every mutation and
	// srcNotes refresh. relayout turns them into synthetic display rows;
	// nothing here ever touches the shared cached textdiff rows.
	notes []domain.ResolvedNote
	// hideAgent mirrors Model.notesAgentOff onto the view, because relayout
	// (called by rebuild, ctrl+w and every resize) has no Model to ask.
	hideAgent bool
```

in `dRow`:

```go
	note     *noteLine // non-nil: a synthetic note row belonging to `line`
	noteMark bool      // fold row: a note hides under this fold (◆ on the rule)
```

and in `relayout`, replace the loop head and the fold case, and append note rows at the end of each line's block:

```go
	byLine, foldMark := v.noteRowIndex()

	for li := range v.lines {
		v.lineStart[li] = len(v.disp)
		ln := v.lines[li]
		switch {
		case ln.Fold > 0:
			v.disp = append(v.disp, dRow{line: li, fold: ln.Fold, noteMark: foldMark[li], first: true})
		case v.long != longWrap || width <= 0:
			v.disp = append(v.disp, dRow{line: li, row: ln.Row, first: true})
		default:
			// … unchanged wrap branch …
		}
		for i := range byLine[li] {
			nl := byLine[li][i]
			v.disp = append(v.disp, dRow{line: li, row: ln.Row, note: &nl})
		}
	}
```

- [ ] **Step 4: Write the note-row builder**

`internal/tui/diff_notes.go`:

```go
package tui

import (
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// Review-note display rows. A resolved note becomes one to three synthetic
// display rows appended AFTER its anchored line's content rows: the summary
// (◆ author: summary), an optional rationale row, and one such pair per
// reply, indented. They live only in v.disp — v.lines, the change blocks and
// the shared textdiff rows are untouched, so the cursor, n/p, wrap and the
// fold machinery keep working on the same logical stream as before.

// noteLine is ONE display row of a note (not one note): the pointer on dRow
// names the row, while id/rootID name the note it came from so E/R/Delete can
// find it again.
type noteLine struct {
	id     string // the note (root or reply) this row shows
	rootID string // the thread's root id (== id on a root row)
	depth  int    // 0 = root, 1 = reply (indent = 2*depth)
	text   string // the whole row, already assembled
	stale  bool   // the anchor text is gone: render dim
}

// noteRowIndex maps logical line index → its note rows, and fold line index →
// "a note hides under this fold". Agent-sourced notes are skipped entirely
// while the agent layer is off; user notes always render (hunk's policy).
func (v *diffView) noteRowIndex() (map[int][]noteLine, map[int]bool) {
	if len(v.notes) == 0 {
		return nil, nil
	}
	byLine := map[int][]noteLine{}
	foldMark := map[int]bool{}
	for _, r := range v.notes {
		if v.hideAgent && r.Note.Source == model.NoteSourceAgent {
			continue
		}
		li, visible := v.noteAnchorLine(r)
		if li < 0 {
			continue // the anchor is not in this view at all
		}
		if !visible {
			foldMark[li] = true // folded away: mark the fold rule instead
			continue
		}
		byLine[li] = append(byLine[li], noteLinesOf(r)...)
	}
	return byLine, foldMark
}

// noteAnchorLine finds the logical line a note hangs off: the line carrying
// the END of its range on its side (§4.4 phase-2 hunks anchor at the range
// end). visible=false means the number exists in the file but is folded away,
// and the returned index is the FOLD entry that hides it. (-1, false) means
// the number is not in this view at all.
func (v *diffView) noteAnchorLine(r domain.ResolvedNote) (int, bool) {
	no := r.Range[1]
	if no <= 0 {
		return -1, false
	}
	old := r.Note.Side == model.NoteSideOld
	numOf := func(ln textdiff.Line) int {
		if old {
			return ln.Row.LeftNo
		}
		return ln.Row.RightNo
	}
	prev := 0
	for i, ln := range v.lines {
		if ln.Fold > 0 {
			// The hidden run spans (prev, next): find the next real number.
			next := 0
			for j := i + 1; j < len(v.lines); j++ {
				if v.lines[j].Fold == 0 && numOf(v.lines[j]) > 0 {
					next = numOf(v.lines[j])
					break
				}
			}
			if no > prev && (next == 0 || no < next) {
				return i, false
			}
			continue
		}
		if numOf(ln) == no {
			return i, true
		}
		if n := numOf(ln); n > 0 {
			prev = n
		}
	}
	return -1, false
}

// noteLinesOf flattens one resolved note (and its replies) into display rows.
func noteLinesOf(r domain.ResolvedNote) []noteLine {
	rows := noteRowsFor(r, r.Note.ID, 0)
	for _, rep := range r.Replies {
		rows = append(rows, noteRowsFor(rep, r.Note.ID, 1)...)
	}
	return rows
}

// noteRowsFor is one note's own rows: the summary, then the rationale.
func noteRowsFor(r domain.ResolvedNote, rootID string, depth int) []noteLine {
	stale := r.Status == model.NoteStale
	head := "◆ "
	if depth > 0 {
		head = "↳ "
	}
	if r.Note.Author != "" {
		head += r.Note.Author + ": "
	}
	head += r.Note.Summary
	if stale {
		head += " " + i18n.T("(stale)")
	}
	rows := []noteLine{{id: r.Note.ID, rootID: rootID, depth: depth, text: head, stale: stale}}
	if r.Note.Rationale != "" {
		rows = append(rows, noteLine{id: r.Note.ID, rootID: rootID, depth: depth,
			text: "  " + r.Note.Rationale, stale: stale})
	}
	return rows
}
```

(add `"github.com/homeend/gigagit/internal/textdiff"` to the import block — `noteAnchorLine` uses `textdiff.Line`.)

- [ ] **Step 5: Render the rows**

`internal/tui/diff_render.go`, in the style block:

```go
	diffNote      = lipgloss.NewStyle().Foreground(lipgloss.Color("110")) // review note rows
	diffNoteStale = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // stale: the anchored text is gone
```

after `foldSeparator`:

```go
// noteRowText renders one note display row across the FULL width: two cells of
// indent per depth level, then the assembled text, truncated to w. A stale row
// is dimmed — the note still says something, it just no longer sits on the
// text it was written about.
func noteRowText(nl noteLine, w int) string {
	txt := strings.Repeat("  ", nl.depth) + nl.text
	style := diffNote
	if nl.stale {
		style = diffNoteStale
	}
	return style.Render(truncate(txt, w))
}
```

in `diffPaneLines`, REPLACE the existing fold branch (the loop already declares
`dr := v.disp[i]` — do not redeclare it) with a note branch in front of it, so a
note row is never mistaken for content and never carries the cursor mark:

```go
		if dr.note != nil {
			out = append(out, noteRowText(*dr.note, w))
			continue
		}
		if dr.fold > 0 {
			out = append(out, foldSeparator(dr.fold, w, dr.noteMark))
			continue
		}
```

and `foldSeparator` gains the marker:

```go
// foldSeparator renders a fold marker as a centered label on a dim rule
// spanning the full width. marked prefixes ◆: a review note anchors on a line
// this fold hides (f, or }/{, brings it into view).
func foldSeparator(n, w int, marked bool) string {
	label := i18n.T(" ⤬ %d unchanged lines ", n)
	if n == 1 {
		label = i18n.T(" ⤬ 1 unchanged line ")
	}
	if marked {
		label = "◆" + label
	}
	// … unchanged from here …
}
```

`internal/tui/diff_cursor.go` — `cursorDispRange` ends at the first note row:

```go
func (v *diffView) cursorDispRange() (start, end int) {
	if v.curLine < 0 || v.curLine >= len(v.lineStart) {
		return 0, 0
	}
	start = v.lineStart[v.curLine]
	end = len(v.disp)
	if v.curLine+1 < len(v.lineStart) {
		end = v.lineStart[v.curLine+1]
	}
	// The cursor marks the CONTENT rows of its line only: the note rows that
	// follow belong to the line but are not part of it.
	for i := start; i < end && i < len(v.disp); i++ {
		if v.disp[i].note != nil {
			end = i
			break
		}
	}
	return start, end
}
```

- [ ] **Step 6: i18n — one new key in all four bundles**

| key | ja | ko | zh | ru |
|---|---|---|---|---|
| `(stale)` | `(古い)` | `(오래됨)` | `(已过时)` | `(устарело)` |

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/tui/ -count=1`
Expected: PASS — including `TestRenderDiffViewPanes` and `TestRenderWithoutNotesIsUnchanged` (no notes ⇒ byte-identical output) and the four i18n gate tests.

- [ ] **Step 8: Commit**

```bash
gofmt -l internal/tui/ && go vet ./internal/tui/
git add internal/tui/diff_notes.go internal/tui/diff_notes_test.go internal/tui/diff_view.go internal/tui/diff_cursor.go internal/tui/diff_render.go internal/i18n/lang/
git commit -m "feat(tui): diff view renders review-note rows, fold markers and stale dimming

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 7: TUI — keys `c`/`E`/`R`/`a`/`}`/`{`, the note popup, `srcNotes`, footer/help/menu

**Files:**
- Create: `internal/tui/note_popup.go`
- Create: `internal/tui/note_keys.go`
- Create: `internal/tui/note_keys_test.go`
- Modify: `internal/tui/diff_view.go` (`updateDiffViewKey`)
- Modify: `internal/tui/model.go` (two fields, `notesLoadedMsg` / `noteMutatedMsg` arms, the `diffMsg` arm, the `srcNotes` arm)
- Modify: `internal/tui/source.go` (`srcNotes` enum + consumers + names + read arm)
- Modify: `internal/tui/i18n_display.go` (`sourceDisplayName`)
- Modify: `internal/tui/action_menu.go` (the Delete-note row)
- Modify: `internal/tui/diff_render.go` (`diffHintFor`)
- Modify: `internal/tui/diff_render_test.go` (`TestRenderDiffViewPanes` render width 140 → 170: the hint grew)
- Modify: `internal/tui/help.go` (five Diff-view rows)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`

**Interfaces:**
- Produces:
  - `type notesLoadedMsg struct{ tag string; notes []domain.ResolvedNote; err error }`
  - `type noteMutatedMsg struct{ err error }`
  - `func (m Model) loadNotesCmd() tea.Cmd`
  - `func (m Model) diffNoteAddress() (model.FileAddress, bool)`
  - `func (m Model) noteAnchorAtCursor() (side model.NoteSide, line int, hash string, ok bool)`
  - `func (m Model) noteNearCursor() (domain.ResolvedNote, bool)`
  - `type notePopup struct{ popupMax; summary, rationale textfield; field, ratScroll int; mode noteFormMode; targetID string; … }`
  - `func (m Model) openNotePopup(mode noteFormMode) (tea.Model, tea.Cmd)`
  - `func (m Model) noteSubmitCmd(p *notePopup) tea.Cmd`, `func (m Model) noteRemoveCmd(id string) tea.Cmd`
  - `func (v *diffView) nextNoteLine(dir int) (int, bool)`
  - `func (m Model) jumpNote(dir int) (Model, bool)`
  - `func (m Model) stepNotedFile(dir int) (tea.Model, tea.Cmd)`, `func (m Model) peekNotedFile(dir int) bool`
  - `func (m Model) noteDeleteRow() (actionRow, bool)`
  - `sourceKey` value `srcNotes`
  - fields `Model.noteCounts domain.NoteCounts`, `Model.notesAgentOff bool`
- Consumes: `domain.NotesFor/NoteAdd/NoteEdit/NoteReply/NoteRemove/NoteCounts`, `textfield`, `popupMax`, `popupBox`/`popupTextWidth`, `viewField`/`viewFieldWindow`, `fileArmDir`, `stepDiffFileStatus`/`stepDiffFileTree` mechanics.

- [ ] **Step 1: Write the failing tests**

`internal/tui/note_keys_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// notedModel opens a diff over 40 rows with notes on lines 5 and 25. The title
// and tag matter: diffNoteAddress() goes through focusedBookmark(), which
// refuses a titleless view, and loadNotesCmd tags its result with m.diffTag.
func notedModel(t *testing.T) Model {
	t.Helper()
	m := openedDiffModel(12, cursorRows(40, 4, 24), []int{4, 24})
	v := m.diffLayer()
	v.title = "a/b.go"
	m.diffTag = statusDiffTag("a/b.go", false)
	v.notes = []domain.ResolvedNote{
		rootNote("n1", 5, "first", "", model.NoteSourceUser, model.NoteActive),
		rootNote("n2", 25, "second", "", model.NoteSourceAgent, model.NoteActive),
	}
	v.relayout(0)
	return m
}

func TestBraceJumpsBetweenAnnotatedLines(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	v.setCursorLine(0, m.diffBodyRows())
	nm, _ := v.update(m, synthKey("}"))
	m = nm.(Model)
	if m.diffLayer().curLine != 4 {
		t.Fatalf("} from the top = line %d, want 4 (the note on line 5)", m.diffLayer().curLine)
	}
	nm, _ = m.diffLayer().update(m, synthKey("}"))
	m = nm.(Model)
	if m.diffLayer().curLine != 24 {
		t.Fatalf("second } = line %d, want 24", m.diffLayer().curLine)
	}
	nm, _ = m.diffLayer().update(m, synthKey("{"))
	m = nm.(Model)
	if m.diffLayer().curLine != 4 {
		t.Fatalf("{ = line %d, want 4", m.diffLayer().curLine)
	}
}

func TestBraceExpandsAFoldedNote(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 4, 34), []int{4, 34})
	v := m.diffLayer()
	v.notes = []domain.ResolvedNote{rootNote("n1", 21, "buried", "", model.NoteSourceUser, model.NoteActive)}
	v.partial = true
	v.rebuild()
	v.setCursorLine(0, m.diffBodyRows())
	nm, _ := v.update(m, synthKey("}"))
	m = nm.(Model)
	v = m.diffLayer()
	if v.partial {
		t.Fatal("} onto a folded note must expand the view (like f)")
	}
	if v.lines[v.curLine].Row.RightNo != 21 {
		t.Fatalf("cursor landed on RightNo %d, want 21", v.lines[v.curLine].Row.RightNo)
	}
}

func TestAgentLayerToggleHidesAgentNotes(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	nm, _ := m.diffLayer().update(m, synthKey("a"))
	m = nm.(Model)
	if !m.notesAgentOff || !m.diffLayer().hideAgent {
		t.Fatal("a must flip BOTH the session flag and the view's mirror")
	}
	for _, dr := range m.diffLayer().disp {
		if dr.note != nil && strings.Contains(dr.note.text, "second") {
			t.Fatal("the agent note must be gone from the display stream")
		}
	}
}

func TestCOpensTheNotePopupAnchoredAtTheCursor(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.diffLayer().setCursorLine(9, m.diffBodyRows())
	nm, _ := m.diffLayer().update(m, synthKey("c"))
	m = nm.(Model)
	p, ok := m.topLayer().(*notePopup)
	if !ok {
		t.Fatalf("c must push a notePopup, top is %T", m.topLayer())
	}
	if p.mode != noteAdd || p.line != 10 || p.side != model.NoteSideNew {
		t.Fatalf("popup anchor = mode %v line %d side %q", p.mode, p.line, p.side)
	}
	// An empty summary cancels: esc-equivalent, nothing submitted.
	// synthKey maps only enter/esc/space to a Type — ctrl+s must be built by hand.
	nm, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m = nm.(Model)
	if _, still := m.topLayer().(*notePopup); still {
		t.Fatal("ctrl+s with an empty summary must close the popup")
	}
	if cmd != nil {
		t.Fatal("ctrl+s with an empty summary must not dispatch a write")
	}
}

func TestERTargetTheNoteNearestAboveTheCursor(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.diffLayer().setCursorLine(30, m.diffBodyRows()) // below both notes
	nm, _ := m.diffLayer().update(m, synthKey("E"))
	m = nm.(Model)
	p, ok := m.topLayer().(*notePopup)
	if !ok || p.mode != noteEdit || p.targetID != "n2" {
		t.Fatalf("E must edit the nearest note above the cursor, got %#v (ok %v)", p, ok)
	}
	m = m.popLayer()
	nm, _ = m.diffLayer().update(m, synthKey("R"))
	m = nm.(Model)
	p, _ = m.topLayer().(*notePopup)
	if p == nil || p.mode != noteReply || p.targetID != "n2" {
		t.Fatalf("R must reply to the same note, got %#v", p)
	}
}

func TestNoteKeysInertWithoutNotes(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	for _, k := range []string{"E", "R", "}", "{"} {
		nm, _ := m.diffLayer().update(m, synthKey(k))
		mm := nm.(Model)
		if _, isPopup := mm.topLayer().(*notePopup); isPopup {
			t.Fatalf("%s must be inert with no notes", k)
		}
	}
}

func TestSrcNotesRegistered(t *testing.T) {
	t.Parallel()
	if sourceNames[srcNotes] != "notes" || sourceDisplayName(srcNotes) != "notes" {
		t.Fatal("srcNotes needs a name and a display name")
	}
	if len(srcConsumers[srcNotes]) == 0 {
		t.Fatal("srcNotes must list its consumer panels")
	}
	for _, it := range scheduledItems {
		if !it.isFetch && !it.isRemoteTags && it.source == srcNotes {
			t.Fatal("srcNotes must never be polled by the background scheduler")
		}
	}
}

func TestNoteDeleteRowOnlyWithANoteInReach(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.diffLayer().setCursorLine(30, m.diffBodyRows())
	if _, ok := m.noteDeleteRow(); !ok {
		t.Fatal("the . menu must offer Delete note when a note sits above the cursor")
	}
	m2 := openedDiffModel(12, cursorRows(40), nil)
	if _, ok := m2.noteDeleteRow(); ok {
		t.Fatal("no notes ⇒ no Delete note row")
	}
}

var _ = tea.KeyMsg{}
```

> `synthKey` already exists in `internal/tui` (used by the mouse double-click
> path); reuse it rather than building `tea.KeyMsg` literals.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestBrace|TestAgentLayer|TestCOpens|TestERTarget|TestNoteKeys|TestSrcNotes|TestNoteDeleteRow' -count=1`
Expected: build failure — `notePopup undefined`, `srcNotes undefined`.

- [ ] **Step 3: The note commands and the anchor helpers**

`internal/tui/note_keys.go`:

```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// notesLoadedMsg carries the resolved notes for one open diff. tag gates a
// stale result exactly like diffMsg (the diff may have been stepped away).
type notesLoadedMsg struct {
	tag   string
	notes []domain.ResolvedNote
	err   error
}

// noteMutatedMsg reports the outcome of an add/edit/reply/remove.
type noteMutatedMsg struct{ err error }

// diffNoteAddress is the address notes hang off for the open diff: the same
// provenance focusedBookmark builds for this surface (working tree vs commit).
// Not available on a two-sided compare (no single file).
func (m Model) diffNoteAddress() (model.FileAddress, bool) {
	v := m.diffLayer()
	if v == nil || v.compare || v.title == "" {
		return model.FileAddress{}, false
	}
	b, ok := m.focusedBookmark()
	if !ok || b.Path == "" {
		return model.FileAddress{}, false
	}
	return b.Address(), true
}

// loadNotesCmd resolves this diff's notes off the UI thread. The rows are the
// SHARED cached rows: they are read, wrapped in a domain.Diff value and never
// mutated.
func (m Model) loadNotesCmd() tea.Cmd {
	v := m.diffLayer()
	addr, ok := m.diffNoteAddress()
	if v == nil || !ok || m.svc == nil {
		return nil
	}
	svc, tag, rows := m.svc, m.diffTag, v.full
	return func() tea.Msg {
		ns, err := svc.NotesFor(context.Background(), addr,
			domain.Diff{Result: textdiff.Result{Rows: rows}})
		return notesLoadedMsg{tag: tag, notes: ns, err: err}
	}
}

// noteAnchorAtCursor is where `c` puts a new note: the cursor row's new-side
// line, or the old-side line on a Del row (§4.1's cursorRow contract). The
// fingerprint is taken from the text the user is looking at.
func (m Model) noteAnchorAtCursor() (model.NoteSide, int, string, bool) {
	v := m.diffLayer()
	if v == nil {
		return "", 0, "", false
	}
	r, ok := v.cursorRow()
	if !ok {
		return "", 0, "", false
	}
	if r.RightNo > 0 {
		return model.NoteSideNew, r.RightNo, model.NoteContextHash([]string{r.Right}), true
	}
	if r.LeftNo > 0 {
		return model.NoteSideOld, r.LeftNo, model.NoteContextHash([]string{r.Left}), true
	}
	return "", 0, "", false
}

// noteNearCursor is the root note E/R/Delete act on: the last one anchored at
// or above the cursor line, else the first note in the view.
func (m Model) noteNearCursor() (domain.ResolvedNote, bool) {
	v := m.diffLayer()
	if v == nil || len(v.notes) == 0 {
		return domain.ResolvedNote{}, false
	}
	best, found := domain.ResolvedNote{}, false
	first, hasFirst := domain.ResolvedNote{}, false
	for _, r := range v.notes {
		if v.hideAgent && r.Note.Source == model.NoteSourceAgent {
			continue
		}
		li, _ := v.noteAnchorLine(r)
		if li < 0 {
			continue
		}
		if !hasFirst {
			first, hasFirst = r, true
		}
		if li <= v.curLine {
			best, found = r, true
		}
	}
	if found {
		return best, true
	}
	return first, hasFirst
}

// nextNoteLine is the next (dir>0) / previous (dir<0) logical line that carries
// a note — including a FOLD entry that hides one, which the caller expands.
func (v *diffView) nextNoteLine(dir int) (int, bool) {
	byLine, foldMark := v.noteRowIndex()
	best, found := -1, false
	consider := func(li int) {
		if dir > 0 && li <= v.curLine {
			return
		}
		if dir < 0 && li >= v.curLine {
			return
		}
		if !found || (dir > 0 && li < best) || (dir < 0 && li > best) {
			best, found = li, true
		}
	}
	for li := range byLine {
		consider(li)
	}
	for li := range foldMark {
		consider(li)
	}
	return best, found
}

// jumpNote moves the cursor to the next/previous annotated line, expanding the
// view when the target hides under a fold (what f does), and reports whether
// it moved.
func (m Model) jumpNote(dir int) (Model, bool) {
	v := m.diffLayer()
	if v == nil {
		return m, false
	}
	body := m.diffBodyRows()
	li, ok := v.nextNoteLine(dir)
	if !ok {
		return m, false
	}
	if v.lines[li].Fold > 0 {
		// The note hides under this fold: expand to the full file (the f
		// toggle), then re-find the anchor in the REBUILT stream. curLine is a
		// partial-mode index and would be stale after the rebuild — always
		// smaller than the same row's full-mode index — so a backward search
		// would reject the very note we expanded for. Re-anchor first.
		cr, hadRow := v.cursorRow()
		v.partial = false
		v.rebuild()
		m.diffPartial = false
		if hadRow {
			v.reanchorCursor(cr.LeftNo, cr.RightNo)
		}
		li, ok = v.nextNoteLine(dir)
		if !ok {
			return m, false
		}
	}
	v.setCursorLine(li, body)
	return m, true
}

// peekNotedFile / stepNotedFile are the }/{ file step: the next file in the
// open diff's source list that CARRIES notes (NoteCounts, no store read).
// They mirror stepDiffFile's structure and reuse its per-nav steppers.
func (m Model) peekNotedFile(dir int) bool {
	_, ok := m.nextNotedFile(dir)
	return ok
}

func (m Model) stepNotedFile(dir int) (tea.Model, tea.Cmd) {
	steps, ok := m.nextNotedFile(dir)
	if !ok {
		return m, nil
	}
	nm, cmd := m, tea.Cmd(nil)
	for i := 0; i < steps; i++ {
		var tm tea.Model
		tm, cmd = nm.stepDiffFile(dir)
		nm = tm.(Model)
	}
	return nm, cmd
}

// nextNotedFile counts how many plain file steps in direction dir land on a
// file that carries notes (0, false = none). Stepping N times reuses the
// existing per-nav steppers unchanged, so tree/status/staged all work.
func (m Model) nextNotedFile(dir int) (int, bool) {
	paths := m.diffFileSequence(dir) // paths after the current one, in step order
	for i, p := range paths {
		if m.notedFilePath(p) {
			return i + 1, true
		}
	}
	return 0, false
}

// notedFilePath reports whether a path carries notes at the open diff's
// provenance: by path for a working-tree diff, by "<sha>:<path>" for a commit.
func (m Model) notedFilePath(path string) bool {
	if v := m.diffLayer(); v != nil && v.rev != "" {
		return m.noteCounts.ByCommitPath[v.rev+":"+path] > 0
	}
	return m.noteCounts.ByPath[path] > 0
}

// noteDeleteRow is the . menu's "Delete note" (there is no key for it).
func (m Model) noteDeleteRow() (actionRow, bool) {
	if _, ok := m.topLayer().(*diffView); !ok {
		return actionRow{}, false
	}
	r, ok := m.noteNearCursor()
	if !ok {
		return actionRow{}, false
	}
	id := r.Note.ID
	return actionRow{
		id:    "note-delete",
		label: i18n.T("Delete note"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.noteRemoveCmd(id)
		},
	}, true
}

// noteRemoveCmd deletes a note (a root takes its replies) off the UI thread.
func (m Model) noteRemoveCmd(id string) tea.Cmd {
	svc := m.svc
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		return noteMutatedMsg{err: svc.NoteRemove(context.Background(), id)}
	}
}
```

> `diffFileSequence(dir)` does not exist yet: add it to
> `internal/tui/diff_filenav.go` as a small read-only walker built from the
> same three sources `stepDiffFile` dispatches on — `m.filesView.visible()`
> file rows for `diffNavTree`, and `m.displayIndices(panelFiles/panelStaged)`
> skipping `model.KindUnmerged` for the two status navs — returning the paths
> AFTER the current selection in step order. Keep `nextStatusFile`/
> `nextFileRow` untouched.

- [ ] **Step 4: The popup**

`internal/tui/note_popup.go`:

```go
package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// noteFormMode is what the popup will do on ctrl+s.
type noteFormMode int

const (
	noteAdd noteFormMode = iota
	noteEdit
	noteReply
)

// notePopup collects a note's summary and optional rationale — the commit
// popup's title/description pair, with the same keys (tab switches, enter is
// next/newline, ctrl+s saves, esc cancels, ctrl+t maximizes). An empty summary
// on ctrl+s is a cancel (§4.4).
type notePopup struct {
	popupMax
	summary   textfield
	rationale textfield
	field     int // 0 = summary, 1 = rationale
	ratScroll int

	mode     noteFormMode
	targetID string // noteEdit: the note; noteReply: the parent
	addr     model.FileAddress
	side     model.NoteSide
	line     int
	hash     string
	author   string
}

// openNotePopup pushes the form for mode, anchored at the cursor (add) or at
// the note nearest the cursor (edit/reply). Inert when the surface carries no
// address, or when edit/reply have no note to act on.
func (m Model) openNotePopup(mode noteFormMode) (tea.Model, tea.Cmd) {
	addr, ok := m.diffNoteAddress()
	if !ok {
		return m, nil
	}
	p := &notePopup{mode: mode, addr: addr, author: m.identity.EffectiveName}
	switch mode {
	case noteAdd:
		side, line, hash, aok := m.noteAnchorAtCursor()
		if !aok {
			return m, nil
		}
		p.side, p.line, p.hash = side, line, hash
		p.summary, p.rationale = newTextField(""), newTextField("")
	case noteEdit, noteReply:
		r, rok := m.noteNearCursor()
		if !rok {
			return m, nil
		}
		p.targetID = r.Note.ID
		p.side, p.line, p.hash = r.Note.Side, r.Range[1], r.Note.ContextHash
		if mode == noteEdit {
			p.summary, p.rationale = newTextField(r.Note.Summary), newTextField(r.Note.Rationale)
		} else {
			p.summary, p.rationale = newTextField(""), newTextField("")
		}
	}
	return m.pushLayer(p), nil
}

// update handles one key. It swallows every key: esc cancels, ctrl+c quits,
// ctrl+s saves (or cancels on an empty summary).
func (p *notePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyCtrlS:
		m = m.popLayer()
		if strings.TrimSpace(p.summary.Value()) == "" {
			return m, nil // an empty summary is a cancel
		}
		return m, m.noteSubmitCmd(p)
	case tea.KeyTab, tea.KeyShiftTab:
		p.field = (p.field + 1) % 2
		return m, nil
	case tea.KeyEnter:
		if p.field == 0 {
			p.field = 1
		} else {
			p.rationale.InsertNewline()
		}
		return m, nil
	case tea.KeyUp:
		if p.field == 1 {
			p.rationale.Up()
		}
		return m, nil
	case tea.KeyDown:
		if p.field == 1 {
			p.rationale.Down()
		}
		return m, nil
	}
	if p.field == 0 {
		p.summary.HandleEditKey(msg)
	} else {
		p.rationale.HandleEditKey(msg)
	}
	return m, nil
}

func (p *notePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *notePopup) box(m Model) string {
	w, _ := m.overlayDims()
	innerW := popupResolveWidth(w, p.maximized, commitNormalWidth(w))
	contentW := popupTextWidth(innerW)
	heading := i18n.T("Add note")
	switch p.mode {
	case noteEdit:
		heading = i18n.T("Edit note")
	case noteReply:
		heading = i18n.T("Reply to note")
	}
	footer := packHints([]string{
		i18n.T("[tab] switch field"),
		i18n.T("[enter] newline/next"),
		i18n.T("[ctrl+t] fullscreen"),
		i18n.T("[ctrl+s] save"),
		i18n.T("[esc] cancel"),
	}, contentW)

	sumCur, ratCur := "  ", "  "
	if p.field == 0 {
		sumCur = "> "
	} else {
		ratCur = "> "
	}
	var b strings.Builder
	b.WriteString(heading + "  " + i18n.T("line %d", p.line) + "\n\n")
	// Field labels follow the commit popup: plain, not translated.
	b.WriteString(viewField(sumCur+"summary:   ", p.summary, p.field == 0, contentW) + "\n")
	b.WriteString(viewFieldWindow(ratCur+"rationale: ", p.rationale, p.field == 1, contentW, 8, &p.ratScroll) + "\n")
	b.WriteString("\n" + footer)
	return popupBox(innerW, b.String())
}

// noteSubmitCmd performs the popup's write off the UI thread.
func (m Model) noteSubmitCmd(p *notePopup) tea.Cmd {
	svc := m.svc
	if svc == nil {
		return nil
	}
	summary := strings.TrimSpace(p.summary.Value())
	rationale := strings.TrimSpace(p.rationale.Value())
	mode, id := p.mode, p.targetID
	n := model.Note{
		Source: model.NoteSourceUser, Author: p.author, Address: p.addr,
		Side: p.side, Range: [2]int{p.line, p.line}, ContextHash: p.hash,
		Summary: summary, Rationale: rationale,
	}
	return func() tea.Msg {
		ctx := context.Background()
		var err error
		switch mode {
		case noteEdit:
			err = svc.NoteEdit(ctx, id, summary, rationale)
		case noteReply:
			_, err = svc.NoteReply(ctx, id, n)
		default:
			_, err = svc.NoteAdd(ctx, n)
		}
		return noteMutatedMsg{err: err}
	}
}
```

- [ ] **Step 5: Wire the keys**

`internal/tui/diff_view.go`, in `updateDiffViewKey` (all six keys were verified
free against the existing switch: `. g G F esc h b e up down k/alt+up j/alt+down
pgup pgdown z home end n/ctrl+down p/ctrl+up N P f ctrl+w left right 0`):

```go
	case "c":
		return m.openNotePopup(noteAdd)
	case "E":
		return m.openNotePopup(noteEdit)
	case "R":
		return m.openNotePopup(noteReply)
	case "a":
		// Session-scoped: the flag lives on the Model and is mirrored onto
		// every view that relayouts (relayout has no Model to ask).
		m.notesAgentOff = !m.notesAgentOff
		v.hideAgent = m.notesAgentOff
		cr, hadRow := v.cursorRow()
		wasVisible := v.cursorVisible(body)
		v.relayout(v.width)
		v.reanchorAfterRebuild(cr, hadRow, wasVisible, body)
	case "}":
		var moved bool
		if m, moved = m.jumpNote(1); !moved {
			switch {
			case m.diffNav == diffNavNone || !m.peekNotedFile(1):
				m.diffNotice = i18n.T("▸ no next file with notes")
			case fileArmed == fileArmNext:
				return m.stepNotedFile(1)
			default:
				v.fileArm = fileArmNext
			}
		}
	case "{":
		var moved bool
		if m, moved = m.jumpNote(-1); !moved {
			switch {
			case m.diffNav == diffNavNone || !m.peekNotedFile(-1):
				m.diffNotice = i18n.T("▸ no previous file with notes")
			case fileArmed == fileArmPrev:
				return m.stepNotedFile(-1)
			default:
				v.fileArm = fileArmPrev
			}
		}
```

`internal/tui/model.go`:
- two `Model` fields, next to `diffCursor`:

```go
	noteCounts    domain.NoteCounts // badge counts (srcNotes); zero value = no badges
	notesAgentOff bool              // `a`: hide agent-written notes for this session
```

- the `diffMsg` arm returns the notes load (ruling 5):

```go
	case diffMsg:
		dv := m.diffLayer()
		if dv == nil || msg.tag != m.diffTag {
			return m, nil
		}
		compare := dv.compare
		*dv = *msg.view
		dv.loading = false
		dv.compare = dv.compare || compare
		dv.hideAgent = m.notesAgentOff
		return m, m.loadNotesCmd()
```

- two new arms next to it:

```go
	case notesLoadedMsg:
		dv := m.diffLayer()
		if dv == nil || msg.tag != m.diffTag {
			return m, nil // closed, or stepped to another file
		}
		if msg.err != nil {
			return m, nil // notes are best-effort; the sweep logs real failures
		}
		body := m.diffBodyRows()
		cr, hadRow := dv.cursorRow()
		wasVisible := dv.cursorVisible(body) // a free-scrolled view keeps its place
		dv.notes = msg.notes
		dv.relayout(dv.width)
		dv.reanchorAfterRebuild(cr, hadRow, wasVisible, body)
		return m, nil
	case noteMutatedMsg:
		if msg.err != nil {
			m.statusMsg = i18n.T("note: %s", msg.err.Error()) // the transient status line (sourceErr's field)
			return m, nil
		}
		var counts tea.Cmd
		m, counts = m.reloadSourcesCmd([]sourceKey{srcNotes}, reloadOpts{})
		return m, tea.Batch(m.loadNotesCmd(), counts)
```

- the `srcNotes` arrival arm, next to `case srcIdentity`:

```go
		case srcNotes:
			m.noteCounts = msg.value.(domain.NoteCounts)
```

`internal/tui/source.go`:
- the enum, before `srcCount`: `srcNotes`
- `srcConsumers`: `srcNotes: {panelFiles, panelStaged, panelCommits},`
- `sourceNames`: `srcNotes: "notes",`
- the read arm in `readSourceCmd`:

```go
		case srcNotes:
			c, err := svc.NoteCounts(ctx)
			if errors.Is(err, domain.ErrNotesDisabled) {
				// No state dir (a read-only home, a locked-down box): notes are
				// simply off. Not an error worth a status line every refresh.
				c, err = domain.NoteCounts{}, nil
			}
			out.value, out.err = c, err
```

(`errors` joins the import block.) `refresh.go`'s `scheduledItems` is NOT touched — `srcNotes` is never polled (ruling 14).

`internal/tui/i18n_display.go`, in `sourceDisplayName`:

```go
	case srcNotes:
		return i18n.T("notes")
```

`internal/tui/action_menu.go`, right after `rows = append(rows, m.diffAlignRows()...)`:

```go
		if r, ok := m.noteDeleteRow(); ok {
			rows = append(rows, r)
		}
```

- [ ] **Step 6: Footer hint and help**

`internal/tui/diff_render.go` — `diffHintFor`'s return becomes:

```go
	return i18n.T("[↑↓] scroll  [j/k] line  [c] note  [}/{] notes  [z] align  [e] edit  [n/p] change  [f] part  [ctrl+w] %s", mode) + pan + i18n.T("  [h] hist  [b] blame  [esc] close")
```

The scroll variant of the hint now measures ~146 display columns (the two new
groups add ~23), so `TestRenderDiffViewPanes` — which renders at 140 and
asserts `[esc] close` survives truncation — must widen with it:
`internal/tui/diff_render_test.go`, `m.width = 140` → `m.width = 170`, and
extend the comment above it to name the note groups as the reason.

`internal/tui/help.go`, in the Diff-view block after the `e` rows:

```go
		r("c", i18n.T("add a review note at the cursor line (a summary, plus an optional rationale; an empty summary cancels)")),
		r("E/R", i18n.T("edit / reply to the note nearest at or above the cursor line")),
		r("a", i18n.T("show or hide agent-written notes; your own notes always stay visible")),
		r("}/{", i18n.T("jump to the next / previous annotated line (a folded note expands the view); at the last / first one, press again to step to the next / previous file that carries notes")),
		r("", i18n.T("the . menu grows a Delete note row while a note sits at or above the cursor line")),
```

- [ ] **Step 7: i18n — the new keys in all four bundles**

| key | ja | ko | zh | ru |
|---|---|---|---|---|
| `Add note` | `ノートを追加` | `노트 추가` | `添加笔记` | `Добавить заметку` |
| `Edit note` | `ノートを編集` | `노트 편집` | `编辑笔记` | `Изменить заметку` |
| `Reply to note` | `ノートに返信` | `노트에 답글` | `回复笔记` | `Ответить на заметку` |
| `Delete note` | `ノートを削除` | `노트 삭제` | `删除笔记` | `Удалить заметку` |
| `notes` | `ノート` | `노트` | `笔记` | `заметки` |
| `note: %s` | `ノート: %s` | `노트: %s` | `笔记: %s` | `заметка: %s` |
| `[ctrl+s] save` | `[ctrl+s] 保存` | `[ctrl+s] 저장` | `[ctrl+s] 保存` | `[ctrl+s] сохранить` |
| `▸ no next file with notes` | `▸ 次のノート付きファイルはありません` | `▸ 노트가 있는 다음 파일이 없습니다` | `▸ 没有下一个带笔记的文件` | `▸ нет следующего файла с заметками` |
| `▸ no previous file with notes` | `▸ 前のノート付きファイルはありません` | `▸ 노트가 있는 이전 파일이 없습니다` | `▸ 没有上一个带笔记的文件` | `▸ нет предыдущего файла с заметками` |
| `[↑↓] scroll  [j/k] line  [c] note  [}/{] notes  [z] align  [e] edit  [n/p] change  [f] part  [ctrl+w] %s` | `[↑↓] スクロール  [j/k] 行  [c] ノート  [}/{] ノート移動  [z] 位置  [e] 編集  [n/p] 変更  [f] 部分  [ctrl+w] %s` | `[↑↓] 스크롤  [j/k] 줄  [c] 노트  [}/{] 노트 이동  [z] 정렬  [e] 편집  [n/p] 변경  [f] 부분  [ctrl+w] %s` | `[↑↓] 滚动  [j/k] 行  [c] 笔记  [}/{] 笔记跳转  [z] 对齐  [e] 编辑  [n/p] 变更  [f] 部分  [ctrl+w] %s` | `[↑↓] прокрутка  [j/k] строка  [c] заметка  [}/{] заметки  [z] выровнять  [e] правка  [n/p] изменение  [f] часть  [ctrl+w] %s` |
| `add a review note at the cursor line (a summary, plus an optional rationale; an empty summary cancels)` | `カーソル行にレビューノートを追加する（要約と、任意の理由。要約が空ならキャンセル）` | `커서 줄에 리뷰 노트를 추가한다(요약과 선택적 근거; 요약이 비면 취소)` | `在光标行添加评审笔记（摘要，以及可选的理由；摘要为空则取消）` | `добавить заметку рецензии на строке курсора (краткое описание и необязательное обоснование; пустое описание отменяет)` |
| `edit / reply to the note nearest at or above the cursor line` | `カーソル行またはその上で最も近いノートを編集／返信する` | `커서 줄 또는 그 위에서 가장 가까운 노트를 편집하거나 답글을 단다` | `编辑／回复光标行或其上方最近的笔记` | `изменить / ответить на ближайшую заметку на строке курсора или выше` |
| `show or hide agent-written notes; your own notes always stay visible` | `エージェントが書いたノートの表示を切り替える。自分のノートは常に表示される` | `에이전트가 쓴 노트를 표시하거나 숨긴다; 내 노트는 항상 보인다` | `显示或隐藏代理写的笔记；你自己的笔记始终可见` | `показать или скрыть заметки агентов; собственные заметки видны всегда` |
| `jump to the next / previous annotated line (a folded note expands the view); at the last / first one, press again to step to the next / previous file that carries notes` | `次／前のノートがある行へ移動する（折りたたまれたノートは表示を展開する）。最後／最初で再度押すとノートのある次／前のファイルへ移動する` | `노트가 있는 다음 / 이전 줄로 이동한다(접힌 노트는 보기를 펼친다); 마지막 / 첫 번째에서 다시 누르면 노트가 있는 다음 / 이전 파일로 이동한다` | `跳到下一／上一个带笔记的行（折叠的笔记会展开视图）；在最后／最前一个再按一次会跳到下一／上一个带笔记的文件` | `перейти к следующей / предыдущей аннотированной строке (свёрнутая заметка разворачивает вид); на последней / первой нажмите ещё раз, чтобы перейти к следующему / предыдущему файлу с заметками` |
| `the . menu grows a Delete note row while a note sits at or above the cursor line` | `カーソル行またはその上にノートがあるとき . メニューに「ノートを削除」行が現れる` | `커서 줄 또는 그 위에 노트가 있으면 . 메뉴에 「노트 삭제」 행이 나타난다` | `当光标行或其上方存在笔记时，. 菜单会出现「删除笔记」行` | `в меню . появляется строка «Удалить заметку», когда заметка находится на строке курсора или выше` |

- [ ] **Step 8: Run the tests**

Run: `go test ./internal/tui/ -count=1`
Expected: PASS — including `i18n_scan_test.go`, `options_vocab_test.go`, `menu_labels_test.go`, `TestSrcConsumersCoverAllSources`, `TestReloadAllBumpsEveryGenAndBatches` and `TestSourceLoadingShowsGlyphAndStatus` (all three iterate `0..srcCount` and must tolerate the new key — `srcNotes` has consumer panels, so no skip is needed).

- [ ] **Step 9: Commit**

```bash
gofmt -l internal/tui/ && go vet ./internal/tui/
git add internal/tui/ internal/i18n/lang/
git commit -m "feat(tui): note keys c/E/R/a/}/{, the note popup, srcNotes refresh source

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 8: `◆N` badges on Files-panel and Commits-panel rows

**Files:**
- Modify: `internal/tui/viewstate.go` (`statusList` field + `Row` + a new `Haystack`, `listFor`)
- Modify: `internal/tui/view.go` (`commitIdentRowAt`)
- Create: `internal/tui/note_badge_test.go`

**Interfaces:**
- Produces:
  - field `statusList.notes map[string]int`
  - `func (l statusList) Haystack(i int) string` — the UNBADGED row (the filter must not match a badge)
  - `func noteBadge(n int) string` — `"  ◆N"`, `""` for n <= 0
- Consumes: `Model.noteCounts` (Task 7), the `haystacker` interface (`viewstate.go:199`).

- [ ] **Step 1: Write the failing tests**

`internal/tui/note_badge_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func TestStatusRowBadgeIsDisplayOnly(t *testing.T) {
	t.Parallel()
	l := statusList{
		files: []model.FileStatus{{Path: "a/b.go", Kind: model.KindModified}},
		p:     panelFiles,
		mtime: map[int]int64{},
		notes: map[string]int{"a/b.go": 3},
	}
	row := l.Row(0)
	if !strings.Contains(row, "◆3") {
		t.Fatalf("row = %q, want a ◆3 badge", row)
	}
	// The filter haystack must NOT carry the badge: typing "3" must not match
	// a file because of its note count (the sanitize-DISPLAY-not-HAYSTACK rule).
	h, ok := any(l).(haystacker)
	if !ok {
		t.Fatal("statusList must implement haystacker once Row carries a badge")
	}
	if strings.Contains(h.Haystack(0), "◆") {
		t.Fatalf("haystack = %q, must be badge-free", h.Haystack(0))
	}
	if l.notes = nil; strings.Contains(l.Row(0), "◆") {
		t.Fatal("no counts ⇒ no badge (rows must be unchanged for repos without notes)")
	}
}

func TestCommitRowShowsNoteBadge(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.commits = []model.Commit{{Hash: "c0ffeeaa", Subject: "do a thing"}}
	m.commitListMode = true
	m.noteCounts = domain.NoteCounts{ByCommit: map[string]int{"c0ffeeaa": 2}}
	row := m.commitIdentRowAt(0, m.commitIdentWidth(), false, -1)
	if !strings.Contains(row, "◆2") {
		t.Fatalf("commit row = %q, want a ◆2 badge", row)
	}
	m.noteCounts = domain.NoteCounts{}
	if strings.Contains(m.commitIdentRowAt(0, m.commitIdentWidth(), false, -1), "◆") {
		t.Fatal("no counts ⇒ no badge")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestStatusRowBadge|TestCommitRowShowsNote' -count=1`
Expected: build failure — `unknown field notes in statusList`.

- [ ] **Step 3: Badge the two rows**

`internal/tui/viewstate.go`:

```go
type statusList struct {
	files []model.FileStatus
	p     panel
	root  string
	mtime map[int]int64
	notes map[string]int // NoteCounts.ByPath: the trailing ◆N badge (display only)
}

// Row is built lazily per index … (unchanged comment) … The ◆N note badge is
// DISPLAY ONLY: Haystack below feeds the / filter the unbadged text, so typing
// a digit never matches a file because of its note count.
func (l statusList) Row(i int) string {
	return l.Haystack(i) + noteBadge(l.notes[l.files[i].Path])
}

// Haystack is the filter-match text: the row WITHOUT its note badge.
func (l statusList) Haystack(i int) string {
	return fmt.Sprintf("%c %s", fileGlyph(l.p, l.files[i]), l.files[i].Path)
}
```

`listFor`'s `panelFiles, panelStaged` arm:

```go
		return statusList{files: m.status.Files, p: p, root: m.currentWorktree,
			mtime: map[int]int64{}, notes: m.noteCounts.ByPath}
```

`internal/tui/view.go`, at the end of `commitIdentRowAt`'s real-commit path, right
before the `switch` that prefixes the dot/graph cells:

```go
	row := tok + group + " " + safeRowText(c.Subject) + noteBadge(m.noteCounts.ByCommit[c.Hash])
```

and, next to `noteRowText` in `internal/tui/diff_notes.go`:

```go
// noteBadge is the trailing "◆N" a Files/Commits row carries when its target
// has notes. Display only — never part of a filter haystack.
func noteBadge(n int) string {
	if n <= 0 {
		return ""
	}
	return "  ◆" + strconv.Itoa(n)
}
```

(`strconv` joins `diff_notes.go`'s imports.)

The startup fan-out needs no wiring: `reloadAllCmd` walks every source
`0..srcCount`, so `srcNotes` loads with the rest on app start and on `r`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -count=1`
Expected: PASS (the filter tests that render Files rows still match on paths).

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/tui/ && go vet ./internal/tui/
git add internal/tui/viewstate.go internal/tui/view.go internal/tui/diff_notes.go internal/tui/note_badge_test.go
git commit -m "feat(tui): ◆N note badges on Files and Commits rows (display only, never filtered)

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 9: web — `/api/notes`, the SSE `notes` event, note rows and keys

**Files:**
- Modify: `internal/domain/notes.go` (one additive method, `NotesAt`)
- Create: `internal/web/notes.go`
- Create: `internal/web/notes_test.go`
- Modify: `internal/web/static/core.js` (four `state` fields)
- Modify: `internal/web/static/files.js` (`fetchNotes`, note rows in `diffHTML`, `◆N` in `renderFiles`, row click)
- Modify: `internal/web/static/keys.js` (`c`/`E`/`R`/`a`/`}`/`{`)
- Modify: `internal/web/static/live.js` (the `notes` source)
- Modify: `internal/web/static/style.css` (`tr.note`, `tr.note.stale`, `tr.cur`)

**Interfaces:**
- Produces:
  - `func (s *Service) NotesAt(ctx context.Context, addr model.FileAddress) ([]ResolvedNote, error)` — resolve without a caller-supplied Diff (reads both sides via `noteSideLines`). **Additive** to §4.4's list; the stateless HTTP handler has no Diff at hand, and duplicating the old-side table in `internal/web` would fork it.
  - `GET /api/notes?path=&rev=&state=` → `{"notes":[{id,parent_id,source,author,side,line,summary,rationale,status,replies:[…]}]}`
  - `GET /api/notes/counts` → `{"by_path":{},"by_commit":{},"by_commit_path":{}}`
  - `POST /api/notes/add|edit|reply|remove` (all `writeGuard`ed)
  - JS: `fetchNotes()`, `noteRowsFor(side, no)`, `refreshNoteCounts()`
- Consumes: `RegisterRoutes` (`routereg.go:29`), `writeGuard`, `isGitArgSafe` (`server.go`), `writeErr`/`writeJSON`, `s.liveHubRef().emit(liveMsg{…})`, `openPrompt({title, body, onSubmit})` (`layers.js:198`), `runOnce` (`core.js:293`).

- [ ] **Step 1: Write the failing tests**

`internal/web/notes_test.go` (SERIAL — `t.Setenv` isolates the state dir):

```go
package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type wireNote struct {
	ID        string     `json:"id"`
	ParentID  string     `json:"parent_id"`
	Source    string     `json:"source"`
	Author    string     `json:"author"`
	Side      string     `json:"side"`
	Line      int        `json:"line"`
	Summary   string     `json:"summary"`
	Rationale string     `json:"rationale"`
	Status    string     `json:"status"`
	Replies   []wireNote `json:"replies"`
}

type notesResp struct {
	Notes []wireNote `json:"notes"`
}

func notesServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // never touch the developer's real store
	return serve(t, New(domain.Open(newRepoDir(t, 2))))
}

func TestNotesAddListReplyRemove(t *testing.T) {
	ts := notesServer(t)
	code, _ := postJSONRaw(t, ts, "/api/notes/add",
		`{"path":"f.txt","rev":"","state":"unstaged","side":"new","line":1,"summary":"tighten this","rationale":"why"}`)
	if code != http.StatusOK {
		t.Fatalf("POST /api/notes/add = %d", code)
	}
	var got notesResp
	if code := getJSON(t, ts, "/api/notes?path=f.txt&state=unstaged", &got); code != http.StatusOK {
		t.Fatalf("GET /api/notes = %d", code)
	}
	if len(got.Notes) != 1 || got.Notes[0].Summary != "tighten this" ||
		got.Notes[0].Line != 1 || got.Notes[0].Status != "active" {
		t.Fatalf("listed %+v", got.Notes)
	}
	id := got.Notes[0].ID

	if code, _ := postJSONRaw(t, ts, "/api/notes/reply",
		`{"id":"`+id+`","summary":"agreed"}`); code != http.StatusOK {
		t.Fatalf("reply = %d", code)
	}
	getJSON(t, ts, "/api/notes?path=f.txt&state=unstaged", &got)
	if len(got.Notes) != 1 || len(got.Notes[0].Replies) != 1 {
		t.Fatalf("threading lost: %+v", got.Notes)
	}

	if code, _ := postJSONRaw(t, ts, "/api/notes/edit",
		`{"id":"`+id+`","summary":"edited","rationale":""}`); code != http.StatusOK {
		t.Fatalf("edit = %d", code)
	}
	getJSON(t, ts, "/api/notes?path=f.txt&state=unstaged", &got)
	if got.Notes[0].Summary != "edited" {
		t.Fatalf("edit lost: %+v", got.Notes[0])
	}

	if code, _ := postJSONRaw(t, ts, "/api/notes/remove", `{"id":"`+id+`"}`); code != http.StatusOK {
		t.Fatalf("remove = %d", code)
	}
	getJSON(t, ts, "/api/notes?path=f.txt&state=unstaged", &got)
	if len(got.Notes) != 0 {
		t.Fatalf("remove left %+v", got.Notes)
	}
}

func TestNotesCountsEndpoint(t *testing.T) {
	ts := notesServer(t)
	postJSONRaw(t, ts, "/api/notes/add",
		`{"path":"f.txt","state":"unstaged","side":"new","line":1,"summary":"x"}`)
	var counts struct {
		ByPath       map[string]int `json:"by_path"`
		ByCommit     map[string]int `json:"by_commit"`
		ByCommitPath map[string]int `json:"by_commit_path"`
	}
	if code := getJSON(t, ts, "/api/notes/counts", &counts); code != http.StatusOK {
		t.Fatalf("counts = %d", code)
	}
	if counts.ByPath["f.txt"] != 1 {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestNotesRejectsBadWireValues(t *testing.T) {
	ts := notesServer(t)
	for _, body := range []string{
		`{"path":"f.txt","state":"bogus","side":"new","line":1,"summary":"x"}`,
		`{"path":"f.txt","state":"unstaged","side":"sideways","line":1,"summary":"x"}`,
		`{"path":"--upload-pack=x","state":"unstaged","side":"new","line":1,"summary":"x"}`,
		`{"path":"f.txt","state":"unstaged","side":"new","line":0,"summary":"x"}`,
		`{"path":"f.txt","state":"unstaged","side":"new","line":1,"summary":"  "}`,
	} {
		if code, _ := postJSONRaw(t, ts, "/api/notes/add", body); code != http.StatusBadRequest {
			t.Fatalf("POST %s = %d, want 400", body, code)
		}
	}
	if code := getJSON(t, ts, "/api/notes?path=f.txt&state=nope", nil); code != http.StatusBadRequest {
		t.Fatalf("GET with a bad state = %d, want 400", code)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/web/ -run TestNotes -count=1`
Expected: 404s (no routes) / build failure.

- [ ] **Step 3: The additive domain method**

`internal/domain/notes.go`:

```go
// NotesAt resolves the notes for addr WITHOUT a caller-supplied diff, reading
// both sides itself (the noteSideLines table). It is the stateless callers'
// door — the web handlers and, in phase 2, the CLI — while NotesFor stays the
// zero-extra-read path for a frontend that already holds the diff.
func (s *Service) NotesAt(ctx context.Context, addr model.FileAddress) ([]ResolvedNote, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return nil, err
	}
	mine := make([]model.Note, 0, len(all))
	for _, n := range all {
		if sameNoteTarget(n.Address, addr) {
			mine = append(mine, n)
		}
	}
	if len(mine) == 0 {
		return nil, nil
	}
	oldLines, _ := s.noteSideLines(ctx, addr, model.NoteSideOld)
	newLines, _ := s.noteSideLines(ctx, addr, model.NoteSideNew)
	res := resolveNotes(mine, oldLines, newLines)
	kept := res[:0]
	for _, r := range res {
		if r.Status != model.NoteOrphaned {
			kept = append(kept, r)
		}
	}
	return kept, nil
}
```

- [ ] **Step 4: The handlers**

`internal/web/notes.go`:

```go
package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/notes", s.handleNotes)
		mux.HandleFunc("GET /api/notes/counts", s.handleNoteCounts)
		mux.HandleFunc("POST /api/notes/add", writeGuard(s.handleNoteAdd))
		mux.HandleFunc("POST /api/notes/edit", writeGuard(s.handleNoteEdit))
		mux.HandleFunc("POST /api/notes/reply", writeGuard(s.handleNoteReply))
		mux.HandleFunc("POST /api/notes/remove", writeGuard(s.handleNoteRemove))
	})
}

// noteState is the allowlist of wire state values → model.FileState. Anything
// else is a 400: untrusted params reach git argv through the address.
func noteState(s string) (model.FileState, bool) {
	switch s {
	case "", "unstaged":
		return model.StateUnstaged, true
	case "staged":
		return model.StateStaged, true
	case "untracked":
		return model.StateUntracked, true
	case "commit":
		return model.StateCommitted, true
	}
	return 0, false
}

func noteSide(s string) (model.NoteSide, bool) {
	switch s {
	case "", "new":
		return model.NoteSideNew, true
	case "old":
		return model.NoteSideOld, true
	}
	return "", false
}

// noteAddress builds the address a note hangs off from wire values.
func noteAddress(path, rev, state string) (model.FileAddress, error) {
	st, ok := noteState(state)
	if !ok {
		return model.FileAddress{}, errors.New("state must be unstaged, staged, untracked or commit")
	}
	if path == "" {
		return model.FileAddress{}, errors.New("path is required")
	}
	if !isGitArgSafe(path) || (rev != "" && !isGitArgSafe(rev)) {
		return model.FileAddress{}, errors.New("invalid path/rev")
	}
	if st == model.StateCommitted && rev == "" {
		return model.FileAddress{}, errors.New("a commit note needs a rev")
	}
	return model.FileAddress{State: st, Commit: rev, Path: path}, nil
}

type wireNote struct {
	ID        string     `json:"id"`
	ParentID  string     `json:"parent_id,omitempty"`
	Source    string     `json:"source"`
	Author    string     `json:"author,omitempty"`
	Side      string     `json:"side"`
	Line      int        `json:"line"`
	Summary   string     `json:"summary"`
	Rationale string     `json:"rationale,omitempty"`
	Status    string     `json:"status"`
	Replies   []wireNote `json:"replies,omitempty"`
}

func toWireNote(r domain.ResolvedNote) wireNote {
	w := wireNote{
		ID: r.Note.ID, ParentID: r.Note.ParentID, Source: string(r.Note.Source),
		Author: r.Note.Author, Side: string(r.Note.Side), Line: r.Range[1],
		Summary: r.Note.Summary, Rationale: r.Note.Rationale, Status: string(r.Status),
	}
	for _, rep := range r.Replies {
		w.Replies = append(w.Replies, toWireNote(rep))
	}
	return w
}

func (s *Server) handleNotes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	addr, err := noteAddress(q.Get("path"), q.Get("rev"), q.Get("state"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.service().NotesAt(r.Context(), addr)
	if err != nil && !errors.Is(err, domain.ErrNotesDisabled) {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]wireNote, 0, len(res))
	for _, n := range res {
		out = append(out, toWireNote(n))
	}
	writeJSON(w, map[string]any{"notes": out})
}

func (s *Server) handleNoteCounts(w http.ResponseWriter, r *http.Request) {
	c, err := s.service().NoteCounts(r.Context())
	if err != nil && !errors.Is(err, domain.ErrNotesDisabled) {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{
		"by_path": c.ByPath, "by_commit": c.ByCommit, "by_commit_path": c.ByCommitPath,
	})
}

type noteReq struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Rev       string `json:"rev"`
	State     string `json:"state"`
	Side      string `json:"side"`
	Line      int    `json:"line"`
	Summary   string `json:"summary"`
	Rationale string `json:"rationale"`
	Author    string `json:"author"`
}

func decodeNoteReq(w http.ResponseWriter, r *http.Request) (noteReq, bool) {
	var req noteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return noteReq{}, false
	}
	return req, true
}

func (s *Server) handleNoteAdd(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeNoteReq(w, r)
	if !ok {
		return
	}
	addr, err := noteAddress(req.Path, req.Rev, req.State)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	side, sok := noteSide(req.Side)
	summary := strings.TrimSpace(req.Summary)
	if !sok || req.Line < 1 || summary == "" {
		writeErr(w, http.StatusBadRequest, errors.New("side, a 1-based line and a summary are required"))
		return
	}
	n := model.Note{
		Source: model.NoteSourceUser, Author: strings.TrimSpace(req.Author), Address: addr,
		Side: side, Range: [2]int{req.Line, req.Line},
		Summary: summary, Rationale: strings.TrimSpace(req.Rationale),
	}
	// ContextHash is left empty on purpose: domain fills it from the side text
	// (the browser has the rendered row, but the server is the authority here).
	got, err := s.service().NoteAdd(r.Context(), n)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.emitNotes()
	writeJSON(w, map[string]any{"id": got.ID})
}

func (s *Server) handleNoteEdit(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeNoteReq(w, r)
	if !ok {
		return
	}
	if req.ID == "" || strings.TrimSpace(req.Summary) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id and a summary are required"))
		return
	}
	if err := s.service().NoteEdit(r.Context(), req.ID,
		strings.TrimSpace(req.Summary), strings.TrimSpace(req.Rationale)); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.emitNotes()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleNoteReply(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeNoteReq(w, r)
	if !ok {
		return
	}
	summary := strings.TrimSpace(req.Summary)
	if req.ID == "" || summary == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id and a summary are required"))
		return
	}
	got, err := s.service().NoteReply(r.Context(), req.ID, model.Note{
		Source: model.NoteSourceUser, Author: strings.TrimSpace(req.Author),
		Summary: summary, Rationale: strings.TrimSpace(req.Rationale),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.emitNotes()
	writeJSON(w, map[string]any{"id": got.ID})
}

func (s *Server) handleNoteRemove(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeNoteReq(w, r)
	if !ok {
		return
	}
	if req.ID == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id is required"))
		return
	}
	if err := s.service().NoteRemove(r.Context(), req.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.emitNotes()
	writeJSON(w, map[string]any{"ok": true})
}

// emitNotes tells every open page that notes changed. "notes" is NOT in
// liveSources: the ticker never polls it, this is the only producer.
func (s *Server) emitNotes() {
	if h := s.liveHubRef(); h != nil {
		h.emit(liveMsg{Changed: []string{"notes"}, Reason: "notes"})
	}
}
```

- [ ] **Step 5: The client**

`internal/web/static/core.js`, in the `state` literal:

```js
  notes: [],                 // resolved notes for the open diff (GET /api/notes)
  noteCounts: { by_path: {}, by_commit: {}, by_commit_path: {} },
  notesAgentOff: false,      // the TUI's `a`: hide agent-written notes
  diffRow: null,             // {side, no} — the clicked diff row `c` anchors on
```

`internal/web/static/files.js`:

```js
// --- review notes -----------------------------------------------------------

// noteQuery builds the /api/notes query for the open diff. Working-tree diffs
// carry their section as `state`; a commit diff carries rev + state=commit.
function noteQuery() {
  if (!state.diffCtx) return null;
  const q = new URLSearchParams({ path: state.diffCtx.path });
  if (state.diffCtx.rev) {
    q.set("rev", state.diffCtx.rev);
    q.set("state", "commit");
  } else {
    q.set("state", state.diffCtx.state || "unstaged");
  }
  return q;
}


async function fetchNotes(rerender = true) {
  const q = noteQuery();
  if (!q) {
    state.notes = [];
    return;
  }
  try {
    const d = await getJSON("/api/notes?" + q);
    state.notes = d.notes || [];
  } catch {
    state.notes = []; // notes are best-effort; never break the diff
  }
  if (rerender && state.lastDiff) renderDiff(state.lastDiff);
}


async function refreshNoteCounts() {
  try {
    const c = await getJSON("/api/notes/counts");
    state.noteCounts = {
      by_path: c.by_path || {},
      by_commit: c.by_commit || {},
      by_commit_path: c.by_commit_path || {},
    };
  } catch {
    /* counts are decoration */
  }
  renderFiles();
}


// noteRowsHTML renders the <tr class="note"> rows anchored on (side, no).
// Agent notes are skipped while the agent layer is off; user notes always show.
function noteRowsHTML(side, no, cols) {
  if (!no || !state.notes.length) return "";
  let html = "";
  for (const n of state.notes) {
    if (n.side !== side || n.line !== no) continue;
    if (state.notesAgentOff && n.source === "agent") continue;
    html += noteRowHTML(n, 0, cols);
    for (const r of n.replies || []) html += noteRowHTML(r, 1, cols);
  }
  return html;
}


function noteRowHTML(n, depth, cols) {
  const cls = "note" + (depth ? " reply" : "") + (n.status === "stale" ? " stale" : "");
  const head = (depth ? "↳ " : "◆ ") + (n.author ? n.author + ": " : "") + n.summary;
  const body = n.rationale ? `<div class="note-rationale">${esc(n.rationale)}</div>` : "";
  return `<tr class="${cls}" data-note="${esc(n.id)}"><td class="note" colspan="${cols}">${esc(head)}${body}</td></tr>`;
}
```

In `diffHTML`, every row gains `data-side`/`data-no` attributes, a `cur` class
when it is the clicked row, and its note rows after it. The side-by-side branch
in full (the other two are the same edit with `cols` = 2 for pure add/del and 3
for unified, and `side`/`no` taken from whichever side that layout shows):

```js
    for (const r of rows) {
      const cur = state.diffRow && state.diffRow.side === "new" && state.diffRow.no === r.right_no ? " cur" : "";
      html +=
        `<tr class="${r.kind}${hunkCls(r)}${cur}"${hunkAttr(r)} data-side="new" data-no="${r.right_no || ""}">` +
        `<td class="no l">${r.left_no || ""}</td>` +
        `<td class="side l">${renderCell(r.left, r.left_spans, r.left_tok, "l")}</td>` +
        `<td class="no r">${r.right_no || ""}</td>` +
        `<td class="side r">${renderCell(r.right, r.right_spans, r.right_tok, "r")}</td></tr>` +
        noteRowsHTML("new", r.right_no, 4) +
        noteRowsHTML("old", r.left_no, 4);
    }
```

Add one delegated listener next to the existing hunk click handler:

```js
$("diff-body").addEventListener("click", (e) => {
  const tr = e.target.closest("tr[data-no]");
  if (!tr) return;
  state.diffRow = { side: tr.dataset.side, no: Number(tr.dataset.no) };
  if (state.lastDiff) renderDiff(state.lastDiff);
});
```

In `renderFiles`, the working-tree branch's row template gains the badge:

```js
    const notes = state.noteCounts.by_path[f.path] || 0;
    const noteBadge = notes ? `<span class="notebadge">◆${notes}</span>` : "";
```

appended after the path span (and the same for the commit-file branch using
`by_commit_path[state.fileSha + ":" + f.path]`).

Both diff-open paths record the provenance so `noteQuery` can name it, and both
load the notes BEFORE the first paint (so the rows are there immediately):

```js
// openStatusDiff (files.js:462) — the working-tree diff:
  state.diffCtx = f.section === "conflicts" ? null
    : { path: f.path, rev: "", state: f.section === "staged" ? "staged" : "unstaged" };
  …
  await fetchNotes(false);          // then renderDiff(await getJSON(...)) as today

// openFile's commit branch (files.js:440) — the commit diff:
  state.diffCtx = { path: f.path, rev: state.filesMode === "compare" ? state.compare.bHash : f.sha || state.fileSha, state: "commit" };
  …
  await fetchNotes(false);
```

A compare view has two revisions and no single provenance: leave
`state.diffCtx.state` as `"commit"` against the RIGHT-hand hash, exactly as the
existing history/blame buttons already treat `diffCtx.rev`.

`internal/web/static/keys.js` — after the `o` branch (all six keys are free in
this handler; `p`/`P`/`r`/`s`/`u`/`m`/`o` are the taken ones):

```js
  } else if (e.key === "c" && state.diffCtx) {
    addNotePrompt();
  } else if (e.key === "E" && state.diffCtx) {
    editNotePrompt();
  } else if (e.key === "R" && state.diffCtx) {
    replyNotePrompt();
  } else if (e.key === "a" && state.diffCtx) {
    state.notesAgentOff = !state.notesAgentOff;
    if (state.lastDiff) renderDiff(state.lastDiff);
  } else if ((e.key === "}" || e.key === "{") && state.diffCtx) {
    stepNote(e.key === "}" ? 1 : -1);
  }
```

with the four helpers in `files.js` (imported by `keys.js`):

```js
// addNotePrompt asks for a summary + optional rationale (the prompt's two-field
// shape) and anchors the note on the clicked row, else the first changed row.
function addNotePrompt() {
  const at = state.diffRow || firstChangedRow();
  if (!at) return;
  openPrompt({
    title: `Add note on line ${at.no}`,
    body: { label: "rationale (optional)" },
    onSubmit: (summary, rationale) => {
      const q = noteQuery();
      if (!q) return;
      runOnce("note-write", async () => {
        await postJSON("/api/notes/add", {
          path: q.get("path"), rev: q.get("rev") || "", state: q.get("state"),
          side: at.side, line: at.no, summary, rationale,
        });
        await Promise.all([fetchNotes(), refreshNoteCounts()]);
      });
    },
  });
}
```

`editNotePrompt`/`replyNotePrompt` are the same shape against
`/api/notes/edit` and `/api/notes/reply`, targeting `nearestNote()` (the last
note at or above `state.diffRow`); `stepNote(dir)` scrolls `#diff-body` to the
next/previous `tr.note` and sets `state.diffRow` from its anchor row.
A `data-note` right-click row in the diff's context menu removes it via
`/api/notes/remove` (the `◆` row's menu), mirroring the TUI's `.`-menu row.

`internal/web/static/live.js` — in `refreshSources`:

```js
  if (want.has("notes")) jobs.push(fetchNotes(), refreshNoteCounts());
```

(add `fetchNotes, refreshNoteCounts` to the existing `./files.js` import.)

`internal/web/static/style.css` — the per-id/per-class rules (never a bare
`.hidden`):

```css
tr.note td.note { color: var(--fg-note, #7aa2f7); padding-left: .6rem; white-space: pre-wrap; }
tr.note.reply td.note { padding-left: 2rem; }
tr.note.stale td.note { color: var(--fg-dim, #777); }
tr.note .note-rationale { opacity: .8; }
tr.cur > td { background: var(--bg-cur, #2a2f3a); }
.notebadge { margin-left: .4rem; opacity: .8; }
```

- [ ] **Step 6: Run the tests + a live probe**

```bash
go test ./internal/web/ ./internal/domain/ -count=1
gofmt -l internal/ && go vet ./internal/web/ ./internal/domain/
node --check internal/web/static/files.js && node --check internal/web/static/keys.js && node --check internal/web/static/live.js
```

Then verify in a browser against a REBUILT binary (the `playwright-web-verification`
memory: rebuild → restart → hard reload → confirm the server is yours with
`curl -s localhost:<port>/api/repo`): open a working-tree diff, press `c`, save
a note, confirm the `◆` row appears without a manual refresh (the SSE arm) and
that the files list shows `◆1`.

- [ ] **Step 7: Commit**

```bash
git add internal/web/ internal/domain/notes.go
git commit -m "feat(web): /api/notes CRUD, notes SSE event, inline note rows and badges

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 10: docs + full gates

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, `CLAUDE.md`

- [ ] **Step 1: CHANGELOG**

Under `## [Unreleased]`, as the first bullet:

```markdown
- **Review notes (phase 1).** Anchor a note to a diff line and it persists —
  per repo, machine-local, outside git. In the diff view `c` adds one at the
  cursor line (summary + optional rationale), `E` edits and `R` replies to the
  nearest note above the cursor, `a` hides or shows agent-written notes (your
  own always stay visible), `}`/`{` jump between annotated lines — expanding a
  fold when the note hides under one, and stepping to the next file that
  carries notes on a second press — and the `.` menu deletes one. Notes render
  as `◆` rows under their line, greyed when the line they were written about
  has changed; the Files and Commits panels show a `◆N` badge. `gg web` mirrors
  all of it over `/api/notes` with live updates. Notes re-anchor by a
  fingerprint of the lines they were written on, so they follow the code as it
  moves; a background sweep at every gg start drops notes older than
  `[notes] max_age_days` (default 30, `-1` keeps forever) and notes whose
  anchor is gone, and `[notes] max_entries` (default 2000) caps the store.
```

- [ ] **Step 2: README**

In the diff-view key list (the `enter` row, ~line 56), after the `e` clause:
"`c` adds a **review note** at the cursor line, `E`/`R` edit or reply to the nearest one, `a` hides agent notes, `}`/`{` jump between annotated lines,".

In `## Configuration`, after the `[versions]` paragraph:

```markdown
`[notes] max_age_days` (default `30`) and `[notes] max_entries` (default
`2000`) bound the review-note store. Notes live outside git, per repo, under
your XDG state dir — they never travel with a branch. A background sweep at
every gg start drops notes past the age limit and notes whose anchored lines
are gone; `-1` in either key means "keep forever" / "uncapped".
```

- [ ] **Step 3: `docs/CLAUDE-details.md`**

One paragraph after the diff-cursor paragraph: the note record and fingerprint
(`model.NoteContextHash`, trimmed lines joined with `\n`); `internal/notes` =
locked TOML store (O_EXCL lock, 30 s stale takeover, cap on write, oldest root
first, `Load` never writes); domain owns resolution (`resolveNotes`/`resolveOne`
— hash at range → moved by outward scan → stale clamped → orphaned; the
old-side base table by `FileState`); `NotesFor` (frontend holds the diff) vs
`NotesAt` (stateless callers); `NoteCounts` cached + invalidated on every
mutation, `ByCommitPath` keyed `sha:path` for `}`/`{` file stepping; the sweep
is per-Service `sync.Once`, started where frontends apply config policy;
`dRow.note` is one DISPLAY row, `cursorDispRange` stops before the note rows,
a folded note marks its fold rule; `srcNotes` is never polled.

- [ ] **Step 4: `CLAUDE.md` package map**

One row, alphabetically placed next to `bookmark`/`shelf`:

```markdown
| `notes`      | Machine-local review-note store (TOML + O_EXCL lock, write-time cap, `Sweep`); records only. Owned by `domain`; frontends never import it. |
```

- [ ] **Step 5: Full gates**

```bash
gofmt -l internal/ && go vet ./...
./test.sh unit 2>&1 | tail -5
./test.sh race 2>&1 | tail -5      # on a quiet machine (nohup + poll, per the memory)
go build -o /tmp/claude-1000/-mnt-t-others-gigagit/a145f667-53f5-4103-95d2-a26fb6444dcf/scratchpad/gg-notes ./cmd/gg
```

Then drive the TUI headlessly (`driving-tui-headless`): open a Files-panel diff,
press `c`, type a summary, `ctrl+s`, then `}`, `a`, `.` — eyeball the snapshots
for the `◆` row, the badge, the footer hint and the Delete-note menu row. Hand
the built binary to the user (absolute path), per the always-deliver-a-verify-binary rule.

- [ ] **Step 6: Commit**

```bash
git add CHANGELOG.md README.md docs/CLAUDE-details.md CLAUDE.md
git commit -m "docs(notes): CHANGELOG, README configuration + diff keys, details, package map

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

## Self-review

**Spec coverage (§4.4 bullet → task):**

| §4.4 bullet | Task |
|---|---|
| Record table (`ID`…`Created/Updated`), reply inherits the anchor | 1 (record), 4 (`NoteReply` inheritance) |
| Old-side base per `Address.State` | 5 (`noteSideLines`) — **Ruling 2 corrects the spec's state names** |
| Fingerprint: trimmed lines, `\n`-joined, hex sha256 | 1 (`model.NoteContextHash`) |
| Resolution: active / moved (outward scan) / stale (clamped) / orphaned (hidden) | 4 (`resolveOne`, `findAnchor`, `NotesFor` drops orphans) |
| Store interface `Load/Put/Remove/Sweep` + `Policy` | 3 |
| `FileStore(root)`, `notes.toml` under `<state>/gg/notes/<repoKey>/`, temp+rename, O_EXCL lock (2 s retry, 30 s stale), process mutex, re-read under lock, cap oldest-first with replies, `Load` never writes | 3 (store), 4 (`notesBaseDir`/`notesStore`) |
| `notes.Now` clock seam | 3 |
| `[notes] max_age_days` / `max_entries`, overlay, two settingDocs, section loop, `TestNotesLayers` | 2 |
| Sweep: goroutine at service construction, resolve every note, `keep = !expired && active`, once per process, errors to the session ring | 5 — **Ruling 8: started explicitly from the three config-apply sites, per-Service `sync.Once`** |
| `ResolvedNote`, `NoteAdd/Edit/Reply/Remove`, `NotesFor`, `NoteCounts` | 4 (+ `NotesAt`, additive, Task 9) |
| `NoteCounts` cached, invalidated by every mutation; painters never touch the store | 4 (+ Ruling 1's third map, Ruling 10) |
| `srcNotes` refresh source, fired after each mutation; `opAffectedSources` untouched | 7 |
| Note rows appended in `relayout`, `◆ author: summary`, rationale row, indented replies, stale grey, agent layer | 6 |
| Cursor never rests on a note row; `cursorDispRange` ends at the first note row; click maps to the owning line; page keys unchanged | 6 |
| `◆` on the fold separator in partial mode | 6 |
| Keys `c`/`E`/`R`/`a`/`}`/`{` + fold expansion + `fileArm` file step; `.`-menu Delete note; footer, help, four bundles | 7 |
| Files + Commits `◆N` badges | 8 |
| Web: `GET /api/notes`, four POSTs, allowlisted wire values, `notes` SSE event, `<tr class="note">`, keys, `◆N` in `renderFiles`, per-id hidden rule | 9 |
| Testing: store round-trip/cap/lock, resolution table, domain on a real repo, TUI relayout/cursor/`}`-`{`/popup, web handlers; e2e deferred to phase 2 | 3, 4, 5, 6, 7, 8, 9 |

**Deviations from decided text** (both flagged in Rulings, both additive):
1. `NoteCounts.ByCommitPath` — required by §4 item 2's cross-file hop on commit diffs; `ByPath`/`ByCommit` unchanged.
2. `domain.NotesAt` — a second door for callers with no Diff (web now, CLI in phase 2); `NotesFor`'s decided signature is untouched.
3. §4.4's old-side wording (`FileStateModified`) names a state this codebase does not have; Ruling 2's table is what the code will do.

**Placeholders:** none — every step carries the real code or the exact edit, against signatures read from the tree (`diffView`/`dRow`/`relayout`/`diffPaneLines`/`foldSeparator`/`cursorDispRange`/`statusList.Row`/`commitIdentRowAt`/`readSourceCmd`/`applyUIPolicies`/`openPrompt`/`refreshSources`). Two helpers are named-but-deferred by design and their construction is spelled out in prose where they appear: `diffFileSequence` (Task 7, Step 3 note) and the `editNotePrompt`/`replyNotePrompt`/`stepNote` twins of `addNotePrompt` (Task 9, Step 5).

**Type consistency:** `model.Note`/`NoteSide`/`NoteSource`/`NoteStatus` (Task 1) are used unchanged in Tasks 3–9. `notes.Store` (with `SetPolicy`) is implemented once (Task 3) and consumed only through `Service.notesStore` (Task 4). `domain.ResolvedNote` crosses into `internal/tui` (Tasks 6–8) and `internal/web` (Task 9) — both frontends import `domain`, never `internal/notes` (archtest). `noteLine` is TUI-private; `wireNote` is web-private. `foldSeparator(n, w, marked)` has exactly one caller (Task 6). `srcNotes` appears in `source.go`, `model.go`, `i18n_display.go` and the Task 7 test, and is deliberately absent from `refresh.go`'s `scheduledItems`.

**Key freedom verified** against `updateDiffViewKey`'s switch as it stands at `58dc0a3b`: used are `. g G F esc h b e up down k alt+up j alt+down pgup pgdown z home end n ctrl+down p ctrl+up N P f ctrl+w left right 0` — `c`, `E`, `R`, `a`, `}`, `{` are all free. In the web's global `keydown`, `p P r s u m o g t / # Enter Escape j k` are taken; `c E R a } {` are free.

**Known limitations (accepted, not defects):** the Files-panel `◆N` badge is
trailing (§4.4's wording), so a path long enough to be truncated by the panel
loses its badge — the diff view still shows the notes; `NoteCounts` is
unresolved (Ruling 10), so a badge can outlive an orphaned note until the next
gg start; and `}`/`{` file stepping walks the nav list one plain step at a time
until it reaches an annotated file, so a long run of unannotated files costs a
few reopens (each is a cached diff).

**Verification gates:** `./test.sh unit` at Task 10 Step 5, `./test.sh race` before the merge request, the four i18n AST gates after Tasks 6 and 7, `TestSettingDocsCoverAllFields` after Task 2, `node --check` on the three touched JS modules, a headless TUI capture and a browser probe against a rebuilt binary.

