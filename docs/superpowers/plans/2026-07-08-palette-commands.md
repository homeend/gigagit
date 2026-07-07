# Command-palette commands Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Grow the `ctrl+p` command palette from one entry to seven — File history, File blame, Find, Open repo, Git config explorer, Set up agent skills — moving the last two out of the Settings `,` menu.

**Architecture:** Two new file-path popups reuse the existing history/blame full-screen surfaces; a new repo-path popup validates via `TopLevel` then `reRoot`s; three entries launch existing surfaces. Popups follow the `gotoCommitPopup` pattern (layer with `update`/`render`, embed `popupMax`); key routing is polymorphic so no wiring is needed beyond one async-message dispatch arm.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style `Model`), lipgloss. Tests use `FakeRunner` or a real `git` in `t.TempDir()`.

## Global Constraints

- **Work in the worktree** `/mnt/t/others/gigagit/.claude/worktrees/palette-commands` (branch `feat/palette-commands`). Use the **worktree absolute path** for every Write/Edit; a subagent must `cd` there and verify `git branch --show-current` == `feat/palette-commands` before editing, and confirm each commit landed in the worktree (`git -C <worktree> status`), not the main checkout.
- **`internal/tui` never imports `internal/git`** — reach git through `internal/domain` (archtest-guarded). Test files may import `internal/domain`/`internal/git`/`internal/gitexec` (existing tests do).
- **`Model` is a value receiver** with pointer layer fields; layers are pushed as pointers (`m.pushLayer(&thing{})`).
- **Every popup embeds `popupMax`** to get central `ctrl+t` maximize for free.
- **Paths can contain spaces** — path popups must NOT swallow `tea.KeySpace` (unlike `gotoCommitPopup`); `textfield.HandleEditKey` already inserts a space on `KeySpace`.
- **`~` expansion uses `os.UserHomeDir()`**, never `$HOME` (unset on Windows).
- Build/test from inside the worktree: `go build ./cmd/gg`, `./test.sh unit` (fast), `./test.sh` (full) before finishing.
- TDD: failing test → minimal code → passing test → commit. Reference: `internal/tui/goto_commit_popup.go` and `internal/tui/command_palette_test.go` are the templates.

Spec: `docs/superpowers/specs/2026-07-08-palette-commands-design.md`.

---

### Task 1: `repoRelPath` path-normalization helper

**Files:**
- Create: `internal/tui/file_path_popup.go`
- Test: `internal/tui/file_path_popup_test.go`

**Interfaces:**
- Produces: `type filePathKind int` with `filePathHistory`, `filePathBlame`; `func repoRelPath(root, p string) string`.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/file_path_popup_test.go`:

```go
package tui

import (
	"path/filepath"
	"testing"
)

