# Shelf Naming Unification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make shelf entries read like bookmarks by storing a shared structured `model.FileAddress` (worktree/branch/commit/state) as each entry's origin and rendering it via one shared `Display()`.

**Architecture:** Introduce `model.FileAddress` + `Display()`/`FileRef()` and rename the state enum `BookmarkState`→`FileState`. Replace `ShelfEntry`'s bare `Source string`+`Path string` with a single `Origin FileAddress`; the shelf store and `domain.ShelfAdd` capture it; the TUI shelf-add reuses `focusedBookmark()` and both frontends render `Origin.Display()`.

**Tech Stack:** Go 1.26, Bubble Tea. No new dependencies.

## Global Constraints

- Work in the existing worktree on branch `worktree-shelf-naming-unify` (off `main` tip `338a24e`). Use worktree-relative paths only — absolute `/mnt/t/others/gigagit/...` paths land in the shared checkout.
- **No backward compatibility / migration** — there is no shelf data in use; change the schema cleanly.
- The shelf entry ID format stays byte-identical: `<source-word>-<slug(path)>-<sha8>` where source-word is `committed→Commit`, `staged→"staged"`, else `"unstaged"`. (e2e `s43_shelf_roundtrip.toml` hardcodes `unstaged-readme-md-81db67b6` — must keep passing.)
- `internal/tui` and `internal/cli` must not import `internal/git`/`internal/shelf`/`internal/bookmark` (archtest-guarded); this change touches none of those imports.
- Run `./test.sh race` before declaring done. Commit messages end with the repo's Co-Authored-By / Claude-Session trailers.

---

### Task 1: Shared address type in `model` (rename + `FileAddress`)

Add the shared `model.FileAddress` value (display + ref mapping), the `Bookmark.Address()` adapter, and rename `BookmarkState`→`FileState`. Purely additive + a type rename — the whole tree still compiles (`ShelfEntry` is untouched here).

**Files:**
- Modify: `internal/model/model.go`
- Test: `internal/model/fileaddress_test.go` (create)

**Interfaces:**
- Produces: `model.FileState` (renamed from `BookmarkState`); `model.FileAddress{Worktree,Branch,Commit,ShelfID,Path string; State FileState}`; `func (a FileAddress) Display() string`; `func (a FileAddress) FileRef() FileRef`; `func (b Bookmark) Address() FileAddress`.

- [ ] **Step 1: Write the failing test**

Create `internal/model/fileaddress_test.go`:

```go
package model

import "testing"

func TestFileAddressDisplay(t *testing.T) {
	cases := []struct {
		name string
		a    FileAddress
		want string
	}{
		{"committed", FileAddress{State: StateCommitted, Branch: "feat", Commit: "a1b2c3d4e5", Path: "src/x.go"}, "feat / a1b2c3d / src/x.go"},
		{"committed-no-branch", FileAddress{State: StateCommitted, Commit: "a1b2c3d4e5", Path: "x.go"}, "commit / a1b2c3d / x.go"},
		{"shelf", FileAddress{State: StateShelf, ShelfID: "id1", Path: "x.go"}, "shelf / shelf / x.go"},
		{"unstaged", FileAddress{State: StateUnstaged, Worktree: "/home/u/repo", Path: "a/b.go"}, "wt:repo / unstaged / a/b.go"},
		{"staged", FileAddress{State: StateStaged, Worktree: "/home/u/repo", Path: "a/b.go"}, "wt:repo / staged / a/b.go"},
	}
	for _, c := range cases {
		if got := c.a.Display(); got != c.want {
			t.Errorf("%s: Display()=%q want %q", c.name, got, c.want)
		}
	}
}

func TestFileAddressFileRef(t *testing.T) {
	cases := []struct {
		a    FileAddress
		want FileRef
	}{
		{FileAddress{State: StateUnstaged, Path: "p"}, FileRef{Source: SourceUnstaged, Path: "p"}},
		{FileAddress{State: StateUntracked, Path: "p"}, FileRef{Source: SourceUnstaged, Path: "p"}},
		{FileAddress{State: StateStaged, Path: "p"}, FileRef{Source: SourceStaged, Path: "p"}},
		{FileAddress{State: StateCommitted, Commit: "abc", Path: "p"}, FileRef{Source: SourceCommit, Locator: "abc", Path: "p"}},
		{FileAddress{State: StateShelf, ShelfID: "id", Path: "p"}, FileRef{Source: SourceShelf, Locator: "id", Path: "p"}},
	}
	for _, c := range cases {
		if got := c.a.FileRef(); got != c.want {
			t.Errorf("FileRef(%+v)=%+v want %+v", c.a, got, c.want)
		}
	}
}

func TestBookmarkAddressRoundTrip(t *testing.T) {
	b := Bookmark{Worktree: "/wt", Branch: "b", Commit: "c", ShelfID: "s", Path: "p", State: StateCommitted}
	a := b.Address()
	if a.Worktree != "/wt" || a.Branch != "b" || a.Commit != "c" || a.ShelfID != "s" || a.Path != "p" || a.State != StateCommitted {
		t.Fatalf("Address() lost a field: %+v", a)
	}
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./internal/model/ -run 'TestFileAddress|TestBookmarkAddress' -v`
Expected: FAIL — build error, `FileAddress` / `Address` undefined.

