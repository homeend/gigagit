# Mouse Focus + Wheel + UI Config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Left-click focuses a window and selects the clicked row; the mouse wheel scrolls the list under the cursor by a configurable `[ui] wheel_step`; a new project skill documents how config entries work (spec: `docs/superpowers/specs/2026-06-13-mouse-focus-design.md`).

**Architecture:** Pure hit-testing against the renderer's own `layout()` geometry (`panelAt`/`panelRowAt`), with `windowRows`' scroll offset extracted into a shared `windowStart` so renderer and hit-test can't drift. All mouse routing lives in a new `internal/tui/mouse.go` (`handleMouse`), mirroring the key-routing precedence: help window → other overlays swallow → files view's two sides → normal panels. A new `UIConfig` section rides the existing defaults→global→repo field-level overlay.

**Tech Stack:** Go 1.26, Bubble Tea + lipgloss, go-toml v2.

**Branch:** `feat/mouse-focus` off `main`.

**Conventions for every task:** TDD (failing test → watch it fail → implement → pass); `gofmt -w` on touched files; commit messages end with
`Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

## File map

| File | Change |
|---|---|
| `internal/config/config.go` | `UIConfig` section, `Defaults`, `overlayUI`, wired in `Load` |
| `internal/config/config_test.go` | wheel_step layer tests |
| `internal/tui/view.go` | extract `windowStart` from `windowRows` |
| `internal/tui/viewstate.go` | `panelAt`, `panelRowAt` hit-test helpers |
| `internal/tui/model.go` | `wheelStep()` helper; `case tea.MouseMsg:` delegates to `handleMouse` |
| `internal/tui/content_popup.go` | delete `contentWheelStep` const (keep `contentFastStep`) |
| `internal/tui/mouse.go` | **new** — `handleMouse`, `mouseInFilesView` |
| `internal/tui/mouse_test.go` | **new** — all mouse + hit-test tests |
| `internal/tui/help.go` | Global `click`/`wheel` rows |
| `.claude/skills/adding-config-entries/SKILL.md` | **new** project skill |
| `README.md`, `CHANGELOG.md` | docs |

---

### Task 1: `[ui] wheel_step` config entry

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Context:** `internal/config/config.go` holds `Config{Worktree WorktreeConfig}`, `Defaults()`, `Load(globalPath, repoPath)` (defaults overlaid by global then repo via per-section `overlayWorktree` — only SET fields copy; zero = unset), `decodeFile`. Tests use `t.TempDir()` + a `writeFile` helper.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
func TestUIWheelStepLayers(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.toml")

	// Default.
	cfg, err := Load(missing, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.WheelStep != 3 {
		t.Errorf("default wheel_step = %d, want 3", cfg.UI.WheelStep)
	}

	// Global only.
	g := filepath.Join(dir, "global.toml")
	writeFile(t, g, "[ui]\nwheel_step = 5\n")
	cfg, err = Load(g, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.WheelStep != 5 {
		t.Errorf("global wheel_step = %d, want 5", cfg.UI.WheelStep)
	}

	// Repo wins over global.
	r := filepath.Join(dir, "repo.toml")
	writeFile(t, r, "[ui]\nwheel_step = 7\n")
	cfg, err = Load(g, r)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.WheelStep != 7 {
		t.Errorf("repo wheel_step = %d, want 7", cfg.UI.WheelStep)
	}

	// Zero and negative are unset: the repo layer cannot reset the global's.
	writeFile(t, r, "[ui]\nwheel_step = -2\n")
	cfg, err = Load(g, r)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.WheelStep != 5 {
		t.Errorf("negative wheel_step must be ignored, got %d, want global 5", cfg.UI.WheelStep)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/config -run TestUIWheelStepLayers -v`
Expected: COMPILE FAILURE — `cfg.UI` undefined.

- [ ] **Step 3: Implement**

In `internal/config/config.go`:

**(a)** After `WorktreeConfig`:

```go
// UIConfig configures TUI behavior. TOML keys are snake_case.
type UIConfig struct {
	WheelStep int `toml:"wheel_step"` // rows per mouse-wheel tick; <=0 = unset
}
```

**(b)** Extend `Config`:

```go
// Config is the merged gigagit configuration.
type Config struct {
	Worktree WorktreeConfig `toml:"worktree"`
	UI       UIConfig       `toml:"ui"`
}
```

**(c)** Extend `Defaults()`:

