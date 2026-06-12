# Repo Switcher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Jump between known repositories — an auto-tracked MRU registry, a TUI popup picker on `R` that re-roots in place, and a `gg repo list|switch` CLI, with the shell following via the existing `--cwd-file` bridge.

**Architecture:** New shared package `internal/repos` (TOML registry in XDG state, atomic writes, lazy pruning) consumed by both frontends. The TUI popup follows the worktree-popup pattern (pointer field, key capture, `overlayCenter`); switching is the existing `reRoot(path)` primitive. **No engine involvement.** Spec: `docs/superpowers/specs/2026-06-12-repo-switcher-design.md`.

**Tech Stack:** Go 1.26, `pelletier/go-toml/v2`, Bubble Tea; real-git test helpers (`newRepoDir`, `loadedModel`, `keyMsg`, `newCLIRepo`).

**Branch:** Create `feat/repo-switcher` off `main` before Task 1.

## File Structure

- `internal/repos/repos.go` (create) — registry: `Entry`, `Load`, `Touch`, `Remove`, `Name`, `DefaultStatePath`; atomic write (the `internal/config/state.go` temp+rename pattern).
- `internal/repos/repos_test.go` (create).
- `internal/tui/repo_popup.go` (create) — popup state, key handler, renderer, `ageString`.
- `internal/tui/repo_popup_test.go` (create).
- `internal/tui/model.go` (modify) — `repoPopup` + `statePath` fields, `R` key, routing.
- `internal/tui/view.go` (modify) — popup overlay branch; footer `[R]epo`.
- `internal/tui/load.go` (modify) — best-effort `Touch` during load.
- `internal/tui/run.go` (modify) — wire the real state path (tests stay hermetic).
- `internal/cli/repo.go` (create) — `cmdRepo` (list/switch).
- `internal/cli/repo_test.go` (create).
- `internal/cli/cli.go` (modify) — `RepoStatePath` var, touch-on-run, `repo` registration.
- `cmd/gg/main.go` (modify) — set `cli.RepoStatePath`; fix the stale help string.
- Docs: `CHANGELOG.md`, `README.md`, `CLAUDE.md`.

**Hermeticity rule (load-bearing):** recording is a no-op when the state path is empty. `tui.New` leaves `Model.statePath` empty and `cli.RepoStatePath` defaults to empty, so the existing test suites never write the developer's real `~/.local/state/gg/repos.toml`. Only `tui.Run` and `cmd/gg/main.go` (production entry points) wire `repos.DefaultStatePath()`. Tests that exercise recording set a `t.TempDir()` path explicitly.

---

### Task 1: `internal/repos` registry package

**Files:**
- Create: `internal/repos/repos.go`
- Test: `internal/repos/repos_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/repos/repos_test.go`:

