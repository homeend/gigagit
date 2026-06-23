# files-view state machine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make files-view transitions the single source of truth for its state, so a mode switch cannot leave a stale field behind — killing the "half-reset" bug class.

**Architecture:** A `filesMode` enum replaces the implicit `filesCompare`/`filesAllFiles` boolean discriminator. A set of transition methods becomes the ONLY mutators of the ~13-field cluster; each sets the complete consistent set for its target state. Fields stay on `Model` (no relocation churn). Staged: add enum alongside booleans → route writes through transitions → delete the booleans.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`), `internal/tui`.

## Global Constraints

- `internal/tui` MUST NOT import `internal/git` (archtest-guarded). No new git access.
- `Model` is a value receiver; transition methods take `Model` and return `Model` (plus `tea.Cmd` when they load).
- **Behavior-preserving.** No user-visible change at any step. Every transition method reproduces the exact current field writes at the site it replaces (the spec's table is the contract for which fields each sets/clears).
- The **cluster** = `filesView, filesTitle, filesHash, filesCompare, filesLeft, filesRight, compareTag, filesStashTag, filesTreeFocused, filesReadInflight, filesAllFiles, filesPreview, filesPreviewTag` (`model.go:62-74`). Plus the new `filesMode`.
- The async load handlers (`dataLoaded`/`treeFiles`/`compareFiles`/`fileContent` msgs) populate `m.filesView.lines`/`.sel` and `m.filesPreview.lines` — those mutate popup CONTENTS, not the cluster; they stay as-is and are NOT transitions.
- `m.focus` (the global panel focus) is NOT part of the cluster — callers that set it (reflog) keep doing so outside the transition methods.
- Run `./test.sh` after each task; `./test.sh race` before the final commit.

---

### Task 1: add the `filesMode` enum + field + derive it (no removal)

Pure addition: introduce the mode, set it at the 4 open sites alongside the existing booleans, add helpers that return the same truth. Nothing removed.

**Files:**
- Modify: `internal/tui/files_view.go` (enum + helpers near the cluster's logic; set mode in `openCompareFiles`)
- Modify: `internal/tui/model.go:1056` (l-key open), `internal/tui/stash_view.go:130`, `internal/tui/reflog_view.go:78` (set mode)
- Test: `internal/tui/files_mode_test.go` (new)

**Interfaces:**
- Produces: `type filesMode int`; consts `filesModeChanged, filesModeFullTree, filesModeCompare, filesModeStash`; `Model.filesMode filesMode`; `func (m Model) inCompareMode() bool { return m.filesMode == filesModeCompare }`; `func (m Model) inFullTree() bool { return m.filesMode == filesModeFullTree }`.

- [ ] **Step 1: Write the failing parity test**

```go
// internal/tui/files_mode_test.go
package tui

import "testing"

// The new mode must agree with the legacy booleans at each open path.
func TestFilesModeMatchesLegacyBooleans(t *testing.T) {
	// changed-files open (l-key path sets these)
	m := loadedModelLinearCommits(t, 2)
	m.filesView = &contentPopup{}
	m.filesAllFiles = false
	m.filesCompare = false
	m.filesMode = filesModeChanged
	if m.inCompareMode() != m.filesCompare || m.inFullTree() != m.filesAllFiles {
		t.Fatal("changed: helpers must equal legacy booleans")
	}
	// compare open
	m.filesCompare = true
	m.filesMode = filesModeCompare
	if !m.inCompareMode() || m.inFullTree() {
		t.Fatal("compare: inCompareMode true, inFullTree false")
	}
	// full-tree
	m.filesCompare = false
	m.filesAllFiles = true
	m.filesMode = filesModeFullTree
	if m.inCompareMode() || !m.inFullTree() {
		t.Fatal("fullTree: inFullTree true, inCompareMode false")
	}
}
```

- [ ] **Step 2: Run — expect compile failure** (`undefined: filesModeChanged`, `m.filesMode`).

Run: `go test ./internal/tui/ -run TestFilesModeMatchesLegacyBooleans`

- [ ] **Step 3: Add the type, field, consts, helpers**

In `internal/tui/files_view.go` (top, after imports):

```go
// filesMode is the files view's source mode — exactly one is active while the
// view is open. It is the authoritative discriminator; the inCompareMode/
// inFullTree helpers read it (legacy filesCompare/filesAllFiles booleans are
// removed in a later task).
type filesMode int

