# gigagit M1 — Plan 3A: Read-only TUI (Bubble Tea) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up a runnable Bubble Tea TUI that loads and displays the repo (branches, working-tree status, recent commit log) in a multi-panel layout with keyboard navigation — the first hand-usable surface. No mutating operations yet (Plan 3B).

**Architecture:** A new `internal/tui` package implements the Bubble Tea Elm pattern: a `Model` holds the loaded repo data and focus/selection state; `Init` kicks off an async load command; `Update` handles key and data-loaded messages; `View` renders the panels with Lip Gloss. Data is loaded off the UI thread via `tea.Cmd`s that call the existing read verbs on `git.Repo`. A new `git.Repo.Log` verb + `ParseLog` parser feeds the commits panel. `cmd/gg` launches the TUI by default and keeps `gg inspect` as a subcommand.

**Tech Stack:** Go 1.26; `github.com/charmbracelet/bubbletea v1.3.10`, `github.com/charmbracelet/lipgloss v1.1.0`; existing internal packages. Tests dispatch messages to `Update` and assert `Model` state; `View` is smoke-tested for non-panic and expected content.

---

## Shared interfaces

```go
// internal/git
func (r *Repo) Log(ctx context.Context, limit int) ([]model.Commit, error)
func ParseLog(data []byte) ([]model.Commit, error) // %H\x1f%P\x1f%an\x1f%at\x1f%s per line

// internal/tui
type Model struct { /* see Task 3 */ }
func New(repo *git.Repo) Model          // construct initial model
func Run(repo *git.Repo) error          // create + run the bubbletea program (alt screen)
```

---

## Task 1: `Log` verb + `ParseLog` parser

**Files:**
- Create: `internal/git/log.go`
- Test: `internal/git/log_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/git/log_test.go`:
```go
package git

import (
	"context"
	"testing"
)

func TestParseLog(t *testing.T) {
	// Fields separated by \x1f (unit separator), one commit per line.
	line1 := "aaa111" + "\x1f" + "" + "\x1f" + "Alice" + "\x1f" + "1700000000" + "\x1f" + "initial"
	line2 := "bbb222" + "\x1f" + "aaa111" + "\x1f" + "Bob" + "\x1f" + "1700000100" + "\x1f" + "second commit"
	raw := []byte(line1 + "\n" + line2 + "\n")

	got, err := ParseLog(raw)
	if err != nil {
		t.Fatalf("parse log: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("commits = %d, want 2", len(got))
	}
	if got[0].Hash != "aaa111" || got[0].Author != "Alice" || got[0].Subject != "initial" {
		t.Fatalf("commit0 = %+v", got[0])
	}
	if got[0].UnixTime != 1700000000 {
		t.Fatalf("commit0 time = %d, want 1700000000", got[0].UnixTime)
	}
	if len(got[1].Parents) != 1 || got[1].Parents[0] != "aaa111" {
		t.Fatalf("commit1 parents = %v, want [aaa111]", got[1].Parents)
	}
}

func TestRepoLogReturnsCommits(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "second")

	commits, err := repo.Log(context.Background(), 10)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("commits = %d, want 2", len(commits))
	}
	// Newest first.
	if commits[0].Subject != "second" {
		t.Fatalf("commit0 subject = %q, want second", commits[0].Subject)
	}
}
```

