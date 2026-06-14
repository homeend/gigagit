package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
)

// stageTestModel: a loaded model on a repo with one unstaged modification,
// focused on the Status panel with that file selected.
func stageTestModel(t *testing.T) (Model, string) {
	t.Helper()
	dir, repo := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelStatus
	m.sel[panelStatus] = 0
	return m, dir
}

// gitInDir runs a raw git command in dir with frozen identity.
func gitInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// driveStage drives a stageCmd to completion (it returns one statusRefreshedMsg).
func driveStage(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a stage command")
	}
	updated, _ := m.Update(cmd())
	return updated.(Model)
}

func TestSpaceStagesSelectedFile(t *testing.T) {
	m, _ := stageTestModel(t)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = driveStage(t, updated.(Model), cmd)

	var staged byte = '.'
	for _, f := range m.status.Files {
		if f.Path == "README.md" {
			staged = f.Staged
		}
	}
	if staged == '.' || staged == 0 {
		t.Fatalf("README.md not staged after space; staged byte = %q", staged)
	}
	if m.running {
		t.Fatal("running must be cleared after the status refresh")
	}
}

func TestSpaceUnstagesFullyStagedFile(t *testing.T) {
	m, dir := stageTestModel(t)
	gitInDir(t, dir, "add", "README.md")
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelStatus
	m.sel[panelStatus] = 0

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = driveStage(t, updated.(Model), cmd)
	for _, f := range m.status.Files {
		if f.Path == "README.md" && f.Staged != '.' && f.Staged != 0 {
			t.Fatalf("README.md should be unstaged; staged byte = %q", f.Staged)
		}
	}
}