const (
	filesModeChanged  filesMode = iota // a commit's changed files (vs parent)
	filesModeFullTree                  // every file at the commit (ls-tree); `a` toggle
	filesModeCompare                   // two endpoints (filesLeft/filesRight)
	filesModeStash                     // a stash's files (filesStashTag)
)

func (m Model) inCompareMode() bool { return m.filesMode == filesModeCompare }
func (m Model) inFullTree() bool    { return m.filesMode == filesModeFullTree }
```

Add the field to the cluster in `internal/tui/model.go` (beside `filesView`, ~`:62`):

```go
	filesMode         filesMode      // authoritative source mode (changed/fullTree/compare/stash)
```

- [ ] **Step 4: Set the mode at the 4 open sites (alongside existing writes)**

- `files_view.go` `openCompareFiles` (~:139, where `m.filesCompare = true`): add `m.filesMode = filesModeCompare`.
- `model.go:1056` l-key open (where `m.filesAllFiles = false`): add `m.filesMode = filesModeChanged`.
- `reflog_view.go:78` open (where `m.filesAllFiles = false`): add `m.filesMode = filesModeChanged`.
- `stash_view.go:130` open (where `m.filesStashTag = e.Ref`): add `m.filesMode = filesModeStash`.
- `files_view.go` `a` toggle (~:235, flips `m.filesAllFiles`): set `m.filesMode` to `filesModeFullTree` when turning on, `filesModeChanged` when off.

- [ ] **Step 5: Run test + package**

Run: `go test ./internal/tui/ -run TestFilesModeMatchesLegacyBooleans` → PASS.
Run: `./test.sh unit` → PASS (pure addition).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/
git commit -m "refactor(tui): add filesMode enum derived alongside legacy booleans"
```

---

### Task 2: transition methods become the only mutators

Add the transition methods (each setting the COMPLETE consistent set), and route every open/close/focus/preview/toggle site through them.

**Files:**
- Modify: `internal/tui/files_view.go` (methods + esc/l close + `a` toggle + focus handlers)
- Modify: `internal/tui/file_preview.go` (openPreview/closePreview)
- Modify: `internal/tui/model.go` (l-key open, narrow-close `:186`, repo-switch `:1755`), `internal/tui/stash_view.go`, `internal/tui/reflog_view.go`
- Test: `internal/tui/files_view_transitions_test.go` (new)

**Interfaces (Produces):**
```go
func (m Model) openChangedFiles(c model.Commit) (Model, tea.Cmd)       // mode=Changed, complete set, treeFocused=false
func (m Model) openCompareFiles(left, right model.Endpoint) (Model, tea.Cmd) // EXISTS; extend to set the complete set incl. filesMode + clear stash
func (m Model) openStashFiles(ref, subject string) (Model, tea.Cmd)    // mode=Stash, complete set, treeFocused=false
func (m Model) toggleFullTree() (Model, tea.Cmd)                       // flip Changed<->FullTree, drop preview, reload
func (m Model) openPreview(hash, path string) (Model, tea.Cmd)         // rename of openFilePreview; sets preview + focusRight
func (m Model) closePreview() Model                                    // preview=nil/tag="", focusTree
func (m Model) focusTree() Model                                       // treeFocused=true
func (m Model) focusRight() Model                                      // treeFocused=false IF not compare (compare has no list side)
func (m Model) closeFilesView() Model                                  // zero the ENTIRE cluster (single close chokepoint)
```

- [ ] **Step 1: Write failing bug-class tests**

