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

// stageTestModel: a loaded model on a repo with one unstaged modification,
// focused on the Status panel with that file selected.
func stageTestModel(t *testing.T) (Model, string) {
	t.Helper()
	dir, repo := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelFiles
	m.sel[panelFiles] = 0
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

func TestSpaceOnConflictedFileIsNoOp(t *testing.T) {
	dir, repo := newRepoDir(t)
	// Build a merge conflict on c.txt so it becomes an unmerged Status row.
	gitInDir(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("feat\n"), 0o644)
	gitInDir(t, dir, "add", ".")
	gitInDir(t, dir, "commit", "-m", "feat")
	gitInDir(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("main\n"), 0o644)
	gitInDir(t, dir, "add", ".")
	gitInDir(t, dir, "commit", "-m", "main")
	merge := exec.Command("git", "merge", "feat")
	merge.Dir = dir
	merge.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	_ = merge.Run() // expected to conflict (non-zero) — that's the point

	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelFiles
	for i, f := range m.status.Files {
		if f.Path == "c.txt" {
			m.sel[panelFiles] = i
		}
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("space on a conflicted file must not dispatch a stage op")
	}
	if !strings.Contains(m.statusMsg, "resolve conflicts") {
		t.Fatalf("statusMsg = %q, want a 'resolve conflicts' hint", m.statusMsg)
	}
}

func TestSpaceUnstagesFullyStagedFile(t *testing.T) {
	m, dir := stageTestModel(t)
	gitInDir(t, dir, "add", "README.md")
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelStaged // a fully-staged file now lives in the Staged panel
	m.sel[panelStaged] = 0

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = driveStage(t, updated.(Model), cmd)
	for _, f := range m.status.Files {
		if f.Path == "README.md" && f.Staged != '.' && f.Staged != 0 {
			t.Fatalf("README.md should be unstaged; staged byte = %q", f.Staged)
		}
	}
}

func TestFilesStagedMembership(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 30
	m.status.Files = []model.FileStatus{
		{Path: "untracked.txt", Kind: model.KindUntracked, Staged: '?', Unstaged: '?'},
		{Path: "unstaged.go", Kind: model.KindTracked, Staged: '.', Unstaged: 'M'},
		{Path: "staged.go", Kind: model.KindTracked, Staged: 'M', Unstaged: '.'},
		{Path: "partial.go", Kind: model.KindTracked, Staged: 'M', Unstaged: 'M'},
		{Path: "conflict.go", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'},
	}
	wantFiles := map[string]bool{"untracked.txt": true, "unstaged.go": true, "partial.go": true, "conflict.go": true}
	wantStaged := map[string]bool{"staged.go": true, "partial.go": true}
	if got := pathsOf(t, m, panelFiles); !sameSet(got, wantFiles) {
		t.Errorf("Files panel = %v, want %v", got, wantFiles)
	}
	if got := pathsOf(t, m, panelStaged); !sameSet(got, wantStaged) {
		t.Errorf("Staged panel = %v, want %v", got, wantStaged)
	}
}

func pathsOf(t *testing.T, m Model, p panel) []string {
	t.Helper()
	_, idx := m.panelView(p)
	out := make([]string, len(idx))
	for n, i := range idx {
		out[n] = m.status.Files[i].Path
	}
	return out
}

func sameSet(got []string, want map[string]bool) bool {
	if len(got) != len(want) {
		return false
	}
	for _, g := range got {
		if !want[g] {
			return false
		}
	}
	return true
}