```go
func Defaults() Config {
	return Config{
		Worktree: WorktreeConfig{
			PathTemplate:          "../<repo>.worktrees/<branch>",
			DefaultBranchTemplate: "b/from-<parent-branch>-<random-alpha:4>",
		},
		UI: UIConfig{WheelStep: 3},
	}
}
```

**(d)** In `Load`, inside the `if ok {` block, after `overlayWorktree(...)`:

```go
			overlayUI(&cfg.UI, layer.UI)
```

**(e)** After `overlayWorktree`:

```go
// overlayUI copies each set field of src onto dst. WheelStep <= 0 is unset
// (same rule as the string fields: a higher layer cannot reset a lower
// layer's value to the zero value).
func overlayUI(dst *UIConfig, src UIConfig) {
	if src.WheelStep > 0 {
		dst.WheelStep = src.WheelStep
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config -v`
Expected: PASS (all, including the new one)

- [ ] **Step 5: Vet + commit**

```bash
go vet ./... && gofmt -l internal cmd
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): [ui] wheel_step entry with field-level overlay"
```

---

### Task 2: Hit-test helpers — `windowStart`, `panelAt`, `panelRowAt`, `wheelStep`

**Files:**
- Modify: `internal/tui/view.go` (`windowRows`, ~line 290)
- Modify: `internal/tui/viewstate.go` (append helpers)
- Modify: `internal/tui/model.go` (append `wheelStep`)
- Create: `internal/tui/mouse_test.go` (hit-test units; mouse routing tests come in Task 3)

**Context:** `layout()` (viewstate.go) returns `layoutGeom{w, h, bodyH, leftW, rightW, boxH map[panel]int, pos map[panel]point}` — the single source of truth for panel geometry (header is screen row 0; panels start at y=1; left panels are `leftW` wide, Commits `rightW`). `renderPanel` draws: top border at the box's `pos.y`, label line, then data rows windowed by `windowRows(rows, rowsCap, m.sel[p])` where `rowsCap = m.panelRowsCap(p)` — so data row i sits at screen `pos.y + 2 + i`. `windowRows` currently computes its start offset inline. `panelView(p)` returns the display rows; `m.cfg` is `config.Config` (zero before the first load). At 80×24: `leftW=26`, `bodyH=21`, Branches box h=7 at {0,1}, Worktrees {0,8}, Status {0,15}, Commits {26,1} h=21.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/mouse_test.go`:

```go
package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/model"
)

// mouseModel is markModel sized 80x24: leftW=26, three left boxes of height 7
// at y=1/8/15, Commits 26..79 full body height.
func mouseModel() Model {
	m := markModel()
	m.width, m.height = 80, 24
	return m
}

func TestPanelAt(t *testing.T) {
	m := mouseModel()
	cases := []struct {
		x, y int
		want panel
		ok   bool
	}{
		{0, 1, panelBranches, true},   // top-left border cell
		{5, 4, panelBranches, true},   // data area
		{0, 8, panelWorktrees, true},  // second box top
		{0, 15, panelStatus, true},    // third box top
		{25, 21, panelStatus, true},   // bottom-right of the left column
		{26, 1, panelCommits, true},   // commits left edge
		{79, 21, panelCommits, true},  // commits bottom-right
		{5, 0, 0, false},              // header row
		{5, 22, 0, false},             // footer row
		{5, 23, 0, false},             // status row
	}
	for _, c := range cases {
		p, ok := m.panelAt(c.x, c.y)
		if ok != c.ok || (ok && p != c.want) {
			t.Errorf("panelAt(%d,%d) = %v,%v want %v,%v", c.x, c.y, p, ok, c.want, c.ok)
		}
	}
}

func TestPanelAtNarrowTerminal(t *testing.T) {
	m := mouseModel()
	m.width = 30 // single commits column
	if p, ok := m.panelAt(5, 5); !ok || p != panelCommits {
		t.Fatalf("panelAt = %v,%v, want commits on a narrow terminal", p, ok)
	}
}

func TestPanelRowAt(t *testing.T) {
	m := mouseModel() // branches box: border y=1, label y=2, data y=3..6
	if idx, ok := m.panelRowAt(panelBranches, 3); !ok || idx != 0 {
		t.Fatalf("row at y=3 = %d,%v, want 0,true", idx, ok)
	}
	if idx, ok := m.panelRowAt(panelBranches, 4); !ok || idx != 1 {
		t.Fatalf("row at y=4 = %d,%v, want 1,true", idx, ok)
	}
	if _, ok := m.panelRowAt(panelBranches, 2); ok {
		t.Fatal("the label line must not map to a row")
	}
	if _, ok := m.panelRowAt(panelBranches, 6); ok {
		t.Fatal("padding below the last row (3 branches) must not map") // rows y=3,4,5 hold the 3 branches
	}
}

func TestPanelRowAtScrolledPanel(t *testing.T) {
	m := mouseModel()
	m.branches = nil
	for i := 0; i < 30; i++ {
		m.branches = append(m.branches, model.Branch{Name: string(rune('a'+i%26)) + "-br"})
	}
	m.sel[panelBranches] = 20 // branches rowsCap = 7-3 = 4; windowStart(30,4,20)=18
	if idx, ok := m.panelRowAt(panelBranches, 3); !ok || idx != 18 {
		t.Fatalf("scrolled row at y=3 = %d,%v, want 18,true (windowStart consistency)", idx, ok)
	}
}

func TestWindowStartMatchesWindowRows(t *testing.T) {
	for _, c := range []struct{ total, n, sel int }{
		{2, 5, 0}, {10, 4, 0}, {10, 4, 9}, {30, 4, 20}, {30, 4, 2},
	} {
		rows := make([]string, c.total)
		_, _, start := windowRows(rows, c.n, c.sel)
		if got := windowStart(c.total, c.n, c.sel); got != start {
			t.Errorf("windowStart(%d,%d,%d) = %d, windowRows start = %d", c.total, c.n, c.sel, got, start)
		}
	}
}

func TestWheelStepHelper(t *testing.T) {
	m := mouseModel()
	if m.wheelStep() != 3 {
		t.Fatalf("wheelStep = %d before any config load, want 3", m.wheelStep())
	}
	m.cfg = config.Config{UI: config.UIConfig{WheelStep: 5}}
	if m.wheelStep() != 5 {
		t.Fatalf("wheelStep = %d, want configured 5", m.wheelStep())
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run 'TestPanelAt|TestPanelRowAt|TestWindowStart|TestWheelStep' -v`
Expected: COMPILE FAILURE — `m.panelAt` undefined.