- [ ] **Step 3: Rename the enum**

In `internal/model/model.go`, rename the type `BookmarkState`→`FileState` in its declaration, its `const` block doc, and its `String()` receiver. Update `Bookmark.State`'s field type to `FileState`. (The constants `StateCommitted`…`StateUntracked` keep their names; `internal/domain/bookmarkstore.go`'s `BookmarkStatePath` var is unrelated — do NOT touch it.)

```go
// BookmarkState is where in its git lifecycle a bookmarked file was taken from.
type FileState int

const (
	StateCommitted FileState = iota // a commit/branch file (permanent → SHA)
	StateShelf                      // a shelf entry (permanent → SHA)
	StateStaged                     // a worktree's index file (live)
	StateUnstaged                   // a worktree's working file, tracked-modified (live)
	StateUntracked                  // a worktree's working file, new (live)
)

// String renders the state word used in an address's display string.
func (s FileState) String() string {
	// (body unchanged)
```

Update the `Bookmark` struct's field: `State    FileState`.

- [ ] **Step 4: Add `FileAddress`, `Display`, `FileRef`, `Bookmark.Address`**

Ensure `internal/model/model.go` imports `"fmt"` and `"path/filepath"` (add them to the import block if absent). Then add, after the `Bookmark` type:

```go
// FileAddress is the shared, structured provenance of a file: the identity AND
// the human display behind both a bookmark's address and a shelf entry's origin.
type FileAddress struct {
	Worktree string // working/index/untracked states; "" otherwise
	Branch   string // branch name when known
	Commit   string // commit sha/rev (StateCommitted)
	ShelfID  string // shelf entry id (StateShelf)
	Path     string // path within the tree/worktree
	State    FileState
}

// Display renders "<container> / <state-or-commit> / <path>".
func (a FileAddress) Display() string {
	container := "?"
	switch a.State {
	case StateCommitted:
		container = a.Branch
		if container == "" {
			container = "commit"
		}
	case StateShelf:
		container = "shelf"
	default:
		container = "wt:" + filepath.Base(a.Worktree)
	}
	mid := a.State.String()
	if a.State == StateCommitted && len(a.Commit) >= 7 {
		mid = a.Commit[:7]
	}
	return fmt.Sprintf("%s / %s / %s", container, mid, a.Path)
}

// FileRef maps the address to the byte-resolution ref used by ResolveBytes.
// Byte resolution stays against the service repo; Worktree/Branch are
// display-only provenance.
func (a FileAddress) FileRef() FileRef {
	switch a.State {
	case StateStaged:
		return FileRef{Source: SourceStaged, Path: a.Path}
	case StateCommitted:
		return FileRef{Source: SourceCommit, Locator: a.Commit, Path: a.Path}
	case StateShelf:
		return FileRef{Source: SourceShelf, Locator: a.ShelfID, Path: a.Path}
	default: // StateUnstaged, StateUntracked
		return FileRef{Source: SourceUnstaged, Path: a.Path}
	}
}

// Address builds the FileAddress a bookmark points at.
func (b Bookmark) Address() FileAddress {
	return FileAddress{
		Worktree: b.Worktree, Branch: b.Branch, Commit: b.Commit,
		ShelfID: b.ShelfID, Path: b.Path, State: b.State,
	}
}
```

