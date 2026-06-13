package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/textdiff"
)

// gitIn runs a git command in dir with standard test env vars.
func gitIn(t *testing.T, dir string, args ...string) {
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

// diffModel is a fixture focused on the Status panel: one modified, one
// untracked, one conflicted file.
func diffModel() Model {
	m := footerModel()
	m.focus = panelStatus
	m.status.Files = []model.FileStatus{
		{Path: "mod.txt", Staged: '.', Unstaged: 'M'},
		{Path: "new.txt", Staged: '.', Unstaged: '?', Kind: model.KindUntracked},
		{Path: "conflict.txt", Staged: 'U', Unstaged: 'U', Kind: model.KindUnmerged},
	}
	return m
}

func TestEnterOnStatusOpensLoadingDiff(t *testing.T) {
	m := diffModel()
	u, cmd := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.diffView == nil || !mm.diffView.loading {
		t.Fatal("enter on a status row must open a loading diff view")
	}
	if cmd == nil {
		t.Fatal("enter must return the loader cmd")
	}
	if mm.diffTag != "status:mod.txt" {
		t.Fatalf("diffTag = %q", mm.diffTag)
	}
}

func TestEnterOnConflictedRowIsNoOp(t *testing.T) {
	m := diffModel()
	m.sel[panelStatus] = 2 // conflict.txt
	u, cmd := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.diffView != nil || cmd != nil {
		t.Fatal("enter on a conflicted row must be a no-op until the conflict editor")
	}
}

func TestEnterOnStatusRefusedWhenNarrow(t *testing.T) {
	m := diffModel()
	m.width = 59
	u, cmd := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.diffView != nil || cmd != nil {
		t.Fatal("enter below 60 cols must not open the diff view")
	}
}

func TestCanShowFileDiffZeroWidthAllowed(t *testing.T) {
	m := diffModel()
	m.width = 0 // before the first WindowSizeMsg
	if !m.canShowFileDiff() {
		t.Fatal("width 0 (unknown) must not refuse the diff view")
	}
}

func TestEnterOnStatusNoOpWhileRunning(t *testing.T) {
	m := diffModel()
	m.running = true
	u, cmd := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.diffView != nil || cmd != nil {
		t.Fatal("enter must be a no-op while an op runs")
	}
}

func TestStatusLoaderModifiedFile(t *testing.T) {
	dir, repo := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "f.txt")
	gitIn(t, dir, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\nTWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := diffModel()
	m.repo = repo
	m.currentWorktree = dir
	msg := m.loadStatusDiffCmd(model.FileStatus{Path: "f.txt", Staged: '.', Unstaged: 'M'})().(diffMsg)
	if msg.view.err != nil {
		t.Fatal(msg.view.err)
	}
	if len(msg.view.blocks) != 1 {
		t.Fatalf("blocks = %v, want one change", msg.view.blocks)
	}
	if msg.tag != "status:f.txt" {
		t.Fatalf("tag = %q", msg.tag)
	}
}

func TestStatusLoaderUntrackedIsAllAdded(t *testing.T) {
	dir, repo := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "u.txt"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := diffModel()
	m.repo = repo
	m.currentWorktree = dir
	msg := m.loadStatusDiffCmd(model.FileStatus{Path: "u.txt", Kind: model.KindUntracked})().(diffMsg)
	if msg.view.err != nil {
		t.Fatal(msg.view.err)
	}
	for _, r := range msg.view.rows {
		if r.Kind != textdiff.Add {
			t.Fatalf("untracked file must be all-Add, got %+v", r)
		}
	}
}

func TestStatusLoaderDeletedIsAllDel(t *testing.T) {
	dir, repo := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "d.txt"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "d.txt")
	gitIn(t, dir, "commit", "-m", "base")
	if err := os.Remove(filepath.Join(dir, "d.txt")); err != nil {
		t.Fatal(err)
	}
	m := diffModel()
	m.repo = repo
	m.currentWorktree = dir
	msg := m.loadStatusDiffCmd(model.FileStatus{Path: "d.txt", Staged: '.', Unstaged: 'D'})().(diffMsg)
	if msg.view.err != nil {
		t.Fatal(msg.view.err)
	}
	for _, r := range msg.view.rows {
		if r.Kind != textdiff.Del {
			t.Fatalf("deleted file must be all-Del, got %+v", r)
		}
	}
}

func TestDiffMsgStaleTagDropped(t *testing.T) {
	m := diffModel()
	m.diffView = &diffView{loading: true}
	m.diffTag = "status:current.txt"
	u, _ := m.Update(diffMsg{tag: "status:old.txt", view: &diffView{}})
	mm := u.(Model)
	if !mm.diffView.loading {
		t.Fatal("a stale-tagged result must be dropped")
	}
	// And when the view is closed entirely:
	m.diffView = nil
	m.diffTag = ""
	u, _ = m.Update(diffMsg{tag: "status:old.txt", view: &diffView{}})
	if u.(Model).diffView != nil {
		t.Fatal("a result after close must be dropped")
	}
}