- [ ] **Step 3: Extract `windowStart`**

In `internal/tui/view.go`, replace `windowRows` with:

```go
// windowRows returns at most n rows scrolled so sel stays visible, sel's
// index within the returned window, and the window's start offset.
func windowRows(rows []string, n, sel int) ([]string, int, int) {
	if n <= 0 {
		n = 1
	}
	if len(rows) <= n {
		return rows, sel, 0
	}
	start := windowStart(len(rows), n, sel)
	return rows[start : start+n], sel - start, start
}

// windowStart is the scroll offset windowRows applies: the first display row
// shown when total rows are windowed to n around sel. Shared with the mouse
// hit-test (panelRowAt) so a click can never select a different row than the
// one rendered on that screen line.
func windowStart(total, n, sel int) int {
	if n <= 0 {
		n = 1
	}
	if total <= n {
		return 0
	}
	start := sel - n/2
	if start < 0 {
		start = 0
	}
	if start+n > total {
		start = total - n
	}
	if start < 0 {
		start = 0
	}
	return start
}
```

- [ ] **Step 4: Add the hit-test helpers**

Append to `internal/tui/viewstate.go`:

```go
// panelAt returns the panel whose box contains screen cell (x, y) under the
// current layout (border cells count as the panel). ok is false on the
// header/footer/status rows and any gap; panels the layout hides never match.
func (m Model) panelAt(x, y int) (panel, bool) {
	g := m.layout()
	for p := panel(0); p < panelCount; p++ {
		h := g.boxH[p]
		if h <= 0 {
			continue
		}
		w := g.leftW
		if p == panelCommits {
			w = g.rightW
		}
		pos := g.pos[p]
		if x >= pos.x && x < pos.x+w && y >= pos.y && y < pos.y+h {
			return p, true
		}
	}
	return 0, false
}

// panelRowAt maps screen row y inside panel p to an index into p's display
// rows (panelView order). ok is false on the border, the label line, and
// the padding below the last row. Uses the same windowStart the renderer
// uses, so the mapping cannot drift from what is on screen.
func (m Model) panelRowAt(p panel, y int) (int, bool) {
	g := m.layout()
	rowsCap := m.panelRowsCap(p)
	i := y - g.pos[p].y - 2 // top border + label line
	if i < 0 || i >= rowsCap {
		return 0, false
	}
	rows, _ := m.panelView(p)
	idx := windowStart(len(rows), rowsCap, m.sel[p]) + i
	if idx >= len(rows) {
		return 0, false
	}
	return idx, true
}
```

- [ ] **Step 5: Add `wheelStep`**

