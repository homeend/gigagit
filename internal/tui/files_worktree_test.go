package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/model"
)

// wtWindow opens F's working-tree files window over paths.
func wtWindow(t *testing.T, paths ...string) Model {
	t.Helper()
	m := loadedNavModel(t)
	m.focus = panelBranches
	tm, _ := m.Update(keyMsg("F"))
	m = tm.(Model)
	tm, _ = m.Update(lsFilesMsg{paths: paths})
	return tm.(Model)
}

func wtRows(m Model) []string {
	var out []string
	for _, l := range m.filesView.visible() {
		if l.path != "" {
			out = append(out, l.path)
		}
	}
	return out
}

func TestFOpensTheWorktreeFilesWindow(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "b.go", "a.go")
	if m.filesView == nil || !m.inWorktreeFiles() {
		t.Fatal("F did not open the working-tree files window")
	}
	if m.topLayer() != nil {
		t.Fatalf("F opened a %T over the window", m.topLayer())
	}
	if got := fmt.Sprint(wtRows(m)); got != "[a.go b.go]" {
		t.Fatalf("rows = %s, want [a.go b.go]", got)
	}
}

func TestWorktreeFilterIsFuzzy(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "internal/foo/view.go", "a.txt")
	m = fvKeys(t, m, keyMsg("/"), keyMsg("f"), keyMsg("v"), keyMsg("g"))
	if got := fmt.Sprint(wtRows(m)); got != "[internal/foo/view.go]" {
		t.Fatalf("filtered rows = %s", got)
	}
	m = fvKeys(t, m, keyMsg("enter"))
	if m.wtFiles.typing || m.wtFiles.query != "fvg" {
		t.Fatalf("enter: typing=%v query=%q, want the query kept", m.wtFiles.typing, m.wtFiles.query)
	}
	m = fvKeys(t, m, keyMsg("esc"))
	if m.wtFiles.query != "" || len(wtRows(m)) != 2 {
		t.Fatal("esc did not clear the committed filter")
	}
	m = fvKeys(t, m, keyMsg("esc"))
	if m.filesView != nil || m.focus != panelBranches {
		t.Fatalf("second esc: view open=%v focus=%v, want closed and back on Branches", m.filesView != nil, m.focus)
	}
}

func TestWorktreeFilterEscWhileTypingClears(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "a.go", "b.go")
	m = fvKeys(t, m, keyMsg("/"), keyMsg("a"), keyMsg("esc"))
	if m.wtFiles.typing || m.wtFiles.query != "" || len(wtRows(m)) != 2 || m.filesView == nil {
		t.Fatal("esc while typing must clear the filter and keep the window")
	}
}

func TestWorktreeFilesRenderFits(t *testing.T) {
	t.Parallel()
	for _, sz := range [][2]int{{80, 24}, {200, 50}} {
		m := wtWindow(t, "a.go", "internal/very/long/path/to/some/file_with_a_long_name.go")
		m.width, m.height = sz[0], sz[1]
		out := m.View()
		if !strings.Contains(out, "Files (working tree)") {
			t.Fatalf("%v: no window title in\n%s", sz, out)
		}
		lines := strings.Split(out, "\n")
		if len(lines) > m.height {
			t.Fatalf("%v: %d lines, want ≤ %d", sz, len(lines), m.height)
		}
		for _, l := range lines {
			if w := lipgloss.Width(l); w > m.width {
				t.Fatalf("%v: line width %d > %d: %q", sz, w, m.width, l)
			}
		}
	}
}

// wtDiskWindow opens F's window over real files written to the worktree.
func wtDiskWindow(t *testing.T, files map[string]string) Model {
	t.Helper()
	m := loadedNavModel(t)
	var paths []string
	for name, body := range files {
		writeWT(t, m, name, body)
		paths = append(paths, name)
	}
	m, _ = m.openWorktreeFiles()
	tm, _ := m.Update(lsFilesMsg{paths: paths})
	return tm.(Model)
}

// settle delivers the preview's settle message for the current cursor.
func settle(t *testing.T, m Model) Model {
	t.Helper()
	tm, cmd := m.Update(wtPreviewMsg{gen: m.wtPreviewGen, path: m.wtSelected()})
	return pumpAll(t, tm.(Model), cmd)
}

