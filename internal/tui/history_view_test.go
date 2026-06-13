package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

func histFixture() *historyView {
	return &historyView{
		ctx: navContext{path: "a.go", rev: ""},
		commits: []model.FileCommit{
			{Commit: model.Commit{Hash: "aaaaaaa", Subject: "edit", Author: "Ada"}, Status: "M", Path: "a.go"},
			{Commit: model.Commit{Hash: "bbbbbbb", Subject: "add", Author: "Bob"}, Status: "A", Path: "a.go"},
		},
	}
}

func TestHistoryRenderListsCommits(t *testing.T) {
	m := Model{width: 100, height: 30}
	h := histFixture()
	out := h.render(m)
	if !strings.Contains(out, "edit") || !strings.Contains(out, "add") {
		t.Errorf("history render missing commit subjects:\n%s", out)
	}
	if !strings.Contains(out, "a.go") {
		t.Errorf("history header missing path:\n%s", out)
	}
}

func TestHistoryDownMovesSelectionAndReloads(t *testing.T) {
	m := Model{width: 100, height: 30}
	h := histFixture()
	m = m.pushSurface(h)
	_, cmd := h.update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if h.sel != 1 {
		t.Fatalf("j should move selection to 1, got %d", h.sel)
	}
	if cmd == nil {
		t.Fatal("moving selection should fire a right-pane reload cmd")
	}
}

func TestHistoryEscPops(t *testing.T) {
	m := Model{width: 100, height: 30}
	h := histFixture()
	m = m.pushSurface(h)
	m, _ = h.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.stackTop() != nil {
		t.Fatal("esc should pop the history surface")
	}
}

func TestStatusHOpensHistory(t *testing.T) {
	m := Model{width: 100, height: 30, focus: panelStatus, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "a.go"}}}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	got := mm.(Model)
	h, ok := got.stackTop().(*historyView)
	if !ok {
		t.Fatal("h on a Status file should push a historyView")
	}
	if h.ctx.path != "a.go" || h.ctx.rev != "" {
		t.Errorf("wrong navContext: %+v", h.ctx)
	}
}