Append to `internal/tui/model.go` (near `rememberLeftFocus`):

```go
// wheelStep is the configured rows-per-mouse-wheel-tick ([ui] wheel_step),
// defaulting to 3 before the first config load (m.cfg is zero until
// dataLoadedMsg arrives).
func (m Model) wheelStep() int {
	if s := m.cfg.UI.WheelStep; s > 0 {
		return s
	}
	return 3
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui -run 'TestPanelAt|TestPanelRowAt|TestWindowStart|TestWheelStep' -v`
Expected: PASS (6 tests)

- [ ] **Step 7: Whole package + vet + commit**

```bash
go test ./internal/tui && go vet ./... && gofmt -l internal cmd
git add internal/tui/view.go internal/tui/viewstate.go internal/tui/model.go internal/tui/mouse_test.go
git commit -m "feat(tui): mouse hit-test helpers — panelAt/panelRowAt over shared windowStart"
```

---

### Task 3: `handleMouse` — normal-mode click + wheel, overlay gating

**Files:**
- Create: `internal/tui/mouse.go`
- Modify: `internal/tui/model.go` (the `case tea.MouseMsg:` block, ~line 376)
- Modify: `internal/tui/content_popup.go` (delete `contentWheelStep`, ~line 72)
- Test: `internal/tui/mouse_test.go` (append)

**Context:** The current `case tea.MouseMsg:` body handles ONLY wheel: contentPopup first, else filesView, both stepping by the `contentWheelStep` const (= 3) — that whole body is replaced by a `handleMouse` delegate. `rememberLeftFocus()` records the focused left panel before focus reassignment (the ←/tab rule). `panelLen(p)` is the display row count. The files-view branch of `handleMouse` is stubbed in this task (wheel parity with today) and completed in Task 4. `contentFastStep` (5) stays. Test helpers: `mouseModel()` from Task 2, `markModel`'s data = 3 branches / 2 commits.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/mouse_test.go` (add `tea "github.com/charmbracelet/bubbletea"` to imports):

```go
func mouseMsg(x, y int, b tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: b}
}

func TestClickFocusesAndSelects(t *testing.T) {
	m := mouseModel() // focus starts on Branches
	u, _ := m.Update(mouseMsg(30, 4, tea.MouseButtonLeft)) // commits, 2nd data row
	m = u.(Model)
	if m.focus != panelCommits {
		t.Fatalf("focus = %v, want commits", m.focus)
	}
	if m.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want the clicked row 1", m.sel[panelCommits])
	}
	if m.lastLeftPanel != panelBranches {
		t.Fatalf("lastLeftPanel = %v, want branches (recorded on leaving)", m.lastLeftPanel)
	}
}

func TestClickOnLabelFocusesWithoutSelecting(t *testing.T) {
	m := mouseModel()
	m.focus = panelCommits
	m.sel[panelBranches] = 2
	u, _ := m.Update(mouseMsg(5, 2, tea.MouseButtonLeft)) // branches label line
	m = u.(Model)
	if m.focus != panelBranches {
		t.Fatalf("focus = %v, want branches", m.focus)
	}
	if m.sel[panelBranches] != 2 {
		t.Fatalf("sel = %d, a label click must not move the selection", m.sel[panelBranches])
	}
}

func TestClickOutsidePanelsNoOps(t *testing.T) {
	m := mouseModel()
	u, _ := m.Update(mouseMsg(5, 0, tea.MouseButtonLeft)) // header
	if got := u.(Model).focus; got != panelBranches {
		t.Fatalf("focus = %v, header click must no-op", got)
	}
}

func TestClickIgnoredUnderOverlays(t *testing.T) {
	overlays := []func(m *Model){
		func(m *Model) { m.modal = &decisionState{} },
		func(m *Model) { m.popup = &worktreePopup{} },
		func(m *Model) { m.repoPopup = &repoPopup{} },
		func(m *Model) { m.settings = &settingsPopup{} },
		func(m *Model) { m.branchPopup = &branchPopup{} },
		func(m *Model) { m.pairPopup = &pairOpPopup{} },
	}
	for i, set := range overlays {
		m := mouseModel()
		set(&m)
		u, _ := m.Update(mouseMsg(30, 4, tea.MouseButtonLeft))
		mm := u.(Model)
		if mm.focus != panelBranches || mm.sel[panelCommits] != 0 {
			t.Fatalf("overlay %d: click must be ignored (focus=%v sel=%d)", i, mm.focus, mm.sel[panelCommits])
		}
	}
}

func TestWheelScrollsHoveredPanelWithoutFocus(t *testing.T) {
	m := mouseModel() // focus Branches; 2 commits
	u, _ := m.Update(mouseMsg(30, 5, tea.MouseButtonWheelDown))
	m = u.(Model)
	if m.focus != panelBranches {
		t.Fatal("wheel must not move focus")
	}
	if m.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, wheel over commits must move ITS selection (step 3 clamped to 1)", m.sel[panelCommits])
	}
	u, _ = m.Update(mouseMsg(30, 5, tea.MouseButtonWheelUp))
	if got := u.(Model).sel[panelCommits]; got != 0 {
		t.Fatalf("sel = %d after wheel up, want 0", got)
	}
}

func TestWheelStepRespectsConfig(t *testing.T) {
	m := mouseModel() // 3 branches
	m.cfg = config.Config{UI: config.UIConfig{WheelStep: 1}}
	u, _ := m.Update(mouseMsg(5, 4, tea.MouseButtonWheelDown))
	if got := u.(Model).sel[panelBranches]; got != 1 {
		t.Fatalf("sel = %d, configured step 1 must move by exactly 1", got)
	}
}

func TestWheelOutsidePanelsNoOps(t *testing.T) {
	m := mouseModel()
	u, _ := m.Update(mouseMsg(5, 23, tea.MouseButtonWheelDown)) // status line
	mm := u.(Model)
	if mm.sel[panelBranches] != 0 || mm.sel[panelCommits] != 0 {
		t.Fatal("wheel outside any panel must no-op")
	}
}

func TestHelpWindowKeepsWheelPriority(t *testing.T) {
	m := mouseModel()
	m.contentPopup = newContentPopup("Help — keys", helpContent())
	u, _ := m.Update(mouseMsg(30, 5, tea.MouseButtonWheelDown))
	mm := u.(Model)
	if mm.contentPopup.sel != 3 {
		t.Fatalf("help sel = %d, want 3 (wheel scrolls the help window)", mm.contentPopup.sel)
	}
	if mm.sel[panelCommits] != 0 {
		t.Fatal("the panel under the help window must not scroll")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run 'TestClick|TestWheel|TestHelpWindowKeepsWheel' -v`
