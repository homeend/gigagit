# Copy to working dir — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `.`-menu action "Copy to working dir" that writes a focused non-working file's content (from a stash, an old commit, or the index) into the working tree at its own path.

**Architecture:** One new TUI row builder, `copyToWorkingDirRow()`, the write-sibling of the existing `compareAgainstWorkingDirRow()`. It freezes the focused file via the shared `focusedCompareRef()` into a `model.FileRef`, and on run resolves the bytes synchronously (`m.svc.ResolveBytes`) and dispatches `engine.WriteFile{Path, Data}` via `startOp` — whose existing Overwrite/Cancel modal handles a clashing working file. Wired into `availableActions` immediately after each `compareAgainstWorkingDirRow()` call site. No engine/domain/git/model changes.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`). Tests use a real `git` in a `t.TempDir()` via existing helpers (`newRepoDir`, `gitRun`, `gitOut`, `driveOp`, `footerModel`, `filesMenuModel`, `findRow`).

## Global Constraints

- TUI-only change; touch only `internal/tui`. No changes to `internal/engine`, `internal/domain`, `internal/git`, or `internal/model` — the primitives (`engine.WriteFile`, `domain.Service.ResolveBytes`, `focusedCompareRef`) already exist.
- `Model` is a value receiver with pointer fields; the new code adds no Model fields.
- Availability MUST equal `compareAgainstWorkingDirRow`'s: present when `focusedCompareRef()` returns `ok` and `ref.Source != model.SourceUnstaged`; absent otherwise (nothing focused, a deletion `status == "D"`, or an already-working file).
- Resolve bytes synchronously in the run handler (matching `bookmarkPastePrompt`); surface resolve errors as `m.statusMsg`, never panic.
- The row id is exactly `copy-working-dir`; the label is exactly `Copy to working dir`.
- Follow existing tui test style; reuse helpers, don't add new ones unless necessary.

---

### Task 1: `copyToWorkingDirRow` builder + wiring + tests

**Files:**
- Modify: `internal/tui/bookmark.go` (add the builder next to `compareAgainstWorkingDirRow`; add the `engine` import)
- Modify: `internal/tui/action_menu.go:82` (content-window block) and `:159` (panel-focus block) — append the row after `compareAgainstWorkingDirRow`
- Test: `internal/tui/copy_working_dir_test.go` (create)

**Interfaces:**
- Consumes: `m.focusedCompareRef() (model.FileRef, string, bool)` (bookmark.go:184); `m.svc.ResolveBytes(ctx, model.FileRef) ([]byte, error)` (domain/fileref.go:14); `engine.WriteFile{Path string; Data []byte}` (engine/writefile.go:14); `m.startOp(engine.Operation) (Model, tea.Cmd)`.
- Produces: `func (m Model) copyToWorkingDirRow() (actionRow, bool)` — a menu row with id `copy-working-dir`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/copy_working_dir_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// On a focused stash file tree (filesHash holds the stash's resolved SHA), the
// "Copy to working dir" row is offered.
func TestCopyToWorkingDirRowPresentOnStashFile(t *testing.T) {
	m := footerModel()
	m.filesView = &contentPopup{lines: []contentLine{{text: "dir/f.go", path: "dir/f.go"}}}
	m.filesTreeFocused = true
	m.filesHash = "0123456789abcdef0123456789abcdef01234567" // stash's resolved commit SHA
	m.filesMode = filesModeStash
	m.filesStashTag = "stash@{0}"
	if _, ok := findRow(availableActions(m), "copy-working-dir"); !ok {
		t.Fatal("Copy to working dir missing on a stash file")
	}
}

// A plain working-tree (unstaged) file is already local — the row is absent.
func TestCopyToWorkingDirRowAbsentOnWorkingFile(t *testing.T) {
	m := filesMenuModel() // panelFiles focused, one tracked file (Source = Unstaged)
	m.currentWorktree = "/wt"
	if _, ok := findRow(availableActions(m), "copy-working-dir"); ok {
		t.Fatal("Copy to working dir must be absent for a working-tree file")
	}
}

// A file deleted in the commit/stash has no content to copy — the row is absent.
func TestCopyToWorkingDirRowAbsentOnDeletion(t *testing.T) {
	m := footerModel()
	m.filesView = &contentPopup{lines: []contentLine{{text: "dir/f.go", path: "dir/f.go", status: "D"}}}
	m.filesTreeFocused = true
	m.filesHash = "0123456789abcdef0123456789abcdef01234567"
	if _, ok := findRow(availableActions(m), "copy-working-dir"); ok {
		t.Fatal("Copy to working dir must be absent for a deletion")
	}
}

// End to end: a file that exists at an old commit but not in the working tree is
// written back. (A stash file takes the identical SourceCommit resolve path; an
// absent destination keeps the test free of the Overwrite modal, which is
// covered by engine.WriteFile's own tests.)
func TestCopyToWorkingDirWritesFile(t *testing.T) {
	dir, repo := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add a")
	sha := gitOut(t, dir, "rev-parse", "HEAD") // commit where a.txt = "v1"
	gitRun(t, dir, "rm", "a.txt")
	gitRun(t, dir, "commit", "-m", "drop a") // a.txt now absent from the working tree

	// Load first (matches the working real-op precedent
	// TestRunCommitOperationFinishesAndClearsRunning, so Execute/repogate has the
	// gitCommonDir/toplevel it needs). The dataLoadedMsg handler does not touch
	// filesView/filesHash, so the tree setup below survives the load.
	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.filesView = &contentPopup{lines: []contentLine{{text: "a.txt", path: "a.txt"}}}
	m.filesTreeFocused = true
	m.filesHash = sha

	row, ok := findRow(availableActions(m), "copy-working-dir")
	if !ok {
		t.Fatal("Copy to working dir row missing")
	}
	tm, cmd := row.run(m)
	m = driveOp(t, tm.(Model), cmd)

	got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatalf("a.txt not written: %v", err)
	}
	if string(got) != "v1\n" {
		t.Fatalf("a.txt = %q, want %q", got, "v1\n")
	}
}

// A resolve failure (bogus commit) surfaces as statusMsg, synchronously, with no
// op dispatched and no panic.
func TestCopyToWorkingDirResolveErrorSetsStatus(t *testing.T) {
	_, repo := newRepoDir(t)
	m := New(domain.New(repo))
	m.filesView = &contentPopup{lines: []contentLine{{text: "nope.txt", path: "nope.txt"}}}
	m.filesTreeFocused = true
	m.filesHash = "0123456789abcdef0123456789abcdef01234567" // nonexistent commit

	row, ok := findRow(availableActions(m), "copy-working-dir")
	if !ok {
		t.Fatal("Copy to working dir row missing")
	}
	tm, _ := row.run(m)
	mm := tm.(Model)
	if mm.running {
		t.Fatal("a failed resolve must not start an op")
	}
	if !strings.Contains(mm.statusMsg, "copy to working dir") {
		t.Fatalf("statusMsg = %q, want a copy-to-working-dir error", mm.statusMsg)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run TestCopyToWorkingDir -v`
