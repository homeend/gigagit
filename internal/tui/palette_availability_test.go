package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The command palette (ctrl+p) opens over the base panels and read-only browse
// windows, but never over an input popup, an interactive editor, a decision
// modal, or while an op is running.

func TestPaletteReachableAllowsBaseAndBrowseWindows(t *testing.T) {
	if !footerModel().paletteReachable() {
		t.Error("palette should be reachable from the base panels")
	}
	browse := []struct {
		name string
		push func(Model) Model
	}{
		{"diff", func(m Model) Model { return m.pushLayer(&diffView{title: "a.go"}) }},
		{"history", func(m Model) Model { return m.pushLayer(newHistoryView(navContext{path: "a.go"})) }},
		{"blame", func(m Model) Model { return m.pushLayer(newBlameView(navContext{path: "a.go"})) }},
	}
	for _, c := range browse {
		if !c.push(footerModel()).paletteReachable() {
			t.Errorf("palette should be reachable over the %s window", c.name)
		}
	}
}

func TestPaletteNotReachableOverPopupsEditorsModalsBusy(t *testing.T) {
	block := []struct {
		name string
		mut  func(Model) Model
	}{
		{"goto-commit popup", func(m Model) Model { return m.pushLayer(&gotoCommitPopup{input: newTextField("")}) }},
		{"command palette itself", func(m Model) Model { return m.pushLayer(&commandPalette{cmds: paletteCommands()}) }},
		{"review viewer (matches g/G/F, which it lacks)", func(m Model) Model { return m.pushLayer(newReviewView("t", "/p", "body")) }},
		{"interactive rebase", func(m Model) Model { return m.pushLayer(&irebaseEditor{}) }},
		{"hunk picker", func(m Model) Model { return m.pushLayer(&hunkPicker{}) }},
		{"decision modal", func(m Model) Model { m.modal = &decisionState{}; return m }},
		{"op running", func(m Model) Model { m.running = true; return m }},
	}
	for _, c := range block {
		if c.mut(footerModel()).paletteReachable() {
			t.Errorf("palette must NOT be reachable while %s is active", c.name)
		}
	}
}

// Integration: ctrl+p opens the palette over the files-view field surface, which
// previously swallowed every key (updateFilesViewKey) so the palette was
// unreachable there — the reported bug.
func TestCtrlPOpensPaletteOverFilesView(t *testing.T) {
	m := footerModel()
	m.filesView = &contentPopup{lines: []contentLine{{text: "a.go", path: "a.go"}}}
	m, _ = send(m, keyType(tea.KeyCtrlP))
	if layerOf[*commandPalette](m) == nil {
		t.Fatal("ctrl+p over the files view should open the command palette")
	}
}

// Integration: ctrl+p opens the palette on top of a browse layer (diff).
func TestCtrlPOpensPaletteOverDiff(t *testing.T) {
	m := footerModel().pushLayer(&diffView{title: "a.go"})
	m, _ = send(m, keyType(tea.KeyCtrlP))
	if p := layerOf[*commandPalette](m); p == nil || p != m.topLayer() {
		t.Fatal("ctrl+p over a diff window should open the palette on top")
	}
}

// Integration: ctrl+p over an input popup is swallowed by the popup (no palette).
func TestCtrlPBlockedOverPopup(t *testing.T) {
	m := footerModel().pushLayer(&gotoCommitPopup{input: newTextField("")})
	m, _ = send(m, keyType(tea.KeyCtrlP))
	if layerOf[*commandPalette](m) != nil {
		t.Fatal("ctrl+p must not open the palette over an input popup")
	}
}

// The palette renders over the files-view field surface without panicking.
func TestPaletteRendersOverFilesView(t *testing.T) {
	m := footerModel()
	m.filesView = &contentPopup{lines: []contentLine{{text: "a.go", path: "a.go"}}}
	m, _ = send(m, keyType(tea.KeyCtrlP))
	if !strings.Contains(m.View(), "Commands") {
		t.Fatal("palette should render over the files view")
	}
}

// Running a command from the palette while a browse window (diff) is beneath
// works end-to-end: it renders, "Find" pops the palette and opens the finder,
// and the diff window remains beneath it. Guards the run+render path that
// layer-presence assertions alone don't cover.
func TestPaletteRunsAndRendersOverDiff(t *testing.T) {
	base := gotoModel(t, gotoFullHash).pushLayer(&diffView{title: "a.go"})
	opened, _ := send(base, keyType(tea.KeyCtrlP))
	if !strings.Contains(opened.View(), "Commands") {
		t.Fatal("palette should render over the diff window")
	}
	m, _ := palettePick(t, base, "Find")
	if layerOf[*fileFinderPopup](m) == nil {
		t.Fatal("running Find over a diff should open the finder")
	}
	if layerOf[*diffView](m) == nil {
		t.Fatal("the diff window should remain beneath the finder")
	}
	_ = m.View() // finder-over-diff must render without panicking
}