Expected: FAIL — clicks/wheel currently do nothing (focus/selection assertions fail).

- [ ] **Step 3: Implement `handleMouse`**

Create `internal/tui/mouse.go`:

```go
package tui

import tea "github.com/charmbracelet/bubbletea"

// handleMouse routes all mouse input. Precedence mirrors the key routing:
// the help window owns the wheel; under any other popup or the modal mouse
// input is ignored entirely (centered overlays — hit-testing the background
// would act on hidden state); then the files view's two sides; then the
// normal panels. Click-to-focus and wheel are ungated on running/loading
// (pure focus/selection movement, like the arrow keys).
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	wheel := 0
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		wheel = -m.wheelStep()
	case tea.MouseButtonWheelDown:
		wheel = m.wheelStep()
	}
	if m.contentPopup != nil {
		if wheel != 0 {
			m.contentPopup.move(wheel)
		}
		return m, nil
	}
	if m.modal != nil || m.popup != nil || m.repoPopup != nil ||
		m.settings != nil || m.branchPopup != nil || m.pairPopup != nil {
		return m, nil
	}
	if m.filesView != nil {
		return m.mouseInFilesView(msg, wheel)
	}

	p, ok := m.panelAt(msg.X, msg.Y)
	if !ok {
		return m, nil
	}
	if wheel != 0 {
		// Position-targeted: scroll the hovered panel, focus untouched.
		if n := m.panelLen(p); n > 0 {
			s := m.sel[p] + wheel
			if s > n-1 {
				s = n - 1
			}
			if s < 0 {
				s = 0
			}
			m.sel[p] = s
		}
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if p != m.focus {
		m = m.rememberLeftFocus()
		m.focus = p
	}
	if idx, ok := m.panelRowAt(p, msg.Y); ok {
		m.sel[p] = idx
	}
	return m, nil
}

// mouseInFilesView routes mouse input while the commit files view is open.
// Completed in the files-view task; wheel keeps today's behavior until then.
func (m Model) mouseInFilesView(msg tea.MouseMsg, wheel int) (tea.Model, tea.Cmd) {
	if wheel != 0 {
		m.filesView.move(wheel)
	}
	return m, nil
}
```

- [ ] **Step 4: Wire it and delete the const**

