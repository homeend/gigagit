# `.` Action Menu + Configurable Footer/Menu Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `.` overlay menu that lists every currently-available action and runs the chosen one, and make footer/menu membership configurable via two `[ui]` id-allowlists.

**Architecture:** The footer registry (`footerBinding`) gains a stable `id` and becomes the single catalog behind the footer, the new `.` menu, and a keypress-replay executor (`synthKey` → re-dispatch through `Update`). Two `[ui]` lists (`footer_actions`, `menu_actions`), both id-based and both unset/empty = full set, filter the footer and menu. The menu is a window-primitive overlay popup mirroring the existing repo/settings popups.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver `Model`, pointer popup fields), `internal/tui/window.go` primitive, `internal/config` TOML overlay.

**Spec:** `docs/superpowers/specs/2026-06-16-tui-action-menu-design.md`.

---

### Task 1: Config — `footer_actions` / `menu_actions`

**Files:**
- Modify: `internal/config/config.go` (`UIConfig`, `overlayUI`)
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go` (mirrors the existing
`TestHScrollStepDefaultAndOverlay`, which calls `overlayUI` directly):
```go
func TestOverlayUIActionLists(t *testing.T) {
	dst := Defaults().UI
	overlayUI(&dst, UIConfig{FooterActions: []string{"pull", "commit"}, MenuActions: []string{"pull"}})
	if len(dst.FooterActions) != 2 || dst.FooterActions[0] != "pull" {
		t.Fatalf("FooterActions = %v, want [pull commit]", dst.FooterActions)
	}
	// A non-empty list overrides; an empty/nil list is unset and must not clobber.
	overlayUI(&dst, UIConfig{FooterActions: []string{"push"}})
	if len(dst.FooterActions) != 1 || dst.FooterActions[0] != "push" {
		t.Fatalf("non-empty list must override; got %v", dst.FooterActions)
	}
	overlayUI(&dst, UIConfig{}) // empty lists = unset
	if len(dst.FooterActions) != 1 || len(dst.MenuActions) != 1 {
		t.Fatalf("empty lists must not clobber; got footer=%v menu=%v", dst.FooterActions, dst.MenuActions)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/config/ -run TestOverlayUIActionLists -v`
Expected: FAIL — `UIConfig` has no `FooterActions` field (compile error).

- [ ] **Step 3: Add the fields + overlay**

In `internal/config/config.go`, `UIConfig`:
```go
type UIConfig struct {
	WheelStep     int      `toml:"wheel_step"`     // rows per mouse-wheel tick; <=0 = unset
	HScrollStep   int      `toml:"hscroll_step"`   // diff scroll-mode pan columns per ←/→; <=0 = unset
	FooterActions []string `toml:"footer_actions"` // action ids shown in the footer; empty = all (default)
	MenuActions   []string `toml:"menu_actions"`   // action ids shown in the . menu; empty = all (default)
}
```
In `overlayUI` (zero-value-is-unset rule — a non-empty slice replaces):
```go
	if len(src.FooterActions) > 0 {
		dst.FooterActions = src.FooterActions
	}
	if len(src.MenuActions) > 0 {
		dst.MenuActions = src.MenuActions
	}
```
Leave `Default()` as-is (nil slices = unset).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/config/ -run TestOverlayUIActionLists -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): footer_actions / menu_actions UI lists"
```

---

### Task 2: `footerBinding.id` + id catalog + drift guard

**Files:**
- Modify: `internal/tui/footer.go` (struct + all literals)
- Test: `internal/tui/footer_test.go`

No behavior change — adds the `id` field and populates every binding. Navigation
keys (`tab`, `ctrl+←/→`) keep `id: ""`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/footer_test.go`:
```go
func TestFooterBindingIDsUniqueAndPresent(t *testing.T) {
	seen := map[string]string{} // id -> label (first seen)
	nav := map[string]bool{"tab": true, "shift+tab": true, "ctrl+←/→": true}
	for _, b := range append(append([]footerBinding{}, contextBindings...), globalBindings...) {
		if nav[b.key] {
			if b.id != "" {
				t.Errorf("navigation key %q must have empty id, got %q", b.key, b.id)
			}
			continue
		}
		if b.id == "" {
			t.Errorf("binding %q (%s) is missing an id", b.key, b.label)
			continue
		}
		if prev, ok := seen[b.id]; ok {
			t.Errorf("duplicate id %q on %q and %q", b.id, prev, b.label)
		}
		seen[b.id] = b.label
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestFooterBindingIDsUniqueAndPresent -v`
Expected: FAIL — `footerBinding` has no `id` field (compile error).

- [ ] **Step 3: Add the `id` field and populate every binding**

In `internal/tui/footer.go`, change the struct (id first):
```go
type footerBinding struct {
	id    string // stable action id ("" for pure-navigation keys); see the . menu
	key   string
	label string
	when  func(Model) bool
}
```
Then add the id as the first element of every literal. Full updated registries:
```go
var contextBindings = []footerBinding{
	{"switch", "s", "[s]witch", func(m Model) bool { return m.focus == panelBranches && m.canSwitchBranch() }},
	{"branch", "b", "[b]ranch", func(m Model) bool { return m.focus == panelBranches && m.canOpenBranchPopup() }},
	{"worktree", "w", "[w]orktree", func(m Model) bool { return m.focus == panelBranches && m.canOpenWorktreePopup() }},
	{"delete-branch", "d", "[d]elete", func(m Model) bool { return m.focus == panelBranches && m.canDeleteBranch() }},
	{"mark", "m", "[m]ark", func(m Model) bool {
		return m.focus == panelBranches && m.canMark() && !m.markOnFocusedPanel()
	}},
	{"unmark", "m", "[m] unmark", func(m Model) bool {
		return m.focus == panelBranches && m.canMark() && m.markOnFocusedPanel() && m.cursorOnMark()
	}},
	{"pair", "m", "[m] pair", func(m Model) bool {
		return m.focus == panelBranches && m.canMark() && m.markOnFocusedPanel() && !m.cursorOnMark()
	}},
	{"switch-worktree", "enter", "[enter] switch", func(m Model) bool { return m.focus == panelWorktrees && m.canEnterWorktree() }},
	{"delete-worktree", "d", "[d]elete", func(m Model) bool { return m.focus == panelWorktrees && m.canDeleteWorktree() }},
	{"file-diff", "enter", "[enter] diff", func(m Model) bool { return m.canShowFileDiff() }},
	{"stage", "space", "[space] stage", func(m Model) bool { return m.focus == panelFiles && m.canStage() }},
	{"unstage", "space", "[space] unstage", func(m Model) bool { return m.focus == panelStaged && m.canStage() }},
	{"stash", "s", "[s] stash", func(m Model) bool {
		return m.focus == panelFiles && m.opsIdle() && len(stashCandidates(m.status)) > 0
	}},
	{"mark-file", "m", "[m] mark", func(m Model) bool { return m.isFilesPanel(m.focus) && m.panelLen(m.focus) > 0 }},
	{"commit-files", "l", "[l] files", func(m Model) bool {
		return m.focus == panelCommits && m.canShowCommitFiles() && !(m.width > 0 && m.width < 40)
	}},
}

var globalBindings = []footerBinding{
	{"resolve", "x", "[x] resolve", func(m Model) bool { return m.opsIdle() && len(m.status.Conflicts()) > 0 }},
	{"commit", "c", "[c] commit", Model.canCommit},
	{"amend", "C", "[C] amend", Model.canAmend},
	{"pull", "p", "[p]ull", Model.opsIdle},
	{"push", "P", "[P]ush", func(m Model) bool { return m.opsIdle() && m.status.Branch != "" }},
	{"stashes", "S", "[S]tashes", Model.opsIdle},
	{"undo", "u", "[u]ndo", Model.opsIdle},
	{"order", "o", "[o]rder", Model.opsIdle},
	{"view", "z", "[z] view", Model.opsIdle},
	{"filter", "/", "[/]filter", Model.opsIdle},
	{"repo", "R", "[R]epo", Model.opsIdle},
	{"settings", ",", "[,] settings", Model.opsIdle},
	{"", "tab", "[tab] focus", func(Model) bool { return true }},
	{"", "ctrl+←/→", "[ctrl+←/→] tab", Model.opsIdle},
	{"reload", "r", "[r] reload", func(m Model) bool { return !m.running }},
	{"help", "?", "[?] help", func(Model) bool { return true }},
	{"quit", "q", "[q] quit", func(Model) bool { return true }},
}
```
(The `actions` binding for `.` is added in Task 4, together with the menu and its help row, so the drift guard stays green.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run 'TestFooterBindingIDsUniqueAndPresent|Footer' -v`
Expected: PASS (existing footer tests still green — labels/predicates unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/footer.go internal/tui/footer_test.go
git commit -m "refactor(tui): give every footer binding a stable action id"
```

---

### Task 3: `availableActions` builder + `synthKey`

**Files:**
- Create: `internal/tui/action_menu.go`
- Test: `internal/tui/action_menu_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/action_menu_test.go`:
```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSynthKey(t *testing.T) {
	if got := synthKey("enter"); got.Type != tea.KeyEnter {
		t.Errorf("enter -> %v, want KeyEnter", got.Type)
	}
	if got := synthKey("space"); got.Type != tea.KeySpace {
		t.Errorf("space -> %v, want KeySpace", got.Type)
	}
	for _, k := range []string{"p", "P", "/", ",", "?", "."} {
		if got := synthKey(k); got.String() != k {
			t.Errorf("synthKey(%q).String() = %q", k, got.String())
		}
	}
}

func TestAvailableActionsExcludesNavAndSelf(t *testing.T) {
	m := footerModel() // Branches focus, opsIdle (see footer_test.go)
	ids := map[string]bool{}
	for _, r := range availableActions(m) {
		ids[r.id] = true
	}
	if !ids["pull"] || !ids["repo"] {
		t.Errorf("expected global actions present, got %v", ids)
	}
	if ids["actions"] {
		t.Error("the menu must not list itself (actions)")
	}
	for _, nav := range []string{"tab", "ctrl+←/→"} {
		if ids[nav] {
			t.Errorf("navigation key %q must not appear as an action", nav)
		}
	}
}
```
(`footerModel()` lives in `internal/tui/footer_test.go`. If it doesn't yield `opsIdle()==true`, set `m.loading = false` like the footer tests do.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run 'TestSynthKey|TestAvailableActions' -v`
Expected: FAIL — `synthKey`/`availableActions` undefined (compile error).

- [ ] **Step 3: Create `action_menu.go` with the builder + synthKey**

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// actionRow is one runnable action in the . menu: its stable id, the key that
// runs it, and the footer-style label.
type actionRow struct {
	id    string
	key   string
	label string
}

// availableActions returns the currently-available actions (context then
// global, registry order) as menu rows: every binding whose predicate is true,
// excluding pure navigation (id == "") and the menu's own entry (actions).
func availableActions(m Model) []actionRow {
	var out []actionRow
	add := func(bs []footerBinding) {
		for _, b := range bs {
			if b.id == "" || b.id == "actions" {
				continue
			}
			if b.when(m) {
				out = append(out, actionRow{id: b.id, key: b.key, label: b.label})
			}
		}
	}
	add(contextBindings)
	add(globalBindings)
	return out
}

// synthKey reproduces the keypress that runs an action's key, for replay
// through Update. enter/space are the only non-rune keys any action id carries;
// everything else (single runes incl. / , ? .) is a KeyRunes.
func synthKey(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
	}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run 'TestSynthKey|TestAvailableActions' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tui/action_menu.go internal/tui/action_menu_test.go
git commit -m "feat(tui): availableActions builder + synthKey replay helper"
```

---

### Task 4: The `.` menu popup (open, keys, execute, routing, render, mouse)

**Files:**
- Modify: `internal/tui/action_menu.go` (struct + open + key handler + render)
- Modify: `internal/tui/model.go` (field, `.` case, routing)
- Modify: `internal/tui/view.go` (render cascade)
- Modify: `internal/tui/mouse.go` (precedence)
- Modify: `internal/tui/footer.go` (`actions` binding)
- Modify: `internal/tui/help.go` (`.` row — keeps the drift guard green)
- Test: `internal/tui/action_menu_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/action_menu_test.go`:
```go
func TestDotOpensActionMenu(t *testing.T) {
	m := footerModel()
	m.loading = false
	u, _ := m.Update(keyMsg("."))
	mm := u.(Model)
	if mm.actionMenu == nil {
		t.Fatal(". must open the action menu")
	}
}

func TestActionMenuRunsPullByKey(t *testing.T) {
	m := footerModel()
	m.loading = false
	u, _ := m.Update(keyMsg(".")) // open
	m = u.(Model)
	u, cmd := m.Update(keyMsg("p")) // direct key runs pull
	mm := u.(Model)
	if mm.actionMenu != nil {
		t.Fatal("running an action must close the menu")
	}
	if !mm.running || cmd == nil {
		t.Fatal("p from the menu must start SmartPull")
	}
}

func TestActionMenuEscCloses(t *testing.T) {
	m := footerModel()
	m.loading = false
	u, _ := m.Update(keyMsg("."))
	m = u.(Model)
	u, _ = m.Update(keyMsg("esc"))
	if u.(Model).actionMenu != nil {
		t.Fatal("esc must close the menu")
	}
}

func TestDotNoOpUnderPopup(t *testing.T) {
	m := footerModel()
	m.repoPopup = &repoPopup{} // a popup owns the keyboard
	u, _ := m.Update(keyMsg("."))
	if u.(Model).actionMenu != nil {
		t.Fatal(". must not open the menu while another popup is open")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run 'ActionMenu|DotOpens|DotNoOp' -v`
Expected: FAIL — `Model.actionMenu`/`actionMenu` type undefined (compile error).

- [ ] **Step 3: Add the struct, open, key handler, and render to `action_menu.go`**

Append to `internal/tui/action_menu.go`:
```go
import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)
// (merge these into the existing import block; tea is already imported)

// actionMenu is the . overlay: a window-primitive list of runnable actions.
type actionMenu struct {
	rows    []actionRow
	sel     int
	typing  bool   // / filter input
	query   string
	mode    dispMode
	hscroll int
}

func (a *actionMenu) visible() []actionRow {
	if a.query == "" {
		return a.rows
	}
	q := strings.ToLower(a.query)
	var out []actionRow
	for _, r := range a.rows {
		if strings.Contains(strings.ToLower(r.label), q) {
			out = append(out, r)
		}
	}
	return out
}

func (a *actionMenu) move(d int) {
	n := len(a.visible())
	a.sel += d
	if a.sel > n-1 {
		a.sel = n - 1
	}
	if a.sel < 0 {
		a.sel = 0
	}
}

// openActionMenu builds the menu from the available actions (Task 5 applies the
// menu_actions allowlist here).
func (m Model) openActionMenu() Model {
	m.actionMenu = &actionMenu{rows: availableActions(m)}
	return m
}

// runVisibleRow closes the menu and replays the row's key through Update, which
// reaches the base-layout handler (the menu is now nil).
func (m Model) runVisibleRow(sel int) (tea.Model, tea.Cmd) {
	vis := m.actionMenu.visible()
	if sel < 0 || sel >= len(vis) {
		m.actionMenu = nil
		return m, nil
	}
	key := vis[sel].key
	m.actionMenu = nil
	return m.Update(synthKey(key))
}

func (m Model) updateActionMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	a := m.actionMenu
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if a.typing { // / filter input captures keys
		switch msg.Type {
		case tea.KeyEsc:
			a.typing = false
			a.query = ""
			a.sel = 0
		case tea.KeyEnter:
			return m.runVisibleRow(a.sel)
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(a.query); len(r) > 0 {
				a.query = string(r[:len(r)-1])
			}
			a.sel = 0
		case tea.KeyRunes:
			a.query += string(msg.Runes)
			a.sel = 0
		}
		return m, nil
	}
	switch msg.String() {
	case "z":
		a.mode = a.mode.next()
		a.hscroll = 0
		return m, nil
	case "shift+left":
		if a.mode == modeScroll && a.hscroll > 0 {
			if a.hscroll -= m.hscrollStep(); a.hscroll < 0 {
				a.hscroll = 0
			}
		}
		return m, nil
	case "shift+right":
		if a.mode == modeScroll {
			a.hscroll += m.hscrollStep()
		}
		return m, nil
	case "esc":
		m.actionMenu = nil
		return m, nil
	case "/":
		a.typing = true
		a.query = ""
		a.sel = 0
		return m, nil
	case "up", "k":
		a.move(-1)
		return m, nil
	case "down", "j":
		a.move(1)
		return m, nil
	case "enter":
		return m.runVisibleRow(a.sel)
	}
	// Direct key: run the visible row whose key matches.
	vis := a.visible()
	for i, r := range vis {
		if r.key == msg.String() {
			return m.runVisibleRow(i)
		}
	}
	return m, nil
}

// renderActionMenu draws the overlay (composited by render via overlayCenter),
// mirroring renderRepoPopup.
func (m Model) renderActionMenu() string {
	a := m.actionMenu
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	vis := a.visible()
	var bodyLines []string
	if len(vis) == 0 {
		bodyLines = []string{padRight("  (no match)", inner)}
	} else {
		wr := make([]winRow, len(vis))
		for i, r := range vis {
			prefix := "  "
			var st lipgloss.Style
			if i == a.sel {
				prefix, st = "> ", selectedRow
			}
			wr[i] = winRow{text: prefix + r.label, style: st}
		}
		h := len(vis)
		if h > 14 {
			h = 14
		}
		bodyLines = renderWindow(wr, winOpts{w: inner, h: h, mode: a.mode, anchor: a.sel, hscroll: a.hscroll})
	}
	header := "Actions"
	if a.typing {
		header += "  /" + a.query + "█"
	} else if a.query != "" {
		header += "  /" + a.query
	}
	parts := []string{header, ""}
	parts = append(parts, bodyLines...)
	parts = append(parts, "", "[key]/[enter] run  [/] filter  [z] mode  [esc] close")
	return modalStyle.Width(inner).Render(strings.Join(parts, "\n")) + "\n"
}
```

- [ ] **Step 4: Add the `actionMenu` field, the `.` case, and routing in `model.go`**

Add the field next to the other popup pointers (near `stashAction *stashActionPopup` ~model.go:49):
```go
	actionMenu *actionMenu // . action menu; nil = closed
```
Add routing FIRST among the popup checks (so it owns the keyboard while open), right after the modal block and before `m.repoPopup` (model.go:295):
```go
		if m.actionMenu != nil {
			return m.updateActionMenuKey(msg)
		}
```
Add the open case in the base key switch, next to `case ","` (model.go:578):
```go
		case ".":
			return m.openActionMenu(), nil
```
(No `running`/`loading` guard: the menu lists whatever is available, which during an op is just the few actions whose predicate is true. It only reaches this case from the base layout because every popup/modal/view returns earlier.)

- [ ] **Step 5: Add the `actions` footer binding + render + mouse + help row**

In `internal/tui/footer.go` `globalBindings`, add (before `tab`, so it sits among the real actions):
```go
	{"actions", ".", "[.] actions", func(Model) bool { return true }},
```
In `internal/tui/view.go`, add to the overlay cascade FIRST among popups (right after the modal check, before `renderWorktreePopup`/the others ~view.go:117-140 — match the routing order):
```go
	if m.actionMenu != nil {
		return overlayCenter(bg, m.renderActionMenu(), w, h)
	}
```
In `internal/tui/mouse.go` `handleMouse`, add to the popup-swallow precedence right after the modal check (so a click/wheel over the menu doesn't fall through):
```go
	if m.actionMenu != nil {
		if wheel := wheelDelta(msg, m.wheelStep()); wheel != 0 {
			m.actionMenu.move(wheel)
		}
		return m, nil
	}
```
(Use the same wheel helper the content popup uses; check `mouse.go:38` for the exact `contentPopup.move(wheel)` pattern and mirror it.)
In `internal/tui/help.go`, add a `.` row in the Global section (next to the other global keys) AND a section, so `TestHelpFooterCoverage` (which now sees `[.] actions`) passes:
```go
		r(".", "open the action menu (list + run any available action)"),
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/tui/ -run 'ActionMenu|DotOpens|DotNoOp|Help|Footer' -v`
Expected: PASS (menu opens/runs/closes; `.` no-op under a popup; help/footer drift guard green).

- [ ] **Step 7: Commit**

```bash
git add internal/tui/action_menu.go internal/tui/model.go internal/tui/view.go internal/tui/mouse.go internal/tui/footer.go internal/tui/help.go internal/tui/action_menu_test.go
git commit -m "feat(tui): . action menu (list + run available actions)"
```

---

### Task 5: `footer_actions` / `menu_actions` allowlists

**Files:**
- Modify: `internal/tui/footer.go` (`footerLine`, `bindingByID`)
- Modify: `internal/tui/action_menu.go` (`openActionMenu` applies `menu_actions`)
- Test: `internal/tui/footer_test.go`, `internal/tui/action_menu_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/footer_test.go`:
```go
func TestFooterActionsAllowlistFiltersAndOrders(t *testing.T) {
	m := footerModel()
	m.loading = false
	m.cfg.UI.FooterActions = []string{"commit", "pull"} // order matters
	line := m.footerLine()
	ci, pi := strings.Index(line, "[c] commit"), strings.Index(line, "[p]ull")
	if ci < 0 || pi < 0 {
		t.Fatalf("allowlisted actions missing: %q", line)
	}
	if ci > pi {
		t.Errorf("order not honored (commit should precede pull): %q", line)
	}
	if strings.Contains(line, "[u]ndo") {
		t.Errorf("non-allowlisted action leaked: %q", line)
	}
	if !strings.Contains(line, "[.] actions") {
		t.Errorf("[.] actions must always stay in the footer: %q", line)
	}
}
```
Add to `internal/tui/action_menu_test.go`:
```go
func TestMenuActionsAllowlistFiltersAndOrders(t *testing.T) {
	m := footerModel()
	m.loading = false
	m.cfg.UI.MenuActions = []string{"repo", "pull"}
	mm := m.openActionMenu()
	got := []string{}
	for _, r := range mm.actionMenu.rows {
		got = append(got, r.id)
	}
	if len(got) != 2 || got[0] != "repo" || got[1] != "pull" {
		t.Errorf("menu rows = %v, want [repo pull] in order", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run 'FooterActionsAllowlist|MenuActionsAllowlist' -v`
Expected: FAIL — footer shows all; menu shows all (allowlist not applied).

- [ ] **Step 3: Apply the allowlists**

In `internal/tui/footer.go`, add a lookup and the footer filter. After the
`m.stashView` early-returns in `footerLine`, before the default two-group build:
```go
	if ids := m.cfg.UI.FooterActions; len(ids) > 0 {
		var labels []string
		for _, id := range ids {
			if b, ok := bindingByID(id); ok && b.when(m) {
				labels = append(labels, b.label)
			}
		}
		if b, ok := bindingByID("actions"); ok { // always discoverable
			labels = append(labels, b.label)
		}
		return strings.Join(labels, " ")
	}
```
And the lookup helper (ids are unique per Task 2's guard, so first match is the only match):
```go
func bindingByID(id string) (footerBinding, bool) {
	for _, b := range contextBindings {
		if b.id == id {
			return b, true
		}
	}
	for _, b := range globalBindings {
		if b.id == id {
			return b, true
		}
	}
	return footerBinding{}, false
}
```
In `internal/tui/action_menu.go`, apply `menu_actions` in `openActionMenu`:
```go
func (m Model) openActionMenu() Model {
	rows := availableActions(m)
	if ids := m.cfg.UI.MenuActions; len(ids) > 0 {
		byID := make(map[string]actionRow, len(rows))
		for _, r := range rows {
			byID[r.id] = r
		}
		ordered := make([]actionRow, 0, len(ids))
		for _, id := range ids {
			if r, ok := byID[id]; ok {
				ordered = append(ordered, r)
			}
		}
		rows = ordered
	}
	m.actionMenu = &actionMenu{rows: rows}
	return m
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/tui/ -run 'FooterActionsAllowlist|MenuActionsAllowlist|Footer|ActionMenu' -v`
Expected: PASS (unset still = default footer / all menu rows — existing footer tests green).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/footer.go internal/tui/action_menu.go internal/tui/footer_test.go internal/tui/action_menu_test.go
git commit -m "feat(tui): footer_actions / menu_actions allowlists filter+order"
```

---

### Task 6: Docs

**Files:**
- Modify: `internal/tui/help.go` (new "Action menu (.)" section)
- Modify: `README.md`, `CHANGELOG.md`
- Test: `internal/tui/help_test.go` (drift guard runs in the suite)

- [ ] **Step 1: help.go — add the menu section**

After the "Repo switcher (R)" section, add:
```go
		h("Action menu (.)"),
		r("(key)", "run the action with that key (e.g. p = pull)"),
		r("↑/k ↓/j", "move; enter runs the highlighted action"),
		r("/", "filter the list by name (enter keeps, esc cancels)"),
		r("z", "cycle text display: cutoff / wrap / scroll"),
		r("esc", "close"),
```
(The Global `.` row was added in Task 4.)

- [ ] **Step 2: README — `.` key row + config**

In the TUI key table add a row:
```
| `.` | open the **action menu**: a popup listing every action available right now; press an action's key to run it, or `↑`/`↓` + `enter`; `/` filters, `z` cycles display mode, `esc` closes |
```
In the Configuration section, document the two lists under `[ui]`:
```
`[ui] footer_actions` and `[ui] menu_actions` are lists of action **ids** that
choose which actions appear in the footer bar and in the `.` menu respectively;
each is unset/empty by default (show everything). Ids: `pull push commit amend
stashes undo order view filter repo settings resolve reload help quit` and the
context actions `switch branch worktree delete-branch delete-worktree mark
unmark pair stage unstage file-diff stash mark-file commit-files switch-worktree`.
```

- [ ] **Step 3: CHANGELOG — entry**

Under the window-framework area add:
```
#### TUI action menu (.)
- **`.`** opens an action menu listing every action available in the current
  context; press the action's key to run it, or `↑`/`↓` + `enter` (`/` filters,
  `z` cycles display mode). New `[ui] footer_actions` and `[ui] menu_actions`
  config lists (action ids; unset/empty = show all) choose which actions appear
  in the footer bar versus only in the menu.
```

- [ ] **Step 4: Run the drift guard + gofmt**

Run: `go test ./internal/tui/ -run 'Help|Footer' -v && gofmt -l internal/`
Expected: PASS, no gofmt diffs.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/help.go README.md CHANGELOG.md
git commit -m "docs(tui): document the . action menu + footer/menu config"
```

---

## Final verification

- [ ] `./test.sh race` green (unit + e2e).
- [ ] Manual smoke (optional): `.` opens the menu; `p` runs pull; `/` filters; `z`
      cycles; `esc` closes; `.` under a popup is inert. Set
      `[ui] footer_actions = ["pull","commit"]` in a `.gg.toml` and confirm the
      footer shrinks to `[p]ull [c] commit  [.] actions`; set `menu_actions` and
      confirm the menu narrows.

## Self-review notes

- **Spec coverage:** ids catalog (Task 2) · menu open/keys/execute (Task 4) ·
  keypress-replay `synthKey` (Task 3) · two allowlists, unset=all (Tasks 1, 5) ·
  navigation excluded (Tasks 2, 3) · `actions` always-in-footer / never-in-menu
  (Tasks 4, 5, 3) · routing/render/mouse together (Task 4) · docs (Task 6).
- **Type consistency:** `footerBinding{id,key,label,when}`, `actionRow{id,key,label}`,
  `actionMenu{rows,sel,typing,query,mode,hscroll}`, `availableActions(Model) []actionRow`,
  `synthKey(string) tea.KeyMsg`, `bindingByID(string) (footerBinding, bool)`,
  `openActionMenu`/`updateActionMenuKey`/`renderActionMenu`/`runVisibleRow` — used
  consistently across tasks.
- **No engine/domain/CLI/MCP change** (TUI + config only).