func TestWorktreePreviewFollowsTheSettledCursor(t *testing.T) {
	t.Parallel()
	m := wtDiskWindow(t, map[string]string{"a.txt": "AAA\n", "b.txt": "BBB\n", "c.txt": "CCC\n"})
	tm, cmd := m.Update(keyMsg("down"))
	m = tm.(Model)
	if cmd == nil {
		t.Fatal("a cursor move scheduled no preview")
	}
	stale := wtPreviewMsg{gen: m.wtPreviewGen, path: m.wtSelected()}
	m = fvKeys(t, m, keyMsg("down"))
	tm, cmd = m.Update(stale)
	if cmd != nil || tm.(Model).filesPreview != nil && tm.(Model).filesPreview.path == "b.txt" {
		t.Fatal("a superseded settle loaded its file")
	}
	m = settle(t, tm.(Model))
	d := m.filesPreview
	if d == nil || d.path != "c.txt" || d.p.lines[0].raw != "CCC" {
		t.Fatalf("preview = %+v, want c.txt's disk bytes", d)
	}
	if !m.filesTreeFocused {
		t.Fatal("the preview took the focus from the list")
	}
}

func TestWorktreePreviewIsNotAnOpenFile(t *testing.T) {
	t.Parallel()
	m := settle(t, wtDiskWindow(t, map[string]string{"a.txt": "AAA\n"}))
	if m.filesPreview == nil {
		t.Fatal("no preview")
	}
	if n := len(m.openFiles.list(m.currentWorktree)); n != 0 {
		t.Fatalf("the live preview joined the open files (%d)", n)
	}
}

func TestWorktreePreviewIsWatched(t *testing.T) {
	t.Parallel()
	m := settle(t, wtDiskWindow(t, map[string]string{"a.txt": "AAA\n"}))
	if !slices.Contains(m.watchedDocs(), m.filesPreview) {
		t.Fatal("the live preview is not watched")
	}
	writeWT(t, m, "a.txt", "CHANGED\n")
	m = tick(t, m, time.Now())
	if m.filesPreview.p.lines[0].raw != "CHANGED" {
		t.Fatalf("the preview did not reload: %+v", m.filesPreview.p.lines[0])
	}
}

func TestWorktreeRightFocusesPreview(t *testing.T) {
	t.Parallel()
	m := settle(t, wtDiskWindow(t, map[string]string{"a.txt": "AAA\n"}))
	m = fvKeys(t, m, keyMsg("right"), keyMsg("/"), keyMsg("x"))
	if m.filesTreeFocused || !m.filesPreview.p.search.typing {
		t.Fatal("right + / did not start the preview's own search")
	}
	if m.wtFiles.query != "" || m.wtFiles.typing {
		t.Fatal("the preview's search typed into the file filter")
	}
	m = fvKeys(t, m, keyMsg("esc"), keyMsg("esc"), keyMsg("left"))
	if !m.filesTreeFocused {
		t.Fatal("left did not hand the keys back to the list")
	}
}

func TestWorktreeFilesCtrlTFullScreen(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "a.go")
	m.width, m.height = 120, 30
	if !strings.Contains(m.View(), "Commits (") {
		t.Fatal("the split view shows no Commits column")
	}
	m = fvKeys(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	out := m.View()
	if strings.Contains(out, "Commits (") {
		t.Fatal("ctrl+t left the Commits column on screen")
	}
	if w := lipgloss.Width(strings.Split(out, "\n")[1]); w != 120 {
		t.Fatalf("the full-screen box is %d wide, want 120", w)
	}
	m = fvKeys(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if !strings.Contains(m.View(), "Commits (") {
		t.Fatal("a second ctrl+t did not restore the split")
	}
}

func TestFFromDiffReturnsToDiff(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	dv := &diffView{title: "x.go"}
	m = m.pushLayer(dv)
	tm, cmd := m.Update(keyMsg("F"))
	m = tm.(Model)
	if !m.inWorktreeFiles() || m.topLayer() != nil {
		t.Fatalf("F over the diff: window %v, top %T — want the window with the diff parked", m.inWorktreeFiles(), m.topLayer())
	}
	tm, _ = m.Update(lsFilesMsg{paths: []string{"a.go"}})
	m = fvKeys(t, tm.(Model), keyMsg("esc"))
	if m.topLayer() != dv {
		t.Fatalf("esc returned to %T, want the diff", m.topLayer())
	}
	_ = cmd
}

func TestPaletteFindOpensTheWindow(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	for _, e := range paletteCommands() {
		if e.keyHint == "F" {
			m = m.pushLayer(&commandPalette{})
			nm, _ := e.run(m)
			if !nm.inWorktreeFiles() || nm.topLayer() != nil {
				t.Fatalf("palette Find: window %v, top %T", nm.inWorktreeFiles(), nm.topLayer())
			}
			return
		}
	}
	t.Fatal("no palette entry for F")
}

func TestWorktreeJKAreQueryTextWhileTyping(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "jk.go", "a.go")
	m = fvKeys(t, m, keyMsg("/"), keyMsg("j"), keyMsg("k"))
	if m.wtFiles.query != "jk" {
		t.Fatalf("query = %q, want jk", m.wtFiles.query)
	}
}

