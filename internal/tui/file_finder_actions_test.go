package tui

import (
	"io"
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

func actionIDs(rows []actionRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.id)
	}
	sort.Strings(out)
	return out
}

// finderRow returns the run func of the row id, or fails the test.
func finderRow(t *testing.T, rows []actionRow, id string) func(Model) (tea.Model, tea.Cmd) {
	t.Helper()
	for _, r := range rows {
		if r.id == id {
			return r.run
		}
	}
	t.Fatalf("no action row %q in %v", id, actionIDs(rows))
	return nil
}

// finderSetup opens F's window over path in a 2-commit model and returns it
// with path's action rows.
func finderSetup(t *testing.T, path string) (Model, []actionRow) {
	t.Helper()
	m := loadedModelLinearCommits(t, 2)
	m, _ = m.openWorktreeFiles()
	tm, _ := m.Update(lsFilesMsg{paths: []string{path}})
	m = tm.(Model)
	return m, m.worktreeFileRows(path, false)
}

func TestWorktreeEnterAndDotOpenTheActions(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"enter", "."} {
		m := wtWindow(t, "a.go")
		m = fvKeys(t, m, keyMsg(key))
		if m.actionMenu == nil {
			t.Fatalf("%s did not open the file's actions", key)
		}
		got := strings.Join(actionIDs(m.actionMenu.rows), " ")
		if !strings.Contains(got, "ff-view") || !strings.Contains(got, "copy-file-link") {
			t.Fatalf("%s: rows = %s", key, got)
		}
	}
}

func TestWorktreeActionsTrackedVsUntracked(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "a.go")
	common := []string{"ff-copy-abspath", "ff-copy-name", "ff-copy-path", "ff-editor", "ff-view"}
	tracked := strings.Join(actionIDs(m.worktreeFileRows("a.go", false)), " ")
	for _, id := range append(common, "ff-diff", "ff-history", "ff-blame", "ff-commits-touching") {
		if !strings.Contains(tracked, id) {
			t.Fatalf("tracked rows %s miss %s", tracked, id)
		}
	}
	untracked := strings.Join(actionIDs(m.worktreeFileRows("n.txt", true)), " ")
	for _, id := range common {
		if !strings.Contains(untracked, id) {
			t.Fatalf("untracked rows %s miss %s", untracked, id)
		}
	}
	for _, id := range []string{"ff-diff", "ff-history", "ff-blame", "ff-commits-touching"} {
		if strings.Contains(untracked, id) {
			t.Fatalf("an untracked file offers %s", id)
		}
	}
}

// View content opens the working-tree version in the full-screen viewer — an
// open file, watched and reloaded — over the window, so esc returns to it.
func TestWorktreeViewContentOpensTheViewer(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	writeWT(t, m, "w.txt", "ON DISK\n")
	m, _ = m.openWorktreeFiles()
	tm, _ := m.Update(lsFilesMsg{paths: []string{"w.txt"}})
	m = tm.(Model)
	tm, cmd := finderRow(t, m.worktreeFileRows("w.txt", false), "ff-view")(m)
	m = pumpAll(t, tm.(Model), cmd)
	fv, ok := m.topLayer().(*fileViewer)
	if !ok {
		t.Fatalf("top layer = %T, want the file viewer", m.topLayer())
	}
	if fv.src.kind != srcWorktree || fv.p.lines[0].raw != "ON DISK" {
		t.Fatalf("viewer shows src %v line %+v, want the working-tree bytes", fv.src, fv.p.lines[0])
	}
	if m.openFiles.find(m.currentWorktree, fv.key()) == nil {
		t.Fatal("the viewed file is not in the open-files list")
	}
	m = fvKeys(t, m, keyMsg("esc"))
	if m.topLayer() != nil || !m.inWorktreeFiles() {
		t.Fatalf("esc on the viewer: top %T, window open %v — want back in the window", m.topLayer(), m.inWorktreeFiles())
	}
}