```go
// internal/tui/files_view_transitions_test.go
package tui

import (
	"testing"
	"github.com/gigagit/gg/internal/model"
)

// closeFilesView must zero the entire cluster — no stale field survives.
func TestCloseFilesViewZeroesEverything(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	// dirty every field the way full-tree-with-preview-after-compare would
	m.filesView = &contentPopup{}
	m.filesTitle = "x"
	m.filesHash = "abc"
	m.filesCompare = true
	m.filesLeft = model.Endpoint{Kind: model.EndpointCommit, Hash: "a"}
	m.filesRight = model.Endpoint{Kind: model.EndpointWorkTree}
	m.compareTag = "cmp:x"
	m.filesStashTag = "stash@{0}"
	m.filesTreeFocused = true
	m.filesReadInflight = true
	m.filesAllFiles = true
	m.filesPreview = &contentPopup{}
	m.filesPreviewTag = "p@h"
	m.filesMode = filesModeFullTree

	m = m.closeFilesView()

	if m.filesView != nil || m.filesPreview != nil || m.filesTitle != "" ||
		m.filesHash != "" || m.filesCompare || m.filesAllFiles || m.compareTag != "" ||
		m.filesStashTag != "" || m.filesTreeFocused || m.filesReadInflight ||
		m.filesPreviewTag != "" || m.filesLeft != (model.Endpoint{}) ||
		m.filesRight != (model.Endpoint{}) || m.filesMode != filesModeChanged {
		t.Fatalf("closeFilesView left stale state: %+v", m)
	}
}

// Switching from full-tree-with-preview into compare drops the preview + full-tree.
func TestOpenCompareDropsPreviewAndFullTree(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.filesAllFiles = true
	m.filesMode = filesModeFullTree
	m.filesPreview = &contentPopup{}
	m.filesPreviewTag = "p@h"

	m, _ = m.openCompareFiles(
		model.Endpoint{Kind: model.EndpointCommit, Hash: "a"},
		model.Endpoint{Kind: model.EndpointWorkTree})

	if m.filesPreview != nil || m.filesPreviewTag != "" || m.inFullTree() ||
		!m.inCompareMode() {
		t.Fatal("openCompareFiles must drop preview+fullTree and enter compare mode")
	}
}

// toggleFullTree drops an open preview.
func TestToggleFullTreeDropsPreview(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.filesView = &contentPopup{}
	m.filesHash = "abc"
	m.filesMode = filesModeFullTree
	m.filesAllFiles = true
	m.filesPreview = &contentPopup{}
	m.filesPreviewTag = "p@h"

	m, _ = m.toggleFullTree() // fullTree -> changed

	if m.filesPreview != nil || m.inFullTree() {
		t.Fatal("toggleFullTree must drop the preview and leave full-tree")
	}
}
```

- [ ] **Step 2: Run — expect failure** (`closeFilesView`/`openChangedFiles`/`toggleFullTree` undefined).

Run: `go test ./internal/tui/ -run 'TestCloseFilesViewZeroes|TestOpenCompareDrops|TestToggleFullTreeDrops'`

- [ ] **Step 3: Add `closeFilesView` (the close chokepoint)**

`internal/tui/files_view.go`:

```go
// closeFilesView closes the view and zeroes the ENTIRE cluster — the single
// place that defines "no files view is open". Replaces the per-site partial
// resets (esc, l, narrow-close, repo-switch) that each cleared a different subset.
func (m Model) closeFilesView() Model {
	m.filesView = nil
	m.filesTitle = ""
	m.filesHash = ""
	m.filesCompare = false
	m.filesLeft = model.Endpoint{}
	m.filesRight = model.Endpoint{}
	m.compareTag = ""
	m.filesStashTag = ""
	m.filesTreeFocused = false
	m.filesReadInflight = false
	m.filesAllFiles = false
	m.filesPreview = nil
	m.filesPreviewTag = ""
	m.filesMode = filesModeChanged
	return m
}
```

- [ ] **Step 4: Add the open/focus/preview transitions**

