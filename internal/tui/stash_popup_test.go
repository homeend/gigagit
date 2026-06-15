package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/model"
)

func TestSOpensStashPopupWithCandidates(t *testing.T) {
	m := statusModel() // a.go, b.go both unstaged 'M', branch main
	mm, _ := m.Update(keyMsg("s"))
	got := mm.(Model)
	if got.stashPopup == nil {
		t.Fatal("s on Status should open the stash popup")
	}
	if got.stashPopup.name != "WIP on main" {
		t.Errorf("default name = %q, want %q", got.stashPopup.name, "WIP on main")
	}
	if len(got.stashPopup.files) != 2 {
		t.Fatalf("want 2 candidate files, got %d", len(got.stashPopup.files))
	}
	for _, f := range got.stashPopup.files { // nothing marked → all checked
		if !f.included {
			t.Errorf("%s should default to included", f.path)
		}
	}
}

func TestStashPopupPrechecksMarks(t *testing.T) {
	m := statusModel()
	m.fileMarks = map[string]bool{"a.go": true}
	mm, _ := m.Update(keyMsg("s"))
	p := mm.(Model).stashPopup
	inc := map[string]bool{}
	for _, f := range p.files {
		inc[f.path] = f.included
	}
	if !inc["a.go"] || inc["b.go"] {
		t.Errorf("only a.go should be pre-checked, got %v", inc)
	}
}

func TestStashPopupOpAssembly(t *testing.T) {
	p := &stashPopup{name: "WIP on main", files: []stashFileItem{
		{path: "a.go", included: true, untracked: false},
		{path: "b.go", included: false},
		{path: "c.txt", included: true, untracked: true},
	}}
	op, ok := p.op()
	if !ok {
		t.Fatal("op should be ok with ≥1 included")
	}
	if op.Message != "WIP on main" || len(op.Paths) != 2 || !op.IncludeUntracked {
		t.Errorf("op = %+v (want a.go,c.txt + untracked)", op)
	}
}

func TestStashPopupCtrlSStashesRealRepo(t *testing.T) {
	dir, repo := newRepoDir(t)
	gitInDir(t, dir, "checkout", "-b", "work")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o644)
	gitInDir(t, dir, "add", "a.txt")
	gitInDir(t, dir, "commit", "-m", "baseline")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644)

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelStatus
	m.fileMarks = map[string]bool{"a.txt": true}

	m = pressRune(t, m, "s")
	if m.stashPopup == nil {
		t.Fatal("s must open the stash popup with a dirty file")
	}
	upd, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = upd.(Model)
	if m.stashPopup != nil {
		t.Fatal("ctrl+s must close the popup")
	}
	if m.fileMarks["a.txt"] {
		t.Error("stashed file's mark should be cleared")
	}
	m = driveOp(t, m, cmd)

	out, _ := exec.Command("git", "-C", dir, "stash", "list").Output()
	if !strings.Contains(string(out), "stash@{0}") {
		t.Fatalf("expected a stash entry, got %q", out)
	}
}

func TestStashPopupEmptySelectionRefuses(t *testing.T) {
	m := statusModel()
	mm, _ := m.Update(keyMsg("s"))
	m = mm.(Model)
	for i := range m.stashPopup.files {
		m.stashPopup.files[i].included = false
	}
	mm, _ = m.updateStashPopupKey(keyMsg("ctrl+s"))
	if mm.(Model).stashPopup == nil {
		t.Fatal("empty selection must not submit/close")
	}
}

func TestSNoCandidatesNoOp(t *testing.T) {
	m := Model{width: 100, height: 30, focus: panelStatus, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Branch: "main"} // no files
	mm, _ := m.Update(keyMsg("s"))
	if mm.(Model).stashPopup != nil {
		t.Fatal("s with nothing to stash should not open the popup")
	}
}

func TestStashPopupRendersFiles(t *testing.T) {
	m := statusModel()
	mm, _ := m.Update(keyMsg("s"))
	out := mm.(Model).renderStashPopup()
	if !contains(out, "a.go") || !contains(out, "WIP on main") {
		t.Errorf("popup should show files + name:\n%s", out)
	}
}