**(a)** In `internal/tui/model.go`, replace the WHOLE `case tea.MouseMsg:` body (the contentPopup/filesView wheel blocks and their comment) with:

```go
	case tea.MouseMsg:
		return m.handleMouse(msg)
```

**(b)** In `internal/tui/content_popup.go`, replace

```go
// contentFastStep is the ctrl+↑/↓ jump; contentWheelStep the mouse-wheel tick.
const (
	contentFastStep  = 5
	contentWheelStep = 3
)
```

with

```go
// contentFastStep is the ctrl+↑/↓ jump (the mouse-wheel tick is the
// configurable wheelStep).
const contentFastStep = 5
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui -run 'TestClick|TestWheel|TestHelpWindowKeepsWheel' -v`
Expected: PASS (8 tests)

- [ ] **Step 6: Whole package + vet + commit**

```bash
go test ./internal/tui && go vet ./... && gofmt -l internal cmd
git add internal/tui/mouse.go internal/tui/model.go internal/tui/content_popup.go internal/tui/mouse_test.go
git commit -m "feat(tui): click-to-focus + wheel-over-panel via handleMouse"
```

---

### Task 4: Files-view mouse — two-sided click + wheel

**Files:**
- Modify: `internal/tui/mouse.go` (`mouseInFilesView`)
- Test: `internal/tui/mouse_test.go` (append)

**Context:** While the files view is open, the left column (`g.leftW` wide, body rows y=1..bodyH) is ONE box: top border at y=1, title at y=2, windowed tree rows from y=3 (capacity `m.filesPageRows()` = bodyH−4, windowed over the FILTERED `m.filesView.visible()` list around `m.filesView.sel`), padding, hint line last. The Commits panel renders normally on the right (so `panelAt`/`panelRowAt` work for it). `m.filesTreeFocused` is the side flag; `moveCommitUnderFilesView(delta)` clamps the commit selection, dedupes by hash, and fires at most one follow-live reload cmd. Test helpers `filesModel()` / `openFilesView(t, m)` live in files_view_test.go (80×24, 2 commits, tree = 3 visible lines).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/mouse_test.go`:

```go
func TestFilesViewTreeClickFocusesAndMovesCursor(t *testing.T) {
	m := openFilesView(t, filesModel())
	u, _ := m.Update(mouseMsg(5, 4, tea.MouseButtonLeft)) // 2nd visible tree line
	m = u.(Model)
	if !m.filesTreeFocused {
		t.Fatal("a tree click must focus the tree side")
	}
	if m.filesView.sel != 1 {
		t.Fatalf("tree sel = %d, want the clicked line 1", m.filesView.sel)
	}
	u, _ = m.Update(mouseMsg(5, 2, tea.MouseButtonLeft)) // title line
	m = u.(Model)
	if m.filesView.sel != 1 {
		t.Fatal("a title click must not move the tree cursor")
	}
}

func TestFilesViewCommitsClickSelectsWithOneReload(t *testing.T) {
	m := openFilesView(t, filesModel())
	u, _ := m.Update(mouseMsg(5, 4, tea.MouseButtonLeft)) // focus the tree first
	m = u.(Model)
	u, cmd := m.Update(mouseMsg(30, 4, tea.MouseButtonLeft)) // commits, 2nd row
	m = u.(Model)
	if m.filesTreeFocused {
		t.Fatal("a commits click must focus the commit side")
	}
	if m.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want the clicked commit 1", m.sel[panelCommits])
	}
	if cmd == nil {
		t.Fatal("selecting another commit must fire ONE follow-live reload")
	}
	u, _ = m.Update(cmd())
	m = u.(Model)
	// Clicking the already-selected commit dedupes: no reload.
	_, cmd = m.Update(mouseMsg(30, 4, tea.MouseButtonLeft))
	if cmd != nil {
		t.Fatal("clicking the selected commit must not reload")
	}
}

func TestFilesViewWheelTargetsHoveredSide(t *testing.T) {
	m := openFilesView(t, filesModel())
	u, _ := m.Update(mouseMsg(5, 5, tea.MouseButtonWheelDown)) // over the tree
	m = u.(Model)
	if m.filesView.sel == 0 {
		t.Fatal("wheel over the tree must scroll the tree")
	}
	if m.sel[panelCommits] != 0 {
		t.Fatal("wheel over the tree must not move the commit selection")
	}
	treeSel := m.filesView.sel
	u, cmd := m.Update(mouseMsg(30, 5, tea.MouseButtonWheelDown)) // over commits
	m = u.(Model)
	if m.sel[panelCommits] != 1 || cmd == nil {
		t.Fatalf("wheel over commits must move the commit selection with a reload (sel=%d)", m.sel[panelCommits])
	}
	if m.filesView.sel != treeSel {
		t.Fatal("wheel over commits must not scroll the tree")
	}
}