func TestWorktreeLsFilesIgnoredWhenClosed(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	m, _ = m.openWorktreeFiles()
	m = fvKeys(t, m, keyMsg("esc"))
	tm, _ := m.Update(lsFilesMsg{paths: []string{"a.go"}})
	if tm.(Model).filesView != nil {
		t.Fatal("a late load reopened the window")
	}
}

func TestWorktreeFooterShowsItsOwnKeys(t *testing.T) {
	t.Parallel()
	m := settle(t, wtDiskWindow(t, map[string]string{"a.txt": "AAA\n"}))
	got, ok := m.footerOverride()
	if !ok || !strings.Contains(got, "[enter/.] actions") || strings.Contains(got, "[h] hist") || strings.Contains(got, "diff") {
		t.Fatalf("list footer = %q", got)
	}
	m = fvKeys(t, m, keyMsg("right"))
	got, _ = m.footerOverride()
	if !strings.Contains(got, "back to list") {
		t.Fatalf("preview footer = %q", got)
	}
}

// wtWindowWith opens F's window over paths with st as the working-tree status.
func wtWindowWith(t *testing.T, st model.WorkingTreeStatus, paths ...string) Model {
	t.Helper()
	m := loadedNavModel(t)
	m.focus = panelBranches
	m.status = st
	tm, _ := m.Update(keyMsg("F"))
	m = tm.(Model)
	tm, _ = m.Update(lsFilesMsg{paths: paths})
	return tm.(Model)
}

// wtTexts is every visible row's text, a heading marked with a trailing "#".
func wtTexts(m Model) []string {
	var out []string
	for _, l := range m.filesView.visible() {
		if l.heading {
			out = append(out, l.text+"#")
			continue
		}
		out = append(out, l.text)
	}
	return out
}

func TestWorktreeFilesGroupUnderDirectoryHeadings(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "src/b.go", "README.md", "src/a.go", "docs/x.md")
	got := fmt.Sprint(wtTexts(m))
	if got != "[   README.md docs/#      x.md src/#      a.go      b.go]" {
		t.Fatalf("rows = %q, want root files first, then one heading per directory with its files indented", got)
	}
	if got := fmt.Sprint(wtRows(m)); got != "[README.md docs/x.md src/a.go src/b.go]" {
		t.Fatalf("paths = %s", got)
	}
}

func TestWorktreeFilesShowStatusLetters(t *testing.T) {
	t.Parallel()
	st := model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "m.go", Kind: model.KindTracked, Staged: '.', Unstaged: 'M'},
		{Path: "s.go", Kind: model.KindTracked, Staged: 'A', Unstaged: '.'},
		{Path: "n.txt", Kind: model.KindUntracked},
	}}
	m := wtWindowWith(t, st, "c.go", "m.go", "s.go")
	if got := fmt.Sprint(wtTexts(m)); got != "[   c.go M  m.go ?  n.txt A  s.go]" {
		t.Fatalf("rows = %q, want a status letter column: blank for a clean file, ? for untracked", got)
	}
}

func TestWorktreeCursorSkipsALeadingHeading(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "src/a.go", "src/b.go")
	if got := m.wtSelected(); got != "src/a.go" {
		t.Fatalf("cursor on %q, want the first file, not the src/ heading", got)
	}
}