- [ ] **Step 5: Run model tests + whole build**

Run: `go test ./internal/model/ -v && go build ./...`
Expected: PASS, and `go build ./...` still succeeds (rename is internal to model; `ShelfEntry` unchanged).

- [ ] **Step 6: Commit**

```bash
git add internal/model/model.go internal/model/fileaddress_test.go
git commit -m "feat(model): shared FileAddress + rename BookmarkState→FileState

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 2: `ShelfEntry.Origin` + storage/capture layer

Flip `ShelfEntry` to carry `Origin FileAddress`, and update the shelf store + `domain.ShelfAdd` to capture it. This intentionally breaks the `tui`/`cli` packages (fixed in Task 3); verify with targeted package tests.

**Files:**
- Modify: `internal/model/model.go` (`ShelfEntry`)
- Modify: `internal/shelf/file_store.go` (`Put`, drop `sourceLabel`, add `idSource`)
- Modify: `internal/shelf/file_store_test.go` (`ref`→`addr` helper, `e.Path`→`e.Origin.Path`)
- Modify: `internal/domain/shelf.go` (`ShelfAdd` signature)
- Modify: `internal/domain/shelf_test.go` (pass `FileAddress`, `e.Path`→`e.Origin.Path`)

**Interfaces:**
- Consumes: `model.FileAddress`, `(FileAddress).FileRef()` (Task 1).
- Produces: `ShelfEntry{ID,Bucket string; Origin FileAddress; SHA string; Size int64; Created time.Time}`; `FileStore.Put(bucket string, addr model.FileAddress, data []byte) (model.ShelfEntry, error)`; `Service.ShelfAdd(ctx, addr model.FileAddress, bucket string) (model.ShelfEntry, error)`.

- [ ] **Step 1: Change the `ShelfEntry` struct**

In `internal/model/model.go`, replace the `Source`+`Path` fields:

```go
// ShelfEntry is one shelved file: immutable content plus structured provenance.
type ShelfEntry struct {
	ID      string // "<source-word>-<pathslug>-<shorthash>"
	Bucket  string
	Origin  FileAddress // where it was captured from (provenance + display)
	SHA     string      // content hash; also the blob filename
	Size    int64
	Created time.Time
}
```

- [ ] **Step 2: Update the shelf store**

In `internal/shelf/file_store.go`, replace `sourceLabel` with `idSource` and rewrite `Put`'s signature + entry construction:

```go
func idSource(a model.FileAddress) string {
	switch a.State {
	case model.StateStaged:
		return "staged"
	case model.StateCommitted:
		return a.Commit
	default:
		return "unstaged"
	}
}