```go
package repos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tmpState returns a state-file path inside a fresh temp dir (file not created).
func tmpState(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "sub", "repos.toml") // parent missing on purpose
}

func TestTouchCreatesAndLoadIsMRUFirst(t *testing.T) {
	state := tmpState(t)
	a, b := t.TempDir(), t.TempDir()
	if err := Touch(state, a, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := Touch(state, b, time.Unix(2000, 0)); err != nil {
		t.Fatal(err)
	}
	got := Load(state)
	if len(got) != 2 || got[0].Path != b || got[1].Path != a {
		t.Fatalf("MRU order wrong: %+v", got)
	}
}

func TestTouchDedupesAndBumps(t *testing.T) {
	state := tmpState(t)
	a, b := t.TempDir(), t.TempDir()
	_ = Touch(state, a, time.Unix(1000, 0))
	_ = Touch(state, b, time.Unix(2000, 0))
	if err := Touch(state, a, time.Unix(3000, 0)); err != nil {
		t.Fatal(err)
	}
	got := Load(state)
	if len(got) != 2 {
		t.Fatalf("dedupe failed: %+v", got)
	}
	if got[0].Path != a || !got[0].LastOpened.Equal(time.Unix(3000, 0)) {
		t.Fatalf("bump failed: %+v", got[0])
	}
}

func TestLoadPrunesDeadPaths(t *testing.T) {
	state := tmpState(t)
	alive := t.TempDir()
	dead := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(dead, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = Touch(state, alive, time.Unix(1000, 0))
	_ = Touch(state, dead, time.Unix(2000, 0))
	if err := os.RemoveAll(dead); err != nil {
		t.Fatal(err)
	}
	got := Load(state)
	if len(got) != 1 || got[0].Path != alive {
		t.Fatalf("dead path not pruned: %+v", got)
	}
}

func TestRemoveForgetsEntry(t *testing.T) {
	state := tmpState(t)
	a, b := t.TempDir(), t.TempDir()
	_ = Touch(state, a, time.Unix(1000, 0))
	_ = Touch(state, b, time.Unix(2000, 0))
	if err := Remove(state, a); err != nil {
		t.Fatal(err)
	}
	got := Load(state)
	if len(got) != 1 || got[0].Path != b {
		t.Fatalf("remove failed: %+v", got)
	}
	// Removing an absent path is not an error.
	if err := Remove(state, filepath.Join(t.TempDir(), "never")); err != nil {
		t.Fatalf("remove of absent entry should be nil, got %v", err)
	}
}

func TestCorruptStateActsEmpty(t *testing.T) {
	state := filepath.Join(t.TempDir(), "repos.toml")
	if err := os.WriteFile(state, []byte("not [valid toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Load(state); len(got) != 0 {
		t.Fatalf("corrupt state should act empty, got %+v", got)
	}
	// And the next Touch rewrites it whole.
	a := t.TempDir()
	if err := Touch(state, a, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if got := Load(state); len(got) != 1 || got[0].Path != a {
		t.Fatalf("touch after corruption failed: %+v", got)
	}
}

func TestEmptyStatePathDisablesRecording(t *testing.T) {
	if err := Touch("", t.TempDir(), time.Now()); err != nil {
		t.Fatalf("empty state path must be a silent no-op, got %v", err)
	}
	if got := Load(""); len(got) != 0 {
		t.Fatalf("Load(\"\") should be empty, got %+v", got)
	}
	if err := Remove("", "/x"); err != nil {
		t.Fatalf("Remove with empty path must be nil, got %v", err)
	}
}

func TestNoTempLitterAfterWrites(t *testing.T) {
	state := tmpState(t)
	_ = Touch(state, t.TempDir(), time.Unix(1000, 0))
	entries, err := os.ReadDir(filepath.Dir(state))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "repos-") && e.Name() != "repos.toml" {
			t.Fatalf("temp litter left behind: %s", e.Name())
		}
	}
}

func TestNameIsBase(t *testing.T) {
	if got := Name(Entry{Path: "/a/b/mono"}); got != "mono" {
		t.Fatalf("Name = %q, want mono", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/repos/ -v`
Expected: FAIL — package doesn't exist / symbols undefined.

- [ ] **Step 3: Implement**

Create `internal/repos/repos.go`:

```go
// Package repos maintains gigagit's machine-local registry of recently opened
// repositories — the data behind the repo switcher. The registry is state, not
// config: it lives under the user's state dir and is never committed.
package repos

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Entry is one known repository.
type Entry struct {
	Path       string    `toml:"path"`        // absolute top-level path
	LastOpened time.Time `toml:"last_opened"` // MRU sort key
}

// Name is an Entry's display name.
func Name(e Entry) string { return filepath.Base(e.Path) }

// registry is the on-disk shape of repos.toml.
type registry struct {
	Repos []Entry `toml:"repos"`
}

// DefaultStatePath resolves the platform-appropriate registry location:
// %LocalAppData%/gg/repos.toml on Windows, else $XDG_STATE_HOME/gg/repos.toml,
// else ~/.local/state/gg/repos.toml. "" (recording disabled) if no home exists.
func DefaultStatePath() string {
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "repos.toml")
		}
	}
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "repos.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "repos.toml")
}

// read loads the raw registry. A missing or corrupt file acts as empty — the
// registry is best-effort history and must never block gg.
func read(statePath string) registry {
	var reg registry
	data, err := os.ReadFile(statePath)
	if err != nil {
		return reg
	}
	if err := toml.Unmarshal(data, &reg); err != nil {
		return registry{}
	}
	return reg
}

// alive reports whether an entry's path still exists.
func alive(e Entry) bool {
	_, err := os.Stat(e.Path)
	return err == nil
}

// prune drops entries whose path no longer exists.
func prune(reg registry) registry {
	kept := reg.Repos[:0]
	for _, e := range reg.Repos {
		if alive(e) {
			kept = append(kept, e)
		}
	}
	reg.Repos = kept
	return reg
}

// Load returns the known repos MRU-first (most recently opened first), with
// dead paths dropped. An empty statePath, or a missing/corrupt file, yields an
// empty list. Pruning here is in-memory only; the file is rewritten on the
// next Touch/Remove.
func Load(statePath string) []Entry {
	if statePath == "" {
		return nil
	}
	reg := prune(read(statePath))
	sort.SliceStable(reg.Repos, func(a, b int) bool {
		return reg.Repos[a].LastOpened.After(reg.Repos[b].LastOpened)
	})
	return reg.Repos
}

// Touch records repoPath with now as its LastOpened, deduplicating by cleaned
// absolute path and pruning dead entries, then persists atomically. An empty
// statePath disables recording (a silent no-op) — production entry points wire
// the real path; tests and bare constructors stay hermetic.
func Touch(statePath, repoPath string, now time.Time) error {
	if statePath == "" {
		return nil
	}
	if abs, err := filepath.Abs(repoPath); err == nil {
		repoPath = abs
	}
	repoPath = filepath.Clean(repoPath)

	reg := prune(read(statePath))
	found := false
	for i := range reg.Repos {
		if reg.Repos[i].Path == repoPath {
			reg.Repos[i].LastOpened = now
			found = true
			break
		}
	}
	if !found {
		reg.Repos = append(reg.Repos, Entry{Path: repoPath, LastOpened: now})
	}
	return write(statePath, reg)
}

// Remove forgets repoPath. Removing an absent entry is not an error. The repo
// on disk is never touched — only the registry entry.
func Remove(statePath, repoPath string) error {
	if statePath == "" {
		return nil
	}
	repoPath = filepath.Clean(repoPath)
	reg := read(statePath)
	kept := reg.Repos[:0]
	for _, e := range reg.Repos {
		if e.Path != repoPath {
			kept = append(kept, e)
		}
	}
	reg.Repos = kept
	return write(statePath, reg)
}

// write persists reg via a temp file + rename in the target directory, so a
// concurrent reader never sees a half-written file (the seq-state pattern).
func write(statePath string, reg registry) error {
	dir := filepath.Dir(statePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(reg)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "repos-*.toml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, statePath); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/repos/ -v`
Expected: PASS (8 tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/repos/
go vet ./internal/repos/
git add internal/repos/
git commit -m "feat(repos): MRU registry of recently opened repositories"
```

---

### Task 2: TUI — `R` repo-switcher popup

**Files:**
- Create: `internal/tui/repo_popup.go`
- Modify: `internal/tui/model.go` (fields, `R` key, routing), `internal/tui/view.go` (overlay, footer), `internal/tui/load.go` (Touch), `internal/tui/run.go` (wire state path), `internal/tui/model_test.go` (`keyMsg` ctrl+d)
- Test: `internal/tui/repo_popup_test.go`

Background: the worktree popup is the exemplar — pointer field on Model (value-receiver invariant), key handler that swallows everything, render via `overlayCenter` in `render()` (view.go ~line 69). Routing precedence in `Update`'s `tea.KeyMsg`: modal → `m.popup` (worktree) → **`m.repoPopup` (new)** → filter input → normal keys.

- [ ] **Step 1: Extend the `keyMsg` helper**

In `internal/tui/model_test.go`, add to the `switch s` in `keyMsg`:

```go
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/tui/repo_popup_test.go`:

```go
package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/repos"
)