func TestRepoRelPath(t *testing.T) {
	root := filepath.FromSlash("/repo")
	outside := filepath.FromSlash("/elsewhere/x.go")
	cases := []struct{ name, in, want string }{
		{"already relative", "internal/tui/model.go", "internal/tui/model.go"},
		{"dot-slash relative", "./internal/x.go", "internal/x.go"},
		{"absolute inside repo", filepath.FromSlash("/repo/internal/x.go"), "internal/x.go"},
		{"absolute outside repo", outside, filepath.ToSlash(filepath.Clean(outside))},
		{"blank", "   ", ""},
	}
	for _, c := range cases {
		if got := repoRelPath(root, c.in); got != c.want {
			t.Errorf("%s: repoRelPath(%q,%q)=%q want %q", c.name, root, c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestRepoRelPath`
Expected: FAIL — `undefined: repoRelPath`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tui/file_path_popup.go`:

```go
package tui

import (
	"path/filepath"
	"strings"
)

// filePathKind selects which surface a filePathPopup opens on submit.
type filePathKind int

const (
	filePathHistory filePathKind = iota
	filePathBlame
)

// repoRelPath turns user-typed input into the repo-relative, forward-slashed
// path the git verbs expect. An absolute path inside root is reduced to its
// repo-relative form; anything else is cleaned and slashed as-is. Blank stays
// blank. A path that escapes the repo (../…) falls back to the cleaned input —
// git then reports no history rather than the popup hard-failing.
func repoRelPath(root, p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if root != "" && filepath.IsAbs(p) {
		if rel, err := filepath.Rel(root, p); err == nil && !escapesRepo(rel) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(filepath.Clean(p))
}

func escapesRepo(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestRepoRelPath`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/palette-commands
git add internal/tui/file_path_popup.go internal/tui/file_path_popup_test.go
git commit -m "feat(tui): repoRelPath path normalization for palette file popups"
```

---

### Task 2: `filePathPopup` + File history / File blame palette entries

**Files:**
- Modify: `internal/tui/file_path_popup.go` (add the popup type + methods)
- Modify: `internal/tui/command_palette.go:28-32` (extend `paletteCommands()`)
- Test: `internal/tui/file_path_popup_test.go` (add popup + palette tests)

**Interfaces:**
- Consumes: `filePathKind` (Task 1); `navContext{path, rev}`, `newHistoryView`, `loadHistoryListCmd`, `newBlameView`, `loadBlameCmd` (existing); `commandPalette`, `paletteCommand` (existing).
- Produces: `type filePathPopup`; `func (m Model) openFilePathPopup(kind filePathKind) (Model, tea.Cmd)`; test helper `func palettePick(t *testing.T, m Model, label string) (Model, tea.Cmd)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/file_path_popup_test.go` (add `tea "github.com/charmbracelet/bubbletea"` to imports):

```go
// palettePick opens the palette, navigates to the command labelled label, and
// presses enter. Reused by every palette entry's test.
func palettePick(t *testing.T, m Model, label string) (Model, tea.Cmd) {
	t.Helper()
	m, _ = send(m, keyType(tea.KeyCtrlP))
	p := layerOf[*commandPalette](m)
	if p == nil {
		t.Fatal("ctrl+p did not open the palette")
	}
	idx := -1
	for i, c := range p.cmds {
		if c.label == label {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("palette has no command %q", label)
	}
	for j := 0; j < idx; j++ {
		m, _ = send(m, keyType(tea.KeyDown))
	}
	return send(m, keyType(tea.KeyEnter))
}

func TestPaletteFileHistoryOpensPopup(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "File history")
	p := layerOf[*filePathPopup](m)
	if p == nil || p.kind != filePathHistory {
		t.Fatal("File history should open a history file-path popup")
	}
	if layerOf[*commandPalette](m) == nil {
		t.Fatal("the palette should stay underneath as the source")
	}
}

func TestFilePathPopupHistoryOpensSurface(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "File history")
	m = typeRunes(t, m, "README.md")
	m, _ = send(m, keyType(tea.KeyEnter))
	if layerOf[*filePathPopup](m) != nil || layerOf[*commandPalette](m) != nil {
		t.Fatal("submit must unwind both the popup and the palette")
	}
	hv := layerOf[*historyView](m)
	if hv == nil {
		t.Fatal("submit should open the history surface")
	}
	if hv.ctx.path != "README.md" || hv.ctx.rev != "" {
		t.Errorf("navContext = %+v, want {path:README.md rev:}", hv.ctx)
	}
}

func TestFilePathPopupBlameOpensSurface(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "File blame")
	m = typeRunes(t, m, "README.md")
	m, _ = send(m, keyType(tea.KeyEnter))
	bv := layerOf[*blameView](m)
	if bv == nil {
		t.Fatal("submit should open the blame surface")
	}
	if bv.ctx.path != "README.md" || bv.ctx.rev != "" {
		t.Errorf("navContext = %+v, want {path:README.md rev:}", bv.ctx)
	}
}

func TestFilePathPopupEmptyKeepsOpen(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m, _ = send(m, keyType(tea.KeyEnter)) // empty input
	if layerOf[*filePathPopup](m) == nil {
		t.Fatal("enter with an empty path must keep the popup open")
	}
}

func TestFilePathPopupEscRevealsPalette(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "File history")
	m, _ = send(m, keyType(tea.KeyEsc))
	if layerOf[*filePathPopup](m) != nil {
		t.Fatal("esc should close the file-path popup")
	}
	if p := layerOf[*commandPalette](m); p == nil || p != m.topLayer() {
		t.Fatal("esc should reveal the palette beneath")
	}
}

func TestFilePathPopupAllowsSpaces(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = m.openFilePathPopup(filePathHistory)
	m, _ = send(m, key("a"))
	m, _ = send(m, keyType(tea.KeySpace))
	m, _ = send(m, key("b"))
	p := layerOf[*filePathPopup](m)
	if p == nil || p.input.Value() != "a b" {
		t.Fatalf("path popup must accept spaces; input=%q", p.input.Value())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestPaletteFileHistory|TestFilePathPopup'`
Expected: FAIL — `undefined: filePathPopup` / `openFilePathPopup`.

- [ ] **Step 3: Add the popup type + methods**

Append to `internal/tui/file_path_popup.go` (add imports `tea "github.com/charmbracelet/bubbletea"`):

```go
// filePathPopup takes a file path and opens the history or blame surface for it.
// Reached from the command palette ("File history" / "File blame"). Mirrors
// gotoCommitPopup but does no pre-validation — a bogus path opens the surface,
// which already renders "(no history)" / a git error.
type filePathPopup struct {
	popupMax
	kind  filePathKind
	input textfield
}

func (m Model) openFilePathPopup(kind filePathKind) (Model, tea.Cmd) {
	return m.pushLayer(&filePathPopup{kind: kind, input: newTextField("")}), nil
}

func (p *filePathPopup) title() string {
	if p.kind == filePathBlame {
		return "File blame"
	}
	return "File history"
}

func (p *filePathPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		rel := repoRelPath(m.currentWorktree, p.input.Value())
		if rel == "" { // nothing to open; keep the popup open
			return m, nil
		}
		// Unwind this popup and, if the palette launched us, the palette too — the
		// full-screen surface must open over the base, not a stale popup.
		m = m.popLayer()
		if _, ok := m.topLayer().(*commandPalette); ok {
			m = m.popLayer()
		}
		ctx := navContext{path: rel, rev: ""}
		if p.kind == filePathBlame {
			bv := newBlameView(ctx)
			m = m.pushLayer(bv)
			return m, m.loadBlameCmd(ctx, bv.tag)
		}
		hv := newHistoryView(ctx)
		m = m.pushLayer(hv)
		return m, m.loadHistoryListCmd(ctx, hv.listTag)
	default:
		p.input.HandleEditKey(msg) // spaces included — do NOT swallow KeySpace
	}
	return m, nil
}

func (p *filePathPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *filePathPopup) box(m Model) string {
	w, _ := m.overlayDims()
	var b strings.Builder
	b.WriteString(p.title() + "\n\n")
	b.WriteString(viewField("path: ", p.input, true, popupContentWidth(w)) + "\n")
	b.WriteString("\n[enter] show  [esc] cancel")
	return modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}
```

- [ ] **Step 4: Wire the two palette entries**

Replace `paletteCommands()` in `internal/tui/command_palette.go` with:

```go
func paletteCommands() []paletteCommand {
	return []paletteCommand{
		{label: "Show commit", keyHint: "#", run: Model.openGotoCommitPopup},
		{label: "File history", run: func(m Model) (Model, tea.Cmd) { return m.openFilePathPopup(filePathHistory) }},
		{label: "File blame", run: func(m Model) (Model, tea.Cmd) { return m.openFilePathPopup(filePathBlame) }},
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestPaletteFileHistory|TestFilePathPopup|TestCommandPalette'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/file_path_popup.go internal/tui/file_path_popup_test.go internal/tui/command_palette.go
git commit -m "feat(tui): File history / File blame palette commands (filePathPopup)"
```

---

### Task 3: `repoPathPopup` + Open repo palette entry

**Files:**
- Create: `internal/tui/repo_path_popup.go`
- Modify: `internal/tui/model.go` (add `case repoResolvedMsg:` in `Model.Update`'s message type-switch, beside the `gotoCommitResolvedMsg` arm at ~line 431)
- Modify: `internal/tui/command_palette.go` (add the "Open repo" entry)
- Test: `internal/tui/repo_path_popup_test.go`

**Interfaces:**
- Consumes: `domain.OpenTUI`, `Service.TopLevel`, `reRoot`, `commandPalette`, `layerOf` (existing).
- Produces: `type repoPathPopup`; `func (m Model) openRepoPathPopup() (Model, tea.Cmd)`; `type repoResolvedMsg struct{ path, top string; err error }`; `func (m Model) resolvedRepoPath(p *repoPathPopup, msg repoResolvedMsg) (tea.Model, tea.Cmd)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/repo_path_popup_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPaletteOpenRepoOpensPopup(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "Open repo")
	if layerOf[*repoPathPopup](m) == nil {
		t.Fatal("Open repo should open the repo-path popup")
	}
	if layerOf[*commandPalette](m) == nil {
		t.Fatal("the palette should stay underneath")
	}
}

func TestRepoPathPopupGoodPathReRoots(t *testing.T) {
	dir, _ := newRepoDir(t)
	want := gitOut(t, dir, "rev-parse", "--show-toplevel")

	m := footerModel()
	m, _ = m.openRepoPathPopup()
	m = typeRunes(t, m, dir)
	m, cmd := send(m, keyType(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter on a non-empty path should fire the resolve cmd")
	}
	m, _ = send(m, cmd()) // run the real TopLevel probe + reRoot
	if layerOf[*repoPathPopup](m) != nil {
		t.Fatal("a valid repo path should close the popup")
	}
	if m.switchTarget != want {
		t.Errorf("switchTarget = %q, want the resolved top-level %q", m.switchTarget, want)
	}
	if !m.loading {
		t.Error("reRoot should put the model into the loading state")
	}
}

func TestRepoPathPopupSubdirResolvesToRoot(t *testing.T) {
	dir, _ := newRepoDir(t)
	want := gitOut(t, dir, "rev-parse", "--show-toplevel")
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	m := footerModel()
	m, _ = m.openRepoPathPopup()
	m = typeRunes(t, m, sub)
	m, cmd := send(m, keyType(tea.KeyEnter))
	m, _ = send(m, cmd())
	if m.switchTarget != want {
		t.Errorf("a subdirectory path should resolve to the repo root %q, got %q", want, m.switchTarget)
	}
}

func TestRepoPathPopupBadPathInlineError(t *testing.T) {
	nonRepo := t.TempDir() // not a git repo

	m := footerModel()
	m, _ = m.openRepoPathPopup()
	m = typeRunes(t, m, nonRepo)
	m, cmd := send(m, keyType(tea.KeyEnter))
	m, _ = send(m, cmd())
	p := layerOf[*repoPathPopup](m)
	if p == nil {
		t.Fatal("a non-repo path must keep the popup open")
	}
	if p.err == "" {
		t.Error("a non-repo path must set an inline error")
	}
	if p.resolving {
		t.Error("resolving should be cleared after the result lands")
	}
	if m.switchTarget != "" {
		t.Error("a failed validation must not switch repos")
	}
}

func TestRepoPathPopupStaleResolveRejected(t *testing.T) {
	m := footerModel()
	m, _ = m.openRepoPathPopup()
	m = typeRunes(t, m, "a")
	m, _ = send(m, keyType(tea.KeyEnter)) // fires resolve for "a"
	m = typeRunes(t, m, "b")              // input is now "ab"
	m, _ = send(m, repoResolvedMsg{path: "a", top: "/x"})
	if layerOf[*repoPathPopup](m) == nil {
		t.Fatal("a stale resolve (input edited) must not close the popup")
	}
	if m.switchTarget == "/x" {
		t.Fatal("a stale resolve must not switch repos")
	}
}

func TestRepoPathPopupEscCloses(t *testing.T) {
	m := footerModel()
	m, _ = m.openRepoPathPopup()
	m, _ = send(m, keyType(tea.KeyEsc))
	if layerOf[*repoPathPopup](m) != nil {
		t.Fatal("esc should close the repo-path popup")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestRepoPathPopup`
Expected: FAIL — `undefined: repoPathPopup` etc.

- [ ] **Step 3: Create the popup**

Create `internal/tui/repo_path_popup.go`:

```go
package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

// repoPathPopup takes a filesystem path to a git repository (any path inside one)
// and switches gg to it. Reached from the command palette's "Open repo". The path
// is validated + normalized off the UI thread via TopLevel before the switch: a
// non-repo path shows an inline error and keeps the popup open (no half-switch on
// a typo). Distinct from the MRU repoPopup (R), which only lists previously
// opened repos.
type repoPathPopup struct {
	popupMax
	input     textfield
	err       string // inline error from the last failed validation; "" = none
	resolving bool   // a validation cmd is in flight
}

func (m Model) openRepoPathPopup() (Model, tea.Cmd) {
	return m.pushLayer(&repoPathPopup{input: newTextField("")}), nil
}

// repoResolvedMsg carries the result of validating a typed repo path. path is the
// exact text submitted (the tag-gate key, before ~ expansion); top is the
// resolved repo root on success; err is non-nil when the path is not in a repo.
type repoResolvedMsg struct {
	path string
	top  string
	err  error
}

// expandHome expands a leading ~ or ~/ to the user's home dir. os.UserHomeDir
// (not $HOME) is used so it works on Windows too.
func expandHome(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func (m Model) resolveRepoCmd(raw string) tea.Cmd {
	return func() tea.Msg {
		svc := domain.OpenTUI(expandHome(raw))
		top, err := svc.TopLevel(context.Background())
		return repoResolvedMsg{path: raw, top: top, err: err}
	}
}

func (p *repoPathPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		raw := strings.TrimSpace(p.input.Value())
		if raw == "" { // nothing to resolve; keep the popup open
			return m, nil
		}
		p.resolving = true
		p.err = ""
		return m, m.resolveRepoCmd(raw)
	default:
		if p.input.HandleEditKey(msg) { // spaces included — do NOT swallow KeySpace
			p.err = "" // editing clears the stale error
		}
	}
	return m, nil
}

// resolvedRepoPath applies a validation result. Tag-gated by the caller: acts
// only when this popup is on top and its input still equals msg.path.
func (m Model) resolvedRepoPath(p *repoPathPopup, msg repoResolvedMsg) (tea.Model, tea.Cmd) {
	p.resolving = false
	if msg.err != nil {
		p.err = "not a git repository: " + msg.path
		return m, nil
	}
	m = m.popLayer() // the repo popup
	if _, ok := m.topLayer().(*commandPalette); ok {
		m = m.popLayer() // the palette that launched it
	}
	return m.reRoot(msg.top)
}

func (p *repoPathPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *repoPathPopup) box(m Model) string {
	w, _ := m.overlayDims()
	var b strings.Builder
	b.WriteString("Open repo\n\n")
	b.WriteString(viewField("path: ", p.input, true, popupContentWidth(w)) + "\n")
	if p.err != "" {
		b.WriteString("\n" + errorStyle.Render(p.err) + "\n")
	}
	b.WriteString("\n[enter] open  [esc] cancel")
	return modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}
```

- [ ] **Step 4: Add the message dispatch arm**

In `internal/tui/model.go`, immediately after the `gotoCommitResolvedMsg` arm (the block ending `return m.resolvedGotoCommit(p, msg)` near line 438), add:

```go
	case repoResolvedMsg:
		p := layerOf[*repoPathPopup](m)
		// Tag-gate by the submitted text: only act if this popup is still on top
		// and its input is unchanged (a since-edited field discards a stale result).
		if p == nil || p != m.topLayer() || strings.TrimSpace(p.input.Value()) != msg.path {
			return m, nil
		}
		return m.resolvedRepoPath(p, msg)
```

(`strings` is already imported in `model.go`.)

- [ ] **Step 5: Wire the "Open repo" palette entry**

Replace `paletteCommands()` in `internal/tui/command_palette.go` with:

```go
func paletteCommands() []paletteCommand {
	return []paletteCommand{
		{label: "Show commit", keyHint: "#", run: Model.openGotoCommitPopup},
		{label: "File history", run: func(m Model) (Model, tea.Cmd) { return m.openFilePathPopup(filePathHistory) }},
		{label: "File blame", run: func(m Model) (Model, tea.Cmd) { return m.openFilePathPopup(filePathBlame) }},
		{label: "Open repo", run: func(m Model) (Model, tea.Cmd) { return m.openRepoPathPopup() }},
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestRepoPathPopup|TestPaletteOpenRepo'`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/repo_path_popup.go internal/tui/repo_path_popup_test.go internal/tui/model.go internal/tui/command_palette.go
git commit -m "feat(tui): Open repo palette command (repoPathPopup, TopLevel-validated reRoot)"
```

---

### Task 4: Find palette entry (fuzzy finder launcher)

**Files:**
- Modify: `internal/tui/command_palette.go` (add "Find" entry)
- Test: `internal/tui/file_path_popup_test.go` (add one test)

**Interfaces:**
- Consumes: `openFileFinder() (Model, tea.Cmd)`, `fileFinderPopup` (existing).

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/file_path_popup_test.go`:

```go
func TestPaletteFindOpensFinder(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "Find")
	if layerOf[*fileFinderPopup](m) == nil {
		t.Fatal("Find should open the fuzzy file finder")
	}
	if layerOf[*commandPalette](m) != nil {
		t.Fatal("Find replaces the palette (it does not stay beneath)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestPaletteFindOpensFinder`
Expected: FAIL — `palette has no command "Find"`.

- [ ] **Step 3: Wire the "Find" entry**

Replace `paletteCommands()` in `internal/tui/command_palette.go` with (Find between File blame and Open repo):

```go
func paletteCommands() []paletteCommand {
	return []paletteCommand{
		{label: "Show commit", keyHint: "#", run: Model.openGotoCommitPopup},
		{label: "File history", run: func(m Model) (Model, tea.Cmd) { return m.openFilePathPopup(filePathHistory) }},
		{label: "File blame", run: func(m Model) (Model, tea.Cmd) { return m.openFilePathPopup(filePathBlame) }},
		{label: "Find", keyHint: "F", run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openFileFinder() }},
		{label: "Open repo", run: func(m Model) (Model, tea.Cmd) { return m.openRepoPathPopup() }},
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestPaletteFindOpensFinder`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/command_palette.go internal/tui/file_path_popup_test.go
git commit -m "feat(tui): Find palette command (opens the fuzzy file finder)"
```

---

### Task 5: Move Git config explorer into the palette

**Files:**
- Modify: `internal/tui/command_palette.go` (add "Git config explorer" entry)
- Modify: `internal/tui/settings_popup.go` (delete `settingsMenuGitConfig` const, its slice entry, its `case`)
- Modify: `internal/tui/gitconfig_popup_test.go:22-44` (repoint `openExplorer` to the palette)
- Test: `internal/tui/file_path_popup_test.go` (add a palette test)

**Interfaces:**
- Consumes: `openGitConfigExplorer() (Model, tea.Cmd)`, `gitConfigPopup` (existing).

- [ ] **Step 1: Write / repoint the tests**

Add to `internal/tui/file_path_popup_test.go`:

```go
func TestPaletteGitConfigOpensExplorer(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "Git config explorer")
	if layerOf[*gitConfigPopup](m) == nil {
		t.Fatal("Git config explorer should open the explorer popup")
	}
	if layerOf[*commandPalette](m) != nil {
		t.Fatal("it replaces the palette")
	}
}
```

Replace the `openExplorer` helper in `internal/tui/gitconfig_popup_test.go` (lines 22-44) with the palette-driven version:

```go
// openExplorer drives the command palette → "Git config explorer", then delivers
// the rows as if the background read landed.
func openExplorer(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = palettePick(t, m, "Git config explorer")
	if layerOf[*gitConfigPopup](m) == nil {
		t.Fatal("palette must open the explorer")
	}
	u, _ := m.Update(gitConfigRowsMsg{gen: m.gitConfigGen, rows: explorerRows()})
	return u.(Model)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestPaletteGitConfig|TestExplorer'`
Expected: FAIL — `palette has no command "Git config explorer"` (the palette entry does not exist yet).

- [ ] **Step 3: Remove the Settings menu row + const**

In `internal/tui/settings_popup.go`:

1. Delete the const line `settingsMenuGitConfig   = "Git config explorer"` (line 55).
2. In the `settingsMenu` slice (line 59), remove the trailing `, settingsMenuGitConfig`.
3. Delete the two-line `case settingsMenuGitConfig:` arm (lines 387-388):

```go
			case settingsMenuGitConfig:
				return m.openGitConfigExplorer()
```

Leave `openGitConfigExplorer` itself untouched.

- [ ] **Step 4: Add the palette entry**

Replace `paletteCommands()` in `internal/tui/command_palette.go` with (Git config after Open repo):

```go
func paletteCommands() []paletteCommand {
	return []paletteCommand{
		{label: "Show commit", keyHint: "#", run: Model.openGotoCommitPopup},
		{label: "File history", run: func(m Model) (Model, tea.Cmd) { return m.openFilePathPopup(filePathHistory) }},
		{label: "File blame", run: func(m Model) (Model, tea.Cmd) { return m.openFilePathPopup(filePathBlame) }},
		{label: "Find", keyHint: "F", run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openFileFinder() }},
		{label: "Open repo", run: func(m Model) (Model, tea.Cmd) { return m.openRepoPathPopup() }},
		{label: "Git config explorer", run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openGitConfigExplorer() }},
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestPaletteGitConfig|TestExplorer|TestSettings'`
Expected: PASS. (If a compile error names `settingsMenuGitConfig`, a reference was missed — grep `grep -rn settingsMenuGitConfig internal/tui` should return nothing.)

- [ ] **Step 6: Commit**

```bash
git add internal/tui/command_palette.go internal/tui/settings_popup.go internal/tui/gitconfig_popup_test.go internal/tui/file_path_popup_test.go
git commit -m "feat(tui): move Git config explorer from Settings to the command palette"
```

---

### Task 6: Move Set up agent skills into the palette

**Files:**
- Modify: `internal/tui/settings_popup.go` (add `pickerFromPalette bool`; two-branch picker `esc`; delete `settingsMenuAgents` const + slice entry + `case`)
- Modify: `internal/tui/command_palette.go` (add "Set up agent skills (using-gg)" entry)
- Modify: `internal/tui/settings_popup_test.go:50` (first selected row is now "External tools")
- Test: `internal/tui/file_path_popup_test.go` (add palette tests)

**Interfaces:**
- Consumes: `openSettings() (Model, tea.Cmd)`, `openAgentPicker() Model`, `settingsPopup` (existing).
- Produces: `settingsPopup.pickerFromPalette bool`.

- [ ] **Step 1: Write / fix the tests**

Add to `internal/tui/file_path_popup_test.go`:

```go
func TestPaletteAgentSkillsOpensPickerDirect(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "Set up agent skills (using-gg)")
	sp := layerOf[*settingsPopup](m)
	if sp == nil || !sp.picker || !sp.pickerFromPalette {
		t.Fatal("agent skills should open Settings pre-set to the palette-launched picker")
	}
	if layerOf[*commandPalette](m) != nil {
		t.Fatal("it replaces the palette")
	}
}

func TestPaletteAgentSkillsEscReturnsToBase(t *testing.T) {
	m := gotoModel(t, gotoFullHash)
	m, _ = palettePick(t, m, "Set up agent skills (using-gg)")
	m, _ = send(m, keyType(tea.KeyEsc))
	if layerOf[*settingsPopup](m) != nil {
		t.Fatal("esc from a palette-launched picker must close Settings entirely (return to base)")
	}
}
```

In `internal/tui/settings_popup_test.go`, change line 50 from:

```go
	sel := lineWith(out, "Set up agent skills") // settingsMenuAgents, selected (menuSel 0)
```

to:

```go
	sel := lineWith(out, "External tools") // settingsMenuTools, now the first row (menuSel 0)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestPaletteAgentSkills|TestSettings'`
Expected: FAIL — `palette has no command "Set up agent skills (using-gg)"` / `pickerFromPalette` undefined.

- [ ] **Step 3: Add `pickerFromPalette` + two-branch esc**

In `internal/tui/settings_popup.go`:

1. In the `settingsPopup` struct, add the field right after `picker`:

```go
	picker           bool // false = menu screen, true = agent picker
	pickerFromPalette bool // true = picker opened from the command palette → esc returns to base, not the menu
```

2. Replace the picker `esc` branch (currently lines 307-310):

```go
		if p.picker {
			p.picker = false
			return m, nil
		}
```

with:

```go
		if p.picker {
			if p.pickerFromPalette { // launched from the palette → esc backs out to base
				m = m.popLayer()
				return m, nil
			}
			p.picker = false // launched from the , menu → esc returns to the menu
			return m, nil
		}
```

- [ ] **Step 4: Remove the Settings menu row + const**

In `internal/tui/settings_popup.go`:

1. Delete the const line `settingsMenuAgents      = "Set up agent skills (using-gg)"` (line 41).
2. In the `settingsMenu` slice (line 59), remove the leading `settingsMenuAgents, ` so the slice now starts with `settingsMenuTools`.
3. Delete the two-line `case settingsMenuAgents:` arm (lines 348-349):

```go
				case settingsMenuAgents:
					return m.openAgentPicker(), nil
```

Leave `openAgentPicker` itself untouched (now reached from the palette).

- [ ] **Step 5: Add the palette entry (final registry)**

Replace `paletteCommands()` in `internal/tui/command_palette.go` with the final seven-entry form:

```go
func paletteCommands() []paletteCommand {
	return []paletteCommand{
		{label: "Show commit", keyHint: "#", run: Model.openGotoCommitPopup},
		{label: "File history", run: func(m Model) (Model, tea.Cmd) { return m.openFilePathPopup(filePathHistory) }},
		{label: "File blame", run: func(m Model) (Model, tea.Cmd) { return m.openFilePathPopup(filePathBlame) }},
		{label: "Find", keyHint: "F", run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openFileFinder() }},
		{label: "Open repo", run: func(m Model) (Model, tea.Cmd) { return m.openRepoPathPopup() }},
		{label: "Git config explorer", run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openGitConfigExplorer() }},
		{label: "Set up agent skills (using-gg)", run: func(m Model) (Model, tea.Cmd) {
			m = m.popLayer()
			m, cmd := m.openSettings()
			m = m.openAgentPicker()
			if sp := layerOf[*settingsPopup](m); sp != nil {
				sp.pickerFromPalette = true
			}
			return m, cmd
		}},
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestPaletteAgentSkills|TestSettings'`
Expected: PASS. Then confirm no dangling refs: `grep -rn settingsMenuAgents internal/tui` returns nothing.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/command_palette.go internal/tui/settings_popup.go internal/tui/settings_popup_test.go internal/tui/file_path_popup_test.go
git commit -m "feat(tui): move Set up agent skills from Settings to the command palette"
```

---

### Task 7: Registry order test, docs, full suite

**Files:**
- Test: `internal/tui/command_palette_test.go` (full-registry assertion)
- Modify: `CHANGELOG.md`
- Modify: `CLAUDE.md` (palette growth + the two Settings→palette moves, in the `tui` row)

**Interfaces:**
- Consumes: `paletteCommands()` (final form from Task 6).

- [ ] **Step 1: Write the registry test**

Append to `internal/tui/command_palette_test.go`:

```go
func TestPaletteRegistryOrder(t *testing.T) {
	want := []struct{ label, keyHint string }{
		{"Show commit", "#"},
		{"File history", ""},
		{"File blame", ""},
		{"Find", "F"},
		{"Open repo", ""},
		{"Git config explorer", ""},
		{"Set up agent skills (using-gg)", ""},
	}
	cmds := paletteCommands()
	if len(cmds) != len(want) {
		t.Fatalf("palette has %d commands, want %d", len(cmds), len(want))
	}
	for i, w := range want {
		if cmds[i].label != w.label || cmds[i].keyHint != w.keyHint {
			t.Errorf("cmd %d = {%q,%q}, want {%q,%q}", i, cmds[i].label, cmds[i].keyHint, w.label, w.keyHint)
		}
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/tui/ -run TestPaletteRegistryOrder`
Expected: PASS.

- [ ] **Step 3: Update CHANGELOG.md**

Add under the unreleased/top section:

```markdown
- Command palette (`ctrl+p`) gains six commands: File history and File blame
  (type a path → the history/blame view), Find (the fuzzy file finder), Open repo
  (type a path to a repo not opened before → switch to it), plus Git config
  explorer and Set up agent skills, both moved out of the Settings (`,`) menu.
```

- [ ] **Step 4: Update CLAUDE.md**

In the `tui` package-map row, note that the command palette now carries File
history / File blame (a `filePathPopup` → history/blame surface), Find, Open repo
(a `repoPathPopup` validating via `TopLevel` then `reRoot`), and the Git config
explorer + agent-skills picker moved from Settings (the picker opens Settings with
`pickerFromPalette` so `esc` returns to base).

- [ ] **Step 5: Run the full suite**

Run: `./test.sh` (from the worktree). If time permits, `./test.sh race`.
Expected: all stages green (vet+gofmt → unit → e2e).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/command_palette_test.go CHANGELOG.md CLAUDE.md
git commit -m "test(tui): palette registry order; docs for palette commands"
```

---

## Self-Review

**Spec coverage:**
- File history / File blame popup → Tasks 1–2. ✓
- Lenient path normalization (`repoRelPath`, abs/`./`→repo-relative) → Task 1. ✓
- Open repo path popup, `TopLevel` validate + normalize, `reRoot`, inline error, stale-drop → Task 3. ✓ (`repoResolvedMsg` dispatch arm mirrors `gotoCommitResolvedMsg`.)
- `~` expansion via `os.UserHomeDir()` → Task 3 (`expandHome`). ✓
- Find / Git config / Agent skills launchers → Tasks 4–6. ✓
- Settings removals (rows + cases + consts) → Tasks 5–6. ✓
- Agent picker as Settings sub-state, `pickerFromPalette` esc-to-base → Task 6. ✓
- No pre-validation on file paths (history "(no history)", blame error) → Task 2 (no validation path). ✓
- Space handling (no `KeySpace` swallow) → Task 2 (`TestFilePathPopupAllowsSpaces`). ✓
- Registry order → Task 7. ✓
- Docs → Task 7. ✓

**Placeholder scan:** No TBD/TODO; every code step shows complete code. ✓

**Type consistency:** `filePathKind`/`filePathHistory`/`filePathBlame` (Task 1) used in Tasks 2/6; `repoResolvedMsg{path,top,err}` produced and consumed in Task 3; `openFilePathPopup`/`openRepoPathPopup`/`resolvedRepoPath` signatures consistent across tasks; `palettePick` defined in Task 2 and reused in Tasks 3–6 (same package). `paletteCommands()` shown in full at each modifying task, ending at the seven-entry form asserted in Task 7. ✓