func TestWorktreeFilterCursorLandsOnBestMatch(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "zz/view.go", "aa/viewer_helper.go", "c.txt")
	m = fvKeys(t, m, keyMsg("/"), keyMsg("v"), keyMsg("i"), keyMsg("e"), keyMsg("w"), keyMsg("."), keyMsg("g"), keyMsg("o"))
	if got := fmt.Sprint(wtRows(m)); got != "[aa/viewer_helper.go zz/view.go]" {
		t.Fatalf("filtered rows = %s, want both matches grouped in tree order", got)
	}
	if got := m.wtSelected(); got != "zz/view.go" {
		t.Fatalf("cursor on %q, want the best-ranked match", got)
	}
}

func TestWorktreeTitleCountsFilesNotHeadings(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "src/a.go", "b.txt")
	m = fvKeys(t, m, keyMsg("/"), keyMsg("a"), keyMsg(".")) // only src/a.go matches
	if got := m.wtTitle(); !strings.Contains(got, "1/2") {
		t.Fatalf("title = %q, want 1/2 (the src/ heading is not a file)", got)
	}
}

func TestWorktreeCursorOpensOnTheFirstRow(t *testing.T) {
	t.Parallel()
	// ".a/x.go" sorts before "b.txt", but the tree lists root files first.
	m := wtWindow(t, ".a/x.go", "b.txt")
	if got := m.wtSelected(); got != "b.txt" {
		t.Fatalf("cursor on %q, want the first row of the tree (the root file)", got)
	}
}

func TestWorktreeEnterOnAHeadingIsInert(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "src/a.go")
	m = fvKeys(t, m, keyMsg("up")) // onto the src/ heading
	if m.wtSelected() != "" {
		t.Fatalf("cursor on %q, want the heading", m.wtSelected())
	}
	m = fvKeys(t, m, keyMsg("enter"))
	if m.actionMenu != nil || m.topLayer() != nil || !m.inWorktreeFiles() {
		t.Fatal("enter on a heading must not open the file actions")
	}
}

// wtListLines is the F window's left-column list: the text of each row line
// under the title, trimmed, in order.
func wtListLines(m Model) []string {
	var out []string
	seen := false
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if !seen {
			seen = strings.Contains(l, "Files (working tree)")
			continue
		}
		if !strings.HasPrefix(l, "│") {
			break
		}
		cell := l[len("│"):]
		if i := strings.Index(cell, "│"); i >= 0 {
			cell = cell[:i]
		}
		out = append(out, strings.TrimSpace(cell))
	}
	return out
}

func TestWorktreeFirstRowNamesTheDirectoryBelow(t *testing.T) {
	t.Parallel()
	paths := []string{"a.go"}
	for i := 0; i < 40; i++ {
		paths = append(paths, fmt.Sprintf("src/f%02d.go", i))
	}
	m := wtWindow(t, paths...)
	m.width, m.height = 120, 20
	if rows := wtListLines(m); len(rows) == 0 || rows[0] != "." {
		t.Fatalf("at the top the first row = %q, want . (the root)\n%s", rows, m.View())
	}
	for i := 0; i < 30; i++ {
		m = fvKeys(t, m, keyMsg("down"))
	}
	if m.wtSelected() != "src/f28.go" { // a.go, the src/ heading, then f00…
		t.Fatalf("cursor on %q", m.wtSelected())
	}
	rows := wtListLines(m)
	if len(rows) == 0 || rows[0] != "src/" {
		t.Fatalf("scrolled into src/: first row = %q, want the directory of the rows below\n%s", rows, m.View())
	}
	if n := strings.Count(strings.Join(rows, "\n"), "src/"); n != 1 {
		t.Fatalf("src/ shown %d times, want once (the heading itself scrolled off)", n)
	}
}

func TestWorktreeFirstRowIsTheHeadingWhenItIsAtTheTop(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "src/a.go", "src/b.go")
	m.width, m.height = 120, 20
	rows := wtListLines(m)
	if len(rows) < 3 || rows[0] != "src/" || !strings.HasSuffix(rows[1], "a.go") {
		t.Fatalf("rows = %q, want src/ once, then its files", rows)
	}
	if n := strings.Count(strings.Join(rows, "\n"), "src/"); n != 1 {
		t.Fatalf("src/ shown %d times, want once", n)
	}
}