// seededModel returns a loaded model whose statePath is a temp registry
// containing otherRepo (older) and the model's own repo (newest, via load Touch).
func seededModel(t *testing.T) (Model, string, string) {
	t.Helper()
	dir, repo := newRepoDir(t)
	otherDir, _ := newRepoDir(t)
	state := filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(state, otherDir, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	m := New(repo)
	m.statePath = state
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	_ = dir
	return m, state, otherDir
}

func TestLoadTouchesRegistry(t *testing.T) {
	m, state, _ := seededModel(t)
	entries := repos.Load(state)
	if len(entries) != 2 {
		t.Fatalf("load should have touched the registry: %+v", entries)
	}
	resolvedWant, _ := filepath.EvalSymlinks(m.currentWorktree)
	resolvedGot, _ := filepath.EvalSymlinks(entries[0].Path)
	if resolvedGot != resolvedWant {
		t.Fatalf("MRU head = %q, want the current repo %q", entries[0].Path, m.currentWorktree)
	}
}

func TestRKeyOpensPopupMRUFirst(t *testing.T) {
	m, _, otherDir := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	if m.repoPopup == nil {
		t.Fatal("R should open the repo popup")
	}
	if len(m.repoPopup.entries) != 2 {
		t.Fatalf("popup entries = %+v", m.repoPopup.entries)
	}
	// MRU head is the current repo; the older entry is second.
	resolvedOther, _ := filepath.EvalSymlinks(otherDir)
	resolvedSecond, _ := filepath.EvalSymlinks(m.repoPopup.entries[1].Path)
	if resolvedSecond != resolvedOther {
		t.Fatalf("second entry = %q, want %q", m.repoPopup.entries[1].Path, otherDir)
	}
}

func TestPopupFilterAndSwitch(t *testing.T) {
	m, _, otherDir := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	// Filter down to the other repo by its unique directory base name.
	base := filepath.Base(otherDir)
	for _, r := range base {
		u, _ = m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	if got := len(m.popupVisible()); got != 1 {
		t.Fatalf("filtered visible = %d, want 1 (query %q)", got, m.repoPopup.query)
	}
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.repoPopup != nil {
		t.Fatal("enter should close the popup")
	}
	resolvedWant, _ := filepath.EvalSymlinks(otherDir)
	resolvedGot, _ := filepath.EvalSymlinks(m.switchTarget)
	if resolvedGot != resolvedWant {
		t.Fatalf("switchTarget = %q, want %q", m.switchTarget, otherDir)
	}
}

func TestEnterOnCurrentRepoIsNoOp(t *testing.T) {
	m, _, _ := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	// Selection starts at 0 = MRU head = the current repo.
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.repoPopup != nil {
		t.Fatal("enter should close the popup")
	}
	if m.switchTarget != "" {
		t.Fatalf("must not re-root into the current repo, switchTarget = %q", m.switchTarget)
	}
}

func TestCtrlDRemovesEntry(t *testing.T) {
	m, state, otherDir := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("down")) // select the older (other) repo
	m = u.(Model)
	u, _ = m.Update(keyMsg("ctrl+d"))
	m = u.(Model)
	if len(m.repoPopup.entries) != 1 {
		t.Fatalf("popup should drop the entry, got %+v", m.repoPopup.entries)
	}
	for _, e := range repos.Load(state) {
		resolvedE, _ := filepath.EvalSymlinks(e.Path)
		resolvedOther, _ := filepath.EvalSymlinks(otherDir)
		if resolvedE == resolvedOther {
			t.Fatal("ctrl+d must remove the entry from the state file")
		}
	}
}

func TestPopupEscCancelsAndSwallowsKeys(t *testing.T) {
	m, _, _ := seededModel(t)
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("p")) // would start SmartPull in normal mode
	m = u.(Model)
	if m.running {
		t.Fatal("popup leaked a global key")
	}
	if m.repoPopup.query != "p" {
		t.Fatalf("typed key should filter, query = %q", m.repoPopup.query)
	}
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.repoPopup != nil {
		t.Fatal("esc should close the popup")
	}
}

func TestPopupRendersAndFits(t *testing.T) {
	m, _, _ := seededModel(t)
	m.width, m.height = 80, 24
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	out := m.View()
	if !strings.Contains(out, "Switch repository") {
		t.Fatalf("popup title missing:\n%s", out)
	}
	for i, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := lipgloss.Width(ln); w > m.width {
			t.Fatalf("line %d is %d cols, want <= %d", i, w, m.width)
		}
	}
}

func TestAgeString(t *testing.T) {
	now := time.Unix(100000, 0)
	cases := []struct {
		t    time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-3 * time.Hour), "3h ago"},
		{now.Add(-49 * time.Hour), "2d ago"},
	}
	for _, c := range cases {
		if got := ageString(now, c.t); got != c.want {
			t.Errorf("ageString(%v) = %q, want %q", c.t, got, c.want)
		}
	}
}
```


- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'RKey|PopupFilter|EnterOnCurrent|CtrlD|PopupEsc|PopupRenders|AgeString|LoadTouches' -v`
Expected: FAIL — `repoPopup`, `statePath`, `ageString` undefined.