func (fs *FileStore) Put(bucket string, addr model.FileAddress, data []byte) (model.ShelfEntry, error) {
	// ... (size check, sha, blob write — UNCHANGED) ...

	e := model.ShelfEntry{
		ID:      fmt.Sprintf("%s-%s-%s", idSource(addr), slug(addr.Path), sha[:8]),
		Bucket:  bucket,
		Origin:  addr,
		SHA:     sha,
		Size:    int64(len(data)),
		Created: time.Now(),
	}
	// ... (idempotent replace/append + write — UNCHANGED) ...
}
```

Delete the old `sourceLabel(ref model.FileRef)` function.

- [ ] **Step 3: Update the shelf store tests**

In `internal/shelf/file_store_test.go`, change the `ref` helper to build a `FileAddress` and rename it `addr`; update every `s.Put("", ref(...), ...)` call to `s.Put("", addr(...), ...)`; change the `e.Path` assertion to `e.Origin.Path`:

```go
func addr(path string) model.FileAddress {
	return model.FileAddress{State: model.StateUnstaged, Path: path}
}
```

(At `file_store_test.go:30`: `if e.Origin.Path != "a/b.go" || e.Size != int64(len(data)) {`.) The ID assertions remain valid: an unstaged `addr("a/b.go")` still yields `unstaged-a-b-go-<sha8>`.

- [ ] **Step 4: Update `domain.ShelfAdd`**

In `internal/domain/shelf.go`:

```go
// ShelfAdd resolves addr's bytes (Read reservation) and stores a frozen copy
// tagged with its structured origin.
func (s *Service) ShelfAdd(ctx context.Context, addr model.FileAddress, bucket string) (model.ShelfEntry, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return model.ShelfEntry{}, ErrShelfDisabled
	}
	data, err := s.ResolveBytes(ctx, addr.FileRef())
	if err != nil {
		return model.ShelfEntry{}, err
	}
	return st.Put(bucket, addr, data)
}
```

- [ ] **Step 5: Update the domain shelf tests**

In `internal/domain/shelf_test.go`, replace the two `ShelfAdd(..., model.FileRef{Source: model.SourceCommit, Locator: "abc", Path: ...}, "")` calls with `ShelfAdd(..., model.FileAddress{State: model.StateCommitted, Commit: "abc", Path: ...}, "")`, and change the `e.Path` check (line ~30) to `e.Origin.Path`. The `ResolveBytes(... model.FileRef{Source: model.SourceShelf, Locator: e.ID, Path: "p.go"})` call (line ~73) stays a `FileRef` — unchanged.

- [ ] **Step 6: Run the targeted package tests**

Run: `go test ./internal/model/ ./internal/shelf/ ./internal/domain/ -v 2>&1 | tail -20`
Expected: PASS. (`go build ./...` will FAIL here because `tui`/`cli` still reference `e.Source`/`e.Path` — that's expected and fixed in Task 3.)

- [ ] **Step 7: Commit**

```bash
git add internal/model/model.go internal/shelf/ internal/domain/shelf.go internal/domain/shelf_test.go
git commit -m "feat(shelf): store structured FileAddress origin per entry

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 3: Frontends — capture via `focusedBookmark`, render `Origin.Display()`

Update the TUI and CLI to the new shape, restoring a full `go build ./...`. TUI shelf-add reuses `focusedBookmark()`; both frontends display `Origin.Display()`.

**Files:**
- Modify: `internal/tui/shelf.go` (`shelfRows`, add `focusedShelfAddress`, `shelfAddCmd`, `shelfAddRow`; remove `focusedShelfRef`, and `commitOrWorktreeRef` if unused)
- Modify: `internal/tui/shelf_actions.go` (`e.Path`→`e.Origin.Path`)
- Modify: `internal/tui/bookmark_popup.go` (`bookmarkDisplay` delegates to `Address().Display()`)
- Modify: `internal/tui/shelf_test.go` (literals → `Origin`; capture-test → `focusedShelfAddress`)
- Modify: `internal/cli/shelf.go` (`shelfAdd` builds address; `shelfList` prints `Origin.Display()`)

**Interfaces:**
- Consumes: `model.FileAddress`, `(FileAddress).Display()`, `(Bookmark).Address()`, `Service.ShelfAdd(ctx, addr, bucket)`, `m.focusedBookmark() (model.Bookmark, bool)`, `svc.TopLevel(ctx)`, `svc.CurrentBranch(ctx)`.
- Produces: `m.focusedShelfAddress() (model.FileAddress, bool)`.

- [ ] **Step 1: Update TUI shelf-add + render tests**

