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

// q no longer quits from the history view — only the base layout quits on q.
func TestHistoryQInert(t *testing.T) {
	m := Model{width: 100, height: 30}
	h := histFixture()
	m = m.pushSurface(h)
	m, cmd := h.update(m, keyMsg("q"))
	if cmd != nil {
		t.Fatal("q must not quit from the history view (inert)")
	}
	if m.stackTop() == nil {
		t.Fatal("q must leave the history surface on the stack")
	}
}

func TestStatusHOpensHistory(t *testing.T) {
	m := Model{width: 100, height: 30, focus: panelFiles, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "a.go", Unstaged: 'M'}}}
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

func TestStagedHOpensHistory(t *testing.T) {
	m := Model{width: 100, height: 30, focus: panelStaged, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "a.go", Staged: 'M'}}}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	got := mm.(Model)
	h, ok := got.stackTop().(*historyView)
	if !ok {
		t.Fatal("h on a Staged file should push a historyView")
	}
	if h.ctx.path != "a.go" || h.ctx.rev != "" {
		t.Errorf("wrong navContext: %+v", h.ctx)
	}
}

func TestFilesViewHOpensHistory(t *testing.T) {
	m := Model{width: 100, height: 30}
	m.filesView = &contentPopup{lines: []contentLine{{text: "a.go", path: "a.go"}}}
	m.filesTreeFocused = true
	m.filesHash = "abc123"
	mm, _ := m.updateFilesViewKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	h, ok := mm.(Model).stackTop().(*historyView)
	if !ok {
		t.Fatal("h on a files-view row should push a historyView")
	}
	if h.ctx.path != "a.go" || h.ctx.rev != "abc123" {
		t.Errorf("wrong navContext: %+v", h.ctx)
	}
}

func TestDiffViewHOpensHistory(t *testing.T) {
	m := Model{width: 100, height: 30}
	m.diffView = &diffView{title: "a.go", rev: "abc123"}
	mm, _ := m.updateDiffViewKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	h, ok := mm.(Model).stackTop().(*historyView)
	if !ok {
		t.Fatal("h in the diff view should push a historyView")
	}
	if h.ctx.path != "a.go" || h.ctx.rev != "abc123" {
		t.Errorf("wrong navContext: %+v", h.ctx)
	}
}

func TestHistoryViewWrapMode(t *testing.T) {
	h := &historyView{
		ctx:     navContext{path: "x"},
		commits: []model.FileCommit{{Commit: model.Commit{Hash: "abcdef0", Subject: strings.Repeat("w", 80)}, Status: "M", Path: "x"}},
		mode:    modeWrap,
	}
	m := Model{width: 50, height: 20} // < 60 => list-only, easier to assert
	out := h.render(m)
	if strings.Count(out, "w") < 30 {
		t.Errorf("history wrap mode did not expand the subject:\n%s", out)
	}
}