func TestFilesViewMouseOutsideColumnsNoOps(t *testing.T) {
	m := openFilesView(t, filesModel())
	u, cmd := m.Update(mouseMsg(5, 0, tea.MouseButtonLeft)) // header
	mm := u.(Model)
	if mm.filesTreeFocused || mm.filesView.sel != 0 || cmd != nil {
		t.Fatal("header click must no-op in the files view")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run 'TestFilesViewTreeClick|TestFilesViewCommitsClick|TestFilesViewWheelTargets|TestFilesViewMouseOutside' -v`
Expected: FAIL (clicks do nothing; wheel over commits scrolls the tree).

- [ ] **Step 3: Implement**

In `internal/tui/mouse.go`, replace the stub `mouseInFilesView` with:

```go
// mouseInFilesView routes mouse input while the commit files view is open:
// the left column is the tree box (border y=1, title y=2, windowed rows
// from y=3), the right column the normally-rendered Commits panel. Wheel
// and click both target whatever is under the cursor; commit-side selection
// changes go through the follow-live path (clamped, deduped, one reload).
func (m Model) mouseInFilesView(msg tea.MouseMsg, wheel int) (tea.Model, tea.Cmd) {
	g := m.layout()
	inTree := g.leftW > 0 && msg.X < g.leftW && msg.Y >= 1 && msg.Y < 1+g.bodyH
	inCommits := false
	if p, ok := m.panelAt(msg.X, msg.Y); ok && p == panelCommits {
		inCommits = true
	}
	switch {
	case wheel != 0 && inTree:
		m.filesView.move(wheel)
	case wheel != 0 && inCommits:
		return m.moveCommitUnderFilesView(wheel)
	case msg.Button == tea.MouseButtonLeft && inTree:
		m.filesTreeFocused = true
		i := msg.Y - 3 // box top (y=1) + border + title line
		if i >= 0 && i < m.filesPageRows() {
			vis := m.filesView.visible()
			if idx := windowStart(len(vis), m.filesPageRows(), m.filesView.sel) + i; idx < len(vis) {
				m.filesView.sel = idx
			}
		}
	case msg.Button == tea.MouseButtonLeft && inCommits:
		m.filesTreeFocused = false
		if idx, ok := m.panelRowAt(panelCommits, msg.Y); ok {
			return m.moveCommitUnderFilesView(idx - m.sel[panelCommits])
		}
	}
	return m, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui -run 'TestFilesViewTreeClick|TestFilesViewCommitsClick|TestFilesViewWheelTargets|TestFilesViewMouseOutside' -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Whole package + vet + commit**

```bash
go test ./internal/tui && go vet ./... && gofmt -l internal cmd
git add internal/tui/mouse.go internal/tui/mouse_test.go
git commit -m "feat(tui): files-view mouse — click and wheel target the hovered side"
```

---

### Task 5: `adding-config-entries` skill, help rows, docs, full gate

**Files:**
- Create: `.claude/skills/adding-config-entries/SKILL.md`
- Modify: `internal/tui/help.go`
- Modify: `README.md`, `CHANGELOG.md`

**Context:** `helpContent()` (help.go) is the `?` table; the footer registry is NOT touched (mouse isn't a key binding; the `TestHelpFooterCoverage` drift guard only checks registry keys). README has a key table (~line 38) and a `## Configuration` section (~line 111). Project skills live in `.claude/skills/<name>/SKILL.md` with name/description frontmatter (see the existing `adding-tui-windows` for the format).

- [ ] **Step 1: Write the skill**

Create `.claude/skills/adding-config-entries/SKILL.md`:

```markdown
---
name: adding-config-entries
description: How gigagit's TOML config works (defaults → global → repo field-level overlay) and the checklist for adding a new config entry end to end.
---

# Adding a config entry

gg's configuration is three layers, each overlaying the previous **per
field** (not per file):

1. **Built-in defaults** — `Defaults()` in `internal/config/config.go`.
2. **Global file** — `$XDG_CONFIG_HOME/gg/config.toml`, falling back to
   `~/.config/gg/config.toml` (`DefaultGlobalPath()`).
3. **Repo file** — `<repo-top>/.gg.toml`, committed to the repository.
   Repo wins.

A missing file is skipped; a present-but-malformed file is an error.

## Overlay semantics (the part people get wrong)

Each config section has an `overlay<Section>(dst, src)` func that copies
ONLY set fields: non-empty string, non-empty slice, positive int. The zero
value means "unset", so **a higher layer can never reset a field to the
zero value** — setting `wheel_step = 0` in `.gg.toml` does not disable the
global's value, it is simply ignored. This is intentional and documented on
`overlayWorktree`.

Local mutable state is NOT config: `<seq>` counters live in
`<git-common-dir>/gg/state.toml` via `internal/config/state.go` (the
committed config is read-only at runtime).

## Checklist for a new entry

1. **Field**: add it to the right section struct in
   `internal/config/config.go` with a snake_case `toml:"…"` tag — or create
   a new section struct, add it to `Config`, and write its
   `overlay<Section>` func.
2. **Default**: set it in `Defaults()`.
3. **Overlay**: extend the section's overlay func (set-detection per the
   field type: `!= ""` / `len > 0` / `> 0`).
4. **Wire**: a NEW section's overlay must be called in `Load` for both
   layers (next to `overlayWorktree`).
5. **Test**: table-test default / global-only / repo-over-global /
   zero-ignored in `internal/config/config_test.go` (see
   `TestUIWheelStepLayers`).
6. **Consume**:
   - TUI: config arrives via `loadCmd` → `dataLoadedMsg` → `m.cfg`, which
     is the ZERO value before the first load — guard with a fallback
     helper (see `Model.wheelStep()` in `internal/tui/model.go`).
   - CLI: load on demand with
     `config.Load(config.DefaultGlobalPath(), filepath.Join(top, ".gg.toml"))`
     (see `internal/cli/worktree.go`).
7. **Document**: README's `## Configuration` section.
8. **e2e**: the harness pins its own `.gg.toml` (worktree templates); touch
   it only if scenarios need the new entry.

## Worked example: `[ui] wheel_step`

`UIConfig{WheelStep int}` (default 3, `> 0` = set) governs every
mouse-wheel tick in the TUI; `Model.wheelStep()` falls back to 3 pre-load.
Set per repo in `.gg.toml`:

```toml
[ui]
wheel_step = 5
```
```

- [ ] **Step 2: Help rows**

In `internal/tui/help.go`, in the Global section directly after the `r("←/→", …)` row, add:

```go
		r("click", "focus the window under the cursor and select the clicked row"),
		r("wheel", "scroll the list under the cursor ([ui] wheel_step rows; files view: tree or commits)"),
```

Run: `go test ./internal/tui -run TestHelp -v` — expect PASS.

- [ ] **Step 3: README.md**

**(a)** In the key table, directly after the `←`/`→` row, add:

```markdown
| mouse | click focuses the window under the cursor and selects the clicked row; the wheel scrolls the hovered list (`[ui] wheel_step` rows per tick) |
```

**(b)** In `## Configuration`, append after the existing template paragraph:

```markdown
`[ui] wheel_step` sets the mouse-wheel scroll step in rows (default 3);
like every entry, the repo's `.gg.toml` overrides the global config
per field.
```

- [ ] **Step 4: CHANGELOG.md**

Under `## [Unreleased]` / `### Added`, insert as the FIRST subsection (above `#### Arrow-key window focus`):

```markdown
#### Mouse focus & wheel
- TUI: left-click focuses the window under the cursor and selects the
  clicked row (files view included: a tree click moves the tree cursor, a
  commits click moves the commit selection through the follow-live reload).
  The mouse wheel scrolls the list under the cursor — focus untouched —
  stepping by the new `[ui] wheel_step` config entry (default 3,
  defaults→global→repo overlay). Mouse input is ignored under popups and
  the decision modal. New project skill `adding-config-entries` documents
  the config system.
```

- [ ] **Step 5: Full gate**

Run: `./test.sh race`
Expected: all stages green.

- [ ] **Step 6: Commit**

```bash
git add .claude/skills/adding-config-entries/SKILL.md internal/tui/help.go README.md CHANGELOG.md
git commit -m "docs: adding-config-entries skill + mouse/help/README/CHANGELOG rows"
```

---

## Final review checklist (for the orchestrator)

- Spec coverage: config entry + overlay (T1); windowStart/panelAt/panelRowAt/wheelStep (T2); handleMouse precedence + normal mode + overlay gating + const removal (T3); files-view two-sided mouse (T4); skill + docs (T5).
- After all tasks: dispatch the final holistic reviewer, fix Important findings, then `superpowers:finishing-a-development-branch`.