In `internal/tui/shelf_test.go`:
- `TestShelfRowsContent`: change literals to `Origin` and drop the sha assertion:

```go
m.shelfEntries = []model.ShelfEntry{
	{ID: "unstaged-a-b-aabbccdd", Origin: model.FileAddress{State: model.StateUnstaged, Worktree: "/repo", Path: "a/b.go"}, SHA: "aabbccddeeff"},
	{ID: "staged-readme-11223344", Origin: model.FileAddress{State: model.StateStaged, Worktree: "/repo", Path: "README.md"}, SHA: "1122334455"},
}
rows := m.shelfRows()
if len(rows) != 2 {
	t.Fatalf("shelfRows len = %d", len(rows))
}
if !strings.Contains(rows[0], "a/b.go") || !strings.Contains(rows[0], "unstaged") {
	t.Fatalf("row0 missing fields: %q", rows[0])
}
if !strings.Contains(rows[1], "README.md") || !strings.Contains(rows[1], "staged") {
	t.Fatalf("row1 missing fields: %q", rows[1])
}
```

- `TestAddToShelfRowOnFilesPanel`: assert via `focusedShelfAddress` (set the worktree like the bookmark test):

```go
func TestAddToShelfRowOnFilesPanel(t *testing.T) {
	m := filesMenuModel() // panelFiles focused with one tracked file "dir/f.txt"
	m.currentWorktree = "/wt"
	if _, ok := findRow(availableActions(m), "shelf-add"); !ok {
		t.Fatalf("Add to shelf missing from menu on Files panel")
	}
	a, ok := m.focusedShelfAddress()
	if !ok || a.State != model.StateUnstaged || a.Path != "dir/f.txt" || a.Worktree != "/wt" {
		t.Fatalf("focusedShelfAddress = %+v ok=%v", a, ok)
	}
}
```

- `TestAddToShelfRowAbsentWhenNoFileFocused`: replace `m.focusedShelfRef()` with `m.focusedShelfAddress()`.
- `shelfTabModel` + any other `model.ShelfEntry{... Path:..., Source:...}` literals in this file: change to `Origin: model.FileAddress{State: model.StateUnstaged, Path: ...}` (drop `Source`/`Path` fields). Search the file for `Source:` / `Path:` inside `ShelfEntry` literals and convert each.

- [ ] **Step 2: Run those tests to confirm they fail**

Run: `go test ./internal/tui/ -run 'TestShelfRowsContent|TestAddToShelf' 2>&1 | tail -15`
Expected: FAIL — build error (`focusedShelfAddress` undefined / `ShelfEntry` has no `Source`).

- [ ] **Step 3: Rewrite `internal/tui/shelf.go`**

Replace `shelfRows`, `focusedShelfRef`/`commitOrWorktreeRef`, `shelfAddCmd`, and `shelfAddRow`:

```go
// shelfRows renders one bookmark-style "<container> / <state> / <path>" line
// per shelf entry from its stored origin.
func (m Model) shelfRows() []string {
	rows := make([]string, len(m.shelfEntries))
	for i, e := range m.shelfEntries {
		rows[i] = e.Origin.Display()
	}
	return rows
}
```

Delete `focusedShelfRef` and `commitOrWorktreeRef` (the latter only existed for `focusedShelfRef`). Add the capture wrapper reusing the bookmark capture, and update the command + row:

```go
// focusedShelfAddress resolves the file under focus to a FileAddress, reusing
// the bookmark capture (same surfaces/precedence). A shelf entry can't be
// re-shelved, so StateShelf is rejected.
func (m Model) focusedShelfAddress() (model.FileAddress, bool) {
	b, ok := m.focusedBookmark()
	if !ok || b.State == model.StateShelf {
		return model.FileAddress{}, false
	}
	return b.Address(), true
}

// shelfAddCmd freezes addr's bytes into the default bucket off the UI thread.
func (m Model) shelfAddCmd(addr model.FileAddress) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		e, err := svc.ShelfAdd(context.Background(), addr, "")
		return shelfAddedMsg{entry: e, err: err}
	}
}

// shelfAddRow is the menu-only "Add to shelf" action wherever a single file is
// focused. It captures the resolved address at build time.
func (m Model) shelfAddRow() (actionRow, bool) {
	addr, ok := m.focusedShelfAddress()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "shelf-add",
		label: "Add to shelf",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.shelfAddCmd(addr)
		},
	}, true
}
```