// The file is on disk, so the editor edits it — the live "Edit in editor"
// path, not a read-only temp copy of HEAD.
func TestWorktreeEditorEditsTheFile(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	for _, r := range m.worktreeFileRows("w.txt", false) {
		if r.id == "ff-editor" {
			if r.label != "Edit in editor" {
				t.Fatalf("ff-editor label = %q, want the live edit", r.label)
			}
			if _, cmd := r.run(m); cmd == nil {
				t.Fatal("ff-editor returned no command")
			}
			return
		}
	}
	t.Fatal("no ff-editor row")
}

func TestCopyFileNameIsTheBaseName(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	copied := ""
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	_, cmd := finderRow(t, m.worktreeFileRows("dir/sub/name.go", false), "ff-copy-name")(m)
	pumpAll(t, m, cmd)
	if copied != "name.go" {
		t.Fatalf("copied %q, want name.go", copied)
	}
}

func TestFileFinderHistoryActionOpensHistoryLayer(t *testing.T) {
	t.Parallel()
	m, rows := finderSetup(t, "a/b.go")
	nm, _ := finderRow(t, rows, "ff-history")(m)
	m = nm.(Model)
	if layerOf[*historyView](m) == nil {
		t.Fatal("history action should push a historyView layer")
	}
	if !m.inWorktreeFiles() {
		t.Fatal("the window must stay under history (esc returns to it)")
	}
}

func TestFileFinderDiffActionOpensDiffLayer(t *testing.T) {
	t.Parallel()
	// file0.txt is a real tracked file in loadedModelLinearCommits's fixture:
	// the diff action does a real HEAD resolve + git show.
	const path = "file0.txt"
	m, rows := finderSetup(t, path)
	nm, cmd := finderRow(t, rows, "ff-diff")(m)
	m = nm.(Model)
	if layerOf[*diffView](m) == nil {
		t.Fatal("ff-diff should push a diffView layer")
	}
	// ff-diff resolves HEAD to a sha before it builds the left Endpoint, so
	// m.diffTag (built from Endpoint.Hash verbatim) carries the sha, never
	// the rev-spec "HEAD". The oracle, m.commits[0], came from a different
	// git call (the feed's git log).
	if len(m.commits) == 0 {
		t.Fatal("fixture must have commits to name the expected HEAD sha")
	}
	wantTag := "cmp:" + m.commits[0].Hash + ":" + model.WorkTreeEndpoint().CacheTag() + ":" + path
	if m.diffTag != wantTag {
		t.Fatalf("diffTag mismatch\n got:  %q\nwant: %q", m.diffTag, wantTag)
	}
	if cmd == nil {
		t.Fatal("ff-diff should return a load command")
	}
	dmsg, ok := cmd().(diffMsg)
	if !ok {
		t.Fatalf("expected a diffMsg, got %T", cmd())
	}
	if dmsg.tag != m.diffTag || dmsg.view.err != nil {
		t.Fatalf("diffMsg tag %q err %v, want tag %q and no error", dmsg.tag, dmsg.view.err, m.diffTag)
	}
}

func TestFileFinderBlameActionOpensBlameLayer(t *testing.T) {
	t.Parallel()
	m, rows := finderSetup(t, "a/b.go")
	nm, _ := finderRow(t, rows, "ff-blame")(m)
	m = nm.(Model)
	if layerOf[*blameView](m) == nil {
		t.Fatal("ff-blame should push a blameView layer")
	}
}

func TestFileFinderCommitsTouchingSeedsPathFilter(t *testing.T) {
	t.Parallel()
	m, rows := finderSetup(t, "internal/engine/ops_basic.go")
	mm, _ := finderRow(t, rows, "ff-commits-touching")(m)
	got := mm.(Model)
	if len(got.commitFilter.Paths) != 1 || got.commitFilter.Paths[0] != "internal/engine/ops_basic.go" {
		t.Fatalf("path not seeded: %+v", got.commitFilter.Paths)
	}
	if got.focus != panelCommits || got.filesView != nil {
		t.Fatal("should close the window and focus Commits after seeding")
	}
}

func TestFileFinderCopyRowsReturnCmds(t *testing.T) {
	t.Parallel()
	m, rows := finderSetup(t, "a/b.go")
	for _, id := range []string{"ff-copy-path", "ff-copy-abspath", "ff-copy-name"} {
		if _, cmd := finderRow(t, rows, id)(m); cmd == nil {
			t.Fatalf("%s returned no command", id)
		}
	}
}
