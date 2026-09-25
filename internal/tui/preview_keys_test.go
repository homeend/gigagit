package tui

import (
	"strings"
	"testing"
)

// maximizedPreview reports whether the screen shows the preview alone: its
// title, and none of the list beside it.
func maximizedPreview(m Model, list string) bool {
	out := m.View()
	return strings.Contains(out, "View a.txt") && !strings.Contains(out, list)
}

func TestCtrlTMaximizesFsPreview(t *testing.T) {
	t.Parallel()
	m := settle(t, wtDiskWindow(t, map[string]string{"a.txt": "AAA\n"}))
	m.width, m.height = 120, 30
	m = fvKeys(t, m, keyMsg("right"), ctrlT())
	if !maximizedPreview(m, "Files (working tree)") {
		t.Fatalf("ctrl+t on F's preview did not fill the screen:\n%s", m.View())
	}
	m = fvKeys(t, m, ctrlT())
	if maximizedPreview(m, "Files (working tree)") {
		t.Fatal("a second ctrl+t did not bring the list back")
	}
	m = fvKeys(t, m, ctrlT(), keyMsg("left"))
	if maximizedPreview(m, "Files (working tree)") || !m.filesTreeFocused {
		t.Fatal("going back to the list must show it")
	}
}

func TestCtrlTMaximizesTheCommitTreePreview(t *testing.T) {
	t.Parallel()
	m := linkPreviewModel(t, "one\ntwo\n", 0)
	m.width, m.height = 120, 30
	m.filesTitle = "Files abcdef tree"
	m = fvKeys(t, m, ctrlT())
	if !maximizedPreview(m, "Files abcdef tree") {
		t.Fatalf("ctrl+t on the commit tree's preview did not fill the screen:\n%s", m.View())
	}
}

// ctrl+] on F's live preview keeps the file open in the background; the
// preview goes on following the cursor.
func TestCtrlBracketOnFsPreviewBackgroundsTheFile(t *testing.T) {
	t.Parallel()
	m := settle(t, wtDiskWindow(t, map[string]string{"a.txt": "AAA\n", "b.txt": "BBB\n"}))
	m = fvKeys(t, m, keyMsg("right"), keyCtrlBracket())
	l := m.openFiles.list(m.currentWorktree)
	if len(l) != 1 || l[0].path != "a.txt" {
		t.Fatalf("open files = %v, want [a.txt]", regPaths(m.openFiles, m.currentWorktree))
	}
	if !m.filesTreeFocused || !strings.Contains(m.statusMsg, "a.txt is in the background") {
		t.Fatalf("focus on list %v, status %q", m.filesTreeFocused, m.statusMsg)
	}
	m = settle(t, fvKeys(t, m, keyMsg("down")))
	if m.filesPreview == nil || m.filesPreview.path != "b.txt" {
		t.Fatal("the preview stopped following the cursor")
	}
	if len(m.openFiles.list(m.currentWorktree)) != 1 || m.docShown(l[0]) {
		t.Fatal("the backgrounded file must stay open, off screen")
	}
}

// A file already open is not opened twice: ctrl+] on its preview brings the
// open one up the list.
func TestCtrlBracketOnFsPreviewReusesAnOpenFile(t *testing.T) {
	t.Parallel()
	m := wtDiskWindow(t, map[string]string{"a.txt": "AAA\n"})
	open := wtDoc("a.txt")
	m.openFiles.touch(m.currentWorktree, open, noneShown)
	m = settle(t, m)
	m = fvKeys(t, m, keyMsg("right"), keyCtrlBracket())
	if l := m.openFiles.list(m.currentWorktree); len(l) != 1 || l[0] != open {
		t.Fatalf("open files = %v, want the one already open", regPaths(m.openFiles, m.currentWorktree))
	}
}

// The list and preview boxes carry no hint line of their own: the app's
// bottom bar shows the focused box's keys.
func TestFilesBoxesHaveNoHintLine(t *testing.T) {
	t.Parallel()
	m := settle(t, wtDiskWindow(t, map[string]string{"a.txt": "AAA\n"}))
	m.width, m.height = 160, 30
	out := m.View()
	for _, boxHint := range []string{"[→] preview  [ctrl+t] full  [esc] close", "[spc] mark  [/] find  [esc] close"} {
		if strings.Contains(out, boxHint) {
			t.Fatalf("a box still draws its hint line %q:\n%s", boxHint, out)
		}
	}
	if !strings.Contains(out, "[enter/.] actions") {
		t.Fatalf("the bottom bar lost the list's keys:\n%s", out)
	}
	if got, want := m.filePreviewRowsCap(), m.layout().boxH[panelCommits]-3; got != want {
		t.Fatalf("preview rows = %d, want %d (the hint row is the file's now)", got, want)
	}
	tree := linkPreviewModel(t, "one\n", 0)
	tree.width, tree.height = 160, 30
	tree.filesTreeFocused = true
	if strings.Contains(tree.View(), "[h] history  [b] blame  [/] search  [esc] close") {
		t.Fatal("the commit file tree still draws its hint line")
	}
}

func TestPreviewFootersOfferFullAndBackground(t *testing.T) {
	t.Parallel()
	f := fvKeys(t, settle(t, wtDiskWindow(t, map[string]string{"a.txt": "AAA\n"})), keyMsg("right"))
	tree := linkPreviewModel(t, "one\n", 0)
	for name, m := range map[string]Model{"F": f, "tree": tree} {
		got, _ := m.footerOverride()
		if !strings.Contains(got, "[ctrl+t] full") || !strings.Contains(got, "[ctrl+]] background") {
			t.Fatalf("%s preview footer = %q", name, got)
		}
	}
}

func TestViewerKeepsItsHintLine(t *testing.T) {
	t.Parallel()
	m, _ := viewerModel(t)
	if !strings.Contains(m.View(), "[ctrl+]] background") {
		t.Fatal("the full-screen viewer lost its hint line")
	}
}

// ctrl+] on a row of F's list opens that file in the background — loaded,
// in the ctrl+\ list, off screen — whether or not the preview shows it yet.
func TestCtrlBracketOnFsListRowBackgroundsTheFile(t *testing.T) {
	t.Parallel()
	m := wtDiskWindow(t, map[string]string{"a.txt": "AAA\n", "b.txt": "BBB\n"})
	tm, _ := m.Update(keyMsg("down")) // on b.txt; its preview has not settled
	m = tm.(Model)
	tm, cmd := m.Update(keyCtrlBracket())
	m = pumpAll(t, tm.(Model), cmd)
	l := m.openFiles.list(m.currentWorktree)
	if len(l) != 1 || l[0].path != "b.txt" {
		t.Fatalf("open files = %v, want [b.txt]", regPaths(m.openFiles, m.currentWorktree))
	}
	if l[0].p.lines[0].raw != "BBB" || m.docShown(l[0]) {
		t.Fatalf("b.txt: loaded %q, shown %v — want loaded and off screen", l[0].p.lines[0].raw, m.docShown(l[0]))
	}
	if !strings.Contains(m.statusMsg, "b.txt is in the background") || !m.inWorktreeFiles() {
		t.Fatalf("status %q, window open %v", m.statusMsg, m.inWorktreeFiles())
	}
	if f, _ := m.footerOverride(); !strings.Contains(f, "[ctrl+]] background") {
		t.Fatalf("the list's bottom bar does not offer it: %q", f)
	}
}
