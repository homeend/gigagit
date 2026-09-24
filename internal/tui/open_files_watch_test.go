package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/i18n"
)

// watchModel is loadedNavModel with name (content) on disk, open in the
// full-screen viewer and loaded.
func watchModel(t *testing.T, name, content string) (Model, *openFile) {
	t.Helper()
	m := loadedNavModel(t)
	writeWT(t, m, name, content)
	nm, cmd := m.openFileViewer(name, 0)
	m = pumpAll(t, nm, cmd)
	d := m.openFiles.find(m.currentWorktree, docKey(fileSource{kind: srcWorktree}, name))
	if d == nil {
		t.Fatal("the opened file is not in the open-files list")
	}
	return m, d
}

func writeWT(t *testing.T, m Model, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(m.currentWorktree, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func removeWT(t *testing.T, m Model, name string) {
	t.Helper()
	if err := os.Remove(filepath.Join(m.currentWorktree, name)); err != nil {
		t.Fatal(err)
	}
}

func deletedPlaceholder(d *openFile) bool {
	return len(d.p.lines) == 1 && d.p.lines[0].text == i18n.T("(file deleted on disk)")
}

func TestLoadDocStampsTheDiskState(t *testing.T) {
	t.Parallel()
	_, d := watchModel(t, "w.txt", "one\ntwo\n")
	if !d.disk.known || d.disk.missing || d.disk.size != int64(len("one\ntwo\n")) {
		t.Fatalf("disk = %+v, want the file's stat", d.disk)
	}
	if d.loading {
		t.Fatal("still loading after the fill")
	}
}

func TestLoadDocOfAMissingFileShowsDeleted(t *testing.T) {
	t.Parallel()
	m, d := watchModel(t, "w.txt", "one\n")
	removeWT(t, m, "w.txt")
	m = pumpAll(t, m, m.loadDoc(d))
	if !deletedPlaceholder(d) || !d.disk.missing {
		t.Fatalf("lines = %+v disk = %+v, want the deleted placeholder", d.p.lines, d.disk)
	}
}

func TestPlaceholderFillKeepsTheSavedPlace(t *testing.T) {
	t.Parallel()
	d := wtDoc("x")
	d.fill(fileContentMsg{tag: d.tag, lines: docLines(50)}, 10, 80)
	d.p.cur, d.p.sel = 29, 20
	d.fill(fileContentMsg{tag: d.tag, lines: []contentLine{{text: "(file deleted on disk)"}}, reload: true}, 10, 80)
	d.keepPlace() // a bringToFront on the placeholder must not overwrite the saved place
	if d.keep.line != 30 {
		t.Fatalf("saved line = %d, want 30", d.keep.line)
	}
	d.fill(fileContentMsg{tag: d.tag, lines: docLines(50), reload: true}, 10, 80)
	if d.p.cur != 29 || d.p.sel != 20 {
		t.Fatalf("cur/sel = %d/%d, want 29/20", d.p.cur, d.p.sel)
	}
}

func TestBackgroundDocReceivesItsLoad(t *testing.T) {
	t.Parallel()
	m, d := watchModel(t, "w.txt", "old\n")
	m = fvKeys(t, m, keyCtrlBracket())
	if m.docShown(d) {
		t.Fatal("ctrl+] left the file on screen")
	}
	writeWT(t, m, "w.txt", "new\n")
	tm, _ := m.Update(m.loadDoc(d)())
	m = tm.(Model)
	if d.p.lines[0].raw != "new" {
		t.Fatalf("background doc not filled: %+v", d.p.lines[0])
	}
	m = m.closeDoc(d)
	writeWT(t, m, "w.txt", "newer\n")
	m.Update(m.loadDoc(d)()) // a closed document's result finds nothing
	if d.p.lines[0].raw != "new" {
		t.Fatalf("closed doc was filled: %+v", d.p.lines[0])
	}
}

func TestLoadingFlagSetUntilFill(t *testing.T) {
	t.Parallel()
	m, d := watchModel(t, "w.txt", "a\n")
	cmd := m.loadDoc(d)
	if !d.loading {
		t.Fatal("loadDoc did not mark the doc loading")
	}
	m.Update(cmd())
	if d.loading {
		t.Fatal("the fill did not clear loading")
	}
}

func TestFindTagSearchesEveryWorktree(t *testing.T) {
	t.Parallel()
	r := &openFilesReg{}
	a, b := wtDoc("a"), wtDoc("b")
	r.touch("wt1", a, noneShown)
	r.touch("wt2", b, noneShown)
	if r.findTag(a.tag) != a || r.findTag(b.tag) != b || r.findTag("nope") != nil {
		t.Fatal("findTag did not search every worktree")
	}
	var nilReg *openFilesReg
	if nilReg.findTag(a.tag) != nil {
		t.Fatal("nil registry found a doc")
	}
}
