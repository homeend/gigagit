package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/model"
)

const elideLongPath = "internal/very/long/path/to/some/file_with_a_long_name.go"

// A path row that does not fit loses its middle, never its file name.
func TestRenderWindowElidesAPathRowInTheMiddle(t *testing.T) {
	t.Parallel()
	out := renderWindow([]winRow{{text: "> " + elideLongPath, elide: true, elideHead: 2}}, winOpts{w: 40, h: 1, mode: modeCutoff})
	got := strings.TrimRight(ansi.Strip(out[0]), " ")
	if !strings.HasSuffix(got, "file_with_a_long_name.go") || !strings.Contains(got, "…") {
		t.Fatalf("row = %q, want the middle elided and the file name kept", got)
	}
	if !strings.HasPrefix(got, "> ") {
		t.Fatalf("row = %q, want the cursor mark kept", got)
	}
}

// wrap and scroll show the whole row: the flag changes cutoff only.
func TestRenderWindowElideIsCutoffOnly(t *testing.T) {
	t.Parallel()
	out := renderWindow([]winRow{{text: elideLongPath, elide: true}}, winOpts{w: 20, h: 1, mode: modeScroll})
	if strings.Contains(ansi.Strip(out[0]), "…") {
		t.Fatalf("scroll mode elided: %q", out[0])
	}
}

func TestFWindowKeepsTheFileName(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "a.go", elideLongPath) // a.go is selected; the long row is not
	m.width, m.height = 100, 24             // room for the name, not the whole path
	if !strings.Contains(m.View(), "file_with_a_long_name.go") {
		t.Fatalf("the F window cut the file name off:\n%s", m.View())
	}
}

func TestOpenFilesRowsKeepTheFileName(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m.width, m.height = 60, 24
	m.openFiles.touch(m.currentWorktree, wtDoc(elideLongPath), noneShown)
	m.openFiles.touch(m.currentWorktree, wtDoc("a.go"), noneShown) // listed first, so selected
	m, _ = openSwitcher(t, m)
	if !strings.Contains(m.View(), "file_with_a_long_name.go") {
		t.Fatalf("the open-files row cut the file name off:\n%s", m.View())
	}
}

func TestPreviewTitleKeepsTheFileName(t *testing.T) {
	t.Parallel()
	const deep = "a_really_quite_long_directory_name/another_long_directory/leaf_name.txt"
	base := loadedNavModel(t)
	if err := os.MkdirAll(filepath.Join(base.currentWorktree, filepath.Dir(deep)), 0o755); err != nil {
		t.Fatal(err)
	}
	writeWT(t, base, deep, "x\n")
	base, _ = base.openWorktreeFiles()
	tm, _ := base.Update(lsFilesMsg{paths: []string{deep}})
	m := settle(t, tm.(Model))
	m.width, m.height = 70, 24
	if !strings.Contains(m.View(), "leaf_name.txt") {
		t.Fatalf("the preview title cut the file name off:\n%s", m.View())
	}
}

func TestBookmarkRowKeepsTheFileName(t *testing.T) {
	t.Parallel()
	p := &bookmarkPopup{
		items: []model.Bookmark{{ID: "1", Path: "a.go"}, {ID: "2", Path: elideLongPath}},
		rows:  []string{"wt:x / unstaged / a.go", "wt:x / unstaged / " + elideLongPath},
	}
	out := ansi.Strip(Model{width: 60, height: 30}.renderBookmarkPopupBox(p))
	if !strings.Contains(out, "file_with_a_long_name.go") {
		t.Fatalf("the bookmark row cut the file name off:\n%s", out)
	}
}
