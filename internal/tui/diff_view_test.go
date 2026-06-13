package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/textdiff"
)

// diffViewWith builds a full-mode view over the given rows and block starts.
func diffViewWith(rows []textdiff.Row, blocks []int) *diffView {
	v := &diffView{full: rows, fullBlocks: blocks}
	v.rebuild()
	return v
}

// gitOut runs a git command in dir and returns its trimmed stdout.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// filesViewModel builds an open files view with a focused tree and payload rows.
func filesViewModel() Model {
	m := footerModel()
	m.focus = panelCommits
	m.filesView = &contentPopup{lines: []contentLine{
		{text: "dir/", heading: true},
		{text: "  M  f.go", path: "dir/f.go", status: "M"},
	}, sel: 1}
	m.filesHash = "abc1234def"
	m.filesTitle = "Files abc1234 subject line"
	m.filesTreeFocused = true
	return m
}

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
	m.svc = domain.New(repo)
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
	m.svc = domain.New(repo)
	m.currentWorktree = dir
	msg := m.loadStatusDiffCmd(model.FileStatus{Path: "u.txt", Kind: model.KindUntracked})().(diffMsg)
	if msg.view.err != nil {
		t.Fatal(msg.view.err)
	}
	for _, r := range msg.view.full {
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
	m.svc = domain.New(repo)
	m.currentWorktree = dir
	msg := m.loadStatusDiffCmd(model.FileStatus{Path: "d.txt", Staged: '.', Unstaged: 'D'})().(diffMsg)
	if msg.view.err != nil {
		t.Fatal(msg.view.err)
	}
	for _, r := range msg.view.full {
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
	m.diffView = diffViewWith(rows, []int{20, 30})
	m.diffTag = "status:x"

	u, _ := m.Update(keyMsg("down"))
	if u.(Model).diffView.offset != 1 {
		t.Fatalf("down: offset = %d", u.(Model).diffView.offset)
	}
	u, _ = m.Update(keyMsg("ctrl+down"))
	if u.(Model).diffView.offset != 17 {
		t.Fatalf("ctrl+down: offset = %d, want 17", u.(Model).diffView.offset)
	}
	m.diffView.offset = 35
	u, _ = m.Update(keyMsg("ctrl+up"))
	if u.(Model).diffView.offset != 27 {
		t.Fatalf("ctrl+up: offset = %d, want 27", u.(Model).diffView.offset)
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
	m.svc = domain.New(repo)
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
	// so we need at least body+wheelStep rows = 41.
	rows := make([]textdiff.Row, 80)
	m.diffView = diffViewWith(rows, nil)
	m.diffTag = "status:x"
	u, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if got, want := u.(Model).diffView.offset, m.wheelStep(); got != want {
		t.Fatalf("wheel: offset = %d, want %d", got, want)
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
	m.diffView = diffViewWith(rows, []int{20, 30})
	m.diffView.offset = 20
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

func TestEnterInTreeOpensCommitDiff(t *testing.T) {
	m := filesViewModel()
	u, cmd := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.diffView == nil || !mm.diffView.loading || cmd == nil {
		t.Fatal("enter on a tree file row must open a loading diff")
	}
	if mm.diffTag != "commit:abc1234def:dir/f.go" {
		t.Fatalf("diffTag = %q", mm.diffTag)
	}
	if mm.filesView == nil {
		t.Fatal("the files view must stay open beneath the diff")
	}
	if !strings.Contains(mm.diffView.context, "abc1234") {
		t.Fatalf("context = %q, want the short hash", mm.diffView.context)
	}
}

func TestEnterInTreeNoOpOnHeading(t *testing.T) {
	m := filesViewModel()
	m.filesView.sel = 0 // the heading row
	u, cmd := m.Update(keyMsg("enter"))
	if u.(Model).diffView != nil || cmd != nil {
		t.Fatal("enter on a heading row must be a no-op")
	}
}

func TestEnterInTreeNoOpOnCommitsSide(t *testing.T) {
	m := filesViewModel()
	m.filesTreeFocused = false
	u, cmd := m.Update(keyMsg("enter"))
	if u.(Model).diffView != nil || cmd != nil {
		t.Fatal("enter on the commits side must not open a diff")
	}
}

func TestEnterInTreeNarrowRefusalExplains(t *testing.T) {
	m := filesViewModel()
	m.width = 55 // files view open (>=40) but diff needs >=60
	u, cmd := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.diffView != nil || cmd != nil {
		t.Fatal("enter at 55 cols must not open the diff")
	}
	if mm.statusMsg == "" {
		t.Fatal("the refusal must explain itself in statusMsg")
	}
}

func TestCommitLoaderModifiedAndAdded(t *testing.T) {
	dir, repo := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "f.txt")
	gitIn(t, dir, "commit", "-m", "c1")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "g.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "c2")
	hash := gitOut(t, dir, "rev-parse", "HEAD")

	m := filesViewModel()
	m.svc = domain.New(repo)

	// Modified: parent vs commit.
	msg := m.loadCommitDiffCmd(hash, contentLine{path: "f.txt", status: "M"})().(diffMsg)
	if msg.view.err != nil {
		t.Fatal(msg.view.err)
	}
	if len(msg.view.blocks) != 1 || msg.view.full[0].Kind != textdiff.Changed {
		t.Fatalf("modified file rows wrong: %+v", msg.view.full)
	}

	// Added in this commit: old side empty.
	msg = m.loadCommitDiffCmd(hash, contentLine{path: "g.txt", status: "A"})().(diffMsg)
	if msg.view.err != nil {
		t.Fatal(msg.view.err)
	}
	for _, r := range msg.view.full {
		if r.Kind != textdiff.Add {
			t.Fatalf("added file must be all-Add: %+v", r)
		}
	}
}

func TestCommitLoaderRootCommit(t *testing.T) {
	dir, repo := newRepoDir(t)
	hash := gitOut(t, dir, "rev-list", "--max-parents=0", "HEAD")
	// Every root-commit file has status "A" (CommitFiles passes --root), so
	// the loader never dereferences hash^.
	files, err := repo.CommitFiles(context.Background(), hash)
	if err != nil {
		t.Fatal(err)
	}
	m := filesViewModel()
	m.svc = domain.New(repo)
	for _, f := range files {
		if f.Status != "A" {
			t.Fatalf("root commit file %q has status %q, want A", f.Path, f.Status)
		}
		msg := m.loadCommitDiffCmd(hash, contentLine{path: f.Path, status: f.Status})().(diffMsg)
		if msg.view.err != nil {
			t.Fatalf("root-commit diff failed: %v", msg.view.err)
		}
	}
}

// sameRowsTUI builds n Same rows then marks the given indices Changed.
func sameRowsTUI(n int, changed ...int) []textdiff.Row {
	rows := make([]textdiff.Row, n)
	for i := range rows {
		rows[i] = textdiff.Row{Kind: textdiff.Same, LeftNo: i + 1, RightNo: i + 1}
	}
	for _, c := range changed {
		rows[c] = textdiff.Row{Kind: textdiff.Changed, Left: "x", Right: "y", LeftNo: c + 1, RightNo: c + 1}
	}
	return rows
}

func TestDiffNPAliasCtrlJumps(t *testing.T) {
	m := diffModel()
	m.height = 12 // body = 10
	rows := sameRowsTUI(40, 20, 30)
	mk := func() Model {
		mm := m
		mm.diffView = diffViewWith(rows, []int{20, 30})
		mm.diffTag = "status:x"
		return mm
	}
	for _, pair := range [][2]string{{"n", "ctrl+down"}, {"p", "ctrl+up"}} {
		a := mk()
		a.diffView.offset = 15
		b := mk()
		b.diffView.offset = 15
		ua, _ := a.Update(keyMsg(pair[0]))
		ub, _ := b.Update(keyMsg(pair[1]))
		if ua.(Model).diffView.offset != ub.(Model).diffView.offset {
			t.Fatalf("%s (%d) != %s (%d)", pair[0], ua.(Model).diffView.offset,
				pair[1], ub.(Model).diffView.offset)
		}
	}
}

func TestDiffJumpLandsWithContextAbove(t *testing.T) {
	m := diffModel()
	m.height = 12
	m.diffView = diffViewWith(sameRowsTUI(40, 20), []int{20})
	m.diffTag = "status:x"
	u, _ := m.Update(keyMsg("n"))
	if got := u.(Model).diffView.offset; got != 17 { // 20 - diffLead(3)
		t.Fatalf("offset = %d, want 17 (change with 3 lines above)", got)
	}
}

func TestDiffToggleFlipsModeAndSession(t *testing.T) {
	m := diffModel()
	m.height = 12
	m.diffView = diffViewWith(sameRowsTUI(40, 20), []int{20})
	m.diffTag = "status:x"
	if m.diffView.partial {
		t.Fatal("default mode is full")
	}
	u, _ := m.Update(keyMsg("f"))
	mm := u.(Model)
	if !mm.diffView.partial {
		t.Fatal("f must switch the open view to partial")
	}
	if !mm.diffPartial {
		t.Fatal("f must remember partial as the session default")
	}
	if len(mm.diffView.lines) >= len(mm.diffView.full) {
		t.Fatal("partial mode must collapse unchanged runs")
	}
	u2, _ := mm.Update(keyMsg("f"))
	if u2.(Model).diffView.partial || u2.(Model).diffPartial {
		t.Fatal("a second f must switch back to full")
	}
}

func TestDiffTogglePreservesBlock(t *testing.T) {
	m := diffModel()
	m.height = 12 // body = 10
	m.diffView = diffViewWith(sameRowsTUI(60, 10, 50), []int{10, 50})
	m.diffTag = "status:x"
	u, _ := m.Update(keyMsg("n"))        // first change
	u, _ = u.(Model).Update(keyMsg("n")) // second change
	mm := u.(Model)
	ord := mm.diffView.currentBlockOrdinal()
	u, _ = mm.Update(keyMsg("f"))
	v := u.(Model).diffView
	body := mm.diffBodyRows()
	target := v.blocks[ord] // the same change, remapped into the new line stream
	if target < v.offset || target >= v.offset+body {
		t.Fatalf("toggle lost the focused change: line %d not in view [%d,%d)",
			target, v.offset, v.offset+body)
	}
}

func TestStatusLoaderOpensAtFirstDifference(t *testing.T) {
	dir, repo := newRepoDir(t)
	var base, work strings.Builder
	for i := 0; i < 60; i++ {
		base.WriteString(itoa(i) + "\n")
		if i == 40 {
			work.WriteString("CHANGED\n")
		} else {
			work.WriteString(itoa(i) + "\n")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(base.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "f.txt")
	gitIn(t, dir, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(work.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	m := diffModel()
	m.height = 12
	m.svc = domain.New(repo)
	m.currentWorktree = dir
	msg := m.loadStatusDiffCmd(model.FileStatus{Path: "f.txt", Staged: '.', Unstaged: 'M'})().(diffMsg)
	if msg.view.err != nil {
		t.Fatal(msg.view.err)
	}
	if msg.view.offset == 0 {
		t.Fatal("a diff with a change far down must open scrolled to it, not at the top")
	}
	if msg.view.offset > msg.view.blocks[0] || msg.view.blocks[0] >= msg.view.offset+m.diffBodyRows() {
		t.Fatalf("first block %d not in view [%d,%d)", msg.view.blocks[0],
			msg.view.offset, msg.view.offset+m.diffBodyRows())
	}
}

func TestDiffOpenInheritsSessionMode(t *testing.T) {
	dir, repo := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "f.txt")
	gitIn(t, dir, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := diffModel()
	m.svc = domain.New(repo)
	m.currentWorktree = dir
	m.diffPartial = true
	msg := m.loadStatusDiffCmd(model.FileStatus{Path: "f.txt", Staged: '.', Unstaged: 'M'})().(diffMsg)
	if !msg.view.partial {
		t.Fatal("a new diff must inherit the session's partial mode")
	}
}

func TestCommitLoaderMergeCommitUsesFirstParent(t *testing.T) {
	dir, repo := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "base")
	gitIn(t, dir, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-am", "side edit")
	gitIn(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "main edit")
	gitIn(t, dir, "merge", "--no-ff", "-m", "merge", "side")
	hash := gitOut(t, dir, "rev-parse", "HEAD")

	// The tree lists the merge's FIRST-PARENT diff (CommitFiles passes
	// --first-parent -m), and hash^ IS the first parent — sides must match.
	files, err := repo.CommitFiles(context.Background(), hash)
	if err != nil {
		t.Fatal(err)
	}
	var fEntry *model.CommitFile
	for i := range files {
		if files[i].Path == "f.txt" {
			fEntry = &files[i]
		}
	}
	if fEntry == nil {
		t.Fatal("merge commit's first-parent diff must list f.txt")
	}
	m := filesViewModel()
	m.svc = domain.New(repo)
	msg := m.loadCommitDiffCmd(hash, contentLine{path: "f.txt", status: fEntry.Status})().(diffMsg)
	if msg.view.err != nil {
		t.Fatal(msg.view.err)
	}
	// Old side = first parent ("base"), new side = merge result ("side").
	var sawBase, sawSide bool
	for _, r := range msg.view.full {
		if r.Left == "base" {
			sawBase = true
		}
		if r.Right == "side" {
			sawSide = true
		}
	}
	if !sawBase || !sawSide {
		t.Fatalf("merge diff sides wrong: %+v", msg.view.full)
	}
}

func TestCommitDiffSecondOpenServedFromCache(t *testing.T) {
	dir, repo := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "f.txt")
	gitIn(t, dir, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\nTWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-am", "edit")
	hash := gitOut(t, dir, "rev-parse", "HEAD")

	m := diffModel()
	m.svc = domain.New(repo) // a real Service so both opens share one cache
	line := contentLine{path: "f.txt", status: "M"}

	first := m.loadCommitDiffCmd(hash, line)().(diffMsg)
	if first.view.err != nil {
		t.Fatalf("first open: %v", first.view.err)
	}
	wantBlocks := len(first.view.blocks)

	// Break git: a bare FakeRunner has no responses configured, so any
	// ShowFile now errors. If the second open hits the cache, the broken repo
	// is never consulted and the result still arrives. Mutating the runner
	// on the in-scope repo breaks git while the svc (and its cache) remain.
	repo.Runner = gitexec.NewFakeRunner()

	second := m.loadCommitDiffCmd(hash, line)().(diffMsg)
	if second.view.err != nil {
		t.Fatalf("second open should be served from cache, got err: %v", second.view.err)
	}
	if len(second.view.blocks) != wantBlocks {
		t.Fatalf("cached result differs: blocks %d vs %d", len(second.view.blocks), wantBlocks)
	}
}