After editing, if `"fmt"` is no longer referenced in `shelf.go`, remove it from the imports. (`gofmt`/`go vet` will flag an unused import.)

- [ ] **Step 4: Fix `shelf_actions.go` and `bookmark_popup.go`**

In `internal/tui/shelf_actions.go`, the `shelfRestorePopup` construction: change `origin: e.Path` → `origin: e.Origin.Path`.

In `internal/tui/bookmark_popup.go`, replace the `bookmarkDisplay` body with a delegation:

```go
// bookmarkDisplay builds "<container> / <commit-or-state> / <path>".
func bookmarkDisplay(b model.Bookmark) string { return b.Address().Display() }
```

If `"fmt"` and/or `"path/filepath"` become unused in `bookmark_popup.go` after this, remove them (note: `filepath.Join` is still used in `loadBookmarkCompareCmd`, so `path/filepath` stays; verify `fmt` and drop only if unused).

- [ ] **Step 5: Run the TUI package**

Run: `go test ./internal/tui/ 2>&1 | tail -15`
Expected: PASS — shelf row/capture tests green; existing bookmark tests (incl. `TestBookmarkDisplayString`) still green via the delegation.

- [ ] **Step 6: Update the CLI**

In `internal/cli/shelf.go`, rewrite `shelfAdd` to build a `FileAddress` (mirroring `gg bookmark add`) and `shelfList` to print the display:

```go
func shelfAdd(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("shelf add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	staged := fs.Bool("staged", false, "shelve the index (staged) version")
	rev := fs.String("rev", "", "shelve the version at this commit/branch")
	bucket := fs.String("bucket", "", "target bucket (default: default)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	paths := fs.Args()
	if len(paths) == 0 {
		fmt.Fprintln(stderr, "usage: gg shelf add [--staged|--rev <commit>] [--bucket <name>] <path>...")
		return 2
	}
	ctx := context.Background()
	var worktree, branch string
	if *rev == "" { // working/index origin: capture worktree + branch for display
		top, err := svc.TopLevel(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		worktree = top
		if br, err := svc.CurrentBranch(ctx); err == nil {
			branch = br
		}
	}
	for _, p := range paths {
		var addr model.FileAddress
		switch {
		case *rev != "":
			addr = model.FileAddress{State: model.StateCommitted, Commit: *rev, Path: p}
		case *staged:
			addr = model.FileAddress{State: model.StateStaged, Worktree: worktree, Branch: branch, Path: p}
		default:
			addr = model.FileAddress{State: model.StateUnstaged, Worktree: worktree, Branch: branch, Path: p}
		}
		e, err := svc.ShelfAdd(ctx, addr, *bucket)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprintln(stdout, e.ID)
	}
	return 0
}
```

And in `shelfList`, replace the print loop body:

```go
	for _, e := range es {
		fmt.Fprintf(stdout, "%s\t%s\t%dB\n", e.ID, e.Origin.Display(), e.Size)
	}
```

(Verify `svc.CurrentBranch(ctx) (string, error)` and `svc.TopLevel(ctx) (string, error)` exist — they back the CLI/TUI already; adjust the call if the signature differs.)

- [ ] **Step 7: Full build + test + e2e shelf scenario**