```go
// openChangedFiles opens a commit's changed-file list (mode=Changed), setting
// the complete consistent set and clearing compare/stash/fullTree/preview.
func (m Model) openChangedFiles(c model.Commit) (Model, tea.Cmd) {
	m = m.closeFilesView() // start from a clean slate, then set this mode
	m.filesView = &contentPopup{lines: []contentLine{{text: "(loading…)"}}}
	m.filesTitle = "Files " + shortHash(c.Hash) + " " + c.Subject
	m.filesHash = c.Hash
	m.filesMode = filesModeChanged
	m.filesReadInflight = true
	return m, m.loadCommitFilesCmd(c)
}

// openStashFiles opens a stash's files (mode=Stash).
func (m Model) openStashFiles(ref, subject string) (Model, tea.Cmd) {
	m = m.closeFilesView()
	m.filesView = &contentPopup{lines: []contentLine{{text: "(loading…)"}}}
	m.filesTitle = "Files " + ref + " " + subject
	m.filesStashTag = ref
	m.filesMode = filesModeStash
	return m, m.loadStashFilesCmd(ref)
}

func (m Model) focusTree() Model  { m.filesTreeFocused = true; return m }
func (m Model) focusRight() Model {
	if !m.inCompareMode() { // compare mode has no commit-list side to focus
		m.filesTreeFocused = false
	}
	return m
}

// closePreview drops the right-column preview and returns focus to the tree.
func (m Model) closePreview() Model {
	m.filesPreview = nil
	m.filesPreviewTag = ""
	return m.focusTree()
}
```

Rewrite `openCompareFiles` to start from `closeFilesView()` then set the compare set (preserving its current h/b `filesHash` derivation and `filesMode = filesModeCompare`, `filesTreeFocused = true`). Rename `openFilePreview` → `openPreview` (same body; ends `return m.focusRight()`-style by setting `filesTreeFocused=false`). Add `toggleFullTree` wrapping the current `a`-handler body (flip `filesAllFiles` + `filesMode`, drop preview via the preview fields, set loading + `filesReadInflight`, return `loadFilesForCmd(...)`).

- [ ] **Step 5: Route every site through the transitions**

- `model.go:1056` l-key open → `return m.openChangedFiles(c)`.
- `reflog_view.go:78` → `m, cmd := m.openChangedFiles(c); m = m.focusTree(); m.focus = panelCommits; return m, cmd`.
- `stash_view.go:130` → `return m.openStashFiles(e.Ref, e.Subject)` then (caller) leave focus as the method sets it (treeFocused=false matches today).
- `files_view.go` esc branch (`:289-305`): preview-first → `if m.filesPreview != nil { m = m.closePreview(); return m, nil }`; the close → `m = m.closeFilesView(); return m, nil`.
- `files_view.go` `l` branch (`:307-313`) → `m = m.closeFilesView(); return m, nil`.
- `files_view.go` `a` (`:235`) → `return m.toggleFullTree()`.
- `files_view.go` focus handlers (`left`/`right`/`tab`) → `m = m.focusTree()` / `m = m.focusRight()` / `tab`: `if !m.inCompareMode() { if m.filesTreeFocused { m = m.focusRight() } else { m = m.focusTree() } }`.
- `file_preview.go` openFilePreview callers → `openPreview`; its close paths → `closePreview()`.
- `model.go:186` narrow-close → `m = m.closeFilesView()` (keep the `statusMsg`).
- `model.go:1755` repo-switch → `m = m.closeFilesView()`.
- `mouse.go` focus-click assignments (`m.filesTreeFocused = …`, 2 sites) → `m = m.focusTree()` / `m = m.focusRight()`.
- **Catch-all:** route EVERY remaining bare cluster assignment (`m.<clusterField> = …`) in `model.go`/`mouse.go`/`stash_view.go`/`reflog_view.go` through a transition. The only bare cluster assignments that may remain are inside the transition methods themselves (in `files_view.go`/`file_preview.go`). The async handlers' `m.filesView.lines = …`/`.sel = …` and `m.filesPreview.lines = …` are popup-content writes (they do NOT match `m\.filesView = `) and stay. Task 3's grep gate confirms none were missed.

- [ ] **Step 6: Run tests + package**

Run: `go test ./internal/tui/ -run 'TestCloseFilesViewZeroes|TestOpenCompareDrops|TestToggleFullTreeDrops'` → PASS.
Run: `./test.sh unit` → PASS (behavior preserved; the focus-on-open for stash/reflog matches the originals).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/
git commit -m "refactor(tui): route files-view state through transition methods