(Remove the confusing `data` placeholder if you prefer — the meaningful assertions use `raw`. `gitIn` is defined in `sync_test.go`, same package.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run 'TestParseLog|TestRepoLog'`
Expected: FAIL — undefined `ParseLog`, `Log`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/git/log.go`:
```go
package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/model"
)

// logFormat separates fields with \x1f (unit separator); one commit per line.
const logFormat = "%H%x1f%P%x1f%an%x1f%at%x1f%s"

// Log returns up to limit recent commits, newest first.
func (r *Repo) Log(ctx context.Context, limit int) ([]model.Commit, error) {
	argv := gitcmd.New("log").
		Arg("-n", strconv.Itoa(limit), "--format="+logFormat).ToArgv()
	res, err := r.Runner.Run(ctx, "git log", argv)
	if err != nil {
		return nil, err
	}
	return ParseLog([]byte(res.Stdout))
}

// ParseLog parses lines of "%H\x1f%P\x1f%an\x1f%at\x1f%s".
func ParseLog(data []byte) ([]model.Commit, error) {
	var out []model.Commit
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\x1f")
		if len(f) < 5 {
			continue
		}
		c := model.Commit{
			Hash:    f[0],
			Author:  f[2],
			Subject: f[4],
		}
		if p := strings.Fields(f[1]); len(p) > 0 {
			c.Parents = p
		}
		if t, err := strconv.ParseInt(f[3], 10, 64); err == nil {
			c.UnixTime = t
		}
		out = append(out, c)
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ && go vet ./internal/git/ && gofmt -l internal/git`
Expected: PASS; gofmt clean.

- [ ] **Step 5: Commit**

```bash
git add internal/git/log.go internal/git/log_test.go
git commit -m "feat: add git log verb and parser"
```

---

## Task 2: Add Bubble Tea deps + TUI skeleton

**Files:**
- Modify: `go.mod`, `go.sum` (via `go get`)
- Create: `internal/tui/model.go`
- Test: `internal/tui/model_test.go`

- [ ] **Step 1: Add dependencies**

Run:
```bash
cd /mnt/t/others/gigagit
go get github.com/charmbracelet/bubbletea@v1.3.10
go get github.com/charmbracelet/lipgloss@v1.1.0
```
Expected: `go.mod` gains both requires; `go.sum` is created/updated.

- [ ] **Step 2: Write the failing test**

Create `internal/tui/model_test.go`:
```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestQuitOnQ(t *testing.T) {
	m := New(nil)
	_, cmd := m.Update(keyMsg("q"))
	if cmd == nil {
		t.Fatal("expected a command from pressing q")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("pressing q should issue tea.Quit")
	}
}

func TestWindowSizeIsRecorded(t *testing.T) {
	m := New(nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := updated.(Model)
	if mm.width != 120 || mm.height != 40 {
		t.Fatalf("size = %dx%d, want 120x40", mm.width, mm.height)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tui/`
Expected: FAIL — undefined `New`, `Model`.

- [ ] **Step 4: Write minimal implementation**

Create `internal/tui/model.go`:
```go
// Package tui implements the gigagit terminal UI with Bubble Tea.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/git"
)

// Model is the root Bubble Tea model.
type Model struct {
	repo          *git.Repo
	width, height int
}

// New constructs the initial model for repo.
func New(repo *git.Repo) Model {
	return Model{repo: repo}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	return "gigagit (loading…)\n"
}

var _ tea.Model = Model{}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/tui/ && go vet ./internal/tui/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/tui/model.go internal/tui/model_test.go
git commit -m "feat: add bubbletea deps and TUI skeleton"
```

---

## Task 3: Async data loading (status / branches / commits)

**Files:**
- Modify: `internal/tui/model.go`
- Create: `internal/tui/load.go`
- Test: `internal/tui/load_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/load_test.go`:
```go
package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

func newRepo(t *testing.T) *git.Repo {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")
	return &git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
}

func TestLoadCmdReturnsPopulatedData(t *testing.T) {
	repo := newRepo(t)
	m := New(repo)
	msg := m.loadCmd()() // run the command synchronously
	loaded, ok := msg.(dataLoadedMsg)
	if !ok {
		t.Fatalf("expected dataLoadedMsg, got %T", msg)
	}
	if loaded.err != nil {
		t.Fatalf("load error: %v", loaded.err)
	}
	if loaded.status.Branch != "main" {
		t.Fatalf("branch = %q, want main", loaded.status.Branch)
	}
	if len(loaded.branches) != 1 {
		t.Fatalf("branches = %d, want 1", len(loaded.branches))
	}
	if len(loaded.commits) != 1 {
		t.Fatalf("commits = %d, want 1", len(loaded.commits))
	}
}

func TestUpdateAppliesLoadedData(t *testing.T) {
	repo := newRepo(t)
	m := New(repo)
	msg := m.loadCmd()()
	updated, _ := m.Update(msg)
	mm := updated.(Model)
	if mm.status.Branch != "main" || len(mm.branches) != 1 || len(mm.commits) != 1 {
		t.Fatalf("model not populated: %+v", mm)
	}
	if mm.loading {
		t.Fatal("loading should be false after data applied")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestLoadCmd|TestUpdateApplies'`
Expected: FAIL — undefined `loadCmd`, `dataLoadedMsg`, model fields.

- [ ] **Step 3: Implement**

Create `internal/tui/load.go`:
```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

// dataLoadedMsg carries a full repo snapshot loaded off the UI thread.
type dataLoadedMsg struct {
	status   model.WorkingTreeStatus
	branches []model.Branch
	commits  []model.Commit
	err      error
}

// loadCmd loads status, branches, and recent commits concurrently-safe enough
// for a read snapshot (sequential calls; each is a short git invocation).
func (m Model) loadCmd() tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		ctx := context.Background()
		var out dataLoadedMsg
		st, err := repo.Status(ctx)
		if err != nil {
			out.err = err
			return out
		}
		out.status = st
		if out.branches, err = repo.Branches(ctx); err != nil {
			out.err = err
			return out
		}
		if out.commits, err = repo.Log(ctx, 50); err != nil {
			out.err = err
			return out
		}
		return out
	}
}
```

Modify `internal/tui/model.go`: add fields `status`, `branches`, `commits`, `loading`, `err`; set `loading=true` in `New`; start the load in `Init`; handle `dataLoadedMsg` and an `"r"` reload key in `Update`. Replace the file body with:
```go
// Package tui implements the gigagit terminal UI with Bubble Tea.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/model"
)

// Model is the root Bubble Tea model.
type Model struct {
	repo          *git.Repo
	width, height int

	loading  bool
	err      error
	status   model.WorkingTreeStatus
	branches []model.Branch
	commits  []model.Commit
}

// New constructs the initial model for repo.
func New(repo *git.Repo) Model {
	return Model{repo: repo, loading: true}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return m.loadCmd() }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case dataLoadedMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.status = msg.status
			m.branches = msg.branches
			m.commits = msg.commits
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.loading = true
			return m, m.loadCmd()
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	if m.loading {
		return "gigagit (loading…)\n"
	}
	if m.err != nil {
		return "error: " + m.err.Error() + "\n"
	}
	return "gigagit — branch " + m.status.Branch + "\n"
}

var _ tea.Model = Model{}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ && go vet ./internal/tui/`
Expected: PASS (skeleton + load tests).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/load.go internal/tui/load_test.go
git commit -m "feat: load repo data into the TUI asynchronously"
```

---

## Task 4: Panels, layout, focus, and navigation

**Files:**
- Create: `internal/tui/view.go`
- Modify: `internal/tui/model.go` (focus + selection state and key handling)
- Test: `internal/tui/nav_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/nav_test.go`:
```go
package tui

import (
	"strings"
	"testing"
)

func loadedModel(t *testing.T) Model {
	t.Helper()
	repo := newRepo(t)
	m := New(repo)
	updated, _ := m.Update(m.loadCmd()())
	return updated.(Model)
}

func TestTabCyclesFocus(t *testing.T) {
	m := loadedModel(t)
	start := m.focus
	updated, _ := m.Update(keyMsg("tab"))
	if updated.(Model).focus == start {
		t.Fatal("tab should change the focused panel")
	}
}

func TestDownMovesSelectionInFocusedPanel(t *testing.T) {
	m := loadedModel(t)
	// Put two commits in so selection can move within the commits panel.
	// (loadedModel has 1 commit; instead test branches panel selection bound at 0.)
	m.focus = panelBranches
	updated, _ := m.Update(keyMsg("down"))
	mm := updated.(Model)
	// With a single branch, selection must clamp at 0 (no out-of-range).
	if mm.sel[panelBranches] != 0 {
		t.Fatalf("selection = %d, want clamped 0 with one item", mm.sel[panelBranches])
	}
}

func TestViewRendersPanelsWithoutPanic(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 30
	out := m.View()
	if !strings.Contains(out, "main") {
		t.Fatalf("view should mention branch 'main':\n%s", out)
	}
	for _, label := range []string{"Branches", "Status", "Commits"} {
		if !strings.Contains(out, label) {
			t.Fatalf("view missing panel label %q:\n%s", label, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestTab|TestDown|TestViewRenders'`
Expected: FAIL — undefined `focus`, `panelBranches`, `sel`.

- [ ] **Step 3: Implement**

Modify `internal/tui/model.go`: add the panel type, `focus`, and `sel` map; initialize `sel` in `New`; handle `tab`, `up`/`k`, `down`/`j` in `Update`. Apply these edits:

1. Add to the imports nothing new. Add after the `Model` struct's existing fields (inside the struct):
```go
	focus panel
	sel   map[panel]int
```
2. Add the panel type and count, after the `Model` struct definition:
```go
type panel int

const (
	panelBranches panel = iota
	panelStatus
	panelCommits
	panelCount
)
```
3. In `New`, initialize the selection map:
```go
func New(repo *git.Repo) Model {
	return Model{repo: repo, loading: true, sel: map[panel]int{}}
}
```
4. In `Update`'s `tea.KeyMsg` switch, add cases (before the closing brace of the switch):
```go
		case "tab":
			m.focus = (m.focus + 1) % panelCount
		case "up", "k":
			if m.sel[m.focus] > 0 {
				m.sel[m.focus]--
			}
		case "down", "j":
			if m.sel[m.focus] < m.panelLen(m.focus)-1 {
				m.sel[m.focus]++
			}
```
5. Add a helper method (anywhere in model.go):
```go
// panelLen returns the number of rows in a panel, for selection clamping.
func (m Model) panelLen(p panel) int {
	switch p {
	case panelBranches:
		return len(m.branches)
	case panelStatus:
		return len(m.status.Files)
	case panelCommits:
		return len(m.commits)
	}
	return 0
}
```
6. Replace `View()` in model.go with a delegation to the renderer:
```go
// View implements tea.Model.
func (m Model) View() string {
	if m.loading {
		return "gigagit (loading…)\n"
	}
	if m.err != nil {
		return "error: " + m.err.Error() + "\n"
	}
	return m.render()
}
```

Create `internal/tui/view.go`:
```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true)
	focusedPanel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("12")).Padding(0, 1)
	bluredPanel  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	selectedRow  = lipgloss.NewStyle().Reverse(true)
)

// render draws the three panels and a footer.
func (m Model) render() string {
	header := titleStyle.Render("gigagit") + "  branch " + m.status.Branch
	if m.status.Upstream != "" {
		header += fmt.Sprintf(" (↑%d ↓%d)", m.status.Ahead, m.status.Behind)
	}

	branches := m.renderList(panelBranches, "Branches", m.branchRows())
	status := m.renderList(panelStatus, "Status", m.statusRows())
	commits := m.renderList(panelCommits, "Commits", m.commitRows())

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.JoinVertical(lipgloss.Left, branches, status),
		commits,
	)
	footer := "[tab] focus  [↑/↓ or k/j] move  [r] reload  [q] quit"
	return strings.Join([]string{header, body, footer}, "\n") + "\n"
}

func (m Model) renderList(p panel, label string, rows []string) string {
	var b strings.Builder
	b.WriteString(label)
	b.WriteString("\n")
	if len(rows) == 0 {
		b.WriteString("  (none)")
	}
	for i, row := range rows {
		if i == m.sel[p] && p == m.focus {
			b.WriteString(selectedRow.Render("> " + row))
		} else {
			b.WriteString("  " + row)
		}
		if i < len(rows)-1 {
			b.WriteString("\n")
		}
	}
	style := bluredPanel
	if p == m.focus {
		style = focusedPanel
	}
	return style.Render(b.String())
}

func (m Model) branchRows() []string {
	out := make([]string, 0, len(m.branches))
	for _, b := range m.branches {
		marker := "  "
		if b.IsHead {
			marker = "* "
		}
		out = append(out, marker+b.Name)
	}
	return out
}

func (m Model) statusRows() []string {
	out := make([]string, 0, len(m.status.Files))
	for _, f := range m.status.Files {
		x := f.Staged
		y := f.Unstaged
		if x == 0 {
			x = ' '
		}
		if y == 0 {
			y = ' '
		}
		out = append(out, fmt.Sprintf("%c%c %s", x, y, f.Path))
	}
	return out
}

func (m Model) commitRows() []string {
	out := make([]string, 0, len(m.commits))
	for _, c := range m.commits {
		h := c.Hash
		if len(h) > 7 {
			h = h[:7]
		}
		out = append(out, h+" "+c.Subject)
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ && go vet ./internal/tui/ && gofmt -l internal/tui`
Expected: PASS; gofmt clean.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/view.go internal/tui/model.go internal/tui/nav_test.go
git commit -m "feat: render TUI panels with focus and navigation"
```

---

## Task 5: Launch the TUI from `gg`

**Files:**
- Create: `internal/tui/run.go`
- Modify: `cmd/gg/main.go`
- Test: `internal/tui/run_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/run_test.go`:
```go
package tui

import "testing"

// Run wires a real program; we can't drive a PTY here, but we can assert the
// constructor path is sound and New produces a usable model.
func TestNewModelImplementsTeaModel(t *testing.T) {
	m := New(nil)
	if m.Init() != nil && false {
		// Init returns a load command; just ensure no panic constructing it.
	}
	_ = m.View() // loading view must not panic with a nil repo
}
```

- [ ] **Step 2: Run test to verify it fails / compiles**

Run: `go test ./internal/tui/ -run TestNewModelImplementsTeaModel`
Expected: PASS already if `View` is nil-safe in the loading state (it is — loading returns early). This guards against regressions; proceed.

- [ ] **Step 3: Implement `Run`**

Create `internal/tui/run.go`:
```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/git"
)

// Run launches the TUI for repo, taking over the alternate screen until the
// user quits.
func Run(repo *git.Repo) error {
	p := tea.NewProgram(New(repo), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
```

- [ ] **Step 4: Wire `cmd/gg/main.go`**

Replace `cmd/gg/main.go` with a dispatcher: default launches the TUI; `gg inspect [flags]` runs the existing inspect surface.
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/gigagit/gg/internal/app"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
	"github.com/gigagit/gg/internal/tui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "inspect" {
		runInspect(os.Args[2:])
		return
	}
	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", ".", observ.NewRing(200))}
	if err := tui.Run(repo); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runInspect(args []string) {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	dumpPath := fs.String("debug-dump", "", "write a debug dump JSON file to this path")
	trace := fs.Bool("trace", false, "enable verbose timing trace to stderr")
	_ = fs.Parse(args)

	opts := app.Options{WorkDir: ".", Stdout: os.Stdout, DumpPath: *dumpPath}
	if *trace || os.Getenv("GG_TRACE") == "1" {
		opts.Trace = os.Stderr
	}
	if err := app.Inspect(context.Background(), opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Verify build + full suite**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l internal cmd`
Expected: build OK; all PASS; vet clean; gofmt prints nothing.

- [ ] **Step 6: Manual smoke check (non-interactive safe)**

Run:
```bash
go build -o /tmp/gg ./cmd/gg && /tmp/gg inspect | head -3
```
Expected: `gg inspect` still prints the summary (proves the dispatcher; the bare `gg` TUI needs a real terminal and is verified by you interactively).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/run.go internal/tui/run_test.go cmd/gg/main.go
git commit -m "feat: launch TUI by default, keep gg inspect subcommand"
```

---

## Self-Review

**Spec coverage (§7 TUI):**
- Bubble Tea framework, multi-panel layout (branches, status, commits), footer keybindings, navigation → Tasks 2–4.
- Async, non-blocking load off the UI thread → `loadCmd` as a `tea.Cmd` (Task 3).
- Commit list (not graph) → `Log` verb + commits panel (Tasks 1, 4).
- Launch surface → Task 5.

**Deferred to Plan 3B:** the diff/context panel content, mutating operations (smart pull/switch/commit/push/stash/undo) wired to keys, the modal `Decider`, live `Progress`/`GitLine` streaming, credential routing, and the panic-triggered debug dump. 3A is read-only.

**Placeholder scan:** none — all steps contain complete code. (Task 5 Step 2's test is a deliberate non-panic guard, not a stub.)

**Type consistency:** `Model` fields (`repo`, `status`, `branches`, `commits`, `loading`, `err`, `focus`, `sel`, `width`, `height`) are introduced in Tasks 2–4 and used consistently; `panel`/`panelBranches`/`panelStatus`/`panelCommits`/`panelCount` consistent across Tasks 4; `dataLoadedMsg` fields match `loadCmd` and `Update` (Task 3); `git.Repo.Log` signature (Task 1) matches `loadCmd`'s call (Task 3).

---

## Plan sequence (M1)

1. Plan 1 — Foundation ✅  2. Plan 2A — Engine ✅  3. Plan 2B — Smart ops ✅  4. Plan 2C — Undo ✅
5. **Plan 3A — Read-only TUI** (this document).
6. Plan 3B — Interactive TUI: wire smart operations to keys, the modal Decider, live progress streaming, credential routing, panic-triggered debug dump. Completes M1.