- [ ] **Step 4: Implement the popup**

Create `internal/tui/repo_popup.go`:

```go
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/repos"
)

// repoPopup is the transient repo-switcher picker opened with R. It holds an
// MRU snapshot taken at open; ctrl+d edits both the snapshot and the registry.
type repoPopup struct {
	entries []repos.Entry
	query   string // case-insensitive substring over name+path
	sel     int    // index into the FILTERED view
	now     time.Time
}

// openRepoPopup snapshots the registry. With no known repos it sets a status
// hint instead of opening an empty picker.
func (m Model) openRepoPopup() (Model, bool) {
	entries := repos.Load(m.statePath)
	if len(entries) == 0 {
		m.statusMsg = "no known repositories yet (gg records them as you open repos)"
		return m, false
	}
	m.repoPopup = &repoPopup{entries: entries, now: time.Now()}
	return m, true
}

// popupVisible returns the filtered entries in display order.
func (m Model) popupVisible() []repos.Entry {
	p := m.repoPopup
	if p == nil {
		return nil
	}
	if p.query == "" {
		return p.entries
	}
	q := strings.ToLower(p.query)
	out := make([]repos.Entry, 0, len(p.entries))
	for _, e := range p.entries {
		if strings.Contains(strings.ToLower(repos.Name(e)), q) ||
			strings.Contains(strings.ToLower(e.Path), q) {
			out = append(out, e)
		}
	}
	return out
}

// updateRepoPopupKey handles all keys while the picker is open. It swallows
// everything (no fallthrough to global handlers).
func (m Model) updateRepoPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.repoPopup
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.repoPopup = nil
		return m, nil
	case tea.KeyUp:
		if p.sel > 0 {
			p.sel--
		}
		return m, nil
	case tea.KeyDown:
		if p.sel < len(m.popupVisible())-1 {
			p.sel++
		}
		return m, nil
	case tea.KeyEnter:
		vis := m.popupVisible()
		m.repoPopup = nil
		if p.sel < 0 || p.sel >= len(vis) {
			return m, nil
		}
		target := vis[p.sel].Path
		if samePathTUI(target, m.currentWorktree) {
			return m, nil // already here
		}
		return m.reRoot(target)
	case tea.KeyCtrlD:
		vis := m.popupVisible()
		if p.sel < 0 || p.sel >= len(vis) {
			return m, nil
		}
		victim := vis[p.sel].Path
		_ = repos.Remove(m.statePath, victim)
		kept := p.entries[:0]
		for _, e := range p.entries {
			if e.Path != victim {
				kept = append(kept, e)
			}
		}
		p.entries = kept
		if n := len(m.popupVisible()); p.sel >= n && n > 0 {
			p.sel = n - 1
		}
		return m, nil
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
		}
		p.sel = 0
		return m, nil
	case tea.KeySpace:
		p.query += " "
		p.sel = 0
		return m, nil
	case tea.KeyRunes:
		p.query += string(msg.Runes)
		p.sel = 0
		return m, nil
	}
	return m, nil
}

// renderRepoPopup draws the picker box (composited by render via overlayCenter).
func (m Model) renderRepoPopup() string {
	p := m.repoPopup
	var b strings.Builder
	b.WriteString("Switch repository")
	if p.query != "" {
		b.WriteString("  /" + p.query)
	}
	b.WriteString("\n\n")
	vis := m.popupVisible()
	if len(vis) == 0 {
		b.WriteString("  (no match)\n")
	}
	for i, e := range vis {
		marker := "  "
		if samePathTUI(e.Path, m.currentWorktree) {
			marker = "● "
		}
		cursor := "  "
		if i == p.sel {
			cursor = "> "
		}
		b.WriteString(fmt.Sprintf("%s%s%s  %s  (%s)\n",
			cursor, marker, repos.Name(e), e.Path, ageString(p.now, e.LastOpened)))
	}
	b.WriteString("\n[enter] switch  [ctrl+d] forget  [esc] cancel")

	w := m.width
	if w <= 0 {
		w = 80
	}
	inner := 56
	if max := w - 8; inner > max {
		inner = max
	}
	if inner < 20 {
		inner = 20
	}
	return modalStyle.Width(inner).Render(strings.TrimRight(b.String(), "\n")) + "\n"
}

// samePathTUI compares two paths after cleaning; symlink divergence is
// tolerated as inequality (worst case: a no-op switch into the same repo).
func samePathTUI(a, b string) bool {
	return strings.TrimRight(a, "/\\") == strings.TrimRight(b, "/\\")
}

// ageString renders a coarse relative age for the picker rows.
func ageString(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
```