Expected: the code **compiles** (the tests reference no new symbol — they go through `availableActions`/`findRow`). The **3 positive tests FAIL** because the `copy-working-dir` row isn't wired yet (`findRow` → `ok=false`): `TestCopyToWorkingDirRowPresentOnStashFile`, `TestCopyToWorkingDirWritesFile`, `TestCopyToWorkingDirResolveErrorSetsStatus`. The **2 absent-row tests already PASS** (no row exists → correctly absent): `TestCopyToWorkingDirRowAbsentOnWorkingFile`, `TestCopyToWorkingDirRowAbsentOnDeletion`. That split is expected — they lock in the absence guard.

- [ ] **Step 3: Add the `engine` import to `bookmark.go`**

In `internal/tui/bookmark.go`, extend the import block (currently `context`, `tea`, `model`) to add engine:

```go
import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)
```

- [ ] **Step 4: Add the row builder**

In `internal/tui/bookmark.go`, immediately after `compareAgainstWorkingDirRow` (it ends near line 252), add:

```go
// copyToWorkingDirRow is the menu action "Copy to working dir": it writes the
// focused file's resolved bytes into the working tree at its own path, as an
// unstaged change. The write-sibling of compareAgainstWorkingDirRow — same
// focused ref, same guard (absent for a working-tree file or a deletion).
// engine.WriteFile owns the overwrite-or-cancel fork when a differing working
// file already exists; identical bytes are a no-op.
func (m Model) copyToWorkingDirRow() (actionRow, bool) {
	ref, _, ok := m.focusedCompareRef()
	if !ok || ref.Source == model.SourceUnstaged {
		return actionRow{}, false
	}
	return actionRow{
		id:    "copy-working-dir",
		label: "Copy to working dir",
		run: func(m Model) (tea.Model, tea.Cmd) {
			data, err := m.svc.ResolveBytes(context.Background(), ref)
			if err != nil {
				m.statusMsg = "copy to working dir: " + err.Error()
				return m, nil
			}
			return m.startOp(engine.WriteFile{Path: ref.Path, Data: data})
		},
	}, true
}
```

- [ ] **Step 5: Wire it into the content-window block**

In `internal/tui/action_menu.go`, right after the `compareAgainstWorkingDirRow` block at lines 82–84 (which uses `rows`), add:

```go
		if r, ok := m.compareAgainstWorkingDirRow(); ok {
			rows = append(rows, r)
		}
		if r, ok := m.copyToWorkingDirRow(); ok {
			rows = append(rows, r)
		}
```

- [ ] **Step 6: Wire it into the panel-focus block**

In `internal/tui/action_menu.go`, right after the `compareAgainstWorkingDirRow` block at lines 159–161 (which uses `out`), add:

```go
	if r, ok := m.compareAgainstWorkingDirRow(); ok {
		out = append(out, r)
	}
	if r, ok := m.copyToWorkingDirRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run TestCopyToWorkingDir -v`
Expected: PASS (all five).

- [ ] **Step 8: Full package + gofmt + vet**

Run: `gofmt -l internal/tui/ && go vet ./internal/tui/ && go test ./internal/tui/`
Expected: no gofmt output, no vet output, `ok` for the package (existing tests still pass).

- [ ] **Step 9: Commit**

```bash
git add internal/tui/bookmark.go internal/tui/action_menu.go internal/tui/copy_working_dir_test.go
git commit -m "feat(tui): Copy to working dir — write a focused non-working file to the worktree"
```

---

### Task 2: Docs + full sweep

**Files:**
- Modify: `CHANGELOG.md`, `internal/tui/help.go`

**Interfaces:** none (docs only).

- [ ] **Step 1: Update CHANGELOG.md**

Add to the top `## [Unreleased]` → `### Added` list:

```markdown
- **Copy to working dir.** The `.` menu on a focused non-working file (a stash
  file, an old commit's file, or a staged file) now offers **Copy to working
  dir**, which writes that file's content into the working tree at its own path
  (with an overwrite prompt if a differing working file already exists). It is
  the write-sibling of **Compare against working dir**.
```

- [ ] **Step 2: Mention the action in help.go**

First locate where the `.`-menu file actions are described:

Run: `grep -n 'Compare against working dir\|working dir' internal/tui/help.go`
Expected: a line describing the `.` menu's file actions. Add "Copy to working dir" alongside "Compare against working dir" in that same description. If `grep` returns no match (the help text doesn't enumerate these rows individually), skip this step — the `.` menu lists rows live, so no help edit is required; note that in the commit body.

- [ ] **Step 3: Full race + e2e sweep**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit tests pass (including `copy_working_dir_test.go`), e2e green, no data races.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md internal/tui/help.go
git commit -m "docs: changelog + help for Copy to working dir"
```

(If Step 2 made no edit, commit only `CHANGELOG.md`.)

---

## Self-Review

**Spec coverage:**
- New "Copy to working dir" action on a focused non-working file → Task 1. ✓
- Resolve bytes (`ResolveBytes`) + write (`engine.WriteFile`) to own path → Task 1 row builder. ✓
- Availability = "Compare against working dir" (stash + commit + staged) → wired at both `compareAgainstWorkingDirRow` call sites (Task 1 Steps 5–6); `TestCopyToWorkingDirRowPresentOnStashFile` + `TestCopyToWorkingDirRowAbsentOnWorkingFile`. ✓
- Deletion excluded → `TestCopyToWorkingDirRowAbsentOnDeletion` (relies on `focusedBookmark`'s existing `status == "D"` guard). ✓
- Overwrite handled by existing modal; identical bytes no-op → inherited from `engine.WriteFile`; e2e test uses an absent destination to stay modal-free (overwrite fork is engine-tested). ✓
- Resolve failure → statusMsg, no panic → `TestCopyToWorkingDirResolveErrorSetsStatus`. ✓
- Binary content fine → byte copy; no special handling needed (no test required — `ResolveBytes`/`WriteFile` don't interpret content). ✓
- Stash untouched (no pop/drop) → the op only writes one working file; no stash verb is invoked (true by construction). ✓
- Docs (CHANGELOG, help) → Task 2. No CLI surface change → no agentskill bump. ✓

**Placeholder scan:** No TBD/TODO; every code step shows the full edit. Task 2 Step 2 is conditional with an explicit grep gate and a defined fallback, not a placeholder.

**Type consistency:** `copyToWorkingDirRow() (actionRow, bool)`, id `copy-working-dir`, label `Copy to working dir`, `model.SourceUnstaged`, `engine.WriteFile{Path, Data}`, `m.svc.ResolveBytes`, `focusedCompareRef`, `findRow`, `availableActions`, `driveOp`, `newRepoDir`, `gitRun`, `gitOut`, `footerModel`, `filesMenuModel`, `contentPopup`/`contentLine{text,path,status}`, `filesModeStash`, `filesStashTag` — all verified against the current source.