func TestWorktreeRevealLandsOnTheCursorRowUnderTheStickyLine(t *testing.T) {
	t.Parallel()
	const long = "a-root-file-with-a-name-far-too-long-for-the-column.go"
	m := wtWindow(t, long, "src/b.go")
	m.width, m.height = 100, 20
	lines, _, y, ok := m.filesTreeReveal()
	if !ok || len(lines) != 1 {
		t.Fatalf("no reveal for a cut cursor row (ok=%v lines=%q)", ok, lines)
	}
	view := strings.Split(ansi.Strip(m.View()), "\n")
	if y < 1 || y >= len(view) || !strings.Contains(view[y], long) {
		t.Fatalf("reveal at y=%d does not carry the cursor row's full name:\n%s", y, strings.Join(view, "\n"))
	}
	if got := strings.TrimSpace(strings.Trim(strings.SplitN(view[y-1], "│", 3)[1], " ")); got != "." {
		t.Fatalf("the line above the reveal = %q, want the sticky . (the reveal must not cover it)", got)
	}
}

func TestWorktreeFilterEditsAtTheCursor(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "ac.go", "b.go") // only ac.go matches the final "ac"
	m = fvKeys(t, m, keyMsg("/"), keyMsg("a"), keyMsg("b"), keyMsg("c"),
		tea.KeyMsg{Type: tea.KeyLeft}, tea.KeyMsg{Type: tea.KeyLeft}, keyMsg("X"))
	if m.wtFiles.query != "aXbc" {
		t.Fatalf("query = %q, want aXbc (typed at the cursor)", m.wtFiles.query)
	}
	m = fvKeys(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.wtFiles.query != "abc" {
		t.Fatalf("query = %q, want abc (backspace erases before the cursor)", m.wtFiles.query)
	}
	m = fvKeys(t, m, tea.KeyMsg{Type: tea.KeyDelete})
	if m.wtFiles.query != "ac" {
		t.Fatalf("query = %q, want ac (delete erases under the cursor)", m.wtFiles.query)
	}
	if got := ansi.Strip(m.wtSearchLine()); got != "/ac" {
		t.Fatalf("search line = %q, want /ac (the cursor is a painted cell, not a glyph)", got)
	}
	if !m.wtFiles.typing || len(wtRows(m)) != 1 {
		t.Fatalf("typing=%v rows=%v: editing must keep the filter live", m.wtFiles.typing, wtRows(m))
	}
}

func TestWorktreeFilterMovesByWord(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "a.go")
	m = fvKeys(t, m, keyMsg("/"), keyMsg("f"), keyMsg("o"), keyMsg("o"), tea.KeyMsg{Type: tea.KeySpace}, keyMsg("b"), keyMsg("a"), keyMsg("r"),
		tea.KeyMsg{Type: tea.KeyCtrlLeft}, keyMsg("X"))
	if m.wtFiles.query != "foo Xbar" {
		t.Fatalf("query = %q, want foo Xbar (ctrl+← to the word start)", m.wtFiles.query)
	}
	m = fvKeys(t, m, tea.KeyMsg{Type: tea.KeyCtrlRight}, keyMsg("Y"))
	if m.wtFiles.query != "foo XbarY" {
		t.Fatalf("query = %q, want foo XbarY (ctrl+→ to the word end)", m.wtFiles.query)
	}
	m = fvKeys(t, m, tea.KeyMsg{Type: tea.KeyHome}, keyMsg("Z"), tea.KeyMsg{Type: tea.KeyEnd}, keyMsg("W"))
	if m.wtFiles.query != "Zfoo XbarYW" {
		t.Fatalf("query = %q, want Zfoo XbarYW (home/end)", m.wtFiles.query)
	}
}

func TestWorktreeFilterReopensWithTheCursorAtTheEnd(t *testing.T) {
	t.Parallel()
	m := wtWindow(t, "a.go")
	m = fvKeys(t, m, keyMsg("/"), keyMsg("a"), keyMsg("enter"), keyMsg("/"), keyMsg("b"))
	if m.wtFiles.query != "ab" {
		t.Fatalf("query = %q, want ab (/ again edits the kept query at its end)", m.wtFiles.query)
	}
}