- [ ] **Step 5: Wire Model, routing, view, load, run**

(a) `internal/tui/model.go` — add fields next to `popup *worktreePopup`:

```go
	repoPopup *repoPopup
	statePath string // repo-registry location; "" disables recording (tests)
```

(b) Routing: in `Update`'s `tea.KeyMsg` block, immediately AFTER the `if m.popup != nil { ... }` dispatch and BEFORE the filter-input block, add:

```go
		if m.repoPopup != nil {
			return m.updateRepoPopupKey(msg)
		}
```

(c) `R` key: in the normal-key switch, after the `case "esc":` block, add:

```go
		case "R":
			if !m.running && !m.loading {
				if mm, ok := m.openRepoPopup(); ok {
					return mm, nil
				}
				return m, nil
			}
```

(d) `internal/tui/view.go` — in `render()`, after the `if m.popup != nil { ... }` overlay branch, add a parallel branch:

```go
	if m.repoPopup != nil {
		w, h := m.width, m.height
		if w <= 0 {
			w = 80
		}
		if h <= 0 {
			h = 24
		}
		return overlayCenter(bg, m.renderRepoPopup(), w, h)
	}
```

(e) `internal/tui/view.go` — footer string gains `[R]epo` after `[/]filter`:

```go
	footer := truncate("[p]ull [P]ush [s]witch [S]tash [u]ndo [w]orktree [d]elete [o]rder [/]filter [R]epo  •  [tab] focus  [r] reload  [q] quit", g.w)
```

(f) `internal/tui/load.go` — `loadCmd`'s closure needs the state path: at the top of `loadCmd`, capture `statePath := m.statePath` next to `repo := m.repo`. Then inside the returned func, in the existing `if top, topErr := repo.TopLevel(ctx); topErr == nil {` block, add as its first line:

```go
			// Record this repo in the switcher registry (best-effort; "" = off).
			_ = repos.Touch(statePath, top, time.Now())
```

Add imports `time` and `github.com/gigagit/gg/internal/repos` to load.go. (Running inside loadCmd covers process start, every reload, and every reRoot — all off the UI thread.)

(g) `internal/tui/run.go` — wire the real path in `Run`:

```go
	m := New(repo)
	m.statePath = repos.DefaultStatePath()
	p := tea.NewProgram(m, tea.WithAltScreen())
```

(import `github.com/gigagit/gg/internal/repos`).

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'RKey|PopupFilter|EnterOnCurrent|CtrlD|PopupEsc|PopupRenders|AgeString|LoadTouches' -v` then the full `go test ./internal/tui/`.
Expected: PASS (9 new tests), no regressions. Existing tests don't set `statePath`, so they never write real user state (the hermeticity rule).

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/tui/
go vet ./internal/tui/
git add internal/tui/
git commit -m "feat(tui): R opens the repo-switcher popup (MRU, filter, re-root)"
```

---

### Task 3: CLI — `gg repo list|switch`

**Files:**
- Create: `internal/cli/repo.go`
- Modify: `internal/cli/cli.go` (var, touch-on-run, registration), `cmd/gg/main.go` (state path wiring + help string)
- Test: `internal/cli/repo_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/repo_test.go`:

```go
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gigagit/gg/internal/repos"
)

// withState points the package at a temp registry for one test.
func withState(t *testing.T) string {
	t.Helper()
	state := filepath.Join(t.TempDir(), "repos.toml")
	old := RepoStatePath
	RepoStatePath = state
	t.Cleanup(func() { RepoStatePath = old })
	return state
}

func TestRepoListMRUFirst(t *testing.T) {
	state := withState(t)
	a, b := t.TempDir(), t.TempDir()
	_ = repos.Touch(state, a, time.Unix(1000, 0))
	_ = repos.Touch(state, b, time.Unix(2000, 0))

	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"repo", "list"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 2 || !strings.Contains(lines[0], b) || !strings.Contains(lines[1], a) {
		t.Fatalf("list not MRU-first:\n%s", out.String())
	}
	if !strings.Contains(lines[0], filepath.Base(b)+"\t") {
		t.Fatalf("expected <name>\\t<path> format: %q", lines[0])
	}
}

func TestRepoSwitchUniqueMatchWritesCwdFile(t *testing.T) {
	state := withState(t)
	target := filepath.Join(t.TempDir(), "unique-zebra")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = repos.Touch(state, target, time.Unix(1000, 0))

	dir := newCLIRepo(t)
	cwdFile := filepath.Join(t.TempDir(), "cwd")
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"repo", "switch", "zebra"}, strings.NewReader(""), &out, &errb, cwdFile); code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), target) {
		t.Fatalf("stdout should print the path, got %q", out.String())
	}
	got, err := os.ReadFile(cwdFile)
	if err != nil || strings.TrimSpace(string(got)) != target {
		t.Fatalf("cwd-file = %q (%v), want %q", got, err, target)
	}
}

func TestRepoSwitchNoMatchErrors(t *testing.T) {
	withState(t)
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"repo", "switch", "nope"}, strings.NewReader(""), &out, &errb, ""); code == 0 {
		t.Fatal("no match should exit non-zero")
	}
	if !strings.Contains(errb.String(), "no known repository") {
		t.Fatalf("stderr should explain, got %q", errb.String())
	}
}

func TestRepoSwitchAmbiguousListsCandidates(t *testing.T) {
	state := withState(t)
	a := filepath.Join(t.TempDir(), "svc-alpha")
	b := filepath.Join(t.TempDir(), "svc-beta")
	for _, p := range []string{a, b} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = repos.Touch(state, p, time.Unix(1000, 0))
	}
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"repo", "switch", "svc"}, strings.NewReader(""), &out, &errb, ""); code == 0 {
		t.Fatal("ambiguous match should exit non-zero")
	}
	if !strings.Contains(errb.String(), "svc-alpha") || !strings.Contains(errb.String(), "svc-beta") {
		t.Fatalf("stderr should list candidates, got %q", errb.String())
	}
}

func TestAnyCommandTouchesRegistry(t *testing.T) {
	state := withState(t)
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"status"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("status exit = %d, stderr=%s", code, errb.String())
	}
	entries := repos.Load(state)
	if len(entries) != 1 {
		t.Fatalf("running a command should record the repo: %+v", entries)
	}
	wantR, _ := filepath.EvalSymlinks(dir)
	gotR, _ := filepath.EvalSymlinks(entries[0].Path)
	if gotR != wantR {
		t.Fatalf("recorded %q, want %q", entries[0].Path, dir)
	}
}

func TestRepoUnknownSubcommand(t *testing.T) {
	withState(t)
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"repo", "bogus"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatal("unknown repo subcommand should exit 2")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run 'Repo|TouchesRegistry' -v`
Expected: FAIL — `RepoStatePath` undefined (compile error).

- [ ] **Step 3: Implement**

(a) Create `internal/cli/repo.go`:

```go
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gigagit/gg/internal/repos"
)

// cmdRepo dispatches `gg repo <list|switch>` — the repo-switcher registry.
// Switching is frontend state (print + cwd-file), not a git mutation, so no
// engine operation is involved.
func cmdRepo(args []string, stdout, stderr io.Writer, cwdFile string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg repo <list|switch> [args]")
		return 2
	}
	switch args[0] {
	case "list":
		return cmdRepoList(stdout)
	case "switch":
		return cmdRepoSwitch(args[1:], stdout, stderr, cwdFile)
	default:
		fmt.Fprintf(stderr, "repo: unknown subcommand %q (use list or switch)\n", args[0])
		return 2
	}
}

func cmdRepoList(stdout io.Writer) int {
	for _, e := range repos.Load(RepoStatePath) {
		fmt.Fprintf(stdout, "%s\t%s\n", repos.Name(e), e.Path)
	}
	return 0
}

func cmdRepoSwitch(args []string, stdout, stderr io.Writer, cwdFile string) int {
	if len(args) < 1 || args[0] == "" {
		fmt.Fprintln(stderr, "repo switch: a query is required")
		return 2
	}
	q := strings.ToLower(args[0])
	var matches []repos.Entry
	for _, e := range repos.Load(RepoStatePath) {
		if strings.Contains(strings.ToLower(repos.Name(e)), q) ||
			strings.Contains(strings.ToLower(e.Path), q) {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 0:
		fmt.Fprintf(stderr, "repo switch: no known repository matches %q\n", args[0])
		return 1
	case 1:
		fmt.Fprintln(stdout, matches[0].Path)
		if cwdFile != "" {
			_ = os.WriteFile(cwdFile, []byte(matches[0].Path), 0o644)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "repo switch: %q is ambiguous:\n", args[0])
		for _, e := range matches {
			fmt.Fprintf(stderr, "  %s\t%s\n", repos.Name(e), e.Path)
		}
		return 1
	}
}
```

(b) In `internal/cli/cli.go`:

- Add imports `context`, `time`, and `github.com/gigagit/gg/internal/repos`.
- Add the package var (near `commands`):

```go
// RepoStatePath is the repo-switcher registry location. "" disables recording
// and yields an empty registry — cmd/gg wires the real path; tests stay
// hermetic by default.
var RepoStatePath string
```

- In `Run`, after `repo := openRepo(workdir)`, add:

```go
	// Record this repo in the switcher registry (best-effort: errors and
	// non-repo working directories are ignored).
	if RepoStatePath != "" {
		if top, err := repo.TopLevel(context.Background()); err == nil {
			_ = repos.Touch(RepoStatePath, top, time.Now())
		}
	}
```

- Add the dispatch case (before `default:`):

```go
	case "repo":
		return cmdRepo(rest, stdout, stderr, cwdFile)
```

- Add `"repo": true` to the `commands` map.

(c) In `cmd/gg/main.go`:

- Import `github.com/gigagit/gg/internal/repos`.
- In `main()`, before the `cli.IsCommand` dispatch, add:

```go
	cli.RepoStatePath = repos.DefaultStatePath()
```

- Update the unknown-command help string (line ~37) — it is stale (missing `worktree`); make it complete:

```go
		fmt.Fprintln(os.Stderr, "commands: status commit pull push switch stash undo worktree repo inspect (run `gg` with no arguments for the TUI)")
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run 'Repo|TouchesRegistry' -v` then `go test ./internal/cli/ ./cmd/gg/`
Expected: PASS (6 new tests), no regressions.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/cli/ cmd/gg/
go vet ./internal/cli/ ./cmd/gg/
git add internal/cli/ cmd/gg/
git commit -m "feat(cli): gg repo list/switch over the MRU registry"
```

---

### Task 4: Docs + final verification

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`

- [ ] **Step 1: CHANGELOG** — under `## [Unreleased]` → `### Added`, append (before `#### Developer tooling`):

```markdown
#### Repo switcher
- gg auto-records every repository it opens in a machine-local MRU registry
  (`~/.local/state/gg/repos.toml`); dead paths are pruned automatically.
- TUI: `R` opens a switcher popup — filter as you type, `enter` re-roots into
  the chosen repo (the shell follows via `--cwd-file`), `ctrl+d` forgets an
  entry.
- CLI: `gg repo list` and `gg repo switch <query>` (unique substring match
  prints the path and writes `--cwd-file` so a wrapped shell `cd`s there).
```

- [ ] **Step 2: README** — key table row after `/`:

```markdown
| `R` | switch repository (popup: type to filter, `enter` to switch, `ctrl+d` to forget) |
```

and in the CLI command list, after `gg worktree remove ...`:

```markdown
gg repo list
gg repo switch <query>
```

- [ ] **Step 3: CLAUDE.md** — add to the package map table (alphabetical-ish, near `worktree`):

```markdown
| `repos`      | Machine-local MRU registry of opened repositories (XDG state file) behind the repo switcher. |
```

- [ ] **Step 4: Full verification**

```bash
gofmt -l internal/ cmd/        # must print nothing
go vet ./...
go test ./... -race
```

Expected: all clean / PASS.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: record the repo switcher (R popup, gg repo CLI)"
```