func TestDiffViewKeysScrollAndJump(t *testing.T) {
	m := diffModel()
	m.height = 12 // body = 10
	rows := make([]textdiff.Row, 40)
	for i := range rows {
		rows[i] = textdiff.Row{Kind: textdiff.Same}
	}
	rows[20] = textdiff.Row{Kind: textdiff.Changed}
	rows[30] = textdiff.Row{Kind: textdiff.Changed}
	m.diffView = &diffView{rows: rows, blocks: []int{20, 30}}
	m.diffTag = "status:x"

	u, _ := m.Update(keyMsg("down"))
	if u.(Model).diffView.offset != 1 {
		t.Fatalf("down: offset = %d", u.(Model).diffView.offset)
	}
	u, _ = m.Update(keyMsg("ctrl+down"))
	if u.(Model).diffView.offset != 20 {
		t.Fatalf("ctrl+down: offset = %d, want 20", u.(Model).diffView.offset)
	}
	m.diffView.offset = 35
	u, _ = m.Update(keyMsg("ctrl+up"))
	if u.(Model).diffView.offset != 30 {
		t.Fatalf("ctrl+up: offset = %d, want 30", u.(Model).diffView.offset)
	}
	m.diffView.offset = 0
	u, _ = m.Update(keyMsg("pgup"))
	if u.(Model).diffView.offset != 0 {
		t.Fatalf("pgup at top must clamp to 0, got %d", u.(Model).diffView.offset)
	}
	m.diffView.offset = 0
	u, _ = m.Update(keyMsg("pgdown"))
	if got := u.(Model).diffView.offset; got != 10 {
		t.Fatalf("pgdown: offset = %d, want one body page (10)", got)
	}
}

func TestDiffViewEscClosesAndQQuits(t *testing.T) {
	m := diffModel()
	m.diffView = &diffView{}
	m.diffTag = "status:x"
	u, _ := m.Update(keyMsg("esc"))
	mm := u.(Model)
	if mm.diffView != nil || mm.diffTag != "" {
		t.Fatal("esc must close the diff view and clear the tag")
	}

	m.diffView = &diffView{}
	_, cmd := m.Update(keyMsg("q"))
	if cmd == nil {
		t.Fatal("q must quit") // tea.Quit cmd
	}
}

func TestDiffViewSwallowsActionKeys(t *testing.T) {
	m := diffModel()
	m.diffView = &diffView{}
	m.diffTag = "status:x"
	for _, k := range []string{"p", "P", "s", "S", "u", "d", "w", "b", "m", "l", "R", ",", "/", "?", "tab", "enter"} {
		u, cmd := m.Update(keyMsg(k))
		mm := u.(Model)
		if cmd != nil || mm.running || mm.popup != nil || mm.contentPopup != nil || mm.filesView != nil || mm.filterTyping || mm.mark != nil {
			t.Fatalf("key %q leaked through the diff view", k)
		}
	}
}

func TestDiffViewEscReturnsToFilesView(t *testing.T) {
	m := diffModel()
	m.filesView = &contentPopup{lines: []contentLine{{text: "x", path: "x"}}, sel: 0}
	m.filesTreeFocused = true
	m.diffView = &diffView{}
	m.diffTag = "commit:abc:x"
	u, _ := m.Update(keyMsg("esc"))
	mm := u.(Model)
	if mm.diffView != nil {
		t.Fatal("esc must close the diff")
	}
	if mm.filesView == nil || !mm.filesTreeFocused {
		t.Fatal("the files view must be intact beneath the diff")
	}
}

func TestDiffViewClosedOnNarrowResize(t *testing.T) {
	m := diffModel()
	m.diffView = &diffView{}
	m.diffTag = "status:x"
	u, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 24})
	mm := u.(Model)
	if mm.diffView != nil || mm.diffTag != "" {
		t.Fatal("resize below 60 must close the diff view")
	}
	if mm.statusMsg == "" {
		t.Fatal("the close must explain itself in statusMsg")
	}
}

func TestReRootClearsDiffView(t *testing.T) {
	dir, repo := newRepoDir(t)
	m := diffModel()
	m.repo = repo
	m.diffView = &diffView{}
	m.diffTag = "status:x"
	u, _ := m.reRoot(dir)
	mm := u.(Model)
	if mm.diffView != nil || mm.diffTag != "" {
		t.Fatal("reRoot must clear the diff view and tag")
	}
}

func TestDiffViewWheelScrolls(t *testing.T) {
	m := diffModel()
	// Need enough rows that the scroll clamp doesn't fire: body = height-2 = 38,
	// so we need at least body+contentWheelStep rows = 41.
	rows := make([]textdiff.Row, 80)
	m.diffView = &diffView{rows: rows}
	m.diffTag = "status:x"
	u, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if got := u.(Model).diffView.offset; got != contentWheelStep {
		t.Fatalf("wheel: offset = %d, want %d", got, contentWheelStep)
	}
}

func TestDiffViewJumpAtMaxScrollIsNoOp(t *testing.T) {
	m := diffModel()
	m.height = 12 // body = 10
	rows := make([]textdiff.Row, 35)
	for i := range rows {
		rows[i] = textdiff.Row{Kind: textdiff.Same}
	}
	rows[20] = textdiff.Row{Kind: textdiff.Changed}
	rows[30] = textdiff.Row{Kind: textdiff.Changed}
	m.diffView = &diffView{rows: rows, blocks: []int{20, 30}, offset: 20}
	m.diffTag = "status:x"

	// 30 clamps to max (25): the jump advances to 25, and a further press
	// is a clean no-op (the remaining block is already visible).
	u, _ := m.Update(keyMsg("ctrl+down"))
	if got := u.(Model).diffView.offset; got != 25 {
		t.Fatalf("clamped jump: offset = %d, want 25", got)
	}
	u, _ = m.Update(keyMsg("ctrl+down"))
	if got := u.(Model).diffView.offset; got != 25 {
		t.Fatalf("jump at max scroll must hold position, got %d", got)
	}
}