Run: `go build ./... && go test ./internal/cli/ ./internal/tui/ ./e2e/ -run 'Shelf|shelf|S43|s43' 2>&1 | tail -20`
Then a full unit run: `go test ./... 2>&1 | tail -20`
Expected: PASS, including `e2e` `s43` (the `unstaged-readme-md-81db67b6` id is unchanged).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/shelf.go internal/tui/shelf_actions.go internal/tui/bookmark_popup.go internal/tui/shelf_test.go internal/cli/shelf.go
git commit -m "feat(tui,cli): render shelf entries via shared FileAddress display

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 4: Docs, agentskill bump, full race gate

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`
- Modify: `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go`

- [ ] **Step 1: CHANGELOG**

Add under `## [Unreleased]` → `### Changed`:

```markdown
- Shelf entries now display like bookmarks: each entry stores a structured
  origin (`model.FileAddress` — worktree/branch/commit/state) captured at
  shelve-time and renders `<container> / <state-or-commit> / <path>` in the TUI
  Shelf tab and `gg shelf list`, instead of the terse `[source] path #sha`.
```

- [ ] **Step 2: README**

Find the `gg shelf list` description/output in `README.md` and update its sample output to the new `id<TAB><container> / <state> / <path><TAB>size` form (grep `shelf list` to locate it; if README only lists the command without sample output, no change needed — note that in the commit).

- [ ] **Step 3: CLAUDE.md package map**

In the `model` row of the package map (or the `shelf`/`bookmark` rows), add a sentence: the shared `model.FileAddress` (worktree/branch/commit/shelf-id/path + state) is the single address/display type behind both a shelf entry's `Origin` and a bookmark's address (`Bookmark.Address()`), rendered by `FileAddress.Display()`.

- [ ] **Step 4: agentskill**

In `internal/agentskill/using-gg.md`, update the `gg shelf list` line to note the bookmark-style display output. Bump `const Version` in `internal/agentskill/agentskill.go` from `14` to `15`.

- [ ] **Step 5: Dogfood the skill refresh**

Run: `go build ./cmd/gg && ./gg init --update 2>&1 | tail -5`
Expected: reports the installed using-gg skill refreshed to v15 (or "no agents detected" — either is fine; this just proves the command runs).

- [ ] **Step 6: Full race gate**

Run: `./test.sh race`
Expected: vet + gofmt clean, all unit tests pass, e2e green.

- [ ] **Step 7: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md internal/agentskill/
git commit -m "docs: shelf bookmark-style display; agentskill v15

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

## Self-Review

**Spec coverage:**
- Rename `BookmarkState`→`FileState` → Task 1 Step 3. ✓
- `FileAddress` + `Display()` + `FileRef()` + `Bookmark.Address()` → Task 1 Step 4. ✓
- `ShelfEntry.Origin` schema flip → Task 2 Step 1. ✓
- `FileStore.Put(addr)` + `idSource` (ID stable) → Task 2 Steps 2-3. ✓
- `domain.ShelfAdd(addr)` via `addr.FileRef()` → Task 2 Step 4. ✓
- TUI `shelfRows` Display, drop `focusedShelfRef`, reuse `focusedBookmark` w/ StateShelf guard → Task 3 Steps 3. ✓
- `shelf_actions.go` `Origin.Path`, `bookmarkDisplay` delegation → Task 3 Step 4. ✓
- CLI add builds address / list prints Display → Task 3 Step 6. ✓
- Tests (model display/ref/address, store, domain, tui rows/capture/guard, cli) → Tasks 1-3. ✓
- Docs + README + CLAUDE + agentskill bump + dogfood + race gate → Task 4. ✓

**Placeholder scan:** none — every code step shows full code; the two "verify signature/locate in README" notes are explicit conditional instructions, not deferred work.

**Type consistency:** `FileState`, `FileAddress{Worktree,Branch,Commit,ShelfID,Path,State}`, `Display() string`, `FileRef() FileRef`, `Bookmark.Address() FileAddress`, `ShelfEntry.Origin`, `Put(bucket, addr, data)`, `ShelfAdd(ctx, addr, bucket)`, `focusedShelfAddress() (model.FileAddress, bool)`, `shelfAddCmd(addr model.FileAddress)` — consistent across all tasks. ✓