closeFilesView is the single close chokepoint (zeroes the whole cluster);
open/toggle/preview/focus all go through transitions. Kills the half-reset
bug class (esc/l/narrow-close/repo-switch previously cleared different subsets)."
```

---

### Task 3: delete the legacy booleans

Remove `filesCompare` and `filesAllFiles`; repoint their reads to `inCompareMode()`/`inFullTree()`.

**Files:**
- Modify: `internal/tui/model.go` (delete 2 fields), and every reader of the two booleans (`files_view.go`, `file_preview.go`, `action_menu.go`, `mouse.go`, `view.go`, …)
- Test: existing suite + a grep gate.

- [ ] **Step 1: Repoint reads**

Convert every READ `m.filesCompare` → `m.inCompareMode()` and `m.filesAllFiles` → `m.inFullTree()`. Delete the WRITES (they are now redundant with `m.filesMode`, which the transition methods already set). Then delete the two fields from `model.go`.

- [ ] **Step 2: Build + grep gates**

Run: `go build ./...` → success.
Run: `grep -rn "filesCompare\|filesAllFiles" internal/tui/*.go | grep -v _test`
Expected: no lines (both gone).
Run (the encapsulation gate — bare cluster assignments only inside the transition methods): `grep -rn -E "m\.(filesView|filesTitle|filesHash|filesLeft|filesRight|compareTag|filesStashTag|filesTreeFocused|filesReadInflight|filesPreview|filesPreviewTag|filesMode) = " internal/tui/*.go | grep -v _test | grep -v "files_view.go\|file_preview.go"`
Expected: **no lines.** All bare cluster assignments now live inside the transition methods (in `files_view.go`/`file_preview.go`, excluded by the grep). The async handlers write `m.filesView.lines`/`.sel` / `m.filesPreview.lines` (popup contents — these do NOT match `m\.filesView = …`), so they don't appear. Investigate and route any hit.

- [ ] **Step 3: Suite**

Run: `./test.sh unit` → PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/
git commit -m "refactor(tui): drop filesCompare/filesAllFiles — mode is authoritative"
```

---

### Task 4: docs + race

- [ ] **Step 1: CHANGELOG**

Add under the unreleased section:

```markdown
### Changed
- TUI internals: the commit/compare/stash files view is now a small state
  machine — a `filesMode` plus transition methods are the single source of
  truth, so mode switches can no longer leave stale state. No user-visible
  change. (Fixes the class of "half-reset" bugs behind the full-tree/preview
  features.)
```

- [ ] **Step 2: Race**

Run: `./test.sh race` → PASS.

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(tui): changelog for the files-view state machine"
```

---

## Self-Review

**Spec coverage:** `filesMode` enum + field (T1) ✓; helpers `inCompareMode`/`inFullTree` (T1) ✓; transition methods as sole mutators incl. `closeFilesView` chokepoint (T2) ✓; the 4 open sites + `a` + focus + preview + narrow-close + repo-switch routed (T2) ✓; `filesCompare`/`filesAllFiles` removed, reads repointed (T3) ✓; data fields (`filesHash`/`filesLeft`/`filesRight`/`filesStashTag`/popups/gates) retained (T2/T3 — never deleted) ✓; no field-relocation churn (reads stay `m.filesHash`) ✓; bug-class guard tests (T2) + parity test (T1) ✓; grep encapsulation gate (T3) ✓; CHANGELOG + race (T4) ✓.

**Placeholder scan:** none — transition method bodies and test code are complete; site conversions cite current file:line.

**Type consistency:** `filesMode`/consts, `inCompareMode()`/`inFullTree()`, `closeFilesView() Model`, `openChangedFiles(model.Commit) (Model, tea.Cmd)`, `openStashFiles(string,string)`, `openPreview(hash,path string)`, `closePreview() Model`, `focusTree()/focusRight() Model` consistent across tasks. `openCompareFiles` keeps its existing signature.
