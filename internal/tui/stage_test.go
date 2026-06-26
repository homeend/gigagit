package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
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

// TestSpaceAsRunesStagesSelectedFile is the Windows regression: Bubble Tea's
// Windows input driver delivers a space keypress as KeyRunes{' '}, not KeySpace
// (key_windows.go), whereas Unix normalizes it to KeySpace. Staging must work
// for both forms — before the normalization fix, space did nothing on Windows.
func TestSpaceAsRunesStagesSelectedFile(t *testing.T) {
	m, _ := stageTestModel(t)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if cmd == nil {
		t.Fatal("space-as-KeyRunes did not trigger staging (Windows path)")
	}
	m = driveStage(t, updated.(Model), cmd)

	var staged byte = '.'
	for _, f := range m.status.Files {
		if f.Path == "README.md" {
			staged = f.Staged
		}
	}
	if staged == '.' || staged == 0 {
		t.Fatalf("README.md not staged after space-as-KeyRunes; staged byte = %q", staged)
	}
}

// multiStageModel: a loaded model on a repo with three unstaged modifications
// (a.txt, b.txt, c.txt), focused on the Files panel.
func multiStageModel(t *testing.T) (Model, string) {
	t.Helper()
	dir, repo := newRepoDir(t)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		os.WriteFile(filepath.Join(dir, name), []byte("seed\n"), 0o644)
	}
	gitInDir(t, dir, "add", ".")
	gitInDir(t, dir, "commit", "-m", "seed")
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		os.WriteFile(filepath.Join(dir, name), []byte("dirty\n"), 0o644)
	}
	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelFiles
	return m, dir
}

func stagedByte(m Model, path string) byte {
	for _, f := range m.status.Files {
		if f.Path == path {
			return f.Staged
		}
	}
	return 0
}

func isStaged(b byte) bool { return b != '.' && b != 0 && b != '?' }

// Marking multiple files in the Files panel and pressing space must stage ALL
// marked files in one op (not just the cursor row) and clear the marks so they
// do not carry over to the Staged panel.
func TestSpaceStagesAllMarkedFiles(t *testing.T) {
	m, _ := multiStageModel(t)
	m.fileMarks = map[string]bool{"a.txt": true, "c.txt": true}
	m.sel[panelFiles] = 0 // cursor is on a.txt; b.txt is unmarked

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = driveStage(t, updated.(Model), cmd)

	if !isStaged(stagedByte(m, "a.txt")) {
		t.Errorf("a.txt should be staged; staged byte = %q", stagedByte(m, "a.txt"))
	}
	if !isStaged(stagedByte(m, "c.txt")) {
		t.Errorf("c.txt should be staged; staged byte = %q", stagedByte(m, "c.txt"))
	}
	if isStaged(stagedByte(m, "b.txt")) {
		t.Errorf("b.txt was not marked and must not be staged; staged byte = %q", stagedByte(m, "b.txt"))
	}
	if len(m.fileMarks) != 0 {
		t.Errorf("marks must be cleared after staging; fileMarks = %v", m.fileMarks)
	}
	// The reported bug: the staged files must not show a mark glyph in the
	// Staged panel.
	if marked := m.markedDisplayIndices(panelStaged); len(marked) != 0 {
		t.Errorf("Staged panel must have no marked rows after staging; got %v", marked)
	}
}

// The same gesture in the Staged panel unstages all marked files.
func TestSpaceUnstagesAllMarkedFiles(t *testing.T) {
	m, dir := multiStageModel(t)
	gitInDir(t, dir, "add", ".") // a,b,c all staged now
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelStaged
	m.fileMarks = map[string]bool{"a.txt": true, "c.txt": true}
	m.sel[panelStaged] = 0

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = driveStage(t, updated.(Model), cmd)

	if isStaged(stagedByte(m, "a.txt")) {
		t.Errorf("a.txt should be unstaged; staged byte = %q", stagedByte(m, "a.txt"))
	}
	if isStaged(stagedByte(m, "c.txt")) {
		t.Errorf("c.txt should be unstaged; staged byte = %q", stagedByte(m, "c.txt"))
	}
	if !isStaged(stagedByte(m, "b.txt")) {
		t.Errorf("b.txt was not marked and must stay staged; staged byte = %q", stagedByte(m, "b.txt"))
	}
	if len(m.fileMarks) != 0 {
		t.Errorf("marks must be cleared after unstaging; fileMarks = %v", m.fileMarks)
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

// TestFilesStagedGlyphs pins the single-letter status each panel shows for its
// own side: Files = working-tree state (untracked → A, modified → M, conflict →
// U), Staged = index state (added → A, modified → M). git's two-byte XY is
// never shown.
func TestFilesStagedGlyphs(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 30
	m.status.Files = []model.FileStatus{
		{Path: "untracked.txt", Kind: model.KindUntracked},
		{Path: "unstaged.go", Kind: model.KindTracked, Staged: '.', Unstaged: 'M'},
		{Path: "staged.go", Kind: model.KindTracked, Staged: 'A', Unstaged: '.'},
		{Path: "partial.go", Kind: model.KindTracked, Staged: 'M', Unstaged: 'M'},
		{Path: "conflict.go", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'},
	}
	// Files panel shows the working-tree letter. staged.go has no worktree
	// change, so it is absent here (membership), not asserted.
	assertGlyphs(t, m, panelFiles, map[string]string{
		"untracked.txt": "A untracked.txt",
		"unstaged.go":   "M unstaged.go",
		"partial.go":    "M partial.go",
		"conflict.go":   "U conflict.go",
	})
	// Staged panel shows the index letter (the X byte).
	assertGlyphs(t, m, panelStaged, map[string]string{
		"staged.go":  "A staged.go",
		"partial.go": "M partial.go",
	})
}

// assertGlyphs checks panel p's rendered rows carry the expected status text
// for the named files (rows align with panelView's backing indices).
func assertGlyphs(t *testing.T, m Model, p panel, want map[string]string) {
	t.Helper()
	rows, idx := m.panelView(p)
	for n, i := range idx {
		path := m.status.Files[i].Path
		w, ok := want[path]
		if !ok {
			continue
		}
		if rows[n] != w {
			t.Errorf("%v row for %s = %q, want %q", p, path, rows[n], w)
		}
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
